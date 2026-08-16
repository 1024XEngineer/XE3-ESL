import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/wire_preparation_launch_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

import '../../support/preparation_contract_fixtures.dart';

void main() {
  test(
    'direct Scene creates one Plan then freezes its version in Session',
    () async {
      final plan = contractPlan();
      final transport = _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.created, contractPlanJson()),
        _response(HttpStatus.created, contractBootstrapJson(plan)),
      ]);
      final client = _client(transport);

      final created = await client.createPlan(
        input: const CreatePracticePlanInput(
          backgroundSummary: contractBackground,
          sceneId: 'project-deep-dive',
          sceneVersion: 1,
          selectedRoleIds: <String>['technical-interviewer'],
          practiceOptionId: 'full-simulation',
        ),
        idempotencyKey: 'direct-plan-key',
      );
      final bootstrap = await client.createSession(
        plan: created,
        input: CreatePreparationSessionInput(
          expectedPlanVersion: created.version,
        ),
        idempotencyKey: 'direct-session-key',
      );

      expect(bootstrap.session.id, contractSessionId);
      expect(bootstrap.session.planVersion, created.version);
      expect(transport.calls.map((call) => call.uri.path), <String>[
        '/v1/practice-plans',
        '/v1/practice-plans/$contractPlanId/practice-sessions',
      ]);
      expect(jsonDecode(transport.calls.first.body!), <String, Object?>{
        'background_summary': contractBackground,
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
    'Agent draft confirmation uses expected_version before Session creation',
    () async {
      final transport = _QueueTransport(<IdentityHttpResponse>[
        _response(
          HttpStatus.ok,
          contractPlanJson(status: PracticePlanStatus.ready, version: 2),
        ),
      ]);
      final client = _client(transport);

      final confirmed = await client.confirmPlan(
        planId: contractPlanId,
        expectedVersion: 1,
        idempotencyKey: 'confirm-plan-key',
      );

      expect(confirmed.status, PracticePlanStatus.ready);
      expect(confirmed.version, 2);
      expect(
        transport.calls.single.uri.path,
        '/v1/practice-plans/$contractPlanId/confirm',
      );
      expect(jsonDecode(transport.calls.single.body!), <String, Object?>{
        'expected_version': 1,
      });
    },
  );

  test('rejects the removed plan_revision response field', () async {
    final response = contractPlanJson();
    response['plan_revision'] = response.remove('version');
    final client = _client(
      _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.created, response),
      ]),
    );

    await expectLater(
      client.createPlan(
        input: const CreatePracticePlanInput(
          backgroundSummary: contractBackground,
          sceneId: 'project-deep-dive',
          sceneVersion: 1,
          selectedRoleIds: <String>['technical-interviewer'],
          practiceOptionId: 'full-simulation',
        ),
        idempotencyKey: 'strict-plan-key',
      ),
      throwsA(
        isA<PreparationLaunchException>().having(
          (error) => error.kind,
          'kind',
          PreparationLaunchFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('account cleanup fences an in-flight response', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final operation = client.getPlan(contractPlanId);

    await client.clearAccountState();
    transport.complete(_response(HttpStatus.ok, contractPlanJson()));

    await expectLater(
      operation,
      throwsA(
        isA<PreparationLaunchException>().having(
          (error) => error.kind,
          'kind',
          PreparationLaunchFailureKind.superseded,
        ),
      ),
    );
  });
}

WirePreparationLaunchClient _client(IdentityHttpTransport transport) {
  const credential = AuthSessionCredential(
    sessionToken: 'sess_account-a',
    generation: 1,
  );
  return WirePreparationLaunchClient(
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
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final String? body;
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
      ),
    );
    return responses.removeAt(0);
  }
}

final class _CompleterTransport implements IdentityHttpTransport {
  final Completer<IdentityHttpResponse> _response =
      Completer<IdentityHttpResponse>();

  void complete(IdentityHttpResponse response) => _response.complete(response);

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) => _response.future;
}
