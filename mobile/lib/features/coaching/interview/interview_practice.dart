/// Interview practice UI boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/conversation_bubble_surface.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/practice/question_tip_sheet.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_message_bubble.dart';
import 'package:speakup/features/coaching/practice/practice_recordings.dart';
import 'package:speakup/features/coaching/practice/practice_completion_sheet.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

final class InterviewPracticeCompletion {
  const InterviewPracticeCompletion({
    required this.practiceSessionId,
    required this.title,
    required this.speechFeedbackSourceKeys,
  });

  final String practiceSessionId;
  final String title;
  final List<String> speechFeedbackSourceKeys;
}

typedef OpenInterviewPracticeReport =
    Future<CompletedPracticeRouteResult?> Function(
      InterviewPracticeCompletion completion,
    );

class InterviewPracticePage extends StatefulWidget {
  const InterviewPracticePage({
    this.previewMode = false,
    this.practiceController,
    this.onExitRequested,
    this.onReturnToConversation,
    this.onOpenInterviewReport,
    this.speechFeedbackController,
    this.practicePromptSpeaker,
    super.key,
  });

  final bool previewMode;
  final PracticeController? practiceController;
  final Future<bool> Function()? onExitRequested;
  final Future<bool> Function()? onReturnToConversation;
  final OpenInterviewPracticeReport? onOpenInterviewReport;
  final SpeechFeedbackController? speechFeedbackController;
  final PracticePromptSpeaker? practicePromptSpeaker;

  @override
  State<InterviewPracticePage> createState() => _InterviewPracticePageState();
}

class _InterviewPracticePageState extends State<InterviewPracticePage>
    with WidgetsBindingObserver {
  final TextEditingController _textAnswerController = TextEditingController();
  final FocusNode _textAnswerFocusNode = FocusNode();
  final ScrollController _messageScrollController = ScrollController();
  final Map<String, String> _feedbackSources = {};
  bool _exitInFlight = false;
  bool _exitApproved = false;
  bool _textAnswerMode = false;
  bool _stickToLatestMessage = true;
  int _messageCount = 0;
  String? _lastMessageId;
  PracticeRecordingState? _lastRecordingState;
  int _speechFeedbackRetryCompletionCount = 0;
  Timer? _recordingTicker;
  DateTime? _recordingStartedAt;
  int _recordingSeconds = 0;
  bool _speechFeedbackRebuildScheduled = false;
  bool _interviewReportRouteActive = false;
  bool _completionInFlight = false;
  PracticePromptSpeaker? _ownedTipSpeaker;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _messageScrollController.addListener(_handleMessageScroll);
    widget.practiceController?.addListener(_handleState);
    _syncSpeechFeedbackSources();
    widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    _captureConversationState();
    _syncRecordingTimer();
    _scheduleScrollToLatest(animated: false);
  }

  @override
  void didUpdateWidget(covariant InterviewPracticePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.speechFeedbackController != widget.speechFeedbackController) {
      oldWidget.speechFeedbackController?.removeListener(
        _handleSpeechFeedbackState,
      );
      _removeSpeechFeedbackSources(oldWidget.speechFeedbackController);
      widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    }
    if (oldWidget.practiceController == widget.practiceController) {
      _syncSpeechFeedbackSources();
      return;
    }
    oldWidget.practiceController?.removeListener(_handleState);
    widget.practiceController?.addListener(_handleState);
    _captureConversationState();
    _syncSpeechFeedbackSources();
    _scheduleScrollToLatest(animated: false);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    widget.practiceController?.removeListener(_handleState);
    widget.speechFeedbackController?.removeListener(_handleSpeechFeedbackState);
    _removeSpeechFeedbackSources(widget.speechFeedbackController);
    _recordingTicker?.cancel();
    _messageScrollController
      ..removeListener(_handleMessageScroll)
      ..dispose();
    _textAnswerController.dispose();
    _textAnswerFocusNode.dispose();
    unawaited(widget.practiceController?.stopPracticeAudio(notify: false));
    if (_ownedTipSpeaker case final speaker?) {
      unawaited(speaker.dispose());
    }
    super.dispose();
  }

  Future<void> _showQuestionTip() async {
    final controller = widget.practiceController;
    if (controller == null) {
      return;
    }
    final tip = await controller.requestQuestionTip();
    if (!mounted ||
        tip == null ||
        controller.currentQuestion?.id != tip.questionId) {
      return;
    }
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) => QuestionTipSheet(
        content: tip.content,
        translation: tip.translation,
        onSpeak: () async {
          final speaker =
              widget.practicePromptSpeaker ??
              (_ownedTipSpeaker ??= SystemPracticePromptSpeaker());
          await speaker.speak(tip.content);
        },
      ),
    );
    try {
      await (widget.practicePromptSpeaker ?? _ownedTipSpeaker)?.stop();
    } on Object {
      // Closing the reference sheet must not be blocked by a platform TTS error.
    }
  }

  void _handleState() {
    if (!mounted) {
      return;
    }
    final shouldScroll =
        _stickToLatestMessage || !_messageScrollController.hasClients;
    final controller = widget.practiceController;
    final messages = controller?.practiceMessages ?? const <PracticeMessage>[];
    final lastMessageId = messages.lastOrNull?.id;
    final recordingState = controller?.recordingState;
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
  }

  Future<void> _openInterviewReport() async {
    final openReport = widget.onOpenInterviewReport;
    final practiceController = widget.practiceController;
    final sessionId = practiceController?.practiceSessionId;
    if (openReport == null ||
        practiceController == null ||
        sessionId == null ||
        practiceController.recordingState != PracticeRecordingState.completed ||
        _interviewReportRouteActive) {
      return;
    }
    _interviewReportRouteActive = true;
    try {
      final result = await openReport(
        InterviewPracticeCompletion(
          practiceSessionId: sessionId,
          title: '${practiceController.scene?.name ?? '面试'} · 复盘',
          speechFeedbackSourceKeys: List<String>.unmodifiable(
            _feedbackSources.keys,
          ),
        ),
      );
      if (mounted && result != null) {
        Navigator.of(context).pop(result);
      }
    } finally {
      _interviewReportRouteActive = false;
    }
  }

  Future<void> _requestUserControlledCompletion() async {
    final controller = widget.practiceController;
    if (!mounted ||
        controller == null ||
        _completionInFlight ||
        !controller.canCompleteActivePractice) {
      return;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('结束面试练习？'),
        content: const Text('结束后将保存本次回答并生成面试复盘。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('继续练习'),
          ),
          FilledButton(
            key: const Key('interview-confirm-completion'),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('结束练习'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) {
      return;
    }
    setState(() => _completionInFlight = true);
    final completed = await controller.completeActivePractice();
    if (!mounted) {
      return;
    }
    setState(() => _completionInFlight = false);
    if (!completed) {
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('面试暂时无法结束，请稍后重试。')));
    }
  }

  Future<void> _leaveCompletedInterview() async {
    if (!mounted || _completionInFlight) {
      return;
    }
    setState(() => _completionInFlight = true);
    final callback = widget.onReturnToConversation;
    var parked = callback == null;
    try {
      if (callback != null) {
        parked = await callback();
      }
    } on Object {
      parked = false;
    }
    if (!mounted) {
      return;
    }
    if (!parked) {
      setState(() => _completionInFlight = false);
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('当前练习正在保存，请稍后再返回。')));
      return;
    }
    _exitApproved = true;
    Navigator.of(context).pop(CompletedPracticeRouteResult.returnToTraining);
  }

  void _syncSpeechFeedbackSources() {
    final controller = widget.speechFeedbackController;
    final practiceController = widget.practiceController;
    if (controller == null || practiceController == null) {
      if (controller != null) {
        for (final sourceKey in _feedbackSources.keys) {
          controller.removeSource(sourceKey);
        }
      }
      _feedbackSources.clear();
      return;
    }
    final current = <String, String>{};
    for (final message in practiceController.practiceMessages) {
      final statusUrl = message.speechFeedbackStatusUrl;
      if (statusUrl != null) {
        current[_practiceFeedbackSourceKey(practiceController, message)] =
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
    final controller = widget.practiceController;
    final messages = controller?.practiceMessages ?? const <PracticeMessage>[];
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
        widget.practiceController?.recordingState ==
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
    if (widget.practiceController?.recordingState !=
        PracticeRecordingState.idle) {
      _textAnswerMode = false;
    }
  }

  Future<void> _submitTextAnswer() async {
    final controller = widget.practiceController;
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
    final controller = widget.practiceController;
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

  void _showPracticeRecordings(PracticeController controller) {
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
    final controller = widget.practiceController;
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
              : Text(scene.name),
          actions: controller == null
              ? null
              : [
                  if (controller.recordings.isNotEmpty)
                    IconButton(
                      key: const Key('practice-open-history'),
                      tooltip: '本轮录音',
                      onPressed:
                          controller.recordingState ==
                              PracticeRecordingState.idle
                          ? () => _showPracticeRecordings(controller)
                          : null,
                      icon: const Icon(Icons.more_horiz_rounded),
                    ),
                  if (controller.completionMode ==
                          PracticeCompletionMode.userControlled &&
                      controller.recordingState !=
                          PracticeRecordingState.completed)
                    TextButton(
                      key: const Key('interview-complete-practice'),
                      onPressed:
                          controller.canCompleteActivePractice &&
                              !_completionInFlight
                          ? _requestUserControlledCompletion
                          : null,
                      child: _completionInFlight
                          ? const SizedBox.square(
                              dimension: 18,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Text('结束练习'),
                    ),
                ],
        ),
        body: SafeArea(
          child: controller == null || scene == null
              ? const _NoScene()
              : Stack(
                  children: [
                    Column(
                      children: [
                        _InterviewProgress(controller: controller),
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
                          onOpenReport: _openInterviewReport,
                          onShowTip: _showQuestionTip,
                        ),
                      ],
                    ),
                    if (controller.recordingState ==
                        PracticeRecordingState.completed)
                      Positioned.fill(
                        child: PracticeCompletionOverlay(
                          keyPrefix: 'interview-completion',
                          title: '面试练习已完成',
                          message: '${controller.completedTurns} 道回答已保存',
                          primaryLabel: '查看复盘报告',
                          secondaryLabel: '返回面试列表',
                          onPrimary: widget.onOpenInterviewReport == null
                              ? null
                              : _openInterviewReport,
                          onSecondary: _completionInFlight
                              ? null
                              : _leaveCompletedInterview,
                          primaryLoading: _interviewReportRouteActive,
                        ),
                      ),
                  ],
                ),
        ),
      ),
    );
  }
}

String _practiceFeedbackSourceKey(
  PracticeController controller,
  PracticeMessage message,
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

class _InterviewProgress extends StatelessWidget {
  const _InterviewProgress({required this.controller});

  final PracticeController controller;

  @override
  Widget build(BuildContext context) {
    final total = controller.turnLimit;
    final current = total < 1
        ? 1
        : (controller.completedTurns +
                  (controller.currentQuestion?.isFollowUp == true ? 0 : 1))
              .clamp(1, total);
    final state = switch (controller.recordingState) {
      PracticeRecordingState.starting ||
      PracticeRecordingState.recording => '正在作答',
      PracticeRecordingState.transcribing ||
      PracticeRecordingState.awaitingConfirmation => '正在识别',
      PracticeRecordingState.submitting =>
        controller.isFinalSubmission ? '正在提交最后一题' : '正在生成下一题',
      PracticeRecordingState.completed => '面试已完成',
      PracticeRecordingState.idle => '等待作答',
    };
    return Container(
      key: const Key('interview-question-progress'),
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 10),
      decoration: const BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        border: Border(bottom: BorderSide(color: SpeakUpDesign.border)),
      ),
      child: Text(
        total < 1 ? state : '第 $current/$total 题 · $state',
        style: SpeakUpDesign.meta.copyWith(
          color: SpeakUpDesign.primary,
          fontWeight: FontWeight.w700,
        ),
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

  final PracticeController controller;
  final ScrollController scrollController;
  final bool previewMode;
  final ValueChanged<SpeechFeedbackItem> onRepractice;
  final SpeechFeedbackController? speechFeedbackController;

  @override
  Widget build(BuildContext context) {
    final messages = controller.practiceMessages;
    final state = controller.recordingState;
    final terminal = state == PracticeRecordingState.completed;
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
            PracticeMessageBubble(
              key: ValueKey('practice-${message.role.name}-${message.id}'),
              message: message,
              feedbackProjection: _feedbackProjection(message),
              onFeedbackRepractice: controller.canStartSpeechFeedbackRetry
                  ? onRepractice
                  : null,
              actions:
                  message.role == PracticeMessageRole.assistant &&
                      message.id == controller.questionId &&
                      controller.canPlayQuestionAudio
                  ? _QuestionAudioAction(controller: controller)
                  : null,
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

  SpeechFeedbackProjection? _feedbackProjection(PracticeMessage message) {
    if (message.speechFeedbackStatusUrl == null ||
        speechFeedbackController == null) {
      return null;
    }
    return speechFeedbackController!.projectionFor(
      _practiceFeedbackSourceKey(controller, message),
    );
  }
}

class _SceneAIThinkingBubble extends StatelessWidget {
  const _SceneAIThinkingBubble({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return ConversationBubbleSurface(
      isUser: false,
      bubbleKey: const Key('practice-ai-thinking'),
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
    );
  }
}

class _QuestionAudioAction extends StatelessWidget {
  const _QuestionAudioAction({required this.controller});

  final PracticeController controller;

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
    required this.onOpenReport,
    required this.onShowTip,
  });

  final PracticeController controller;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final VoidCallback onSubmitText;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final int recordingSeconds;
  final VoidCallback onOpenReport;
  final VoidCallback onShowTip;

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
    await WidgetsBinding.instance.endOfFrame;
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
                ? PracticePendingAudioComposer(
                    keyPrefix: 'practice',
                    onDelete: widget.controller.discardPendingPracticeAudio,
                    onRetry: widget.controller.retryPracticeTranscription,
                  )
                : _IdleAnswerPanel(
                    controller: widget.controller,
                    textController: widget.textController,
                    textFocusNode: widget.textFocusNode,
                    onSubmitText: widget.onSubmitText,
                    textMode: widget.textMode,
                    onToggleTextMode: widget.onToggleTextMode,
                    capture: capture,
                    onShowTip: widget.onShowTip,
                  ),
          PracticeRecordingState.starting ||
          PracticeRecordingState.recording => PracticeRecordingComposer(
            capture: capture,
            phase: capturePhase,
            keyPrefix: 'practice',
            elapsed: Duration(seconds: widget.recordingSeconds),
            transcript: widget.controller.transcript ?? '',
          ),
          PracticeRecordingState.transcribing => PracticeLoadingComposer(
            label: '正在识别英文回答…',
          ),
          PracticeRecordingState.awaitingConfirmation =>
            PracticeTranscriptComposer(
              transcript: widget.controller.transcript ?? '',
              keyPrefix: 'practice',
              onRerecord: widget.controller.rerecord,
              onConfirm: widget.controller.confirmTranscript,
              confirmLabel: '发送回答',
            ),
          PracticeRecordingState.submitting => PracticeLoadingComposer(
            label: widget.controller.isSpeechFeedbackRetryActive
                ? '正在提交同题复练…'
                : widget.controller.isFinalSubmission
                ? '正在提交最后一轮回答，完成后将生成报告…'
                : '回答已发送，Agent 正在回复…',
          ),
          PracticeRecordingState.completed => PracticeComposerAction(
            label: '本次练习已完成，可以查看完整报告。',
            actionLabel: '查看报告',
            onPressed: widget.onOpenReport,
            containerKey: const Key('practice-completed-actions'),
            actionKey: const Key('practice-open-report'),
          ),
        };
        return PracticeComposerSurface(child: panel);
      },
    );
  }
}

class _IdleAnswerPanel extends StatelessWidget {
  const _IdleAnswerPanel({
    required this.controller,
    required this.textController,
    required this.textFocusNode,
    required this.onSubmitText,
    required this.textMode,
    required this.onToggleTextMode,
    required this.capture,
    required this.onShowTip,
  });

  final PracticeController controller;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final VoidCallback onSubmitText;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final VoiceCaptureView capture;
  final VoidCallback onShowTip;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Align(
          alignment: Alignment.centerRight,
          child: TextButton.icon(
            key: const Key('practice-question-tip'),
            onPressed: controller.canRequestQuestionTip ? onShowTip : null,
            icon: controller.isQuestionTipLoading
                ? const SizedBox.square(
                    dimension: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.lightbulb_outline_rounded, size: 19),
            label: Text(controller.isQuestionTipLoading ? '正在生成' : 'Tips'),
          ),
        ),
        if (controller.questionTipErrorMessage case final message?)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Text(
              message,
              key: const Key('practice-question-tip-error'),
              textAlign: TextAlign.right,
              style: const TextStyle(color: SpeakUpDesign.error, fontSize: 12),
            ),
          ),
        PracticeIdleComposer(
          capture: capture,
          textController: textController,
          textFocusNode: textFocusNode,
          textMode: textMode,
          onToggleTextMode: onToggleTextMode,
          onSubmitText: onSubmitText,
          keyPrefix: 'practice',
        ),
      ],
    );
  }
}
