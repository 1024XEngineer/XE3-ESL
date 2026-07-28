import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

void main() {
  test('consumes server turnLimit and Review from confirmation', () async {
    final practice = _TwoTurnPracticeClient();
    final recorder = _Recorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-id',
    );

    await controller.initialize();
    final threadId = controller.threadId;
    await controller.selectScene(agentScenes.first);

    expect(controller.practiceSessionId, _sessionId);
    expect(controller.practiceSessionId, isNot(threadId));
    expect(controller.turnLimit, 2);

    for (var expected = 1; expected <= 2; expected++) {
      await controller.startRecording();
      await controller.stopRecording();
      expect(controller.candidateId, 'candidate-$expected');
      await controller.confirmTranscript();
      expect(controller.completedTurns, expected);
    }

    expect(controller.review?.id, _reviewId);
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(practice.confirmedQuestionIds, ['question-1', 'question-2']);
    expect(recorder.discarded, 2);
  });

  test(
    'private-state cleanup clears Practice transport and temporary audio',
    () async {
      final practice = _TwoTurnPracticeClient();
      final recorder = _Recorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: recorder,
      );

      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      await controller.startRecording();
      await controller.clearPrivateState();

      expect(practice.cleanupCount, 1);
      expect(recorder.cleanupCount, 1);
      expect(controller.practiceSessionId, isNull);
      expect(controller.candidateId, isNull);
    },
  );

  test('same Session retains every server-issued recording handle', () async {
    final practice = _TwoTurnPracticeClient(includeAudioAssets: true);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
      mediaClient: _NoopMediaClient(),
      audioPlayer: _NoopAudioPlayer(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.selectScene(agentScenes.first);

    for (var turn = 0; turn < 2; turn++) {
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }

    expect(controller.recordings.map((recording) => recording.audioAssetId), [
      'audio-1',
      'audio-2',
    ]);
  });

  test('confirm retry reuses one Idempotency-Key', () async {
    final practice = _TwoTurnPracticeClient()..failConfirmOnce = true;
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
      clientIdFactory: (scope) => '$scope-stable',
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);
    await controller.startRecording();
    await controller.stopRecording();

    await controller.confirmTranscript();
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
    await controller.confirmTranscript();

    expect(practice.confirmationKeys, ['confirm-stable', 'confirm-stable']);
  });

  test(
    'pending question audio is fenced when confirmation advances the turn',
    () async {
      final practice = _TwoTurnPracticeClient();
      final pendingSpeech = Completer<Uint8List>();
      final media = _QuestionMediaClient(pendingSpeech: pendingSpeech);
      final player = _TrackingAudioPlayer();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
        mediaClient: media,
        audioPlayer: player,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      await controller.startRecording();
      await controller.stopRecording();

      final playback = controller.toggleQuestionAudio();
      await media.questionStarted.future;
      await controller.confirmTranscript();
      expect(controller.questionId, 'question-2');

      pendingSpeech.complete(_wave());
      await playback;

      expect(player.playCount, 0);
      expect(controller.isQuestionAudioLoading, isFalse);
      expect(controller.isQuestionAudioPlaying, isFalse);
    },
  );

  test(
    'playing question audio stops before confirmation changes question',
    () async {
      final practice = _TwoTurnPracticeClient();
      final media = _QuestionMediaClient();
      final player = _TrackingAudioPlayer();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
        mediaClient: media,
        audioPlayer: player,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      await controller.startRecording();
      await controller.stopRecording();
      await controller.toggleQuestionAudio();
      expect(controller.isQuestionAudioPlaying, isTrue);
      final stopsBeforeConfirmation = player.stopCount;

      await controller.confirmTranscript();

      expect(controller.questionId, 'question-2');
      expect(player.stopCount, greaterThan(stopsBeforeConfirmation));
      expect(controller.isQuestionAudioPlaying, isFalse);
    },
  );

  test(
    'non-retryable recording conflict directs the user to rerecord',
    () async {
      final practice = _TwoTurnPracticeClient()
        ..confirmFailure = const AgentClientException(
          kind: AgentClientFailureKind.conflict,
          errorCode: 'resource_conflict',
          retryable: false,
        );
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      await controller.startRecording();
      await controller.stopRecording();

      await controller.confirmTranscript();

      expect(controller.errorMessage, '录音已失效，请重新录音。');
      expect(
        controller.recordingState,
        PracticeRecordingState.awaitingConfirmation,
      );
      controller.rerecord();
      expect(controller.recordingState, PracticeRecordingState.idle);
    },
  );

  test(
    'retryReview returns to reviewFailed when restore has no state',
    () async {
      final practice = _TwoTurnPracticeClient()..omitReview = true;
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      for (var turn = 0; turn < 2; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }
      expect(controller.recordingState, PracticeRecordingState.reviewFailed);

      await controller.retryReview();

      expect(controller.recordingState, PracticeRecordingState.reviewFailed);
      expect(controller.errorMessage, '复盘仍在生成，请稍后重试。');
    },
  );

  test('logout waits for a late recorder start and then stops it', () async {
    final recorder = _ControlledStartRecorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: _TwoTurnPracticeClient(),
      recorder: recorder,
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);

    final start = controller.startRecording();
    expect(controller.recordingState, PracticeRecordingState.starting);
    await controller.stopRecording();
    expect(recorder.stopCount, 0);

    var cleanupFinished = false;
    final cleanup = controller.clearPrivateState().then((_) {
      cleanupFinished = true;
    });
    await Future<void>.delayed(Duration.zero);
    expect(cleanupFinished, isFalse);

    recorder.startCompleter.complete();
    await start;
    await cleanup;

    expect(recorder.recording, isFalse);
    expect(recorder.cleanupCount, 1);
  });

  test(
    'logout during native stop deletes account A audio without a B upload',
    () async {
      final practice = _TwoTurnPracticeClient();
      final recorder = _ControlledStopRecorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: recorder,
      );
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      await controller.startRecording();

      final stop = controller.stopRecording();
      await Future<void>.delayed(Duration.zero);
      final logout = controller.clearPrivateState();
      await Future<void>.delayed(Duration.zero);
      expect(practice.transcribeCount, 0);

      recorder.stopCompleter.complete();
      await stop;
      await logout;

      expect(practice.transcribeCount, 0);
      expect(recorder.discarded, 1);
      expect(recorder.cleanupCount, 1);

      // A later account may initialize only after A's stop chain is isolated.
      await controller.initialize();
      expect(practice.transcribeCount, 0);
    },
  );

  test('recording deadline stops before the server audio limit', () async {
    final practice = _TwoTurnPracticeClient();
    final deadlineCompleted = Completer<void>();
    final recorder = _Recorder(discardSignal: deadlineCompleted);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
      recordingLimit: const Duration(milliseconds: 5),
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);

    await controller.startRecording();
    expect(recorder.recording, isTrue);
    await deadlineCompleted.future.timeout(const Duration(seconds: 2));

    expect(recorder.recording, isFalse);
    expect(practice.transcribeCount, 1);
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
  });

  test(
    'same-Session Review retry does not downgrade known recording handles',
    () async {
      final practice = _TwoTurnPracticeClient(includeAudioAssets: true)
        ..omitReview = true;
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
        mediaClient: _NoopMediaClient(),
        audioPlayer: _NoopAudioPlayer(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      for (var turn = 0; turn < 2; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }
      practice.restoreResult = PracticeSessionSnapshot(
        sessionId: _sessionId,
        matter: AgentMatter(id: 'matter-1', scene: agentScenes.first),
        completedTurns: 2,
        turnLimit: 2,
        sessionCompleted: true,
        currentTurn: const PracticeTurnSnapshot(
          id: 'turn-2',
          sessionId: _sessionId,
          questionId: 'question-2',
          respondentParticipantId: 'participant-user',
          candidateId: 'candidate-2',
          answerText: 'Answer 2',
          evidenceVersion: 2,
          effectiveTurns: 2,
          sessionCompleted: true,
          reviewId: _reviewId,
          audioAssetId: 'audio-2',
        ),
        review: const AgentReview(
          id: _reviewId,
          title: 'Review',
          summary: 'Summary',
          strength: 'Strength',
          nextFocus: 'Next focus',
        ),
      );

      await controller.retryReview();

      expect(controller.recordings.map((recording) => recording.audioAssetId), [
        'audio-1',
        'audio-2',
      ]);
    },
  );

  test(
    'Review retry rejects a different Session without mixing recordings',
    () async {
      final practice = _TwoTurnPracticeClient(includeAudioAssets: true)
        ..omitReview = true;
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
        mediaClient: _NoopMediaClient(),
        audioPlayer: _NoopAudioPlayer(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await controller.selectScene(agentScenes.first);
      for (var turn = 0; turn < 2; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }
      practice.restoreResult = PracticeSessionSnapshot(
        sessionId: 'different-session',
        matter: AgentMatter(id: 'matter-foreign', scene: agentScenes.first),
        completedTurns: 1,
        turnLimit: 1,
        sessionCompleted: true,
        currentTurn: const PracticeTurnSnapshot(
          id: 'turn-foreign',
          sessionId: 'different-session',
          questionId: 'question-foreign',
          respondentParticipantId: 'participant-user',
          candidateId: 'candidate-foreign',
          answerText: 'Foreign answer',
          evidenceVersion: 1,
          effectiveTurns: 1,
          sessionCompleted: true,
          reviewId: 'review-foreign',
          audioAssetId: 'audio-foreign',
        ),
        review: const AgentReview(
          id: 'review-foreign',
          title: 'Foreign Review',
          summary: 'Foreign',
          strength: 'Foreign',
          nextFocus: 'Foreign',
        ),
      );

      await controller.retryReview();

      expect(controller.practiceSessionId, _sessionId);
      expect(controller.recordingState, PracticeRecordingState.reviewFailed);
      expect(controller.recordings.map((recording) => recording.audioAssetId), [
        'audio-1',
        'audio-2',
      ]);
    },
  );

  test('logout cancels the pending recording deadline', () async {
    Timer? deadline;
    await runZoned(
      () async {
        final controller = AgentController(
          client: FakeAgentClient(),
          practiceClient: _TwoTurnPracticeClient(),
          recorder: _Recorder(),
        );
        await controller.initialize();
        await controller.selectScene(agentScenes.first);
        await controller.startRecording();
        expect(deadline?.isActive, isTrue);

        await controller.clearPrivateState();

        expect(deadline?.isActive, isFalse);
      },
      zoneSpecification: ZoneSpecification(
        createTimer: (self, parent, zone, duration, callback) {
          final timer = parent.createTimer(zone, duration, callback);
          if (duration == const Duration(seconds: 58)) {
            deadline = timer;
          }
          return timer;
        },
      ),
    );
  });

  test('dispose cancels the pending recording deadline', () async {
    Timer? deadline;
    await runZoned(
      () async {
        final controller = AgentController(
          client: FakeAgentClient(),
          practiceClient: _TwoTurnPracticeClient(),
          recorder: _Recorder(),
        );
        await controller.initialize();
        await controller.selectScene(agentScenes.first);
        await controller.startRecording();
        expect(deadline?.isActive, isTrue);

        controller.dispose();

        expect(deadline?.isActive, isFalse);
      },
      zoneSpecification: ZoneSpecification(
        createTimer: (self, parent, zone, duration, callback) {
          final timer = parent.createTimer(zone, duration, callback);
          if (duration == const Duration(seconds: 58)) {
            deadline = timer;
          }
          return timer;
        },
      ),
    );
  });

  testWidgets('practice shows actionable iOS microphone permission guidance', (
    tester,
  ) async {
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: _TwoTurnPracticeClient(),
      recorder: _PermissionDeniedRecorder(),
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    await tester.tap(find.byKey(const Key('practice-record')));
    await tester.pump();

    expect(find.text('需要麦克风权限；请在 iOS“设置”中允许 SpeakUp 使用麦克风。'), findsOneWidget);
  });

  testWidgets('practice surfaces free voice quota without counting the turn', (
    tester,
  ) async {
    final practice = _TwoTurnPracticeClient()
      ..transcribeFailure = const AgentClientException(
        kind: AgentClientFailureKind.rateLimited,
        statusCode: 429,
        errorCode: 'quota_exhausted',
        retryable: false,
      );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    await tester.tap(find.byKey(const Key('practice-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('practice-stop-recording')));
    await tester.pumpAndSettle();

    expect(find.text('今日免费语音额度已用完，本轮未计入进度。'), findsOneWidget);
    expect(controller.completedTurns, 0);
    expect(controller.recordingState, PracticeRecordingState.idle);
  });

  testWidgets('practice keeps candidate text after a confirm network error', (
    tester,
  ) async {
    final practice = _TwoTurnPracticeClient()
      ..confirmFailure = const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    await tester.tap(find.byKey(const Key('practice-confirm-turn')));
    await tester.pumpAndSettle();

    expect(find.text('网络连接不稳定，这一轮尚未确认，请重试。'), findsOneWidget);
    expect(find.text('Answer 1'), findsOneWidget);
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
  });

  testWidgets('nullable Review offers retry and surfaces a network failure', (
    tester,
  ) async {
    final practice = _TwoTurnPracticeClient()..omitReview = true;
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    await controller.initialize();
    await controller.selectScene(agentScenes.first);
    for (var turn = 0; turn < 2; turn++) {
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }
    practice.restoreFailure = const AgentClientException(
      kind: AgentClientFailureKind.network,
      retryable: true,
    );
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    expect(find.byKey(const Key('practice-retry-review')), findsOneWidget);
    await tester.tap(find.byKey(const Key('practice-retry-review')));
    await tester.pumpAndSettle();

    expect(find.text('网络连接不稳定，暂时无法刷新复盘。'), findsOneWidget);
    expect(controller.review, isNull);
    expect(controller.recordingState, PracticeRecordingState.reviewFailed);
  });
}

final class _TwoTurnPracticeClient implements PracticeClient {
  _TwoTurnPracticeClient({this.includeAudioAssets = false});

  final bool includeAudioAssets;
  int completed = 0;
  int cleanupCount = 0;
  final List<String> confirmedQuestionIds = [];
  final List<String> confirmationKeys = [];
  bool failConfirmOnce = false;
  bool omitReview = false;
  Object? transcribeFailure;
  Object? confirmFailure;
  Object? restoreFailure;
  PracticeSessionSnapshot? restoreResult;
  int transcribeCount = 0;

  @override
  Future<void> clearAccountState() async {
    cleanupCount++;
    completed = 0;
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    if (restoreFailure case final failure?) {
      throw failure;
    }
    return restoreResult;
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    return PracticeStartResult(
      snapshot: PracticeSessionSnapshot(
        sessionId: _sessionId,
        matter: activeMatter,
        completedTurns: 0,
        turnLimit: 2,
        sessionCompleted: false,
        currentQuestion: const PracticeQuestion(
          id: 'question-1',
          sessionId: _sessionId,
          text: 'First question',
          speechPath: '/v1/voice-questions/question-1/speech',
        ),
      ),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    transcribeCount++;
    if (transcribeFailure case final failure?) {
      throw failure;
    }
    return TranscriptionCandidate(
      id: 'candidate-${completed + 1}',
      sessionId: request.sessionId,
      questionId: request.questionId,
      text: 'Answer ${completed + 1}',
    );
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    confirmationKeys.add(idempotencyKey);
    if (confirmFailure case final failure?) {
      throw failure;
    }
    if (failConfirmOnce) {
      failConfirmOnce = false;
      throw StateError('ambiguous confirmation');
    }
    confirmedQuestionIds.add(questionId);
    completed++;
    final done = completed == 2;
    final nextQuestion = done
        ? null
        : const PracticeQuestion(
            id: 'question-2',
            sessionId: _sessionId,
            text: 'Second question',
            speechPath: '/v1/voice-questions/question-2/speech',
          );
    return PracticeTurnConfirmation(
      turnId: 'turn-$completed',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      answer: AgentMessage(
        id: 'answer-$completed',
        role: AgentMessageRole.user,
        text: 'Answer $completed',
      ),
      completedTurns: completed,
      turnLimit: 2,
      sessionCompleted: done,
      nextQuestion: nextQuestion,
      review: done && !omitReview
          ? const AgentReview(
              id: _reviewId,
              title: 'Review',
              summary: 'Summary',
              strength: 'Strength',
              nextFocus: 'Next focus',
            )
          : null,
      audioAssetId: includeAudioAssets ? 'audio-$completed' : null,
    );
  }
}

final class _NoopMediaClient implements PracticeMediaClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> deleteRecording(String audioAssetId) async {}

  @override
  Future<void> dispose() async {}

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) {
    throw UnimplementedError();
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) {
    throw UnimplementedError();
  }
}

final class _NoopAudioPlayer implements PracticeAudioPlayer {
  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}

  @override
  Future<void> playWav(Uint8List bytes) async {}

  @override
  Future<void> stop() async {}
}

final class _QuestionMediaClient implements PracticeMediaClient {
  _QuestionMediaClient({this.pendingSpeech});

  final Completer<Uint8List>? pendingSpeech;
  final Completer<void> questionStarted = Completer<void>();

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async {
    if (!questionStarted.isCompleted) {
      questionStarted.complete();
    }
    return pendingSpeech == null ? _wave() : await pendingSpeech!.future;
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) {
    throw UnimplementedError();
  }

  @override
  Future<void> deleteRecording(String audioAssetId) async {}

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

final class _TrackingAudioPlayer implements PracticeAudioPlayer {
  final StreamController<void> _completions =
      StreamController<void>.broadcast();
  int playCount = 0;
  int stopCount = 0;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playWav(Uint8List bytes) async {
    playCount++;
  }

  @override
  Future<void> stop() async {
    stopCount++;
  }

  @override
  Future<void> clearAccountState() async {
    stopCount++;
  }

  @override
  Future<void> dispose() => _completions.close();
}

final class _PermissionDeniedRecorder implements PracticeRecorder {
  @override
  Future<void> start() {
    throw const PracticeRecordingException(
      PracticeRecordingFailureKind.permissionDenied,
    );
  }

  @override
  Future<RecordedPracticeAudio> stop() {
    throw const PracticeRecordingException(
      PracticeRecordingFailureKind.notRecording,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> clearAccountState() async {}
}

final class _ControlledStopRecorder implements PracticeRecorder {
  final Completer<void> stopCompleter = Completer<void>();
  int discarded = 0;
  int cleanupCount = 0;

  @override
  Future<void> start() async {}

  @override
  Future<RecordedPracticeAudio> stop() async {
    await stopCompleter.future;
    return const RecordedPracticeAudio(
      path: 'account-a.wav',
      contentType: 'audio/wav',
      sizeBytes: 100,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {
    discarded++;
  }

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> clearAccountState() async {
    cleanupCount++;
  }
}

final class _ControlledStartRecorder implements PracticeRecorder {
  final Completer<void> startCompleter = Completer<void>();
  bool recording = false;
  int stopCount = 0;
  int cleanupCount = 0;

  @override
  Future<void> start() async {
    await startCompleter.future;
    recording = true;
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    stopCount++;
    recording = false;
    return const RecordedPracticeAudio(
      path: 'controlled.wav',
      contentType: 'audio/wav',
      sizeBytes: 100,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> discardCurrent() async {
    recording = false;
  }

  @override
  Future<void> clearAccountState() async {
    cleanupCount++;
    recording = false;
  }
}

final class _Recorder implements PracticeRecorder {
  _Recorder({this.discardSignal});

  final Completer<void>? discardSignal;
  int discarded = 0;
  int cleanupCount = 0;
  bool recording = false;

  @override
  Future<void> start() async {
    recording = true;
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    recording = false;
    return const RecordedPracticeAudio(
      path: 'controlled.wav',
      contentType: 'audio/wav',
      sizeBytes: 100,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {
    discarded++;
    final signal = discardSignal;
    if (signal != null && !signal.isCompleted) {
      signal.complete();
    }
  }

  @override
  Future<void> discardCurrent() async {
    recording = false;
  }

  @override
  Future<void> clearAccountState() async {
    cleanupCount++;
    recording = false;
  }
}

const _sessionId = 'practice-session-server';
const _reviewId = 'review-server';

Uint8List _wave() {
  final bytes = Uint8List(44);
  bytes.setAll(0, const [0x52, 0x49, 0x46, 0x46]);
  bytes.setAll(8, const [0x57, 0x41, 0x56, 0x45]);
  return bytes;
}
