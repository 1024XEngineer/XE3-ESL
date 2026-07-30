import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/review/turn_feedback.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'completed Daily retry cancel, rerecord, confirm and cleanup stay isolated',
    () async {
      final practice = _RetryPracticeClient(_completedDailySnapshot);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.canStartSpeechFeedbackRetry, isTrue);
      final originalMessages = controller.practiceMessages
          .map((message) => message.id)
          .toList();

      expect(await controller.startSpeechFeedbackRetry(_feedbackItem), isTrue);
      expect(controller.recordingState, PracticeRecordingState.recording);
      await controller.cancelRecording();
      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.isSpeechFeedbackRetryActive, isFalse);

      expect(await controller.startSpeechFeedbackRetry(_feedbackItem), isTrue);
      await controller.stopRecording();
      expect(
        controller.recordingState,
        PracticeRecordingState.awaitingConfirmation,
      );
      controller.rerecord();
      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.isSpeechFeedbackRetryActive, isFalse);

      expect(await controller.startSpeechFeedbackRetry(_feedbackItem), isTrue);
      await controller.stopRecording();
      await controller.confirmTranscript();

      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.completedTurns, 3);
      expect(controller.speechFeedbackRetryCompletionCount, 1);
      expect(
        controller.practiceMessages.map((message) => message.id),
        originalMessages,
      );
      expect(practice.confirmations, 1);

      expect(await controller.startSpeechFeedbackRetry(_feedbackItem), isTrue);
      await controller.clearPrivateState();
      expect(controller.isSpeechFeedbackRetryActive, isFalse);
      expect(controller.practiceSessionId, isNull);
      expect(controller.recordingState, PracticeRecordingState.idle);
    },
  );

  test(
    'stable retry failure stays visible and never starts a fake recording',
    () async {
      final practice = _RetryPracticeClient(
        _completedDailySnapshot,
        failCreation: true,
      );
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.startSpeechFeedbackRetry(_feedbackItem), isFalse);

      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.isSpeechFeedbackRetryActive, isFalse);
      expect(controller.errorMessage, contains('原题'));
      expect(practice.transcriptions, 0);
    },
  );
}

final class _RetryPracticeClient
    implements PracticeClient, PracticeSpeechFeedbackRetryClient {
  _RetryPracticeClient(this.snapshot, {this.failCreation = false});

  final PracticeSessionSnapshot snapshot;
  final bool failCreation;
  int confirmations = 0;
  int transcriptions = 0;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async => snapshot;

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeRetryRequest> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
  }) async {
    expect(feedbackItemId, _feedbackItem.feedbackItemId);
    return failCreation ? _failedRetryRequest : _retryRequest;
  }

  @override
  Future<PracticeRetryRequest> getSameQuestionRetryRequest({
    required String retryRequestId,
  }) async => _retryRequest;

  @override
  Future<RetryTranscriptionCandidate> transcribeRetry({
    required String answerPath,
    required String idempotencyKey,
    required RecordedPracticeAudio audio,
  }) async {
    transcriptions++;
    return RetryTranscriptionCandidate(
      id: 'candidate_retry_daily_001',
      retryTurnId: 'turn_retry_daily_001',
      retryRequestId: _retryRequest.retryRequestId,
      sessionId: _retryRequest.sessionId,
      questionId: _retryRequest.questionId,
      respondentParticipantId: 'participant_user',
      transcriptId: 'transcript_retry_daily_001',
      evidenceVersion: 1,
      text: 'I explained the issue clearly.',
      createdAt: DateTime.utc(2026, 7, 30, 11, 1),
    );
  }

  @override
  Future<ConfirmedRetryTurn> confirmRetry({
    required String retryTurnId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    confirmations++;
    return ConfirmedRetryTurn(
      turnId: retryTurnId,
      retryRequestId: _retryRequest.retryRequestId,
      originalTurnId: _retryRequest.originalTurnId,
      sessionId: _retryRequest.sessionId,
      questionId: _retryRequest.questionId,
      respondentParticipantId: 'participant_user',
      candidateId: candidateId,
      answerText: 'I explained the issue clearly.',
      evidenceVersion: 1,
      countsTowardTurnLimit: false,
      createdAt: DateTime.utc(2026, 7, 30, 11),
      confirmedAt: DateTime.utc(2026, 7, 30, 11, 1, 1),
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

final _feedbackItem = SpeechFeedbackItem(
  feedbackItemId: 'feedback_item_daily_001',
  speechFeedbackId: 'speech_feedback_daily_001',
  kind: SpeechFeedbackItemKind.correction,
  anchor: const ConversationTranscriptFeedbackAnchor(
    evidenceRefId: 'evidence_daily_001',
    turnId: 'turn_daily_001',
    startUtf8Byte: 0,
    endUtf8Byte: 9,
    originalExcerpt: 'I explain',
  ),
  explanation: 'Use past tense.',
  suggestedText: 'I explained',
  repracticeMode: SpeechFeedbackRepracticeMode.sameQuestion,
  createdAt: DateTime.utc(2026, 7, 30, 10, 59),
);

final _retryRequest = PracticeRetryRequest(
  retryRequestId: 'retry_request_daily_001',
  feedbackItemId: _feedbackItem.feedbackItemId,
  sessionId: 'session_daily_001',
  originalTurnId: 'turn_daily_001',
  questionId: 'question_daily_001',
  retryStatus: PracticeRetryRequestStatus.turnCreated,
  statusUrl: '/v1/retry-requests/retry_request_daily_001',
  createdAt: DateTime.utc(2026, 7, 30, 11),
  updatedAt: DateTime.utc(2026, 7, 30, 11, 0, 1),
  newTurnId: 'turn_retry_daily_001',
  answerPath: '/v1/retry-turns/turn_retry_daily_001/transcription-candidates',
  completedAt: DateTime.utc(2026, 7, 30, 11, 0, 1),
);

final _failedRetryRequest = PracticeRetryRequest(
  retryRequestId: 'retry_request_daily_failed',
  feedbackItemId: _feedbackItem.feedbackItemId,
  sessionId: 'session_daily_001',
  originalTurnId: 'turn_daily_001',
  questionId: 'question_daily_001',
  retryStatus: PracticeRetryRequestStatus.failed,
  statusUrl: '/v1/retry-requests/retry_request_daily_failed',
  createdAt: DateTime.utc(2026, 7, 30, 11),
  updatedAt: DateTime.utc(2026, 7, 30, 11, 0, 1),
  stableFailure: const PracticeRetryFailure(
    reason: PracticeRetryFailureReason.sourceNoLongerAvailable,
    retryable: false,
  ),
  completedAt: DateTime.utc(2026, 7, 30, 11, 0, 1),
);

const _review = AgentReview(
  id: 'review_daily_001',
  title: 'Daily review',
  summary: 'Summary',
  strength: 'Strength',
  nextFocus: 'Next focus',
);

final _completedDailySnapshot = PracticeSessionSnapshot(
  sessionId: 'session_daily_001',
  threadId: 'thread_daily_001',
  scenarioType: 'DAILY',
  scenarioModel: 'DAILY_BASIC_DIALOGUE',
  matter: AgentMatter(
    id: 'matter_daily_001',
    scene: AgentScene(
      id: 'scene_daily_001',
      title: 'Daily',
      description: 'Daily practice',
    ),
  ),
  completedTurns: 3,
  turnLimit: 3,
  sessionCompleted: true,
  review: _review,
  turnHistory: const [
    PracticeTurnExchange(
      question: PracticeQuestion(
        id: 'question_daily_001',
        sessionId: 'session_daily_001',
        text: 'How did you solve it?',
      ),
      turn: PracticeTurnSnapshot(
        id: 'turn_daily_001',
        sessionId: 'session_daily_001',
        questionId: 'question_daily_001',
        respondentParticipantId: 'participant_user',
        candidateId: 'candidate_daily_001',
        answerText: 'I explain the issue clearly.',
        evidenceVersion: 1,
        effectiveTurns: 3,
        sessionCompleted: true,
      ),
    ),
  ],
);
