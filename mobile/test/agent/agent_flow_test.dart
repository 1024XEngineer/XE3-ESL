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
import 'package:speakup/features/coaching/interview/interview_practice.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
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
    final scene = testScene(
      experience: PracticeExperience.ieltsSpeaking,
      category: SceneCategory.ieltsSpeaking,
      practiceOptions: const [
        PracticeOption(
          id: 'option-scene-test-part1',
          sceneId: 'scene-test',
          mode: PracticeMode.part1,
          displayName: 'Part 1',
          suggestedDurationSeconds: 300,
          turnPolicyRef: 'turn-ielts-part1',
          sessionPolicyRef: 'session-ielts-part1',
          evaluationPolicyRef: 'evaluation-ielts-part1',
        ),
      ],
    );
    final harness = _agentHarness(
      practiceClient: FakePracticeClient(
        practiceExperience: scene.experience,
        sceneCategory: scene.category,
        practiceMode: PracticeMode.part1,
        ieltsAssignment: testIeltsAssignment(
          mode: PracticeMode.part1,
          part1QuestionCount: 3,
        ),
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
    for (var turn = 1; turn <= 3; turn++) {
      await harness.practice.startRecording();
      await harness.practice.stopRecording();
      await harness.practice.confirmTranscript();
      if (turn < 3) {
        expect(harness.practice.practiceMessages, hasLength(turn * 2 + 1));
        expect(harness.practice.recordingState, PracticeRecordingState.idle);
      }
    }

    expect(harness.practice.recordingState, PracticeRecordingState.completed);
    expect(harness.conversation.threadId, isNotNull);
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
      final ieltsPreparationController = IeltsPreparationController(
        client: _UnusedIeltsQuestionBankClient(),
      );
      addTearDown(preparationController.dispose);
      addTearDown(ieltsPreparationController.dispose);
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
            ieltsPreparationController.clearPrivateState(),
          ]);
        },
      );

      await tester.pumpWidget(
        SpeakUpApp(
          authController: authController,
          conversationController: harness.conversation,
          composerController: harness.composer,
          messageAudioController: harness.messageAudio,
          messageTranslationClient: null,
          practiceController: harness.practice,
          preparationController: preparationController,
          ieltsPreparationController: ieltsPreparationController,
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(harness.conversation.threadId, isNotNull);

      await tester.tap(find.byKey(const Key('conversation-menu-button')));
      await tester.pumpAndSettle();
      expect(find.text('聊天'), findsOneWidget);
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
    final ieltsPreparationController = IeltsPreparationController(
      client: _UnusedIeltsQuestionBankClient(),
    );
    addTearDown(preparationController.dispose);
    addTearDown(ieltsPreparationController.dispose);
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
          ieltsPreparationController.clearPrivateState(),
        ]);
      },
    );
    await tester.pumpWidget(
      SpeakUpApp(
        authController: authController,
        conversationController: harness.conversation,
        composerController: harness.composer,
        messageAudioController: harness.messageAudio,
        messageTranslationClient: null,
        practiceController: harness.practice,
        preparationController: preparationController,
        ieltsPreparationController: ieltsPreparationController,
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
      MaterialApp(
        home: InterviewPracticePage(practiceController: harness.practice),
      ),
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
        builder: (_) =>
            InterviewPracticePage(practiceController: harness.practice),
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
      practiceExperience: scene.experience,
      sceneCategory: scene.category,
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

final class _UnusedIeltsQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getQuestionBank() {
    throw UnimplementedError();
  }
}
