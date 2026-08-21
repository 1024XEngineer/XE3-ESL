import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

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

    test(
      'profile registration and update use the frozen wire contract',
      () async {
        transport.response = const IdentityHttpResponse(
          statusCode: 201,
          body: '{"user_id":"user_1","email":"learner@example.com"}',
        );
        await client.registerWithProfile(
          email: 'learner@example.com',
          password: 'correct horse battery staple',
          displayName: '小林',
        );
        expect(jsonDecode(transport.body!), <String, Object?>{
          'email': 'learner@example.com',
          'password': 'correct horse battery staple',
          'display_name': '小林',
        });

        transport.response = const IdentityHttpResponse(
          statusCode: 200,
          body:
              '{"user_id":"user_1","display_name":"林同学",'
              '"profile_version":2,"created_at":"2026-08-23T10:00:00Z",'
              '"updated_at":"2026-08-23T11:00:00Z"}',
        );
        final profile = await client.updateProfile(
          sessionToken: 'sess_opaque-secret',
          displayName: '林同学',
          expectedProfileVersion: 1,
        );
        expect(profile.displayName, '林同学');
        expect(profile.profileVersion, 2);
        expect(transport.method, 'PATCH');
        expect(transport.uri!.path, '/v1/me/profile');
        expect(transport.headers?['Idempotency-Key'], isNull);
        expect(
          transport.headers?[HttpHeaders.authorizationHeader],
          'Bearer sess_opaque-secret',
        );
        expect(jsonDecode(transport.body!), <String, Object?>{
          'display_name': '林同学',
          'expected_profile_version': 1,
        });
      },
    );

    test(
      'avatar operations preserve version, bytes, and private URL',
      () async {
        transport.response = const IdentityHttpResponse(
          statusCode: 200,
          body:
              '{"user_id":"user_1","display_name":"小林",'
              '"profile_version":2,"avatar":{"width":512,"height":512,'
              '"updated_at":"2026-08-18T08:00:00Z"},'
              '"created_at":"2026-08-18T07:00:00Z",'
              '"updated_at":"2026-08-18T08:00:00Z"}',
        );
        final profile = await client.uploadAvatar(
          sessionToken: 'sess_opaque-secret',
          image: UserAvatarImage(
            contentType: 'image/png',
            bytes: Uint8List.fromList([1, 2, 3]),
          ),
          expectedProfileVersion: 1,
          idempotencyKey: 'avatar-request-1',
        );
        expect(profile.avatar?.width, 512);
        expect(transport.uri?.path, '/v1/me/avatar');
        expect(transport.headers?[HttpHeaders.ifMatchHeader], '"1"');
        expect(transport.headers?['Idempotency-Key'], 'avatar-request-1');
        expect(transport.bodyBytes, [1, 2, 3]);

        transport.response = const IdentityHttpResponse(
          statusCode: 200,
          body:
              '{"user_id":"user_1","display_name":"小林",'
              '"profile_version":3,"created_at":"2026-08-18T07:00:00Z",'
              '"updated_at":"2026-08-18T09:00:00Z"}',
        );
        final defaultProfile = await client.useDefaultAvatar(
          sessionToken: 'sess_opaque-secret',
          expectedProfileVersion: 2,
        );
        expect(defaultProfile.avatar, isNull);
        expect(transport.method, 'DELETE');

        transport.response = const IdentityHttpResponse(
          statusCode: 200,
          body: '',
          bodyBytes: [1, 2, 3],
          headers: {'content-type': 'image/png'},
        );
        final content = await client.currentAvatarContent(
          sessionToken: 'sess_opaque-secret',
        );
        expect(content.contentType, 'image/png');
        expect(content.bytes, [1, 2, 3]);
        expect(transport.uri?.path, '/v1/me/avatar/content');
      },
    );

    test(
      'missing profile remains distinct from authentication failure',
      () async {
        transport.response = const IdentityHttpResponse(
          statusCode: 404,
          body:
              '{"error":{"code":"profile_not_found","message":"missing",'
              '"retryable":false,"correlation_id":"corr_profile"}}',
        );
        await expectLater(
          client.currentProfile(sessionToken: 'sess_opaque-secret'),
          throwsA(
            isA<IdentityClientException>()
                .having(
                  (error) => error.kind,
                  'kind',
                  IdentityFailureKind.profileNotFound,
                )
                .having(
                  (error) => error.isAuthenticationFailure,
                  'isAuthenticationFailure',
                  false,
                ),
          ),
        );
      },
    );

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

    test(
      'login accepts the complete RFC3339 separator and leap-second range',
      () async {
        for (final expiresAt in <String>[
          '2026-08-23t10:00:00z',
          '2026-12-31T23:59:60Z',
        ]) {
          transport.response = IdentityHttpResponse(
            statusCode: 200,
            body:
                '''
            {
              "user":{"user_id":"user_1","email":"learner@example.com"},
              "session_token":"sess_opaque-secret",
              "token_type":"Bearer",
              "expires_at":"$expiresAt"
            }
          ''',
          );

          final result = await client.login(
            email: 'learner@example.com',
            password: 'correct horse battery staple',
          );

          expect(result.sessionToken, 'sess_opaque-secret');
        }
      },
    );

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

    test(
      'normalizes unknown error code by login operation and status',
      () async {
        const unknownCode = 'unknown_sess_server_code';
        transport.response = const IdentityHttpResponse(
          statusCode: 401,
          body:
              '''
          {"error":{
            "code":"$unknownCode",
            "message":"Untrusted server error.",
            "retryable":true,
            "correlation_id":"corr_unknown"
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
        expect(error.errorCode, 'invalid_credentials');
        expect(error.retryable, isFalse);
        expect(error.toString(), isNot(contains(unknownCode)));
      },
    );

    test('does not accept a stable code from the wrong operation', () async {
      transport.response = const IdentityHttpResponse(
        statusCode: 401,
        body: '''
          {"error":{
            "code":"invalid_credentials",
            "message":"Untrusted server error.",
            "retryable":false,
            "correlation_id":"corr_wrong_operation"
          }}
        ''',
      );

      final error = await _captureIdentityError(
        client.currentUser(sessionToken: 'sess_opaque-secret'),
      );

      expect(error.kind, IdentityFailureKind.authenticationRequired);
      expect(error.errorCode, 'authentication_required');
      expect(error.toString(), isNot(contains('invalid_credentials')));
    });

    test(
      'drops unknown code when status has no safe operation mapping',
      () async {
        const unknownCode = 'future_unknown_code';
        transport.response = const IdentityHttpResponse(
          statusCode: 418,
          body:
              '''
          {"error":{
            "code":"$unknownCode",
            "message":"Untrusted server error.",
            "retryable":true,
            "correlation_id":"corr_unknown_status"
          }}
        ''',
        );

        final error = await _captureIdentityError(
          client.register(
            email: 'learner@example.com',
            password: 'correct horse battery staple',
          ),
        );

        expect(error.kind, IdentityFailureKind.unexpected);
        expect(error.errorCode, isNull);
        expect(error.retryable, isFalse);
        expect(error.toString(), isNot(contains(unknownCode)));
      },
    );

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

    test('rejects successful responses outside the exact #77 shape', () async {
      const privateValue = 'sess_unexpected-private-value';
      for (final body in <String>[
        '''
          {
            "user_id":"user_1",
            "email":"UPPER@example.com"
          }
        ''',
        '''
          {
            "user_id":"user_1",
            "email":"learner@example.com",
            "session_token":"$privateValue"
          }
        ''',
      ]) {
        transport.response = IdentityHttpResponse(statusCode: 200, body: body);

        final error = await _captureIdentityError(
          client.currentUser(sessionToken: 'sess_opaque-secret'),
        );

        expect(error.kind, IdentityFailureKind.invalidResponse);
        expect(error.toString(), isNot(contains(privateValue)));
      }

      for (final expiresAt in <String>[
        '2026-08-23',
        '2026-08-23 10:00:00Z',
        '2026-08-23T10:00:00',
        '2026-02-30T10:00:00Z',
        '2026-13-01T10:00:00Z',
        '2026-08-23T24:00:00Z',
        '2026-08-23T10:60:00Z',
        '2026-08-23T10:00:62Z',
        '2026-08-23T10:00:00+24:00',
        '2026-08-23T10:00:00+08:60',
      ]) {
        transport.response = IdentityHttpResponse(
          statusCode: 200,
          body:
              '''
            {
              "user":{"user_id":"user_1","email":"learner@example.com"},
              "session_token":"sess_opaque-secret",
              "token_type":"Bearer",
              "expires_at":"$expiresAt"
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
        expect(error.toString(), isNot(contains('sess_opaque-secret')));
      }
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

    test('rejects unsafe base origins before sending credentials', () {
      for (final baseUri in <Uri>[
        Uri.parse('https://api.speak-up.test./'),
        Uri.parse('https://t%C3%A9st.example/'),
        Uri.parse('https://api.speak-up.test/#'),
        Uri.parse('https://sess_configured-token.speak-up.test/'),
      ]) {
        final unsafeTransport = _FakeTransport();

        expect(
          () =>
              WireIdentityClient(baseUri: baseUri, transport: unsafeTransport),
          throwsA(
            isA<ArgumentError>().having(
              (error) => error.toString(),
              'redacted error',
              allOf(
                isNot(contains(baseUri.toString())),
                isNot(contains('sess_configured-token')),
              ),
            ),
          ),
        );
        expect(unsafeTransport.uri, isNull);
      }
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
  List<int>? bodyBytes;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    this.method = method;
    this.uri = uri;
    this.headers = Map<String, String>.of(headers);
    this.body = body;
    this.bodyBytes = bodyBytes == null ? null : List<int>.of(bodyBytes);
    final failure = this.failure;
    if (failure != null) {
      throw failure;
    }
    return response;
  }
}
