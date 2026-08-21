import 'dart:convert';

import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';

final class SpeechFeedbackDecodeException implements Exception {
  const SpeechFeedbackDecodeException();

  @override
  String toString() => 'SpeechFeedbackDecodeException';
}

SpeechFeedback decodeSpeechFeedbackJson(
  String body, {
  required String statusUrl,
}) {
  try {
    if (utf8.encode(body).length > maximumFeedbackResponseBytes) {
      throw const SpeechFeedbackDecodeException();
    }
    return decodeSpeechFeedback(jsonDecode(body), statusUrl: statusUrl);
  } on FormatException {
    throw const SpeechFeedbackDecodeException();
  }
}

SpeechFeedback decodeSpeechFeedback(
  Object? value, {
  required String statusUrl,
}) {
  final statusSource = speechFeedbackStatusSource(statusUrl);
  if (statusSource == null) {
    throw const SpeechFeedbackDecodeException();
  }
  final root = _exactObject(
    value,
    required: const {
      'evaluation_id',
      'kind',
      'source_id',
      'context_id',
      'status',
      'created_at',
      'updated_at',
      'feedback_items',
    },
    optional: const {'result', 'error'},
  );
  final evaluationId = _uuid(root['evaluation_id']);
  final sourceId = _uuid(root['source_id']);
  final contextId = _uuid(root['context_id']);
  final sourceKind = _sourceKind(root['kind']);
  if (sourceKind != statusSource.kind || sourceId != statusSource.sourceId) {
    throw const SpeechFeedbackDecodeException();
  }
  final status = _status(root['status']);
  final createdAt = _dateTime(root['created_at']);
  final updatedAt = _dateTime(root['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const SpeechFeedbackDecodeException();
  }
  final items = _feedbackItems(
    root['feedback_items'],
    evaluationId: evaluationId,
    sourceId: sourceId,
    sourceKind: sourceKind,
  );

  SpeechFeedbackScoreabilityStatus? scoreability;
  String? summary;
  var reasonCodes = const <String>[];
  SpeechFeedbackAcousticAssessment? acoustic;
  SpeechFeedbackStableFailure? failure;
  switch (status) {
    case SpeechFeedbackStatus.queued:
    case SpeechFeedbackStatus.running:
      if (root.containsKey('result') ||
          root.containsKey('error') ||
          items.isNotEmpty) {
        throw const SpeechFeedbackDecodeException();
      }
    case SpeechFeedbackStatus.ready:
      if (!root.containsKey('result') || root.containsKey('error')) {
        throw const SpeechFeedbackDecodeException();
      }
      final result = _speechResult(root['result']);
      scoreability = result.scoreability;
      summary = result.summary;
      reasonCodes = result.reasonCodes;
      acoustic = result.acoustic;
      if ((scoreability == SpeechFeedbackScoreabilityStatus.provisional &&
              items.isEmpty) ||
          (scoreability == SpeechFeedbackScoreabilityStatus.insufficient &&
              items.isNotEmpty)) {
        throw const SpeechFeedbackDecodeException();
      }
    case SpeechFeedbackStatus.failed:
      if (root.containsKey('result') ||
          !root.containsKey('error') ||
          items.isNotEmpty) {
        throw const SpeechFeedbackDecodeException();
      }
      failure = _failure(root['error']);
  }

  return SpeechFeedback(
    evaluationId: evaluationId,
    source: SpeechFeedbackSource(
      kind: sourceKind,
      sourceId: sourceId,
      contextId: contextId,
    ),
    feedbackStatus: status,
    scoreabilityStatus: scoreability,
    summary: summary,
    reasonCodes: reasonCodes,
    items: items,
    acousticAssessment: acoustic,
    stableFailure: failure,
    statusUrl: statusUrl,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

List<SpeechFeedbackItem> _feedbackItems(
  Object? value, {
  required String evaluationId,
  required String sourceId,
  required SpeechFeedbackSourceKind sourceKind,
}) {
  if (value is! List<Object?> || value.length > 32) {
    throw const SpeechFeedbackDecodeException();
  }
  final ids = <String>{};
  final items = <SpeechFeedbackItem>[];
  for (var index = 0; index < value.length; index++) {
    final root = _exactObject(
      value[index],
      required: const {
        'feedback_item_id',
        'evaluation_id',
        'position',
        'category',
        'evidence',
        'recommendation',
        'repractice_mode',
        'created_at',
      },
      optional: const {'severity', 'correction'},
    );
    final itemId = _uuid(root['feedback_item_id']);
    if (!ids.add(itemId) ||
        _uuid(root['evaluation_id']) != evaluationId ||
        _integer(root['position'], minimum: 1, maximum: 32) != index + 1) {
      throw const SpeechFeedbackDecodeException();
    }
    final kind = _itemKind(root['category']);
    final mode = _repracticeMode(root['repractice_mode']);
    final correction = root.containsKey('correction')
        ? _text(root['correction'], maximumBytes: 4096)
        : null;
    if ((kind == SpeechFeedbackItemKind.strength &&
            (correction != null ||
                mode != SpeechFeedbackRepracticeMode.none)) ||
        (kind != SpeechFeedbackItemKind.strength && correction == null) ||
        (sourceKind == SpeechFeedbackSourceKind.agentMessage &&
            mode != SpeechFeedbackRepracticeMode.none)) {
      throw const SpeechFeedbackDecodeException();
    }
    final anchor = _evidence(root['evidence']);
    if (anchor.evidenceRefId != sourceId) {
      throw const SpeechFeedbackDecodeException();
    }
    final item = SpeechFeedbackItem(
      feedbackItemId: itemId,
      evaluationId: evaluationId,
      position: index + 1,
      kind: kind,
      severity: root.containsKey('severity')
          ? _identifier(root['severity'])
          : null,
      anchor: anchor,
      explanation: _text(root['recommendation'], maximumBytes: 4096),
      suggestedText: correction,
      repracticeMode: mode,
      createdAt: _dateTime(root['created_at']),
    );
    if (kind == SpeechFeedbackItemKind.correction &&
        !item.hasLocatableLanguageCorrection) {
      throw const SpeechFeedbackDecodeException();
    }
    items.add(item);
  }
  if (items.any((item) => item.kind == SpeechFeedbackItemKind.strength) &&
      (items.length != 1 ||
          items.single.kind != SpeechFeedbackItemKind.strength)) {
    throw const SpeechFeedbackDecodeException();
  }
  return List<SpeechFeedbackItem>.unmodifiable(items);
}

SpeechFeedbackAnchor _evidence(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'evidence_ref_id',
      'start_utf8_byte',
      'end_utf8_byte',
      'original_excerpt',
    },
  );
  final excerpt = _text(root['original_excerpt'], maximumBytes: 16384);
  final start = _integer(root['start_utf8_byte'], minimum: 0, maximum: 16383);
  final end = _integer(root['end_utf8_byte'], minimum: 1, maximum: 16384);
  if (end <= start || end - start != utf8.encode(excerpt).length) {
    throw const SpeechFeedbackDecodeException();
  }
  return SpeechFeedbackAnchor(
    evidenceRefId: _uuid(root['evidence_ref_id']),
    startUtf8Byte: start,
    endUtf8Byte: end,
    originalExcerpt: excerpt,
  );
}

_SpeechResult _speechResult(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'schema_version',
      'scoreability_status',
      'summary',
      'reason_codes',
      'acoustic',
    },
  );
  if (root['schema_version'] != 'speech-feedback/v1') {
    throw const SpeechFeedbackDecodeException();
  }
  final scoreability = switch (root['scoreability_status']) {
    'PROVISIONAL' => SpeechFeedbackScoreabilityStatus.provisional,
    'INSUFFICIENT' => SpeechFeedbackScoreabilityStatus.insufficient,
    _ => throw const SpeechFeedbackDecodeException(),
  };
  final reasons = _reasonCodes(root['reason_codes']);
  if ((scoreability == SpeechFeedbackScoreabilityStatus.provisional &&
          reasons.isNotEmpty) ||
      (scoreability == SpeechFeedbackScoreabilityStatus.insufficient &&
          reasons.isEmpty)) {
    throw const SpeechFeedbackDecodeException();
  }
  return _SpeechResult(
    scoreability: scoreability,
    summary: _text(root['summary'], maximumBytes: 4096),
    reasonCodes: reasons,
    acoustic: _acoustic(root['acoustic']),
  );
}

SpeechFeedbackAcousticAssessment _acoustic(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const SpeechFeedbackDecodeException();
  }
  switch (value['status']) {
    case 'NOT_ASSESSED':
      final root = _exactObject(value, required: const {'status', 'reason'});
      return SpeechFeedbackAcousticAssessment.notAssessed(
        reason: _identifier(root['reason']),
      );
    case 'ASSESSED':
      final root = _exactObject(
        value,
        required: const {'status', 'pronunciation'},
        optional: const {'fluency', 'integrity', 'speaking_speed_wpm'},
      );
      return SpeechFeedbackAcousticAssessment.assessed(
        pronunciationScore: _score(root['pronunciation']),
        fluencyScore: root.containsKey('fluency')
            ? _score(root['fluency'])
            : null,
        integrityScore: root.containsKey('integrity')
            ? _score(root['integrity'])
            : null,
        speakingSpeedWpm: root.containsKey('speaking_speed_wpm')
            ? _number(root['speaking_speed_wpm'], minimum: 0, maximum: 1000)
            : null,
      );
    default:
      throw const SpeechFeedbackDecodeException();
  }
}

SpeechFeedbackStableFailure _failure(Object? value) {
  final root = _exactObject(
    value,
    required: const {'code', 'retryable', 'message'},
  );
  final retryable = root['retryable'];
  if (retryable is! bool) {
    throw const SpeechFeedbackDecodeException();
  }
  return SpeechFeedbackStableFailure(
    code: _identifier(root['code']),
    retryable: retryable,
    message: _text(root['message'], maximumBytes: 2048),
  );
}

List<String> _reasonCodes(Object? value) {
  if (value is! List<Object?> || value.length > 16) {
    throw const SpeechFeedbackDecodeException();
  }
  final values = value.map(_identifier).toList(growable: false);
  if (values.toSet().length != values.length) {
    throw const SpeechFeedbackDecodeException();
  }
  return List<String>.unmodifiable(values);
}

SpeechFeedbackSourceKind _sourceKind(Object? value) => switch (value) {
  'PRACTICE_TURN_FEEDBACK' => SpeechFeedbackSourceKind.practiceTurn,
  'AGENT_MESSAGE_FEEDBACK' => SpeechFeedbackSourceKind.agentMessage,
  _ => throw const SpeechFeedbackDecodeException(),
};

SpeechFeedbackStatus _status(Object? value) => switch (value) {
  'QUEUED' => SpeechFeedbackStatus.queued,
  'RUNNING' => SpeechFeedbackStatus.running,
  'READY' => SpeechFeedbackStatus.ready,
  'FAILED' => SpeechFeedbackStatus.failed,
  _ => throw const SpeechFeedbackDecodeException(),
};

SpeechFeedbackItemKind _itemKind(Object? value) => switch (value) {
  'CORRECTION' => SpeechFeedbackItemKind.correction,
  'STRENGTH' => SpeechFeedbackItemKind.strength,
  'RECOMMENDED_EXPRESSION' => SpeechFeedbackItemKind.recommendedExpression,
  _ => throw const SpeechFeedbackDecodeException(),
};

SpeechFeedbackRepracticeMode _repracticeMode(Object? value) => switch (value) {
  'NONE' => SpeechFeedbackRepracticeMode.none,
  'SAME_QUESTION' => SpeechFeedbackRepracticeMode.sameQuestion,
  _ => throw const SpeechFeedbackDecodeException(),
};

Map<String, Object?> _exactObject(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const {},
}) {
  if (value is! Map<String, Object?> ||
      !value.keys.toSet().containsAll(required) ||
      value.keys.any(
        (key) => !required.contains(key) && !optional.contains(key),
      )) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String || !_uuidPattern.hasMatch(value)) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String || !_identifierPattern.hasMatch(value)) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

String _text(Object? value, {required int maximumBytes}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value != value.trim() ||
      utf8.encode(value).length > maximumBytes) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

int _integer(Object? value, {required int minimum, required int maximum}) {
  if (value is! int || value < minimum || value > maximum) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

double _score(Object? value) => _number(value, minimum: 0, maximum: 100);

double _number(
  Object? value, {
  required double minimum,
  required double maximum,
}) {
  if (value is! num || !value.isFinite || value < minimum || value > maximum) {
    throw const SpeechFeedbackDecodeException();
  }
  return value.toDouble();
}

DateTime _dateTime(Object? value) {
  if (value is! String || !value.endsWith('Z')) {
    throw const SpeechFeedbackDecodeException();
  }
  final parsed = DateTime.tryParse(value);
  if (parsed == null || !parsed.isUtc) {
    throw const SpeechFeedbackDecodeException();
  }
  return parsed;
}

final class _SpeechResult {
  const _SpeechResult({
    required this.scoreability,
    required this.summary,
    required this.reasonCodes,
    required this.acoustic,
  });

  final SpeechFeedbackScoreabilityStatus scoreability;
  final String summary;
  final List<String> reasonCodes;
  final SpeechFeedbackAcousticAssessment acoustic;
}

const maximumFeedbackResponseBytes = 512 * 1024;

final _uuidPattern = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);
final _identifierPattern = RegExp(r'^[A-Za-z][A-Za-z0-9_]{0,63}$');
