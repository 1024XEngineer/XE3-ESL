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

final class AudioplayersEphemeralWavAudioPlayer extends WidgetsBindingObserver
    implements EphemeralWavAudioPlayer {
  AudioplayersEphemeralWavAudioPlayer({
    AudioPlayer? player,
    Future<Directory> Function()? temporaryDirectory,
  }) : _player = player ?? AudioPlayer(),
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory {
    AudioLogger.logLevel = AudioLogLevel.none;
    WidgetsBinding.instance.addObserver(this);
    _completionSubscription = _player.onPlayerComplete.listen((_) {
      unawaited(_complete());
    });
  }

  static const _managedDirectoryName = 'speakup-voice-previews';

  final AudioPlayer _player;
  final Future<Directory> Function() _temporaryDirectory;
  final Random _random = Random.secure();
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  late final StreamSubscription<void> _completionSubscription;
  String? _ownedPath;
  int _generation = 0;
  bool _disposed = false;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> play(Uint8List bytes) async {
    if (_disposed || !_isWave(bytes)) {
      throw const EphemeralWavPlaybackException();
    }
    final generation = ++_generation;
    await _stopActive();
    if (!_isCurrent(generation)) return;

    final root = await _temporaryDirectory();
    final directory = Directory('${root.path}/$_managedDirectoryName');
    await directory.create(recursive: true);
    final path = '${directory.path}/${_random.nextInt(1 << 32)}.wav';
    final copy = Uint8List.fromList(bytes);
    try {
      await File(path).writeAsBytes(copy, flush: true);
    } finally {
      copy.fillRange(0, copy.length, 0);
    }
    if (!_isCurrent(generation)) {
      await _delete(path);
      return;
    }
    _ownedPath = path;
    try {
      await _player.setReleaseMode(ReleaseMode.release);
      await _player.play(
        DeviceFileSource(path, mimeType: 'audio/wav'),
        mode: PlayerMode.mediaPlayer,
      );
    } catch (_) {
      await _stopActive();
      if (_isCurrent(generation)) {
        throw const EphemeralWavPlaybackException();
      }
    }
  }

  @override
  Future<void> stop() async {
    _generation++;
    await _stopActive();
  }

  @override
  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _generation++;
    await _stopActive();
    await _completionSubscription.cancel();
    await _player.dispose();
    await _completions.close();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      unawaited(stop());
    }
  }

  Future<void> _complete() async {
    if (_disposed) return;
    await _deleteOwnedPath();
    if (!_completions.isClosed) _completions.add(null);
  }

  Future<void> _stopActive() async {
    try {
      await _player.stop();
    } catch (_) {
      // Cleanup still owns the temporary file when the native player fails.
    }
    await _deleteOwnedPath();
  }

  Future<void> _deleteOwnedPath() async {
    final path = _ownedPath;
    _ownedPath = null;
    if (path != null) await _delete(path);
  }

  Future<void> _delete(String path) async {
    try {
      final file = File(path);
      if (await file.exists()) await file.delete();
    } on FileSystemException {
      // Temporary cleanup is best-effort and never blocks navigation.
    }
  }

  bool _isCurrent(int generation) => !_disposed && generation == _generation;

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
