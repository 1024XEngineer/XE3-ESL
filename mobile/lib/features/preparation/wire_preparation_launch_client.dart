import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/preparation/preparation_launch_client.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

const _scenarioFamilies = <String>{'INTERVIEW', 'EXAM', 'WORKPLACE', 'DAILY'};
const _scenarioModels = <String>{
  'PROJECT_EXPERIENCE_DEEP_DIVE',
  'INTERVIEW_BASIC_DIALOGUE',
  'IELTS_SPEAKING_PART_2',
  'IELTS_SPEAKING_FULL_MOCK',
  'EXAM_BASIC_DIALOGUE',
  'PROGRESS_AND_RISK_UPDATE',
  'WORKPLACE_BASIC_DIALOGUE',
  'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
  'DAILY_BASIC_DIALOGUE',
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
    _requireBackground(input.backgroundSummary);
    final response = await _post(
      path: '/v1/preparation-profiles',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'background_summary': input.backgroundSummary},
      stage: PreparationLaunchStage.profile,
    );
    return _decodeCreated(
      stage: PreparationLaunchStage.profile,
      decode: () =>
          _profile(response.body, expectedBackground: input.backgroundSummary),
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
      decode: () => _snapshot(
        _decode(response.body),
        expectedProfileId: profileId,
        expectedSourceVersion: sourceVersion,
      ),
    );
  }

  @override
  Future<PreparationPracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    _requireContext(input.context);
    _requireSelection(input.selection);
    _requireResourceId(input.preparationProfileId);
    _requireResourceId(input.preparationSnapshotId);
    _requireResourceId(input.preparationUserId);
    final response = await _post(
      path: '/v1/practice-plans',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'agent_thread_id': input.context.threadId,
        'matter_id': input.context.matterId,
        'scenario_definition_id': input.selection.scenarioDefinitionId,
        'scenario_definition_version':
            input.selection.scenarioDefinitionVersion,
        'scenario_config_id': input.selection.scenarioConfigId,
        'scenario_config_version': input.selection.scenarioConfigVersion,
        'preparation_profile_id': input.preparationProfileId,
        'preparation_snapshot_id': input.preparationSnapshotId,
        'selected_role_ids': <String>[input.selection.roleDefinitionId],
        'practice_option_id': input.selection.practiceOptionId,
        'practice_option_version': input.selection.practiceOptionVersion,
        'max_effective_turns':
            input.selection.practiceOptionType ==
                PreparationOptionType.fullSimulation
            ? 6
            : 3,
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
    required String planId,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    _requireResourceId(planId);
    _requireResourceId(input.agentThreadId);
    _requireSelection(input.selection);
    _requireResourceId(input.preparationSnapshotId);
    _requireResourceId(input.preparationProfileId);
    _requireResourceId(input.preparationUserId);
    _requireBackground(input.backgroundSummary);
    if (input.expectedPlanRevision < 1) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: PreparationLaunchStage.session,
      );
    }
    final response = await _post(
      path:
          '/v1/agent-threads/${Uri.encodeComponent(input.agentThreadId)}'
          '/practice-start-confirmations',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_revision': input.expectedPlanRevision,
        'user_confirmed': true,
        'practice_plan_id': planId,
      },
      stage: PreparationLaunchStage.session,
    );
    return _decodeCreated(
      stage: PreparationLaunchStage.session,
      decode: () => _bootstrap(
        _decode(response.body),
        expectedPlanId: planId,
        expected: input,
      ),
    );
  }

  Future<IdentityHttpResponse> _post({
    required String path,
    required String idempotencyKey,
    required Map<String, Object?> body,
    required PreparationLaunchStage stage,
  }) async {
    _requireIdempotencyKey(idempotencyKey);
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: 'POST',
            uri: uri,
            headers: <String, String>{
              HttpHeaders.acceptHeader: ContentType.json.mimeType,
              HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
              'Idempotency-Key': idempotencyKey,
            },
            body: jsonEncode(body),
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
    if (response.statusCode == HttpStatus.created) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidResponse,
          stage: stage,
          statusCode: HttpStatus.created,
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
  try {
    return decode();
  } on PreparationLaunchException catch (error) {
    if (error.kind != PreparationLaunchFailureKind.invalidResponse) {
      rethrow;
    }
    throw PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidResponse,
      stage: stage,
      statusCode: HttpStatus.created,
      retryable: true,
    );
  }
}

PreparationProfile _profile(String body, {required String expectedBackground}) {
  final object = _object(
    _decode(body),
    required: const <String>{
      'preparation_profile_id',
      'user_id',
      'background_summary',
      'version',
      'updated_at',
    },
    optional: const <String>{'resume_ref', 'job_description_ref'},
  );
  if (object['resume_ref'] != null || object['job_description_ref'] != null) {
    throw _invalidResponse();
  }
  final background = _text(object['background_summary'], maxBytes: 64 * 1024);
  if (background != expectedBackground) {
    throw _invalidResponse();
  }
  return PreparationProfile(
    id: _resourceId(object['preparation_profile_id']),
    userId: _resourceId(object['user_id']),
    backgroundSummary: background,
    version: _version(object['version']),
    updatedAt: _dateTime(object['updated_at']),
  );
}

PreparationSnapshot _snapshot(
  Object? value, {
  required String expectedProfileId,
  required int expectedSourceVersion,
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
    optional: const <String>{'resume_snapshot', 'job_description_snapshot'},
  );
  final profileId = _resourceId(object['source_profile_id']);
  final version = _version(object['source_version']);
  if (profileId != expectedProfileId ||
      version != expectedSourceVersion ||
      object['resume_snapshot'] != null ||
      object['job_description_snapshot'] != null) {
    throw _invalidResponse();
  }
  return PreparationSnapshot(
    id: _resourceId(object['preparation_snapshot_id']),
    sourceProfileId: profileId,
    sourceVersion: version,
    backgroundSnapshot: _text(
      object['background_snapshot'],
      maxBytes: 64 * 1024,
    ),
    createdAt: _dateTime(object['created_at']),
  );
}

PreparationPracticePlan _plan(
  Object? value, {
  required CreatePreparationPlanInput expected,
}) {
  final object = _object(
    value,
    required: const <String>{
      'practice_plan_id',
      'user_id',
      'agent_thread_id',
      'matter_id',
      'scenario_definition_id',
      'scenario_definition_version',
      'scenario_type',
      'scenario_model',
      'scenario_config_id',
      'scenario_config_version',
      'preparation_profile_id',
      'selected_role_ids',
      'plan_revision',
      'practice_plan_status',
      'created_at',
      'updated_at',
    },
  );
  final context = AgentPracticeContext(
    threadId: _resourceId(object['agent_thread_id']),
    matterId: _resourceId(object['matter_id']),
  );
  final roles = _resourceIdList(object['selected_role_ids'], min: 1, max: 4);
  final status = _enumText(object['practice_plan_status'], const {'ready'});
  if (context != expected.context ||
      _resourceId(object['user_id']) != expected.preparationUserId ||
      _resourceId(object['scenario_definition_id']) !=
          expected.selection.scenarioDefinitionId ||
      _version(object['scenario_definition_version']) !=
          expected.selection.scenarioDefinitionVersion ||
      _enumText(object['scenario_type'], _scenarioFamilies) !=
          expected.selection.scenarioType ||
      _enumText(object['scenario_model'], _scenarioModels) !=
          expected.selection.scenarioModel ||
      _resourceId(object['scenario_config_id']) !=
          expected.selection.scenarioConfigId ||
      _version(object['scenario_config_version']) !=
          expected.selection.scenarioConfigVersion ||
      _resourceId(object['preparation_profile_id']) !=
          expected.preparationProfileId ||
      roles.length != 1 ||
      roles.single != expected.selection.roleDefinitionId) {
    throw _invalidResponse();
  }
  _dateTime(object['created_at']);
  _dateTime(object['updated_at']);
  return PreparationPracticePlan(
    id: _resourceId(object['practice_plan_id']),
    userId: _resourceId(object['user_id']),
    context: context,
    selection: expected.selection,
    preparationProfileId: expected.preparationProfileId,
    revision: _version(object['plan_revision']),
    status: status,
  );
}

PreparationPracticeBootstrap _bootstrap(
  Object? value, {
  required String expectedPlanId,
  required CreatePreparationSessionInput expected,
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
      'scenario_type',
      'scenario_model',
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
  });
  final scenarioType = _enumText(
    sessionObject['scenario_type'],
    _scenarioFamilies,
  );
  final scenarioModel = _enumText(
    sessionObject['scenario_model'],
    _scenarioModels,
  );
  if (_resourceId(sessionObject['practice_plan_id']) != expectedPlanId ||
      scenarioType != expected.selection.scenarioType ||
      scenarioModel != expected.selection.scenarioModel ||
      sessionObject['started_at'] != null ||
      sessionObject['ended_at'] != null ||
      sessionObject['end_reason'] != null) {
    throw _invalidResponse();
  }
  final snapshotObject = _object(
    root['snapshot'],
    required: const <String>{
      'snapshot_id',
      'practice_session_id',
      'plan_revision',
      'scenario_type',
      'scenario_model',
      'scenario_definition_snapshot',
      'scenario_config_snapshot',
      'preparation_snapshot',
      'participants',
      'practice_option',
      'session_policy',
      'practice_focuses',
      'created_at',
    },
  );
  if (_resourceId(snapshotObject['snapshot_id']) != snapshotId ||
      _resourceId(snapshotObject['practice_session_id']) != sessionId ||
      _version(snapshotObject['plan_revision']) !=
          expected.expectedPlanRevision ||
      _enumText(snapshotObject['scenario_type'], _scenarioFamilies) !=
          expected.selection.scenarioType ||
      _enumText(snapshotObject['scenario_model'], _scenarioModels) !=
          expected.selection.scenarioModel) {
    throw _invalidResponse();
  }
  _validateScenarioSnapshot(
    snapshotObject['scenario_definition_snapshot'],
    expected.selection,
  );
  _validateConfigSnapshot(
    snapshotObject['scenario_config_snapshot'],
    expected.selection,
  );
  final preparation = _snapshot(
    snapshotObject['preparation_snapshot'],
    expectedProfileId: expected.preparationProfileId,
    expectedSourceVersion: expected.preparationProfileVersion,
  );
  if (preparation.id != expected.preparationSnapshotId ||
      preparation.backgroundSnapshot != expected.backgroundSummary) {
    throw _invalidResponse();
  }
  _validateParticipants(
    snapshotObject['participants'],
    sessionId: sessionId,
    roleId: expected.selection.roleDefinitionId,
    roleVersion: expected.selection.roleDefinitionVersion,
    scenarioId: expected.selection.scenarioDefinitionId,
    candidateUserId: expected.preparationUserId,
  );
  _validatePracticeOption(
    snapshotObject['practice_option'],
    expected.selection,
  );
  final maxEffectiveTurns = _validateSessionPolicy(
    snapshotObject['session_policy'],
    optionType: expected.selection.practiceOptionType,
    scenarioModel: expected.selection.scenarioModel,
  );
  _validateObjectives(snapshotObject['practice_focuses'], allowEmpty: true);
  _dateTime(snapshotObject['created_at']);
  return PreparationPracticeBootstrap(
    session: PreparationPracticeSession(
      id: sessionId,
      planId: expectedPlanId,
      scenarioType: scenarioType,
      scenarioModel: scenarioModel,
      snapshotId: snapshotId,
      status: status,
      version: _version(sessionObject['session_version']),
      createdAt: _dateTime(sessionObject['created_at']),
    ),
    preparationSnapshotId: preparation.id,
    maxEffectiveTurns: maxEffectiveTurns,
  );
}

void _validateScenarioSnapshot(
  Object? value,
  PreparationLaunchSelection expected,
) {
  final object = _object(
    value,
    required: const <String>{
      'scenario_definition_id',
      'scenario_type',
      'scenario_model',
      'name',
      'version',
      'status',
      'turn_policy_ref',
      'session_policy_ref',
    },
  );
  if (_resourceId(object['scenario_definition_id']) !=
          expected.scenarioDefinitionId ||
      _enumText(object['scenario_type'], _scenarioFamilies) !=
          expected.scenarioType ||
      _enumText(object['scenario_model'], _scenarioModels) !=
          expected.scenarioModel ||
      _version(object['version']) != expected.scenarioDefinitionVersion ||
      _enumText(object['status'], const {'active'}) != 'active') {
    throw _invalidResponse();
  }
  _text(object['name']);
  _resourceId(object['turn_policy_ref']);
  _resourceId(object['session_policy_ref']);
}

void _validateConfigSnapshot(
  Object? value,
  PreparationLaunchSelection expected,
) {
  final object = _object(
    value,
    required: const <String>{
      'scenario_config_id',
      'scenario_definition_id',
      'config_type',
      'scenario_model',
      'version',
      'prompt_model',
    },
    optional: const <String>{'job_title', 'job_description'},
  );
  if (_resourceId(object['scenario_config_id']) != expected.scenarioConfigId ||
      _resourceId(object['scenario_definition_id']) !=
          expected.scenarioDefinitionId ||
      _enumText(object['config_type'], _scenarioFamilies) !=
          expected.scenarioType ||
      _enumText(object['scenario_model'], _scenarioModels) !=
          expected.scenarioModel ||
      _version(object['version']) != expected.scenarioConfigVersion) {
    throw _invalidResponse();
  }
  if (object['job_title'] case final value?) {
    _text(value);
  }
  if (object['job_description'] case final value?) {
    _text(value);
  }
  _validatePromptModel(object['prompt_model']);
}

void _validatePromptModel(Object? value) {
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
  _text(object['public_scene_brief']);
  _text(object['practice_goal']);
  _text(object['user_role']);
  _text(object['ai_role']);
  _text(object['persona_summary']);
  _nonEmptyTextList(object['focus_areas']);
  _nonEmptyTextList(object['turn_blueprints']);
  final duration = object['suggested_duration_seconds'];
  if (duration is! int || duration < 1 || duration > 3600) {
    throw _invalidResponse();
  }
}

void _validateParticipants(
  Object? value, {
  required String sessionId,
  required String roleId,
  required int roleVersion,
  required String scenarioId,
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
      'INTERVIEWER',
      'CANDIDATE',
    });
    final subject = _object(
      object['subject_ref'],
      required: const <String>{'namespace', 'subject_id'},
    );
    final subjectNamespace = _text(subject['namespace']);
    final subjectId = _resourceId(subject['subject_id']);
    if (role == 'LEARNER' || role == 'CANDIDATE') {
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
        roleVersion: roleVersion,
        scenarioId: scenarioId,
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
  required int roleVersion,
  required String scenarioId,
}) {
  final object = _object(
    value,
    required: const <String>{
      'role_definition_id',
      'scenario_definition_id',
      'role_type',
      'display_name',
      'responsibilities',
      'style',
      'focus_areas',
      'version',
    },
    optional: const <String>{'voice_config_ref'},
  );
  if (_resourceId(object['role_definition_id']) != roleId ||
      _resourceId(object['scenario_definition_id']) != scenarioId ||
      _version(object['version']) != roleVersion) {
    throw _invalidResponse();
  }
  _text(object['role_type']);
  _text(object['display_name']);
  _text(object['responsibilities']);
  _text(object['style']);
  _nonEmptyTextList(object['focus_areas']);
  if (object['voice_config_ref'] case final value?) {
    _text(value);
  }
}

void _validatePracticeOption(
  Object? value,
  PreparationLaunchSelection expected,
) {
  final object = _object(
    value,
    required: const <String>{
      'practice_option_id',
      'scenario_definition_id',
      'practice_option_type',
      'display_name',
      'version',
    },
    optional: const <String>{'role_definition_id'},
  );
  if (_resourceId(object['practice_option_id']) != expected.practiceOptionId ||
      _resourceId(object['scenario_definition_id']) !=
          expected.scenarioDefinitionId ||
      _enumText(object['practice_option_type'], const {
            'FULL_SIMULATION',
            'FOCUS',
          }) !=
          expected.practiceOptionType.wireValue ||
      _version(object['version']) != expected.practiceOptionVersion) {
    throw _invalidResponse();
  }
  if (expected.practiceOptionType == PreparationOptionType.focus) {
    if (_resourceId(object['role_definition_id']) !=
        expected.roleDefinitionId) {
      throw _invalidResponse();
    }
  } else if (object['role_definition_id'] != null) {
    throw _invalidResponse();
  }
  _text(object['display_name']);
}

int _validateSessionPolicy(
  Object? value, {
  required PreparationOptionType optionType,
  required String scenarioModel,
}) {
  final object = _object(
    value,
    required: const <String>{
      'suggested_duration_seconds',
      'min_effective_turns',
      'max_effective_turns',
      'coverage_checkpoint_turn',
      'max_follow_ups_per_question',
      'target_objectives',
      'early_completion_rule',
    },
  );
  final minimum = _version(object['min_effective_turns']);
  final maximum = _version(object['max_effective_turns']);
  final checkpoint = _version(object['coverage_checkpoint_turn']);
  final isIeltsFullMock =
      scenarioModel == 'IELTS_SPEAKING_FULL_MOCK' &&
      optionType == PreparationOptionType.fullSimulation;
  final expectedMaximum = isIeltsFullMock
      ? 14
      : switch (optionType) {
          PreparationOptionType.fullSimulation => 6,
          PreparationOptionType.focus => 3,
        };
  if (_version(object['suggested_duration_seconds']) < 1 ||
      minimum > checkpoint ||
      checkpoint > maximum ||
      maximum != expectedMaximum ||
      (isIeltsFullMock &&
          (minimum != 14 ||
              checkpoint != 14 ||
              object['max_follow_ups_per_question'] != 0)) ||
      object['max_follow_ups_per_question'] is! int ||
      (object['max_follow_ups_per_question'] as int) < 0) {
    throw _invalidResponse();
  }
  _validateObjectives(object['target_objectives']);
  _text(object['early_completion_rule']);
  return maximum;
}

void _validateObjectives(Object? value, {bool allowEmpty = false}) {
  if (value is! List<Object?> ||
      (!allowEmpty && value.isEmpty) ||
      value.length > 100) {
    throw _invalidResponse();
  }
  final ids = <String>{};
  for (final raw in value) {
    final object = _object(
      raw,
      required: const <String>{'objective_id', 'description'},
    );
    if (!ids.add(_text(object['objective_id']))) {
      throw _invalidResponse();
    }
    _text(object['description']);
  }
}

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

List<String> _resourceIdList(
  Object? value, {
  required int min,
  required int max,
}) {
  if (value is! List<Object?> || value.length < min || value.length > max) {
    throw _invalidResponse();
  }
  final result = value.map(_resourceId).toList(growable: false);
  if (result.toSet().length != result.length) {
    throw _invalidResponse();
  }
  return result;
}

List<String> _nonEmptyTextList(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 100) {
    throw _invalidResponse();
  }
  final result = value.map(_text).toList(growable: false);
  if (result.toSet().length != result.length) {
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

void _requireContext(AgentPracticeContext context) {
  _requireResourceId(context.threadId);
  _requireResourceId(context.matterId);
}

void _requireSelection(PreparationLaunchSelection value) {
  _requireResourceId(value.scenarioDefinitionId);
  _requireResourceId(value.scenarioConfigId);
  _requireResourceId(value.roleDefinitionId);
  _requireResourceId(value.practiceOptionId);
  _requireDisplayText(value.scenarioDisplayName);
  _requireDisplayText(value.scenarioDescription);
  if (value.scenarioDefinitionVersion < 1 ||
      value.scenarioConfigVersion < 1 ||
      value.roleDefinitionVersion < 1 ||
      value.practiceOptionVersion < 1 ||
      !_validScenarioFamilyModel(value.scenarioType, value.scenarioModel)) {
    throw const PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
    );
  }
}

bool _validScenarioFamilyModel(String family, String model) {
  return switch ((family, model)) {
    ('INTERVIEW', 'PROJECT_EXPERIENCE_DEEP_DIVE') ||
    ('INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE') ||
    ('EXAM', 'IELTS_SPEAKING_PART_2') ||
    ('EXAM', 'IELTS_SPEAKING_FULL_MOCK') ||
    ('EXAM', 'EXAM_BASIC_DIALOGUE') ||
    ('WORKPLACE', 'PROGRESS_AND_RISK_UPDATE') ||
    ('WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE') ||
    ('DAILY', 'HOTEL_CHECKIN_AND_ISSUE_HANDLING') ||
    ('DAILY', 'DAILY_BASIC_DIALOGUE') => true,
    _ => false,
  };
}

void _requireDisplayText(String value) {
  if (value.isEmpty ||
      value.length > 2048 ||
      value.trim() != value ||
      value.contains('\u0000')) {
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
