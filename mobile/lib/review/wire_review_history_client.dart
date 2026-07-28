import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/review/review_history_client.dart';

final class WireReviewHistoryClient implements ReviewHistoryClient {
  factory WireReviewHistoryClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    final rawTransport =
        transport ?? _IoReviewHistoryHttpTransport(requestTimeout);
    return WireReviewHistoryClient._(
      baseUri,
      SessionAuthenticatedHttpTransport(
        transport: rawTransport,
        credentialProvider: credentialProvider,
        invalidateSession: invalidateSession,
        trustedBaseUri: baseUri,
      ),
      requestTimeout,
    );
  }

  WireReviewHistoryClient._(
    this._baseUri,
    this._transport,
    this._requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  int _accountGeneration = 0;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    if (limit < 1 || limit > 50 || (cursor != null && !_validCursor(cursor))) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidRequest,
      );
    }
    final generation = _accountGeneration;
    final uri = _baseUri
        .resolve('/v1/formal-reviews')
        .replace(
          queryParameters: <String, String>{
            'limit': '$limit',
            ...cursor == null
                ? const <String, String>{}
                : <String, String>{'cursor': cursor},
          },
        );
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: 'GET',
            uri: uri,
            headers: const <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
            },
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.superseded,
      );
    } on StateError {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    }
    if (generation != _accountGeneration) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.superseded,
      );
    }
    switch (response.statusCode) {
      case HttpStatus.ok:
        return _decodeHistoryPage(response.body, limit: limit);
      case HttpStatus.unauthorized:
        throw const ReviewHistoryException(
          kind: ReviewHistoryFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.badRequest:
        throw const ReviewHistoryException(
          kind: ReviewHistoryFailureKind.invalidRequest,
          statusCode: HttpStatus.badRequest,
        );
      default:
        if (response.statusCode >= 500) {
          throw ReviewHistoryException(
            kind: ReviewHistoryFailureKind.server,
            statusCode: response.statusCode,
            retryable: true,
          );
        }
        throw ReviewHistoryException(
          kind: ReviewHistoryFailureKind.invalidResponse,
          statusCode: response.statusCode,
        );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

ReviewHistoryPage _decodeHistoryPage(String body, {required int limit}) {
  late final Object? decoded;
  try {
    decoded = jsonDecode(body);
  } on FormatException {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final root = _object(
    decoded,
    required: const <String>{'items'},
    optional: const <String>{'next_cursor'},
  );
  final rawItems = root['items'];
  if (rawItems is! List<Object?> || rawItems.length > limit) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final items = <ReviewHistoryItem>[];
  final ids = <String>{};
  ReviewHistoryItem? previous;
  for (final rawItem in rawItems) {
    final item = _decodeHistoryItem(rawItem);
    if (!ids.add(item.review.id) ||
        (previous != null && !_isBefore(item, previous))) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidResponse,
      );
    }
    items.add(item);
    previous = item;
  }
  String? nextCursor;
  if (root.containsKey('next_cursor')) {
    final value = root['next_cursor'];
    if (value is! String || !_validCursor(value) || items.length != limit) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidResponse,
      );
    }
    nextCursor = value;
  }
  return ReviewHistoryPage(
    items: List<ReviewHistoryItem>.unmodifiable(items),
    nextCursor: nextCursor,
  );
}

ReviewHistoryItem _decodeHistoryItem(Object? value) {
  final root = _object(
    value,
    required: const <String>{
      'review_id',
      'practice_session_id',
      'status',
      'implementation_version',
      'source_turn_id',
      'source_turn_version',
      'result',
      'created_at',
      'updated_at',
      'completed_at',
    },
    optional: const <String>{'evaluation_context_type', 'evaluation_context'},
  );
  final id = _uuid(root['review_id']);
  final practiceSessionId = _nonEmptyString(
    root['practice_session_id'],
    maxBytes: _maxReviewMetadataBytes,
  );
  if (root['status'] != 'completed') {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final implementationVersion = _nonEmptyString(
    root['implementation_version'],
    maxBytes: _maxReviewMetadataBytes,
  );
  _nonEmptyString(root['source_turn_id'], maxBytes: _maxReviewMetadataBytes);
  final sourceVersion = _nonEmptyString(
    root['source_turn_version'],
    maxBytes: _maxReviewMetadataBytes,
  );
  if (!RegExp(
    r'^conversation-turn:evidence-v[1-9][0-9]*$',
  ).hasMatch(sourceVersion)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final createdAt = _dateTime(root['created_at']);
  final updatedAt = _dateTime(root['updated_at']);
  final completedAt = _dateTime(root['completed_at']);
  if (updatedAt.isBefore(createdAt) || completedAt.isBefore(createdAt)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final contextType = root.containsKey('evaluation_context_type')
      ? _evaluationContextType(root['evaluation_context_type'])
      : null;
  if (root.containsKey('evaluation_context')) {
    _validateEvaluationContext(root['evaluation_context'], contextType);
  }
  if (implementationVersion == 'qianwen-scenario-review-v2' &&
      (contextType == null || !root.containsKey('evaluation_context'))) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final result = _object(
    root['result'],
    required: const <String>{'summary', 'conclusions'},
    optional: const <String>{
      'summary_eligibility',
      'overall_score',
      'feedback_items',
      'repractice_suggestion_refs',
      'insufficient_evidence_reasons',
    },
  );
  if (utf8.encode(jsonEncode(result)).length > _maxReviewResultBytes) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final eligibility = result.containsKey('summary_eligibility')
      ? result['summary_eligibility']
      : 'eligible';
  if (eligibility != 'eligible' && eligibility != 'insufficient_evidence') {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final score = result['overall_score'];
  final summary = _nonEmptyString(
    result['summary'],
    maxBytes: _maxReviewTextBytes,
  );
  final rawConclusions = result['conclusions'];
  if (rawConclusions is! List<Object?> ||
      rawConclusions.length > _maxReviewConclusions) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  if (eligibility == 'eligible' &&
      (score is! int || score < 0 || score > 100 || rawConclusions.isEmpty)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  if (eligibility == 'insufficient_evidence' &&
      (result.containsKey('overall_score') ||
          rawConclusions.isNotEmpty ||
          !_validReasonList(result['insufficient_evidence_reasons']))) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final conclusions = rawConclusions
      .map(_decodeConclusion)
      .toList(growable: false);
  final feedback = _decodeFeedbackItems(result['feedback_items']);
  final strength =
      feedback
          .where((item) => item.kind == 'strength')
          .map((item) => item.message)
          .firstOrNull ??
      conclusions.map((item) => item.message).firstOrNull ??
      summary;
  final nextFocus =
      feedback
          .where(
            (item) =>
                item.kind == 'improvement' ||
                item.kind == 'correction' ||
                item.kind == 'recommended_expression',
          )
          .map((item) => item.suggestion ?? item.message)
          .firstOrNull ??
      conclusions
          .map((item) => item.suggestion)
          .whereType<String>()
          .firstOrNull ??
      conclusions.map((item) => item.message).lastOrNull ??
      '完成一段更充分的回答后重新评测。';
  return ReviewHistoryItem(
    review: AgentReview(
      id: id,
      title: _reviewTitle(
        eligibility: eligibility as String,
        score: score as int?,
        contextType: contextType,
      ),
      summary: summary,
      strength: strength,
      nextFocus: nextFocus,
    ),
    practiceSessionId: practiceSessionId,
    createdAt: createdAt,
    completedAt: completedAt,
  );
}

({String message, String? suggestion}) _decodeConclusion(Object? value) {
  final root = _object(
    value,
    required: const <String>{'key', 'category', 'message'},
    optional: const <String>{'score', 'suggestion'},
  );
  _nonEmptyString(root['key'], maxBytes: _maxReviewLabelBytes);
  _nonEmptyString(root['category'], maxBytes: _maxReviewLabelBytes);
  final message = _nonEmptyString(
    root['message'],
    maxBytes: _maxReviewTextBytes,
  );
  final suggestion = root.containsKey('suggestion')
      ? _nonEmptyString(root['suggestion'], maxBytes: _maxReviewTextBytes)
      : null;
  return (message: message, suggestion: suggestion);
}

List<({String kind, String message, String? suggestion})> _decodeFeedbackItems(
  Object? value,
) {
  if (value == null) {
    return const [];
  }
  if (value is! List<Object?> || value.length > 16) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return value
      .map((item) {
        final root = _object(
          item,
          required: const <String>{'key', 'kind', 'message'},
          optional: const <String>{'suggestion'},
        );
        _nonEmptyString(root['key'], maxBytes: _maxReviewLabelBytes);
        final kind = _nonEmptyString(
          root['kind'],
          maxBytes: _maxReviewLabelBytes,
        );
        if (!const <String>{
          'correction',
          'strength',
          'improvement',
          'recommended_expression',
        }.contains(kind)) {
          throw const ReviewHistoryException(
            kind: ReviewHistoryFailureKind.invalidResponse,
          );
        }
        final message = _nonEmptyString(
          root['message'],
          maxBytes: _maxReviewTextBytes,
        );
        final suggestion = root.containsKey('suggestion')
            ? _nonEmptyString(root['suggestion'], maxBytes: _maxReviewTextBytes)
            : null;
        return (kind: kind, message: message, suggestion: suggestion);
      })
      .toList(growable: false);
}

bool _validReasonList(Object? value) {
  return value is List<Object?> &&
      value.isNotEmpty &&
      value.every(
        (item) =>
            item is String &&
            item.trim().isNotEmpty &&
            utf8.encode(item).length <= _maxReviewLabelBytes,
      );
}

String? _evaluationContextType(Object? value) {
  const allowed = <String>{
    'interview.project_deep_dive',
    'ielts.speaking_part2',
    'workplace.progress_risk_update',
    'daily.hotel_checkin_issue',
    'generic.practice',
  };
  if (value is! String || !allowed.contains(value)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return value;
}

void _validateEvaluationContext(Object? value, String? contextType) {
  final context = _object(
    value,
    required: const <String>{
      'schema_version',
      'context_type',
      'scene_key',
      'scenario_definition_id',
      'scenario_definition_version',
      'practice_option_type',
      'difficulty_ref',
      'assistance_ref',
      'turn_policy_ref',
      'session_policy_ref',
      'scene_specific_context',
    },
  );
  final embeddedType = _evaluationContextType(context['context_type']);
  if (context['schema_version'] != 'evaluation-context.v1' ||
      embeddedType != contextType ||
      context['scenario_definition_version'] is! int ||
      (context['scenario_definition_version']! as int) < 1) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  for (final key in const <String>[
    'scene_key',
    'scenario_definition_id',
    'practice_option_type',
    'difficulty_ref',
    'assistance_ref',
    'turn_policy_ref',
    'session_policy_ref',
  ]) {
    _nonEmptyString(context[key], maxBytes: _maxReviewMetadataBytes);
  }
  final variantKey = switch (embeddedType) {
    'interview.project_deep_dive' => 'interview_project_deep_dive',
    'ielts.speaking_part2' => 'ielts_speaking_part2',
    'workplace.progress_risk_update' => 'workplace_progress_risk_update',
    'daily.hotel_checkin_issue' => 'daily_hotel_checkin_issue',
    'generic.practice' => 'generic_practice',
    _ => throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    ),
  };
  final sceneSpecific = _object(
    context['scene_specific_context'],
    required: <String>{'type', variantKey},
  );
  if (sceneSpecific['type'] != embeddedType ||
      sceneSpecific[variantKey] is! Map<String, Object?>) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
}

String _reviewTitle({
  required String eligibility,
  required int? score,
  required String? contextType,
}) {
  if (eligibility == 'insufficient_evidence') {
    return '证据不足 · 暂不评分';
  }
  switch (contextType) {
    case 'ielts.speaking_part2':
      return 'IELTS Speaking Part 2 场景评测（AI 文本估分，非官方成绩）';
    case 'generic.practice':
      return '通用英语沟通反馈（非官方考试评分）';
    case null:
      return '本次练习 · $score 分';
    default:
      return '本次场景评测 · $score 分';
  }
}

Map<String, Object?> _object(
  Object? value, {
  Set<String> required = const <String>{},
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return value;
}

String _nonEmptyString(Object? value, {required int maxBytes}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return value;
}

String _uuid(Object? value) {
  final result = _nonEmptyString(value, maxBytes: 36);
  if (!RegExp(
    r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
  ).hasMatch(result)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return result;
}

DateTime _dateTime(Object? value) {
  final text = _nonEmptyString(value, maxBytes: 64);
  if (!RegExp(
    r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$',
  ).hasMatch(text)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final result = DateTime.tryParse(text);
  if (result == null) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return result.toUtc();
}

bool _validCursor(String value) =>
    value.isNotEmpty &&
    value.length <= 512 &&
    RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(value);

bool _isBefore(ReviewHistoryItem item, ReviewHistoryItem boundary) =>
    item.createdAt.isBefore(boundary.createdAt) ||
    (item.createdAt == boundary.createdAt &&
        item.review.id.compareTo(boundary.review.id) < 0);

final class _IoReviewHistoryHttpTransport implements IdentityHttpTransport {
  const _IoReviewHistoryHttpTransport(this._requestTimeout);

  final Duration _requestTimeout;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    final client = HttpClient()..connectionTimeout = _requestTimeout;
    HttpClientRequest? request;
    try {
      final operation = () async {
        request = await client.openUrl(method, uri);
        request!.followRedirects = false;
        headers.forEach(request!.headers.set);
        if (body != null) {
          request!.add(utf8.encode(body));
        }
        final response = await request!.close();
        if (response.contentLength > _maxReviewHistoryResponseBytes) {
          request!.abort();
          throw const ReviewHistoryException(
            kind: ReviewHistoryFailureKind.invalidResponse,
            retryable: true,
          );
        }
        final responseBytes = await _readBoundedReviewHistoryResponse(
          response,
          request!,
        );
        late final String responseBody;
        try {
          responseBody = utf8.decode(responseBytes, allowMalformed: false);
        } on FormatException {
          request!.abort();
          throw const ReviewHistoryException(
            kind: ReviewHistoryFailureKind.invalidResponse,
            retryable: true,
          );
        }
        final responseHeaders = <String, String>{};
        response.headers.forEach((name, values) {
          responseHeaders[name] = values.join(',');
        });
        return IdentityHttpResponse(
          statusCode: response.statusCode,
          body: responseBody,
          headers: responseHeaders,
        );
      }();
      return await operation.timeout(
        _requestTimeout,
        onTimeout: () {
          request?.abort();
          client.close(force: true);
          throw TimeoutException('Review History HTTP request timed out.');
        },
      );
    } on TimeoutException {
      request?.abort();
      rethrow;
    } finally {
      client.close(force: true);
    }
  }
}

Future<Uint8List> _readBoundedReviewHistoryResponse(
  HttpClientResponse response,
  HttpClientRequest request,
) async {
  final builder = BytesBuilder(copy: false);
  var length = 0;
  await for (final chunk in response) {
    length += chunk.length;
    if (length > _maxReviewHistoryResponseBytes) {
      request.abort();
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidResponse,
        retryable: true,
      );
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

const _maxReviewHistoryResponseBytes = 1024 * 1024;
const _maxReviewResultBytes = 12 * 1024;
const _maxReviewMetadataBytes = 128;
const _maxReviewLabelBytes = 64;
const _maxReviewTextBytes = 2048;
const _maxReviewConclusions = 8;
