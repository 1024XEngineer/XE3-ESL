import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/review/formal_review.dart';

const _practiceScenarioTypes = <String>{
  'INTERVIEW',
  'EXAM',
  'WORKPLACE',
  'DAILY',
};

const _practiceScenarioModels = <String>{
  'PROJECT_EXPERIENCE_DEEP_DIVE',
  'INTERVIEW_BASIC_DIALOGUE',
  'IELTS_SPEAKING_PART_1',
  'IELTS_SPEAKING_PART_2',
  'IELTS_SPEAKING_PART_3',
  'IELTS_SPEAKING_FULL_MOCK',
  'EXAM_BASIC_DIALOGUE',
  'PROGRESS_AND_RISK_UPDATE',
  'WORKPLACE_BASIC_DIALOGUE',
  'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
  'DAILY_BASIC_DIALOGUE',
};

bool validPracticeScenarioIdentity(
  String? scenarioType,
  String? scenarioModel, {
  bool allowMissing = false,
}) {
  if (scenarioType == null || scenarioModel == null) {
    return allowMissing && scenarioType == null && scenarioModel == null;
  }
  return _practiceScenarioTypes.contains(scenarioType) &&
      _practiceScenarioModels.contains(scenarioModel);
}

bool isIeltsSpeakingFullMockScenario(
  String? scenarioType,
  String? scenarioModel,
) => scenarioType == 'EXAM' && scenarioModel == 'IELTS_SPEAKING_FULL_MOCK';

bool isInterviewPracticeScenario(String? scenarioType, String? scenarioModel) =>
    scenarioType == 'INTERVIEW' &&
    (scenarioModel == 'PROJECT_EXPERIENCE_DEEP_DIVE' ||
        scenarioModel == 'INTERVIEW_BASIC_DIALOGUE');

bool usesAsynchronousPracticeReport(
  String? scenarioType,
  String? scenarioModel,
) =>
    isInterviewPracticeScenario(scenarioType, scenarioModel) ||
    isIeltsSpeakingFullMockScenario(scenarioType, scenarioModel);

bool isTurnFeedbackEligiblePracticeScenario(
  String? scenarioType,
  String? scenarioModel,
) =>
    (scenarioType == 'WORKPLACE' &&
        (scenarioModel == 'PROGRESS_AND_RISK_UPDATE' ||
            scenarioModel == 'WORKPLACE_BASIC_DIALOGUE')) ||
    (scenarioType == 'DAILY' &&
        (scenarioModel == 'HOTEL_CHECKIN_AND_ISSUE_HANDLING' ||
            scenarioModel == 'DAILY_BASIC_DIALOGUE'));

final class PracticeQuestion {
  const PracticeQuestion({
    required this.id,
    required this.sessionId,
    required this.text,
    this.questionType = 'PRIMARY',
    this.parentQuestionId,
    this.speakerParticipantId,
    this.addresseeParticipantIds = const <String>[],
    this.speechPath,
  });

  final String id;
  final String sessionId;
  final String text;
  final String questionType;
  final String? parentQuestionId;
  final String? speakerParticipantId;
  final List<String> addresseeParticipantIds;
  final String? speechPath;

  bool get isFollowUp => questionType == 'FOLLOW_UP';

  AgentMessage get presentation =>
      AgentMessage(id: id, role: AgentMessageRole.assistant, text: text);
}

/// The server-authoritative practice projection consumed by Flutter.
///
/// A Thread only provides conversational continuity. Every practice resource
/// keeps its own opaque identity; in particular, [sessionId] is never derived
/// from or replaced by an Agent Thread ID.
final class PracticeSessionSnapshot {
  const PracticeSessionSnapshot({
    required this.sessionId,
    this.planId,
    this.threadId,
    this.scenarioType,
    this.scenarioModel,
    this.sessionVersion,
    required this.matter,
    required this.completedTurns,
    required this.turnLimit,
    required this.sessionCompleted,
    this.currentQuestion,
    this.currentTurn,
    this.turnHistory = const <PracticeTurnExchange>[],
    this.review,
    this.formalReview,
  });

  final String sessionId;
  final String? planId;
  final String? threadId;
  final String? scenarioType;
  final String? scenarioModel;
  final int? sessionVersion;
  final AgentMatter matter;
  final int completedTurns;
  final int turnLimit;
  final bool sessionCompleted;
  final PracticeQuestion? currentQuestion;
  final PracticeTurnSnapshot? currentTurn;
  final List<PracticeTurnExchange> turnHistory;
  final AgentReview? review;
  final FormalReview? formalReview;
}

final class PracticeTurnExchange {
  const PracticeTurnExchange({required this.question, required this.turn});

  final PracticeQuestion question;
  final PracticeTurnSnapshot turn;
}

final class PracticeTurnSnapshot {
  const PracticeTurnSnapshot({
    required this.id,
    required this.sessionId,
    required this.questionId,
    required this.respondentParticipantId,
    required this.candidateId,
    required this.answerText,
    required this.evidenceVersion,
    required this.effectiveTurns,
    required this.sessionCompleted,
    this.countsTowardEffectiveTurnLimit = true,
    this.reviewId,
    this.audioAssetId,
    this.speechFeedbackStatusUrl,
  });

  final String id;
  final String sessionId;
  final String questionId;
  final String respondentParticipantId;
  final String candidateId;
  final String answerText;
  final int evidenceVersion;
  final int effectiveTurns;
  final bool sessionCompleted;
  final bool countsTowardEffectiveTurnLimit;
  final String? reviewId;
  final String? audioAssetId;
  final String? speechFeedbackStatusUrl;
}

final class PracticeStartResult {
  const PracticeStartResult({required this.snapshot});

  final PracticeSessionSnapshot snapshot;
}

/// A server-issued handle for one confirmed recording in the active Session.
///
/// Flutter may retain handles learned during the current in-memory Session.
/// After restore, only the latest handle present in the server projection is
/// available; the client never invents a recording history.
final class PracticeRecordingReference {
  const PracticeRecordingReference({
    required this.audioAssetId,
    required this.effectiveTurn,
  });

  final String audioAssetId;
  final int effectiveTurn;
}

/// Candidate ASR text. It is not an effective Turn until explicitly confirmed.
final class TranscriptionCandidate {
  const TranscriptionCandidate({
    required this.id,
    required this.sessionId,
    required this.questionId,
    required this.text,
    this.respondentParticipantId,
    this.transcriptId,
    this.evidenceVersion,
    this.createdAt,
  });

  final String id;
  final String sessionId;
  final String questionId;
  final String text;
  final String? respondentParticipantId;
  final String? transcriptId;
  final int? evidenceVersion;
  final DateTime? createdAt;
}

final class PracticeTurnConfirmation {
  const PracticeTurnConfirmation({
    required this.turnId,
    required this.sessionId,
    required this.questionId,
    required this.candidateId,
    required this.answer,
    required this.completedTurns,
    required this.turnLimit,
    required this.sessionCompleted,
    this.scenarioType,
    this.scenarioModel,
    this.sessionVersion,
    this.nextQuestion,
    this.review,
    this.formalReview,
    this.audioAssetId,
    this.speechFeedbackStatusUrl,
  });

  final String turnId;
  final String sessionId;
  final String questionId;
  final String candidateId;
  final AgentMessage answer;
  final int completedTurns;
  final int turnLimit;
  final bool sessionCompleted;
  final String? scenarioType;
  final String? scenarioModel;
  final int? sessionVersion;
  final PracticeQuestion? nextQuestion;
  final AgentReview? review;
  final FormalReview? formalReview;
  final String? audioAssetId;
  final String? speechFeedbackStatusUrl;
}

enum PracticeRetryRequestStatus { pending, turnCreated, failed }

enum PracticeRetryFailureReason {
  sourceNoLongerAvailable,
  retryTurnCreationFailed,
}

final class PracticeRetryFailure {
  const PracticeRetryFailure({required this.reason, required this.retryable});

  final PracticeRetryFailureReason reason;
  final bool retryable;
}

/// Review-owned creation state for one same-question retry Turn.
final class PracticeRetryRequest {
  const PracticeRetryRequest({
    required this.retryRequestId,
    required this.feedbackItemId,
    required this.sessionId,
    required this.originalTurnId,
    required this.questionId,
    required this.retryStatus,
    required this.statusUrl,
    required this.createdAt,
    required this.updatedAt,
    this.newTurnId,
    this.answerPath,
    this.stableFailure,
    this.completedAt,
  });

  final String retryRequestId;
  final String feedbackItemId;
  final String sessionId;
  final String originalTurnId;
  final String questionId;
  final PracticeRetryRequestStatus retryStatus;
  final String statusUrl;
  final DateTime createdAt;
  final DateTime updatedAt;
  final String? newTurnId;
  final String? answerPath;
  final PracticeRetryFailure? stableFailure;
  final DateTime? completedAt;
}

/// Ready ASR text bound to a server-created retry Turn draft.
final class RetryTranscriptionCandidate {
  const RetryTranscriptionCandidate({
    required this.id,
    required this.retryTurnId,
    required this.retryRequestId,
    required this.sessionId,
    required this.questionId,
    required this.respondentParticipantId,
    required this.transcriptId,
    required this.evidenceVersion,
    required this.text,
    required this.createdAt,
  });

  final String id;
  final String retryTurnId;
  final String retryRequestId;
  final String sessionId;
  final String questionId;
  final String respondentParticipantId;
  final String transcriptId;
  final int evidenceVersion;
  final String text;
  final DateTime createdAt;
}

/// Confirmation of a retry Turn that never advances Practice progress.
final class ConfirmedRetryTurn {
  const ConfirmedRetryTurn({
    required this.turnId,
    required this.retryRequestId,
    required this.originalTurnId,
    required this.sessionId,
    required this.questionId,
    required this.respondentParticipantId,
    required this.candidateId,
    required this.answerText,
    required this.evidenceVersion,
    required this.countsTowardTurnLimit,
    required this.createdAt,
    required this.confirmedAt,
    this.audioAssetId,
  });

  final String turnId;
  final String retryRequestId;
  final String originalTurnId;
  final String sessionId;
  final String questionId;
  final String respondentParticipantId;
  final String candidateId;
  final String answerText;
  final int evidenceVersion;
  final bool countsTowardTurnLimit;
  final String? audioAssetId;
  final DateTime createdAt;
  final DateTime confirmedAt;
}

enum PracticeSessionLifecycleStatus {
  starting,
  inProgress,
  paused,
  completed,
  endedEarly,
}

enum CompletedPracticeRouteResult { continueWithAgent }

final class PracticeSessionLifecycle {
  const PracticeSessionLifecycle({
    required this.sessionId,
    required this.status,
    required this.version,
  });

  final String sessionId;
  final PracticeSessionLifecycleStatus status;
  final int version;
}

final class PracticeTranscriptionRequest {
  const PracticeTranscriptionRequest({
    required this.sessionId,
    required this.questionId,
    required this.clientTurnId,
    required this.audio,
  });

  final String sessionId;
  final String questionId;
  final String clientTurnId;
  final RecordedPracticeAudio audio;
}
