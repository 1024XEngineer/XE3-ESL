import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/practice/practice_models.dart';

String testPracticePlanId(String sessionId) => 'practice-plan-$sessionId';

PracticeSessionSnapshot testPracticeSnapshot({
  required SceneDefinition scene,
  String sessionId = 'session-test',
  int completedTurns = 0,
  int turnLimit = 3,
  int? sessionVersion,
  AgentReview? review,
}) {
  if (completedTurns < 0 ||
      completedTurns > turnLimit ||
      turnLimit < 1 ||
      turnLimit > 14) {
    throw ArgumentError('Invalid test Practice progress.');
  }
  final completed = completedTurns == turnLimit;
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: testPracticePlanId(sessionId),
    sceneFamily: scene.family,
    sceneModel: scene.model,
    sessionVersion: sessionVersion ?? completedTurns + 1,
    completedTurns: completedTurns,
    turnLimit: turnLimit,
    sessionCompleted: completed,
    currentQuestion: completed
        ? null
        : PracticeQuestion(
            id: 'question-$sessionId-${completedTurns + 1}',
            sessionId: sessionId,
            text: 'Question ${completedTurns + 1}',
          ),
    review: review,
  );
}

Future<void> activateTestPractice({
  required AgentController controller,
  required SceneDefinition scene,
  String sessionId = 'session-test',
  String clientOperationId = 'activate-test-session',
}) async {
  await controller.selectScene(scene);
  final threadId = controller.threadId;
  final goal = controller.activeGoal;
  if (threadId == null || goal == null) {
    throw StateError('The test Agent did not activate the requested Goal.');
  }
  await controller.activateCreatedPractice(
    threadId: threadId,
    goalId: goal.id,
    scene: scene,
    sessionId: sessionId,
    planId: testPracticePlanId(sessionId),
    turnLimit: 3,
    clientOperationId: clientOperationId,
  );
}
