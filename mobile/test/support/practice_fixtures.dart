import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

String testPracticePlanId(String sessionId) => 'practice-plan-$sessionId';

const testPracticeCapabilities = PracticeCapabilities(
  retryAllowed: true,
  questionTranslationAllowed: true,
  questionTipsAllowed: true,
  speechFeedbackAllowed: true,
);

PracticeSessionSnapshot testPracticeSnapshot({
  required SceneDefinition scene,
  String sessionId = 'session-test',
  String? planId,
  int completedTurns = 0,
  int turnLimit = 3,
  int? sessionVersion,
  PracticeMode? practiceMode,
  IeltsPracticeAssignment? ieltsAssignment,
  PracticeCapabilities capabilities = testPracticeCapabilities,
}) {
  if (completedTurns < 0 ||
      completedTurns > turnLimit ||
      turnLimit < 1 ||
      turnLimit > practiceTurnSafetyLimit ||
      (scene.experience == PracticeExperience.ieltsSpeaking) !=
          (ieltsAssignment != null)) {
    throw ArgumentError('Invalid test Practice progress.');
  }
  final completed = completedTurns == turnLimit;
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: planId ?? testPracticePlanId(sessionId),
    practiceExperience: scene.experience,
    sceneCategory: scene.category,
    practiceMode: practiceMode ?? scene.practiceOptions.first.mode,
    capabilities: capabilities,
    sessionVersion: sessionVersion ?? completedTurns + 1,
    completedTurns: completedTurns,
    turnLimit: turnLimit,
    sessionCompleted: completed,
    ieltsAssignment: ieltsAssignment,
    currentQuestion: completed
        ? null
        : PracticeQuestion(
            id: 'question-$sessionId-${completedTurns + 1}',
            sessionId: sessionId,
            text: 'Question ${completedTurns + 1}',
          ),
  );
}

IeltsPracticeAssignment testIeltsAssignment({
  required PracticeMode mode,
  int part1QuestionCount = 8,
  int part3QuestionCount = 5,
}) {
  final parts = switch (mode) {
    PracticeMode.fullMock => <IeltsPracticePartAssignment>[
      _testIeltsPart1(part1QuestionCount),
      _testIeltsPart2(),
      _testIeltsPart3(part3QuestionCount),
    ],
    PracticeMode.part1 => <IeltsPracticePartAssignment>[
      _testIeltsPart1(part1QuestionCount),
    ],
    PracticeMode.part2 => <IeltsPracticePartAssignment>[_testIeltsPart2()],
    PracticeMode.part3 => <IeltsPracticePartAssignment>[
      _testIeltsPart3(part3QuestionCount),
    ],
    PracticeMode.fullSimulation ||
    PracticeMode.focus => throw ArgumentError.value(mode, 'mode'),
  };
  return IeltsPracticeAssignment(
    bankId: 'ielts-bank-test',
    season: 'test-season',
    mode: mode,
    parts: List<IeltsPracticePartAssignment>.unmodifiable(parts),
  );
}

IeltsPracticePartAssignment _testIeltsPart1(int questionCount) =>
    IeltsPracticePartAssignment(
      part: IeltsSpeakingPart.part1,
      sourceId: 'part-1-set-test',
      turnBlueprints: List<String>.unmodifiable(
        List<String>.generate(
          questionCount,
          (index) => 'Part 1 question ${index + 1}',
        ),
      ),
    );

IeltsPracticePartAssignment _testIeltsPart2() =>
    const IeltsPracticePartAssignment(
      part: IeltsSpeakingPart.part2,
      sourceId: 'topic-group-test',
      topicTitle: 'Test topic',
      cueCard: 'Describe a useful skill you learned.',
      turnBlueprints: <String>['Describe a useful skill you learned.'],
    );

IeltsPracticePartAssignment _testIeltsPart3(int questionCount) =>
    IeltsPracticePartAssignment(
      part: IeltsSpeakingPart.part3,
      sourceId: 'topic-group-test',
      topicTitle: 'Test topic',
      turnBlueprints: List<String>.unmodifiable(
        List<String>.generate(
          questionCount,
          (index) => 'Part 3 question ${index + 1}',
        ),
      ),
    );

Future<void> activateTestPractice({
  required PracticeController controller,
  required SceneDefinition scene,
  String sessionId = 'session-test',
  String? planId,
  String clientOperationId = 'activate-test-session',
}) async {
  await controller.activateCreatedPractice(
    scene: scene,
    sessionId: sessionId,
    planId: planId ?? testPracticePlanId(sessionId),
    practiceMode: scene.practiceOptions.first.mode,
    turnLimit: 3,
    clientOperationId: clientOperationId,
  );
}
