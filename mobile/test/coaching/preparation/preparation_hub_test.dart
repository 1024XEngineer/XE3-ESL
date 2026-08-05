import '../../support/scene_fixtures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';

void main() {
  testWidgets('shows exactly the three product-level practice entries', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PreparationPage(preparationController: controller)),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-hub-interview')), findsOneWidget);
    expect(find.byKey(const Key('practice-hub-exam')), findsOneWidget);
    expect(find.byKey(const Key('practice-hub-roleplay')), findsOneWidget);
    expect(find.byKey(const Key('practice-continuation')), findsNothing);
    expect(find.text('最近练习'), findsNothing);
    expect(find.byKey(const Key('preparation-family-INTERVIEW')), findsNothing);
    expect(find.byKey(const Key('preparation-family-EXAM')), findsNothing);
    expect(find.byKey(const Key('preparation-family-WORKPLACE')), findsNothing);
    expect(find.byKey(const Key('preparation-family-DAILY')), findsNothing);
  });

  testWidgets('opens the English interview module from the hub', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    await _openModule(tester, const Key('practice-hub-interview'));

    expect(find.text('英文面试'), findsOneWidget);
    expect(find.byKey(const Key('interview-mode-hr')), findsOneWidget);
    expect(
      find.byKey(const Key('interview-mode-professional')),
      findsOneWidget,
    );
    expect(find.text('英文自我介绍'), findsNothing);
    expect(find.text('案例面试'), findsNothing);

    await tester.tap(find.byKey(const Key('interview-mode-hr')));
    await tester.pumpAndSettle();
    expect(find.text('英文自我介绍'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('interview-mode-professional')));
    await tester.pumpAndSettle();
    expect(find.text('案例面试'), findsOneWidget);
    expect(find.text('IELTS 口语完整模拟'), findsNothing);
    expect(find.text('进度与风险汇报'), findsNothing);
  });

  testWidgets(
    'opens IELTS as a fixed exam flow without the custom speaking exam',
    (tester) async {
      final controller = PreparationController(client: _HubFixtureClient());
      addTearDown(controller.dispose);
      await _pumpHub(tester, controller);

      await _openModule(tester, const Key('practice-hub-exam'));

      expect(find.text('IELTS 口语'), findsOneWidget);
      expect(find.text('一次完成三个 Part'), findsOneWidget);
      expect(find.text('按 Part 专项突破'), findsOneWidget);
      expect(find.byKey(const Key('ielts-mode-full')), findsOneWidget);
      expect(find.byKey(const Key('ielts-mode-special')), findsOneWidget);
      expect(find.byKey(const Key('ielts-browser-search')), findsNothing);
      expect(find.text('自定义口语考试'), findsNothing);
      expect(find.text('英文自我介绍'), findsNothing);
    },
  );

  testWidgets('opens IELTS section cards before starting practice', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);
    await _openModule(tester, const Key('practice-hub-exam'));
    await _openIeltsBrowser(tester);

    await tester.tap(find.byKey(const Key('ielts-part-part2')));
    await tester.pumpAndSettle();

    expect(find.text('共 1 道专项题'), findsOneWidget);
    expect(
      find.text('Describe a skill you would like to learn'),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-browser-card-part2-p23-001')),
      findsOneWidget,
    );
  });

  testWidgets('starts full mock and the exact selected specialty card', (
    tester,
  ) async {
    final catalog = PreparationController(client: _HubFixtureClient());
    final launchClient = _HubLaunchClient();
    final launch = _hubLaunchController(launchClient);
    var starts = 0;
    addTearDown(catalog.dispose);
    addTearDown(launch.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          preparationController: catalog,
          launchController: launch,
          onPracticeStarted: () => starts++,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await _openModule(tester, const Key('practice-hub-exam'));
    await _openIeltsBrowser(tester);
    final part1Card = find.byKey(
      const Key('ielts-browser-card-part1-p1-topic-001'),
    );
    await tester.scrollUntilVisible(
      part1Card,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(
      find.descendant(of: part1Card, matching: find.byType(InkWell)),
    );
    await tester.pumpAndSettle();

    expect(catalog.errorMessage, isNull);
    expect(launch.errorMessage, isNull);
    expect(launchClient.lastPlanInput, isNotNull);
    expect(starts, 1);
    expect(
      launchClient.lastPlanInput?.ieltsSelection,
      const IeltsPracticeSelection(
        mode: IeltsPracticeMode.part1,
        part1SetId: 'p1-topic-001',
      ),
    );

    await _openModule(tester, const Key('practice-hub-exam'));
    await tester.tap(find.byKey(const Key('ielts-mode-full')));
    await tester.pumpAndSettle();

    expect(starts, 2);
    expect(
      launchClient.lastPlanInput?.ieltsSelection,
      const IeltsPracticeSelection(
        mode: IeltsPracticeMode.fullMock,
        part1SetId: 'p1-001',
        topicGroupId: 'p23-001',
      ),
    );
  });

  testWidgets('filters and searches IELTS topic cards by intersection', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);
    await _openModule(tester, const Key('practice-hub-exam'));
    await _openIeltsBrowser(tester);

    expect(find.text('共 3 道专项题'), findsOneWidget);
    await tester.enterText(
      find.byKey(const Key('ielts-browser-search')),
      'Music',
    );
    await tester.pumpAndSettle();
    expect(find.text('共 1 道专项题'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-browser-card-part1-p1-topic-001')),
      findsOneWidget,
    );

    await tester.enterText(find.byKey(const Key('ielts-browser-search')), '');
    await tester.tap(find.byKey(const Key('ielts-part-part3')));
    await tester.tap(find.byKey(const Key('ielts-category-event')));
    await tester.pumpAndSettle();
    expect(find.text('共 1 道专项题'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-browser-card-part3-p23-001')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('ielts-source-evergreen')));
    await tester.pumpAndSettle();
    expect(find.text('共 0 道专项题'), findsOneWidget);
    expect(find.text('没有找到符合条件的题目'), findsOneWidget);
  });

  testWidgets('keeps IELTS browser filters when returning to the catalog', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);
    await _openModule(tester, const Key('practice-hub-exam'));
    await _openIeltsBrowser(tester);

    await tester.tap(find.byKey(const Key('ielts-part-part3')));
    await tester.tap(find.byKey(const Key('ielts-category-event')));
    await tester.enterText(
      find.byKey(const Key('ielts-browser-search')),
      'skill',
    );
    await tester.pumpAndSettle();
    expect(find.text('共 1 道专项题'), findsOneWidget);

    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('ielts-mode-special')), findsOneWidget);
    await _openIeltsBrowser(tester);

    expect(find.text('共 1 道专项题'), findsOneWidget);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('ielts-browser-search')))
          .controller!
          .text,
      'skill',
    );
    expect(
      find.byKey(const Key('ielts-browser-card-part3-p23-001')),
      findsOneWidget,
    );
  });

  testWidgets('opens the specialty browser with its exit control visible', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);
    await _openModule(tester, const Key('practice-hub-exam'));

    final specialty = find.byKey(const Key('ielts-mode-special'));
    await tester.scrollUntilVisible(
      specialty,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(specialty);
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('preparation-back-to-families')).hitTestable(),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-special-browser-title')).hitTestable(),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-browser-search')).hitTestable(),
      findsOneWidget,
    );
  });

  testWidgets('combines workplace and daily templates in AI roleplay', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    await _openModule(tester, const Key('practice-hub-roleplay'));

    expect(find.text('情景对话'), findsOneWidget);
    expect(find.text('进度与风险汇报'), findsOneWidget);
    expect(find.text('酒店入住与问题处理'), findsOneWidget);
    expect(find.byKey(const Key('roleplay-filter-workplace')), findsOneWidget);
    expect(find.byKey(const Key('roleplay-filter-travel')), findsOneWidget);
    expect(find.text('英文自我介绍'), findsNothing);
    expect(find.text('IELTS 口语完整模拟'), findsNothing);
    expect(find.text('自定义职场沟通'), findsNothing);
    expect(find.text('自定义日常交流'), findsNothing);
    expect(find.byKey(const Key('roleplay-custom-reserved')), findsOneWidget);
  });

  testWidgets('keeps the three entries usable at 320px and 3x text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 3;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    for (final entryData in const [
      (key: Key('practice-hub-interview'), title: '英文面试'),
      (key: Key('practice-hub-exam'), title: 'IELTS 口语'),
      (key: Key('practice-hub-roleplay'), title: '情景对话'),
    ]) {
      final entry = find.byKey(entryData.key);
      await tester.scrollUntilVisible(
        entry,
        180,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      expect(find.text(entryData.title).hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);
    }
  });

  testWidgets('keeps interview and roleplay cards usable at 3x text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 3;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    await _openModule(tester, const Key('practice-hub-interview'));
    final interviewMode = find.byKey(const Key('interview-mode-professional'));
    await tester.scrollUntilVisible(
      interviewMode,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(interviewMode.hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);

    final back = find.byKey(const Key('preparation-back-to-families'));
    await tester.scrollUntilVisible(
      back,
      -180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(back);
    await tester.pumpAndSettle();

    await _openModule(tester, const Key('practice-hub-roleplay'));
    final roleplayScene = find.byKey(
      const Key('catalog-scene-scn_daily_hotel_checkin_issue'),
    );
    await tester.scrollUntilVisible(
      roleplayScene,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(roleplayScene.hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('opens a scrolled product hub from the top', (tester) async {
    tester.view.physicalSize = const Size(390, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    final entry = find.byKey(const Key('practice-hub-roleplay'));
    await tester.scrollUntilVisible(
      entry,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.drag(find.byType(Scrollable).first, const Offset(0, -100));
    await tester.pumpAndSettle();
    await tester.tap(entry);
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('preparation-back-to-families')).hitTestable(),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('practice-hub-title-roleplay')).hitTestable(),
      findsOneWidget,
    );
  });

  testWidgets('keeps exam and roleplay modules usable in landscape', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(844, 390);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    for (final module in const [
      (
        entry: Key('practice-hub-exam'),
        title: Key('practice-hub-title-ielts'),
        scene: Key('ielts-mode-special'),
      ),
      (
        entry: Key('practice-hub-roleplay'),
        title: Key('practice-hub-title-roleplay'),
        scene: Key('catalog-scene-scn_workplace_progress_risk_update'),
      ),
    ]) {
      final entry = find.byKey(module.entry);
      await tester.scrollUntilVisible(
        entry,
        120,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.tapAt(tester.getTopLeft(entry) + const Offset(24, 24));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('preparation-back-to-families')).hitTestable(),
        findsOneWidget,
      );
      expect(find.byKey(module.title).hitTestable(), findsOneWidget);
      final scene = find.byKey(module.scene);
      await tester.scrollUntilVisible(
        scene,
        140,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      expect(scene.hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);

      final back = find.byKey(const Key('preparation-back-to-families'));
      await tester.scrollUntilVisible(
        back,
        -140,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.tap(back);
      await tester.pumpAndSettle();
    }
  });

  testWidgets('exposes one clear button semantic for each product entry', (
    tester,
  ) async {
    final semantics = tester.ensureSemantics();
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    try {
      await _pumpHub(tester, controller);

      for (final entryData in const [
        (key: Key('practice-hub-interview'), label: '英文面试。模拟面试与轮次专项练习'),
        (key: Key('practice-hub-exam'), label: 'IELTS 口语。Part 1、2、3 与完整模考'),
        (key: Key('practice-hub-roleplay'), label: '情景对话。工作、旅行与日常英语实战'),
      ]) {
        final entry = find.byKey(entryData.key);
        await tester.scrollUntilVisible(
          entry,
          180,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.pumpAndSettle();
        expect(
          tester.getSemantics(entry),
          isSemantics(
            label: entryData.label,
            isButton: true,
            hasTapAction: true,
          ),
        );
      }
    } finally {
      semantics.dispose();
    }
  });
}

Future<void> _pumpHub(
  WidgetTester tester,
  PreparationController controller,
) async {
  await tester.pumpWidget(
    MaterialApp(home: PreparationPage(preparationController: controller)),
  );
  await tester.pumpAndSettle();
}

Future<void> _openModule(WidgetTester tester, Key key) async {
  final entry = find.byKey(key);
  for (var attempt = 0; attempt < 12 && entry.evaluate().isEmpty; attempt++) {
    await tester.drag(find.byType(Scrollable).first, const Offset(0, -180));
    await tester.pumpAndSettle();
  }
  expect(entry, findsOneWidget);
  await tester.ensureVisible(entry);
  await tester.pumpAndSettle();
  await tester.tap(entry);
  await tester.pumpAndSettle();
}

Future<void> _openIeltsBrowser(WidgetTester tester) async {
  final entry = find.byKey(const Key('ielts-mode-special'));
  await tester.ensureVisible(entry);
  await tester.tap(entry);
  await tester.pumpAndSettle();
  expect(find.byKey(const Key('ielts-browser-search')), findsOneWidget);
}

final class _HubFixtureClient implements SceneClient, SceneQuestionBankClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) async {
    final scene = _hubScenes.firstWhere((item) => item.id == sceneId);
    return testScene(
      id: scene.id,
      family: scene.family,
      model: scene.model,
      name: scene.name,
      version: scene.version,
      status: scene.status,
      turnPolicyRef: scene.turnPolicyRef,
      sessionPolicyRef: scene.sessionPolicyRef,
      prompt: scene.prompt,
      roles: scene.roles,
      practiceOptions: [
        PracticeOption(
          id: 'option-${scene.id}-full',
          sceneId: scene.id,
          type: PracticeOptionType.fullSimulation,
          displayName: '完整练习',
        ),
        for (final role in scene.roles)
          PracticeOption(
            id: 'option-${scene.id}-${role.id}',
            sceneId: scene.id,
            type: PracticeOptionType.focus,
            displayName: role.displayName,
            roleId: role.id,
          ),
      ],
    );
  }

  @override
  Future<List<SceneDefinition>> listScenes() async => _hubScenes;

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async =>
      _hubScenes.firstWhere((scene) => scene.id == sceneId).roles;

  @override
  Future<IeltsQuestionBank> getIeltsQuestionBank() async => _ieltsBank;
}

PreparationLaunchController _hubLaunchController(_HubLaunchClient client) =>
    PreparationLaunchController(
      client: client,
      contextProvider: () => const AgentPracticeContext(
        threadId: 'thread-ielts-hub',
        goalId: 'goal-ielts-hub',
      ),
      threadIdProvider: () => 'thread-ielts-hub',
      goalActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) async => const AgentPracticeContext(
            threadId: 'thread-ielts-hub',
            goalId: 'goal-ielts-hub',
          ),
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {},
      idFactory: (scope) => '$scope-ielts-hub',
    );

final class _HubLaunchClient implements PreparationLaunchClient {
  CreatePreparationPlanInput? lastPlanInput;
  PreparationSnapshot? _snapshot;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async => PreparationProfile(
    id: 'profile-ielts-hub',
    userId: 'user-ielts-hub',
    backgroundSummary: input.backgroundSummary,
    version: 1,
    updatedAt: DateTime.utc(2026, 8, 5),
  );

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    final snapshot = PreparationSnapshot(
      id: 'snapshot-ielts-hub',
      sourceProfileId: profileId,
      sourceVersion: sourceVersion,
      backgroundSnapshot: 'IELTS speaking practice',
      createdAt: DateTime.utc(2026, 8, 5),
    );
    _snapshot = snapshot;
    return snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    lastPlanInput = input;
    final snapshot = _snapshot!;
    final scene = _hubScenes.firstWhere((item) => item.id == input.sceneId);
    final selection = input.ieltsSelection!;
    final assignment = switch (selection.mode) {
      IeltsPracticeMode.fullMock => const IeltsPracticeAssignment(
        bankId: 'ielts-bank-1',
        season: '2026-05-08',
        mode: IeltsPracticeMode.fullMock,
        part1SetId: 'p1-001',
        topicGroupId: 'p23-001',
        topicTitle: '学习技能',
        part2CueCard: 'Describe a skill you would like to learn',
        part1QuestionCount: 8,
        part2QuestionCount: 1,
        part3QuestionCount: 5,
        turnBlueprints: <String>[
          'P1-1',
          'P1-2',
          'P1-3',
          'P1-4',
          'P1-5',
          'P1-6',
          'P1-7',
          'P1-8',
          'P2',
          'P3-1',
          'P3-2',
          'P3-3',
          'P3-4',
          'P3-5',
        ],
      ),
      IeltsPracticeMode.part1 => const IeltsPracticeAssignment(
        bankId: 'ielts-bank-1',
        season: '2026-05-08',
        mode: IeltsPracticeMode.part1,
        part1SetId: 'p1-topic-001',
        part1QuestionCount: 4,
        part2QuestionCount: 0,
        part3QuestionCount: 0,
        turnBlueprints: <String>['P1-1', 'P1-2', 'P1-3', 'P1-4'],
      ),
      _ => throw StateError('Unexpected IELTS mode in hub launch test.'),
    };
    return PracticePlan(
      id: 'plan-ielts-hub',
      userId: 'user-ielts-hub',
      sourceThreadId: input.sourceThreadId,
      goalSnapshot: PreparationGoalSnapshot(
        id: input.goalId!,
        title: scene.name,
        version: 1,
      ),
      preparationSnapshot: snapshot,
      sceneSelection: SceneSelectionSnapshot(
        scene: scene,
        selectedRoleIds: input.selectedRoleIds,
        practiceOptionId: input.practiceOptionId,
      ),
      sessionPolicy: PreparationSessionPolicy(
        suggestedDurationSeconds: 600,
        minEffectiveTurns: assignment.turnBlueprints.length,
        maxEffectiveTurns: assignment.turnBlueprints.length,
        coverageCheckpointTurn: assignment.turnBlueprints.length,
        maxFollowUpsPerQuestion: 0,
        earlyCompletionRule: 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
        retryAllowed: false,
        questionTranslationAllowed: false,
      ),
      practiceObjectives: const <PracticeObjective>[
        PracticeObjective(id: 'ielts', description: 'Complete IELTS practice.'),
      ],
      ieltsAssignment: assignment,
      revision: 1,
      status: PracticePlanStatus.ready,
      createdAt: DateTime.utc(2026, 8, 5),
      updatedAt: DateTime.utc(2026, 8, 5),
    );
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async => PreparationPracticeBootstrap(
    session: PreparationPracticeSession(
      id: 'session-ielts-${plan.ieltsAssignment!.mode.name}',
      planId: plan.id,
      sceneFamily: plan.sceneSelection.scene.family,
      sceneModel: plan.sceneSelection.scene.model,
      snapshotId: 'session-snapshot-ielts-hub',
      status: 'starting',
      version: 1,
      createdAt: DateTime.utc(2026, 8, 5),
    ),
    preparationSnapshotId: plan.preparationSnapshot.id,
    maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
  );
}

final _ieltsBank = IeltsQuestionBank(
  bankId: 'ielts-bank-1',
  season: '2026-05-08',
  sourceCutoff: DateTime.utc(2026, 6, 18),
  part1Sets: const [
    IeltsPart1Set(
      id: 'p1-001',
      title: 'Part 1 套题 01',
      topics: [
        IeltsPart1Topic(
          title: 'Hometown',
          release: 'carry_over',
          questions: ['Q1', 'Q2'],
        ),
        IeltsPart1Topic(
          title: 'Music',
          release: 'new',
          questions: ['Q3', 'Q4', 'Q5'],
        ),
        IeltsPart1Topic(
          title: 'Parks',
          release: 'new',
          questions: ['Q6', 'Q7', 'Q8'],
        ),
      ],
      questionCount: 8,
    ),
  ],
  part1Topics: const [
    IeltsPart1PracticeTopic(
      id: 'p1-topic-001',
      titleZh: '音乐',
      titleEn: 'Music',
      release: 'new',
      category: IeltsTopicCategory.thing,
      questions: ['Q1', 'Q2', 'Q3', 'Q4'],
    ),
  ],
  topicGroups: const [
    IeltsTopicGroup(
      id: 'p23-001',
      title: '学习技能',
      release: 'new',
      category: IeltsTopicCategory.event,
      cueCard: IeltsCueCard(
        prompt: 'Describe a skill you would like to learn',
        points: ['What', 'Why', 'How', 'Benefit'],
      ),
      part3Questions: ['Q1', 'Q2', 'Q3', 'Q4', 'Q5'],
      supplementedQuestionCount: 0,
    ),
  ],
);

final _hubScenes = <SceneDefinition>[
  testScene(
    id: 'scn_interview_self_introduction',
    family: SceneFamily.interview,
    model: SceneModel.interviewBasicDialogue,
    name: '英文自我介绍',
    prompt: _hubPrompt('练习简洁介绍背景、优势和岗位匹配。'),
    version: 1,
  ),
  testScene(
    id: 'scn_interview_case_study',
    family: SceneFamily.interview,
    model: SceneModel.interviewBasicDialogue,
    name: '案例面试',
    prompt: _hubPrompt('服务端后续新增的面试场景仍可进入。'),
    version: 1,
  ),
  testScene(
    id: 'scn_ielts_speaking_full',
    family: SceneFamily.exam,
    model: SceneModel.ieltsSpeakingFullMock,
    name: 'IELTS 口语完整模拟',
    prompt: _hubPrompt('按 Part 1、Part 2、Part 3 完成一轮练习。'),
    version: 1,
  ),
  testScene(
    id: 'scn_ielts_speaking_part_1',
    family: SceneFamily.exam,
    model: SceneModel.ieltsSpeakingPart1,
    name: 'IELTS Speaking Part 1',
    prompt: _hubPrompt('围绕熟悉话题完成简短问答。'),
    version: 1,
  ),
  testScene(
    id: 'scn_ielts_speaking_part_2',
    family: SceneFamily.exam,
    model: SceneModel.ieltsSpeakingPart2,
    name: 'IELTS Speaking Part 2',
    prompt: _hubPrompt('完成题卡陈述，并可继续练习同主题 Part 3。'),
    version: 1,
  ),
  testScene(
    id: 'scn_ielts_speaking_part_3',
    family: SceneFamily.exam,
    model: SceneModel.ieltsSpeakingPart3,
    name: 'IELTS Speaking Part 3',
    prompt: _hubPrompt('基于对应的 Part 2 主题展开讨论。'),
    version: 1,
  ),
  testScene(
    id: 'scn_speaking_exam_custom',
    family: SceneFamily.exam,
    model: SceneModel.examBasicDialogue,
    name: '自定义口语考试',
    prompt: _hubPrompt('练习其他考试形式的口语问题。'),
    version: 1,
  ),
  testScene(
    id: 'scn_workplace_progress_risk_update',
    family: SceneFamily.workplace,
    model: SceneModel.progressAndRiskUpdate,
    name: '进度与风险汇报',
    prompt: _hubPrompt('向直属领导汇报进展、风险和支持请求。'),
    version: 1,
  ),
  testScene(
    id: 'scn_daily_hotel_checkin_issue',
    family: SceneFamily.daily,
    model: SceneModel.hotelCheckinAndIssueHandling,
    name: '酒店入住与问题处理',
    prompt: _hubPrompt('办理入住并解决一个房间问题。'),
    version: 1,
  ),
  testScene(
    id: 'scn_workplace_custom',
    family: SceneFamily.workplace,
    model: SceneModel.workplaceBasicDialogue,
    name: '自定义职场沟通',
    prompt: _hubPrompt('使用自定义背景练习职场沟通。'),
    version: 1,
  ),
  testScene(
    id: 'scn_daily_custom',
    family: SceneFamily.daily,
    model: SceneModel.dailyBasicDialogue,
    name: '自定义日常交流',
    prompt: _hubPrompt('使用自定义背景练习日常交流。'),
    version: 1,
  ),
];

ScenePrompt _hubPrompt(String brief) => ScenePrompt(
  publicSceneBrief: brief,
  practiceGoal: 'Complete the selected practice.',
  userRole: 'Learner',
  aiRole: 'Coach',
  personaSummary: 'Structured and focused.',
  focusAreas: const <String>['clarity'],
  turnBlueprints: const <String>['Ask one relevant question.'],
  suggestedDurationSeconds: 600,
);
