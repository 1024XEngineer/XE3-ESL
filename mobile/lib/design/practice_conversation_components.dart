import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/design/voice_composer_dock.dart';

final class InlineLanguageSuggestion {
  const InlineLanguageSuggestion({
    required this.text,
    this.originalText,
    this.explanation,
  });

  final String text;
  final String? originalText;
  final String? explanation;
}

/// Shared lightweight feedback used inside Agent and Scene message bubbles.
class InlineLanguageFeedback extends StatefulWidget {
  const InlineLanguageFeedback({
    this.leading,
    this.trailing,
    this.correction,
    this.polish,
    this.feedbackNotice,
    this.correctionFooter,
    this.polishFooter,
    this.onSpeakSuggestion,
    this.suggestionLoading = false,
    this.suggestionPlaying = false,
    this.feedbackLoading = false,
    this.optimizeIconOnly = false,
    this.onExpandedChanged,
    this.foregroundColor = SpeakUpDesign.primary,
    this.textColor = SpeakUpDesign.ink,
    super.key,
  });

  final Widget? leading;
  final Widget? trailing;
  final InlineLanguageSuggestion? correction;
  final InlineLanguageSuggestion? polish;
  final String? feedbackNotice;
  final Widget? correctionFooter;
  final Widget? polishFooter;
  final ValueChanged<String>? onSpeakSuggestion;
  final bool suggestionLoading;
  final bool suggestionPlaying;
  final bool feedbackLoading;
  final bool optimizeIconOnly;
  final ValueChanged<bool>? onExpandedChanged;
  final Color foregroundColor;
  final Color textColor;

  @override
  State<InlineLanguageFeedback> createState() => _InlineLanguageFeedbackState();
}

class _InlineLanguageFeedbackState extends State<InlineLanguageFeedback> {
  bool _expanded = false;

  @override
  void didUpdateWidget(covariant InlineLanguageFeedback oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.correction == null &&
        widget.polish == null &&
        widget.feedbackNotice == null) {
      _expanded = false;
    }
  }

  void _toggle() {
    final expanded = !_expanded;
    setState(() => _expanded = expanded);
    widget.onExpandedChanged?.call(expanded);
  }

  @override
  Widget build(BuildContext context) {
    final feedbackNotice = widget.correction == null && widget.polish == null
        ? widget.feedbackNotice
        : null;
    final hasFeedback =
        widget.correction != null ||
        widget.polish != null ||
        feedbackNotice != null;
    final spokenSuggestion = widget.polish ?? widget.correction;
    final actions = <Widget>[
      if (widget.leading != null) widget.leading!,
      if (hasFeedback)
        _InlineFeedbackAction(
          key: const Key('inline-language-optimize'),
          icon: Icons.auto_awesome_outlined,
          label: '优化',
          iconOnly: widget.optimizeIconOnly,
          selected: _expanded,
          color: widget.foregroundColor,
          onPressed: _toggle,
        ),
      if (!hasFeedback && widget.feedbackLoading)
        _InlineFeedbackAction(
          key: const Key('inline-language-optimizing'),
          icon: Icons.auto_awesome_outlined,
          label: '优化中',
          loading: true,
          iconOnly: widget.optimizeIconOnly,
          color: widget.foregroundColor,
          onPressed: null,
        ),
    ];
    final actionWrap = Wrap(
      spacing: SpeakUpDesign.space4,
      runSpacing: SpeakUpDesign.space4,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: actions,
    );
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          mainAxisSize: widget.trailing == null
              ? MainAxisSize.min
              : MainAxisSize.max,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (widget.trailing == null)
              actionWrap
            else
              Expanded(child: actionWrap),
            if (widget.trailing != null) widget.trailing!,
          ],
        ),
        if (_expanded && hasFeedback) ...[
          const SizedBox(height: SpeakUpDesign.space8),
          if (feedbackNotice case final notice?)
            Text(
              notice,
              key: const Key('inline-language-feedback-notice'),
              style: SpeakUpDesign.body.copyWith(color: widget.textColor),
            ),
          if (widget.correction case final correction?) ...[
            _InlineFeedbackHeading(label: '纠错', color: widget.foregroundColor),
            const SizedBox(height: SpeakUpDesign.space4),
            _InlineCorrectionDiff(
              originalText: correction.originalText!,
              correctedText: correction.text,
              textColor: widget.textColor,
            ),
            if (correction.explanation case final explanation?
                when explanation.trim().isNotEmpty) ...[
              const SizedBox(height: SpeakUpDesign.space4),
              Text(
                explanation,
                key: const Key('inline-language-correction-explanation'),
                style: SpeakUpDesign.meta,
              ),
            ],
            if (widget.correctionFooter case final footer?) ...[
              const SizedBox(height: SpeakUpDesign.space4),
              footer,
            ],
          ],
          if (widget.polish case final polish?) ...[
            if (widget.correction != null)
              const SizedBox(height: SpeakUpDesign.space12),
            _InlineFeedbackHeading(
              label: '更自然的表达',
              color: widget.foregroundColor,
            ),
            const SizedBox(height: SpeakUpDesign.space4),
            Text(
              polish.text,
              key: const Key('inline-language-polish-text'),
              style: SpeakUpDesign.body.copyWith(
                color: widget.textColor,
                height: 1.45,
              ),
            ),
            if (polish.explanation case final explanation?
                when explanation.trim().isNotEmpty) ...[
              const SizedBox(height: SpeakUpDesign.space4),
              Text(
                explanation,
                key: const Key('inline-language-polish-explanation'),
                style: SpeakUpDesign.meta,
              ),
            ],
            if (widget.polishFooter case final footer?) ...[
              const SizedBox(height: SpeakUpDesign.space4),
              footer,
            ],
          ],
          if (widget.onSpeakSuggestion != null && spokenSuggestion != null) ...[
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
                  : () => widget.onSpeakSuggestion!(spokenSuggestion.text),
            ),
          ],
        ],
      ],
    );
  }
}

class InlineVoicePlaybackAction extends StatelessWidget {
  const InlineVoicePlaybackAction({
    required this.loading,
    required this.playing,
    required this.onPressed,
    this.duration,
    super.key,
  });

  final bool loading;
  final bool playing;
  final Duration? duration;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final durationLabel = duration == null
        ? ''
        : '，${_formatInlineDuration(duration!)}';
    return Semantics(
      button: true,
      enabled: onPressed != null,
      label: playing ? '停止播放原声' : '播放原声$durationLabel',
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          onTap: onPressed,
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          child: SizedBox.square(
            dimension: SpeakUpDesign.minTapTarget,
            child: Center(
              child: loading
                  ? const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      playing ? Icons.pause_rounded : Icons.graphic_eq_rounded,
                      size: 24,
                      color: onPressed == null
                          ? SpeakUpDesign.tertiary
                          : SpeakUpDesign.primary,
                    ),
            ),
          ),
        ),
      ),
    );
  }
}

String _formatInlineDuration(Duration value) {
  final totalSeconds = value.inSeconds.clamp(0, 3599);
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes:${seconds.toString().padLeft(2, '0')}';
}

class _InlineFeedbackHeading extends StatelessWidget {
  const _InlineFeedbackHeading({required this.label, required this.color});

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Text(
      label,
      style: SpeakUpDesign.meta.copyWith(
        color: color,
        fontWeight: FontWeight.w700,
      ),
    );
  }
}

class _InlineCorrectionDiff extends StatelessWidget {
  const _InlineCorrectionDiff({
    required this.originalText,
    required this.correctedText,
    required this.textColor,
  });

  final String originalText;
  final String correctedText;
  final Color textColor;

  @override
  Widget build(BuildContext context) {
    return Text.rich(
      TextSpan(
        children: [
          for (final part in _correctionDiff(originalText, correctedText))
            TextSpan(
              text: part.text,
              style: switch (part.change) {
                _CorrectionChange.unchanged => TextStyle(color: textColor),
                _CorrectionChange.removed => const TextStyle(
                  color: SpeakUpDesign.error,
                  decoration: TextDecoration.lineThrough,
                  decorationColor: SpeakUpDesign.error,
                  decorationThickness: 1.5,
                ),
                _CorrectionChange.added => const TextStyle(
                  color: SpeakUpDesign.success,
                  fontWeight: FontWeight.w600,
                ),
              },
            ),
        ],
      ),
      key: const Key('inline-language-correction-diff'),
      style: SpeakUpDesign.body.copyWith(height: 1.55),
    );
  }
}

enum _CorrectionChange { unchanged, removed, added }

final class _CorrectionPart {
  const _CorrectionPart(this.text, this.change);

  final String text;
  final _CorrectionChange change;
}

final class _CorrectionToken {
  const _CorrectionToken(this.leading, this.value);

  final String leading;
  final String value;
}

List<_CorrectionPart> _correctionDiff(String original, String corrected) {
  final before = _correctionTokens(_englishCorrectionReference(original));
  final after = _correctionTokens(corrected.trim());
  final width = after.length + 1;
  final lengths = Uint16List((before.length + 1) * width);
  for (var beforeIndex = before.length - 1; beforeIndex >= 0; beforeIndex--) {
    for (var afterIndex = after.length - 1; afterIndex >= 0; afterIndex--) {
      final index = beforeIndex * width + afterIndex;
      lengths[index] = before[beforeIndex].value == after[afterIndex].value
          ? lengths[(beforeIndex + 1) * width + afterIndex + 1] + 1
          : lengths[(beforeIndex + 1) * width + afterIndex] >=
                lengths[beforeIndex * width + afterIndex + 1]
          ? lengths[(beforeIndex + 1) * width + afterIndex]
          : lengths[beforeIndex * width + afterIndex + 1];
    }
  }

  final parts = <_CorrectionPart>[];
  var beforeIndex = 0;
  var afterIndex = 0;
  while (beforeIndex < before.length || afterIndex < after.length) {
    if (beforeIndex < before.length &&
        afterIndex < after.length &&
        before[beforeIndex].value == after[afterIndex].value) {
      _appendCorrectionPart(
        parts,
        before[beforeIndex],
        _CorrectionChange.unchanged,
      );
      beforeIndex++;
      afterIndex++;
      continue;
    }
    final remove =
        beforeIndex < before.length &&
        (afterIndex == after.length ||
            lengths[(beforeIndex + 1) * width + afterIndex] >=
                lengths[beforeIndex * width + afterIndex + 1]);
    if (remove) {
      _appendCorrectionPart(
        parts,
        before[beforeIndex++],
        _CorrectionChange.removed,
      );
    } else {
      _appendCorrectionPart(
        parts,
        after[afterIndex++],
        _CorrectionChange.added,
      );
    }
  }
  return parts;
}

void _appendCorrectionPart(
  List<_CorrectionPart> parts,
  _CorrectionToken token,
  _CorrectionChange change,
) {
  var text = '${token.leading}${token.value}';
  if (parts.isNotEmpty &&
      token.leading.isEmpty &&
      _wordCorrectionToken(parts.last.text) &&
      _wordCorrectionToken(token.value)) {
    text = ' $text';
  }
  if (parts.isNotEmpty && parts.last.change == change) {
    final previous = parts.removeLast();
    parts.add(_CorrectionPart('${previous.text}$text', change));
    return;
  }
  parts.add(_CorrectionPart(text, change));
}

List<_CorrectionToken> _correctionTokens(String value) {
  final matches = RegExp(
    r"\s+|[A-Za-z0-9]+(?:['’][A-Za-z0-9]+)*|[^\s]",
  ).allMatches(value);
  final tokens = <_CorrectionToken>[];
  var leading = '';
  for (final match in matches) {
    final token = match.group(0)!;
    if (token.trim().isEmpty) {
      leading = token;
      continue;
    }
    tokens.add(_CorrectionToken(leading, token));
    leading = '';
  }
  return tokens;
}

String _englishCorrectionReference(String value) {
  return value
      .replaceAll(RegExp(r'[^\x20-\x7E]'), ' ')
      .replaceAll(RegExp(r'\s+'), ' ')
      .trim()
      .replaceAll(RegExp(r'^[,;:!?\-.\s]+|[,;:!?\-.\s]+$'), '');
}

bool _wordCorrectionToken(String value) {
  final trimmed = value.trim();
  return trimmed.isNotEmpty &&
      RegExp(r'^[A-Za-z0-9]').hasMatch(trimmed) &&
      RegExp(r'[A-Za-z0-9]$').hasMatch(trimmed);
}

class _InlineFeedbackAction extends StatelessWidget {
  const _InlineFeedbackAction({
    required this.icon,
    required this.label,
    required this.color,
    required this.onPressed,
    this.selected = false,
    this.loading = false,
    this.iconOnly = false,
    super.key,
  });

  final IconData icon;
  final String label;
  final Color color;
  final VoidCallback? onPressed;
  final bool selected;
  final bool loading;
  final bool iconOnly;

  @override
  Widget build(BuildContext context) {
    final actionIcon = loading
        ? const SizedBox.square(
            dimension: 16,
            child: CircularProgressIndicator(strokeWidth: 2),
          )
        : Icon(selected ? Icons.keyboard_arrow_up_rounded : icon, size: 19);
    if (iconOnly) {
      return IconButton(
        tooltip: label,
        onPressed: onPressed,
        color: color,
        constraints: const BoxConstraints.tightFor(
          width: SpeakUpDesign.minTapTarget,
          height: SpeakUpDesign.minTapTarget,
        ),
        padding: EdgeInsets.zero,
        visualDensity: VisualDensity.compact,
        icon: actionIcon,
      );
    }
    return TextButton.icon(
      onPressed: onPressed,
      style: TextButton.styleFrom(
        foregroundColor: color,
        minimumSize: const Size(0, SpeakUpDesign.minTapTarget),
        padding: const EdgeInsets.symmetric(horizontal: SpeakUpDesign.space4),
        visualDensity: VisualDensity.compact,
        textStyle: SpeakUpDesign.label,
      ),
      icon: actionIcon,
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
    this.transcript = '',
    this.upwardCancelOnly = false,
    super.key,
  });

  final VoiceCaptureView capture;
  final VoiceCapturePhase phase;
  final String keyPrefix;
  final Duration? elapsed;
  final String transcript;
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
      liveTranscript: transcript,
      liveTranscriptKey: Key('$keyPrefix-live-transcript'),
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
