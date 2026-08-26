import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_stage.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice_session.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';
import 'avatar/avatar_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('waits for an explicit question playback request', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient()..release();
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
    final avatarController = _avatarController(renderer);
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
          surfaceKey: const Key('explicit-playback-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return avatar.surfaceBuilder?.call(context) ??
                const SizedBox.expand();
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => sessionView?.surfaceVisible ?? false);

    expect(renderer.sends, isEmpty);
    expect(nativePlayer.events, isEmpty);

    final completed = sessionView!.onPlayQuestion!();
    await tester.pumpAndSettle();

    expect(await completed, isTrue);
    expect(renderer.sends.map((send) => send.end), <bool>[false, true]);
    expect(nativePlayer.events, isEmpty);
  });

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
    final firstPlayback = sessionView!.onPlayQuestion!();
    media.release();
    await tester.pumpAndSettle();
    expect(await firstPlayback, isTrue);

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
    final secondPlayback = sessionView!.onPlayQuestion!();
    await tester.pumpAndSettle();
    expect(await secondPlayback, isTrue);
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
    PracticeAvatarSessionView? sessionView;
    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
          surfaceKey: const Key('ielts-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return avatar.surfaceBuilder?.call(context) ??
                const SizedBox.expand();
          },
        ),
      ),
    );
    await tester.pump();
    final playback = sessionView!.onPlayQuestion!();
    media.release();
    await tester.pumpAndSettle();
    expect(await playback, isTrue);

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
      PracticeAvatarSessionView? sessionView;
      await tester.pumpWidget(
        MaterialApp(
          home: PracticeAvatarSession(
            practiceController: practiceController,
            avatarControllerFactory: () => avatarController,
            surfaceKey: const Key('disconnect-before-pcm-surface'),
            builder: (context, avatar) {
              sessionView = avatar;
              return avatar.surfaceBuilder?.call(context) ??
                  const SizedBox.expand();
            },
          ),
        ),
      );
      await tester.pump();
      final playback = sessionView!.onPlayQuestion!();
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
      expect(await playback, isTrue);

      expect(renderer.sends, isEmpty);
      expect(nativePlayer.events, <String>[
        'start',
        'append:4',
        'append:4',
        'finish',
      ]);
    },
  );

  testWidgets('keeps fallback over a mounted surface before first connection', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final renderer = FakeAvatarRenderer(connectOnPrepare: false);
    final avatarController = _avatarController(renderer);
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => renderer.preparedGrant != null);
    await tester.pump();

    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.text('正在准备情景角色'), findsOneWidget);

    renderer.emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();

    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.text('正在重新连接情景角色'), findsOneWidget);
    expect(find.text('画面暂不可用，语音仍可继续'), findsNothing);
  });

  testWidgets('keeps fallback stable through the first retry replacement', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final surfaceEvents = <String>[];
    final firstRenderer = FakeAvatarRenderer(
      surfaceBuilder: (key) =>
          _SurfaceLifecycleProbe(key: key, id: 'first', events: surfaceEvents),
    );
    final secondRenderer = FakeAvatarRenderer(
      connectOnPrepare: false,
      surfaceBuilder: (key) =>
          _SurfaceLifecycleProbe(key: key, id: 'second', events: surfaceEvents),
    );
    final controllers = <AvatarController>[
      _avatarController(firstRenderer),
      _avatarController(secondRenderer),
    ];
    var factoryCalls = 0;
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => controllers[factoryCalls++],
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => sessionView?.surfaceVisible ?? false);
    await tester.pump();
    expect(sessionView?.surfaceVisible, isTrue);
    expect(find.byKey(const Key('static-fallback')), findsNothing);
    expect(surfaceEvents, <String>['init:first']);

    firstRenderer.emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();

    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(find.text('正在重新连接情景角色'), findsOneWidget);
    expect(nativePlayer.events, isEmpty);

    await tester.pump(const Duration(milliseconds: 999));
    expect(factoryCalls, 1);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 1));
    await _flushControllerReplacement(tester);
    await _pumpUntil(tester, () => secondRenderer.preparedGrant != null);
    await tester.pump();

    expect(factoryCalls, 2);
    expect(firstRenderer.closeCount, 1);
    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.text('正在重新连接情景角色'), findsOneWidget);
    expect(find.text('画面暂不可用，语音仍可继续'), findsNothing);
    expect(nativePlayer.events, isEmpty);
    expect(surfaceEvents, hasLength(3));
    expect(
      surfaceEvents,
      containsAll(<String>['init:first', 'dispose:first', 'init:second']),
    );
  });

  testWidgets('cancels replacement when the renderer reconnects itself', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final firstRenderer = FakeAvatarRenderer();
    final secondRenderer = FakeAvatarRenderer();
    final controllers = <AvatarController>[
      _avatarController(firstRenderer),
      _avatarController(secondRenderer),
    ];
    var factoryCalls = 0;
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => controllers[factoryCalls++],
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => sessionView?.surfaceVisible ?? false);
    await tester.pump();

    firstRenderer.emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.text('正在重新连接情景角色'), findsOneWidget);

    firstRenderer.emit(
      const AvatarRendererState(connection: AvatarRendererConnection.connected),
    );
    await tester.pump();
    expect(sessionView?.surfaceVisible, isTrue);
    expect(find.byKey(const Key('static-fallback')), findsNothing);
    expect(find.text('正在重新连接情景角色'), findsNothing);

    await tester.pump(const Duration(seconds: 1));
    expect(factoryCalls, 1);
    expect(firstRenderer.closeCount, 0);
    expect(secondRenderer.preparedGrant, isNull);
  });

  testWidgets('rebuilds the controller after an immediate pause and resume', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final firstRenderer = FakeAvatarRenderer();
    final secondRenderer = FakeAvatarRenderer();
    final controllers = <AvatarController>[
      _avatarController(firstRenderer),
      _avatarController(secondRenderer),
    ];
    var factoryCalls = 0;
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => controllers[factoryCalls++],
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => sessionView?.surfaceVisible ?? false);
    await tester.pump();

    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pump();
    await _flushControllerReplacement(tester);
    await _pumpUntil(tester, () => secondRenderer.preparedGrant != null);
    await tester.pump();

    expect(factoryCalls, 2);
    expect(firstRenderer.closeCount, 1);
    expect(sessionView?.surfaceVisible, isTrue);
    expect(find.byKey(const Key('static-fallback')), findsNothing);
  });

  testWidgets('shows unavailable only after exactly three failed attempts', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final renderers = List<FakeAvatarRenderer>.generate(
      3,
      (_) => FakeAvatarRenderer(connectOnPrepare: false),
    );
    final controllers = renderers.map(_avatarController).toList();
    var factoryCalls = 0;
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => controllers[factoryCalls++],
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => renderers[0].preparedGrant != null);
    await tester.pump();
    final playback = sessionView!.onPlayQuestion!();

    renderers[0].emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();
    expect(find.text('正在重新连接情景角色'), findsOneWidget);
    expect(find.text('画面暂不可用，语音仍可继续'), findsNothing);
    await tester.pump(const Duration(seconds: 1));
    await _flushControllerReplacement(tester);
    await _pumpUntil(tester, () => renderers[1].preparedGrant != null);
    await tester.pump();

    renderers[1].emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();
    expect(find.text('正在重新连接情景角色'), findsOneWidget);
    expect(find.text('画面暂不可用，语音仍可继续'), findsNothing);
    await tester.pump(const Duration(milliseconds: 1500));
    await _flushControllerReplacement(tester);
    await _pumpUntil(tester, () => renderers[2].preparedGrant != null);
    await tester.pump();

    renderers[2].emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await _pumpUntil(tester, () => nativePlayer.events.length == 4);

    expect(factoryCalls, 3);
    expect(
      renderers.map((renderer) => renderer.preparedGrant),
      everyElement(isNotNull),
    );
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(find.text('正在重新连接情景角色'), findsNothing);
    expect(find.text('画面暂不可用，语音仍可继续'), findsOneWidget);
    expect(nativePlayer.events, <String>[
      'start',
      'append:4',
      'append:4',
      'finish',
    ]);
    expect(await playback, isTrue);

    await tester.pump(const Duration(seconds: 2));
    expect(factoryCalls, 3);
    expect(nativePlayer.events.length, 4);
  });

  testWidgets(
    'does not replace the controller while avatar assets are still loading',
    (tester) async {
      final media = _RealtimeQuestionMediaClient();
      media.release();
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
      final prepareGate = Completer<void>();
      final renderers = <FakeAvatarRenderer>[
        FakeAvatarRenderer(connectOnPrepare: false, prepareGate: prepareGate),
        FakeAvatarRenderer(),
      ];
      final controllers = renderers.map(_avatarController).toList();
      var factoryCalls = 0;
      PracticeAvatarSessionView? sessionView;

      await tester.pumpWidget(
        MaterialApp(
          home: PracticeAvatarSession(
            practiceController: practiceController,
            avatarControllerFactory: () => controllers[factoryCalls++],
            surfaceKey: const Key('test-avatar-surface'),
            builder: (context, avatar) {
              sessionView = avatar;
              return _avatarStage(avatar);
            },
          ),
        ),
      );

      await _pumpUntil(tester, () => renderers.first.preparedGrant != null);
      await tester.pump(const Duration(seconds: 15));
      await tester.pump(const Duration(seconds: 2));

      expect(factoryCalls, 1);
      expect(renderers.first.closeCount, 0);
      expect(renderers.last.preparedGrant, isNull);
      expect(sessionView?.surfaceVisible, isFalse);
      expect(find.byKey(const Key('static-fallback')), findsOneWidget);
      expect(find.text('正在重新连接情景角色'), findsNothing);
      expect(find.text('画面暂不可用，语音仍可继续'), findsOneWidget);

      prepareGate.complete();
      await _pumpUntil(
        tester,
        () =>
            renderers.first.state.connection ==
            AvatarRendererConnection.surfaceReady,
      );
      renderers.first.emit(
        const AvatarRendererState(
          connection: AvatarRendererConnection.connected,
        ),
      );
      await tester.pump();

      expect(factoryCalls, 1);
      expect(sessionView?.surfaceVisible, isTrue);
      expect(find.byKey(const Key('static-fallback')), findsNothing);
    },
  );

  testWidgets('retries connected readiness timeout exactly three times', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final renderers = List<FakeAvatarRenderer>.generate(
      3,
      (_) => FakeAvatarRenderer(connectOnPrepare: false),
    );
    final controllers = renderers.map(_avatarController).toList();
    var factoryCalls = 0;
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => controllers[factoryCalls++],
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );

    for (var attempt = 0; attempt < 3; attempt++) {
      await _pumpUntil(tester, () => renderers[attempt].preparedGrant != null);
      await tester.pump(const Duration(seconds: 15));
      await tester.pump();

      if (attempt < 2) {
        expect(find.text('正在重新连接情景角色'), findsOneWidget);
        expect(find.text('画面暂不可用，语音仍可继续'), findsNothing);
        await tester.pump(Duration(milliseconds: 1000 + attempt * 500));
        await _flushControllerReplacement(tester);
      }
    }

    expect(factoryCalls, 3);
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(find.text('正在重新连接情景角色'), findsNothing);
    expect(find.text('画面暂不可用，语音仍可继续'), findsOneWidget);

    await tester.pump(const Duration(seconds: 2));
    expect(factoryCalls, 3);
  });

  testWidgets('reveals only the connected replacement after a retry succeeds', (
    tester,
  ) async {
    final media = _RealtimeQuestionMediaClient();
    media.release();
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
    final firstRenderer = FakeAvatarRenderer();
    final secondRenderer = FakeAvatarRenderer();
    final controllers = <AvatarController>[
      _avatarController(firstRenderer),
      _avatarController(secondRenderer),
    ];
    var factoryCalls = 0;
    PracticeAvatarSessionView? sessionView;

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarSession(
          practiceController: practiceController,
          avatarControllerFactory: () => controllers[factoryCalls++],
          surfaceKey: const Key('test-avatar-surface'),
          builder: (context, avatar) {
            sessionView = avatar;
            return _avatarStage(avatar);
          },
        ),
      ),
    );
    await _pumpUntil(tester, () => sessionView?.surfaceVisible ?? false);
    await tester.pump();
    expect(sessionView?.surfaceVisible, isTrue);
    expect(find.byKey(const Key('static-fallback')), findsNothing);

    firstRenderer.emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump();
    expect(sessionView?.surfaceVisible, isFalse);
    expect(find.byKey(const Key('static-fallback')), findsOneWidget);
    expect(find.text('正在重新连接情景角色'), findsOneWidget);

    await tester.pump(const Duration(seconds: 1));
    await _flushControllerReplacement(tester);
    await _pumpUntil(tester, () => secondRenderer.preparedGrant != null);
    await tester.pump();

    expect(factoryCalls, 2);
    expect(firstRenderer.closeCount, 1);
    expect(sessionView?.surfaceVisible, isTrue);
    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.byKey(const Key('static-fallback')), findsNothing);
    expect(find.text('正在重新连接情景角色'), findsNothing);
    expect(find.text('画面暂不可用，语音仍可继续'), findsNothing);
    expect(nativePlayer.events, isEmpty);
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

AvatarController _avatarController(FakeAvatarRenderer renderer) {
  return AvatarController(
    renderer: renderer,
    tokenClient: FakeAvatarSessionTokenClient(),
    fallbackPlayback: (_) async {},
    fallbackStop: () async {},
    delay: (_) async {},
  );
}

Widget _avatarStage(PracticeAvatarSessionView avatar) {
  return PracticeRoleStage(
    title: 'Test role',
    fallback: const ColoredBox(
      key: Key('static-fallback'),
      color: Colors.black,
    ),
    surfaceBuilder: avatar.surfaceBuilder,
    surfaceVisible: avatar.surfaceVisible,
    statusLabel: avatar.statusLabel,
    onExit: _doNothing,
  );
}

void _doNothing() {}

final class _SurfaceLifecycleProbe extends StatefulWidget {
  const _SurfaceLifecycleProbe({
    required this.id,
    required this.events,
    super.key,
  });

  final String id;
  final List<String> events;

  @override
  State<_SurfaceLifecycleProbe> createState() => _SurfaceLifecycleProbeState();
}

final class _SurfaceLifecycleProbeState extends State<_SurfaceLifecycleProbe> {
  @override
  void initState() {
    super.initState();
    widget.events.add('init:${widget.id}');
  }

  @override
  Widget build(BuildContext context) => const SizedBox.expand();

  @override
  void dispose() {
    widget.events.add('dispose:${widget.id}');
    super.dispose();
  }
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

Future<void> _flushControllerReplacement(WidgetTester tester) async {
  await tester.runAsync(() => Future<void>.delayed(Duration.zero));
  await tester.pump();
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
