import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';

import 'agent_voice_input_client.dart';
import 'agent_voice_recording.dart';

typedef AgentVoiceInputIdFactory = String Function(String scope);
typedef AgentVoiceInputClock = DateTime Function();

enum AgentVoiceInputState {
  idle,
  starting,
  recording,
  completing,
  editing,
  failed,
}

/// Owns ephemeral microphone input until it becomes an editable text draft.
///
/// This controller never creates a voice candidate, MessageAudio, or Message.
/// The edited transcript is submitted through the ordinary text composer.
final class AgentVoiceInputController extends ChangeNotifier
    with WidgetsBindingObserver {
  AgentVoiceInputController({
    required this.client,
    required this.recorder,
    required this.idFactory,
    this.clock = DateTime.now,
    this.recordingLimit = const Duration(seconds: 58),
    this.completionTimeout = const Duration(seconds: 75),
  }) {
    if (recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 60) ||
        completionTimeout <= Duration.zero) {
      throw ArgumentError('Agent voice input configuration is invalid.');
    }
    WidgetsBinding.instance.addObserver(this);
  }

  final AgentVoiceInputClient client;
  final AgentVoiceRecorder recorder;
  final AgentVoiceInputIdFactory idFactory;
  final AgentVoiceInputClock clock;
  final Duration recordingLimit;
  final Duration completionTimeout;

  String? _threadId;
  AgentVoiceInputState _state = AgentVoiceInputState.idle;
  String _liveTranscript = '';
  String _editedTranscript = '';
  String? _errorMessage;
  bool _canRetry = false;
  DateTime? _recordingStartedAt;
  Duration _recordingElapsed = Duration.zero;
  Timer? _recordingTicker;
  Timer? _recordingLimitTimer;
  StreamSubscription<AgentVoiceInputEvent>? _eventSubscription;
  Completer<_AgentVoiceInputTerminal>? _terminal;
  Future<void>? _operation;
  Future<void>? _workflowCleanup;
  Future<void>? _cleanupFuture;
  int _workflowGeneration = 0;
  int _accountEpoch = 0;
  bool _backgrounded = false;
  bool _disposed = false;

  String? get threadId => _threadId;
  AgentVoiceInputState get state => _state;
  String get liveTranscript => _liveTranscript;
  String get editedTranscript => _editedTranscript;
  String? get errorMessage => _errorMessage;
  Duration get recordingElapsed => _recordingElapsed;
  bool get canRetry => _canRetry;
  bool get hasActiveWorkflow =>
      _state != AgentVoiceInputState.idle ||
      _workflowCleanup != null ||
      _cleanupFuture != null;
  bool get canStartRecording =>
      !_disposed &&
      !_backgrounded &&
      _workflowCleanup == null &&
      _cleanupFuture == null &&
      _threadId != null &&
      _state == AgentVoiceInputState.idle;
  bool get canStopRecording => _state == AgentVoiceInputState.recording;
  bool get canSubmitTranscript =>
      _state == AgentVoiceInputState.editing &&
      _editedTranscript.trim().isNotEmpty &&
      utf8.encode(_editedTranscript.trim()).length <= 16384;

  Future<void> bindThread(String? threadId) async {
    if (threadId == _threadId) {
      await _workflowCleanup;
      return;
    }
    _threadId = threadId;
    await _cancelWorkflow();
  }

  Future<void> startRecording() {
    if (!canStartRecording || _operation != null) {
      return Future<void>.value();
    }
    final fence = _newFence();
    _state = AgentVoiceInputState.starting;
    _liveTranscript = '';
    _editedTranscript = '';
    _errorMessage = null;
    _canRetry = false;
    notifyListeners();
    late final Future<void> operation;
    operation = _startRecording(fence).whenComplete(() {
      if (identical(_operation, operation)) {
        _operation = null;
      }
    });
    _operation = operation;
    return operation;
  }

  Future<void> _startRecording(_AgentVoiceInputFence fence) async {
    final streamingRecorder = recorder;
    if (streamingRecorder is! AgentVoiceEphemeralStreamingRecorder) {
      _showFailure(
        fence,
        const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.unavailable,
        ),
      );
      return;
    }
    try {
      final chunks =
          await (streamingRecorder as AgentVoiceEphemeralStreamingRecorder)
              .startAudioStream();
      if (!_isCurrent(fence)) {
        await _discardCurrentBestEffort();
        return;
      }
      final terminal = Completer<_AgentVoiceInputTerminal>();
      _terminal = terminal;
      final events = client.transcribeRealtime(
        threadId: fence.threadId,
        audioChunks: chunks,
        idempotencyKey: _newId('voice-input'),
      );
      late final StreamSubscription<AgentVoiceInputEvent> subscription;
      subscription = events.listen(
        (event) => _handleEvent(fence, event),
        onError: (Object error, StackTrace stackTrace) {
          _completeTerminal(
            terminal,
            _AgentVoiceInputTerminal.failure(error, stackTrace),
          );
          if (_isCurrent(fence) &&
              (_state == AgentVoiceInputState.starting ||
                  _state == AgentVoiceInputState.recording)) {
            unawaited(_abortCaptureAfterFailure(fence, error));
          }
        },
        onDone: () {
          if (!terminal.isCompleted) {
            const error = AgentClientException(
              kind: AgentClientFailureKind.network,
              retryable: true,
            );
            _completeTerminal(
              terminal,
              const _AgentVoiceInputTerminal.failure(error, StackTrace.empty),
            );
            if (_isCurrent(fence) &&
                (_state == AgentVoiceInputState.starting ||
                    _state == AgentVoiceInputState.recording)) {
              unawaited(_abortCaptureAfterFailure(fence, error));
            }
          }
        },
        cancelOnError: false,
      );
      _eventSubscription = subscription;
      if (!_isCurrent(fence)) {
        _completeTerminal(terminal, const _AgentVoiceInputTerminal.cancelled());
        Future<void>? subscriptionCancellation;
        try {
          subscriptionCancellation = subscription.cancel();
        } catch (_) {
          // Recorder cleanup below still releases the stale capture.
        }
        await _discardCurrentBestEffort();
        try {
          await subscriptionCancellation;
        } catch (_) {
          // The generation fence already made this stream unobservable.
        }
        return;
      }
      _state = AgentVoiceInputState.recording;
      _recordingElapsed = Duration.zero;
      _startRecordingTimers(fence);
      notifyListeners();
    } catch (error) {
      await _discardCurrentBestEffort();
      _showFailure(fence, error);
    }
  }

  void _handleEvent(_AgentVoiceInputFence fence, AgentVoiceInputEvent event) {
    if (!_isCurrent(fence)) {
      return;
    }
    switch (event) {
      case AgentVoiceInputStarted():
        return;
      case AgentVoiceInputUpdated(:final transcript):
        _liveTranscript = transcript;
        notifyListeners();
      case AgentVoiceInputCompleted(:final transcript):
        _liveTranscript = transcript;
        _completeTerminal(
          _terminal,
          _AgentVoiceInputTerminal.completed(transcript),
        );
        notifyListeners();
      case AgentVoiceInputFailed(:final kind, :final retryable):
        final failure = AgentVoiceInputFailure(
          kind: kind,
          retryable: retryable,
        );
        _completeTerminal(
          _terminal,
          _AgentVoiceInputTerminal.serverFailure(failure, StackTrace.current),
        );
        if (_state == AgentVoiceInputState.starting ||
            _state == AgentVoiceInputState.recording) {
          unawaited(_abortCaptureAfterFailure(fence, failure));
        }
    }
  }

  Future<void> stopRecording() {
    if (_state == AgentVoiceInputState.completing) {
      return _operation ?? Future<void>.value();
    }
    if (!canStopRecording || _operation != null) {
      return Future<void>.value();
    }
    final fence = _captureFence();
    _cancelRecordingTimers();
    _state = AgentVoiceInputState.completing;
    notifyListeners();
    late final Future<void> operation;
    operation = _stopRecording(fence).whenComplete(() {
      if (identical(_operation, operation)) {
        _operation = null;
      }
    });
    _operation = operation;
    return operation;
  }

  Future<void> _stopRecording(_AgentVoiceInputFence fence) async {
    String? completedTranscript;
    Object? failure;
    var serverTerminal = false;
    final terminal = _terminal;
    final subscription = _eventSubscription;
    try {
      final streamingRecorder = recorder;
      if (streamingRecorder is! AgentVoiceEphemeralStreamingRecorder ||
          terminal == null) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.unavailable,
        );
      }
      await (streamingRecorder as AgentVoiceEphemeralStreamingRecorder)
          .stopAudioStreamAndDiscard();
      if (!_isCurrent(fence)) {
        return;
      }
      final result = await terminal.future.timeout(completionTimeout);
      serverTerminal = result.serverTerminal;
      if (!_isCurrent(fence) || result.cancelled) {
        return;
      }
      if (result.error case final error?) {
        Error.throwWithStackTrace(error, result.stackTrace!);
      }
      final transcript = result.transcript!.trim();
      if (transcript.isEmpty) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
        );
      }
      completedTranscript = transcript;
    } catch (error) {
      failure = error;
    } finally {
      _completeTerminal(terminal, const _AgentVoiceInputTerminal.cancelled());
      if (identical(_eventSubscription, subscription)) {
        _eventSubscription = null;
      }
      if (identical(_terminal, terminal)) {
        _terminal = null;
      }
      if (!serverTerminal) {
        _cancelSubscriptionWithoutWaiting(subscription);
      }
      await _discardCurrentBestEffort();
    }
    if (!_isCurrent(fence)) {
      return;
    }
    if (failure case final error?) {
      _showFailure(fence, error);
      return;
    }
    final transcript = completedTranscript!;
    _liveTranscript = transcript;
    _editedTranscript = transcript;
    _state = AgentVoiceInputState.editing;
    _errorMessage = null;
    _canRetry = false;
    notifyListeners();
  }

  Future<void> _abortCaptureAfterFailure(
    _AgentVoiceInputFence fence,
    Object error,
  ) async {
    if (!_isCurrent(fence)) {
      return;
    }
    _workflowGeneration++;
    final cleanupFence = _captureFence();
    _cancelRecordingTimers();
    final subscription = _eventSubscription;
    _eventSubscription = null;
    _terminal = null;
    _liveTranscript = '';
    _editedTranscript = '';
    _state = AgentVoiceInputState.completing;
    _errorMessage = null;
    _canRetry = false;
    notifyListeners();
    await _discardCurrentBestEffort();
    _cancelSubscriptionWithoutWaiting(subscription);
    if (!_isCurrent(cleanupFence)) {
      return;
    }
    _state = AgentVoiceInputState.failed;
    _errorMessage = _failureMessage(error);
    _canRetry = _isRetryable(error);
    notifyListeners();
  }

  Future<void> cancel() => _cancelWorkflow();

  Future<void> _cancelWorkflow() {
    final existing = _workflowCleanup;
    if (existing != null) {
      return existing;
    }
    _workflowGeneration++;
    _cancelRecordingTimers();
    final terminal = _terminal;
    final subscription = _eventSubscription;
    final operation = _operation;
    _terminal = null;
    _eventSubscription = null;
    _resetPresentation();
    _completeTerminal(terminal, const _AgentVoiceInputTerminal.cancelled());
    late final Future<void> cleanup;
    cleanup =
        Future<void>.microtask(() async {
          Future<void>? subscriptionCancellation;
          try {
            subscriptionCancellation = subscription?.cancel();
          } catch (_) {
            // Closing the recorder below still fences and releases the workflow.
          }
          await _discardCurrentBestEffort();
          try {
            await subscriptionCancellation;
          } catch (_) {
            // Recorder cleanup already released the local capture.
          }
          try {
            await operation;
          } catch (_) {
            // The generation fence already made the stale operation unobservable.
          }
          await _discardCurrentBestEffort();
        }).whenComplete(() {
          if (identical(_workflowCleanup, cleanup)) {
            _workflowCleanup = null;
            if (!_disposed) {
              notifyListeners();
            }
          }
        });
    _workflowCleanup = cleanup;
    if (!_disposed) {
      notifyListeners();
    }
    return cleanup;
  }

  void updateTranscript(String value) {
    if (_state != AgentVoiceInputState.editing || value == _editedTranscript) {
      return;
    }
    _editedTranscript = value;
    notifyListeners();
  }

  Future<void> retry() async {
    if (!_canRetry || _state != AgentVoiceInputState.failed) {
      return;
    }
    final operation = _operation;
    if (operation != null) {
      try {
        await operation;
      } catch (_) {
        // The visible failure already describes the completed operation.
      }
    }
    await _workflowCleanup;
    if (!_canRetry || _state != AgentVoiceInputState.failed) {
      return;
    }
    _resetPresentation();
    notifyListeners();
    await startRecording();
  }

  Future<void> clearPrivateState() async {
    final existing = _cleanupFuture;
    if (existing != null) {
      await existing;
      return;
    }
    _accountEpoch++;
    _threadId = null;
    final cleanup = () async {
      await _cancelWorkflow();
      await Future.wait<void>([
        Future<void>.sync(recorder.clearAccountState),
        Future<void>.sync(client.clearAccountState),
      ]);
    }();
    _cleanupFuture = cleanup;
    try {
      await cleanup;
    } finally {
      if (identical(_cleanupFuture, cleanup)) {
        _cleanupFuture = null;
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (_disposed) {
      return;
    }
    if (state == AppLifecycleState.resumed) {
      _backgrounded = false;
      return;
    }
    if (_backgrounded) {
      return;
    }
    _backgrounded = true;
    unawaited(_cancelWorkflow());
  }

  @override
  void dispose() {
    if (_disposed) {
      return;
    }
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _accountEpoch++;
    _workflowGeneration++;
    _cancelRecordingTimers();
    final terminal = _terminal;
    final subscription = _eventSubscription;
    final operation = _operation;
    final workflowCleanup = _workflowCleanup;
    _terminal = null;
    _eventSubscription = null;
    _completeTerminal(terminal, const _AgentVoiceInputTerminal.cancelled());
    unawaited(() async {
      Future<void>? subscriptionCancellation;
      try {
        subscriptionCancellation = subscription?.cancel();
      } catch (_) {
        // Recorder cleanup remains authoritative during disposal.
      }
      await _discardCurrentBestEffort();
      try {
        await subscriptionCancellation;
      } catch (_) {
        // Recorder cleanup already released the local capture.
      }
      await workflowCleanup;
      try {
        await operation;
      } catch (_) {
        // The disposed fence makes the operation unobservable.
      }
      await _discardCurrentBestEffort();
      await client.dispose();
    }());
    super.dispose();
  }

  void _startRecordingTimers(_AgentVoiceInputFence fence) {
    _cancelRecordingTimers();
    _recordingStartedAt = clock();
    _recordingTicker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!_isCurrent(fence) || _state != AgentVoiceInputState.recording) {
        _cancelRecordingTimers();
        return;
      }
      final startedAt = _recordingStartedAt;
      if (startedAt != null) {
        final elapsed = clock().difference(startedAt);
        _recordingElapsed = elapsed.isNegative ? Duration.zero : elapsed;
        notifyListeners();
      }
    });
    _recordingLimitTimer = Timer(recordingLimit, () {
      if (_isCurrent(fence) && _state == AgentVoiceInputState.recording) {
        unawaited(stopRecording());
      }
    });
  }

  void _cancelRecordingTimers() {
    _recordingTicker?.cancel();
    _recordingLimitTimer?.cancel();
    _recordingTicker = null;
    _recordingLimitTimer = null;
    _recordingStartedAt = null;
  }

  void _showFailure(_AgentVoiceInputFence fence, Object error) {
    if (!_isCurrent(fence)) {
      return;
    }
    _cancelRecordingTimers();
    _liveTranscript = '';
    _editedTranscript = '';
    _state = AgentVoiceInputState.failed;
    _errorMessage = _failureMessage(error);
    _canRetry = _isRetryable(error);
    notifyListeners();
  }

  void _resetPresentation() {
    _state = AgentVoiceInputState.idle;
    _liveTranscript = '';
    _editedTranscript = '';
    _errorMessage = null;
    _canRetry = false;
    _recordingElapsed = Duration.zero;
  }

  _AgentVoiceInputFence _newFence() {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('Agent voice input requires a focused Thread.');
    }
    return _AgentVoiceInputFence(
      accountEpoch: _accountEpoch,
      generation: ++_workflowGeneration,
      threadId: threadId,
    );
  }

  _AgentVoiceInputFence _captureFence() {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('Agent voice input requires a focused Thread.');
    }
    return _AgentVoiceInputFence(
      accountEpoch: _accountEpoch,
      generation: _workflowGeneration,
      threadId: threadId,
    );
  }

  bool _isCurrent(_AgentVoiceInputFence fence) =>
      !_disposed &&
      fence.accountEpoch == _accountEpoch &&
      fence.generation == _workflowGeneration &&
      fence.threadId == _threadId;

  String _newId(String scope) {
    final id = idFactory(scope);
    if (id.length < 8 || id.length > 128) {
      throw StateError('Agent voice input identity is invalid.');
    }
    return id;
  }

  Future<void> _discardCurrentBestEffort() async {
    try {
      await recorder.discardCurrent();
    } catch (_) {
      // Cleanup remains fenced even if the platform cannot remove the file.
    }
  }

  void _cancelSubscriptionWithoutWaiting(
    StreamSubscription<AgentVoiceInputEvent>? subscription,
  ) {
    if (subscription == null) {
      return;
    }
    unawaited(subscription.cancel().catchError((_) {}));
  }

  bool _isRetryable(Object error) => switch (error) {
    AgentVoiceInputFailure(:final retryable) => retryable,
    AgentClientException(:final kind, :final retryable) =>
      retryable ||
          kind == AgentClientFailureKind.network ||
          kind == AgentClientFailureKind.rateLimited ||
          kind == AgentClientFailureKind.server,
    AgentVoiceRecordingException(:final kind) =>
      kind != AgentVoiceRecordingFailureKind.permissionDenied,
    TimeoutException() => true,
    _ => false,
  };

  String _failureMessage(Object error) => switch (error) {
    AgentVoiceRecordingException(
      kind: AgentVoiceRecordingFailureKind.permissionDenied,
    ) =>
      '请允许使用麦克风后再试。',
    AgentVoiceRecordingException(
      kind: AgentVoiceRecordingFailureKind.emptyAudio,
    ) =>
      '没有检测到语音，请重试。',
    AgentClientException(kind: AgentClientFailureKind.authenticationRequired) =>
      '登录状态已失效，请重新登录。',
    AgentClientException(kind: AgentClientFailureKind.network) ||
    TimeoutException() => '语音识别连接中断，请重试。',
    AgentVoiceInputFailure() => '语音识别失败，请重试。',
    _ => '暂时无法识别语音，请重试。',
  };

  static void _completeTerminal(
    Completer<_AgentVoiceInputTerminal>? terminal,
    _AgentVoiceInputTerminal result,
  ) {
    if (terminal != null && !terminal.isCompleted) {
      terminal.complete(result);
    }
  }
}

final class _AgentVoiceInputFence {
  const _AgentVoiceInputFence({
    required this.accountEpoch,
    required this.generation,
    required this.threadId,
  });

  final int accountEpoch;
  final int generation;
  final String threadId;
}

final class _AgentVoiceInputTerminal {
  const _AgentVoiceInputTerminal.completed(this.transcript)
    : error = null,
      stackTrace = null,
      cancelled = false,
      serverTerminal = true;

  const _AgentVoiceInputTerminal.failure(this.error, this.stackTrace)
    : transcript = null,
      cancelled = false,
      serverTerminal = false;

  const _AgentVoiceInputTerminal.serverFailure(this.error, this.stackTrace)
    : transcript = null,
      cancelled = false,
      serverTerminal = true;

  const _AgentVoiceInputTerminal.cancelled()
    : transcript = null,
      error = null,
      stackTrace = null,
      cancelled = true,
      serverTerminal = false;

  final String? transcript;
  final Object? error;
  final StackTrace? stackTrace;
  final bool cancelled;
  final bool serverTerminal;
}
