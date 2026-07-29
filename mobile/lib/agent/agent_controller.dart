import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_client.dart';
import 'package:speakup/agent/agent_voice_controller.dart';
import 'package:speakup/agent/agent_voice_recording.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

typedef AgentClientIdFactory = String Function(String scope);

final class AgentController extends ChangeNotifier with WidgetsBindingObserver {
  AgentController({
    required this.client,
    PracticeClient? practiceClient,
    PracticeRecorder? recorder,
    this.mediaClient,
    this.audioPlayer,
    AgentVoiceClient? voiceClient,
    AgentVoiceRecorder? voiceRecorder,
    AgentVoiceAudioPlayer? voiceAudioPlayer,
    AgentClientIdFactory? clientIdFactory,
    Duration recordingLimit = const Duration(seconds: 58),
  }) : practiceClient = practiceClient ?? LegacyAgentPracticeClient(client),
       recorder = recorder ?? FakePracticeRecorder(),
       _clientIdFactory = clientIdFactory ?? _createSecureClientId,
       _recordingLimit = recordingLimit {
    if ((mediaClient == null) != (audioPlayer == null)) {
      throw ArgumentError(
        'Practice media client and audio player must be injected together.',
      );
    }
    final AgentVoiceClient? supportedVoiceClient = switch (client) {
      final AgentVoiceClient supported => supported,
      _ => null,
    };
    final resolvedVoiceClient = voiceClient ?? supportedVoiceClient;
    if (resolvedVoiceClient != null) {
      if ((voiceRecorder == null) != (voiceAudioPlayer == null)) {
        throw ArgumentError(
          'Agent voice recorder and audio player must be injected together.',
        );
      }
      _voiceController = AgentVoiceController(
        client: resolvedVoiceClient,
        recorder: voiceRecorder ?? FakeAgentVoiceRecorder(),
        audioPlayer: voiceAudioPlayer ?? FakeAgentVoiceAudioPlayer(),
        onMessagesCommitted: _commitVoiceMessages,
        onMessageAudioDeleted: _markVoiceMessageAudioDeleted,
        idFactory: _newClientId,
      )..addListener(_handleVoiceState);
    }
    if (recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 60)) {
      throw ArgumentError.value(
        recordingLimit,
        'recordingLimit',
        'must be positive and no longer than the server 60-second limit',
      );
    }
    _mediaCompletionSubscription = audioPlayer?.onComplete.listen((_) {
      _handleMediaCompletion();
    });
    if (mediaClient != null) {
      WidgetsBinding.instance.addObserver(this);
    }
  }

  final AgentClient client;
  final PracticeClient? practiceClient;
  final PracticeRecorder recorder;
  final PracticeMediaClient? mediaClient;
  final PracticeAudioPlayer? audioPlayer;
  final AgentClientIdFactory _clientIdFactory;
  final Duration _recordingLimit;
  AgentVoiceController? _voiceController;
  bool _relayedVoiceWorkflowActive = false;

  String? _threadId;
  AgentThreadSummary? _currentThreadSummary;
  List<AgentThreadSummary> _threads = const <AgentThreadSummary>[];
  String? _nextThreadCursor;
  String? _nextMessageCursor;
  String? _threadHistoryErrorMessage;
  _ThreadHistoryRecovery? _threadHistoryRecovery;
  String? _pendingFocusThreadId;
  int _draftThreadRecoveryGeneration = 0;
  bool _loadingMoreThreads = false;
  bool _loadingEarlierMessages = false;
  bool _threadTransitionInFlight = false;
  int _threadTransitionGeneration = 0;
  String? _practiceSessionId;
  int? _practiceSessionVersion;
  String? _endPracticeClientId;
  PracticeQuestion? _currentQuestion;
  TranscriptionCandidate? _candidate;
  String? _activeConfirmationId;
  String? _activeTextAnswer;
  AgentMatter? _activeMatter;
  List<AgentMessage> _messages = const <AgentMessage>[];
  List<AgentMessage> _practiceMessages = const <AgentMessage>[];
  PracticeRecordingState _recordingState = PracticeRecordingState.idle;
  AgentReview? _review;
  String? _errorMessage;
  _AgentRetry? _retry;
  int _completedTurns = 0;
  int _turnLimit = 0;
  bool _sessionCompleted = false;
  int _epoch = 0;
  int _practiceGeneration = 0;
  bool _initialized = false;
  bool _busy = false;
  bool _disposed = false;
  Future<void>? _initializationFuture;
  Future<void>? _accountCleanupFuture;
  Future<void>? _recorderStartFuture;
  Future<void>? _stopRecordingFuture;
  Timer? _recordingLimitTimer;
  StreamSubscription<void>? _mediaCompletionSubscription;
  List<PracticeRecordingReference> _recordings =
      const <PracticeRecordingReference>[];
  String? _playingMediaKey;
  String? _loadingMediaKey;
  String? _deletingAudioAssetId;
  String? _mediaErrorMessage;
  int _mediaGeneration = 0;
  Future<void>? _mediaOperation;

  String? get threadId => _threadId;
  AgentThreadSummary? get currentThreadSummary => _currentThreadSummary;
  List<AgentThreadSummary> get threads =>
      List<AgentThreadSummary>.unmodifiable(_threads);
  bool get isInitialized => _initialized;
  bool get supportsThreadHistory => client is AgentThreadHistoryClient;
  bool get isThreadTransitionInFlight => _threadTransitionInFlight;
  bool get hasMoreThreads => _nextThreadCursor != null;
  bool get isLoadingMoreThreads => _loadingMoreThreads;
  String? get threadHistoryErrorMessage => _threadHistoryErrorMessage;
  bool get canRetryThreadHistory => _threadHistoryRecovery != null;
  bool get hasPendingThreadCreationRecovery =>
      _pendingFocusThreadId != null ||
      _threadHistoryRecovery == _ThreadHistoryRecovery.create;
  int get draftThreadRecoveryGeneration => _draftThreadRecoveryGeneration;
  bool get hasEarlierMessages => _nextMessageCursor != null;
  bool get isLoadingEarlierMessages => _loadingEarlierMessages;
  String? get practiceSessionId => _practiceSessionId;
  int? get practiceSessionVersion => _practiceSessionVersion;
  String? get questionId => _currentQuestion?.id;
  String? get candidateId => _candidate?.id;
  AgentMatter? get activeMatter => _activeMatter;
  AgentScene? get scene => _activeMatter?.scene;
  List<AgentMessage> get messages => List.unmodifiable(_messages);
  List<AgentMessage> get practiceMessages =>
      List.unmodifiable(_practiceMessages);
  AgentVoiceController? get voiceController => _voiceController;
  bool get supportsAgentVoice => _voiceController != null;
  PracticeRecordingState get recordingState => _recordingState;
  String? get transcript => _candidate?.text;
  AgentReview? get review => _review;
  String? get errorMessage => _errorMessage;
  int get completedTurns => _completedTurns;
  int get turnLimit => _turnLimit;
  bool get isBusy =>
      _busy ||
      _practiceRequestInFlight ||
      _threadTransitionInFlight ||
      (_voiceController?.hasActiveWorkflow ?? false);
  bool get canRetry => _retry != null;
  bool get supportsPracticeFlow => practiceClient != null;
  bool get supportsPracticeMedia => mediaClient != null && audioPlayer != null;
  List<PracticeRecordingReference> get recordings =>
      List<PracticeRecordingReference>.unmodifiable(_recordings);
  String? get mediaErrorMessage => _mediaErrorMessage;
  bool get isQuestionAudioLoading =>
      _currentQuestion != null &&
      _loadingMediaKey == _questionMediaKey(_currentQuestion!.id);
  bool get isQuestionAudioPlaying =>
      _currentQuestion != null &&
      _playingMediaKey == _questionMediaKey(_currentQuestion!.id);
  bool get canPlayQuestionAudio =>
      supportsPracticeMedia && _currentQuestion?.speechPath != null;
  bool get canUsePracticeAudio =>
      supportsPracticeMedia &&
      !_disposed &&
      !_busy &&
      switch (_recordingState) {
        PracticeRecordingState.idle ||
        PracticeRecordingState.awaitingConfirmation ||
        PracticeRecordingState.reviewFailed ||
        PracticeRecordingState.completed => true,
        PracticeRecordingState.starting ||
        PracticeRecordingState.recording ||
        PracticeRecordingState.transcribing ||
        PracticeRecordingState.submitting => false,
      };
  bool isRecordingAudioLoading(String audioAssetId) =>
      _loadingMediaKey == _recordingMediaKey(audioAssetId);
  bool isRecordingAudioPlaying(String audioAssetId) =>
      _playingMediaKey == _recordingMediaKey(audioAssetId);
  bool isRecordingDeleting(String audioAssetId) =>
      _deletingAudioAssetId == audioAssetId;

  bool get canSelectScene {
    return !_disposed &&
        supportsPracticeFlow &&
        _threadId != null &&
        !_busy &&
        switch (_recordingState) {
          PracticeRecordingState.idle ||
          PracticeRecordingState.reviewFailed ||
          PracticeRecordingState.completed => true,
          _ => false,
        };
  }

  bool get hasActivePractice {
    return _practiceSessionId != null &&
        _activeMatter != null &&
        !_isSessionCompleted;
  }

  bool get _isSessionCompleted => _sessionCompleted;

  bool get _practiceRequestInFlight {
    return _recordingState == PracticeRecordingState.transcribing ||
        _recordingState == PracticeRecordingState.submitting;
  }

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
      // A previous process can be terminated before its normal disposal path.
      // Purge only Agent voice scratch media before restoring this account;
      // durable Messages and candidates remain server-owned.
      await _voiceController?.clearPrivateState(clearClient: false);
      if (!_isOperationCurrent(fence)) {
        return;
      }
      if (client case final AgentThreadHistoryClient historyClient) {
        await _restoreThreadHistory(historyClient, fence);
      } else {
        final thread = await client.restoreThread();
        await _applyThreadSnapshot(thread, fence: fence);
      }
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
    AgentThreadHistoryClient historyClient,
    _AgentOperationFence fence,
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

    final focused = await historyClient.getFocusedThread();
    if (!_isOperationCurrent(fence)) {
      return;
    }
    if (focused == null) {
      await _voiceController?.bindThread(null);
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _resetSelectedThreadPresentation();
      _initialized = true;
      return;
    }
    await _applyThreadSnapshot(
      focused,
      fence: fence,
      summary: _threads
          .where((thread) => thread.id == focused.threadId)
          .firstOrNull,
    );
  }

  Future<void> _applyThreadSnapshot(
    AgentThreadSnapshot thread, {
    required _AgentOperationFence fence,
    AgentThreadSummary? summary,
  }) async {
    _validateThreadSnapshot(thread);
    if (!_isOperationCurrent(fence)) {
      return;
    }
    if (practiceClient case final LegacyAgentPracticeClient legacy) {
      legacy.seedRestoredThread(thread);
    }
    PracticeSessionSnapshot? practice;
    try {
      practice = await practiceClient?.restorePractice(
        threadId: thread.threadId,
        activeMatter: thread.activeMatter,
      );
    } catch (error) {
      if (!_isPracticeRestoreAmbiguity(error)) {
        rethrow;
      }
      // Multiple completed Sessions are durable history, but no single one
      // is the current Practice. Keep the independently restored Agent
      // context and let Review history present those completed Sessions.
      practice = null;
    }
    if (!_isOperationCurrent(fence)) {
      return;
    }
    if (practice != null) {
      _validatePracticeSnapshot(practice);
    }
    final messages = List<AgentMessage>.from(thread.messages);
    final resolvedSummary =
        summary ?? _threadSummaryFromSnapshot(thread) ?? _currentThreadSummary;
    await _voiceController?.bindThread(thread.threadId, messages: messages);
    if (!_isOperationCurrent(fence)) {
      return;
    }
    _threadId = thread.threadId;
    _currentThreadSummary = resolvedSummary;
    _nextMessageCursor = thread.nextMessageCursor;
    _messages = messages;
    _applyPracticeSnapshot(practice);
    if (practice == null) {
      _activeMatter = thread.activeMatter;
    }
    _initialized = true;
    _applyRestoredTextState(thread);
  }

  void _applyRestoredTextState(AgentThreadSnapshot thread) {
    final textRecovery = thread.textRecovery;
    if (textRecovery != null) {
      _retry = textRecovery.retryable
          ? _TextRetry(
              text: textRecovery.text,
              clientMessageId: textRecovery.clientMessageId,
            )
          : null;
      _errorMessage = textRecovery.retryable
          ? '上次 Agent 运行未能完成，可以继续重试。'
          : '上次 Agent 运行未能完成，服务端不允许重试。';
    } else if (_isSessionCompleted && _review == null) {
      _errorMessage = '练习已完成，正在等待服务端恢复同一次复盘。';
    }
  }

  Future<bool> createThread() async {
    if (client is! AgentThreadHistoryClient || _disposed) {
      return false;
    }
    final pendingFocusThreadId = _pendingFocusThreadId;
    if (pendingFocusThreadId != null) {
      return selectThread(pendingFocusThreadId);
    }
    return _createNewThread();
  }

  /// Creates a new Thread for a caller that requires an isolated workspace.
  ///
  /// Unlike the ordinary conversation entry point, this never consumes a
  /// pending Home draft recovery. The caller must let that recovery settle
  /// before acquiring a dedicated Thread.
  Future<bool> createIndependentThread() async {
    if (hasPendingThreadCreationRecovery) {
      return false;
    }
    return _createNewThread();
  }

  Future<bool> _createNewThread() async {
    if (client is! AgentThreadHistoryClient || _disposed) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    final historyClient = client as AgentThreadHistoryClient;
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return false;
      }
      return await _transitionThread(historyClient, createNew: true);
    } finally {
      _finishThreadTransition(transitionGeneration);
    }
  }

  Future<bool> selectThread(String threadId) async {
    if (client is! AgentThreadHistoryClient ||
        _disposed ||
        threadId.trim().isEmpty) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    final historyClient = client as AgentThreadHistoryClient;
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
    if (client is! AgentThreadHistoryClient ||
        _disposed ||
        currentThreadId == null) {
      return false;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return false;
    }
    final historyClient = client as AgentThreadHistoryClient;
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
    AgentThreadHistoryClient historyClient, {
    String? selectedThreadId,
    required bool createNew,
  }) async {
    final recoveryAtStart = _threadHistoryRecovery;
    final pendingFocusAtStart = _pendingFocusThreadId;
    final recoveringDraftThread =
        _threadId == null &&
        ((createNew && recoveryAtStart == _ThreadHistoryRecovery.create) ||
            (recoveryAtStart == _ThreadHistoryRecovery.focus &&
                selectedThreadId == pendingFocusAtStart));
    _epoch++;
    _practiceGeneration++;
    _cancelRecordingLimit();
    _loadingMoreThreads = false;
    _loadingEarlierMessages = false;
    final fence = _captureOperationFence();
    _retry = null;
    _errorMessage = null;
    if (_threadHistoryRecovery == _ThreadHistoryRecovery.refresh) {
      _threadHistoryRecovery = null;
    }
    if (_threadHistoryRecovery != _ThreadHistoryRecovery.create) {
      _threadHistoryErrorMessage = null;
    }
    AgentThreadSummary? summary;
    var targetThreadId = selectedThreadId;
    _setBusy(true);
    try {
      await _voiceController?.bindThread(null);
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      if (createNew) {
        summary = await historyClient.createThread();
        _validateThreadSummary(summary);
        if (!_isOperationCurrent(fence)) {
          return false;
        }
        _pendingFocusThreadId = summary.id;
        _threadHistoryRecovery = _ThreadHistoryRecovery.focus;
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
      final snapshot = await historyClient.setFocusedThread(
        threadId: targetThreadId,
      );
      if (snapshot.threadId != targetThreadId) {
        throw StateError('Focused Thread identity did not match the request.');
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
      final resolvedPendingFocus = targetThreadId == _pendingFocusThreadId;
      if (resolvedPendingFocus) {
        _pendingFocusThreadId = null;
      }
      if (recoveringDraftThread) {
        _draftThreadRecoveryGeneration++;
      }
      if (resolvedPendingFocus ||
          _threadHistoryRecovery != _ThreadHistoryRecovery.create) {
        if (_threadHistoryRecovery == _ThreadHistoryRecovery.focus) {
          _pendingFocusThreadId = null;
        }
        _threadHistoryRecovery = null;
        _threadHistoryErrorMessage = null;
      }
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        if (createNew &&
            error is AgentClientException &&
            error.errorCode == 'thread_creation_ambiguous') {
          _pendingFocusThreadId = null;
          _threadHistoryRecovery = _ThreadHistoryRecovery.create;
          _threadHistoryErrorMessage = '新对话的创建结果尚未确认。请重试恢复；系统不会重复创建。';
        } else if (targetThreadId != null &&
            (createNew || targetThreadId == _pendingFocusThreadId)) {
          _pendingFocusThreadId = targetThreadId;
          _threadHistoryRecovery = _ThreadHistoryRecovery.focus;
          _threadHistoryErrorMessage = '新对话已创建，但暂时无法打开。重试不会重复创建。';
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

  Future<void> clearFocusedThread() async {
    if (client is! AgentThreadHistoryClient || _disposed) {
      return;
    }
    final accountEpoch = _epoch;
    final transitionGeneration = _beginThreadTransition();
    if (transitionGeneration == null) {
      return;
    }
    final historyClient = client as AgentThreadHistoryClient;
    try {
      await _ensureInitialized();
      if (!_isCurrent(accountEpoch)) {
        return;
      }
      await _clearFocusedThread(historyClient);
    } finally {
      _finishThreadTransition(transitionGeneration);
    }
  }

  Future<void> _clearFocusedThread(
    AgentThreadHistoryClient historyClient,
  ) async {
    _epoch++;
    _practiceGeneration++;
    _cancelRecordingLimit();
    final fence = _captureOperationFence();
    _retry = null;
    _errorMessage = null;
    if (_threadHistoryRecovery == _ThreadHistoryRecovery.refresh) {
      _threadHistoryRecovery = null;
      _threadHistoryErrorMessage = null;
    } else if (_threadHistoryRecovery != _ThreadHistoryRecovery.create) {
      _threadHistoryErrorMessage = null;
    }
    _setBusy(true);
    try {
      await historyClient.clearFocusedThread();
      if (!_isOperationCurrent(fence)) {
        return;
      }
      await _voiceController?.bindThread(null);
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _pendingFocusThreadId = null;
      if (_threadHistoryRecovery == _ThreadHistoryRecovery.focus) {
        _threadHistoryRecovery = null;
        _threadHistoryErrorMessage = null;
      }
      _resetSelectedThreadPresentation();
      _initialized = true;
    } catch (_) {
      if (_isOperationCurrent(fence) &&
          _threadHistoryRecovery != _ThreadHistoryRecovery.create) {
        _threadHistoryErrorMessage = '暂时无法清除当前对话，请稍后再试。';
      }
    } finally {
      if (_isOperationCurrent(fence)) {
        _setBusy(false);
      }
    }
  }

  Future<void> loadMoreThreads() async {
    final cursor = _nextThreadCursor;
    if (client is! AgentThreadHistoryClient ||
        cursor == null ||
        _loadingMoreThreads ||
        _disposed) {
      return;
    }
    final historyClient = client as AgentThreadHistoryClient;
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
    if (client is! AgentThreadHistoryClient ||
        threadId == null ||
        cursor == null ||
        _loadingEarlierMessages ||
        _disposed) {
      return;
    }
    final historyClient = client as AgentThreadHistoryClient;
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
      _voiceController?.syncMessages(_messages);
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

  /// Starts an ordinary Agent voice Message in the focused Thread.
  ///
  /// If the account has no focused Thread, the existing safe Thread creation
  /// path runs first. This microphone is intentionally independent from the
  /// Practice turn recorder.
  Future<void> startAgentVoiceRecording() async {
    final voice = _voiceController;
    if (voice == null || _disposed || voice.hasActiveWorkflow) {
      return;
    }
    await _ensureInitialized();
    if (_disposed) {
      return;
    }
    if (_threadId == null) {
      final created = await createThread();
      if (!created || _threadId == null || _disposed) {
        return;
      }
    }
    final threadId = _threadId!;
    await voice.bindThread(threadId, messages: _messages);
    if (_disposed || _threadId != threadId || voice.threadId != threadId) {
      return;
    }
    await stopPracticeAudio();
    if (_disposed || _threadId != threadId || voice.threadId != threadId) {
      return;
    }
    await voice.startRecording();
  }

  Future<void> selectScene(AgentScene scene) async {
    final accountFence = _captureOperationFence();
    await _ensureInitialized();
    if (!_isOperationCurrent(accountFence) || !canSelectScene) {
      return;
    }
    await _selectScene(
      _SceneRetry(scene: scene, clientOperationId: _newClientId('scene')),
    );
  }

  /// Creates or activates the catalog-backed Matter without starting the
  /// legacy voice-practice route.
  Future<AgentMatter> activateMatterForScenario({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) async {
    final accountFence = _captureOperationFence();
    await _ensureInitialized();
    if (!_isOperationCurrent(accountFence) ||
        _threadId != threadId ||
        scene.id.trim().isEmpty ||
        scene.title.trim().isEmpty ||
        clientOperationId.trim().isEmpty ||
        isBusy ||
        hasActivePractice ||
        _disposed) {
      throw StateError('The Agent context cannot activate this Matter.');
    }
    final current = _activeMatter;
    if (current != null &&
        current.scene.id == 'matter-${current.id}' &&
        current.status == 'active') {
      final adopted = AgentMatter(
        id: current.id,
        scene: scene,
        status: current.status,
        version: current.version,
        createdAt: current.createdAt,
        updatedAt: current.updatedAt,
      );
      _activeMatter = adopted;
      notifyListeners();
      return adopted;
    }
    if (current != null &&
        current.scene.id == scene.id &&
        current.scene.title == scene.title) {
      return current;
    }
    final fence = _captureOperationFence(threadId: threadId);
    final epoch = fence.epoch;
    _setBusy(true);
    try {
      final selection = await client.startScene(
        threadId: threadId,
        scene: scene,
        clientOperationId: clientOperationId,
      );
      if (!_isOperationCurrent(fence)) {
        throw const AgentClientOperationCancelled();
      }
      final matter = selection.activeMatter;
      if (matter.id.trim().isEmpty ||
          matter.scene.id != scene.id ||
          matter.scene.title != scene.title) {
        throw StateError('Matter activation returned a different scenario.');
      }
      _activeMatter = matter;
      notifyListeners();
      return matter;
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  bool prepareActiveMatterForScenario(String matterId) {
    final current = _activeMatter;
    if (current == null ||
        current.id != matterId ||
        current.status != 'active' ||
        _disposed) {
      return false;
    }
    _activeMatter = AgentMatter(
      id: current.id,
      scene: AgentScene(
        id: 'matter-${current.id}',
        title: current.scene.title,
        description: current.scene.description,
      ),
      status: current.status,
      version: current.version,
      createdAt: current.createdAt,
      updatedAt: current.updatedAt,
    );
    notifyListeners();
    return true;
  }

  /// Adopts the exact Session created by the Preparation launch chain.
  ///
  /// The voice client activates the formal Session already created for the
  /// trusted Thread and Matter. This method requires the returned identities
  /// and frozen Turn budget to match; it never guesses a recent Session.
  Future<void> activateCreatedPractice({
    required String threadId,
    required String matterId,
    required String sessionId,
    required int turnLimit,
    required String clientOperationId,
  }) async {
    final accountFence = _captureOperationFence();
    await _ensureInitialized();
    final practice = practiceClient;
    final matter = _activeMatter;
    if (!_isOperationCurrent(accountFence) ||
        practice == null ||
        _threadId != threadId ||
        matter?.id != matterId ||
        turnLimit < 1 ||
        turnLimit > 14 ||
        clientOperationId.trim().isEmpty ||
        isBusy ||
        _disposed) {
      throw StateError('The Agent context changed before voice activation.');
    }
    if (hasActivePractice) {
      if (_practiceSessionId != sessionId || _turnLimit != turnLimit) {
        throw StateError(
          'A different active Practice Session cannot be replaced.',
        );
      }
      return;
    }
    final fence = _captureOperationFence(threadId: threadId);
    final epoch = fence.epoch;
    _setBusy(true);
    try {
      final result = await practice.startPractice(
        threadId: threadId,
        activeMatter: matter!,
        clientOperationId: clientOperationId,
      );
      if (!_isOperationCurrent(fence)) {
        throw const AgentClientOperationCancelled();
      }
      final snapshot = result.snapshot;
      if (snapshot.sessionId != sessionId ||
          snapshot.threadId != threadId ||
          snapshot.matter.id != matterId ||
          snapshot.turnLimit != turnLimit) {
        throw StateError(
          'Voice activation did not return the created Practice Session.',
        );
      }
      _applyPracticeSnapshot(snapshot);
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> _selectScene(_SceneRetry operation) async {
    final threadId = _threadId;
    final practice = practiceClient;
    if (threadId == null || practice == null || !canSelectScene) {
      return;
    }
    final fence = _captureOperationFence(threadId: threadId);
    await stopPracticeAudio();
    if (!_isOperationCurrent(fence) || !canSelectScene) {
      return;
    }
    final epoch = fence.epoch;
    _retry = null;
    _errorMessage = null;
    _setBusy(true);
    try {
      final selection = await client.startScene(
        threadId: threadId,
        scene: operation.scene,
        clientOperationId: operation.clientOperationId,
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      if (practice case final LegacyAgentPracticeClient legacy) {
        legacy.seedSceneSelection(selection);
      }
      final result = await practice.startPractice(
        threadId: threadId,
        activeMatter: selection.activeMatter,
        clientOperationId: operation.clientOperationId,
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _practiceGeneration++;
      _applyPracticeSnapshot(result.snapshot);
      _retry = null;
      _errorMessage = null;
    } catch (error) {
      if (_isCurrent(epoch)) {
        _retry = _canRetry(error) ? operation : null;
        _errorMessage = _sceneFailureMessage(error);
      }
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<bool> sendText(String value) async {
    final text = value.trim();
    if (text.isEmpty) {
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
    final operation = retry is _TextRetry && retry.text == text
        ? retry
        : _TextRetry(text: text, clientMessageId: _newClientId('message'));
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
    _setBusy(true);
    try {
      final exchange = await client.sendText(
        threadId: threadId,
        text: operation.text,
        clientMessageId: operation.clientMessageId,
      );
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      _appendMessages([exchange.userMessage, ?exchange.assistantMessage]);
      notifyListeners();
      if (client case final AgentThreadHistoryClient historyClient) {
        await _refreshAuthoritativeThreadPage(
          historyClient,
          fence: fence,
          failureMessage: '消息已发送，但对话顺序暂时无法刷新。请重试。',
        );
      }
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
        _setBusy(false);
      }
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
      case _ThreadHistoryRecovery.focus:
        final threadId = _pendingFocusThreadId;
        if (threadId != null) {
          await selectThread(threadId);
        }
        return;
      case _ThreadHistoryRecovery.refresh:
        await _retryThreadHistoryRefresh();
        return;
      case null:
        return;
    }
  }

  Future<void> _retryThreadHistoryRefresh() async {
    final threadId = _threadId;
    if (client is! AgentThreadHistoryClient || threadId == null || _disposed) {
      return;
    }
    final historyClient = client as AgentThreadHistoryClient;
    final fence = _captureOperationFence(threadId: threadId);
    _setBusy(true);
    try {
      await _refreshAuthoritativeThreadPage(
        historyClient,
        fence: fence,
        failureMessage: '对话顺序暂时无法刷新，请稍后再试。',
      );
    } finally {
      if (_isOperationCurrent(fence)) {
        _setBusy(false);
      }
    }
  }

  Future<bool> _refreshAuthoritativeThreadPage(
    AgentThreadHistoryClient historyClient, {
    required _AgentOperationFence fence,
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
      _threadHistoryRecovery = null;
      _threadHistoryErrorMessage = null;
      notifyListeners();
      return true;
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _threadHistoryRecovery = _ThreadHistoryRecovery.refresh;
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
      case final _SceneRetry operation:
        await _selectScene(operation);
      case final _TextRetry operation:
        await _sendText(operation);
    }
  }

  Future<void> toggleQuestionAudio() async {
    final question = _currentQuestion;
    final speechPath = question?.speechPath;
    if (question == null || speechPath == null) {
      return;
    }
    await _togglePracticeMedia(
      key: _questionMediaKey(question.id)!,
      fence: _captureOperationFence(
        threadId: _threadId,
        practiceSessionId: _practiceSessionId,
        questionId: question.id,
        questionSpeechPath: speechPath,
      ),
      load: (client) => client.loadQuestionSpeech(speechPath),
      isStillAvailable: () =>
          _currentQuestion?.id == question.id &&
          _currentQuestion?.speechPath == speechPath,
    );
  }

  Future<void> toggleRecordingAudio(String audioAssetId) async {
    if (!_recordings.any(
      (recording) => recording.audioAssetId == audioAssetId,
    )) {
      return;
    }
    await _togglePracticeMedia(
      key: _recordingMediaKey(audioAssetId),
      fence: _captureOperationFence(
        threadId: _threadId,
        practiceSessionId: _practiceSessionId,
        recordingAudioAssetId: audioAssetId,
      ),
      load: (client) => client.loadRecording(audioAssetId),
      isStillAvailable: () => _recordings.any(
        (recording) => recording.audioAssetId == audioAssetId,
      ),
    );
  }

  Future<void> deleteRecording(String audioAssetId) async {
    final client = mediaClient;
    if (client == null ||
        _disposed ||
        _deletingAudioAssetId != null ||
        !_recordings.any(
          (recording) => recording.audioAssetId == audioAssetId,
        )) {
      return;
    }
    final fence = _captureOperationFence(
      threadId: _threadId,
      practiceSessionId: _practiceSessionId,
      recordingAudioAssetId: audioAssetId,
    );
    final targetKey = _recordingMediaKey(audioAssetId);
    if (_playingMediaKey == targetKey || _loadingMediaKey == targetKey) {
      final pendingPlayback = _mediaOperation;
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return;
      }
      await pendingPlayback;
      if (!_isOperationCurrent(fence)) {
        return;
      }
      try {
        await audioPlayer?.stop();
      } catch (_) {
        // The generation fence already prevents a late playback presentation.
      }
      if (!_isOperationCurrent(fence)) {
        return;
      }
    }
    if (!_isOperationCurrent(fence)) {
      return;
    }
    final epoch = fence.epoch;
    _deletingAudioAssetId = audioAssetId;
    _mediaErrorMessage = null;
    notifyListeners();
    try {
      await client.deleteRecording(audioAssetId);
      if (!_isCurrent(epoch)) {
        return;
      }
      _recordings = [
        for (final recording in _recordings)
          if (recording.audioAssetId != audioAssetId) recording,
      ];
      _mediaErrorMessage = null;
    } on AgentClientOperationCancelled {
      // Account cleanup already removed the private presentation.
    } catch (error) {
      if (_isCurrent(epoch)) {
        _mediaErrorMessage = _mediaFailureMessage(error, action: '删除录音');
        if (error is AgentClientException &&
            error.kind == AgentClientFailureKind.authenticationRequired) {
          await _clearPlayerAfterAuthenticationFailure();
        }
      }
    } finally {
      if (_isCurrent(epoch) && _deletingAudioAssetId == audioAssetId) {
        _deletingAudioAssetId = null;
        notifyListeners();
      }
    }
  }

  Future<void> stopPracticeAudio({bool notify = true}) async {
    _mediaGeneration++;
    _playingMediaKey = null;
    _loadingMediaKey = null;
    if (notify && !_disposed) {
      notifyListeners();
    }
    try {
      await audioPlayer?.stop();
    } catch (_) {
      // The private UI is already cleared; native cleanup remains best effort.
    }
  }

  Future<bool> prepareToLeavePractice() async {
    if (_disposed ||
        _practiceRequestInFlight ||
        _threadTransitionInFlight ||
        (_voiceController?.hasActiveWorkflow ?? false)) {
      return false;
    }
    final state = _recordingState;
    if (state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording ||
        state == PracticeRecordingState.awaitingConfirmation) {
      _practiceGeneration++;
      _cancelRecordingLimit();
      await _recorderStartFuture;
      if (_disposed || _practiceRequestInFlight) {
        return false;
      }
      await recorder.discardCurrent();
      _candidate = null;
      _activeConfirmationId = null;
      _activeTextAnswer = null;
      _recordingState = PracticeRecordingState.idle;
      _errorMessage = null;
      notifyListeners();
    }
    await stopPracticeAudio();
    return !_disposed && !_practiceRequestInFlight;
  }

  Future<bool> endActivePracticeEarly() async {
    final practice = practiceClient;
    final PracticeLifecycleClient? lifecycle =
        practice is PracticeLifecycleClient
        ? practice as PracticeLifecycleClient
        : null;
    final sessionId = _practiceSessionId;
    final sessionVersion = _practiceSessionVersion;
    if (lifecycle == null ||
        sessionId == null ||
        sessionVersion == null ||
        !hasActivePractice ||
        isBusy ||
        _disposed) {
      return false;
    }
    final fence = _captureOperationFence(
      threadId: _threadId,
      practiceSessionId: sessionId,
    );
    final operationId = _endPracticeClientId ??= _newClientId('practice-end');
    _setBusy(true);
    _errorMessage = null;
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      final result = await lifecycle.endEarly(
        sessionId: sessionId,
        expectedSessionVersion: sessionVersion,
        idempotencyKey: operationId,
      );
      if (!_isOperationCurrent(fence) ||
          result.sessionId != sessionId ||
          result.status != PracticeSessionLifecycleStatus.endedEarly ||
          result.version <= sessionVersion) {
        throw StateError('Practice end response did not match the session.');
      }
      _applyPracticeSnapshot(null);
      _endPracticeClientId = null;
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _errorMessage =
            error is AgentClientException &&
                error.kind == AgentClientFailureKind.authenticationRequired
            ? '登录状态已失效，请重新登录后继续。'
            : '暂时无法结束当前练习，进度仍已保留，可以重试。';
      }
      return false;
    } finally {
      if (_isCurrent(fence.epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> _togglePracticeMedia({
    required String key,
    required _AgentOperationFence fence,
    required Future<Uint8List> Function(PracticeMediaClient client) load,
    required bool Function() isStillAvailable,
  }) async {
    final client = mediaClient;
    final player = audioPlayer;
    if (client == null ||
        player == null ||
        _disposed ||
        !canUsePracticeAudio ||
        _loadingMediaKey != null ||
        _mediaOperation != null) {
      return;
    }
    if (_playingMediaKey == key) {
      await stopPracticeAudio();
      return;
    }
    await stopPracticeAudio();
    if (!_isOperationCurrent(fence) ||
        !canUsePracticeAudio ||
        !isStillAvailable()) {
      return;
    }
    final generation = ++_mediaGeneration;
    _loadingMediaKey = key;
    _mediaErrorMessage = null;
    notifyListeners();
    final operation = _loadAndPlayPracticeMedia(
      generation: generation,
      key: key,
      fence: fence,
      client: client,
      player: player,
      load: load,
      isStillAvailable: isStillAvailable,
    );
    _mediaOperation = operation;
    try {
      await operation;
    } finally {
      if (identical(_mediaOperation, operation)) {
        _mediaOperation = null;
      }
    }
  }

  Future<void> _loadAndPlayPracticeMedia({
    required int generation,
    required String key,
    required _AgentOperationFence fence,
    required PracticeMediaClient client,
    required PracticeAudioPlayer player,
    required Future<Uint8List> Function(PracticeMediaClient client) load,
    required bool Function() isStillAvailable,
  }) async {
    Uint8List? bytes;
    try {
      bytes = await load(client);
      if (!_isCurrentMedia(generation) || !_isOperationCurrent(fence)) {
        return;
      }
      if (!isStillAvailable()) {
        if (_loadingMediaKey == key) {
          _loadingMediaKey = null;
        }
        return;
      }
      await player.playWav(bytes);
      if (!_isCurrentMedia(generation) || !_isOperationCurrent(fence)) {
        await player.stop();
        return;
      }
      if (!isStillAvailable()) {
        if (_loadingMediaKey == key) {
          _loadingMediaKey = null;
        }
        await player.stop();
        return;
      }
      _loadingMediaKey = null;
      _playingMediaKey = key;
      _mediaErrorMessage = null;
    } on AgentClientOperationCancelled {
      // Account cleanup already removed the private presentation.
    } on PracticeAudioPlaybackInterruptedException {
      if (_isCurrentMedia(generation)) {
        _loadingMediaKey = null;
        _playingMediaKey = null;
      }
    } catch (error) {
      if (_isCurrentMedia(generation)) {
        _loadingMediaKey = null;
        _playingMediaKey = null;
        _mediaErrorMessage = _mediaFailureMessage(error, action: '播放音频');
        if (error is AgentClientException &&
            error.kind == AgentClientFailureKind.authenticationRequired) {
          await _clearPlayerAfterAuthenticationFailure();
        }
      }
    } finally {
      if (bytes != null) {
        try {
          bytes.fillRange(0, bytes.length, 0);
        } catch (_) {
          // The production media client always returns an owned byte buffer.
        }
      }
      if (_isCurrentMedia(generation)) {
        notifyListeners();
      }
    }
  }

  Future<void> startRecording({Duration? limit}) {
    if (!hasActivePractice ||
        isBusy ||
        _currentQuestion == null ||
        _recordingState != PracticeRecordingState.idle) {
      return Future<void>.value();
    }
    final recordingLimit = limit ?? _recordingLimit;
    if (recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 120)) {
      throw ArgumentError.value(
        recordingLimit,
        'limit',
        'must be positive and no longer than 120 seconds',
      );
    }
    final generation = ++_practiceGeneration;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _retry = null;
    _errorMessage = null;
    _cancelRecordingLimit();
    _recordingState = PracticeRecordingState.starting;
    notifyListeners();
    final operation = _startRecorder(
      _captureOperationFence(
        threadId: _threadId,
        practiceGeneration: generation,
        practiceSessionId: _practiceSessionId,
        questionId: _currentQuestion?.id,
      ),
      recordingLimit,
    );
    _recorderStartFuture = operation;
    return operation.whenComplete(() {
      if (identical(_recorderStartFuture, operation)) {
        _recorderStartFuture = null;
      }
    });
  }

  Future<void> _startRecorder(
    _AgentOperationFence fence,
    Duration recordingLimit,
  ) async {
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence) ||
          _recordingState != PracticeRecordingState.starting) {
        return;
      }
      await recorder.start();
      if (!_isOperationCurrent(fence)) {
        await recorder.discardCurrent();
        return;
      }
      _recordingState = PracticeRecordingState.recording;
      _recordingLimitTimer = Timer(recordingLimit, () {
        if (_isOperationCurrent(fence) &&
            !_disposed &&
            _recordingState == PracticeRecordingState.recording) {
          unawaited(stopRecording());
        }
      });
    } on PracticeRecordingException catch (error) {
      if (_isOperationCurrent(fence)) {
        _recordingState = PracticeRecordingState.idle;
        _errorMessage =
            error.kind == PracticeRecordingFailureKind.permissionDenied
            ? '需要麦克风权限；请在 iOS“设置”中允许 SpeakUp 使用麦克风。'
            : '暂时无法开始录音，请稍后重试。';
      }
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = '暂时无法开始录音，请稍后重试。';
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<void> stopRecording() {
    final practice = practiceClient;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    if (practice == null ||
        sessionId == null ||
        question == null ||
        _recordingState != PracticeRecordingState.recording) {
      return Future<void>.value();
    }
    _cancelRecordingLimit();
    final fence = _captureOperationFence(
      threadId: _threadId,
      practiceGeneration: _practiceGeneration,
      practiceSessionId: sessionId,
      questionId: question.id,
    );
    final clientTurnId = _newClientId('turn');
    final operation = _stopRecording(
      practice: practice,
      sessionId: sessionId,
      question: question,
      fence: fence,
      clientTurnId: clientTurnId,
    );
    _stopRecordingFuture = operation;
    return operation.whenComplete(() {
      if (identical(_stopRecordingFuture, operation)) {
        _stopRecordingFuture = null;
      }
    });
  }

  Future<void> finishRecordingGesture() async {
    if (_recordingState == PracticeRecordingState.starting) {
      await _recorderStartFuture;
    }
    if (_recordingState == PracticeRecordingState.recording) {
      await stopRecording();
    }
  }

  Future<void> cancelRecording() async {
    if (_recordingState != PracticeRecordingState.starting &&
        _recordingState != PracticeRecordingState.recording) {
      return;
    }
    _practiceGeneration++;
    _cancelRecordingLimit();
    await _recorderStartFuture;
    if (_disposed) {
      return;
    }
    await recorder.discardCurrent();
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _recordingState = PracticeRecordingState.idle;
    _errorMessage = null;
    notifyListeners();
  }

  Future<void> _stopRecording({
    required PracticeClient practice,
    required String sessionId,
    required PracticeQuestion question,
    required _AgentOperationFence fence,
    required String clientTurnId,
  }) async {
    RecordedPracticeAudio? audio;
    _recordingState = PracticeRecordingState.transcribing;
    notifyListeners();
    try {
      audio = await recorder.stop();
      if (!_isOperationCurrent(fence)) {
        return;
      }
      final candidate = await practice.transcribe(
        PracticeTranscriptionRequest(
          sessionId: sessionId,
          questionId: question.id,
          clientTurnId: clientTurnId,
          audio: audio,
        ),
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _validateCandidate(candidate, sessionId, question.id);
      _candidate = candidate;
      _activeConfirmationId = null;
      _activeTextAnswer = null;
      _recordingState = PracticeRecordingState.awaitingConfirmation;
      _errorMessage = null;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _candidate = null;
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = _transcriptionFailureMessage(error);
      }
    } finally {
      if (audio != null) {
        try {
          await recorder.discard(audio);
        } catch (_) {
          // Account cleanup retries deletion before another user can enter.
        }
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  void rerecord() {
    if (_recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    _practiceGeneration++;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _recordingState = PracticeRecordingState.idle;
    _errorMessage = null;
    notifyListeners();
  }

  Future<void> confirmTranscript() async {
    final practice = practiceClient;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    final candidate = _candidate;
    if (practice == null ||
        sessionId == null ||
        question == null ||
        candidate == null ||
        _isSessionCompleted ||
        _recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final fence = _captureOperationFence(
      threadId: _threadId,
      practiceGeneration: _practiceGeneration,
      practiceSessionId: sessionId,
      questionId: question.id,
      candidateId: candidate.id,
    );
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return;
      }
      final confirmation = await practice.confirm(
        sessionId: sessionId,
        questionId: question.id,
        candidateId: candidate.id,
        idempotencyKey: _activeConfirmationId ??= _newClientId('confirm'),
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _validateConfirmation(confirmation, candidate);
      _applyPracticeConfirmation(confirmation);
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _recordingState = PracticeRecordingState.awaitingConfirmation;
        _errorMessage = _confirmationFailureMessage(error);
      }
    }
    if (_isCurrent(fence.epoch)) {
      notifyListeners();
    }
  }

  Future<bool> submitPracticeText(String answerText) async {
    final practice = practiceClient;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    final text = answerText.trim();
    if (practice == null ||
        sessionId == null ||
        question == null ||
        text.isEmpty ||
        text.length > 8000 ||
        _isSessionCompleted ||
        isBusy ||
        _recordingState != PracticeRecordingState.idle) {
      return false;
    }
    final generation = ++_practiceGeneration;
    if (_activeTextAnswer != text) {
      _activeConfirmationId = _newClientId('text-turn');
      _activeTextAnswer = text;
    }
    final fence = _captureOperationFence(
      threadId: _threadId,
      practiceGeneration: generation,
      practiceSessionId: sessionId,
      questionId: question.id,
    );
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      final confirmation = await practice.submitText(
        sessionId: sessionId,
        questionId: question.id,
        answerText: text,
        idempotencyKey: _activeConfirmationId!,
      );
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      _validateConfirmationFields(
        confirmation,
        expectedSessionId: sessionId,
        expectedQuestionId: question.id,
        expectedAnswer: text,
      );
      _applyPracticeConfirmation(confirmation);
      _activeTextAnswer = null;
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = _confirmationFailureMessage(error);
      }
      return false;
    } finally {
      if (_isCurrent(fence.epoch)) {
        notifyListeners();
      }
    }
  }

  void _applyPracticeConfirmation(PracticeTurnConfirmation confirmation) {
    _completedTurns = confirmation.completedTurns;
    _turnLimit = confirmation.turnLimit;
    _sessionCompleted = confirmation.sessionCompleted;
    _practiceSessionVersion =
        confirmation.sessionVersion ?? _practiceSessionVersion;
    _endPracticeClientId = null;
    _currentQuestion = confirmation.nextQuestion;
    _review = confirmation.review;
    final audioAssetId = confirmation.audioAssetId;
    if (audioAssetId != null &&
        !_recordings.any(
          (recording) => recording.audioAssetId == audioAssetId,
        )) {
      _recordings = [
        ..._recordings,
        PracticeRecordingReference(
          audioAssetId: audioAssetId,
          effectiveTurn: confirmation.completedTurns,
        ),
      ];
    }
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _appendMessages([
      confirmation.answer,
      ?confirmation.nextQuestion?.presentation,
    ]);
    _appendPracticeMessages([
      confirmation.answer,
      ?confirmation.nextQuestion?.presentation,
    ]);
    if (confirmation.sessionCompleted) {
      _recordingState = confirmation.review == null
          ? PracticeRecordingState.reviewFailed
          : PracticeRecordingState.completed;
      _errorMessage = confirmation.review == null
          ? '练习已完成，正在等待服务端恢复同一次复盘。'
          : null;
    } else {
      _recordingState = PracticeRecordingState.idle;
    }
  }

  /// Refetches the server-owned result. Flutter never creates a Review.
  Future<void> retryReview() async {
    final practice = practiceClient;
    final threadId = _threadId;
    final expectedSessionId = _practiceSessionId;
    if (practice == null ||
        threadId == null ||
        expectedSessionId == null ||
        !_isSessionCompleted ||
        _review != null ||
        _recordingState != PracticeRecordingState.reviewFailed) {
      return;
    }
    final fence = _captureOperationFence(
      threadId: threadId,
      practiceSessionId: expectedSessionId,
    );
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      final snapshot = await practice.restorePractice(
        threadId: threadId,
        activeMatter: _activeMatter,
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      if (snapshot == null) {
        throw StateError('Practice Session is not restorable.');
      }
      if (snapshot.sessionId != expectedSessionId) {
        throw StateError('Practice Session identity changed during Review.');
      }
      _applyPracticeSnapshot(snapshot, preserveKnownRecordings: true);
      if (_review == null) {
        throw StateError('Review is not ready.');
      }
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _recordingState = PracticeRecordingState.reviewFailed;
        _errorMessage = _reviewFailureMessage(error);
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  /// Invalidates private UI state synchronously, then removes temporary audio
  /// and waits for all account-scoped transports to stop.
  Future<void> clearPrivateState() async {
    _cancelRecordingLimit();
    _epoch++;
    _practiceGeneration++;
    _initializationFuture = null;
    _threadId = null;
    _currentThreadSummary = null;
    _threads = const <AgentThreadSummary>[];
    _nextThreadCursor = null;
    _nextMessageCursor = null;
    _threadHistoryErrorMessage = null;
    _threadHistoryRecovery = null;
    _pendingFocusThreadId = null;
    _draftThreadRecoveryGeneration = 0;
    _loadingMoreThreads = false;
    _loadingEarlierMessages = false;
    _threadTransitionGeneration++;
    _threadTransitionInFlight = false;
    _practiceSessionId = null;
    _practiceSessionVersion = null;
    _endPracticeClientId = null;
    _currentQuestion = null;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _activeMatter = null;
    _messages = const <AgentMessage>[];
    _practiceMessages = const <AgentMessage>[];
    _recordingState = PracticeRecordingState.idle;
    _review = null;
    _errorMessage = null;
    _retry = null;
    _completedTurns = 0;
    _turnLimit = 0;
    _sessionCompleted = false;
    _recordings = const <PracticeRecordingReference>[];
    _playingMediaKey = null;
    _loadingMediaKey = null;
    _deletingAudioAssetId = null;
    _mediaErrorMessage = null;
    _mediaGeneration++;
    _initialized = false;
    _busy = false;
    if (!_disposed) {
      notifyListeners();
    }

    final cleanup = Future.wait<void>([
      Future<void>.sync(client.clearAccountState),
      if (_voiceController case final voice?)
        Future<void>.sync(
          () => voice.clearPrivateState(
            clearClient: !identical(client, voice.client),
          ),
        ),
      if (practiceClient case final practice?)
        Future<void>.sync(practice.clearAccountState),
      Future<void>.sync(() async {
        await _recorderStartFuture;
        await _stopRecordingFuture;
        await recorder.clearAccountState();
      }),
      if (mediaClient case final media?)
        Future<void>.sync(media.clearAccountState),
      if (audioPlayer case final player?)
        Future<void>.sync(player.clearAccountState),
    ]);
    _accountCleanupFuture = cleanup;
    try {
      await cleanup;
    } finally {
      await _mediaOperation;
      // A second strict cleanup pass fences a late native play completion.
      // Failure must remain visible to Auth so account switching stays closed.
      await audioPlayer?.clearAccountState();
      if (identical(_accountCleanupFuture, cleanup)) {
        _accountCleanupFuture = null;
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    if (mediaClient != null) {
      WidgetsBinding.instance.removeObserver(this);
    }
    _cancelRecordingLimit();
    _epoch++;
    _practiceGeneration++;
    _mediaGeneration++;
    _threadTransitionGeneration++;
    _threadTransitionInFlight = false;
    _initializationFuture = null;
    _voiceController?.removeListener(_handleVoiceState);
    _voiceController?.dispose();
    unawaited(recorder.discardCurrent());
    unawaited(_mediaCompletionSubscription?.cancel());
    unawaited(mediaClient?.dispose());
    unawaited(audioPlayer?.dispose());
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed ||
        _disposed ||
        !supportsPracticeMedia ||
        (_playingMediaKey == null &&
            _loadingMediaKey == null &&
            _mediaOperation == null)) {
      return;
    }
    unawaited(stopPracticeAudio());
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
    _voiceController?.syncMessages(_messages);
    _retry = null;
    _errorMessage = null;
    _applyPracticeSnapshot(null);
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
      activeMatterId: snapshot.activeMatter?.id,
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

  void _applyPracticeSnapshot(
    PracticeSessionSnapshot? snapshot, {
    bool preserveKnownRecordings = false,
  }) {
    final previousSessionId = _practiceSessionId;
    _cancelRecordingLimit();
    _practiceGeneration++;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _playingMediaKey = null;
    _loadingMediaKey = null;
    _deletingAudioAssetId = null;
    _mediaErrorMessage = null;
    _mediaGeneration++;
    unawaited(audioPlayer?.stop());
    if (snapshot == null) {
      _practiceSessionId = null;
      _practiceSessionVersion = null;
      _endPracticeClientId = null;
      _currentQuestion = null;
      _activeMatter = null;
      _completedTurns = 0;
      _turnLimit = 0;
      _sessionCompleted = false;
      _review = null;
      _recordings = const <PracticeRecordingReference>[];
      _practiceMessages = const <AgentMessage>[];
      _recordingState = PracticeRecordingState.idle;
      return;
    }
    _validatePracticeSnapshot(snapshot);
    if (snapshot.sessionId != previousSessionId) {
      _practiceMessages = const <AgentMessage>[];
    }
    final mayPreserveKnownRecordings =
        preserveKnownRecordings && snapshot.sessionId == _practiceSessionId;
    _practiceSessionId = snapshot.sessionId;
    _practiceSessionVersion = snapshot.sessionVersion;
    _endPracticeClientId = null;
    _currentQuestion = snapshot.currentQuestion;
    _activeMatter = snapshot.matter;
    _completedTurns = snapshot.completedTurns;
    _turnLimit = snapshot.turnLimit;
    _sessionCompleted = snapshot.sessionCompleted;
    _review = snapshot.review;
    final currentTurn = snapshot.currentTurn;
    final audioAssetId = currentTurn?.audioAssetId;
    if (!mayPreserveKnownRecordings) {
      _recordings = audioAssetId == null
          ? const <PracticeRecordingReference>[]
          : <PracticeRecordingReference>[
              PracticeRecordingReference(
                audioAssetId: audioAssetId,
                effectiveTurn: currentTurn!.effectiveTurns,
              ),
            ];
    }
    final currentQuestion = snapshot.currentQuestion?.presentation;
    _appendMessages([?currentQuestion]);
    _appendPracticeMessages([?currentQuestion]);
    _recordingState = snapshot.sessionCompleted
        ? snapshot.review == null
              ? PracticeRecordingState.reviewFailed
              : PracticeRecordingState.completed
        : PracticeRecordingState.idle;
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
    _voiceController?.syncMessages(_messages);
  }

  void _appendPracticeMessages(Iterable<AgentMessage> values) {
    final messages = List<AgentMessage>.from(_practiceMessages);
    final ids = {for (final message in messages) message.id};
    for (final message in values) {
      if (ids.add(message.id)) {
        messages.add(message);
      }
    }
    _practiceMessages = messages;
  }

  void _commitVoiceMessages(Iterable<AgentMessage> values) {
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
        activeMatterId: current.activeMatterId,
        createdAt: current.createdAt,
        updatedAt: now.isBefore(current.updatedAt) ? current.updatedAt : now,
      );
      _currentThreadSummary = updated;
      _mergeThreadSummary(updated, placeFirst: true);
    }
    notifyListeners();
    if (client case final AgentThreadHistoryClient historyClient) {
      final threadId = _threadId;
      if (threadId != null) {
        final fence = _captureOperationFence(threadId: threadId);
        unawaited(
          _refreshAuthoritativeThreadPage(
            historyClient,
            fence: fence,
            failureMessage: '语音消息已发送，但对话标题暂时无法刷新。请重试。',
          ),
        );
      }
    }
  }

  void _markVoiceMessageAudioDeleted(
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
    _voiceController?.syncMessages(_messages);
    notifyListeners();
  }

  void _handleVoiceState() {
    final workflowActive = _voiceController?.hasActiveWorkflow ?? false;
    if (_disposed || workflowActive == _relayedVoiceWorkflowActive) {
      return;
    }
    _relayedVoiceWorkflowActive = workflowActive;
    notifyListeners();
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _epoch;

  _AgentOperationFence _captureOperationFence({
    String? threadId,
    int? practiceGeneration,
    String? practiceSessionId,
    String? questionId,
    String? candidateId,
    String? questionSpeechPath,
    String? recordingAudioAssetId,
  }) {
    return _AgentOperationFence(
      epoch: _epoch,
      threadId: threadId,
      practiceGeneration: practiceGeneration,
      practiceSessionId: practiceSessionId,
      questionId: questionId,
      candidateId: candidateId,
      questionSpeechPath: questionSpeechPath,
      recordingAudioAssetId: recordingAudioAssetId,
    );
  }

  bool _isOperationCurrent(_AgentOperationFence fence) {
    return _isCurrent(fence.epoch) &&
        (fence.threadId == null || fence.threadId == _threadId) &&
        (fence.practiceGeneration == null ||
            fence.practiceGeneration == _practiceGeneration) &&
        (fence.practiceSessionId == null ||
            fence.practiceSessionId == _practiceSessionId) &&
        (fence.questionId == null ||
            fence.questionId == _currentQuestion?.id) &&
        (fence.candidateId == null || fence.candidateId == _candidate?.id) &&
        (fence.questionSpeechPath == null ||
            fence.questionSpeechPath == _currentQuestion?.speechPath) &&
        (fence.recordingAudioAssetId == null ||
            _recordings.any(
              (recording) =>
                  recording.audioAssetId == fence.recordingAudioAssetId,
            ));
  }

  bool _isCurrentMedia(int generation) {
    return !_disposed && generation == _mediaGeneration;
  }

  void _handleMediaCompletion() {
    if (_disposed || _playingMediaKey == null) {
      return;
    }
    _playingMediaKey = null;
    notifyListeners();
  }

  Future<void> _clearPlayerAfterAuthenticationFailure() async {
    _mediaGeneration++;
    _playingMediaKey = null;
    _loadingMediaKey = null;
    _deletingAudioAssetId = null;
    try {
      await audioPlayer?.clearAccountState();
    } catch (_) {
      // Authentication invalidation will repeat private-state cleanup.
    }
    if (!_disposed) {
      notifyListeners();
    }
  }

  String _mediaFailureMessage(Object error, {required String action}) {
    if (error is AgentClientException) {
      if (error.kind == AgentClientFailureKind.notFound) {
        return '$action失败：录音不存在或已删除。';
      }
      if (error.kind == AgentClientFailureKind.authenticationRequired) {
        return '登录状态已失效，请重新登录。';
      }
      if (error.kind == AgentClientFailureKind.network) {
        return '$action失败：请检查网络后重试。';
      }
      if (error.kind == AgentClientFailureKind.rateLimited) {
        return '$action过于频繁，请稍后重试。';
      }
    }
    return '$action暂时不可用，请稍后重试。';
  }

  void _cancelRecordingLimit() {
    _recordingLimitTimer?.cancel();
    _recordingLimitTimer = null;
  }

  String _sceneFailureMessage(Object error) {
    if (error is! AgentClientException) {
      return '暂时无法开始这个场景，请稍后重试。';
    }
    if (error.isUnavailable) {
      return '场景与语音练习尚未开放，当前可以继续使用 Agent 文本对话。';
    }
    if (_isFreeQuotaExhausted(error)) {
      return '今日免费练习额度已用完，请稍后再试。';
    }
    if (error.kind == AgentClientFailureKind.network) {
      return '网络连接不稳定，暂时无法开始练习。';
    }
    if (error.kind == AgentClientFailureKind.rateLimited) {
      return '请求过于频繁，请稍后再开始练习。';
    }
    return '暂时无法开始这个场景，请稍后重试。';
  }

  String _transcriptionFailureMessage(Object error) {
    if (error is AgentClientException) {
      if (_isFreeQuotaExhausted(error)) {
        return '今日免费语音额度已用完，本轮未计入进度。';
      }
      if (error.kind == AgentClientFailureKind.network) {
        return '网络连接不稳定，未能转写；请重新录音。';
      }
      if (error.kind == AgentClientFailureKind.rateLimited) {
        return '语音请求过于频繁，请稍后重新录音。';
      }
    }
    return '没有识别出这一轮，请重新录音。';
  }

  String _confirmationFailureMessage(Object error) {
    if (error is AgentClientException) {
      if (_isFreeQuotaExhausted(error)) {
        return '今日免费练习额度已用完，这一轮尚未确认。';
      }
      if (error.kind == AgentClientFailureKind.conflict &&
          error.errorCode == 'resource_conflict' &&
          !error.retryable) {
        return '录音已失效，请重新录音。';
      }
      if (error.kind == AgentClientFailureKind.network) {
        return '网络连接不稳定，这一轮尚未确认，请重试。';
      }
      if (error.kind == AgentClientFailureKind.rateLimited) {
        return '提交过于频繁，请稍后重试；转写内容已保留。';
      }
    }
    return '这一轮没有提交成功，请重试。';
  }

  String _reviewFailureMessage(Object error) {
    if (error is AgentClientException) {
      if (_isFreeQuotaExhausted(error)) {
        return '今日免费复盘额度已用完，请稍后刷新。';
      }
      if (error.kind == AgentClientFailureKind.network) {
        return '网络连接不稳定，暂时无法刷新复盘。';
      }
      if (error.kind == AgentClientFailureKind.rateLimited) {
        return '复盘刷新过于频繁，请稍后再试。';
      }
    }
    return '复盘仍在生成，请稍后重试。';
  }

  bool _isFreeQuotaExhausted(AgentClientException error) {
    return error.errorCode == 'quota_exhausted';
  }

  bool _isPracticeRestoreAmbiguity(Object error) {
    return error is AgentClientException &&
        error.kind == AgentClientFailureKind.conflict &&
        error.statusCode == 409 &&
        error.errorCode == 'resource_conflict' &&
        !error.retryable;
  }

  void _setBusy(bool value) {
    if (_disposed) {
      return;
    }
    _busy = value;
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
    var invalidMessages = false;
    for (final message in snapshot.messages) {
      final sequence = message.sequence;
      if (message.id.trim().isEmpty ||
          message.text.trim().isEmpty ||
          !messageIds.add(message.id) ||
          (sequence != null &&
              (sequence < 1 || sequence <= previousSequence))) {
        invalidMessages = true;
        break;
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
        (snapshot.activeMatter != null &&
            (snapshot.activeMatter!.id.trim().isEmpty ||
                snapshot.activeMatter!.scene.id.trim().isEmpty ||
                snapshot.activeMatter!.scene.title.trim().isEmpty)) ||
        invalidMessages ||
        (recovery != null &&
            (recovery.text.trim().isEmpty ||
                recovery.clientMessageId.trim().isEmpty ||
                recovery.failureKind.trim().isEmpty))) {
      throw StateError('Invalid Agent Thread snapshot.');
    }
  }

  void _validateThreadSummary(AgentThreadSummary summary) {
    if (summary.id.trim().isEmpty ||
        summary.updatedAt.isBefore(summary.createdAt) ||
        (summary.activeMatterId != null &&
            summary.activeMatterId!.trim().isEmpty)) {
      throw StateError('Invalid Agent Thread summary.');
    }
  }

  void _validateThreadPage(AgentThreadPage page) {
    if (page.threads.length > 100 ||
        (page.focusedThreadId != null &&
            page.focusedThreadId!.trim().isEmpty) ||
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
          message.text.trim().isEmpty ||
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

  void _validatePracticeSnapshot(PracticeSessionSnapshot snapshot) {
    if (snapshot.sessionId.trim().isEmpty ||
        snapshot.matter.id.trim().isEmpty ||
        snapshot.matter.scene.id.trim().isEmpty ||
        snapshot.completedTurns < 0 ||
        snapshot.turnLimit < 1 ||
        snapshot.turnLimit > 14 ||
        snapshot.completedTurns > snapshot.turnLimit ||
        (snapshot.sessionVersion != null && snapshot.sessionVersion! < 1) ||
        (!snapshot.sessionCompleted && snapshot.currentQuestion == null) ||
        (snapshot.currentQuestion != null &&
            snapshot.currentQuestion!.sessionId != snapshot.sessionId) ||
        (snapshot.currentTurn?.audioAssetId != null &&
            (snapshot.currentTurn!.audioAssetId!.trim().isEmpty ||
                snapshot.currentTurn!.audioAssetId!.length > 128 ||
                snapshot.currentTurn!.audioAssetId!.trim() !=
                    snapshot.currentTurn!.audioAssetId))) {
      throw StateError('Invalid Practice Session snapshot.');
    }
  }

  void _validateCandidate(
    TranscriptionCandidate candidate,
    String sessionId,
    String questionId,
  ) {
    if (candidate.id.trim().isEmpty ||
        candidate.text.trim().isEmpty ||
        candidate.sessionId != sessionId ||
        candidate.questionId != questionId) {
      throw StateError('Invalid transcription candidate.');
    }
  }

  void _validateConfirmation(
    PracticeTurnConfirmation confirmation,
    TranscriptionCandidate candidate,
  ) {
    _validateConfirmationFields(
      confirmation,
      expectedSessionId: candidate.sessionId,
      expectedQuestionId: candidate.questionId,
      expectedCandidateId: candidate.id,
      expectedAnswer: candidate.text,
    );
  }

  void _validateConfirmationFields(
    PracticeTurnConfirmation confirmation, {
    required String expectedSessionId,
    required String expectedQuestionId,
    String? expectedCandidateId,
    required String expectedAnswer,
  }) {
    if (confirmation.turnId.trim().isEmpty ||
        confirmation.candidateId.trim().isEmpty ||
        confirmation.sessionId != expectedSessionId ||
        confirmation.questionId != expectedQuestionId ||
        (expectedCandidateId != null &&
            confirmation.candidateId != expectedCandidateId) ||
        confirmation.answer.text != expectedAnswer ||
        confirmation.completedTurns < 1 ||
        confirmation.turnLimit < 1 ||
        confirmation.turnLimit > 14 ||
        confirmation.completedTurns > confirmation.turnLimit ||
        (confirmation.sessionVersion != null &&
            confirmation.sessionVersion! < 1) ||
        (!confirmation.sessionCompleted && confirmation.nextQuestion == null) ||
        (confirmation.nextQuestion != null &&
            confirmation.nextQuestion!.sessionId != confirmation.sessionId) ||
        (confirmation.audioAssetId != null &&
            (confirmation.audioAssetId!.trim().isEmpty ||
                confirmation.audioAssetId!.length > 128 ||
                confirmation.audioAssetId!.trim() !=
                    confirmation.audioAssetId))) {
      throw StateError('Invalid Practice Turn confirmation.');
    }
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

final class _AgentOperationFence {
  const _AgentOperationFence({
    required this.epoch,
    this.threadId,
    this.practiceGeneration,
    this.practiceSessionId,
    this.questionId,
    this.candidateId,
    this.questionSpeechPath,
    this.recordingAudioAssetId,
  });

  final int epoch;
  final String? threadId;
  final int? practiceGeneration;
  final String? practiceSessionId;
  final String? questionId;
  final String? candidateId;
  final String? questionSpeechPath;
  final String? recordingAudioAssetId;
}

sealed class _AgentRetry {
  const _AgentRetry();
}

enum _ThreadHistoryRecovery { create, focus, refresh }

final class _RestoreRetry extends _AgentRetry {
  const _RestoreRetry();
}

final class _SceneRetry extends _AgentRetry {
  const _SceneRetry({required this.scene, required this.clientOperationId});

  final AgentScene scene;
  final String clientOperationId;
}

final class _TextRetry extends _AgentRetry {
  const _TextRetry({required this.text, required this.clientMessageId});

  final String text;
  final String clientMessageId;
}

final Random _clientIdRandom = Random.secure();

String _createSecureClientId(String scope) {
  final buffer = StringBuffer('${scope}_');
  for (var index = 0; index < 16; index++) {
    buffer.write(
      _clientIdRandom.nextInt(256).toRadixString(16).padLeft(2, '0'),
    );
  }
  return buffer.toString();
}

String? _questionMediaKey(String? questionId) {
  return questionId == null ? null : 'question:$questionId';
}

String _recordingMediaKey(String audioAssetId) {
  return 'recording:$audioAssetId';
}
