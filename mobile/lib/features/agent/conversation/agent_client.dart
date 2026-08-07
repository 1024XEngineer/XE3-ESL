import 'dart:async';

import 'agent_models.dart';

abstract interface class AgentClient {
  /// Cancels account-scoped work, closes live resources, and removes temporary
  /// private artifacts before the next account can use this client.
  ///
  /// Implementations must be idempotent. Completion means cleanup is finished,
  /// not merely scheduled.
  Future<void> clearAccountState();

  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor});

  Future<AgentThreadSnapshot?> getFocusedThread();

  Future<AgentThreadSummary> createThread();

  Future<AgentThreadSnapshot> setFocusedThread({required String threadId});

  Future<void> clearFocusedThread();

  Future<void> deleteThread({required String threadId});

  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  });

  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  });
}

abstract interface class AgentStreamingTextClient {
  Stream<AgentTextStreamEvent> sendTextStream({
    required String threadId,
    required String text,
    required String clientMessageId,
  });
}

abstract interface class AgentMessageTranslationClient {
  Future<AgentMessageTranslation> translateMessage({required String messageId});
}

final class AgentMessageTranslation {
  const AgentMessageTranslation({
    required this.messageId,
    required this.targetLanguage,
    required this.content,
  });

  final String messageId;
  final String targetLanguage;
  final String content;
}

sealed class AgentTextStreamEvent {
  const AgentTextStreamEvent({required this.runId});

  final String runId;
}

final class AgentInputCommitted extends AgentTextStreamEvent {
  const AgentInputCommitted({required super.runId, required this.userMessage});

  final AgentMessage userMessage;
}

final class AgentAssistantStarted extends AgentTextStreamEvent {
  const AgentAssistantStarted({required super.runId});
}

final class AgentAssistantDelta extends AgentTextStreamEvent {
  const AgentAssistantDelta({required super.runId, required this.delta});

  final String delta;
}

final class AgentRunCompleted extends AgentTextStreamEvent {
  const AgentRunCompleted({
    required super.runId,
    required this.assistantMessageId,
  });

  final String assistantMessageId;
}

final class AgentRunFailed extends AgentTextStreamEvent {
  const AgentRunFailed({
    required super.runId,
    required this.kind,
    required this.retryable,
  });

  final String kind;
  final bool retryable;
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

final class FakeAgentClient implements AgentClient {
  FakeAgentClient({this.delay = Duration.zero});

  final Duration delay;
  int _messageSequence = 0;
  int _threadSequence = 0;
  int _accountGeneration = 0;
  bool _seededPreviewThread = false;
  String? _focusedThreadId;
  final Map<String, AgentThreadSummary> _threads = {};
  final Map<String, List<AgentMessage>> _threadMessages = {};
  final Map<String, AgentExchange> _textExchanges = {};
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
    _textExchanges.clear();
    await Future.wait(staleOperations);
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
  Future<void> deleteThread({required String threadId}) {
    return _runAccountOperation((generation) async {
      await _wait(generation);
      _requireCurrentGeneration(generation);
      if (!_threads.containsKey(threadId)) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.notFound,
          errorCode: 'resource_not_found',
        );
      }
      _threads.remove(threadId);
      _threadMessages.remove(threadId);
      if (_focusedThreadId == threadId) {
        _focusedThreadId = null;
      }
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
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    return _sendMessage(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
      imageAssetIds: imageAssetIds,
    );
  }

  Future<AgentExchange> _sendMessage({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    return _runAccountOperation((generation) async {
      final key = _operationKey(threadId, clientMessageId);
      await _wait(generation);
      _requireCurrentGeneration(generation);
      return _textExchanges.putIfAbsent(key, () {
        if (imageAssetIds.any((id) => id.trim().isEmpty)) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidRequest,
          );
        }
        final now = DateTime.now().toUtc();
        final images = <AgentImageAsset>[
          for (final id in imageAssetIds)
            AgentImageAsset(
              id: id,
              threadId: threadId,
              contentType: 'image/png',
              sizeBytes: 1,
              width: 1,
              height: 1,
              status: AgentImageAssetStatus.attached,
              createdAt: now,
              attachedAt: now,
            ),
        ];
        final exchange = AgentExchange(
          userMessage: AgentMessage(
            id: _nextMessageId(),
            role: AgentMessageRole.user,
            text: text,
            modality: images.isEmpty
                ? AgentMessageModality.text
                : AgentMessageModality.multimodal,
            images: images,
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
      title: thread.title,
      activeGoalId: thread.activeGoalId,
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
      title: thread.title,
      activeGoalId: thread.activeGoalId,
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
