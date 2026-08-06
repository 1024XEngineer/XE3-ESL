import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/design/voice_composer_dock.dart';

final class InlineLanguageSuggestion {
  const InlineLanguageSuggestion({required this.text, this.explanation});

  final String text;
  final String? explanation;
}

enum _InlineLanguageFeedbackSection { correction, polish }

/// Shared lightweight feedback used inside Agent and Scene message bubbles.
class InlineLanguageFeedback extends StatefulWidget {
  const InlineLanguageFeedback({
    this.leading,
    this.trailing,
    this.correction,
    this.polish,
    this.correctionFooter,
    this.polishFooter,
    this.onSpeakSuggestion,
    this.suggestionLoading = false,
    this.suggestionPlaying = false,
    this.foregroundColor = SpeakUpDesign.primary,
    this.textColor = SpeakUpDesign.ink,
    super.key,
  });

  final Widget? leading;
  final Widget? trailing;
  final InlineLanguageSuggestion? correction;
  final InlineLanguageSuggestion? polish;
  final Widget? correctionFooter;
  final Widget? polishFooter;
  final ValueChanged<String>? onSpeakSuggestion;
  final bool suggestionLoading;
  final bool suggestionPlaying;
  final Color foregroundColor;
  final Color textColor;

  @override
  State<InlineLanguageFeedback> createState() => _InlineLanguageFeedbackState();
}

class _InlineLanguageFeedbackState extends State<InlineLanguageFeedback> {
  _InlineLanguageFeedbackSection? _expanded;

  @override
  void didUpdateWidget(covariant InlineLanguageFeedback oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_expanded == _InlineLanguageFeedbackSection.correction &&
        widget.correction == null) {
      _expanded = null;
    }
    if (_expanded == _InlineLanguageFeedbackSection.polish &&
        widget.polish == null) {
      _expanded = null;
    }
  }

  void _toggle(_InlineLanguageFeedbackSection section) {
    setState(() => _expanded = _expanded == section ? null : section);
  }

  @override
  Widget build(BuildContext context) {
    final suggestion = switch (_expanded) {
      _InlineLanguageFeedbackSection.correction => widget.correction,
      _InlineLanguageFeedbackSection.polish => widget.polish,
      null => null,
    };
    final footer = switch (_expanded) {
      _InlineLanguageFeedbackSection.correction => widget.correctionFooter,
      _InlineLanguageFeedbackSection.polish => widget.polishFooter,
      null => null,
    };
    final actions = <Widget>[
      if (widget.leading != null) widget.leading!,
      if (widget.correction != null)
        _InlineFeedbackAction(
          key: const Key('inline-language-correction'),
          icon: Icons.edit_outlined,
          label: '纠错',
          selected: _expanded == _InlineLanguageFeedbackSection.correction,
          color: widget.foregroundColor,
          onPressed: () => _toggle(_InlineLanguageFeedbackSection.correction),
        ),
      if (widget.polish != null)
        _InlineFeedbackAction(
          key: const Key('inline-language-polish'),
          icon: Icons.auto_awesome_outlined,
          label: '润色',
          selected: _expanded == _InlineLanguageFeedbackSection.polish,
          color: widget.foregroundColor,
          onPressed: () => _toggle(_InlineLanguageFeedbackSection.polish),
        ),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Wrap(
                spacing: SpeakUpDesign.space4,
                runSpacing: SpeakUpDesign.space4,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: actions,
              ),
            ),
            if (widget.trailing != null) widget.trailing!,
          ],
        ),
        if (suggestion != null) ...[
          const SizedBox(height: SpeakUpDesign.space8),
          Text(
            _expanded == _InlineLanguageFeedbackSection.correction
                ? '建议改为'
                : '更自然的表达',
            style: SpeakUpDesign.meta.copyWith(
              color: widget.foregroundColor,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: SpeakUpDesign.space4),
          Text(
            suggestion.text,
            key: const Key('inline-language-suggestion-text'),
            style: SpeakUpDesign.body.copyWith(
              color: widget.textColor,
              height: 1.45,
            ),
          ),
          if (suggestion.explanation case final explanation?
              when explanation.trim().isNotEmpty) ...[
            const SizedBox(height: SpeakUpDesign.space4),
            Text(
              explanation,
              key: const Key('inline-language-suggestion-explanation'),
              style: SpeakUpDesign.meta,
            ),
          ],
          if (widget.onSpeakSuggestion != null) ...[
            const SizedBox(height: SpeakUpDesign.space4),
            _InlineFeedbackAction(
              key: const Key('inline-language-suggestion-play'),
              icon: widget.suggestionPlaying
                  ? Icons.stop_rounded
                  : Icons.volume_up_outlined,
              label: widget.suggestionPlaying ? '停止朗读' : '朗读',
              loading: widget.suggestionLoading,
              color: widget.foregroundColor,
              onPressed: widget.suggestionLoading
                  ? null
                  : () => widget.onSpeakSuggestion!(suggestion.text),
            ),
          ],
          if (footer != null) ...[
            const SizedBox(height: SpeakUpDesign.space4),
            footer,
          ],
        ],
      ],
    );
  }
}

class _InlineFeedbackAction extends StatelessWidget {
  const _InlineFeedbackAction({
    required this.icon,
    required this.label,
    required this.color,
    required this.onPressed,
    this.selected = false,
    this.loading = false,
    super.key,
  });

  final IconData icon;
  final String label;
  final Color color;
  final VoidCallback? onPressed;
  final bool selected;
  final bool loading;

  @override
  Widget build(BuildContext context) {
    return TextButton.icon(
      onPressed: onPressed,
      style: TextButton.styleFrom(
        foregroundColor: color,
        minimumSize: const Size(0, SpeakUpDesign.minTapTarget),
        padding: const EdgeInsets.symmetric(horizontal: SpeakUpDesign.space4),
        visualDensity: VisualDensity.compact,
        textStyle: SpeakUpDesign.label,
      ),
      icon: loading
          ? const SizedBox.square(
              dimension: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(selected ? Icons.keyboard_arrow_up_rounded : icon, size: 19),
      label: Text(label),
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
