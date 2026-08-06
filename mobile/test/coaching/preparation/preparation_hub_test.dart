import '../../support/scene_fixtures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';

void main() {
  testWidgets('shows exactly the four product-level practice entries', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    final ieltsController = IeltsPreparationController(
      client: _HubQuestionBankClient(),
    );
    addTearDown(controller.dispose);
    addTearDown(ieltsController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          preparationController: controller,
          ieltsController: ieltsController,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-hub-interview')), findsOneWidget);
    expect(find.byKey(const Key('practice-hub-exam')), findsOneWidget);
    expect(find.byKey(const Key('practice-hub-workplace')), findsOneWidget);
    expect(find.byKey(const Key('practice-hub-life')), findsOneWidget);
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
    'opens IELTS from one scene with four server-defined practice options',
    (tester) async {
      final controller = PreparationController(client: _HubFixtureClient());
      addTearDown(controller.dispose);
      await _pumpHub(tester, controller);

      await _openModule(tester, const Key('practice-hub-exam'));

      expect(find.text('专项练习'), findsOneWidget);
      expect(find.text('快速开始整轮模考'), findsOneWidget);
      expect(find.text('Part 1'), findsWidgets);
      expect(find.text('Part 2'), findsOneWidget);
      expect(find.text('Part 3'), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-part1-set-p1-topic-001')),
        findsOneWidget,
      );
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

    expect(find.text('共 3 道专项题'), findsOneWidget);
    expect(
      find.textContaining('Describe a skill you would like to learn'),
      findsWidgets,
    );
    expect(find.byKey(const Key('ielts-part2-set-p23-001')), findsOneWidget);
  });

  testWidgets('separates workplace from life and travel scenes', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    await _openModule(tester, const Key('practice-hub-workplace'));

    expect(find.text('职场英语'), findsOneWidget);
    expect(find.text('进度与风险汇报'), findsOneWidget);
    expect(find.text('酒店入住与问题处理'), findsNothing);
    expect(find.byKey(const Key('scenario-filter-workplace')), findsNothing);
    expect(find.text('英文自我介绍'), findsNothing);
    expect(find.text('IELTS 口语完整模拟'), findsNothing);
    expect(find.text('自定义职场沟通'), findsNothing);
    expect(find.text('自定义日常交流'), findsNothing);
    expect(find.byKey(const Key('scenario-custom-reserved')), findsOneWidget);

    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();
    await _openModule(tester, const Key('practice-hub-life'));

    expect(find.text('生活与旅行'), findsOneWidget);
    expect(find.text('酒店入住与问题处理'), findsOneWidget);
    expect(find.text('餐厅点餐'), findsOneWidget);
    expect(find.text('进度与风险汇报'), findsNothing);
    expect(find.byKey(const Key('scenario-filter-travel')), findsOneWidget);
    expect(find.byKey(const Key('scenario-filter-workplace')), findsNothing);
  });

  testWidgets('keeps the four entries usable at 320px and 3x text', (
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
      (key: Key('practice-hub-workplace'), title: '职场英语'),
      (key: Key('practice-hub-life'), title: '生活与旅行'),
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

  testWidgets('keeps interview and scenario cards usable at 3x text', (
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

    await _openModule(tester, const Key('practice-hub-life'));
    final scenarioScene = find.byKey(
      const Key('catalog-scene-scn_daily_hotel_checkin_issue'),
    );
    await tester.scrollUntilVisible(
      scenarioScene,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(scenarioScene.hitTestable(), findsOneWidget);
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

    final entry = find.byKey(const Key('practice-hub-workplace'));
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
      find.byKey(const Key('practice-hub-title-workplace')).hitTestable(),
      findsOneWidget,
    );
  });

  testWidgets('keeps exam and workplace modules usable in landscape', (
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
        scene: Key('ielts-part1-set-p1-topic-001'),
      ),
      (
        entry: Key('practice-hub-workplace'),
        title: Key('practice-hub-title-workplace'),
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
        (key: Key('practice-hub-workplace'), label: '职场英语。会议、协作与客户沟通'),
        (key: Key('practice-hub-life'), label: '生活与旅行。日常交流与出行场景实战'),
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
  final ieltsController = IeltsPreparationController(
    client: _HubQuestionBankClient(),
  );
  addTearDown(ieltsController.dispose);
  await tester.pumpWidget(
    MaterialApp(
      home: PreparationPage(
        preparationController: controller,
        ieltsController: ieltsController,
      ),
    ),
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

final class _HubFixtureClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError('The hub test does not open scene details.');
  }

  @override
  Future<List<SceneDefinition>> listScenes() async => _hubScenes;

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError('The hub test does not open scene details.');
  }
}

final class _HubQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getQuestionBank() async => _ieltsBank;
}

final _ieltsBank = IeltsQuestionBank(
  bankId: 'ielts-bank-1',
  season: '2026-05-08',
  seasonLabel: '5–8 月题库',
  seasonStart: DateTime.utc(2026, 5),
  seasonEnd: DateTime.utc(2026, 8, 31),
  sourceCutoff: DateTime.utc(2026, 6, 18),
  filters: const IeltsCatalogFilters(
    releases: [IeltsFilterOption(code: 'new', label: '本季新增')],
    parts: [
      IeltsFilterOption(code: 'PART_1', label: 'Part 1'),
      IeltsFilterOption(code: 'PART_2', label: 'Part 2'),
      IeltsFilterOption(code: 'PART_3', label: 'Part 3'),
    ],
    topicTags: [IeltsFilterOption(code: 'daily_life', label: '日常生活')],
    cueCardTypes: [IeltsFilterOption(code: 'thing', label: '事物')],
  ),
  part1Topics: const [
    IeltsPart1PracticeTopic(
      id: 'p1-topic-001',
      titleZh: '家乡',
      titleEn: 'Hometown',
      releaseStatus: 'carry_over',
      tagCodes: ['daily_life'],
      questions: ['Q1', 'Q2'],
    ),
  ],
  topicGroups: const [
    IeltsTopicGroup(
      id: 'p23-001',
      title: '学习技能',
      releaseStatus: 'new',
      cueCardType: 'thing',
      tagCodes: ['daily_life'],
      cueCard: IeltsCueCard(
        prompt: 'Describe a skill you would like to learn',
        points: ['What', 'Why', 'How', 'Benefit'],
      ),
      part3Questions: ['Q1', 'Q2', 'Q3', 'Q4', 'Q5'],
    ),
  ],
);

final _hubScenes = <SceneDefinition>[
  testScene(
    id: 'scn_interview_self_introduction',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewRecruiter,
    name: '英文自我介绍',
    prompt: _hubPrompt('练习简洁介绍背景、优势和岗位匹配。'),
    version: 1,
  ),
  testScene(
    id: 'scn_interview_case_study',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewProfessional,
    name: '案例面试',
    prompt: _hubPrompt('服务端后续新增的面试场景仍可进入。'),
    version: 1,
  ),
  testScene(
    id: 'scene_ielts_speaking',
    experience: PracticeExperience.ieltsSpeaking,
    category: SceneCategory.ieltsSpeaking,
    name: 'IELTS 口语',
    prompt: _hubPrompt('按 Part 1、Part 2、Part 3 完成一轮练习。'),
    version: 1,
    practiceOptions: [
      testPracticeOption(
        id: 'option_ielts_full_mock',
        sceneId: 'scene_ielts_speaking',
        mode: PracticeMode.fullMock,
        displayName: '完整模考',
      ),
      testPracticeOption(
        id: 'option_ielts_part_1',
        sceneId: 'scene_ielts_speaking',
        mode: PracticeMode.part1,
        displayName: 'Part 1',
      ),
      testPracticeOption(
        id: 'option_ielts_part_2',
        sceneId: 'scene_ielts_speaking',
        mode: PracticeMode.part2,
        displayName: 'Part 2',
      ),
      testPracticeOption(
        id: 'option_ielts_part_3',
        sceneId: 'scene_ielts_speaking',
        mode: PracticeMode.part3,
        displayName: 'Part 3',
      ),
    ],
  ),
  testScene(
    id: 'scn_workplace_progress_risk_update',
    experience: PracticeExperience.workplace,
    category: SceneCategory.workplaceGeneral,
    name: '进度与风险汇报',
    prompt: _hubPrompt('向直属领导汇报进展、风险和支持请求。'),
    version: 1,
  ),
  testScene(
    id: 'scn_daily_hotel_checkin_issue',
    experience: PracticeExperience.lifeAndTravel,
    category: SceneCategory.lifeTravel,
    name: '酒店入住与问题处理',
    prompt: _hubPrompt('办理入住并解决一个房间问题。'),
    version: 1,
  ),
  testScene(
    id: 'scn_daily_restaurant_ordering',
    experience: PracticeExperience.lifeAndTravel,
    category: SceneCategory.lifeDaily,
    name: '餐厅点餐',
    prompt: _hubPrompt('练习点餐、确认需求与礼貌沟通。'),
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
);
