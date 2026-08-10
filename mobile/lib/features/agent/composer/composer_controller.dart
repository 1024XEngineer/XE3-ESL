import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';

typedef ComposerClientIdFactory = String Function(String scope);

/// Owns uncommitted text-adjacent input: selected images and voice capture.
///
/// A successful submission is handed to [ConversationController], which owns
/// the resulting durable Messages and Run state.
final class ComposerController extends ChangeNotifier {
  ComposerController({
    required this.conversationController,
    this.imageClient,
    this.imagePicker,
    AgentVoiceInputClient? voiceInputClient,
    AgentVoiceRecorder? voiceRecorder,
    ComposerClientIdFactory? clientIdFactory,
  }) : _clientIdFactory = clientIdFactory ?? _createSecureComposerId {
    if (voiceInputClient != null) {
      _voiceController = AgentVoiceInputController(
        client: voiceInputClient,
        recorder: voiceRecorder ?? FakeAgentVoiceStreamingRecorder(),
        idFactory: _newId,
        submitTranscript: sendText,
      )..addListener(_relayVoiceState);
    }
    conversationController.addListener(_syncConversationState);
    _syncConversationState();
  }

  final ConversationController conversationController;
  final AgentImageClient? imageClient;
  final AgentImagePicker? imagePicker;
  final ComposerClientIdFactory _clientIdFactory;

  AgentVoiceInputController? _voiceController;
  List<AgentPendingImage> _pendingImages = const <AgentPendingImage>[];
  String? _imageErrorMessage;
  bool _imageSelectionInFlight = false;
  bool _disposed = false;
  int _generation = 0;
  int _voiceStartGeneration = 0;
  Future<void>? _voiceStartFuture;
  bool _departureInFlight = false;
  String? _boundThreadId;

  AgentVoiceInputController? get voiceController => _voiceController;
  bool get supportsAgentVoice => _voiceController != null;
  bool get supportsAgentImages => imageClient != null && imagePicker != null;
  List<AgentPendingImage> get pendingImages =>
      List<AgentPendingImage>.unmodifiable(_pendingImages);
  String? get imageErrorMessage => _imageErrorMessage;
  bool get isImageSelectionInFlight => _imageSelectionInFlight;
  bool get hasPendingImageUpload => _pendingImages.any(
    (image) => image.state == AgentPendingImageState.uploading,
  );
  bool get canSendPendingImages =>
      !_imageSelectionInFlight &&
      _pendingImages.every(
        (image) => image.state == AgentPendingImageState.ready,
      );
  bool get hasActiveWorkflow =>
      _imageSelectionInFlight ||
      hasPendingImageUpload ||
      (_voiceController?.hasActiveWorkflow ?? false);

  Future<bool> sendText(String value) async {
    if (!canSendPendingImages) {
      _imageErrorMessage = '请等待图片上传完成，或重试失败的图片。';
      notifyListeners();
      return false;
    }
    final threadId = conversationController.threadId;
    if (_pendingImages.any((pending) => pending.asset?.threadId != threadId)) {
      _imageErrorMessage = '图片不属于当前对话，请重新选择。';
      notifyListeners();
      return false;
    }
    final assetIds = [for (final pending in _pendingImages) pending.asset!.id];
    final sent = await conversationController.sendText(
      value,
      imageAssetIds: assetIds,
    );
    if (sent && !_disposed) {
      _pendingImages = const <AgentPendingImage>[];
      _imageErrorMessage = null;
      notifyListeners();
    }
    return sent;
  }

  Future<void> startAgentVoiceRecording() {
    final voice = _voiceController;
    if (voice == null ||
        _disposed ||
        voice.hasActiveWorkflow ||
        _departureInFlight ||
        _voiceStartFuture != null) {
      return Future<void>.value();
    }
    final generation = ++_voiceStartGeneration;
    late final Future<void> operation;
    operation = _startAgentVoiceRecording(voice, generation).whenComplete(() {
      if (identical(_voiceStartFuture, operation)) {
        _voiceStartFuture = null;
      }
    });
    _voiceStartFuture = operation;
    return operation;
  }

  Future<void> _startAgentVoiceRecording(
    AgentVoiceInputController voice,
    int generation,
  ) async {
    await conversationController.initialize();
    if (!_isVoiceStartCurrent(voice, generation)) {
      return;
    }
    if (conversationController.threadId == null &&
        !await conversationController.createThread()) {
      return;
    }
    if (!_isVoiceStartCurrent(voice, generation)) {
      return;
    }
    final threadId = conversationController.threadId;
    if (threadId == null) {
      return;
    }
    await voice.bindThread(threadId);
    if (!_isVoiceStartCurrent(voice, generation) ||
        conversationController.threadId != threadId ||
        voice.threadId != threadId) {
      return;
    }
    await voice.startRecording();
  }

  Future<void> pickAgentImages() async {
    final picker = imagePicker;
    if (picker == null) {
      return;
    }
    await _selectImages(
      () => picker.pickFromGallery(
        limit: agentMaximumImagesPerMessage - _pendingImages.length,
      ),
    );
  }

  Future<void> takeAgentPhoto() async {
    final picker = imagePicker;
    if (picker == null) {
      return;
    }
    await _selectImages(() async {
      final image = await picker.takePhoto();
      return image == null ? const <AgentLocalImage>[] : [image];
    });
  }

  Future<void> _selectImages(
    Future<List<AgentLocalImage>> Function() select,
  ) async {
    if (_disposed ||
        !supportsAgentImages ||
        _imageSelectionInFlight ||
        _pendingImages.length >= agentMaximumImagesPerMessage) {
      return;
    }
    final generation = _generation;
    _imageSelectionInFlight = true;
    _imageErrorMessage = null;
    notifyListeners();
    try {
      final selected = await select();
      if (!_isCurrent(generation)) {
        return;
      }
      if (selected.isEmpty) {
        return;
      }
      if (await _ensureThread() == null || !_isCurrent(generation)) {
        return;
      }
      await _stageAndUpload(selected);
    } catch (error) {
      if (_isCurrent(generation)) {
        _imageErrorMessage = _selectionFailureMessage(error);
      }
    } finally {
      if (_isCurrent(generation)) {
        _imageSelectionInFlight = false;
        notifyListeners();
      }
    }
  }

  Future<void> _stageAndUpload(List<AgentLocalImage> selected) async {
    if (selected.isEmpty) {
      return;
    }
    final available = agentMaximumImagesPerMessage - _pendingImages.length;
    final accepted = selected.take(available).where(_validLocalImage).toList();
    if (accepted.length != selected.take(available).length) {
      _imageErrorMessage = '图片格式、尺寸或文件大小不符合要求，请重新选择。';
    }
    final staged = [
      for (final image in accepted)
        AgentPendingImage(
          localId: _newId('local-image'),
          uploadRequestId: _newId('image-upload'),
          image: image,
          state: AgentPendingImageState.uploading,
        ),
    ];
    _pendingImages = [..._pendingImages, ...staged];
    notifyListeners();
    await Future.wait([
      for (final pending in staged) _uploadPendingImage(pending.localId),
    ]);
  }

  Future<void> retryPendingImage(String localId) async {
    final pending = _pendingImages
        .where((image) => image.localId == localId)
        .firstOrNull;
    if (pending == null ||
        pending.state != AgentPendingImageState.failed ||
        _disposed) {
      return;
    }
    _replacePendingImage(
      pending.copyWith(state: AgentPendingImageState.uploading),
    );
    _imageErrorMessage = null;
    notifyListeners();
    await _uploadPendingImage(localId);
  }

  Future<void> _uploadPendingImage(String localId) async {
    final client = imageClient;
    final threadId = conversationController.threadId;
    final pending = _pendingImages
        .where((image) => image.localId == localId)
        .firstOrNull;
    if (client == null || threadId == null || pending == null || _disposed) {
      return;
    }
    final generation = _generation;
    try {
      final asset = await client.uploadImage(
        threadId: threadId,
        image: pending.image,
        idempotencyKey: pending.uploadRequestId,
      );
      if (!_isCurrent(generation)) {
        if (asset.threadId == threadId &&
            asset.status == AgentImageAssetStatus.staged) {
          unawaited(_deleteImageBestEffort(asset.id));
        }
        return;
      }
      if (asset.threadId != threadId ||
          asset.status != AgentImageAssetStatus.staged) {
        return;
      }
      final current = _pendingImages
          .where((image) => image.localId == localId)
          .firstOrNull;
      if (current != null) {
        _replacePendingImage(
          current.copyWith(state: AgentPendingImageState.ready, asset: asset),
        );
      }
    } catch (error) {
      if (_isCurrent(generation)) {
        final current = _pendingImages
            .where((image) => image.localId == localId)
            .firstOrNull;
        if (current != null) {
          _replacePendingImage(
            current.copyWith(state: AgentPendingImageState.failed),
          );
          _imageErrorMessage = _uploadFailureMessage(error);
        }
      }
    }
    if (_isCurrent(generation)) {
      notifyListeners();
    }
  }

  Future<String?> _ensureThread() async {
    await conversationController.initialize();
    if (conversationController.threadId == null &&
        !await conversationController.createThread()) {
      return null;
    }
    return conversationController.threadId;
  }

  Future<void> removePendingImage(String localId) async {
    final pending = _pendingImages
        .where((image) => image.localId == localId)
        .firstOrNull;
    if (pending == null) {
      return;
    }
    _pendingImages = [
      for (final image in _pendingImages)
        if (image.localId != localId) image,
    ];
    _imageErrorMessage = null;
    notifyListeners();
    final asset = pending.asset;
    if (asset != null) {
      try {
        await imageClient?.deleteImage(imageAssetId: asset.id);
      } catch (_) {
        // Staged assets are also bounded by server-side expiry and cleanup.
      }
    }
  }

  void _replacePendingImage(AgentPendingImage replacement) {
    _pendingImages = [
      for (final image in _pendingImages)
        if (image.localId == replacement.localId) replacement else image,
    ];
  }

  bool _validLocalImage(AgentLocalImage image) =>
      image.name.trim().isNotEmpty &&
      image.contentType.trim().isNotEmpty &&
      image.sizeBytes > 0 &&
      image.sizeBytes <= agentMaximumImageBytes;

  void _syncConversationState() {
    if (_disposed) {
      return;
    }
    final threadId = conversationController.threadId;
    if (threadId != _boundThreadId) {
      final previousThreadId = _boundThreadId;
      _boundThreadId = threadId;
      if (previousThreadId != null) {
        _generation++;
        final stagedAssetIds = <String>[
          for (final pending in _pendingImages)
            if (pending.asset case final asset?) asset.id,
        ];
        _pendingImages = const <AgentPendingImage>[];
        _imageErrorMessage = null;
        _imageSelectionInFlight = false;
        for (final assetId in stagedAssetIds) {
          unawaited(_deleteImageBestEffort(assetId));
        }
        notifyListeners();
      }
      if (_voiceController case final voice?) {
        unawaited(voice.bindThread(threadId));
      }
    }
  }

  Future<void> _deleteImageBestEffort(String imageAssetId) async {
    try {
      await imageClient?.deleteImage(imageAssetId: imageAssetId);
    } catch (_) {
      // Staged assets are also bounded by server-side expiry and cleanup.
    }
  }

  void _relayVoiceState() {
    if (!_disposed) {
      notifyListeners();
    }
  }

  Future<bool> prepareToLeave() async {
    if (_disposed ||
        _departureInFlight ||
        _imageSelectionInFlight ||
        hasPendingImageUpload) {
      return false;
    }
    _departureInFlight = true;
    try {
      _voiceStartGeneration++;
      final voice = _voiceController;
      final voiceStart = _voiceStartFuture;
      if (voiceStart != null && voice != null) {
        await voice.cancel();
      }
      await voiceStart;
      if (_disposed) {
        return false;
      }
      if (voice == null) {
        return true;
      }
      if (voice.state != AgentVoiceInputState.idle) {
        await voice.cancel();
      }
      return !_disposed && !voice.hasActiveWorkflow;
    } finally {
      _departureInFlight = false;
    }
  }

  Future<void> clearPrivateState() async {
    _generation++;
    final stagedAssets = [
      for (final pending in _pendingImages)
        if (pending.asset != null) pending.asset!,
    ];
    _pendingImages = const <AgentPendingImage>[];
    _imageErrorMessage = null;
    _imageSelectionInFlight = false;
    _boundThreadId = null;
    if (!_disposed) {
      notifyListeners();
    }
    await Future.wait([
      if (_voiceController case final AgentVoiceInputController voice)
        voice.clearPrivateState(),
      for (final asset in stagedAssets)
        imageClient?.deleteImage(imageAssetId: asset.id).catchError((_) {}) ??
            Future<void>.value(),
      if (imageClient case final images?) images.clearAccountState(),
    ]);
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    conversationController.removeListener(_syncConversationState);
    _voiceController?.removeListener(_relayVoiceState);
    _voiceController?.dispose();
    super.dispose();
  }

  bool _isCurrent(int generation) => !_disposed && generation == _generation;

  bool _isVoiceStartCurrent(AgentVoiceInputController voice, int generation) =>
      !_disposed &&
      !_departureInFlight &&
      generation == _voiceStartGeneration &&
      identical(_voiceController, voice);

  String _newId(String scope) {
    final id = _clientIdFactory(scope);
    if (id.isEmpty) {
      throw StateError('Composer client identity must not be empty.');
    }
    return id;
  }

  String _selectionFailureMessage(Object error) =>
      error is AgentClientException &&
          error.kind == AgentClientFailureKind.authenticationRequired
      ? '登录状态已失效，请重新登录。'
      : '无法读取所选图片，请检查相册或相机权限后重试。';

  String _uploadFailureMessage(Object error) {
    if (error is AgentClientException) {
      if (error.kind == AgentClientFailureKind.authenticationRequired) {
        return '登录状态已失效，请重新登录。';
      }
      if (error.kind == AgentClientFailureKind.network) {
        return '图片上传失败，请检查网络后重试。';
      }
      if (error.kind == AgentClientFailureKind.rateLimited) {
        return '图片上传过于频繁，请稍后重试。';
      }
    }
    return '图片暂时无法上传，可以重试或移除。';
  }
}

final Random _composerIdRandom = Random.secure();

String _createSecureComposerId(String scope) {
  final buffer = StringBuffer(scope)..write('_');
  for (var index = 0; index < 16; index++) {
    buffer.write(
      _composerIdRandom.nextInt(256).toRadixString(16).padLeft(2, '0'),
    );
  }
  return buffer.toString();
}
