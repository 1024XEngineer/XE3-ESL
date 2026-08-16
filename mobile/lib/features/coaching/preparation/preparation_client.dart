import 'package:speakup/features/coaching/preparation/preparation_models.dart';

abstract interface class PreparationClient {
  Future<PracticePlan> createPlan({
    required CreatePracticePlanInput input,
    required String idempotencyKey,
  });

  Future<PracticePlan> getPlan(String planId);

  Future<PracticePlan> confirmPlan({
    required String planId,
    required int expectedVersion,
    required String idempotencyKey,
  });

  Future<void> clearAccountState();
}
