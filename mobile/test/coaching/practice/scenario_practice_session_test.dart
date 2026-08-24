import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice_session.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';
import 'avatar/avatar_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('routes realtime question PCM exclusively through the avatar', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    final nativePlayer = _RecordingPCMStreamPlayer();
    final practiceController = PracticeController(
      client: FakePracticeClient(),
      mediaClient: media,
      audioPlayer: _SilentPracticeAudioPlayer(),
      questionSpeechPlayer: nativePlayer,
      automaticQuestionSpeechEnabled: false,
    );
    addTearDown(practiceController.dispose);
    await activateTestPractice(
      controller: practiceController,
      scene: testScenes[2],
    );

    final renderer = FakeAvatarRenderer();
    final avatarController = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async {},
      fallbackStop: () async {},
      delay: (_) async {},
    );
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
          surfaceKey: const Key('avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return avatar.surfaceBuilder?.call(context) ??
                const SizedBox.expand();
          },
        ),
      ),
    );
    await tester.pump();
    media.release();
    await tester.pumpAndSettle();

    expect(renderer.sends.map((send) => send.bytes), <Uint8List>[
      Uint8List.fromList(<int>[1, 2, 3, 4]),
      Uint8List.fromList(<int>[5, 6, 7, 8]),
    ]);
    expect(renderer.sends.map((send) => send.end), <bool>[false, true]);
    expect(nativePlayer.events, isEmpty);

    final firstQuestionId = practiceController.questionId;
    await sessionView!.interruptForUserTurn();
    expect(
      await practiceController.submitPracticeText('My first answer.'),
      isTrue,
    );
    expect(practiceController.questionId, isNot(firstQuestionId));
    await tester.pumpAndSettle();
    expect(
      renderer.sends.length,
      4,
      reason:
          'questions=${media.questionIds}, native=${nativePlayer.events}, '
          'loading=${practiceController.isQuestionAudioLoading}, '
          'playing=${practiceController.isQuestionAudioPlaying}, '
          'mediaError=${practiceController.mediaErrorMessage}, '
          'avatar=${avatarController.state.phase}, '
          'replayLoading=${sessionView?.replayLoading}, '
          'replayPlaying=${sessionView?.replayPlaying}, '
          'recording=${practiceController.recordingState}, '
          'busy=${practiceController.isBusy}',
    );
    expect(renderer.sends.map((send) => send.end), <bool>[
      false,
      true,
      false,
      true,
    ]);
    expect(nativePlayer.events, isEmpty);

    await sessionView!.onReplayQuestion!();
    await _pumpUntil(tester, () => renderer.sends.length == 6);
    expect(renderer.sends.map((send) => send.end), <bool>[
      false,
      true,
      false,
      true,
      false,
      true,
    ]);
    expect(nativePlayer.events, isEmpty);
  });

  testWidgets('routes the first IELTS realtime question through the avatar', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    final nativePlayer = _RecordingPCMStreamPlayer();
    const mode = PracticeMode.part1;
    final scene = testScene(
      id: 'ielts-avatar-realtime',
      experience: PracticeExperience.ieltsSpeaking,
      category: SceneCategory.ieltsSpeaking,
      practiceOptions: const <PracticeOption>[
        PracticeOption(
          id: 'ielts-part-1',
          sceneId: 'ielts-avatar-realtime',
          mode: mode,
          displayName: 'Part 1',
          suggestedDurationSeconds: 300,
          turnPolicyRef: 'turn-ielts-avatar',
          sessionPolicyRef: 'session-ielts-avatar',
          evaluationPolicyRef: 'evaluation-ielts-avatar',
        ),
      ],
    );
    final practiceController = PracticeController(
      client: FakePracticeClient(
        practiceExperience: PracticeExperience.ieltsSpeaking,
        sceneCategory: SceneCategory.ieltsSpeaking,
        practiceMode: mode,
        turnLimit: 3,
        ieltsAssignment: testIeltsAssignment(mode: mode, part1QuestionCount: 3),
      ),
      mediaClient: media,
      audioPlayer: _SilentPracticeAudioPlayer(),
      questionSpeechPlayer: nativePlayer,
      automaticQuestionSpeechEnabled: false,
    );
    addTearDown(practiceController.dispose);
    await activateTestPractice(controller: practiceController, scene: scene);

    final renderer = FakeAvatarRenderer();
    final avatarController = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async {},
      fallbackStop: () async {},
      delay: (_) async {},
    );
    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
          surfaceKey: const Key('ielts-avatar-surface'),
          builder: (context, avatar) =>
              avatar.surfaceBuilder?.call(context) ?? const SizedBox.expand(),
        ),
      ),
    );
    await tester.pump();
    media.release();
    await tester.pumpAndSettle();

    expect(renderer.sends.map((send) => send.end), <bool>[false, true]);
    expect(nativePlayer.events, isEmpty);
  });

  testWidgets(
    'uses native speech when the avatar disconnects before the first PCM send',
    (tester) async {
      final media = _RealtimeQuestionMediaClient();
      final nativePlayer = _RecordingPCMStreamPlayer();
      final practiceController = PracticeController(
        client: FakePracticeClient(),
        mediaClient: media,
        audioPlayer: _SilentPracticeAudioPlayer(),
        questionSpeechPlayer: nativePlayer,
        automaticQuestionSpeechEnabled: false,
      );
      addTearDown(practiceController.dispose);
      await activateTestPractice(
        controller: practiceController,
        scene: testScenes[2],
      );

      final renderer = FakeAvatarRenderer()..interruptGate = Completer<void>();
      final avatarController = AvatarController(
        renderer: renderer,
        tokenClient: FakeAvatarSessionTokenClient(),
        fallbackPlayback: (_) async {},
        fallbackStop: () async {},
        delay: (_) async {},
      );
      await tester.pumpWidget(
        MaterialApp(
          home: PracticeAvatarSession(
            practiceController: practiceController,
            avatarControllerFactory: () => avatarController,
            surfaceKey: const Key('disconnect-before-pcm-surface'),
            builder: (context, avatar) =>
                avatar.surfaceBuilder?.call(context) ?? const SizedBox.expand(),
          ),
        ),
      );
      await tester.pump();
      media.release();
      await _pumpUntil(tester, () => renderer.interruptCount == 1);

      renderer.emit(
        const AvatarRendererState(
          connection: AvatarRendererConnection.failed,
          failure: AvatarRendererFailure.network,
        ),
      );
      renderer.interruptGate!.complete();
      await tester.pumpAndSettle();

      expect(renderer.sends, isEmpty);
      expect(nativePlayer.events, <String>[
        'start',
        'append:4',
        'append:4',
        'finish',
      ]);
    },
  );

  testWidgets('keeps a loaded avatar surface visible after a disconnect', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    final practiceController = PracticeController(
      client: FakePracticeClient(),
      mediaClient: media,
      audioPlayer: _SilentPracticeAudioPlayer(),
      questionSpeechPlayer: _RecordingPCMStreamPlayer(),
      automaticQuestionSpeechEnabled: false,
    );
    addTearDown(practiceController.dispose);
    await activateTestPractice(
      controller: practiceController,
      scene: testScenes[2],
    );
    final renderer = FakeAvatarRenderer();
    final avatarController = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: (_) async {},
      fallbackStop: () async {},
      delay: (_) async {},
    );

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
          surfaceKey: const Key('retained-avatar-surface'),
          builder: (context, avatar) =>
              avatar.surfaceBuilder?.call(context) ??
              const SizedBox(key: Key('static-fallback')),
        ),
      ),
    );
    await tester.pump();
    media.release();
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('retained-avatar-surface')), findsOneWidget);

    renderer.emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();

    expect(find.byKey(const Key('retained-avatar-surface')), findsOneWidget);
    expect(find.byKey(const Key('static-fallback')), findsNothing);
  });

  testWidgets(
    'keeps the existing static role and native speech when avatar prepare fails',
    (tester) async {
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final media = _RealtimeQuestionMediaClient();
      final nativePlayer = _RecordingPCMStreamPlayer();
      final practiceController = PracticeController(
        client: FakePracticeClient(),
        mediaClient: media,
        audioPlayer: _SilentPracticeAudioPlayer(),
        questionSpeechPlayer: nativePlayer,
        automaticQuestionSpeechEnabled: false,
      );
      addTearDown(practiceController.dispose);
      await activateTestPractice(
        controller: practiceController,
        scene: testScenes[2],
      );
      final renderer = FakeAvatarRenderer(
        prepareFailure: AvatarRendererFailure.unsupportedDevice,
      );
      final avatarController = AvatarController(
        renderer: renderer,
        tokenClient: FakeAvatarSessionTokenClient(),
        fallbackPlayback: (_) async {},
        fallbackStop: () async {},
        delay: (_) async {},
      );

      await tester.pumpWidget(
        MaterialApp(
          home: ScenarioPracticeSession(
            practiceController: practiceController,
            avatarControllerFactory: () => avatarController,
          ),
        ),
      );
      await tester.pump();

      expect(
        find.byKey(const Key('scenario-role-placeholder')),
        findsOneWidget,
      );
      expect(find.text('画面暂不可用，语音仍可继续'), findsOneWidget);

      media.release();
      await tester.pumpAndSettle();

      expect(renderer.sends, isEmpty);
      expect(nativePlayer.events, <String>[
        'start',
        'append:4',
        'append:4',
        'finish',
      ]);
    },
  );
}

Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var attempts = 0; attempts < 50; attempts++) {
    if (condition()) {
      return;
    }
    await tester.pump(const Duration(milliseconds: 10));
  }
  fail('Condition was not reached.');
}

final class _RealtimeQuestionMediaClient
    implements PracticeMediaClient, PracticeQuestionSpeechClient {
  final Completer<void> _release = Completer<void>();
  final List<String> questionIds = <String>[];

  void release() => _release.complete();

  @override
  Stream<Uint8List> streamQuestionSpeech(String questionId) async* {
    questionIds.add(questionId);
    await _release.future;
    yield Uint8List.fromList(<int>[1, 2, 3, 4]);
    yield Uint8List.fromList(<int>[5, 6, 7, 8]);
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> deleteRecording(String audioAssetId) async {}

  @override
  Future<void> dispose() async {}

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) =>
      throw UnimplementedError();

  @override
  Future<Uint8List> loadRecording(String audioAssetId) =>
      throw UnimplementedError();
}

final class _RecordingPCMStreamPlayer implements PracticePCMStreamPlayer {
  final List<String> events = <String>[];

  @override
  Future<void> appendPCM(Uint8List bytes) async {
    events.add('append:${bytes.length}');
  }

  @override
  Future<void> disposePCMStream() async {}

  @override
  Future<void> finishPCMStream() async {
    events.add('finish');
  }

  @override
  Future<void> startPCMStream() async {
    events.add('start');
  }

  @override
  Future<void> stopPCMStream() async {}
}

final class _SilentPracticeAudioPlayer implements PracticeAudioPlayer {
  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}

  @override
  Future<void> playWav(Uint8List bytes) async {}

  @override
  Future<void> stop() async {}
}
