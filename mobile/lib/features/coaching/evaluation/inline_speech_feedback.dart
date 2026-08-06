import 'package:flutter/material.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

/// Projects asynchronous SpeechFeedback into the lightweight in-bubble UI.
class InlineSpeechFeedback extends StatelessWidget {
  const InlineSpeechFeedback({
    required this.projection,
    this.leading,
    this.trailing,
    this.onRepractice,
    this.foregroundColor = SpeakUpDesign.primary,
    this.textColor = SpeakUpDesign.ink,
    super.key,
  });

  final SpeechFeedbackProjection? projection;
  final Widget? leading;
  final Widget? trailing;
  final ValueChanged<SpeechFeedbackItem>? onRepractice;
  final Color foregroundColor;
  final Color textColor;

  @override
  Widget build(BuildContext context) {
    final feedback = projection?.feedback;
    final visible =
        feedback?.feedbackStatus == SpeechFeedbackStatus.ready &&
        feedback?.scoreabilityStatus !=
            SpeechFeedbackScoreabilityStatus.insufficient;
    final correction = visible ? feedback!.items.correction : null;
    final polish = visible ? feedback!.items.polish : null;
    return InlineLanguageFeedback(
      leading: leading,
      trailing: trailing,
      correction: correction?.suggestedText == null
          ? null
          : InlineLanguageSuggestion(
              text: correction!.suggestedText!,
              originalText: correction.anchor.originalExcerpt,
              explanation: correction.explanation,
            ),
      polish: polish?.suggestedText == null
          ? null
          : InlineLanguageSuggestion(
              text: polish!.suggestedText!,
              explanation: polish.explanation,
            ),
      correctionFooter: correction == null || onRepractice == null
          ? null
          : TextButton.icon(
              key: Key(
                'speech-feedback-repractice-${correction.feedbackItemId}',
              ),
              onPressed: () => onRepractice!(correction),
              icon: const Icon(Icons.mic_none_rounded, size: 18),
              label: const Text('再练一次'),
            ),
      foregroundColor: foregroundColor,
      textColor: textColor,
    );
  }
}
