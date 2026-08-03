enum IeltsSpeakingReportEvaluationStatus { queued, running, ready, failed }

enum IeltsSpeakingReportScoreabilityStatus { provisional, insufficient }

enum IeltsSpeakingReportGateStatus { feedbackOnly, blocked }

enum IeltsSpeakingCriterionId {
  fluencyAndCoherence,
  lexicalResource,
  grammaticalRangeAndAccuracy,
  pronunciation,
}

enum IeltsSpeakingPartId { part1, part2, part3 }

enum IeltsSpeakingOpportunityStatus { provided, notProvided }

enum IeltsSpeakingAssessmentStatus { assessed, notAssessed }

enum IeltsSpeakingOverallStatus { available, notAvailable }

enum IeltsSpeakingTargetPlanStatus { notConfigured }

final class IeltsSpeakingReportEnvelope {
  const IeltsSpeakingReportEnvelope({
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
  final IeltsSpeakingReportEvaluationStatus evaluationStatus;
  final bool isFinal;
  final String statusUrl;
  final IeltsSpeakingReport? report;
  final IeltsSpeakingReportStableFailure? stableFailure;
}

final class IeltsSpeakingReportStableFailure {
  const IeltsSpeakingReportStableFailure({
    required this.reasonCode,
    required this.retryable,
  });

  final String reasonCode;
  final bool retryable;
}

final class IeltsSpeakingReport {
  const IeltsSpeakingReport({
    required this.schemaVersion,
    required this.disclaimerCode,
    required this.disclaimer,
    required this.scoreabilityStatus,
    required this.gateStatus,
    required this.testSummary,
    required this.criteria,
    required this.speakingOverallStatus,
    required this.speakingOverallExplanation,
    required this.partReviews,
    required this.questions,
    required this.targetPlanStatus,
    required this.priorityActions,
    this.speakingOverallBand,
  });

  final String schemaVersion;
  final String disclaimerCode;
  final String disclaimer;
  final IeltsSpeakingReportScoreabilityStatus scoreabilityStatus;
  final IeltsSpeakingReportGateStatus gateStatus;
  final IeltsSpeakingTestSummary testSummary;
  final List<IeltsSpeakingCriterion> criteria;
  final IeltsSpeakingOverallStatus speakingOverallStatus;
  final double? speakingOverallBand;
  final String speakingOverallExplanation;
  final List<IeltsSpeakingPartReview> partReviews;
  final List<IeltsSpeakingQuestionReview> questions;
  final IeltsSpeakingTargetPlanStatus targetPlanStatus;
  final List<IeltsSpeakingPriorityAction> priorityActions;

  IeltsSpeakingFinding? finding(String findingId) {
    for (final criterion in criteria) {
      for (final finding in [
        ...criterion.strengths,
        ...criterion.improvements,
        ...criterion.upgradeExamples,
      ]) {
        if (finding.id == findingId) {
          return finding;
        }
      }
    }
    return null;
  }
}

final class IeltsSpeakingTestSummary {
  const IeltsSpeakingTestSummary({
    required this.part1Topic,
    required this.part2Topic,
    required this.part3Topic,
    required this.questionCount,
    required this.answeredCount,
    required this.recordingDurationMs,
  });

  final String part1Topic;
  final String part2Topic;
  final String part3Topic;
  final int questionCount;
  final int answeredCount;
  final int recordingDurationMs;
}

final class IeltsSpeakingCriterion {
  const IeltsSpeakingCriterion({
    required this.id,
    required this.scoreabilityStatus,
    required this.gateStatus,
    required this.explanation,
    required this.coverage,
    required this.confidence,
    required this.reasonCodes,
    required this.evidenceRefIds,
    required this.strengths,
    required this.improvements,
    required this.upgradeExamples,
    this.estimatedBand,
    this.bandDescriptor,
  });

  final IeltsSpeakingCriterionId id;
  final IeltsSpeakingReportScoreabilityStatus scoreabilityStatus;
  final IeltsSpeakingReportGateStatus gateStatus;
  final String explanation;
  final int? estimatedBand;
  final String? bandDescriptor;
  final double coverage;
  final double confidence;
  final List<String> reasonCodes;
  final List<String> evidenceRefIds;
  final List<IeltsSpeakingFinding> strengths;
  final List<IeltsSpeakingFinding> improvements;
  final List<IeltsSpeakingFinding> upgradeExamples;
}

final class IeltsSpeakingFinding {
  const IeltsSpeakingFinding({
    required this.id,
    required this.message,
    required this.evidence,
    this.suggestion,
  });

  final String id;
  final String message;
  final String? suggestion;
  final List<IeltsSpeakingEvidence> evidence;
}

final class IeltsSpeakingEvidence {
  const IeltsSpeakingEvidence({
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

final class IeltsSpeakingPartReview {
  const IeltsSpeakingPartReview({
    required this.id,
    required this.questionIndexes,
    required this.evidenceRefIds,
    required this.strengthFindingIds,
    required this.improvementFindingIds,
    required this.upgradeExampleFindingIds,
  });

  final IeltsSpeakingPartId id;
  final List<int> questionIndexes;
  final List<String> evidenceRefIds;
  final List<String> strengthFindingIds;
  final List<String> improvementFindingIds;
  final List<String> upgradeExampleFindingIds;
}

final class IeltsSpeakingQuestionReview {
  const IeltsSpeakingQuestionReview({
    required this.questionId,
    required this.partId,
    required this.index,
    required this.questionText,
    required this.opportunityStatus,
    required this.assessmentStatus,
    required this.evidenceRefIds,
    required this.criterionFindings,
    this.confirmedTranscript,
    this.responseTurnId,
  });

  final String questionId;
  final IeltsSpeakingPartId partId;
  final int index;
  final String questionText;
  final IeltsSpeakingOpportunityStatus opportunityStatus;
  final IeltsSpeakingAssessmentStatus assessmentStatus;
  final String? confirmedTranscript;
  final String? responseTurnId;
  final List<String> evidenceRefIds;
  final List<IeltsSpeakingQuestionCriterionFindings> criterionFindings;
}

final class IeltsSpeakingQuestionCriterionFindings {
  const IeltsSpeakingQuestionCriterionFindings({
    required this.criterionId,
    required this.strengthFindingIds,
    required this.improvementFindingIds,
    required this.upgradeExampleFindingIds,
  });

  final IeltsSpeakingCriterionId criterionId;
  final List<String> strengthFindingIds;
  final List<String> improvementFindingIds;
  final List<String> upgradeExampleFindingIds;
}

final class IeltsSpeakingPriorityAction {
  const IeltsSpeakingPriorityAction({
    required this.criterionId,
    required this.findingId,
  });

  final IeltsSpeakingCriterionId criterionId;
  final String findingId;
}
