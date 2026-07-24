import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/network/authenticated_web_socket.dart';

void main() {
  group('IoAuthenticatedWebSocketConnector', () {
    test(
      'injects Bearer Upgrade header and the fixed event protocol',
      () async {
        String? capturedUrl;
        Iterable<String>? capturedProtocols;
        Map<String, dynamic>? capturedHeaders;
        final connector = IoAuthenticatedWebSocketConnector(
          dialer: (url, {protocols, headers}) async {
            capturedUrl = url;
            capturedProtocols = protocols;
            capturedHeaders = headers;
            throw const SocketException('offline');
          },
        );

        final error = await _captureWebSocketError(
          connector.connect(
            uri: Uri.parse(
              'wss://api.speak-up.test/v1/practice-sessions/session_1/events'
              '?after_sequence=42',
            ),
            sessionToken: 'sess_opaque-secret',
          ),
        );

        expect(error.kind, WebSocketFailureKind.recoverable);
        expect(capturedUrl, contains('after_sequence=42'));
        expect(capturedUrl, isNot(contains('sess_opaque-secret')));
        expect(capturedProtocols, <String>[identityWebSocketProtocol]);
        expect(
          capturedHeaders?[HttpHeaders.authorizationHeader],
          'Bearer sess_opaque-secret',
        );
      },
    );

    test('Upgrade 401 is an authentication invalidation', () async {
      final connector = IoAuthenticatedWebSocketConnector(
        dialer: (_, {protocols, headers}) async {
          throw const WebSocketException(
            'Connection was not upgraded.',
            HttpStatus.unauthorized,
          );
        },
      );

      final error = await _captureWebSocketError(
        connector.connect(
          uri: Uri.parse('wss://api.speak-up.test/events'),
          sessionToken: 'sess_opaque-secret',
        ),
      );

      expect(error.invalidatesAuthentication, isTrue);
      expect(error.toString(), isNot(contains('sess_opaque-secret')));
    });

    test(
      'non-401 Upgrade and ordinary transport failures are recoverable',
      () async {
        final connector = IoAuthenticatedWebSocketConnector(
          dialer: (_, {protocols, headers}) async {
            throw const WebSocketException(
              'Connection was not upgraded.',
              HttpStatus.serviceUnavailable,
            );
          },
        );

        final error = await _captureWebSocketError(
          connector.connect(
            uri: Uri.parse('wss://api.speak-up.test/events'),
            sessionToken: 'sess_opaque-secret',
          ),
        );

        expect(error.kind, WebSocketFailureKind.recoverable);
      },
    );

    test('rejects a Session Token accidentally placed in the URI', () async {
      final connector = IoAuthenticatedWebSocketConnector();

      expect(
        () => connector.connect(
          uri: Uri.parse(
            'wss://api.speak-up.test/events?token=sess_opaque-secret',
          ),
          sessionToken: 'sess_opaque-secret',
        ),
        throwsArgumentError,
      );
    });

    test('rejects percent-encoded Session Tokens in path and query', () async {
      final connector = IoAuthenticatedWebSocketConnector();

      for (final uri in <Uri>[
        Uri.parse('wss://api.speak-up.test/events/sess%5Fopaque%2Dsecret'),
        Uri.parse(
          'wss://api.speak-up.test/events?credential=sess%255Fopaque%252Dsecret',
        ),
      ]) {
        expect(
          () => connector.connect(uri: uri, sessionToken: 'sess_opaque-secret'),
          throwsArgumentError,
        );
      }
    });

    test('rejects a credential without the sess_ marker before dialing', () {
      var dialed = false;
      final connector = IoAuthenticatedWebSocketConnector(
        dialer: (_, {protocols, headers}) async {
          dialed = true;
          throw const SocketException('offline');
        },
      );

      expect(
        () => connector.connect(
          uri: Uri.parse('wss://api.speak-up.test/events'),
          sessionToken: 'abc123==',
        ),
        throwsArgumentError,
      );
      expect(dialed, isFalse);
    });

    test('permits ws only for loopback development', () async {
      final connector = IoAuthenticatedWebSocketConnector();

      expect(
        () => connector.connect(
          uri: Uri.parse('ws://api.speak-up.test/events'),
          sessionToken: 'sess_opaque-secret',
        ),
        throwsArgumentError,
      );
    });
  });

  group('WebSocketDisconnect', () {
    test('4401 session_invalid invalidates authentication', () {
      final disconnect = WebSocketDisconnect.fromClose(
        closeCode: 4401,
        closeReason: 'session_invalid',
      );

      expect(disconnect.invalidatesAuthentication, isTrue);
    });

    test('ordinary disconnect and mismatched close reason are recoverable', () {
      expect(
        WebSocketDisconnect.fromClose(
          closeCode: WebSocketStatus.goingAway,
          closeReason: 'server_restart',
        ).kind,
        WebSocketFailureKind.recoverable,
      );
      expect(
        WebSocketDisconnect.fromClose(
          closeCode: 4401,
          closeReason: 'other',
        ).kind,
        WebSocketFailureKind.recoverable,
      );
    });
  });
}

Future<AuthenticatedWebSocketException> _captureWebSocketError(
  Future<WebSocket> future,
) async {
  try {
    await future;
    fail('Expected AuthenticatedWebSocketException.');
  } on AuthenticatedWebSocketException catch (error) {
    return error;
  }
}
