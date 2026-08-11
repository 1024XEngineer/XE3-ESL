import 'dart:convert';

import 'package:speakup/features/coaching/review/ielts_practice_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';

final class IeltsPracticeReportDecodeException implements Exception {
  const IeltsPracticeReportDecodeException();

  @override
  String toString() => 'IeltsPracticeReportDecodeException';
}

IeltsPracticeReportDetail decodeIeltsPracticeReportDetail(Object? value) {
  final root = _exactObject(
    value,
    required: const <String>{
      'schema_version',
      'report_scope',
      'available_sections',
      'questions',
      'section_reviews',
    },
  );
  if (root['schema_version'] != 'ielts-speaking-practice-report/v1') {
    throw const IeltsPracticeReportDecodeException();
  }
  final scope = _scope(root['report_scope']);
  if (scope == PracticeReportScope.fullMock) {
    throw const IeltsPracticeReportDecodeException();
  }
  final rawSections = root['available_sections'];
  if (rawSections is! List<Object?> || rawSections.isEmpty) {
    throw const IeltsPracticeReportDecodeException();
  }
  final sections = rawSections.map(_part).toList(growable: false);
  if (sections.toSet().length != sections.length ||
      !_sameValues(sections, _expectedParts(scope))) {
    throw const IeltsPracticeReportDecodeException();
  }

  final rawQuestions = root['questions'];
  if (rawQuestions is! List<Object?> ||
      rawQuestions.isEmpty ||
      rawQuestions.length > 64) {
    throw const IeltsPracticeReportDecodeException();
  }
  final questions = rawQuestions.map(_question).toList(growable: false);
  final questionIds = <String>{};
  final turnIds = <String>{};
  final evidenceRefIds = <String>{};
  for (var index = 0; index < questions.length; index++) {
    final question = questions[index];
    if (!sections.contains(question.partId) ||
        !questionIds.add(question.questionId) ||
        question.index != index + 1 ||
        (question.responseTurnId != null &&
            !turnIds.add(question.responseTurnId!)) ||
        question.evidenceRefIds.any((refId) => !evidenceRefIds.add(refId))) {
      throw const IeltsPracticeReportDecodeException();
    }
  }
  if (!_hasCanonicalPartSequence(questions, sections)) {
    throw const IeltsPracticeReportDecodeException();
  }

  final rawReviews = root['section_reviews'];
  if (rawReviews is! List<Object?> || rawReviews.length != sections.length) {
    throw const IeltsPracticeReportDecodeException();
  }
  final reviews = rawReviews.map(_sectionReview).toList(growable: false);
  if (!_sameValues(
    reviews.map((review) => review.partId).toList(growable: false),
    sections,
  )) {
    throw const IeltsPracticeReportDecodeException();
  }
  for (final review in reviews) {
    final partQuestions = questions
        .where((question) => question.partId == review.partId)
        .toList(growable: false);
    if (!_sameValues(
          review.questionIndexes,
          partQuestions.map((question) => question.index).toList(),
        ) ||
        !_sameValues(review.evidenceRefIds, <String>[
          for (final question in partQuestions) ...question.evidenceRefIds,
        ])) {
      throw const IeltsPracticeReportDecodeException();
    }
  }

  return IeltsPracticeReportDetail(
    reportScope: scope,
    availableSections: List<IeltsSpeakingPartId>.unmodifiable(sections),
    questions: List<IeltsPracticeReportQuestion>.unmodifiable(questions),
    sectionReviews: List<IeltsPracticeSectionReview>.unmodifiable(reviews),
  );
}

IeltsPracticeReportQuestion _question(Object? value) {
  final root = _exactObject(
    value,
    required: const <String>{
      'question_id',
      'part_id',
      'index',
      'question_text',
      'evidence_ref_ids',
    },
    optional: const <String>{'confirmed_transcript', 'response_turn_id'},
  );
  final evidenceRefs = _identifiers(root['evidence_ref_ids'], maximum: 1);
  final hasTranscript = root.containsKey('confirmed_transcript');
  final hasTurn = root.containsKey('response_turn_id');
  if (hasTranscript != hasTurn ||
      (hasTurn && evidenceRefs.length != 1) ||
      (!hasTurn && evidenceRefs.isNotEmpty)) {
    throw const IeltsPracticeReportDecodeException();
  }
  return IeltsPracticeReportQuestion(
    questionId: _identifier(root['question_id']),
    partId: _part(root['part_id']),
    index: _positiveInt(root['index']),
    questionText: _text(root['question_text'], maxBytes: 16384),
    confirmedTranscript: hasTranscript
        ? _text(root['confirmed_transcript'], maxBytes: 16384)
        : null,
    responseTurnId: hasTurn ? _identifier(root['response_turn_id']) : null,
    evidenceRefIds: evidenceRefs,
  );
}

IeltsPracticeSectionReview _sectionReview(Object? value) {
  final root = _exactObject(
    value,
    required: const <String>{
      'part_id',
      'question_indexes',
      'evidence_ref_ids',
      'strength_finding_ids',
      'improvement_finding_ids',
      'upgrade_example_finding_ids',
    },
  );
  return IeltsPracticeSectionReview(
    partId: _part(root['part_id']),
    questionIndexes: _positiveInts(root['question_indexes']),
    evidenceRefIds: _identifiers(root['evidence_ref_ids']),
    strengthFindingIds: _findingReferences(root['strength_finding_ids']),
    improvementFindingIds: _findingReferences(root['improvement_finding_ids']),
    upgradeExampleFindingIds: _findingReferences(
      root['upgrade_example_finding_ids'],
    ),
  );
}

List<IeltsSpeakingPartId> _expectedParts(PracticeReportScope scope) =>
    switch (scope) {
      PracticeReportScope.part1 => const <IeltsSpeakingPartId>[
        IeltsSpeakingPartId.part1,
      ],
      PracticeReportScope.part2And3 => const <IeltsSpeakingPartId>[
        IeltsSpeakingPartId.part2,
        IeltsSpeakingPartId.part3,
      ],
      PracticeReportScope.part3 => const <IeltsSpeakingPartId>[
        IeltsSpeakingPartId.part3,
      ],
      PracticeReportScope.fullMock =>
        throw const IeltsPracticeReportDecodeException(),
    };

PracticeReportScope _scope(Object? value) => switch (value) {
  'PART_1' => PracticeReportScope.part1,
  'PART_2_3' => PracticeReportScope.part2And3,
  'PART_3' => PracticeReportScope.part3,
  'FULL_MOCK' => PracticeReportScope.fullMock,
  _ => throw const IeltsPracticeReportDecodeException(),
};

IeltsSpeakingPartId _part(Object? value) => switch (value) {
  'PART_1' => IeltsSpeakingPartId.part1,
  'PART_2' => IeltsSpeakingPartId.part2,
  'PART_3' => IeltsSpeakingPartId.part3,
  _ => throw const IeltsPracticeReportDecodeException(),
};

Map<String, Object?> _exactObject(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw const IeltsPracticeReportDecodeException();
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const IeltsPracticeReportDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.length > 128 ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value)) {
    throw const IeltsPracticeReportDecodeException();
  }
  return value;
}

String _text(Object? value, {required int maxBytes}) {
  if (value is! String ||
      value.trim() != value ||
      value.isEmpty ||
      utf8.encode(value).length > maxBytes) {
    throw const IeltsPracticeReportDecodeException();
  }
  return value;
}

int _positiveInt(Object? value) {
  if (value is! int || value < 1) {
    throw const IeltsPracticeReportDecodeException();
  }
  return value;
}

List<int> _positiveInts(Object? value) {
  if (value is! List<Object?>) {
    throw const IeltsPracticeReportDecodeException();
  }
  final result = value.map(_positiveInt).toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const IeltsPracticeReportDecodeException();
  }
  return result;
}

List<String> _identifiers(Object? value, {int maximum = 64}) {
  if (value is! List<Object?> || value.length > maximum) {
    throw const IeltsPracticeReportDecodeException();
  }
  final result = value.map(_identifier).toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const IeltsPracticeReportDecodeException();
  }
  return result;
}

List<String> _findingReferences(Object? value) {
  if (value is! List<Object?> || value.length > 64) {
    throw const IeltsPracticeReportDecodeException();
  }
  final result = value.map(_findingReference).toList(growable: false);
  if (result.toSet().length != result.length) {
    throw const IeltsPracticeReportDecodeException();
  }
  return result;
}

String _findingReference(Object? value) {
  if (value is! String ||
      value.length > 160 ||
      !RegExp(r'^[A-Za-z][A-Za-z0-9._:/-]*$').hasMatch(value)) {
    throw const IeltsPracticeReportDecodeException();
  }
  return value;
}

bool _sameValues<T>(List<T> left, List<T> right) {
  if (left.length != right.length) return false;
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) return false;
  }
  return true;
}

bool _hasCanonicalPartSequence(
  List<IeltsPracticeReportQuestion> questions,
  List<IeltsSpeakingPartId> sections,
) {
  if (questions.first.partId != sections.first) return false;
  var sectionIndex = 0;
  for (final question in questions.skip(1)) {
    if (question.partId == sections[sectionIndex]) continue;
    sectionIndex++;
    if (sectionIndex >= sections.length ||
        question.partId != sections[sectionIndex]) {
      return false;
    }
  }
  return sectionIndex == sections.length - 1;
}
