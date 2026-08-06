import 'dart:convert';

import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';

final class IeltsQuestionBankWireFormatException implements Exception {
  const IeltsQuestionBankWireFormatException();
}

IeltsQuestionBank decodeIeltsQuestionBank(Object? value) {
  final root = _object(
    value,
    required: const {
      'schema_version',
      'bank_id',
      'season',
      'season_label',
      'season_start',
      'season_end',
      'source_cutoff',
      'filters',
      'part1_topics',
      'topic_groups',
    },
  );
  final seasonStart = DateTime.tryParse(_string(root['season_start']));
  final seasonEnd = DateTime.tryParse(_string(root['season_end']));
  final sourceCutoff = DateTime.tryParse(_string(root['source_cutoff']));
  final rawTopics = root['part1_topics'];
  final rawGroups = root['topic_groups'];
  if (root['schema_version'] != 3 ||
      seasonStart == null ||
      seasonEnd == null ||
      sourceCutoff == null ||
      rawTopics is! List<Object?> ||
      rawTopics.isEmpty ||
      rawGroups is! List<Object?> ||
      rawGroups.isEmpty) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final topicIds = <String>{};
  final topics = rawTopics.map(_part1Topic).toList(growable: false);
  final groupIds = <String>{};
  final groups = rawGroups.map(_topicGroup).toList(growable: false);
  if (topics.any((topic) => !topicIds.add(topic.id)) ||
      groups.any((group) => !groupIds.add(group.id))) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsQuestionBank(
    bankId: _resourceId(root['bank_id']),
    season: _resourceId(root['season']),
    seasonLabel: _string(root['season_label']),
    seasonStart: seasonStart,
    seasonEnd: seasonEnd,
    sourceCutoff: sourceCutoff.toUtc(),
    filters: _filters(root['filters']),
    part1Topics: List.unmodifiable(topics),
    topicGroups: List.unmodifiable(groups),
  );
}

IeltsCatalogFilters _filters(Object? value) {
  final object = _object(
    value,
    required: const {'releases', 'parts', 'topic_tags', 'cue_card_types'},
  );
  return IeltsCatalogFilters(
    releases: _options(object['releases']),
    parts: _options(object['parts']),
    topicTags: _options(object['topic_tags']),
    cueCardTypes: _options(object['cue_card_types']),
  );
}

List<IeltsFilterOption> _options(Object? value) {
  if (value is! List<Object?>) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final codes = <String>{};
  return List.unmodifiable(
    value.map((item) {
      final object = _object(item, required: const {'code', 'label'});
      final option = IeltsFilterOption(
        code: _resourceId(object['code']),
        label: _string(object['label']),
      );
      if (!codes.add(option.code)) {
        throw const IeltsQuestionBankWireFormatException();
      }
      return option;
    }),
  );
}

IeltsPart1PracticeTopic _part1Topic(Object? value) {
  final object = _object(
    value,
    required: const {
      'id',
      'title_zh',
      'title_en',
      'release_status',
      'tag_codes',
      'questions',
    },
  );
  final release = _string(object['release_status'], maximumBytes: 32);
  final tags = _stringList(object['tag_codes']);
  final questions = _stringList(object['questions'], maximumItemBytes: 1024);
  if (!const {'new', 'carry_over', 'evergreen'}.contains(release) ||
      tags.isEmpty ||
      questions.length < 2) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsPart1PracticeTopic(
    id: _resourceId(object['id']),
    titleZh: _string(object['title_zh']),
    titleEn: _string(object['title_en']),
    releaseStatus: release,
    tagCodes: tags,
    questions: questions,
  );
}

IeltsTopicGroup _topicGroup(Object? value) {
  final object = _object(
    value,
    required: const {
      'id',
      'title_zh',
      'release_status',
      'cue_card_type',
      'tag_codes',
      'part2',
      'part3_questions',
    },
  );
  final release = _string(object['release_status'], maximumBytes: 32);
  final cueCardType = _string(object['cue_card_type'], maximumBytes: 16);
  final tags = _stringList(object['tag_codes']);
  final cue = _object(object['part2'], required: const {'prompt', 'points'});
  final points = _stringList(cue['points'], maximumItemBytes: 1024);
  final questions = _stringList(
    object['part3_questions'],
    maximumItemBytes: 1024,
  );
  if (!const {'new', 'carry_over', 'evergreen'}.contains(release) ||
      !const {'person', 'place', 'thing', 'experience'}.contains(cueCardType) ||
      tags.isEmpty ||
      points.length < 3 ||
      questions.isEmpty ||
      questions.length > 6) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsTopicGroup(
    id: _resourceId(object['id']),
    title: _string(object['title_zh']),
    releaseStatus: release,
    cueCardType: cueCardType,
    tagCodes: tags,
    cueCard: IeltsCueCard(prompt: _string(cue['prompt']), points: points),
    part3Questions: questions,
  );
}

Map<String, Object?> _object(Object? value, {required Set<String> required}) {
  if (value is! Map<String, Object?> ||
      value.keys.toSet().length != required.length ||
      !value.keys.toSet().containsAll(required)) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return value;
}

String _resourceId(Object? value) => _string(value, maximumBytes: 128);

String _string(Object? value, {int maximumBytes = 4096}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maximumBytes) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return value;
}

List<String> _stringList(Object? value, {int maximumItemBytes = 128}) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final seen = <String>{};
  final result = value
      .map((item) {
        final text = _string(item, maximumBytes: maximumItemBytes);
        if (!seen.add(text)) throw const IeltsQuestionBankWireFormatException();
        return text;
      })
      .toList(growable: false);
  return List.unmodifiable(result);
}
