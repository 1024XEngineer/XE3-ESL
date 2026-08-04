import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

const _confirmPracticePlanFields = <String>{
  'type',
  'label',
  'practice_plan_id',
  'plan_revision',
  'target',
  'scene_name',
  'scene_family',
  'scene_model',
  'roles',
  'practice_scope',
  'suggested_duration_seconds',
  'min_effective_turns',
  'max_effective_turns',
  'executable_status',
  'confirmation_prompt',
};

final _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);

List<AgentHandoff> decodeAgentHandoffs(Object? value) {
  if (value is! List || value.length > 4) {
    _rejectHandoffPayload();
  }
  return List<AgentHandoff>.unmodifiable(
    value.map(_decodeConfirmPracticePlanHandoff),
  );
}

ConfirmPracticePlanHandoff _decodeConfirmPracticePlanHandoff(Object? value) {
  if (value is! Map) {
    _rejectHandoffPayload();
  }
  final object = <String, Object?>{};
  for (final entry in value.entries) {
    final key = entry.key;
    if (key is! String ||
        !_confirmPracticePlanFields.contains(key) ||
        object.containsKey(key)) {
      _rejectHandoffPayload();
    }
    object[key] = entry.value;
  }
  if (object.length != _confirmPracticePlanFields.length) {
    _rejectHandoffPayload();
  }

  final sceneFamily = _string(object['scene_family'], 1, 100);
  final sceneModel = _string(object['scene_model'], 1, 200);
  final rolesValue = object['roles'];
  if (rolesValue is! List || rolesValue.isEmpty || rolesValue.length > 8) {
    _rejectHandoffPayload();
  }
  final roles = rolesValue
      .map((role) => _string(role, 1, 200))
      .toList(growable: false);
  final minTurns = _integer(object['min_effective_turns'], 1, 100);
  final maxTurns = _integer(object['max_effective_turns'], 1, 100);
  if (_string(object['type'], 1, 64) != 'confirm_practice_plan' ||
      SceneFamily.fromWireValue(sceneFamily) == null ||
      SceneModel.fromWireValue(sceneModel) == null ||
      roles.toSet().length != roles.length ||
      maxTurns < minTurns ||
      object['executable_status'] != 'ready') {
    _rejectHandoffPayload();
  }

  return ConfirmPracticePlanHandoff(
    label: _string(object['label'], 1, 100),
    practicePlanId: _uuid(object['practice_plan_id']),
    planRevision: _integer(object['plan_revision'], 1),
    target: _string(object['target'], 1, 500),
    sceneName: _string(object['scene_name'], 1, 200),
    sceneFamily: sceneFamily,
    sceneModel: sceneModel,
    roles: List<String>.unmodifiable(roles),
    practiceScope: _string(object['practice_scope'], 1, 200),
    suggestedDuration: Duration(
      seconds: _integer(object['suggested_duration_seconds'], 1),
    ),
    minEffectiveTurns: minTurns,
    maxEffectiveTurns: maxTurns,
    executableStatus: 'ready',
    confirmationPrompt: _string(object['confirmation_prompt'], 1, 300),
  );
}

String _string(Object? value, int minimum, int maximum) {
  if (value is! String ||
      value.runes.length < minimum ||
      value.runes.length > maximum) {
    _rejectHandoffPayload();
  }
  return value;
}

String _uuid(Object? value) {
  final result = _string(value, 36, 36);
  if (!_uuidPattern.hasMatch(result)) {
    _rejectHandoffPayload();
  }
  return result;
}

int _integer(Object? value, int minimum, [int? maximum]) {
  if (value is! int ||
      value < minimum ||
      (maximum != null && value > maximum)) {
    _rejectHandoffPayload();
  }
  return value;
}

Never _rejectHandoffPayload() {
  throw const FormatException('Invalid Agent Handoff payload.');
}
