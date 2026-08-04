import 'package:speakup/features/coaching/goal/goal.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';

enum AgentMessageRole { user, assistant }

enum AgentMessageModality { text, voice, multimodal }

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

enum AgentImageAssetStatus { staged, attached, deleting, deleted }

final class AgentImageAsset {
  const AgentImageAsset({
    required this.id,
    required this.threadId,
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
  final String threadId;
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
      (status == AgentImageAssetStatus.staged ||
          status == AgentImageAssetStatus.attached) &&
      contentUrl != null &&
      contentExpiresAt?.isAfter(DateTime.now().toUtc()) == true;

  AgentImageAsset withContent({
    required Uri contentUrl,
    required DateTime expiresAt,
  }) {
    return AgentImageAsset(
      id: id,
      threadId: threadId,
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

enum PracticeRecordingState {
  idle,
  starting,
  recording,
  transcribing,
  awaitingConfirmation,
  submitting,
  completed,
}

final class AgentMessage {
  const AgentMessage({
    required this.id,
    required this.role,
    required this.text,
    this.sequence,
    this.createdAt,
    this.modality = AgentMessageModality.text,
    this.audio,
    this.images = const <AgentImageAsset>[],
    this.isStreaming = false,
    this.hasFailed = false,
    this.handoffs = const <AgentHandoff>[],
    this.speechFeedbackStatusUrl,
  }) : assert(
         (modality == AgentMessageModality.voice && audio != null) ||
             (modality != AgentMessageModality.voice && audio == null),
       );

  final String id;
  final AgentMessageRole role;
  final String text;
  final int? sequence;
  final DateTime? createdAt;
  final AgentMessageModality modality;
  final AgentMessageAudio? audio;
  final List<AgentImageAsset> images;
  final bool isStreaming;
  final bool hasFailed;
  final List<AgentHandoff> handoffs;
  final String? speechFeedbackStatusUrl;

  AgentMessage copyWith({
    String? id,
    String? text,
    AgentMessageAudio? audio,
    bool clearAudio = false,
    List<AgentImageAsset>? images,
    bool? isStreaming,
    bool? hasFailed,
    List<AgentHandoff>? handoffs,
    String? speechFeedbackStatusUrl,
    bool clearSpeechFeedbackStatusUrl = false,
  }) {
    return AgentMessage(
      id: id ?? this.id,
      role: role,
      text: text ?? this.text,
      sequence: sequence,
      createdAt: createdAt,
      modality: clearAudio ? AgentMessageModality.text : modality,
      audio: clearAudio ? null : audio ?? this.audio,
      images: images ?? this.images,
      isStreaming: isStreaming ?? this.isStreaming,
      hasFailed: hasFailed ?? this.hasFailed,
      handoffs: handoffs ?? this.handoffs,
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
    this.activeGoalId,
  });

  final String id;
  final String? title;
  final String? activeGoalId;
  final DateTime createdAt;
  final DateTime updatedAt;
}

final class AgentThreadPage {
  const AgentThreadPage({
    required this.threads,
    this.focusedThreadId,
    this.nextCursor,
  });

  final List<AgentThreadSummary> threads;
  final String? focusedThreadId;
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
    this.activeGoal,
    this.textRecovery,
    this.messages = const <AgentMessage>[],
    this.createdAt,
    this.updatedAt,
    this.nextMessageCursor,
  });

  final String threadId;
  final String? title;
  final Goal? activeGoal;
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

final class AgentReview {
  const AgentReview({
    required this.id,
    required this.title,
    required this.summary,
    required this.strength,
    required this.nextFocus,
  });

  final String id;
  final String title;
  final String summary;
  final String strength;
  final String nextFocus;
}
