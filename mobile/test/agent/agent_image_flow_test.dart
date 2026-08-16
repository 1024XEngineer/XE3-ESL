import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('uploads selected image before sending a multimodal Message', () async {
    final client = FakeAgentClient();
    final imageClient = FakeAgentImageClient();
    final picker = _ImagePicker(gallery: <AgentLocalImage>[_fixtureImage()]);
    final conversationController = ConversationController(client: client);
    final composerController = ComposerController(
      conversationController: conversationController,
      imageClient: imageClient,
      imagePicker: picker,
    );
    addTearDown(() {
      composerController.dispose();
      conversationController.dispose();
    });
    await conversationController.initialize();

    await composerController.pickAgentImages();

    expect(composerController.pendingImages, hasLength(1));
    expect(
      composerController.pendingImages.single.state,
      AgentPendingImageState.ready,
    );
    expect(
      await composerController.sendText('Please explain this image.'),
      isTrue,
    );
    expect(composerController.pendingImages, isEmpty);
    expect(conversationController.messages, hasLength(2));
    expect(
      conversationController.messages.first.modality,
      AgentMessageModality.multimodal,
    );
    expect(conversationController.messages.first.images, hasLength(1));
  });

  test('image upload retry reuses the original idempotency key', () async {
    final imageClient = _FailOnceImageClient();
    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
      imageClient: imageClient,
      imagePicker: _ImagePicker(gallery: <AgentLocalImage>[_fixtureImage()]),
    );
    addTearDown(() {
      composerController.dispose();
      conversationController.dispose();
    });
    await conversationController.initialize();

    await composerController.pickAgentImages();

    expect(
      composerController.pendingImages.single.state,
      AgentPendingImageState.failed,
    );
    final localId = composerController.pendingImages.single.localId;
    await composerController.retryPendingImage(localId);

    expect(
      composerController.pendingImages.single.state,
      AgentPendingImageState.ready,
    );
    expect(imageClient.idempotencyKeys, hasLength(2));
    expect(imageClient.idempotencyKeys.toSet(), hasLength(1));
  });

  test(
    'switching Threads removes a staged image from the old Thread',
    () async {
      final imageClient = _RecordingImageClient();
      final conversationController = ConversationController(
        client: FakeAgentClient(),
      );
      final composerController = ComposerController(
        conversationController: conversationController,
        imageClient: imageClient,
        imagePicker: _ImagePicker(gallery: <AgentLocalImage>[_fixtureImage()]),
      );
      addTearDown(() {
        composerController.dispose();
        conversationController.dispose();
      });
      await conversationController.initialize();
      expect(await conversationController.sendText('Existing message'), isTrue);

      await composerController.pickAgentImages();
      final oldThreadId = conversationController.threadId;
      final stagedAssetId = composerController.pendingImages.single.asset!.id;

      expect(await conversationController.createThread(), isTrue);
      await Future<void>.delayed(Duration.zero);

      expect(conversationController.threadId, isNot(oldThreadId));
      expect(composerController.pendingImages, isEmpty);
      expect(imageClient.deletedAssetIds, contains(stagedAssetId));
    },
  );

  test('an old Thread upload cannot repopulate the new Thread draft', () async {
    final imageClient = _ControlledImageClient();
    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
      imageClient: imageClient,
      imagePicker: _ImagePicker(gallery: <AgentLocalImage>[_fixtureImage()]),
    );
    addTearDown(() {
      composerController.dispose();
      conversationController.dispose();
    });
    await conversationController.initialize();
    expect(await conversationController.sendText('Existing message'), isTrue);
    final oldThreadId = conversationController.threadId!;

    final selection = composerController.pickAgentImages();
    await imageClient.uploadStarted.future;
    expect(await conversationController.createThread(), isTrue);
    imageClient.completeUpload(threadId: oldThreadId);
    await selection;
    await Future<void>.delayed(Duration.zero);

    expect(conversationController.threadId, isNot(oldThreadId));
    expect(composerController.pendingImages, isEmpty);
    expect(imageClient.deletedAssetIds, contains('image-controlled'));
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
      contentType: 'image/png',
      sizeBytes: image.sizeBytes,
      width: 1,
      height: 1,
      status: AgentImageAssetStatus.ready,
      createdAt: DateTime.now().toUtc(),
    );
  }
}

class _RecordingImageClient implements AgentImageClient {
  final List<String> deletedAssetIds = <String>[];

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> deleteImage({required String imageAssetId}) async {
    deletedAssetIds.add(imageAssetId);
  }

  @override
  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  }) async {
    return AgentImageAsset(
      id: 'image-recorded',
      contentType: image.contentType,
      sizeBytes: image.sizeBytes,
      width: 1,
      height: 1,
      status: AgentImageAssetStatus.ready,
      createdAt: DateTime.now().toUtc(),
    );
  }
}

final class _ControlledImageClient extends _RecordingImageClient {
  final uploadStarted = Completer<void>();
  final _uploadResult = Completer<AgentImageAsset>();

  @override
  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  }) {
    uploadStarted.complete();
    return _uploadResult.future;
  }

  void completeUpload({required String threadId}) {
    _uploadResult.complete(
      AgentImageAsset(
        id: 'image-controlled',
        contentType: 'image/png',
        sizeBytes: 4,
        width: 1,
        height: 1,
        status: AgentImageAssetStatus.ready,
        createdAt: DateTime.now().toUtc(),
      ),
    );
  }
}

AgentLocalImage _fixtureImage() => AgentLocalImage(
  name: 'fixture.png',
  contentType: 'image/png',
  bytes: Uint8List.fromList(<int>[137, 80, 78, 71]),
);
