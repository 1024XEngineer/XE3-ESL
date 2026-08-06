import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:path_provider/path_provider.dart';

/// Device audio output shared by Agent draft playback and committed Messages.
abstract interface class AgentAudioPlayer {
  Stream<Duration> get onPosition;

  Stream<void> get onComplete;

  Future<void> playFile(String path, {required double speed});

  Future<void> playWav(Uint8List bytes, {required double speed});

  Future<void> stop();

  Future<void> clearAccountState();

  Future<void> dispose();
}

abstract interface class AgentPCMStreamPlayer {
  Future<void> startPCMStream({required double speed});

  Future<void> appendPCM(Uint8List bytes);

  Future<void> finishPCMStream();
}

final class AgentAudioPlaybackException implements Exception {
  const AgentAudioPlaybackException();
}

final class AudioplayersAgentAudioPlayer extends WidgetsBindingObserver
    implements AgentAudioPlayer, AgentPCMStreamPlayer {
  AudioplayersAgentAudioPlayer({
    AudioPlayer? player,
    Future<Directory> Function()? temporaryDirectory,
  }) : _player = player ?? AudioPlayer(),
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory {
    AudioLogger.logLevel = AudioLogLevel.none;
    WidgetsBinding.instance.addObserver(this);
  }

  static const _managedDirectoryName = 'speakup-agent-voice-playback';
  static const _pcmChannel = MethodChannel('speakup/agent_pcm_player');

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
  bool _pcmStreaming = false;

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
      throw const AgentAudioPlaybackException();
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

  @override
  Future<void> startPCMStream({required double speed}) async {
    if (_disposed || speed < 0.5 || speed > 2) {
      throw const AgentAudioPlaybackException();
    }
    _generation++;
    await _stopActive();
    try {
      await _pcmChannel.invokeMethod<void>('start', <String, Object>{
        'sampleRate': 24000,
        'channelCount': 1,
        'bitsPerSample': 16,
        'speed': speed,
      });
      _pcmStreaming = true;
    } on PlatformException {
      await _stopActive();
      throw const AgentAudioPlaybackException();
    }
  }

  @override
  Future<void> appendPCM(Uint8List bytes) async {
    if (!_pcmStreaming || bytes.isEmpty || bytes.length.isOdd) {
      throw const AgentAudioPlaybackException();
    }
    try {
      await _pcmChannel.invokeMethod<void>('append', bytes);
    } on PlatformException {
      await _stopActive();
      throw const AgentAudioPlaybackException();
    }
  }

  @override
  Future<void> finishPCMStream() async {
    if (!_pcmStreaming) {
      throw const AgentAudioPlaybackException();
    }
    try {
      await _pcmChannel.invokeMethod<void>('finish');
      _pcmStreaming = false;
      _completions.add(null);
    } on PlatformException {
      await _stopActive();
      throw const AgentAudioPlaybackException();
    }
  }

  Future<void> _play(
    Source source, {
    required double speed,
    int? generation,
  }) async {
    if (_disposed || speed < 0.5 || speed > 2) {
      throw const AgentAudioPlaybackException();
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
        throw const AgentAudioPlaybackException();
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
    if (_pcmStreaming) {
      _pcmStreaming = false;
      try {
        await _pcmChannel.invokeMethod<void>('stop');
      } on PlatformException {
        // Native playback is already treated as stopped locally.
      }
    }
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
      throw const AgentAudioPlaybackException();
    }
  }
}

final class FakeAgentAudioPlayer
    implements AgentAudioPlayer, AgentPCMStreamPlayer {
  final StreamController<Duration> _positions =
      StreamController<Duration>.broadcast(sync: true);
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  bool playing = false;
  double? speed;
  final List<Uint8List> pcmChunks = <Uint8List>[];

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

  @override
  Future<void> startPCMStream({required double speed}) async {
    playing = true;
    this.speed = speed;
  }

  @override
  Future<void> appendPCM(Uint8List bytes) async {
    pcmChunks.add(Uint8List.fromList(bytes));
  }

  @override
  Future<void> finishPCMStream() async {}

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
