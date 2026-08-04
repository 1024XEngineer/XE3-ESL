import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_wire_client.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  test('wire client fetches the exact Actor-owned report resource', () async {
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(ieltsSpeakingReportContractFixture()['ready']),
      ),
    );
    final client = _client(transport);

    final result = await client.getReport('session_ielts_report_001');

    expect(result.evaluationStatus, IeltsSpeakingReportEvaluationStatus.ready);
    expect(
      transport.uri.path,
      '/v1/practice-sessions/session_ielts_report_001/ielts-speaking-report',
    );
    expect(transport.uri.query, isEmpty);
    expect(transport.authorization, 'Bearer sess_ielts_report');
    expect(transport.method, 'GET');
  });

  test('wire client accepts a digit-leading Practice session UUID', () async {
    const practiceSessionId = '20000000-0000-4000-8000-000000000001';
    final ready = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['ready'],
    );
    ready['practice_session_id'] = practiceSessionId;
    ready['status_url'] =
        '/v1/practice-sessions/$practiceSessionId/ielts-speaking-report';
    final transport = _Transport(
      IdentityHttpResponse(statusCode: HttpStatus.ok, body: jsonEncode(ready)),
    );
    final client = _client(transport);

    final result = await client.getReport(practiceSessionId);

    expect(result.practiceSessionId, practiceSessionId);
    expect(
      transport.uri.path,
      '/v1/practice-sessions/$practiceSessionId/ielts-speaking-report',
    );
  });

  test('wire client fetches the explicit IELTS report index', () async {
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(ieltsSpeakingReportContractFixture()['index_page']),
      ),
    );
    final client = _client(transport);

    final result = await client.listReports(
      cursor: 'eyJpZCI6ImlsdHNfMDAxIn0',
      limit: 25,
    );

    expect(result.items, hasLength(1));
    expect(transport.uri.path, '/v1/ielts-speaking-reports');
    expect(transport.uri.queryParameters['limit'], '25');
    expect(transport.uri.queryParameters['cursor'], 'eyJpZCI6ImlsdHNfMDAxIn0');
    expect(transport.authorization, 'Bearer sess_ielts_report');
  });

  test('first index page omits rather than empties the cursor', () async {
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(ieltsSpeakingReportContractFixture()['index_page']),
      ),
    );
    final client = _client(transport);

    await client.listReports();

    expect(transport.uri.queryParameters, <String, String>{'limit': '20'});
  });

  test('wire client maps resource failures without fabricating data', () async {
    for (final testCase
        in <
          ({int status, IeltsSpeakingReportFailureKind kind, bool retryable})
        >[
          (
            status: HttpStatus.notFound,
            kind: IeltsSpeakingReportFailureKind.notFound,
            retryable: false,
          ),
          (
            status: HttpStatus.conflict,
            kind: IeltsSpeakingReportFailureKind.conflict,
            retryable: false,
          ),
          (
            status: HttpStatus.serviceUnavailable,
            kind: IeltsSpeakingReportFailureKind.server,
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
        client.getReport('session_ielts_report_001'),
        throwsA(
          isA<IeltsSpeakingReportException>()
              .having((error) => error.kind, 'kind', testCase.kind)
              .having(
                (error) => error.retryable,
                'retryable',
                testCase.retryable,
              ),
        ),
      );
    }
  });

  test('account clear fences a late private report response', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final pending = client.getReport('session_ielts_report_001');
    await transport.started.future;

    await client.clearAccountState();
    transport.response.complete(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(ieltsSpeakingReportContractFixture()['ready']),
      ),
    );

    await expectLater(
      pending,
      throwsA(
        isA<IeltsSpeakingReportException>().having(
          (error) => error.kind,
          'kind',
          IeltsSpeakingReportFailureKind.superseded,
        ),
      ),
    );
  });

  test('wire client rejects unsafe identifiers, cursors, and limits', () {
    final client = _client(
      _Transport(
        const IdentityHttpResponse(statusCode: HttpStatus.ok, body: '{}'),
      ),
    );

    expect(
      client.getReport('../other-account'),
      throwsA(isA<IeltsSpeakingReportException>()),
    );
    expect(
      client.listReports(cursor: 'short'),
      throwsA(isA<IeltsSpeakingReportException>()),
    );
    expect(
      client.listReports(limit: 101),
      throwsA(isA<IeltsSpeakingReportException>()),
    );
  });
}

WireIeltsSpeakingReportClient _client(IdentityHttpTransport transport) =>
    WireIeltsSpeakingReportClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_ielts_report',
        generation: 7,
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

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
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
  }) {
    started.complete();
    return response.future;
  }
}
