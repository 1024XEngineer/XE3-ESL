import '../../support/scene_fixtures.dart';
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_draft_store.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/interview/job_preparation_wizard.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/resume/resume.dart';

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

  testWidgets('keeps one job input for either title or JD', (tester) async {
    final controller = JobPreparationController(
      client: _WizardClient(),
      threadIdProvider: () => 'thread-1',
      goalActivator:
          ({
            required threadId,
            required candidate,
            required clientOperationId,
          }) async =>
              AgentPracticeContext(threadId: threadId, goalId: 'goal-1'),
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {},
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: JobPreparationWizard(controller: controller)),
    );

    expect(find.byKey(const Key('job-wizard-input-step')), findsOneWidget);
    expect(find.text('第 1/3 步 · 岗位信息'), findsOneWidget);
    expect(find.byKey(const Key('job-input-field')), findsOneWidget);
    expect(find.byKey(const Key('job-source-selector')), findsNothing);
    expect(find.byKey(const Key('job-description-field')), findsNothing);
    expect(find.byKey(const Key('job-title-field')), findsNothing);
    expect(find.byKey(const Key('job-company-field')), findsNothing);
    expect(find.byKey(const Key('job-background-field')), findsNothing);
    expect(find.byKey(const Key('job-goal-field')), findsNothing);

    await tester.enterText(
      find.byKey(const Key('job-input-field')),
      'Backend engineer',
    );
    expect(controller.input.source, JobTargetSource.quickStart);
    expect(controller.input.jobTitle, 'Backend engineer');
    await _scrollTo(
      tester,
      target: const Key('analyze-job-button'),
      scrollable: const Key('job-wizard-input-step'),
    );
    await tester.tap(find.byKey(const Key('analyze-job-button')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('job-wizard-confirmation-step')),
      findsOneWidget,
    );
    expect(find.text('第 2/3 步 · AI 预生成'), findsOneWidget);
  });

  testWidgets('automatically treats structured text as a JD', (tester) async {
    final controller = _controller(_WizardClient());
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: JobPreparationWizard(controller: controller)),
    );
    await tester.enterText(
      find.byKey(const Key('job-input-field')),
      '岗位职责：负责 Go 服务开发\n任职要求：熟悉 PostgreSQL',
    );

    expect(controller.input.source, JobTargetSource.jobDescription);
    expect(controller.input.jobTitle, isNull);
    expect(controller.input.jobDescription, contains('任职要求'));
  });

  testWidgets('pre-generated step supports focus text and quick tags', (
    tester,
  ) async {
    final controller = _controller(_WizardClient());
    addTearDown(controller.dispose);
    controller.updateInput(_input);
    await controller.analyze();

    await tester.pumpWidget(
      MaterialApp(home: JobPreparationWizard(controller: controller)),
    );
    await _scrollTo(
      tester,
      target: const Key('job-practice-focus-field'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );

    expect(find.byKey(const Key('job-practice-focus-field')), findsOneWidget);
    expect(
      find.byKey(const Key('job-practice-focus-suggestions')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('job-practice-focus-技术深挖')));
    await tester.pump();

    expect(controller.candidate?.practiceGoals, contains('技术深挖'));
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
            required scene,
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
      find.byKey(const Key('job-input-field')),
      'Build reliable Go APIs and explain system design trade-offs.',
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
    expect(find.byKey(const Key('job-wizard-preview-step')), findsOneWidget);
    expect(find.byKey(const Key('job-wizard-setup-step')), findsNothing);
    expect(find.text('第 3/3 步 · 确认面试'), findsOneWidget);
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

  testWidgets('confirmation selects an existing READY resume or no resume', (
    tester,
  ) async {
    final controller = _controller(_WizardClient());
    final resumeController = ResumeController(
      client: _WizardResumeClient(
        items: <ResumeItem>[_wizardResume('Backend resume')],
      ),
      filePicker: const _WizardResumePicker(null),
      urlOpener: const _WizardResumeOpener(),
    );
    addTearDown(controller.dispose);
    addTearDown(resumeController.dispose);
    controller.updateInput(_input);
    await controller.analyze();
    await resumeController.load();

    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: JobPreparationWizard(
          controller: controller,
          resumeController: resumeController,
        ),
      ),
    );
    await _scrollTo(
      tester,
      target: const Key('job-resume-source-card'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );

    expect(find.text('简历（可选）'), findsOneWidget);
    expect(find.text('不使用简历'), findsOneWidget);
    await tester.tap(find.text('不使用简历'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Backend resume').last);
    await tester.pumpAndSettle();

    expect(controller.resumeSelection?.title, 'Backend resume');
    expect(controller.resumeSelection?.temporary, isFalse);

    await tester.tap(find.text('Backend resume'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('不使用简历').last);
    await tester.pumpAndSettle();
    expect(controller.resumeSelection, isNull);
  });

  testWidgets('temporary parse failure keeps job context and can be skipped', (
    tester,
  ) async {
    final controller = _controller(_WizardClient());
    final resumeController = ResumeController(
      client: _WizardResumeClient(
        temporary: _wizardResume(
          'Temporary resume',
          status: ResumeParseStatus.failed,
          revision: null,
        ),
      ),
      filePicker: _WizardResumePicker(
        ResumePdfFile(name: 'temporary.pdf', bytes: '%PDF-temp'.codeUnits),
      ),
      urlOpener: const _WizardResumeOpener(),
    );
    addTearDown(controller.dispose);
    addTearDown(resumeController.dispose);
    controller.updateInput(_input);
    await controller.analyze();

    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: JobPreparationWizard(
          controller: controller,
          resumeController: resumeController,
        ),
      ),
    );
    await _scrollTo(
      tester,
      target: const Key('temporary-resume-upload-button'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );
    await tester.tap(find.byKey(const Key('temporary-resume-upload-button')));
    await tester.pumpAndSettle();

    expect(find.text('临时简历解析失败，可以重试或重新上传。'), findsOneWidget);
    expect(find.text('重试解析'), findsOneWidget);
    expect(controller.candidate?.jobTitle, 'Backend engineer');
    await _scrollTo(
      tester,
      target: const Key('confirm-job-analysis-button'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );
    await tester.tap(find.byKey(const Key('confirm-job-analysis-button')));
    await tester.pumpAndSettle();

    expect(controller.resumeSelection, isNull);
    expect(find.byKey(const Key('job-wizard-preview-step')), findsOneWidget);
  });

  testWidgets('temporary upload becomes the selected parsed resume', (
    tester,
  ) async {
    final controller = _controller(_WizardClient());
    final resumeController = ResumeController(
      client: _WizardResumeClient(
        temporaryCreated: _wizardResume(
          'Temporary resume',
          status: ResumeParseStatus.queued,
          revision: null,
        ),
        temporaryDetail: _wizardResume('Temporary resume'),
      ),
      filePicker: _WizardResumePicker(
        ResumePdfFile(name: 'temporary.pdf', bytes: '%PDF-temp'.codeUnits),
      ),
      urlOpener: const _WizardResumeOpener(),
    );
    addTearDown(controller.dispose);
    addTearDown(resumeController.dispose);
    controller.updateInput(_input);
    await controller.analyze();

    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: JobPreparationWizard(
          controller: controller,
          resumeController: resumeController,
        ),
      ),
    );
    await _scrollTo(
      tester,
      target: const Key('temporary-resume-upload-button'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );
    await tester.tap(find.byKey(const Key('temporary-resume-upload-button')));
    await tester.pumpAndSettle();

    expect(controller.resumeSelection?.title, 'Temporary resume');
    expect(controller.resumeSelection?.temporary, isTrue);
    expect(find.text('临时简历已解析，可用于本次面试。'), findsOneWidget);
  });

  testWidgets('temporary file is deleted once its snapshot exists', (
    tester,
  ) async {
    final client = _WizardClient(failPlan: true);
    final controller = _controller(client);
    final resumeClient = _WizardResumeClient(
      temporary: _wizardResume('Temporary resume'),
    );
    final resumeController = ResumeController(
      client: resumeClient,
      filePicker: _WizardResumePicker(
        ResumePdfFile(name: 'temporary.pdf', bytes: '%PDF-temp'.codeUnits),
      ),
      urlOpener: const _WizardResumeOpener(),
    );
    addTearDown(controller.dispose);
    addTearDown(resumeController.dispose);
    controller.updateInput(_input);
    await controller.analyze();
    await controller.confirm();
    await resumeController.pickTemporary();
    controller.selectResume(
      JobPreparationResumeSelection(
        resumeId: resumeController.temporaryItem!.id,
        revision: resumeController.temporaryItem!.currentRevision!,
        resourceVersion: resumeController.temporaryItem!.version,
        temporary: true,
        title: resumeController.temporaryItem!.title,
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: JobPreparationWizard(
          controller: controller,
          resumeController: resumeController,
        ),
      ),
    );
    await _scrollTo(
      tester,
      target: const Key('confirm-job-analysis-button'),
      scrollable: const Key('job-wizard-confirmation-step'),
    );
    await tester.tap(find.byKey(const Key('confirm-job-analysis-button')));
    await tester.pumpAndSettle();

    expect(client.snapshotCalls, 1);
    expect(resumeClient.deleteTemporaryCalls, 1);
    expect(resumeController.temporaryItem, isNull);
  });

  testWidgets('voice retry reuses committed Session', (tester) async {
    final client = _WizardClient();
    var voiceCalls = 0;
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
    await tester.tap(find.byKey(const Key('job-input-field')));
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.byKey(const Key('job-company-field')), findsNothing);
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
    goalActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async => AgentPracticeContext(threadId: threadId, goalId: _goalId),
    voiceActivator:
        voiceActivator ??
        ({
          required context,
          required scene,
          required bootstrap,
          required clientOperationId,
        }) async {},
    idFactory: (scope) => '$scope-widget-${++sequence}',
    analysisPollInterval: Duration.zero,
  );
}

final class _WizardClient implements JobPreparationClient {
  _WizardClient({this.sessionCompleter, this.failPlan = false});

  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  final bool failPlan;
  JobTarget? _target;
  PreparationSnapshot? _snapshotValue;
  PracticePlan? _planValue;
  int sessionCalls = 0;
  int snapshotCalls = 0;

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
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async => _profile;

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    if (failPlan) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: JobPreparationOperationStage.plan,
        retryable: true,
      );
    }
    final snapshot = _snapshotValue ?? _snapshot;
    _planValue = _planFrom(
      snapshot: snapshot,
      context: AgentPracticeContext(
        threadId: input.sourceThreadId!,
        goalId: input.goalId!,
      ),
      revision: 1,
    );
    return _planValue!;
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    sessionCalls++;
    return sessionCompleter?.future ?? _bootstrap;
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    snapshotCalls++;
    final target = _target ?? _targetFor(JobTargetStage.confirmed);
    final candidate =
        target.confirmation?.candidate ?? _candidateFor(target.input.source);
    _snapshotValue = PreparationSnapshot(
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
  Future<PracticePlan> getPlan(String planId) async => _planValue ?? _plan;

  @override
  Future<JobTarget> getJobTarget(String jobTargetId) async =>
      _target ?? _targetFor(JobTargetStage.awaitingConfirmation);

  @override
  Future<PracticePlan> revisePlan({
    required String planId,
    required RevisePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    final current = _planValue ?? _plan;
    _planValue = _planFrom(
      snapshot: current.preparationSnapshot,
      context: current.agentContext!,
      revision: input.expectedPlanRevision + 1,
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

ResumeItem _wizardResume(
  String title, {
  ResumeParseStatus status = ResumeParseStatus.ready,
  int? revision = 1,
}) => ResumeItem(
  id: '70000000-0000-4000-8000-000000000007',
  title: title,
  originalFilename: 'resume.pdf',
  sizeBytes: 1024,
  parseStatus: status,
  currentRevision: revision,
  version: 2,
  updatedAt: DateTime.utc(2026, 8, 6),
);

final class _WizardResumePicker implements ResumeFilePicker {
  const _WizardResumePicker(this.file);

  final ResumePdfFile? file;

  @override
  Future<ResumePdfFile?> pickPdf() async => file;
}

final class _WizardResumeOpener implements ResumeUrlOpener {
  const _WizardResumeOpener();

  @override
  Future<bool> open(Uri url) async => true;
}

final class _WizardResumeClient implements ResumeClient {
  _WizardResumeClient({
    this.items = const <ResumeItem>[],
    this.temporary,
    this.temporaryCreated,
    this.temporaryDetail,
  });

  final List<ResumeItem> items;
  final ResumeItem? temporary;
  final ResumeItem? temporaryCreated;
  final ResumeItem? temporaryDetail;
  int deleteTemporaryCalls = 0;

  @override
  Future<List<ResumeItem>> list() async => items;

  @override
  Future<ResumeItem> create({
    required String title,
    required ResumePdfFile file,
  }) async => throw UnimplementedError();

  @override
  Future<ResumeItem> createTemporary(ResumePdfFile file) async =>
      temporaryCreated ?? temporary!;

  @override
  Future<ResumeDetail> getTemporary(String resumeId) async =>
      ResumeDetail(resume: temporaryDetail ?? temporary!);

  @override
  Future<ResumeItem> retryTemporaryParse(ResumeItem resume) async => resume;

  @override
  Future<void> deleteTemporary(ResumeItem resume) async {
    deleteTemporaryCalls += 1;
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> delete(ResumeItem resume) async => throw UnimplementedError();

  @override
  Future<ResumeDetail> get(String resumeId) async => throw UnimplementedError();

  @override
  Future<Uri> getContentUrl(String resumeId) async =>
      throw UnimplementedError();

  @override
  Future<ResumeItem> rename(ResumeItem resume, String title) async =>
      throw UnimplementedError();

  @override
  Future<ResumeItem> replace(ResumeItem resume, ResumePdfFile file) async =>
      throw UnimplementedError();

  @override
  Future<ResumeItem> retryParse(ResumeItem resume) async =>
      throw UnimplementedError();

  @override
  Future<ResumeDetail> updateContent(
    ResumeDetail detail,
    ResumeContent content,
  ) async => throw UnimplementedError();
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
    sceneId: _sceneId,
    sceneVersion: 1,
    selectedRoleIds: [_roleId],
    practiceOptionId: _optionId,
  ),
);

PracticePlan _planWithRevision(int revision) => _planFrom(
  snapshot: _snapshot,
  context: const AgentPracticeContext(threadId: _threadId, goalId: _goalId),
  revision: revision,
);

PracticePlan _planFrom({
  required PreparationSnapshot snapshot,
  required AgentPracticeContext context,
  required int revision,
}) => PracticePlan(
  id: _planId,
  userId: _userId,
  sourceThreadId: context.threadId,
  goalSnapshot: PreparationGoalSnapshot(
    id: context.goalId,
    title: _scene.name,
    version: 1,
  ),
  preparationSnapshot: snapshot,
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

final _now = DateTime.utc(2026, 7, 26, 12);

const _input = JobTargetInput(
  source: JobTargetSource.jobDescription,
  jobDescription: 'Build reliable APIs and explain trade-offs.',
  candidateBackground: _background,
);

final _profile = PreparationProfile(
  id: _profileId,
  userId: _userId,
  backgroundSummary: _background,
  jobTargetId: _targetId,
  jobTargetConfirmationVersion: 1,
  version: 1,
  updatedAt: _now,
);

final _snapshot = PreparationSnapshot(
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

final _scene = testScene(
  id: _sceneId,
  experience: PracticeExperience.interview,
  category: SceneCategory.interviewProfessional,
  name: 'Technical interview',
  version: 1,
  prompt: _prompt,
  roles: [_role],
  practiceOptions: [_option],
);

const _prompt = ScenePrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: ['system_design'],
  turnBlueprints: ['Ask for a project overview.'],
);

final _role = testRole(
  id: _roleId,
  sceneId: _sceneId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical interviewer',
  responsibilities: 'Probe technical depth.',
  style: 'Precise',
  practiceObjectiveIds: ['system_design'],
);

final _option = testPracticeOption(
  id: _optionId,
  sceneId: _sceneId,
  mode: PracticeMode.focus,
  displayName: 'System design focus',
  roleId: _roleId,
);

const _objectives = [
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
  retryAllowed: false,
  questionTranslationAllowed: true,
  questionTipsAllowed: true,
  avatarAllowed: true,
  speechFeedbackAllowed: true,
);

final _plan = _planWithRevision(1);

final _bootstrap = PreparationPracticeBootstrap(
  session: PreparationPracticeSession(
    id: _sessionId,
    planId: _planId,
    practiceExperience: PracticeExperience.interview,
    sceneCategory: SceneCategory.interviewProfessional,
    practiceMode: PracticeMode.focus,
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
const _goalId = 'goal-1';
const _sceneId = 'scene-1';
const _roleId = 'role-1';
const _optionId = 'option-1';
