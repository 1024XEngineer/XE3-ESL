import '../../support/scene_fixtures.dart';

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'runs the typed launch chain and reuses every key after network failure',
    () async {
      final client = _LaunchClient(failFirstSession: true);
      final activations = <String>[];
      final goalKeys = <String>[];
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async {
              goalKeys.add(clientOperationId);
              return _context;
            },
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {
              activations.add(
                '${context.threadId}:${bootstrap.session.id}:$clientOperationId',
              );
            },
        idFactory: (scope) => '$scope-stable-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      expect(await controller.start(_selection), isFalse);
      expect(controller.canRetry, isTrue);
      expect(controller.errorMessage, contains('进度已保留'));
      expect(client.calls, ['profile', 'snapshot', 'plan', 'session']);

      expect(await controller.retry(), isTrue);
      expect(
        client.calls,
        ['profile', 'snapshot', 'plan', 'session'] +
            ['profile', 'snapshot', 'plan', 'session'],
      );
      expect(client.profileKeys.toSet(), {'prep-profile-stable-key'});
      expect(client.snapshotKeys.toSet(), {'prep-snapshot-stable-key'});
      expect(client.planKeys.toSet(), {'practice-plan-stable-key'});
      expect(client.sessionKeys.toSet(), {'practice-session-stable-key'});
      expect(goalKeys, ['agent-goal-stable-key', 'agent-goal-stable-key']);
      expect(activations, ['$_threadId:$_sessionId:practice-voice-stable-key']);
      expect(controller.bootstrap?.session.id, _sessionId);
      expect(controller.canRetry, isFalse);
    },
  );

  test(
    'creates a real Goal when a new user has only an Agent Thread',
    () async {
      AgentPracticeContext? context;
      final client = _LaunchClient();
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => context,
        threadIdProvider: () => _threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async {
              context = _context;
              return _context;
            },
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {},
        idFactory: (scope) => '$scope-recovered-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      expect(await controller.start(_selection), isTrue);
      expect(controller.backgroundSummary, _background);
      expect(client.calls, ['profile', 'snapshot', 'plan', 'session']);
      expect(client.lastPlanInput?.sceneId, _selection.scene.id);
      expect(client.lastPlanInput?.selectedRoleIds, _selection.selectedRoleIds);
    },
  );

  test('passes the IELTS selection to Plan creation', () async {
    final scene = testScene(
      id: 'ielts-speaking-part-1',
      family: SceneFamily.exam,
      model: SceneModel.ieltsSpeakingPart1,
    );
    final selection = PreparationLaunchSelection(
      scene: scene,
      selectedRoleIds: <String>[scene.roles.single.id],
      practiceOptionId: scene.practiceOptions.single.id,
      ieltsSelection: const IeltsPracticeSelection(
        mode: IeltsPracticeMode.part1,
        part1SetId: 'part-1-set-1',
      ),
    );
    final client = _LaunchClient();
    final controller = PreparationLaunchController(
      client: client,
      contextProvider: () => _context,
      threadIdProvider: () => _threadId,
      goalActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) async => _context,
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {},
      idFactory: (scope) => '$scope-ielts-plan-key',
    );
    addTearDown(controller.dispose);
    controller.updateBackgroundSummary(_background);

    expect(await controller.start(selection), isTrue);
    expect(client.lastPlanInput?.ieltsSelection, selection.ieltsSelection);
  });

  test(
    'locks a committed Session and reuses every key after voice failure',
    () async {
      final voiceKeys = <String>[];
      final goalKeys = <String>[];
      final client = _LaunchClient();
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async {
              goalKeys.add(clientOperationId);
              return _context;
            },
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {
              voiceKeys.add(clientOperationId);
              if (voiceKeys.length == 1) {
                throw const PreparationLaunchException(
                  kind: PreparationLaunchFailureKind.invalidResponse,
                  stage: PreparationLaunchStage.voice,
                );
              }
            },
        idFactory: (scope) => '$scope-voice-retry-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      expect(await controller.start(_selection), isFalse);
      expect(controller.canRetry, isTrue);
      expect(controller.isSelectionLocked, isTrue);
      expect(controller.bootstrap, same(_bootstrap));

      controller.selectionChanged();
      controller.updateBackgroundSummary('A different background.');

      expect(controller.backgroundSummary, _background);
      expect(controller.bootstrap, same(_bootstrap));
      expect(controller.canRetry, isTrue);
      expect(await controller.retry(), isTrue);

      expect(goalKeys, [
        'agent-goal-voice-retry-key',
        'agent-goal-voice-retry-key',
      ]);
      expect(client.calls, [
        'profile',
        'snapshot',
        'plan',
        'session',
        'profile',
        'snapshot',
        'plan',
        'session',
      ]);
      expect(client.profileKeys, [
        'prep-profile-voice-retry-key',
        'prep-profile-voice-retry-key',
      ]);
      expect(client.snapshotKeys, [
        'prep-snapshot-voice-retry-key',
        'prep-snapshot-voice-retry-key',
      ]);
      expect(client.planKeys, [
        'practice-plan-voice-retry-key',
        'practice-plan-voice-retry-key',
      ]);
      expect(client.sessionKeys, [
        'practice-session-voice-retry-key',
        'practice-session-voice-retry-key',
      ]);
      expect(voiceKeys, [
        'practice-voice-voice-retry-key',
        'practice-voice-voice-retry-key',
      ]);
      expect(controller.isSelectionLocked, isFalse);
    },
  );

  test(
    'fences a late response after the selected Agent Goal changes',
    () async {
      var context = _context;
      final profile = Completer<PreparationProfile>();
      final client = _LaunchClient(profileCompleter: profile);
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => context,
        threadIdProvider: () => context.threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => context,
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {},
        idFactory: (scope) => '$scope-context-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      final start = controller.start(_selection);
      context = const AgentPracticeContext(
        threadId: _threadId,
        goalId: 'goal-changed',
      );
      profile.complete(_profile);

      expect(await start, isFalse);
      expect(controller.stage, PreparationLaunchStage.context);
      expect(controller.errorMessage, contains('已变化'));
      expect(client.calls, isEmpty);
    },
  );

  test(
    'retries a malformed 201 Session with the same key before voice',
    () async {
      final voiceKeys = <String>[];
      final client = _LaunchClient(failFirstSessionResponse: true);
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => _context,
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {
              voiceKeys.add(clientOperationId);
            },
        idFactory: (scope) => '$scope-session-201-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      expect(await controller.start(_selection), isFalse);
      expect(controller.stage, PreparationLaunchStage.session);
      expect(controller.bootstrap, isNull);
      expect(controller.canRetry, isTrue);
      expect(controller.isSelectionLocked, isTrue);

      controller.selectionChanged();
      controller.updateBackgroundSummary('A different background.');

      expect(controller.backgroundSummary, _background);
      expect(controller.canRetry, isTrue);
      expect(await controller.retry(), isTrue);
      expect(client.sessionKeys, [
        'practice-session-session-201-key',
        'practice-session-session-201-key',
      ]);
      expect(voiceKeys, ['practice-voice-session-201-key']);
      expect(controller.bootstrap, same(_bootstrap));
      expect(controller.isSelectionLocked, isFalse);
    },
  );

  test(
    'logout clears input and suppresses an old account completion',
    () async {
      final profile = Completer<PreparationProfile>();
      final client = _LaunchClient(profileCompleter: profile);
      var activated = false;
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => _context,
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {
              activated = true;
            },
        idFactory: (scope) => '$scope-logout-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      final start = controller.start(_selection);
      await controller.clearPrivateState();
      profile.complete(_profile);

      expect(await start, isFalse);
      expect(client.clearCalls, 1);
      expect(controller.backgroundSummary, isEmpty);
      expect(controller.bootstrap, isNull);
      expect(controller.errorMessage, isNull);
      expect(activated, isFalse);
    },
  );

  test(
    'selection changes cannot orphan a Session while launch is pending',
    () async {
      final session = Completer<PreparationPracticeBootstrap>();
      final voice = Completer<void>();
      final client = _LaunchClient(sessionCompleter: session);
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        goalActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => _context,
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) => voice.future,
        idFactory: (scope) => '$scope-selection-lock-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      final start = controller.start(_selection);
      await Future<void>.delayed(Duration.zero);
      expect(client.calls, ['profile', 'snapshot', 'plan', 'session']);
      expect(controller.isStarting, isTrue);

      controller.selectionChanged();
      controller.updateBackgroundSummary('A stale field callback.');

      expect(controller.isStarting, isTrue);
      expect(controller.backgroundSummary, _background);
      session.complete(_bootstrap);
      await Future<void>.delayed(Duration.zero);

      expect(controller.stage, PreparationLaunchStage.voice);
      expect(controller.bootstrap, same(_bootstrap));
      expect(controller.isStarting, isTrue);

      voice.complete();

      expect(await start, isTrue);
      expect(controller.bootstrap, same(_bootstrap));
      expect(controller.canRetry, isFalse);
    },
  );

  test(
    'workspace launch creates, parks, and resumes an independent practice',
    () async {
      final harness = await _createWorkspaceLaunchHarness();
      addTearDown(harness.dispose);
      final originalThreadCount = harness.agentController.threads.length;

      expect(await harness.launchController.start(_selection), isTrue);

      final practiceThreadId = harness.agentController.threadId;
      final goalId = harness.agentController.activeGoal?.id;
      expect(practiceThreadId, isNotNull);
      expect(practiceThreadId, isNot(harness.homeThreadId));
      expect(
        harness.agentController.threads,
        hasLength(originalThreadCount + 1),
      );
      expect(harness.agentController.hasActivePractice, isTrue);
      expect(harness.agentController.practiceSessionId, _sessionId);
      expect(harness.launchController.hasResumablePractice, isTrue);
      expect(harness.launchController.resumableHasProgress, isFalse);
      expect(harness.launchController.resumableGoalId, goalId);

      final persisted =
          jsonDecode((await harness.recordStore.read(_userId))!)
              as Map<String, Object?>;
      expect(persisted, containsPair('practice_thread_id', practiceThreadId));
      expect(persisted, containsPair('return_thread_id', harness.homeThreadId));
      expect(persisted, containsPair('goal_id', goalId));
      expect(persisted, containsPair('practice_session_id', _sessionId));
      expect(
        persisted,
        containsPair('scene', containsPair('scene_id', _selection.scene.id)),
      );
      expect(persisted, containsPair('completed_turns', 0));

      expect(await harness.launchController.parkCurrentPractice(), isTrue);
      expect(harness.agentController.threadId, harness.homeThreadId);
      expect(harness.agentController.hasActivePractice, isFalse);
      expect(harness.launchController.hasResumablePractice, isTrue);

      expect(await harness.launchController.resumeCurrentPractice(), isTrue);
      expect(harness.agentController.threadId, practiceThreadId);
      expect(harness.agentController.activeGoal?.id, goalId);
      expect(harness.agentController.practiceSessionId, _sessionId);
      expect(harness.agentController.hasActivePractice, isTrue);
    },
  );

  test(
    'workspace voice retry reuses its Thread and every launch identity',
    () async {
      final harness = await _createWorkspaceLaunchHarness(failFirstVoice: true);
      addTearDown(harness.dispose);
      final originalThreadCount = harness.agentController.threads.length;

      expect(await harness.launchController.start(_selection), isFalse);

      final practiceThreadId =
          harness.workspaceController.currentPracticeThreadId;
      expect(practiceThreadId, isNotNull);
      expect(practiceThreadId, isNot(harness.homeThreadId));
      expect(harness.agentController.threadId, harness.homeThreadId);
      expect(
        harness.agentController.threads,
        hasLength(originalThreadCount + 1),
      );
      expect(harness.launchController.stage, PreparationLaunchStage.voice);
      expect(harness.launchController.canRetry, isTrue);
      expect(harness.launchController.hasResumablePractice, isTrue);

      expect(await harness.launchController.retry(), isTrue);

      expect(harness.agentController.threadId, practiceThreadId);
      expect(
        harness.agentController.threads,
        hasLength(originalThreadCount + 1),
      );
      expect(harness.agentController.practiceSessionId, _sessionId);
      expect(harness.agentController.hasActivePractice, isTrue);
      expect(harness.goalKeys, hasLength(2));
      expect(harness.goalKeys.toSet(), hasLength(1));
      expect(harness.voiceKeys, hasLength(2));
      expect(harness.voiceKeys.toSet(), hasLength(1));
      expect(harness.launchClient.profileKeys, hasLength(2));
      expect(harness.launchClient.profileKeys.toSet(), hasLength(1));
      expect(harness.launchClient.snapshotKeys, hasLength(2));
      expect(harness.launchClient.snapshotKeys.toSet(), hasLength(1));
      expect(harness.launchClient.planKeys, hasLength(2));
      expect(harness.launchClient.planKeys.toSet(), hasLength(1));
      expect(harness.launchClient.sessionKeys, hasLength(2));
      expect(harness.launchClient.sessionKeys.toSet(), hasLength(1));
    },
  );
}

Future<_WorkspaceLaunchHarness> _createWorkspaceLaunchHarness({
  bool failFirstVoice = false,
}) async {
  final agentClient = FakeAgentClient();
  final practiceClient = _WorkspacePracticeClient();
  final agentController = AgentController(
    client: agentClient,
    practiceClient: practiceClient,
    clientIdFactory: (scope) => '$scope-workspace-agent-key',
  );
  await agentController.initialize();
  final homeThreadId = agentController.threadId!;
  final recordStore = MemoryPracticeLaunchRecordStore();
  final workspaceController = PracticeWorkspaceController(
    agentController: agentController,
    recordStore: recordStore,
  );
  await workspaceController.activateAccount(_userId);
  final launchClient = _WorkspaceLaunchClient();
  final goalKeys = <String>[];
  final voiceKeys = <String>[];
  var voiceCalls = 0;
  final launchController = PreparationLaunchController(
    client: launchClient,
    contextProvider: () {
      final threadId = agentController.threadId;
      final goalId = agentController.activeGoal?.id;
      if (threadId == null || goalId == null) {
        return null;
      }
      return AgentPracticeContext(threadId: threadId, goalId: goalId);
    },
    threadIdProvider: () => agentController.threadId,
    goalActivator:
        ({
          required threadId,
          required selection,
          required clientOperationId,
        }) async {
          goalKeys.add(clientOperationId);
          final goal = await agentController.activateGoalForScene(
            threadId: threadId,
            scene: selection.scene,
            clientOperationId: clientOperationId,
          );
          return AgentPracticeContext(threadId: threadId, goalId: goal.id);
        },
    voiceActivator:
        ({
          required context,
          required scene,
          required bootstrap,
          required clientOperationId,
        }) async {
          voiceKeys.add(clientOperationId);
          voiceCalls++;
          if (failFirstVoice && voiceCalls == 1) {
            throw const PreparationLaunchException(
              kind: PreparationLaunchFailureKind.network,
              stage: PreparationLaunchStage.voice,
              retryable: true,
            );
          }
          practiceClient.armStart(
            threadId: context.threadId,
            sessionId: bootstrap.session.id,
          );
          await agentController.activateCreatedPractice(
            threadId: context.threadId,
            scene: scene,
            sessionId: bootstrap.session.id,
            planId: bootstrap.session.planId,
            turnLimit: bootstrap.maxEffectiveTurns,
            clientOperationId: clientOperationId,
          );
        },
    workspaceController: workspaceController,
    idFactory: (scope) => '$scope-workspace-launch-key',
  );
  launchController.updateBackgroundSummary(_background);
  return _WorkspaceLaunchHarness(
    agentController: agentController,
    workspaceController: workspaceController,
    launchController: launchController,
    launchClient: launchClient,
    recordStore: recordStore,
    homeThreadId: homeThreadId,
    goalKeys: goalKeys,
    voiceKeys: voiceKeys,
  );
}

final class _WorkspaceLaunchHarness {
  const _WorkspaceLaunchHarness({
    required this.agentController,
    required this.workspaceController,
    required this.launchController,
    required this.launchClient,
    required this.recordStore,
    required this.homeThreadId,
    required this.goalKeys,
    required this.voiceKeys,
  });

  final AgentController agentController;
  final PracticeWorkspaceController workspaceController;
  final PreparationLaunchController launchController;
  final _WorkspaceLaunchClient launchClient;
  final MemoryPracticeLaunchRecordStore recordStore;
  final String homeThreadId;
  final List<String> goalKeys;
  final List<String> voiceKeys;

  void dispose() {
    launchController.dispose();
    workspaceController.dispose();
    agentController.dispose();
  }
}

final class _WorkspaceLaunchClient implements PreparationLaunchClient {
  final profileKeys = <String>[];
  final snapshotKeys = <String>[];
  final planKeys = <String>[];
  final sessionKeys = <String>[];

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    profileKeys.add(idempotencyKey);
    expect(input.backgroundSummary, _background);
    return _profile;
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    snapshotKeys.add(idempotencyKey);
    expect(profileId, _profileId);
    expect(sourceVersion, 1);
    return _snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    planKeys.add(idempotencyKey);
    return _planForInput(input);
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    sessionKeys.add(idempotencyKey);
    expect(plan.id, _planId);
    expect(input.expectedPlanRevision, plan.revision);
    expect(input.userConfirmed, isTrue);
    return _bootstrap;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _WorkspacePracticeClient implements PracticeClient {
  final Map<String, PracticeSessionSnapshot> _sessions =
      <String, PracticeSessionSnapshot>{};
  ({String threadId, String sessionId})? _nextStart;

  void armStart({required String threadId, required String sessionId}) {
    _nextStart = (threadId: threadId, sessionId: sessionId);
  }

  @override
  Future<void> clearAccountState() async {
    _sessions.clear();
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
    final snapshot = PracticeSessionSnapshot(
      sessionId: next.sessionId,
      planId: _planId,
      sessionVersion: 1,
      sceneFamily: SceneFamily.interview,
      sceneModel: SceneModel.projectExperienceDeepDive,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-${next.sessionId}',
        sessionId: next.sessionId,
        text: 'Tell me about your experience.',
      ),
    );
    _sessions[sessionId] = snapshot;
    return snapshot;
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
    throw UnimplementedError();
  }
}

final class _LaunchClient implements PreparationLaunchClient {
  _LaunchClient({
    this.failFirstSession = false,
    this.failFirstSessionResponse = false,
    this.profileCompleter,
    this.sessionCompleter,
  });

  final bool failFirstSession;
  final bool failFirstSessionResponse;
  final Completer<PreparationProfile>? profileCompleter;
  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  final calls = <String>[];
  final profileKeys = <String>[];
  final snapshotKeys = <String>[];
  final planKeys = <String>[];
  final sessionKeys = <String>[];
  CreatePreparationPlanInput? lastPlanInput;
  int clearCalls = 0;
  int _sessionCalls = 0;

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    calls.add('profile');
    profileKeys.add(idempotencyKey);
    expect(input.backgroundSummary, _background);
    return profileCompleter?.future ?? _profile;
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    calls.add('snapshot');
    snapshotKeys.add(idempotencyKey);
    expect(profileId, _profileId);
    expect(sourceVersion, 1);
    return _snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    calls.add('plan');
    planKeys.add(idempotencyKey);
    lastPlanInput = input;
    expect(input.preparationSnapshotId, _snapshotId);
    expect(input.sourceThreadId, _threadId);
    expect(input.goalId, _goalId);
    return _plan;
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    calls.add('session');
    sessionKeys.add(idempotencyKey);
    _sessionCalls++;
    expect(plan.id, _planId);
    expect(input.expectedPlanRevision, plan.revision);
    expect(input.userConfirmed, isTrue);
    if (failFirstSession && _sessionCalls == 1) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.network,
        stage: PreparationLaunchStage.session,
        retryable: true,
      );
    }
    if (failFirstSessionResponse && _sessionCalls == 1) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.invalidResponse,
        stage: PreparationLaunchStage.session,
        statusCode: HttpStatus.created,
        retryable: true,
      );
    }
    return sessionCompleter?.future ?? _bootstrap;
  }

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }
}

const _threadId = 'thread-1';
const _goalId = 'goal-1';
const _userId = 'user-1';
const _profileId = 'profile-1';
const _snapshotId = 'preparation-snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _background = 'Backend engineer preparing a technical interview.';

const _context = AgentPracticeContext(threadId: _threadId, goalId: _goalId);

final _selectionScene = testScene(
  id: 'scene-1',
  family: SceneFamily.interview,
  model: SceneModel.projectExperienceDeepDive,
  name: 'Technical interview',
  version: 1,
  prompt: const ScenePrompt(
    publicSceneBrief: 'Backend engineer: technical interview practice',
    practiceGoal: 'Explain technical decisions with evidence.',
    userRole: 'Candidate',
    aiRole: 'Technical interviewer',
    personaSummary: 'Precise and evidence seeking.',
    focusAreas: <String>['system_design'],
    turnBlueprints: <String>['Ask for a project overview.'],
    suggestedDurationSeconds: 900,
  ),
  roles: const <RoleDefinition>[
    RoleDefinition(
      id: 'role-1',
      sceneId: 'scene-1',
      type: 'TECHNICAL_INTERVIEWER',
      displayName: 'Technical interviewer',
      responsibilities: 'Probe technical depth.',
      style: 'Precise.',
      practiceObjectives: <RolePracticeObjective>[
        RolePracticeObjective(
          objectiveId: 'system_design',
          description: 'Explain system design decisions.',
        ),
      ],
    ),
  ],
  practiceOptions: const <PracticeOption>[
    PracticeOption(
      id: 'option-1',
      sceneId: 'scene-1',
      type: PracticeOptionType.focus,
      displayName: 'System design focus',
      roleId: 'role-1',
    ),
  ],
);

final _selection = PreparationLaunchSelection(
  scene: _selectionScene,
  selectedRoleIds: const <String>['role-1'],
  practiceOptionId: 'option-1',
);

final _profile = PreparationProfile(
  id: _profileId,
  userId: _userId,
  backgroundSummary: _background,
  version: 1,
  updatedAt: DateTime.utc(2026, 7, 26),
);

final _snapshot = PreparationSnapshot(
  id: _snapshotId,
  sourceProfileId: _profileId,
  sourceVersion: 1,
  backgroundSnapshot: _background,
  createdAt: DateTime.utc(2026, 7, 26),
);

final _plan = _planForInput(
  CreatePreparationPlanInput(
    sourceThreadId: _threadId,
    goalId: _goalId,
    preparationSnapshotId: _snapshotId,
    sceneId: _selection.scene.id,
    sceneVersion: _selection.scene.version,
    selectedRoleIds: _selection.selectedRoleIds,
    practiceOptionId: _selection.practiceOptionId,
  ),
);

PracticePlan _planForInput(CreatePreparationPlanInput input) => PracticePlan(
  id: _planId,
  userId: _userId,
  sourceThreadId: input.sourceThreadId,
  goalSnapshot: PreparationGoalSnapshot(
    id: input.goalId!,
    title: _selection.scene.name,
    version: 1,
  ),
  preparationSnapshot: _snapshot,
  sceneSelection: SceneSelectionSnapshot(
    scene: _selection.scene,
    selectedRoleIds: input.selectedRoleIds,
    practiceOptionId: input.practiceOptionId,
  ),
  sessionPolicy: const PreparationSessionPolicy(
    suggestedDurationSeconds: 300,
    minEffectiveTurns: 1,
    maxEffectiveTurns: 3,
    coverageCheckpointTurn: 1,
    maxFollowUpsPerQuestion: 1,
    earlyCompletionRule: 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
  ),
  practiceObjectives: const <PracticeObjective>[
    PracticeObjective(
      id: 'system_design',
      description: 'Explain one design trade-off.',
    ),
  ],
  revision: 1,
  status: PracticePlanStatus.ready,
  createdAt: DateTime.utc(2026, 7, 26),
  updatedAt: DateTime.utc(2026, 7, 26),
);

final _bootstrap = PreparationPracticeBootstrap(
  session: PreparationPracticeSession(
    id: _sessionId,
    planId: _planId,
    sceneFamily: SceneFamily.interview,
    sceneModel: SceneModel.projectExperienceDeepDive,
    snapshotId: 'session-snapshot-1',
    status: 'starting',
    version: 1,
    createdAt: DateTime.utc(2026, 7, 26),
  ),
  preparationSnapshotId: _snapshotId,
  maxEffectiveTurns: 3,
);
