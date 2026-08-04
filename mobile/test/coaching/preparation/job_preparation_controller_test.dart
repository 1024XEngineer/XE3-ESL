import '../../support/scene_fixtures.dart';

import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/features/coaching/preparation/job_preparation_client.dart';
import 'package:speakup/features/coaching/preparation/job_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/job_preparation_draft_store.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'runs JD confirmation and preview before explicit Session start',
    () async {
      final client = _FakeJobPreparationClient();
      final voiceKeys = <String>[];
      final controller = _controller(
        client,
        voiceActivator:
            ({
              required context,
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {
              voiceKeys.add(clientOperationId);
            },
      );
      addTearDown(controller.dispose);
      controller.updateInput(_input);

      expect(await controller.analyze(), isTrue);
      expect(controller.step, JobPreparationStep.confirmation);
      expect(controller.candidate, isNotNull);
      expect(await controller.confirm(), isTrue);
      expect(controller.step, JobPreparationStep.setup);
      expect(await controller.createPreview(), isTrue);
      expect(controller.step, JobPreparationStep.preview);
      expect(client.calls, <String>[
        'create-target',
        'analyze-target',
        'confirm-target',
        'profile',
        'snapshot',
        'plan',
      ]);
      expect(client.sessionKeys, isEmpty);

      expect(await controller.startPractice(), isTrue);
      expect(client.calls.last, 'session');
      expect(client.sessionKeys, hasLength(1));
      expect(voiceKeys, hasLength(1));
    },
  );

  test('double start creates exactly one Session', () async {
    final session = Completer<PreparationPracticeBootstrap>();
    final client = _FakeJobPreparationClient(sessionCompleter: session);
    final controller = _controller(client);
    addTearDown(controller.dispose);
    controller.updateInput(_input);
    await controller.analyze();
    await controller.confirm();
    await controller.createPreview();

    final first = controller.startPractice();
    final second = controller.startPractice();
    expect(await second, isFalse);
    expect(client.sessionKeys, hasLength(1));
    session.complete(_bootstrap);

    expect(await first, isTrue);
    expect(client.sessionKeys, hasLength(1));
  });

  test('voice retry never creates a second committed Session', () async {
    final client = _FakeJobPreparationClient();
    var voiceCalls = 0;
    final voiceKeys = <String>[];
    final controller = _controller(
      client,
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {
            voiceCalls++;
            voiceKeys.add(clientOperationId);
            if (voiceCalls == 1) {
              throw StateError('voice unavailable');
            }
          },
    );
    addTearDown(controller.dispose);
    controller.updateInput(_input);
    await controller.analyze();
    await controller.confirm();
    await controller.createPreview();

    expect(await controller.startPractice(), isFalse);
    expect(controller.bootstrap, same(_bootstrap));
    expect(await controller.retry(), isTrue);

    expect(client.sessionKeys, hasLength(1));
    expect(voiceKeys, <String>[
      'job-target-voice-stable-key',
      'job-target-voice-stable-key',
    ]);
  });

  test('network retry keeps the same JobTarget analysis identity', () async {
    final client = _FakeJobPreparationClient(failFirstAnalysis: true);
    final controller = _controller(client);
    addTearDown(controller.dispose);
    controller.updateInput(_input);

    expect(await controller.analyze(), isFalse);
    expect(controller.canRetry, isTrue);
    expect(await controller.retry(), isTrue);

    expect(client.analysisKeys, <String>[
      'job-target-analysis-stable-key',
      'job-target-analysis-stable-key',
    ]);
    expect(client.createTargetKeys, <String>['job-target-stable-key']);
  });

  test(
    'editing input immediately invalidates analysis and late response',
    () async {
      final analysis = Completer<JobTarget>();
      final client = _FakeJobPreparationClient(analysisCompleter: analysis);
      final controller = _controller(client);
      addTearDown(controller.dispose);
      controller.updateInput(_input);

      final operation = controller.analyze();
      await _flush();
      controller.updateInput(_quickInput);
      analysis.complete(_target(JobTargetStage.awaitingConfirmation));

      expect(await operation, isFalse);
      expect(controller.input, _quickInput);
      expect(controller.candidate, isNull);
      expect(controller.plan, isNull);
      expect(controller.step, JobPreparationStep.input);
    },
  );

  test(
    'editing confirmed input invalidates confirmation and preview',
    () async {
      final client = _FakeJobPreparationClient();
      final controller = _controller(client);
      addTearDown(controller.dispose);
      controller.updateInput(_input);
      await controller.analyze();
      await controller.confirm();
      await controller.createPreview();
      expect(controller.plan, isNotNull);

      controller.updateInput(
        const JobTargetInput(
          source: JobTargetSource.jobDescription,
          jobDescription: 'A materially changed JD.',
          candidateBackground: _background,
        ),
      );

      expect(controller.candidate, isNull);
      expect(controller.plan, isNull);
      expect(controller.bootstrap, isNull);
      expect(controller.step, JobPreparationStep.input);
    },
  );

  test(
    'analysis polling accepts only the current terminal projection',
    () async {
      final client = _FakeJobPreparationClient(
        analyzeAsParsing: true,
        restoredTarget: _target(JobTargetStage.awaitingConfirmation),
      );
      final controller = _controller(client);
      addTearDown(controller.dispose);
      controller.updateInput(_input);

      expect(await controller.analyze(), isTrue);
      expect(
        client.calls,
        containsAllInOrder(<String>[
          'create-target',
          'analyze-target',
          'get-target',
        ]),
      );
      expect(controller.step, JobPreparationStep.confirmation);
    },
  );

  test(
    'draft survives controller restart and remains account isolated',
    () async {
      final store = MemoryJobPreparationDraftStore();
      final first = _controller(_FakeJobPreparationClient(), draftStore: store);
      await first.activateAccount(_userId);
      first.updateInput(_input);
      await _flush();
      first.dispose();

      final restored = _controller(
        _FakeJobPreparationClient(),
        draftStore: store,
      );
      addTearDown(restored.dispose);
      await restored.activateAccount(_userId);
      expect(restored.hasRestorableDraft, isTrue);
      expect(await restored.resumeDraft(), isTrue);
      expect(restored.input, _input);

      final otherAccount = _controller(
        _FakeJobPreparationClient(),
        draftStore: store,
      );
      addTearDown(otherAccount.dispose);
      await otherAccount.activateAccount('user-2');
      expect(otherAccount.hasRestorableDraft, isFalse);
    },
  );

  test(
    'logout clears the current account draft and fences late work',
    () async {
      final store = MemoryJobPreparationDraftStore();
      final analysis = Completer<JobTarget>();
      final client = _FakeJobPreparationClient(analysisCompleter: analysis);
      final controller = _controller(client, draftStore: store);
      addTearDown(controller.dispose);
      await controller.activateAccount(_userId);
      controller.updateInput(_input);
      final operation = controller.analyze();
      await _flush();

      final cleanup = controller.clearPrivateState();
      analysis.complete(_target(JobTargetStage.awaitingConfirmation));
      await cleanup;

      expect(await operation, isFalse);
      expect(client.clearCount, 1);
      final replacement = _controller(
        _FakeJobPreparationClient(),
        draftStore: store,
      );
      addTearDown(replacement.dispose);
      await replacement.activateAccount(_userId);
      expect(replacement.hasRestorableDraft, isFalse);
    },
  );

  test(
    'discard recovers an ambiguous create key before server discard',
    () async {
      final store = MemoryJobPreparationDraftStore();
      final firstClient = _FakeJobPreparationClient(failFirstCreate: true);
      final first = _controller(firstClient, draftStore: store);
      await first.activateAccount(_userId);
      first.updateInput(_input);
      expect(await first.analyze(), isFalse);
      await _flush();
      first.dispose();

      final client = _FakeJobPreparationClient();
      final restored = _controller(client, draftStore: store);
      addTearDown(restored.dispose);
      await restored.activateAccount(_userId);
      expect(restored.hasRestorableDraft, isTrue);

      expect(await restored.discardDraft(), isTrue);
      expect(client.calls, <String>['create-target', 'discard-target']);
      expect(client.createTargetKeys, <String>['job-target-stable-key']);
    },
  );

  test(
    'workspace creates a dedicated practice without a focused home Thread and resumes it exactly',
    () async {
      final harness = await _createWorkspaceHarness(
        _FakeJobPreparationClient(),
        clearHomeFocus: true,
      );
      addTearDown(harness.dispose);
      expect(harness.agentController.threadId, isNull);
      final originalThreadCount = harness.agentController.threads.length;

      await _prepareJobPreview(harness.controller);

      final practiceThreadId =
          harness.workspaceController.currentPracticeThreadId!;
      expect(practiceThreadId, isNot(_threadId));
      expect(harness.controller.plan?.sourceThreadId, practiceThreadId);
      expect(
        harness.workspaceController.currentPracticeThreadId,
        practiceThreadId,
      );
      expect(
        harness.agentController.threads,
        hasLength(originalThreadCount + 1),
      );
      expect(harness.agentController.threadId, isNull);

      expect(await harness.controller.startPractice(), isTrue);
      final sessionId = harness.agentController.practiceSessionId;
      final goalId = harness.agentController.activeGoal?.id;
      expect(sessionId, _sessionId);
      expect(harness.controller.hasResumablePractice, isTrue);
      expect(harness.controller.resumablePracticeTitle, _scene.name);
      expect(harness.workspaceController.currentSessionId, sessionId);

      expect(await harness.controller.parkCurrentPractice(), isTrue);
      expect(harness.agentController.threadId, isNull);
      expect(harness.agentController.hasActivePractice, isFalse);

      expect(await harness.controller.resumeCurrentPractice(), isTrue);
      expect(harness.agentController.threadId, practiceThreadId);
      expect(harness.agentController.practiceSessionId, sessionId);
      expect(harness.agentController.activeGoal?.id, goalId);
    },
  );

  test(
    'workspace preview retry reuses the exact lease and idempotency identities',
    () async {
      final client = _FakeJobPreparationClient(failFirstProfile: true);
      final harness = await _createWorkspaceHarness(client);
      addTearDown(harness.dispose);
      final homeThreadId = harness.agentController.threadId;
      final originalThreadCount = harness.agentController.threads.length;
      harness.controller.updateInput(_input);
      await harness.controller.analyze();
      await harness.controller.confirm();

      expect(await harness.controller.createPreview(), isFalse);
      final firstLease = harness.workspaceController.currentLease;
      expect(firstLease, isNotNull);
      expect(harness.agentController.threadId, homeThreadId);
      expect(
        harness.agentController.threads,
        hasLength(originalThreadCount + 1),
      );

      expect(await harness.controller.retry(), isTrue);

      expect(harness.workspaceController.currentLease, firstLease);
      expect(harness.agentController.threadId, homeThreadId);
      expect(
        harness.controller.plan?.sourceThreadId,
        firstLease?.practiceThreadId,
      );
      expect(
        harness.agentController.threads,
        hasLength(originalThreadCount + 1),
      );
      expect(harness.goalKeys, hasLength(2));
      expect(harness.goalKeys.toSet(), hasLength(1));
      expect(client.profileKeys, hasLength(2));
      expect(client.profileKeys.toSet(), hasLength(1));
    },
  );

  test(
    'workspace voice retry reuses the committed Session and exact lease',
    () async {
      final client = _FakeJobPreparationClient();
      final harness = await _createWorkspaceHarness(
        client,
        failFirstVoice: true,
      );
      addTearDown(harness.dispose);
      final homeThreadId = harness.agentController.threadId;
      await _prepareJobPreview(harness.controller);
      final lease = harness.workspaceController.currentLease;
      final practiceThreadId = lease?.practiceThreadId;
      expect(harness.agentController.threadId, homeThreadId);

      expect(await harness.controller.startPractice(), isFalse);
      expect(harness.agentController.threadId, homeThreadId);
      expect(harness.workspaceController.currentLease, lease);
      expect(harness.workspaceController.currentSessionId, _sessionId);
      expect(harness.controller.bootstrap, same(_bootstrap));

      expect(await harness.controller.retry(), isTrue);

      expect(client.sessionKeys, hasLength(1));
      expect(harness.voiceKeys, hasLength(2));
      expect(harness.voiceKeys.toSet(), hasLength(1));
      expect(harness.workspaceController.currentLease, lease);
      expect(harness.agentController.threadId, practiceThreadId);
      expect(harness.agentController.practiceSessionId, _sessionId);
    },
  );

  test(
    'replacement ends the resumable Session before creating a new workspace',
    () async {
      final first = await _createWorkspaceHarness(_FakeJobPreparationClient());
      addTearDown(first.dispose);
      final homeThreadId = first.agentController.threadId;
      await _prepareJobPreview(first.controller);
      expect(await first.controller.startPractice(), isTrue);
      final firstPracticeThreadId = first.agentController.threadId!;
      final firstSessionId = first.agentController.practiceSessionId!;
      expect(await first.controller.parkCurrentPractice(), isTrue);
      expect(first.agentController.threadId, homeThreadId);

      final replacementClient = _FakeJobPreparationClient(
        sessionResult: _replacementBootstrap,
      );
      final replacement = _workspaceJobController(
        client: replacementClient,
        agentController: first.agentController,
        practiceClient: first.practiceClient,
        workspaceController: first.workspaceController,
        goalKeys: first.goalKeys,
      );
      addTearDown(replacement.dispose);
      await replacement.activateAccount(_userId);
      replacement.updateInput(_input);
      await replacement.analyze();
      await replacement.confirm();

      expect(
        await replacement.createPreview(replaceCurrentPractice: true),
        isTrue,
      );

      final replacementThreadId =
          first.workspaceController.currentPracticeThreadId!;
      expect(first.practiceClient.endedSessionIds, <String>[firstSessionId]);
      expect(replacementThreadId, isNot(firstPracticeThreadId));
      expect(replacementThreadId, isNot(homeThreadId));
      expect(replacement.plan?.sourceThreadId, replacementThreadId);
      expect(first.workspaceController.hasResumable, isFalse);
      expect(first.agentController.threads, hasLength(3));
      expect(first.agentController.threadId, homeThreadId);

      expect(await replacement.startPractice(), isTrue);
      expect(first.agentController.practiceSessionId, _replacementSessionId);
      expect(first.workspaceController.currentSessionId, _replacementSessionId);
      expect(replacement.hasResumablePractice, isTrue);
    },
  );

  test(
    'workspace start reacquires its lease and parks to the latest Home Thread',
    () async {
      final harness = await _createWorkspaceHarness(
        _FakeJobPreparationClient(),
      );
      addTearDown(harness.dispose);
      await _prepareJobPreview(harness.controller);
      final practiceThreadId =
          harness.workspaceController.currentPracticeThreadId;
      final originalHomeThreadId = harness.agentController.threadId;

      expect(await harness.agentController.createThread(), isTrue);
      final latestHomeThreadId = harness.agentController.threadId;
      expect(latestHomeThreadId, isNot(originalHomeThreadId));

      expect(await harness.controller.startPractice(), isTrue);
      expect(harness.agentController.threadId, practiceThreadId);
      expect(await harness.controller.parkCurrentPractice(), isTrue);
      expect(harness.agentController.threadId, latestHomeThreadId);
    },
  );

  test(
    'Agent intent is only offered until the user explicitly applies it',
    () async {
      final client = _FakeJobPreparationClient();
      final controller = _controller(client);
      addTearDown(controller.dispose);

      controller.offerAgentIntent('A pasted role from the Agent conversation');
      expect(controller.agentIntentPrefill, isNotNull);
      expect(controller.input.jobDescription, isNull);
      expect(client.calls, isEmpty);

      controller.applyAgentIntentPrefill();
      expect(
        controller.input.jobDescription,
        'A pasted role from the Agent conversation',
      );
      expect(client.calls, isEmpty);
    },
  );
}

Future<void> _prepareJobPreview(JobPreparationController controller) async {
  controller.updateInput(_input);
  expect(await controller.analyze(), isTrue);
  expect(await controller.confirm(), isTrue);
  expect(await controller.createPreview(), isTrue);
}

Future<_WorkspaceHarness> _createWorkspaceHarness(
  _FakeJobPreparationClient client, {
  bool clearHomeFocus = false,
  bool failFirstVoice = false,
}) async {
  final agentClient = FakeAgentClient();
  final practiceClient = _WorkspacePracticeClient();
  final agentController = AgentController(
    client: agentClient,
    practiceClient: practiceClient,
    clientIdFactory: (scope) => '$scope-workspace-stable-key',
  );
  await agentController.initialize();
  if (clearHomeFocus) {
    await agentController.clearFocusedThread();
  }
  final workspaceController = PracticeWorkspaceController(
    agentController: agentController,
    recordStore: MemoryPracticeLaunchRecordStore(),
  );
  final goalKeys = <String>[];
  final voiceKeys = <String>[];
  var voiceCalls = 0;
  final controller = _workspaceJobController(
    client: client,
    agentController: agentController,
    practiceClient: practiceClient,
    workspaceController: workspaceController,
    goalKeys: goalKeys,
    voiceKeys: voiceKeys,
    failFirstVoice: failFirstVoice,
    voiceCalls: () => ++voiceCalls,
  );
  await controller.activateAccount(_userId);
  return _WorkspaceHarness(
    controller: controller,
    agentController: agentController,
    practiceClient: practiceClient,
    workspaceController: workspaceController,
    goalKeys: goalKeys,
    voiceKeys: voiceKeys,
  );
}

JobPreparationController _workspaceJobController({
  required _FakeJobPreparationClient client,
  required AgentController agentController,
  required _WorkspacePracticeClient practiceClient,
  required PracticeWorkspaceController workspaceController,
  required List<String> goalKeys,
  List<String>? voiceKeys,
  bool failFirstVoice = false,
  int Function()? voiceCalls,
}) {
  return JobPreparationController(
    client: client,
    workspaceController: workspaceController,
    threadIdProvider: () => agentController.threadId,
    goalActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async {
          goalKeys.add(clientOperationId);
          final goal = await agentController.activateGoalForScene(
            threadId: threadId,
            scene: _scene,
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
          voiceKeys?.add(clientOperationId);
          final call = voiceCalls?.call() ?? 1;
          if (failFirstVoice && call == 1) {
            throw StateError('Voice activation unavailable.');
          }
          practiceClient.armStart(
            sessionId: bootstrap.session.id,
            planId: bootstrap.session.planId,
            turnLimit: bootstrap.maxEffectiveTurns,
          );
          await agentController.activateCreatedPractice(
            threadId: context.threadId,
            goalId: context.goalId,
            scene: scene,
            sessionId: bootstrap.session.id,
            planId: bootstrap.session.planId,
            turnLimit: bootstrap.maxEffectiveTurns,
            clientOperationId: clientOperationId,
          );
        },
    idFactory: (scope) => '$scope-workspace-stable-key',
    analysisPollInterval: Duration.zero,
    maxAnalysisPollAttempts: 2,
  );
}

final class _WorkspaceHarness {
  const _WorkspaceHarness({
    required this.controller,
    required this.agentController,
    required this.practiceClient,
    required this.workspaceController,
    required this.goalKeys,
    required this.voiceKeys,
  });

  final JobPreparationController controller;
  final AgentController agentController;
  final _WorkspacePracticeClient practiceClient;
  final PracticeWorkspaceController workspaceController;
  final List<String> goalKeys;
  final List<String> voiceKeys;

  void dispose() {
    controller.dispose();
    workspaceController.dispose();
    agentController.dispose();
  }
}

final class _WorkspacePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  final Map<String, PracticeSessionSnapshot> _sessions =
      <String, PracticeSessionSnapshot>{};
  final List<String> endedSessionIds = <String>[];
  _WorkspaceStartSeed? _nextStart;

  void armStart({
    required String sessionId,
    required String planId,
    required int turnLimit,
  }) {
    _nextStart = _WorkspaceStartSeed(
      sessionId: sessionId,
      planId: planId,
      turnLimit: turnLimit,
    );
  }

  @override
  Future<void> clearAccountState() async {
    _sessions.clear();
    endedSessionIds.clear();
    _nextStart = null;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    return _sessions[sessionId] ??
        (throw StateError('No exact JD-first Session was prepared.'));
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final seed = _nextStart;
    if (seed == null || seed.sessionId != sessionId) {
      throw StateError('No exact JD-first Session was prepared.');
    }
    _nextStart = null;
    final snapshot = PracticeSessionSnapshot(
      sessionId: seed.sessionId,
      planId: seed.planId,
      sessionVersion: 1,
      sceneFamily: SceneFamily.interview,
      sceneModel: SceneModel.projectExperienceDeepDive,
      completedTurns: 0,
      turnLimit: seed.turnLimit,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-${seed.sessionId}',
        sessionId: seed.sessionId,
        text: 'Tell me about the most relevant project for this role.',
      ),
    );
    _sessions[sessionId] = snapshot;
    return snapshot;
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    final snapshot = _sessions[sessionId];
    if (snapshot == null || snapshot.sessionVersion != expectedSessionVersion) {
      throw StateError('The exact JD-first Session was not active.');
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

final class _WorkspaceStartSeed {
  const _WorkspaceStartSeed({
    required this.sessionId,
    required this.planId,
    required this.turnLimit,
  });

  final String sessionId;
  final String planId;
  final int turnLimit;
}

JobPreparationController _controller(
  _FakeJobPreparationClient client, {
  JobPreparationDraftStore? draftStore,
  JobPreparationVoiceActivator? voiceActivator,
}) {
  return JobPreparationController(
    client: client,
    draftStore: draftStore,
    threadIdProvider: () => _threadId,
    goalActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async => _context,
    voiceActivator:
        voiceActivator ??
        ({
          required context,
          required scene,
          required bootstrap,
          required clientOperationId,
        }) async {},
    idFactory: (scope) => '$scope-stable-key',
    analysisPollInterval: Duration.zero,
    maxAnalysisPollAttempts: 2,
  );
}

Future<void> _flush() async {
  await Future<void>.delayed(Duration.zero);
  await Future<void>.delayed(Duration.zero);
}

final class _FakeJobPreparationClient implements JobPreparationClient {
  _FakeJobPreparationClient({
    this.analysisCompleter,
    this.sessionCompleter,
    this.sessionResult,
    this.failFirstCreate = false,
    this.failFirstAnalysis = false,
    this.failFirstProfile = false,
    this.analyzeAsParsing = false,
    JobTarget? restoredTarget,
  }) : restoredTarget =
           restoredTarget ?? _target(JobTargetStage.awaitingConfirmation);

  final Completer<JobTarget>? analysisCompleter;
  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  final PreparationPracticeBootstrap? sessionResult;
  final bool failFirstCreate;
  final bool failFirstAnalysis;
  final bool failFirstProfile;
  final bool analyzeAsParsing;
  final JobTarget restoredTarget;

  final List<String> calls = <String>[];
  final List<String> createTargetKeys = <String>[];
  final List<String> analysisKeys = <String>[];
  final List<String> profileKeys = <String>[];
  final List<String> sessionKeys = <String>[];
  int clearCount = 0;
  int _createAttempts = 0;
  int _analysisAttempts = 0;
  int _profileAttempts = 0;

  @override
  Future<JobTarget> createJobTarget({
    required JobTargetInput input,
    required String idempotencyKey,
  }) async {
    calls.add('create-target');
    createTargetKeys.add(idempotencyKey);
    _createAttempts++;
    if (failFirstCreate && _createAttempts == 1) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: JobPreparationOperationStage.target,
        retryable: true,
      );
    }
    return _target(JobTargetStage.draft, input: input);
  }

  @override
  Future<JobTarget> getJobTarget(String jobTargetId) async {
    calls.add('get-target');
    return restoredTarget;
  }

  @override
  Future<JobTarget> updateJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required JobTargetInput input,
    required String idempotencyKey,
  }) async {
    calls.add('update-target');
    return _target(JobTargetStage.draft, input: input);
  }

  @override
  Future<JobTarget> analyzeJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  }) async {
    calls.add('analyze-target');
    analysisKeys.add(idempotencyKey);
    _analysisAttempts++;
    if (failFirstAnalysis && _analysisAttempts == 1) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: JobPreparationOperationStage.analysis,
        retryable: true,
      );
    }
    if (analysisCompleter != null) {
      return analysisCompleter!.future;
    }
    return analyzeAsParsing
        ? _target(JobTargetStage.parsing)
        : _target(JobTargetStage.awaitingConfirmation);
  }

  @override
  Future<JobTarget> confirmJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required int expectedAnalysisVersion,
    required JobTargetCandidate candidate,
    required String idempotencyKey,
  }) async {
    calls.add('confirm-target');
    return _target(JobTargetStage.confirmed, confirmedCandidate: candidate);
  }

  @override
  Future<JobTarget> discardJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  }) async {
    calls.add('discard-target');
    return _target(JobTargetStage.discarded);
  }

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    calls.add('profile');
    profileKeys.add(idempotencyKey);
    _profileAttempts++;
    if (failFirstProfile && _profileAttempts == 1) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: JobPreparationOperationStage.profile,
        retryable: true,
      );
    }
    expect(input.backgroundSummary, _background);
    expect(input.jobTargetId, _targetId);
    expect(input.jobTargetConfirmationVersion, 1);
    return _profile;
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    calls.add('snapshot');
    return _snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    calls.add('plan');
    expect(input.preparationSnapshotId, _snapshotId);
    return _planWithRevision(
      1,
      context: AgentPracticeContext(
        threadId: input.sourceThreadId!,
        goalId: input.goalId!,
      ),
    );
  }

  @override
  Future<PracticePlan> getPlan(String planId) async {
    calls.add('get-plan');
    return _plan;
  }

  @override
  Future<PracticePlan> revisePlan({
    required String planId,
    required RevisePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    calls.add('revise-plan');
    return _planWithRevision(input.expectedPlanRevision + 1);
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    calls.add('session');
    sessionKeys.add(idempotencyKey);
    expect(input.expectedPlanRevision, plan.revision);
    expect(input.userConfirmed, isTrue);
    final pending = sessionCompleter;
    if (pending != null) {
      return pending.future;
    }
    return sessionResult ?? _bootstrap;
  }

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }
}

JobTarget _target(
  JobTargetStage stage, {
  JobTargetInput input = _input,
  JobTargetCandidate? confirmedCandidate,
}) {
  final now = DateTime.utc(2026, 7, 26, 12);
  final analysis = switch (stage) {
    JobTargetStage.parsing => JobTargetAnalysis(
      inputVersion: 1,
      analysisVersion: 1,
      attempt: 1,
      status: JobTargetAnalysisStatus.running,
      startedAt: now,
    ),
    JobTargetStage.awaitingConfirmation ||
    JobTargetStage.confirmed => JobTargetAnalysis(
      inputVersion: 1,
      analysisVersion: 1,
      attempt: 1,
      status: JobTargetAnalysisStatus.succeeded,
      candidate: _candidate,
      startedAt: now,
      finishedAt: now,
    ),
    JobTargetStage.analysisFailed => JobTargetAnalysis(
      inputVersion: 1,
      analysisVersion: 1,
      attempt: 1,
      status: JobTargetAnalysisStatus.failed,
      stableErrorCategory: 'provider_unavailable',
      startedAt: now,
      finishedAt: now,
    ),
    JobTargetStage.draft || JobTargetStage.discarded => null,
  };
  return JobTarget(
    id: _targetId,
    userId: _userId,
    input: input,
    inputVersion: 1,
    stage: stage,
    analysis: analysis,
    confirmation: stage == JobTargetStage.confirmed
        ? JobTargetConfirmation(
            inputVersion: 1,
            analysisVersion: 1,
            confirmationVersion: 1,
            candidate: confirmedCandidate ?? _candidate,
            confirmedAt: now,
          )
        : null,
    createdAt: now,
    updatedAt: now,
  );
}

PracticePlan _planWithRevision(
  int revision, {
  AgentPracticeContext context = _context,
}) {
  return PracticePlan(
    id: _planId,
    userId: _userId,
    sourceThreadId: context.threadId,
    goalSnapshot: PreparationGoalSnapshot(
      id: context.goalId,
      title: _scene.name,
      version: 1,
    ),
    preparationSnapshot: _snapshot,
    sceneSelection: SceneSelectionSnapshot(
      scene: _scene,
      selectedRoleIds: const <String>[_roleId],
      practiceOptionId: _optionId,
    ),
    sessionPolicy: _policy,
    practiceObjectives: _objectives,
    revision: revision,
    status: PracticePlanStatus.ready,
    createdAt: _now,
    updatedAt: _now,
  );
}

final DateTime _now = DateTime.utc(2026, 7, 26, 12);

const _input = JobTargetInput(
  source: JobTargetSource.jobDescription,
  jobDescription: 'Build reliable APIs and explain trade-offs.',
  candidateBackground: _background,
  practiceFocus: 'System design',
);

const _quickInput = JobTargetInput(
  source: JobTargetSource.quickStart,
  jobTitle: 'Backend engineer',
  candidateBackground: _background,
);

const _candidate = JobTargetCandidate(
  source: JobTargetSource.jobDescription,
  generalAdviceOnly: false,
  jobTitle: 'Backend engineer',
  seniority: 'Senior',
  responsibilities: <String>['Build reliable APIs'],
  coreSkills: <String>['Go services'],
  communicationFocus: <String>['Explain trade-offs'],
  practiceGoals: <String>['Practice system design'],
  scopeNotice: 'Based on the supplied JD.',
  catalogRecommendation: JobTargetCatalogRecommendation(
    sceneId: _sceneId,
    sceneVersion: 1,
    selectedRoleIds: <String>[_roleId],
    practiceOptionId: _optionId,
  ),
);

final PreparationProfile _profile = PreparationProfile(
  id: _profileId,
  userId: _userId,
  backgroundSummary: _background,
  jobTargetId: _targetId,
  jobTargetConfirmationVersion: 1,
  version: 1,
  updatedAt: _now,
);

final PreparationSnapshot _snapshot = PreparationSnapshot(
  id: _snapshotId,
  sourceProfileId: _profileId,
  sourceVersion: 1,
  sourceJobTargetId: _targetId,
  sourceJobTargetConfirmationVersion: 1,
  jobTargetInput: _input,
  jobTargetCandidate: _candidate,
  backgroundSnapshot: _background,
  createdAt: _now,
);

final _scene = testScene(
  id: _sceneId,
  family: SceneFamily.interview,
  model: SceneModel.projectExperienceDeepDive,
  name: 'Technical interview',
  version: 1,
  prompt: _prompt,
  roles: <RoleDefinition>[_role],
  practiceOptions: <PracticeOption>[_option],
);

const _prompt = ScenePrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: <String>['system_design'],
  turnBlueprints: <String>['Ask for a project overview.'],
  suggestedDurationSeconds: 900,
);

final _role = testRole(
  id: _roleId,
  sceneId: _sceneId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical interviewer',
  responsibilities: 'Probe technical depth.',
  style: 'Precise',
  practiceObjectiveIds: <String>['system_design'],
);

final _option = testPracticeOption(
  id: _optionId,
  sceneId: _sceneId,
  type: PracticeOptionType.focus,
  displayName: 'System design focus',
  roleId: _roleId,
);

const _objectives = <PracticeObjective>[
  PracticeObjective(
    id: 'system_design',
    description: 'Explain one design trade-off.',
  ),
];

const _policy = PreparationSessionPolicy(
  suggestedDurationSeconds: 720,
  minEffectiveTurns: 2,
  maxEffectiveTurns: 5,
  coverageCheckpointTurn: 3,
  maxFollowUpsPerQuestion: 2,
  earlyCompletionRule: 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
);

final PracticePlan _plan = _planWithRevision(1);

final PreparationPracticeBootstrap _bootstrap = PreparationPracticeBootstrap(
  session: PreparationPracticeSession(
    id: _sessionId,
    planId: _planId,
    sceneFamily: SceneFamily.interview,
    sceneModel: SceneModel.projectExperienceDeepDive,
    snapshotId: 'session-snapshot-1',
    status: 'starting',
    version: 1,
    createdAt: _now,
  ),
  preparationSnapshotId: _snapshotId,
  maxEffectiveTurns: 5,
);

final PreparationPracticeBootstrap _replacementBootstrap =
    PreparationPracticeBootstrap(
      session: PreparationPracticeSession(
        id: _replacementSessionId,
        planId: _planId,
        sceneFamily: SceneFamily.interview,
        sceneModel: SceneModel.projectExperienceDeepDive,
        snapshotId: 'session-snapshot-2',
        status: 'starting',
        version: 1,
        createdAt: _now,
      ),
      preparationSnapshotId: _snapshotId,
      maxEffectiveTurns: 5,
    );

const _context = AgentPracticeContext(threadId: _threadId, goalId: _goalId);

const _userId = 'user-1';
const _targetId = 'target-1';
const _profileId = 'profile-1';
const _snapshotId = 'snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _replacementSessionId = 'session-2';
const _threadId = 'thread-1';
const _goalId = 'goal-1';
const _sceneId = 'scene-1';
const _roleId = 'role-1';
const _optionId = 'option-1';
const _background = 'Built reliable Go services for three years.';
