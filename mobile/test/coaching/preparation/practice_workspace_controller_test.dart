import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('pending workspace persists no Agent Thread or Goal identity', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);

    final lease = await harness.workspace.acquirePractice('launch-operation-1');

    expect(lease, isNotNull);
    expect(lease!.operationId, 'launch-operation-1');
    final record = jsonDecode((await harness.store.read(_accountId))!);
    expect(record, containsPair('schema_version', 8));
    expect(record, isNot(contains('practice_thread_id')));
    expect(record, isNot(contains('return_thread_id')));
    expect(record, isNot(contains('goal_id')));
    expect(record, containsPair('practice_plan_id', null));
    expect(record, containsPair('practice_session_id', null));
  });

  test(
    'committed practice parks and restores by exact Session identity',
    () async {
      final harness = _Harness();
      addTearDown(harness.dispose);
      await harness.workspace.activateAccount(_accountId);
      await harness.launch(sessionId: 'practice-session-1');

      expect(harness.practiceController.hasActivePractice, isTrue);
      expect(harness.workspace.hasResumable, isTrue);
      expect(
        harness.workspace.currentPlanId,
        testPracticePlanId('practice-session-1'),
      );
      expect(
        harness.workspace.hasResumableForPlan(
          testPracticePlanId('practice-session-1'),
        ),
        isTrue,
      );
      expect(
        harness.workspace.hasResumableForPlan('another-practice-plan'),
        isFalse,
      );
      expect(await harness.workspace.parkCurrentPractice(), isTrue);
      expect(harness.practiceController.hasActivePractice, isFalse);

      expect(await harness.workspace.resumeCurrentPractice(), isTrue);

      expect(harness.practiceClient.restoreCalls, 1);
      expect(
        harness.practiceController.practiceSessionId,
        'practice-session-1',
      );
      expect(harness.practiceController.scene?.id, testScenes.first.id);
    },
  );

  test(
    'restored workspace survives controller recreation without a Thread',
    () async {
      final store = MemoryPracticeLaunchRecordStore();
      final practiceClient = _WorkspacePracticeClient();
      final firstPractice = PracticeController(client: practiceClient);
      final firstWorkspace = PracticeWorkspaceController(
        practiceController: firstPractice,
        recordStore: store,
      );
      await firstWorkspace.activateAccount(_accountId);
      final first = _Harness.from(
        store: store,
        practiceClient: practiceClient,
        practiceController: firstPractice,
        workspace: firstWorkspace,
      );
      await first.launch(sessionId: 'practice-session-restart');
      expect(await firstWorkspace.parkCurrentPractice(), isTrue);
      firstWorkspace.dispose();
      firstPractice.dispose();

      final restartedPractice = PracticeController(client: practiceClient);
      final restartedWorkspace = PracticeWorkspaceController(
        practiceController: restartedPractice,
        recordStore: store,
      );
      addTearDown(() {
        restartedWorkspace.dispose();
        restartedPractice.dispose();
      });
      await restartedWorkspace.activateAccount(_accountId);

      expect(restartedWorkspace.currentSessionId, 'practice-session-restart');
      expect(await restartedWorkspace.resumeCurrentPractice(), isTrue);
      expect(restartedPractice.practiceSessionId, 'practice-session-restart');
    },
  );

  test('captured server progress remains resumable', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-progress', completedTurns: 1);
    await Future<void>.delayed(Duration.zero);

    expect(harness.workspace.resumableHasProgress, isTrue);
    final record = jsonDecode((await harness.store.read(_accountId))!);
    expect(
      record,
      containsPair('practice_plan_id', testPracticePlanId('practice-progress')),
    );
    expect(record, containsPair('completed_turns', 1));
  });

  test('replacing a practice ends the exact active Session', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-to-replace');

    final replacement = await harness.workspace.replaceCurrentPractice(
      'replacement-operation',
    );

    expect(replacement?.operationId, 'replacement-operation');
    expect(harness.practiceClient.endedSessionIds, ['practice-to-replace']);
    expect(harness.workspace.hasResumable, isFalse);
  });

  test('terminal Session clears its local resume record', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-completed');
    expect(await harness.workspace.parkCurrentPractice(), isTrue);
    harness.practiceClient.complete('practice-completed');

    expect(await harness.workspace.resumeCurrentPractice(), isFalse);

    expect(harness.workspace.hasResumable, isFalse);
    expect(await harness.store.read(_accountId), isNull);
  });

  test('missing remote Session clears its stale local resume record', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-deleted');
    expect(await harness.workspace.parkCurrentPractice(), isTrue);
    harness.practiceClient.remove('practice-deleted');

    final outcome = await harness.workspace.resumeCurrentPracticeWithOutcome();

    expect(outcome, PracticeWorkspaceResumeOutcome.stale);
    expect(harness.workspace.hasResumable, isFalse);
    expect(harness.workspace.errorMessage, contains('记录已清理'));
    expect(await harness.store.read(_accountId), isNull);
  });

  test('park refuses to detach while a turn is being submitted', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-submitting');
    harness.practiceClient.holdNextTextSubmission();

    final submission = harness.practiceController.submitPracticeText(
      'A pending answer.',
    );
    await Future<void>.delayed(Duration.zero);

    expect(await harness.workspace.parkCurrentPractice(), isFalse);
    expect(harness.practiceController.practiceSessionId, 'practice-submitting');
    expect(harness.workspace.errorMessage, contains('正在提交'));

    harness.practiceClient.failPendingTextSubmission();
    expect(await submission, isFalse);
  });

  test('resume rejects a remote snapshot for a different Session', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-exact');
    expect(await harness.workspace.parkCurrentPractice(), isTrue);
    harness.practiceClient.restoreOverride = testPracticeSnapshot(
      scene: testScenes.first,
      sessionId: 'practice-unrelated',
    );

    expect(await harness.workspace.resumeCurrentPractice(), isFalse);

    expect(harness.practiceController.practiceSessionId, isNull);
    expect(harness.workspace.currentSessionId, 'practice-exact');
    expect(harness.workspace.hasResumable, isTrue);
    expect(harness.workspace.errorMessage, contains('无法核验'));
    expect(await harness.store.read(_accountId), isNotNull);
  });

  test('replacement never ends an unrelated remote Session', () async {
    final harness = _Harness();
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount(_accountId);
    await harness.launch(sessionId: 'practice-exact');
    expect(await harness.workspace.parkCurrentPractice(), isTrue);
    harness.practiceClient.replaceWithUnrelatedSession(
      currentSessionId: 'practice-exact',
      unrelatedSessionId: 'practice-unrelated',
    );

    final replacement = await harness.workspace.replaceCurrentPractice(
      'replacement-operation',
    );

    expect(replacement, isNull);
    expect(harness.practiceClient.endedSessionIds, isEmpty);
    expect(harness.workspace.currentSessionId, 'practice-exact');
    expect(harness.workspace.hasResumable, isTrue);
    expect(harness.workspace.errorMessage, contains('无法核验'));
    expect(await harness.store.read(_accountId), isNotNull);
  });

  test(
    'replacement preserves progress when ending the Session fails',
    () async {
      final harness = _Harness();
      addTearDown(harness.dispose);
      await harness.workspace.activateAccount(_accountId);
      await harness.launch(sessionId: 'practice-end-retry');
      expect(await harness.workspace.parkCurrentPractice(), isTrue);
      harness.practiceClient.endFailures = 1;

      expect(
        await harness.workspace.replaceCurrentPractice('replacement-operation'),
        isNull,
      );
      expect(harness.workspace.hasResumable, isTrue);
      expect(harness.workspace.currentSessionId, 'practice-end-retry');
      expect(harness.workspace.errorMessage, contains('进度仍已保留'));
      expect(harness.practiceClient.endedSessionIds, isEmpty);

      expect(
        await harness.workspace.replaceCurrentPractice('replacement-operation'),
        isNotNull,
      );
      expect(harness.practiceClient.endedSessionIds, ['practice-end-retry']);
      expect(harness.workspace.hasResumable, isFalse);
    },
  );

  test(
    'replacement reports local cleanup failure after ending the Session',
    () async {
      final store = _InspectableRecordStore(deleteFailures: 1);
      final harness = _Harness(store: store);
      addTearDown(harness.dispose);
      await harness.workspace.activateAccount(_accountId);
      await harness.launch(sessionId: 'practice-cleanup-failure');
      expect(await harness.workspace.parkCurrentPractice(), isTrue);

      expect(
        await harness.workspace.replaceCurrentPractice('replacement-operation'),
        isNull,
      );

      expect(harness.practiceClient.endedSessionIds, [
        'practice-cleanup-failure',
      ]);
      expect(harness.workspace.hasResumable, isFalse);
      expect(harness.workspace.errorMessage, contains('本机记录清理失败'));
      expect(await store.read(_accountId), isNotNull);

      expect(
        await harness.workspace.acquirePractice('replacement-operation'),
        isNotNull,
      );
    },
  );

  test('workspace records and cleanup stay isolated by account', () async {
    final store = _InspectableRecordStore();
    final harness = _Harness(store: store);
    addTearDown(harness.dispose);
    await harness.workspace.activateAccount('account-1');
    await harness.launch(sessionId: 'practice-account-1');
    expect(await harness.workspace.parkCurrentPractice(), isTrue);

    await harness.workspace.activateAccount('account-2');
    expect(harness.workspace.hasResumable, isFalse);
    expect(await store.read('account-1'), isNotNull);
    await store.write('account-2', 'other-account-record');

    await harness.workspace.activateAccount('account-1');
    expect(harness.workspace.currentSessionId, 'practice-account-1');
    await harness.workspace.clearPrivateState();

    expect(await store.read('account-1'), isNull);
    expect(await store.read('account-2'), 'other-account-record');
  });

  test('a stale account read cannot publish into a new account', () async {
    final staleRead = Completer<String?>();
    final store = _InspectableRecordStore(
      readOverrides: <String, Future<String?>>{'account-1': staleRead.future},
    );
    final harness = _Harness(store: store);
    addTearDown(harness.dispose);

    final firstActivation = harness.workspace.activateAccount('account-1');
    await harness.workspace.activateAccount('account-2');
    staleRead.complete('{invalid');
    await firstActivation;

    expect(harness.workspace.isBusy, isFalse);
    expect(harness.workspace.hasResumable, isFalse);
    expect(harness.workspace.errorMessage, isNull);
    expect(store.deletedAccountIds, isEmpty);
    expect(
      await harness.workspace.acquirePractice('account-2-operation'),
      isNotNull,
    );
    final account2Record = jsonDecode((await store.read('account-2'))!);
    expect(account2Record, containsPair('account_id', 'account-2'));
  });

  test('a failed local record read can be retried before launch', () async {
    final store = _InspectableRecordStore(readFailures: 1);
    final harness = _Harness(store: store);
    addTearDown(harness.dispose);

    await harness.workspace.activateAccount(_accountId);

    expect(harness.workspace.canRetryActivation, isTrue);
    expect(harness.workspace.errorMessage, contains('无法读取'));
    expect(
      await harness.workspace.acquirePractice('launch-operation-1'),
      isNull,
    );

    await harness.workspace.retryActivation();

    expect(harness.workspace.canRetryActivation, isFalse);
    expect(harness.workspace.errorMessage, isNull);
    expect(
      await harness.workspace.acquirePractice('launch-operation-1'),
      isNotNull,
    );
  });

  test(
    'legacy Thread and Goal workspace schema is rejected, not migrated',
    () async {
      final store = MemoryPracticeLaunchRecordStore();
      await store.write(
        _accountId,
        jsonEncode(<String, Object?>{
          'schema_version': 6,
          'account_id': _accountId,
          'operation_id': 'legacy-operation',
          'practice_thread_id': 'legacy-thread',
          'return_thread_id': 'home-thread',
          'goal_id': 'legacy-goal',
          'practice_session_id': 'legacy-session',
          'scene': null,
          'completed_turns': 0,
        }),
      );
      final practice = PracticeController(client: _WorkspacePracticeClient());
      final workspace = PracticeWorkspaceController(
        practiceController: practice,
        recordStore: store,
      );
      addTearDown(() {
        workspace.dispose();
        practice.dispose();
      });

      await workspace.activateAccount(_accountId);

      expect(workspace.hasResumable, isFalse);
      expect(workspace.errorMessage, contains('已失效'));
      expect(await store.read(_accountId), isNull);
    },
  );

  test('schema 7 workspace without Plan identity is rejected', () async {
    final store = MemoryPracticeLaunchRecordStore();
    await store.write(
      _accountId,
      jsonEncode(<String, Object?>{
        'schema_version': 7,
        'account_id': _accountId,
        'operation_id': 'legacy-operation',
        'practice_session_id': 'legacy-session',
        'scene': null,
        'completed_turns': 0,
      }),
    );
    final practice = PracticeController(client: _WorkspacePracticeClient());
    final workspace = PracticeWorkspaceController(
      practiceController: practice,
      recordStore: store,
    );
    addTearDown(() {
      workspace.dispose();
      practice.dispose();
    });

    await workspace.activateAccount(_accountId);

    expect(workspace.hasResumable, isFalse);
    expect(workspace.errorMessage, contains('已失效'));
    expect(await store.read(_accountId), isNull);
  });
}

final class _Harness {
  factory _Harness({PracticeLaunchRecordStore? store}) {
    final resolvedStore = store ?? MemoryPracticeLaunchRecordStore();
    final client = _WorkspacePracticeClient();
    final practice = PracticeController(client: client);
    return _Harness.from(
      store: resolvedStore,
      practiceClient: client,
      practiceController: practice,
      workspace: PracticeWorkspaceController(
        practiceController: practice,
        recordStore: resolvedStore,
      ),
    );
  }

  _Harness.from({
    required this.store,
    required this.practiceClient,
    required this.practiceController,
    required this.workspace,
  });

  final PracticeLaunchRecordStore store;
  final _WorkspacePracticeClient practiceClient;
  final PracticeController practiceController;
  final PracticeWorkspaceController workspace;

  Future<void> launch({
    required String sessionId,
    int completedTurns = 0,
  }) async {
    final lease = await workspace.acquirePractice('launch-$sessionId');
    expect(lease, isNotNull);
    expect(
      await workspace.commitSession(
        lease: lease!,
        planId: testPracticePlanId(sessionId),
        sessionId: sessionId,
        scene: testScenes.first,
      ),
      isTrue,
    );
    practiceClient.arm(
      scene: testScenes.first,
      sessionId: sessionId,
      completedTurns: completedTurns,
    );
    await practiceController.activateCreatedPractice(
      scene: testScenes.first,
      sessionId: sessionId,
      planId: testPracticePlanId(sessionId),
      practiceMode: testScenes.first.practiceOptions.first.mode,
      turnLimit: 3,
      clientOperationId: 'voice-$sessionId',
    );
  }

  void dispose() {
    workspace.dispose();
    practiceController.dispose();
  }
}

final class _WorkspacePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  final Map<String, PracticeSessionSnapshot> _sessions = {};
  final Set<String> _notFoundSessionIds = {};
  final List<String> endedSessionIds = [];
  PracticeSessionSnapshot? _armed;
  PracticeSessionSnapshot? restoreOverride;
  Completer<PracticeTurnConfirmation>? _pendingTextSubmission;
  int restoreCalls = 0;
  int endFailures = 0;

  void arm({
    required SceneDefinition scene,
    required String sessionId,
    int completedTurns = 0,
  }) {
    _notFoundSessionIds.remove(sessionId);
    _armed = testPracticeSnapshot(
      scene: scene,
      sessionId: sessionId,
      completedTurns: completedTurns,
    );
  }

  void complete(String sessionId) {
    final current = _sessions[sessionId]!;
    _sessions[sessionId] = PracticeSessionSnapshot(
      sessionId: current.sessionId,
      planId: current.planId,
      practiceExperience: current.practiceExperience,
      sceneCategory: current.sceneCategory,
      practiceMode: current.practiceMode,
      capabilities: current.capabilities,
      sessionVersion: current.sessionVersion + 1,
      completedTurns: current.turnLimit,
      turnLimit: current.turnLimit,
      sessionCompleted: true,
    );
  }

  void remove(String sessionId) {
    _sessions.remove(sessionId);
    _notFoundSessionIds.add(sessionId);
  }

  void replaceWithUnrelatedSession({
    required String currentSessionId,
    required String unrelatedSessionId,
  }) {
    _sessions.remove(currentSessionId);
    _sessions[unrelatedSessionId] = testPracticeSnapshot(
      scene: testScenes.first,
      sessionId: unrelatedSessionId,
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
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final snapshot = _armed;
    if (snapshot == null || snapshot.sessionId != sessionId) {
      throw StateError('No matching Practice Session was armed.');
    }
    _armed = null;
    _sessions[sessionId] = snapshot;
    return snapshot;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    restoreCalls++;
    final override = restoreOverride;
    if (override != null) {
      return override;
    }
    final snapshot = _sessions[sessionId];
    if (snapshot != null) {
      return snapshot;
    }
    if (_notFoundSessionIds.contains(sessionId)) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.notFound,
      );
    }
    throw StateError('Unknown Practice Session.');
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
    final current = _sessions[sessionId];
    if (current == null || current.sessionVersion != expectedSessionVersion) {
      throw StateError('Practice Session version changed.');
    }
    _sessions.remove(sessionId);
    endedSessionIds.add(sessionId);
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.endedEarly,
      version: expectedSessionVersion + 1,
    );
  }

  @override
  Future<void> clearAccountState() async {
    _sessions.clear();
    _notFoundSessionIds.clear();
    _armed = null;
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) => throw UnimplementedError();

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) => throw UnimplementedError();

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    final pending = _pendingTextSubmission;
    if (pending == null) {
      throw StateError('No text submission was prepared.');
    }
    return pending.future;
  }
}

final class _InspectableRecordStore implements PracticeLaunchRecordStore {
  _InspectableRecordStore({
    this.readFailures = 0,
    this.deleteFailures = 0,
    this.readOverrides = const <String, Future<String?>>{},
  });

  final Map<String, String> values = <String, String>{};
  final Map<String, Future<String?>> readOverrides;
  final List<String> deletedAccountIds = <String>[];
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

const _accountId = 'account-workspace';
