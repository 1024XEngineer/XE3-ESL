import '../../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';

import '../../support/practice_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'completed Daily retry cancel, rerecord, confirm and cleanup stay isolated',
    () async {
      final practice = _RetryPracticeClient(_completedDailySnapshot);
      final controller = PracticeController(client: practice);
      addTearDown(controller.dispose);
      await _restoreCompletedDailyPractice(controller);

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
      final controller = PracticeController(client: practice);
      addTearDown(controller.dispose);
      await _restoreCompletedDailyPractice(controller);

      expect(await controller.startSpeechFeedbackRetry(_feedbackItem), isFalse);

      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.isSpeechFeedbackRetryActive, isFalse);
      expect(controller.errorMessage, contains('原题'));
      expect(practice.transcriptions, 0);
    },
  );
}

Future<void> _restoreCompletedDailyPractice(
  PracticeController controller,
) async {
  await controller.restoreCreatedPractice(
    sessionId: _completedDailySnapshot.sessionId,
    scene: _dailyScene,
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
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async => snapshot;

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeRetryTurn> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
  }) async {
    expect(feedbackItemId, _feedbackItem.feedbackItemId);
    if (failCreation) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.conflict,
        errorCode: 'source_no_longer_available',
      );
    }
    return _retryTurn;
  }

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
      sessionId: _retryTurn.sessionId,
      questionId: _retryTurn.questionId,
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
      originalTurnId: _retryTurn.originalTurnId,
      sessionId: _retryTurn.sessionId,
      questionId: _retryTurn.questionId,
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
  evaluationId: 'evaluation_daily_001',
  position: 1,
  kind: SpeechFeedbackItemKind.correction,
  anchor: const SpeechFeedbackAnchor(
    evidenceRefId: 'turn_daily_001',
    startUtf8Byte: 0,
    endUtf8Byte: 9,
    originalExcerpt: 'I explain',
  ),
  explanation: 'Use past tense.',
  suggestedText: 'I explained',
  repracticeMode: SpeechFeedbackRepracticeMode.sameQuestion,
  createdAt: DateTime.utc(2026, 7, 30, 10, 59),
);

final _retryTurn = PracticeRetryTurn(
  turnId: 'turn_retry_daily_001',
  sessionId: 'session_daily_001',
  originalTurnId: 'turn_daily_001',
  questionId: 'question_daily_001',
  sequence: 4,
  status: PracticeRetryTurnStatus.answering,
  createdAt: DateTime.utc(2026, 7, 30, 11),
  replayed: false,
);

final _dailyScene = testScene(
  id: 'scene_daily_001',
  experience: PracticeExperience.lifeAndTravel,
  category: SceneCategory.lifeDaily,
  name: 'Daily',
  prompt: const ScenePrompt(
    publicSceneBrief: 'Daily practice',
    practiceGoal: 'Complete the daily practice.',
    userRole: 'Learner',
    aiRole: 'Coach',
    personaSummary: 'Supportive and focused.',
    focusAreas: <String>['clarity'],
    turnBlueprints: <String>['Ask one daily-life question.'],
  ),
);

final _completedDailySnapshot = PracticeSessionSnapshot(
  sessionId: 'session_daily_001',
  planId: 'plan_daily_001',
  practiceExperience: PracticeExperience.lifeAndTravel,
  sceneCategory: SceneCategory.lifeDaily,
  practiceMode: PracticeMode.fullSimulation,
  capabilities: testPracticeCapabilities,
  sessionVersion: 1,
  completedTurns: 3,
  turnLimit: 3,
  sessionCompleted: true,
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
