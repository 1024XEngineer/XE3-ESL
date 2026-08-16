import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_wire_codec.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireJobPreparationClient implements JobPreparationClient {
  factory WireJobPreparationClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 90),
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
  Future<InterviewPreparation> createInterviewPreparation({
    required InterviewPreparationInput input,
    InterviewResumeFile? resume,
    required String idempotencyKey,
  }) async {
    _requireInput(input);
    _requireResume(resume);
    final multipart = _interviewMultipart(input, resume, idempotencyKey);
    final response = await _request(
      method: 'POST',
      path: '/v1/interview-preparations',
      stage: JobPreparationOperationStage.interviewPreparation,
      expectedStatus: HttpStatus.created,
      idempotencyKey: idempotencyKey,
      bodyBytes: multipart.body,
      contentType: multipart.contentType,
    );
    return _decode(
      stage: JobPreparationOperationStage.interviewPreparation,
      statusCode: response.statusCode,
      decode: () {
        final preparation = decodeInterviewPreparationBody(response.body);
        _requireMatchingPreparation(preparation, input, resume);
        return preparation;
      },
    );
  }

  @override
  Future<InterviewPreparation> getInterviewPreparation(
    String interviewPreparationId,
  ) async {
    _requireAggregateId(interviewPreparationId);
    final response = await _request(
      method: 'GET',
      path:
          '/v1/interview-preparations/'
          '${Uri.encodeComponent(interviewPreparationId)}',
      stage: JobPreparationOperationStage.interviewPreparation,
      expectedStatus: HttpStatus.ok,
    );
    return _decode(
      stage: JobPreparationOperationStage.interviewPreparation,
      statusCode: response.statusCode,
      decode: () {
        final preparation = decodeInterviewPreparationBody(response.body);
        if (preparation.id != interviewPreparationId) {
          throw const PreparationWireFormatException();
        }
        return preparation;
      },
    );
  }

  @override
  Future<InterviewPreparation> regenerateInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationInput input,
    required String idempotencyKey,
  }) async {
    _requireAggregateId(interviewPreparationId);
    _requireVersion(expectedVersion);
    _requireInput(input);
    final response = await _patchPreparation(
      interviewPreparationId: interviewPreparationId,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_version': expectedVersion,
        'action': 'regenerate',
        'input': encodeInterviewPreparationInput(input),
      },
      stage: JobPreparationOperationStage.interviewPreparation,
    );
    return _decode(
      stage: JobPreparationOperationStage.interviewPreparation,
      statusCode: response.statusCode,
      decode: () {
        final preparation = decodeInterviewPreparationBody(response.body);
        if (preparation.id != interviewPreparationId ||
            preparation.version < expectedVersion) {
          throw const PreparationWireFormatException();
        }
        if (preparation.input != input) {
          throw const PreparationWireFormatException();
        }
        return preparation;
      },
    );
  }

  @override
  Future<InterviewPreparation> confirmInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationCandidate candidate,
    required String idempotencyKey,
  }) async {
    _requireAggregateId(interviewPreparationId);
    _requireVersion(expectedVersion);
    final response = await _patchPreparation(
      interviewPreparationId: interviewPreparationId,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_version': expectedVersion,
        'action': 'confirm',
        'candidate': encodeInterviewPreparationCandidate(candidate),
      },
      stage: JobPreparationOperationStage.confirmation,
    );
    return _decode(
      stage: JobPreparationOperationStage.confirmation,
      statusCode: response.statusCode,
      decode: () {
        final preparation = decodeInterviewPreparationBody(response.body);
        if (preparation.id != interviewPreparationId ||
            preparation.status != InterviewPreparationStatus.confirmed ||
            preparation.version < expectedVersion ||
            preparation.candidate.source != candidate.source) {
          throw const PreparationWireFormatException();
        }
        return preparation;
      },
    );
  }

  @override
  Future<InterviewPreparation> discardInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required String idempotencyKey,
  }) async {
    _requireAggregateId(interviewPreparationId);
    _requireVersion(expectedVersion);
    final response = await _patchPreparation(
      interviewPreparationId: interviewPreparationId,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_version': expectedVersion,
        'action': 'discard',
      },
      stage: JobPreparationOperationStage.interviewPreparation,
    );
    return _decode(
      stage: JobPreparationOperationStage.interviewPreparation,
      statusCode: response.statusCode,
      decode: () {
        final preparation = decodeInterviewPreparationBody(response.body);
        if (preparation.id != interviewPreparationId ||
            preparation.status != InterviewPreparationStatus.discarded ||
            preparation.version < expectedVersion) {
          throw const PreparationWireFormatException();
        }
        return preparation;
      },
    );
  }

  Future<IdentityHttpResponse> _patchPreparation({
    required String interviewPreparationId,
    required String idempotencyKey,
    required Map<String, Object?> body,
    required JobPreparationOperationStage stage,
  }) => _request(
    method: 'PATCH',
    path:
        '/v1/interview-preparations/'
        '${Uri.encodeComponent(interviewPreparationId)}',
    stage: stage,
    expectedStatus: HttpStatus.ok,
    idempotencyKey: idempotencyKey,
    body: body,
  );

  @override
  Future<List<PracticePlanSummary>> listPlans({
    required PracticeExperience experience,
  }) async {
    final response = await _request(
      method: 'GET',
      path:
          '/v1/practice-plans?practice_experience='
          '${Uri.encodeQueryComponent(experience.wireValue)}',
      stage: JobPreparationOperationStage.plan,
      expectedStatus: HttpStatus.ok,
    );
    return _decode(
      stage: JobPreparationOperationStage.plan,
      statusCode: response.statusCode,
      decode: () {
        final plans = decodePracticePlanListBody(response.body);
        if (plans.any((plan) => plan.experience != experience)) {
          throw const PreparationWireFormatException();
        }
        return plans;
      },
    );
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePracticePlanInput input,
    required String idempotencyKey,
  }) async {
    _requirePlanInput(input);
    final response = await _request(
      method: 'POST',
      path: '/v1/practice-plans',
      stage: JobPreparationOperationStage.plan,
      expectedStatus: HttpStatus.created,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        if (input.sourceThreadId != null)
          'source_thread_id': input.sourceThreadId,
        if (input.backgroundSummary.isNotEmpty)
          'background_summary': input.backgroundSummary,
        if (input.interviewPreparationId != null)
          'interview_preparation_id': input.interviewPreparationId,
        if (input.expectedInterviewVersion != null)
          'expected_interview_version': input.expectedInterviewVersion,
        'scene_id': input.sceneId,
        'scene_version': input.sceneVersion,
        'selected_role_ids': input.selectedRoleIds,
        'practice_option_id': input.practiceOptionId,
        if (input.maxEffectiveTurns != null)
          'max_effective_turns': input.maxEffectiveTurns,
        if (input.ieltsSelection case final selection?)
          'ielts_selection': selection.toJson(),
      },
    );
    return _decode(
      stage: JobPreparationOperationStage.plan,
      statusCode: response.statusCode,
      decode: () {
        final plan = decodePracticePlanBody(response.body);
        _requireMatchingPlan(plan, input);
        return plan;
      },
    );
  }

  @override
  Future<PracticePlan> getPlan(String planId) async {
    _requireAggregateId(planId);
    final response = await _request(
      method: 'GET',
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}',
      stage: JobPreparationOperationStage.plan,
      expectedStatus: HttpStatus.ok,
    );
    return _decode(
      stage: JobPreparationOperationStage.plan,
      statusCode: response.statusCode,
      decode: () {
        final plan = decodePracticePlanBody(response.body);
        if (plan.id != planId) throw const PreparationWireFormatException();
        return plan;
      },
    );
  }

  @override
  Future<PracticePlan> confirmPlan({
    required String planId,
    required int expectedVersion,
    required String idempotencyKey,
  }) async {
    _requireAggregateId(planId);
    _requireVersion(expectedVersion);
    final response = await _request(
      method: 'POST',
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}/confirm',
      stage: JobPreparationOperationStage.plan,
      expectedStatus: HttpStatus.ok,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'expected_version': expectedVersion},
    );
    return _decode(
      stage: JobPreparationOperationStage.plan,
      statusCode: response.statusCode,
      decode: () {
        final plan = decodePracticePlanBody(response.body);
        if (plan.id != planId || plan.status != PracticePlanStatus.ready) {
          throw const PreparationWireFormatException();
        }
        return plan;
      },
    );
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    _requireAggregateId(plan.id);
    if (plan.status != PracticePlanStatus.ready ||
        input.expectedPlanVersion != plan.version) {
      throw _invalidRequest(JobPreparationOperationStage.session);
    }
    final response = await _request(
      method: 'POST',
      path:
          '/v1/practice-plans/${Uri.encodeComponent(plan.id)}'
          '/practice-sessions',
      stage: JobPreparationOperationStage.session,
      expectedStatus: HttpStatus.created,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_version': input.expectedPlanVersion,
      },
    );
    return _decode(
      stage: JobPreparationOperationStage.session,
      statusCode: response.statusCode,
      decode: () => decodePreparationPracticeBootstrapBody(
        response.body,
        expectedPlan: plan,
      ),
    );
  }

  Future<IdentityHttpResponse> _request({
    required String method,
    required String path,
    required JobPreparationOperationStage stage,
    required int expectedStatus,
    String? idempotencyKey,
    Map<String, Object?>? body,
    List<int>? bodyBytes,
    String? contentType,
  }) async {
    if (body != null && bodyBytes != null) {
      throw ArgumentError('Only one request body may be provided.');
    }
    if (idempotencyKey != null) _requireIdempotencyKey(idempotencyKey, stage);
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    final headers = <String, String>{
      HttpHeaders.acceptHeader: ContentType.json.mimeType,
      if (body != null || bodyBytes != null)
        HttpHeaders.contentTypeHeader: contentType ?? ContentType.json.mimeType,
    };
    if (idempotencyKey != null) {
      headers['Idempotency-Key'] = idempotencyKey;
    }
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: method,
            uri: uri,
            headers: headers,
            body: body == null ? null : jsonEncode(body),
            bodyBytes: bodyBytes,
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
      throw _network(stage);
    } on SocketException {
      throw _network(stage);
    } on HttpException {
      throw _network(stage);
    } on IOException {
      throw _network(stage);
    }
    if (generation != _accountGeneration) {
      throw JobPreparationException(
        kind: JobPreparationFailureKind.superseded,
        stage: stage,
      );
    }
    if (response.statusCode == expectedStatus) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: stage,
          statusCode: response.statusCode,
          retryable: true,
        );
      }
      return response;
    }
    final code = _errorCode(response.body);
    throw switch (response.statusCode) {
      HttpStatus.badRequest => JobPreparationException(
        kind: JobPreparationFailureKind.invalidRequest,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      HttpStatus.unauthorized => JobPreparationException(
        kind: JobPreparationFailureKind.authenticationRequired,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      HttpStatus.notFound => JobPreparationException(
        kind: JobPreparationFailureKind.notFound,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      HttpStatus.conflict => JobPreparationException(
        kind: JobPreparationFailureKind.conflict,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      _ when response.statusCode >= 500 => JobPreparationException(
        kind: JobPreparationFailureKind.server,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
        retryable: true,
      ),
      _ => JobPreparationException(
        kind: JobPreparationFailureKind.invalidResponse,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
    };
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

({List<int> body, String contentType}) _interviewMultipart(
  InterviewPreparationInput input,
  InterviewResumeFile? resume,
  String idempotencyKey,
) {
  final boundary = 'speakup-interview-$idempotencyKey';
  final bytes = BytesBuilder(copy: false);
  void text(String value) => bytes.add(utf8.encode(value));
  text('--$boundary\r\n');
  text('Content-Disposition: form-data; name="input"\r\n');
  text('Content-Type: application/json\r\n\r\n');
  text(jsonEncode(encodeInterviewPreparationInput(input)));
  text('\r\n');
  if (resume != null) {
    text('--$boundary\r\n');
    text(
      'Content-Disposition: form-data; name="resume"; '
      'filename="${resume.name}"\r\n',
    );
    text('Content-Type: application/pdf\r\n\r\n');
    bytes.add(resume.bytes);
    text('\r\n');
  }
  text('--$boundary--\r\n');
  return (
    body: bytes.takeBytes(),
    contentType: 'multipart/form-data; boundary=$boundary',
  );
}

T _decode<T>({
  required JobPreparationOperationStage stage,
  required int statusCode,
  required T Function() decode,
}) {
  try {
    return decode();
  } on PreparationWireFormatException {
    throw JobPreparationException(
      kind: JobPreparationFailureKind.invalidResponse,
      stage: stage,
      statusCode: statusCode,
      retryable: true,
    );
  }
}

void _requireMatchingPreparation(
  InterviewPreparation preparation,
  InterviewPreparationInput input,
  InterviewResumeFile? resume,
) {
  if (preparation.status != InterviewPreparationStatus.draft ||
      preparation.input != input ||
      preparation.resumeUsed != (resume != null)) {
    throw const PreparationWireFormatException();
  }
}

void _requireMatchingPlan(PracticePlan plan, CreatePracticePlanInput input) {
  final interview = plan.preparationSnapshot.interview;
  if (plan.sourceThreadId != input.sourceThreadId ||
      plan.preparationSnapshot.backgroundSummary != input.backgroundSummary ||
      interview?.id != input.interviewPreparationId ||
      interview?.version != input.expectedInterviewVersion ||
      plan.sceneSelection.scene.id != input.sceneId ||
      plan.sceneSelection.scene.version != input.sceneVersion ||
      !listEquals(plan.sceneSelection.selectedRoleIds, input.selectedRoleIds) ||
      plan.sceneSelection.practiceOptionId != input.practiceOptionId ||
      (input.maxEffectiveTurns != null &&
          plan.sessionPolicy.maxEffectiveTurns != input.maxEffectiveTurns)) {
    throw const PreparationWireFormatException();
  }
}

void _requireInput(InterviewPreparationInput input) {
  bool valid(String? value, int maximumBytes) =>
      value == null ||
      (value.isNotEmpty &&
          value.trim() == value &&
          !value.contains('\u0000') &&
          utf8.encode(value).length <= maximumBytes);
  if (!valid(input.jobTitle, 512) ||
      !valid(input.jobDescription, 64 * 1024) ||
      !valid(input.company, 512) ||
      !valid(input.seniority, 256) ||
      !valid(input.candidateBackground, 16 * 1024) ||
      !valid(input.practiceFocus, 8 * 1024) ||
      switch (input.source) {
        InterviewPreparationSource.jobDescription =>
          input.jobDescription == null,
        InterviewPreparationSource.quickStart =>
          input.jobTitle == null || input.jobDescription != null,
      }) {
    throw _invalidRequest(JobPreparationOperationStage.interviewPreparation);
  }
}

void _requireResume(InterviewResumeFile? resume) {
  if (resume == null) return;
  if (!resume.name.toLowerCase().endsWith('.pdf') ||
      resume.name.contains(RegExp(r'[\r\n"]')) ||
      resume.bytes.length < 5 ||
      resume.bytes.length > 10 * 1024 * 1024 ||
      String.fromCharCodes(resume.bytes.take(5)) != '%PDF-') {
    throw _invalidRequest(JobPreparationOperationStage.interviewPreparation);
  }
}

void _requirePlanInput(CreatePracticePlanInput input) {
  if (input.sourceThreadId != null) _requireAggregateId(input.sourceThreadId!);
  final hasInterview = input.interviewPreparationId != null;
  if (hasInterview != (input.expectedInterviewVersion != null) ||
      (input.expectedInterviewVersion != null &&
          input.expectedInterviewVersion! < 1) ||
      input.sceneVersion < 1 ||
      input.selectedRoleIds.isEmpty ||
      input.selectedRoleIds.toSet().length != input.selectedRoleIds.length ||
      (input.maxEffectiveTurns != null && input.maxEffectiveTurns! < 1) ||
      input.backgroundSummary.trim() != input.backgroundSummary ||
      input.backgroundSummary.contains('\u0000') ||
      utf8.encode(input.backgroundSummary).length > 64 * 1024) {
    throw _invalidRequest(JobPreparationOperationStage.plan);
  }
  if (input.interviewPreparationId != null) {
    _requireAggregateId(input.interviewPreparationId!);
  }
  _requireResourceId(input.sceneId);
  _requireResourceId(input.practiceOptionId);
  for (final role in input.selectedRoleIds) {
    _requireResourceId(role);
  }
  if (input.ieltsSelection != null &&
      !input.ieltsSelection!.isValidCreateShape) {
    throw _invalidRequest(JobPreparationOperationStage.plan);
  }
}

void _requireAggregateId(String value) {
  if (!_uuidV4.hasMatch(value)) {
    throw _invalidRequest(JobPreparationOperationStage.plan);
  }
}

void _requireResourceId(String value) {
  if (!_resourceId.hasMatch(value)) {
    throw _invalidRequest(JobPreparationOperationStage.plan);
  }
}

void _requireVersion(int value) {
  if (value < 1) {
    throw _invalidRequest(JobPreparationOperationStage.interviewPreparation);
  }
}

void _requireIdempotencyKey(String value, JobPreparationOperationStage stage) {
  if (value.length < 8 ||
      value.length > 128 ||
      value.trim() != value ||
      value.contains('\u0000')) {
    throw _invalidRequest(stage);
  }
}

JobPreparationException _invalidRequest(JobPreparationOperationStage stage) =>
    JobPreparationException(
      kind: JobPreparationFailureKind.invalidRequest,
      stage: stage,
    );

JobPreparationException _network(JobPreparationOperationStage stage) =>
    JobPreparationException(
      kind: JobPreparationFailureKind.network,
      stage: stage,
      retryable: true,
    );

String? _errorCode(String body) {
  try {
    final root = jsonDecode(body);
    if (root is! Map<String, Object?> ||
        root['error'] is! Map<String, Object?>) {
      return null;
    }
    final code = (root['error']! as Map<String, Object?>)['code'];
    return code is String && code.length <= 128 ? code : null;
  } on FormatException {
    return null;
  }
}

final RegExp _uuidV4 = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);
final RegExp _resourceId = RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$');
