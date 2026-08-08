import '../../support/scene_fixtures.dart';
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';

Future<void> _openFamily(WidgetTester tester, String family) async {
  final hubKey = switch (family) {
    'INTERVIEW' => 'practice-hub-interview',
    'EXAM' => 'practice-hub-exam',
    'WORKPLACE' => 'practice-hub-workplace',
    'DAILY' => 'practice-hub-life',
    _ => throw ArgumentError.value(family, 'family'),
  };
  final hub = find.byKey(Key(hubKey));
  await tester.ensureVisible(hub);
  await tester.pumpAndSettle();
  await tester.tap(hub);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('product training center opens the interview plan library', (
    tester,
  ) async {
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
    expect(find.text('Practice'), findsOneWidget);
    expect(find.text('今天想练什么？'), findsNothing);
    expect(find.text('英文面试'), findsOneWidget);

    await _openFamily(tester, 'INTERVIEW');

    expect(find.byKey(const Key('create-interview-plan')), findsOneWidget);
    expect(find.byKey(const Key('interview-plan-empty')), findsOneWidget);
    final titleCenter = tester.getCenter(
      find.byKey(const Key('practice-hub-title-interview')),
    );
    final createButton = find.byKey(const Key('create-interview-plan'));
    final createCenter = tester.getCenter(createButton);
    final createSize = tester.getSize(createButton);
    expect((titleCenter.dy - createCenter.dy).abs(), lessThan(1));
    expect(createSize.width, greaterThanOrEqualTo(44));
    expect(createSize.height, greaterThanOrEqualTo(44));

    await tester.tap(createButton);
    await tester.pump();

    expect(opens, 1);
    expect(controller.selectedScene, isNull);
  });

  testWidgets('only the interview topic opens JD-first when catalog grows', (
    tester,
  ) async {
    final controller = PreparationController(client: _MultiSceneClient());
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
      find.byKey(const Key('catalog-scene-scn_general_speaking')),
    );
    await tester.pumpAndSettle();

    expect(opens, 0);
    expect(controller.selectedScene, isNull);
    expect(find.text('General speaking practice'), findsOneWidget);
    expect(find.byKey(const Key('preparation-scene-title')), findsNothing);
  });

  testWidgets('groups the catalog into four product directions', (
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
      'practice-hub-workplace',
      'practice-hub-life',
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
      const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preparation-catalog-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('preparation-catalog-retry')));
    await tester.pump();
    client.second.complete(const <SceneDefinition>[]);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preparation-catalog-empty')), findsOneWidget);
  });

  testWidgets('ordinary scene requires five-field Preparation before launch', (
    tester,
  ) async {
    final preparationController = PreparationController(
      client: _MultiSceneClient(),
    );
    final launchClient = _PageLaunchClient();
    var navigations = 0;
    final launchController = PreparationLaunchController(
      client: launchClient,
      contextProvider: () => _pageContext,
      threadIdProvider: () => _pageContext.threadId,
      goalActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) async => _pageContext,
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {},
      idFactory: (scope) => '$scope-scenario-widget-key',
    );
    addTearDown(preparationController.dispose);
    addTearDown(launchController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          preparationController: preparationController,
          launchController: launchController,
          onPracticeStarted: () => navigations++,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await _openFamily(tester, 'WORKPLACE');
    await tester.tap(
      find.byKey(const Key('catalog-scene-scn_general_speaking')),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('scenario-preparation-form')), findsOneWidget);
    expect(launchClient.calls, isEmpty);
    expect(find.text('情景描述'), findsOneWidget);
    expect(find.text('我的身份'), findsOneWidget);
    expect(find.text('对方身份'), findsOneWidget);
    expect(find.text('我的目标'), findsOneWidget);
    expect(find.text('对方人设'), findsOneWidget);

    final situation = find.descendant(
      of: find.byKey(const Key('scenario-situation')),
      matching: find.byType(TextFormField),
    );
    final goal = find.descendant(
      of: find.byKey(const Key('scenario-goal')),
      matching: find.byType(TextFormField),
    );
    expect(
      tester.widget<TextFormField>(situation).controller?.text,
      _workplacePrompt.publicSceneBrief,
    );
    await tester.enterText(situation, 'Report a delayed workplace project.');
    await tester.enterText(goal, 'Agree on a clear recovery plan.');

    final submit = find.byKey(const Key('scenario-preparation-submit'));
    await tester.scrollUntilVisible(
      submit,
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(submit);
    await tester.pumpAndSettle();

    expect(launchClient.calls, ['profile', 'snapshot', 'plan', 'session']);
    expect(navigations, 1);
    expect(launchClient.lastProfileInput?.kind, PreparationKind.scenario);
    expect(
      launchClient.lastProfileInput?.scenario,
      const ScenarioPreparationContext(
        situation: 'Report a delayed workplace project.',
        userRole: 'Project owner',
        counterpartRole: 'Stakeholder',
        goal: 'Agree on a clear recovery plan.',
        counterpartPersona: 'Direct and supportive.',
      ),
    );
  });

  testWidgets('back cancels a pending preparation and clears launch status', (
    tester,
  ) async {
    final preparationController = PreparationController(
      client: _FixtureClient(),
    );
    await preparationController.loadIfNeeded();
    await preparationController.selectScene(_scene);
    expect(preparationController.selectRecommendedConfiguration(), isTrue);
    final profile = Completer<PreparationProfile>();
    final launchClient = _PageLaunchClient(profileCompleter: profile);
    var navigations = 0;
    final launchController = PreparationLaunchController(
      client: launchClient,
      contextProvider: () => _pageContext,
      threadIdProvider: () => _pageContext.threadId,
      goalActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) async => _pageContext,
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {},
      idFactory: (scope) => '$scope-cancel-widget-key',
    );
    launchController.updateBackgroundSummary(
      'Backend engineer preparing a technical interview.',
    );
    addTearDown(preparationController.dispose);
    addTearDown(launchController.dispose);

    final start = launchController.start(
      PreparationLaunchSelection.fromCatalog(
        scene: preparationController.detail!,
        role: preparationController.selectedRole!,
        option: preparationController.selectedOption!,
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          preparationController: preparationController,
          launchController: launchController,
          onPracticeStarted: () => navigations++,
        ),
      ),
    );
    await tester.pump();

    expect(find.text('正在准备练习…'), findsOneWidget);
    await tester.tap(find.byKey(const Key('preparation-back-to-catalog')));
    await tester.pump();

    expect(find.text('正在准备练习…'), findsNothing);
    expect(find.byKey(const Key('preparation-launch-progress')), findsNothing);
    expect(preparationController.selectedScene, isNull);

    profile.complete(
      PreparationProfile(
        id: 'profile-1',
        userId: 'user-1',
        backgroundSummary: 'Backend engineer preparing a technical interview.',
        version: 1,
        updatedAt: DateTime.utc(2026, 7, 26),
      ),
    );
    expect(await start, isFalse);
    await tester.pumpAndSettle();
    expect(navigations, 0);
    expect(find.text('正在准备练习…'), findsNothing);
  });
}

class _FixtureClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) async => _detail;

  @override
  Future<List<SceneDefinition>> listScenes() async => [_scene];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async => _roles;
}

final class _MultiSceneClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) async =>
      sceneId == _sceneId ? _detail : _otherDetail;

  @override
  Future<List<SceneDefinition>> listScenes() async => [_scene, _otherScene];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async =>
      sceneId == _sceneId ? _roles : [_otherRole];
}

final class _FourFamilyClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() async => [
    _scene,
    _examScene,
    _otherScene,
    _dailyScene,
  ];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }
}

final class _ControlledListClient implements SceneClient {
  final Completer<List<SceneDefinition>> first =
      Completer<List<SceneDefinition>>();
  final Completer<List<SceneDefinition>> second =
      Completer<List<SceneDefinition>>();
  int calls = 0;

  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() {
    calls++;
    return calls == 1 ? first.future : second.future;
  }

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }
}

final class _PageLaunchClient implements PreparationLaunchClient {
  _PageLaunchClient({this.profileCompleter});
  final Completer<PreparationProfile>? profileCompleter;
  final calls = <String>[];
  PreparationLaunchSelection? selection;
  CreatePreparationProfileInput? lastProfileInput;
  PreparationSnapshot? _snapshot;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    calls.add('profile');
    lastProfileInput = input;
    final result = PreparationProfile(
      id: 'profile-1',
      userId: 'user-1',
      backgroundSummary: input.backgroundSummary,
      context: input.scenario,
      version: 1,
      updatedAt: DateTime.utc(2026, 7, 26),
    );
    return profileCompleter?.future ?? result;
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    calls.add('snapshot');
    final snapshot = PreparationSnapshot(
      id: 'preparation-snapshot-1',
      sourceProfileId: profileId,
      sourceVersion: sourceVersion,
      backgroundSnapshot: 'Backend engineer preparing a technical interview.',
      context: lastProfileInput?.scenario,
      createdAt: DateTime.utc(2026, 7, 26),
    );
    _snapshot = snapshot;
    return snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    calls.add('plan');
    final scene = switch (input.sceneId) {
      _sceneId => _detail,
      'scn_general_speaking' => _otherDetail,
      _ => throw StateError('Unknown Page test Scene.'),
    };
    selection = PreparationLaunchSelection(
      scene: scene,
      selectedRoleIds: input.selectedRoleIds,
      practiceOptionId: input.practiceOptionId,
    );
    final snapshot = _snapshot;
    if (snapshot == null || snapshot.id != input.preparationSnapshotId) {
      throw StateError('Plan did not use the created Snapshot.');
    }
    return PracticePlan(
      id: 'plan-1',
      userId: 'user-1',
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
      sessionPolicy: const PreparationSessionPolicy(
        suggestedDurationSeconds: 600,
        minEffectiveTurns: 1,
        maxEffectiveTurns: 6,
        coverageCheckpointTurn: 1,
        maxFollowUpsPerQuestion: 1,
        earlyCompletionRule: 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
        retryAllowed: false,
        questionTranslationAllowed: true,
        questionTipsAllowed: true,
        avatarAllowed: true,
        speechFeedbackAllowed: true,
      ),
      practiceObjectives: const <PracticeObjective>[
        PracticeObjective(
          id: 'clear_response',
          description: 'Give a clear response.',
        ),
      ],
      revision: 1,
      status: PracticePlanStatus.ready,
      createdAt: DateTime.utc(2026, 7, 26),
      updatedAt: DateTime.utc(2026, 7, 26),
    );
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    calls.add('session');
    final bootstrap = PreparationPracticeBootstrap(
      session: PreparationPracticeSession(
        id: 'session-1',
        planId: plan.id,
        practiceExperience: plan.sceneSelection.scene.experience,
        sceneCategory: plan.sceneSelection.scene.category,
        practiceMode: plan.practiceOption.mode,
        snapshotId: 'session-snapshot-1',
        status: 'starting',
        version: 1,
        createdAt: DateTime.utc(2026, 7, 26),
      ),
      preparationSnapshotId: plan.preparationSnapshot.id,
      maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
    );
    return bootstrap;
  }
}

const _pageContext = AgentPracticeContext(
  threadId: '10000000-0000-4000-8000-000000000001',
  goalId: '40000000-0000-4000-8000-000000000001',
);

const _sceneId = 'scn_programmer_interview';

final _scene = testScene(
  id: _sceneId,
  experience: PracticeExperience.interview,
  category: SceneCategory.interviewProfessional,
  name: 'English interview for technical roles',
  version: 1,
  prompt: _interviewPrompt,
);

final _otherScene = testScene(
  id: 'scn_general_speaking',
  experience: PracticeExperience.workplace,
  category: SceneCategory.workplaceGeneral,
  name: 'General speaking practice',
  version: 1,
  prompt: _workplacePrompt,
);

final _examScene = testScene(
  id: 'scn_ielts_speaking_test',
  experience: PracticeExperience.ieltsSpeaking,
  category: SceneCategory.ieltsSpeaking,
  name: 'IELTS Speaking Part 2',
  version: 1,
);

final _dailyScene = testScene(
  id: 'scn_daily_hotel_checkin_issue',
  experience: PracticeExperience.lifeAndTravel,
  category: SceneCategory.lifeTravel,
  name: 'Hotel check-in and issue handling',
  version: 1,
);

final _otherRole = testRole(
  id: 'role_general_coach',
  sceneId: 'scn_general_speaking',
  type: 'COACH',
  displayName: 'Speaking coach',
  responsibilities: 'Guide a focused conversation.',
  style: 'Supportive.',
  practiceObjectiveIds: ['fluency'],
);

final _otherDetail = testScene(
  id: _otherScene.id,
  experience: _otherScene.experience,
  category: _otherScene.category,
  name: _otherScene.name,
  version: _otherScene.version,
  status: _otherScene.status,
  prompt: _workplacePrompt,
  roles: [_otherRole],
  practiceOptions: [
    testPracticeOption(
      id: 'option_general_full',
      sceneId: 'scn_general_speaking',
      mode: PracticeMode.fullSimulation,
      displayName: 'Full practice',
    ),
    testPracticeOption(
      id: 'option_general_focus',
      sceneId: 'scn_general_speaking',
      roleId: 'role_general_coach',
      mode: PracticeMode.focus,
      displayName: 'Fluency focus',
    ),
  ],
);

const _interviewPrompt = ScenePrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: ['introduction', 'system_design'],
  turnBlueprints: ['Ask for a project overview.'],
);

const _workplacePrompt = ScenePrompt(
  publicSceneBrief: 'Give a workplace progress update.',
  practiceGoal: 'Explain progress and risk clearly.',
  userRole: 'Project owner',
  aiRole: 'Stakeholder',
  personaSummary: 'Direct and supportive.',
  focusAreas: ['fluency'],
  turnBlueprints: ['Ask for current progress.'],
);

final _technicalRole = testRole(
  id: 'role_technical_interviewer',
  sceneId: _sceneId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical depth perspective',
  responsibilities: 'Probe technical depth and decision making.',
  style: 'Precise and evidence seeking.',
  practiceObjectiveIds: ['system_design'],
);

final _recruiterRole = testRole(
  id: 'role_hr_interviewer',
  sceneId: _sceneId,
  type: 'HR_INTERVIEWER',
  displayName: 'Recruiter and motivation perspective',
  responsibilities: 'Explore motivation and communication clarity.',
  style: 'Warm and structured.',
  practiceObjectiveIds: ['motivation'],
);

final _projectRole = testRole(
  id: 'role_project_manager',
  sceneId: _sceneId,
  type: 'PROJECT_MANAGER',
  displayName: 'Delivery and collaboration perspective',
  responsibilities: 'Explore delivery and collaboration.',
  style: 'Outcome oriented.',
  practiceObjectiveIds: ['delivery'],
);

final _leadershipRole = testRole(
  id: 'role_executive_interviewer',
  sceneId: _sceneId,
  type: 'EXECUTIVE_INTERVIEWER',
  displayName: 'Leadership and impact perspective',
  responsibilities: 'Optional for senior, lead, or management roles.',
  style: 'Concise and high level.',
  practiceObjectiveIds: ['impact'],
);

final _roles = [_technicalRole, _recruiterRole, _projectRole, _leadershipRole];

final _detail = testScene(
  id: _scene.id,
  experience: _scene.experience,
  category: _scene.category,
  name: _scene.name,
  version: _scene.version,
  status: _scene.status,
  prompt: _interviewPrompt,
  roles: _roles,
  practiceOptions: [
    testPracticeOption(
      id: 'option_full_simulation',
      sceneId: _sceneId,
      mode: PracticeMode.fullSimulation,
      displayName: 'Full simulation',
    ),
    testPracticeOption(
      id: 'option_technical_focus',
      sceneId: _sceneId,
      roleId: 'role_technical_interviewer',
      mode: PracticeMode.focus,
      displayName: 'Technical depth focus',
    ),
    testPracticeOption(
      id: 'option_hr_focus',
      sceneId: _sceneId,
      roleId: 'role_hr_interviewer',
      mode: PracticeMode.focus,
      displayName: 'Recruiter and motivation focus',
    ),
    testPracticeOption(
      id: 'option_project_manager_focus',
      sceneId: _sceneId,
      roleId: 'role_project_manager',
      mode: PracticeMode.focus,
      displayName: 'Delivery and collaboration focus',
    ),
    testPracticeOption(
      id: 'option_executive_focus',
      sceneId: _sceneId,
      roleId: 'role_executive_interviewer',
      mode: PracticeMode.focus,
      displayName: 'Leadership and impact focus',
    ),
  ],
);
