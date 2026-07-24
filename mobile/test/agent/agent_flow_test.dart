import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/app/speak_up_app.dart';
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
