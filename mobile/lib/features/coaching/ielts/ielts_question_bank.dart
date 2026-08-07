import 'package:speakup/features/coaching/scene/scene.dart';

final class IeltsFilterOption {
  const IeltsFilterOption({required this.code, required this.label});

  final String code;
  final String label;
}

final class IeltsCatalogFilters {
  const IeltsCatalogFilters({
    required this.releases,
    required this.parts,
    required this.topicTags,
    required this.cueCardTypes,
  });

  final List<IeltsFilterOption> releases;
  final List<IeltsFilterOption> parts;
  final List<IeltsFilterOption> topicTags;
  final List<IeltsFilterOption> cueCardTypes;
}

final class IeltsQuestionBank {
  const IeltsQuestionBank({
    required this.bankId,
    required this.season,
    required this.seasonLabel,
    required this.seasonStart,
    required this.seasonEnd,
    required this.sourceCutoff,
    required this.filters,
    required this.part1Topics,
    required this.topicGroups,
  });

  final String bankId;
  final String season;
  final String seasonLabel;
  final DateTime seasonStart;
  final DateTime seasonEnd;
  final DateTime sourceCutoff;
  final IeltsCatalogFilters filters;
  final List<IeltsPart1PracticeTopic> part1Topics;
  final List<IeltsTopicGroup> topicGroups;
}

final class IeltsPart1PracticeTopic {
  const IeltsPart1PracticeTopic({
    required this.id,
    required this.titleZh,
    required this.titleEn,
    required this.releaseStatus,
    required this.tagCodes,
    required this.questions,
  });

  final String id;
  final String titleZh;
  final String titleEn;
  final String releaseStatus;
  final List<String> tagCodes;
  final List<String> questions;
}

final class IeltsTopicGroup {
  const IeltsTopicGroup({
    required this.id,
    required this.title,
    required this.releaseStatus,
    required this.cueCardType,
    required this.tagCodes,
    required this.cueCard,
    required this.part3Questions,
  });

  final String id;
  final String title;
  final String releaseStatus;
  final String cueCardType;
  final List<String> tagCodes;
  final IeltsCueCard cueCard;
  final List<String> part3Questions;
}

final class IeltsCueCard {
  const IeltsCueCard({required this.prompt, required this.points});

  final String prompt;
  final List<String> points;
}

final class IeltsPracticeSelection {
  const IeltsPracticeSelection({this.part1SetId, this.topicGroupId});

  final String? part1SetId;
  final String? topicGroupId;

  bool isValidForMode(PracticeMode mode) => switch (mode) {
    PracticeMode.fullMock => part1SetId != null && topicGroupId != null,
    PracticeMode.part1 => part1SetId != null && topicGroupId == null,
    PracticeMode.part2 ||
    PracticeMode.part3 => part1SetId == null && topicGroupId != null,
    _ => false,
  };

  Map<String, Object> toJson() => <String, Object>{
    'part_1_set_id': ?part1SetId,
    'topic_group_id': ?topicGroupId,
  };

  @override
  bool operator ==(Object other) =>
      other is IeltsPracticeSelection &&
      other.part1SetId == part1SetId &&
      other.topicGroupId == topicGroupId;

  @override
  int get hashCode => Object.hash(part1SetId, topicGroupId);
}
