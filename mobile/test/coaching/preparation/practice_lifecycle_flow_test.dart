import '../../support/scene_fixtures.dart';
import '../../support/practice_fixtures.dart';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

import 'preparation_test_fakes.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'training owns independent resumable practices without a home-thread precondition',
    (tester) async {
      final agentClient = GoalAwareAgentClient();
      final practiceClient = _LifecyclePracticeClient();
      var agentOperationSequence = 0;
      final conversationController = ConversationController(
        client: agentClient,
        clientIdFactory: (scope) =>
            '$scope-lifecycle-${++agentOperationSequence}',
      );
      final practiceController = PracticeController(
        client: practiceClient,
        clientIdFactory: (scope) =>
            '$scope-practice-${++agentOperationSequence}',
      );
      await conversationController.initialize();
      final homeThreadId = conversationController.threadId!;
      expect(conversationController.threads, hasLength(1));

      final workspaceController = PracticeWorkspaceController(
        conversationController: conversationController,
        practiceController: practiceController,
        recordStore: MemoryPracticeLaunchRecordStore(),
      );
      await workspaceController.activateAccount('account-lifecycle-flow');

      final launchClient = _LifecycleLaunchClient();
      var launchOperationSequence = 0;
      final launchController = PreparationLaunchController(
        client: launchClient,
        contextProvider: () {
          final threadId = conversationController.threadId;
          final goalId = conversationController.activeGoalId;
          if (threadId == null || goalId == null) {
            return null;
          }
          return AgentPracticeContext(threadId: threadId, goalId: goalId);
        },
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
            }) async {
              practiceClient.armStart(
                sessionId: bootstrap.session.id,
                planId: bootstrap.session.planId,
                scene: scene,
                practiceMode: bootstrap.session.practiceMode,
              );
              await practiceController.activateCreatedPractice(
                scene: scene,
                sessionId: bootstrap.session.id,
                planId: bootstrap.session.planId,
                practiceMode: bootstrap.session.practiceMode,
                turnLimit: bootstrap.maxEffectiveTurns,
                clientOperationId: clientOperationId,
              );
            },
        workspaceController: workspaceController,
        idFactory: (scope) => '$scope-lifecycle-${++launchOperationSequence}',
      );
      final preparationController = PreparationController(
        client: _LifecycleCatalogClient(),
      );
      addTearDown(() {
        launchController.dispose();
        workspaceController.dispose();
        preparationController.dispose();
        practiceController.dispose();
        conversationController.dispose();
      });

      await tester.pumpWidget(
        SpeakUpApp.preview(
          conversationController: conversationController,
          practiceController: practiceController,
          preparationController: preparationController,
          preparationLaunchController: launchController,
        ),
      );
      await tester.pumpAndSettle();

      await _tapVisible(tester, find.byKey(const Key('primary-tab-scenes')));
      await _openScene(tester, _progressScene);

      expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
      final firstPracticeThreadId = conversationController.threadId!;
      final firstSessionId = practiceController.practiceSessionId!;
      expect(firstPracticeThreadId, isNot(homeThreadId));
      expect(
        workspaceController.currentPracticeThreadId,
        firstPracticeThreadId,
      );
      expect(workspaceController.currentSessionId, firstSessionId);
      expect(conversationController.threads, hasLength(2));

      await _tapVisible(
        tester,
        find.byKey(const Key('immersive-open-keyboard')),
      );
      await tester.enterText(
        find.byKey(const Key('immersive-text-answer')),
        'The migration is on schedule, and I have isolated the main risk.',
      );
      await _tapVisible(tester, find.byKey(const Key('immersive-submit-text')));

      expect(practiceController.completedTurns, 1);
      expect(practiceController.practiceSessionVersion, 2);
      expect(practiceClient.snapshotFor(firstSessionId)?.completedTurns, 1);

      await _leavePractice(tester);

      expect(find.byKey(const Key('immersive-roleplay-page')), findsNothing);
      expect(find.byKey(const Key('practice-continuation')), findsNothing);
      expect(conversationController.threadId, homeThreadId);
      expect(practiceController.hasActivePractice, isFalse);
      expect(workspaceController.hasResumable, isTrue);

      await _tapVisible(tester, find.byKey(const Key('primary-tab-agent')));
      await _tapVisible(
        tester,
        find.byKey(const Key('quick-action-continue-practice')),
      );
      await _waitForPracticePage(tester);

      expect(conversationController.threadId, firstPracticeThreadId);
      expect(practiceController.hasActivePractice, isTrue);
      expect(workspaceController.errorMessage, isNull);
      expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
      expect(workspaceController.hasResumable, isTrue);

      await _leavePractice(tester);
      expect(conversationController.threadId, homeThreadId);
      await _tapVisible(
        tester,
        find.byKey(const Key('quick-action-continue-practice')),
      );
      await _waitForPracticePage(tester);

      expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
      expect(conversationController.threadId, firstPracticeThreadId);
      expect(practiceController.practiceSessionId, firstSessionId);
      expect(practiceController.completedTurns, 1);
      expect(practiceController.practiceSessionVersion, 2);

      await _leavePractice(tester);
      expect(conversationController.threadId, homeThreadId);

      expect(await conversationController.createThread(), isTrue);
      final unrelatedPracticeThreadId = conversationController.threadId!;
      final unrelatedScene = testScene(
        id: 'unrelated-practice',
        name: '其他练习',
        prompt: const ScenePrompt(
          publicSceneBrief:
              'A different active practice selected from history.',
          practiceGoal: 'Complete a different practice.',
          userRole: 'Learner',
          aiRole: 'Coach',
          personaSummary: 'Structured and focused.',
          focusAreas: <String>['clarity'],
          turnBlueprints: <String>['Ask one relevant question.'],
        ),
      );
      await activateTestGoal(
        goalClient: agentClient,
        conversationController: conversationController,
        threadId: unrelatedPracticeThreadId,
        scene: unrelatedScene,
        clientOperationId: 'unrelated-legacy-goal',
      );
      practiceClient.armStart(
        sessionId: 'unrelated-legacy-session',
        planId: 'unrelated-legacy-plan',
        scene: unrelatedScene,
        practiceMode: unrelatedScene.practiceOptions.first.mode,
      );
      await practiceController.activateCreatedPractice(
        scene: unrelatedScene,
        sessionId: 'unrelated-legacy-session',
        planId: 'unrelated-legacy-plan',
        practiceMode: unrelatedScene.practiceOptions.first.mode,
        turnLimit: 3,
        clientOperationId: 'unrelated-legacy-voice',
      );
      await tester.pump();
      expect(practiceController.hasActivePractice, isTrue);
      await _tapVisible(tester, find.byKey(const Key('primary-tab-scenes')));
      await _openScene(tester, _progressScene);

      expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
      expect(conversationController.threadId, firstPracticeThreadId);
      expect(conversationController.threadId, isNot(unrelatedPracticeThreadId));
      expect(practiceController.practiceSessionId, firstSessionId);
      expect(practiceController.completedTurns, 1);
      expect(practiceController.practiceSessionVersion, 2);

      await _leavePractice(tester);
      expect(conversationController.threadId, homeThreadId);

      expect(find.byKey(const Key('practice-continuation')), findsNothing);
      await _openScene(tester, _progressScene);

      expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
      expect(find.text('开始新的练习？'), findsNothing);
      expect(practiceController.practiceSessionId, firstSessionId);
      expect(practiceController.completedTurns, 1);

      await _leavePractice(tester);
      expect(conversationController.threadId, homeThreadId);

      await _openScene(tester, _hotelScene);

      expect(find.text('开始新的练习？'), findsOneWidget);
      expect(find.text('开始“${_hotelScene.name}”'), findsOneWidget);
      expect(practiceClient.endedSessionIds, isEmpty);

      final replace = find.byKey(const Key('replace-existing-practice'));
      expect(replace, findsOneWidget);
      await tester.tap(replace);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await _confirmScenarioPreparation(tester);

      expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
      expect(practiceClient.endedSessionIds, [firstSessionId]);
      expect(practiceClient.snapshotFor(firstSessionId), isNull);
      expect(conversationController.threadId, isNot(firstPracticeThreadId));
      expect(conversationController.threadId, isNot(homeThreadId));
      expect(practiceController.practiceSessionId, isNot(firstSessionId));
      expect(
        practiceController.practiceSessionId,
        launchClient.sessionIds.last,
      );
      expect(practiceController.completedTurns, 0);
      expect(workspaceController.currentSceneId, _hotelScene.id);
      expect(conversationController.threads, hasLength(4));
      expect(launchClient.sessionIds, hasLength(2));
    },
  );
}

Future<void> _openScene(WidgetTester tester, SceneDefinition definition) async {
  final hubKey = switch (definition.category) {
    SceneCategory.roleplayWorkplace => const Key('practice-hub-workplace'),
    SceneCategory.roleplayDaily ||
    SceneCategory.roleplayTravel => const Key('practice-hub-life'),
    _ => throw ArgumentError.value(definition.category, 'category'),
  };
  await _tapVisible(tester, find.byKey(hubKey));
  final scene = find.byKey(Key('catalog-scene-${definition.id}'));
  expect(scene, findsOneWidget);
  await tester.ensureVisible(scene);
  await tester.pumpAndSettle();
  await tester.tap(scene);
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
  await _confirmScenarioPreparation(tester);
}

Future<void> _confirmScenarioPreparation(WidgetTester tester) async {
  if (find.byKey(const Key('scenario-preparation-form')).evaluate().isEmpty) {
    return;
  }
  final submit = find.byKey(const Key('scenario-preparation-submit'));
  await tester.scrollUntilVisible(
    submit,
    240,
    scrollable: find.byType(Scrollable).first,
  );
  await tester.tap(submit);
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 500));
}

Future<void> _leavePractice(WidgetTester tester) async {
  final backButton = find.descendant(
    of: find.byKey(const Key('immersive-roleplay-page')),
    matching: find.byKey(const Key('immersive-exit')),
  );
  expect(backButton, findsOneWidget);
  await tester.tap(backButton);
  await tester.pumpAndSettle();
}

Future<void> _waitForPracticePage(WidgetTester tester) async {
  final exit = find.descendant(
    of: find.byKey(const Key('immersive-roleplay-page')),
    matching: find.byKey(const Key('immersive-exit')),
  );
  for (var attempt = 0; attempt < 100 && exit.evaluate().isEmpty; attempt++) {
    await tester.pump(const Duration(milliseconds: 20));
  }
  expect(exit, findsOneWidget);
  await tester.pumpAndSettle();
}

Future<void> _tapVisible(WidgetTester tester, Finder finder) async {
  expect(finder, findsOneWidget);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

final class _LifecycleCatalogClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) async {
    return switch (sceneId) {
      _progressSceneId => _progressDetail,
      _hotelSceneId => _hotelDetail,
      _ => throw StateError('Unknown lifecycle scene: $sceneId'),
    };
  }

  @override
  Future<List<SceneDefinition>> listScenes() async {
    return <SceneDefinition>[_progressScene, _hotelScene];
  }

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async {
    return switch (sceneId) {
      _progressSceneId => <RoleDefinition>[_progressRole],
      _hotelSceneId => <RoleDefinition>[_hotelRole],
      _ => throw StateError('Unknown lifecycle scene: $sceneId'),
    };
  }
}

final class _LifecycleLaunchClient implements PreparationLaunchClient {
  final List<String> sessionIds = <String>[];
  final Map<String, PreparationSnapshot> _snapshots =
      <String, PreparationSnapshot>{};
  int _profileSequence = 0;
  int _snapshotSequence = 0;
  int _planSequence = 0;
  int _sessionSequence = 0;

  @override
  Future<void> clearAccountState() async {
    sessionIds.clear();
    _snapshots.clear();
  }

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    final sequence = ++_profileSequence;
    return PreparationProfile(
      id: 'lifecycle-profile-$sequence',
      userId: 'account-lifecycle-flow',
      backgroundSummary: input.backgroundSummary,
      version: 1,
      updatedAt: DateTime.utc(2026, 7, 29, 8, sequence),
    );
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    final sequence = ++_snapshotSequence;
    final snapshot = PreparationSnapshot(
      id: 'lifecycle-snapshot-$sequence',
      sourceProfileId: profileId,
      sourceVersion: sourceVersion,
      backgroundSnapshot: 'Lifecycle flow background $sequence',
      createdAt: DateTime.utc(2026, 7, 29, 8, sequence),
    );
    _snapshots[snapshot.id] = snapshot;
    return snapshot;
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    final planId = 'lifecycle-plan-${++_planSequence}';
    final snapshot = _snapshots[input.preparationSnapshotId];
    final scene = switch (input.sceneId) {
      _progressSceneId => _progressDetail,
      _hotelSceneId => _hotelDetail,
      _ => throw StateError('Unknown lifecycle Scene.'),
    };
    if (snapshot == null || scene.version != input.sceneVersion) {
      throw StateError('Plan did not use its exact Preparation inputs.');
    }
    return PracticePlan(
      id: planId,
      userId: 'account-lifecycle-flow',
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
        maxEffectiveTurns: 3,
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
      createdAt: DateTime.utc(2026, 7, 29, 8, _planSequence),
      updatedAt: DateTime.utc(2026, 7, 29, 8, _planSequence),
    );
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    if (input.expectedPlanRevision != plan.revision || !input.userConfirmed) {
      throw StateError('Session did not use its exact prepared plan.');
    }
    final sequence = ++_sessionSequence;
    final sessionId = 'lifecycle-session-$sequence';
    sessionIds.add(sessionId);
    return PreparationPracticeBootstrap(
      session: PreparationPracticeSession(
        id: sessionId,
        planId: plan.id,
        practiceExperience: plan.sceneSelection.scene.experience,
        sceneCategory: plan.sceneSelection.scene.category,
        practiceMode: plan.practiceOption.mode,
        snapshotId: plan.preparationSnapshot.id,
        status: 'starting',
        version: 1,
        createdAt: DateTime.utc(2026, 7, 29, 9, sequence),
      ),
      preparationSnapshotId: plan.preparationSnapshot.id,
      maxEffectiveTurns: plan.sessionPolicy.maxEffectiveTurns,
    );
  }
}

final class _LifecyclePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  final Map<String, PracticeSessionSnapshot> _sessions =
      <String, PracticeSessionSnapshot>{};
  final List<String> endedSessionIds = <String>[];
  _StartSeed? _nextStart;

  PracticeSessionSnapshot? snapshotFor(String sessionId) =>
      _sessions[sessionId];

  void armStart({
    required String sessionId,
    required String planId,
    required SceneDefinition scene,
    required PracticeMode practiceMode,
  }) {
    _nextStart = _StartSeed(
      sessionId: sessionId,
      planId: planId,
      scene: scene,
      practiceMode: practiceMode,
    );
  }

  @override
  Future<void> clearAccountState() async {
    _sessions.clear();
    endedSessionIds.clear();
    _nextStart = null;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    return _sessions[sessionId] ??
        (throw StateError('No exact lifecycle session was prepared.'));
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final seed = _nextStart;
    if (seed == null || seed.sessionId != sessionId) {
      throw StateError('No exact lifecycle session was prepared.');
    }
    _nextStart = null;
    final snapshot = PracticeSessionSnapshot(
      sessionId: seed.sessionId,
      planId: seed.planId,
      sessionVersion: 1,
      practiceExperience: seed.scene.experience,
      sceneCategory: seed.scene.category,
      practiceMode: seed.practiceMode,
      capabilities: testPracticeCapabilities,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-${seed.sessionId}-1',
        sessionId: seed.sessionId,
        text: 'Please begin this practice in English.',
      ),
    );
    _sessions[sessionId] = snapshot;
    return snapshot;
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    final snapshot = _sessions[sessionId];
    if (snapshot == null || snapshot.sessionVersion != expectedSessionVersion) {
      throw StateError('The exact lifecycle session could not be ended.');
    }
    _sessions.remove(sessionId);
    endedSessionIds.add(sessionId);
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.endedEarly,
      version: expectedSessionVersion + 1,
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async {
    final current = _sessions[sessionId];
    if (current == null) {
      throw StateError('The exact lifecycle session was not active.');
    }
    if (current.currentQuestion?.id != questionId ||
        current.sessionCompleted ||
        answerText.trim().isEmpty) {
      throw StateError('The submitted lifecycle turn was stale.');
    }
    final completedTurns = current.completedTurns + 1;
    final sessionVersion = current.sessionVersion + 1;
    final nextQuestion = PracticeQuestion(
      id: 'question-$sessionId-${completedTurns + 1}',
      sessionId: sessionId,
      text: 'Please add one concrete example.',
    );
    _sessions[sessionId] = PracticeSessionSnapshot(
      sessionId: current.sessionId,
      planId: current.planId,
      sessionVersion: sessionVersion,
      practiceExperience: current.practiceExperience,
      sceneCategory: current.sceneCategory,
      practiceMode: current.practiceMode,
      capabilities: current.capabilities,
      completedTurns: completedTurns,
      turnLimit: current.turnLimit,
      sessionCompleted: false,
      currentQuestion: nextQuestion,
    );
    return PracticeTurnConfirmation(
      turnId: 'turn-$sessionId-$completedTurns',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: 'text-candidate-$sessionId-$completedTurns',
      practiceExperience: current.practiceExperience,
      sceneCategory: current.sceneCategory,
      practiceMode: current.practiceMode,
      capabilities: current.capabilities,
      answer: PracticeMessage(
        id: 'answer-$sessionId-$completedTurns',
        role: PracticeMessageRole.user,
        text: answerText.trim(),
      ),
      completedTurns: completedTurns,
      turnLimit: current.turnLimit,
      sessionCompleted: false,
      sessionVersion: sessionVersion,
      nextQuestion: nextQuestion,
    );
  }
}

final class _StartSeed {
  const _StartSeed({
    required this.sessionId,
    required this.planId,
    required this.scene,
    required this.practiceMode,
  });

  final String sessionId;
  final String planId;
  final SceneDefinition scene;
  final PracticeMode practiceMode;
}

const _progressSceneId = 'scn_workplace_progress_risk_update';
const _hotelSceneId = 'scn_daily_hotel_checkin_issue';

final _progressScene = testScene(
  id: _progressSceneId,
  experience: PracticeExperience.roleplay,
  category: SceneCategory.roleplayWorkplace,
  name: '项目进度同步',
  version: 1,
  prompt: _progressPrompt,
);

final _hotelScene = testScene(
  id: _hotelSceneId,
  experience: PracticeExperience.roleplay,
  category: SceneCategory.roleplayTravel,
  name: '酒店入住与问题处理',
  version: 1,
  prompt: _hotelPrompt,
);

final _progressRole = testRole(
  id: 'role-project-stakeholder',
  sceneId: _progressSceneId,
  type: 'STAKEHOLDER',
  displayName: '项目协作方',
  responsibilities: '追问当前进展、主要风险和行动计划。',
  style: '直接、清晰。',
  practiceObjectiveIds: <String>['progress', 'risk', 'next_steps'],
);

final _hotelRole = testRole(
  id: 'role-hotel-receptionist',
  sceneId: _hotelSceneId,
  type: 'RECEPTIONIST',
  displayName: '酒店前台',
  responsibilities: '核对预订并帮助处理房间问题。',
  style: '礼貌、专业。',
  practiceObjectiveIds: <String>['check_in', 'issue_resolution'],
);

final _progressDetail = testScene(
  id: _progressScene.id,
  experience: _progressScene.experience,
  category: _progressScene.category,
  name: _progressScene.name,
  version: _progressScene.version,
  status: _progressScene.status,
  prompt: _progressPrompt,
  roles: <RoleDefinition>[_progressRole],
  practiceOptions: <PracticeOption>[
    testPracticeOption(
      id: 'option-workplace-progress-full',
      sceneId: _progressSceneId,
      mode: PracticeMode.fullSimulation,
      displayName: '完整情景练习',
    ),
    testPracticeOption(
      id: 'option-workplace-progress-focus',
      sceneId: _progressSceneId,
      roleId: 'role-project-stakeholder',
      mode: PracticeMode.focus,
      displayName: '风险表达专项',
    ),
  ],
);

final _hotelDetail = testScene(
  id: _hotelScene.id,
  experience: _hotelScene.experience,
  category: _hotelScene.category,
  name: _hotelScene.name,
  version: _hotelScene.version,
  status: _hotelScene.status,
  prompt: _hotelPrompt,
  roles: <RoleDefinition>[_hotelRole],
  practiceOptions: <PracticeOption>[
    testPracticeOption(
      id: 'option-hotel-checkin-full',
      sceneId: _hotelSceneId,
      mode: PracticeMode.fullSimulation,
      displayName: '完整情景练习',
    ),
    testPracticeOption(
      id: 'option-hotel-checkin-focus',
      sceneId: _hotelSceneId,
      roleId: 'role-hotel-receptionist',
      mode: PracticeMode.focus,
      displayName: '问题处理专项',
    ),
  ],
);

const _progressPrompt = ScenePrompt(
  publicSceneBrief: '向协作方同步项目进度、风险和下一步。',
  practiceGoal: '在一轮双向确认中表达清楚。',
  userRole: '项目负责人',
  aiRole: '项目协作方',
  personaSummary: '关注事实、风险和行动。',
  focusAreas: <String>['progress', 'risk'],
  turnBlueprints: <String>['询问进度。', '追问风险与下一步。'],
);

const _hotelPrompt = ScenePrompt(
  publicSceneBrief: '办理酒店入住并沟通一个房间问题。',
  practiceGoal: '礼貌提出需求并确认解决方案。',
  userRole: '住客',
  aiRole: '酒店前台',
  personaSummary: '专业并愿意协助。',
  focusAreas: <String>['check_in', 'issue_resolution'],
  turnBlueprints: <String>['核对预订。', '协商房间问题。'],
);
