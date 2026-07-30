import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/preparation/job_preparation_client.dart';
import 'package:speakup/features/preparation/job_preparation_controller.dart';
import 'package:speakup/features/preparation/job_preparation_draft_store.dart';
import 'package:speakup/features/preparation/job_preparation_models.dart';
import 'package:speakup/features/preparation/job_preparation_wizard.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

void main() {
  testWidgets('restorable draft actions fit the shared button theme', (
    tester,
  ) async {
    final store = MemoryJobPreparationDraftStore();
    final first = _controller(_WizardClient(), draftStore: store);
    await first.activateAccount('user-1');
    first.updateInput(_input);
    await tester.runAsync(() async {
      while (await store.read('user-1') == null) {
        await Future<void>.delayed(const Duration(milliseconds: 1));
      }
    });
    first.dispose();

    final restored = _controller(_WizardClient(), draftStore: store);
    addTearDown(restored.dispose);
    await restored.activateAccount('user-1');

    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: JobPreparationWizard(controller: restored),
      ),
    );
    await tester.pump();

    expect(restored.hasRestorableDraft, isTrue);
    await _scrollTo(
      tester,
      target: const Key('job-draft-card'),
      scrollable: const Key('job-wizard-input-step'),
    );
    expect(find.byKey(const Key('job-draft-card')), findsOneWidget);
    expect(find.byKey(const Key('resume-job-draft-button')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('starts with JD-first input and labels quick start honestly', (
    tester,
  ) async {
    final controller = JobPreparationController(
      client: _WizardClient(),
      threadIdProvider: () => 'thread-1',
      matterActivator:
          ({
            required threadId,
            required candidate,
            required clientOperationId,
          }) async =>
              AgentPracticeContext(threadId: threadId, matterId: 'matter-1'),
      voiceActivator:
          ({
            required context,
            required bootstrap,
            required clientOperationId,
          }) async {},
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: JobPreparationWizard(controller: controller)),
    );

    expect(find.byKey(const Key('job-wizard-input-step')), findsOneWidget);
    expect(find.byKey(const Key('job-description-field')), findsOneWidget);
    expect(find.byKey(const Key('job-title-field')), findsNothing);

    await tester.tap(find.text('岗位快速开始'));
    await tester.pump();

    expect(find.byKey(const Key('job-title-field')), findsOneWidget);
    expect(find.text('快速开始不会基于真实 JD，只会提供通用岗位建议。'), findsOneWidget);
  });

  testWidgets('runs confirmation and preview before one explicit start', (
    tester,
  ) async {
    final pendingSession = Completer<PreparationPracticeBootstrap>();
    final client = _WizardClient(sessionCompleter: pendingSession);
    var voiceCalls = 0;
    var opened = 0;
    final controller = _controller(
      client,
      voiceActivator:
          ({
            required context,
            required bootstrap,
            required clientOperationId,
          }) async {
            voiceCalls++;
          },
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: JobPreparationWizard(
          controller: controller,
          onPracticeStarted: () => opened++,
        ),
      ),
    );
    await tester.enterText(
      find.byKey(const Key('job-description-field')),
      'Build reliable Go APIs and explain system design trade-offs.',
    );
    await tester.enterText(
      find.byKey(const Key('job-background-field')),
      _background,
    );
    await _scrollTo(
      tester,
      target: const Key('analyze-job-button'),
      scrollable: const Key('job-wizard-input-step'),
    );
    await tester.tap(find.byKey(const Key('analyze-job-button')));
    await tester.pump();

    expect(
      find.byKey(const Key('job-wizard-confirmation-step')),
      findsOneWidget,
    );
    await _scrollTo(
      tester,
      target: const Key('confirm-job-analysis-button'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );
    await tester.tap(find.byKey(const Key('confirm-job-analysis-button')));
    await tester.pump();
    expect(find.byKey(const Key('job-wizard-setup-step')), findsOneWidget);

    await _scrollTo(
      tester,
      target: const Key('create-plan-preview-button'),
      scrollable: const Key('job-wizard-setup-step'),
    );
    await tester.tap(find.byKey(const Key('create-plan-preview-button')));
    await tester.pump();
    expect(find.byKey(const Key('job-wizard-preview-step')), findsOneWidget);
    expect(client.sessionCalls, 0);

    await _scrollTo(
      tester,
      target: const Key('start-job-practice-button'),
      scrollable: const Key('job-wizard-preview-step'),
    );
    await tester.tap(find.byKey(const Key('start-job-practice-button')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('start-job-practice-button')));
    expect(client.sessionCalls, 1);
    pendingSession.complete(_bootstrap);
    await tester.pump();
    await tester.pump();

    expect(voiceCalls, 1);
    expect(opened, 1);
  });

  testWidgets('voice retry reuses committed Session', (tester) async {
    final client = _WizardClient();
    var voiceCalls = 0;
    final controller = _controller(
      client,
      voiceActivator:
          ({
            required context,
            required bootstrap,
            required clientOperationId,
          }) async {
            voiceCalls++;
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

    await tester.pumpWidget(
      MaterialApp(home: JobPreparationWizard(controller: controller)),
    );
    await _scrollTo(
      tester,
      target: const Key('start-job-practice-button'),
      scrollable: const Key('job-wizard-preview-step'),
    );
    await tester.tap(find.byKey(const Key('start-job-practice-button')));
    await tester.pumpAndSettle();
    await _scrollTo(
      tester,
      target: const Key('start-job-practice-button'),
      scrollable: const Key('job-wizard-preview-step'),
    );
    expect(find.text('重新连接语音练习'), findsOneWidget);
    expect(find.byKey(const Key('job-preview-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('start-job-practice-button')));
    await tester.pump();

    expect(client.sessionCalls, 1);
    expect(voiceCalls, 2);
  });

  testWidgets('narrow screen, keyboard and 2x text remain usable', (
    tester,
  ) async {
    final controller = _controller(_WizardClient());
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MediaQuery(
        data: const MediaQueryData(
          size: Size(320, 640),
          textScaler: TextScaler.linear(2),
        ),
        child: MaterialApp(home: JobPreparationWizard(controller: controller)),
      ),
    );
    await tester.tap(find.text('岗位快速开始'));
    await tester.pump();
    await tester.tap(find.byKey(const Key('job-title-field')));
    await tester.pump();

    expect(tester.takeException(), isNull);
    final semantics = tester.getSemantics(
      find.byKey(const Key('quick-start-notice')),
    );
    expect(semantics.label, contains('快速开始不会基于真实 JD'));
    await _scrollTo(
      tester,
      target: const Key('analyze-job-button'),
      scrollable: const Key('job-wizard-input-step'),
    );
    expect(find.byKey(const Key('analyze-job-button')), findsOneWidget);
  });
}

Future<void> _scrollTo(
  WidgetTester tester, {
  required Key target,
  required Key scrollable,
}) async {
  await tester.scrollUntilVisible(
    find.byKey(target),
    260,
    scrollable: find
        .descendant(
          of: find.byKey(scrollable),
          matching: find.byType(Scrollable),
        )
        .first,
    maxScrolls: 12,
  );
  await tester.pump();
}

JobPreparationController _controller(
  _WizardClient client, {
  JobPreparationDraftStore? draftStore,
  JobPreparationVoiceActivator? voiceActivator,
}) {
  var sequence = 0;
  return JobPreparationController(
    client: client,
    draftStore: draftStore,
    threadIdProvider: () => _threadId,
    matterActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async =>
            AgentPracticeContext(threadId: threadId, matterId: _matterId),
    voiceActivator:
        voiceActivator ??
        ({
          required context,
          required bootstrap,
          required clientOperationId,
        }) async {},
    idFactory: (scope) => '$scope-widget-${++sequence}',
    analysisPollInterval: Duration.zero,
  );
}

final class _WizardClient implements JobPreparationClient {
  _WizardClient({this.sessionCompleter});

  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  JobTarget? _target;
  JobPreparationSnapshot? _snapshotValue;
  JobPracticePlanPreview? _planValue;
  int sessionCalls = 0;

  @override
  Future<JobTarget> analyzeJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  }) async {
    _target = _targetFor(
      JobTargetStage.awaitingConfirmation,
      input: _target?.input ?? _input,
    );
    return _target!;
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<JobTarget> confirmJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required int expectedAnalysisVersion,
    required JobTargetCandidate candidate,
    required String idempotencyKey,
  }) async {
    _target = _targetFor(
      JobTargetStage.confirmed,
      input: _target?.input ?? _input,
      confirmedCandidate: candidate,
    );
    return _target!;
  }

  @override
  Future<JobPreparationProfile> createProfileForJobTarget({
    required String backgroundSummary,
    required String jobTargetId,
    required int jobTargetConfirmationVersion,
    required String idempotencyKey,
  }) async => _profile;

  @override
  Future<JobPracticePlanPreview> createJobPracticePlan({
    required AgentPracticeContext context,
    required String preparationSnapshotId,
    required String idempotencyKey,
  }) async {
    final snapshot = _snapshotValue ?? _snapshot;
    _planValue = _planFrom(snapshot: snapshot, context: context, revision: 1);
    return _planValue!;
  }

  @override
  Future<PreparationPracticeBootstrap> createJobPracticeSession({
    required JobPracticePlanPreview plan,
    required String idempotencyKey,
  }) async {
    sessionCalls++;
    return sessionCompleter?.future ?? _bootstrap;
  }

  @override
  Future<JobPreparationSnapshot> createJobPreparationSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    final target = _target ?? _targetFor(JobTargetStage.confirmed);
    final candidate =
        target.confirmation?.candidate ?? _candidateFor(target.input.source);
    _snapshotValue = JobPreparationSnapshot(
      id: _snapshotId,
      sourceProfileId: _profileId,
      sourceVersion: 1,
      sourceJobTargetId: target.id,
      sourceJobTargetConfirmationVersion: 1,
      jobTargetInput: target.input,
      jobTargetCandidate: candidate,
      backgroundSnapshot: target.input.candidateBackground ?? _background,
      createdAt: _now,
    );
    return _snapshotValue!;
  }

  @override
  Future<JobTarget> createJobTarget({
    required JobTargetInput input,
    required String idempotencyKey,
  }) async {
    _target = _targetFor(JobTargetStage.draft, input: input);
    return _target!;
  }

  @override
  Future<JobTarget> discardJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required String idempotencyKey,
  }) async =>
      _targetFor(JobTargetStage.discarded, input: _target?.input ?? _input);

  @override
  Future<JobPracticePlanPreview> getJobPracticePlan(String planId) async =>
      _planValue ?? _plan;

  @override
  Future<JobTarget> getJobTarget(String jobTargetId) async =>
      _target ?? _targetFor(JobTargetStage.awaitingConfirmation);

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
    final current = _planValue ?? _plan;
    _planValue = _planFrom(
      snapshot: current.preparationSnapshot,
      context: current.context,
      revision: expectedPlanRevision + 1,
    );
    return _planValue!;
  }

  @override
  Future<JobTarget> updateJobTarget({
    required String jobTargetId,
    required int expectedInputVersion,
    required JobTargetInput input,
    required String idempotencyKey,
  }) async {
    _target = _targetFor(JobTargetStage.draft, input: input);
    return _target!;
  }
}

JobTarget _targetFor(
  JobTargetStage stage, {
  JobTargetInput input = _input,
  JobTargetCandidate? confirmedCandidate,
}) {
  final analysis = switch (stage) {
    JobTargetStage.parsing => JobTargetAnalysis(
      inputVersion: 1,
      analysisVersion: 1,
      attempt: 1,
      status: JobTargetAnalysisStatus.running,
      startedAt: _now,
    ),
    JobTargetStage.awaitingConfirmation ||
    JobTargetStage.confirmed => JobTargetAnalysis(
      inputVersion: 1,
      analysisVersion: 1,
      attempt: 1,
      status: JobTargetAnalysisStatus.succeeded,
      candidate: _candidateFor(input.source),
      startedAt: _now,
      finishedAt: _now,
    ),
    JobTargetStage.analysisFailed => JobTargetAnalysis(
      inputVersion: 1,
      analysisVersion: 1,
      attempt: 1,
      status: JobTargetAnalysisStatus.failed,
      stableErrorCategory: 'provider_unavailable',
      startedAt: _now,
      finishedAt: _now,
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
            candidate: confirmedCandidate ?? _candidateFor(input.source),
            confirmedAt: _now,
          )
        : null,
    createdAt: _now,
    updatedAt: _now,
  );
}

JobTargetCandidate _candidateFor(JobTargetSource source) => JobTargetCandidate(
  source: source,
  generalAdviceOnly: source == JobTargetSource.quickStart,
  jobTitle: 'Backend engineer',
  seniority: 'Senior',
  responsibilities: const ['Build reliable APIs'],
  coreSkills: const ['Go services'],
  communicationFocus: const ['Explain trade-offs'],
  practiceGoals: const ['System design'],
  scopeNotice: source == JobTargetSource.quickStart
      ? 'Not based on a real JD.'
      : 'Based on the supplied JD.',
  catalogRecommendation: const JobTargetCatalogRecommendation(
    scenarioDefinitionId: _scenarioId,
    scenarioDefinitionVersion: 1,
    selectedRoleIds: [_roleId],
    practiceOptionId: _optionId,
    practiceOptionVersion: 1,
  ),
);

JobPracticePlanPreview _planWithRevision(int revision) => _planFrom(
  snapshot: _snapshot,
  context: const AgentPracticeContext(threadId: _threadId, matterId: _matterId),
  revision: revision,
);

JobPracticePlanPreview _planFrom({
  required JobPreparationSnapshot snapshot,
  required AgentPracticeContext context,
  required int revision,
}) => JobPracticePlanPreview(
  id: _planId,
  userId: _userId,
  context: context,
  preparationProfileId: _profileId,
  preparationSnapshot: snapshot,
  catalog: _catalog,
  sessionPolicy: _policy,
  practiceFocuses: _objectives,
  revision: revision,
  status: 'ready',
  createdAt: _now,
  updatedAt: _now,
);

final _now = DateTime.utc(2026, 7, 26, 12);

const _input = JobTargetInput(
  source: JobTargetSource.jobDescription,
  jobDescription: 'Build reliable APIs and explain trade-offs.',
  candidateBackground: _background,
);

final _profile = JobPreparationProfile(
  id: _profileId,
  userId: _userId,
  backgroundSummary: _background,
  jobTargetId: _targetId,
  jobTargetConfirmationVersion: 1,
  version: 1,
  updatedAt: _now,
);

final _snapshot = JobPreparationSnapshot(
  id: _snapshotId,
  sourceProfileId: _profileId,
  sourceVersion: 1,
  sourceJobTargetId: _targetId,
  sourceJobTargetConfirmationVersion: 1,
  jobTargetInput: _input,
  jobTargetCandidate: _candidateFor(JobTargetSource.jobDescription),
  backgroundSnapshot: _background,
  createdAt: _now,
);

const _scenario = PreparationScenario(
  id: _scenarioId,
  type: 'INTERVIEW',
  model: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  name: 'Technical interview',
  summary: 'Discuss one backend project.',
  version: 1,
  status: 'active',
);

const _config = PreparationScenarioConfig(
  id: 'config-1',
  scenarioId: _scenarioId,
  type: 'INTERVIEW',
  model: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  version: 1,
  jobTitle: 'Backend engineer',
  jobDescription: 'Explain trade-offs.',
  prompt: _prompt,
);

const _prompt = PreparationScenarioPrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: ['system_design'],
  turnBlueprints: ['Ask for a project overview.'],
  suggestedDurationSeconds: 900,
);

const _role = PreparationRole(
  id: _roleId,
  scenarioId: _scenarioId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical interviewer',
  responsibilities: 'Probe technical depth.',
  style: 'Precise',
  focusAreas: ['system_design'],
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

const _objectives = [
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

final _plan = _planWithRevision(1);

final _bootstrap = PreparationPracticeBootstrap(
  session: PreparationPracticeSession(
    id: _sessionId,
    planId: _planId,
    scenarioType: 'INTERVIEW',
    scenarioModel: 'PROJECT_EXPERIENCE_DEEP_DIVE',
    snapshotId: _snapshotId,
    status: 'starting',
    version: 1,
    createdAt: _now,
  ),
  preparationSnapshotId: _snapshotId,
  maxEffectiveTurns: 5,
);

const _background = 'Built reliable Go services for three years.';
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
