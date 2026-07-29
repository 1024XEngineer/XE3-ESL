import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_models.dart';
import 'package:speakup/features/preparation/wire_preparation_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'reads the anonymous scenario directory without a bearer credential',
    () async {
      final transport = _QueueTransport([
        _response(<String, Object?>{
          'scenarios': [_scenarioJson],
        }),
      ]);
      final client = WirePreparationCatalogClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        transport: transport,
      );

      final scenarios = await client.listScenarios();

      expect(scenarios, hasLength(1));
      expect(scenarios.single.id, _scenarioId);
      expect(scenarios.single.name, 'English interview for technical roles');
      expect(scenarios.single.summary, 'Discuss one backend project.');
      expect(transport.calls.single.path, '/v1/scenario-definitions');
      expect(transport.calls.single.authorization, isNull);
      transport.expectDone();
    },
  );

  test('accepts a server-provided summary for each directory item', () async {
    final client = WirePreparationCatalogClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: _QueueTransport([
        _response(<String, Object?>{
          'scenarios': [
            <String, Object?>{
              ..._scenarioJson,
              'summary': 'Discuss one real backend project.',
            },
          ],
        }),
      ]),
    );

    final scenarios = await client.listScenarios();

    expect(scenarios, hasLength(1));
    expect(scenarios.single.summary, 'Discuss one real backend project.');
  });

  test('decodes detail and preserves server role and option order', () async {
    final transport = _QueueTransport([
      _response(_detailJson),
      _response(<String, Object?>{
        'roles': [_technicalRoleJson, _recruiterRoleJson],
      }),
    ]);
    final client = WirePreparationCatalogClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: transport,
    );

    final detail = await client.getScenario(_scenarioId);
    final roles = await client.listRoles(_scenarioId);

    expect(detail.config.id, 'scfg_backend_engineer');
    expect(detail.options.map((option) => option.type), const [
      PreparationOptionType.fullSimulation,
      PreparationOptionType.focus,
      PreparationOptionType.focus,
    ]);
    expect(roles.map((role) => role.id), const [
      'role_technical_interviewer',
      'role_hr_interviewer',
    ]);
    expect(transport.calls.map((call) => call.path), const [
      '/v1/scenario-definitions/$_scenarioId',
      '/v1/scenario-definitions/$_scenarioId/role-definitions',
    ]);
    expect(transport.calls.every((call) => call.authorization == null), isTrue);
    transport.expectDone();
  });

  test('accepts the four supported scenario family and model pairs', () async {
    final scenarios = <Map<String, Object?>>[
      _scenarioJson,
      {
        ..._scenarioJson,
        'scenario_definition_id': 'scn_ielts_speaking_part_2',
        'scenario_type': 'EXAM',
        'scenario_model': 'IELTS_SPEAKING_PART_2',
        'name': 'IELTS Speaking Part 2',
      },
      {
        ..._scenarioJson,
        'scenario_definition_id': 'scn_workplace_progress_risk_update',
        'scenario_type': 'WORKPLACE',
        'scenario_model': 'PROGRESS_AND_RISK_UPDATE',
        'name': 'Progress and risk update',
      },
      {
        ..._scenarioJson,
        'scenario_definition_id': 'scn_daily_hotel_checkin_issue',
        'scenario_type': 'DAILY',
        'scenario_model': 'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
        'name': 'Hotel check-in and issue handling',
      },
    ];
    final client = WirePreparationCatalogClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: _QueueTransport([
        _response(<String, Object?>{'scenarios': scenarios}),
      ]),
    );

    final result = await client.listScenarios();

    expect(result.map((scenario) => scenario.type), [
      'INTERVIEW',
      'EXAM',
      'WORKPLACE',
      'DAILY',
    ]);
  });

  test(
    'rejects unknown fields instead of inventing a client contract',
    () async {
      final transport = _QueueTransport([
        _response(<String, Object?>{
          'scenarios': [
            <String, Object?>{..._scenarioJson, 'display_order': 10},
          ],
        }),
      ]);
      final client = WirePreparationCatalogClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        transport: transport,
      );

      await expectLater(
        client.listScenarios(),
        throwsA(
          isA<PreparationCatalogException>().having(
            (error) => error.kind,
            'kind',
            PreparationCatalogFailureKind.invalidResponse,
          ),
        ),
      );
    },
  );

  test('rejects a FULL_SIMULATION option that claims one fixed role', () async {
    final invalidDetail = <String, Object?>{
      ..._detailJson,
      'practice_options': [
        <String, Object?>{
          ..._fullOptionJson,
          'role_definition_id': 'role_technical_interviewer',
        },
      ],
    };
    final transport = _QueueTransport([_response(invalidDetail)]);
    final client = WirePreparationCatalogClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: transport,
    );

    await expectLater(
      client.getScenario(_scenarioId),
      throwsA(
        isA<PreparationCatalogException>().having(
          (error) => error.kind,
          'kind',
          PreparationCatalogFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('account cleanup fences a late catalog response', () async {
    final transport = _ControlledTransport();
    final client = WirePreparationCatalogClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: transport,
    );

    final request = client.listScenarios();
    await client.clearAccountState();
    transport.complete(
      _response(<String, Object?>{
        'scenarios': [_scenarioJson],
      }),
    );

    await expectLater(
      request,
      throwsA(
        isA<PreparationCatalogException>().having(
          (error) => error.kind,
          'kind',
          PreparationCatalogFailureKind.superseded,
        ),
      ),
    );
  });
}

final class _Call {
  const _Call({required this.path, required this.authorization});

  final String path;
  final String? authorization;
}

final class _QueueTransport implements IdentityHttpTransport {
  _QueueTransport(this.responses);

  final List<IdentityHttpResponse> responses;
  final List<_Call> calls = <_Call>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    expect(method, 'GET');
    expect(body, isNull);
    if (responses.isEmpty) {
      throw StateError('Unexpected request.');
    }
    calls.add(
      _Call(
        path: uri.path,
        authorization: headers[HttpHeaders.authorizationHeader],
      ),
    );
    return responses.removeAt(0);
  }

  void expectDone() => expect(responses, isEmpty);
}

final class _ControlledTransport implements IdentityHttpTransport {
  final Completer<IdentityHttpResponse> response =
      Completer<IdentityHttpResponse>();

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) => response.future;

  void complete(IdentityHttpResponse value) => response.complete(value);
}

IdentityHttpResponse _response(Object body) =>
    IdentityHttpResponse(statusCode: HttpStatus.ok, body: jsonEncode(body));

const _scenarioId = 'scn_programmer_interview';

const _scenarioDefinitionJson = <String, Object?>{
  'scenario_definition_id': _scenarioId,
  'scenario_type': 'INTERVIEW',
  'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
  'name': 'English interview for technical roles',
  'version': 1,
  'status': 'active',
  'turn_policy_ref': 'interview.project_deep_dive.turn.v1',
  'session_policy_ref': 'interview.project_deep_dive.session.v1',
};

const _scenarioJson = <String, Object?>{
  ..._scenarioDefinitionJson,
  'summary': 'Discuss one backend project.',
};

const _configJson = <String, Object?>{
  'scenario_config_id': 'scfg_backend_engineer',
  'scenario_definition_id': _scenarioId,
  'config_type': 'INTERVIEW',
  'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
  'version': 1,
  'job_title': 'Backend engineer',
  'job_description': 'Build reliable APIs and explain engineering trade-offs.',
  'prompt_model': {
    'public_scene_brief': 'Discuss one backend project.',
    'practice_goal': 'Explain decisions with evidence.',
    'user_role': 'Candidate',
    'ai_role': 'Technical interviewer',
    'persona_summary': 'Precise and evidence seeking.',
    'focus_areas': ['introduction', 'system_design'],
    'turn_blueprints': ['Ask for a project overview.'],
    'suggested_duration_seconds': 900,
  },
};

const _fullOptionJson = <String, Object?>{
  'practice_option_id': 'option_full_simulation',
  'scenario_definition_id': _scenarioId,
  'practice_option_type': 'FULL_SIMULATION',
  'display_name': 'Full simulation',
  'version': 1,
};

const _technicalOptionJson = <String, Object?>{
  'practice_option_id': 'option_technical_focus',
  'scenario_definition_id': _scenarioId,
  'role_definition_id': 'role_technical_interviewer',
  'practice_option_type': 'FOCUS',
  'display_name': 'Technical depth focus',
  'version': 1,
};

const _recruiterOptionJson = <String, Object?>{
  'practice_option_id': 'option_hr_focus',
  'scenario_definition_id': _scenarioId,
  'role_definition_id': 'role_hr_interviewer',
  'practice_option_type': 'FOCUS',
  'display_name': 'Recruiter and motivation focus',
  'version': 1,
};

const _detailJson = <String, Object?>{
  'scenario_definition': _scenarioDefinitionJson,
  'scenario_config': _configJson,
  'practice_options': [
    _fullOptionJson,
    _technicalOptionJson,
    _recruiterOptionJson,
  ],
};

const _technicalRoleJson = <String, Object?>{
  'role_definition_id': 'role_technical_interviewer',
  'scenario_definition_id': _scenarioId,
  'role_type': 'TECHNICAL_INTERVIEWER',
  'display_name': 'Technical depth perspective',
  'responsibilities': 'Probe technical depth and decision making.',
  'style': 'Precise and evidence seeking.',
  'focus_areas': ['system_design', 'project_depth'],
  'version': 1,
};

const _recruiterRoleJson = <String, Object?>{
  'role_definition_id': 'role_hr_interviewer',
  'scenario_definition_id': _scenarioId,
  'role_type': 'HR_INTERVIEWER',
  'display_name': 'Recruiter and motivation perspective',
  'responsibilities': 'Explore motivation and communication clarity.',
  'style': 'Warm and structured.',
  'focus_areas': ['motivation', 'communication'],
  'version': 1,
};
