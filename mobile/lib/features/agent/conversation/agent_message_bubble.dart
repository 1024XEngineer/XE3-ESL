import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/conversation_bubble_surface.dart';
import 'package:speakup/design/conversation_message_content.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/agent/handoff/practice_plan_handoff_card.dart';

import 'agent_models.dart';
import 'agent_message_audio_controller.dart';

final class AgentMessageBubble extends StatefulWidget {
  const AgentMessageBubble({
    required this.message,
    this.messageAudioController,
    this.onTranslate,
    this.onHandoff,
    this.onRefreshImage,
    this.correction,
    this.polish,
    this.feedbackNotice,
    super.key,
  });

  final AgentMessage message;
  final AgentMessageAudioController? messageAudioController;
  final Future<String> Function(AgentMessage message)? onTranslate;
  final ValueChanged<AgentHandoff>? onHandoff;
  final FutureOr<void> Function(String messageId, String imageAssetId)?
  onRefreshImage;
  final InlineLanguageSuggestion? correction;
  final InlineLanguageSuggestion? polish;
  final String? feedbackNotice;

  @override
  State<AgentMessageBubble> createState() => _AgentMessageBubbleState();
}

class _AgentMessageBubbleState extends State<AgentMessageBubble> {
  late _AgentMessageAudioSnapshot _audioSnapshot;
  bool _languageFeedbackExpanded = false;

  @override
  void initState() {
    super.initState();
    _audioSnapshot = _captureAudioSnapshot();
    widget.messageAudioController?.addListener(_handleAudioController);
  }

  @override
  void didUpdateWidget(covariant AgentMessageBubble oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.message.id != widget.message.id ||
        (widget.correction == null &&
            widget.polish == null &&
            widget.feedbackNotice == null)) {
      _languageFeedbackExpanded = false;
    }
    if (oldWidget.messageAudioController != widget.messageAudioController) {
      oldWidget.messageAudioController?.removeListener(_handleAudioController);
      widget.messageAudioController?.addListener(_handleAudioController);
    }
    _audioSnapshot = _captureAudioSnapshot();
  }

  @override
  void dispose() {
    widget.messageAudioController?.removeListener(_handleAudioController);
    super.dispose();
  }

  void _handleAudioController() {
    if (!mounted) {
      return;
    }
    final snapshot = _captureAudioSnapshot();
    if (snapshot == _audioSnapshot) {
      return;
    }
    _audioSnapshot = snapshot;
    setState(() {});
  }

  _AgentMessageAudioSnapshot _captureAudioSnapshot() {
    final audioController = widget.messageAudioController;
    final message = widget.message;
    final playing = audioController?.playingMessageId == message.id;
    return (
      loading: audioController?.loadingMessageId == message.id,
      playing: playing,
      deleting: audioController?.deletingMessageId == message.id,
      error: audioController?.errorMessageId == message.id
          ? audioController?.errorMessage
          : null,
      playbackPosition:
          message.modality == AgentMessageModality.voice && playing
          ? audioController?.playbackPosition ?? Duration.zero
          : Duration.zero,
      speechSpeed: message.role == AgentMessageRole.assistant
          ? audioController?.speechSpeed ?? 1
          : 1,
      usesPreview: audioController?.messagePlaybackUsesPreview ?? false,
    );
  }

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final isUser = message.role == AgentMessageRole.user;
    final voiceNeedsBottomInset =
        isUser &&
        message.modality == AgentMessageModality.voice &&
        (_languageFeedbackExpanded || _audioSnapshot.error != null);
    const foreground = SpeakUpDesign.ink;
    final primaryContent = switch (message.modality) {
      AgentMessageModality.voice => _buildUserVoice(context, foreground),
      AgentMessageModality.multimodal => _buildMultimodalMessage(
        context,
        foreground,
      ),
      AgentMessageModality.text => _buildTextMessage(),
    };
    return ConversationBubbleSurface(
      bubbleKey: Key('agent-message-${message.id}'),
      isUser: isUser,
      margin: const EdgeInsets.only(bottom: 7),
      padding: isUser && message.modality == AgentMessageModality.voice
          ? EdgeInsets.fromLTRB(14, 11, 12, voiceNeedsBottomInset ? 11 : 0)
          : null,
      child: message.handoffs.isEmpty
          ? primaryContent
          : Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                primaryContent,
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
    );
  }

  Widget _buildTextMessage() {
    final message = widget.message;
    final audioController = widget.messageAudioController;
    final loading = audioController?.loadingMessageId == message.id;
    final playing = audioController?.playingMessageId == message.id;
    final error = audioController?.errorMessageId == message.id
        ? audioController?.errorMessage
        : null;
    final actions = <Widget>[];
    if (audioController != null && message.role == AgentMessageRole.assistant) {
      actions.addAll([
        TextButton.icon(
          key: Key('agent-assistant-tts-${message.id}'),
          onPressed: message.isStreaming
              ? null
              : () => audioController.toggleMessagePlayback(message),
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
          onPressed: audioController.cycleSpeechSpeed,
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
          child: Text('${_formatSpeed(audioController.speechSpeed)}×'),
        ),
      ]);
    }
    return ConversationTextMessageContent(
      sourceId: message.id,
      text: message.text,
      isUser: message.role == AgentMessageRole.user,
      isStreaming: message.isStreaming,
      hasFailed: message.hasFailed,
      textKey: message.role == AgentMessageRole.assistant
          ? Key('agent-assistant-text-${message.id}')
          : null,
      streamingKey: const Key('agent-assistant-streaming'),
      translationButtonKey: Key('agent-assistant-translate-${message.id}'),
      translationContentKey: Key('agent-assistant-translation-${message.id}'),
      translationErrorKey: Key(
        'agent-assistant-translation-error-${message.id}',
      ),
      onTranslate: widget.onTranslate == null
          ? null
          : () => widget.onTranslate!(message),
      leadingActions: actions,
      mediaError: error,
      mediaErrorKey: Key('agent-message-media-error-${message.id}'),
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
    final audioController = widget.messageAudioController;
    final readable = audio.isReadable && audioController != null;
    final loading =
        audioController?.loadingMessageId == message.id &&
        audioController?.messagePlaybackUsesPreview == false;
    final playing =
        audioController?.playingMessageId == message.id &&
        audioController?.messagePlaybackUsesPreview == false;
    final previewLoading =
        audioController?.loadingMessageId == message.id &&
        audioController?.messagePlaybackUsesPreview == true;
    final previewPlaying =
        audioController?.playingMessageId == message.id &&
        audioController?.messagePlaybackUsesPreview == true;
    final error = audioController?.errorMessageId == message.id
        ? audioController?.errorMessage
        : null;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          message.text,
          key: Key('agent-user-voice-transcript-${message.id}'),
          style: TextStyle(color: foreground, fontSize: 16, height: 1.45),
        ),
        InlineLanguageFeedback(
          leading: _VoicePlaybackAction(
            key: Key('agent-user-voice-play-${message.id}'),
            loading: loading,
            playing: playing,
            duration: audio.duration,
            onPressed: readable
                ? () => audioController.toggleMessagePlayback(message)
                : null,
          ),
          correction: widget.correction,
          polish: widget.polish,
          feedbackNotice: widget.feedbackNotice,
          optimizeIconOnly: true,
          onExpandedChanged: (expanded) {
            if (_languageFeedbackExpanded == expanded) {
              return;
            }
            setState(() => _languageFeedbackExpanded = expanded);
          },
          suggestionLoading: previewLoading,
          suggestionPlaying: previewPlaying,
          onSpeakSuggestion: audioController == null
              ? null
              : (text) => audioController.toggleSpeechPreview(message, text),
        ),
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

class _VoicePlaybackAction extends StatelessWidget {
  const _VoicePlaybackAction({
    required this.loading,
    required this.playing,
    required this.duration,
    required this.onPressed,
    super.key,
  });

  final bool loading;
  final bool playing;
  final Duration duration;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      enabled: onPressed != null,
      label: playing ? '停止播放原声' : '播放原声，${_formatDuration(duration)}',
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

typedef _AgentMessageAudioSnapshot = ({
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
