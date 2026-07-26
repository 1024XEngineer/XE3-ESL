import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_controller.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/agent/agent_voice_recording.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/conversation/conversation.dart';

void main() {
  testWidgets(
    'records, confirms, plays, and deletes one ordinary Agent voice Message',
    (tester) async {
      final controller = AgentController(
        client: FakeAgentClient(),
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(controller.dispose);

      await tester.pumpWidget(SpeakUpApp.preview(agentController: controller));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
      await tester.pump();
      expect(
        controller.voiceController?.state,
        AgentVoiceComposerState.recording,
      );
      expect(find.byKey(const Key('agent-composer-surface')), findsOneWidget);
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);
      expect(find.byKey(const Key('agent-voice-stop')), findsOneWidget);

      await tester.tap(find.byKey(const Key('agent-voice-stop')));
      await _pumpVoiceOperation(tester);
      expect(
        controller.voiceController?.state,
        AgentVoiceComposerState.awaitingConfirmation,
      );
      expect(find.byKey(const Key('agent-composer-surface')), findsOneWidget);
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);
      expect(find.text('试听'), findsNothing);
      expect(find.text('重录'), findsNothing);
      expect(find.text('上传并转写'), findsNothing);
      final transcriptField = find.byKey(const Key('agent-composer-field'));
      expect(transcriptField, findsOneWidget);
      await tester.enterText(
        transcriptField,
        'I kept the migration safe with staged checks.',
      );
      await tester.pump();
      expect(
        controller.voiceController?.editedTranscript,
        'I kept the migration safe with staged checks.',
      );
      expect(controller.voiceController?.canConfirm, isTrue);

      await tester.tap(find.byKey(const Key('agent-voice-confirm')));
      await _pumpVoiceOperation(tester);

      final voiceMessages = controller.messages
          .where((message) => message.modality == AgentMessageModality.voice)
          .toList();
      expect(voiceMessages, hasLength(1));
      expect(
        voiceMessages.single.text,
        'I kept the migration safe with staged checks.',
      );
      expect(voiceMessages.single.audio?.isReadable, isTrue);
      expect(
        controller.messages.where(
          (message) => message.role == AgentMessageRole.assistant,
        ),
        hasLength(1),
      );
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);

      final assistant = controller.messages.singleWhere(
        (message) => message.role == AgentMessageRole.assistant,
      );
      final assistantTts = find.byKey(
        Key('agent-assistant-tts-${assistant.id}'),
      );
      expect(assistantTts.hitTestable(), findsOneWidget);
      await tester.tap(assistantTts);
      await tester.pump();
      expect(
        find.byKey(Key('agent-assistant-text-${assistant.id}')),
        findsOneWidget,
      );
      expect(controller.voiceController?.playingMessageId, assistant.id);
      await tester.tap(
        find.byKey(Key('agent-assistant-speed-${assistant.id}')),
      );
      await tester.pump();
      expect(controller.voiceController?.speechSpeed, 1.25);

      final voiceMessage = voiceMessages.single;
      final voiceBubble = find.byKey(Key('agent-message-${voiceMessage.id}'));
      final voiceDecoration =
          tester.widget<Container>(voiceBubble).decoration! as BoxDecoration;
      expect(voiceDecoration.color, const Color(0xFFE7E7E3));
      expect(tester.getSize(voiceBubble).height, lessThan(140));
      expect(
        find.byKey(Key('agent-user-voice-play-${voiceMessage.id}')),
        findsOneWidget,
      );
      expect(
        find.byKey(Key('agent-user-voice-duration-${voiceMessage.id}')),
        findsOneWidget,
      );
      expect(
        find.byKey(Key('agent-user-voice-progress-${voiceMessage.id}')),
        findsOneWidget,
      );
      expect(
        find.byKey(Key('agent-user-voice-transcript-${voiceMessage.id}')),
        findsNothing,
      );
      final transcriptToggle = find.byKey(
        Key('agent-user-voice-transcript-toggle-${voiceMessage.id}'),
      );
      await tester.ensureVisible(transcriptToggle);
      await tester.pump();
      await tester.tap(transcriptToggle);
      await tester.pump();
      expect(
        find.byKey(Key('agent-user-voice-transcript-${voiceMessage.id}')),
        findsOneWidget,
      );

      await tester.tap(
        find.byKey(Key('agent-user-voice-delete-${voiceMessage.id}')),
      );
      await _pumpVoiceOperation(tester);
      final retained = controller.messages.singleWhere(
        (message) => message.id == voiceMessage.id,
      );
      expect(retained.text, voiceMessage.text);
      expect(retained.audio?.status, AgentMessageAudioStatus.deleted);
      expect(
        find.byKey(Key('agent-user-voice-deleted-${voiceMessage.id}')),
        findsOneWidget,
      );
      await controller.voiceController?.cancel();
      await tester.pump();
    },
  );

  testWidgets('recording elapsed time updates inside the original composer', (
    tester,
  ) async {
    var now = DateTime.utc(2026, 7, 27, 9);
    final voiceController = AgentVoiceController(
      client: FakeAgentClient(),
      recorder: FakeAgentVoiceRecorder(),
      audioPlayer: FakeAgentVoiceAudioPlayer(),
      onMessagesCommitted: (_) {},
      onMessageAudioDeleted: (_, _) {},
      idFactory: (scope) => '${scope}_1',
      clock: () => now,
    );
    addTearDown(voiceController.dispose);
    await voiceController.bindThread('thread-a');

    await tester.pumpWidget(
      MaterialApp(
        home: ConversationPage(
          onStartVoice: voiceController.startRecording,
          voiceController: voiceController,
          onSubmitText: (_) async => true,
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await tester.pump();

    expect(voiceController.state, AgentVoiceComposerState.recording);
    expect(find.byKey(const Key('agent-composer-surface')), findsOneWidget);
    expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);
    expect(
      tester
          .widget<Text>(find.byKey(const Key('agent-voice-recording-duration')))
          .data,
      '0:00',
    );

    now = now.add(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 1));

    expect(
      tester
          .widget<Text>(find.byKey(const Key('agent-voice-recording-duration')))
          .data,
      '0:01',
    );
    await voiceController.cancel();
    await tester.pump();
  });

  testWidgets('composer drafts cannot cross the Thread boundary', (
    tester,
  ) async {
    final voiceController = AgentVoiceController(
      client: FakeAgentClient(),
      recorder: FakeAgentVoiceRecorder(),
      audioPlayer: FakeAgentVoiceAudioPlayer(),
      onMessagesCommitted: (_) {},
      onMessageAudioDeleted: (_, _) {},
      idFactory: (scope) => '${scope}_1',
    );
    addTearDown(voiceController.dispose);
    var threadId = 'thread-a';
    late StateSetter update;
    await voiceController.bindThread(threadId);

    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            update = setState;
            return ConversationPage(
              threadId: threadId,
              onStartVoice: voiceController.startRecording,
              voiceController: voiceController,
              onSubmitText: (_) async => true,
            );
          },
        ),
      ),
    );
    await tester.enterText(
      find.byKey(const Key('agent-composer-field')),
      'Thread A private draft',
    );
    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await tester.pump();
    expect(voiceController.state, AgentVoiceComposerState.recording);

    await voiceController.bindThread('thread-b');
    await voiceController.startRecording();
    update(() => threadId = 'thread-b');
    await tester.pump();

    expect(voiceController.state, AgentVoiceComposerState.recording);
    await tester.tap(find.byKey(const Key('agent-voice-cancel')));
    await _pumpVoiceOperation(tester);

    final field = find.byKey(const Key('agent-composer-field'));
    expect(field, findsOneWidget);
    expect(tester.widget<TextField>(field).controller?.text, isEmpty);
    expect(find.text('Thread A private draft'), findsNothing);
  });

  testWidgets(
    'microphone safely creates and focuses a Thread when none is focused',
    (tester) async {
      final controller = AgentController(
        client: FakeAgentClient(),
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      final previousThreadId = controller.threadId;
      await controller.clearFocusedThread();
      expect(controller.threadId, isNull);

      await tester.pumpWidget(SpeakUpApp.preview(agentController: controller));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('no-focused-conversation-home')),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
      await tester.pump();

      expect(controller.threadId, isNotNull);
      expect(controller.threadId, isNot(previousThreadId));
      expect(
        controller.voiceController?.state,
        AgentVoiceComposerState.recording,
      );
      await controller.voiceController?.cancel();
      await tester.pump();
    },
  );

  testWidgets(
    'voice transcript stays in the original composer on narrow layouts',
    (tester) async {
      tester.view.physicalSize = const Size(320, 640);
      tester.view.devicePixelRatio = 1;
      tester.view.viewInsets = const FakeViewPadding(bottom: 240);
      tester.platformDispatcher.textScaleFactorTestValue = 2;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetViewInsets);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

      final controller = AgentController(
        client: FakeAgentClient(),
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(controller.dispose);
      await tester.pumpWidget(SpeakUpApp.preview(agentController: controller));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
      await tester.pump();

      final surface = find.byKey(const Key('agent-composer-surface'));
      final stateLabel = find.byKey(const Key('agent-voice-state-label'));
      final duration = find.byKey(const Key('agent-voice-recording-duration'));
      final stop = find.byKey(const Key('agent-voice-stop'));
      expect(stateLabel, findsOneWidget);
      expect(duration, findsOneWidget);
      expect(stop.hitTestable(), findsOneWidget);
      expect(tester.getRect(stateLabel).left, greaterThanOrEqualTo(0));
      expect(
        tester.getRect(duration).right,
        lessThanOrEqualTo(tester.getRect(surface).right),
      );
      expect(
        tester.getRect(stop).right,
        lessThanOrEqualTo(tester.getRect(surface).right),
      );
      expect(tester.takeException(), isNull);

      await tester.tap(find.byKey(const Key('agent-voice-stop')));
      await _pumpVoiceOperation(tester);

      expect(find.byKey(const Key('agent-composer-field')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await controller.voiceController?.cancel();
      await tester.pump();
    },
  );
}

Future<void> _pumpVoiceOperation(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

AgentClientIdFactory _sequentialIdFactory() {
  var sequence = 0;
  return (scope) => '${scope}_${++sequence}'.replaceAll('-', '_');
}
