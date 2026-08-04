import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';

typedef AgentPracticeContextProvider = AgentPracticeContext? Function();
typedef AgentThreadIdProvider = String? Function();
typedef PreparationGoalActivator =
    Future<AgentPracticeContext> Function({
      required String threadId,
      required PreparationLaunchSelection selection,
      required String clientOperationId,
    });
typedef PreparationLaunchIdFactory = String Function(String scope);
typedef VoicePracticeActivator =
    Future<void> Function({
      required AgentPracticeContext context,
      required SceneDefinition scene,
      required PreparationPracticeBootstrap bootstrap,
      required String clientOperationId,
    });

final class PreparationLaunchController extends ChangeNotifier {
  PreparationLaunchController({
    required this.client,
    required this.contextProvider,
    required this.threadIdProvider,
    required this.goalActivator,
    required this.voiceActivator,
    this.workspaceController,
    PreparationLaunchIdFactory? idFactory,
  }) : _idFactory = idFactory ?? _secureLaunchId {
    workspaceController?.addListener(_handleWorkspaceState);
  }

  final PreparationLaunchClient client;
  final AgentPracticeContextProvider contextProvider;
  final AgentThreadIdProvider threadIdProvider;
  final PreparationGoalActivator goalActivator;
  final VoicePracticeActivator voiceActivator;
  final PracticeWorkspaceController? workspaceController;
  final PreparationLaunchIdFactory _idFactory;

  String _backgroundSummary = '';
  String? _errorMessage;
  PreparationLaunchStage? _stage;
  PreparationPracticeBootstrap? _bootstrap;
  _LaunchAttempt? _retry;
  int _epoch = 0;
  bool _starting = false;
  bool _commitMayHaveSucceeded = false;
  bool _disposed = false;

  String get backgroundSummary => _backgroundSummary;
  String? get errorMessage => _errorMessage;
  PreparationLaunchStage? get stage => _stage;
  PreparationPracticeBootstrap? get bootstrap => _bootstrap;
  bool get isStarting => _starting || (workspaceController?.isBusy ?? false);
  bool get isSelectionLocked =>
      isStarting ||
      (_retry != null &&
          (_bootstrap != null ||
              _commitMayHaveSucceeded ||
              (workspaceController != null && _retry!.threadId != null)));
  bool get canRetry => _retry != null && !isStarting;
  bool get hasValidBackground => _validBackground(_backgroundSummary.trim());
  bool get hasResumablePractice => workspaceController?.hasResumable ?? false;
  bool get resumableHasProgress =>
      workspaceController?.resumableHasProgress ?? false;
  String? get resumablePracticeTitle => workspaceController?.currentTitle;
  String? get resumableGoalId => workspaceController?.currentGoalId;
  String? get resumableSceneId => workspaceController?.currentSceneId;
  String? get resumableSceneFamily => workspaceController?.currentSceneFamily;
  ScenePresentationMode get resumablePresentationMode =>
      workspaceController?.currentPresentationMode ??
      ScenePresentationMode.standard;
  String? get resumableSessionId => workspaceController?.currentSessionId;
  String? get workspaceErrorMessage => workspaceController?.errorMessage;
  bool get canRetryWorkspaceActivation =>
      workspaceController?.canRetryActivation ?? false;

  Future<bool> resumeCurrentPractice() async {
    final workspace = workspaceController;
    return workspace == null || await workspace.resumeCurrentPractice();
  }

  Future<bool> parkCurrentPractice() async {
    final workspace = workspaceController;
    return workspace == null ||
        workspace.currentLease == null ||
        await workspace.parkCurrentPractice();
  }

  Future<bool> completeAndContinueWithAgent() async {
    final workspace = workspaceController;
    return workspace != null && await workspace.completeAndContinueWithAgent();
  }

  Future<void> activateAccount(String accountId) async {
    await workspaceController?.activateAccount(accountId);
  }

  Future<void> retryWorkspaceActivation() async {
    await workspaceController?.retryActivation();
  }

  void updateBackgroundSummary(String value) {
    if (_disposed || isSelectionLocked || value == _backgroundSummary) {
      return;
    }
    _backgroundSummary = value;
    _invalidateAttempt();
  }

  void selectionChanged() {
    if (_disposed || isSelectionLocked) {
      return;
    }
    _invalidateAttempt();
  }

  Future<bool> start(
    PreparationLaunchSelection selection, {
    bool replaceCurrentPractice = false,
  }) {
    if (_disposed || isStarting) {
      return Future<bool>.value(false);
    }
    final background = _backgroundSummary.trim();
    if (!_validBackground(background)) {
      _errorMessage = background.isEmpty
          ? '请先补充你的背景与本次练习目标。'
          : '背景内容过长，请精简后再开始练习。';
      _stage = PreparationLaunchStage.profile;
      if (!_hasCommittedOrAmbiguousCreate) {
        _retry = null;
      }
      notifyListeners();
      return Future<bool>.value(false);
    }
    final workspace = workspaceController;
    final threadId = workspace == null ? threadIdProvider() : null;
    if (workspace == null &&
        (threadId == null || !_validResourceId(threadId))) {
      _errorMessage = 'Agent 对话仍在恢复，请稍后再试。你的背景内容会保留在本机当前页面。';
      _stage = PreparationLaunchStage.context;
      if (!_hasCommittedOrAmbiguousCreate) {
        _retry = null;
      }
      notifyListeners();
      return Future<bool>.value(false);
    }
    final existing = _retry;
    if (isSelectionLocked &&
        (existing == null ||
            !existing.matches(
              selection: selection,
              backgroundSummary: background,
              threadId: threadId,
            ))) {
      _errorMessage = '练习已经创建，请先重试连接原练习。';
      _stage = PreparationLaunchStage.voice;
      notifyListeners();
      return Future<bool>.value(false);
    }
    final attempt =
        existing != null &&
            existing.matches(
              selection: selection,
              backgroundSummary: background,
              threadId: threadId,
            )
        ? existing
        : _LaunchAttempt(
            selection: selection,
            backgroundSummary: background,
            threadId: threadId,
            workspaceOperationId: _newId('practice-workspace'),
            workspaceLease: null,
            replaceCurrentPractice: replaceCurrentPractice,
            context: null,
            goalKey: _newId('agent-goal'),
            profileKey: _newId('prep-profile'),
            snapshotKey: _newId('prep-snapshot'),
            planKey: _newId('practice-plan'),
            sessionKey: _newId('practice-session'),
            voiceKey: _newId('practice-voice'),
          );
    return _run(attempt);
  }

  Future<bool> retry() {
    final attempt = _retry;
    if (_disposed || isStarting || attempt == null) {
      return Future<bool>.value(false);
    }
    if (workspaceController != null) {
      return _run(attempt);
    }
    final threadId = threadIdProvider();
    if (threadId == null || !_validResourceId(threadId)) {
      _errorMessage = 'Agent 对话仍在恢复，请稍后再试。你的背景内容会继续保留。';
      _stage = PreparationLaunchStage.context;
      notifyListeners();
      return Future<bool>.value(false);
    }
    if (attempt.threadId != threadId) {
      _errorMessage = '当前 Agent 对话已变化，请重新确认后开始练习。';
      _stage = PreparationLaunchStage.context;
      if (!_hasCommittedOrAmbiguousCreate) {
        _retry = null;
      }
      notifyListeners();
      return Future<bool>.value(false);
    }
    return _run(attempt);
  }

  Future<bool> _run(_LaunchAttempt attempt) async {
    final operationEpoch = _epoch;
    final workspace = workspaceController;
    var launchSucceeded = false;
    final preserveCommittedState =
        identical(_retry, attempt) && _hasCommittedOrAmbiguousCreate;
    _starting = true;
    _errorMessage = null;
    if (!preserveCommittedState) {
      _bootstrap = null;
      _commitMayHaveSucceeded = false;
    }
    _retry = attempt;
    notifyListeners();
    try {
      var activeAttempt = attempt;
      if (workspace != null) {
        _stage = PreparationLaunchStage.context;
        notifyListeners();
        final lease = attempt.threadId == null && attempt.replaceCurrentPractice
            ? await workspace.replaceCurrentPractice(
                attempt.workspaceOperationId,
              )
            : await workspace.acquireThread(attempt.workspaceOperationId);
        if (lease == null ||
            (attempt.threadId != null &&
                attempt.threadId != lease.practiceThreadId)) {
          throw const PreparationLaunchException(
            kind: PreparationLaunchFailureKind.contextChanged,
            stage: PreparationLaunchStage.context,
            retryable: true,
          );
        }
        activeAttempt = attempt.withWorkspaceLease(lease);
        _retry = activeAttempt;
      }
      final threadId = activeAttempt.threadId;
      if (threadId == null || !_validResourceId(threadId)) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.contextMissing,
          stage: PreparationLaunchStage.context,
          retryable: true,
        );
      }
      _requireThreadCurrent(operationEpoch, threadId);
      _stage = PreparationLaunchStage.goal;
      notifyListeners();
      final activeContext = await goalActivator(
        threadId: threadId,
        selection: activeAttempt.selection,
        clientOperationId: activeAttempt.goalKey,
      );
      if (!_validContext(activeContext) || activeContext.threadId != threadId) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidResponse,
          stage: PreparationLaunchStage.goal,
        );
      }
      activeAttempt = activeAttempt.withContext(activeContext);
      _retry = activeAttempt;
      _requireCurrent(operationEpoch, activeContext);

      _stage = PreparationLaunchStage.profile;
      notifyListeners();
      final profile = await client.createProfile(
        input: CreatePreparationProfileInput(
          backgroundSummary: activeAttempt.backgroundSummary,
        ),
        idempotencyKey: activeAttempt.profileKey,
      );
      _requireCurrent(operationEpoch, activeContext);

      _stage = PreparationLaunchStage.snapshot;
      notifyListeners();
      final snapshot = await client.createSnapshot(
        profileId: profile.id,
        sourceVersion: profile.version,
        idempotencyKey: activeAttempt.snapshotKey,
      );
      _requireCurrent(operationEpoch, activeContext);

      _stage = PreparationLaunchStage.plan;
      notifyListeners();
      final plan = await client.createPlan(
        input: CreatePreparationPlanInput(
          sourceThreadId: activeContext.threadId,
          goalId: activeContext.goalId,
          preparationSnapshotId: snapshot.id,
          sceneId: activeAttempt.selection.scene.id,
          sceneVersion: activeAttempt.selection.scene.version,
          selectedRoleIds: activeAttempt.selection.selectedRoleIds,
          practiceOptionId: activeAttempt.selection.practiceOptionId,
          ieltsSelection: activeAttempt.selection.ieltsSelection,
        ),
        idempotencyKey: activeAttempt.planKey,
      );
      _requireCurrent(operationEpoch, activeContext);

      _stage = PreparationLaunchStage.session;
      notifyListeners();
      final bootstrap = await client.createSession(
        plan: plan,
        input: CreatePreparationSessionInput(
          expectedPlanRevision: plan.revision,
          userConfirmed: true,
        ),
        idempotencyKey: activeAttempt.sessionKey,
      );
      _requireCurrent(operationEpoch, activeContext);
      _bootstrap = bootstrap;

      final lease = activeAttempt.workspaceLease;
      if (workspace != null && lease != null) {
        final committed = await workspace.commitSession(
          lease: lease,
          goalId: activeContext.goalId,
          sessionId: bootstrap.session.id,
          scene: activeAttempt.selection.scene,
        );
        if (!committed) {
          throw const PreparationLaunchException(
            kind: PreparationLaunchFailureKind.network,
            stage: PreparationLaunchStage.session,
            retryable: true,
          );
        }
        _requireCurrent(operationEpoch, activeContext);
      }

      _stage = PreparationLaunchStage.voice;
      notifyListeners();
      await voiceActivator(
        context: activeContext,
        scene: activeAttempt.selection.scene,
        bootstrap: bootstrap,
        clientOperationId: activeAttempt.voiceKey,
      );
      _requireCurrent(operationEpoch, activeContext);

      _retry = null;
      _commitMayHaveSucceeded = false;
      _errorMessage = null;
      launchSucceeded = true;
      return true;
    } on PreparationLaunchException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _stage = error.stage ?? _stage;
        _errorMessage = workspaceController?.errorMessage ?? _messageFor(error);
        if (error.kind == PreparationLaunchFailureKind.invalidResponse &&
            error.statusCode == 201) {
          _commitMayHaveSucceeded = true;
        }
        if (!error.retryable &&
            error.kind != PreparationLaunchFailureKind.contextChanged &&
            !_hasCommittedOrAmbiguousCreate) {
          _retry = null;
        }
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage =
            workspaceController?.errorMessage ??
            (_stage == PreparationLaunchStage.voice
                ? '练习已创建，但语音题目暂时无法连接。请重试连接。'
                : '暂时无法开始练习，请稍后重试。');
      }
      return false;
    } finally {
      if (!launchSucceeded &&
          _isCurrent(operationEpoch) &&
          workspace != null &&
          workspace.currentLease?.operationId == attempt.workspaceOperationId) {
        final parked = await workspace.parkCurrentPractice();
        if (!parked && _isCurrent(operationEpoch)) {
          _errorMessage ??= workspace.errorMessage ?? '练习准备已暂停，但暂时无法返回首页。';
        }
      }
      if (_isCurrent(operationEpoch)) {
        _starting = false;
        notifyListeners();
      }
    }
  }

  Future<void> clearPrivateState() async {
    _epoch++;
    _backgroundSummary = '';
    _errorMessage = null;
    _stage = null;
    _bootstrap = null;
    _retry = null;
    _commitMayHaveSucceeded = false;
    _starting = false;
    await Future.wait<void>([
      client.clearAccountState(),
      if (workspaceController case final workspace?)
        workspace.clearPrivateState(),
    ]);
    if (!_disposed) {
      notifyListeners();
    }
  }

  void _invalidateAttempt() {
    _epoch++;
    _errorMessage = null;
    _stage = null;
    _bootstrap = null;
    _retry = null;
    _commitMayHaveSucceeded = false;
    _starting = false;
    notifyListeners();
  }

  void _requireCurrent(int epoch, AgentPracticeContext context) {
    if (!_isCurrent(epoch)) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.superseded,
      );
    }
    if (contextProvider() != context) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.contextChanged,
        stage: PreparationLaunchStage.context,
        retryable: true,
      );
    }
  }

  void _requireThreadCurrent(int epoch, String threadId) {
    if (!_isCurrent(epoch)) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.superseded,
      );
    }
    if (threadIdProvider() != threadId) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.contextChanged,
        stage: PreparationLaunchStage.context,
        retryable: true,
      );
    }
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _epoch;

  bool get _hasCommittedOrAmbiguousCreate =>
      _bootstrap != null || _commitMayHaveSucceeded;

  String _newId(String scope) {
    final value = _idFactory(scope);
    if (value.length < 8 || value.length > 128 || value.trim() != value) {
      throw StateError('Invalid launch idempotency identity.');
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

  void _handleWorkspaceState() {
    if (!_disposed) {
      notifyListeners();
    }
  }
}

final class _LaunchAttempt {
  const _LaunchAttempt({
    required this.selection,
    required this.backgroundSummary,
    required this.threadId,
    required this.workspaceOperationId,
    required this.workspaceLease,
    required this.replaceCurrentPractice,
    required this.context,
    required this.goalKey,
    required this.profileKey,
    required this.snapshotKey,
    required this.planKey,
    required this.sessionKey,
    required this.voiceKey,
  });

  final PreparationLaunchSelection selection;
  final String backgroundSummary;
  final String? threadId;
  final String workspaceOperationId;
  final PracticeWorkspaceLease? workspaceLease;
  final bool replaceCurrentPractice;
  final AgentPracticeContext? context;
  final String goalKey;
  final String profileKey;
  final String snapshotKey;
  final String planKey;
  final String sessionKey;
  final String voiceKey;

  bool matches({
    required PreparationLaunchSelection selection,
    required String backgroundSummary,
    required String? threadId,
  }) =>
      this.selection == selection &&
      this.backgroundSummary == backgroundSummary &&
      (this.threadId == threadId || threadId == null);

  _LaunchAttempt withWorkspaceLease(PracticeWorkspaceLease value) {
    return _LaunchAttempt(
      selection: selection,
      backgroundSummary: backgroundSummary,
      threadId: value.practiceThreadId,
      workspaceOperationId: workspaceOperationId,
      workspaceLease: value,
      replaceCurrentPractice: replaceCurrentPractice,
      context: context,
      goalKey: goalKey,
      profileKey: profileKey,
      snapshotKey: snapshotKey,
      planKey: planKey,
      sessionKey: sessionKey,
      voiceKey: voiceKey,
    );
  }

  _LaunchAttempt withContext(AgentPracticeContext value) {
    return _LaunchAttempt(
      selection: selection,
      backgroundSummary: backgroundSummary,
      threadId: threadId,
      workspaceOperationId: workspaceOperationId,
      workspaceLease: workspaceLease,
      replaceCurrentPractice: replaceCurrentPractice,
      context: value,
      goalKey: goalKey,
      profileKey: profileKey,
      snapshotKey: snapshotKey,
      planKey: planKey,
      sessionKey: sessionKey,
      voiceKey: voiceKey,
    );
  }
}

bool _validContext(AgentPracticeContext? context) =>
    context != null &&
    _validResourceId(context.threadId) &&
    _validResourceId(context.goalId);

bool _validResourceId(String value) =>
    value.isNotEmpty &&
    value.length <= 128 &&
    value.trim() == value &&
    !value.contains('\u0000');

bool _validBackground(String value) =>
    value.isNotEmpty &&
    value.trim() == value &&
    !value.contains('\u0000') &&
    utf8.encode(value).length <= 64 * 1024;

String _messageFor(PreparationLaunchException error) {
  return switch (error.kind) {
    PreparationLaunchFailureKind.contextMissing => 'Agent 对话仍在恢复，请稍后在当前页面重试。',
    PreparationLaunchFailureKind.contextChanged => '当前 Agent 事项已变化，请重新确认后重试。',
    PreparationLaunchFailureKind.authenticationRequired => '登录状态已失效，请重新登录后继续。',
    PreparationLaunchFailureKind.invalidRequest =>
      '当前练习配置无法提交，请重新确认背景、视角和练习方式。',
    PreparationLaunchFailureKind.notFound => '练习所需的目录或 Agent 事项已变化，请返回场景页重新选择。',
    PreparationLaunchFailureKind.conflict =>
      '当前 Agent 对话已有练习，或配置版本已变化，请先处理现有练习后重试。',
    PreparationLaunchFailureKind.network => '网络连接不稳定，本次进度已保留，可以重试。',
    PreparationLaunchFailureKind.server =>
      error.stage == PreparationLaunchStage.voice
          ? '练习已创建，但语音题目暂时无法连接。请重试连接。'
          : '练习服务暂时不可用，本次进度已保留，可以重试。',
    PreparationLaunchFailureKind.invalidResponse => '练习服务返回了无法识别的数据，请稍后重试。',
    PreparationLaunchFailureKind.superseded => '',
  };
}

final Random _launchRandom = Random.secure();

String _secureLaunchId(String scope) {
  final value = StringBuffer('${scope}_');
  for (var index = 0; index < 16; index++) {
    value.write(_launchRandom.nextInt(256).toRadixString(16).padLeft(2, '0'));
  }
  return value.toString();
}
