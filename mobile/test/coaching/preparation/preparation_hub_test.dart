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
  testWidgets('shows exactly the three product-level practice entries', (
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
    'opens IELTS from one scene with four server-defined practice options',
    (tester) async {
      final controller = PreparationController(client: _HubFixtureClient());
      addTearDown(controller.dispose);
      await _pumpHub(tester, controller);

      await _openModule(tester, const Key('practice-hub-exam'));

      expect(find.text('IELTS 口语'), findsOneWidget);
      expect(find.text('一次完成三个 Part'), findsOneWidget);
      expect(find.text('Part 1'), findsOneWidget);
      expect(find.text('题卡陈述 · 可继续 Part 3'), findsOneWidget);
      expect(find.text('承接 Part 2 主题讨论'), findsOneWidget);
      expect(
        find.byKey(const Key('catalog-scene-scene_ielts_speaking-part1')),
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

    await tester.tap(
      find.byKey(const Key('catalog-scene-scene_ielts_speaking-part2')),
    );
    await tester.pumpAndSettle();

    expect(find.text('Part 2 题卡'), findsOneWidget);
    expect(find.text('已完成 0 / 1 套'), findsOneWidget);
    expect(
      find.text('Describe a skill you would like to learn'),
      findsOneWidget,
    );
    expect(find.textContaining('可继续对应 Part 3'), findsOneWidget);
    expect(find.textContaining('Part 2 未练习'), findsOneWidget);
    expect(find.byKey(const Key('ielts-part2-set-p23-001')), findsOneWidget);
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
        scene: Key('catalog-scene-scene_ielts_speaking-part1'),
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
      titleZh: '家乡',
      titleEn: 'Hometown',
      release: 'carry_over',
      category: IeltsTopicCategory.place,
      questions: ['Q1', 'Q2'],
    ),
  ],
  topicGroups: const [
    IeltsTopicGroup(
      id: 'p23-001',
      title: '学习技能',
      release: 'new',
      category: IeltsTopicCategory.thing,
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
    experience: PracticeExperience.roleplay,
    category: SceneCategory.roleplayWorkplace,
    name: '进度与风险汇报',
    prompt: _hubPrompt('向直属领导汇报进展、风险和支持请求。'),
    version: 1,
  ),
  testScene(
    id: 'scn_daily_hotel_checkin_issue',
    experience: PracticeExperience.roleplay,
    category: SceneCategory.roleplayTravel,
    name: '酒店入住与问题处理',
    prompt: _hubPrompt('办理入住并解决一个房间问题。'),
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
