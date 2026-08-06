import 'package:speakup/features/coaching/goal/goal.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

abstract interface class GoalClient {
  Future<Goal> createGoal({required String title});

  Future<Goal> getGoal(String goalId);

  Future<List<Goal>> listGoals();

  Future<void> clearAccountState();
}

/// Coaching-owned boundary for attaching a Goal to an Agent Thread.
abstract interface class GoalActivationClient {
  Future<Goal> startScene({
    required String threadId,
    required SceneDefinition scene,
    required String clientOperationId,
  });

  Future<Goal> selectExistingGoal({
    required String threadId,
    required String goalId,
  });
}

enum GoalClientFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  rateLimited,
  server,
  network,
  invalidResponse,
  superseded,
}

/// A redacted failure produced by the Coaching Goal boundary.
final class GoalClientException implements Exception {
  const GoalClientException({
    required this.kind,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
    this.correlationId,
  });

  final GoalClientFailureKind kind;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;
  final String? correlationId;

  @override
  String toString() {
    final status = statusCode == null ? '' : ', statusCode: $statusCode';
    final code = errorCode == null ? '' : ', errorCode: $errorCode';
    return 'GoalClientException(kind: ${kind.name}$status$code)';
  }
}
