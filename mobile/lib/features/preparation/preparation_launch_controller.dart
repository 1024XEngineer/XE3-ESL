import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/preparation/preparation_launch_client.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';

typedef AgentPracticeContextProvider = AgentPracticeContext? Function();
typedef AgentThreadIdProvider = String? Function();
typedef PreparationMatterActivator =
    Future<AgentPracticeContext> Function({
      required String threadId,
      required PreparationLaunchSelection selection,
      required String clientOperationId,
    });
typedef PreparationLaunchIdFactory = String Function(String scope);
typedef VoicePracticeActivator =
    Future<void> Function({
      required AgentPracticeContext context,
      required PreparationPracticeBootstrap bootstrap,
      required String clientOperationId,
    });

final class PreparationLaunchController extends ChangeNotifier {
  PreparationLaunchController({
    required this.client,
    required this.contextProvider,
    required this.threadIdProvider,
    required this.matterActivator,
    required this.voiceActivator,
    PreparationLaunchIdFactory? idFactory,
  }) : _idFactory = idFactory ?? _secureLaunchId;

  final PreparationLaunchClient client;
  final AgentPracticeContextProvider contextProvider;
  final AgentThreadIdProvider threadIdProvider;
  final PreparationMatterActivator matterActivator;
  final VoicePracticeActivator voiceActivator;
  final PreparationLaunchIdFactory _idFactory;

  String _backgroundSummary = '';
  String? _errorMessage;
  PreparationLaunchStage? _stage;
  PreparationPracticeBootstrap? _bootstrap;
  _LaunchAttempt? _retry;
  int _epoch = 0;
  bool _starting = false;
  bool _disposed = false;

  String get backgroundSummary => _backgroundSummary;
  String? get errorMessage => _errorMessage;
  PreparationLaunchStage? get stage => _stage;
  PreparationPracticeBootstrap? get bootstrap => _bootstrap;
  bool get isStarting => _starting;
  bool get canRetry => _retry != null && !_starting;
  bool get hasValidBackground => _validBackground(_backgroundSummary.trim());

  void updateBackgroundSummary(String value) {
    if (_disposed || _starting || value == _backgroundSummary) {
      return;
    }
    _backgroundSummary = value;
    _invalidateAttempt();
  }

  void selectionChanged() {
    if (_disposed || _starting) {
      return;
    }
    _invalidateAttempt();
  }

  Future<bool> start(PreparationLaunchSelection selection) {
    if (_disposed || _starting) {
      return Future<bool>.value(false);
    }
    final background = _backgroundSummary.trim();
    if (!_validBackground(background)) {
      _errorMessage = background.isEmpty
          ? '请先补充你的背景与本次练习目标。'
          : '背景内容过长，请精简后再开始练习。';
      _stage = PreparationLaunchStage.profile;
      _retry = null;
      notifyListeners();
      return Future<bool>.value(false);
    }
    final threadId = threadIdProvider();
    if (threadId == null || !_validResourceId(threadId)) {
      _errorMessage = 'Agent 对话仍在恢复，请稍后再试。你的背景内容会保留在本机当前页面。';
      _stage = PreparationLaunchStage.context;
      _retry = null;
      notifyListeners();
      return Future<bool>.value(false);
    }
    final existing = _retry;
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
            context: null,
            matterKey: _newId('agent-matter'),
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
    if (_disposed || _starting || attempt == null) {
      return Future<bool>.value(false);
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
      _retry = null;
      notifyListeners();
      return Future<bool>.value(false);
    }
    return _run(attempt);
  }

  Future<bool> _run(_LaunchAttempt attempt) async {
    final operationEpoch = _epoch;
    _starting = true;
    _errorMessage = null;
    _bootstrap = null;
    _retry = attempt;
    notifyListeners();
    try {
      _requireThreadCurrent(operationEpoch, attempt.threadId);
      _stage = PreparationLaunchStage.matter;
      notifyListeners();
      final activeContext = await matterActivator(
        threadId: attempt.threadId,
        selection: attempt.selection,
        clientOperationId: attempt.matterKey,
      );
      if (!_validContext(activeContext) ||
          activeContext.threadId != attempt.threadId) {
        throw const PreparationLaunchException(
          kind: PreparationLaunchFailureKind.invalidResponse,
          stage: PreparationLaunchStage.matter,
        );
      }
      final activeAttempt = attempt.withContext(activeContext);
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
          context: activeContext,
          selection: activeAttempt.selection,
          preparationProfileId: profile.id,
          preparationUserId: profile.userId,
        ),
        idempotencyKey: activeAttempt.planKey,
      );
      _requireCurrent(operationEpoch, activeContext);

      _stage = PreparationLaunchStage.session;
      notifyListeners();
      final bootstrap = await client.createSession(
        planId: plan.id,
        input: CreatePreparationSessionInput(
          expectedPlanRevision: plan.revision,
          preparationSnapshotId: snapshot.id,
          preparationProfileId: profile.id,
          preparationProfileVersion: profile.version,
          preparationUserId: profile.userId,
          backgroundSummary: profile.backgroundSummary,
          selection: activeAttempt.selection,
        ),
        idempotencyKey: activeAttempt.sessionKey,
      );
      _requireCurrent(operationEpoch, activeContext);
      _bootstrap = bootstrap;

      _stage = PreparationLaunchStage.voice;
      notifyListeners();
      await voiceActivator(
        context: activeContext,
        bootstrap: bootstrap,
        clientOperationId: activeAttempt.voiceKey,
      );
      _requireCurrent(operationEpoch, activeContext);

      _retry = null;
      _errorMessage = null;
      return true;
    } on PreparationLaunchException catch (error) {
      if (_isCurrent(operationEpoch)) {
        _stage = error.stage ?? _stage;
        _errorMessage = _messageFor(error);
        if (!error.retryable &&
            error.kind != PreparationLaunchFailureKind.contextChanged) {
          _retry = null;
        }
      }
      return false;
    } on Object {
      if (_isCurrent(operationEpoch)) {
        _errorMessage = _stage == PreparationLaunchStage.voice
            ? '练习已创建，但语音题目暂时无法连接。请重试连接。'
            : '暂时无法开始练习，请稍后重试。';
      }
      return false;
    } finally {
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
    _starting = false;
    await client.clearAccountState();
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
    super.dispose();
  }
}

final class _LaunchAttempt {
  const _LaunchAttempt({
    required this.selection,
    required this.backgroundSummary,
    required this.threadId,
    required this.context,
    required this.matterKey,
    required this.profileKey,
    required this.snapshotKey,
    required this.planKey,
    required this.sessionKey,
    required this.voiceKey,
  });

  final PreparationLaunchSelection selection;
  final String backgroundSummary;
  final String threadId;
  final AgentPracticeContext? context;
  final String matterKey;
  final String profileKey;
  final String snapshotKey;
  final String planKey;
  final String sessionKey;
  final String voiceKey;

  bool matches({
    required PreparationLaunchSelection selection,
    required String backgroundSummary,
    required String threadId,
  }) =>
      this.selection == selection &&
      this.backgroundSummary == backgroundSummary &&
      this.threadId == threadId;

  _LaunchAttempt withContext(AgentPracticeContext value) {
    return _LaunchAttempt(
      selection: selection,
      backgroundSummary: backgroundSummary,
      threadId: threadId,
      context: value,
      matterKey: matterKey,
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
    _validResourceId(context.matterId);

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
