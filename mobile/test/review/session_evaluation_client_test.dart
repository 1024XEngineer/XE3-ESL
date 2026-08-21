import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

import 'evaluation_report_fixture.dart';

void main() {
  test('loads the canonical actor-owned session evaluation URL', () async {
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(_readyResource()),
      ),
    );
    final client = _client(transport);

    final result = await client.get(_sessionId);

    expect(result.status, SessionEvaluationStatus.ready);
    expect(transport.uri.path, '/v1/practice-sessions/$_sessionId/evaluation');
    expect(transport.method, 'GET');
    expect(transport.authorization, 'Bearer sess_session_evaluation_token');
  });

  test('retries through the actor-owned session evaluation command', () async {
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.accepted,
        body: jsonEncode(_queuedResource()),
      ),
    );

    final result = await _client(transport).retry(_sessionId);

    expect(result.status, SessionEvaluationStatus.queued);
    expect(
      transport.uri.path,
      '/v1/practice-sessions/$_sessionId/evaluation/retry',
    );
    expect(transport.method, 'POST');
  });

  test('rejects invalid session ids before transport', () async {
    final transport = _Transport(
      const IdentityHttpResponse(statusCode: HttpStatus.ok, body: '{}'),
    );

    await expectLater(
      _client(transport).get('../evaluation'),
      throwsA(
        isA<SessionEvaluationException>().having(
          (error) => error.kind,
          'kind',
          SessionEvaluationFailureKind.invalidRequest,
        ),
      ),
    );
    expect(transport.calls, 0);
  });

  test('account clear fences a late response', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final pending = client.get(_sessionId);
    await transport.started.future;

    await client.clearAccountState();
    transport.response.complete(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(_readyResource()),
      ),
    );

    await expectLater(
      pending,
      throwsA(
        isA<SessionEvaluationException>().having(
          (error) => error.kind,
          'kind',
          SessionEvaluationFailureKind.superseded,
        ),
      ),
    );
  });
}

Map<String, Object?> _readyResource() {
  final stored = evaluationReportWireFixture(practiceSessionId: _sessionId);
  return <String, Object?>{
    'evaluation_id': stored['evaluation_id'],
    'kind': 'SESSION_REPORT',
    'source_id': _sessionId,
    'context_id': _sessionId,
    'status': 'READY',
    'created_at': stored['created_at'],
    'updated_at': stored['created_at'],
    'feedback_items': <Object?>[],
    'result': stored['report'],
  };
}

Map<String, Object?> _queuedResource() {
  final ready = _readyResource();
  ready['status'] = 'QUEUED';
  ready.remove('result');
  return ready;
}

WireSessionEvaluationClient _client(IdentityHttpTransport transport) =>
    WireSessionEvaluationClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_session_evaluation_token',
        generation: 1,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
    );

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

const _sessionId = '30000000-0000-4000-8000-000000000003';
