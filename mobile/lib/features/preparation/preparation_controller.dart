import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

const _ieltsSpeakingFullMockScenarioId = 'scn_ielts_speaking_full';

final class PreparationController extends ChangeNotifier {
  PreparationController({required this.client});

  final PreparationCatalogClient client;

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
    _selectedOption = _selectedScenario!.id == _ieltsSpeakingFullMockScenarioId
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
    await client.clearAccountState();
    if (!_disposed) {
      notifyListeners();
    }
  }

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

String _messageFor(PreparationCatalogException error) {
  return switch (error.kind) {
    PreparationCatalogFailureKind.network ||
    PreparationCatalogFailureKind.unavailable => _catalogUnavailableMessage,
    PreparationCatalogFailureKind.invalidResponse => _catalogInvalidMessage,
    PreparationCatalogFailureKind.superseded => '',
  };
}
