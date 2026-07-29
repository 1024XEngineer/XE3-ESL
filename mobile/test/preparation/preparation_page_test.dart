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
import 'package:speakup/features/preparation/preparation_launch_client.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

Future<void> _openFamily(WidgetTester tester, String family) async {
  final hubKey = switch (family) {
    'INTERVIEW' => 'practice-hub-interview',
    'EXAM' => 'practice-hub-exam',
    'WORKPLACE' || 'DAILY' => 'practice-hub-roleplay',
    _ => throw ArgumentError.value(family, 'family'),
  };
  final hub = find.byKey(Key(hubKey));
  await tester.ensureVisible(hub);
  await tester.pumpAndSettle();
  await tester.tap(hub);
  await tester.pumpAndSettle();
}

Future<void> _openInterviewScenario(
  WidgetTester tester, {
  required String scenarioId,
  required String modeId,
}) async {
  final scenario = find.byKey(Key('catalog-scenario-$scenarioId'));
  if (scenario.evaluate().isEmpty) {
    final mode = find.byKey(Key('interview-mode-$modeId'));
    await tester.scrollUntilVisible(
      mode,
      140,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(mode);
    await tester.pumpAndSettle();
  }
  final revealedScenario = find.byKey(Key('catalog-scenario-$scenarioId'));
  await tester.ensureVisible(revealedScenario);
  await tester.tap(revealedScenario);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'product training center previews a topic and keeps JD-first optional',
    (tester) async {
      final controller = PreparationController(client: _FixtureClient());
      addTearDown(controller.dispose);
      var opens = 0;

      await tester.pumpWidget(
        MaterialApp(
          home: PreparationPage(
            preparationController: controller,
            onOpenJobPreparation: () => opens++,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('training-center-title')), findsOneWidget);
      expect(find.text('场景练习'), findsOneWidget);
      expect(find.text('今天想练什么？'), findsOneWidget);
      expect(find.text('英文面试'), findsOneWidget);

      await _openFamily(tester, 'INTERVIEW');
      await tester.tap(find.byKey(const Key('open-job-preparation')));
      await tester.pump();

      expect(opens, 1);
      expect(controller.selectedScenario, isNull);

      await _openInterviewScenario(
        tester,
        scenarioId: _scenarioId,
        modeId: 'professional',
      );

      expect(controller.selectedScenario?.id, _scenarioId);
      expect(
        find.byKey(const Key('preparation-scenario-config')),
        findsOneWidget,
      );
    },
  );

  testWidgets('only the interview topic opens JD-first when catalog grows', (
    tester,
  ) async {
    final controller = PreparationController(client: _MultiScenarioClient());
    addTearDown(controller.dispose);
    var opens = 0;

    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          preparationController: controller,
          onOpenJobPreparation: () => opens++,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await _openFamily(tester, 'WORKPLACE');
    await tester.tap(
      find.byKey(const Key('catalog-scenario-scn_general_speaking')),
    );
    await tester.pumpAndSettle();

    expect(opens, 0);
    expect(controller.selectedScenario?.id, 'scn_general_speaking');
    expect(
      tester
          .widget<Text>(find.byKey(const Key('preparation-scenario-title')))
          .data,
      'General speaking practice',
    );
  });

  testWidgets('subscenes show their own server summaries', (tester) async {
    tester.view.physicalSize = const Size(375, 812);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = PreparationController(client: _InterviewSummaryClient());
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MediaQuery(
        data: const MediaQueryData(textScaler: TextScaler.linear(2)),
        child: MaterialApp(
          home: PreparationPage(preparationController: controller),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await _openFamily(tester, 'INTERVIEW');

    expect(find.text('Discuss one backend project.'), findsOneWidget);
    final hr = find.byKey(const Key('interview-mode-hr'));
    await tester.scrollUntilVisible(
      hr,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(hr.hitTestable(), findsOneWidget);
    await tester.tap(hr);
    await tester.pumpAndSettle();
    final recruiter = find.byKey(
      const Key('catalog-scenario-scn_interview_recruiter_screening'),
      skipOffstage: false,
    );
    expect(recruiter, findsOneWidget);
    await tester.ensureVisible(recruiter);
    await tester.pumpAndSettle();
    expect(
      find.descendant(
        of: recruiter,
        matching: find.text(
          'Discuss motivation, role fit, and basic expectations.',
          skipOffstage: false,
        ),
        skipOffstage: false,
      ),
      findsOneWidget,
    );
    expect(find.text('围绕真实项目经历，练习结构化表达与追问应对'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('groups the catalog into three product directions', (
    tester,
  ) async {
    final controller = PreparationController(client: _FourFamilyClient());
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PreparationPage(preparationController: controller)),
    );
    await tester.pumpAndSettle();

    for (final hub in const [
      'practice-hub-interview',
      'practice-hub-exam',
      'practice-hub-roleplay',
    ]) {
      final entry = find.byKey(Key(hub));
      await tester.scrollUntilVisible(
        entry,
        160,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      expect(entry, findsOneWidget);
    }
  });

  testWidgets(
    'loads the server catalog and keeps perspectives independent of stages',
    (tester) async {
      final controller = PreparationController(client: _FixtureClient());
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(home: PreparationPage(preparationController: controller)),
      );
      await tester.pumpAndSettle();

      expect(find.text('场景练习'), findsOneWidget);
      expect(find.text('今天想练什么？'), findsOneWidget);
      await _openFamily(tester, 'INTERVIEW');
      await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
      await tester.pumpAndSettle();

      expect(
        find.text('English interview for technical roles'),
        findsOneWidget,
      );
      final technical = find.byKey(
        const Key('preparation-role-role_technical_interviewer'),
      );
      expect(controller.roles.take(2).map((role) => role.id), [
        'role_technical_interviewer',
        'role_hr_interviewer',
      ]);

      final guidance = find.byKey(const Key('preparation-role-guidance'));
      await tester.scrollUntilVisible(guidance, 160);
      await tester.pumpAndSettle();
      expect(guidance, findsOneWidget);
      await tester.scrollUntilVisible(technical, 200);
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
        const Key('preparation-launch-unavailable'),
      );
      await tester.scrollUntilVisible(selectionNotice, 200);
      await tester.pumpAndSettle();

      expect(selectionNotice, findsOneWidget);
      expect(find.textContaining('正式练习启动服务未注入'), findsOneWidget);
    },
  );

  testWidgets('role and option cards expose one selectable button node', (
    tester,
  ) async {
    final semantics = tester.ensureSemantics();
    var semanticsDisposed = false;
    void disposeSemantics() {
      if (semanticsDisposed) {
        return;
      }
      semanticsDisposed = true;
      semantics.dispose();
    }

    addTearDown(disposeSemantics);
    final controller = PreparationController(client: _FixtureClient());
    addTearDown(controller.dispose);
    try {
      await tester.pumpWidget(
        MaterialApp(home: PreparationPage(preparationController: controller)),
      );
      await tester.pumpAndSettle();
      await _openFamily(tester, 'INTERVIEW');
      await controller.selectScenario(_scenario);
      await tester.pumpAndSettle();

      const roleLabel =
          'Technical depth perspective. '
          'Probe technical depth and decision making. '
          'Precise and evidence seeking.';
      final role = find.byKey(
        const Key('preparation-role-role_technical_interviewer'),
      );
      await tester.scrollUntilVisible(
        role,
        160,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      expect(
        tester.getSemantics(role),
        isSemantics(
          label: roleLabel,
          isButton: true,
          hasSelectedState: true,
          isSelected: false,
          isInMutuallyExclusiveGroup: true,
          hasTapAction: true,
        ),
      );
      expect(find.bySemanticsLabel(roleLabel), findsOneWidget);

      await tester.tap(role);
      await tester.pump();
      expect(
        tester.getSemantics(role),
        isSemantics(
          label: roleLabel,
          isButton: true,
          hasSelectedState: true,
          isSelected: true,
          isInMutuallyExclusiveGroup: true,
          hasTapAction: true,
        ),
      );

      const optionLabel = '完整模拟: Full simulation';
      final option = find.byKey(
        const Key('preparation-option-option_full_simulation'),
      );
      await tester.scrollUntilVisible(option, 200);
      await tester.ensureVisible(option);
      await tester.pumpAndSettle();
      expect(
        tester.getSemantics(option),
        isSemantics(
          label: optionLabel,
          isButton: true,
          hasSelectedState: true,
          isSelected: false,
          isInMutuallyExclusiveGroup: true,
          hasTapAction: true,
        ),
      );
      expect(find.bySemanticsLabel(optionLabel), findsOneWidget);

      await tester.tap(option);
      await tester.pump();
      expect(
        tester.getSemantics(option),
        isSemantics(
          label: optionLabel,
          isButton: true,
          hasSelectedState: true,
          isSelected: true,
          isInMutuallyExclusiveGroup: true,
          hasTapAction: true,
        ),
      );
    } finally {
      disposeSemantics();
    }
  });

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
    await _openFamily(tester, 'INTERVIEW');
    await controller.selectScenario(_scenario);
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
      await _openFamily(tester, 'INTERVIEW');
      await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('primary-tab-agent')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(agentController.threadId, originalThreadId);
      expect(agentController.activeMatter?.id, originalMatterId);
    },
  );

  testWidgets('starts the real typed chain and reports success to navigation', (
    tester,
  ) async {
    final agentController = AgentController(client: FakeAgentClient());
    final preparationController = PreparationController(
      client: _FixtureClient(),
    );
    final launchClient = _PageLaunchClient();
    var navigations = 0;
    await agentController.initialize();
    await agentController.selectScene(agentScenes.first);
    final launchController = PreparationLaunchController(
      client: launchClient,
      contextProvider: () => AgentPracticeContext(
        threadId: agentController.threadId!,
        matterId: agentController.activeMatter!.id,
      ),
      threadIdProvider: () => agentController.threadId,
      matterActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) async {
            final matter = agentController.activeMatter!;
            return AgentPracticeContext(
              threadId: threadId,
              matterId: matter.id,
            );
          },
      voiceActivator:
          ({
            required context,
            required bootstrap,
            required clientOperationId,
          }) async {},
      idFactory: (scope) => '$scope-widget-key',
    );
    addTearDown(agentController.dispose);
    addTearDown(preparationController.dispose);
    addTearDown(launchController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          agentController: agentController,
          preparationController: preparationController,
          launchController: launchController,
          onPracticeStarted: () => navigations++,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await _openFamily(tester, 'INTERVIEW');
    await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
    await tester.pumpAndSettle();
    final role = find.byKey(
      const Key('preparation-role-role_technical_interviewer'),
    );
    await tester.scrollUntilVisible(role, 200);
    await tester.pumpAndSettle();
    await tester.tap(role);
    await tester.pump();
    final option = find.byKey(
      const Key('preparation-option-option_full_simulation'),
    );
    await tester.scrollUntilVisible(option, 200);
    await tester.tap(option);
    await tester.pump();
    final background = find.byKey(const Key('preparation-background-summary'));
    await tester.scrollUntilVisible(
      background,
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.enterText(
      background,
      'Backend engineer preparing a technical interview.',
    );
    final start = find.byKey(const Key('preparation-start-practice'));
    await tester.scrollUntilVisible(
      start,
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(start);
    await tester.pumpAndSettle();

    expect(launchClient.calls, ['profile', 'snapshot', 'plan', 'session']);
    expect(navigations, 1);
    expect(agentController.threadId, isNotNull);
    expect(agentController.activeMatter, isNotNull);
    expect(launchController.bootstrap?.maxEffectiveTurns, 6);
  });

  testWidgets(
    'keeps start on the current page while the Agent Thread restores',
    (tester) async {
      final preparationController = PreparationController(
        client: _FixtureClient(),
      );
      final launchClient = _PageLaunchClient();
      final launchController = PreparationLaunchController(
        client: launchClient,
        contextProvider: () => null,
        threadIdProvider: () => null,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) {
              throw StateError('Matter activation must wait for the Thread.');
            },
        voiceActivator:
            ({
              required context,
              required bootstrap,
              required clientOperationId,
            }) async {},
        idFactory: (scope) => '$scope-thread-wait-key',
      );
      addTearDown(preparationController.dispose);
      addTearDown(launchController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: PreparationPage(
            preparationController: preparationController,
            launchController: launchController,
          ),
        ),
      );
      await tester.pumpAndSettle();
      await _openFamily(tester, 'INTERVIEW');
      await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
      await tester.pumpAndSettle();
      final role = find.byKey(
        const Key('preparation-role-role_technical_interviewer'),
      );
      await tester.scrollUntilVisible(role, 200);
      await tester.pumpAndSettle();
      await tester.tap(role);
      await tester.pump();
      final option = find.byKey(
        const Key('preparation-option-option_full_simulation'),
      );
      await tester.scrollUntilVisible(option, 200);
      await tester.pumpAndSettle();
      await tester.tap(option);
      await tester.pump();

      final startFinder = find.byKey(const Key('preparation-start-practice'));
      await tester.scrollUntilVisible(
        startFinder,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      expect(tester.widget<FilledButton>(startFinder).onPressed, isNotNull);
      expect(
        find.byKey(const Key('preparation-agent-context-missing')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('preparation-open-agent-home')),
        findsNothing,
      );

      final background = find.byKey(
        const Key('preparation-background-summary'),
      );
      await tester.scrollUntilVisible(
        background,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.enterText(
        background,
        'Backend engineer preparing a technical interview.',
      );
      await tester.tap(startFinder);
      await tester.pump();

      expect(find.textContaining('Agent 对话仍在恢复'), findsWidgets);
      expect(launchClient.calls, isEmpty);
    },
  );

  testWidgets(
    'keeps selection locked after Session creation until voice retry succeeds',
    (tester) async {
      final session = Completer<PreparationPracticeBootstrap>();
      final preparationController = PreparationController(
        client: _FixtureClient(),
      );
      final launchClient = _PageLaunchClient(sessionCompleter: session);
      var navigations = 0;
      var voiceCalls = 0;
      final launchController = PreparationLaunchController(
        client: launchClient,
        contextProvider: () => _pageContext,
        threadIdProvider: () => _pageContext.threadId,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => _pageContext,
        voiceActivator:
            ({
              required context,
              required bootstrap,
              required clientOperationId,
            }) async {
              voiceCalls++;
              if (voiceCalls == 1) {
                throw const PreparationLaunchException(
                  kind: PreparationLaunchFailureKind.invalidResponse,
                  stage: PreparationLaunchStage.voice,
                );
              }
            },
        idFactory: (scope) => '$scope-pending-widget-key',
      );
      addTearDown(preparationController.dispose);
      addTearDown(launchController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: PreparationPage(
            showBackButton: true,
            preparationController: preparationController,
            launchController: launchController,
            onPracticeStarted: () => navigations++,
          ),
        ),
      );
      await tester.pumpAndSettle();
      await _openFamily(tester, 'INTERVIEW');
      await tester.tap(find.byKey(const Key('catalog-scenario-$_scenarioId')));
      await tester.pumpAndSettle();

      final role = find.byKey(
        const Key('preparation-role-role_technical_interviewer'),
      );
      await tester.scrollUntilVisible(role, 200);
      await tester.pumpAndSettle();
      await tester.tap(role);
      await tester.pump();
      final option = find.byKey(
        const Key('preparation-option-option_full_simulation'),
      );
      await tester.scrollUntilVisible(option, 200);
      await tester.pumpAndSettle();
      if (option.hitTestable().evaluate().isEmpty) {
        await tester.drag(find.byType(Scrollable).first, const Offset(0, -80));
        await tester.pumpAndSettle();
      }
      expect(option.hitTestable(), findsOneWidget);
      await tester.tap(option);
      await tester.pump();
      final background = find.byKey(
        const Key('preparation-background-summary'),
      );
      await tester.scrollUntilVisible(
        background,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.enterText(
        background,
        'Backend engineer preparing a technical interview.',
      );
      final start = find.byKey(const Key('preparation-start-practice'));
      await tester.scrollUntilVisible(
        start,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.tap(start);
      await tester.pump();

      expect(launchClient.calls, ['profile', 'snapshot', 'plan', 'session']);
      expect(launchController.isStarting, isTrue);
      final detailBack = find.byKey(const Key('preparation-back-to-catalog'));
      await tester.scrollUntilVisible(
        detailBack,
        -200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(tester.widget<IconButton>(detailBack).onPressed, isNull);
      expect(
        tester
            .widget<IconButton>(
              find.byKey(const Key('preparation-route-back-button')),
            )
            .onPressed,
        isNull,
      );
      expect(
        tester.widget<PopScope<void>>(find.byType(PopScope<void>)).canPop,
        isFalse,
      );
      await tester.scrollUntilVisible(
        role,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(
        tester
            .widget<InkWell>(
              find.descendant(of: role, matching: find.byType(InkWell)),
            )
            .onTap,
        isNull,
      );
      await tester.scrollUntilVisible(
        option,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(
        tester
            .widget<InkWell>(
              find.descendant(of: option, matching: find.byType(InkWell)),
            )
            .onTap,
        isNull,
      );
      expect(preparationController.selectedRole?.id, _technicalRole.id);
      expect(
        preparationController.selectedOption?.id,
        'option_full_simulation',
      );

      session.complete(
        PreparationPracticeBootstrap(
          session: PreparationPracticeSession(
            id: 'session-1',
            planId: 'plan-1',
            scenarioType: 'INTERVIEW',
            scenarioModel: 'PROJECT_EXPERIENCE_DEEP_DIVE',
            snapshotId: 'session-snapshot-1',
            status: 'starting',
            version: 1,
            createdAt: DateTime.utc(2026, 7, 26),
          ),
          preparationSnapshotId: 'preparation-snapshot-1',
          maxEffectiveTurns: 6,
        ),
      );
      await tester.pumpAndSettle();

      expect(launchController.isStarting, isFalse);
      expect(launchController.isSelectionLocked, isTrue);
      expect(launchController.bootstrap, isNotNull);
      expect(launchController.canRetry, isTrue);
      expect(navigations, 0);

      await tester.scrollUntilVisible(
        detailBack,
        -200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(tester.widget<IconButton>(detailBack).onPressed, isNull);
      expect(
        tester
            .widget<IconButton>(
              find.byKey(const Key('preparation-route-back-button')),
            )
            .onPressed,
        isNull,
      );
      expect(
        tester.widget<PopScope<void>>(find.byType(PopScope<void>)).canPop,
        isFalse,
      );
      await tester.scrollUntilVisible(
        role,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(
        tester
            .widget<InkWell>(
              find.descendant(of: role, matching: find.byType(InkWell)),
            )
            .onTap,
        isNull,
      );
      await tester.scrollUntilVisible(
        option,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(
        tester
            .widget<InkWell>(
              find.descendant(of: option, matching: find.byType(InkWell)),
            )
            .onTap,
        isNull,
      );
      await tester.scrollUntilVisible(
        background,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(tester.widget<TextField>(background).enabled, isFalse);

      final retry = find.byKey(const Key('preparation-retry-launch'));
      await tester.scrollUntilVisible(
        retry,
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.tap(retry);
      await tester.pumpAndSettle();

      expect(voiceCalls, 2);
      expect(
        launchClient.calls,
        ['profile', 'snapshot', 'plan', 'session'] +
            ['profile', 'snapshot', 'plan', 'session'],
      );
      expect(launchController.isStarting, isFalse);
      expect(launchController.isSelectionLocked, isFalse);
      expect(
        tester.widget<PopScope<void>>(find.byType(PopScope<void>)).canPop,
        isTrue,
      );
      expect(navigations, 1);
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

final class _MultiScenarioClient implements PreparationCatalogClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) async =>
      scenarioId == _scenarioId ? _detail : _otherDetail;

  @override
  Future<List<PreparationScenario>> listScenarios() async => const [
    _scenario,
    _otherScenario,
  ];

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) async =>
      scenarioId == _scenarioId ? _roles : const [_otherRole];
}

final class _InterviewSummaryClient implements PreparationCatalogClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) {
    throw UnimplementedError();
  }

  @override
  Future<List<PreparationScenario>> listScenarios() async => const [
    _scenario,
    _secondInterviewScenario,
  ];

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) {
    throw UnimplementedError();
  }
}

final class _FourFamilyClient implements PreparationCatalogClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) {
    throw UnimplementedError();
  }

  @override
  Future<List<PreparationScenario>> listScenarios() async => const [
    _scenario,
    _examScenario,
    _otherScenario,
    _dailyScenario,
  ];

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) {
    throw UnimplementedError();
  }
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

final class _PageLaunchClient implements PreparationLaunchClient {
  _PageLaunchClient({this.sessionCompleter});

  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  final calls = <String>[];

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    calls.add('profile');
    return PreparationProfile(
      id: 'profile-1',
      userId: 'user-1',
      backgroundSummary: input.backgroundSummary,
      version: 1,
      updatedAt: DateTime.utc(2026, 7, 26),
    );
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    calls.add('snapshot');
    return PreparationSnapshot(
      id: 'preparation-snapshot-1',
      sourceProfileId: profileId,
      sourceVersion: sourceVersion,
      backgroundSnapshot: 'Backend engineer preparing a technical interview.',
      createdAt: DateTime.utc(2026, 7, 26),
    );
  }

  @override
  Future<PreparationPracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    calls.add('plan');
    return PreparationPracticePlan(
      id: 'plan-1',
      userId: input.preparationUserId,
      context: input.context,
      selection: input.selection,
      preparationProfileId: input.preparationProfileId,
      revision: 1,
      status: 'ready',
    );
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required String planId,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    calls.add('session');
    final bootstrap = PreparationPracticeBootstrap(
      session: PreparationPracticeSession(
        id: 'session-1',
        planId: planId,
        scenarioType: 'INTERVIEW',
        scenarioModel: 'PROJECT_EXPERIENCE_DEEP_DIVE',
        snapshotId: 'session-snapshot-1',
        status: 'starting',
        version: 1,
        createdAt: DateTime.utc(2026, 7, 26),
      ),
      preparationSnapshotId: input.preparationSnapshotId,
      maxEffectiveTurns: 6,
    );
    return sessionCompleter?.future ?? bootstrap;
  }
}

const _pageContext = AgentPracticeContext(
  threadId: '10000000-0000-4000-8000-000000000001',
  matterId: '40000000-0000-4000-8000-000000000001',
);

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

const _secondInterviewScenario = PreparationScenario(
  id: 'scn_interview_recruiter_screening',
  type: 'INTERVIEW',
  model: 'INTERVIEW_BASIC_DIALOGUE',
  name: 'Recruiter screening',
  summary: 'Discuss motivation, role fit, and basic expectations.',
  version: 1,
  status: 'active',
);

const _otherScenario = PreparationScenario(
  id: 'scn_general_speaking',
  type: 'WORKPLACE',
  model: 'PROGRESS_AND_RISK_UPDATE',
  name: 'General speaking practice',
  summary: 'Give a workplace progress update.',
  version: 1,
  status: 'active',
);

const _examScenario = PreparationScenario(
  id: 'scn_ielts_speaking_part_2',
  type: 'EXAM',
  model: 'IELTS_SPEAKING_PART_2',
  name: 'IELTS Speaking Part 2',
  summary: 'Develop a clear answer from one cue card.',
  version: 1,
  status: 'active',
);

const _dailyScenario = PreparationScenario(
  id: 'scn_daily_hotel_checkin_issue',
  type: 'DAILY',
  model: 'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
  name: 'Hotel check-in and issue handling',
  summary: 'Check in and resolve one room issue.',
  version: 1,
  status: 'active',
);

const _otherRole = PreparationRole(
  id: 'role_general_coach',
  scenarioId: 'scn_general_speaking',
  type: 'COACH',
  displayName: 'Speaking coach',
  responsibilities: 'Guide a focused conversation.',
  style: 'Supportive.',
  focusAreas: ['fluency'],
  version: 1,
);

const _otherDetail = PreparationScenarioDetail(
  scenario: _otherScenario,
  config: PreparationScenarioConfig(
    id: 'scfg_general_speaking',
    scenarioId: 'scn_general_speaking',
    type: 'WORKPLACE',
    model: 'PROGRESS_AND_RISK_UPDATE',
    version: 1,
    jobTitle: 'General speaking',
    jobDescription: 'Practice everyday spoken English.',
    prompt: _workplacePrompt,
  ),
  options: [
    PreparationOption(
      id: 'option_general_full',
      scenarioId: 'scn_general_speaking',
      type: PreparationOptionType.fullSimulation,
      displayName: 'Full practice',
      version: 1,
    ),
    PreparationOption(
      id: 'option_general_focus',
      scenarioId: 'scn_general_speaking',
      roleId: 'role_general_coach',
      type: PreparationOptionType.focus,
      displayName: 'Fluency focus',
      version: 1,
    ),
  ],
);

const _config = PreparationScenarioConfig(
  id: 'scfg_backend_engineer',
  scenarioId: _scenarioId,
  type: 'INTERVIEW',
  model: 'PROJECT_EXPERIENCE_DEEP_DIVE',
  version: 1,
  jobTitle: 'Backend engineer',
  jobDescription: 'Build reliable APIs and explain engineering trade-offs.',
  prompt: _interviewPrompt,
);

const _interviewPrompt = PreparationScenarioPrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: ['introduction', 'system_design'],
  turnBlueprints: ['Ask for a project overview.'],
  suggestedDurationSeconds: 900,
);

const _workplacePrompt = PreparationScenarioPrompt(
  publicSceneBrief: 'Give a workplace progress update.',
  practiceGoal: 'Explain progress and risk clearly.',
  userRole: 'Project owner',
  aiRole: 'Stakeholder',
  personaSummary: 'Direct and supportive.',
  focusAreas: ['fluency'],
  turnBlueprints: ['Ask for current progress.'],
  suggestedDurationSeconds: 600,
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
