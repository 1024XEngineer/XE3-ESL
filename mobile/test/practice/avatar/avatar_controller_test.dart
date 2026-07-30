import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/practice/avatar/avatar.dart';

import 'avatar_test_fakes.dart';

void main() {
  test(
    'connect keeps the session capability inside the renderer boundary',
    () async {
      final renderer = FakeAvatarRenderer();
      final tokenClient = FakeAvatarSessionTokenClient();
      final controller = AvatarController(
        renderer: renderer,
        tokenClient: tokenClient,
        fallbackPlayback: (_) async {},
        fallbackStop: () async {},
        delay: (_) async {},
      );
      addTearDown(controller.close);

      expect(await controller.connect(practiceSessionId: 'practice-1'), isTrue);

      expect(tokenClient.requestedSessionIds, ['practice-1']);
      expect(renderer.preparedGrant, same(testAvatarGrant));
      expect(controller.state.phase, AvatarControllerPhase.ready);
      expect(controller.state.canUseAvatar, isTrue);
      expect(controller.buildSurface(), isNotNull);
    },
  );

  test('avatar owns WAV playback and marks end exactly once', () async {
    final renderer = FakeAvatarRenderer();
    var fallbackCount = 0;
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async => fallbackCount++,
      fallbackStop: () async {},
      delay: (_) async {},
    );
    addTearDown(controller.close);
    await controller.connect(practiceSessionId: 'practice-1');
    final wav = buildPcmWave(pcm: Uint8List(100000));

    final result = await controller.speakWav(wav);

    expect(result, AvatarSpeechResult.avatar);
    expect(fallbackCount, 0);
    expect(renderer.sends.map((send) => send.bytes.length), [
      48000,
      48000,
      4000,
    ]);
    expect(renderer.sends.map((send) => send.end), [false, false, true]);
    expect(renderer.sends.where((send) => send.end), hasLength(1));
  });

  test('invalid WAV exclusively falls back without sending PCM', () async {
    final renderer = FakeAvatarRenderer();
    final fallbackWaves = <Uint8List>[];
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (bytes) async => fallbackWaves.add(bytes),
      fallbackStop: () async {},
      delay: (_) async {},
    );
    addTearDown(controller.close);
    await controller.connect(practiceSessionId: 'practice-1');
    final invalid = buildPcmWave(sampleRate: 16000);

    final result = await controller.speakWav(invalid);

    expect(result, AvatarSpeechResult.fallback);
    expect(renderer.sends, isEmpty);
    expect(fallbackWaves, [invalid]);
  });

  test('native send failure interrupts and falls back once', () async {
    final renderer = FakeAvatarRenderer()..failSendAt = 1;
    var fallbackCount = 0;
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async => fallbackCount++,
      fallbackStop: () async {},
      delay: (_) async {},
    );
    addTearDown(controller.close);
    await controller.connect(practiceSessionId: 'practice-1');

    final result = await controller.speakWav(
      buildPcmWave(pcm: Uint8List(100000)),
    );

    expect(result, AvatarSpeechResult.fallback);
    expect(renderer.sends, hasLength(2));
    expect(renderer.sends.where((send) => send.end), isEmpty);
    expect(renderer.interruptCount, greaterThanOrEqualTo(2));
    expect(fallbackCount, 1);
  });

  test(
    'interrupt fences an in-flight send and prevents late fallback',
    () async {
      final renderer = FakeAvatarRenderer()..holdSends = true;
      var fallbackCount = 0;
      final controller = AvatarController(
        renderer: renderer,
        tokenClient: FakeAvatarSessionTokenClient(),
        fallbackPlayback: (_) async => fallbackCount++,
        fallbackStop: () async {},
        delay: (_) async {},
      );
      addTearDown(controller.close);
      await controller.connect(practiceSessionId: 'practice-1');

      final speech = controller.speakWav(buildPcmWave(pcm: Uint8List(96000)));
      await _until(() => renderer.pendingSends.isNotEmpty);
      await controller.interrupt();
      renderer.pendingSends.single.complete();

      expect(await speech, AvatarSpeechResult.interrupted);
      expect(renderer.sends, hasLength(1));
      expect(renderer.sends.single.end, isFalse);
      expect(fallbackCount, 0);
    },
  );

  test('a fresh utterance waits for one coalesced interrupt', () async {
    final gate = Completer<void>();
    final renderer = FakeAvatarRenderer()..interruptGate = gate;
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async {},
      fallbackStop: () async {},
      delay: (_) async {},
    );
    addTearDown(controller.close);
    await controller.connect(practiceSessionId: 'practice-1');

    final firstInterrupt = controller.interrupt();
    await _until(() => renderer.interruptCount == 1);
    final speech = controller.speakWav(buildPcmWave(pcm: Uint8List(48000)));
    await Future<void>.delayed(Duration.zero);

    expect(renderer.interruptCount, 1);
    expect(renderer.sends, isEmpty);

    gate.complete();
    await firstInterrupt;

    expect(await speech, AvatarSpeechResult.avatar);
    expect(renderer.interruptCount, 1);
    expect(renderer.actions.last, 'send');
  });

  test('partial native send is stopped before local fallback starts', () async {
    final renderer = FakeAvatarRenderer()
      ..failSendAt = 1
      ..interruptError = StateError('native interrupt failed');
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async => renderer.actions.add('fallback'),
      fallbackStop: () async {},
      delay: (_) async {},
    );
    addTearDown(controller.close);
    await controller.connect(practiceSessionId: 'practice-1');

    final result = await controller.speakWav(
      buildPcmWave(pcm: Uint8List(100000)),
    );

    expect(result, AvatarSpeechResult.fallback);
    expect(renderer.actions, containsAllInOrder(['send', 'close', 'fallback']));
    expect(
      renderer.actions.lastIndexOf('close'),
      lessThan(renderer.actions.indexOf('fallback')),
    );
  });

  test(
    'never starts local fallback when native playback cannot be stopped',
    () async {
      final renderer = FakeAvatarRenderer()
        ..failSendAt = 0
        ..interruptError = StateError('native interrupt failed')
        ..closeError = StateError('native close failed');
      var fallbackCount = 0;
      final controller = AvatarController(
        renderer: renderer,
        tokenClient: FakeAvatarSessionTokenClient(),
        fallbackPlayback: (_) async => fallbackCount++,
        fallbackStop: () async {},
        delay: (_) async {},
      );
      await controller.connect(practiceSessionId: 'practice-1');

      final result = await controller.speakWav(
        buildPcmWave(pcm: Uint8List(48000)),
      );

      expect(result, AvatarSpeechResult.interrupted);
      expect(fallbackCount, 0);
      renderer
        ..interruptError = null
        ..closeError = null;
      await controller.close();
    },
  );

  test('close and account cleanup are idempotent', () async {
    final renderer = FakeAvatarRenderer();
    final tokenClient = FakeAvatarSessionTokenClient();
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: tokenClient,
      fallbackPlayback: (_) async {},
      fallbackStop: () async {},
    );

    await Future.wait([controller.clearAccountState(), controller.close()]);
    await controller.close();

    expect(renderer.closeCount, 1);
    expect(tokenClient.clearCount, 1);
    expect(controller.state.phase, AvatarControllerPhase.closed);
  });

  test(
    'account cleanup still clears token state when native close fails',
    () async {
      final renderer = FakeAvatarRenderer()
        ..closeError = StateError('native close failed');
      final tokenClient = FakeAvatarSessionTokenClient();
      final controller = AvatarController(
        renderer: renderer,
        tokenClient: tokenClient,
        fallbackPlayback: (_) async {},
        fallbackStop: () async {},
      );
      await controller.connect(practiceSessionId: 'practice-1');

      await expectLater(controller.clearAccountState(), throwsStateError);

      expect(tokenClient.clearCount, 1);
      expect(controller.state.phase, AvatarControllerPhase.closed);
    },
  );

  test('interrupt and close stop fallback playback', () async {
    final renderer = FakeAvatarRenderer();
    final fallbackStarted = Completer<void>();
    final fallbackFinished = Completer<void>();
    var stopCount = 0;
    final controller = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async {
        fallbackStarted.complete();
        await fallbackFinished.future;
      },
      fallbackStop: () async {
        stopCount++;
        if (!fallbackFinished.isCompleted) {
          fallbackFinished.complete();
        }
      },
    );
    await controller.connect(practiceSessionId: 'practice-1');
    final speech = controller.speakWav(buildPcmWave(sampleRate: 16000));
    await fallbackStarted.future;

    await controller.interrupt();

    expect(await speech, AvatarSpeechResult.interrupted);
    expect(stopCount, greaterThanOrEqualTo(2));

    await controller.close();
    expect(stopCount, greaterThanOrEqualTo(3));
  });
}

Future<void> _until(bool Function() condition) async {
  for (var attempts = 0; attempts < 50; attempts += 1) {
    if (condition()) {
      return;
    }
    await Future<void>.delayed(Duration.zero);
  }
  fail('Condition was not reached.');
}
