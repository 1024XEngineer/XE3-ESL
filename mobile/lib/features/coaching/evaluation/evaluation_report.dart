enum EvaluationReportSceneType {
  ieltsSpeaking,
  interview,
  overseasDailyLife,
  overseasWorkplace,
}

enum EvaluationReportScoreability { provisional, insufficient }

enum EvaluationReportScoreScale { percentage100, ieltsBand }

final class EvaluationReport {
  const EvaluationReport({
    required this.id,
    required this.evaluationId,
    required this.practiceSessionId,
    required this.sceneType,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.scoreability,
    required this.summary,
    required this.questions,
    required this.dimensions,
    required this.priorityActions,
    required this.createdAt,
  });

  final String id;
  final String evaluationId;
  final String practiceSessionId;
  final EvaluationReportSceneType sceneType;
  final String practiceExperience;
  final String sceneCategory;
  final String practiceMode;
  final EvaluationReportScoreability scoreability;
  final String summary;
  final List<EvaluationReportQuestion> questions;
  final List<EvaluationReportDimension> dimensions;
  final List<EvaluationReportPriorityAction> priorityActions;
  final DateTime createdAt;
}

final class EvaluationReportQuestion {
  const EvaluationReportQuestion({
    required this.id,
    required this.position,
    required this.text,
    this.parentQuestionId,
    this.answer,
  });

  final String id;
  final int position;
  final String text;
  final String? parentQuestionId;
  final EvaluationReportAnswer? answer;
}

final class EvaluationReportAnswer {
  const EvaluationReportAnswer({
    required this.turnId,
    required this.transcript,
  });

  final String turnId;
  final String transcript;
}

final class EvaluationReportDimension {
  const EvaluationReportDimension({
    required this.key,
    required this.scale,
    required this.coverage,
    required this.confidence,
    required this.reasonCodes,
    required this.evidenceRefIds,
    required this.strengths,
    required this.improvements,
    required this.recommendedExamples,
    this.score,
  });

  final String key;
  final double? score;
  final EvaluationReportScoreScale scale;
  final double coverage;
  final double confidence;
  final List<String> reasonCodes;
  final List<String> evidenceRefIds;
  final List<EvaluationReportFinding> strengths;
  final List<EvaluationReportFinding> improvements;
  final List<EvaluationReportFinding> recommendedExamples;
}

final class EvaluationReportFinding {
  const EvaluationReportFinding({
    required this.id,
    required this.message,
    required this.evidence,
    this.suggestion,
  });

  final String id;
  final String message;
  final String? suggestion;
  final List<EvaluationReportEvidence> evidence;
}

final class EvaluationReportEvidence {
  const EvaluationReportEvidence({
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

final class EvaluationReportPriorityAction {
  const EvaluationReportPriorityAction({
    required this.dimensionKey,
    required this.findingId,
  });

  final String dimensionKey;
  final String findingId;
}
