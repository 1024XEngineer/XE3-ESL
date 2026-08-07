import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
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
  });
}

final class _FixtureQuestionBankClient implements IeltsQuestionBankClient {
  @override
  Future<IeltsQuestionBank> getQuestionBank() async => _bank;
}

final _bank = IeltsQuestionBank(
  bankId: 'ielts-bank-1',
  season: '2026-05-08',
  seasonLabel: '5–8 月题库',
  seasonStart: DateTime.utc(2026, 5),
  seasonEnd: DateTime.utc(2026, 8, 31),
  sourceCutoff: DateTime.utc(2026, 6, 18),
  filters: const IeltsCatalogFilters(
    releases: [IeltsFilterOption(code: 'new', label: '本季新增')],
    parts: [
      IeltsFilterOption(code: 'PART_1', label: 'Part 1'),
      IeltsFilterOption(code: 'PART_2', label: 'Part 2'),
      IeltsFilterOption(code: 'PART_3', label: 'Part 3'),
    ],
    topicTags: [IeltsFilterOption(code: 'daily_life', label: '日常生活')],
    cueCardTypes: [IeltsFilterOption(code: 'thing', label: '事物')],
  ),
  part1Topics: const [
    IeltsPart1PracticeTopic(
      id: 'p1-topic-001',
      titleZh: '家乡',
      titleEn: 'Hometown',
      releaseStatus: 'carry_over',
      tagCodes: ['daily_life'],
      questions: ['Where is your hometown?', 'Do you like it?'],
    ),
  ],
  topicGroups: const [
    IeltsTopicGroup(
      id: 'p23-001',
      title: '语言学习',
      releaseStatus: 'new',
      cueCardType: 'thing',
      tagCodes: ['daily_life'],
      cueCard: IeltsCueCard(
        prompt: 'Describe a language',
        points: ['What', 'Why', 'How', 'Benefit'],
      ),
      part3Questions: ['Q1', 'Q2'],
    ),
    IeltsTopicGroup(
      id: 'p23-002',
      title: '环境保护',
      releaseStatus: 'carry_over',
      cueCardType: 'experience',
      tagCodes: ['daily_life'],
      cueCard: IeltsCueCard(
        prompt: 'Describe a law',
        points: ['What', 'Where', 'Who', 'Effect'],
      ),
      part3Questions: ['Q1'],
    ),
  ],
);
