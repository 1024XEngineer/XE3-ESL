final class PreparationScenario {
  const PreparationScenario({
    required this.id,
    required this.type,
    required this.name,
    required this.version,
    required this.status,
  });

  final String id;
  final String type;
  final String name;
  final int version;
  final String status;
}

final class PreparationScenarioConfig {
  const PreparationScenarioConfig({
    required this.id,
    required this.scenarioId,
    required this.type,
    required this.version,
    required this.jobTitle,
    required this.jobDescription,
    required this.focusAreas,
  });

  final String id;
  final String scenarioId;
  final String type;
  final int version;
  final String jobTitle;
  final String jobDescription;
  final List<String> focusAreas;
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
