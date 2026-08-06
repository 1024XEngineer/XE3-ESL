import 'dart:convert';

import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment_codec.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
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
      'preparation_context',
      'resume_id',
      'resume_revision',
      'job_description_ref',
      'job_target_id',
      'job_target_confirmation_version',
    },
  );
  final hasJobTarget = object.containsKey('job_target_id');
  final hasResume = object.containsKey('resume_id');
  if (hasResume != object.containsKey('resume_revision')) {
    throw const PreparationWireFormatException();
  }
  if (hasJobTarget != object.containsKey('job_target_confirmation_version')) {
    throw const PreparationWireFormatException();
  }
  return PreparationProfile(
    id: _resourceId(object['preparation_profile_id']),
    userId: _resourceId(object['user_id']),
    resumeId: hasResume ? _resourceId(object['resume_id']) : null,
    resumeRevision: hasResume ? _version(object['resume_revision']) : null,
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
    context: object.containsKey('preparation_context')
        ? _preparationContext(object['preparation_context'])
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
      'preparation_context',
      'preparation_kind',
      'scenario_context',
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
  final hasCanonicalContext = object.containsKey('preparation_context');
  final hasProjectedKind = object.containsKey('preparation_kind');
  final hasProjectedScenario = object.containsKey('scenario_context');
  if (hasCanonicalContext && (hasProjectedKind || hasProjectedScenario) ||
      !hasProjectedKind && hasProjectedScenario) {
    throw const PreparationWireFormatException();
  }
  PreparationContext? context;
  if (hasCanonicalContext) {
    context = _preparationContext(object['preparation_context']);
  } else if (hasProjectedKind) {
    final kind = _text(object['preparation_kind'], maximumBytes: 16);
    switch (kind) {
      case 'scenario':
        if (!hasProjectedScenario) {
          throw const PreparationWireFormatException();
        }
        context = _scenarioPreparationPayload(object['scenario_context']);
      case 'interview':
        if (hasProjectedScenario) {
          throw const PreparationWireFormatException();
        }
      default:
        throw const PreparationWireFormatException();
    }
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
    context: context,
    backgroundSnapshot: _text(
      object['background_snapshot'],
      maximumBytes: 64 * 1024,
    ),
    createdAt: _dateTime(object['created_at']),
  );
}

PreparationContext _preparationContext(Object? value) {
  if (value is! Map<String, Object?> || value['kind'] is! String) {
    throw const PreparationWireFormatException();
  }
  return switch (value['kind']) {
    'scenario' => _scenarioPreparationContext(value),
    'interview' => _interviewPreparationContext(value),
    _ => throw const PreparationWireFormatException(),
  };
}

ScenarioPreparationContext _scenarioPreparationContext(
  Map<String, Object?> value,
) {
  final context = _object(value, required: const <String>{'kind', 'scenario'});
  return _scenarioPreparationPayload(context['scenario']);
}

ScenarioPreparationContext _scenarioPreparationPayload(Object? value) {
  final scenario = _object(
    value,
    required: const <String>{
      'situation',
      'user_role',
      'counterpart_role',
      'goal',
      'counterpart_persona',
    },
  );
  return ScenarioPreparationContext(
    situation: _text(scenario['situation'], maximumBytes: 16 * 1024),
    userRole: _text(scenario['user_role'], maximumBytes: 16 * 1024),
    counterpartRole: _text(
      scenario['counterpart_role'],
      maximumBytes: 16 * 1024,
    ),
    goal: _text(scenario['goal'], maximumBytes: 16 * 1024),
    counterpartPersona: _text(
      scenario['counterpart_persona'],
      maximumBytes: 16 * 1024,
    ),
  );
}

InterviewPreparationContext _interviewPreparationContext(
  Map<String, Object?> value,
) {
  final context = _object(value, required: const <String>{'kind', 'interview'});
  final interview = _object(
    context['interview'],
    required: const <String>{'job_target'},
    optional: const <String>{'resume'},
  );
  final target = _object(
    interview['job_target'],
    required: const <String>{'job_target_id', 'confirmation_version'},
  );
  final resume = interview.containsKey('resume')
      ? _object(
          interview['resume'],
          required: const <String>{'resume_id', 'revision'},
        )
      : null;
  return InterviewPreparationContext(
    resume: resume == null
        ? null
        : PreparationResumeReference(
            resumeId: _resourceId(resume['resume_id']),
            revision: _version(resume['revision']),
          ),
    jobTarget: PreparationJobTargetReference(
      jobTargetId: _resourceId(target['job_target_id']),
      confirmationVersion: _version(target['confirmation_version']),
    ),
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
  final selectedOption = sceneSelection.scene.practiceOptions
      .where((option) => option.id == sceneSelection.practiceOptionId)
      .firstOrNull;
  final expectedIeltsMode =
      selectedOption == null ||
          sceneSelection.scene.experience != PracticeExperience.ieltsSpeaking
      ? null
      : selectedOption.mode;
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
  try {
    return decodeIeltsAssignment(value);
  } on IeltsAssignmentWireFormatException {
    throw const PreparationWireFormatException();
  }
}

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
      'question_tips_allowed',
      'avatar_allowed',
      'speech_feedback_allowed',
    },
  );
  final minimum = _version(object['min_effective_turns']);
  final maximum = _version(object['max_effective_turns']);
  final checkpoint = _version(object['coverage_checkpoint_turn']);
  final followUps = object['max_follow_ups_per_question'];
  final rule = _text(object['early_completion_rule'], maximumBytes: 128);
  final retryAllowed = object['retry_allowed'];
  final questionTranslationAllowed = object['question_translation_allowed'];
  final questionTipsAllowed = object['question_tips_allowed'];
  final avatarAllowed = object['avatar_allowed'];
  final speechFeedbackAllowed = object['speech_feedback_allowed'];
  if (minimum > checkpoint ||
      checkpoint > maximum ||
      followUps is! int ||
      followUps < 0 ||
      followUps > 3 ||
      retryAllowed is! bool ||
      questionTranslationAllowed is! bool ||
      questionTipsAllowed is! bool ||
      avatarAllowed is! bool ||
      speechFeedbackAllowed is! bool ||
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
    questionTipsAllowed: questionTipsAllowed,
    avatarAllowed: avatarAllowed,
    speechFeedbackAllowed: speechFeedbackAllowed,
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
    left.experience == right.experience &&
    left.category == right.category &&
    left.name == right.name &&
    left.version == right.version &&
    left.status == right.status &&
    left.prompt.publicSceneBrief == right.prompt.publicSceneBrief &&
    left.prompt.practiceGoal == right.prompt.practiceGoal &&
    left.prompt.userRole == right.prompt.userRole &&
    left.prompt.aiRole == right.prompt.aiRole &&
    left.prompt.personaSummary == right.prompt.personaSummary &&
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
          left[index].mode == right[index].mode &&
          left[index].displayName == right[index].displayName &&
          left[index].roleId == right[index].roleId &&
          left[index].suggestedDurationSeconds ==
              right[index].suggestedDurationSeconds &&
          left[index].turnPolicyRef == right[index].turnPolicyRef &&
          left[index].sessionPolicyRef == right[index].sessionPolicyRef &&
          left[index].evaluationPolicyRef == right[index].evaluationPolicyRef,
    ).every((same) => same);

bool _sameStrings(List<String> left, List<String> right) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) => left[index] == right[index],
    ).every((same) => same);
