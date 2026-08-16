import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';

typedef PreparationLaunchIdFactory = String Function(String scope);
typedef VoicePracticeActivator =
    Future<void> Function({
      required SceneDefinition scene,
      required PreparationPracticeBootstrap bootstrap,
      required String clientOperationId,
    });

final class PreparationLaunchController extends ChangeNotifier {
  PreparationLaunchController({
    required this.client,
    required this.voiceActivator,
    required this.workspaceController,
    PreparationLaunchIdFactory? idFactory,
  }) : _idFactory = idFactory ?? _secureLaunchId {
    workspaceController.addListener(_handleWorkspaceState);
  }

  final PreparationLaunchClient client;
  final VoicePracticeActivator voiceActivator;
  final PracticeWorkspaceController workspaceController;
  final PreparationLaunchIdFactory _idFactory;

  String _backgroundSummary = '';
  String? _errorMessage;
  PreparationLaunchStage? _stage;
  PreparationPracticeBootstrap? _bootstrap;
  _LaunchAttempt? _retry;
  int _epoch = 0;
  bool _starting = false;
  bool _commitMayHaveSucceeded = false;
  bool _failedAttemptSafelyParked = false;
  bool _disposed = false;

  String get backgroundSummary => _backgroundSummary;
  String? get errorMessage => _errorMessage;
  PreparationLaunchStage? get stage => _stage;
  PreparationPracticeBootstrap? get bootstrap => _bootstrap;
  bool get isStarting => _starting || workspaceController.isBusy;
  bool get isSelectionLocked =>
      isStarting ||
      (_retry != null &&
          (_bootstrap != null ||
              _commitMayHaveSucceeded ||
              workspaceController.currentLease != null));
  bool get isNavigationLocked =>
      isStarting || (isSelectionLocked && !_failedAttemptSafelyParked);
  bool get canRetry => _retry != null && !isStarting;
  bool get canDismissFailedLaunch =>
      !isStarting && _retry != null && _errorMessage != null;
  bool get hasValidBackground => _validBackground(_backgroundSummary.trim());
  bool get hasResumablePractice => workspaceController.hasResumable;
  bool get resumableHasProgress => workspaceController.resumableHasProgress;
  String? get resumablePracticeTitle => workspaceController.currentTitle;
  String? get resumableSceneId => workspaceController.currentSceneId;
  String? get resumablePracticeExperience =>
      workspaceController.currentPracticeExperience;
  String? get resumableSessionId => workspaceController.currentSessionId;
  String? get workspaceErrorMessage => workspaceController.errorMessage;
  bool get canRetryWorkspaceActivation =>
      workspaceController.canRetryActivation;

  Future<bool> resumeCurrentPractice() async {
    return workspaceController.resumeCurrentPractice();
  }

  Future<bool> parkCurrentPractice() async {
    return workspaceController.currentLease == null ||
        await workspaceController.parkCurrentPractice();
  }

  /// Cancels a preparation launch that has not produced a usable practice.
  ///
  /// The launch epoch is invalidated first so late profile/plan/session
  /// responses cannot restore the preparation screen after the user leaves.
  /// An in-flight launch parks its workspace from the launcher's finally block
  /// once the current workspace operation has completed.
  Future<bool> cancelCurrentPreparation() async {
    if (_disposed) {
      return false;
    }
    final wasStarting = isStarting;
    _invalidateAttempt();
    if (wasStarting) {
      return true;
    }
    if (workspaceController.currentLease == null) {
      return true;
    }
    return workspaceController.parkCurrentPractice();
  }

  Future<void> activateAccount(String accountId) async {
    await workspaceController.activateAccount(accountId);
  }

  Future<void> retryWorkspaceActivation() async {
    await workspaceController.retryActivation();
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
    ScenarioPreparationContext? scenarioContext,
  }) {
    if (_disposed || isStarting) {
      return Future<bool>.value(false);
    }
    final background = scenarioContext == null
        ? _backgroundSummary.trim()
        : _scenarioBackground(scenarioContext);
    if (scenarioContext != null && !_validScenarioContext(scenarioContext)) {
      _errorMessage = '请完整填写本次场景信息后再开始练习。';
      _stage = PreparationLaunchStage.context;
      if (!_hasCommittedOrAmbiguousCreate) {
        _retry = null;
      }
      notifyListeners();
      return Future<bool>.value(false);
    }
    if (!_validBackground(background)) {
      _errorMessage = background.isEmpty
          ? '请先补充你的背景与本次练习目标。'
          : '背景内容过长，请精简后再开始练习。';
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
              scenarioContext: scenarioContext,
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
              scenarioContext: scenarioContext,
            )
        ? existing
        : _LaunchAttempt(
            selection: selection,
            backgroundSummary: background,
            scenarioContext: scenarioContext,
            workspaceOperationId: _newId('practice-workspace'),
            replaceCurrentPractice: replaceCurrentPractice,
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
    return _run(attempt);
  }

  bool prepareFailedAttemptForNavigation() {
    if (_disposed || isNavigationLocked) {
      return false;
    }
    if (_retry == null) {
      return true;
    }
    final retryCanBeReplaced =
        !_commitMayHaveSucceeded &&
        (_bootstrap == null || hasResumablePractice);
    if (retryCanBeReplaced) {
      _invalidateAttempt();
    }
    return true;
  }

  Future<bool> dismissFailedLaunch() async {
    if (_disposed || !canDismissFailedLaunch) {
      return false;
    }
    if (workspaceController.currentLease != null) {
      final parked = await workspaceController.parkCurrentPractice();
      if (!parked) {
        _errorMessage = workspaceController.errorMessage ?? '暂时无法安全退出当前练习，请重试。';
        notifyListeners();
        return false;
      }
    }
    _invalidateAttempt();
    return true;
  }

  Future<bool> _run(_LaunchAttempt attempt) async {
    final operationEpoch = _epoch;
    var launchSucceeded = false;
    final preserveCommittedState =
        identical(_retry, attempt) && _hasCommittedOrAmbiguousCreate;
    _starting = true;
    _failedAttemptSafelyParked = false;
    _errorMessage = null;
    if (!preserveCommittedState) {
      _bootstrap = null;
      _commitMayHaveSucceeded = false;
    }
    _retry = attempt;
    notifyListeners();
    try {
      _stage = PreparationLaunchStage.context;
      notifyListeners();
      final lease = attempt.replaceCurrentPractice
          ? await workspaceController.replaceCurrentPractice(
              attempt.workspaceOperationId,
            )
          : await workspaceController.acquirePractice(
              attempt.workspaceOperationId,
            );
      if (lease == null) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.contextChanged,
          stage: PreparationLaunchStage.context,
          retryable: true,
        );
      }
      _retry = attempt;
      _requireCurrent(operationEpoch);

      _stage = PreparationLaunchStage.plan;
      notifyListeners();
      final plan = await client.createPlan(
        input: CreatePracticePlanInput(
          backgroundSummary: attempt.backgroundSummary,
          sceneId: attempt.selection.scene.id,
          sceneVersion: attempt.selection.scene.version,
          selectedRoleIds: attempt.selection.selectedRoleIds,
          practiceOptionId: attempt.selection.practiceOptionId,
          ieltsSelection: attempt.selection.ieltsSelection,
          ieltsPreparedAnswers: attempt.selection.ieltsPreparedAnswers,
        ),
        idempotencyKey: attempt.planKey,
      );
      _requireCurrent(operationEpoch);

      if (plan.status != PracticePlanStatus.ready) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidResponse,
          stage: PreparationLaunchStage.plan,
        );
      }

      _stage = PreparationLaunchStage.session;
      notifyListeners();
      final bootstrap = await client.createSession(
        plan: plan,
        input: CreatePreparationSessionInput(expectedPlanVersion: plan.version),
        idempotencyKey: attempt.sessionKey,
      );
      _requireCurrent(operationEpoch);
      _bootstrap = bootstrap;

      final committed = await workspaceController.commitSession(
        lease: lease,
        planId: bootstrap.session.planId,
        sessionId: bootstrap.session.id,
        scene: attempt.selection.scene,
      );
      if (!committed) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.network,
          stage: PreparationLaunchStage.session,
          retryable: true,
        );
      }
      _requireCurrent(operationEpoch);

      _stage = PreparationLaunchStage.voice;
      notifyListeners();
      await voiceActivator(
        scene: attempt.selection.scene,
        bootstrap: bootstrap,
        clientOperationId: attempt.voiceKey,
      );
      _requireCurrent(operationEpoch);

      _retry = null;
      _commitMayHaveSucceeded = false;
      _errorMessage = null;
      launchSucceeded = true;
      return true;
    } on PreparationLaunchException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _stage = error.stage ?? _stage;
        _errorMessage = workspaceController.errorMessage ?? _messageFor(error);
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
            workspaceController.errorMessage ??
            (_stage == PreparationLaunchStage.voice
                ? '练习已创建，但语音题目暂时无法连接。请重试连接。'
                : '暂时无法开始练习，请稍后重试。');
      }
      return false;
    } finally {
      var safelyParked = true;
      if (!launchSucceeded &&
          workspaceController.currentLease?.operationId ==
              attempt.workspaceOperationId) {
        final parked = await workspaceController.parkCurrentPractice();
        safelyParked = parked;
        if (!parked && _isCurrent(operationEpoch)) {
          _errorMessage ??=
              workspaceController.errorMessage ?? '练习准备已暂停，但暂时无法返回首页。';
        }
      }
      if (_isCurrent(operationEpoch)) {
        _failedAttemptSafelyParked = !launchSucceeded && safelyParked;
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
    _failedAttemptSafelyParked = false;
    _starting = false;
    await Future.wait<void>([
      client.clearAccountState(),
      workspaceController.clearPrivateState(),
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
    _failedAttemptSafelyParked = false;
    _starting = false;
    notifyListeners();
  }

  void _requireCurrent(int epoch) {
    if (!_isCurrent(epoch)) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.superseded,
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
    workspaceController.removeListener(_handleWorkspaceState);
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
    required this.scenarioContext,
    required this.workspaceOperationId,
    required this.replaceCurrentPractice,
    required this.planKey,
    required this.sessionKey,
    required this.voiceKey,
  });

  final PreparationLaunchSelection selection;
  final String backgroundSummary;
  final ScenarioPreparationContext? scenarioContext;
  final String workspaceOperationId;
  final bool replaceCurrentPractice;
  final String planKey;
  final String sessionKey;
  final String voiceKey;

  bool matches({
    required PreparationLaunchSelection selection,
    required String backgroundSummary,
    required ScenarioPreparationContext? scenarioContext,
  }) =>
      this.selection == selection &&
      this.backgroundSummary == backgroundSummary &&
      this.scenarioContext == scenarioContext;
}

bool _validBackground(String value) =>
    value.isNotEmpty &&
    value.trim() == value &&
    !value.contains('\u0000') &&
    utf8.encode(value).length <= 64 * 1024;

bool _validScenarioContext(ScenarioPreparationContext context) =>
    <String>[
      context.situation,
      context.userRole,
      context.counterpartRole,
      context.goal,
      context.counterpartPersona,
    ].every(
      (value) =>
          value.isNotEmpty &&
          value.trim() == value &&
          !value.contains('\u0000') &&
          utf8.encode(value).length <= 16 * 1024,
    );

String _scenarioBackground(ScenarioPreparationContext context) =>
    '情境：${context.situation}\n'
    '我的角色：${context.userRole}\n'
    '对方角色：${context.counterpartRole}\n'
    '练习目标：${context.goal}\n'
    '对方设定：${context.counterpartPersona}';

String _messageFor(PreparationLaunchException error) {
  return switch (error.kind) {
    PreparationLaunchFailureKind.contextChanged => '当前练习记录已变化，请重新确认后重试。',
    PreparationLaunchFailureKind.authenticationRequired => '登录状态已失效，请重新登录后继续。',
    PreparationLaunchFailureKind.invalidRequest =>
      '当前练习配置无法提交，请重新确认背景、视角和练习方式。',
    PreparationLaunchFailureKind.notFound => '练习所需的目录或配置已变化，请返回场景页重新选择。',
    PreparationLaunchFailureKind.conflict => '当前已有练习，或配置版本已变化，请先处理现有练习后重试。',
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
