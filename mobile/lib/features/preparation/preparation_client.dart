import 'package:speakup/features/preparation/preparation_models.dart';

enum PreparationCatalogFailureKind {
  network,
  unavailable,
  invalidResponse,
  superseded,
}

final class PreparationCatalogException implements Exception {
  const PreparationCatalogException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final PreparationCatalogFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'PreparationCatalogException(kind: ${kind.name})';
}

abstract interface class PreparationCatalogClient {
  Future<List<PreparationScenario>> listScenarios();

  Future<PreparationScenarioDetail> getScenario(String scenarioId);

  Future<List<PreparationRole>> listRoles(String scenarioId);

  Future<void> clearAccountState();
}
