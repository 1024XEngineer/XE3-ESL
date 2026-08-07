import 'package:flutter/material.dart';
import 'package:speakup/design/conversation_bubble_surface.dart';
import 'package:speakup/design/message_translation.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/inline_speech_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

final class PracticeMessageBubble extends StatefulWidget {
  const PracticeMessageBubble({
    required this.message,
    this.feedbackProjection,
    this.onFeedbackRepractice,
    this.messageTextVisible = true,
    this.onTranslate,
    this.actions,
    this.maxWidth = 340,
    super.key,
  });

  final PracticeMessage message;
  final SpeechFeedbackProjection? feedbackProjection;
  final ValueChanged<SpeechFeedbackItem>? onFeedbackRepractice;
  final bool messageTextVisible;
  final Future<String> Function(PracticeMessage message)? onTranslate;
  final Widget? actions;
  final double maxWidth;

  @override
  State<PracticeMessageBubble> createState() => _PracticeMessageBubbleState();
}

final class _PracticeMessageBubbleState extends State<PracticeMessageBubble> {
  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final user = message.role == PracticeMessageRole.user;
    return ConversationBubbleSurface(
      bubbleKey: Key('practice-message-${message.id}'),
      isUser: user,
      maxWidth: widget.maxWidth,
      semanticsLabel: '${user ? '你' : '对话伙伴'}：${message.text}',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Visibility(
            key: Key('practice-message-text-${message.id}'),
            visible: widget.messageTextVisible,
            maintainAnimation: true,
            maintainSize: true,
            maintainState: true,
            child: Text(
              message.text,
              style: TextStyle(color: SpeakUpDesign.ink, height: 1.45),
            ),
          ),
          MessageTranslationDisclosure(
            sourceId: message.id,
            buttonKey: Key('practice-assistant-translate-${message.id}'),
            contentKey: Key('practice-assistant-translation-${message.id}'),
            errorKey: Key('practice-assistant-translation-error-${message.id}'),
            onTranslate: widget.onTranslate == null
                ? null
                : () => widget.onTranslate!(message),
          ),
          if (user && widget.feedbackProjection != null) ...[
            const SizedBox(height: SpeakUpDesign.space4),
            InlineSpeechFeedback(
              projection: widget.feedbackProjection,
              onRepractice: widget.onFeedbackRepractice,
            ),
          ],
          if (widget.actions != null) ...[
            const SizedBox(height: 6),
            widget.actions!,
          ],
        ],
      ),
    );
  }
}
