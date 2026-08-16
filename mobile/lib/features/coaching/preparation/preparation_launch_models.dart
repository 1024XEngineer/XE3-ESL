import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class PreparationLaunchSelection {
  const PreparationLaunchSelection({
    required this.scene,
    required this.selectedRoleIds,
    required this.practiceOptionId,
    this.ieltsSelection,
    this.ieltsPreparedAnswers = const <IeltsPreparedAnswer>[],
  });

  factory PreparationLaunchSelection.fromCatalog({
    required SceneDefinition scene,
    required RoleDefinition role,
    required PracticeOption option,
    IeltsPracticeSelection? ieltsSelection,
    List<IeltsPreparedAnswer> ieltsPreparedAnswers = const [],
  }) => PreparationLaunchSelection(
    scene: scene,
    selectedRoleIds: <String>[role.id],
    practiceOptionId: option.id,
    ieltsSelection: ieltsSelection,
    ieltsPreparedAnswers: ieltsPreparedAnswers,
  );

  final SceneDefinition scene;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
  final IeltsPracticeSelection? ieltsSelection;
  final List<IeltsPreparedAnswer> ieltsPreparedAnswers;

  @override
  bool operator ==(Object other) =>
      other is PreparationLaunchSelection &&
      identical(other.scene, scene) &&
      _sameStrings(other.selectedRoleIds, selectedRoleIds) &&
      other.practiceOptionId == practiceOptionId &&
      other.ieltsSelection == ieltsSelection &&
      _samePreparedAnswers(other.ieltsPreparedAnswers, ieltsPreparedAnswers);

  @override
  int get hashCode => Object.hash(
    scene,
    Object.hashAll(selectedRoleIds),
    practiceOptionId,
    ieltsSelection,
    Object.hashAll(ieltsPreparedAnswers.map(_preparedAnswerHash)),
  );
}

bool _samePreparedAnswers(
  List<IeltsPreparedAnswer> left,
  List<IeltsPreparedAnswer> right,
) {
  if (left.length != right.length) return false;
  for (var index = 0; index < left.length; index++) {
    if (_preparedAnswerHash(left[index]) != _preparedAnswerHash(right[index])) {
      return false;
    }
  }
  return true;
}

int _preparedAnswerHash(IeltsPreparedAnswer value) => Object.hash(
  value.bankId,
  value.part,
  value.sourceId,
  value.questionPosition,
  value.answer,
  value.personalized,
);

bool _sameStrings(List<String> left, List<String> right) {
  if (left.length != right.length) return false;
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) return false;
  }
  return true;
}

final class PreparationPracticeSession {
  const PreparationPracticeSession({
    required this.id,
    required this.planId,
    required this.planVersion,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.status,
    required this.version,
    required this.createdAt,
  });

  final String id;
  final String planId;
  final int planVersion;
  final PracticeExperience practiceExperience;
  final SceneCategory sceneCategory;
  final PracticeMode practiceMode;
  final String status;
  final int version;
  final DateTime createdAt;
}

final class PreparationPracticeBootstrap {
  const PreparationPracticeBootstrap({
    required this.session,
    required this.maxEffectiveTurns,
  });

  final PreparationPracticeSession session;
  final int maxEffectiveTurns;
}

final class CreatePreparationSessionInput {
  const CreatePreparationSessionInput({required this.expectedPlanVersion});

  final int expectedPlanVersion;
}

enum PreparationLaunchStage { context, plan, session, voice }

enum PreparationLaunchFailureKind {
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

  PreparationLaunchException at(PreparationLaunchStage value) =>
      PreparationLaunchException(
        kind: kind,
        stage: value,
        statusCode: statusCode,
        errorCode: errorCode,
        retryable: retryable,
      );

  @override
  String toString() =>
      'PreparationLaunchException(kind: ${kind.name}, stage: ${stage?.name})';
}
