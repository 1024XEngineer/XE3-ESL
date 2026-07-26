enum AgentMessageRole { user, assistant }

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
  });

  final String id;
  final AgentMessageRole role;
  final String text;
  final int? sequence;
  final DateTime? createdAt;
}

/// One durable Agent Thread as returned by the bounded history endpoint.
///
/// Threads deliberately have no client-invented title, summary, archive, or
/// unread state. The Drawer presents the server-owned update time instead.
final class AgentThreadSummary {
  const AgentThreadSummary({
    required this.id,
    required this.createdAt,
    required this.updatedAt,
    this.activeMatterId,
  });

  final String id;
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
    this.review,
    this.pendingReviewClientId,
  }) : assert(completedTurns >= 0 && completedTurns <= 3),
       assert(review == null || completedTurns == 3);

  final int completedTurns;
  final AgentReview? review;
  final String? pendingReviewClientId;
}

final class AgentThreadSnapshot {
  const AgentThreadSnapshot({
    required this.threadId,
    this.activeMatter,
    this.practice,
    this.textRecovery,
    this.messages = const <AgentMessage>[],
    this.createdAt,
    this.updatedAt,
    this.nextMessageCursor,
  }) : assert(practice == null || activeMatter != null);

  final String threadId;
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
  });

  final String text;
  final String clientMessageId;
  final String failureKind;
  final bool retryable;
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
