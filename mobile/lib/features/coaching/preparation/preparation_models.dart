import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

/// UI input for direct custom-scenario launch. The Plan stores only the
/// resulting background summary, not a second scenario aggregate.
final class ScenarioPreparationContext {
  const ScenarioPreparationContext({
    required this.situation,
    required this.userRole,
    required this.counterpartRole,
    required this.goal,
    required this.counterpartPersona,
  });

  final String situation;
  final String userRole;
  final String counterpartRole;
  final String goal;
  final String counterpartPersona;

  @override
  bool operator ==(Object other) =>
      other is ScenarioPreparationContext &&
      other.situation == situation &&
      other.userRole == userRole &&
      other.counterpartRole == counterpartRole &&
      other.goal == goal &&
      other.counterpartPersona == counterpartPersona;

  @override
  int get hashCode => Object.hash(
    situation,
    userRole,
    counterpartRole,
    goal,
    counterpartPersona,
  );
}

final class PlanPreparationSnapshot {
  const PlanPreparationSnapshot({
    required this.backgroundSummary,
    this.interview,
  });

  final String backgroundSummary;
  final InterviewPreparationSnapshot? interview;
}

final class PracticeObjective {
  const PracticeObjective({required this.id, required this.description});

  final String id;
  final String description;
}

final class PreparationSessionPolicy {
  const PreparationSessionPolicy({
    required this.completionMode,
    required this.suggestedDurationSeconds,
    required this.minEffectiveTurns,
    required this.maxEffectiveTurns,
    required this.coverageCheckpointTurn,
    required this.maxFollowUpsPerQuestion,
    required this.earlyCompletionRule,
    required this.retryAllowed,
    required this.questionTranslationAllowed,
    required this.questionTipsAllowed,
    required this.speechFeedbackAllowed,
  });

  final PreparationCompletionMode completionMode;
  final int suggestedDurationSeconds;
  final int minEffectiveTurns;
  final int maxEffectiveTurns;
  final int coverageCheckpointTurn;
  final int maxFollowUpsPerQuestion;
  final String earlyCompletionRule;
  final bool retryAllowed;
  final bool questionTranslationAllowed;
  final bool questionTipsAllowed;
  final bool speechFeedbackAllowed;
}

enum PreparationCompletionMode {
  turnLimited,
  userControlled;

  static PreparationCompletionMode? fromWireValue(String value) =>
      switch (value) {
        'TURN_LIMITED' => PreparationCompletionMode.turnLimited,
        'USER_CONTROLLED' => PreparationCompletionMode.userControlled,
        _ => null,
      };
}

enum PracticePlanStatus { draft, ready }

final class PracticePlanSummary {
  const PracticePlanSummary({
    required this.id,
    required this.version,
    required this.status,
    required this.experience,
    required this.sceneName,
    required this.practiceScope,
    required this.jobTitle,
    required this.practiceObjectives,
    required this.resumeUsed,
    required this.suggestedDurationSeconds,
    required this.minEffectiveTurns,
    required this.maxEffectiveTurns,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final int version;
  final PracticePlanStatus status;
  final PracticeExperience experience;
  final String sceneName;
  final String practiceScope;
  final String jobTitle;
  final List<String> practiceObjectives;
  final bool resumeUsed;
  final int suggestedDurationSeconds;
  final int minEffectiveTurns;
  final int maxEffectiveTurns;
  final DateTime createdAt;
  final DateTime updatedAt;
}

final class PracticePlan {
  const PracticePlan({
    required this.id,
    required this.userId,
    required this.preparationSnapshot,
    required this.sceneSelection,
    required this.sessionPolicy,
    required this.practiceObjectives,
    required this.version,
    required this.status,
    required this.createdAt,
    required this.updatedAt,
    this.sourceThreadId,
    this.ieltsAssignment,
  });

  final String id;
  final String userId;
  final String? sourceThreadId;
  final PlanPreparationSnapshot preparationSnapshot;
  final SceneSelectionSnapshot sceneSelection;
  final PreparationSessionPolicy sessionPolicy;
  final List<PracticeObjective> practiceObjectives;
  final IeltsPracticeAssignment? ieltsAssignment;
  final int version;
  final PracticePlanStatus status;
  final DateTime createdAt;
  final DateTime updatedAt;

  List<RoleDefinition> get selectedRoles {
    final rolesById = <String, RoleDefinition>{
      for (final role in sceneSelection.scene.roles) role.id: role,
    };
    return List<RoleDefinition>.unmodifiable(
      sceneSelection.selectedRoleIds.map((id) => rolesById[id]!),
    );
  }

  PracticeOption get practiceOption => sceneSelection.scene.practiceOptions
      .firstWhere((option) => option.id == sceneSelection.practiceOptionId);
}

final class CreatePracticePlanInput {
  const CreatePracticePlanInput({
    required this.sceneId,
    required this.sceneVersion,
    required this.selectedRoleIds,
    required this.practiceOptionId,
    this.sourceThreadId,
    this.backgroundSummary = '',
    this.interviewPreparationId,
    this.expectedInterviewVersion,
    this.maxEffectiveTurns,
    this.ieltsSelection,
    this.ieltsPreparedAnswers = const <IeltsPreparedAnswer>[],
  });

  final String? sourceThreadId;
  final String backgroundSummary;
  final String? interviewPreparationId;
  final int? expectedInterviewVersion;
  final String sceneId;
  final int sceneVersion;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
  final int? maxEffectiveTurns;
  final IeltsPracticeSelection? ieltsSelection;
  final List<IeltsPreparedAnswer> ieltsPreparedAnswers;
}
