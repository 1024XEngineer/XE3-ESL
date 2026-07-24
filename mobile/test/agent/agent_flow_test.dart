import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  testWidgets('uses one Agent Thread for text, 3 voice turns, and Review', (
    tester,
  ) async {
    final agentController = AgentController(client: FakeAgentClient());
    await tester.pumpWidget(SpeakUpApp(agentController: agentController));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('scene-self-introduction')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('agent-thread-title')), findsOneWidget);
    expect(find.text('英文自我介绍'), findsOneWidget);

    const textMessage = 'Please help me make this answer more specific.';
    await tester.enterText(
      find.byKey(const Key('agent-composer-field')),
      textMessage,
    );
    await tester.pump();
    expect(
      tester
          .widget<IconButton>(find.byKey(const Key('agent-send-button')))
          .onPressed,
      isNotNull,
    );
    await tester.ensureVisible(find.byKey(const Key('agent-send-button')));
    await tester.tap(find.byKey(const Key('agent-send-button')));
    await tester.pumpAndSettle();

    expect(find.text(textMessage), findsOneWidget);
    expect(
      agentController.messages.any((message) => message.text.contains('具体例子')),
      isTrue,
    );
    expect(
      find.byKey(Key('agent-message-${agentController.messages.last.id}')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-page')), findsOneWidget);

    for (var turn = 1; turn <= 3; turn++) {
      await tester.tap(find.byKey(const Key('practice-record')));
      await tester.pump();
      expect(find.byKey(const Key('practice-stop-recording')), findsOneWidget);

      await tester.tap(find.byKey(const Key('practice-stop-recording')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('practice-transcript')), findsOneWidget);

      if (turn == 1) {
        await tester.tap(find.byKey(const Key('practice-rerecord')));
        await tester.pumpAndSettle();
        expect(find.text('0 / 3'), findsOneWidget);
        await tester.tap(find.byKey(const Key('practice-record')));
        await tester.pump();
        await tester.tap(find.byKey(const Key('practice-stop-recording')));
        await tester.pumpAndSettle();
      }

      await tester.tap(find.byKey(const Key('practice-confirm-turn')));
      await tester.pumpAndSettle();
      if (turn < 3) {
        expect(find.text('$turn / 3'), findsOneWidget);
      }
    }

    expect(find.byKey(const Key('practice-page')), findsNothing);
    expect(find.byKey(const Key('review-content')), findsOneWidget);
    expect(find.textContaining('三轮复盘'), findsOneWidget);
  });

  testWidgets(
    'global AuthGate shows the real email and logout clears Agent data',
    (tester) async {
      final agentController = AgentController(client: FakeAgentClient());
      final authController = AuthController(
        identityClient: _AuthenticatedIdentityClient(),
        sessionStore: _MemorySessionStore('sess_stored-token'),
        clearPrivateState: agentController.clearPrivateState,
      );

      await tester.pumpWidget(
        SpeakUpApp(
          authController: authController,
          agentController: agentController,
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(agentController.threadId, isNotNull);

      await tester.tap(find.byKey(const Key('primary-tab-profile')));
      await tester.pumpAndSettle();
      expect(find.text('learner@example.com'), findsOneWidget);

      await tester.tap(find.byKey(const Key('profile-logout-button')));
      await tester.pumpAndSettle();

      expect(find.text('Welcome back'), findsOneWidget);
      expect(find.byKey(const Key('primary-navigation')), findsNothing);
      expect(agentController.threadId, isNull);
      expect(agentController.messages, isEmpty);
    },
  );

  testWidgets('logout removes the entire Navigator from a deep private route', (
    tester,
  ) async {
    final agentController = AgentController(client: FakeAgentClient());
    final authController = AuthController(
      identityClient: _AuthenticatedIdentityClient(),
      sessionStore: _MemorySessionStore('sess_stored-token'),
      clearPrivateState: agentController.clearPrivateState,
    );
    await tester.pumpWidget(
      SpeakUpApp(
        authController: authController,
        agentController: agentController,
      ),
    );
    await tester.pumpAndSettle();

    final shellContext = tester.element(find.byType(SpeakUpShell));
    Navigator.of(shellContext).pushNamed(AppRoutes.practice);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-page')), findsOneWidget);

    await authController.logout();
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-page')), findsNothing);
    expect(find.byType(SpeakUpShell), findsNothing);
    expect(find.byKey(const Key('primary-navigation')), findsNothing);
    expect(find.text('Welcome back'), findsOneWidget);
  });

  testWidgets(
    'scene failure exposes a retry that completes the same operation',
    (tester) async {
      final client = _FailOnceSceneClient();
      final agentController = AgentController(client: client);
      await tester.pumpWidget(SpeakUpApp(agentController: agentController));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('primary-tab-scenes')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('scene-self-introduction')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('scene-operation-error')), findsOneWidget);
      expect(find.byKey(const Key('scene-retry-operation')), findsOneWidget);

      await tester.tap(find.byKey(const Key('scene-retry-operation')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-thread-title')), findsOneWidget);
      expect(find.text('英文自我介绍'), findsOneWidget);
      expect(client.sceneClientIds, hasLength(2));
      expect(client.sceneClientIds.toSet(), hasLength(1));
    },
  );

  testWidgets('restored Review opens directly and an existing practice exits', (
    tester,
  ) async {
    final agentController = AgentController(client: FakeAgentClient());
    await agentController.initialize();
    await agentController.selectScene(agentScenes.first);
    for (var turn = 0; turn < 3; turn++) {
      agentController.startRecording();
      await agentController.stopRecording();
      await agentController.confirmTranscript();
    }
    expect(agentController.review, isNotNull);

    await tester.pumpWidget(SpeakUpApp(agentController: agentController));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('review-content')), findsOneWidget);

    final navigatorContext = tester.element(find.byType(SpeakUpShell));
    Navigator.of(navigatorContext).push(
      MaterialPageRoute<void>(
        builder: (_) => PracticePage(agentController: agentController),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-page')), findsNothing);
  });
}

final class _AuthenticatedIdentityClient implements IdentityClient {
  static const user = User(id: 'user_1', email: 'learner@example.com');

  @override
  Future<User> currentUser({required String sessionToken}) async => user;

  @override
  Future<void> logout({required String sessionToken}) async {}

  @override
  Future<LoginResult> login({required String email, required String password}) {
    throw UnimplementedError();
  }

  @override
  Future<User> register({required String email, required String password}) {
    throw UnimplementedError();
  }
}

final class _MemorySessionStore implements SessionStore {
  _MemorySessionStore(this.token);

  String? token;

  @override
  Future<void> deleteToken() async {
    token = null;
  }

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String token) async {
    this.token = token;
  }
}

final class _FailOnceSceneClient implements AgentClient {
  final FakeAgentClient _delegate = FakeAgentClient();
  final List<String> sceneClientIds = <String>[];

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    return _delegate.createReview(
      threadId: threadId,
      scene: scene,
      clientReviewId: clientReviewId,
    );
  }

  @override
  Future<AgentThreadSnapshot> restoreThread() => _delegate.restoreThread();

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    return _delegate.sendText(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
    );
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    sceneClientIds.add(clientOperationId);
    if (sceneClientIds.length == 1) {
      throw StateError('temporary scene failure');
    }
    return _delegate.startScene(
      threadId: threadId,
      scene: scene,
      clientOperationId: clientOperationId,
    );
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    return _delegate.submitPracticeTurn(
      threadId: threadId,
      scene: scene,
      turnNumber: turnNumber,
      transcript: transcript,
      clientTurnId: clientTurnId,
    );
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    return _delegate.transcribeTurn(
      threadId: threadId,
      turnNumber: turnNumber,
      clientTurnId: clientTurnId,
    );
  }
}
