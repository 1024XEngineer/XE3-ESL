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
