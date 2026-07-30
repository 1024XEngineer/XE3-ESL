import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/identity/auth_state.dart';

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

  test('sends Unicode stream input as UTF-8 and preserves retry', () async {
    const threadId = '11111111-1111-4111-8111-111111111111';
    const runId = '22222222-2222-4222-8222-222222222222';
    const retryRunId = '33333333-3333-4333-8333-333333333333';
    const userMessageId = '44444444-4444-4444-8444-444444444444';
    const assistantMessageId = '55555555-5555-4555-8555-555555555555';
    const clientMessageId = 'unicode-message';
    const content = '你好，小花';
    const createdAt = '2026-07-30T03:00:00Z';
    final requests = <({String path, Map<String, dynamic> body})>[];
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final subscription = server.listen((request) async {
      final bodyBytes = await request.fold<List<int>>(
        <int>[],
        (buffer, chunk) => buffer..addAll(chunk),
      );
      requests.add((
        path: request.uri.path,
        body: jsonDecode(utf8.decode(bodyBytes)) as Map<String, dynamic>,
      ));
      final isRetry = requests.length == 2;
      final effectiveRunId = isRetry ? retryRunId : runId;
      final events = <String>[
        _sse('input.committed', {
          'run_id': effectiveRunId,
          'message': <String, Object?>{
            'message_id': userMessageId,
            'thread_id': threadId,
            'sequence': 1,
            'role': 'user',
            'client_message_id': clientMessageId,
            'content': content,
            'created_at': createdAt,
          },
        }),
        if (!isRetry)
          _sse('run.failed', {
            'run_id': runId,
            'kind': 'provider_unavailable',
            'retryable': true,
          })
        else ...[
          _sse('assistant.started', {'run_id': retryRunId}),
          _sse('assistant.delta', {'run_id': retryRunId, 'delta': '你好，小花。'}),
          _sse('run.completed', {
            'run': <String, Object?>{
              'run_id': retryRunId,
              'assistant_message_id': assistantMessageId,
            },
          }),
        ],
      ];
      request.response
        ..statusCode = HttpStatus.ok
        ..headers.set(
          HttpHeaders.contentTypeHeader,
          'text/event-stream; charset=utf-8',
        )
        ..add(utf8.encode(events.join()));
      await request.response.close();
    });
    addTearDown(() async {
      await subscription.cancel();
      await server.close(force: true);
    });
    const credential = AuthSessionCredential(
      sessionToken: 'sess_unicode',
      generation: 1,
    );
    final client = WireAgentClient(
      baseUri: Uri.parse('http://127.0.0.1:${server.port}'),
      credentialProvider: () => credential,
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
    );
    addTearDown(client.clearAccountState);

    final firstEvents = await client
        .sendTextStream(
          threadId: threadId,
          text: content,
          clientMessageId: clientMessageId,
        )
        .toList();
    final retryEvents = await client
        .sendTextStream(
          threadId: threadId,
          text: content,
          clientMessageId: clientMessageId,
        )
        .toList();

    expect(firstEvents.last, isA<AgentRunFailed>());
    expect(retryEvents.last, isA<AgentRunCompleted>());
    expect(requests.map((request) => request.path), [
      '/v1/agent-threads/$threadId/runs/stream',
      '/v1/agent-runs/$runId/retries/stream',
    ]);
    expect(requests[0].body, {
      'client_message_id': clientMessageId,
      'content': content,
    });
    expect(requests[1].body, {'client_retry_id': 'retry:$runId'});
  });
}

String _sse(String event, Map<String, Object?> data) {
  return 'event: $event\ndata: ${jsonEncode(data)}\n\n';
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
