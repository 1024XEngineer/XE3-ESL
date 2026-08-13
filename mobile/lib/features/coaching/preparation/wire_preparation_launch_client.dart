import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_wire_codec.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

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

final class WirePreparationLaunchClient implements PreparationLaunchClient {
  factory WirePreparationLaunchClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 20),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WirePreparationLaunchClient._(
      baseUri,
      SessionAuthenticatedHttpTransport(
        transport: transport ?? IoIdentityHttpTransport(),
        credentialProvider: credentialProvider,
        invalidateSession: invalidateSession,
        trustedBaseUri: baseUri,
      ),
      requestTimeout,
    );
  }

  WirePreparationLaunchClient._(
    this._baseUri,
    this._transport,
    this._requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  static const _maximumBodyBytes = 1024 * 1024;

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  int _accountGeneration = 0;

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    _requireProfileInput(input);
    final response = await _post(
      path: '/v1/preparation-profiles',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'kind': ?input.kind?.wireValue,
        'scenario': ?_scenarioInputJson(input.scenario),
        'background_summary': input.backgroundSummary,
        'resume_id': ?input.resumeId,
        'resume_revision': ?input.resumeRevision,
        'job_description_ref': ?input.jobDescriptionRef,
        'job_target_id': ?input.jobTargetId,
        'job_target_confirmation_version': ?input.jobTargetConfirmationVersion,
      },
      stage: PreparationLaunchStage.profile,
    );
    return _decodeCreated(
      stage: PreparationLaunchStage.profile,
      decode: () {
        final profile = decodePreparationProfileBody(response.body);
        if (profile.backgroundSummary != input.backgroundSummary ||
            profile.context != input.scenario ||
            profile.resumeId != input.resumeId ||
            profile.resumeRevision != input.resumeRevision ||
            profile.jobDescriptionRef != input.jobDescriptionRef ||
            profile.jobTargetId != input.jobTargetId ||
            profile.jobTargetConfirmationVersion !=
                input.jobTargetConfirmationVersion) {
          throw _invalidResponse();
        }
        return profile;
      },
    );
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    _requireResourceId(profileId);
    if (sourceVersion < 1) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: PreparationLaunchStage.snapshot,
      );
    }
    final response = await _post(
      path:
          '/v1/preparation-profiles/${Uri.encodeComponent(profileId)}'
          '/snapshots',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'source_version': sourceVersion},
      stage: PreparationLaunchStage.snapshot,
    );
    return _decodeCreated(
      stage: PreparationLaunchStage.snapshot,
      decode: () {
        final snapshot = decodePreparationSnapshotBody(response.body);
        if (snapshot.sourceProfileId != profileId ||
            snapshot.sourceVersion != sourceVersion) {
          throw _invalidResponse();
        }
        return snapshot;
      },
    );
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    _requirePlanInput(input);
    _requireResourceId(input.preparationSnapshotId);
    final response = await _post(
      path: '/v1/practice-plans',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'source_thread_id': ?input.sourceThreadId,
        'goal_id': ?input.goalId,
        'preparation_snapshot_id': input.preparationSnapshotId,
        'scene_id': input.sceneId,
        'scene_version': input.sceneVersion,
        'selected_role_ids': input.selectedRoleIds,
        'practice_option_id': input.practiceOptionId,
        'max_effective_turns': ?input.maxEffectiveTurns,
        if (input.ieltsSelection case final selection?)
          'ielts_selection': selection.toJson(),
      },
      stage: PreparationLaunchStage.plan,
    );
    return _decodeCreated(
      stage: PreparationLaunchStage.plan,
      decode: () => _plan(_decode(response.body), expected: input),
    );
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    _requireResourceId(plan.id);
    if (input.expectedPlanRevision != plan.revision ||
        !input.userConfirmed ||
        plan.status != PracticePlanStatus.ready) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: PreparationLaunchStage.session,
      );
    }
    final response = await _post(
      path:
          '/v1/practice-plans/${Uri.encodeComponent(plan.id)}'
          '/practice-sessions',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_revision': input.expectedPlanRevision,
        'user_confirmed': input.userConfirmed,
      },
      stage: PreparationLaunchStage.session,
    );
    return _decodeCreated(
      stage: PreparationLaunchStage.session,
      decode: () => _bootstrap(_decode(response.body), expectedPlan: plan),
    );
  }

  Future<PracticePlan> getPlan(String planId) async {
    _requireResourceId(planId);
    final response = await _get(
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}',
      stage: PreparationLaunchStage.plan,
    );
    return _decodeSucceeded(
      stage: PreparationLaunchStage.plan,
      statusCode: HttpStatus.ok,
      decode: () {
        final plan = decodePracticePlanBody(response.body);
        if (plan.id != planId) {
          throw _invalidResponse();
        }
        return plan;
      },
    );
  }

  Future<IdentityHttpResponse> _post({
    required String path,
    required String idempotencyKey,
    required Map<String, Object?> body,
    required PreparationLaunchStage stage,
  }) {
    _requireIdempotencyKey(idempotencyKey);
    return _request(
      method: 'POST',
      path: path,
      stage: stage,
      expectedStatus: HttpStatus.created,
      idempotencyKey: idempotencyKey,
      body: body,
    );
  }

  Future<IdentityHttpResponse> _get({
    required String path,
    required PreparationLaunchStage stage,
  }) => _request(
    method: 'GET',
    path: path,
    stage: stage,
    expectedStatus: HttpStatus.ok,
  );

  Future<IdentityHttpResponse> _request({
    required String method,
    required String path,
    required PreparationLaunchStage stage,
    required int expectedStatus,
    String? idempotencyKey,
    Map<String, Object?>? body,
  }) async {
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    final headers = <String, String>{
      HttpHeaders.acceptHeader: ContentType.json.mimeType,
    };
    if (body != null && idempotencyKey != null) {
      headers[HttpHeaders.contentTypeHeader] = ContentType.json.mimeType;
      headers['Idempotency-Key'] = idempotencyKey;
    }
    try {
      response = await _transport
          .send(
            method: method,
            uri: uri,
            headers: headers,
            body: body == null ? null : jsonEncode(body),
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.superseded,
        stage: stage,
      );
    } on StateError {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.authenticationRequired,
        stage: stage,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.network,
        stage: stage,
        retryable: true,
      );
    } on SocketException {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.network,
        stage: stage,
        retryable: true,
      );
    } on HttpException {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.network,
        stage: stage,
        retryable: true,
      );
    } on IOException {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.network,
        stage: stage,
        retryable: true,
      );
    }
    if (generation != _accountGeneration) {
      throw PreparationLaunchException(
        kind: PreparationLaunchFailureKind.superseded,
        stage: stage,
      );
    }
    if (response.statusCode == expectedStatus) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidResponse,
          stage: stage,
          statusCode: expectedStatus,
          retryable: true,
        );
      }
      return response;
    }
    final errorCode = _errorCode(response.body);
    final exception = switch (response.statusCode) {
      HttpStatus.badRequest => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      HttpStatus.unauthorized => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.authenticationRequired,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      HttpStatus.notFound => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.notFound,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      HttpStatus.conflict => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.conflict,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      _ when response.statusCode >= 500 => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.server,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
        retryable: true,
      ),
      _ => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidResponse,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
    };
    throw exception;
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

T _decodeCreated<T>({
  required PreparationLaunchStage stage,
  required T Function() decode,
}) {
  return _decodeSucceeded(
    stage: stage,
    statusCode: HttpStatus.created,
    decode: decode,
  );
}

T _decodeSucceeded<T>({
  required PreparationLaunchStage stage,
  required int statusCode,
  required T Function() decode,
}) {
  try {
    return decode();
  } on PreparationLaunchException catch (error) {
    if (error.kind != PreparationLaunchFailureKind.invalidResponse) {
      rethrow;
    }
    throw PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidResponse,
      stage: stage,
      statusCode: statusCode,
      retryable: true,
    );
  } on PreparationWireFormatException {
    throw PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidResponse,
      stage: stage,
      statusCode: statusCode,
      retryable: true,
    );
  }
}

PracticePlan _plan(
  Object? value, {
  required CreatePreparationPlanInput expected,
}) {
  final plan = decodePracticePlan(value);
  final selection = plan.sceneSelection;
  final selectedOption = selection.scene.practiceOptions
      .where((option) => option.id == selection.practiceOptionId)
      .firstOrNull;
  final expectedIeltsMode =
      selection.scene.experience == PracticeExperience.ieltsSpeaking
      ? selectedOption?.mode
      : null;
  if (plan.sourceThreadId != expected.sourceThreadId ||
      plan.goalSnapshot?.id != expected.goalId ||
      plan.preparationSnapshot.id != expected.preparationSnapshotId ||
      selection.scene.id != expected.sceneId ||
      selection.scene.version != expected.sceneVersion ||
      !_sameStrings(selection.selectedRoleIds, expected.selectedRoleIds) ||
      selection.practiceOptionId != expected.practiceOptionId ||
      (expected.maxEffectiveTurns != null &&
          plan.sessionPolicy.maxEffectiveTurns != expected.maxEffectiveTurns) ||
      !_matchesIeltsSelection(
        plan.ieltsAssignment,
        expected.ieltsSelection,
        expectedServerMode: expectedIeltsMode,
      ) ||
      plan.status != PracticePlanStatus.ready) {
    throw _invalidResponse();
  }
  return plan;
}

SceneSelectionSnapshot _decodeSceneSelection(Object? value) {
  try {
    return decodeSceneSelectionSnapshot(value);
  } on SceneWireFormatException {
    throw _invalidResponse();
  }
}

bool _sameStrings(List<String> left, List<String> right) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) {
      return false;
    }
  }
  return true;
}

PreparationPracticeBootstrap _bootstrap(
  Object? value, {
  required PracticePlan expectedPlan,
}) {
  final root = _object(
    value,
    required: const <String>{'practice_session', 'snapshot'},
  );
  final sessionObject = _object(
    root['practice_session'],
    required: const <String>{
      'practice_session_id',
      'practice_plan_id',
      'plan_revision',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'evaluation_policy_ref',
      'snapshot_id',
      'practice_session_status',
      'session_version',
      'created_at',
    },
    optional: const <String>{'started_at', 'ended_at', 'end_reason'},
  );
  final sessionId = _resourceId(sessionObject['practice_session_id']);
  final snapshotId = _resourceId(sessionObject['snapshot_id']);
  final status = _enumText(sessionObject['practice_session_status'], const {
    'starting',
    'in_progress',
  });
  final practiceExperience =
      PracticeExperience.fromWireValue(
        _enumText(sessionObject['practice_experience'], _practiceExperiences),
      ) ??
      (throw _invalidResponse());
  final sceneCategory =
      SceneCategory.fromWireValue(
        _enumText(sessionObject['scene_category'], _sceneCategories),
      ) ??
      (throw _invalidResponse());
  final practiceMode =
      PracticeMode.fromWireValue(
        _enumText(sessionObject['practice_mode'], _practiceModes),
      ) ??
      (throw _invalidResponse());
  final expectedScene = expectedPlan.sceneSelection.scene;
  final expectedOption = expectedScene.practiceOptions
      .where(
        (option) => option.id == expectedPlan.sceneSelection.practiceOptionId,
      )
      .firstOrNull;
  if (_resourceId(sessionObject['practice_plan_id']) != expectedPlan.id ||
      _version(sessionObject['plan_revision']) != expectedPlan.revision ||
      practiceExperience != expectedScene.experience ||
      sceneCategory != expectedScene.category ||
      expectedOption == null ||
      practiceMode != expectedOption.mode ||
      _resourceId(sessionObject['evaluation_policy_ref']) !=
          expectedOption.evaluationPolicyRef ||
      (status == 'starting' && sessionObject['started_at'] != null) ||
      (status == 'in_progress' && sessionObject['started_at'] == null) ||
      sessionObject['ended_at'] != null ||
      sessionObject['end_reason'] != null) {
    throw _invalidResponse();
  }
  if (status == 'in_progress') {
    _dateTime(sessionObject['started_at']);
  }
  final snapshotObject = _object(
    root['snapshot'],
    required: const <String>{
      'snapshot_id',
      'practice_session_id',
      'plan_revision',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'scene_selection',
      'preparation_snapshot',
      'participants',
      'session_policy',
      'practice_objectives',
      'created_at',
    },
    optional: const <String>{'ielts_assignment'},
  );
  if (_resourceId(snapshotObject['snapshot_id']) != snapshotId ||
      _resourceId(snapshotObject['practice_session_id']) != sessionId ||
      _version(snapshotObject['plan_revision']) != expectedPlan.revision ||
      _enumText(snapshotObject['practice_experience'], _practiceExperiences) !=
          expectedScene.experience.wireValue ||
      _enumText(snapshotObject['scene_category'], _sceneCategories) !=
          expectedScene.category.wireValue ||
      _enumText(snapshotObject['practice_mode'], _practiceModes) !=
          practiceMode.wireValue) {
    throw _invalidResponse();
  }
  final sceneSelection = _decodeSceneSelection(
    snapshotObject['scene_selection'],
  );
  if (!samePracticeSceneSelection(
    sceneSelection,
    expectedPlan.sceneSelection,
  )) {
    throw _invalidResponse();
  }
  final preparation = decodePreparationSnapshot(
    snapshotObject['preparation_snapshot'],
  );
  if (!_samePreparationSnapshot(
    preparation,
    expectedPlan.preparationSnapshot,
  )) {
    throw _invalidResponse();
  }
  if (expectedPlan.sceneSelection.selectedRoleIds.length != 1) {
    throw _invalidResponse();
  }
  _validateParticipants(
    snapshotObject['participants'],
    sessionId: sessionId,
    roleId: expectedPlan.sceneSelection.selectedRoleIds.single,
    sceneId: expectedScene.id,
    candidateUserId: expectedPlan.userId,
  );
  final ieltsAssignment = snapshotObject.containsKey('ielts_assignment')
      ? decodeIeltsPracticeAssignment(snapshotObject['ielts_assignment'])
      : null;
  final policy = decodePreparationSessionPolicy(
    snapshotObject['session_policy'],
  );
  final objectives = decodePracticeObjectives(
    snapshotObject['practice_objectives'],
  );
  if (!_sameSessionPolicy(policy, expectedPlan.sessionPolicy) ||
      !_sameObjectives(objectives, expectedPlan.practiceObjectives) ||
      ieltsAssignment != expectedPlan.ieltsAssignment ||
      (ieltsAssignment != null &&
          ieltsAssignment.turnBlueprints.length != policy.maxEffectiveTurns)) {
    throw _invalidResponse();
  }
  _dateTime(snapshotObject['created_at']);
  return PreparationPracticeBootstrap(
    session: PreparationPracticeSession(
      id: sessionId,
      planId: expectedPlan.id,
      practiceExperience: practiceExperience,
      sceneCategory: sceneCategory,
      practiceMode: practiceMode,
      snapshotId: snapshotId,
      status: status,
      version: _version(sessionObject['session_version']),
      createdAt: _dateTime(sessionObject['created_at']),
    ),
    preparationSnapshotId: preparation.id,
    maxEffectiveTurns: policy.maxEffectiveTurns,
  );
}

bool _matchesIeltsSelection(
  IeltsPracticeAssignment? assignment,
  IeltsPracticeSelection? selection, {
  required PracticeMode? expectedServerMode,
}) {
  if (selection == null) {
    return expectedServerMode == null
        ? assignment == null
        : assignment?.mode == expectedServerMode;
  }
  return expectedServerMode != null &&
      assignment?.mode == expectedServerMode &&
      (assignment?.matchesSelection(selection) ?? false);
}

void _validateParticipants(
  Object? value, {
  required String sessionId,
  required String roleId,
  required String sceneId,
  required String candidateUserId,
}) {
  if (value is! List<Object?> || value.length < 2 || value.length > 16) {
    throw _invalidResponse();
  }
  var selectedRoleCount = 0;
  var candidateCount = 0;
  final participantIds = <String>{};
  final orders = <int>{};
  for (final raw in value) {
    final object = _object(
      raw,
      required: const <String>{
        'practice_participant_id',
        'practice_session_id',
        'participant_role',
        'subject_ref',
        'participant_order',
      },
      optional: const <String>{'role_definition_id', 'role_snapshot'},
    );
    if (_resourceId(object['practice_session_id']) != sessionId ||
        !participantIds.add(_resourceId(object['practice_participant_id']))) {
      throw _invalidResponse();
    }
    final order = _version(object['participant_order']);
    if (!orders.add(order)) {
      throw _invalidResponse();
    }
    final role = _enumText(object['participant_role'], const {
      'FACILITATOR',
      'LEARNER',
    });
    final subject = _object(
      object['subject_ref'],
      required: const <String>{'namespace', 'subject_id'},
    );
    final subjectNamespace = _text(subject['namespace']);
    final subjectId = _resourceId(subject['subject_id']);
    if (role == 'LEARNER') {
      candidateCount++;
      if (object['role_definition_id'] != null ||
          object['role_snapshot'] != null ||
          subjectNamespace != 'speakup.user' ||
          subjectId != candidateUserId) {
        throw _invalidResponse();
      }
    } else {
      if (_resourceId(object['role_definition_id']) != roleId) {
        throw _invalidResponse();
      }
      _validateRoleSnapshot(
        object['role_snapshot'],
        roleId: roleId,
        sceneId: sceneId,
      );
      selectedRoleCount++;
    }
  }
  if (candidateCount != 1 || selectedRoleCount != 1) {
    throw _invalidResponse();
  }
}

void _validateRoleSnapshot(
  Object? value, {
  required String roleId,
  required String sceneId,
}) {
  late final RoleDefinition role;
  try {
    role = decodeRoleDefinition(value);
  } on SceneWireFormatException {
    throw _invalidResponse();
  }
  if (role.id != roleId || role.sceneId != sceneId) {
    throw _invalidResponse();
  }
}

bool _samePreparationSnapshot(
  PreparationSnapshot left,
  PreparationSnapshot right,
) =>
    left.id == right.id &&
    left.sourceProfileId == right.sourceProfileId &&
    left.sourceVersion == right.sourceVersion &&
    left.sourceJobTargetId == right.sourceJobTargetId &&
    left.sourceJobTargetConfirmationVersion ==
        right.sourceJobTargetConfirmationVersion &&
    left.jobTargetInput == right.jobTargetInput &&
    left.jobTargetCandidate == right.jobTargetCandidate &&
    left.resumeSnapshot == right.resumeSnapshot &&
    left.jobDescriptionSnapshot == right.jobDescriptionSnapshot &&
    _sameScenarioPreparationContext(left.context, right.context) &&
    left.backgroundSnapshot == right.backgroundSnapshot &&
    left.createdAt == right.createdAt;

bool _sameScenarioPreparationContext(
  PreparationContext? left,
  PreparationContext? right,
) {
  if (left is ScenarioPreparationContext ||
      right is ScenarioPreparationContext) {
    return left == right;
  }
  return true;
}

bool _sameSessionPolicy(
  PreparationSessionPolicy left,
  PreparationSessionPolicy right,
) =>
    left.completionMode == right.completionMode &&
    left.suggestedDurationSeconds == right.suggestedDurationSeconds &&
    left.minEffectiveTurns == right.minEffectiveTurns &&
    left.maxEffectiveTurns == right.maxEffectiveTurns &&
    left.coverageCheckpointTurn == right.coverageCheckpointTurn &&
    left.maxFollowUpsPerQuestion == right.maxFollowUpsPerQuestion &&
    left.earlyCompletionRule == right.earlyCompletionRule &&
    left.retryAllowed == right.retryAllowed &&
    left.questionTranslationAllowed == right.questionTranslationAllowed &&
    left.questionTipsAllowed == right.questionTipsAllowed &&
    left.avatarAllowed == right.avatarAllowed &&
    left.speechFeedbackAllowed == right.speechFeedbackAllowed;

bool _sameObjectives(
  List<PracticeObjective> left,
  List<PracticeObjective> right,
) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) =>
          left[index].id == right[index].id &&
          left[index].description == right[index].description,
    ).every((same) => same);

Object? _decode(String body) {
  try {
    return jsonDecode(body);
  } on FormatException {
    throw _invalidResponse();
  }
}

Map<String, Object?> _object(
  Object? value, {
  Set<String> required = const <String>{},
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?> ||
      !value.keys.toSet().containsAll(required) ||
      value.keys.any(
        (key) => !required.contains(key) && !optional.contains(key),
      )) {
    throw _invalidResponse();
  }
  return value;
}

String _resourceId(Object? value) {
  final result = _text(value, maxBytes: 128);
  if (result.length > 128) {
    throw _invalidResponse();
  }
  return result;
}

String _text(Object? value, {int maxBytes = 256 * 1024}) {
  if (value is! String ||
      value.isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw _invalidResponse();
  }
  return value;
}

String _enumText(Object? value, Set<String> allowed) {
  final result = _text(value, maxBytes: 128);
  if (!allowed.contains(result)) {
    throw _invalidResponse();
  }
  return result;
}

int _version(Object? value) {
  if (value is! int || value < 1) {
    throw _invalidResponse();
  }
  return value;
}

DateTime _dateTime(Object? value) {
  if (value is! String) {
    throw _invalidResponse();
  }
  final result = DateTime.tryParse(value);
  if (result == null) {
    throw _invalidResponse();
  }
  return result;
}

String? _errorCode(String body) {
  try {
    final root = jsonDecode(body);
    if (root is! Map<String, Object?> ||
        root['error'] is! Map<String, Object?>) {
      return null;
    }
    final code = (root['error'] as Map<String, Object?>)['code'];
    return code is String && code.length <= 128 ? code : null;
  } on FormatException {
    return null;
  }
}

Never _invalidResponse() => throw const PreparationLaunchException(
  kind: PreparationLaunchFailureKind.invalidResponse,
);

void _requireResourceId(String value) {
  if (value.isEmpty ||
      value.length > 128 ||
      value.trim() != value ||
      value.contains('\u0000')) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
    );
  }
}

void _requireBackground(String value) {
  if (value.isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > 64 * 1024) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: PreparationLaunchStage.profile,
    );
  }
}

void _requireProfileInput(CreatePreparationProfileInput input) {
  _requireBackground(input.backgroundSummary);
  switch (input.kind) {
    case null:
      if (input.scenario != null) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidRequest,
          stage: PreparationLaunchStage.profile,
        );
      }
    case PreparationKind.scenario:
      final scenario = input.scenario;
      if (scenario == null ||
          input.resumeId != null ||
          input.jobDescriptionRef != null ||
          input.jobTargetId != null) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidRequest,
          stage: PreparationLaunchStage.profile,
        );
      }
      _requireScenarioContext(scenario);
    case PreparationKind.interview:
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: PreparationLaunchStage.profile,
      );
  }
  final hasResume = input.resumeId != null;
  if (hasResume != (input.resumeRevision != null)) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: PreparationLaunchStage.profile,
    );
  }
  if (input.resumeId case final value?) {
    _requireResourceId(value);
  }
  if (input.resumeRevision case final value? when value < 1) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: PreparationLaunchStage.profile,
    );
  }
  if (input.jobDescriptionRef case final value?) {
    _requireText(value, 16 * 1024);
  }
  final hasJobTarget = input.jobTargetId != null;
  if (hasJobTarget != (input.jobTargetConfirmationVersion != null)) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: PreparationLaunchStage.profile,
    );
  }
  if (input.jobTargetId case final value?) {
    _requireResourceId(value);
  }
  if (input.jobTargetConfirmationVersion case final value? when value < 1) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: PreparationLaunchStage.profile,
    );
  }
}

void _requireScenarioContext(ScenarioPreparationContext context) {
  for (final value in <String>[
    context.situation,
    context.userRole,
    context.counterpartRole,
    context.goal,
    context.counterpartPersona,
  ]) {
    _requireText(value, 16 * 1024);
  }
}

Map<String, Object?>? _scenarioInputJson(ScenarioPreparationContext? context) =>
    context == null
    ? null
    : <String, Object?>{
        'situation': context.situation,
        'user_role': context.userRole,
        'counterpart_role': context.counterpartRole,
        'goal': context.goal,
        'counterpart_persona': context.counterpartPersona,
      };

void _requirePlanInput(CreatePreparationPlanInput input) {
  if (input.sourceThreadId case final value?) {
    _requireResourceId(value);
  }
  if (input.goalId case final value?) {
    _requireResourceId(value);
  }
  _requireResourceId(input.preparationSnapshotId);
  _requireResourceId(input.sceneId);
  _requireResourceId(input.practiceOptionId);
  for (final roleId in input.selectedRoleIds) {
    _requireResourceId(roleId);
  }
  if (input.ieltsSelection case final selection?) {
    if (!selection.isValidCreateShape) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: PreparationLaunchStage.plan,
      );
    }
    if (selection.part1SetId case final value?) {
      _requireResourceId(value);
    }
    if (selection.topicGroupId case final value?) {
      _requireResourceId(value);
    }
  }
  if (input.sceneVersion < 1 ||
      input.selectedRoleIds.isEmpty ||
      input.selectedRoleIds.toSet().length != input.selectedRoleIds.length ||
      (input.maxEffectiveTurns != null && input.maxEffectiveTurns! < 1)) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: PreparationLaunchStage.plan,
    );
  }
}

void _requireText(String value, int maxBytes) {
  if (value.trim().isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
    );
  }
}

void _requireIdempotencyKey(String value) {
  if (value.length < 8 ||
      value.length > 128 ||
      value.trim() != value ||
      value.contains('\u0000')) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
    );
  }
}
