import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';

class VoiceComposerDock extends StatelessWidget {
  const VoiceComposerDock({
    required this.capture,
    required this.phase,
    required this.elapsed,
    required this.enabled,
    required this.recordKey,
    required this.stopRecordingKey,
    required this.stateLabelKey,
    required this.durationKey,
    this.leading,
    this.onShowText,
    this.showTextKey,
    this.textEnabled = true,
    this.upwardCancelOnly = true,
    this.showTextAction = true,
    this.directTapToSend = false,
    super.key,
  });

  final VoiceCaptureView capture;
  final VoiceCapturePhase phase;
  final Duration elapsed;
  final bool enabled;
  final Key recordKey;
  final Key stopRecordingKey;
  final Key stateLabelKey;
  final Key durationKey;
  final Widget? leading;
  final VoidCallback? onShowText;
  final Key? showTextKey;
  final bool textEnabled;
  final bool upwardCancelOnly;
  final bool showTextAction;
  final bool directTapToSend;

  @override
  Widget build(BuildContext context) {
    final capturing =
        phase == VoiceCapturePhase.starting ||
        phase == VoiceCapturePhase.recording;
    final label = switch ((phase, capture.releaseIntent, capture.tapMode)) {
      (VoiceCapturePhase.starting, _, _) => '正在打开麦克风…',
      (_, VoiceCaptureReleaseIntent.cancel, _) => '松开取消',
      (_, VoiceCaptureReleaseIntent.convertToText, _) => '松开转文字',
      (VoiceCapturePhase.recording, _, true) =>
        upwardCancelOnly ? '点击发送 · 上滑取消' : '点击发送 · 左取消 · 右转文字',
      (VoiceCapturePhase.recording, _, false) =>
        upwardCancelOnly ? '上滑取消 · 松开发送' : '松开发送 · 左取消 · 右转文字',
      _ => enabled ? '点击或长按说话' : '暂时无法录音',
    };
    final targetContent = AnimatedContainer(
      key: capturing ? stopRecordingKey : null,
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
              key: stateLabelKey,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: capturing ? SpeakUpDesign.ink : SpeakUpDesign.secondary,
                fontSize: 15,
                fontWeight: FontWeight.w700,
              ),
            ),
          ),
          if (phase == VoiceCapturePhase.recording) ...[
            const SizedBox(width: 10),
            Text(
              _formatDuration(elapsed),
              key: durationKey,
              style: const TextStyle(
                color: SpeakUpDesign.secondary,
                fontSize: 13,
                fontWeight: FontWeight.w700,
              ),
            ),
          ],
        ],
      ),
    );
    final mainTarget = directTapToSend && capturing
        ? Semantics(
            button: true,
            label: '发送语音',
            child: GestureDetector(
              key: recordKey,
              behavior: HitTestBehavior.opaque,
              onTap: capture.sendVoiceTapCapture,
              child: targetContent,
            ),
          )
        : capture.wrapTarget(
            key: recordKey,
            semanticsLabel: capturing ? '发送语音' : '开始录音',
            child: targetContent,
          );
    return Row(
      children: [
        if (!capturing) leading ?? const SizedBox(width: 42),
        Expanded(child: mainTarget),
        if (!capturing)
          showTextAction
              ? IconButton(
                  key: showTextKey,
                  tooltip: '切换到键盘输入',
                  onPressed: textEnabled ? onShowText : null,
                  constraints: const BoxConstraints.tightFor(
                    width: 42,
                    height: 42,
                  ),
                  padding: EdgeInsets.zero,
                  color: SpeakUpDesign.secondary,
                  icon: const Icon(Icons.keyboard_alt_outlined, size: 24),
                )
              : const SizedBox(width: 42),
      ],
    );
  }
}

class ConversationComposerCapsule extends StatelessWidget {
  const ConversationComposerCapsule({
    required this.child,
    this.minHeight = 54,
    super.key,
  });

  final Widget child;
  final double minHeight;

  @override
  Widget build(BuildContext context) {
    return AnimatedContainer(
      duration: const Duration(milliseconds: 180),
      curve: Curves.easeOut,
      constraints: BoxConstraints(minHeight: minHeight),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
      decoration: BoxDecoration(
        color: SpeakUpDesign.primaryMuted.withValues(alpha: 0.9),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: SpeakUpDesign.surface.withValues(alpha: 0.72),
        ),
      ),
      child: child,
    );
  }
}

class ConversationTextComposerDock extends StatelessWidget {
  const ConversationTextComposerDock({
    required this.controller,
    required this.focusNode,
    required this.enabled,
    required this.canSubmit,
    required this.onReturn,
    required this.onSubmit,
    required this.returnKey,
    required this.fieldKey,
    required this.submitKey,
    this.hintText = '输入你的回答',
    this.returnTooltip = '切换到语音输入',
    this.returnIcon = Icons.mic_none_rounded,
    this.maxLines = 2,
    this.maxLength,
    this.submitting = false,
    this.textCapitalization = TextCapitalization.none,
    this.textInputAction = TextInputAction.send,
    this.inputFormatters,
    super.key,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool enabled;
  final bool canSubmit;
  final FutureOr<void> Function() onReturn;
  final FutureOr<void> Function() onSubmit;
  final Key returnKey;
  final Key fieldKey;
  final Key submitKey;
  final String hintText;
  final String returnTooltip;
  final IconData returnIcon;
  final int maxLines;
  final int? maxLength;
  final bool submitting;
  final TextCapitalization textCapitalization;
  final TextInputAction textInputAction;
  final List<TextInputFormatter>? inputFormatters;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        IconButton(
          key: returnKey,
          tooltip: returnTooltip,
          onPressed: enabled ? () => onReturn() : null,
          constraints: const BoxConstraints.tightFor(width: 44, height: 44),
          padding: EdgeInsets.zero,
          color: SpeakUpDesign.secondary,
          icon: Icon(returnIcon, size: 21),
        ),
        Expanded(
          child: TextField(
            key: fieldKey,
            controller: controller,
            focusNode: focusNode,
            enabled: enabled,
            minLines: 1,
            maxLines: maxLines,
            maxLength: maxLength,
            inputFormatters: inputFormatters,
            textCapitalization: textCapitalization,
            textInputAction: textInputAction,
            onSubmitted: (_) {
              if (enabled &&
                  canSubmit &&
                  !submitting &&
                  controller.text.trim().isNotEmpty) {
                onSubmit();
              }
            },
            style: const TextStyle(
              color: SpeakUpDesign.ink,
              fontSize: 15,
              height: 1.4,
            ),
            decoration: InputDecoration(
              hintText: hintText,
              counterText: '',
              hintStyle: const TextStyle(
                color: SpeakUpDesign.tertiary,
                fontSize: 15,
              ),
              border: InputBorder.none,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
              isDense: true,
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 7,
                vertical: 10,
              ),
            ),
          ),
        ),
        ValueListenableBuilder<TextEditingValue>(
          valueListenable: controller,
          builder: (context, value, _) => IconButton.filled(
            key: submitKey,
            tooltip: '发送',
            onPressed:
                enabled &&
                    canSubmit &&
                    !submitting &&
                    value.text.trim().isNotEmpty
                ? () => onSubmit()
                : null,
            constraints: const BoxConstraints.tightFor(width: 40, height: 40),
            padding: EdgeInsets.zero,
            icon: submitting
                ? const SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Colors.white,
                    ),
                  )
                : const Icon(Icons.arrow_upward_rounded, size: 20),
          ),
        ),
      ],
    );
  }
}

String _formatDuration(Duration value) {
  final totalSeconds = value.inSeconds.clamp(0, 3599);
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes:${seconds.toString().padLeft(2, '0')}';
}
