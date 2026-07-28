import 'package:speakup/features/preparation/preparation_launch_models.dart';

abstract interface class PreparationLaunchClient {
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  });

  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  });

  Future<PreparationPracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  });

  Future<PreparationPracticeBootstrap> createSession({
    required String planId,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  });

  Future<void> clearAccountState();
}
