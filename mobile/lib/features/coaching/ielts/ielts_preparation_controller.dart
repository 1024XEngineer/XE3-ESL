import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class IeltsSetProgress {
  const IeltsSetProgress({
    this.attemptCount = 0,
    this.inProgress = false,
    this.lastPracticedAt,
  });

  final int attemptCount;
  final bool inProgress;
  final DateTime? lastPracticedAt;

  bool get completed => attemptCount > 0;
}

final class IeltsPracticeNavigationRequest {
  const IeltsPracticeNavigationRequest({required this.mode, this.selection});

  final PracticeMode mode;
  final IeltsPracticeSelection? selection;
}

final class IeltsPreparationController extends ChangeNotifier {
  IeltsPreparationController({
    required this.client,
    this.historyStore = const NullIeltsPracticeHistoryStore(),
  });

  final IeltsQuestionBankClient client;
  final IeltsPracticeHistoryStore historyStore;

  IeltsQuestionBank? _questionBank;
  String? _errorMessage;
  bool _loading = false;
  bool _disposed = false;
  Future<void>? _questionBankLoad;
  String? _accountId;
  int _accountEpoch = 0;
  final Map<String, IeltsSetProgress> _part1Progress = {};
  final Map<String, IeltsSetProgress> _part2Progress = {};
  final Map<String, IeltsSetProgress> _part3Progress = {};
  final Map<String, IeltsPracticeSelection> _sessionSelections = {};
  final Set<String> _completedSessionParts = {};
  IeltsPracticeNavigationRequest? _navigationRequest;

  IeltsQuestionBank? get questionBank => _questionBank;
  String? get errorMessage => _errorMessage;
  bool get isLoading => _loading;

  Future<void> loadIfNeeded() {
    final existing = _questionBankLoad;
    if (existing != null) {
      return existing;
    }
    final operation = _load();
    _questionBankLoad = operation;
    return operation;
  }

  Future<void> retryLoad() {
    _questionBankLoad = null;
    return loadIfNeeded();
  }

  Future<void> _load() async {
    final accountEpoch = _accountEpoch;
    _loading = true;
    _errorMessage = null;
    notifyListeners();
    try {
      final bank = await client.getQuestionBank();
      if (_isCurrent(accountEpoch)) {
        _questionBank = bank;
      }
    } on Object {
      if (_isCurrent(accountEpoch)) {
        _errorMessage = '雅思口语题库暂时无法加载，请稍后重试。';
      }
    } finally {
      if (_isCurrent(accountEpoch)) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> activateAccount(String accountId) async {
    if (_disposed || accountId.isEmpty || _accountId == accountId) {
      return;
    }
    final accountEpoch = _accountId == null ? _accountEpoch : ++_accountEpoch;
    _accountId = accountId;
    _clearProgress();
    final stored = await historyStore.read(accountId);
    if (!_isCurrent(accountEpoch) || _accountId != accountId) {
      return;
    }
    if (stored != null) {
      _restoreHistory(stored);
    }
    notifyListeners();
  }

  IeltsSetProgress progress(PracticeMode mode, String setId) {
    final value = switch (mode) {
      PracticeMode.fullMock || PracticeMode.part1 => _part1Progress[setId],
      PracticeMode.part2 => _part2Progress[setId],
      PracticeMode.part3 => _part3Progress[setId],
      PracticeMode.fullSimulation ||
      PracticeMode.focus => throw ArgumentError.value(mode, 'mode'),
    };
    return value ?? const IeltsSetProgress();
  }

  IeltsPracticeSelection? randomFullMockSelection() {
    final bank = _questionBank;
    if (bank == null || bank.part1Sets.isEmpty || bank.topicGroups.isEmpty) {
      return null;
    }
    return randomIeltsFullMockSelection(
      bank: bank,
      completedPart1SetIds: _completedIds(_part1Progress),
      completedTopicGroupIds: _completedIds(
        _part2Progress,
      ).intersection(_completedIds(_part3Progress)),
    );
  }

  IeltsPracticeSelection? nextUnfinishedSelection(
    PracticeMode mode, {
    String? afterId,
  }) {
    final bank = _questionBank;
    if (bank == null) {
      return null;
    }
    if (mode != PracticeMode.part1 &&
        mode != PracticeMode.part2 &&
        mode != PracticeMode.part3) {
      throw ArgumentError.value(mode, 'mode');
    }
    final ids = mode == PracticeMode.part1
        ? bank.part1Topics.map((topic) => topic.id).toList(growable: false)
        : bank.topicGroups.map((group) => group.id).toList(growable: false);
    if (ids.isEmpty) {
      return null;
    }
    final start = afterId == null ? 0 : (ids.indexOf(afterId) + 1) % ids.length;
    for (var offset = 0; offset < ids.length; offset++) {
      final id = ids[(start + offset) % ids.length];
      if (!progress(mode, id).completed) {
        return _selectionFor(mode, id);
      }
    }
    return _selectionFor(mode, ids[start]);
  }

  IeltsPracticeSelection _selectionFor(PracticeMode mode, String id) =>
      IeltsPracticeSelection(
        part1SetId: mode == PracticeMode.part1 ? id : null,
        topicGroupId: mode == PracticeMode.part1 ? null : id,
      );

  Future<void> beginSession(
    String sessionId,
    PracticeMode mode,
    IeltsPracticeSelection selection,
  ) async {
    if (!selection.isValidForMode(mode) || sessionId.isEmpty) {
      return;
    }
    _sessionSelections[sessionId] = selection;
    final now = DateTime.now().toUtc();
    switch (mode) {
      case PracticeMode.fullMock:
        _markStarted(_part1Progress, selection.part1SetId!, now);
        _markStarted(_part2Progress, selection.topicGroupId!, now);
      case PracticeMode.part1:
        _markStarted(_part1Progress, selection.part1SetId!, now);
      case PracticeMode.part2:
        _markStarted(_part2Progress, selection.topicGroupId!, now);
      case PracticeMode.part3:
        _markStarted(_part3Progress, selection.topicGroupId!, now);
      case PracticeMode.fullSimulation || PracticeMode.focus:
        throw ArgumentError.value(mode, 'mode');
    }
    await _saveHistory();
    if (!_disposed) {
      notifyListeners();
    }
  }

  IeltsPracticeSelection? selectionForSession(String sessionId) =>
      _sessionSelections[sessionId];

  void requestNavigation(IeltsPracticeNavigationRequest request) {
    if (_disposed) {
      return;
    }
    _navigationRequest = request;
    notifyListeners();
  }

  IeltsPracticeNavigationRequest? takeNavigationRequest() {
    final request = _navigationRequest;
    _navigationRequest = null;
    return request;
  }

  Future<void> markPartStarted(String sessionId, PracticeMode mode) async {
    final groupId = _sessionSelections[sessionId]?.topicGroupId;
    if (groupId == null || mode != PracticeMode.part3) {
      return;
    }
    _markStarted(_part3Progress, groupId, DateTime.now().toUtc());
    await _saveHistory();
    if (!_disposed) {
      notifyListeners();
    }
  }

  Future<void> markPartCompleted(String sessionId, PracticeMode mode) async {
    final selection = _sessionSelections[sessionId];
    if (selection == null) {
      return;
    }
    if (!_completedSessionParts.add('$sessionId:${mode.wireValue}')) {
      return;
    }
    final now = DateTime.now().toUtc();
    switch (mode) {
      case PracticeMode.fullMock:
        if (selection.part1SetId case final id?) {
          _markCompleted(_part1Progress, id, now);
        }
        if (selection.topicGroupId case final id?) {
          _markCompleted(_part2Progress, id, now);
          _markCompleted(_part3Progress, id, now);
        }
      case PracticeMode.part1:
        if (selection.part1SetId case final id?) {
          _markCompleted(_part1Progress, id, now);
        }
      case PracticeMode.part2:
        if (selection.topicGroupId case final id?) {
          _markCompleted(_part2Progress, id, now);
        }
      case PracticeMode.part3:
        if (selection.topicGroupId case final id?) {
          _markCompleted(_part3Progress, id, now);
        }
      case PracticeMode.fullSimulation || PracticeMode.focus:
        throw ArgumentError.value(mode, 'mode');
    }
    await _saveHistory();
    if (!_disposed) {
      notifyListeners();
    }
  }

  Future<void> clearPrivateState() async {
    _accountEpoch++;
    _questionBank = null;
    _errorMessage = null;
    _loading = false;
    _questionBankLoad = null;
    _accountId = null;
    _clearProgress();
    if (!_disposed) {
      notifyListeners();
    }
  }

  void _markStarted(
    Map<String, IeltsSetProgress> values,
    String id,
    DateTime now,
  ) {
    final current = values[id] ?? const IeltsSetProgress();
    values[id] = IeltsSetProgress(
      attemptCount: current.attemptCount,
      inProgress: true,
      lastPracticedAt: now,
    );
  }

  void _markCompleted(
    Map<String, IeltsSetProgress> values,
    String id,
    DateTime now,
  ) {
    final current = values[id] ?? const IeltsSetProgress();
    values[id] = IeltsSetProgress(
      attemptCount: current.attemptCount + 1,
      lastPracticedAt: now,
    );
  }

  Set<String> _completedIds(Map<String, IeltsSetProgress> values) => values
      .entries
      .where((entry) => entry.value.completed)
      .map((entry) => entry.key)
      .toSet();

  void _clearProgress() {
    _part1Progress.clear();
    _part2Progress.clear();
    _part3Progress.clear();
    _sessionSelections.clear();
    _completedSessionParts.clear();
    _navigationRequest = null;
  }

  void _restoreHistory(String value) {
    try {
      final decoded = jsonDecode(value);
      if (decoded is! Map<String, Object?> || decoded['version'] != 1) {
        return;
      }
      _restoreProgressMap(decoded['part1'], _part1Progress);
      _restoreProgressMap(decoded['part2'], _part2Progress);
      _restoreProgressMap(decoded['part3'], _part3Progress);
      final sessions = decoded['sessions'];
      if (sessions is Map<String, Object?>) {
        for (final entry in sessions.entries) {
          final selection = _decodeSelection(entry.value);
          if (selection != null) {
            _sessionSelections[entry.key] = selection;
          }
        }
      }
      final completedParts = decoded['completed_session_parts'];
      if (completedParts is List<Object?>) {
        for (final value in completedParts) {
          if (value is String && value.isNotEmpty) {
            _completedSessionParts.add(value);
          }
        }
      }
    } on Object {
      _clearProgress();
    }
  }

  void _restoreProgressMap(Object? raw, Map<String, IeltsSetProgress> target) {
    if (raw is! Map<String, Object?>) {
      return;
    }
    for (final entry in raw.entries) {
      final value = entry.value;
      if (value is! Map<String, Object?>) {
        continue;
      }
      final attempts = value['attempts'];
      final inProgress = value['in_progress'];
      final last = value['last_practiced_at'];
      if (attempts is! int ||
          attempts < 0 ||
          inProgress is! bool ||
          last is! String) {
        continue;
      }
      final date = DateTime.tryParse(last);
      if (date != null) {
        target[entry.key] = IeltsSetProgress(
          attemptCount: attempts,
          inProgress: inProgress,
          lastPracticedAt: date.toUtc(),
        );
      }
    }
  }

  IeltsPracticeSelection? _decodeSelection(Object? raw) {
    if (raw is! Map<String, Object?> ||
        raw.keys.any(
          (key) => !const {'part_1_set_id', 'topic_group_id'}.contains(key),
        )) {
      return null;
    }
    final selection = IeltsPracticeSelection(
      part1SetId: raw['part_1_set_id'] as String?,
      topicGroupId: raw['topic_group_id'] as String?,
    );
    return selection.part1SetId != null || selection.topicGroupId != null
        ? selection
        : null;
  }

  Future<void> _saveHistory() async {
    final accountId = _accountId;
    if (accountId == null) {
      return;
    }
    await historyStore.write(
      accountId,
      jsonEncode({
        'version': 1,
        'part1': _encodeProgressMap(_part1Progress),
        'part2': _encodeProgressMap(_part2Progress),
        'part3': _encodeProgressMap(_part3Progress),
        'sessions': _sessionSelections.map(
          (key, value) => MapEntry(key, value.toJson()),
        ),
        'completed_session_parts': _completedSessionParts.toList(
          growable: false,
        ),
      }),
    );
  }

  Map<String, Object> _encodeProgressMap(
    Map<String, IeltsSetProgress> values,
  ) => values.map(
    (key, value) => MapEntry(key, {
      'attempts': value.attemptCount,
      'in_progress': value.inProgress,
      'last_practiced_at': value.lastPracticedAt!.toIso8601String(),
    }),
  );

  bool _isCurrent(int accountEpoch) =>
      !_disposed && accountEpoch == _accountEpoch;

  @override
  void dispose() {
    _disposed = true;
    _accountEpoch++;
    super.dispose();
  }
}
