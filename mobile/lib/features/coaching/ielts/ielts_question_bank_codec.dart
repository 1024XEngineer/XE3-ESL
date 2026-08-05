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
      'source_cutoff',
      'part1_sets',
      'part1_topics',
      'topic_groups',
    },
  );
  final sourceCutoff = DateTime.tryParse(_string(root['source_cutoff']));
  final rawPart1Sets = root['part1_sets'];
  final rawPart1Topics = root['part1_topics'];
  final rawTopicGroups = root['topic_groups'];
  if (root['schema_version'] != 2 ||
      sourceCutoff == null ||
      rawPart1Sets is! List<Object?> ||
      rawPart1Sets.length != 38 ||
      rawPart1Topics is! List<Object?> ||
      rawPart1Topics.length != 38 ||
      rawTopicGroups is! List<Object?> ||
      rawTopicGroups.length != 56) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final part1Ids = <String>{};
  final part1Sets = rawPart1Sets
      .map((raw) {
        final set = _part1Set(raw);
        if (!part1Ids.add(set.id)) {
          throw const IeltsQuestionBankWireFormatException();
        }
        return set;
      })
      .toList(growable: false);
  final part1TopicIds = <String>{};
  final part1Topics = rawPart1Topics
      .map((raw) {
        final topic = _part1PracticeTopic(raw);
        if (!part1TopicIds.add(topic.id)) {
          throw const IeltsQuestionBankWireFormatException();
        }
        return topic;
      })
      .toList(growable: false);
  final groupIds = <String>{};
  final topicGroups = rawTopicGroups
      .map((raw) {
        final group = _topicGroup(raw);
        if (!groupIds.add(group.id)) {
          throw const IeltsQuestionBankWireFormatException();
        }
        return group;
      })
      .toList(growable: false);
  return IeltsQuestionBank(
    bankId: _resourceId(root['bank_id']),
    season: _string(root['season']),
    sourceCutoff: sourceCutoff.toUtc(),
    part1Sets: List.unmodifiable(part1Sets),
    part1Topics: List.unmodifiable(part1Topics),
    topicGroups: List.unmodifiable(topicGroups),
  );
}

IeltsPart1PracticeTopic _part1PracticeTopic(Object? value) {
  final object = _object(
    value,
    required: const {
      'id',
      'title_zh',
      'title_en',
      'release',
      'category',
      'questions',
      'published',
    },
  );
  final release = _string(object['release'], maximumBytes: 32);
  final category = IeltsTopicCategory.fromWireName(
    _string(object['category'], maximumBytes: 16),
  );
  final questions = _stringList(object['questions'], maximumItemBytes: 1024);
  if (!const {'new', 'carry_over', 'evergreen'}.contains(release) ||
      category == null ||
      object['published'] != true ||
      questions.length < 2) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsPart1PracticeTopic(
    id: _resourceId(object['id']),
    titleZh: _string(object['title_zh']),
    titleEn: _string(object['title_en']),
    release: release,
    category: category,
    questions: questions,
  );
}

IeltsPart1Set _part1Set(Object? value) {
  final object = _object(
    value,
    required: const {'id', 'title', 'topics', 'question_count', 'published'},
  );
  final rawTopics = object['topics'];
  if (rawTopics is! List<Object?> ||
      rawTopics.length != 3 ||
      object['question_count'] != 8 ||
      object['published'] != true) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final topics = rawTopics.map(_part1Topic).toList(growable: false);
  if (topics.fold<int>(0, (total, topic) => total + topic.questions.length) !=
      8) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsPart1Set(
    id: _resourceId(object['id']),
    title: _string(object['title']),
    topics: List.unmodifiable(topics),
    questionCount: 8,
  );
}

IeltsPart1Topic _part1Topic(Object? value) {
  final object = _object(
    value,
    required: const {'title', 'release', 'questions'},
  );
  final release = _string(object['release'], maximumBytes: 32);
  if (!const {'new', 'carry_over', 'evergreen'}.contains(release)) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final questions = _stringList(object['questions'], maximumItemBytes: 1024);
  if (questions.length < 2) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsPart1Topic(
    title: _string(object['title']),
    release: release,
    questions: questions,
  );
}

IeltsTopicGroup _topicGroup(Object? value) {
  final object = _object(
    value,
    required: const {
      'id',
      'title_zh',
      'release',
      'region',
      'category',
      'part2',
      'part3_questions',
      'published',
      'supplemented_question_count',
    },
  );
  final release = _string(object['release'], maximumBytes: 32);
  final category = IeltsTopicCategory.fromWireName(
    _string(object['category'], maximumBytes: 16),
  );
  final supplemented = object['supplemented_question_count'];
  if (!const {'new', 'carry_over'}.contains(release) ||
      object['region'] != 'mainland' ||
      category == null ||
      object['published'] != true ||
      supplemented is! int ||
      supplemented < 0 ||
      supplemented > 5) {
    throw const IeltsQuestionBankWireFormatException();
  }
  final cueObject = _object(
    object['part2'],
    required: const {'prompt', 'points'},
  );
  final points = _stringList(cueObject['points'], maximumItemBytes: 1024);
  final questions = _stringList(
    object['part3_questions'],
    maximumItemBytes: 1024,
  );
  if (points.length < 3 || questions.isEmpty || questions.length > 6) {
    throw const IeltsQuestionBankWireFormatException();
  }
  return IeltsTopicGroup(
    id: _resourceId(object['id']),
    title: _string(object['title_zh']),
    release: release,
    category: category,
    cueCard: IeltsCueCard(prompt: _string(cueObject['prompt']), points: points),
    part3Questions: questions,
    supplementedQuestionCount: supplemented,
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
  final result = <String>[];
  for (final item in value) {
    final text = _string(item, maximumBytes: maximumItemBytes);
    if (!seen.add(text)) {
      throw const IeltsQuestionBankWireFormatException();
    }
    result.add(text);
  }
  return List.unmodifiable(result);
}
