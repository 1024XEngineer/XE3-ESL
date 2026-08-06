import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_wire_codec.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';
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
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    _requireProfileInput(input);
    final response = await _request(
      method: 'POST',
      path: '/v1/preparation-profiles',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'background_summary': input.backgroundSummary,
        'resume_id': ?input.resumeId,
        'resume_revision': ?input.resumeRevision,
        'job_description_ref': ?input.jobDescriptionRef,
        'job_target_id': ?input.jobTargetId,
        'job_target_confirmation_version': ?input.jobTargetConfirmationVersion,
      },
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.profile,
    );
    return _decodeResponse(
      response,
      stage: JobPreparationOperationStage.profile,
      decode: (body) {
        final profile = decodePreparationProfileBody(body);
        if (profile.backgroundSummary != input.backgroundSummary ||
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
      decode: (body) => decodePreparationSnapshotBody(
        body,
        decodeJobTargetInput: _jobTargetInput,
        decodeJobTargetCandidate: _jobTargetCandidate,
      ),
    );
    if (snapshot.sourceProfileId != profileId ||
        snapshot.sourceVersion != sourceVersion) {
      throw _invalidResponse(JobPreparationOperationStage.snapshot);
    }
    return snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    _requirePlanInput(input);
    final response = await _request(
      method: 'POST',
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
      acceptedStatuses: const <int>{HttpStatus.created},
      stage: JobPreparationOperationStage.plan,
    );
    final plan = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.plan,
      decode: _decodePracticePlan,
    );
    if (plan.sourceThreadId != input.sourceThreadId ||
        plan.goalSnapshot?.id != input.goalId ||
        plan.preparationSnapshot.id != input.preparationSnapshotId ||
        plan.sceneSelection.scene.id != input.sceneId ||
        plan.sceneSelection.scene.version != input.sceneVersion ||
        !_sameStrings(
          plan.sceneSelection.selectedRoleIds,
          input.selectedRoleIds,
        ) ||
        plan.sceneSelection.practiceOptionId != input.practiceOptionId ||
        (input.maxEffectiveTurns != null &&
            plan.sessionPolicy.maxEffectiveTurns != input.maxEffectiveTurns) ||
        (input.ieltsSelection == null
            ? plan.ieltsAssignment != null
            : !(plan.ieltsAssignment?.matchesSelection(input.ieltsSelection!) ??
                  false)) ||
        plan.status != PracticePlanStatus.ready) {
      throw _invalidResponse(JobPreparationOperationStage.plan);
    }
    return plan;
  }

  @override
  Future<PracticePlan> getPlan(String planId) async {
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
      decode: _decodePracticePlan,
    );
    if (plan.id != planId) {
      throw _invalidResponse(JobPreparationOperationStage.plan);
    }
    return plan;
  }

  @override
  Future<PracticePlan> revisePlan({
    required String planId,
    required RevisePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    _requireResourceId(planId);
    _requireRevisePlanInput(input);
    final response = await _request(
      method: 'PUT',
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_revision': input.expectedPlanRevision,
        'selected_role_ids': input.selectedRoleIds,
        'practice_option_id': input.practiceOptionId,
        'max_effective_turns': input.maxEffectiveTurns,
      },
      acceptedStatuses: const <int>{HttpStatus.ok},
      stage: JobPreparationOperationStage.plan,
    );
    final plan = _decodeResponse(
      response,
      stage: JobPreparationOperationStage.plan,
      decode: _decodePracticePlan,
    );
    if (plan.id != planId ||
        plan.revision <= input.expectedPlanRevision ||
        !_sameStrings(
          plan.sceneSelection.selectedRoleIds,
          input.selectedRoleIds,
        ) ||
        plan.sceneSelection.practiceOptionId != input.practiceOptionId ||
        plan.sessionPolicy.maxEffectiveTurns != input.maxEffectiveTurns ||
        plan.status != PracticePlanStatus.ready) {
      throw _invalidResponse(JobPreparationOperationStage.plan);
    }
    return plan;
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
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.invalidRequest,
        stage: JobPreparationOperationStage.session,
      );
    }
    final response = await _request(
      method: 'POST',
      path:
          '/v1/practice-plans/${Uri.encodeComponent(plan.id)}'
          '/practice-sessions',
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_revision': input.expectedPlanRevision,
        'user_confirmed': input.userConfirmed,
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
      'scene_id',
      'scene_version',
      'selected_role_ids',
      'practice_option_id',
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
      sceneId: _resourceId(recommendationObject['scene_id']),
      sceneVersion: _version(recommendationObject['scene_version']),
      selectedRoleIds: selectedRoleIds,
      practiceOptionId: _resourceId(recommendationObject['practice_option_id']),
    ),
  );
}

PracticePlan _decodePracticePlan(String body) => decodePracticePlanBody(
  body,
  decodeJobTargetInput: _jobTargetInput,
  decodeJobTargetCandidate: _jobTargetCandidate,
);

RoleDefinition _preparationRole(Object? value) {
  try {
    return decodeRoleDefinition(value);
  } on SceneWireFormatException {
    throw _invalidResponse();
  }
}

PreparationPracticeBootstrap _decodeJobPracticeBootstrap(
  String body, {
  required PracticePlan expectedPlan,
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
  final practiceExperience =
      PracticeExperience.fromWireValue(
        _enumText(sessionObject['practice_experience'], const <String>{
          'INTERVIEW',
        }),
      ) ??
      (throw _invalidResponse());
  final sceneCategory =
      SceneCategory.fromWireValue(
        _enumText(sessionObject['scene_category'], const <String>{
          'INTERVIEW_RECRUITER',
          'INTERVIEW_BEHAVIORAL',
          'INTERVIEW_PROFESSIONAL',
          'INTERVIEW_HIRING_MANAGER',
          'INTERVIEW_CUSTOM',
        }),
      ) ??
      (throw _invalidResponse());
  final practiceMode =
      PracticeMode.fromWireValue(
        _enumText(sessionObject['practice_mode'], const <String>{
          'FULL_SIMULATION',
          'FOCUS',
        }),
      ) ??
      (throw _invalidResponse());
  final expectedOption = expectedPlan.sceneSelection.scene.practiceOptions
      .where(
        (option) => option.id == expectedPlan.sceneSelection.practiceOptionId,
      )
      .firstOrNull;
  final status = _enumText(
    sessionObject['practice_session_status'],
    const <String>{'starting', 'in_progress'},
  );
  if (_resourceId(sessionObject['practice_plan_id']) != expectedPlan.id ||
      _version(sessionObject['plan_revision']) != expectedPlan.revision ||
      practiceExperience != expectedPlan.sceneSelection.scene.experience ||
      sceneCategory != expectedPlan.sceneSelection.scene.category ||
      expectedOption == null ||
      practiceMode != expectedOption.mode ||
      _resourceId(sessionObject['evaluation_policy_ref']) !=
          expectedOption.evaluationPolicyRef ||
      (status == 'starting' && sessionObject.containsKey('started_at')) ||
      (status == 'in_progress' && !sessionObject.containsKey('started_at')) ||
      sessionObject.containsKey('ended_at') ||
      sessionObject.containsKey('end_reason')) {
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
  );
  final selection = decodeSceneSelectionSnapshot(
    snapshotObject['scene_selection'],
  );
  final preparation = decodePreparationSnapshot(
    snapshotObject['preparation_snapshot'],
    decodeJobTargetInput: _jobTargetInput,
    decodeJobTargetCandidate: _jobTargetCandidate,
  );
  final policy = decodePreparationSessionPolicy(
    snapshotObject['session_policy'],
  );
  final objectives = decodePracticeObjectives(
    snapshotObject['practice_objectives'],
  );
  if (_resourceId(snapshotObject['snapshot_id']) != snapshotId ||
      _resourceId(snapshotObject['practice_session_id']) != sessionId ||
      _version(snapshotObject['plan_revision']) != expectedPlan.revision ||
      _enumText(snapshotObject['practice_experience'], const <String>{
            'INTERVIEW',
          }) !=
          practiceExperience.wireValue ||
      _enumText(snapshotObject['scene_category'], const <String>{
            'INTERVIEW_RECRUITER',
            'INTERVIEW_BEHAVIORAL',
            'INTERVIEW_PROFESSIONAL',
            'INTERVIEW_HIRING_MANAGER',
            'INTERVIEW_CUSTOM',
          }) !=
          sceneCategory.wireValue ||
      _enumText(snapshotObject['practice_mode'], const <String>{
            'FULL_SIMULATION',
            'FOCUS',
          }) !=
          practiceMode.wireValue ||
      !samePracticeSceneSelection(selection, expectedPlan.sceneSelection) ||
      !_samePreparationSnapshot(
        preparation,
        expectedPlan.preparationSnapshot,
      ) ||
      !_samePolicy(policy, expectedPlan.sessionPolicy) ||
      !_sameObjectives(objectives, expectedPlan.practiceObjectives)) {
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

void _validateParticipants(
  Object? value, {
  required String sessionId,
  required PracticePlan expectedPlan,
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
      const <String>{'FACILITATOR', 'LEARNER'},
    );
    if (participantRole == 'LEARNER') {
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
            expectedPlan.selectedRoles.single.id ||
        !_sameRole(role, expectedPlan.selectedRoles.single)) {
      throw _invalidResponse();
    }
    _text(subject['namespace']);
    _resourceId(subject['subject_id']);
  }
  if (interviewerCount != 1 || candidateCount != 1) {
    throw _invalidResponse();
  }
}

bool _sameRole(RoleDefinition left, RoleDefinition right) {
  return left.id == right.id &&
      left.sceneId == right.sceneId &&
      left.type == right.type &&
      left.displayName == right.displayName &&
      left.responsibilities == right.responsibilities &&
      left.style == right.style &&
      left.voiceConfigRef == right.voiceConfigRef &&
      _sameRoleObjectives(left.practiceObjectives, right.practiceObjectives);
}

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

bool _samePreparationSnapshot(
  PreparationSnapshot left,
  PreparationSnapshot right,
) {
  return left.id == right.id &&
      left.sourceProfileId == right.sourceProfileId &&
      left.sourceVersion == right.sourceVersion &&
      left.sourceJobTargetId == right.sourceJobTargetId &&
      left.sourceJobTargetConfirmationVersion ==
          right.sourceJobTargetConfirmationVersion &&
      left.resumeSnapshot == right.resumeSnapshot &&
      left.jobDescriptionSnapshot == right.jobDescriptionSnapshot &&
      left.backgroundSnapshot == right.backgroundSnapshot &&
      left.createdAt == right.createdAt &&
      left.jobTargetInput == right.jobTargetInput &&
      ((left.jobTargetCandidate == null && right.jobTargetCandidate == null) ||
          (left.jobTargetCandidate != null &&
              right.jobTargetCandidate != null &&
              _sameCandidate(
                left.jobTargetCandidate!,
                right.jobTargetCandidate!,
              )));
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
      leftRecommendation.sceneId == rightRecommendation.sceneId &&
      leftRecommendation.sceneVersion == rightRecommendation.sceneVersion &&
      _sameStrings(
        leftRecommendation.selectedRoleIds,
        rightRecommendation.selectedRoleIds,
      ) &&
      leftRecommendation.practiceOptionId ==
          rightRecommendation.practiceOptionId;
}

bool _samePolicy(
  PreparationSessionPolicy left,
  PreparationSessionPolicy right,
) {
  return left.suggestedDurationSeconds == right.suggestedDurationSeconds &&
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
}

bool _sameObjectives(
  List<PracticeObjective> left,
  List<PracticeObjective> right,
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
      'scene_id': candidate.catalogRecommendation.sceneId,
      'scene_version': candidate.catalogRecommendation.sceneVersion,
      'selected_role_ids': candidate.catalogRecommendation.selectedRoleIds,
      'practice_option_id': candidate.catalogRecommendation.practiceOptionId,
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

void _requireProfileInput(CreatePreparationProfileInput input) {
  _requireText(input.backgroundSummary, maxBytes: 64 * 1024);
  final hasResume = input.resumeId != null;
  if (hasResume != (input.resumeRevision != null)) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: JobPreparationOperationStage.profile,
    );
  }
  if (input.resumeId case final value?) {
    _requireResourceId(value);
  }
  if (input.resumeRevision case final value?) {
    _requireVersion(value);
  }
  if (input.jobDescriptionRef case final value?) {
    _requireText(value, maxBytes: 16 * 1024);
  }
  final hasJobTarget = input.jobTargetId != null;
  if (hasJobTarget != (input.jobTargetConfirmationVersion != null)) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: JobPreparationOperationStage.profile,
    );
  }
  if (input.jobTargetId case final value?) {
    _requireResourceId(value);
  }
  if (input.jobTargetConfirmationVersion case final value?) {
    _requireVersion(value);
  }
}

void _requirePlanInput(CreatePreparationPlanInput input) {
  if (input.sourceThreadId case final value?) {
    _requireResourceId(value);
  }
  if (input.goalId case final value?) {
    _requireResourceId(value);
  }
  _requireResourceId(input.preparationSnapshotId);
  _requireResourceId(input.sceneId);
  _requireVersion(input.sceneVersion);
  _requireResourceId(input.practiceOptionId);
  if (input.selectedRoleIds.isEmpty ||
      input.selectedRoleIds.toSet().length != input.selectedRoleIds.length) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: JobPreparationOperationStage.plan,
    );
  }
  for (final roleId in input.selectedRoleIds) {
    _requireResourceId(roleId);
  }
  if (input.ieltsSelection case final selection?) {
    if ((selection.part1SetId == null && selection.topicGroupId == null)) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.invalidRequest,
        stage: JobPreparationOperationStage.plan,
      );
    }
    if (selection.part1SetId case final value?) {
      _requireResourceId(value);
    }
    if (selection.topicGroupId case final value?) {
      _requireResourceId(value);
    }
  }
  if (input.maxEffectiveTurns case final value?) {
    _requireVersion(value);
  }
}

void _requireRevisePlanInput(RevisePreparationPlanInput input) {
  _requireVersion(input.expectedPlanRevision);
  _requireResourceId(input.practiceOptionId);
  _requireVersion(input.maxEffectiveTurns);
  if (input.selectedRoleIds.isEmpty ||
      input.selectedRoleIds.toSet().length != input.selectedRoleIds.length) {
    throw const JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: JobPreparationOperationStage.plan,
    );
  }
  for (final roleId in input.selectedRoleIds) {
    _requireResourceId(roleId);
  }
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
