import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_wire_codec.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/preparation/wire_preparation_launch_client.dart';
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
          sourceThreadId: _threadId,
          goalId: _goalId,
          preparationSnapshotId: snapshot.id,
          sceneId: _selection.scene.id,
          sceneVersion: _selection.scene.version,
          selectedRoleIds: _selection.selectedRoleIds,
          practiceOptionId: _selection.practiceOptionId,
        ),
        idempotencyKey: 'plan-key-123456',
      );
      final bootstrap = await client.createSession(
        plan: plan,
        input: CreatePreparationSessionInput(
          expectedPlanRevision: plan.revision,
          userConfirmed: true,
        ),
        idempotencyKey: 'session-key-123',
      );

      expect(bootstrap.session.id, _sessionId);
      expect(transport.calls.map((call) => call.uri.path), [
        '/v1/preparation-profiles',
        '/v1/preparation-profiles/$_profileId/snapshots',
        '/v1/practice-plans',
        '/v1/practice-plans/$_planId/practice-sessions',
      ]);
      expect(jsonDecode(transport.calls.first.body!), {
        'background_summary': _background,
      });
      expect(jsonDecode(transport.calls[2].body!), {
        'source_thread_id': _threadId,
        'goal_id': _goalId,
        'preparation_snapshot_id': _preparationSnapshotId,
        'scene_id': _sceneId,
        'scene_version': 1,
        'selected_role_ids': [_roleId],
        'practice_option_id': _optionId,
      });
      expect(jsonDecode(transport.calls.last.body!), {
        'expected_plan_revision': 1,
        'user_confirmed': true,
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

  test('accepts the complete canonical selection in a created Plan', () async {
    final response = _planJson();
    final client = _client(_QueueTransport([_response(response)]));

    final plan = await client.createPlan(
      input: _planInput(),
      idempotencyKey: 'configured-plan-key',
    );

    expect(plan.id, _planId);
  });

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
        expectedPlanRevision: 1,
        userConfirmed: true,
      );
      final plan = decodePracticePlan(_planJson());
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
                input: _planInput(),
                idempotencyKey: 'plan-malformed-key',
              ),
            ),
            (
              stage: PreparationLaunchStage.session,
              run: () => client.createSession(
                plan: plan,
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
        plan: plan,
        input: sessionInput,
        idempotencyKey: 'session-malformed-key',
      );

      expect(bootstrap.session.id, _sessionId);
      final sessionCalls = transport.calls.where(
        (call) => call.uri.path.endsWith('/practice-sessions'),
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

  test('rejects a Plan bound to a different Agent Goal', () async {
    final response = _planJson();
    (response['goal_snapshot']! as Map<String, Object?>)['goal_id'] =
        'goal-other';
    final transport = _QueueTransport([_response(response)]);
    final client = _client(transport);

    await expectLater(
      client.createPlan(input: _planInput(), idempotencyKey: 'plan-key-123456'),
      throwsA(_invalidResponse),
    );
  });

  test('rejects an unknown Plan response field', () async {
    final response = _planJson()..['unexpected_field'] = true;
    final client = _client(_QueueTransport([_response(response)]));

    await expectLater(
      client.createPlan(input: _planInput(), idempotencyKey: 'plan-key-123456'),
      throwsA(_invalidResponse),
    );
  });

  test('rejects an IELTS assignment on a non-IELTS Plan', () {
    final response = _planJson()..['ielts_assignment'] = _ieltsAssignmentJson();

    expect(
      () => decodePracticePlan(response),
      throwsA(isA<PreparationWireFormatException>()),
    );
  });

  test('decodes the canonical IELTS Part 3 assignment shape', () {
    final assignment = decodeIeltsPracticeAssignment(<String, Object?>{
      'bank_id': 'ielts-2026-05-08',
      'season': '2026-05-08',
      'mode': 'PART_3',
      'topic_group_id': 'p23-new-001',
      'topic_title': '语言学习',
      'part_1_questions': 0,
      'part_2_questions': 0,
      'part_3_questions': 2,
      'turn_blueprints': <String>['Question 1', 'Question 2'],
    });

    expect(assignment.mode, IeltsPracticeMode.part3);
    expect(assignment.part2CueCard, isNull);
    expect(assignment.turnBlueprints, hasLength(2));
  });

  group('rejects cross-resource Session bootstrap data', () {
    final cases = <String, void Function(Map<String, Object?>)>{
      'unknown role field': (root) {
        _roleSnapshot(root)['version'] = 99;
      },
      'role scene': (root) {
        _roleSnapshot(root)['scene_id'] = 'scene-other';
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
        _sceneSnapshot(root).remove('turn_policy_ref');
      },
      'invalid session policy reference': (root) {
        _sceneSnapshot(root)['session_policy_ref'] = '';
      },
    };

    for (final entry in cases.entries) {
      test(entry.key, () async {
        final response = _bootstrapJson();
        entry.value(response);
        final client = _client(_QueueTransport([_response(response)]));

        await expectLater(
          client.createSession(
            plan: decodePracticePlan(_planJson()),
            input: const CreatePreparationSessionInput(
              expectedPlanRevision: 1,
              userConfirmed: true,
            ),
            idempotencyKey: 'session-key-123',
          ),
          throwsA(_invalidResponse),
        );
      });
    }
  });

  test('accepts the frozen six-turn full simulation budget', () async {
    final response = _bootstrapJson(selection: _fullSelection);
    final policy = _sessionPolicy(response);
    policy
      ..['min_effective_turns'] = 4
      ..['max_effective_turns'] = 6
      ..['coverage_checkpoint_turn'] = 4;
    final planJson = _planJson(selection: _fullSelection);
    final planPolicy = planJson['session_policy']! as Map<String, Object?>;
    planPolicy
      ..['min_effective_turns'] = 4
      ..['max_effective_turns'] = 6
      ..['coverage_checkpoint_turn'] = 4;
    final client = _client(_QueueTransport([_response(response)]));

    final bootstrap = await client.createSession(
      plan: decodePracticePlan(planJson),
      input: const CreatePreparationSessionInput(
        expectedPlanRevision: 1,
        userConfirmed: true,
      ),
      idempotencyKey: 'session-full-key',
    );

    expect(bootstrap.maxEffectiveTurns, 6);
  });

  test('accepts the dedicated fourteen-turn IELTS full mock budget', () async {
    final response = _bootstrapJson(selection: _ieltsFullSelection);
    final policy = _sessionPolicy(response);
    policy
      ..['suggested_duration_seconds'] = 900
      ..['min_effective_turns'] = 14
      ..['max_effective_turns'] = 14
      ..['coverage_checkpoint_turn'] = 14
      ..['max_follow_ups_per_question'] = 0;
    final planJson = _planJson(selection: _ieltsFullSelection);
    final planPolicy = planJson['session_policy']! as Map<String, Object?>;
    planPolicy
      ..['suggested_duration_seconds'] = 900
      ..['min_effective_turns'] = 14
      ..['max_effective_turns'] = 14
      ..['coverage_checkpoint_turn'] = 14
      ..['max_follow_ups_per_question'] = 0;
    final transport = _QueueTransport([
      _response(planJson),
      _response(response),
    ]);
    final client = _client(transport);

    final plan = await client.createPlan(
      input: _planInput(selection: _ieltsFullSelection),
      idempotencyKey: 'plan-ielts-full-key',
    );
    final bootstrap = await client.createSession(
      plan: plan,
      input: const CreatePreparationSessionInput(
        expectedPlanRevision: 1,
        userConfirmed: true,
      ),
      idempotencyKey: 'session-ielts-full-key',
    );

    expect(bootstrap.maxEffectiveTurns, 14);
    expect(bootstrap.session.sceneModel, SceneModel.ieltsSpeakingFullMock);
    expect(plan.ieltsAssignment?.topicGroupId, 'p23-new-001');
    expect(jsonDecode(transport.calls.first.body!), {
      'source_thread_id': _threadId,
      'goal_id': _goalId,
      'preparation_snapshot_id': _preparationSnapshotId,
      'scene_id': _ieltsSceneId,
      'scene_version': 2,
      'selected_role_ids': [_roleId],
      'practice_option_id': _ieltsFullOptionId,
      'ielts_selection': {
        'mode': 'FULL_MOCK',
        'part_1_set_id': 'p1-002',
        'topic_group_id': 'p23-new-001',
      },
    });
    expect(jsonDecode(transport.calls.last.body!), {
      'expected_plan_revision': 1,
      'user_confirmed': true,
    });
  });

  test(
    'rejects Session IELTS data that differs from its frozen Plan',
    () async {
      final planJson = _planJson(selection: _ieltsFullSelection);
      final planPolicy = planJson['session_policy']! as Map<String, Object?>;
      _configureIeltsPolicy(planPolicy);
      final response = _bootstrapJson(selection: _ieltsFullSelection);
      _configureIeltsPolicy(_sessionPolicy(response));
      final snapshot = response['snapshot']! as Map<String, Object?>;
      final assignment = snapshot['ielts_assignment']! as Map<String, Object?>;
      assignment['topic_title'] = 'A different frozen topic';
      final client = _client(_QueueTransport([_response(response)]));

      await expectLater(
        client.createSession(
          plan: decodePracticePlan(planJson),
          input: const CreatePreparationSessionInput(
            expectedPlanRevision: 1,
            userConfirmed: true,
          ),
          idempotencyKey: 'session-ielts-mismatch-key',
        ),
        throwsA(_invalidResponse),
      );
    },
  );

  test('rejects an IELTS Plan without a frozen assignment', () async {
    final response = _planJson(selection: _ieltsFullSelection);
    _configureIeltsPolicy(response['session_policy']! as Map<String, Object?>);
    response.remove('ielts_assignment');
    final client = _client(_QueueTransport([_response(response)]));

    await expectLater(
      client.createPlan(
        input: _planInput(selection: _ieltsFullSelection),
        idempotencyKey: 'plan-ielts-missing-assignment',
      ),
      throwsA(_invalidResponse),
    );
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

Map<String, Object?> _planJson({
  PreparationLaunchSelection selection = _selection,
}) {
  return {
    'practice_plan_id': _planId,
    'user_id': _userId,
    'source_thread_id': _threadId,
    'goal_snapshot': {
      'goal_id': _goalId,
      'title': selection.scene.name,
      'version': 1,
    },
    'preparation_snapshot': _snapshotJson(),
    'scene_selection': _sceneSelectionJson(selection),
    'session_policy': _sessionPolicyJson(),
    'practice_objectives': _practiceObjectivesJson(),
    if (selection.ieltsSelection != null)
      'ielts_assignment': _ieltsAssignmentJson(),
    'plan_revision': 1,
    'practice_plan_status': 'ready',
    'created_at': _time,
    'updated_at': _time,
  };
}

Map<String, Object?> _bootstrapJson({
  PreparationLaunchSelection selection = _selection,
}) {
  final role = selection.scene.roles.singleWhere(
    (role) => role.id == selection.selectedRoleIds.single,
  );
  return {
    'practice_session': {
      'practice_session_id': _sessionId,
      'practice_plan_id': _planId,
      'plan_revision': 1,
      'scene_family': selection.scene.family.wireValue,
      'scene_model': selection.scene.model.wireValue,
      'snapshot_id': _sessionSnapshotId,
      'practice_session_status': 'starting',
      'session_version': 1,
      'created_at': _time,
    },
    'snapshot': {
      'snapshot_id': _sessionSnapshotId,
      'practice_session_id': _sessionId,
      'plan_revision': 1,
      'scene_family': selection.scene.family.wireValue,
      'scene_model': selection.scene.model.wireValue,
      'scene_selection': _sceneSelectionJson(selection),
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
          'role_definition_id': role.id,
          'role_snapshot': _roleJson(role),
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
      'session_policy': _sessionPolicyJson(),
      'practice_objectives': _practiceObjectivesJson(),
      if (selection.ieltsSelection != null)
        'ielts_assignment': _ieltsAssignmentJson(),
      'created_at': _time,
    },
  };
}

Map<String, Object?> _sceneSelectionJson(
  PreparationLaunchSelection selection,
) => <String, Object?>{
  'scene': _sceneJson(selection.scene),
  'selected_role_ids': selection.selectedRoleIds,
  'practice_option_id': selection.practiceOptionId,
};

Map<String, Object?> _sceneJson(SceneDefinition scene) => <String, Object?>{
  'scene_id': scene.id,
  'scene_family': scene.family.wireValue,
  'scene_model': scene.model.wireValue,
  'name': scene.name,
  'scene_version': scene.version,
  'status': scene.status.name,
  'turn_policy_ref': scene.turnPolicyRef,
  'session_policy_ref': scene.sessionPolicyRef,
  'prompt': <String, Object?>{
    'public_scene_brief': scene.prompt.publicSceneBrief,
    'practice_goal': scene.prompt.practiceGoal,
    'user_role': scene.prompt.userRole,
    'ai_role': scene.prompt.aiRole,
    'persona_summary': scene.prompt.personaSummary,
    'focus_areas': scene.prompt.focusAreas,
    'turn_blueprints': scene.model == SceneModel.ieltsSpeakingFullMock
        ? _ieltsTurnBlueprints()
        : scene.prompt.turnBlueprints,
    'suggested_duration_seconds': scene.prompt.suggestedDurationSeconds,
  },
  'roles': scene.roles.map(_roleJson).toList(growable: false),
  'practice_options': scene.practiceOptions
      .map(_practiceOptionJson)
      .toList(growable: false),
};

Map<String, Object?> _roleJson(RoleDefinition role) => <String, Object?>{
  'role_definition_id': role.id,
  'scene_id': role.sceneId,
  'role_type': role.type,
  'display_name': role.displayName,
  'responsibilities': role.responsibilities,
  'style': role.style,
  'practice_objectives': role.practiceObjectives
      .map(
        (objective) => <String, Object?>{
          'objective_id': objective.objectiveId,
          'description': objective.description,
        },
      )
      .toList(growable: false),
  'voice_config_ref': ?role.voiceConfigRef,
};

Map<String, Object?> _practiceOptionJson(PracticeOption option) =>
    <String, Object?>{
      'practice_option_id': option.id,
      'scene_id': option.sceneId,
      'practice_option_type': option.type.wireValue,
      'display_name': option.displayName,
      'role_definition_id': ?option.roleId,
    };

Map<String, Object?> _sessionPolicyJson() => <String, Object?>{
  'suggested_duration_seconds': 300,
  'min_effective_turns': 1,
  'max_effective_turns': 3,
  'coverage_checkpoint_turn': 1,
  'max_follow_ups_per_question': 1,
  'early_completion_rule': 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
};

CreatePreparationPlanInput _planInput({
  PreparationLaunchSelection selection = _selection,
}) => CreatePreparationPlanInput(
  sourceThreadId: _threadId,
  goalId: _goalId,
  preparationSnapshotId: _preparationSnapshotId,
  sceneId: selection.scene.id,
  sceneVersion: selection.scene.version,
  selectedRoleIds: selection.selectedRoleIds,
  practiceOptionId: selection.practiceOptionId,
  ieltsSelection: selection.ieltsSelection,
);

Map<String, Object?> _ieltsAssignmentJson() => <String, Object?>{
  'bank_id': 'ielts-2026-05-08',
  'season': '2026-05-08',
  'mode': 'FULL_MOCK',
  'part_1_set_id': 'p1-002',
  'topic_group_id': 'p23-new-001',
  'topic_title': '语言学习',
  'part_2_cue_card': 'Describe a language you would like to learn',
  'part_1_questions': 8,
  'part_2_questions': 1,
  'part_3_questions': 5,
  'turn_blueprints': _ieltsTurnBlueprints(),
};

List<String> _ieltsTurnBlueprints() => List<String>.generate(
  14,
  (index) => 'Question ${index + 1}',
  growable: false,
);

void _configureIeltsPolicy(Map<String, Object?> policy) {
  policy
    ..['suggested_duration_seconds'] = 900
    ..['min_effective_turns'] = 14
    ..['max_effective_turns'] = 14
    ..['coverage_checkpoint_turn'] = 14
    ..['max_follow_ups_per_question'] = 0;
}

List<Object?> _practiceObjectivesJson() => <Object?>[
  <String, Object?>{
    'objective_id': 'system_design',
    'description': 'Explain one design trade-off.',
  },
];

Map<String, Object?> _roleSnapshot(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  final participants = snapshot['participants']! as List<Object?>;
  final interviewer = participants.first as Map<String, Object?>;
  return interviewer['role_snapshot']! as Map<String, Object?>;
}

Map<String, Object?> _sceneSnapshot(Map<String, Object?> root) {
  final snapshot = root['snapshot']! as Map<String, Object?>;
  final selection = snapshot['scene_selection']! as Map<String, Object?>;
  return selection['scene']! as Map<String, Object?>;
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
const _goalId = '10000000-0000-4000-8000-000000000001';
const _userId = 'user-1';
const _profileId = 'profile-1';
const _preparationSnapshotId = 'preparation-snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _sessionSnapshotId = 'session-snapshot-1';
const _sceneId = 'scene-1';
const _roleId = 'role-1';
const _optionId = 'option-1';
const _fullOptionId = 'option-full';
const _ieltsSceneId = 'scn_ielts_speaking_full';
const _ieltsFullOptionId = 'option_ielts_speaking_full_full';
const _background = 'Backend engineer preparing a technical interview.';

const _technicalRole = RoleDefinition(
  id: _roleId,
  sceneId: _sceneId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical interviewer',
  responsibilities: 'Probe technical depth.',
  style: 'Precise',
  practiceObjectives: <RolePracticeObjective>[
    RolePracticeObjective(
      objectiveId: 'system_design',
      description: 'Explain system design decisions.',
    ),
  ],
);

const _focusOption = PracticeOption(
  id: _optionId,
  sceneId: _sceneId,
  type: PracticeOptionType.focus,
  displayName: 'Focused practice',
  roleId: _roleId,
);

const _fullOption = PracticeOption(
  id: _fullOptionId,
  sceneId: _sceneId,
  type: PracticeOptionType.fullSimulation,
  displayName: 'Full simulation',
);

const _scene = SceneDefinition(
  id: _sceneId,
  family: SceneFamily.interview,
  model: SceneModel.projectExperienceDeepDive,
  name: 'Technical interview',
  version: 1,
  status: SceneStatus.active,
  turnPolicyRef: 'interview.project_deep_dive.turn.v1',
  sessionPolicyRef: 'interview.project_deep_dive.session.v1',
  prompt: ScenePrompt(
    publicSceneBrief: 'Discuss one backend project.',
    practiceGoal: 'Explain decisions with evidence.',
    userRole: 'Candidate',
    aiRole: 'Technical interviewer',
    personaSummary: 'Precise and evidence seeking.',
    focusAreas: <String>['system_design'],
    turnBlueprints: <String>['Ask for a project overview.'],
    suggestedDurationSeconds: 900,
  ),
  roles: <RoleDefinition>[_technicalRole],
  practiceOptions: <PracticeOption>[_focusOption, _fullOption],
);

const _selection = PreparationLaunchSelection(
  scene: _scene,
  selectedRoleIds: <String>[_roleId],
  practiceOptionId: _optionId,
);

const _fullSelection = PreparationLaunchSelection(
  scene: _scene,
  selectedRoleIds: <String>[_roleId],
  practiceOptionId: _fullOptionId,
);

const _ieltsRole = RoleDefinition(
  id: _roleId,
  sceneId: _ieltsSceneId,
  type: 'IELTS_EXAMINER',
  displayName: 'IELTS examiner',
  responsibilities: 'Run the complete speaking mock.',
  style: 'Neutral and concise.',
  practiceObjectives: <RolePracticeObjective>[
    RolePracticeObjective(
      objectiveId: 'part_1',
      description: 'Answer Part 1 questions.',
    ),
    RolePracticeObjective(
      objectiveId: 'part_2',
      description: 'Deliver the Part 2 response.',
    ),
    RolePracticeObjective(
      objectiveId: 'part_3',
      description: 'Develop Part 3 answers.',
    ),
  ],
);

const _ieltsFullOption = PracticeOption(
  id: _ieltsFullOptionId,
  sceneId: _ieltsSceneId,
  type: PracticeOptionType.fullSimulation,
  displayName: '完整模考',
);

const _ieltsScene = SceneDefinition(
  id: _ieltsSceneId,
  family: SceneFamily.exam,
  model: SceneModel.ieltsSpeakingFullMock,
  name: 'IELTS 口语完整模拟',
  version: 2,
  status: SceneStatus.active,
  turnPolicyRef: 'ielts.speaking.full.turn.v2',
  sessionPolicyRef: 'ielts.speaking.full.session.v2',
  prompt: ScenePrompt(
    publicSceneBrief: '按 Part 1、Part 2、Part 3 连续完成。',
    practiceGoal: 'Complete a full speaking mock.',
    userRole: 'Candidate',
    aiRole: 'IELTS examiner',
    personaSummary: 'Neutral and concise.',
    focusAreas: <String>['part_1', 'part_2', 'part_3'],
    turnBlueprints: <String>['Run the complete speaking mock.'],
    suggestedDurationSeconds: 900,
  ),
  roles: <RoleDefinition>[_ieltsRole],
  practiceOptions: <PracticeOption>[_ieltsFullOption],
);

const _ieltsFullSelection = PreparationLaunchSelection(
  scene: _ieltsScene,
  selectedRoleIds: <String>[_roleId],
  practiceOptionId: _ieltsFullOptionId,
  ieltsSelection: IeltsPracticeSelection(
    mode: IeltsPracticeMode.fullMock,
    part1SetId: 'p1-002',
    topicGroupId: 'p23-new-001',
  ),
);
