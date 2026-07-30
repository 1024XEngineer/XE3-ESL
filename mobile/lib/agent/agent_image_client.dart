import 'dart:typed_data';

import 'agent_models.dart';

const agentMaximumImagesPerMessage = 4;
const agentMaximumImageBytes = 10 * 1024 * 1024;

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

final class AgentImageContent {
  const AgentImageContent({required this.url, required this.expiresAt});

  final Uri url;
  final DateTime expiresAt;
}

abstract interface class AgentImageClient {
  Future<void> clearAccountState();

  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  });

  Future<AgentImageContent> getImageContent({required String imageAssetId});

  Future<void> deleteImage({required String imageAssetId});
}

abstract interface class AgentMultimodalClient {
  Future<AgentExchange> sendMultimodal({
    required String threadId,
    required String text,
    required String clientMessageId,
    required List<String> imageAssetIds,
  });
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
