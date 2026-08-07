import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';

import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';

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
/// The server remains authoritative for Thread, Goal, and Session state. This
/// controller only persists their opaque identities so navigation never has to
/// guess a recent Session or reuse the ordinary Agent Thread.
final class PracticeWorkspaceController extends ChangeNotifier {
  PracticeWorkspaceController({
    required this.conversationController,
    required this.practiceController,
    required this.recordStore,
  }) {
    practiceController.addListener(_capturePracticeProgress);
  }

  final ConversationController conversationController;
  final PracticeController practiceController;
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

  String? get currentTitle => _current?.scene?.name;
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
  bool get resumableHasProgress => _current?.hasMeaningfulProgress ?? false;
  PracticeWorkspaceLease? get currentLease => _current?.lease;
  String? get currentPracticeThreadId => _current?.practiceThreadId;
  String? get currentGoalId => _current?.goalId;
  String? get currentSessionId => _current?.sessionId;
  String? get currentSceneId => _current?.scene?.id;
  String? get currentPracticeExperience =>
      _current?.scene?.experience.wireValue;

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
        await conversationController.initialize();
        if (!_isCurrentAccount(accountGeneration, accountId)) {
          return;
        }
        if (!conversationController.isInitialized) {
          _setErrorIfAbsent('Agent 对话仍在恢复，暂时无法核对上次练习。');
          return;
        }
        if (!practiceController.hasActivePractice) {
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
          await conversationController.initialize();
          if (!_isCurrentAccount(accountGeneration, accountId) ||
              !await _prepareToLeavePractice()) {
            return;
          }
          await conversationController.clearFocusedThread();
          if (!_isCurrentAccount(accountGeneration, accountId) ||
              conversationController.threadId != null) {
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
        final latestReturnThreadId = conversationController.threadId;
        if (!practiceController.hasActivePractice &&
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
    required String? goalId,
    required String sessionId,
    required SceneDefinition scene,
  }) async {
    if (!_canStartOperation() ||
        (goalId != null && !_validOpaqueId(goalId)) ||
        !_validOpaqueId(sessionId) ||
        !_validOpaqueId(scene.id) ||
        !_validTitle(scene.name) ||
        scene.status != SceneStatus.active) {
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
          conversationController.threadId != lease.practiceThreadId ||
          (goalId != null && conversationController.activeGoalId != goalId)) {
        _setError('练习空间已经变化，未保存本次练习。');
        return false;
      }
      final committed = current.commit(
        goalId: goalId,
        sessionId: sessionId,
        scene: scene,
        completedTurns: practiceController.completedTurns,
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
      final latestReturnThreadId = conversationController.threadId;
      if (!practiceController.hasActivePractice &&
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
      _capturePracticeProgress();
      final terminalPracticeWasFocused =
          current.isCommitted &&
          conversationController.threadId == current.practiceThreadId &&
          practiceController.practiceSessionId == current.sessionId &&
          (current.goalId == null ||
              conversationController.activeGoalId == current.goalId) &&
          !practiceController.hasActivePractice;
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
        conversationController.threadId != current.practiceThreadId ||
        practiceController.practiceSessionId != current.sessionId ||
        practiceController.recordingState != PracticeRecordingState.completed) {
      _setError('练习尚未完整结束，暂时无法回到 Agent 复盘。');
      return false;
    }
    final title = current.scene!.name;
    final completedTurns = practiceController.completedTurns;
    if (!await parkCurrentPractice()) {
      return false;
    }
    final sent = await conversationController.sendText(
      '我刚完成了“$title”的 $completedTurns 轮练习。'
      '请直接读取这次练习的真实评分与报告，先概括我的主要表现，'
      '再问我想重点复盘哪一项。',
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
      final latestReturnThreadId = conversationController.threadId;
      if (!practiceController.hasActivePractice &&
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
        final ended = await practiceController.endActivePracticeEarly();
        if (!ended || practiceController.hasActivePractice) {
          final parked = await practiceController.parkPractice();
          final focusRestored =
              parked &&
              await _restoreReturnFocus(
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
    await conversationController.initialize();
    if (!_isCurrentAccount(accountGeneration, record.accountId)) {
      return false;
    }
    if (conversationController.threadId != record.practiceThreadId &&
        !practiceController.hasActivePractice) {
      return true;
    }
    if (!await _prepareToLeavePractice() ||
        !_isCurrentAccount(accountGeneration, record.accountId)) {
      return false;
    }
    var restored = false;
    final returnThreadId = record.returnThreadId;
    if (returnThreadId == null) {
      await conversationController.clearFocusedThread();
      restored = conversationController.threadId == null;
    } else {
      restored = await conversationController.selectThread(returnThreadId);
      restored =
          restored &&
          conversationController.threadId == returnThreadId &&
          !practiceController.hasActivePractice;
    }
    if (!_isCurrentAccount(accountGeneration, record.accountId)) {
      return false;
    }
    if (!restored) {
      await conversationController.clearFocusedThread();
      if (!_isCurrentAccount(accountGeneration, record.accountId)) {
        return false;
      }
      restored = conversationController.threadId == null;
    }
    if (!restored ||
        conversationController.threadId == record.practiceThreadId ||
        practiceController.hasActivePractice) {
      _setError('上次练习已保留，但暂时无法返回首页对话。');
      return false;
    }
    return true;
  }

  _StoredPracticeWorkspace? _adoptFocusedPractice(String accountId) {
    if (!practiceController.hasActivePractice) {
      return null;
    }
    final practiceThreadId = conversationController.threadId;
    final goalId = conversationController.activeGoalId;
    final scene = practiceController.scene;
    final sessionId = practiceController.practiceSessionId;
    if (practiceThreadId == null ||
        scene == null ||
        sessionId == null ||
        !_validOpaqueId(practiceThreadId) ||
        (goalId != null && !_validOpaqueId(goalId)) ||
        !_validOpaqueId(sessionId) ||
        !_validOpaqueId(scene.id) ||
        !_validTitle(scene.name)) {
      _setError('检测到上次练习，但练习标识不完整，暂时无法安全恢复。');
      return null;
    }
    return _StoredPracticeWorkspace.pending(
      accountId: accountId,
      operationId: 'adopted-practice-session',
      practiceThreadId: practiceThreadId,
      returnThreadId: null,
    ).commit(
      goalId: goalId,
      sessionId: sessionId,
      scene: scene,
      completedTurns: practiceController.completedTurns,
    );
  }

  void _capturePracticeProgress() {
    final current = _current;
    if (_disposed ||
        current == null ||
        !current.isCommitted ||
        conversationController.threadId != current.practiceThreadId ||
        practiceController.practiceSessionId != current.sessionId) {
      return;
    }
    final completedTurns = practiceController.completedTurns;
    if (current.completedTurns == completedTurns) {
      return;
    }
    final updated = current.withCompletedTurns(completedTurns);
    _current = updated;
    notifyListeners();
    final accountId = _accountId;
    final accountGeneration = _accountGeneration;
    if (accountId == null || current.accountId != accountId) {
      return;
    }
    unawaited(
      _enqueueStoreWrite(
        () => recordStore.write(accountId, updated.encode()),
      ).catchError((_) {
        if (_isCurrentAccount(accountGeneration, accountId)) {
          _setErrorIfAbsent('练习进度暂时无法保存，请稍后重试。');
        }
      }),
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
    if (conversationController.hasPendingThreadCreationRecovery) {
      _setError('有一条新对话仍在恢复，请先回到首页完成恢复，再开始练习。');
      return null;
    }
    if (!preparedToLeave && !await _prepareToLeavePractice()) {
      return null;
    }
    final created = await conversationController.createIndependentThread();
    if (!_isCurrentOperation(
      accountGeneration,
      operationGeneration,
      accountId,
    )) {
      return null;
    }
    if (!created && conversationController.hasPendingThreadCreationRecovery) {
      _setError('有一条新对话仍在恢复，请先回到首页完成恢复，再开始练习。');
      return null;
    }
    final practiceThreadId = conversationController.threadId;
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
    try {
      await practiceController.restoreCreatedPractice(
        sessionId: record.sessionId!,
        scene: record.scene!,
      );
    } on Object {
      return _PracticeResumeVerification.unavailable;
    }
    if (conversationController.threadId != record.practiceThreadId ||
        practiceController.practiceSessionId != record.sessionId ||
        (record.goalId != null &&
            conversationController.activeGoalId != record.goalId)) {
      return _PracticeResumeVerification.mismatch;
    }
    return practiceController.hasActivePractice
        ? _PracticeResumeVerification.active
        : _PracticeResumeVerification.terminal;
  }

  Future<bool> _restoreReturnFocus(
    _StoredPracticeWorkspace record, {
    required bool preparedToLeave,
    bool fallbackToEmpty = false,
  }) async {
    // Prefer the conversation the user is currently viewing so parking the
    // practice lands them back where they were, instead of the stale launch
    // Home. Only fall back to the stored return thread when the current
    // thread is the practice thread itself or there is no focused Home.
    final currentThreadId = conversationController.threadId;
    final returnThreadId =
        currentThreadId != null && currentThreadId != record.practiceThreadId
        ? currentThreadId
        : record.returnThreadId;
    if (returnThreadId == null) {
      await conversationController.clearFocusedThread();
      return conversationController.threadId == null;
    }
    final restored = await _focusThread(
      returnThreadId,
      preparedToLeave: preparedToLeave,
    );
    final safeHomeFocus = restored && !practiceController.hasActivePractice;
    if (safeHomeFocus || !fallbackToEmpty) {
      return safeHomeFocus;
    }
    await conversationController.clearFocusedThread();
    return conversationController.threadId == null;
  }

  String? get _safeCurrentReturnThreadId => practiceController.hasActivePractice
      ? null
      : conversationController.threadId;

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
    if (conversationController.threadId == threadId) {
      return true;
    }
    if (!preparedToLeave && !await _prepareToLeavePractice()) {
      return false;
    }
    final selected = await conversationController.selectThread(threadId);
    return selected && conversationController.threadId == threadId;
  }

  Future<bool> _prepareToLeavePractice() async {
    if (await practiceController.parkPractice()) {
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
    practiceController.removeListener(_capturePracticeProgress);
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
    required this.goalId,
    required this.sessionId,
    required this.scene,
    required this.completedTurns,
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
      goalId: null,
      sessionId: null,
      scene: null,
      completedTurns: null,
    );
  }

  static const schemaVersion = 6;

  final String accountId;
  final String operationId;
  final String practiceThreadId;
  final String? returnThreadId;
  final String? goalId;
  final String? sessionId;
  final SceneDefinition? scene;
  final int? completedTurns;

  bool get isCommitted => sessionId != null && scene != null;

  bool get hasMeaningfulProgress =>
      isCommitted && completedTurns != null && completedTurns! > 0;

  PracticeWorkspaceLease get lease => PracticeWorkspaceLease(
    operationId: operationId,
    practiceThreadId: practiceThreadId,
    returnThreadId: returnThreadId,
  );

  _StoredPracticeWorkspace commit({
    required String? goalId,
    required String sessionId,
    required SceneDefinition scene,
    int completedTurns = 0,
  }) {
    return _StoredPracticeWorkspace(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: returnThreadId,
      goalId: goalId,
      sessionId: sessionId,
      scene: scene,
      completedTurns: completedTurns,
    );
  }

  _StoredPracticeWorkspace withReturnThreadId(String? value) {
    return _StoredPracticeWorkspace(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: value,
      goalId: goalId,
      sessionId: sessionId,
      scene: scene,
      completedTurns: completedTurns,
    );
  }

  _StoredPracticeWorkspace withCompletedTurns(int value) {
    return _StoredPracticeWorkspace(
      accountId: accountId,
      operationId: operationId,
      practiceThreadId: practiceThreadId,
      returnThreadId: returnThreadId,
      goalId: goalId,
      sessionId: sessionId,
      scene: scene,
      completedTurns: value,
    );
  }

  String encode() {
    return jsonEncode(<String, Object?>{
      'schema_version': schemaVersion,
      'account_id': accountId,
      'operation_id': operationId,
      'practice_thread_id': practiceThreadId,
      'return_thread_id': returnThreadId,
      'goal_id': goalId,
      'practice_session_id': sessionId,
      'scene': scene == null ? null : encodeSceneDefinition(scene!),
      'completed_turns': completedTurns,
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
      const expectedKeys = <String>{
        'schema_version',
        'account_id',
        'operation_id',
        'practice_thread_id',
        'return_thread_id',
        'goal_id',
        'practice_session_id',
        'scene',
        'completed_turns',
      };
      if (!setEquals(decoded.keys.toSet(), expectedKeys) ||
          version != schemaVersion ||
          decoded['account_id'] != expectedAccountId) {
        return null;
      }
      final accountId = decoded['account_id'];
      final operationId = decoded['operation_id'];
      final practiceThreadId = decoded['practice_thread_id'];
      final returnThreadId = decoded['return_thread_id'];
      final goalId = decoded['goal_id'];
      final sessionId = decoded['practice_session_id'];
      final sceneValue = decoded['scene'];
      final completedTurns = decoded['completed_turns'];
      if (accountId is! String ||
          operationId is! String ||
          practiceThreadId is! String ||
          (returnThreadId != null && returnThreadId is! String) ||
          (goalId != null && goalId is! String) ||
          (sessionId != null && sessionId is! String) ||
          (completedTurns != null &&
              (completedTurns is! int || completedTurns < 0)) ||
          !_validOpaqueId(accountId) ||
          !_validOperationId(operationId) ||
          !_validOpaqueId(practiceThreadId) ||
          (returnThreadId is String &&
              (!_validOpaqueId(returnThreadId) ||
                  returnThreadId == practiceThreadId))) {
        return null;
      }
      final committedValues = <Object?>[sessionId, sceneValue];
      final committed = committedValues.every((value) => value != null);
      if (!committed && committedValues.any((value) => value != null)) {
        return null;
      }
      if ((!committed && (goalId != null || completedTurns != null)) ||
          (committed && completedTurns == null)) {
        return null;
      }
      final scene = committed ? decodeSceneDefinition(sceneValue) : null;
      if (committed &&
          (!_validOpaqueId(sessionId! as String) ||
              scene!.status != SceneStatus.active)) {
        return null;
      }
      if (goalId is String && !_validOpaqueId(goalId)) {
        return null;
      }
      return _StoredPracticeWorkspace(
        accountId: accountId,
        operationId: operationId,
        practiceThreadId: practiceThreadId,
        returnThreadId: returnThreadId as String?,
        goalId: goalId as String?,
        sessionId: sessionId as String?,
        scene: scene,
        completedTurns: completedTurns as int?,
      );
    } on Object {
      return null;
    }
  }
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
