import 'dart:convert';

import 'package:speakup/features/coaching/scene/scene.dart';

final class SceneWireFormatException implements Exception {
  const SceneWireFormatException();
}

Map<String, Object?> encodeSceneDefinition(SceneDefinition scene) =>
    <String, Object?>{
      'scene_id': scene.id,
      'scene_family': scene.family.wireValue,
      'scene_model': scene.model.wireValue,
      'name': scene.name,
      'scene_version': scene.version,
      'status': switch (scene.status) {
        SceneStatus.active => 'active',
        SceneStatus.inactive => 'inactive',
      },
      'turn_policy_ref': scene.turnPolicyRef,
      'session_policy_ref': scene.sessionPolicyRef,
      'prompt': <String, Object?>{
        'public_scene_brief': scene.prompt.publicSceneBrief,
        'practice_goal': scene.prompt.practiceGoal,
        'user_role': scene.prompt.userRole,
        'ai_role': scene.prompt.aiRole,
        'persona_summary': scene.prompt.personaSummary,
        'focus_areas': scene.prompt.focusAreas,
        'turn_blueprints': scene.prompt.turnBlueprints,
        'suggested_duration_seconds': scene.prompt.suggestedDurationSeconds,
      },
      'roles': scene.roles
          .map(
            (role) => <String, Object?>{
              'role_definition_id': role.id,
              'scene_id': role.sceneId,
              'role_type': role.type,
              'display_name': role.displayName,
              'responsibilities': role.responsibilities,
              'style': role.style,
              'practice_objectives': role.practiceObjectives
                  .map(
                    (objective) => <String, Object?>{
                      'objective_id': objective.objectiveId,
                      'description': objective.description,
                    },
                  )
                  .toList(growable: false),
              'voice_config_ref': ?role.voiceConfigRef,
            },
          )
          .toList(growable: false),
      'practice_options': scene.practiceOptions
          .map(
            (option) => <String, Object?>{
              'practice_option_id': option.id,
              'scene_id': option.sceneId,
              'practice_option_type': option.type.wireValue,
              'display_name': option.displayName,
              'role_definition_id': ?option.roleId,
            },
          )
          .toList(growable: false),
    };

SceneDefinition decodeSceneDefinition(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scene_id',
      'scene_family',
      'scene_model',
      'name',
      'scene_version',
      'status',
      'turn_policy_ref',
      'session_policy_ref',
      'prompt',
      'roles',
      'practice_options',
    },
  );
  final sceneId = _resourceId(object['scene_id']);
  final family = SceneFamily.fromWireValue(_wireEnum(object['scene_family']));
  final model = SceneModel.fromWireValue(_wireEnum(object['scene_model']));
  final status = switch (_string(object['status'], maximumBytes: 16)) {
    'active' => SceneStatus.active,
    'inactive' => SceneStatus.inactive,
    _ => throw const SceneWireFormatException(),
  };
  if (family == null || model == null || !_validFamilyModel(family, model)) {
    throw const SceneWireFormatException();
  }

  final rawRoles = object['roles'];
  final rawOptions = object['practice_options'];
  if (rawRoles is! List<Object?> ||
      rawRoles.isEmpty ||
      rawRoles.length > 50 ||
      rawOptions is! List<Object?> ||
      rawOptions.isEmpty ||
      rawOptions.length > 100) {
    throw const SceneWireFormatException();
  }
  final roleIds = <String>{};
  final roles = <RoleDefinition>[];
  for (final value in rawRoles) {
    final role = decodeRoleDefinition(value);
    if (role.sceneId != sceneId || !roleIds.add(role.id)) {
      throw const SceneWireFormatException();
    }
    roles.add(role);
  }
  final optionIds = <String>{};
  final options = <PracticeOption>[];
  for (final value in rawOptions) {
    final option = decodePracticeOption(value);
    if (option.sceneId != sceneId ||
        !optionIds.add(option.id) ||
        (option.roleId != null && !roleIds.contains(option.roleId))) {
      throw const SceneWireFormatException();
    }
    options.add(option);
  }

  return SceneDefinition(
    id: sceneId,
    family: family,
    model: model,
    name: _string(object['name']),
    version: _version(object['scene_version']),
    status: status,
    turnPolicyRef: _resourceId(object['turn_policy_ref']),
    sessionPolicyRef: _resourceId(object['session_policy_ref']),
    prompt: _scenePrompt(object['prompt']),
    roles: List<RoleDefinition>.unmodifiable(roles),
    practiceOptions: List<PracticeOption>.unmodifiable(options),
  );
}

SceneSelectionSnapshot decodeSceneSelectionSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scene',
      'selected_role_ids',
      'practice_option_id',
    },
  );
  final scene = decodeSceneDefinition(object['scene']);
  final selectedRoleIds = _resourceIdList(object['selected_role_ids']);
  final practiceOptionId = _resourceId(object['practice_option_id']);
  final roleIds = scene.roles.map((role) => role.id).toSet();
  if (selectedRoleIds.any((id) => !roleIds.contains(id)) ||
      !scene.practiceOptions.any((option) => option.id == practiceOptionId)) {
    throw const SceneWireFormatException();
  }
  return SceneSelectionSnapshot(
    scene: scene,
    selectedRoleIds: selectedRoleIds,
    practiceOptionId: practiceOptionId,
  );
}

RoleDefinition decodeRoleDefinition(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'role_definition_id',
      'scene_id',
      'role_type',
      'display_name',
      'responsibilities',
      'style',
      'practice_objectives',
    },
    optional: const <String>{'voice_config_ref'},
  );
  return RoleDefinition(
    id: _resourceId(object['role_definition_id']),
    sceneId: _resourceId(object['scene_id']),
    type: _wireEnum(object['role_type']),
    displayName: _string(object['display_name']),
    responsibilities: _string(object['responsibilities']),
    style: _string(object['style']),
    practiceObjectives: _practiceObjectives(object['practice_objectives']),
    voiceConfigRef: object.containsKey('voice_config_ref')
        ? _resourceId(object['voice_config_ref'])
        : null,
  );
}

List<RolePracticeObjective> _practiceObjectives(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw const SceneWireFormatException();
  }
  final ids = <String>{};
  final objectives = <RolePracticeObjective>[];
  for (final item in value) {
    final object = _object(
      item,
      required: const <String>{'objective_id', 'description'},
    );
    final objectiveId = _resourceId(object['objective_id']);
    if (!ids.add(objectiveId)) {
      throw const SceneWireFormatException();
    }
    objectives.add(
      RolePracticeObjective(
        objectiveId: objectiveId,
        description: _string(object['description']),
      ),
    );
  }
  return List<RolePracticeObjective>.unmodifiable(objectives);
}

PracticeOption decodePracticeOption(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'practice_option_id',
      'scene_id',
      'practice_option_type',
      'display_name',
    },
    optional: const <String>{'role_definition_id'},
  );
  final type = PracticeOptionType.fromWireValue(
    _wireEnum(object['practice_option_type']),
  );
  final roleId = object.containsKey('role_definition_id')
      ? _resourceId(object['role_definition_id'])
      : null;
  if (type == null ||
      (type == PracticeOptionType.fullSimulation && roleId != null) ||
      (type == PracticeOptionType.focus && roleId == null)) {
    throw const SceneWireFormatException();
  }
  return PracticeOption(
    id: _resourceId(object['practice_option_id']),
    sceneId: _resourceId(object['scene_id']),
    type: type,
    displayName: _string(object['display_name']),
    roleId: roleId,
  );
}

ScenePrompt _scenePrompt(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'public_scene_brief',
      'practice_goal',
      'user_role',
      'ai_role',
      'persona_summary',
      'focus_areas',
      'turn_blueprints',
      'suggested_duration_seconds',
    },
  );
  final duration = object['suggested_duration_seconds'];
  if (duration is! int || duration < 1 || duration > 3600) {
    throw const SceneWireFormatException();
  }
  return ScenePrompt(
    publicSceneBrief: _string(object['public_scene_brief']),
    practiceGoal: _string(object['practice_goal']),
    userRole: _string(object['user_role']),
    aiRole: _string(object['ai_role']),
    personaSummary: _string(object['persona_summary']),
    focusAreas: _stringList(object['focus_areas']),
    turnBlueprints: _stringList(
      object['turn_blueprints'],
      maximumItemBytes: 4096,
    ),
    suggestedDurationSeconds: duration,
  );
}

Map<String, Object?> _object(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw const SceneWireFormatException();
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const SceneWireFormatException();
  }
  return value;
}

List<String> _resourceIdList(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw const SceneWireFormatException();
  }
  final ids = value.map(_resourceId).toList(growable: false);
  if (ids.toSet().length != ids.length) {
    throw const SceneWireFormatException();
  }
  return List<String>.unmodifiable(ids);
}

String _resourceId(Object? value) => _string(value, maximumBytes: 128);

String _wireEnum(Object? value) {
  final result = _string(value, maximumBytes: 64);
  if (!RegExp(r'^[A-Z][A-Z0-9_]*$').hasMatch(result)) {
    throw const SceneWireFormatException();
  }
  return result;
}

int _version(Object? value) {
  if (value is! int || value < 1) {
    throw const SceneWireFormatException();
  }
  return value;
}

String _string(Object? value, {int maximumBytes = 4096}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maximumBytes) {
    throw const SceneWireFormatException();
  }
  return value;
}

List<String> _stringList(Object? value, {int maximumItemBytes = 128}) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw const SceneWireFormatException();
  }
  final result = value
      .map((item) => _string(item, maximumBytes: maximumItemBytes))
      .toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const SceneWireFormatException();
  }
  return List<String>.unmodifiable(result);
}

bool _validFamilyModel(SceneFamily family, SceneModel model) =>
    switch ((family, model)) {
      (SceneFamily.interview, SceneModel.projectExperienceDeepDive) ||
      (SceneFamily.interview, SceneModel.interviewBasicDialogue) ||
      (SceneFamily.exam, SceneModel.ieltsSpeakingPart1) ||
      (SceneFamily.exam, SceneModel.ieltsSpeakingPart2) ||
      (SceneFamily.exam, SceneModel.ieltsSpeakingPart3) ||
      (SceneFamily.exam, SceneModel.ieltsSpeakingFullMock) ||
      (SceneFamily.exam, SceneModel.examBasicDialogue) ||
      (SceneFamily.workplace, SceneModel.progressAndRiskUpdate) ||
      (SceneFamily.workplace, SceneModel.workplaceBasicDialogue) ||
      (SceneFamily.daily, SceneModel.hotelCheckinAndIssueHandling) ||
      (SceneFamily.daily, SceneModel.dailyBasicDialogue) => true,
      _ => false,
    };
