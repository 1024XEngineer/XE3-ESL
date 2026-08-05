import 'dart:typed_data';

abstract interface class AgentMessageMemeClient {
  Future<Uint8List> getMemeContent({
    required String contentPath,
    required int expectedSizeBytes,
    required String expectedContentType,
  });
}
