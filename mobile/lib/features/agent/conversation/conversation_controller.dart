import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_image_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';

typedef ConversationClientIdFactory = String Function(String scope);
typedef ConversationAssistantStreamStarted =
    Future<void> Function(String transientMessageId);
typedef ConversationAssistantStreamDelta =
    void Function(String transientMessageId, String delta);
typedef ConversationAssistantStreamCompleted =
    void Function(String transientMessageId, AgentMessage message);
typedef ConversationAssistantStreamFailed =
    void Function(String transientMessageId);

/// Owns the locally selected Agent Thread, committed Messages, and text Run lifecycle.
///
/// Composer drafts and formal Practice state deliberately live elsewhere.
final class ConversationController extends ChangeNotifier {
  ConversationController({
    required this.client,
    this.messageImageClient,
    this.onAssistantStreamStarted,
    this.onAssistantStreamDelta,
    this.onAssistantStreamCompleted,
    this.onAssistantStreamFailed,
    ConversationClientIdFactory? clientIdFactory,
  }) : _clientIdFactory = clientIdFactory ?? _createSecureClientId;

  final AgentClient client;
  final AgentMessageImageClient? messageImageClient;
  final ConversationAssistantStreamStarted? onAssistantStreamStarted;
  final ConversationAssistantStreamDelta? onAssistantStreamDelta;
  final ConversationAssistantStreamCompleted? onAssistantStreamCompleted;
  final ConversationAssistantStreamFailed? onAssistantStreamFailed;
  final ConversationClientIdFactory _clientIdFactory;

  String? _threadId;
  AgentThreadSummary? _currentThreadSummary;
  List<AgentThreadSummary> _threads = const <AgentThreadSummary>[];
  String? _nextThreadCursor;
  String? _nextMessageCursor;
  String? _threadHistoryErrorMessage;
  _ThreadHistoryRecovery? _threadHistoryRecovery;
  int _draftThreadRecoveryGeneration = 0;
  bool _loadingMoreThreads = false;
  bool _loadingEarlierMessages = false;
  bool _threadTransitionInFlight = false;
  int _threadTransitionGeneration = 0;
  List<AgentMessage> _messages = const <AgentMessage>[];
  String? _errorMessage;
  _ConversationRetry? _retry;
  bool _initialized = false;
  bool _busy = false;
  bool _replyPending = false;
  bool _disposed = false;
  int _epoch = 0;
  Future<void>? _initializationFuture;
  Future<void>? _accountCleanupFuture;

  String? get threadId => _threadId;
  AgentThreadSummary? get currentThreadSummary => _currentThreadSummary;
  List<AgentThreadSummary> get threads =>
      List<AgentThreadSummary>.unmodifiable(_threads);
  List<AgentMessage> get messages => List<AgentMessage>.unmodifiable(_messages);
  bool get isInitialized => _initialized;

  bool get isThreadTransitionInFlight => _threadTransitionInFlight;
  bool get hasMoreThreads => _nextThreadCursor != null;
  bool get isLoadingMoreThreads => _loadingMoreThreads;
  String? get threadHistoryErrorMessage => _threadHistoryErrorMessage;
  bool get canRetryThreadHistory => _threadHistoryRecovery != null;
  bool get hasPendingThreadCreationRecovery =>
      _threadHistoryRecovery == _ThreadHistoryRecovery.create;
  int get draftThreadRecoveryGeneration => _draftThreadRecoveryGeneration;
  bool get hasEarlierMessages => _nextMessageCursor != null;
  bool get isLoadingEarlierMessages => _loadingEarlierMessages;
  String? get errorMessage => _errorMessage;
  bool get isBusy => _busy || _replyPending || _threadTransitionInFlight;
  bool get isRestoring => !_initialized && _busy && !_replyPending;
  bool get isReplyPending => _replyPending;
  bool get isComposerBlocked =>
      _replyPending || _threadTransitionInFlight || (_busy && !isRestoring);
  bool get canRetry => _retry != null;
  String get retryActionLabel => switch (_retry) {
    _TextRetry(restored: true) => '继续',
    _ => '重试',
  };

  Future<void> initialize() async {
    if (_initialized || _disposed) {
      return;
    }
    final cleanup = _accountCleanupFuture;
    if (cleanup != null) {
      await cleanup;
      if (_initialized || _disposed) {
        return;
      }
    }
    final inFlight = _initializationFuture;
    if (inFlight != null) {
      await inFlight;
      return;
    }
    final operation = _restore();
    _initializationFuture = operation;
    try {
      await operation;
    } finally {
      if (identical(_initializationFuture, operation)) {
        _initializationFuture = null;
      }
    }
  }

  Future<void> _restore() async {
    final fence = _captureOperationFence();
    final epoch = fence.epoch;
    _retry = null;
    _errorMessage = null;
    _threadHistoryErrorMessage = null;
    _setBusy(true);
    try {
      await _restoreThreadHistory(client, fence);
    } catch (_) {
      if (_isCurrent(epoch)) {
        _retry = const _RestoreRetry();
        _errorMessage = '暂时无法恢复对话，请稍后重试。';
      }
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> _restoreThreadHistory(
    AgentClient historyClient,
    _ConversationOperationFence fence,
  ) async {
    final page = await historyClient.listThreads();
    _validateThreadPage(page);
    if (!_isOperationCurrent(fence)) {
      return;
    }
    _threads = List<AgentThreadSummary>.from(page.threads);
    _nextThreadCursor = page.nextCursor;
    _threadHistoryErrorMessage = null;
    notifyListeners();

    final newest = page.threads.firstOrNull;
    if (newest == null) {
      _resetSelectedThreadPresentation();
      _initialized = true;
      return;
    }
    final snapshot = await historyClient.getThread(threadId: newest.id);
    if (!_isOperationCurrent(fence)) {
      return;
    }
    await _applyThreadSnapshot(snapshot, fence: fence, summary: newest);
  }

  Future<void> _applyThreadSnapshot(
    AgentThreadSnapshot thread, {
    required _ConversationOperationFence fence,
    AgentThreadSummary? summary,
  }) async {
    _validateThreadSnapshot(thread);
    if (!_isOperationCurrent(fence)) {
      return;
    }
    final messages = List<AgentMessage>.from(thread.messages);
    final resolvedSummary =
        summary ?? _threadSummaryFromSnapshot(thread) ?? _currentThreadSummary;
    _threadId = thread.threadId;
    _currentThreadSummary = resolvedSummary;
    _nextMessageCursor = thread.nextMessageCursor;
    _messages = messages;
    _initialized = true;
    _applyRestoredTextState(thread);
    unawaited(_hydrateMessageImageContents(fence));
  }

  void _applyRestoredTextState(AgentThreadSnapshot thread) {
    final textRecovery = thread.textRecovery;
    if (textRecovery != null) {
      final latestRun = textRecovery.latestRun;
      final inputMessageID =
          latestRun?.inputMessageId ?? thread.messages.last.id;
      _retry = textRecovery.canContinue
          ? _TextRetry(
              text: textRecovery.text,
              clientMessageId: textRecovery.clientMessageId,
              imageAssetIds: textRecovery.imageAssetIds,
              committedUserMessageID: inputMessageID,
              restored: true,
            )
          : null;
      _errorMessage = switch (latestRun?.status) {
        AgentRunStatus.pending || AgentRunStatus.running => '上次回复未完成，点击“继续”恢复。',
        AgentRunStatus.failed when latestRun!.failureRetryable =>
          '上次回复未完成，可以继续。',
        AgentRunStatus.failed => '上次回复未完成，服务端不允许继续。',
        AgentRunStatus.completed => null,
        null => '上次回复未完成，但没有可恢复的运行记录。',
      };
    }
  }

  Future<bool> createThread() async {
    if (_disposed) {
      return false;
    }
    return _createNewThread(reuseBlankCurrent: true);
  }

  Future<bool> _createNewThread({bool reuseBlankCurrent = false}) async {
    if (_disposed) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    final historyClient = client;
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return false;
      }
      if (reuseBlankCurrent && _canReuseCurrentBlankThread) {
        return true;
      }
      return await _transitionThread(historyClient, createNew: true);
    } finally {
      _finishThreadTransition(transitionGeneration);
    }
  }

  bool get _canReuseCurrentBlankThread {
    final current = _currentThreadSummary;
    return _threadId != null &&
        current?.id == _threadId &&
        _messages.isEmpty &&
        _retry == null;
  }

  Future<bool> selectThread(String threadId) async {
    if (_disposed || threadId.trim().isEmpty) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    final historyClient = client;
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return false;
      }
      if (threadId == _threadId) {
        return true;
      }
      return await _transitionThread(
        historyClient,
        selectedThreadId: threadId,
        createNew: false,
      );
    } finally {
      _finishThreadTransition(transitionGeneration);
    }
  }

  Future<bool> reloadCurrentThread() async {
    final currentThreadId = _threadId;
    if (_disposed || currentThreadId == null) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    final historyClient = client;
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return false;
      }
      return await _transitionThread(
        historyClient,
        selectedThreadId: currentThreadId,
        createNew: false,
      );
    } finally {
      _finishThreadTransition(transitionGeneration);
    }
  }

  Future<bool> _transitionThread(
    AgentClient historyClient, {
    String? selectedThreadId,
    required bool createNew,
  }) async {
    final recoveryAtStart = _threadHistoryRecovery;
    final recoveringDraftThread =
        _threadId == null &&
        createNew &&
        recoveryAtStart == _ThreadHistoryRecovery.create;
    _epoch++;
    _loadingMoreThreads = false;
    _loadingEarlierMessages = false;
    final fence = _captureOperationFence();
    _retry = null;
    _errorMessage = null;
    if (_isPageRefreshRecovery(_threadHistoryRecovery)) {
      _threadHistoryRecovery = null;
    }
    if (_threadHistoryRecovery != _ThreadHistoryRecovery.create) {
      _threadHistoryErrorMessage = null;
    }
    AgentThreadSummary? summary;
    var targetThreadId = selectedThreadId;
    _setBusy(true);
    try {
      if (createNew) {
        summary = await historyClient.createThread();
        _validateThreadSummary(summary);
        if (!_isOperationCurrent(fence)) {
          return false;
        }
        _mergeThreadSummary(summary, placeFirst: true);
        notifyListeners();
        targetThreadId = summary.id;
      } else {
        summary = _threads
            .where((thread) => thread.id == selectedThreadId)
            .firstOrNull;
      }
      if (!_isOperationCurrent(fence) || targetThreadId == null) {
        return false;
      }
      final snapshot = createNew
          ? AgentThreadSnapshot(
              threadId: summary!.id,
              title: summary.title,
              messages: const <AgentMessage>[],
              createdAt: summary.createdAt,
              updatedAt: summary.updatedAt,
            )
          : await historyClient.getThread(threadId: targetThreadId);
      if (snapshot.threadId != targetThreadId) {
        throw StateError('Thread identity did not match the request.');
      }
      await _applyThreadSnapshot(snapshot, fence: fence, summary: summary);
      if (!_isOperationCurrent(fence) || _threadId != targetThreadId) {
        return false;
      }
      final canonicalSummary = summary ?? _threadSummaryFromSnapshot(snapshot);
      if (canonicalSummary != null) {
        _mergeThreadSummary(canonicalSummary, placeFirst: createNew);
        _currentThreadSummary = canonicalSummary;
      }
      if (recoveringDraftThread) {
        _draftThreadRecoveryGeneration++;
      }
      _threadHistoryRecovery = null;
      _threadHistoryErrorMessage = null;
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        if (createNew &&
            error is AgentClientException &&
            error.errorCode == 'thread_creation_ambiguous') {
          _threadHistoryRecovery = _ThreadHistoryRecovery.create;
          _threadHistoryErrorMessage = '新对话的创建结果尚未确认。请重试恢复；系统不会重复创建。';
        } else if (_threadHistoryRecovery != _ThreadHistoryRecovery.create) {
          _threadHistoryErrorMessage = createNew
              ? '暂时无法创建新对话，请稍后再试。'
              : '暂时无法切换对话，请稍后再试。';
        }
      }
      return false;
    } finally {
      if (_isOperationCurrent(fence)) {
        _setBusy(false);
      }
    }
  }

  Future<void> clearCurrentThread() async {
    if (_disposed) {
      return;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return;
    }
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return;
      }
      _clearCurrentThread();
    } finally {
      _finishThreadTransition(transitionGeneration);
    }
  }

  Future<bool> deleteThread(String threadId) async {
    if (_disposed || threadId.trim().isEmpty) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    _threadHistoryErrorMessage = null;
    _setBusy(true);
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return false;
      }
      await client.deleteThread(threadId: threadId);
      if (!_isCurrent(accountEpoch)) {
        return false;
      }
      _threads = <AgentThreadSummary>[
        for (final thread in _threads)
          if (thread.id != threadId) thread,
      ];
      if (_threadId == threadId) {
        _epoch++;
        _threadHistoryRecovery = null;
        _resetSelectedThreadPresentation();
        _initialized = true;
      }
      notifyListeners();
      return true;
    } on AgentClientException catch (_) {
      _threadHistoryErrorMessage = '暂时无法删除对话，请稍后再试。';
      return false;
    } catch (_) {
      _threadHistoryErrorMessage = '暂时无法删除对话，请稍后再试。';
      return false;
    } finally {
      _setBusy(false);
      _finishThreadTransition(transitionGeneration);
    }
  }

  void _clearCurrentThread() {
    _epoch++;
    _retry = null;
    _errorMessage = null;
    _threadHistoryRecovery = null;
    _threadHistoryErrorMessage = null;
    _resetSelectedThreadPresentation();
    _initialized = true;
    notifyListeners();
  }

  Future<void> loadMoreThreads() async {
    final cursor = _nextThreadCursor;
    if (cursor == null || _loadingMoreThreads || _disposed) {
      return;
    }
    final historyClient = client;
    final fence = _captureOperationFence();
    _loadingMoreThreads = true;
    _threadHistoryErrorMessage = null;
    notifyListeners();
    try {
      final page = await historyClient.listThreads(cursor: cursor);
      _validateThreadPage(page);
      if (!_isOperationCurrent(fence)) {
        return;
      }
      if (page.nextCursor == cursor) {
        throw StateError('Thread cursor did not advance.');
      }
      final knownIds = <String>{for (final thread in _threads) thread.id};
      if (page.threads.any((thread) => knownIds.contains(thread.id))) {
        throw StateError('Thread pages overlapped.');
      }
      if (_threads.isNotEmpty &&
          page.threads.isNotEmpty &&
          !_threadSortsAfter(page.threads.first, _threads.last)) {
        throw StateError('Thread page crossed the existing keyset boundary.');
      }
      _threads = <AgentThreadSummary>[..._threads, ...page.threads];
      _nextThreadCursor = page.nextCursor;
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _threadHistoryErrorMessage = '暂时无法加载更早的对话，请稍后再试。';
      }
    } finally {
      if (_isOperationCurrent(fence)) {
        _loadingMoreThreads = false;
        notifyListeners();
      }
    }
  }

  Future<void> loadEarlierMessages() async {
    final threadId = _threadId;
    final cursor = _nextMessageCursor;
    if (threadId == null ||
        cursor == null ||
        _loadingEarlierMessages ||
        _disposed) {
      return;
    }
    final historyClient = client;
    final fence = _captureOperationFence(threadId: threadId);
    _loadingEarlierMessages = true;
    notifyListeners();
    try {
      final page = await historyClient.listMessages(
        threadId: threadId,
        cursor: cursor,
      );
      _validateMessagePage(page);
      if (!_isOperationCurrent(fence)) {
        return;
      }
      if (page.nextCursor == cursor) {
        throw StateError('Message cursor did not advance.');
      }
      final ids = <String>{for (final message in _messages) message.id};
      if (page.messages.any((message) => ids.contains(message.id))) {
        throw StateError('Message pages overlapped.');
      }
      final currentFirstSequence = _messages
          .map((message) => message.sequence)
          .whereType<int>()
          .firstOrNull;
      if (page.messages.isNotEmpty &&
          (currentFirstSequence == null ||
              page.messages.last.sequence! >= currentFirstSequence)) {
        throw StateError(
          'Message page crossed the existing sequence boundary.',
        );
      }
      _messages = <AgentMessage>[...page.messages, ..._messages];
      _nextMessageCursor = page.nextCursor;
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _errorMessage = '暂时无法加载更早的消息，请稍后再试。';
      }
    } finally {
      if (_isOperationCurrent(fence)) {
        _loadingEarlierMessages = false;
        notifyListeners();
      }
    }
  }

  /// Starts an ordinary Agent voice Message in the selected Thread.
  ///
  /// If the client has no selected Thread, the existing safe Thread creation
  /// path runs first. This microphone is intentionally independent from the
  /// Practice turn recorder.

  Future<void> refreshMessageImage(
    String messageId,
    String imageAssetId,
  ) async {
    final imageClient = messageImageClient;
    final message = _messages
        .where((message) => message.id == messageId)
        .firstOrNull;
    if (imageClient == null ||
        message == null ||
        !message.images.any((image) => image.id == imageAssetId)) {
      return;
    }
    final fence = _captureOperationFence(threadId: _threadId);
    try {
      final content = await imageClient.getMessageImageContent(
        imageAssetId: imageAssetId,
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _messages = <AgentMessage>[
        for (final candidate in _messages)
          if (candidate.id == messageId)
            candidate.copyWith(
              images: <AgentImageAsset>[
                for (final image in candidate.images)
                  if (image.id == imageAssetId)
                    image.withContent(
                      contentUrl: content.url,
                      expiresAt: content.expiresAt,
                    )
                  else
                    image,
              ],
            )
          else
            candidate,
      ];
      notifyListeners();
    } catch (_) {
      // The bubble remains a safe placeholder and can request another retry.
    }
  }

  Future<void> _hydrateMessageImageContents(
    _ConversationOperationFence fence,
  ) async {
    final targets = <({String messageId, String imageId})>[
      for (final message in _messages)
        for (final image in message.images)
          if (!image.isReadable) (messageId: message.id, imageId: image.id),
    ];
    for (final target in targets) {
      if (!_isOperationCurrent(fence)) {
        return;
      }
      await refreshMessageImage(target.messageId, target.imageId);
    }
  }

  Future<bool> sendText(
    String value, {
    List<String> imageAssetIds = const <String>[],
  }) async {
    final text = value.trim();
    if (text.isEmpty || imageAssetIds.any((id) => id.trim().isEmpty)) {
      return false;
    }
    final accountEpoch = _epoch;
    await _ensureInitialized();
    if (!_isCurrent(accountEpoch) || isBusy || _disposed) {
      return false;
    }
    if (_threadId == null) {
      final created = await createThread();
      if (!created || _threadId == null || _disposed) {
        return false;
      }
    }
    final retry = _retry;
    final operation =
        retry is _TextRetry &&
            !retry.restored &&
            retry.text == text &&
            listEquals(retry.imageAssetIds, imageAssetIds)
        ? retry
        : _TextRetry(
            text: text,
            clientMessageId: _newClientId('message'),
            imageAssetIds: List<String>.unmodifiable(imageAssetIds),
          );
    return _sendText(operation);
  }

  Future<bool> _sendText(_TextRetry operation) async {
    final threadId = _threadId;
    if (threadId == null || isBusy || _disposed) {
      return false;
    }
    final fence = _captureOperationFence(threadId: threadId);
    _retry = null;
    _errorMessage = null;
    _setReplyPending(true);
    if (!operation.restored &&
        operation.imageAssetIds.isEmpty &&
        client is AgentStreamingTextClient) {
      final streamingClient = client as AgentStreamingTextClient;
      final committedUserMessageID = operation.committedUserMessageID;
      final reuseCommittedUser =
          committedUserMessageID != null &&
          _messages.any(
            (message) =>
                message.id == committedUserMessageID &&
                message.role == AgentMessageRole.user,
          );
      final localUserID = reuseCommittedUser
          ? committedUserMessageID
          : 'pending-user-${operation.clientMessageId}';
      final localAssistantID = 'pending-assistant-${operation.clientMessageId}';
      _appendMessages([
        if (!reuseCommittedUser)
          AgentMessage(
            id: localUserID,
            role: AgentMessageRole.user,
            text: operation.text,
          ),
        AgentMessage(
          id: localAssistantID,
          role: AgentMessageRole.assistant,
          text: '',
          isStreaming: true,
        ),
      ]);
      notifyListeners();
      unawaited(
        _consumeTextStream(
          streamingClient.sendTextStream(
            threadId: threadId,
            text: operation.text,
            clientMessageId: operation.clientMessageId,
          ),
          operation: operation,
          fence: fence,
          localUserID: localUserID,
          localAssistantID: localAssistantID,
        ),
      );
      return true;
    }
    try {
      final exchange = await client.sendText(
        threadId: threadId,
        text: operation.text,
        clientMessageId: operation.clientMessageId,
        imageAssetIds: operation.imageAssetIds,
      );
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      _appendMessages([exchange.userMessage, ?exchange.assistantMessage]);
      notifyListeners();
      unawaited(_hydrateMessageImageContents(fence));
      await _refreshAuthoritativeThreadPage(
        client,
        fence: fence,
        failureMessage: '消息已发送，但对话顺序暂时无法刷新。请重试。',
      );
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _retry = _canRetry(error) ? operation : null;
        _errorMessage =
            error is AgentClientException &&
                error.kind == AgentClientFailureKind.runFailed &&
                !error.retryable
            ? '这次 Agent 运行未能完成，服务端不允许重试。'
            : error is AgentClientException && !error.retryable
            ? '消息未发送，请检查内容后再试。'
            : '消息没有发送成功，可以重试。';
      }
      return false;
    } finally {
      if (_isOperationCurrent(fence)) {
        _setReplyPending(false);
      }
    }
  }

  Future<void> _consumeTextStream(
    Stream<AgentTextStreamEvent> stream, {
    required _TextRetry operation,
    required _ConversationOperationFence fence,
    required String localUserID,
    required String localAssistantID,
  }) async {
    var assistantID = localAssistantID;
    var assistantText = '';
    String? completedAssistantMessageID;
    final pending = StringBuffer();
    Timer? frameTimer;

    void replaceMessage(String id, AgentMessage replacement) {
      final index = _messages.indexWhere((message) => message.id == id);
      if (index < 0) {
        return;
      }
      final next = List<AgentMessage>.from(_messages);
      next[index] = replacement;
      _messages = next;
    }

    void flushDelta() {
      frameTimer = null;
      if (!_isOperationCurrent(fence) || pending.isEmpty) {
        pending.clear();
        return;
      }
      assistantText += pending.toString();
      pending.clear();
      final current = _messages
          .where((message) => message.id == assistantID)
          .firstOrNull;
      if (current != null) {
        replaceMessage(
          assistantID,
          current.copyWith(text: assistantText, isStreaming: true),
        );
        notifyListeners();
      }
    }

    try {
      await for (final event in stream) {
        if (!_isOperationCurrent(fence)) {
          break;
        }
        switch (event) {
          case AgentInputCommitted(:final userMessage):
            operation.committedUserMessageID = userMessage.id;
            final pendingUser = _messages
                .where((message) => message.id == localUserID)
                .firstOrNull;
            if (pendingUser != null) {
              replaceMessage(localUserID, userMessage);
            }
          case AgentToolStepEvent():
            break;
          case AgentAssistantOutputStarted(:final outputId):
            final current = _messages
                .where((message) => message.id == assistantID)
                .firstOrNull;
            if (current != null && assistantID != outputId) {
              replaceMessage(
                assistantID,
                current.copyWith(
                  id: outputId,
                  text: '',
                  isStreaming: true,
                  hasFailed: false,
                ),
              );
              assistantID = outputId;
              assistantText = '';
            }
            await _startAssistantStream(assistantID);
          case AgentAssistantOutputDelta(:final delta):
            pending.write(delta);
            _appendAssistantStream(assistantID, delta);
            frameTimer ??= Timer(const Duration(milliseconds: 16), flushDelta);
          case AgentAssistantOutputCompleted(:final outputId, :final text):
            frameTimer?.cancel();
            flushDelta();
            if (assistantID != outputId || assistantText != text) {
              throw const AgentClientException(
                kind: AgentClientFailureKind.invalidResponse,
                retryable: true,
              );
            }
            final current = _messages
                .where((message) => message.id == assistantID)
                .firstOrNull;
            if (current != null) {
              final completed = current.copyWith(
                id: outputId,
                text: text,
                isStreaming: false,
                hasFailed: false,
              );
              replaceMessage(assistantID, completed);
              _completeAssistantStream(assistantID, completed);
            }
            completedAssistantMessageID = outputId;
          case AgentRunCompleted(:final assistantMessageId):
            if (completedAssistantMessageID != null &&
                assistantMessageId != completedAssistantMessageID) {
              throw const AgentClientException(
                kind: AgentClientFailureKind.invalidResponse,
                retryable: true,
              );
            }
            if (completedAssistantMessageID == null) {
              _messages = <AgentMessage>[
                for (final message in _messages)
                  if (message.id != assistantID) message,
              ];
            }
            completedAssistantMessageID = assistantMessageId;
          case AgentRunFailed(:final kind, :final retryable):
            throw AgentClientException(
              kind: AgentClientFailureKind.runFailed,
              errorCode: kind,
              retryable: retryable,
            );
        }
        notifyListeners();
      }
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _retry = null;
      _errorMessage = null;
      await _refreshAuthoritativeThreadPage(
        client,
        fence: fence,
        failureMessage: '消息已发送，但对话顺序暂时无法刷新。请重试。',
      );
      await _refreshAuthoritativeMessagePage(
        client,
        fence: fence,
        requiredMessageID: completedAssistantMessageID,
        failureMessage: '回复已完成，但确认入口暂时无法读取。请重试刷新。',
      );
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _failAssistantStream(assistantID);
        frameTimer?.cancel();
        flushDelta();
        final current = _messages
            .where((message) => message.id == assistantID)
            .firstOrNull;
        if (current != null) {
          replaceMessage(
            assistantID,
            current.copyWith(
              text: assistantText,
              isStreaming: false,
              hasFailed: true,
            ),
          );
        }
        _retry = _canRetry(error) ? operation : null;
        _errorMessage = error is AgentClientException && !error.retryable
            ? '这次 Agent 运行未能完成，服务端不允许重试。'
            : '回复中断了，可以重试。';
      }
    } finally {
      frameTimer?.cancel();
      if (_isOperationCurrent(fence)) {
        _setReplyPending(false);
      }
    }
  }

  Future<void> _startAssistantStream(String transientMessageId) async {
    final callback = onAssistantStreamStarted;
    if (callback == null) {
      return;
    }
    try {
      await callback(transientMessageId);
    } catch (_) {
      _failAssistantStream(transientMessageId);
    }
  }

  void _appendAssistantStream(String transientMessageId, String delta) {
    try {
      onAssistantStreamDelta?.call(transientMessageId, delta);
    } catch (_) {
      _failAssistantStream(transientMessageId);
    }
  }

  void _completeAssistantStream(
    String transientMessageId,
    AgentMessage message,
  ) {
    try {
      onAssistantStreamCompleted?.call(transientMessageId, message);
    } catch (_) {
      _failAssistantStream(transientMessageId);
    }
  }

  void _failAssistantStream(String transientMessageId) {
    try {
      onAssistantStreamFailed?.call(transientMessageId);
    } catch (_) {
      // Speech presentation cannot change the authoritative text Run result.
    }
  }

  Future<void> retryThreadHistory() async {
    if (_disposed || isBusy) {
      return;
    }
    switch (_threadHistoryRecovery) {
      case _ThreadHistoryRecovery.create:
        await createThread();
        return;
      case _ThreadHistoryRecovery.refreshThreadPage:
      case _ThreadHistoryRecovery.refreshMessagePage:
      case _ThreadHistoryRecovery.refreshThreadAndMessagePages:
        await _retryThreadHistoryRefresh();
        return;
      case null:
        return;
    }
  }

  Future<void> refreshThreadHistory() async {
    final threadId = _threadId;
    if (_disposed || !_initialized || isBusy || threadId == null) {
      return;
    }
    final fence = _captureOperationFence(threadId: threadId);
    try {
      final page = await client.listThreads();
      _validateThreadPage(page);
      if (!_isOperationCurrent(fence)) {
        return;
      }
      final refreshedIds = <String>{
        for (final thread in page.threads) thread.id,
      };
      final boundary = page.threads.lastOrNull;
      final preservedOlderThreads = boundary == null
          ? const <AgentThreadSummary>[]
          : <AgentThreadSummary>[
              for (final thread in _threads)
                if (!refreshedIds.contains(thread.id) &&
                    _threadSortsAfter(thread, boundary))
                  thread,
            ];
      final previousCursor = _nextThreadCursor;
      _threads = <AgentThreadSummary>[
        ...page.threads,
        ...preservedOlderThreads,
      ];
      _nextThreadCursor = preservedOlderThreads.isEmpty
          ? page.nextCursor
          : previousCursor;
      _currentThreadSummary =
          page.threads.where((thread) => thread.id == threadId).firstOrNull ??
          preservedOlderThreads
              .where((thread) => thread.id == threadId)
              .firstOrNull ??
          _currentThreadSummary;
      _resolveThreadPageRefresh();
      notifyListeners();
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _requireThreadPageRefresh();
        _threadHistoryErrorMessage = '对话列表暂时无法刷新，请稍后再试。';
        notifyListeners();
      }
    }
  }

  Future<void> _retryThreadHistoryRefresh() async {
    final threadId = _threadId;
    if (threadId == null || _disposed) {
      return;
    }
    final historyClient = client;
    final fence = _captureOperationFence(threadId: threadId);
    final recovery = _threadHistoryRecovery;
    final refreshThreadPage = _requiresThreadPageRefresh(recovery);
    final refreshMessagePage = _requiresMessagePageRefresh(recovery);
    _setBusy(true);
    try {
      if (refreshThreadPage) {
        await _refreshAuthoritativeThreadPage(
          historyClient,
          fence: fence,
          failureMessage: '对话顺序暂时无法刷新，请稍后再试。',
        );
      }
      if (refreshMessagePage) {
        await _refreshAuthoritativeMessagePage(
          historyClient,
          fence: fence,
          failureMessage: '对话消息暂时无法刷新，请稍后再试。',
        );
      }
    } finally {
      if (_isOperationCurrent(fence)) {
        _setBusy(false);
      }
    }
  }

  void _requireThreadPageRefresh() {
    _threadHistoryRecovery = switch (_threadHistoryRecovery) {
      _ThreadHistoryRecovery.refreshMessagePage =>
        _ThreadHistoryRecovery.refreshThreadAndMessagePages,
      _ThreadHistoryRecovery.refreshThreadAndMessagePages =>
        _ThreadHistoryRecovery.refreshThreadAndMessagePages,
      _ThreadHistoryRecovery.create => _threadHistoryRecovery,
      _ => _ThreadHistoryRecovery.refreshThreadPage,
    };
  }

  void _requireMessagePageRefresh() {
    _threadHistoryRecovery = switch (_threadHistoryRecovery) {
      _ThreadHistoryRecovery.refreshThreadPage =>
        _ThreadHistoryRecovery.refreshThreadAndMessagePages,
      _ThreadHistoryRecovery.refreshThreadAndMessagePages =>
        _ThreadHistoryRecovery.refreshThreadAndMessagePages,
      _ThreadHistoryRecovery.create => _threadHistoryRecovery,
      _ => _ThreadHistoryRecovery.refreshMessagePage,
    };
  }

  void _resolveThreadPageRefresh() {
    switch (_threadHistoryRecovery) {
      case _ThreadHistoryRecovery.refreshThreadPage:
        _threadHistoryRecovery = null;
        _threadHistoryErrorMessage = null;
        break;
      case _ThreadHistoryRecovery.refreshThreadAndMessagePages:
        _threadHistoryRecovery = _ThreadHistoryRecovery.refreshMessagePage;
        break;
      case null:
        _threadHistoryErrorMessage = null;
        break;
      case _ThreadHistoryRecovery.create:
      case _ThreadHistoryRecovery.refreshMessagePage:
        break;
    }
  }

  void _resolveMessagePageRefresh() {
    switch (_threadHistoryRecovery) {
      case _ThreadHistoryRecovery.refreshMessagePage:
        _threadHistoryRecovery = null;
        _threadHistoryErrorMessage = null;
        break;
      case _ThreadHistoryRecovery.refreshThreadAndMessagePages:
        _threadHistoryRecovery = _ThreadHistoryRecovery.refreshThreadPage;
        break;
      case null:
        _threadHistoryErrorMessage = null;
        break;
      case _ThreadHistoryRecovery.create:
      case _ThreadHistoryRecovery.refreshThreadPage:
        break;
    }
  }

  Future<bool> _refreshAuthoritativeThreadPage(
    AgentClient historyClient, {
    required _ConversationOperationFence fence,
    required String failureMessage,
  }) async {
    try {
      final page = await historyClient.listThreads();
      _validateThreadPage(page);
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      final threadId = _threadId;
      final current = page.threads
          .where((thread) => thread.id == threadId)
          .firstOrNull;
      if (threadId == null || current == null) {
        throw StateError(
          'The authoritative Thread page omitted the selected Thread.',
        );
      }
      _threads = List<AgentThreadSummary>.from(page.threads);
      _nextThreadCursor = page.nextCursor;
      _currentThreadSummary = current;
      _resolveThreadPageRefresh();
      notifyListeners();
      return true;
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _requireThreadPageRefresh();
        _threadHistoryErrorMessage = failureMessage;
        notifyListeners();
      }
      return false;
    }
  }

  Future<bool> _refreshAuthoritativeMessagePage(
    AgentClient historyClient, {
    required _ConversationOperationFence fence,
    required String failureMessage,
    String? requiredMessageID,
  }) async {
    try {
      final threadId = _threadId;
      if (threadId == null) {
        throw StateError(
          'No selected Thread is available for Message refresh.',
        );
      }
      final page = await historyClient.listMessages(threadId: threadId);
      _validateMessagePage(page);
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      if (requiredMessageID != null &&
          !page.messages.any((message) => message.id == requiredMessageID)) {
        throw StateError(
          'The authoritative Message page omitted the completed response.',
        );
      }
      _messages = List<AgentMessage>.from(page.messages);
      _nextMessageCursor = page.nextCursor;
      _resolveMessagePageRefresh();
      notifyListeners();
      return true;
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _requireMessagePageRefresh();
        _threadHistoryErrorMessage = failureMessage;
        notifyListeners();
      }
      return false;
    }
  }

  Future<void> retryLastOperation() async {
    final retry = _retry;
    if (retry == null || isBusy || _disposed) {
      return;
    }
    switch (retry) {
      case _RestoreRetry():
        await initialize();
      case final _TextRetry operation:
        await _sendText(operation);
    }
  }

  /// Commits durable voice Messages returned by the Composer workflow.
  void commitComposerMessages(Iterable<AgentMessage> values) {
    if (_disposed || _threadId == null) {
      return;
    }
    _appendMessages(values);
    final current = _currentThreadSummary;
    if (current != null) {
      final now = DateTime.now().toUtc();
      final updated = AgentThreadSummary(
        id: current.id,
        title: current.title,
        createdAt: current.createdAt,
        updatedAt: now.isBefore(current.updatedAt) ? current.updatedAt : now,
      );
      _currentThreadSummary = updated;
      _mergeThreadSummary(updated, placeFirst: true);
    }
    notifyListeners();
  }

  void changeComposerStreamMessage(
    String? previousMessageId,
    AgentMessage message,
  ) {
    if (_disposed || _threadId == null) {
      return;
    }
    final messages = List<AgentMessage>.from(_messages);
    final index = previousMessageId == null
        ? -1
        : messages.indexWhere((item) => item.id == previousMessageId);
    if (index < 0) {
      if (!messages.any((item) => item.id == message.id)) {
        messages.add(message);
      }
    } else {
      messages[index] = message;
    }
    _messages = messages;
    notifyListeners();
  }

  void markMessageAudioDeleted(
    String messageId,
    AgentMessageAudio deletedAudio,
  ) {
    if (_disposed) {
      return;
    }
    _messages = <AgentMessage>[
      for (final message in _messages)
        if (message.id == messageId)
          message.copyWith(audio: deletedAudio)
        else
          message,
    ];
    notifyListeners();
  }

  Future<bool> prepareToLeaveConversation() async => !_disposed && !isBusy;

  Future<void> clearPrivateState() async {
    _epoch++;
    _initializationFuture = null;
    _threadId = null;
    _currentThreadSummary = null;
    _threads = const <AgentThreadSummary>[];
    _nextThreadCursor = null;
    _nextMessageCursor = null;
    _threadHistoryErrorMessage = null;
    _threadHistoryRecovery = null;
    _draftThreadRecoveryGeneration = 0;
    _loadingMoreThreads = false;
    _loadingEarlierMessages = false;
    _threadTransitionGeneration++;
    _threadTransitionInFlight = false;
    _messages = const <AgentMessage>[];
    _errorMessage = null;
    _retry = null;
    _initialized = false;
    _busy = false;
    _replyPending = false;
    if (!_disposed) {
      notifyListeners();
    }
    final cleanup = Future<void>.sync(client.clearAccountState);
    _accountCleanupFuture = cleanup;
    try {
      await cleanup;
    } finally {
      if (identical(_accountCleanupFuture, cleanup)) {
        _accountCleanupFuture = null;
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _epoch++;
    _threadTransitionGeneration++;
    _threadTransitionInFlight = false;
    _initializationFuture = null;
    super.dispose();
  }

  Future<void> _ensureInitialized() async {
    if (!_initialized) {
      await initialize();
    }
  }

  void _resetSelectedThreadPresentation() {
    _threadId = null;
    _currentThreadSummary = null;
    _nextMessageCursor = null;
    _messages = const <AgentMessage>[];
    _retry = null;
    _errorMessage = null;
  }

  AgentThreadSummary? _threadSummaryFromSnapshot(AgentThreadSnapshot snapshot) {
    final createdAt = snapshot.createdAt;
    final updatedAt = snapshot.updatedAt;
    if (createdAt == null || updatedAt == null) {
      return null;
    }
    return AgentThreadSummary(
      id: snapshot.threadId,
      title: snapshot.title,
      createdAt: createdAt,
      updatedAt: updatedAt,
    );
  }

  void _mergeThreadSummary(
    AgentThreadSummary summary, {
    required bool placeFirst,
  }) {
    final remaining = <AgentThreadSummary>[
      for (final thread in _threads)
        if (thread.id != summary.id) thread,
    ];
    _threads = placeFirst
        ? <AgentThreadSummary>[summary, ...remaining]
        : <AgentThreadSummary>[
            for (final thread in _threads)
              if (thread.id == summary.id) summary else thread,
            if (!_threads.any((thread) => thread.id == summary.id)) summary,
          ];
  }

  void _appendMessages(Iterable<AgentMessage> values) {
    final messages = List<AgentMessage>.from(_messages);
    final ids = {for (final message in messages) message.id};
    for (final message in values) {
      if (ids.add(message.id)) {
        messages.add(message);
      }
    }
    _messages = messages;
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _epoch;

  _ConversationOperationFence _captureOperationFence({String? threadId}) =>
      _ConversationOperationFence(epoch: _epoch, threadId: threadId);

  bool _isOperationCurrent(_ConversationOperationFence fence) =>
      _isCurrent(fence.epoch) &&
      (fence.threadId == null || fence.threadId == _threadId);

  void _setBusy(bool value) {
    if (_disposed) {
      return;
    }
    _busy = value;
    notifyListeners();
  }

  void _setReplyPending(bool value) {
    if (_disposed) {
      return;
    }
    _replyPending = value;
    notifyListeners();
  }

  int? _beginThreadTransition() {
    if (_disposed || _threadTransitionInFlight) {
      return null;
    }
    _threadTransitionInFlight = true;
    final generation = ++_threadTransitionGeneration;
    notifyListeners();
    return generation;
  }

  void _finishThreadTransition(int generation) {
    if (_disposed ||
        !_threadTransitionInFlight ||
        generation != _threadTransitionGeneration) {
      return;
    }
    _threadTransitionInFlight = false;
    notifyListeners();
  }

  void _validateThreadSnapshot(AgentThreadSnapshot snapshot) {
    final messageIds = <String>{};
    var previousSequence = 0;
    for (final message in snapshot.messages) {
      final sequence = message.sequence;
      if (message.id.trim().isEmpty ||
          !messageIds.add(message.id) ||
          (sequence != null &&
              (sequence < 1 || sequence <= previousSequence))) {
        throw StateError('Invalid Agent Thread snapshot.');
      }
      if (sequence != null) {
        previousSequence = sequence;
      }
    }
    final recovery = snapshot.textRecovery;
    final createdAt = snapshot.createdAt;
    final updatedAt = snapshot.updatedAt;
    if (snapshot.threadId.trim().isEmpty ||
        ((createdAt == null) != (updatedAt == null)) ||
        (createdAt != null && updatedAt!.isBefore(createdAt)) ||
        (snapshot.nextMessageCursor != null &&
            (snapshot.nextMessageCursor!.isEmpty ||
                snapshot.nextMessageCursor!.runes.length > 1024)) ||
        (recovery != null && !_validTextRecovery(snapshot, recovery))) {
      throw StateError('Invalid Agent Thread snapshot.');
    }
  }

  bool _validTextRecovery(
    AgentThreadSnapshot snapshot,
    AgentTextRecovery recovery,
  ) {
    final message = snapshot.messages.lastOrNull;
    final run = recovery.latestRun;
    return recovery.text.trim().isNotEmpty &&
        recovery.clientMessageId.trim().isNotEmpty &&
        message != null &&
        message.role == AgentMessageRole.user &&
        message.modality != AgentMessageModality.voice &&
        message.text == recovery.text &&
        message.clientMessageId == recovery.clientMessageId &&
        (run == null ||
            (run.threadId == snapshot.threadId &&
                run.inputMessageId == message.id &&
                run.status != AgentRunStatus.completed));
  }

  void _validateThreadSummary(AgentThreadSummary summary) {
    if (summary.id.trim().isEmpty ||
        summary.updatedAt.isBefore(summary.createdAt)) {
      throw StateError('Invalid Agent Thread summary.');
    }
  }

  void _validateThreadPage(AgentThreadPage page) {
    if (page.threads.length > 100 ||
        (page.nextCursor != null &&
            (page.nextCursor!.isEmpty ||
                page.nextCursor!.runes.length > 1024))) {
      throw StateError('Invalid Agent Thread page.');
    }
    final ids = <String>{};
    AgentThreadSummary? previous;
    for (final thread in page.threads) {
      _validateThreadSummary(thread);
      if (!ids.add(thread.id) ||
          (previous != null &&
              (thread.updatedAt.isAfter(previous.updatedAt) ||
                  (thread.updatedAt == previous.updatedAt &&
                      previous.id.compareTo(thread.id) <= 0)))) {
        throw StateError('Invalid Agent Thread page ordering.');
      }
      previous = thread;
    }
  }

  void _validateMessagePage(AgentMessagePage page) {
    if (page.messages.length > 100 ||
        (page.nextCursor != null &&
            (page.nextCursor!.isEmpty ||
                page.nextCursor!.runes.length > 1024))) {
      throw StateError('Invalid Agent Message page.');
    }
    final ids = <String>{};
    var previousSequence = 0;
    for (final message in page.messages) {
      final sequence = message.sequence;
      if (message.id.trim().isEmpty ||
          !ids.add(message.id) ||
          sequence == null ||
          sequence < 1 ||
          sequence <= previousSequence) {
        throw StateError('Invalid Agent Message page ordering.');
      }
      previousSequence = sequence;
    }
  }

  bool _threadSortsAfter(
    AgentThreadSummary candidate,
    AgentThreadSummary boundary,
  ) {
    return candidate.updatedAt.isBefore(boundary.updatedAt) ||
        (candidate.updatedAt == boundary.updatedAt &&
            candidate.id.compareTo(boundary.id) < 0);
  }

  String _newClientId(String scope) {
    final value = _clientIdFactory(scope);
    if (value.isEmpty) {
      throw StateError('Agent client identity must not be empty.');
    }
    return value;
  }

  bool _canRetry(Object error) {
    return error is! AgentClientException || error.retryable;
  }
}

final class _ConversationOperationFence {
  const _ConversationOperationFence({required this.epoch, this.threadId});

  final int epoch;
  final String? threadId;
}

sealed class _ConversationRetry {
  const _ConversationRetry();
}

enum _ThreadHistoryRecovery {
  create,
  refreshThreadPage,
  refreshMessagePage,
  refreshThreadAndMessagePages,
}

bool _isPageRefreshRecovery(_ThreadHistoryRecovery? recovery) =>
    _requiresThreadPageRefresh(recovery) ||
    _requiresMessagePageRefresh(recovery);

bool _requiresThreadPageRefresh(_ThreadHistoryRecovery? recovery) =>
    recovery == _ThreadHistoryRecovery.refreshThreadPage ||
    recovery == _ThreadHistoryRecovery.refreshThreadAndMessagePages;

bool _requiresMessagePageRefresh(_ThreadHistoryRecovery? recovery) =>
    recovery == _ThreadHistoryRecovery.refreshMessagePage ||
    recovery == _ThreadHistoryRecovery.refreshThreadAndMessagePages;

final class _RestoreRetry extends _ConversationRetry {
  const _RestoreRetry();
}

final class _TextRetry extends _ConversationRetry {
  _TextRetry({
    required this.text,
    required this.clientMessageId,
    this.imageAssetIds = const <String>[],
    this.committedUserMessageID,
    this.restored = false,
  });

  final String text;
  final String clientMessageId;
  final List<String> imageAssetIds;
  String? committedUserMessageID;
  final bool restored;
}

final Random _clientIdRandom = Random.secure();

String _createSecureClientId(String scope) {
  final buffer = StringBuffer(scope)..write('_');
  for (var index = 0; index < 16; index++) {
    buffer.write(
      _clientIdRandom.nextInt(256).toRadixString(16).padLeft(2, '0'),
    );
  }
  return buffer.toString();
}
