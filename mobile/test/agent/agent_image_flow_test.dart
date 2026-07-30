import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_image_client.dart';
import 'package:speakup/agent/agent_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('uploads selected image before sending a multimodal Message', () async {
    final client = FakeAgentClient();
    final picker = _ImagePicker(gallery: <AgentLocalImage>[_fixtureImage()]);
    final controller = AgentController(client: client, imagePicker: picker);
    await controller.initialize();

    await controller.pickAgentImages();

    expect(controller.pendingImages, hasLength(1));
    expect(controller.pendingImages.single.state, AgentPendingImageState.ready);
    expect(await controller.sendText('Please explain this image.'), isTrue);
    expect(controller.pendingImages, isEmpty);
    expect(controller.messages, hasLength(2));
    expect(controller.messages.first.modality, AgentMessageModality.multimodal);
    expect(controller.messages.first.images, hasLength(1));
  });

  test('image upload retry reuses the original idempotency key', () async {
    final imageClient = _FailOnceImageClient();
    final controller = AgentController(
      client: FakeAgentClient(),
      imageClient: imageClient,
      imagePicker: _ImagePicker(gallery: <AgentLocalImage>[_fixtureImage()]),
    );
    await controller.initialize();

    await controller.pickAgentImages();

    expect(
      controller.pendingImages.single.state,
      AgentPendingImageState.failed,
    );
    final localId = controller.pendingImages.single.localId;
    await controller.retryPendingImage(localId);

    expect(controller.pendingImages.single.state, AgentPendingImageState.ready);
    expect(imageClient.idempotencyKeys, hasLength(2));
    expect(imageClient.idempotencyKeys.toSet(), hasLength(1));
  });
}

final class _ImagePicker implements AgentImagePicker {
  _ImagePicker({this.gallery = const <AgentLocalImage>[]});

  final List<AgentLocalImage> gallery;

  @override
  Future<List<AgentLocalImage>> pickFromGallery({required int limit}) async {
    return gallery.take(limit).toList();
  }

  @override
  Future<List<AgentLocalImage>> recoverLostImages() async {
    return const <AgentLocalImage>[];
  }

  @override
  Future<AgentLocalImage?> takePhoto() async => null;
}

final class _FailOnceImageClient implements AgentImageClient {
  final List<String> idempotencyKeys = <String>[];

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> deleteImage({required String imageAssetId}) async {}

  @override
  Future<AgentImageContent> getImageContent({required String imageAssetId}) {
    throw UnimplementedError();
  }

  @override
  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  }) async {
    idempotencyKeys.add(idempotencyKey);
    if (idempotencyKeys.length == 1) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    }
    return AgentImageAsset(
      id: 'image-retry',
      threadId: threadId,
      contentType: 'image/png',
      sizeBytes: image.sizeBytes,
      width: 1,
      height: 1,
      status: AgentImageAssetStatus.staged,
      createdAt: DateTime.now().toUtc(),
    );
  }
}

AgentLocalImage _fixtureImage() => AgentLocalImage(
  name: 'fixture.png',
  contentType: 'image/png',
  bytes: Uint8List.fromList(<int>[137, 80, 78, 71]),
);
