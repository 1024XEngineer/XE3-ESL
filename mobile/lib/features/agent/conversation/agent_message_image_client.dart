final class AgentMessageImageContent {
  const AgentMessageImageContent({required this.url, required this.expiresAt});

  final Uri url;
  final DateTime expiresAt;
}

/// Reads expiring content for images already attached to committed Messages.
abstract interface class AgentMessageImageClient {
  Future<AgentMessageImageContent> getMessageImageContent({
    required String imageAssetId,
  });
}

final class FakeAgentMessageImageClient implements AgentMessageImageClient {
  @override
  Future<AgentMessageImageContent> getMessageImageContent({
    required String imageAssetId,
  }) async {
    if (imageAssetId.trim().isEmpty) {
      throw ArgumentError.value(imageAssetId, 'imageAssetId');
    }
    return AgentMessageImageContent(
      url: Uri.parse('https://example.invalid/$imageAssetId'),
      expiresAt: DateTime.now().toUtc().add(const Duration(minutes: 2)),
    );
  }
}
