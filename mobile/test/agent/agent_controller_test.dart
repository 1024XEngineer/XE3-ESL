import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_recording.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

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
      expect(controller.activeMatter?.id, 'matter_self-introduction');
      expect(controller.messages.single.role, AgentMessageRole.assistant);

      for (var turn = 1; turn <= 3; turn++) {
        await controller.startRecording();
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
      expect(client.reviewClientIds, hasLength(1));
      expect(client.turnClientIds.toSet(), hasLength(3));

      await controller.confirmTranscript();
      expect(client.reviewClientIds, hasLength(1));
    },
  );

  test(
    'clears UI before awaiting client cleanup and discards a late response',
    () async {
      final client = _ControlledCleanupAgentClient();
      final controller = AgentController(client: client);
      await controller.initialize();

      final request = controller.sendText('private account message');
      await client.sendStarted.future;
      final cleanup = controller.clearPrivateState();
      await client.cleanupStarted.future;

      expect(client.cleanupCalls, 1);
      expect(controller.threadId, isNull);
      expect(controller.messages, isEmpty);
      expect(controller.review, isNull);

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
    final controller = AgentController(client: client);
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
    final controller = AgentController(client: client);
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
    final controller = AgentController(client: client);

    await controller.initialize();

    expect(controller.canRetry, isTrue);
    expect(controller.messages, hasLength(1));
    expect(controller.errorMessage, contains('继续重试'));

    await controller.retryLastOperation();

    expect(client.messageClientIds, <String>['message_restored_stable']);
    expect(controller.messages, hasLength(2));
    expect(controller.canRetry, isFalse);
  });

  test('retries a failed Turn with one stable client Turn identity', () async {
    final client = _FailOnceTurnAgentClient();
    final controller = AgentController(client: client);
    await controller.initialize();
    await controller.selectScene(agentScenes.first);

    await controller.startRecording();
    await controller.stopRecording();
    final transcript = controller.transcript;
    await controller.confirmTranscript();

    expect(controller.completedTurns, 0);
    expect(controller.transcript, transcript);
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );

    await controller.confirmTranscript();

    expect(controller.completedTurns, 1);
    expect(client.turnClientIds, hasLength(2));
    expect(client.turnClientIds.toSet(), hasLength(1));
  });

  test(
    'retries Review with one identity and never submits a fourth Turn',
    () async {
      final client = _FailOnceReviewAgentClient();
      final controller = AgentController(client: client);

      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      for (var turn = 1; turn <= 3; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }

      expect(controller.completedTurns, 3);
      expect(controller.review, isNull);
      expect(controller.recordingState, PracticeRecordingState.reviewFailed);
      expect(client.turnClientIds, hasLength(3));
      expect(client.reviewClientIds, hasLength(1));

      await controller.retryReview();

      expect(controller.completedTurns, 3);
      expect(controller.review, isNotNull);
      expect(controller.recordingState, PracticeRecordingState.completed);
      expect(client.turnClientIds, hasLength(3));
      expect(client.reviewClientIds, hasLength(2));
      expect(client.reviewClientIds.toSet(), hasLength(1));
    },
  );

  test(
    'restores active Matter and continues from authoritative 2 of 3',
    () async {
      final scene = agentScenes.first;
      final client = _SnapshotAgentClient(
        AgentThreadSnapshot(
          threadId: 'thread_server_1',
          activeMatter: AgentMatter(id: 'matter_server_1', scene: scene),
          practice: const AgentPracticeSnapshot(completedTurns: 2),
          messages: const <AgentMessage>[
            AgentMessage(
              id: 'message_server_1',
              role: AgentMessageRole.assistant,
              text: 'Third question',
            ),
          ],
        ),
      );
      final controller = AgentController(client: client);

      await controller.initialize();

      expect(controller.threadId, 'thread_server_1');
      expect(controller.activeMatter?.id, 'matter_server_1');
      expect(controller.scene, same(scene));
      expect(controller.completedTurns, 2);
      expect(controller.recordingState, PracticeRecordingState.idle);

      await controller.startRecording();
      await controller.stopRecording();

      expect(client.transcribedTurnNumbers, <int>[3]);
      await controller.confirmTranscript();
      expect(controller.completedTurns, 3);
      expect(controller.review, isNotNull);
    },
  );

  test('restores a pending Review with its stable client identity', () async {
    final scene = agentScenes.first;
    final client = _SnapshotAgentClient(
      AgentThreadSnapshot(
        threadId: 'thread_server_2',
        activeMatter: AgentMatter(id: 'matter_server_2', scene: scene),
        practice: const AgentPracticeSnapshot(
          completedTurns: 3,
          pendingReviewClientId: 'review_stable_from_snapshot',
        ),
      ),
    );
    final controller = AgentController(client: client);

    await controller.initialize();

    expect(controller.completedTurns, 3);
    expect(controller.recordingState, PracticeRecordingState.reviewFailed);
    await controller.retryReview();

    expect(client.reviewClientIds, <String>['review_stable_from_snapshot']);
    expect(controller.review, isNotNull);
  });

  test(
    'restore and startScene expose executable operation-specific retries',
    () async {
      final client = _FailOnceRestoreAndSceneAgentClient();
      final controller = AgentController(client: client);

      await controller.initialize();

      expect(controller.threadId, isNull);
      expect(controller.canRetry, isTrue);
      await controller.retryLastOperation();
      expect(controller.threadId, isNotNull);
      expect(controller.canRetry, isFalse);

      await controller.selectScene(agentScenes.first);
      expect(controller.scene, isNull);
      expect(controller.canRetry, isTrue);
      await controller.retryLastOperation();

      expect(controller.scene, same(agentScenes.first));
      expect(client.sceneClientIds, hasLength(2));
      expect(client.sceneClientIds.toSet(), hasLength(1));
      expect(controller.canRetry, isFalse);
    },
  );

  test('serializes scene selection while transcription is in flight', () async {
    final client = _ControlledTranscriptionAgentClient();
    final controller = AgentController(client: client);
    await controller.initialize();
    await controller.startRecording();

    final transcription = controller.stopRecording();
    await client.transcriptionStarted.future;
    await controller.selectScene(agentScenes[1]);

    expect(client.startSceneCalls, 0);
    expect(controller.scene, same(agentScenes.first));

    client.transcriptionResult.complete('old scene transcript');
    await transcription;
    controller.rerecord();
    await controller.selectScene(agentScenes[1]);

    expect(client.startSceneCalls, 1);
    expect(controller.scene, same(agentScenes[1]));
    expect(controller.transcript, isNull);
  });

  test(
    'selecting a new scene preserves the same Thread message history',
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
      final controller = AgentController(client: client);
      await controller.initialize();

      await controller.selectScene(agentScenes.first);

      expect(controller.threadId, 'thread_history');
      expect(controller.messages, hasLength(2));
      expect(controller.messages.first.id, 'existing');
      expect(controller.messages.last.role, AgentMessageRole.assistant);
    },
  );

  test(
    'Fake client cancels old account work and reuses stable write IDs',
    () async {
      final client = FakeAgentClient(delay: const Duration(milliseconds: 20));
      final staleRestore = client.restoreThread();
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

      final snapshot = await client.restoreThread();
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
      final nextAccount = await client.restoreThread();
      expect(nextAccount.threadId, isNot(snapshot.threadId));
    },
  );

  test(
    'restores focused Thread and continues bounded Thread and Message pages',
    () async {
      final client = _HistoryAgentClient();
      final controller = AgentController(client: client);
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
    'keeps an absent focused Thread empty until explicit creation',
    () async {
      final client = _HistoryAgentClient(startsWithoutFocus: true);
      final controller = AgentController(client: client);
      addTearDown(controller.dispose);

      await controller.initialize();

      expect(controller.threadId, isNull);
      expect(controller.messages, isEmpty);
      expect(controller.threads.single.id, _historyThreadOne);

      expect(await controller.createThread(), isTrue);
      expect(controller.threadId, _historyThreadThree);
      expect(client.focusRequests, <String>[_historyThreadThree]);
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
        final controller = AgentController(client: client);

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
    final controller = AgentController(client: client);
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
      final controller = AgentController(client: client);
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
    'retains a created Thread when focus fails and retries PUT without POST',
    () async {
      final client = _HistoryAgentClient(focusFailuresRemaining: 1);
      final controller = AgentController(client: client);
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
    'does not publish a selected Thread until its voice state is bound',
    () async {
      final client = FakeAgentClient();
      final audioPlayer = _FailingBindAgentVoiceAudioPlayer();
      final controller = AgentController(
        client: client,
        voiceRecorder: FakeAgentVoiceRecorder(),
        voiceAudioPlayer: audioPlayer,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      final homeThreadId = controller.threadId!;

      expect(await controller.createThread(), isTrue);
      final targetThreadId = controller.threadId!;
      expect(await controller.selectThread(homeThreadId), isTrue);

      audioPlayer.failOnStopCall = audioPlayer.stopCalls + 2;
      expect(await controller.selectThread(targetThreadId), isFalse);

      expect(controller.threadId, homeThreadId);
      expect(await controller.selectThread(targetThreadId), isTrue);
      expect(controller.threadId, targetThreadId);
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
      final controller = AgentController(client: client);
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
      final controller = AgentController(client: client);
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
        final controller = AgentController(client: client);
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
    final controller = AgentController(client: client);
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
    final scene = agentScenes.first;
    final matter = AgentMatter(id: 'matter_valid', scene: scene);
    const review = AgentReview(
      id: 'review_valid',
      title: 'Review',
      summary: 'Summary',
      strength: 'Strength',
      nextFocus: 'Next focus',
    );
    final invalidSnapshots = <AgentThreadSnapshot>[
      AgentThreadSnapshot(
        threadId: 'thread_pending_too_early',
        activeMatter: matter,
        practice: const AgentPracticeSnapshot(
          completedTurns: 2,
          pendingReviewClientId: 'review_wrong_state',
        ),
      ),
      AgentThreadSnapshot(
        threadId: 'thread_review_and_pending',
        activeMatter: matter,
        practice: const AgentPracticeSnapshot(
          completedTurns: 3,
          review: review,
          pendingReviewClientId: 'review_duplicate_state',
        ),
      ),
      AgentThreadSnapshot(
        threadId: 'thread_missing_pending_review',
        activeMatter: matter,
        practice: const AgentPracticeSnapshot(completedTurns: 3),
      ),
      AgentThreadSnapshot(
        threadId: 'thread_empty_matter',
        activeMatter: AgentMatter(id: ' ', scene: scene),
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
      final controller = AgentController(
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
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    return _delegate.createReview(
      threadId: threadId,
      scene: scene,
      clientReviewId: clientReviewId,
    );
  }

  @override
  Future<AgentThreadSnapshot> restoreThread() => _delegate.restoreThread();

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    return _delegate.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
    );
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    return _delegate.startScene(
      threadId: threadId,
      scene: scene,
      clientOperationId: clientOperationId,
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
    return _delegate.submitPracticeTurn(
      threadId: threadId,
      scene: scene,
      turnNumber: turnNumber,
      transcript: transcript,
      clientTurnId: clientTurnId,
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    return _delegate.transcribeTurn(
      threadId: threadId,
      turnNumber: turnNumber,
      clientTurnId: clientTurnId,
    );
  }
}

final class _HistoryAgentClient extends _DelegatingAgentClient
    implements AgentThreadHistoryClient {
  _HistoryAgentClient({
    this.controlOldSend = false,
    this.startsWithoutFocus = false,
    this.focusFailuresRemaining = 0,
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

final class _CountingAgentClient extends _DelegatingAgentClient {
  final List<String> turnClientIds = <String>[];
  final List<String> reviewClientIds = <String>[];

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    turnClientIds.add(clientTurnId);
    return super.submitPracticeTurn(
      threadId: threadId,
      scene: scene,
      turnNumber: turnNumber,
      transcript: transcript,
      clientTurnId: clientTurnId,
    );
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    reviewClientIds.add(clientReviewId);
    return super.createReview(
      threadId: threadId,
      scene: scene,
      clientReviewId: clientReviewId,
    );
  }
}

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
  }) {
    messageClientIds.add(clientMessageId);
    if (messageClientIds.length == 1) {
      throw StateError('temporary text failure');
    }
    return super.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
    );
  }
}

final class _FailOnceTurnAgentClient extends _DelegatingAgentClient {
  final List<String> turnClientIds = <String>[];

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    turnClientIds.add(clientTurnId);
    if (turnClientIds.length == 1) {
      throw StateError('temporary Turn failure');
    }
    return super.submitPracticeTurn(
      threadId: threadId,
      scene: scene,
      turnNumber: turnNumber,
      transcript: transcript,
      clientTurnId: clientTurnId,
    );
  }
}

final class _FailOnceReviewAgentClient extends _CountingAgentClient {
  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    reviewClientIds.add(clientReviewId);
    if (reviewClientIds.length == 1) {
      throw StateError('temporary Review failure');
    }
    return _delegateReview(
      threadId: threadId,
      scene: scene,
      clientReviewId: clientReviewId,
    );
  }

  Future<AgentReview> _delegateReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    return FakeAgentClient().createReview(
      threadId: threadId,
      scene: scene,
      clientReviewId: clientReviewId,
    );
  }
}

final class _SnapshotAgentClient extends _DelegatingAgentClient {
  _SnapshotAgentClient(this.snapshot);

  final AgentThreadSnapshot snapshot;
  final List<int> transcribedTurnNumbers = <int>[];
  final List<String> reviewClientIds = <String>[];
  final List<String> messageClientIds = <String>[];

  @override
  Future<AgentThreadSnapshot> restoreThread() async => snapshot;

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
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
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    transcribedTurnNumbers.add(turnNumber);
    return super.transcribeTurn(
      threadId: threadId,
      turnNumber: turnNumber,
      clientTurnId: clientTurnId,
    );
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    reviewClientIds.add(clientReviewId);
    return super.createReview(
      threadId: threadId,
      scene: scene,
      clientReviewId: clientReviewId,
    );
  }
}

final class _FailOnceRestoreAndSceneAgentClient extends _DelegatingAgentClient {
  int restoreCalls = 0;
  final List<String> sceneClientIds = <String>[];

  @override
  Future<AgentThreadSnapshot> restoreThread() {
    restoreCalls++;
    if (restoreCalls == 1) {
      throw StateError('temporary restore failure');
    }
    return super.restoreThread();
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    sceneClientIds.add(clientOperationId);
    if (sceneClientIds.length == 1) {
      throw StateError('temporary scene failure');
    }
    return super.startScene(
      threadId: threadId,
      scene: scene,
      clientOperationId: clientOperationId,
    );
  }
}

final class _FailingBindAgentVoiceAudioPlayer implements AgentVoiceAudioPlayer {
  final FakeAgentVoiceAudioPlayer _delegate = FakeAgentVoiceAudioPlayer();

  int stopCalls = 0;
  int? failOnStopCall;

  @override
  Stream<Duration> get onPosition => _delegate.onPosition;

  @override
  Stream<void> get onComplete => _delegate.onComplete;

  @override
  Future<void> playFile(String path, {required double speed}) =>
      _delegate.playFile(path, speed: speed);

  @override
  Future<void> playWav(Uint8List bytes, {required double speed}) =>
      _delegate.playWav(bytes, speed: speed);

  @override
  Future<void> stop() {
    stopCalls++;
    if (stopCalls == failOnStopCall) {
      throw StateError('Injected voice bind failure.');
    }
    return _delegate.stop();
  }

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<void> dispose() => _delegate.dispose();
}

final class _ControlledTranscriptionAgentClient extends _DelegatingAgentClient {
  final transcriptionStarted = Completer<void>();
  final transcriptionResult = Completer<String>();
  int startSceneCalls = 0;

  @override
  Future<AgentThreadSnapshot> restoreThread() async {
    return AgentThreadSnapshot(
      threadId: 'thread_transcription',
      activeMatter: AgentMatter(
        id: 'matter_original',
        scene: agentScenes.first,
      ),
      practice: const AgentPracticeSnapshot(completedTurns: 0),
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    transcriptionStarted.complete();
    return transcriptionResult.future;
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    startSceneCalls++;
    return super.startScene(
      threadId: threadId,
      scene: scene,
      clientOperationId: clientOperationId,
    );
  }
}
