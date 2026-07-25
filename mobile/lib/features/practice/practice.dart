/// Practice module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';

class PracticePage extends StatefulWidget {
  const PracticePage({
    this.previewMode = false,
    this.agentController,
    super.key,
  });

  final bool previewMode;
  final AgentController? agentController;

  @override
  State<PracticePage> createState() => _PracticePageState();
}

class _PracticePageState extends State<PracticePage> {
  static const _maxReviewExitFrameAttempts = 60;

  bool _scheduledReviewExit = false;
  int _reviewExitAttempts = 0;
  Animation<double>? _observedSecondaryAnimation;
  AnimationStatusListener? _reviewRouteStatusListener;

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

  @override
  Widget build(BuildContext context) {
    final controller = widget.agentController;
    final scene = controller?.scene;
    return Scaffold(
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
                  _CurrentQuestion(messages: controller.messages),
                  const SizedBox(height: 18),
                  _RecordingPanel(controller: controller),
                  if (controller.errorMessage case final message?) ...[
                    const SizedBox(height: 14),
                    Text(
                      message,
                      key: const Key('practice-error-message'),
                      style: const TextStyle(color: Color(0xFF8B2E26)),
                    ),
                  ],
                  const SizedBox(height: 18),
                  if (widget.previewMode)
                    const Text(
                      '当前页面仅供显式 Fake 预览，不代表生产语音服务已经接入。',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Color(0xFF85878E), fontSize: 12),
                    ),
                ],
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

class _CurrentQuestion extends StatelessWidget {
  const _CurrentQuestion({required this.messages});

  final List<AgentMessage> messages;

  @override
  Widget build(BuildContext context) {
    final question = messages.reversed
        .where((message) => message.role == AgentMessageRole.assistant)
        .map((message) => message.text)
        .firstOrNull;
    return Card(
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '当前问题',
              style: TextStyle(
                color: Color(0xFF777983),
                fontSize: 13,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              question ?? '准备好后开始第一轮。',
              key: const Key('practice-current-question'),
              style: const TextStyle(
                fontSize: 17,
                fontWeight: FontWeight.w600,
                height: 1.45,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _RecordingPanel extends StatelessWidget {
  const _RecordingPanel({required this.controller});

  final AgentController controller;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: switch (controller.recordingState) {
          PracticeRecordingState.idle => _RecordAction(
            label: '开始录音',
            icon: Icons.mic_none_rounded,
            onPressed: controller.startRecording,
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
