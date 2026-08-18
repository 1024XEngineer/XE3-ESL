import 'package:speakup/features/agent/client_action/agent_client_action.dart';

const practicePlanConfirmClientActionV1Type = 'practice.plan.confirm.v1';
const practicePlanConfirmClientActionType = 'practice.plan.confirm.v2';

bool isConfirmPracticePlanClientActionType(String type) =>
    type == practicePlanConfirmClientActionV1Type ||
    type == practicePlanConfirmClientActionType;

ConfirmPracticePlanClientAction? tryDecodeConfirmPracticePlanClientAction(
  AgentClientAction action,
) {
  if (!isConfirmPracticePlanClientActionType(action.type)) {
    return null;
  }
  try {
    return decodeConfirmPracticePlanClientAction(action);
  } on FormatException {
    return null;
  }
}

enum ConfirmPracticePlanProtocol { v1, v2 }

final class ConfirmPracticePlanClientAction {
  const ConfirmPracticePlanClientAction({
    required this.label,
    required this.practicePlanId,
    required this.planVersion,
    required this.sceneId,
    required this.sceneName,
    required this.practiceGoal,
    required this.aiRoles,
    required this.practiceExperience,
    required this.sceneCategory,
    required this.practiceMode,
    required this.practiceScope,
    required this.suggestedDuration,
    required this.minEffectiveTurns,
    required this.maxEffectiveTurns,
    required this.confirmationPrompt,
    this.userRole,
    this.protocol = ConfirmPracticePlanProtocol.v2,
  });

  final ConfirmPracticePlanProtocol protocol;
  final String label;
  final String practicePlanId;
  final int planVersion;
  final String? sceneId;
  final String sceneName;
  final String? userRole;
  final List<String> aiRoles;
  final String practiceGoal;
  final String practiceExperience;
  final String sceneCategory;
  final String practiceMode;
  final String practiceScope;
  final Duration suggestedDuration;
  final int minEffectiveTurns;
  final int maxEffectiveTurns;
  final String confirmationPrompt;
}

const _confirmPracticePlanV1Fields = <String>{
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

const _confirmPracticePlanV2Fields = <String>{
  'label',
  'practice_plan_id',
  'plan_version',
  'scene_id',
  'scene_name',
  'user_role',
  'ai_roles',
  'practice_goal',
  'practice_experience',
  'scene_category',
  'practice_mode',
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
  if (action.protocol != ConfirmPracticePlanProtocol.v2 ||
      action.userRole == null) {
    _rejectPracticePlanAction();
  }
  final envelope = AgentClientAction(
    type: practicePlanConfirmClientActionType,
    payload: <String, Object?>{
      'label': action.label,
      'practice_plan_id': action.practicePlanId,
      'plan_version': action.planVersion,
      'scene_id': action.sceneId,
      'scene_name': action.sceneName,
      'user_role': action.userRole,
      'ai_roles': action.aiRoles,
      'practice_goal': action.practiceGoal,
      'practice_experience': action.practiceExperience,
      'scene_category': action.sceneCategory,
      'practice_mode': action.practiceMode,
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
  final protocol = switch (action.type) {
    practicePlanConfirmClientActionV1Type => ConfirmPracticePlanProtocol.v1,
    practicePlanConfirmClientActionType => ConfirmPracticePlanProtocol.v2,
    _ => _rejectPracticePlanAction(),
  };
  final expectedFields = switch (protocol) {
    ConfirmPracticePlanProtocol.v1 => _confirmPracticePlanV1Fields,
    ConfirmPracticePlanProtocol.v2 => _confirmPracticePlanV2Fields,
  };
  final actualFields = action.payload.keys.toSet();
  if (actualFields.length != expectedFields.length ||
      !actualFields.containsAll(expectedFields)) {
    _rejectPracticePlanAction();
  }
  final object = action.payload;
  final practiceExperience = _string(object['practice_experience'], 1, 100);
  final sceneCategory = _string(object['scene_category'], 1, 200);
  final practiceMode = _string(object['practice_mode'], 1, 64);
  final aiRoles = _roles(
    object[switch (protocol) {
      ConfirmPracticePlanProtocol.v1 => 'roles',
      ConfirmPracticePlanProtocol.v2 => 'ai_roles',
    }],
  );
  final minTurns = _integer(object['min_effective_turns'], 1, 100);
  final maxTurns = _integer(object['max_effective_turns'], 0, 100);
  if (!_practiceExperiences.contains(practiceExperience) ||
      !_sceneCategories.contains(sceneCategory) ||
      !_practiceModes.contains(practiceMode) ||
      (maxTurns != 0 && maxTurns < minTurns)) {
    _rejectPracticePlanAction();
  }

  return ConfirmPracticePlanClientAction(
    protocol: protocol,
    label: _string(object['label'], 1, 100),
    practicePlanId: _uuid(object['practice_plan_id']),
    planVersion: _integer(object['plan_version'], 1),
    sceneId: protocol == ConfirmPracticePlanProtocol.v2
        ? _string(object['scene_id'], 1, 200)
        : null,
    sceneName: _string(object['scene_name'], 1, 200),
    userRole: protocol == ConfirmPracticePlanProtocol.v2
        ? _string(object['user_role'], 1, 200)
        : null,
    aiRoles: aiRoles,
    practiceGoal: _string(
      object[switch (protocol) {
        ConfirmPracticePlanProtocol.v1 => 'target',
        ConfirmPracticePlanProtocol.v2 => 'practice_goal',
      }],
      1,
      500,
    ),
    practiceExperience: practiceExperience,
    sceneCategory: sceneCategory,
    practiceMode: practiceMode,
    practiceScope: _string(object['practice_scope'], 1, 200),
    suggestedDuration: Duration(
      seconds: _integer(object['suggested_duration_seconds'], 1),
    ),
    minEffectiveTurns: minTurns,
    maxEffectiveTurns: maxTurns,
    confirmationPrompt: _string(object['confirmation_prompt'], 1, 300),
  );
}

List<String> _roles(Object? value) {
  if (value is! List || value.isEmpty || value.length > 8) {
    _rejectPracticePlanAction();
  }
  final roles = value
      .map((role) => _string(role, 1, 200))
      .toList(growable: false);
  if (roles.toSet().length != roles.length) {
    _rejectPracticePlanAction();
  }
  return List<String>.unmodifiable(roles);
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
