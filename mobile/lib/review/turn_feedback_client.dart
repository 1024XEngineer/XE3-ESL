import 'package:speakup/review/turn_feedback.dart';

enum SpeechFeedbackFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  network,
  server,
  invalidResponse,
  superseded,
}

final class SpeechFeedbackException implements Exception {
  const SpeechFeedbackException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final SpeechFeedbackFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'SpeechFeedbackException(kind: ${kind.name})';
}

abstract interface class SpeechFeedbackClient {
  Future<SpeechFeedback> getFeedback(String statusUrl);

  Future<void> clearAccountState();
}
