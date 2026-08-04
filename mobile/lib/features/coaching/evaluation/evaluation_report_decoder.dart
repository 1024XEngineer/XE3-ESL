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
      'evaluation_revision_id',
      'practice_session_id',
      'revision',
      'schema_version',
      'scene_type',
      'scene_model',
      'scoreability_status',
      'summary',
      'dimensions',
      'priority_actions',
      'detail_schema',
      'detail',
      'created_at',
    },
  );
  if (root['schema_version'] != 'evaluation-report/v1') {
    throw const EvaluationReportDecodeException();
  }
  final id = _uuid(root['report_id']);
  final evaluationId = _uuid(root['evaluation_id']);
  final evaluationRevisionId = _uuid(root['evaluation_revision_id']);
  final practiceSessionId = _identifier(root['practice_session_id']);
  final revision = root['revision'];
  if (revision is! int || revision < 1) {
    throw const EvaluationReportDecodeException();
  }
  final sceneType = _sceneType(root['scene_type']);
  final sceneModel = _version(root['scene_model']);
  final scoreability = _scoreability(root['scoreability_status']);
  final summary = _text(root['summary'], maximumBytes: 2048);
  final findings = <String>{};
  final dimensions = _dimensions(
    root['dimensions'],
    scoreability: scoreability,
    findingIds: findings,
  );
  final priorityActions = _priorityActions(
    root['priority_actions'],
    dimensions: dimensions,
    findingIds: findings,
  );
  final detailSchema = _version(root['detail_schema']);
  final detail = _exactObject(root['detail'], allowUnknown: true);
  if (utf8.encode(jsonEncode(detail)).length > 256 * 1024) {
    throw const EvaluationReportDecodeException();
  }
  return EvaluationReport(
    id: id,
    evaluationId: evaluationId,
    evaluationRevisionId: evaluationRevisionId,
    practiceSessionId: practiceSessionId,
    revision: revision,
    sceneType: sceneType,
    sceneModel: sceneModel,
    scoreability: scoreability,
    summary: summary,
    dimensions: dimensions,
    priorityActions: priorityActions,
    detailSchema: detailSchema,
    detail: Map<String, Object?>.unmodifiable(detail),
    createdAt: _dateTime(root['created_at']),
  );
}

List<EvaluationReportDimension> _dimensions(
  Object? value, {
  required EvaluationReportScoreability scoreability,
  required Set<String> findingIds,
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
          'scale',
          'coverage',
          'confidence',
          'reason_codes',
          'evidence_ref_ids',
          'strengths',
          'improvements',
          'recommended_examples',
        },
        optional: const {'score'},
      );
      final key = _version(root['key']);
      if (!keys.add(key)) {
        throw const EvaluationReportDecodeException();
      }
      final scale = _scale(root['scale']);
      final score = root.containsKey('score')
          ? _number(root['score'], minimum: 0, maximum: _scoreMaximum(scale))
          : null;
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
        reasonCodes: _versions(root['reason_codes'], maximumItems: 16),
        evidenceRefIds: _identifiers(
          root['evidence_ref_ids'],
          maximumItems: 64,
        ),
        strengths: _findings(root['strengths'], findingIds),
        improvements: _findings(root['improvements'], findingIds),
        recommendedExamples: _findings(
          root['recommended_examples'],
          findingIds,
        ),
      );
    }),
  );
}

List<EvaluationReportFinding> _findings(Object? value, Set<String> findingIds) {
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
      final id = _version(root['finding_id']);
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
          return EvaluationReportEvidence(
            evidenceRefId: _identifier(source['evidence_ref_id']),
            turnId: _identifier(source['turn_id']),
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
      final dimensionKey = _version(root['dimension_key']);
      final findingId = _version(root['finding_id']);
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

String _version(Object? value) {
  if (value is! String || !_versionPattern.hasMatch(value)) {
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

List<String> _versions(Object? value, {required int maximumItems}) {
  if (value is! List<Object?> || value.length > maximumItems) {
    throw const EvaluationReportDecodeException();
  }
  final seen = <String>{};
  final result = value.map(_version).toList(growable: false);
  if (result.any((item) => !seen.add(item))) {
    throw const EvaluationReportDecodeException();
  }
  return List<String>.unmodifiable(result);
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

EvaluationReportScoreability _scoreability(Object? value) => switch (value) {
  'PROVISIONAL' => EvaluationReportScoreability.provisional,
  'INSUFFICIENT' => EvaluationReportScoreability.insufficient,
  _ => throw const EvaluationReportDecodeException(),
};

EvaluationReportScoreScale _scale(Object? value) => switch (value) {
  'PERCENTAGE_100' => EvaluationReportScoreScale.percentage100,
  'IELTS_BAND' => EvaluationReportScoreScale.ieltsBand,
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
final _identifierPattern = RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$');
final _versionPattern = RegExp(r'^[A-Za-z][A-Za-z0-9._:/-]{0,159}$');
