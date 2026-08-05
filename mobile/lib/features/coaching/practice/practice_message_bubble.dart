import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

final class PracticeChatBubble extends StatelessWidget {
  const PracticeChatBubble({
    required this.message,
    this.actions,
    this.maxWidth = 520,
    super.key,
  });

  final PracticeMessage message;
  final Widget? actions;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    final isUser = message.role == PracticeMessageRole.user;
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

final class PracticeMessageBubble extends StatelessWidget {
  const PracticeMessageBubble({
    required this.message,
    this.polishedText,
    this.polishLoading = false,
    this.messageTextVisible = true,
    super.key,
  });

  final PracticeMessage message;
  final String? polishedText;
  final bool polishLoading;
  final bool messageTextVisible;

  @override
  Widget build(BuildContext context) {
    final user = message.role == PracticeMessageRole.user;
    return Align(
      key: Key('practice-message-${message.id}'),
      alignment: user ? Alignment.centerRight : Alignment.centerLeft,
      child: FractionallySizedBox(
        widthFactor: 0.82,
        child: DecoratedBox(
          decoration: BoxDecoration(
            color: user ? SpeakUpDesign.primary : SpeakUpDesign.surface,
            borderRadius: BorderRadius.circular(18),
            border: user ? null : Border.all(color: SpeakUpDesign.border),
          ),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Visibility(
                  key: Key('practice-message-text-${message.id}'),
                  visible: messageTextVisible,
                  maintainAnimation: true,
                  maintainSize: true,
                  maintainState: true,
                  child: Text(
                    message.text,
                    style: TextStyle(
                      color: user ? Colors.white : SpeakUpDesign.ink,
                      height: 1.45,
                    ),
                  ),
                ),
                if (polishLoading) ...[
                  const SizedBox(height: 8),
                  const LinearProgressIndicator(minHeight: 2),
                ] else if (polishedText case final text?) ...[
                  const SizedBox(height: 8),
                  Text(
                    text,
                    style: TextStyle(
                      color: user
                          ? Colors.white.withValues(alpha: 0.82)
                          : SpeakUpDesign.secondary,
                      fontSize: 13,
                      height: 1.4,
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
