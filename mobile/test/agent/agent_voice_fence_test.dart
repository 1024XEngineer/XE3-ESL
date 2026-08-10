import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';
import 'package:speakup/features/agent/conversation/agent_message_bubble.dart';

void main() {
  test('production-capable recorder uses realtime input before stop', () async {
    final client = _ControlledVoiceClient();
    final recorder = _StreamingVoiceRecorder();
    final controller = _controller(
      client,
      <AgentMessage>[],
      recorder: recorder,
    );
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');

    await controller.startRecording();
    expect(controller.state, AgentVoiceComposerState.recording);
    await controller.stopRecording();

    expect(client.realtimeCalls, 1);
    expect(client.fileUploadCalls, 0);
    expect(controller.state, AgentVoiceComposerState.awaitingConfirmation);
    expect(controller.editedTranscript, 'Candidate text');
  });
  test('late upload result cannot cross the Thread fence', () async {
    final client = _ControlledVoiceClient();
    final committed = <AgentMessage>[];
    final controller = _controller(client, committed);
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');
    await controller.startRecording();
    await controller.stopRecording();

    client.createCompleter = Completer<AgentVoiceCandidate>();
    final upload = controller.upload();
    await Future<void>.delayed(Duration.zero);
    await controller.bindThread('thread-b');
    client.createCompleter!.complete(_readyCandidate(threadId: 'thread-a'));
    await upload;

    expect(controller.threadId, 'thread-b');
    expect(controller.state, AgentVoiceComposerState.idle);
    expect(controller.candidate, isNull);
    expect(committed, isEmpty);
    expect(client.deletedCandidateIds, contains('candidate-a'));
  });

  test('late confirmation cannot cross Candidate and Thread fences', () async {
    final client = _ControlledVoiceClient()
      ..createResult = _readyCandidate(threadId: 'thread-a');
    final committed = <AgentMessage>[];
    final controller = _controller(client, committed);
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');
    await controller.startRecording();
    await controller.stopRecording();
    await controller.upload();
    expect(controller.candidate?.version, 1);
    controller.updateTranscript('Edited confirmed text');

    client.confirmCompleter = Completer<AgentVoiceConfirmation>();
    final confirmation = controller.confirm();
    await Future<void>.delayed(Duration.zero);
    await controller.bindThread('thread-b');
    client.confirmCompleter!.complete(
      _confirmation(
        candidate: _readyCandidate(threadId: 'thread-a'),
        text: 'Edited confirmed text',
      ),
    );
    await confirmation;

    expect(controller.threadId, 'thread-b');
    expect(controller.state, AgentVoiceComposerState.idle);
    expect(committed, isEmpty);
  });

  test('account epoch and Message fence discard late TTS bytes', () async {
    final client = _ControlledVoiceClient();
    final player = FakeAgentAudioPlayer();
    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    const message = AgentMessage(
      id: 'assistant-a',
      role: AgentMessageRole.assistant,
      text: 'Text remains visible while speech is loading.',
    );
    await conversationController.initialize();
    conversationController.commitComposerMessages(const [message]);
    final controller = AgentMessageAudioController(
      conversationController: conversationController,
      client: client,
      audioPlayer: player,
    );
    addTearDown(() {
      controller.dispose();
      conversationController.dispose();
    });

    client.speechCompleter = Completer<Uint8List>();
    final playback = controller.toggleMessagePlayback(message);
    await Future<void>.delayed(Duration.zero);
    final cleanup = controller.clearPrivateState();
    client.speechCompleter!.complete(Uint8List.fromList(_waveBytes));
    await Future.wait<void>([playback, cleanup]);

    expect(controller.threadId, isNull);
    expect(controller.loadingMessageId, isNull);
    expect(controller.playingMessageId, isNull);
    expect(player.playing, isFalse);
  });

  test(
    'Message audio projection fence stops deleted recording playback',
    () async {
      final client = _ControlledVoiceClient();
      final player = FakeAgentAudioPlayer();
      final conversationController = ConversationController(
        client: FakeAgentClient(),
      );
      const readable = AgentMessage(
        id: 'message-a',
        role: AgentMessageRole.user,
        text: 'The confirmed transcript remains durable.',
        modality: AgentMessageModality.voice,
        audio: AgentMessageAudio(
          id: 'audio-a',
          status: AgentMessageAudioStatus.readable,
          contentType: 'audio/wav',
          sizeBytes: 128,
          duration: Duration(seconds: 3),
          playbackPath: '/v1/agent-message-audios/audio-a/playback',
        ),
      );
      await conversationController.initialize();
      conversationController.commitComposerMessages(const [readable]);
      final controller = AgentMessageAudioController(
        conversationController: conversationController,
        client: client,
        audioPlayer: player,
      );
      addTearDown(() {
        controller.dispose();
        conversationController.dispose();
      });
      await controller.toggleMessagePlayback(readable);
      expect(controller.playingMessageId, readable.id);
      expect(player.playing, isTrue);

      conversationController.markMessageAudioDeleted(
        readable.id,
        readable.audio!.copyWith(
          status: AgentMessageAudioStatus.deleted,
          clearPlaybackPath: true,
          deletedAt: DateTime.utc(2026, 7, 26, 12),
        ),
      );
      await Future<void>.delayed(Duration.zero);

      expect(controller.playingMessageId, isNull);
      expect(player.playing, isFalse);
    },
  );

  test(
    'microphone denial and native stop failure remain recoverable',
    () async {
      final client = _ControlledVoiceClient();
      final deniedRecorder = FakeAgentVoiceRecorder(
        failure: AgentVoiceRecordingFailureKind.permissionDenied,
      );
      final denied = _controller(
        client,
        <AgentMessage>[],
        recorder: deniedRecorder,
      );
      addTearDown(denied.dispose);
      await denied.bindThread('thread-a');
      await denied.startRecording();
      expect(denied.state, AgentVoiceComposerState.failed);
      expect(denied.canRetry, isFalse);
      expect(denied.errorMessage, contains('麦克风权限'));

      final recorder = FakeAgentVoiceRecorder();
      final stopped = _controller(client, <AgentMessage>[], recorder: recorder);
      addTearDown(stopped.dispose);
      await stopped.bindThread('thread-a');
      await stopped.startRecording();
      recorder.failure = AgentVoiceRecordingFailureKind.unavailable;
      await stopped.stopRecording();
      expect(stopped.state, AgentVoiceComposerState.failed);
      expect(stopped.canRetry, isTrue);
      expect(stopped.errorMessage, contains('录音没有完成'));
    },
  );

  testWidgets(
    'recording ticker preserves its start clock and publishes elapsed time',
    (tester) async {
      var now = DateTime.utc(2026, 7, 27, 9);
      final controller = _controller(
        _ControlledVoiceClient(),
        <AgentMessage>[],
        clock: () => now,
      );
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();
      var notifications = 0;
      controller.addListener(() => notifications++);

      now = now.add(const Duration(seconds: 1));
      await tester.pump(const Duration(seconds: 1));

      expect(controller.recordingElapsed, const Duration(seconds: 1));
      expect(notifications, 1);

      now = now.add(const Duration(seconds: 2));
      await tester.pump(const Duration(seconds: 1));

      expect(controller.recordingElapsed, const Duration(seconds: 3));
      expect(notifications, 2);
      await controller.cancel();
    },
  );

  testWidgets('recording limit automatically stops and uploads', (
    tester,
  ) async {
    final upload = Completer<AgentVoiceCandidate>();
    final client = _ControlledVoiceClient()..createCompleter = upload;
    final controller = _controller(
      client,
      <AgentMessage>[],
      recordingLimit: const Duration(seconds: 2),
    );
    addTearDown(controller.dispose);
    await controller.bindThread('thread-a');
    await controller.startRecording();

    await tester.pump(const Duration(seconds: 2));
    await tester.pump();

    expect(controller.state, AgentVoiceComposerState.uploading);
    expect(controller.recording, isNotNull);

    upload.complete(_readyCandidate(threadId: 'thread-a'));
    await tester.pump();

    expect(controller.state, AgentVoiceComposerState.awaitingConfirmation);
    expect(controller.recording, isNull);
    expect(controller.candidate, isNotNull);
  });

  test('upload and ASR failures expose bounded retry states', () async {
    final uploadClient = _ControlledVoiceClient()
      ..createError = const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    final uploadController = _controller(uploadClient, <AgentMessage>[]);
    addTearDown(uploadController.dispose);
    await uploadController.bindThread('thread-a');
    await uploadController.startRecording();
    await uploadController.stopRecording();
    await uploadController.upload();
    expect(uploadController.state, AgentVoiceComposerState.failed);
    expect(uploadController.canRetry, isTrue);
    expect(uploadController.recording, isNotNull);
    expect(uploadController.errorMessage, contains('检查网络'));

    final asrClient = _ControlledVoiceClient()
      ..createResult = _failedCandidate(threadId: 'thread-a');
    final asrController = _controller(asrClient, <AgentMessage>[]);
    addTearDown(asrController.dispose);
    await asrController.bindThread('thread-a');
    await asrController.startRecording();
    await asrController.stopRecording();
    await asrController.upload();
    expect(asrController.state, AgentVoiceComposerState.failed);
    expect(asrController.canRetry, isTrue);
    expect(asrController.errorMessage, contains('重试识别'));
  });

  test(
    'Run retry identity is stable per failed Run and rotates for its retry',
    () async {
      final candidate = _readyCandidate(threadId: 'thread-a');
      final firstRun = _failedRun('run-a');
      final secondRun = _failedRun('run-b');
      final thirdRun = _failedRun('run-c');
      final client = _ControlledVoiceClient()
        ..createResult = candidate
        ..confirmResults.add(
          _confirmation(
            candidate: candidate,
            text: 'Candidate text',
            run: firstRun,
          ),
        )
        ..retryRunResults.addAll(<Object>[
          const AgentClientException(
            kind: AgentClientFailureKind.network,
            retryable: true,
          ),
          secondRun,
          thirdRun,
        ]);
      final controller = _controller(client, <AgentMessage>[]);
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();
      await controller.stopRecording();
      await controller.upload();
      await controller.confirm();

      await controller.retry();
      await controller.retry();
      await controller.retry();

      expect(client.retryRunCalls.map((call) => call.runId), <String>[
        'run-a',
        'run-a',
        'run-b',
      ]);
      expect(
        client.retryRunCalls[0].clientRetryId,
        client.retryRunCalls[1].clientRetryId,
      );
      expect(
        client.retryRunCalls[2].clientRetryId,
        isNot(client.retryRunCalls[1].clientRetryId),
      );
    },
  );

  test('ambiguous ASR retry GET-reconciles before any second POST', () async {
    final failed = _failedCandidate(threadId: 'thread-a');
    final ready = _readyCandidate(threadId: 'thread-a', version: 2);
    final retryClient = _ControlledVoiceClient()
      ..createResult = failed
      ..retryCandidateResults.addAll(<Object>[
        const AgentClientException(
          kind: AgentClientFailureKind.network,
          retryable: true,
        ),
        ready,
      ])
      ..getCandidateResults.add(failed);
    final retryController = _controller(retryClient, <AgentMessage>[]);
    addTearDown(retryController.dispose);
    await retryController.bindThread('thread-a');
    await retryController.startRecording();
    await retryController.stopRecording();
    await retryController.upload();
    retryClient.asrOperations.clear();

    await retryController.retry();
    await retryController.retry();

    expect(retryClient.asrOperations, <String>['POST', 'GET', 'POST']);
    expect(retryController.state, AgentVoiceComposerState.awaitingConfirmation);

    final reconciledClient = _ControlledVoiceClient()
      ..createResult = failed
      ..retryCandidateResults.add(
        const AgentClientException(
          kind: AgentClientFailureKind.network,
          retryable: true,
        ),
      )
      ..getCandidateResults.add(ready);
    final reconciledController = _controller(
      reconciledClient,
      <AgentMessage>[],
    );
    addTearDown(reconciledController.dispose);
    await reconciledController.bindThread('thread-a');
    await reconciledController.startRecording();
    await reconciledController.stopRecording();
    await reconciledController.upload();
    reconciledClient.asrOperations.clear();

    await reconciledController.retry();
    await reconciledController.retry();

    expect(reconciledClient.asrOperations, <String>['POST', 'GET']);
    expect(
      reconciledController.state,
      AgentVoiceComposerState.awaitingConfirmation,
    );
  });

  test(
    'ambiguous confirmation freezes its command and reconciles Message and Run',
    () async {
      const confirmedText = 'Frozen confirmed transcript';
      final candidate = _readyCandidate(threadId: 'thread-a');
      final durableRun = _failedRun('run-a', retryable: false);
      final durable = _confirmation(
        candidate: candidate,
        text: confirmedText,
        run: durableRun,
      );
      final client = _ControlledVoiceClient()
        ..createResult = candidate
        ..confirmResults.add(
          const AgentClientException(
            kind: AgentClientFailureKind.network,
            retryable: true,
          ),
        )
        ..getCandidateResults.add(durable.candidate)
        ..getRunResults.add(durableRun)
        ..messages[durable.message.id] = durable.message;
      final committed = <AgentMessage>[];
      final controller = _controller(client, committed);
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();
      await controller.stopRecording();
      await controller.upload();
      controller.updateTranscript(confirmedText);

      await controller.confirm();
      controller.updateTranscript('Must not replace the frozen command');
      expect(controller.editedTranscript, confirmedText);
      await controller.retry();

      expect(client.confirmationCalls, hasLength(1));
      expect(client.getMessageCalls, <String>['message-a']);
      expect(client.getRunCalls, <String>['run-a']);
      expect(committed.single.text, confirmedText);

      final replayClient = _ControlledVoiceClient()
        ..createResult = candidate
        ..confirmResults.addAll(<Object>[
          const AgentClientException(
            kind: AgentClientFailureKind.network,
            retryable: true,
          ),
          durable,
        ])
        ..getCandidateResults.add(candidate);
      final replayController = _controller(replayClient, <AgentMessage>[]);
      addTearDown(replayController.dispose);
      await replayController.bindThread('thread-a');
      await replayController.startRecording();
      await replayController.stopRecording();
      await replayController.upload();
      replayController.updateTranscript(confirmedText);
      await replayController.confirm();
      replayController.updateTranscript('Different text');
      await replayController.retry();

      expect(replayClient.confirmationCalls, hasLength(2));
      expect(
        replayClient.confirmationCalls[1],
        replayClient.confirmationCalls[0],
      );
    },
  );

  test(
    'background preserves confirmed Run and foreground resumes it with GET',
    () async {
      final candidate = _readyCandidate(threadId: 'thread-a');
      final pendingRun = AgentVoiceRun(
        id: 'run-a',
        threadId: 'thread-a',
        inputMessageId: 'message-a',
        status: AgentVoiceRunStatus.pending,
      );
      final stalledRun = Completer<AgentVoiceRun>();
      final client = _ControlledVoiceClient()
        ..createResult = candidate
        ..confirmResults.add(
          _confirmation(
            candidate: candidate,
            text: 'Candidate text',
            run: pendingRun,
          ),
        )
        ..getRunCompleter = stalledRun;
      final controller = _controller(client, <AgentMessage>[]);
      addTearDown(controller.dispose);
      await controller.bindThread('thread-a');
      await controller.startRecording();
      await controller.stopRecording();
      await controller.upload();

      final confirmation = controller.confirm();
      await Future<void>.delayed(Duration.zero);
      controller.didChangeAppLifecycleState(AppLifecycleState.paused);
      expect(controller.state, AgentVoiceComposerState.awaitingAssistant);
      expect(controller.canStartRecording, isFalse);
      await controller.startRecording();
      expect(controller.state, AgentVoiceComposerState.awaitingAssistant);
      stalledRun.complete(pendingRun);
      await confirmation;

      client.getRunResults.add(_failedRun('run-a', retryable: false));
      controller.didChangeAppLifecycleState(AppLifecycleState.resumed);
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);

      expect(client.getRunCalls, <String>['run-a', 'run-a']);
      expect(controller.state, AgentVoiceComposerState.failed);
      expect(controller.canStartRecording, isFalse);
    },
  );

  test(
    '401 and background transitions synchronously clear private audio',
    () async {
      final authenticationClient = _ControlledVoiceClient()
        ..createError = const AgentClientException(
          kind: AgentClientFailureKind.authenticationRequired,
          statusCode: 401,
        );
      final authenticatedRecorder = FakeAgentVoiceRecorder();
      final authenticationController = _controller(
        authenticationClient,
        <AgentMessage>[],
        recorder: authenticatedRecorder,
      );
      addTearDown(authenticationController.dispose);
      await authenticationController.bindThread('thread-a');
      await authenticationController.startRecording();
      await authenticationController.stopRecording();
      await authenticationController.upload();
      expect(authenticationController.state, AgentVoiceComposerState.idle);
      expect(authenticationController.recording, isNull);
      expect(authenticationController.candidate, isNull);

      final backgroundController = _controller(
        _ControlledVoiceClient(),
        <AgentMessage>[],
      );
      addTearDown(backgroundController.dispose);
      await backgroundController.bindThread('thread-a');
      await backgroundController.startRecording();
      backgroundController.didChangeAppLifecycleState(AppLifecycleState.paused);
      await Future<void>.delayed(Duration.zero);
      expect(backgroundController.state, AgentVoiceComposerState.idle);
      expect(backgroundController.recording, isNull);
    },
  );

  testWidgets(
    'TTS and private OSS failures never hide canonical Message text',
    (tester) async {
      final client = _ControlledVoiceClient()
        ..speechError = const AgentClientException(
          kind: AgentClientFailureKind.network,
          retryable: true,
        )
        ..messageAudioError = const AgentClientException(
          kind: AgentClientFailureKind.notFound,
        );
      final conversationController = ConversationController(
        client: FakeAgentClient(),
      );
      const assistant = AgentMessage(
        id: 'assistant-a',
        role: AgentMessageRole.assistant,
        text: 'Canonical assistant text must always remain visible.',
      );
      const user = AgentMessage(
        id: 'message-a',
        role: AgentMessageRole.user,
        text: 'Canonical confirmed transcript remains after OSS failure.',
        modality: AgentMessageModality.voice,
        audio: AgentMessageAudio(
          id: 'audio-a',
          status: AgentMessageAudioStatus.readable,
          contentType: 'audio/wav',
          sizeBytes: 128,
          duration: Duration(seconds: 3),
          playbackPath: '/v1/agent-message-audios/audio-a/playback',
        ),
      );
      await conversationController.initialize();
      conversationController.commitComposerMessages(const [user, assistant]);
      final controller = AgentMessageAudioController(
        conversationController: conversationController,
        client: client,
        audioPlayer: FakeAgentAudioPlayer(),
      );
      addTearDown(() {
        controller.dispose();
        conversationController.dispose();
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ListView(
              children: [
                AgentMessageBubble(
                  message: assistant,
                  messageAudioController: controller,
                ),
                AgentMessageBubble(
                  message: user,
                  messageAudioController: controller,
                ),
              ],
            ),
          ),
        ),
      );

      await tester.tap(
        find.byKey(const Key('agent-assistant-tts-assistant-a')),
      );
      await _pumpVoiceOperation(tester);
      expect(
        find.byKey(const Key('agent-assistant-text-assistant-a')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('agent-message-media-error-assistant-a')),
        findsOneWidget,
      );

      await tester.tap(
        find.byKey(const Key('agent-user-voice-play-message-a')),
      );
      await _pumpVoiceOperation(tester);
      expect(
        find.byKey(const Key('agent-user-voice-transcript-message-a')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('agent-message-media-error-message-a')),
        findsOneWidget,
      );
    },
  );
}

Future<void> _pumpVoiceOperation(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

final class _StreamingVoiceRecorder
    implements AgentVoiceRecorder, AgentVoiceStreamingRecorder {
  StreamController<Uint8List>? _stream;

  @override
  Future<Stream<Uint8List>> startAudioStream() async {
    final stream = StreamController<Uint8List>();
    _stream = stream;
    stream.add(Uint8List.fromList(<int>[1, 2, 3, 4]));
    return stream.stream;
  }

  @override
  Future<AgentVoiceLocalRecording> stopAudioStream() async {
    await _stream?.close();
    _stream = null;
    return const AgentVoiceLocalRecording(
      path: '/tmp/realtime.wav',
      contentType: 'audio/wav',
      sizeBytes: 48,
      duration: Duration(seconds: 1),
    );
  }

  @override
  Future<void> start() => throw UnimplementedError();

  @override
  Future<AgentVoiceLocalRecording> stop() => throw UnimplementedError();

  @override
  Future<void> discardCurrent() async {
    await _stream?.close();
    _stream = null;
  }

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) async {}

  @override
  Future<void> clearAccountState() => discardCurrent();
}

AgentVoiceController _controller(
  _ControlledVoiceClient client,
  List<AgentMessage> committed, {
  FakeAgentAudioPlayer? player,
  AgentVoiceRecorder? recorder,
  AgentVoiceControllerClock? clock,
  Duration recordingLimit = const Duration(seconds: 58),
}) {
  var sequence = 0;
  return AgentVoiceController(
    client: client,
    recorder: recorder ?? FakeAgentVoiceRecorder(),
    audioPlayer: player ?? FakeAgentAudioPlayer(),
    onMessagesCommitted: committed.addAll,
    idFactory: (scope) => '${scope}_${++sequence}'.replaceAll('-', '_'),
    clock: clock ?? DateTime.now,
    recordingLimit: recordingLimit,
    pollInterval: Duration.zero,
    maximumCandidatePolls: 2,
    maximumRunPolls: 2,
  );
}

AgentVoiceCandidate _failedCandidate({
  required String threadId,
  int version = 1,
}) {
  final now = DateTime.utc(2026, 7, 26, 12);
  return AgentVoiceCandidate(
    id: 'candidate-a',
    threadId: threadId,
    status: AgentVoiceCandidateStatus.failed,
    asrAttempt: version,
    version: version,
    recording: const AgentVoiceRecordingMetadata(
      contentType: 'audio/wav',
      sizeBytes: 128,
      duration: Duration(seconds: 3),
      sampleRate: 16000,
    ),
    failure: const AgentVoiceCandidateFailure(
      kind: 'provider_unavailable',
      retryable: true,
    ),
    expiresAt: now.add(const Duration(hours: 1)),
    createdAt: now,
    updatedAt: now,
  );
}

AgentVoiceCandidate _readyCandidate({
  required String threadId,
  int version = 1,
}) {
  final now = DateTime.utc(2026, 7, 26, 12);
  return AgentVoiceCandidate(
    id: 'candidate-a',
    threadId: threadId,
    status: AgentVoiceCandidateStatus.candidateReady,
    asrAttempt: version,
    version: version,
    recording: const AgentVoiceRecordingMetadata(
      contentType: 'audio/wav',
      sizeBytes: 128,
      duration: Duration(seconds: 3),
      sampleRate: 16000,
    ),
    transcript: const AgentVoiceTranscript(
      text: 'Candidate text',
      requestId: 'request-a',
      provider: 'fake',
      model: 'fake-asr',
    ),
    expiresAt: now.add(const Duration(hours: 1)),
    createdAt: now,
    updatedAt: now,
  );
}

AgentVoiceRun _failedRun(String id, {bool retryable = true}) {
  return AgentVoiceRun(
    id: id,
    threadId: 'thread-a',
    inputMessageId: 'message-a',
    status: AgentVoiceRunStatus.failed,
    failureKind: 'provider_unavailable',
    failureRetryable: retryable,
  );
}

AgentVoiceConfirmation _confirmation({
  required AgentVoiceCandidate candidate,
  required String text,
  AgentVoiceRun? run,
}) {
  final now = candidate.updatedAt;
  const messageId = 'message-a';
  const audioId = 'audio-a';
  final resolvedRun =
      run ??
      AgentVoiceRun(
        id: 'run-a',
        threadId: candidate.threadId,
        inputMessageId: messageId,
        status: AgentVoiceRunStatus.completed,
        assistantMessageId: 'assistant-a',
      );
  final confirmed = AgentVoiceCandidate(
    id: candidate.id,
    threadId: candidate.threadId,
    status: AgentVoiceCandidateStatus.confirmed,
    asrAttempt: candidate.asrAttempt,
    version: candidate.version,
    recording: candidate.recording,
    transcript: candidate.transcript,
    expiresAt: candidate.expiresAt,
    confirmedMessageId: messageId,
    confirmedRunId: resolvedRun.id,
    messageAudioId: audioId,
    confirmedAt: now,
    createdAt: candidate.createdAt,
    updatedAt: now,
  );
  return AgentVoiceConfirmation(
    candidate: confirmed,
    message: AgentMessage(
      id: messageId,
      role: AgentMessageRole.user,
      text: text,
      modality: AgentMessageModality.voice,
      audio: const AgentMessageAudio(
        id: audioId,
        status: AgentMessageAudioStatus.readable,
        contentType: 'audio/wav',
        sizeBytes: 128,
        duration: Duration(seconds: 3),
        playbackPath: '/v1/agent-message-audios/audio-a/playback',
      ),
    ),
    run: resolvedRun,
  );
}

typedef _ConfirmationCall = ({
  String candidateId,
  int candidateVersion,
  String clientMessageId,
  String confirmedText,
});

typedef _RetryRunCall = ({String runId, String clientRetryId});

final class _ControlledVoiceClient
    implements
        AgentVoiceClient,
        AgentVoiceRealtimeInputClient,
        AgentMessageAudioClient {
  Completer<AgentVoiceCandidate>? createCompleter;
  Completer<AgentVoiceConfirmation>? confirmCompleter;
  Completer<Uint8List>? speechCompleter;
  AgentVoiceCandidate? createResult;
  Object? createError;
  Object? speechError;
  Object? messageAudioError;
  final List<Object> getCandidateResults = <Object>[];
  final List<Object> retryCandidateResults = <Object>[];
  final List<Object> confirmResults = <Object>[];
  final List<Object> getRunResults = <Object>[];
  final List<Object> retryRunResults = <Object>[];
  final Map<String, AgentMessage> messages = <String, AgentMessage>{};
  Completer<AgentVoiceRun>? getRunCompleter;
  final List<String> asrOperations = <String>[];
  final List<_ConfirmationCall> confirmationCalls = <_ConfirmationCall>[];
  final List<_RetryRunCall> retryRunCalls = <_RetryRunCall>[];
  final List<String> getRunCalls = <String>[];
  final List<String> getMessageCalls = <String>[];
  final List<String> deletedCandidateIds = <String>[];
  int realtimeCalls = 0;
  int fileUploadCalls = 0;

  @override
  Stream<AgentVoiceTranscriptionEvent> createCandidateStream({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) async* {
    fileUploadCalls++;
    final candidate = await createCandidate(
      threadId: threadId,
      recording: recording,
      idempotencyKey: idempotencyKey,
    );
    yield AgentVoiceCandidateCompleted(candidate);
  }

  @override
  Stream<AgentVoiceTranscriptionEvent> createCandidateRealtime({
    required String threadId,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
  }) async* {
    realtimeCalls++;
    await for (final _ in audioChunks) {}
    yield const AgentVoiceTranscriptUpdated(
      text: 'Realtime candidate text.',
      finalResult: true,
    );
    yield AgentVoiceCandidateCompleted(_readyCandidate(threadId: threadId));
  }

  @override
  Future<AgentVoiceCandidate> createCandidate({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) {
    if (createError case final error?) {
      return Future<AgentVoiceCandidate>.error(error);
    }
    return createCompleter?.future ??
        Future<AgentVoiceCandidate>.value(
          createResult ?? _readyCandidate(threadId: threadId),
        );
  }

  @override
  Future<AgentVoiceConfirmation> confirmCandidate({
    required String candidateId,
    required int candidateVersion,
    required String clientMessageId,
    required String confirmedText,
  }) {
    confirmationCalls.add((
      candidateId: candidateId,
      candidateVersion: candidateVersion,
      clientMessageId: clientMessageId,
      confirmedText: confirmedText,
    ));
    if (confirmResults.isNotEmpty) {
      return _result<AgentVoiceConfirmation>(confirmResults.removeAt(0));
    }
    return confirmCompleter?.future ??
        Future<AgentVoiceConfirmation>.value(
          _confirmation(
            candidate: createResult ?? _readyCandidate(threadId: 'thread-a'),
            text: confirmedText,
          ),
        );
  }

  @override
  Future<void> deleteCandidate({required String candidateId}) async {
    deletedCandidateIds.add(candidateId);
  }

  @override
  Future<AgentVoiceCandidate> getCandidate({
    required String candidateId,
  }) async {
    asrOperations.add('GET');
    if (getCandidateResults.isNotEmpty) {
      return _result<AgentVoiceCandidate>(getCandidateResults.removeAt(0));
    }
    return createResult ?? _readyCandidate(threadId: 'thread-a');
  }

  @override
  Future<AgentVoiceCandidate> retryCandidate({required String candidateId}) {
    asrOperations.add('POST');
    if (retryCandidateResults.isNotEmpty) {
      return _result<AgentVoiceCandidate>(retryCandidateResults.removeAt(0));
    }
    return getCandidate(candidateId: candidateId);
  }

  @override
  Future<AgentVoiceRun> getRun({required String runId}) {
    getRunCalls.add(runId);
    final completer = getRunCompleter;
    if (completer != null) {
      getRunCompleter = null;
      return completer.future;
    }
    if (getRunResults.isNotEmpty) {
      return _result<AgentVoiceRun>(getRunResults.removeAt(0));
    }
    throw UnimplementedError();
  }

  @override
  Future<AgentVoiceRun> retryRun({
    required String runId,
    required String clientRetryId,
  }) {
    retryRunCalls.add((runId: runId, clientRetryId: clientRetryId));
    if (retryRunResults.isNotEmpty) {
      return _result<AgentVoiceRun>(retryRunResults.removeAt(0));
    }
    throw UnimplementedError();
  }

  @override
  Future<AgentMessage?> getMessage({
    required String threadId,
    required String messageId,
  }) async {
    getMessageCalls.add(messageId);
    return messages[messageId];
  }

  @override
  Future<Uint8List> loadAssistantSpeech({required String messageId}) {
    if (speechError case final error?) {
      return Future<Uint8List>.error(error);
    }
    return speechCompleter?.future ??
        Future<Uint8List>.value(Uint8List.fromList(_waveBytes));
  }

  @override
  Future<Uint8List> loadSpeechPreview({
    required String messageId,
    required String text,
  }) {
    return loadAssistantSpeech(messageId: messageId);
  }

  @override
  Future<Uint8List> loadMessageAudio({required String audioId}) async {
    if (messageAudioError case final error?) {
      return Future<Uint8List>.error(error);
    }
    return Uint8List.fromList(_waveBytes);
  }

  @override
  Future<void> deleteMessageAudio({required String audioId}) async {}

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

Future<T> _result<T>(Object result) {
  if (result is T) {
    return Future<T>.value(result as T);
  }
  return Future<T>.error(result);
}

const _waveBytes = <int>[
  0x52,
  0x49,
  0x46,
  0x46,
  0x28,
  0x00,
  0x00,
  0x00,
  0x57,
  0x41,
  0x56,
  0x45,
  0x66,
  0x6d,
  0x74,
  0x20,
  0x10,
  0x00,
  0x00,
  0x00,
  0x01,
  0x00,
  0x01,
  0x00,
  0x80,
  0x3e,
  0x00,
  0x00,
  0x00,
  0x7d,
  0x00,
  0x00,
  0x02,
  0x00,
  0x10,
  0x00,
  0x64,
  0x61,
  0x74,
  0x61,
  0x04,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
];
