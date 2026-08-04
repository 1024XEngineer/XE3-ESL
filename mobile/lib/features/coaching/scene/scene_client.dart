import 'package:speakup/features/coaching/scene/scene.dart';

enum SceneClientFailureKind { network, unavailable, invalidResponse }

final class SceneClientException implements Exception {
  const SceneClientException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final SceneClientFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'SceneClientException(kind: ${kind.name})';
}

abstract interface class SceneClient {
  Future<List<SceneDefinition>> listScenes();

  Future<SceneDefinition> getScene(String sceneId);

  Future<List<RoleDefinition>> listRoles(String sceneId);
}
