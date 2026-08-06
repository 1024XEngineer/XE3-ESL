enum PracticeExperience {
  interview('INTERVIEW'),
  ieltsSpeaking('IELTS_SPEAKING'),
  workplace('WORKPLACE'),
  lifeAndTravel('LIFE_AND_TRAVEL');

  const PracticeExperience(this.wireValue);

  final String wireValue;

  static PracticeExperience? fromWireValue(String value) => PracticeExperience
      .values
      .where((experience) => experience.wireValue == value)
      .firstOrNull;
}

enum SceneCategory {
  interviewRecruiter('INTERVIEW_RECRUITER'),
  interviewBehavioral('INTERVIEW_BEHAVIORAL'),
  interviewProfessional('INTERVIEW_PROFESSIONAL'),
  interviewHiringManager('INTERVIEW_HIRING_MANAGER'),
  interviewCustom('INTERVIEW_CUSTOM'),
  ieltsSpeaking('IELTS_SPEAKING'),
  workplaceGeneral('WORKPLACE_GENERAL'),
  lifeTravel('LIFE_TRAVEL'),
  lifeDaily('LIFE_DAILY');

  const SceneCategory(this.wireValue);

  final String wireValue;

  static SceneCategory? fromWireValue(String value) => SceneCategory.values
      .where((category) => category.wireValue == value)
      .firstOrNull;
}

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
  });

  final String publicSceneBrief;
  final String practiceGoal;
  final String userRole;
  final String aiRole;
  final String personaSummary;
  final List<String> focusAreas;
  final List<String> turnBlueprints;
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

enum PracticeMode {
  fullSimulation('FULL_SIMULATION'),
  focus('FOCUS'),
  fullMock('FULL_MOCK'),
  part1('PART_1'),
  part2('PART_2'),
  part3('PART_3');

  const PracticeMode(this.wireValue);

  final String wireValue;

  static PracticeMode? fromWireValue(String value) =>
      PracticeMode.values.where((mode) => mode.wireValue == value).firstOrNull;
}

final class PracticeOption {
  const PracticeOption({
    required this.id,
    required this.sceneId,
    required this.mode,
    required this.displayName,
    required this.suggestedDurationSeconds,
    required this.turnPolicyRef,
    required this.sessionPolicyRef,
    required this.evaluationPolicyRef,
    this.roleId,
  });

  final String id;
  final String sceneId;
  final PracticeMode mode;
  final String displayName;
  final int suggestedDurationSeconds;
  final String turnPolicyRef;
  final String sessionPolicyRef;
  final String evaluationPolicyRef;
  final String? roleId;
}

final class SceneDefinition {
  const SceneDefinition({
    required this.id,
    required this.experience,
    required this.category,
    required this.name,
    required this.version,
    required this.status,
    required this.prompt,
    required this.roles,
    required this.practiceOptions,
  });

  final String id;
  final PracticeExperience experience;
  final SceneCategory category;
  final String name;
  final int version;
  final SceneStatus status;
  final ScenePrompt prompt;
  final List<RoleDefinition> roles;
  final List<PracticeOption> practiceOptions;
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
