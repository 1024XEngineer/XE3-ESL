import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/preparation/ielts_practice_history_store.dart';
import 'package:speakup/features/preparation/ielts_question_bank.dart';
import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

const _ieltsSpeakingFullMockScenarioId = 'scn_ielts_speaking_full';

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
    IeltsQuestionBankClient? ieltsQuestionBankClient,
    this.ieltsHistoryStore = const NullIeltsPracticeHistoryStore(),
  }) : _ieltsQuestionBankClient =
           ieltsQuestionBankClient ??
           (client is IeltsQuestionBankClient
               ? client as IeltsQuestionBankClient
               : null);

  final PreparationCatalogClient client;
  final IeltsQuestionBankClient? _ieltsQuestionBankClient;
  final IeltsPracticeHistoryStore ieltsHistoryStore;

  List<PreparationScenario> _scenarios = const <PreparationScenario>[];
  PreparationScenario? _selectedScenario;
  PreparationScenarioDetail? _detail;
  List<PreparationRole> _roles = const <PreparationRole>[];
  PreparationRole? _selectedRole;
  PreparationOption? _selectedOption;
  String? _errorMessage;
  bool _loadingScenarios = false;
  bool _loadingDetail = false;
  bool _disposed = false;
  int _accountEpoch = 0;
  int _selectionEpoch = 0;
  Future<void>? _scenarioLoad;
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

  List<PreparationScenario> get scenarios =>
      List<PreparationScenario>.unmodifiable(_scenarios);
  PreparationScenario? get selectedScenario => _selectedScenario;
  PreparationScenarioDetail? get detail => _detail;
  List<PreparationRole> get roles => List<PreparationRole>.unmodifiable(_roles);
  PreparationRole? get selectedRole => _selectedRole;
  PreparationOption? get selectedOption => _selectedOption;
  String? get errorMessage => _errorMessage;
  bool get isLoadingScenarios => _loadingScenarios;
  bool get isLoadingDetail => _loadingDetail;
  bool get hasLoadedScenarios => !_loadingScenarios && _scenarioLoad != null;
  IeltsQuestionBank? get ieltsQuestionBank => _ieltsQuestionBank;
  String? get ieltsErrorMessage => _ieltsErrorMessage;
  bool get isLoadingIeltsQuestionBank => _loadingIeltsQuestionBank;

  List<PreparationOption> get availableOptions {
    final role = _selectedRole;
    final scenarioDetail = _detail;
    if (role == null || scenarioDetail == null) {
      return const <PreparationOption>[];
    }
    return List<PreparationOption>.unmodifiable(
      scenarioDetail.options.where(
        (option) =>
            option.type == PreparationOptionType.fullSimulation ||
            option.roleId == role.id,
      ),
    );
  }

  bool get hasCompleteSelection =>
      _selectedScenario != null &&
      _selectedRole != null &&
      _selectedOption != null;

  Future<void> loadIfNeeded() {
    final existing = _scenarioLoad;
    if (existing != null) {
      return existing;
    }
    final operation = _loadScenarios();
    _scenarioLoad = operation;
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
        ? bank.part1Sets.map((set) => set.id).toList(growable: false)
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
    if (failed.scenario == null) {
      _scenarioLoad = null;
      return loadIfNeeded();
    }
    return selectScenario(failed.scenario!);
  }

  Future<void> _loadScenarios() async {
    final accountEpoch = _accountEpoch;
    _loadingScenarios = true;
    _errorMessage = null;
    _failedRequest = null;
    notifyListeners();
    try {
      final scenarios = await client.listScenarios();
      if (!_isCurrentAccount(accountEpoch)) {
        return;
      }
      _scenarios = List<PreparationScenario>.unmodifiable(scenarios);
    } on PreparationCatalogException catch (error) {
      if (_isCurrentAccount(accountEpoch) &&
          error.kind != PreparationCatalogFailureKind.superseded) {
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
        _loadingScenarios = false;
        notifyListeners();
      }
    }
  }

  Future<void> selectScenario(PreparationScenario scenario) async {
    if (_disposed) {
      return;
    }
    final canonicalScenario = _scenarios
        .where(
          (item) => item.id == scenario.id && item.version == scenario.version,
        )
        .firstOrNull;
    if (canonicalScenario == null) {
      return;
    }
    final accountEpoch = _accountEpoch;
    final selectionEpoch = ++_selectionEpoch;
    _selectedScenario = canonicalScenario;
    _detail = null;
    _roles = const <PreparationRole>[];
    _selectedRole = null;
    _selectedOption = null;
    _errorMessage = null;
    _failedRequest = null;
    _loadingDetail = true;
    notifyListeners();

    try {
      final results = await Future.wait<Object>([
        client.getScenario(canonicalScenario.id),
        client.listRoles(canonicalScenario.id),
      ]);
      if (!_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScenario.id,
      )) {
        return;
      }
      final detail = results[0] as PreparationScenarioDetail;
      final roles = results[1] as List<PreparationRole>;
      _validateAggregate(
        summary: canonicalScenario,
        detail: detail,
        roles: roles,
      );
      _detail = detail;
      _roles = List<PreparationRole>.unmodifiable(roles);
      if (roles.length == 1) {
        _selectedRole = roles.single;
        _selectedOption = detail.options
            .where(
              (option) => option.type == PreparationOptionType.fullSimulation,
            )
            .firstOrNull;
      }
    } on PreparationCatalogException catch (error) {
      if (_isCurrentSelection(
            accountEpoch,
            selectionEpoch,
            canonicalScenario.id,
          ) &&
          error.kind != PreparationCatalogFailureKind.superseded) {
        _errorMessage = _messageFor(error);
        _failedRequest = _FailedPreparationRequest(scenario: canonicalScenario);
      }
    } on Object {
      if (_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScenario.id,
      )) {
        _errorMessage = _catalogInvalidMessage;
        _failedRequest = _FailedPreparationRequest(scenario: canonicalScenario);
      }
    } finally {
      if (_isCurrentSelection(
        accountEpoch,
        selectionEpoch,
        canonicalScenario.id,
      )) {
        _loadingDetail = false;
        notifyListeners();
      }
    }
  }

  void selectRole(PreparationRole role) {
    if (_disposed) {
      return;
    }
    final canonicalRole = _roles
        .where((item) => item.id == role.id && item.version == role.version)
        .firstOrNull;
    if (canonicalRole == null) {
      return;
    }
    _selectedRole = canonicalRole;
    _selectedOption = null;
    notifyListeners();
  }

  void selectOption(PreparationOption option) {
    if (_disposed) {
      return;
    }
    final canonicalOption = availableOptions
        .where((item) => item.id == option.id && item.version == option.version)
        .firstOrNull;
    if (canonicalOption == null) {
      return;
    }
    _selectedOption = canonicalOption;
    notifyListeners();
  }

  bool selectRecommendedConfiguration() {
    if (_disposed ||
        _selectedScenario == null ||
        _detail == null ||
        _roles.isEmpty) {
      return false;
    }
    final preferredRoleType = switch (_selectedScenario!.id) {
      'scn_interview_recruiter_screening' ||
      'scn_interview_self_introduction' => 'HR_INTERVIEWER',
      'scn_interview_behavioral' => 'BEHAVIORAL_INTERVIEWER',
      'scn_interview_system_design_spoken' => 'SYSTEM_DESIGN_INTERVIEWER',
      'scn_interview_hiring_manager' => 'HIRING_MANAGER',
      'scn_interview_custom' => 'CUSTOM_INTERVIEWER',
      _ => 'TECHNICAL_INTERVIEWER',
    };
    final role =
        _roles.where((item) => item.type == preferredRoleType).firstOrNull ??
        _roles.first;
    _selectedRole = role;
    final compatibleOptions = _detail!.options
        .where(
          (option) =>
              option.type == PreparationOptionType.fullSimulation ||
              option.roleId == role.id,
        )
        .toList(growable: false);
    final fullSimulation = compatibleOptions
        .where((option) => option.type == PreparationOptionType.fullSimulation)
        .firstOrNull;
    final roleFocus = compatibleOptions
        .where(
          (option) =>
              option.type == PreparationOptionType.focus &&
              option.roleId == role.id,
        )
        .firstOrNull;
    _selectedOption = _isIeltsSpeakingScenario(_selectedScenario!.id)
        ? fullSimulation ?? roleFocus ?? compatibleOptions.firstOrNull
        : roleFocus ?? fullSimulation ?? compatibleOptions.firstOrNull;
    notifyListeners();
    return hasCompleteSelection;
  }

  void showScenarioList() {
    if (_disposed) {
      return;
    }
    _selectionEpoch++;
    _selectedScenario = null;
    _detail = null;
    _roles = const <PreparationRole>[];
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
    _scenarioLoad = null;
    _scenarios = const <PreparationScenario>[];
    _selectedScenario = null;
    _detail = null;
    _roles = const <PreparationRole>[];
    _selectedRole = null;
    _selectedOption = null;
    _errorMessage = null;
    _failedRequest = null;
    _loadingScenarios = false;
    _loadingDetail = false;
    _ieltsQuestionBank = null;
    _ieltsErrorMessage = null;
    _loadingIeltsQuestionBank = false;
    _ieltsQuestionBankLoad = null;
    _accountId = null;
    _clearIeltsProgress();
    await client.clearAccountState();
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
    String scenarioId,
  ) =>
      _isCurrentAccount(accountEpoch) &&
      selectionEpoch == _selectionEpoch &&
      _selectedScenario?.id == scenarioId;

  @override
  void dispose() {
    _disposed = true;
    _accountEpoch++;
    _selectionEpoch++;
    super.dispose();
  }
}

final class _FailedPreparationRequest {
  const _FailedPreparationRequest({this.scenario});

  final PreparationScenario? scenario;
}

void _validateAggregate({
  required PreparationScenario summary,
  required PreparationScenarioDetail detail,
  required List<PreparationRole> roles,
}) {
  final scenario = detail.scenario;
  if (scenario.id != summary.id ||
      scenario.version != summary.version ||
      scenario.type != summary.type ||
      scenario.model != summary.model ||
      scenario.name != summary.name ||
      scenario.status != 'active' ||
      detail.config.scenarioId != scenario.id ||
      detail.config.type != scenario.type ||
      detail.config.model != scenario.model ||
      roles.isEmpty ||
      roles.any((role) => role.scenarioId != scenario.id)) {
    throw const PreparationCatalogException(
      kind: PreparationCatalogFailureKind.invalidResponse,
    );
  }

  final roleIds = roles.map((role) => role.id).toSet();
  if (roleIds.length != roles.length) {
    throw const PreparationCatalogException(
      kind: PreparationCatalogFailureKind.invalidResponse,
    );
  }

  var fullSimulationCount = 0;
  final focusRoleIds = <String>{};
  for (final option in detail.options) {
    if (option.scenarioId != scenario.id) {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.invalidResponse,
      );
    }
    switch (option.type) {
      case PreparationOptionType.fullSimulation:
        fullSimulationCount++;
        if (option.roleId != null) {
          throw const PreparationCatalogException(
            kind: PreparationCatalogFailureKind.invalidResponse,
          );
        }
      case PreparationOptionType.focus:
        final roleId = option.roleId;
        if (roleId == null ||
            !roleIds.contains(roleId) ||
            !focusRoleIds.add(roleId)) {
          throw const PreparationCatalogException(
            kind: PreparationCatalogFailureKind.invalidResponse,
          );
        }
    }
  }
  if (fullSimulationCount != 1 ||
      focusRoleIds.length != roles.length ||
      !focusRoleIds.containsAll(roleIds)) {
    throw const PreparationCatalogException(
      kind: PreparationCatalogFailureKind.invalidResponse,
    );
  }
}

const _catalogUnavailableMessage = '练习目录暂时无法加载，请检查网络后重试。';
const _catalogInvalidMessage = '练习目录响应无法识别，请稍后重试。';

bool _isIeltsSpeakingScenario(String scenarioId) =>
    scenarioId == _ieltsSpeakingFullMockScenarioId ||
    scenarioId == 'scn_ielts_speaking_part_1' ||
    scenarioId == 'scn_ielts_speaking_part_2' ||
    scenarioId == 'scn_ielts_speaking_part_3';

String _messageFor(PreparationCatalogException error) {
  return switch (error.kind) {
    PreparationCatalogFailureKind.network ||
    PreparationCatalogFailureKind.unavailable => _catalogUnavailableMessage,
    PreparationCatalogFailureKind.invalidResponse => _catalogInvalidMessage,
    PreparationCatalogFailureKind.superseded => '',
  };
}
