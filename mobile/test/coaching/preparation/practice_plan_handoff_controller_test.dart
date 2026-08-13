import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_handoff_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/practice_fixtures.dart';
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
      ieltsPreparationController: harness.ieltsPreparation,
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

  testWidgets('existing Practice can be continued without confirming Handoff', (
    tester,
  ) async {
    final seedPlan = _plan(sourceThreadId: 'seed-thread');
    final harness = await _createHarness(
      client: _SeededHandoffAgentClient(_handoff(seedPlan)),
    );
    addTearDown(harness.dispose);
    final sourceThreadId = harness.conversation.threadId!;
    final plan = _plan(sourceThreadId: sourceThreadId);
    await _seedExistingPractice(harness, plan.sceneSelection.scene);
    final confirmer = _ConfirmPlanSpy(plan);
    final controller = _handoffController(harness, plan, confirmer);
    addTearDown(controller.dispose);

    await _pumpConflictShell(tester, harness, controller);
    await tester.tap(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    await tester.pumpAndSettle();

    expect(find.text('开始新的练习？'), findsOneWidget);
    await tester.tap(find.byKey(const Key('continue-existing-practice')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('confirmed-practice-route')), findsOneWidget);
    expect(confirmer.calls, 0);
    expect(harness.practice.practiceSessionId, _existingSessionId);
    expect(harness.workspace.currentSessionId, _existingSessionId);
  });

  testWidgets('existing Practice can be replaced from Handoff confirmation', (
    tester,
  ) async {
    final seedPlan = _plan(sourceThreadId: 'seed-thread');
    final harness = await _createHarness(
      client: _SeededHandoffAgentClient(_handoff(seedPlan)),
    );
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    await _seedExistingPractice(harness, plan.sceneSelection.scene);
    final confirmer = _ConfirmPlanSpy(plan);
    final controller = _handoffController(harness, plan, confirmer);
    addTearDown(controller.dispose);

    await _pumpConflictShell(tester, harness, controller);
    await tester.tap(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('replace-existing-practice')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('confirmed-practice-route')), findsOneWidget);
    expect(confirmer.calls, 1);
    expect(harness.practice.practiceSessionId, _sessionId);
    expect(harness.workspace.currentSessionId, _sessionId);
    expect(
      harness.practice.client.restorePractice(sessionId: _existingSessionId),
      throwsStateError,
    );
  });

  testWidgets('double Handoff tap opens one choice and cancel does not write', (
    tester,
  ) async {
    final seedPlan = _plan(sourceThreadId: 'seed-thread');
    final harness = await _createHarness(
      client: _SeededHandoffAgentClient(_handoff(seedPlan)),
    );
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    await _seedExistingPractice(harness, plan.sceneSelection.scene);
    final confirmer = _ConfirmPlanSpy(plan);
    final controller = _handoffController(harness, plan, confirmer);
    addTearDown(controller.dispose);
    final threadCount = harness.conversation.threads.length;

    await _pumpConflictShell(tester, harness, controller);
    final confirmButton = tester.widget<FilledButton>(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    confirmButton.onPressed!();
    confirmButton.onPressed!();
    await tester.pumpAndSettle();
    expect(find.text('开始新的练习？'), findsOneWidget);
    await tester.tap(find.byKey(const Key('cancel-existing-practice-action')));
    await tester.pumpAndSettle();

    expect(find.text('开始新的练习？'), findsNothing);
    expect(find.byKey(const Key('confirmed-practice-route')), findsNothing);
    expect(confirmer.calls, 0);
    expect(harness.practice.practiceSessionId, _existingSessionId);
    expect(harness.workspace.currentSessionId, _existingSessionId);
    expect(harness.conversation.threads, hasLength(threadCount));
  });

  testWidgets('zero-turn existing Practice is replaced without prompting', (
    tester,
  ) async {
    final seedPlan = _plan(sourceThreadId: 'seed-thread');
    final harness = await _createHarness(
      client: _SeededHandoffAgentClient(_handoff(seedPlan)),
    );
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    await _seedExistingPractice(
      harness,
      plan.sceneSelection.scene,
      withProgress: false,
    );
    final confirmer = _ConfirmPlanSpy(plan);
    final controller = _handoffController(harness, plan, confirmer);
    addTearDown(controller.dispose);

    await _pumpConflictShell(tester, harness, controller);
    await tester.tap(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    await tester.pumpAndSettle();

    expect(find.text('开始新的练习？'), findsNothing);
    expect(find.byKey(const Key('confirmed-practice-route')), findsOneWidget);
    expect(confirmer.calls, 1);
    expect(harness.practice.practiceSessionId, _sessionId);
    expect(harness.workspace.currentSessionId, _sessionId);
  });

  testWidgets(
    'Practice committed while reading Plan opens replacement choice',
    (tester) async {
      final seedPlan = _plan(sourceThreadId: 'seed-thread');
      final harness = await _createHarness(
        client: _SeededHandoffAgentClient(_handoff(seedPlan)),
      );
      addTearDown(harness.dispose);
      final plan = _plan(sourceThreadId: harness.conversation.threadId!);
      final confirmer = _ConfirmPlanSpy(plan);
      var reads = 0;
      final controller = PracticePlanHandoffController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
        workspaceController: harness.workspace,
        readPlan: (_) async {
          reads++;
          if (reads == 1) {
            await _seedExistingPractice(harness, plan.sceneSelection.scene);
          }
          return plan;
        },
        confirmPlan: confirmer.call,
        idFactory: _fixedId,
      );
      addTearDown(controller.dispose);

      await _pumpConflictShell(tester, harness, controller);
      await tester.tap(
        find.byKey(
          const Key('confirm-practice-plan-practice-plan-session-1-2'),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        controller.failure,
        PracticePlanHandoffFailure.localExistingPractice,
      );
      expect(find.text('开始新的练习？'), findsOneWidget);
      await tester.tap(find.byKey(const Key('replace-existing-practice')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('confirmed-practice-route')), findsOneWidget);
      expect(confirmer.calls, 1);
      expect(harness.practice.practiceSessionId, _sessionId);
      expect(
        harness.practice.client.restorePractice(sessionId: _existingSessionId),
        throwsStateError,
      );
    },
  );

  testWidgets('server active Session conflict restores the source Thread', (
    tester,
  ) async {
    final seedPlan = _plan(sourceThreadId: 'seed-thread');
    final harness = await _createHarness(
      client: _SeededHandoffAgentClient(_handoff(seedPlan)),
    );
    addTearDown(harness.dispose);
    final sourceThreadId = harness.conversation.threadId!;
    final plan = _plan(sourceThreadId: sourceThreadId);
    var confirmationCalls = 0;
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      ieltsPreparationController: harness.ieltsPreparation,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async {
            confirmationCalls++;
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

    await _pumpConflictShell(tester, harness, controller);
    await tester.tap(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    await tester.pumpAndSettle();

    expect(controller.failure, PracticePlanHandoffFailure.serverActivePractice);
    expect(find.text('开始新的练习？'), findsNothing);
    expect(harness.workspace.hasResumable, isFalse);
    expect(harness.workspace.currentLease, isNull);
    expect(harness.conversation.threadId, sourceThreadId);
    expect(find.textContaining('当前设备无法直接替换'), findsOneWidget);

    await tester.tap(
      find.byKey(const Key('confirm-practice-plan-practice-plan-session-1-2')),
    );
    await tester.pumpAndSettle();

    expect(confirmationCalls, 2);
    expect(find.byKey(const Key('confirmed-practice-route')), findsNothing);
    expect(harness.workspace.currentLease, isNull);
    expect(harness.conversation.threadId, sourceThreadId);
  });

  testWidgets(
    'IELTS message confirmation registers its exact session selection',
    (tester) async {
      final assignment = testIeltsAssignment(
        mode: PracticeMode.part1,
        part1QuestionCount: 3,
      );
      final seedPlan = _ieltsPlan(
        sourceThreadId: 'seed-thread',
        assignment: assignment,
      );
      final harness = await _createHarness(
        client: _SeededHandoffAgentClient(_handoff(seedPlan)),
        practiceClient: FakePracticeClient(
          practiceExperience: PracticeExperience.ieltsSpeaking,
          sceneCategory: SceneCategory.ieltsSpeaking,
          practiceMode: PracticeMode.part1,
          turnLimit: assignment.turnBlueprints.length,
          ieltsAssignment: assignment,
        ),
      );
      addTearDown(harness.dispose);
      final plan = _ieltsPlan(
        sourceThreadId: harness.conversation.threadId!,
        assignment: assignment,
      );
      final controller = PracticePlanHandoffController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
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
            ieltsPreparationController: harness.ieltsPreparation,
            practicePlanHandoffController: controller,
          ),
          onGenerateRoute: (settings) {
            if (settings.name != AppRoutes.practice) {
              return null;
            }
            return MaterialPageRoute<void>(
              settings: settings,
              builder: (_) =>
                  const Scaffold(key: Key('confirmed-ielts-practice-route')),
            );
          },
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(
          const Key('confirm-practice-plan-practice-plan-session-1-2'),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('confirmed-ielts-practice-route')),
        findsOneWidget,
      );
      expect(
        harness.ieltsPreparation.selectionForSession(_sessionId),
        const IeltsPracticeSelection(part1SetId: 'part-1-set-test'),
      );
    },
  );

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
        ieltsPreparationController: harness.ieltsPreparation,
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
      expect(harness.ieltsPreparation.selectionForSession(_sessionId), isNull);
      expect(controller.errorMessage, isNull);
    },
  );

  test('starts IELTS history from the frozen plan assignment', () async {
    final assignment = testIeltsAssignment(
      mode: PracticeMode.part2,
      part3QuestionCount: 2,
    );
    final harness = await _createHarness(
      practiceClient: FakePracticeClient(
        practiceExperience: PracticeExperience.ieltsSpeaking,
        sceneCategory: SceneCategory.ieltsSpeaking,
        practiceMode: PracticeMode.part2,
        turnLimit: assignment.turnBlueprints.length,
        ieltsAssignment: assignment,
      ),
    );
    addTearDown(harness.dispose);
    final plan = _ieltsPlan(
      sourceThreadId: harness.conversation.threadId!,
      assignment: assignment,
    );
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      ieltsPreparationController: harness.ieltsPreparation,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async =>
              _bootstrap(plan),
      idFactory: _fixedId,
    );
    addTearDown(controller.dispose);

    expect(await controller.confirm(_handoff(plan)), isTrue);

    expect(
      harness.ieltsPreparation.selectionForSession(_sessionId),
      const IeltsPracticeSelection(topicGroupId: 'topic-group-test'),
    );
    expect(
      harness.ieltsPreparation
          .progress(PracticeMode.part2, 'topic-group-test')
          .inProgress,
      isTrue,
    );
  });

  test(
    'IELTS history write failure does not reverse created Practice',
    () async {
      final assignment = testIeltsAssignment(
        mode: PracticeMode.part1,
        part1QuestionCount: 3,
      );
      final harness = await _createHarness(
        practiceClient: FakePracticeClient(
          practiceExperience: PracticeExperience.ieltsSpeaking,
          sceneCategory: SceneCategory.ieltsSpeaking,
          practiceMode: PracticeMode.part1,
          turnLimit: assignment.turnBlueprints.length,
          ieltsAssignment: assignment,
        ),
        historyStore: const _FailingIeltsHistoryStore(),
      );
      addTearDown(harness.dispose);
      final plan = _ieltsPlan(
        sourceThreadId: harness.conversation.threadId!,
        assignment: assignment,
      );
      final controller = PracticePlanHandoffController(
        conversationController: harness.conversation,
        practiceController: harness.practice,
        ieltsPreparationController: harness.ieltsPreparation,
        workspaceController: harness.workspace,
        readPlan: (_) async => plan,
        confirmPlan:
            ({required plan, required input, required idempotencyKey}) async =>
                _bootstrap(plan),
        idFactory: _fixedId,
      );
      addTearDown(controller.dispose);

      expect(await controller.confirm(_handoff(plan)), isTrue);

      expect(harness.practice.practiceSessionId, _sessionId);
      expect(harness.workspace.currentSessionId, _sessionId);
      expect(
        harness.ieltsPreparation.selectionForSession(_sessionId),
        const IeltsPracticeSelection(part1SetId: 'part-1-set-test'),
      );
      expect(harness.ieltsPreparation.errorMessage, contains('本地题目进度'));
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
      ieltsPreparationController: harness.ieltsPreparation,
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

  test('replacement retry reuses its Lease without replacing twice', () async {
    final harness = await _createHarness();
    addTearDown(harness.dispose);
    final plan = _plan(sourceThreadId: harness.conversation.threadId!);
    await _seedExistingPractice(harness, plan.sceneSelection.scene);
    var calls = 0;
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      ieltsPreparationController: harness.ieltsPreparation,
      workspaceController: harness.workspace,
      readPlan: (_) async => plan,
      confirmPlan:
          ({required plan, required input, required idempotencyKey}) async {
            calls++;
            if (calls == 1) {
              throw StateError('ambiguous response');
            }
            return _bootstrap(plan);
          },
      idFactory: _fixedId,
    );
    addTearDown(controller.dispose);

    expect(
      await controller.confirm(_handoff(plan), replaceCurrentPractice: true),
      isFalse,
    );
    final replacementThreadId = harness.workspace.currentPracticeThreadId;
    final threadCount = harness.conversation.threads.length;
    expect(replacementThreadId, isNotNull);
    expect(
      harness.practice.client.restorePractice(sessionId: _existingSessionId),
      throwsStateError,
    );

    expect(
      await controller.confirm(_handoff(plan), replaceCurrentPractice: true),
      isTrue,
    );
    expect(harness.workspace.currentPracticeThreadId, replacementThreadId);
    expect(harness.conversation.threads, hasLength(threadCount));
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
      ieltsPreparationController: harness.ieltsPreparation,
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
    final sourceThreadId = harness.conversation.threadId!;
    final plan = _plan(sourceThreadId: sourceThreadId);
    final controller = PracticePlanHandoffController(
      conversationController: harness.conversation,
      practiceController: harness.practice,
      ieltsPreparationController: harness.ieltsPreparation,
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
    expect(harness.workspace.currentLease, isNull);
    expect(harness.conversation.threadId, sourceThreadId);
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
        ieltsPreparationController: harness.ieltsPreparation,
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

      expect(
        controller.failure,
        PracticePlanHandoffFailure.serverActivePractice,
      );
      expect(controller.errorMessage, contains('服务端还有一场未完成'));
      expect(harness.practice.practiceSessionId, isNull);
      expect(harness.workspace.currentLease, isNull);
    },
  );
}

Future<_Harness> _createHarness({
  AgentClient? client,
  PracticeClient? practiceClient,
  IeltsPracticeHistoryStore? historyStore,
}) async {
  final conversation = ConversationController(
    client: client ?? FakeAgentClient(),
  );
  final composer = ComposerController(conversationController: conversation);
  final practice = PracticeController(
    client:
        practiceClient ??
        FakePracticeClient(
          practiceExperience: PracticeExperience.interview,
          sceneCategory: SceneCategory.interviewProfessional,
          turnLimit: 3,
        ),
  );
  final ieltsPreparation = IeltsPreparationController(
    client: _UnusedIeltsQuestionBankClient(),
    historyStore: historyStore ?? const NullIeltsPracticeHistoryStore(),
  );
  await conversation.initialize();
  await ieltsPreparation.activateAccount('account-1');
  final workspace = PracticeWorkspaceController(
    conversationController: conversation,
    practiceController: practice,
    recordStore: MemoryPracticeLaunchRecordStore(),
  );
  await workspace.activateAccount('account-1');
  final launch = PreparationLaunchController(
    client: _UnusedPreparationLaunchClient(),
    contextProvider: () => null,
    threadIdProvider: () => conversation.threadId,
    goalActivator:
        ({required threadId, required selection, required clientOperationId}) =>
            throw UnimplementedError(),
    voiceActivator:
        ({
          required context,
          required scene,
          required bootstrap,
          required clientOperationId,
        }) => throw UnimplementedError(),
    workspaceController: workspace,
  );
  return _Harness(
    conversation: conversation,
    composer: composer,
    practice: practice,
    ieltsPreparation: ieltsPreparation,
    workspace: workspace,
    launch: launch,
  );
}

Future<void> _seedExistingPractice(
  _Harness harness,
  SceneDefinition scene, {
  bool withProgress = true,
}) async {
  final returnThreadId = harness.conversation.threadId!;
  final lease = await harness.workspace.acquireThread('existing-workspace');
  expect(lease, isNotNull);
  await activateTestPractice(
    controller: harness.practice,
    scene: scene,
    sessionId: _existingSessionId,
    clientOperationId: 'activate-existing-session',
  );
  if (withProgress) {
    expect(
      await harness.practice.submitPracticeText('An existing answer.'),
      isTrue,
    );
  }
  expect(
    await harness.workspace.commitSession(
      lease: lease!,
      goalId: null,
      sessionId: _existingSessionId,
      scene: scene,
    ),
    isTrue,
  );
  expect(await harness.conversation.selectThread(returnThreadId), isTrue);
  expect(harness.workspace.hasResumable, isTrue);
}

PracticePlanHandoffController _handoffController(
  _Harness harness,
  PracticePlan plan,
  _ConfirmPlanSpy confirmer,
) {
  return PracticePlanHandoffController(
    conversationController: harness.conversation,
    practiceController: harness.practice,
    ieltsPreparationController: harness.ieltsPreparation,
    workspaceController: harness.workspace,
    readPlan: (_) async => plan,
    confirmPlan: confirmer.call,
    idFactory: _fixedId,
  );
}

Future<void> _pumpConflictShell(
  WidgetTester tester,
  _Harness harness,
  PracticePlanHandoffController controller,
) async {
  await tester.pumpWidget(
    MaterialApp(
      home: SpeakUpShell(
        conversationController: harness.conversation,
        composerController: harness.composer,
        practiceController: harness.practice,
        practicePlanHandoffController: controller,
        preparationLaunchController: harness.launch,
      ),
      onGenerateRoute: (settings) {
        if (settings.name != AppRoutes.practice) {
          return null;
        }
        return MaterialPageRoute<void>(
          settings: settings,
          builder: (_) => const Scaffold(key: Key('confirmed-practice-route')),
        );
      },
    ),
  );
  await tester.pumpAndSettle();
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

PracticePlan _ieltsPlan({
  required String sourceThreadId,
  required IeltsPracticeAssignment assignment,
}) {
  final mode = assignment.mode;
  final scope = switch (mode) {
    PracticeMode.fullMock => '完整模考',
    PracticeMode.part1 => 'Part 1',
    PracticeMode.part2 => 'Part 2',
    PracticeMode.part3 => 'Part 3',
    PracticeMode.fullSimulation ||
    PracticeMode.focus => throw ArgumentError.value(mode, 'mode'),
  };
  final scene = testScene(
    id: 'ielts-speaking',
    experience: PracticeExperience.ieltsSpeaking,
    category: SceneCategory.ieltsSpeaking,
    name: '雅思口语',
    prompt: ScenePrompt(
      publicSceneBrief: '按雅思口语流程进行练习。',
      practiceGoal: '完成 $scope 练习。',
      userRole: 'Candidate',
      aiRole: 'Examiner',
      personaSummary: 'IELTS speaking examiner.',
      focusAreas: const <String>['fluency'],
      turnBlueprints: assignment.turnBlueprints,
    ),
    practiceOptions: <PracticeOption>[
      PracticeOption(
        id: 'option-ielts-${mode.wireValue}',
        sceneId: 'ielts-speaking',
        mode: mode,
        displayName: scope,
        suggestedDurationSeconds: 300,
        turnPolicyRef: 'turn-ielts-${mode.wireValue}',
        sessionPolicyRef: 'session-ielts-${mode.wireValue}',
        evaluationPolicyRef: 'evaluation-ielts-${mode.wireValue}',
      ),
    ],
  );
  return PracticePlan(
    id: _planId,
    userId: 'account-1',
    sourceThreadId: sourceThreadId,
    goalSnapshot: PreparationGoalSnapshot(
      id: 'goal-ielts',
      title: 'IELTS $scope',
      version: 1,
    ),
    preparationSnapshot: PreparationSnapshot(
      id: 'snapshot-ielts',
      sourceProfileId: 'profile-ielts',
      sourceVersion: 1,
      backgroundSnapshot: 'IELTS speaking practice.',
      createdAt: DateTime.utc(2026, 8, 4),
    ),
    sceneSelection: SceneSelectionSnapshot(
      scene: scene,
      selectedRoleIds: <String>[scene.roles.single.id],
      practiceOptionId: scene.practiceOptions.single.id,
    ),
    sessionPolicy: PreparationSessionPolicy(
      suggestedDurationSeconds: 300,
      minEffectiveTurns: assignment.turnBlueprints.length,
      maxEffectiveTurns: assignment.turnBlueprints.length,
      coverageCheckpointTurn: 1,
      maxFollowUpsPerQuestion: 0,
      earlyCompletionRule: 'all_questions_answered',
      retryAllowed: true,
      questionTranslationAllowed: true,
      questionTipsAllowed: true,
      avatarAllowed: false,
      speechFeedbackAllowed: true,
    ),
    practiceObjectives: const <PracticeObjective>[
      PracticeObjective(id: 'fluency', description: 'Speak fluently.'),
    ],
    ieltsAssignment: assignment,
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
    required this.ieltsPreparation,
    required this.workspace,
    required this.launch,
  });

  final ConversationController conversation;
  final ComposerController composer;
  final PracticeController practice;
  final IeltsPreparationController ieltsPreparation;
  final PracticeWorkspaceController workspace;
  final PreparationLaunchController launch;

  void dispose() {
    launch.dispose();
    workspace.dispose();
    ieltsPreparation.dispose();
    practice.dispose();
    composer.dispose();
    conversation.dispose();
  }
}

final class _UnusedIeltsQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<Never> getQuestionBank() => throw UnimplementedError();
}

final class _FailingIeltsHistoryStore implements IeltsPracticeHistoryStore {
  const _FailingIeltsHistoryStore();

  @override
  Future<String?> read(String accountId) async => null;

  @override
  Future<void> write(String accountId, String value) =>
      throw StateError('write failed');

  @override
  Future<void> delete(String accountId) async {}
}

final class _UnusedPreparationLaunchClient implements PreparationLaunchClient {
  @override
  Future<Never> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) => throw UnimplementedError();

  @override
  Future<Never> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) => throw UnimplementedError();

  @override
  Future<Never> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) => throw UnimplementedError();

  @override
  Future<Never> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) => throw UnimplementedError();

  @override
  Future<void> clearAccountState() async {}
}

final class _ConfirmPlanSpy {
  _ConfirmPlanSpy(this.plan);

  final PracticePlan plan;
  int calls = 0;

  Future<PreparationPracticeBootstrap> call({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    calls++;
    return _bootstrap(this.plan);
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
    return _withHandoff(snapshot);
  }

  AgentThreadSnapshot _withHandoff(AgentThreadSnapshot snapshot) {
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
  Future<AgentThreadSnapshot> setFocusedThread({
    required String threadId,
  }) async {
    return _withHandoff(await _delegate.setFocusedThread(threadId: threadId));
  }

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
const _existingSessionId = 'existing-session';
const _planId = 'practice-plan-$_sessionId';
