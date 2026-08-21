import 'dart:convert';

import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';

final class EvaluationReportDecodeException implements Exception {
  const EvaluationReportDecodeException();

  @override
  String toString() => 'EvaluationReportDecodeException';
}

EvaluationReport decodeEvaluationReport(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'report_id',
      'evaluation_id',
      'practice_session_id',
      'report',
      'created_at',
    },
  );
  final formal = _exactObject(
    root['report'],
    required: const {
      'schema_version',
      'scene_type',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'scoreability_status',
      'summary',
      'questions',
      'dimensions',
      'priority_actions',
    },
  );
  if (formal['schema_version'] != 'evaluation-report/v2') {
    throw const EvaluationReportDecodeException();
  }
  final id = _uuid(root['report_id']);
  final evaluationId = _uuid(root['evaluation_id']);
  final practiceSessionId = _uuid(root['practice_session_id']);
  final sceneType = _sceneType(formal['scene_type']);
  final practiceExperience = _identifier(formal['practice_experience']);
  final sceneCategory = _identifier(formal['scene_category']);
  final practiceMode = _identifier(formal['practice_mode']);
  if (!_validPracticeContext(
    sceneType: sceneType,
    practiceExperience: practiceExperience,
    sceneCategory: sceneCategory,
    practiceMode: practiceMode,
  )) {
    throw const EvaluationReportDecodeException();
  }
  final scoreability = _scoreability(formal['scoreability_status']);
  final summary = _text(formal['summary'], maximumBytes: 2048);
  final questions = _questions(formal['questions']);
  final answerTurnIds = questions
      .map((question) => question.answer?.turnId)
      .nonNulls
      .toSet();
  final findings = <String>{};
  final dimensions = _dimensions(
    formal['dimensions'],
    scoreability: scoreability,
    findingIds: findings,
    answerTurnIds: answerTurnIds,
  );
  final priorityActions = _priorityActions(
    formal['priority_actions'],
    dimensions: dimensions,
    findingIds: findings,
  );
  return EvaluationReport(
    id: id,
    evaluationId: evaluationId,
    practiceSessionId: practiceSessionId,
    sceneType: sceneType,
    practiceExperience: practiceExperience,
    sceneCategory: sceneCategory,
    practiceMode: practiceMode,
    scoreability: scoreability,
    summary: summary,
    questions: questions,
    dimensions: dimensions,
    priorityActions: priorityActions,
    createdAt: _dateTime(root['created_at']),
  );
}

List<EvaluationReportQuestion> _questions(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 128) {
    throw const EvaluationReportDecodeException();
  }
  final ids = <String>{};
  final positions = <int>{};
  final answerTurnIds = <String>{};
  final result = value
      .map((item) {
        final root = _exactObject(
          item,
          required: const {'question_id', 'position', 'text', 'answer'},
          optional: const {'parent_question_id'},
        );
        final id = _uuid(root['question_id']);
        final position = root['position'];
        if (position is! int ||
            position < 1 ||
            !ids.add(id) ||
            !positions.add(position)) {
          throw const EvaluationReportDecodeException();
        }
        final parentQuestionId = root.containsKey('parent_question_id')
            ? _uuid(root['parent_question_id'])
            : null;
        final rawAnswer = root['answer'];
        EvaluationReportAnswer? answer;
        if (rawAnswer != null) {
          final source = _exactObject(
            rawAnswer,
            required: const {'turn_id', 'transcript'},
          );
          final turnId = _uuid(source['turn_id']);
          if (!answerTurnIds.add(turnId)) {
            throw const EvaluationReportDecodeException();
          }
          answer = EvaluationReportAnswer(
            turnId: turnId,
            transcript: _text(source['transcript'], maximumBytes: 65536),
          );
        }
        return EvaluationReportQuestion(
          id: id,
          position: position,
          parentQuestionId: parentQuestionId,
          text: _text(root['text'], maximumBytes: 16384),
          answer: answer,
        );
      })
      .toList(growable: false);
  for (final question in result) {
    if (question.parentQuestionId == question.id ||
        (question.parentQuestionId != null &&
            !ids.contains(question.parentQuestionId))) {
      throw const EvaluationReportDecodeException();
    }
  }
  return List<EvaluationReportQuestion>.unmodifiable(result);
}

List<EvaluationReportDimension> _dimensions(
  Object? value, {
  required EvaluationReportScoreability scoreability,
  required Set<String> findingIds,
  required Set<String> answerTurnIds,
}) {
  if (value is! List<Object?> || value.isEmpty || value.length > 16) {
    throw const EvaluationReportDecodeException();
  }
  final keys = <String>{};
  return List<EvaluationReportDimension>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {
          'key',
          'score',
          'scale',
          'coverage',
          'confidence',
          'reason_codes',
          'evidence_ref_ids',
          'strengths',
          'improvements',
          'recommended_examples',
        },
      );
      final key = _identifier(root['key']);
      if (!keys.add(key)) {
        throw const EvaluationReportDecodeException();
      }
      final scale = _scale(root['scale']);
      final rawScore = root['score'];
      final score = rawScore == null
          ? null
          : _number(rawScore, minimum: 0, maximum: _scoreMaximum(scale));
      if (scoreability == EvaluationReportScoreability.insufficient &&
          score != null) {
        throw const EvaluationReportDecodeException();
      }
      return EvaluationReportDimension(
        key: key,
        score: score,
        scale: scale,
        coverage: _number(root['coverage'], minimum: 0, maximum: 1),
        confidence: _number(root['confidence'], minimum: 0, maximum: 1),
        reasonCodes: _identifiers(root['reason_codes'], maximumItems: 8),
        evidenceRefIds: _uuids(root['evidence_ref_ids'], maximumItems: 64),
        strengths: _findings(root['strengths'], findingIds, answerTurnIds),
        improvements: _findings(
          root['improvements'],
          findingIds,
          answerTurnIds,
        ),
        recommendedExamples: _findings(
          root['recommended_examples'],
          findingIds,
          answerTurnIds,
        ),
      );
    }),
  );
}

List<EvaluationReportFinding> _findings(
  Object? value,
  Set<String> findingIds,
  Set<String> answerTurnIds,
) {
  if (value is! List<Object?> || value.length > 5) {
    throw const EvaluationReportDecodeException();
  }
  return List<EvaluationReportFinding>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {'finding_id', 'message', 'evidence'},
        optional: const {'suggestion'},
      );
      final id = _identifier(root['finding_id']);
      if (!findingIds.add(id)) {
        throw const EvaluationReportDecodeException();
      }
      final rawEvidence = root['evidence'];
      if (rawEvidence is! List<Object?> || rawEvidence.length > 8) {
        throw const EvaluationReportDecodeException();
      }
      final evidence = List<EvaluationReportEvidence>.unmodifiable(
        rawEvidence.map((entry) {
          final source = _exactObject(
            entry,
            required: const {
              'evidence_ref_id',
              'turn_id',
              'start_utf8_byte',
              'end_utf8_byte',
              'original_excerpt',
            },
          );
          final start = source['start_utf8_byte'];
          final end = source['end_utf8_byte'];
          if (start is! int || end is! int || start < 0 || end <= start) {
            throw const EvaluationReportDecodeException();
          }
          final turnId = _uuid(source['turn_id']);
          if (!answerTurnIds.contains(turnId)) {
            throw const EvaluationReportDecodeException();
          }
          return EvaluationReportEvidence(
            evidenceRefId: _uuid(source['evidence_ref_id']),
            turnId: turnId,
            startUtf8Byte: start,
            endUtf8Byte: end,
            originalExcerpt: _text(
              source['original_excerpt'],
              maximumBytes: 16384,
            ),
          );
        }),
      );
      return EvaluationReportFinding(
        id: id,
        message: _text(root['message'], maximumBytes: 2048),
        suggestion: root.containsKey('suggestion')
            ? _text(root['suggestion'], maximumBytes: 2048)
            : null,
        evidence: evidence,
      );
    }),
  );
}

List<EvaluationReportPriorityAction> _priorityActions(
  Object? value, {
  required List<EvaluationReportDimension> dimensions,
  required Set<String> findingIds,
}) {
  if (value is! List<Object?> || value.length > 5) {
    throw const EvaluationReportDecodeException();
  }
  final dimensionKeys = dimensions.map((item) => item.key).toSet();
  final actions = <String>{};
  return List<EvaluationReportPriorityAction>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {'dimension_key', 'finding_id'},
      );
      final dimensionKey = _identifier(root['dimension_key']);
      final findingId = _identifier(root['finding_id']);
      if (!dimensionKeys.contains(dimensionKey) ||
          !findingIds.contains(findingId) ||
          !actions.add('$dimensionKey\u0000$findingId')) {
        throw const EvaluationReportDecodeException();
      }
      return EvaluationReportPriorityAction(
        dimensionKey: dimensionKey,
        findingId: findingId,
      );
    }),
  );
}

Map<String, Object?> _exactObject(
  Object? value, {
  Set<String> required = const {},
  Set<String> optional = const {},
  bool allowUnknown = false,
}) {
  if (value is! Map<String, Object?> ||
      !value.keys.toSet().containsAll(required) ||
      (!allowUnknown &&
          value.keys.any(
            (key) => !required.contains(key) && !optional.contains(key),
          ))) {
    throw const EvaluationReportDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String || !_uuidPattern.hasMatch(value)) {
    throw const EvaluationReportDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String || !_identifierPattern.hasMatch(value)) {
    throw const EvaluationReportDecodeException();
  }
  return value;
}

String _text(Object? value, {required int maximumBytes}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maximumBytes) {
    throw const EvaluationReportDecodeException();
  }
  return value;
}

List<String> _identifiers(Object? value, {required int maximumItems}) {
  if (value is! List<Object?> || value.length > maximumItems) {
    throw const EvaluationReportDecodeException();
  }
  final seen = <String>{};
  final result = value.map(_identifier).toList(growable: false);
  if (result.any((item) => !seen.add(item))) {
    throw const EvaluationReportDecodeException();
  }
  return List<String>.unmodifiable(result);
}

List<String> _uuids(Object? value, {required int maximumItems}) {
  if (value is! List<Object?> || value.length > maximumItems) {
    throw const EvaluationReportDecodeException();
  }
  final seen = <String>{};
  final result = value.map(_uuid).toList(growable: false);
  if (result.any((item) => !seen.add(item))) {
    throw const EvaluationReportDecodeException();
  }
  return List<String>.unmodifiable(result);
}

double _number(
  Object? value, {
  required double minimum,
  required double maximum,
}) {
  if (value is! num) {
    throw const EvaluationReportDecodeException();
  }
  final result = value.toDouble();
  if (!result.isFinite || result < minimum || result > maximum) {
    throw const EvaluationReportDecodeException();
  }
  return result;
}

double _scoreMaximum(EvaluationReportScoreScale scale) => switch (scale) {
  EvaluationReportScoreScale.percentage100 => 100,
  EvaluationReportScoreScale.ieltsBand => 9,
};

EvaluationReportSceneType _sceneType(Object? value) => switch (value) {
  'IELTS_SPEAKING' => EvaluationReportSceneType.ieltsSpeaking,
  'INTERVIEW' => EvaluationReportSceneType.interview,
  'OVERSEAS_DAILY_LIFE' => EvaluationReportSceneType.overseasDailyLife,
  'OVERSEAS_WORKPLACE' => EvaluationReportSceneType.overseasWorkplace,
  _ => throw const EvaluationReportDecodeException(),
};

bool _validPracticeContext({
  required EvaluationReportSceneType sceneType,
  required String practiceExperience,
  required String sceneCategory,
  required String practiceMode,
}) => switch (sceneType) {
  EvaluationReportSceneType.ieltsSpeaking =>
    practiceExperience == 'IELTS_SPEAKING' &&
        sceneCategory == 'IELTS_SPEAKING' &&
        const {
          'FULL_MOCK',
          'PART_1',
          'PART_2',
          'PART_3',
        }.contains(practiceMode),
  EvaluationReportSceneType.interview =>
    practiceExperience == 'INTERVIEW' &&
        const {
          'INTERVIEW_RECRUITER',
          'INTERVIEW_BEHAVIORAL',
          'INTERVIEW_PROFESSIONAL',
          'INTERVIEW_HIRING_MANAGER',
          'INTERVIEW_CUSTOM',
        }.contains(sceneCategory) &&
        const {'FULL_SIMULATION', 'FOCUS'}.contains(practiceMode),
  EvaluationReportSceneType.overseasDailyLife =>
    practiceExperience == 'LIFE_AND_TRAVEL' &&
        const {'LIFE_TRAVEL', 'LIFE_DAILY'}.contains(sceneCategory) &&
        const {'FULL_SIMULATION', 'FOCUS'}.contains(practiceMode),
  EvaluationReportSceneType.overseasWorkplace =>
    practiceExperience == 'WORKPLACE' &&
        sceneCategory == 'WORKPLACE_GENERAL' &&
        const {'FULL_SIMULATION', 'FOCUS'}.contains(practiceMode),
};

EvaluationReportScoreability _scoreability(Object? value) => switch (value) {
  'PROVISIONAL' => EvaluationReportScoreability.provisional,
  'INSUFFICIENT' => EvaluationReportScoreability.insufficient,
  _ => throw const EvaluationReportDecodeException(),
};

EvaluationReportScoreScale _scale(Object? value) => switch (value) {
  'PERCENTAGE_100' => EvaluationReportScoreScale.percentage100,
  'IELTS_BAND_9' => EvaluationReportScoreScale.ieltsBand,
  _ => throw const EvaluationReportDecodeException(),
};

DateTime _dateTime(Object? value) {
  if (value is! String || value.length > 64 || value.contains('\u0000')) {
    throw const EvaluationReportDecodeException();
  }
  final result = DateTime.tryParse(value);
  if (result == null || !value.contains(RegExp(r'(?:Z|[+-]\d\d:\d\d)$'))) {
    throw const EvaluationReportDecodeException();
  }
  return result.toUtc();
}

final _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
);
final _identifierPattern = RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$');
