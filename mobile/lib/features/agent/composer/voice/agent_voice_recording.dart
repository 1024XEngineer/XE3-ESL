import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'package:speakup/platform/audio/pcm16_stream_capture.dart';

import 'agent_voice_models.dart';

abstract interface class AgentVoiceRecorder {
  Future<void> start();

  Future<AgentVoiceLocalRecording> stop();

  Future<void> discardCurrent();

  Future<void> discard(AgentVoiceLocalRecording recording);

  Future<void> clearAccountState();
}

abstract interface class AgentVoiceStreamingRecorder {
  Future<Stream<Uint8List>> startAudioStream();

  Future<AgentVoiceLocalRecording> stopAudioStream();
}

/// Streams ephemeral voice input without materializing a local WAV file.
abstract interface class AgentVoiceEphemeralStreamingRecorder {
  Future<Stream<Uint8List>> startAudioStream();

  Future<void> stopAudioStreamAndDiscard();
}

final class AgentVoiceRecordingException implements Exception {
  const AgentVoiceRecordingException(this.kind);

  final AgentVoiceRecordingFailureKind kind;
}

enum AgentVoiceRecordingFailureKind {
  permissionDenied,
  alreadyRecording,
  notRecording,
  emptyAudio,
  invalidAudio,
  unavailable,
}

abstract interface class NativeAgentVoiceRecorder {
  Future<bool> hasPermission();

  Future<void> startWav(String path);

  Future<Stream<Uint8List>> startPcm16Stream();

  Future<String?> stop();
}

final class RecordNativeAgentVoiceRecorder implements NativeAgentVoiceRecorder {
  RecordNativeAgentVoiceRecorder([AudioRecorder? recorder])
    : _recorder = recorder ?? AudioRecorder();

  final AudioRecorder _recorder;

  @override
  Future<bool> hasPermission() => _recorder.hasPermission();

  @override
  Future<void> startWav(String path) {
    return _recorder.start(
      const RecordConfig(
        encoder: AudioEncoder.wav,
        sampleRate: 16000,
        numChannels: 1,
        bitRate: 256000,
        autoGain: false,
        echoCancel: false,
        noiseSuppress: false,
      ),
      path: path,
    );
  }

  @override
  Future<Stream<Uint8List>> startPcm16Stream() {
    return _recorder.startStream(
      const RecordConfig(
        encoder: AudioEncoder.pcm16bits,
        sampleRate: 16000,
        numChannels: 1,
        autoGain: false,
        echoCancel: false,
        noiseSuppress: false,
      ),
    );
  }

  @override
  Future<String?> stop() => _recorder.stop();
}

typedef AgentVoiceClock = DateTime Function();

final class IosAgentVoiceRecorder
    implements
        AgentVoiceRecorder,
        AgentVoiceStreamingRecorder,
        AgentVoiceEphemeralStreamingRecorder {
  IosAgentVoiceRecorder({
    NativeAgentVoiceRecorder? recorder,
    Future<Directory> Function()? temporaryDirectory,
    AgentVoiceClock? clock,
  }) : _recorder = recorder ?? RecordNativeAgentVoiceRecorder(),
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory,
       _clock = clock ?? DateTime.now;

  static const _managedDirectoryName = 'speakup-agent-voice-recordings';
  static const _minimumWavBytes = 45;
  static const _maximumWavBytes = 7400000;
  static const _maximumPCMBytes = 16000 * 2 * 60;
  static const _nativeCleanupTimeout = Duration(seconds: 2);

  final NativeAgentVoiceRecorder _recorder;
  final Future<Directory> Function() _temporaryDirectory;
  final AgentVoiceClock _clock;
  final Random _random = Random.secure();
  final Set<String> _pendingDeletionPaths = <String>{};
  String? _activePath;
  DateTime? _startedAt;
  Pcm16StreamCapture? _streamCapture;
  int _startGeneration = 0;
  bool _startPending = false;
  Future<String?>? _nativeStop;

  @override
  Future<void> start() async {
    final generation = _beginStart();
    String? path;
    var started = false;
    try {
      await _retryPendingDeletions();
      _requireStartCurrent(generation);
      final hasPermission = await _recorder.hasPermission();
      _requireStartCurrent(generation);
      if (!hasPermission) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.permissionDenied,
        );
      }
      final directory = await _managedDirectory();
      _requireStartCurrent(generation);
      path = '${directory.path}/${_newFileName()}';
      _activePath = path;
      _startedAt = _clock();
      await _recorder.startWav(path);
      _requireStartCurrent(generation);
      if (_activePath != path) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.unavailable,
        );
      }
      started = true;
    } on AgentVoiceRecordingException {
      rethrow;
    } catch (_) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    } finally {
      try {
        if (!started) {
          if (path != null) {
            await _stopNativeBestEffort(forceAfterCurrent: true);
          }
          if (_activePath == path) {
            _activePath = null;
            _startedAt = null;
          }
          if (path != null) {
            await _deletePath(path);
          }
        }
      } finally {
        _startPending = false;
      }
    }
  }

  @override
  Future<Stream<Uint8List>> startAudioStream() async {
    final generation = _beginStart();
    String? path;
    Pcm16StreamCapture? capture;
    var started = false;
    try {
      await _retryPendingDeletions();
      _requireStartCurrent(generation);
      final hasPermission = await _recorder.hasPermission();
      _requireStartCurrent(generation);
      if (!hasPermission) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.permissionDenied,
        );
      }
      final directory = await _managedDirectory();
      _requireStartCurrent(generation);
      path = '${directory.path}/${_newFileName()}';
      _activePath = path;
      _startedAt = _clock();
      final input = await _recorder.startPcm16Stream();
      capture = Pcm16StreamCapture(
        input: input,
        maximumPcmBytes: _maximumPCMBytes,
      );
      _requireStartCurrent(generation);
      if (_activePath != path) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.unavailable,
        );
      }
      _streamCapture = capture;
      started = true;
      return capture.stream;
    } on AgentVoiceRecordingException {
      rethrow;
    } catch (_) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    } finally {
      try {
        if (!started) {
          await Future.wait<void>([
            _cancelCaptureBestEffort(capture),
            if (path != null) _stopNativeBestEffort(forceAfterCurrent: true),
          ]);
          if (identical(_streamCapture, capture)) {
            _clearStreamingState();
          }
          if (_activePath == path) {
            _activePath = null;
            _startedAt = null;
          }
          if (path != null) {
            await _deletePath(path);
          }
        }
      } finally {
        _startPending = false;
      }
    }
  }

  @override
  Future<AgentVoiceLocalRecording> stop() async {
    final expectedPath = _activePath;
    final startedAt = _startedAt;
    if (expectedPath == null || startedAt == null) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.notRecording,
      );
    }
    _activePath = null;
    _startedAt = null;
    String? stoppedPath;
    try {
      stoppedPath = await _stopNative();
    } catch (_) {
      await _deletePath(expectedPath);
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    }
    final path = stoppedPath ?? expectedPath;
    if (path != expectedPath) {
      await _deletePath(expectedPath);
    }
    if (!await _isManagedPath(path)) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.invalidAudio,
      );
    }
    final file = File(path);
    final size = await file.length();
    if (size < _minimumWavBytes || size > _maximumWavBytes) {
      await _deletePath(path);
      throw AgentVoiceRecordingException(
        size < _minimumWavBytes
            ? AgentVoiceRecordingFailureKind.emptyAudio
            : AgentVoiceRecordingFailureKind.invalidAudio,
      );
    }
    final header = await file
        .openRead(0, 12)
        .fold<BytesBuilder>(
          BytesBuilder(copy: false),
          (builder, chunk) => builder..add(chunk),
        );
    if (!_isWave(header.takeBytes())) {
      await _deletePath(path);
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.invalidAudio,
      );
    }
    final elapsed = _clock().difference(startedAt);
    final duration = elapsed <= Duration.zero
        ? const Duration(milliseconds: 1)
        : elapsed > const Duration(seconds: 60)
        ? const Duration(seconds: 60)
        : elapsed;
    return AgentVoiceLocalRecording(
      path: path,
      contentType: 'audio/wav',
      sizeBytes: size,
      duration: duration,
    );
  }

  @override
  Future<AgentVoiceLocalRecording> stopAudioStream() async {
    final path = _activePath;
    final startedAt = _startedAt;
    final capture = _streamCapture;
    if (path == null || startedAt == null || capture == null) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.notRecording,
      );
    }
    _activePath = null;
    _startedAt = null;
    try {
      await _stopNative();
      final wav = await capture.finish();
      await File(path).writeAsBytes(wav, flush: true);
      final elapsed = _boundedRecordingDuration(startedAt);
      return AgentVoiceLocalRecording(
        path: path,
        contentType: 'audio/wav',
        sizeBytes: wav.lengthInBytes,
        duration: elapsed,
      );
    } on Pcm16StreamCaptureException catch (error) {
      await capture.cancel();
      await _deletePath(path);
      throw AgentVoiceRecordingException(_mapStreamFailure(error.kind));
    } on AgentVoiceRecordingException {
      await capture.cancel();
      await _deletePath(path);
      rethrow;
    } catch (_) {
      await capture.cancel();
      await _deletePath(path);
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    } finally {
      _clearStreamingState();
    }
  }

  @override
  Future<void> stopAudioStreamAndDiscard() async {
    final path = _activePath;
    final startedAt = _startedAt;
    final capture = _streamCapture;
    if (path == null || startedAt == null || capture == null) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.notRecording,
      );
    }
    _activePath = null;
    _startedAt = null;
    try {
      await _stopNative();
      await capture.finishAndDiscard();
    } on Pcm16StreamCaptureException catch (error) {
      await capture.cancel();
      throw AgentVoiceRecordingException(_mapStreamFailure(error.kind));
    } on AgentVoiceRecordingException {
      await capture.cancel();
      rethrow;
    } catch (_) {
      await capture.cancel();
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    } finally {
      _clearStreamingState();
      await _deletePath(path);
    }
  }

  @override
  Future<void> discardCurrent() async {
    _startGeneration++;
    final path = _activePath;
    final capture = _streamCapture;
    final shouldStopNative = path != null || capture != null || _startPending;
    _activePath = null;
    _startedAt = null;
    _clearStreamingState();
    await Future.wait<void>([
      _cancelCaptureBestEffort(capture),
      if (shouldStopNative) _stopNativeBestEffort(),
    ]);
    if (path == null) {
      await _retryPendingDeletions();
      return;
    }
    await _deletePath(path);
  }

  int _beginStart() {
    if (_activePath != null ||
        _streamCapture != null ||
        _startPending ||
        _nativeStop != null) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.alreadyRecording,
      );
    }
    _startPending = true;
    return ++_startGeneration;
  }

  void _requireStartCurrent(int generation) {
    if (generation != _startGeneration) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    }
  }

  Future<String?> _stopNative({bool forceAfterCurrent = false}) {
    final existing = _nativeStop;
    if (existing != null && !forceAfterCurrent) {
      return existing;
    }
    // Cleanup callers share one stop. A fenced late start queues one successor
    // because an earlier stop may have run before native capture became active.
    final predecessor = existing == null
        ? Future<void>.value()
        : existing.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    late final Future<String?> stop;
    stop = predecessor.then((_) => _recorder.stop()).whenComplete(() {
      if (identical(_nativeStop, stop)) {
        _nativeStop = null;
      }
    });
    _nativeStop = stop;
    return stop;
  }

  Future<void> _stopNativeBestEffort({bool forceAfterCurrent = false}) async {
    try {
      await _stopNative(
        forceAfterCurrent: forceAfterCurrent,
      ).timeout(_nativeCleanupTimeout);
    } catch (_) {
      // The owned local state is still cleared by the caller.
    }
  }

  Future<void> _cancelCaptureBestEffort(Pcm16StreamCapture? capture) async {
    if (capture == null) {
      return;
    }
    try {
      await capture.cancel().timeout(_nativeCleanupTimeout);
    } catch (_) {
      // The paired native stop still releases the microphone at the fence.
    }
  }

  Duration _boundedRecordingDuration(DateTime startedAt) {
    final elapsed = _clock().difference(startedAt);
    return elapsed <= Duration.zero
        ? const Duration(milliseconds: 1)
        : elapsed > const Duration(seconds: 60)
        ? const Duration(seconds: 60)
        : elapsed;
  }

  void _clearStreamingState() {
    _streamCapture = null;
  }

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) async {
    if (!await _isManagedPath(recording.path)) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.invalidAudio,
      );
    }
    await _deletePath(recording.path);
  }

  @override
  Future<void> clearAccountState() async {
    await discardCurrent();
    await _retryPendingDeletions();
    final directory = await _managedDirectory(create: false);
    if (!await directory.exists()) {
      return;
    }
    await for (final entity in directory.list(followLinks: false)) {
      if (entity is File && entity.path.endsWith('.wav')) {
        await _deletePath(entity.path);
      }
    }
  }

  Future<bool> _isManagedPath(String path) async {
    final directory = await _managedDirectory(create: false);
    final file = File(path);
    if (!await directory.exists() ||
        await FileSystemEntity.type(path, followLinks: false) !=
            FileSystemEntityType.file) {
      return false;
    }
    final canonicalRoot = await directory.resolveSymbolicLinks();
    final canonicalFile = await file.resolveSymbolicLinks();
    return canonicalFile.startsWith(
          '$canonicalRoot${Platform.pathSeparator}',
        ) &&
        canonicalFile.endsWith('.wav');
  }

  Future<Directory> _managedDirectory({bool create = true}) async {
    final root = await _temporaryDirectory();
    final directory = Directory('${root.path}/$_managedDirectoryName');
    if (create && !await directory.exists()) {
      await directory.create(recursive: true);
    }
    return directory;
  }

  String _newFileName() {
    final buffer = StringBuffer('message-');
    for (var index = 0; index < 16; index++) {
      buffer.write(_random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return '$buffer.wav';
  }

  Future<void> _deletePath(String path) async {
    _pendingDeletionPaths.add(path);
    try {
      final file = File(path);
      if (await file.exists()) {
        await file.delete();
      }
      _pendingDeletionPaths.remove(path);
    } on FileSystemException {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    }
  }

  Future<void> _retryPendingDeletions() async {
    for (final path in _pendingDeletionPaths.toList(growable: false)) {
      await _deletePath(path);
    }
  }
}

AgentVoiceRecordingFailureKind _mapStreamFailure(
  Pcm16StreamCaptureFailureKind kind,
) => switch (kind) {
  Pcm16StreamCaptureFailureKind.emptyAudio =>
    AgentVoiceRecordingFailureKind.emptyAudio,
  Pcm16StreamCaptureFailureKind.invalidAudio =>
    AgentVoiceRecordingFailureKind.invalidAudio,
  Pcm16StreamCaptureFailureKind.unavailable ||
  Pcm16StreamCaptureFailureKind.cancelled =>
    AgentVoiceRecordingFailureKind.unavailable,
};

final class FakeAgentVoiceRecorder implements AgentVoiceRecorder {
  FakeAgentVoiceRecorder({
    this.failure,
    this.recordingDuration = const Duration(seconds: 3),
  });

  AgentVoiceRecordingFailureKind? failure;
  final Duration recordingDuration;
  bool _recording = false;
  int _sequence = 0;

  @override
  Future<void> start() async {
    if (failure case final kind?) {
      throw AgentVoiceRecordingException(kind);
    }
    if (_recording) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.alreadyRecording,
      );
    }
    _recording = true;
  }

  @override
  Future<AgentVoiceLocalRecording> stop() async {
    if (failure case final kind?) {
      _recording = false;
      throw AgentVoiceRecordingException(kind);
    }
    if (!_recording) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.notRecording,
      );
    }
    _recording = false;
    return AgentVoiceLocalRecording(
      path: 'fake-agent-voice-${++_sequence}.wav',
      contentType: 'audio/wav',
      sizeBytes: 64,
      duration: recordingDuration,
    );
  }

  @override
  Future<void> discardCurrent() async {
    _recording = false;
  }

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) async {}

  @override
  Future<void> clearAccountState() => discardCurrent();
}

final class FakeAgentVoiceStreamingRecorder
    implements
        AgentVoiceRecorder,
        AgentVoiceStreamingRecorder,
        AgentVoiceEphemeralStreamingRecorder {
  FakeAgentVoiceStreamingRecorder({
    AgentVoiceRecordingFailureKind? failure,
    Duration recordingDuration = const Duration(seconds: 3),
  }) : _recorder = FakeAgentVoiceRecorder(
         failure: failure,
         recordingDuration: recordingDuration,
       );

  final FakeAgentVoiceRecorder _recorder;
  StreamController<Uint8List>? _audioChunks;

  AgentVoiceRecordingFailureKind? get failure => _recorder.failure;

  set failure(AgentVoiceRecordingFailureKind? value) {
    _recorder.failure = value;
  }

  @override
  Future<void> start() => _recorder.start();

  @override
  Future<Stream<Uint8List>> startAudioStream() async {
    await _recorder.start();
    final chunks = StreamController<Uint8List>();
    _audioChunks = chunks;
    scheduleMicrotask(() {
      if (identical(_audioChunks, chunks) && !chunks.isClosed) {
        chunks.add(Uint8List.fromList(const <int>[1, 0, 2, 0]));
      }
    });
    return chunks.stream;
  }

  @override
  Future<AgentVoiceLocalRecording> stop() => _recorder.stop();

  @override
  Future<AgentVoiceLocalRecording> stopAudioStream() async {
    final chunks = _audioChunks;
    _audioChunks = null;
    await chunks?.close();
    return _recorder.stop();
  }

  @override
  Future<void> stopAudioStreamAndDiscard() async {
    final chunks = _audioChunks;
    _audioChunks = null;
    await chunks?.close();
    await _recorder.stop();
  }

  @override
  Future<void> discardCurrent() async {
    final chunks = _audioChunks;
    _audioChunks = null;
    await chunks?.close();
    await _recorder.discardCurrent();
  }

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) =>
      _recorder.discard(recording);

  @override
  Future<void> clearAccountState() => discardCurrent();
}

bool _isWave(List<int> bytes) {
  return bytes.length >= 12 &&
      bytes[0] == 0x52 &&
      bytes[1] == 0x49 &&
      bytes[2] == 0x46 &&
      bytes[3] == 0x46 &&
      bytes[8] == 0x57 &&
      bytes[9] == 0x41 &&
      bytes[10] == 0x56 &&
      bytes[11] == 0x45;
}
