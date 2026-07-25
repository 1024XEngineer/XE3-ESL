import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_client.dart';
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
    final recorder = _Recorder();
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
    await Future<void>.delayed(const Duration(milliseconds: 50));

    expect(recorder.recording, isFalse);
    expect(practice.transcribeCount, 1);
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
  });

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
}

final class _TwoTurnPracticeClient implements PracticeClient {
  int completed = 0;
  int cleanupCount = 0;
  final List<String> confirmedQuestionIds = [];
  final List<String> confirmationKeys = [];
  bool failConfirmOnce = false;
  bool omitReview = false;
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
  }) async => null;

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
        ),
      ),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    transcribeCount++;
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
    );
  }
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
