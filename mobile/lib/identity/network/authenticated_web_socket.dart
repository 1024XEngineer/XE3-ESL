import 'dart:async';
import 'dart:io';

import 'package:speakup/identity/auth_state.dart';

import 'bearer_authentication.dart';
import 'transport_security.dart';

const identityWebSocketProtocol = 'speakup.events.v1';

typedef WebSocketDialer =
    Future<WebSocket> Function(
      String url, {
      Iterable<String>? protocols,
      Map<String, dynamic>? headers,
    });

enum WebSocketFailureKind { authenticationInvalid, recoverable }

final class AuthenticatedWebSocketException implements Exception {
  const AuthenticatedWebSocketException(this.kind);

  final WebSocketFailureKind kind;

  bool get invalidatesAuthentication =>
      kind == WebSocketFailureKind.authenticationInvalid;

  @override
  String toString() => 'AuthenticatedWebSocketException(kind: ${kind.name})';
}

final class WebSocketDisconnect {
  const WebSocketDisconnect({required this.kind, required this.closeCode});

  factory WebSocketDisconnect.fromClose({
    required int? closeCode,
    required String? closeReason,
  }) {
    final invalidSession =
        closeCode == 4401 && closeReason == 'session_invalid';
    return WebSocketDisconnect(
      kind: invalidSession
          ? WebSocketFailureKind.authenticationInvalid
          : WebSocketFailureKind.recoverable,
      closeCode: closeCode,
    );
  }

  final WebSocketFailureKind kind;
  final int? closeCode;

  bool get invalidatesAuthentication =>
      kind == WebSocketFailureKind.authenticationInvalid;
}

abstract interface class AuthenticatedWebSocketConnector {
  Future<WebSocket> connect({required Uri uri, required String sessionToken});
}

final class SessionAuthenticatedWebSocketConnection {
  SessionAuthenticatedWebSocketConnection({
    required this.socket,
    required AuthSessionCredential credential,
    required AuthSessionInvalidator invalidateSession,
  }) : _capturedCredential = credential,
       _onInvalidateSession = invalidateSession;

  final WebSocket socket;
  final AuthSessionCredential _capturedCredential;
  final AuthSessionInvalidator _onInvalidateSession;

  Future<void> handleDisconnect({
    required int? closeCode,
    required String? closeReason,
  }) async {
    final disconnect = WebSocketDisconnect.fromClose(
      closeCode: closeCode,
      closeReason: closeReason,
    );
    if (disconnect.invalidatesAuthentication) {
      await _onInvalidateSession(
        expectedSessionToken: _capturedCredential.sessionToken,
        expectedGeneration: _capturedCredential.generation,
      );
    }
  }
}

final class SessionAuthenticatedWebSocketConnector {
  factory SessionAuthenticatedWebSocketConnector({
    required AuthenticatedWebSocketConnector connector,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    required Uri trustedBaseUri,
  }) {
    return SessionAuthenticatedWebSocketConnector._(
      connector,
      credentialProvider,
      invalidateSession,
      TrustedIdentityWebSocketOrigin(trustedBaseUri),
    );
  }

  SessionAuthenticatedWebSocketConnector._(
    this.connector,
    this.credentialProvider,
    this.invalidateSession,
    this._trustedOrigin,
  );

  final AuthenticatedWebSocketConnector connector;
  final AuthSessionCredentialProvider credentialProvider;
  final AuthSessionInvalidator invalidateSession;
  final TrustedIdentityWebSocketOrigin _trustedOrigin;

  Future<SessionAuthenticatedWebSocketConnection> connect({
    required Uri uri,
  }) async {
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    final credential = credentialProvider();
    if (credential == null) {
      throw StateError('An authenticated session is required.');
    }
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    try {
      final socket = await connector.connect(
        uri: uri,
        sessionToken: credential.sessionToken,
      );
      return SessionAuthenticatedWebSocketConnection(
        socket: socket,
        credential: credential,
        invalidateSession: invalidateSession,
      );
    } on AuthenticatedWebSocketException catch (error) {
      if (error.invalidatesAuthentication) {
        await invalidateSession(
          expectedSessionToken: credential.sessionToken,
          expectedGeneration: credential.generation,
        );
      }
      rethrow;
    }
  }
}

final class IoAuthenticatedWebSocketConnector
    implements AuthenticatedWebSocketConnector {
  IoAuthenticatedWebSocketConnector({WebSocketDialer? dialer})
    : _dialer = dialer ?? WebSocket.connect;

  final WebSocketDialer _dialer;

  @override
  Future<WebSocket> connect({
    required Uri uri,
    required String sessionToken,
  }) async {
    _validateUri(uri, sessionToken);
    final authorization = bearerAuthorizationValue(sessionToken);
    try {
      return await _dialer(
        uri.toString(),
        protocols: const <String>[identityWebSocketProtocol],
        headers: <String, dynamic>{
          HttpHeaders.authorizationHeader: authorization,
        },
      );
    } on WebSocketException catch (error) {
      throw AuthenticatedWebSocketException(
        error.httpStatusCode == HttpStatus.unauthorized
            ? WebSocketFailureKind.authenticationInvalid
            : WebSocketFailureKind.recoverable,
      );
    } on SocketException {
      throw const AuthenticatedWebSocketException(
        WebSocketFailureKind.recoverable,
      );
    } on HttpException {
      throw const AuthenticatedWebSocketException(
        WebSocketFailureKind.recoverable,
      );
    } catch (_) {
      throw const AuthenticatedWebSocketException(
        WebSocketFailureKind.recoverable,
      );
    }
  }

  void _validateUri(Uri uri, String sessionToken) {
    if (uri.scheme != 'wss' && uri.scheme != 'ws') {
      throw ArgumentError('WebSocket URI must use ws or wss.');
    }
    if (!uri.hasAuthority || uri.host.isEmpty) {
      throw ArgumentError('WebSocket URI must include a host.');
    }
    if (uri.hasFragment) {
      throw ArgumentError('WebSocket URI must not include a fragment.');
    }

    if (uri.userInfo.isNotEmpty) {
      throw ArgumentError('WebSocket URI must not contain credentials.');
    }
    validateNoSessionCredentialInUri(uri, sessionToken: sessionToken);
    if (uri.scheme == 'ws' && !isLoopbackHost(uri.host)) {
      throw ArgumentError('Non-loopback WebSocket connections must use wss.');
    }
  }
}
