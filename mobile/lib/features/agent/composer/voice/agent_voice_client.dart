import 'dart:typed_data';

import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'agent_voice_models.dart';

abstract interface class AgentVoiceClient {
  Stream<AgentVoiceTranscriptionEvent> createDraftStream({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  });

  Future<AgentVoiceDraft> createDraft({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  });

  Future<AgentVoiceDraft> getDraft({required String draftId});

  Future<AgentVoiceDraft> retryDraft({required String draftId});

  Future<void> deleteDraft({required String draftId});

  Future<AgentVoiceConfirmation> confirmDraft({
    required String draftId,
    required int draftVersion,
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

  Future<void> clearAccountState();

  Future<void> dispose();
}

abstract interface class AgentVoiceStreamingClient {
  Stream<AgentVoiceConfirmationStreamEvent> confirmDraftStream({
    required String draftId,
    required int draftVersion,
    required String clientMessageId,
    required String confirmedText,
  });
}

abstract interface class AgentVoiceRealtimeInputClient {
  Stream<AgentVoiceTranscriptionEvent> createDraftRealtime({
    required String threadId,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
  });
}

/// Explicit preview/test provider for voice drafts and committed Message audio.
final class FakeAgentVoiceClient
    implements
        AgentVoiceClient,
        AgentVoiceStreamingClient,
        AgentMessageAudioClient {
  FakeAgentVoiceClient({this.delay = Duration.zero});

  final Duration delay;
  int _accountGeneration = 0;
  int _draftSequence = 0;
  int _messageSequence = 0;
  final Map<String, AgentVoiceDraft> _drafts = {};
  final Map<String, AgentVoiceConfirmation> _confirmations = {};
  final Map<String, AgentVoiceRun> _runs = {};
  final Map<String, AgentMessage> _messages = {};
  final Set<String> _audioIds = {};

  @override
  Stream<AgentVoiceTranscriptionEvent> createDraftStream({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) async* {
    final draft = await createDraft(
      threadId: threadId,
      recording: recording,
      idempotencyKey: idempotencyKey,
    );
    yield AgentVoiceTranscriptUpdated(
      text: draft.transcript!.text,
      finalResult: true,
    );
    yield AgentVoiceDraftCompleted(draft);
  }

  @override
  Future<AgentVoiceDraft> createDraft({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) async {
    final generation = _accountGeneration;
    await _wait(generation);
    if (threadId.trim().isEmpty ||
        recording.contentType != 'audio/wav' ||
        recording.sizeBytes < 1 ||
        recording.duration <= Duration.zero ||
        idempotencyKey.trim().isEmpty) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidRequest,
      );
    }
    final requestId = '$threadId\u{0}$idempotencyKey';
    for (final draft in _drafts.values) {
      if (draft.transcript?.requestId == requestId) {
        return draft;
      }
    }
    final now = DateTime.now().toUtc();
    final draft = AgentVoiceDraft(
      id: 'voice_draft_${++_draftSequence}',
      threadId: threadId,
      status: AgentVoiceDraftStatus.ready,
      asrAttempt: 1,
      version: 1,
      recording: AgentVoiceRecordingMetadata(
        contentType: 'audio/wav',
        sizeBytes: recording.sizeBytes,
        duration: recording.duration,
        sampleRate: 16000,
      ),
      transcript: AgentVoiceTranscript(
        text: 'I explained the problem, the trade-off, and the result clearly.',
        requestId: requestId,
        provider: 'fake',
        model: 'fake-asr',
        language: 'en',
        finishReason: 'stop',
      ),
      expiresAt: now.add(const Duration(hours: 1)),
      createdAt: now,
      updatedAt: now,
    );
    _drafts[draft.id] = draft;
    return draft;
  }

  @override
  Future<AgentVoiceDraft> getDraft({required String draftId}) async {
    final generation = _accountGeneration;
    await _wait(generation);
    final draft = _drafts[draftId];
    if (draft == null) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    return draft;
  }

  @override
  Future<AgentVoiceDraft> retryDraft({required String draftId}) {
    return getDraft(draftId: draftId);
  }

  @override
  Future<void> deleteDraft({required String draftId}) async {
    final generation = _accountGeneration;
    await _wait(generation);
    final draft = _drafts[draftId];
    if (draft == null) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    _drafts.remove(draftId);
  }

  @override
  Future<AgentVoiceConfirmation> confirmDraft({
    required String draftId,
    required int draftVersion,
    required String clientMessageId,
    required String confirmedText,
  }) async {
    final generation = _accountGeneration;
    await _wait(generation);
    final operationKey = '$draftId\u{0}$clientMessageId';
    final replay = _confirmations[operationKey];
    if (replay != null) {
      if (replay.message.text != confirmedText ||
          replay.draft.version != draftVersion) {
        throw const AgentClientException(kind: AgentClientFailureKind.conflict);
      }
      return replay;
    }
    final draft = _drafts[draftId];
    if (draft == null) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    if (!draft.isReady ||
        draft.version != draftVersion ||
        confirmedText.trim().isEmpty) {
      throw const AgentClientException(kind: AgentClientFailureKind.conflict);
    }
    final now = DateTime.now().toUtc();
    final audioId = 'voice_audio_$draftId';
    final message = AgentMessage(
      id: _nextMessageId(),
      role: AgentMessageRole.user,
      text: confirmedText,
      createdAt: now,
      modality: AgentMessageModality.voice,
      audio: AgentMessageAudio(
        id: audioId,
        status: AgentMessageAudioStatus.readable,
        contentType: 'audio/wav',
        sizeBytes: draft.recording.sizeBytes,
        duration: draft.recording.duration,
        playbackPath: '/v1/agent-message-audios/$audioId/playback',
      ),
    );
    final assistantMessage = AgentMessage(
      id: _nextMessageId(),
      role: AgentMessageRole.assistant,
      text:
          'That was clear. Add one measurable result to make the answer stronger.',
      createdAt: now,
    );
    final run = AgentVoiceRun(
      id: 'voice_run_$draftId',
      threadId: draft.threadId,
      inputMessageId: message.id,
      status: AgentVoiceRunStatus.completed,
      assistantMessageId: assistantMessage.id,
    );
    final confirmedDraft = AgentVoiceDraft(
      id: draft.id,
      threadId: draft.threadId,
      status: AgentVoiceDraftStatus.confirmed,
      asrAttempt: draft.asrAttempt,
      version: draft.version,
      recording: draft.recording,
      transcript: draft.transcript,
      confirmedMessageId: message.id,
      confirmedRunId: run.id,
      messageAudioId: audioId,
      confirmedAt: now,
      createdAt: draft.createdAt,
      updatedAt: now,
    );
    final confirmation = AgentVoiceConfirmation(
      draft: confirmedDraft,
      message: message,
      run: run,
      assistantMessage: assistantMessage,
    );
    _drafts[draft.id] = confirmedDraft;
    _confirmations[operationKey] = confirmation;
    _runs[run.id] = run;
    _messages[message.id] = message;
    _messages[assistantMessage.id] = assistantMessage;
    _audioIds.add(audioId);
    return confirmation;
  }

  @override
  Stream<AgentVoiceConfirmationStreamEvent> confirmDraftStream({
    required String draftId,
    required int draftVersion,
    required String clientMessageId,
    required String confirmedText,
  }) async* {
    final confirmation = await confirmDraft(
      draftId: draftId,
      draftVersion: draftVersion,
      clientMessageId: clientMessageId,
      confirmedText: confirmedText,
    );
    yield AgentVoiceInputCommitted(confirmation);
    if (confirmation.assistantMessage case final assistant?) {
      yield AgentVoiceAssistantOutputStarted(
        runId: confirmation.run.id,
        outputId: assistant.id,
      );
      yield AgentVoiceAssistantOutputDelta(
        runId: confirmation.run.id,
        outputId: assistant.id,
        sequence: 1,
        delta: assistant.text,
      );
      yield AgentVoiceAssistantOutputCompleted(
        runId: confirmation.run.id,
        outputId: assistant.id,
        text: assistant.text,
      );
    }
    yield AgentVoiceRunCompleted(confirmation.run);
  }

  @override
  Future<AgentVoiceRun> getRun({required String runId}) async {
    final generation = _accountGeneration;
    await _wait(generation);
    final run = _runs[runId];
    if (run == null) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    return run;
  }

  @override
  Future<AgentVoiceRun> retryRun({
    required String runId,
    required String clientRetryId,
  }) {
    return getRun(runId: runId);
  }

  @override
  Future<AgentMessage?> getMessage({
    required String threadId,
    required String messageId,
  }) async {
    final generation = _accountGeneration;
    await _wait(generation);
    return _messages[messageId];
  }

  @override
  Future<Uint8List> loadMessageAudio({required String audioId}) async {
    final generation = _accountGeneration;
    await _wait(generation);
    if (!_audioIds.contains(audioId)) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    return Uint8List.fromList(_fakeWaveBytes);
  }

  @override
  Future<void> deleteMessageAudio({required String audioId}) async {
    final generation = _accountGeneration;
    await _wait(generation);
    if (!_audioIds.remove(audioId)) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
  }

  @override
  Future<Uint8List> loadAssistantSpeech({required String messageId}) async {
    final generation = _accountGeneration;
    await _wait(generation);
    if (messageId.trim().isEmpty) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    return Uint8List.fromList(_fakeWaveBytes);
  }

  @override
  Future<Uint8List> loadSpeechPreview({
    required String messageId,
    required String text,
  }) async {
    final generation = _accountGeneration;
    await _wait(generation);
    if (messageId.trim().isEmpty || text.trim().isEmpty) {
      throw const AgentClientException(kind: AgentClientFailureKind.notFound);
    }
    return Uint8List.fromList(_fakeWaveBytes);
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
    _draftSequence = 0;
    _messageSequence = 0;
    _drafts.clear();
    _confirmations.clear();
    _runs.clear();
    _messages.clear();
    _audioIds.clear();
  }

  @override
  Future<void> dispose() async {}

  Future<void> _wait(int generation) async {
    if (delay != Duration.zero) {
      await Future<void>.delayed(delay);
    }
    if (generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }

  String _nextMessageId() => 'message_${++_messageSequence}';
}

const _fakeWaveBytes = <int>[
  0x52,
  0x49,
  0x46,
  0x46,
  0x28,
  0x00,
  0x00,
  0x00,
  0x57,
  0x41,
  0x56,
  0x45,
  0x66,
  0x6d,
  0x74,
  0x20,
  0x10,
  0x00,
  0x00,
  0x00,
  0x01,
  0x00,
  0x01,
  0x00,
  0x80,
  0x3e,
  0x00,
  0x00,
  0x00,
  0x7d,
  0x00,
  0x00,
  0x02,
  0x00,
  0x10,
  0x00,
  0x64,
  0x61,
  0x74,
  0x61,
  0x04,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
];
