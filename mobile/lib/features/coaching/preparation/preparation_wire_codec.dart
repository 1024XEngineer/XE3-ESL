import 'dart:convert';

import 'package:speakup/features/coaching/preparation/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';

final class PreparationWireFormatException implements Exception {
  const PreparationWireFormatException();
}

typedef JobTargetInputDecoder = JobTargetInput Function(Object? value);
typedef JobTargetCandidateDecoder = JobTargetCandidate Function(Object? value);

PreparationProfile decodePreparationProfileBody(String body) =>
    decodePreparationProfile(_decodeJson(body));

PreparationProfile decodePreparationProfile(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'preparation_profile_id',
      'user_id',
      'background_summary',
      'version',
      'updated_at',
    },
    optional: const <String>{
      'resume_ref',
      'job_description_ref',
      'job_target_id',
      'job_target_confirmation_version',
    },
  );
  final hasJobTarget = object.containsKey('job_target_id');
  if (hasJobTarget != object.containsKey('job_target_confirmation_version')) {
    throw const PreparationWireFormatException();
  }
  return PreparationProfile(
    id: _resourceId(object['preparation_profile_id']),
    userId: _resourceId(object['user_id']),
    resumeRef: object.containsKey('resume_ref')
        ? _text(object['resume_ref'], maximumBytes: 16 * 1024)
        : null,
    jobDescriptionRef: object.containsKey('job_description_ref')
        ? _text(object['job_description_ref'], maximumBytes: 16 * 1024)
        : null,
    backgroundSummary: _text(
      object['background_summary'],
      maximumBytes: 64 * 1024,
    ),
    jobTargetId: hasJobTarget ? _resourceId(object['job_target_id']) : null,
    jobTargetConfirmationVersion: hasJobTarget
        ? _version(object['job_target_confirmation_version'])
        : null,
    version: _version(object['version']),
    updatedAt: _dateTime(object['updated_at']),
  );
}

PreparationSnapshot decodePreparationSnapshotBody(
  String body, {
  JobTargetInputDecoder? decodeJobTargetInput,
  JobTargetCandidateDecoder? decodeJobTargetCandidate,
}) => decodePreparationSnapshot(
  _decodeJson(body),
  decodeJobTargetInput: decodeJobTargetInput,
  decodeJobTargetCandidate: decodeJobTargetCandidate,
);

PreparationSnapshot decodePreparationSnapshot(
  Object? value, {
  JobTargetInputDecoder? decodeJobTargetInput,
  JobTargetCandidateDecoder? decodeJobTargetCandidate,
}) {
  final object = _object(
    value,
    required: const <String>{
      'preparation_snapshot_id',
      'source_profile_id',
      'source_version',
      'background_snapshot',
      'created_at',
    },
    optional: const <String>{
      'source_job_target_id',
      'source_job_target_confirmation_version',
      'job_target_input_snapshot',
      'job_target_candidate_snapshot',
      'resume_snapshot',
      'job_description_snapshot',
    },
  );
  const jobKeys = <String>{
    'source_job_target_id',
    'source_job_target_confirmation_version',
    'job_target_input_snapshot',
    'job_target_candidate_snapshot',
  };
  final presentJobKeys = jobKeys.where(object.containsKey).length;
  if (presentJobKeys != 0 &&
      (presentJobKeys != jobKeys.length ||
          decodeJobTargetInput == null ||
          decodeJobTargetCandidate == null)) {
    throw const PreparationWireFormatException();
  }
  final hasJobTarget = presentJobKeys == jobKeys.length;
  final input = hasJobTarget
      ? decodeJobTargetInput!(object['job_target_input_snapshot'])
      : null;
  final candidate = hasJobTarget
      ? decodeJobTargetCandidate!(object['job_target_candidate_snapshot'])
      : null;
  if (input != null && candidate != null && input.source != candidate.source) {
    throw const PreparationWireFormatException();
  }
  return PreparationSnapshot(
    id: _resourceId(object['preparation_snapshot_id']),
    sourceProfileId: _resourceId(object['source_profile_id']),
    sourceVersion: _version(object['source_version']),
    sourceJobTargetId: hasJobTarget
        ? _resourceId(object['source_job_target_id'])
        : null,
    sourceJobTargetConfirmationVersion: hasJobTarget
        ? _version(object['source_job_target_confirmation_version'])
        : null,
    jobTargetInput: input,
    jobTargetCandidate: candidate,
    resumeSnapshot: object.containsKey('resume_snapshot')
        ? _text(object['resume_snapshot'], maximumBytes: 64 * 1024)
        : null,
    jobDescriptionSnapshot: object.containsKey('job_description_snapshot')
        ? _text(object['job_description_snapshot'], maximumBytes: 64 * 1024)
        : null,
    backgroundSnapshot: _text(
      object['background_snapshot'],
      maximumBytes: 64 * 1024,
    ),
    createdAt: _dateTime(object['created_at']),
  );
}

PracticePlan decodePracticePlanBody(
  String body, {
  JobTargetInputDecoder? decodeJobTargetInput,
  JobTargetCandidateDecoder? decodeJobTargetCandidate,
}) => decodePracticePlan(
  _decodeJson(body),
  decodeJobTargetInput: decodeJobTargetInput,
  decodeJobTargetCandidate: decodeJobTargetCandidate,
);

PracticePlan decodePracticePlan(
  Object? value, {
  JobTargetInputDecoder? decodeJobTargetInput,
  JobTargetCandidateDecoder? decodeJobTargetCandidate,
}) {
  final object = _object(
    value,
    required: const <String>{
      'practice_plan_id',
      'user_id',
      'preparation_snapshot',
      'scene_selection',
      'session_policy',
      'practice_objectives',
      'plan_revision',
      'practice_plan_status',
      'created_at',
      'updated_at',
    },
    optional: const <String>{
      'source_thread_id',
      'goal_snapshot',
      'ielts_assignment',
    },
  );
  late final SceneSelectionSnapshot sceneSelection;
  try {
    sceneSelection = decodeSceneSelectionSnapshot(object['scene_selection']);
  } on SceneWireFormatException {
    throw const PreparationWireFormatException();
  }
  final createdAt = _dateTime(object['created_at']);
  final updatedAt = _dateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const PreparationWireFormatException();
  }
  final sessionPolicy = decodePreparationSessionPolicy(
    object['session_policy'],
  );
  final ieltsAssignment = object.containsKey('ielts_assignment')
      ? decodeIeltsPracticeAssignment(object['ielts_assignment'])
      : null;
  final expectedIeltsMode = _ieltsModeForScene(sceneSelection.scene.model);
  if (ieltsAssignment?.mode != expectedIeltsMode ||
      (ieltsAssignment != null &&
          (ieltsAssignment.turnBlueprints.length !=
                  sessionPolicy.maxEffectiveTurns ||
              !_sameStrings(
                ieltsAssignment.turnBlueprints,
                sceneSelection.scene.prompt.turnBlueprints,
              )))) {
    throw const PreparationWireFormatException();
  }
  return PracticePlan(
    id: _resourceId(object['practice_plan_id']),
    userId: _resourceId(object['user_id']),
    sourceThreadId: object.containsKey('source_thread_id')
        ? _resourceId(object['source_thread_id'])
        : null,
    goalSnapshot: object.containsKey('goal_snapshot')
        ? _goalSnapshot(object['goal_snapshot'])
        : null,
    preparationSnapshot: decodePreparationSnapshot(
      object['preparation_snapshot'],
      decodeJobTargetInput: decodeJobTargetInput,
      decodeJobTargetCandidate: decodeJobTargetCandidate,
    ),
    sceneSelection: sceneSelection,
    sessionPolicy: sessionPolicy,
    practiceObjectives: decodePracticeObjectives(object['practice_objectives']),
    ieltsAssignment: ieltsAssignment,
    revision: _version(object['plan_revision']),
    status: switch (_text(object['practice_plan_status'], maximumBytes: 16)) {
      'ready' => PracticePlanStatus.ready,
      'archived' => PracticePlanStatus.archived,
      _ => throw const PreparationWireFormatException(),
    },
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

IeltsPracticeAssignment decodeIeltsPracticeAssignment(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'bank_id',
      'season',
      'mode',
      'part_1_questions',
      'part_2_questions',
      'part_3_questions',
      'turn_blueprints',
    },
    optional: const <String>{
      'part_1_set_id',
      'topic_group_id',
      'topic_title',
      'part_2_cue_card',
    },
  );
  final mode = IeltsPracticeMode.fromWireName(
    _text(object['mode'], maximumBytes: 16),
  );
  if (mode == null) {
    throw const PreparationWireFormatException();
  }
  final part1SetId = object.containsKey('part_1_set_id')
      ? _resourceId(object['part_1_set_id'])
      : null;
  final topicGroupId = object.containsKey('topic_group_id')
      ? _resourceId(object['topic_group_id'])
      : null;
  final topicTitle = object.containsKey('topic_title')
      ? _text(object['topic_title'])
      : null;
  final part2CueCard = object.containsKey('part_2_cue_card')
      ? _text(object['part_2_cue_card'])
      : null;
  final part1QuestionCount = _count(object['part_1_questions'], maximum: 24);
  final part2QuestionCount = _count(object['part_2_questions'], maximum: 1);
  final part3QuestionCount = _count(object['part_3_questions'], maximum: 6);
  final turnBlueprints = _textList(
    object['turn_blueprints'],
    minimumLength: 1,
    maximumLength: 24,
  );
  final validShape = switch (mode) {
    IeltsPracticeMode.fullMock =>
      part1SetId != null &&
          topicGroupId != null &&
          topicTitle != null &&
          part2CueCard != null &&
          part1QuestionCount == 8 &&
          part2QuestionCount == 1 &&
          part3QuestionCount >= 1 &&
          turnBlueprints.length == 9 + part3QuestionCount,
    IeltsPracticeMode.part1 =>
      part1SetId != null &&
          topicGroupId == null &&
          topicTitle == null &&
          part2CueCard == null &&
          part1QuestionCount >= 2 &&
          part2QuestionCount == 0 &&
          part3QuestionCount == 0 &&
          turnBlueprints.length == part1QuestionCount,
    IeltsPracticeMode.part2 =>
      part1SetId == null &&
          topicGroupId != null &&
          topicTitle != null &&
          part2CueCard != null &&
          part1QuestionCount == 0 &&
          part2QuestionCount == 1 &&
          part3QuestionCount >= 1 &&
          turnBlueprints.length == 1 + part3QuestionCount,
    IeltsPracticeMode.part3 =>
      part1SetId == null &&
          topicGroupId != null &&
          topicTitle != null &&
          part2CueCard == null &&
          part1QuestionCount == 0 &&
          part2QuestionCount == 0 &&
          part3QuestionCount >= 1 &&
          turnBlueprints.length == part3QuestionCount,
  };
  if (!validShape) {
    throw const PreparationWireFormatException();
  }
  return IeltsPracticeAssignment(
    bankId: _resourceId(object['bank_id']),
    season: _text(object['season']),
    mode: mode,
    part1SetId: part1SetId,
    topicGroupId: topicGroupId,
    topicTitle: topicTitle,
    part2CueCard: part2CueCard,
    part1QuestionCount: part1QuestionCount,
    part2QuestionCount: part2QuestionCount,
    part3QuestionCount: part3QuestionCount,
    turnBlueprints: turnBlueprints,
  );
}

IeltsPracticeMode? _ieltsModeForScene(SceneModel model) => switch (model) {
  SceneModel.ieltsSpeakingFullMock => IeltsPracticeMode.fullMock,
  SceneModel.ieltsSpeakingPart1 => IeltsPracticeMode.part1,
  SceneModel.ieltsSpeakingPart2 => IeltsPracticeMode.part2,
  SceneModel.ieltsSpeakingPart3 => IeltsPracticeMode.part3,
  _ => null,
};

PreparationGoalSnapshot _goalSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{'goal_id', 'title', 'version'},
  );
  return PreparationGoalSnapshot(
    id: _resourceId(object['goal_id']),
    title: _text(object['title'], maximumBytes: 512),
    version: _version(object['version']),
  );
}

PreparationSessionPolicy decodePreparationSessionPolicy(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'suggested_duration_seconds',
      'min_effective_turns',
      'max_effective_turns',
      'coverage_checkpoint_turn',
      'max_follow_ups_per_question',
      'early_completion_rule',
      'retry_allowed',
      'question_translation_allowed',
    },
  );
  final minimum = _version(object['min_effective_turns']);
  final maximum = _version(object['max_effective_turns']);
  final checkpoint = _version(object['coverage_checkpoint_turn']);
  final followUps = object['max_follow_ups_per_question'];
  final rule = _text(object['early_completion_rule'], maximumBytes: 128);
  final retryAllowed = object['retry_allowed'];
  final questionTranslationAllowed = object['question_translation_allowed'];
  if (minimum > checkpoint ||
      checkpoint > maximum ||
      followUps is! int ||
      followUps < 0 ||
      followUps > 3 ||
      retryAllowed is! bool ||
      questionTranslationAllowed is! bool ||
      !RegExp(r'^[A-Z][A-Z0-9_]*$').hasMatch(rule)) {
    throw const PreparationWireFormatException();
  }
  return PreparationSessionPolicy(
    suggestedDurationSeconds: _version(object['suggested_duration_seconds']),
    minEffectiveTurns: minimum,
    maxEffectiveTurns: maximum,
    coverageCheckpointTurn: checkpoint,
    maxFollowUpsPerQuestion: followUps,
    earlyCompletionRule: rule,
    retryAllowed: retryAllowed,
    questionTranslationAllowed: questionTranslationAllowed,
  );
}

List<PracticeObjective> decodePracticeObjectives(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 100) {
    throw const PreparationWireFormatException();
  }
  final ids = <String>{};
  return List<PracticeObjective>.unmodifiable(
    value.map((item) {
      final object = _object(
        item,
        required: const <String>{'objective_id', 'description'},
      );
      final id = _text(object['objective_id'], maximumBytes: 128);
      if (!RegExp(r'^[a-z][a-z0-9_]*$').hasMatch(id) || !ids.add(id)) {
        throw const PreparationWireFormatException();
      }
      return PracticeObjective(
        id: id,
        description: _text(object['description']),
      );
    }),
  );
}

Object? _decodeJson(String body) {
  try {
    return jsonDecode(body);
  } on FormatException {
    throw const PreparationWireFormatException();
  }
}

Map<String, Object?> _object(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?> ||
      !value.keys.toSet().containsAll(required) ||
      value.keys.any(
        (key) => !required.contains(key) && !optional.contains(key),
      )) {
    throw const PreparationWireFormatException();
  }
  return value;
}

String _resourceId(Object? value) => _text(value, maximumBytes: 128);

String _text(Object? value, {int maximumBytes = 256 * 1024}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maximumBytes) {
    throw const PreparationWireFormatException();
  }
  return value;
}

int _version(Object? value) {
  if (value is! int || value < 1) {
    throw const PreparationWireFormatException();
  }
  return value;
}

int _count(Object? value, {required int maximum}) {
  if (value is! int || value < 0 || value > maximum) {
    throw const PreparationWireFormatException();
  }
  return value;
}

List<String> _textList(
  Object? value, {
  required int minimumLength,
  required int maximumLength,
}) {
  if (value is! List<Object?> ||
      value.length < minimumLength ||
      value.length > maximumLength) {
    throw const PreparationWireFormatException();
  }
  return List<String>.unmodifiable(value.map(_text));
}

DateTime _dateTime(Object? value) {
  if (value is! String) {
    throw const PreparationWireFormatException();
  }
  final result = DateTime.tryParse(value);
  if (result == null) {
    throw const PreparationWireFormatException();
  }
  return result;
}

bool sameSceneSelection(
  SceneSelectionSnapshot left,
  SceneSelectionSnapshot right,
) =>
    sameSceneDefinition(left.scene, right.scene) &&
    _sameStrings(left.selectedRoleIds, right.selectedRoleIds) &&
    left.practiceOptionId == right.practiceOptionId;

bool samePracticeSceneSelection(
  SceneSelectionSnapshot projected,
  SceneSelectionSnapshot source,
) {
  if (!_sameSceneCore(projected.scene, source.scene) ||
      !_sameStrings(projected.selectedRoleIds, source.selectedRoleIds) ||
      projected.practiceOptionId != source.practiceOptionId) {
    return false;
  }
  final selectedRoles = source.scene.roles
      .where((role) => source.selectedRoleIds.contains(role.id))
      .toList(growable: false);
  final selectedOptions = source.scene.practiceOptions
      .where((option) => option.id == source.practiceOptionId)
      .toList(growable: false);
  return selectedRoles.length == source.selectedRoleIds.length &&
      selectedOptions.length == 1 &&
      _sameRoles(projected.scene.roles, selectedRoles) &&
      _sameOptions(projected.scene.practiceOptions, selectedOptions);
}

bool sameSceneDefinition(SceneDefinition left, SceneDefinition right) =>
    _sameSceneCore(left, right) &&
    _sameRoles(left.roles, right.roles) &&
    _sameOptions(left.practiceOptions, right.practiceOptions);

bool _sameSceneCore(SceneDefinition left, SceneDefinition right) =>
    left.id == right.id &&
    left.family == right.family &&
    left.model == right.model &&
    left.name == right.name &&
    left.version == right.version &&
    left.status == right.status &&
    left.turnPolicyRef == right.turnPolicyRef &&
    left.sessionPolicyRef == right.sessionPolicyRef &&
    left.evaluationPolicyRef == right.evaluationPolicyRef &&
    left.prompt.publicSceneBrief == right.prompt.publicSceneBrief &&
    left.prompt.practiceGoal == right.prompt.practiceGoal &&
    left.prompt.userRole == right.prompt.userRole &&
    left.prompt.aiRole == right.prompt.aiRole &&
    left.prompt.personaSummary == right.prompt.personaSummary &&
    left.prompt.suggestedDurationSeconds ==
        right.prompt.suggestedDurationSeconds &&
    _sameStrings(left.prompt.focusAreas, right.prompt.focusAreas) &&
    _sameStrings(left.prompt.turnBlueprints, right.prompt.turnBlueprints);

bool _sameRoles(List<RoleDefinition> left, List<RoleDefinition> right) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) =>
          left[index].id == right[index].id &&
          left[index].sceneId == right[index].sceneId &&
          left[index].type == right[index].type &&
          left[index].displayName == right[index].displayName &&
          left[index].responsibilities == right[index].responsibilities &&
          left[index].style == right[index].style &&
          _sameRoleObjectives(
            left[index].practiceObjectives,
            right[index].practiceObjectives,
          ) &&
          left[index].voiceConfigRef == right[index].voiceConfigRef,
    ).every((same) => same);

bool _sameRoleObjectives(
  List<RolePracticeObjective> left,
  List<RolePracticeObjective> right,
) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) =>
          left[index].objectiveId == right[index].objectiveId &&
          left[index].description == right[index].description,
    ).every((same) => same);

bool _sameOptions(List<PracticeOption> left, List<PracticeOption> right) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) =>
          left[index].id == right[index].id &&
          left[index].sceneId == right[index].sceneId &&
          left[index].type == right[index].type &&
          left[index].displayName == right[index].displayName &&
          left[index].roleId == right[index].roleId,
    ).every((same) => same);

bool _sameStrings(List<String> left, List<String> right) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) => left[index] == right[index],
    ).every((same) => same);
