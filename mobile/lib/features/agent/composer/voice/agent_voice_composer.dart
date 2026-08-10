import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/design/voice_composer_dock.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_controller.dart';

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
    this.liveTranscript,
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
  final String? liveTranscript;

  @override
  Widget build(BuildContext context) {
    return VoiceComposerDock(
      capture: capture,
      phase: phase,
      elapsed: elapsed,
      enabled: enabled,
      textEnabled: textEnabled,
      recordKey: const Key('agent-mic-placeholder'),
      stopRecordingKey: const Key('agent-voice-stop'),
      stateLabelKey: const Key('agent-voice-state-label'),
      durationKey: const Key('agent-voice-recording-duration'),
      showTextKey: const Key('agent-show-text-composer'),
      onShowText: onShowText,
      liveTranscript: liveTranscript,
      liveTranscriptKey: const Key('agent-voice-live-transcript'),
      tapRecordingLabel: '点击转文字 · 上滑取消',
      holdRecordingLabel: '上滑取消 · 松开转文字',
      capturingSemanticsLabel: '停止录音并转文字',
      leading: IconButton(
        key: const Key('agent-image-picker-button'),
        tooltip: '添加图片',
        onPressed: textEnabled && canAddImages ? () => onAddImages() : null,
        constraints: const BoxConstraints.tightFor(width: 42, height: 42),
        padding: EdgeInsets.zero,
        color: SpeakUpDesign.secondary,
        icon: const Icon(Icons.add_rounded, size: 28),
      ),
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

  final AgentVoiceInputState state;
  final String message;
  final bool canCancel;
  final bool canRetry;
  final FutureOr<void> Function() onCancel;
  final FutureOr<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    final failed = state == AgentVoiceInputState.failed;
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

String agentComposerVoiceStateLabel(AgentVoiceInputState state) {
  return switch (state) {
    AgentVoiceInputState.starting => '正在打开麦克风…',
    AgentVoiceInputState.completing => '正在整理识别文字…',
    _ => '正在处理…',
  };
}
