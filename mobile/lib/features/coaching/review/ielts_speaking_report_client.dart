import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';

enum IeltsSpeakingReportFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  network,
  server,
  invalidResponse,
  superseded,
}

final class IeltsSpeakingReportException implements Exception {
  const IeltsSpeakingReportException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final IeltsSpeakingReportFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'IeltsSpeakingReportException(kind: ${kind.name})';
}

abstract interface class IeltsSpeakingReportClient {
  Future<IeltsSpeakingReportEnvelope> getReport(String practiceSessionId);

  Future<void> clearAccountState();
}
