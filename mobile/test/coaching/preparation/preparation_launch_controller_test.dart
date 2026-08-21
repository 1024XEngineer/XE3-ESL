import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/preparation_contract_fixtures.dart';

void main() {
  test(
    'direct Scene launch creates Plan, Session, then activates Voice',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);
      harness.controller.updateBackgroundSummary(contractBackground);

      final started = await harness.controller.start(
        PreparationLaunchSelection.fromCatalog(
          scene: contractScene,
          role: contractScene.roles.single,
          option: contractScene.practiceOptions.single,
        ),
      );

      expect(started, isTrue);
      expect(harness.client.planInputs, hasLength(1));
      expect(
        harness.client.planInputs.single.backgroundSummary,
        contractBackground,
      );
      expect(harness.client.sessionInputs.single.expectedPlanVersion, 1);
      expect(harness.voiceActivations, <String>[contractSessionId]);
      expect(harness.workspace.currentSessionId, contractSessionId);
      expect(harness.controller.bootstrap?.session.planVersion, 1);
    },
  );

  test('custom scenario is embedded once as Plan background', () async {
    final harness = await _Harness.create();
    addTearDown(harness.dispose);
    const context = ScenarioPreparationContext(
      situation: 'A delayed product launch',
      userRole: 'Engineering lead',
      counterpartRole: 'Product director',
      goal: 'Explain risk and agree on scope',
      counterpartPersona: 'Direct and evidence seeking',
    );

    final started = await harness.controller.start(
      PreparationLaunchSelection.fromCatalog(
        scene: contractScene,
        role: contractScene.roles.single,
        option: contractScene.practiceOptions.single,
      ),
      scenarioContext: context,
    );

    expect(started, isTrue);
    expect(
      harness.client.planInputs.single.backgroundSummary,
      '情境：A delayed product launch\n'
      '我的角色：Engineering lead\n'
      '对方角色：Product director\n'
      '练习目标：Explain risk and agree on scope\n'
      '对方设定：Direct and evidence seeking',
    );
    expect(harness.client.planInputs, hasLength(1));
    expect(harness.client.sessionInputs, hasLength(1));
  });
}

final class _Harness {
  _Harness._({
    required this.practice,
    required this.workspace,
    required this.client,
    required this.controller,
    required this.voiceActivations,
  });

  final PracticeController practice;
  final PracticeWorkspaceController workspace;
  final _FakePreparationLaunchClient client;
  final PreparationLaunchController controller;
  final List<String> voiceActivations;

  static Future<_Harness> create() async {
    final practice = PracticeController(
      client: FakePracticeClient(
        planId: contractPlanId,
        practiceExperience: PracticeExperience.interview,
        sceneCategory: SceneCategory.interviewProfessional,
        turnLimit: 6,
      ),
    );
    final workspace = PracticeWorkspaceController(
      practiceController: practice,
      recordStore: MemoryPracticeLaunchRecordStore(),
    );
    await workspace.activateAccount(contractUserId);
    final client = _FakePreparationLaunchClient();
    final voiceActivations = <String>[];
    final controller = PreparationLaunchController(
      client: client,
      workspaceController: workspace,
      idFactory: (scope) => '$scope-contract-key',
      voiceActivator:
          ({
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {
            voiceActivations.add(bootstrap.session.id);
          },
    );
    return _Harness._(
      practice: practice,
      workspace: workspace,
      client: client,
      controller: controller,
      voiceActivations: voiceActivations,
    );
  }

  void dispose() {
    controller.dispose();
    workspace.dispose();
    practice.dispose();
  }
}

final class _FakePreparationLaunchClient implements PreparationLaunchClient {
  final List<CreatePracticePlanInput> planInputs = <CreatePracticePlanInput>[];
  final List<CreatePreparationSessionInput> sessionInputs =
      <CreatePreparationSessionInput>[];

  @override
  Future<PracticePlan> createPlan({
    required CreatePracticePlanInput input,
    required String idempotencyKey,
  }) async {
    planInputs.add(input);
    return contractPlan();
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    sessionInputs.add(input);
    return contractBootstrap(plan);
  }

  @override
  Future<PracticePlan> getPlan(String planId) async => contractPlan();

  @override
  Future<PracticePlan> confirmPlan({
    required String planId,
    required int expectedVersion,
    required String idempotencyKey,
  }) async => contractPlan();

  @override
  Future<void> clearAccountState() async {}
}
