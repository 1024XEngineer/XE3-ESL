import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/wire_job_preparation_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'runs the exact JD-first wire chain without creating Session early',
    () async {
      final transport = _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.created, _targetJson(JobTargetStage.draft)),
        _response(
          HttpStatus.ok,
          _targetJson(JobTargetStage.awaitingConfirmation),
        ),
        _response(HttpStatus.ok, _targetJson(JobTargetStage.confirmed)),
        _response(HttpStatus.created, _profileJson()),
        _response(HttpStatus.created, _preparationSnapshotJson()),
        _response(HttpStatus.created, _planJson()),
        _response(HttpStatus.created, _bootstrapJson()),
      ]);
      final client = _client(transport);

      final draft = await client.createJobTarget(
        input: _input,
        idempotencyKey: 'target-create-key',
      );
      final analyzed = await client.analyzeJobTarget(
        jobTargetId: draft.id,
        expectedInputVersion: draft.inputVersion,
        idempotencyKey: 'target-analysis-key',
      );
      final confirmed = await client.confirmJobTarget(
        jobTargetId: analyzed.id,
        expectedInputVersion: analyzed.inputVersion,
        expectedAnalysisVersion: analyzed.analysis!.analysisVersion,
        candidate: analyzed.analysis!.candidate!,
        idempotencyKey: 'target-confirm-key',
      );
      final profile = await client.createProfile(
        input: CreatePreparationProfileInput(
          backgroundSummary: _background,
          jobTargetId: confirmed.id,
          jobTargetConfirmationVersion:
              confirmed.confirmation!.confirmationVersion,
        ),
        idempotencyKey: 'target-profile-key',
      );
      final snapshot = await client.createSnapshot(
        profileId: profile.id,
        sourceVersion: profile.version,
        idempotencyKey: 'target-snapshot-key',
      );
      final plan = await client.createPlan(
        input: _planInput(preparationSnapshotId: snapshot.id),
        idempotencyKey: 'target-plan-key',
      );

      expect(transport.calls, hasLength(6));
      expect(
        transport.calls.where(
          (call) => call.uri.path.endsWith('/practice-sessions'),
        ),
        isEmpty,
      );
      expect(plan.sessionPolicy.maxEffectiveTurns, 5);
      expect(jsonDecode(transport.calls[3].body!), <String, Object?>{
        'background_summary': _background,
        'job_target_id': _targetId,
        'job_target_confirmation_version': 1,
      });
      expect(jsonDecode(transport.calls[5].body!), <String, Object?>{
        'source_thread_id': _threadId,
        'goal_id': _goalId,
        'preparation_snapshot_id': _preparationSnapshotId,
        'scene_id': _sceneId,
        'scene_version': 1,
        'selected_role_ids': <String>[_roleId],
        'practice_option_id': _optionId,
        'max_effective_turns': 5,
      });

      final bootstrap = await client.createSession(
        plan: plan,
        input: const CreatePreparationSessionInput(
          expectedPlanRevision: 1,
          userConfirmed: true,
        ),
        idempotencyKey: 'target-session-key',
      );

      expect(bootstrap.session.id, _sessionId);
      expect(transport.calls, hasLength(7));
      expect(jsonDecode(transport.calls.last.body!), <String, Object?>{
        'practice_plan_id': _planId,
        'expected_plan_revision': 1,
        'user_confirmed': true,
      });
      expect(
        transport.calls.last.body,
        isNot(
          anyOf(
            contains('role_definition_ids'),
            contains('practice_option_id'),
          ),
        ),
      );
      for (final call in transport.calls) {
        expect(
          call.headers[HttpHeaders.authorizationHeader],
          'Bearer sess_account-a',
        );
        if (call.method != 'GET') {
          expect(call.headers['Idempotency-Key'], isNotEmpty);
        }
      }
    },
  );

  test('quick-start sends no invented job description', () async {
    final response = _targetJson(JobTargetStage.draft, input: _quickInput);
    final transport = _QueueTransport(<IdentityHttpResponse>[
      _response(HttpStatus.created, response),
    ]);
    final client = _client(transport);

    await client.createJobTarget(
      input: _quickInput,
      idempotencyKey: 'quick-target-key',
    );

    expect(jsonDecode(transport.calls.single.body!), <String, Object?>{
      'source': 'quick_start',
      'job_title': 'Backend engineer',
      'candidate_background': _background,
    });
  });

  test('accepts 202 only for a parsing projection and polls by GET', () async {
    final transport = _QueueTransport(<IdentityHttpResponse>[
      _response(HttpStatus.accepted, _targetJson(JobTargetStage.parsing)),
      _response(
        HttpStatus.ok,
        _targetJson(JobTargetStage.awaitingConfirmation),
      ),
    ]);
    final client = _client(transport);

    final parsing = await client.analyzeJobTarget(
      jobTargetId: _targetId,
      expectedInputVersion: 1,
      idempotencyKey: 'analysis-poll-key',
    );
    final completed = await client.getJobTarget(parsing.id);

    expect(parsing.stage, JobTargetStage.parsing);
    expect(completed.stage, JobTargetStage.awaitingConfirmation);
    expect(transport.calls.last.method, 'GET');
    expect(transport.calls.last.headers['Idempotency-Key'], isNull);
  });

  test(
    'revises only server catalog identities and server turn budget',
    () async {
      final revised = _planJson()..['plan_revision'] = 2;
      final transport = _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.ok, revised),
      ]);
      final client = _client(transport);

      final plan = await client.revisePlan(
        planId: _planId,
        input: const RevisePreparationPlanInput(
          expectedPlanRevision: 1,
          selectedRoleIds: <String>[_roleId],
          practiceOptionId: _optionId,
          maxEffectiveTurns: 5,
        ),
        idempotencyKey: 'plan-revision-key',
      );

      expect(plan.revision, 2);
      expect(jsonDecode(transport.calls.single.body!), <String, Object?>{
        'expected_plan_revision': 1,
        'selected_role_ids': <String>[_roleId],
        'practice_option_id': _optionId,
        'max_effective_turns': 5,
      });
    },
  );

  group('strict JobTarget decoder', () {
    final cases = <String, void Function(Map<String, Object?>)>{
      'unknown root field': (target) => target['invented'] = true,
      'draft analysis': (target) {
        target['analysis'] = _analysisJson(JobTargetAnalysisStatus.running);
      },
      'parsing without running analysis': (target) {
        target
          ..['stage'] = 'parsing'
          ..remove('analysis');
      },
      'succeeded analysis without candidate': (target) {
        target['stage'] = 'awaiting_confirmation';
        target['analysis'] = _analysisJson(JobTargetAnalysisStatus.succeeded)
          ..remove('candidate');
      },
      'confirmation version mismatch': (target) {
        target['confirmation'] = _confirmationJson()..['analysis_version'] = 2;
      },
      'candidate source mismatch': (target) {
        final confirmation = target['confirmation']! as Map<String, Object?>;
        final candidate = confirmation['candidate']! as Map<String, Object?>;
        candidate
          ..['source'] = 'quick_start'
          ..['general_advice_only'] = true;
      },
    };

    for (final entry in cases.entries) {
      test(entry.key, () async {
        final value = _targetJson(JobTargetStage.confirmed);
        entry.value(value);
        final client = _client(
          _QueueTransport(<IdentityHttpResponse>[
            _response(HttpStatus.ok, value),
          ]),
        );

        await expectLater(
          client.getJobTarget(_targetId),
          throwsA(_invalidResponse),
        );
      });
    }
  });

  group('strict targeted Profile and Snapshot decoder', () {
    final cases = <String, Map<String, Object?> Function()>{
      'profile missing confirmation pair': () =>
          _profileJson()..remove('job_target_confirmation_version'),
      'profile accepts no legacy resume field': () =>
          _profileJson()..['resume_ref'] = 'resume-local',
      'snapshot missing candidate snapshot': () =>
          _preparationSnapshotJson()..remove('job_target_candidate_snapshot'),
      'snapshot candidate source mismatch': () {
        final value = _preparationSnapshotJson();
        final candidate =
            value['job_target_candidate_snapshot']! as Map<String, Object?>;
        candidate
          ..['source'] = 'quick_start'
          ..['general_advice_only'] = true;
        return value;
      },
    };

    for (final entry in cases.entries) {
      test(entry.key, () async {
        final isProfile = entry.key.startsWith('profile');
        final client = _client(
          _QueueTransport(<IdentityHttpResponse>[
            _response(HttpStatus.created, entry.value()),
          ]),
        );

        final operation = isProfile
            ? client.createProfile(
                input: const CreatePreparationProfileInput(
                  backgroundSummary: _background,
                  jobTargetId: _targetId,
                  jobTargetConfirmationVersion: 1,
                ),
                idempotencyKey: 'invalid-profile-key',
              )
            : client.createSnapshot(
                profileId: _profileId,
                sourceVersion: 1,
                idempotencyKey: 'invalid-snapshot-key',
              );
        await expectLater(operation, throwsA(_invalidResponse));
      });
    }
  });

  group('strict targeted Plan decoder', () {
    final cases = <String, void Function(Map<String, Object?>)>{
      'missing targeted optional group': (plan) {
        plan.remove('session_policy');
      },
      'selected role differs from catalog': (plan) {
        final selection = plan['scene_selection']! as Map<String, Object?>;
        selection['selected_role_ids'] = <String>['role-other'];
      },
      'scene differs from catalog': (plan) {
        _planScene(plan)['scene_id'] = 'scene-other';
      },
      'focus option differs from selected role': (plan) {
        final options = _planScene(plan)['practice_options']! as List<Object?>;
        final option = options.single as Map<String, Object?>;
        option['role_definition_id'] = 'role-other';
      },
      'policy checkpoint exceeds max': (plan) {
        final policy = plan['session_policy']! as Map<String, Object?>;
        policy['coverage_checkpoint_turn'] = 6;
      },
      'duplicate objective identity': (plan) {
        final objectives = plan['practice_objectives']! as List<Object?>;
        objectives.add(_clone(objectives.single));
      },
      'missing turn policy reference': (plan) {
        _planScene(plan).remove('turn_policy_ref');
      },
    };

    for (final entry in cases.entries) {
      test(entry.key, () async {
        final value = _planJson();
        entry.value(value);
        final client = _client(
          _QueueTransport(<IdentityHttpResponse>[
            _response(HttpStatus.created, value),
          ]),
        );

        await expectLater(
          client.createPlan(
            input: _planInput(),
            idempotencyKey: 'invalid-plan-key',
          ),
          throwsA(_invalidResponse),
        );
      });
    }
  });

  group('strict targeted Session decoder', () {
    final cases = <String, void Function(Map<String, Object?>)>{
      'plan revision mismatch': (root) {
        _sessionSnapshot(root)['plan_revision'] = 2;
      },
      'candidate account mismatch': (root) {
        _candidateSubject(root)['subject_id'] = 'user-other';
      },
      'role snapshot mismatch': (root) {
        _sessionRole(root)['display_name'] = 'Different interviewer';
      },
      'preparation confirmation mismatch': (root) {
        _sessionPreparation(root)['source_job_target_confirmation_version'] = 2;
      },
      'policy content mismatch': (root) {
        _sessionPolicy(root)['suggested_duration_seconds'] = 999;
      },
      'focus content mismatch': (root) {
        final focuses =
            _sessionSnapshot(root)['practice_objectives']! as List<Object?>;
        (focuses.single as Map<String, Object?>)['description'] = 'Changed';
      },
      'unexpected session field': (root) {
        final session = root['practice_session']! as Map<String, Object?>;
        session['effective_turns'] = 0;
      },
      'invalid session policy reference': (root) {
        _sessionScene(root)['session_policy_ref'] = '';
      },
    };

    for (final entry in cases.entries) {
      test(entry.key, () async {
        final bootstrap = _bootstrapJson();
        entry.value(bootstrap);
        final transport = _QueueTransport(<IdentityHttpResponse>[
          _response(HttpStatus.created, _planJson()),
          _response(HttpStatus.created, bootstrap),
        ]);
        final client = _client(transport);
        final plan = await client.createPlan(
          input: _planInput(),
          idempotencyKey: 'valid-plan-key',
        );

        await expectLater(
          client.createSession(
            plan: plan,
            input: const CreatePreparationSessionInput(
              expectedPlanRevision: 1,
              userConfirmed: true,
            ),
            idempotencyKey: 'invalid-session-key',
          ),
          throwsA(_invalidResponse),
        );
      });
    }
  });

  test('marks malformed 201 create responses retryable with stage', () async {
    final client = _client(
      _QueueTransport(<IdentityHttpResponse>[
        IdentityHttpResponse(
          statusCode: HttpStatus.created,
          body: '{"truncated":',
        ),
      ]),
    );

    await expectLater(
      client.createJobTarget(
        input: _input,
        idempotencyKey: 'ambiguous-create-key',
      ),
      throwsA(
        isA<JobPreparationException>()
            .having(
              (error) => error.kind,
              'kind',
              JobPreparationFailureKind.invalidResponse,
            )
            .having(
              (error) => error.stage,
              'stage',
              JobPreparationOperationStage.target,
            )
            .having((error) => error.statusCode, 'status', 201)
            .having((error) => error.retryable, 'retryable', isTrue),
      ),
    );
  });

  test('fences a response arriving after account cleanup', () async {
    final transport = _CompleterTransport();
    final client = _client(transport);
    final operation = client.createJobTarget(
      input: _input,
      idempotencyKey: 'late-target-key',
    );

    await client.clearAccountState();
    transport.complete(
      _response(HttpStatus.created, _targetJson(JobTargetStage.draft)),
    );

    await expectLater(
      operation,
      throwsA(
        isA<JobPreparationException>().having(
          (error) => error.kind,
          'kind',
          JobPreparationFailureKind.superseded,
        ),
      ),
    );
  });
}

WireJobPreparationClient _client(IdentityHttpTransport transport) {
  const credential = AuthSessionCredential(
    sessionToken: 'sess_account-a',
    generation: 1,
  );
  return WireJobPreparationClient(
    baseUri: Uri.parse('https://api.speak-up.top'),
    credentialProvider: () => credential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

Matcher get _invalidResponse => isA<JobPreparationException>().having(
  (error) => error.kind,
  'kind',
  JobPreparationFailureKind.invalidResponse,
);

final class _Call {
  const _Call({
    required this.method,
    required this.uri,
    required this.headers,
    this.body,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final String? body;
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
    calls.add(
      _Call(
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
  final Completer<IdentityHttpResponse> _completer =
      Completer<IdentityHttpResponse>();

  void complete(IdentityHttpResponse response) {
    _completer.complete(response);
  }

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) {
    return _completer.future;
  }
}

IdentityHttpResponse _response(int status, Map<String, Object?> body) {
  return IdentityHttpResponse(statusCode: status, body: jsonEncode(body));
}

Map<String, Object?> _targetJson(
  JobTargetStage stage, {
  JobTargetInput input = _input,
}) {
  return <String, Object?>{
    'job_target_id': _targetId,
    'user_id': _userId,
    'input': _inputJson(input),
    'input_version': 1,
    'stage': stage.wireValue,
    if (stage == JobTargetStage.parsing)
      'analysis': _analysisJson(JobTargetAnalysisStatus.running),
    if (stage == JobTargetStage.analysisFailed)
      'analysis': _analysisJson(JobTargetAnalysisStatus.failed),
    if (stage == JobTargetStage.awaitingConfirmation ||
        stage == JobTargetStage.confirmed)
      'analysis': _analysisJson(JobTargetAnalysisStatus.succeeded),
    if (stage == JobTargetStage.confirmed) 'confirmation': _confirmationJson(),
    'created_at': _time,
    'updated_at': _time,
  };
}

Map<String, Object?> _inputJson(JobTargetInput input) {
  return <String, Object?>{
    'source': input.source.wireValue,
    'job_title': ?input.jobTitle,
    'job_description': ?input.jobDescription,
    'company': ?input.company,
    'seniority': ?input.seniority,
    'candidate_background': ?input.candidateBackground,
    'resume_ref': ?input.resumeRef,
    'practice_focus': ?input.practiceFocus,
  };
}

Map<String, Object?> _analysisJson(JobTargetAnalysisStatus status) {
  return <String, Object?>{
    'input_version': 1,
    'analysis_version': 1,
    'attempt': 1,
    'status': status.wireValue,
    if (status == JobTargetAnalysisStatus.succeeded)
      'candidate': _candidateJson(),
    if (status == JobTargetAnalysisStatus.failed)
      'stable_error_category': 'provider_unavailable',
    'started_at': _time,
    if (status != JobTargetAnalysisStatus.running) 'finished_at': _time,
  };
}

Map<String, Object?> _confirmationJson() {
  return <String, Object?>{
    'input_version': 1,
    'analysis_version': 1,
    'confirmation_version': 1,
    'candidate': _candidateJson(),
    'confirmed_at': _time,
  };
}

Map<String, Object?> _candidateJson() {
  return <String, Object?>{
    'source': 'job_description',
    'general_advice_only': false,
    'job_title': 'Backend engineer',
    'seniority': 'Senior',
    'responsibilities': <String>['Build reliable APIs'],
    'core_skills': <String>['Go services'],
    'communication_focus': <String>['Explain trade-offs'],
    'practice_goals': <String>['Practice a system design answer'],
    'scope_notice': 'Based on the supplied job description.',
    'catalog_recommendation': <String, Object?>{
      'scene_id': _sceneId,
      'scene_version': 1,
      'selected_role_ids': <String>[_roleId],
      'practice_option_id': _optionId,
    },
  };
}

Map<String, Object?> _profileJson() {
  return <String, Object?>{
    'preparation_profile_id': _profileId,
    'user_id': _userId,
    'background_summary': _background,
    'job_target_id': _targetId,
    'job_target_confirmation_version': 1,
    'version': 1,
    'updated_at': _time,
  };
}

Map<String, Object?> _preparationSnapshotJson() {
  return <String, Object?>{
    'preparation_snapshot_id': _preparationSnapshotId,
    'source_profile_id': _profileId,
    'source_version': 1,
    'source_job_target_id': _targetId,
    'source_job_target_confirmation_version': 1,
    'job_target_input_snapshot': _inputJson(_input),
    'job_target_candidate_snapshot': _candidateJson(),
    'background_snapshot': _background,
    'created_at': _time,
  };
}

Map<String, Object?> _sceneJson() {
  return <String, Object?>{
    'scene_id': _sceneId,
    'scene_family': 'INTERVIEW',
    'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
    'name': 'Technical interview',
    'scene_version': 1,
    'status': 'active',
    'turn_policy_ref': 'interview.project_deep_dive.turn.v1',
    'session_policy_ref': 'interview.project_deep_dive.session.v1',
    'prompt': <String, Object?>{
      'public_scene_brief': 'Discuss one backend project.',
      'practice_goal': 'Explain decisions with evidence.',
      'user_role': 'Candidate',
      'ai_role': 'Technical interviewer',
      'persona_summary': 'Precise and evidence seeking.',
      'focus_areas': <String>['system_design'],
      'turn_blueprints': <String>['Ask for a project overview.'],
      'suggested_duration_seconds': 900,
    },
    'roles': <Object?>[_roleJson()],
    'practice_options': <Object?>[_optionJson()],
  };
}

Map<String, Object?> _roleJson() {
  return <String, Object?>{
    'role_definition_id': _roleId,
    'scene_id': _sceneId,
    'role_type': 'TECHNICAL_INTERVIEWER',
    'display_name': 'Technical interviewer',
    'responsibilities': 'Probe technical depth.',
    'style': 'Precise',
    'practice_objectives': <Object?>[
      <String, Object?>{
        'objective_id': 'system_design',
        'description': 'Explain system design decisions.',
      },
    ],
  };
}

Map<String, Object?> _optionJson() {
  return <String, Object?>{
    'practice_option_id': _optionId,
    'scene_id': _sceneId,
    'role_definition_id': _roleId,
    'practice_option_type': 'FOCUS',
    'display_name': 'System design focus',
  };
}

Map<String, Object?> _policyJson() {
  return <String, Object?>{
    'suggested_duration_seconds': 720,
    'min_effective_turns': 2,
    'max_effective_turns': 5,
    'coverage_checkpoint_turn': 3,
    'max_follow_ups_per_question': 2,
    'early_completion_rule': 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
  };
}

Map<String, Object?> _objectiveJson() {
  return <String, Object?>{
    'objective_id': 'system_design',
    'description': 'Explain one design trade-off.',
  };
}

Map<String, Object?> _planJson() {
  return <String, Object?>{
    'practice_plan_id': _planId,
    'user_id': _userId,
    'source_thread_id': _threadId,
    'goal_snapshot': <String, Object?>{
      'goal_id': _goalId,
      'title': 'Technical interview',
      'version': 1,
    },
    'preparation_snapshot': _preparationSnapshotJson(),
    'scene_selection': <String, Object?>{
      'scene': _sceneJson(),
      'selected_role_ids': <String>[_roleId],
      'practice_option_id': _optionId,
    },
    'session_policy': _policyJson(),
    'practice_objectives': <Object?>[_objectiveJson()],
    'plan_revision': 1,
    'practice_plan_status': 'ready',
    'created_at': _time,
    'updated_at': _time,
  };
}

Map<String, Object?> _bootstrapJson() {
  return <String, Object?>{
    'practice_session': <String, Object?>{
      'practice_session_id': _sessionId,
      'practice_plan_id': _planId,
      'plan_revision': 1,
      'scene_family': 'INTERVIEW',
      'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
      'snapshot_id': _sessionSnapshotId,
      'practice_session_status': 'starting',
      'session_version': 1,
      'created_at': _time,
    },
    'snapshot': <String, Object?>{
      'snapshot_id': _sessionSnapshotId,
      'practice_session_id': _sessionId,
      'plan_revision': 1,
      'scene_family': 'INTERVIEW',
      'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
      'scene_selection': <String, Object?>{
        'scene': _sceneJson(),
        'selected_role_ids': <String>[_roleId],
        'practice_option_id': _optionId,
      },
      'preparation_snapshot': _preparationSnapshotJson(),
      'participants': <Object?>[
        <String, Object?>{
          'practice_participant_id': 'participant-interviewer',
          'practice_session_id': _sessionId,
          'participant_role': 'FACILITATOR',
          'subject_ref': <String, Object?>{
            'namespace': 'mock.actor',
            'subject_id': 'interviewer-technical',
          },
          'role_definition_id': _roleId,
          'role_snapshot': _roleJson(),
          'participant_order': 1,
        },
        <String, Object?>{
          'practice_participant_id': 'participant-candidate',
          'practice_session_id': _sessionId,
          'participant_role': 'LEARNER',
          'subject_ref': <String, Object?>{
            'namespace': 'speakup.user',
            'subject_id': _userId,
          },
          'participant_order': 2,
        },
      ],
      'session_policy': _policyJson(),
      'practice_objectives': <Object?>[_objectiveJson()],
      'created_at': _time,
    },
  };
}

Map<String, Object?> _sessionSnapshot(Map<String, Object?> root) {
  return root['snapshot']! as Map<String, Object?>;
}

Map<String, Object?> _planScene(Map<String, Object?> plan) {
  final selection = plan['scene_selection']! as Map<String, Object?>;
  return selection['scene']! as Map<String, Object?>;
}

Map<String, Object?> _sessionScene(Map<String, Object?> root) {
  final selection =
      _sessionSnapshot(root)['scene_selection']! as Map<String, Object?>;
  return selection['scene']! as Map<String, Object?>;
}

Map<String, Object?> _sessionPreparation(Map<String, Object?> root) {
  return _sessionSnapshot(root)['preparation_snapshot']!
      as Map<String, Object?>;
}

Map<String, Object?> _sessionPolicy(Map<String, Object?> root) {
  return _sessionSnapshot(root)['session_policy']! as Map<String, Object?>;
}

Map<String, Object?> _sessionRole(Map<String, Object?> root) {
  final participants = _sessionSnapshot(root)['participants']! as List<Object?>;
  final interviewer = participants.first as Map<String, Object?>;
  return interviewer['role_snapshot']! as Map<String, Object?>;
}

Map<String, Object?> _candidateSubject(Map<String, Object?> root) {
  final participants = _sessionSnapshot(root)['participants']! as List<Object?>;
  final candidate = participants.last as Map<String, Object?>;
  return candidate['subject_ref']! as Map<String, Object?>;
}

Map<String, Object?> _clone(Object? value) {
  return jsonDecode(jsonEncode(value))! as Map<String, Object?>;
}

const _input = JobTargetInput(
  source: JobTargetSource.jobDescription,
  jobDescription: 'Build reliable APIs and explain engineering trade-offs.',
  company: 'Example Co',
  seniority: 'Senior',
  candidateBackground: _background,
  practiceFocus: 'System design communication',
);

const _quickInput = JobTargetInput(
  source: JobTargetSource.quickStart,
  jobTitle: 'Backend engineer',
  candidateBackground: _background,
);

CreatePreparationPlanInput _planInput({
  String preparationSnapshotId = _preparationSnapshotId,
}) => CreatePreparationPlanInput(
  sourceThreadId: _threadId,
  goalId: _goalId,
  preparationSnapshotId: preparationSnapshotId,
  sceneId: _sceneId,
  sceneVersion: 1,
  selectedRoleIds: const <String>[_roleId],
  practiceOptionId: _optionId,
  maxEffectiveTurns: 5,
);

const _time = '2026-07-26T12:00:00Z';
const _userId = 'user-1';
const _targetId = 'target-1';
const _profileId = 'profile-1';
const _preparationSnapshotId = 'preparation-snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _sessionSnapshotId = 'session-snapshot-1';
const _threadId = 'thread-1';
const _goalId = 'goal-1';
const _sceneId = 'scene-1';
const _roleId = 'role-1';
const _optionId = 'option-1';
const _background = 'Built reliable Go services for three years.';
