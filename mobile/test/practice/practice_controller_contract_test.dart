import '../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

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
import 'package:speakup/review/formal_review.dart';

void main() {
  test('consumes server turnLimit and Review from confirmation', () async {
    final formalReview = _provisionalFormalReview();
    final practice = _TwoTurnPracticeClient(formalReview: formalReview);
    final recorder = _Recorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-id',
    );

    await controller.initialize();
    final threadId = controller.threadId;
    await _activatePractice(controller, testScenes.first);

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
    expect(controller.formalReview, same(formalReview));
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(practice.confirmedQuestionIds, ['question-1', 'question-2']);
    expect(recorder.discarded, 2);
  });

  test('reconciles an ambiguous final interview voice confirmation', () async {
    final practice = _TwoTurnPracticeClient(
      sceneFamily: SceneFamily.interview,
      sceneModel: SceneModel.interviewBasicDialogue,
    );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    await controller.confirmTranscript();

    const answer = 'Answer 2';
    practice
      ..confirmFailure = const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      )
      ..restoreResult = _completedInterviewSnapshot(
        questionId: 'question-2',
        candidateId: 'candidate-2',
        answer: answer,
      );
    await controller.startRecording();
    await controller.stopRecording();
    expect(controller.isFinalInterviewSubmission, isFalse);
    final restoreCount = practice.restoreCount;

    final confirmation = controller.confirmTranscript();
    expect(controller.isFinalInterviewSubmission, isTrue);
    await confirmation;

    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(controller.completedTurns, 2);
    expect(controller.errorMessage, isNull);
    expect(practice.restoreCount, restoreCount + 1);
  });

  test('reconciles an ambiguous final interview text submission', () async {
    final practice = _TwoTurnPracticeClient(
      sceneFamily: SceneFamily.interview,
      sceneModel: SceneModel.interviewBasicDialogue,
    );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    await controller.confirmTranscript();

    const answer = 'Typed final answer';
    practice
      ..textFailure = const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      )
      ..restoreResult = _completedInterviewSnapshot(
        questionId: 'question-2',
        candidateId: 'server-text-candidate',
        answer: answer,
      );
    final restoreCount = practice.restoreCount;

    final submission = controller.submitPracticeText(answer);
    expect(controller.isFinalInterviewSubmission, isTrue);
    expect(await submission, isTrue);
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(controller.completedTurns, 2);
    expect(controller.errorMessage, isNull);
    expect(practice.restoreCount, restoreCount + 1);
  });

  test('does not accept a mismatched final interview recovery', () async {
    final practice = _TwoTurnPracticeClient(
      sceneFamily: SceneFamily.interview,
      sceneModel: SceneModel.interviewBasicDialogue,
    );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    await controller.confirmTranscript();

    practice
      ..confirmFailure = const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      )
      ..restoreResult = _completedInterviewSnapshot(
        questionId: 'question-2',
        candidateId: 'different-candidate',
        answer: 'Answer 2',
      );
    await controller.startRecording();
    await controller.stopRecording();
    await controller.confirmTranscript();

    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
    expect(controller.completedTurns, 1);
    expect(controller.candidateId, 'candidate-2');
    expect(controller.errorMessage, '网络连接不稳定，这一轮尚未确认，请重试。');
  });

  test(
    'does not apply final recovery after private state is cleared',
    () async {
      final practice = _TwoTurnPracticeClient(
        sceneFamily: SceneFamily.interview,
        sceneModel: SceneModel.interviewBasicDialogue,
      );
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, testScenes.first);
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
      await controller.startRecording();
      await controller.stopRecording();
      final restoreGate = Completer<PracticeSessionSnapshot>();
      practice
        ..confirmFailure = const AgentClientException(
          kind: AgentClientFailureKind.network,
          retryable: true,
        )
        ..restoreGate = restoreGate;
      final confirmation = controller.confirmTranscript();
      await Future<void>.delayed(Duration.zero);

      await controller.clearPrivateState();
      restoreGate.complete(
        _completedInterviewSnapshot(
          questionId: 'question-2',
          candidateId: 'candidate-2',
          answer: 'Answer 2',
        ),
      );
      await confirmation;

      expect(controller.practiceSessionId, isNull);
      expect(controller.recordingState, PracticeRecordingState.idle);
      expect(controller.completedTurns, 0);
    },
  );

  test(
    'retains failed transcription audio and retries with one Turn identity',
    () async {
      final practice = _TwoTurnPracticeClient()
        ..transcribeFailure = const AgentClientException(
          kind: AgentClientFailureKind.network,
          retryable: true,
        );
      final recorder = _Recorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: recorder,
        clientIdFactory: (scope) => '$scope-stable',
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, testScenes.first);

      await controller.startRecording();
      await controller.stopRecording();

      expect(controller.recordingState, PracticeRecordingState.idle);
      expect(controller.hasPendingPracticeAudio, isTrue);
      expect(recorder.discarded, 0);
      expect(practice.transcriptionClientTurnIds, <String>['turn-stable']);

      await controller.startRecording();
      expect(recorder.recording, isFalse);

      practice.transcribeFailure = null;
      await controller.retryPracticeTranscription();

      expect(
        controller.recordingState,
        PracticeRecordingState.awaitingConfirmation,
      );
      expect(controller.hasPendingPracticeAudio, isFalse);
      expect(recorder.discarded, 1);
      expect(practice.transcriptionClientTurnIds, <String>[
        'turn-stable',
        'turn-stable',
      ]);
    },
  );

  test('explicitly deletes retained transcription audio', () async {
    final practice = _TwoTurnPracticeClient()
      ..transcribeFailure = StateError('transcription failed');
    final recorder = _Recorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();

    await controller.discardPendingPracticeAudio();

    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(recorder.discarded, 1);
    expect(controller.errorMessage, isNull);

    await controller.startRecording();
    expect(recorder.recording, isTrue);
    await controller.cancelRecording();
  });

  test(
    'failed local deletion keeps the retained recording addressable',
    () async {
      final practice = _TwoTurnPracticeClient()
        ..transcribeFailure = StateError('transcription failed');
      final recorder = _Recorder()
        ..discardFailure = StateError('local delete failed');
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: recorder,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, testScenes.first);
      await controller.startRecording();
      await controller.stopRecording();

      await controller.discardPendingPracticeAudio();

      expect(controller.hasPendingPracticeAudio, isTrue);
      expect(controller.errorMessage, '暂时无法删除本地录音，请重试。');

      recorder.discardFailure = null;
      await controller.discardPendingPracticeAudio();
      expect(controller.hasPendingPracticeAudio, isFalse);
    },
  );

  test('retained transcription audio blocks Thread replacement', () async {
    final practice = _TwoTurnPracticeClient()
      ..transcribeFailure = StateError('transcription failed');
    final recorder = _Recorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    final threadId = controller.threadId;

    final changed = await controller.createThread();

    expect(changed, isFalse);
    expect(controller.threadId, threadId);
    expect(controller.hasPendingPracticeAudio, isTrue);
    expect(recorder.discarded, 0);
  });

  test(
    'retained transcription audio blocks leaving without deleting it',
    () async {
      final practice = _TwoTurnPracticeClient()
        ..transcribeFailure = StateError('transcription failed');
      final recorder = _Recorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: recorder,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, testScenes.first);
      await controller.startRecording();
      await controller.stopRecording();

      expect(await controller.prepareToLeavePractice(), isFalse);
      expect(controller.hasPendingPracticeAudio, isTrue);
      expect(recorder.discarded, 0);
    },
  );

  test('account cleanup removes retained transcription audio state', () async {
    final practice = _TwoTurnPracticeClient()
      ..transcribeFailure = StateError('transcription failed');
    final recorder = _Recorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
    );
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    expect(controller.hasPendingPracticeAudio, isTrue);

    await controller.clearPrivateState();

    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(recorder.cleanupCount, 1);
    controller.dispose();
  });

  test('dispose deletes retained transcription audio', () async {
    final practice = _TwoTurnPracticeClient()
      ..transcribeFailure = StateError('transcription failed');
    final discarded = Completer<void>();
    final recorder = _Recorder(discardSignal: discarded);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
    );
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();

    controller.dispose();
    await discarded.future.timeout(const Duration(seconds: 2));

    expect(recorder.discarded, 1);
  });

  test(
    'dispose retries account cleanup after an in-flight delete fails',
    () async {
      final practice = _TwoTurnPracticeClient()
        ..transcribeFailure = StateError('transcription failed');
      final discardGate = Completer<void>();
      final cleanupSignal = Completer<void>();
      final recorder = _Recorder(
        discardGate: discardGate,
        cleanupSignal: cleanupSignal,
      )..discardFailure = StateError('first delete failed');
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: recorder,
      );
      await controller.initialize();
      await _activatePractice(controller, testScenes.first);
      await controller.startRecording();
      await controller.stopRecording();

      unawaited(controller.discardPendingPracticeAudio());
      await Future<void>.delayed(Duration.zero);
      controller.dispose();
      discardGate.complete();
      await cleanupSignal.future.timeout(const Duration(seconds: 2));

      expect(recorder.cleanupCount, 1);
    },
  );

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
      await _activatePractice(controller, testScenes.first);
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
    await _activatePractice(controller, testScenes.first);

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
    await _activatePractice(controller, testScenes.first);
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
      await _activatePractice(controller, testScenes.first);
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
      await _activatePractice(controller, testScenes.first);
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
      await _activatePractice(controller, testScenes.first);
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
      await _activatePractice(controller, _synchronousReviewScene);
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
    await _activatePractice(controller, testScenes.first);

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

  test('finishing a starting recording waits and stops exactly once', () async {
    final recorder = _ControlledStartRecorder();
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: _TwoTurnPracticeClient(),
      recorder: recorder,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);

    final start = controller.startRecording();
    final finish = controller.finishRecordingGesture();
    await Future<void>.delayed(Duration.zero);
    expect(controller.recordingState, PracticeRecordingState.starting);
    expect(recorder.stopCount, 0);

    recorder.startCompleter.complete();
    await start;
    await finish;

    expect(recorder.stopCount, 1);
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
  });

  testWidgets(
    'tap recording ignores duplicate finish on a narrow large-text screen',
    (tester) async {
      tester.view.physicalSize = const Size(320, 780);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final recorder = _ControlledStartRecorder();
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: _TwoTurnPracticeClient(),
        recorder: recorder,
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, testScenes.first);

      await tester.pumpWidget(
        MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(2)),
          child: MaterialApp(home: PracticePage(agentController: controller)),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('practice-record')));
      await tester.pump();
      expect(controller.recordingState, PracticeRecordingState.starting);
      expect(
        find.byKey(const Key('practice-voice-target-cancel')),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);

      final stop = find.byKey(const Key('practice-stop-recording'));
      await tester.tap(stop);
      await tester.tap(stop);
      recorder.startCompleter.complete();
      await tester.pumpAndSettle();

      expect(recorder.stopCount, 1);
      expect(controller.completedTurns, 1);
      expect(find.byKey(const Key('practice-transcript')), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );

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
      await _activatePractice(controller, testScenes.first);
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
    await _activatePractice(controller, testScenes.first);

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
      await _activatePractice(controller, _synchronousReviewScene);
      for (var turn = 0; turn < 2; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }
      practice.restoreResult = PracticeSessionSnapshot(
        sessionId: _sessionId,
        planId: _planId,
        sceneFamily: _synchronousReviewScene.family,
        sceneModel: _synchronousReviewScene.model,
        sessionVersion: 3,
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
      await _activatePractice(controller, _synchronousReviewScene);
      for (var turn = 0; turn < 2; turn++) {
        await controller.startRecording();
        await controller.stopRecording();
        await controller.confirmTranscript();
      }
      practice.restoreResult = PracticeSessionSnapshot(
        sessionId: 'different-session',
        planId: 'different-plan',
        sceneFamily: _synchronousReviewScene.family,
        sceneModel: _synchronousReviewScene.model,
        sessionVersion: 2,
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
        await _activatePractice(controller, testScenes.first);
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
        await _activatePractice(controller, testScenes.first);
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
    await _activatePractice(controller, testScenes.first);
    expect(controller.hasActivePractice, isTrue);
    expect(controller.errorMessage, isNull);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    await controller.startRecording();
    await tester.pumpAndSettle();

    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(controller.errorMessage, '需要麦克风权限；请在 iOS“设置”中允许 SpeakUp 使用麦克风。');
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
    await _activatePractice(controller, testScenes.first);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    await controller.startRecording();
    await tester.pump();
    await controller.stopRecording();
    await tester.pumpAndSettle();

    expect(find.text('今日免费语音额度已用完，录音已保留，本轮未计入进度。'), findsOneWidget);
    expect(
      find.byKey(const Key('practice-retry-transcription')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('practice-delete-pending-audio')),
      findsOneWidget,
    );
    expect(controller.completedTurns, 0);
    expect(controller.recordingState, PracticeRecordingState.idle);
  });

  testWidgets('practice renders the first question as a conversation bubble', (
    tester,
  ) async {
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: _ThreeTurnPracticeClient(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes[1]);

    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );
    await tester.pumpAndSettle();

    expect(find.text(testScenes[1].name), findsOneWidget);
    expect(
      find.byKey(const Key('practice-ai-message-question-0-1')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('practice-message-list')), findsOneWidget);
    expect(find.byKey(const Key('practice-turn-progress')), findsNothing);
    expect(find.byKey(const Key('practice-turn-count')), findsNothing);
    expect(find.byKey(const Key('practice-current-question')), findsNothing);
    expect(find.textContaining('第 1 题'), findsNothing);
    expect(find.textContaining('共 3 题'), findsNothing);

    final bubble = tester.getRect(
      find.byKey(const Key('practice-ai-message-question-0-1')),
    );
    expect(bubble.center.dx, lessThan(tester.view.physicalSize.width / 2));
  });

  testWidgets('practice submits a typed English answer and advances one turn', (
    tester,
  ) async {
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: _ThreeTurnPracticeClient(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    const answer =
        'I led the rollout, communicated the risk, and delivered it safely.';
    await tester.tap(find.byKey(const Key('practice-open-keyboard')));
    await tester.pump();
    expect(find.byKey(const Key('practice-page')), findsOneWidget);
    expect(
      Navigator.of(
        tester.element(find.byKey(const Key('practice-page'))),
      ).canPop(),
      isFalse,
    );
    expect(find.byKey(const Key('practice-return-to-voice')), findsOneWidget);
    final input = find.byKey(const Key('practice-text-answer'));
    await tester.scrollUntilVisible(
      input,
      120,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.enterText(input, answer);
    await tester.tap(find.byKey(const Key('practice-submit-text')));
    await tester.pumpAndSettle();

    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(
      controller.messages
          .lastWhere((message) => message.role == AgentMessageRole.user)
          .text,
      answer,
    );
    expect(
      find.byKey(const Key('practice-ai-message-question-0-1')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('practice-user-message-answer-1')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('practice-ai-message-question-0-2')),
      findsOneWidget,
    );
    expect(find.text(answer), findsOneWidget);

    final firstQuestion = tester.getTopLeft(
      find.byKey(const Key('practice-ai-message-question-0-1')),
    );
    final userAnswer = tester.getTopLeft(
      find.byKey(const Key('practice-user-message-answer-1')),
    );
    final secondQuestion = tester.getTopLeft(
      find.byKey(const Key('practice-ai-message-question-0-2')),
    );
    expect(firstQuestion.dy, lessThan(userAnswer.dy));
    expect(userAnswer.dy, lessThan(secondQuestion.dy));

    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-user-message-answer-1')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('practice-open-keyboard')));
    await tester.pump();
    expect(tester.widget<TextField>(input).controller?.text, isEmpty);
  });

  testWidgets('practice keeps a stable five-message conversation', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: _ThreeTurnPracticeClient(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes[1]);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );
    await tester.pumpAndSettle();

    for (final answer in ['First answer', 'Second answer']) {
      await tester.tap(find.byKey(const Key('practice-open-keyboard')));
      await tester.pump();
      await tester.enterText(
        find.byKey(const Key('practice-text-answer')),
        answer,
      );
      await tester.tap(find.byKey(const Key('practice-submit-text')));
      await tester.pumpAndSettle();
    }

    const orderedKeys = [
      'practice-ai-message-question-0-1',
      'practice-user-message-answer-1',
      'practice-ai-message-question-0-2',
      'practice-user-message-answer-2',
      'practice-ai-message-question-0-3',
    ];
    final offsets = <double>[];
    for (final key in orderedKeys) {
      final finder = find.byKey(Key(key));
      expect(finder, findsOneWidget);
      offsets.add(tester.getTopLeft(finder).dy);
    }
    expect(offsets, orderedEquals(offsets.toList()..sort()));

    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );
    await tester.pumpAndSettle();
    for (final key in orderedKeys) {
      expect(find.byKey(Key(key)), findsOneWidget);
    }

    final scrollable = tester.state<ScrollableState>(
      find.descendant(
        of: find.byKey(const Key('practice-message-list')),
        matching: find.byType(Scrollable),
      ),
    );
    expect(scrollable.position.maxScrollExtent, greaterThan(0));
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
    await _activatePractice(controller, testScenes.first);
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
      find.byKey(const Key('practice-user-message-answer-1')),
      findsNothing,
    );
    expect(controller.practiceMessages, hasLength(1));
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );

    practice.confirmFailure = null;
    await tester.tap(find.byKey(const Key('practice-confirm-turn')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('practice-user-message-answer-1')),
      findsOneWidget,
    );
    expect(controller.practiceMessages, hasLength(3));
  });

  testWidgets('practice does not append a failed typed answer', (tester) async {
    final practice = _TwoTurnPracticeClient()
      ..textFailure = const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await tester.pumpWidget(
      MaterialApp(home: PracticePage(agentController: controller)),
    );

    await tester.tap(find.byKey(const Key('practice-open-keyboard')));
    await tester.pump();
    await tester.enterText(
      find.byKey(const Key('practice-text-answer')),
      'This answer must not appear as confirmed.',
    );
    await tester.tap(find.byKey(const Key('practice-submit-text')));
    await tester.pumpAndSettle();

    expect(find.text('网络连接不稳定，这一轮尚未确认，请重试。'), findsOneWidget);
    expect(controller.practiceMessages, hasLength(1));
    expect(
      find.byKey(const Key('practice-user-message-answer-1')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('practice-ai-message-question-1')),
      findsOneWidget,
    );
  });

  testWidgets('interview and daily scenes share the conversation page', (
    tester,
  ) async {
    final interviewScene = testScene(
      id: 'behavioral-interview',
      family: SceneFamily.interview,
      model: SceneModel.interviewBasicDialogue,
      name: '行为面试',
      prompt: const ScenePrompt(
        publicSceneBrief: '使用具体经历回答协作、冲突和成长类问题。',
        practiceGoal: 'Complete the behavioral interview.',
        userRole: 'Candidate',
        aiRole: 'Interviewer',
        personaSummary: 'Professional and focused.',
        focusAreas: <String>['clarity'],
        turnBlueprints: <String>['Ask one behavioral question.'],
        suggestedDurationSeconds: 600,
      ),
    );
    final dailyScene = testScene(
      id: 'hotel-check-in',
      family: SceneFamily.daily,
      model: SceneModel.hotelCheckinAndIssueHandling,
      name: '酒店入住',
      prompt: const ScenePrompt(
        publicSceneBrief: '练习办理入住和沟通房间问题。',
        practiceGoal: 'Complete the hotel check-in conversation.',
        userRole: 'Guest',
        aiRole: 'Receptionist',
        personaSummary: 'Professional and helpful.',
        focusAreas: <String>['check_in'],
        turnBlueprints: <String>['Confirm the booking.'],
        suggestedDurationSeconds: 600,
      ),
    );

    for (final scene in [interviewScene, dailyScene]) {
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: _ThreeTurnPracticeClient(),
      );
      await controller.initialize();
      await _activatePractice(controller, scene);
      await tester.pumpWidget(
        MaterialApp(home: PracticePage(agentController: controller)),
      );
      await tester.pumpAndSettle();

      expect(find.text(scene.name), findsOneWidget);
      expect(find.byKey(const Key('practice-message-list')), findsOneWidget);
      expect(
        find.byKey(const Key('practice-ai-message-question-0-1')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('practice-record')), findsOneWidget);
      await tester.tap(find.byKey(const Key('practice-open-keyboard')));
      await tester.pump();
      expect(find.byKey(const Key('practice-text-answer')), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
    }
  });

  testWidgets('long conversation wraps and scrolls on a narrow large-text view', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final practice = _TwoTurnPracticeClient(
      firstQuestion: List.filled(
        12,
        'Please explain the context, your decision, and the measurable result.',
      ).join(' '),
      firstAnswer: List.filled(
        12,
        'I aligned the team, reduced the risk, and measured the outcome.',
      ).join(' '),
      secondQuestion: List.filled(
        10,
        'What trade-off would you revisit and why?',
      ).join(' '),
    );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, testScenes.first);
    await controller.startRecording();
    await controller.stopRecording();
    await controller.confirmTranscript();

    await tester.pumpWidget(
      MediaQuery(
        data: const MediaQueryData(textScaler: TextScaler.linear(2)),
        child: MaterialApp(home: PracticePage(agentController: controller)),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(
      find.byKey(const Key('practice-ai-message-question-1')),
      findsOneWidget,
    );
    final scrollable = tester.state<ScrollableState>(
      find.descendant(
        of: find.byKey(const Key('practice-message-list')),
        matching: find.byType(Scrollable),
      ),
    );
    expect(scrollable.position.maxScrollExtent, greaterThan(0));
    await tester.drag(
      find.byKey(const Key('practice-message-list')),
      const Offset(0, 240),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-user-message-answer-1')),
      findsOneWidget,
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
    await _activatePractice(controller, _synchronousReviewScene);
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

final _synchronousReviewScene = testScene(
  id: 'workplace-review-retry',
  family: SceneFamily.workplace,
  model: SceneModel.workplaceBasicDialogue,
  name: 'Workplace update',
);

Future<void> _activatePractice(
  AgentController controller,
  SceneDefinition scene,
) async {
  final practice = controller.practiceClient;
  late final String planId;
  late final int turnLimit;
  if (practice case final _TwoTurnPracticeClient client) {
    client.bindScene(scene);
    planId = _planId;
    turnLimit = 2;
  } else if (practice case final _ThreeTurnPracticeClient client) {
    client.bindScene(scene);
    planId = _threeTurnPlanId;
    turnLimit = 3;
  } else {
    throw StateError('Unsupported explicit Practice test client.');
  }

  await controller.selectScene(scene);
  await controller.activateCreatedPractice(
    threadId: controller.threadId!,
    goalId: controller.activeGoal!.id,
    scene: scene,
    sessionId: _sessionId,
    planId: planId,
    turnLimit: turnLimit,
    clientOperationId: 'activate-$_sessionId',
  );
}

final class _TwoTurnPracticeClient implements PracticeClient {
  _TwoTurnPracticeClient({
    this.includeAudioAssets = false,
    this.firstQuestion = 'First question',
    this.firstAnswer = 'Answer 1',
    this.secondQuestion = 'Second question',
    this.formalReview,
    this.sceneFamily,
    this.sceneModel,
  });

  final bool includeAudioAssets;
  final String firstQuestion;
  final String firstAnswer;
  final String secondQuestion;
  final FormalReview? formalReview;
  final SceneFamily? sceneFamily;
  final SceneModel? sceneModel;
  int completed = 0;
  int cleanupCount = 0;
  final List<String> confirmedQuestionIds = [];
  final List<String> confirmationKeys = [];
  bool failConfirmOnce = false;
  bool omitReview = false;
  Object? transcribeFailure;
  Object? confirmFailure;
  Object? textFailure;
  Object? restoreFailure;
  PracticeSessionSnapshot? restoreResult;
  Completer<PracticeSessionSnapshot>? restoreGate;
  int transcribeCount = 0;
  int restoreCount = 0;
  SceneDefinition? activeScene;
  PracticeSessionSnapshot? _snapshot;
  final List<String> transcriptionClientTurnIds = <String>[];

  void bindScene(SceneDefinition scene) {
    activeScene = scene;
  }

  @override
  Future<void> clearAccountState() async {
    cleanupCount++;
    completed = 0;
    activeScene = null;
    _snapshot = null;
    restoreResult = null;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    restoreCount++;
    if (sessionId != _sessionId) {
      throw StateError('Unknown Practice Session.');
    }
    if (restoreFailure case final failure?) {
      throw failure;
    }
    if (restoreGate case final gate?) {
      return gate.future;
    }
    final restored = restoreResult ?? _snapshot;
    if (restored == null) {
      throw StateError('Practice Session has not been activated.');
    }
    return restored;
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final scene = activeScene;
    if (sessionId != _sessionId ||
        clientOperationId.trim().isEmpty ||
        scene == null) {
      throw StateError('Invalid explicit Practice activation.');
    }
    final snapshot = PracticeSessionSnapshot(
      sessionId: _sessionId,
      planId: _planId,
      sceneFamily: sceneFamily ?? scene.family,
      sceneModel: sceneModel ?? scene.model,
      sessionVersion: 1,
      completedTurns: 0,
      turnLimit: 2,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-1',
        sessionId: _sessionId,
        text: firstQuestion,
        speechPath: '/v1/voice-questions/question-1/speech',
      ),
    );
    _snapshot = snapshot;
    return snapshot;
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    transcribeCount++;
    transcriptionClientTurnIds.add(request.clientTurnId);
    if (transcribeFailure case final failure?) {
      throw failure;
    }
    return TranscriptionCandidate(
      id: 'candidate-${completed + 1}',
      sessionId: request.sessionId,
      questionId: request.questionId,
      text: completed == 0 ? firstAnswer : 'Answer ${completed + 1}',
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
        : PracticeQuestion(
            id: 'question-2',
            sessionId: _sessionId,
            text: secondQuestion,
            speechPath: '/v1/voice-questions/question-2/speech',
          );
    final answerText = completed == 1 ? firstAnswer : 'Answer $completed';
    final review = done && !omitReview
        ? const AgentReview(
            id: _reviewId,
            title: 'Review',
            summary: 'Summary',
            strength: 'Strength',
            nextFocus: 'Next focus',
          )
        : null;
    final resolvedSceneFamily = sceneFamily ?? activeScene?.family;
    final resolvedSceneModel = sceneModel ?? activeScene?.model;
    if (resolvedSceneFamily == null || resolvedSceneModel == null) {
      throw StateError('Practice Scene identity is not bound.');
    }
    final audioAssetId = includeAudioAssets ? 'audio-$completed' : null;
    final turn = PracticeTurnSnapshot(
      id: 'turn-$completed',
      sessionId: sessionId,
      questionId: questionId,
      respondentParticipantId: 'participant-user',
      candidateId: candidateId,
      answerText: answerText,
      evidenceVersion: completed,
      effectiveTurns: completed,
      sessionCompleted: done,
      reviewId: review?.id,
      audioAssetId: audioAssetId,
    );
    _snapshot = PracticeSessionSnapshot(
      sessionId: sessionId,
      planId: _planId,
      sceneFamily: resolvedSceneFamily,
      sceneModel: resolvedSceneModel,
      sessionVersion: completed + 1,
      completedTurns: completed,
      turnLimit: 2,
      sessionCompleted: done,
      currentQuestion: nextQuestion,
      currentTurn: turn,
      review: review,
      formalReview: done && !omitReview ? formalReview : null,
    );
    return PracticeTurnConfirmation(
      turnId: 'turn-$completed',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      sceneFamily: resolvedSceneFamily,
      sceneModel: resolvedSceneModel,
      sessionVersion: completed + 1,
      answer: AgentMessage(
        id: 'answer-$completed',
        role: AgentMessageRole.user,
        text: answerText,
      ),
      completedTurns: completed,
      turnLimit: 2,
      sessionCompleted: done,
      nextQuestion: nextQuestion,
      review: review,
      formalReview: done && !omitReview ? formalReview : null,
      audioAssetId: audioAssetId,
    );
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async {
    if (textFailure case final failure?) {
      throw failure;
    }
    throw UnimplementedError();
  }
}

PracticeSessionSnapshot _completedInterviewSnapshot({
  required String questionId,
  required String candidateId,
  required String answer,
}) {
  return PracticeSessionSnapshot(
    sessionId: _sessionId,
    planId: _planId,
    sceneFamily: SceneFamily.interview,
    sceneModel: SceneModel.interviewBasicDialogue,
    sessionVersion: 3,
    completedTurns: 2,
    turnLimit: 2,
    sessionCompleted: true,
    currentTurn: PracticeTurnSnapshot(
      id: 'turn-2',
      sessionId: _sessionId,
      questionId: questionId,
      respondentParticipantId: 'respondent-1',
      candidateId: candidateId,
      answerText: answer,
      evidenceVersion: 1,
      effectiveTurns: 2,
      sessionCompleted: true,
    ),
  );
}

final class _ThreeTurnPracticeClient implements PracticeClient {
  final FakePracticeClient _delegate = FakePracticeClient();
  SceneDefinition? _scene;

  void bindScene(SceneDefinition scene) {
    _scene = scene;
  }

  @override
  Future<void> clearAccountState() async {
    _scene = null;
    await _delegate.clearAccountState();
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async =>
      _withBoundScene(await _delegate.restorePractice(sessionId: sessionId));

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async => _withBoundScene(
    await _delegate.activatePractice(
      sessionId: sessionId,
      clientOperationId: clientOperationId,
    ),
  );

  PracticeSessionSnapshot _withBoundScene(PracticeSessionSnapshot snapshot) {
    final scene = _scene;
    if (scene == null) {
      throw StateError('Practice Scene identity is not bound.');
    }
    return PracticeSessionSnapshot(
      sessionId: snapshot.sessionId,
      planId: snapshot.planId,
      sceneFamily: scene.family,
      sceneModel: scene.model,
      sessionVersion: snapshot.sessionVersion,
      completedTurns: snapshot.completedTurns,
      turnLimit: snapshot.turnLimit,
      sessionCompleted: snapshot.sessionCompleted,
      currentQuestion: snapshot.currentQuestion,
      currentTurn: snapshot.currentTurn,
      turnHistory: snapshot.turnHistory,
      review: snapshot.review,
      formalReview: snapshot.formalReview,
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) => _delegate.transcribe(request);

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async => _withBoundSceneConfirmation(
    await _delegate.confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      idempotencyKey: idempotencyKey,
    ),
  );

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async => _withBoundSceneConfirmation(
    await _delegate.submitText(
      sessionId: sessionId,
      questionId: questionId,
      answerText: answerText,
      idempotencyKey: idempotencyKey,
    ),
  );

  PracticeTurnConfirmation _withBoundSceneConfirmation(
    PracticeTurnConfirmation confirmation,
  ) {
    final scene = _scene;
    if (scene == null) {
      throw StateError('Practice Scene identity is not bound.');
    }
    return PracticeTurnConfirmation(
      turnId: confirmation.turnId,
      sessionId: confirmation.sessionId,
      questionId: confirmation.questionId,
      candidateId: confirmation.candidateId,
      answer: confirmation.answer,
      completedTurns: confirmation.completedTurns,
      turnLimit: confirmation.turnLimit,
      sessionCompleted: confirmation.sessionCompleted,
      sceneFamily: scene.family,
      sceneModel: scene.model,
      sessionVersion: confirmation.sessionVersion,
      nextQuestion: confirmation.nextQuestion,
      review: confirmation.review,
      formalReview: confirmation.formalReview,
      audioAssetId: confirmation.audioAssetId,
      speechFeedbackStatusUrl: confirmation.speechFeedbackStatusUrl,
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
  _Recorder({this.discardSignal, this.discardGate, this.cleanupSignal});

  final Completer<void>? discardSignal;
  final Completer<void>? discardGate;
  final Completer<void>? cleanupSignal;
  Object? discardFailure;
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
    await discardGate?.future;
    if (discardFailure case final failure?) {
      throw failure;
    }
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
    final signal = cleanupSignal;
    if (signal != null && !signal.isCompleted) {
      signal.complete();
    }
  }
}

const _sessionId = 'practice-session-server';
const _planId = 'practice-plan-server';
const _threeTurnPlanId = 'practice-plan-practice-session-server';
const _reviewId = 'review-server';

FormalReview _provisionalFormalReview() {
  final createdAt = DateTime.utc(2026, 7, 30, 3);
  return FormalReview(
    id: _reviewId,
    practiceSessionId: _sessionId,
    status: FormalReviewStatus.completed,
    schema: FormalReviewSchema.sceneV2,
    implementationVersion: 'qianwen-scene-review-v2',
    sourceTurnId: 'turn-2',
    sourceTurnVersion: 'conversation-turn:evidence-v2',
    contextType: FormalReviewContextType.interviewProjectDeepDive,
    result: const FormalReviewResult(
      eligibility: FormalReviewSummaryEligibility.provisional,
      summary: '本次仅根据文本给出暂定反馈。',
      dimensions: <FormalReviewDimension>[
        FormalReviewDimension(
          key: 'relevance_structure',
          category: 'relevance_structure',
          score: 76,
          message: '回答与问题相关。',
        ),
      ],
      feedbackItems: <FormalReviewFeedbackItem>[],
      repracticeSuggestionRefs: <String>[],
      insufficientEvidenceReasons: <String>[
        'pronunciation_audio_evidence_unavailable',
      ],
    ),
    createdAt: createdAt,
    updatedAt: createdAt,
    completedAt: createdAt,
  );
}

Uint8List _wave() {
  final bytes = Uint8List(44);
  bytes.setAll(0, const [0x52, 0x49, 0x46, 0x46]);
  bytes.setAll(8, const [0x57, 0x41, 0x56, 0x45]);
  return bytes;
}
