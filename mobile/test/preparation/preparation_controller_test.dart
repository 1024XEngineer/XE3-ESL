import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

void main() {
  test(
    'keeps role order optional and filters options for the selected role',
    () async {
      final controller = PreparationController(client: _FixtureCatalogClient());
      addTearDown(controller.dispose);

      await controller.loadIfNeeded();
      await controller.selectScenario(_scenario);

      expect(controller.roles, [_technicalRole, _recruiterRole]);
      expect(controller.selectedRole, isNull);
      expect(controller.availableOptions, isEmpty);

      controller.selectRole(_technicalRole);
      expect(controller.availableOptions.map((option) => option.id), [
        'option_full_simulation',
        'option_technical_focus',
      ]);
      controller.selectOption(_fullOption);
      expect(controller.hasCompleteSelection, isTrue);

      controller.selectRole(_recruiterRole);
      expect(controller.selectedOption, isNull);
      expect(controller.availableOptions.map((option) => option.id), [
        'option_full_simulation',
        'option_hr_focus',
      ]);
    },
  );

  test('retries the failed directory request', () async {
    final client = _FailOnceCatalogClient();
    final controller = PreparationController(client: client);
    addTearDown(controller.dispose);

    await controller.loadIfNeeded();
    expect(controller.errorMessage, isNotNull);
    expect(controller.scenarios, isEmpty);

    await controller.retryLastFailure();
    expect(controller.errorMessage, isNull);
    expect(controller.scenarios, [_scenario]);
    expect(client.listCalls, 2);
  });

  test(
    'account cleanup clears selection and ignores a late response',
    () async {
      final client = _ControlledCatalogClient();
      final controller = PreparationController(client: client);
      addTearDown(controller.dispose);

      final load = controller.loadIfNeeded();
      await controller.clearPrivateState();
      client.listResponse.complete(const [_scenario]);
      await load;

      expect(controller.scenarios, isEmpty);
      expect(controller.selectedScenario, isNull);
      expect(controller.selectedRole, isNull);
      expect(controller.selectedOption, isNull);
      expect(controller.errorMessage, isNull);
      expect(controller.isLoadingScenarios, isFalse);
    },
  );

  test('rejects a catalog that omits one role focus option', () async {
    final controller = PreparationController(
      client: _FixtureCatalogClient(
        detail: const PreparationScenarioDetail(
          scenario: _scenario,
          config: _config,
          options: [_fullOption, _technicalFocus],
        ),
      ),
    );
    addTearDown(controller.dispose);

    await controller.loadIfNeeded();
    await controller.selectScenario(_scenario);

    expect(controller.detail, isNull);
    expect(controller.roles, isEmpty);
    expect(controller.errorMessage, '练习目录响应无法识别，请稍后重试。');
  });
}

class _FixtureCatalogClient implements PreparationCatalogClient {
  _FixtureCatalogClient({this.detail = _detail});

  final PreparationScenarioDetail detail;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) async =>
      detail;

  @override
  Future<List<PreparationScenario>> listScenarios() async => const [_scenario];

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) async => const [
    _technicalRole,
    _recruiterRole,
  ];
}

final class _FailOnceCatalogClient extends _FixtureCatalogClient {
  int listCalls = 0;

  @override
  Future<List<PreparationScenario>> listScenarios() async {
    listCalls++;
    if (listCalls == 1) {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.network,
        retryable: true,
      );
    }
    return const [_scenario];
  }
}

final class _ControlledCatalogClient implements PreparationCatalogClient {
  final Completer<List<PreparationScenario>> listResponse =
      Completer<List<PreparationScenario>>();

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) {
    throw UnimplementedError();
  }

  @override
  Future<List<PreparationScenario>> listScenarios() => listResponse.future;

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) {
    throw UnimplementedError();
  }
}

const _scenarioId = 'scn_programmer_interview';

const _scenario = PreparationScenario(
  id: _scenarioId,
  type: 'INTERVIEW',
  model: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  name: 'English interview for technical roles',
  summary: 'Discuss one backend project.',
  version: 1,
  status: 'active',
);

const _config = PreparationScenarioConfig(
  id: 'scfg_backend_engineer',
  scenarioId: _scenarioId,
  type: 'INTERVIEW',
  model: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  version: 1,
  jobTitle: 'Backend engineer',
  jobDescription: 'Build reliable APIs.',
  prompt: _prompt,
);

const _prompt = PreparationScenarioPrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: ['system_design'],
  turnBlueprints: ['Ask for a project overview.'],
  suggestedDurationSeconds: 900,
);

const _technicalRole = PreparationRole(
  id: 'role_technical_interviewer',
  scenarioId: _scenarioId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical depth perspective',
  responsibilities: 'Probe technical depth.',
  style: 'Precise and evidence seeking.',
  focusAreas: ['system_design'],
  version: 1,
);

const _recruiterRole = PreparationRole(
  id: 'role_hr_interviewer',
  scenarioId: _scenarioId,
  type: 'HR_INTERVIEWER',
  displayName: 'Recruiter and motivation perspective',
  responsibilities: 'Explore motivation.',
  style: 'Warm and structured.',
  focusAreas: ['motivation'],
  version: 1,
);

const _fullOption = PreparationOption(
  id: 'option_full_simulation',
  scenarioId: _scenarioId,
  type: PreparationOptionType.fullSimulation,
  displayName: 'Full simulation',
  version: 1,
);

const _technicalFocus = PreparationOption(
  id: 'option_technical_focus',
  scenarioId: _scenarioId,
  roleId: 'role_technical_interviewer',
  type: PreparationOptionType.focus,
  displayName: 'Technical depth focus',
  version: 1,
);

const _recruiterFocus = PreparationOption(
  id: 'option_hr_focus',
  scenarioId: _scenarioId,
  roleId: 'role_hr_interviewer',
  type: PreparationOptionType.focus,
  displayName: 'Recruiter and motivation focus',
  version: 1,
);

const _detail = PreparationScenarioDetail(
  scenario: _scenario,
  config: _config,
  options: [_fullOption, _technicalFocus, _recruiterFocus],
);
