import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/wire_scene_client.dart';
import 'package:speakup/features/coaching/ielts/wire_ielts_question_bank_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'reads the anonymous scene directory without a bearer credential',
    () async {
      final transport = _QueueTransport([
        _response(<String, Object?>{
          'scenes': [_sceneJson],
        }),
      ]);
      final client = WireSceneClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        transport: transport,
      );

      final scenes = await client.listScenes();

      expect(scenes, hasLength(1));
      expect(scenes.single.id, _sceneId);
      expect(scenes.single.name, 'English interview for technical roles');
      expect(
        scenes.single.practiceOptions.first.evaluationPolicyRef,
        'interview.shadow.evaluation.v1',
      );
      expect(
        scenes.single.prompt.publicSceneBrief,
        'Discuss one backend project.',
      );
      expect(transport.calls.single.path, '/v1/scenes');
      expect(transport.calls.single.authorization, isNull);
      transport.expectDone();
    },
  );

  test('accepts a server-provided public brief for each scene', () async {
    final prompt = <String, Object?>{
      ..._sceneJson['prompt']! as Map<String, Object?>,
      'public_scene_brief': 'Discuss one real backend project.',
    };
    final client = WireSceneClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: _QueueTransport([
        _response(<String, Object?>{
          'scenes': [
            <String, Object?>{..._sceneJson, 'prompt': prompt},
          ],
        }),
      ]),
    );

    final scenes = await client.listScenes();

    expect(scenes, hasLength(1));
    expect(
      scenes.single.prompt.publicSceneBrief,
      'Discuss one real backend project.',
    );
  });

  test('rejects a scene without its evaluation policy reference', () async {
    final option = <String, Object?>{..._fullOptionJson}
      ..remove('evaluation_policy_ref');
    final scene = <String, Object?>{
      ..._sceneJson,
      'practice_options': <Object?>[option],
    };
    final client = WireSceneClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: _QueueTransport([
        _response(<String, Object?>{
          'scenes': [scene],
        }),
      ]),
    );

    await expectLater(
      client.listScenes(),
      throwsA(
        isA<SceneClientException>().having(
          (error) => error.kind,
          'kind',
          SceneClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('decodes detail and preserves server role and option order', () async {
    final transport = _QueueTransport([
      _response(_detailJson),
      _response(<String, Object?>{
        'roles': [_technicalRoleJson, _recruiterRoleJson],
      }),
    ]);
    final client = WireSceneClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: transport,
    );

    final detail = await client.getScene(_sceneId);
    final roles = await client.listRoles(_sceneId);

    expect(detail.prompt.publicSceneBrief, 'Discuss one backend project.');
    expect(detail.practiceOptions.map((option) => option.mode), const [
      PracticeMode.fullSimulation,
      PracticeMode.focus,
      PracticeMode.focus,
    ]);
    expect(roles.map((role) => role.id), const [
      'role_technical_interviewer',
      'role_hr_interviewer',
    ]);
    expect(transport.calls.map((call) => call.path), const [
      '/v1/scenes/$_sceneId',
      '/v1/scenes/$_sceneId/roles',
    ]);
    expect(transport.calls.every((call) => call.authorization == null), isTrue);
    transport.expectDone();
  });

  test('accepts a complete IELTS cue card as one turn blueprint', () async {
    const cueCard =
        'Part 2 cue card: Describe a skill you would like to learn.\n'
        'You should say:\n'
        '• What the skill is\n'
        '• Why you want to learn it\n'
        '• How you would learn it\n'
        '• And explain how learning this skill would benefit you';
    final prompt = <String, Object?>{
      ..._sceneJson['prompt']! as Map<String, Object?>,
      'turn_blueprints': [cueCard],
    };
    final client = WireSceneClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: _QueueTransport([
        _response(<String, Object?>{..._detailJson, 'prompt': prompt}),
      ]),
    );

    final detail = await client.getScene(_sceneId);

    expect(detail.prompt.turnBlueprints, [cueCard]);
    expect(utf8.encode(cueCard).length, greaterThan(128));
  });

  test('accepts supported experience and category pairs', () async {
    final scenes = <Map<String, Object?>>[
      _sceneJsonFor(
        id: 'scn_interview_project',
        experience: 'INTERVIEW',
        category: 'INTERVIEW_PROFESSIONAL',
        name: 'Project interview',
      ),
      _sceneJsonFor(
        id: 'scn_ielts_speaking_test',
        experience: 'IELTS_SPEAKING',
        category: 'IELTS_SPEAKING',
        name: 'IELTS Speaking Part 2',
      ),
      _sceneJsonFor(
        id: 'scn_workplace_progress_risk_update',
        experience: 'WORKPLACE',
        category: 'WORKPLACE_GENERAL',
        name: 'Progress and risk update',
      ),
      _sceneJsonFor(
        id: 'scn_travel_hotel_checkin',
        experience: 'LIFE_AND_TRAVEL',
        category: 'LIFE_TRAVEL',
        name: 'Hotel check-in and issue handling',
      ),
    ];
    final client = WireSceneClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: _QueueTransport([
        _response(<String, Object?>{'scenes': scenes}),
      ]),
    );

    final result = await client.listScenes();

    expect(result.map((scene) => scene.experience), [
      PracticeExperience.interview,
      PracticeExperience.ieltsSpeaking,
      PracticeExperience.workplace,
      PracticeExperience.lifeAndTravel,
    ]);
  });

  test(
    'rejects unknown fields instead of inventing a client contract',
    () async {
      final transport = _QueueTransport([
        _response(<String, Object?>{
          'scenes': [
            <String, Object?>{..._sceneJson, 'display_order': 10},
          ],
        }),
      ]);
      final client = WireSceneClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        transport: transport,
      );

      await expectLater(
        client.listScenes(),
        throwsA(
          isA<SceneClientException>().having(
            (error) => error.kind,
            'kind',
            SceneClientFailureKind.invalidResponse,
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
    final client = WireSceneClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: transport,
    );

    await expectLater(
      client.getScene(_sceneId),
      throwsA(
        isA<SceneClientException>().having(
          (error) => error.kind,
          'kind',
          SceneClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('decodes the published IELTS set catalog without credentials', () async {
    final transport = _QueueTransport([_response(_ieltsQuestionBankJson())]);
    final client = WireIeltsQuestionBankClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      transport: transport,
    );

    final bank = await client.getQuestionBank();

    expect(bank.seasonLabel, '5–8 月题库');
    expect(bank.part1Topics, hasLength(2));
    expect(bank.part1Topics.first.titleZh, '主题 1');
    expect(bank.filters.topicTags.single.code, 'daily_life');
    expect(bank.topicGroups, hasLength(2));
    expect(bank.topicGroups.first.part3Questions, hasLength(5));
    expect(bank.topicGroups.first.cueCard.points, hasLength(4));
    expect(transport.calls.single.path, '/v1/ielts-speaking/question-bank');
    expect(transport.calls.single.authorization, isNull);
  });

  test(
    'preserves a published IELTS topic with one original Part 3 question',
    () async {
      final response = _ieltsQuestionBankJson();
      final groups = response['topic_groups']! as List<Object?>;
      final shortGroup = groups.first as Map<String, Object?>;
      shortGroup['part3_questions'] = <Object?>[
        'How important is it for schools to help children become smarter?',
      ];
      final transport = _QueueTransport([_response(response)]);
      final client = WireIeltsQuestionBankClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        transport: transport,
      );

      final bank = await client.getQuestionBank();

      expect(bank.topicGroups.first.part3Questions, hasLength(1));
      expect(bank.topicGroups.first.part3Questions.single, contains('schools'));
    },
  );
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
    List<int>? bodyBytes,
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

IdentityHttpResponse _response(Object body) =>
    IdentityHttpResponse(statusCode: HttpStatus.ok, body: jsonEncode(body));

Map<String, Object?> _ieltsQuestionBankJson() => <String, Object?>{
  'schema_version': 4,
  'bank_id': 'ielts-speaking-2026-season',
  'season': '2026-05-08',
  'season_label': '5–8 月题库',
  'season_start': '2026-05-01',
  'season_end': '2026-08-31',
  'source_cutoff': '2026-06-18T10:00:00Z',
  'filters': <String, Object?>{
    'releases': [
      <String, Object?>{'code': 'new', 'label': '本季新增'},
    ],
    'parts': [
      <String, Object?>{'code': 'PART_1', 'label': 'Part 1'},
      <String, Object?>{'code': 'PART_2', 'label': 'Part 2'},
      <String, Object?>{'code': 'PART_3', 'label': 'Part 3'},
    ],
    'topic_tags': [
      <String, Object?>{'code': 'daily_life', 'label': '日常生活'},
    ],
    'cue_card_types': [
      <String, Object?>{'code': 'thing', 'label': '事物'},
    ],
  },
  'part1_topics': List<Object?>.generate(
    2,
    (index) => <String, Object?>{
      'id': 'p1-topic-${index + 1}',
      'title_zh': '主题 ${index + 1}',
      'title_en': 'Topic ${index + 1}',
      'release_status': index.isEven ? 'new' : 'carry_over',
      'cue_card_type': index.isEven ? 'thing' : 'experience',
      'tag_codes': ['daily_life'],
      'questions': ['Topic ${index + 1}-1', 'Topic ${index + 1}-2'],
    },
  ),
  'topic_groups': List<Object?>.generate(
    2,
    (index) => <String, Object?>{
      'id': 'p23-${index + 1}',
      'title_zh': '主题 ${index + 1}',
      'release_status': index.isEven ? 'new' : 'carry_over',
      'cue_card_type': index.isEven ? 'person' : 'place',
      'tag_codes': ['daily_life'],
      'part2': <String, Object?>{
        'prompt': 'Describe topic ${index + 1}',
        'points': ['What', 'Where', 'Who', 'Why'],
      },
      'part3_questions': List<Object?>.generate(
        5,
        (question) => 'Question ${index + 1}-${question + 1}',
      ),
    },
  ),
};

const _sceneId = 'scn_programmer_interview';

const _fullOptionJson = <String, Object?>{
  'practice_option_id': 'option_full_simulation',
  'scene_id': _sceneId,
  'practice_mode': 'FULL_SIMULATION',
  'display_name': 'Full simulation',
  'suggested_duration_seconds': 900,
  'turn_policy_ref': 'interview.project_deep_dive.turn.v1',
  'session_policy_ref': 'interview.project_deep_dive.session.v1',
  'evaluation_policy_ref': 'interview.shadow.evaluation.v1',
};

const _technicalOptionJson = <String, Object?>{
  'practice_option_id': 'option_technical_focus',
  'scene_id': _sceneId,
  'role_definition_id': 'role_technical_interviewer',
  'practice_mode': 'FOCUS',
  'display_name': 'Technical depth focus',
  'suggested_duration_seconds': 600,
  'turn_policy_ref': 'interview.technical_focus.turn.v1',
  'session_policy_ref': 'interview.technical_focus.session.v1',
  'evaluation_policy_ref': 'interview.shadow.evaluation.v1',
};

const _recruiterOptionJson = <String, Object?>{
  'practice_option_id': 'option_hr_focus',
  'scene_id': _sceneId,
  'role_definition_id': 'role_hr_interviewer',
  'practice_mode': 'FOCUS',
  'display_name': 'Recruiter and motivation focus',
  'suggested_duration_seconds': 600,
  'turn_policy_ref': 'interview.recruiter_focus.turn.v1',
  'session_policy_ref': 'interview.recruiter_focus.session.v1',
  'evaluation_policy_ref': 'interview.shadow.evaluation.v1',
};

const _technicalRoleJson = <String, Object?>{
  'role_definition_id': 'role_technical_interviewer',
  'scene_id': _sceneId,
  'role_type': 'TECHNICAL_INTERVIEWER',
  'display_name': 'Technical depth perspective',
  'responsibilities': 'Probe technical depth and decision making.',
  'style': 'Precise and evidence seeking.',
  'practice_objectives': <Object?>[
    <String, Object?>{
      'objective_id': 'system_design',
      'description': 'Explain system design decisions.',
    },
    <String, Object?>{
      'objective_id': 'project_depth',
      'description': 'Provide concrete project depth.',
    },
  ],
};

const _recruiterRoleJson = <String, Object?>{
  'role_definition_id': 'role_hr_interviewer',
  'scene_id': _sceneId,
  'role_type': 'HR_INTERVIEWER',
  'display_name': 'Recruiter and motivation perspective',
  'responsibilities': 'Explore motivation and communication clarity.',
  'style': 'Warm and structured.',
  'practice_objectives': <Object?>[
    <String, Object?>{
      'objective_id': 'motivation',
      'description': 'Explain role motivation.',
    },
    <String, Object?>{
      'objective_id': 'communication',
      'description': 'Communicate clearly.',
    },
  ],
};

const _sceneJson = <String, Object?>{
  'scene_id': _sceneId,
  'practice_experience': 'INTERVIEW',
  'scene_category': 'INTERVIEW_PROFESSIONAL',
  'name': 'English interview for technical roles',
  'scene_version': 1,
  'status': 'active',
  'prompt': {
    'public_scene_brief': 'Discuss one backend project.',
    'practice_goal': 'Explain decisions with evidence.',
    'user_role': 'Candidate',
    'ai_role': 'Technical interviewer',
    'persona_summary': 'Precise and evidence seeking.',
    'focus_areas': ['introduction', 'system_design'],
    'turn_blueprints': ['Ask for a project overview.'],
  },
  'roles': [_technicalRoleJson, _recruiterRoleJson],
  'practice_options': [
    _fullOptionJson,
    _technicalOptionJson,
    _recruiterOptionJson,
  ],
};

const _detailJson = _sceneJson;

Map<String, Object?> _sceneJsonFor({
  required String id,
  required String experience,
  required String category,
  required String name,
}) {
  final roleId = 'role-$id';
  return <String, Object?>{
    'scene_id': id,
    'practice_experience': experience,
    'scene_category': category,
    'name': name,
    'scene_version': 1,
    'status': 'active',
    'prompt': <String, Object?>{
      'public_scene_brief': 'Practice $name.',
      'practice_goal': 'Complete this practice.',
      'user_role': 'Learner',
      'ai_role': 'Coach',
      'persona_summary': 'Structured and focused.',
      'focus_areas': <String>['clarity'],
      'turn_blueprints': <String>['Ask one relevant question.'],
    },
    'roles': <Object?>[
      <String, Object?>{
        'role_definition_id': roleId,
        'scene_id': id,
        'role_type': 'COACH',
        'display_name': 'Coach',
        'responsibilities': 'Guide the practice.',
        'style': 'Structured.',
        'practice_objectives': <Object?>[
          <String, Object?>{
            'objective_id': 'clarity',
            'description': 'Communicate clearly.',
          },
        ],
      },
    ],
    'practice_options': <Object?>[
      <String, Object?>{
        'practice_option_id': 'option-$id',
        'scene_id': id,
        'practice_mode': experience == 'IELTS_SPEAKING'
            ? 'FULL_MOCK'
            : 'FULL_SIMULATION',
        'display_name': 'Full practice',
        'suggested_duration_seconds': 600,
        'turn_policy_ref': 'turn-$id',
        'session_policy_ref': 'session-$id',
        'evaluation_policy_ref': 'evaluation-$id',
      },
    ],
  };
}
