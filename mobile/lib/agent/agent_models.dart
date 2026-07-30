enum AgentMessageRole { user, assistant }

enum AgentMessageModality { text, voice, multimodal }

enum AgentMessageAudioStatus { readable, deleting, deleted }

enum AgentMessageActionType { openInterviewPreparation }

final class AgentMessageAction {
  const AgentMessageAction({
    required this.type,
    required this.label,
    required this.matterId,
    required this.title,
  });

  final AgentMessageActionType type;
  final String label;
  final String matterId;
  final String title;
}

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
  reviewFailed,
  completed,
}

final class AgentScene {
  const AgentScene({
    required this.id,
    required this.title,
    required this.description,
  });

  final String id;
  final String title;
  final String description;
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
    this.actions = const <AgentMessageAction>[],
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
  final List<AgentMessageAction> actions;

  AgentMessage copyWith({
    AgentMessageAudio? audio,
    bool clearAudio = false,
    List<AgentImageAsset>? images,
  }) {
    return AgentMessage(
      id: id,
      role: role,
      text: text,
      sequence: sequence,
      createdAt: createdAt,
      modality: clearAudio ? AgentMessageModality.text : modality,
      audio: clearAudio ? null : audio ?? this.audio,
      images: images ?? this.images,
      actions: actions,
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
    this.activeMatterId,
  });

  final String id;
  final String? title;
  final String? activeMatterId;
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

/// The user-owned Matter currently selected for one durable Agent Thread.
///
/// [scene] remains a presentation model. [id] is the opaque resource identity
/// that a future real client obtains from the backend.
final class AgentMatter {
  const AgentMatter({
    required this.id,
    required this.scene,
    this.status,
    this.version,
    this.createdAt,
    this.updatedAt,
  });

  final String id;
  final AgentScene scene;
  final String? status;
  final int? version;
  final DateTime? createdAt;
  final DateTime? updatedAt;
}

/// The smallest server-authoritative practice projection needed after restart.
///
/// Transient recorder states are deliberately not persisted here. A restored
/// client resumes from the number of committed Turns and either the existing
/// Review or the stable pending Review request identity.
final class AgentPracticeSnapshot {
  const AgentPracticeSnapshot({
    required this.completedTurns,
    this.turnLimit = 3,
    bool? sessionCompleted,
    this.review,
    this.pendingReviewClientId,
  }) : sessionCompleted = sessionCompleted ?? completedTurns == turnLimit,
       assert(turnLimit >= 1 && turnLimit <= 14),
       assert(completedTurns >= 0 && completedTurns <= turnLimit),
       assert(
         review == null || (sessionCompleted ?? completedTurns == turnLimit),
       );

  final int completedTurns;
  final int turnLimit;
  final bool sessionCompleted;
  final AgentReview? review;
  final String? pendingReviewClientId;
}

final class AgentThreadSnapshot {
  const AgentThreadSnapshot({
    required this.threadId,
    this.title,
    this.activeMatter,
    this.practice,
    this.textRecovery,
    this.messages = const <AgentMessage>[],
    this.createdAt,
    this.updatedAt,
    this.nextMessageCursor,
  }) : assert(practice == null || activeMatter != null);

  final String threadId;
  final String? title;
  final AgentMatter? activeMatter;
  final AgentPracticeSnapshot? practice;
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

final class AgentSceneStart {
  const AgentSceneStart({
    required this.activeMatter,
    required this.assistantMessage,
  });

  final AgentMatter activeMatter;
  final AgentMessage assistantMessage;
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

const agentScenes = <AgentScene>[
  AgentScene(
    id: 'self-introduction',
    title: '英文自我介绍',
    description: '用清楚的结构介绍背景、优势和求职目标。',
  ),
  AgentScene(
    id: 'behavioral-interview',
    title: '行为面试',
    description: '使用具体经历回答协作、冲突和成长类问题。',
  ),
  AgentScene(
    id: 'project-deep-dive',
    title: '项目经历深挖',
    description: '练习讲清问题、方案、取舍和最终结果。',
  ),
  AgentScene(
    id: 'technical-qa',
    title: '技术问答',
    description: '围绕高频基础题练习准确、简洁的英文表达。',
  ),
];
