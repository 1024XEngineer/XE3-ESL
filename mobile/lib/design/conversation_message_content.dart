import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:speakup/design/message_translation.dart';
import 'package:speakup/design/speak_up_design.dart';

/// Text-message presentation shared by Agent and Practice conversations.
///
/// Business models, playback controllers, and network clients remain owned by
/// their feature. Callers provide only presentation state and actions.
final class ConversationTextMessageContent extends StatelessWidget {
  const ConversationTextMessageContent({
    required this.sourceId,
    required this.text,
    required this.isUser,
    required this.translationButtonKey,
    required this.translationContentKey,
    required this.translationErrorKey,
    this.isStreaming = false,
    this.hasFailed = false,
    this.textVisible = true,
    this.textVisibilityKey,
    this.textKey,
    this.streamingKey,
    this.onTranslate,
    this.leadingActions = const <Widget>[],
    this.mediaError,
    this.mediaErrorKey,
    super.key,
  });

  final String sourceId;
  final String text;
  final bool isUser;
  final bool isStreaming;
  final bool hasFailed;
  final bool textVisible;
  final Key? textVisibilityKey;
  final Key? textKey;
  final Key? streamingKey;
  final Key translationButtonKey;
  final Key translationContentKey;
  final Key translationErrorKey;
  final Future<String> Function()? onTranslate;
  final List<Widget> leadingActions;
  final String? mediaError;
  final Key? mediaErrorKey;

  @override
  Widget build(BuildContext context) {
    final content = Visibility(
      key: textVisibilityKey,
      visible: textVisible,
      maintainAnimation: true,
      maintainSize: true,
      maintainState: true,
      child: _messageText(),
    );
    if (hasFailed) {
      return content;
    }
    final translate = isUser || isStreaming ? null : onTranslate;
    final hasActions = leadingActions.isNotEmpty || translate != null;
    final visibleMediaError = isUser ? null : mediaError;
    if (!hasActions && visibleMediaError == null) {
      return content;
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        content,
        if (hasActions)
          MessageTranslationDisclosure(
            sourceId: sourceId,
            buttonKey: translationButtonKey,
            contentKey: translationContentKey,
            errorKey: translationErrorKey,
            onTranslate: translate,
            leadingActions: leadingActions,
          ),
        if (visibleMediaError case final error?) ...[
          const SizedBox(height: 3),
          Text(
            error,
            key: mediaErrorKey,
            style: const TextStyle(
              color: SpeakUpDesign.error,
              fontSize: 12,
              height: 1.35,
            ),
          ),
        ],
      ],
    );
  }

  Widget _messageText() {
    if (isUser) {
      return Text(
        text,
        key: textKey,
        style: const TextStyle(
          color: SpeakUpDesign.ink,
          fontSize: 15,
          height: 1.45,
        ),
      );
    }
    if (isStreaming && text.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: SizedBox.square(
          key: streamingKey,
          dimension: 16,
          child: const CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }
    return ConversationAssistantMarkdown(
      key: textKey,
      data: text,
      foreground: SpeakUpDesign.ink,
    );
  }
}

final class ConversationAssistantMarkdown extends StatelessWidget {
  const ConversationAssistantMarkdown({
    required this.data,
    this.foreground = SpeakUpDesign.ink,
    super.key,
  });

  final String data;
  final Color foreground;

  @override
  Widget build(BuildContext context) {
    final body = TextStyle(color: foreground, fontSize: 15, height: 1.48);
    return MarkdownBody(
      data: data,
      selectable: true,
      fitContent: true,
      styleSheet: MarkdownStyleSheet(
        a: body,
        p: body,
        pPadding: EdgeInsets.zero,
        em: body.copyWith(fontStyle: FontStyle.italic),
        strong: body.copyWith(fontWeight: FontWeight.w700),
        code: body.copyWith(
          fontFamily: 'monospace',
          fontSize: 13.5,
          backgroundColor: SpeakUpDesign.surfaceMuted,
        ),
        h1: body.copyWith(fontSize: 20, fontWeight: FontWeight.w700),
        h2: body.copyWith(fontSize: 18, fontWeight: FontWeight.w700),
        h3: body.copyWith(fontSize: 16, fontWeight: FontWeight.w700),
        h4: body.copyWith(fontWeight: FontWeight.w700),
        h5: body.copyWith(fontWeight: FontWeight.w700),
        h6: body.copyWith(fontWeight: FontWeight.w700),
        blockquote: body.copyWith(color: SpeakUpDesign.secondary),
        listBullet: body,
        listIndent: 20,
        blockSpacing: 8,
        blockquotePadding: const EdgeInsets.fromLTRB(10, 5, 8, 5),
        blockquoteDecoration: const BoxDecoration(
          color: SpeakUpDesign.surfaceMuted,
          border: Border(
            left: BorderSide(color: SpeakUpDesign.primary, width: 3),
          ),
        ),
        codeblockPadding: const EdgeInsets.all(10),
        codeblockDecoration: BoxDecoration(
          color: SpeakUpDesign.surfaceMuted,
          borderRadius: BorderRadius.circular(8),
        ),
      ),
      imageBuilder: (uri, title, alt) => Text(
        alt == null || alt.trim().isEmpty ? '[图片已隐藏]' : '[图片：$alt]',
        style: body.copyWith(color: SpeakUpDesign.secondary),
      ),
    );
  }
}
