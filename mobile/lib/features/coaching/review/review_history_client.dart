import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/review/formal_review.dart';

final class ReviewHistoryItem {
  const ReviewHistoryItem({
    required this.review,
    required this.formalReview,
    required this.practiceSessionId,
    required this.createdAt,
    required this.completedAt,
  });

  final AgentReview review;
  final FormalReview formalReview;
  final String practiceSessionId;
  final DateTime createdAt;
  final DateTime completedAt;
}

final class ReviewHistoryPage {
  const ReviewHistoryPage({required this.items, this.nextCursor});

  final List<ReviewHistoryItem> items;
  final String? nextCursor;
}

enum ReviewHistoryFailureKind {
  authenticationRequired,
  invalidRequest,
  network,
  server,
  invalidResponse,
  superseded,
}

final class ReviewHistoryException implements Exception {
  const ReviewHistoryException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final ReviewHistoryFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'ReviewHistoryException(kind: ${kind.name})';
}

abstract interface class ReviewHistoryClient {
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20});

  Future<void> clearAccountState();
}
