import 'dart:typed_data';

import 'package:speakup/features/agent/conversation/agent_client.dart';

sealed class AgentVoiceInputEvent {
  const AgentVoiceInputEvent();
}

final class AgentVoiceInputStarted extends AgentVoiceInputEvent {
  const AgentVoiceInputStarted();
}

final class AgentVoiceInputUpdated extends AgentVoiceInputEvent {
  const AgentVoiceInputUpdated(this.transcript);

  final String transcript;
}

final class AgentVoiceInputCompleted extends AgentVoiceInputEvent {
  const AgentVoiceInputCompleted(this.transcript);

  final String transcript;
}

final class AgentVoiceInputFailed extends AgentVoiceInputEvent {
  const AgentVoiceInputFailed({required this.kind, required this.retryable});

  final String kind;
  final bool retryable;
}

abstract interface class AgentVoiceInputClient {
  Stream<AgentVoiceInputEvent> transcribeRealtime({
    required String threadId,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
  });

  Future<void> clearAccountState();

  Future<void> dispose();
}

final class AgentVoiceInputFailure implements Exception {
  const AgentVoiceInputFailure({required this.kind, required this.retryable});

  final String kind;
  final bool retryable;
}

/// Explicit preview/test provider for ephemeral Agent voice-to-text input.
final class FakeAgentVoiceInputClient implements AgentVoiceInputClient {
  FakeAgentVoiceInputClient({
    this.partialTranscript = 'I explained the problem and the trade-off',
    this.completedTranscript =
        'I explained the problem, the trade-off, and the result clearly.',
  });

  final String partialTranscript;
  final String completedTranscript;
  int _accountGeneration = 0;
  bool _disposed = false;

  @override
  Stream<AgentVoiceInputEvent> transcribeRealtime({
    required String threadId,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
  }) async* {
    if (_disposed) {
      throw const AgentClientOperationCancelled();
    }
    if (threadId.trim().isEmpty ||
        idempotencyKey.length < 8 ||
        idempotencyKey.length > 128 ||
        partialTranscript.trim().isEmpty ||
        completedTranscript.trim().isEmpty) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidRequest,
      );
    }
    final generation = _accountGeneration;
    yield const AgentVoiceInputStarted();
    var receivedAudio = false;
    await for (final chunk in audioChunks) {
      _requireCurrent(generation);
      if (chunk.isEmpty) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      if (!receivedAudio) {
        receivedAudio = true;
        yield AgentVoiceInputUpdated(partialTranscript);
      }
    }
    _requireCurrent(generation);
    if (!receivedAudio) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidRequest,
      );
    }
    yield AgentVoiceInputCompleted(completedTranscript);
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _accountGeneration++;
  }

  void _requireCurrent(int generation) {
    if (_disposed || generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }
}
