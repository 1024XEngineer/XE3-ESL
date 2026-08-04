import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/agent/handoff/practice_plan_handoff_card.dart';

import 'agent_models.dart';
import 'agent_voice_controller.dart';

class AgentMessageBubble extends StatefulWidget {
  const AgentMessageBubble({
    required this.message,
    this.voiceController,
    this.onHandoff,
    this.onRefreshImage,
    this.polishedText,
    this.polishLoading = false,
    super.key,
  });

  final AgentMessage message;
  final AgentVoiceController? voiceController;
  final ValueChanged<AgentHandoff>? onHandoff;
  final FutureOr<void> Function(String messageId, String imageAssetId)?
  onRefreshImage;
  final String? polishedText;
  final bool polishLoading;

  @override
  State<AgentMessageBubble> createState() => _AgentMessageBubbleState();
}

class _AgentMessageBubbleState extends State<AgentMessageBubble> {
  late _AgentMessageVoiceSnapshot _voiceSnapshot;
  bool _polishExpanded = false;

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
      _polishExpanded = false;
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
      usesPreview: voice?.messagePlaybackUsesPreview ?? false,
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
        child: message.handoffs.isEmpty
            ? content
            : Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  content,
                  const SizedBox(height: 10),
                  for (final handoff in message.handoffs)
                    switch (handoff) {
                      ConfirmPracticePlanHandoff() => PracticePlanHandoffCard(
                        handoff: handoff,
                        onConfirm: widget.onHandoff == null
                            ? null
                            : () => widget.onHandoff!(handoff),
                      ),
                    },
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
    final loading =
        voice?.loadingMessageId == message.id &&
        voice?.messagePlaybackUsesPreview == false;
    final playing =
        voice?.playingMessageId == message.id &&
        voice?.messagePlaybackUsesPreview == false;
    final previewLoading =
        voice?.loadingMessageId == message.id &&
        voice?.messagePlaybackUsesPreview == true;
    final previewPlaying =
        voice?.playingMessageId == message.id &&
        voice?.messagePlaybackUsesPreview == true;
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
        Text(
          message.text,
          key: Key('agent-user-voice-transcript-${message.id}'),
          style: TextStyle(color: foreground, fontSize: 16, height: 1.45),
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            _VoiceTextAction(
              key: Key('agent-user-voice-play-${message.id}'),
              icon: playing ? Icons.stop_rounded : Icons.volume_up_rounded,
              loading: loading,
              label: '原声',
              onPressed: readable
                  ? () => voice.toggleMessagePlayback(message)
                  : null,
            ),
            const SizedBox(width: 4),
            Text(
              _formatDuration(audio.duration),
              key: Key('agent-user-voice-duration-${message.id}'),
              style: TextStyle(
                color: SpeakUpDesign.secondary,
                fontSize: 12,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(width: 4),
            _VoiceTextAction(
              key: Key('agent-user-voice-tts-${message.id}'),
              icon: previewPlaying
                  ? Icons.stop_rounded
                  : Icons.record_voice_over_rounded,
              loading: previewLoading,
              label: 'TTS',
              onPressed: voice == null || widget.polishedText == null
                  ? null
                  : () => voice.toggleSpeechPreview(
                      message,
                      widget.polishedText!,
                    ),
            ),
            const SizedBox(width: 4),
            _VoiceTextAction(
              key: Key('agent-user-voice-polish-${message.id}'),
              icon: Icons.auto_awesome_rounded,
              loading: widget.polishLoading,
              label: '润色',
              onPressed: widget.polishedText == null
                  ? null
                  : () => setState(() => _polishExpanded = !_polishExpanded),
            ),
            const Spacer(),
            if (!audio.isReadable)
              Text(
                audio.status == AgentMessageAudioStatus.deleting
                    ? '正在删除录音'
                    : '录音已删除',
                key: Key(
                  audio.status == AgentMessageAudioStatus.deleting
                      ? 'agent-user-voice-deleting-${message.id}'
                      : 'agent-user-voice-deleted-${message.id}',
                ),
                style: SpeakUpDesign.meta,
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
        if (_polishExpanded && widget.polishedText != null) ...[
          const SizedBox(height: 10),
          const Divider(height: 1),
          const SizedBox(height: 10),
          Text(
            widget.polishedText!,
            key: Key('agent-user-voice-polish-text-${message.id}'),
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

class _VoiceTextAction extends StatelessWidget {
  const _VoiceTextAction({
    required this.icon,
    required this.loading,
    required this.label,
    required this.onPressed,
    super.key,
  });

  final IconData icon;
  final bool loading;
  final String label;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return TextButton.icon(
      onPressed: onPressed,
      style: TextButton.styleFrom(
        foregroundColor: SpeakUpDesign.secondary,
        minimumSize: const Size(0, 32),
        padding: const EdgeInsets.symmetric(horizontal: 3),
        visualDensity: VisualDensity.compact,
      ),
      icon: loading
          ? const SizedBox.square(
              dimension: 14,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(icon, size: 17),
      label: Text(label, style: const TextStyle(fontSize: 12)),
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
  bool usesPreview,
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
