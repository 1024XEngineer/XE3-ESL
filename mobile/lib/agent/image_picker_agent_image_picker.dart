import 'dart:typed_data';

import 'package:flutter/foundation.dart'
    show TargetPlatform, defaultTargetPlatform;
import 'package:image_picker/image_picker.dart';

import 'agent_image_client.dart';

final class ImagePickerAgentImagePicker implements AgentImagePicker {
  ImagePickerAgentImagePicker({ImagePicker? picker})
    : _picker = picker ?? ImagePicker();

  final ImagePicker _picker;

  @override
  Future<List<AgentLocalImage>> pickFromGallery({required int limit}) async {
    if (limit < 1 || limit > agentMaximumImagesPerMessage) {
      return const <AgentLocalImage>[];
    }
    if (limit == 1) {
      final file = await _picker.pickImage(
        source: ImageSource.gallery,
        maxWidth: 2048,
        maxHeight: 2048,
        requestFullMetadata: false,
      );
      return file == null
          ? const <AgentLocalImage>[]
          : <AgentLocalImage>[await _readFile(file)];
    }
    final files = await _picker.pickMultiImage(
      limit: limit,
      maxWidth: 2048,
      maxHeight: 2048,
      requestFullMetadata: false,
    );
    return _readFiles(files.take(limit));
  }

  @override
  Future<AgentLocalImage?> takePhoto() async {
    final file = await _picker.pickImage(
      source: ImageSource.camera,
      maxWidth: 2048,
      maxHeight: 2048,
      requestFullMetadata: false,
    );
    if (file == null) {
      return null;
    }
    return _readFile(file);
  }

  @override
  Future<List<AgentLocalImage>> recoverLostImages() async {
    // Lost-data recovery is an Android MainActivity lifecycle mechanism.
    // The iOS implementation intentionally does not implement this API.
    if (defaultTargetPlatform != TargetPlatform.android) {
      return const <AgentLocalImage>[];
    }
    final response = await _picker.retrieveLostData();
    if (response.isEmpty || response.files == null) {
      return const <AgentLocalImage>[];
    }
    return _readFiles(response.files!.take(agentMaximumImagesPerMessage));
  }

  Future<List<AgentLocalImage>> _readFiles(Iterable<XFile> files) async {
    final result = <AgentLocalImage>[];
    for (final file in files) {
      result.add(await _readFile(file));
    }
    return List<AgentLocalImage>.unmodifiable(result);
  }

  Future<AgentLocalImage> _readFile(XFile file) async {
    final bytes = await file.readAsBytes();
    return AgentLocalImage(
      name: file.name,
      contentType: _imageContentType(file),
      bytes: Uint8List.fromList(bytes),
    );
  }
}

String _imageContentType(XFile file) {
  final declared = file.mimeType?.toLowerCase();
  if (declared == 'image/jpeg' ||
      declared == 'image/png' ||
      declared == 'image/webp') {
    return declared!;
  }
  final name = file.name.toLowerCase();
  if (name.endsWith('.jpg') || name.endsWith('.jpeg')) {
    return 'image/jpeg';
  }
  if (name.endsWith('.png')) {
    return 'image/png';
  }
  if (name.endsWith('.webp')) {
    return 'image/webp';
  }
  return 'application/octet-stream';
}
