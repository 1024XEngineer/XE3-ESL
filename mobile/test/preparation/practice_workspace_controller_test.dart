import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/preparation/practice_workspace_controller.dart';
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
      expect(record, containsPair('schema_version', 3));

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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
        sessionId: 'practice-session-1',
        scenarioType: 'INTERVIEW',
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
      expect(restoredWorkspace.currentScenarioId, 'interview-screening');
      expect(
        restoredWorkspace.currentPresentationMode,
        AgentScenePresentationMode.immersiveRoleplay,
      );
      expect(restoredWorkspace.hasResumable, isTrue);
      expect(await restoredWorkspace.resumeCurrentPractice(), isTrue);
      expect(harness.agent.threadId, launched.lease.practiceThreadId);
      expect(harness.agent.activeMatter?.id, launched.matter.id);
      expect(harness.agent.practiceSessionId, 'practice-session-1');
      expect(harness.agent.hasActivePractice, isTrue);
      expect(await restoredWorkspace.parkCurrentPractice(), isTrue);
      expect(harness.agent.threadId, newerHomeThreadId);
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
        scenarioId: 'daily-hotel',
        scenarioTitle: '酒店入住',
        sessionId: 'practice-roleplay-session',
        scenarioType: 'DAILY',
        presentationMode: AgentScenePresentationMode.immersiveRoleplay,
      );
      expect(firstWorkspace.currentScenarioType, 'DAILY');
      expect(
        firstWorkspace.currentPresentationMode,
        AgentScenePresentationMode.immersiveRoleplay,
      );
      expect(await firstWorkspace.parkCurrentPractice(), isTrue);
      firstWorkspace.dispose();

      final restoredWorkspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(restoredWorkspace.dispose);
      await restoredWorkspace.activateAccount('account-1');

      expect(restoredWorkspace.currentScenarioType, 'DAILY');
      expect(
        restoredWorkspace.currentPresentationMode,
        AgentScenePresentationMode.immersiveRoleplay,
      );
      final record = jsonDecode((await store.read('account-1'))!);
      expect(record, containsPair('scenario_type', 'DAILY'));
      expect(record, containsPair('presentation_mode', 'immersiveRoleplay'));
      expect(await restoredWorkspace.resumeCurrentPractice(), isTrue);
      expect(restoredWorkspace.currentScenarioId, 'daily-hotel');
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
    'activation adopts a legacy active Practice and makes it resumable',
    () async {
      final store = _InspectableRecordStore();
      final harness = await _createHarness();
      final legacyThreadId = harness.agent.threadId!;
      final legacyScene = const AgentScene(
        id: 'legacy-interview-practice',
        title: '旧版英文面试',
        description: 'A Practice created before workspace records existed.',
      );
      final legacyMatter = await harness.agent.activateMatterForScenario(
        threadId: legacyThreadId,
        scene: legacyScene,
        clientOperationId: 'legacy-practice-matter',
      );
      harness.practice.armStart(
        threadId: legacyThreadId,
        sessionId: 'legacy-practice-session',
      );
      await harness.agent.activateCreatedPractice(
        threadId: legacyThreadId,
        matterId: legacyMatter.id,
        sessionId: 'legacy-practice-session',
        turnLimit: 3,
        clientOperationId: 'legacy-practice-voice',
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
      expect(workspace.currentPracticeThreadId, legacyThreadId);
      expect(workspace.currentMatterId, legacyMatter.id);
      expect(workspace.currentSessionId, 'legacy-practice-session');
      expect(workspace.currentScenarioId, legacyScene.id);
      expect(workspace.currentTitle, legacyScene.title);
      expect(workspace.currentLease?.returnThreadId, isNull);
      expect(harness.agent.threadId, isNull);
      expect(await store.read('account-1'), isNotNull);

      expect(await workspace.resumeCurrentPractice(), isTrue);
      expect(harness.agent.threadId, legacyThreadId);
      expect(harness.agent.practiceSessionId, 'legacy-practice-session');
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
    'resume rejects a different Session restored for the saved Thread',
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.replaceSession(
        launched.lease.practiceThreadId,
        'practice-session-unrelated',
      );

      expect(await workspace.resumeCurrentPractice(), isFalse);
      expect(workspace.errorMessage, contains('服务端状态已经变化'));
      expect(harness.agent.threadId, homeThreadId);
      expect(harness.agent.practiceSessionId, isNull);
      expect(workspace.currentSessionId, isNull);
      expect(workspace.hasResumable, isFalse);
      expect(await store.read('account-1'), isNull);

      final fresh = await workspace.acquireThread('fresh-operation-2');
      expect(fresh, isNotNull);
      expect(fresh?.returnThreadId, homeThreadId);
    },
  );

  test(
    'resume keeps its record when a stale Session cannot return Home',
    () async {
      final store = _InspectableRecordStore();
      final client = _FailingFocusAgentClient();
      final harness = await _createHarness(client: client);
      final workspace = PracticeWorkspaceController(
        agentController: harness.agent,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        harness.agent.dispose();
      });
      await workspace.activateAccount('account-1');
      final homeThreadId = harness.agent.threadId!;
      final launched = await _launchPractice(
        harness: harness,
        workspace: workspace,
        operationId: 'launch-operation-1',
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
        sessionId: 'practice-session-1',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.replaceSession(
        launched.lease.practiceThreadId,
        'practice-session-unrelated',
      );
      client
        ..failSetFocusedThreadId = homeThreadId
        ..failClearFocusedThread = true;

      expect(await workspace.resumeCurrentPractice(), isFalse);

      expect(workspace.hasResumable, isTrue);
      expect(workspace.currentSessionId, 'practice-session-1');
      expect(await store.read('account-1'), isNotNull);
      expect(workspace.errorMessage, contains('记录仍已保留'));

      client
        ..failSetFocusedThreadId = null
        ..failClearFocusedThread = false;
      expect(await workspace.resumeCurrentPractice(), isFalse);
      expect(harness.agent.threadId, homeThreadId);
      expect(workspace.hasResumable, isFalse);
      expect(await store.read('account-1'), isNull);
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
      expect(record, containsPair('matter_id', isNull));
      expect(record, containsPair('practice_session_id', isNull));
    },
  );

  test(
    'replace discards a stale Session record without ending the unrelated Session',
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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

      expect(replacement, isNotNull);
      expect(harness.practice.endedSessionIds, isEmpty);
      expect(replacement?.returnThreadId, homeThreadId);
      expect(
        replacement?.practiceThreadId,
        isNot(launched.lease.practiceThreadId),
      );
      expect(harness.agent.threadId, replacement?.practiceThreadId);
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
      scenarioId: 'interview-screening',
      scenarioTitle: '招聘初筛',
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
      scenarioId: 'interview-screening',
      scenarioTitle: '招聘初筛',
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
      scenarioId: 'interview-screening',
      scenarioTitle: '招聘初筛',
      sessionId: 'practice-session-1',
    );
    expect(await workspace.parkCurrentPractice(), isTrue);
    harness.practice.complete(launched.lease.practiceThreadId);
    expect(
      await harness.agent.selectThread(launched.lease.practiceThreadId),
      isTrue,
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
        sessionId: 'practice-session-1',
        scenarioType: 'INTERVIEW',
      );
      expect(await workspace.parkCurrentPractice(), isTrue);
      harness.practice.complete(launched.lease.practiceThreadId);
      expect(
        await harness.agent.selectThread(launched.lease.practiceThreadId),
        isTrue,
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
        scenarioId: 'interview-screening',
        scenarioTitle: '招聘初筛',
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
  required String scenarioId,
  required String scenarioTitle,
  required String sessionId,
  String? scenarioType,
  AgentScenePresentationMode presentationMode =
      AgentScenePresentationMode.standard,
}) async {
  final lease = await workspace.acquireThread(operationId);
  expect(lease, isNotNull);
  final scene = AgentScene(
    id: scenarioId,
    title: scenarioTitle,
    description: 'Test practice scenario.',
    scenarioType: scenarioType,
    presentationMode: presentationMode,
  );
  final matter = await harness.agent.activateMatterForScenario(
    threadId: lease!.practiceThreadId,
    scene: scene,
    clientOperationId: 'matter-$operationId',
  );
  expect(
    await workspace.commitSession(
      lease: lease,
      matterId: matter.id,
      sessionId: sessionId,
      scenarioId: scenarioId,
      scenarioTitle: scenarioTitle,
      scenarioType: scenarioType,
      presentationMode: presentationMode,
    ),
    isTrue,
  );
  harness.practice.armStart(
    threadId: lease.practiceThreadId,
    sessionId: sessionId,
  );
  await harness.agent.activateCreatedPractice(
    threadId: lease.practiceThreadId,
    matterId: matter.id,
    sessionId: sessionId,
    turnLimit: 3,
    clientOperationId: 'voice-$operationId',
  );
  return _LaunchedPractice(lease: lease, matter: matter);
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
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
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

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) => _delegate.transcribeTurn(
    threadId: threadId,
    turnNumber: turnNumber,
    clientTurnId: clientTurnId,
  );

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) => _delegate.submitPracticeTurn(
    threadId: threadId,
    scene: scene,
    turnNumber: turnNumber,
    transcript: transcript,
    clientTurnId: clientTurnId,
  );

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) => _delegate.createReview(
    threadId: threadId,
    scene: scene,
    clientReviewId: clientReviewId,
  );
}

final class _LaunchedPractice {
  const _LaunchedPractice({required this.lease, required this.matter});

  final PracticeWorkspaceLease lease;
  final AgentMatter matter;
}

final class _WorkspacePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  final Map<String, PracticeSessionSnapshot> _sessions =
      <String, PracticeSessionSnapshot>{};
  final List<String> endedSessionIds = <String>[];
  int endFailures = 0;
  _StartSeed? _nextStart;
  Completer<PracticeTurnConfirmation>? _pendingTextSubmission;

  void armStart({required String threadId, required String sessionId}) {
    _nextStart = _StartSeed(threadId: threadId, sessionId: sessionId);
  }

  void replaceSession(String threadId, String sessionId) {
    final current = _sessions[threadId]!;
    _sessions[threadId] = _activeSnapshot(
      threadId: threadId,
      sessionId: sessionId,
      matter: current.matter,
      version: (current.sessionVersion ?? 1) + 1,
    );
  }

  void complete(String threadId) {
    final current = _sessions[threadId]!;
    _sessions[threadId] = PracticeSessionSnapshot(
      sessionId: current.sessionId,
      threadId: threadId,
      sessionVersion: (current.sessionVersion ?? 1) + 1,
      scenarioType: current.scenarioType,
      scenarioModel: current.scenarioModel,
      matter: current.matter,
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
    _nextStart = null;
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    return _sessions[threadId];
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    final next = _nextStart;
    if (next == null || next.threadId != threadId) {
      throw StateError('No exact Practice Session was prepared.');
    }
    _nextStart = null;
    final snapshot = _activeSnapshot(
      threadId: threadId,
      sessionId: next.sessionId,
      matter: activeMatter,
      version: 1,
    );
    _sessions[threadId] = snapshot;
    return PracticeStartResult(snapshot: snapshot);
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
    final matching = _sessions.entries
        .where((entry) => entry.value.sessionId == sessionId)
        .toList();
    if (matching.length != 1 ||
        matching.single.value.sessionVersion != expectedSessionVersion) {
      throw StateError('The exact active Practice Session was not found.');
    }
    _sessions.remove(matching.single.key);
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
    throw UnimplementedError();
  }

  PracticeSessionSnapshot _activeSnapshot({
    required String threadId,
    required String sessionId,
    required AgentMatter matter,
    required int version,
  }) {
    return PracticeSessionSnapshot(
      sessionId: sessionId,
      threadId: threadId,
      scenarioType: matter.scene.scenarioType,
      scenarioModel: switch (matter.scene.scenarioType) {
        'INTERVIEW' => 'INTERVIEW_BASIC_DIALOGUE',
        'DAILY' => 'DAILY_BASIC_DIALOGUE',
        'WORKPLACE' => 'WORKPLACE_BASIC_DIALOGUE',
        'EXAM' => 'EXAM_BASIC_DIALOGUE',
        _ => null,
      },
      sessionVersion: version,
      matter: matter,
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
  const _StartSeed({required this.threadId, required this.sessionId});

  final String threadId;
  final String sessionId;
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
