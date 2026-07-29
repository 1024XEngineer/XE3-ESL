import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_controller.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/agent/agent_voice_recording.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_media.dart';

void main() {
  testWidgets(
    'records, directly sends, plays, and deletes one Agent voice Message',
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
      expect(controller.voiceController?.state, AgentVoiceComposerState.idle);
      expect(find.byKey(const Key('agent-composer-surface')), findsOneWidget);
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);
      expect(find.text('试听'), findsNothing);
      expect(find.text('重录'), findsNothing);
      expect(find.text('上传并转写'), findsNothing);

      final voiceMessages = controller.messages
          .where((message) => message.modality == AgentMessageModality.voice)
          .toList();
      expect(voiceMessages, hasLength(1));
      expect(
        voiceMessages.single.text,
        'I explained the problem, the trade-off, and the result clearly.',
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
      expect(voiceDecoration.color, SpeakUpDesign.primaryMuted);
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

  testWidgets('right swipe converts home recording into editable text', (
    tester,
  ) async {
    final controller = AgentController(
      client: FakeAgentClient(),
      clientIdFactory: _sequentialIdFactory(),
    );
    addTearDown(controller.dispose);
    await tester.pumpWidget(SpeakUpApp.preview(agentController: controller));
    await tester.pumpAndSettle();

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('agent-mic-placeholder'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await gesture.moveBy(const Offset(90, 0));
    await tester.pump();
    expect(find.text('松开转成文字'), findsOneWidget);
    await gesture.up();
    await _pumpVoiceOperation(tester);

    final field = find.byKey(const Key('agent-composer-field'));
    expect(field, findsOneWidget);
    expect(
      tester.widget<TextField>(field).controller?.text,
      'I explained the problem, the trade-off, and the result clearly.',
    );
    expect(
      controller.voiceController?.state,
      AgentVoiceComposerState.awaitingConfirmation,
    );

    await tester.enterText(field, 'Edited transcript sent as text.');
    await tester.tap(find.byKey(const Key('agent-voice-confirm')));
    await _pumpVoiceOperation(tester);

    final userMessage = controller.messages.lastWhere(
      (message) => message.role == AgentMessageRole.user,
    );
    expect(userMessage.text, 'Edited transcript sent as text.');
    expect(userMessage.modality, AgentMessageModality.text);
  });

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

  testWidgets('leaving the Agent tab cancels hands-free recording', (
    tester,
  ) async {
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

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('scenes-page')), findsOneWidget);
    expect(controller.voiceController?.state, AgentVoiceComposerState.idle);
    await tester.pump(const Duration(seconds: 60));
    expect(
      controller.messages.where(
        (message) => message.modality == AgentMessageModality.voice,
      ),
      isEmpty,
    );
  });

  test(
    'leaving Agent while playback is stopping fences microphone start',
    () async {
      final player = _GatedPracticeAudioPlayer();
      final recorder = _TrackingAgentVoiceRecorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        mediaClient: _NoopPracticeMediaClient(),
        audioPlayer: player,
        voiceRecorder: recorder,
        voiceAudioPlayer: FakeAgentVoiceAudioPlayer(),
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      final stop = player.blockNextStop();
      final start = controller.startAgentVoiceRecording();
      await stop.entered.future;

      final leave = controller.prepareToLeaveAgent();
      stop.release.complete();

      await start;
      expect(await leave, isTrue);
      expect(recorder.startCalls, 0);
      expect(controller.voiceController?.state, AgentVoiceComposerState.idle);
    },
  );

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
    await tester.tap(find.byKey(const Key('agent-show-text-composer')));
    await tester.pump();
    await tester.enterText(
      find.byKey(const Key('agent-composer-field')),
      'Thread A private draft',
    );
    await tester.tap(find.byKey(const Key('agent-show-voice-composer')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await tester.pump();
    expect(voiceController.state, AgentVoiceComposerState.recording);

    await voiceController.bindThread('thread-b');
    await voiceController.startRecording();
    update(() => threadId = 'thread-b');
    await tester.pump();

    expect(voiceController.state, AgentVoiceComposerState.recording);
    await tester.tap(find.byKey(const Key('agent-voice-target-cancel')));
    await _pumpVoiceOperation(tester);

    await tester.tap(find.byKey(const Key('agent-show-text-composer')));
    await tester.pump();
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
        findsNothing,
      );
      expect(find.byKey(const Key('agent-show-text-composer')), findsOneWidget);

      await tester.tap(find.byKey(const Key('agent-show-text-composer')));
      await tester.pump();
      await tester.enterText(
        find.byKey(const Key('agent-composer-field')),
        'Keep this typed draft',
      );
      await tester.pump();
      await tester.tap(find.byKey(const Key('agent-show-voice-composer')));
      await tester.pump();
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
      await tester.tap(find.byKey(const Key('agent-show-text-composer')));
      await tester.pump();
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('agent-composer-field')))
            .controller
            ?.text,
        'Keep this typed draft',
      );
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

      await tester.tap(find.byKey(const Key('agent-voice-target-convert')));
      await _pumpVoiceOperation(tester);

      expect(find.byKey(const Key('agent-composer-field')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await controller.voiceController?.cancel();
      await tester.pump();
    },
  );
}

final class _TrackingAgentVoiceRecorder implements AgentVoiceRecorder {
  int startCalls = 0;

  @override
  Future<void> start() async {
    startCalls++;
  }

  @override
  Future<AgentVoiceLocalRecording> stop() {
    throw UnimplementedError();
  }

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) async {}

  @override
  Future<void> clearAccountState() async {}
}

final class _NoopPracticeMediaClient implements PracticeMediaClient {
  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async => Uint8List(0);

  @override
  Future<Uint8List> loadRecording(String audioAssetId) async => Uint8List(0);

  @override
  Future<void> deleteRecording(String audioAssetId) async {}

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

final class _GatedPracticeAudioPlayer implements PracticeAudioPlayer {
  Completer<void>? _nextStopEntered;
  Completer<void>? _nextStopGate;

  ({Completer<void> entered, Completer<void> release}) blockNextStop() {
    final entered = Completer<void>();
    final release = Completer<void>();
    _nextStopEntered = entered;
    _nextStopGate = release;
    return (entered: entered, release: release);
  }

  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> playWav(Uint8List bytes) async {}

  @override
  Future<void> stop() async {
    final entered = _nextStopEntered;
    final gate = _nextStopGate;
    _nextStopEntered = null;
    _nextStopGate = null;
    entered?.complete();
    await gate?.future;
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

Future<void> _pumpVoiceOperation(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

AgentClientIdFactory _sequentialIdFactory() {
  var sequence = 0;
  return (scope) => '${scope}_${++sequence}'.replaceAll('-', '_');
}
