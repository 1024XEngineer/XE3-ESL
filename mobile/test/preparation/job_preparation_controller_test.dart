import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/preparation/job_preparation_client.dart';
import 'package:speakup/features/preparation/job_preparation_controller.dart';
import 'package:speakup/features/preparation/job_preparation_draft_store.dart';
import 'package:speakup/features/preparation/job_preparation_models.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

void main() {
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

JobPreparationController _controller(
  _FakeJobPreparationClient client, {
  JobPreparationDraftStore? draftStore,
  JobPreparationVoiceActivator? voiceActivator,
}) {
  return JobPreparationController(
    client: client,
    draftStore: draftStore,
    threadIdProvider: () => _threadId,
    matterActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async => _context,
    voiceActivator:
        voiceActivator ??
        ({
          required context,
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
    this.failFirstCreate = false,
    this.failFirstAnalysis = false,
    this.analyzeAsParsing = false,
    JobTarget? restoredTarget,
  }) : restoredTarget =
           restoredTarget ?? _target(JobTargetStage.awaitingConfirmation);

  final Completer<JobTarget>? analysisCompleter;
  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  final bool failFirstCreate;
  final bool failFirstAnalysis;
  final bool analyzeAsParsing;
  final JobTarget restoredTarget;

  final List<String> calls = <String>[];
  final List<String> createTargetKeys = <String>[];
  final List<String> analysisKeys = <String>[];
  final List<String> sessionKeys = <String>[];
  int clearCount = 0;
  int _createAttempts = 0;
  int _analysisAttempts = 0;

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
  Future<JobPreparationProfile> createProfileForJobTarget({
    required String backgroundSummary,
    required String jobTargetId,
    required int jobTargetConfirmationVersion,
    required String idempotencyKey,
  }) async {
    calls.add('profile');
    return _profile;
  }

  @override
  Future<JobPreparationSnapshot> createJobPreparationSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    calls.add('snapshot');
    return _snapshot;
  }

  @override
  Future<JobPracticePlanPreview> createJobPracticePlan({
    required AgentPracticeContext context,
    required String preparationSnapshotId,
    required String idempotencyKey,
  }) async {
    calls.add('plan');
    return _plan;
  }

  @override
  Future<JobPracticePlanPreview> getJobPracticePlan(String planId) async {
    calls.add('get-plan');
    return _plan;
  }

  @override
  Future<JobPracticePlanPreview> reviseJobPracticePlan({
    required String planId,
    required int expectedPlanRevision,
    required String roleDefinitionId,
    required String practiceOptionId,
    required int practiceOptionVersion,
    required int maxEffectiveTurns,
    required String idempotencyKey,
  }) async {
    calls.add('revise-plan');
    return _planWithRevision(expectedPlanRevision + 1);
  }

  @override
  Future<PreparationPracticeBootstrap> createJobPracticeSession({
    required JobPracticePlanPreview plan,
    required String idempotencyKey,
  }) async {
    calls.add('session');
    sessionKeys.add(idempotencyKey);
    final pending = sessionCompleter;
    if (pending != null) {
      return pending.future;
    }
    return _bootstrap;
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

JobPracticePlanPreview _planWithRevision(int revision) {
  return JobPracticePlanPreview(
    id: _planId,
    userId: _userId,
    context: _context,
    preparationProfileId: _profileId,
    preparationSnapshot: _snapshot,
    catalog: _catalog,
    sessionPolicy: _policy,
    practiceFocuses: _objectives,
    revision: revision,
    status: 'ready',
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
    scenarioDefinitionId: _scenarioId,
    scenarioDefinitionVersion: 1,
    selectedRoleIds: <String>[_roleId],
    practiceOptionId: _optionId,
    practiceOptionVersion: 1,
  ),
);

final JobPreparationProfile _profile = JobPreparationProfile(
  id: _profileId,
  userId: _userId,
  backgroundSummary: _background,
  jobTargetId: _targetId,
  jobTargetConfirmationVersion: 1,
  version: 1,
  updatedAt: _now,
);

final JobPreparationSnapshot _snapshot = JobPreparationSnapshot(
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

const _scenario = PreparationScenario(
  id: _scenarioId,
  type: 'INTERVIEW',
  name: 'Technical interview',
  version: 1,
  status: 'active',
);

const _config = PreparationScenarioConfig(
  id: 'config-1',
  scenarioId: _scenarioId,
  type: 'INTERVIEW',
  version: 1,
  jobTitle: 'Backend engineer',
  jobDescription: 'Explain trade-offs.',
  focusAreas: <String>['system_design'],
);

const _role = PreparationRole(
  id: _roleId,
  scenarioId: _scenarioId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical interviewer',
  responsibilities: 'Probe technical depth.',
  style: 'Precise',
  focusAreas: <String>['system_design'],
  version: 1,
);

const _option = PreparationOption(
  id: _optionId,
  scenarioId: _scenarioId,
  type: PreparationOptionType.focus,
  displayName: 'System design focus',
  version: 1,
  roleId: _roleId,
);

const _catalog = JobPlanCatalog(
  scenario: _scenario,
  config: _config,
  selectedRole: _role,
  practiceOption: _option,
);

const _objectives = <JobPracticeObjective>[
  JobPracticeObjective(
    id: 'system_design',
    description: 'Explain one design trade-off.',
  ),
];

const _policy = JobSessionPolicy(
  suggestedDurationSeconds: 720,
  minEffectiveTurns: 2,
  maxEffectiveTurns: 5,
  coverageCheckpointTurn: 3,
  maxFollowUpsPerQuestion: 2,
  targetObjectives: _objectives,
  earlyCompletionRule: 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
);

final JobPracticePlanPreview _plan = _planWithRevision(1);

final PreparationPracticeBootstrap _bootstrap = PreparationPracticeBootstrap(
  session: PreparationPracticeSession(
    id: _sessionId,
    planId: _planId,
    scenarioType: 'INTERVIEW',
    snapshotId: 'session-snapshot-1',
    status: 'starting',
    version: 1,
    createdAt: _now,
  ),
  preparationSnapshotId: _snapshotId,
  maxEffectiveTurns: 5,
);

const _context = AgentPracticeContext(threadId: _threadId, matterId: _matterId);

const _userId = 'user-1';
const _targetId = 'target-1';
const _profileId = 'profile-1';
const _snapshotId = 'snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _threadId = 'thread-1';
const _matterId = 'matter-1';
const _scenarioId = 'scenario-1';
const _roleId = 'role-1';
const _optionId = 'option-1';
const _background = 'Built reliable Go services for three years.';
