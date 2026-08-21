import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_wire_codec.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

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
  Future<PracticePlan> createPlan({
    required CreatePracticePlanInput input,
    required String idempotencyKey,
  }) async {
    _requirePlanInput(input);
    final response = await _request(
      method: 'POST',
      path: '/v1/practice-plans',
      stage: PreparationLaunchStage.plan,
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
        if (input.ieltsPreparedAnswers.isNotEmpty)
          'ielts_prepared_answers': input.ieltsPreparedAnswers
              .map((answer) => answer.toJson())
              .toList(growable: false),
      },
    );
    return _decode(
      stage: PreparationLaunchStage.plan,
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
      stage: PreparationLaunchStage.plan,
      expectedStatus: HttpStatus.ok,
    );
    return _decode(
      stage: PreparationLaunchStage.plan,
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
    if (expectedVersion < 1) throw _invalidRequest(PreparationLaunchStage.plan);
    final response = await _request(
      method: 'POST',
      path: '/v1/practice-plans/${Uri.encodeComponent(planId)}/confirm',
      stage: PreparationLaunchStage.plan,
      expectedStatus: HttpStatus.ok,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{'expected_version': expectedVersion},
    );
    return _decode(
      stage: PreparationLaunchStage.plan,
      statusCode: response.statusCode,
      decode: () {
        final plan = decodePracticePlanBody(response.body);
        if (plan.id != planId ||
            plan.status != PracticePlanStatus.ready ||
            plan.version < expectedVersion) {
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
      throw _invalidRequest(PreparationLaunchStage.session);
    }
    final response = await _request(
      method: 'POST',
      path:
          '/v1/practice-plans/${Uri.encodeComponent(plan.id)}'
          '/practice-sessions',
      stage: PreparationLaunchStage.session,
      expectedStatus: HttpStatus.created,
      idempotencyKey: idempotencyKey,
      body: <String, Object?>{
        'expected_plan_version': input.expectedPlanVersion,
      },
    );
    return _decode(
      stage: PreparationLaunchStage.session,
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
    required PreparationLaunchStage stage,
    required int expectedStatus,
    String? idempotencyKey,
    Map<String, Object?>? body,
  }) async {
    if (idempotencyKey != null) _requireIdempotencyKey(idempotencyKey, stage);
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    final headers = <String, String>{
      HttpHeaders.acceptHeader: ContentType.json.mimeType,
      if (body != null)
        HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
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
      throw _network(stage);
    } on SocketException {
      throw _network(stage);
    } on HttpException {
      throw _network(stage);
    } on IOException {
      throw _network(stage);
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
          statusCode: response.statusCode,
          retryable: true,
        );
      }
      return response;
    }
    final code = _errorCode(response.body);
    throw switch (response.statusCode) {
      HttpStatus.badRequest => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidRequest,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      HttpStatus.unauthorized => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.authenticationRequired,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      HttpStatus.notFound => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.notFound,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      HttpStatus.conflict => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.conflict,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
      ),
      _ when response.statusCode >= 500 => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.server,
        stage: stage,
        statusCode: response.statusCode,
        errorCode: code,
        retryable: true,
      ),
      _ => PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidResponse,
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

T _decode<T>({
  required PreparationLaunchStage stage,
  required int statusCode,
  required T Function() decode,
}) {
  try {
    return decode();
  } on PreparationWireFormatException {
    throw PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidResponse,
      stage: stage,
      statusCode: statusCode,
      retryable: true,
    );
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
      !_sameStrings(
        plan.sceneSelection.selectedRoleIds,
        input.selectedRoleIds,
      ) ||
      plan.sceneSelection.practiceOptionId != input.practiceOptionId ||
      (input.maxEffectiveTurns != null &&
          plan.sessionPolicy.maxEffectiveTurns != input.maxEffectiveTurns)) {
    throw const PreparationWireFormatException();
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
    throw _invalidRequest(PreparationLaunchStage.plan);
  }
  if (input.interviewPreparationId != null) {
    _requireAggregateId(input.interviewPreparationId!);
  }
  _requireResourceId(input.sceneId);
  _requireResourceId(input.practiceOptionId);
  for (final role in input.selectedRoleIds) {
    _requireResourceId(role);
  }
  if (input.ieltsSelection case final selection?) {
    if (!selection.isValidCreateShape) {
      throw _invalidRequest(PreparationLaunchStage.plan);
    }
  }
}

void _requireAggregateId(String value) {
  if (!_uuidV4.hasMatch(value)) {
    throw _invalidRequest(PreparationLaunchStage.plan);
  }
}

void _requireResourceId(String value) {
  if (!_resourceId.hasMatch(value)) {
    throw _invalidRequest(PreparationLaunchStage.plan);
  }
}

void _requireIdempotencyKey(String value, PreparationLaunchStage stage) {
  if (value.length < 8 ||
      value.length > 128 ||
      value.trim() != value ||
      value.contains('\u0000')) {
    throw _invalidRequest(stage);
  }
}

PreparationLaunchException _invalidRequest(PreparationLaunchStage stage) =>
    PreparationLaunchException(
      kind: PreparationLaunchFailureKind.invalidRequest,
      stage: stage,
    );

PreparationLaunchException _network(PreparationLaunchStage stage) =>
    PreparationLaunchException(
      kind: PreparationLaunchFailureKind.network,
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

bool _sameStrings(List<String> left, List<String> right) {
  if (left.length != right.length) return false;
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) return false;
  }
  return true;
}

final RegExp _uuidV4 = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);
final RegExp _resourceId = RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$');
