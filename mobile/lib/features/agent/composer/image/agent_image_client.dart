import 'dart:typed_data';

import 'package:speakup/features/agent/conversation/agent_models.dart';

final class AgentLocalImage {
  AgentLocalImage({
    required this.name,
    required this.contentType,
    required Uint8List bytes,
  }) : bytes = Uint8List.fromList(bytes);

  final String name;
  final String contentType;
  final Uint8List bytes;

  int get sizeBytes => bytes.length;
}

abstract interface class AgentImageClient {
  Future<void> clearAccountState();

  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  });

  Future<void> deleteImage({required String imageAssetId});
}

final class FakeAgentImageClient implements AgentImageClient {
  int _sequence = 0;
  int _accountGeneration = 0;
  final Map<String, AgentImageAsset> _assets = <String, AgentImageAsset>{};

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
    _sequence = 0;
    _assets.clear();
  }

  @override
  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  }) async {
    if (threadId.trim().isEmpty ||
        idempotencyKey.trim().isEmpty ||
        image.sizeBytes < 1 ||
        image.sizeBytes > agentMaximumImageBytes) {
      throw ArgumentError('Fake Agent image upload is invalid.');
    }
    final asset = AgentImageAsset(
      id: 'image_local_${_accountGeneration}_${++_sequence}',
      threadId: threadId,
      contentType: image.contentType,
      sizeBytes: image.sizeBytes,
      width: 1,
      height: 1,
      status: AgentImageAssetStatus.staged,
      createdAt: DateTime.now().toUtc(),
    );
    _assets[asset.id] = asset;
    return asset;
  }

  @override
  Future<void> deleteImage({required String imageAssetId}) async {
    _assets.remove(imageAssetId);
  }
}

abstract interface class AgentImagePicker {
  Future<List<AgentLocalImage>> pickFromGallery({required int limit});

  Future<AgentLocalImage?> takePhoto();

  Future<List<AgentLocalImage>> recoverLostImages();
}

enum AgentPendingImageState { uploading, ready, failed }

final class AgentPendingImage {
  const AgentPendingImage({
    required this.localId,
    required this.uploadRequestId,
    required this.image,
    required this.state,
    this.asset,
  });

  final String localId;
  final String uploadRequestId;
  final AgentLocalImage image;
  final AgentPendingImageState state;
  final AgentImageAsset? asset;

  AgentPendingImage copyWith({
    AgentPendingImageState? state,
    AgentImageAsset? asset,
  }) {
    return AgentPendingImage(
      localId: localId,
      uploadRequestId: uploadRequestId,
      image: image,
      state: state ?? this.state,
      asset: asset ?? this.asset,
    );
  }
}
