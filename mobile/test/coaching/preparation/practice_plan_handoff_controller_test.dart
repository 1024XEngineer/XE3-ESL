import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_handoff_controller.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/scene_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('message confirmation opens the exact created practice', (
    tester,
  ) async {
    final seedPlan = _plan(sourceThreadId: 'seed-thread');
    final seededHandoff = _handoff(seedPlan);
    final harness = await _createHarness(
      client: _SeededHandoffAgentClient(seededHandoff),
    );
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async =>
              _bootstrap(plan),
      idFactory: _fixedId,
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SpeakUpShell(
          conversationController: harness.conversation,
          composerController: harness.composer,
          practiceController: harness.practice,
          practicePlanHandoffController: controller,
        ),
        onGenerateRoute: (settings) {
          if (settings.name != AppRoutes.practice) {
            return null;
          }
          return MaterialPageRoute<void>(
            settings: settings,
            builder: (_) =>
                const Scaffold(key: Key('confirmed-practice-route')),
          );
        },
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
      findsOneWidget,
    );
    await tester.tap(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('confirmed-practice-route')), findsOneWidget);
    expect(harness.practice.practiceSessionId, _sessionId);
    expect(harness.workspace.currentSessionId, _sessionId);
  });

  test(
    'confirms the exact persisted plan through the canonical session path',
    () async {
      final harness = await _createHarness();
      addTearDown(harness.dispose);
      final sourceThreadId = harness.conversation.threadId!;
      final persistedPlan = _plan(sourceThreadId: sourceThreadId);
      final handoff = _handoff(persistedPlan);
      String? confirmedPlanId;
      CreatePreparationSessionInput? confirmedInput;
      String? confirmedKey;
      final controller = PracticePlanHandoffController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        workspaceController: harness.workspace,
        readPlan: (planId) async {
          expect(planId, persistedPlan.id);
          return persistedPlan;
        },
        confirmPlan:
            ({required plan, required input, required idempotencyKey}) async {
              confirmedPlanId = plan.id;
              confirmedInput = input;
              confirmedKey = idempotencyKey;
              return _bootstrap(persistedPlan);
            },
        idFactory: _fixedId,
      );
      addTearDown(controller.dispose);

      expect(await controller.confirm(handoff), isTrue);

      expect(confirmedPlanId, persistedPlan.id);
      expect(confirmedInput?.expectedPlanRevision, persistedPlan.revision);
      expect(confirmedInput?.userConfirmed, isTrue);
      expect(confirmedKey, 'handoff-session-id');
      expect(harness.workspace.currentSessionId, _sessionId);
      expect(harness.workspace.currentGoalId, isNull);
      expect(
        harness.conversation.threadId,
        harness.workspace.currentPracticeThreadId,
      );
      expect(harness.practice.practiceSessionId, _sessionId);
      expect(harness.practice.hasActivePractice, isTrue);
      expect(controller.errorMessage, isNull);
    },
  );

  test('retries an ambiguous confirmation with the same identities', () async {
    final harness = await _createHarness();
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    final handoff = _handoff(plan);
    final keys = <String>[];
    var calls = 0;
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async {
            calls++;
            keys.add(idempotencyKey);
            if (calls == 1) {
              throw StateError('ambiguous response');
            }
            return _bootstrap(plan);
          },
      idFactory: _fixedId,
    );
    addTearDown(controller.dispose);

    expect(await controller.confirm(handoff), isFalse);
    final retainedThreadId = harness.workspace.currentPracticeThreadId;
    expect(retainedThreadId, isNotNull);
    expect(controller.errorMessage, isNotNull);

    expect(await controller.confirm(handoff), isTrue);
    expect(keys, <String>['handoff-session-id', 'handoff-session-id']);
    expect(harness.workspace.currentPracticeThreadId, retainedThreadId);
    expect(harness.practice.practiceSessionId, _sessionId);
  });

  test('rejects a handoff that no longer matches the persisted plan', () async {
    final harness = await _createHarness();
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    final handoff = _handoff(plan, revision: plan.revision + 1);
    var confirmationCalls = 0;
    final initialThreadCount = harness.conversation.threads.length;
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async {
            confirmationCalls++;
            return _bootstrap(plan);
          },
      idFactory: _fixedId,
    );
    addTearDown(controller.dispose);

    expect(await controller.confirm(handoff), isFalse);

    expect(confirmationCalls, 0);
    expect(harness.conversation.threads, hasLength(initialThreadCount));
    expect(harness.workspace.currentLease, isNull);
    expect(controller.errorMessage, contains('已经变化'));
  });

  test('reports a revision changed during confirmation as stale', () async {
    final harness = await _createHarness();
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async {
            throw const PreparationLaunchException(
              kind: PreparationLaunchFailureKind.conflict,
              stage: PreparationLaunchStage.session,
              statusCode: 409,
              errorCode: 'version_conflict',
            );
          },
      idFactory: _fixedId,
    );
    addTearDown(controller.dispose);

    expect(await controller.confirm(_handoff(plan)), isFalse);

    expect(controller.errorMessage, contains('已经变化'));
    expect(harness.practice.practiceSessionId, isNull);
  });

  test(
    'reports an active Practice conflict without starting another',
    () async {
      final harness = await _createHarness();
      addTearDown(harness.dispose);
      final plan = _plan(sourceThreadId: harness.conversation.threadId!);
      final controller = PracticePlanHandoffController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        workspaceController: harness.workspace,
        readPlan: (_) async => plan,
        confirmPlan:
            ({required plan, required input, required idempotencyKey}) async {
              throw const PreparationLaunchException(
                kind: PreparationLaunchFailureKind.conflict,
                stage: PreparationLaunchStage.session,
                statusCode: 409,
                errorCode: 'active_session_conflict',
              );
            },
        idFactory: _fixedId,
      );
      addTearDown(controller.dispose);

      expect(await controller.confirm(_handoff(plan)), isFalse);

      expect(controller.errorMessage, contains('进行中的练习'));
      expect(harness.practice.practiceSessionId, isNull);
    },
  );
}

Future<_Harness> _createHarness({AgentClient? client}) async {
  final conversation = ConversationController(
    client: client ?? FakeAgentClient(),
  );
  final composer = ComposerController(conversationController: conversation);
  final practice = PracticeController(
    client: FakePracticeClient(
      practiceExperience: PracticeExperience.interview,
      sceneCategory: SceneCategory.interviewProfessional,
      turnLimit: 3,
    ),
  );
  await conversation.initialize();
  final workspace = PracticeWorkspaceController(
    conversationController: conversation,
    practiceController: practice,
    recordStore: MemoryPracticeLaunchRecordStore(),
  );
  await workspace.activateAccount('account-1');
  return _Harness(
    conversation: conversation,
    composer: composer,
    practice: practice,
    workspace: workspace,
  );
}

PracticePlan _plan({required String sourceThreadId}) {
  final scene = testScene(
    id: 'project-deep-dive',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewProfessional,
    name: '项目经历深挖',
  );
  return PracticePlan(
    id: _planId,
    userId: 'account-1',
    sourceThreadId: sourceThreadId,
    goalSnapshot: const PreparationGoalSnapshot(
      id: 'goal-1',
      title: 'Java 后端面试',
      version: 1,
    ),
    preparationSnapshot: PreparationSnapshot(
      id: 'snapshot-1',
      sourceProfileId: 'profile-1',
      sourceVersion: 1,
      backgroundSnapshot: 'Backend engineer.',
      createdAt: DateTime.utc(2026, 8, 4),
    ),
    sceneSelection: SceneSelectionSnapshot(
      scene: scene,
      selectedRoleIds: <String>[scene.roles.single.id],
      practiceOptionId: scene.practiceOptions.single.id,
    ),
    sessionPolicy: const PreparationSessionPolicy(
      suggestedDurationSeconds: 600,
      minEffectiveTurns: 3,
      maxEffectiveTurns: 3,
      coverageCheckpointTurn: 2,
      maxFollowUpsPerQuestion: 1,
      earlyCompletionRule: 'all_objectives_covered',
      retryAllowed: false,
      questionTranslationAllowed: true,
      questionTipsAllowed: true,
      avatarAllowed: true,
      speechFeedbackAllowed: true,
    ),
    practiceObjectives: const <PracticeObjective>[
      PracticeObjective(id: 'clarity', description: 'Communicate clearly.'),
    ],
    revision: 2,
    status: PracticePlanStatus.ready,
    createdAt: DateTime.utc(2026, 8, 4),
    updatedAt: DateTime.utc(2026, 8, 4),
  );
}

ConfirmPracticePlanHandoff _handoff(PracticePlan plan, {int? revision}) {
  return ConfirmPracticePlanHandoff(
    label: '确认并开始练习',
    practicePlanId: plan.id,
    planRevision: revision ?? plan.revision,
    target: plan.goalSnapshot!.title,
    sceneName: plan.sceneSelection.scene.name,
    practiceExperience: plan.sceneSelection.scene.experience.wireValue,
    sceneCategory: plan.sceneSelection.scene.category.wireValue,
    practiceMode: plan.practiceOption.mode.wireValue,
    roles: plan.selectedRoles.map((role) => role.displayName).toList(),
    practiceScope: plan.practiceOption.displayName,
    suggestedDuration: Duration(
      seconds: plan.sessionPolicy.suggestedDurationSeconds,
    ),
    minEffectiveTurns: plan.sessionPolicy.minEffectiveTurns,
    maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
    executableStatus: 'ready',
    confirmationPrompt: '请确认是否按此方案开始练习。',
  );
}

PreparationPracticeBootstrap _bootstrap(PracticePlan plan) {
  return PreparationPracticeBootstrap(
    session: PreparationPracticeSession(
      id: _sessionId,
      planId: plan.id,
      practiceExperience: plan.sceneSelection.scene.experience,
      sceneCategory: plan.sceneSelection.scene.category,
      practiceMode: plan.practiceOption.mode,
      snapshotId: 'session-snapshot-1',
      status: 'starting',
      version: 1,
      createdAt: DateTime.utc(2026, 8, 4),
    ),
    preparationSnapshotId: plan.preparationSnapshot.id,
    maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
  );
}

String _fixedId(String scope) => '$scope-id';

final class _Harness {
  const _Harness({
    required this.conversation,
    required this.composer,
    required this.practice,
    required this.workspace,
  });

  final ConversationController conversation;
  final ComposerController composer;
  final PracticeController practice;
  final PracticeWorkspaceController workspace;

  void dispose() {
    workspace.dispose();
    practice.dispose();
    composer.dispose();
    conversation.dispose();
  }
}

final class _SeededHandoffAgentClient implements AgentClient {
  _SeededHandoffAgentClient(this.handoff);

  final ConfirmPracticePlanHandoff handoff;
  final FakeAgentClient _delegate = FakeAgentClient();

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) =>
      _delegate.listThreads(pageSize: pageSize, cursor: cursor);

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() async {
    final snapshot = await _delegate.getFocusedThread();
    if (snapshot == null) {
      return null;
    }
    return AgentThreadSnapshot(
      threadId: snapshot.threadId,
      title: snapshot.title,
      activeGoalId: snapshot.activeGoalId,
      messages: <AgentMessage>[
        ...snapshot.messages,
        AgentMessage(
          id: 'assistant-handoff-message',
          role: AgentMessageRole.assistant,
          text: '练习方案已准备好。',
          handoffs: <AgentHandoff>[handoff],
        ),
      ],
      createdAt: snapshot.createdAt,
      updatedAt: snapshot.updatedAt,
      nextMessageCursor: snapshot.nextMessageCursor,
    );
  }

  @override
  Future<AgentThreadSummary> createThread() => _delegate.createThread();

  @override
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) =>
      _delegate.setFocusedThread(threadId: threadId);

  @override
  Future<void> clearFocusedThread() => _delegate.clearFocusedThread();

  @override
  Future<void> deleteThread({required String threadId}) =>
      _delegate.deleteThread(threadId: threadId);

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) => _delegate.listMessages(
    threadId: threadId,
    pageSize: pageSize,
    cursor: cursor,
  );

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) => _delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
    imageAssetIds: imageAssetIds,
  );
}

const _sessionId = 'session-1';
const _planId = 'practice-plan-$_sessionId';
