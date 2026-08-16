import '../../support/scene_fixtures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_catalog.dart';
import 'package:speakup/features/coaching/ielts/ielts_set_detail.dart';

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

    expect(find.byKey(const Key('practice-hub-carousel')), findsOneWidget);
    expect(
      find.byKey(const Key('practice-hub-page-indicator')),
      findsOneWidget,
    );
    for (final key in const [
      Key('practice-hub-interview'),
      Key('practice-hub-exam'),
      Key('practice-hub-workplace'),
      Key('practice-hub-life'),
    ]) {
      await _showModule(tester, key);
      expect(find.byKey(key).hitTestable(), findsOneWidget);
    }
    expect(find.byKey(const Key('practice-continuation')), findsNothing);
    expect(find.text('最近练习'), findsNothing);
    expect(find.byKey(const Key('preparation-family-INTERVIEW')), findsNothing);
    expect(find.byKey(const Key('preparation-family-EXAM')), findsNothing);
    expect(find.byKey(const Key('preparation-family-WORKPLACE')), findsNothing);
    expect(find.byKey(const Key('preparation-family-DAILY')), findsNothing);
  });

  testWidgets('loops seamlessly between the first and last practice entries', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    final carousel = find.byKey(const Key('practice-hub-carousel'));
    final swipeDistance = tester.getSize(carousel).width * 0.8;
    await tester.drag(carousel, Offset(swipeDistance, 0));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-hub-life')).hitTestable(),
      findsOneWidget,
    );

    await tester.drag(carousel, Offset(-swipeDistance, 0));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-hub-interview')).hitTestable(),
      findsOneWidget,
    );
  });

  testWidgets('preview opens IELTS without an injected controller', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(home: PreparationPage(previewMode: true)),
    );
    await tester.pumpAndSettle();

    await _openModule(tester, const Key('practice-hub-exam'));

    expect(find.byKey(const Key('practice-hub-title-ielts')), findsOneWidget);
    expect(find.text('连接场景服务后即可查看练习。'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('returning from practice loads the IELTS catalog', (
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

    ieltsController.requestNavigation(
      const IeltsPracticeNavigationRequest(mode: PracticeMode.part1),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('ielts-browser-search')), findsOneWidget);
    expect(find.byKey(const Key('ielts-question-bank-retry')), findsNothing);
  });

  testWidgets('opens the English interview module from the hub', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    await _openModule(tester, const Key('practice-hub-interview'));

    expect(
      find.byKey(const Key('practice-hub-title-interview')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('create-interview-plan')), findsOneWidget);
    expect(find.byKey(const Key('interview-plan-empty')), findsOneWidget);
    expect(find.text('专项练习'), findsNothing);
    expect(find.text('英文自我介绍'), findsNothing);
    expect(find.text('案例面试'), findsNothing);
    expect(find.text('IELTS 口语完整模拟'), findsNothing);
    expect(find.text('进度与风险汇报'), findsNothing);
  });

  testWidgets('uses English display titles for all four practice hubs', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    for (final hub in const [
      (
        entry: Key('practice-hub-interview'),
        titleKey: Key('practice-hub-title-interview'),
        title: 'Interview',
        semanticLabel: '英文面试',
      ),
      (
        entry: Key('practice-hub-exam'),
        titleKey: Key('practice-hub-title-ielts'),
        title: 'IELTS',
        semanticLabel: 'IELTS 口语',
      ),
      (
        entry: Key('practice-hub-workplace'),
        titleKey: Key('practice-hub-title-workplace'),
        title: 'Workplace',
        semanticLabel: '职场英语',
      ),
      (
        entry: Key('practice-hub-life'),
        titleKey: Key('practice-hub-title-life'),
        title: 'Travel',
        semanticLabel: '生活与旅行',
      ),
    ]) {
      await _openModule(tester, hub.entry);

      final title = tester.widget<Text>(find.byKey(hub.titleKey));
      expect(title.data, hub.title);
      expect(title.style, SpeakUpDesign.secondaryDisplayTitle);
      expect(find.bySemanticsLabel(hub.semanticLabel), findsOneWidget);

      await tester.tap(
        find.byKey(const Key('preparation-back-to-families')).hitTestable(),
      );
      await tester.pumpAndSettle();
    }
  });

  testWidgets(
    'opens IELTS from one scene with four server-defined practice options',
    (tester) async {
      final controller = PreparationController(client: _HubFixtureClient());
      addTearDown(controller.dispose);
      await _pumpHub(tester, controller);

      await _openModule(tester, const Key('practice-hub-exam'));

      expect(find.byKey(const Key('practice-hub-title-ielts')), findsOneWidget);
      expect(find.byKey(const Key('ielts-mode-full')), findsOneWidget);
      expect(find.text('模考'), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, 'Part 1'), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, 'Part 2'), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, 'Part 3'), findsOneWidget);
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

    expect(find.text('2 道题'), findsOneWidget);
    expect(
      find.textContaining('Describe a skill you would like to learn'),
      findsWidgets,
    );
    expect(find.text('PART 1'), findsNothing);
    expect(find.textContaining('对应 Part 2'), findsNothing);
    expect(find.textContaining('完成后继续同组 Part 3'), findsNothing);
    expect(find.byKey(const Key('ielts-part2-set-p23-001')), findsOneWidget);
  });

  testWidgets('opens Part 1 details before emitting the exact selection', (
    tester,
  ) async {
    final ieltsController = IeltsPreparationController(
      client: _HubQuestionBankClient(),
    );
    addTearDown(ieltsController.dispose);
    SceneDefinition? selectedScene;
    PracticeMode? selectedMode;
    IeltsPracticeSelection? selectedSet;
    await ieltsController.loadIfNeeded();

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: IeltsCatalog(
            controller: ieltsController,
            scenes: _hubScenes,
            onRetry: ieltsController.retryLoad,
            onSelectionPressed: (scene, mode, selection, preparedAnswers) {
              selectedScene = scene;
              selectedMode = mode;
              selectedSet = selection;
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('ielts-part1-set-p1-topic-001')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('ielts-set-detail')), findsOneWidget);
    expect(find.text('家乡'), findsOneWidget);
    expect(find.text('Hometown'), findsOneWidget);
    expect(find.text('Q1'), findsOneWidget);
    expect(find.text('Q2'), findsOneWidget);
    final topicBottom = tester.getBottomLeft(
      find.byKey(const Key('ielts-set-detail-topic')),
    );
    final questionsTop = tester.getTopLeft(
      find.byKey(const Key('ielts-set-detail-question-1')),
    );
    expect(topicBottom.dy, lessThanOrEqualTo(questionsTop.dy));
    expect(find.byKey(const Key('ielts-set-detail-title')), findsOneWidget);
    expect(find.text('开始整组练习'), findsOneWidget);
    expect(selectedSet, isNull);

    await tester.tap(find.byKey(const Key('ielts-set-detail-start')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('ielts-set-detail')), findsNothing);
    expect(selectedScene?.id, 'scene_ielts_speaking');
    expect(selectedMode, PracticeMode.part1);
    expect(
      selectedSet,
      const IeltsPracticeSelection(part1SetId: 'p1-topic-001'),
    );
  });

  testWidgets('keeps the detail action reachable on a narrow large-text view', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 1.6;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSetDetailPage(
          mode: PracticeMode.part2,
          title: _ieltsBank.topicGroups.single.title,
          subtitle: _ieltsBank.topicGroups.single.cueCard.prompt,
          cueCard: _ieltsBank.topicGroups.single.cueCard,
          questions: _ieltsBank.topicGroups.single.part3Questions,
          onStart: (_) {},
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('ielts-set-detail-start')).hitTestable(),
      findsOneWidget,
    );
    expect(find.byKey(const Key('ielts-set-detail-scroll')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'keeps three compact IELTS filter rows with cue card categories',
    (tester) async {
      final controller = PreparationController(client: _HubFixtureClient());
      addTearDown(controller.dispose);
      await _pumpHub(tester, controller);
      await _openModule(tester, const Key('practice-hub-exam'));

      expect(find.byKey(const Key('ielts-release-filter')), findsOneWidget);
      expect(find.byKey(const Key('ielts-tag-filter')), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, '全部'), findsNWidgets(3));
      for (final label in const ['人物', '地点', '事物', '经历']) {
        expect(find.widgetWithText(ChoiceChip, label), findsOneWidget);
      }
      expect(find.widgetWithText(ChoiceChip, 'Part 1'), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, '本季新增'), findsOneWidget);
      expect(find.widgetWithText(ChoiceChip, '日常生活'), findsNothing);

      await tester.tap(find.widgetWithText(ChoiceChip, '事物'));
      await tester.pumpAndSettle();

      expect(find.text('1 道题'), findsOneWidget);
      expect(find.byKey(const Key('ielts-part2-set-p23-001')), findsOneWidget);

      await tester.tap(find.widgetWithText(ChoiceChip, 'Part 3'));
      await tester.pumpAndSettle();

      expect(find.text('1 道题'), findsOneWidget);
      expect(find.byKey(const Key('ielts-part3-set-p23-001')), findsOneWidget);

      await tester.tap(find.widgetWithText(ChoiceChip, 'Part 1'));
      await tester.tap(find.widgetWithText(ChoiceChip, '地点'));
      await tester.pumpAndSettle();

      expect(find.text('1 道题'), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-part1-set-p1-topic-001')),
        findsOneWidget,
      );
    },
  );

  testWidgets('separates workplace from life and travel scenes', (
    tester,
  ) async {
    final controller = PreparationController(client: _HubFixtureClient());
    addTearDown(controller.dispose);
    await _pumpHub(tester, controller);

    await _openModule(tester, const Key('practice-hub-workplace'));

    expect(find.text('Workplace'), findsOneWidget);
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

    expect(find.text('Travel'), findsOneWidget);
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
      await _showModule(tester, entryData.key);
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
    final interviewMode = find.byKey(const Key('create-interview-plan'));
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

    const key = Key('practice-hub-workplace');
    await _showModule(tester, key);
    final entry = find.byKey(key).hitTestable();
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
      await _showModule(tester, module.entry);
      final entry = find.byKey(module.entry).hitTestable();
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
        await _showModule(tester, entryData.key);
        final entry = find.byKey(entryData.key).hitTestable();
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
  await _showModule(tester, key);
  final entry = find.byKey(key).hitTestable();
  expect(entry, findsOneWidget);
  await tester.tap(entry);
  await tester.pumpAndSettle();
}

Future<void> _showModule(WidgetTester tester, Key key) async {
  final entry = find.byKey(key).hitTestable();
  for (var attempt = 0; attempt < 4 && entry.evaluate().isEmpty; attempt++) {
    final carousel = find.byKey(const Key('practice-hub-carousel'));
    await tester.drag(
      carousel,
      Offset(-tester.getSize(carousel).width * 0.8, 0),
    );
    await tester.pumpAndSettle();
  }
  expect(entry, findsOneWidget);
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
    cueCardTypes: [
      IeltsFilterOption(code: 'person', label: '人物'),
      IeltsFilterOption(code: 'place', label: '地点'),
      IeltsFilterOption(code: 'thing', label: '事物'),
      IeltsFilterOption(code: 'experience', label: '经历'),
    ],
  ),
  part1Topics: const [
    IeltsPart1PracticeTopic(
      id: 'p1-topic-001',
      titleZh: '家乡',
      titleEn: 'Hometown',
      releaseStatus: 'carry_over',
      cueCardType: 'place',
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
