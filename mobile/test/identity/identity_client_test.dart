import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  group('WireIdentityClient', () {
    late _FakeTransport transport;
    late WireIdentityClient client;

    setUp(() {
      transport = _FakeTransport();
      client = WireIdentityClient(
        baseUri: Uri.parse('https://api.speak-up.test/root/'),
        transport: transport,
      );
    });

    test('register sends only email and password and returns a User', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 201,
        body: '{"user_id":"user_1","email":"learner@example.com"}',
      );

      final user = await client.register(
        email: ' Learner@Example.com ',
        password: 'correct horse battery staple',
      );

      expect(user.id, 'user_1');
      expect(user.email, 'learner@example.com');
      expect(transport.method, 'POST');
      expect(
        transport.uri,
        Uri.parse('https://api.speak-up.test/v1/auth/register'),
      );
      expect(
        transport.headers,
        isNot(contains(HttpHeaders.authorizationHeader)),
      );
      expect(jsonDecode(transport.body!), <String, Object?>{
        'email': ' Learner@Example.com ',
        'password': 'correct horse battery staple',
      });
    });

    test('login parses the one-time Bearer Session result', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 200,
        body: '''
          {
            "user":{"user_id":"user_1","email":"learner@example.com"},
            "session_token":"sess_opaque-secret",
            "token_type":"Bearer",
            "expires_at":"2026-08-23T10:00:00Z"
          }
        ''',
      );

      final result = await client.login(
        email: 'learner@example.com',
        password: 'correct horse battery staple',
      );

      expect(result.user.id, 'user_1');
      expect(result.sessionToken, 'sess_opaque-secret');
      expect(result.expiresAt, DateTime.utc(2026, 8, 23, 10));
      expect(
        transport.headers,
        isNot(contains(HttpHeaders.authorizationHeader)),
      );
    });

    test('currentUser injects Bearer only in Authorization header', () async {
      const token = 'sess_opaque-secret';
      transport.response = const IdentityHttpResponse(
        statusCode: 200,
        body: '{"user_id":"user_1","email":"learner@example.com"}',
      );

      await client.currentUser(sessionToken: token);

      expect(transport.method, 'GET');
      expect(transport.uri, Uri.parse('https://api.speak-up.test/v1/me'));
      expect(
        transport.headers![HttpHeaders.authorizationHeader],
        'Bearer $token',
      );
      expect(transport.uri.toString(), isNot(contains(token)));
      expect(transport.body, isNull);
    });

    test('logout sends no body and injects the same Bearer header', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 204,
        body: '',
      );

      await client.logout(sessionToken: 'sess_opaque-secret');

      expect(transport.method, 'POST');
      expect(transport.uri!.path, '/v1/auth/logout');
      expect(transport.body, isNull);
      expect(
        transport.headers![HttpHeaders.authorizationHeader],
        'Bearer sess_opaque-secret',
      );
    });

    test('protected 401 is an authentication failure', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 401,
        body: '''
          {"error":{
            "code":"authentication_required",
            "message":"Rejected sess_opaque-secret.",
            "retryable":false,
            "correlation_id":"corr_auth"
          }}
        ''',
      );

      final error = await _captureIdentityError(
        client.currentUser(sessionToken: 'sess_opaque-secret'),
      );

      expect(error.kind, IdentityFailureKind.authenticationRequired);
      expect(error.isAuthenticationFailure, isTrue);
      expect(error.statusCode, 401);
      expect(error.correlationId, 'corr_auth');
      expect(error.toString(), isNot(contains('sess_opaque-secret')));
      expect(error.toString(), isNot(contains('Rejected')));
    });

    test(
      'login invalid_credentials remains distinct from expired auth',
      () async {
        transport.response = const IdentityHttpResponse(
          statusCode: 401,
          body: '''
          {"error":{
            "code":"invalid_credentials",
            "message":"Email or password is invalid.",
            "retryable":false,
            "correlation_id":"corr_login"
          }}
        ''',
        );

        final error = await _captureIdentityError(
          client.login(
            email: 'learner@example.com',
            password: 'incorrect password',
          ),
        );

        expect(error.kind, IdentityFailureKind.invalidCredentials);
        expect(error.isAuthenticationFailure, isFalse);
      },
    );

    test('maps stable registration and rate-limit error codes', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 409,
        body: '''
          {"error":{
            "code":"account_registration_unavailable",
            "message":"Account registration is unavailable.",
            "retryable":false,
            "correlation_id":"corr_register"
          }}
        ''',
      );
      var error = await _captureIdentityError(
        client.register(
          email: 'learner@example.com',
          password: 'correct horse battery staple',
        ),
      );
      expect(error.kind, IdentityFailureKind.registrationUnavailable);

      transport.response = const IdentityHttpResponse(
        statusCode: 429,
        body: '''
          {"error":{
            "code":"rate_limited",
            "message":"Too many requests.",
            "retryable":true,
            "correlation_id":"corr_rate"
          }}
        ''',
      );
      error = await _captureIdentityError(
        client.login(
          email: 'learner@example.com',
          password: 'correct horse battery staple',
        ),
      );
      expect(error.kind, IdentityFailureKind.rateLimited);
      expect(error.retryable, isTrue);
    });

    test('network failures are retryable and do not become 401', () async {
      transport.failure = const SocketException('offline');

      final error = await _captureIdentityError(
        client.currentUser(sessionToken: 'sess_opaque-secret'),
      );

      expect(error.kind, IdentityFailureKind.network);
      expect(error.retryable, isTrue);
      expect(error.isAuthenticationFailure, isFalse);
      expect(error.toString(), isNot(contains('sess_opaque-secret')));
    });

    test(
      'rejects malformed successful response without retaining body',
      () async {
        transport.response = const IdentityHttpResponse(
          statusCode: 200,
          body: '{"session_token":"sess_opaque-secret","token_type":"Bearer"}',
        );

        final error = await _captureIdentityError(
          client.login(
            email: 'learner@example.com',
            password: 'correct horse battery staple',
          ),
        );

        expect(error.kind, IdentityFailureKind.invalidResponse);
        expect(error.toString(), isNot(contains('sess_opaque-secret')));
      },
    );

    test('rejects a login response without the sess_ token prefix', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 200,
        body: '''
          {
            "user":{"user_id":"user_1","email":"learner@example.com"},
            "session_token":"abc123==",
            "token_type":"Bearer",
            "expires_at":"2026-08-23T10:00:00Z"
          }
        ''',
      );

      final error = await _captureIdentityError(
        client.login(
          email: 'learner@example.com',
          password: 'correct horse battery staple',
        ),
      );

      expect(error.kind, IdentityFailureKind.invalidResponse);
      expect(error.toString(), isNot(contains('abc123==')));
    });

    test('rejects plaintext non-loopback HTTP for all operations', () {
      expect(
        () => WireIdentityClient(
          baseUri: Uri.parse('http://api.speak-up.test'),
          transport: transport,
        ),
        throwsA(
          isA<ArgumentError>().having(
            (error) => error.toString(),
            'safe message',
            isNot(contains('api.speak-up.test')),
          ),
        ),
      );
    });

    test('allows plaintext HTTP only for loopback development', () async {
      final loopbackClient = WireIdentityClient(
        baseUri: Uri.parse('http://127.0.0.1:8080'),
        transport: transport,
      );
      transport.response = const IdentityHttpResponse(
        statusCode: 201,
        body: '{"user_id":"user_1","email":"learner@example.com"}',
      );

      await loopbackClient.register(
        email: 'learner@example.com',
        password: 'correct horse battery staple',
      );

      expect(transport.uri?.scheme, 'http');
      expect(transport.uri?.host, '127.0.0.1');
    });
  });
}

Future<IdentityClientException> _captureIdentityError(
  Future<Object?> future,
) async {
  try {
    await future;
    fail('Expected IdentityClientException.');
  } on IdentityClientException catch (error) {
    return error;
  }
}

final class _FakeTransport implements IdentityHttpTransport {
  IdentityHttpResponse response = const IdentityHttpResponse(
    statusCode: 500,
    body: '',
  );
  Object? failure;
  String? method;
  Uri? uri;
  Map<String, String>? headers;
  String? body;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    this.method = method;
    this.uri = uri;
    this.headers = Map<String, String>.of(headers);
    this.body = body;
    final failure = this.failure;
    if (failure != null) {
      throw failure;
    }
    return response;
  }
}
