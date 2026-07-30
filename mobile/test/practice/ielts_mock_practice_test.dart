import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/practice/ielts_mock_practice.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/practice/ielts_mock_progress_store.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

void main() {
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
      await controller.selectScene(_ieltsScene);

      await tester.pumpWidget(
        MaterialApp(
          home: PracticePage(
            agentController: controller,
            ieltsMockProgressStore: store,
          ),
        ),
      );
      await tester.pump();

      expect(
        find.byKey(const Key('ielts-mock-part-1-complete')),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('ielts-mock-continue')));
      await tester.pump();
      expect(find.byKey(const Key('ielts-mock-part-2-intro')), findsOneWidget);

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
      expect(store.value?.notes, contains('weekly practice'));
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
    await controller.selectScene(_ieltsScene);
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
    await controller.selectScene(_ieltsScene);

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

  testWidgets('restored matter identity still opens the three-part mock flow', (
    tester,
  ) async {
    final practice = _IeltsPracticeClient(
      initialCompleted: 8,
      snapshotScene: const AgentScene(
        id: 'matter-restored',
        title: 'IELTS 口语完整模拟',
        description: '恢复的练习场景',
      ),
    );
    final controller = AgentController(
      client: FakeAgentClient(),
      practiceClient: practice,
      recorder: _Recorder(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.selectScene(_ieltsScene);

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
    await controller.selectScene(_ieltsScene);
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

final class _IeltsPracticeClient implements PracticeClient {
  _IeltsPracticeClient({required this.initialCompleted, this.snapshotScene})
    : completed = initialCompleted;

  final int initialCompleted;
  final AgentScene? snapshotScene;
  int completed;
  final List<String> confirmedQuestionIds = [];

  @override
  Future<void> clearAccountState() async {}

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
    final done = completed == 14;
    return PracticeStartResult(
      snapshot: PracticeSessionSnapshot(
        sessionId: _sessionId,
        matter: snapshotScene == null
            ? activeMatter
            : AgentMatter(id: activeMatter.id, scene: snapshotScene!),
        completedTurns: completed,
        turnLimit: 14,
        sessionCompleted: done,
        currentQuestion: done ? null : _question(completed + 1),
        review: done ? _review : null,
      ),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
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
    confirmedQuestionIds.add(questionId);
    completed++;
    final done = completed == 14;
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
      turnLimit: 14,
      sessionCompleted: done,
      nextQuestion: done ? null : _question(completed + 1),
      review: done ? _review : null,
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
  @override
  Future<void> start() async {}

  @override
  Future<RecordedPracticeAudio> stop() async {
    return const RecordedPracticeAudio(
      path: 'ielts.wav',
      contentType: 'audio/wav',
      sizeBytes: 100,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> discardCurrent() async {}

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
  );
}

const _sessionId = 'session-ielts-full';
const _ieltsScene = AgentScene(
  id: ieltsSpeakingFullMockScenarioId,
  title: 'IELTS 口语完整模拟',
  description: 'Part 1, Part 2, Part 3',
);
const _review = AgentReview(
  id: 'review-ielts',
  title: 'Review',
  summary: 'Summary',
  strength: 'Strength',
  nextFocus: 'Next focus',
);
