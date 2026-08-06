import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class PreparationController extends ChangeNotifier {
  PreparationController({required this.client});

  final SceneClient client;

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

  List<PracticeOption> get availableOptions {
    final role = _selectedRole;
    final sceneDetail = _detail;
    if (role == null || sceneDetail == null) {
      return const <PracticeOption>[];
    }
    return List<PracticeOption>.unmodifiable(
      sceneDetail.practiceOptions.where(
        (option) => option.roleId == null || option.roleId == role.id,
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
            .where((option) => option.mode == PracticeMode.fullSimulation)
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

  bool selectRecommendedConfiguration({PracticeMode? practiceMode}) {
    if (_disposed ||
        _selectedScene == null ||
        _detail == null ||
        _roles.isEmpty) {
      return false;
    }
    final role = _roles.first;
    _selectedRole = role;
    final compatibleOptions = _detail!.practiceOptions
        .where(
          (option) =>
              (practiceMode == null || option.mode == practiceMode) &&
              (option.roleId == null || option.roleId == role.id),
        )
        .toList(growable: false);
    final fullSimulation = compatibleOptions
        .where((option) => option.mode == PracticeMode.fullSimulation)
        .firstOrNull;
    final roleFocus = compatibleOptions
        .where(
          (option) =>
              option.mode == PracticeMode.focus && option.roleId == role.id,
        )
        .firstOrNull;
    _selectedOption = practiceMode != null
        ? compatibleOptions.firstOrNull
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
    if (!_disposed) {
      notifyListeners();
    }
  }

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
      detail.experience != summary.experience ||
      detail.category != summary.category ||
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
    switch (option.mode) {
      case PracticeMode.fullSimulation:
        fullSimulationCount++;
        if (option.roleId != null) {
          throw const SceneClientException(
            kind: SceneClientFailureKind.invalidResponse,
          );
        }
      case PracticeMode.focus:
        final roleId = option.roleId;
        if (roleId == null ||
            !roleIds.contains(roleId) ||
            !focusRoleIds.add(roleId)) {
          throw const SceneClientException(
            kind: SceneClientFailureKind.invalidResponse,
          );
        }
      case PracticeMode.fullMock ||
          PracticeMode.part1 ||
          PracticeMode.part2 ||
          PracticeMode.part3:
        if (option.roleId != null) {
          throw const SceneClientException(
            kind: SceneClientFailureKind.invalidResponse,
          );
        }
    }
  }
  final validOptions = switch (detail.experience) {
    PracticeExperience.ieltsSpeaking =>
      detail.practiceOptions
              .map((option) => option.mode)
              .toSet()
              .containsAll(const {
                PracticeMode.fullMock,
                PracticeMode.part1,
                PracticeMode.part2,
                PracticeMode.part3,
              }) &&
          detail.practiceOptions.length == 4,
    PracticeExperience.interview ||
    PracticeExperience.workplace ||
    PracticeExperience.lifeAndTravel =>
      fullSimulationCount == 1 &&
          focusRoleIds.length == detail.roles.length &&
          focusRoleIds.containsAll(roleIds),
  };
  if (!validOptions) {
    throw const SceneClientException(
      kind: SceneClientFailureKind.invalidResponse,
    );
  }
}

const _catalogUnavailableMessage = '练习目录暂时无法加载，请检查网络后重试。';
const _catalogInvalidMessage = '练习目录响应无法识别，请稍后重试。';

String _messageFor(SceneClientException error) {
  return switch (error.kind) {
    SceneClientFailureKind.network ||
    SceneClientFailureKind.unavailable => _catalogUnavailableMessage,
    SceneClientFailureKind.invalidResponse => _catalogInvalidMessage,
  };
}
