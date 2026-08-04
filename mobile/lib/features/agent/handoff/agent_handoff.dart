sealed class AgentHandoff {
  const AgentHandoff();
}

final class ConfirmPracticePlanHandoff extends AgentHandoff {
  const ConfirmPracticePlanHandoff({
    required this.label,
    required this.practicePlanId,
    required this.planRevision,
    required this.target,
    required this.sceneName,
    required this.sceneFamily,
    required this.sceneModel,
    required this.roles,
    required this.practiceScope,
    required this.suggestedDuration,
    required this.minEffectiveTurns,
    required this.maxEffectiveTurns,
    required this.executableStatus,
    required this.confirmationPrompt,
  });

  final String label;
  final String practicePlanId;
  final int planRevision;
  final String target;
  final String sceneName;
  final String sceneFamily;
  final String sceneModel;
  final List<String> roles;
  final String practiceScope;
  final Duration suggestedDuration;
  final int minEffectiveTurns;
  final int maxEffectiveTurns;
  final String executableStatus;
  final String confirmationPrompt;
}
