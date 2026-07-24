import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  const credential = AuthSessionCredential(
    sessionToken: 'sess_current-token',
    generation: 7,
  );

  test(
    'injects the captured credential for a normal same-origin resource',
    () async {
      final transport = _RecordingTransport();
      var providerReads = 0;
      final trustedBaseUri = Uri.parse('https://api.speak-up.test/v1/');
      final authenticatedTransport = SessionAuthenticatedHttpTransport(
        transport: transport,
        credentialProvider: () {
          providerReads += 1;
          return credential;
        },
        invalidateSession: _noInvalidation,
        trustedBaseUri: trustedBaseUri,
      );

      await authenticatedTransport.send(
        method: 'GET',
        uri: trustedBaseUri.resolve(
          'private%20resource?after=42&label=hello%20world',
        ),
        headers: const <String, String>{'Accept': 'application/json'},
      );

      expect(providerReads, 1);
      expect(transport.calls, 1);
      expect(
        transport.headers[HttpHeaders.authorizationHeader],
        'Bearer sess_current-token',
      );
    },
  );

  test(
    'rejects unsafe or different origins before reading the credential',
    () async {
      final unsafeUris = <Uri>[
        Uri.parse('https://other.speak-up.test/v1/private-resource'),
        Uri.parse('http://api.speak-up.test/v1/private-resource'),
        Uri.parse('https://api.speak-up.test:8443/v1/private-resource'),
        Uri.parse('https://user@api.speak-up.test/v1/private-resource'),
        Uri.parse('https://api.speak-up.test/v1/private-resource#fragment'),
        Uri.parse('https://api.speak-up.test/v1/private-resource#'),
        Uri.parse('https://api.speak-up.test./v1/private-resource'),
        Uri.parse('https://t%C3%A9st.example/v1/private-resource'),
      ];

      for (final uri in unsafeUris) {
        final transport = _RecordingTransport();
        var providerRead = false;
        final authenticatedTransport = SessionAuthenticatedHttpTransport(
          transport: transport,
          credentialProvider: () {
            providerRead = true;
            return credential;
          },
          invalidateSession: _noInvalidation,
          trustedBaseUri: Uri.parse('https://api.speak-up.test'),
        );

        await expectLater(
          authenticatedTransport.send(
            method: 'GET',
            uri: uri,
            headers: const <String, String>{},
          ),
          throwsA(
            isA<ArgumentError>().having(
              (error) => error.toString(),
              'redacted error',
              allOf(
                isNot(contains(uri.toString())),
                isNot(contains(credential.sessionToken)),
              ),
            ),
          ),
        );
        expect(providerRead, isFalse);
        expect(transport.calls, 0);
      }
    },
  );

  test(
    'rejects encoded or malformed credentials on a same-origin URL',
    () async {
      var deeplyEncodedMarker = 'sess%5Fother';
      for (var pass = 0; pass < 12; pass += 1) {
        deeplyEncodedMarker = deeplyEncodedMarker.replaceAll('%', '%25');
      }
      final unsafeUris = <Uri>[
        Uri.parse('https://api.speak-up.test/v1/sess_other/resource'),
        Uri.parse(
          'https://api.speak-up.test/v1/resource?credential=sess%5Fother',
        ),
        Uri.parse(
          'https://api.speak-up.test/v1/resource'
          '?credential=sess%255Fother',
        ),
        Uri.parse('https://api.speak-up.test/v1/resource?credential=%25ZZ'),
        Uri.parse(
          'https://api.speak-up.test/v1/resource'
          '?credential=$deeplyEncodedMarker',
        ),
      ];

      for (final uri in unsafeUris) {
        final transport = _RecordingTransport();
        var providerRead = false;
        final authenticatedTransport = SessionAuthenticatedHttpTransport(
          transport: transport,
          credentialProvider: () {
            providerRead = true;
            return credential;
          },
          invalidateSession: _noInvalidation,
          trustedBaseUri: Uri.parse('https://api.speak-up.test'),
        );

        await expectLater(
          authenticatedTransport.send(
            method: 'GET',
            uri: uri,
            headers: const <String, String>{},
          ),
          throwsA(
            isA<ArgumentError>().having(
              (error) => error.toString(),
              'redacted error',
              allOf(
                isNot(contains(uri.toString())),
                isNot(contains(credential.sessionToken)),
              ),
            ),
          ),
        );
        expect(providerRead, isFalse);
        expect(transport.calls, 0);
      }
    },
  );

  test('rejects unsafe trusted origins at construction', () {
    for (final trustedBaseUri in <Uri>[
      Uri.parse('https://api.speak-up.test:0'),
      Uri.parse('https://api.speak-up.test/#'),
      Uri.parse('https://api.speak-up.test/sess_configured-token'),
    ]) {
      var providerRead = false;

      expect(
        () => SessionAuthenticatedHttpTransport(
          transport: _RecordingTransport(),
          credentialProvider: () {
            providerRead = true;
            return credential;
          },
          invalidateSession: _noInvalidation,
          trustedBaseUri: trustedBaseUri,
        ),
        throwsArgumentError,
      );
      expect(providerRead, isFalse);
    }
  });

  test('allows plaintext HTTP only for the exact loopback origin', () async {
    final transport = _RecordingTransport();
    final authenticatedTransport = SessionAuthenticatedHttpTransport(
      transport: transport,
      credentialProvider: () => credential,
      invalidateSession: _noInvalidation,
      trustedBaseUri: Uri.parse('http://127.0.0.1:8080'),
    );

    await authenticatedTransport.send(
      method: 'GET',
      uri: Uri.parse('http://127.0.0.1:8080/v1/private-resource'),
      headers: const <String, String>{},
    );

    expect(transport.calls, 1);
  });

  test(
    'canonicalizes the default HTTPS port without changing origin',
    () async {
      final transport = _RecordingTransport();
      final authenticatedTransport = SessionAuthenticatedHttpTransport(
        transport: transport,
        credentialProvider: () => credential,
        invalidateSession: _noInvalidation,
        trustedBaseUri: Uri.parse('https://api.speak-up.test'),
      );

      await authenticatedTransport.send(
        method: 'GET',
        uri: Uri.parse('https://api.speak-up.test:443/v1/private-resource'),
        headers: const <String, String>{},
      );

      expect(transport.calls, 1);
    },
  );

  test(
    'native transport never forwards Authorization through redirects',
    () async {
      final redirectTarget = await HttpServer.bind(
        InternetAddress.loopbackIPv4,
        0,
      );
      var redirectTargetCalled = false;
      redirectTarget.listen((request) async {
        redirectTargetCalled = true;
        request.response.statusCode = HttpStatus.ok;
        await request.response.close();
      });
      final origin = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      origin.listen((request) async {
        request.response.statusCode = HttpStatus.found;
        request.response.headers.set(
          HttpHeaders.locationHeader,
          'http://127.0.0.1:${redirectTarget.port}/capture',
        );
        await request.response.close();
      });

      try {
        final transport = IoIdentityHttpTransport();
        final response = await transport.send(
          method: 'GET',
          uri: Uri.parse('http://127.0.0.1:${origin.port}/resource'),
          headers: const <String, String>{
            HttpHeaders.authorizationHeader: 'Bearer sess_current-token',
          },
        );

        expect(response.statusCode, HttpStatus.found);
        expect(redirectTargetCalled, isFalse);
        transport.close(force: true);
      } finally {
        await origin.close(force: true);
        await redirectTarget.close(force: true);
      }
    },
  );
}

Future<void> _noInvalidation({
  required String expectedSessionToken,
  required int expectedGeneration,
}) async {}

final class _RecordingTransport implements IdentityHttpTransport {
  int calls = 0;
  Map<String, String> headers = const <String, String>{};

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    calls += 1;
    this.headers = headers;
    return const IdentityHttpResponse(statusCode: 200, body: '{}');
  }
}
