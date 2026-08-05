import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_meme_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/providers/agent/wire_agent_client.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/identity/auth_state.dart';

void main() {
  test(
    'publishes input immediately and coalesces canonical stream deltas',
    () async {
      final client = _StreamingAgentClient();
      final controller = ConversationController(
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

  test(
    'loads the trusted handoff immediately after stream completion',
    () async {
      final client = _StreamingHistoryAgentClient();
      final controller = ConversationController(
        client: client,
        clientIdFactory: (_) => 'stream-handoff-message',
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('帮我创建练习'), isTrue);
      client.authoritativeMessages = const <AgentMessage>[
        AgentMessage(
          id: 'user-handoff-1',
          role: AgentMessageRole.user,
          text: '帮我创建练习',
          sequence: 1,
        ),
        AgentMessage(
          id: 'assistant-handoff-1',
          role: AgentMessageRole.assistant,
          text: '练习方案已准备好。',
          sequence: 2,
          handoffs: <AgentHandoff>[_practiceHandoff],
        ),
      ];
      client.events
        ..add(
          const AgentInputCommitted(
            runId: 'run-handoff-1',
            userMessage: AgentMessage(
              id: 'user-handoff-1',
              role: AgentMessageRole.user,
              text: '帮我创建练习',
              sequence: 1,
            ),
          ),
        )
        ..add(const AgentAssistantStarted(runId: 'run-handoff-1'))
        ..add(
          const AgentAssistantDelta(runId: 'run-handoff-1', delta: '练习方案已准备好。'),
        )
        ..add(
          const AgentRunCompleted(
            runId: 'run-handoff-1',
            assistantMessageId: 'assistant-handoff-1',
          ),
        );
      await client.events.close();
      await Future<void>.delayed(Duration.zero);

      expect(controller.isBusy, isFalse);
      expect(controller.messages.last.id, 'assistant-handoff-1');
      expect(controller.messages.last.handoffs, <AgentHandoff>[
        _practiceHandoff,
      ]);
      expect(client.messagePageCalls, 1);
    },
  );

  test('loads Meme content after the authoritative stream refresh', () async {
    final client = _StreamingHistoryAgentClient();
    final memeClient = _RecordingMemeClient();
    final controller = ConversationController(
      client: client,
      messageMemeClient: memeClient,
      clientIdFactory: (_) => 'stream-meme-message',
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    expect(await controller.sendText('给我一个表情'), isTrue);
    client.authoritativeMessages = const <AgentMessage>[
      AgentMessage(
        id: 'user-meme-1',
        role: AgentMessageRole.user,
        text: '给我一个表情',
        sequence: 1,
      ),
      AgentMessage(
        id: 'assistant-meme-1',
        role: AgentMessageRole.assistant,
        text: '当然可以。',
        sequence: 2,
        memes: <AgentMessageMeme>[_authoritativeMeme],
      ),
    ];
    client.events
      ..add(
        const AgentInputCommitted(
          runId: 'run-meme-1',
          userMessage: AgentMessage(
            id: 'user-meme-1',
            role: AgentMessageRole.user,
            text: '给我一个表情',
            sequence: 1,
          ),
        ),
      )
      ..add(const AgentAssistantStarted(runId: 'run-meme-1'))
      ..add(const AgentAssistantDelta(runId: 'run-meme-1', delta: '当然可以。'))
      ..add(
        const AgentRunCompleted(
          runId: 'run-meme-1',
          assistantMessageId: 'assistant-meme-1',
        ),
      );
    await client.events.close();
    await memeClient.requested.future;
    await Future<void>.delayed(Duration.zero);

    expect(memeClient.calls, 1);
    expect(controller.messages.last.memes.single.bytes, <int>[1, 2, 3]);
  });

  test('retries only the authoritative handoff Message refresh', () async {
    final client = _StreamingHistoryAgentClient(messageFailuresRemaining: 1);
    final controller = ConversationController(
      client: client,
      clientIdFactory: (_) => 'stream-handoff-retry-message',
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    expect(await controller.sendText('帮我创建练习'), isTrue);
    client.authoritativeMessages = const <AgentMessage>[
      AgentMessage(
        id: 'user-handoff-retry-1',
        role: AgentMessageRole.user,
        text: '帮我创建练习',
        sequence: 1,
      ),
      AgentMessage(
        id: 'assistant-handoff-retry-1',
        role: AgentMessageRole.assistant,
        text: '练习方案已准备好。',
        sequence: 2,
        handoffs: <AgentHandoff>[_practiceHandoff],
      ),
    ];
    client.events
      ..add(
        const AgentInputCommitted(
          runId: 'run-handoff-retry-1',
          userMessage: AgentMessage(
            id: 'user-handoff-retry-1',
            role: AgentMessageRole.user,
            text: '帮我创建练习',
            sequence: 1,
          ),
        ),
      )
      ..add(const AgentAssistantStarted(runId: 'run-handoff-retry-1'))
      ..add(
        const AgentAssistantDelta(
          runId: 'run-handoff-retry-1',
          delta: '练习方案已准备好。',
        ),
      )
      ..add(
        const AgentRunCompleted(
          runId: 'run-handoff-retry-1',
          assistantMessageId: 'assistant-handoff-retry-1',
        ),
      );
    await client.events.close();
    await Future<void>.delayed(Duration.zero);

    expect(controller.canRetryThreadHistory, isTrue);
    expect(controller.messages.last.handoffs, isEmpty);
    expect(client.messagePageCalls, 1);

    await controller.retryThreadHistory();

    expect(controller.canRetryThreadHistory, isFalse);
    expect(controller.threadHistoryErrorMessage, isNull);
    expect(controller.messages.last.handoffs, <AgentHandoff>[_practiceHandoff]);
    expect(client.messagePageCalls, 2);
  });

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
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) =>
      delegate.listThreads(pageSize: pageSize, cursor: cursor);

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() =>
      delegate.getFocusedThread();

  @override
  Future<AgentThreadSummary> createThread() => delegate.createThread();

  @override
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) =>
      delegate.setFocusedThread(threadId: threadId);

  @override
  Future<void> clearFocusedThread() => delegate.clearFocusedThread();

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
  Future<AgentThreadSnapshot?> getFocusedThread() =>
      _delegate.getFocusedThread();

  @override
  Future<AgentThreadSummary> createThread() => _delegate.createThread();

  @override
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) =>
      _delegate.setFocusedThread(threadId: threadId);

  @override
  Future<void> clearFocusedThread() => _delegate.clearFocusedThread();

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

final class _RecordingMemeClient implements AgentMessageMemeClient {
  final Completer<void> requested = Completer<void>();
  int calls = 0;

  @override
  Future<Uint8List> getMemeContent({
    required String contentPath,
    required int expectedSizeBytes,
    required String expectedContentType,
  }) async {
    calls++;
    expect(contentPath, _authoritativeMeme.contentPath);
    expect(expectedSizeBytes, 3);
    expect(expectedContentType, 'image/jpeg');
    if (!requested.isCompleted) {
      requested.complete();
    }
    return Uint8List.fromList(<int>[1, 2, 3]);
  }
}

const _authoritativeMeme = AgentMessageMeme(
  id: '20000000-0000-4000-8000-000000000003',
  memeId: 'official-001:happy:01',
  category: 'happy',
  contentType: 'image/jpeg',
  sizeBytes: 3,
  width: 100,
  height: 80,
  contentPath:
      '/v1/agent-message-memes/20000000-0000-4000-8000-000000000003/content',
);

const _practiceHandoff = ConfirmPracticePlanHandoff(
  label: '确认并开始练习',
  practicePlanId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  planRevision: 2,
  target: 'Java 后端面试',
  sceneName: '项目经历深挖',
  practiceExperience: 'INTERVIEW',
  sceneCategory: 'INTERVIEW_PROFESSIONAL',
  practiceMode: 'FULL_SIMULATION',
  roles: <String>['技术面试官'],
  practiceScope: '完整模拟',
  suggestedDuration: Duration(minutes: 10),
  minEffectiveTurns: 3,
  maxEffectiveTurns: 5,
  executableStatus: 'ready',
  confirmationPrompt: '请确认是否按此方案开始练习。',
);
