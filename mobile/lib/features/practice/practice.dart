/// Practice module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/practice/ielts_mock_practice.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/practice/ielts_mock_progress_store.dart';
import 'package:speakup/practice/practice_recordings.dart';
import 'package:speakup/review/ielts_speaking_report_controller.dart';
import 'package:speakup/review/turn_feedback.dart';
import 'package:speakup/review/turn_feedback_controller.dart';
import 'package:speakup/review/turn_feedback_disclosure.dart';

class PracticePage extends StatefulWidget {
  const PracticePage({
    this.previewMode = false,
    this.agentController,
    this.onExitRequested,
    this.ieltsMockProgressStore,
    this.preparationController,
    this.ieltsSpeakingReportController,
    this.speechFeedbackController,
    super.key,
  });

  final bool previewMode;
  final AgentController? agentController;
  final Future<bool> Function()? onExitRequested;
  final IeltsMockProgressStore? ieltsMockProgressStore;
  final PreparationController? preparationController;
  final IeltsSpeakingReportController? ieltsSpeakingReportController;
  final SpeechFeedbackController? speechFeedbackController;

  @override
  State<PracticePage> createState() => _PracticePageState();
}

class _PracticePageState extends State<PracticePage>
    with WidgetsBindingObserver {
  static const _maxReviewExitFrameAttempts = 60;

  final TextEditingController _textAnswerController = TextEditingController();
  final FocusNode _textAnswerFocusNode = FocusNode();
  final ScrollController _messageScrollController = ScrollController();
  final Map<String, String> _feedbackSources = {};
  bool _scheduledReviewExit = false;
  int _reviewExitAttempts = 0;
  Animation<double>? _observedSecondaryAnimation;
  AnimationStatusListener? _reviewRouteStatusListener;
  bool _exitInFlight = false;
  bool _exitApproved = false;
  bool _textAnswerMode = false;
  bool _stickToLatestMessage = true;
  bool _ieltsRouteActive = false;
  int _messageCount = 0;
  String? _lastMessageId;
  PracticeRecordingState? _lastRecordingState;
  int _speechFeedbackRetryCompletionCount = 0;
  Timer? _recordingTicker;
  DateTime? _recordingStartedAt;
  int _recordingSeconds = 0;
  bool _speechFeedbackRebuildScheduled = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _messageScrollController.addListener(_handleMessageScroll);
    widget.agentController?.addListener(_handleState);
    _ieltsRouteActive = _controllerIsIeltsSpeaking;
    _syncSpeechFeedbackSources();
    widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    _captureConversationState();
    _syncRecordingTimer();
    _scheduleReviewExitIfNeeded();
    _scheduleScrollToLatest(animated: false);
  }

  @override
  void didUpdateWidget(covariant PracticePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.speechFeedbackController != widget.speechFeedbackController) {
      oldWidget.speechFeedbackController?.removeListener(
        _handleSpeechFeedbackState,
      );
      _removeSpeechFeedbackSources(oldWidget.speechFeedbackController);
      widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    }
    if (oldWidget.agentController == widget.agentController) {
      _syncSpeechFeedbackSources();
      return;
    }
    oldWidget.agentController?.removeListener(_handleState);
    _resetReviewExit();
    widget.agentController?.addListener(_handleState);
    _ieltsRouteActive = _controllerIsIeltsSpeaking;
    _captureConversationState();
    _syncSpeechFeedbackSources();
    _scheduleReviewExitIfNeeded();
    _scheduleScrollToLatest(animated: false);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    widget.agentController?.removeListener(_handleState);
    widget.speechFeedbackController?.removeListener(_handleSpeechFeedbackState);
    _removeSpeechFeedbackSources(widget.speechFeedbackController);
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
    _ieltsRouteActive = _ieltsRouteActive || _controllerIsIeltsSpeaking;
    final conversationChanged =
        messages.length != _messageCount ||
        lastMessageId != _lastMessageId ||
        recordingState != _lastRecordingState;
    _messageCount = messages.length;
    _lastMessageId = lastMessageId;
    _lastRecordingState = recordingState;
    final retryCompletionCount =
        controller?.speechFeedbackRetryCompletionCount ?? 0;
    final retryCompleted =
        retryCompletionCount > _speechFeedbackRetryCompletionCount;
    _speechFeedbackRetryCompletionCount = retryCompletionCount;
    _syncRecordingTimer();
    _syncSpeechFeedbackSources();
    setState(() {});
    if (retryCompleted) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) {
          return;
        }
        ScaffoldMessenger.of(context)
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('同题复练已提交，不影响场景进度。')));
      });
    }
    if (conversationChanged && shouldScroll) {
      _scheduleScrollToLatest();
    }
    if (_isIeltsSpeaking) {
      _resetReviewExit();
      return;
    }
    if (widget.agentController?.review == null) {
      _resetReviewExit();
      return;
    }
    _scheduleReviewExitIfNeeded();
  }

  void _syncSpeechFeedbackSources() {
    final controller = widget.speechFeedbackController;
    final agentController = widget.agentController;
    if (controller == null || agentController == null) {
      if (controller != null) {
        for (final sourceKey in _feedbackSources.keys) {
          controller.removeSource(sourceKey);
        }
      }
      _feedbackSources.clear();
      return;
    }
    final current = <String, String>{};
    for (final message in agentController.practiceMessages) {
      final statusUrl = message.speechFeedbackStatusUrl;
      if (statusUrl != null) {
        current[_practiceFeedbackSourceKey(agentController, message)] =
            statusUrl;
      }
    }
    for (final entry in _feedbackSources.entries.toList()) {
      if (current[entry.key] != entry.value) {
        controller.removeSource(entry.key);
        _feedbackSources.remove(entry.key);
      }
    }
    for (final entry in current.entries) {
      if (_feedbackSources[entry.key] == entry.value) {
        continue;
      }
      _feedbackSources[entry.key] = entry.value;
      unawaited(controller.load(sourceKey: entry.key, statusUrl: entry.value));
    }
  }

  void _removeSpeechFeedbackSources(SpeechFeedbackController? controller) {
    if (controller != null) {
      for (final sourceKey in _feedbackSources.keys) {
        controller.removeSource(sourceKey);
      }
    }
    _feedbackSources.clear();
  }

  void _handleSpeechFeedbackState() {
    if (_speechFeedbackRebuildScheduled) {
      return;
    }
    _speechFeedbackRebuildScheduled = true;
    scheduleMicrotask(() {
      _speechFeedbackRebuildScheduled = false;
      if (mounted) {
        setState(() {});
      }
    });
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
    _speechFeedbackRetryCompletionCount =
        controller?.speechFeedbackRetryCompletionCount ?? 0;
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
    if (_isIeltsSpeaking ||
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

  void _startSpeechFeedbackRetry(SpeechFeedbackItem item) {
    final controller = widget.agentController;
    if (controller == null) {
      return;
    }
    _textAnswerFocusNode.unfocus();
    if (_textAnswerMode) {
      setState(() => _textAnswerMode = false);
    }
    _stickToLatestMessage = true;
    _scheduleScrollToLatest();
    unawaited(controller.startSpeechFeedbackRetry(item));
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
    if (controller != null && _isIeltsSpeaking) {
      return IeltsSpeakingMockPage(
        controller: controller,
        onExitRequested: widget.onExitRequested,
        progressStore: widget.ieltsMockProgressStore,
        preparationController: widget.preparationController,
        reportController: widget.ieltsSpeakingReportController,
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
                        speechFeedbackController:
                            widget.speechFeedbackController,
                        onRepractice: _startSpeechFeedbackRetry,
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

  bool get _controllerIsIeltsSpeaking =>
      widget.agentController != null &&
      isIeltsSpeakingSession(widget.agentController!);

  bool get _isIeltsSpeaking => _ieltsRouteActive || _controllerIsIeltsSpeaking;
}

String _practiceFeedbackSourceKey(
  AgentController controller,
  AgentMessage message,
) => 'practice:${controller.practiceSessionId}:${message.id}';

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
    required this.onRepractice,
    this.speechFeedbackController,
  });

  final AgentController controller;
  final ScrollController scrollController;
  final bool previewMode;
  final SpeechFeedbackRepracticeCallback onRepractice;
  final SpeechFeedbackController? speechFeedbackController;

  @override
  Widget build(BuildContext context) {
    final messages = controller.practiceMessages;
    final state = controller.recordingState;
    final terminal =
        state == PracticeRecordingState.reviewFailed ||
        state == PracticeRecordingState.completed;
    final showThinking =
        (state == PracticeRecordingState.submitting &&
            !controller.isSpeechFeedbackRetryActive) ||
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
            if (_feedbackProjection(message) case final projection?) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              Align(
                alignment: Alignment.centerRight,
                child: FractionallySizedBox(
                  widthFactor: 0.78,
                  child: SpeechFeedbackDisclosure(
                    key: ValueKey(
                      'practice-speech-feedback-${projection.sourceKey}',
                    ),
                    projection: projection,
                    onRetry: projection.canRetry
                        ? () => unawaited(
                            speechFeedbackController!.retry(
                              projection.sourceKey,
                            ),
                          )
                        : null,
                    onRepractice: controller.canStartSpeechFeedbackRetry
                        ? onRepractice
                        : null,
                  ),
                ),
              ),
            ],
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

  SpeechFeedbackProjection? _feedbackProjection(AgentMessage message) {
    if (message.speechFeedbackStatusUrl == null ||
        speechFeedbackController == null) {
      return null;
    }
    return speechFeedbackController!.projectionFor(
      _practiceFeedbackSourceKey(controller, message),
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
  Future<void> _sendVoice() async {
    await widget.controller.finishRecordingGesture();
    if (!mounted ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    await widget.controller.confirmTranscript();
  }

  Future<void> _convertToText() async {
    await widget.controller.finishRecordingGesture();
    if (!mounted ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final transcript = widget.controller.transcript?.trim() ?? '';
    widget.controller.rerecord();
    widget.textController.value = TextEditingValue(
      text: transcript,
      selection: TextSelection.collapsed(offset: transcript.length),
    );
    if (!widget.textMode) {
      widget.onToggleTextMode();
    }
  }

  Future<void> _cancel() => widget.controller.cancelRecording();

  @override
  Widget build(BuildContext context) {
    final state = widget.controller.recordingState;
    final capturePhase = switch (state) {
      PracticeRecordingState.idle => VoiceCapturePhase.idle,
      PracticeRecordingState.starting => VoiceCapturePhase.starting,
      PracticeRecordingState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    final captureEnabled =
        !widget.textMode &&
        !widget.controller.hasPendingPracticeAudio &&
        (state == PracticeRecordingState.idle ||
            state == PracticeRecordingState.starting ||
            state == PracticeRecordingState.recording);
    return VoiceCaptureControl(
      phase: capturePhase,
      enabled: captureEnabled,
      onStart: widget.controller.startRecording,
      onSendVoice: _sendVoice,
      onConvertToText: _convertToText,
      onCancel: _cancel,
      builder: (context, capture) {
        final panel = switch (state) {
          PracticeRecordingState.idle =>
            widget.controller.hasPendingPracticeAudio
                ? _PendingPracticeAudioPanel(controller: widget.controller)
                : _IdleAnswerPanel(
                    textController: widget.textController,
                    textFocusNode: widget.textFocusNode,
                    onSubmitText: widget.onSubmitText,
                    textMode: widget.textMode,
                    onToggleTextMode: widget.onToggleTextMode,
                    capture: capture,
                  ),
          PracticeRecordingState.starting ||
          PracticeRecordingState.recording => Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              VoiceCaptureIntentTargets(
                capture: capture,
                elapsed: Duration(seconds: widget.recordingSeconds),
                keyPrefix: 'practice',
              ),
              const SizedBox(height: 10),
              capture.wrapTarget(
                key: const Key('practice-record'),
                semanticsLabel: capture.tapMode
                    ? '发送语音回答'
                    : '录音中，左滑取消，右滑转文字，松开发送',
                child: _ActiveRecordingPanel(
                  preparing: state == PracticeRecordingState.starting,
                  releaseIntent: capture.releaseIntent,
                  recordingSeconds: widget.recordingSeconds,
                  tapMode: capture.tapMode,
                ),
              ),
            ],
          ),
          PracticeRecordingState.transcribing => const _WorkingState(
            label: '正在识别英文回答…',
          ),
          PracticeRecordingState.awaitingConfirmation =>
            _TranscriptConfirmation(controller: widget.controller),
          PracticeRecordingState.submitting => _WorkingState(
            label: widget.controller.isSpeechFeedbackRetryActive
                ? '正在提交同题复练…'
                : '回答已发送，正在准备下一题…',
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
              child: panel,
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
    required this.releaseIntent,
    required this.recordingSeconds,
    required this.tapMode,
  });

  final bool preparing;
  final VoiceCaptureReleaseIntent releaseIntent;
  final int recordingSeconds;
  final bool tapMode;

  @override
  Widget build(BuildContext context) {
    final minutes = (recordingSeconds ~/ 60).toString().padLeft(2, '0');
    final seconds = (recordingSeconds % 60).toString().padLeft(2, '0');
    final cancelArmed = releaseIntent == VoiceCaptureReleaseIntent.cancel;
    final convertArmed =
        releaseIntent == VoiceCaptureReleaseIntent.convertToText;
    final accent = cancelArmed ? SpeakUpDesign.error : SpeakUpDesign.primary;
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
        border: Border.all(color: accent),
      ),
      child: Row(
        children: [
          Icon(
            cancelArmed
                ? Icons.delete_outline_rounded
                : convertArmed
                ? Icons.text_fields_rounded
                : Icons.graphic_eq_rounded,
            color: accent,
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
                      : convertArmed
                      ? '松开转成文字'
                      : preparing
                      ? '正在打开麦克风…'
                      : tapMode
                      ? '点击发送语音'
                      : '松开发送语音',
                  style: SpeakUpDesign.cardTitle.copyWith(color: accent),
                ),
                const SizedBox(height: 2),
                Text(
                  cancelArmed
                      ? '录音不会保存'
                      : convertArmed
                      ? '识别后可编辑再发送'
                      : tapMode
                      ? '也可点击上方取消或转文字 · $minutes:$seconds'
                      : '左滑取消 · 右滑转文字 · $minutes:$seconds',
                  style: SpeakUpDesign.meta,
                ),
              ],
            ),
          ),
          if (!cancelArmed && !convertArmed)
            Icon(
              tapMode ? Icons.send_rounded : Icons.keyboard_arrow_down_rounded,
              color: accent,
            ),
        ],
      ),
    );
  }
}

class _PendingPracticeAudioPanel extends StatelessWidget {
  const _PendingPracticeAudioPanel({required this.controller});

  final AgentController controller;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('practice-pending-audio'),
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text('这段录音已保留', style: SpeakUpDesign.cardTitle),
        const SizedBox(height: 4),
        const Text('刚才没有识别成功，可以重试转文字，或删除后重新录音。', style: SpeakUpDesign.body),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                key: const Key('practice-delete-pending-audio'),
                onPressed: controller.discardPendingPracticeAudio,
                child: const Text('删除录音'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                key: const Key('practice-retry-transcription'),
                onPressed: controller.retryPracticeTranscription,
                child: const Text('重试转文字'),
              ),
            ),
          ],
        ),
      ],
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
