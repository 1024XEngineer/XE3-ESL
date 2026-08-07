import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class AgentPracticeContext {
  const AgentPracticeContext({required this.threadId, required this.goalId});

  final String threadId;
  final String goalId;

  @override
  bool operator ==(Object other) =>
      other is AgentPracticeContext &&
      other.threadId == threadId &&
      other.goalId == goalId;

  @override
  int get hashCode => Object.hash(threadId, goalId);
}

enum PreparationKind {
  interview('interview'),
  scenario('scenario');

  const PreparationKind(this.wireValue);

  final String wireValue;
}

sealed class PreparationContext {
  const PreparationContext(this.kind);

  final PreparationKind kind;
}

final class ScenarioPreparationContext extends PreparationContext {
  const ScenarioPreparationContext({
    required this.situation,
    required this.userRole,
    required this.counterpartRole,
    required this.goal,
    required this.counterpartPersona,
  }) : super(PreparationKind.scenario);

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

final class PreparationResumeReference {
  const PreparationResumeReference({
    required this.resumeId,
    required this.revision,
  });

  final String resumeId;
  final int revision;
}

final class PreparationJobTargetReference {
  const PreparationJobTargetReference({
    required this.jobTargetId,
    required this.confirmationVersion,
  });

  final String jobTargetId;
  final int confirmationVersion;
}

final class InterviewPreparationContext extends PreparationContext {
  const InterviewPreparationContext({required this.jobTarget, this.resume})
    : super(PreparationKind.interview);

  final PreparationResumeReference? resume;
  final PreparationJobTargetReference jobTarget;
}

final class PreparationProfile {
  const PreparationProfile({
    required this.id,
    required this.userId,
    required this.backgroundSummary,
    required this.version,
    required this.updatedAt,
    this.resumeId,
    this.resumeRevision,
    this.jobDescriptionRef,
    this.jobTargetId,
    this.jobTargetConfirmationVersion,
    this.context,
  });

  final String id;
  final String userId;
  final String? resumeId;
  final int? resumeRevision;
  final String? jobDescriptionRef;
  final String backgroundSummary;
  final String? jobTargetId;
  final int? jobTargetConfirmationVersion;
  final PreparationContext? context;
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
    this.sourceJobTargetId,
    this.sourceJobTargetConfirmationVersion,
    this.jobTargetInput,
    this.jobTargetCandidate,
    this.resumeSnapshot,
    this.jobDescriptionSnapshot,
    this.context,
  });

  final String id;
  final String sourceProfileId;
  final int sourceVersion;
  final String? sourceJobTargetId;
  final int? sourceJobTargetConfirmationVersion;
  final JobTargetInput? jobTargetInput;
  final JobTargetCandidate? jobTargetCandidate;
  final String? resumeSnapshot;
  final String? jobDescriptionSnapshot;
  final PreparationContext? context;
  final String backgroundSnapshot;
  final DateTime createdAt;
}

final class PreparationGoalSnapshot {
  const PreparationGoalSnapshot({
    required this.id,
    required this.title,
    required this.version,
  });

  final String id;
  final String title;
  final int version;
}

final class PracticeObjective {
  const PracticeObjective({required this.id, required this.description});

  final String id;
  final String description;
}

final class PreparationSessionPolicy {
  const PreparationSessionPolicy({
    this.completionMode = PreparationCompletionMode.turnLimited,
    required this.suggestedDurationSeconds,
    required this.minEffectiveTurns,
    required this.maxEffectiveTurns,
    required this.coverageCheckpointTurn,
    required this.maxFollowUpsPerQuestion,
    required this.earlyCompletionRule,
    required this.retryAllowed,
    required this.questionTranslationAllowed,
    required this.questionTipsAllowed,
    required this.avatarAllowed,
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
  final bool avatarAllowed;
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

enum PracticePlanStatus { ready, archived }

final class PracticePlanSummary {
  const PracticePlanSummary({
    required this.id,
    required this.revision,
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
  final int revision;
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
    required this.revision,
    required this.status,
    required this.createdAt,
    required this.updatedAt,
    this.sourceThreadId,
    this.goalSnapshot,
    this.ieltsAssignment,
  });

  final String id;
  final String userId;
  final String? sourceThreadId;
  final PreparationGoalSnapshot? goalSnapshot;
  final PreparationSnapshot preparationSnapshot;
  final SceneSelectionSnapshot sceneSelection;
  final PreparationSessionPolicy sessionPolicy;
  final List<PracticeObjective> practiceObjectives;
  final IeltsPracticeAssignment? ieltsAssignment;
  final int revision;
  final PracticePlanStatus status;
  final DateTime createdAt;
  final DateTime updatedAt;

  AgentPracticeContext? get agentContext {
    final threadId = sourceThreadId;
    final goal = goalSnapshot;
    if (threadId == null || goal == null) {
      return null;
    }
    return AgentPracticeContext(threadId: threadId, goalId: goal.id);
  }

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

final class CreatePreparationProfileInput {
  const CreatePreparationProfileInput({
    required this.backgroundSummary,
    this.resumeId,
    this.resumeRevision,
    this.jobDescriptionRef,
    this.jobTargetId,
    this.jobTargetConfirmationVersion,
    this.kind,
    this.scenario,
  });

  factory CreatePreparationProfileInput.scenario({
    required ScenarioPreparationContext context,
  }) => CreatePreparationProfileInput(
    backgroundSummary: context.situation,
    kind: PreparationKind.scenario,
    scenario: context,
  );

  final String backgroundSummary;
  final String? resumeId;
  final int? resumeRevision;
  final String? jobDescriptionRef;
  final String? jobTargetId;
  final int? jobTargetConfirmationVersion;
  final PreparationKind? kind;
  final ScenarioPreparationContext? scenario;
}

final class CreatePreparationPlanInput {
  const CreatePreparationPlanInput({
    required this.preparationSnapshotId,
    required this.sceneId,
    required this.sceneVersion,
    required this.selectedRoleIds,
    required this.practiceOptionId,
    this.sourceThreadId,
    this.goalId,
    this.maxEffectiveTurns,
    this.ieltsSelection,
  });

  final String? sourceThreadId;
  final String? goalId;
  final String preparationSnapshotId;
  final String sceneId;
  final int sceneVersion;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
  final int? maxEffectiveTurns;
  final IeltsPracticeSelection? ieltsSelection;
}

final class RevisePreparationPlanInput {
  const RevisePreparationPlanInput({
    required this.expectedPlanRevision,
    required this.selectedRoleIds,
    required this.practiceOptionId,
    required this.maxEffectiveTurns,
  });

  final int expectedPlanRevision;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
  final int maxEffectiveTurns;
}
