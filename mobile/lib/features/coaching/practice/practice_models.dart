import 'package:speakup/features/coaching/scene/scene.dart';

import 'package:speakup/features/coaching/practice/practice_recording.dart';

enum PracticeMessageRole { user, assistant }

final class PracticeMessage {
  const PracticeMessage({
    required this.id,
    required this.role,
    required this.text,
    this.speechFeedbackStatusUrl,
  });

  final String id;
  final PracticeMessageRole role;
  final String text;
  final String? speechFeedbackStatusUrl;
}

enum PracticeRecordingState {
  idle,
  starting,
  recording,
  transcribing,
  awaitingConfirmation,
  submitting,
  completed,
}

bool validPracticeSceneIdentity(
  SceneFamily? sceneFamily,
  SceneModel? sceneModel, {
  bool allowMissing = false,
}) {
  if (sceneFamily == null || sceneModel == null) {
    return allowMissing && sceneFamily == null && sceneModel == null;
  }
  return switch (sceneModel) {
    SceneModel.projectExperienceDeepDive ||
    SceneModel.interviewBasicDialogue => sceneFamily == SceneFamily.interview,
    SceneModel.ieltsSpeakingPart1 ||
    SceneModel.ieltsSpeakingPart2 ||
    SceneModel.ieltsSpeakingPart3 ||
    SceneModel.ieltsSpeakingFullMock ||
    SceneModel.examBasicDialogue => sceneFamily == SceneFamily.exam,
    SceneModel.progressAndRiskUpdate ||
    SceneModel.workplaceBasicDialogue => sceneFamily == SceneFamily.workplace,
    SceneModel.hotelCheckinAndIssueHandling ||
    SceneModel.dailyBasicDialogue => sceneFamily == SceneFamily.daily,
  };
}

bool isIeltsSpeakingFullMockScene(
  SceneFamily? sceneFamily,
  SceneModel? sceneModel,
) =>
    sceneFamily == SceneFamily.exam &&
    sceneModel == SceneModel.ieltsSpeakingFullMock;

bool isInterviewPracticeScene(
  SceneFamily? sceneFamily,
  SceneModel? sceneModel,
) =>
    sceneFamily == SceneFamily.interview &&
    (sceneModel == SceneModel.projectExperienceDeepDive ||
        sceneModel == SceneModel.interviewBasicDialogue);

bool isTurnFeedbackEligiblePracticeScene(
  SceneFamily? sceneFamily,
  SceneModel? sceneModel,
) =>
    (sceneFamily == SceneFamily.workplace &&
        (sceneModel == SceneModel.progressAndRiskUpdate ||
            sceneModel == SceneModel.workplaceBasicDialogue)) ||
    (sceneFamily == SceneFamily.daily &&
        (sceneModel == SceneModel.hotelCheckinAndIssueHandling ||
            sceneModel == SceneModel.dailyBasicDialogue));

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

  PracticeMessage get presentation =>
      PracticeMessage(id: id, role: PracticeMessageRole.assistant, text: text);
}

/// The server-authoritative practice projection consumed by Flutter.
///
/// The projection is always loaded by the explicit opaque [sessionId].
final class PracticeSessionSnapshot {
  const PracticeSessionSnapshot({
    required this.sessionId,
    required this.planId,
    required this.sceneFamily,
    required this.sceneModel,
    required this.sessionVersion,
    required this.completedTurns,
    required this.turnLimit,
    required this.sessionCompleted,
    this.currentQuestion,
    this.currentTurn,
    this.turnHistory = const <PracticeTurnExchange>[],
  });

  final String sessionId;
  final String planId;
  final SceneFamily sceneFamily;
  final SceneModel sceneModel;
  final int sessionVersion;
  final int completedTurns;
  final int turnLimit;
  final bool sessionCompleted;
  final PracticeQuestion? currentQuestion;
  final PracticeTurnSnapshot? currentTurn;
  final List<PracticeTurnExchange> turnHistory;
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
  final String? audioAssetId;
  final String? speechFeedbackStatusUrl;
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
    this.sceneFamily,
    this.sceneModel,
    this.sessionVersion,
    this.nextQuestion,
    this.audioAssetId,
    this.speechFeedbackStatusUrl,
  });

  final String turnId;
  final String sessionId;
  final String questionId;
  final String candidateId;
  final PracticeMessage answer;
  final int completedTurns;
  final int turnLimit;
  final bool sessionCompleted;
  final SceneFamily? sceneFamily;
  final SceneModel? sceneModel;
  final int? sessionVersion;
  final PracticeQuestion? nextQuestion;
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
