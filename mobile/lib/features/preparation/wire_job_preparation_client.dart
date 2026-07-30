import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/preparation/job_preparation_client.dart';
import 'package:speakup/features/preparation/job_preparation_models.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireJobPreparationClient implements JobPreparationClient {
  factory WireJobPreparationClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 75),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WireJobPreparationClient._(
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

  WireJobPreparationClient._(
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
  Future<JobTarget> createJobTarget({
    required JobTargetInput input,
    required String idempotencyKey,
  }) async {
    _requireJobTargetInput(input);
    final response = await _request(
      method: 'POST',
      path: '/v1/job-targets',
      idempotencyKey: idempotencyKey,
      body: _jobTargetInputJson(input),
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.target,
    );
    return _decodeResponse(
      response,
      stage: JobPreparationOperationStage.target,
      decode: _decodeJobTarget,
    );
  }

  @override
  Future<JobTarget> getJobTarget(String jobTargetId) async {
    _requireResourceId(jobTargetId);
    final response = await _request(
      method: 'GET',
      path: '/v1/job-targets/${Uri.encodeComponent(jobTargetId)}',
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.target,
    );
    final target = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.target,
      decode: _decodeJobTarget,
    );
    if (target.id != jobTargetId) {
      throw _invalidResponse(JobPreparationOperationStage.target);
    }
    return target;
  }

  @override
  Future<JobTarget> updateJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required JobTargetInput input,
    required String idempotencyKey,
  }) async {
    _requireResourceId(jobTargetId);
    _requireVersion(expectedInputVersion);
    _requireJobTargetInput(input);
    final response = await _request(
      method: 'PUT',
      path: '/v1/job-targets/${Uri.encodeComponent(jobTargetId)}',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_input_version': expectedInputVersion,
        ..._jobTargetInputJson(input),
      },
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.target,
    );
    final target = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.target,
      decode: _decodeJobTarget,
    );
    if (target.id != jobTargetId || target.input != input) {
      throw _invalidResponse(JobPreparationOperationStage.target);
    }
    return target;
  }

  @override
  Future<JobTarget> analyzeJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  }) async {
    _requireResourceId(jobTargetId);
    _requireVersion(expectedInputVersion);
    final response = await _request(
      method: 'POST',
      path: '/v1/job-targets/${Uri.encodeComponent(jobTargetId)}/analyses',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'expected_input_version': expectedInputVersion},
      acceptedStatuses: const <int>{HttpStatus.ok, HttpStatus.accepted},
      stage: JobPreparationOperationStage.analysis,
    );
    final target = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.analysis,
      decode: _decodeJobTarget,
    );
    if (target.id != jobTargetId ||
        target.inputVersion != expectedInputVersion ||
        (response.statusCode == HttpStatus.accepted &&
            target.stage != JobTargetStage.parsing) ||
        (response.statusCode == HttpStatus.ok &&
            target.stage == JobTargetStage.parsing)) {
      throw _invalidResponse(JobPreparationOperationStage.analysis);
    }
    return target;
  }

  @override
  Future<JobTarget> confirmJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required int expectedAnalysisVersion,
    required JobTargetCandidate candidate,
    required String idempotencyKey,
  }) async {
    _requireResourceId(jobTargetId);
    _requireVersion(expectedInputVersion);
    _requireVersion(expectedAnalysisVersion);
    _requireJobTargetCandidate(candidate);
    final response = await _request(
      method: 'POST',
      path:
          '/v1/job-targets/${Uri.encodeComponent(jobTargetId)}'
          '/confirmations',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_input_version': expectedInputVersion,
        'expected_analysis_version': expectedAnalysisVersion,
        'candidate': _jobTargetCandidateJson(candidate),
      },
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.confirmation,
    );
    final target = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.confirmation,
      decode: _decodeJobTarget,
    );
    if (target.id != jobTargetId ||
        target.inputVersion != expectedInputVersion ||
        target.stage != JobTargetStage.confirmed ||
        target.confirmation?.analysisVersion != expectedAnalysisVersion) {
      throw _invalidResponse(JobPreparationOperationStage.confirmation);
    }
    return target;
  }

  @override
  Future<JobTarget> discardJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  }) async {
    _requireResourceId(jobTargetId);
    _requireVersion(expectedInputVersion);
    final response = await _request(
      method: 'POST',
      path: '/v1/job-targets/${Uri.encodeComponent(jobTargetId)}/discard',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'expected_input_version': expectedInputVersion},
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.target,
    );
    final target = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.target,
      decode: _decodeJobTarget,
    );
    if (target.id != jobTargetId ||
        target.inputVersion != expectedInputVersion ||
        target.stage != JobTargetStage.discarded) {
      throw _invalidResponse(JobPreparationOperationStage.target);
    }
    return target;
  }

  @override
  Future<JobPreparationProfile> createProfileForJobTarget({
    required String backgroundSummary,
    required String jobTargetId,
    required int jobTargetConfirmationVersion,
    required String idempotencyKey,
  }) async {
    _requireText(backgroundSummary, maxBytes: 64 * 1024);
    _requireResourceId(jobTargetId);
    _requireVersion(jobTargetConfirmationVersion);
    final response = await _request(
      method: 'POST',
      path: '/v1/preparation-profiles',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'background_summary': backgroundSummary,
        'job_target_id': jobTargetId,
        'job_target_confirmation_version': jobTargetConfirmationVersion,
      },
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.profile,
    );
    return _decodeResponse(
      response,
      stage: JobPreparationOperationStage.profile,
      decode: (body) => _decodeJobPreparationProfile(
        body,
        expectedBackground: backgroundSummary,
        expectedJobTargetId: jobTargetId,
        expectedConfirmationVersion: jobTargetConfirmationVersion,
      ),
    );
  }

  @override
  Future<JobPreparationSnapshot> createJobPreparationSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    _requireResourceId(profileId);
    _requireVersion(sourceVersion);
    final response = await _request(
      method: 'POST',
      path:
          '/v1/preparation-profiles/${Uri.encodeComponent(profileId)}'
          '/snapshots',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'source_version': sourceVersion},
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.snapshot,
    );
    final snapshot = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.snapshot,
      decode: _decodeJobPreparationSnapshot,
    );
    if (snapshot.sourceProfileId != profileId ||
        snapshot.sourceVersion != sourceVersion) {
      throw _invalidResponse(JobPreparationOperationStage.snapshot);
    }
    return snapshot;
  }

  @override
  Future<JobPracticePlanPreview> createJobPracticePlan({
    required AgentPracticeContext context,
    required String preparationSnapshotId,
    required String idempotencyKey,
  }) async {
    _requireContext(context);
    _requireResourceId(preparationSnapshotId);
    final response = await _request(
      method: 'POST',
      path: '/v1/practice-plans',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'agent_thread_id': context.threadId,
        'matter_id': context.matterId,
        'preparation_snapshot_id': preparationSnapshotId,
      },
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.plan,
    );
    final plan = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.plan,
      decode: _decodeJobPracticePlan,
    );
    if (plan.context != context ||
        plan.preparationSnapshot.id != preparationSnapshotId) {
      throw _invalidResponse(JobPreparationOperationStage.plan);
    }
    return plan;
  }

  @override
  Future<JobPracticePlanPreview> getJobPracticePlan(String planId) async {
    _requireResourceId(planId);
    final response = await _request(
      method: 'GET',
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}',
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.plan,
    );
    final plan = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.plan,
      decode: _decodeJobPracticePlan,
    );
    if (plan.id != planId) {
      throw _invalidResponse(JobPreparationOperationStage.plan);
    }
    return plan;
  }

  @override
  Future<JobPracticePlanPreview> reviseJobPracticePlan({
    required String planId,
    required int expectedPlanRevision,
    required String roleDefinitionId,
    required String practiceOptionId,
    required int practiceOptionVersion,
    required int maxEffectiveTurns,
    required String idempotencyKey,
  }) async {
    _requireResourceId(planId);
    _requireVersion(expectedPlanRevision);
    _requireResourceId(roleDefinitionId);
    _requireResourceId(practiceOptionId);
    _requireVersion(practiceOptionVersion);
    _requireVersion(maxEffectiveTurns);
    final response = await _request(
      method: 'PUT',
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_revision': expectedPlanRevision,
        'selected_role_ids': <String>[roleDefinitionId],
        'practice_option_id': practiceOptionId,
        'practice_option_version': practiceOptionVersion,
        'max_effective_turns': maxEffectiveTurns,
      },
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.plan,
    );
    final plan = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.plan,
      decode: _decodeJobPracticePlan,
    );
    if (plan.id != planId ||
        plan.revision <= expectedPlanRevision ||
        plan.catalog.selectedRole.id != roleDefinitionId ||
        plan.catalog.practiceOption.id != practiceOptionId ||
        plan.catalog.practiceOption.version != practiceOptionVersion ||
        plan.sessionPolicy.maxEffectiveTurns != maxEffectiveTurns) {
      throw _invalidResponse(JobPreparationOperationStage.plan);
    }
    return plan;
  }

  @override
  Future<PreparationPracticeBootstrap> createJobPracticeSession({
    required JobPracticePlanPreview plan,
    required String idempotencyKey,
  }) async {
    _requireResourceId(plan.id);
    _requireVersion(plan.revision);
    final response = await _request(
      method: 'POST',
      path:
          '/v1/practice-plans/${Uri.encodeComponent(plan.id)}'
          '/practice-sessions',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_revision': plan.revision,
        'user_confirmed': true,
      },
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.session,
    );
    return _decodeResponse(
      response,
      stage: JobPreparationOperationStage.session,
      decode: (body) => _decodeJobPracticeBootstrap(body, expectedPlan: plan),
    );
  }

  Future<IdentityHttpResponse> _request({
    required String method,
    required String path,
    required Set<int> acceptedStatuses,
    required JobPreparationOperationStage stage,
    String? idempotencyKey,
    Map<String, Object?>? body,
  }) async {
    if ((method == 'GET') != (body == null) ||
        (method == 'GET') != (idempotencyKey == null)) {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.invalidRequest,
        stage: stage,
      );
    }
    if (idempotencyKey != null) {
      _requireIdempotencyKey(idempotencyKey);
    }
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: method,
            uri: uri,
            headers: <String, String>{
              HttpHeaders.acceptHeader: ContentType.json.mimeType,
              if (body != null)
                HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
              'Idempotency-Key': ?idempotencyKey,
            },
            body: body == null ? null : jsonEncode(body),
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.superseded,
        stage: stage,
      );
    } on StateError {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.authenticationRequired,
        stage: stage,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: stage,
        retryable: true,
      );
    } on SocketException {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: stage,
        retryable: true,
      );
    } on HttpException {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: stage,
        retryable: true,
      );
    } on IOException {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: stage,
        retryable: true,
      );
    }
    if (generation != _accountGeneration) {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.superseded,
        stage: stage,
      );
    }
    if (acceptedStatuses.contains(response.statusCode)) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: stage,
          statusCode: response.statusCode,
          retryable: method != 'GET',
        );
      }
      return response;
    }
    final errorCode = _decodeErrorCode(response.body);
    throw switch (response.statusCode) {
      HttpStatus.badRequest => JobPreparationException(
        kind: JobPreparationFailureKind.invalidRequest,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      HttpStatus.unauthorized => JobPreparationException(
        kind: JobPreparationFailureKind.authenticationRequired,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      HttpStatus.notFound => JobPreparationException(
        kind: JobPreparationFailureKind.notFound,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
      HttpStatus.conflict => JobPreparationException(
        kind: JobPreparationFailureKind.conflict,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
        retryable:
            errorCode == 'job_target_analysis_claim_lost' ||
            errorCode == 'resource_conflict',
      ),
      _ when response.statusCode >= 500 => JobPreparationException(
        kind: JobPreparationFailureKind.server,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
        retryable: true,
      ),
      _ => JobPreparationException(
        kind: JobPreparationFailureKind.invalidResponse,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: errorCode,
      ),
    };
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

T _decodeResponse<T>(
  IdentityHttpResponse response, {
  required JobPreparationOperationStage stage,
  required T Function(String body) decode,
}) {
  try {
    return decode(response.body);
  } on JobPreparationException catch (error) {
    if (error.kind != JobPreparationFailureKind.invalidResponse) {
      rethrow;
    }
    throw JobPreparationException(
      kind: JobPreparationFailureKind.invalidResponse,
      stage: stage,
      statusCode: response.statusCode,
      retryable: response.statusCode == HttpStatus.created,
    );
  } on Object {
    throw JobPreparationException(
      kind: JobPreparationFailureKind.invalidResponse,
      stage: stage,
      statusCode: response.statusCode,
      retryable: response.statusCode == HttpStatus.created,
    );
  }
}

JobTarget _decodeJobTarget(String body) {
  return _jobTarget(_decodeJson(body));
}

JobTarget _jobTarget(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'job_target_id',
      'user_id',
      'input',
      'input_version',
      'stage',
      'created_at',
      'updated_at',
    },
    optional: const <String>{'analysis', 'confirmation'},
  );
  final input = _jobTargetInput(object['input']);
  final inputVersion = _version(object['input_version']);
  final stage = _jobTargetStage(object['stage']);
  final analysis = object.containsKey('analysis')
      ? _jobTargetAnalysis(object['analysis'])
      : null;
  final confirmation = object.containsKey('confirmation')
      ? _jobTargetConfirmation(object['confirmation'])
      : null;
  final createdAt = _dateTime(object['created_at']);
  final updatedAt = _dateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt) ||
      (analysis != null && analysis.inputVersion != inputVersion)) {
    throw _invalidResponse();
  }
  switch (stage) {
    case JobTargetStage.draft:
      if (analysis != null || confirmation != null) {
        throw _invalidResponse();
      }
    case JobTargetStage.parsing:
      if (analysis?.status != JobTargetAnalysisStatus.running ||
          confirmation != null) {
        throw _invalidResponse();
      }
    case JobTargetStage.analysisFailed:
      if (analysis?.status != JobTargetAnalysisStatus.failed ||
          confirmation != null) {
        throw _invalidResponse();
      }
    case JobTargetStage.awaitingConfirmation:
      if (analysis?.status != JobTargetAnalysisStatus.succeeded ||
          analysis?.candidate == null ||
          confirmation != null) {
        throw _invalidResponse();
      }
    case JobTargetStage.confirmed:
      if (analysis?.status != JobTargetAnalysisStatus.succeeded ||
          analysis?.candidate == null ||
          confirmation == null ||
          confirmation.inputVersion != inputVersion ||
          confirmation.analysisVersion != analysis?.analysisVersion ||
          confirmation.candidate.source != input.source) {
        throw _invalidResponse();
      }
    case JobTargetStage.discarded:
      if (confirmation != null) {
        throw _invalidResponse();
      }
  }
  return JobTarget(
    id: _resourceId(object['job_target_id']),
    userId: _resourceId(object['user_id']),
    input: input,
    inputVersion: inputVersion,
    stage: stage,
    analysis: analysis,
    confirmation: confirmation,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

JobTargetInput _jobTargetInput(Object? value) {
  final object = _object(
    value,
    required: const <String>{'source'},
    optional: const <String>{
      'job_title',
      'job_description',
      'company',
      'seniority',
      'candidate_background',
      'resume_ref',
      'practice_focus',
    },
  );
  final input = JobTargetInput(
    source: _jobTargetSource(object['source']),
    jobTitle: _optionalText(object, 'job_title', maxBytes: 512),
    jobDescription: _optionalText(
      object,
      'job_description',
      maxBytes: 64 * 1024,
    ),
    company: _optionalText(object, 'company', maxBytes: 512),
    seniority: _optionalText(object, 'seniority', maxBytes: 256),
    candidateBackground: _optionalText(
      object,
      'candidate_background',
      maxBytes: 16 * 1024,
    ),
    resumeRef: _optionalText(object, 'resume_ref', maxBytes: 16 * 1024),
    practiceFocus: _optionalText(object, 'practice_focus', maxBytes: 8 * 1024),
  );
  _requireJobTargetInput(input, invalidResponse: true);
  return input;
}

JobTargetAnalysis _jobTargetAnalysis(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'input_version',
      'analysis_version',
      'attempt',
      'status',
      'started_at',
    },
    optional: const <String>{
      'candidate',
      'stable_error_category',
      'finished_at',
    },
  );
  final status = _jobTargetAnalysisStatus(object['status']);
  final candidate = object.containsKey('candidate')
      ? _jobTargetCandidate(object['candidate'])
      : null;
  final stableError = _optionalPatternText(
    object,
    'stable_error_category',
    RegExp(r'^[a-z][a-z0-9_]{0,63}$'),
  );
  final finishedAt = object.containsKey('finished_at')
      ? _dateTime(object['finished_at'])
      : null;
  if (switch (status) {
    JobTargetAnalysisStatus.running =>
      candidate != null || stableError != null || finishedAt != null,
    JobTargetAnalysisStatus.succeeded =>
      candidate == null || stableError != null || finishedAt == null,
    JobTargetAnalysisStatus.failed =>
      candidate != null || stableError == null || finishedAt == null,
  }) {
    throw _invalidResponse();
  }
  final startedAt = _dateTime(object['started_at']);
  if (finishedAt != null && finishedAt.isBefore(startedAt)) {
    throw _invalidResponse();
  }
  return JobTargetAnalysis(
    inputVersion: _version(object['input_version']),
    analysisVersion: _version(object['analysis_version']),
    attempt: _version(object['attempt']),
    status: status,
    candidate: candidate,
    stableErrorCategory: stableError,
    startedAt: startedAt,
    finishedAt: finishedAt,
  );
}

JobTargetConfirmation _jobTargetConfirmation(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'input_version',
      'analysis_version',
      'confirmation_version',
      'candidate',
      'confirmed_at',
    },
  );
  return JobTargetConfirmation(
    inputVersion: _version(object['input_version']),
    analysisVersion: _version(object['analysis_version']),
    confirmationVersion: _version(object['confirmation_version']),
    candidate: _jobTargetCandidate(object['candidate']),
    confirmedAt: _dateTime(object['confirmed_at']),
  );
}

JobTargetCandidate _jobTargetCandidate(Object? value) {
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
  );
  if (utf8.encode(jsonEncode(object)).length > 64 * 1024) {
    throw _invalidResponse();
  }
  final source = _jobTargetSource(object['source']);
  final generalAdviceOnly = object['general_advice_only'];
  if (generalAdviceOnly is! bool ||
      generalAdviceOnly != (source == JobTargetSource.quickStart)) {
    throw _invalidResponse();
  }
  final recommendationObject = _object(
    object['catalog_recommendation'],
    required: const <String>{
      'scenario_definition_id',
      'scenario_definition_version',
      'selected_role_ids',
      'practice_option_id',
      'practice_option_version',
    },
  );
  final selectedRoleIds = _resourceIdList(
    recommendationObject['selected_role_ids'],
    min: 1,
    max: 1,
  );
  return JobTargetCandidate(
    source: source,
    generalAdviceOnly: generalAdviceOnly,
    jobTitle: _text(object['job_title'], maxBytes: 512),
    seniority: _text(object['seniority'], maxBytes: 256),
    responsibilities: _candidateItems(object['responsibilities']),
    coreSkills: _candidateItems(object['core_skills']),
    communicationFocus: _candidateItems(object['communication_focus']),
    practiceGoals: _candidateItems(object['practice_goals']),
    scopeNotice: _text(object['scope_notice'], maxBytes: 2048),
    catalogRecommendation: JobTargetCatalogRecommendation(
      scenarioDefinitionId: _resourceId(
        recommendationObject['scenario_definition_id'],
      ),
      scenarioDefinitionVersion: _version(
        recommendationObject['scenario_definition_version'],
      ),
      selectedRoleIds: selectedRoleIds,
      practiceOptionId: _resourceId(recommendationObject['practice_option_id']),
      practiceOptionVersion: _version(
        recommendationObject['practice_option_version'],
      ),
    ),
  );
}

JobPreparationProfile _decodeJobPreparationProfile(
  String body, {
  required String expectedBackground,
  required String expectedJobTargetId,
  required int expectedConfirmationVersion,
}) {
  final object = _object(
    _decodeJson(body),
    required: const <String>{
      'preparation_profile_id',
      'user_id',
      'background_summary',
      'job_target_id',
      'job_target_confirmation_version',
      'version',
      'updated_at',
    },
    optional: const <String>{'resume_ref', 'job_description_ref'},
  );
  final background = _text(object['background_summary'], maxBytes: 64 * 1024);
  final targetId = _resourceId(object['job_target_id']);
  final confirmationVersion = _version(
    object['job_target_confirmation_version'],
  );
  if (background != expectedBackground ||
      targetId != expectedJobTargetId ||
      confirmationVersion != expectedConfirmationVersion ||
      object.containsKey('resume_ref') ||
      object.containsKey('job_description_ref')) {
    throw _invalidResponse();
  }
  return JobPreparationProfile(
    id: _resourceId(object['preparation_profile_id']),
    userId: _resourceId(object['user_id']),
    backgroundSummary: background,
    jobTargetId: targetId,
    jobTargetConfirmationVersion: confirmationVersion,
    version: _version(object['version']),
    updatedAt: _dateTime(object['updated_at']),
  );
}

JobPreparationSnapshot _decodeJobPreparationSnapshot(String body) {
  return _jobPreparationSnapshot(_decodeJson(body));
}

JobPreparationSnapshot _jobPreparationSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'preparation_snapshot_id',
      'source_profile_id',
      'source_version',
      'source_job_target_id',
      'source_job_target_confirmation_version',
      'job_target_input_snapshot',
      'job_target_candidate_snapshot',
      'background_snapshot',
      'created_at',
    },
    optional: const <String>{'resume_snapshot', 'job_description_snapshot'},
  );
  if (object.containsKey('resume_snapshot') ||
      object.containsKey('job_description_snapshot')) {
    throw _invalidResponse();
  }
  final input = _jobTargetInput(object['job_target_input_snapshot']);
  final candidate = _jobTargetCandidate(
    object['job_target_candidate_snapshot'],
  );
  if (input.source != candidate.source) {
    throw _invalidResponse();
  }
  return JobPreparationSnapshot(
    id: _resourceId(object['preparation_snapshot_id']),
    sourceProfileId: _resourceId(object['source_profile_id']),
    sourceVersion: _version(object['source_version']),
    sourceJobTargetId: _resourceId(object['source_job_target_id']),
    sourceJobTargetConfirmationVersion: _version(
      object['source_job_target_confirmation_version'],
    ),
    jobTargetInput: input,
    jobTargetCandidate: candidate,
    backgroundSnapshot: _text(
      object['background_snapshot'],
      maxBytes: 64 * 1024,
    ),
    createdAt: _dateTime(object['created_at']),
  );
}

JobPracticePlanPreview _decodeJobPracticePlan(String body) {
  final object = _object(
    _decodeJson(body),
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
      'preparation_snapshot',
      'catalog_snapshot',
      'session_policy',
      'practice_focuses',
      'plan_revision',
      'practice_plan_status',
      'created_at',
      'updated_at',
    },
  );
  final catalog = _jobPlanCatalog(object['catalog_snapshot']);
  final selectedRoleIds = _resourceIdList(
    object['selected_role_ids'],
    min: 1,
    max: 1,
  );
  final preparationSnapshot = _jobPreparationSnapshot(
    object['preparation_snapshot'],
  );
  final policy = _jobSessionPolicy(object['session_policy']);
  final createdAt = _dateTime(object['created_at']);
  final updatedAt = _dateTime(object['updated_at']);
  if (_resourceId(object['scenario_definition_id']) != catalog.scenario.id ||
      _version(object['scenario_definition_version']) !=
          catalog.scenario.version ||
      _enumText(object['scenario_type'], const <String>{'INTERVIEW'}) !=
          catalog.scenario.type ||
      _enumText(object['scenario_model'], const <String>{
            'PROJECT_EXPERIENCE_DEEP_DIVE',
          }) !=
          catalog.scenario.model ||
      _resourceId(object['scenario_config_id']) != catalog.config.id ||
      _version(object['scenario_config_version']) != catalog.config.version ||
      _resourceId(object['preparation_profile_id']) !=
          preparationSnapshot.sourceProfileId ||
      selectedRoleIds.single != catalog.selectedRole.id ||
      catalog.practiceOption.scenarioId != catalog.scenario.id ||
      updatedAt.isBefore(createdAt)) {
    throw _invalidResponse();
  }
  return JobPracticePlanPreview(
    id: _resourceId(object['practice_plan_id']),
    userId: _resourceId(object['user_id']),
    context: AgentPracticeContext(
      threadId: _resourceId(object['agent_thread_id']),
      matterId: _resourceId(object['matter_id']),
    ),
    preparationProfileId: preparationSnapshot.sourceProfileId,
    preparationSnapshot: preparationSnapshot,
    catalog: catalog,
    sessionPolicy: policy,
    practiceFocuses: _jobObjectives(object['practice_focuses']),
    revision: _version(object['plan_revision']),
    status: _enumText(object['practice_plan_status'], const <String>{'ready'}),
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

JobPlanCatalog _jobPlanCatalog(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scenario_definition',
      'scenario_config',
      'selected_roles',
      'practice_option',
    },
  );
  final scenarioObject = _object(
    object['scenario_definition'],
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
  final configObject = _object(
    object['scenario_config'],
    required: const <String>{
      'scenario_config_id',
      'scenario_definition_id',
      'config_type',
      'scenario_model',
      'version',
      'job_title',
      'job_description',
      'prompt_model',
    },
  );
  final config = PreparationScenarioConfig(
    id: _resourceId(configObject['scenario_config_id']),
    scenarioId: _resourceId(configObject['scenario_definition_id']),
    type: _enumText(configObject['config_type'], const <String>{'INTERVIEW'}),
    model: _enumText(configObject['scenario_model'], const <String>{
      'PROJECT_EXPERIENCE_DEEP_DIVE',
    }),
    version: _version(configObject['version']),
    jobTitle: _text(configObject['job_title']),
    jobDescription: _text(configObject['job_description']),
    prompt: _scenarioPrompt(configObject['prompt_model']),
  );
  final scenario = PreparationScenario(
    id: _resourceId(scenarioObject['scenario_definition_id']),
    type: _enumText(scenarioObject['scenario_type'], const <String>{
      'INTERVIEW',
    }),
    model: _enumText(scenarioObject['scenario_model'], const <String>{
      'PROJECT_EXPERIENCE_DEEP_DIVE',
    }),
    name: _text(scenarioObject['name']),
    summary: config.prompt.publicSceneBrief,
    version: _version(scenarioObject['version']),
    status: _enumText(scenarioObject['status'], const <String>{'active'}),
  );
  _resourceId(scenarioObject['turn_policy_ref']);
  _resourceId(scenarioObject['session_policy_ref']);
  final rolesValue = object['selected_roles'];
  if (rolesValue is! List<Object?> || rolesValue.length != 1) {
    throw _invalidResponse();
  }
  final role = _preparationRole(rolesValue.single);
  final option = _preparationOption(object['practice_option']);
  if (config.scenarioId != scenario.id ||
      config.type != scenario.type ||
      config.model != scenario.model ||
      role.scenarioId != scenario.id ||
      option.scenarioId != scenario.id ||
      (option.type == PreparationOptionType.focus &&
          option.roleId != role.id) ||
      (option.type == PreparationOptionType.fullSimulation &&
          option.roleId != null)) {
    throw _invalidResponse();
  }
  return JobPlanCatalog(
    scenario: scenario,
    config: config,
    selectedRole: role,
    practiceOption: option,
  );
}

PreparationRole _preparationRole(Object? value) {
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
  return PreparationRole(
    id: _resourceId(object['role_definition_id']),
    scenarioId: _resourceId(object['scenario_definition_id']),
    type: _text(object['role_type'], maxBytes: 128),
    displayName: _text(object['display_name']),
    responsibilities: _text(object['responsibilities']),
    style: _text(object['style']),
    focusAreas: _nonEmptyTextList(object['focus_areas'], max: 100),
    version: _version(object['version']),
    voiceConfigRef: _optionalText(object, 'voice_config_ref'),
  );
}

PreparationOption _preparationOption(Object? value) {
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
  final type = switch (_enumText(object['practice_option_type'], const <String>{
    'FULL_SIMULATION',
    'FOCUS',
  })) {
    'FULL_SIMULATION' => PreparationOptionType.fullSimulation,
    'FOCUS' => PreparationOptionType.focus,
    _ => throw _invalidResponse(),
  };
  final roleId = _optionalResourceId(object, 'role_definition_id');
  if ((type == PreparationOptionType.focus) != (roleId != null)) {
    throw _invalidResponse();
  }
  return PreparationOption(
    id: _resourceId(object['practice_option_id']),
    scenarioId: _resourceId(object['scenario_definition_id']),
    type: type,
    displayName: _text(object['display_name']),
    version: _version(object['version']),
    roleId: roleId,
  );
}

JobSessionPolicy _jobSessionPolicy(Object? value) {
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
  final duration = _version(object['suggested_duration_seconds']);
  final minimum = _version(object['min_effective_turns']);
  final maximum = _version(object['max_effective_turns']);
  final checkpoint = _version(object['coverage_checkpoint_turn']);
  final followUps = object['max_follow_ups_per_question'];
  if (minimum > checkpoint ||
      checkpoint > maximum ||
      followUps is! int ||
      followUps < 0) {
    throw _invalidResponse();
  }
  return JobSessionPolicy(
    suggestedDurationSeconds: duration,
    minEffectiveTurns: minimum,
    maxEffectiveTurns: maximum,
    coverageCheckpointTurn: checkpoint,
    maxFollowUpsPerQuestion: followUps,
    targetObjectives: _jobObjectives(object['target_objectives']),
    earlyCompletionRule: _patternText(
      object['early_completion_rule'],
      RegExp(r'^[A-Z][A-Z0-9_]*$'),
    ),
  );
}

List<JobPracticeObjective> _jobObjectives(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 100) {
    throw _invalidResponse();
  }
  final ids = <String>{};
  return List<JobPracticeObjective>.unmodifiable(
    value.map((raw) {
      final object = _object(
        raw,
        required: const <String>{'objective_id', 'description'},
      );
      final id = _patternText(
        object['objective_id'],
        RegExp(r'^[a-z][a-z0-9_]*$'),
      );
      if (!ids.add(id)) {
        throw _invalidResponse();
      }
      return JobPracticeObjective(
        id: id,
        description: _text(object['description']),
      );
    }),
  );
}

PreparationPracticeBootstrap _decodeJobPracticeBootstrap(
  String body, {
  required JobPracticePlanPreview expectedPlan,
}) {
  final root = _object(
    _decodeJson(body),
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
  final scenarioType = _enumText(sessionObject['scenario_type'], const <String>{
    'INTERVIEW',
  });
  final scenarioModel = _enumText(
    sessionObject['scenario_model'],
    const <String>{'PROJECT_EXPERIENCE_DEEP_DIVE'},
  );
  if (_resourceId(sessionObject['practice_plan_id']) != expectedPlan.id ||
      scenarioType != expectedPlan.catalog.scenario.type ||
      scenarioModel != expectedPlan.catalog.scenario.model ||
      _enumText(sessionObject['practice_session_status'], const <String>{
            'starting',
          }) !=
          'starting' ||
      sessionObject.containsKey('started_at') ||
      sessionObject.containsKey('ended_at') ||
      sessionObject.containsKey('end_reason')) {
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
  final config = _configSnapshot(snapshotObject['scenario_config_snapshot']);
  final scenario = _scenarioSnapshot(
    snapshotObject['scenario_definition_snapshot'],
    summary: config.prompt.publicSceneBrief,
  );
  final preparation = _jobPreparationSnapshot(
    snapshotObject['preparation_snapshot'],
  );
  final option = _preparationOption(snapshotObject['practice_option']);
  final policy = _jobSessionPolicy(snapshotObject['session_policy']);
  final focuses = _jobObjectives(snapshotObject['practice_focuses']);
  if (_resourceId(snapshotObject['snapshot_id']) != snapshotId ||
      _resourceId(snapshotObject['practice_session_id']) != sessionId ||
      _version(snapshotObject['plan_revision']) != expectedPlan.revision ||
      _enumText(snapshotObject['scenario_type'], const <String>{'INTERVIEW'}) !=
          scenarioType ||
      _enumText(snapshotObject['scenario_model'], const <String>{
            'PROJECT_EXPERIENCE_DEEP_DIVE',
          }) !=
          scenarioModel ||
      !_sameScenario(scenario, expectedPlan.catalog.scenario) ||
      !_sameConfig(config, expectedPlan.catalog.config) ||
      !_samePreparationSnapshot(
        preparation,
        expectedPlan.preparationSnapshot,
      ) ||
      !_sameOption(option, expectedPlan.catalog.practiceOption) ||
      !_samePolicy(policy, expectedPlan.sessionPolicy) ||
      !_sameObjectives(focuses, expectedPlan.practiceFocuses)) {
    throw _invalidResponse();
  }
  _validateParticipants(
    snapshotObject['participants'],
    sessionId: sessionId,
    expectedPlan: expectedPlan,
  );
  return PreparationPracticeBootstrap(
    session: PreparationPracticeSession(
      id: sessionId,
      planId: expectedPlan.id,
      scenarioType: scenarioType,
      scenarioModel: scenarioModel,
      snapshotId: snapshotId,
      status: 'starting',
      version: _version(sessionObject['session_version']),
      createdAt: _dateTime(sessionObject['created_at']),
    ),
    preparationSnapshotId: preparation.id,
    maxEffectiveTurns: policy.maxEffectiveTurns,
  );
}

PreparationScenario _scenarioSnapshot(
  Object? value, {
  required String summary,
}) {
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
  _resourceId(object['turn_policy_ref']);
  _resourceId(object['session_policy_ref']);
  return PreparationScenario(
    id: _resourceId(object['scenario_definition_id']),
    type: _enumText(object['scenario_type'], const <String>{'INTERVIEW'}),
    model: _enumText(object['scenario_model'], const <String>{
      'PROJECT_EXPERIENCE_DEEP_DIVE',
    }),
    name: _text(object['name']),
    summary: summary,
    version: _version(object['version']),
    status: _enumText(object['status'], const <String>{'active'}),
  );
}

PreparationScenarioConfig _configSnapshot(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scenario_config_id',
      'scenario_definition_id',
      'config_type',
      'scenario_model',
      'version',
      'job_title',
      'job_description',
      'prompt_model',
    },
  );
  return PreparationScenarioConfig(
    id: _resourceId(object['scenario_config_id']),
    scenarioId: _resourceId(object['scenario_definition_id']),
    type: _enumText(object['config_type'], const <String>{'INTERVIEW'}),
    model: _enumText(object['scenario_model'], const <String>{
      'PROJECT_EXPERIENCE_DEEP_DIVE',
    }),
    version: _version(object['version']),
    jobTitle: _text(object['job_title']),
    jobDescription: _text(object['job_description']),
    prompt: _scenarioPrompt(object['prompt_model']),
  );
}

PreparationScenarioPrompt _scenarioPrompt(Object? value) {
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
    throw _invalidResponse();
  }
  return PreparationScenarioPrompt(
    publicSceneBrief: _text(object['public_scene_brief']),
    practiceGoal: _text(object['practice_goal']),
    userRole: _text(object['user_role']),
    aiRole: _text(object['ai_role']),
    personaSummary: _text(object['persona_summary']),
    focusAreas: _nonEmptyTextList(object['focus_areas'], max: 100),
    turnBlueprints: _nonEmptyTextList(object['turn_blueprints'], max: 100),
    suggestedDurationSeconds: duration,
  );
}

void _validateParticipants(
  Object? value, {
  required String sessionId,
  required JobPracticePlanPreview expectedPlan,
}) {
  if (value is! List<Object?> || value.length < 2 || value.length > 16) {
    throw _invalidResponse();
  }
  final participantIds = <String>{};
  final orders = <int>{};
  var interviewerCount = 0;
  var candidateCount = 0;
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
        !participantIds.add(_resourceId(object['practice_participant_id'])) ||
        !orders.add(_version(object['participant_order']))) {
      throw _invalidResponse();
    }
    final subject = _object(
      object['subject_ref'],
      required: const <String>{'namespace', 'subject_id'},
    );
    final participantRole = _enumText(
      object['participant_role'],
      const <String>{'FACILITATOR', 'LEARNER', 'INTERVIEWER', 'CANDIDATE'},
    );
    if (participantRole == 'LEARNER' || participantRole == 'CANDIDATE') {
      candidateCount++;
      if (object.containsKey('role_definition_id') ||
          object.containsKey('role_snapshot') ||
          _text(subject['namespace']) != 'speakup.user' ||
          _resourceId(subject['subject_id']) != expectedPlan.userId) {
        throw _invalidResponse();
      }
      continue;
    }
    interviewerCount++;
    final role = _preparationRole(object['role_snapshot']);
    if (_resourceId(object['role_definition_id']) !=
            expectedPlan.catalog.selectedRole.id ||
        !_sameRole(role, expectedPlan.catalog.selectedRole)) {
      throw _invalidResponse();
    }
    _text(subject['namespace']);
    _resourceId(subject['subject_id']);
  }
  if (interviewerCount != 1 || candidateCount != 1) {
    throw _invalidResponse();
  }
}

bool _sameScenario(PreparationScenario left, PreparationScenario right) {
  return left.id == right.id &&
      left.type == right.type &&
      left.model == right.model &&
      left.name == right.name &&
      left.summary == right.summary &&
      left.version == right.version &&
      left.status == right.status;
}

bool _sameConfig(
  PreparationScenarioConfig left,
  PreparationScenarioConfig right,
) {
  return left.id == right.id &&
      left.scenarioId == right.scenarioId &&
      left.type == right.type &&
      left.model == right.model &&
      left.version == right.version &&
      left.jobTitle == right.jobTitle &&
      left.jobDescription == right.jobDescription &&
      _samePrompt(left.prompt, right.prompt);
}

bool _sameRole(PreparationRole left, PreparationRole right) {
  return left.id == right.id &&
      left.scenarioId == right.scenarioId &&
      left.type == right.type &&
      left.displayName == right.displayName &&
      left.responsibilities == right.responsibilities &&
      left.style == right.style &&
      left.version == right.version &&
      left.voiceConfigRef == right.voiceConfigRef &&
      _sameStrings(left.focusAreas, right.focusAreas);
}

bool _samePrompt(
  PreparationScenarioPrompt left,
  PreparationScenarioPrompt right,
) {
  return left.publicSceneBrief == right.publicSceneBrief &&
      left.practiceGoal == right.practiceGoal &&
      left.userRole == right.userRole &&
      left.aiRole == right.aiRole &&
      left.personaSummary == right.personaSummary &&
      left.suggestedDurationSeconds == right.suggestedDurationSeconds &&
      _sameStrings(left.focusAreas, right.focusAreas) &&
      _sameStrings(left.turnBlueprints, right.turnBlueprints);
}

bool _sameOption(PreparationOption left, PreparationOption right) {
  return left.id == right.id &&
      left.scenarioId == right.scenarioId &&
      left.type == right.type &&
      left.displayName == right.displayName &&
      left.version == right.version &&
      left.roleId == right.roleId;
}

bool _samePreparationSnapshot(
  JobPreparationSnapshot left,
  JobPreparationSnapshot right,
) {
  return left.id == right.id &&
      left.sourceProfileId == right.sourceProfileId &&
      left.sourceVersion == right.sourceVersion &&
      left.sourceJobTargetId == right.sourceJobTargetId &&
      left.sourceJobTargetConfirmationVersion ==
          right.sourceJobTargetConfirmationVersion &&
      left.backgroundSnapshot == right.backgroundSnapshot &&
      left.createdAt == right.createdAt &&
      left.jobTargetInput == right.jobTargetInput &&
      _sameCandidate(left.jobTargetCandidate, right.jobTargetCandidate);
}

bool _sameCandidate(JobTargetCandidate left, JobTargetCandidate right) {
  final leftRecommendation = left.catalogRecommendation;
  final rightRecommendation = right.catalogRecommendation;
  return left.source == right.source &&
      left.generalAdviceOnly == right.generalAdviceOnly &&
      left.jobTitle == right.jobTitle &&
      left.seniority == right.seniority &&
      _sameStrings(left.responsibilities, right.responsibilities) &&
      _sameStrings(left.coreSkills, right.coreSkills) &&
      _sameStrings(left.communicationFocus, right.communicationFocus) &&
      _sameStrings(left.practiceGoals, right.practiceGoals) &&
      left.scopeNotice == right.scopeNotice &&
      leftRecommendation.scenarioDefinitionId ==
          rightRecommendation.scenarioDefinitionId &&
      leftRecommendation.scenarioDefinitionVersion ==
          rightRecommendation.scenarioDefinitionVersion &&
      _sameStrings(
        leftRecommendation.selectedRoleIds,
        rightRecommendation.selectedRoleIds,
      ) &&
      leftRecommendation.practiceOptionId ==
          rightRecommendation.practiceOptionId &&
      leftRecommendation.practiceOptionVersion ==
          rightRecommendation.practiceOptionVersion;
}

bool _samePolicy(JobSessionPolicy left, JobSessionPolicy right) {
  return left.suggestedDurationSeconds == right.suggestedDurationSeconds &&
      left.minEffectiveTurns == right.minEffectiveTurns &&
      left.maxEffectiveTurns == right.maxEffectiveTurns &&
      left.coverageCheckpointTurn == right.coverageCheckpointTurn &&
      left.maxFollowUpsPerQuestion == right.maxFollowUpsPerQuestion &&
      left.earlyCompletionRule == right.earlyCompletionRule &&
      _sameObjectives(left.targetObjectives, right.targetObjectives);
}

bool _sameObjectives(
  List<JobPracticeObjective> left,
  List<JobPracticeObjective> right,
) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index].id != right[index].id ||
        left[index].description != right[index].description) {
      return false;
    }
  }
  return true;
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

Object? _decodeJson(String body) {
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

JobTargetSource _jobTargetSource(Object? value) {
  return switch (_enumText(value, const <String>{
    'job_description',
    'quick_start',
  })) {
    'job_description' => JobTargetSource.jobDescription,
    'quick_start' => JobTargetSource.quickStart,
    _ => throw _invalidResponse(),
  };
}

JobTargetStage _jobTargetStage(Object? value) {
  final wire = _enumText(value, const <String>{
    'draft',
    'parsing',
    'analysis_failed',
    'awaiting_confirmation',
    'confirmed',
    'discarded',
  });
  return JobTargetStage.values.singleWhere(
    (candidate) => candidate.wireValue == wire,
  );
}

JobTargetAnalysisStatus _jobTargetAnalysisStatus(Object? value) {
  final wire = _enumText(value, const <String>{
    'running',
    'succeeded',
    'failed',
  });
  return JobTargetAnalysisStatus.values.singleWhere(
    (candidate) => candidate.wireValue == wire,
  );
}

String _resourceId(Object? value) {
  return _text(value, maxBytes: 128);
}

String? _optionalResourceId(Map<String, Object?> object, String key) {
  return object.containsKey(key) ? _resourceId(object[key]) : null;
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

String? _optionalText(
  Map<String, Object?> object,
  String key, {
  int maxBytes = 256 * 1024,
}) {
  return object.containsKey(key)
      ? _text(object[key], maxBytes: maxBytes)
      : null;
}

String _patternText(Object? value, RegExp pattern) {
  final result = _text(value, maxBytes: 2048);
  if (!pattern.hasMatch(result)) {
    throw _invalidResponse();
  }
  return result;
}

String? _optionalPatternText(
  Map<String, Object?> object,
  String key,
  RegExp pattern,
) {
  return object.containsKey(key) ? _patternText(object[key], pattern) : null;
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
  final parsed = DateTime.tryParse(value);
  if (parsed == null) {
    throw _invalidResponse();
  }
  return parsed;
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
  return List<String>.unmodifiable(result);
}

List<String> _candidateItems(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 20) {
    throw _invalidResponse();
  }
  final result = value
      .map((item) {
        final text = _text(item, maxBytes: 8 * 1024);
        if (text.runes.length > 2048) {
          throw _invalidResponse();
        }
        return text;
      })
      .toList(growable: false);
  if (result.toSet().length != result.length) {
    throw _invalidResponse();
  }
  return List<String>.unmodifiable(result);
}

List<String> _nonEmptyTextList(Object? value, {required int max}) {
  if (value is! List<Object?> || value.isEmpty || value.length > max) {
    throw _invalidResponse();
  }
  final result = value.map(_text).toList(growable: false);
  if (result.toSet().length != result.length) {
    throw _invalidResponse();
  }
  return List<String>.unmodifiable(result);
}

Map<String, Object?> _jobTargetInputJson(JobTargetInput input) {
  return <String, Object?>{
    'source': input.source.wireValue,
    if (input.jobTitle != null) 'job_title': input.jobTitle,
    if (input.jobDescription != null) 'job_description': input.jobDescription,
    if (input.company != null) 'company': input.company,
    if (input.seniority != null) 'seniority': input.seniority,
    if (input.candidateBackground != null)
      'candidate_background': input.candidateBackground,
    if (input.resumeRef != null) 'resume_ref': input.resumeRef,
    if (input.practiceFocus != null) 'practice_focus': input.practiceFocus,
  };
}

Map<String, Object?> _jobTargetCandidateJson(JobTargetCandidate candidate) {
  return <String, Object?>{
    'source': candidate.source.wireValue,
    'general_advice_only': candidate.generalAdviceOnly,
    'job_title': candidate.jobTitle,
    'seniority': candidate.seniority,
    'responsibilities': candidate.responsibilities,
    'core_skills': candidate.coreSkills,
    'communication_focus': candidate.communicationFocus,
    'practice_goals': candidate.practiceGoals,
    'scope_notice': candidate.scopeNotice,
    'catalog_recommendation': <String, Object?>{
      'scenario_definition_id':
          candidate.catalogRecommendation.scenarioDefinitionId,
      'scenario_definition_version':
          candidate.catalogRecommendation.scenarioDefinitionVersion,
      'selected_role_ids': candidate.catalogRecommendation.selectedRoleIds,
      'practice_option_id': candidate.catalogRecommendation.practiceOptionId,
      'practice_option_version':
          candidate.catalogRecommendation.practiceOptionVersion,
    },
  };
}

String? _decodeErrorCode(String body) {
  try {
    final root = jsonDecode(body);
    if (root is! Map<String, Object?> ||
        root['error'] is! Map<String, Object?>) {
      return null;
    }
    final value = (root['error'] as Map<String, Object?>)['code'];
    return value is String && value.length <= 128 ? value : null;
  } on FormatException {
    return null;
  }
}

Never _invalidResponse([JobPreparationOperationStage? stage]) {
  throw JobPreparationException(
    kind: JobPreparationFailureKind.invalidResponse,
    stage: stage,
  );
}

void _requireContext(AgentPracticeContext context) {
  _requireResourceId(context.threadId);
  _requireResourceId(context.matterId);
}

void _requireVersion(int value) {
  if (value < 1) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
    );
  }
}

void _requireResourceId(String value) {
  if (value.isEmpty ||
      value.length > 128 ||
      value.trim() != value ||
      value.contains('\u0000')) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
    );
  }
}

void _requireText(String value, {required int maxBytes}) {
  if (value.isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
    );
  }
}

void _requireJobTargetInput(
  JobTargetInput input, {
  bool invalidResponse = false,
}) {
  void invalid() {
    if (invalidResponse) {
      throw _invalidResponse();
    }
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: JobPreparationOperationStage.target,
    );
  }

  bool validOptional(String? value, int maxBytes) {
    return value == null ||
        (value.isNotEmpty &&
            value.trim() == value &&
            !value.contains('\u0000') &&
            utf8.encode(value).length <= maxBytes);
  }

  if (!validOptional(input.jobTitle, 512) ||
      !validOptional(input.jobDescription, 64 * 1024) ||
      !validOptional(input.company, 512) ||
      !validOptional(input.seniority, 256) ||
      !validOptional(input.candidateBackground, 16 * 1024) ||
      !validOptional(input.resumeRef, 16 * 1024) ||
      !validOptional(input.practiceFocus, 8 * 1024) ||
      (input.source == JobTargetSource.jobDescription &&
          input.jobDescription == null) ||
      (input.source == JobTargetSource.quickStart &&
          (input.jobTitle == null || input.jobDescription != null))) {
    invalid();
  }
}

void _requireJobTargetCandidate(JobTargetCandidate candidate) {
  try {
    _jobTargetCandidate(_jobTargetCandidateJson(candidate));
  } on JobPreparationException {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: JobPreparationOperationStage.confirmation,
    );
  }
}

void _requireIdempotencyKey(String value) {
  if (value.length < 8 ||
      value.length > 128 ||
      value.trim() != value ||
      value.contains('\u0000')) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
    );
  }
}
