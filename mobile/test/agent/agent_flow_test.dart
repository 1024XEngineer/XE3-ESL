import '../support/scene_fixtures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_client.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/coaching/practice/practice.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import '../support/practice_fixtures.dart';

void main() {
  testWidgets('uses one Agent Thread for text and 3 Practice turns', (
    tester,
  ) async {
    final scene = testScenes.first;
    final harness = _agentHarness(
      practiceClient: FakePracticeClient(
        sceneFamily: scene.family,
        sceneModel: scene.model,
      ),
    );
    await harness.conversation.initialize();
    addTearDown(harness.dispose);
    await tester.pumpWidget(
      SpeakUpApp.preview(
        conversationController: harness.conversation,
        composerController: harness.composer,
        practiceController: harness.practice,
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
    expect(harness.conversation.threadId, isNotNull);

    const textMessage = 'Please help me make this answer more specific.';
    await tester.tap(find.byKey(const Key('agent-show-text-composer')));
    await tester.pump();
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
      harness.conversation.messages.any(
        (message) => message.text.contains('具体例子'),
      ),
      isTrue,
    );
    expect(
      find.byKey(Key('agent-message-${harness.conversation.messages.last.id}')),
      findsOneWidget,
    );

    await activateTestPractice(controller: harness.practice, scene: scene);
    final shellContext = tester.element(find.byType(SpeakUpShell));
    Navigator.of(shellContext).pushNamed(AppRoutes.practice);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-page')), findsOneWidget);

    await tester.tap(find.byKey(const Key('practice-record')));
    await tester.pumpAndSettle();
    expect(harness.practice.recordingState, PracticeRecordingState.recording);
    expect(find.text('点击发送语音'), findsOneWidget);
    expect(
      find.byKey(const Key('practice-voice-target-cancel')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('practice-voice-target-cancel')));
    await tester.pumpAndSettle();
    expect(harness.practice.recordingState, PracticeRecordingState.idle);
    expect(find.byKey(const Key('practice-transcript')), findsNothing);

    await tester.tap(find.byKey(const Key('practice-record')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('practice-voice-target-convert')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-text-answer')), findsOneWidget);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('practice-text-answer')))
          .controller
          ?.text,
      isNotEmpty,
    );
    expect(harness.practice.recordingState, PracticeRecordingState.idle);
    await tester.tap(find.byKey(const Key('practice-return-to-voice')));
    await tester.pumpAndSettle();
    expect(harness.practice.recordingState, PracticeRecordingState.idle);

    final cancelledGesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('practice-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await cancelledGesture.moveBy(const Offset(-90, 0));
    await tester.pump();
    expect(find.text('松开取消'), findsOneWidget);
    await cancelledGesture.up();
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-transcript')), findsNothing);
    expect(harness.practice.recordingState, PracticeRecordingState.idle);

    for (var turn = 1; turn <= 3; turn++) {
      expect(
        find
            .byKey(Key('practice-ai-message-${harness.practice.questionId}'))
            .hitTestable(),
        findsOneWidget,
      );
      await _holdAndReleaseAnswer(tester);
      if (turn < 3) {
        expect(harness.practice.practiceMessages, hasLength(turn * 2 + 1));
        expect(harness.practice.recordingState, PracticeRecordingState.idle);
      }
    }

    expect(find.byKey(const Key('practice-page')), findsOneWidget);
    expect(find.byKey(const Key('practice-completed-actions')), findsOneWidget);
    expect(find.byKey(const Key('review-content')), findsNothing);
    expect(harness.practice.recordingState, PracticeRecordingState.completed);

    Navigator.of(tester.element(find.byKey(const Key('practice-page')))).pop();
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('profile-page')), findsOneWidget);
  });

  testWidgets(
    'global AuthGate shows the real email and logout clears Agent data',
    (tester) async {
      final harness = _agentHarness();
      addTearDown(harness.dispose);
      final preparationController = PreparationController(
        client: _EmptySceneClient(),
      );
      addTearDown(preparationController.dispose);
      final authController = AuthController(
        identityClient: _AuthenticatedIdentityClient(),
        sessionStore: _MemorySessionStore('sess_stored-token'),
        clearPrivateState: () async {
          await Future.wait<void>([
            harness.conversation.clearPrivateState(),
            harness.composer.clearPrivateState(),
            harness.messageAudio.clearPrivateState(),
            harness.practice.clearPrivateState(),
            preparationController.clearPrivateState(),
          ]);
        },
      );

      await tester.pumpWidget(
        SpeakUpApp(
          authController: authController,
          conversationController: harness.conversation,
          composerController: harness.composer,
          messageAudioController: harness.messageAudio,
          practiceController: harness.practice,
          preparationController: preparationController,
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(harness.conversation.threadId, isNotNull);

      await tester.tap(find.byKey(const Key('conversation-menu-button')));
      await tester.pumpAndSettle();
      expect(find.text('已连接当前账号'), findsOneWidget);
      expect(find.textContaining('本地 Fake 预览'), findsNothing);
      Navigator.of(tester.element(find.byType(Drawer))).pop();
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('primary-tab-profile')));
      await tester.pumpAndSettle();
      expect(find.text('learner@example.com'), findsOneWidget);

      await tester.tap(find.byKey(const Key('profile-logout-button')));
      await tester.pumpAndSettle();

      expect(find.text('欢迎回来'), findsOneWidget);
      expect(find.byKey(const Key('primary-navigation')), findsNothing);
      expect(harness.conversation.threadId, isNull);
      expect(harness.conversation.messages, isEmpty);
    },
  );

  testWidgets('logout removes the entire Navigator from a deep private route', (
    tester,
  ) async {
    final harness = _agentHarness();
    addTearDown(harness.dispose);
    final preparationController = PreparationController(
      client: _EmptySceneClient(),
    );
    addTearDown(preparationController.dispose);
    final authController = AuthController(
      identityClient: _AuthenticatedIdentityClient(),
      sessionStore: _MemorySessionStore('sess_stored-token'),
      clearPrivateState: () async {
        await Future.wait<void>([
          harness.conversation.clearPrivateState(),
          harness.composer.clearPrivateState(),
          harness.messageAudio.clearPrivateState(),
          harness.practice.clearPrivateState(),
          preparationController.clearPrivateState(),
        ]);
      },
    );
    await tester.pumpWidget(
      SpeakUpApp(
        authController: authController,
        conversationController: harness.conversation,
        composerController: harness.composer,
        messageAudioController: harness.messageAudio,
        practiceController: harness.practice,
        preparationController: preparationController,
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
    expect(find.text('欢迎回来'), findsOneWidget);
  });

  testWidgets('completed root Practice route keeps its completion state', (
    tester,
  ) async {
    final harness = await _startedHarness();
    addTearDown(harness.dispose);
    for (var turn = 0; turn < 3; turn++) {
      await harness.practice.startRecording();
      await harness.practice.stopRecording();
      await harness.practice.confirmTranscript();
    }
    expect(harness.practice.recordingState, PracticeRecordingState.completed);

    await tester.pumpWidget(
      MaterialApp(home: PracticePage(practiceController: harness.practice)),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byKey(const Key('practice-page')), findsOneWidget);
    expect(find.byKey(const Key('practice-completed-actions')), findsOneWidget);
    expect(find.byKey(const Key('review-content')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('completion does not replace a newer route', (tester) async {
    final harness = await _startedHarness();
    addTearDown(harness.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SpeakUpShell(
          previewMode: true,
          conversationController: harness.conversation,
          composerController: harness.composer,
          practiceController: harness.practice,
        ),
      ),
    );
    await tester.pumpAndSettle();

    final navigator = Navigator.of(tester.element(find.byType(SpeakUpShell)));
    navigator.push(
      MaterialPageRoute<void>(
        builder: (_) => PracticePage(practiceController: harness.practice),
      ),
    );
    await tester.pumpAndSettle();
    navigator.push(
      MaterialPageRoute<void>(
        builder: (_) => const Scaffold(key: Key('temporary-practice-overlay')),
      ),
    );
    await tester.pumpAndSettle();

    for (var turn = 0; turn < 3; turn++) {
      await harness.practice.startRecording();
      await harness.practice.stopRecording();
      await harness.practice.confirmTranscript();
    }
    await tester.pump();

    expect(harness.practice.recordingState, PracticeRecordingState.completed);
    expect(find.byKey(const Key('temporary-practice-overlay')), findsOneWidget);
    expect(
      find.byKey(const Key('practice-page'), skipOffstage: false),
      findsOneWidget,
    );
    for (var frame = 0; frame < 75; frame++) {
      await tester.pump(const Duration(milliseconds: 16));
    }

    navigator.pop();
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('temporary-practice-overlay')), findsNothing);
    expect(find.byKey(const Key('practice-page')), findsOneWidget);
    expect(find.byKey(const Key('practice-completed-actions')), findsOneWidget);
  });
}

Future<_AgentHarness> _startedHarness() async {
  final scene = testScenes.first;
  final harness = _agentHarness(
    practiceClient: FakePracticeClient(
      sceneFamily: scene.family,
      sceneModel: scene.model,
    ),
  );
  await harness.conversation.initialize();
  await activateTestPractice(controller: harness.practice, scene: scene);
  return harness;
}

_AgentHarness _agentHarness({PracticeClient? practiceClient}) {
  final client = FakeAgentClient();
  final voiceClient = FakeAgentVoiceClient();
  final conversation = ConversationController(client: client);
  final messageAudio = AgentMessageAudioController(
    conversationController: conversation,
    client: voiceClient,
    audioPlayer: FakeAgentAudioPlayer(),
  );
  return _AgentHarness(
    conversation: conversation,
    composer: ComposerController(
      conversationController: conversation,
      voiceClient: voiceClient,
      onAssistantCommitted: messageAudio.playCommittedAssistant,
    ),
    messageAudio: messageAudio,
    practice: PracticeController(
      client: practiceClient ?? FakePracticeClient(),
    ),
  );
}

final class _AgentHarness {
  const _AgentHarness({
    required this.conversation,
    required this.composer,
    required this.messageAudio,
    required this.practice,
  });

  final ConversationController conversation;
  final ComposerController composer;
  final AgentMessageAudioController messageAudio;
  final PracticeController practice;

  void dispose() {
    composer.dispose();
    messageAudio.dispose();
    conversation.dispose();
    practice.dispose();
  }
}

Future<void> _holdAndReleaseAnswer(WidgetTester tester) async {
  final holdTarget = find.byKey(const Key('practice-record'));
  final gesture = await tester.startGesture(tester.getCenter(holdTarget));
  await tester.pump(const Duration(milliseconds: 220));
  expect(find.byKey(const Key('practice-stop-recording')), findsOneWidget);
  await gesture.up();
  await tester.pumpAndSettle();
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

final class _EmptySceneClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() async => const <SceneDefinition>[];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }
}
