import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';

final class PracticeWorkspaceLease {
  const PracticeWorkspaceLease({required this.operationId});

  final String operationId;

  @override
  bool operator ==(Object other) =>
      other is PracticeWorkspaceLease && other.operationId == operationId;

  @override
  int get hashCode => operationId.hashCode;
}

enum PracticeWorkspaceResumeOutcome {
  resumed,
  terminal,
  stale,
  unavailable,
  none,
}

/// Owns one resumable formal Practice Session on this device.
///
/// Agent Threads remain conversation-only. Practice navigation never creates,
/// focuses, hides, or restores an Agent Thread.
final class PracticeWorkspaceController extends ChangeNotifier {
  PracticeWorkspaceController({
    required this.practiceController,
    required this.recordStore,
  }) {
    practiceController.addListener(_capturePracticeProgress);
  }

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
  String? get errorMessage => _errorMessage;
  bool get isBusy => _busy;
  bool get canRetryActivation =>
      !_disposed &&
      !_busy &&
      _accountId != null &&
      _loadedAccountId != _accountId;
  bool get hasResumable => _current?.isCommitted ?? false;
  bool get resumableHasProgress => _current?.hasMeaningfulProgress ?? false;
  PracticeWorkspaceLease? get currentLease => _current?.lease;
  String? get currentPlanId => _current?.planId;
  String? get currentSessionId => _current?.sessionId;
  String? get currentSceneId => _current?.scene?.id;
  String? get currentPracticeExperience =>
      _current?.scene?.experience.wireValue;
  bool hasResumableForPlan(String planId) =>
      _validOpaqueId(planId) &&
      (_current?.isCommitted ?? false) &&
      _current!.planId == planId;

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
        if (!practiceController.hasActivePractice) {
          loaded = true;
        } else {
          final adopted = _adoptPresentedPractice(accountId);
          if (adopted == null) {
            return;
          }
          _current = adopted;
          notifyListeners();
          if (!await _persistCurrent(
                accountId: accountId,
                accountGeneration: accountGeneration,
                record: adopted,
              ) ||
              !await _prepareToLeavePractice()) {
            return;
          }
          loaded = true;
        }
      } else {
        final restored = _StoredPracticeWorkspace.tryDecode(
          encoded,
          expectedAccountId: accountId,
        );
        if (restored == null) {
          if (!await _prepareToLeavePractice()) {
            return;
          }
          await _enqueueStoreWrite(() => recordStore.delete(accountId));
          if (_isCurrentAccount(accountGeneration, accountId)) {
            _errorMessage = '本机练习记录已失效，请重新开始练习。';
            loaded = true;
          }
        } else if (!restored.isCommitted) {
          await _enqueueStoreWrite(() => recordStore.delete(accountId));
          loaded = _isCurrentAccount(accountGeneration, accountId);
        } else {
          _current = restored;
          if (practiceController.hasActivePractice &&
              !await _prepareToLeavePractice()) {
            return;
          }
          loaded = true;
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

  Future<PracticeWorkspaceLease?> acquirePractice(String operationId) async {
    if (!_canStartOperation() || !_validOperationId(operationId)) {
      if (!_busy && _accountId != null && _loadedAccountId == _accountId) {
        _setError('无法创建练习记录，请重新进入训练页后重试。');
      }
      return null;
    }
    final accountId = _accountId!;
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      final current = _current;
      if (current?.operationId == operationId) {
        _errorMessage = null;
        return current!.lease;
      }
      if (current?.isCommitted ?? false) {
        _setError('已有练习尚未处理，请先继续或更换当前练习。');
        return null;
      }
      return _replacePendingRecord(
        accountId: accountId,
        accountGeneration: accountGeneration,
        operationId: operationId,
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
    required String planId,
    required String sessionId,
    required SceneDefinition scene,
  }) async {
    if (!_canStartOperation() ||
        !_validOpaqueId(planId) ||
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
      final current = _current;
      if (current == null || current.lease != lease) {
        _setError('练习记录已经变化，未保存本次练习。');
        return false;
      }
      final committed = current.commit(
        planId: planId,
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
    return await resumeCurrentPracticeWithOutcome() ==
        PracticeWorkspaceResumeOutcome.resumed;
  }

  Future<PracticeWorkspaceResumeOutcome>
  resumeCurrentPracticeWithOutcome() async {
    if (!_canStartOperation()) {
      return PracticeWorkspaceResumeOutcome.unavailable;
    }
    final current = _current;
    if (current == null || !current.isCommitted) {
      _setError('没有可以继续的练习。');
      return PracticeWorkspaceResumeOutcome.none;
    }
    final activeScene = practiceController.scene;
    if (practiceController.hasActivePractice &&
        practiceController.practicePlanId == current.planId &&
        practiceController.practiceSessionId == current.sessionId &&
        activeScene?.id == current.scene!.id &&
        activeScene?.version == current.scene!.version) {
      if (_errorMessage != null) {
        _errorMessage = null;
        notifyListeners();
      }
      return PracticeWorkspaceResumeOutcome.resumed;
    }
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      if (practiceController.hasActivePractice &&
          !await _prepareToLeavePractice()) {
        return PracticeWorkspaceResumeOutcome.unavailable;
      }
      final verification = await _resumeAndVerify(current);
      switch (verification) {
        case _PracticeResumeVerification.active:
          _errorMessage = null;
          return PracticeWorkspaceResumeOutcome.resumed;
        case _PracticeResumeVerification.terminal:
          final cleared = await _clearStoredRecord(
            current,
            accountGeneration: accountGeneration,
            operationGeneration: operationGeneration,
          );
          if (!cleared) {
            return PracticeWorkspaceResumeOutcome.unavailable;
          }
          _setErrorIfAbsent('上次练习已经结束，可以开始新的练习。');
          return PracticeWorkspaceResumeOutcome.terminal;
        case _PracticeResumeVerification.mismatch:
          final cleared = await _clearStoredRecord(
            current,
            accountGeneration: accountGeneration,
            operationGeneration: operationGeneration,
          );
          if (!cleared) {
            return PracticeWorkspaceResumeOutcome.unavailable;
          }
          _setErrorIfAbsent('上次练习的服务端状态已经变化，记录已清理，可以开始新的练习。');
          return PracticeWorkspaceResumeOutcome.stale;
        case _PracticeResumeVerification.unavailable:
          _setErrorIfAbsent('无法核验上次练习，请稍后重试。');
          return PracticeWorkspaceResumeOutcome.unavailable;
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
      final terminalPracticeWasPresented =
          current.isCommitted &&
          practiceController.practicePlanId == current.planId &&
          practiceController.practiceSessionId == current.sessionId &&
          !practiceController.hasActivePractice;
      if (!await _prepareToLeavePractice()) {
        return false;
      }
      if (terminalPracticeWasPresented) {
        if (!await _clearStoredRecord(
          current,
          accountGeneration: accountGeneration,
          operationGeneration: operationGeneration,
        )) {
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

  Future<bool> discardPendingPractice(PracticeWorkspaceLease lease) async {
    if (!_canStartOperation()) {
      return false;
    }
    final current = _current;
    if (current == null || current.isCommitted || current.lease != lease) {
      _setError('当前没有可撤销的练习准备。');
      return false;
    }
    final accountId = _accountId!;
    final accountGeneration = _accountGeneration;
    final operationGeneration = ++_operationGeneration;
    _beginOperation();
    try {
      await _enqueueStoreWrite(() => recordStore.delete(accountId));
      if (!_isCurrentOperation(
        accountGeneration,
        operationGeneration,
        accountId,
      )) {
        return false;
      }
      _current = null;
      _errorMessage = null;
      notifyListeners();
      return true;
    } on Object {
      if (_isCurrentOperation(
        accountGeneration,
        operationGeneration,
        accountId,
      )) {
        _setError('练习准备记录清理失败。');
      }
      return false;
    } finally {
      _finishOperation(
        accountGeneration: accountGeneration,
        operationGeneration: operationGeneration,
      );
    }
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
      final current = _current;
      if (current == null || !current.isCommitted) {
        if (current?.operationId == operationId) {
          _errorMessage = null;
          return current!.lease;
        }
        return _replacePendingRecord(
          accountId: accountId,
          accountGeneration: accountGeneration,
          operationId: operationId,
        );
      }

      if (practiceController.hasActivePractice &&
          practiceController.practiceSessionId != current.sessionId &&
          !await _prepareToLeavePractice()) {
        return null;
      }
      final verification = await _resumeAndVerify(current);
      if (verification == _PracticeResumeVerification.unavailable) {
        _setErrorIfAbsent('无法核验当前练习，因此没有创建新的练习。');
        return null;
      }
      if (verification == _PracticeResumeVerification.active) {
        final ended = await practiceController.endActivePracticeEarly();
        if (!ended || practiceController.hasActivePractice) {
          await practiceController.parkPractice();
          _setError('暂时无法结束当前练习，进度仍已保留。');
          return null;
        }
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
      return _replacePendingRecord(
        accountId: accountId,
        accountGeneration: accountGeneration,
        operationId: operationId,
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

  _StoredPracticeWorkspace? _adoptPresentedPractice(String accountId) {
    final scene = practiceController.scene;
    final planId = practiceController.practicePlanId;
    final sessionId = practiceController.practiceSessionId;
    if (!practiceController.hasActivePractice ||
        scene == null ||
        planId == null ||
        sessionId == null ||
        !_validOpaqueId(planId) ||
        !_validOpaqueId(sessionId) ||
        !_validOpaqueId(scene.id) ||
        !_validTitle(scene.name)) {
      _setError('检测到上次练习，但练习标识不完整，暂时无法安全恢复。');
      return null;
    }
    return _StoredPracticeWorkspace.pending(
      accountId: accountId,
      operationId: 'adopted-practice-session',
    ).commit(
      planId: planId,
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
        practiceController.practicePlanId != current.planId ||
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

  Future<PracticeWorkspaceLease?> _replacePendingRecord({
    required String accountId,
    required int accountGeneration,
    required String operationId,
  }) async {
    final record = _StoredPracticeWorkspace.pending(
      accountId: accountId,
      operationId: operationId,
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
    _StoredPracticeWorkspace record,
  ) async {
    if (!record.isCommitted) {
      return _PracticeResumeVerification.mismatch;
    }
    if (practiceController.hasActivePractice &&
        practiceController.practiceSessionId == record.sessionId &&
        practiceController.practicePlanId == record.planId) {
      return _PracticeResumeVerification.active;
    }
    try {
      await practiceController.restoreCreatedPractice(
        sessionId: record.sessionId!,
        scene: record.scene!,
      );
    } on Object {
      return _PracticeResumeVerification.unavailable;
    }
    if (practiceController.practicePlanId != record.planId ||
        practiceController.practiceSessionId != record.sessionId ||
        practiceController.scene?.id != record.scene!.id ||
        practiceController.scene?.version != record.scene!.version) {
      return _PracticeResumeVerification.mismatch;
    }
    return practiceController.hasActivePractice
        ? _PracticeResumeVerification.active
        : _PracticeResumeVerification.terminal;
  }

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
      return _isCurrentAccount(accountGeneration, accountId) &&
          identical(_current, record);
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

  bool _canStartOperation() =>
      !_disposed &&
      !_busy &&
      _accountId != null &&
      _loadedAccountId == _accountId;

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

  bool _isCurrentAccount(int generation, String accountId) =>
      !_disposed && generation == _accountGeneration && accountId == _accountId;

  bool _isCurrentOperation(
    int accountGeneration,
    int operationGeneration,
    String accountId,
  ) =>
      _isCurrentAccount(accountGeneration, accountId) &&
      operationGeneration == _operationGeneration;

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
    required this.planId,
    required this.sessionId,
    required this.scene,
    required this.completedTurns,
  });

  factory _StoredPracticeWorkspace.pending({
    required String accountId,
    required String operationId,
  }) => _StoredPracticeWorkspace(
    accountId: accountId,
    operationId: operationId,
    planId: null,
    sessionId: null,
    scene: null,
    completedTurns: null,
  );

  static const schemaVersion = 8;

  final String accountId;
  final String operationId;
  final String? planId;
  final String? sessionId;
  final SceneDefinition? scene;
  final int? completedTurns;

  bool get isCommitted => planId != null && sessionId != null && scene != null;

  bool get hasMeaningfulProgress =>
      isCommitted && completedTurns != null && completedTurns! > 0;

  PracticeWorkspaceLease get lease =>
      PracticeWorkspaceLease(operationId: operationId);

  _StoredPracticeWorkspace commit({
    required String planId,
    required String sessionId,
    required SceneDefinition scene,
    int completedTurns = 0,
  }) => _StoredPracticeWorkspace(
    accountId: accountId,
    operationId: operationId,
    planId: planId,
    sessionId: sessionId,
    scene: scene,
    completedTurns: completedTurns,
  );

  _StoredPracticeWorkspace withCompletedTurns(int value) =>
      _StoredPracticeWorkspace(
        accountId: accountId,
        operationId: operationId,
        planId: planId,
        sessionId: sessionId,
        scene: scene,
        completedTurns: value,
      );

  String encode() => jsonEncode(<String, Object?>{
    'schema_version': schemaVersion,
    'account_id': accountId,
    'operation_id': operationId,
    'practice_plan_id': planId,
    'practice_session_id': sessionId,
    'scene': scene == null ? null : encodeSceneDefinition(scene!),
    'completed_turns': completedTurns,
  });

  static _StoredPracticeWorkspace? tryDecode(
    String encoded, {
    required String expectedAccountId,
  }) {
    try {
      final decoded = jsonDecode(encoded);
      if (decoded is! Map<String, Object?>) {
        return null;
      }
      const expectedKeys = <String>{
        'schema_version',
        'account_id',
        'operation_id',
        'practice_plan_id',
        'practice_session_id',
        'scene',
        'completed_turns',
      };
      if (!setEquals(decoded.keys.toSet(), expectedKeys) ||
          decoded['schema_version'] != schemaVersion ||
          decoded['account_id'] != expectedAccountId) {
        return null;
      }
      final accountId = decoded['account_id'];
      final operationId = decoded['operation_id'];
      final planId = decoded['practice_plan_id'];
      final sessionId = decoded['practice_session_id'];
      final sceneValue = decoded['scene'];
      final completedTurns = decoded['completed_turns'];
      if (accountId is! String ||
          operationId is! String ||
          (planId != null && planId is! String) ||
          (sessionId != null && sessionId is! String) ||
          (completedTurns != null &&
              (completedTurns is! int || completedTurns < 0)) ||
          !_validOpaqueId(accountId) ||
          !_validOperationId(operationId)) {
        return null;
      }
      final committedValues = <Object?>[planId, sessionId, sceneValue];
      final committed = committedValues.every((value) => value != null);
      if (!committed && committedValues.any((value) => value != null)) {
        return null;
      }
      if ((!committed && completedTurns != null) ||
          (committed && completedTurns == null)) {
        return null;
      }
      final scene = committed ? decodeSceneDefinition(sceneValue) : null;
      if (committed &&
          (!_validOpaqueId(planId! as String) ||
              !_validOpaqueId(sessionId! as String) ||
              scene!.status != SceneStatus.active)) {
        return null;
      }
      return _StoredPracticeWorkspace(
        accountId: accountId,
        operationId: operationId,
        planId: planId as String?,
        sessionId: sessionId as String?,
        scene: scene,
        completedTurns: completedTurns as int?,
      );
    } on Object {
      return null;
    }
  }
}

bool _validOpaqueId(String value) =>
    value.isNotEmpty &&
    value.length <= 128 &&
    value.trim() == value &&
    !value.contains('\u0000');

bool _validOperationId(String value) =>
    value.length >= 8 &&
    value.length <= 128 &&
    value.trim() == value &&
    !value.contains('\u0000');

bool _validTitle(String value) =>
    value.isNotEmpty &&
    value.length <= 200 &&
    value.trim() == value &&
    !value.contains('\u0000') &&
    utf8.encode(value).length <= 512;
