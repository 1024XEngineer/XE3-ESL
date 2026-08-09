import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:speakup/features/coaching/ielts/ielts_answer_preparation.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireIeltsAnswerPreparationClient
    implements IeltsAnswerPreparationClient {
  factory WireIeltsAnswerPreparationClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 45),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WireIeltsAnswerPreparationClient._(
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

  WireIeltsAnswerPreparationClient._(
    this._baseUri,
    this._transport,
    this._requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<IeltsAnswerPreparation> create({
    required IeltsAnswerQuestionReference question,
    required List<String> personalPoints,
    required double targetBand,
  }) async {
    final response = await _send(
      'POST',
      '/v1/ielts-speaking/answer-preparations',
      body: <String, Object>{
        'question': question.toJson(),
        'personal_points': personalPoints,
        'target_band': targetBand,
      },
      idempotencyKey: _newIdempotencyKey('create'),
    );
    if (response.statusCode != HttpStatus.created &&
        response.statusCode != HttpStatus.ok) {
      throw _statusFailure(response.statusCode);
    }
    return _decodePreparation(response.body);
  }

  @override
  Future<IeltsAnswerPreparation> get(String id) async {
    final response = await _send(
      'GET',
      '/v1/ielts-speaking/answer-preparations/${Uri.encodeComponent(id)}',
    );
    if (response.statusCode != HttpStatus.ok) {
      throw _statusFailure(response.statusCode);
    }
    return _decodePreparation(response.body);
  }

  @override
  Future<IeltsAnswerPreparation> update({
    required String id,
    required int expectedVersion,
    required List<String> personalPoints,
    required double targetBand,
  }) async {
    final response = await _send(
      'PATCH',
      '/v1/ielts-speaking/answer-preparations/${Uri.encodeComponent(id)}',
      body: <String, Object>{
        'expected_version': expectedVersion,
        'personal_points': personalPoints,
        'target_band': targetBand,
      },
      idempotencyKey: _newIdempotencyKey('update'),
    );
    if (response.statusCode != HttpStatus.ok) {
      throw _statusFailure(response.statusCode);
    }
    return _decodePreparation(response.body);
  }

  @override
  Future<IeltsAnswerPreparation> generate({
    required String id,
    required int expectedVersion,
  }) async {
    final response = await _send(
      'POST',
      '/v1/ielts-speaking/answer-preparations/${Uri.encodeComponent(id)}/generations',
      body: <String, Object>{'expected_version': expectedVersion},
      idempotencyKey: _newIdempotencyKey('generate'),
    );
    if (response.statusCode != HttpStatus.ok) {
      throw _statusFailure(response.statusCode);
    }
    return _decodePreparation(response.body);
  }

  @override
  Future<void> delete({
    required String id,
    required int expectedVersion,
  }) async {
    final response = await _send(
      'DELETE',
      '/v1/ielts-speaking/answer-preparations/${Uri.encodeComponent(id)}?expected_version=$expectedVersion',
      idempotencyKey: _newIdempotencyKey('delete'),
    );
    if (response.statusCode != HttpStatus.noContent) {
      throw _statusFailure(response.statusCode);
    }
  }

  Future<IdentityHttpResponse> _send(
    String method,
    String path, {
    Map<String, Object>? body,
    String? idempotencyKey,
  }) async {
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    try {
      return await _transport
          .send(
            method: method,
            uri: uri,
            headers: <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
              if (body != null)
                HttpHeaders.contentTypeHeader: 'application/json',
              'Idempotency-Key': ?idempotencyKey,
            },
            body: body == null ? null : jsonEncode(body),
          )
          .timeout(_requestTimeout);
    } on StateError {
      throw const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.authenticationRequired,
      );
    } on TimeoutException {
      throw const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.network,
        retryable: true,
      );
    }
  }
}

IeltsAnswerPreparation _decodePreparation(String body) {
  try {
    final root = jsonDecode(body);
    if (root is! Map<String, Object?>) {
      throw const FormatException();
    }
    final questionObject = root['question'];
    if (questionObject is! Map<String, Object?>) {
      throw const FormatException();
    }
    final referenceObject = questionObject['reference'];
    if (referenceObject is! Map<String, Object?>) {
      throw const FormatException();
    }
    final status = IeltsAnswerPreparationStatus.values.byName(
      _string(root, 'status'),
    );
    return IeltsAnswerPreparation(
      id: _string(root, 'answer_preparation_id'),
      question: IeltsAnswerQuestionReference(
        bankId: _string(referenceObject, 'bank_id'),
        part: _string(referenceObject, 'part'),
        sourceId: _string(referenceObject, 'source_id'),
        questionPosition: _integer(referenceObject, 'question_position'),
      ),
      personalPoints: _strings(root, 'personal_points'),
      targetBand: _number(root, 'target_band').toDouble(),
      status: status,
      version: _integer(root, 'version'),
      generationRevision: _integer(root, 'generation_revision'),
      answer: _optionalString(root, 'answer'),
      outline: _optionalStrings(root, 'outline'),
      usefulExpressions: _optionalStrings(root, 'useful_expressions'),
      speechText: _optionalString(root, 'speech_text'),
    );
  } on Object {
    throw const IeltsAnswerPreparationException(
      kind: IeltsAnswerPreparationFailureKind.invalidResponse,
    );
  }
}

String _string(Map<String, Object?> object, String key) {
  final value = object[key];
  if (value is! String || value.trim().isEmpty) {
    throw const FormatException();
  }
  return value;
}

String? _optionalString(Map<String, Object?> object, String key) {
  if (!object.containsKey(key)) {
    return null;
  }
  return _string(object, key);
}

int _integer(Map<String, Object?> object, String key) {
  final value = object[key];
  if (value is! int || value < 0) {
    throw const FormatException();
  }
  return value;
}

num _number(Map<String, Object?> object, String key) {
  final value = object[key];
  if (value is! num) {
    throw const FormatException();
  }
  return value;
}

List<String> _strings(Map<String, Object?> object, String key) {
  final value = object[key];
  if (value is! List<Object?>) {
    throw const FormatException();
  }
  return value
      .map((item) {
        if (item is! String || item.trim().isEmpty) {
          throw const FormatException();
        }
        return item;
      })
      .toList(growable: false);
}

List<String> _optionalStrings(Map<String, Object?> object, String key) =>
    object.containsKey(key) ? _strings(object, key) : const <String>[];

IeltsAnswerPreparationException _statusFailure(int statusCode) =>
    switch (statusCode) {
      HttpStatus.unauthorized => const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      ),
      HttpStatus.badRequest => const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.invalidRequest,
        statusCode: HttpStatus.badRequest,
      ),
      HttpStatus.notFound => const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.notFound,
        statusCode: HttpStatus.notFound,
      ),
      HttpStatus.conflict => const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.conflict,
        statusCode: HttpStatus.conflict,
      ),
      HttpStatus.badGateway => const IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.generationFailed,
        statusCode: HttpStatus.badGateway,
        retryable: true,
      ),
      >= 500 => IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.server,
        statusCode: statusCode,
        retryable: true,
      ),
      _ => IeltsAnswerPreparationException(
        kind: IeltsAnswerPreparationFailureKind.invalidResponse,
        statusCode: statusCode,
      ),
    };

String _newIdempotencyKey(String operation) {
  final random = Random.secure();
  final bytes = List<int>.generate(18, (_) => random.nextInt(256));
  return 'ielts_${operation}_${base64Url.encode(bytes).replaceAll('=', '')}';
}
