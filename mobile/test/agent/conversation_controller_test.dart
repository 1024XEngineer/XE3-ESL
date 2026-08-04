import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'clears UI before awaiting client cleanup and discards a late response',
    () async {
      final client = _ControlledCleanupAgentClient();
      final controller = ConversationController(client: client);
      await controller.initialize();

      final request = controller.sendText('private account message');
      await client.sendStarted.future;
      final cleanup = controller.clearPrivateState();
      await client.cleanupStarted.future;

      expect(client.cleanupCalls, 1);
      expect(controller.threadId, isNull);
      expect(controller.messages, isEmpty);

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

      var cleanupCompleted = false;
      cleanup.whenComplete(() => cleanupCompleted = true);
      await Future<void>.delayed(Duration.zero);
      expect(cleanupCompleted, isFalse);
      expect(controller.messages, isEmpty);

      client.cleanupResult.complete();
      await cleanup;
      expect(cleanupCompleted, isTrue);
    },
  );

  test('retries text with one stable client Message identity', () async {
    final client = _FailOnceTextAgentClient();
    final controller = ConversationController(client: client);
    await controller.initialize();

    await controller.sendText('retry this message');

    expect(controller.messages, isEmpty);
    expect(controller.canRetry, isTrue);
    expect(client.messageClientIds, hasLength(1));

    await controller.retryLastOperation();

    expect(controller.messages, hasLength(2));
    expect(client.messageClientIds, hasLength(2));
    expect(client.messageClientIds.toSet(), hasLength(1));
    expect(controller.canRetry, isFalse);
  });

  test('resubmits retained failed text with its original identity', () async {
    final client = _FailOnceTextAgentClient();
    final controller = ConversationController(client: client);
    await controller.initialize();

    expect(await controller.sendText('retry from the composer'), isFalse);
    expect(await controller.sendText('retry from the composer'), isTrue);

    expect(client.messageClientIds, hasLength(2));
    expect(client.messageClientIds.toSet(), hasLength(1));
    expect(controller.messages, hasLength(2));
    expect(controller.canRetry, isFalse);
  });

  test('restores a failed text operation with its server identity', () async {
    final client = _SnapshotAgentClient(
      const AgentThreadSnapshot(
        threadId: 'thread_restored_text',
        textRecovery: AgentTextRecovery(
          text: 'retry the restored text',
          clientMessageId: 'message_restored_stable',
          failureKind: 'timeout',
          retryable: true,
        ),
        messages: <AgentMessage>[
          AgentMessage(
            id: 'message_restored_user',
            role: AgentMessageRole.user,
            text: 'retry the restored text',
          ),
        ],
      ),
    );
    final controller = ConversationController(client: client);

    await controller.initialize();

    expect(controller.canRetry, isTrue);
    expect(controller.messages, hasLength(1));
    expect(controller.errorMessage, contains('继续重试'));

    await controller.retryLastOperation();

    expect(client.messageClientIds, <String>['message_restored_stable']);
    expect(controller.messages, hasLength(2));
    expect(controller.canRetry, isFalse);
  });

  test('restore exposes an executable operation-specific retry', () async {
    final client = _FailOnceRestoreAgentClient();
    final controller = ConversationController(client: client);

    await controller.initialize();

    expect(controller.threadId, isNull);
    expect(controller.canRetry, isTrue);
    await controller.retryLastOperation();
    expect(controller.threadId, isNotNull);
    expect(controller.canRetry, isFalse);
  });

  test(
    'applying an active Goal leaves Thread message history authoritative',
    () async {
      final client = _SnapshotAgentClient(
        AgentThreadSnapshot(
          threadId: 'thread_history',
          messages: const <AgentMessage>[
            AgentMessage(
              id: 'existing',
              role: AgentMessageRole.user,
              text: 'Existing Thread message',
            ),
          ],
        ),
      );
      final controller = ConversationController(client: client);
      await controller.initialize();

      controller.applyActiveGoal(
        threadId: 'thread_history',
        goalId: 'goal_history',
      );

      expect(controller.threadId, 'thread_history');
      expect(controller.activeGoalId, 'goal_history');
      expect(controller.messages, hasLength(1));
      expect(controller.messages.single.id, 'existing');
    },
  );

  test(
    'Fake client cancels old account work and reuses stable write IDs',
    () async {
      final client = FakeAgentClient(delay: const Duration(milliseconds: 20));
      final staleRestore = client.getFocusedThread();
      final staleCancellation = expectLater(
        staleRestore,
        throwsA(isA<AgentClientOperationCancelled>()),
      );
      var cleanupCompleted = false;

      final cleanup = client.clearAccountState();
      cleanup.whenComplete(() => cleanupCompleted = true);
      await Future<void>.delayed(Duration.zero);
      expect(cleanupCompleted, isFalse);

      await staleCancellation;
      await cleanup;
      expect(cleanupCompleted, isTrue);

      final snapshot = (await client.getFocusedThread())!;
      final first = await client.sendText(
        threadId: snapshot.threadId,
        text: 'same logical message',
        clientMessageId: 'message_stable',
      );
      final retried = await client.sendText(
        threadId: snapshot.threadId,
        text: 'same logical message',
        clientMessageId: 'message_stable',
      );

      expect(retried, same(first));
      await client.clearAccountState();
      final nextAccount = (await client.getFocusedThread())!;
      expect(nextAccount.threadId, isNot(snapshot.threadId));
    },
  );

  test(
    'restores focused Thread and continues bounded Thread and Message pages',
    () async {
      final client = _HistoryAgentClient();
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);

      await controller.initialize();

      expect(controller.threadId, _historyThreadOne);
      expect(controller.threads.map((thread) => thread.id), [
        _historyThreadOne,
      ]);
      expect(controller.hasMoreThreads, isTrue);
      expect(controller.hasEarlierMessages, isTrue);

      await controller.loadMoreThreads();
      await controller.loadEarlierMessages();

      expect(controller.threads.map((thread) => thread.id), [
        _historyThreadOne,
        _historyThreadTwo,
      ]);
      expect(controller.hasMoreThreads, isFalse);
      expect(controller.messages.map((message) => message.id), [
        'message_older',
        'message_current_$_historyThreadOne',
      ]);

      expect(await controller.createThread(), isTrue);
      expect(controller.threadId, _historyThreadThree);
      expect(client.focusRequests.last, _historyThreadThree);
      expect(controller.currentThreadSummary?.id, _historyThreadThree);
    },
  );

  test(
    'first text materializes an absent focused Thread exactly once',
    () async {
      final client = _HistoryAgentClient(
        startsWithoutFocus: true,
        textExchange: const AgentExchange(
          userMessage: AgentMessage(
            id: 'message_first_user',
            role: AgentMessageRole.user,
            text: 'first message',
          ),
          assistantMessage: AgentMessage(
            id: 'message_first_assistant',
            role: AgentMessageRole.assistant,
            text: 'first reply',
          ),
        ),
      );
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);

      await controller.initialize();

      expect(controller.threadId, isNull);
      expect(controller.messages, isEmpty);
      expect(controller.threads.single.id, _historyThreadOne);

      expect(await controller.sendText('first message'), isTrue);
      expect(controller.threadId, _historyThreadThree);
      expect(client.createCalls, 1);
      expect(client.focusRequests, <String>[_historyThreadThree]);
      expect(
        controller.messages.map((message) => message.id),
        containsAllInOrder(<String>[
          'message_first_user',
          'message_first_assistant',
        ]),
      );
    },
  );

  test(
    'lazy text retries the created Thread focus without another POST',
    () async {
      final client = _HistoryAgentClient(
        startsWithoutFocus: true,
        focusFailuresRemaining: 1,
        textExchange: const AgentExchange(
          userMessage: AgentMessage(
            id: 'message_recovered_user',
            role: AgentMessageRole.user,
            text: 'recover this message',
          ),
          assistantMessage: AgentMessage(
            id: 'message_recovered_assistant',
            role: AgentMessageRole.assistant,
            text: 'recovered reply',
          ),
        ),
      );
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('recover this message'), isFalse);
      expect(controller.threadId, isNull);
      expect(controller.canRetryThreadHistory, isTrue);
      expect(controller.threadHistoryErrorMessage, contains('不会重复创建'));
      expect(client.createCalls, 1);
      expect(client.focusRequests, <String>[_historyThreadThree]);

      expect(await controller.sendText('recover this message'), isTrue);

      expect(controller.threadId, _historyThreadThree);
      expect(controller.draftThreadRecoveryGeneration, 1);
      expect(client.createCalls, 1);
      expect(client.focusRequests, <String>[
        _historyThreadThree,
        _historyThreadThree,
      ]);
      expect(
        controller.messages.map((message) => message.id),
        containsAllInOrder(<String>[
          'message_recovered_user',
          'message_recovered_assistant',
        ]),
      );
    },
  );

  test(
    'inline Thread recovery focuses the created draft without another POST',
    () async {
      final client = _HistoryAgentClient(
        startsWithoutFocus: true,
        focusFailuresRemaining: 1,
      );
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.sendText('keep this draft'), isFalse);
      expect(controller.draftThreadRecoveryGeneration, 0);

      await controller.retryThreadHistory();

      expect(controller.threadId, _historyThreadThree);
      expect(controller.draftThreadRecoveryGeneration, 1);
      expect(client.createCalls, 1);
      expect(client.focusRequests, <String>[
        _historyThreadThree,
        _historyThreadThree,
      ]);
    },
  );

  test(
    'account cleanup fences Thread intents that are waiting for initialization',
    () async {
      for (final operation in <String>['create', 'select', 'clear']) {
        final initializationGate = Completer<void>();
        final client = _HistoryAgentClient(
          initializationGate: initializationGate,
        );
        final controller = ConversationController(client: client);

        final pending = switch (operation) {
          'create' => controller.createThread(),
          'select' => controller.selectThread(_historyThreadTwo),
          'clear' => controller.clearFocusedThread().then<Object?>((_) => null),
          _ => throw StateError('Unknown test operation.'),
        };
        await client.initialListStarted.future;

        await controller.clearPrivateState();
        initializationGate.complete();
        final result = await pending;

        if (operation != 'clear') {
          expect(result, isFalse, reason: operation);
        }
        expect(client.createCalls, 0, reason: operation);
        expect(client.focusRequests, isEmpty, reason: operation);
        expect(client.clearFocusedCalls, 0, reason: operation);
        expect(controller.threadId, isNull, reason: operation);
        controller.dispose();
      }
    },
  );

  test('rejects a concurrent Thread transition before a second POST', () async {
    final createGate = Completer<void>();
    final client = _HistoryAgentClient(createGate: createGate);
    final controller = ConversationController(client: client);
    addTearDown(controller.dispose);
    await controller.initialize();

    final firstCreate = controller.createThread();
    await client.createStarted.future;

    expect(controller.isThreadTransitionInFlight, isTrue);
    expect(await controller.createThread(), isFalse);
    expect(client.createCalls, 1);

    createGate.complete();
    expect(await firstCreate, isTrue);
    expect(client.createCalls, 1);
    expect(client.focusRequests, <String>[_historyThreadThree]);
    expect(controller.isThreadTransitionInFlight, isFalse);
  });

  test(
    'exposes ambiguous Thread creation as an explicit recovery operation',
    () async {
      final client = _HistoryAgentClient(ambiguousCreateFailuresRemaining: 1);
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.createThread(), isFalse);

      expect(controller.canRetryThreadHistory, isTrue);
      expect(controller.threadHistoryErrorMessage, contains('系统不会重复创建'));
      expect(controller.threadId, _historyThreadOne);

      await controller.retryThreadHistory();

      expect(controller.canRetryThreadHistory, isFalse);
      expect(controller.threadHistoryErrorMessage, isNull);
      expect(controller.threadId, _historyThreadThree);
      expect(client.createCalls, 2);
      expect(client.focusRequests, <String>[_historyThreadThree]);
    },
  );

  test(
    'opening history does not treat an ambiguous creation as draft recovery',
    () async {
      final client = _HistoryAgentClient(
        startsWithoutFocus: true,
        ambiguousCreateFailuresRemaining: 1,
      );
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.createThread(), isFalse);
      expect(controller.canRetryThreadHistory, isTrue);
      expect(controller.draftThreadRecoveryGeneration, 0);

      expect(await controller.selectThread(_historyThreadOne), isTrue);

      expect(controller.threadId, _historyThreadOne);
      expect(controller.draftThreadRecoveryGeneration, 0);
    },
  );

  test('surfaces a definite lazy Thread creation failure', () async {
    final client = _HistoryAgentClient(
      startsWithoutFocus: true,
      createFailuresRemaining: 1,
    );
    final controller = ConversationController(client: client);
    addTearDown(controller.dispose);
    await controller.initialize();

    expect(await controller.sendText('show this failure'), isFalse);

    expect(controller.threadId, isNull);
    expect(controller.canRetryThreadHistory, isFalse);
    expect(controller.threadHistoryErrorMessage, '暂时无法创建新对话，请稍后再试。');
  });

  test(
    'retains a created Thread when focus fails and retries PUT without POST',
    () async {
      final client = _HistoryAgentClient(focusFailuresRemaining: 1);
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(await controller.createThread(), isFalse);

      expect(client.createCalls, 1);
      expect(controller.threadId, _historyThreadOne);
      expect(controller.threads.first.id, _historyThreadThree);
      expect(controller.threadHistoryErrorMessage, isNotNull);

      expect(await controller.selectThread(_historyThreadThree), isTrue);

      expect(client.createCalls, 1);
      expect(client.focusRequests, [_historyThreadThree, _historyThreadThree]);
      expect(controller.threadId, _historyThreadThree);
    },
  );

  test(
    'keeps sent Messages while authoritative Thread ordering is retried',
    () async {
      final updatedSummaryOne = AgentThreadSummary(
        id: _historyThreadOne,
        createdAt: _historySummaryOne.createdAt,
        updatedAt: DateTime.utc(2026, 7, 25, 12),
      );
      final client = _HistoryAgentClient(
        initialThreadPage: AgentThreadPage(
          threads: <AgentThreadSummary>[
            _historySummaryThree,
            _historySummaryOne,
          ],
          focusedThreadId: _historyThreadOne,
        ),
        refreshedThreadPage: AgentThreadPage(
          threads: <AgentThreadSummary>[
            updatedSummaryOne,
            _historySummaryThree,
          ],
          focusedThreadId: _historyThreadOne,
        ),
        threadRefreshFailuresRemaining: 1,
        textExchange: const AgentExchange(
          userMessage: AgentMessage(
            id: 'message_sent_user',
            role: AgentMessageRole.user,
            text: 'sent text',
          ),
          assistantMessage: AgentMessage(
            id: 'message_sent_assistant',
            role: AgentMessageRole.assistant,
            text: 'sent reply',
          ),
        ),
      );
      final controller = ConversationController(client: client);
      addTearDown(controller.dispose);
      await controller.initialize();

      expect(controller.threads.first.id, _historyThreadThree);
      expect(await controller.sendText('sent text'), isTrue);

      expect(
        controller.messages.map((message) => message.id),
        containsAll(<String>['message_sent_user', 'message_sent_assistant']),
      );
      expect(controller.threads.first.id, _historyThreadThree);
      expect(controller.canRetryThreadHistory, isTrue);
      expect(controller.threadHistoryErrorMessage, contains('消息已发送'));

      await controller.retryThreadHistory();

      expect(controller.canRetryThreadHistory, isFalse);
      expect(controller.threadHistoryErrorMessage, isNull);
      expect(controller.threads.map((thread) => thread.id), [
        _historyThreadOne,
        _historyThreadThree,
      ]);
      expect(
        controller.currentThreadSummary?.updatedAt,
        updatedSummaryOne.updatedAt,
      );
      expect(client.firstPageCalls, 3);
    },
  );

  test('rejects overlapping and out-of-bound Thread pages', () async {
    final invalidPages = <AgentThreadPage>[
      AgentThreadPage(threads: <AgentThreadSummary>[_historySummaryOne]),
      AgentThreadPage(
        threads: <AgentThreadSummary>[
          AgentThreadSummary(
            id: 'thread_newer_than_boundary',
            createdAt: DateTime.utc(2026, 7, 25, 8),
            updatedAt: DateTime.utc(2026, 7, 25, 11),
          ),
        ],
      ),
    ];

    for (final invalidPage in invalidPages) {
      final client = _HistoryAgentClient(laterThreadPage: invalidPage);
      final controller = ConversationController(client: client);
      await controller.initialize();

      await controller.loadMoreThreads();

      expect(controller.threads.map((thread) => thread.id), [
        _historyThreadOne,
      ]);
      expect(controller.hasMoreThreads, isTrue);
      expect(controller.threadHistoryErrorMessage, isNotNull);
      controller.dispose();
    }
  });

  test(
    'rejects unsafe earlier Message pages without changing history',
    () async {
      final invalidPages = <AgentMessagePage>[
        const AgentMessagePage(
          messages: <AgentMessage>[
            AgentMessage(
              id: 'message_without_sequence',
              role: AgentMessageRole.user,
              text: 'missing sequence',
            ),
          ],
        ),
        const AgentMessagePage(
          messages: <AgentMessage>[
            AgentMessage(
              id: 'message_current_$_historyThreadOne',
              role: AgentMessageRole.assistant,
              text: 'overlap',
              sequence: 1,
            ),
          ],
        ),
        const AgentMessagePage(
          messages: <AgentMessage>[
            AgentMessage(
              id: 'message_not_older',
              role: AgentMessageRole.user,
              text: 'not older',
              sequence: 2,
            ),
          ],
        ),
        const AgentMessagePage(
          messages: <AgentMessage>[
            AgentMessage(
              id: 'message_out_of_order_2',
              role: AgentMessageRole.user,
              text: 'second',
              sequence: 2,
            ),
            AgentMessage(
              id: 'message_out_of_order_1',
              role: AgentMessageRole.user,
              text: 'first',
              sequence: 1,
            ),
          ],
        ),
      ];

      for (final invalidPage in invalidPages) {
        final client = _HistoryAgentClient(olderMessagePage: invalidPage);
        final controller = ConversationController(client: client);
        await controller.initialize();

        await controller.loadEarlierMessages();

        expect(
          controller.messages.single.id,
          'message_current_$_historyThreadOne',
        );
        expect(controller.hasEarlierMessages, isTrue);
        expect(controller.errorMessage, isNotNull);
        controller.dispose();
      }
    },
  );

  test('late text response cannot pollute a newly selected Thread', () async {
    final client = _HistoryAgentClient(controlOldSend: true);
    final controller = ConversationController(client: client);
    addTearDown(controller.dispose);
    await controller.initialize();

    final staleSend = controller.sendText('old Thread request');
    await client.sendStarted.future;
    expect(await controller.selectThread(_historyThreadTwo), isTrue);
    expect(controller.threadId, _historyThreadTwo);

    client.sendResult.complete(
      const AgentExchange(
        userMessage: AgentMessage(
          id: 'message_late_user',
          role: AgentMessageRole.user,
          text: 'old Thread request',
        ),
        assistantMessage: AgentMessage(
          id: 'message_late_assistant',
          role: AgentMessageRole.assistant,
          text: 'late old response',
        ),
      ),
    );

    expect(await staleSend, isFalse);
    expect(controller.threadId, _historyThreadTwo);
    expect(
      controller.messages.map((message) => message.text),
      isNot(contains('late old response')),
    );
    expect(controller.messages.single.id, 'message_current_$_historyThreadTwo');
  });

  test('rejects inconsistent or unsafe restored snapshots', () async {
    final invalidSnapshots = <AgentThreadSnapshot>[
      const AgentThreadSnapshot(
        threadId: 'thread_empty_goal',
        activeGoalId: ' ',
      ),
      const AgentThreadSnapshot(
        threadId: 'thread_empty_message',
        messages: <AgentMessage>[
          AgentMessage(id: ' ', role: AgentMessageRole.user, text: 'Message'),
        ],
      ),
      const AgentThreadSnapshot(
        threadId: 'thread_duplicate_message',
        messages: <AgentMessage>[
          AgentMessage(
            id: 'message_duplicate',
            role: AgentMessageRole.user,
            text: 'First',
          ),
          AgentMessage(
            id: 'message_duplicate',
            role: AgentMessageRole.assistant,
            text: 'Second',
          ),
        ],
      ),
    ];

    for (final snapshot in invalidSnapshots) {
      final controller = ConversationController(
        client: _SnapshotAgentClient(snapshot),
      );

      await controller.initialize();

      expect(controller.threadId, isNull, reason: snapshot.threadId);
      expect(controller.canRetry, isTrue, reason: snapshot.threadId);
      expect(controller.errorMessage, isNotNull, reason: snapshot.threadId);
      controller.dispose();
    }
  });
}

class _DelegatingAgentClient implements AgentClient {
  _DelegatingAgentClient([FakeAgentClient? delegate])
    : _delegate = delegate ?? FakeAgentClient();

  final FakeAgentClient _delegate;

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
  }) => _delegate.listMessages(
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
  }) {
    return _delegate.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
      imageAssetIds: imageAssetIds,
    );
  }
}

final class _HistoryAgentClient extends _DelegatingAgentClient {
  _HistoryAgentClient({
    this.controlOldSend = false,
    this.startsWithoutFocus = false,
    this.focusFailuresRemaining = 0,
    this.createFailuresRemaining = 0,
    this.ambiguousCreateFailuresRemaining = 0,
    this.initializationGate,
    this.createGate,
    this.initialThreadPage,
    this.refreshedThreadPage,
    this.threadRefreshFailuresRemaining = 0,
    this.textExchange,
    this.laterThreadPage,
    this.olderMessagePage,
  });

  final bool controlOldSend;
  final bool startsWithoutFocus;
  int focusFailuresRemaining;
  int createFailuresRemaining;
  int ambiguousCreateFailuresRemaining;
  final Completer<void>? initializationGate;
  final Completer<void>? createGate;
  final AgentThreadPage? initialThreadPage;
  final AgentThreadPage? refreshedThreadPage;
  int threadRefreshFailuresRemaining;
  final AgentExchange? textExchange;
  final AgentThreadPage? laterThreadPage;
  final AgentMessagePage? olderMessagePage;
  final initialListStarted = Completer<void>();
  final createStarted = Completer<void>();
  final sendStarted = Completer<void>();
  final sendResult = Completer<AgentExchange>();
  final List<String> focusRequests = <String>[];
  int createCalls = 0;
  int firstPageCalls = 0;
  int clearFocusedCalls = 0;

  @override
  Future<AgentThreadPage> listThreads({
    int pageSize = 20,
    String? cursor,
  }) async {
    if (cursor == null) {
      firstPageCalls++;
      if (!initialListStarted.isCompleted) {
        initialListStarted.complete();
      }
      if (firstPageCalls == 1) {
        await initializationGate?.future;
        final page = initialThreadPage;
        if (page != null) {
          return page;
        }
      } else {
        if (threadRefreshFailuresRemaining > 0) {
          threadRefreshFailuresRemaining--;
          throw StateError('Temporary Thread refresh failure.');
        }
        final page = refreshedThreadPage;
        if (page != null) {
          return page;
        }
      }
      return AgentThreadPage(
        threads: <AgentThreadSummary>[_historySummaryOne],
        focusedThreadId: startsWithoutFocus ? null : _historyThreadOne,
        nextCursor: 'older_threads',
      );
    }
    if (cursor == 'older_threads') {
      final override = laterThreadPage;
      if (override != null) {
        return override;
      }
      return AgentThreadPage(
        threads: <AgentThreadSummary>[_historySummaryTwo],
        focusedThreadId: startsWithoutFocus ? null : _historyThreadOne,
      );
    }
    throw StateError('Unexpected Thread cursor.');
  }

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() async {
    if (startsWithoutFocus) {
      return null;
    }
    return _historySnapshot(_historySummaryOne, hasOlderMessages: true);
  }

  @override
  Future<AgentThreadSummary> createThread() async {
    createCalls++;
    if (!createStarted.isCompleted) {
      createStarted.complete();
    }
    await createGate?.future;
    if (createFailuresRemaining > 0) {
      createFailuresRemaining--;
      throw StateError('Definite Thread creation failure.');
    }
    if (ambiguousCreateFailuresRemaining > 0) {
      ambiguousCreateFailuresRemaining--;
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        errorCode: 'thread_creation_ambiguous',
        retryable: true,
      );
    }
    return _historySummaryThree;
  }

  @override
  Future<AgentThreadSnapshot> setFocusedThread({
    required String threadId,
  }) async {
    focusRequests.add(threadId);
    if (focusFailuresRemaining > 0) {
      focusFailuresRemaining--;
      throw StateError('Temporary focus failure.');
    }
    final summary = switch (threadId) {
      _historyThreadOne => _historySummaryOne,
      _historyThreadTwo => _historySummaryTwo,
      _historyThreadThree => _historySummaryThree,
      _ => throw StateError('Unknown Thread.'),
    };
    return _historySnapshot(summary);
  }

  @override
  Future<void> clearFocusedThread() async {
    clearFocusedCalls++;
  }

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) async {
    if (threadId != _historyThreadOne || cursor != 'older_messages') {
      throw StateError('Unexpected Message page request.');
    }
    final override = olderMessagePage;
    if (override != null) {
      return override;
    }
    return const AgentMessagePage(
      messages: <AgentMessage>[
        AgentMessage(
          id: 'message_older',
          role: AgentMessageRole.user,
          text: 'older message',
          sequence: 1,
        ),
      ],
    );
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    final exchange = textExchange;
    if (exchange != null) {
      return Future<AgentExchange>.value(exchange);
    }
    if (controlOldSend && threadId == _historyThreadOne) {
      if (!sendStarted.isCompleted) {
        sendStarted.complete();
      }
      return sendResult.future;
    }
    return super.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
      imageAssetIds: imageAssetIds,
    );
  }
}

AgentThreadSnapshot _historySnapshot(
  AgentThreadSummary summary, {
  bool hasOlderMessages = false,
}) {
  return AgentThreadSnapshot(
    threadId: summary.id,
    messages: <AgentMessage>[
      AgentMessage(
        id: 'message_current_${summary.id}',
        role: AgentMessageRole.assistant,
        text: 'current ${summary.id}',
        sequence: 2,
      ),
    ],
    createdAt: summary.createdAt,
    updatedAt: summary.updatedAt,
    nextMessageCursor: hasOlderMessages ? 'older_messages' : null,
  );
}

final _historySummaryOne = AgentThreadSummary(
  id: _historyThreadOne,
  createdAt: DateTime.utc(2026, 7, 25, 8),
  updatedAt: DateTime.utc(2026, 7, 25, 10),
);
final _historySummaryTwo = AgentThreadSummary(
  id: _historyThreadTwo,
  createdAt: DateTime.utc(2026, 7, 24, 8),
  updatedAt: DateTime.utc(2026, 7, 25, 9),
);
final _historySummaryThree = AgentThreadSummary(
  id: _historyThreadThree,
  createdAt: DateTime.utc(2026, 7, 25, 11),
  updatedAt: DateTime.utc(2026, 7, 25, 11),
);

const _historyThreadOne = 'thread_history_one';
const _historyThreadTwo = 'thread_history_two';
const _historyThreadThree = 'thread_history_three';

final class _ControlledCleanupAgentClient extends _DelegatingAgentClient {
  final sendStarted = Completer<void>();
  final sendResult = Completer<AgentExchange>();
  final cleanupStarted = Completer<void>();
  final cleanupResult = Completer<void>();
  int cleanupCalls = 0;

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    sendStarted.complete();
    return sendResult.future;
  }

  @override
  Future<void> clearAccountState() async {
    cleanupCalls++;
    cleanupStarted.complete();
    await cleanupResult.future;
    await super.clearAccountState();
  }
}

final class _FailOnceTextAgentClient extends _DelegatingAgentClient {
  final List<String> messageClientIds = <String>[];

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    messageClientIds.add(clientMessageId);
    if (messageClientIds.length == 1) {
      throw StateError('temporary text failure');
    }
    return super.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
      imageAssetIds: imageAssetIds,
    );
  }
}

final class _SnapshotAgentClient extends _DelegatingAgentClient {
  _SnapshotAgentClient(this.snapshot);

  final AgentThreadSnapshot snapshot;
  final List<String> messageClientIds = <String>[];

  @override
  Future<AgentThreadPage> listThreads({
    int pageSize = 20,
    String? cursor,
  }) async {
    return AgentThreadPage(
      threads: const <AgentThreadSummary>[],
      focusedThreadId: snapshot.threadId,
    );
  }

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() async => snapshot;

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    messageClientIds.add(clientMessageId);
    if (snapshot.textRecovery != null) {
      return Future<AgentExchange>.value(
        AgentExchange(
          userMessage: snapshot.messages.last,
          assistantMessage: const AgentMessage(
            id: 'message_restored_assistant',
            role: AgentMessageRole.assistant,
            text: 'restored answer',
          ),
        ),
      );
    }
    return super.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
      imageAssetIds: imageAssetIds,
    );
  }
}

final class _FailOnceRestoreAgentClient extends _DelegatingAgentClient {
  int restoreCalls = 0;

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) {
    restoreCalls++;
    if (restoreCalls == 1) {
      throw StateError('temporary restore failure');
    }
    return super.listThreads(pageSize: pageSize, cursor: cursor);
  }
}
