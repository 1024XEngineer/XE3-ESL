import 'agent_models.dart';

enum AgentVoiceCandidateStatus {
  staged,
  transcribing,
  candidateReady,
  failed,
  confirming,
  confirmed,
  deleting,
  deleted,
}

final class AgentVoiceLocalRecording {
  const AgentVoiceLocalRecording({
    required this.path,
    required this.contentType,
    required this.sizeBytes,
    required this.duration,
  });

  final String path;
  final String contentType;
  final int sizeBytes;
  final Duration duration;
}

final class AgentVoiceRecordingMetadata {
  const AgentVoiceRecordingMetadata({
    required this.contentType,
    required this.sizeBytes,
    required this.duration,
    required this.sampleRate,
  });

  final String contentType;
  final int sizeBytes;
  final Duration duration;
  final int sampleRate;
}

final class AgentVoiceTranscript {
  const AgentVoiceTranscript({
    required this.text,
    required this.requestId,
    required this.provider,
    required this.model,
    this.language,
    this.emotion,
    this.finishReason,
  });

  final String text;
  final String requestId;
  final String provider;
  final String model;
  final String? language;
  final String? emotion;
  final String? finishReason;
}

final class AgentVoiceCandidateFailure {
  const AgentVoiceCandidateFailure({
    required this.kind,
    required this.retryable,
  });

  final String kind;
  final bool retryable;
}

final class AgentVoiceCandidate {
  const AgentVoiceCandidate({
    required this.id,
    required this.threadId,
    required this.status,
    required this.asrAttempt,
    required this.version,
    required this.recording,
    required this.expiresAt,
    required this.createdAt,
    required this.updatedAt,
    this.transcript,
    this.failure,
    this.confirmedMessageId,
    this.confirmedRunId,
    this.messageAudioId,
    this.confirmedAt,
    this.deletedAt,
  });

  final String id;
  final String threadId;
  final AgentVoiceCandidateStatus status;
  final int asrAttempt;
  final int version;
  final AgentVoiceRecordingMetadata recording;
  final AgentVoiceTranscript? transcript;
  final AgentVoiceCandidateFailure? failure;
  final DateTime expiresAt;
  final String? confirmedMessageId;
  final String? confirmedRunId;
  final String? messageAudioId;
  final DateTime? confirmedAt;
  final DateTime? deletedAt;
  final DateTime createdAt;
  final DateTime updatedAt;

  bool get isAsrPending =>
      status == AgentVoiceCandidateStatus.staged ||
      status == AgentVoiceCandidateStatus.transcribing;

  bool get isReady =>
      status == AgentVoiceCandidateStatus.candidateReady &&
      transcript != null &&
      version >= 1;
}

enum AgentVoiceRunStatus { pending, running, completed, failed }

final class AgentVoiceRun {
  const AgentVoiceRun({
    required this.id,
    required this.threadId,
    required this.inputMessageId,
    required this.status,
    this.assistantMessageId,
    this.failureKind,
    this.failureRetryable = false,
  });

  final String id;
  final String threadId;
  final String inputMessageId;
  final AgentVoiceRunStatus status;
  final String? assistantMessageId;
  final String? failureKind;
  final bool failureRetryable;

  bool get isTerminal =>
      status == AgentVoiceRunStatus.completed ||
      status == AgentVoiceRunStatus.failed;
}

final class AgentVoiceConfirmation {
  const AgentVoiceConfirmation({
    required this.candidate,
    required this.message,
    required this.run,
    this.assistantMessage,
  });

  final AgentVoiceCandidate candidate;
  final AgentMessage message;
  final AgentVoiceRun run;
  final AgentMessage? assistantMessage;
}

enum AgentVoiceComposerState {
  idle,
  starting,
  recording,
  recorded,
  uploading,
  transcribing,
  awaitingConfirmation,
  confirming,
  awaitingAssistant,
  failed,
}
