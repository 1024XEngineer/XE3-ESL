import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

import 'agent_client.dart';
import 'agent_models.dart';
import 'agent_voice_client.dart';
import 'agent_voice_models.dart';
import 'agent_voice_recording.dart';

typedef AgentVoiceIdFactory = String Function(String scope);
typedef AgentVoiceControllerClock = DateTime Function();
typedef AgentVoiceMessagesCommitted =
    void Function(Iterable<AgentMessage> messages);
typedef AgentVoiceMessageAudioDeleted =
    void Function(String messageId, AgentMessageAudio deletedAudio);

final class AgentVoiceController extends ChangeNotifier
    with WidgetsBindingObserver {
  AgentVoiceController({
    required this.client,
    required this.recorder,
    required this.audioPlayer,
    required this.onMessagesCommitted,
    required this.onMessageAudioDeleted,
    required this.idFactory,
    this.clock = DateTime.now,
    this.pollInterval = const Duration(seconds: 1),
    this.maximumCandidatePolls = 75,
    this.maximumRunPolls = 75,
    this.recordingLimit = const Duration(seconds: 58),
  }) {
    if (pollInterval.isNegative ||
        maximumCandidatePolls < 1 ||
        maximumRunPolls < 1 ||
        recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 60)) {
      throw ArgumentError('Agent voice controller configuration is invalid.');
    }
    _positionSubscription = audioPlayer.onPosition.listen(_handlePosition);
    _completionSubscription = audioPlayer.onComplete.listen((_) {
      _handlePlaybackCompletion();
    });
    WidgetsBinding.instance.addObserver(this);
  }

  final AgentVoiceClient client;
  final AgentVoiceRecorder recorder;
  final AgentVoiceAudioPlayer audioPlayer;
  final AgentVoiceMessagesCommitted onMessagesCommitted;
  final AgentVoiceMessageAudioDeleted onMessageAudioDeleted;
  final AgentVoiceIdFactory idFactory;
  final AgentVoiceControllerClock clock;
  final Duration pollInterval;
  final int maximumCandidatePolls;
  final int maximumRunPolls;
  final Duration recordingLimit;

  String? _threadId;
  Set<String> _visibleMessageIds = <String>{};
  Map<String, String?> _visibleMessageAudioIds = <String, String?>{};
  AgentVoiceComposerState _state = AgentVoiceComposerState.idle;
  AgentVoiceLocalRecording? _recording;
  AgentVoiceCandidate? _candidate;
  AgentVoiceRun? _pendingRun;
  String _editedTranscript = '';
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
  int _mediaGeneration = 0;
  bool _disposed = false;
  bool _backgrounded = false;
  int _lifecycleGeneration = 0;
  Future<void>? _workflowOperation;
  Future<void>? _mediaOperation;
  Future<void>? _cleanupFuture;

  String? _loadingMessageId;
  String? _playingMessageId;
  String? _activeMessageAudioId;
  String? _deletingMessageId;
  String? _deletingAudioId;
  String? _mediaErrorMessage;
  String? _mediaErrorMessageId;
  Duration _playbackPosition = Duration.zero;
  double _speechSpeed = 1;
  bool _draftPlaying = false;
  StreamSubscription<Duration>? _positionSubscription;
  StreamSubscription<void>? _completionSubscription;

  String? get threadId => _threadId;
  AgentVoiceComposerState get state => _state;
  AgentVoiceLocalRecording? get recording => _recording;
  AgentVoiceCandidate? get candidate => _candidate;
  String get editedTranscript => _editedTranscript;
  String? get errorMessage => _errorMessage;
  Duration get recordingElapsed => _recordingElapsed;
  bool get hasActiveWorkflow => _state != AgentVoiceComposerState.idle;
  bool get canRetry => _retry != null;
  bool get canStartRecording =>
      !_disposed &&
      !_backgrounded &&
      _threadId != null &&
      _state == AgentVoiceComposerState.idle;
  bool get canStopRecording => _state == AgentVoiceComposerState.recording;
  bool get canUpload =>
      _recording != null &&
      (_state == AgentVoiceComposerState.recorded ||
          (_state == AgentVoiceComposerState.failed &&
              _retry == _VoiceRetry.upload));
  bool get canConfirm =>
      _candidate?.isReady == true &&
      _editedTranscript.trim().isNotEmpty &&
      utf8.encode(_editedTranscript.trim()).length <= 16384 &&
      _state == AgentVoiceComposerState.awaitingConfirmation;
  bool get isDraftPlaying => _draftPlaying;
  double get speechSpeed => _speechSpeed;
  String? get loadingMessageId => _loadingMessageId;
  String? get playingMessageId => _playingMessageId;
  String? get deletingMessageId => _deletingMessageId;
  String? get mediaErrorMessage => _mediaErrorMessage;
  String? get mediaErrorMessageId => _mediaErrorMessageId;
  Duration get playbackPosition => _playbackPosition;

  Future<void> bindThread(
    String? threadId, {
    Iterable<AgentMessage> messages = const <AgentMessage>[],
  }) async {
    final messageIds = {for (final message in messages) message.id};
    final messageAudioIds = {
      for (final message in messages)
        message.id: message.audio?.isReadable == true
            ? message.audio!.id
            : null,
    };
    if (threadId == _threadId) {
      _visibleMessageIds = messageIds;
      _visibleMessageAudioIds = messageAudioIds;
      _fenceUnavailablePlayback();
      return;
    }
    final staleRecording = _recording;
    final staleCandidate = _candidate;
    _workflowGeneration++;
    _mediaGeneration++;
    _cancelRecordingTimers();
    _threadId = threadId;
    _visibleMessageIds = messageIds;
    _visibleMessageAudioIds = messageAudioIds;
    _resetWorkflowPresentation();
    _resetMediaPresentation();
    if (!_disposed) {
      notifyListeners();
    }
    await Future.wait<void>([
      _discardCurrentBestEffort(),
      if (staleRecording != null) _discardRecordingBestEffort(staleRecording),
      Future<void>.sync(audioPlayer.stop),
      if (staleCandidate != null && !_isConfirmed(staleCandidate))
        _deleteCandidateBestEffort(staleCandidate.id),
    ]);
  }

  void syncMessages(Iterable<AgentMessage> messages) {
    _visibleMessageIds = {for (final message in messages) message.id};
    _visibleMessageAudioIds = {
      for (final message in messages)
        message.id: message.audio?.isReadable == true
            ? message.audio!.id
            : null,
    };
    _fenceUnavailablePlayback();
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
      await recorder.start();
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
    try {
      final recording = await recorder.stop();
      if (!_isWorkflowCurrent(fence)) {
        await recorder.discard(recording);
        return;
      }
      _recording = recording;
      _recordingElapsed = recording.duration;
      _uploadIdempotencyKey = _newId('voice-upload');
      _state = AgentVoiceComposerState.recorded;
      _errorMessage = null;
      _retry = null;
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        _state = AgentVoiceComposerState.failed;
        _errorMessage = _recordingFailureMessage(error);
        _retry = _VoiceRetry.start;
      }
    }
    if (_isWorkflowCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<void> cancel() async {
    final staleRecording = _recording;
    final staleCandidate = _candidate;
    _workflowGeneration++;
    _mediaGeneration++;
    _cancelRecordingTimers();
    _resetWorkflowPresentation();
    _resetMediaPresentation();
    if (!_disposed) {
      notifyListeners();
    }
    await Future.wait<void>([
      _discardCurrentBestEffort(),
      if (staleRecording != null) _discardRecordingBestEffort(staleRecording),
      Future<void>.sync(audioPlayer.stop),
      if (staleCandidate != null && !_isConfirmed(staleCandidate))
        _deleteCandidateBestEffort(staleCandidate.id),
    ]);
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
      _mediaGeneration++;
      _resetPlaybackPresentation();
      await audioPlayer.stop();
      if (!_disposed) {
        notifyListeners();
      }
      return;
    }
    final fence = _captureMediaFence(messageId: 'draft');
    _resetPlaybackPresentation();
    _draftPlaying = true;
    notifyListeners();
    try {
      await audioPlayer.playFile(recording.path, speed: 1);
      if (!_isMediaCurrent(fence)) {
        await audioPlayer.stop();
      }
    } catch (_) {
      if (_isMediaCurrent(fence)) {
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
      final candidate = await client.createCandidate(
        threadId: fence.threadId,
        recording: recording,
        idempotencyKey: idempotencyKey,
      );
      if (!_isWorkflowCurrent(fence)) {
        await _deleteCandidateBestEffort(candidate.id);
        await _discardRecordingBestEffort(recording);
        return;
      }
      _validateCandidate(candidate, expectedThreadId: fence.threadId);
      _candidate = candidate;
      _recording = null;
      await _discardRecordingBestEffort(recording);
      if (!_isWorkflowCurrent(fence)) {
        await _deleteCandidateBestEffort(candidate.id);
        return;
      }
      await _resolveCandidate(fence, candidate);
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        _state = AgentVoiceComposerState.failed;
        final uploadCommitted = _candidate != null;
        _errorMessage = uploadCommitted
            ? _asrFailureMessage(error)
            : _uploadFailureMessage(error);
        _retry = uploadCommitted
            ? _VoiceRetry.restoreCandidate
            : _VoiceRetry.upload;
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    }
  }

  Future<void> _resolveCandidate(
    _WorkflowFence fence,
    AgentVoiceCandidate initial,
  ) async {
    var candidate = initial;
    for (var attempt = 0; attempt < maximumCandidatePolls; attempt++) {
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      _validateCandidate(candidate, expectedThreadId: fence.threadId);
      _candidate = candidate;
      if (candidate.isReady) {
        _editedTranscript = candidate.transcript!.text;
        _state = AgentVoiceComposerState.awaitingConfirmation;
        _retry = null;
        _errorMessage = null;
        notifyListeners();
        return;
      }
      if (candidate.status == AgentVoiceCandidateStatus.failed) {
        _state = AgentVoiceComposerState.failed;
        _retry = candidate.failure?.retryable == true ? _VoiceRetry.asr : null;
        _errorMessage = candidate.failure?.retryable == true
            ? '语音转写没有完成，可以重试识别。'
            : '语音转写失败，请取消后重新录音。';
        notifyListeners();
        return;
      }
      if (!candidate.isAsrPending) {
        throw StateError('Unexpected Agent voice candidate state.');
      }
      _state = AgentVoiceComposerState.transcribing;
      notifyListeners();
      await _waitForPoll();
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      final restored = await client.getCandidate(candidateId: candidate.id);
      if (restored.version < candidate.version) {
        throw StateError('Agent voice candidate version moved backwards.');
      }
      candidate = restored;
    }
    if (_isWorkflowCurrent(fence)) {
      _state = AgentVoiceComposerState.failed;
      _retry = _VoiceRetry.restoreCandidate;
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
      case _VoiceRetry.restoreCandidate:
        await restoreCandidate();
      case _VoiceRetry.confirm:
        await _retryConfirmation();
      case _VoiceRetry.run:
        await retryAssistant();
    }
  }

  Future<void> retryAsr() async {
    final candidate = _candidate;
    if (candidate == null ||
        candidate.failure?.retryable != true ||
        _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence(
      candidateId: candidate.id,
      candidateVersion: candidate.version,
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
                (ambiguous.candidateId != candidate.id ||
                    ambiguous.candidateVersion != candidate.version)) {
              _ambiguousAsrRetry = null;
            }
            if (ambiguous != null &&
                ambiguous.candidateId == candidate.id &&
                ambiguous.candidateVersion == candidate.version) {
              final durable = await client.getCandidate(
                candidateId: candidate.id,
              );
              _validateCandidate(durable, expectedThreadId: fence.threadId);
              if (durable.id != candidate.id ||
                  durable.version < candidate.version) {
                throw StateError(
                  'Agent voice candidate reconciliation moved backwards.',
                );
              }
              if (!_isWorkflowCurrent(
                fence,
                allowCandidateVersionAdvance: true,
              )) {
                return;
              }
              if (durable.version != candidate.version ||
                  durable.status != AgentVoiceCandidateStatus.failed ||
                  durable.failure?.retryable != true) {
                _ambiguousAsrRetry = null;
                await _resolveCandidate(
                  fence.withoutCandidateVersion(),
                  durable,
                );
                return;
              }
            }
            _ambiguousAsrRetry = null;
            retryPosted = true;
            final retried = await client.retryCandidate(
              candidateId: candidate.id,
            );
            _ambiguousAsrRetry = null;
            if (!_isWorkflowCurrent(
              fence,
              allowCandidateVersionAdvance: true,
            )) {
              return;
            }
            if (retried.version <= candidate.version) {
              throw StateError('ASR retry did not advance candidate version.');
            }
            await _resolveCandidate(fence.withoutCandidateVersion(), retried);
          } catch (error) {
            if (_isWorkflowCurrent(fence, allowCandidateVersionAdvance: true)) {
              if (retryPosted && _isAmbiguousMutationFailure(error)) {
                _ambiguousAsrRetry = _AsrRetryCommand(
                  candidateId: candidate.id,
                  candidateVersion: candidate.version,
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

  Future<void> restoreCandidate() async {
    final candidate = _candidate;
    if (candidate == null || _workflowOperation != null) {
      return;
    }
    final fence = _captureWorkflowFence(candidateId: candidate.id);
    _state = AgentVoiceComposerState.transcribing;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        Future<void>.sync(() async {
          try {
            final restored = await client.getCandidate(
              candidateId: candidate.id,
            );
            if (_isWorkflowCurrent(fence, allowCandidateVersionAdvance: true)) {
              await _resolveCandidate(
                fence.withoutCandidateVersion(),
                restored,
              );
            }
          } catch (error) {
            if (_isWorkflowCurrent(fence, allowCandidateVersionAdvance: true)) {
              _state = AgentVoiceComposerState.failed;
              _retry = _VoiceRetry.restoreCandidate;
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
    final candidate = _candidate;
    final text = _editedTranscript.trim();
    if (candidate == null ||
        !candidate.isReady ||
        !canConfirm ||
        _workflowOperation != null ||
        text.isEmpty) {
      return Future<void>.value();
    }
    final command = _ConfirmationCommand(
      candidateId: candidate.id,
      candidateVersion: candidate.version,
      clientMessageId: _newId('voice-message'),
      confirmedText: text,
    );
    _confirmationCommand = command;
    return _startConfirmation(
      candidate: candidate,
      command: command,
      reconcileFirst: false,
    );
  }

  Future<void> _retryConfirmation() {
    final candidate = _candidate;
    final command = _confirmationCommand;
    if (candidate == null ||
        command == null ||
        candidate.id != command.candidateId ||
        candidate.version != command.candidateVersion ||
        _workflowOperation != null) {
      return Future<void>.value();
    }
    return _startConfirmation(
      candidate: candidate,
      command: command,
      reconcileFirst: true,
    );
  }

  Future<void> _startConfirmation({
    required AgentVoiceCandidate candidate,
    required _ConfirmationCommand command,
    required bool reconcileFirst,
  }) {
    final fence = _captureWorkflowFence(
      candidateId: candidate.id,
      candidateVersion: candidate.version,
    );
    _state = AgentVoiceComposerState.confirming;
    _errorMessage = null;
    _retry = null;
    notifyListeners();
    late final Future<void> operation;
    operation =
        _confirm(
          fence: fence,
          candidate: candidate,
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
    required AgentVoiceCandidate candidate,
    required _ConfirmationCommand command,
    required bool reconcileFirst,
  }) async {
    var messageCommitted = false;
    try {
      final confirmation = reconcileFirst
          ? await _reconcileConfirmation(fence, command)
          : await client.confirmCandidate(
              candidateId: command.candidateId,
              candidateVersion: command.candidateVersion,
              clientMessageId: command.clientMessageId,
              confirmedText: command.confirmedText,
            );
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      _validateConfirmation(
        confirmation,
        expectedCandidate: candidate,
        confirmedText: command.confirmedText,
      );
      _candidate = confirmation.candidate;
      _pendingRun = confirmation.run;
      onMessagesCommitted(<AgentMessage>[confirmation.message]);
      _visibleMessageIds.add(confirmation.message.id);
      _visibleMessageAudioIds[confirmation.message.id] =
          confirmation.message.audio?.isReadable == true
          ? confirmation.message.audio!.id
          : null;
      messageCommitted = true;
      if (confirmation.assistantMessage case final assistant?) {
        onMessagesCommitted(<AgentMessage>[assistant]);
        _visibleMessageIds.add(assistant.id);
        _visibleMessageAudioIds[assistant.id] = assistant.audio?.id;
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
        return;
      }
      _state = AgentVoiceComposerState.awaitingAssistant;
      notifyListeners();
      if (_backgrounded) {
        return;
      }
      await _resolveRun(fence.withoutCandidate(), confirmation.run);
    } catch (error) {
      if (_isWorkflowCurrent(fence)) {
        if (messageCommitted) {
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
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    }
  }

  Future<AgentVoiceConfirmation> _reconcileConfirmation(
    _WorkflowFence fence,
    _ConfirmationCommand command,
  ) async {
    final durable = await client.getCandidate(candidateId: command.candidateId);
    _validateCandidate(durable, expectedThreadId: fence.threadId);
    if (durable.id != command.candidateId ||
        durable.version != command.candidateVersion) {
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
      return AgentVoiceConfirmation(
        candidate: durable,
        message: message,
        run: run,
      );
    }
    if (durable.status == AgentVoiceCandidateStatus.candidateReady) {
      return client.confirmCandidate(
        candidateId: command.candidateId,
        candidateVersion: command.candidateVersion,
        clientMessageId: command.clientMessageId,
        confirmedText: command.confirmedText,
      );
    }
    if (durable.status == AgentVoiceCandidateStatus.confirming) {
      throw const _ConfirmationReconciliationPending();
    }
    throw const _ConfirmationCommandConflict();
  }

  Future<void> _resolveRun(_WorkflowFence fence, AgentVoiceRun initial) async {
    var run = initial;
    for (var attempt = 0; attempt < maximumRunPolls; attempt++) {
      if (!_isWorkflowCurrent(fence)) {
        return;
      }
      _validateRun(run, expectedThreadId: fence.threadId);
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
        final messageId = run.assistantMessageId!;
        final assistant = await client.getMessage(
          threadId: fence.threadId,
          messageId: messageId,
        );
        if (!_isWorkflowCurrent(fence)) {
          return;
        }
        if (assistant == null) {
          await _waitForPoll();
          run = await client.getRun(runId: run.id);
          continue;
        }
        if (assistant.id != messageId ||
            assistant.role != AgentMessageRole.assistant ||
            assistant.text.trim().isEmpty) {
          throw StateError('Invalid assistant Message after voice Run.');
        }
        onMessagesCommitted(<AgentMessage>[assistant]);
        _visibleMessageIds.add(assistant.id);
        _resetWorkflowPresentation();
        notifyListeners();
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
            } else {
              next = await client.getRun(runId: run.id);
            }
            if (_isWorkflowCurrent(fence)) {
              await _resolveRun(fence, next);
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

  Future<void> toggleMessagePlayback(AgentMessage message) {
    if (!_visibleMessageIds.contains(message.id) ||
        _threadId == null ||
        _disposed ||
        _deletingMessageId == message.id) {
      return Future<void>.value();
    }
    if (_playingMessageId == message.id || _loadingMessageId == message.id) {
      _mediaGeneration++;
      _resetPlaybackPresentation();
      notifyListeners();
      return audioPlayer.stop();
    }
    final audio = message.audio;
    if (message.role == AgentMessageRole.user &&
        (message.modality != AgentMessageModality.voice ||
            audio == null ||
            !audio.isReadable)) {
      return Future<void>.value();
    }
    final generation = ++_mediaGeneration;
    final fence = _MediaFence(
      accountEpoch: _accountEpoch,
      generation: generation,
      threadId: _threadId!,
      messageId: message.id,
      audioId: audio?.id,
    );
    _resetPlaybackPresentation();
    _loadingMessageId = message.id;
    _activeMessageAudioId = audio?.id;
    notifyListeners();
    late final Future<void> operation;
    operation = _playMessage(fence, message).whenComplete(() {
      if (identical(_mediaOperation, operation)) {
        _mediaOperation = null;
      }
    });
    _mediaOperation = operation;
    return operation;
  }

  Future<void> _playMessage(_MediaFence fence, AgentMessage message) async {
    Uint8List? bytes;
    try {
      await audioPlayer.stop();
      bytes = message.role == AgentMessageRole.user
          ? await client.loadMessageAudio(audioId: message.audio!.id)
          : await client.loadAssistantSpeech(messageId: message.id);
      if (!_isMediaCurrent(fence)) {
        return;
      }
      _loadingMessageId = null;
      _playingMessageId = message.id;
      _playbackPosition = Duration.zero;
      notifyListeners();
      await audioPlayer.playWav(
        bytes,
        speed: message.role == AgentMessageRole.assistant ? _speechSpeed : 1,
      );
      if (!_isMediaCurrent(fence)) {
        await audioPlayer.stop();
      }
    } catch (error) {
      if (_isMediaCurrent(fence)) {
        _loadingMessageId = null;
        _playingMessageId = null;
        _mediaErrorMessageId = message.id;
        _mediaErrorMessage = _playbackFailureMessage(error, message.role);
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    } finally {
      bytes?.fillRange(0, bytes.length, 0);
    }
  }

  void cycleSpeechSpeed() {
    const speeds = <double>[0.75, 1, 1.25, 1.5];
    final index = speeds.indexOf(_speechSpeed);
    _speechSpeed = speeds[(index + 1) % speeds.length];
    if (_playingMessageId != null) {
      _mediaGeneration++;
      _resetPlaybackPresentation();
      unawaited(audioPlayer.stop());
    }
    notifyListeners();
  }

  Future<void> deleteMessageAudio(AgentMessage message) async {
    final audio = message.audio;
    if (message.role != AgentMessageRole.user ||
        message.modality != AgentMessageModality.voice ||
        audio == null ||
        !audio.isReadable ||
        !_visibleMessageIds.contains(message.id) ||
        _deletingMessageId != null ||
        _threadId == null) {
      return;
    }
    final fence = _captureMediaFence(messageId: message.id, audioId: audio.id);
    _deletingMessageId = message.id;
    _deletingAudioId = audio.id;
    _mediaErrorMessage = null;
    _mediaErrorMessageId = null;
    final stopsCurrentPlayback =
        _playingMessageId == message.id || _loadingMessageId == message.id;
    if (stopsCurrentPlayback) {
      _mediaGeneration++;
      _resetPlaybackPresentation();
    }
    notifyListeners();
    try {
      if (stopsCurrentPlayback) {
        await audioPlayer.stop();
        if (!_isMediaCurrent(fence, allowGenerationAdvance: true)) {
          return;
        }
      }
      await client.deleteMessageAudio(audioId: audio.id);
      if (!_isMediaCurrent(fence, allowGenerationAdvance: true)) {
        return;
      }
      final deletedAudio = audio.copyWith(
        status: AgentMessageAudioStatus.deleted,
        clearPlaybackPath: true,
        deletedAt: DateTime.now().toUtc(),
      );
      onMessageAudioDeleted(message.id, deletedAudio);
      _deletingMessageId = null;
      _deletingAudioId = null;
      notifyListeners();
    } catch (error) {
      if (_isMediaCurrent(fence, allowGenerationAdvance: true)) {
        _deletingMessageId = null;
        _deletingAudioId = null;
        _mediaErrorMessageId = message.id;
        _mediaErrorMessage = _deleteFailureMessage(error);
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    }
  }

  double playbackProgressFor(AgentMessage message) {
    if (_playingMessageId != message.id) {
      return 0;
    }
    final duration = message.audio?.duration;
    if (duration == null || duration <= Duration.zero) {
      return 0;
    }
    return (_playbackPosition.inMilliseconds / duration.inMilliseconds).clamp(
      0,
      1,
    );
  }

  Future<void> clearPrivateState({bool clearClient = true}) async {
    final existing = _cleanupFuture;
    if (existing != null) {
      await existing;
      return;
    }
    _accountEpoch++;
    _workflowGeneration++;
    _mediaGeneration++;
    _lifecycleGeneration++;
    _backgrounded = false;
    _cancelRecordingTimers();
    _threadId = null;
    _visibleMessageIds = <String>{};
    _visibleMessageAudioIds = <String, String?>{};
    _resetWorkflowPresentation();
    _resetMediaPresentation();
    if (!_disposed) {
      notifyListeners();
    }
    final cleanup = Future.wait<void>([
      Future<void>.sync(recorder.clearAccountState),
      Future<void>.sync(audioPlayer.clearAccountState),
      if (clearClient) Future<void>.sync(client.clearAccountState),
    ]);
    _cleanupFuture = cleanup;
    try {
      await cleanup;
      await _workflowOperation;
      await _mediaOperation;
      await recorder.clearAccountState();
      await audioPlayer.clearAccountState();
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
      _mediaGeneration++;
      _resetPlaybackPresentation();
      notifyListeners();
      unawaited(audioPlayer.stop());
      return;
    }
    _workflowGeneration++;
    _mediaGeneration++;
    _cancelRecordingTimers();
    _resetPlaybackPresentation();
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
            if (durable.id != run.id) {
              throw StateError('Foreground Agent Run identity changed.');
            }
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
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _accountEpoch++;
    _workflowGeneration++;
    _mediaGeneration++;
    _lifecycleGeneration++;
    _cancelRecordingTimers();
    unawaited(_positionSubscription?.cancel());
    unawaited(_completionSubscription?.cancel());
    unawaited(recorder.discardCurrent());
    unawaited(audioPlayer.dispose());
    unawaited(client.dispose());
    super.dispose();
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

  void _handlePosition(Duration position) {
    if (_disposed || _playingMessageId == null) {
      return;
    }
    _playbackPosition = position;
    notifyListeners();
  }

  void _handlePlaybackCompletion() {
    if (_disposed) {
      return;
    }
    _resetPlaybackPresentation(clearError: false);
    notifyListeners();
  }

  void _fenceUnavailablePlayback() {
    var changed = false;
    var stopPlayback = false;
    final playbackId = _playingMessageId ?? _loadingMessageId;
    if (playbackId != null &&
        (!_visibleMessageIds.contains(playbackId) ||
            (_activeMessageAudioId != null &&
                _visibleMessageAudioIds[playbackId] !=
                    _activeMessageAudioId))) {
      _mediaGeneration++;
      _resetPlaybackPresentation();
      stopPlayback = true;
      changed = true;
    }
    final deletingId = _deletingMessageId;
    if (deletingId != null &&
        (!_visibleMessageIds.contains(deletingId) ||
            _visibleMessageAudioIds[deletingId] != _deletingAudioId)) {
      _deletingMessageId = null;
      _deletingAudioId = null;
      changed = true;
    }
    if (!changed) {
      return;
    }
    if (stopPlayback) {
      unawaited(audioPlayer.stop());
    }
    notifyListeners();
  }

  void _resetWorkflowPresentation() {
    _state = AgentVoiceComposerState.idle;
    _recording = null;
    _candidate = null;
    _pendingRun = null;
    _editedTranscript = '';
    _errorMessage = null;
    _retry = null;
    _uploadIdempotencyKey = null;
    _ambiguousAsrRetry = null;
    _confirmationCommand = null;
    _runRetrySourceRunId = null;
    _runRetryId = null;
    _recordingElapsed = Duration.zero;
  }

  void _resetPlaybackPresentation({bool clearError = true}) {
    _draftPlaying = false;
    _loadingMessageId = null;
    _playingMessageId = null;
    _activeMessageAudioId = null;
    if (clearError) {
      _mediaErrorMessage = null;
      _mediaErrorMessageId = null;
    }
    _playbackPosition = Duration.zero;
  }

  void _resetMediaPresentation() {
    _resetPlaybackPresentation();
    _deletingMessageId = null;
    _deletingAudioId = null;
  }

  _WorkflowFence _captureWorkflowFence({
    String? candidateId,
    int? candidateVersion,
  }) {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('An Agent Thread is required for voice work.');
    }
    return _WorkflowFence(
      accountEpoch: _accountEpoch,
      generation: _workflowGeneration,
      threadId: threadId,
      candidateId: candidateId,
      candidateVersion: candidateVersion,
    );
  }

  bool _isWorkflowCurrent(
    _WorkflowFence fence, {
    bool allowCandidateVersionAdvance = false,
  }) {
    final candidate = _candidate;
    return !_disposed &&
        fence.accountEpoch == _accountEpoch &&
        fence.generation == _workflowGeneration &&
        fence.threadId == _threadId &&
        (fence.candidateId == null || fence.candidateId == candidate?.id) &&
        (fence.candidateVersion == null ||
            fence.candidateVersion == candidate?.version ||
            (allowCandidateVersionAdvance &&
                candidate != null &&
                candidate.version >= fence.candidateVersion!));
  }

  _MediaFence _captureMediaFence({required String messageId, String? audioId}) {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('An Agent Thread is required for voice playback.');
    }
    return _MediaFence(
      accountEpoch: _accountEpoch,
      generation: _mediaGeneration,
      threadId: threadId,
      messageId: messageId,
      audioId: audioId,
    );
  }

  bool _isMediaCurrent(
    _MediaFence fence, {
    bool allowGenerationAdvance = false,
  }) {
    return !_disposed &&
        fence.accountEpoch == _accountEpoch &&
        (allowGenerationAdvance
            ? fence.generation <= _mediaGeneration
            : fence.generation == _mediaGeneration) &&
        fence.threadId == _threadId &&
        (fence.messageId == 'draft' ||
            (_visibleMessageIds.contains(fence.messageId) &&
                (fence.audioId == null ||
                    _visibleMessageAudioIds[fence.messageId] ==
                        fence.audioId)));
  }

  Future<void> _waitForPoll() async {
    if (pollInterval > Duration.zero) {
      await Future<void>.delayed(pollInterval);
    }
  }

  Future<void> _deleteCandidateBestEffort(String candidateId) async {
    try {
      await client.deleteCandidate(candidateId: candidateId);
    } catch (_) {
      // Candidates expire server-side; local private state is already fenced.
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

  Future<void> _clearOnAuthenticationFailure(Object error) async {
    if (error is! AgentClientException ||
        error.kind != AgentClientFailureKind.authenticationRequired) {
      return;
    }
    _workflowGeneration++;
    _mediaGeneration++;
    _cancelRecordingTimers();
    _resetWorkflowPresentation();
    _resetMediaPresentation();
    await Future.wait<void>([
      Future<void>.sync(recorder.clearAccountState),
      Future<void>.sync(audioPlayer.clearAccountState),
    ]);
    if (!_disposed) {
      notifyListeners();
    }
  }

  void _validateCandidate(
    AgentVoiceCandidate candidate, {
    required String expectedThreadId,
  }) {
    final hasTranscript = candidate.transcript != null;
    final hasFailure = candidate.failure != null;
    if (candidate.id.trim().isEmpty ||
        candidate.threadId != expectedThreadId ||
        candidate.version < 0 ||
        candidate.asrAttempt < 0 ||
        candidate.recording.contentType != 'audio/wav' ||
        candidate.recording.sizeBytes < 1 ||
        candidate.recording.sizeBytes > 7400000 ||
        candidate.recording.duration <= Duration.zero ||
        candidate.recording.duration > const Duration(seconds: 60) ||
        candidate.recording.sampleRate < 8000 ||
        candidate.recording.sampleRate > 48000 ||
        candidate.expiresAt.isBefore(candidate.createdAt) ||
        candidate.updatedAt.isBefore(candidate.createdAt) ||
        (candidate.status == AgentVoiceCandidateStatus.candidateReady &&
            (!hasTranscript || hasFailure || candidate.version < 1)) ||
        (candidate.status == AgentVoiceCandidateStatus.failed &&
            (hasTranscript || !hasFailure))) {
      throw StateError('Invalid Agent voice candidate.');
    }
  }

  void _validateConfirmation(
    AgentVoiceConfirmation confirmation, {
    required AgentVoiceCandidate expectedCandidate,
    required String confirmedText,
  }) {
    _validateCandidate(
      confirmation.candidate,
      expectedThreadId: expectedCandidate.threadId,
    );
    final message = confirmation.message;
    final audio = message.audio;
    if (confirmation.candidate.id != expectedCandidate.id ||
        !_hasConfirmationProjection(confirmation.candidate) ||
        confirmation.candidate.version != expectedCandidate.version ||
        message.id != confirmation.candidate.confirmedMessageId ||
        message.role != AgentMessageRole.user ||
        message.modality != AgentMessageModality.voice ||
        message.text != confirmedText ||
        audio == null ||
        audio.id != confirmation.candidate.messageAudioId ||
        confirmation.run.id != confirmation.candidate.confirmedRunId ||
        confirmation.run.threadId != expectedCandidate.threadId ||
        confirmation.run.inputMessageId != message.id) {
      throw StateError('Invalid Agent voice confirmation.');
    }
    _validateRun(
      confirmation.run,
      expectedThreadId: expectedCandidate.threadId,
    );
  }

  void _validateRun(AgentVoiceRun run, {required String expectedThreadId}) {
    if (run.id.trim().isEmpty ||
        run.threadId != expectedThreadId ||
        run.inputMessageId.trim().isEmpty ||
        (run.status == AgentVoiceRunStatus.completed) !=
            (run.assistantMessageId != null) ||
        (run.status == AgentVoiceRunStatus.failed) !=
            (run.failureKind != null)) {
      throw StateError('Invalid Agent voice Run.');
    }
  }

  bool get _hasDurableRunWorkflow =>
      _pendingRun != null &&
      _candidate != null &&
      _hasConfirmationProjection(_candidate!);

  bool _hasConfirmationProjection(AgentVoiceCandidate candidate) {
    final statusAllowsProjection =
        candidate.status == AgentVoiceCandidateStatus.confirmed ||
        candidate.status == AgentVoiceCandidateStatus.deleting ||
        candidate.status == AgentVoiceCandidateStatus.deleted;
    return statusAllowsProjection &&
        candidate.confirmedMessageId != null &&
        candidate.confirmedRunId != null &&
        candidate.messageAudioId != null &&
        candidate.confirmedAt != null;
  }

  bool _isConfirmed(AgentVoiceCandidate candidate) {
    return _hasConfirmationProjection(candidate);
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

  String _playbackFailureMessage(Object error, AgentMessageRole role) {
    final prefix = role == AgentMessageRole.user ? '录音播放' : '朗读';
    if (error is AgentClientException) {
      return switch (error.kind) {
        AgentClientFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
        AgentClientFailureKind.notFound =>
          role == AgentMessageRole.user ? '录音不存在或已删除，确认文字仍会保留。' : '这条回复暂时无法朗读。',
        AgentClientFailureKind.network => '$prefix失败，请检查网络后重试。',
        _ => '$prefix暂时不可用，可以重试。',
      };
    }
    return '$prefix暂时不可用，可以重试。';
  }

  String _deleteFailureMessage(Object error) {
    if (error is AgentClientException &&
        error.kind == AgentClientFailureKind.authenticationRequired) {
      return '登录状态已失效，请重新登录。';
    }
    if (error is AgentClientException &&
        error.kind == AgentClientFailureKind.network) {
      return '录音尚未删除，请检查网络后重试。';
    }
    return '录音尚未删除，可以重试；确认文字不会受影响。';
  }
}

final class _AsrRetryCommand {
  const _AsrRetryCommand({
    required this.candidateId,
    required this.candidateVersion,
  });

  final String candidateId;
  final int candidateVersion;
}

final class _ConfirmationCommand {
  const _ConfirmationCommand({
    required this.candidateId,
    required this.candidateVersion,
    required this.clientMessageId,
    required this.confirmedText,
  });

  final String candidateId;
  final int candidateVersion;
  final String clientMessageId;
  final String confirmedText;
}

final class _ConfirmationReconciliationPending implements Exception {
  const _ConfirmationReconciliationPending();
}

final class _ConfirmationCommandConflict implements Exception {
  const _ConfirmationCommandConflict();
}

enum _VoiceRetry { start, upload, asr, restoreCandidate, confirm, run }

final class _WorkflowFence {
  const _WorkflowFence({
    required this.accountEpoch,
    required this.generation,
    required this.threadId,
    this.candidateId,
    this.candidateVersion,
  });

  final int accountEpoch;
  final int generation;
  final String threadId;
  final String? candidateId;
  final int? candidateVersion;

  _WorkflowFence withoutCandidate() {
    return _WorkflowFence(
      accountEpoch: accountEpoch,
      generation: generation,
      threadId: threadId,
    );
  }

  _WorkflowFence withoutCandidateVersion() {
    return _WorkflowFence(
      accountEpoch: accountEpoch,
      generation: generation,
      threadId: threadId,
      candidateId: candidateId,
    );
  }
}

final class _MediaFence {
  const _MediaFence({
    required this.accountEpoch,
    required this.generation,
    required this.threadId,
    required this.messageId,
    this.audioId,
  });

  final int accountEpoch;
  final int generation;
  final String threadId;
  final String messageId;
  final String? audioId;
}
