import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/preparation/preparation_launch_client.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

void main() {
  test(
    'runs the typed launch chain and reuses every key after network failure',
    () async {
      final client = _LaunchClient(failFirstSession: true);
      final activations = <String>[];
      final matterKeys = <String>[];
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async {
              matterKeys.add(clientOperationId);
              return _context;
            },
        voiceActivator: ({required context, required bootstrap}) async {
          activations.add('${context.threadId}:${bootstrap.session.id}');
        },
        idFactory: (scope) => '$scope-stable-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      expect(await controller.start(_selection), isFalse);
      expect(controller.canRetry, isTrue);
      expect(controller.errorMessage, contains('进度已保留'));
      expect(client.calls, ['profile', 'snapshot', 'plan', 'session']);

      expect(await controller.retry(), isTrue);
      expect(
        client.calls,
        ['profile', 'snapshot', 'plan', 'session'] +
            ['profile', 'snapshot', 'plan', 'session'],
      );
      expect(client.profileKeys.toSet(), {'prep-profile-stable-key'});
      expect(client.snapshotKeys.toSet(), {'prep-snapshot-stable-key'});
      expect(client.planKeys.toSet(), {'practice-plan-stable-key'});
      expect(client.sessionKeys.toSet(), {'practice-session-stable-key'});
      expect(matterKeys, [
        'agent-matter-stable-key',
        'agent-matter-stable-key',
      ]);
      expect(activations, ['$_threadId:$_sessionId']);
      expect(controller.bootstrap?.session.id, _sessionId);
      expect(controller.canRetry, isFalse);
    },
  );

  test(
    'creates a real Matter when a new user has only an Agent Thread',
    () async {
      AgentPracticeContext? context;
      final client = _LaunchClient();
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => context,
        threadIdProvider: () => _threadId,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async {
              context = _context;
              return _context;
            },
        voiceActivator: ({required context, required bootstrap}) async {},
        idFactory: (scope) => '$scope-recovered-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      expect(await controller.start(_selection), isTrue);
      expect(controller.backgroundSummary, _background);
      expect(client.calls, ['profile', 'snapshot', 'plan', 'session']);
      expect(client.lastPlanInput?.selection, _selection);
    },
  );

  test(
    'fences a late response after the selected Agent Matter changes',
    () async {
      var context = _context;
      final profile = Completer<PreparationProfile>();
      final client = _LaunchClient(profileCompleter: profile);
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => context,
        threadIdProvider: () => context.threadId,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => context,
        voiceActivator: ({required context, required bootstrap}) async {},
        idFactory: (scope) => '$scope-context-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      final start = controller.start(_selection);
      context = const AgentPracticeContext(
        threadId: _threadId,
        matterId: 'matter-changed',
      );
      profile.complete(_profile);

      expect(await start, isFalse);
      expect(controller.stage, PreparationLaunchStage.context);
      expect(controller.errorMessage, contains('已变化'));
      expect(client.calls, isEmpty);
    },
  );

  test(
    'logout clears input and suppresses an old account completion',
    () async {
      final profile = Completer<PreparationProfile>();
      final client = _LaunchClient(profileCompleter: profile);
      var activated = false;
      final controller = PreparationLaunchController(
        client: client,
        contextProvider: () => _context,
        threadIdProvider: () => _threadId,
        matterActivator:
            ({
              required threadId,
              required selection,
              required clientOperationId,
            }) async => _context,
        voiceActivator: ({required context, required bootstrap}) async {
          activated = true;
        },
        idFactory: (scope) => '$scope-logout-key',
      );
      addTearDown(controller.dispose);
      controller.updateBackgroundSummary(_background);

      final start = controller.start(_selection);
      await controller.clearPrivateState();
      profile.complete(_profile);

      expect(await start, isFalse);
      expect(client.clearCalls, 1);
      expect(controller.backgroundSummary, isEmpty);
      expect(controller.bootstrap, isNull);
      expect(controller.errorMessage, isNull);
      expect(activated, isFalse);
    },
  );
}

final class _LaunchClient implements PreparationLaunchClient {
  _LaunchClient({this.failFirstSession = false, this.profileCompleter});

  final bool failFirstSession;
  final Completer<PreparationProfile>? profileCompleter;
  final calls = <String>[];
  final profileKeys = <String>[];
  final snapshotKeys = <String>[];
  final planKeys = <String>[];
  final sessionKeys = <String>[];
  CreatePreparationPlanInput? lastPlanInput;
  int clearCalls = 0;
  int _sessionCalls = 0;

  @override
  Future<PreparationProfile> createProfile({
    required CreatePreparationProfileInput input,
    required String idempotencyKey,
  }) async {
    calls.add('profile');
    profileKeys.add(idempotencyKey);
    expect(input.backgroundSummary, _background);
    return profileCompleter?.future ?? _profile;
  }

  @override
  Future<PreparationSnapshot> createSnapshot({
    required String profileId,
    required int sourceVersion,
    required String idempotencyKey,
  }) async {
    calls.add('snapshot');
    snapshotKeys.add(idempotencyKey);
    expect(profileId, _profileId);
    expect(sourceVersion, 1);
    return _snapshot;
  }

  @override
  Future<PreparationPracticePlan> createPlan({
    required CreatePreparationPlanInput input,
    required String idempotencyKey,
  }) async {
    calls.add('plan');
    planKeys.add(idempotencyKey);
    lastPlanInput = input;
    expect(input.preparationProfileId, _profileId);
    expect(input.preparationUserId, _userId);
    return _plan;
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required String planId,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    calls.add('session');
    sessionKeys.add(idempotencyKey);
    _sessionCalls++;
    expect(planId, _planId);
    expect(input.preparationProfileId, _profileId);
    expect(input.preparationProfileVersion, 1);
    expect(input.preparationUserId, _userId);
    expect(input.backgroundSummary, _background);
    if (failFirstSession && _sessionCalls == 1) {
      throw const PreparationLaunchException(
        kind: PreparationLaunchFailureKind.network,
        stage: PreparationLaunchStage.session,
        retryable: true,
      );
    }
    return _bootstrap;
  }

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }
}

const _threadId = 'thread-1';
const _matterId = 'matter-1';
const _userId = 'user-1';
const _profileId = 'profile-1';
const _snapshotId = 'preparation-snapshot-1';
const _planId = 'plan-1';
const _sessionId = 'session-1';
const _background = 'Backend engineer preparing a technical interview.';

const _context = AgentPracticeContext(threadId: _threadId, matterId: _matterId);

const _selection = PreparationLaunchSelection(
  scenarioDefinitionId: 'scenario-1',
  scenarioDefinitionVersion: 1,
  scenarioType: 'INTERVIEW',
  scenarioDisplayName: 'Technical interview',
  scenarioDescription: 'Backend engineer: technical interview practice',
  scenarioConfigId: 'config-1',
  scenarioConfigVersion: 1,
  roleDefinitionId: 'role-1',
  roleDefinitionVersion: 2,
  practiceOptionId: 'option-1',
  practiceOptionType: PreparationOptionType.focus,
  practiceOptionVersion: 1,
);

final _profile = PreparationProfile(
  id: _profileId,
  userId: _userId,
  backgroundSummary: _background,
  version: 1,
  updatedAt: DateTime.utc(2026, 7, 26),
);

final _snapshot = PreparationSnapshot(
  id: _snapshotId,
  sourceProfileId: _profileId,
  sourceVersion: 1,
  backgroundSnapshot: _background,
  createdAt: DateTime.utc(2026, 7, 26),
);

const _plan = PreparationPracticePlan(
  id: _planId,
  userId: _userId,
  context: _context,
  selection: _selection,
  preparationProfileId: _profileId,
  revision: 1,
  status: 'ready',
);

final _bootstrap = PreparationPracticeBootstrap(
  session: PreparationPracticeSession(
    id: _sessionId,
    planId: _planId,
    scenarioType: 'INTERVIEW',
    snapshotId: 'session-snapshot-1',
    status: 'starting',
    version: 1,
    createdAt: DateTime.utc(2026, 7, 26),
  ),
  preparationSnapshotId: _snapshotId,
  maxEffectiveTurns: 3,
);
