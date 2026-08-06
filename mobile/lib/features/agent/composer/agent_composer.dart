import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/design/voice_composer_dock.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/composer/pending_image_strip.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_composer.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';

typedef AgentComposerAction = FutureOr<void> Function();

class AgentComposer extends StatefulWidget {
  const AgentComposer({
    required this.threadId,
    required this.draftThreadRecoveryGeneration,
    required this.keyboardVisible,
    required this.acceptedUserMessageId,
    required this.acceptedUserMessageText,
    required this.onStartVoice,
    required this.voiceController,
    required this.voiceEnabled,
    required this.onSubmitText,
    required this.enabled,
    required this.isBusy,
    required this.pendingImages,
    required this.imageErrorMessage,
    required this.imageSelectionInFlight,
    required this.onPickImages,
    required this.onTakePhoto,
    required this.onRemovePendingImage,
    required this.onRetryPendingImage,
    super.key,
  });

  final String? threadId;
  final int draftThreadRecoveryGeneration;
  final bool keyboardVisible;
  final String? acceptedUserMessageId;
  final String? acceptedUserMessageText;
  final AgentComposerAction? onStartVoice;
  final AgentVoiceController? voiceController;
  final bool voiceEnabled;
  final Future<bool> Function(String)? onSubmitText;
  final bool enabled;
  final bool isBusy;
  final List<AgentPendingImage> pendingImages;
  final String? imageErrorMessage;
  final bool imageSelectionInFlight;
  final AgentComposerAction? onPickImages;
  final AgentComposerAction? onTakePhoto;
  final AgentComposerPendingImageAction? onRemovePendingImage;
  final AgentComposerPendingImageAction? onRetryPendingImage;

  @override
  State<AgentComposer> createState() => _AgentComposerState();
}

class _AgentComposerState extends State<AgentComposer> {
  final _controller = TextEditingController();
  final _focusNode = FocusNode();
  bool _suppressControllerNotifications = false;
  bool _textSubmissionInFlight = false;
  bool _draftMaterializationPending = false;
  bool _voiceMaterializationPending = false;
  bool _textMode = false;
  String _draftBeforeVoice = '';

  @override
  void initState() {
    super.initState();
    _controller.addListener(_handleTextChanged);
    widget.voiceController?.addListener(_handleVoiceChanged);
  }

  @override
  void didUpdateWidget(covariant AgentComposer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.voiceController != widget.voiceController) {
      oldWidget.voiceController?.removeListener(_handleVoiceChanged);
      widget.voiceController?.addListener(_handleVoiceChanged);
    }
    if (oldWidget.threadId != widget.threadId) {
      final preserveMaterializedDraft =
          oldWidget.threadId == null &&
          widget.threadId != null &&
          (_draftMaterializationPending ||
              _voiceMaterializationPending ||
              oldWidget.draftThreadRecoveryGeneration !=
                  widget.draftThreadRecoveryGeneration);
      _draftMaterializationPending = false;
      _voiceMaterializationPending = false;
      if (!preserveMaterializedDraft && _controller.text.isNotEmpty) {
        _draftBeforeVoice = '';
        _suppressControllerNotifications = true;
        _controller.clear();
        _suppressControllerNotifications = false;
        _textMode = false;
      }
    }
    if (widget.acceptedUserMessageId != null &&
        widget.acceptedUserMessageId != oldWidget.acceptedUserMessageId &&
        _controller.text.trim() == widget.acceptedUserMessageText) {
      _suppressControllerNotifications = true;
      _controller.clear();
      _suppressControllerNotifications = false;
    }
  }

  @override
  void dispose() {
    widget.voiceController?.removeListener(_handleVoiceChanged);
    _controller
      ..removeListener(_handleTextChanged)
      ..dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _handleTextChanged() {
    if (!_suppressControllerNotifications) {
      final voice = widget.voiceController;
      if (voice?.state == AgentVoiceComposerState.awaitingConfirmation) {
        voice?.updateTranscript(_controller.text);
      }
      setState(() {});
    }
  }

  void _handleVoiceChanged() {
    if (!mounted) {
      return;
    }
    final voice = widget.voiceController;
    if (voice?.state == AgentVoiceComposerState.awaitingConfirmation &&
        _controller.text != voice?.editedTranscript) {
      _suppressControllerNotifications = true;
      _controller.value = TextEditingValue(
        text: voice!.editedTranscript,
        selection: TextSelection.collapsed(
          offset: voice.editedTranscript.length,
        ),
      );
      _suppressControllerNotifications = false;
    }
    if (voice?.state == AgentVoiceComposerState.awaitingConfirmation) {
      _textMode = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _focusNode.requestFocus();
        }
      });
    }
    setState(() {});
  }

  Future<void> _startVoice() async {
    _draftBeforeVoice = _controller.text;
    _voiceMaterializationPending = widget.threadId == null;
    try {
      await widget.onStartVoice?.call();
    } finally {
      if (mounted && _voiceMaterializationPending) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && widget.threadId == null) {
            _voiceMaterializationPending = false;
          }
        });
      }
    }
  }

  Future<void> _sendVoiceMessage() async {
    final voice = widget.voiceController;
    if (voice == null) {
      return;
    }
    await voice.stopRecordingAndUpload();
    if (!mounted || !voice.canConfirm) {
      return;
    }
    await voice.confirm();
    if (!mounted || voice.editedTranscript.isNotEmpty) {
      return;
    }
    _replaceComposerText(_draftBeforeVoice);
  }

  Future<void> _convertVoiceToText() async {
    final voice = widget.voiceController;
    if (voice == null) {
      return;
    }
    await voice.stopRecordingAndUpload();
  }

  Future<void> _cancelVoice() async {
    final voice = widget.voiceController;
    if (voice == null ||
        voice.state == AgentVoiceComposerState.confirming ||
        voice.state == AgentVoiceComposerState.awaitingAssistant) {
      return;
    }
    await voice.cancel();
    if (!mounted) {
      return;
    }
    _replaceComposerText(_draftBeforeVoice);
  }

  void _replaceComposerText(String value) {
    _suppressControllerNotifications = true;
    _controller.value = TextEditingValue(
      text: value,
      selection: TextSelection.collapsed(offset: value.length),
    );
    _suppressControllerNotifications = false;
    setState(() => _textMode = value.isNotEmpty);
    if (value.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _focusNode.requestFocus();
        }
      });
    }
  }

  void _showTextComposer() {
    setState(() => _textMode = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _focusNode.requestFocus();
      }
    });
  }

  void _showVoiceComposer() {
    _focusNode.unfocus();
    setState(() => _textMode = false);
  }

  Future<void> _submitConvertedText() async {
    final voice = widget.voiceController;
    final text = _controller.text.trim();
    if (voice == null ||
        !voice.canConfirm ||
        text.isEmpty ||
        _textSubmissionInFlight ||
        widget.onSubmitText == null) {
      return;
    }
    setState(() => _textSubmissionInFlight = true);
    try {
      await voice.cancel();
      final sent = await widget.onSubmitText!(text);
      if (mounted && sent) {
        _controller.clear();
        setState(() => _textMode = false);
      }
    } finally {
      if (mounted) {
        setState(() => _textSubmissionInFlight = false);
      }
    }
  }

  Future<void> _submit() async {
    final text = _controller.text.trim();
    final imageUploadPending = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.uploading,
    );
    final imageUploadFailed = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.failed,
    );
    if (!widget.enabled ||
        text.isEmpty ||
        imageUploadPending ||
        imageUploadFailed ||
        widget.isBusy ||
        _textSubmissionInFlight ||
        widget.onSubmitText == null) {
      return;
    }
    setState(() => _textSubmissionInFlight = true);
    _draftMaterializationPending = widget.threadId == null;
    try {
      final sent = await widget.onSubmitText!(text);
      if (mounted && sent) {
        _controller.clear();
      }
    } finally {
      if (mounted) {
        setState(() => _textSubmissionInFlight = false);
        if (_draftMaterializationPending) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted && widget.threadId == null) {
              _draftMaterializationPending = false;
            }
          });
        }
      }
    }
  }

  Future<void> _showImageSource() async {
    if (widget.onPickImages == null && widget.onTakePhoto == null) {
      return;
    }
    final source = await showModalBottomSheet<_AgentImageSource>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (widget.onPickImages != null)
              ListTile(
                key: const Key('agent-image-source-gallery'),
                leading: const Icon(Icons.photo_library_outlined),
                title: const Text('从相册选择'),
                onTap: () =>
                    Navigator.of(context).pop(_AgentImageSource.gallery),
              ),
            if (widget.onTakePhoto != null)
              ListTile(
                key: const Key('agent-image-source-camera'),
                leading: const Icon(Icons.photo_camera_outlined),
                title: const Text('拍照'),
                onTap: () =>
                    Navigator.of(context).pop(_AgentImageSource.camera),
              ),
          ],
        ),
      ),
    );
    if (!mounted) {
      return;
    }
    switch (source) {
      case _AgentImageSource.gallery:
        await widget.onPickImages?.call();
      case _AgentImageSource.camera:
        await widget.onTakePhoto?.call();
      case null:
        return;
    }
  }

  @override
  Widget build(BuildContext context) {
    final voice = widget.voiceController;
    final voiceState = voice?.state ?? AgentVoiceComposerState.idle;
    final starting = voiceState == AgentVoiceComposerState.starting;
    final recording = voiceState == AgentVoiceComposerState.recording;
    final confirmingText =
        voiceState == AgentVoiceComposerState.awaitingConfirmation;
    final voiceProgress =
        voiceState == AgentVoiceComposerState.uploading ||
        voiceState == AgentVoiceComposerState.transcribing ||
        voiceState == AgentVoiceComposerState.confirming ||
        voiceState == AgentVoiceComposerState.awaitingAssistant;
    final voiceSubmissionInFlight =
        voiceState == AgentVoiceComposerState.confirming ||
        voiceState == AgentVoiceComposerState.awaitingAssistant;
    final voiceFailure = voiceState == AgentVoiceComposerState.failed;
    final capturePhase = switch (voiceState) {
      AgentVoiceComposerState.idle => VoiceCapturePhase.idle,
      AgentVoiceComposerState.starting => VoiceCapturePhase.starting,
      AgentVoiceComposerState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    final voiceCaptureEnabled =
        starting ||
        recording ||
        (widget.onStartVoice != null &&
            widget.voiceEnabled &&
            widget.enabled &&
            !widget.isBusy);
    final showTextComposer =
        confirmingText ||
        (_textMode &&
            !starting &&
            !recording &&
            !voiceProgress &&
            !voiceFailure);
    final imageUploadPending = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.uploading,
    );
    final imageUploadFailed = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.failed,
    );

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.pendingImages.isNotEmpty) ...[
          AgentPendingImageStrip(
            images: widget.pendingImages,
            onRemove: widget.onRemovePendingImage,
            onRetry: widget.onRetryPendingImage,
          ),
          const SizedBox(height: 8),
        ],
        if (widget.imageErrorMessage case final error?) ...[
          Text(
            error,
            key: const Key('agent-image-error'),
            style: const TextStyle(
              color: SpeakUpDesign.error,
              fontSize: 12,
              height: 1.35,
            ),
          ),
          const SizedBox(height: 8),
        ],
        VoiceCaptureControl(
          phase: capturePhase,
          enabled: voiceCaptureEnabled,
          onStart: _startVoice,
          onSendVoice: _sendVoiceMessage,
          onConvertToText: _convertVoiceToText,
          onCancel: _cancelVoice,
          upwardCancelOnly: true,
          builder: (context, capture) => ConversationComposerCapsule(
            key: const Key('agent-composer-surface'),
            minHeight: widget.keyboardVisible ? 52 : 54,
            child: voiceProgress || voiceFailure
                ? AgentComposerVoiceStatusDock(
                    state: voiceState,
                    message: voiceFailure
                        ? voice?.errorMessage ?? '语音识别失败'
                        : voiceState == AgentVoiceComposerState.transcribing &&
                              voice?.liveTranscript.trim().isNotEmpty == true
                        ? voice!.liveTranscript
                        : agentComposerVoiceStateLabel(voiceState),
                    canCancel: !voiceSubmissionInFlight,
                    canRetry: voiceFailure && voice?.canRetry == true,
                    onCancel: _cancelVoice,
                    onRetry: voice?.retry,
                  )
                : showTextComposer
                ? _AgentTextDock(
                    controller: _controller,
                    focusNode: _focusNode,
                    keyboardVisible: widget.keyboardVisible,
                    enabled: confirmingText || widget.enabled,
                    confirmingConvertedText: confirmingText,
                    submitting: _textSubmissionInFlight,
                    canSubmitConvertedText: voice?.canConfirm == true,
                    canSubmitText:
                        widget.onSubmitText != null &&
                        widget.enabled &&
                        !widget.isBusy &&
                        !imageUploadPending &&
                        !imageUploadFailed,
                    onReturnToVoice: confirmingText
                        ? _cancelVoice
                        : _showVoiceComposer,
                    onSubmit: confirmingText ? _submitConvertedText : _submit,
                  )
                : AgentComposerVoiceDock(
                    capture: capture,
                    phase: capturePhase,
                    elapsed: voice?.recordingElapsed ?? Duration.zero,
                    enabled: voiceCaptureEnabled,
                    textEnabled: widget.enabled,
                    canAddImages:
                        widget.onPickImages != null ||
                        widget.onTakePhoto != null,
                    onAddImages: _showImageSource,
                    onShowText: _showTextComposer,
                  ),
          ),
        ),
      ],
    );
  }
}

class _AgentTextDock extends StatelessWidget {
  const _AgentTextDock({
    required this.controller,
    required this.focusNode,
    required this.keyboardVisible,
    required this.enabled,
    required this.confirmingConvertedText,
    required this.submitting,
    required this.canSubmitConvertedText,
    required this.canSubmitText,
    required this.onReturnToVoice,
    required this.onSubmit,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool keyboardVisible;
  final bool enabled;
  final bool confirmingConvertedText;
  final bool submitting;
  final bool canSubmitConvertedText;
  final bool canSubmitText;
  final FutureOr<void> Function() onReturnToVoice;
  final FutureOr<void> Function() onSubmit;

  @override
  Widget build(BuildContext context) {
    return ConversationTextComposerDock(
      controller: controller,
      focusNode: focusNode,
      enabled: enabled,
      canSubmit: confirmingConvertedText
          ? canSubmitConvertedText
          : canSubmitText,
      submitting: submitting,
      onReturn: onReturnToVoice,
      onSubmit: onSubmit,
      returnKey: Key(
        confirmingConvertedText
            ? 'agent-voice-cancel'
            : 'agent-show-voice-composer',
      ),
      fieldKey: const Key('agent-composer-field'),
      submitKey: Key(
        confirmingConvertedText ? 'agent-voice-confirm' : 'agent-send-button',
      ),
      returnTooltip: confirmingConvertedText ? '取消转文字' : '切换到语音输入',
      returnIcon: confirmingConvertedText
          ? Icons.close_rounded
          : Icons.mic_none_rounded,
      hintText: enabled
          ? confirmingConvertedText
                ? '编辑识别文字后发送'
                : '问问 SpeakUp'
          : '暂时无法开始对话',
      maxLines: keyboardVisible ? 3 : 2,
      inputFormatters: <TextInputFormatter>[_agentContentFormatter],
    );
  }
}

enum _AgentImageSource { gallery, camera }

final TextInputFormatter _agentContentFormatter =
    TextInputFormatter.withFunction((oldValue, newValue) {
      final text = newValue.text;
      return text.runes.length <= 4096 && utf8.encode(text).length <= 16384
          ? newValue
          : oldValue;
    });
