import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';

void main() {
  test(
    'publishes input immediately and coalesces canonical stream deltas',
    () async {
      final client = _StreamingAgentClient();
      final controller = AgentController(
        client: client,
        clientIdFactory: (_) => 'stream-client-message',
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('你好'), isTrue);
      expect(controller.isBusy, isTrue);
      expect(controller.messages.last.text, isEmpty);
      expect(controller.messages.last.isStreaming, isTrue);

      client.events
        ..add(
          const AgentInputCommitted(
            runId: 'run-1',
            userMessage: AgentMessage(
              id: 'user-1',
              role: AgentMessageRole.user,
              text: '你好',
            ),
          ),
        )
        ..add(const AgentAssistantStarted(runId: 'run-1'))
        ..add(const AgentAssistantDelta(runId: 'run-1', delta: '你'))
        ..add(const AgentAssistantDelta(runId: 'run-1', delta: '好，**小花**。'));
    await Future<void>.delayed(const Duration(milliseconds: 100));

      expect(controller.messages.last.text, '你好，**小花**。');
      expect(controller.messages.last.isStreaming, isTrue);

      client.events.add(
        const AgentRunCompleted(
          runId: 'run-1',
          assistantMessageId: 'assistant-1',
        ),
      );
      await client.events.close();
      await Future<void>.delayed(Duration.zero);

      expect(controller.isBusy, isFalse);
      expect(controller.messages.map((message) => message.id), <String>[
        'user-1',
        'assistant-1',
      ]);
      expect(controller.messages.last.isStreaming, isFalse);
    },
  );
}

final class _StreamingAgentClient
    implements AgentClient, AgentStreamingTextClient {
  _StreamingAgentClient() : delegate = FakeAgentClient();

  final FakeAgentClient delegate;
  final StreamController<AgentTextStreamEvent> events =
      StreamController<AgentTextStreamEvent>();

  @override
  Stream<AgentTextStreamEvent> sendTextStream({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) => events.stream;

  @override
  Future<void> clearAccountState() => delegate.clearAccountState();

  @override
  Future<AgentThreadSnapshot> restoreThread() => delegate.restoreThread();

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) => delegate.startScene(
    threadId: threadId,
    scene: scene,
    clientOperationId: clientOperationId,
  );

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) => delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
  );

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) => delegate.transcribeTurn(
    threadId: threadId,
    turnNumber: turnNumber,
    clientTurnId: clientTurnId,
  );

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) => delegate.submitPracticeTurn(
    threadId: threadId,
    scene: scene,
    turnNumber: turnNumber,
    transcript: transcript,
    clientTurnId: clientTurnId,
  );

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) => delegate.createReview(
    threadId: threadId,
    scene: scene,
    clientReviewId: clientReviewId,
  );
}
