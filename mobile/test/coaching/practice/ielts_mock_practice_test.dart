import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';

import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_practice.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/interview/interview_practice.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_progress_store.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';

void main() {
  testWidgets('Part 1 keeps Tips visible while recording', (tester) async {
    const capabilities = PracticeCapabilities(
      retryAllowed: false,
      questionTranslationAllowed: false,
      questionTipsAllowed: true,
      avatarAllowed: false,
      speechFeedbackAllowed: false,
    );
    final practice = _IeltsPracticeClient(
      initialCompleted: 0,
      capabilities: capabilities,
    );
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(
      controller,
      practice,
      _ieltsScene,
      mode: PracticeMode.part1,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(
      find.byKey(const ValueKey('ielts-question-tip-question-1')),
    );
    await tester.pump();
    expect(find.byKey(const Key('practice-question-tip-card')), findsOneWidget);
    expect(find.text('可边看边说'), findsNothing);

    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();

    expect(controller.recordingState, PracticeRecordingState.recording);
    expect(find.byKey(const Key('practice-question-tip-card')), findsOneWidget);
    await controller.cancelRecording();
    await tester.pump();
  });

  testWidgets(
    'Part 1 uses the shared avatar, conversation, and composer stage',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 0);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: _MemoryProgressStore(),
          ),
        ),
      );
      await tester.pump();

      expect(find.byKey(const Key('ielts-avatar-region')), findsOneWidget);
      expect(find.byKey(const Key('ielts-avatar-placeholder')), findsOneWidget);
      expect(find.byKey(const Key('ielts-mock-conversation')), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-mock-record')).hitTestable(),
        findsOneWidget,
      );
    },
  );

  testWidgets('Part 1 prefers the shared practice question voice', (
    tester,
  ) async {
    final speaker = _ImmediateExaminerSpeaker();
    final media = _QuestionMediaClient();
    final player = _QuestionAudioPlayer();
    final practice = _IeltsPracticeClient(initialCompleted: 0);
    final controller = PracticeController(
      client: practice,
      mediaClient: media,
      audioPlayer: player,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: speaker,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(media.loadedPaths, ['speech/question-1.wav']);
    expect(player.playCount, 1);
    expect(speaker.spoken, isEmpty);
  });

  testWidgets(
    'Part 1 auto-plays and keeps examiner text visible in shared bubbles',
    (tester) async {
      final speaker = _ImmediateExaminerSpeaker();
      final practice = _IeltsPracticeClient(initialCompleted: 0);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: _MemoryProgressStore(),
            examinerSpeaker: speaker,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(speaker.spoken, [_question(1).text]);
      expect(
        find.byKey(const Key('ielts-question-voice-question-1')),
        findsOneWidget,
      );
      expect(find.text(_question(1).text), findsOneWidget);

      await tester.tap(find.byKey(const Key('ielts-mock-record')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-record')));
      await tester.pump();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));
      await tester.pump();

      expect(speaker.spoken.last, _question(2).text);
      expect(find.text(_question(2).text), findsOneWidget);
    },
  );

  testWidgets('Part 3 starts with an auto-playing visible text bubble', (
    tester,
  ) async {
    final speaker = _ImmediateExaminerSpeaker();
    final media = _QuestionMediaClient();
    final player = _QuestionAudioPlayer();
    final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 1);
    final controller = PracticeController(
      client: practice,
      mediaClient: media,
      audioPlayer: player,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(
      controller,
      practice,
      _ieltsScene,
      mode: PracticeMode.part3,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: speaker,
        ),
      ),
    );
    await tester.pump();
    expect(speaker.spoken, isEmpty);

    await tester.tap(find.byKey(const Key('ielts-part3-start')));
    await tester.pump();
    await tester.pump();

    expect(speaker.spoken, [_question(1).text]);
    expect(media.loadedPaths, isEmpty);
    expect(player.playCount, 0);
    expect(
      find.byKey(const Key('ielts-question-voice-question-1')),
      findsOneWidget,
    );
    expect(find.text(_question(1).text), findsOneWidget);
  });

  testWidgets(
    'Part 2 reads instructions and Cue Card before starting 60 seconds',
    (tester) async {
      var now = DateTime.utc(2026, 8, 3, 4, 30);
      final speaker = _ControlledExaminerSpeaker();
      final practice = _IeltsPracticeClient(initialCompleted: 8);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore();
      addTearDown(controller.dispose);
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: store,
            examinerSpeaker: speaker,
            now: () => now,
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.text('进入 Part 2'));
      await tester.pump();
      expect(speaker.spoken.single, contains('one minute to prepare'));
      expect(find.text('Examiner is speaking…'), findsOneWidget);
      expect(store.value?.preparationDeadline, isNull);

      speaker.completeCurrent();
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-2-cue-card-reading')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('ielts-mock-preparation-countdown')),
        findsNothing,
      );
      expect(speaker.spoken.last, _question(9).text);
      expect(store.value?.phase, IeltsMockPhase.part2CueCard);
      expect(store.value?.preparationDeadline, isNull);

      speaker.completeCurrent();
      await tester.pump();
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-2-long-turn')),
        findsOneWidget,
      );
      expect(find.text('1:00'), findsOneWidget);
      expect(find.byKey(const Key('ielts-mock-cue-card')), findsOneWidget);
      expect(
        store.value?.preparationDeadline,
        now.add(const Duration(seconds: 60)),
      );

      now = now.add(const Duration(seconds: 61));
      await tester.pump(const Duration(seconds: 1));
      await tester.pump();

      expect(store.value?.phase, IeltsMockPhase.part2Speaking);
      expect(controller.recordingState, PracticeRecordingState.recording);
      expect(find.byKey(const Key('ielts-mock-cue-card')), findsNothing);
      expect(find.textContaining('录音中·'), findsOneWidget);
      expect(find.byKey(const Key('ielts-mock-finish-speaking')), findsNothing);
    },
  );

  testWidgets(
    'Part 2 section hides the Cue Card after formal recording starts',
    (tester) async {
      final now = DateTime.utc(2026, 8, 4, 8);
      final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 6);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore(
        IeltsMockProgress(
          sessionId: _sessionId,
          phase: IeltsMockPhase.part2Preparation,
          startedAt: now,
          preparationDeadline: now.add(const Duration(seconds: 60)),
        ),
      );
      addTearDown(controller.dispose);
      await _activatePractice(
        controller,
        practice,
        _ieltsScene,
        mode: PracticeMode.part2,
      );

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: store,
            examinerSpeaker: _ImmediateExaminerSpeaker(),
            now: () => now,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(find.byKey(const Key('ielts-mock-cue-card')), findsOneWidget);
      expect(find.text('1:00'), findsOneWidget);

      await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
      await tester.pump();

      expect(controller.recordingState, PracticeRecordingState.recording);
      expect(find.byKey(const Key('ielts-mock-cue-card')), findsNothing);
      expect(
        find.byKey(const Key('ielts-part2-recording-status')),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'Part 1 boundary enters prep, keeps notes, and submits the Part 2 long turn',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 8);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore();
      addTearDown(controller.dispose);
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: store,
            examinerSpeaker: _ImmediateExaminerSpeaker(),
          ),
        ),
      );
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-1-complete')),
        findsOneWidget,
      );
      await tester.tap(find.text('进入 Part 2'));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-2-intro')), findsOneWidget);
      expect(find.byKey(const Key('ielts-mock-cue-card')), findsNothing);
      expect(
        tester
            .widget<Text>(
              find.text(
                'You will have 1 minute to prepare and up to 2 minutes to speak. You may take notes during preparation.',
              ),
            )
            .textAlign,
        TextAlign.center,
      );

      await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
      await tester.pump();
      expect(
        find.byKey(const Key('ielts-mock-part-2-long-turn')),
        findsOneWidget,
      );
      await tester.enterText(
        find.byKey(const Key('ielts-mock-notes')),
        'online course, weekly practice, useful at work',
      );
      await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-2-long-turn')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('ielts-mock-part-2-speaking')), findsNothing);
      expect(controller.recordingState, PracticeRecordingState.recording);
      expect(
        find.byKey(const Key('ielts-part2-recording-status')),
        findsOneWidget,
      );
      expect(find.textContaining('录音中·'), findsOneWidget);
      expect(
        find.text('online course, weekly practice, useful at work'),
        findsOneWidget,
      );

      await tester.pump(const Duration(seconds: 120));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));

      expect(controller.completedTurns, 9);
      expect(practice.confirmedQuestionIds, ['question-9']);
      expect(
        find.byKey(const Key('ielts-mock-part-2-transition')),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('ielts-part2-continue-part3')));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-part2-answer-feedback')),
        findsNothing,
      );
      expect(find.text('Answer 9'), findsNothing);
      expect(store.value?.notes, contains('weekly practice'));
      expect(find.text('Part 3 · Discussion'), findsOneWidget);
    },
  );

  testWidgets(
    'failed Part 2 transcription retries the same audio in background',
    (tester) async {
      final practice = _IeltsPracticeClient(
        initialCompleted: 8,
        transcriptionFailuresRemaining: 1,
      );
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore();
      addTearDown(controller.dispose);
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: store,
            examinerSpeaker: _ImmediateExaminerSpeaker(),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.text('进入 Part 2'));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
      await tester.pump();
      await controller.finishRecordingGesture();
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));

      expect(controller.completedTurns, 9);
      expect(
        find.byKey(const Key('ielts-mock-part-2-transition')),
        findsOneWidget,
      );
      expect(controller.hasPendingPracticeAudio, isFalse);
      await tester.tap(find.byKey(const Key('ielts-part2-continue-part3')));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
    },
  );

  testWidgets('Part 2 waits for confirmation before offering Part 3', (
    tester,
  ) async {
    final transcriptionGate = Completer<void>();
    final practice = _IeltsPracticeClient(initialCompleted: 8)
      ..transcriptionGate = transcriptionGate;
    final recorder = _Recorder();
    final controller = PracticeController(client: practice, recorder: recorder);
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.text('进入 Part 2'));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
    await tester.pump();

    await tester.pump(const Duration(seconds: 120));
    await tester.pump();

    expect(controller.completedTurns, 8);
    expect(controller.recordingState, PracticeRecordingState.transcribing);
    expect(find.byKey(const Key('ielts-mock-part-2-transition')), findsNothing);
    expect(
      find.byKey(const Key('ielts-mock-part-2-long-turn')),
      findsOneWidget,
    );
    expect(find.text('正在识别你的作答…'), findsOneWidget);
    expect(recorder.startCalls, 1);

    transcriptionGate.complete();
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    expect(practice.transcribedQuestionIds, ['question-9']);
    expect(controller.completedTurns, 9);
    expect(practice.confirmedQuestionIds, ['question-9']);
    expect(
      find.byKey(const Key('ielts-mock-part-2-transition')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('ielts-part2-continue-part3')));
    await tester.pump();

    expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
    expect(find.byKey(const Key('ielts-mock-record')), findsOneWidget);
  });

  testWidgets('Part 2 recovers immediately from an early realtime failure', (
    tester,
  ) async {
    final realtimeFailure = Completer<void>();
    final practice = _IeltsPracticeClient(initialCompleted: 8)
      ..firstRealtimeFailure = realtimeFailure;
    final recorder = _RestartableStreamingRecorder();
    final controller = PracticeController(client: practice, recorder: recorder);
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.text('进入 Part 2'));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
    await tester.pump();

    expect(controller.recordingState, PracticeRecordingState.recording);
    recorder.add(Uint8List.fromList(<int>[1, 2]));
    await practice.firstRealtimeUpdate.future;
    await tester.runAsync(() async {
      realtimeFailure.complete();
      await practice.firstRealtimeFailureObserved.future;
      await recorder.firstDiscardFinished.future;
    });
    await tester.pump();

    expect(
      controller.recordingState,
      PracticeRecordingState.idle,
      reason:
          'error=${controller.errorMessage}, discards=${recorder.discardCurrentCalls}, realtime=${practice.realtimeTranscriptions}',
    );
    expect(controller.errorMessage, contains('实时识别已中断'));
    expect(recorder.discardCurrentCalls, 1);
    expect(find.byKey(const Key('ielts-mock-part-2-transition')), findsNothing);
    expect(find.byKey(const Key('ielts-mock-finish-speaking')), findsOneWidget);

    await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
    await tester.pump();
    expect(controller.recordingState, PracticeRecordingState.recording);
    expect(recorder.streamingStarts, 2);
    expect(controller.completedTurns, 8);
    expect(find.byKey(const Key('ielts-mock-part-2-transition')), findsNothing);

    recorder.add(Uint8List.fromList(<int>[3, 4]));
    final stopping = controller.finishRecordingGesture();
    await stopping;
    for (
      var attempt = 0;
      attempt < 10 && controller.completedTurns == 8;
      attempt++
    ) {
      await tester.pump(const Duration(milliseconds: 10));
    }
    await tester.pump(const Duration(milliseconds: 220));

    expect(controller.completedTurns, 9);
    expect(
      find.byKey(const Key('ielts-mock-part-2-transition')),
      findsOneWidget,
    );
  });

  testWidgets('Chinese-only voice answer stays on the current IELTS question', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(
      initialCompleted: 0,
      transcriptionText: '我今天没有什么想说的',
    );
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    expect(controller.completedTurns, 0);
    expect(practice.confirmedQuestionIds, isEmpty);
    expect(
      find.byKey(const Key('ielts-answer-language-error')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('ielts-mock-record')), findsOneWidget);
  });

  testWidgets('mixed Chinese and English answer is submitted and scored', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(
      initialCompleted: 0,
      transcriptionText: '我 usually spend time reading books',
    );
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    // This test exercises the language gate. Complete the ordinary transcript
    // confirmation explicitly so recurring IELTS timers do not make
    // pumpAndSettle wait forever.
    for (
      var attempt = 0;
      attempt < 10 && controller.completedTurns == 0;
      attempt++
    ) {
      if (controller.recordingState ==
          PracticeRecordingState.awaitingConfirmation) {
        await controller.confirmTranscript();
      }
      await tester.pump(const Duration(milliseconds: 20));
    }

    expect(controller.completedTurns, 1);
    expect(practice.confirmedQuestionIds, ['question-1']);
    expect(find.byKey(const Key('ielts-answer-language-error')), findsNothing);
  });

  testWidgets('Part 2 exhausts retries then starts a clean re-recording', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(
      initialCompleted: 8,
      transcriptionFailuresRemaining: 4,
    );
    final recorder = _Recorder();
    final controller = PracticeController(client: practice, recorder: recorder);
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.text('进入 Part 2'));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
    await tester.pump();

    expect(recorder.startCalls, 1);
    await tester.pump(const Duration(seconds: 120));
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 2));
    await tester.pump(const Duration(seconds: 3));
    await tester.pump();

    expect(controller.completedTurns, 8);
    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(find.byKey(const Key('ielts-mock-part-2-transition')), findsNothing);
    expect(
      find.byKey(const Key('ielts-mock-part-2-long-turn')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('ielts-mock-finish-speaking')), findsOneWidget);
    expect(controller.hasPendingPracticeAudio, isTrue);
    expect(recorder.startCalls, 1);

    await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
    await tester.pump();

    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(controller.recordingState, PracticeRecordingState.recording);
    expect(recorder.startCalls, 2);
  });

  testWidgets('restores an unexpired preparation checkpoint and notes', (
    tester,
  ) async {
    final now = DateTime.utc(2026, 7, 29, 8);
    final practice = _IeltsPracticeClient(initialCompleted: 8);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);
    final store = _MemoryProgressStore(
      IeltsMockProgress(
        sessionId: _sessionId,
        phase: IeltsMockPhase.part2Preparation,
        startedAt: now.subtract(const Duration(minutes: 5)),
        preparationDeadline: now.add(const Duration(seconds: 33)),
        notes: 'restored note',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: store,
          now: () => now,
        ),
      ),
    );
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-mock-part-2-long-turn')),
      findsOneWidget,
    );
    expect(find.text('0:33'), findsOneWidget);
    expect(find.text('restored note'), findsOneWidget);
  });

  testWidgets('Part 2 transcription failure stays open for re-recording', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 8)
      ..transcribeFailure = StateError('transcription failed');
    final recorder = _Recorder();
    final controller = PracticeController(client: practice, recorder: recorder);
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-continue')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
    await tester.pump();
    await controller.finishRecordingGesture();
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 2));
    await tester.pump(const Duration(seconds: 3));
    await tester.pump();

    expect(find.byKey(const Key('ielts-mock-part-2-transition')), findsNothing);
    expect(controller.hasPendingPracticeAudio, isTrue);
    expect(controller.completedTurns, 8);
    expect(find.byKey(const Key('ielts-mock-finish-speaking')), findsOneWidget);

    await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
    await tester.pump();

    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(controller.recordingState, PracticeRecordingState.recording);
    expect(recorder.startCalls, 2);
  });

  testWidgets('Part 1 clears failed transcription without a recovery dock', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 0)
      ..transcribeFailure = StateError('transcription failed');
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.pump();
    await tester.pump();

    expect(find.byKey(const Key('ielts-mock-pending-audio')), findsNothing);
    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(find.byKey(const Key('ielts-mock-record')), findsOneWidget);
  });

  testWidgets('disposing Part 2 cancels recording without an exit callback', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 8);
    final recorder = _Recorder();
    final controller = PracticeController(client: practice, recorder: recorder);
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-continue')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
    await tester.pump();
    expect(recorder.recording, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();

    expect(recorder.recording, isFalse);
    expect(controller.recordingState, PracticeRecordingState.idle);
  });

  testWidgets('Part 1 supports upward cancel without auto-submit', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 0);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('ielts-mock-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await tester.pump(const Duration(seconds: 1));
    expect(
      tester.getSize(find.byKey(const Key('ielts-mock-stop-recording'))).height,
      48,
    );
    expect(
      tester
          .widget<Text>(
            find.byKey(const Key('ielts-mock-voice-target-duration')),
          )
          .data,
      '0:01',
    );
    expect(find.byKey(const Key('ielts-mock-voice-targets')), findsNothing);
    await gesture.moveBy(const Offset(0, -90));
    await tester.pump();
    expect(find.text('松开取消'), findsOneWidget);
    await gesture.up();
    await tester.pump();
    await tester.pump();

    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(controller.completedTurns, 0);
    expect(practice.confirmedQuestionIds, isEmpty);
  });

  testWidgets('completed full mock remains on completion instead of report', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 14);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 250));

    expect(find.byKey(const Key('ielts-mock-complete')), findsOneWidget);
    expect(find.text('模考已完成'), findsOneWidget);
    expect(find.textContaining('OVERALL BAND'), findsNothing);
    expect(find.byKey(const Key('practice-page')), findsNothing);
  });

  testWidgets(
    'completed full mock exits before parked context can show the empty practice page',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 14);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const Key('open-completed-mock'),
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => IeltsSpeakingMockPage(
                      controller: controller,
                      progressStore: _MemoryProgressStore(),
                      onExitRequested: () async => true,
                    ),
                  ),
                ),
                child: const Text('Open completed mock'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.byKey(const Key('open-completed-mock')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('ielts-mock-complete')), findsOneWidget);
      await tester.tap(find.byKey(const Key('ielts-mock-back-to-training')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('open-completed-mock')), findsOneWidget);
      expect(find.byKey(const Key('practice-page')), findsNothing);
      expect(find.byKey(const Key('ielts-mock-page')), findsNothing);
    },
  );

  testWidgets('clearing practice state fences the bound report poll', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 14);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    final reportClient = _PendingReportClient();
    final reportController = IeltsSpeakingReportController(
      client: reportClient,
    );
    addTearDown(controller.dispose);
    addTearDown(reportController.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          completedReportBuilder: (_, practiceSessionId) =>
              IeltsSpeakingSessionReportPanel(
                practiceSessionId: practiceSessionId,
                controller: reportController,
              ),
        ),
      ),
    );
    await tester.pump();
    await reportClient.started.future;
    expect(reportController.practiceSessionId, _sessionId);

    await controller.clearPrivateState();
    await tester.pump();

    expect(controller.practiceSessionId, isNull);
    expect(reportController.practiceSessionId, isNull);
    expect(reportController.isLoading, isFalse);
  });

  testWidgets(
    'Part 1 completion keeps the final answer visible before section review',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 4);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      final preparation = IeltsPreparationController(
        client: _UnusedQuestionBankClient(),
      );
      final reportClient = _PendingReportClient();
      final reportController = IeltsSpeakingReportController(
        client: reportClient,
      );
      addTearDown(controller.dispose);
      addTearDown(preparation.dispose);
      addTearDown(reportController.dispose);
      await _activatePractice(
        controller,
        practice,
        _ieltsScene,
        mode: PracticeMode.part1,
      );
      expect(controller.errorMessage, isNull);
      expect(controller.practiceSessionId, _sessionId);
      await preparation.beginSession(
        _sessionId,
        PracticeMode.part1,
        const IeltsPracticeSelection(part1SetId: 'p1-set-02'),
      );

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: _MemoryProgressStore(),
            ieltsController: preparation,
            examinerSpeaker: _ImmediateExaminerSpeaker(),
            completedReportBuilder: (_, practiceSessionId) =>
                IeltsSpeakingSessionReportPanel(
                  practiceSessionId: practiceSessionId,
                  controller: reportController,
                ),
          ),
        ),
      );
      await tester.pump();

      for (var turn = 0; turn < 4; turn++) {
        await _answerCurrentShortQuestion(tester, controller);
      }

      expect(controller.completedTurns, 4);
      final conversation = tester
          .widget<ListView>(find.byKey(const Key('ielts-mock-conversation')))
          .controller!;
      expect(
        conversation.position.pixels,
        closeTo(conversation.position.maxScrollExtent, 0.5),
      );
      expect(find.text('Answer 4'), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-section-completion-sheet')),
        findsOneWidget,
      );
      expect(find.text('Part 1 已完成'), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-section-practice-complete-part1')),
        findsNothing,
      );
      expect(reportClient.started.isCompleted, isFalse);
      expect(reportController.practiceSessionId, isNull);

      await tester.tap(find.byKey(const Key('ielts-section-review-action')));
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-section-completion-sheet')),
        findsNothing,
      );
      expect(find.byKey(const Key('ielts-mock-conversation')), findsOneWidget);
      expect(find.text('Answer 4'), findsOneWidget);
      expect(reportClient.started.isCompleted, isFalse);
      expect(reportController.practiceSessionId, isNull);
    },
  );

  testWidgets('Part 1 completion returns to the section list', (tester) async {
    final practice = _IeltsPracticeClient(initialCompleted: 3, turnLimit: 4);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    final preparation = IeltsPreparationController(
      client: _UnusedQuestionBankClient(),
    );
    addTearDown(controller.dispose);
    addTearDown(preparation.dispose);
    await _activatePractice(
      controller,
      practice,
      _ieltsScene,
      mode: PracticeMode.part1,
    );
    await preparation.beginSession(
      _sessionId,
      PracticeMode.part1,
      const IeltsPracticeSelection(part1SetId: 'p1-set-02'),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              key: const Key('open-section-completion'),
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<IeltsPracticeRouteResult>(
                  builder: (_) => IeltsSpeakingMockPage(
                    controller: controller,
                    progressStore: _MemoryProgressStore(),
                    ieltsController: preparation,
                    examinerSpeaker: _ImmediateExaminerSpeaker(),
                    onExitRequested: () async => true,
                  ),
                ),
              ),
              child: const Text('Open section completion'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('open-section-completion')));
    await tester.pumpAndSettle();
    await _answerCurrentShortQuestion(tester, controller);

    await tester.tap(find.byKey(const Key('ielts-section-list-action')));
    await tester.pumpAndSettle();

    final request = preparation.takeNavigationRequest();
    expect(request?.mode, PracticeMode.part1);
    expect(request?.selection, isNull);
    expect(find.byKey(const Key('open-section-completion')), findsOneWidget);
  });

  testWidgets('the full-mock PracticeOption opens the three-part flow', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 8);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const Key('ielts-mock-part-1-complete')), findsOneWidget);
    expect(find.byKey(const Key('practice-page')), findsNothing);
  });

  testWidgets(
    'same title and fourteen-turn limit do not impersonate the full mock',
    (tester) async {
      final sameTitleScene = _sceneFixture(
        id: 'scn_same_title_general_exam',
        name: 'IELTS 口语完整模拟',
        brief: '同名但不是完整模考',
        experience: PracticeExperience.lifeAndTravel,
        category: SceneCategory.lifeDaily,
      );
      final practice = _IeltsPracticeClient(initialCompleted: 8);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await _activatePractice(
        controller,
        practice,
        sameTitleScene,
        mode: PracticeMode.fullSimulation,
      );

      await tester.pumpWidget(
        MaterialApp(
          home: InterviewPracticePage(practiceController: controller),
        ),
      );
      await tester.pump();

      expect(controller.turnLimit, 14);
      expect(find.byKey(const Key('practice-page')), findsOneWidget);
      expect(find.byKey(const Key('ielts-mock-page')), findsNothing);
    },
  );

  testWidgets('save and exit returns from an in-progress full mock', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 0);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);
    var parkCalls = 0;

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              key: const Key('open-mock'),
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => IeltsSpeakingMockPage(
                    controller: controller,
                    progressStore: _MemoryProgressStore(),
                    onExitRequested: () async {
                      parkCalls++;
                      return true;
                    },
                  ),
                ),
              ),
              child: const Text('Open mock'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('open-mock')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('ielts-mock-exit')));
    await tester.pumpAndSettle();
    expect(find.text('Exit mock test?'), findsOneWidget);
    await tester.tap(find.text('Save & exit'));
    await tester.pumpAndSettle();

    expect(parkCalls, 1);
    expect(find.byKey(const Key('open-mock')), findsOneWidget);
    expect(find.byKey(const Key('ielts-mock-page')), findsNothing);
  });

  testWidgets('save and exit returns section practice to its set list', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 8);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    final preparation = IeltsPreparationController(
      client: _UnusedQuestionBankClient(),
    );
    addTearDown(controller.dispose);
    addTearDown(preparation.dispose);
    await _activatePractice(
      controller,
      practice,
      _ieltsScene,
      mode: PracticeMode.part1,
    );
    await preparation.beginSession(
      _sessionId,
      PracticeMode.part1,
      const IeltsPracticeSelection(part1SetId: 'p1-set-02'),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              key: const Key('open-section'),
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => IeltsSpeakingMockPage(
                    controller: controller,
                    progressStore: _MemoryProgressStore(),
                    ieltsController: preparation,
                    onExitRequested: () async => true,
                  ),
                ),
              ),
              child: const Text('Open section'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('open-section')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('ielts-mock-exit')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Save & exit'));
    await tester.pumpAndSettle();

    final request = preparation.takeNavigationRequest();
    expect(request?.mode, PracticeMode.part1);
    expect(request?.selection, isNull);
    expect(find.byKey(const Key('open-section')), findsOneWidget);
  });

  testWidgets(
    'restored Part 2 completion asks before entering its bound Part 3',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 1, turnLimit: 6);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await _activatePractice(
        controller,
        practice,
        _ieltsScene,
        mode: PracticeMode.part2,
      );

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: _MemoryProgressStore(),
            examinerSpeaker: _ImmediateExaminerSpeaker(),
          ),
        ),
      );
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-2-transition')),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('ielts-part2-continue-part3')));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
      expect(find.text('Part 3 · Discussion'), findsOneWidget);
      expect(find.text('继续对应 Part 3'), findsNothing);
    },
  );

  testWidgets(
    'one-question Part 3 shows its final answer before section review',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 1);
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await _activatePractice(
        controller,
        practice,
        _ieltsScene,
        mode: PracticeMode.part3,
      );

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            controller: controller,
            progressStore: _MemoryProgressStore(),
            examinerSpeaker: _ImmediateExaminerSpeaker(),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.byKey(const Key('ielts-part3-start')));
      await tester.pump();
      expect(find.text('0/1'), findsOneWidget);

      await _answerCurrentShortQuestion(tester, controller);

      expect(controller.completedTurns, 1);
      expect(find.text('Answer 1'), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-section-completion-sheet')),
        findsOneWidget,
      );
      expect(find.text('Part 3 已完成'), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-section-practice-complete-part3')),
        findsNothing,
      );

      await tester.tap(find.byKey(const Key('ielts-section-review-action')));
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-section-completion-sheet')),
        findsNothing,
      );
      expect(find.byKey(const Key('ielts-mock-conversation')), findsOneWidget);
      expect(find.text('Answer 1'), findsOneWidget);
    },
  );

  testWidgets('full mock completes after a single original Part 3 question', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 9, turnLimit: 10);
    final controller = PracticeController(
      client: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          examinerSpeaker: _ImmediateExaminerSpeaker(),
        ),
      ),
    );
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-mock-part-2-transition')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('ielts-part2-continue-part3')));
    await tester.pumpAndSettle();
    expect(find.text('0/1'), findsOneWidget);
    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(controller.hasPendingPracticeAudio, isFalse);

    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    for (
      var attempt = 0;
      attempt < 10 &&
          controller.recordingState != PracticeRecordingState.recording;
      attempt++
    ) {
      await tester.pump(const Duration(milliseconds: 20));
    }
    expect(controller.recordingState, PracticeRecordingState.recording);
    await controller.finishRecordingGesture();
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    for (
      var attempt = 0;
      attempt < 10 && controller.completedTurns == 9;
      attempt++
    ) {
      if (controller.recordingState ==
          PracticeRecordingState.awaitingConfirmation) {
        await controller.confirmTranscript();
      }
      await tester.pump(const Duration(milliseconds: 20));
    }

    expect(controller.completedTurns, 10);
    expect(find.text('Answer 10'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-section-completion-sheet')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('ielts-mock-complete')), findsNothing);

    await tester.tap(find.byKey(const Key('ielts-section-review-action')));
    await tester.pump(const Duration(milliseconds: 220));

    expect(find.byKey(const Key('ielts-mock-complete')), findsOneWidget);
    expect(find.text('1 题'), findsOneWidget);
  });

  testWidgets('section PracticeOption modes open the matching IELTS flow', (
    tester,
  ) async {
    for (final testCase in <({PracticeMode mode, int turnLimit, Key expected})>[
      (
        mode: PracticeMode.part1,
        turnLimit: 8,
        expected: const Key('ielts-mock-part-1'),
      ),
      (
        mode: PracticeMode.part2,
        turnLimit: 6,
        expected: const Key('ielts-mock-part-2-intro'),
      ),
      (
        mode: PracticeMode.part3,
        turnLimit: 5,
        expected: const Key('ielts-part3-topic-intro'),
      ),
    ]) {
      final practice = _IeltsPracticeClient(
        initialCompleted: 0,
        turnLimit: testCase.turnLimit,
      );
      final controller = PracticeController(
        client: practice,
        recorder: _Recorder(),
      );
      await _activatePractice(
        controller,
        practice,
        _ieltsScene,
        mode: testCase.mode,
      );

      await tester.pumpWidget(
        MaterialApp(
          home: IeltsSpeakingMockPage(
            key: ValueKey(testCase.mode),
            controller: controller,
            progressStore: _MemoryProgressStore(),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(testCase.expected), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      controller.dispose();
    }
  });
}

Future<void> _activatePractice(
  PracticeController controller,
  _IeltsPracticeClient practice,
  SceneDefinition scene, {
  PracticeMode mode = PracticeMode.fullMock,
}) async {
  practice.activeScene = scene;
  practice.activeMode = mode;
  await controller.activateCreatedPractice(
    scene: scene,
    sessionId: _sessionId,
    planId: _planId,
    practiceMode: mode,
    turnLimit: practice.turnLimit,
    clientOperationId: 'activate-${scene.id}',
  );
}

Future<void> _answerCurrentShortQuestion(
  WidgetTester tester,
  PracticeController controller,
) async {
  final completedTurns = controller.completedTurns;
  await tester.tap(find.byKey(const Key('ielts-mock-record')));
  for (
    var attempt = 0;
    attempt < 20 &&
        controller.recordingState != PracticeRecordingState.recording;
    attempt++
  ) {
    await tester.pump(const Duration(milliseconds: 20));
  }
  expect(controller.recordingState, PracticeRecordingState.recording);
  await tester.tap(find.byKey(const Key('ielts-mock-record')));
  for (
    var attempt = 0;
    attempt < 20 && controller.completedTurns == completedTurns;
    attempt++
  ) {
    if (controller.recordingState ==
        PracticeRecordingState.awaitingConfirmation) {
      await controller.confirmTranscript();
    }
    await tester.pump(const Duration(milliseconds: 20));
  }
  await tester.pump(const Duration(milliseconds: 220));
  await tester.pump(const Duration(milliseconds: 220));
  expect(controller.completedTurns, completedTurns + 1);
}

final class _MemoryProgressStore implements IeltsMockProgressStore {
  _MemoryProgressStore([this.value]);

  IeltsMockProgress? value;

  @override
  Future<IeltsMockProgress?> read(String sessionId) async {
    return value?.sessionId == sessionId ? value : null;
  }

  @override
  Future<void> write(IeltsMockProgress progress) async {
    value = progress;
  }

  @override
  Future<void> delete(String sessionId) async {
    if (value?.sessionId == sessionId) {
      value = null;
    }
  }
}

final class _UnusedQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getQuestionBank() {
    throw UnimplementedError();
  }
}

final class _IeltsPracticeClient
    implements
        PracticeClient,
        PracticeQuestionTipClient,
        PracticeRealtimeTranscriptionClient {
  _IeltsPracticeClient({
    required this.initialCompleted,
    this.turnLimit = 14,
    this.transcriptionFailuresRemaining = 0,
    this.transcriptionText,
    this.capabilities = _practiceCapabilities,
  }) : completed = initialCompleted;

  final int initialCompleted;
  Object? transcribeFailure;
  Completer<void>? transcriptionGate;
  Completer<void>? firstRealtimeFailure;
  final Completer<void> firstRealtimeUpdate = Completer<void>();
  final Completer<void> firstRealtimeFailureObserved = Completer<void>();
  final int turnLimit;
  int transcriptionFailuresRemaining;
  final String? transcriptionText;
  final PracticeCapabilities capabilities;
  int completed;
  SceneDefinition? activeScene;
  PracticeMode activeMode = PracticeMode.fullMock;
  final List<String> confirmedQuestionIds = [];
  final List<String> transcribedQuestionIds = [];
  int realtimeTranscriptions = 0;

  @override
  Future<void> clearAccountState() async {
    activeScene = null;
  }

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async => _snapshot();

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async => _snapshot();

  PracticeSessionSnapshot _snapshot() {
    final scene = activeScene ?? (throw StateError('No active IELTS Scene.'));
    final done = completed == turnLimit;
    final assignment = scene.experience == PracticeExperience.ieltsSpeaking
        ? _assignmentForActiveMode()
        : null;
    return PracticeSessionSnapshot(
      sessionId: _sessionId,
      planId: _planId,
      practiceExperience: scene.experience,
      sceneCategory: scene.category,
      practiceMode: activeMode,
      capabilities: capabilities,
      sessionVersion: completed + 1,
      completedTurns: completed,
      turnLimit: turnLimit,
      sessionCompleted: done,
      ieltsAssignment: assignment,
      currentQuestion: done ? null : _question(completed + 1),
    );
  }

  IeltsPracticeAssignment _assignmentForActiveMode() {
    return switch (activeMode) {
      PracticeMode.fullMock => testIeltsAssignment(
        mode: activeMode,
        part3QuestionCount: turnLimit - 9,
      ),
      PracticeMode.part1 => testIeltsAssignment(
        mode: activeMode,
        part1QuestionCount: turnLimit,
      ),
      PracticeMode.part2 => testIeltsAssignment(
        mode: activeMode,
        part3QuestionCount: turnLimit - 1,
      ),
      PracticeMode.part3 => testIeltsAssignment(
        mode: activeMode,
        part3QuestionCount: turnLimit,
      ),
      PracticeMode.fullSimulation || PracticeMode.focus => throw StateError(
        'Unsupported IELTS practice mode: $activeMode',
      ),
    };
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    transcribedQuestionIds.add(request.questionId);
    await transcriptionGate?.future;
    final failure = transcribeFailure;
    if (failure != null) {
      throw failure;
    }
    if (transcriptionFailuresRemaining > 0) {
      transcriptionFailuresRemaining--;
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        retryable: true,
      );
    }
    return TranscriptionCandidate(
      id: 'candidate-${completed + 1}',
      sessionId: request.sessionId,
      questionId: request.questionId,
      text: transcriptionText ?? 'Answer ${completed + 1}',
    );
  }

  @override
  Stream<PracticeTranscriptionEvent> transcribeRealtime({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
    required Stream<Uint8List> audioChunks,
  }) {
    final attempt = ++realtimeTranscriptions;
    var sentUpdate = false;
    var failureScheduled = false;
    final events = StreamController<PracticeTranscriptionEvent>(sync: true);
    audioChunks.listen(
      (chunk) {
        if (chunk.isEmpty || sentUpdate) {
          return;
        }
        sentUpdate = true;
        if (!firstRealtimeUpdate.isCompleted) {
          firstRealtimeUpdate.complete();
        }
        events.add(
          const PracticeTranscriptUpdated(
            text: 'I led the migration',
            isFinal: false,
          ),
        );
        final failure = firstRealtimeFailure;
        if (attempt == 1 && failure != null && !failureScheduled) {
          failureScheduled = true;
          unawaited(
            failure.future.then((_) async {
              if (!firstRealtimeFailureObserved.isCompleted) {
                firstRealtimeFailureObserved.complete();
              }
              events.addError(
                const PracticeClientException(
                  kind: PracticeClientFailureKind.network,
                  retryable: true,
                ),
              );
              await events.close();
            }),
          );
        }
      },
      onError: events.addError,
      onDone: () {
        if (events.isClosed) {
          return;
        }
        final text = 'I led the migration safely.';
        events.add(PracticeTranscriptUpdated(text: text, isFinal: true));
        events.add(
          PracticeCandidateCompleted(
            TranscriptionCandidate(
              id: 'candidate-${completed + 1}',
              sessionId: sessionId,
              questionId: questionId,
              text: text,
            ),
          ),
        );
        unawaited(events.close());
      },
    );
    return events.stream;
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    final scene = activeScene ?? (throw StateError('No active IELTS Scene.'));
    confirmedQuestionIds.add(questionId);
    completed++;
    final done = completed == turnLimit;
    final answerText = realtimeTranscriptions > 0
        ? 'I led the migration safely.'
        : transcriptionText ?? 'Answer $completed';
    return PracticeTurnConfirmation(
      turnId: 'turn-$completed',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      answer: PracticeMessage(
        id: 'answer-$completed',
        role: PracticeMessageRole.user,
        text: answerText,
      ),
      completedTurns: completed,
      turnLimit: turnLimit,
      sessionCompleted: done,
      practiceExperience: scene.experience,
      sceneCategory: scene.category,
      practiceMode: activeMode,
      capabilities: capabilities,
      sessionVersion: completed + 1,
      nextQuestion: done ? null : _question(completed + 1),
    );
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeQuestionTip> ensureQuestionTip({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
  }) async => PracticeQuestionTip(
    id: 'tip-$questionId',
    sessionId: sessionId,
    questionId: questionId,
    content: 'Give a direct answer and one short reason.',
    createdAt: DateTime.utc(2026, 8, 10),
  );
}

final class _Recorder implements PracticeRecorder {
  bool recording = false;
  int startCalls = 0;

  @override
  Future<void> start() async {
    startCalls++;
    recording = true;
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    recording = false;
    return const RecordedPracticeAudio(
      path: 'ielts.wav',
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
    recording = false;
  }
}

final class _RestartableStreamingRecorder
    implements PracticeRecorder, PracticeStreamingRecorder {
  StreamController<Uint8List>? _chunks;
  int streamingStarts = 0;
  int discardCurrentCalls = 0;
  final Completer<void> firstDiscardFinished = Completer<void>();

  void add(Uint8List chunk) => _chunks?.add(chunk);

  @override
  Future<void> start() => throw UnimplementedError();

  @override
  Future<Stream<Uint8List>> startAudioStream() async {
    if (_chunks != null) {
      throw StateError('Recorder is already active.');
    }
    streamingStarts++;
    final chunks = StreamController<Uint8List>();
    _chunks = chunks;
    return chunks.stream;
  }

  @override
  Future<RecordedPracticeAudio> stop() => throw UnimplementedError();

  @override
  Future<RecordedPracticeAudio> stopAudioStream() async {
    await _closeCurrent();
    return const RecordedPracticeAudio(
      path: 'ielts-realtime.wav',
      contentType: 'audio/wav',
      sizeBytes: 48,
    );
  }

  @override
  Future<void> discardCurrent() async {
    discardCurrentCalls++;
    await _closeCurrent();
    if (!firstDiscardFinished.isCompleted) {
      firstDiscardFinished.complete();
    }
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> clearAccountState() => _closeCurrent();

  Future<void> _closeCurrent() async {
    final chunks = _chunks;
    _chunks = null;
    await chunks?.close();
  }
}

final class _ImmediateExaminerSpeaker implements PracticePromptSpeaker {
  final List<String> spoken = <String>[];

  @override
  Future<void> speak(String text) async {
    spoken.add(text);
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> dispose() async {}
}

final class _ControlledExaminerSpeaker implements PracticePromptSpeaker {
  final List<String> spoken = <String>[];
  Completer<void>? _current;

  @override
  Future<void> speak(String text) {
    spoken.add(text);
    final completion = Completer<void>();
    _current = completion;
    return completion.future;
  }

  void completeCurrent() {
    _current?.complete();
    _current = null;
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> dispose() async {}
}

final class _PendingReportClient implements IeltsSpeakingReportClient {
  final started = Completer<void>();
  final response = Completer<IeltsSpeakingReportEnvelope>();

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(String practiceSessionId) {
    started.complete();
    return response.future;
  }

  @override
  Future<void> clearAccountState() async {}
}

PracticeQuestion _question(int turn) {
  return PracticeQuestion(
    id: 'question-$turn',
    sessionId: _sessionId,
    text: turn == 9
        ? 'Describe a skill you would like to learn.\n'
              'You should say:\n'
              '• What the skill is\n'
              '• Why you want to learn it'
        : 'Question $turn',
    speechPath: 'speech/question-$turn.wav',
  );
}

final class _QuestionMediaClient implements PracticeMediaClient {
  final List<String> loadedPaths = <String>[];

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async {
    loadedPaths.add(speechPath);
    return Uint8List.fromList(<int>[1, 2, 3]);
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) async => Uint8List(0);

  @override
  Future<void> deleteRecording(String audioAssetId) async {}

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

final class _QuestionAudioPlayer implements PracticeAudioPlayer {
  final StreamController<void> _completions =
      StreamController<void>.broadcast();
  int playCount = 0;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> playWav(Uint8List bytes) async => playCount++;

  @override
  Future<void> stop() async {}

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() => _completions.close();
}

const _sessionId = 'session-ielts-full';
const _planId = 'plan-ielts-full';
const _ieltsSceneId = 'scn_ielts_speaking_test';
const _practiceCapabilities = PracticeCapabilities(
  retryAllowed: false,
  questionTranslationAllowed: false,
  questionTipsAllowed: false,
  avatarAllowed: false,
  speechFeedbackAllowed: false,
);
final _ieltsScene = _sceneFixture(
  id: _ieltsSceneId,
  name: 'IELTS 口语完整模拟',
  brief: 'Part 1, Part 2, Part 3',
  practiceOptions: const <PracticeOption>[
    PracticeOption(
      id: 'option-ielts-full-mock',
      sceneId: _ieltsSceneId,
      mode: PracticeMode.fullMock,
      displayName: '完整模考',
      suggestedDurationSeconds: 900,
      turnPolicyRef: 'ielts.full_mock.turn.v1',
      sessionPolicyRef: 'ielts.full_mock.session.v1',
      evaluationPolicyRef: 'ielts.full_mock.evaluation.v1',
    ),
    PracticeOption(
      id: 'option-ielts-part-1',
      sceneId: _ieltsSceneId,
      mode: PracticeMode.part1,
      displayName: 'Part 1',
      suggestedDurationSeconds: 300,
      turnPolicyRef: 'ielts.part_1.turn.v1',
      sessionPolicyRef: 'ielts.part_1.session.v1',
      evaluationPolicyRef: 'ielts.section.evaluation.v1',
    ),
    PracticeOption(
      id: 'option-ielts-part-2',
      sceneId: _ieltsSceneId,
      mode: PracticeMode.part2,
      displayName: 'Part 2',
      suggestedDurationSeconds: 420,
      turnPolicyRef: 'ielts.part_2.turn.v1',
      sessionPolicyRef: 'ielts.part_2.session.v1',
      evaluationPolicyRef: 'ielts.section.evaluation.v1',
    ),
    PracticeOption(
      id: 'option-ielts-part-3',
      sceneId: _ieltsSceneId,
      mode: PracticeMode.part3,
      displayName: 'Part 3',
      suggestedDurationSeconds: 300,
      turnPolicyRef: 'ielts.part_3.turn.v1',
      sessionPolicyRef: 'ielts.part_3.session.v1',
      evaluationPolicyRef: 'ielts.section.evaluation.v1',
    ),
  ],
);
SceneDefinition _sceneFixture({
  required String id,
  required String name,
  required String brief,
  PracticeExperience experience = PracticeExperience.ieltsSpeaking,
  SceneCategory category = SceneCategory.ieltsSpeaking,
  List<PracticeOption>? practiceOptions,
}) => testScene(
  id: id,
  experience: experience,
  category: category,
  name: name,
  prompt: ScenePrompt(
    publicSceneBrief: brief,
    practiceGoal: 'Complete the IELTS speaking practice.',
    userRole: 'Candidate',
    aiRole: 'IELTS examiner',
    personaSummary: 'Neutral and concise.',
    focusAreas: const <String>['fluency'],
    turnBlueprints: const <String>['Ask the next speaking question.'],
  ),
  practiceOptions: practiceOptions,
);
