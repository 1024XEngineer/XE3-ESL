import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
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

    test('rejects malformed percent escapes exposed after decoding', () async {
      final connector = IoAuthenticatedWebSocketConnector();
      final uri = Uri.parse('wss://api.speak-up.test/events?credential=%25ZZ');

      final error = await _captureArgumentError(
        connector.connect(uri: uri, sessionToken: 'sess_current-token'),
      );

      expect(error.toString(), isNot(contains(uri.toString())));
      expect(error.toString(), isNot(contains('sess_current-token')));
    });

    test('rejects any sess_ credential marker across URI surfaces', () async {
      final connector = IoAuthenticatedWebSocketConnector();
      const currentToken = 'sess_current-token';
      var deeplyEncodedMarker = 'sess%5Fother';
      for (var pass = 0; pass < 12; pass += 1) {
        deeplyEncodedMarker = deeplyEncodedMarker.replaceAll('%', '%25');
      }
      final unsafeUris = <Uri>[
        Uri.parse('wss://sess_other@api.speak-up.test/events'),
        Uri.parse('wss://sess_other.api.speak-up.test/events'),
        Uri.parse('wss://api.speak-up.test/events/sess_other'),
        Uri.parse(
          'wss://api.speak-up.test/events?credential='
          's%2565ss%255Fother',
        ),
        Uri.parse(
          'wss://api.speak-up.test/events?credential=$deeplyEncodedMarker',
        ),
      ];

      for (final uri in unsafeUris) {
        final error = await _captureArgumentError(
          connector.connect(uri: uri, sessionToken: currentToken),
        );
        expect(error.toString(), isNot(contains(uri.toString())));
        expect(error.toString(), isNot(contains('sess_')));
      }
    });

    test('rejects fragments and does not echo the URI or credential', () async {
      final connector = IoAuthenticatedWebSocketConnector();
      for (final uri in <Uri>[
        Uri.parse('wss://api.speak-up.test/events#sess%255Fother'),
        Uri.parse('wss://api.speak-up.test/events#'),
      ]) {
        final error = await _captureArgumentError(
          connector.connect(uri: uri, sessionToken: 'sess_current-token'),
        );

        expect(error.toString(), isNot(contains(uri.toString())));
        expect(error.toString(), isNot(contains('sess_current-token')));
        expect(error.toString(), isNot(contains('sess_')));
      }
    });

    test('allows ordinary percent encoding in a normal URL', () async {
      var dialed = false;
      final connector = IoAuthenticatedWebSocketConnector(
        dialer: (_, {protocols, headers}) async {
          dialed = true;
          throw const SocketException('offline');
        },
      );

      final error = await _captureWebSocketError(
        connector.connect(
          uri: Uri.parse(
            'wss://api.speak-up.test/practice%20events'
            '?after_sequence=42&label=hello%20world',
          ),
          sessionToken: 'sess_current-token',
        ),
      );

      expect(dialed, isTrue);
      expect(error.kind, WebSocketFailureKind.recoverable);
    });

    test(
      'rejects invalid scheme and missing authority without echoing input',
      () async {
        final connector = IoAuthenticatedWebSocketConnector();

        for (final uri in <Uri>[
          Uri.parse('https://api.speak-up.test/events'),
          Uri.parse('wss:events'),
        ]) {
          final error = await _captureArgumentError(
            connector.connect(uri: uri, sessionToken: 'sess_current-token'),
          );
          expect(error.toString(), isNot(contains(uri.toString())));
        }
      },
    );

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

  test(
    'session connector rejects cross-origin WSS before reading credential',
    () async {
      var providerRead = false;
      final connector = SessionAuthenticatedWebSocketConnector(
        connector: IoAuthenticatedWebSocketConnector(),
        credentialProvider: () {
          providerRead = true;
          return const AuthSessionCredential(
            sessionToken: 'sess_current-token',
            generation: 1,
          );
        },
        invalidateSession: _noInvalidation,
        trustedBaseUri: Uri.parse('wss://api.speak-up.test'),
      );

      await expectLater(
        connector.connect(uri: Uri.parse('wss://other.speak-up.test/events')),
        throwsA(
          isA<ArgumentError>().having(
            (error) => error.toString(),
            'redacted error',
            allOf(
              isNot(contains('other.speak-up.test')),
              isNot(contains('sess_current-token')),
            ),
          ),
        ),
      );
      expect(providerRead, isFalse);
    },
  );

  for (final portCase in <({String trusted, String resource})>[
    (
      trusted: 'wss://api.speak-up.test',
      resource: 'wss://api.speak-up.test:443/events',
    ),
    (trusted: 'ws://127.0.0.1', resource: 'ws://127.0.0.1:80/events'),
  ]) {
    test('session connector accepts equivalent default port for '
        '${portCase.trusted}', () async {
      var dialed = false;
      final connector = SessionAuthenticatedWebSocketConnector(
        connector: IoAuthenticatedWebSocketConnector(
          dialer: (_, {protocols, headers}) async {
            dialed = true;
            throw const SocketException('offline');
          },
        ),
        credentialProvider: () => const AuthSessionCredential(
          sessionToken: 'sess_current-token',
          generation: 1,
        ),
        invalidateSession: _noInvalidation,
        trustedBaseUri: Uri.parse(portCase.trusted),
      );

      final error = await _captureSessionWebSocketError(
        connector.connect(uri: Uri.parse(portCase.resource)),
      );

      expect(dialed, isTrue);
      expect(error.kind, WebSocketFailureKind.recoverable);
    });
  }

  test(
    'session connector rejects WSS and WS port mismatches before credentials',
    () async {
      for (final portCase in <({String trusted, String resource})>[
        (
          trusted: 'wss://api.speak-up.test',
          resource: 'wss://api.speak-up.test:8443/events',
        ),
        (trusted: 'ws://127.0.0.1', resource: 'ws://127.0.0.1:8080/events'),
      ]) {
        var providerRead = false;
        final connector = SessionAuthenticatedWebSocketConnector(
          connector: IoAuthenticatedWebSocketConnector(),
          credentialProvider: () {
            providerRead = true;
            return const AuthSessionCredential(
              sessionToken: 'sess_current-token',
              generation: 1,
            );
          },
          invalidateSession: _noInvalidation,
          trustedBaseUri: Uri.parse(portCase.trusted),
        );

        final resourceUri = Uri.parse(portCase.resource);
        final error = await _captureArgumentError(
          connector.connect(uri: resourceUri),
        );
        expect(providerRead, isFalse);
        expect(error.toString(), isNot(contains(resourceUri.toString())));
        expect(error.toString(), isNot(contains('sess_current-token')));
      }
    },
  );

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

Future<AuthenticatedWebSocketException> _captureSessionWebSocketError(
  Future<SessionAuthenticatedWebSocketConnection> future,
) async {
  try {
    await future;
    fail('Expected AuthenticatedWebSocketException.');
  } on AuthenticatedWebSocketException catch (error) {
    return error;
  }
}

Future<ArgumentError> _captureArgumentError(Future<Object?> future) async {
  try {
    await future;
    fail('Expected ArgumentError.');
  } on ArgumentError catch (error) {
    return error;
  }
}

Future<void> _noInvalidation({
  required String expectedSessionToken,
  required int expectedGeneration,
}) async {}
