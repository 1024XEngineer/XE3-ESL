import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

typedef JobPreparationIdFactory = String Function(String scope);
typedef JobPreparationVoiceActivator =
    Future<void> Function({
      required SceneDefinition scene,
      required PreparationPracticeBootstrap bootstrap,
      required String clientOperationId,
    });

final class JobPreparationController extends ChangeNotifier {
  JobPreparationController({
    required this.client,
    required this.voiceActivator,
    required this.workspaceController,
    JobPreparationIdFactory? idFactory,
  }) : _idFactory = idFactory ?? _secureJobPreparationId {
    workspaceController.addListener(_handleWorkspaceState);
  }

  final JobPreparationClient client;
  final JobPreparationVoiceActivator voiceActivator;
  final PracticeWorkspaceController workspaceController;
  final JobPreparationIdFactory _idFactory;

  InterviewPreparationInput _input = const InterviewPreparationInput(
    source: InterviewPreparationSource.jobDescription,
  );
  InterviewPreparation? _interviewPreparation;
  InterviewPreparationCandidate? _candidate;
  InterviewResumeFile? _resumeSelection;
  PracticePlan? _plan;
  PreparationPracticeBootstrap? _bootstrap;
  JobPreparationOperationStage? _operationStage;
  String? _errorMessage;
  String? _agentIntentPrefill;
  String? _accountId;
  bool _busy = false;
  bool _disposed = false;
  bool _openedSavedPlan = false;
  int _epoch = 0;
  int _accountGeneration = 0;
  int _planListEpoch = 0;

  List<PracticePlanSummary> _interviewPlans = const [];
  bool _plansLoading = false;
  bool _plansLoaded = false;
  String? _plansErrorMessage;

  String? _createPreparationKey;
  String? _regeneratePreparationKey;
  String? _confirmPreparationKey;
  String? _planKey;
  String? _savedPlanConfirmationKey;
  String? _sessionKey;
  String? _voiceKey;
  String? _workspaceKey;
  PracticeWorkspaceLease? _workspaceLease;

  InterviewPreparationInput get input => _input;
  InterviewPreparation? get interviewPreparation => _interviewPreparation;
  InterviewPreparationCandidate? get candidate => _candidate;
  InterviewResumeFile? get resumeSelection => _resumeSelection;
  PracticePlan? get plan => _plan;
  PreparationPracticeBootstrap? get bootstrap => _bootstrap;
  JobPreparationOperationStage? get operationStage => _operationStage;
  String? get errorMessage => _errorMessage ?? workspaceController.errorMessage;
  String? get agentIntentPrefill => _agentIntentPrefill;
  bool get isBusy => _busy || workspaceController.isBusy;
  bool get canRetry => _errorMessage != null && !isBusy;
  bool get openedSavedPlan => _openedSavedPlan;
  bool get hasResumablePractice => workspaceController.hasResumable;
  String? get resumablePracticeTitle => workspaceController.currentTitle;
  String? get workspaceErrorMessage => workspaceController.errorMessage;
  List<PracticePlanSummary> get interviewPlans => _interviewPlans;
  bool get plansLoading => _plansLoading;
  bool get plansLoaded => _plansLoaded;
  String? get plansErrorMessage => _plansErrorMessage;

  Future<void> loadInterviewPlans({bool force = false}) async {
    if (_disposed || _plansLoading || (_plansLoaded && !force)) return;
    _plansLoading = true;
    _plansErrorMessage = null;
    final requestEpoch = ++_planListEpoch;
    notifyListeners();
    try {
      final plans = await client.listPlans(
        experience: PracticeExperience.interview,
      );
      if (_disposed || requestEpoch != _planListEpoch) return;
      _interviewPlans = plans;
      _plansLoaded = true;
    } on Object {
      if (!_disposed && requestEpoch == _planListEpoch) {
        _plansErrorMessage = '暂时无法加载模拟面试，请稍后重试。';
      }
    } finally {
      if (!_disposed && requestEpoch == _planListEpoch) {
        _plansLoading = false;
        notifyListeners();
      }
    }
  }

  Future<bool> deleteInterviewPlan(String planId) async {
    if (_disposed || _plansLoading || !_validUUID(planId)) return false;
    _plansLoading = true;
    _plansErrorMessage = null;
    final requestEpoch = ++_planListEpoch;
    notifyListeners();
    try {
      await client.deletePlan(planId);
      if (_disposed || requestEpoch != _planListEpoch) return false;
      _interviewPlans = _interviewPlans
          .where((plan) => plan.id != planId)
          .toList(growable: false);
      return true;
    } on Object {
      if (!_disposed && requestEpoch == _planListEpoch) {
        _plansErrorMessage = '暂时无法删除这场模拟面试，请稍后重试。';
      }
      return false;
    } finally {
      if (!_disposed && requestEpoch == _planListEpoch) {
        _plansLoading = false;
        notifyListeners();
      }
    }
  }

  void beginNewPreparation() {
    if (_disposed || isBusy) return;
    _epoch++;
    _resetPresentation();
    notifyListeners();
  }

  Future<bool> openSavedPlan(String planId) async {
    if (_disposed || isBusy || !_validUUID(planId)) return false;
    final operationEpoch = ++_epoch;
    _resetPresentation();
    _begin(JobPreparationOperationStage.plan);
    try {
      final plan = await client.getPlan(planId);
      _requireCurrent(operationEpoch, _input);
      final interview = plan.preparationSnapshot.interview;
      final openableStatus =
          plan.status == PracticePlanStatus.ready ||
          (plan.status == PracticePlanStatus.draft && interview == null);
      if (!openableStatus ||
          plan.sceneSelection.scene.experience !=
              PracticeExperience.interview) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.plan,
        );
      }
      if (interview != null) {
        _input = interview.input;
        _candidate = interview.candidate;
      }
      _plan = plan;
      _openedSavedPlan = true;
      _errorMessage = null;
      return true;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法打开这场模拟面试，请稍后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  void offerAgentIntent(String? value) {
    final normalized = value?.trim();
    if (_disposed ||
        _interviewPreparation != null ||
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
    if (_disposed || value == null || _interviewPreparation != null) return;
    _agentIntentPrefill = null;
    updateInput(
      InterviewPreparationInput(
        source: InterviewPreparationSource.jobDescription,
        jobDescription: value,
        company: _input.company,
        seniority: _input.seniority,
        candidateBackground: _input.candidateBackground,
        practiceFocus: _input.practiceFocus,
      ),
    );
  }

  void dismissAgentIntentPrefill() {
    if (_disposed || _agentIntentPrefill == null) return;
    _agentIntentPrefill = null;
    notifyListeners();
  }

  Future<void> activateAccount(String accountId) async {
    if (_disposed || !_validUUID(accountId) || _accountId == accountId) return;
    final generation = ++_accountGeneration;
    _epoch++;
    _accountId = accountId;
    _planListEpoch++;
    _interviewPlans = const [];
    _plansLoading = false;
    _plansLoaded = false;
    _plansErrorMessage = null;
    _resetPresentation();
    notifyListeners();
    await workspaceController.activateAccount(accountId);
    if (!_disposed && generation == _accountGeneration) notifyListeners();
  }

  void updateInput(InterviewPreparationInput value) {
    if (_disposed || isBusy || value == _input) return;
    _epoch++;
    _input = value;
    _interviewPreparation = null;
    _candidate = null;
    _plan = null;
    _bootstrap = null;
    _openedSavedPlan = false;
    _errorMessage = null;
    _operationStage = null;
    _clearAttemptKeys();
    notifyListeners();
  }

  void updateCandidate(InterviewPreparationCandidate value) {
    if (_disposed ||
        isBusy ||
        _interviewPreparation?.status != InterviewPreparationStatus.draft) {
      return;
    }
    _candidate = value;
    _confirmPreparationKey = null;
    _plan = null;
    _bootstrap = null;
    notifyListeners();
  }

  void selectResume(InterviewResumeFile? value) {
    if (_disposed || isBusy || _sameResume(_resumeSelection, value)) return;
    _epoch++;
    _resumeSelection = value;
    _interviewPreparation = null;
    _candidate = null;
    _plan = null;
    _bootstrap = null;
    _errorMessage = null;
    _clearAttemptKeys();
    notifyListeners();
  }

  Future<bool> analyze() async {
    if (_disposed || isBusy) return false;
    final input = _input;
    if (!_validInput(input)) {
      _errorMessage = input.source == InterviewPreparationSource.jobDescription
          ? '请粘贴完整职位描述后再分析。'
          : '请填写目标岗位后再开始。';
      _operationStage = JobPreparationOperationStage.interviewPreparation;
      notifyListeners();
      return false;
    }
    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.interviewPreparation);
    try {
      final existing = _interviewPreparation;
      final preparation = existing == null
          ? await client.createInterviewPreparation(
              input: input,
              resume: _resumeSelection,
              idempotencyKey: _createPreparationKey ??= _newId(
                'interview-preparation',
              ),
            )
          : await client.regenerateInterviewPreparation(
              interviewPreparationId: existing.id,
              expectedVersion: existing.version,
              input: input,
              idempotencyKey: _regeneratePreparationKey ??= _newId(
                'interview-preparation-regenerate',
              ),
            );
      _requireCurrent(operationEpoch, input);
      if (preparation.status != InterviewPreparationStatus.draft ||
          preparation.input != input ||
          preparation.resumeUsed != (_resumeSelection != null)) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.interviewPreparation,
        );
      }
      _interviewPreparation = preparation;
      _candidate = preparation.candidate;
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
        _errorMessage = '暂时无法分析岗位信息，请稍后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  Future<bool> confirm() async {
    if (_disposed || isBusy) return false;
    final preparation = _interviewPreparation;
    final candidate = _candidate;
    if (preparation == null ||
        candidate == null ||
        preparation.status != InterviewPreparationStatus.draft ||
        preparation.input != _input ||
        candidate.source != _input.source) {
      _errorMessage = '当前分析已失效，请重新分析岗位信息。';
      _operationStage = JobPreparationOperationStage.confirmation;
      notifyListeners();
      return false;
    }
    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.confirmation);
    try {
      final confirmed = await client.confirmInterviewPreparation(
        interviewPreparationId: preparation.id,
        expectedVersion: preparation.version,
        candidate: candidate,
        idempotencyKey: _confirmPreparationKey ??= _newId(
          'interview-preparation-confirm',
        ),
      );
      _requireCurrent(operationEpoch, _input);
      if (confirmed.status != InterviewPreparationStatus.confirmed ||
          confirmed.id != preparation.id ||
          confirmed.input != _input ||
          confirmed.candidate.source != candidate.source) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.confirmation,
        );
      }
      _interviewPreparation = confirmed;
      _candidate = confirmed.candidate;
      _errorMessage = null;
      return true;
    } on JobPreparationException catch (error) {
      if (_isCurrent(operationEpoch)) _errorMessage = _messageFor(error);
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

  Future<bool> createPreview() async {
    if (_disposed || isBusy) return false;
    final preparation = _interviewPreparation;
    final candidate = preparation?.candidate;
    if (preparation == null ||
        candidate == null ||
        preparation.status != InterviewPreparationStatus.confirmed) {
      _errorMessage = '岗位信息尚未确认，请重新分析。';
      _operationStage = JobPreparationOperationStage.confirmation;
      notifyListeners();
      return false;
    }
    final operationEpoch = _epoch;
    _begin(JobPreparationOperationStage.plan);
    try {
      final plan = await client.createPlan(
        input: CreatePracticePlanInput(
          backgroundSummary:
              _input.candidateBackground ??
              '未提供个人背景，本次按${candidate.jobTitle}通用要求练习。',
          interviewPreparationId: preparation.id,
          expectedInterviewVersion: preparation.version,
          sceneId: candidate.catalogRecommendation.sceneId,
          sceneVersion: candidate.catalogRecommendation.sceneVersion,
          selectedRoleIds: candidate.catalogRecommendation.selectedRoleIds,
          practiceOptionId: candidate.catalogRecommendation.practiceOptionId,
        ),
        idempotencyKey: _planKey ??= _newId('interview-practice-plan'),
      );
      _requireCurrent(operationEpoch, _input);
      final snapshot = plan.preparationSnapshot.interview;
      if (plan.status != PracticePlanStatus.ready ||
          snapshot?.id != preparation.id ||
          snapshot?.version != preparation.version) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.invalidResponse,
          stage: JobPreparationOperationStage.plan,
        );
      }
      _plan = plan;
      _bootstrap = null;
      _upsertInterviewPlan(plan);
      _errorMessage = null;
      return true;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = '暂时无法生成练习计划，请稍后重试。';
      }
      return false;
    } finally {
      _finish(operationEpoch);
    }
  }

  Future<bool> createAndStartPractice() async {
    if (_disposed || isBusy) return false;
    if (!await analyze()) return false;
    if (!await confirm()) return false;
    if (!await createPreview()) return false;
    return startPractice();
  }

  Future<bool> startPractice() async {
    var plan = _plan;
    if (_disposed || isBusy || plan == null) return false;
    final operationEpoch = _epoch;
    var workspaceOperationId = _workspaceLease?.operationId;
    var practiceStarted = false;
    _begin(
      _bootstrap == null
          ? JobPreparationOperationStage.session
          : JobPreparationOperationStage.voice,
    );
    try {
      if (_bootstrap == null &&
          _workspaceLease == null &&
          workspaceController.hasResumableForPlan(plan.id)) {
        final outcome = await workspaceController
            .resumeCurrentPracticeWithOutcome();
        if (outcome == PracticeWorkspaceResumeOutcome.resumed) {
          _errorMessage = null;
          practiceStarted = true;
          return true;
        }
        if (outcome == PracticeWorkspaceResumeOutcome.unavailable ||
            outcome == PracticeWorkspaceResumeOutcome.none) {
          _errorMessage = workspaceController.errorMessage ?? '暂时无法恢复这场模拟面试。';
          return false;
        }
        _bootstrap = null;
        _sessionKey = null;
        _voiceKey = null;
        _workspaceKey = null;
        _workspaceLease = null;
      }
      final existingLease = _workspaceLease;
      final operationId =
          existingLease?.operationId ??
          (_workspaceKey ??= _newId('interview-workspace'));
      final lease = existingLease == null
          ? await workspaceController.replaceCurrentPractice(operationId)
          : await workspaceController.acquirePractice(operationId);
      workspaceOperationId = operationId;
      if (lease == null || (existingLease != null && lease != existingLease)) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.conflict,
          stage: JobPreparationOperationStage.session,
          retryable: true,
        );
      }
      _workspaceLease = lease;
      _requireCurrent(operationEpoch, _input);
      if (plan.status == PracticePlanStatus.draft) {
        final confirmedPlan = await client.confirmPlan(
          planId: plan.id,
          expectedVersion: plan.version,
          idempotencyKey: _savedPlanConfirmationKey ??= _newId(
            'interview-plan-confirmation',
          ),
        );
        _requireCurrent(operationEpoch, _input);
        if (confirmedPlan.id != plan.id ||
            confirmedPlan.version != plan.version + 1 ||
            confirmedPlan.status != PracticePlanStatus.ready ||
            confirmedPlan.sceneSelection.scene.experience !=
                PracticeExperience.interview) {
          throw const JobPreparationException(
            kind: JobPreparationFailureKind.conflict,
            stage: JobPreparationOperationStage.session,
            retryable: true,
          );
        }
        plan = confirmedPlan;
        _plan = confirmedPlan;
      }
      final bootstrap =
          _bootstrap ??
          await client.createSession(
            plan: plan,
            input: CreatePreparationSessionInput(
              expectedPlanVersion: plan.version,
            ),
            idempotencyKey: _sessionKey ??= _newId('interview-session'),
          );
      _requireCurrent(operationEpoch, _input);
      _bootstrap = bootstrap;
      if (!await workspaceController.commitSession(
        lease: lease,
        planId: bootstrap.session.planId,
        sessionId: bootstrap.session.id,
        scene: plan.sceneSelection.scene,
      )) {
        throw const JobPreparationException(
          kind: JobPreparationFailureKind.network,
          stage: JobPreparationOperationStage.session,
          retryable: true,
        );
      }
      _requireCurrent(operationEpoch, _input);
      _operationStage = JobPreparationOperationStage.voice;
      notifyListeners();
      await voiceActivator(
        scene: plan.sceneSelection.scene,
        bootstrap: bootstrap,
        clientOperationId: _voiceKey ??= _newId('interview-voice'),
      );
      _requireCurrent(operationEpoch, _input);
      _errorMessage = null;
      practiceStarted = true;
      return true;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage =
            workspaceController.errorMessage ??
            (_bootstrap == null
                ? '暂时无法创建练习，本次计划已保留，可以重试。'
                : '练习已创建，但语音题目暂时无法连接。请重试连接。');
      }
      return false;
    } finally {
      if (!practiceStarted &&
          _isCurrent(operationEpoch) &&
          workspaceController.currentLease?.operationId ==
              workspaceOperationId) {
        final parked = await workspaceController.parkCurrentPractice();
        if (!parked && _isCurrent(operationEpoch)) {
          _errorMessage ??=
              workspaceController.errorMessage ?? '练习已保留，但暂时无法返回首页。';
        }
      }
      _finish(operationEpoch);
    }
  }

  Future<bool> retry() => switch (_operationStage) {
    JobPreparationOperationStage.interviewPreparation => analyze(),
    JobPreparationOperationStage.confirmation => confirm(),
    JobPreparationOperationStage.plan => createPreview(),
    JobPreparationOperationStage.session ||
    JobPreparationOperationStage.voice => startPractice(),
    null => Future<bool>.value(false),
  };

  Future<bool> resumeCurrentPractice() =>
      workspaceController.resumeCurrentPractice();

  Future<bool> parkCurrentPractice() => workspaceController.currentLease == null
      ? Future<bool>.value(true)
      : workspaceController.parkCurrentPractice();

  Future<void> clearPrivateState() async {
    _accountGeneration++;
    _epoch++;
    _accountId = null;
    _planListEpoch++;
    _interviewPlans = const [];
    _plansLoading = false;
    _plansLoaded = false;
    _plansErrorMessage = null;
    _resetPresentation();
    await client.clearAccountState();
    if (!_disposed) notifyListeners();
  }

  void _upsertInterviewPlan(PracticePlan plan) {
    final candidate = plan.preparationSnapshot.interview?.candidate;
    final summary = PracticePlanSummary(
      id: plan.id,
      version: plan.version,
      status: plan.status,
      experience: plan.sceneSelection.scene.experience,
      sceneName: plan.sceneSelection.scene.name,
      practiceScope: plan.practiceOption.displayName,
      jobTitle: candidate?.jobTitle ?? '',
      practiceObjectives: List<String>.unmodifiable(
        plan.practiceObjectives.map((objective) => objective.description),
      ),
      resumeUsed: plan.preparationSnapshot.interview?.resumeUsed == true,
      suggestedDurationSeconds: plan.sessionPolicy.suggestedDurationSeconds,
      minEffectiveTurns: plan.sessionPolicy.minEffectiveTurns,
      maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
      createdAt: plan.createdAt,
      updatedAt: plan.updatedAt,
    );
    _interviewPlans = <PracticePlanSummary>[
      summary,
      for (final existing in _interviewPlans)
        if (existing.id != summary.id) existing,
    ];
    _plansLoaded = true;
    _plansErrorMessage = null;
  }

  void _resetPresentation() {
    _input = const InterviewPreparationInput(
      source: InterviewPreparationSource.jobDescription,
    );
    _interviewPreparation = null;
    _candidate = null;
    _resumeSelection = null;
    _plan = null;
    _bootstrap = null;
    _operationStage = null;
    _errorMessage = null;
    _agentIntentPrefill = null;
    _busy = false;
    _openedSavedPlan = false;
    _clearAttemptKeys();
  }

  void _clearAttemptKeys() {
    _createPreparationKey = null;
    _regeneratePreparationKey = null;
    _confirmPreparationKey = null;
    _planKey = null;
    _savedPlanConfirmationKey = null;
    _sessionKey = null;
    _voiceKey = null;
    _workspaceKey = null;
    _workspaceLease = null;
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
    }
  }

  void _requireCurrent(
    int operationEpoch,
    InterviewPreparationInput expectedInput,
  ) {
    if (!_isCurrent(operationEpoch) || _input != expectedInput) {
      throw const JobPreparationException(
        kind: JobPreparationFailureKind.superseded,
      );
    }
  }

  bool _isCurrent(int operationEpoch) => !_disposed && operationEpoch == _epoch;

  String _newId(String scope) {
    final value = _idFactory(scope);
    if (value.length < 8 ||
        value.length > 128 ||
        value.trim() != value ||
        value.contains('\u0000')) {
      throw StateError('Invalid interview preparation idempotency identity.');
    }
    return value;
  }

  void _handleWorkspaceState() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _epoch++;
    workspaceController.removeListener(_handleWorkspaceState);
    super.dispose();
  }
}

bool _validInput(InterviewPreparationInput input) {
  bool valid(String? value, int maximumBytes) =>
      value == null ||
      (value.isNotEmpty &&
          value.trim() == value &&
          !value.contains('\u0000') &&
          utf8.encode(value).length <= maximumBytes);

  if (!valid(input.jobTitle, 512) ||
      !valid(input.jobDescription, 64 * 1024) ||
      !valid(input.company, 512) ||
      !valid(input.seniority, 256) ||
      !valid(input.candidateBackground, 16 * 1024) ||
      !valid(input.practiceFocus, 8 * 1024)) {
    return false;
  }
  return switch (input.source) {
    InterviewPreparationSource.jobDescription => input.jobDescription != null,
    InterviewPreparationSource.quickStart =>
      input.jobTitle != null && input.jobDescription == null,
  };
}

bool _sameResume(InterviewResumeFile? left, InterviewResumeFile? right) =>
    identical(left, right) ||
    (left != null &&
        right != null &&
        left.name == right.name &&
        listEquals(left.bytes, right.bytes));

bool _validUUID(String value) => _uuidV4.hasMatch(value);

String _messageFor(JobPreparationException error) => switch (error.kind) {
  JobPreparationFailureKind.authenticationRequired => '登录状态已失效，请重新登录后继续。',
  JobPreparationFailureKind.invalidRequest => '当前信息无法提交，请检查填写内容后重试。',
  JobPreparationFailureKind.notFound => '这份准备卡片已不存在，请重新开始。',
  JobPreparationFailureKind.conflict => '服务端版本已变化，请重新打开后继续。',
  JobPreparationFailureKind.network => '网络连接不稳定，本次进度已保留，可以重试。',
  JobPreparationFailureKind.server => '准备服务暂时不可用，本次进度已保留，可以重试。',
  JobPreparationFailureKind.invalidResponse => '准备服务返回了无法识别的数据，请稍后重试。',
  JobPreparationFailureKind.superseded => '',
};

final RegExp _uuidV4 = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);
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
