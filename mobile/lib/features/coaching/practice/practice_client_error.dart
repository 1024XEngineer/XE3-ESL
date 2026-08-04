enum PracticeClientFailureKind {
  unavailable,
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  rateLimited,
  server,
  network,
  invalidResponse,
  pollingTimedOut,
  unexpected,
}

final class PracticeClientException implements Exception {
  const PracticeClientException({
    required this.kind,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
    this.correlationId,
    this.retryAfter,
  });

  final PracticeClientFailureKind kind;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;
  final String? correlationId;
  final Duration? retryAfter;

  bool get isUnavailable => kind == PracticeClientFailureKind.unavailable;
}

final class PracticeClientOperationCancelled implements Exception {
  const PracticeClientOperationCancelled();
}
