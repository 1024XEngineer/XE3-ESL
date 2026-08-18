import 'dart:convert';

import 'package:speakup/features/coaching/scene/scene.dart';

final class SceneWireFormatException implements Exception {
  const SceneWireFormatException();
}

Map<String, Object?> encodeSceneDefinition(SceneDefinition scene) =>
    <String, Object?>{
      'scene_id': scene.id,
      'practice_experience': scene.experience.wireValue,
      'scene_category': scene.category.wireValue,
      'name': scene.name,
      'scene_version': scene.version,
      'status': switch (scene.status) {
        SceneStatus.active => 'active',
        SceneStatus.inactive => 'inactive',
      },
      'prompt': <String, Object?>{
        'public_scene_brief': scene.prompt.publicSceneBrief,
        'practice_goal': scene.prompt.practiceGoal,
        'user_role': scene.prompt.userRole,
        'ai_role': scene.prompt.aiRole,
        'persona_summary': scene.prompt.personaSummary,
        'focus_areas': scene.prompt.focusAreas,
        'turn_blueprints': scene.prompt.turnBlueprints,
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
              'practice_mode': option.mode.wireValue,
              'display_name': option.displayName,
              'suggested_duration_seconds': option.suggestedDurationSeconds,
              'turn_policy_ref': option.turnPolicyRef,
              'session_policy_ref': option.sessionPolicyRef,
              'evaluation_policy_ref': option.evaluationPolicyRef,
              'role_definition_id': ?option.roleId,
            },
          )
          .toList(growable: false),
    };

Map<String, Object?> encodeSceneSelectionSnapshot(
  SceneSelectionSnapshot selection,
) {
  final definition = encodeSceneDefinition(selection.scene);
  final roles = (definition['roles']! as List<Object?>)
      .map((value) {
        final role = Map<String, Object?>.from(value! as Map<String, Object?>);
        role['scene_key'] = role.remove('scene_id');
        return role;
      })
      .toList(growable: false);
  final options = (definition['practice_options']! as List<Object?>)
      .map((value) {
        final option = Map<String, Object?>.from(
          value! as Map<String, Object?>,
        );
        option['scene_key'] = option.remove('scene_id');
        return option;
      })
      .toList(growable: false);
  return <String, Object?>{
    'source': switch (selection.source.type) {
      SceneSourceType.catalog => <String, Object?>{
        'type': 'CATALOG',
        'scene_id': selection.source.sceneId,
        'scene_version': selection.source.sceneVersion,
      },
      SceneSourceType.custom => <String, Object?>{'type': 'CUSTOM'},
    },
    'scene': <String, Object?>{
      'scene_key': definition['scene_id'],
      'scene_revision': definition['scene_version'],
      'practice_experience': definition['practice_experience'],
      'scene_category': definition['scene_category'],
      'name': definition['name'],
      'prompt': definition['prompt'],
      'roles': roles,
      'practice_options': options,
    },
    'selected_role_ids': selection.selectedRoleIds,
    'practice_option_id': selection.practiceOptionId,
  };
}

SceneDefinition decodeSceneDefinition(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scene_id',
      'practice_experience',
      'scene_category',
      'name',
      'scene_version',
      'status',
      'prompt',
      'roles',
      'practice_options',
    },
  );
  final sceneId = _resourceId(object['scene_id']);
  final experience = PracticeExperience.fromWireValue(
    _wireEnum(object['practice_experience']),
  );
  final category = SceneCategory.fromWireValue(
    _wireEnum(object['scene_category']),
  );
  final status = switch (_string(object['status'], maximumBytes: 16)) {
    'active' => SceneStatus.active,
    'inactive' => SceneStatus.inactive,
    _ => throw const SceneWireFormatException(),
  };
  if (experience == null ||
      category == null ||
      !_validExperienceCategory(experience, category)) {
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
    experience: experience,
    category: category,
    name: _string(object['name']),
    version: _version(object['scene_version']),
    status: status,
    prompt: _scenePrompt(object['prompt']),
    roles: List<RoleDefinition>.unmodifiable(roles),
    practiceOptions: List<PracticeOption>.unmodifiable(options),
  );
}

SceneSelectionSnapshot decodeSceneSelectionSnapshot(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const SceneWireFormatException();
  }
  return value.containsKey('source')
      ? _decodeCurrentSceneSelectionSnapshot(value)
      : _decodeLegacySceneSelectionSnapshot(value);
}

SceneSelectionSnapshot _decodeCurrentSceneSelectionSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'source',
      'scene',
      'selected_role_ids',
      'practice_option_id',
    },
  );
  final source = _decodeSceneSource(object['source']);
  final scene = _decodeExecutableSceneSnapshot(object['scene']);
  if ((source.type == SceneSourceType.catalog &&
          (source.sceneId != scene.id ||
              source.sceneVersion != scene.version)) ||
      (source.type == SceneSourceType.custom &&
          (!scene.id.startsWith('custom:') || scene.version != 1))) {
    throw const SceneWireFormatException();
  }
  return _validatedSceneSelectionSnapshot(
    source: source,
    scene: scene,
    selectedRoleIds: object['selected_role_ids'],
    practiceOptionId: object['practice_option_id'],
  );
}

SceneSelectionSnapshot _decodeLegacySceneSelectionSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scene',
      'selected_role_ids',
      'practice_option_id',
    },
  );
  final scene = decodeSceneDefinition(object['scene']);
  return _validatedSceneSelectionSnapshot(
    source: SceneSource.catalog(sceneId: scene.id, sceneVersion: scene.version),
    scene: scene,
    selectedRoleIds: object['selected_role_ids'],
    practiceOptionId: object['practice_option_id'],
  );
}

SceneSelectionSnapshot _validatedSceneSelectionSnapshot({
  required SceneSource source,
  required SceneDefinition scene,
  required Object? selectedRoleIds,
  required Object? practiceOptionId,
}) {
  final decodedRoleIds = _resourceIdList(selectedRoleIds);
  final decodedOptionId = _resourceId(practiceOptionId);
  final roleIds = scene.roles.map((role) => role.id).toSet();
  if (decodedRoleIds.any((id) => !roleIds.contains(id)) ||
      !scene.practiceOptions.any((option) => option.id == decodedOptionId)) {
    throw const SceneWireFormatException();
  }
  return SceneSelectionSnapshot(
    source: source,
    scene: scene,
    selectedRoleIds: decodedRoleIds,
    practiceOptionId: decodedOptionId,
  );
}

SceneSource _decodeSceneSource(Object? value) {
  final base = _object(
    value,
    required: const <String>{'type'},
    optional: const <String>{'scene_id', 'scene_version'},
  );
  return switch (_string(base['type'], maximumBytes: 16)) {
    'CATALOG' => SceneSource.catalog(
      sceneId: _resourceId(base['scene_id']),
      sceneVersion: _version(base['scene_version']),
    ),
    'CUSTOM'
        when !base.containsKey('scene_id') &&
            !base.containsKey('scene_version') =>
      const SceneSource.custom(),
    _ => throw const SceneWireFormatException(),
  };
}

// The Practice UI consumes the frozen executable scene through the existing
// SceneDefinition view model. This adapter does not recover catalog identity:
// custom scenes keep their runtime scene_key and remain distinguished by
// SceneSelectionSnapshot.source.
SceneDefinition _decodeExecutableSceneSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scene_key',
      'scene_revision',
      'practice_experience',
      'scene_category',
      'name',
      'prompt',
      'roles',
      'practice_options',
    },
  );
  final sceneKey = _resourceId(object['scene_key']);
  final roles = object['roles'];
  final options = object['practice_options'];
  if (roles is! List<Object?> || options is! List<Object?>) {
    throw const SceneWireFormatException();
  }
  return decodeSceneDefinition(<String, Object?>{
    'scene_id': sceneKey,
    'scene_version': object['scene_revision'],
    'practice_experience': object['practice_experience'],
    'scene_category': object['scene_category'],
    'name': object['name'],
    'status': 'active',
    'prompt': object['prompt'],
    'roles': roles
        .map((value) {
          final role = _object(
            value,
            required: const <String>{
              'role_definition_id',
              'scene_key',
              'role_type',
              'display_name',
              'responsibilities',
              'style',
              'practice_objectives',
            },
            optional: const <String>{'voice_config_ref'},
          );
          if (_resourceId(role['scene_key']) != sceneKey) {
            throw const SceneWireFormatException();
          }
          return <String, Object?>{...role, 'scene_id': role['scene_key']}
            ..remove('scene_key');
        })
        .toList(growable: false),
    'practice_options': options
        .map((value) {
          final option = _object(
            value,
            required: const <String>{
              'practice_option_id',
              'scene_key',
              'practice_mode',
              'display_name',
              'suggested_duration_seconds',
              'turn_policy_ref',
              'session_policy_ref',
              'evaluation_policy_ref',
            },
            optional: const <String>{'role_definition_id'},
          );
          if (_resourceId(option['scene_key']) != sceneKey) {
            throw const SceneWireFormatException();
          }
          return <String, Object?>{...option, 'scene_id': option['scene_key']}
            ..remove('scene_key');
        })
        .toList(growable: false),
  });
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
      'practice_mode',
      'display_name',
      'suggested_duration_seconds',
      'turn_policy_ref',
      'session_policy_ref',
      'evaluation_policy_ref',
    },
    optional: const <String>{'role_definition_id'},
  );
  final mode = PracticeMode.fromWireValue(_wireEnum(object['practice_mode']));
  final roleId = object.containsKey('role_definition_id')
      ? _resourceId(object['role_definition_id'])
      : null;
  final duration = object['suggested_duration_seconds'];
  if (mode == null ||
      duration is! int ||
      duration < 1 ||
      duration > 3600 ||
      (mode == PracticeMode.fullSimulation && roleId != null) ||
      (mode == PracticeMode.focus && roleId == null) ||
      (mode != PracticeMode.fullSimulation &&
          mode != PracticeMode.focus &&
          roleId != null)) {
    throw const SceneWireFormatException();
  }
  return PracticeOption(
    id: _resourceId(object['practice_option_id']),
    sceneId: _resourceId(object['scene_id']),
    mode: mode,
    displayName: _string(object['display_name']),
    suggestedDurationSeconds: duration,
    turnPolicyRef: _resourceId(object['turn_policy_ref']),
    sessionPolicyRef: _resourceId(object['session_policy_ref']),
    evaluationPolicyRef: _resourceId(object['evaluation_policy_ref']),
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
    },
  );
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

bool _validExperienceCategory(
  PracticeExperience experience,
  SceneCategory category,
) => switch ((experience, category)) {
  (PracticeExperience.interview, SceneCategory.interviewRecruiter) ||
  (PracticeExperience.interview, SceneCategory.interviewBehavioral) ||
  (PracticeExperience.interview, SceneCategory.interviewProfessional) ||
  (PracticeExperience.interview, SceneCategory.interviewHiringManager) ||
  (PracticeExperience.interview, SceneCategory.interviewCustom) ||
  (PracticeExperience.ieltsSpeaking, SceneCategory.ieltsSpeaking) ||
  (PracticeExperience.workplace, SceneCategory.workplaceGeneral) ||
  (PracticeExperience.lifeAndTravel, SceneCategory.lifeTravel) ||
  (PracticeExperience.lifeAndTravel, SceneCategory.lifeDaily) => true,
  _ => false,
};
