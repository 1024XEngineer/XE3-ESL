import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../model/identity_models.dart';
import '../network/bearer_authentication.dart';
import '../network/identity_http_transport.dart';
import '../network/transport_security.dart';

enum IdentityFailureKind {
  invalidRequest,
  invalidCredentials,
  authenticationRequired,
  registrationUnavailable,
  rateLimited,
  server,
  network,
  invalidResponse,
  unexpected,
}

final class IdentityClientException implements Exception {
  const IdentityClientException({
    required this.kind,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
    this.correlationId,
  });

  final IdentityFailureKind kind;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;
  final String? correlationId;

  bool get isAuthenticationFailure =>
      kind == IdentityFailureKind.authenticationRequired;

  @override
  String toString() {
    final status = statusCode == null ? '' : ', statusCode: $statusCode';
    final code = errorCode == null ? '' : ', errorCode: $errorCode';
    return 'IdentityClientException(kind: ${kind.name}$status$code)';
  }
}

abstract interface class IdentityClient {
  Future<User> register({required String email, required String password});

  Future<LoginResult> login({required String email, required String password});

  Future<User> currentUser({required String sessionToken});

  Future<void> logout({required String sessionToken});
}

final class WireIdentityClient implements IdentityClient {
  factory WireIdentityClient({
    required Uri baseUri,
    IdentityHttpTransport? transport,
  }) {
    validateIdentityHttpBaseUri(baseUri);
    return WireIdentityClient._(
      baseUri,
      transport ?? IoIdentityHttpTransport(),
    );
  }

  WireIdentityClient._(this._baseUri, this._transport);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;

  @override
  Future<User> register({
    required String email,
    required String password,
  }) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/register',
      body: <String, Object?>{'email': email, 'password': password},
    );
    _requireStatus(response, 201);
    return _decode(response, (json) => User.fromJson(json));
  }

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/login',
      body: <String, Object?>{'email': email, 'password': password},
    );
    _requireStatus(response, 200);
    return _decode(response, (json) => LoginResult.fromJson(json));
  }

  @override
  Future<User> currentUser({required String sessionToken}) async {
    final response = await _send(
      method: 'GET',
      path: '/v1/me',
      sessionToken: sessionToken,
    );
    _requireStatus(response, 200);
    return _decode(response, (json) => User.fromJson(json));
  }

  @override
  Future<void> logout({required String sessionToken}) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/logout',
      sessionToken: sessionToken,
    );
    _requireStatus(response, 204);
  }

  Future<IdentityHttpResponse> _send({
    required String method,
    required String path,
    Map<String, Object?>? body,
    String? sessionToken,
  }) async {
    final uri = _baseUri.resolve(path);
    final headers = <String, String>{
      HttpHeaders.acceptHeader: ContentType.json.mimeType,
      if (body != null)
        HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
      if (sessionToken != null)
        HttpHeaders.authorizationHeader: bearerAuthorizationValue(sessionToken),
    };

    try {
      return await _transport.send(
        method: method,
        uri: uri,
        headers: headers,
        body: body == null ? null : jsonEncode(body),
      );
    } on IdentityClientException {
      rethrow;
    } on SocketException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } on TimeoutException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      throw const IdentityClientException(kind: IdentityFailureKind.unexpected);
    }
  }

  void _requireStatus(IdentityHttpResponse response, int expected) {
    if (response.statusCode == expected) {
      return;
    }
    throw _exceptionFor(response);
  }

  IdentityClientException _exceptionFor(IdentityHttpResponse response) {
    String? errorCode;
    bool retryable = response.statusCode >= 500;
    String? correlationId;
    try {
      final decoded = jsonDecode(response.body);
      if (decoded is Map<String, Object?>) {
        final error = decoded['error'];
        if (error is Map<String, Object?>) {
          final decodedCode = error['code'];
          final decodedRetryable = error['retryable'];
          final decodedCorrelationId = error['correlation_id'];
          if (decodedCode is String) {
            errorCode = decodedCode;
          }
          if (decodedRetryable is bool) {
            retryable = decodedRetryable;
          }
          if (decodedCorrelationId is String) {
            correlationId = decodedCorrelationId;
          }
        }
      }
    } on FormatException {
      // Status remains authoritative when an intermediary returns non-JSON.
    }

    final kind = switch (errorCode) {
      'invalid_request' => IdentityFailureKind.invalidRequest,
      'invalid_credentials' => IdentityFailureKind.invalidCredentials,
      'authentication_required' => IdentityFailureKind.authenticationRequired,
      'account_registration_unavailable' =>
        IdentityFailureKind.registrationUnavailable,
      'rate_limited' => IdentityFailureKind.rateLimited,
      _ when response.statusCode == 401 =>
        IdentityFailureKind.authenticationRequired,
      _ when response.statusCode == 409 =>
        IdentityFailureKind.registrationUnavailable,
      _ when response.statusCode == 429 => IdentityFailureKind.rateLimited,
      _ when response.statusCode >= 500 => IdentityFailureKind.server,
      _ => IdentityFailureKind.unexpected,
    };

    return IdentityClientException(
      kind: kind,
      statusCode: response.statusCode,
      errorCode: errorCode,
      retryable: retryable,
      correlationId: correlationId,
    );
  }

  T _decode<T>(
    IdentityHttpResponse response,
    T Function(Map<String, Object?> json) decode,
  ) {
    try {
      final json = jsonDecode(response.body);
      if (json is! Map<String, Object?>) {
        throw const FormatException('Invalid identity response.');
      }
      return decode(json);
    } on FormatException {
      throw IdentityClientException(
        kind: IdentityFailureKind.invalidResponse,
        statusCode: response.statusCode,
      );
    } on TypeError {
      throw IdentityClientException(
        kind: IdentityFailureKind.invalidResponse,
        statusCode: response.statusCode,
      );
    }
  }
}
