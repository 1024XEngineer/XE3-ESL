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

final class PreparationProfile {
  const PreparationProfile({
    required this.id,
    required this.userId,
    required this.backgroundSummary,
    required this.version,
    required this.updatedAt,
    this.resumeRef,
    this.jobDescriptionRef,
    this.jobTargetId,
    this.jobTargetConfirmationVersion,
  });

  final String id;
  final String userId;
  final String? resumeRef;
  final String? jobDescriptionRef;
  final String backgroundSummary;
  final String? jobTargetId;
  final int? jobTargetConfirmationVersion;
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

enum PracticePlanStatus { ready, archived }

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
    this.resumeRef,
    this.jobDescriptionRef,
    this.jobTargetId,
    this.jobTargetConfirmationVersion,
  });

  final String backgroundSummary;
  final String? resumeRef;
  final String? jobDescriptionRef;
  final String? jobTargetId;
  final int? jobTargetConfirmationVersion;
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
