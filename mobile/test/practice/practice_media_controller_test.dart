import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'restore exposes only the latest server recording and plays both sources',
    () async {
      final media = _MediaClient();
      final player = _AudioPlayer();
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-latest'),
        media: media,
        player: player,
      );
      addTearDown(controller.dispose);

      await controller.initialize();

      expect(controller.recordings.map((recording) => recording.audioAssetId), [
        'audio-latest',
      ]);
      await controller.toggleQuestionAudio();
      expect(player.lastPlayed, _wave());
      expect(media.lastQuestionBytes, everyElement(0));
      expect(controller.isQuestionAudioPlaying, isTrue);

      await controller.toggleRecordingAudio('audio-latest');
      expect(player.stopCount, greaterThanOrEqualTo(2));
      expect(player.lastPlayed, _wave());
      expect(media.lastRecordingBytes, everyElement(0));
      expect(controller.isRecordingAudioPlaying('audio-latest'), isTrue);
    },
  );

  test('play, stop, and successful delete remove the UI handle', () async {
    final media = _MediaClient();
    final player = _AudioPlayer();
    final controller = _controller(
      snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    await controller.toggleRecordingAudio('audio-1');
    await controller.toggleRecordingAudio('audio-1');

    expect(controller.isRecordingAudioPlaying('audio-1'), isFalse);
    await controller.deleteRecording('audio-1');
    expect(media.deleted, ['audio-1']);
    expect(controller.recordings, isEmpty);
  });

  test(
    'recording critical states reject direct media playback calls',
    () async {
      final media = _MediaClient();
      final player = _AudioPlayer();
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
        media: media,
        player: player,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await controller.startRecording();

      expect(controller.recordingState, PracticeRecordingState.recording);
      expect(controller.canUsePracticeAudio, isFalse);
      await controller.toggleQuestionAudio();
      await controller.toggleRecordingAudio('audio-1');

      expect(media.questionLoadCount, 0);
      expect(media.recordingLoadCount, 0);
      expect(player.playCount, 0);
      await controller.clearPrivateState();
    },
  );

  test(
    'recording waits for existing playback cleanup before microphone start',
    () async {
      final media = _MediaClient();
      final player = _AudioPlayer();
      final recorder = _OrderingRecorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: _SnapshotPracticeClient(
          _activeSnapshot(audioAssetId: 'audio-1'),
        ),
        recorder: recorder,
        mediaClient: media,
        audioPlayer: player,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await controller.toggleQuestionAudio();
      final stopGate = player.nextStopGate = Completer<void>();

      final start = controller.startRecording();
      await Future<void>.delayed(Duration.zero);
      expect(controller.recordingState, PracticeRecordingState.starting);
      expect(recorder.startCount, 0);

      stopGate.complete();
      await start;
      expect(recorder.startCount, 1);
      expect(controller.recordingState, PracticeRecordingState.recording);
      await controller.clearPrivateState();
    },
  );

  test('player interruption clears stale UI and one tap can replay', () async {
    final media = _MediaClient();
    final player = _AudioPlayer();
    final controller = _controller(
      snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.toggleQuestionAudio();
    expect(controller.isQuestionAudioPlaying, isTrue);

    player.complete();
    await Future<void>.delayed(Duration.zero);
    expect(controller.isQuestionAudioPlaying, isFalse);

    await controller.toggleQuestionAudio();
    expect(player.playCount, 2);
    expect(controller.isQuestionAudioPlaying, isTrue);
  });

  test(
    'background fences a pending media request even when it resumes first',
    () async {
      final pendingQuestion = Completer<Uint8List>();
      final media = _MediaClient(pendingQuestion: pendingQuestion);
      final player = _AudioPlayer();
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
        media: media,
        player: player,
      );
      addTearDown(controller.dispose);
      addTearDown(
        () => binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
      );
      await controller.initialize();

      final playback = controller.toggleQuestionAudio();
      await media.questionStarted.future;
      binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      pendingQuestion.complete(_wave());
      await playback;

      expect(player.playCount, 0);
      expect(controller.isQuestionAudioLoading, isFalse);
      expect(controller.isQuestionAudioPlaying, isFalse);

      await controller.toggleQuestionAudio();
      expect(player.playCount, 1);
      expect(controller.isQuestionAudioPlaying, isTrue);
    },
  );

  test(
    'delete error retains the recording and surfaces a bounded message',
    () async {
      final media = _MediaClient(
        deleteError: const AgentClientException(
          kind: AgentClientFailureKind.server,
          statusCode: 503,
          retryable: true,
        ),
      );
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
        media: media,
        player: _AudioPlayer(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      await controller.deleteRecording('audio-1');

      expect(controller.recordings, hasLength(1));
      expect(controller.mediaErrorMessage, '删除录音暂时不可用，请稍后重试。');
    },
  );

  test('delete fences a loading copy before deleting the same asset', () async {
    final load = Completer<Uint8List>();
    final media = _MediaClient(pendingRecording: load);
    final player = _AudioPlayer();
    final controller = _controller(
      snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    final playback = controller.toggleRecordingAudio('audio-1');
    await media.recordingStarted.future;
    final deletion = controller.deleteRecording('audio-1');
    await Future<void>.delayed(Duration.zero);
    expect(media.deleted, isEmpty);

    load.complete(_wave());
    await playback;
    await deletion;

    expect(player.playCount, 0);
    expect(media.deleted, ['audio-1']);
    expect(controller.recordings, isEmpty);
  });

  test(
    'tab playback stop cannot invalidate a pending recording deletion',
    () async {
      final deletionGate = Completer<void>();
      final media = _MediaClient(pendingDelete: deletionGate);
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
        media: media,
        player: _AudioPlayer(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      final deletion = controller.deleteRecording('audio-1');
      await media.deleteStarted.future;
      expect(controller.isRecordingDeleting('audio-1'), isTrue);
      await controller.stopPracticeAudio();
      deletionGate.complete();
      await deletion;

      expect(controller.recordings, isEmpty);
      expect(controller.isRecordingDeleting('audio-1'), isFalse);
    },
  );

  test(
    'private cleanup propagates a permanent player deletion failure',
    () async {
      final player = _AudioPlayer(
        clearError: const PracticeAudioPlaybackException(),
      );
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
        media: _MediaClient(),
        player: player,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      await expectLater(
        controller.clearPrivateState(),
        throwsA(isA<PracticeAudioPlaybackException>()),
      );

      expect(controller.recordings, isEmpty);
      expect(controller.practiceSessionId, isNull);
      expect(player.clearCount, greaterThanOrEqualTo(1));
    },
  );

  test(
    'logout fences a late fetch and clears player and private handles',
    () async {
      final load = Completer<Uint8List>();
      final media = _MediaClient(pendingRecording: load);
      final player = _AudioPlayer();
      final controller = _controller(
        snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
        media: media,
        player: player,
      );
      addTearDown(controller.dispose);
      await controller.initialize();

      final playback = controller.toggleRecordingAudio('audio-1');
      await media.recordingStarted.future;
      final cleanup = controller.clearPrivateState();
      load.complete(_wave());
      await playback;
      await cleanup;

      expect(controller.recordings, isEmpty);
      expect(controller.practiceSessionId, isNull);
      expect(player.playCount, 0);
      expect(player.clearCount, greaterThanOrEqualTo(1));
      expect(media.clearCount, 1);
    },
  );

  test('media 401 stops and clears native playback state', () async {
    final media = _MediaClient(
      recordingError: const AgentClientException(
        kind: AgentClientFailureKind.authenticationRequired,
        statusCode: 401,
      ),
    );
    final player = _AudioPlayer();
    final controller = _controller(
      snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    await controller.toggleRecordingAudio('audio-1');

    expect(controller.mediaErrorMessage, '登录状态已失效，请重新登录。');
    expect(player.clearCount, 1);
    expect(controller.isRecordingAudioLoading('audio-1'), isFalse);
  });

  testWidgets('Practice controls remain usable on narrow and large text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 780);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = _controller(
      snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
      media: _MediaClient(),
      player: _AudioPlayer(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    await tester.pumpWidget(
      MediaQuery(
        data: const MediaQueryData(
          textScaler: TextScaler.linear(2),
          viewInsets: EdgeInsets.only(bottom: 240),
        ),
        child: MaterialApp(home: PracticePage(agentController: controller)),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-question-audio')), findsOneWidget);
    await tester.scrollUntilVisible(
      find.byKey(const Key('practice-recording-play-audio-1')),
      220,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.drag(
      find.byType(ListView),
      const Offset(0, -180),
      warnIfMissed: false,
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-recording-play-audio-1')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('practice-recording-play-audio-1')).hitTestable(),
      findsOneWidget,
    );
    await tester.tap(
      find.byKey(const Key('practice-recording-play-audio-1')).hitTestable(),
    );
    await tester.pump();
    expect(controller.isRecordingAudioPlaying('audio-1'), isTrue);
    expect(tester.takeException(), isNull);
  });

  testWidgets('Practice media buttons are disabled while recording', (
    tester,
  ) async {
    final controller = _controller(
      snapshot: _activeSnapshot(audioAssetId: 'audio-1'),
      media: _MediaClient(),
      player: _AudioPlayer(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.startRecording();
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    expect(
      tester
          .widget<IconButton>(find.byKey(const Key('practice-question-audio')))
          .onPressed,
      isNull,
    );
    await tester.scrollUntilVisible(
      find.byKey(const Key('practice-recording-play-audio-1')),
      220,
      scrollable: find.byType(Scrollable).first,
    );
    expect(
      tester
          .widget<IconButton>(
            find.byKey(const Key('practice-recording-play-audio-1')),
          )
          .onPressed,
      isNull,
    );
    await controller.clearPrivateState();
  });

  testWidgets('Review exposes the same compact recording boundary', (
    tester,
  ) async {
    final controller = _controller(
      snapshot: _completedSnapshot(audioAssetId: 'audio-final'),
      media: _MediaClient(),
      player: _AudioPlayer(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(agentController: controller)),
    );

    expect(
      find.byKey(const Key('practice-recording-play-audio-final')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('practice-recording-delete-audio-final')),
      findsOneWidget,
    );
  });
}

AgentController _controller({
  required PracticeSessionSnapshot snapshot,
  required PracticeMediaClient media,
  required PracticeAudioPlayer player,
}) {
  return AgentController(
    client: FakeAgentClient(),
    practiceClient: _SnapshotPracticeClient(snapshot),
    mediaClient: media,
    audioPlayer: player,
  );
}

PracticeSessionSnapshot _activeSnapshot({required String audioAssetId}) {
  return PracticeSessionSnapshot(
    sessionId: 'session-1',
    matter: AgentMatter(id: 'matter-1', scene: agentScenes.first),
    completedTurns: 1,
    turnLimit: 2,
    sessionCompleted: false,
    currentQuestion: const PracticeQuestion(
      id: 'question-2',
      sessionId: 'session-1',
      text: 'What did you learn from this result?',
      speechPath: '/v1/voice-questions/question-2/speech',
    ),
    currentTurn: PracticeTurnSnapshot(
      id: 'turn-1',
      sessionId: 'session-1',
      questionId: 'question-1',
      respondentParticipantId: 'participant-user',
      candidateId: 'candidate-1',
      answerText: 'I reduced rollout risk.',
      evidenceVersion: 1,
      effectiveTurns: 1,
      sessionCompleted: false,
      audioAssetId: audioAssetId,
    ),
  );
}

PracticeSessionSnapshot _completedSnapshot({required String audioAssetId}) {
  return PracticeSessionSnapshot(
    sessionId: 'session-1',
    matter: AgentMatter(id: 'matter-1', scene: agentScenes.first),
    completedTurns: 2,
    turnLimit: 2,
    sessionCompleted: true,
    currentTurn: PracticeTurnSnapshot(
      id: 'turn-2',
      sessionId: 'session-1',
      questionId: 'question-2',
      respondentParticipantId: 'participant-user',
      candidateId: 'candidate-2',
      answerText: 'I learned to surface risks early.',
      evidenceVersion: 2,
      effectiveTurns: 2,
      sessionCompleted: true,
      reviewId: 'review-1',
      audioAssetId: audioAssetId,
    ),
    review: const AgentReview(
      id: 'review-1',
      title: '本次复盘',
      summary: '表达清晰。',
      strength: '有具体证据。',
      nextFocus: '补充取舍。',
    ),
  );
}

final class _SnapshotPracticeClient implements PracticeClient {
  _SnapshotPracticeClient(this.snapshot);

  final PracticeSessionSnapshot snapshot;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async => snapshot;

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async => PracticeStartResult(snapshot: snapshot);

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }
}

final class _MediaClient implements PracticeMediaClient {
  _MediaClient({
    this.deleteError,
    this.recordingError,
    this.pendingRecording,
    this.pendingQuestion,
    this.pendingDelete,
  });

  final Object? deleteError;
  final Object? recordingError;
  final Completer<Uint8List>? pendingRecording;
  final Completer<Uint8List>? pendingQuestion;
  final Completer<void>? pendingDelete;
  final Completer<void> recordingStarted = Completer<void>();
  final Completer<void> questionStarted = Completer<void>();
  final Completer<void> deleteStarted = Completer<void>();
  Uint8List? lastQuestionBytes;
  Uint8List? lastRecordingBytes;
  final List<String> deleted = [];
  int clearCount = 0;
  int questionLoadCount = 0;
  int recordingLoadCount = 0;

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async {
    questionLoadCount++;
    if (!questionStarted.isCompleted) {
      questionStarted.complete();
    }
    final pending = pendingQuestion;
    return lastQuestionBytes = pending != null && questionLoadCount == 1
        ? await pending.future
        : _wave();
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) async {
    recordingLoadCount++;
    if (!recordingStarted.isCompleted) {
      recordingStarted.complete();
    }
    if (recordingError case final error?) {
      throw error;
    }
    final pending = pendingRecording;
    return lastRecordingBytes = pending == null
        ? _wave()
        : await pending.future;
  }

  @override
  Future<void> deleteRecording(String audioAssetId) async {
    if (deleteError case final error?) {
      throw error;
    }
    if (!deleteStarted.isCompleted) {
      deleteStarted.complete();
    }
    await pendingDelete?.future;
    deleted.add(audioAssetId);
  }

  @override
  Future<void> clearAccountState() async => clearCount++;

  @override
  Future<void> dispose() async {}
}

final class _AudioPlayer implements PracticeAudioPlayer {
  _AudioPlayer({this.clearError});

  final Object? clearError;
  final StreamController<void> _completions = StreamController<void>.broadcast(
    sync: true,
  );
  Uint8List? lastPlayed;
  int playCount = 0;
  int stopCount = 0;
  int clearCount = 0;
  Completer<void>? nextStopGate;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playWav(Uint8List bytes) async {
    playCount++;
    lastPlayed = Uint8List.fromList(bytes);
  }

  @override
  Future<void> stop() async {
    stopCount++;
    final gate = nextStopGate;
    nextStopGate = null;
    await gate?.future;
  }

  @override
  Future<void> clearAccountState() async {
    clearCount++;
    stopCount++;
    if (clearError case final error?) {
      throw error;
    }
  }

  @override
  Future<void> dispose() async {
    await _completions.close();
  }

  void complete() => _completions.add(null);
}

final class _OrderingRecorder implements PracticeRecorder {
  int startCount = 0;

  @override
  Future<void> start() async {
    startCount++;
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    return const RecordedPracticeAudio(
      path: 'ordering.wav',
      contentType: 'audio/wav',
      sizeBytes: 44,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> clearAccountState() async {}
}

Uint8List _wave() {
  final bytes = Uint8List(44);
  bytes.setAll(0, const [0x52, 0x49, 0x46, 0x46]);
  bytes.setAll(8, const [0x57, 0x41, 0x56, 0x45]);
  return bytes;
}
