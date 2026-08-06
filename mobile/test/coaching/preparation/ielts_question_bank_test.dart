import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  test(
    'full mock randomization includes unfinished non-five-question sets',
    () {
      final selection = randomIeltsFullMockSelection(
        bank: _bank,
        completedPart1SetIds: const {'p1-001'},
        completedTopicGroupIds: const {'p23-001'},
        random: Random(7),
      );

      expect(
        selection,
        const IeltsPracticeSelection(
          part1SetId: 'p1-002',
          topicGroupId: 'p23-002',
        ),
      );
      expect(selection.toJson(), {
        'part_1_set_id': 'p1-002',
        'topic_group_id': 'p23-002',
      });
    },
  );

  test('Part 2 and Part 3 progress is separate and survives restore', () async {
    final history = MemoryIeltsPracticeHistoryStore();
    final first = IeltsPreparationController(
      client: _FixtureQuestionBankClient(),
      historyStore: history,
    );
    addTearDown(first.dispose);
    await first.activateAccount('account-1');
    await first.loadIfNeeded();

    const selection = IeltsPracticeSelection(topicGroupId: 'p23-001');
    await first.beginSession('session-1', PracticeMode.part2, selection);
    expect(first.progress(PracticeMode.part2, 'p23-001').inProgress, isTrue);
    expect(first.progress(PracticeMode.part3, 'p23-001').inProgress, isFalse);

    await first.markPartCompleted('session-1', PracticeMode.part2);
    await first.markPartCompleted('session-1', PracticeMode.part2);
    await first.markPartStarted('session-1', PracticeMode.part3);
    await first.markPartCompleted('session-1', PracticeMode.part3);

    expect(first.progress(PracticeMode.part2, 'p23-001').attemptCount, 1);
    expect(first.progress(PracticeMode.part3, 'p23-001').attemptCount, 1);
    expect(
      first.nextUnfinishedSelection(PracticeMode.part2, afterId: 'p23-001'),
      const IeltsPracticeSelection(topicGroupId: 'p23-002'),
    );

    final restored = IeltsPreparationController(
      client: _FixtureQuestionBankClient(),
      historyStore: history,
    );
    addTearDown(restored.dispose);
    await restored.activateAccount('account-1');

    expect(restored.selectionForSession('session-1'), selection);
    expect(restored.progress(PracticeMode.part2, 'p23-001').attemptCount, 1);
    expect(restored.progress(PracticeMode.part3, 'p23-001').attemptCount, 1);
  });

  test(
    'full mock still prioritizes a topic with one unfinished bound part',
    () async {
      final controller = IeltsPreparationController(
        client: _FixtureQuestionBankClient(),
        historyStore: MemoryIeltsPracticeHistoryStore(),
      );
      addTearDown(controller.dispose);
      await controller.activateAccount('account-1');
      await controller.loadIfNeeded();

      const partlyFinished = IeltsPracticeSelection(topicGroupId: 'p23-001');
      await controller.beginSession(
        'session-partly',
        PracticeMode.part2,
        partlyFinished,
      );
      await controller.markPartCompleted('session-partly', PracticeMode.part2);

      const fullyFinished = IeltsPracticeSelection(topicGroupId: 'p23-002');
      await controller.beginSession(
        'session-fully',
        PracticeMode.part2,
        fullyFinished,
      );
      await controller.markPartCompleted('session-fully', PracticeMode.part2);
      await controller.markPartStarted('session-fully', PracticeMode.part3);
      await controller.markPartCompleted('session-fully', PracticeMode.part3);

      for (var index = 0; index < 32; index++) {
        expect(controller.randomFullMockSelection()?.topicGroupId, 'p23-001');
      }
    },
  );
}

final class _FixtureQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getQuestionBank() async => _bank;
}

final _bank = IeltsQuestionBank(
  bankId: 'ielts-bank-1',
  season: '2026-05-08',
  sourceCutoff: DateTime.utc(2026, 6, 18),
  part1Sets: const [
    IeltsPart1Set(
      id: 'p1-001',
      title: 'Part 1 套题 01',
      topics: [
        IeltsPart1Topic(
          title: 'Hometown',
          release: 'carry_over',
          questions: ['Where is your hometown?', 'Do you like it?'],
        ),
        IeltsPart1Topic(
          title: 'Music',
          release: 'new',
          questions: [
            'Do you like music?',
            'Have you taken music classes?',
            'What music do you prefer?',
          ],
        ),
        IeltsPart1Topic(
          title: 'Parks',
          release: 'new',
          questions: [
            'Do you visit parks?',
            'Should cities have more parks?',
            'Did you visit parks as a child?',
          ],
        ),
      ],
      questionCount: 8,
    ),
    IeltsPart1Set(
      id: 'p1-002',
      title: 'Part 1 套题 02',
      topics: [
        IeltsPart1Topic(
          title: 'Work',
          release: 'carry_over',
          questions: ['What do you do?', 'Do you enjoy your work?'],
        ),
        IeltsPart1Topic(
          title: 'Reading',
          release: 'new',
          questions: [
            'Do you enjoy reading?',
            'What do you read?',
            'Did you read as a child?',
          ],
        ),
        IeltsPart1Topic(
          title: 'Science',
          release: 'new',
          questions: [
            'Do you like science?',
            'Is science useful?',
            'Should children learn science?',
          ],
        ),
      ],
      questionCount: 8,
    ),
  ],
  part1Topics: const [
    IeltsPart1PracticeTopic(
      id: 'p1-topic-001',
      titleZh: '家乡',
      titleEn: 'Hometown',
      release: 'carry_over',
      category: IeltsTopicCategory.place,
      questions: ['Where is your hometown?', 'Do you like it?'],
    ),
    IeltsPart1PracticeTopic(
      id: 'p1-topic-002',
      titleZh: '音乐',
      titleEn: 'Music',
      release: 'new',
      category: IeltsTopicCategory.thing,
      questions: ['Do you like music?', 'What music do you prefer?'],
    ),
  ],
  topicGroups: const [
    IeltsTopicGroup(
      id: 'p23-001',
      title: '语言学习',
      release: 'new',
      category: IeltsTopicCategory.thing,
      cueCard: IeltsCueCard(
        prompt: 'Describe a language you would like to learn',
        points: ['What it is', 'Why', 'How', 'And explain the benefit'],
      ),
      part3Questions: ['Q1', 'Q2', 'Q3', 'Q4', 'Q5'],
      supplementedQuestionCount: 0,
    ),
    IeltsTopicGroup(
      id: 'p23-002',
      title: '环境保护',
      release: 'carry_over',
      category: IeltsTopicCategory.event,
      cueCard: IeltsCueCard(
        prompt: 'Describe an environmental law',
        points: ['What it is', 'Where', 'Who', 'And explain the effect'],
      ),
      part3Questions: ['Q1', 'Q2'],
      supplementedQuestionCount: 0,
    ),
  ],
);
