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
