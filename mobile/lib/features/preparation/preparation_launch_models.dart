import 'package:speakup/features/preparation/ielts_question_bank.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

final class AgentPracticeContext {
  const AgentPracticeContext({required this.threadId, required this.matterId});

  final String threadId;
  final String matterId;

  @override
  bool operator ==(Object other) =>
      other is AgentPracticeContext &&
      other.threadId == threadId &&
      other.matterId == matterId;

  @override
  int get hashCode => Object.hash(threadId, matterId);
}

final class PreparationLaunchSelection {
  const PreparationLaunchSelection({
    required this.scenarioDefinitionId,
    required this.scenarioDefinitionVersion,
    required this.scenarioType,
    required this.scenarioModel,
    required this.scenarioDisplayName,
    required this.scenarioDescription,
    required this.scenarioConfigId,
    required this.scenarioConfigVersion,
    required this.roleDefinitionId,
    required this.roleDefinitionVersion,
    required this.practiceOptionId,
    required this.practiceOptionType,
    required this.practiceOptionVersion,
    this.ieltsSelection,
  });

  factory PreparationLaunchSelection.fromCatalog({
    required PreparationScenario scenario,
    required PreparationScenarioConfig config,
    required PreparationRole role,
    required PreparationOption option,
    IeltsPracticeSelection? ieltsSelection,
    String? scenarioDisplayName,
    String? scenarioDescription,
  }) {
    return PreparationLaunchSelection(
      scenarioDefinitionId: scenario.id,
      scenarioDefinitionVersion: scenario.version,
      scenarioType: scenario.type,
      scenarioModel: scenario.model,
      scenarioDisplayName: scenarioDisplayName ?? scenario.name,
      scenarioDescription:
          scenarioDescription ?? config.prompt.publicSceneBrief,
      scenarioConfigId: config.id,
      scenarioConfigVersion: config.version,
      roleDefinitionId: role.id,
      roleDefinitionVersion: role.version,
      practiceOptionId: option.id,
      practiceOptionType: option.type,
      practiceOptionVersion: option.version,
      ieltsSelection: ieltsSelection,
    );
  }

  final String scenarioDefinitionId;
  final int scenarioDefinitionVersion;
  final String scenarioType;
  final String scenarioModel;
  final String scenarioDisplayName;
  final String scenarioDescription;
  final String scenarioConfigId;
  final int scenarioConfigVersion;
  final String roleDefinitionId;
  final int roleDefinitionVersion;
  final String practiceOptionId;
  final PreparationOptionType practiceOptionType;
  final int practiceOptionVersion;
  final IeltsPracticeSelection? ieltsSelection;

  @override
  bool operator ==(Object other) =>
      other is PreparationLaunchSelection &&
      other.scenarioDefinitionId == scenarioDefinitionId &&
      other.scenarioDefinitionVersion == scenarioDefinitionVersion &&
      other.scenarioType == scenarioType &&
      other.scenarioModel == scenarioModel &&
      other.scenarioDisplayName == scenarioDisplayName &&
      other.scenarioDescription == scenarioDescription &&
      other.scenarioConfigId == scenarioConfigId &&
      other.scenarioConfigVersion == scenarioConfigVersion &&
      other.roleDefinitionId == roleDefinitionId &&
      other.roleDefinitionVersion == roleDefinitionVersion &&
      other.practiceOptionId == practiceOptionId &&
      other.practiceOptionType == practiceOptionType &&
      other.practiceOptionVersion == practiceOptionVersion &&
      other.ieltsSelection == ieltsSelection;

  @override
  int get hashCode => Object.hash(
    scenarioDefinitionId,
    scenarioDefinitionVersion,
    scenarioType,
    scenarioModel,
    scenarioDisplayName,
    scenarioDescription,
    scenarioConfigId,
    scenarioConfigVersion,
    roleDefinitionId,
    roleDefinitionVersion,
    practiceOptionId,
    practiceOptionType,
    practiceOptionVersion,
    ieltsSelection,
  );
}

final class PreparationProfile {
  const PreparationProfile({
    required this.id,
    required this.userId,
    required this.backgroundSummary,
    required this.version,
    required this.updatedAt,
  });

  final String id;
  final String userId;
  final String backgroundSummary;
  final int version;
  final DateTime updatedAt;
}

final class PreparationSnapshot {
  const PreparationSnapshot({
    required this.id,
    required this.sourceProfileId,
    required this.sourceVersion,
    required this.backgroundSnapshot,
    required this.createdAt,
  });

  final String id;
  final String sourceProfileId;
  final int sourceVersion;
  final String backgroundSnapshot;
  final DateTime createdAt;
}

final class PreparationPracticePlan {
  const PreparationPracticePlan({
    required this.id,
    required this.userId,
    required this.context,
    required this.selection,
    required this.preparationProfileId,
    required this.revision,
    required this.status,
  });

  final String id;
  final String userId;
  final AgentPracticeContext context;
  final PreparationLaunchSelection selection;
  final String preparationProfileId;
  final int revision;
  final String status;
}

final class PreparationPracticeSession {
  const PreparationPracticeSession({
    required this.id,
    required this.planId,
    required this.scenarioType,
    required this.scenarioModel,
    required this.snapshotId,
    required this.status,
    required this.version,
    required this.createdAt,
  });

  final String id;
  final String planId;
  final String scenarioType;
  final String scenarioModel;
  final String snapshotId;
  final String status;
  final int version;
  final DateTime createdAt;
}

final class PreparationPracticeBootstrap {
  const PreparationPracticeBootstrap({
    required this.session,
    required this.preparationSnapshotId,
    required this.maxEffectiveTurns,
  });

  final PreparationPracticeSession session;
  final String preparationSnapshotId;
  final int maxEffectiveTurns;
}

final class CreatePreparationProfileInput {
  const CreatePreparationProfileInput({required this.backgroundSummary});

  final String backgroundSummary;
}

final class CreatePreparationPlanInput {
  const CreatePreparationPlanInput({
    required this.context,
    required this.selection,
    required this.preparationProfileId,
    required this.preparationSnapshotId,
    required this.preparationUserId,
  });

  final AgentPracticeContext context;
  final PreparationLaunchSelection selection;
  final String preparationProfileId;
  final String preparationSnapshotId;
  final String preparationUserId;
}

final class CreatePreparationSessionInput {
  const CreatePreparationSessionInput({
    required this.agentThreadId,
    required this.expectedPlanRevision,
    required this.preparationSnapshotId,
    required this.preparationProfileId,
    required this.preparationProfileVersion,
    required this.preparationUserId,
    required this.backgroundSummary,
    required this.selection,
  });

  final String agentThreadId;
  final int expectedPlanRevision;
  final String preparationSnapshotId;
  final String preparationProfileId;
  final int preparationProfileVersion;
  final String preparationUserId;
  final String backgroundSummary;
  final PreparationLaunchSelection selection;
}

enum PreparationLaunchStage {
  context,
  matter,
  profile,
  snapshot,
  plan,
  session,
  voice,
}

enum PreparationLaunchFailureKind {
  contextMissing,
  contextChanged,
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  network,
  server,
  invalidResponse,
  superseded,
}

final class PreparationLaunchException implements Exception {
  const PreparationLaunchException({
    required this.kind,
    this.stage,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
  });

  final PreparationLaunchFailureKind kind;
  final PreparationLaunchStage? stage;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;

  PreparationLaunchException at(PreparationLaunchStage value) {
    return PreparationLaunchException(
      kind: kind,
      stage: value,
      statusCode: statusCode,
      errorCode: errorCode,
      retryable: retryable,
    );
  }

  @override
  String toString() =>
      'PreparationLaunchException(kind: ${kind.name}, stage: ${stage?.name})';
}
