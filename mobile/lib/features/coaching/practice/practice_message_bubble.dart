import 'package:flutter/material.dart';
import 'package:speakup/design/conversation_bubble_surface.dart';
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
  String? _translation;
  bool _translationExpanded = false;
  bool _translationLoading = false;
  bool _translationFailed = false;

  @override
  void didUpdateWidget(covariant PracticeMessageBubble oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.message.id != widget.message.id) {
      _translation = null;
      _translationExpanded = false;
      _translationLoading = false;
      _translationFailed = false;
    }
  }

  Future<void> _toggleTranslation() async {
    if (_translationExpanded) {
      setState(() => _translationExpanded = false);
      return;
    }
    if (_translation != null) {
      setState(() => _translationExpanded = true);
      return;
    }
    final translate = widget.onTranslate;
    if (translate == null || _translationLoading) {
      return;
    }
    setState(() {
      _translationLoading = true;
      _translationFailed = false;
    });
    try {
      final translation = (await translate(widget.message)).trim();
      if (!mounted) {
        return;
      }
      setState(() {
        _translation = translation;
        _translationExpanded = true;
        _translationLoading = false;
      });
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() {
        _translationLoading = false;
        _translationFailed = true;
      });
    }
  }

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
          if (_translationExpanded && _translation != null) ...[
            const SizedBox(height: 8),
            Container(
              key: Key('practice-assistant-translation-${message.id}'),
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: SpeakUpDesign.surfaceMuted,
                borderRadius: BorderRadius.circular(10),
              ),
              child: Text(
                _translation!,
                style: const TextStyle(
                  color: SpeakUpDesign.ink,
                  fontSize: 14,
                  height: 1.45,
                ),
              ),
            ),
          ],
          if (_translationFailed) ...[
            const SizedBox(height: 6),
            Text(
              '翻译失败，请重试。',
              key: Key('practice-assistant-translation-error-${message.id}'),
              style: const TextStyle(
                color: SpeakUpDesign.error,
                fontSize: 12,
                height: 1.35,
              ),
            ),
          ],
          if (widget.onTranslate != null) ...[
            const SizedBox(height: 6),
            TextButton.icon(
              key: Key('practice-assistant-translate-${message.id}'),
              onPressed: _translationLoading ? null : _toggleTranslation,
              style: TextButton.styleFrom(
                foregroundColor: SpeakUpDesign.primary,
                minimumSize: const Size(0, 32),
                padding: const EdgeInsets.symmetric(horizontal: 8),
                visualDensity: VisualDensity.compact,
              ),
              icon: _translationLoading
                  ? const SizedBox.square(
                      dimension: 14,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.translate_rounded, size: 17),
              label: Text(
                _translationLoading
                    ? '翻译中'
                    : _translationExpanded
                    ? '收起'
                    : _translationFailed
                    ? '重试'
                    : '翻译',
              ),
            ),
          ],
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
