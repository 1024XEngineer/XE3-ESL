import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_draft_store.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';

typedef JobPreparationIdFactory = String Function(String scope);
typedef JobPreparationThreadIdProvider = String? Function();
typedef JobPreparationGoalActivator =
    Future<AgentPracticeContext> Function({
      required String threadId,
      required JobTargetCandidate candidate,
      required String clientOperationId,
    });
typedef JobPreparationVoiceActivator =
    Future<void> Function({
      required AgentPracticeContext context,
      required SceneDefinition scene,
      required PreparationPracticeBootstrap bootstrap,
      required String clientOperationId,
    });

enum JobPreparationStep { input, confirmation, setup, preview }

final class JobPreparationController extends ChangeNotifier {
  JobPreparationController({
    required this.client,
    required this.threadIdProvider,
    required this.goalActivator,
    required this.voiceActivator,
    JobPreparationDraftStore? draftStore,
    JobPreparationIdFactory? idFactory,
    this.workspaceController,
    this.analysisPollInterval = const Duration(seconds: 1),
    this.maxAnalysisPollAttempts = 75,
  }) : draftStore = draftStore ?? const NullJobPreparationDraftStore(),
       _idFactory = idFactory ?? _secureJobPreparationId {
    if (analysisPollInterval.isNegative || maxAnalysisPollAttempts < 1) {
      throw ArgumentError('Invalid JobTarget polling configuration.');
    }
    workspaceController?.addListener(_handleWorkspaceState);
  }

  final JobPreparationClient client;
  final JobPreparationDraftStore draftStore;
  final JobPreparationThreadIdProvider threadIdProvider;
  final JobPreparationGoalActivator goalActivator;
  final JobPreparationVoiceActivator voiceActivator;
  final PracticeWorkspaceController? workspaceController;
  final JobPreparationIdFactory _idFactory;
  final Duration analysisPollInterval;
  final int maxAnalysisPollAttempts;

  JobTargetInput _input = const JobTargetInput(
    source: JobTargetSource.jobDescription,
  );
  JobTarget? _target;
  JobTargetCandidate? _candidate;
  JobPreparationResumeSelection? _resumeSelection;
  PracticePlan? _plan;
  PreparationPracticeBootstrap? _bootstrap;
  JobPreparationStep _step = JobPreparationStep.input;
  JobPreparationOperationStage? _operationStage;
  String? _errorMessage;
  bool _busy = false;
  bool _initializingDraft = false;
  bool _draftCompleted = false;
  bool _disposed = false;
  int _epoch = 0;
  int _accountGeneration = 0;
  String? _accountId;
  String? _loadedDraftAccountId;
  String? _agentIntentPrefill;
  _StoredJobPreparationDraft? _restorableDraft;
  Future<void> _draftWriteTail = Future<void>.value();

  String? _createTargetKey;
  String? _updateTargetKey;
  String? _analysisKey;
  String? _confirmationKey;
  String? _discardTargetKey;
  String? _goalKey;
  String? _profileKey;
  String? _snapshotKey;
  String? _planKey;
  String? _planRevisionKey;
  String? _sessionKey;
  String? _voiceKey;
  String? _workspaceKey;
  PracticeWorkspaceLease? _workspaceLease;
  bool _workspaceReplaceRequested = false;

  JobTargetInput get input => _input;
  JobTarget? get target => _target;
  JobTargetCandidate? get candidate => _candidate;
  JobPreparationResumeSelection? get resumeSelection => _resumeSelection;
  PracticePlan? get plan => _plan;
  PreparationPracticeBootstrap? get bootstrap => _bootstrap;
  JobPreparationStep get step => _step;
  JobPreparationOperationStage? get operationStage => _operationStage;
  String? get errorMessage =>
      _errorMessage ?? workspaceController?.errorMessage;
  bool get isBusy => _busy || (workspaceController?.isBusy ?? false);
  bool get isInitializingDraft => _initializingDraft;
  bool get hasRestorableDraft => _restorableDraft != null;
  bool get canRetry => _errorMessage != null && !isBusy;
  bool get isQuickStart => _input.source == JobTargetSource.quickStart;
  String? get agentIntentPrefill => _agentIntentPrefill;
  bool get hasResumablePractice => workspaceController?.hasResumable ?? false;
  String? get resumablePracticeTitle => workspaceController?.currentTitle;
  String? get workspaceErrorMessage => workspaceController?.errorMessage;

  Future<bool> resumeCurrentPractice() async {
    final workspace = workspaceController;
    return workspace != null && await workspace.resumeCurrentPractice();
  }

  Future<bool> parkCurrentPractice() async {
    final workspace = workspaceController;
    return workspace == null ||
        workspace.currentLease == null ||
        await workspace.parkCurrentPractice();
  }

  void offerAgentIntent(String? value) {
    final normalized = value?.trim();
    if (_disposed ||
        _target != null ||
        _step != JobPreparationStep.input ||
        normalized == null ||
        normalized.isEmpty ||
        utf8.encode(normalized).length > 64 * 1024) {
      return;
    }
    _agentIntentPrefill = normalized;
    notifyListeners();
  }

  void applyAgentIntentPrefill() {
    final value = _agentIntentPrefill;
    if (_disposed || value == null || _target != null) {
      return;
    }
    _agentIntentPrefill = null;
    updateInput(
      JobTargetInput(
        source: JobTargetSource.jobDescription,
        jobDescription: value,
        company: _input.company,
        seniority: _input.seniority,
        candidateBackground: _input.candidateBackground,
        resumeRef: _input.resumeRef,
        practiceFocus: _input.practiceFocus,
      ),
    );
  }

  void dismissAgentIntentPrefill() {
    if (_disposed || _agentIntentPrefill == null) {
      return;
    }
    _agentIntentPrefill = null;
    notifyListeners();
  }

  Future<void> activateAccount(String accountId) async {
    if (_disposed ||
        !_validResourceId(accountId) ||
        (_accountId == accountId &&
            (_initializingDraft || _loadedDraftAccountId == accountId))) {
      return;
    }
    final generation = ++_accountGeneration;
    _epoch++;
    _accountId = accountId;
    _loadedDraftAccountId = null;
    _restorableDraft = null;
    _agentIntentPrefill = null;
    _resetPresentation();
    _initializingDraft = true;
    notifyListeners();
    try {
      await workspaceController?.activateAccount(accountId);
      if (!_isCurrentAccount(generation, accountId)) {
        return;
      }
      final encoded = await draftStore.read(accountId);
      if (!_isCurrentAccount(generation, accountId) || encoded == null) {
        return;
      }
      final draft = _StoredJobPreparationDraft.tryDecode(encoded);
      if (draft == null) {
        await draftStore.delete(accountId);
        return;
      }
      if (!_isCurrentAccount(generation, accountId)) {
        return;
      }
      _restorableDraft = draft;
    } on Object {
      if (_isCurrentAccount(generation, accountId)) {
        _errorMessage = '无法安全读取本机草稿，你仍可以重新开始。';
      }
    } finally {
      if (_isCurrentAccount(generation, accountId)) {
        _loadedDraftAccountId = accountId;
        _initializingDraft = false;
        notifyListeners();
      }
    }
  }

  Future<bool> resumeDraft() async {
    final accountId = _accountId;
    final draft = _restorableDraft;
    if (_disposed || _busy || accountId == null || draft == null) {
      return false;
    }
    final operationEpoch = ++_epoch;
    _applyStoredDraft(draft);
    _restorableDraft = null;
    _begin(JobPreparationOperationStage.target);
    try {
      final targetId = draft.targetId;
      if (targetId == null) {
        _step = JobPreparationStep.input;
        _errorMessage = null;
        return true;
      }
      final target = await client.getJobTarget(targetId);
      _requireCurrent(operationEpoch, _input);
      if (target.userId != accountId || target.input != _input) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.target,
        );
      }
      _target = target;
      _applyTargetStage(target, storedCandidate: draft.candidate);
      final planId = draft.planId;
      if (target.stage == JobTargetStage.confirmed && planId != null) {
        _operationStage = JobPreparationOperationStage.plan;
        notifyListeners();
        final plan = await client.getPlan(planId);
        _requireCurrent(operationEpoch, _input);
        if (plan.userId != accountId ||
            plan.preparationSnapshot.sourceJobTargetId != target.id ||
            plan.preparationSnapshot.sourceJobTargetConfirmationVersion !=
                target.confirmation?.confirmationVersion) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.invalidResponse,
            stage: JobPreparationOperationStage.plan,
          );
        }
        if (threadIdProvider() == plan.agentContext?.threadId) {
          _plan = plan;
          _step = JobPreparationStep.preview;
        } else {
          _clearPreviewAttempt();
          _step = JobPreparationStep.setup;
        }
      }
      _errorMessage = null;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _operationStage = error.stage ?? _operationStage;
        _errorMessage = _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法恢复草稿，请检查网络后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  Future<bool> discardDraft() async {
    final accountId = _accountId;
    final draft = _restorableDraft;
    if (_disposed || _busy || accountId == null || draft == null) {
      return false;
    }
    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.target);
    try {
      JobTarget? target;
      if (draft.targetId != null) {
        try {
          target = await client.getJobTarget(draft.targetId!);
        } on JobPreparationException catch (error) {
          if (error.kind != JobPreparationFailureKind.notFound) {
            rethrow;
          }
        }
      } else if (draft.createTargetKey != null) {
        target = await client.createJobTarget(
          input: draft.input,
          idempotencyKey: draft.createTargetKey!,
        );
      }
      if (!_isCurrent(operationEpoch)) {
        return false;
      }
      if (target != null &&
          target.userId == accountId &&
          target.stage != JobTargetStage.confirmed &&
          target.stage != JobTargetStage.discarded) {
        await client.discardJobTarget(
          jobTargetId: target.id,
          expectedInputVersion: target.inputVersion,
          idempotencyKey:
              draft.discardTargetKey ??
              (_discardTargetKey ??= _newId('job-target-discard')),
        );
      }
      await _draftWriteTail;
      await draftStore.delete(accountId);
      if (!_isCurrent(operationEpoch)) {
        return false;
      }
      _restorableDraft = null;
      _resetPresentation();
      _draftCompleted = true;
      _errorMessage = null;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法丢弃草稿，请检查网络后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  void updateInput(JobTargetInput value) {
    if (_disposed || value == _input) {
      return;
    }
    _epoch++;
    _input = value;
    _draftCompleted = false;
    _candidate = null;
    _plan = null;
    _bootstrap = null;
    _step = JobPreparationStep.input;
    _operationStage = null;
    _errorMessage = null;
    _busy = false;
    _updateTargetKey = null;
    _analysisKey = null;
    _confirmationKey = null;
    _clearPreviewAttempt();
    notifyListeners();
    _queueDraftWrite();
  }

  void updateCandidate(JobTargetCandidate value) {
    if (_disposed || _busy || _step != JobPreparationStep.confirmation) {
      return;
    }
    _epoch++;
    _candidate = value;
    _confirmationKey = null;
    _errorMessage = null;
    _operationStage = null;
    notifyListeners();
    _queueDraftWrite();
  }

  void selectResume(JobPreparationResumeSelection? value) {
    if (_disposed || _busy || identical(value, _resumeSelection)) {
      return;
    }
    _epoch++;
    _resumeSelection = value;
    _plan = null;
    _bootstrap = null;
    if (_step == JobPreparationStep.preview) {
      _step = JobPreparationStep.setup;
    }
    _clearPreviewAttempt();
    _errorMessage = null;
    notifyListeners();
    _queueDraftWrite();
  }

  void returnToInput() {
    if (_disposed || _busy) {
      return;
    }
    _step = JobPreparationStep.input;
    _errorMessage = null;
    _operationStage = null;
    notifyListeners();
    _queueDraftWrite();
  }

  void returnToSetup() {
    if (_disposed || _busy || _target?.stage != JobTargetStage.confirmed) {
      return;
    }
    _step = JobPreparationStep.setup;
    _errorMessage = null;
    _operationStage = null;
    notifyListeners();
    _queueDraftWrite();
  }

  Future<bool> analyze() async {
    if (_disposed || _busy) {
      return false;
    }
    final input = _input;
    if (!_validJobTargetInput(input)) {
      _errorMessage = input.source == JobTargetSource.jobDescription
          ? '请粘贴完整职位描述后再分析。'
          : '请填写目标岗位后再开始。';
      _operationStage = JobPreparationOperationStage.target;
      notifyListeners();
      return false;
    }

    final current = _target;
    if (current != null && current.input == input) {
      if (current.stage == JobTargetStage.confirmed &&
          current.confirmation != null) {
        _candidate = current.confirmation!.candidate;
        _step = JobPreparationStep.setup;
        _errorMessage = null;
        notifyListeners();
        _queueDraftWrite();
        return true;
      }
      if (current.stage == JobTargetStage.awaitingConfirmation &&
          current.analysis?.candidate != null) {
        _candidate = current.analysis!.candidate;
        _step = JobPreparationStep.confirmation;
        _errorMessage = null;
        notifyListeners();
        _queueDraftWrite();
        return true;
      }
    }

    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.target);
    try {
      var target = current;
      if (target == null) {
        target = await client.createJobTarget(
          input: input,
          idempotencyKey: _createTargetKey ??= _newId('job-target'),
        );
      } else if (target.input != input) {
        target = await client.updateJobTarget(
          jobTargetId: target.id,
          expectedInputVersion: target.inputVersion,
          input: input,
          idempotencyKey: _updateTargetKey ??= _newId('job-target-update'),
        );
      }
      _requireCurrent(operationEpoch, input);
      if (target.input != input || target.stage == JobTargetStage.discarded) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.target,
        );
      }
      _target = target;

      if (target.stage == JobTargetStage.confirmed &&
          target.confirmation != null) {
        _candidate = target.confirmation!.candidate;
        _step = JobPreparationStep.setup;
        return true;
      }
      if (target.stage == JobTargetStage.awaitingConfirmation &&
          target.analysis?.candidate != null) {
        _candidate = target.analysis!.candidate;
        _step = JobPreparationStep.confirmation;
        return true;
      }
      if (target.stage == JobTargetStage.analysisFailed) {
        _analysisKey = null;
      }

      _operationStage = JobPreparationOperationStage.analysis;
      notifyListeners();
      target = await client.analyzeJobTarget(
        jobTargetId: target.id,
        expectedInputVersion: target.inputVersion,
        idempotencyKey: _analysisKey ??= _newId('job-target-analysis'),
      );
      _requireCurrent(operationEpoch, input);
      target = await _pollAnalysis(
        target,
        operationEpoch: operationEpoch,
        expectedInput: input,
      );
      _requireCurrent(operationEpoch, input);
      _target = target;
      final candidate = target.analysis?.candidate;
      if (target.stage != JobTargetStage.awaitingConfirmation ||
          candidate == null) {
        if (target.stage == JobTargetStage.analysisFailed) {
          _analysisKey = null;
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.server,
            stage: JobPreparationOperationStage.analysis,
            errorCode: 'job_target_analysis_failed',
            retryable: true,
          );
        }
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.analysis,
        );
      }
      _candidate = candidate;
      _step = JobPreparationStep.confirmation;
      _errorMessage = null;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _operationStage = error.stage ?? _operationStage;
        if (error.stage == JobPreparationOperationStage.analysis &&
            error.errorCode == 'job_target_analysis_failed') {
          // A terminal parser failure is durably tied to the previous
          // idempotency intent. A user retry must claim a new parser attempt.
          _analysisKey = null;
        }
        _errorMessage = _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法分析岗位信息，请稍后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  Future<JobTarget> _pollAnalysis(
    JobTarget initial, {
    required int operationEpoch,
    required JobTargetInput expectedInput,
  }) async {
    var target = initial;
    for (
      var attempt = 0;
      target.stage == JobTargetStage.parsing &&
          attempt < maxAnalysisPollAttempts;
      attempt++
    ) {
      if (analysisPollInterval != Duration.zero) {
        await Future<void>.delayed(analysisPollInterval);
      }
      _requireCurrent(operationEpoch, expectedInput);
      target = await client.getJobTarget(target.id);
      _requireCurrent(operationEpoch, expectedInput);
    }
    if (target.stage == JobTargetStage.parsing) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.network,
        stage: JobPreparationOperationStage.analysis,
        retryable: true,
      );
    }
    return target;
  }

  Future<bool> confirm() async {
    if (_disposed || _busy) {
      return false;
    }
    final target = _target;
    final candidate = _candidate;
    final analysis = target?.analysis;
    if (target == null ||
        candidate == null ||
        analysis == null ||
        target.input != _input ||
        target.stage != JobTargetStage.awaitingConfirmation ||
        analysis.status != JobTargetAnalysisStatus.succeeded ||
        candidate.source != _input.source) {
      _errorMessage = '当前分析已失效，请返回并重新分析岗位信息。';
      _operationStage = JobPreparationOperationStage.confirmation;
      notifyListeners();
      return false;
    }
    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.confirmation);
    try {
      final confirmed = await client.confirmJobTarget(
        jobTargetId: target.id,
        expectedInputVersion: target.inputVersion,
        expectedAnalysisVersion: analysis.analysisVersion,
        candidate: candidate,
        idempotencyKey: _confirmationKey ??= _newId('job-target-confirmation'),
      );
      _requireCurrent(operationEpoch, _input);
      if (confirmed.stage != JobTargetStage.confirmed ||
          confirmed.input != _input ||
          confirmed.confirmation?.candidate.source != candidate.source) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.confirmation,
        );
      }
      _target = confirmed;
      _candidate = confirmed.confirmation!.candidate;
      _step = JobPreparationStep.setup;
      _errorMessage = null;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法确认岗位分析，请稍后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  Future<bool> createPreview({bool replaceCurrentPractice = false}) async {
    if (_disposed || _busy) {
      return false;
    }
    final target = _target;
    final confirmation = target?.confirmation;
    final candidate = confirmation?.candidate;
    final background =
        _input.candidateBackground ??
        '未提供个人背景，本次按${candidate?.jobTitle ?? '目标岗位'}通用要求练习。';
    final workspace = workspaceController;
    var threadId = workspace == null ? threadIdProvider() : null;
    if (target == null ||
        confirmation == null ||
        candidate == null ||
        target.input != _input ||
        target.stage != JobTargetStage.confirmed) {
      _errorMessage = '岗位信息尚未确认，请先完成分析与确认。';
      _operationStage = JobPreparationOperationStage.confirmation;
      notifyListeners();
      return false;
    }
    if (workspace == null &&
        (threadId == null || !_validResourceId(threadId))) {
      _errorMessage = 'Agent 对话仍在恢复，请稍后重试。';
      _operationStage = JobPreparationOperationStage.goal;
      notifyListeners();
      return false;
    }

    final operationEpoch = _epoch;
    String? workspaceOperationId;
    var workspaceParked = false;
    _begin(JobPreparationOperationStage.goal);
    try {
      if (workspace != null) {
        if (replaceCurrentPractice) {
          _workspaceReplaceRequested = true;
        }
        final operationId = _workspaceKey ??= _newId('job-target-workspace');
        workspaceOperationId = operationId;
        final previousLease = _workspaceLease;
        final lease = previousLease == null && _workspaceReplaceRequested
            ? await workspace.replaceCurrentPractice(operationId)
            : await workspace.acquireThread(operationId);
        if (lease == null ||
            (previousLease != null &&
                !_sameWorkspaceIdentity(lease, previousLease))) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.conflict,
            stage: JobPreparationOperationStage.goal,
            retryable: true,
          );
        }
        _workspaceLease = lease;
        threadId = lease.practiceThreadId;
        if (threadIdProvider() != threadId) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.invalidResponse,
            stage: JobPreparationOperationStage.goal,
            retryable: true,
          );
        }
      }
      if (threadId == null || !_validResourceId(threadId)) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.goal,
          retryable: true,
        );
      }
      final context = await goalActivator(
        threadId: threadId,
        candidate: candidate,
        clientOperationId: _goalKey ??= _newId('job-target-goal'),
      );
      _requireCurrent(operationEpoch, _input);
      if (context.threadId != threadId || !_validResourceId(context.goalId)) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.goal,
        );
      }

      _operationStage = JobPreparationOperationStage.profile;
      notifyListeners();
      final profile = await client.createProfile(
        input: CreatePreparationProfileInput(
          backgroundSummary: background,
          resumeId: _resumeSelection?.resumeId,
          resumeRevision: _resumeSelection?.revision,
          jobTargetId: target.id,
          jobTargetConfirmationVersion: confirmation.confirmationVersion,
        ),
        idempotencyKey: _profileKey ??= _newId('job-target-profile'),
      );
      _requireCurrent(operationEpoch, _input);
      if (profile.userId != target.userId) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.profile,
        );
      }

      _operationStage = JobPreparationOperationStage.snapshot;
      notifyListeners();
      final snapshot = await client.createSnapshot(
        profileId: profile.id,
        sourceVersion: profile.version,
        idempotencyKey: _snapshotKey ??= _newId('job-target-snapshot'),
      );
      _requireCurrent(operationEpoch, _input);
      if (snapshot.sourceJobTargetId != target.id ||
          snapshot.sourceJobTargetConfirmationVersion !=
              confirmation.confirmationVersion ||
          snapshot.jobTargetInput != _input ||
          snapshot.jobTargetCandidate == null ||
          !_sameJobTargetCandidate(
            snapshot.jobTargetCandidate!,
            confirmation.candidate,
          )) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.snapshot,
        );
      }

      _operationStage = JobPreparationOperationStage.plan;
      notifyListeners();
      final plan = await client.createPlan(
        input: CreatePreparationPlanInput(
          sourceThreadId: context.threadId,
          goalId: context.goalId,
          preparationSnapshotId: snapshot.id,
          sceneId: candidate.catalogRecommendation.sceneId,
          sceneVersion: candidate.catalogRecommendation.sceneVersion,
          selectedRoleIds: candidate.catalogRecommendation.selectedRoleIds,
          practiceOptionId: candidate.catalogRecommendation.practiceOptionId,
        ),
        idempotencyKey: _planKey ??= _newId('job-target-plan'),
      );
      _requireCurrent(operationEpoch, _input);
      if (plan.userId != target.userId ||
          plan.agentContext != context ||
          plan.preparationSnapshot.sourceJobTargetId != target.id ||
          plan.preparationSnapshot.sourceJobTargetConfirmationVersion !=
              confirmation.confirmationVersion) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.plan,
        );
      }
      _plan = plan;
      _bootstrap = null;
      _sessionKey = null;
      _voiceKey = null;
      _step = JobPreparationStep.preview;
      _errorMessage = null;
      if (workspace != null) {
        workspaceParked = await parkCurrentPractice();
        if (!workspaceParked) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.network,
            stage: JobPreparationOperationStage.goal,
            retryable: true,
          );
        }
      }
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _operationStage = error.stage ?? _operationStage;
        _errorMessage = workspace?.errorMessage ?? _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = workspace?.errorMessage ?? '暂时无法生成练习预览，请稍后重试。';
      }
      return false;
    } finally {
      if (!workspaceParked &&
          _isCurrent(operationEpoch) &&
          workspace != null &&
          workspace.currentLease?.operationId == workspaceOperationId) {
        final parked = await workspace.parkCurrentPractice();
        if (!parked && _isCurrent(operationEpoch)) {
          _errorMessage ??= workspace.errorMessage ?? '面试预览已保留，但暂时无法返回首页。';
        }
      }
      _finish(operationEpoch);
    }
  }

  Future<bool> revisePreview({
    required String roleDefinitionId,
    required String practiceOptionId,
    required int maxEffectiveTurns,
  }) async {
    final plan = _plan;
    if (_disposed || _busy || plan == null) {
      return false;
    }
    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.plan);
    try {
      final revised = await client.revisePlan(
        planId: plan.id,
        input: RevisePreparationPlanInput(
          expectedPlanRevision: plan.revision,
          selectedRoleIds: <String>[roleDefinitionId],
          practiceOptionId: practiceOptionId,
          maxEffectiveTurns: maxEffectiveTurns,
        ),
        idempotencyKey: _planRevisionKey ??= _newId('job-target-plan-revision'),
      );
      _requireCurrent(operationEpoch, _input);
      if (revised.agentContext != plan.agentContext ||
          revised.preparationSnapshot.id != plan.preparationSnapshot.id) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.plan,
        );
      }
      _plan = revised;
      _planRevisionKey = null;
      _bootstrap = null;
      _sessionKey = null;
      _voiceKey = null;
      _errorMessage = null;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法更新练习预览，请稍后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  Future<bool> startPractice() async {
    final plan = _plan;
    if (_disposed || _busy || plan == null) {
      return false;
    }
    final operationEpoch = _epoch;
    final workspace = workspaceController;
    final context = plan.agentContext;
    if (context == null) {
      _errorMessage = '练习计划缺少 Agent 上下文，请重新生成。';
      _operationStage = JobPreparationOperationStage.plan;
      notifyListeners();
      return false;
    }
    final workspaceOperationId = _workspaceLease?.operationId;
    var practiceStarted = false;
    _begin(
      _bootstrap == null
          ? JobPreparationOperationStage.session
          : JobPreparationOperationStage.voice,
    );
    try {
      if (workspace != null) {
        final previousLease = _workspaceLease;
        if (previousLease == null) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.conflict,
            stage: JobPreparationOperationStage.goal,
            retryable: true,
          );
        }
        final lease = await workspace.acquireThread(previousLease.operationId);
        if (lease == null ||
            !_sameWorkspaceIdentity(lease, previousLease) ||
            lease.practiceThreadId != context.threadId ||
            threadIdProvider() != context.threadId) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.conflict,
            stage: JobPreparationOperationStage.goal,
            retryable: true,
          );
        }
        _workspaceLease = lease;
        _requireCurrent(operationEpoch, _input);
      }
      final bootstrap =
          _bootstrap ??
          await client.createSession(
            plan: plan,
            input: CreatePreparationSessionInput(
              expectedPlanRevision: plan.revision,
              userConfirmed: true,
            ),
            idempotencyKey: _sessionKey ??= _newId('job-target-session'),
          );
      _requireCurrent(operationEpoch, _input);
      _bootstrap = bootstrap;

      if (workspace != null) {
        _operationStage = JobPreparationOperationStage.session;
        notifyListeners();
        final lease = _workspaceLease;
        final scene = plan.sceneSelection.scene;
        if (lease == null ||
            lease.practiceThreadId != context.threadId ||
            !await workspace.commitSession(
              lease: lease,
              goalId: context.goalId,
              sessionId: bootstrap.session.id,
              scene: scene,
            )) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.network,
            stage: JobPreparationOperationStage.session,
            retryable: true,
          );
        }
        _requireCurrent(operationEpoch, _input);
      }

      _operationStage = JobPreparationOperationStage.voice;
      notifyListeners();
      await voiceActivator(
        context: context,
        scene: plan.sceneSelection.scene,
        bootstrap: bootstrap,
        clientOperationId: _voiceKey ??= _newId('job-target-voice'),
      );
      _requireCurrent(operationEpoch, _input);
      _errorMessage = null;
      _draftCompleted = true;
      await _deleteActiveDraft();
      _requireCurrent(operationEpoch, _input);
      practiceStarted = true;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _operationStage = error.stage ?? _operationStage;
        _errorMessage = workspaceController?.errorMessage ?? _messageFor(error);
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage =
            workspaceController?.errorMessage ??
            (_bootstrap == null
                ? '暂时无法创建练习，本次预览已保留，可以重试。'
                : '练习已创建，但语音题目暂时无法连接。请重试连接。');
      }
      return false;
    } finally {
      if (!practiceStarted &&
          _isCurrent(operationEpoch) &&
          workspace != null &&
          workspace.currentLease?.operationId == workspaceOperationId) {
        final parked = await workspace.parkCurrentPractice();
        if (!parked && _isCurrent(operationEpoch)) {
          _errorMessage ??= workspace.errorMessage ?? '练习已保留，但暂时无法返回首页。';
        }
      }
      _finish(operationEpoch);
    }
  }

  Future<bool> retry() {
    return switch (_operationStage) {
      JobPreparationOperationStage.target ||
      JobPreparationOperationStage.analysis => analyze(),
      JobPreparationOperationStage.confirmation => confirm(),
      JobPreparationOperationStage.profile ||
      JobPreparationOperationStage.snapshot ||
      JobPreparationOperationStage.goal => createPreview(),
      JobPreparationOperationStage.plan
          when _step != JobPreparationStep.preview =>
        createPreview(),
      JobPreparationOperationStage.plan => Future<bool>.value(false),
      JobPreparationOperationStage.session ||
      JobPreparationOperationStage.voice => startPractice(),
      null => Future<bool>.value(false),
    };
  }

  Future<void> clearPrivateState() async {
    final accountId = _accountId;
    _accountGeneration++;
    _epoch++;
    _accountId = null;
    _loadedDraftAccountId = null;
    _restorableDraft = null;
    _initializingDraft = false;
    _resetPresentation();
    await client.clearAccountState();
    await _draftWriteTail;
    if (accountId != null) {
      await draftStore.delete(accountId);
    }
    if (!_disposed) {
      notifyListeners();
    }
  }

  void _applyStoredDraft(_StoredJobPreparationDraft draft) {
    _input = draft.input;
    _target = null;
    _candidate = draft.candidate;
    _resumeSelection = draft.resumeSelection;
    _plan = null;
    _bootstrap = null;
    _step = JobPreparationStep.input;
    _operationStage = null;
    _errorMessage = null;
    _draftCompleted = false;
    _createTargetKey = draft.createTargetKey;
    _updateTargetKey = draft.updateTargetKey;
    _analysisKey = draft.analysisKey;
    _confirmationKey = draft.confirmationKey;
    _discardTargetKey = draft.discardTargetKey;
    _goalKey = draft.goalKey;
    _profileKey = draft.profileKey;
    _snapshotKey = draft.snapshotKey;
    _planKey = draft.planKey;
    _planRevisionKey = null;
    _sessionKey = draft.sessionKey;
    _voiceKey = draft.voiceKey;
  }

  void _applyTargetStage(
    JobTarget target, {
    JobTargetCandidate? storedCandidate,
  }) {
    switch (target.stage) {
      case JobTargetStage.awaitingConfirmation:
        final serverCandidate = target.analysis?.candidate;
        _candidate = storedCandidate?.source == serverCandidate?.source
            ? storedCandidate
            : serverCandidate;
        _step = JobPreparationStep.confirmation;
      case JobTargetStage.confirmed:
        _candidate = target.confirmation?.candidate;
        _step = JobPreparationStep.setup;
      case JobTargetStage.draft ||
          JobTargetStage.parsing ||
          JobTargetStage.analysisFailed:
        _candidate = null;
        _step = JobPreparationStep.input;
      case JobTargetStage.discarded:
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.notFound,
          stage: JobPreparationOperationStage.target,
        );
    }
  }

  void _resetPresentation() {
    _input = const JobTargetInput(source: JobTargetSource.jobDescription);
    _agentIntentPrefill = null;
    _target = null;
    _candidate = null;
    _resumeSelection = null;
    _plan = null;
    _bootstrap = null;
    _step = JobPreparationStep.input;
    _operationStage = null;
    _errorMessage = null;
    _busy = false;
    _draftCompleted = false;
    _createTargetKey = null;
    _updateTargetKey = null;
    _analysisKey = null;
    _confirmationKey = null;
    _discardTargetKey = null;
    _clearPreviewAttempt();
  }

  void _queueDraftWrite() {
    final accountId = _accountId;
    if (_disposed ||
        accountId == null ||
        _restorableDraft != null ||
        _draftCompleted) {
      return;
    }
    final encoded = _StoredJobPreparationDraft(
      input: _input,
      targetId: _target?.id,
      candidate: _candidate,
      resumeSelection: _resumeSelection,
      planId: _plan?.id,
      createTargetKey: _createTargetKey,
      updateTargetKey: _updateTargetKey,
      analysisKey: _analysisKey,
      confirmationKey: _confirmationKey,
      discardTargetKey: _discardTargetKey,
      goalKey: _goalKey,
      profileKey: _profileKey,
      snapshotKey: _snapshotKey,
      planKey: _planKey,
      sessionKey: _sessionKey,
      voiceKey: _voiceKey,
    ).encode();
    _draftWriteTail = _draftWriteTail
        .then((_) => draftStore.write(accountId, encoded))
        .catchError((Object _) {});
  }

  Future<void> _deleteActiveDraft() async {
    final accountId = _accountId;
    if (accountId == null) {
      return;
    }
    await _draftWriteTail;
    await draftStore.delete(accountId);
  }

  void _clearPreviewAttempt() {
    _goalKey = null;
    _profileKey = null;
    _snapshotKey = null;
    _planKey = null;
    _planRevisionKey = null;
    _sessionKey = null;
    _voiceKey = null;
    _workspaceKey = null;
    _workspaceLease = null;
    _workspaceReplaceRequested = false;
  }

  void _handleWorkspaceState() {
    if (!_disposed) {
      notifyListeners();
    }
  }

  void _begin(JobPreparationOperationStage stage) {
    _busy = true;
    _operationStage = stage;
    _errorMessage = null;
    notifyListeners();
  }

  void _finish(int operationEpoch) {
    if (_isCurrent(operationEpoch)) {
      _busy = false;
      notifyListeners();
      _queueDraftWrite();
    }
  }

  void _requireCurrent(int operationEpoch, JobTargetInput expectedInput) {
    if (!_isCurrent(operationEpoch) || _input != expectedInput) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.superseded,
      );
    }
  }

  bool _isCurrent(int operationEpoch) {
    return !_disposed && operationEpoch == _epoch;
  }

  bool _isCurrentAccount(int generation, String accountId) {
    return !_disposed &&
        generation == _accountGeneration &&
        accountId == _accountId;
  }

  String _newId(String scope) {
    final value = _idFactory(scope);
    if (value.length < 8 ||
        value.length > 128 ||
        value.trim() != value ||
        value.contains('\u0000')) {
      throw StateError('Invalid Job preparation idempotency identity.');
    }
    return value;
  }

  @override
  void dispose() {
    _disposed = true;
    _epoch++;
    workspaceController?.removeListener(_handleWorkspaceState);
    super.dispose();
  }
}

final class _StoredJobPreparationDraft {
  const _StoredJobPreparationDraft({
    required this.input,
    this.targetId,
    this.candidate,
    this.resumeSelection,
    this.planId,
    this.createTargetKey,
    this.updateTargetKey,
    this.analysisKey,
    this.confirmationKey,
    this.discardTargetKey,
    this.goalKey,
    this.profileKey,
    this.snapshotKey,
    this.planKey,
    this.sessionKey,
    this.voiceKey,
  });

  final JobTargetInput input;
  final String? targetId;
  final JobTargetCandidate? candidate;
  final JobPreparationResumeSelection? resumeSelection;
  final String? planId;
  final String? createTargetKey;
  final String? updateTargetKey;
  final String? analysisKey;
  final String? confirmationKey;
  final String? discardTargetKey;
  final String? goalKey;
  final String? profileKey;
  final String? snapshotKey;
  final String? planKey;
  final String? sessionKey;
  final String? voiceKey;

  String encode() {
    return jsonEncode(<String, Object?>{
      'schema_version': 3,
      'input': _storedInputJson(input),
      'target_id': ?targetId,
      'candidate': ?(candidate == null
          ? null
          : _storedCandidateJson(candidate!)),
      'resume_selection': ?(resumeSelection == null
          ? null
          : <String, Object?>{
              'resume_id': resumeSelection!.resumeId,
              'revision': resumeSelection!.revision,
              'resource_version': resumeSelection!.resourceVersion,
              'temporary': resumeSelection!.temporary,
              'title': resumeSelection!.title,
            }),
      'plan_id': ?planId,
      'create_target_key': ?createTargetKey,
      'update_target_key': ?updateTargetKey,
      'analysis_key': ?analysisKey,
      'confirmation_key': ?confirmationKey,
      'discard_target_key': ?discardTargetKey,
      'goal_key': ?goalKey,
      'profile_key': ?profileKey,
      'snapshot_key': ?snapshotKey,
      'plan_key': ?planKey,
      'session_key': ?sessionKey,
      'voice_key': ?voiceKey,
    });
  }

  static _StoredJobPreparationDraft? tryDecode(String encoded) {
    try {
      if (utf8.encode(encoded).length > 256 * 1024) {
        return null;
      }
      final value = jsonDecode(encoded);
      final schemaVersion = value is Map<String, Object?>
          ? value['schema_version']
          : null;
      if (value is! Map<String, Object?> ||
          (schemaVersion != 2 && schemaVersion != 3) ||
          !value.containsKey('input') ||
          value.keys.any(
            (key) => !const <String>{
              'schema_version',
              'input',
              'target_id',
              'candidate',
              'resume_selection',
              'plan_id',
              'create_target_key',
              'update_target_key',
              'analysis_key',
              'confirmation_key',
              'discard_target_key',
              'goal_key',
              'profile_key',
              'snapshot_key',
              'plan_key',
              'session_key',
              'voice_key',
            }.contains(key),
          ) ||
          (schemaVersion == 2 && value.containsKey('resume_selection'))) {
        return null;
      }
      final input = _storedInput(value['input']);
      final candidate = value.containsKey('candidate')
          ? _storedCandidate(value['candidate'])
          : null;
      final resumeSelection = value.containsKey('resume_selection')
          ? _storedResumeSelection(value['resume_selection'])
          : null;
      final targetId = _storedOptionalId(value, 'target_id');
      final planId = _storedOptionalId(value, 'plan_id');
      if ((candidate != null && candidate.source != input.source) ||
          (planId != null && targetId == null)) {
        return null;
      }
      return _StoredJobPreparationDraft(
        input: input,
        targetId: targetId,
        candidate: candidate,
        resumeSelection: resumeSelection,
        planId: planId,
        createTargetKey: _storedOptionalKey(value, 'create_target_key'),
        updateTargetKey: _storedOptionalKey(value, 'update_target_key'),
        analysisKey: _storedOptionalKey(value, 'analysis_key'),
        confirmationKey: _storedOptionalKey(value, 'confirmation_key'),
        discardTargetKey: _storedOptionalKey(value, 'discard_target_key'),
        goalKey: _storedOptionalKey(value, 'goal_key'),
        profileKey: _storedOptionalKey(value, 'profile_key'),
        snapshotKey: _storedOptionalKey(value, 'snapshot_key'),
        planKey: _storedOptionalKey(value, 'plan_key'),
        sessionKey: _storedOptionalKey(value, 'session_key'),
        voiceKey: _storedOptionalKey(value, 'voice_key'),
      );
    } on Object {
      return null;
    }
  }
}

JobPreparationResumeSelection _storedResumeSelection(Object? value) {
  if (value is! Map<String, Object?> ||
      value.keys.toSet().difference(const <String>{
        'resume_id',
        'revision',
        'resource_version',
        'temporary',
        'title',
      }).isNotEmpty ||
      value.length != 5 ||
      value['resume_id'] is! String ||
      value['revision'] is! int ||
      value['resource_version'] is! int ||
      value['temporary'] is! bool ||
      value['title'] is! String) {
    throw const FormatException('Invalid stored resume selection.');
  }
  final id = value['resume_id']! as String;
  final revision = value['revision']! as int;
  final resourceVersion = value['resource_version']! as int;
  final title = (value['title']! as String).trim();
  if (!_validResourceId(id) ||
      revision < 1 ||
      resourceVersion < 1 ||
      title.isEmpty) {
    throw const FormatException('Invalid stored resume selection.');
  }
  return JobPreparationResumeSelection(
    resumeId: id,
    revision: revision,
    resourceVersion: resourceVersion,
    temporary: value['temporary']! as bool,
    title: title,
  );
}

Map<String, Object?> _storedInputJson(JobTargetInput input) {
  return <String, Object?>{
    'source': input.source.wireValue,
    'job_title': ?input.jobTitle,
    'job_description': ?input.jobDescription,
    'company': ?input.company,
    'seniority': ?input.seniority,
    'candidate_background': ?input.candidateBackground,
    'resume_ref': ?input.resumeRef,
    'practice_focus': ?input.practiceFocus,
  };
}

JobTargetInput _storedInput(Object? value) {
  if (value is! Map<String, Object?> ||
      value['source'] is! String ||
      value.keys.any(
        (key) => !const <String>{
          'source',
          'job_title',
          'job_description',
          'company',
          'seniority',
          'candidate_background',
          'resume_ref',
          'practice_focus',
        }.contains(key),
      )) {
    throw const FormatException('Invalid stored JobTarget input.');
  }
  final source = switch (value['source']) {
    'job_description' => JobTargetSource.jobDescription,
    'quick_start' => JobTargetSource.quickStart,
    _ => throw const FormatException('Invalid stored JobTarget source.'),
  };
  final input = JobTargetInput(
    source: source,
    jobTitle: _storedOptionalInputText(value, 'job_title', 512),
    jobDescription: _storedOptionalInputText(
      value,
      'job_description',
      64 * 1024,
    ),
    company: _storedOptionalInputText(value, 'company', 512),
    seniority: _storedOptionalInputText(value, 'seniority', 256),
    candidateBackground: _storedOptionalInputText(
      value,
      'candidate_background',
      16 * 1024,
    ),
    resumeRef: _storedOptionalInputText(value, 'resume_ref', 16 * 1024),
    practiceFocus: _storedOptionalInputText(value, 'practice_focus', 8 * 1024),
  );
  if (!_validPartialJobTargetInput(input)) {
    throw const FormatException('Invalid stored JobTarget input.');
  }
  return input;
}

Map<String, Object?> _storedCandidateJson(JobTargetCandidate candidate) {
  return <String, Object?>{
    'source': candidate.source.wireValue,
    'general_advice_only': candidate.generalAdviceOnly,
    'job_title': candidate.jobTitle,
    'seniority': candidate.seniority,
    'responsibilities': candidate.responsibilities,
    'core_skills': candidate.coreSkills,
    'communication_focus': candidate.communicationFocus,
    'practice_goals': candidate.practiceGoals,
    'scope_notice': candidate.scopeNotice,
    'catalog_recommendation': <String, Object?>{
      'scene_id': candidate.catalogRecommendation.sceneId,
      'scene_version': candidate.catalogRecommendation.sceneVersion,
      'selected_role_ids': candidate.catalogRecommendation.selectedRoleIds,
      'practice_option_id': candidate.catalogRecommendation.practiceOptionId,
    },
  };
}

JobTargetCandidate _storedCandidate(Object? value) {
  if (value is! Map<String, Object?> ||
      value.keys.toSet().difference(const <String>{
        'source',
        'general_advice_only',
        'job_title',
        'seniority',
        'responsibilities',
        'core_skills',
        'communication_focus',
        'practice_goals',
        'scope_notice',
        'catalog_recommendation',
      }).isNotEmpty ||
      value.length != 10) {
    throw const FormatException('Invalid stored JobTarget candidate.');
  }
  final source = switch (value['source']) {
    'job_description' => JobTargetSource.jobDescription,
    'quick_start' => JobTargetSource.quickStart,
    _ => throw const FormatException('Invalid stored JobTarget source.'),
  };
  final recommendation = value['catalog_recommendation'];
  if (recommendation is! Map<String, Object?> ||
      recommendation.length != 4 ||
      recommendation.keys.toSet().difference(const <String>{
        'scene_id',
        'scene_version',
        'selected_role_ids',
        'practice_option_id',
      }).isNotEmpty) {
    throw const FormatException('Invalid stored catalog recommendation.');
  }
  final roles = _storedStringList(
    recommendation['selected_role_ids'],
    maxItems: 1,
    maxBytes: 128,
  );
  final candidate = JobTargetCandidate(
    source: source,
    generalAdviceOnly: value['general_advice_only'] as bool,
    jobTitle: _storedText(value['job_title'], 512),
    seniority: _storedText(value['seniority'], 256),
    responsibilities: _storedStringList(value['responsibilities']),
    coreSkills: _storedStringList(value['core_skills']),
    communicationFocus: _storedStringList(value['communication_focus']),
    practiceGoals: _storedStringList(value['practice_goals']),
    scopeNotice: _storedText(value['scope_notice'], 2048),
    catalogRecommendation: JobTargetCatalogRecommendation(
      sceneId: _storedId(recommendation['scene_id']),
      sceneVersion: _storedVersion(recommendation['scene_version']),
      selectedRoleIds: roles,
      practiceOptionId: _storedId(recommendation['practice_option_id']),
    ),
  );
  if (candidate.generalAdviceOnly !=
      (candidate.source == JobTargetSource.quickStart)) {
    throw const FormatException('Invalid stored JobTarget advice scope.');
  }
  return candidate;
}

String? _storedOptionalId(Map<String, Object?> value, String key) {
  return value.containsKey(key) ? _storedId(value[key]) : null;
}

String _storedId(Object? value) {
  final result = _storedText(value, 128);
  if (!_validResourceId(result)) {
    throw const FormatException('Invalid stored resource identity.');
  }
  return result;
}

String? _storedOptionalKey(Map<String, Object?> value, String key) {
  if (!value.containsKey(key)) {
    return null;
  }
  final result = _storedText(value[key], 128);
  if (result.length < 8) {
    throw const FormatException('Invalid stored idempotency identity.');
  }
  return result;
}

String? _storedOptionalInputText(
  Map<String, Object?> value,
  String key,
  int maxBytes,
) {
  if (!value.containsKey(key)) {
    return null;
  }
  final result = value[key];
  if (result is! String ||
      result.isEmpty ||
      result.contains('\u0000') ||
      utf8.encode(result).length > maxBytes) {
    throw const FormatException('Invalid stored input text.');
  }
  return result;
}

String _storedText(Object? value, int maxBytes) {
  if (value is! String ||
      value.isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const FormatException('Invalid stored text.');
  }
  return value;
}

int _storedVersion(Object? value) {
  if (value is! int || value < 1) {
    throw const FormatException('Invalid stored version.');
  }
  return value;
}

List<String> _storedStringList(
  Object? value, {
  int maxItems = 20,
  int maxBytes = 8 * 1024,
}) {
  if (value is! List<Object?> || value.isEmpty || value.length > maxItems) {
    throw const FormatException('Invalid stored list.');
  }
  final result = value
      .map((item) => _storedText(item, maxBytes))
      .toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const FormatException('Invalid stored list.');
  }
  return List<String>.unmodifiable(result);
}

bool _validPartialJobTargetInput(JobTargetInput input) {
  bool valid(String? value, int maxBytes) {
    return value == null ||
        (value.isNotEmpty &&
            !value.contains('\u0000') &&
            utf8.encode(value).length <= maxBytes);
  }

  return valid(input.jobTitle, 512) &&
      valid(input.jobDescription, 64 * 1024) &&
      valid(input.company, 512) &&
      valid(input.seniority, 256) &&
      valid(input.candidateBackground, 16 * 1024) &&
      valid(input.resumeRef, 16 * 1024) &&
      valid(input.practiceFocus, 8 * 1024) &&
      !(input.source == JobTargetSource.quickStart &&
          input.jobDescription != null);
}

bool _sameJobTargetCandidate(
  JobTargetCandidate left,
  JobTargetCandidate right,
) {
  final leftRecommendation = left.catalogRecommendation;
  final rightRecommendation = right.catalogRecommendation;
  return left.source == right.source &&
      left.generalAdviceOnly == right.generalAdviceOnly &&
      left.jobTitle == right.jobTitle &&
      left.seniority == right.seniority &&
      listEquals(left.responsibilities, right.responsibilities) &&
      listEquals(left.coreSkills, right.coreSkills) &&
      listEquals(left.communicationFocus, right.communicationFocus) &&
      listEquals(left.practiceGoals, right.practiceGoals) &&
      left.scopeNotice == right.scopeNotice &&
      leftRecommendation.sceneId == rightRecommendation.sceneId &&
      leftRecommendation.sceneVersion == rightRecommendation.sceneVersion &&
      listEquals(
        leftRecommendation.selectedRoleIds,
        rightRecommendation.selectedRoleIds,
      ) &&
      leftRecommendation.practiceOptionId ==
          rightRecommendation.practiceOptionId;
}

bool _validJobTargetInput(JobTargetInput input) {
  bool valid(String? value, int maxBytes) {
    return value == null ||
        (value.isNotEmpty &&
            value.trim() == value &&
            !value.contains('\u0000') &&
            utf8.encode(value).length <= maxBytes);
  }

  return valid(input.jobTitle, 512) &&
      valid(input.jobDescription, 64 * 1024) &&
      valid(input.company, 512) &&
      valid(input.seniority, 256) &&
      valid(input.candidateBackground, 16 * 1024) &&
      valid(input.resumeRef, 16 * 1024) &&
      valid(input.practiceFocus, 8 * 1024) &&
      switch (input.source) {
        JobTargetSource.jobDescription => input.jobDescription != null,
        JobTargetSource.quickStart =>
          input.jobTitle != null && input.jobDescription == null,
      };
}

bool _sameWorkspaceIdentity(
  PracticeWorkspaceLease left,
  PracticeWorkspaceLease right,
) {
  return left.operationId == right.operationId &&
      left.practiceThreadId == right.practiceThreadId;
}

bool _validResourceId(String value) {
  return value.isNotEmpty &&
      value.length <= 128 &&
      value.trim() == value &&
      !value.contains('\u0000');
}

String _messageFor(JobPreparationException error) {
  return switch (error.kind) {
    JobPreparationFailureKind.authenticationRequired => '登录状态已失效，请重新登录后继续。',
    JobPreparationFailureKind.invalidRequest =>
      error.stage == JobPreparationOperationStage.confirmation
          ? '确认内容无法提交，请检查每一项后重试。'
          : '当前信息无法提交，请检查填写内容后重试。',
    JobPreparationFailureKind.notFound => '这份准备草稿已不存在，请重新开始。',
    JobPreparationFailureKind.conflict => '服务端版本已变化，请恢复最新草稿后再继续。',
    JobPreparationFailureKind.network => '网络连接不稳定，本次进度已保留，可以重试。',
    JobPreparationFailureKind.server =>
      error.stage == JobPreparationOperationStage.analysis
          ? '岗位分析暂时失败，可以重试；已填写内容不会丢失。'
          : '准备服务暂时不可用，本次进度已保留，可以重试。',
    JobPreparationFailureKind.invalidResponse => '准备服务返回了无法识别的数据，请稍后重试。',
    JobPreparationFailureKind.superseded => '',
  };
}

final Random _jobPreparationRandom = Random.secure();

String _secureJobPreparationId(String scope) {
  final value = StringBuffer('${scope}_');
  for (var index = 0; index < 16; index++) {
    value.write(
      _jobPreparationRandom.nextInt(256).toRadixString(16).padLeft(2, '0'),
    );
  }
  return value.toString();
}
