import 'dart:async';
import 'dart:developer' as developer;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

typedef AvatarControllerFactory = AvatarController Function();

typedef PracticeAvatarSessionBuilder =
    Widget Function(BuildContext context, PracticeAvatarSessionView avatar);

final class PracticeAvatarSessionView {
  const PracticeAvatarSessionView({
    required this.surfaceBuilder,
    required this.surfaceVisible,
    required this.statusLabel,
    required this.interruptForUserTurn,
    required this.onReplayQuestion,
    required this.replayLoading,
    required this.replayPlaying,
  });

  final WidgetBuilder? surfaceBuilder;
  final bool surfaceVisible;
  final String? statusLabel;
  final Future<void> Function() interruptForUserTurn;
  final Future<void> Function()? onReplayQuestion;
  final bool replayLoading;
  final bool replayPlaying;
}

/// Owns one avatar connection while the supplied practice page owns its UI.
class PracticeAvatarSession extends StatefulWidget {
  const PracticeAvatarSession({
    required this.practiceController,
    required this.avatarControllerFactory,
    required this.builder,
    required this.surfaceKey,
    super.key,
  });

  final PracticeController practiceController;
  final AvatarControllerFactory avatarControllerFactory;
  final PracticeAvatarSessionBuilder builder;
  final Key surfaceKey;

  @override
  State<PracticeAvatarSession> createState() => _PracticeAvatarSessionState();
}

/// Owns one avatar connection for one scenario practice route.
///
/// The product shell remains vendor-neutral: this coordinator only knows the
/// AvatarController boundary, while the composition root chooses Spatius (or a
/// future provider).
class ScenarioPracticeSession extends StatelessWidget {
  const ScenarioPracticeSession({
    required this.practiceController,
    required this.avatarControllerFactory,
    this.onPracticeCompleted,
    this.onOpenReport,
    this.speechFeedbackController,
    this.onExitRequested,
    super.key,
  });

  final PracticeController practiceController;
  final AvatarControllerFactory avatarControllerFactory;
  final Future<bool> Function()? onPracticeCompleted;
  final OpenScenarioPracticeReport? onOpenReport;
  final SpeechFeedbackController? speechFeedbackController;
  final Future<bool> Function()? onExitRequested;

  @override
  Widget build(BuildContext context) {
    return PracticeAvatarSession(
      practiceController: practiceController,
      avatarControllerFactory: avatarControllerFactory,
      surfaceKey: const Key('scenario-avatar-surface'),
      builder: (context, avatar) => ScenarioPracticePage(
        practiceController: practiceController,
        questionSpeaker: practiceController.promptSpeaker,
        avatarSurfaceBuilder: avatar.surfaceBuilder,
        avatarSurfaceVisible: avatar.surfaceVisible,
        avatarStatusLabel: avatar.statusLabel,
        onBeforeStartRecording: avatar.interruptForUserTurn,
        onBeforeSubmitText: avatar.interruptForUserTurn,
        onReplayQuestion: avatar.onReplayQuestion,
        onPracticeCompleted: onPracticeCompleted,
        onOpenReport: onOpenReport,
        speechFeedbackController: speechFeedbackController,
        replayLoading: avatar.replayLoading,
        replayPlaying: avatar.replayPlaying,
        onExitRequested: onExitRequested,
      ),
    );
  }
}

class _PracticeAvatarSessionState extends State<PracticeAvatarSession>
    with WidgetsBindingObserver {
  static const _avatarReadinessTimeout = Duration(seconds: 15);
  static const _userTurnInterruptBudget = Duration(milliseconds: 500);
  static const _maximumConnectionAttempts = 3;

  late AvatarController _avatarController;
  Timer? _readinessTimer;
  Timer? _reconnectTimer;
  String? _connectionAttemptedSessionId;
  String? _autoHandledQuestionId;
  String? _observedQuestionId;
  bool _readinessExpired = false;
  bool _connectionInFlight = false;
  bool _speechInFlight = false;
  bool _replayLoading = false;
  bool _syncInFlight = false;
  bool _syncAgain = false;
  bool _foreground = true;
  bool _hasLiveAvatarController = true;
  bool _hasUsableAvatarSurface = false;
  bool _reconnecting = false;
  bool _disposed = false;
  int _connectionAttempts = 0;
  int _connectionAttemptSequence = 0;
  int _connectionOperationId = 0;
  int _generation = 0;
  int _lastExhaustedAttemptSequence = 0;
  String? _lastConnectionDiagnostic;
  Future<void> _lifecycleTail = Future<void>.value();
  Future<void> _controllerReplacementTail = Future<void>.value();

  @override
  void initState() {
    super.initState();
    _avatarController = widget.avatarControllerFactory();
    _observedQuestionId = widget.practiceController.questionId;
    widget.practiceController.addListener(_handlePracticeState);
    _avatarController.addListener(_handleAvatarState);
    WidgetsBinding.instance.addObserver(this);
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    _foreground = lifecycle == null || lifecycle == AppLifecycleState.resumed;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_foreground) {
        _scheduleSync();
      } else {
        _queueLifecycleReconciliation(replaceController: true);
      }
    });
  }

  @override
  void didUpdateWidget(covariant PracticeAvatarSession oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.practiceController == widget.practiceController) {
      return;
    }
    oldWidget.practiceController.removeListener(_handlePracticeState);
    widget.practiceController.addListener(_handlePracticeState);
    _observedQuestionId = widget.practiceController.questionId;
    _autoHandledQuestionId = null;
    _generation++;
    _scheduleSync();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    _generation++;
    _cancelConnectionTimers();
    if (!_foreground) {
      // Never replay an interrupted question just because the App resumes.
      _autoHandledQuestionId = widget.practiceController.questionId;
    }
    _queueLifecycleReconciliation(replaceController: !_foreground);
  }

  void _queueLifecycleReconciliation({bool replaceController = false}) {
    final previous = _lifecycleTail;
    final operation = previous
        .catchError((_) {})
        .then((_) => _reconcileLifecycle(replaceController: replaceController));
    _lifecycleTail = operation;
    unawaited(operation);
  }

  Future<void> _reconcileLifecycle({required bool replaceController}) async {
    if (_disposed) {
      return;
    }
    if (replaceController || !_foreground || !_hasLiveAvatarController) {
      await _replaceAvatarController(resetConnectionAttempts: true);
    }
    if (!_disposed && _foreground && _hasLiveAvatarController) {
      _scheduleSync();
    }
  }

  void _cancelConnectionTimers() {
    _readinessTimer?.cancel();
    _readinessTimer = null;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
  }

  Future<void> _replaceAvatarController({
    required bool resetConnectionAttempts,
  }) {
    final previous = _controllerReplacementTail;
    final operation = previous.catchError((_) {}).then((_) async {
      if (_disposed) {
        return;
      }
      _cancelConnectionTimers();
      _connectionOperationId++;
      _connectionInFlight = false;
      _connectionAttemptedSessionId = null;
      _readinessExpired = false;
      _hasUsableAvatarSurface = false;
      if (resetConnectionAttempts) {
        _connectionAttempts = 0;
        _reconnecting = false;
      }

      if (_hasLiveAvatarController) {
        final previousController = _avatarController;
        _hasLiveAvatarController = false;
        previousController.removeListener(_handleAvatarState);
        await _bestEffort(previousController.close);
      }

      if (_disposed || !_foreground) {
        if (mounted) {
          setState(() {});
        }
        return;
      }

      final replacement = widget.avatarControllerFactory();
      if (_disposed || !_foreground) {
        await _bestEffort(replacement.close);
        return;
      }
      _avatarController = replacement;
      _hasLiveAvatarController = true;
      replacement.addListener(_handleAvatarState);
      if (mounted) {
        setState(() {});
      }
    });
    _controllerReplacementTail = operation;
    return operation;
  }

  void _handlePracticeState() {
    final questionId = widget.practiceController.questionId;
    if (questionId != _observedQuestionId) {
      _observedQuestionId = questionId;
      _autoHandledQuestionId = null;
      _generation++;
      if (_hasLiveAvatarController) {
        unawaited(_avatarController.interrupt());
      }
    }
    if (mounted) {
      setState(() {});
    }
    _scheduleSync();
  }

  void _handleAvatarState() {
    if (!_hasLiveAvatarController) {
      return;
    }
    final avatarState = _avatarController.state;
    _logConnectionState(avatarState);
    if (switch (avatarState.renderer.connection) {
      AvatarRendererConnection.surfaceReady ||
      AvatarRendererConnection.connecting ||
      AvatarRendererConnection.connected => true,
      _ => false,
    }) {
      _hasUsableAvatarSurface = true;
    }
    if (avatarState.canUseAvatar) {
      _connectionAttempts = 0;
      _reconnecting = false;
      _readinessTimer?.cancel();
      _readinessTimer = null;
      _reconnectTimer?.cancel();
      _reconnectTimer = null;
    }
    if (avatarState.failure != null) {
      _readinessTimer?.cancel();
      _readinessTimer = null;
      if (!_isRetryableAvatarFailure(avatarState.failure) ||
          _connectionAttempts >= _maximumConnectionAttempts) {
        _reconnecting = false;
      }
    }
    final sessionId = widget.practiceController.practiceSessionId;
    if (sessionId != null &&
        !_connectionInFlight &&
        !avatarState.canUseAvatar &&
        _isRetryableAvatarFailure(avatarState.failure)) {
      _scheduleReconnect(sessionId);
    }
    if (mounted) {
      setState(() {});
    }
    _scheduleSync();
  }

  void _scheduleSync() {
    if (_disposed || !_foreground) {
      return;
    }
    if (_syncInFlight) {
      _syncAgain = true;
      return;
    }
    unawaited(_runSyncLoop());
  }

  Future<void> _runSyncLoop() async {
    if (_syncInFlight || _disposed) {
      return;
    }
    _syncInFlight = true;
    try {
      do {
        _syncAgain = false;
        await _synchronize();
      } while (_syncAgain && !_disposed && _foreground);
    } finally {
      _syncInFlight = false;
    }
  }

  Future<void> _synchronize() async {
    final sessionId = widget.practiceController.practiceSessionId;
    if (sessionId == null || sessionId.isEmpty) {
      return;
    }
    if (!_hasLiveAvatarController) {
      _queueLifecycleReconciliation();
      return;
    }
    if (_connectionAttemptedSessionId != sessionId && !_connectionInFlight) {
      await _connect(sessionId);
    }
    if (_disposed || !_foreground || !_hasLiveAvatarController) {
      return;
    }
    final question = widget.practiceController.currentQuestion;
    if (question == null ||
        _speechInFlight ||
        question.id == _autoHandledQuestionId ||
        !_canSpeakNow(widget.practiceController)) {
      return;
    }
    final phase = _avatarController.state.phase;
    final retrying =
        _isRetryableAvatarFailure(_avatarController.state.failure) &&
        _connectionAttempts < _maximumConnectionAttempts &&
        (_connectionInFlight || _reconnectTimer != null);
    if (retrying) {
      return;
    }
    final readyForQuestion =
        _avatarController.state.canUseAvatar ||
        phase == AvatarControllerPhase.failed ||
        _readinessExpired;
    if (!readyForQuestion) {
      return;
    }
    _autoHandledQuestionId = question.id;
    if (widget.practiceController.supportsRealtimeQuestionSpeech) {
      await _speakRealtimeQuestion(question);
      return;
    }
    await _speakQuestion(question);
  }

  Future<void> _connect(String sessionId) async {
    if (!_hasLiveAvatarController || _disposed || !_foreground) {
      return;
    }
    final controller = _avatarController;
    final operationId = ++_connectionOperationId;
    _connectionInFlight = true;
    _connectionAttemptedSessionId = sessionId;
    _connectionAttempts++;
    _connectionAttemptSequence++;
    _readinessExpired = false;
    _readinessTimer?.cancel();
    _readinessTimer = null;
    var retry = false;
    try {
      final connected = await controller
          .connect(practiceSessionId: sessionId)
          .timeout(_avatarReadinessTimeout);
      if (!_connectionFenceMatches(
        controller: controller,
        operationId: operationId,
        sessionId: sessionId,
      )) {
        return;
      }
      if (!connected) {
        retry = _isRetryableAvatarFailure(controller.state.failure);
        _readinessExpired = !retry;
        return;
      }
      if (controller.state.canUseAvatar) {
        _reconnectTimer?.cancel();
        _reconnectTimer = null;
        return;
      }
      final failure = controller.state.failure;
      if (_isRetryableAvatarFailure(failure)) {
        retry = true;
        return;
      }
      if (failure != null) {
        _readinessExpired = true;
        return;
      }
      _readinessTimer = Timer(_avatarReadinessTimeout, () {
        if (!_connectionFenceMatches(
              controller: controller,
              operationId: operationId,
              sessionId: sessionId,
            ) ||
            controller.state.canUseAvatar) {
          return;
        }
        _scheduleReconnect(sessionId);
        _scheduleSync();
        if (mounted) {
          setState(() {});
        }
      });
    } on TimeoutException {
      if (_connectionFenceMatches(
        controller: controller,
        operationId: operationId,
        sessionId: sessionId,
      )) {
        retry = true;
        _readinessExpired = false;
      }
    } catch (_) {
      if (_connectionFenceMatches(
        controller: controller,
        operationId: operationId,
        sessionId: sessionId,
      )) {
        retry = true;
        _readinessExpired = false;
      }
    } finally {
      if (operationId == _connectionOperationId) {
        _connectionInFlight = false;
        if (retry) {
          _scheduleReconnect(sessionId);
        }
        if (mounted) {
          setState(() {});
        }
      }
    }
  }

  bool _connectionFenceMatches({
    required AvatarController controller,
    required int operationId,
    required String sessionId,
  }) {
    return !_disposed &&
        _foreground &&
        _hasLiveAvatarController &&
        identical(_avatarController, controller) &&
        operationId == _connectionOperationId &&
        widget.practiceController.practiceSessionId == sessionId;
  }

  bool _isRetryableAvatarFailure(AvatarRendererFailure? failure) {
    return failure == AvatarRendererFailure.network ||
        failure == AvatarRendererFailure.sessionExpired ||
        failure == AvatarRendererFailure.rendering ||
        failure == AvatarRendererFailure.unavailable;
  }

  void _scheduleReconnect(String sessionId) {
    if (_disposed ||
        !_foreground ||
        !_hasLiveAvatarController ||
        widget.practiceController.practiceSessionId != sessionId) {
      return;
    }
    if (_connectionAttempts >= _maximumConnectionAttempts) {
      _reconnecting = false;
      _readinessExpired = true;
      if (_lastExhaustedAttemptSequence != _connectionAttemptSequence) {
        _lastExhaustedAttemptSequence = _connectionAttemptSequence;
        developer.log(
          'avatar_connection attempt=$_connectionAttemptSequence '
          'state=exhausted mapped_failure='
          '${_avatarController.state.failure?.name ?? 'none'}',
          name: 'speakup.avatar',
        );
      }
      return;
    }
    if (_reconnectTimer != null) {
      return;
    }
    _reconnecting = true;
    final delay = Duration(milliseconds: 500 * (_connectionAttempts + 1));
    developer.log(
      'avatar_connection attempt=$_connectionAttemptSequence '
      'state=reconnect_scheduled mapped_failure='
      '${_avatarController.state.failure?.name ?? 'none'}',
      name: 'speakup.avatar',
    );
    _reconnectTimer = Timer(delay, () async {
      _reconnectTimer = null;
      if (_disposed ||
          !_foreground ||
          widget.practiceController.practiceSessionId != sessionId) {
        return;
      }
      await _replaceAvatarController(resetConnectionAttempts: false);
      if (_disposed ||
          !_foreground ||
          !_hasLiveAvatarController ||
          widget.practiceController.practiceSessionId != sessionId) {
        return;
      }
      _connectionAttemptedSessionId = null;
      _scheduleSync();
    });
  }

  Future<void> _speakQuestion(
    PracticeQuestion question, {
    bool replay = false,
  }) async {
    if (!_hasLiveAvatarController) {
      return;
    }
    final avatarController = _avatarController;
    final mediaClient = widget.practiceController.mediaClient;
    final speechPath = question.speechPath;
    if (_speechInFlight || mediaClient == null || speechPath == null) {
      return;
    }
    final sessionId = widget.practiceController.practiceSessionId;
    if (sessionId == null || question.sessionId != sessionId) {
      return;
    }
    final generation = ++_generation;
    _speechInFlight = true;
    if (replay) {
      _replayLoading = true;
    }
    if (mounted) {
      setState(() {});
    }

    Uint8List? bytes;
    try {
      await widget.practiceController.stopPracticeAudio(notify: false);
      bytes = await mediaClient.loadQuestionSpeech(speechPath);
      if (!_speechFenceMatches(
        generation: generation,
        sessionId: sessionId,
        questionId: question.id,
        speechPath: speechPath,
        avatarController: avatarController,
      )) {
        return;
      }
      _replayLoading = false;
      if (mounted) {
        setState(() {});
      }
      await avatarController.speakWav(bytes);
    } catch (_) {
      // The scenario remains usable through text/recording when media or the
      // provider is unavailable. A replay tap can retry the same question.
    } finally {
      if (bytes != null) {
        try {
          bytes.fillRange(0, bytes.length, 0);
        } catch (_) {
          // Production media clients return an owned mutable byte buffer.
        }
      }
      _speechInFlight = false;
      _replayLoading = false;
      if (mounted) {
        setState(() {});
      }
      _scheduleSync();
    }
  }

  Future<void> _speakRealtimeQuestion(
    PracticeQuestion question, {
    bool replay = false,
  }) async {
    if (_speechInFlight || !_hasLiveAvatarController) {
      return;
    }
    final sessionId = widget.practiceController.practiceSessionId;
    if (sessionId == null || question.sessionId != sessionId) {
      return;
    }
    final avatarController = _avatarController;
    final nativeOutput = widget.practiceController.questionSpeechPlayer;
    if (nativeOutput == null) {
      return;
    }
    _speechInFlight = true;
    if (replay) {
      _replayLoading = true;
    }
    if (mounted) {
      setState(() {});
    }
    try {
      await widget.practiceController.playCurrentQuestionSpeech(
        output: _AvatarOrNativeQuestionSpeechSink(
          avatarController,
          nativeOutput,
          _readinessExpired,
        ),
      );
    } finally {
      _speechInFlight = false;
      _replayLoading = false;
      if (mounted) {
        setState(() {});
      }
      _scheduleSync();
    }
  }

  bool _speechFenceMatches({
    required int generation,
    required String sessionId,
    required String questionId,
    required String speechPath,
    required AvatarController avatarController,
  }) {
    final current = widget.practiceController.currentQuestion;
    return !_disposed &&
        _foreground &&
        _hasLiveAvatarController &&
        identical(_avatarController, avatarController) &&
        generation == _generation &&
        widget.practiceController.practiceSessionId == sessionId &&
        current?.id == questionId &&
        current?.speechPath == speechPath;
  }

  Future<void> _replayQuestion() async {
    if (_replayLoading || !_canSpeakNow(widget.practiceController)) {
      return;
    }
    if (_isAvatarSpeaking ||
        widget.practiceController.isQuestionAudioLoading ||
        widget.practiceController.isQuestionAudioPlaying) {
      await _interruptForUserTurn();
      return;
    }
    if (widget.practiceController.supportsRealtimeQuestionSpeech) {
      final question = widget.practiceController.currentQuestion;
      if (question != null) {
        await _speakRealtimeQuestion(question, replay: true);
      }
      return;
    }
    final question = widget.practiceController.currentQuestion;
    if (question != null) {
      await _speakQuestion(question, replay: true);
    }
  }

  Future<void> _interruptForUserTurn() async {
    _generation++;
    final operations = <Future<void>>[
      _bestEffort(
        () => widget.practiceController.stopPracticeAudio(notify: false),
      ),
    ];
    if (_hasLiveAvatarController) {
      operations.add(_bestEffort(_avatarController.interrupt));
    }
    final interruption = Future.wait<void>(operations);
    await _waitAtMost(interruption, _userTurnInterruptBudget);
  }

  bool get _isAvatarSpeaking {
    if (!_hasLiveAvatarController) {
      return false;
    }
    final state = _avatarController.state;
    return state.phase == AvatarControllerPhase.speaking ||
        state.phase == AvatarControllerPhase.fallback ||
        state.renderer.conversation == AvatarRendererConversation.playing;
  }

  String? get _avatarStatusLabel {
    if (!_hasLiveAvatarController) {
      return _foreground ? '正在重新连接情景角色' : '情景角色已暂停';
    }
    final state = _avatarController.state;
    if (_reconnecting ||
        (_isRetryableAvatarFailure(state.failure) &&
            _connectionAttempts < _maximumConnectionAttempts)) {
      return '正在重新连接情景角色';
    }
    if (state.phase == AvatarControllerPhase.idle ||
        state.phase == AvatarControllerPhase.preparing) {
      return _readinessExpired ? '画面暂不可用，语音仍可继续' : '正在准备情景角色';
    }
    if (state.phase == AvatarControllerPhase.failed ||
        state.failure != null && !state.canUseAvatar) {
      return '画面暂不可用，语音仍可继续';
    }
    return null;
  }

  void _logConnectionState(AvatarControllerState state) {
    final diagnostic =
        'avatar_connection attempt=$_connectionAttemptSequence '
        'state=${state.renderer.connection.name} '
        'mapped_failure=${state.failure?.name ?? 'none'}';
    if (_lastConnectionDiagnostic == diagnostic) {
      return;
    }
    _lastConnectionDiagnostic = diagnostic;
    developer.log(diagnostic, name: 'speakup.avatar');
  }

  @override
  Widget build(BuildContext context) {
    final avatarController = _avatarController;
    return widget.builder(
      context,
      PracticeAvatarSessionView(
        surfaceBuilder: _hasLiveAvatarController && _hasUsableAvatarSurface
            ? (_) => KeyedSubtree(
                key: ObjectKey(avatarController),
                child: avatarController.buildSurface(key: widget.surfaceKey),
              )
            : null,
        surfaceVisible:
            _hasLiveAvatarController && avatarController.state.canUseAvatar,
        statusLabel: _avatarStatusLabel,
        interruptForUserTurn: _interruptForUserTurn,
        onReplayQuestion: !widget.practiceController.canPlayQuestionAudio
            ? null
            : _replayQuestion,
        replayLoading:
            _replayLoading || widget.practiceController.isQuestionAudioLoading,
        replayPlaying:
            _isAvatarSpeaking ||
            widget.practiceController.isQuestionAudioPlaying,
      ),
    );
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    _connectionOperationId++;
    _cancelConnectionTimers();
    WidgetsBinding.instance.removeObserver(this);
    widget.practiceController.removeListener(_handlePracticeState);
    if (_hasLiveAvatarController) {
      final controller = _avatarController;
      _hasLiveAvatarController = false;
      controller.removeListener(_handleAvatarState);
      unawaited(_bestEffort(controller.close));
    }
    super.dispose();
  }
}

final class _AvatarOrNativeQuestionSpeechSink implements PracticePCMStreamSink {
  _AvatarOrNativeQuestionSpeechSink(
    this._avatarController,
    this._nativeOutput,
    this._readinessExpired,
  );

  final AvatarController _avatarController;
  final PracticePCMStreamSink _nativeOutput;
  final bool _readinessExpired;
  PracticePCMStreamSink? _selectedOutput;
  bool _stopped = false;

  @override
  Future<void> startPCMStream() async {
    if (_selectedOutput != null) {
      throw StateError('A realtime question output was already selected.');
    }
    if (_avatarController.state.canUseAvatar) {
      _selectedOutput = _AvatarQuestionSpeechSink(_avatarController);
      try {
        await _selectedOutput!.startPCMStream();
        if (_stopped) {
          await _selectedOutput!.stopPCMStream();
          throw const AvatarRendererException(
            AvatarRendererFailure.unavailable,
          );
        }
        developer.log(
          'realtime_question_route output=avatar',
          name: 'speakup.avatar',
        );
        return;
      } catch (_) {
        if (_stopped) {
          rethrow;
        }
        await _bestEffort(_selectedOutput!.stopPCMStream);
      }
    }

    final fallbackReason =
        _avatarController.state.failure?.name ??
        (_readinessExpired ? 'readiness_timeout' : 'not_ready');
    _selectedOutput = _nativeOutput;
    await _nativeOutput.startPCMStream();
    if (_stopped) {
      await _nativeOutput.stopPCMStream();
      throw const AvatarRendererException(AvatarRendererFailure.unavailable);
    }
    developer.log(
      'realtime_question_route output=native reason=$fallbackReason',
      name: 'speakup.avatar',
    );
  }

  @override
  Future<void> appendPCM(Uint8List bytes) {
    final output = _selectedOutput;
    if (_stopped || output == null) {
      throw const AvatarRendererException(AvatarRendererFailure.unavailable);
    }
    return output.appendPCM(bytes);
  }

  @override
  Future<void> finishPCMStream() {
    final output = _selectedOutput;
    if (_stopped || output == null) {
      throw const AvatarRendererException(AvatarRendererFailure.unavailable);
    }
    return output.finishPCMStream();
  }

  @override
  Future<void> stopPCMStream() async {
    _stopped = true;
    await _selectedOutput?.stopPCMStream();
  }
}

final class _AvatarQuestionSpeechSink implements PracticePCMStreamSink {
  const _AvatarQuestionSpeechSink(this._controller);

  final AvatarController _controller;

  @override
  Future<void> startPCMStream() => _controller.startPcmStream();

  @override
  Future<void> appendPCM(Uint8List bytes) => _controller.appendPcm(bytes);

  @override
  Future<void> finishPCMStream() => _controller.finishPcmStream();

  @override
  Future<void> stopPCMStream() => _controller.stopPcmStream();
}

bool _canSpeakNow(PracticeController controller) {
  if (controller.isBusy) {
    return false;
  }
  return switch (controller.recordingState) {
    PracticeRecordingState.idle ||
    PracticeRecordingState.awaitingConfirmation ||
    PracticeRecordingState.completed => true,
    PracticeRecordingState.starting ||
    PracticeRecordingState.recording ||
    PracticeRecordingState.transcribing ||
    PracticeRecordingState.submitting => false,
  };
}

Future<void> _bestEffort(Future<void> Function() operation) async {
  try {
    await operation();
  } catch (_) {
    // Lifecycle and user-turn interruption remain best-effort.
  }
}

Future<void> _waitAtMost(
  Future<void> operation,
  Duration timeoutDuration,
) async {
  final timeout = Completer<void>();
  final timer = Timer(timeoutDuration, timeout.complete);
  try {
    await Future.any<void>([operation, timeout.future]);
  } finally {
    timer.cancel();
  }
}
