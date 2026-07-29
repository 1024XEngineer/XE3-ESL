/// Practice module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
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

  @override
  void initState() {
    super.initState();
    widget.agentController?.addListener(_handleState);
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
    _textAnswerController.dispose();
    unawaited(widget.agentController?.stopPracticeAudio(notify: false));
    super.dispose();
  }

  void _handleState() {
    if (!mounted) {
      return;
    }
    setState(() {});
    if (widget.agentController?.review == null) {
      _resetReviewExit();
      return;
    }
    _scheduleReviewExitIfNeeded();
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
    }
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
        backgroundColor: const Color(0xFFF3F3F0),
        appBar: AppBar(
          title: const Text('按轮练习'),
          backgroundColor: const Color(0xFFF3F3F0),
          surfaceTintColor: Colors.transparent,
        ),
        body: SafeArea(
          child: controller == null || scene == null
              ? const _NoScene()
              : ListView(
                  padding: const EdgeInsets.fromLTRB(20, 16, 20, 32),
                  children: [
                    Text(
                      scene.title,
                      style: const TextStyle(
                        fontSize: 28,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '一问一答，完成 ${controller.turnLimit} 轮有效回答后自动生成复盘。',
                      style: TextStyle(color: Color(0xFF696B73), height: 1.4),
                    ),
                    const SizedBox(height: 22),
                    _TurnProgress(
                      completedTurns: controller.completedTurns,
                      turnLimit: controller.turnLimit,
                    ),
                    const SizedBox(height: 22),
                    _CurrentQuestion(
                      controller: controller,
                      expandedHintQuestionId: _expandedHintQuestionId,
                      onToggleHint: _toggleHint,
                    ),
                    const SizedBox(height: 12),
                    const _CorrectionIntegrationStatus(),
                    if (controller.errorMessage case final message?) ...[
                      const SizedBox(height: 14),
                      Text(
                        message,
                        key: const Key('practice-error-message'),
                        style: const TextStyle(color: Color(0xFF8B2E26)),
                      ),
                    ],
                    if (controller.mediaErrorMessage case final message?) ...[
                      const SizedBox(height: 14),
                      Text(
                        message,
                        key: const Key('practice-media-error-message'),
                        style: const TextStyle(color: Color(0xFF8B2E26)),
                      ),
                    ],
                    const SizedBox(height: 18),
                    _RecordingPanel(
                      controller: controller,
                      textController: _textAnswerController,
                      onSubmitText: _submitTextAnswer,
                    ),
                    if (controller.recordings.isNotEmpty) ...[
                      const SizedBox(height: 18),
                      PracticeRecordingsCard(controller: controller),
                    ],
                    if (controller.messages.any(
                      (message) => message.id != controller.questionId,
                    )) ...[
                      const SizedBox(height: 22),
                      const Text(
                        '本次对话记录',
                        style: TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      const SizedBox(height: 12),
                      _ConversationHistory(
                        messages: controller.messages
                            .where(
                              (message) => message.id != controller.questionId,
                            )
                            .toList(growable: false),
                        expandedHintQuestionId: _expandedHintQuestionId,
                        onToggleHint: _toggleHint,
                      ),
                    ],
                    const SizedBox(height: 18),
                    if (widget.previewMode)
                      const Text(
                        '当前页面仅供显式 Fake 预览，不代表生产语音服务已经接入。',
                        textAlign: TextAlign.center,
                        style: TextStyle(
                          color: Color(0xFF85878E),
                          fontSize: 12,
                        ),
                      ),
                  ],
                ),
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
                    ? const Color(0xFF2B2C30)
                    : const Color(0xFFDCDDD9),
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
                    ? const Color(0xFF303136)
                    : Colors.white,
                borderRadius: BorderRadius.circular(16),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    message.text,
                    style: TextStyle(
                      color: message.role == AgentMessageRole.user
                          ? Colors.white
                          : const Color(0xFF303136),
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
    return Card(
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Expanded(
                  child: Text(
                    '当前问题',
                    style: TextStyle(
                      color: Color(0xFF777983),
                      fontSize: 13,
                      fontWeight: FontWeight.w700,
                    ),
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
                    label: Text(hintExpanded ? '收起提示' : '提示'),
                  ),
                if (controller.canPlayQuestionAudio)
                  IconButton(
                    key: const Key('practice-question-audio'),
                    tooltip: controller.isQuestionAudioPlaying
                        ? '停止播放'
                        : '朗读问题',
                    visualDensity: VisualDensity.compact,
                    onPressed: controller.canUsePracticeAudio
                        ? controller.toggleQuestionAudio
                        : null,
                    icon: controller.isQuestionAudioLoading
                        ? const SizedBox.square(
                            dimension: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : Icon(
                            controller.isQuestionAudioPlaying
                                ? Icons.stop_circle_outlined
                                : Icons.volume_up_outlined,
                          ),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              question?.text ?? '准备好后开始第一轮。',
              key: const Key('practice-current-question'),
              style: const TextStyle(
                fontSize: 17,
                fontWeight: FontWeight.w600,
                height: 1.45,
              ),
            ),
            if (hintExpanded) ...[
              const SizedBox(height: 16),
              const _AnswerHint(),
            ],
          ],
        ),
      ),
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
        color: Color(0xFFF3F3F0),
        borderRadius: BorderRadius.all(Radius.circular(16)),
      ),
      child: Padding(
        padding: EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('参考回答', style: TextStyle(fontWeight: FontWeight.w800)),
            SizedBox(height: 6),
            Text(
              'From my perspective, the core responsibility of this role is '
              'to understand the goal, work closely with the team, and '
              'deliver reliable results.',
              style: TextStyle(color: Color(0xFF5F6168), height: 1.45),
            ),
          ],
        ),
      ),
    );
  }
}

class _CorrectionIntegrationStatus extends StatelessWidget {
  const _CorrectionIntegrationStatus();

  @override
  Widget build(BuildContext context) {
    return const Material(
      key: Key('practice-correction-status'),
      color: Color(0xFFE9E9E5),
      borderRadius: BorderRadius.all(Radius.circular(14)),
      child: Padding(
        padding: EdgeInsets.symmetric(horizontal: 14, vertical: 11),
        child: Row(
          children: [
            Icon(Icons.fact_check_outlined, size: 19),
            SizedBox(width: 9),
            Expanded(
              child: Text(
                '逐轮纠错：当前暂无结果（已预留正式接口）',
                style: TextStyle(fontSize: 13, color: Color(0xFF5F6168)),
              ),
            ),
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
  });

  final AgentController controller;
  final TextEditingController textController;
  final VoidCallback onSubmitText;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: switch (controller.recordingState) {
          PracticeRecordingState.idle => _IdleAnswerPanel(
            textController: textController,
            onSubmitText: onSubmitText,
            onStartRecording: controller.startRecording,
          ),
          PracticeRecordingState.starting => const _WorkingState(
            label: '正在请求麦克风权限',
          ),
          PracticeRecordingState.recording => _RecordAction(
            label: '停止并转写',
            icon: Icons.stop_rounded,
            onPressed: controller.stopRecording,
            emphasized: true,
          ),
          PracticeRecordingState.transcribing => const _WorkingState(
            label: '正在转写这一轮',
          ),
          PracticeRecordingState.awaitingConfirmation =>
            _TranscriptConfirmation(controller: controller),
          PracticeRecordingState.submitting => const _WorkingState(
            label: '正在提交并生成下一步',
          ),
          PracticeRecordingState.reviewFailed => _ReviewRetry(
            onPressed: controller.retryReview,
          ),
          PracticeRecordingState.completed => const _WorkingState(
            label: '复盘已生成，正在打开',
          ),
        },
      ),
    );
  }
}

class _IdleAnswerPanel extends StatelessWidget {
  const _IdleAnswerPanel({
    required this.textController,
    required this.onSubmitText,
    required this.onStartRecording,
  });

  final TextEditingController textController;
  final VoidCallback onSubmitText;
  final VoidCallback onStartRecording;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        OutlinedButton.icon(
          key: const Key('practice-record'),
          onPressed: onStartRecording,
          icon: const Icon(Icons.mic_none_rounded),
          label: const Text('使用语音回答'),
          style: OutlinedButton.styleFrom(
            minimumSize: const Size.fromHeight(48),
          ),
        ),
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 12),
          child: Row(
            children: [
              Expanded(child: Divider()),
              Padding(
                padding: EdgeInsets.symmetric(horizontal: 12),
                child: Text(
                  '或输入文字',
                  style: TextStyle(color: Color(0xFF777981)),
                ),
              ),
              Expanded(child: Divider()),
            ],
          ),
        ),
        const Text(
          '用英文回答',
          style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 10),
        TextField(
          key: const Key('practice-text-answer'),
          controller: textController,
          minLines: 2,
          maxLines: 4,
          maxLength: 8000,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(
            hintText: 'Type your answer in English…',
            border: OutlineInputBorder(),
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
        const Icon(Icons.refresh_rounded, size: 42, color: Color(0xFF303136)),
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
    required this.label,
    required this.icon,
    required this.onPressed,
    this.emphasized = false,
  });

  final String label;
  final IconData icon;
  final VoidCallback onPressed;
  final bool emphasized;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Icon(
          icon,
          size: 42,
          color: emphasized ? const Color(0xFF8B2E26) : const Color(0xFF303136),
        ),
        const SizedBox(height: 14),
        FilledButton(
          key: Key(emphasized ? 'practice-stop-recording' : 'practice-record'),
          onPressed: onPressed,
          style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
          child: Text(label),
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
        const Text('确认转写', style: TextStyle(fontWeight: FontWeight.w700)),
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
                child: const Text('确认提交'),
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
    return Column(
      children: [
        const CircularProgressIndicator(),
        const SizedBox(height: 14),
        Text(label),
      ],
    );
  }
}
