import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/practice/immersive_roleplay.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/review/interview_report_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

typedef AvatarControllerFactory = AvatarController Function();

/// Owns one avatar connection for one immersive practice route.
///
/// The product shell remains vendor-neutral: this coordinator only knows the
/// AvatarController boundary, while the composition root chooses Spatius (or a
/// future provider).
class ImmersiveRoleplaySession extends StatefulWidget {
  const ImmersiveRoleplaySession({
    required this.agentController,
    required this.avatarControllerFactory,
    this.interviewReportController,
    this.speechFeedbackController,
    this.onExitRequested,
    this.onContinueWithAgent,
    super.key,
  });

  final AgentController agentController;
  final AvatarControllerFactory avatarControllerFactory;
  final InterviewReportController? interviewReportController;
  final SpeechFeedbackController? speechFeedbackController;
  final Future<bool> Function()? onExitRequested;
  final Future<bool> Function()? onContinueWithAgent;

  @override
  State<ImmersiveRoleplaySession> createState() =>
      _ImmersiveRoleplaySessionState();
}

class _ImmersiveRoleplaySessionState extends State<ImmersiveRoleplaySession>
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
  bool _disposed = false;
  int _connectionAttempts = 0;
  int _connectionOperationId = 0;
  int _generation = 0;
  Future<void> _lifecycleTail = Future<void>.value();
  Future<void> _controllerReplacementTail = Future<void>.value();

  @override
  void initState() {
    super.initState();
    _avatarController = widget.avatarControllerFactory();
    _observedQuestionId = widget.agentController.questionId;
    widget.agentController.addListener(_handleAgentState);
    _avatarController.addListener(_handleAvatarState);
    WidgetsBinding.instance.addObserver(this);
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    _foreground = lifecycle == null || lifecycle == AppLifecycleState.resumed;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_foreground) {
        _scheduleSync();
      } else {
        _queueLifecycleReconciliation();
      }
    });
  }

  @override
  void didUpdateWidget(covariant ImmersiveRoleplaySession oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.agentController == widget.agentController) {
      return;
    }
    oldWidget.agentController.removeListener(_handleAgentState);
    widget.agentController.addListener(_handleAgentState);
    _observedQuestionId = widget.agentController.questionId;
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
      _autoHandledQuestionId = widget.agentController.questionId;
    }
    _queueLifecycleReconciliation();
  }

  void _queueLifecycleReconciliation() {
    final previous = _lifecycleTail;
    final operation = previous
        .catchError((_) {})
        .then((_) => _reconcileLifecycle());
    _lifecycleTail = operation;
    unawaited(operation);
  }

  Future<void> _reconcileLifecycle() async {
    if (_disposed) {
      return;
    }
    if (!_foreground || !_hasLiveAvatarController) {
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
      if (resetConnectionAttempts) {
        _connectionAttempts = 0;
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

  void _handleAgentState() {
    final questionId = widget.agentController.questionId;
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
    if (_avatarController.state.canUseAvatar) {
      _connectionAttempts = 0;
      _readinessTimer?.cancel();
      _readinessTimer = null;
    }
    final sessionId = widget.agentController.practiceSessionId;
    if (sessionId != null &&
        !_connectionInFlight &&
        _isRetryableAvatarFailure(_avatarController.state.failure)) {
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
    final sessionId = widget.agentController.practiceSessionId;
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
    final question = widget.agentController.currentQuestion;
    if (question == null ||
        _speechInFlight ||
        question.id == _autoHandledQuestionId ||
        !_canSpeakNow(widget.agentController)) {
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
      _reconnectTimer?.cancel();
      _reconnectTimer = null;
      if (controller.state.canUseAvatar) {
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
        _readinessExpired = true;
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
        _readinessExpired = true;
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
        widget.agentController.practiceSessionId == sessionId;
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
        _reconnectTimer != null ||
        _connectionAttempts >= _maximumConnectionAttempts ||
        widget.agentController.practiceSessionId != sessionId) {
      return;
    }
    final delay = Duration(milliseconds: 500 * (_connectionAttempts + 1));
    _reconnectTimer = Timer(delay, () async {
      _reconnectTimer = null;
      if (_disposed ||
          !_foreground ||
          widget.agentController.practiceSessionId != sessionId) {
        return;
      }
      await _replaceAvatarController(resetConnectionAttempts: false);
      if (_disposed ||
          !_foreground ||
          !_hasLiveAvatarController ||
          widget.agentController.practiceSessionId != sessionId) {
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
    final mediaClient = widget.agentController.mediaClient;
    final speechPath = question.speechPath;
    if (_speechInFlight || mediaClient == null || speechPath == null) {
      return;
    }
    final sessionId = widget.agentController.practiceSessionId;
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
      await widget.agentController.stopPracticeAudio(notify: false);
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
      // The roleplay remains usable through text/recording when media or the
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

  bool _speechFenceMatches({
    required int generation,
    required String sessionId,
    required String questionId,
    required String speechPath,
    required AvatarController avatarController,
  }) {
    final current = widget.agentController.currentQuestion;
    return !_disposed &&
        _foreground &&
        _hasLiveAvatarController &&
        identical(_avatarController, avatarController) &&
        generation == _generation &&
        widget.agentController.practiceSessionId == sessionId &&
        current?.id == questionId &&
        current?.speechPath == speechPath;
  }

  Future<void> _replayQuestion() async {
    if (_replayLoading || !_canSpeakNow(widget.agentController)) {
      return;
    }
    if (_isAvatarSpeaking) {
      await _interruptForUserTurn();
      return;
    }
    final question = widget.agentController.currentQuestion;
    if (question != null) {
      await _speakQuestion(question, replay: true);
    }
  }

  Future<void> _interruptForUserTurn() async {
    _generation++;
    final operations = <Future<void>>[
      _bestEffort(
        () => widget.agentController.stopPracticeAudio(notify: false),
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

  @override
  Widget build(BuildContext context) {
    return ImmersiveRoleplayPage(
      agentController: widget.agentController,
      avatarSurfaceBuilder: (_) => _hasLiveAvatarController
          ? _avatarController.buildSurface(
              key: const Key('immersive-avatar-surface'),
            )
          : const SizedBox.expand(key: Key('immersive-avatar-surface')),
      avatarStatusLabel: _avatarStatusLabel,
      onBeforeStartRecording: _interruptForUserTurn,
      onBeforeSubmitText: _interruptForUserTurn,
      onReplayQuestion:
          widget.agentController.currentQuestion?.speechPath == null
          ? null
          : _replayQuestion,
      interviewReportController: widget.interviewReportController,
      speechFeedbackController: widget.speechFeedbackController,
      replayLoading: _replayLoading,
      replayPlaying: _isAvatarSpeaking,
      onExitRequested: widget.onExitRequested,
      onContinueWithAgent: widget.onContinueWithAgent,
    );
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    _connectionOperationId++;
    _cancelConnectionTimers();
    WidgetsBinding.instance.removeObserver(this);
    widget.agentController.removeListener(_handleAgentState);
    if (_hasLiveAvatarController) {
      final controller = _avatarController;
      _hasLiveAvatarController = false;
      controller.removeListener(_handleAvatarState);
      unawaited(_bestEffort(controller.close));
    }
    super.dispose();
  }
}

bool _canSpeakNow(AgentController controller) {
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
