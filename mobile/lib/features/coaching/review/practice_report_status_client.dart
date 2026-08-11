import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';

enum PracticeReportStatusFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  network,
  server,
  invalidResponse,
  superseded,
}

final class PracticeReportStatusException implements Exception {
  const PracticeReportStatusException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final PracticeReportStatusFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'PracticeReportStatusException(kind: ${kind.name})';
}

abstract interface class PracticeReportStatusClient {
  Future<PracticeReportStatus> getStatus(String practiceSessionId);

  Future<EvaluationReport> getReadyReport(PracticeReportRef reportRef);

  Future<void> clearAccountState();
}

abstract interface class PracticeReportRegenerationClient {
  Future<void> regenerateReport(PracticeReportStatus status);
}
