import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';
import 'package:speakup/features/preparation/wire_preparation_launch_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'sends the exact authenticated Profile to Session production chain',
    () async {
      final transport = _QueueTransport([
        _response(_profileJson()),
        _response(_snapshotJson()),
        _response(_planJson()),
        _response(_bootstrapJson()),
      ]);
      final client = _client(transport);

      final profile = await client.createProfile(
        input: const CreatePreparationProfileInput(
          backgroundSummary: _background,
        ),
        idempotencyKey: 'profile-key-123',
      );
      final snapshot = await client.createSnapshot(
        profileId: profile.id,
        sourceVersion: profile.version,
        idempotencyKey: 'snapshot-key-123',
      );
      final plan = await client.createPlan(
        input: CreatePreparationPlanInput(
          context: _context,
          selection: _selection,
          preparationProfileId: profile.id,
          preparationSnapshotId: snapshot.id,
          preparationUserId: profile.userId,
        ),
        idempotencyKey: 'plan-key-123456',
      );
      final bootstrap = await client.createSession(
        planId: plan.id,
        input: CreatePreparationSessionInput(
          agentThreadId: _threadId,
          expectedPlanRevision: plan.revision,
          preparationSnapshotId: snapshot.id,
          preparationProfileId: profile.id,
          preparationProfileVersion: profile.version,
          preparationUserId: profile.userId,
          backgroundSummary: profile.backgroundSummary,
          selection: _selection,
        ),
        idempotencyKey: 'session-key-123',
      );

      expect(bootstrap.session.id, _sessionId);
      expect(transport.calls.map((call) => call.uri.path), [
        '/v1/preparation-profiles',
        '/v1/preparation-profiles/$_profileId/snapshots',
        '/v1/practice-plans',
        '/v1/agent-threads/$_threadId/practice-start-confirmations',
      ]);
      expect(jsonDecode(transport.calls.first.body!), {
        'background_summary': _background,
      });
      expect(jsonDecode(transport.calls[2].body!), {
        'agent_thread_id': _threadId,
        'matter_id': _matterId,
        'scenario_definition_id': _scenarioId,
        'scenario_definition_version': 1,
        'scenario_config_id': _configId,
        'scenario_config_version': 1,
        'preparation_profile_id': _profileId,
        'preparation_snapshot_id': _preparationSnapshotId,
        'selected_role_ids': [_roleId],
        'practice_option_id': _optionId,
        'practice_option_version': 1,
        'max_effective_turns': 3,
      });
      expect(jsonDecode(transport.calls.last.body!), {
        'expected_plan_revision': 1,
        'user_confirmed': true,
        'practice_plan_id': _planId,
      });
      for (final call in transport.calls) {
        expect(
          call.headers[HttpHeaders.authorizationHeader],
          'Bearer sess_account-a',
        );
        expect(call.headers[HttpHeaders.contentTypeHeader], 'application/json');
        expect(call.headers['Idempotency-Key'], isNotEmpty);
      }
    },
  );

  test(
    'marks malformed 201 creates ambiguous and retries Session with one key',
    () async {
      final transport = _QueueTransport([
        _rawResponse(HttpStatus.created, '{"truncated":'),
        _rawResponse(HttpStatus.created, '{"truncated":'),
        _rawResponse(HttpStatus.created, '{"truncated":'),
        _rawResponse(HttpStatus.created, '{"truncated":'),
        _response(_bootstrapJson()),
      ]);
      final client = _client(transport);
      const sessionInput = CreatePreparationSessionInput(
        agentThreadId: _threadId,
        expectedPlanRevision: 1,
        preparationSnapshotId: _preparationSnapshotId,
        preparationProfileId: _profileId,
        preparationProfileVersion: 1,
        preparationUserId: _userId,
        backgroundSummary: _background,
        selection: _selection,
      );
      final operations =
          <({PreparationLaunchStage stage, Future<Object?> Function() run})>[
            (
              stage: PreparationLaunchStage.profile,
              run: () => client.createProfile(
                input: const CreatePreparationProfileInput(
                  backgroundSummary: _background,
                ),
                idempotencyKey: 'profile-malformed-key',
              ),
            ),
            (
              stage: PreparationLaunchStage.snapshot,
              run: () => client.createSnapshot(
                profileId: _profileId,
                sourceVersion: 1,
                idempotencyKey: 'snapshot-malformed-key',
              ),
            ),
            (
              stage: PreparationLaunchStage.plan,
              run: () => client.createPlan(
                input: const CreatePreparationPlanInput(
                  context: _context,
                  selection: _selection,
                  preparationProfileId: _profileId,
                  preparationSnapshotId: _preparationSnapshotId,
                  preparationUserId: _userId,
                ),
                idempotencyKey: 'plan-malformed-key',
              ),
            ),
            (
              stage: PreparationLaunchStage.session,
              run: () => client.createSession(
                planId: _planId,
                input: sessionInput,
                idempotencyKey: 'session-malformed-key',
              ),
            ),
          ];

      for (final operation in operations) {
        await expectLater(
          operation.run(),
          throwsA(
            isA<PreparationLaunchException>()
                .having(
                  (error) => error.kind,
                  'kind',
                  PreparationLaunchFailureKind.invalidResponse,
                )
                .having((error) => error.stage, 'stage', operation.stage)
                .having(
                  (error) => error.statusCode,
                  'statusCode',
                  HttpStatus.created,
                )
                .having((error) => error.retryable, 'retryable', isTrue),
          ),
        );
      }

      final bootstrap = await client.createSession(
        planId: _planId,
        input: sessionInput,
        idempotencyKey: 'session-malformed-key',
      );

      expect(bootstrap.session.id, _sessionId);
      final sessionCalls = transport.calls.where(
        (call) => call.uri.path.endsWith('/practice-start-confirmations'),
      );
      expect(sessionCalls, hasLength(2));
      expect(
        sessionCalls.map((call) => call.headers['Idempotency-Key']),
        everyElement('session-malformed-key'),
      );
    },
  );

  test(
    'keeps a definite 400 create validation failure non-retryable',
    () async {
      final client = _client(
        _QueueTransport([
          _rawResponse(
            HttpStatus.badRequest,
            jsonEncode({
              'error': {'code': 'invalid_request'},
            }),
          ),
        ]),
      );

      await expectLater(
        client.createProfile(
          input: const CreatePreparationProfileInput(
            backgroundSummary: _background,
          ),
          idempotencyKey: 'profile-validation-key',
        ),
        throwsA(
          isA<PreparationLaunchException>()
              .having(
                (error) => error.kind,
                'kind',
                PreparationLaunchFailureKind.invalidRequest,
              )
              .having((error) => error.statusCode, 'statusCode', 400)
              .having((error) => error.retryable, 'retryable', isFalse),
        ),
      );
    },
  );

  test('rejects a Plan owned by a different user', () async {
    final response = _planJson()..['user_id'] = 'user-other';
    final client = _client(_QueueTransport([_response(response)]));

    await expectLater(
      client.createPlan(
        input: const CreatePreparationPlanInput(
          context: _context,
          selection: _selection,
          preparationProfileId: _profileId,
          preparationSnapshotId: _preparationSnapshotId,
          preparationUserId: _userId,
        ),
        idempotencyKey: 'plan-key-123456',
      ),
      throwsA(_invalidResponse),
    );
  });

  test('rejects a Plan bound to a different Agent Matter', () async {
    final response = _planJson()..['matter_id'] = 'matter-other';
    final client = _client(_QueueTransport([_response(response)]));

    await expectLater(
      client.createPlan(
        input: const CreatePreparationPlanInput(
          context: _context,
          selection: _selection,
          preparationProfileId: _profileId,
          preparationSnapshotId: _preparationSnapshotId,
          preparationUserId: _userId,
        ),
        idempotencyKey: 'plan-key-123456',
      ),
      throwsA(_invalidResponse),
    );
  });

  group('rejects cross-resource Session bootstrap data', () {
    final cases = <String, void Function(Map<String, Object?>)>{
      'role version': (root) {
        _roleSnapshot(root)['version'] = 99;
      },
      'role scenario': (root) {
        _roleSnapshot(root)['scenario_definition_id'] = 'scenario-other';
      },
      'background': (root) {
        _preparationSnapshot(root)['background_snapshot'] = 'Other account';
      },
      'source profile': (root) {
        _preparationSnapshot(root)['source_profile_id'] = 'profile-other';
      },
      'source version': (root) {
        _preparationSnapshot(root)['source_version'] = 2;
      },
      'unexpected resume snapshot': (root) {
        _preparationSnapshot(root)['resume_snapshot'] = 'Unexpected';
      },
      'candidate user': (root) {
        _candidateSubject(root)['subject_id'] = 'user-other';
      },
      'option turn budget': (root) {
        _sessionPolicy(root)['max_effective_turns'] = 6;
      },
      'missing turn policy reference': (root) {
        _scenarioDefinitionSnapshot(root).remove('turn_policy_ref');
      },
      'invalid session policy reference': (root) {
        _scenarioDefinitionSnapshot(root)['session_policy_ref'] = '';
      },
    };

    for (final entry in cases.entries) {
      test(entry.key, () async {
        final response = _bootstrapJson();
        entry.value(response);
        final client = _client(_QueueTransport([_response(response)]));

        await expectLater(
          client.createSession(
            planId: _planId,
            input: const CreatePreparationSessionInput(
              agentThreadId: _threadId,
              expectedPlanRevision: 1,
              preparationSnapshotId: _preparationSnapshotId,
              preparationProfileId: _profileId,
              preparationProfileVersion: 1,
              preparationUserId: _userId,
              backgroundSummary: _background,
              selection: _selection,
            ),
            idempotencyKey: 'session-key-123',
          ),
          throwsA(_invalidResponse),
        );
      });
    }
  });

  test('accepts the frozen six-turn full simulation budget', () async {
    final response = _bootstrapJson();
    final snapshot = response['snapshot']! as Map<String, Object?>;
    final option = snapshot['practice_option']! as Map<String, Object?>;
    option
      ..remove('role_definition_id')
      ..['practice_option_id'] = _fullOptionId
      ..['practice_option_type'] = 'FULL_SIMULATION'
      ..['display_name'] = 'Full simulation';
    final policy = _sessionPolicy(response);
    policy
      ..['min_effective_turns'] = 4
      ..['max_effective_turns'] = 6
      ..['coverage_checkpoint_turn'] = 4;
    final client = _client(_QueueTransport([_response(response)]));

    final bootstrap = await client.createSession(
      planId: _planId,
      input: const CreatePreparationSessionInput(
        agentThreadId: _threadId,
        expectedPlanRevision: 1,
        preparationSnapshotId: _preparationSnapshotId,
        preparationProfileId: _profileId,
        preparationProfileVersion: 1,
        preparationUserId: _userId,
        backgroundSummary: _background,
        selection: _fullSelection,
      ),
      idempotencyKey: 'session-full-key',
    );

    expect(bootstrap.maxEffectiveTurns, 6);
  });

  test('accepts the dedicated fourteen-turn IELTS full mock budget', () async {
    final response = _bootstrapJson();
    final session = response['practice_session']! as Map<String, Object?>;
    session
      ..['scenario_type'] = 'EXAM'
      ..['scenario_model'] = 'IELTS_SPEAKING_FULL_MOCK';
    final snapshot = response['snapshot']! as Map<String, Object?>;
    snapshot
      ..['scenario_type'] = 'EXAM'
      ..['scenario_model'] = 'IELTS_SPEAKING_FULL_MOCK';
    final scenario =
        snapshot['scenario_definition_snapshot']! as Map<String, Object?>;
    scenario
      ..['scenario_definition_id'] = _ieltsScenarioId
      ..['scenario_type'] = 'EXAM'
      ..['scenario_model'] = 'IELTS_SPEAKING_FULL_MOCK'
      ..['name'] = 'IELTS 口语完整模拟'
      ..['version'] = 2;
    final config =
        snapshot['scenario_config_snapshot']! as Map<String, Object?>;
    config
      ..['scenario_config_id'] = _ieltsConfigId
      ..['scenario_definition_id'] = _ieltsScenarioId
      ..['config_type'] = 'EXAM'
      ..['scenario_model'] = 'IELTS_SPEAKING_FULL_MOCK'
      ..['version'] = 2
      ..remove('job_title')
      ..remove('job_description');
    final participants = snapshot['participants']! as List<Object?>;
    final examiner = participants.first as Map<String, Object?>;
    final role = examiner['role_snapshot']! as Map<String, Object?>;
    role['scenario_definition_id'] = _ieltsScenarioId;
    final option = snapshot['practice_option']! as Map<String, Object?>;
    option
      ..remove('role_definition_id')
      ..['practice_option_id'] = _ieltsFullOptionId
      ..['scenario_definition_id'] = _ieltsScenarioId
      ..['practice_option_type'] = 'FULL_SIMULATION'
      ..['display_name'] = '完整模考'
      ..['version'] = 2;
    final policy = _sessionPolicy(response);
    policy
      ..['suggested_duration_seconds'] = 900
      ..['min_effective_turns'] = 14
      ..['max_effective_turns'] = 14
      ..['coverage_checkpoint_turn'] = 14
      ..['max_follow_ups_per_question'] = 0;
    final client = _client(_QueueTransport([_response(response)]));

    final bootstrap = await client.createSession(
      planId: _planId,
      input: const CreatePreparationSessionInput(
        agentThreadId: _threadId,
        expectedPlanRevision: 1,
        preparationSnapshotId: _preparationSnapshotId,
        preparationProfileId: _profileId,
        preparationProfileVersion: 1,
        preparationUserId: _userId,
        backgroundSummary: _background,
        selection: _ieltsFullSelection,
      ),
      idempotencyKey: 'session-ielts-full-key',
    );

    expect(bootstrap.maxEffectiveTurns, 14);
    expect(bootstrap.session.scenarioModel, 'IELTS_SPEAKING_FULL_MOCK');
  });

  test('fences a response that completes after account cleanup', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final operation = client.createProfile(
      input: const CreatePreparationProfileInput(
        backgroundSummary: _background,
      ),
      idempotencyKey: 'profile-key-123',
    );

    await client.clearAccountState();
    transport.complete(_response(_profileJson()));

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
    baseUri: Uri.parse('https://api.speak-up.top'),
    credentialProvider: () => credential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

Matcher get _invalidResponse => isA<PreparationLaunchException>().having(
  (error) => error.kind,
  'kind',
  PreparationLaunchFailureKind.invalidResponse,
);

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
  final calls = <_TransportCall>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
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
  final _responseCompleter = Completer<IdentityHttpResponse>();

  void complete(IdentityHttpResponse response) {
    _responseCompleter.complete(response);
  }

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) {
    return _responseCompleter.future;
  }
}

IdentityHttpResponse _response(Map<String, Object?> body) {
  return IdentityHttpResponse(
    statusCode: HttpStatus.created,
    body: jsonEncode(body),
  );
}

IdentityHttpResponse _rawResponse(int statusCode, String body) {
  return IdentityHttpResponse(statusCode: statusCode, body: body);
}

Map<String, Object?> _profileJson() => {
  'preparation_profile_id': _profileId,
  'user_id': _userId,
  'background_summary': _background,
  'version': 1,
  'updated_at': _time,
};

Map<String, Object?> _snapshotJson() => {
  'preparation_snapshot_id': _preparationSnapshotId,
  'source_profile_id': _profileId,
  'source_version': 1,
  'background_snapshot': _background,
  'created_at': _time,
};

Map<String, Object?> _planJson() => {
  'practice_plan_id': _planId,
  'user_id': _userId,
  'agent_thread_id': _threadId,
  'matter_id': _matterId,
  'scenario_definition_id': _scenarioId,
  'scenario_definition_version': 1,
  'scenario_type': 'INTERVIEW',
  'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
  'scenario_config_id': _configId,
  'scenario_config_version': 1,
  'preparation_profile_id': _profileId,
  'selected_role_ids': [_roleId],
  'plan_revision': 1,
  'practice_plan_status': 'ready',
  'created_at': _time,
  'updated_at': _time,
};

Map<String, Object?> _bootstrapJson() => {
  'practice_session': {
    'practice_session_id': _sessionId,
    'practice_plan_id': _planId,
    'scenario_type': 'INTERVIEW',
    'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
    'snapshot_id': _sessionSnapshotId,
    'practice_session_status': 'starting',
    'session_version': 1,
    'created_at': _time,
  },
  'snapshot': {
    'snapshot_id': _sessionSnapshotId,
    'practice_session_id': _sessionId,
    'plan_revision': 1,
    'scenario_type': 'INTERVIEW',
    'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
    'scenario_definition_snapshot': {
      'scenario_definition_id': _scenarioId,
      'scenario_type': 'INTERVIEW',
      'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
      'name': 'Technical interview',
      'version': 1,
      'status': 'active',
      'turn_policy_ref': 'interview.project_deep_dive.turn.v1',
      'session_policy_ref': 'interview.project_deep_dive.session.v1',
    },
    'scenario_config_snapshot': {
      'scenario_config_id': _configId,
      'scenario_definition_id': _scenarioId,
      'config_type': 'INTERVIEW',
      'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
      'version': 1,
      'job_title': 'Backend engineer',
      'job_description': 'Explain engineering decisions.',
      'prompt_model': {
        'public_scene_brief': 'Discuss one backend project.',
        'practice_goal': 'Explain decisions with evidence.',
        'user_role': 'Candidate',
        'ai_role': 'Technical interviewer',
        'persona_summary': 'Precise and evidence seeking.',
        'focus_areas': ['system_design'],
        'turn_blueprints': ['Ask for a project overview.'],
        'suggested_duration_seconds': 900,
      },
    },
    'preparation_snapshot': _snapshotJson(),
    'participants': [
      {
        'practice_participant_id': 'participant-interviewer',
        'practice_session_id': _sessionId,
        'participant_role': 'FACILITATOR',
        'subject_ref': {
          'namespace': 'mock.actor',
          'subject_id': 'interviewer-technical',
        },
        'role_definition_id': _roleId,
        'role_snapshot': {
          'role_definition_id': _roleId,
          'scenario_definition_id': _scenarioId,
          'role_type': 'TECHNICAL_INTERVIEWER',
          'display_name': 'Technical interviewer',
          'responsibilities': 'Probe technical depth.',
          'style': 'Precise',
          'focus_areas': ['system_design'],
          'version': 2,
        },
        'participant_order': 1,
      },
      {
        'practice_participant_id': 'participant-candidate',
        'practice_session_id': _sessionId,
        'participant_role': 'LEARNER',
        'subject_ref': {'namespace': 'speakup.user', 'subject_id': _userId},
        'participant_order': 2,
      },
    ],
    'practice_option': {
      'practice_option_id': _optionId,
      'scenario_definition_id': _scenarioId,
      'role_definition_id': _roleId,
      'practice_option_type': 'FOCUS',
      'display_name': 'Focused practice',
      'version': 1,
    },
    'session_policy': {
      'suggested_duration_seconds': 300,
      'min_effective_turns': 1,
      'max_effective_turns': 3,
      'coverage_checkpoint_turn': 1,
      'max_follow_ups_per_question': 1,
      'target_objectives': [
        {
          'objective_id': 'system_design',
          'description': 'Explain one design trade-off.',
        },
      ],
      'early_completion_rule': 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
    },
    'practice_focuses': [
      {
        'objective_id': 'system_design',
        'description': 'Explain one design trade-off.',
      },
    ],
    'created_at': _time,
  },
};

Map<String, Object?> _roleSnapshot(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  final participants = snapshot['participants']! as List<Object?>;
  final interviewer = participants.first as Map<String, Object?>;
  return interviewer['role_snapshot']! as Map<String, Object?>;
}

Map<String, Object?> _scenarioDefinitionSnapshot(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  return snapshot['scenario_definition_snapshot']! as Map<String, Object?>;
}

Map<String, Object?> _preparationSnapshot(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  return snapshot['preparation_snapshot']! as Map<String, Object?>;
}

Map<String, Object?> _candidateSubject(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  final participants = snapshot['participants']! as List<Object?>;
  final candidate = participants[1] as Map<String, Object?>;
  return candidate['subject_ref']! as Map<String, Object?>;
}

Map<String, Object?> _sessionPolicy(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  return snapshot['session_policy']! as Map<String, Object?>;
}

const _time = '2026-07-26T12:00:00Z';
const _threadId = '20000000-0000-4000-8000-000000000001';
const _matterId = '10000000-0000-4000-8000-000000000001';
const _userId = 'user-1';
const _profileId = 'profile-1';
const _preparationSnapshotId = 'preparation-snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _sessionSnapshotId = 'session-snapshot-1';
const _scenarioId = 'scenario-1';
const _configId = 'config-1';
const _roleId = 'role-1';
const _optionId = 'option-1';
const _fullOptionId = 'option-full';
const _ieltsScenarioId = 'scn_ielts_speaking_full';
const _ieltsConfigId = 'scfg_ielts_speaking_full';
const _ieltsFullOptionId = 'option_ielts_speaking_full_full';
const _background = 'Backend engineer preparing a technical interview.';

const _context = AgentPracticeContext(threadId: _threadId, matterId: _matterId);

const _selection = PreparationLaunchSelection(
  scenarioDefinitionId: _scenarioId,
  scenarioDefinitionVersion: 1,
  scenarioType: 'INTERVIEW',
  scenarioModel: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  scenarioDisplayName: 'Technical interview',
  scenarioDescription: 'Backend engineer: technical interview practice',
  scenarioConfigId: _configId,
  scenarioConfigVersion: 1,
  roleDefinitionId: _roleId,
  roleDefinitionVersion: 2,
  practiceOptionId: _optionId,
  practiceOptionType: PreparationOptionType.focus,
  practiceOptionVersion: 1,
);

const _fullSelection = PreparationLaunchSelection(
  scenarioDefinitionId: _scenarioId,
  scenarioDefinitionVersion: 1,
  scenarioType: 'INTERVIEW',
  scenarioModel: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  scenarioDisplayName: 'Technical interview',
  scenarioDescription: 'Backend engineer: technical interview practice',
  scenarioConfigId: _configId,
  scenarioConfigVersion: 1,
  roleDefinitionId: _roleId,
  roleDefinitionVersion: 2,
  practiceOptionId: _fullOptionId,
  practiceOptionType: PreparationOptionType.fullSimulation,
  practiceOptionVersion: 1,
);

const _ieltsFullSelection = PreparationLaunchSelection(
  scenarioDefinitionId: _ieltsScenarioId,
  scenarioDefinitionVersion: 2,
  scenarioType: 'EXAM',
  scenarioModel: 'IELTS_SPEAKING_FULL_MOCK',
  scenarioDisplayName: 'IELTS 口语完整模拟',
  scenarioDescription: '按 Part 1、Part 2、Part 3 连续完成。',
  scenarioConfigId: _ieltsConfigId,
  scenarioConfigVersion: 2,
  roleDefinitionId: _roleId,
  roleDefinitionVersion: 2,
  practiceOptionId: _ieltsFullOptionId,
  practiceOptionType: PreparationOptionType.fullSimulation,
  practiceOptionVersion: 2,
);
