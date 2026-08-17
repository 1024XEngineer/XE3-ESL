import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/interview/wire_job_preparation_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

import '../../support/preparation_contract_fixtures.dart';

void main() {
  test(
    'Interview and Resume flow creates the confirmed aggregate and Session',
    () async {
      final confirmedPreparation = contractInterviewPreparationJson(
        status: InterviewPreparationStatus.confirmed,
        version: 2,
      );
      final plan = contractPlan(includeInterview: true);
      final transport = _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.created, contractInterviewPreparationJson()),
        _response(HttpStatus.ok, confirmedPreparation),
        _response(HttpStatus.created, contractPlanJson(includeInterview: true)),
        _response(HttpStatus.created, contractBootstrapJson(plan)),
      ]);
      final client = _client(transport);
      const resume = InterviewResumeFile(
        name: 'backend-resume.pdf',
        bytes: <int>[0x25, 0x50, 0x44, 0x46, 0x2d, 0x31, 0x2e, 0x37],
      );

      final preparation = await client.createInterviewPreparation(
        input: contractInterviewInput,
        resume: resume,
        idempotencyKey: 'interview-create-key',
      );
      final confirmed = await client.confirmInterviewPreparation(
        interviewPreparationId: preparation.id,
        expectedVersion: preparation.version,
        candidate: preparation.candidate,
        idempotencyKey: 'interview-confirm-key',
      );
      final createdPlan = await client.createPlan(
        input: CreatePracticePlanInput(
          backgroundSummary: contractBackground,
          interviewPreparationId: confirmed.id,
          expectedInterviewVersion: confirmed.version,
          sceneId: 'project-deep-dive',
          sceneVersion: 1,
          selectedRoleIds: const <String>['technical-interviewer'],
          practiceOptionId: 'full-simulation',
        ),
        idempotencyKey: 'interview-plan-key',
      );
      final bootstrap = await client.createSession(
        plan: createdPlan,
        input: CreatePreparationSessionInput(
          expectedPlanVersion: createdPlan.version,
        ),
        idempotencyKey: 'interview-session-key',
      );

      expect(confirmed.status, InterviewPreparationStatus.confirmed);
      expect(confirmed.resumeUsed, isTrue);
      expect(
        createdPlan.preparationSnapshot.interview?.id,
        contractInterviewId,
      );
      expect(bootstrap.session.id, contractSessionId);
      expect(transport.calls.map((call) => call.uri.path), <String>[
        '/v1/interview-preparations',
        '/v1/interview-preparations/$contractInterviewId',
        '/v1/practice-plans',
        '/v1/practice-plans/$contractPlanId/practice-sessions',
      ]);
      final createCall = transport.calls.first;
      expect(createCall.body, isNull);
      expect(
        createCall.headers[HttpHeaders.contentTypeHeader],
        'multipart/form-data; boundary=speakup-interview-interview-create-key',
      );
      final multipart = utf8.decode(createCall.bodyBytes!);
      expect(multipart, contains('name="input"'));
      expect(
        multipart,
        contains(
          jsonEncode(<String, Object?>{
            'source': 'job_description',
            'job_description': contractInterviewInput.jobDescription,
            'candidate_background': contractInterviewInput.candidateBackground,
            'practice_focus': contractInterviewInput.practiceFocus,
          }),
        ),
      );
      expect(
        multipart,
        contains('name="resume"; filename="backend-resume.pdf"'),
      );
      expect(multipart, contains('%PDF-1.7'));
      final confirmationBody =
          jsonDecode(transport.calls[1].body!) as Map<String, Object?>;
      expect(confirmationBody['expected_version'], 1);
      expect(confirmationBody['action'], 'confirm');
      expect(confirmationBody, contains('candidate'));
      expect(jsonDecode(transport.calls[2].body!), <String, Object?>{
        'background_summary': contractBackground,
        'interview_preparation_id': contractInterviewId,
        'expected_interview_version': 2,
        'scene_id': 'project-deep-dive',
        'scene_version': 1,
        'selected_role_ids': <String>['technical-interviewer'],
        'practice_option_id': 'full-simulation',
      });
      expect(jsonDecode(transport.calls.last.body!), <String, Object?>{
        'expected_plan_version': 1,
      });
    },
  );

  test(
    'Plan confirmation uses version, never the removed revision alias',
    () async {
      final transport = _QueueTransport(<IdentityHttpResponse>[
        _response(
          HttpStatus.ok,
          contractPlanJson(status: PracticePlanStatus.ready, version: 2),
        ),
      ]);
      final client = _client(transport);

      await client.confirmPlan(
        planId: contractPlanId,
        expectedVersion: 1,
        idempotencyKey: 'agent-confirm-key',
      );

      expect(jsonDecode(transport.calls.single.body!), <String, Object?>{
        'expected_version': 1,
      });
    },
  );

  test('rejects a non-v4 persisted aggregate id before transport', () async {
    final transport = _QueueTransport(<IdentityHttpResponse>[]);
    final client = _client(transport);

    await expectLater(
      client.getInterviewPreparation('interview-preparation-1'),
      throwsA(isA<JobPreparationException>()),
    );
    expect(transport.calls, isEmpty);
  });

  test('archives a practice plan with DELETE', () async {
    final transport = _QueueTransport(<IdentityHttpResponse>[
      _response(HttpStatus.noContent, const <String, Object?>{}),
    ]);
    final client = _client(transport);

    await client.deletePlan(contractPlanId);

    expect(transport.calls.single.method, 'DELETE');
    expect(
      transport.calls.single.uri.path,
      '/v1/practice-plans/$contractPlanId',
    );
  });
}

WireJobPreparationClient _client(IdentityHttpTransport transport) {
  const credential = AuthSessionCredential(
    sessionToken: 'sess_account-a',
    generation: 1,
  );
  return WireJobPreparationClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => credential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

IdentityHttpResponse _response(int status, Map<String, Object?> body) =>
    IdentityHttpResponse(statusCode: status, body: jsonEncode(body));

final class _TransportCall {
  const _TransportCall({
    required this.method,
    required this.uri,
    required this.headers,
    required this.body,
    required this.bodyBytes,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final String? body;
  final List<int>? bodyBytes;
}

final class _QueueTransport implements IdentityHttpTransport {
  _QueueTransport(this.responses);

  final List<IdentityHttpResponse> responses;
  final List<_TransportCall> calls = <_TransportCall>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    calls.add(
      _TransportCall(
        method: method,
        uri: uri,
        headers: Map<String, String>.of(headers),
        body: body,
        bodyBytes: bodyBytes == null ? null : List<int>.of(bodyBytes),
      ),
    );
    return responses.removeAt(0);
  }
}
