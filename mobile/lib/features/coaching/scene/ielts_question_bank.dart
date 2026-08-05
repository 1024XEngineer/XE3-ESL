import 'dart:math';

enum IeltsPracticeMode {
  fullMock('FULL_MOCK'),
  part1('PART_1'),
  part2('PART_2'),
  part3('PART_3');

  const IeltsPracticeMode(this.wireName);

  final String wireName;

  static IeltsPracticeMode? fromWireName(String value) => IeltsPracticeMode
      .values
      .where((mode) => mode.wireName == value)
      .firstOrNull;
}

enum IeltsTopicCategory {
  person('person'),
  place('place'),
  thing('thing'),
  event('event');

  const IeltsTopicCategory(this.wireName);

  final String wireName;

  static IeltsTopicCategory? fromWireName(String value) => IeltsTopicCategory
      .values
      .where((category) => category.wireName == value)
      .firstOrNull;
}

final class IeltsQuestionBank {
  const IeltsQuestionBank({
    required this.bankId,
    required this.season,
    required this.sourceCutoff,
    required this.part1Sets,
    this.part1Topics = const <IeltsPart1PracticeTopic>[],
    required this.topicGroups,
  });

  final String bankId;
  final String season;
  final DateTime sourceCutoff;
  final List<IeltsPart1Set> part1Sets;
  final List<IeltsPart1PracticeTopic> part1Topics;
  final List<IeltsTopicGroup> topicGroups;
}

final class IeltsPart1PracticeTopic {
  const IeltsPart1PracticeTopic({
    required this.id,
    required this.titleZh,
    required this.titleEn,
    required this.release,
    required this.category,
    required this.questions,
  });

  final String id;
  final String titleZh;
  final String titleEn;
  final String release;
  final IeltsTopicCategory category;
  final List<String> questions;
}

final class IeltsPart1Set {
  const IeltsPart1Set({
    required this.id,
    required this.title,
    required this.topics,
    required this.questionCount,
  });

  final String id;
  final String title;
  final List<IeltsPart1Topic> topics;
  final int questionCount;

  String get topicSummary => topics.map((topic) => topic.title).join(' · ');
}

final class IeltsPart1Topic {
  const IeltsPart1Topic({
    required this.title,
    required this.release,
    required this.questions,
  });

  final String title;
  final String release;
  final List<String> questions;
}

final class IeltsTopicGroup {
  const IeltsTopicGroup({
    required this.id,
    required this.title,
    required this.release,
    this.category = IeltsTopicCategory.thing,
    required this.cueCard,
    required this.part3Questions,
    required this.supplementedQuestionCount,
  });

  final String id;
  final String title;
  final String release;
  final IeltsTopicCategory category;
  final IeltsCueCard cueCard;
  final List<String> part3Questions;
  final int supplementedQuestionCount;
}

final class IeltsCueCard {
  const IeltsCueCard({required this.prompt, required this.points});

  final String prompt;
  final List<String> points;
}

final class IeltsPracticeSelection {
  const IeltsPracticeSelection({
    required this.mode,
    this.part1SetId,
    this.topicGroupId,
  });

  final IeltsPracticeMode mode;
  final String? part1SetId;
  final String? topicGroupId;

  bool get isValid => switch (mode) {
    IeltsPracticeMode.fullMock => part1SetId != null && topicGroupId != null,
    IeltsPracticeMode.part1 => part1SetId != null && topicGroupId == null,
    IeltsPracticeMode.part2 ||
    IeltsPracticeMode.part3 => part1SetId == null && topicGroupId != null,
  };

  Map<String, Object> toJson() => <String, Object>{
    'mode': mode.wireName,
    'part_1_set_id': ?part1SetId,
    'topic_group_id': ?topicGroupId,
  };

  @override
  bool operator ==(Object other) =>
      other is IeltsPracticeSelection &&
      other.mode == mode &&
      other.part1SetId == part1SetId &&
      other.topicGroupId == topicGroupId;

  @override
  int get hashCode => Object.hash(mode, part1SetId, topicGroupId);
}

abstract interface class SceneQuestionBankClient {
  Future<IeltsQuestionBank> getIeltsQuestionBank();
}

IeltsPracticeSelection randomIeltsFullMockSelection({
  required IeltsQuestionBank bank,
  required Set<String> completedPart1SetIds,
  required Set<String> completedTopicGroupIds,
  Random? random,
}) {
  final generator = random ?? Random.secure();
  final availablePart1 = bank.part1Sets
      .where((set) => !completedPart1SetIds.contains(set.id))
      .toList(growable: false);
  final availableGroups = bank.topicGroups
      .where((group) => !completedTopicGroupIds.contains(group.id))
      .toList(growable: false);
  final part1Pool = availablePart1.isEmpty ? bank.part1Sets : availablePart1;
  final groupPool = availableGroups.isEmpty
      ? bank.topicGroups
      : availableGroups;
  return IeltsPracticeSelection(
    mode: IeltsPracticeMode.fullMock,
    part1SetId: part1Pool[generator.nextInt(part1Pool.length)].id,
    topicGroupId: groupPool[generator.nextInt(groupPool.length)].id,
  );
}
