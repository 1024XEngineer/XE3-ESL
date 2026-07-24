import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';

void main() {
  test(
    'keeps one Thread through a scene and creates one Review after 3 turns',
    () async {
      final client = _CountingAgentClient();
      final controller = AgentController(client: client);

      await controller.initialize();
      final threadId = controller.threadId;
      await controller.selectScene(agentScenes.first);

      expect(controller.threadId, threadId);
      expect(controller.scene, same(agentScenes.first));
      expect(controller.messages.single.role, AgentMessageRole.assistant);

      for (var turn = 1; turn <= 3; turn++) {
        controller.startRecording();
        expect(controller.recordingState, PracticeRecordingState.recording);

        await controller.stopRecording();
        expect(
          controller.recordingState,
          PracticeRecordingState.awaitingConfirmation,
        );
        expect(controller.completedTurns, turn - 1);

        await controller.confirmTranscript();
        expect(controller.completedTurns, turn);
      }

      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(controller.review, isNotNull);
      expect(client.reviewRequests, 1);

      await controller.confirmTranscript();
      expect(client.reviewRequests, 1);
    },
  );

  test(
    'does not expose a late response after private state is cleared',
    () async {
      final client = _ControlledAgentClient();
      final controller = AgentController(client: client);
      await controller.initialize();

      final request = controller.sendText('private account message');
      await client.sendStarted.future;
      await controller.clearPrivateState();
      client.sendResult.complete(
        const AgentExchange(
          userMessage: AgentMessage(
            id: 'late-user',
            role: AgentMessageRole.user,
            text: 'private account message',
          ),
          assistantMessage: AgentMessage(
            id: 'late-assistant',
            role: AgentMessageRole.assistant,
            text: 'private response',
          ),
        ),
      );
      await request;

      expect(controller.threadId, isNull);
      expect(controller.messages, isEmpty);
      expect(controller.review, isNull);
    },
  );

  test('retries Review without submitting a fourth practice turn', () async {
    final client = _FailOnceReviewAgentClient();
    final controller = AgentController(client: client);

    await controller.initialize();
    await controller.selectScene(agentScenes.first);
    for (var turn = 1; turn <= 3; turn++) {
      controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }

    expect(controller.completedTurns, 3);
    expect(controller.review, isNull);
    expect(controller.recordingState, PracticeRecordingState.reviewFailed);
    expect(client.submittedTurns, 3);
    expect(client.reviewRequests, 1);

    await controller.retryReview();

    expect(controller.completedTurns, 3);
    expect(controller.review, isNotNull);
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(client.submittedTurns, 3);
    expect(client.reviewRequests, 2);
  });
}

final class _CountingAgentClient implements AgentClient {
  final FakeAgentClient _delegate = FakeAgentClient();
  int reviewRequests = 0;

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
  }) {
    reviewRequests++;
    return _delegate.createReview(threadId: threadId, scene: scene);
  }

  @override
  Future<AgentThreadSnapshot> restoreThread() => _delegate.restoreThread();

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
  }) {
    return _delegate.sendText(threadId: threadId, text: text);
  }

  @override
  Future<AgentMessage> startScene({
    required String threadId,
    required AgentScene scene,
  }) {
    return _delegate.startScene(threadId: threadId, scene: scene);
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
  }) {
    return _delegate.submitPracticeTurn(
      threadId: threadId,
      scene: scene,
      turnNumber: turnNumber,
      transcript: transcript,
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
  }) {
    return _delegate.transcribeTurn(threadId: threadId, turnNumber: turnNumber);
  }
}

final class _FailOnceReviewAgentClient implements AgentClient {
  final FakeAgentClient _delegate = FakeAgentClient();
  int submittedTurns = 0;
  int reviewRequests = 0;

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
  }) {
    reviewRequests++;
    if (reviewRequests == 1) {
      throw StateError('temporary review failure');
    }
    return _delegate.createReview(threadId: threadId, scene: scene);
  }

  @override
  Future<AgentThreadSnapshot> restoreThread() => _delegate.restoreThread();

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
  }) {
    return _delegate.sendText(threadId: threadId, text: text);
  }

  @override
  Future<AgentMessage> startScene({
    required String threadId,
    required AgentScene scene,
  }) {
    return _delegate.startScene(threadId: threadId, scene: scene);
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
  }) {
    submittedTurns++;
    return _delegate.submitPracticeTurn(
      threadId: threadId,
      scene: scene,
      turnNumber: turnNumber,
      transcript: transcript,
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
  }) {
    return _delegate.transcribeTurn(threadId: threadId, turnNumber: turnNumber);
  }
}

final class _ControlledAgentClient implements AgentClient {
  final sendStarted = Completer<void>();
  final sendResult = Completer<AgentExchange>();

  @override
  Future<AgentThreadSnapshot> restoreThread() async {
    return const AgentThreadSnapshot(threadId: 'controlled-thread');
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
  }) {
    sendStarted.complete();
    return sendResult.future;
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentMessage> startScene({
    required String threadId,
    required AgentScene scene,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
  }) {
    throw UnimplementedError();
  }
}
