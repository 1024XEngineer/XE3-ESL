import 'package:flutter/material.dart';

import 'agent_models.dart';
import 'agent_voice_controller.dart';

class AgentMessageBubble extends StatefulWidget {
  const AgentMessageBubble({
    required this.message,
    this.voiceController,
    super.key,
  });

  final AgentMessage message;
  final AgentVoiceController? voiceController;

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
    const foreground = Color(0xFF25262A);
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
          color: isUser ? const Color(0xFFE7E7E3) : Colors.transparent,
          borderRadius: BorderRadius.circular(18),
          border: isUser ? Border.all(color: const Color(0xFFDCDCD7)) : null,
        ),
        child: message.modality == AgentMessageModality.voice
            ? _buildUserVoice(context, foreground)
            : _buildTextMessage(context, foreground),
      ),
    );
  }

  Widget _buildTextMessage(BuildContext context, Color foreground) {
    final message = widget.message;
    final voice = widget.voiceController;
    if (message.role == AgentMessageRole.user || voice == null) {
      return Text(
        message.text,
        style: TextStyle(color: foreground, fontSize: 15, height: 1.45),
      );
    }
    final loading = voice.loadingMessageId == message.id;
    final playing = voice.playingMessageId == message.id;
    final error = voice.mediaErrorMessageId == message.id
        ? voice.mediaErrorMessage
        : null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          message.text,
          key: Key('agent-assistant-text-${message.id}'),
          style: TextStyle(color: foreground, fontSize: 15, height: 1.48),
        ),
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
                foregroundColor: const Color(0xFF55575E),
                backgroundColor: const Color(0xFFE8E8E4),
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
                foregroundColor: const Color(0xFF66686F),
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
              color: Color(0xFF8B2E26),
              fontSize: 12,
              height: 1.35,
            ),
          ),
        ],
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
                backgroundColor: const Color(0xFFD6D6D1),
                foregroundColor: foreground,
                disabledBackgroundColor: const Color(0xFFDCDCD7),
                disabledForegroundColor: const Color(0xFF8A8B90),
                minimumSize: const Size.square(36),
                maximumSize: const Size.square(36),
                padding: EdgeInsets.zero,
              ),
              icon: loading
                  ? const SizedBox.square(
                      dimension: 15,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Color(0xFF55575E),
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
                style: const TextStyle(color: Color(0xFF6B6D73), fontSize: 13),
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
                color: const Color(0xFF686A70),
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
            backgroundColor: const Color(0xFFCECEC9),
            valueColor: const AlwaysStoppedAnimation<Color>(Color(0xFF55575E)),
          ),
        ],
        const SizedBox(height: 4),
        TextButton.icon(
          key: Key('agent-user-voice-transcript-toggle-${message.id}'),
          style: TextButton.styleFrom(
            foregroundColor: const Color(0xFF5F6167),
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
              color: Color(0xFF8B2E26),
              fontSize: 12,
              height: 1.35,
            ),
          ),
        ],
      ],
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
