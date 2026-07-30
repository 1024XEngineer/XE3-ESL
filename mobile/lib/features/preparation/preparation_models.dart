final class PreparationScenario {
  const PreparationScenario({
    required this.id,
    required this.type,
    required this.model,
    required this.name,
    required this.summary,
    required this.version,
    required this.status,
  });

  final String id;
  final String type;
  final String model;
  final String name;
  final String summary;
  final int version;
  final String status;
}

final class PreparationScenarioConfig {
  const PreparationScenarioConfig({
    required this.id,
    required this.scenarioId,
    required this.type,
    required this.model,
    required this.version,
    required this.jobTitle,
    required this.jobDescription,
    required this.prompt,
  });

  final String id;
  final String scenarioId;
  final String type;
  final String model;
  final int version;
  final String? jobTitle;
  final String? jobDescription;
  final PreparationScenarioPrompt prompt;
}

final class PreparationScenarioPrompt {
  const PreparationScenarioPrompt({
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

final class PreparationRole {
  const PreparationRole({
    required this.id,
    required this.scenarioId,
    required this.type,
    required this.displayName,
    required this.responsibilities,
    required this.style,
    required this.focusAreas,
    required this.version,
    this.voiceConfigRef,
  });

  final String id;
  final String scenarioId;
  final String type;
  final String displayName;
  final String responsibilities;
  final String style;
  final List<String> focusAreas;
  final int version;
  final String? voiceConfigRef;
}

enum PreparationOptionType {
  fullSimulation('FULL_SIMULATION'),
  focus('FOCUS');

  const PreparationOptionType(this.wireValue);

  final String wireValue;
}

final class PreparationOption {
  const PreparationOption({
    required this.id,
    required this.scenarioId,
    required this.type,
    required this.displayName,
    required this.version,
    this.roleId,
  });

  final String id;
  final String scenarioId;
  final PreparationOptionType type;
  final String displayName;
  final int version;
  final String? roleId;
}

final class PreparationScenarioDetail {
  const PreparationScenarioDetail({
    required this.scenario,
    required this.config,
    required this.options,
  });

  final PreparationScenario scenario;
  final PreparationScenarioConfig config;
  final List<PreparationOption> options;
}
