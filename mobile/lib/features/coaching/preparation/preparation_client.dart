import 'package:speakup/features/coaching/preparation/preparation_models.dart';

abstract interface class PreparationClient {
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  });

  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  });

  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  });

  Future<void> clearAccountState();
}
