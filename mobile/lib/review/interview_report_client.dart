import 'package:speakup/review/interview_report.dart';

enum InterviewReportFailureKind {
  authenticationRequired,
  notFound,
  conflict,
  network,
  server,
  invalidResponse,
  superseded,
}

final class InterviewReportException implements Exception {
  const InterviewReportException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final InterviewReportFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'InterviewReportException(kind: ${kind.name})';
}

abstract interface class InterviewReportClient {
  Future<InterviewReportEnvelope> getReport(String practiceSessionId);

  Future<void> clearAccountState();
}
