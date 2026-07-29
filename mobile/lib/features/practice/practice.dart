/// Practice module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/practice/ielts_mock_practice.dart';
import 'package:speakup/practice/ielts_mock_progress_store.dart';
import 'package:speakup/practice/practice_recordings.dart';

class PracticePage extends StatefulWidget {
  const PracticePage({
    this.previewMode = false,
    this.agentController,
    this.onExitRequested,
    this.ieltsMockProgressStore,
    super.key,
  });

  final bool previewMode;
  final AgentController? agentController;
  final Future<bool> Function()? onExitRequested;
  final IeltsMockProgressStore? ieltsMockProgressStore;

  @override
  State<PracticePage> createState() => _PracticePageState();
}

class _PracticePageState extends State<PracticePage>
    with WidgetsBindingObserver {
  static const _maxReviewExitFrameAttempts = 60;

  final TextEditingController _textAnswerController = TextEditingController();
  final FocusNode _textAnswerFocusNode = FocusNode();
  final ScrollController _messageScrollController = ScrollController();
  bool _scheduledReviewExit = false;
  int _reviewExitAttempts = 0;
  Animation<double>? _observedSecondaryAnimation;
  AnimationStatusListener? _reviewRouteStatusListener;
  bool _exitInFlight = false;
  bool _exitApproved = false;
  bool _textAnswerMode = false;
  bool _stickToLatestMessage = true;
  int _messageCount = 0;
  String? _lastMessageId;
  PracticeRecordingState? _lastRecordingState;
  Timer? _recordingTicker;
  DateTime? _recordingStartedAt;
  int _recordingSeconds = 0;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _messageScrollController.addListener(_handleMessageScroll);
    widget.agentController?.addListener(_handleState);
    _captureConversationState();
    _syncRecordingTimer();
    _scheduleReviewExitIfNeeded();
    _scheduleScrollToLatest(animated: false);
  }

  @override
  void didUpdateWidget(covariant PracticePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.agentController == widget.agentController) {
      return;
    }
    oldWidget.agentController?.removeListener(_handleState);
    _resetReviewExit();
    widget.agentController?.addListener(_handleState);
    _captureConversationState();
    _scheduleReviewExitIfNeeded();
    _scheduleScrollToLatest(animated: false);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    widget.agentController?.removeListener(_handleState);
    _clearReviewRouteWait();
    _recordingTicker?.cancel();
    _messageScrollController
      ..removeListener(_handleMessageScroll)
      ..dispose();
    _textAnswerController.dispose();
    _textAnswerFocusNode.dispose();
    unawaited(widget.agentController?.stopPracticeAudio(notify: false));
    super.dispose();
  }

  void _handleState() {
    if (!mounted) {
      return;
    }
    final shouldScroll =
        _stickToLatestMessage || !_messageScrollController.hasClients;
    final controller = widget.agentController;
    final messages = controller?.practiceMessages ?? const <AgentMessage>[];
    final lastMessageId = messages.lastOrNull?.id;
    final recordingState = controller?.recordingState;
    final conversationChanged =
        messages.length != _messageCount ||
        lastMessageId != _lastMessageId ||
        recordingState != _lastRecordingState;
    _messageCount = messages.length;
    _lastMessageId = lastMessageId;
    _lastRecordingState = recordingState;
    _syncRecordingTimer();
    setState(() {});
    if (conversationChanged && shouldScroll) {
      _scheduleScrollToLatest();
    }
    if (_isIeltsSpeakingFullMock) {
      _resetReviewExit();
      return;
    }
    if (widget.agentController?.review == null) {
      _resetReviewExit();
      return;
    }
    _scheduleReviewExitIfNeeded();
  }

  @override
  void didChangeMetrics() {
    if (_stickToLatestMessage) {
      _scheduleScrollToLatest();
    }
  }

  void _captureConversationState() {
    final controller = widget.agentController;
    final messages = controller?.practiceMessages ?? const <AgentMessage>[];
    _messageCount = messages.length;
    _lastMessageId = messages.lastOrNull?.id;
    _lastRecordingState = controller?.recordingState;
  }

  void _handleMessageScroll() {
    if (!_messageScrollController.hasClients) {
      return;
    }
    final position = _messageScrollController.position;
    _stickToLatestMessage = position.maxScrollExtent - position.pixels <= 72;
  }

  void _scheduleScrollToLatest({bool animated = true}) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_messageScrollController.hasClients) {
        return;
      }
      final target = _messageScrollController.position.maxScrollExtent;
      if (animated) {
        unawaited(
          _messageScrollController.animateTo(
            target,
            duration: const Duration(milliseconds: 180),
            curve: Curves.easeOut,
          ),
        );
      } else {
        _messageScrollController.jumpTo(target);
      }
    });
  }

  void _syncRecordingTimer() {
    final recording =
        widget.agentController?.recordingState ==
        PracticeRecordingState.recording;
    if (recording) {
      if (_recordingTicker != null) {
        return;
      }
      _recordingStartedAt = DateTime.now();
      _recordingSeconds = 0;
      _recordingTicker = Timer.periodic(const Duration(seconds: 1), (_) {
        if (!mounted || _recordingStartedAt == null) {
          return;
        }
        setState(() {
          _recordingSeconds = DateTime.now()
              .difference(_recordingStartedAt!)
              .inSeconds;
        });
      });
      return;
    }
    _recordingTicker?.cancel();
    _recordingTicker = null;
    _recordingStartedAt = null;
    _recordingSeconds = 0;
    if (widget.agentController?.recordingState != PracticeRecordingState.idle) {
      _textAnswerMode = false;
    }
  }

  void _scheduleReviewExitIfNeeded() {
    if (_isIeltsSpeakingFullMock ||
        widget.agentController?.review == null ||
        _scheduledReviewExit ||
        _reviewExitAttempts >= _maxReviewExitFrameAttempts) {
      return;
    }
    _scheduledReviewExit = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      unawaited(_attemptReviewExit());
    });
    WidgetsBinding.instance.scheduleFrame();
  }

  Future<void> _attemptReviewExit() async {
    if (!mounted) {
      return;
    }
    if (widget.agentController?.review == null) {
      _resetReviewExit();
      return;
    }

    final route = ModalRoute.of(context);
    if (route == null) {
      _scheduledReviewExit = false;
      return;
    }
    final navigator = Navigator.of(context);
    if (!route.isCurrent) {
      _waitForRouteToBecomeCurrent(route);
      return;
    }
    if (!navigator.canPop()) {
      // A deep-linked or standalone Practice route has no safe destination.
      // Keep it visible rather than removing the app's only route.
      _scheduledReviewExit = false;
      _reviewExitAttempts = _maxReviewExitFrameAttempts;
      return;
    }

    _reviewExitAttempts++;
    final disposition = route.popDisposition;
    late final bool handled;
    try {
      handled = await navigator.maybePop();
    } catch (_) {
      if (mounted) {
        _retryReviewExitOnNextFrame();
      }
      return;
    }
    if (!mounted) {
      return;
    }
    if (widget.agentController?.review == null) {
      _resetReviewExit();
      return;
    }
    final popStarted = handled && disposition == RoutePopDisposition.pop;
    if (popStarted) {
      await route.completed;
    } else if (route.isCurrent) {
      _retryReviewExitOnNextFrame();
    }
  }

  void _retryReviewExitOnNextFrame() {
    _scheduledReviewExit = false;
    _scheduleReviewExitIfNeeded();
  }

  void _waitForRouteToBecomeCurrent(ModalRoute<dynamic> route) {
    _scheduledReviewExit = false;
    _clearReviewRouteWait();
    final animation = route.secondaryAnimation;
    if (animation == null) {
      return;
    }
    late final AnimationStatusListener listener;
    listener = (_) {
      if (!mounted || widget.agentController?.review == null) {
        _clearReviewRouteWait();
        return;
      }
      if (!route.isCurrent) {
        return;
      }
      _clearReviewRouteWait();
      _scheduleReviewExitIfNeeded();
    };
    _observedSecondaryAnimation = animation;
    _reviewRouteStatusListener = listener;
    animation.addStatusListener(listener);
    if (route.isCurrent) {
      _clearReviewRouteWait();
      _scheduleReviewExitIfNeeded();
    }
  }

  void _clearReviewRouteWait() {
    final animation = _observedSecondaryAnimation;
    final listener = _reviewRouteStatusListener;
    if (animation != null && listener != null) {
      animation.removeStatusListener(listener);
    }
    _observedSecondaryAnimation = null;
    _reviewRouteStatusListener = null;
  }

  void _resetReviewExit() {
    _clearReviewRouteWait();
    _scheduledReviewExit = false;
    _reviewExitAttempts = 0;
  }

  Future<void> _submitTextAnswer() async {
    final controller = widget.agentController;
    if (controller == null) {
      return;
    }
    final submitted = await controller.submitPracticeText(
      _textAnswerController.text,
    );
    if (submitted && mounted) {
      _textAnswerController.clear();
      _textAnswerFocusNode.unfocus();
      setState(() => _textAnswerMode = false);
    }
  }

  void _toggleTextAnswerMode() {
    final textMode = !_textAnswerMode;
    setState(() => _textAnswerMode = textMode);
    if (!textMode) {
      _textAnswerFocusNode.unfocus();
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _textAnswerFocusNode.requestFocus();
      }
    });
  }

  void _showPracticeRecordings(AgentController controller) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (context) => DraggableScrollableSheet(
        expand: false,
        initialChildSize: 0.72,
        minChildSize: 0.4,
        maxChildSize: 0.92,
        builder: (context, scrollController) => ListView(
          controller: scrollController,
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 32),
          children: [
            const Text('本轮录音', style: SpeakUpDesign.sectionTitle),
            const SizedBox(height: 16),
            PracticeRecordingsCard(controller: controller),
          ],
        ),
      ),
    );
  }

  Future<void> _requestExit() async {
    if (!mounted || _exitInFlight || _exitApproved) {
      return;
    }
    final callback = widget.onExitRequested;
    if (callback == null) {
      _exitApproved = true;
    } else {
      _exitInFlight = true;
      bool approved;
      try {
        approved = await callback();
      } on Object {
        approved = false;
      }
      if (!mounted) {
        return;
      }
      _exitInFlight = false;
      if (!approved) {
        ScaffoldMessenger.of(context)
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('当前练习正在保存，请稍后再返回。')));
        setState(() {});
        return;
      }
      _exitApproved = true;
    }
    if (mounted) {
      setState(() {});
      await WidgetsBinding.instance.endOfFrame;
    }
    if (mounted) {
      await Navigator.of(context).maybePop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.agentController;
    if (controller != null && isIeltsSpeakingFullMockSession(controller)) {
      return IeltsSpeakingMockPage(
        controller: controller,
        onExitRequested: widget.onExitRequested,
        progressStore: widget.ieltsMockProgressStore,
      );
    }
    final scene = controller?.scene;
    return PopScope<void>(
      canPop: widget.onExitRequested == null || _exitApproved,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          unawaited(_requestExit());
        }
      },
      child: Scaffold(
        key: const Key('practice-page'),
        appBar: AppBar(
          title: scene == null || controller == null
              ? const Text('练习')
              : Text(scene.title),
          actions: controller == null || controller.recordings.isEmpty
              ? null
              : [
                  IconButton(
                    key: const Key('practice-open-history'),
                    tooltip: '本轮录音',
                    onPressed:
                        controller.recordingState == PracticeRecordingState.idle
                        ? () => _showPracticeRecordings(controller)
                        : null,
                    icon: const Icon(Icons.more_horiz_rounded),
                  ),
                ],
        ),
        body: SafeArea(
          child: controller == null || scene == null
              ? const _NoScene()
              : Column(
                  children: [
                    Expanded(
                      child: _SceneConversationMessageList(
                        controller: controller,
                        scrollController: _messageScrollController,
                        previewMode: widget.previewMode,
                      ),
                    ),
                    _RecordingPanel(
                      controller: controller,
                      textController: _textAnswerController,
                      textFocusNode: _textAnswerFocusNode,
                      onSubmitText: _submitTextAnswer,
                      textMode: _textAnswerMode,
                      onToggleTextMode: _toggleTextAnswerMode,
                      recordingSeconds: _recordingSeconds,
                    ),
                  ],
                ),
        ),
      ),
    );
  }

  bool get _isIeltsSpeakingFullMock =>
      widget.agentController != null &&
      isIeltsSpeakingFullMockSession(widget.agentController!);
}

class _NoScene extends StatelessWidget {
  const _NoScene();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: Padding(
        padding: EdgeInsets.all(24),
        child: Text('请先从“场景”选择本次练习内容。', textAlign: TextAlign.center),
      ),
    );
  }
}

class _SceneConversationMessageList extends StatelessWidget {
  const _SceneConversationMessageList({
    required this.controller,
    required this.scrollController,
    required this.previewMode,
  });

  final AgentController controller;
  final ScrollController scrollController;
  final bool previewMode;

  @override
  Widget build(BuildContext context) {
    final messages = controller.practiceMessages;
    final state = controller.recordingState;
    final terminal =
        state == PracticeRecordingState.reviewFailed ||
        state == PracticeRecordingState.completed;
    final showThinking =
        state == PracticeRecordingState.submitting ||
        (messages.isEmpty && !terminal);

    return SingleChildScrollView(
      key: const Key('practice-message-list'),
      controller: scrollController,
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      padding: const EdgeInsets.fromLTRB(16, 20, 16, 20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (final message in messages) ...[
            if (message.role == AgentMessageRole.assistant)
              _SceneAIMessageBubble(
                key: ValueKey('practice-ai-${message.id}'),
                message: message,
                roleName: 'AI 教练',
                actions:
                    message.id == controller.questionId &&
                        controller.canPlayQuestionAudio
                    ? _QuestionAudioAction(controller: controller)
                    : null,
              )
            else
              _SceneUserMessageBubble(
                key: ValueKey('practice-user-${message.id}'),
                message: message,
              ),
            const SizedBox(height: 14),
          ],
          if (showThinking) ...[
            _SceneAIThinkingBubble(
              label: messages.isEmpty ? '正在准备第一轮…' : 'AI 正在思考…',
            ),
            const SizedBox(height: 14),
          ],
          if (controller.errorMessage case final message?) ...[
            Text(
              message,
              key: const Key('practice-error-message'),
              style: const TextStyle(color: SpeakUpDesign.error),
            ),
            const SizedBox(height: 12),
          ],
          if (controller.mediaErrorMessage case final message?) ...[
            Text(
              message,
              key: const Key('practice-media-error-message'),
              style: const TextStyle(color: SpeakUpDesign.error),
            ),
            const SizedBox(height: 12),
          ],
          if (previewMode)
            const Text(
              '当前页面仅供显式 Fake 预览，不代表生产语音服务已经接入。',
              textAlign: TextAlign.center,
              style: SpeakUpDesign.meta,
            ),
        ],
      ),
    );
  }
}

class _SceneAIMessageBubble extends StatelessWidget {
  const _SceneAIMessageBubble({
    required this.message,
    required this.roleName,
    this.actions,
    super.key,
  });

  final AgentMessage message;
  final String roleName;
  final Widget? actions;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const CircleAvatar(
            radius: 17,
            backgroundColor: SpeakUpDesign.primaryMuted,
            foregroundColor: SpeakUpDesign.primary,
            child: Icon(Icons.person_outline_rounded, size: 20),
          ),
          const SizedBox(width: 10),
          Container(
            key: Key('practice-ai-message-${message.id}'),
            constraints: BoxConstraints(maxWidth: constraints.maxWidth * 0.76),
            padding: const EdgeInsets.fromLTRB(14, 11, 14, 12),
            decoration: BoxDecoration(
              color: SpeakUpDesign.surface,
              borderRadius: const BorderRadius.only(
                topRight: Radius.circular(SpeakUpDesign.radiusControl),
                bottomLeft: Radius.circular(SpeakUpDesign.radiusControl),
                bottomRight: Radius.circular(SpeakUpDesign.radiusControl),
              ),
              border: Border.all(color: SpeakUpDesign.border),
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  roleName,
                  style: SpeakUpDesign.meta.copyWith(
                    color: SpeakUpDesign.primary,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 5),
                Text(
                  message.text,
                  style: SpeakUpDesign.body.copyWith(height: 1.45),
                ),
                if (actions != null) ...[const SizedBox(height: 8), actions!],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _SceneUserMessageBubble extends StatelessWidget {
  const _SceneUserMessageBubble({required this.message, super.key});

  final AgentMessage message;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => Align(
        alignment: Alignment.centerRight,
        child: Container(
          key: Key('practice-user-message-${message.id}'),
          constraints: BoxConstraints(maxWidth: constraints.maxWidth * 0.78),
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          decoration: const BoxDecoration(
            color: SpeakUpDesign.primary,
            borderRadius: BorderRadius.only(
              topLeft: Radius.circular(SpeakUpDesign.radiusControl),
              bottomLeft: Radius.circular(SpeakUpDesign.radiusControl),
              bottomRight: Radius.circular(SpeakUpDesign.radiusControl),
            ),
          ),
          child: Text(
            message.text,
            style: SpeakUpDesign.body.copyWith(
              color: Colors.white,
              height: 1.45,
            ),
          ),
        ),
      ),
    );
  }
}

class _SceneAIThinkingBubble extends StatelessWidget {
  const _SceneAIThinkingBubble({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: Row(
        key: const Key('practice-ai-thinking'),
        mainAxisSize: MainAxisSize.min,
        children: [
          const CircleAvatar(
            radius: 17,
            backgroundColor: SpeakUpDesign.primaryMuted,
            foregroundColor: SpeakUpDesign.primary,
            child: Icon(Icons.person_outline_rounded, size: 20),
          ),
          const SizedBox(width: 10),
          Flexible(
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: SpeakUpDesign.surfaceMuted,
                borderRadius: BorderRadius.circular(
                  SpeakUpDesign.radiusControl,
                ),
              ),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 12,
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const SizedBox.square(
                      dimension: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                    const SizedBox(width: 10),
                    Flexible(child: Text(label, style: SpeakUpDesign.meta)),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _QuestionAudioAction extends StatelessWidget {
  const _QuestionAudioAction({required this.controller});

  final AgentController controller;

  @override
  Widget build(BuildContext context) {
    return TextButton.icon(
      key: const Key('practice-question-audio'),
      onPressed: controller.canUsePracticeAudio
          ? controller.toggleQuestionAudio
          : null,
      icon: controller.isQuestionAudioLoading
          ? const SizedBox.square(
              dimension: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(
              controller.isQuestionAudioPlaying
                  ? Icons.stop_circle_outlined
                  : Icons.volume_up_outlined,
              size: 19,
            ),
      label: Text(controller.isQuestionAudioPlaying ? '停止播放' : '重听问题'),
    );
  }
}

class _RecordingPanel extends StatefulWidget {
  const _RecordingPanel({
    required this.controller,
    required this.textController,
    required this.textFocusNode,
    required this.onSubmitText,
    required this.textMode,
    required this.onToggleTextMode,
    required this.recordingSeconds,
  });

  final AgentController controller;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final VoidCallback onSubmitText;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final int recordingSeconds;

  @override
  State<_RecordingPanel> createState() => _RecordingPanelState();
}

class _RecordingPanelState extends State<_RecordingPanel> {
  @override
  Widget build(BuildContext context) {
    final state = widget.controller.recordingState;
    final phase = switch (state) {
      PracticeRecordingState.idle => VoiceCapturePhase.idle,
      PracticeRecordingState.starting => VoiceCapturePhase.starting,
      PracticeRecordingState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    return VoiceCaptureControl(
      phase: phase,
      enabled: !widget.textMode,
      onStart: widget.controller.startRecording,
      onFinish: widget.controller.finishRecordingGesture,
      onCancel: widget.controller.cancelRecording,
      builder: (context, capture) {
        final tapRecordingActive =
            capture.tapMode &&
            (state == PracticeRecordingState.starting ||
                state == PracticeRecordingState.recording);
        final panel = switch (state) {
          PracticeRecordingState.idle => _IdleAnswerPanel(
            textController: widget.textController,
            textFocusNode: widget.textFocusNode,
            onSubmitText: widget.onSubmitText,
            textMode: widget.textMode,
            onToggleTextMode: widget.onToggleTextMode,
            capture: capture,
          ),
          PracticeRecordingState.starting ||
          PracticeRecordingState.recording => capture.wrapTarget(
            key: const Key('practice-record'),
            semanticsLabel: state == PracticeRecordingState.starting
                ? '正在打开麦克风'
                : tapRecordingActive
                ? '结束录音并自动转写'
                : '正在录音，上滑取消，松开完成录音',
            child: _ActiveRecordingPanel(
              preparing: state == PracticeRecordingState.starting,
              cancelArmed: capture.cancelArmed,
              recordingSeconds: widget.recordingSeconds,
              tapMode: tapRecordingActive,
            ),
          ),
          PracticeRecordingState.transcribing => const _WorkingState(
            label: '正在识别英文回答…',
          ),
          PracticeRecordingState.awaitingConfirmation =>
            _TranscriptConfirmation(controller: widget.controller),
          PracticeRecordingState.submitting => const _WorkingState(
            label: '回答已发送，正在准备下一题…',
          ),
          PracticeRecordingState.reviewFailed => _ReviewRetry(
            onPressed: widget.controller.retryReview,
          ),
          PracticeRecordingState.completed => const _WorkingState(
            label: '复盘已生成，正在打开',
          ),
        };
        return Material(
          color: SpeakUpDesign.surface,
          shape: const Border(top: BorderSide(color: SpeakUpDesign.border)),
          child: SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(20, 14, 20, 12),
              child: tapRecordingActive
                  ? Row(
                      children: [
                        Expanded(child: panel),
                        const SizedBox(width: 10),
                        IconButton.outlined(
                          key: const Key('practice-cancel-tap-recording'),
                          tooltip: '取消录音',
                          onPressed: capture.cancelTapCapture,
                          icon: const Icon(Icons.close_rounded),
                          style: IconButton.styleFrom(
                            minimumSize: const Size.square(56),
                          ),
                        ),
                      ],
                    )
                  : panel,
            ),
          ),
        );
      },
    );
  }
}

class _IdleAnswerPanel extends StatelessWidget {
  const _IdleAnswerPanel({
    required this.textController,
    required this.textFocusNode,
    required this.onSubmitText,
    required this.textMode,
    required this.onToggleTextMode,
    required this.capture,
  });

  final TextEditingController textController;
  final FocusNode textFocusNode;
  final VoidCallback onSubmitText;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final VoiceCaptureView capture;

  @override
  Widget build(BuildContext context) {
    if (!textMode) {
      return Row(
        children: [
          Expanded(
            child: capture.wrapTarget(
              key: const Key('practice-record'),
              semanticsLabel: '点击或按住说话',
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 100),
                height: 56,
                decoration: BoxDecoration(
                  color: capture.pressed
                      ? SpeakUpDesign.primary.withValues(alpha: 0.82)
                      : SpeakUpDesign.primary,
                  borderRadius: BorderRadius.circular(
                    SpeakUpDesign.radiusControl,
                  ),
                ),
                child: const Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Icon(Icons.mic_rounded, color: Colors.white),
                    SizedBox(width: 10),
                    Flexible(
                      child: Text(
                        '点击或按住说话',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          color: Colors.white,
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(width: 10),
          IconButton.outlined(
            key: const Key('practice-open-keyboard'),
            tooltip: '键盘回答',
            onPressed: onToggleTextMode,
            icon: const Icon(Icons.keyboard_alt_outlined),
            style: IconButton.styleFrom(minimumSize: const Size.square(56)),
          ),
        ],
      );
    }
    return Row(
      children: [
        IconButton.outlined(
          key: const Key('practice-return-to-voice'),
          tooltip: '切换到语音回答',
          onPressed: onToggleTextMode,
          icon: const Icon(Icons.mic_none_rounded),
          style: IconButton.styleFrom(minimumSize: const Size.square(52)),
        ),
        const SizedBox(width: 10),
        Expanded(
          child: TextField(
            key: const Key('practice-text-answer'),
            controller: textController,
            focusNode: textFocusNode,
            minLines: 1,
            maxLines: 3,
            maxLength: 8000,
            textCapitalization: TextCapitalization.sentences,
            textInputAction: TextInputAction.newline,
            decoration: const InputDecoration(
              hintText: 'Type your answer…',
              counterText: '',
              contentPadding: EdgeInsets.symmetric(
                horizontal: 14,
                vertical: 13,
              ),
            ),
          ),
        ),
        const SizedBox(width: 10),
        IconButton.filled(
          key: const Key('practice-submit-text'),
          onPressed: onSubmitText,
          tooltip: '发送文字回答',
          icon: const Icon(Icons.arrow_upward_rounded),
          style: IconButton.styleFrom(minimumSize: const Size.square(52)),
        ),
      ],
    );
  }
}

class _ActiveRecordingPanel extends StatelessWidget {
  const _ActiveRecordingPanel({
    required this.preparing,
    required this.cancelArmed,
    required this.recordingSeconds,
    required this.tapMode,
  });

  final bool preparing;
  final bool cancelArmed;
  final int recordingSeconds;
  final bool tapMode;

  @override
  Widget build(BuildContext context) {
    final minutes = (recordingSeconds ~/ 60).toString().padLeft(2, '0');
    final seconds = (recordingSeconds % 60).toString().padLeft(2, '0');
    return AnimatedContainer(
      key: const Key('practice-stop-recording'),
      duration: const Duration(milliseconds: 120),
      constraints: const BoxConstraints(minHeight: 72),
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
      decoration: BoxDecoration(
        color: cancelArmed
            ? SpeakUpDesign.errorMuted
            : SpeakUpDesign.primaryMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        border: Border.all(
          color: cancelArmed ? SpeakUpDesign.error : SpeakUpDesign.primary,
        ),
      ),
      child: Row(
        children: [
          Icon(
            cancelArmed ? Icons.delete_outline_rounded : Icons.mic_rounded,
            color: cancelArmed ? SpeakUpDesign.error : SpeakUpDesign.primary,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  cancelArmed
                      ? '松开取消'
                      : preparing
                      ? '正在打开麦克风…'
                      : tapMode
                      ? '再次点击结束'
                      : '松开完成',
                  style: SpeakUpDesign.cardTitle.copyWith(
                    color: cancelArmed
                        ? SpeakUpDesign.error
                        : SpeakUpDesign.primary,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  cancelArmed
                      ? '录音不会保存'
                      : tapMode
                      ? '点击结束并识别 · $minutes:$seconds'
                      : '上滑取消 · $minutes:$seconds',
                  style: SpeakUpDesign.meta,
                ),
              ],
            ),
          ),
          if (!cancelArmed && !tapMode)
            const Icon(
              Icons.keyboard_arrow_up_rounded,
              color: SpeakUpDesign.primary,
            ),
          if (!cancelArmed && tapMode)
            const Icon(
              Icons.stop_circle_outlined,
              color: SpeakUpDesign.primary,
            ),
        ],
      ),
    );
  }
}

class _ReviewRetry extends StatelessWidget {
  const _ReviewRetry({required this.onPressed});

  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        const Icon(
          Icons.refresh_rounded,
          size: 36,
          color: SpeakUpDesign.primary,
        ),
        const SizedBox(height: 14),
        FilledButton(
          key: const Key('practice-retry-review'),
          onPressed: onPressed,
          style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
          child: const Text('刷新复盘'),
        ),
      ],
    );
  }
}

class _TranscriptConfirmation extends StatelessWidget {
  const _TranscriptConfirmation({required this.controller});

  final AgentController controller;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('识别结果', style: SpeakUpDesign.label),
        const SizedBox(height: 8),
        ConstrainedBox(
          constraints: const BoxConstraints(maxHeight: 96),
          child: SingleChildScrollView(
            child: Text(
              controller.transcript ?? '',
              key: const Key('practice-transcript'),
              style: const TextStyle(height: 1.45),
            ),
          ),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                key: const Key('practice-rerecord'),
                onPressed: controller.rerecord,
                child: const Text('取消'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                key: const Key('practice-confirm-turn'),
                onPressed: controller.confirmTranscript,
                child: const Text('发送回答'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _WorkingState extends StatelessWidget {
  const _WorkingState({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        const SizedBox.square(
          dimension: 22,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
        const SizedBox(width: 12),
        Expanded(child: Text(label, style: SpeakUpDesign.body)),
      ],
    );
  }
}
