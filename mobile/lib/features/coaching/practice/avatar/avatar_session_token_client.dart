import 'avatar_models.dart';

abstract interface class AvatarSessionTokenClient {
  Future<AvatarSessionGrant> createSession({required String practiceSessionId});

  Future<void> clearAccountState();

  Future<void> dispose();
}

enum AvatarSessionTokenFailure {
  authenticationRequired,
  forbidden,
  notFound,
  conflict,
  unavailable,
  invalidResponse,
  network,
  cancelled,
}

final class AvatarSessionTokenException implements Exception {
  const AvatarSessionTokenException({
    required this.failure,
    this.statusCode,
    this.retryable = false,
  });

  final AvatarSessionTokenFailure failure;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() =>
      'Avatar session is unavailable (${failure.name}, '
      'status: ${statusCode ?? 'none'}).';
}
