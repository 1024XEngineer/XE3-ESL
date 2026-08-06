import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/coaching/roleplay/immersive_roleplay_session.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

import 'avatar/avatar_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('owns one avatar connection and fences app lifecycle', (
    tester,
  ) async {
    final practiceController = await _immersivePracticeController();
    addTearDown(practiceController.dispose);
    final tokenClient = FakeAvatarSessionTokenClient();
    final renderers = <FakeAvatarRenderer>[];
    AvatarController createAvatarController() {
      final renderer = FakeAvatarRenderer();
      renderers.add(renderer);
      return AvatarController(
        renderer: renderer,
        tokenClient: tokenClient,
        fallbackPlayback: (_) async {},
        fallbackStop: () async {},
        delay: (_) async {},
      );
    }

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: createAvatarController,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(tokenClient.requestedSessionIds, [
      practiceController.practiceSessionId,
    ]);
    expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
    expect(find.byKey(const Key('immersive-avatar-surface')), findsOneWidget);

    // Selectable assistant messages register an AppLifecycleListener that
    // validates the full transition chain, so drive each state in order.
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await _pumpUntil(tester, () => renderers.first.closeCount == 1);
    expect(renderers, hasLength(1));
    expect(renderers.first.closeCount, 1);

    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await _pumpUntil(tester, () => renderers.length == 2);
    expect(renderers, hasLength(2));
    expect(tokenClient.requestedSessionIds, hasLength(2));

    final resumedRenderer = renderers.last;
    await tester.pumpWidget(const SizedBox.shrink());
    await _pumpUntil(tester, () => resumedRenderer.closeCount == 1);
    expect(renderers, hasLength(2));
    expect(resumedRenderer.closeCount, 1);
  });

  testWidgets('retries a transient session failure with a fresh renderer', (
    tester,
  ) async {
    final practiceController = await _immersivePracticeController();
    addTearDown(practiceController.dispose);
    final failedTokenClient = FakeAvatarSessionTokenClient(
      error: const AvatarSessionTokenException(
        failure: AvatarSessionTokenFailure.unavailable,
        statusCode: 503,
        retryable: true,
      ),
    );
    final readyTokenClient = FakeAvatarSessionTokenClient();
    final renderers = <FakeAvatarRenderer>[];
    var factoryCalls = 0;

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: () {
            final renderer = FakeAvatarRenderer();
            renderers.add(renderer);
            final tokenClient = factoryCalls++ == 0
                ? failedTokenClient
                : readyTokenClient;
            return AvatarController(
              renderer: renderer,
              tokenClient: tokenClient,
              fallbackPlayback: (_) async {},
              fallbackStop: () async {},
              delay: (_) async {},
            );
          },
        ),
      ),
    );
    await tester.pump();

    expect(failedTokenClient.requestedSessionIds, hasLength(1));
    expect(factoryCalls, 1);

    await tester.pump(const Duration(seconds: 1));
    await _pumpUntil(tester, () => factoryCalls == 2);

    expect(factoryCalls, 2);
    expect(renderers.first.closeCount, 1);
    expect(readyTokenClient.requestedSessionIds, hasLength(1));
  });

  testWidgets('reconnects after a live renderer network failure', (
    tester,
  ) async {
    final practiceController = await _immersivePracticeController();
    addTearDown(practiceController.dispose);
    final tokenClient = FakeAvatarSessionTokenClient();
    final renderers = <FakeAvatarRenderer>[];

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: () {
            final renderer = FakeAvatarRenderer();
            renderers.add(renderer);
            return AvatarController(
              renderer: renderer,
              tokenClient: tokenClient,
              fallbackPlayback: (_) async {},
              fallbackStop: () async {},
              delay: (_) async {},
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    renderers.single.emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump(const Duration(milliseconds: 500));
    await _pumpUntil(tester, () => renderers.length == 2);

    expect(renderers, hasLength(2));
    expect(renderers.first.closeCount, 1);
    expect(tokenClient.requestedSessionIds, hasLength(2));
  });

  testWidgets('falls back when avatar preparation does not finish in time', (
    tester,
  ) async {
    final practiceController = await _immersivePracticeController();
    addTearDown(practiceController.dispose);
    final prepareGate = Completer<void>();
    final renderer = FakeAvatarRenderer(prepareGate: prepareGate);

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: () => AvatarController(
            renderer: renderer,
            tokenClient: FakeAvatarSessionTokenClient(),
            fallbackPlayback: (_) async {},
            fallbackStop: () async {},
            delay: (_) async {},
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('正在准备情景角色'), findsOneWidget);

    await tester.pump(const Duration(seconds: 15));
    await tester.pump();

    expect(find.text('画面暂不可用，语音仍可继续'), findsOneWidget);

    prepareGate.complete();
    await tester.pumpAndSettle();
  });

  testWidgets('waits for a retry before loading assistant fallback audio', (
    tester,
  ) async {
    final scene = _dailyTravelScene('daily-retry');
    final snapshot = PracticeSessionSnapshot(
      sessionId: 'session-retry-1',
      planId: 'plan-retry-1',
      practiceExperience: scene.experience,
      sceneCategory: scene.category,
      practiceMode: PracticeMode.fullSimulation,
      capabilities: testPracticeCapabilities,
      sessionVersion: 1,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: const PracticeQuestion(
        id: 'question-retry-1',
        sessionId: 'session-retry-1',
        text: 'Where would you like to go?',
        speechPath: '/v1/voice-questions/question-retry-1/speech',
      ),
    );
    final media = _QuestionMediaClient();
    final fallbackPlayer = _FallbackPlayer();
    final practiceController = PracticeController(
      client: _SnapshotPracticeClient(snapshot),
      mediaClient: media,
      audioPlayer: fallbackPlayer,
    );
    addTearDown(practiceController.dispose);
    await _activateCreatedPractice(practiceController, scene, snapshot);
    final failedTokenClient = FakeAvatarSessionTokenClient(
      error: const AvatarSessionTokenException(
        failure: AvatarSessionTokenFailure.unavailable,
        statusCode: 503,
        retryable: true,
      ),
    );
    final readyTokenClient = FakeAvatarSessionTokenClient();
    final renderers = <FakeAvatarRenderer>[];

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: () {
            final renderer = FakeAvatarRenderer();
            renderers.add(renderer);
            return AvatarController(
              renderer: renderer,
              tokenClient: renderers.length == 1
                  ? failedTokenClient
                  : readyTokenClient,
              fallbackPlayback: fallbackPlayer.playWav,
              fallbackStop: fallbackPlayer.stop,
              delay: (_) async {},
            );
          },
        ),
      ),
    );
    await tester.pump();

    expect(media.questionLoadCount, 0);
    expect(fallbackPlayer.playCount, 0);

    await tester.pump(const Duration(seconds: 1));
    await _pumpUntil(
      tester,
      () => renderers.length == 2 && media.questionLoadCount == 1,
    );

    expect(renderers.last.sends, hasLength(1));
    expect(fallbackPlayer.playCount, 0);
  });

  testWidgets('caps repeated surface connection failures at three attempts', (
    tester,
  ) async {
    final practiceController = await _immersivePracticeController();
    addTearDown(practiceController.dispose);
    final tokenClient = FakeAvatarSessionTokenClient();
    final renderers = <FakeAvatarRenderer>[];

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: () {
            final renderer = FakeAvatarRenderer(connectOnPrepare: false);
            renderers.add(renderer);
            return AvatarController(
              renderer: renderer,
              tokenClient: tokenClient,
              fallbackPlayback: (_) async {},
              fallbackStop: () async {},
              delay: (_) async {},
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    renderers[0].emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump(const Duration(seconds: 1));
    await _pumpUntil(tester, () => renderers.length == 2);

    renderers[1].emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump(const Duration(milliseconds: 1500));
    await _pumpUntil(tester, () => renderers.length == 3);

    renderers[2].emit(
      const AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: AvatarRendererFailure.network,
      ),
    );
    await tester.pump(const Duration(seconds: 3));
    await tester.runAsync(() => Future<void>.delayed(Duration.zero));

    expect(renderers, hasLength(3));
    expect(tokenClient.requestedSessionIds, hasLength(3));
  });

  testWidgets('routes an immersive scene through the avatar coordinator', (
    tester,
  ) async {
    final practiceController = await _immersivePracticeController();
    addTearDown(practiceController.dispose);
    final renderer = FakeAvatarRenderer();
    final tokenClient = FakeAvatarSessionTokenClient();
    var factoryCalls = 0;

    await tester.pumpWidget(
      SpeakUpApp.preview(
        practiceController: practiceController,
        avatarControllerFactory: () {
          factoryCalls++;
          return AvatarController(
            renderer: renderer,
            tokenClient: tokenClient,
            fallbackPlayback: (_) async {},
            fallbackStop: () async {},
            delay: (_) async {},
          );
        },
      ),
    );
    await tester.pumpAndSettle();

    Navigator.of(
      tester.element(find.byType(SpeakUpShell)),
    ).pushNamed(AppRoutes.practice);
    await tester.pumpAndSettle();

    expect(factoryCalls, 1);
    expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
    expect(find.byKey(const Key('practice-page')), findsNothing);
  });

  testWidgets('loads each assistant WAV once and sends it only to the avatar', (
    tester,
  ) async {
    final scene = _dailyTravelScene('daily-travel');
    final snapshot = PracticeSessionSnapshot(
      sessionId: 'session-avatar-1',
      planId: 'plan-avatar-1',
      practiceExperience: scene.experience,
      sceneCategory: scene.category,
      practiceMode: PracticeMode.fullSimulation,
      capabilities: testPracticeCapabilities,
      sessionVersion: 1,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: const PracticeQuestion(
        id: 'question-avatar-1',
        sessionId: 'session-avatar-1',
        text: 'Where would you like to go?',
        speechPath: '/v1/voice-questions/question-avatar-1/speech',
      ),
    );
    final media = _QuestionMediaClient();
    final fallbackPlayer = _FallbackPlayer();
    final practiceController = PracticeController(
      client: _SnapshotPracticeClient(snapshot),
      mediaClient: media,
      audioPlayer: fallbackPlayer,
    );
    addTearDown(practiceController.dispose);
    await _activateCreatedPractice(practiceController, scene, snapshot);
    final renderer = FakeAvatarRenderer();
    final avatarController = AvatarController(
      renderer: renderer,
      tokenClient: FakeAvatarSessionTokenClient(),
      fallbackPlayback: fallbackPlayer.playWav,
      fallbackStop: fallbackPlayer.stop,
      delay: (_) async {},
    );

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplaySession(
          practiceController: practiceController,
          avatarControllerFactory: () => avatarController,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(media.questionLoadCount, 1);
    expect(renderer.sends, hasLength(1));
    expect(renderer.sends.single.end, isTrue);
    expect(fallbackPlayer.playCount, 0);
  });
}

Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var attempts = 0; attempts < 50; attempts += 1) {
    if (condition()) {
      return;
    }
    await tester.runAsync(() => Future<void>.delayed(Duration.zero));
    await tester.pump(const Duration(milliseconds: 1));
  }
  fail('Condition was not reached.');
}

Future<PracticeController> _immersivePracticeController() async {
  final scene = _dailyTravelScene('daily-travel');
  const sessionId = 'session-daily-travel';
  final snapshot = PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: 'plan-daily-travel',
    practiceExperience: scene.experience,
    sceneCategory: scene.category,
    practiceMode: PracticeMode.fullSimulation,
    capabilities: testPracticeCapabilities,
    sessionVersion: 1,
    completedTurns: 0,
    turnLimit: 3,
    sessionCompleted: false,
    currentQuestion: const PracticeQuestion(
      id: 'question-daily-travel-1',
      sessionId: sessionId,
      text: 'Where would you like to go?',
    ),
  );
  final controller = PracticeController(
    client: _SnapshotPracticeClient(snapshot),
  );
  await _activateCreatedPractice(controller, scene, snapshot);
  return controller;
}

Future<void> _activateCreatedPractice(
  PracticeController controller,
  SceneDefinition scene,
  PracticeSessionSnapshot snapshot,
) => controller.activateCreatedPractice(
  scene: scene,
  sessionId: snapshot.sessionId,
  planId: snapshot.planId,
  practiceMode: snapshot.practiceMode,
  turnLimit: snapshot.turnLimit,
  clientOperationId: 'activate-${snapshot.sessionId}',
);

SceneDefinition _dailyTravelScene(String id) => testScene(
  id: id,
  experience: PracticeExperience.lifeAndTravel,
  category: SceneCategory.lifeDaily,
  name: '旅行对话',
  prompt: const ScenePrompt(
    publicSceneBrief: '练习旅行中的真实交流。',
    practiceGoal: 'Complete the travel conversation.',
    userRole: 'Traveler',
    aiRole: 'Conversation partner',
    personaSummary: 'Helpful and natural.',
    focusAreas: <String>['clarity'],
    turnBlueprints: <String>['Ask one travel question.'],
  ),
);

final class _SnapshotPracticeClient implements PracticeClient {
  _SnapshotPracticeClient(this.snapshot);

  final PracticeSessionSnapshot snapshot;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    if (sessionId != snapshot.sessionId) {
      throw StateError('Unexpected Practice Session.');
    }
    return snapshot;
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    if (sessionId != snapshot.sessionId || clientOperationId.trim().isEmpty) {
      throw StateError('Unexpected Practice activation.');
    }
    return snapshot;
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    throw UnimplementedError();
  }
}

final class _QuestionMediaClient implements PracticeMediaClient {
  int questionLoadCount = 0;

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async {
    questionLoadCount++;
    return buildPcmWave(pcm: Uint8List(48000));
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> deleteRecording(String audioAssetId) async {}

  @override
  Future<void> dispose() async {}

  @override
  Future<Uint8List> loadRecording(String audioAssetId) {
    throw UnimplementedError();
  }
}

final class _FallbackPlayer implements PracticeAudioPlayer {
  final StreamController<void> _completions =
      StreamController<void>.broadcast();
  int playCount = 0;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playWav(Uint8List bytes) async {
    playCount++;
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() => _completions.close();
}
