import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

typedef AgentClientIdFactory = String Function(String scope);

final class AgentController extends ChangeNotifier {
  AgentController({
    required this.client,
    PracticeClient? practiceClient,
    PracticeRecorder? recorder,
    AgentClientIdFactory? clientIdFactory,
    Duration recordingLimit = const Duration(seconds: 58),
  }) : practiceClient = practiceClient ?? LegacyAgentPracticeClient(client),
       recorder = recorder ?? FakePracticeRecorder(),
       _clientIdFactory = clientIdFactory ?? _createSecureClientId,
       _recordingLimit = recordingLimit {
    if (recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 60)) {
      throw ArgumentError.value(
        recordingLimit,
        'recordingLimit',
        'must be positive and no longer than the server 60-second limit',
      );
    }
  }

  final AgentClient client;
  final PracticeClient? practiceClient;
  final PracticeRecorder recorder;
  final AgentClientIdFactory _clientIdFactory;
  final Duration _recordingLimit;

  String? _threadId;
  String? _practiceSessionId;
  PracticeQuestion? _currentQuestion;
  TranscriptionCandidate? _candidate;
  String? _activeConfirmationId;
  AgentMatter? _activeMatter;
  List<AgentMessage> _messages = const <AgentMessage>[];
  PracticeRecordingState _recordingState = PracticeRecordingState.idle;
  AgentReview? _review;
  String? _errorMessage;
  _AgentRetry? _retry;
  int _completedTurns = 0;
  int _turnLimit = 0;
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

  String? get threadId => _threadId;
  String? get practiceSessionId => _practiceSessionId;
  String? get questionId => _currentQuestion?.id;
  String? get candidateId => _candidate?.id;
  AgentMatter? get activeMatter => _activeMatter;
  AgentScene? get scene => _activeMatter?.scene;
  List<AgentMessage> get messages => List.unmodifiable(_messages);
  PracticeRecordingState get recordingState => _recordingState;
  String? get transcript => _candidate?.text;
  AgentReview? get review => _review;
  String? get errorMessage => _errorMessage;
  int get completedTurns => _completedTurns;
  int get turnLimit => _turnLimit;
  bool get isBusy => _busy || _practiceRequestInFlight;
  bool get canRetry => _retry != null;
  bool get supportsPracticeFlow => practiceClient != null;

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

  bool get _isSessionCompleted =>
      _turnLimit > 0 && _completedTurns == _turnLimit;

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
    final epoch = _epoch;
    _retry = null;
    _errorMessage = null;
    _setBusy(true);
    try {
      final thread = await client.restoreThread();
      _validateThreadSnapshot(thread);
      if (practiceClient case final LegacyAgentPracticeClient legacy) {
        legacy.seedRestoredThread(thread);
      }
      final practice = await practiceClient?.restorePractice(
        threadId: thread.threadId,
        activeMatter: thread.activeMatter,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      _threadId = thread.threadId;
      _messages = List<AgentMessage>.from(thread.messages);
      _applyPracticeSnapshot(practice);
      _initialized = true;
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

  Future<void> selectScene(AgentScene scene) async {
    await _ensureInitialized();
    if (!canSelectScene) {
      return;
    }
    await _selectScene(
      _SceneRetry(scene: scene, clientOperationId: _newClientId('scene')),
    );
  }

  Future<void> _selectScene(_SceneRetry operation) async {
    final threadId = _threadId;
    final practice = practiceClient;
    if (threadId == null || practice == null || !canSelectScene) {
      return;
    }
    final epoch = _epoch;
    _retry = null;
    _errorMessage = null;
    _setBusy(true);
    try {
      final selection = await client.startScene(
        threadId: threadId,
        scene: operation.scene,
        clientOperationId: operation.clientOperationId,
      );
      if (practice case final LegacyAgentPracticeClient legacy) {
        legacy.seedSceneSelection(selection);
      }
      final result = await practice.startPractice(
        threadId: threadId,
        activeMatter: selection.activeMatter,
        clientOperationId: operation.clientOperationId,
      );
      if (!_isCurrent(epoch)) {
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
    await _ensureInitialized();
    if (_threadId == null || isBusy || _disposed) {
      return false;
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
    final epoch = _epoch;
    _retry = null;
    _errorMessage = null;
    _setBusy(true);
    try {
      final exchange = await client.sendText(
        threadId: threadId,
        text: operation.text,
        clientMessageId: operation.clientMessageId,
      );
      if (!_isCurrent(epoch)) {
        return false;
      }
      _appendMessages([exchange.userMessage, ?exchange.assistantMessage]);
      return true;
    } catch (error) {
      if (_isCurrent(epoch)) {
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
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
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

  Future<void> startRecording() {
    if (!hasActivePractice ||
        isBusy ||
        _currentQuestion == null ||
        _recordingState != PracticeRecordingState.idle) {
      return Future<void>.value();
    }
    final generation = ++_practiceGeneration;
    _candidate = null;
    _activeConfirmationId = null;
    _retry = null;
    _errorMessage = null;
    _cancelRecordingLimit();
    _recordingState = PracticeRecordingState.starting;
    notifyListeners();
    final operation = _startRecorder(generation);
    _recorderStartFuture = operation;
    return operation.whenComplete(() {
      if (identical(_recorderStartFuture, operation)) {
        _recorderStartFuture = null;
      }
    });
  }

  Future<void> _startRecorder(int generation) async {
    try {
      await recorder.start();
      if (generation != _practiceGeneration || _disposed) {
        await recorder.discardCurrent();
        return;
      }
      _recordingState = PracticeRecordingState.recording;
      _recordingLimitTimer = Timer(_recordingLimit, () {
        if (generation == _practiceGeneration &&
            !_disposed &&
            _recordingState == PracticeRecordingState.recording) {
          unawaited(stopRecording());
        }
      });
    } on PracticeRecordingException catch (error) {
      _recordingState = PracticeRecordingState.idle;
      _errorMessage =
          error.kind == PracticeRecordingFailureKind.permissionDenied
          ? '需要麦克风权限；请在 iOS“设置”中允许 SpeakUp 使用麦克风。'
          : '暂时无法开始录音，请稍后重试。';
    } catch (_) {
      _recordingState = PracticeRecordingState.idle;
      _errorMessage = '暂时无法开始录音，请稍后重试。';
    }
    if (!_disposed) {
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
    final epoch = _epoch;
    final generation = _practiceGeneration;
    final clientTurnId = _newClientId('turn');
    final operation = _stopRecording(
      practice: practice,
      sessionId: sessionId,
      question: question,
      epoch: epoch,
      generation: generation,
      clientTurnId: clientTurnId,
    );
    _stopRecordingFuture = operation;
    return operation.whenComplete(() {
      if (identical(_stopRecordingFuture, operation)) {
        _stopRecordingFuture = null;
      }
    });
  }

  Future<void> _stopRecording({
    required PracticeClient practice,
    required String sessionId,
    required PracticeQuestion question,
    required int epoch,
    required int generation,
    required String clientTurnId,
  }) async {
    RecordedPracticeAudio? audio;
    _recordingState = PracticeRecordingState.transcribing;
    notifyListeners();
    try {
      audio = await recorder.stop();
      if (!_isCurrentPractice(
        epoch: epoch,
        generation: generation,
        sessionId: sessionId,
        questionId: question.id,
      )) {
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
      if (!_isCurrentPractice(
        epoch: epoch,
        generation: generation,
        sessionId: sessionId,
        questionId: question.id,
      )) {
        return;
      }
      _validateCandidate(candidate, sessionId, question.id);
      _candidate = candidate;
      _activeConfirmationId = null;
      _recordingState = PracticeRecordingState.awaitingConfirmation;
      _errorMessage = null;
    } catch (error) {
      if (_isCurrentPractice(
        epoch: epoch,
        generation: generation,
        sessionId: sessionId,
        questionId: question.id,
      )) {
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
    if (_isCurrentPractice(
      epoch: epoch,
      generation: generation,
      sessionId: sessionId,
      questionId: question.id,
    )) {
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
    final epoch = _epoch;
    final generation = _practiceGeneration;
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      final confirmation = await practice.confirm(
        sessionId: sessionId,
        questionId: question.id,
        candidateId: candidate.id,
        idempotencyKey: _activeConfirmationId ??= _newClientId('confirm'),
      );
      if (!_isCurrentPractice(
        epoch: epoch,
        generation: generation,
        sessionId: sessionId,
        questionId: question.id,
      )) {
        return;
      }
      _validateConfirmation(confirmation, candidate);
      _completedTurns = confirmation.completedTurns;
      _turnLimit = confirmation.turnLimit;
      _currentQuestion = confirmation.nextQuestion;
      _review = confirmation.review;
      _candidate = null;
      _activeConfirmationId = null;
      _appendMessages([
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
    } catch (error) {
      if (_isCurrentPractice(
        epoch: epoch,
        generation: generation,
        sessionId: sessionId,
        questionId: question.id,
      )) {
        _recordingState = PracticeRecordingState.awaitingConfirmation;
        _errorMessage = _confirmationFailureMessage(error);
      }
    }
    if (_isCurrent(epoch)) {
      notifyListeners();
    }
  }

  /// Refetches the server-owned result. Flutter never creates a Review.
  Future<void> retryReview() async {
    final practice = practiceClient;
    final threadId = _threadId;
    if (practice == null ||
        threadId == null ||
        !_isSessionCompleted ||
        _review != null ||
        _recordingState != PracticeRecordingState.reviewFailed) {
      return;
    }
    final epoch = _epoch;
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      final snapshot = await practice.restorePractice(
        threadId: threadId,
        activeMatter: _activeMatter,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      if (snapshot == null) {
        throw StateError('Practice Session is not restorable.');
      }
      _applyPracticeSnapshot(snapshot);
      if (_review == null) {
        throw StateError('Review is not ready.');
      }
    } catch (error) {
      if (_isCurrent(epoch)) {
        _recordingState = PracticeRecordingState.reviewFailed;
        _errorMessage = _reviewFailureMessage(error);
      }
    }
    if (_isCurrent(epoch)) {
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
    _practiceSessionId = null;
    _currentQuestion = null;
    _candidate = null;
    _activeConfirmationId = null;
    _activeMatter = null;
    _messages = const <AgentMessage>[];
    _recordingState = PracticeRecordingState.idle;
    _review = null;
    _errorMessage = null;
    _retry = null;
    _completedTurns = 0;
    _turnLimit = 0;
    _initialized = false;
    _busy = false;
    if (!_disposed) {
      notifyListeners();
    }

    final cleanup = Future.wait<void>([
      Future<void>.sync(client.clearAccountState),
      if (practiceClient case final practice?)
        Future<void>.sync(practice.clearAccountState),
      Future<void>.sync(() async {
        await _recorderStartFuture;
        await _stopRecordingFuture;
        await recorder.clearAccountState();
      }),
    ]);
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
    _cancelRecordingLimit();
    _epoch++;
    _practiceGeneration++;
    _initializationFuture = null;
    unawaited(recorder.discardCurrent());
    super.dispose();
  }

  Future<void> _ensureInitialized() async {
    if (!_initialized) {
      await initialize();
    }
  }

  void _applyPracticeSnapshot(PracticeSessionSnapshot? snapshot) {
    _cancelRecordingLimit();
    _practiceGeneration++;
    _candidate = null;
    _activeConfirmationId = null;
    if (snapshot == null) {
      _practiceSessionId = null;
      _currentQuestion = null;
      _activeMatter = null;
      _completedTurns = 0;
      _turnLimit = 0;
      _review = null;
      _recordingState = PracticeRecordingState.idle;
      return;
    }
    _validatePracticeSnapshot(snapshot);
    _practiceSessionId = snapshot.sessionId;
    _currentQuestion = snapshot.currentQuestion;
    _activeMatter = snapshot.matter;
    _completedTurns = snapshot.completedTurns;
    _turnLimit = snapshot.turnLimit;
    _review = snapshot.review;
    _appendMessages([?snapshot.currentQuestion?.presentation]);
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
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _epoch;

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

  bool _isCurrentPractice({
    required int epoch,
    required int generation,
    required String sessionId,
    required String questionId,
  }) {
    return _isCurrent(epoch) &&
        generation == _practiceGeneration &&
        sessionId == _practiceSessionId &&
        questionId == _currentQuestion?.id;
  }

  void _setBusy(bool value) {
    if (_disposed) {
      return;
    }
    _busy = value;
    notifyListeners();
  }

  void _validateThreadSnapshot(AgentThreadSnapshot snapshot) {
    final messageIds = <String>{};
    final invalidMessages = snapshot.messages.any(
      (message) =>
          message.id.trim().isEmpty ||
          message.text.trim().isEmpty ||
          !messageIds.add(message.id),
    );
    final recovery = snapshot.textRecovery;
    if (snapshot.threadId.trim().isEmpty ||
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

  void _validatePracticeSnapshot(PracticeSessionSnapshot snapshot) {
    if (snapshot.sessionId.trim().isEmpty ||
        snapshot.matter.id.trim().isEmpty ||
        snapshot.matter.scene.id.trim().isEmpty ||
        snapshot.completedTurns < 0 ||
        snapshot.turnLimit < 1 ||
        snapshot.completedTurns > snapshot.turnLimit ||
        snapshot.sessionCompleted !=
            (snapshot.completedTurns == snapshot.turnLimit) ||
        (!snapshot.sessionCompleted && snapshot.currentQuestion == null) ||
        (snapshot.currentQuestion != null &&
            snapshot.currentQuestion!.sessionId != snapshot.sessionId)) {
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
    if (confirmation.turnId.trim().isEmpty ||
        confirmation.sessionId != candidate.sessionId ||
        confirmation.questionId != candidate.questionId ||
        confirmation.candidateId != candidate.id ||
        confirmation.answer.text != candidate.text ||
        confirmation.completedTurns < 1 ||
        confirmation.turnLimit < 1 ||
        confirmation.completedTurns > confirmation.turnLimit ||
        confirmation.sessionCompleted !=
            (confirmation.completedTurns == confirmation.turnLimit) ||
        (!confirmation.sessionCompleted && confirmation.nextQuestion == null) ||
        (confirmation.nextQuestion != null &&
            confirmation.nextQuestion!.sessionId != confirmation.sessionId)) {
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

sealed class _AgentRetry {
  const _AgentRetry();
}

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
