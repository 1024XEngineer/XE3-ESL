enum AgentMessageRole { user, assistant }

enum PracticeRecordingState {
  idle,
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
  });

  final String id;
  final AgentMessageRole role;
  final String text;
}

final class AgentThreadSnapshot {
  const AgentThreadSnapshot({
    required this.threadId,
    this.scene,
    this.messages = const <AgentMessage>[],
  });

  final String threadId;
  final AgentScene? scene;
  final List<AgentMessage> messages;
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
