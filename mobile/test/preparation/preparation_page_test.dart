import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

void main() {
  testWidgets(
    'loads the server catalog and keeps perspectives independent of stages',
    (tester) async {
      final controller = PreparationController(client: _FixtureClient());
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(home: PreparationPage(preparationController: controller)),
      );
      await tester.pumpAndSettle();

      expect(find.textContaining('通用职业英语 Agent'), findsOneWidget);
      expect(find.textContaining('技术岗位英文面试'), findsOneWidget);
      await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
      await tester.pumpAndSettle();

      expect(
        find.text('English interview for technical roles'),
        findsOneWidget,
      );
      expect(find.text('每个视角都可以独立练习，排列顺序不代表固定招聘阶段。'), findsOneWidget);
      final technical = find.byKey(
        const Key('preparation-role-role_technical_interviewer'),
      );
      final recruiter = find.byKey(
        const Key('preparation-role-role_hr_interviewer'),
      );
      expect(
        tester.getTopLeft(technical).dy,
        lessThan(tester.getTopLeft(recruiter).dy),
      );

      await tester.ensureVisible(technical);
      await tester.pumpAndSettle();
      await tester.tap(technical);
      await tester.pump();
      final fullSimulation = find.byKey(
        const Key('preparation-option-option_full_simulation'),
      );
      await tester.scrollUntilVisible(fullSimulation, 200);
      await tester.pumpAndSettle();
      expect(fullSimulation, findsOneWidget);
      expect(
        find.byKey(const Key('preparation-option-option_technical_focus')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('preparation-option-option_hr_focus')),
        findsNothing,
      );

      await tester.tap(fullSimulation);
      await tester.pump();
      final selectionNotice = find.byKey(
        const Key('preparation-read-only-selection'),
      );
      await tester.scrollUntilVisible(selectionNotice, 200);
      await tester.pumpAndSettle();

      expect(selectionNotice, findsOneWidget);
      final startButton = tester.widget<FilledButton>(
        find.byKey(const Key('preparation-start-unavailable')),
      );
      expect(startButton.onPressed, isNull);
      expect(find.textContaining('尚未创建练习或写入业务数据'), findsOneWidget);
    },
  );

  testWidgets('shows loading, failure retry, and empty states', (tester) async {
    final client = _ControlledListClient();
    final controller = PreparationController(client: client);
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PreparationPage(preparationController: controller)),
    );
    await tester.pump();
    expect(
      find.byKey(const Key('preparation-catalog-loading')),
      findsOneWidget,
    );

    client.first.completeError(
      const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.network,
        retryable: true,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preparation-catalog-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('preparation-catalog-retry')));
    await tester.pump();
    client.second.complete(const <PreparationScenario>[]);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preparation-catalog-empty')), findsOneWidget);
  });

  testWidgets('remains usable on a narrow screen with large text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    final controller = PreparationController(client: _FixtureClient());
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PreparationPage(preparationController: controller)),
    );
    await tester.pumpAndSettle();
    final scenario = find.byKey(const Key('catalog-scenario-$_scenarioId'));
    await tester.ensureVisible(scenario);
    await tester.pumpAndSettle();
    await tester.tap(scenario);
    await tester.pumpAndSettle();

    final leadership = find.byKey(
      const Key('preparation-role-role_executive_interviewer'),
    );
    await tester.scrollUntilVisible(
      leadership,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(leadership.hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'catalog exploration preserves the same Agent Thread and Matter',
    (tester) async {
      final agentController = AgentController(client: FakeAgentClient());
      final preparationController = PreparationController(
        client: _FixtureClient(),
      );
      addTearDown(agentController.dispose);
      addTearDown(preparationController.dispose);
      await agentController.initialize();
      await agentController.selectScene(agentScenes.first);
      final originalThreadId = agentController.threadId;
      final originalMatterId = agentController.activeMatter?.id;

      await tester.pumpWidget(
        MaterialApp(
          home: SpeakUpShell(
            agentController: agentController,
            preparationController: preparationController,
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('primary-tab-scenes')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('primary-tab-agent')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(agentController.threadId, originalThreadId);
      expect(agentController.activeMatter?.id, originalMatterId);
    },
  );
}

class _FixtureClient implements PreparationCatalogClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) async =>
      _detail;

  @override
  Future<List<PreparationScenario>> listScenarios() async => const [_scenario];

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) async => _roles;
}

final class _ControlledListClient implements PreparationCatalogClient {
  final Completer<List<PreparationScenario>> first =
      Completer<List<PreparationScenario>>();
  final Completer<List<PreparationScenario>> second =
      Completer<List<PreparationScenario>>();
  int calls = 0;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) {
    throw UnimplementedError();
  }

  @override
  Future<List<PreparationScenario>> listScenarios() {
    calls++;
    return calls == 1 ? first.future : second.future;
  }

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) {
    throw UnimplementedError();
  }
}

const _scenarioId = 'scn_programmer_interview';

const _scenario = PreparationScenario(
  id: _scenarioId,
  type: 'INTERVIEW',
  name: 'English interview for technical roles',
  version: 1,
  status: 'active',
);

const _config = PreparationScenarioConfig(
  id: 'scfg_backend_engineer',
  scenarioId: _scenarioId,
  type: 'INTERVIEW',
  version: 1,
  jobTitle: 'Backend engineer',
  jobDescription: 'Build reliable APIs and explain engineering trade-offs.',
  focusAreas: ['introduction', 'system_design'],
);

const _technicalRole = PreparationRole(
  id: 'role_technical_interviewer',
  scenarioId: _scenarioId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical depth perspective',
  responsibilities: 'Probe technical depth and decision making.',
  style: 'Precise and evidence seeking.',
  focusAreas: ['system_design'],
  version: 1,
);

const _recruiterRole = PreparationRole(
  id: 'role_hr_interviewer',
  scenarioId: _scenarioId,
  type: 'HR_INTERVIEWER',
  displayName: 'Recruiter and motivation perspective',
  responsibilities: 'Explore motivation and communication clarity.',
  style: 'Warm and structured.',
  focusAreas: ['motivation'],
  version: 1,
);

const _projectRole = PreparationRole(
  id: 'role_project_manager',
  scenarioId: _scenarioId,
  type: 'PROJECT_MANAGER',
  displayName: 'Delivery and collaboration perspective',
  responsibilities: 'Explore delivery and collaboration.',
  style: 'Outcome oriented.',
  focusAreas: ['delivery'],
  version: 1,
);

const _leadershipRole = PreparationRole(
  id: 'role_executive_interviewer',
  scenarioId: _scenarioId,
  type: 'EXECUTIVE_INTERVIEWER',
  displayName: 'Leadership and impact perspective',
  responsibilities: 'Optional for senior, lead, or management roles.',
  style: 'Concise and high level.',
  focusAreas: ['impact'],
  version: 1,
);

const _roles = [_technicalRole, _recruiterRole, _projectRole, _leadershipRole];

const _detail = PreparationScenarioDetail(
  scenario: _scenario,
  config: _config,
  options: [
    PreparationOption(
      id: 'option_full_simulation',
      scenarioId: _scenarioId,
      type: PreparationOptionType.fullSimulation,
      displayName: 'Full simulation',
      version: 1,
    ),
    PreparationOption(
      id: 'option_technical_focus',
      scenarioId: _scenarioId,
      roleId: 'role_technical_interviewer',
      type: PreparationOptionType.focus,
      displayName: 'Technical depth focus',
      version: 1,
    ),
    PreparationOption(
      id: 'option_hr_focus',
      scenarioId: _scenarioId,
      roleId: 'role_hr_interviewer',
      type: PreparationOptionType.focus,
      displayName: 'Recruiter and motivation focus',
      version: 1,
    ),
    PreparationOption(
      id: 'option_project_manager_focus',
      scenarioId: _scenarioId,
      roleId: 'role_project_manager',
      type: PreparationOptionType.focus,
      displayName: 'Delivery and collaboration focus',
      version: 1,
    ),
    PreparationOption(
      id: 'option_executive_focus',
      scenarioId: _scenarioId,
      roleId: 'role_executive_interviewer',
      type: PreparationOptionType.focus,
      displayName: 'Leadership and impact focus',
      version: 1,
    ),
  ],
);
