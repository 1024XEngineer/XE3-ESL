import 'package:speakup/features/agent/client_action/agent_client_action.dart';

const practicePlanConfirmClientActionType = 'practice.plan.confirm.v1';

final class ConfirmPracticePlanClientAction {
  const ConfirmPracticePlanClientAction({
    required this.label,
    required this.practicePlanId,
    required this.planVersion,
    required this.target,
    required this.sceneName,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.roles,
    required this.practiceScope,
    required this.suggestedDuration,
    required this.minEffectiveTurns,
    required this.maxEffectiveTurns,
    required this.confirmationPrompt,
  });

  final String label;
  final String practicePlanId;
  final int planVersion;
  final String target;
  final String sceneName;
  final String practiceExperience;
  final String sceneCategory;
  final String practiceMode;
  final List<String> roles;
  final String practiceScope;
  final Duration suggestedDuration;
  final int minEffectiveTurns;
  final int maxEffectiveTurns;
  final String confirmationPrompt;
}

const _confirmPracticePlanFields = <String>{
  'label',
  'practice_plan_id',
  'plan_version',
  'target',
  'scene_name',
  'practice_experience',
  'scene_category',
  'practice_mode',
  'roles',
  'practice_scope',
  'suggested_duration_seconds',
  'min_effective_turns',
  'max_effective_turns',
  'confirmation_prompt',
};

const _practiceExperiences = <String>{
  'INTERVIEW',
  'IELTS_SPEAKING',
  'WORKPLACE',
  'LIFE_AND_TRAVEL',
};

const _sceneCategories = <String>{
  'INTERVIEW_RECRUITER',
  'INTERVIEW_BEHAVIORAL',
  'INTERVIEW_PROFESSIONAL',
  'INTERVIEW_HIRING_MANAGER',
  'INTERVIEW_CUSTOM',
  'IELTS_SPEAKING',
  'WORKPLACE_GENERAL',
  'LIFE_TRAVEL',
  'LIFE_DAILY',
};

const _practiceModes = <String>{
  'FULL_SIMULATION',
  'FOCUS',
  'FULL_MOCK',
  'PART_1',
  'PART_2',
  'PART_3',
};

final _uuidPattern = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);

AgentClientAction encodeConfirmPracticePlanClientAction(
  ConfirmPracticePlanClientAction action,
) {
  final envelope = AgentClientAction(
    type: practicePlanConfirmClientActionType,
    payload: <String, Object?>{
      'label': action.label,
      'practice_plan_id': action.practicePlanId,
      'plan_version': action.planVersion,
      'target': action.target,
      'scene_name': action.sceneName,
      'practice_experience': action.practiceExperience,
      'scene_category': action.sceneCategory,
      'practice_mode': action.practiceMode,
      'roles': action.roles,
      'practice_scope': action.practiceScope,
      'suggested_duration_seconds': action.suggestedDuration.inSeconds,
      'min_effective_turns': action.minEffectiveTurns,
      'max_effective_turns': action.maxEffectiveTurns,
      'confirmation_prompt': action.confirmationPrompt,
    },
  );
  decodeConfirmPracticePlanClientAction(envelope);
  return envelope;
}

ConfirmPracticePlanClientAction decodeConfirmPracticePlanClientAction(
  AgentClientAction action,
) {
  if (action.type != practicePlanConfirmClientActionType ||
      action.payload.keys
          .toSet()
          .difference(_confirmPracticePlanFields)
          .isNotEmpty ||
      action.payload.length != _confirmPracticePlanFields.length) {
    _rejectPracticePlanAction();
  }
  final object = action.payload;
  final practiceExperience = _string(object['practice_experience'], 1, 100);
  final sceneCategory = _string(object['scene_category'], 1, 200);
  final practiceMode = _string(object['practice_mode'], 1, 64);
  final rolesValue = object['roles'];
  if (rolesValue is! List || rolesValue.isEmpty || rolesValue.length > 8) {
    _rejectPracticePlanAction();
  }
  final roles = rolesValue
      .map((role) => _string(role, 1, 200))
      .toList(growable: false);
  final minTurns = _integer(object['min_effective_turns'], 1, 100);
  final maxTurns = _integer(object['max_effective_turns'], 0, 100);
  if (!_practiceExperiences.contains(practiceExperience) ||
      !_sceneCategories.contains(sceneCategory) ||
      !_practiceModes.contains(practiceMode) ||
      roles.toSet().length != roles.length ||
      (maxTurns != 0 && maxTurns < minTurns)) {
    _rejectPracticePlanAction();
  }

  return ConfirmPracticePlanClientAction(
    label: _string(object['label'], 1, 100),
    practicePlanId: _uuid(object['practice_plan_id']),
    planVersion: _integer(object['plan_version'], 1),
    target: _string(object['target'], 1, 500),
    sceneName: _string(object['scene_name'], 1, 200),
    practiceExperience: practiceExperience,
    sceneCategory: sceneCategory,
    practiceMode: practiceMode,
    roles: List<String>.unmodifiable(roles),
    practiceScope: _string(object['practice_scope'], 1, 200),
    suggestedDuration: Duration(
      seconds: _integer(object['suggested_duration_seconds'], 1),
    ),
    minEffectiveTurns: minTurns,
    maxEffectiveTurns: maxTurns,
    confirmationPrompt: _string(object['confirmation_prompt'], 1, 300),
  );
}

String _string(Object? value, int minimum, int maximum) {
  if (value is! String ||
      value.runes.length < minimum ||
      value.runes.length > maximum) {
    _rejectPracticePlanAction();
  }
  return value;
}

String _uuid(Object? value) {
  final result = _string(value, 36, 36);
  if (!_uuidPattern.hasMatch(result)) {
    _rejectPracticePlanAction();
  }
  return result;
}

int _integer(Object? value, int minimum, [int? maximum]) {
  if (value is! int ||
      value < minimum ||
      (maximum != null && value > maximum)) {
    _rejectPracticePlanAction();
  }
  return value;
}

Never _rejectPracticePlanAction() {
  throw const FormatException('Invalid Practice Plan client action.');
}
