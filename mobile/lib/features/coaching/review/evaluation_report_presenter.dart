import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';

final class EvaluationDimensionSpec {
  const EvaluationDimensionSpec({
    required this.label,
    required this.shortLabel,
    required this.order,
  });

  final String label;
  final String shortLabel;
  final int order;
}

const evaluationDimensionSpecs = <String, EvaluationDimensionSpec>{
  'FLUENCY_COHERENCE': EvaluationDimensionSpec(
    label: '流利度与连贯性',
    shortLabel: '流利与连贯',
    order: 10,
  ),
  'LEXICAL_RESOURCE': EvaluationDimensionSpec(
    label: '词汇资源',
    shortLabel: '词汇丰富度',
    order: 20,
  ),
  'GRAMMATICAL_RANGE_ACCURACY': EvaluationDimensionSpec(
    label: '语法范围与准确性',
    shortLabel: '语法准确性',
    order: 30,
  ),
  'PRONUNCIATION': EvaluationDimensionSpec(
    label: '发音',
    shortLabel: '发音',
    order: 40,
  ),
  'INTERVIEW_RELEVANCE': EvaluationDimensionSpec(
    label: '回答相关性',
    shortLabel: '相关性',
    order: 110,
  ),
  'INTERVIEW_STRUCTURE': EvaluationDimensionSpec(
    label: '回答结构',
    shortLabel: '结构',
    order: 120,
  ),
  'INTERVIEW_EVIDENCE': EvaluationDimensionSpec(
    label: '证据与说服力',
    shortLabel: '说服力',
    order: 130,
  ),
  'INTERVIEW_PROFESSIONAL': EvaluationDimensionSpec(
    label: '职业表达',
    shortLabel: '职业表达',
    order: 140,
  ),
  'INTERVIEW_INTERACTION': EvaluationDimensionSpec(
    label: '追问应对能力',
    shortLabel: '追问应对',
    order: 150,
  ),
  'TASK_ACHIEVEMENT': EvaluationDimensionSpec(
    label: '任务达成',
    shortLabel: '任务达成',
    order: 210,
  ),
  'CLARITY_COHERENCE': EvaluationDimensionSpec(
    label: '清晰度与连贯性',
    shortLabel: '清晰连贯',
    order: 220,
  ),
  'LANGUAGE_CONTROL': EvaluationDimensionSpec(
    label: '语言运用',
    shortLabel: '语言运用',
    order: 230,
  ),
  'INTERACTION': EvaluationDimensionSpec(
    label: '互动表现',
    shortLabel: '互动表现',
    order: 240,
  ),
};

final class EvaluationReportViewModel {
  const EvaluationReportViewModel({
    required this.pageTitle,
    required this.badgeLabel,
    required this.contextLabel,
    required this.overallScoreLabel,
    required this.scaleSuffix,
    required this.radarAxes,
    required this.dimensions,
    required this.questions,
    required this.insufficient,
    required this.hasPriorityDimensions,
  });

  final String pageTitle;
  final String badgeLabel;
  final String contextLabel;
  final String overallScoreLabel;
  final String scaleSuffix;
  final List<EvaluationRadarAxis> radarAxes;
  final List<EvaluationDimensionViewModel> dimensions;
  final List<EvaluationQuestionViewModel> questions;
  final bool insufficient;
  final bool hasPriorityDimensions;
}

final class EvaluationRadarAxis {
  const EvaluationRadarAxis({
    required this.label,
    required this.scoreLabel,
    required this.normalizedValue,
  });

  final String label;
  final String scoreLabel;
  final double? normalizedValue;
}

final class EvaluationDimensionViewModel {
  const EvaluationDimensionViewModel({
    required this.key,
    required this.label,
    required this.scoreLabel,
    required this.normalizedScore,
    required this.coverage,
    required this.findings,
    required this.suggestions,
    required this.prioritized,
    required this.unavailableReason,
  });

  final String key;
  final String label;
  final String scoreLabel;
  final double? normalizedScore;
  final double? coverage;
  final List<EvaluationFindingViewModel> findings;
  final List<String> suggestions;
  final bool prioritized;
  final String unavailableReason;
}

enum EvaluationFindingKind { improvement, strength }

final class EvaluationFindingViewModel {
  const EvaluationFindingViewModel({
    required this.id,
    required this.kind,
    required this.message,
    required this.evidenceExcerpts,
  });

  final String id;
  final EvaluationFindingKind kind;
  final String message;
  final List<String> evidenceExcerpts;
}

final class EvaluationQuestionViewModel {
  const EvaluationQuestionViewModel({
    required this.id,
    required this.position,
    required this.text,
    required this.answerText,
    required this.answered,
  });

  final String id;
  final int position;
  final String text;
  final String answerText;
  final bool answered;
}

EvaluationReportViewModel presentEvaluationReportDetail(
  EvaluationReport report,
) {
  final dimensionsByKey = <String, EvaluationReportDimension>{
    for (final dimension in report.dimensions) dimension.key: dimension,
  };
  final catalogKeys = evaluationDimensionKeys(report);
  final cardKeys = report.sceneType == EvaluationReportSceneType.ieltsSpeaking
      ? <String>[
          ...catalogKeys,
          ...report.dimensions
              .map((dimension) => dimension.key)
              .where((key) => !catalogKeys.contains(key)),
        ]
      : <String>[...report.dimensions.map((dimension) => dimension.key)];
  final priorityFindingIds = <String, List<String>>{};
  final priorityDimensionKeys = <String>[];
  for (final action in report.priorityActions) {
    final dimension = dimensionsByKey[action.dimensionKey];
    if (dimension == null || !cardKeys.contains(action.dimensionKey)) continue;
    if (!dimension.improvements.any((item) => item.id == action.findingId)) {
      continue;
    }
    final ids = priorityFindingIds.putIfAbsent(
      action.dimensionKey,
      () => <String>[],
    );
    if (!ids.contains(action.findingId)) ids.add(action.findingId);
    if (!priorityDimensionKeys.contains(action.dimensionKey)) {
      priorityDimensionKeys.add(action.dimensionKey);
    }
  }

  final dimensions =
      <EvaluationDimensionViewModel>[
        for (final key in cardKeys)
          _presentDimension(
            key,
            dimensionsByKey[key],
            priorityFindingIds[key] ?? const <String>[],
          ),
      ]..sort((left, right) {
        final leftPriority = priorityDimensionKeys.indexOf(left.key);
        final rightPriority = priorityDimensionKeys.indexOf(right.key);
        if (leftPriority >= 0 || rightPriority >= 0) {
          if (leftPriority < 0) return 1;
          if (rightPriority < 0) return -1;
          return leftPriority.compareTo(rightPriority);
        }
        final leftScore = left.normalizedScore;
        final rightScore = right.normalizedScore;
        if (leftScore == null && rightScore != null) return 1;
        if (leftScore != null && rightScore == null) return -1;
        if (leftScore != null && rightScore != null) {
          final scoreOrder = leftScore.compareTo(rightScore);
          if (scoreOrder != 0) return scoreOrder;
        }
        return _dimensionOrder(left.key).compareTo(_dimensionOrder(right.key));
      });

  final catalogDimensions = <EvaluationReportDimension?>[
    for (final key in catalogKeys) dimensionsByKey[key],
  ];
  final overall = _overallScore(catalogDimensions);
  final questions = <EvaluationQuestionViewModel>[
    for (final question in report.questions)
      EvaluationQuestionViewModel(
        id: question.id,
        position: question.position,
        text: question.text,
        answerText: question.answer?.transcript ?? '未作答',
        answered: question.answer != null,
      ),
  ]..sort((left, right) => left.position.compareTo(right.position));

  return EvaluationReportViewModel(
    pageTitle: evaluationReportTitle(report),
    badgeLabel: evaluationReportBadge(report),
    contextLabel: evaluationReportContextLabel(report),
    overallScoreLabel: overall == null ? '--' : evaluationScoreLabel(overall),
    scaleSuffix: evaluationScaleSuffix(_reportScale(report)),
    radarAxes: List<EvaluationRadarAxis>.unmodifiable([
      for (final key in catalogKeys)
        EvaluationRadarAxis(
          label: _dimensionSpec(key).shortLabel,
          scoreLabel: dimensionsByKey[key]?.score == null
              ? '--'
              : evaluationScoreLabel(dimensionsByKey[key]!.score!),
          normalizedValue: evaluationNormalizedScore(dimensionsByKey[key]),
        ),
    ]),
    dimensions: List<EvaluationDimensionViewModel>.unmodifiable(dimensions),
    questions: List<EvaluationQuestionViewModel>.unmodifiable(questions),
    insufficient:
        report.scoreability == EvaluationReportScoreability.insufficient,
    hasPriorityDimensions: priorityDimensionKeys.isNotEmpty,
  );
}

List<String> evaluationDimensionKeys(EvaluationReport report) =>
    switch (report.sceneType) {
      EvaluationReportSceneType.ieltsSpeaking => const <String>[
        'FLUENCY_COHERENCE',
        'LEXICAL_RESOURCE',
        'GRAMMATICAL_RANGE_ACCURACY',
        'PRONUNCIATION',
      ],
      EvaluationReportSceneType.interview => const <String>[
        'INTERVIEW_RELEVANCE',
        'INTERVIEW_STRUCTURE',
        'INTERVIEW_EVIDENCE',
        'INTERVIEW_PROFESSIONAL',
        'INTERVIEW_INTERACTION',
      ],
      EvaluationReportSceneType.overseasDailyLife ||
      EvaluationReportSceneType.overseasWorkplace => const <String>[
        'TASK_ACHIEVEMENT',
        'CLARITY_COHERENCE',
        'LANGUAGE_CONTROL',
        'INTERACTION',
      ],
    };

double evaluationScoreMaximum(EvaluationReportScoreScale scale) =>
    switch (scale) {
      EvaluationReportScoreScale.ieltsBand => 9,
      EvaluationReportScoreScale.percentage100 => 100,
    };

double? evaluationNormalizedScore(EvaluationReportDimension? dimension) {
  final score = dimension?.score;
  if (score == null || score < 0) return null;
  final maximum = evaluationScoreMaximum(dimension!.scale);
  if (score > maximum) return null;
  return (score / maximum).clamp(0, 1);
}

String evaluationScoreLabel(double value) => value == value.roundToDouble()
    ? value.toInt().toString()
    : value.toStringAsFixed(1);

String evaluationScaleSuffix(EvaluationReportScoreScale scale) =>
    scale == EvaluationReportScoreScale.ieltsBand ? '/ 9' : '/ 100';

String evaluationUnavailableReason(EvaluationReportDimension? dimension) {
  final reason = dimension?.reasonCodes.firstOrNull;
  return switch (reason) {
    'ACOUSTIC_ASSESSMENT_NOT_CONFIGURED' => '当前环境未启用发音评测。',
    'ACOUSTIC_ASSESSMENT_FAILED' => '本次发音评测失败，未使用文字推测发音。',
    'PRACTICE_TURN_AUDIO_UNAVAILABLE' => '本次录音不可用，无法形成发音依据。',
    'NO_EFFECTIVE_TURNS' => '本次没有足够的有效作答。',
    _ => '本项没有足够的评分依据。',
  };
}

EvaluationDimensionViewModel _presentDimension(
  String key,
  EvaluationReportDimension? dimension,
  List<String> priorityFindingIds,
) {
  final improvementById = <String, EvaluationReportFinding>{
    for (final finding in dimension?.improvements ?? const [])
      finding.id: finding,
  };
  final prioritized = <EvaluationReportFinding>[
    for (final id in priorityFindingIds)
      if (improvementById[id] != null) improvementById[id]!,
  ];
  final prioritizedIds = prioritized.map((item) => item.id).toSet();
  final orderedImprovements = <EvaluationReportFinding>[
    ...prioritized,
    ...?dimension?.improvements.where(
      (finding) => !prioritizedIds.contains(finding.id),
    ),
  ];
  final findings = <EvaluationFindingViewModel>[
    for (final finding in orderedImprovements)
      _presentFinding(finding, EvaluationFindingKind.improvement),
    for (final finding in dimension?.strengths ?? const [])
      _presentFinding(finding, EvaluationFindingKind.strength),
  ];
  final seenSuggestions = <String>{};
  final suggestions = <String>[];
  for (final finding in <EvaluationReportFinding>[
    ...orderedImprovements,
    ...?dimension?.recommendedExamples,
  ]) {
    final suggestion = (finding.suggestion ?? finding.message).trim();
    if (suggestion.isNotEmpty && seenSuggestions.add(suggestion)) {
      suggestions.add(suggestion);
    }
  }
  final score = dimension?.score;
  return EvaluationDimensionViewModel(
    key: key,
    label: _dimensionSpec(key).label,
    scoreLabel: score == null
        ? '未评分'
        : '${evaluationScoreLabel(score)} ${evaluationScaleSuffix(dimension!.scale)}',
    normalizedScore: evaluationNormalizedScore(dimension),
    coverage: dimension?.coverage,
    findings: List<EvaluationFindingViewModel>.unmodifiable(findings),
    suggestions: List<String>.unmodifiable(suggestions),
    prioritized: priorityFindingIds.isNotEmpty,
    unavailableReason: evaluationUnavailableReason(dimension),
  );
}

EvaluationFindingViewModel _presentFinding(
  EvaluationReportFinding finding,
  EvaluationFindingKind kind,
) => EvaluationFindingViewModel(
  id: finding.id,
  kind: kind,
  message: finding.message,
  evidenceExcerpts: List<String>.unmodifiable(
    finding.evidence.map((evidence) => evidence.originalExcerpt),
  ),
);

double? _overallScore(List<EvaluationReportDimension?> dimensions) {
  final scale = _commonScale(dimensions);
  if (scale == null) return null;
  final values = <double>[];
  for (final dimension in dimensions) {
    final score = dimension?.score;
    if (score == null || evaluationNormalizedScore(dimension) == null) {
      return null;
    }
    values.add(score);
  }
  final average = values.reduce((left, right) => left + right) / values.length;
  return switch (scale) {
    EvaluationReportScoreScale.ieltsBand => (average * 2).roundToDouble() / 2,
    EvaluationReportScoreScale.percentage100 => average.roundToDouble(),
  };
}

EvaluationReportScoreScale? _commonScale(
  List<EvaluationReportDimension?> dimensions,
) {
  if (dimensions.isEmpty || dimensions.any((item) => item == null)) return null;
  return _availableCommonScale(dimensions);
}

EvaluationReportScoreScale? _availableCommonScale(
  List<EvaluationReportDimension?> dimensions,
) {
  final scales = dimensions
      .whereType<EvaluationReportDimension>()
      .map((dimension) => dimension.scale)
      .toSet();
  return scales.length == 1 ? scales.single : null;
}

EvaluationDimensionSpec _dimensionSpec(String key) =>
    evaluationDimensionSpecs[key] ??
    EvaluationDimensionSpec(label: key, shortLabel: key, order: 1000);

int _dimensionOrder(String key) => _dimensionSpec(key).order;

EvaluationReportScoreScale _reportScale(EvaluationReport report) =>
    report.sceneType == EvaluationReportSceneType.ieltsSpeaking
    ? EvaluationReportScoreScale.ieltsBand
    : EvaluationReportScoreScale.percentage100;
