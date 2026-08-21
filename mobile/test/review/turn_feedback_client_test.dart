import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_client.dart';
import 'package:speakup/features/coaching/evaluation/wire_turn_feedback_client.dart';

import 'turn_feedback_fixture.dart';

void main() {
  test('wire client follows the exact Actor-owned status URL', () async {
    final fixture = readyPracticeFeedbackFixture();
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(fixture),
      ),
    );
    final client = _client(transport);

    final result = await client.getFeedback(practiceStatusUrl);

    expect(result.feedbackStatus, SpeechFeedbackStatus.ready);
    expect(transport.uri.path, practiceStatusUrl);
    expect(transport.uri.query, isEmpty);
    expect(transport.method, 'GET');
    expect(transport.authorization, 'Bearer sess_speech_feedback');
  });

  test('wire client rejects untrusted status URLs before transport', () async {
    final transport = _Transport(
      const IdentityHttpResponse(statusCode: HttpStatus.ok, body: '{}'),
    );
    final client = _client(transport);

    for (final value in [
      'https://other.test$practiceStatusUrl',
      '$practiceStatusUrl?token=secret',
      '/v1/practice-turns/%2e%2e/evaluation',
    ]) {
      await expectLater(
        client.getFeedback(value),
        throwsA(
          isA<SpeechFeedbackException>().having(
            (error) => error.kind,
            'kind',
            SpeechFeedbackFailureKind.invalidRequest,
          ),
        ),
      );
    }
    expect(transport.calls, 0);
  });

  test('wire client rejects a response for a different status URL', () async {
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(pendingPracticeFeedbackFixture('QUEUED')),
      ),
    );
    final client = _client(transport);

    await expectLater(
      client.getFeedback(agentStatusUrl),
      throwsA(
        isA<SpeechFeedbackException>().having(
          (error) => error.kind,
          'kind',
          SpeechFeedbackFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test(
    'wire client maps private resource failures without fake data',
    () async {
      for (final testCase
          in <({int status, SpeechFeedbackFailureKind kind, bool retryable})>[
            (
              status: HttpStatus.badRequest,
              kind: SpeechFeedbackFailureKind.invalidRequest,
              retryable: false,
            ),
            (
              status: HttpStatus.notFound,
              kind: SpeechFeedbackFailureKind.notFound,
              retryable: false,
            ),
            (
              status: HttpStatus.conflict,
              kind: SpeechFeedbackFailureKind.conflict,
              retryable: false,
            ),
            (
              status: HttpStatus.serviceUnavailable,
              kind: SpeechFeedbackFailureKind.server,
              retryable: true,
            ),
          ]) {
        final client = _client(
          _Transport(
            IdentityHttpResponse(
              statusCode: testCase.status,
              body: '{"error":{"code":"fixture"}}',
            ),
          ),
        );

        await expectLater(
          client.getFeedback(practiceStatusUrl),
          throwsA(
            isA<SpeechFeedbackException>()
                .having((error) => error.kind, 'kind', testCase.kind)
                .having(
                  (error) => error.retryable,
                  'retryable',
                  testCase.retryable,
                ),
          ),
        );
      }
    },
  );

  test('account clear fences a late private response', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final pending = client.getFeedback(practiceStatusUrl);
    await transport.started.future;

    await client.clearAccountState();
    transport.response.complete(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(readyPracticeFeedbackFixture()),
      ),
    );

    await expectLater(
      pending,
      throwsA(
        isA<SpeechFeedbackException>().having(
          (error) => error.kind,
          'kind',
          SpeechFeedbackFailureKind.superseded,
        ),
      ),
    );
  });
}

WireSpeechFeedbackClient _client(IdentityHttpTransport transport) {
  return WireSpeechFeedbackClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => const AuthSessionCredential(
      sessionToken: 'sess_speech_feedback',
      generation: 9,
    ),
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

final class _Transport implements IdentityHttpTransport {
  _Transport(this.response);

  final IdentityHttpResponse response;
  late Uri uri;
  late String method;
  String? authorization;
  int calls = 0;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    calls++;
    this.method = method;
    this.uri = uri;
    authorization = headers[HttpHeaders.authorizationHeader];
    return response;
  }
}

final class _CompleterTransport implements IdentityHttpTransport {
  final started = Completer<void>();
  final response = Completer<IdentityHttpResponse>();

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) {
    started.complete();
    return response.future;
  }
}
