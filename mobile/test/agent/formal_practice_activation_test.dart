import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';

void main() {
  test('new user activates a real Matter without legacy voice start', () async {
    final agent = _ActivationAgentClient(activeMatter: null);
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
    );
    final controller = AgentController(client: agent, practiceClient: practice);
    addTearDown(controller.dispose);
    await controller.initialize();

    final matter = await controller.activateMatterForScenario(
      threadId: _threadId,
      scene: _matter.scene,
      clientOperationId: 'matter-stable-operation',
    );

    expect(matter.id, _matterId);
    expect(controller.activeMatter?.id, _matterId);
    expect(agent.sceneStarts, 1);
    expect(practice.startCalls, 0);
  });

  test(
    'does not reuse a same-title Matter from another catalog scene',
    () async {
      const staleMatter = AgentMatter(
        id: 'matter-stale',
        scene: AgentScene(
          id: 'another-catalog-scene',
          title: 'Technical interview',
          description: 'A different catalog entry with the same title.',
        ),
      );
      final agent = _ActivationAgentClient(activeMatter: staleMatter);
      final controller = AgentController(client: agent);
      addTearDown(controller.dispose);
      await controller.initialize();

      final matter = await controller.activateMatterForScenario(
        threadId: _threadId,
        scene: _matter.scene,
        clientOperationId: 'matter-exact-catalog-match',
      );

      expect(agent.sceneStarts, 1);
      expect(matter.scene.id, _matter.scene.id);
      expect(matter.scene.title, _matter.scene.title);
    },
  );

  test('rejects a Matter response for a different catalog scene', () async {
    const mismatchedMatter = AgentMatter(
      id: 'matter-other',
      scene: AgentScene(
        id: 'other-scene',
        title: 'Other interview',
        description: 'Wrong catalog entry.',
      ),
    );
    final agent = _ActivationAgentClient(
      activeMatter: null,
      startSceneMatter: mismatchedMatter,
    );
    final controller = AgentController(client: agent);
    addTearDown(controller.dispose);
    await controller.initialize();

    await expectLater(
      controller.activateMatterForScenario(
        threadId: _threadId,
        scene: _matter.scene,
        clientOperationId: 'matter-reject-mismatch',
      ),
      throwsStateError,
    );
    expect(controller.activeMatter, isNull);
  });

  test(
    'activates only the exact formal Session with the frozen turn limit',
    () async {
      final practice = _ActivationPracticeClient(
        snapshot: _snapshot(turnLimit: 6),
      );
      final controller = AgentController(
        client: _ActivationAgentClient(),
        practiceClient: practice,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      await controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: _sessionId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      );

      expect(controller.practiceSessionId, _sessionId);
      expect(controller.turnLimit, 6);
      expect(controller.completedTurns, 0);
      expect(controller.hasActivePractice, isTrue);
      expect(practice.restoreCalls, 1);
      expect(practice.startCalls, 1);
      expect(practice.startKeys, [_voiceKey]);
    },
  );

  test(
    'rejects a voice restore whose limit differs from the formal snapshot',
    () async {
      final practice = _ActivationPracticeClient(
        snapshot: _snapshot(turnLimit: 3),
      );
      final controller = AgentController(
        client: _ActivationAgentClient(),
        practiceClient: practice,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      await expectLater(
        controller.activateCreatedPractice(
          threadId: _threadId,
          matterId: _matterId,
          sessionId: _sessionId,
          turnLimit: 6,
          clientOperationId: _voiceKey,
        ),
        throwsStateError,
      );
      expect(controller.practiceSessionId, isNull);
    },
  );

  test('retries formal activation with the same operation key', () async {
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
      failFirstStart: true,
    );
    final controller = AgentController(
      client: _ActivationAgentClient(),
      practiceClient: practice,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    await expectLater(
      controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: _sessionId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      ),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.network,
        ),
      ),
    );
    await controller.activateCreatedPractice(
      threadId: _threadId,
      matterId: _matterId,
      sessionId: _sessionId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );

    expect(practice.startKeys, [_voiceKey, _voiceKey]);
    expect(controller.practiceSessionId, _sessionId);
  });

  test('rejects activation of a different formal Session response', () async {
    final practice = _ActivationPracticeClient(
      snapshot: PracticeSessionSnapshot(
        sessionId: 'session-other',
        planId: 'plan-1',
        threadId: _threadId,
        matter: _matter,
        completedTurns: 0,
        turnLimit: 6,
        sessionCompleted: false,
        currentQuestion: const PracticeQuestion(
          id: 'question-other',
          sessionId: 'session-other',
          text: 'Wrong session.',
        ),
      ),
    );
    final controller = AgentController(
      client: _ActivationAgentClient(),
      practiceClient: practice,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    await expectLater(
      controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: _sessionId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      ),
      throwsStateError,
    );
    expect(controller.practiceSessionId, isNull);
  });

  test('logout fences a late formal activation response', () async {
    final startCompleter = Completer<PracticeStartResult>();
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
      startCompleter: startCompleter,
    );
    final controller = AgentController(
      client: _ActivationAgentClient(),
      practiceClient: practice,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    final activation = controller.activateCreatedPractice(
      threadId: _threadId,
      matterId: _matterId,
      sessionId: _sessionId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );
    await Future<void>.delayed(Duration.zero);
    expect(practice.startCalls, 1);

    await controller.clearPrivateState();
    startCompleter.complete(
      PracticeStartResult(snapshot: _snapshot(turnLimit: 6)),
    );

    await expectLater(
      activation,
      throwsA(isA<AgentClientOperationCancelled>()),
    );
    expect(controller.threadId, isNull);
    expect(controller.practiceSessionId, isNull);
    expect(practice.clearCalls, 1);
  });

  test('does not replace or relabel an active formal Session', () async {
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
    );
    final controller = AgentController(
      client: _ActivationAgentClient(),
      practiceClient: practice,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.activateCreatedPractice(
      threadId: _threadId,
      matterId: _matterId,
      sessionId: _sessionId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );

    await expectLater(
      controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: _sessionId,
        turnLimit: 3,
        clientOperationId: _voiceKey,
      ),
      throwsStateError,
    );
    await expectLater(
      controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: 'session-other',
        turnLimit: 6,
        clientOperationId: _voiceKey,
      ),
      throwsStateError,
    );
    expect(controller.practiceSessionId, _sessionId);
    expect(controller.turnLimit, 6);
    expect(practice.restoreCalls, 1);
    expect(practice.startCalls, 1);
  });

  test('ends the exact active formal Session with one stable intent', () async {
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
    );
    final controller = AgentController(
      client: _ActivationAgentClient(),
      practiceClient: practice,
      clientIdFactory: (scope) => '$scope-stable-operation',
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.activateCreatedPractice(
      threadId: _threadId,
      matterId: _matterId,
      sessionId: _sessionId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );

    expect(await controller.endActivePracticeEarly(), isTrue);

    expect(practice.endKeys, ['practice-end-stable-operation']);
    expect(practice.endVersions, [1]);
    expect(controller.hasActivePractice, isFalse);
    expect(controller.practiceSessionId, isNull);
    expect(controller.activeMatter, isNull);
  });

  test(
    'treats server sessionCompleted as authoritative before max turns',
    () async {
      final practice = _ActivationPracticeClient(
        snapshot: PracticeSessionSnapshot(
          sessionId: _sessionId,
          planId: 'plan-1',
          threadId: _threadId,
          matter: _matter,
          completedTurns: 4,
          turnLimit: 6,
          sessionCompleted: true,
          review: const AgentReview(
            id: 'review-1',
            title: 'Review',
            summary: 'Covered',
            strength: 'Clear evidence',
            nextFocus: 'Concise delivery',
          ),
        ),
      );
      final controller = AgentController(
        client: _ActivationAgentClient(),
        practiceClient: practice,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      await controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: _sessionId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      );

      expect(controller.completedTurns, 4);
      expect(controller.turnLimit, 6);
      expect(controller.hasActivePractice, isFalse);
      expect(controller.review?.id, 'review-1');
    },
  );
}

PracticeSessionSnapshot _snapshot({required int turnLimit}) {
  return PracticeSessionSnapshot(
    sessionId: _sessionId,
    planId: 'plan-1',
    threadId: _threadId,
    sessionVersion: 1,
    matter: _matter,
    completedTurns: 0,
    turnLimit: turnLimit,
    sessionCompleted: false,
    currentQuestion: const PracticeQuestion(
      id: 'question-1',
      sessionId: _sessionId,
      text: 'Tell me about your experience.',
    ),
  );
}

final class _ActivationPracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  _ActivationPracticeClient({
    required this.snapshot,
    this.failFirstStart = false,
    this.startCompleter,
  });

  final PracticeSessionSnapshot snapshot;
  final bool failFirstStart;
  final Completer<PracticeStartResult>? startCompleter;
  int restoreCalls = 0;
  int startCalls = 0;
  int clearCalls = 0;
  final startKeys = <String>[];
  final endKeys = <String>[];
  final endVersions = <int>[];

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    restoreCalls++;
    return restoreCalls == 1 ? null : snapshot;
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) {
    startCalls++;
    startKeys.add(clientOperationId);
    if (failFirstStart && startCalls == 1) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    }
    final pending = startCompleter;
    if (pending != null) {
      return pending.future;
    }
    return Future<PracticeStartResult>.value(
      PracticeStartResult(snapshot: snapshot),
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
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    endKeys.add(idempotencyKey);
    endVersions.add(expectedSessionVersion);
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.endedEarly,
      version: expectedSessionVersion + 1,
    );
  }
}

final class _ActivationAgentClient implements AgentClient {
  _ActivationAgentClient({this.activeMatter = _matter, this.startSceneMatter});

  final AgentMatter? activeMatter;
  final AgentMatter? startSceneMatter;
  int sceneStarts = 0;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<AgentThreadSnapshot> restoreThread() async {
    return AgentThreadSnapshot(threadId: _threadId, activeMatter: activeMatter);
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    sceneStarts++;
    return Future<AgentSceneStart>.value(
      AgentSceneStart(
        activeMatter:
            startSceneMatter ?? AgentMatter(id: _matterId, scene: scene),
        assistantMessage: AgentMessage(
          id: 'matter-$clientOperationId',
          role: AgentMessageRole.assistant,
          text: scene.title,
        ),
      ),
    );
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    throw UnimplementedError();
  }
}

const _threadId = 'thread-1';
const _matterId = 'matter-1';
const _sessionId = 'session-1';
const _voiceKey = 'formal-voice-activation-key';
const _matter = AgentMatter(
  id: _matterId,
  scene: AgentScene(
    id: 'technical-interview',
    title: 'Technical interview',
    description: 'Practice technical interview answers.',
  ),
);
