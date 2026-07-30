enum SpeechFeedbackStatus { queued, running, ready, failed }

enum SpeechFeedbackScoreabilityStatus { provisional, insufficient }

enum SpeechFeedbackGateStatus { feedbackOnly, blocked }

enum SpeechFeedbackItemKind {
  correction,
  strength,
  improvement,
  recommendedExpression,
}

enum SpeechFeedbackRepracticeMode { none, sameQuestion, sameThread }

enum SpeechFeedbackAssessmentStatus { notAssessed }

bool validSpeechFeedbackStatusUrl(String value) {
  if (value.length > 160 || !_speechFeedbackStatusUrlPattern.hasMatch(value)) {
    return false;
  }
  final opaqueId = value.substring('/v1/speech-feedback/'.length);
  return opaqueId != '.' && opaqueId != '..';
}

sealed class SpeechFeedbackSource {
  const SpeechFeedbackSource();
}

final class ConversationTurnFeedbackSource extends SpeechFeedbackSource {
  const ConversationTurnFeedbackSource({
    required this.practiceSessionId,
    required this.turnId,
    required this.inputRevision,
    required this.evidenceSnapshotId,
  });

  final String practiceSessionId;
  final String turnId;
  final int inputRevision;
  final String evidenceSnapshotId;
}

final class AgentVoiceMessageFeedbackSource extends SpeechFeedbackSource {
  const AgentVoiceMessageFeedbackSource({
    required this.threadId,
    required this.messageId,
    required this.transcriptEvidenceId,
    required this.candidateVersion,
  });

  final String threadId;
  final String messageId;
  final String transcriptEvidenceId;
  final int candidateVersion;
}

sealed class SpeechFeedbackAnchor {
  const SpeechFeedbackAnchor({
    required this.startUtf8Byte,
    required this.endUtf8Byte,
    required this.originalExcerpt,
  });

  final int startUtf8Byte;
  final int endUtf8Byte;
  final String originalExcerpt;
}

final class ConversationTranscriptFeedbackAnchor extends SpeechFeedbackAnchor {
  const ConversationTranscriptFeedbackAnchor({
    required this.evidenceRefId,
    required this.turnId,
    required super.startUtf8Byte,
    required super.endUtf8Byte,
    required super.originalExcerpt,
  });

  final String evidenceRefId;
  final String turnId;
}

final class AgentTranscriptFeedbackAnchor extends SpeechFeedbackAnchor {
  const AgentTranscriptFeedbackAnchor({
    required this.transcriptEvidenceId,
    required this.messageId,
    required super.startUtf8Byte,
    required super.endUtf8Byte,
    required super.originalExcerpt,
  });

  final String transcriptEvidenceId;
  final String messageId;
}

final class SpeechFeedbackItem {
  const SpeechFeedbackItem({
    required this.feedbackItemId,
    required this.speechFeedbackId,
    required this.kind,
    required this.anchor,
    required this.explanation,
    required this.repracticeMode,
    required this.createdAt,
    this.suggestedText,
  });

  final String feedbackItemId;
  final String speechFeedbackId;
  final SpeechFeedbackItemKind kind;
  final SpeechFeedbackAnchor anchor;
  final String explanation;
  final String? suggestedText;
  final SpeechFeedbackRepracticeMode repracticeMode;
  final DateTime createdAt;

  bool get canRepractice => repracticeMode != SpeechFeedbackRepracticeMode.none;
}

final class SpeechFeedbackAcousticAssessment {
  const SpeechFeedbackAcousticAssessment({
    required this.pronunciation,
    required this.acousticFluency,
    required this.reasonCode,
  });

  final SpeechFeedbackAssessmentStatus pronunciation;
  final SpeechFeedbackAssessmentStatus acousticFluency;
  final String reasonCode;
}

final class SpeechFeedbackStableFailure {
  const SpeechFeedbackStableFailure({
    required this.reasonCode,
    required this.retryable,
  });

  final String reasonCode;
  final bool retryable;
}

final class SpeechFeedback {
  const SpeechFeedback({
    required this.speechFeedbackId,
    required this.source,
    required this.feedbackStatus,
    required this.schemaVersion,
    required this.strategyRef,
    required this.pipelineVersion,
    required this.isFinal,
    required this.items,
    required this.acousticAssessment,
    required this.statusUrl,
    required this.createdAt,
    required this.updatedAt,
    required this.reasonCodes,
    this.scoreabilityStatus,
    this.gateStatus,
    this.stableFailure,
    this.completedAt,
  });

  final String speechFeedbackId;
  final SpeechFeedbackSource source;
  final SpeechFeedbackStatus feedbackStatus;
  final SpeechFeedbackScoreabilityStatus? scoreabilityStatus;
  final SpeechFeedbackGateStatus? gateStatus;
  final List<String> reasonCodes;
  final String schemaVersion;
  final String strategyRef;
  final String pipelineVersion;
  final bool isFinal;
  final List<SpeechFeedbackItem> items;
  final SpeechFeedbackAcousticAssessment acousticAssessment;
  final SpeechFeedbackStableFailure? stableFailure;
  final String statusUrl;
  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? completedAt;

  bool get isPending =>
      feedbackStatus == SpeechFeedbackStatus.queued ||
      feedbackStatus == SpeechFeedbackStatus.running;
}

final _speechFeedbackStatusUrlPattern = RegExp(
  r'^/v1/speech-feedback/[A-Za-z0-9._~-]{1,128}$',
);
