import '../../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'package:speakup/features/coaching/goal/goal.dart';

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'acquire creates one dedicated Thread and safely retries its lease',
    () async {
      final store = _InspectableRecordStore(writeFailures: 1);
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final initialThreadCount = harness.agent.threads.length;

      final firstAttempt = await workspace.acquireThread('launch-operation-1');

      expect(firstAttempt, isNull);
      expect(workspace.currentLease, isNotNull);
      expect(harness.agent.threadId, isNot(homeThreadId));
      expect(harness.agent.threads, hasLength(initialThreadCount + 1));

      final retried = await workspace.acquireThread('launch-operation-1');

      expect(retried, workspace.currentLease);
      expect(retried?.returnThreadId, homeThreadId);
      expect(harness.agent.threads, hasLength(initialThreadCount + 1));
      final record = jsonDecode((await store.read('account-1'))!);
      expect(
        record,
        containsPair('practice_thread_id', retried?.practiceThreadId),
      );
      expect(record, containsPair('return_thread_id', homeThreadId));
      expect(record, containsPair('schema_version', 5));

      final replacement = await workspace.acquireThread(
        'different-operation-2',
      );
      expect(replacement, isNotNull);
      expect(replacement?.practiceThreadId, isNot(retried?.practiceThreadId));
      expect(replacement?.returnThreadId, homeThreadId);
      expect(harness.agent.threads, hasLength(initialThreadCount + 2));
    },
  );

  test(
    'committed practice parks on home and resumes by exact identities',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final firstWorkspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(harness.agent.dispose);
      await firstWorkspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final launched = await _launchPractice(
        harness: harness,
        workspace: firstWorkspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
        sceneFamily: 'INTERVIEW',
      );

      expect(firstWorkspace.hasResumable, isTrue);
      expect(firstWorkspace.resumableHasProgress, isFalse);
      expect(await firstWorkspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, homeThreadId);
      expect(await harness.agent.createThread(), isTrue);
      final newerHomeThreadId = harness.agent.threadId;
      expect(newerHomeThreadId, isNot(homeThreadId));
      firstWorkspace.dispose();

      final restoredWorkspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(restoredWorkspace.dispose);
      await restoredWorkspace.activateAccount('account-1');

      expect(restoredWorkspace.currentTitle, '招聘初筛');
      expect(restoredWorkspace.currentSceneId, 'interview-screening');
      expect(
        restoredWorkspace.currentPresentationMode,
        ScenePresentationMode.standard,
      );
      expect(restoredWorkspace.hasResumable, isTrue);
      expect(await restoredWorkspace.resumeCurrentPractice(), isTrue);
      expect(harness.agent.threadId, launched.lease.practiceThreadId);
      expect(harness.agent.activeGoal?.id, launched.goal.id);
      expect(harness.agent.practiceSessionId, 'practice-session-1');
      expect(harness.agent.hasActivePractice, isTrue);
      expect(await restoredWorkspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, newerHomeThreadId);
    },
  );

  test(
    'only a confirmed answer marks a practice as resumable progress',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-progress-operation',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-progress-session',
      );

      expect(workspace.resumableHasProgress, isFalse);
      expect(
        await harness.agent.submitPracticeText('A confirmed answer.'),
        isTrue,
      );
      await Future<void>.delayed(Duration.zero);

      expect(workspace.resumableHasProgress, isTrue);
      expect(await workspace.parkCurrentPractice(), isTrue);
      final record = jsonDecode((await store.read('account-1'))!);
      expect(record, containsPair('completed_turns', 1));
    },
  );

  test(
    'park returns to the conversation being viewed, not a stale launch Home',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final launchHomeThreadId = harness.agent.threadId;
      await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, launchHomeThreadId);

      // The user switches to a different conversation (e.g. via the drawer)
      // while the practice stays parked and resumable.
      await harness.agent.createThread();
      final otherHomeThreadId = harness.agent.threadId;
      expect(otherHomeThreadId, isNot(launchHomeThreadId));

      // Leaving the training tab parks the practice again; the user should
      // land back on the conversation they were actually viewing instead of
      // the original launch Home.
      expect(await workspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, otherHomeThreadId);
    },
  );

  test(
    'roleplay presentation survives parking and cold workspace restore',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final firstWorkspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(harness.agent.dispose);
      await firstWorkspace.activateAccount('account-1');
      await _launchPractice(
        harness: harness,
        workspace: firstWorkspace,
        operationId: 'launch-roleplay-operation',
        sceneId: 'daily-hotel',
        sceneTitle: '酒店入住',
        sessionId: 'practice-roleplay-session',
        sceneFamily: 'DAILY',
      );
      expect(firstWorkspace.currentSceneFamily, 'DAILY');
      expect(
        firstWorkspace.currentPresentationMode,
        ScenePresentationMode.immersiveRoleplay,
      );
      expect(await firstWorkspace.parkCurrentPractice(), isTrue);
      firstWorkspace.dispose();

      final restoredWorkspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(restoredWorkspace.dispose);
      await restoredWorkspace.activateAccount('account-1');

      expect(restoredWorkspace.currentSceneFamily, 'DAILY');
      expect(
        restoredWorkspace.currentPresentationMode,
        ScenePresentationMode.immersiveRoleplay,
      );
      final record = jsonDecode((await store.read('account-1'))!);
      expect(
        record,
        containsPair('scene', containsPair('scene_family', 'DAILY')),
      );
      expect(await restoredWorkspace.resumeCurrentPractice(), isTrue);
      expect(restoredWorkspace.currentSceneId, 'daily-hotel');
    },
  );

  test(
    'practice starts without a focused Home Thread and parks back to empty Home',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      await harness.agent.clearFocusedThread();
      expect(harness.agent.threadId, isNull);
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');

      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-without-home-thread',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-without-home-thread',
      );

      expect(launched.lease.returnThreadId, isNull);
      expect(harness.agent.threadId, launched.lease.practiceThreadId);
      expect(harness.agent.hasActivePractice, isTrue);

      expect(await workspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, isNull);
      expect(workspace.hasResumable, isTrue);

      expect(await workspace.resumeCurrentPractice(), isTrue);
      expect(harness.agent.threadId, launched.lease.practiceThreadId);
      expect(
        harness.agent.practiceSessionId,
        'practice-session-without-home-thread',
      );
    },
  );

  test(
    'practice does not consume a pending Home draft Thread recovery',
    () async {
      final client = _FailingFocusAgentClient();
      final harness = await _createHarness(client: client);
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: _InspectableRecordStore(),
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      await harness.agent.clearFocusedThread();
      client.focusFailuresRemaining = 1;

      expect(await harness.agent.sendText('Keep this Home draft'), isFalse);
      expect(harness.agent.threadId, isNull);
      expect(harness.agent.hasPendingThreadCreationRecovery, isTrue);
      final createCallsAfterDraft = client.createCalls;

      expect(
        await workspace.acquireThread('practice-must-be-independent'),
        isNull,
      );

      expect(workspace.errorMessage, contains('先回到首页完成恢复'));
      expect(client.createCalls, createCallsAfterDraft);
      expect(harness.agent.threadId, isNull);

      await harness.agent.retryThreadHistory();
      final recoveredHomeThreadId = harness.agent.threadId;
      expect(recoveredHomeThreadId, isNotNull);

      final lease = await workspace.acquireThread(
        'practice-must-be-independent',
      );

      expect(lease, isNotNull);
      expect(lease?.returnThreadId, recoveredHomeThreadId);
      expect(lease?.practiceThreadId, isNot(recoveredHomeThreadId));
      expect(client.createCalls, createCallsAfterDraft + 1);
    },
  );

  test(
    'activation adopts an active Practice before its workspace record exists',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final practiceThreadId = harness.agent.threadId!;
      final scene = testScene(
        id: 'interview-practice-without-record',
        name: '英文面试',
        prompt: const ScenePrompt(
          publicSceneBrief: 'A Practice created before its workspace record.',
          practiceGoal: 'Complete the interview practice.',
          userRole: 'Candidate',
          aiRole: 'Interviewer',
          personaSummary: 'Professional and focused.',
          focusAreas: <String>['clarity'],
          turnBlueprints: <String>['Ask one interview question.'],
          suggestedDurationSeconds: 600,
        ),
      );
      final goal = await harness.agent.activateGoalForScene(
        threadId: practiceThreadId,
        scene: scene,
        clientOperationId: 'practice-goal-without-record',
      );
      harness.practice.armStart(
        threadId: practiceThreadId,
        sessionId: 'practice-session-without-record',
        planId: 'practice-plan-without-record',
        scene: scene,
      );
      await harness.agent.activateCreatedPractice(
        threadId: practiceThreadId,
        goalId: goal.id,
        scene: scene,
        sessionId: 'practice-session-without-record',
        planId: 'practice-plan-without-record',
        turnLimit: 3,
        clientOperationId: 'practice-voice-without-record',
      );
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });

      await workspace.activateAccount('account-1');

      expect(workspace.hasResumable, isTrue);
      expect(workspace.currentPracticeThreadId, practiceThreadId);
      expect(workspace.currentGoalId, goal.id);
      expect(workspace.currentSessionId, 'practice-session-without-record');
      expect(workspace.currentSceneId, scene.id);
      expect(workspace.currentTitle, scene.name);
      expect(workspace.currentLease?.returnThreadId, isNull);
      expect(harness.agent.threadId, isNull);
      expect(await store.read('account-1'), isNotNull);

      expect(await workspace.resumeCurrentPractice(), isTrue);
      expect(harness.agent.threadId, practiceThreadId);
      expect(
        harness.agent.practiceSessionId,
        'practice-session-without-record',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, isNull);
    },
  );

  test(
    'cold activation restores home focus while keeping practice resumable',
    () async {
      final store = _InspectableRecordStore();
      final firstHarness = await _createHarness();
      final firstWorkspace = PracticeWorkspaceController(
        agentController: firstHarness.agent,
        recordStore: store,
      );
      await firstWorkspace.activateAccount('account-1');
      final homeThreadId = firstHarness.agent.threadId;
      final launched = await _launchPractice(
        harness: firstHarness,
        workspace: firstWorkspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(firstHarness.agent.threadId, launched.lease.practiceThreadId);
      firstWorkspace.dispose();
      firstHarness.agent.dispose();

      final restartedAgent = AgentController(
        client: firstHarness.client,
        practiceClient: firstHarness.practice,
        clientIdFactory: (scope) => '$scope-restarted-operation',
      );
      final restartedWorkspace = PracticeWorkspaceController(
        agentController: restartedAgent,
        recordStore: store,
      );
      addTearDown(() {
        restartedWorkspace.dispose();
        restartedAgent.dispose();
      });

      await restartedWorkspace.activateAccount('account-1');

      expect(restartedAgent.isInitialized, isTrue);
      expect(restartedAgent.threadId, homeThreadId);
      expect(restartedWorkspace.hasResumable, isTrue);
      expect(
        restartedWorkspace.currentPracticeThreadId,
        launched.lease.practiceThreadId,
      );
    },
  );

  test(
    'cold activation discards an incomplete lease and allows a new launch',
    () async {
      final store = _InspectableRecordStore();
      final firstHarness = await _createHarness();
      final firstWorkspace = PracticeWorkspaceController(
        agentController: firstHarness.agent,
        recordStore: store,
      );
      await firstWorkspace.activateAccount('account-1');
      final homeThreadId = firstHarness.agent.threadId;
      final incomplete = await firstWorkspace.acquireThread(
        'incomplete-operation-1',
      );
      expect(incomplete, isNotNull);
      expect(firstHarness.agent.threadId, incomplete?.practiceThreadId);
      firstWorkspace.dispose();
      firstHarness.agent.dispose();

      final restartedAgent = AgentController(
        client: firstHarness.client,
        practiceClient: firstHarness.practice,
        clientIdFactory: (scope) => '$scope-restarted-operation',
      );
      final restartedWorkspace = PracticeWorkspaceController(
        agentController: restartedAgent,
        recordStore: store,
      );
      addTearDown(() {
        restartedWorkspace.dispose();
        restartedAgent.dispose();
      });

      await restartedWorkspace.activateAccount('account-1');

      expect(restartedAgent.threadId, homeThreadId);
      expect(restartedWorkspace.currentLease, isNull);
      expect(restartedWorkspace.hasResumable, isFalse);
      expect(await store.read('account-1'), isNull);

      final fresh = await restartedWorkspace.acquireThread(
        'fresh-launch-operation-2',
      );
      expect(fresh, isNotNull);
      expect(fresh?.practiceThreadId, isNot(incomplete?.practiceThreadId));
      expect(fresh?.returnThreadId, homeThreadId);
    },
  );

  test(
    'resume never falls back to another Session on the saved Thread',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.replaceSession(
        launched.lease.practiceThreadId,
        'practice-session-unrelated',
      );

      expect(await workspace.resumeCurrentPractice(), isFalse);
      expect(workspace.errorMessage, contains('无法核验上次练习'));
      expect(harness.agent.threadId, launched.lease.practiceThreadId);
      expect(harness.agent.practiceSessionId, isNull);
      expect(workspace.hasResumable, isTrue);
      expect(workspace.currentSessionId, 'practice-session-1');
      expect(await store.read('account-1'), isNotNull);

      expect(await workspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, homeThreadId);
      expect(workspace.hasResumable, isTrue);
    },
  );

  test(
    'resume clears an exact practice that became terminal on another client',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.complete(launched.lease.practiceThreadId);

      expect(await workspace.resumeCurrentPractice(), isFalse);

      expect(workspace.errorMessage, contains('已经结束'));
      expect(harness.agent.threadId, homeThreadId);
      expect(workspace.hasResumable, isFalse);
      expect(await store.read('account-1'), isNull);
    },
  );

  test(
    'replace ends the exact practice before creating a new Thread',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);

      final replacement = await workspace.replaceCurrentPractice(
        'replace-operation-2',
      );

      expect(harness.practice.endedSessionIds, ['practice-session-1']);
      expect(replacement, isNotNull);
      expect(
        replacement?.practiceThreadId,
        isNot(launched.lease.practiceThreadId),
      );
      expect(replacement?.returnThreadId, homeThreadId);
      expect(harness.agent.threadId, replacement?.practiceThreadId);
      expect(harness.agent.hasActivePractice, isFalse);
      expect(workspace.hasResumable, isFalse);
      final record = jsonDecode((await store.read('account-1'))!);
      expect(record, containsPair('goal_id', isNull));
      expect(record, containsPair('practice_session_id', isNull));
    },
  );

  test(
    'replace preserves an unverified Session and never ends an unrelated Session',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.replaceSession(
        launched.lease.practiceThreadId,
        'practice-session-unrelated',
      );

      final replacement = await workspace.replaceCurrentPractice(
        'replace-operation-2',
      );

      expect(replacement, isNull);
      expect(harness.practice.endedSessionIds, isEmpty);
      expect(workspace.currentSessionId, 'practice-session-1');
      expect(workspace.hasResumable, isTrue);
      expect(await store.read('account-1'), isNotNull);
      expect(workspace.errorMessage, contains('无法核验当前练习'));
      expect(harness.agent.threadId, launched.lease.practiceThreadId);
      expect(homeThreadId, isNot(launched.lease.practiceThreadId));
    },
  );

  test('replace returns Home when ending the current Session fails', () async {
    final store = _InspectableRecordStore();
    final harness = await _createHarness();
    final workspace = PracticeWorkspaceController(
      agentController: harness.agent,
      recordStore: store,
    );
    addTearDown(() {
      workspace.dispose();
      harness.agent.dispose();
    });
    await workspace.activateAccount('account-1');
    final homeThreadId = harness.agent.threadId;
    await _launchPractice(
      harness: harness,
      workspace: workspace,
      operationId: 'launch-operation-1',
      sceneId: 'interview-screening',
      sceneTitle: '招聘初筛',
      sessionId: 'practice-session-1',
    );
    expect(await workspace.parkCurrentPractice(), isTrue);
    harness.practice.endFailures = 1;

    final failed = await workspace.replaceCurrentPractice(
      'replace-operation-2',
    );

    expect(failed, isNull);
    expect(harness.agent.threadId, homeThreadId);
    expect(workspace.hasResumable, isTrue);
    expect(workspace.errorMessage, contains('进度仍已保留'));

    final retried = await workspace.replaceCurrentPractice(
      'replace-operation-2',
    );
    expect(retried, isNotNull);
    expect(harness.practice.endedSessionIds, ['practice-session-1']);
  });

  test(
    'replace restores Home before reporting a local record cleanup failure',
    () async {
      final store = _InspectableRecordStore(deleteFailures: 1);
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);

      final replacement = await workspace.replaceCurrentPractice(
        'replace-operation-2',
      );

      expect(replacement, isNull);
      expect(harness.practice.endedSessionIds, ['practice-session-1']);
      expect(harness.agent.threadId, homeThreadId);
      expect(workspace.hasResumable, isFalse);
      expect(workspace.errorMessage, contains('本机记录清理失败'));

      final retried = await workspace.acquireThread('replace-operation-2');
      expect(retried, isNotNull);
      expect(harness.agent.threadId, retried?.practiceThreadId);
      expect(retried?.returnThreadId, homeThreadId);
    },
  );

  test('park keeps focus while the current turn is still submitting', () async {
    final store = _InspectableRecordStore();
    final harness = await _createHarness();
    final workspace = PracticeWorkspaceController(
      agentController: harness.agent,
      recordStore: store,
    );
    addTearDown(() {
      workspace.dispose();
      harness.agent.dispose();
    });
    await workspace.activateAccount('account-1');
    final launched = await _launchPractice(
      harness: harness,
      workspace: workspace,
      operationId: 'launch-operation-1',
      sceneId: 'interview-screening',
      sceneTitle: '招聘初筛',
      sessionId: 'practice-session-1',
    );
    harness.practice.holdNextTextSubmission();
    final submission = harness.agent.submitPracticeText('A pending answer.');
    await Future<void>.delayed(Duration.zero);

    expect(await workspace.parkCurrentPractice(), isFalse);

    expect(harness.agent.threadId, launched.lease.practiceThreadId);
    expect(workspace.errorMessage, contains('正在提交'));
    harness.practice.failPendingTextSubmission();
    expect(await submission, isFalse);
  });

  test('parking a completed practice clears its resumable record', () async {
    final store = _InspectableRecordStore();
    final harness = await _createHarness();
    final workspace = PracticeWorkspaceController(
      agentController: harness.agent,
      recordStore: store,
    );
    addTearDown(() {
      workspace.dispose();
      harness.agent.dispose();
    });
    await workspace.activateAccount('account-1');
    final homeThreadId = harness.agent.threadId;
    final launched = await _launchPractice(
      harness: harness,
      workspace: workspace,
      operationId: 'launch-operation-1',
      sceneId: 'interview-screening',
      sceneTitle: '招聘初筛',
      sessionId: 'practice-session-1',
    );
    expect(await workspace.parkCurrentPractice(), isTrue);
    harness.practice.complete(launched.lease.practiceThreadId);
    expect(
      await harness.agent.selectThread(launched.lease.practiceThreadId),
      isTrue,
    );
    await harness.agent.restoreCreatedPractice(
      sessionId: 'practice-session-1',
      scene: launched.scene,
    );
    expect(harness.agent.practiceSessionId, 'practice-session-1');
    expect(harness.agent.hasActivePractice, isFalse);

    expect(await workspace.parkCurrentPractice(), isTrue);

    expect(harness.agent.threadId, homeThreadId);
    expect(workspace.hasResumable, isFalse);
    expect(workspace.currentLease, isNull);
    expect(await store.read('account-1'), isNull);
  });

  test(
    'completed interview returns to its source Agent thread for review',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId;
      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'agent-created-interview',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
        sceneFamily: 'INTERVIEW',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.complete(launched.lease.practiceThreadId);
      expect(
        await harness.agent.selectThread(launched.lease.practiceThreadId),
        isTrue,
      );
      await harness.agent.restoreCreatedPractice(
        sessionId: 'practice-session-1',
        scene: launched.scene,
      );
      expect(harness.agent.recordingState, PracticeRecordingState.completed);

      expect(await workspace.completeAndContinueWithAgent(), isTrue);

      expect(harness.agent.threadId, homeThreadId);
      expect(workspace.hasResumable, isFalse);
      expect(
        harness.agent.messages.any(
          (message) =>
              message.role == AgentMessageRole.user &&
              message.text.contains('招聘初筛') &&
              !message.text.contains('practice-session-1') &&
              !message.text.contains('练习记录 ID') &&
              !message.text.contains('profile ID') &&
              message.text.contains('直接读取这次练习的真实评分与报告'),
        ),
        isTrue,
      );
    },
  );

  test(
    'records stay isolated by account and private cleanup deletes one account',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        sceneId: 'interview-screening',
        sceneTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);

      await workspace.activateAccount('account-2');
      expect(workspace.hasResumable, isFalse);
      expect(await store.read('account-1'), isNotNull);
      await store.write('account-2', 'other-account-record');

      await workspace.activateAccount('account-1');
      expect(workspace.hasResumable, isTrue);
      await workspace.clearPrivateState();

      expect(workspace.hasResumable, isFalse);
      expect(await store.read('account-1'), isNull);
      expect(await store.read('account-2'), 'other-account-record');
    },
  );

  test(
    'a stale account read cannot publish into the newly active account',
    () async {
      final staleRead = Completer<String?>();
      final store = _InspectableRecordStore(
        readOverrides: <String, Future<String?>>{'account-1': staleRead.future},
      );
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });

      final firstActivation = workspace.activateAccount('account-1');
      await workspace.activateAccount('account-2');
      staleRead.complete('{invalid');
      await firstActivation;

      expect(workspace.isBusy, isFalse);
      expect(workspace.hasResumable, isFalse);
      expect(workspace.errorMessage, isNull);
      expect(store.deletedAccountIds, isEmpty);
    },
  );

  test(
    'a failed local record read can be retried before creating work',
    () async {
      final store = _InspectableRecordStore(readFailures: 1);
      final harness = await _createHarness();
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });

      await workspace.activateAccount('account-1');

      expect(workspace.canRetryActivation, isTrue);
      expect(workspace.errorMessage, contains('无法读取'));
      expect(await workspace.acquireThread('launch-operation-1'), isNull);

      await workspace.retryActivation();

      expect(workspace.canRetryActivation, isFalse);
      expect(workspace.errorMessage, isNull);
      expect(await workspace.acquireThread('launch-operation-1'), isNotNull);
    },
  );
}

Future<_Harness> _createHarness({AgentClient? client}) async {
  final practice = _WorkspacePracticeClient();
  final resolvedClient = client ?? FakeAgentClient();
  final agent = AgentController(
    client: resolvedClient,
    practiceClient: practice,
    clientIdFactory: (scope) => '$scope-client-operation',
  );
  await agent.initialize();
  return _Harness(agent: agent, client: resolvedClient, practice: practice);
}

Future<_LaunchedPractice> _launchPractice({
  required _Harness harness,
  required PracticeWorkspaceController workspace,
  required String operationId,
  required String sceneId,
  required String sceneTitle,
  required String sessionId,
  String? sceneFamily,
}) async {
  final lease = await workspace.acquireThread(operationId);
  expect(lease, isNotNull);
  final family = switch (sceneFamily) {
    'DAILY' => SceneFamily.daily,
    'WORKPLACE' => SceneFamily.workplace,
    'EXAM' => SceneFamily.exam,
    _ => SceneFamily.interview,
  };
  final model = switch (family) {
    SceneFamily.interview => SceneModel.interviewBasicDialogue,
    SceneFamily.exam => SceneModel.examBasicDialogue,
    SceneFamily.workplace => SceneModel.workplaceBasicDialogue,
    SceneFamily.daily => SceneModel.dailyBasicDialogue,
  };
  final scene = testScene(
    id: sceneId,
    family: family,
    model: model,
    name: sceneTitle,
    prompt: const ScenePrompt(
      publicSceneBrief: 'Test practice scene.',
      practiceGoal: 'Complete the test practice.',
      userRole: 'Learner',
      aiRole: 'Coach',
      personaSummary: 'Structured and focused.',
      focusAreas: <String>['clarity'],
      turnBlueprints: <String>['Ask one relevant question.'],
      suggestedDurationSeconds: 600,
    ),
  );
  final goal = await harness.agent.activateGoalForScene(
    threadId: lease!.practiceThreadId,
    scene: scene,
    clientOperationId: 'goal-$operationId',
  );
  expect(
    await workspace.commitSession(
      lease: lease,
      goalId: goal.id,
      sessionId: sessionId,
      scene: scene,
    ),
    isTrue,
  );
  harness.practice.armStart(
    threadId: lease.practiceThreadId,
    sessionId: sessionId,
    planId: 'practice-plan-$sessionId',
    scene: scene,
  );
  await harness.agent.activateCreatedPractice(
    threadId: lease.practiceThreadId,
    goalId: goal.id,
    scene: scene,
    sessionId: sessionId,
    planId: 'practice-plan-$sessionId',
    turnLimit: 3,
    clientOperationId: 'voice-$operationId',
  );
  return _LaunchedPractice(lease: lease, goal: goal, scene: scene);
}

final class _Harness {
  const _Harness({
    required this.agent,
    required this.client,
    required this.practice,
  });

  final AgentController agent;
  final AgentClient client;
  final _WorkspacePracticeClient practice;
}

final class _FailingFocusAgentClient
    implements AgentClient, AgentThreadHistoryClient {
  final FakeAgentClient _delegate = FakeAgentClient();

  String? failSetFocusedThreadId;
  bool failClearFocusedThread = false;
  int focusFailuresRemaining = 0;
  int createCalls = 0;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<AgentThreadSnapshot> restoreThread() => _delegate.restoreThread();

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) =>
      _delegate.listThreads(pageSize: pageSize, cursor: cursor);

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() =>
      _delegate.getFocusedThread();

  @override
  Future<AgentThreadSummary> createThread() {
    createCalls++;
    return _delegate.createThread();
  }

  @override
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) {
    if (focusFailuresRemaining > 0) {
      focusFailuresRemaining--;
      throw const AgentClientException(
        kind: AgentClientFailureKind.unavailable,
      );
    }
    if (threadId == failSetFocusedThreadId) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.unavailable,
      );
    }
    return _delegate.setFocusedThread(threadId: threadId);
  }

  @override
  Future<void> clearFocusedThread() {
    if (failClearFocusedThread) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.unavailable,
      );
    }
    return _delegate.clearFocusedThread();
  }

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
  Future<Goal> startScene({
    required String threadId,
    required SceneDefinition scene,
    required String clientOperationId,
  }) => _delegate.startScene(
    threadId: threadId,
    scene: scene,
    clientOperationId: clientOperationId,
  );

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) => _delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
  );
}

final class _LaunchedPractice {
  const _LaunchedPractice({
    required this.lease,
    required this.goal,
    required this.scene,
  });

  final PracticeWorkspaceLease lease;
  final Goal goal;
  final SceneDefinition scene;
}

final class _WorkspacePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  final Map<String, PracticeSessionSnapshot> _sessions =
      <String, PracticeSessionSnapshot>{};
  final Map<String, String> _sessionsByThread = <String, String>{};
  final List<String> endedSessionIds = <String>[];
  int endFailures = 0;
  _StartSeed? _nextStart;
  Completer<PracticeTurnConfirmation>? _pendingTextSubmission;

  void armStart({
    required String threadId,
    required String sessionId,
    required String planId,
    required SceneDefinition scene,
  }) {
    _nextStart = _StartSeed(
      threadId: threadId,
      sessionId: sessionId,
      planId: planId,
      scene: scene,
    );
  }

  void replaceSession(String threadId, String sessionId) {
    final currentSessionId = _sessionsByThread[threadId]!;
    final current = _sessions.remove(currentSessionId)!;
    _sessions[sessionId] = _activeSnapshot(
      sessionId: sessionId,
      planId: 'practice-plan-$sessionId',
      sceneFamily: current.sceneFamily,
      sceneModel: current.sceneModel,
      version: current.sessionVersion + 1,
    );
    _sessionsByThread[threadId] = sessionId;
  }

  void complete(String threadId) {
    final sessionId = _sessionsByThread[threadId]!;
    final current = _sessions[sessionId]!;
    _sessions[sessionId] = PracticeSessionSnapshot(
      sessionId: current.sessionId,
      planId: current.planId,
      sessionVersion: current.sessionVersion + 1,
      sceneFamily: current.sceneFamily,
      sceneModel: current.sceneModel,
      completedTurns: current.turnLimit,
      turnLimit: current.turnLimit,
      sessionCompleted: true,
    );
  }

  void holdNextTextSubmission() {
    _pendingTextSubmission = Completer<PracticeTurnConfirmation>();
  }

  void failPendingTextSubmission() {
    final pending = _pendingTextSubmission;
    _pendingTextSubmission = null;
    pending?.completeError(StateError('Test submission failure.'));
  }

  @override
  Future<void> clearAccountState() async {
    _sessions.clear();
    _sessionsByThread.clear();
    _nextStart = null;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    return _sessions[sessionId] ??
        (throw StateError('No exact Practice Session was prepared.'));
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final next = _nextStart;
    if (next == null || next.sessionId != sessionId) {
      throw StateError('No exact Practice Session was prepared.');
    }
    _nextStart = null;
    final snapshot = _activeSnapshot(
      sessionId: next.sessionId,
      planId: next.planId,
      sceneFamily: next.scene.family,
      sceneModel: next.scene.model,
      version: 1,
    );
    _sessions[sessionId] = snapshot;
    _sessionsByThread[next.threadId] = sessionId;
    return snapshot;
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    if (endFailures > 0) {
      endFailures--;
      throw StateError('Test end failure.');
    }
    final snapshot = _sessions[sessionId];
    if (snapshot == null || snapshot.sessionVersion != expectedSessionVersion) {
      throw StateError('The exact active Practice Session was not found.');
    }
    _sessions.remove(sessionId);
    _sessionsByThread.removeWhere((_, value) => value == sessionId);
    endedSessionIds.add(sessionId);
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.endedEarly,
      version: expectedSessionVersion + 1,
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    final pending = _pendingTextSubmission;
    if (pending != null) {
      return pending.future;
    }
    final current = _sessions.values.single;
    final completedTurns = current.completedTurns + 1;
    final completed = completedTurns >= current.turnLimit;
    final nextQuestion = completed
        ? null
        : PracticeQuestion(
            id: 'question-${current.sessionId}-$completedTurns',
            sessionId: current.sessionId,
            text: 'What happened next?',
          );
    _sessions[current.sessionId] = PracticeSessionSnapshot(
      sessionId: current.sessionId,
      planId: current.planId,
      sessionVersion: current.sessionVersion + 1,
      sceneFamily: current.sceneFamily,
      sceneModel: current.sceneModel,
      completedTurns: completedTurns,
      turnLimit: current.turnLimit,
      sessionCompleted: completed,
      currentQuestion: nextQuestion,
    );
    return Future<PracticeTurnConfirmation>.value(
      PracticeTurnConfirmation(
        turnId: 'turn-$completedTurns',
        sessionId: sessionId,
        questionId: questionId,
        candidateId: 'text-candidate-$completedTurns',
        answer: AgentMessage(
          id: 'answer-$completedTurns',
          role: AgentMessageRole.user,
          text: answerText,
        ),
        completedTurns: completedTurns,
        turnLimit: current.turnLimit,
        sessionCompleted: completed,
        sceneFamily: current.sceneFamily,
        sceneModel: current.sceneModel,
        sessionVersion: current.sessionVersion + 1,
        nextQuestion: nextQuestion,
      ),
    );
  }

  PracticeSessionSnapshot _activeSnapshot({
    required String sessionId,
    required String planId,
    required SceneFamily sceneFamily,
    required SceneModel sceneModel,
    required int version,
  }) {
    return PracticeSessionSnapshot(
      sessionId: sessionId,
      planId: planId,
      sceneFamily: sceneFamily,
      sceneModel: sceneModel,
      sessionVersion: version,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-$sessionId',
        sessionId: sessionId,
        text: 'Tell me about yourself.',
      ),
    );
  }
}

final class _StartSeed {
  const _StartSeed({
    required this.threadId,
    required this.sessionId,
    required this.planId,
    required this.scene,
  });

  final String threadId;
  final String sessionId;
  final String planId;
  final SceneDefinition scene;
}

final class _InspectableRecordStore implements PracticeLaunchRecordStore {
  _InspectableRecordStore({
    this.writeFailures = 0,
    this.readFailures = 0,
    this.deleteFailures = 0,
    this.readOverrides = const <String, Future<String?>>{},
  });

  final Map<String, String> values = <String, String>{};
  final Map<String, Future<String?>> readOverrides;
  final List<String> deletedAccountIds = <String>[];
  int writeFailures;
  int readFailures;
  int deleteFailures;

  @override
  Future<String?> read(String accountId) async {
    if (readFailures > 0) {
      readFailures--;
      throw StateError('Test read failure.');
    }
    return readOverrides[accountId] ?? Future<String?>.value(values[accountId]);
  }

  @override
  Future<void> write(String accountId, String value) async {
    if (writeFailures > 0) {
      writeFailures--;
      throw StateError('Test write failure.');
    }
    values[accountId] = value;
  }

  @override
  Future<void> delete(String accountId) async {
    if (deleteFailures > 0) {
      deleteFailures--;
      throw StateError('Test delete failure.');
    }
    deletedAccountIds.add(accountId);
    values.remove(accountId);
  }
}
