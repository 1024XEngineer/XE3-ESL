import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';

class PracticeChatBubble extends StatelessWidget {
  const PracticeChatBubble({
    required this.message,
    this.actions,
    this.maxWidth = 520,
    super.key,
  });

  final AgentMessage message;
  final Widget? actions;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    final isUser = message.role == AgentMessageRole.user;
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Semantics(
        label: '${isUser ? '你' : '对话伙伴'}：${message.text}',
        child: Container(
          constraints: BoxConstraints(maxWidth: maxWidth),
          padding: EdgeInsets.fromLTRB(
            isUser ? 14 : 2,
            10,
            isUser ? 14 : 10,
            10,
          ),
          decoration: BoxDecoration(
            color: isUser ? SpeakUpDesign.primaryMuted : Colors.transparent,
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
            border: isUser ? Border.all(color: SpeakUpDesign.border) : null,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                message.text,
                style: SpeakUpDesign.body.copyWith(
                  color: SpeakUpDesign.ink,
                  height: 1.45,
                ),
              ),
              if (actions != null) ...[const SizedBox(height: 6), actions!],
            ],
          ),
        ),
      ),
    );
  }
}

class PracticeIdleComposer extends StatelessWidget {
  const PracticeIdleComposer({
    required this.capture,
    required this.textMode,
    required this.onToggleTextMode,
    required this.onSubmitText,
    required this.keyPrefix,
    this.textController,
    this.textFocusNode,
    this.enabled = true,
    super.key,
  }) : assert(!textMode || textController != null),
       assert(!textMode || textFocusNode != null);

  final VoiceCaptureView capture;
  final TextEditingController? textController;
  final FocusNode? textFocusNode;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final FutureOr<void> Function() onSubmitText;
  final String keyPrefix;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    if (textMode) {
      return Row(
        children: [
          IconButton(
            key: Key('$keyPrefix-return-to-voice'),
            tooltip: '切换到语音',
            onPressed: enabled ? onToggleTextMode : null,
            color: SpeakUpDesign.secondary,
            icon: const Icon(Icons.mic_none_rounded),
          ),
          Expanded(
            child: TextField(
              key: Key('$keyPrefix-text-answer'),
              controller: textController!,
              focusNode: textFocusNode!,
              enabled: enabled,
              minLines: 1,
              maxLines: 2,
              maxLength: 8000,
              textCapitalization: TextCapitalization.sentences,
              textAlignVertical: TextAlignVertical.center,
              decoration: const InputDecoration(
                hintText: '输入你的回答',
                counterText: '',
                border: InputBorder.none,
                enabledBorder: InputBorder.none,
                focusedBorder: InputBorder.none,
                isDense: true,
                contentPadding: EdgeInsets.symmetric(
                  horizontal: 8,
                  vertical: 10,
                ),
              ),
              onSubmitted: (_) => onSubmitText(),
            ),
          ),
          IconButton(
            key: Key('$keyPrefix-submit-text'),
            tooltip: '发送',
            onPressed: enabled ? () => onSubmitText() : null,
            color: SpeakUpDesign.primary,
            icon: const Icon(Icons.arrow_upward_rounded),
          ),
        ],
      );
    }

    return Row(
      children: [
        const SizedBox(width: 42),
        Expanded(
          child: capture.wrapTarget(
            key: Key('$keyPrefix-record'),
            semanticsLabel: '点击或长按说话',
            child: const SizedBox(
              height: 48,
              child: Center(
                child: Text(
                  '点击或长按说话',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: SpeakUpDesign.secondary,
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
            ),
          ),
        ),
        IconButton(
          key: Key('$keyPrefix-open-keyboard'),
          tooltip: '切换到键盘输入',
          onPressed: enabled ? onToggleTextMode : null,
          color: SpeakUpDesign.secondary,
          icon: const Icon(Icons.keyboard_alt_outlined, size: 24),
        ),
      ],
    );
  }
}

class PracticeComposerSurface extends StatelessWidget {
  const PracticeComposerSurface({required this.child, super.key});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 0, 12, 8),
      child: Material(
        color: SpeakUpDesign.primaryMuted.withValues(alpha: 0.92),
        elevation: 4,
        shadowColor: const Color(0x16000000),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(999),
          side: BorderSide(
            color: SpeakUpDesign.surface.withValues(alpha: 0.72),
          ),
        ),
        clipBehavior: Clip.antiAlias,
        child: SafeArea(
          top: false,
          minimum: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
          child: child,
        ),
      ),
    );
  }
}

class PracticeRecordingComposer extends StatelessWidget {
  const PracticeRecordingComposer({
    required this.capture,
    required this.phase,
    required this.keyPrefix,
    this.elapsed,
    this.upwardCancelOnly = false,
    super.key,
  });

  final VoiceCaptureView capture;
  final VoiceCapturePhase phase;
  final String keyPrefix;
  final Duration? elapsed;
  final bool upwardCancelOnly;

  @override
  Widget build(BuildContext context) {
    final preparing = phase == VoiceCapturePhase.starting;
    final canceling = capture.releaseIntent == VoiceCaptureReleaseIntent.cancel;
    final label = canceling
        ? '松开取消'
        : preparing
        ? '正在打开麦克风…'
        : capture.tapMode
        ? '点击发送语音'
        : upwardCancelOnly
        ? '松开发送 · 上滑取消'
        : '松开发送 · 左滑取消 · 右滑转文字';
    final color = canceling ? SpeakUpDesign.error : SpeakUpDesign.primary;
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        VoiceCaptureIntentTargets(
          capture: capture,
          elapsed: elapsed ?? Duration.zero,
          keyPrefix: keyPrefix,
        ),
        const SizedBox(height: 10),
        capture.wrapTarget(
          key: Key('$keyPrefix-record'),
          semanticsLabel: label,
          child: KeyedSubtree(
            key: Key('$keyPrefix-stop-recording'),
            child: SizedBox(
              height: 48,
              child: Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(
                    canceling ? Icons.close_rounded : Icons.graphic_eq_rounded,
                    size: 21,
                    color: color,
                  ),
                  const SizedBox(width: 8),
                  Flexible(
                    child: Text(
                      label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      textAlign: TextAlign.center,
                      style: TextStyle(
                        color: color,
                        fontSize: 15,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

class PracticeLoadingComposer extends StatelessWidget {
  const PracticeLoadingComposer({required this.label, super.key});

  final String label;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 48,
      child: Center(
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const SizedBox.square(
              dimension: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            const SizedBox(width: 10),
            Flexible(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body.copyWith(
                  color: SpeakUpDesign.secondary,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
