import 'dart:typed_data';

/// Remote media operations for audio attached to committed Agent Messages.
abstract interface class AgentMessageAudioClient {
  Future<Uint8List> loadMessageAudio({required String audioId});

  Future<void> deleteMessageAudio({required String audioId});

  Future<Uint8List> loadAssistantSpeech({required String messageId});

  Future<Uint8List> loadSpeechPreview({
    required String messageId,
    required String text,
  });
}
