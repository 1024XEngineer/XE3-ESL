import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/widgets.dart';
import 'package:path_provider/path_provider.dart';

abstract interface class EphemeralWavAudioPlayer {
  Stream<void> get onComplete;

  Future<void> play(Uint8List bytes);

  Future<void> stop();

  Future<void> dispose();
}

final class EphemeralWavPlaybackException implements Exception {
  const EphemeralWavPlaybackException();
}

final class EphemeralWavPlaybackInterruptedException implements Exception {
  const EphemeralWavPlaybackInterruptedException();
}

abstract interface class NativeEphemeralWavAudioPlayer {
  Stream<void> get onComplete;

  /// Starts one file. Once [stop] or [dispose] returns, this call must not
  /// start playback even if its Future settles later.
  Future<void> playFile(String path);

  Future<void> stop();

  Future<void> dispose();
}

typedef NativeEphemeralWavAudioPlayerFactory =
    NativeEphemeralWavAudioPlayer Function();

final class AudioplayersNativeEphemeralWavAudioPlayer
    implements NativeEphemeralWavAudioPlayer {
  AudioplayersNativeEphemeralWavAudioPlayer([AudioPlayer? player])
    : _player = player ?? AudioPlayer();

  final AudioPlayer _player;
  bool _stopped = false;
  bool _disposed = false;

  @override
  Stream<void> get onComplete => _player.onPlayerComplete;

  @override
  Future<void> playFile(String path) async {
    if (_stopped || _disposed) return;
    await _player.setReleaseMode(ReleaseMode.release);
    if (_stopped || _disposed) return;
    await _player.play(
      DeviceFileSource(path, mimeType: 'audio/wav'),
      mode: PlayerMode.mediaPlayer,
    );
  }

  @override
  Future<void> stop() async {
    if (_stopped || _disposed) return;
    _stopped = true;
    await _player.stop();
  }

  @override
  Future<void> dispose() async {
    if (_disposed) return;
    _stopped = true;
    _disposed = true;
    await _player.dispose();
  }
}

final class AudioplayersEphemeralWavAudioPlayer extends WidgetsBindingObserver
    implements EphemeralWavAudioPlayer {
  AudioplayersEphemeralWavAudioPlayer({
    NativeEphemeralWavAudioPlayerFactory? nativePlayerFactory,
    Future<Directory> Function()? temporaryDirectory,
  }) : _nativePlayerFactory =
           nativePlayerFactory ?? AudioplayersNativeEphemeralWavAudioPlayer.new,
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory {
    AudioLogger.logLevel = AudioLogLevel.none;
    final lifecycleState = WidgetsBinding.instance.lifecycleState;
    _isForeground =
        lifecycleState == null || lifecycleState == AppLifecycleState.resumed;
    WidgetsBinding.instance.addObserver(this);
  }

  static const _managedDirectoryName = 'speakup-voice-previews';

  final NativeEphemeralWavAudioPlayerFactory _nativePlayerFactory;
  final Future<Directory> Function() _temporaryDirectory;
  final Random _random = Random.secure();
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  final Set<Future<void>> _inFlight = <Future<void>>{};
  final Set<_PlaybackAttempt> _attempts = <_PlaybackAttempt>{};
  _ActivePlayback? _active;
  Future<void>? _cleanupFuture;
  Future<void>? _disposeFuture;
  int _generation = 0;
  bool _disposed = false;
  bool _isForeground = true;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> play(Uint8List bytes) {
    if (_disposed || !_isWave(bytes)) {
      return Future<void>.error(const EphemeralWavPlaybackException());
    }
    if (!_isForeground) {
      return Future<void>.error(
        const EphemeralWavPlaybackInterruptedException(),
      );
    }
    final generation = ++_generation;
    final pendingCleanup = _cleanupFuture;
    final staleOperations = List<Future<void>>.of(_inFlight);
    _interruptAttempts();
    final attempt = _PlaybackAttempt(generation);
    late final Future<void> operation;
    operation = _play(bytes, attempt, pendingCleanup, staleOperations)
        .whenComplete(() {
          _inFlight.remove(operation);
          _attempts.remove(attempt);
        });
    _attempts.add(attempt);
    _inFlight.add(operation);
    return operation;
  }

  Future<void> _play(
    Uint8List bytes,
    _PlaybackAttempt attempt,
    Future<void>? pendingCleanup,
    List<Future<void>> staleOperations,
  ) async {
    String? path;
    _ActivePlayback? playback;
    try {
      if (pendingCleanup != null) {
        await pendingCleanup;
      }
      _throwIfStale(attempt);
      await _stopActive(emitCompletion: true);
      _throwIfStale(attempt);
      await _waitWithoutFailure(staleOperations);
      _throwIfStale(attempt);
      await _stopActive(emitCompletion: true);
      _throwIfStale(attempt);

      final root = await _awaitOrInterrupt(_temporaryDirectory(), attempt);
      _throwIfStale(attempt);
      final directory = Directory('${root.path}/$_managedDirectoryName');
      await _awaitOrInterrupt(directory.create(recursive: true), attempt);
      _throwIfStale(attempt);
      path = '${directory.path}/${_random.nextInt(1 << 32)}.wav';
      final copy = Uint8List.fromList(bytes);
      final write = File(path).writeAsBytes(copy, flush: true).whenComplete(() {
        copy.fillRange(0, copy.length, 0);
        if (attempt.isInterrupted) unawaited(_delete(path!));
      });
      await _awaitOrInterrupt(write, attempt);
      _throwIfStale(attempt);

      final player = _nativePlayerFactory();
      playback = _ActivePlayback(
        generation: attempt.generation,
        player: player,
        path: path,
      );
      _active = playback;
      void handleTerminalEvent() {
        final current = playback!;
        if (current.reportedStarted) {
          _track(_complete(current));
        } else {
          attempt.interrupt();
        }
      }

      playback.completion = player.onComplete.listen(
        (_) => handleTerminalEvent(),
        onError: (Object _, StackTrace _) => handleTerminalEvent(),
      );
      await _awaitOrInterrupt(player.playFile(path), attempt);
      _throwIfStale(attempt);
      if (!identical(_active, playback)) {
        throw const _PlaybackInterrupted();
      }
      playback.reportedStarted = true;
    } on _PlaybackInterrupted {
      final ownedPlayback = playback != null && identical(_active, playback)
          ? playback
          : null;
      if (ownedPlayback != null) {
        await _cleanup(ownedPlayback, stopFirst: true, emitCompletion: false);
      } else if (playback == null && path != null) {
        await _delete(path);
      }
      throw const EphemeralWavPlaybackInterruptedException();
    } catch (_) {
      final ownedPlayback = playback != null && identical(_active, playback)
          ? playback
          : null;
      if (ownedPlayback != null) {
        await _cleanup(ownedPlayback, stopFirst: true, emitCompletion: false);
      } else if (playback == null && path != null) {
        await _delete(path);
      }
      if (!_isCurrent(attempt.generation)) {
        throw const EphemeralWavPlaybackInterruptedException();
      }
      throw const EphemeralWavPlaybackException();
    }
  }

  void _track(Future<void> operation) {
    _inFlight.add(operation);
    unawaited(
      operation.then<void>(
        (_) => _inFlight.remove(operation),
        onError: (Object _, StackTrace _) {
          _inFlight.remove(operation);
        },
      ),
    );
  }

  @override
  Future<void> stop() {
    _generation++;
    return _scheduleCleanup();
  }

  @override
  Future<void> dispose() {
    final existing = _disposeFuture;
    if (existing != null) return existing;
    _disposed = true;
    _generation++;
    WidgetsBinding.instance.removeObserver(this);
    final cleanup = _scheduleCleanup();
    final disposal = cleanup.then((_) => _completions.close());
    _disposeFuture = disposal;
    return disposal;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _isForeground = true;
      return;
    }
    _isForeground = false;
    _generation++;
    unawaited(_scheduleCleanup());
  }

  Future<void> _scheduleCleanup() {
    final previous = _cleanupFuture;
    _interruptAttempts();
    final operations = List<Future<void>>.of(_inFlight);
    late final Future<void> cleanup;
    cleanup =
        Future<void>.sync(() async {
          await previous;
          await _fenceAndCleanup(operations);
        }).whenComplete(() {
          if (identical(_cleanupFuture, cleanup)) {
            _cleanupFuture = null;
          }
        });
    _cleanupFuture = cleanup;
    return cleanup;
  }

  Future<void> _fenceAndCleanup(List<Future<void>> operations) async {
    await _stopActive(emitCompletion: true);
    await _waitWithoutFailure(operations);
    await _stopActive(emitCompletion: true);
  }

  Future<void> _complete(_ActivePlayback playback) async {
    if (!identical(_active, playback) || !_isCurrent(playback.generation)) {
      return;
    }
    await _cleanup(playback, stopFirst: false, emitCompletion: true);
  }

  Future<void> _stopActive({required bool emitCompletion}) async {
    final playback = _active;
    if (playback != null) {
      await _cleanup(playback, stopFirst: true, emitCompletion: emitCompletion);
    }
  }

  Future<void> _cleanup(
    _ActivePlayback playback, {
    required bool stopFirst,
    required bool emitCompletion,
  }) async {
    if (!identical(_active, playback)) return;
    _active = null;
    try {
      await playback.completion?.cancel();
    } catch (_) {
      // Native event-stream shutdown cannot prevent owned resource cleanup.
    }
    await _cleanupDetached(playback, stopFirst: stopFirst);
    if (emitCompletion && playback.reportedStarted && !_completions.isClosed) {
      _completions.add(null);
    }
  }

  Future<void> _cleanupDetached(
    _ActivePlayback playback, {
    required bool stopFirst,
  }) async {
    if (stopFirst) {
      try {
        await playback.player.stop();
      } catch (_) {
        // Cleanup remains authoritative after a native stop failure.
      }
    }
    try {
      await playback.player.dispose();
    } catch (_) {
      // Keep deleting the private preview after a native dispose failure.
    }
    await _delete(playback.path);
  }

  Future<void> _delete(String path) async {
    try {
      final file = File(path);
      if (await file.exists()) await file.delete();
    } on FileSystemException {
      // Temporary cleanup is best-effort and never blocks navigation.
    }
  }

  bool _isCurrent(int generation) =>
      !_disposed && _isForeground && generation == _generation;

  void _interruptAttempts() {
    for (final attempt in _attempts) {
      attempt.interrupt();
    }
  }

  void _throwIfStale(_PlaybackAttempt attempt) {
    if (attempt.isInterrupted || !_isCurrent(attempt.generation)) {
      throw const _PlaybackInterrupted();
    }
  }

  static bool _isWave(Uint8List bytes) {
    if (bytes.length < 44) return false;
    return bytes[0] == 0x52 &&
        bytes[1] == 0x49 &&
        bytes[2] == 0x46 &&
        bytes[3] == 0x46 &&
        bytes[8] == 0x57 &&
        bytes[9] == 0x41 &&
        bytes[10] == 0x56 &&
        bytes[11] == 0x45;
  }
}

final class _ActivePlayback {
  _ActivePlayback({
    required this.generation,
    required this.player,
    required this.path,
  });

  final int generation;
  final NativeEphemeralWavAudioPlayer player;
  final String path;
  bool reportedStarted = false;
  StreamSubscription<void>? completion;
}

final class _PlaybackAttempt {
  _PlaybackAttempt(this.generation);

  final int generation;
  final Completer<void> interrupted = Completer<void>();

  bool get isInterrupted => interrupted.isCompleted;

  void interrupt() {
    if (!interrupted.isCompleted) interrupted.complete();
  }
}

final class _PlaybackInterrupted implements Exception {
  const _PlaybackInterrupted();
}

Future<T> _awaitOrInterrupt<T>(Future<T> operation, _PlaybackAttempt attempt) {
  final result = Completer<T>();
  operation.then(
    (value) {
      if (!result.isCompleted) result.complete(value);
    },
    onError: (Object error, StackTrace stackTrace) {
      if (!result.isCompleted) result.completeError(error, stackTrace);
    },
  );
  attempt.interrupted.future.then((_) {
    if (!result.isCompleted) {
      result.completeError(const _PlaybackInterrupted());
    }
  });
  return result.future;
}

Future<void> _waitWithoutFailure(Iterable<Future<void>> operations) async {
  await Future.wait([
    for (final operation in operations)
      operation.catchError((Object _) {
        // The replacing or cleanup operation owns the final safety outcome.
      }),
  ]);
}
