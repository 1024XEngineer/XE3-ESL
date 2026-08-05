import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class PreparationLaunchSelection {
  const PreparationLaunchSelection({
    required this.scene,
    required this.selectedRoleIds,
    required this.practiceOptionId,
    this.ieltsSelection,
  });

  factory PreparationLaunchSelection.fromCatalog({
    required SceneDefinition scene,
    required RoleDefinition role,
    required PracticeOption option,
    IeltsPracticeSelection? ieltsSelection,
  }) {
    return PreparationLaunchSelection(
      scene: scene,
      selectedRoleIds: <String>[role.id],
      practiceOptionId: option.id,
      ieltsSelection: ieltsSelection,
    );
  }

  final SceneDefinition scene;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
  final IeltsPracticeSelection? ieltsSelection;

  @override
  bool operator ==(Object other) =>
      other is PreparationLaunchSelection &&
      identical(other.scene, scene) &&
      _sameStrings(other.selectedRoleIds, selectedRoleIds) &&
      other.practiceOptionId == practiceOptionId &&
      other.ieltsSelection == ieltsSelection;

  @override
  int get hashCode => Object.hash(
    scene,
    Object.hashAll(selectedRoleIds),
    practiceOptionId,
    ieltsSelection,
  );
}

bool _sameStrings(List<String> left, List<String> right) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) {
      return false;
    }
  }
  return true;
}

final class PreparationPracticeSession {
  const PreparationPracticeSession({
    required this.id,
    required this.planId,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.snapshotId,
    required this.status,
    required this.version,
    required this.createdAt,
  });

  final String id;
  final String planId;
  final PracticeExperience practiceExperience;
  final SceneCategory sceneCategory;
  final PracticeMode practiceMode;
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

final class CreatePreparationSessionInput {
  const CreatePreparationSessionInput({
    required this.expectedPlanRevision,
    required this.userConfirmed,
  });

  final int expectedPlanRevision;
  final bool userConfirmed;
}

enum PreparationLaunchStage {
  context,
  goal,
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
