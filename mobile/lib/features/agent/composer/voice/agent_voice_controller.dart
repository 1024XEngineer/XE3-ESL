import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart';

import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'agent_voice_client.dart';
import 'agent_voice_models.dart';
import 'agent_voice_recording.dart';

typedef AgentVoiceIdFactory = String Function(String scope);
typedef AgentVoiceControllerClock = DateTime Function();
typedef AgentVoiceMessagesCommitted =
    void Function(Iterable<AgentMessage> messages);
typedef AgentVoiceAssistantCommitted =
    FutureOr<void> Function(AgentMessage message);
typedef AgentVoiceAssistantStreamStarted =
    Future<void> Function(String transientMessageId);
typedef AgentVoiceAssistantStreamDelta =
    void Function(String transientMessageId, String delta);
typedef AgentVoiceAssistantStreamCompleted =
    void Function(String transientMessageId, AgentMessage message);
typedef AgentVoiceAssistantStreamFailed =
    void Function(String transientMessageId);
typedef AgentVoiceStreamMessageChanged =
    void Function(String? previousMessageId, AgentMessage message);

final class AgentVoiceController extends ChangeNotifier
    with WidgetsBindingObserver {
  AgentVoiceController({
    required this.client,
    required this.recorder,
    required this.audioPlayer,
    required this.onMessagesCommitted,
    this.onAssistantCommitted,
    this.onAssistantStreamStarted,
    this.onAssistantStreamDelta,
    this.onAssistantStreamCompleted,
    this.onAssistantStreamFailed,
    this.onStreamMessageChanged,
    required this.idFactory,
    this.clock = DateTime.now,
    this.pollInterval = const Duration(seconds: 1),
    this.maximumDraftPolls = 75,
    this.maximumRunPolls = 75,
    this.recordingLimit = const Duration(seconds: 58),
    this.realtimeCompletionTimeout = const Duration(seconds: 75),
    this.workflowCleanupTimeout = const Duration(seconds: 2),
  }) {
    if (pollInterval.isNegative ||
        maximumDraftPolls < 1 ||
        maximumRunPolls < 1 ||
        recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 60) ||
        realtimeCompletionTimeout <= Duration.zero ||
        workflowCleanupTimeout <= Duration.zero) {
      throw ArgumentError('Agent voice controller configuration is invalid.');
    }
    _completionSubscription = audioPlayer.onComplete.listen((_) {
      _handlePlaybackCompletion();
    });
    WidgetsBinding.instance.addObserver(this);
  }

  final AgentVoiceClient client;
  final AgentVoiceRecorder recorder;
  final AgentAudioPlayer audioPlayer;
  final AgentVoiceMessagesCommitted onMessagesCommitted;
  final AgentVoiceAssistantCommitted? onAssistantCommitted;
  final AgentVoiceAssistantStreamStarted? onAssistantStreamStarted;
  final AgentVoiceAssistantStreamDelta? onAssistantStreamDelta;
  final AgentVoiceAssistantStreamCompleted? onAssistantStreamCompleted;
  final AgentVoiceAssistantStreamFailed? onAssistantStreamFailed;
  final AgentVoiceStreamMessageChanged? onStreamMessageChanged;
  final AgentVoiceIdFactory idFactory;
  final AgentVoiceControllerClock clock;
  final Duration pollInterval;
  final int maximumDraftPolls;
  final int maximumRunPolls;
  final Duration recordingLimit;
  final Duration realtimeCompletionTimeout;
  final Duration workflowCleanupTimeout;

  String? _threadId;
  AgentVoiceComposerState _state = AgentVoiceComposerState.idle;
  AgentVoiceLocalRecording? _recording;
  AgentVoiceDraft? _draft;
  AgentVoiceRun? _pendingRun;
  String? _completedStreamMessageId;
  String? _completedStreamText;
  String _editedTranscript = '';
  String _liveTranscript = '';
  String? _errorMessage;
  _VoiceRetry? _retry;
  String? _uploadIdempotencyKey;
  _AsrRetryCommand? _ambiguousAsrRetry;
  _ConfirmationCommand? _confirmationCommand;
  String? _runRetrySourceRunId;
  String? _runRetryId;
  DateTime? _recordingStartedAt;
  Duration _recordingElapsed = Duration.zero;
  Timer? _recordingTicker;
  Timer? _recordingLimitTimer;
  int _accountEpoch = 0;
  int _workflowGeneration = 0;
  int _draftPlaybackGeneration = 0;
  bool _disposed = false;
  bool _backgrounded = false;
  int _lifecycleGeneration = 0;
  Future<void>? _workflowOperation;
  _RealtimeDraftSession? _realtimeSession;
  Future<void>? _workflowCleanup;
  Future<void>? _cleanupFuture;

  bool _draftPlaying = false;
  StreamSubscription<void>? _completionSubscription;

  String? get threadId => _threadId;
  AgentVoiceComposerState get state => _state;
  AgentVoiceLocalRecording? get recording => _recording;
  AgentVoiceDraft? get draft => _draft;
  String get editedTranscript => _editedTranscript;
  String get liveTranscript => _liveTranscript;
  String? get errorMessage => _errorMessage;
  Duration get recordingElapsed => _recordingElapsed;
  bool get hasActiveWorkflow => _state != AgentVoiceComposerState.idle;
  bool get canRetry => _retry != null;
  bool get canStartRecording =>
      !_disposed &&
      !_backgrounded &&
      _workflowCleanup == null &&
      _workflowOperation == null &&
      _threadId != null &&
      _state == AgentVoiceComposerState.idle;
  bool get canStopRecording => _state == AgentVoiceComposerState.recording;
  bool get canUpload =>
      _recording != null &&
      (_state == AgentVoiceComposerState.recorded ||
          (_state == AgentVoiceComposerState.failed &&
              _retry == _VoiceRetry.upload));
  bool get canConfirm =>
      _draft?.isReady == true &&
      _editedTranscript.trim().isNotEmpty &&
      utf8.encode(_editedTranscript.trim()).length <= 16384 &&
      _state == AgentVoiceComposerState.awaitingConfirmation;
  bool get isDraftPlaying => _draftPlaying;

  /// Rebinds local voice-draft state to the authoritative focused Thread.
  ///
  /// A local playback cleanup failure is surfaced as retryable Composer state;
  /// it never rolls back or desynchronizes the server-confirmed Thread focus.
  Future<void> bindThread(String? threadId) async {
    final cleanup = _workflowCleanup;
    if (cleanup != null) {
      await cleanup;
    }
    if (threadId == _threadId) {
      // A concurrent bind may have installed cleanup after the first await.
      // Starting capture must wait for that cleanup instead of becoming a
      // silent no-op behind [canStartRecording].
      final concurrentCleanup = _workflowCleanup;
      if (concurrentCleanup != null) {
        await concurrentCleanup;
      }
      return;
    }
    final staleRecording = _recording;
    final staleDraft = _draft;
    final staleRealtime = _realtimeSession;
    final staleOperation = _workflowOperation;
    _workflowGeneration++;
    _draftPlaybackGeneration++;
    _cancelRecordingTimers();
    _threadId = threadId;
    _resetWorkflowPresentation();
    _resetDraftPlayback();
    if (!_disposed) {
      notifyListeners();
    }
    try {
      await _startWorkflowCleanup(
        realtime: staleRealtime,
        operation: staleOperation,
        recording: staleRecording,
        draft: staleDraft,
      );
    } catch (_) {
      if (!_disposed && _threadId == threadId) {
        _state = AgentVoiceComposerState.failed;
        _retry = _VoiceRetry.start;
        _errorMessage = '对话已切换，但语音播放未能停止。请重试语音输入。';
        notifyListeners();
      }
    }
  }

  Future<void> startRecording() {
    if (!canStartRecording || _workflowOperation != null) {
      return Future<void>.value();
    }
    final generation = ++_workflowGeneration;
    final fence = _WorkflowFence(
      accountEpoch: _accountEpoch,
      generation: generation,
      threadId: _threadId!,
    );
    _state = AgentVoiceComposerState.starting;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation = _startRecording(fence).whenComplete(() {
      if (identical(_workflowOperation, operation)) {
        _workflowOperation = null;
      }
    });
    _workflowOperation = operation;
    return operation;
  }

  Future<void> _startRecording(_WorkflowFence fence) async {
    try {
      await audioPlayer.stop();
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      if (recorder case final AgentVoiceStreamingRecorder streamingRecorder
          when client is AgentVoiceRealtimeInputClient) {
        final idempotencyKey = _newId('voice-upload');
        final chunks = await streamingRecorder.startAudioStream();
        if (!_isWorkflowCurrent(fence)) {
          await recorder.discardCurrent();
          return;
        }
        _uploadIdempotencyKey = idempotencyKey;
        _realtimeSession = _RealtimeDraftSession(
          events: (client as AgentVoiceRealtimeInputClient).createDraftRealtime(
            threadId: fence.threadId,
            audioChunks: chunks,
            idempotencyKey: idempotencyKey,
          ),
          collect: (events) => _collectRealtimeDraft(fence, events),
        );
      } else {
        await recorder.start();
      }
      if (!_isWorkflowCurrent(fence)) {
        await recorder.discardCurrent();
        return;
      }
      _state = AgentVoiceComposerState.recording;
      _recordingElapsed = Duration.zero;
      _startRecordingTimers(fence);
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        _state = AgentVoiceComposerState.failed;
        _errorMessage = _recordingFailureMessage(error);
        _retry =
            error is AgentVoiceRecordingException &&
                error.kind == AgentVoiceRecordingFailureKind.permissionDenied
            ? null
            : _VoiceRetry.start;
      }
    }
    if (_isWorkflowCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<void> stopRecording() {
    if (!canStopRecording || _workflowOperation != null) {
      return Future<void>.value();
    }
    final fence = _captureWorkflowFence();
    _cancelRecordingTimers();
    late final Future<void> operation;
    operation = _stopRecording(fence).whenComplete(() {
      if (identical(_workflowOperation, operation)) {
        _workflowOperation = null;
      }
    });
    _workflowOperation = operation;
    return operation;
  }

  Future<void> stopRecordingAndUpload() async {
    await stopRecording();
    if (_state == AgentVoiceComposerState.recorded) {
      await upload();
    }
  }

  Future<void> _stopRecording(_WorkflowFence fence) async {
    final realtime = _realtimeSession;
    final usedRealtimeInput = realtime != null;
    try {
      final recording =
          realtime != null && recorder is AgentVoiceStreamingRecorder
          ? await (recorder as AgentVoiceStreamingRecorder).stopAudioStream()
          : await recorder.stop();
      if (!_isWorkflowCurrent(fence)) {
        await recorder.discard(recording);
        return;
      }
      _recording = recording;
      _recordingElapsed = recording.duration;
      if (realtime == null) {
        _uploadIdempotencyKey = _newId('voice-upload');
        _state = AgentVoiceComposerState.recorded;
      } else {
        _state = AgentVoiceComposerState.transcribing;
        notifyListeners();
        final result = await realtime.result.timeout(
          realtimeCompletionTimeout,
          onTimeout: () async {
            await _cancelRealtimeBestEffort(realtime);
            return const _RealtimeDraftResult(
              error: AgentClientException(
                kind: AgentClientFailureKind.network,
                retryable: true,
              ),
            );
          },
        );
        if (identical(_realtimeSession, realtime)) {
          _realtimeSession = null;
        }
        if (!_isWorkflowCurrent(fence)) {
          return;
        }
        if (result.error case final error?) {
          throw error;
        }
        final draft = result.draft;
        if (draft == null) {
          throw StateError('Realtime voice stream ended without a draft.');
        }
        _validateDraft(draft, expectedThreadId: fence.threadId);
        _draft = draft;
        _recording = null;
        await _discardRecordingBestEffort(recording);
        if (!_isWorkflowCurrent(fence)) {
          await _deleteDraftBestEffort(draft.id);
          return;
        }
        await _resolveDraft(fence, draft);
        return;
      }
      _errorMessage = null;
      _retry = null;
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        if (identical(_realtimeSession, realtime)) {
          _realtimeSession = null;
        }
        _state = AgentVoiceComposerState.failed;
        if (usedRealtimeInput && _recording != null) {
          _errorMessage = _uploadFailureMessage(error);
          _retry = _VoiceRetry.upload;
        } else {
          _errorMessage = _recordingFailureMessage(error);
          _retry = _VoiceRetry.start;
        }
      }
    }
    if (_isWorkflowCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<_RealtimeDraftResult> _collectRealtimeDraft(
    _WorkflowFence fence,
    StreamIterator<AgentVoiceTranscriptionEvent> events,
  ) async {
    AgentVoiceDraft? completed;
    try {
      while (await events.moveNext()) {
        final event = events.current;
        if (!_isWorkflowCurrent(fence)) {
          if (event case AgentVoiceDraftCompleted(:final draft)) {
            await _deleteDraftBestEffort(draft.id);
          }
          continue;
        }
        switch (event) {
          case AgentVoiceTranscriptUpdated(:final text):
            _liveTranscript = text;
            notifyListeners();
          case AgentVoiceDraftCompleted(:final draft):
            completed = draft;
        }
      }
      return _RealtimeDraftResult(draft: completed);
    } catch (error) {
      return _RealtimeDraftResult(error: error);
    }
  }

  Future<void> cancel() async {
    final existingCleanup = _workflowCleanup;
    if (existingCleanup != null) {
      await existingCleanup;
      return;
    }
    final staleRecording = _recording;
    final staleDraft = _draft;
    final staleRealtime = _realtimeSession;
    final staleOperation = _workflowOperation;
    _workflowGeneration++;
    _draftPlaybackGeneration++;
    _cancelRecordingTimers();
    _resetWorkflowPresentation();
    _resetDraftPlayback();
    if (!_disposed) {
      notifyListeners();
    }
    await _startWorkflowCleanup(
      realtime: staleRealtime,
      operation: staleOperation,
      recording: staleRecording,
      draft: staleDraft,
    );
  }

  Future<void> rerecord() async {
    final threadId = _threadId;
    await cancel();
    if (!_disposed && threadId != null && threadId == _threadId) {
      await startRecording();
    }
  }

  Future<void> toggleDraftPlayback() async {
    final recording = _recording;
    if (recording == null ||
        (_state != AgentVoiceComposerState.recorded &&
            _state != AgentVoiceComposerState.failed)) {
      return;
    }
    if (_draftPlaying) {
      _draftPlaybackGeneration++;
      _resetDraftPlayback();
      await audioPlayer.stop();
      if (!_disposed) {
        notifyListeners();
      }
      return;
    }
    final fence = _captureDraftPlaybackFence();
    _resetDraftPlayback();
    _draftPlaying = true;
    notifyListeners();
    try {
      await audioPlayer.playFile(recording.path, speed: 1);
      if (!_isDraftPlaybackCurrent(fence)) {
        await audioPlayer.stop();
      }
    } catch (_) {
      if (_isDraftPlaybackCurrent(fence)) {
        _draftPlaying = false;
        _errorMessage = '暂时无法试听这段录音，请重试或重新录音。';
        notifyListeners();
      }
    }
  }

  Future<void> upload() {
    final recording = _recording;
    final threadId = _threadId;
    final key = _uploadIdempotencyKey;
    if (recording == null ||
        threadId == null ||
        key == null ||
        !canUpload ||
        _workflowOperation != null) {
      return Future<void>.value();
    }
    final fence = _captureWorkflowFence();
    _state = AgentVoiceComposerState.uploading;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation = _upload(fence: fence, recording: recording, idempotencyKey: key)
        .whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    return operation;
  }

  Future<void> _upload({
    required _WorkflowFence fence,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) async {
    try {
      AgentVoiceDraft? completedDraft;
      await for (final event in client.createDraftStream(
        threadId: fence.threadId,
        recording: recording,
        idempotencyKey: idempotencyKey,
      )) {
        if (!_isWorkflowCurrent(fence)) {
          if (event case AgentVoiceDraftCompleted(:final draft)) {
            await _deleteDraftBestEffort(draft.id);
          }
          return;
        }
        switch (event) {
          case AgentVoiceTranscriptUpdated(:final text):
            _liveTranscript = text;
            _state = AgentVoiceComposerState.transcribing;
            notifyListeners();
          case AgentVoiceDraftCompleted(:final draft):
            completedDraft = draft;
        }
      }
      final draft = completedDraft;
      if (draft == null) {
        throw StateError('Voice transcription stream ended without a draft.');
      }
      if (!_isWorkflowCurrent(fence)) {
        await _deleteDraftBestEffort(draft.id);
        await _discardRecordingBestEffort(recording);
        return;
      }
      _validateDraft(draft, expectedThreadId: fence.threadId);
      _draft = draft;
      _recording = null;
      await _discardRecordingBestEffort(recording);
      if (!_isWorkflowCurrent(fence)) {
        await _deleteDraftBestEffort(draft.id);
        return;
      }
      await _resolveDraft(fence, draft);
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        _state = AgentVoiceComposerState.failed;
        final uploadCommitted = _draft != null;
        _errorMessage = uploadCommitted
            ? _asrFailureMessage(error)
            : _uploadFailureMessage(error);
        _retry = uploadCommitted
            ? _VoiceRetry.restoreDraft
            : _VoiceRetry.upload;
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    }
  }

  Future<void> _resolveDraft(
    _WorkflowFence fence,
    AgentVoiceDraft initial,
  ) async {
    var draft = initial;
    for (var attempt = 0; attempt < maximumDraftPolls; attempt++) {
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      _validateDraft(draft, expectedThreadId: fence.threadId);
      _draft = draft;
      if (draft.isReady) {
        _editedTranscript = draft.transcript!.text;
        _liveTranscript = _editedTranscript;
        _state = AgentVoiceComposerState.awaitingConfirmation;
        _retry = null;
        _errorMessage = null;
        notifyListeners();
        return;
      }
      if (draft.status == AgentVoiceDraftStatus.failed) {
        _state = AgentVoiceComposerState.failed;
        _retry = draft.failure?.retryable == true ? _VoiceRetry.asr : null;
        _errorMessage = draft.failure?.retryable == true
            ? '语音转写没有完成，可以重试识别。'
            : '语音转写失败，请取消后重新录音。';
        notifyListeners();
        return;
      }
      if (!draft.isAsrPending) {
        throw StateError('Unexpected Agent voice draft state.');
      }
      _state = AgentVoiceComposerState.transcribing;
      notifyListeners();
      await _waitForPoll();
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      final restored = await client.getDraft(draftId: draft.id);
      if (restored.version < draft.version) {
        throw StateError('Agent voice draft version moved backwards.');
      }
      draft = restored;
    }
    if (_isWorkflowCurrent(fence)) {
      _state = AgentVoiceComposerState.failed;
      _retry = _VoiceRetry.restoreDraft;
      _errorMessage = '转写仍在处理中，请稍后继续检查。';
      notifyListeners();
    }
  }

  Future<void> retry() async {
    final retry = _retry;
    if (retry == null || _workflowOperation != null || _disposed) {
      return;
    }
    switch (retry) {
      case _VoiceRetry.start:
        _state = AgentVoiceComposerState.idle;
        _retry = null;
        await startRecording();
      case _VoiceRetry.upload:
        await upload();
      case _VoiceRetry.asr:
        await retryAsr();
      case _VoiceRetry.restoreDraft:
        await restoreDraft();
      case _VoiceRetry.confirm:
        await _retryConfirmation();
      case _VoiceRetry.run:
        await retryAssistant();
      case _VoiceRetry.message:
        await retryAssistantMessage();
    }
  }

  Future<void> retryAsr() async {
    final draft = _draft;
    if (draft == null ||
        draft.failure?.retryable != true ||
        _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence(
      draftId: draft.id,
      draftVersion: draft.version,
    );
    _state = AgentVoiceComposerState.transcribing;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        Future<void>.sync(() async {
          var retryPosted = false;
          try {
            final ambiguous = _ambiguousAsrRetry;
            if (ambiguous != null &&
                (ambiguous.draftId != draft.id ||
                    ambiguous.draftVersion != draft.version)) {
              _ambiguousAsrRetry = null;
            }
            if (ambiguous != null &&
                ambiguous.draftId == draft.id &&
                ambiguous.draftVersion == draft.version) {
              final durable = await client.getDraft(draftId: draft.id);
              _validateDraft(durable, expectedThreadId: fence.threadId);
              if (durable.id != draft.id || durable.version < draft.version) {
                throw StateError(
                  'Agent voice draft reconciliation moved backwards.',
                );
              }
              if (!_isWorkflowCurrent(fence, allowDraftVersionAdvance: true)) {
                return;
              }
              if (durable.version != draft.version ||
                  durable.status != AgentVoiceDraftStatus.failed ||
                  durable.failure?.retryable != true) {
                _ambiguousAsrRetry = null;
                await _resolveDraft(fence.withoutDraftVersion(), durable);
                return;
              }
            }
            _ambiguousAsrRetry = null;
            retryPosted = true;
            final retried = await client.retryDraft(draftId: draft.id);
            _ambiguousAsrRetry = null;
            if (!_isWorkflowCurrent(fence, allowDraftVersionAdvance: true)) {
              return;
            }
            if (retried.version <= draft.version) {
              throw StateError('ASR retry did not advance draft version.');
            }
            await _resolveDraft(fence.withoutDraftVersion(), retried);
          } catch (error) {
            if (_isWorkflowCurrent(fence, allowDraftVersionAdvance: true)) {
              if (retryPosted && _isAmbiguousMutationFailure(error)) {
                _ambiguousAsrRetry = _AsrRetryCommand(
                  draftId: draft.id,
                  draftVersion: draft.version,
                );
              }
              _state = AgentVoiceComposerState.failed;
              _retry = _VoiceRetry.asr;
              _errorMessage = _asrFailureMessage(error);
              notifyListeners();
              await _clearOnAuthenticationFailure(error);
            }
          }
        }).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    await operation;
  }

  Future<void> restoreDraft() async {
    final draft = _draft;
    if (draft == null || _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence(draftId: draft.id);
    _state = AgentVoiceComposerState.transcribing;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        Future<void>.sync(() async {
          try {
            final restored = await client.getDraft(draftId: draft.id);
            if (_isWorkflowCurrent(fence, allowDraftVersionAdvance: true)) {
              await _resolveDraft(fence.withoutDraftVersion(), restored);
            }
          } catch (error) {
            if (_isWorkflowCurrent(fence, allowDraftVersionAdvance: true)) {
              _state = AgentVoiceComposerState.failed;
              _retry = _VoiceRetry.restoreDraft;
              _errorMessage = _asrFailureMessage(error);
              notifyListeners();
              await _clearOnAuthenticationFailure(error);
            }
          }
        }).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    await operation;
  }

  void updateTranscript(String value) {
    if (_state != AgentVoiceComposerState.awaitingConfirmation ||
        value.runes.length > 4096 ||
        utf8.encode(value).length > 16384) {
      return;
    }
    _editedTranscript = value;
    notifyListeners();
  }

  Future<void> confirm() {
    final draft = _draft;
    final text = _editedTranscript.trim();
    if (draft == null ||
        !draft.isReady ||
        !canConfirm ||
        _workflowOperation != null ||
        text.isEmpty) {
      return Future<void>.value();
    }
    final command = _ConfirmationCommand(
      draftId: draft.id,
      draftVersion: draft.version,
      clientMessageId: _newId('voice-message'),
      confirmedText: text,
    );
    _confirmationCommand = command;
    if (client is AgentVoiceStreamingClient) {
      return _startConfirmationStream(
        client: client as AgentVoiceStreamingClient,
        draft: draft,
        command: command,
      );
    }
    return _startConfirmation(
      draft: draft,
      command: command,
      reconcileFirst: false,
    );
  }

  Future<void> _startConfirmationStream({
    required AgentVoiceStreamingClient client,
    required AgentVoiceDraft draft,
    required _ConfirmationCommand command,
  }) {
    final fence = _captureWorkflowFence(
      draftId: draft.id,
      draftVersion: draft.version,
    );
    _state = AgentVoiceComposerState.confirming;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        _confirmStream(
          client: client,
          fence: fence,
          draft: draft,
          command: command,
        ).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    return operation;
  }

  Future<void> _confirmStream({
    required AgentVoiceStreamingClient client,
    required _WorkflowFence fence,
    required AgentVoiceDraft draft,
    required _ConfirmationCommand command,
  }) async {
    var messageCommitted = false;
    var assistantText = '';
    String? transientAssistantId;
    try {
      await for (final event in client.confirmDraftStream(
        draftId: command.draftId,
        draftVersion: command.draftVersion,
        clientMessageId: command.clientMessageId,
        confirmedText: command.confirmedText,
      )) {
        if (!_isWorkflowCurrent(fence)) {
          return;
        }
        switch (event) {
          case AgentVoiceInputCommitted(:final confirmation):
            _validateConfirmation(
              confirmation,
              expectedDraft: draft,
              expectedClientMessageId: command.clientMessageId,
              confirmedText: command.confirmedText,
            );
            _draft = confirmation.draft;
            _pendingRun = confirmation.run;
            messageCommitted = true;
            _commitMessages(<AgentMessage>[confirmation.message]);
            _editedTranscript = '';
            _recording = null;
            _uploadIdempotencyKey = null;
            _confirmationCommand = null;
            _state = AgentVoiceComposerState.awaitingAssistant;
            notifyListeners();
          case AgentVoiceToolStepEvent():
            break;
          case AgentVoiceAssistantOutputStarted(:final outputId):
            transientAssistantId = outputId;
            _changeStreamMessage(
              null,
              AgentMessage(
                id: outputId,
                role: AgentMessageRole.assistant,
                text: '',
                isStreaming: true,
              ),
            );
            await _startAssistantStream(outputId);
          case AgentVoiceAssistantOutputDelta(:final delta):
            final messageId = transientAssistantId;
            if (messageId == null) {
              throw StateError('Voice assistant delta started out of order.');
            }
            assistantText += delta;
            _changeStreamMessage(
              messageId,
              AgentMessage(
                id: messageId,
                role: AgentMessageRole.assistant,
                text: assistantText,
                isStreaming: true,
              ),
            );
            _appendAssistantStream(messageId, delta);
          case AgentVoiceAssistantOutputCompleted(:final outputId, :final text):
            if (transientAssistantId != outputId || assistantText != text) {
              throw StateError(
                'Voice assistant output completed inconsistently.',
              );
            }
            _completedStreamMessageId = outputId;
            _completedStreamText = text;
            final completed = AgentMessage(
              id: outputId,
              role: AgentMessageRole.assistant,
              text: text,
            );
            _completeAssistantStream(outputId, completed);
            _changeStreamMessage(outputId, completed);
          case AgentVoiceRunCompleted(:final run):
            final initial = _pendingRun;
            if (initial == null) {
              throw StateError('Completed voice Run has no committed input.');
            }
            _validateRun(
              run,
              expectedThreadId: fence.threadId,
              expectedRunId: initial.id,
              expectedInputMessageId: initial.inputMessageId,
              expectedAttempt: initial.attempt,
              expectedRetryOfRunId: initial.retryOfRunId,
              expectedClientRetryId: initial.clientRetryId,
            );
            if (run.status != AgentRunStatus.completed) {
              throw StateError('Voice Run completion event was not completed.');
            }
            _pendingRun = run;
            try {
              await _hydrateCompletedRun(
                fence,
                run,
                expectedMessageId: transientAssistantId,
                expectedText: assistantText,
              );
            } catch (error) {
              if (_isWorkflowCurrent(fence)) {
                _setMessageHydrationFailure(error);
                await _clearOnAuthenticationFailure(error);
              }
            }
            return;
          case AgentVoiceRunFailed(:final run, :final kind, :final retryable):
            if (run != null) {
              final initial = _pendingRun;
              if (initial == null) {
                throw StateError('Failed voice Run has no committed input.');
              }
              _validateRun(
                run,
                expectedThreadId: fence.threadId,
                expectedRunId: initial.id,
                expectedInputMessageId: initial.inputMessageId,
                expectedAttempt: initial.attempt,
                expectedRetryOfRunId: initial.retryOfRunId,
                expectedClientRetryId: initial.clientRetryId,
              );
              if (run.status != AgentRunStatus.failed ||
                  run.failureKind != kind ||
                  run.failureRetryable != retryable) {
                throw StateError('Voice Run failure event is inconsistent.');
              }
              _pendingRun = run;
            }
            throw AgentClientException(
              kind: AgentClientFailureKind.runFailed,
              errorCode: kind,
              retryable: retryable,
            );
        }
      }
    } catch (error) {
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      if (transientAssistantId case final messageId?) {
        _failAssistantStream(messageId);
        _changeStreamMessage(
          messageId,
          AgentMessage(
            id: messageId,
            role: AgentMessageRole.assistant,
            text: assistantText,
            hasFailed: true,
          ),
        );
      }
      if (messageCommitted) {
        final run = _pendingRun;
        final retryable = switch (run?.status) {
          AgentRunStatus.failed => run!.failureRetryable,
          _ => error is! AgentClientException || error.retryable,
        };
        _state = AgentVoiceComposerState.failed;
        _retry = retryable ? _VoiceRetry.run : null;
        _errorMessage = retryable
            ? '回复中断了，可以重试；你的语音消息已保留。'
            : 'Agent 回复失败；你的语音消息与确认文字已保留。';
      } else if (_isAmbiguousMutationFailure(error)) {
        _state = AgentVoiceComposerState.failed;
        _retry = _VoiceRetry.confirm;
        _errorMessage = _confirmationFailureMessage(error);
      } else {
        _confirmationCommand = null;
        _state = AgentVoiceComposerState.awaitingConfirmation;
        _retry = null;
        _errorMessage = _confirmationFailureMessage(error);
      }
      notifyListeners();
      await _clearOnAuthenticationFailure(error);
    }
  }

  void _commitMessages(Iterable<AgentMessage> messages) {
    try {
      onMessagesCommitted(messages);
    } catch (_) {
      // Presentation callbacks cannot change the authoritative Agent result.
    }
  }

  void _changeStreamMessage(String? previousMessageId, AgentMessage message) {
    try {
      final callback = onStreamMessageChanged;
      if (callback == null) {
        _commitMessages(<AgentMessage>[message]);
        return;
      }
      callback(previousMessageId, message);
    } catch (_) {
      // Presentation callbacks cannot change the authoritative Agent result.
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
      // Speech presentation cannot change the authoritative Agent result.
    }
  }

  Future<void> _hydrateCompletedRun(
    _WorkflowFence fence,
    AgentRun run, {
    String? expectedMessageId,
    String? expectedText,
  }) async {
    final messageId = run.assistantMessageId;
    if (run.status != AgentRunStatus.completed || messageId == null) {
      throw StateError('Completed voice Run has no assistant Message.');
    }
    final assistant = await client.getMessage(
      threadId: fence.threadId,
      messageId: messageId,
    );
    if (!_isWorkflowCurrent(fence)) {
      return;
    }
    if (assistant == null) {
      throw const _AssistantMessageHydrationPending();
    }
    if (assistant.id != messageId ||
        assistant.role != AgentMessageRole.assistant ||
        assistant.producedByRunId != run.id ||
        assistant.text.trim().isEmpty ||
        (expectedMessageId != null && assistant.id != expectedMessageId) ||
        (expectedText != null && assistant.text != expectedText)) {
      throw StateError('Invalid assistant Message after voice Run.');
    }
    if (expectedMessageId == null) {
      _commitMessages(<AgentMessage>[assistant]);
    } else {
      _changeStreamMessage(expectedMessageId, assistant);
    }
    _resetWorkflowPresentation();
    notifyListeners();
    _notifyAssistantCommitted(assistant);
  }

  void _setMessageHydrationFailure(Object error) {
    _state = AgentVoiceComposerState.failed;
    _retry = _VoiceRetry.message;
    _errorMessage =
        error is AgentClientException &&
            error.kind == AgentClientFailureKind.authenticationRequired
        ? '登录状态已失效，请重新登录。'
        : '回复已完成，但确认入口暂时无法读取。请重试刷新。';
    notifyListeners();
  }

  Future<void> _retryConfirmation() {
    final draft = _draft;
    final command = _confirmationCommand;
    if (draft == null ||
        command == null ||
        draft.id != command.draftId ||
        draft.version != command.draftVersion ||
        _workflowOperation != null) {
      return Future<void>.value();
    }
    return _startConfirmation(
      draft: draft,
      command: command,
      reconcileFirst: true,
    );
  }

  Future<void> _startConfirmation({
    required AgentVoiceDraft draft,
    required _ConfirmationCommand command,
    required bool reconcileFirst,
  }) {
    final fence = _captureWorkflowFence(
      draftId: draft.id,
      draftVersion: draft.version,
    );
    _state = AgentVoiceComposerState.confirming;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        _confirm(
          fence: fence,
          draft: draft,
          command: command,
          reconcileFirst: reconcileFirst,
        ).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    return operation;
  }

  Future<void> _confirm({
    required _WorkflowFence fence,
    required AgentVoiceDraft draft,
    required _ConfirmationCommand command,
    required bool reconcileFirst,
  }) async {
    var messageCommitted = false;
    try {
      final confirmation = reconcileFirst
          ? await _reconcileConfirmation(fence, command)
          : await client.confirmDraft(
              draftId: command.draftId,
              draftVersion: command.draftVersion,
              clientMessageId: command.clientMessageId,
              confirmedText: command.confirmedText,
            );
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      _validateConfirmation(
        confirmation,
        expectedDraft: draft,
        expectedClientMessageId: command.clientMessageId,
        confirmedText: command.confirmedText,
      );
      _draft = confirmation.draft;
      _pendingRun = confirmation.run;
      messageCommitted = true;
      _commitMessages(<AgentMessage>[confirmation.message]);
      if (confirmation.assistantMessage case final assistant?) {
        _commitMessages(<AgentMessage>[assistant]);
      }
      _editedTranscript = '';
      _recording = null;
      _uploadIdempotencyKey = null;
      _confirmationCommand = null;
      _retry = null;
      _errorMessage = null;
      if (confirmation.assistantMessage != null) {
        _resetWorkflowPresentation();
        notifyListeners();
        _notifyAssistantCommitted(confirmation.assistantMessage!);
        return;
      }
      _state = AgentVoiceComposerState.awaitingAssistant;
      notifyListeners();
      if (_backgrounded) {
        return;
      }
      await _resolveRun(fence.withoutDraft(), confirmation.run);
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        if (messageCommitted &&
            _pendingRun?.status == AgentRunStatus.completed) {
          _setMessageHydrationFailure(error);
        } else if (messageCommitted) {
          _state = AgentVoiceComposerState.failed;
          _retry = _VoiceRetry.run;
          _errorMessage = '暂时无法恢复 Agent 回复。你的语音消息已保留。';
        } else if (error is _ConfirmationReconciliationPending ||
            (reconcileFirst && !_isConfirmationConflict(error)) ||
            _isAmbiguousMutationFailure(error)) {
          _state = AgentVoiceComposerState.failed;
          _retry = _VoiceRetry.confirm;
          _errorMessage = _confirmationFailureMessage(error);
        } else {
          _confirmationCommand = null;
          _state = AgentVoiceComposerState.awaitingConfirmation;
          _retry = null;
          _errorMessage = _confirmationFailureMessage(error);
        }
        if (_retry != _VoiceRetry.message) {
          notifyListeners();
        }
        await _clearOnAuthenticationFailure(error);
      }
    }
  }

  Future<AgentVoiceConfirmation> _reconcileConfirmation(
    _WorkflowFence fence,
    _ConfirmationCommand command,
  ) async {
    final durable = await client.getDraft(draftId: command.draftId);
    _validateDraft(durable, expectedThreadId: fence.threadId);
    if (durable.id != command.draftId ||
        durable.version != command.draftVersion) {
      throw const _ConfirmationCommandConflict();
    }
    if (_hasConfirmationProjection(durable)) {
      final messageId = durable.confirmedMessageId!;
      final runId = durable.confirmedRunId!;
      final results = await Future.wait<Object?>([
        client.getMessage(threadId: fence.threadId, messageId: messageId),
        client.getRun(runId: runId),
      ]);
      final message = results[0] as AgentMessage?;
      final run = results[1] as AgentVoiceRun;
      if (message == null) {
        throw const _ConfirmationReconciliationPending();
      }
      return AgentVoiceConfirmation(draft: durable, message: message, run: run);
    }
    if (durable.status == AgentVoiceDraftStatus.ready) {
      return client.confirmDraft(
        draftId: command.draftId,
        draftVersion: command.draftVersion,
        clientMessageId: command.clientMessageId,
        confirmedText: command.confirmedText,
      );
    }
    throw const _ConfirmationCommandConflict();
  }

  Future<void> _resolveRun(_WorkflowFence fence, AgentVoiceRun initial) async {
    var run = initial;
    for (var attempt = 0; attempt < maximumRunPolls; attempt++) {
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      _validateRun(
        run,
        expectedThreadId: fence.threadId,
        expectedRunId: initial.id,
        expectedInputMessageId: initial.inputMessageId,
        expectedAttempt: initial.attempt,
        expectedRetryOfRunId: initial.retryOfRunId,
        expectedClientRetryId: initial.clientRetryId,
      );
      _pendingRun = run;
      if (run.status == AgentVoiceRunStatus.failed) {
        _state = AgentVoiceComposerState.failed;
        _retry = run.failureRetryable ? _VoiceRetry.run : null;
        _errorMessage = run.failureRetryable
            ? 'Agent 回复没有完成，可以重试；你的语音消息已保留。'
            : 'Agent 回复失败；你的语音消息与确认文字已保留。';
        notifyListeners();
        return;
      }
      if (run.status == AgentVoiceRunStatus.completed) {
        try {
          await _hydrateCompletedRun(fence, run);
        } catch (error) {
          if (_isWorkflowCurrent(fence)) {
            _setMessageHydrationFailure(error);
            await _clearOnAuthenticationFailure(error);
          }
        }
        return;
      }
      await _waitForPoll();
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      run = await client.getRun(runId: run.id);
    }
    if (_isWorkflowCurrent(fence)) {
      _state = AgentVoiceComposerState.failed;
      _retry = _VoiceRetry.run;
      _errorMessage = 'Agent 仍在回复，可以稍后继续检查。你的语音消息已保留。';
      notifyListeners();
    }
  }

  Future<void> retryAssistant() async {
    final run = _pendingRun;
    if (run == null || _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence();
    _state = AgentVoiceComposerState.awaitingAssistant;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        Future<void>.sync(() async {
          try {
            AgentVoiceRun next;
            if (run.status == AgentVoiceRunStatus.failed &&
                run.failureRetryable) {
              if (_runRetrySourceRunId != run.id) {
                _runRetrySourceRunId = run.id;
                _runRetryId = _newId('voice-run-retry');
              }
              next = await client.retryRun(
                runId: run.id,
                clientRetryId: _runRetryId!,
              );
              _validateRun(
                next,
                expectedThreadId: fence.threadId,
                expectedInputMessageId: run.inputMessageId,
                expectedAttempt: run.attempt + 1,
                expectedRetryOfRunId: run.id,
                expectedClientRetryId: _runRetryId,
              );
              if (next.id == run.id) {
                throw StateError('Agent Run retry did not create a new Run.');
              }
            } else {
              next = await client.getRun(runId: run.id);
              _validateRun(
                next,
                expectedThreadId: fence.threadId,
                expectedRunId: run.id,
                expectedInputMessageId: run.inputMessageId,
                expectedAttempt: run.attempt,
                expectedRetryOfRunId: run.retryOfRunId,
                expectedClientRetryId: run.clientRetryId,
              );
            }
            if (_isWorkflowCurrent(fence)) {
              await _resolveRun(fence, next);
            }
          } catch (error) {
            if (_isWorkflowCurrent(fence)) {
              if (_pendingRun?.status == AgentRunStatus.completed) {
                _setMessageHydrationFailure(error);
              } else {
                _state = AgentVoiceComposerState.failed;
                _retry = _VoiceRetry.run;
                _errorMessage = '暂时无法恢复 Agent 回复。你的语音消息已保留。';
                notifyListeners();
              }
              await _clearOnAuthenticationFailure(error);
            }
          }
        }).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    await operation;
  }

  Future<void> retryAssistantMessage() async {
    final run = _pendingRun;
    if (run == null ||
        run.status != AgentRunStatus.completed ||
        _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence();
    _state = AgentVoiceComposerState.awaitingAssistant;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        Future<void>.sync(() async {
          try {
            await _hydrateCompletedRun(
              fence,
              run,
              expectedMessageId: _completedStreamMessageId,
              expectedText: _completedStreamText,
            );
          } catch (error) {
            if (_isWorkflowCurrent(fence)) {
              _setMessageHydrationFailure(error);
              await _clearOnAuthenticationFailure(error);
            }
          }
        }).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    await operation;
  }

  Future<void> _startWorkflowCleanup({
    required _RealtimeDraftSession? realtime,
    required Future<void>? operation,
    required AgentVoiceLocalRecording? recording,
    required AgentVoiceDraft? draft,
  }) {
    final existing = _workflowCleanup;
    if (existing != null) {
      return existing;
    }
    late final Future<void> cleanup;
    cleanup =
        Future<void>.sync(() async {
          await _cancelRealtimeBestEffort(realtime);
          await _awaitOperationBestEffort(operation);
          await Future.wait<void>([
            _discardCurrentBestEffort(),
            if (recording != null) _discardRecordingBestEffort(recording),
            Future<void>.sync(audioPlayer.stop),
            if (draft != null && !_isConfirmed(draft))
              _deleteDraftBestEffort(draft.id),
          ]);
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

  Future<void> _cancelRealtimeBestEffort(
    _RealtimeDraftSession? realtime,
  ) async {
    if (realtime == null) {
      return;
    }
    try {
      await realtime.cancel().timeout(workflowCleanupTimeout);
    } catch (_) {
      // The workflow fence and recorder cleanup still isolate stale input.
    }
  }

  Future<void> _awaitOperationBestEffort(Future<void>? operation) async {
    if (operation == null) {
      return;
    }
    try {
      await operation.timeout(workflowCleanupTimeout);
    } catch (_) {
      // The generation fence prevents the stale operation from publishing.
      // Keeping `_workflowOperation` set until its source actually completes
      // also prevents a new recording from running concurrently with it.
    }
  }

  Future<void> clearPrivateState({bool clearClient = true}) async {
    final existing = _cleanupFuture;
    if (existing != null) {
      await existing;
      return;
    }
    final staleRecording = _recording;
    final staleDraft = _draft;
    final staleRealtime = _realtimeSession;
    final staleOperation = _workflowOperation;
    _accountEpoch++;
    _workflowGeneration++;
    _draftPlaybackGeneration++;
    _lifecycleGeneration++;
    _backgrounded = false;
    _cancelRecordingTimers();
    _threadId = null;
    _resetWorkflowPresentation();
    _resetDraftPlayback();
    if (!_disposed) {
      notifyListeners();
    }
    final cleanup = Future<void>.sync(() async {
      await _startWorkflowCleanup(
        realtime: staleRealtime,
        operation: staleOperation,
        recording: staleRecording,
        draft: staleDraft,
      );
      await Future.wait<void>([
        Future<void>.sync(recorder.clearAccountState),
        Future<void>.sync(audioPlayer.clearAccountState),
        if (clearClient) Future<void>.sync(client.clearAccountState),
      ]);
    });
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
      if (!_backgrounded) {
        return;
      }
      _backgrounded = false;
      final lifecycleGeneration = ++_lifecycleGeneration;
      if (_hasDurableRunWorkflow) {
        unawaited(_resumeDurableRunOnForeground(lifecycleGeneration));
      }
      return;
    }
    if (_backgrounded) {
      return;
    }
    _backgrounded = true;
    _lifecycleGeneration++;
    final hasLocalRecording =
        _state == AgentVoiceComposerState.starting ||
        _state == AgentVoiceComposerState.recording ||
        _recording != null;
    if (hasLocalRecording) {
      unawaited(cancel());
      return;
    }
    if (!_hasDurableRunWorkflow) {
      _draftPlaybackGeneration++;
      _resetDraftPlayback();
      notifyListeners();
      unawaited(audioPlayer.stop());
      return;
    }
    _workflowGeneration++;
    _draftPlaybackGeneration++;
    _cancelRecordingTimers();
    _resetDraftPlayback();
    _state = AgentVoiceComposerState.awaitingAssistant;
    _retry = null;
    _errorMessage = null;
    notifyListeners();
    unawaited(audioPlayer.stop());
  }

  Future<void> _resumeDurableRunOnForeground(int lifecycleGeneration) async {
    final staleOperation = _workflowOperation;
    try {
      await staleOperation;
    } catch (_) {
      // The foreground GET below is the source of truth after the fence.
    }
    final run = _pendingRun;
    if (_disposed ||
        _backgrounded ||
        lifecycleGeneration != _lifecycleGeneration ||
        !_hasDurableRunWorkflow ||
        run == null ||
        _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence();
    _state = AgentVoiceComposerState.awaitingAssistant;
    _retry = null;
    _errorMessage = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        Future<void>.sync(() async {
          try {
            final durable = await client.getRun(runId: run.id);
            _validateRun(
              durable,
              expectedThreadId: fence.threadId,
              expectedRunId: run.id,
              expectedInputMessageId: run.inputMessageId,
              expectedAttempt: run.attempt,
              expectedRetryOfRunId: run.retryOfRunId,
              expectedClientRetryId: run.clientRetryId,
            );
            if (_isWorkflowCurrent(fence)) {
              await _resolveRun(fence, durable);
            }
          } catch (error) {
            if (_isWorkflowCurrent(fence)) {
              _state = AgentVoiceComposerState.failed;
              _retry = _VoiceRetry.run;
              _errorMessage = '暂时无法恢复 Agent 回复。你的语音消息已保留。';
              notifyListeners();
              await _clearOnAuthenticationFailure(error);
            }
          }
        }).whenComplete(() {
          if (identical(_workflowOperation, operation)) {
            _workflowOperation = null;
          }
        });
    _workflowOperation = operation;
    await operation;
  }

  @override
  void dispose() {
    if (_disposed) {
      return;
    }
    final staleRealtime = _realtimeSession;
    final staleOperation = _workflowOperation;
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _accountEpoch++;
    _workflowGeneration++;
    _draftPlaybackGeneration++;
    _lifecycleGeneration++;
    _cancelRecordingTimers();
    unawaited(_completionSubscription?.cancel());
    unawaited(_disposeResources(staleRealtime, staleOperation));
    super.dispose();
  }

  Future<void> _disposeResources(
    _RealtimeDraftSession? realtime,
    Future<void>? operation,
  ) async {
    await _cancelRealtimeBestEffort(realtime);
    await _awaitOperationBestEffort(operation);
    await Future.wait<void>([
      Future<void>.sync(recorder.discardCurrent),
      Future<void>.sync(audioPlayer.dispose),
      Future<void>.sync(client.dispose),
    ]);
  }

  void _startRecordingTimers(_WorkflowFence fence) {
    _cancelRecordingTimers();
    _recordingStartedAt = clock();
    _recordingTicker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!_isWorkflowCurrent(fence) ||
          _state != AgentVoiceComposerState.recording) {
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
      if (_isWorkflowCurrent(fence) &&
          _state == AgentVoiceComposerState.recording) {
        unawaited(stopRecordingAndUpload());
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

  void _handlePlaybackCompletion() {
    if (_disposed) {
      return;
    }
    if (!_draftPlaying) {
      return;
    }
    _resetDraftPlayback();
    notifyListeners();
  }

  void _resetWorkflowPresentation() {
    _state = AgentVoiceComposerState.idle;
    _recording = null;
    _draft = null;
    _pendingRun = null;
    _completedStreamMessageId = null;
    _completedStreamText = null;
    _editedTranscript = '';
    _liveTranscript = '';
    _errorMessage = null;
    _retry = null;
    _uploadIdempotencyKey = null;
    _ambiguousAsrRetry = null;
    _confirmationCommand = null;
    _runRetrySourceRunId = null;
    _runRetryId = null;
    _realtimeSession = null;
    _recordingElapsed = Duration.zero;
  }

  void _resetDraftPlayback() => _draftPlaying = false;

  _WorkflowFence _captureWorkflowFence({String? draftId, int? draftVersion}) {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('An Agent Thread is required for voice work.');
    }
    return _WorkflowFence(
      accountEpoch: _accountEpoch,
      generation: _workflowGeneration,
      threadId: threadId,
      draftId: draftId,
      draftVersion: draftVersion,
    );
  }

  bool _isWorkflowCurrent(
    _WorkflowFence fence, {
    bool allowDraftVersionAdvance = false,
  }) {
    final draft = _draft;
    return !_disposed &&
        fence.accountEpoch == _accountEpoch &&
        fence.generation == _workflowGeneration &&
        fence.threadId == _threadId &&
        (fence.draftId == null || fence.draftId == draft?.id) &&
        (fence.draftVersion == null ||
            fence.draftVersion == draft?.version ||
            (allowDraftVersionAdvance &&
                draft != null &&
                draft.version >= fence.draftVersion!));
  }

  _DraftPlaybackFence _captureDraftPlaybackFence() {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('An Agent Thread is required for draft playback.');
    }
    return _DraftPlaybackFence(
      accountEpoch: _accountEpoch,
      generation: _draftPlaybackGeneration,
      threadId: threadId,
    );
  }

  bool _isDraftPlaybackCurrent(_DraftPlaybackFence fence) {
    return !_disposed &&
        fence.accountEpoch == _accountEpoch &&
        fence.generation == _draftPlaybackGeneration &&
        fence.threadId == _threadId;
  }

  Future<void> _waitForPoll() async {
    if (pollInterval > Duration.zero) {
      await Future<void>.delayed(pollInterval);
    }
  }

  Future<void> _deleteDraftBestEffort(String draftId) async {
    try {
      await client.deleteDraft(draftId: draftId);
    } catch (_) {
      // Drafts expire server-side; local private state is already fenced.
    }
  }

  Future<void> _discardRecordingBestEffort(
    AgentVoiceLocalRecording recording,
  ) async {
    try {
      await recorder.discard(recording);
    } catch (_) {
      // Account cleanup makes a second strict pass over the private directory.
    }
  }

  Future<void> _discardCurrentBestEffort() async {
    try {
      await recorder.discardCurrent();
    } catch (_) {
      // Account cleanup makes a second strict pass over the private directory.
    }
  }

  void _notifyAssistantCommitted(AgentMessage message) {
    final callback = onAssistantCommitted;
    if (callback != null) {
      unawaited(
        Future<void>.sync(() => callback(message)).catchError((_) {
          // Post-commit presentation cannot change the durable Run result.
        }),
      );
    }
  }

  Future<void> _clearOnAuthenticationFailure(Object error) async {
    if (error is! AgentClientException ||
        error.kind != AgentClientFailureKind.authenticationRequired) {
      return;
    }
    _workflowGeneration++;
    _draftPlaybackGeneration++;
    _cancelRecordingTimers();
    _resetWorkflowPresentation();
    _resetDraftPlayback();
    await Future.wait<void>([
      Future<void>.sync(recorder.clearAccountState),
      Future<void>.sync(audioPlayer.clearAccountState),
    ]);
    if (!_disposed) {
      notifyListeners();
    }
  }

  void _validateDraft(
    AgentVoiceDraft draft, {
    required String expectedThreadId,
  }) {
    final hasTranscript = draft.transcript != null;
    final hasFailure = draft.failure != null;
    final confirmationFields = <Object?>[
      draft.confirmedMessageId,
      draft.confirmedRunId,
      draft.messageAudioId,
      draft.confirmedAt,
    ];
    final hasAnyConfirmation = confirmationFields.any((field) => field != null);
    final hasAllConfirmation = confirmationFields.every(
      (field) => field != null,
    );
    if (draft.id.trim().isEmpty ||
        draft.threadId != expectedThreadId ||
        draft.version < 1 ||
        draft.asrAttempt < 1 ||
        draft.version != draft.asrAttempt ||
        draft.recording.contentType != 'audio/wav' ||
        draft.recording.sizeBytes < 1 ||
        draft.recording.sizeBytes > 7400000 ||
        draft.recording.duration <= Duration.zero ||
        draft.recording.duration > const Duration(seconds: 60) ||
        draft.recording.sampleRate < 8000 ||
        draft.recording.sampleRate > 48000 ||
        (draft.expiresAt != null &&
            !draft.expiresAt!.isAfter(draft.createdAt)) ||
        (draft.confirmedAt?.isBefore(draft.createdAt) ?? false) ||
        draft.updatedAt.isBefore(draft.createdAt) ||
        (draft.status == AgentVoiceDraftStatus.confirmed &&
            draft.expiresAt != null) ||
        (draft.status != AgentVoiceDraftStatus.confirmed &&
            draft.expiresAt == null) ||
        (draft.status == AgentVoiceDraftStatus.transcribing &&
            (hasTranscript || hasFailure || hasAnyConfirmation)) ||
        (draft.status == AgentVoiceDraftStatus.ready &&
            (!hasTranscript ||
                hasFailure ||
                draft.version < 1 ||
                hasAnyConfirmation)) ||
        (draft.status == AgentVoiceDraftStatus.failed &&
            (hasTranscript || !hasFailure || hasAnyConfirmation)) ||
        (draft.status == AgentVoiceDraftStatus.confirmed &&
            (!hasTranscript ||
                hasFailure ||
                draft.version < 1 ||
                !hasAllConfirmation))) {
      throw StateError('Invalid Agent voice draft.');
    }
  }

  void _validateConfirmation(
    AgentVoiceConfirmation confirmation, {
    required AgentVoiceDraft expectedDraft,
    required String expectedClientMessageId,
    required String confirmedText,
  }) {
    _validateDraft(
      confirmation.draft,
      expectedThreadId: expectedDraft.threadId,
    );
    final message = confirmation.message;
    final audio = message.audio;
    if (confirmation.draft.id != expectedDraft.id ||
        !_hasConfirmationProjection(confirmation.draft) ||
        confirmation.draft.version != expectedDraft.version ||
        message.id != confirmation.draft.confirmedMessageId ||
        message.role != AgentMessageRole.user ||
        message.modality != AgentMessageModality.voice ||
        message.text != confirmedText ||
        message.clientMessageId != expectedClientMessageId ||
        message.producedByRunId != null ||
        audio == null ||
        audio.id != confirmation.draft.messageAudioId ||
        confirmation.run.id != confirmation.draft.confirmedRunId ||
        confirmation.run.threadId != expectedDraft.threadId ||
        confirmation.run.inputMessageId != message.id ||
        confirmation.run.attempt != 1 ||
        confirmation.run.retryOfRunId != null ||
        confirmation.run.clientRetryId != null) {
      throw StateError('Invalid Agent voice confirmation.');
    }
    _validateRun(
      confirmation.run,
      expectedThreadId: expectedDraft.threadId,
      expectedInputMessageId: message.id,
      expectedAttempt: 1,
      expectedRetryOfRunId: null,
      expectedClientRetryId: null,
    );
    if (confirmation.assistantMessage case final assistant?) {
      if (confirmation.run.status != AgentRunStatus.completed ||
          assistant.id != confirmation.run.assistantMessageId ||
          assistant.role != AgentMessageRole.assistant ||
          assistant.producedByRunId != confirmation.run.id ||
          assistant.text.trim().isEmpty) {
        throw StateError('Invalid Agent voice assistant Message.');
      }
    }
  }

  void _validateRun(
    AgentVoiceRun run, {
    required String expectedThreadId,
    String? expectedRunId,
    String? expectedInputMessageId,
    int? expectedAttempt,
    String? expectedRetryOfRunId,
    String? expectedClientRetryId,
  }) {
    final validateRetryIdentity = expectedAttempt != null;
    if (run.id.trim().isEmpty ||
        run.threadId != expectedThreadId ||
        run.inputMessageId.trim().isEmpty ||
        run.attempt < 1 ||
        run.requestedProvider.trim().isEmpty ||
        run.requestedModel.trim().isEmpty ||
        run.maxOutputTokens < 1 ||
        run.updatedAt.isBefore(run.createdAt) ||
        (run.retryOfRunId == null) != (run.clientRetryId == null) ||
        (run.attempt == 1 && run.retryOfRunId != null) ||
        (run.attempt > 1 && run.retryOfRunId == null) ||
        (run.status == AgentVoiceRunStatus.completed) !=
            (run.assistantMessageId != null && run.completion != null) ||
        (run.status == AgentVoiceRunStatus.failed) != (run.failure != null) ||
        (run.status == AgentRunStatus.pending && run.startedAt != null) ||
        (run.status == AgentRunStatus.running && run.startedAt == null) ||
        (run.isTerminal &&
            (run.startedAt == null || run.completedAt == null)) ||
        (!run.isTerminal && run.completedAt != null) ||
        (expectedRunId != null && run.id != expectedRunId) ||
        (expectedInputMessageId != null &&
            run.inputMessageId != expectedInputMessageId) ||
        (expectedAttempt != null && run.attempt != expectedAttempt) ||
        (validateRetryIdentity && run.retryOfRunId != expectedRetryOfRunId) ||
        (validateRetryIdentity && run.clientRetryId != expectedClientRetryId)) {
      throw StateError('Invalid Agent voice Run.');
    }
  }

  bool get _hasDurableRunWorkflow =>
      _pendingRun != null &&
      _draft != null &&
      _hasConfirmationProjection(_draft!);

  bool _hasConfirmationProjection(AgentVoiceDraft draft) {
    return draft.status == AgentVoiceDraftStatus.confirmed &&
        draft.confirmedMessageId != null &&
        draft.confirmedRunId != null &&
        draft.messageAudioId != null &&
        draft.confirmedAt != null;
  }

  bool _isConfirmed(AgentVoiceDraft draft) {
    return _hasConfirmationProjection(draft);
  }

  String _newId(String scope) {
    final value = idFactory(scope);
    if (value.isEmpty) {
      throw StateError('Agent voice client identity must not be empty.');
    }
    return value;
  }

  bool _isAmbiguousMutationFailure(Object error) {
    if (error is! AgentClientException) {
      return false;
    }
    return error.kind == AgentClientFailureKind.network ||
        error.kind == AgentClientFailureKind.server ||
        error.kind == AgentClientFailureKind.invalidResponse ||
        error.kind == AgentClientFailureKind.unexpected;
  }

  bool _isConfirmationConflict(Object error) {
    return error is _ConfirmationCommandConflict ||
        (error is AgentClientException &&
            (error.kind == AgentClientFailureKind.conflict ||
                error.kind == AgentClientFailureKind.invalidRequest ||
                error.kind == AgentClientFailureKind.notFound));
  }

  String _recordingFailureMessage(Object error) {
    if (error is AgentVoiceRecordingException) {
      return switch (error.kind) {
        AgentVoiceRecordingFailureKind.permissionDenied =>
          '没有麦克风权限。请在系统设置中允许 SpeakUp 使用麦克风。',
        AgentVoiceRecordingFailureKind.emptyAudio => '没有录到声音，请重新录音。',
        AgentVoiceRecordingFailureKind.invalidAudio => '录音格式无效，请重新录音。',
        _ => '录音没有完成，请重试。',
      };
    }
    return '录音没有完成，请重试。';
  }

  String _uploadFailureMessage(Object error) {
    if (error is AgentClientException) {
      return switch (error.kind) {
        AgentClientFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
        AgentClientFailureKind.network => '录音尚未上传，请检查网络后重试。',
        AgentClientFailureKind.rateLimited => '上传过于频繁，请稍后重试。',
        AgentClientFailureKind.invalidRequest => '录音不符合要求，请重新录音。',
        _ => '录音尚未上传，可以重试。',
      };
    }
    return '录音尚未上传，可以重试。';
  }

  String _asrFailureMessage(Object error) {
    if (error is AgentClientException &&
        error.kind == AgentClientFailureKind.authenticationRequired) {
      return '登录状态已失效，请重新登录。';
    }
    if (error is AgentClientException &&
        error.kind == AgentClientFailureKind.network) {
      return '暂时无法检查转写结果，请检查网络后重试。';
    }
    return '语音转写暂时不可用，请稍后重试。';
  }

  String _confirmationFailureMessage(Object error) {
    if (error is AgentClientException) {
      return switch (error.kind) {
        AgentClientFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
        AgentClientFailureKind.network => '确认尚未完成，文字与录音已保留，可以重试。',
        AgentClientFailureKind.conflict => '转写版本已经变化，请重新检查后再确认。',
        AgentClientFailureKind.invalidRequest => '确认文字不符合要求，请编辑后重试。',
        _ => '确认尚未完成，文字与录音已保留，可以重试。',
      };
    }
    return '确认尚未完成，文字与录音已保留，可以重试。';
  }
}

final class _RealtimeDraftResult {
  const _RealtimeDraftResult({this.draft, this.error});

  final AgentVoiceDraft? draft;
  final Object? error;
}

final class _RealtimeDraftSession {
  _RealtimeDraftSession({
    required Stream<AgentVoiceTranscriptionEvent> events,
    required Future<_RealtimeDraftResult> Function(
      StreamIterator<AgentVoiceTranscriptionEvent> events,
    )
    collect,
  }) {
    final iterator = StreamIterator<AgentVoiceTranscriptionEvent>(events);
    _events = iterator;
    result = collect(iterator);
  }

  late final StreamIterator<AgentVoiceTranscriptionEvent> _events;
  late final Future<_RealtimeDraftResult> result;
  Future<void>? _cancellation;

  Future<void> cancel() {
    final existing = _cancellation;
    if (existing != null) {
      return existing;
    }
    final cancellation = Future<void>.sync(_events.cancel);
    _cancellation = cancellation;
    return cancellation;
  }
}

final class _AsrRetryCommand {
  const _AsrRetryCommand({required this.draftId, required this.draftVersion});

  final String draftId;
  final int draftVersion;
}

final class _ConfirmationCommand {
  const _ConfirmationCommand({
    required this.draftId,
    required this.draftVersion,
    required this.clientMessageId,
    required this.confirmedText,
  });

  final String draftId;
  final int draftVersion;
  final String clientMessageId;
  final String confirmedText;
}

final class _ConfirmationReconciliationPending implements Exception {
  const _ConfirmationReconciliationPending();
}

final class _AssistantMessageHydrationPending implements Exception {
  const _AssistantMessageHydrationPending();
}

final class _ConfirmationCommandConflict implements Exception {
  const _ConfirmationCommandConflict();
}

enum _VoiceRetry { start, upload, asr, restoreDraft, confirm, run, message }

final class _WorkflowFence {
  const _WorkflowFence({
    required this.accountEpoch,
    required this.generation,
    required this.threadId,
    this.draftId,
    this.draftVersion,
  });

  final int accountEpoch;
  final int generation;
  final String threadId;
  final String? draftId;
  final int? draftVersion;

  _WorkflowFence withoutDraft() {
    return _WorkflowFence(
      accountEpoch: accountEpoch,
      generation: generation,
      threadId: threadId,
    );
  }

  _WorkflowFence withoutDraftVersion() {
    return _WorkflowFence(
      accountEpoch: accountEpoch,
      generation: generation,
      threadId: threadId,
      draftId: draftId,
    );
  }
}

final class _DraftPlaybackFence {
  const _DraftPlaybackFence({
    required this.accountEpoch,
    required this.generation,
    required this.threadId,
  });

  final int accountEpoch;
  final int generation;
  final String threadId;
}
