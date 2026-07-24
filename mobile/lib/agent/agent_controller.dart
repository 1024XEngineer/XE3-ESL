import 'package:flutter/foundation.dart';

import 'agent_client.dart';
import 'agent_models.dart';

final class AgentController extends ChangeNotifier {
  AgentController({required this.client});

  final AgentClient client;

  String? _threadId;
  AgentScene? _scene;
  List<AgentMessage> _messages = const <AgentMessage>[];
  PracticeRecordingState _recordingState = PracticeRecordingState.idle;
  String? _transcript;
  AgentReview? _review;
  String? _errorMessage;
  String? _retryText;
  int _completedTurns = 0;
  int _epoch = 0;
  bool _initialized = false;
  bool _busy = false;
  bool _disposed = false;
  Future<void>? _initializationFuture;

  String? get threadId => _threadId;
  AgentScene? get scene => _scene;
  List<AgentMessage> get messages => List.unmodifiable(_messages);
  PracticeRecordingState get recordingState => _recordingState;
  String? get transcript => _transcript;
  AgentReview? get review => _review;
  String? get errorMessage => _errorMessage;
  int get completedTurns => _completedTurns;
  bool get isBusy => _busy;
  bool get hasActivePractice => _scene != null && _review == null;

  Future<void> initialize() async {
    if (_initialized || _disposed) {
      return;
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
    _setBusy(true);
    try {
      final snapshot = await client.restoreThread();
      if (!_isCurrent(epoch)) {
        return;
      }
      _threadId = snapshot.threadId;
      _scene = snapshot.scene;
      _messages = List<AgentMessage>.from(snapshot.messages);
      _initialized = true;
      _errorMessage = null;
    } catch (_) {
      if (_isCurrent(epoch)) {
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
    final threadId = _threadId;
    if (threadId == null ||
        _busy ||
        _recordingState == PracticeRecordingState.transcribing ||
        _recordingState == PracticeRecordingState.submitting) {
      return;
    }
    final epoch = _epoch;
    _setBusy(true);
    try {
      final question = await client.startScene(
        threadId: threadId,
        scene: scene,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      _scene = scene;
      _messages = <AgentMessage>[question];
      _recordingState = PracticeRecordingState.idle;
      _transcript = null;
      _review = null;
      _completedTurns = 0;
      _errorMessage = null;
      _retryText = null;
    } catch (_) {
      if (_isCurrent(epoch)) {
        _errorMessage = '暂时无法开始这个场景，请稍后重试。';
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
    final threadId = _threadId;
    if (threadId == null || _busy) {
      return;
    }
    final epoch = _epoch;
    _setBusy(true);
    try {
      final exchange = await client.sendText(threadId: threadId, text: text);
      if (!_isCurrent(epoch)) {
        return;
      }
      _appendExchange(exchange);
      _errorMessage = null;
      _retryText = null;
    } catch (_) {
      if (_isCurrent(epoch)) {
        _retryText = text;
        _errorMessage = '消息没有发送成功，可以重试。';
      }
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> retryLastText() async {
    final text = _retryText;
    if (text == null) {
      return;
    }
    await sendText(text);
  }

  void startRecording() {
    if (!hasActivePractice ||
        _busy ||
        _recordingState != PracticeRecordingState.idle) {
      return;
    }
    _transcript = null;
    _errorMessage = null;
    _recordingState = PracticeRecordingState.recording;
    notifyListeners();
  }

  Future<void> stopRecording() async {
    final threadId = _threadId;
    if (threadId == null ||
        _recordingState != PracticeRecordingState.recording) {
      return;
    }
    final epoch = _epoch;
    _recordingState = PracticeRecordingState.transcribing;
    notifyListeners();
    try {
      final transcript = await client.transcribeTurn(
        threadId: threadId,
        turnNumber: _completedTurns + 1,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      _transcript = transcript;
      _recordingState = PracticeRecordingState.awaitingConfirmation;
      _errorMessage = null;
    } catch (_) {
      if (_isCurrent(epoch)) {
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = '没有识别出这一轮，请重新录音。';
      }
    }
    if (_isCurrent(epoch)) {
      notifyListeners();
    }
  }

  void rerecord() {
    if (_recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    _transcript = null;
    _recordingState = PracticeRecordingState.idle;
    _errorMessage = null;
    notifyListeners();
  }

  Future<void> confirmTranscript() async {
    final threadId = _threadId;
    final scene = _scene;
    final transcript = _transcript;
    if (threadId == null ||
        scene == null ||
        transcript == null ||
        _recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final epoch = _epoch;
    _recordingState = PracticeRecordingState.submitting;
    notifyListeners();
    try {
      final turnNumber = _completedTurns + 1;
      final exchange = await client.submitPracticeTurn(
        threadId: threadId,
        scene: scene,
        turnNumber: turnNumber,
        transcript: transcript,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      _appendExchange(exchange);
      _completedTurns = turnNumber;
      _transcript = null;
      _errorMessage = null;
      if (_completedTurns < 3) {
        _recordingState = PracticeRecordingState.idle;
        notifyListeners();
        return;
      }
    } catch (_) {
      if (_isCurrent(epoch)) {
        _recordingState = PracticeRecordingState.awaitingConfirmation;
        _errorMessage = '这一轮没有提交成功，请重试。';
      }
      if (_isCurrent(epoch)) {
        notifyListeners();
      }
      return;
    }
    await _generateReview(epoch: epoch, threadId: threadId, scene: scene);
  }

  Future<void> retryReview() async {
    final threadId = _threadId;
    final scene = _scene;
    if (threadId == null ||
        scene == null ||
        _completedTurns != 3 ||
        _review != null ||
        _recordingState != PracticeRecordingState.reviewFailed) {
      return;
    }
    final epoch = _epoch;
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    await _generateReview(epoch: epoch, threadId: threadId, scene: scene);
  }

  Future<void> clearPrivateState() async {
    _epoch++;
    _initializationFuture = null;
    _threadId = null;
    _scene = null;
    _messages = const <AgentMessage>[];
    _recordingState = PracticeRecordingState.idle;
    _transcript = null;
    _review = null;
    _errorMessage = null;
    _retryText = null;
    _completedTurns = 0;
    _initialized = false;
    _busy = false;
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _epoch++;
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

  bool _isCurrent(int epoch) => epoch == _epoch;

  void _setBusy(bool value) {
    if (_disposed) {
      return;
    }
    _busy = value;
    notifyListeners();
  }

  Future<void> _generateReview({
    required int epoch,
    required String threadId,
    required AgentScene scene,
  }) async {
    try {
      final review = await client.createReview(
        threadId: threadId,
        scene: scene,
      );
      if (!_isCurrent(epoch)) {
        return;
      }
      _review = review;
      _recordingState = PracticeRecordingState.completed;
      _errorMessage = null;
    } catch (_) {
      if (_isCurrent(epoch)) {
        _recordingState = PracticeRecordingState.reviewFailed;
        _errorMessage = '三轮回答已保存，但复盘暂时没有生成，可以重试。';
      }
    }
    if (_isCurrent(epoch)) {
      notifyListeners();
    }
  }
}
