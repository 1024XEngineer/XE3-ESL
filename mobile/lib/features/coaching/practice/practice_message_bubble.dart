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

final class PracticeMessageBubble extends StatefulWidget {
  const PracticeMessageBubble({
    required this.message,
    this.polishedText,
    this.polishLoading = false,
    this.messageTextVisible = true,
    this.onTranslate,
    super.key,
  });

  final PracticeMessage message;
  final String? polishedText;
  final bool polishLoading;
  final bool messageTextVisible;
  final Future<String> Function(PracticeMessage message)? onTranslate;

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
                  visible: widget.messageTextVisible,
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
                    key: Key(
                      'practice-assistant-translation-error-${message.id}',
                    ),
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
                if (widget.polishLoading) ...[
                  const SizedBox(height: 8),
                  const LinearProgressIndicator(minHeight: 2),
                ] else if (widget.polishedText case final text?) ...[
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
