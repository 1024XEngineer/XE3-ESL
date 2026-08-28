import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/platform/audio/ephemeral_wav_audio_player.dart';

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  });
  tearDown(() {
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  });

  test(
    'completion removes the managed WAV without mutating caller bytes',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-preview-complete-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final native = _NativePlayer();
      final player = AudioplayersEphemeralWavAudioPlayer(
        nativePlayerFactory: () => native,
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);
      final input = _wave();

      await player.play(input);

      final path = native.path!;
      expect(input, _wave());
      expect(path, contains('/speakup-voice-previews/'));
      expect(path, endsWith('.wav'));
      expect(await File(path).exists(), isTrue);

      final completion = player.onComplete.first;
      native.complete();
      await completion;

      expect(await File(path).exists(), isFalse);
    },
  );

  test('native completion errors terminate and clean the preview', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-event-error-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final native = _NativePlayer();
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);

    await player.play(_wave());
    final path = native.path!;
    final ended = player.onComplete.first;
    native.failCompletion(StateError('event stream failed'));
    await ended;

    expect(await File(path).exists(), isFalse);
    expect(native.disposeCount, 1);
  });

  test(
    'a terminal event settles a native play call that never returns',
    () async {
      final temporary = await Directory.systemTemp.createTemp(
        'speakup-preview-event-pending-',
      );
      addTearDown(() => temporary.delete(recursive: true));
      final playGate = Completer<void>();
      final native = _NativePlayer(playGate: playGate);
      final player = AudioplayersEphemeralWavAudioPlayer(
        nativePlayerFactory: () => native,
        temporaryDirectory: () async => temporary,
      );
      addTearDown(player.dispose);
      var completionCount = 0;
      final subscription = player.onComplete.listen((_) => completionCount++);
      addTearDown(subscription.cancel);

      final playback = player.play(_wave());
      await native.playStarted.future;
      final path = native.path!;
      final interrupted = expectLater(
        playback,
        throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
      );
      native.failCompletion(StateError('terminal before play returns'));
      await interrupted;

      expect(await File(path).exists(), isFalse);
      expect(native.disposeCount, 1);
      expect(completionCount, 0);
    },
  );

  test('stop, app background, and dispose remove each active WAV', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-lifecycle-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final natives = <_NativePlayer>[];
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () {
        final native = _NativePlayer();
        natives.add(native);
        return native;
      },
      temporaryDirectory: () async => temporary,
    );

    await player.play(_wave());
    final stoppedPath = natives[0].path!;
    await player.stop();
    expect(await File(stoppedPath).exists(), isFalse);

    await player.play(_wave());
    final backgroundedPath = natives[1].path!;
    final backgroundEnded = player.onComplete.first;
    binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await backgroundEnded;
    await _eventually(() async => !await File(backgroundedPath).exists());

    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await player.play(_wave());
    final disposedPath = natives[2].path!;
    await player.dispose();
    expect(await File(disposedPath).exists(), isFalse);
    expect(natives.map((native) => native.disposeCount), everyElement(1));

    await player.dispose();
    expect(natives.map((native) => native.disposeCount), everyElement(1));
    await expectLater(
      player.play(_wave()),
      throwsA(isA<EphemeralWavPlaybackException>()),
    );
  });

  test('invalid WAV and native playback failure never leak a file', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-failure-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final native = _NativePlayer(failPlay: true);
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);

    await expectLater(
      player.play(Uint8List(44)),
      throwsA(isA<EphemeralWavPlaybackException>()),
    );

    final input = _wave();
    await expectLater(
      player.play(input),
      throwsA(isA<EphemeralWavPlaybackException>()),
    );

    expect(input, _wave());
    expect(native.path, isNotNull);
    expect(await File(native.path!).exists(), isFalse);
  });

  test('stop failure still cleans the private preview file', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-stop-failure-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final native = _NativePlayer(failStop: true);
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);

    await player.play(_wave());
    final path = native.path!;
    await player.stop();

    expect(native.stopCount, 1);
    expect(await File(path).exists(), isFalse);
  });

  test('stop does not wait for a stalled temporary directory', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-pending-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final directory = Completer<Directory>();
    final directoryRequested = Completer<void>();
    var nativeCount = 0;
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () {
        nativeCount++;
        return _NativePlayer();
      },
      temporaryDirectory: () {
        directoryRequested.complete();
        return directory.future;
      },
    );
    addTearDown(player.dispose);

    final playback = player.play(_wave());
    await directoryRequested.future;
    final interrupted = expectLater(
      playback,
      throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
    );
    final stopping = player.stop();
    await stopping.timeout(const Duration(seconds: 1));
    await interrupted;

    expect(nativeCount, 0);
    expect(await _managedWavs(temporary), isEmpty);
  });

  test('app background fences a preview waiting for its directory', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-background-pending-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final directory = Completer<Directory>();
    final directoryRequested = Completer<void>();
    var nativeCount = 0;
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () {
        nativeCount++;
        return _NativePlayer();
      },
      temporaryDirectory: () {
        directoryRequested.complete();
        return directory.future;
      },
    );
    addTearDown(player.dispose);

    final playback = player.play(_wave());
    await directoryRequested.future;
    final interrupted = expectLater(
      playback,
      throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
    );
    binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await interrupted;

    expect(nativeCount, 0);
    expect(await _managedWavs(temporary), isEmpty);
    await expectLater(
      player.play(_wave()),
      throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
    );
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  });

  test('an older stop cannot clean a replacement preview', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-stop-replacement-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final stopGate = Completer<void>();
    final natives = <_NativePlayer>[];
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () {
        final native = _NativePlayer(
          stopGate: natives.isEmpty ? stopGate : null,
        );
        natives.add(native);
        return native;
      },
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);

    await player.play(_wave());
    final oldPath = natives.single.path!;
    final stopping = player.stop();
    await natives.single.stopStarted.future;
    final replacement = player.play(_wave());
    await _drain();
    expect(natives, hasLength(1));

    stopGate.complete();
    await Future.wait([stopping, replacement]);

    expect(natives, hasLength(2));
    expect(await File(oldPath).exists(), isFalse);
    expect(await File(natives.last.path!).exists(), isTrue);
    expect(natives.last.disposeCount, 0);
  });

  test('a third play waits for cleanup already started by a second', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-triple-play-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final stopGate = Completer<void>();
    final natives = <_NativePlayer>[];
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () {
        final native = _NativePlayer(
          stopGate: natives.isEmpty ? stopGate : null,
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

    await player.play(_wave());
    final firstPath = natives.single.path!;
    final second = player.play(_wave());
    await natives.single.stopStarted.future;
    final secondInterrupted = expectLater(
      second,
      throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
    );
    final third = player.play(_wave());
    var thirdStarted = false;
    unawaited(third.then((_) => thirdStarted = true));
    await _drain();

    expect(natives, hasLength(1));
    expect(thirdStarted, isFalse);
    stopGate.complete();
    await secondInterrupted;
    await third;

    expect(natives, hasLength(2));
    expect(await File(firstPath).exists(), isFalse);
    expect(await File(natives.last.path!).exists(), isTrue);
    expect(completionCount, 1);
  });

  test('dispose waits for cleanup started by a replacing play', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-dispose-replacement-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final stopGate = Completer<void>();
    final native = _NativePlayer(stopGate: stopGate);
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );

    await player.play(_wave());
    final path = native.path!;
    final replacement = player.play(_wave());
    await native.stopStarted.future;
    final interrupted = expectLater(
      replacement,
      throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
    );
    final disposal = player.dispose();
    var disposed = false;
    unawaited(disposal.then((_) => disposed = true));
    await _drain();

    expect(disposed, isFalse);
    stopGate.complete();
    await Future.wait([interrupted, disposal]);

    expect(disposed, isTrue);
    expect(await File(path).exists(), isFalse);
    expect(native.disposeCount, 1);
  });

  test('a replaced player cannot complete the current preview', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-replacement-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final natives = <_NativePlayer>[];
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () {
        final native = _NativePlayer(
          closeStreamOnDispose: false,
          deliverCompletionsAfterCancel: true,
        );
        natives.add(native);
        return native;
      },
      temporaryDirectory: () async => temporary,
    );
    addTearDown(player.dispose);
    addTearDown(() async {
      for (final native in natives) {
        await native.close();
      }
    });
    var completionCount = 0;
    final subscription = player.onComplete.listen((_) => completionCount++);
    addTearDown(subscription.cancel);

    await player.play(_wave());
    final replacedPath = natives[0].path!;
    await player.play(_wave());
    final currentPath = natives[1].path!;

    expect(await File(replacedPath).exists(), isFalse);
    expect(await File(currentPath).exists(), isTrue);
    expect(completionCount, 1);
    natives[0].complete();
    await _drain();
    expect(await File(currentPath).exists(), isTrue);
    expect(completionCount, 1);
    expect(natives[1].disposeCount, 0);

    final completion = player.onComplete.first;
    natives[1].complete();
    await completion;
    expect(await File(currentPath).exists(), isFalse);
    expect(completionCount, 2);
    expect(natives[1].disposeCount, 1);
  });

  test('dispose fences a native play call that has not returned', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-dispose-fence-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final playGate = Completer<void>();
    final native = _NativePlayer(
      playGate: playGate,
      closeStreamOnDispose: false,
    );
    addTearDown(native.close);
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    var completionCount = 0;
    final subscription = player.onComplete.listen((_) => completionCount++);
    addTearDown(subscription.cancel);

    final playback = player.play(_wave());
    await native.playStarted.future;
    final path = native.path!;
    final interrupted = expectLater(
      playback,
      throwsA(isA<EphemeralWavPlaybackInterruptedException>()),
    );
    final disposal = player.dispose();
    await Future.wait([interrupted, disposal]);

    expect(await File(path).exists(), isFalse);
    expect(native.stopCount, 1);
    expect(native.disposeCount, 1);
    native.complete();
    await _drain();
    expect(completionCount, 0);
    await expectLater(
      player.play(_wave()),
      throwsA(isA<EphemeralWavPlaybackException>()),
    );
  });

  test('concurrent dispose calls share the same cleanup', () async {
    final temporary = await Directory.systemTemp.createTemp(
      'speakup-preview-concurrent-dispose-',
    );
    addTearDown(() => temporary.delete(recursive: true));
    final stopGate = Completer<void>();
    final native = _NativePlayer(stopGate: stopGate);
    final player = AudioplayersEphemeralWavAudioPlayer(
      nativePlayerFactory: () => native,
      temporaryDirectory: () async => temporary,
    );
    await player.play(_wave());

    final first = player.dispose();
    await native.stopStarted.future;
    final second = player.dispose();
    expect(identical(first, second), isTrue);

    var completed = false;
    unawaited(second.then((_) => completed = true));
    await _drain();
    expect(completed, isFalse);
    stopGate.complete();
    await Future.wait([first, second]);

    expect(completed, isTrue);
    expect(native.stopCount, 1);
    expect(native.disposeCount, 1);
  });

  test('native adapter cannot start after stop overtakes setup', () async {
    final releaseGate = Completer<void>();
    final audioPlayer = _AudioPlayerDouble(releaseGate: releaseGate);
    final player = AudioplayersNativeEphemeralWavAudioPlayer(audioPlayer);

    final playback = player.playFile('/tmp/preview.wav');
    await audioPlayer.releaseStarted.future;
    await player.stop();
    releaseGate.complete();
    await playback;

    expect(audioPlayer.playCount, 0);
    expect(audioPlayer.stopCount, 1);
    await player.dispose();
    await player.dispose();
    expect(audioPlayer.disposeCount, 1);
  });
}

final class _AudioPlayerDouble implements AudioPlayer {
  _AudioPlayerDouble({required this.releaseGate});

  final Completer<void> releaseGate;
  final Completer<void> releaseStarted = Completer<void>();
  int playCount = 0;
  int stopCount = 0;
  int disposeCount = 0;

  @override
  Stream<void> get onPlayerComplete => const Stream<void>.empty();

  @override
  Future<void> setReleaseMode(ReleaseMode releaseMode) async {
    releaseStarted.complete();
    await releaseGate.future;
  }

  @override
  Future<void> play(
    Source source, {
    double? volume,
    double? balance,
    AudioContext? ctx,
    Duration? position,
    PlayerMode? mode,
  }) async {
    playCount++;
  }

  @override
  Future<void> stop() async {
    stopCount++;
  }

  @override
  Future<void> dispose() async {
    disposeCount++;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

final class _NativePlayer implements NativeEphemeralWavAudioPlayer {
  _NativePlayer({
    this.failPlay = false,
    this.failStop = false,
    this.playGate,
    this.stopGate,
    this.closeStreamOnDispose = true,
    bool deliverCompletionsAfterCancel = false,
  }) : _completions = _CompletionEvents(
         deliverAfterCancel: deliverCompletionsAfterCancel,
       );

  final bool failPlay;
  final bool failStop;
  final Completer<void>? playGate;
  final Completer<void>? stopGate;
  final bool closeStreamOnDispose;
  final _CompletionEvents _completions;
  final Completer<void> playStarted = Completer<void>();
  final Completer<void> stopStarted = Completer<void>();
  String? path;
  int playCount = 0;
  int stopCount = 0;
  int disposeCount = 0;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playFile(String path) async {
    this.path = path;
    playCount++;
    if (!playStarted.isCompleted) playStarted.complete();
    await playGate?.future;
    if (failPlay) throw StateError('native play failed');
  }

  @override
  Future<void> stop() async {
    stopCount++;
    if (!stopStarted.isCompleted) stopStarted.complete();
    await stopGate?.future;
    if (failStop) throw StateError('native stop failed');
  }

  @override
  Future<void> dispose() async {
    disposeCount++;
    if (closeStreamOnDispose) await close();
  }

  void complete() => _completions.add();

  void failCompletion(Object error) => _completions.addError(error);

  Future<void> close() => _completions.close();
}

final class _CompletionEvents {
  _CompletionEvents({required bool deliverAfterCancel}) {
    stream = deliverAfterCancel
        ? _UncancellableStream<void>(_controller.stream)
        : _controller.stream;
  }

  final StreamController<void> _controller = StreamController<void>.broadcast(
    sync: true,
  );
  late final Stream<void> stream;

  void add() => _controller.add(null);

  void addError(Object error) => _controller.addError(error);

  Future<void> close() async {
    if (!_controller.isClosed) await _controller.close();
  }
}

final class _UncancellableStream<T> extends Stream<T> {
  const _UncancellableStream(this._source);

  final Stream<T> _source;

  @override
  StreamSubscription<T> listen(
    void Function(T event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) => _UncancellableSubscription<T>(
    _source.listen(
      onData,
      onError: onError,
      onDone: onDone,
      cancelOnError: cancelOnError,
    ),
  );
}

final class _UncancellableSubscription<T> implements StreamSubscription<T> {
  const _UncancellableSubscription(this._source);

  final StreamSubscription<T> _source;

  @override
  Future<E> asFuture<E>([E? futureValue]) => _source.asFuture(futureValue);

  @override
  Future<void> cancel() async {}

  @override
  bool get isPaused => _source.isPaused;

  @override
  void onData(void Function(T data)? handleData) => _source.onData(handleData);

  @override
  void onDone(void Function()? handleDone) => _source.onDone(handleDone);

  @override
  void onError(Function? handleError) => _source.onError(handleError);

  @override
  void pause([Future<void>? resumeSignal]) => _source.pause(resumeSignal);

  @override
  void resume() => _source.resume();
}

Future<List<FileSystemEntity>> _managedWavs(Directory temporary) async {
  final managed = Directory('${temporary.path}/speakup-voice-previews');
  if (!await managed.exists()) return const <FileSystemEntity>[];
  return managed.list().toList();
}

Future<void> _eventually(Future<bool> Function() condition) async {
  for (var attempt = 0; attempt < 100; attempt++) {
    if (await condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  fail('Condition was not satisfied before timeout.');
}

Future<void> _drain() async {
  await Future<void>.delayed(Duration.zero);
  await Future<void>.delayed(Duration.zero);
}

Uint8List _wave() => Uint8List.fromList(const <int>[
  0x52,
  0x49,
  0x46,
  0x46,
  0x26,
  0,
  0,
  0,
  0x57,
  0x41,
  0x56,
  0x45,
  0x66,
  0x6d,
  0x74,
  0x20,
  0x10,
  0,
  0,
  0,
  1,
  0,
  1,
  0,
  0xc0,
  0x5d,
  0,
  0,
  0x80,
  0xbb,
  0,
  0,
  2,
  0,
  0x10,
  0,
  0x64,
  0x61,
  0x74,
  0x61,
  2,
  0,
  0,
  0,
  0,
  0,
]);
