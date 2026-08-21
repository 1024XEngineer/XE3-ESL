import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_composer.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';

void main() {
  testWidgets('home microphone commits a playable voice Message', (
    tester,
  ) async {
    final recorder = _TrackingStreamingRecorder();
    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
      voiceClient: FakeAgentVoiceClient(),
      voiceRecorder: recorder,
      draftAudioPlayer: FakeAgentAudioPlayer(),
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
    await tester.pump();

    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.recording,
    );
    expect(conversationController.messages, isEmpty);

    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await _pumpUntil(
      tester,
      () => conversationController.messages.any(
        (message) => message.role == AgentMessageRole.user,
      ),
    );

    final userMessages = conversationController.messages
        .where((message) => message.role == AgentMessageRole.user)
        .toList();
    expect(userMessages, hasLength(1));
    expect(
      userMessages.single.text,
      'I explained the problem, the trade-off, and the result clearly.',
    );
    expect(userMessages.single.modality, AgentMessageModality.voice);
    expect(userMessages.single.audio?.isReadable, isTrue);
    expect(recorder.discardedRecordings, 1);
    expect(
      find.byKey(Key('agent-user-voice-play-${userMessages.single.id}')),
      findsOneWidget,
    );
    expect(
      conversationController.messages.where(
        (message) => message.role == AgentMessageRole.assistant,
      ),
      hasLength(1),
    );
    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.idle,
    );
  });

  testWidgets('staged images disable voice capture and remain staged', (
    tester,
  ) async {
    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
      imageClient: FakeAgentImageClient(),
      imagePicker: _SingleImagePicker(),
      voiceClient: FakeAgentVoiceClient(),
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
    await composerController.pickAgentImages();
    await tester.pump();

    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await tester.pump();

    expect(composerController.pendingImages, hasLength(1));
    expect(
      composerController.voiceController?.state,
      AgentVoiceComposerState.idle,
    );
    expect(conversationController.messages, isEmpty);
  });

  test(
    'completed transcription discards PCM without creating a local WAV',
    () async {
      final recorder = _TrackingStreamingRecorder();
      final submitted = <String>[];
      final controller = AgentVoiceInputController(
        client: FakeAgentVoiceInputClient(),
        recorder: recorder,
        idFactory: _sequentialIdFactory(),
        submitTranscript: (transcript) async {
          submitted.add(transcript);
          return true;
        },
      );
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');

      await controller.startRecording();
      await Future<void>.delayed(Duration.zero);
      expect(controller.liveTranscript, isNotEmpty);

      await controller.stopRecording();

      expect(controller.state, AgentVoiceInputState.idle);
      expect(submitted, <String>[
        'I explained the problem, the trade-off, and the result clearly.',
      ]);
      expect(recorder.stopAudioStreamAndDiscardCalls, 1);
      expect(recorder.stopAudioStreamCalls, 0);
      expect(recorder.discardedRecordings, 0);
      expect(recorder.hasCurrentRecording, isFalse);
    },
  );

  test('failed automatic send retains transcript and retries it', () async {
    final recorder = _TrackingStreamingRecorder();
    final submitted = <String>[];
    var shouldSucceed = false;
    final controller = AgentVoiceInputController(
      client: FakeAgentVoiceInputClient(),
      recorder: recorder,
      idFactory: _sequentialIdFactory(),
      submitTranscript: (transcript) async {
        submitted.add(transcript);
        return shouldSucceed;
      },
    );
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');

    await controller.startRecording();
    await Future<void>.delayed(Duration.zero);
    await controller.stopRecording();

    expect(controller.state, AgentVoiceInputState.failed);
    expect(controller.canRetry, isTrue);
    expect(
      controller.liveTranscript,
      'I explained the problem, the trade-off, and the result clearly.',
    );

    shouldSucceed = true;
    await controller.retry();

    expect(controller.state, AgentVoiceInputState.idle);
    expect(submitted, hasLength(2));
    expect(submitted.toSet(), hasLength(1));
  });

  test('automatic send cannot be cancelled after submission starts', () async {
    final recorder = _TrackingStreamingRecorder();
    final submission = Completer<bool>();
    final controller = AgentVoiceInputController(
      client: FakeAgentVoiceInputClient(),
      recorder: recorder,
      idFactory: _sequentialIdFactory(),
      submitTranscript: (_) => submission.future,
    );
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');
    await controller.startRecording();
    await Future<void>.delayed(Duration.zero);

    final stopping = controller.stopRecording();
    while (controller.state != AgentVoiceInputState.submitting) {
      await Future<void>.delayed(Duration.zero);
    }

    expect(controller.liveTranscript, isNotEmpty);
    submission.complete(true);
    await stopping;
    expect(controller.state, AgentVoiceInputState.idle);
  });

  testWidgets('confirming status does not expose a cancel action', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposerVoiceStatusDock(
            state: AgentVoiceComposerState.confirming,
            message: '正在发送…',
            canCancel: false,
            canRetry: false,
            onCancel: () {},
            onRetry: null,
          ),
        ),
      ),
    );

    expect(find.byKey(const Key('agent-voice-cancel')), findsNothing);
    expect(find.text('正在发送…'), findsOneWidget);
  });

  test(
    'local cleanup failure stays failed and retries retained cleanup',
    () async {
      final recorder = _TrackingStreamingRecorder()
        ..failEphemeralCleanup = true;
      final controller = AgentVoiceInputController(
        client: FakeAgentVoiceInputClient(),
        recorder: recorder,
        idFactory: _sequentialIdFactory(),
        submitTranscript: (_) async => true,
      );
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();
      await Future<void>.delayed(Duration.zero);

      await controller.stopRecording();

      expect(controller.state, AgentVoiceInputState.failed);
      expect(controller.canRetry, isTrue);
      expect(recorder.hasPendingCleanup, isTrue);

      recorder
        ..failEphemeralCleanup = false
        ..allowPendingCleanup = true;
      await controller.retry();

      expect(recorder.hasPendingCleanup, isFalse);
      expect(controller.state, AgentVoiceInputState.recording);
    },
  );

  test(
    'Thread switch cleans capture and fences late transcription events',
    () async {
      final client = _ControlledVoiceInputClient();
      final recorder = _TrackingStreamingRecorder();
      final controller = AgentVoiceInputController(
        client: client,
        recorder: recorder,
        idFactory: _sequentialIdFactory(),
        submitTranscript: (_) async => true,
      );
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();
      client.events.add(const AgentVoiceInputStarted());
      client.events.add(const AgentVoiceInputUpdated('Thread A partial'));
      await Future<void>.delayed(Duration.zero);
      expect(controller.liveTranscript, 'Thread A partial');

      await controller.bindThread('thread-b');
      client.events.add(const AgentVoiceInputCompleted('Late Thread A text'));
      await Future<void>.delayed(Duration.zero);

      expect(controller.threadId, 'thread-b');
      expect(controller.state, AgentVoiceInputState.idle);
      expect(controller.liveTranscript, isEmpty);
      expect(recorder.discardCurrentCalls, greaterThan(0));
      expect(recorder.hasCurrentRecording, isFalse);
    },
  );

  test(
    'failed realtime transcription cleans audio without creating a draft',
    () async {
      final client = _ControlledVoiceInputClient();
      final recorder = _TrackingStreamingRecorder();
      final controller = AgentVoiceInputController(
        client: client,
        recorder: recorder,
        idFactory: _sequentialIdFactory(),
        submitTranscript: (_) async => true,
      );
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();

      client.events.add(const AgentVoiceInputStarted());
      client.events.add(
        const AgentVoiceInputFailed(
          kind: 'provider_unavailable',
          retryable: true,
        ),
      );
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);

      expect(controller.state, AgentVoiceInputState.failed);
      expect(controller.canRetry, isTrue);
      expect(recorder.hasCurrentRecording, isFalse);
      expect(recorder.discardCurrentCalls, greaterThan(0));
    },
  );

  test('account cleanup removes capture and fences late events', () async {
    final client = _ControlledVoiceInputClient();
    final recorder = _TrackingStreamingRecorder();
    final controller = AgentVoiceInputController(
      client: client,
      recorder: recorder,
      idFactory: _sequentialIdFactory(),
      submitTranscript: (_) async => true,
    );
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');
    await controller.startRecording();
    client.events.add(const AgentVoiceInputUpdated('Private partial'));
    await Future<void>.delayed(Duration.zero);

    await controller.clearPrivateState();
    client.events.add(const AgentVoiceInputCompleted('Late private text'));
    await Future<void>.delayed(Duration.zero);

    expect(controller.threadId, isNull);
    expect(controller.state, AgentVoiceInputState.idle);
    expect(controller.liveTranscript, isEmpty);
    expect(recorder.clearAccountStateCalls, greaterThan(0));
    expect(recorder.hasCurrentRecording, isFalse);
  });

  test('same-thread restart waits for cancellation cleanup', () async {
    final gate = Completer<void>();
    final recorder = _TrackingStreamingRecorder(discardGate: gate);
    final controller = AgentVoiceInputController(
      client: _ControlledVoiceInputClient(),
      recorder: recorder,
      idFactory: _sequentialIdFactory(),
      submitTranscript: (_) async => true,
    );
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');
    await controller.startRecording();

    final cleanup = controller.cancel();
    await Future<void>.delayed(Duration.zero);
    expect(controller.state, AgentVoiceInputState.idle);
    expect(controller.canStartRecording, isFalse);

    var rebound = false;
    final rebind = controller.bindThread('thread-a').then((_) {
      rebound = true;
    });
    await Future<void>.delayed(Duration.zero);
    expect(rebound, isFalse);

    gate.complete();
    await cleanup;
    await rebind;
    expect(controller.canStartRecording, isTrue);
    await controller.startRecording();
    expect(controller.state, AgentVoiceInputState.recording);
  });

  test('dispose releases an active ephemeral capture', () async {
    final client = _ControlledVoiceInputClient();
    final recorder = _TrackingStreamingRecorder();
    final controller = AgentVoiceInputController(
      client: client,
      recorder: recorder,
      idFactory: _sequentialIdFactory(),
      submitTranscript: (_) async => true,
    );
    await controller.bindThread('thread-a');
    await controller.startRecording();

    controller.dispose();
    for (var attempt = 0; attempt < 20 && client.disposeCalls == 0; attempt++) {
      await Future<void>.delayed(Duration.zero);
    }

    expect(client.disposeCalls, 1);
    expect(recorder.discardCurrentCalls, greaterThan(0));
    expect(recorder.hasCurrentRecording, isFalse);
  });

  testWidgets('upward swipe cancels capture and restores the typed draft', (
    tester,
  ) async {
    final recorder = _TrackingStreamingRecorder();
    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
      voiceClient: FakeAgentVoiceClient(),
      voiceRecorder: recorder,
      draftAudioPlayer: FakeAgentAudioPlayer(),
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

    await tester.tap(find.byKey(const Key('agent-show-text-composer')));
    await tester.pump();
    await tester.enterText(
      find.byKey(const Key('agent-composer-field')),
      'Keep this draft',
    );
    await tester.tap(find.byKey(const Key('agent-show-voice-composer')));
    await tester.pump();
    final target = find.byKey(const Key('agent-mic-placeholder'));
    await tester.tap(target);
    await tester.pump();

    final gesture = await tester.startGesture(tester.getCenter(target));
    await gesture.moveBy(const Offset(0, -90));
    await tester.pump();
    expect(find.text('松开取消'), findsOneWidget);
    await gesture.up();
    await _pumpVoiceOperation(tester);
    await composerController.voiceController!.cancel();

    expect(
      tester
          .widget<TextField>(find.byKey(const Key('agent-composer-field')))
          .controller
          ?.text,
      'Keep this draft',
    );
    expect(conversationController.messages, isEmpty);
    expect(recorder.discardCurrentCalls, greaterThan(0));
    expect(recorder.hasCurrentRecording, isFalse);
  });
}

final class _TrackingStreamingRecorder
    implements
        AgentVoiceRecorder,
        AgentVoiceStreamingRecorder,
        AgentVoiceEphemeralStreamingRecorder {
  _TrackingStreamingRecorder({this.discardGate});

  final Completer<void>? discardGate;
  StreamController<Uint8List>? _chunks;
  bool _recording = false;
  int stopAudioStreamCalls = 0;
  int stopAudioStreamAndDiscardCalls = 0;
  int discardCurrentCalls = 0;
  int discardedRecordings = 0;
  int clearAccountStateCalls = 0;
  bool failEphemeralCleanup = false;
  bool allowPendingCleanup = false;
  bool _pendingCleanup = false;

  bool get hasCurrentRecording => _recording;
  bool get hasPendingCleanup => _pendingCleanup;

  @override
  Future<void> start() async {
    if (_pendingCleanup) {
      if (!allowPendingCleanup) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.unavailable,
        );
      }
      _pendingCleanup = false;
    }
    _recording = true;
  }

  @override
  Future<Stream<Uint8List>> startAudioStream() async {
    await start();
    final chunks = StreamController<Uint8List>();
    _chunks = chunks;
    scheduleMicrotask(() {
      if (identical(_chunks, chunks) && !chunks.isClosed) {
        chunks.add(Uint8List.fromList(const <int>[1, 0, 2, 0]));
      }
    });
    return chunks.stream;
  }

  @override
  Future<AgentVoiceLocalRecording> stop() async {
    _recording = false;
    return const AgentVoiceLocalRecording(
      path: 'temporary-agent-input.wav',
      contentType: 'audio/wav',
      sizeBytes: 48,
      duration: Duration(seconds: 1),
    );
  }

  @override
  Future<AgentVoiceLocalRecording> stopAudioStream() async {
    stopAudioStreamCalls++;
    final chunks = _chunks;
    _chunks = null;
    await chunks?.close();
    return stop();
  }

  @override
  Future<void> stopAudioStreamAndDiscard() async {
    stopAudioStreamAndDiscardCalls++;
    final chunks = _chunks;
    _chunks = null;
    await chunks?.close();
    _recording = false;
    if (failEphemeralCleanup) {
      _pendingCleanup = true;
      throw const AgentVoiceRecordingException(
        AgentVoiceRecordingFailureKind.unavailable,
      );
    }
  }

  @override
  Future<void> discardCurrent() async {
    discardCurrentCalls++;
    if (_recording) {
      await discardGate?.future;
    }
    final chunks = _chunks;
    _chunks = null;
    await chunks?.close();
    _recording = false;
    if (_pendingCleanup) {
      if (!allowPendingCleanup) {
        throw const AgentVoiceRecordingException(
          AgentVoiceRecordingFailureKind.unavailable,
        );
      }
      _pendingCleanup = false;
    }
  }

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) async {
    discardedRecordings++;
  }

  @override
  Future<void> clearAccountState() async {
    clearAccountStateCalls++;
    await discardCurrent();
  }
}

final class _SingleImagePicker implements AgentImagePicker {
  @override
  Future<List<AgentLocalImage>> pickFromGallery({required int limit}) async =>
      <AgentLocalImage>[
        AgentLocalImage(
          name: 'fixture.png',
          contentType: 'image/png',
          bytes: base64Decode(
            'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk'
            '+A8AAQUBAScY42YAAAAASUVORK5CYII=',
          ),
        ),
      ];

  @override
  Future<List<AgentLocalImage>> recoverLostImages() async =>
      const <AgentLocalImage>[];

  @override
  Future<AgentLocalImage?> takePhoto() async => null;
}

final class _ControlledVoiceInputClient implements AgentVoiceInputClient {
  final events = StreamController<AgentVoiceInputEvent>.broadcast();
  StreamSubscription<Uint8List>? _audioSubscription;
  int disposeCalls = 0;

  @override
  Stream<AgentVoiceInputEvent> transcribeRealtime({
    required String threadId,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
  }) {
    _audioSubscription = audioChunks.listen((_) {});
    return events.stream;
  }

  @override
  Future<void> clearAccountState() async {
    await _audioSubscription?.cancel();
    _audioSubscription = null;
  }

  @override
  Future<void> dispose() async {
    disposeCalls++;
    await _audioSubscription?.cancel();
    await events.close();
  }
}

Future<void> _pumpVoiceOperation(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
  await tester.pump();
}

Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var attempt = 0; attempt < 100 && !condition(); attempt++) {
    await tester.pump(const Duration(milliseconds: 10));
  }
  await tester.pump();
}

String Function(String scope) _sequentialIdFactory() {
  var sequence = 0;
  return (scope) => '${scope}_${++sequence}'.replaceAll('-', '_');
}
