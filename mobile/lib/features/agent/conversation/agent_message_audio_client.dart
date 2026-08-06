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

final class AgentAssistantSpeechTextSegment {
  const AgentAssistantSpeechTextSegment({
    required this.sequence,
    required this.text,
  });

  final int sequence;
  final String text;
}

final class AgentAssistantSpeechAudioSegment {
  const AgentAssistantSpeechAudioSegment({
    required this.sequence,
    required this.audio,
  });

  final int sequence;
  final Uint8List audio;
}

abstract interface class AgentAssistantSpeechClient {
  Stream<AgentAssistantSpeechAudioSegment> streamAssistantSpeech({
    required String threadId,
    required Stream<AgentAssistantSpeechTextSegment> segments,
  });
}
