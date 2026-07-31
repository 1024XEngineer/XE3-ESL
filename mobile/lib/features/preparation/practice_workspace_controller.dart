import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/preparation/practice_launch_record_store.dart';

final class PracticeWorkspaceLease {
  const PracticeWorkspaceLease({
    required this.operationId,
    required this.practiceThreadId,
    required this.returnThreadId,
  });

  final String operationId;
  final String practiceThreadId;
  final String? returnThreadId;

  @override
  bool operator ==(Object other) {
    return other is PracticeWorkspaceLease &&
        other.operationId == operationId &&
        other.practiceThreadId == practiceThreadId &&
        other.returnThreadId == returnThreadId;
  }

  @override
  int get hashCode =>
      Object.hash(operationId, practiceThreadId, returnThreadId);
}

/// Owns the client-side boundary between the ordinary Agent workspace and one
/// resumable formal Practice Session.
///
/// The server remains authoritative for Thread, Matter, and Session state. This
/// controller only persists their opaque identities so navigation never has to
/// guess a recent Session or reuse the ordinary Agent Thread.
final class PracticeWorkspaceController extends ChangeNotifier {
  PracticeWorkspaceController({
    required this.agentController,
    required this.recordStore,
  });

  final AgentController agentController;
  final PracticeLaunchRecordStore recordStore;

  String? _accountId;
  String? _loadedAccountId;
  _StoredPracticeWorkspace? _current;
  String? _errorMessage;
  bool _busy = false;
  bool _disposed = false;
  int _accountGeneration = 0;
  int _operationGeneration = 0;
  Future<void> _writeTail = Future<void>.value();
  Completer<void>? _activeOperationDone;

  String? get currentTitle => _current?.scenarioTitle;
  String? get error => _errorMessage;
  String? get errorMessage => _errorMessage;
  bool get isBusy => _busy;
  bool get busy => _busy;
  bool get canRetryActivation =>
      !_disposed &&
      !_busy &&
      _accountId != null &&
      _loadedAccountId != _accountId;
  bool get hasResumable => _current?.isCommitted ?? false;
  PracticeWorkspaceLease? get currentLease => _current?.lease;
  String? get currentPracticeThreadId => _current?.practiceThreadId;
  String? get currentMatterId => _current?.matterId;
  String? get currentSessionId => _current?.sessionId;
  String? get currentScenarioId => _current?.scenarioId;
  String? get currentScenarioType => _current?.scenarioType;
  AgentScenePresentationMode get currentPresentationMode =>
      _current?.presentationMode ?? AgentScenePresentationMode.standard;

  Future<void> activateAccount(String accountId) async {
    await _activeOperationDone?.future;
    if (_disposed ||
        !_validOpaqueId(accountId) ||
        (_accountId == accountId && _loadedAccountId == accountId)) {
      return;
    }
    final accountGeneration = ++_accountGeneration;
    ++_operationGeneration;
    _accountId = accountId;
    _loadedAccountId = null;
    _current = null;
    _errorMessage = null;
    _busy = true;
    notifyListeners();
    var loaded = false;
    try {
      final encoded = await recordStore.read(accountId);
      if (!_isCurrentAccount(accountGeneration, accountId)) {
        return;
      }
      if (encoded == null) {
        await agentController.initialize();
        if (!_isCurrentAccount(accountGeneration, accountId)) {
          return;
        }
        if (!agentController.isInitialized) {
          _setErrorIfAbsent('Agent 对话仍在恢复，暂时无法核对上次练习。');
          return;
        }
        if (!agentController.hasActivePractice) {
          loaded = true;
        } else {
          final adopted = _adoptFocusedPractice(accountId);
          if (adopted == null) {
            return;
          }
          _current = adopted;
          notifyListeners();
          if (!await _persistCurrent(
            accountId: accountId,
            accountGeneration: accountGeneration,
            record: adopted,
          )) {
            return;
          }
          loaded = await _restoreHomeFocusAfterActivation(
            adopted,
            accountGeneration: accountGeneration,
          );
        }
      } else {
        final restored = _StoredPracticeWorkspace.tryDecode(
          encoded,
          expectedAccountId: accountId,
        );
        if (restored == null) {
          await agentController.initialize();
          if (!_isCurrentAccount(accountGeneration, accountId) ||
              !await _prepareToLeavePractice()) {
            return;
          }
          await agentController.clearFocusedThread();
          if (!_isCurrentAccount(accountGeneration, accountId) ||
              agentController.threadId != null) {
            _setErrorIfAbsent('本机练习记录已失效，但暂时无法重置首页状态。');
            return;
          }
          await _enqueueStoreWrite(() => recordStore.delete(accountId));
          if (_isCurrentAccount(accountGeneration, accountId)) {
            _errorMessage = '本机练习记录已失效，请重新开始练习。';
            loaded = true;
          }
        } else if (!restored.isCommitted) {
          final focusRestored = await _restoreHomeFocusAfterActivation(
            restored,
            accountGeneration: accountGeneration,
          );
          if (_isCurrentAccount(accountGeneration, accountId) &&
              focusRestored) {
            await _enqueueStoreWrite(() => recordStore.delete(accountId));
            loaded = true;
          }
        } else {
          _current = restored;
          loaded = await _restoreHomeFocusAfterActivation(
            restored,
            accountGeneration: accountGeneration,
          );
        }
      }
    } on Object {
      if (_isCurrentAccount(accountGeneration, accountId)) {
        _errorMessage = '暂时无法读取本机练习记录，请稍后重试。';
      }
    } finally {
      if (_isCurrentAccount(accountGeneration, accountId)) {
        _loadedAccountId = loaded ? accountId : null;
        _busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> retryActivation() async {
    final accountId = _accountId;
    if (accountId == null || !canRetryActivation) {
      return;
    }
    await activateAccount(accountId);
  }

  Future<PracticeWorkspaceLease?> acquireThread(String operationId) async {
    if (!_canStartOperation() || !_validOperationId(operationId)) {
      if (!_busy && _accountId != null && _loadedAccountId == _accountId) {
        _setError('无法创建练习空间，请重新进入训练页后重试。');
      }
      return null;
    }
    final accountId = _accountId!;
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      var current = _current;
      if (current != null) {
        if (current.operationId != operationId) {
          if (current.isCommitted) {
            _setError('已有练习尚未处理，请先继续或更换当前练习。');
            return null;
          }
          if (!await _prepareToLeavePractice()) {
            return null;
          }
          _current = null;
          final replacement = await _createLease(
            accountId: accountId,
            accountGeneration: accountGeneration,
            operationGeneration: operationGeneration,
            operationId: operationId,
            returnThreadId: current.returnThreadId,
            preparedToLeave: true,
          );
          if (replacement == null && _current == null) {
            _current = current;
            notifyListeners();
          }
          return replacement;
        }
        final latestReturnThreadId = agentController.threadId;
        if (!agentController.hasActivePractice &&
            latestReturnThreadId != current.practiceThreadId &&
            latestReturnThreadId != current.returnThreadId) {
          current = current.withReturnThreadId(latestReturnThreadId);
          _current = current;
          notifyListeners();
        }
        if (!await _focusThread(current.practiceThreadId)) {
          _setErrorIfAbsent('暂时无法恢复已创建的练习空间，请稍后重试。');
          return null;
        }
        if (!await _persistCurrent(
          accountId: accountId,
          accountGeneration: accountGeneration,
          record: current,
        )) {
          return null;
        }
        _errorMessage = null;
        return current.lease;
      }
      return await _createLease(
        accountId: accountId,
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
        operationId: operationId,
        returnThreadId: _safeCurrentReturnThreadId,
      );
    } finally {
      _finishOperation(
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
      );
    }
  }

  Future<bool> commitSession({
    required PracticeWorkspaceLease lease,
    required String matterId,
    required String sessionId,
    required String scenarioId,
    required String scenarioTitle,
    String? scenarioType,
    AgentScenePresentationMode presentationMode =
        AgentScenePresentationMode.standard,
  }) async {
    if (!_canStartOperation() ||
        !_validOpaqueId(matterId) ||
        !_validOpaqueId(sessionId) ||
        !_validOpaqueId(scenarioId) ||
        !_validTitle(scenarioTitle) ||
        !_validScenePresentation(scenarioType, presentationMode)) {
      if (!_busy) {
        _setError('练习记录不完整，暂时无法保存。');
      }
      return false;
    }
    final accountId = _accountId!;
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      var current = _current;
      if (current == null ||
          current.lease != lease ||
          agentController.threadId != lease.practiceThreadId ||
          agentController.activeMatter?.id != matterId) {
        _setError('练习空间已经变化，未保存本次练习。');
        return false;
      }
      final committed = current.commit(
        matterId: matterId,
        sessionId: sessionId,
        scenarioId: scenarioId,
        scenarioTitle: scenarioTitle,
        scenarioType: scenarioType,
        presentationMode: presentationMode,
      );
      _current = committed;
      notifyListeners();
      if (!await _persistCurrent(
        accountId: accountId,
        accountGeneration: accountGeneration,
        record: committed,
      )) {
        return false;
      }
      _errorMessage = null;
      return true;
    } finally {
      _finishOperation(
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
      );
    }
  }

  Future<bool> resumeCurrentPractice() async {
    if (!_canStartOperation()) {
      return false;
    }
    var current = _current;
    if (current == null || !current.isCommitted) {
      _setError('没有可以继续的练习。');
      return false;
    }
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      final latestReturnThreadId = agentController.threadId;
      if (!agentController.hasActivePractice &&
          latestReturnThreadId != current.practiceThreadId) {
        current = current.withReturnThreadId(latestReturnThreadId);
        _current = current;
        notifyListeners();
        if (!await _persistCurrent(
          accountId: current.accountId,
          accountGeneration: accountGeneration,
          record: current,
        )) {
          return false;
        }
      }
      final verification = await _resumeAndVerify(current);
      switch (verification) {
        case _PracticeResumeVerification.active:
          _errorMessage = null;
          return true;
        case _PracticeResumeVerification.terminal:
          final focusRestored = await _restoreReturnFocus(
            current,
            preparedToLeave: true,
            fallbackToEmpty: true,
          );
          if (!focusRestored) {
            _setErrorIfAbsent('上次练习已经结束，但暂时无法返回首页；练习记录仍已保留。');
            return false;
          }
          await _clearStoredRecord(
            current,
            accountGeneration: accountGeneration,
            operationGeneration: operationGeneration,
          );
          _setErrorIfAbsent('上次练习已经结束，可以开始新的练习。');
          return false;
        case _PracticeResumeVerification.mismatch:
          final focusRestored = await _restoreReturnFocus(
            current,
            preparedToLeave: true,
            fallbackToEmpty: true,
          );
          if (!focusRestored) {
            _setErrorIfAbsent('上次练习状态已经变化，但暂时无法返回首页；练习记录仍已保留。');
            return false;
          }
          await _clearStoredRecord(
            current,
            accountGeneration: accountGeneration,
            operationGeneration: operationGeneration,
          );
          _setErrorIfAbsent('上次练习的服务端状态已经变化，记录已清理，可以开始新的练习。');
          return false;
        case _PracticeResumeVerification.unavailable:
          _setErrorIfAbsent('无法核验上次练习，请稍后重试。');
          return false;
      }
    } finally {
      _finishOperation(
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
      );
    }
  }

  Future<bool> parkCurrentPractice() async {
    if (!_canStartOperation()) {
      return false;
    }
    final current = _current;
    if (current == null) {
      _setError('当前没有需要暂存的练习。');
      return false;
    }
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      final terminalPracticeWasFocused =
          current.isCommitted &&
          agentController.threadId == current.practiceThreadId &&
          agentController.practiceSessionId == current.sessionId &&
          agentController.activeMatter?.id == current.matterId &&
          !agentController.hasActivePractice;
      if (!await _prepareToLeavePractice()) {
        return false;
      }
      if (!await _restoreReturnFocus(
        current,
        preparedToLeave: true,
        fallbackToEmpty: true,
      )) {
        _setError('暂时无法返回原来的首页对话，请稍后重试。');
        return false;
      }
      if (terminalPracticeWasFocused) {
        _current = null;
        notifyListeners();
        try {
          await _enqueueStoreWrite(() => recordStore.delete(current.accountId));
        } on Object {
          if (_isCurrentOperation(
            accountGeneration,
            operationGeneration,
            current.accountId,
          )) {
            _setError('练习已完成，但本机记录清理失败。');
          }
          return false;
        }
      }
      _errorMessage = null;
      return true;
    } finally {
      _finishOperation(
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
      );
    }
  }

  Future<bool> completeAndContinueWithAgent() async {
    final current = _current;
    if (current == null ||
        !current.isCommitted ||
        current.returnThreadId == null ||
        agentController.threadId != current.practiceThreadId ||
        agentController.practiceSessionId != current.sessionId ||
        agentController.recordingState != PracticeRecordingState.completed) {
      _setError('练习尚未完整结束，暂时无法回到 Agent 复盘。');
      return false;
    }
    final title = current.scenarioTitle!;
    final sessionId = current.sessionId!;
    final completedTurns = agentController.completedTurns;
    if (!await parkCurrentPractice()) {
      return false;
    }
    final sent = await agentController.sendText(
      '我刚完成了“$title”的 $completedTurns 轮练习，练习记录 ID 是 $sessionId。'
      '请读取这次练习的真实评分与报告，先概括我的主要表现，再问我想重点复盘哪一部分。',
    );
    if (!sent) {
      _setError('已回到原会话，但暂时无法把练习结果发送给 Agent。');
      return false;
    }
    _errorMessage = null;
    return true;
  }

  Future<PracticeWorkspaceLease?> replaceCurrentPractice(
    String operationId,
  ) async {
    if (!_canStartOperation() || !_validOperationId(operationId)) {
      return null;
    }
    final accountId = _accountId!;
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      var current = _current;
      if (!await _prepareToLeavePractice()) {
        return null;
      }
      if (current == null) {
        return await _createLease(
          accountId: accountId,
          accountGeneration: accountGeneration,
          operationGeneration: operationGeneration,
          operationId: operationId,
          returnThreadId: _safeCurrentReturnThreadId,
          preparedToLeave: true,
        );
      }
      if (!current.isCommitted) {
        if (current.operationId == operationId) {
          if (!await _focusThread(
                current.practiceThreadId,
                preparedToLeave: true,
              ) ||
              !await _persistCurrent(
                accountId: accountId,
                accountGeneration: accountGeneration,
                record: current,
              )) {
            _setErrorIfAbsent('暂时无法恢复已创建的练习空间，请稍后重试。');
            return null;
          }
          _errorMessage = null;
          return current.lease;
        }
        _current = null;
        final replacement = await _createLease(
          accountId: accountId,
          accountGeneration: accountGeneration,
          operationGeneration: operationGeneration,
          operationId: operationId,
          returnThreadId: current.returnThreadId,
          preparedToLeave: true,
        );
        if (replacement == null && _current == null) {
          _current = current;
          notifyListeners();
        }
        return replacement;
      }
      final latestReturnThreadId = agentController.threadId;
      if (!agentController.hasActivePractice &&
          latestReturnThreadId != current.practiceThreadId) {
        current = current.withReturnThreadId(latestReturnThreadId);
        _current = current;
        notifyListeners();
        if (!await _persistCurrent(
          accountId: accountId,
          accountGeneration: accountGeneration,
          record: current,
        )) {
          return null;
        }
      }
      final verification = await _resumeAndVerify(
        current,
        preparedToLeave: true,
      );
      if (verification == _PracticeResumeVerification.mismatch) {
        if (!await _restoreReturnFocus(
          current,
          preparedToLeave: true,
          fallbackToEmpty: true,
        )) {
          _setErrorIfAbsent('当前练习状态已经变化，但暂时无法返回首页。');
          return null;
        }
        if (!await _clearStoredRecord(
          current,
          accountGeneration: accountGeneration,
          operationGeneration: operationGeneration,
        )) {
          return null;
        }
        return await _createLease(
          accountId: accountId,
          accountGeneration: accountGeneration,
          operationGeneration: operationGeneration,
          operationId: operationId,
          returnThreadId: current.returnThreadId,
          preparedToLeave: true,
        );
      }
      if (verification == _PracticeResumeVerification.unavailable) {
        _setErrorIfAbsent('无法核验当前练习，因此没有创建新的练习。');
        return null;
      }
      if (verification == _PracticeResumeVerification.active) {
        final ended = await agentController.endActivePracticeEarly();
        if (!ended || agentController.hasActivePractice) {
          final focusRestored = await _restoreReturnFocus(
            current,
            preparedToLeave: true,
            fallbackToEmpty: true,
          );
          _setError(
            focusRestored ? '暂时无法结束当前练习，进度仍已保留。' : '暂时无法结束当前练习，也无法返回首页，请稍后重试。',
          );
          return null;
        }
      }
      if (!await _restoreReturnFocus(
        current,
        preparedToLeave: true,
        fallbackToEmpty: true,
      )) {
        _setError('当前练习已结束，但暂时无法返回首页。');
        return null;
      }
      _current = null;
      notifyListeners();
      try {
        await _enqueueStoreWrite(() => recordStore.delete(accountId));
      } on Object {
        if (_isCurrentAccount(accountGeneration, accountId)) {
          _setError('当前练习已结束，但本机记录清理失败，请稍后重试。');
        }
        return null;
      }
      if (!_isCurrentOperation(
        accountGeneration,
        operationGeneration,
        accountId,
      )) {
        return null;
      }
      return await _createLease(
        accountId: accountId,
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
        operationId: operationId,
        returnThreadId: current.returnThreadId,
        preparedToLeave: true,
      );
    } finally {
      _finishOperation(
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
      );
    }
  }

  Future<void> clearPrivateState() async {
    final activeOperation = _activeOperationDone?.future;
    final accountId = _accountId;
    ++_accountGeneration;
    ++_operationGeneration;
    _accountId = null;
    _loadedAccountId = null;
    _current = null;
    _errorMessage = null;
    _busy = false;
    if (!_disposed) {
      notifyListeners();
    }
    await activeOperation;
    await _writeTail;
    if (accountId != null) {
      await recordStore.delete(accountId);
    }
  }

  Future<bool> _restoreHomeFocusAfterActivation(
    _StoredPracticeWorkspace record, {
    required int accountGeneration,
  }) async {
    await agentController.initialize();
    if (!_isCurrentAccount(accountGeneration, record.accountId)) {
      return false;
    }
    if (agentController.threadId != record.practiceThreadId &&
        !agentController.hasActivePractice) {
      return true;
    }
    if (!await _prepareToLeavePractice() ||
        !_isCurrentAccount(accountGeneration, record.accountId)) {
      return false;
    }
    var restored = false;
    final returnThreadId = record.returnThreadId;
    if (returnThreadId == null) {
      await agentController.clearFocusedThread();
      restored = agentController.threadId == null;
    } else {
      restored = await agentController.selectThread(returnThreadId);
      restored =
          restored &&
          agentController.threadId == returnThreadId &&
          !agentController.hasActivePractice;
    }
    if (!_isCurrentAccount(accountGeneration, record.accountId)) {
      return false;
    }
    if (!restored) {
      await agentController.clearFocusedThread();
      if (!_isCurrentAccount(accountGeneration, record.accountId)) {
        return false;
      }
      restored = agentController.threadId == null;
    }
    if (!restored ||
        agentController.threadId == record.practiceThreadId ||
        agentController.hasActivePractice) {
      _setError('上次练习已保留，但暂时无法返回首页对话。');
      return false;
    }
    return true;
  }

  _StoredPracticeWorkspace? _adoptFocusedPractice(String accountId) {
    if (!agentController.hasActivePractice) {
      return null;
    }
    final practiceThreadId = agentController.threadId;
    final matter = agentController.activeMatter;
    final sessionId = agentController.practiceSessionId;
    if (practiceThreadId == null ||
        matter == null ||
        sessionId == null ||
        !_validOpaqueId(practiceThreadId) ||
        !_validOpaqueId(matter.id) ||
        !_validOpaqueId(sessionId) ||
        !_validOpaqueId(matter.scene.id) ||
        !_validTitle(matter.scene.title)) {
      _setError('检测到上次练习，但练习标识不完整，暂时无法安全恢复。');
      return null;
    }
    return _StoredPracticeWorkspace.pending(
      accountId: accountId,
      operationId: 'adopted-practice-session',
      practiceThreadId: practiceThreadId,
      returnThreadId: null,
    ).commit(
      matterId: matter.id,
      sessionId: sessionId,
      scenarioId: matter.scene.id,
      scenarioTitle: matter.scene.title,
      scenarioType: matter.scene.scenarioType,
      presentationMode: matter.scene.presentationMode,
    );
  }

  Future<PracticeWorkspaceLease?> _createLease({
    required String accountId,
    required int accountGeneration,
    required int operationGeneration,
    required String operationId,
    required String? returnThreadId,
    bool preparedToLeave = false,
  }) async {
    if (returnThreadId != null && !_validOpaqueId(returnThreadId)) {
      _setError('首页对话状态异常，暂时无法开始练习。');
      return null;
    }
    if (agentController.hasPendingThreadCreationRecovery) {
      _setError('有一条新对话仍在恢复，请先回到首页完成恢复，再开始练习。');
      return null;
    }
    if (!preparedToLeave && !await _prepareToLeavePractice()) {
      return null;
    }
    final created = await agentController.createIndependentThread();
    if (!_isCurrentOperation(
      accountGeneration,
      operationGeneration,
      accountId,
    )) {
      return null;
    }
    if (!created && agentController.hasPendingThreadCreationRecovery) {
      _setError('有一条新对话仍在恢复，请先回到首页完成恢复，再开始练习。');
      return null;
    }
    final practiceThreadId = agentController.threadId;
    if (!created ||
        practiceThreadId == null ||
        !_validOpaqueId(practiceThreadId) ||
        practiceThreadId == returnThreadId) {
      _setError('暂时无法创建独立练习空间，请稍后重试。');
      return null;
    }
    final record = _StoredPracticeWorkspace.pending(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: returnThreadId,
    );
    _current = record;
    notifyListeners();
    if (!await _persistCurrent(
      accountId: accountId,
      accountGeneration: accountGeneration,
      record: record,
    )) {
      return null;
    }
    _errorMessage = null;
    return record.lease;
  }

  Future<_PracticeResumeVerification> _resumeAndVerify(
    _StoredPracticeWorkspace record, {
    bool preparedToLeave = false,
  }) async {
    if (!record.isCommitted ||
        !await _focusThread(
          record.practiceThreadId,
          preparedToLeave: preparedToLeave,
        )) {
      return _PracticeResumeVerification.unavailable;
    }
    if (agentController.threadId != record.practiceThreadId ||
        agentController.practiceSessionId != record.sessionId ||
        agentController.activeMatter?.id != record.matterId) {
      return _PracticeResumeVerification.mismatch;
    }
    return agentController.hasActivePractice
        ? _PracticeResumeVerification.active
        : _PracticeResumeVerification.terminal;
  }

  Future<bool> _restoreReturnFocus(
    _StoredPracticeWorkspace record, {
    required bool preparedToLeave,
    bool fallbackToEmpty = false,
  }) async {
    final returnThreadId = record.returnThreadId;
    if (returnThreadId == null) {
      await agentController.clearFocusedThread();
      return agentController.threadId == null;
    }
    final restored = await _focusThread(
      returnThreadId,
      preparedToLeave: preparedToLeave,
    );
    final safeHomeFocus = restored && !agentController.hasActivePractice;
    if (safeHomeFocus || !fallbackToEmpty) {
      return safeHomeFocus;
    }
    await agentController.clearFocusedThread();
    return agentController.threadId == null;
  }

  String? get _safeCurrentReturnThreadId =>
      agentController.hasActivePractice ? null : agentController.threadId;

  Future<bool> _clearStoredRecord(
    _StoredPracticeWorkspace record, {
    required int accountGeneration,
    required int operationGeneration,
  }) async {
    _current = null;
    notifyListeners();
    try {
      await _enqueueStoreWrite(() => recordStore.delete(record.accountId));
      return _isCurrentOperation(
        accountGeneration,
        operationGeneration,
        record.accountId,
      );
    } on Object {
      if (_isCurrentOperation(
        accountGeneration,
        operationGeneration,
        record.accountId,
      )) {
        _setError('练习已经结束，但本机记录清理失败。');
      }
      return false;
    }
  }

  Future<bool> _focusThread(
    String threadId, {
    bool preparedToLeave = false,
  }) async {
    if (agentController.threadId == threadId) {
      return true;
    }
    if (!preparedToLeave && !await _prepareToLeavePractice()) {
      return false;
    }
    final selected = await agentController.selectThread(threadId);
    return selected && agentController.threadId == threadId;
  }

  Future<bool> _prepareToLeavePractice() async {
    if (await agentController.prepareToLeavePractice()) {
      return true;
    }
    _setError('请先完成正在提交的一轮，再离开当前练习。');
    return false;
  }

  Future<bool> _persistCurrent({
    required String accountId,
    required int accountGeneration,
    required _StoredPracticeWorkspace record,
  }) async {
    try {
      await _enqueueStoreWrite(
        () => recordStore.write(accountId, record.encode()),
      );
      if (!_isCurrentAccount(accountGeneration, accountId) ||
          !identical(_current, record)) {
        return false;
      }
      return true;
    } on Object {
      if (_isCurrentAccount(accountGeneration, accountId) &&
          identical(_current, record)) {
        _setError('暂时无法安全保存练习记录，请在当前页面重试。');
      }
      return false;
    }
  }

  Future<void> _enqueueStoreWrite(Future<void> Function() operation) {
    final result = _writeTail.then((_) => operation());
    _writeTail = result.then<void>(
      (_) {},
      onError: (Object error, StackTrace stackTrace) {},
    );
    return result;
  }

  bool _canStartOperation() {
    return !_disposed &&
        !_busy &&
        _accountId != null &&
        _loadedAccountId == _accountId;
  }

  void _beginOperation() {
    _activeOperationDone = Completer<void>();
    _busy = true;
    _errorMessage = null;
    notifyListeners();
  }

  void _finishOperation({
    required int accountGeneration,
    required int operationGeneration,
  }) {
    final activeOperation = _activeOperationDone;
    _activeOperationDone = null;
    if (activeOperation != null && !activeOperation.isCompleted) {
      activeOperation.complete();
    }
    final accountId = _accountId;
    if (accountId != null &&
        _isCurrentOperation(
          accountGeneration,
          operationGeneration,
          accountId,
        )) {
      _busy = false;
      notifyListeners();
    }
  }

  void _setError(String message) {
    if (_disposed) {
      return;
    }
    _errorMessage = message;
    notifyListeners();
  }

  void _setErrorIfAbsent(String message) {
    if (_errorMessage == null) {
      _setError(message);
    }
  }

  bool _isCurrentAccount(int generation, String accountId) {
    return !_disposed &&
        generation == _accountGeneration &&
        accountId == _accountId;
  }

  bool _isCurrentOperation(
    int accountGeneration,
    int operationGeneration,
    String accountId,
  ) {
    return _isCurrentAccount(accountGeneration, accountId) &&
        operationGeneration == _operationGeneration;
  }

  @override
  void dispose() {
    _disposed = true;
    ++_accountGeneration;
    ++_operationGeneration;
    super.dispose();
  }
}

enum _PracticeResumeVerification { active, terminal, mismatch, unavailable }

final class _StoredPracticeWorkspace {
  const _StoredPracticeWorkspace({
    required this.accountId,
    required this.operationId,
    required this.practiceThreadId,
    required this.returnThreadId,
    required this.matterId,
    required this.sessionId,
    required this.scenarioId,
    required this.scenarioTitle,
    required this.scenarioType,
    required this.presentationMode,
  });

  factory _StoredPracticeWorkspace.pending({
    required String accountId,
    required String operationId,
    required String practiceThreadId,
    required String? returnThreadId,
  }) {
    return _StoredPracticeWorkspace(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: returnThreadId,
      matterId: null,
      sessionId: null,
      scenarioId: null,
      scenarioTitle: null,
      scenarioType: null,
      presentationMode: AgentScenePresentationMode.standard,
    );
  }

  static const schemaVersion = 2;

  final String accountId;
  final String operationId;
  final String practiceThreadId;
  final String? returnThreadId;
  final String? matterId;
  final String? sessionId;
  final String? scenarioId;
  final String? scenarioTitle;
  final String? scenarioType;
  final AgentScenePresentationMode presentationMode;

  bool get isCommitted =>
      matterId != null &&
      sessionId != null &&
      scenarioId != null &&
      scenarioTitle != null;

  PracticeWorkspaceLease get lease => PracticeWorkspaceLease(
    operationId: operationId,
    practiceThreadId: practiceThreadId,
    returnThreadId: returnThreadId,
  );

  _StoredPracticeWorkspace commit({
    required String matterId,
    required String sessionId,
    required String scenarioId,
    required String scenarioTitle,
    String? scenarioType,
    AgentScenePresentationMode presentationMode =
        AgentScenePresentationMode.standard,
  }) {
    return _StoredPracticeWorkspace(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: returnThreadId,
      matterId: matterId,
      sessionId: sessionId,
      scenarioId: scenarioId,
      scenarioTitle: scenarioTitle,
      scenarioType: scenarioType,
      presentationMode: presentationMode,
    );
  }

  _StoredPracticeWorkspace withReturnThreadId(String? value) {
    return _StoredPracticeWorkspace(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: value,
      matterId: matterId,
      sessionId: sessionId,
      scenarioId: scenarioId,
      scenarioTitle: scenarioTitle,
      scenarioType: scenarioType,
      presentationMode: presentationMode,
    );
  }

  String encode() {
    return jsonEncode(<String, Object?>{
      'schema_version': schemaVersion,
      'account_id': accountId,
      'operation_id': operationId,
      'practice_thread_id': practiceThreadId,
      'return_thread_id': returnThreadId,
      'matter_id': matterId,
      'practice_session_id': sessionId,
      'scenario_definition_id': scenarioId,
      'scenario_title': scenarioTitle,
      'scenario_type': scenarioType,
      'presentation_mode': presentationMode.name,
    });
  }

  static _StoredPracticeWorkspace? tryDecode(
    String encoded, {
    required String expectedAccountId,
  }) {
    try {
      final decoded = jsonDecode(encoded);
      if (decoded is! Map<String, Object?>) {
        return null;
      }
      final version = decoded['schema_version'];
      final expectedKeys = version == 1
          ? const <String>{
              'schema_version',
              'account_id',
              'operation_id',
              'practice_thread_id',
              'return_thread_id',
              'matter_id',
              'practice_session_id',
              'scenario_definition_id',
              'scenario_title',
            }
          : const <String>{
              'schema_version',
              'account_id',
              'operation_id',
              'practice_thread_id',
              'return_thread_id',
              'matter_id',
              'practice_session_id',
              'scenario_definition_id',
              'scenario_title',
              'scenario_type',
              'presentation_mode',
            };
      if (!setEquals(decoded.keys.toSet(), expectedKeys) ||
          (version != 1 && version != schemaVersion) ||
          decoded['account_id'] != expectedAccountId) {
        return null;
      }
      final accountId = decoded['account_id'];
      final operationId = decoded['operation_id'];
      final practiceThreadId = decoded['practice_thread_id'];
      final returnThreadId = decoded['return_thread_id'];
      final matterId = decoded['matter_id'];
      final sessionId = decoded['practice_session_id'];
      final scenarioId = decoded['scenario_definition_id'];
      final scenarioTitle = decoded['scenario_title'];
      final scenarioType = version == 1 ? null : decoded['scenario_type'];
      final presentationModeName = version == 1
          ? AgentScenePresentationMode.standard.name
          : decoded['presentation_mode'];
      final storedPresentationMode = AgentScenePresentationMode.values
          .where((value) => value.name == presentationModeName)
          .firstOrNull;
      if (accountId is! String ||
          operationId is! String ||
          practiceThreadId is! String ||
          (returnThreadId != null && returnThreadId is! String) ||
          (matterId != null && matterId is! String) ||
          (sessionId != null && sessionId is! String) ||
          (scenarioId != null && scenarioId is! String) ||
          (scenarioTitle != null && scenarioTitle is! String) ||
          (scenarioType != null && scenarioType is! String) ||
          storedPresentationMode == null ||
          !_validOpaqueId(accountId) ||
          !_validOperationId(operationId) ||
          !_validOpaqueId(practiceThreadId) ||
          (returnThreadId is String &&
              (!_validOpaqueId(returnThreadId) ||
                  returnThreadId == practiceThreadId))) {
        return null;
      }
      final presentationMode =
          scenarioType == 'INTERVIEW' &&
              storedPresentationMode == AgentScenePresentationMode.standard
          ? AgentScenePresentationMode.immersiveRoleplay
          : storedPresentationMode;
      final committedValues = <Object?>[
        matterId,
        sessionId,
        scenarioId,
        scenarioTitle,
      ];
      final committed = committedValues.every((value) => value != null);
      if (!committed && committedValues.any((value) => value != null)) {
        return null;
      }
      if (committed &&
          (!_validOpaqueId(matterId! as String) ||
              !_validOpaqueId(sessionId! as String) ||
              !_validOpaqueId(scenarioId! as String) ||
              !_validTitle(scenarioTitle! as String) ||
              !_validScenePresentation(
                scenarioType as String?,
                presentationMode,
              ))) {
        return null;
      }
      return _StoredPracticeWorkspace(
        accountId: accountId,
        operationId: operationId,
        practiceThreadId: practiceThreadId,
        returnThreadId: returnThreadId as String?,
        matterId: matterId as String?,
        sessionId: sessionId as String?,
        scenarioId: scenarioId as String?,
        scenarioTitle: scenarioTitle as String?,
        scenarioType: scenarioType as String?,
        presentationMode: presentationMode,
      );
    } on Object {
      return null;
    }
  }
}

bool _validScenePresentation(
  String? scenarioType,
  AgentScenePresentationMode presentationMode,
) {
  if (scenarioType != null &&
      (scenarioType.isEmpty ||
          scenarioType.length > 32 ||
          scenarioType.trim() != scenarioType ||
          !RegExp(r'^[A-Z][A-Z0-9_]*$').hasMatch(scenarioType))) {
    return false;
  }
  return presentationMode != AgentScenePresentationMode.immersiveRoleplay ||
      scenarioType == 'WORKPLACE' ||
      scenarioType == 'DAILY' ||
      scenarioType == 'INTERVIEW';
}

bool _validOpaqueId(String value) {
  return value.isNotEmpty &&
      value.length <= 128 &&
      value.trim() == value &&
      !value.contains('\u0000');
}

bool _validOperationId(String value) {
  return value.length >= 8 &&
      value.length <= 128 &&
      value.trim() == value &&
      !value.contains('\u0000');
}

bool _validTitle(String value) {
  return value.isNotEmpty &&
      value.length <= 200 &&
      value.trim() == value &&
      !value.contains('\u0000') &&
      utf8.encode(value).length <= 512;
}
