import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

enum PracticeReportScope { part1, part2And3, part3, fullMock }

enum PracticeReportEvaluationStatus { queued, running, ready, failed }

final class PracticeReportRef {
  const PracticeReportRef({required this.reportId, required this.href});

  final String reportId;
  final String href;
}

final class PracticeReportStableFailure {
  const PracticeReportStableFailure({
    required this.reasonCode,
    required this.retryable,
  });

  final String reasonCode;
  final bool retryable;
}

final class PracticeReportStatus {
  const PracticeReportStatus({
    required this.practiceSessionId,
    required this.practiceMode,
    required this.reportScope,
    required this.availableSections,
    required this.detailSchema,
    required this.evaluationStatus,
    required this.statusUrl,
    this.evaluationId,
    this.evaluationRevisionId,
    this.revision,
    this.reportRef,
    this.scoreability,
    this.summary,
    this.stableFailure,
  });

  final String practiceSessionId;
  final PracticeMode practiceMode;
  final PracticeReportScope reportScope;
  final List<IeltsSpeakingPartId> availableSections;
  final String detailSchema;
  final PracticeReportEvaluationStatus evaluationStatus;
  final String statusUrl;
  final String? evaluationId;
  final String? evaluationRevisionId;
  final int? revision;
  final PracticeReportRef? reportRef;
  final EvaluationReportScoreability? scoreability;
  final String? summary;
  final PracticeReportStableFailure? stableFailure;
}
