import 'agent_models.dart';

abstract interface class AgentClient {
  Future<AgentThreadSnapshot> restoreThread();

  Future<AgentMessage> startScene({
    required String threadId,
    required AgentScene scene,
  });

  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
  });

  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
  });

  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
  });

  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
  });
}

final class FakeAgentClient implements AgentClient {
  FakeAgentClient({this.delay = Duration.zero});

  final Duration delay;
  int _messageSequence = 0;

  @override
  Future<AgentThreadSnapshot> restoreThread() async {
    await _wait();
    return const AgentThreadSnapshot(threadId: 'thread_local_preview');
  }

  @override
  Future<AgentMessage> startScene({
    required String threadId,
    required AgentScene scene,
  }) async {
    await _wait();
    return AgentMessage(
      id: _nextMessageId(),
      role: AgentMessageRole.assistant,
      text: '我们开始“${scene.title}”。第一轮：请先用英文回答，你希望面试官首先了解你的哪段经历？',
    );
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
  }) async {
    await _wait();
    return AgentExchange(
      userMessage: AgentMessage(
        id: _nextMessageId(),
        role: AgentMessageRole.user,
        text: text,
      ),
      assistantMessage: AgentMessage(
        id: _nextMessageId(),
        role: AgentMessageRole.assistant,
        text: '我会围绕这点继续追问。你能补充一个具体例子和最终结果吗？',
      ),
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
  }) async {
    await _wait();
    return switch (turnNumber) {
      1 =>
        'I led the backend migration and kept the rollout safe with staged checks.',
      2 =>
        'The main trade-off was delivery speed versus reliability, so I reduced the scope first.',
      _ =>
        'The result was a stable release, and I learned to communicate risks much earlier.',
    };
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
  }) async {
    await _wait();
    final nextQuestion = switch (turnNumber) {
      1 => '第二轮：当时最困难的取舍是什么？请说明你为什么这样决定。',
      2 => '第三轮：结果如何？如果再做一次，你会改变什么？',
      _ => null,
    };
    return AgentExchange(
      userMessage: AgentMessage(
        id: _nextMessageId(),
        role: AgentMessageRole.user,
        text: transcript,
      ),
      assistantMessage: nextQuestion == null
          ? null
          : AgentMessage(
              id: _nextMessageId(),
              role: AgentMessageRole.assistant,
              text: nextQuestion,
            ),
    );
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
  }) async {
    await _wait();
    return AgentReview(
      id: 'review_$threadId',
      title: '${scene.title} · 三轮复盘',
      summary: '你已经能按“背景—行动—结果”组织回答，整体表达连贯。',
      strength: '能够说明自己的责任，并给出具体取舍。',
      nextFocus: '下一次把结果量化，同时缩短开场句。',
    );
  }

  String _nextMessageId() => 'message_${++_messageSequence}';

  Future<void> _wait() {
    if (delay == Duration.zero) {
      return Future<void>.value();
    }
    return Future<void>.delayed(delay);
  }
}
