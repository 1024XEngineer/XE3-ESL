import 'dart:async';

import 'agent_models.dart';

abstract interface class AgentClient {
  /// Cancels account-scoped work, closes live resources, and removes temporary
  /// private artifacts before the next account can use this client.
  ///
  /// Implementations must be idempotent. Completion means cleanup is finished,
  /// not merely scheduled.
  Future<void> clearAccountState();

  Future<AgentThreadSnapshot> restoreThread();

  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  });

  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  });

  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  });

  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  });

  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  });
}

/// Optional bounded history and focused-Thread capability from Issue #102.
///
/// Older preview/test clients can keep the original single-Thread contract.
/// Production and the bundled Fake implement this capability so one
/// [AgentController] remains authoritative for every selected Thread.
abstract interface class AgentThreadHistoryClient {
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor});

  Future<AgentThreadSnapshot?> getFocusedThread();

  Future<AgentThreadSummary> createThread();

  Future<AgentThreadSnapshot> setFocusedThread({required String threadId});

  Future<void> clearFocusedThread();

  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  });
}

/// Optional capability declaration for the preview-only practice flow.
///
/// Clients that do not implement this interface retain the existing Fake/test
/// behavior. Production clients must explicitly opt out when the accepted API
/// cannot represent the scene, voice, and Review lifecycle without inventing
/// fields.
abstract interface class AgentPracticeAvailability {
  bool get supportsPracticeFlow;
}

enum AgentClientFailureKind {
  unavailable,
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  rateLimited,
  server,
  network,
  invalidResponse,
  runFailed,
  pollingTimedOut,
  unexpected,
}

/// A redacted Agent failure safe to surface in diagnostics.
///
/// Request content, response bodies, and Session credentials are deliberately
/// never retained by this value.
final class AgentClientException implements Exception {
  const AgentClientException({
    required this.kind,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
    this.correlationId,
    this.retryAfter,
  });

  final AgentClientFailureKind kind;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;
  final String? correlationId;
  final Duration? retryAfter;

  bool get isUnavailable => kind == AgentClientFailureKind.unavailable;

  @override
  String toString() {
    final status = statusCode == null ? '' : ', statusCode: $statusCode';
    final code = errorCode == null ? '' : ', errorCode: $errorCode';
    return 'AgentClientException(kind: ${kind.name}$status$code)';
  }
}

final class AgentClientOperationCancelled implements Exception {
  const AgentClientOperationCancelled();

  @override
  String toString() => 'Agent operation was cancelled during account cleanup.';
}

final class FakeAgentClient implements AgentClient, AgentThreadHistoryClient {
  FakeAgentClient({this.delay = Duration.zero});

  final Duration delay;
  int _messageSequence = 0;
  int _threadSequence = 0;
  int _accountGeneration = 0;
  bool _seededPreviewThread = false;
  String? _focusedThreadId;
  final Map<String, AgentThreadSummary> _threads = {};
  final Map<String, List<AgentMessage>> _threadMessages = {};
  final Map<String, AgentMatter> _threadMatters = {};
  final Map<String, AgentSceneStart> _sceneStarts = {};
  final Map<String, AgentExchange> _textExchanges = {};
  final Map<String, String> _transcripts = {};
  final Map<String, AgentExchange> _practiceExchanges = {};
  final Map<String, AgentReview> _reviews = {};
  final Set<Future<void>> _inFlightOperations = <Future<void>>{};

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
    final staleOperations = List<Future<void>>.of(_inFlightOperations);
    _messageSequence = 0;
    _threadSequence = 0;
    _seededPreviewThread = false;
    _focusedThreadId = null;
    _threads.clear();
    _threadMessages.clear();
    _threadMatters.clear();
    _sceneStarts.clear();
    _textExchanges.clear();
    _transcripts.clear();
    _practiceExchanges.clear();
    _reviews.clear();
    await Future.wait(staleOperations);
  }

  @override
  Future<AgentThreadSnapshot> restoreThread() {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      _seedPreviewThread(generation);
      return _snapshotFor(_focusedThreadId!);
    });
  }

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      if (pageSize < 1 || pageSize > 100) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      _seedPreviewThread(generation);
      final offset = _fakeCursorOffset(cursor, prefix: 'threads');
      final values = _threads.values.toList()
        ..sort((left, right) {
          final byUpdatedAt = right.updatedAt.compareTo(left.updatedAt);
          return byUpdatedAt != 0 ? byUpdatedAt : right.id.compareTo(left.id);
        });
      if (offset > values.length) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      final end = (offset + pageSize).clamp(0, values.length).toInt();
      return AgentThreadPage(
        threads: List<AgentThreadSummary>.unmodifiable(
          values.sublist(offset, end),
        ),
        focusedThreadId: _focusedThreadId,
        nextCursor: end < values.length ? 'threads:$end' : null,
      );
    });
  }

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      _seedPreviewThread(generation);
      final threadId = _focusedThreadId;
      return threadId == null ? null : _snapshotFor(threadId);
    });
  }

  @override
  Future<AgentThreadSummary> createThread() {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      _seededPreviewThread = true;
      final now = DateTime.now().toUtc();
      final thread = AgentThreadSummary(
        id: 'thread_local_preview_${generation}_${++_threadSequence}',
        createdAt: now,
        updatedAt: now,
      );
      _threads[thread.id] = thread;
      _threadMessages[thread.id] = <AgentMessage>[];
      return thread;
    });
  }

  @override
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      if (!_threads.containsKey(threadId)) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.notFound,
          errorCode: 'resource_not_found',
        );
      }
      _focusedThreadId = threadId;
      return _snapshotFor(threadId);
    });
  }

  @override
  Future<void> clearFocusedThread() {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      _focusedThreadId = null;
    });
  }

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      final messages = _threadMessages[threadId];
      if (messages == null || pageSize < 1 || pageSize > 100) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      final before = cursor == null
          ? messages.length
          : _fakeCursorOffset(cursor, prefix: 'messages');
      if (before > messages.length) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      final start = (before - pageSize).clamp(0, before).toInt();
      return AgentMessagePage(
        messages: List<AgentMessage>.unmodifiable(
          messages.sublist(start, before),
        ),
        nextCursor: start > 0 ? 'messages:$start' : null,
      );
    });
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    return _runAccountOperation((generation) async {
      final key = _operationKey(threadId, clientOperationId);
      await _wait(generation);
      _requireCurrentGeneration(generation);
      return _sceneStarts.putIfAbsent(key, () {
        final activeMatter = AgentMatter(
          id: 'matter_${scene.id}',
          scene: scene,
        );
        final assistantMessage = AgentMessage(
          id: _nextMessageId(),
          role: AgentMessageRole.assistant,
          text: '我们开始“${scene.title}”。第一轮：请先用英文回答，你希望面试官首先了解你的哪段经历？',
        );
        _threadMatters[threadId] = activeMatter;
        _appendThreadMessages(threadId, <AgentMessage>[assistantMessage]);
        return AgentSceneStart(
          activeMatter: activeMatter,
          assistantMessage: assistantMessage,
        );
      });
    });
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    return _runAccountOperation((generation) async {
      final key = _operationKey(threadId, clientMessageId);
      await _wait(generation);
      _requireCurrentGeneration(generation);
      return _textExchanges.putIfAbsent(key, () {
        final exchange = AgentExchange(
          userMessage: AgentMessage(
            id: _nextMessageId(),
            role: AgentMessageRole.user,
            text: text,
          ),
          assistantMessage: AgentMessage(
            id: _nextMessageId(),
            role: AgentMessageRole.assistant,
            text: '我会围绕这点继续追问。你能补充一个具体例子和最终结果吗？',
          ),
        );
        _appendThreadMessages(threadId, <AgentMessage>[
          exchange.userMessage,
          ?exchange.assistantMessage,
        ]);
        return exchange;
      });
    });
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    return _runAccountOperation((generation) async {
      final key = _operationKey(threadId, clientTurnId);
      await _wait(generation);
      _requireCurrentGeneration(generation);
      return _transcripts.putIfAbsent(
        key,
        () => switch (turnNumber) {
          1 =>
            'I led the backend migration and kept the rollout safe with staged checks.',
          2 =>
            'The main trade-off was delivery speed versus reliability, so I reduced the scope first.',
          _ =>
            'The result was a stable release, and I learned to communicate risks much earlier.',
        },
      );
    });
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    return _runAccountOperation((generation) async {
      final key = _operationKey(threadId, clientTurnId);
      await _wait(generation);
      _requireCurrentGeneration(generation);
      return _practiceExchanges.putIfAbsent(key, () {
        final nextQuestion = switch (turnNumber) {
          1 => '第二轮：当时最困难的取舍是什么？请说明你为什么这样决定。',
          2 => '第三轮：结果如何？如果再做一次，你会改变什么？',
          _ => null,
        };
        final exchange = AgentExchange(
          userMessage: AgentMessage(
            id: _nextMessageId(),
            role: AgentMessageRole.user,
            text: transcript,
          ),
          assistantMessage: nextQuestion == null
              ? null
              : AgentMessage(
                  id: _nextMessageId(),
                  role: AgentMessageRole.assistant,
                  text: nextQuestion,
                ),
        );
        _appendThreadMessages(threadId, <AgentMessage>[
          exchange.userMessage,
          ?exchange.assistantMessage,
        ]);
        return exchange;
      });
    });
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    return _runAccountOperation((generation) async {
      final key = _operationKey(threadId, clientReviewId);
      await _wait(generation);
      _requireCurrentGeneration(generation);
      return _reviews.putIfAbsent(
        key,
        () => AgentReview(
          id: 'review_$threadId',
          title: '${scene.title} · 三轮复盘',
          summary: '你已经能按“背景—行动—结果”组织回答，整体表达连贯。',
          strength: '能够说明自己的责任，并给出具体取舍。',
          nextFocus: '下一次把结果量化，同时缩短开场句。',
        ),
      );
    });
  }

  String _nextMessageId() => 'message_${++_messageSequence}';

  void _seedPreviewThread(int generation) {
    if (_seededPreviewThread) {
      return;
    }
    _seededPreviewThread = true;
    final now = DateTime.now().toUtc();
    final thread = AgentThreadSummary(
      id: 'thread_local_preview_${generation}_${++_threadSequence}',
      createdAt: now,
      updatedAt: now,
    );
    _threads[thread.id] = thread;
    _threadMessages[thread.id] = <AgentMessage>[];
    _focusedThreadId = thread.id;
  }

  AgentThreadSnapshot _snapshotFor(String threadId) {
    final thread = _threads[threadId]!;
    return AgentThreadSnapshot(
      threadId: thread.id,
      activeMatter: _threadMatters[threadId],
      messages: List<AgentMessage>.unmodifiable(
        _threadMessages[threadId] ?? const <AgentMessage>[],
      ),
      createdAt: thread.createdAt,
      updatedAt: thread.updatedAt,
    );
  }

  void _appendThreadMessages(String threadId, Iterable<AgentMessage> messages) {
    final target = _threadMessages[threadId];
    final thread = _threads[threadId];
    if (target == null || thread == null) {
      return;
    }
    final ids = <String>{for (final message in target) message.id};
    for (final message in messages) {
      if (ids.add(message.id)) {
        target.add(message);
      }
    }
    final now = DateTime.now().toUtc();
    _threads[threadId] = AgentThreadSummary(
      id: thread.id,
      activeMatterId: _threadMatters[threadId]?.id,
      createdAt: thread.createdAt,
      updatedAt: now.isBefore(thread.updatedAt) ? thread.updatedAt : now,
    );
  }

  String _operationKey(String threadId, String clientId) {
    return '$threadId\u{0}$clientId';
  }

  Future<T> _runAccountOperation<T>(
    Future<T> Function(int generation) operation,
  ) {
    final generation = _accountGeneration;
    final completion = Completer<void>();
    final marker = completion.future;
    _inFlightOperations.add(marker);
    return Future<T>.sync(() => operation(generation)).whenComplete(() {
      _inFlightOperations.remove(marker);
      completion.complete();
    });
  }

  Future<void> _wait(int generation) async {
    if (delay != Duration.zero) {
      await Future<void>.delayed(delay);
    }
    _requireCurrentGeneration(generation);
  }

  void _requireCurrentGeneration(int generation) {
    if (generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }
}

int _fakeCursorOffset(String? cursor, {required String prefix}) {
  if (cursor == null) {
    return 0;
  }
  final parts = cursor.split(':');
  final offset = parts.length == 2 && parts.first == prefix
      ? int.tryParse(parts.last)
      : null;
  if (offset == null || offset < 0) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
  return offset;
}
