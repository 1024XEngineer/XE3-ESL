import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/widgets.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

import 'agent_voice_models.dart';

abstract interface class AgentVoiceRecorder {
  Future<void> start();

  Future<AgentVoiceLocalRecording> stop();

  Future<void> discardCurrent();

  Future<void> discard(AgentVoiceLocalRecording recording);

  Future<void> clearAccountState();
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
  Future<String?> stop() => _recorder.stop();
}

typedef AgentVoiceClock = DateTime Function();

final class IosAgentVoiceRecorder implements AgentVoiceRecorder {
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

  final NativeAgentVoiceRecorder _recorder;
  final Future<Directory> Function() _temporaryDirectory;
  final AgentVoiceClock _clock;
  final Random _random = Random.secure();
  String? _activePath;
  DateTime? _startedAt;

  @override
  Future<void> start() async {
    if (_activePath != null) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.alreadyRecording,
      );
    }
    if (!await _recorder.hasPermission()) {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.permissionDenied,
      );
    }
    final directory = await _managedDirectory();
    final path = '${directory.path}/${_newFileName()}';
    _activePath = path;
    _startedAt = _clock();
    try {
      await _recorder.startWav(path);
    } catch (_) {
      _activePath = null;
      _startedAt = null;
      await _deletePath(path);
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
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
      stoppedPath = await _recorder.stop();
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
  Future<void> discardCurrent() async {
    final path = _activePath;
    _activePath = null;
    _startedAt = null;
    if (path == null) {
      return;
    }
    try {
      await _recorder.stop();
    } catch (_) {
      // The owned path is still removed below.
    }
    await _deletePath(path);
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
    try {
      final file = File(path);
      if (await file.exists()) {
        await file.delete();
      }
    } on FileSystemException {
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    }
  }
}

abstract interface class AgentVoiceAudioPlayer {
  Stream<Duration> get onPosition;

  Stream<void> get onComplete;

  Future<void> playFile(String path, {required double speed});

  Future<void> playWav(Uint8List bytes, {required double speed});

  Future<void> stop();

  Future<void> clearAccountState();

  Future<void> dispose();
}

final class AgentVoicePlaybackException implements Exception {
  const AgentVoicePlaybackException();
}

final class AudioplayersAgentVoiceAudioPlayer extends WidgetsBindingObserver
    implements AgentVoiceAudioPlayer {
  AudioplayersAgentVoiceAudioPlayer({
    AudioPlayer? player,
    Future<Directory> Function()? temporaryDirectory,
  }) : _player = player ?? AudioPlayer(),
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory {
    AudioLogger.logLevel = AudioLogLevel.none;
    WidgetsBinding.instance.addObserver(this);
  }

  static const _managedDirectoryName = 'speakup-agent-voice-playback';

  final AudioPlayer _player;
  final Future<Directory> Function() _temporaryDirectory;
  final Random _random = Random.secure();
  final StreamController<Duration> _positions =
      StreamController<Duration>.broadcast(sync: true);
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  StreamSubscription<Duration>? _positionSubscription;
  StreamSubscription<void>? _completionSubscription;
  String? _ownedPlaybackPath;
  int _generation = 0;
  bool _disposed = false;

  @override
  Stream<Duration> get onPosition => _positions.stream;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playFile(String path, {required double speed}) {
    return _play(DeviceFileSource(path, mimeType: 'audio/wav'), speed: speed);
  }

  @override
  Future<void> playWav(Uint8List bytes, {required double speed}) async {
    if (!_isWave(bytes)) {
      throw const AgentVoicePlaybackException();
    }
    final generation = ++_generation;
    await _stopActive();
    if (!_isCurrent(generation)) {
      return;
    }
    final directory = await _managedDirectory();
    final path = '${directory.path}/${_newFileName()}';
    final copy = Uint8List.fromList(bytes);
    try {
      await File(path).writeAsBytes(copy, flush: true);
    } finally {
      copy.fillRange(0, copy.length, 0);
    }
    if (!_isCurrent(generation)) {
      await _deletePath(path);
      return;
    }
    _ownedPlaybackPath = path;
    await _play(
      DeviceFileSource(path, mimeType: 'audio/wav'),
      speed: speed,
      generation: generation,
    );
  }

  Future<void> _play(
    Source source, {
    required double speed,
    int? generation,
  }) async {
    if (_disposed || speed < 0.5 || speed > 2) {
      throw const AgentVoicePlaybackException();
    }
    final currentGeneration = generation ?? ++_generation;
    if (generation == null) {
      await _stopActive();
    }
    if (!_isCurrent(currentGeneration)) {
      return;
    }
    await _positionSubscription?.cancel();
    await _completionSubscription?.cancel();
    _positionSubscription = _player.onPositionChanged.listen(_positions.add);
    _completionSubscription = _player.onPlayerComplete.listen((_) {
      unawaited(_complete(currentGeneration));
    });
    try {
      await _player.setReleaseMode(ReleaseMode.stop);
      await _player.setPlaybackRate(speed);
      await _player.play(source, mode: PlayerMode.mediaPlayer);
    } catch (_) {
      await _stopActive();
      if (_isCurrent(currentGeneration)) {
        throw const AgentVoicePlaybackException();
      }
    }
  }

  @override
  Future<void> stop() async {
    _generation++;
    await _stopActive();
  }

  @override
  Future<void> clearAccountState() async {
    _generation++;
    await _stopActive();
    final directory = await _managedDirectory(create: false);
    if (await directory.exists()) {
      await for (final entity in directory.list(followLinks: false)) {
        if (entity is File && entity.path.endsWith('.wav')) {
          await _deletePath(entity.path);
        }
      }
    }
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _generation++;
    await _stopActive();
    await _player.dispose();
    await _positions.close();
    await _completions.close();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      unawaited(stop());
    }
  }

  Future<void> _complete(int generation) async {
    if (!_isCurrent(generation)) {
      return;
    }
    await _stopActive();
    if (_isCurrent(generation)) {
      _completions.add(null);
    }
  }

  Future<void> _stopActive() async {
    try {
      await _player.stop();
    } catch (_) {
      // Cleanup of the owned file still proceeds.
    }
    await _positionSubscription?.cancel();
    await _completionSubscription?.cancel();
    _positionSubscription = null;
    _completionSubscription = null;
    final path = _ownedPlaybackPath;
    _ownedPlaybackPath = null;
    if (path != null) {
      await _deletePath(path);
    }
  }

  bool _isCurrent(int generation) => !_disposed && generation == _generation;

  Future<Directory> _managedDirectory({bool create = true}) async {
    final root = await _temporaryDirectory();
    final directory = Directory('${root.path}/$_managedDirectoryName');
    if (create && !await directory.exists()) {
      await directory.create(recursive: true);
    }
    return directory;
  }

  String _newFileName() {
    final buffer = StringBuffer('playback-');
    for (var index = 0; index < 16; index++) {
      buffer.write(_random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return '$buffer.wav';
  }

  Future<void> _deletePath(String path) async {
    try {
      final file = File(path);
      if (await file.exists()) {
        await file.delete();
      }
    } on FileSystemException {
      throw const AgentVoicePlaybackException();
    }
  }
}

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

final class FakeAgentVoiceAudioPlayer implements AgentVoiceAudioPlayer {
  final StreamController<Duration> _positions =
      StreamController<Duration>.broadcast(sync: true);
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  bool playing = false;
  double? speed;

  @override
  Stream<Duration> get onPosition => _positions.stream;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playFile(String path, {required double speed}) async {
    playing = true;
    this.speed = speed;
  }

  @override
  Future<void> playWav(Uint8List bytes, {required double speed}) async {
    playing = true;
    this.speed = speed;
  }

  void emitPosition(Duration position) => _positions.add(position);

  void complete() {
    playing = false;
    _completions.add(null);
  }

  @override
  Future<void> stop() async {
    playing = false;
  }

  @override
  Future<void> clearAccountState() => stop();

  @override
  Future<void> dispose() async {
    await _positions.close();
    await _completions.close();
  }
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
