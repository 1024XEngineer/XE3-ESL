import 'package:speakup/features/preparation/job_preparation_models.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';

abstract interface class JobPreparationClient {
  Future<JobTarget> createJobTarget({
    required JobTargetInput input,
    required String idempotencyKey,
  });

  Future<JobTarget> getJobTarget(String jobTargetId);

  Future<JobTarget> updateJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required JobTargetInput input,
    required String idempotencyKey,
  });

  Future<JobTarget> analyzeJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  });

  Future<JobTarget> confirmJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required int expectedAnalysisVersion,
    required JobTargetCandidate candidate,
    required String idempotencyKey,
  });

  Future<JobTarget> discardJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  });

  Future<JobPreparationProfile> createProfileForJobTarget({
    required String backgroundSummary,
    required String jobTargetId,
    required int jobTargetConfirmationVersion,
    required String idempotencyKey,
  });

  Future<JobPreparationSnapshot> createJobPreparationSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  });

  Future<JobPracticePlanPreview> createJobPracticePlan({
    required AgentPracticeContext context,
    required String preparationSnapshotId,
    required String idempotencyKey,
  });

  Future<JobPracticePlanPreview> getJobPracticePlan(String planId);

  Future<JobPracticePlanPreview> reviseJobPracticePlan({
    required String planId,
    required int expectedPlanRevision,
    required String roleDefinitionId,
    required String practiceOptionId,
    required int practiceOptionVersion,
    required int maxEffectiveTurns,
    required String idempotencyKey,
  });

  Future<PreparationPracticeBootstrap> createJobPracticeSession({
    required JobPracticePlanPreview plan,
    required String idempotencyKey,
  });

  Future<void> clearAccountState();
}
