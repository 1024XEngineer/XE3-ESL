import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/review_summary.dart';

ReviewSummary presentEvaluationReport(EvaluationReport report) {
  final strength =
      report.dimensions
          .expand((dimension) => dimension.strengths)
          .map((finding) => finding.message)
          .firstOrNull ??
      report.summary;
  final findings = <String, EvaluationReportFinding>{
    for (final dimension in report.dimensions)
      for (final finding in <EvaluationReportFinding>[
        ...dimension.strengths,
        ...dimension.improvements,
        ...dimension.recommendedExamples,
      ])
        '${dimension.key}:${finding.id}': finding,
  };
  String? nextFocus;
  for (final action in report.priorityActions) {
    final finding = findings['${action.dimensionKey}:${action.findingId}'];
    if (finding != null) {
      nextFocus = finding.suggestion ?? finding.message;
      break;
    }
  }
  nextFocus ??=
      report.dimensions
          .expand((dimension) => dimension.improvements)
          .map((finding) => finding.suggestion ?? finding.message)
          .firstOrNull ??
      report.summary;
  return ReviewSummary(
    id: report.id,
    title: evaluationReportTitle(report),
    summary: report.summary,
    strength: strength,
    nextFocus: nextFocus,
  );
}

String evaluationReportTitle(EvaluationReport report) {
  final base = switch (report.sceneType) {
    EvaluationReportSceneType.interview => '面试复盘',
    EvaluationReportSceneType.ieltsSpeaking => switch (report.practiceMode) {
      'PART_1' => 'Part 1 专项复盘',
      'PART_2' => 'Part 2 + Part 3 联合复盘',
      'PART_3' => 'Part 3 专项复盘',
      'FULL_MOCK' => 'IELTS 口语模考报告',
      _ => 'IELTS 口语复盘',
    },
    EvaluationReportSceneType.overseasDailyLife => '日常英语复盘',
    EvaluationReportSceneType.overseasWorkplace => '职场英语复盘',
  };
  return report.scoreability == EvaluationReportScoreability.insufficient
      ? '$base · 证据不足'
      : base;
}

String evaluationReportBadge(EvaluationReport report) =>
    switch (report.sceneType) {
      EvaluationReportSceneType.ieltsSpeaking => switch (report.practiceMode) {
        'PART_1' => 'IELTS Part 1',
        'PART_2' => 'IELTS Part 2',
        'PART_3' => 'IELTS Part 3',
        'FULL_MOCK' => 'IELTS Full Mock',
        _ => 'IELTS Speaking',
      },
      EvaluationReportSceneType.interview => '模拟面试',
      EvaluationReportSceneType.overseasDailyLife => '日常沟通',
      EvaluationReportSceneType.overseasWorkplace => '职场沟通',
    };

String evaluationReportContextLabel(EvaluationReport report) {
  final answeredCount = report.questions
      .where((question) => question.answer != null)
      .length;
  return switch (report.sceneType) {
    EvaluationReportSceneType.ieltsSpeaking =>
      '基于本次 $answeredCount 道已记录回答的阶段性估分，不等同于官方考试成绩。',
    EvaluationReportSceneType.interview =>
      '基于本次模拟面试中 $answeredCount 道已记录回答形成的阶段性评估。',
    EvaluationReportSceneType.overseasDailyLife =>
      '基于本次 $answeredCount 道已记录回答形成的日常沟通评估。',
    EvaluationReportSceneType.overseasWorkplace =>
      '基于本次 $answeredCount 道已记录回答形成的职场沟通评估。',
  };
}
