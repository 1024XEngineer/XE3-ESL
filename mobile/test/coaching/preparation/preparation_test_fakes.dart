import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/coaching/goal/goal.dart';
import 'package:speakup/features/coaching/goal/goal_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

/// Test client that mirrors the production Agent + Goal transport boundary.
///
/// Goal activation updates the server-owned Thread projection. Tests apply the
/// confirmed identity to [ConversationController] before committing Practice.
final class GoalAwareAgentClient implements AgentClient, GoalActivationClient {
  GoalAwareAgentClient({FakeAgentClient? delegate})
    : _delegate = delegate ?? FakeAgentClient();

  final FakeAgentClient _delegate;
  final Map<String, String> _activeGoalIds = <String, String>{};
  final Map<String, Goal> _goals = <String, Goal>{};
  final Map<(String, String), ({String sceneId, Goal goal})> _activations =
      <(String, String), ({String sceneId, Goal goal})>{};
  int _goalSequence = 0;

  @override
  Future<void> clearAccountState() async {
    _activeGoalIds.clear();
    _goals.clear();
    _activations.clear();
    _goalSequence = 0;
    await _delegate.clearAccountState();
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) => _delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
    imageAssetIds: imageAssetIds,
  );

  @override
  Future<AgentThreadPage> listThreads({
    int pageSize = 20,
    String? cursor,
  }) async {
    final page = await _delegate.listThreads(
      pageSize: pageSize,
      cursor: cursor,
    );
    return AgentThreadPage(
      threads: page.threads.map(_summaryWithGoal).toList(growable: false),
      focusedThreadId: page.focusedThreadId,
      nextCursor: page.nextCursor,
    );
  }

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() async {
    final snapshot = await _delegate.getFocusedThread();
    return snapshot == null ? null : _snapshotWithGoal(snapshot);
  }

  @override
  Future<AgentThreadSummary> createThread() async =>
      _summaryWithGoal(await _delegate.createThread());

  @override
  Future<AgentThreadSnapshot> setFocusedThread({
    required String threadId,
  }) async =>
      _snapshotWithGoal(await _delegate.setFocusedThread(threadId: threadId));

  @override
  Future<void> clearFocusedThread() => _delegate.clearFocusedThread();

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) => _delegate.listMessages(
    threadId: threadId,
    pageSize: pageSize,
    cursor: cursor,
  );

  @override
  Future<void> deleteThread({required String threadId}) async {
    await _delegate.deleteThread(threadId: threadId);
    _activeGoalIds.remove(threadId);
  }

  @override
  Future<Goal> startScene({
    required String threadId,
    required SceneDefinition scene,
    required String clientOperationId,
  }) async {
    await _requireThread(threadId);
    final key = (threadId, clientOperationId);
    final existing = _activations[key];
    if (existing != null) {
      if (existing.sceneId != scene.id) {
        throw StateError(
          'Goal activation identity was reused for a new Scene.',
        );
      }
      _activeGoalIds[threadId] = existing.goal.id;
      return existing.goal;
    }
    final now = DateTime.now().toUtc();
    final goal = Goal(
      id: 'goal_test_${++_goalSequence}',
      title: scene.name,
      status: GoalStatus.active,
      version: 1,
      createdAt: now,
      updatedAt: now,
    );
    _goals[goal.id] = goal;
    _activations[key] = (sceneId: scene.id, goal: goal);
    _activeGoalIds[threadId] = goal.id;
    return goal;
  }

  @override
  Future<Goal> selectExistingGoal({
    required String threadId,
    required String goalId,
  }) async {
    await _requireThread(threadId);
    final goal = _goals[goalId];
    if (goal == null) {
      throw StateError('Unknown test Goal.');
    }
    _activeGoalIds[threadId] = goalId;
    return goal;
  }

  Future<void> _requireThread(String threadId) async {
    final page = await _delegate.listThreads(pageSize: 100);
    if (!page.threads.any((thread) => thread.id == threadId)) {
      throw StateError('Unknown test Agent Thread.');
    }
  }

  AgentThreadSummary _summaryWithGoal(AgentThreadSummary thread) =>
      AgentThreadSummary(
        id: thread.id,
        title: thread.title,
        activeGoalId: _activeGoalIds[thread.id],
        createdAt: thread.createdAt,
        updatedAt: thread.updatedAt,
      );

  AgentThreadSnapshot _snapshotWithGoal(AgentThreadSnapshot snapshot) =>
      AgentThreadSnapshot(
        threadId: snapshot.threadId,
        title: snapshot.title,
        activeGoalId: _activeGoalIds[snapshot.threadId],
        textRecovery: snapshot.textRecovery,
        messages: snapshot.messages,
        createdAt: snapshot.createdAt,
        updatedAt: snapshot.updatedAt,
        nextMessageCursor: snapshot.nextMessageCursor,
      );
}

Future<Goal> activateTestGoal({
  required GoalActivationClient goalClient,
  required ConversationController conversationController,
  required String threadId,
  required SceneDefinition scene,
  required String clientOperationId,
}) async {
  final goal = await goalClient.startScene(
    threadId: threadId,
    scene: scene,
    clientOperationId: clientOperationId,
  );
  conversationController.applyActiveGoal(threadId: threadId, goalId: goal.id);
  if (conversationController.activeGoalId != goal.id) {
    throw StateError(
      'The activated Goal was not restored on its Agent Thread.',
    );
  }
  return goal;
}
