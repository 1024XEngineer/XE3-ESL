import '../support/practice_fixtures.dart';
import '../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation.dart';
import 'package:speakup/features/coaching/interview/interview_practice.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_practice.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_progress_store.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice.dart';
import 'package:speakup/features/coaching/evaluation/agent_conversation_feedback_presenter.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_client.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_disclosure.dart';

void main() {
  testWidgets(
    'Agent voice bubble loads correction and polish in the background',
    (tester) async {
      final feedback = _agentFeedback();
      final client = _Client(feedback);
      final controller = SpeechFeedbackController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 1,
      );
      final presenter = AgentConversationFeedbackPresenter(
        controller: controller,
      );
      addTearDown(controller.dispose);
      addTearDown(presenter.dispose);
      await tester.pumpWidget(
        MaterialApp(
          home: ConversationPage(
            threadId: 'thread_001',
            messages: [
              AgentMessage(
                id: 'message_001',
                role: AgentMessageRole.user,
                text: 'I manage the release.',
                modality: AgentMessageModality.voice,
                audio: const AgentMessageAudio(
                  id: 'audio_001',
                  status: AgentMessageAudioStatus.readable,
                  contentType: 'audio/mp4',
                  sizeBytes: 1024,
                  duration: Duration(seconds: 2),
                  playbackPath: '/v1/agent-audio/audio_001/playback',
                ),
                speechFeedbackStatusUrl: feedback.statusUrl,
              ),
            ],
            feedbackPresenter: presenter,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(client.calls, 1);
      expect(find.textContaining('评分'), findsNothing);
      expect(find.text('I managed'), findsNothing);

      await tester.tap(find.byKey(const Key('inline-language-optimize')));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('inline-language-correction-diff')),
        findsOneWidget,
      );
      expect(find.text('I handled the release successfully.'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      expect(controller.projections, isEmpty);
    },
  );

  testWidgets(
    'Scenario practice reuses correction and polish from the shared feedback UI',
    (tester) async {
      final feedback = _practiceFeedback();
      final client = _Client(feedback);
      final feedbackController = SpeechFeedbackController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 1,
      );
      final snapshot = _practiceSnapshot(feedback.statusUrl);
      final practiceController = PracticeController(
        client: _PracticeClient(snapshot),
      );
      addTearDown(feedbackController.dispose);
      addTearDown(practiceController.dispose);
      await _restorePractice(practiceController, snapshot);

      await tester.pumpWidget(
        MaterialApp(
          home: ScenarioPracticePage(
            practiceController: practiceController,
            speechFeedbackController: feedbackController,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(client.calls, 1);
      expect(find.text('优化'), findsNothing);
      expect(find.byKey(const Key('inline-language-optimize')), findsOneWidget);
      expect(find.text('I managed'), findsNothing);
      expect(find.text('I handled the release successfully.'), findsNothing);

      await tester.tap(find.byKey(const Key('inline-language-optimize')));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('inline-language-correction-diff')),
        findsOneWidget,
      );
      expect(find.text('I handled the release successfully.'), findsOneWidget);
    },
  );

  testWidgets(
    'Practice SAME_QUESTION reuses recorder and never advances progress',
    (tester) async {
      final feedback = _practiceFeedback();
      final client = _Client(feedback);
      final feedbackController = SpeechFeedbackController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 1,
      );
      final snapshot = _practiceSnapshot(feedback.statusUrl);
      final practiceClient = _PracticeClient(snapshot);
      final practiceController = PracticeController(client: practiceClient);
      addTearDown(feedbackController.dispose);
      addTearDown(practiceController.dispose);
      await _restorePractice(practiceController, snapshot);

      await tester.pumpWidget(
        MaterialApp(
          home: InterviewPracticePage(
            practiceController: practiceController,
            speechFeedbackController: feedbackController,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(client.calls, 1);
      expect(find.text('优化'), findsNothing);
      expect(find.byKey(const Key('inline-language-optimize')), findsOneWidget);
      expect(find.text('I managed'), findsNothing);

      await tester.tap(find.byKey(const Key('inline-language-optimize')));
      await tester.pump();

      expect(
        find.byKey(const Key('inline-language-correction-diff')),
        findsOneWidget,
      );
      final retryAction = find.byKey(
        const Key('speech-feedback-repractice-item_practice_001'),
      );
      expect(retryAction, findsOneWidget);
      final messageIdsBefore = practiceController.practiceMessages
          .map((message) => message.id)
          .toList();

      await tester.ensureVisible(retryAction);
      await tester.tap(retryAction);
      await tester.pumpAndSettle();

      expect(practiceClient.retryRequests, 1);
      expect(
        practiceController.recordingState,
        PracticeRecordingState.recording,
      );
      expect(practiceController.completedTurns, 1);

      await tester.tap(find.byKey(const Key('practice-stop-recording')));
      await tester.pumpAndSettle();

      expect(practiceClient.retryConfirmations, 1);
      expect(practiceController.recordingState, PracticeRecordingState.idle);
      expect(practiceController.completedTurns, 1);
      expect(
        practiceController.practiceMessages.map((message) => message.id),
        messageIdsBefore,
      );
      expect(find.text('同题复练已提交，不影响场景进度。'), findsOneWidget);
    },
  );

  testWidgets(
    'IELTS Part 2 English answer keeps feedback behind independent completion',
    (tester) async {
      final feedback = _practiceFeedback();
      final client = _Client(feedback);
      final feedbackController = SpeechFeedbackController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 1,
      );
      final snapshot = _practiceSnapshot(
        feedback.statusUrl,
        practiceExperience: PracticeExperience.ieltsSpeaking,
        sceneCategory: SceneCategory.ieltsSpeaking,
        turnLimit: 1,
      );
      final practiceController = PracticeController(
        client: _PracticeClient(snapshot),
      );
      addTearDown(feedbackController.dispose);
      addTearDown(practiceController.dispose);
      await _restorePractice(practiceController, snapshot);

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: practiceController,
            speechFeedbackController: feedbackController,
            progressStore: _MemoryIeltsProgressStore(),
          ),
        ),
      );
      await tester.pump();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      expect(client.calls, 1);
      expect(find.byKey(const Key('ielts-section-completion-sheet')), findsOne);
      expect(find.text('Part 2 已完成'), findsOne);
      expect(
        find.byKey(const Key('ielts-mock-part-2-transition')),
        findsNothing,
      );
      expect(find.byKey(const Key('ielts-part2-continue-part3')), findsNothing);
      expect(
        feedbackController.projectionFor(
          'practice:practice_session_001:practice_turn_001',
        ),
        isNotNull,
      );
      expect(find.text('评分与纠错'), findsNothing);
    },
  );

  testWidgets('IELTS keeps a Chinese answer but hides insufficient feedback', (
    tester,
  ) async {
    final feedback = _practiceFeedback(insufficient: true);
    final client = _Client(feedback);
    final feedbackController = SpeechFeedbackController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    final snapshot = _practiceSnapshot(
      feedback.statusUrl,
      practiceExperience: PracticeExperience.ieltsSpeaking,
      sceneCategory: SceneCategory.ieltsSpeaking,
      turnLimit: 1,
    );
    final practiceController = PracticeController(
      client: _PracticeClient(
        PracticeSessionSnapshot(
          sessionId: snapshot.sessionId,
          planId: snapshot.planId,
          practiceExperience: snapshot.practiceExperience,
          sceneCategory: snapshot.sceneCategory,
          practiceMode: snapshot.practiceMode,
          capabilities: snapshot.capabilities,
          sessionVersion: snapshot.sessionVersion,
          completedTurns: snapshot.completedTurns,
          turnLimit: snapshot.turnLimit,
          sessionCompleted: snapshot.sessionCompleted,
          ieltsAssignment: snapshot.ieltsAssignment,
          currentQuestion: snapshot.currentQuestion,
          turnHistory: [
            PracticeTurnExchange(
              question: snapshot.turnHistory.single.question,
              turn: PracticeTurnSnapshot(
                id: snapshot.turnHistory.single.turn.id,
                sessionId: snapshot.sessionId,
                questionId: snapshot.turnHistory.single.question.id,
                respondentParticipantId: 'participant_user',
                candidateId: 'candidate_001',
                answerText: '然后，黄天宇主要来把这个。',
                evidenceVersion: 1,
                effectiveTurns: 1,
                sessionCompleted:
                    snapshot.turnHistory.single.turn.sessionCompleted,
                audioAssetId: 'audio_asset_001',
                speechFeedbackStatusUrl: feedback.statusUrl,
              ),
            ),
          ],
        ),
      ),
    );
    addTearDown(feedbackController.dispose);
    addTearDown(practiceController.dispose);
    await _restorePractice(practiceController, snapshot);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: practiceController,
          speechFeedbackController: feedbackController,
          progressStore: _MemoryIeltsProgressStore(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.byKey(const Key('ielts-section-completion-sheet')), findsOne);
    expect(find.text('Part 2 已完成'), findsOne);
    expect(find.byKey(const Key('ielts-mock-part-2-transition')), findsNothing);
    expect(find.byKey(const Key('ielts-part2-continue-part3')), findsNothing);
    expect(find.textContaining('证据不足'), findsNothing);
    expect(find.byType(SpeechFeedbackDisclosure), findsNothing);
  });

  testWidgets(
    'shared feedback controller does not rebuild another route mid-frame',
    (tester) async {
      final feedback = _agentFeedback();
      final client = _Client(feedback);
      final controller = SpeechFeedbackController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 1,
      );
      final presenter = AgentConversationFeedbackPresenter(
        controller: controller,
      );
      addTearDown(controller.dispose);
      addTearDown(presenter.dispose);
      final navigatorKey = GlobalKey<NavigatorState>();
      late StateSetter updateConversation;
      var messages = <AgentMessage>[];

      await tester.pumpWidget(
        MaterialApp(
          navigatorKey: navigatorKey,
          home: StatefulBuilder(
            builder: (context, setState) {
              updateConversation = setState;
              return ConversationPage(
                threadId: 'thread_001',
                messages: messages,
                feedbackPresenter: presenter,
              );
            },
          ),
        ),
      );
      navigatorKey.currentState!.push(
        MaterialPageRoute<void>(
          builder: (_) =>
              InterviewPracticePage(speechFeedbackController: controller),
        ),
      );
      await tester.pumpAndSettle();

      updateConversation(() {
        messages = [
          AgentMessage(
            id: 'message_001',
            role: AgentMessageRole.user,
            text: 'I manage the release.',
            modality: AgentMessageModality.voice,
            audio: const AgentMessageAudio(
              id: 'audio_001',
              status: AgentMessageAudioStatus.readable,
              contentType: 'audio/mp4',
              sizeBytes: 1024,
              duration: Duration(seconds: 2),
              playbackPath: '/v1/agent-audio/audio_001/playback',
            ),
            speechFeedbackStatusUrl: feedback.statusUrl,
          ),
        ];
      });
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(client.calls, 1);
      expect(
        controller.projections.values.single.feedback?.evaluationId,
        feedback.evaluationId,
      );
    },
  );
}

final class _MemoryIeltsProgressStore implements IeltsMockProgressStore {
  IeltsMockProgress? value;

  @override
  Future<IeltsMockProgress?> read(String sessionId) async {
    return value?.sessionId == sessionId ? value : null;
  }

  @override
  Future<void> write(IeltsMockProgress progress) async {
    value = progress;
  }

  @override
  Future<void> delete(String sessionId) async {
    if (value?.sessionId == sessionId) {
      value = null;
    }
  }
}

SpeechFeedback _agentFeedback() {
  const messageId = '20000000-0000-4000-8000-000000000010';
  const statusUrl = '/v1/agent-messages/$messageId/evaluation';
  return SpeechFeedback(
    evaluationId: '10000000-0000-4000-8000-000000000010',
    source: const SpeechFeedbackSource(
      kind: SpeechFeedbackSourceKind.agentMessage,
      sourceId: messageId,
      contextId: '30000000-0000-4000-8000-000000000010',
    ),
    feedbackStatus: SpeechFeedbackStatus.ready,
    scoreabilityStatus: SpeechFeedbackScoreabilityStatus.provisional,
    summary: 'Use the past tense and a more natural expression.',
    reasonCodes: const [],
    items: [
      SpeechFeedbackItem(
        feedbackItemId: 'item_agent_001',
        evaluationId: '10000000-0000-4000-8000-000000000010',
        position: 1,
        kind: SpeechFeedbackItemKind.correction,
        anchor: const SpeechFeedbackAnchor(
          evidenceRefId: messageId,
          startUtf8Byte: 0,
          endUtf8Byte: 8,
          originalExcerpt: 'I manage',
        ),
        explanation: 'Use the past tense for the completed release.',
        suggestedText: 'I managed',
        repracticeMode: SpeechFeedbackRepracticeMode.none,
        createdAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
      ),
      SpeechFeedbackItem(
        feedbackItemId: 'item_agent_002',
        evaluationId: '10000000-0000-4000-8000-000000000010',
        position: 2,
        kind: SpeechFeedbackItemKind.recommendedExpression,
        anchor: const SpeechFeedbackAnchor(
          evidenceRefId: messageId,
          startUtf8Byte: 0,
          endUtf8Byte: 8,
          originalExcerpt: 'I manage',
        ),
        explanation: 'Use a more natural completed-action expression.',
        suggestedText: 'I handled the release successfully.',
        repracticeMode: SpeechFeedbackRepracticeMode.none,
        createdAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
      ),
    ],
    acousticAssessment: const SpeechFeedbackAcousticAssessment.notAssessed(
      reason: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    ),
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
  );
}

SpeechFeedback _practiceFeedback({bool insufficient = false}) {
  const turnId = '20000000-0000-4000-8000-000000000020';
  const statusUrl = '/v1/practice-turns/$turnId/evaluation';
  return SpeechFeedback(
    evaluationId: '10000000-0000-4000-8000-000000000020',
    source: const SpeechFeedbackSource(
      kind: SpeechFeedbackSourceKind.practiceTurn,
      sourceId: turnId,
      contextId: '30000000-0000-4000-8000-000000000020',
    ),
    feedbackStatus: SpeechFeedbackStatus.ready,
    scoreabilityStatus: insufficient
        ? SpeechFeedbackScoreabilityStatus.insufficient
        : SpeechFeedbackScoreabilityStatus.provisional,
    summary: insufficient ? 'Evidence is insufficient.' : 'Use past tense.',
    reasonCodes: insufficient
        ? const ['TRANSCRIPT_CONFIDENCE_INSUFFICIENT']
        : const [],
    items: insufficient
        ? const []
        : [
            SpeechFeedbackItem(
              feedbackItemId: 'item_practice_001',
              evaluationId: '10000000-0000-4000-8000-000000000020',
              position: 1,
              kind: SpeechFeedbackItemKind.correction,
              anchor: const SpeechFeedbackAnchor(
                evidenceRefId: 'practice_turn_001',
                startUtf8Byte: 0,
                endUtf8Byte: 8,
                originalExcerpt: 'I manage',
              ),
              explanation: 'Use the past tense for the completed release.',
              suggestedText: 'I managed',
              repracticeMode: SpeechFeedbackRepracticeMode.sameQuestion,
              createdAt: DateTime.utc(2026, 7, 30, 10, 1, 1),
            ),
            SpeechFeedbackItem(
              feedbackItemId: 'item_practice_002',
              evaluationId: '10000000-0000-4000-8000-000000000020',
              position: 2,
              kind: SpeechFeedbackItemKind.recommendedExpression,
              anchor: const SpeechFeedbackAnchor(
                evidenceRefId: 'practice_turn_001',
                startUtf8Byte: 0,
                endUtf8Byte: 8,
                originalExcerpt: 'I manage',
              ),
              explanation: 'Use a more natural completed-action expression.',
              suggestedText: 'I handled the release successfully.',
              repracticeMode: SpeechFeedbackRepracticeMode.none,
              createdAt: DateTime.utc(2026, 7, 30, 10, 1, 1),
            ),
          ],
    acousticAssessment: const SpeechFeedbackAcousticAssessment.notAssessed(
      reason: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    ),
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10, 1),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 1, 1),
  );
}

PracticeSessionSnapshot _practiceSnapshot(
  String statusUrl, {
  PracticeExperience practiceExperience = PracticeExperience.workplace,
  SceneCategory sceneCategory = SceneCategory.workplaceGeneral,
  int turnLimit = 3,
}) {
  const sessionId = 'practice_session_001';
  final ielts = practiceExperience == PracticeExperience.ieltsSpeaking;
  const question = PracticeQuestion(
    id: 'practice_question_001',
    sessionId: sessionId,
    text: 'What did you manage?',
  );
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: 'plan_practice_session_001',
    practiceExperience: practiceExperience,
    sceneCategory: sceneCategory,
    practiceMode: ielts ? PracticeMode.part2 : PracticeMode.fullSimulation,
    capabilities: testPracticeCapabilities,
    sessionVersion: 2,
    completedTurns: 1,
    turnLimit: turnLimit,
    sessionCompleted: ielts,
    ieltsAssignment: ielts
        ? testIeltsAssignment(mode: PracticeMode.part2)
        : null,
    currentQuestion: ielts
        ? null
        : const PracticeQuestion(
            id: 'practice_question_002',
            sessionId: sessionId,
            text: 'What happened next?',
          ),
    turnHistory: [
      PracticeTurnExchange(
        question: question,
        turn: PracticeTurnSnapshot(
          id: 'practice_turn_001',
          sessionId: sessionId,
          questionId: question.id,
          respondentParticipantId: 'participant_user',
          candidateId: 'candidate_001',
          answerText: 'I manage the release.',
          evidenceVersion: 1,
          effectiveTurns: 1,
          sessionCompleted: ielts,
          audioAssetId: 'audio_asset_001',
          speechFeedbackStatusUrl: statusUrl,
        ),
      ),
    ],
  );
}

Future<void> _restorePractice(
  PracticeController controller,
  PracticeSessionSnapshot snapshot,
) async {
  final scene = _practiceScene(
    snapshot.practiceExperience,
    snapshot.sceneCategory,
  );
  await controller.restoreCreatedPractice(
    sessionId: snapshot.sessionId,
    scene: scene,
  );
}

SceneDefinition _practiceScene(PracticeExperience family, SceneCategory model) {
  return testScene(
    id: model == SceneCategory.ieltsSpeaking
        ? 'feedback-ielts-part-2'
        : 'feedback-workplace',
    experience: family,
    category: model,
    name: model == SceneCategory.ieltsSpeaking
        ? 'IELTS Speaking Part 2'
        : 'Workplace feedback',
  );
}

final class _Client implements SpeechFeedbackClient {
  _Client(this.feedback);

  final SpeechFeedback feedback;
  int calls = 0;

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) async {
    calls++;
    return feedback;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _PracticeClient
    implements PracticeClient, PracticeSpeechFeedbackRetryClient {
  _PracticeClient(this.snapshot);

  final PracticeSessionSnapshot snapshot;
  int retryRequests = 0;
  int retryConfirmations = 0;

  static final _retryTurn = PracticeRetryTurn(
    turnId: 'retry_turn_001',
    sessionId: 'practice_session_001',
    originalTurnId: 'practice_turn_001',
    questionId: 'practice_question_001',
    sequence: 2,
    status: PracticeRetryTurnStatus.answering,
    createdAt: DateTime.utc(2026, 7, 30, 10, 2),
    replayed: false,
  );

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    if (sessionId != snapshot.sessionId) {
      throw StateError('Unknown test Practice Session.');
    }
    return snapshot;
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    if (sessionId != snapshot.sessionId || clientOperationId.trim().isEmpty) {
      throw StateError('Unknown test Practice Session activation.');
    }
    return snapshot;
  }

  @override
  Future<PracticeRetryTurn> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
  }) async {
    expect(feedbackItemId, 'item_practice_001');
    expect(idempotencyKey, isNotEmpty);
    retryRequests++;
    return _retryTurn;
  }

  @override
  Future<RetryTranscriptionCandidate> transcribeRetry({
    required String answerPath,
    required String idempotencyKey,
    required RecordedPracticeAudio audio,
  }) async {
    expect(answerPath, _retryTurn.answerPath);
    expect(idempotencyKey, isNotEmpty);
    expect(audio.contentType, 'audio/wav');
    return RetryTranscriptionCandidate(
      id: 'retry_candidate_001',
      retryTurnId: 'retry_turn_001',
      sessionId: _retryTurn.sessionId,
      questionId: _retryTurn.questionId,
      respondentParticipantId: 'participant_user',
      transcriptId: 'retry_transcript_001',
      evidenceVersion: 1,
      text: 'I managed the release.',
      createdAt: DateTime.utc(2026, 7, 30, 10, 3),
    );
  }

  @override
  Future<ConfirmedRetryTurn> confirmRetry({
    required String retryTurnId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    expect(retryTurnId, _retryTurn.turnId);
    expect(candidateId, 'retry_candidate_001');
    expect(idempotencyKey, isNotEmpty);
    retryConfirmations++;
    return ConfirmedRetryTurn(
      turnId: retryTurnId,
      originalTurnId: _retryTurn.originalTurnId,
      sessionId: _retryTurn.sessionId,
      questionId: _retryTurn.questionId,
      respondentParticipantId: 'participant_user',
      candidateId: candidateId,
      answerText: 'I managed the release.',
      evidenceVersion: 1,
      countsTowardTurnLimit: false,
      createdAt: DateTime.utc(2026, 7, 30, 10, 2, 1),
      confirmedAt: DateTime.utc(2026, 7, 30, 10, 3, 1),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    throw UnimplementedError();
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
}
