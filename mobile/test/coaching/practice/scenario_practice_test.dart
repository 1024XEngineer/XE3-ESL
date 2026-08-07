import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_client.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_disclosure.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('shows an injected avatar above the live conversation', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _scenarioController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          avatarStatusLabel: '画面已连接',
          avatarSurfaceBuilder: (_) => const ColoredBox(
            key: Key('test-avatar-surface'),
            color: Colors.green,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const Key('scenario-practice-page')), findsOneWidget);
    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.text('画面已连接'), findsOneWidget);
    expect(find.byKey(const Key('scenario-live-subtitle')), findsOneWidget);
    expect(
      find.byKey(const Key('scenario-toggle-conversation-text')),
      findsNothing,
    );
    expect(
      tester.getSize(find.byKey(const Key('scenario-avatar-region'))).height,
      closeTo(844 * 0.44, 0.1),
    );
    expect(find.byKey(const Key('scenario-conversation-history')), findsOne);
    expect(find.textContaining('评分'), findsNothing);
    expect(find.text('翻译'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('keeps scenario text and avatar subtitle visible', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    final question = controller.currentQuestion!.text;
    final questionMessage = controller.practiceMessages.last;

    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    expect(
      find.byKey(const Key('scenario-toggle-conversation-text')),
      findsNothing,
    );
    expect(find.byKey(const Key('scenario-live-subtitle')), findsOneWidget);
    expect(find.text(question), findsNWidgets(2));
    final messageBubble = find.byKey(
      Key('practice-message-${questionMessage.id}'),
    );
    expect(messageBubble, findsOneWidget);
    expect(
      (tester.widget<Container>(messageBubble).decoration! as BoxDecoration)
          .color,
      Colors.transparent,
    );
    expect(find.byKey(const Key('scenario-record')).hitTestable(), findsOne);
    expect(tester.takeException(), isNull);
  });

  testWidgets('hides round progress and lets open scenarios finish manually', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final scene = testScene(
      id: 'daily-open-practice',
      experience: PracticeExperience.lifeAndTravel,
      category: SceneCategory.lifeTravel,
      name: 'Open travel practice',
    );
    final controller = PracticeController(
      client: FakePracticeClient(
        practiceExperience: scene.experience,
        sceneCategory: scene.category,
        completionMode: PracticeCompletionMode.userControlled,
        turnLimit: 0,
      ),
    );
    addTearDown(controller.dispose);
    await controller.activateCreatedPractice(
      scene: scene,
      sessionId: 'session-open-scenario',
      planId: testPracticePlanId('session-open-scenario'),
      practiceMode: scene.practiceOptions.first.mode,
      turnLimit: 0,
      clientOperationId: 'activate-open-scenario',
    );
    await controller.submitPracticeText('I would like to check in.');

    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    expect(find.byKey(const Key('scenario-turn-progress')), findsNothing);
    expect(find.byKey(const Key('scenario-complete-practice')), findsOneWidget);

    await tester.tap(find.byKey(const Key('scenario-complete-practice')));
    await tester.pumpAndSettle();
    expect(find.text('结束练习并复盘？'), findsOneWidget);
    await tester.tap(find.byKey(const Key('scenario-confirm-completion')));
    await tester.pumpAndSettle();

    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(controller.currentQuestion, isNull);
    expect(tester.takeException(), isNull);
  });

  testWidgets('hides the duplicate avatar subtitle for interviews', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _scenarioController(
      selectedScene: testScenes.first,
    );
    addTearDown(controller.dispose);
    final question = controller.currentQuestion!.text;

    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    expect(find.byKey(const Key('scenario-avatar-region')), findsOneWidget);
    expect(find.byKey(const Key('scenario-live-subtitle')), findsNothing);
    expect(find.text(question), findsOneWidget);
    expect(find.byKey(const Key('scenario-conversation-history')), findsOne);
    expect(find.byKey(const Key('scenario-record')).hitTestable(), findsOne);
    expect(tester.takeException(), isNull);
  });

  testWidgets('translates a scenario question once and toggles the read aid', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final practice = _TranslationPracticeClient();
    final controller = await _scenarioController(practiceClient: practice);
    addTearDown(controller.dispose);
    final question = controller.currentQuestion!;

    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    final button = find.byKey(
      Key('practice-assistant-translate-${question.id}'),
    );
    expect(button, findsOneWidget);

    await tester.tap(button);
    await tester.pumpAndSettle();

    expect(find.text(practice.translation), findsOneWidget);
    expect(practice.translationCalls, 1);
    expect(controller.completedTurns, 0);

    await tester.tap(button);
    await tester.pump();
    expect(find.text(practice.translation), findsNothing);

    await tester.tap(button);
    await tester.pump();
    expect(find.text(practice.translation), findsOneWidget);
    expect(practice.translationCalls, 1);
    expect(tester.takeException(), isNull);
  });

  testWidgets('shows capability-gated Tips without blocking the composer', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _scenarioController(
      practiceClient: _QuestionTipPracticeClient(),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    expect(find.byKey(const Key('scenario-question-tip')), findsOneWidget);
    await tester.tap(find.byKey(const Key('scenario-question-tip')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-question-tip-card')), findsOneWidget);
    expect(
      find.text('I would describe the situation and my specific role.'),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('scenario-record')).hitTestable(),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('removes the avatar surface before leaving practice', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            key: const Key('open-scenario-practice'),
            onPressed: () => Navigator.of(context).push<void>(
              MaterialPageRoute<void>(
                builder: (_) => ScenarioPracticePage(
                  practiceController: controller,
                  avatarSurfaceBuilder: (_) => const ColoredBox(
                    key: Key('test-avatar-surface'),
                    color: Colors.green,
                  ),
                  onExitRequested: () async => true,
                ),
              ),
            ),
            child: const Text('Open'),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('open-scenario-practice')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    await tester.tap(find.byKey(const Key('scenario-exit')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('scenario-practice-page')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('keeps the existing typed-answer flow in the scenario shell', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    var interruptedBeforeSubmit = false;
    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onBeforeSubmitText: () async {
            interruptedBeforeSubmit = true;
          },
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byKey(const Key('scenario-open-keyboard')));
    await tester.pump();
    const answer = 'Could I change my reservation to tomorrow morning?';
    await tester.enterText(
      find.byKey(const Key('scenario-text-answer')),
      answer,
    );
    await tester.pump();
    expect(
      tester
          .widget<IconButton>(find.byKey(const Key('scenario-submit-text')))
          .onPressed,
      isNotNull,
    );
    await tester.tap(find.byKey(const Key('scenario-submit-text')));
    await tester.pumpAndSettle();

    expect(
      controller.practiceMessages.any(
        (message) =>
            message.role == PracticeMessageRole.user && message.text == answer,
      ),
      isTrue,
    );
    expect(find.text(answer), findsOneWidget);
    expect(controller.completedTurns, 1);
    expect(interruptedBeforeSubmit, isTrue);
  });

  testWidgets('keeps a follow-up in the current displayed scenario round', (
    tester,
  ) async {
    final controller = await _scenarioController(
      practiceClient: _AsyncReviewPracticeClient(
        practiceExperience: PracticeExperience.lifeAndTravel,
        sceneCategory: SceneCategory.lifeTravel,
        followUpAfterAnswer: true,
      ),
    );
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    expect(find.text('第 1 轮 · 共 3 轮'), findsOneWidget);
    await tester.tap(find.byKey(const Key('scenario-open-keyboard')));
    await tester.pump();
    await tester.enterText(
      find.byKey(const Key('scenario-text-answer')),
      'I led the API redesign for our checkout flow.',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('scenario-submit-text')));
    await tester.pumpAndSettle();

    expect(controller.completedTurns, 1);
    expect(controller.currentQuestion?.isFollowUp, isTrue);
    expect(find.text('第 1 轮 · 共 3 轮'), findsOneWidget);
  });

  testWidgets('bounds avatar interruption before opening the microphone', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    final neverCompletes = Completer<void>();
    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onBeforeStartRecording: () => neverCompletes.future,
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('scenario-record')));
    await tester.pump(const Duration(milliseconds: 499));
    expect(controller.recordingState, PracticeRecordingState.idle);

    await tester.pump(const Duration(milliseconds: 2));
    await tester.pump();
    expect(controller.recordingState, PracticeRecordingState.recording);
    await controller.cancelRecording();
  });

  testWidgets('interrupts the avatar before tap and hold recording starts', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    final tapInterrupt = Completer<void>();
    var interruptCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onBeforeStartRecording: () {
            interruptCalls++;
            return tapInterrupt.future;
          },
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('scenario-record')));
    await tester.pump();
    expect(interruptCalls, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);

    tapInterrupt.complete();
    await tester.pumpAndSettle();
    expect(controller.recordingState, PracticeRecordingState.recording);
    await tester.tap(find.byKey(const Key('scenario-record')));
    await tester.pumpAndSettle();
    controller.rerecord();
    await tester.pump();

    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onBeforeStartRecording: () async {
            interruptCalls++;
          },
        ),
      ),
    );
    final hold = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('scenario-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    expect(interruptCalls, 2);
    expect(controller.recordingState, PracticeRecordingState.recording);
    await hold.up();
    await tester.pumpAndSettle();
    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(controller.completedTurns, 2);
  });

  testWidgets('supports send and upward cancel', (tester) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );

    final send = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('scenario-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    expect(find.textContaining('上滑取消'), findsOneWidget);
    expect(
      tester.getSize(find.byKey(const Key('scenario-stop-recording'))).height,
      48,
    );
    expect(find.byKey(const Key('scenario-voice-targets')), findsNothing);
    await send.up();
    await tester.pumpAndSettle();
    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);
    final userMessage = controller.practiceMessages.lastWhere(
      (message) => message.role == PracticeMessageRole.user,
    );
    final userBubble = find.byKey(Key('practice-message-${userMessage.id}'));
    expect(
      (tester.widget<Container>(userBubble).decoration! as BoxDecoration).color,
      SpeakUpDesign.primaryMuted,
    );

    final userTurnsAfterSend = controller.practiceMessages
        .where((message) => message.role == PracticeMessageRole.user)
        .length;
    final cancel = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('scenario-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await cancel.moveBy(const Offset(0, -80));
    await tester.pump();
    await cancel.up();
    await tester.pumpAndSettle();
    expect(controller.completedTurns, 1);
    expect(
      controller.practiceMessages
          .where((message) => message.role == PracticeMessageRole.user)
          .length,
      userTurnsAfterSend,
    );

    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);
  });

  testWidgets('keeps failed transcription audio for retry or deletion', (
    tester,
  ) async {
    final practiceClient = _FailOncePracticeClient();
    final controller = await _scenarioController(
      practiceClient: practiceClient,
    );
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );

    final send = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('scenario-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await send.up();
    await tester.pumpAndSettle();

    expect(controller.hasPendingPracticeAudio, isTrue);
    expect(find.byKey(const Key('scenario-pending-audio')), findsOneWidget);
    expect(
      find.byKey(const Key('scenario-retry-transcription')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('scenario-delete-pending-audio')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('scenario-delete-pending-audio')));
    await tester.pumpAndSettle();
    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(find.byKey(const Key('scenario-record')), findsOneWidget);
  });

  testWidgets(
    'keeps pending feedback silent without breaking the composer row',
    (tester) async {
      final controller = await _scenarioController(
        practiceClient: _AsyncReviewPracticeClient(),
      );
      addTearDown(controller.dispose);
      for (var turn = 0; turn < 3; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }
      expect(controller.recordingState, PracticeRecordingState.completed);

      final feedbackController = SpeechFeedbackController(
        client: _PendingSpeechFeedbackClient(),
      );
      addTearDown(feedbackController.dispose);
      await tester.pumpWidget(
        MaterialApp(
          theme: SpeakUpTheme.light,
          home: ScenarioPracticePage(
            practiceController: controller,
            speechFeedbackController: feedbackController,
          ),
        ),
      );
      await tester.pump();

      expect(tester.takeException(), isNull);
      expect(
        find.byKey(const Key('speech-feedback-loading-indicator')),
        findsNothing,
      );
      expect(find.byType(SpeechFeedbackDisclosure), findsNothing);
      expect(find.text('正在生成评分与纠错…'), findsNothing);
      expect(find.text('完成并返回'), findsOneWidget);
    },
  );

  testWidgets('hands completed scenario to the generic completion callback', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    for (var turn = 0; turn < 3; turn++) {
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }
    expect(controller.recordingState, PracticeRecordingState.completed);

    var completionCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onPracticeCompleted: () async {
            completionCalls++;
            return false;
          },
        ),
      ),
    );
    await tester.tap(find.text('完成并返回'));
    await tester.pump();

    expect(completionCalls, 1);
    expect(find.text('练习正在完成，请稍后重试。'), findsOneWidget);
  });

  testWidgets('prefers the injected avatar replay action over audio playback', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    var replayCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onReplayQuestion: () async {
            replayCalls++;
          },
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('scenario-replay-question')));
    await tester.pump();

    expect(replayCalls, 1);
  });

  testWidgets('disables injected replay while the microphone is recording', (
    tester,
  ) async {
    final controller = await _scenarioController();
    addTearDown(controller.dispose);
    await controller.startRecording();
    await tester.pumpWidget(
      MaterialApp(
        home: ScenarioPracticePage(
          practiceController: controller,
          onReplayQuestion: () async {},
        ),
      ),
    );

    final button = tester.widget<IconButton>(
      find.byKey(const Key('scenario-replay-question')),
    );
    expect(button.onPressed, isNull);
    await controller.cancelRecording();
  });

  testWidgets('fits a compact phone with large text and landscape layout', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _scenarioController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 568),
            textScaler: TextScaler.linear(2),
          ),
          child: ScenarioPracticePage(practiceController: controller),
        ),
      ),
    );
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.byKey(const Key('scenario-record')), findsOneWidget);

    tester.view.physicalSize = const Size(844, 390);
    await tester.pumpWidget(
      MaterialApp(home: ScenarioPracticePage(practiceController: controller)),
    );
    await tester.pump();

    expect(
      tester.getSize(find.byKey(const Key('scenario-avatar-region'))).width,
      closeTo(844 * 0.44, 0.1),
    );
    expect(tester.takeException(), isNull);
  });
}

Future<PracticeController> _scenarioController({
  PracticeClient? practiceClient,
  SceneDefinition? selectedScene,
}) async {
  final scene =
      selectedScene ??
      testScene(
        id: 'daily-hotel',
        experience: PracticeExperience.lifeAndTravel,
        category: SceneCategory.lifeTravel,
        name: '酒店入住',
        prompt: const ScenePrompt(
          publicSceneBrief: '练习办理入住与需求沟通。',
          practiceGoal: 'Complete the hotel check-in conversation.',
          userRole: 'Guest',
          aiRole: 'Receptionist',
          personaSummary: 'Professional and helpful.',
          focusAreas: <String>['check_in'],
          turnBlueprints: <String>['Confirm the booking.'],
        ),
      );
  final resolvedPracticeClient =
      practiceClient ??
      _ScenePracticeClient(
        practiceExperience: scene.experience,
        sceneCategory: scene.category,
      );
  final controller = PracticeController(client: resolvedPracticeClient);
  await controller.activateCreatedPractice(
    scene: scene,
    sessionId: _scenarioSessionId,
    planId: 'practice-plan-$_scenarioSessionId',
    practiceMode: scene.practiceOptions.first.mode,
    turnLimit: 3,
    clientOperationId: 'activate-$_scenarioSessionId',
  );
  return controller;
}

final class _ScenePracticeClient implements PracticeClient {
  _ScenePracticeClient({
    required this.practiceExperience,
    required this.sceneCategory,
  });

  final _delegate = FakePracticeClient(capabilities: testPracticeCapabilities);
  final PracticeExperience practiceExperience;
  final SceneCategory sceneCategory;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async => _withSceneIdentity(
    await _delegate.restorePractice(sessionId: sessionId),
    practiceExperience,
    sceneCategory,
  );

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async => _withSceneIdentity(
    await _delegate.activatePractice(
      sessionId: sessionId,
      clientOperationId: clientOperationId,
    ),
    practiceExperience,
    sceneCategory,
  );

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) => _delegate.transcribe(request);

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async => _withSceneIdentityConfirmation(
    await _delegate.confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      idempotencyKey: idempotencyKey,
    ),
    practiceExperience,
    sceneCategory,
  );

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async => _withSceneIdentityConfirmation(
    await _delegate.submitText(
      sessionId: sessionId,
      questionId: questionId,
      answerText: answerText,
      idempotencyKey: idempotencyKey,
    ),
    practiceExperience,
    sceneCategory,
  );
}

final class _TranslationPracticeClient
    implements PracticeClient, PracticeQuestionTranslationClient {
  final _delegate = _AsyncReviewPracticeClient(
    practiceExperience: PracticeExperience.lifeAndTravel,
    sceneCategory: SceneCategory.lifeTravel,
  );

  final String translation = '请介绍一次你解决团队分歧的经历。';
  int translationCalls = 0;

  PracticeExperience get resolvedExperience => _delegate.resolvedExperience;
  SceneCategory get resolvedCategory => _delegate.resolvedCategory;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) => _delegate.restorePractice(sessionId: sessionId);

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) => _delegate.activatePractice(
    sessionId: sessionId,
    clientOperationId: clientOperationId,
  );

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) => _delegate.transcribe(request);

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) => _delegate.confirm(
    sessionId: sessionId,
    questionId: questionId,
    candidateId: candidateId,
    idempotencyKey: idempotencyKey,
  );

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) => _delegate.submitText(
    sessionId: sessionId,
    questionId: questionId,
    answerText: answerText,
    idempotencyKey: idempotencyKey,
  );

  @override
  Future<PracticeQuestionTranslation> translateQuestion({
    required String questionId,
  }) async {
    translationCalls++;
    return PracticeQuestionTranslation(
      questionId: questionId,
      targetLanguage: 'zh-CN',
      content: translation,
    );
  }
}

final class _FailOncePracticeClient implements PracticeClient {
  final _delegate = FakePracticeClient(capabilities: testPracticeCapabilities);
  bool _shouldFail = true;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async => _withSceneIdentity(
    await _delegate.restorePractice(sessionId: sessionId),
    PracticeExperience.lifeAndTravel,
    SceneCategory.lifeTravel,
  );

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async => _withSceneIdentity(
    await _delegate.activatePractice(
      sessionId: sessionId,
      clientOperationId: clientOperationId,
    ),
    PracticeExperience.lifeAndTravel,
    SceneCategory.lifeTravel,
  );

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    if (_shouldFail) {
      _shouldFail = false;
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        retryable: true,
      );
    }
    return _delegate.transcribe(request);
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async => _withSceneIdentityConfirmation(
    await _delegate.confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      idempotencyKey: idempotencyKey,
    ),
    PracticeExperience.lifeAndTravel,
    SceneCategory.lifeTravel,
  );

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async => _withSceneIdentityConfirmation(
    await _delegate.submitText(
      sessionId: sessionId,
      questionId: questionId,
      answerText: answerText,
      idempotencyKey: idempotencyKey,
    ),
    PracticeExperience.lifeAndTravel,
    SceneCategory.lifeTravel,
  );
}

final class _QuestionTipPracticeClient
    implements PracticeClient, PracticeQuestionTipClient {
  final _delegate = FakePracticeClient(
    practiceExperience: PracticeExperience.lifeAndTravel,
    sceneCategory: SceneCategory.lifeTravel,
  );

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) => _delegate.restorePractice(sessionId: sessionId);

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) => _delegate.activatePractice(
    sessionId: sessionId,
    clientOperationId: clientOperationId,
  );

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) => _delegate.transcribe(request);

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) => _delegate.confirm(
    sessionId: sessionId,
    questionId: questionId,
    candidateId: candidateId,
    idempotencyKey: idempotencyKey,
  );

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) => _delegate.submitText(
    sessionId: sessionId,
    questionId: questionId,
    answerText: answerText,
    idempotencyKey: idempotencyKey,
  );

  @override
  Future<PracticeQuestionTip> ensureQuestionTip({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
  }) async => PracticeQuestionTip(
    id: 'tip-1',
    sessionId: sessionId,
    questionId: questionId,
    content: 'I would describe the situation and my specific role.',
    createdAt: DateTime.utc(2026, 8, 3),
  );
}

final class _AsyncReviewPracticeClient implements PracticeClient {
  _AsyncReviewPracticeClient({
    this.practiceExperience,
    this.sceneCategory,
    this.followUpAfterAnswer = false,
  });

  final _delegate = FakePracticeClient(capabilities: testPracticeCapabilities);
  final PracticeExperience? practiceExperience;
  final SceneCategory? sceneCategory;
  final bool followUpAfterAnswer;

  PracticeExperience get resolvedExperience =>
      practiceExperience ?? PracticeExperience.lifeAndTravel;
  SceneCategory get resolvedCategory =>
      sceneCategory ?? SceneCategory.lifeTravel;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async => _withSceneIdentity(
    await _delegate.restorePractice(sessionId: sessionId),
    resolvedExperience,
    resolvedCategory,
  );

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final snapshot = await _delegate.activatePractice(
      sessionId: sessionId,
      clientOperationId: clientOperationId,
    );
    return _withSceneIdentity(snapshot, resolvedExperience, resolvedCategory);
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) => _delegate.transcribe(request);

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    final confirmation = await _delegate.confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      idempotencyKey: idempotencyKey,
    );
    const statusUrl =
        '/v1/speech-feedback/10000000-0000-4000-8000-000000000001';
    return PracticeTurnConfirmation(
      turnId: confirmation.turnId,
      sessionId: confirmation.sessionId,
      questionId: confirmation.questionId,
      candidateId: confirmation.candidateId,
      answer: PracticeMessage(
        id: confirmation.answer.id,
        role: confirmation.answer.role,
        text: confirmation.answer.text,
        speechFeedbackStatusUrl: statusUrl,
      ),
      completedTurns: confirmation.completedTurns,
      turnLimit: confirmation.turnLimit,
      sessionCompleted: confirmation.sessionCompleted,
      practiceExperience: resolvedExperience,
      sceneCategory: resolvedCategory,
      practiceMode: confirmation.practiceMode,
      capabilities: confirmation.capabilities,
      sessionVersion: confirmation.sessionVersion,
      nextQuestion: confirmation.nextQuestion,
      audioAssetId: confirmation.audioAssetId,
      speechFeedbackStatusUrl: statusUrl,
    );
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async {
    final confirmation = await _delegate.submitText(
      sessionId: sessionId,
      questionId: questionId,
      answerText: answerText,
      idempotencyKey: idempotencyKey,
    );
    if (!followUpAfterAnswer || confirmation.nextQuestion == null) {
      return _withSceneIdentityConfirmation(
        confirmation,
        resolvedExperience,
        resolvedCategory,
      );
    }
    final nextQuestion = confirmation.nextQuestion!;
    return PracticeTurnConfirmation(
      turnId: confirmation.turnId,
      sessionId: confirmation.sessionId,
      questionId: confirmation.questionId,
      candidateId: confirmation.candidateId,
      answer: confirmation.answer,
      completedTurns: confirmation.completedTurns,
      turnLimit: confirmation.turnLimit,
      sessionCompleted: confirmation.sessionCompleted,
      practiceExperience: resolvedExperience,
      sceneCategory: resolvedCategory,
      practiceMode: confirmation.practiceMode,
      capabilities: confirmation.capabilities,
      sessionVersion: confirmation.sessionVersion,
      nextQuestion: PracticeQuestion(
        id: nextQuestion.id,
        sessionId: nextQuestion.sessionId,
        text: nextQuestion.text,
        questionType: 'FOLLOW_UP',
        parentQuestionId: questionId,
        speakerParticipantId: nextQuestion.speakerParticipantId,
        addresseeParticipantIds: nextQuestion.addresseeParticipantIds,
        speechPath: nextQuestion.speechPath,
      ),
      audioAssetId: confirmation.audioAssetId,
      speechFeedbackStatusUrl: confirmation.speechFeedbackStatusUrl,
    );
  }
}

PracticeSessionSnapshot _withSceneIdentity(
  PracticeSessionSnapshot snapshot,
  PracticeExperience practiceExperience,
  SceneCategory sceneCategory,
) => PracticeSessionSnapshot(
  sessionId: snapshot.sessionId,
  planId: snapshot.planId,
  practiceExperience: practiceExperience,
  sceneCategory: sceneCategory,
  practiceMode: snapshot.practiceMode,
  capabilities: snapshot.capabilities,
  sessionVersion: snapshot.sessionVersion,
  completedTurns: snapshot.completedTurns,
  turnLimit: snapshot.turnLimit,
  sessionCompleted: snapshot.sessionCompleted,
  currentQuestion: snapshot.currentQuestion,
  currentTurn: snapshot.currentTurn,
  turnHistory: snapshot.turnHistory,
);

PracticeTurnConfirmation _withSceneIdentityConfirmation(
  PracticeTurnConfirmation confirmation,
  PracticeExperience practiceExperience,
  SceneCategory sceneCategory,
) => PracticeTurnConfirmation(
  turnId: confirmation.turnId,
  sessionId: confirmation.sessionId,
  questionId: confirmation.questionId,
  candidateId: confirmation.candidateId,
  answer: confirmation.answer,
  completedTurns: confirmation.completedTurns,
  turnLimit: confirmation.turnLimit,
  sessionCompleted: confirmation.sessionCompleted,
  practiceExperience: practiceExperience,
  sceneCategory: sceneCategory,
  practiceMode: confirmation.practiceMode,
  capabilities: confirmation.capabilities,
  sessionVersion: confirmation.sessionVersion,
  nextQuestion: confirmation.nextQuestion,
  audioAssetId: confirmation.audioAssetId,
  speechFeedbackStatusUrl: confirmation.speechFeedbackStatusUrl,
);

const _scenarioSessionId = 'practice-session-scenario';

final class _PendingSpeechFeedbackClient implements SpeechFeedbackClient {
  final _pending = Completer<SpeechFeedback>();

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) => _pending.future;

  @override
  Future<void> clearAccountState() async {}
}
