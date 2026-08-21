import 'dart:convert';

import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment_codec.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';

final class PreparationWireFormatException implements Exception {
  const PreparationWireFormatException();
}

InterviewPreparation decodeInterviewPreparationBody(String body) =>
    decodeInterviewPreparation(_decodeJson(body));

InterviewPreparation decodeInterviewPreparation(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'interview_preparation_id',
      'user_id',
      'input',
      'candidate',
      'status',
      'version',
      'created_at',
      'updated_at',
    },
    optional: const <String>{'resume_content'},
  );
  final createdAt = _dateTime(object['created_at']);
  final updatedAt = _dateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const PreparationWireFormatException();
  }
  final input = decodeInterviewPreparationInput(object['input']);
  final candidate = decodeInterviewPreparationCandidate(object['candidate']);
  if (candidate.source != input.source) {
    throw const PreparationWireFormatException();
  }
  return InterviewPreparation(
    id: _aggregateId(object['interview_preparation_id']),
    userId: _aggregateId(object['user_id']),
    input: input,
    candidate: candidate,
    resumeUsed: _validateOptionalResumeContent(object),
    status: switch (_text(object['status'], maximumBytes: 16)) {
      'draft' => InterviewPreparationStatus.draft,
      'confirmed' => InterviewPreparationStatus.confirmed,
      'discarded' => InterviewPreparationStatus.discarded,
      _ => throw const PreparationWireFormatException(),
    },
    version: _positiveInteger(object['version']),
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

InterviewPreparationInput decodeInterviewPreparationInput(Object? value) {
  final object = _object(
    value,
    required: const <String>{'source'},
    optional: const <String>{
      'job_title',
      'job_description',
      'company',
      'seniority',
      'candidate_background',
      'practice_focus',
    },
  );
  final source = switch (_text(object['source'], maximumBytes: 32)) {
    'job_description' => InterviewPreparationSource.jobDescription,
    'quick_start' => InterviewPreparationSource.quickStart,
    _ => throw const PreparationWireFormatException(),
  };
  final input = InterviewPreparationInput(
    source: source,
    jobTitle: _optionalText(object, 'job_title', 512),
    jobDescription: _optionalText(object, 'job_description', 64 * 1024),
    company: _optionalText(object, 'company', 512),
    seniority: _optionalText(object, 'seniority', 256),
    candidateBackground: _optionalText(
      object,
      'candidate_background',
      16 * 1024,
    ),
    practiceFocus: _optionalText(object, 'practice_focus', 8 * 1024),
  );
  if ((source == InterviewPreparationSource.jobDescription &&
          input.jobDescription == null) ||
      (source == InterviewPreparationSource.quickStart &&
          (input.jobTitle == null || input.jobDescription != null))) {
    throw const PreparationWireFormatException();
  }
  return input;
}

InterviewPreparationCandidate decodeInterviewPreparationCandidate(
  Object? value,
) {
  final object = _object(
    value,
    required: const <String>{
      'source',
      'general_advice_only',
      'job_title',
      'seniority',
      'responsibilities',
      'core_skills',
      'communication_focus',
      'practice_goals',
      'scope_notice',
      'catalog_recommendation',
    },
    optional: const <String>{'company'},
  );
  final source = switch (_text(object['source'], maximumBytes: 32)) {
    'job_description' => InterviewPreparationSource.jobDescription,
    'quick_start' => InterviewPreparationSource.quickStart,
    _ => throw const PreparationWireFormatException(),
  };
  final generalAdviceOnly = object['general_advice_only'];
  if (generalAdviceOnly is! bool ||
      generalAdviceOnly != (source == InterviewPreparationSource.quickStart)) {
    throw const PreparationWireFormatException();
  }
  final recommendation = _object(
    object['catalog_recommendation'],
    required: const <String>{
      'scene_id',
      'scene_version',
      'selected_role_ids',
      'practice_option_id',
    },
  );
  return InterviewPreparationCandidate(
    source: source,
    generalAdviceOnly: generalAdviceOnly,
    jobTitle: _text(object['job_title'], maximumBytes: 512),
    company: _optionalText(object, 'company', 512) ?? '',
    seniority: _text(object['seniority'], maximumBytes: 256),
    responsibilities: _nonEmptyTextList(object['responsibilities']),
    coreSkills: _nonEmptyTextList(object['core_skills']),
    communicationFocus: _nonEmptyTextList(object['communication_focus']),
    practiceGoals: _nonEmptyTextList(object['practice_goals']),
    scopeNotice: _text(object['scope_notice'], maximumBytes: 2048),
    catalogRecommendation: InterviewCatalogRecommendation(
      sceneId: _resourceId(recommendation['scene_id']),
      sceneVersion: _positiveInteger(recommendation['scene_version']),
      selectedRoleIds: _resourceIdList(recommendation['selected_role_ids']),
      practiceOptionId: _resourceId(recommendation['practice_option_id']),
    ),
  );
}

Map<String, Object?> encodeInterviewPreparationInput(
  InterviewPreparationInput input,
) => <String, Object?>{
  'source': input.source.wireValue,
  if (input.jobTitle != null) 'job_title': input.jobTitle,
  if (input.jobDescription != null) 'job_description': input.jobDescription,
  if (input.company != null) 'company': input.company,
  if (input.seniority != null) 'seniority': input.seniority,
  if (input.candidateBackground != null)
    'candidate_background': input.candidateBackground,
  if (input.practiceFocus != null) 'practice_focus': input.practiceFocus,
};

Map<String, Object?> encodeInterviewPreparationCandidate(
  InterviewPreparationCandidate candidate,
) => <String, Object?>{
  'source': candidate.source.wireValue,
  'general_advice_only': candidate.generalAdviceOnly,
  'job_title': candidate.jobTitle,
  if (candidate.company.isNotEmpty) 'company': candidate.company,
  'seniority': candidate.seniority,
  'responsibilities': candidate.responsibilities,
  'core_skills': candidate.coreSkills,
  'communication_focus': candidate.communicationFocus,
  'practice_goals': candidate.practiceGoals,
  'scope_notice': candidate.scopeNotice,
  'catalog_recommendation': <String, Object?>{
    'scene_id': candidate.catalogRecommendation.sceneId,
    'scene_version': candidate.catalogRecommendation.sceneVersion,
    'selected_role_ids': candidate.catalogRecommendation.selectedRoleIds,
    'practice_option_id': candidate.catalogRecommendation.practiceOptionId,
  },
};

PracticePlan decodePracticePlanBody(String body) =>
    decodePracticePlan(_decodeJson(body));

PracticePlan decodePracticePlan(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'practice_plan_id',
      'user_id',
      'preparation_snapshot',
      'scene_selection',
      'session_policy',
      'practice_objectives',
      'version',
      'practice_plan_status',
      'created_at',
      'updated_at',
    },
    optional: const <String>{'source_thread_id', 'ielts_assignment'},
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
  final policy = decodePreparationSessionPolicy(object['session_policy']);
  final assignment = object.containsKey('ielts_assignment')
      ? _ieltsAssignment(object['ielts_assignment'])
      : null;
  final expectedIelts =
      sceneSelection.scene.experience == PracticeExperience.ieltsSpeaking;
  if (expectedIelts != (assignment != null) ||
      (assignment != null &&
          (assignment.mode !=
                  sceneSelection.scene.practiceOptions
                      .firstWhere(
                        (item) => item.id == sceneSelection.practiceOptionId,
                      )
                      .mode ||
              assignment.turnBlueprints.length != policy.maxEffectiveTurns))) {
    throw const PreparationWireFormatException();
  }
  return PracticePlan(
    id: _aggregateId(object['practice_plan_id']),
    userId: _aggregateId(object['user_id']),
    sourceThreadId: object.containsKey('source_thread_id')
        ? _aggregateId(object['source_thread_id'])
        : null,
    preparationSnapshot: _planPreparationSnapshot(
      object['preparation_snapshot'],
    ),
    sceneSelection: sceneSelection,
    sessionPolicy: policy,
    practiceObjectives: decodePracticeObjectives(object['practice_objectives']),
    ieltsAssignment: assignment,
    version: _positiveInteger(object['version']),
    status: switch (_text(object['practice_plan_status'], maximumBytes: 16)) {
      'draft' => PracticePlanStatus.draft,
      'ready' => PracticePlanStatus.ready,
      _ => throw const PreparationWireFormatException(),
    },
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

List<PracticePlanSummary> decodePracticePlanListBody(String body) {
  final root = _object(
    _decodeJson(body),
    required: const <String>{'practice_plans'},
  );
  final values = root['practice_plans'];
  if (values is! List<Object?>) throw const PreparationWireFormatException();
  return List<PracticePlanSummary>.unmodifiable(
    values.map((value) {
      final object = _object(
        value,
        required: const <String>{
          'practice_plan_id',
          'version',
          'practice_plan_status',
          'practice_experience',
          'scene_name',
          'practice_scope',
          'job_title',
          'practice_objectives',
          'resume_used',
          'suggested_duration_seconds',
          'min_effective_turns',
          'max_effective_turns',
          'created_at',
          'updated_at',
        },
      );
      final experience = PracticeExperience.fromWireValue(
        _text(object['practice_experience'], maximumBytes: 32),
      );
      final resumeUsed = object['resume_used'];
      final maxTurns = _nonNegativeInteger(object['max_effective_turns']);
      if (experience == null || resumeUsed is! bool) {
        throw const PreparationWireFormatException();
      }
      return PracticePlanSummary(
        id: _aggregateId(object['practice_plan_id']),
        version: _positiveInteger(object['version']),
        status: switch (_text(
          object['practice_plan_status'],
          maximumBytes: 16,
        )) {
          'draft' => PracticePlanStatus.draft,
          'ready' => PracticePlanStatus.ready,
          _ => throw const PreparationWireFormatException(),
        },
        experience: experience,
        sceneName: _text(object['scene_name']),
        practiceScope: _text(object['practice_scope']),
        jobTitle: _allowEmptyText(object['job_title']),
        practiceObjectives: _nonEmptyTextList(object['practice_objectives']),
        resumeUsed: resumeUsed,
        suggestedDurationSeconds: _positiveInteger(
          object['suggested_duration_seconds'],
        ),
        minEffectiveTurns: _positiveInteger(object['min_effective_turns']),
        maxEffectiveTurns: maxTurns,
        createdAt: _dateTime(object['created_at']),
        updatedAt: _dateTime(object['updated_at']),
      );
    }),
  );
}

PreparationPracticeBootstrap decodePreparationPracticeBootstrapBody(
  String body, {
  required PracticePlan expectedPlan,
}) {
  final root = _object(
    _decodeJson(body),
    required: const <String>{'practice_session', 'snapshot'},
  );
  final session = _object(
    root['practice_session'],
    required: const <String>{
      'practice_session_id',
      'practice_plan_id',
      'plan_version',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'evaluation_policy_ref',
      'practice_session_status',
      'session_version',
      'created_at',
    },
    optional: const <String>{'started_at', 'ended_at', 'end_reason'},
  );
  final snapshot = _object(
    root['snapshot'],
    required: const <String>{
      'practice_session_id',
      'plan_version',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'scene_selection',
      'preparation_snapshot',
      'participants',
      'session_policy',
      'practice_objectives',
    },
    optional: const <String>{'ielts_assignment'},
  );
  final id = _aggregateId(session['practice_session_id']);
  final planID = _aggregateId(session['practice_plan_id']);
  final planVersion = _positiveInteger(session['plan_version']);
  final experience = PracticeExperience.fromWireValue(
    _text(session['practice_experience'], maximumBytes: 32),
  );
  final category = SceneCategory.fromWireValue(
    _text(session['scene_category'], maximumBytes: 64),
  );
  final mode = PracticeMode.fromWireValue(
    _text(session['practice_mode'], maximumBytes: 32),
  );
  final snapshotPolicy = decodePreparationSessionPolicy(
    snapshot['session_policy'],
  );
  if (experience == null ||
      category == null ||
      mode == null ||
      planID != expectedPlan.id ||
      planVersion != expectedPlan.version ||
      id != _aggregateId(snapshot['practice_session_id']) ||
      planVersion != _positiveInteger(snapshot['plan_version']) ||
      experience.wireValue != snapshot['practice_experience'] ||
      category.wireValue != snapshot['scene_category'] ||
      mode.wireValue != snapshot['practice_mode'] ||
      !_samePracticeExecutionSelection(
        snapshot['scene_selection'],
        expectedPlan.sceneSelection,
      ) ||
      snapshotPolicy.maxEffectiveTurns !=
          expectedPlan.sessionPolicy.maxEffectiveTurns) {
    throw const PreparationWireFormatException();
  }
  return PreparationPracticeBootstrap(
    session: PreparationPracticeSession(
      id: id,
      planId: planID,
      planVersion: planVersion,
      practiceExperience: experience,
      sceneCategory: category,
      practiceMode: mode,
      status: _text(session['practice_session_status'], maximumBytes: 32),
      version: _positiveInteger(session['session_version']),
      createdAt: _dateTime(session['created_at']),
    ),
    maxEffectiveTurns: snapshotPolicy.maxEffectiveTurns,
  );
}

PlanPreparationSnapshot _planPreparationSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{'background_summary'},
    optional: const <String>{'interview'},
  );
  return PlanPreparationSnapshot(
    backgroundSummary: _allowEmptyText(object['background_summary'], 64 * 1024),
    interview: object.containsKey('interview')
        ? _interviewPreparationSnapshot(object['interview'])
        : null,
  );
}

InterviewPreparationSnapshot _interviewPreparationSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'interview_preparation_id',
      'version',
      'input',
      'candidate',
    },
    optional: const <String>{'resume_content'},
  );
  final input = decodeInterviewPreparationInput(object['input']);
  final candidate = decodeInterviewPreparationCandidate(object['candidate']);
  if (input.source != candidate.source) {
    throw const PreparationWireFormatException();
  }
  return InterviewPreparationSnapshot(
    id: _aggregateId(object['interview_preparation_id']),
    version: _positiveInteger(object['version']),
    input: input,
    candidate: candidate,
    resumeUsed: _validateOptionalResumeContent(object),
  );
}

bool _validateOptionalResumeContent(Map<String, Object?> object) {
  if (!object.containsKey('resume_content')) {
    return false;
  }
  _validateResumeMaterial(object['resume_content']);
  return true;
}

void _validateResumeMaterial(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'target_position',
      'professional_summary',
      'work_experiences',
      'project_experiences',
      'education_experiences',
      'skills',
      'awards',
    },
  );
  _allowEmptyText(object['target_position']);
  _allowEmptyText(object['professional_summary']);
  for (final item in _list(object['work_experiences'])) {
    final experience = _object(
      item,
      required: const <String>{'company', 'position', 'duties', 'achievements'},
      optional: const <String>{'start_date', 'end_date'},
    );
    _allowEmptyText(experience['company']);
    _allowEmptyText(experience['position']);
    _optionalAllowEmptyText(experience, 'start_date');
    _optionalAllowEmptyText(experience, 'end_date');
    _stringList(experience['duties']);
    _stringList(experience['achievements']);
  }
  for (final item in _list(object['project_experiences'])) {
    final experience = _object(
      item,
      required: const <String>{
        'project_name',
        'role',
        'description',
        'technologies',
        'duties',
        'achievements',
      },
    );
    _allowEmptyText(experience['project_name']);
    _allowEmptyText(experience['role']);
    _allowEmptyText(experience['description']);
    _stringList(experience['technologies']);
    _stringList(experience['duties']);
    _stringList(experience['achievements']);
  }
  for (final item in _list(object['education_experiences'])) {
    final experience = _object(
      item,
      required: const <String>{'school', 'major', 'degree'},
      optional: const <String>{'gpa', 'start_date', 'end_date'},
    );
    _allowEmptyText(experience['school']);
    _allowEmptyText(experience['major']);
    _allowEmptyText(experience['degree']);
    _optionalAllowEmptyText(experience, 'gpa');
    _optionalAllowEmptyText(experience, 'start_date');
    _optionalAllowEmptyText(experience, 'end_date');
  }
  _stringList(object['skills']);
  _stringList(object['awards']);
}

List<Object?> _list(Object? value) {
  if (value is! List<Object?>) {
    throw const PreparationWireFormatException();
  }
  return value;
}

void _stringList(Object? value) {
  for (final item in _list(value)) {
    _allowEmptyText(item);
  }
}

void _optionalAllowEmptyText(Map<String, Object?> object, String key) {
  if (object.containsKey(key)) {
    _allowEmptyText(object[key]);
  }
}

PreparationSessionPolicy decodePreparationSessionPolicy(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'completion_mode',
      'suggested_duration_seconds',
      'min_effective_turns',
      'max_effective_turns',
      'coverage_checkpoint_turn',
      'max_follow_ups_per_question',
      'early_completion_rule',
      'retry_allowed',
      'question_translation_allowed',
      'question_tips_allowed',
      'speech_feedback_allowed',
    },
  );
  final mode = PreparationCompletionMode.fromWireValue(
    _text(object['completion_mode'], maximumBytes: 32),
  );
  final minimum = _positiveInteger(object['min_effective_turns']);
  final maximum = _nonNegativeInteger(object['max_effective_turns']);
  final checkpoint = _positiveInteger(object['coverage_checkpoint_turn']);
  final followUps = _nonNegativeInteger(object['max_follow_ups_per_question']);
  final flags = <Object?>[
    object['retry_allowed'],
    object['question_translation_allowed'],
    object['question_tips_allowed'],
    object['speech_feedback_allowed'],
  ];
  if (mode == null ||
      (mode == PreparationCompletionMode.turnLimited &&
          (maximum < 1 || minimum > checkpoint || checkpoint > maximum)) ||
      (mode == PreparationCompletionMode.userControlled &&
          (maximum != 0 || checkpoint != 1)) ||
      followUps > 3 ||
      flags.any((value) => value is! bool)) {
    throw const PreparationWireFormatException();
  }
  return PreparationSessionPolicy(
    completionMode: mode,
    suggestedDurationSeconds: _positiveInteger(
      object['suggested_duration_seconds'],
    ),
    minEffectiveTurns: minimum,
    maxEffectiveTurns: maximum,
    coverageCheckpointTurn: checkpoint,
    maxFollowUpsPerQuestion: followUps,
    earlyCompletionRule: _text(
      object['early_completion_rule'],
      maximumBytes: 128,
    ),
    retryAllowed: object['retry_allowed']! as bool,
    questionTranslationAllowed: object['question_translation_allowed']! as bool,
    questionTipsAllowed: object['question_tips_allowed']! as bool,
    speechFeedbackAllowed: object['speech_feedback_allowed']! as bool,
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
      final id = _resourceId(object['objective_id']);
      if (!ids.add(id)) throw const PreparationWireFormatException();
      return PracticeObjective(
        id: id,
        description: _text(object['description']),
      );
    }),
  );
}

IeltsPracticeAssignment _ieltsAssignment(Object? value) {
  try {
    return decodeIeltsAssignment(value);
  } on IeltsAssignmentWireFormatException {
    throw const PreparationWireFormatException();
  }
}

bool sameSceneSelection(
  SceneSelectionSnapshot left,
  SceneSelectionSnapshot right,
) =>
    left.scene.id == right.scene.id &&
    left.scene.version == right.scene.version &&
    left.practiceOptionId == right.practiceOptionId &&
    _sameStrings(left.selectedRoleIds, right.selectedRoleIds);

bool _samePracticeExecutionSelection(
  Object? value,
  SceneSelectionSnapshot expected,
) {
  try {
    final selection = _object(
      value,
      required: const <String>{
        'scene',
        'selected_role_ids',
        'practice_option_id',
      },
    );
    final scene = decodeSceneDefinition(selection['scene']);
    final selectedRoleIds = _resourceIdList(selection['selected_role_ids']);
    final practiceOptionId = _resourceId(selection['practice_option_id']);
    return scene.id == expected.scene.id &&
        scene.version == expected.scene.version &&
        practiceOptionId == expected.practiceOptionId &&
        _sameStrings(selectedRoleIds, expected.selectedRoleIds);
  } on SceneWireFormatException {
    throw const PreparationWireFormatException();
  }
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

String _aggregateId(Object? value) {
  final id = _text(value, maximumBytes: 36);
  if (!_uuidV4.hasMatch(id)) throw const PreparationWireFormatException();
  return id;
}

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

String _allowEmptyText(Object? value, [int maximumBytes = 256 * 1024]) {
  if (value is! String ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maximumBytes) {
    throw const PreparationWireFormatException();
  }
  return value;
}

String? _optionalText(
  Map<String, Object?> object,
  String key,
  int maximumBytes,
) => object.containsKey(key)
    ? _text(object[key], maximumBytes: maximumBytes)
    : null;

int _positiveInteger(Object? value) {
  if (value is! int || value < 1) throw const PreparationWireFormatException();
  return value;
}

int _nonNegativeInteger(Object? value) {
  if (value is! int || value < 0) throw const PreparationWireFormatException();
  return value;
}

DateTime _dateTime(Object? value) {
  if (value is! String) throw const PreparationWireFormatException();
  final result = DateTime.tryParse(value);
  if (result == null) throw const PreparationWireFormatException();
  return result;
}

List<String> _resourceIdList(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 8) {
    throw const PreparationWireFormatException();
  }
  final result = value.map(_resourceId).toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const PreparationWireFormatException();
  }
  return List<String>.unmodifiable(result);
}

List<String> _nonEmptyTextList(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 20) {
    throw const PreparationWireFormatException();
  }
  final result = value
      .map((item) => _text(item, maximumBytes: 2048))
      .toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const PreparationWireFormatException();
  }
  return List<String>.unmodifiable(result);
}

bool _sameStrings(List<String> left, List<String> right) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) => left[index] == right[index],
    ).every((same) => same);

final RegExp _uuidV4 = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);
