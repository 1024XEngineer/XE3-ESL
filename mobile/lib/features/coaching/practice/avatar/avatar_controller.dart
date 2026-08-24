import 'dart:async';
import 'dart:developer' as developer;

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

import 'avatar_audio.dart';
import 'avatar_models.dart';
import 'avatar_renderer.dart';
import 'avatar_session_token_client.dart';

/// Coordinates the short-lived capability, renderer lifecycle, and exclusive
/// avatar audio playback for one practice page.
final class AvatarController extends ChangeNotifier {
  AvatarController({
    required AvatarRenderer renderer,
    required this._tokenClient,
    required this._fallbackPlayback,
    required this._fallbackStop,
    AvatarDelay? delay,
    this.chunkDuration = const Duration(seconds: 1),
    this.interChunkDelay = const Duration(milliseconds: 100),
  }) : _renderer = renderer,
       _delay = delay ?? Future<void>.delayed,
       _state = AvatarControllerState(
         phase: AvatarControllerPhase.idle,
         renderer: renderer.state,
       ) {
    if (chunkDuration <= Duration.zero || interChunkDelay < Duration.zero) {
      throw ArgumentError('Avatar audio pacing must not be negative.');
    }
    _rendererSubscription = renderer.states.listen(_onRendererState);
  }

  final AvatarRenderer _renderer;
  final AvatarSessionTokenClient _tokenClient;
  final AvatarFallbackPlayback _fallbackPlayback;
  final AvatarFallbackStop _fallbackStop;
  final AvatarDelay _delay;
  final Duration chunkDuration;
  final Duration interChunkDelay;

  late final StreamSubscription<AvatarRendererState> _rendererSubscription;
  AvatarControllerState _state;
  int _generation = 0;
  bool _closed = false;
  bool _disposed = false;
  Future<void>? _interruptFuture;
  Future<void>? _closeFuture;
  int? _realtimePcmGeneration;
  Uint8List? _pendingRealtimePcm;
  int _realtimePcmChunkCount = 0;
  int _realtimePcmByteCount = 0;

  AvatarControllerState get state => _state;

  Widget buildSurface({Key? key}) {
    if (_closed) {
      return SizedBox.shrink(key: key);
    }
    return _renderer.buildSurface(key: key);
  }

  /// Mints a capability and prepares the renderer without exposing the token.
  ///
  /// `true` means the visual surface can be built. The renderer may still be
  /// connecting, so callers should use [state] to decide when audio is ready.
  Future<bool> connect({required String practiceSessionId}) async {
    _ensureOpen();
    final generation = ++_generation;
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.preparing,
        renderer: _renderer.state,
      ),
    );
    try {
      final grant = await _tokenClient.createSession(
        practiceSessionId: practiceSessionId,
      );
      if (!_isCurrent(generation)) {
        return false;
      }
      await _renderer.prepare(grant);
      if (!_isCurrent(generation)) {
        await _renderer.close();
        return false;
      }
      _setState(
        AvatarControllerState(
          phase: _renderer.state.canAcceptAudio
              ? AvatarControllerPhase.ready
              : AvatarControllerPhase.preparing,
          renderer: _renderer.state,
        ),
      );
      return true;
    } on AvatarRendererException catch (error) {
      if (_isCurrent(generation)) {
        _setState(
          AvatarControllerState(
            phase: AvatarControllerPhase.failed,
            renderer: _renderer.state,
            failure: error.failure,
          ),
        );
      }
      return false;
    } on AvatarSessionTokenException catch (error) {
      if (_isCurrent(generation)) {
        _setState(
          AvatarControllerState(
            phase: AvatarControllerPhase.failed,
            renderer: _renderer.state,
            failure: _mapTokenFailure(error.failure),
          ),
        );
      }
      return false;
    } catch (_) {
      if (_isCurrent(generation)) {
        _setState(
          AvatarControllerState(
            phase: AvatarControllerPhase.failed,
            renderer: _renderer.state,
            failure: AvatarRendererFailure.unavailable,
          ),
        );
      }
      return false;
    }
  }

  /// Sends the owned WAV to the avatar, or exclusively invokes the existing
  /// audio player when avatar playback is unavailable.
  Future<AvatarSpeechResult> speakWav(Uint8List wavBytes) async {
    _ensureOpen();

    // A fresh utterance always fences and interrupts the previous native send.
    await interrupt();
    if (_closed) {
      return AvatarSpeechResult.interrupted;
    }
    final generation = ++_generation;

    late final AvatarPcmAudio audio;
    try {
      audio = parseAvatarPcmWave(wavBytes);
    } on AvatarAudioException {
      return _playFallback(
        wavBytes,
        generation,
        AvatarRendererFailure.invalidConfiguration,
      );
    }

    if (!_renderer.state.canAcceptAudio) {
      return _playFallback(
        wavBytes,
        generation,
        _renderer.state.failure ?? AvatarRendererFailure.unavailable,
      );
    }

    final chunks = chunkAvatarPcm(
      audio,
      chunkDuration: chunkDuration,
    ).toList(growable: false);
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.speaking,
        renderer: _renderer.state,
      ),
    );

    try {
      for (var index = 0; index < chunks.length; index += 1) {
        if (!_isCurrent(generation)) {
          return AvatarSpeechResult.interrupted;
        }
        final isLast = index == chunks.length - 1;
        await _renderer.sendPcm(chunks[index], end: isLast);
        if (!_isCurrent(generation)) {
          return AvatarSpeechResult.interrupted;
        }
        if (!isLast && interChunkDelay > Duration.zero) {
          await _delay(interChunkDelay);
        }
      }
      if (!_isCurrent(generation)) {
        return AvatarSpeechResult.interrupted;
      }
      _setState(
        AvatarControllerState(
          phase: AvatarControllerPhase.ready,
          renderer: _renderer.state,
        ),
      );
      return AvatarSpeechResult.avatar;
    } catch (_) {
      if (!_isCurrent(generation)) {
        return AvatarSpeechResult.interrupted;
      }
      if (!await _stopRendererForFallback()) {
        if (_isCurrent(generation)) {
          _setState(
            AvatarControllerState(
              phase: AvatarControllerPhase.failed,
              renderer: _renderer.state,
              failure: AvatarRendererFailure.rendering,
            ),
          );
        }
        return AvatarSpeechResult.interrupted;
      }
      return _playFallback(
        wavBytes,
        generation,
        _renderer.state.failure ?? AvatarRendererFailure.unavailable,
      );
    }
  }

  /// Starts one realtime PCM utterance owned by the avatar.
  ///
  /// One chunk is retained so the final non-empty SDK send can be marked with
  /// `end: true`, as required by AvatarKit. A stream failure never starts the
  /// local player halfway through the utterance.
  Future<void> startPcmStream() async {
    _ensureOpen();
    await interrupt();
    if (_closed || !_renderer.state.canAcceptAudio) {
      throw AvatarRendererException(
        _renderer.state.failure ?? AvatarRendererFailure.unavailable,
      );
    }
    final generation = ++_generation;
    _realtimePcmGeneration = generation;
    _realtimePcmChunkCount = 0;
    _realtimePcmByteCount = 0;
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.speaking,
        renderer: _renderer.state,
      ),
    );
    developer.log(
      'realtime_pcm_started sample_rate_hz=24000 channels=1 bits=16',
      name: 'speakup.avatar',
    );
  }

  Future<void> appendPcm(Uint8List pcmBytes) async {
    _ensureOpen();
    final generation = _realtimePcmGeneration;
    if (generation == null ||
        !_isCurrent(generation) ||
        pcmBytes.isEmpty ||
        pcmBytes.length.isOdd) {
      throw const AvatarRendererException(
        AvatarRendererFailure.invalidConfiguration,
      );
    }

    final owned = Uint8List.fromList(pcmBytes);
    final previous = _pendingRealtimePcm;
    _pendingRealtimePcm = owned;
    _realtimePcmChunkCount++;
    _realtimePcmByteCount += owned.length;
    if (previous == null) {
      return;
    }
    try {
      await _renderer.sendPcm(previous, end: false);
    } catch (_) {
      if (_isCurrent(generation)) {
        _failRealtimePcmStream(
          _renderer.state.failure ?? AvatarRendererFailure.rendering,
        );
      }
      throw AvatarRendererException(
        _renderer.state.failure ?? AvatarRendererFailure.rendering,
      );
    } finally {
      previous.fillRange(0, previous.length, 0);
    }
    if (!_isCurrent(generation) || _realtimePcmGeneration != generation) {
      throw const AvatarRendererException(AvatarRendererFailure.unavailable);
    }
  }

  Future<void> finishPcmStream() async {
    _ensureOpen();
    final generation = _realtimePcmGeneration;
    final finalChunk = _pendingRealtimePcm;
    _pendingRealtimePcm = null;
    if (generation == null || finalChunk == null || !_isCurrent(generation)) {
      finalChunk?.fillRange(0, finalChunk.length, 0);
      throw const AvatarRendererException(
        AvatarRendererFailure.invalidConfiguration,
      );
    }
    try {
      await _renderer.sendPcm(finalChunk, end: true);
    } catch (_) {
      if (_isCurrent(generation)) {
        _failRealtimePcmStream(
          _renderer.state.failure ?? AvatarRendererFailure.rendering,
        );
      }
      throw AvatarRendererException(
        _renderer.state.failure ?? AvatarRendererFailure.rendering,
      );
    } finally {
      finalChunk.fillRange(0, finalChunk.length, 0);
    }
    if (!_isCurrent(generation) || _realtimePcmGeneration != generation) {
      return;
    }
    _realtimePcmGeneration = null;
    developer.log(
      'realtime_pcm_completed chunks=$_realtimePcmChunkCount '
      'bytes=$_realtimePcmByteCount',
      name: 'speakup.avatar',
    );
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.ready,
        renderer: _renderer.state,
      ),
    );
  }

  Future<void> stopPcmStream() => interrupt();

  Future<void> interrupt() {
    if (_closed) {
      return Future<void>.value();
    }
    _clearRealtimePcmStream();
    _generation++;
    final existing = _interruptFuture;
    if (existing != null) {
      return existing;
    }
    final operation = _performInterrupt();
    _interruptFuture = operation;
    return operation.whenComplete(() {
      if (identical(_interruptFuture, operation)) {
        _interruptFuture = null;
      }
    });
  }

  Future<void> _performInterrupt() async {
    try {
      await _fallbackStop();
    } catch (_) {
      // The renderer still needs interruption when local stop has failed.
    }
    try {
      await _renderer.interrupt();
    } catch (_) {
      // Interruption is best-effort; the generation fence rejects late sends.
    }
    if (_closed) {
      return;
    }
    _setState(
      AvatarControllerState(
        phase:
            _renderer.state.failure != null ||
                _renderer.state.connection == AvatarRendererConnection.failed
            ? AvatarControllerPhase.failed
            : _renderer.state.canAcceptAudio
            ? AvatarControllerPhase.ready
            : AvatarControllerPhase.preparing,
        renderer: _renderer.state,
        failure: _renderer.state.failure,
      ),
    );
  }

  Future<void> pauseRendering() async {
    if (!_closed) {
      await _renderer.pauseRendering();
    }
  }

  Future<void> resumeRendering() async {
    if (!_closed) {
      await _renderer.resumeRendering();
    }
  }

  /// Used on logout or account switch to fence all old-account work.
  Future<void> clearAccountState() async {
    try {
      await close();
    } finally {
      await _tokenClient.clearAccountState();
    }
  }

  Future<void> close() {
    final existing = _closeFuture;
    if (existing != null) {
      return existing;
    }
    if (_closed) {
      return Future<void>.value();
    }
    _closed = true;
    _clearRealtimePcmStream();
    _generation++;
    final completion = _performClose();
    _closeFuture = completion;
    return completion;
  }

  Future<void> _performClose() async {
    await _rendererSubscription.cancel();
    try {
      final pendingInterrupt = _interruptFuture;
      if (pendingInterrupt != null) {
        try {
          await pendingInterrupt;
        } catch (_) {
          // Renderer close below is the final interruption boundary.
        }
      }
      try {
        await _fallbackStop();
      } catch (_) {
        // Continue native teardown even if local playback is already gone.
      }
      await _renderer.close();
    } finally {
      _setState(
        AvatarControllerState(
          phase: AvatarControllerPhase.closed,
          renderer: const AvatarRendererState(
            connection: AvatarRendererConnection.closed,
          ),
        ),
      );
    }
  }

  Future<bool> _stopRendererForFallback() async {
    try {
      await _renderer.interrupt();
      return true;
    } catch (_) {
      try {
        await _renderer.close();
        return true;
      } catch (_) {
        return false;
      }
    }
  }

  Future<AvatarSpeechResult> _playFallback(
    Uint8List wavBytes,
    int generation,
    AvatarRendererFailure failure,
  ) async {
    if (!_isCurrent(generation)) {
      return AvatarSpeechResult.interrupted;
    }
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.fallback,
        renderer: _renderer.state,
        failure: failure,
      ),
    );
    try {
      await _fallbackPlayback(Uint8List.fromList(wavBytes));
    } catch (_) {
      if (_isCurrent(generation)) {
        _setState(
          AvatarControllerState(
            phase: AvatarControllerPhase.failed,
            renderer: _renderer.state,
            failure: AvatarRendererFailure.unavailable,
          ),
        );
      }
      rethrow;
    }
    if (!_isCurrent(generation)) {
      return AvatarSpeechResult.interrupted;
    }
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.ready,
        renderer: _renderer.state,
        failure: failure,
      ),
    );
    return AvatarSpeechResult.fallback;
  }

  void _failRealtimePcmStream(AvatarRendererFailure failure) {
    _clearRealtimePcmStream();
    developer.log(
      'realtime_pcm_failed failure=${failure.name}',
      name: 'speakup.avatar',
    );
    _setState(
      AvatarControllerState(
        phase: AvatarControllerPhase.failed,
        renderer: _renderer.state,
        failure: failure,
      ),
    );
  }

  void _clearRealtimePcmStream() {
    final pending = _pendingRealtimePcm;
    _pendingRealtimePcm = null;
    pending?.fillRange(0, pending.length, 0);
    _realtimePcmGeneration = null;
    _realtimePcmChunkCount = 0;
    _realtimePcmByteCount = 0;
  }

  void _onRendererState(AvatarRendererState rendererState) {
    if (_closed) {
      return;
    }
    final phase = switch (rendererState.connection) {
      AvatarRendererConnection.connected =>
        _state.phase == AvatarControllerPhase.speaking
            ? AvatarControllerPhase.speaking
            : AvatarControllerPhase.ready,
      AvatarRendererConnection.failed => AvatarControllerPhase.failed,
      AvatarRendererConnection.closed => AvatarControllerPhase.failed,
      _ =>
        _state.phase == AvatarControllerPhase.fallback
            ? AvatarControllerPhase.fallback
            : AvatarControllerPhase.preparing,
    };
    _setState(
      AvatarControllerState(
        phase: phase,
        renderer: rendererState,
        failure: rendererState.connection == AvatarRendererConnection.connected
            ? rendererState.failure
            : rendererState.failure ?? _state.failure,
      ),
    );
  }

  bool _isCurrent(int generation) => !_closed && generation == _generation;

  void _ensureOpen() {
    if (_closed || _disposed) {
      throw StateError('Avatar controller is closed.');
    }
  }

  void _setState(AvatarControllerState next) {
    _state = next;
    if (!_disposed) {
      notifyListeners();
    }
  }

  static AvatarRendererFailure _mapTokenFailure(
    AvatarSessionTokenFailure failure,
  ) {
    return switch (failure) {
      AvatarSessionTokenFailure.authenticationRequired ||
      AvatarSessionTokenFailure.forbidden =>
        AvatarRendererFailure.authentication,
      AvatarSessionTokenFailure.notFound ||
      AvatarSessionTokenFailure.invalidResponse =>
        AvatarRendererFailure.invalidConfiguration,
      AvatarSessionTokenFailure.conflict => AvatarRendererFailure.sessionLimit,
      AvatarSessionTokenFailure.network ||
      AvatarSessionTokenFailure.unavailable => AvatarRendererFailure.network,
      AvatarSessionTokenFailure.cancelled => AvatarRendererFailure.unavailable,
    };
  }

  @override
  void dispose() {
    if (_disposed) {
      return;
    }
    _disposed = true;
    unawaited(close().catchError((_) {}));
    super.dispose();
  }
}
