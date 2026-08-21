import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action_controller.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/preparation_contract_fixtures.dart';
import '../../support/practice_fixtures.dart';

void main() {
  test('loads the frozen IELTS assignment before starting the Plan', () async {
    final harness = await _Harness.create();
    addTearDown(harness.dispose);
    final assignment = testIeltsAssignment(
      mode: PracticeMode.part1,
      part1QuestionCount: 3,
    );
    final plan = _ieltsPlanForThread(
      harness.conversation.threadId!,
      assignment: assignment,
    );
    final controller = PracticePlanClientActionController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      ieltsPreparationController: harness.ieltsPreparation,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({
            required planId,
            required expectedVersion,
            required idempotencyKey,
          }) async => throw UnimplementedError(),
      createSession:
          ({required plan, required input, required idempotencyKey}) async =>
              throw UnimplementedError(),
    );
    addTearDown(controller.dispose);

    final preview = await controller.loadIELTSPreview(_action(plan));

    expect(preview, same(assignment));
    expect(preview.parts.single.turnBlueprints, hasLength(3));
  });

  test(
    'historical v1 action confirms draft Plan before creating Session',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);
      final draft = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.draft,
        version: 1,
      );
      final ready = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.ready,
        version: 2,
      );
      final calls = <String>[];
      late CreatePreparationSessionInput sessionInput;
      final controller = PracticePlanClientActionController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
        workspaceController: harness.workspace,
        readPlan: (_) async {
          calls.add('read');
          return draft;
        },
        confirmPlan:
            ({
              required planId,
              required expectedVersion,
              required idempotencyKey,
            }) async {
              calls.add('confirm:$expectedVersion');
              return ready;
            },
        createSession:
            ({required plan, required input, required idempotencyKey}) async {
              calls.add('session:${plan.version}');
              sessionInput = input;
              return contractBootstrap(ready);
            },
        idFactory: (scope) => '$scope-contract-key',
      );
      addTearDown(controller.dispose);

      final historicalAction = decodeConfirmPracticePlanClientAction(
        _legacyActionEnvelope(draft),
      );

      expect(historicalAction.protocol, ConfirmPracticePlanProtocol.v1);
      expect(await controller.confirm(historicalAction), isTrue);

      expect(calls, <String>['read', 'confirm:1', 'session:2']);
      expect(sessionInput.expectedPlanVersion, 2);
      expect(harness.practice.practiceSessionId, contractSessionId);
      expect(harness.workspace.currentSessionId, contractSessionId);
    },
  );

  test(
    'retry skips confirmation once Plan is ready and reuses Session identity',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);
      final draft = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.draft,
        version: 1,
      );
      final ready = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.ready,
        version: 2,
      );
      var confirmed = false;
      var sessionAttempts = 0;
      final confirmationKeys = <String>[];
      final sessionKeys = <String>[];
      final controller = PracticePlanClientActionController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
        workspaceController: harness.workspace,
        readPlan: (_) async => confirmed ? ready : draft,
        confirmPlan:
            ({
              required planId,
              required expectedVersion,
              required idempotencyKey,
            }) async {
              confirmationKeys.add(idempotencyKey);
              confirmed = true;
              return ready;
            },
        createSession:
            ({required plan, required input, required idempotencyKey}) async {
              sessionKeys.add(idempotencyKey);
              sessionAttempts++;
              if (sessionAttempts == 1) {
                throw const PreparationLaunchException(
                  kind: PreparationLaunchFailureKind.network,
                  stage: PreparationLaunchStage.session,
                  retryable: true,
                );
              }
              return contractBootstrap(ready);
            },
        idFactory: (scope) => '$scope-contract-key',
      );
      addTearDown(controller.dispose);
      final action = _action(draft);

      expect(await controller.confirm(action), isFalse);
      expect(await controller.confirm(action), isTrue);

      expect(confirmationKeys, hasLength(1));
      expect(sessionKeys, hasLength(2));
      expect(sessionKeys.toSet(), hasLength(1));
    },
  );

  test(
    'ready Plan skips duplicate confirmation before creating Session',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);
      final draft = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.draft,
        version: 1,
      );
      final ready = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.ready,
        version: 2,
      );
      var confirmationCalls = 0;
      var sessionCalls = 0;
      final controller = PracticePlanClientActionController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
        workspaceController: harness.workspace,
        readPlan: (_) async => ready,
        confirmPlan:
            ({
              required planId,
              required expectedVersion,
              required idempotencyKey,
            }) async {
              confirmationCalls++;
              return ready;
            },
        createSession:
            ({required plan, required input, required idempotencyKey}) async {
              sessionCalls++;
              return contractBootstrap(ready);
            },
        idFactory: (scope) => '$scope-contract-key',
      );
      addTearDown(controller.dispose);

      expect(await controller.confirm(_action(draft)), isTrue);

      expect(confirmationCalls, 0);
      expect(sessionCalls, 1);
    },
  );

  test(
    'second click on the same Plan resumes its parked Session without writes',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);
      final draft = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.draft,
        version: 1,
      );
      final ready = _planForThread(
        harness.conversation.threadId!,
        status: PracticePlanStatus.ready,
        version: 2,
      );
      var confirmed = false;
      var confirmationCalls = 0;
      var sessionCalls = 0;
      final controller = PracticePlanClientActionController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
        workspaceController: harness.workspace,
        readPlan: (_) async => confirmed ? ready : draft,
        confirmPlan:
            ({
              required planId,
              required expectedVersion,
              required idempotencyKey,
            }) async {
              confirmationCalls++;
              confirmed = true;
              return ready;
            },
        createSession:
            ({required plan, required input, required idempotencyKey}) async {
              sessionCalls++;
              return contractBootstrap(ready);
            },
        idFactory: (scope) => '$scope-contract-key',
      );
      addTearDown(controller.dispose);
      final action = _action(draft);

      expect(await controller.confirm(action), isTrue);
      expect(await harness.workspace.parkCurrentPractice(), isTrue);
      expect(harness.practice.hasActivePractice, isFalse);

      expect(await controller.confirm(action), isTrue);

      expect(harness.practice.practiceSessionId, contractSessionId);
      expect(harness.practice.practicePlanId, contractPlanId);
      expect(confirmationCalls, 1);
      expect(sessionCalls, 1);
    },
  );

  test('action from another Agent thread never confirms the Plan', () async {
    final harness = await _Harness.create();
    addTearDown(harness.dispose);
    final foreign = _planForThread(
      contractThreadId,
      status: PracticePlanStatus.draft,
      version: 1,
    );
    var writes = 0;
    final controller = PracticePlanClientActionController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      ieltsPreparationController: harness.ieltsPreparation,
      workspaceController: harness.workspace,
      readPlan: (_) async => foreign,
      confirmPlan:
          ({
            required planId,
            required expectedVersion,
            required idempotencyKey,
          }) async {
            writes++;
            return foreign;
          },
      createSession:
          ({required plan, required input, required idempotencyKey}) async {
            writes++;
            return contractBootstrap(plan);
          },
      idFactory: (scope) => '$scope-contract-key',
    );
    addTearDown(controller.dispose);

    expect(await controller.confirm(_action(foreign)), isFalse);
    expect(writes, 0);
    expect(controller.errorMessage, contains('已经变化'));
  });
}

final class _Harness {
  _Harness._({
    required this.conversation,
    required this.practice,
    required this.ieltsPreparation,
    required this.workspace,
  });

  final ConversationController conversation;
  final PracticeController practice;
  final IeltsPreparationController ieltsPreparation;
  final PracticeWorkspaceController workspace;

  static Future<_Harness> create() async {
    final conversation = ConversationController(client: FakeAgentClient());
    await conversation.initialize();
    final practice = PracticeController(
      client: FakePracticeClient(
        planId: contractPlanId,
        practiceExperience: PracticeExperience.interview,
        sceneCategory: SceneCategory.interviewProfessional,
        turnLimit: 6,
      ),
    );
    final ieltsPreparation = IeltsPreparationController(
      client: _UnusedIeltsQuestionBankClient(),
      historyStore: const NullIeltsPracticeHistoryStore(),
    );
    final workspace = PracticeWorkspaceController(
      practiceController: practice,
      recordStore: MemoryPracticeLaunchRecordStore(),
    );
    await workspace.activateAccount(contractUserId);
    return _Harness._(
      conversation: conversation,
      practice: practice,
      ieltsPreparation: ieltsPreparation,
      workspace: workspace,
    );
  }

  void dispose() {
    workspace.dispose();
    ieltsPreparation.dispose();
    practice.dispose();
    conversation.dispose();
  }
}

PracticePlan _planForThread(
  String threadId, {
  required PracticePlanStatus status,
  required int version,
}) {
  final base = contractPlan(status: status, version: version);
  return PracticePlan(
    id: base.id,
    userId: base.userId,
    sourceThreadId: threadId,
    preparationSnapshot: base.preparationSnapshot,
    sceneSelection: base.sceneSelection,
    sessionPolicy: base.sessionPolicy,
    practiceObjectives: base.practiceObjectives,
    version: base.version,
    status: base.status,
    createdAt: base.createdAt,
    updatedAt: base.updatedAt,
  );
}

PracticePlan _ieltsPlanForThread(
  String threadId, {
  required IeltsPracticeAssignment assignment,
}) {
  const scene = SceneDefinition(
    id: 'ielts-speaking',
    experience: PracticeExperience.ieltsSpeaking,
    category: SceneCategory.ieltsSpeaking,
    name: 'IELTS 口语',
    version: 1,
    status: SceneStatus.active,
    prompt: ScenePrompt(
      publicSceneBrief: 'Complete an IELTS Speaking Part 1 practice.',
      practiceGoal: 'Answer the assigned Part 1 questions clearly.',
      userRole: 'Candidate',
      aiRole: 'Examiner',
      personaSummary: 'A neutral IELTS examiner.',
      focusAreas: <String>['fluency'],
      turnBlueprints: <String>['Ask the assigned questions in order.'],
    ),
    roles: <RoleDefinition>[
      RoleDefinition(
        id: 'ielts-examiner',
        sceneId: 'ielts-speaking',
        type: 'EXAMINER',
        displayName: 'IELTS 口语考官',
        responsibilities: 'Ask the assigned IELTS questions.',
        style: 'Neutral.',
        practiceObjectives: <RolePracticeObjective>[
          RolePracticeObjective(
            objectiveId: 'fluency',
            description: 'Answer fluently.',
          ),
        ],
      ),
    ],
    practiceOptions: <PracticeOption>[
      PracticeOption(
        id: 'ielts-part-1',
        sceneId: 'ielts-speaking',
        mode: PracticeMode.part1,
        displayName: 'Part 1',
        suggestedDurationSeconds: 300,
        turnPolicyRef: 'ielts-part1.turn.v1',
        sessionPolicyRef: 'ielts-part1.session.v1',
        evaluationPolicyRef: 'ielts-part1.evaluation.v1',
      ),
    ],
  );
  final base = contractPlan(status: PracticePlanStatus.draft, version: 1);
  return PracticePlan(
    id: base.id,
    userId: base.userId,
    sourceThreadId: threadId,
    preparationSnapshot: base.preparationSnapshot,
    sceneSelection: const SceneSelectionSnapshot(
      source: SceneSource.catalog(sceneId: 'ielts-speaking', sceneVersion: 1),
      scene: scene,
      selectedRoleIds: <String>['ielts-examiner'],
      practiceOptionId: 'ielts-part-1',
    ),
    sessionPolicy: base.sessionPolicy,
    practiceObjectives: base.practiceObjectives,
    ieltsAssignment: assignment,
    version: 1,
    status: PracticePlanStatus.draft,
    createdAt: base.createdAt,
    updatedAt: base.updatedAt,
  );
}

ConfirmPracticePlanClientAction _action(PracticePlan plan) =>
    ConfirmPracticePlanClientAction(
      label: 'Confirm and start',
      practicePlanId: plan.id,
      planVersion: plan.version,
      sceneId: plan.sceneSelection.scene.id,
      sceneName: plan.sceneSelection.scene.name,
      userRole: plan.sceneSelection.scene.prompt.userRole,
      practiceGoal: plan.sceneSelection.scene.prompt.practiceGoal,
      practiceExperience: plan.sceneSelection.scene.experience.wireValue,
      sceneCategory: plan.sceneSelection.scene.category.wireValue,
      practiceMode: plan.practiceOption.mode.wireValue,
      aiRoles: plan.selectedRoles.map((role) => role.displayName).toList(),
      practiceScope: plan.practiceOption.displayName,
      suggestedDuration: Duration(
        seconds: plan.sessionPolicy.suggestedDurationSeconds,
      ),
      minEffectiveTurns: plan.sessionPolicy.minEffectiveTurns,
      maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
      confirmationPrompt: 'Confirm to create a Practice Session.',
    );

AgentClientAction _legacyActionEnvelope(PracticePlan plan) => AgentClientAction(
  type: practicePlanConfirmClientActionV1Type,
  payload: <String, Object?>{
    'label': 'Confirm and start',
    'practice_plan_id': plan.id,
    'plan_version': plan.version,
    'target': plan.sceneSelection.scene.prompt.practiceGoal,
    'scene_name': plan.sceneSelection.scene.name,
    'practice_experience': plan.sceneSelection.scene.experience.wireValue,
    'scene_category': plan.sceneSelection.scene.category.wireValue,
    'practice_mode': plan.practiceOption.mode.wireValue,
    'roles': plan.selectedRoles.map((role) => role.displayName).toList(),
    'practice_scope': plan.practiceOption.displayName,
    'suggested_duration_seconds': plan.sessionPolicy.suggestedDurationSeconds,
    'min_effective_turns': plan.sessionPolicy.minEffectiveTurns,
    'max_effective_turns': plan.sessionPolicy.maxEffectiveTurns,
    'confirmation_prompt': 'Confirm to create a Practice Session.',
  },
);

final class _UnusedIeltsQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getQuestionBank() => throw UnimplementedError();
}
