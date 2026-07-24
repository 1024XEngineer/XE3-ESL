import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'agent_client.dart';
import 'agent_models.dart';

typedef AgentClientIdFactory = String Function(String scope);

final class AgentController extends ChangeNotifier {
  AgentController({required this.client, AgentClientIdFactory? clientIdFactory})
    : _clientIdFactory = clientIdFactory ?? _createSecureClientId;

  final AgentClient client;
  final AgentClientIdFactory _clientIdFactory;

  String? _threadId;
  AgentMatter? _activeMatter;
  List<AgentMessage> _messages = const <AgentMessage>[];
  PracticeRecordingState _recordingState = PracticeRecordingState.idle;
  String? _transcript;
  String? _activeTurnClientId;
  String? _pendingReviewClientId;
  AgentReview? _review;
  String? _errorMessage;
  _AgentRetry? _retry;
  int _completedTurns = 0;
  int _epoch = 0;
  int _practiceGeneration = 0;
  bool _initialized = false;
  bool _busy = false;
  bool _disposed = false;
  Future<void>? _initializationFuture;
  Future<void>? _accountCleanupFuture;

  String? get threadId => _threadId;
  AgentMatter? get activeMatter => _activeMatter;
  AgentScene? get scene => _activeMatter?.scene;
  List<AgentMessage> get messages => List.unmodifiable(_messages);
  PracticeRecordingState get recordingState => _recordingState;
  String? get transcript => _transcript;
  AgentReview? get review => _review;
  String? get errorMessage => _errorMessage;
  int get completedTurns => _completedTurns;
  bool get isBusy => _busy || _practiceRequestInFlight;
  bool get canRetry => _retry != null;
  bool get supportsPracticeFlow {
    return switch (client) {
      AgentPracticeAvailability(:final supportsPracticeFlow) =>
        supportsPracticeFlow,
      _ => true,
    };
  }

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

  bool get hasActivePractice => _activeMatter != null && _review == null;

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
    final operation = _restoreThread();
    _initializationFuture = operation;
    try {
      await operation;
    } finally {
      if (identical(_initializationFuture, operation)) {
        _initializationFuture = null;
      }
    }
  }

  Future<void> _restoreThread() async {
    final epoch = _epoch;
    _retry = null;
    _errorMessage = null;
    _setBusy(true);
    try {
      final snapshot = await client.restoreThread();
      if (!_isCurrent(epoch)) {
        return;
      }
      _validateSnapshot(snapshot);
      final practice = snapshot.practice;
      _practiceGeneration++;
      _threadId = snapshot.threadId;
      _activeMatter = snapshot.activeMatter;
      _messages = List<AgentMessage>.from(snapshot.messages);
      _completedTurns = practice?.completedTurns ?? 0;
      _review = practice?.review;
      _pendingReviewClientId = practice?.pendingReviewClientId;
      _activeTurnClientId = null;
      _transcript = null;
      _recordingState = switch ((_completedTurns, _review)) {
        (_, AgentReview()) => PracticeRecordingState.completed,
        (3, null) => PracticeRecordingState.reviewFailed,
        _ => PracticeRecordingState.idle,
      };
      _initialized = true;
      _retry = null;
      _errorMessage = _completedTurns == 3 && _review == null
          ? '三轮回答已保存，但复盘暂时没有生成，可以重试。'
          : null;
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
    if (threadId == null || !canSelectScene) {
      return;
    }
    final epoch = _epoch;
    _retry = null;
    _errorMessage = null;
    _setBusy(true);
    try {
      final result = await client.startScene(
        threadId: threadId,
        scene: operation.scene,
        clientOperationId: operation.clientOperationId,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      _practiceGeneration++;
      _activeMatter = result.activeMatter;
      _messages = <AgentMessage>[..._messages, result.assistantMessage];
      _recordingState = PracticeRecordingState.idle;
      _transcript = null;
      _activeTurnClientId = null;
      _pendingReviewClientId = null;
      _review = null;
      _completedTurns = 0;
      _retry = null;
      _errorMessage = null;
    } catch (error) {
      if (_isCurrent(epoch)) {
        if (error is AgentClientException && error.isUnavailable) {
          _retry = null;
          _errorMessage = '场景与语音练习尚未开放，当前可以继续使用 Agent 文本对话。';
        } else {
          _retry = _canRetry(error) ? operation : null;
          _errorMessage = '暂时无法开始这个场景，请稍后重试。';
        }
      }
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> sendText(String value) async {
    final text = value.trim();
    if (text.isEmpty) {
      return;
    }
    await _ensureInitialized();
    if (_threadId == null || isBusy || _disposed) {
      return;
    }
    await _sendText(
      _TextRetry(text: text, clientMessageId: _newClientId('message')),
    );
  }

  Future<void> _sendText(_TextRetry operation) async {
    final threadId = _threadId;
    if (threadId == null || isBusy || _disposed) {
      return;
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
        return;
      }
      _appendExchange(exchange);
      _retry = null;
      _errorMessage = null;
    } catch (error) {
      if (_isCurrent(epoch)) {
        _retry = _canRetry(error) ? operation : null;
        _errorMessage =
            error is AgentClientException &&
                error.kind == AgentClientFailureKind.runFailed &&
                !error.retryable
            ? '这次 Agent 运行未能完成，服务端不允许重试。'
            : '消息没有发送成功，可以重试。';
      }
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

  void startRecording() {
    if (!hasActivePractice ||
        isBusy ||
        _recordingState != PracticeRecordingState.idle) {
      return;
    }
    _practiceGeneration++;
    _activeTurnClientId = _newClientId('turn');
    _transcript = null;
    _retry = null;
    _errorMessage = null;
    _recordingState = PracticeRecordingState.recording;
    notifyListeners();
  }

  Future<void> stopRecording() async {
    final threadId = _threadId;
    final matterId = _activeMatter?.id;
    final clientTurnId = _activeTurnClientId;
    if (threadId == null ||
        matterId == null ||
        clientTurnId == null ||
        _recordingState != PracticeRecordingState.recording) {
      return;
    }
    final epoch = _epoch;
    final practiceGeneration = _practiceGeneration;
    _recordingState = PracticeRecordingState.transcribing;
    notifyListeners();
    try {
      final transcript = await client.transcribeTurn(
        threadId: threadId,
        turnNumber: _completedTurns + 1,
        clientTurnId: clientTurnId,
      );
      if (!_isCurrentPractice(
        epoch: epoch,
        practiceGeneration: practiceGeneration,
        threadId: threadId,
        matterId: matterId,
      )) {
        return;
      }
      _transcript = transcript;
      _recordingState = PracticeRecordingState.awaitingConfirmation;
      _errorMessage = null;
    } catch (_) {
      if (_isCurrentPractice(
        epoch: epoch,
        practiceGeneration: practiceGeneration,
        threadId: threadId,
        matterId: matterId,
      )) {
        _activeTurnClientId = null;
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = '没有识别出这一轮，请重新录音。';
      }
    }
    if (_isCurrentPractice(
      epoch: epoch,
      practiceGeneration: practiceGeneration,
      threadId: threadId,
      matterId: matterId,
    )) {
      notifyListeners();
    }
  }

  void rerecord() {
    if (_recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    _practiceGeneration++;
    _activeTurnClientId = null;
    _transcript = null;
    _recordingState = PracticeRecordingState.idle;
    _errorMessage = null;
    notifyListeners();
  }

  Future<void> confirmTranscript() async {
    final threadId = _threadId;
    final matter = _activeMatter;
    final transcript = _transcript;
    final clientTurnId = _activeTurnClientId;
    if (threadId == null ||
        matter == null ||
        transcript == null ||
        clientTurnId == null ||
        _completedTurns >= 3 ||
        _recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final epoch = _epoch;
    final practiceGeneration = _practiceGeneration;
    _recordingState = PracticeRecordingState.submitting;
    notifyListeners();
    try {
      final turnNumber = _completedTurns + 1;
      final exchange = await client.submitPracticeTurn(
        threadId: threadId,
        scene: matter.scene,
        turnNumber: turnNumber,
        transcript: transcript,
        clientTurnId: clientTurnId,
      );
      if (!_isCurrentPractice(
        epoch: epoch,
        practiceGeneration: practiceGeneration,
        threadId: threadId,
        matterId: matter.id,
      )) {
        return;
      }
      _appendExchange(exchange);
      _completedTurns = turnNumber;
      _activeTurnClientId = null;
      _transcript = null;
      _errorMessage = null;
      if (_completedTurns < 3) {
        _recordingState = PracticeRecordingState.idle;
        notifyListeners();
        return;
      }
    } catch (_) {
      if (_isCurrentPractice(
        epoch: epoch,
        practiceGeneration: practiceGeneration,
        threadId: threadId,
        matterId: matter.id,
      )) {
        _recordingState = PracticeRecordingState.awaitingConfirmation;
        _errorMessage = '这一轮没有提交成功，请重试。';
        notifyListeners();
      }
      return;
    }
    final clientReviewId = _pendingReviewClientId ??= _newClientId('review');
    await _generateReview(
      epoch: epoch,
      practiceGeneration: practiceGeneration,
      threadId: threadId,
      matter: matter,
      clientReviewId: clientReviewId,
    );
  }

  Future<void> retryReview() async {
    final threadId = _threadId;
    final matter = _activeMatter;
    if (threadId == null ||
        matter == null ||
        _completedTurns != 3 ||
        _review != null ||
        _recordingState != PracticeRecordingState.reviewFailed) {
      return;
    }
    final epoch = _epoch;
    final practiceGeneration = _practiceGeneration;
    final clientReviewId = _pendingReviewClientId ??= _newClientId('review');
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    await _generateReview(
      epoch: epoch,
      practiceGeneration: practiceGeneration,
      threadId: threadId,
      matter: matter,
      clientReviewId: clientReviewId,
    );
  }

  /// Invalidates private UI state synchronously, then waits until the client
  /// has cancelled account work and removed temporary private artifacts.
  Future<void> clearPrivateState() async {
    _epoch++;
    _practiceGeneration++;
    _initializationFuture = null;
    _threadId = null;
    _activeMatter = null;
    _messages = const <AgentMessage>[];
    _recordingState = PracticeRecordingState.idle;
    _transcript = null;
    _activeTurnClientId = null;
    _pendingReviewClientId = null;
    _review = null;
    _errorMessage = null;
    _retry = null;
    _completedTurns = 0;
    _initialized = false;
    _busy = false;
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
    _practiceGeneration++;
    _initializationFuture = null;
    super.dispose();
  }

  Future<void> _ensureInitialized() async {
    if (!_initialized) {
      await initialize();
    }
  }

  void _appendExchange(AgentExchange exchange) {
    _messages = <AgentMessage>[
      ..._messages,
      exchange.userMessage,
      ?exchange.assistantMessage,
    ];
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _epoch;

  bool _isCurrentPractice({
    required int epoch,
    required int practiceGeneration,
    required String threadId,
    required String matterId,
  }) {
    return _isCurrent(epoch) &&
        practiceGeneration == _practiceGeneration &&
        threadId == _threadId &&
        matterId == _activeMatter?.id;
  }

  void _setBusy(bool value) {
    if (_disposed) {
      return;
    }
    _busy = value;
    notifyListeners();
  }

  Future<void> _generateReview({
    required int epoch,
    required int practiceGeneration,
    required String threadId,
    required AgentMatter matter,
    required String clientReviewId,
  }) async {
    try {
      final review = await client.createReview(
        threadId: threadId,
        scene: matter.scene,
        clientReviewId: clientReviewId,
      );
      if (!_isCurrentPractice(
        epoch: epoch,
        practiceGeneration: practiceGeneration,
        threadId: threadId,
        matterId: matter.id,
      )) {
        return;
      }
      _review = review;
      _recordingState = PracticeRecordingState.completed;
      _errorMessage = null;
    } catch (_) {
      if (_isCurrentPractice(
        epoch: epoch,
        practiceGeneration: practiceGeneration,
        threadId: threadId,
        matterId: matter.id,
      )) {
        _recordingState = PracticeRecordingState.reviewFailed;
        _errorMessage = '三轮回答已保存，但复盘暂时没有生成，可以重试。';
      }
    }
    if (_isCurrentPractice(
      epoch: epoch,
      practiceGeneration: practiceGeneration,
      threadId: threadId,
      matterId: matter.id,
    )) {
      notifyListeners();
    }
  }

  void _validateSnapshot(AgentThreadSnapshot snapshot) {
    final practice = snapshot.practice;
    final activeMatter = snapshot.activeMatter;
    final review = practice?.review;
    final pendingReviewClientId = practice?.pendingReviewClientId;
    final messageIds = <String>{};
    final invalidMessages = snapshot.messages.any(
      (message) =>
          message.id.trim().isEmpty ||
          message.text.trim().isEmpty ||
          !messageIds.add(message.id),
    );
    final invalidPractice =
        practice != null &&
        (practice.completedTurns < 0 ||
            practice.completedTurns > 3 ||
            (review != null && practice.completedTurns != 3) ||
            (pendingReviewClientId != null &&
                (pendingReviewClientId.trim().isEmpty ||
                    practice.completedTurns != 3 ||
                    review != null)) ||
            (practice.completedTurns == 3 &&
                review == null &&
                pendingReviewClientId == null));
    final invalidReview =
        review != null &&
        (review.id.trim().isEmpty ||
            review.title.trim().isEmpty ||
            review.summary.trim().isEmpty ||
            review.strength.trim().isEmpty ||
            review.nextFocus.trim().isEmpty);
    if (snapshot.threadId.trim().isEmpty ||
        (activeMatter != null &&
            (activeMatter.id.trim().isEmpty ||
                activeMatter.scene.id.trim().isEmpty ||
                activeMatter.scene.title.trim().isEmpty)) ||
        (practice != null && snapshot.activeMatter == null) ||
        invalidPractice ||
        invalidReview ||
        invalidMessages) {
      throw StateError('Invalid Agent Thread snapshot.');
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
