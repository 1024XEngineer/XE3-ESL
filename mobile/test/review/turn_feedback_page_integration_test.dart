import '../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/practice/conversation.dart';
import 'package:speakup/features/coaching/practice/practice.dart';
import 'package:speakup/features/coaching/practice/ielts_mock_progress_store.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/review/turn_feedback.dart';
import 'package:speakup/features/coaching/review/turn_feedback_client.dart';
import 'package:speakup/features/coaching/review/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/review/turn_feedback_disclosure.dart';

void main() {
  testWidgets(
    'Agent voice bubble loads folded feedback and SAME_THREAD starts voice',
    (tester) async {
      final feedback = _agentFeedback();
      final client = _Client(feedback);
      final controller = SpeechFeedbackController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 1,
      );
      addTearDown(controller.dispose);
      var voiceStarts = 0;

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
            onStartVoice: () async {
              voiceStarts++;
            },
            speechFeedbackController: controller,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(client.calls, 1);
      expect(find.text('评分与纠错'), findsOneWidget);
      expect(find.text('I managed'), findsNothing);

      await tester.tap(
        find.byKey(const Key('speech-feedback-disclosure-toggle')),
      );
      await tester.pump();
      expect(find.text('I managed'), findsOneWidget);
      final action = find.byKey(
        const Key('speech-feedback-repractice-item_agent_001'),
      );
      await tester.ensureVisible(action);
      await tester.tap(action);
      await tester.pump();
      expect(voiceStarts, 1);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      expect(controller.projections, isEmpty);
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
      final practiceController = AgentController(
        client: FakeAgentClient(),
        practiceClient: practiceClient,
      );
      addTearDown(feedbackController.dispose);
      addTearDown(practiceController.dispose);
      await _restorePractice(practiceController, snapshot);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            agentController: practiceController,
            speechFeedbackController: feedbackController,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(client.calls, 1);
      expect(find.text('评分与纠错'), findsOneWidget);
      expect(find.text('I managed'), findsNothing);

      await tester.tap(
        find.byKey(const Key('speech-feedback-disclosure-toggle')),
      );
      await tester.pump();

      expect(find.text('I managed'), findsOneWidget);
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

  testWidgets('IELTS Part 2 English answer shows the shared feedback entry', (
    tester,
  ) async {
    final feedback = _practiceFeedback();
    final client = _Client(feedback);
    final feedbackController = SpeechFeedbackController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    final snapshot = _practiceSnapshot(
      feedback.statusUrl,
      sceneFamily: SceneFamily.exam,
      sceneModel: SceneModel.ieltsSpeakingPart2,
      turnLimit: 6,
    );
    final practiceController = AgentController(
      client: FakeAgentClient(),
      practiceClient: _PracticeClient(snapshot),
    );
    addTearDown(feedbackController.dispose);
    addTearDown(practiceController.dispose);
    await _restorePractice(practiceController, snapshot);

    await tester.pumpWidget(
      MaterialApp(
        home: PracticePage(
          agentController: practiceController,
          speechFeedbackController: feedbackController,
          ieltsMockProgressStore: _MemoryIeltsProgressStore(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(client.calls, 1);
    expect(
      find.byKey(const Key('ielts-part2-practice-complete')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-part2-answer-feedback')),
      findsOneWidget,
    );
    expect(find.text('I manage the release.'), findsOneWidget);
    expect(
      feedbackController.projectionFor(
        'practice:practice_session_001:practice_turn_001',
      ),
      isNotNull,
    );
    expect(find.text('评分与纠错'), findsOneWidget);
    expect(
      find.byKey(
        const ValueKey(
          'ielts-speech-feedback-practice:practice_session_001:practice_turn_001',
        ),
      ),
      findsOneWidget,
    );
  });

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
      sceneFamily: SceneFamily.exam,
      sceneModel: SceneModel.ieltsSpeakingPart2,
      turnLimit: 6,
    );
    final practiceController = AgentController(
      client: FakeAgentClient(),
      practiceClient: _PracticeClient(
        PracticeSessionSnapshot(
          sessionId: snapshot.sessionId,
          planId: snapshot.planId,
          sceneFamily: snapshot.sceneFamily,
          sceneModel: snapshot.sceneModel,
          sessionVersion: snapshot.sessionVersion,
          completedTurns: snapshot.completedTurns,
          turnLimit: snapshot.turnLimit,
          sessionCompleted: snapshot.sessionCompleted,
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
                sessionCompleted: false,
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
        home: PracticePage(
          agentController: practiceController,
          speechFeedbackController: feedbackController,
          ieltsMockProgressStore: _MemoryIeltsProgressStore(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('然后，黄天宇主要来把这个。'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-part2-practice-complete')),
      findsOneWidget,
    );
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
      addTearDown(controller.dispose);
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
                speechFeedbackController: controller,
              );
            },
          ),
        ),
      );
      navigatorKey.currentState!.push(
        MaterialPageRoute<void>(
          builder: (_) => PracticePage(speechFeedbackController: controller),
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
        controller.projections.values.single.feedback?.speechFeedbackId,
        feedback.speechFeedbackId,
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
  const statusUrl = '/v1/speech-feedback/speech_feedback_agent_001';
  return SpeechFeedback(
    speechFeedbackId: 'speech_feedback_agent_001',
    source: const AgentVoiceMessageFeedbackSource(
      threadId: 'thread_001',
      messageId: 'message_001',
      transcriptEvidenceId: 'transcript_evidence_001',
      candidateVersion: 1,
    ),
    feedbackStatus: SpeechFeedbackStatus.ready,
    scoreabilityStatus: SpeechFeedbackScoreabilityStatus.provisional,
    gateStatus: SpeechFeedbackGateStatus.feedbackOnly,
    reasonCodes: const [],
    schemaVersion: 'speech-feedback/v1',
    strategyRef: 'qianwen-speech-feedback/v1',
    pipelineVersion: 'speech-feedback-pipeline/v1',
    isFinal: false,
    items: [
      SpeechFeedbackItem(
        feedbackItemId: 'item_agent_001',
        speechFeedbackId: 'speech_feedback_agent_001',
        kind: SpeechFeedbackItemKind.correction,
        anchor: const AgentTranscriptFeedbackAnchor(
          transcriptEvidenceId: 'transcript_evidence_001',
          messageId: 'message_001',
          startUtf8Byte: 0,
          endUtf8Byte: 8,
          originalExcerpt: 'I manage',
        ),
        explanation: 'Use the past tense for the completed release.',
        suggestedText: 'I managed',
        repracticeMode: SpeechFeedbackRepracticeMode.sameThread,
        createdAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
      ),
    ],
    acousticAssessment: const SpeechFeedbackAcousticAssessment(
      pronunciation: SpeechFeedbackAssessmentStatus.notAssessed,
      acousticFluency: SpeechFeedbackAssessmentStatus.notAssessed,
      reasonCode: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    ),
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
    completedAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
  );
}

SpeechFeedback _practiceFeedback({bool insufficient = false}) {
  const statusUrl = '/v1/speech-feedback/speech_feedback_practice_001';
  return SpeechFeedback(
    speechFeedbackId: 'speech_feedback_practice_001',
    source: const ConversationTurnFeedbackSource(
      practiceSessionId: 'practice_session_001',
      turnId: 'practice_turn_001',
      inputRevision: 1,
      evidenceSnapshotId: 'evidence_snapshot_001',
    ),
    feedbackStatus: SpeechFeedbackStatus.ready,
    scoreabilityStatus: insufficient
        ? SpeechFeedbackScoreabilityStatus.insufficient
        : SpeechFeedbackScoreabilityStatus.provisional,
    gateStatus: SpeechFeedbackGateStatus.feedbackOnly,
    reasonCodes: insufficient
        ? const ['TRANSCRIPT_CONFIDENCE_INSUFFICIENT']
        : const [],
    schemaVersion: 'speech-feedback/v1',
    strategyRef: 'qianwen-speech-feedback/v1',
    pipelineVersion: 'speech-feedback-pipeline/v1',
    isFinal: false,
    items: insufficient
        ? const []
        : [
            SpeechFeedbackItem(
              feedbackItemId: 'item_practice_001',
              speechFeedbackId: 'speech_feedback_practice_001',
              kind: SpeechFeedbackItemKind.correction,
              anchor: const ConversationTranscriptFeedbackAnchor(
                evidenceRefId: 'evidence_ref_001',
                turnId: 'practice_turn_001',
                startUtf8Byte: 0,
                endUtf8Byte: 8,
                originalExcerpt: 'I manage',
              ),
              explanation: 'Use the past tense for the completed release.',
              suggestedText: 'I managed',
              repracticeMode: SpeechFeedbackRepracticeMode.sameQuestion,
              createdAt: DateTime.utc(2026, 7, 30, 10, 1, 1),
            ),
          ],
    acousticAssessment: const SpeechFeedbackAcousticAssessment(
      pronunciation: SpeechFeedbackAssessmentStatus.notAssessed,
      acousticFluency: SpeechFeedbackAssessmentStatus.notAssessed,
      reasonCode: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    ),
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10, 1),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 1, 1),
    completedAt: DateTime.utc(2026, 7, 30, 10, 1, 1),
  );
}

PracticeSessionSnapshot _practiceSnapshot(
  String statusUrl, {
  SceneFamily sceneFamily = SceneFamily.workplace,
  SceneModel sceneModel = SceneModel.workplaceBasicDialogue,
  int turnLimit = 3,
}) {
  const sessionId = 'practice_session_001';
  const question = PracticeQuestion(
    id: 'practice_question_001',
    sessionId: sessionId,
    text: 'What did you manage?',
  );
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: 'plan_practice_session_001',
    sceneFamily: sceneFamily,
    sceneModel: sceneModel,
    sessionVersion: 2,
    completedTurns: 1,
    turnLimit: turnLimit,
    sessionCompleted: false,
    currentQuestion: const PracticeQuestion(
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
          sessionCompleted: false,
          audioAssetId: 'audio_asset_001',
          speechFeedbackStatusUrl: statusUrl,
        ),
      ),
    ],
  );
}

Future<void> _restorePractice(
  AgentController controller,
  PracticeSessionSnapshot snapshot,
) async {
  final scene = _practiceScene(snapshot.sceneFamily, snapshot.sceneModel);
  await controller.initialize();
  await controller.selectScene(scene);
  await controller.restoreCreatedPractice(
    sessionId: snapshot.sessionId,
    scene: scene,
  );
}

SceneDefinition _practiceScene(SceneFamily family, SceneModel model) {
  return testScene(
    id: model == SceneModel.ieltsSpeakingPart2
        ? 'feedback-ielts-part-2'
        : 'feedback-workplace',
    family: family,
    model: model,
    name: model == SceneModel.ieltsSpeakingPart2
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

  static final _retryRequest = PracticeRetryRequest(
    retryRequestId: 'retry_request_001',
    feedbackItemId: 'item_practice_001',
    sessionId: 'practice_session_001',
    originalTurnId: 'practice_turn_001',
    questionId: 'practice_question_001',
    retryStatus: PracticeRetryRequestStatus.turnCreated,
    statusUrl: '/v1/retry-requests/retry_request_001',
    createdAt: DateTime.utc(2026, 7, 30, 10, 2),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 2, 1),
    newTurnId: 'retry_turn_001',
    answerPath: '/v1/retry-turns/retry_turn_001/transcription-candidates',
    completedAt: DateTime.utc(2026, 7, 30, 10, 2, 1),
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
  Future<PracticeRetryRequest> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
  }) async {
    expect(feedbackItemId, 'item_practice_001');
    expect(idempotencyKey, isNotEmpty);
    retryRequests++;
    return _retryRequest;
  }

  @override
  Future<PracticeRetryRequest> getSameQuestionRetryRequest({
    required String retryRequestId,
  }) async {
    expect(retryRequestId, _retryRequest.retryRequestId);
    return _retryRequest;
  }

  @override
  Future<RetryTranscriptionCandidate> transcribeRetry({
    required String answerPath,
    required String idempotencyKey,
    required RecordedPracticeAudio audio,
  }) async {
    expect(answerPath, _retryRequest.answerPath);
    expect(idempotencyKey, isNotEmpty);
    expect(audio.contentType, 'audio/wav');
    return RetryTranscriptionCandidate(
      id: 'retry_candidate_001',
      retryTurnId: 'retry_turn_001',
      retryRequestId: _retryRequest.retryRequestId,
      sessionId: _retryRequest.sessionId,
      questionId: _retryRequest.questionId,
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
    expect(retryTurnId, _retryRequest.newTurnId);
    expect(candidateId, 'retry_candidate_001');
    expect(idempotencyKey, isNotEmpty);
    retryConfirmations++;
    return ConfirmedRetryTurn(
      turnId: retryTurnId,
      retryRequestId: _retryRequest.retryRequestId,
      originalTurnId: _retryRequest.originalTurnId,
      sessionId: _retryRequest.sessionId,
      questionId: _retryRequest.questionId,
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
