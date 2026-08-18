import 'dart:typed_data';

import 'package:image_picker/image_picker.dart';
import 'package:speakup/identity/client/identity_client.dart';

abstract interface class ProfileAvatarPicker {
  Future<UserAvatarImage?> pickFromGallery();

  Future<UserAvatarImage?> takePhoto();
}

final class SystemProfileAvatarPicker implements ProfileAvatarPicker {
  SystemProfileAvatarPicker({ImagePicker? picker})
    : _picker = picker ?? ImagePicker();

  final ImagePicker _picker;

  @override
  Future<UserAvatarImage?> pickFromGallery() => _pick(ImageSource.gallery);

  @override
  Future<UserAvatarImage?> takePhoto() => _pick(ImageSource.camera);

  Future<UserAvatarImage?> _pick(ImageSource source) async {
    final file = await _picker.pickImage(
      source: source,
      maxWidth: 2048,
      maxHeight: 2048,
      imageQuality: 92,
      requestFullMetadata: false,
    );
    if (file == null) {
      return null;
    }
    final bytes = Uint8List.fromList(await file.readAsBytes());
    return UserAvatarImage(contentType: _contentType(file), bytes: bytes);
  }
}

String _contentType(XFile file) {
  final declared = file.mimeType?.toLowerCase();
  if (declared == 'image/jpeg' ||
      declared == 'image/png' ||
      declared == 'image/webp') {
    return declared!;
  }
  final name = file.name.toLowerCase();
  if (name.endsWith('.jpg') || name.endsWith('.jpeg')) return 'image/jpeg';
  if (name.endsWith('.png')) return 'image/png';
  if (name.endsWith('.webp')) return 'image/webp';
  return 'application/octet-stream';
}
