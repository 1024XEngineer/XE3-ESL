import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';

void main() {
  final now = DateTime.utc(2026, 7, 25, 12);

  test('question TTS uses Bearer and returns validated WAV bytes', () async {
    final api = _Transport([
      _Response(
        statusCode: HttpStatus.ok,
        body: _wave(),
        headers: const {'content-type': 'audio/wav'},
      ),
    ]);
    final client = _client(api: api, signed: _Transport([]), clock: () => now);

    final bytes = await client.loadQuestionSpeech(
      '/v1/voice-questions/question-1/speech',
    );

    expect(bytes, _wave());
    expect(api.requests.single.method, 'GET');
    expect(
      api.requests.single.uri.path,
      '/v1/voice-questions/question-1/speech',
    );
    expect(
      api.requests.single.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_practice-media',
    );
  });

  test(
    'recording metadata is protected but signed WAV fetch has no Bearer',
    () async {
      final signedUri = Uri.parse(
        'https://speakup-audio.example.test/audio.wav?x-oss-signature=opaque',
      );
      final api = _Transport([
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': signedUri.toString(),
            'expires_at': now.add(const Duration(minutes: 2)).toIso8601String(),
          }),
          headers: {
            'cache-control': 'no-store',
            'date': HttpDate.format(now),
            'content-type': 'application/json',
          },
        ),
      ]);
      final signed = _Transport([
        _Response(
          statusCode: HttpStatus.ok,
          body: _wave(),
          headers: const {'content-type': 'audio/wav'},
        ),
      ]);
      final client = _client(api: api, signed: signed, clock: () => now);

      final bytes = await client.loadRecording('audio-asset-1');

      expect(bytes, _wave());
      expect(
        api.requests.single.uri.path,
        '/v1/audio-assets/audio-asset-1/playback',
      );
      expect(
        api.requests.single.headers[HttpHeaders.authorizationHeader],
        'Bearer sess_practice-media',
      );
      final signedRequest = signed.requests.single;
      expect(signedRequest.uri, signedUri);
      expect(
        signedRequest.headers,
        isNot(contains(HttpHeaders.authorizationHeader)),
      );
      expect(signedRequest.headers.values, isNot(contains(contains('sess_'))));
    },
  );

  test(
    'recording metadata rejects plaintext and overlong capabilities',
    () async {
      for (final playback in <Map<String, Object?>>[
        {
          'playback_url': 'http://media.example.test/audio.wav',
          'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
        },
        {
          'playback_url': 'https://media.example.test/audio.wav',
          'expires_at': now
              .add(const Duration(minutes: 2, seconds: 1))
              .toIso8601String(),
        },
      ]) {
        final api = _Transport([
          _Response(
            statusCode: HttpStatus.ok,
            body: _jsonBytes(playback),
            headers: {
              'cache-control': 'no-store',
              'date': HttpDate.format(now),
              'content-type': 'application/json',
            },
          ),
        ]);
        final signed = _Transport([]);
        final client = _client(api: api, signed: signed, clock: () => now);

        await expectLater(
          client.loadRecording('audio-asset-1'),
          throwsA(
            isA<PracticeClientException>().having(
              (error) => error.kind,
              'kind',
              PracticeClientFailureKind.invalidResponse,
            ),
          ),
        );
        expect(signed.requests, isEmpty);
      }
    },
  );

  test(
    'metadata requires exact JSON fields, no-store, and JSON content type',
    () async {
      for (final response in <_Response>[
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': 'https://media.example.test/audio.wav',
            'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
            'permanent': true,
          }),
          headers: {
            'cache-control': 'no-store',
            'content-type': 'application/json',
            'date': HttpDate.format(now),
          },
        ),
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': 'https://media.example.test/audio.wav',
            'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
          }),
          headers: {
            'cache-control': 'private',
            'content-type': 'application/json',
            'date': HttpDate.format(now),
          },
        ),
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': 'https://media.example.test/audio.wav',
            'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
          }),
          headers: {
            'cache-control': 'no-store',
            'content-type': 'text/plain',
            'date': HttpDate.format(now),
          },
        ),
      ]) {
        final signed = _Transport([]);
        final client = _client(
          api: _Transport([response]),
          signed: signed,
          clock: () => now,
        );
        await expectLater(
          client.loadRecording('audio-asset-1'),
          throwsA(
            isA<PracticeClientException>().having(
              (error) => error.kind,
              'kind',
              PracticeClientFailureKind.invalidResponse,
            ),
          ),
        );
        expect(signed.requests, isEmpty);
      }
    },
  );

  test('HTTP Date precision accepts 120.999s but rejects 121s', () async {
    for (final testCase in <({Duration lifetime, bool accepted})>[
      (lifetime: const Duration(minutes: 2, milliseconds: 999), accepted: true),
      (lifetime: const Duration(minutes: 2, seconds: 1), accepted: false),
    ]) {
      final api = _Transport([
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': 'https://media.example.test/audio.wav',
            'expires_at': now.add(testCase.lifetime).toIso8601String(),
          }),
          headers: {
            'cache-control': 'no-store',
            'content-type': 'application/json; charset=utf-8',
            'date': HttpDate.format(now),
          },
        ),
      ]);
      final signed = _Transport([
        if (testCase.accepted)
          _Response(
            statusCode: HttpStatus.ok,
            body: _wave(),
            headers: const {'content-type': 'audio/wav'},
          ),
      ]);
      final client = _client(api: api, signed: signed, clock: () => now);

      if (testCase.accepted) {
        expect(await client.loadRecording('audio-1'), _wave());
      } else {
        await expectLater(
          client.loadRecording('audio-1'),
          throwsA(
            isA<PracticeClientException>().having(
              (error) => error.kind,
              'kind',
              PracticeClientFailureKind.invalidResponse,
            ),
          ),
        );
        expect(signed.requests, isEmpty);
      }
    }
  });

  test(
    'signed fetch does not follow a redirect and never gains Bearer',
    () async {
      final api = _Transport([
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url':
                'https://media.example.test/audio.wav?signature=one',
            'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
          }),
          headers: {
            'cache-control': 'no-store',
            'content-type': 'application/json',
            'date': HttpDate.format(now),
          },
        ),
      ]);
      final signed = _Transport([
        const _Response(
          statusCode: HttpStatus.found,
          headers: {'location': 'https://attacker.example.test/audio.wav'},
        ),
      ]);
      final client = _client(api: api, signed: signed, clock: () => now);

      await expectLater(
        client.loadRecording('audio-asset-1'),
        throwsA(isA<PracticeClientException>()),
      );

      expect(signed.requests, hasLength(1));
      expect(
        signed.requests.single.headers,
        isNot(contains(HttpHeaders.authorizationHeader)),
      );
    },
  );

  test('semantic layer rejects audio beyond the frozen 7.4 MB limit', () async {
    final oversized = Uint8List(7400001);
    oversized.setAll(0, ascii.encode('RIFF'));
    oversized.setAll(8, ascii.encode('WAVE'));
    final api = _Transport([
      _Response(
        statusCode: HttpStatus.ok,
        body: oversized,
        headers: const {'content-type': 'audio/wav'},
      ),
    ]);
    final client = _client(api: api, signed: _Transport([]), clock: () => now);

    await expectLater(
      client.loadQuestionSpeech('/v1/voice-questions/question-1/speech'),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.invalidResponse,
        ),
      ),
    );
    expect(api.requests.single.maximumResponseBytes, 7400000);
  });

  test('API 401 triggers compare-and-swap Session invalidation', () async {
    final invalidations = <({String token, int generation})>[];
    final api = _Transport([
      _Response(
        statusCode: HttpStatus.unauthorized,
        body: _jsonBytes({
          'error': {
            'code': 'authentication_required',
            'message': 'Authentication is required.',
            'retryable': false,
            'correlation_id': 'corr-media-401',
          },
        }),
      ),
    ]);
    final client = _client(
      api: api,
      signed: _Transport([]),
      clock: () => now,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) async {
            invalidations.add((
              token: expectedSessionToken,
              generation: expectedGeneration,
            ));
          },
    );

    await expectLater(
      client.loadQuestionSpeech('/v1/voice-questions/question-1/speech'),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.authenticationRequired,
        ),
      ),
    );
    await Future<void>.delayed(Duration.zero);
    expect(invalidations, [(token: 'sess_practice-media', generation: 1)]);
  });

  test('signed 401 and 403 never invalidate the App Session', () async {
    for (final status in <int>[HttpStatus.unauthorized, HttpStatus.forbidden]) {
      var invalidationCount = 0;
      final api = _Transport([
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': 'https://media.example.test/audio.wav',
            'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
          }),
          headers: {
            'cache-control': 'no-store',
            'content-type': 'application/json',
            'date': HttpDate.format(now),
          },
        ),
      ]);
      final signed = _Transport([_Response(statusCode: status)]);
      final client = _client(
        api: api,
        signed: signed,
        clock: () => now,
        invalidateSession:
            ({
              required expectedSessionToken,
              required expectedGeneration,
            }) async {
              invalidationCount++;
            },
      );

      await expectLater(
        client.loadRecording('audio-1'),
        throwsA(
          isA<PracticeClientException>()
              .having(
                (error) => error.kind,
                'kind',
                isNot(PracticeClientFailureKind.authenticationRequired),
              )
              .having(
                (error) => error.errorCode,
                'errorCode',
                'recording_playback_capability_rejected',
              ),
        ),
      );
      await Future<void>.delayed(Duration.zero);
      expect(invalidationCount, 0);
    }
  });

  test(
    'private metadata and transport WAV buffers are zeroed after use',
    () async {
      final questionBuffer = _wave();
      final questionClient = _client(
        api: _Transport([
          _Response(
            statusCode: HttpStatus.ok,
            body: questionBuffer,
            headers: const {'content-type': 'audio/wav'},
          ),
        ]),
        signed: _Transport([]),
        clock: () => now,
      );

      expect(
        await questionClient.loadQuestionSpeech(
          '/v1/voice-questions/question-1/speech',
        ),
        _wave(),
      );
      expect(questionBuffer, everyElement(0));

      final metadataBuffer = _jsonBytes({
        'playback_url': 'https://media.example.test/audio.wav',
        'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
      });
      final recordingBuffer = _wave();
      final recordingClient = _client(
        api: _Transport([
          _Response(
            statusCode: HttpStatus.ok,
            body: metadataBuffer,
            headers: {
              'cache-control': 'no-store',
              'content-type': 'application/json',
              'date': HttpDate.format(now),
            },
          ),
        ]),
        signed: _Transport([
          _Response(
            statusCode: HttpStatus.ok,
            body: recordingBuffer,
            headers: const {'content-type': 'audio/wav'},
          ),
        ]),
        clock: () => now,
      );

      expect(await recordingClient.loadRecording('audio-1'), _wave());
      expect(metadataBuffer, everyElement(0));
      expect(recordingBuffer, everyElement(0));
    },
  );

  test(
    'account cleanup zeroes a TTS response that arrives after logout',
    () async {
      final api = _PendingTransport();
      final client = _client(
        api: api,
        signed: _Transport([]),
        clock: () => now,
      );
      final source = _wave();

      final load = client.loadQuestionSpeech(
        '/v1/voice-questions/question-1/speech',
      );
      await api.requestStarted.future;
      final cleanup = client.clearAccountState();
      api.response.complete(
        PracticeMediaWireResponse(
          statusCode: HttpStatus.ok,
          body: source,
          headers: const {'content-type': 'audio/wav'},
        ),
      );

      await expectLater(load, throwsA(isA<PracticeClientOperationCancelled>()));
      await cleanup;
      expect(source, everyElement(0));
    },
  );

  test(
    'account cleanup zeroes a signed WAV response that arrives late',
    () async {
      final signed = _PendingTransport();
      final api = _Transport([
        _Response(
          statusCode: HttpStatus.ok,
          body: _jsonBytes({
            'playback_url': 'https://media.example.test/audio.wav',
            'expires_at': now.add(const Duration(minutes: 1)).toIso8601String(),
          }),
          headers: {
            'cache-control': 'no-store',
            'content-type': 'application/json',
            'date': HttpDate.format(now),
          },
        ),
      ]);
      final client = _client(api: api, signed: signed, clock: () => now);
      final source = _wave();

      final load = client.loadRecording('audio-1');
      await signed.requestStarted.future;
      final cleanup = client.clearAccountState();
      signed.response.complete(
        PracticeMediaWireResponse(
          statusCode: HttpStatus.ok,
          body: source,
          headers: const {'content-type': 'audio/wav'},
        ),
      );

      await expectLater(load, throwsA(isA<PracticeClientOperationCancelled>()));
      await cleanup;
      expect(source, everyElement(0));
    },
  );

  test('a response arriving after timeout is still zeroed', () async {
    final api = _PendingTransport();
    final source = _wave();
    final client = WirePracticeMediaClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_practice-media',
        generation: 1,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      apiTransport: api,
      signedAudioTransport: _Transport([]),
      clock: () => now,
      timeout: const Duration(milliseconds: 1),
    );

    final load = client.loadQuestionSpeech(
      '/v1/voice-questions/question-1/speech',
    );
    await api.requestStarted.future;
    await expectLater(
      load,
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.errorCode,
          'errorCode',
          'practice_media_request_timed_out',
        ),
      ),
    );
    api.response.complete(
      PracticeMediaWireResponse(
        statusCode: HttpStatus.ok,
        body: source,
        headers: const {'content-type': 'audio/wav'},
      ),
    );
    await Future<void>.delayed(Duration.zero);

    expect(source, everyElement(0));
  });

  test(
    'concurrent account clears create only the latest transport pair',
    () async {
      final transports = <_Transport>[];
      final client = WirePracticeMediaClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        credentialProvider: () => const AuthSessionCredential(
          sessionToken: 'sess_practice-media',
          generation: 1,
        ),
        invalidateSession:
            ({
              required expectedSessionToken,
              required expectedGeneration,
            }) async {},
        transportFactory: () {
          final transport = _Transport([]);
          transports.add(transport);
          return transport;
        },
        clock: () => now,
      );

      await Future.wait([
        client.clearAccountState(),
        client.clearAccountState(),
      ]);

      expect(transports, hasLength(4));
      expect(transports[0].closeCount, 2);
      expect(transports[1].closeCount, 2);
      expect(transports[2].closeCount, 0);
      expect(transports[3].closeCount, 0);
      await client.dispose();
      expect(transports[2].closeCount, 1);
      expect(transports[3].closeCount, 1);
    },
  );

  test('dispose racing a clear never resurrects a transport', () async {
    final transports = <_Transport>[];
    final client = WirePracticeMediaClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_practice-media',
        generation: 1,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transportFactory: () {
        final transport = _Transport([]);
        transports.add(transport);
        return transport;
      },
      clock: () => now,
    );

    await Future.wait([client.clearAccountState(), client.dispose()]);

    expect(transports, hasLength(2));
    expect(transports[0].closeCount, 2);
    expect(transports[1].closeCount, 2);
  });

  test('recording delete is Bearer protected and repeatable as 204', () async {
    final api = _Transport([
      const _Response(statusCode: HttpStatus.noContent),
      const _Response(statusCode: HttpStatus.noContent),
    ]);
    final client = _client(api: api, signed: _Transport([]), clock: () => now);

    await client.deleteRecording('audio-asset-1');
    await client.deleteRecording('audio-asset-1');

    expect(api.requests, hasLength(2));
    for (final request in api.requests) {
      expect(request.method, 'DELETE');
      expect(request.uri.path, '/v1/audio-assets/audio-asset-1');
      expect(
        request.headers[HttpHeaders.authorizationHeader],
        'Bearer sess_practice-media',
      );
    }
  });

  test('empty and invalid resource IDs are rejected', () async {
    final api = _Transport([]);
    final client = _client(api: api, signed: _Transport([]), clock: () => now);

    for (final value in <String>[
      '',
      ' ',
      List<String>.filled(129, 'a').join(),
    ]) {
      await expectLater(
        client.deleteRecording(value),
        throwsA(isA<ArgumentError>()),
      );
    }
    expect(api.requests, isEmpty);
  });
}

WirePracticeMediaClient _client({
  required PracticeMediaWireTransport api,
  required PracticeMediaWireTransport signed,
  required PracticeMediaClock clock,
  AuthSessionInvalidator? invalidateSession,
}) {
  return WirePracticeMediaClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => const AuthSessionCredential(
      sessionToken: 'sess_practice-media',
      generation: 1,
    ),
    invalidateSession:
        invalidateSession ??
        ({required expectedSessionToken, required expectedGeneration}) async {},
    apiTransport: api,
    signedAudioTransport: signed,
    clock: clock,
  );
}

Uint8List _wave() {
  final bytes = Uint8List(44);
  bytes.setAll(0, ascii.encode('RIFF'));
  bytes.setAll(8, ascii.encode('WAVE'));
  return bytes;
}

Uint8List _jsonBytes(Object value) =>
    Uint8List.fromList(utf8.encode(jsonEncode(value)));

final class _Response {
  const _Response({
    required this.statusCode,
    this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final Uint8List? body;
  final Map<String, String> headers;
}

final class _Transport implements PracticeMediaWireTransport {
  _Transport(Iterable<_Response> responses)
    : _responses = Queue<_Response>.of(responses);

  final Queue<_Response> _responses;
  final List<PracticeMediaWireRequest> requests = [];
  int closeCount = 0;

  @override
  Future<PracticeMediaWireResponse> send(
    PracticeMediaWireRequest request,
  ) async {
    requests.add(request);
    final response = _responses.removeFirst();
    return PracticeMediaWireResponse(
      statusCode: response.statusCode,
      body: response.body ?? Uint8List(0),
      headers: response.headers,
    );
  }

  @override
  void close({bool force = false}) {
    closeCount++;
  }
}

final class _PendingTransport implements PracticeMediaWireTransport {
  final Completer<void> requestStarted = Completer<void>();
  final Completer<PracticeMediaWireResponse> response =
      Completer<PracticeMediaWireResponse>();

  @override
  Future<PracticeMediaWireResponse> send(PracticeMediaWireRequest request) {
    if (!requestStarted.isCompleted) {
      requestStarted.complete();
    }
    return response.future;
  }

  @override
  void close({bool force = false}) {}
}
