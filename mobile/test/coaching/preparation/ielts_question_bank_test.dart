import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  test('full mock randomization prioritizes unfinished frozen sets', () {
    final selection = randomIeltsFullMockSelection(
      bank: _bank,
      completedPart1SetIds: const {'p1-001'},
      completedTopicGroupIds: const {'p23-001'},
      random: Random(7),
    );

    expect(
      selection,
      const IeltsPracticeSelection(
        mode: IeltsPracticeMode.fullMock,
        part1SetId: 'p1-002',
        topicGroupId: 'p23-002',
      ),
    );
    expect(selection.toJson(), {
      'mode': 'FULL_MOCK',
      'part_1_set_id': 'p1-002',
      'topic_group_id': 'p23-002',
    });
  });

  test('Part 2 and Part 3 progress is separate and survives restore', () async {
    final history = MemoryIeltsPracticeHistoryStore();
    final first = PreparationController(
      client: _EmptyCatalogClient(),
      ieltsQuestionBankClient: _FixtureQuestionBankClient(),
      ieltsHistoryStore: history,
    );
    addTearDown(first.dispose);
    await first.activateAccount('account-1');
    await first.loadIeltsQuestionBankIfNeeded();

    const selection = IeltsPracticeSelection(
      mode: IeltsPracticeMode.part2,
      topicGroupId: 'p23-001',
    );
    await first.beginIeltsSession('session-1', selection);
    expect(
      first.ieltsProgress(IeltsPracticeMode.part2, 'p23-001').inProgress,
      isTrue,
    );
    expect(
      first.ieltsProgress(IeltsPracticeMode.part3, 'p23-001').inProgress,
      isFalse,
    );

    await first.markIeltsPartCompleted('session-1', IeltsPracticeMode.part2);
    await first.markIeltsPartCompleted('session-1', IeltsPracticeMode.part2);
    await first.markIeltsPartStarted('session-1', IeltsPracticeMode.part3);
    await first.markIeltsPartCompleted('session-1', IeltsPracticeMode.part3);

    expect(
      first.ieltsProgress(IeltsPracticeMode.part2, 'p23-001').attemptCount,
      1,
    );
    expect(
      first.ieltsProgress(IeltsPracticeMode.part3, 'p23-001').attemptCount,
      1,
    );
    expect(
      first.nextUnfinishedSelection(
        IeltsPracticeMode.part2,
        afterId: 'p23-001',
      ),
      const IeltsPracticeSelection(
        mode: IeltsPracticeMode.part2,
        topicGroupId: 'p23-002',
      ),
    );

    final restored = PreparationController(
      client: _EmptyCatalogClient(),
      ieltsQuestionBankClient: _FixtureQuestionBankClient(),
      ieltsHistoryStore: history,
    );
    addTearDown(restored.dispose);
    await restored.activateAccount('account-1');

    expect(restored.ieltsSelectionForSession('session-1'), selection);
    expect(
      restored.ieltsProgress(IeltsPracticeMode.part2, 'p23-001').attemptCount,
      1,
    );
    expect(
      restored.ieltsProgress(IeltsPracticeMode.part3, 'p23-001').attemptCount,
      1,
    );
  });

  test(
    'full mock still prioritizes a topic with one unfinished bound part',
    () async {
      final controller = PreparationController(
        client: _EmptyCatalogClient(),
        ieltsQuestionBankClient: _FixtureQuestionBankClient(),
        ieltsHistoryStore: MemoryIeltsPracticeHistoryStore(),
      );
      addTearDown(controller.dispose);
      await controller.activateAccount('account-1');
      await controller.loadIeltsQuestionBankIfNeeded();

      const partlyFinished = IeltsPracticeSelection(
        mode: IeltsPracticeMode.part2,
        topicGroupId: 'p23-001',
      );
      await controller.beginIeltsSession('session-partly', partlyFinished);
      await controller.markIeltsPartCompleted(
        'session-partly',
        IeltsPracticeMode.part2,
      );

      const fullyFinished = IeltsPracticeSelection(
        mode: IeltsPracticeMode.part2,
        topicGroupId: 'p23-002',
      );
      await controller.beginIeltsSession('session-fully', fullyFinished);
      await controller.markIeltsPartCompleted(
        'session-fully',
        IeltsPracticeMode.part2,
      );
      await controller.markIeltsPartStarted(
        'session-fully',
        IeltsPracticeMode.part3,
      );
      await controller.markIeltsPartCompleted(
        'session-fully',
        IeltsPracticeMode.part3,
      );

      for (var index = 0; index < 32; index++) {
        expect(controller.randomFullMockSelection()?.topicGroupId, 'p23-001');
      }
    },
  );
}

final class _FixtureQuestionBankClient implements SceneQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getIeltsQuestionBank() async => _bank;
}

final class _EmptyCatalogClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() async => const <SceneDefinition>[];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }
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
  topicGroups: const [
    IeltsTopicGroup(
      id: 'p23-001',
      title: '语言学习',
      release: 'new',
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
      cueCard: IeltsCueCard(
        prompt: 'Describe an environmental law',
        points: ['What it is', 'Where', 'Who', 'And explain the effect'],
      ),
      part3Questions: ['Q1', 'Q2', 'Q3', 'Q4', 'Q5'],
      supplementedQuestionCount: 0,
    ),
  ],
);
