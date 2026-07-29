import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_client.dart';
import 'package:speakup/review/wire_interview_report_client.dart';

import 'interview_report_fixture.dart';

void main() {
  test(
    'wire client sends Bearer and decodes the exact report resource',
    () async {
      final ready = interviewReportContractFixture()['ready']!;
      final transport = _Transport(
        IdentityHttpResponse(
          statusCode: HttpStatus.ok,
          body: jsonEncode(ready),
        ),
      );
      final client = _client(transport);

      final report = await client.getReport('session_interview_report_001');

      expect(report.evaluationStatus, InterviewReportEvaluationStatus.ready);
      expect(
        transport.uri.path,
        '/v1/practice-sessions/session_interview_report_001/interview-report',
      );
      expect(transport.authorization, 'Bearer sess_interview_report');
      expect(transport.method, 'GET');
    },
  );

  test('wire client accepts a digit-leading Practice session UUID', () async {
    const practiceSessionId = '20000000-0000-4000-8000-000000000001';
    final ready = cloneInterviewReportFixture(
      interviewReportContractFixture()['ready'],
    );
    ready['practice_session_id'] = practiceSessionId;
    ready['status_url'] =
        '/v1/practice-sessions/$practiceSessionId/interview-report';
    final transport = _Transport(
      IdentityHttpResponse(statusCode: HttpStatus.ok, body: jsonEncode(ready)),
    );
    final client = _client(transport);

    final report = await client.getReport(practiceSessionId);

    expect(report.practiceSessionId, practiceSessionId);
    expect(
      transport.uri.path,
      '/v1/practice-sessions/$practiceSessionId/interview-report',
    );
  });

  test(
    'wire client maps report HTTP failures without fabricating data',
    () async {
      for (final testCase
          in <({int status, InterviewReportFailureKind kind, bool retryable})>[
            (
              status: HttpStatus.notFound,
              kind: InterviewReportFailureKind.notFound,
              retryable: false,
            ),
            (
              status: HttpStatus.conflict,
              kind: InterviewReportFailureKind.conflict,
              retryable: false,
            ),
            (
              status: HttpStatus.serviceUnavailable,
              kind: InterviewReportFailureKind.server,
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
          client.getReport('session_interview_report_001'),
          throwsA(
            isA<InterviewReportException>()
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

  test('account clear fences a late report response', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final pending = client.getReport('session_interview_report_001');
    await transport.started.future;

    await client.clearAccountState();
    transport.response.complete(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode(interviewReportContractFixture()['ready']),
      ),
    );

    await expectLater(
      pending,
      throwsA(
        isA<InterviewReportException>().having(
          (error) => error.kind,
          'kind',
          InterviewReportFailureKind.superseded,
        ),
      ),
    );
  });

  test('wire client rejects a path-unsafe or overlong session identifier', () {
    final client = _client(
      _Transport(
        const IdentityHttpResponse(statusCode: HttpStatus.ok, body: '{}'),
      ),
    );

    expect(
      client.getReport('../other-account'),
      throwsA(isA<InterviewReportException>()),
    );
    expect(
      client.getReport('s${'a' * 128}'),
      throwsA(isA<InterviewReportException>()),
    );
  });
}

WireInterviewReportClient _client(IdentityHttpTransport transport) =>
    WireInterviewReportClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_interview_report',
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
