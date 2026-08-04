import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

import '../../support/scene_fixtures.dart';

void main() {
  test(
    'activates only the exact formal Session with the frozen turn limit',
    () async {
      final practice = _ActivationPracticeClient(
        snapshot: _snapshot(turnLimit: 6),
      );
      final controller = PracticeController(client: practice);
      addTearDown(controller.dispose);

      await controller.activateCreatedPractice(
        scene: _scene,
        sessionId: _sessionId,
        planId: _planId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      );

      expect(controller.practiceSessionId, _sessionId);
      expect(controller.turnLimit, 6);
      expect(controller.completedTurns, 0);
      expect(controller.hasActivePractice, isTrue);
      expect(practice.activationCalls, 1);
      expect(practice.activationKeys, [_voiceKey]);
    },
  );

  test(
    'rejects a voice restore whose limit differs from the formal snapshot',
    () async {
      final practice = _ActivationPracticeClient(
        snapshot: _snapshot(turnLimit: 3),
      );
      final controller = PracticeController(client: practice);
      addTearDown(controller.dispose);

      await expectLater(
        controller.activateCreatedPractice(
          scene: _scene,
          sessionId: _sessionId,
          planId: _planId,
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
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);

    await expectLater(
      controller.activateCreatedPractice(
        scene: _scene,
        sessionId: _sessionId,
        planId: _planId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      ),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.network,
        ),
      ),
    );
    await controller.activateCreatedPractice(
      scene: _scene,
      sessionId: _sessionId,
      planId: _planId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );

    expect(practice.activationKeys, [_voiceKey, _voiceKey]);
    expect(controller.practiceSessionId, _sessionId);
  });

  test('rejects activation of a different formal Session response', () async {
    final practice = _ActivationPracticeClient(
      snapshot: PracticeSessionSnapshot(
        sessionId: 'session-other',
        planId: _planId,
        sceneFamily: _scene.family,
        sceneModel: _scene.model,
        sessionVersion: 1,
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
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);

    await expectLater(
      controller.activateCreatedPractice(
        scene: _scene,
        sessionId: _sessionId,
        planId: _planId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      ),
      throwsStateError,
    );
    expect(controller.practiceSessionId, isNull);
  });

  test('logout fences a late formal activation response', () async {
    final activationCompleter = Completer<PracticeSessionSnapshot>();
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
      activationCompleter: activationCompleter,
    );
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);

    final activation = controller.activateCreatedPractice(
      scene: _scene,
      sessionId: _sessionId,
      planId: _planId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );
    await Future<void>.delayed(Duration.zero);
    expect(practice.activationCalls, 1);

    await controller.clearPrivateState();
    activationCompleter.complete(_snapshot(turnLimit: 6));

    await expectLater(
      activation,
      throwsA(isA<PracticeClientOperationCancelled>()),
    );
    expect(controller.practiceSessionId, isNull);
    expect(practice.clearCalls, 1);
  });

  test('does not replace or relabel an active formal Session', () async {
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
    );
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);
    await controller.activateCreatedPractice(
      scene: _scene,
      sessionId: _sessionId,
      planId: _planId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );

    await expectLater(
      controller.activateCreatedPractice(
        scene: _scene,
        sessionId: _sessionId,
        planId: _planId,
        turnLimit: 3,
        clientOperationId: _voiceKey,
      ),
      throwsStateError,
    );
    await expectLater(
      controller.activateCreatedPractice(
        scene: _scene,
        sessionId: 'session-other',
        planId: _planId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      ),
      throwsStateError,
    );
    expect(controller.practiceSessionId, _sessionId);
    expect(controller.turnLimit, 6);
    expect(practice.activationCalls, 1);
  });

  test('ends the exact active formal Session with one stable intent', () async {
    final practice = _ActivationPracticeClient(
      snapshot: _snapshot(turnLimit: 6),
    );
    final controller = PracticeController(
      client: practice,
      clientIdFactory: (scope) => '$scope-stable-operation',
    );
    addTearDown(controller.dispose);
    await controller.activateCreatedPractice(
      scene: _scene,
      sessionId: _sessionId,
      planId: _planId,
      turnLimit: 6,
      clientOperationId: _voiceKey,
    );

    expect(await controller.endActivePracticeEarly(), isTrue);

    expect(practice.endKeys, ['practice-end-stable-operation']);
    expect(practice.endVersions, [1]);
    expect(controller.hasActivePractice, isFalse);
    expect(controller.practiceSessionId, isNull);
  });

  test(
    'treats server sessionCompleted as authoritative before max turns',
    () async {
      final practice = _ActivationPracticeClient(
        snapshot: PracticeSessionSnapshot(
          sessionId: _sessionId,
          planId: _planId,
          sceneFamily: _scene.family,
          sceneModel: _scene.model,
          sessionVersion: 1,
          completedTurns: 4,
          turnLimit: 6,
          sessionCompleted: true,
        ),
      );
      final controller = PracticeController(client: practice);
      addTearDown(controller.dispose);

      await controller.activateCreatedPractice(
        scene: _scene,
        sessionId: _sessionId,
        planId: _planId,
        turnLimit: 6,
        clientOperationId: _voiceKey,
      );

      expect(controller.completedTurns, 4);
      expect(controller.turnLimit, 6);
      expect(controller.hasActivePractice, isFalse);
      expect(controller.recordingState, PracticeRecordingState.completed);
    },
  );
}

PracticeSessionSnapshot _snapshot({required int turnLimit}) {
  return PracticeSessionSnapshot(
    sessionId: _sessionId,
    planId: _planId,
    sceneFamily: _scene.family,
    sceneModel: _scene.model,
    sessionVersion: 1,
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
    this.activationCompleter,
  });

  final PracticeSessionSnapshot snapshot;
  final bool failFirstStart;
  final Completer<PracticeSessionSnapshot>? activationCompleter;
  int activationCalls = 0;
  int clearCalls = 0;
  final activationKeys = <String>[];
  final endKeys = <String>[];
  final endVersions = <int>[];

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async => snapshot;

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) {
    activationCalls++;
    activationKeys.add(clientOperationId);
    if (failFirstStart && activationCalls == 1) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        retryable: true,
      );
    }
    final pending = activationCompleter;
    if (pending != null) {
      return pending.future;
    }
    return Future<PracticeSessionSnapshot>.value(snapshot);
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

const _sessionId = 'session-1';
const _planId = 'plan-1';
const _voiceKey = 'formal-voice-activation-key';
final _scene = testScene(
  id: 'technical-interview',
  name: 'Technical interview',
  prompt: const ScenePrompt(
    publicSceneBrief: 'Practice technical interview answers.',
    practiceGoal: 'Complete the technical interview practice.',
    userRole: 'Candidate',
    aiRole: 'Interviewer',
    personaSummary: 'Professional and focused.',
    focusAreas: <String>['clarity'],
    turnBlueprints: <String>['Ask one technical interview question.'],
    suggestedDurationSeconds: 600,
  ),
);
