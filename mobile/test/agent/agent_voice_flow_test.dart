import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';

void main() {
  testWidgets(
    'home voice input shows partial text then auto-sends an ordinary Message',
    (tester) async {
      final recorder = _TrackingStreamingRecorder();
      final conversationController = ConversationController(
        client: FakeAgentClient(),
      );
      final composerController = ComposerController(
        conversationController: conversationController,
        voiceInputClient: FakeAgentVoiceInputClient(),
        voiceRecorder: recorder,
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
        AgentVoiceInputState.recording,
      );
      expect(
        find.byKey(const Key('agent-voice-live-transcript')),
        findsOneWidget,
      );
      expect(find.text('点击转文字 · 上滑取消'), findsOneWidget);
      expect(conversationController.messages, isEmpty);

      await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
      await _pumpUntil(
        tester,
        () => conversationController.messages.any(
          (message) => message.role == AgentMessageRole.user,
        ),
      );

      expect(find.byKey(const Key('agent-composer-field')), findsNothing);

      final userMessages = conversationController.messages
          .where((message) => message.role == AgentMessageRole.user)
          .toList();
      expect(userMessages, hasLength(1));
      expect(
        userMessages.single.text,
        'I explained the problem, the trade-off, and the result clearly.',
      );
      expect(userMessages.single.modality, AgentMessageModality.text);
      expect(userMessages.single.audio, isNull);
      expect(recorder.stopAudioStreamAndDiscardCalls, 1);
      expect(recorder.discardedRecordings, 0);
      expect(
        conversationController.messages.where(
          (message) => message.modality == AgentMessageModality.voice,
        ),
        isEmpty,
      );
      expect(
        conversationController.messages.where(
          (message) => message.role == AgentMessageRole.assistant,
        ),
        hasLength(1),
      );
      expect(
        composerController.voiceController?.state,
        AgentVoiceInputState.idle,
      );
    },
  );

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

  testWidgets('automatic text send failure does not retain local audio', (
    tester,
  ) async {
    final recorder = _TrackingStreamingRecorder();
    final conversationController = ConversationController(
      client: _FailingTextAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
      voiceInputClient: FakeAgentVoiceInputClient(),
      voiceRecorder: recorder,
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
    await tester.tap(find.byKey(const Key('agent-mic-placeholder')));
    await _pumpUntil(
      tester,
      () =>
          composerController.voiceController?.state ==
          AgentVoiceInputState.idle,
    );

    expect(conversationController.messages, isEmpty);
    expect(recorder.stopAudioStreamAndDiscardCalls, 1);
    expect(recorder.discardedRecordings, 0);
    expect(recorder.hasCurrentRecording, isFalse);
    expect(find.byKey(const Key('agent-composer-field')), findsNothing);
    expect(
      composerController.voiceController?.state,
      AgentVoiceInputState.idle,
    );
  });

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
      voiceInputClient: FakeAgentVoiceInputClient(),
      voiceRecorder: recorder,
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

final class _FailingTextAgentClient implements AgentClient {
  final _delegate = FakeAgentClient();

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<AgentThreadSummary> createThread() => _delegate.createThread();

  @override
  Future<void> clearFocusedThread() => _delegate.clearFocusedThread();

  @override
  Future<void> deleteThread({required String threadId}) =>
      _delegate.deleteThread(threadId: threadId);

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() =>
      _delegate.getFocusedThread();

  @override
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) =>
      _delegate.listThreads(pageSize: pageSize, cursor: cursor);

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
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) =>
      _delegate.setFocusedThread(threadId: threadId);

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.network,
      retryable: true,
    );
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
