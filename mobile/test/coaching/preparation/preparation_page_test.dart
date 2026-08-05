import '../../support/scene_fixtures.dart';
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';

import 'preparation_test_fakes.dart';

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

Future<void> _openInterviewScene(
  WidgetTester tester, {
  required String sceneId,
  required String modeId,
}) async {
  final scene = find.byKey(Key('catalog-scene-$sceneId'));
  if (scene.evaluate().isEmpty) {
    final mode = find.byKey(Key('interview-mode-$modeId'));
    await tester.scrollUntilVisible(
      mode,
      140,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(mode);
    await tester.pumpAndSettle();
  }
  final revealedScene = find.byKey(Key('catalog-scene-$sceneId'));
  await tester.ensureVisible(revealedScene);
  await tester.tap(revealedScene);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'product training center separates full interview from specialties',
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
      expect(controller.selectedScene, isNull);

      await _openInterviewScene(
        tester,
        sceneId: _sceneId,
        modeId: 'professional',
      );

      expect(controller.selectedScene, isNull);
      expect(find.byKey(const Key('preparation-scene-config')), findsNothing);
      expect(find.text('选择对话角色'), findsNothing);
    },
  );

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
      const Key('catalog-scene-scn_interview_recruiter_screening'),
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

  testWidgets('starts the real typed chain and reports success to navigation', (
    tester,
  ) async {
    final agentClient = GoalAwareAgentClient();
    final conversationController = ConversationController(client: agentClient);
    final preparationController = PreparationController(
      client: _FixtureClient(),
    );
    final launchClient = _PageLaunchClient();
    var navigations = 0;
    await conversationController.initialize();
    await activateTestGoal(
      goalClient: agentClient,
      conversationController: conversationController,
      threadId: conversationController.threadId!,
      scene: _scene,
      clientOperationId: 'activate-page-scene',
    );
    final launchController = PreparationLaunchController(
      client: launchClient,
      contextProvider: () => AgentPracticeContext(
        threadId: conversationController.threadId!,
        goalId: conversationController.activeGoalId!,
      ),
      threadIdProvider: () => conversationController.threadId,
      goalActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) async {
            final goal = await activateTestGoal(
              goalClient: agentClient,
              conversationController: conversationController,
              threadId: threadId,
              scene: selection.scene,
              clientOperationId: clientOperationId,
            );
            return AgentPracticeContext(threadId: threadId, goalId: goal.id);
          },
      voiceActivator:
          ({
            required context,
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {},
      idFactory: (scope) => '$scope-widget-key',
    );
    addTearDown(conversationController.dispose);
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
    await _openFamily(tester, 'INTERVIEW');
    await tester.tap(find.byKey(const Key('catalog-scene-$_sceneId')));
    await tester.pumpAndSettle();

    expect(launchClient.calls, ['profile', 'snapshot', 'plan', 'session']);
    expect(navigations, 1);
    expect(find.byKey(const Key('preparation-scene-detail')), findsNothing);
    expect(conversationController.threadId, isNotNull);
    expect(conversationController.activeGoalId, isNotNull);
    expect(launchController.bootstrap?.maxEffectiveTurns, 6);
  });

  testWidgets('direct start reports a missing practice context in place', (
    tester,
  ) async {
    final preparationController = PreparationController(
      client: _FixtureClient(),
    );
    final launchClient = _PageLaunchClient();
    final launchController = PreparationLaunchController(
      client: launchClient,
      contextProvider: () => null,
      threadIdProvider: () => null,
      goalActivator:
          ({
            required threadId,
            required selection,
            required clientOperationId,
          }) {
            throw StateError('Goal activation must wait for the Thread.');
          },
      voiceActivator:
          ({
            required context,
            required scene,
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
    await tester.tap(find.byKey(const Key('catalog-scene-$_sceneId')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('preparation-scene-launch-status')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('preparation-launch-error')), findsOneWidget);
    expect(find.text('选择对话角色'), findsNothing);
    expect(launchClient.calls, isEmpty);
  });

  testWidgets('direct start stays locked until voice retry succeeds', (
    tester,
  ) async {
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
    await tester.tap(find.byKey(const Key('catalog-scene-$_sceneId')));
    await tester.pump();
    await tester.pump();

    expect(launchClient.calls, ['profile', 'snapshot', 'plan', 'session']);
    expect(launchController.isStarting, isTrue);
    expect(
      tester
          .widget<IconButton>(
            find.byKey(const Key('preparation-back-to-catalog')),
          )
          .onPressed,
      isNull,
    );
    expect(find.text('选择对话角色'), findsNothing);

    session.complete(
      PreparationPracticeBootstrap(
        session: PreparationPracticeSession(
          id: 'session-1',
          planId: 'plan-1',
          sceneFamily: SceneFamily.interview,
          sceneModel: SceneModel.projectExperienceDeepDive,
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

    expect(launchController.canRetry, isTrue);
    expect(navigations, 0);
    await tester.tap(find.byKey(const Key('preparation-catalog-retry')));
    await tester.pumpAndSettle();

    expect(voiceCalls, 2);
    expect(navigations, 1);
    expect(
      find.byKey(const Key('preparation-scene-launch-status')),
      findsNothing,
    );
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

final class _InterviewSummaryClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() async => [
    _scene,
    _secondInterviewScene,
  ];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }
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
  _PageLaunchClient({this.sessionCompleter});

  final Completer<PreparationPracticeBootstrap>? sessionCompleter;
  final calls = <String>[];
  PreparationLaunchSelection? selection;
  PreparationSnapshot? _snapshot;

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
    final snapshot = PreparationSnapshot(
      id: 'preparation-snapshot-1',
      sourceProfileId: profileId,
      sourceVersion: sourceVersion,
      backgroundSnapshot: 'Backend engineer preparing a technical interview.',
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
        sceneFamily: plan.sceneSelection.scene.family,
        sceneModel: plan.sceneSelection.scene.model,
        snapshotId: 'session-snapshot-1',
        status: 'starting',
        version: 1,
        createdAt: DateTime.utc(2026, 7, 26),
      ),
      preparationSnapshotId: plan.preparationSnapshot.id,
      maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
    );
    return sessionCompleter?.future ?? bootstrap;
  }
}

const _pageContext = AgentPracticeContext(
  threadId: '10000000-0000-4000-8000-000000000001',
  goalId: '40000000-0000-4000-8000-000000000001',
);

const _sceneId = 'scn_programmer_interview';

final _scene = testScene(
  id: _sceneId,
  family: SceneFamily.interview,
  model: SceneModel.projectExperienceDeepDive,
  name: 'English interview for technical roles',
  version: 1,
  prompt: _interviewPrompt,
);

final _secondInterviewScene = testScene(
  id: 'scn_interview_recruiter_screening',
  family: SceneFamily.interview,
  model: SceneModel.interviewBasicDialogue,
  name: 'Recruiter screening',
  version: 1,
  prompt: _recruiterPrompt,
);

final _otherScene = testScene(
  id: 'scn_general_speaking',
  family: SceneFamily.workplace,
  model: SceneModel.progressAndRiskUpdate,
  name: 'General speaking practice',
  version: 1,
  prompt: _workplacePrompt,
);

final _examScene = testScene(
  id: 'scn_ielts_speaking_part_2',
  family: SceneFamily.exam,
  model: SceneModel.ieltsSpeakingPart2,
  name: 'IELTS Speaking Part 2',
  version: 1,
);

final _dailyScene = testScene(
  id: 'scn_daily_hotel_checkin_issue',
  family: SceneFamily.daily,
  model: SceneModel.hotelCheckinAndIssueHandling,
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
  family: _otherScene.family,
  model: _otherScene.model,
  name: _otherScene.name,
  version: _otherScene.version,
  status: _otherScene.status,
  turnPolicyRef: _otherScene.turnPolicyRef,
  sessionPolicyRef: _otherScene.sessionPolicyRef,
  prompt: _workplacePrompt,
  roles: [_otherRole],
  practiceOptions: [
    testPracticeOption(
      id: 'option_general_full',
      sceneId: 'scn_general_speaking',
      type: PracticeOptionType.fullSimulation,
      displayName: 'Full practice',
    ),
    testPracticeOption(
      id: 'option_general_focus',
      sceneId: 'scn_general_speaking',
      roleId: 'role_general_coach',
      type: PracticeOptionType.focus,
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
  suggestedDurationSeconds: 900,
);

const _workplacePrompt = ScenePrompt(
  publicSceneBrief: 'Give a workplace progress update.',
  practiceGoal: 'Explain progress and risk clearly.',
  userRole: 'Project owner',
  aiRole: 'Stakeholder',
  personaSummary: 'Direct and supportive.',
  focusAreas: ['fluency'],
  turnBlueprints: ['Ask for current progress.'],
  suggestedDurationSeconds: 600,
);

const _recruiterPrompt = ScenePrompt(
  publicSceneBrief: 'Discuss motivation, role fit, and basic expectations.',
  practiceGoal: 'Explain motivation and expectations clearly.',
  userRole: 'Candidate',
  aiRole: 'Recruiter',
  personaSummary: 'Warm and structured.',
  focusAreas: ['motivation'],
  turnBlueprints: ['Ask about motivation and role fit.'],
  suggestedDurationSeconds: 600,
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
  family: _scene.family,
  model: _scene.model,
  name: _scene.name,
  version: _scene.version,
  status: _scene.status,
  turnPolicyRef: _scene.turnPolicyRef,
  sessionPolicyRef: _scene.sessionPolicyRef,
  prompt: _interviewPrompt,
  roles: _roles,
  practiceOptions: [
    testPracticeOption(
      id: 'option_full_simulation',
      sceneId: _sceneId,
      type: PracticeOptionType.fullSimulation,
      displayName: 'Full simulation',
    ),
    testPracticeOption(
      id: 'option_technical_focus',
      sceneId: _sceneId,
      roleId: 'role_technical_interviewer',
      type: PracticeOptionType.focus,
      displayName: 'Technical depth focus',
    ),
    testPracticeOption(
      id: 'option_hr_focus',
      sceneId: _sceneId,
      roleId: 'role_hr_interviewer',
      type: PracticeOptionType.focus,
      displayName: 'Recruiter and motivation focus',
    ),
    testPracticeOption(
      id: 'option_project_manager_focus',
      sceneId: _sceneId,
      roleId: 'role_project_manager',
      type: PracticeOptionType.focus,
      displayName: 'Delivery and collaboration focus',
    ),
    testPracticeOption(
      id: 'option_executive_focus',
      sceneId: _sceneId,
      roleId: 'role_executive_interviewer',
      type: PracticeOptionType.focus,
      displayName: 'Leadership and impact focus',
    ),
  ],
);
