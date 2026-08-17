import 'package:speakup/features/agent/conversation/agent_models.dart';

enum AgentVoiceDraftStatus { transcribing, ready, failed, confirmed }

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

final class AgentVoiceDraftFailure {
  const AgentVoiceDraftFailure({required this.kind, required this.retryable});

  final String kind;
  final bool retryable;
}

final class AgentVoiceDraft {
  const AgentVoiceDraft({
    required this.id,
    required this.threadId,
    required this.status,
    required this.asrAttempt,
    required this.version,
    required this.recording,
    required this.createdAt,
    required this.updatedAt,
    this.expiresAt,
    this.transcript,
    this.failure,
    this.confirmedMessageId,
    this.confirmedRunId,
    this.messageAudioId,
    this.confirmedAt,
  });

  final String id;
  final String threadId;
  final AgentVoiceDraftStatus status;
  final int asrAttempt;
  final int version;
  final AgentVoiceRecordingMetadata recording;
  final AgentVoiceTranscript? transcript;
  final AgentVoiceDraftFailure? failure;
  final DateTime? expiresAt;
  final String? confirmedMessageId;
  final String? confirmedRunId;
  final String? messageAudioId;
  final DateTime? confirmedAt;
  final DateTime createdAt;
  final DateTime updatedAt;

  bool get isAsrPending => status == AgentVoiceDraftStatus.transcribing;

  bool get isReady =>
      status == AgentVoiceDraftStatus.ready &&
      transcript != null &&
      version >= 1;
}

sealed class AgentVoiceTranscriptionEvent {
  const AgentVoiceTranscriptionEvent();
}

final class AgentVoiceTranscriptUpdated extends AgentVoiceTranscriptionEvent {
  const AgentVoiceTranscriptUpdated({
    required this.text,
    required this.finalResult,
  });

  final String text;
  final bool finalResult;
}

final class AgentVoiceDraftCompleted extends AgentVoiceTranscriptionEvent {
  const AgentVoiceDraftCompleted(this.draft);

  final AgentVoiceDraft draft;
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
    required this.draft,
    required this.message,
    required this.run,
    this.assistantMessage,
  });

  final AgentVoiceDraft draft;
  final AgentMessage message;
  final AgentVoiceRun run;
  final AgentMessage? assistantMessage;
}

sealed class AgentVoiceConfirmationStreamEvent {
  const AgentVoiceConfirmationStreamEvent();
}

final class AgentVoiceInputCommitted extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceInputCommitted(this.confirmation);

  final AgentVoiceConfirmation confirmation;
}

enum AgentVoiceToolStepStatus { started, completed, failed }

final class AgentVoiceToolStepEvent extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceToolStepEvent({
    required this.runId,
    required this.stepId,
    required this.name,
    required this.status,
  });

  final String runId;
  final String stepId;
  final String name;
  final AgentVoiceToolStepStatus status;
}

final class AgentVoiceAssistantOutputStarted
    extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceAssistantOutputStarted({
    required this.runId,
    required this.outputId,
  });

  final String runId;
  final String outputId;
}

final class AgentVoiceAssistantOutputDelta
    extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceAssistantOutputDelta({
    required this.runId,
    required this.outputId,
    required this.sequence,
    required this.delta,
  });

  final String runId;
  final String outputId;
  final int sequence;
  final String delta;
}

final class AgentVoiceAssistantOutputCompleted
    extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceAssistantOutputCompleted({
    required this.runId,
    required this.outputId,
    required this.text,
  });

  final String runId;
  final String outputId;
  final String text;
}

final class AgentVoiceRunCompleted extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceRunCompleted(this.run);

  final AgentVoiceRun run;
}

final class AgentVoiceRunFailed extends AgentVoiceConfirmationStreamEvent {
  const AgentVoiceRunFailed({
    required this.runId,
    required this.kind,
    required this.retryable,
  });

  final String runId;
  final String kind;
  final bool retryable;
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
