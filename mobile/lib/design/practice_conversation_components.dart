import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/design/voice_composer_dock.dart';

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
      return ConversationTextComposerDock(
        controller: textController!,
        focusNode: textFocusNode!,
        enabled: enabled,
        canSubmit: enabled,
        onReturn: onToggleTextMode,
        onSubmit: onSubmitText,
        returnKey: Key('$keyPrefix-return-to-voice'),
        fieldKey: Key('$keyPrefix-text-answer'),
        submitKey: Key('$keyPrefix-submit-text'),
        maxLength: 8000,
        textCapitalization: TextCapitalization.sentences,
      );
    }

    return VoiceComposerDock(
      capture: capture,
      phase: VoiceCapturePhase.idle,
      elapsed: Duration.zero,
      enabled: enabled,
      textEnabled: enabled,
      recordKey: Key('$keyPrefix-record'),
      stopRecordingKey: Key('$keyPrefix-stop-recording'),
      stateLabelKey: Key('$keyPrefix-voice-state-label'),
      durationKey: Key('$keyPrefix-voice-recording-duration'),
      showTextKey: Key('$keyPrefix-open-keyboard'),
      onShowText: onToggleTextMode,
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
      child: SafeArea(
        top: false,
        child: ConversationComposerCapsule(child: child),
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
    return VoiceComposerDock(
      capture: capture,
      phase: phase,
      elapsed: elapsed ?? Duration.zero,
      enabled: true,
      recordKey: Key('$keyPrefix-record'),
      stopRecordingKey: Key('$keyPrefix-stop-recording'),
      stateLabelKey: Key('$keyPrefix-voice-state-label'),
      durationKey: Key('$keyPrefix-voice-target-duration'),
      upwardCancelOnly: upwardCancelOnly,
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

class PracticePendingAudioComposer extends StatelessWidget {
  const PracticePendingAudioComposer({
    required this.keyPrefix,
    required this.onDelete,
    required this.onRetry,
    super.key,
  });

  final String keyPrefix;
  final VoidCallback onDelete;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: Key('$keyPrefix-pending-audio'),
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text('这段录音已保留', style: SpeakUpDesign.cardTitle),
        const SizedBox(height: 4),
        const Text('刚才没有识别成功，可以重试转文字，或删除后重新录音。', style: SpeakUpDesign.body),
        const SizedBox(height: 10),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                key: Key('$keyPrefix-delete-pending-audio'),
                onPressed: onDelete,
                child: const Text('删除录音'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                key: Key('$keyPrefix-retry-transcription'),
                onPressed: onRetry,
                child: const Text('重试转文字'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class PracticeTranscriptComposer extends StatelessWidget {
  const PracticeTranscriptComposer({
    required this.transcript,
    required this.keyPrefix,
    required this.onRerecord,
    required this.onConfirm,
    this.confirmLabel = '发送',
    super.key,
  });

  final String transcript;
  final String keyPrefix;
  final VoidCallback onRerecord;
  final VoidCallback onConfirm;
  final String confirmLabel;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          transcript,
          key: Key('$keyPrefix-transcript'),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: SpeakUpDesign.body,
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                key: Key('$keyPrefix-rerecord'),
                onPressed: onRerecord,
                child: const Text('重录'),
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: FilledButton(
                key: Key('$keyPrefix-confirm-turn'),
                onPressed: onConfirm,
                child: Text(confirmLabel),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class PracticeComposerAction extends StatelessWidget {
  const PracticeComposerAction({
    required this.label,
    required this.actionLabel,
    required this.onPressed,
    this.containerKey,
    this.actionKey,
    super.key,
  });

  final String label;
  final String actionLabel;
  final VoidCallback onPressed;
  final Key? containerKey;
  final Key? actionKey;

  @override
  Widget build(BuildContext context) {
    return Row(
      key: containerKey,
      children: [
        Expanded(
          child: Text(
            label,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: SpeakUpDesign.body,
          ),
        ),
        const SizedBox(width: 8),
        FilledButton(
          key: actionKey,
          style: FilledButton.styleFrom(
            minimumSize: const Size(0, SpeakUpDesign.minTapTarget),
          ),
          onPressed: onPressed,
          child: Text(actionLabel),
        ),
      ],
    );
  }
}
