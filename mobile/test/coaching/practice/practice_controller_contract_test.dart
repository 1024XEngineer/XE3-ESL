import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  test('completed Practice confirmation is independent from Review', () {
    const confirmation = PracticeTurnConfirmation(
      turnId: 'turn-3',
      sessionId: 'session-1',
      questionId: 'question-3',
      candidateId: 'candidate-3',
      answer: PracticeMessage(
        id: 'answer-3',
        role: PracticeMessageRole.user,
        text: 'Final answer',
      ),
      completedTurns: 3,
      turnLimit: 3,
      sessionCompleted: true,
      sceneFamily: SceneFamily.interview,
      sceneModel: SceneModel.projectExperienceDeepDive,
      sessionVersion: 4,
    );

    expect(confirmation.sessionCompleted, isTrue);
    expect(confirmation.completedTurns, confirmation.turnLimit);
  });
}
