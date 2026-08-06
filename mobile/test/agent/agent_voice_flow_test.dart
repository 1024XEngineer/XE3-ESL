import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/agent/conversation/conversation.dart';

void main() {
  test(
    'Thread focus stays authoritative when voice cleanup needs a retry',
    () async {
      final player = _FailNextStopAgentAudioPlayer();
      final recorder = _TrackingAgentVoiceRecorder();
      final conversationController = ConversationController(
        client: FakeAgentClient(),
      );
      final composerController = ComposerController(
        conversationController: conversationController,
        voiceClient: FakeAgentVoiceClient(),
        voiceRecorder: recorder,
        draftAudioPlayer: player,
      );
      addTearDown(() {
        composerController.dispose();
        conversationController.dispose();
      });
      await conversationController.initialize();
      await Future<void>.delayed(Duration.zero);
      final oldThreadId = conversationController.threadId;
      final voiceController = composerController.voiceController!;

      player.failNextStop = true;
      expect(await conversationController.createThread(), isTrue);
      await Future<void>.delayed(Duration.zero);

      expect(conversationController.threadId, isNot(oldThreadId));
      expect(voiceController.threadId, conversationController.threadId);
      expect(voiceController.state, AgentVoiceComposerState.failed);
      expect(voiceController.errorMessage, contains('语音播放未能停止'));

      await voiceController.retry();

      expect(voiceController.state, AgentVoiceComposerState.recording);
      expect(recorder.startCalls, 1);
      await voiceController.cancel();
    },
  );

  testWidgets(
    'records, directly sends, plays, and deletes one Agent voice Message',
    (tester) async {
      final client = FakeAgentClient();
      final voiceClient = FakeAgentVoiceClient();
      final assistantSpeech = _FlowAssistantSpeechClient();
      final messagePlayer = FakeAgentAudioPlayer();
      final conversationController = ConversationController(client: client);
      final messageAudioController = AgentMessageAudioController(
        conversationController: conversationController,
        client: voiceClient,
        audioPlayer: messagePlayer,
        assistantSpeechClient: assistantSpeech,
      );
      final composerController = ComposerController(
        conversationController: conversationController,
        voiceClient: voiceClient,
        onAssistantStreamStarted: (transientMessageId) => messageAudioController
            .startLiveAssistantSpeech(transientMessageId: transientMessageId),
        onAssistantStreamDelta: (transientMessageId, delta) =>
            messageAudioController.appendLiveAssistantSpeech(
              transientMessageId: transientMessageId,
              delta: delta,
            ),
        onAssistantStreamCompleted: (transientMessageId, message) =>
            messageAudioController.completeLiveAssistantSpeech(
              transientMessageId: transientMessageId,
              message: message,
            ),
        onAssistantStreamFailed: (transientMessageId) => messageAudioController
            .failLiveAssistantSpeech(transientMessageId: transientMessageId),
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(() {
        composerController.dispose();
        messageAudioController.dispose();
        conversationController.dispose();
      });

      await tester.pumpWidget(
        SpeakUpApp.preview(
          conversationController: conversationController,
          composerController: composerController,
          messageAudioController: messageAudioController,
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
      await tester.pump();
      expect(
        composerController.voiceController?.state,
        AgentVoiceComposerState.recording,
      );
      expect(find.byKey(const Key('agent-composer-surface')), findsOneWidget);
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);
      expect(find.byKey(const Key('agent-voice-stop')), findsOneWidget);

      await tester.tap(find.byKey(const Key('agent-voice-stop')));
      await _pumpVoiceOperation(tester);
      expect(
        composerController.voiceController?.state,
        AgentVoiceComposerState.idle,
      );
      expect(find.byKey(const Key('agent-composer-surface')), findsOneWidget);
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);
      expect(find.text('试听'), findsNothing);
      expect(find.text('重录'), findsNothing);
      expect(find.text('上传并转写'), findsNothing);

      final voiceMessages = conversationController.messages
          .where((message) => message.modality == AgentMessageModality.voice)
          .toList();
      expect(voiceMessages, hasLength(1));
      expect(
        voiceMessages.single.text,
        'I explained the problem, the trade-off, and the result clearly.',
      );
      expect(voiceMessages.single.audio?.isReadable, isTrue);
      expect(
        conversationController.messages.where(
          (message) => message.role == AgentMessageRole.assistant,
        ),
        hasLength(1),
      );
      expect(find.byKey(const Key('agent-voice-composer-panel')), findsNothing);

      final assistant = conversationController.messages.singleWhere(
        (message) => message.role == AgentMessageRole.assistant,
      );
      final assistantTts = find.byKey(
        Key('agent-assistant-tts-${assistant.id}'),
      );
      await tester.pumpAndSettle();
      expect(assistantTts.hitTestable(), findsOneWidget);
      expect(assistantSpeech.texts, <String>[
        'That was clear.',
        'Add one measurable result to make the answer stronger.',
      ]);
      expect(messageAudioController.playingMessageId, assistant.id);
      messagePlayer.complete();
      await tester.pump();
      expect(messageAudioController.playingMessageId, isNull);
      await tester.tap(assistantTts);
      await tester.pump();
      expect(
        find.byKey(Key('agent-assistant-text-${assistant.id}')),
        findsOneWidget,
      );
      expect(messageAudioController.playingMessageId, assistant.id);
      await tester.tap(
        find.byKey(Key('agent-assistant-speed-${assistant.id}')),
      );
      await tester.pump();
      expect(messageAudioController.speechSpeed, 1.25);

      final voiceMessage = voiceMessages.single;
      final voiceBubble = find.byKey(Key('agent-message-${voiceMessage.id}'));
      final voiceDecoration =
          tester.widget<Container>(voiceBubble).decoration! as BoxDecoration;
      expect(voiceDecoration.color, SpeakUpDesign.primaryMuted);
      expect(tester.getSize(voiceBubble).height, lessThan(220));
      expect(
        find.byKey(Key('agent-user-voice-play-${voiceMessage.id}')),
        findsOneWidget,
      );
      expect(
        find.byKey(Key('agent-user-voice-duration-${voiceMessage.id}')),
        findsOneWidget,
      );
      expect(
        find.byKey(Key('agent-user-voice-transcript-${voiceMessage.id}')),
        findsOneWidget,
      );

      await tester.tap(
        find.byKey(Key('agent-user-voice-delete-${voiceMessage.id}')),
      );
      await _pumpVoiceOperation(tester);
      final retained = conversationController.messages.singleWhere(
        (message) => message.id == voiceMessage.id,
      );
      expect(retained.text, voiceMessage.text);
      expect(retained.audio?.status, AgentMessageAudioStatus.deleted);
      expect(
        find.byKey(Key('agent-user-voice-deleted-${voiceMessage.id}')),
        findsOneWidget,
      );
      await composerController.voiceController?.cancel();
      await tester.pump();
    },
  );

  testWidgets('upward swipe cancels home recording', (tester) async {
    final client = FakeAgentClient();
    final voiceClient = FakeAgentVoiceClient();
    final conversationController = ConversationController(client: client);
    final composerController = ComposerController(
      conversationController: conversationController,
      voiceClient: voiceClient,
      clientIdFactory: _sequentialIdFactory(),
    );
    addTearDown(() {
      composerController.dispose();
      conversationController.dispose();
    });
    await tester.pumpWidget(
      SpeakUpApp.preview(
        conversationController: conversationController,
        composerController: composerController,
      ),
    );
    await tester.pumpAndSettle();

    final target = find.byKey(const Key('agent-mic-placeholder'));
    await tester.tap(target);
    await tester.pump();
    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.recording,
    );

    final gesture = await tester.startGesture(tester.getCenter(target));
    await gesture.moveBy(const Offset(0, -90));
    await tester.pump();
    expect(find.text('松开取消'), findsOneWidget);
    await gesture.up();
    await _pumpVoiceOperation(tester);

    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.idle,
    );
    expect(find.byKey(const Key('agent-composer-field')), findsNothing);
  });

  testWidgets('recording elapsed time updates inside the original composer', (
    tester,
  ) async {
    var now = DateTime.utc(2026, 7, 27, 9);
    final voiceController = AgentVoiceController(
      client: FakeAgentVoiceClient(),
      recorder: FakeAgentVoiceRecorder(),
      audioPlayer: FakeAgentAudioPlayer(),
      onMessagesCommitted: (_) {},
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
    final client = FakeAgentClient();
    final voiceClient = FakeAgentVoiceClient();
    final conversationController = ConversationController(client: client);
    final composerController = ComposerController(
      conversationController: conversationController,
      voiceClient: voiceClient,
      clientIdFactory: _sequentialIdFactory(),
    );
    addTearDown(() {
      composerController.dispose();
      conversationController.dispose();
    });
    await tester.pumpWidget(
      SpeakUpApp.preview(
        conversationController: conversationController,
        composerController: composerController,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await tester.pump();
    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.recording,
    );

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('scenes-page')), findsOneWidget);
    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.idle,
    );
    await tester.pump(const Duration(seconds: 60));
    expect(
      conversationController.messages.where(
        (message) => message.modality == AgentMessageModality.voice,
      ),
      isEmpty,
    );
  });

  test(
    'leaving Agent while playback is stopping fences microphone start',
    () async {
      final player = _GatedAgentAudioPlayer();
      final recorder = _TrackingAgentVoiceRecorder();
      final client = FakeAgentClient();
      final voiceClient = FakeAgentVoiceClient();
      final conversationController = ConversationController(client: client);
      final composerController = ComposerController(
        conversationController: conversationController,
        voiceClient: voiceClient,
        voiceRecorder: recorder,
        draftAudioPlayer: player,
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(() {
        composerController.dispose();
        conversationController.dispose();
      });
      await conversationController.initialize();

      final stop = player.blockNextStop();
      final start = composerController.startAgentVoiceRecording();
      await stop.entered.future;

      final leave = composerController.prepareToLeave();
      stop.release.complete();

      await start;
      expect(await leave, isTrue);
      expect(recorder.startCalls, 0);
      expect(
        composerController.voiceController?.state,
        AgentVoiceComposerState.idle,
      );
    },
  );

  testWidgets('composer drafts cannot cross the Thread boundary', (
    tester,
  ) async {
    final voiceController = AgentVoiceController(
      client: FakeAgentVoiceClient(),
      recorder: FakeAgentVoiceRecorder(),
      audioPlayer: FakeAgentAudioPlayer(),
      onMessagesCommitted: (_) {},
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
    await voiceController.cancel();
    await tester.pump();

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
      final client = FakeAgentClient();
      final voiceClient = FakeAgentVoiceClient();
      final conversationController = ConversationController(client: client);
      final composerController = ComposerController(
        conversationController: conversationController,
        voiceClient: voiceClient,
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(() {
        composerController.dispose();
        conversationController.dispose();
      });
      await conversationController.initialize();
      final previousThreadId = conversationController.threadId;
      await conversationController.clearFocusedThread();
      expect(conversationController.threadId, isNull);

      await tester.pumpWidget(
        SpeakUpApp.preview(
          conversationController: conversationController,
          composerController: composerController,
        ),
      );
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

      expect(conversationController.threadId, isNotNull);
      expect(conversationController.threadId, isNot(previousThreadId));
      expect(
        composerController.voiceController?.state,
        AgentVoiceComposerState.recording,
      );
      await composerController.voiceController?.cancel();
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

      final client = FakeAgentClient();
      final voiceClient = FakeAgentVoiceClient();
      final conversationController = ConversationController(client: client);
      final composerController = ComposerController(
        conversationController: conversationController,
        voiceClient: voiceClient,
        clientIdFactory: _sequentialIdFactory(),
      );
      addTearDown(() {
        composerController.dispose();
        conversationController.dispose();
      });
      await tester.pumpWidget(
        SpeakUpApp.preview(
          conversationController: conversationController,
          composerController: composerController,
        ),
      );
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

      expect(find.text('点击发送 · 上滑取消'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await composerController.voiceController?.cancel();
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

final class _GatedAgentAudioPlayer implements AgentAudioPlayer {
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
  Stream<Duration> get onPosition => const Stream<Duration>.empty();

  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> playFile(String path, {required double speed}) async {}

  @override
  Future<void> playWav(Uint8List bytes, {required double speed}) async {}

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

final class _FailNextStopAgentAudioPlayer implements AgentAudioPlayer {
  bool failNextStop = false;

  @override
  Stream<Duration> get onPosition => const Stream<Duration>.empty();

  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> playFile(String path, {required double speed}) async {}

  @override
  Future<void> playWav(Uint8List bytes, {required double speed}) async {}

  @override
  Future<void> stop() async {
    if (failNextStop) {
      failNextStop = false;
      throw const AgentAudioPlaybackException();
    }
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

final class _FlowAssistantSpeechClient implements AgentAssistantSpeechClient {
  final List<String> texts = <String>[];

  @override
  Stream<AgentAssistantSpeechAudioSegment> streamAssistantSpeech({
    required String threadId,
    required Stream<AgentAssistantSpeechTextSegment> segments,
  }) async* {
    await for (final segment in segments) {
      texts.add(segment.text);
      yield AgentAssistantSpeechAudioSegment(
        sequence: segment.sequence,
        chunkIndex: 1,
        audio: Uint8List.fromList(<int>[1, 2, 3, 4]),
      );
    }
  }
}

Future<void> _pumpVoiceOperation(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

String Function(String scope) _sequentialIdFactory() {
  var sequence = 0;
  return (scope) => '${scope}_${++sequence}'.replaceAll('-', '_');
}
