import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

class ConversationBubbleSurface extends StatelessWidget {
  const ConversationBubbleSurface({
    required this.isUser,
    required this.child,
    this.bubbleKey,
    this.maxWidth = 340,
    this.margin = EdgeInsets.zero,
    this.padding,
    this.semanticsLabel,
    super.key,
  });

  final bool isUser;
  final Widget child;
  final Key? bubbleKey;
  final double maxWidth;
  final EdgeInsetsGeometry margin;
  final EdgeInsetsGeometry? padding;
  final String? semanticsLabel;

  @override
  Widget build(BuildContext context) {
    final bubble = Container(
      key: bubbleKey,
      constraints: BoxConstraints(maxWidth: maxWidth),
      margin: margin,
      padding:
          padding ??
          (isUser
              ? const EdgeInsets.fromLTRB(14, 11, 12, 11)
              : const EdgeInsets.fromLTRB(2, 7, 12, 9)),
      decoration: BoxDecoration(
        color: isUser ? SpeakUpDesign.primaryMuted : Colors.transparent,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: isUser ? Border.all(color: SpeakUpDesign.border) : null,
      ),
      child: child,
    );
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: semanticsLabel == null
          ? bubble
          : Semantics(label: semanticsLabel, child: bubble),
    );
  }
}
