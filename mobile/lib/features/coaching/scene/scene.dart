enum SceneFamily {
  interview('INTERVIEW'),
  exam('EXAM'),
  workplace('WORKPLACE'),
  daily('DAILY');

  const SceneFamily(this.wireValue);

  final String wireValue;

  static SceneFamily? fromWireValue(String value) => SceneFamily.values
      .where((family) => family.wireValue == value)
      .firstOrNull;
}

enum SceneModel {
  projectExperienceDeepDive('PROJECT_EXPERIENCE_DEEP_DIVE'),
  interviewBasicDialogue('INTERVIEW_BASIC_DIALOGUE'),
  ieltsSpeakingPart1('IELTS_SPEAKING_PART_1'),
  ieltsSpeakingPart2('IELTS_SPEAKING_PART_2'),
  ieltsSpeakingPart3('IELTS_SPEAKING_PART_3'),
  ieltsSpeakingFullMock('IELTS_SPEAKING_FULL_MOCK'),
  examBasicDialogue('EXAM_BASIC_DIALOGUE'),
  progressAndRiskUpdate('PROGRESS_AND_RISK_UPDATE'),
  workplaceBasicDialogue('WORKPLACE_BASIC_DIALOGUE'),
  hotelCheckinAndIssueHandling('HOTEL_CHECKIN_AND_ISSUE_HANDLING'),
  dailyBasicDialogue('DAILY_BASIC_DIALOGUE');

  const SceneModel(this.wireValue);

  final String wireValue;

  static SceneModel? fromWireValue(String value) =>
      SceneModel.values.where((model) => model.wireValue == value).firstOrNull;
}

enum ScenePresentationMode { standard, immersiveRoleplay }

enum SceneStatus { active, inactive }

final class ScenePrompt {
  const ScenePrompt({
    required this.publicSceneBrief,
    required this.practiceGoal,
    required this.userRole,
    required this.aiRole,
    required this.personaSummary,
    required this.focusAreas,
    required this.turnBlueprints,
    required this.suggestedDurationSeconds,
  });

  final String publicSceneBrief;
  final String practiceGoal;
  final String userRole;
  final String aiRole;
  final String personaSummary;
  final List<String> focusAreas;
  final List<String> turnBlueprints;
  final int suggestedDurationSeconds;
}

final class RoleDefinition {
  const RoleDefinition({
    required this.id,
    required this.sceneId,
    required this.type,
    required this.displayName,
    required this.responsibilities,
    required this.style,
    required this.practiceObjectives,
    this.voiceConfigRef,
  });

  final String id;
  final String sceneId;
  final String type;
  final String displayName;
  final String responsibilities;
  final String style;
  final List<RolePracticeObjective> practiceObjectives;
  final String? voiceConfigRef;
}

final class RolePracticeObjective {
  const RolePracticeObjective({
    required this.objectiveId,
    required this.description,
  });

  final String objectiveId;
  final String description;
}

enum PracticeOptionType {
  fullSimulation('FULL_SIMULATION'),
  focus('FOCUS');

  const PracticeOptionType(this.wireValue);

  final String wireValue;

  static PracticeOptionType? fromWireValue(String value) => PracticeOptionType
      .values
      .where((type) => type.wireValue == value)
      .firstOrNull;
}

final class PracticeOption {
  const PracticeOption({
    required this.id,
    required this.sceneId,
    required this.type,
    required this.displayName,
    this.roleId,
  });

  final String id;
  final String sceneId;
  final PracticeOptionType type;
  final String displayName;
  final String? roleId;
}

final class SceneDefinition {
  const SceneDefinition({
    required this.id,
    required this.family,
    required this.model,
    required this.name,
    required this.version,
    required this.status,
    required this.turnPolicyRef,
    required this.sessionPolicyRef,
    required this.prompt,
    required this.roles,
    required this.practiceOptions,
  });

  final String id;
  final SceneFamily family;
  final SceneModel model;
  final String name;
  final int version;
  final SceneStatus status;
  final String turnPolicyRef;
  final String sessionPolicyRef;
  final ScenePrompt prompt;
  final List<RoleDefinition> roles;
  final List<PracticeOption> practiceOptions;
}

extension ScenePresentation on SceneDefinition {
  ScenePresentationMode get presentationMode => switch (family) {
    SceneFamily.workplace ||
    SceneFamily.daily => ScenePresentationMode.immersiveRoleplay,
    SceneFamily.interview || SceneFamily.exam => ScenePresentationMode.standard,
  };
}

final class SceneSelectionSnapshot {
  const SceneSelectionSnapshot({
    required this.scene,
    required this.selectedRoleIds,
    required this.practiceOptionId,
  });

  final SceneDefinition scene;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
}
