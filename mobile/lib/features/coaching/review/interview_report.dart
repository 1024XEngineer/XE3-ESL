enum InterviewReportEvaluationStatus { queued, running, ready, failed }

enum InterviewReportScoreabilityStatus { provisional, insufficient }

enum InterviewReportGateStatus { feedbackOnly, blocked }

enum InterviewReportReadinessLevel { notAssessed }

enum InterviewReportOpportunityStatus { provided, notProvided }

enum InterviewReportAssessmentStatus { assessed, notAssessed }

enum InterviewReportQuestionType { primary, followUp }

enum InterviewReportDimensionId {
  relevance,
  structure,
  evidence,
  professional,
  interaction,
}

final class InterviewReportEnvelope {
  const InterviewReportEnvelope({
    required this.practiceSessionId,
    required this.evaluationId,
    required this.evaluationRevisionId,
    required this.revision,
    required this.evaluationStatus,
    required this.isFinal,
    required this.statusUrl,
    this.report,
    this.stableFailure,
  });

  final String practiceSessionId;
  final String evaluationId;
  final String evaluationRevisionId;
  final int revision;
  final InterviewReportEvaluationStatus evaluationStatus;
  final bool isFinal;
  final String statusUrl;
  final InterviewReport? report;
  final InterviewReportStableFailure? stableFailure;
}

final class InterviewReportStableFailure {
  const InterviewReportStableFailure({
    required this.reasonCode,
    required this.retryable,
  });

  final String reasonCode;
  final bool retryable;
}

final class InterviewReport {
  const InterviewReport({
    required this.schemaVersion,
    required this.scoreabilityStatus,
    required this.gateStatus,
    required this.readinessLevel,
    required this.readinessNotice,
    required this.dimensions,
    required this.questions,
    required this.priorityActions,
  });

  final String schemaVersion;
  final InterviewReportScoreabilityStatus scoreabilityStatus;
  final InterviewReportGateStatus gateStatus;
  final InterviewReportReadinessLevel readinessLevel;
  final String readinessNotice;
  final List<InterviewReportDimension> dimensions;
  final List<InterviewReportQuestion> questions;
  final List<InterviewReportPriorityAction> priorityActions;

  InterviewReportFinding? finding(String findingId) {
    for (final dimension in dimensions) {
      for (final finding in [
        ...dimension.strengths,
        ...dimension.improvements,
        ...dimension.recommendedExpressions,
      ]) {
        if (finding.id == findingId) {
          return finding;
        }
      }
    }
    return null;
  }
}

final class InterviewReportDimension {
  const InterviewReportDimension({
    required this.id,
    required this.score,
    required this.scoreabilityStatus,
    required this.gateStatus,
    required this.coverage,
    required this.confidence,
    required this.reasonCodes,
    required this.evidenceRefIds,
    required this.strengths,
    required this.improvements,
    required this.recommendedExpressions,
  });

  final InterviewReportDimensionId id;
  final int? score;
  final InterviewReportScoreabilityStatus scoreabilityStatus;
  final InterviewReportGateStatus gateStatus;
  final double coverage;
  final double confidence;
  final List<String> reasonCodes;
  final List<String> evidenceRefIds;
  final List<InterviewReportFinding> strengths;
  final List<InterviewReportFinding> improvements;
  final List<InterviewReportFinding> recommendedExpressions;
}

final class InterviewReportFinding {
  const InterviewReportFinding({
    required this.id,
    required this.message,
    required this.evidence,
    this.suggestion,
  });

  final String id;
  final String message;
  final String? suggestion;
  final List<InterviewReportEvidence> evidence;
}

final class InterviewReportEvidence {
  const InterviewReportEvidence({
    required this.evidenceRefId,
    required this.turnId,
    required this.startUtf8Byte,
    required this.endUtf8Byte,
    required this.originalExcerpt,
  });

  final String evidenceRefId;
  final String turnId;
  final int startUtf8Byte;
  final int endUtf8Byte;
  final String originalExcerpt;
}

final class InterviewReportQuestion {
  const InterviewReportQuestion({
    required this.questionId,
    required this.questionType,
    required this.opportunityStatus,
    required this.questionText,
    required this.evidenceRefIds,
    required this.assessmentStatus,
    required this.dimensionFindings,
    this.parentQuestionId,
    this.responseTurnId,
    this.confirmedTranscript,
  });

  final String questionId;
  final InterviewReportQuestionType questionType;
  final String? parentQuestionId;
  final InterviewReportOpportunityStatus opportunityStatus;
  final InterviewReportAssessmentStatus assessmentStatus;
  final String questionText;
  final String? responseTurnId;
  final String? confirmedTranscript;
  final List<String> evidenceRefIds;
  final List<InterviewReportQuestionDimensionFindings> dimensionFindings;
}

final class InterviewReportQuestionDimensionFindings {
  const InterviewReportQuestionDimensionFindings({
    required this.dimensionId,
    required this.strengthFindingIds,
    required this.improvementFindingIds,
    required this.recommendedExpressionFindingIds,
  });

  final InterviewReportDimensionId dimensionId;
  final List<String> strengthFindingIds;
  final List<String> improvementFindingIds;
  final List<String> recommendedExpressionFindingIds;
}

final class InterviewReportPriorityAction {
  const InterviewReportPriorityAction({
    required this.dimensionId,
    required this.findingId,
  });

  final InterviewReportDimensionId dimensionId;
  final String findingId;
}
