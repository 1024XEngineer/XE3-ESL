import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

const int practiceTurnSafetyLimit = 64;

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

enum PracticeCompletionMode {
  turnLimited,
  userControlled;

  static PracticeCompletionMode? fromWireValue(String value) => switch (value) {
    'TURN_LIMITED' => PracticeCompletionMode.turnLimited,
    'USER_CONTROLLED' => PracticeCompletionMode.userControlled,
    _ => null,
  };
}

final class PracticeCapabilities {
  const PracticeCapabilities({
    required this.retryAllowed,
    required this.questionTranslationAllowed,
    required this.questionTipsAllowed,
    required this.speechFeedbackAllowed,
  });

  final bool retryAllowed;
  final bool questionTranslationAllowed;
  final bool questionTipsAllowed;
  final bool speechFeedbackAllowed;

  @override
  bool operator ==(Object other) =>
      other is PracticeCapabilities &&
      other.retryAllowed == retryAllowed &&
      other.questionTranslationAllowed == questionTranslationAllowed &&
      other.questionTipsAllowed == questionTipsAllowed &&
      other.speechFeedbackAllowed == speechFeedbackAllowed;

  @override
  int get hashCode => Object.hash(
    retryAllowed,
    questionTranslationAllowed,
    questionTipsAllowed,
    speechFeedbackAllowed,
  );
}

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

final class PracticeQuestionTranslation {
  const PracticeQuestionTranslation({
    required this.questionId,
    required this.targetLanguage,
    required this.content,
  });

  final String questionId;
  final String targetLanguage;
  final String content;
}

final class PracticeQuestionTip {
  const PracticeQuestionTip({
    required this.id,
    required this.sessionId,
    required this.questionId,
    required this.content,
    required this.createdAt,
  });

  final String id;
  final String sessionId;
  final String questionId;
  final String content;
  final DateTime createdAt;
}

/// The server-authoritative practice projection consumed by Flutter.
///
/// The projection is always loaded by the explicit opaque [sessionId].
final class PracticeSessionSnapshot {
  const PracticeSessionSnapshot({
    required this.sessionId,
    required this.planId,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.capabilities,
    required this.sessionVersion,
    required this.completedTurns,
    required this.turnLimit,
    this.completionMode = PracticeCompletionMode.turnLimited,
    required this.sessionCompleted,
    this.ieltsAssignment,
    this.currentQuestion,
    this.currentTurn,
    this.turnHistory = const <PracticeTurnExchange>[],
  });

  final String sessionId;
  final String planId;
  final PracticeExperience practiceExperience;
  final SceneCategory sceneCategory;
  final PracticeMode practiceMode;
  final PracticeCapabilities capabilities;
  final int sessionVersion;
  final int completedTurns;
  final int turnLimit;
  final PracticeCompletionMode completionMode;
  final bool sessionCompleted;
  final IeltsPracticeAssignment? ieltsAssignment;
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
    this.completionMode = PracticeCompletionMode.turnLimited,
    required this.sessionCompleted,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.capabilities,
    required this.sessionVersion,
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
  final PracticeCompletionMode completionMode;
  final bool sessionCompleted;
  final PracticeExperience practiceExperience;
  final SceneCategory sceneCategory;
  final PracticeMode practiceMode;
  final PracticeCapabilities capabilities;
  final int sessionVersion;
  final PracticeQuestion? nextQuestion;
  final String? audioAssetId;
  final String? speechFeedbackStatusUrl;
}

enum PracticeRetryTurnStatus {
  answering,
  transcribing,
  transcribed,
  confirmed,
  failed,
}

/// Actor-owned same-question Turn created atomically from one feedback item.
final class PracticeRetryTurn {
  const PracticeRetryTurn({
    required this.turnId,
    required this.sessionId,
    required this.questionId,
    required this.originalTurnId,
    required this.sequence,
    required this.status,
    required this.createdAt,
    required this.replayed,
  });

  final String turnId;
  final String sessionId;
  final String questionId;
  final String originalTurnId;
  final int sequence;
  final PracticeRetryTurnStatus status;
  final DateTime createdAt;
  final bool replayed;

  String get answerPath => '/v1/retry-turns/$turnId/transcription-candidates';
}

/// Ready ASR text bound to a server-created retry Turn draft.
final class RetryTranscriptionCandidate {
  const RetryTranscriptionCandidate({
    required this.id,
    required this.retryTurnId,
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

enum CompletedPracticeRouteResult { returnToConversation }

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
