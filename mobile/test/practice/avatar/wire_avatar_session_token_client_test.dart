import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/practice/avatar/avatar.dart';

void main() {
  final now = DateTime.utc(2026, 7, 29, 10);

  test(
    'uses Bearer/no-store and decodes the exact six-field contract',
    () async {
      final responseBody = _jsonBytes(_grantJson(now));
      final transport = _Transport(
        AvatarSessionWireResponse(
          statusCode: HttpStatus.ok,
          body: responseBody,
          headers: const {
            'Content-Type': 'application/json; charset=utf-8',
            'Cache-Control': 'no-store',
          },
        ),
      );
      final client = _client(transport, now: now);

      final grant = await client.createSession(
        practiceSessionId: 'practice-session-1',
      );

      final request = transport.request!;
      expect(request.method, 'POST');
      expect(
        request.uri.path,
        '/v1/practice-sessions/practice-session-1/avatar-session-token',
      );
      expect(request.uri.query, isEmpty);
      expect(
        request.headers[HttpHeaders.authorizationHeader],
        'Bearer sess_test',
      );
      expect(request.headers[HttpHeaders.acceptHeader], 'application/json');
      expect(request.headers[HttpHeaders.cacheControlHeader], 'no-store');
      expect(request.headers, isNot(contains(HttpHeaders.contentTypeHeader)));
      expect(grant.appId, 'app-1');
      expect(grant.avatarId, 'avatar-1');
      expect(grant.sessionToken, 'private-avatar-token');
      expect(grant.region, AvatarRegion.apNortheast);
      expect(grant.audioFormat.isSupported, isTrue);
      expect(responseBody, everyElement(0));
      expect(grant.toString(), isNot(contains('private-avatar-token')));
      expect(grant.toString(), isNot(contains('app-1')));
      expect(grant.toString(), isNot(contains('avatar-1')));
    },
  );

  test('rejects extra, missing, and mismatched audio fields', () async {
    final invalidBodies = <Map<String, Object?>>[
      {..._grantJson(now), 'unexpected': true},
      {..._grantJson(now)}..remove('avatar_id'),
      {
        ..._grantJson(now),
        'audio_format': {
          'encoding': 'PCM_S16LE',
          'sample_rate_hz': 16000,
          'channels': 1,
        },
      },
      {
        ..._grantJson(now),
        'audio_format': {
          'encoding': 'PCM_S16LE',
          'sample_rate_hz': 24000,
          'channels': 1,
          'extra': false,
        },
      },
    ];

    for (final json in invalidBodies) {
      final client = _client(_Transport(_success(json)), now: now);
      await expectLater(
        client.createSession(practiceSessionId: 'practice-1'),
        throwsA(
          isA<AvatarSessionTokenException>().having(
            (error) => error.failure,
            'failure',
            AvatarSessionTokenFailure.invalidResponse,
          ),
        ),
      );
    }
  });

  test('requires JSON content type, no-store, future UTC expiry', () async {
    final scenarios = [
      AvatarSessionWireResponse(
        statusCode: HttpStatus.ok,
        body: _jsonBytes(_grantJson(now)),
        headers: const {
          'content-type': 'application/json',
          'cache-control': 'private, no-store',
        },
      ),
      AvatarSessionWireResponse(
        statusCode: HttpStatus.ok,
        body: _jsonBytes(_grantJson(now)),
        headers: const {
          'content-type': 'text/plain',
          'cache-control': 'no-store',
        },
      ),
      _success(
        _grantJson(now, expiresAt: now.subtract(const Duration(seconds: 1))),
      ),
      _success({..._grantJson(now), 'expires_at': '2026-07-29T18:10:00+08:00'}),
    ];

    for (final response in scenarios) {
      final client = _client(_Transport(response), now: now);
      await expectLater(
        client.createSession(practiceSessionId: 'practice-1'),
        throwsA(isA<AvatarSessionTokenException>()),
      );
    }
  });

  test('invalidates exactly the captured app session on 401', () async {
    String? invalidatedToken;
    int? invalidatedGeneration;
    final client = WireAvatarSessionTokenClient(
      baseUri: Uri.parse('http://localhost:8080'),
      credentialProvider: () =>
          const AuthSessionCredential(sessionToken: 'sess_test', generation: 7),
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) async {
            invalidatedToken = expectedSessionToken;
            invalidatedGeneration = expectedGeneration;
          },
      transport: _Transport(
        AvatarSessionWireResponse(
          statusCode: HttpStatus.unauthorized,
          body: Uint8List(0),
        ),
      ),
      clock: () => now,
    );

    await expectLater(
      client.createSession(practiceSessionId: 'practice-1'),
      throwsA(
        isA<AvatarSessionTokenException>().having(
          (error) => error.failure,
          'failure',
          AvatarSessionTokenFailure.authenticationRequired,
        ),
      ),
    );

    expect(invalidatedToken, 'sess_test');
    expect(invalidatedGeneration, 7);
  });

  test('discards a response completed after account switch', () async {
    var credential = const AuthSessionCredential(
      sessionToken: 'sess_first',
      generation: 1,
    );
    final body = _jsonBytes(_grantJson(now));
    final transport = _PendingTransport();
    final client = WireAvatarSessionTokenClient(
      baseUri: Uri.parse('http://localhost:8080'),
      credentialProvider: () => credential,
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
      clock: () => now,
    );

    final pending = client.createSession(practiceSessionId: 'practice-1');
    await Future<void>.delayed(Duration.zero);
    credential = const AuthSessionCredential(
      sessionToken: 'sess_second',
      generation: 2,
    );
    transport.complete(
      AvatarSessionWireResponse(
        statusCode: HttpStatus.ok,
        body: body,
        headers: const {
          'content-type': 'application/json',
          'cache-control': 'no-store',
        },
      ),
    );

    await expectLater(
      pending,
      throwsA(
        isA<AvatarSessionTokenException>().having(
          (error) => error.failure,
          'failure',
          AvatarSessionTokenFailure.cancelled,
        ),
      ),
    );
    expect(body, everyElement(0));
  });

  test('zeros a response completed during account cleanup', () async {
    final body = _jsonBytes(_grantJson(now));
    final transport = _PendingTransport();
    final client = _client(transport, now: now);

    final pending = client.createSession(practiceSessionId: 'practice-1');
    await Future<void>.delayed(Duration.zero);
    final cleanup = client.clearAccountState();
    await Future<void>.delayed(Duration.zero);
    transport.complete(
      AvatarSessionWireResponse(
        statusCode: HttpStatus.ok,
        body: body,
        headers: const {
          'content-type': 'application/json',
          'cache-control': 'no-store',
        },
      ),
    );

    await expectLater(
      pending,
      throwsA(
        isA<AvatarSessionTokenException>().having(
          (error) => error.failure,
          'failure',
          AvatarSessionTokenFailure.cancelled,
        ),
      ),
    );
    await cleanup;
    expect(body, everyElement(0));
  });

  test(
    'maps every provider-side 5xx response to retryable unavailable',
    () async {
      for (final statusCode in [
        HttpStatus.internalServerError,
        HttpStatus.notImplemented,
        HttpStatus.badGateway,
      ]) {
        final client = _client(
          _Transport(
            AvatarSessionWireResponse(
              statusCode: statusCode,
              body: Uint8List(0),
            ),
          ),
          now: now,
        );
        await expectLater(
          client.createSession(practiceSessionId: 'practice-1'),
          throwsA(
            isA<AvatarSessionTokenException>()
                .having(
                  (error) => error.failure,
                  'failure',
                  AvatarSessionTokenFailure.unavailable,
                )
                .having((error) => error.retryable, 'retryable', isTrue),
          ),
        );
      }
    },
  );

  test('IO transport sends POST with an explicit zero-length body', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    final observed = Completer<({int contentLength, bool chunked})>();
    server.listen((request) async {
      observed.complete((
        contentLength: request.contentLength,
        chunked: request.headers.chunkedTransferEncoding,
      ));
      await request.drain<void>();
      request.response
        ..statusCode = HttpStatus.ok
        ..headers.contentType = ContentType.json
        ..headers.set(HttpHeaders.cacheControlHeader, 'no-store')
        ..write(jsonEncode(_grantJson(now)));
      await request.response.close();
    });
    final transport = IoAvatarSessionWireTransport(
      timeout: const Duration(seconds: 5),
    );
    addTearDown(transport.close);

    final response = await transport.send(
      AvatarSessionWireRequest(
        method: 'POST',
        uri: Uri.parse(
          'http://${server.address.address}:${server.port}/avatar-token',
        ),
        headers: const {HttpHeaders.acceptHeader: 'application/json'},
        maximumResponseBytes: 32 * 1024,
      ),
    );

    expect(await observed.future, (contentLength: 0, chunked: false));
    expect(response.statusCode, HttpStatus.ok);
  });
}

WireAvatarSessionTokenClient _client(
  AvatarSessionWireTransport transport, {
  required DateTime now,
}) {
  return WireAvatarSessionTokenClient(
    baseUri: Uri.parse('http://localhost:8080'),
    credentialProvider: () =>
        const AuthSessionCredential(sessionToken: 'sess_test', generation: 1),
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
    clock: () => now,
  );
}

Map<String, Object?> _grantJson(DateTime now, {DateTime? expiresAt}) {
  return {
    'app_id': 'app-1',
    'avatar_id': 'avatar-1',
    'session_token': 'private-avatar-token',
    'region': 'ap-northeast',
    'expires_at': (expiresAt ?? now.add(const Duration(minutes: 10)))
        .toIso8601String(),
    'audio_format': {
      'encoding': 'PCM_S16LE',
      'sample_rate_hz': 24000,
      'channels': 1,
    },
  };
}

AvatarSessionWireResponse _success(Map<String, Object?> body) {
  return AvatarSessionWireResponse(
    statusCode: HttpStatus.ok,
    body: _jsonBytes(body),
    headers: const {
      'content-type': 'application/json',
      'cache-control': 'no-store',
    },
  );
}

Uint8List _jsonBytes(Map<String, Object?> json) {
  return Uint8List.fromList(utf8.encode(jsonEncode(json)));
}

final class _Transport implements AvatarSessionWireTransport {
  _Transport(this.response);

  final AvatarSessionWireResponse response;
  AvatarSessionWireRequest? request;

  @override
  Future<AvatarSessionWireResponse> send(
    AvatarSessionWireRequest request,
  ) async {
    this.request = request;
    return response;
  }

  @override
  void close({bool force = false}) {}
}

final class _PendingTransport implements AvatarSessionWireTransport {
  final _completer = Completer<AvatarSessionWireResponse>();

  void complete(AvatarSessionWireResponse response) {
    _completer.complete(response);
  }

  @override
  Future<AvatarSessionWireResponse> send(AvatarSessionWireRequest request) {
    return _completer.future;
  }

  @override
  void close({bool force = false}) {}
}
