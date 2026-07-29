/// Practice module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/practice/practice_recordings.dart';

class PracticePage extends StatefulWidget {
  const PracticePage({
    this.previewMode = false,
    this.agentController,
    this.onExitRequested,
    super.key,
  });

  final bool previewMode;
  final AgentController? agentController;
  final Future<bool> Function()? onExitRequested;

  @override
  State<PracticePage> createState() => _PracticePageState();
}

class _PracticePageState extends State<PracticePage> {
  static const _maxReviewExitFrameAttempts = 60;

  final TextEditingController _textAnswerController = TextEditingController();
  bool _scheduledReviewExit = false;
  int _reviewExitAttempts = 0;
  Animation<double>? _observedSecondaryAnimation;
  AnimationStatusListener? _reviewRouteStatusListener;
  String? _expandedHintQuestionId;
  bool _exitInFlight = false;
  bool _exitApproved = false;
  bool _textAnswerMode = false;
  Timer? _recordingTicker;
  DateTime? _recordingStartedAt;
  int _recordingSeconds = 0;

  @override
  void initState() {
    super.initState();
    widget.agentController?.addListener(_handleState);
    _syncRecordingTimer();
    _scheduleReviewExitIfNeeded();
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
    _scheduleReviewExitIfNeeded();
  }

  @override
  void dispose() {
    widget.agentController?.removeListener(_handleState);
    _clearReviewRouteWait();
    _recordingTicker?.cancel();
    _textAnswerController.dispose();
    unawaited(widget.agentController?.stopPracticeAudio(notify: false));
    super.dispose();
  }

  void _handleState() {
    if (!mounted) {
      return;
    }
    _syncRecordingTimer();
    setState(() {});
    if (widget.agentController?.review == null) {
      _resetReviewExit();
      return;
    }
    _scheduleReviewExitIfNeeded();
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
    if (widget.agentController?.review == null ||
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

  void _toggleHint(String questionId) {
    setState(() {
      _expandedHintQuestionId = _expandedHintQuestionId == questionId
          ? null
          : questionId;
    });
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
      setState(() => _textAnswerMode = false);
    }
  }

  void _showPracticeHistory(AgentController controller) {
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
            const Text('本轮记录', style: SpeakUpDesign.sectionTitle),
            if (controller.recordings.isNotEmpty) ...[
              const SizedBox(height: 16),
              PracticeRecordingsCard(controller: controller),
            ],
            if (controller.messages.any(
              (message) => message.id != controller.questionId,
            )) ...[
              const SizedBox(height: 20),
              _ConversationHistory(
                messages: controller.messages
                    .where((message) => message.id != controller.questionId)
                    .toList(growable: false),
                expandedHintQuestionId: null,
                onToggleHint: (_) {},
              ),
            ],
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
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(scene.title),
                    Text(
                      '第 ${(controller.completedTurns + 1).clamp(1, controller.turnLimit)} 题，共 ${controller.turnLimit} 题',
                      style: SpeakUpDesign.meta,
                    ),
                  ],
                ),
          actions:
              controller == null ||
                  (controller.recordings.isEmpty &&
                      !controller.messages.any(
                        (message) => message.id != controller.questionId,
                      ))
              ? null
              : [
                  IconButton(
                    key: const Key('practice-open-history'),
                    tooltip: '本轮记录',
                    onPressed:
                        controller.recordingState == PracticeRecordingState.idle
                        ? () => _showPracticeHistory(controller)
                        : null,
                    icon: const Icon(Icons.more_horiz_rounded),
                  ),
                ],
        ),
        body: SafeArea(
          bottom: false,
          child: controller == null || scene == null
              ? const _NoScene()
              : Column(
                  children: [
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 8, 20, 0),
                      child: _TurnProgress(
                        completedTurns: controller.completedTurns,
                        turnLimit: controller.turnLimit,
                      ),
                    ),
                    Expanded(
                      child: SingleChildScrollView(
                        padding: const EdgeInsets.fromLTRB(20, 28, 20, 32),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            _CurrentQuestion(
                              controller: controller,
                              expandedHintQuestionId: _expandedHintQuestionId,
                              onToggleHint: _toggleHint,
                            ),
                            if (controller.errorMessage
                                case final message?) ...[
                              const SizedBox(height: 16),
                              Text(
                                message,
                                key: const Key('practice-error-message'),
                                style: const TextStyle(
                                  color: SpeakUpDesign.error,
                                ),
                              ),
                            ],
                            if (controller.mediaErrorMessage
                                case final message?) ...[
                              const SizedBox(height: 16),
                              Text(
                                message,
                                key: const Key('practice-media-error-message'),
                                style: const TextStyle(
                                  color: SpeakUpDesign.error,
                                ),
                              ),
                            ],
                            if (widget.previewMode) ...[
                              const SizedBox(height: 20),
                              const Text(
                                '当前页面仅供显式 Fake 预览，不代表生产语音服务已经接入。',
                                textAlign: TextAlign.center,
                                style: SpeakUpDesign.meta,
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
        ),
        bottomNavigationBar: controller == null || scene == null
            ? null
            : _RecordingPanel(
                controller: controller,
                textController: _textAnswerController,
                onSubmitText: _submitTextAnswer,
                textMode: _textAnswerMode,
                onToggleTextMode: () =>
                    setState(() => _textAnswerMode = !_textAnswerMode),
                recordingSeconds: _recordingSeconds,
              ),
      ),
    );
  }
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

class _TurnProgress extends StatelessWidget {
  const _TurnProgress({required this.completedTurns, required this.turnLimit});

  final int completedTurns;
  final int turnLimit;

  @override
  Widget build(BuildContext context) {
    return Row(
      key: const Key('practice-turn-progress'),
      children: [
        for (var index = 0; index < turnLimit; index++) ...[
          Expanded(
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 180),
              height: 7,
              decoration: BoxDecoration(
                color: index < completedTurns
                    ? SpeakUpDesign.primary
                    : SpeakUpDesign.border,
                borderRadius: BorderRadius.circular(8),
              ),
            ),
          ),
          if (index + 1 != turnLimit) const SizedBox(width: 8),
        ],
        const SizedBox(width: 12),
        Text(
          '$completedTurns / $turnLimit',
          key: const Key('practice-turn-count'),
          style: const TextStyle(fontWeight: FontWeight.w700),
        ),
      ],
    );
  }
}

class _ConversationHistory extends StatelessWidget {
  const _ConversationHistory({
    required this.messages,
    required this.expandedHintQuestionId,
    required this.onToggleHint,
  });

  final List<AgentMessage> messages;
  final String? expandedHintQuestionId;
  final ValueChanged<String> onToggleHint;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('practice-conversation-history'),
      children: [
        for (final message in messages) ...[
          Align(
            alignment: message.role == AgentMessageRole.user
                ? Alignment.centerRight
                : Alignment.centerLeft,
            child: Container(
              key: Key('practice-history-message-${message.id}'),
              constraints: const BoxConstraints(maxWidth: 560),
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: message.role == AgentMessageRole.user
                    ? SpeakUpDesign.primary
                    : SpeakUpDesign.surface,
                borderRadius: BorderRadius.circular(
                  SpeakUpDesign.radiusControl,
                ),
                border: Border.all(color: SpeakUpDesign.border),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    message.text,
                    style: TextStyle(
                      color: message.role == AgentMessageRole.user
                          ? Colors.white
                          : SpeakUpDesign.ink,
                      height: 1.45,
                    ),
                  ),
                  if (message.role == AgentMessageRole.assistant) ...[
                    const SizedBox(height: 8),
                    TextButton.icon(
                      key: Key('practice-history-hint-${message.id}'),
                      onPressed: () => onToggleHint(message.id),
                      icon: const Icon(Icons.lightbulb_outline_rounded),
                      label: Text(
                        expandedHintQuestionId == message.id ? '收起提示' : '提示',
                      ),
                    ),
                    if (expandedHintQuestionId == message.id)
                      const _AnswerHint(),
                  ],
                ],
              ),
            ),
          ),
          const SizedBox(height: 10),
        ],
      ],
    );
  }
}

class _CurrentQuestion extends StatelessWidget {
  const _CurrentQuestion({
    required this.controller,
    required this.expandedHintQuestionId,
    required this.onToggleHint,
  });

  final AgentController controller;
  final String? expandedHintQuestionId;
  final ValueChanged<String> onToggleHint;

  @override
  Widget build(BuildContext context) {
    final question = controller.messages.reversed
        .where((message) => message.role == AgentMessageRole.assistant)
        .firstOrNull;
    final hintExpanded =
        question != null && expandedHintQuestionId == question.id;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Row(
          children: [
            CircleAvatar(
              radius: 22,
              backgroundColor: SpeakUpDesign.primaryMuted,
              foregroundColor: SpeakUpDesign.primary,
              child: Icon(Icons.person_outline_rounded),
            ),
            SizedBox(width: 12),
            Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('AI 面试官', style: SpeakUpDesign.cardTitle),
                SizedBox(height: 2),
                Text('请用英文回答', style: SpeakUpDesign.meta),
              ],
            ),
          ],
        ),
        const SizedBox(height: 24),
        Text(
          question?.text ?? '准备好后开始第一轮。',
          key: const Key('practice-current-question'),
          style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 24, height: 1.4),
        ),
        const SizedBox(height: 18),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            if (controller.canPlayQuestionAudio)
              TextButton.icon(
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
                label: Text(
                  controller.isQuestionAudioPlaying ? '停止播放' : '重听问题',
                ),
              ),
            if (question != null)
              TextButton.icon(
                key: Key('practice-hint-${question.id}'),
                onPressed: () => onToggleHint(question.id),
                icon: Icon(
                  hintExpanded
                      ? Icons.lightbulb_rounded
                      : Icons.lightbulb_outline_rounded,
                  size: 19,
                ),
                label: Text(hintExpanded ? '收起思路' : '回答思路'),
              ),
          ],
        ),
        if (hintExpanded) ...[const SizedBox(height: 14), const _AnswerHint()],
      ],
    );
  }
}

class _AnswerHint extends StatelessWidget {
  const _AnswerHint();

  @override
  Widget build(BuildContext context) {
    return const DecoratedBox(
      key: Key('practice-answer-hint'),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.all(
          Radius.circular(SpeakUpDesign.radiusControl),
        ),
      ),
      child: Padding(
        padding: EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('回答框架', style: SpeakUpDesign.cardTitle),
            SizedBox(height: 10),
            Text('1. 先直接回答你的核心观点', style: SpeakUpDesign.body),
            SizedBox(height: 4),
            Text('2. 用一个具体经历或事实支持', style: SpeakUpDesign.body),
            SizedBox(height: 4),
            Text('3. 回到岗位匹配与结果', style: SpeakUpDesign.body),
          ],
        ),
      ),
    );
  }
}

class _RecordingPanel extends StatelessWidget {
  const _RecordingPanel({
    required this.controller,
    required this.textController,
    required this.onSubmitText,
    required this.textMode,
    required this.onToggleTextMode,
    required this.recordingSeconds,
  });

  final AgentController controller;
  final TextEditingController textController;
  final VoidCallback onSubmitText;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final int recordingSeconds;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SpeakUpDesign.surface,
      shape: const Border(top: BorderSide(color: SpeakUpDesign.border)),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 14, 20, 12),
          child: switch (controller.recordingState) {
            PracticeRecordingState.idle => _IdleAnswerPanel(
              textController: textController,
              onSubmitText: onSubmitText,
              onStartRecording: controller.startRecording,
              textMode: textMode,
              onToggleTextMode: onToggleTextMode,
            ),
            PracticeRecordingState.starting => const _WorkingState(
              label: '正在准备麦克风…',
            ),
            PracticeRecordingState.recording => _RecordAction(
              onPressed: controller.stopRecording,
              recordingSeconds: recordingSeconds,
            ),
            PracticeRecordingState.transcribing => const _WorkingState(
              label: '正在整理你的回答…',
            ),
            PracticeRecordingState.awaitingConfirmation =>
              _TranscriptConfirmation(controller: controller),
            PracticeRecordingState.submitting => const _WorkingState(
              label: '回答已提交，正在准备下一题…',
            ),
            PracticeRecordingState.reviewFailed => _ReviewRetry(
              onPressed: controller.retryReview,
            ),
            PracticeRecordingState.completed => const _WorkingState(
              label: '复盘已生成，正在打开',
            ),
          },
        ),
      ),
    );
  }
}

class _IdleAnswerPanel extends StatelessWidget {
  const _IdleAnswerPanel({
    required this.textController,
    required this.onSubmitText,
    required this.onStartRecording,
    required this.textMode,
    required this.onToggleTextMode,
  });

  final TextEditingController textController;
  final VoidCallback onSubmitText;
  final VoidCallback onStartRecording;
  final bool textMode;
  final VoidCallback onToggleTextMode;

  @override
  Widget build(BuildContext context) {
    if (!textMode) {
      return Row(
        children: [
          Expanded(
            child: FilledButton.icon(
              key: const Key('practice-record'),
              onPressed: onStartRecording,
              icon: const Icon(Icons.mic_rounded),
              label: const Text('开始回答'),
              style: FilledButton.styleFrom(
                minimumSize: const Size.fromHeight(56),
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
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Expanded(child: Text('键盘回答', style: SpeakUpDesign.cardTitle)),
            IconButton(
              tooltip: '返回语音回答',
              onPressed: onToggleTextMode,
              icon: const Icon(Icons.close_rounded),
            ),
          ],
        ),
        const SizedBox(height: 8),
        TextField(
          key: const Key('practice-text-answer'),
          controller: textController,
          minLines: 2,
          maxLines: 4,
          maxLength: 8000,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(
            hintText: 'Type your answer in English…',
            counterText: '',
          ),
        ),
        const SizedBox(height: 10),
        FilledButton.icon(
          key: const Key('practice-submit-text'),
          onPressed: onSubmitText,
          icon: const Icon(Icons.send_rounded),
          label: const Text('发送文字回答'),
          style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
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

class _RecordAction extends StatelessWidget {
  const _RecordAction({
    required this.onPressed,
    required this.recordingSeconds,
  });

  final VoidCallback onPressed;
  final int recordingSeconds;

  @override
  Widget build(BuildContext context) {
    final minutes = (recordingSeconds ~/ 60).toString().padLeft(2, '0');
    final seconds = (recordingSeconds % 60).toString().padLeft(2, '0');
    return Row(
      children: [
        const SizedBox(
          width: 12,
          height: 28,
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: SpeakUpDesign.error,
              shape: BoxShape.circle,
            ),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('正在听你说', style: SpeakUpDesign.cardTitle),
              Text('$minutes:$seconds', style: SpeakUpDesign.meta),
            ],
          ),
        ),
        FilledButton.icon(
          key: const Key('practice-stop-recording'),
          onPressed: onPressed,
          icon: const Icon(Icons.stop_rounded),
          label: const Text('完成回答'),
          style: FilledButton.styleFrom(
            backgroundColor: SpeakUpDesign.error,
            minimumSize: const Size(0, 52),
          ),
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
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('检查一下识别结果', style: SpeakUpDesign.cardTitle),
        const SizedBox(height: 10),
        Text(
          controller.transcript ?? '',
          key: const Key('practice-transcript'),
          style: const TextStyle(height: 1.5),
        ),
        const SizedBox(height: 18),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                key: const Key('practice-rerecord'),
                onPressed: controller.rerecord,
                child: const Text('重新录音'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                key: const Key('practice-confirm-turn'),
                onPressed: controller.confirmTranscript,
                child: const Text('提交回答'),
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
