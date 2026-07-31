import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/practice/immersive_roleplay.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_client.dart';
import 'package:speakup/review/interview_report_controller.dart';
import 'package:speakup/review/turn_feedback.dart';
import 'package:speakup/review/turn_feedback_client.dart';
import 'package:speakup/review/turn_feedback_controller.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('shows an injected avatar above the live conversation', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _roleplayController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          avatarStatusLabel: '画面已连接',
          avatarSurfaceBuilder: (_) => const ColoredBox(
            key: Key('test-avatar-surface'),
            color: Colors.green,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.text('画面已连接'), findsOneWidget);
    expect(find.byKey(const Key('immersive-live-subtitle')), findsOneWidget);
    expect(
      tester.getSize(find.byKey(const Key('immersive-avatar-region'))).height,
      closeTo(844 * 0.44, 0.1),
    );
    expect(find.byKey(const Key('immersive-conversation-history')), findsOne);
    expect(find.textContaining('评分'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('removes the avatar surface before leaving practice', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            key: const Key('open-immersive-practice'),
            onPressed: () => Navigator.of(context).push<void>(
              MaterialPageRoute<void>(
                builder: (_) => ImmersiveRoleplayPage(
                  agentController: controller,
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
    await tester.tap(find.byKey(const Key('open-immersive-practice')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    await tester.tap(find.byKey(const Key('immersive-exit')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('immersive-roleplay-page')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('keeps the existing typed-answer flow in the immersive shell', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    var interruptedBeforeSubmit = false;
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeSubmitText: () async {
            interruptedBeforeSubmit = true;
          },
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byKey(const Key('immersive-open-keyboard')));
    await tester.pump();
    const answer = 'Could I change my reservation to tomorrow morning?';
    await tester.enterText(
      find.byKey(const Key('immersive-text-answer')),
      answer,
    );
    await tester.tap(find.byKey(const Key('immersive-submit-text')));
    await tester.pumpAndSettle();

    expect(
      controller.messages.any(
        (message) =>
            message.role == AgentMessageRole.user && message.text == answer,
      ),
      isTrue,
    );
    expect(find.text(answer), findsOneWidget);
    expect(controller.completedTurns, 1);
    expect(interruptedBeforeSubmit, isTrue);
  });

  testWidgets('bounds avatar interruption before opening the microphone', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    final neverCompletes = Completer<void>();
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeStartRecording: () => neverCompletes.future,
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('immersive-record')));
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
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    final tapInterrupt = Completer<void>();
    var interruptCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeStartRecording: () {
            interruptCalls++;
            return tapInterrupt.future;
          },
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('immersive-record')));
    await tester.pump();
    expect(interruptCalls, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);

    tapInterrupt.complete();
    await tester.pumpAndSettle();
    expect(controller.recordingState, PracticeRecordingState.recording);
    await tester.tap(find.byKey(const Key('immersive-stop-recording')));
    await tester.pumpAndSettle();
    controller.rerecord();
    await tester.pump();

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeStartRecording: () async {
            interruptCalls++;
          },
        ),
      ),
    );
    final hold = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    expect(interruptCalls, 2);
    expect(controller.recordingState, PracticeRecordingState.recording);
    await hold.up();
    await tester.pumpAndSettle();
    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(controller.completedTurns, 2);
  });

  testWidgets('supports send, left cancel, and right editable text release', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ImmersiveRoleplayPage(agentController: controller)),
    );

    final send = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    expect(find.byKey(const Key('immersive-voice-targets')), findsOneWidget);
    await send.up();
    await tester.pumpAndSettle();
    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);

    final userTurnsAfterSend = controller.messages
        .where((message) => message.role == AgentMessageRole.user)
        .length;
    final cancel = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await cancel.moveBy(const Offset(-80, 0));
    await tester.pump();
    expect(find.text('松开取消'), findsWidgets);
    await cancel.up();
    await tester.pumpAndSettle();
    expect(controller.completedTurns, 1);
    expect(
      controller.messages
          .where((message) => message.role == AgentMessageRole.user)
          .length,
      userTurnsAfterSend,
    );

    final convert = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await convert.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(find.text('松开转成文字'), findsWidgets);
    await convert.up();
    await tester.pumpAndSettle();

    final textField = tester.widget<TextField>(
      find.byKey(const Key('immersive-text-answer')),
    );
    expect(
      textField.controller?.text,
      'The main trade-off was delivery speed versus reliability, so I reduced the scope first.',
    );
    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);
  });

  testWidgets('keeps failed transcription audio for retry or deletion', (
    tester,
  ) async {
    final practiceClient = _FailOncePracticeClient();
    final controller = await _roleplayController(
      practiceClient: practiceClient,
    );
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ImmersiveRoleplayPage(agentController: controller)),
    );

    final send = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await send.up();
    await tester.pumpAndSettle();

    expect(controller.hasPendingPracticeAudio, isTrue);
    expect(find.byKey(const Key('immersive-pending-audio')), findsOneWidget);
    expect(
      find.byKey(const Key('immersive-retry-transcription')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('immersive-delete-pending-audio')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('immersive-delete-pending-audio')));
    await tester.pumpAndSettle();
    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(find.byKey(const Key('immersive-record')), findsOneWidget);
  });

  testWidgets('shows asynchronous scoring without breaking the composer row', (
    tester,
  ) async {
    final controller = await _roleplayController(
      practiceClient: _AsyncReviewPracticeClient(),
    );
    addTearDown(controller.dispose);
    for (var turn = 0; turn < 3; turn++) {
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }
    expect(controller.recordingState, PracticeRecordingState.reviewFailed);

    final feedbackController = SpeechFeedbackController(
      client: _PendingSpeechFeedbackClient(),
    );
    addTearDown(feedbackController.dispose);
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: ImmersiveRoleplayPage(
          agentController: controller,
          speechFeedbackController: feedbackController,
        ),
      ),
    );
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(
      find.byKey(const Key('speech-feedback-loading-indicator')),
      findsNWidgets(3),
    );
    expect(find.text('正在生成评分与纠错…'), findsNWidgets(3));
    expect(find.text('重试'), findsOneWidget);
  });

  testWidgets('keeps the final turn visible until report is requested', (
    tester,
  ) async {
    final controller = await _roleplayController(
      practiceClient: _AsyncReviewPracticeClient(
        scenarioType: 'INTERVIEW',
        scenarioModel: 'INTERVIEW_BASIC_DIALOGUE',
      ),
    );
    addTearDown(controller.dispose);
    for (var turn = 0; turn < 3; turn++) {
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }
    expect(controller.recordingState, PracticeRecordingState.completed);

    final reportController = InterviewReportController(
      client: _PendingInterviewReportClient(),
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(reportController.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          interviewReportController: reportController,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(find.byKey(const Key('interview-report-page')), findsNothing);
    expect(find.text('查看报告'), findsOneWidget);
    expect(
      find.byKey(const Key('immersive-conversation-history')),
      findsOneWidget,
    );

    await tester.tap(find.text('查看报告'));
    await tester.pump();
    await tester.pump();

    expect(find.byKey(const Key('interview-report-page')), findsOneWidget);
    expect(
      find.byKey(const Key('interview-report-generating')),
      findsOneWidget,
    );
    expect(reportController.practiceSessionId, controller.practiceSessionId);
  });

  testWidgets('prefers the injected avatar replay action over audio playback', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    var replayCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onReplayQuestion: () async {
            replayCalls++;
          },
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('immersive-replay-question')));
    await tester.pump();

    expect(replayCalls, 1);
  });

  testWidgets('disables injected replay while the microphone is recording', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    await controller.startRecording();
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onReplayQuestion: () async {},
        ),
      ),
    );

    final button = tester.widget<IconButton>(
      find.byKey(const Key('immersive-replay-question')),
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
    final controller = await _roleplayController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 568),
            textScaler: TextScaler.linear(2),
          ),
          child: ImmersiveRoleplayPage(agentController: controller),
        ),
      ),
    );
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.byKey(const Key('immersive-record')), findsOneWidget);

    tester.view.physicalSize = const Size(844, 390);
    await tester.pumpWidget(
      MaterialApp(home: ImmersiveRoleplayPage(agentController: controller)),
    );
    await tester.pump();

    expect(
      tester.getSize(find.byKey(const Key('immersive-avatar-region'))).width,
      closeTo(844 * 0.44, 0.1),
    );
    expect(tester.takeException(), isNull);
  });
}

Future<AgentController> _roleplayController({
  PracticeClient? practiceClient,
}) async {
  final controller = AgentController(
    client: FakeAgentClient(),
    practiceClient: practiceClient,
  );
  await controller.initialize();
  await controller.selectScene(
    const AgentScene(
      id: 'daily-hotel',
      title: '酒店入住',
      description: '练习办理入住与需求沟通。',
      scenarioType: 'DAILY',
      presentationMode: AgentScenePresentationMode.immersiveRoleplay,
    ),
  );
  return controller;
}

final class _FailOncePracticeClient implements PracticeClient {
  final _delegate = FakePracticeClient();
  bool _shouldFail = true;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) =>
      _delegate.restorePractice(threadId: threadId, activeMatter: activeMatter);

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) => _delegate.startPractice(
    threadId: threadId,
    activeMatter: activeMatter,
    clientOperationId: clientOperationId,
  );

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    if (_shouldFail) {
      _shouldFail = false;
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
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
}

final class _AsyncReviewPracticeClient implements PracticeClient {
  _AsyncReviewPracticeClient({this.scenarioType, this.scenarioModel});

  final _delegate = FakePracticeClient();
  final String? scenarioType;
  final String? scenarioModel;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) =>
      _delegate.restorePractice(threadId: threadId, activeMatter: activeMatter);

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    final result = await _delegate.startPractice(
      threadId: threadId,
      activeMatter: activeMatter,
      clientOperationId: clientOperationId,
    );
    final snapshot = result.snapshot;
    if (scenarioType == null && scenarioModel == null) {
      return result;
    }
    return PracticeStartResult(
      snapshot: PracticeSessionSnapshot(
        sessionId: snapshot.sessionId,
        planId: snapshot.planId,
        threadId: snapshot.threadId,
        scenarioType: scenarioType,
        scenarioModel: scenarioModel,
        sessionVersion: snapshot.sessionVersion,
        matter: snapshot.matter,
        completedTurns: snapshot.completedTurns,
        turnLimit: snapshot.turnLimit,
        sessionCompleted: snapshot.sessionCompleted,
        currentQuestion: snapshot.currentQuestion,
        currentTurn: snapshot.currentTurn,
        turnHistory: snapshot.turnHistory,
        review: snapshot.review,
        formalReview: snapshot.formalReview,
      ),
    );
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
      answer: confirmation.answer.copyWith(speechFeedbackStatusUrl: statusUrl),
      completedTurns: confirmation.completedTurns,
      turnLimit: confirmation.turnLimit,
      sessionCompleted: confirmation.sessionCompleted,
      scenarioType: scenarioType ?? confirmation.scenarioType,
      scenarioModel: scenarioModel ?? confirmation.scenarioModel,
      sessionVersion: confirmation.sessionVersion,
      nextQuestion: confirmation.nextQuestion,
      review: confirmation.sessionCompleted ? null : confirmation.review,
      formalReview: confirmation.formalReview,
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
  }) => _delegate.submitText(
    sessionId: sessionId,
    questionId: questionId,
    answerText: answerText,
    idempotencyKey: idempotencyKey,
  );
}

final class _PendingSpeechFeedbackClient implements SpeechFeedbackClient {
  final _pending = Completer<SpeechFeedback>();

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) => _pending.future;

  @override
  Future<void> clearAccountState() async {}
}

final class _PendingInterviewReportClient implements InterviewReportClient {
  final _pending = Completer<InterviewReportEnvelope>();

  @override
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) =>
      _pending.future;

  @override
  Future<void> clearAccountState() async {}
}
