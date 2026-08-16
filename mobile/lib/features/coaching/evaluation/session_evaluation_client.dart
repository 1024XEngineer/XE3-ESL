import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

enum SessionEvaluationFailureKind {
  authenticationRequired,
  notFound,
  invalidRequest,
  invalidResponse,
  network,
  server,
  superseded,
}

final class SessionEvaluationException implements Exception {
  const SessionEvaluationException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final SessionEvaluationFailureKind kind;
  final int? statusCode;
  final bool retryable;
}

abstract interface class SessionEvaluationClient {
  Future<SessionEvaluation> get(String practiceSessionId);

  Future<void> clearAccountState();
}

final class WireSessionEvaluationClient implements SessionEvaluationClient {
  factory WireSessionEvaluationClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    final rawTransport = transport ?? IoIdentityHttpTransport();
    return WireSessionEvaluationClient._(
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

  WireSessionEvaluationClient._(
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
  Future<SessionEvaluation> get(String practiceSessionId) async {
    if (!_uuidPattern.hasMatch(practiceSessionId)) {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.invalidRequest,
      );
    }
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(
      '/v1/practice-sessions/${Uri.encodeComponent(practiceSessionId)}/evaluation',
    );
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: 'GET',
            uri: uri,
            headers: const {HttpHeaders.acceptHeader: 'application/json'},
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.superseded,
      );
    } on StateError {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.network,
        retryable: true,
      );
    } on SocketException catch (_) {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.network,
        retryable: true,
      );
    } on IOException catch (_) {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.network,
        retryable: true,
      );
    }
    if (generation != _accountGeneration) {
      throw const SessionEvaluationException(
        kind: SessionEvaluationFailureKind.superseded,
      );
    }
    switch (response.statusCode) {
      case HttpStatus.ok:
        try {
          return decodeSessionEvaluation(jsonDecode(response.body));
        } on FormatException catch (_) {
          throw const SessionEvaluationException(
            kind: SessionEvaluationFailureKind.invalidResponse,
          );
        } on SessionEvaluationDecodeException catch (_) {
          throw const SessionEvaluationException(
            kind: SessionEvaluationFailureKind.invalidResponse,
          );
        }
      case HttpStatus.unauthorized:
        throw const SessionEvaluationException(
          kind: SessionEvaluationFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.notFound:
        throw const SessionEvaluationException(
          kind: SessionEvaluationFailureKind.notFound,
          statusCode: HttpStatus.notFound,
        );
      case HttpStatus.badRequest:
        throw const SessionEvaluationException(
          kind: SessionEvaluationFailureKind.invalidRequest,
          statusCode: HttpStatus.badRequest,
        );
      default:
        throw SessionEvaluationException(
          kind: response.statusCode >= 500
              ? SessionEvaluationFailureKind.server
              : SessionEvaluationFailureKind.invalidResponse,
          statusCode: response.statusCode,
          retryable: response.statusCode >= 500,
        );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

final _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
);
