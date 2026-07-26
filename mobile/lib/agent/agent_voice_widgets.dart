import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'agent_models.dart';
import 'agent_voice_controller.dart';
import 'agent_voice_models.dart';

class AgentVoiceComposerPanel extends StatefulWidget {
  const AgentVoiceComposerPanel({required this.controller, super.key});

  final AgentVoiceController controller;

  @override
  State<AgentVoiceComposerPanel> createState() =>
      _AgentVoiceComposerPanelState();
}

class _AgentVoiceComposerPanelState extends State<AgentVoiceComposerPanel> {
  final _transcriptController = TextEditingController();
  bool _updatingTranscript = false;

  @override
  void initState() {
    super.initState();
    _transcriptController.text = widget.controller.editedTranscript;
    _transcriptController.addListener(_handleTranscript);
    widget.controller.addListener(_handleController);
  }

  @override
  void didUpdateWidget(covariant AgentVoiceComposerPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller.removeListener(_handleController);
      widget.controller.addListener(_handleController);
    }
    _syncTranscript();
  }

  @override
  void dispose() {
    widget.controller.removeListener(_handleController);
    _transcriptController
      ..removeListener(_handleTranscript)
      ..dispose();
    super.dispose();
  }

  void _handleController() {
    if (!mounted) {
      return;
    }
    _syncTranscript();
    setState(() {});
  }

  void _syncTranscript() {
    final value = widget.controller.editedTranscript;
    if (_transcriptController.text == value) {
      return;
    }
    _updatingTranscript = true;
    _transcriptController.value = TextEditingValue(
      text: value,
      selection: TextSelection.collapsed(offset: value.length),
    );
    _updatingTranscript = false;
  }

  void _handleTranscript() {
    if (!_updatingTranscript) {
      widget.controller.updateTranscript(_transcriptController.text);
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final state = controller.state;
    final largeText = MediaQuery.textScalerOf(context).scale(1) > 1.25;
    return ClipRRect(
      borderRadius: BorderRadius.circular(28),
      child: BackdropFilter(
        filter: ui.ImageFilter.blur(sigmaX: 24, sigmaY: 24),
        child: Container(
          key: const Key('agent-voice-composer-panel'),
          padding: const EdgeInsets.fromLTRB(12, 10, 10, 10),
          decoration: BoxDecoration(
            color: const Color(0xEFFFFFFF),
            borderRadius: BorderRadius.circular(28),
            border: Border.all(color: Colors.white),
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (state != AgentVoiceComposerState.awaitingConfirmation)
                Row(
                  children: [
                    IconButton(
                      key: const Key('agent-voice-cancel'),
                      tooltip: '取消语音输入',
                      onPressed: state == AgentVoiceComposerState.confirming
                          ? null
                          : controller.cancel,
                      icon: const Icon(Icons.close_rounded),
                    ),
                    const SizedBox(width: 2),
                    if (state == AgentVoiceComposerState.recording) ...[
                      const _RecordingPulse(),
                      const SizedBox(width: 10),
                    ] else if (_isVoiceProgressState(state)) ...[
                      const SizedBox.square(
                        dimension: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                      const SizedBox(width: 10),
                    ],
                    Expanded(
                      child: Text(
                        state == AgentVoiceComposerState.recording
                            ? '正在聆听  ${_formatDuration(controller.recordingElapsed)}'
                            : _stateLabel(controller),
                        key: const Key('agent-voice-state-label'),
                        style: const TextStyle(
                          color: Color(0xFF44464D),
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    if (state == AgentVoiceComposerState.recording)
                      IconButton.filled(
                        key: const Key('agent-voice-stop'),
                        tooltip: '结束录音并自动转写',
                        onPressed: () async {
                          await controller.stopRecording();
                          await controller.upload();
                        },
                        icon: const Icon(Icons.stop_rounded),
                      ),
                  ],
                ),
              if (state == AgentVoiceComposerState.awaitingConfirmation) ...[
                Row(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    IconButton(
                      key: const Key('agent-voice-cancel'),
                      tooltip: '取消语音输入',
                      onPressed: controller.cancel,
                      icon: const Icon(Icons.close_rounded),
                    ),
                    Expanded(
                      child: TextField(
                        key: const Key('agent-voice-transcript-field'),
                        controller: _transcriptController,
                        minLines: largeText ? 2 : 1,
                        maxLines: 4,
                        inputFormatters: <TextInputFormatter>[
                          _agentVoiceContentFormatter,
                        ],
                        decoration: const InputDecoration(
                          hintText: '检查识别文字',
                          border: InputBorder.none,
                          isDense: true,
                          contentPadding: EdgeInsets.fromLTRB(8, 8, 8, 10),
                        ),
                      ),
                    ),
                    IconButton.filled(
                      key: const Key('agent-voice-confirm'),
                      tooltip: '发送',
                      onPressed: controller.canConfirm
                          ? controller.confirm
                          : null,
                      icon: const Icon(Icons.arrow_upward_rounded),
                    ),
                  ],
                ),
              ],
              if (state == AgentVoiceComposerState.recorded ||
                  (state == AgentVoiceComposerState.failed &&
                      controller.recording != null)) ...[
                const SizedBox(height: 6),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    key: const Key('agent-voice-upload'),
                    onPressed: controller.canUpload ? controller.upload : null,
                    child: const Text('重新尝试转写'),
                  ),
                ),
              ],
              if (controller.errorMessage case final error?) ...[
                const SizedBox(height: 6),
                Text(
                  error,
                  key: const Key('agent-voice-error'),
                  style: const TextStyle(
                    color: Color(0xFF8B2E26),
                    height: 1.35,
                  ),
                ),
                if (controller.canRetry) ...[
                  const SizedBox(height: 6),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      key: const Key('agent-voice-retry'),
                      onPressed: controller.retry,
                      icon: const Icon(Icons.refresh_rounded),
                      label: const Text('重试'),
                    ),
                  ),
                ],
              ],
            ],
          ),
        ),
      ),
    );
  }
}

bool _isVoiceProgressState(AgentVoiceComposerState state) {
  return state == AgentVoiceComposerState.starting ||
      state == AgentVoiceComposerState.uploading ||
      state == AgentVoiceComposerState.transcribing ||
      state == AgentVoiceComposerState.confirming ||
      state == AgentVoiceComposerState.awaitingAssistant;
}

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

  @override
  void initState() {
    super.initState();
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
  }

  @override
  void dispose() {
    widget.voiceController?.removeListener(_handleVoiceController);
    super.dispose();
  }

  void _handleVoiceController() {
    if (mounted) {
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final isUser = message.role == AgentMessageRole.user;
    final foreground = isUser ? Colors.white : const Color(0xFF202124);
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        key: Key('agent-message-${message.id}'),
        constraints: const BoxConstraints(maxWidth: 330),
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 13),
        decoration: BoxDecoration(
          color: isUser ? const Color(0xFF202124) : Colors.white,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(
            color: isUser ? const Color(0xFF202124) : const Color(0xFFE6E6E2),
          ),
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
        style: TextStyle(color: foreground, height: 1.45),
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
          style: TextStyle(color: foreground, height: 1.45),
        ),
        const SizedBox(height: 10),
        const Divider(height: 1),
        const SizedBox(height: 7),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            TextButton.icon(
              key: Key('agent-assistant-tts-${message.id}'),
              onPressed: () => voice.toggleMessagePlayback(message),
              icon: loading
                  ? const SizedBox.square(
                      dimension: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      playing ? Icons.stop_rounded : Icons.volume_up_outlined,
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
              child: Text('${_formatSpeed(voice.speechSpeed)}×'),
            ),
          ],
        ),
        if (error != null)
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
        Wrap(
          spacing: 6,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
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
                backgroundColor: const Color(0x2EFFFFFF),
                foregroundColor: foreground,
              ),
              icon: loading
                  ? const SizedBox.square(
                      dimension: 17,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Colors.white,
                      ),
                    )
                  : Icon(
                      playing ? Icons.stop_rounded : Icons.play_arrow_rounded,
                    ),
            ),
            Text(
              _formatDuration(audio.duration),
              key: Key('agent-user-voice-duration-${message.id}'),
              style: TextStyle(color: foreground),
            ),
            if (!audio.isReadable)
              Text(
                audio.status == AgentMessageAudioStatus.deleting
                    ? '录音删除中'
                    : '录音已删除',
                key: Key(
                  audio.status == AgentMessageAudioStatus.deleting
                      ? 'agent-user-voice-deleting-${message.id}'
                      : 'agent-user-voice-deleted-${message.id}',
                ),
                style: const TextStyle(color: Color(0xFFCACBCD)),
              ),
          ],
        ),
        if (audio.isReadable) ...[
          const SizedBox(height: 6),
          LinearProgressIndicator(
            key: Key('agent-user-voice-progress-${message.id}'),
            value: playing ? progress : 0,
            minHeight: 3,
            borderRadius: BorderRadius.circular(2),
            backgroundColor: const Color(0x3DFFFFFF),
            valueColor: const AlwaysStoppedAnimation<Color>(Colors.white),
          ),
        ],
        const SizedBox(height: 7),
        TextButton.icon(
          key: Key('agent-user-voice-transcript-toggle-${message.id}'),
          style: TextButton.styleFrom(
            foregroundColor: const Color(0xFFE7E7E8),
            padding: EdgeInsets.zero,
            visualDensity: VisualDensity.compact,
          ),
          onPressed: () {
            setState(() => _transcriptExpanded = !_transcriptExpanded);
          },
          icon: Icon(
            _transcriptExpanded
                ? Icons.expand_less_rounded
                : Icons.expand_more_rounded,
          ),
          label: const Text('确认文字'),
        ),
        if (_transcriptExpanded)
          Text(
            message.text,
            key: Key('agent-user-voice-transcript-${message.id}'),
            style: TextStyle(color: foreground, height: 1.45),
          ),
        if (audio.status != AgentMessageAudioStatus.deleted && voice != null)
          Align(
            alignment: Alignment.centerRight,
            child: TextButton.icon(
              key: Key('agent-user-voice-delete-${message.id}'),
              style: TextButton.styleFrom(
                foregroundColor: const Color(0xFFE7E7E8),
                visualDensity: VisualDensity.compact,
              ),
              onPressed: deleting
                  ? null
                  : () => voice.deleteMessageAudio(message),
              icon: deleting
                  ? const SizedBox.square(
                      dimension: 15,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Colors.white,
                      ),
                    )
                  : const Icon(Icons.delete_outline_rounded),
              label: Text(deleting ? '删除中' : '删除录音'),
            ),
          ),
        if (error != null)
          Text(
            error,
            key: Key('agent-message-media-error-${message.id}'),
            style: const TextStyle(
              color: Color(0xFFFFC5BE),
              fontSize: 12,
              height: 1.35,
            ),
          ),
      ],
    );
  }
}

class _RecordingPulse extends StatelessWidget {
  const _RecordingPulse();

  @override
  Widget build(BuildContext context) {
    return const SizedBox.square(
      dimension: 12,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: Color(0xFFC83D32),
          shape: BoxShape.circle,
        ),
      ),
    );
  }
}

String _stateLabel(AgentVoiceController controller) {
  return switch (controller.state) {
    AgentVoiceComposerState.idle => '语音消息',
    AgentVoiceComposerState.starting => '正在打开麦克风…',
    AgentVoiceComposerState.recording => '正在录音',
    AgentVoiceComposerState.recorded => '录音完成',
    AgentVoiceComposerState.uploading => '正在上传录音…',
    AgentVoiceComposerState.transcribing => '正在识别语音…',
    AgentVoiceComposerState.awaitingConfirmation => '编辑并确认识别文字',
    AgentVoiceComposerState.confirming => '正在确认语音消息…',
    AgentVoiceComposerState.awaitingAssistant => '消息已发送，Agent 正在回复…',
    AgentVoiceComposerState.failed => '语音消息需要处理',
  };
}

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

final TextInputFormatter _agentVoiceContentFormatter =
    TextInputFormatter.withFunction((oldValue, newValue) {
      final text = newValue.text;
      return text.runes.length <= 4096 && utf8.encode(text).length <= 16384
          ? newValue
          : oldValue;
    });
