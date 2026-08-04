import 'package:speakup/features/coaching/goal/goal.dart';

abstract interface class GoalClient {
  Future<Goal> createGoal({required String title});

  Future<Goal> getGoal(String goalId);

  Future<List<Goal>> listGoals();

  Future<void> clearAccountState();
}
