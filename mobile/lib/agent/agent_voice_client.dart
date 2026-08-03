import 'dart:typed_data';

import 'agent_models.dart';
import 'agent_voice_models.dart';

abstract interface class AgentVoiceClient {
  Stream<AgentVoiceTranscriptionEvent> createCandidateStream({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  });

  Future<AgentVoiceCandidate> createCandidate({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  });

  Future<AgentVoiceCandidate> getCandidate({required String candidateId});

  Future<AgentVoiceCandidate> retryCandidate({required String candidateId});

  Future<void> deleteCandidate({required String candidateId});

  Future<AgentVoiceConfirmation> confirmCandidate({
    required String candidateId,
    required int candidateVersion,
    required String clientMessageId,
    required String confirmedText,
  });

  Future<AgentVoiceRun> getRun({required String runId});

  Future<AgentVoiceRun> retryRun({
    required String runId,
    required String clientRetryId,
  });

  Future<AgentMessage?> getMessage({
    required String threadId,
    required String messageId,
  });

  Future<Uint8List> loadMessageAudio({required String audioId});

  Future<void> deleteMessageAudio({required String audioId});

  Future<Uint8List> loadAssistantSpeech({required String messageId});

  Future<Uint8List> loadSpeechPreview({
    required String messageId,
    required String text,
  });

  Future<void> clearAccountState();

  Future<void> dispose();
}
