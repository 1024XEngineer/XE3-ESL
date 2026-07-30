import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:speakup/design/speak_up_design.dart';

import 'agent_models.dart';
import 'agent_voice_controller.dart';

class AgentMessageBubble extends StatefulWidget {
  const AgentMessageBubble({
    required this.message,
    this.voiceController,
    this.onAction,
    this.onRefreshImage,
    super.key,
  });

  final AgentMessage message;
  final AgentVoiceController? voiceController;
  final ValueChanged<AgentMessageAction>? onAction;
  final FutureOr<void> Function(String messageId, String imageAssetId)?
  onRefreshImage;

  @override
  State<AgentMessageBubble> createState() => _AgentMessageBubbleState();
}

class _AgentMessageBubbleState extends State<AgentMessageBubble> {
  bool _transcriptExpanded = false;
  late _AgentMessageVoiceSnapshot _voiceSnapshot;

  @override
  void initState() {
    super.initState();
    _voiceSnapshot = _captureVoiceSnapshot();
    widget.voiceController?.addListener(_handleVoiceController);
  }

  @override
  void didUpdateWidget(covariant AgentMessageBubble oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.voiceController != widget.voiceController) {
      oldWidget.voiceController?.removeListener(_handleVoiceController);
      widget.voiceController?.addListener(_handleVoiceController);
    }
    if (oldWidget.message.id != widget.message.id) {
      _transcriptExpanded = false;
    }
    _voiceSnapshot = _captureVoiceSnapshot();
  }

  @override
  void dispose() {
    widget.voiceController?.removeListener(_handleVoiceController);
    super.dispose();
  }

  void _handleVoiceController() {
    if (!mounted) {
      return;
    }
    final snapshot = _captureVoiceSnapshot();
    if (snapshot == _voiceSnapshot) {
      return;
    }
    _voiceSnapshot = snapshot;
    setState(() {});
  }

  _AgentMessageVoiceSnapshot _captureVoiceSnapshot() {
    final voice = widget.voiceController;
    final message = widget.message;
    final playing = voice?.playingMessageId == message.id;
    return (
      loading: voice?.loadingMessageId == message.id,
      playing: playing,
      deleting: voice?.deletingMessageId == message.id,
      error: voice?.mediaErrorMessageId == message.id
          ? voice?.mediaErrorMessage
          : null,
      playbackPosition:
          message.modality == AgentMessageModality.voice && playing
          ? voice?.playbackPosition ?? Duration.zero
          : Duration.zero,
      speechSpeed: message.role == AgentMessageRole.assistant
          ? voice?.speechSpeed ?? 1
          : 1,
    );
  }

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final isUser = message.role == AgentMessageRole.user;
    const foreground = SpeakUpDesign.ink;
    final content = switch (message.modality) {
      AgentMessageModality.voice => _buildUserVoice(context, foreground),
      AgentMessageModality.multimodal => _buildMultimodalMessage(
        context,
        foreground,
      ),
      AgentMessageModality.text => _buildTextMessage(context, foreground),
    };
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        key: Key('agent-message-${message.id}'),
        constraints: const BoxConstraints(maxWidth: 340),
        margin: const EdgeInsets.only(bottom: 7),
        padding: isUser
            ? const EdgeInsets.fromLTRB(14, 11, 12, 11)
            : const EdgeInsets.fromLTRB(2, 7, 12, 9),
        decoration: BoxDecoration(
          color: isUser ? SpeakUpDesign.primaryMuted : Colors.transparent,
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
          border: isUser ? Border.all(color: SpeakUpDesign.border) : null,
        ),
        child: message.actions.isEmpty
            ? content
            : Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  content,
                  const SizedBox(height: 10),
                  for (final action in message.actions)
                    _InterviewPreparationAction(
                      action: action,
                      onPressed: widget.onAction == null
                          ? null
                          : () => widget.onAction!(action),
                    ),
                ],
              ),
      ),
    );
  }

  Widget _buildTextMessage(BuildContext context, Color foreground) {
    final message = widget.message;
    final voice = widget.voiceController;
    if (message.role == AgentMessageRole.user) {
      return Text(
        message.text,
        style: TextStyle(color: foreground, fontSize: 15, height: 1.45),
      );
    }
    final markdown = _AssistantMarkdown(
      key: Key('agent-assistant-text-${message.id}'),
      data: message.text,
      foreground: foreground,
    );
    if (message.isStreaming && message.text.isEmpty) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 6),
        child: SizedBox.square(
          key: Key('agent-assistant-streaming'),
          dimension: 16,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }
    if (message.isStreaming || message.hasFailed) {
      return markdown;
    }
    if (voice == null) {
      return markdown;
    }
    final loading = voice.loadingMessageId == message.id;
    final playing = voice.playingMessageId == message.id;
    final error = voice.mediaErrorMessageId == message.id
        ? voice.mediaErrorMessage
        : null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        markdown,
        const SizedBox(height: 6),
        Wrap(
          spacing: 4,
          runSpacing: 4,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            TextButton.icon(
              key: Key('agent-assistant-tts-${message.id}'),
              onPressed: () => voice.toggleMessagePlayback(message),
              style: TextButton.styleFrom(
                foregroundColor: SpeakUpDesign.primary,
                backgroundColor: SpeakUpDesign.surfaceMuted,
                minimumSize: const Size(0, 32),
                padding: const EdgeInsets.symmetric(horizontal: 10),
                visualDensity: VisualDensity.compact,
                textStyle: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              icon: loading
                  ? const SizedBox.square(
                      dimension: 14,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      playing ? Icons.stop_rounded : Icons.volume_up_outlined,
                      size: 17,
                    ),
              label: Text(
                loading
                    ? '加载中'
                    : playing
                    ? '停止朗读'
                    : error == null
                    ? '朗读'
                    : '重试朗读',
              ),
            ),
            TextButton(
              key: Key('agent-assistant-speed-${message.id}'),
              onPressed: voice.cycleSpeechSpeed,
              style: TextButton.styleFrom(
                foregroundColor: SpeakUpDesign.secondary,
                minimumSize: const Size(0, 32),
                padding: const EdgeInsets.symmetric(horizontal: 8),
                visualDensity: VisualDensity.compact,
                textStyle: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              child: Text('${_formatSpeed(voice.speechSpeed)}×'),
            ),
          ],
        ),
        if (error != null) ...[
          const SizedBox(height: 3),
          Text(
            error,
            key: Key('agent-message-media-error-${message.id}'),
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

  Widget _buildMultimodalMessage(BuildContext context, Color foreground) {
    final message = widget.message;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final image in message.images)
              _MessageImageThumbnail(
                key: Key('agent-message-image-${image.id}'),
                image: image,
                onRefresh: widget.onRefreshImage == null
                    ? null
                    : () => widget.onRefreshImage!(message.id, image.id),
              ),
          ],
        ),
        const SizedBox(height: 8),
        Text(
          message.text,
          style: TextStyle(color: foreground, fontSize: 15, height: 1.45),
        ),
      ],
    );
  }

  Widget _buildUserVoice(BuildContext context, Color foreground) {
    final message = widget.message;
    final audio = message.audio!;
    final voice = widget.voiceController;
    final readable = audio.isReadable && voice != null;
    final loading = voice?.loadingMessageId == message.id;
    final playing = voice?.playingMessageId == message.id;
    final deleting =
        voice?.deletingMessageId == message.id ||
        audio.status == AgentMessageAudioStatus.deleting;
    final progress = voice?.playbackProgressFor(message) ?? 0;
    final error = voice?.mediaErrorMessageId == message.id
        ? voice?.mediaErrorMessage
        : null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            IconButton(
              key: Key('agent-user-voice-play-${message.id}'),
              tooltip: readable
                  ? playing
                        ? '停止播放录音'
                        : '播放录音'
                  : '录音不可用',
              onPressed: readable
                  ? () => voice.toggleMessagePlayback(message)
                  : null,
              style: IconButton.styleFrom(
                backgroundColor: SpeakUpDesign.surfaceMuted,
                foregroundColor: foreground,
                disabledBackgroundColor: SpeakUpDesign.surfaceMuted,
                disabledForegroundColor: SpeakUpDesign.tertiary,
                minimumSize: const Size.square(36),
                maximumSize: const Size.square(36),
                padding: EdgeInsets.zero,
              ),
              icon: loading
                  ? const SizedBox.square(
                      dimension: 15,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: SpeakUpDesign.secondary,
                      ),
                    )
                  : Icon(
                      playing ? Icons.stop_rounded : Icons.play_arrow_rounded,
                      size: 21,
                    ),
            ),
            const SizedBox(width: 9),
            Text(
              _formatDuration(audio.duration),
              key: Key('agent-user-voice-duration-${message.id}'),
              style: TextStyle(
                color: foreground,
                fontSize: 14,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                playing
                    ? '正在播放'
                    : audio.isReadable
                    ? '语音消息'
                    : audio.status == AgentMessageAudioStatus.deleting
                    ? '正在删除录音'
                    : '录音已删除',
                key: !audio.isReadable
                    ? Key(
                        audio.status == AgentMessageAudioStatus.deleting
                            ? 'agent-user-voice-deleting-${message.id}'
                            : 'agent-user-voice-deleted-${message.id}',
                      )
                    : null,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: SpeakUpDesign.meta,
              ),
            ),
            if (!audio.isReadable) const SizedBox(width: 2),
            if (audio.status != AgentMessageAudioStatus.deleted &&
                voice != null)
              IconButton(
                key: Key('agent-user-voice-delete-${message.id}'),
                tooltip: deleting ? '正在删除录音' : '删除录音',
                onPressed: deleting
                    ? null
                    : () => voice.deleteMessageAudio(message),
                constraints: const BoxConstraints.tightFor(
                  width: 36,
                  height: 36,
                ),
                padding: EdgeInsets.zero,
                visualDensity: VisualDensity.compact,
                color: SpeakUpDesign.secondary,
                icon: deleting
                    ? const SizedBox.square(
                        dimension: 14,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.delete_outline_rounded, size: 19),
              ),
          ],
        ),
        if (audio.isReadable) ...[
          const SizedBox(height: 7),
          LinearProgressIndicator(
            key: Key('agent-user-voice-progress-${message.id}'),
            value: playing ? progress : 0,
            minHeight: 2,
            borderRadius: BorderRadius.circular(1),
            backgroundColor: SpeakUpDesign.border,
            valueColor: const AlwaysStoppedAnimation<Color>(
              SpeakUpDesign.primary,
            ),
          ),
        ],
        const SizedBox(height: 4),
        TextButton.icon(
          key: Key('agent-user-voice-transcript-toggle-${message.id}'),
          style: TextButton.styleFrom(
            foregroundColor: SpeakUpDesign.secondary,
            minimumSize: const Size(0, 30),
            padding: const EdgeInsets.symmetric(horizontal: 2),
            visualDensity: VisualDensity.compact,
            textStyle: const TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w600,
            ),
          ),
          onPressed: () {
            setState(() => _transcriptExpanded = !_transcriptExpanded);
          },
          icon: Icon(
            _transcriptExpanded
                ? Icons.expand_less_rounded
                : Icons.expand_more_rounded,
            size: 18,
          ),
          label: const Text('转写'),
        ),
        if (_transcriptExpanded) ...[
          const SizedBox(height: 2),
          Text(
            message.text,
            key: Key('agent-user-voice-transcript-${message.id}'),
            style: TextStyle(color: foreground, fontSize: 15, height: 1.45),
          ),
        ],
        if (error != null) ...[
          const SizedBox(height: 4),
          Text(
            error,
            key: Key('agent-message-media-error-${message.id}'),
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
}

class _MessageImageThumbnail extends StatelessWidget {
  const _MessageImageThumbnail({
    required this.image,
    required this.onRefresh,
    super.key,
  });

  final AgentImageAsset image;
  final FutureOr<void> Function()? onRefresh;

  @override
  Widget build(BuildContext context) {
    final url = image.isReadable ? image.contentUrl : null;
    final placeholder = Container(
      width: 104,
      height: 104,
      color: SpeakUpDesign.surfaceMuted,
      alignment: Alignment.center,
      child: IconButton(
        tooltip: onRefresh == null ? '图片不可用' : '重新加载图片',
        onPressed: onRefresh == null ? null : () => onRefresh!(),
        icon: const Icon(Icons.broken_image_outlined),
      ),
    );
    return ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: url == null
          ? placeholder
          : InkWell(
              onTap: () => showDialog<void>(
                context: context,
                builder: (context) => Dialog(
                  backgroundColor: Colors.black,
                  insetPadding: const EdgeInsets.all(16),
                  child: InteractiveViewer(
                    maxScale: 5,
                    child: Image.network(
                      url.toString(),
                      fit: BoxFit.contain,
                      errorBuilder: (_, _, _) => placeholder,
                    ),
                  ),
                ),
              ),
              child: Image.network(
                url.toString(),
                width: 104,
                height: 104,
                fit: BoxFit.cover,
                errorBuilder: (_, _, _) => placeholder,
              ),
            ),
    );
  }
}

class _InterviewPreparationAction extends StatelessWidget {
  const _InterviewPreparationAction({
    required this.action,
    required this.onPressed,
  });

  final AgentMessageAction action;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SpeakUpDesign.surface,
      shape: RoundedRectangleBorder(
        side: const BorderSide(color: SpeakUpDesign.border),
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: Key('agent-action-interview-${action.matterId}'),
        onTap: onPressed,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 12, 10, 12),
          child: Row(
            children: [
              const Icon(
                Icons.work_outline_rounded,
                size: 22,
                color: SpeakUpDesign.primary,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      action.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: SpeakUpDesign.cardTitle,
                    ),
                    const SizedBox(height: 2),
                    Text(action.label, style: SpeakUpDesign.meta),
                  ],
                ),
              ),
              const Icon(
                Icons.chevron_right_rounded,
                color: SpeakUpDesign.secondary,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _AssistantMarkdown extends StatelessWidget {
  const _AssistantMarkdown({
    required this.data,
    required this.foreground,
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

typedef _AgentMessageVoiceSnapshot = ({
  bool loading,
  bool playing,
  bool deleting,
  String? error,
  Duration playbackPosition,
  double speechSpeed,
});

String _formatDuration(Duration value) {
  final totalSeconds = value.inSeconds.clamp(0, 3599);
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes:${seconds.toString().padLeft(2, '0')}';
}

String _formatSpeed(double value) {
  return value == value.roundToDouble()
      ? value.toStringAsFixed(0)
      : value.toStringAsFixed(2).replaceFirst(RegExp(r'0$'), '');
}
