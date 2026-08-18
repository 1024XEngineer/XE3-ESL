import 'package:speakup/features/agent/client_action/agent_client_action.dart';

const agentMaximumImagesPerMessage = 4;
const agentMaximumImageBytes = 10 * 1024 * 1024;

enum AgentMessageRole { user, assistant }

enum AgentMessageModality { text, voice, multimodal }

enum AgentRunStatus { pending, running, completed, failed }

final class AgentRunUsage {
  const AgentRunUsage({
    required this.inputTokens,
    required this.outputTokens,
    required this.totalTokens,
  });

  final int inputTokens;
  final int outputTokens;
  final int totalTokens;
}

sealed class AgentRunCompletion {
  const AgentRunCompletion();
}

final class AgentModelRunCompletion extends AgentRunCompletion {
  const AgentModelRunCompletion({
    required this.providerCompletionId,
    required this.providerModel,
    required this.finishReason,
    required this.usage,
  });

  final String providerCompletionId;
  final String providerModel;
  final String finishReason;
  final AgentRunUsage usage;
}

final class AgentDomainRunCompletion extends AgentRunCompletion {
  const AgentDomainRunCompletion({
    required this.toolCallId,
    required this.toolName,
  });

  final String toolCallId;
  final String toolName;
}

final class AgentRunFailure {
  const AgentRunFailure({required this.kind, required this.retryable});

  final String kind;
  final bool retryable;
}

/// The authoritative durable Run returned by the Agent API.
///
/// Text and voice transports share this model. Presentation work such as
/// Message hydration and TTS happens after a Run reaches a terminal status and
/// must not change that durable outcome.
final class AgentRun {
  const AgentRun({
    required this.id,
    required this.threadId,
    required this.inputMessageId,
    required this.attempt,
    required this.status,
    required this.requestedProvider,
    required this.requestedModel,
    required this.maxOutputTokens,
    required this.createdAt,
    required this.updatedAt,
    this.retryOfRunId,
    this.clientRetryId,
    this.assistantMessageId,
    this.completion,
    this.failure,
    this.startedAt,
    this.completedAt,
  });

  final String id;
  final String threadId;
  final String inputMessageId;
  final int attempt;
  final String? retryOfRunId;
  final String? clientRetryId;
  final AgentRunStatus status;
  final String requestedProvider;
  final String requestedModel;
  final int maxOutputTokens;
  final String? assistantMessageId;
  final AgentRunCompletion? completion;
  final AgentRunFailure? failure;
  final DateTime createdAt;
  final DateTime? startedAt;
  final DateTime? completedAt;
  final DateTime updatedAt;

  bool get isTerminal =>
      status == AgentRunStatus.completed || status == AgentRunStatus.failed;

  String? get failureKind => failure?.kind;

  bool get failureRetryable => failure?.retryable ?? false;
}

bool validAgentSpeechFeedbackStatusUrl(String value) =>
    _agentSpeechFeedbackStatusUrlPattern.hasMatch(value);

enum AgentMessageAudioStatus { readable, deleting, deleted }

final class AgentMessageAudio {
  const AgentMessageAudio({
    required this.id,
    required this.status,
    required this.contentType,
    required this.sizeBytes,
    required this.duration,
    this.playbackPath,
    this.deletedAt,
  });

  final String id;
  final AgentMessageAudioStatus status;
  final String contentType;
  final int sizeBytes;
  final Duration duration;
  final String? playbackPath;
  final DateTime? deletedAt;

  bool get isReadable =>
      status == AgentMessageAudioStatus.readable && playbackPath != null;

  AgentMessageAudio copyWith({
    AgentMessageAudioStatus? status,
    String? playbackPath,
    bool clearPlaybackPath = false,
    DateTime? deletedAt,
  }) {
    return AgentMessageAudio(
      id: id,
      status: status ?? this.status,
      contentType: contentType,
      sizeBytes: sizeBytes,
      duration: duration,
      playbackPath: clearPlaybackPath
          ? null
          : playbackPath ?? this.playbackPath,
      deletedAt: deletedAt ?? this.deletedAt,
    );
  }
}

final _agentSpeechFeedbackStatusUrlPattern = RegExp(
  r'^/v1/agent-messages/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/evaluation$',
);

enum AgentImageAssetStatus { staged, ready, deleting }

final class AgentImageAsset {
  const AgentImageAsset({
    required this.id,
    required this.contentType,
    required this.sizeBytes,
    required this.width,
    required this.height,
    required this.status,
    required this.createdAt,
    this.attachedAt,
    this.contentUrl,
    this.contentExpiresAt,
  });

  final String id;
  final String contentType;
  final int sizeBytes;
  final int width;
  final int height;
  final AgentImageAssetStatus status;
  final DateTime createdAt;
  final DateTime? attachedAt;
  final Uri? contentUrl;
  final DateTime? contentExpiresAt;

  bool get isReadable =>
      status == AgentImageAssetStatus.ready &&
      contentUrl != null &&
      contentExpiresAt?.isAfter(DateTime.now().toUtc()) == true;

  AgentImageAsset withContent({
    required Uri contentUrl,
    required DateTime expiresAt,
  }) {
    return AgentImageAsset(
      id: id,
      contentType: contentType,
      sizeBytes: sizeBytes,
      width: width,
      height: height,
      status: status,
      createdAt: createdAt,
      attachedAt: attachedAt,
      contentUrl: contentUrl,
      contentExpiresAt: expiresAt,
    );
  }
}

final class AgentMessage {
  const AgentMessage({
    required this.id,
    required this.role,
    required this.text,
    this.sequence,
    this.createdAt,
    this.clientMessageId,
    this.producedByRunId,
    this.modality = AgentMessageModality.text,
    this.audio,
    this.images = const <AgentImageAsset>[],
    this.isStreaming = false,
    this.hasFailed = false,
    this.clientActions = const <AgentClientAction>[],
    this.speechFeedbackStatusUrl,
  }) : assert(modality == AgentMessageModality.voice || audio == null);

  final String id;
  final AgentMessageRole role;
  final String text;
  final int? sequence;
  final DateTime? createdAt;
  final String? clientMessageId;
  final String? producedByRunId;
  final AgentMessageModality modality;
  final AgentMessageAudio? audio;
  final List<AgentImageAsset> images;
  final bool isStreaming;
  final bool hasFailed;
  final List<AgentClientAction> clientActions;
  final String? speechFeedbackStatusUrl;

  AgentMessage copyWith({
    String? id,
    String? text,
    AgentMessageAudio? audio,
    bool clearAudio = false,
    List<AgentImageAsset>? images,
    String? clientMessageId,
    String? producedByRunId,
    bool? isStreaming,
    bool? hasFailed,
    List<AgentClientAction>? clientActions,
    String? speechFeedbackStatusUrl,
    bool clearSpeechFeedbackStatusUrl = false,
  }) {
    return AgentMessage(
      id: id ?? this.id,
      role: role,
      text: text ?? this.text,
      sequence: sequence,
      createdAt: createdAt,
      clientMessageId: clientMessageId ?? this.clientMessageId,
      producedByRunId: producedByRunId ?? this.producedByRunId,
      modality: clearAudio ? AgentMessageModality.text : modality,
      audio: clearAudio ? null : audio ?? this.audio,
      images: images ?? this.images,
      isStreaming: isStreaming ?? this.isStreaming,
      hasFailed: hasFailed ?? this.hasFailed,
      clientActions: clientActions ?? this.clientActions,
      speechFeedbackStatusUrl: clearSpeechFeedbackStatusUrl
          ? null
          : speechFeedbackStatusUrl ?? this.speechFeedbackStatusUrl,
    );
  }
}

/// One durable Agent Thread as returned by the bounded history endpoint.
///
/// Thread titles are server-owned and may be absent until the first committed
/// user Message. Clients never infer a title from local Message content.
final class AgentThreadSummary {
  const AgentThreadSummary({
    required this.id,
    required this.createdAt,
    required this.updatedAt,
    this.title,
  });

  final String id;
  final String? title;
  final DateTime createdAt;
  final DateTime updatedAt;
}

final class AgentThreadPage {
  const AgentThreadPage({required this.threads, this.nextCursor});

  final List<AgentThreadSummary> threads;
  final String? nextCursor;
}

final class AgentMessagePage {
  const AgentMessagePage({required this.messages, this.nextCursor});

  final List<AgentMessage> messages;
  final String? nextCursor;
}

final class AgentThreadSnapshot {
  const AgentThreadSnapshot({
    required this.threadId,
    this.title,
    this.textRecovery,
    this.messages = const <AgentMessage>[],
    this.createdAt,
    this.updatedAt,
    this.nextMessageCursor,
  });

  final String threadId;
  final String? title;
  final AgentTextRecovery? textRecovery;
  final List<AgentMessage> messages;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final String? nextMessageCursor;
}

/// A server-restored failed text operation that keeps its idempotency identity.
///
/// This is a transient presentation projection. The durable Message and Run
/// remain authoritative on the server.
final class AgentTextRecovery {
  const AgentTextRecovery({
    required this.text,
    required this.clientMessageId,
    required this.failureKind,
    required this.retryable,
    this.imageAssetIds = const <String>[],
  });

  final String text;
  final String clientMessageId;
  final String failureKind;
  final bool retryable;
  final List<String> imageAssetIds;
}

final class AgentExchange {
  const AgentExchange({required this.userMessage, this.assistantMessage});

  final AgentMessage userMessage;
  final AgentMessage? assistantMessage;
}
