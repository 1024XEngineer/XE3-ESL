import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:path_provider/path_provider.dart';

abstract interface class PracticeAudioPlayer {
  /// Implementations copy [bytes] before this Future completes.
  Stream<void> get onComplete;

  Future<void> playWav(Uint8List bytes);

  Future<void> stop();

  Future<void> clearAccountState();

  Future<void> dispose();
}

abstract interface class PracticePCMStreamPlayer {
  Future<void> startPCMStream();

  Future<void> appendPCM(Uint8List bytes);

  Future<void> finishPCMStream();

  Future<void> stopPCMStream();

  Future<void> disposePCMStream();
}

final class MethodChannelPracticePCMStreamPlayer
    implements PracticePCMStreamPlayer {
  static const _channel = MethodChannel('speakup/agent_pcm_player');
  bool _started = false;
  bool _disposed = false;

  @override
  Future<void> startPCMStream() async {
    if (_disposed) {
      throw const PracticeAudioPlaybackException();
    }
    await stopPCMStream();
    await _channel.invokeMethod<void>('start', const <String, Object>{
      'sampleRate': 24000,
      'channelCount': 1,
      'bitsPerSample': 16,
      'speed': 1.0,
    });
    if (_disposed) {
      await _channel.invokeMethod<void>('stop');
      throw const PracticeAudioPlaybackException();
    }
    _started = true;
  }

  @override
  Future<void> appendPCM(Uint8List bytes) async {
    if (_disposed || !_started || bytes.isEmpty || bytes.length.isOdd) {
      throw const PracticeAudioPlaybackException();
    }
    await _channel.invokeMethod<void>('append', bytes);
  }

  @override
  Future<void> finishPCMStream() async {
    if (!_started) {
      return;
    }
    await _channel.invokeMethod<void>('finish');
    _started = false;
  }

  @override
  Future<void> stopPCMStream() async {
    if (!_started) {
      return;
    }
    _started = false;
    await _channel.invokeMethod<void>('stop');
  }

  @override
  Future<void> disposePCMStream() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    if (_started) {
      _started = false;
      await _channel.invokeMethod<void>('stop');
    }
  }
}

abstract interface class NativePracticeAudioPlayer {
  Stream<void> get onComplete;

  Future<void> playFile(String path);

  Future<void> stop();

  Future<void> release();

  Future<void> dispose();
}

typedef NativePracticeAudioPlayerFactory = NativePracticeAudioPlayer Function();
typedef PracticePlaybackFileWriter =
    Future<void> Function(File file, Uint8List bytes);
typedef PracticePlaybackFileDeleter = Future<void> Function(String path);

final class AudioplayersNativePracticeAudioPlayer
    implements NativePracticeAudioPlayer {
  AudioplayersNativePracticeAudioPlayer([AudioPlayer? player])
    : _player = player ?? AudioPlayer();

  final AudioPlayer _player;

  @override
  Stream<void> get onComplete => _player.onPlayerComplete;

  @override
  Future<void> playFile(String path) async {
    await _player.setReleaseMode(ReleaseMode.release);
    await _player.play(
      DeviceFileSource(path, mimeType: 'audio/wav'),
      mode: PlayerMode.mediaPlayer,
    );
  }

  @override
  Future<void> stop() => _player.stop();

  @override
  Future<void> release() => _player.release();

  @override
  Future<void> dispose() => _player.dispose();
}

/// Foreground-only WAV player with a private, fully cleaned temporary folder.
///
/// `BytesSource` is intentionally avoided because audioplayers writes it to
/// an unmanaged temporary file on iOS. The adapter owns the file lifecycle and
/// never receives a signed URL or App Session credential.
final class AudioplayersPracticeAudioPlayer extends WidgetsBindingObserver
    implements PracticeAudioPlayer {
  AudioplayersPracticeAudioPlayer({
    NativePracticeAudioPlayerFactory? nativePlayerFactory,
    Future<Directory> Function()? temporaryDirectory,
    PracticePlaybackFileWriter? fileWriter,
    PracticePlaybackFileDeleter? fileDeleter,
    Random? random,
  }) : _nativePlayerFactory =
           nativePlayerFactory ?? AudioplayersNativePracticeAudioPlayer.new,
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory,
       _fileWriter = fileWriter ?? _writePrivateWave,
       _fileDeleter = fileDeleter ?? _deletePrivateWave,
       _random = random ?? Random.secure() {
    AudioLogger.logLevel = AudioLogLevel.none;
    final lifecycleState = WidgetsBinding.instance.lifecycleState;
    _isForeground =
        lifecycleState == null || lifecycleState == AppLifecycleState.resumed;
    WidgetsBinding.instance.addObserver(this);
  }

  static const _managedDirectoryName = 'speakup-playback-audio';

  final NativePracticeAudioPlayerFactory _nativePlayerFactory;
  final Future<Directory> Function() _temporaryDirectory;
  final PracticePlaybackFileWriter _fileWriter;
  final PracticePlaybackFileDeleter _fileDeleter;
  final Random _random;
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  final Set<Future<void>> _inFlightPlaybacks = <Future<void>>{};

  NativePracticeAudioPlayer? _activePlayer;
  StreamSubscription<void>? _activeCompletion;
  String? _activePath;
  Future<Directory>? _managedDirectoryFuture;
  Future<void>? _lifecycleCleanupFuture;
  bool _lifecycleInterruptionNotified = false;
  int _generation = 0;
  int _interruptedThroughGeneration = 0;
  bool _disposed = false;
  bool _isForeground = true;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playWav(Uint8List bytes) {
    if (_disposed || !_isForeground) {
      return Future<void>.error(
        const PracticeAudioPlaybackInterruptedException(),
      );
    }
    if (!_isWave(bytes)) {
      return Future<void>.error(const PracticeAudioPlaybackException());
    }
    final ownedBytes = Uint8List.fromList(bytes);
    final generation = ++_generation;
    final stalePlaybacks = List<Future<void>>.of(_inFlightPlaybacks);
    final lifecycleCleanup = _lifecycleCleanupFuture;
    late final Future<void> operation;
    operation =
        _play(
              generation: generation,
              bytes: ownedBytes,
              stalePlaybacks: stalePlaybacks,
              lifecycleCleanup: lifecycleCleanup,
            )
            .then<void>((_) {
              if (generation <= _interruptedThroughGeneration) {
                throw const PracticeAudioPlaybackInterruptedException();
              }
            })
            .whenComplete(() {
              ownedBytes.fillRange(0, ownedBytes.length, 0);
              _inFlightPlaybacks.remove(operation);
            });
    _inFlightPlaybacks.add(operation);
    return operation;
  }

  Future<void> _play({
    required int generation,
    required Uint8List bytes,
    required List<Future<void>> stalePlaybacks,
    required Future<void>? lifecycleCleanup,
  }) async {
    String? path;
    NativePracticeAudioPlayer? player;
    try {
      await lifecycleCleanup;
      if (!_isCurrent(generation)) {
        return;
      }
      await _stopActive();
      await _waitWithoutFailure(stalePlaybacks);
      await _stopActive();
      if (!_isCurrent(generation)) {
        return;
      }

      final directory = await _managedDirectory();
      if (!_isCurrent(generation)) {
        return;
      }
      path = '${directory.path}/${_newFileName()}';
      await _fileWriter(File(path), bytes);
      if (!_isCurrent(generation)) {
        await _deletePath(path);
        return;
      }

      player = _nativePlayerFactory();
      if (!_isCurrent(generation)) {
        await _cleanupDetached(player, path);
        return;
      }
      _activePlayer = player;
      _activePath = path;
      _activeCompletion = player.onComplete.listen((_) {
        unawaited(_complete(player!, path!, generation));
      });
      await player.playFile(path);
      if (!_isCurrent(generation)) {
        if (identical(_activePlayer, player)) {
          await _cleanup(player: player, path: path, stopFirst: true);
        } else {
          await _deletePath(path);
        }
      }
    } catch (_) {
      if (player != null && path != null) {
        if (identical(_activePlayer, player)) {
          await _cleanup(player: player, path: path, stopFirst: true);
        } else {
          await _cleanupDetached(player, path);
        }
      } else if (path != null) {
        await _deletePath(path);
      }
      if (_isCurrent(generation)) {
        throw const PracticeAudioPlaybackException();
      }
    }
  }

  @override
  Future<void> stop() {
    _generation++;
    return _fenceAndCleanup(cleanDirectory: false);
  }

  @override
  Future<void> clearAccountState() {
    _generation++;
    return _fenceAndCleanup(cleanDirectory: true, strictCleanup: true);
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _generation++;
    WidgetsBinding.instance.removeObserver(this);
    await _fenceAndCleanup(cleanDirectory: true);
    await _completions.close();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _isForeground = true;
      _lifecycleInterruptionNotified = false;
      return;
    }
    _isForeground = false;
    _interruptedThroughGeneration = _generation;
    final playbacksToFence = List<Future<void>>.of(_inFlightPlaybacks);
    final hadActivity = _activePlayer != null || playbacksToFence.isNotEmpty;
    _generation++;
    if (hadActivity && !_lifecycleInterruptionNotified && !_disposed) {
      _lifecycleInterruptionNotified = true;
      _completions.add(null);
    }
    final previousCleanup = _lifecycleCleanupFuture;
    late final Future<void> cleanup;
    cleanup =
        Future<void>.sync(() async {
          await previousCleanup;
          await _fenceAndCleanup(
            cleanDirectory: true,
            playbacksToFence: playbacksToFence,
          );
        }).whenComplete(() {
          if (identical(_lifecycleCleanupFuture, cleanup)) {
            _lifecycleCleanupFuture = null;
          }
        });
    _lifecycleCleanupFuture = cleanup;
    unawaited(cleanup);
  }

  Future<void> _fenceAndCleanup({
    required bool cleanDirectory,
    bool strictCleanup = false,
    List<Future<void>>? playbacksToFence,
  }) async {
    final playbacks =
        playbacksToFence ?? List<Future<void>>.of(_inFlightPlaybacks);
    await _stopActive();
    await _waitWithoutFailure(playbacks);
    await _stopActive();
    if (cleanDirectory) {
      if (strictCleanup) {
        await _cleanManagedDirectory(strict: true);
      } else {
        try {
          await _cleanManagedDirectory();
        } catch (_) {
          // Exact paths are already deleted; crash cleanup retries next launch.
        }
      }
    }
  }

  Future<void> _complete(
    NativePracticeAudioPlayer player,
    String path,
    int generation,
  ) async {
    if (!identical(_activePlayer, player) || _activePath != path) {
      return;
    }
    await _cleanup(player: player, path: path, stopFirst: false);
    if (!_disposed && generation == _generation) {
      _completions.add(null);
    }
  }

  Future<void> _stopActive() async {
    final player = _activePlayer;
    final path = _activePath;
    if (player != null && path != null) {
      await _cleanup(player: player, path: path, stopFirst: true);
    }
  }

  Future<void> _cleanup({
    required NativePracticeAudioPlayer player,
    required String path,
    required bool stopFirst,
  }) async {
    if (identical(_activePlayer, player)) {
      _activePlayer = null;
      _activePath = null;
      final completion = _activeCompletion;
      _activeCompletion = null;
      // Completion callbacks are generation fenced. Awaiting cancellation from
      // a synchronous callback can deadlock the callback that owns cleanup.
      unawaited(completion?.cancel());
    }
    await _cleanupDetached(player, path, stopFirst: stopFirst);
  }

  Future<void> _cleanupDetached(
    NativePracticeAudioPlayer player,
    String path, {
    bool stopFirst = true,
  }) async {
    try {
      if (stopFirst) {
        await player.stop();
      }
    } catch (_) {
      // Cleanup remains authoritative even if the native player has failed.
    }
    try {
      await player.release();
    } catch (_) {
      // Keep deleting owned private artifacts.
    }
    try {
      await player.dispose();
    } catch (_) {
      // Keep deleting owned private artifacts.
    }
    await _deletePath(path);
  }

  Future<Directory> _managedDirectory() {
    final existing = _managedDirectoryFuture;
    if (existing != null) {
      return existing;
    }
    late final Future<Directory> future;
    future = _prepareManagedDirectory().catchError((Object error) {
      if (identical(_managedDirectoryFuture, future)) {
        _managedDirectoryFuture = null;
      }
      throw error;
    });
    _managedDirectoryFuture = future;
    return future;
  }

  Future<Directory> _prepareManagedDirectory() async {
    final base = await _temporaryDirectory();
    final directory = Directory('${base.path}/$_managedDirectoryName');
    await _cleanDirectory(directory, strict: true);
    await directory.create(recursive: true);
    if (await FileSystemEntity.type(directory.path, followLinks: false) !=
        FileSystemEntityType.directory) {
      throw const PracticeAudioPlaybackException();
    }
    return directory;
  }

  Future<void> _cleanManagedDirectory({bool strict = false}) async {
    final directory = await _managedDirectory();
    await _cleanDirectory(directory, strict: strict);
  }

  Future<void> _cleanDirectory(
    Directory directory, {
    required bool strict,
  }) async {
    final type = await FileSystemEntity.type(
      directory.path,
      followLinks: false,
    );
    if (type == FileSystemEntityType.notFound) {
      return;
    }
    if (type != FileSystemEntityType.directory) {
      throw const PracticeAudioPlaybackException();
    }
    await for (final entity in directory.list(followLinks: false)) {
      final name = entity.uri.pathSegments.last;
      if (entity is File && name.startsWith('clip-') && name.endsWith('.wav')) {
        await _deletePath(entity.path, strict: strict);
      }
    }
    if (strict) {
      await for (final entity in directory.list(followLinks: false)) {
        final name = entity.uri.pathSegments.last;
        if (entity is File &&
            name.startsWith('clip-') &&
            name.endsWith('.wav')) {
          throw const PracticeAudioPlaybackException();
        }
      }
    }
  }

  Future<void> _deletePath(String path, {bool strict = false}) async {
    try {
      final type = await FileSystemEntity.type(path, followLinks: false);
      if (type == FileSystemEntityType.file) {
        await _fileDeleter(path);
      } else if (type != FileSystemEntityType.notFound && strict) {
        throw const PracticeAudioPlaybackException();
      }
      if (strict &&
          await FileSystemEntity.type(path, followLinks: false) !=
              FileSystemEntityType.notFound) {
        throw const PracticeAudioPlaybackException();
      }
    } catch (error) {
      if (strict) {
        throw const PracticeAudioPlaybackException();
      }
      // Repeated cleanup is deliberately idempotent.
    }
  }

  String _newFileName() {
    final id = StringBuffer();
    for (var index = 0; index < 16; index++) {
      id.write(_random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return 'clip-$id.wav';
  }

  bool _isCurrent(int generation) {
    return !_disposed && _isForeground && generation == _generation;
  }
}

final class PracticeAudioPlaybackException implements Exception {
  const PracticeAudioPlaybackException();

  @override
  String toString() => 'Practice audio playback is unavailable.';
}

final class PracticeAudioPlaybackInterruptedException implements Exception {
  const PracticeAudioPlaybackInterruptedException();

  @override
  String toString() => 'Practice audio playback was interrupted.';
}

Future<void> _writePrivateWave(File file, Uint8List bytes) async {
  await file.create(exclusive: true);
  if (await FileSystemEntity.type(file.path, followLinks: false) !=
      FileSystemEntityType.file) {
    throw const FileSystemException('Unsafe playback file.');
  }
  await file.writeAsBytes(bytes, flush: true);
}

Future<void> _deletePrivateWave(String path) => File(path).delete();

Future<void> _waitWithoutFailure(Iterable<Future<void>> operations) async {
  await Future.wait([
    for (final operation in operations)
      operation.catchError((Object _) {
        // The replacing/cleanup operation owns the final safety outcome.
      }),
  ]);
}

bool _isWave(Uint8List bytes) {
  return bytes.length >= 44 &&
      bytes[0] == 0x52 &&
      bytes[1] == 0x49 &&
      bytes[2] == 0x46 &&
      bytes[3] == 0x46 &&
      bytes[8] == 0x57 &&
      bytes[9] == 0x41 &&
      bytes[10] == 0x56 &&
      bytes[11] == 0x45;
}
