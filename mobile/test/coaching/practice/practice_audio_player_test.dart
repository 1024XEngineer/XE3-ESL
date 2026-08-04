import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  });
  tearDown(() {
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  });

  test(
    'managed WAV is deleted on stop and caller bytes are untouched',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-test-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final native = _NativePlayer();
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () => native,
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);
      final input = _wave();

      await player.playWav(input);

      expect(input, _wave());
      expect(native.path, contains('/speakup-playback-audio/clip-'));
      expect(await File(native.path!).exists(), isTrue);

      await player.stop();

      expect(native.stopCount, 1);
      expect(native.releaseCount, 1);
      expect(native.disposeCount, 1);
      expect(await File(native.path!).exists(), isFalse);
    },
  );

  test(
    'completion and replacement cannot leak or clear the new clip',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-complete-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final natives = <_NativePlayer>[];
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () {
          final native = _NativePlayer();
          natives.add(native);
          return native;
        },
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);

      await player.playWav(_wave());
      final oldPath = natives.first.path!;
      await player.playWav(_wave());
      final newPath = natives.last.path!;

      expect(await File(oldPath).exists(), isFalse);
      expect(await File(newPath).exists(), isTrue);
      natives.first.complete();
      await _drain();
      expect(await File(newPath).exists(), isTrue);

      final completed = player.onComplete.first;
      natives.last.complete();
      await completed;
      expect(await File(newPath).exists(), isFalse);
    },
  );

  test('play failure and app background remove the managed clip', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-player-lifecycle-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final failing = _NativePlayer(failPlay: true);
    final failedPlayer = AudioplayersPracticeAudioPlayer(
      nativePlayerFactory: () => failing,
      temporaryDirectory: () async => temporary,
    );

    await expectLater(
      failedPlayer.playWav(_wave()),
      throwsA(isA<PracticeAudioPlaybackException>()),
    );
    expect(await File(failing.path!).exists(), isFalse);
    await failedPlayer.dispose();

    final native = _NativePlayer();
    final player = AudioplayersPracticeAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);
    await player.playWav(_wave());

    binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await _eventually(() async => !await File(native.path!).exists());

    expect(native.stopCount, 1);
    expect(await File(native.path!).exists(), isFalse);
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  });

  test(
    'inactive then paused emits one cleanup completion for the UI',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-multi-lifecycle-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final native = _NativePlayer();
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () => native,
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);
      var completionCount = 0;
      final completion = Completer<void>();
      final subscription = player.onComplete.listen((_) {
        completionCount++;
        if (!completion.isCompleted) {
          completion.complete();
        }
      });
      addTearDown(subscription.cancel);
      await player.playWav(_wave());

      try {
        binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
        binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
        await completion.future.timeout(const Duration(seconds: 1));
        await _eventually(() async => !await File(native.path!).exists());

        expect(completionCount, 1);
        expect(native.stopCount, 1);
        expect(native.releaseCount, 1);
        expect(native.disposeCount, 1);
        expect(await File(native.path!).exists(), isFalse);
      } finally {
        binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      }
    },
  );

  test('bounded crash cleanup leaves unrelated temp data untouched', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-player-clean-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final managed = Directory('${temporary.path}/speakup-playback-audio');
    await managed.create();
    final orphan = File('${managed.path}/clip-orphan.wav');
    final unrelated = File('${managed.path}/keep.txt');
    await orphan.writeAsBytes(_wave());
    await unrelated.writeAsString('keep');
    final native = _NativePlayer();
    final player = AudioplayersPracticeAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);

    await player.playWav(_wave());

    expect(await orphan.exists(), isFalse);
    expect(await unrelated.exists(), isTrue);
  });

  test('account switch and dispose delete each active private file', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-player-account-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final natives = <_NativePlayer>[];
    final player = AudioplayersPracticeAudioPlayer(
      nativePlayerFactory: () {
        final native = _NativePlayer();
        natives.add(native);
        return native;
      },
      temporaryDirectory: () async => temporary,
    );

    await player.playWav(_wave());
    final accountAPath = natives.first.path!;
    await player.clearAccountState();
    expect(await File(accountAPath).exists(), isFalse);

    await player.playWav(_wave());
    final accountBPath = natives.last.path!;
    await player.dispose();
    expect(await File(accountBPath).exists(), isFalse);
  });

  test(
    'dispose fences a playback waiting for the temporary directory',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-pending-directory-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final directory = Completer<Directory>();
      var nativeCount = 0;
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () {
          nativeCount++;
          return _NativePlayer();
        },
        temporaryDirectory: () => directory.future,
      );

      final playback = player.playWav(_wave());
      await _drain();
      final disposal = player.dispose();
      directory.complete(temporary);
      await Future.wait([playback, disposal]);

      expect(nativeCount, 0);
      expect(await _managedClips(temporary), isEmpty);
    },
  );

  test(
    'background fences a playback waiting for its private file write',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-pending-write-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final writeStarted = Completer<String>();
      final allowWrite = Completer<void>();
      var nativeCount = 0;
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () {
          nativeCount++;
          return _NativePlayer();
        },
        temporaryDirectory: () async => temporary,
        fileWriter: (file, bytes) async {
          await file.create(exclusive: true);
          await file.writeAsBytes(bytes, flush: true);
          writeStarted.complete(file.path);
          await allowWrite.future;
        },
      );
      addTearDown(player.dispose);

      final playback = player.playWav(_wave());
      final path = await writeStarted.future;
      final interrupted = player.onComplete.first;
      binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      allowWrite.complete();
      await expectLater(
        playback,
        throwsA(isA<PracticeAudioPlaybackInterruptedException>()),
      );
      await interrupted;

      expect(nativeCount, 0);
      expect(await File(path).exists(), isFalse);
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    },
  );

  test(
    'account cleanup fences a native play call that has not returned',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-pending-native-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final nativePlay = Completer<void>();
      final native = _NativePlayer(playGate: nativePlay);
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () => native,
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);

      final playback = player.playWav(_wave());
      await native.playStarted.future;
      final path = native.path!;
      final cleanup = player.clearAccountState();
      await Future.wait([playback, cleanup]);

      expect(native.stopCount, greaterThanOrEqualTo(1));
      expect(native.releaseCount, 1);
      expect(native.disposeCount, 1);
      expect(await File(path).exists(), isFalse);
    },
  );

  test(
    'a fetch completing in background cannot create a native player',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-background-fetch-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      var nativeCount = 0;
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () {
          nativeCount++;
          return _NativePlayer();
        },
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);

      binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      await expectLater(
        player.playWav(_wave()),
        throwsA(isA<PracticeAudioPlaybackInterruptedException>()),
      );

      expect(nativeCount, 0);
      expect(await _managedClips(temporary), isEmpty);
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    },
  );

  test(
    'late completion cleanup cannot clear a newer playback generation',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-completion-fence-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final oldRelease = Completer<void>();
      final natives = <_NativePlayer>[];
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () {
          final native = _NativePlayer(
            releaseGate: natives.isEmpty ? oldRelease : null,
          );
          natives.add(native);
          return native;
        },
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);
      var completionCount = 0;
      final subscription = player.onComplete.listen((_) => completionCount++);
      addTearDown(subscription.cancel);

      await player.playWav(_wave());
      final oldPath = natives.first.path!;
      natives.first.complete();
      await natives.first.releaseStarted.future;

      await player.playWav(_wave());
      final newPath = natives.last.path!;
      oldRelease.complete();
      await _eventually(() async => !await File(oldPath).exists());

      expect(completionCount, 0);
      expect(await File(newPath).exists(), isTrue);
      expect(natives.last.stopCount, 0);
    },
  );

  test(
    'resumed playback waits for stale background cleanup to finish',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-background-fence-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final oldRelease = Completer<void>();
      final natives = <_NativePlayer>[];
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: () {
          final native = _NativePlayer(
            releaseGate: natives.isEmpty ? oldRelease : null,
          );
          natives.add(native);
          return native;
        },
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);
      var completionCount = 0;
      final subscription = player.onComplete.listen((_) => completionCount++);
      addTearDown(subscription.cancel);

      await player.playWav(_wave());
      binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      await natives.first.releaseStarted.future;
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final replacement = player.playWav(_wave());
      await _drain();
      expect(natives, hasLength(1));

      oldRelease.complete();
      await replacement;

      expect(natives, hasLength(2));
      expect(natives.last.stopCount, 0);
      expect(await File(natives.last.path!).exists(), isTrue);
      expect(completionCount, 1);
    },
  );

  test(
    'strict account cleanup retries a transient private-file failure',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-delete-retry-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      var attempts = 0;
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: _NativePlayer.new,
        temporaryDirectory: () async => temporary,
        fileDeleter: (path) async {
          attempts++;
          if (attempts == 1) {
            throw const FileSystemException('transient');
          }
          await File(path).delete();
        },
      );
      addTearDown(player.dispose);

      await player.playWav(_wave());
      await player.clearAccountState();

      expect(attempts, greaterThanOrEqualTo(2));
      expect(await _managedClips(temporary), isEmpty);
    },
  );

  test(
    'strict account cleanup fails closed on a retained private file',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-player-delete-fail-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final player = AudioplayersPracticeAudioPlayer(
        nativePlayerFactory: _NativePlayer.new,
        temporaryDirectory: () async => temporary,
        fileDeleter: (_) async {
          throw const FileSystemException('permanent');
        },
      );
      addTearDown(player.dispose);

      await player.playWav(_wave());

      await expectLater(
        player.clearAccountState(),
        throwsA(isA<PracticeAudioPlaybackException>()),
      );
      expect(await _managedClips(temporary), isNotEmpty);
    },
  );
}

final class _NativePlayer implements NativePracticeAudioPlayer {
  _NativePlayer({this.failPlay = false, this.playGate, this.releaseGate});

  final bool failPlay;
  final Completer<void>? playGate;
  final Completer<void>? releaseGate;
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  final Completer<void> playStarted = Completer<void>();
  final Completer<void> releaseStarted = Completer<void>();
  String? path;
  int stopCount = 0;
  int releaseCount = 0;
  int disposeCount = 0;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playFile(String value) async {
    path = value;
    if (!playStarted.isCompleted) {
      playStarted.complete();
    }
    if (failPlay) {
      throw StateError('redacted native failure');
    }
    await playGate?.future;
  }

  void complete() => _completions.add(null);

  @override
  Future<void> stop() async {
    stopCount++;
    final gate = playGate;
    if (gate != null && !gate.isCompleted) {
      gate.complete();
    }
  }

  @override
  Future<void> release() async {
    releaseCount++;
    if (!releaseStarted.isCompleted) {
      releaseStarted.complete();
    }
    await releaseGate?.future;
  }

  @override
  Future<void> dispose() async {
    disposeCount++;
  }
}

Uint8List _wave() {
  final bytes = Uint8List(44);
  bytes.setAll(0, const [0x52, 0x49, 0x46, 0x46]);
  bytes.setAll(8, const [0x57, 0x41, 0x56, 0x45]);
  return bytes;
}

Future<void> _drain() async {
  await Future<void>.delayed(Duration.zero);
  await Future<void>.delayed(Duration.zero);
}

Future<void> _eventually(Future<bool> Function() predicate) async {
  for (var attempt = 0; attempt < 100; attempt++) {
    if (await predicate()) {
      return;
    }
    await Future<void>.delayed(const Duration(milliseconds: 5));
  }
  fail('Condition did not become true.');
}

Future<List<FileSystemEntity>> _managedClips(Directory temporary) async {
  final directory = Directory('${temporary.path}/speakup-playback-audio');
  if (!await directory.exists()) {
    return const <FileSystemEntity>[];
  }
  return [
    await for (final entity in directory.list(followLinks: false))
      if (entity is File &&
          entity.uri.pathSegments.last.startsWith('clip-') &&
          entity.uri.pathSegments.last.endsWith('.wav'))
        entity,
  ];
}
