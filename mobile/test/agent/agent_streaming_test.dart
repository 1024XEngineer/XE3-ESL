import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/providers/agent/wire_agent_client.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/identity/auth_state.dart';

void main() {
  test(
    'publishes input immediately and coalesces canonical stream deltas',
    () async {
      final client = _StreamingAgentClient();
      final speechEvents = <String>[];
      final controller = ConversationController(
        client: client,
        clientIdFactory: (_) => 'stream-client-message',
        onAssistantStreamStarted: (messageId) async {
          speechEvents.add('start:$messageId');
        },
        onAssistantStreamDelta: (messageId, delta) {
          speechEvents.add('delta:$messageId:$delta');
        },
        onAssistantStreamCompleted: (messageId, message) {
          speechEvents.add('complete:$messageId:${message.id}:${message.text}');
        },
        onAssistantStreamFailed: (messageId) {
          speechEvents.add('fail:$messageId');
        },
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
        ..add(
          const AgentAssistantOutputStarted(
            runId: 'run-1',
            outputId: 'assistant-1',
          ),
        )
        ..add(
          const AgentAssistantOutputDelta(
            runId: 'run-1',
            outputId: 'assistant-1',
            sequence: 1,
            delta: '你',
          ),
        )
        ..add(
          const AgentAssistantOutputDelta(
            runId: 'run-1',
            outputId: 'assistant-1',
            sequence: 2,
            delta: '好，**小花**。',
          ),
        );
      await Future<void>.delayed(const Duration(milliseconds: 100));

      expect(controller.messages.last.text, '你好，**小花**。');
      expect(controller.messages.last.isStreaming, isTrue);

      client.events
        ..add(
          const AgentAssistantOutputCompleted(
            runId: 'run-1',
            outputId: 'assistant-1',
            text: '你好，**小花**。',
          ),
        )
        ..add(
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
      expect(speechEvents, <String>[
        'start:assistant-1',
        'delta:assistant-1:你',
        'delta:assistant-1:好，**小花**。',
        'complete:assistant-1:assistant-1:你好，**小花**。',
      ]);
    },
  );

  test('stops assistant speech when the text stream fails', () async {
    final client = _StreamingAgentClient();
    final speechEvents = <String>[];
    final controller = ConversationController(
      client: client,
      clientIdFactory: (_) => 'stream-failure-message',
      onAssistantStreamStarted: (messageId) async {
        speechEvents.add('start:$messageId');
      },
      onAssistantStreamDelta: (messageId, delta) {
        speechEvents.add('delta:$messageId:$delta');
      },
      onAssistantStreamFailed: (messageId) {
        speechEvents.add('fail:$messageId');
      },
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    expect(await controller.sendText('Please help me.'), isTrue);
    client.events
      ..add(
        const AgentAssistantOutputStarted(
          runId: 'run-failed',
          outputId: 'assistant-failed',
        ),
      )
      ..add(
        const AgentAssistantOutputDelta(
          runId: 'run-failed',
          outputId: 'assistant-failed',
          sequence: 1,
          delta: 'I can help',
        ),
      )
      ..add(
        const AgentRunFailed(
          runId: 'run-failed',
          kind: 'provider_unavailable',
          retryable: true,
        ),
      );
    await client.events.close();
    await Future<void>.delayed(Duration.zero);

    expect(speechEvents, <String>[
      'start:assistant-failed',
      'delta:assistant-failed:I can help',
      'fail:assistant-failed',
    ]);
    expect(controller.messages.last.hasFailed, isTrue);
  });

  test('stream retry reuses the committed user message', () async {
    final client = _StreamingAgentClient();
    final controller = ConversationController(
      client: client,
      clientIdFactory: (_) => 'stream-retry-message',
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    expect(await controller.sendText('Please retry this.'), isTrue);
    client.events
      ..add(
        const AgentInputCommitted(
          runId: 'run-retry-1',
          userMessage: AgentMessage(
            id: 'user-retry-1',
            role: AgentMessageRole.user,
            text: 'Please retry this.',
          ),
        ),
      )
      ..add(
        const AgentRunFailed(
          runId: 'run-retry-1',
          kind: 'provider_unavailable',
          retryable: true,
        ),
      );
    await client.events.close();
    await Future<void>.delayed(Duration.zero);

    expect(controller.canRetry, isTrue);
    expect(
      controller.messages.where(
        (message) => message.role == AgentMessageRole.user,
      ),
      hasLength(1),
    );

    await controller.retryLastOperation();

    expect(client.streamCalls, 2);
    expect(client.clientMessageIds, <String>[
      'stream-retry-message',
      'stream-retry-message',
    ]);
    expect(
      controller.messages.where(
        (message) => message.role == AgentMessageRole.user,
      ),
      hasLength(1),
    );

    client.events
      ..add(
        const AgentInputCommitted(
          runId: 'run-retry-2',
          userMessage: AgentMessage(
            id: 'user-retry-1',
            role: AgentMessageRole.user,
            text: 'Please retry this.',
          ),
        ),
      )
      ..add(
        const AgentAssistantOutputStarted(
          runId: 'run-retry-2',
          outputId: 'assistant-retry-2',
        ),
      )
      ..add(
        const AgentAssistantOutputDelta(
          runId: 'run-retry-2',
          outputId: 'assistant-retry-2',
          sequence: 1,
          delta: 'Recovered.',
        ),
      )
      ..add(
        const AgentAssistantOutputCompleted(
          runId: 'run-retry-2',
          outputId: 'assistant-retry-2',
          text: 'Recovered.',
        ),
      )
      ..add(
        const AgentRunCompleted(
          runId: 'run-retry-2',
          assistantMessageId: 'assistant-retry-2',
        ),
      );
    await client.events.close();
    await Future<void>.delayed(Duration.zero);

    expect(
      controller.messages.where(
        (message) => message.role == AgentMessageRole.user,
      ),
      hasLength(1),
    );
    expect(controller.messages.first.id, 'user-retry-1');
    expect(controller.messages.last.id, 'assistant-retry-2');
  });

  test(
    'stops committed assistant speech when the stream fails after completion',
    () async {
      final client = _StreamingAgentClient();
      final speechEvents = <String>[];
      final controller = ConversationController(
        client: client,
        clientIdFactory: (_) => 'stream-completed-failure-message',
        onAssistantStreamStarted: (messageId) async {
          speechEvents.add('start:$messageId');
        },
        onAssistantStreamDelta: (messageId, delta) {
          speechEvents.add('delta:$messageId:$delta');
        },
        onAssistantStreamCompleted: (messageId, message) {
          speechEvents.add('complete:$messageId:${message.id}');
        },
        onAssistantStreamFailed: (messageId) {
          speechEvents.add('fail:$messageId');
        },
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('Please continue.'), isTrue);
      client.events
        ..add(
          const AgentAssistantOutputStarted(
            runId: 'run-completed-failure',
            outputId: 'assistant-completed-failure',
          ),
        )
        ..add(
          const AgentAssistantOutputDelta(
            runId: 'run-completed-failure',
            outputId: 'assistant-completed-failure',
            sequence: 1,
            delta: 'The response is complete.',
          ),
        )
        ..add(
          const AgentAssistantOutputCompleted(
            runId: 'run-completed-failure',
            outputId: 'assistant-completed-failure',
            text: 'The response is complete.',
          ),
        )
        ..add(
          const AgentRunCompleted(
            runId: 'run-completed-failure',
            assistantMessageId: 'assistant-completed-failure',
          ),
        )
        ..addError(StateError('Stream failed after completion.'));
      await client.events.close();
      await Future<void>.delayed(Duration.zero);

      expect(speechEvents, <String>[
        'start:assistant-completed-failure',
        'delta:assistant-completed-failure:The response is complete.',
        'complete:assistant-completed-failure:assistant-completed-failure',
        'fail:assistant-completed-failure',
      ]);
      expect(controller.messages.last.id, 'assistant-completed-failure');
      expect(controller.messages.last.hasFailed, isTrue);
    },
  );

  test('speech presentation failure does not fail the text stream', () async {
    final client = _StreamingAgentClient();
    final controller = ConversationController(
      client: client,
      clientIdFactory: (_) => 'stream-speech-failure-message',
      onAssistantStreamStarted: (_) async {
        throw StateError('Audio output unavailable.');
      },
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    expect(await controller.sendText('Please continue.'), isTrue);
    client.events
      ..add(
        const AgentAssistantOutputStarted(
          runId: 'run-text-success',
          outputId: 'assistant-text-success',
        ),
      )
      ..add(
        const AgentAssistantOutputDelta(
          runId: 'run-text-success',
          outputId: 'assistant-text-success',
          sequence: 1,
          delta: 'The text still succeeds.',
        ),
      )
      ..add(
        const AgentAssistantOutputCompleted(
          runId: 'run-text-success',
          outputId: 'assistant-text-success',
          text: 'The text still succeeds.',
        ),
      )
      ..add(
        const AgentRunCompleted(
          runId: 'run-text-success',
          assistantMessageId: 'assistant-text-success',
        ),
      );
    await client.events.close();
    await Future<void>.delayed(Duration.zero);

    expect(controller.errorMessage, isNull);
    expect(controller.messages.last.id, 'assistant-text-success');
    expect(controller.messages.last.text, 'The text still succeeds.');
    expect(controller.messages.last.hasFailed, isFalse);
  });

  test(
    'loads the trusted client action immediately after stream completion',
    () async {
      final client = _StreamingHistoryAgentClient();
      final controller = ConversationController(
        client: client,
        clientIdFactory: (_) => 'stream-client-action-message',
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('帮我创建练习'), isTrue);
      client.authoritativeMessages = const <AgentMessage>[
        AgentMessage(
          id: 'user-client-action-1',
          role: AgentMessageRole.user,
          text: '帮我创建练习',
          sequence: 1,
        ),
        AgentMessage(
          id: 'assistant-client-action-1',
          role: AgentMessageRole.assistant,
          text: '练习方案已准备好。',
          sequence: 2,
          clientActions: <AgentClientAction>[_practiceClientAction],
        ),
      ];
      client.events
        ..add(
          const AgentInputCommitted(
            runId: 'run-client-action-1',
            userMessage: AgentMessage(
              id: 'user-client-action-1',
              role: AgentMessageRole.user,
              text: '帮我创建练习',
              sequence: 1,
            ),
          ),
        )
        ..add(
          const AgentAssistantOutputStarted(
            runId: 'run-client-action-1',
            outputId: 'assistant-client-action-1',
          ),
        )
        ..add(
          const AgentAssistantOutputDelta(
            runId: 'run-client-action-1',
            outputId: 'assistant-client-action-1',
            sequence: 1,
            delta: '练习方案已准备好。',
          ),
        )
        ..add(
          const AgentAssistantOutputCompleted(
            runId: 'run-client-action-1',
            outputId: 'assistant-client-action-1',
            text: '练习方案已准备好。',
          ),
        )
        ..add(
          const AgentRunCompleted(
            runId: 'run-client-action-1',
            assistantMessageId: 'assistant-client-action-1',
          ),
        );
      await client.events.close();
      await Future<void>.delayed(Duration.zero);

      expect(controller.isBusy, isFalse);
      expect(controller.messages.last.id, 'assistant-client-action-1');
      expect(controller.messages.last.clientActions, <AgentClientAction>[
        _practiceClientAction,
      ]);
      expect(client.messagePageCalls, 1);
    },
  );

  test(
    'retries only the authoritative client-action Message refresh',
    () async {
      final client = _StreamingHistoryAgentClient(messageFailuresRemaining: 1);
      final controller = ConversationController(
        client: client,
        clientIdFactory: (_) => 'stream-client-action-retry-message',
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('帮我创建练习'), isTrue);
      client.authoritativeMessages = const <AgentMessage>[
        AgentMessage(
          id: 'user-client-action-retry-1',
          role: AgentMessageRole.user,
          text: '帮我创建练习',
          sequence: 1,
        ),
        AgentMessage(
          id: 'assistant-client-action-retry-1',
          role: AgentMessageRole.assistant,
          text: '练习方案已准备好。',
          sequence: 2,
          clientActions: <AgentClientAction>[_practiceClientAction],
        ),
      ];
      client.events
        ..add(
          const AgentInputCommitted(
            runId: 'run-client-action-retry-1',
            userMessage: AgentMessage(
              id: 'user-client-action-retry-1',
              role: AgentMessageRole.user,
              text: '帮我创建练习',
              sequence: 1,
            ),
          ),
        )
        ..add(
          const AgentAssistantOutputStarted(
            runId: 'run-client-action-retry-1',
            outputId: 'assistant-client-action-retry-1',
          ),
        )
        ..add(
          const AgentAssistantOutputDelta(
            runId: 'run-client-action-retry-1',
            outputId: 'assistant-client-action-retry-1',
            sequence: 1,
            delta: '练习方案已准备好。',
          ),
        )
        ..add(
          const AgentAssistantOutputCompleted(
            runId: 'run-client-action-retry-1',
            outputId: 'assistant-client-action-retry-1',
            text: '练习方案已准备好。',
          ),
        )
        ..add(
          const AgentRunCompleted(
            runId: 'run-client-action-retry-1',
            assistantMessageId: 'assistant-client-action-retry-1',
          ),
        );
      await client.events.close();
      await Future<void>.delayed(Duration.zero);

      expect(controller.canRetryThreadHistory, isTrue);
      expect(controller.messages.last.clientActions, isEmpty);
      expect(client.messagePageCalls, 1);

      await controller.retryThreadHistory();

      expect(controller.canRetryThreadHistory, isFalse);
      expect(controller.threadHistoryErrorMessage, isNull);
      expect(controller.messages.last.clientActions, <AgentClientAction>[
        _practiceClientAction,
      ]);
      expect(client.messagePageCalls, 2);
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
          _sse('tool.started', {
            'run_id': retryRunId,
            'step_id': 'tool-step-1',
            'name': 'practice.preview.v1',
          }),
          _sse('tool.completed', {
            'run_id': retryRunId,
            'step_id': 'tool-step-1',
            'name': 'practice.preview.v1',
          }),
          _sse('assistant.output.started', {
            'run_id': retryRunId,
            'output_id': assistantMessageId,
          }),
          _sse('assistant.output.delta', {
            'run_id': retryRunId,
            'output_id': assistantMessageId,
            'sequence': 1,
            'delta': '你好，小花。',
          }),
          _sse('assistant.output.completed', {
            'run_id': retryRunId,
            'output_id': assistantMessageId,
            'text': '你好，小花。',
          }),
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
  StreamController<AgentTextStreamEvent> events =
      StreamController<AgentTextStreamEvent>();
  final List<String> clientMessageIds = <String>[];
  int streamCalls = 0;

  @override
  Stream<AgentTextStreamEvent> sendTextStream({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    if (streamCalls > 0) {
      events = StreamController<AgentTextStreamEvent>();
    }
    streamCalls++;
    clientMessageIds.add(clientMessageId);
    return events.stream;
  }

  @override
  Future<void> clearAccountState() => delegate.clearAccountState();

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) =>
      delegate.listThreads(pageSize: pageSize, cursor: cursor);

  @override
  Future<AgentThreadSnapshot> getThread({required String threadId}) =>
      delegate.getThread(threadId: threadId);

  @override
  Future<AgentThreadSummary> createThread() => delegate.createThread();

  @override
  Future<void> deleteThread({required String threadId}) =>
      delegate.deleteThread(threadId: threadId);

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) => delegate.listMessages(
    threadId: threadId,
    pageSize: pageSize,
    cursor: cursor,
  );

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) => delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
    imageAssetIds: imageAssetIds,
  );
}

final class _StreamingHistoryAgentClient
    implements AgentClient, AgentStreamingTextClient {
  _StreamingHistoryAgentClient({this.messageFailuresRemaining = 0});

  final FakeAgentClient _delegate = FakeAgentClient();
  final StreamController<AgentTextStreamEvent> events =
      StreamController<AgentTextStreamEvent>();
  List<AgentMessage> authoritativeMessages = const <AgentMessage>[];
  int messageFailuresRemaining;
  int messagePageCalls = 0;

  @override
  Stream<AgentTextStreamEvent> sendTextStream({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) => events.stream;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) =>
      _delegate.listThreads(pageSize: pageSize, cursor: cursor);

  @override
  Future<AgentThreadSnapshot> getThread({required String threadId}) =>
      _delegate.getThread(threadId: threadId);

  @override
  Future<AgentThreadSummary> createThread() => _delegate.createThread();

  @override
  Future<void> deleteThread({required String threadId}) =>
      _delegate.deleteThread(threadId: threadId);

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) async {
    messagePageCalls++;
    if (messageFailuresRemaining > 0) {
      messageFailuresRemaining--;
      throw StateError('Temporary Message refresh failure.');
    }
    return AgentMessagePage(messages: authoritativeMessages);
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) => _delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
    imageAssetIds: imageAssetIds,
  );
}

const _practiceClientAction = AgentClientAction(
  type: 'practice.plan.confirm.v1',
  payload: <String, Object?>{
    'practice_plan_id': 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  },
);
