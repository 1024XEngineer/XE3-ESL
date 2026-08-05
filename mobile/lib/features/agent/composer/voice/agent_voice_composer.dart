import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';

class AgentComposerVoiceDock extends StatelessWidget {
  const AgentComposerVoiceDock({
    required this.capture,
    required this.phase,
    required this.elapsed,
    required this.enabled,
    required this.textEnabled,
    required this.canAddImages,
    required this.onAddImages,
    required this.onShowText,
    super.key,
  });

  final VoiceCaptureView capture;
  final VoiceCapturePhase phase;
  final Duration elapsed;
  final bool enabled;
  final bool textEnabled;
  final bool canAddImages;
  final FutureOr<void> Function() onAddImages;
  final VoidCallback onShowText;

  @override
  Widget build(BuildContext context) {
    final capturing =
        phase == VoiceCapturePhase.starting ||
        phase == VoiceCapturePhase.recording;
    final label = switch ((phase, capture.releaseIntent, capture.tapMode)) {
      (VoiceCapturePhase.starting, _, _) => '正在打开麦克风…',
      (_, VoiceCaptureReleaseIntent.cancel, _) => '松开取消',
      (VoiceCapturePhase.recording, _, true) => '点击发送 · 上滑取消',
      (VoiceCapturePhase.recording, _, false) => '上滑取消 · 松开发送',
      (_, VoiceCaptureReleaseIntent.convertToText, _) => '松开发送语音',
      _ => enabled ? '点击或长按说话' : '暂时无法录音',
    };
    final mainTarget = capture.wrapTarget(
      key: const Key('agent-mic-placeholder'),
      semanticsLabel: capturing ? '发送语音' : '开始录音',
      child: AnimatedContainer(
        key: capturing ? const Key('agent-voice-stop') : null,
        duration: const Duration(milliseconds: 100),
        constraints: const BoxConstraints(minHeight: 48),
        decoration: BoxDecoration(
          color: capture.cancelArmed
              ? SpeakUpDesign.errorMuted
              : capture.convertArmed
              ? SpeakUpDesign.primaryMuted
              : capturing
              ? SpeakUpDesign.surfaceMuted
              : Colors.transparent,
          borderRadius: BorderRadius.circular(999),
          border: Border.all(
            color: capture.cancelArmed
                ? SpeakUpDesign.error
                : capture.convertArmed
                ? SpeakUpDesign.primary
                : capturing
                ? SpeakUpDesign.border
                : Colors.transparent,
          ),
        ),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            if (capturing) ...[
              const Icon(
                Icons.graphic_eq_rounded,
                color: SpeakUpDesign.ink,
                size: 22,
              ),
              const SizedBox(width: 9),
            ],
            Flexible(
              child: Text(
                label,
                key: const Key('agent-voice-state-label'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  color: capturing
                      ? SpeakUpDesign.ink
                      : SpeakUpDesign.secondary,
                  fontSize: 15,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
            if (phase == VoiceCapturePhase.recording) ...[
              const SizedBox(width: 10),
              Text(
                _formatDuration(elapsed),
                key: const Key('agent-voice-recording-duration'),
                style: const TextStyle(
                  color: SpeakUpDesign.secondary,
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ],
        ),
      ),
    );

    return Row(
      children: [
        if (!capturing)
          IconButton(
            key: const Key('agent-image-picker-button'),
            tooltip: '添加图片',
            onPressed: textEnabled && canAddImages ? () => onAddImages() : null,
            constraints: const BoxConstraints.tightFor(width: 42, height: 42),
            padding: EdgeInsets.zero,
            color: SpeakUpDesign.secondary,
            icon: const Icon(Icons.add_rounded, size: 28),
          ),
        Expanded(child: mainTarget),
        if (!capturing)
          IconButton(
            key: const Key('agent-show-text-composer'),
            tooltip: '切换到键盘输入',
            onPressed: textEnabled ? onShowText : null,
            constraints: const BoxConstraints.tightFor(width: 42, height: 42),
            color: SpeakUpDesign.secondary,
            icon: const Icon(Icons.keyboard_alt_outlined, size: 24),
          ),
      ],
    );
  }
}

class AgentComposerVoiceStatusDock extends StatelessWidget {
  const AgentComposerVoiceStatusDock({
    required this.state,
    required this.message,
    required this.canCancel,
    required this.canRetry,
    required this.onCancel,
    required this.onRetry,
    super.key,
  });

  final AgentVoiceComposerState state;
  final String message;
  final bool canCancel;
  final bool canRetry;
  final FutureOr<void> Function() onCancel;
  final FutureOr<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    final failed = state == AgentVoiceComposerState.failed;
    if (!failed) {
      return SizedBox(
        height: 48,
        child: Stack(
          alignment: Alignment.center,
          children: [
            if (canCancel)
              Align(
                alignment: Alignment.centerLeft,
                child: IconButton(
                  key: const Key('agent-voice-cancel'),
                  tooltip: '取消语音输入',
                  onPressed: () => onCancel(),
                  constraints: const BoxConstraints.tightFor(
                    width: 40,
                    height: 40,
                  ),
                  padding: EdgeInsets.zero,
                  icon: const Icon(Icons.close_rounded, size: 21),
                ),
              ),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox.square(
                  dimension: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                const SizedBox(width: 8),
                Flexible(
                  child: Text(
                    message,
                    key: const Key('agent-voice-state-label'),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      color: SpeakUpDesign.secondary,
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      );
    }
    return Row(
      children: [
        if (canCancel) ...[
          IconButton(
            key: const Key('agent-voice-cancel'),
            tooltip: '取消语音输入',
            onPressed: () => onCancel(),
            constraints: const BoxConstraints.tightFor(width: 40, height: 40),
            padding: EdgeInsets.zero,
            icon: const Icon(Icons.close_rounded, size: 21),
          ),
          const SizedBox(width: 4),
        ],
        Expanded(
          child: Text(
            message,
            key: const Key('agent-voice-state-label'),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              color: failed ? SpeakUpDesign.error : SpeakUpDesign.secondary,
              fontSize: 14,
              fontWeight: FontWeight.w600,
              height: 1.3,
            ),
          ),
        ),
        if (canRetry)
          IconButton(
            key: const Key('agent-voice-retry'),
            tooltip: '重试转文字',
            onPressed: onRetry == null ? null : () => onRetry?.call(),
            icon: const Icon(Icons.refresh_rounded),
          ),
      ],
    );
  }
}

String agentComposerVoiceStateLabel(AgentVoiceComposerState state) {
  return switch (state) {
    AgentVoiceComposerState.starting => '正在打开麦克风…',
    AgentVoiceComposerState.uploading => '正在处理语音…',
    AgentVoiceComposerState.transcribing => '正在转写…',
    AgentVoiceComposerState.confirming => '已识别，SpeakUp 正在回复…',
    AgentVoiceComposerState.awaitingAssistant => 'SpeakUp 正在回复…',
    _ => '正在处理…',
  };
}

String _formatDuration(Duration value) {
  final totalSeconds = value.inSeconds.clamp(0, 3599);
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes:${seconds.toString().padLeft(2, '0')}';
}
