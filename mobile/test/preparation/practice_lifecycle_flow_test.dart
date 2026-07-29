import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_client.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'training owns independent resumable practices without a home-thread precondition',
    (tester) async {
      final agentClient = FakeAgentClient();
      final practiceClient = _LifecyclePracticeClient();
      var agentOperationSequence = 0;
      final agentController = AgentController(
        client: agentClient,
        practiceClient: practiceClient,
        clientIdFactory: (scope) =>
            '$scope-lifecycle-${++agentOperationSequence}',
      );
      await agentController.initialize();
      final homeThreadId = agentController.threadId!;
      expect(agentController.threads, hasLength(1));

      final workspaceController = PracticeWorkspaceController(
        agentController: agentController,
        recordStore: MemoryPracticeLaunchRecordStore(),
      );
      await workspaceController.activateAccount('account-lifecycle-flow');

      final launchClient = _LifecycleLaunchClient();
      var launchOperationSequence = 0;
      final launchController = PreparationLaunchController(
        client: launchClient,
        contextProvider: () {
          final threadId = agentController.threadId;
          final matterId = agentController.activeMatter?.id;
          if (threadId == null || matterId == null) {
            return null;
          }
          return AgentPracticeContext(threadId: threadId, matterId: matterId);
        },
        threadIdProvider: () => agentController.threadId,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async {
              final matter = await agentController.activateMatterForScenario(
                threadId: threadId,
                scene: AgentScene(
                  id: selection.scenarioDefinitionId,
                  title: selection.scenarioDisplayName,
                  description: selection.scenarioDescription,
                ),
                clientOperationId: clientOperationId,
              );
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
            }) async {
              practiceClient.armStart(
                threadId: context.threadId,
                sessionId: bootstrap.session.id,
                planId: bootstrap.session.planId,
              );
              await agentController.activateCreatedPractice(
                threadId: context.threadId,
                matterId: context.matterId,
                sessionId: bootstrap.session.id,
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
        agentController.dispose();
      });

      await tester.pumpWidget(
        SpeakUpApp.preview(
          agentController: agentController,
          preparationController: preparationController,
          preparationLaunchController: launchController,
        ),
      );
      await tester.pumpAndSettle();

      await _tapVisible(tester, find.byKey(const Key('primary-tab-scenes')));
      await _openScenario(tester, _progressScenario.id);

      expect(find.byKey(const Key('practice-page')), findsOneWidget);
      final firstPracticeThreadId = agentController.threadId!;
      final firstSessionId = agentController.practiceSessionId!;
      expect(firstPracticeThreadId, isNot(homeThreadId));
      expect(
        workspaceController.currentPracticeThreadId,
        firstPracticeThreadId,
      );
      expect(workspaceController.currentSessionId, firstSessionId);
      expect(agentController.threads, hasLength(2));

      await _tapVisible(
        tester,
        find.byKey(const Key('practice-open-keyboard')),
      );
      await tester.enterText(
        find.byKey(const Key('practice-text-answer')),
        'The migration is on schedule, and I have isolated the main risk.',
      );
      await _tapVisible(tester, find.byKey(const Key('practice-submit-text')));

      expect(agentController.completedTurns, 1);
      expect(agentController.practiceSessionVersion, 2);
      expect(
        practiceClient.snapshotFor(firstPracticeThreadId)?.completedTurns,
        1,
      );

      await _leavePractice(tester);

      expect(find.byKey(const Key('practice-page')), findsNothing);
      expect(find.byKey(const Key('practice-continuation')), findsOneWidget);
      expect(agentController.threadId, homeThreadId);
      expect(agentController.hasActivePractice, isFalse);
      expect(workspaceController.hasResumable, isTrue);

      await _tapVisible(tester, find.byKey(const Key('primary-tab-agent')));
      practiceClient.holdNextRestore();
      await tester.tap(find.byKey(const Key('quick-action-continue-practice')));
      await practiceClient.restoreStarted;
      await tester.tap(find.byKey(const Key('primary-tab-review')));
      await tester.pump();
      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(find.byKey(const Key('review-page')), findsNothing);
      practiceClient.releaseRestore();
      for (var attempt = 0; attempt < 100; attempt++) {
        await tester.pump(const Duration(milliseconds: 20));
        if (find.byKey(const Key('practice-page')).evaluate().isNotEmpty) {
          break;
        }
      }

      expect(agentController.threadId, firstPracticeThreadId);
      expect(agentController.hasActivePractice, isTrue);
      expect(workspaceController.errorMessage, isNull);
      expect(find.byKey(const Key('practice-page')), findsOneWidget);
      expect(workspaceController.hasResumable, isTrue);

      await _leavePractice(tester);
      expect(agentController.threadId, homeThreadId);
      await _tapVisible(
        tester,
        find.byKey(const Key('quick-action-continue-practice')),
      );

      expect(find.byKey(const Key('practice-page')), findsOneWidget);
      expect(agentController.threadId, firstPracticeThreadId);
      expect(agentController.practiceSessionId, firstSessionId);
      expect(agentController.completedTurns, 1);
      expect(agentController.practiceSessionVersion, 2);

      await _leavePractice(tester);
      expect(agentController.threadId, homeThreadId);

      expect(await agentController.createThread(), isTrue);
      final unrelatedPracticeThreadId = agentController.threadId!;
      final unrelatedMatter = await agentController.activateMatterForScenario(
        threadId: unrelatedPracticeThreadId,
        scene: const AgentScene(
          id: 'unrelated-legacy-practice',
          title: '旧练习',
          description: 'A different active practice selected from history.',
        ),
        clientOperationId: 'unrelated-legacy-matter',
      );
      practiceClient.armStart(
        threadId: unrelatedPracticeThreadId,
        sessionId: 'unrelated-legacy-session',
        planId: 'unrelated-legacy-plan',
      );
      await agentController.activateCreatedPractice(
        threadId: unrelatedPracticeThreadId,
        matterId: unrelatedMatter.id,
        sessionId: 'unrelated-legacy-session',
        turnLimit: 3,
        clientOperationId: 'unrelated-legacy-voice',
      );
      await tester.pump();
      expect(agentController.hasActivePractice, isTrue);
      await _tapVisible(tester, find.byKey(const Key('primary-tab-scenes')));
      await _tapVisible(tester, find.byKey(const Key('practice-continuation')));

      expect(find.byKey(const Key('practice-page')), findsOneWidget);
      expect(agentController.threadId, firstPracticeThreadId);
      expect(agentController.threadId, isNot(unrelatedPracticeThreadId));
      expect(agentController.practiceSessionId, firstSessionId);
      expect(agentController.completedTurns, 1);
      expect(agentController.practiceSessionVersion, 2);

      await _leavePractice(tester);
      expect(agentController.threadId, homeThreadId);

      expect(find.byKey(const Key('practice-continuation')), findsOneWidget);
      await _openScenario(tester, _hotelScenario.id);

      expect(find.text('你还有一项练习未完成'), findsOneWidget);
      expect(find.text('结束并开始新的'), findsOneWidget);
      expect(practiceClient.endedSessionIds, isEmpty);

      final replace = find.byKey(const Key('replace-existing-practice'));
      expect(replace, findsOneWidget);
      await tester.tap(replace);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

      expect(find.byKey(const Key('practice-page')), findsOneWidget);
      expect(practiceClient.endedSessionIds, [firstSessionId]);
      expect(practiceClient.snapshotFor(firstPracticeThreadId), isNull);
      expect(agentController.threadId, isNot(firstPracticeThreadId));
      expect(agentController.threadId, isNot(homeThreadId));
      expect(agentController.practiceSessionId, isNot(firstSessionId));
      expect(agentController.practiceSessionId, launchClient.sessionIds.last);
      expect(agentController.completedTurns, 0);
      expect(workspaceController.currentScenarioId, _hotelScenario.id);
      expect(agentController.threads, hasLength(4));
      expect(launchClient.sessionIds, hasLength(2));
    },
  );
}

Future<void> _openScenario(WidgetTester tester, String scenarioId) async {
  await _tapVisible(tester, find.byKey(const Key('practice-hub-roleplay')));
  final scenario = find.byKey(Key('catalog-scenario-$scenarioId'));
  expect(scenario, findsOneWidget);
  await tester.ensureVisible(scenario);
  await tester.pumpAndSettle();
  await tester.tap(scenario);
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
}

Future<void> _leavePractice(WidgetTester tester) async {
  final backButton = find.descendant(
    of: find.byKey(const Key('practice-page')),
    matching: find.byType(BackButton),
  );
  expect(backButton, findsOneWidget);
  await tester.tap(backButton);
  await tester.pumpAndSettle();
}

Future<void> _tapVisible(WidgetTester tester, Finder finder) async {
  expect(finder, findsOneWidget);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

final class _LifecycleCatalogClient implements PreparationCatalogClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) async {
    return switch (scenarioId) {
      _progressScenarioId => _progressDetail,
      _hotelScenarioId => _hotelDetail,
      _ => throw StateError('Unknown lifecycle scenario: $scenarioId'),
    };
  }

  @override
  Future<List<PreparationScenario>> listScenarios() async {
    return const <PreparationScenario>[_progressScenario, _hotelScenario];
  }

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) async {
    return switch (scenarioId) {
      _progressScenarioId => const <PreparationRole>[_progressRole],
      _hotelScenarioId => const <PreparationRole>[_hotelRole],
      _ => throw StateError('Unknown lifecycle scenario: $scenarioId'),
    };
  }
}

final class _LifecycleLaunchClient implements PreparationLaunchClient {
  final List<String> sessionIds = <String>[];
  final Map<String, PreparationLaunchSelection> _planSelections =
      <String, PreparationLaunchSelection>{};
  int _profileSequence = 0;
  int _snapshotSequence = 0;
  int _planSequence = 0;
  int _sessionSequence = 0;

  @override
  Future<void> clearAccountState() async {
    sessionIds.clear();
    _planSelections.clear();
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
    return PreparationSnapshot(
      id: 'lifecycle-snapshot-$sequence',
      sourceProfileId: profileId,
      sourceVersion: sourceVersion,
      backgroundSnapshot: 'Lifecycle flow background $sequence',
      createdAt: DateTime.utc(2026, 7, 29, 8, sequence),
    );
  }

  @override
  Future<PreparationPracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    final planId = 'lifecycle-plan-${++_planSequence}';
    _planSelections[planId] = input.selection;
    return PreparationPracticePlan(
      id: planId,
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
    final selection = _planSelections[planId];
    if (selection == null || selection != input.selection) {
      throw StateError('Session did not use its exact prepared plan.');
    }
    final sequence = ++_sessionSequence;
    final sessionId = 'lifecycle-session-$sequence';
    sessionIds.add(sessionId);
    return PreparationPracticeBootstrap(
      session: PreparationPracticeSession(
        id: sessionId,
        planId: planId,
        scenarioType: input.selection.scenarioType,
        scenarioModel: input.selection.scenarioModel,
        snapshotId: input.preparationSnapshotId,
        status: 'starting',
        version: 1,
        createdAt: DateTime.utc(2026, 7, 29, 9, sequence),
      ),
      preparationSnapshotId: input.preparationSnapshotId,
      maxEffectiveTurns: 3,
    );
  }
}

final class _LifecyclePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  final Map<String, PracticeSessionSnapshot> _sessions =
      <String, PracticeSessionSnapshot>{};
  final List<String> endedSessionIds = <String>[];
  _StartSeed? _nextStart;
  Completer<void>? _restoreGate;
  Completer<void>? _restoreStarted;

  PracticeSessionSnapshot? snapshotFor(String threadId) => _sessions[threadId];

  Future<void> get restoreStarted => _restoreStarted!.future;

  void holdNextRestore() {
    _restoreGate = Completer<void>();
    _restoreStarted = Completer<void>();
  }

  void releaseRestore() {
    _restoreGate?.complete();
  }

  void armStart({
    required String threadId,
    required String sessionId,
    required String planId,
  }) {
    _nextStart = _StartSeed(
      threadId: threadId,
      sessionId: sessionId,
      planId: planId,
    );
  }

  @override
  Future<void> clearAccountState() async {
    _sessions.clear();
    endedSessionIds.clear();
    _nextStart = null;
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    if (_restoreGate case final gate?) {
      _restoreStarted?.complete();
      await gate.future;
      if (identical(_restoreGate, gate)) {
        _restoreGate = null;
      }
      _restoreStarted = null;
    }
    return _sessions[threadId];
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    final seed = _nextStart;
    if (seed == null || seed.threadId != threadId) {
      throw StateError('No exact lifecycle session was prepared.');
    }
    _nextStart = null;
    final snapshot = PracticeSessionSnapshot(
      sessionId: seed.sessionId,
      planId: seed.planId,
      threadId: threadId,
      sessionVersion: 1,
      matter: activeMatter,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-${seed.sessionId}-1',
        sessionId: seed.sessionId,
        text: 'Please begin this practice in English.',
      ),
    );
    _sessions[threadId] = snapshot;
    return PracticeStartResult(snapshot: snapshot);
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    final matches = _sessions.entries
        .where((entry) => entry.value.sessionId == sessionId)
        .toList(growable: false);
    if (matches.length != 1 ||
        matches.single.value.sessionVersion != expectedSessionVersion) {
      throw StateError('The exact lifecycle session could not be ended.');
    }
    _sessions.remove(matches.single.key);
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
    final matches = _sessions.entries
        .where((entry) => entry.value.sessionId == sessionId)
        .toList(growable: false);
    if (matches.length != 1) {
      throw StateError('The exact lifecycle session was not active.');
    }
    final threadId = matches.single.key;
    final current = matches.single.value;
    if (current.currentQuestion?.id != questionId ||
        current.sessionCompleted ||
        answerText.trim().isEmpty) {
      throw StateError('The submitted lifecycle turn was stale.');
    }
    final completedTurns = current.completedTurns + 1;
    final sessionVersion = (current.sessionVersion ?? 1) + 1;
    final nextQuestion = PracticeQuestion(
      id: 'question-$sessionId-${completedTurns + 1}',
      sessionId: sessionId,
      text: 'Please add one concrete example.',
    );
    _sessions[threadId] = PracticeSessionSnapshot(
      sessionId: current.sessionId,
      planId: current.planId,
      threadId: threadId,
      sessionVersion: sessionVersion,
      matter: current.matter,
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
      answer: AgentMessage(
        id: 'answer-$sessionId-$completedTurns',
        role: AgentMessageRole.user,
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
    required this.threadId,
    required this.sessionId,
    required this.planId,
  });

  final String threadId;
  final String sessionId;
  final String planId;
}

const _progressScenarioId = 'scn_workplace_progress_risk_update';
const _hotelScenarioId = 'scn_daily_hotel_checkin_issue';

const _progressScenario = PreparationScenario(
  id: _progressScenarioId,
  type: 'WORKPLACE',
  model: 'PROGRESS_AND_RISK_UPDATE',
  name: '项目进度同步',
  summary: '向协作方同步进展、风险和下一步。',
  version: 1,
  status: 'active',
);

const _hotelScenario = PreparationScenario(
  id: _hotelScenarioId,
  type: 'DAILY',
  model: 'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
  name: '酒店入住与问题处理',
  summary: '办理入住并沟通房间问题。',
  version: 1,
  status: 'active',
);

const _progressRole = PreparationRole(
  id: 'role-project-stakeholder',
  scenarioId: _progressScenarioId,
  type: 'STAKEHOLDER',
  displayName: '项目协作方',
  responsibilities: '追问当前进展、主要风险和行动计划。',
  style: '直接、清晰。',
  focusAreas: <String>['progress', 'risk', 'next_steps'],
  version: 1,
);

const _hotelRole = PreparationRole(
  id: 'role-hotel-receptionist',
  scenarioId: _hotelScenarioId,
  type: 'RECEPTIONIST',
  displayName: '酒店前台',
  responsibilities: '核对预订并帮助处理房间问题。',
  style: '礼貌、专业。',
  focusAreas: <String>['check_in', 'issue_resolution'],
  version: 1,
);

const _progressDetail = PreparationScenarioDetail(
  scenario: _progressScenario,
  config: PreparationScenarioConfig(
    id: 'config-workplace-progress',
    scenarioId: _progressScenarioId,
    type: 'WORKPLACE',
    model: 'PROGRESS_AND_RISK_UPDATE',
    version: 1,
    jobTitle: null,
    jobDescription: null,
    prompt: PreparationScenarioPrompt(
      publicSceneBrief: '向协作方同步项目进度、风险和下一步。',
      practiceGoal: '在一轮双向确认中表达清楚。',
      userRole: '项目负责人',
      aiRole: '项目协作方',
      personaSummary: '关注事实、风险和行动。',
      focusAreas: <String>['progress', 'risk'],
      turnBlueprints: <String>['询问进度。', '追问风险与下一步。'],
      suggestedDurationSeconds: 600,
    ),
  ),
  options: <PreparationOption>[
    PreparationOption(
      id: 'option-workplace-progress-full',
      scenarioId: _progressScenarioId,
      type: PreparationOptionType.fullSimulation,
      displayName: '完整情景练习',
      version: 1,
    ),
    PreparationOption(
      id: 'option-workplace-progress-focus',
      scenarioId: _progressScenarioId,
      roleId: 'role-project-stakeholder',
      type: PreparationOptionType.focus,
      displayName: '风险表达专项',
      version: 1,
    ),
  ],
);

const _hotelDetail = PreparationScenarioDetail(
  scenario: _hotelScenario,
  config: PreparationScenarioConfig(
    id: 'config-hotel-checkin',
    scenarioId: _hotelScenarioId,
    type: 'DAILY',
    model: 'HOTEL_CHECKIN_AND_ISSUE_HANDLING',
    version: 1,
    jobTitle: null,
    jobDescription: null,
    prompt: PreparationScenarioPrompt(
      publicSceneBrief: '办理酒店入住并沟通一个房间问题。',
      practiceGoal: '礼貌提出需求并确认解决方案。',
      userRole: '住客',
      aiRole: '酒店前台',
      personaSummary: '专业并愿意协助。',
      focusAreas: <String>['check_in', 'issue_resolution'],
      turnBlueprints: <String>['核对预订。', '协商房间问题。'],
      suggestedDurationSeconds: 480,
    ),
  ),
  options: <PreparationOption>[
    PreparationOption(
      id: 'option-hotel-checkin-full',
      scenarioId: _hotelScenarioId,
      type: PreparationOptionType.fullSimulation,
      displayName: '完整情景练习',
      version: 1,
    ),
    PreparationOption(
      id: 'option-hotel-checkin-focus',
      scenarioId: _hotelScenarioId,
      roleId: 'role-hotel-receptionist',
      type: PreparationOptionType.focus,
      displayName: '问题处理专项',
      version: 1,
    ),
  ],
);
