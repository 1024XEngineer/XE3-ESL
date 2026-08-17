import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/wire_preparation_launch_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';
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

  test('accepts a Part 2 Plan with a frozen prepared answer', () async {
    final transport = _QueueTransport(<IdentityHttpResponse>[
      _response(HttpStatus.created, _ieltsPlanJson()),
    ]);
    final client = _client(transport);

    final plan = await client.createPlan(
      input: _ieltsPlanInput,
      idempotencyKey: 'ielts-part-2-plan-key',
    );

    final preparedAnswer = plan.ieltsAssignment!
        .part(IeltsSpeakingPart.part2)!
        .preparedAnswers
        .single;
    expect(preparedAnswer.bankId, 'ielts-bank');
    expect(preparedAnswer.part, 'PART_2');
    expect(preparedAnswer.sourceId, 'famous-person');
    expect(preparedAnswer.questionPosition, 1);
    expect(
      preparedAnswer.answer,
      'I would like to meet a songwriter I admire.',
    );
    expect(preparedAnswer.personalized, isTrue);
  });

  test('rejects unknown fields in a frozen prepared answer', () async {
    final response = _ieltsPlanJson();
    final assignment = response['ielts_assignment']! as Map<String, Object?>;
    final parts = assignment['parts']! as List<Object?>;
    final part2 = parts.first! as Map<String, Object?>;
    final answers = part2['prepared_answers']! as List<Object?>;
    final answer = answers.single! as Map<String, Object?>;
    answer['unexpected'] = true;
    final client = _client(
      _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.created, response),
      ]),
    );

    await expectLater(
      client.createPlan(
        input: _ieltsPlanInput,
        idempotencyKey: 'strict-ielts-plan-key',
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

const _ieltsScene = SceneDefinition(
  id: 'ielts-speaking',
  experience: PracticeExperience.ieltsSpeaking,
  category: SceneCategory.ieltsSpeaking,
  name: 'IELTS Speaking',
  version: 1,
  status: SceneStatus.active,
  prompt: ScenePrompt(
    publicSceneBrief: 'Practice IELTS Speaking Part 2 and Part 3.',
    practiceGoal: 'Answer one cue card and six follow-up questions.',
    userRole: 'Candidate',
    aiRole: 'Examiner',
    personaSummary: 'A neutral IELTS examiner.',
    focusAreas: <String>['fluency'],
    turnBlueprints: <String>['Ask the frozen IELTS questions.'],
  ),
  roles: <RoleDefinition>[
    RoleDefinition(
      id: 'ielts-examiner',
      sceneId: 'ielts-speaking',
      type: 'EXAMINER',
      displayName: 'IELTS examiner',
      responsibilities: 'Ask the frozen questions in order.',
      style: 'Neutral',
      practiceObjectives: <RolePracticeObjective>[
        RolePracticeObjective(
          objectiveId: 'fluency',
          description: 'Speak fluently and coherently.',
        ),
      ],
    ),
  ],
  practiceOptions: <PracticeOption>[
    PracticeOption(
      id: 'ielts-part-2',
      sceneId: 'ielts-speaking',
      mode: PracticeMode.part2,
      displayName: 'Part 2',
      suggestedDurationSeconds: 600,
      turnPolicyRef: 'ielts-part-2-turn-policy',
      sessionPolicyRef: 'ielts-part-2-session-policy',
      evaluationPolicyRef: 'ielts-part-2-evaluation-policy',
    ),
  ],
);

const _ieltsPlanInput = CreatePracticePlanInput(
  backgroundSummary: contractBackground,
  sceneId: 'ielts-speaking',
  sceneVersion: 1,
  selectedRoleIds: <String>['ielts-examiner'],
  practiceOptionId: 'ielts-part-2',
  maxEffectiveTurns: 7,
  ieltsSelection: IeltsPracticeSelection(topicGroupId: 'famous-person'),
  ieltsPreparedAnswers: <IeltsPreparedAnswer>[
    IeltsPreparedAnswer(
      bankId: 'ielts-bank',
      part: 'PART_2',
      sourceId: 'famous-person',
      questionPosition: 1,
      answer: 'I would like to meet a songwriter I admire.',
      personalized: true,
    ),
  ],
);

Map<String, Object?> _ieltsPlanJson() {
  final response = contractPlanJson();
  response['scene_selection'] = encodeSceneSelectionSnapshot(
    const SceneSelectionSnapshot(
      source: SceneSource.catalog(sceneId: 'ielts-speaking', sceneVersion: 1),
      scene: _ieltsScene,
      selectedRoleIds: <String>['ielts-examiner'],
      practiceOptionId: 'ielts-part-2',
    ),
  );
  response['session_policy'] = <String, Object?>{
    ...contractSessionPolicyJson(),
    'max_effective_turns': 7,
  };
  response['ielts_assignment'] = _ieltsPart2AssignmentJson();
  return response;
}

Map<String, Object?> _ieltsPart2AssignmentJson() => <String, Object?>{
  'bank_id': 'ielts-bank',
  'season': '2026-08',
  'mode': 'PART_2',
  'parts': <Object?>[
    <String, Object?>{
      'part': 'PART_2',
      'source_id': 'famous-person',
      'topic_title': 'Famous people',
      'cue_card': 'Describe a famous person you would like to meet.',
      'turn_blueprints': <String>[
        'Describe a famous person you would like to meet.',
      ],
      'prepared_answers': <Object?>[
        <String, Object?>{
          'question_position': 1,
          'answer': 'I would like to meet a songwriter I admire.',
          'personalized': true,
        },
      ],
    },
    <String, Object?>{
      'part': 'PART_3',
      'source_id': 'famous-person',
      'topic_title': 'Famous people',
      'turn_blueprints': <String>[
        'Why do people become famous?',
        'Is talent necessary for fame?',
        'How does fame affect children?',
        'What responsibilities do famous people have?',
        'Is it easier to become famous today?',
        'Would you like to be famous?',
      ],
    },
  ],
};
