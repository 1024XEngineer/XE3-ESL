import '../../support/scene_fixtures.dart';

import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/practice/ielts_mock_practice.dart';
import 'package:speakup/features/coaching/practice/ielts_examiner_speaker.dart';
import 'package:speakup/features/coaching/practice/practice.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/practice/ielts_mock_progress_store.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/review/ielts_speaking_report.dart';
import 'package:speakup/review/ielts_speaking_report_client.dart';
import 'package:speakup/review/ielts_speaking_report_controller.dart';

void main() {
  testWidgets('Part 1 prefers the shared practice question voice', (
    tester,
  ) async {
    final speaker = _ImmediateExaminerSpeaker();
    final media = _QuestionMediaClient();
    final player = _QuestionAudioPlayer();
    final practice = _IeltsPracticeClient(initialCompleted: 0);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      mediaClient: media,
      audioPlayer: player,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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
    'Part 1 auto-plays voice bubbles and reveals English only on request',
    (tester) async {
      final speaker = _ImmediateExaminerSpeaker();
      final practice = _IeltsPracticeClient(initialCompleted: 0);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
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
      expect(find.text(_question(1).text), findsNothing);

      await tester.tap(
        find.byKey(const Key('ielts-question-transcript-toggle-question-1')),
      );
      await tester.pump();

      expect(find.text(_question(1).text), findsOneWidget);
      expect(
        find.byKey(const Key('ielts-question-transcript-question-1')),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('ielts-mock-record')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-record')));
      await tester.pump();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));
      await tester.pump();

      expect(speaker.spoken.last, _question(2).text);
      expect(find.text(_question(2).text), findsNothing);
    },
  );

  testWidgets('Part 3 starts with an auto-playing hidden-text voice bubble', (
    tester,
  ) async {
    final speaker = _ImmediateExaminerSpeaker();
    final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 1);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsPart3Scene);

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
    expect(
      find.byKey(const Key('ielts-question-voice-question-1')),
      findsOneWidget,
    );
    expect(find.text(_question(1).text), findsNothing);
  });

  testWidgets(
    'Part 2 reads instructions and Cue Card before starting 60 seconds',
    (tester) async {
      final now = DateTime.utc(2026, 8, 3, 4, 30);
      final speaker = _ControlledExaminerSpeaker();
      final practice = _IeltsPracticeClient(initialCompleted: 8);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore();
      addTearDown(controller.dispose);
      await controller.initialize();
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

      await tester.tap(find.text('Continue to Part 2'));
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
        find.byKey(const Key('ielts-mock-part-2-preparation')),
        findsOneWidget,
      );
      expect(find.text('60s'), findsOneWidget);
      expect(
        store.value?.preparationDeadline,
        now.add(const Duration(seconds: 60)),
      );
    },
  );

  testWidgets(
    'Part 1 boundary enters prep, keeps notes, and submits the Part 2 long turn',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 8);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore();
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            agentController: controller,
            ieltsMockProgressStore: store,
            ieltsExaminerSpeaker: _ImmediateExaminerSpeaker(),
          ),
        ),
      );
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-1-complete')),
        findsOneWidget,
      );
      await tester.tap(find.text('Continue to Part 2'));
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
        find.byKey(const Key('ielts-mock-part-2-preparation')),
        findsOneWidget,
      );
      await tester.enterText(
        find.byKey(const Key('ielts-mock-notes')),
        'online course, weekly practice, useful at work',
      );
      await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-2-speaking')),
        findsOneWidget,
      );
      expect(controller.recordingState, PracticeRecordingState.recording);
      expect(
        find.byKey(const Key('ielts-part2-recording-status')),
        findsOneWidget,
      );
      expect(find.textContaining('Listening ·'), findsOneWidget);
      expect(
        find.text('online course, weekly practice, useful at work'),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
      await tester.pump();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));

      expect(controller.completedTurns, 9);
      expect(practice.confirmedQuestionIds, ['question-9']);
      expect(
        find.byKey(const Key('ielts-mock-part-2-complete')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('ielts-part2-answer-feedback')),
        findsOneWidget,
      );
      expect(find.text('Answer 9'), findsOneWidget);
      expect(store.value?.notes, contains('weekly practice'));

      await tester.tap(find.text('Continue to Part 3'));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
      expect(find.text('Part 3 · Discussion'), findsOneWidget);
    },
  );

  testWidgets(
    'failed Part 2 transcription returns to recording without recovery actions',
    (tester) async {
      final practice = _IeltsPracticeClient(
        initialCompleted: 8,
        transcriptionFailuresRemaining: 1,
      );
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      final store = _MemoryProgressStore();
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            agentController: controller,
            ieltsMockProgressStore: store,
            ieltsExaminerSpeaker: _ImmediateExaminerSpeaker(),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.text('Continue to Part 2'));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-part-2-start')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-start-speaking')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));

      expect(controller.completedTurns, 8);
      expect(
        find.byKey(const Key('ielts-mock-part-2-speaking')),
        findsOneWidget,
      );
      expect(find.text('Start Speaking →'), findsOneWidget);
      expect(controller.hasPendingPracticeAudio, isFalse);
      expect(find.text('Delete'), findsNothing);
      expect(find.text('Retry transcript'), findsNothing);

      await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
      await tester.pump();
      expect(controller.recordingState, PracticeRecordingState.recording);
      expect(find.textContaining('Listening ·'), findsOneWidget);

      await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
      await tester.pump();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 220));

      expect(controller.completedTurns, 9);
      expect(
        find.byKey(const Key('ielts-mock-part-2-complete')),
        findsOneWidget,
      );
      await tester.tap(find.text('Continue to Part 3'));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
    },
  );

  testWidgets('restores an unexpired preparation checkpoint and notes', (
    tester,
  ) async {
    final now = DateTime.utc(2026, 7, 29, 8);
    final practice = _IeltsPracticeClient(initialCompleted: 8);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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
      find.byKey(const Key('ielts-mock-part-2-preparation')),
      findsOneWidget,
    );
    expect(find.text('33s'), findsOneWidget);
    expect(find.text('restored note'), findsOneWidget);
  });

  testWidgets('Part 2 clears failed audio without Delete or Retry actions', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 8)
      ..transcribeFailure = StateError('transcription failed');
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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
    await tester.tap(find.byKey(const Key('ielts-mock-finish-speaking')));
    await tester.pump();
    await tester.pump();

    expect(find.byKey(const Key('ielts-mock-part-2-speaking')), findsOneWidget);
    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(controller.completedTurns, 8);
    expect(find.text('Delete'), findsNothing);
    expect(find.text('Retry transcript'), findsNothing);
    expect(find.text('Start Speaking →'), findsOneWidget);
  });

  testWidgets('Part 1 clears failed transcription without a recovery dock', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 0)
      ..transcribeFailure = StateError('transcription failed');
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: recorder,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('ielts-mock-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
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
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: PracticePage(
          agentController: controller,
          ieltsMockProgressStore: _MemoryProgressStore(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 250));

    expect(find.byKey(const Key('ielts-mock-complete')), findsOneWidget);
    expect(find.text('Mock Test Complete'), findsOneWidget);
    expect(find.textContaining('OVERALL BAND'), findsNothing);
    expect(find.byKey(const Key('practice-page')), findsNothing);
  });

  testWidgets(
    'completed full mock exits before parked context can show the empty practice page',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 14);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, practice, _ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const Key('open-completed-mock'),
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => PracticePage(
                      agentController: controller,
                      ieltsMockProgressStore: _MemoryProgressStore(),
                      onExitRequested: () async {
                        await controller.selectScene(_nonIeltsScene);
                        return true;
                      },
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
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    final reportClient = _PendingReportClient();
    final reportController = IeltsSpeakingReportController(
      client: reportClient,
    );
    addTearDown(controller.dispose);
    addTearDown(reportController.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          reportController: reportController,
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

  testWidgets('section completion never requests the full-mock report', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 7, turnLimit: 8);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    final preparation = PreparationController(client: _EmptySceneClient());
    final reportClient = _PendingReportClient();
    final reportController = IeltsSpeakingReportController(
      client: reportClient,
    );
    addTearDown(controller.dispose);
    addTearDown(preparation.dispose);
    addTearDown(reportController.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsPart1Scene);
    expect(controller.errorMessage, isNull);
    expect(controller.practiceSessionId, _sessionId);
    await preparation.beginIeltsSession(
      _sessionId,
      const IeltsPracticeSelection(
        mode: IeltsPracticeMode.part1,
        part1SetId: 'p1-set-02',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSpeakingMockPage(
          controller: controller,
          progressStore: _MemoryProgressStore(),
          preparationController: preparation,
          reportController: reportController,
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    expect(controller.completedTurns, 8);
    expect(
      find.byKey(const Key('ielts-section-practice-complete-part1')),
      findsOneWidget,
    );
    expect(reportClient.started.isCompleted, isFalse);
    expect(reportController.practiceSessionId, isNull);
  });

  testWidgets('the full-mock Scene model opens the three-part flow', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 8);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: PracticePage(
          agentController: controller,
          ieltsMockProgressStore: _MemoryProgressStore(),
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
        model: SceneModel.examBasicDialogue,
      );
      final practice = _IeltsPracticeClient(initialCompleted: 8);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, practice, sameTitleScene);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            agentController: controller,
            ieltsMockProgressStore: _MemoryProgressStore(),
          ),
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
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
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
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    final preparation = PreparationController(client: _EmptySceneClient());
    addTearDown(controller.dispose);
    addTearDown(preparation.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsPart1Scene);
    await preparation.beginIeltsSession(
      _sessionId,
      const IeltsPracticeSelection(
        mode: IeltsPracticeMode.part1,
        part1SetId: 'p1-set-02',
      ),
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
                    preparationController: preparation,
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

    final request = preparation.takeIeltsNavigationRequest();
    expect(request?.mode, IeltsPracticeMode.part1);
    expect(request?.selection, isNull);
    expect(find.byKey(const Key('open-section')), findsOneWidget);
  });

  testWidgets(
    'Part 2 section completion offers bound Part 3 and list actions',
    (tester) async {
      final practice = _IeltsPracticeClient(initialCompleted: 1, turnLimit: 6);
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      addTearDown(controller.dispose);
      await controller.initialize();
      await _activatePractice(controller, practice, _ieltsPart2Scene);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            agentController: controller,
            ieltsMockProgressStore: _MemoryProgressStore(),
          ),
        ),
      );
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-part2-practice-complete')),
        findsOneWidget,
      );
      expect(find.text('继续对应 Part 3'), findsOneWidget);
      expect(find.text('下一套未练习'), findsOneWidget);
      expect(find.text('再练本套'), findsOneWidget);
      expect(find.text('返回套题列表'), findsOneWidget);

      await tester.tap(find.text('继续对应 Part 3'));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-3')), findsOneWidget);
      expect(find.text('Part 3 · Discussion'), findsOneWidget);
    },
  );

  testWidgets('one-question Part 3 section completes after its original item', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 0, turnLimit: 1);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsPart3Scene);

    await tester.pumpWidget(
      MaterialApp(
        home: PracticePage(
          agentController: controller,
          ieltsMockProgressStore: _MemoryProgressStore(),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byKey(const Key('ielts-part3-start')));
    await tester.pump();
    expect(find.text('0/1'), findsOneWidget);

    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    expect(controller.completedTurns, 1);
    expect(
      find.byKey(const Key('ielts-section-practice-complete-part3')),
      findsOneWidget,
    );
  });

  testWidgets('full mock completes after a single original Part 3 question', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(initialCompleted: 9, turnLimit: 10);
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await _activatePractice(controller, practice, _ieltsScene);

    await tester.pumpWidget(
      MaterialApp(
        home: PracticePage(
          agentController: controller,
          ieltsMockProgressStore: _MemoryProgressStore(),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('Continue to Part 3'));
    await tester.pump();
    expect(find.text('0/1'), findsOneWidget);

    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('ielts-mock-record')));
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 220));

    expect(controller.completedTurns, 10);
    expect(find.byKey(const Key('ielts-mock-complete')), findsOneWidget);
    expect(find.text('1 answers'), findsOneWidget);
  });

  testWidgets('section Scene models open the matching IELTS flow', (
    tester,
  ) async {
    for (final testCase
        in <({SceneDefinition selected, int turnLimit, Key expected})>[
          (
            selected: _ieltsPart1Scene,
            turnLimit: 8,
            expected: const Key('ielts-mock-part-1'),
          ),
          (
            selected: _ieltsPart2Scene,
            turnLimit: 6,
            expected: const Key('ielts-mock-part-2-intro'),
          ),
          (
            selected: _ieltsPart3Scene,
            turnLimit: 5,
            expected: const Key('ielts-part3-topic-intro'),
          ),
        ]) {
      final practice = _IeltsPracticeClient(
        initialCompleted: 0,
        turnLimit: testCase.turnLimit,
      );
      final controller = AgentController(
        client: FakeAgentClient(),
        practiceClient: practice,
        recorder: _Recorder(),
      );
      await controller.initialize();
      await _activatePractice(controller, practice, testCase.selected);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            key: ValueKey(testCase.selected.id),
            agentController: controller,
            ieltsMockProgressStore: _MemoryProgressStore(),
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
  AgentController controller,
  _IeltsPracticeClient practice,
  SceneDefinition scene,
) async {
  await controller.selectScene(scene);
  practice.activeScene = scene;
  await controller.activateCreatedPractice(
    threadId: controller.threadId!,
    goalId: controller.activeGoal!.id,
    scene: scene,
    sessionId: _sessionId,
    planId: _planId,
    turnLimit: practice.turnLimit,
    clientOperationId: 'activate-${scene.id}',
  );
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

final class _EmptySceneClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() {
    throw UnimplementedError();
  }
}

final class _IeltsPracticeClient implements PracticeClient {
  _IeltsPracticeClient({
    required this.initialCompleted,
    this.turnLimit = 14,
    this.transcriptionFailuresRemaining = 0,
  }) : completed = initialCompleted;

  final int initialCompleted;
  Object? transcribeFailure;
  final int turnLimit;
  int transcriptionFailuresRemaining;
  int completed;
  SceneDefinition? activeScene;
  final List<String> confirmedQuestionIds = [];

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
    return PracticeSessionSnapshot(
      sessionId: _sessionId,
      planId: _planId,
      sceneFamily: scene.family,
      sceneModel: scene.model,
      sessionVersion: completed + 1,
      completedTurns: completed,
      turnLimit: turnLimit,
      sessionCompleted: done,
      currentQuestion: done ? null : _question(completed + 1),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    final failure = transcribeFailure;
    if (failure != null) {
      throw failure;
    }
    if (transcriptionFailuresRemaining > 0) {
      transcriptionFailuresRemaining--;
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
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
    final scene = activeScene ?? (throw StateError('No active IELTS Scene.'));
    confirmedQuestionIds.add(questionId);
    completed++;
    final done = completed == turnLimit;
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
      turnLimit: turnLimit,
      sessionCompleted: done,
      sceneFamily: scene.family,
      sceneModel: scene.model,
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
}

final class _Recorder implements PracticeRecorder {
  bool recording = false;

  @override
  Future<void> start() async {
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

final class _ImmediateExaminerSpeaker implements IeltsExaminerSpeaker {
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

final class _ControlledExaminerSpeaker implements IeltsExaminerSpeaker {
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
final _ieltsScene = _sceneFixture(
  id: ieltsSpeakingFullMockSceneId,
  name: 'IELTS 口语完整模拟',
  brief: 'Part 1, Part 2, Part 3',
  model: SceneModel.ieltsSpeakingFullMock,
);
final _ieltsPart2Scene = _sceneFixture(
  id: 'scn_ielts_speaking_part_2',
  name: 'IELTS Speaking Part 2',
  brief: 'Part 2 cue card with bound Part 3',
  model: SceneModel.ieltsSpeakingPart2,
);
final _ieltsPart1Scene = _sceneFixture(
  id: 'scn_ielts_speaking_part_1',
  name: 'IELTS Speaking Part 1',
  brief: 'Part 1 familiar-topic questions',
  model: SceneModel.ieltsSpeakingPart1,
);
final _ieltsPart3Scene = _sceneFixture(
  id: 'scn_ielts_speaking_part_3',
  name: 'IELTS Speaking Part 3',
  brief: 'Part 3 discussion',
  model: SceneModel.ieltsSpeakingPart3,
);
final _nonIeltsScene = _sceneFixture(
  id: 'scn_general_practice',
  name: 'General practice',
  brief: 'Non-IELTS practice scene',
  family: SceneFamily.interview,
  model: SceneModel.interviewBasicDialogue,
);
SceneDefinition _sceneFixture({
  required String id,
  required String name,
  required String brief,
  SceneFamily family = SceneFamily.exam,
  SceneModel model = SceneModel.examBasicDialogue,
}) => testScene(
  id: id,
  family: family,
  model: model,
  name: name,
  prompt: ScenePrompt(
    publicSceneBrief: brief,
    practiceGoal: 'Complete the IELTS speaking practice.',
    userRole: 'Candidate',
    aiRole: 'IELTS examiner',
    personaSummary: 'Neutral and concise.',
    focusAreas: const <String>['fluency'],
    turnBlueprints: const <String>['Ask the next speaking question.'],
    suggestedDurationSeconds: 600,
  ),
);
