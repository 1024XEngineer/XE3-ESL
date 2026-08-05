import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/preparation/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

const _ieltsSpeakingFullMockSceneId = 'scn_ielts_speaking_full';

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

  final IeltsPracticeMode mode;
  final IeltsPracticeSelection? selection;
}

final class PreparationController extends ChangeNotifier {
  PreparationController({
    required this.client,
    SceneQuestionBankClient? ieltsQuestionBankClient,
    this.ieltsHistoryStore = const NullIeltsPracticeHistoryStore(),
  }) : _ieltsQuestionBankClient =
           ieltsQuestionBankClient ??
           (client is SceneQuestionBankClient
               ? client as SceneQuestionBankClient
               : null);

  final SceneClient client;
  final SceneQuestionBankClient? _ieltsQuestionBankClient;
  final IeltsPracticeHistoryStore ieltsHistoryStore;

  List<SceneDefinition> _scenes = const <SceneDefinition>[];
  SceneDefinition? _selectedScene;
  SceneDefinition? _detail;
  List<RoleDefinition> _roles = const <RoleDefinition>[];
  RoleDefinition? _selectedRole;
  PracticeOption? _selectedOption;
  String? _errorMessage;
  bool _loadingScenes = false;
  bool _loadingDetail = false;
  bool _disposed = false;
  int _accountEpoch = 0;
  int _selectionEpoch = 0;
  Future<void>? _sceneLoad;
  _FailedPreparationRequest? _failedRequest;
  IeltsQuestionBank? _ieltsQuestionBank;
  String? _ieltsErrorMessage;
  bool _loadingIeltsQuestionBank = false;
  Future<void>? _ieltsQuestionBankLoad;
  String? _accountId;
  final Map<String, IeltsSetProgress> _part1Progress =
      <String, IeltsSetProgress>{};
  final Map<String, IeltsSetProgress> _part2Progress =
      <String, IeltsSetProgress>{};
  final Map<String, IeltsSetProgress> _part3Progress =
      <String, IeltsSetProgress>{};
  final Map<String, IeltsPracticeSelection> _sessionSelections =
      <String, IeltsPracticeSelection>{};
  final Set<String> _completedSessionParts = <String>{};
  IeltsPracticeNavigationRequest? _ieltsNavigationRequest;

  List<SceneDefinition> get scenes =>
      List<SceneDefinition>.unmodifiable(_scenes);
  SceneDefinition? get selectedScene => _selectedScene;
  SceneDefinition? get detail => _detail;
  List<RoleDefinition> get roles => List<RoleDefinition>.unmodifiable(_roles);
  RoleDefinition? get selectedRole => _selectedRole;
  PracticeOption? get selectedOption => _selectedOption;
  String? get errorMessage => _errorMessage;
  bool get isLoadingScenes => _loadingScenes;
  bool get isLoadingDetail => _loadingDetail;
  bool get hasLoadedScenes => !_loadingScenes && _sceneLoad != null;
  IeltsQuestionBank? get ieltsQuestionBank => _ieltsQuestionBank;
  String? get ieltsErrorMessage => _ieltsErrorMessage;
  bool get isLoadingIeltsQuestionBank => _loadingIeltsQuestionBank;

  List<PracticeOption> get availableOptions {
    final role = _selectedRole;
    final sceneDetail = _detail;
    if (role == null || sceneDetail == null) {
      return const <PracticeOption>[];
    }
    return List<PracticeOption>.unmodifiable(
      sceneDetail.practiceOptions.where(
        (option) =>
            option.type == PracticeOptionType.fullSimulation ||
            option.roleId == role.id,
      ),
    );
  }

  bool get hasCompleteSelection =>
      _selectedScene != null &&
      _selectedRole != null &&
      _selectedOption != null;

  Future<void> loadIfNeeded() {
    final existing = _sceneLoad;
    if (existing != null) {
      return existing;
    }
    final operation = _loadScenes();
    _sceneLoad = operation;
    return operation;
  }

  Future<void> loadIeltsQuestionBankIfNeeded() {
    final existing = _ieltsQuestionBankLoad;
    if (existing != null) {
      return existing;
    }
    final operation = _loadIeltsQuestionBank();
    _ieltsQuestionBankLoad = operation;
    return operation;
  }

  Future<void> _loadIeltsQuestionBank() async {
    final reader = _ieltsQuestionBankClient;
    if (reader == null) {
      _ieltsErrorMessage = '雅思口语题库暂时不可用。';
      notifyListeners();
      return;
    }
    final accountEpoch = _accountEpoch;
    _loadingIeltsQuestionBank = true;
    _ieltsErrorMessage = null;
    notifyListeners();
    try {
      final bank = await reader.getIeltsQuestionBank();
      if (_isCurrentAccount(accountEpoch)) {
        _ieltsQuestionBank = bank;
      }
    } on Object {
      if (_isCurrentAccount(accountEpoch)) {
        _ieltsErrorMessage = '雅思口语题库暂时无法加载，请稍后重试。';
      }
    } finally {
      if (_isCurrentAccount(accountEpoch)) {
        _loadingIeltsQuestionBank = false;
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
    _clearIeltsProgress();
    final stored = await ieltsHistoryStore.read(accountId);
    if (!_isCurrentAccount(accountEpoch) || _accountId != accountId) {
      return;
    }
    if (stored != null) {
      _restoreIeltsHistory(stored);
    }
    notifyListeners();
  }

  IeltsSetProgress ieltsProgress(IeltsPracticeMode mode, String setId) {
    final progress = switch (mode) {
      IeltsPracticeMode.fullMock ||
      IeltsPracticeMode.part1 => _part1Progress[setId],
      IeltsPracticeMode.part2 => _part2Progress[setId],
      IeltsPracticeMode.part3 => _part3Progress[setId],
    };
    return progress ?? const IeltsSetProgress();
  }

  IeltsPracticeSelection? randomFullMockSelection() {
    final bank = _ieltsQuestionBank;
    if (bank == null || bank.part1Sets.isEmpty || bank.topicGroups.isEmpty) {
      return null;
    }
    final completedPart2 = _completedIds(_part2Progress);
    final completedPart3 = _completedIds(_part3Progress);
    return randomIeltsFullMockSelection(
      bank: bank,
      completedPart1SetIds: _completedIds(_part1Progress),
      completedTopicGroupIds: completedPart2.intersection(completedPart3),
    );
  }

  IeltsPracticeSelection? nextUnfinishedSelection(
    IeltsPracticeMode mode, {
    String? afterId,
  }) {
    final bank = _ieltsQuestionBank;
    if (bank == null) {
      return null;
    }
    final ids = mode == IeltsPracticeMode.part1
        ? bank.part1Topics.map((topic) => topic.id).toList(growable: false)
        : bank.topicGroups.map((group) => group.id).toList(growable: false);
    if (ids.isEmpty) {
      return null;
    }
    final start = afterId == null ? 0 : (ids.indexOf(afterId) + 1) % ids.length;
    for (var offset = 0; offset < ids.length; offset++) {
      final id = ids[(start + offset) % ids.length];
      if (!ieltsProgress(mode, id).completed) {
        return IeltsPracticeSelection(
          mode: mode,
          part1SetId: mode == IeltsPracticeMode.part1 ? id : null,
          topicGroupId: mode == IeltsPracticeMode.part1 ? null : id,
        );
      }
    }
    final id = ids[start];
    return IeltsPracticeSelection(
      mode: mode,
      part1SetId: mode == IeltsPracticeMode.part1 ? id : null,
      topicGroupId: mode == IeltsPracticeMode.part1 ? null : id,
    );
  }

  Future<void> beginIeltsSession(
    String sessionId,
    IeltsPracticeSelection selection,
  ) async {
    if (!selection.isValid || sessionId.isEmpty) {
      return;
    }
    _sessionSelections[sessionId] = selection;
    final now = DateTime.now().toUtc();
    switch (selection.mode) {
      case IeltsPracticeMode.fullMock:
        _markStarted(_part1Progress, selection.part1SetId!, now);
        _markStarted(_part2Progress, selection.topicGroupId!, now);
      case IeltsPracticeMode.part1:
        _markStarted(_part1Progress, selection.part1SetId!, now);
      case IeltsPracticeMode.part2:
        _markStarted(_part2Progress, selection.topicGroupId!, now);
      case IeltsPracticeMode.part3:
        _markStarted(_part3Progress, selection.topicGroupId!, now);
    }
    await _saveIeltsHistory();
    if (!_disposed) {
      notifyListeners();
    }
  }

  IeltsPracticeSelection? ieltsSelectionForSession(String sessionId) =>
      _sessionSelections[sessionId];

  void requestIeltsNavigation(IeltsPracticeNavigationRequest request) {
    if (_disposed) {
      return;
    }
    _ieltsNavigationRequest = request;
    notifyListeners();
  }

  IeltsPracticeNavigationRequest? takeIeltsNavigationRequest() {
    final request = _ieltsNavigationRequest;
    _ieltsNavigationRequest = null;
    return request;
  }

  Future<void> markIeltsPartStarted(
    String sessionId,
    IeltsPracticeMode mode,
  ) async {
    final selection = _sessionSelections[sessionId];
    final groupId = selection?.topicGroupId;
    if (groupId == null || mode != IeltsPracticeMode.part3) {
      return;
    }
    _markStarted(_part3Progress, groupId, DateTime.now().toUtc());
    await _saveIeltsHistory();
    if (!_disposed) {
      notifyListeners();
    }
  }

  Future<void> markIeltsPartCompleted(
    String sessionId,
    IeltsPracticeMode mode,
  ) async {
    final selection = _sessionSelections[sessionId];
    if (selection == null) {
      return;
    }
    final completionKey = '$sessionId:${mode.wireName}';
    if (!_completedSessionParts.add(completionKey)) {
      return;
    }
    final now = DateTime.now().toUtc();
    switch (mode) {
      case IeltsPracticeMode.fullMock:
        if (selection.part1SetId case final id?) {
          _markCompleted(_part1Progress, id, now);
        }
        if (selection.topicGroupId case final id?) {
          _markCompleted(_part2Progress, id, now);
          _markCompleted(_part3Progress, id, now);
        }
      case IeltsPracticeMode.part1:
        if (selection.part1SetId case final id?) {
          _markCompleted(_part1Progress, id, now);
        }
      case IeltsPracticeMode.part2:
        if (selection.topicGroupId case final id?) {
          _markCompleted(_part2Progress, id, now);
        }
      case IeltsPracticeMode.part3:
        if (selection.topicGroupId case final id?) {
          _markCompleted(_part3Progress, id, now);
        }
    }
    await _saveIeltsHistory();
    if (!_disposed) {
      notifyListeners();
    }
  }

  Future<void> retryLastFailure() {
    final failed = _failedRequest;
    if (failed == null) {
      return Future<void>.value();
    }
    if (failed.scene == null) {
      _sceneLoad = null;
      return loadIfNeeded();
    }
    return selectScene(failed.scene!);
  }

  Future<void> _loadScenes() async {
    final accountEpoch = _accountEpoch;
    _loadingScenes = true;
    _errorMessage = null;
    _failedRequest = null;
    notifyListeners();
    try {
      final scenes = await client.listScenes();
      if (!_isCurrentAccount(accountEpoch)) {
        return;
      }
      _scenes = List<SceneDefinition>.unmodifiable(scenes);
    } on SceneClientException catch (error) {
      if (_isCurrentAccount(accountEpoch)) {
        _errorMessage = _messageFor(error);
        _failedRequest = const _FailedPreparationRequest();
      }
    } on Object {
      if (_isCurrentAccount(accountEpoch)) {
        _errorMessage = _catalogUnavailableMessage;
        _failedRequest = const _FailedPreparationRequest();
      }
    } finally {
      if (_isCurrentAccount(accountEpoch)) {
        _loadingScenes = false;
        notifyListeners();
      }
    }
  }

  Future<void> selectScene(SceneDefinition scene) async {
    if (_disposed) {
      return;
    }
    final canonicalScene = _scenes
        .where((item) => item.id == scene.id && item.version == scene.version)
        .firstOrNull;
    if (canonicalScene == null) {
      return;
    }
    final accountEpoch = _accountEpoch;
    final selectionEpoch = ++_selectionEpoch;
    _selectedScene = canonicalScene;
    _detail = null;
    _roles = const <RoleDefinition>[];
    _selectedRole = null;
    _selectedOption = null;
    _errorMessage = null;
    _failedRequest = null;
    _loadingDetail = true;
    notifyListeners();

    try {
      final detail = await client.getScene(canonicalScene.id);
      if (!_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScene.id,
      )) {
        return;
      }
      _validateAggregate(summary: canonicalScene, detail: detail);
      final roles = detail.roles;
      _detail = detail;
      _roles = List<RoleDefinition>.unmodifiable(roles);
      if (roles.length == 1) {
        _selectedRole = roles.single;
        _selectedOption = detail.practiceOptions
            .where((option) => option.type == PracticeOptionType.fullSimulation)
            .firstOrNull;
      }
    } on SceneClientException catch (error) {
      if (_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScene.id,
      )) {
        _errorMessage = _messageFor(error);
        _failedRequest = _FailedPreparationRequest(scene: canonicalScene);
      }
    } on Object {
      if (_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScene.id,
      )) {
        _errorMessage = _catalogInvalidMessage;
        _failedRequest = _FailedPreparationRequest(scene: canonicalScene);
      }
    } finally {
      if (_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScene.id,
      )) {
        _loadingDetail = false;
        notifyListeners();
      }
    }
  }

  void selectRole(RoleDefinition role) {
    if (_disposed) {
      return;
    }
    final canonicalRole = _roles
        .where((item) => item.id == role.id)
        .firstOrNull;
    if (canonicalRole == null) {
      return;
    }
    _selectedRole = canonicalRole;
    _selectedOption = null;
    notifyListeners();
  }

  void selectOption(PracticeOption option) {
    if (_disposed) {
      return;
    }
    final canonicalOption = availableOptions
        .where((item) => item.id == option.id)
        .firstOrNull;
    if (canonicalOption == null) {
      return;
    }
    _selectedOption = canonicalOption;
    notifyListeners();
  }

  bool selectRecommendedConfiguration() {
    if (_disposed ||
        _selectedScene == null ||
        _detail == null ||
        _roles.isEmpty) {
      return false;
    }
    final preferredRoleType = switch (_selectedScene!.id) {
      'scn_interview_recruiter_screening' ||
      'scn_interview_self_introduction' => 'HR_INTERVIEWER',
      'scn_interview_behavioral' => 'BEHAVIORAL_INTERVIEWER',
      'scn_interview_system_design_spoken' => 'SYSTEM_DESIGN_INTERVIEWER',
      'scn_interview_hiring_manager' => 'HIRING_MANAGER',
      _ => 'TECHNICAL_INTERVIEWER',
    };
    final role =
        _roles.where((item) => item.type == preferredRoleType).firstOrNull ??
        _roles.first;
    _selectedRole = role;
    final compatibleOptions = _detail!.practiceOptions
        .where(
          (option) =>
              option.type == PracticeOptionType.fullSimulation ||
              option.roleId == role.id,
        )
        .toList(growable: false);
    final fullSimulation = compatibleOptions
        .where((option) => option.type == PracticeOptionType.fullSimulation)
        .firstOrNull;
    final roleFocus = compatibleOptions
        .where(
          (option) =>
              option.type == PracticeOptionType.focus &&
              option.roleId == role.id,
        )
        .firstOrNull;
    _selectedOption = _isIeltsSpeakingScene(_selectedScene!.id)
        ? fullSimulation ?? roleFocus ?? compatibleOptions.firstOrNull
        : roleFocus ?? fullSimulation ?? compatibleOptions.firstOrNull;
    notifyListeners();
    return hasCompleteSelection;
  }

  void showSceneList() {
    if (_disposed) {
      return;
    }
    _selectionEpoch++;
    _selectedScene = null;
    _detail = null;
    _roles = const <RoleDefinition>[];
    _selectedRole = null;
    _selectedOption = null;
    _errorMessage = null;
    _failedRequest = null;
    _loadingDetail = false;
    notifyListeners();
  }

  Future<void> clearPrivateState() async {
    _accountEpoch++;
    _selectionEpoch++;
    _sceneLoad = null;
    _scenes = const <SceneDefinition>[];
    _selectedScene = null;
    _detail = null;
    _roles = const <RoleDefinition>[];
    _selectedRole = null;
    _selectedOption = null;
    _errorMessage = null;
    _failedRequest = null;
    _loadingScenes = false;
    _loadingDetail = false;
    _ieltsQuestionBank = null;
    _ieltsErrorMessage = null;
    _loadingIeltsQuestionBank = false;
    _ieltsQuestionBankLoad = null;
    _accountId = null;
    _clearIeltsProgress();
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

  void _clearIeltsProgress() {
    _part1Progress.clear();
    _part2Progress.clear();
    _part3Progress.clear();
    _sessionSelections.clear();
    _completedSessionParts.clear();
    _ieltsNavigationRequest = null;
  }

  void _restoreIeltsHistory(String value) {
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
      _clearIeltsProgress();
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
      if (date == null) {
        continue;
      }
      target[entry.key] = IeltsSetProgress(
        attemptCount: attempts,
        inProgress: inProgress,
        lastPracticedAt: date.toUtc(),
      );
    }
  }

  IeltsPracticeSelection? _decodeSelection(Object? raw) {
    if (raw is! Map<String, Object?> || raw['mode'] is! String) {
      return null;
    }
    final mode = IeltsPracticeMode.fromWireName(raw['mode']! as String);
    if (mode == null) {
      return null;
    }
    final selection = IeltsPracticeSelection(
      mode: mode,
      part1SetId: raw['part_1_set_id'] as String?,
      topicGroupId: raw['topic_group_id'] as String?,
    );
    return selection.isValid ? selection : null;
  }

  Future<void> _saveIeltsHistory() async {
    final accountId = _accountId;
    if (accountId == null) {
      return;
    }
    final value = jsonEncode(<String, Object>{
      'version': 1,
      'part1': _encodeProgressMap(_part1Progress),
      'part2': _encodeProgressMap(_part2Progress),
      'part3': _encodeProgressMap(_part3Progress),
      'sessions': _sessionSelections.map(
        (key, value) => MapEntry(key, value.toJson()),
      ),
      'completed_session_parts': _completedSessionParts.toList(growable: false),
    });
    await ieltsHistoryStore.write(accountId, value);
  }

  Map<String, Object> _encodeProgressMap(
    Map<String, IeltsSetProgress> values,
  ) => values.map(
    (key, value) => MapEntry(key, <String, Object>{
      'attempts': value.attemptCount,
      'in_progress': value.inProgress,
      'last_practiced_at': value.lastPracticedAt!.toIso8601String(),
    }),
  );

  bool _isCurrentAccount(int accountEpoch) =>
      !_disposed && accountEpoch == _accountEpoch;

  bool _isCurrentSelection(
    int accountEpoch,
    int selectionEpoch,
    String sceneId,
  ) =>
      _isCurrentAccount(accountEpoch) &&
      selectionEpoch == _selectionEpoch &&
      _selectedScene?.id == sceneId;

  @override
  void dispose() {
    _disposed = true;
    _accountEpoch++;
    _selectionEpoch++;
    super.dispose();
  }
}

final class _FailedPreparationRequest {
  const _FailedPreparationRequest({this.scene});

  final SceneDefinition? scene;
}

void _validateAggregate({
  required SceneDefinition summary,
  required SceneDefinition detail,
}) {
  if (detail.id != summary.id ||
      detail.version != summary.version ||
      detail.family != summary.family ||
      detail.model != summary.model ||
      detail.name != summary.name ||
      detail.status != SceneStatus.active ||
      detail.roles.isEmpty ||
      detail.roles.any((role) => role.sceneId != detail.id)) {
    throw const SceneClientException(
      kind: SceneClientFailureKind.invalidResponse,
    );
  }

  final roleIds = detail.roles.map((role) => role.id).toSet();
  if (roleIds.length != detail.roles.length) {
    throw const SceneClientException(
      kind: SceneClientFailureKind.invalidResponse,
    );
  }

  var fullSimulationCount = 0;
  final focusRoleIds = <String>{};
  for (final option in detail.practiceOptions) {
    if (option.sceneId != detail.id) {
      throw const SceneClientException(
        kind: SceneClientFailureKind.invalidResponse,
      );
    }
    switch (option.type) {
      case PracticeOptionType.fullSimulation:
        fullSimulationCount++;
        if (option.roleId != null) {
          throw const SceneClientException(
            kind: SceneClientFailureKind.invalidResponse,
          );
        }
      case PracticeOptionType.focus:
        final roleId = option.roleId;
        if (roleId == null ||
            !roleIds.contains(roleId) ||
            !focusRoleIds.add(roleId)) {
          throw const SceneClientException(
            kind: SceneClientFailureKind.invalidResponse,
          );
        }
    }
  }
  if (fullSimulationCount != 1 ||
      focusRoleIds.length != detail.roles.length ||
      !focusRoleIds.containsAll(roleIds)) {
    throw const SceneClientException(
      kind: SceneClientFailureKind.invalidResponse,
    );
  }
}

const _catalogUnavailableMessage = '练习目录暂时无法加载，请检查网络后重试。';
const _catalogInvalidMessage = '练习目录响应无法识别，请稍后重试。';

bool _isIeltsSpeakingScene(String sceneId) =>
    sceneId == _ieltsSpeakingFullMockSceneId ||
    sceneId == 'scn_ielts_speaking_part_1' ||
    sceneId == 'scn_ielts_speaking_part_2' ||
    sceneId == 'scn_ielts_speaking_part_3';

String _messageFor(SceneClientException error) {
  return switch (error.kind) {
    SceneClientFailureKind.network ||
    SceneClientFailureKind.unavailable => _catalogUnavailableMessage,
    SceneClientFailureKind.invalidResponse => _catalogInvalidMessage,
  };
}
