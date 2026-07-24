import 'dart:async';
import 'dart:io';

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
      throw ArgumentError.value(uri.scheme, 'uri', 'Must use ws or wss.');
    }
    final credentialSurface = '${uri.userInfo}\n${uri.path}\n${uri.query}';
    if (uri.userInfo.isNotEmpty ||
        _containsTokenInAnyEncoding(credentialSurface, sessionToken)) {
      throw ArgumentError('Session Token must not be present in the URI.');
    }
    if (uri.scheme == 'ws' && !isLoopbackHost(uri.host)) {
      throw ArgumentError('Non-loopback WebSocket connections must use wss.');
    }
  }

  bool _containsTokenInAnyEncoding(String value, String sessionToken) {
    var decoded = value;
    final encodedToken = Uri.encodeComponent(sessionToken);
    for (var pass = 0; pass < 8; pass += 1) {
      if (decoded.contains(sessionToken) || decoded.contains(encodedToken)) {
        return true;
      }
      try {
        final next = Uri.decodeFull(decoded);
        if (next == decoded) {
          return false;
        }
        decoded = next;
      } on FormatException {
        return false;
      }
    }
    return decoded.contains(sessionToken);
  }
}
