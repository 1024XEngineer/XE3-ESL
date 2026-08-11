import 'dart:convert';

import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class PracticeReportStatusDecodeException implements Exception {
  const PracticeReportStatusDecodeException();

  @override
  String toString() => 'PracticeReportStatusDecodeException';
}

PracticeReportStatus decodePracticeReportStatusJson(String body) {
  try {
    return decodePracticeReportStatus(jsonDecode(body));
  } on FormatException {
    throw const PracticeReportStatusDecodeException();
  }
}

PracticeReportStatus decodePracticeReportStatus(Object? value) {
  final root = _exactObject(
    value,
    required: const <String>{
      'practice_session_id',
      'practice_mode',
      'report_scope',
      'available_sections',
      'detail_schema',
      'evaluation_status',
      'status_url',
    },
    optional: const <String>{
      'evaluation_id',
      'evaluation_revision_id',
      'revision',
      'report_ref',
      'scoreability_status',
      'summary',
      'stable_failure',
    },
  );
  final sessionId = _identifier(root['practice_session_id']);
  final modeValue = root['practice_mode'];
  final mode = modeValue is String
      ? PracticeMode.fromWireValue(modeValue)
      : null;
  if (mode != PracticeMode.part1 &&
      mode != PracticeMode.part2 &&
      mode != PracticeMode.part3 &&
      mode != PracticeMode.fullMock) {
    throw const PracticeReportStatusDecodeException();
  }
  final scope = _scope(root['report_scope']);
  final expectedScope = switch (mode!) {
    PracticeMode.part1 => PracticeReportScope.part1,
    PracticeMode.part2 => PracticeReportScope.part2And3,
    PracticeMode.part3 => PracticeReportScope.part3,
    PracticeMode.fullMock => PracticeReportScope.fullMock,
    _ => throw const PracticeReportStatusDecodeException(),
  };
  if (scope != expectedScope) {
    throw const PracticeReportStatusDecodeException();
  }
  final rawSections = root['available_sections'];
  if (rawSections is! List<Object?> || rawSections.length > 4) {
    throw const PracticeReportStatusDecodeException();
  }
  final sections = rawSections.map(_part).toList(growable: false);
  if (sections.toSet().length != sections.length ||
      !_sameParts(sections, _expectedParts(mode))) {
    throw const PracticeReportStatusDecodeException();
  }
  final detailSchema = _text(root['detail_schema'], maxBytes: 128);
  final status = _status(root['evaluation_status']);
  final validDetailSchema = mode == PracticeMode.fullMock
      ? detailSchema == 'ielts-speaking-report/v1'
      : detailSchema == 'ielts-speaking-practice-report/v1' ||
            status == PracticeReportEvaluationStatus.ready &&
                detailSchema == 'general-scene-evaluation/v1';
  if (!validDetailSchema) {
    throw const PracticeReportStatusDecodeException();
  }
  final statusUrl = root['status_url'];
  if (statusUrl != '/v1/practice-sessions/$sessionId/report') {
    throw const PracticeReportStatusDecodeException();
  }

  final hasEvaluationId = root.containsKey('evaluation_id');
  final hasEvaluationRevisionId = root.containsKey('evaluation_revision_id');
  final hasRevision = root.containsKey('revision');
  if (hasEvaluationId != hasEvaluationRevisionId ||
      hasEvaluationId != hasRevision) {
    throw const PracticeReportStatusDecodeException();
  }
  final evaluationId = hasEvaluationId ? _uuid(root['evaluation_id']) : null;
  final evaluationRevisionId = hasEvaluationId
      ? _uuid(root['evaluation_revision_id'])
      : null;
  final revision = hasEvaluationId ? _positiveInt(root['revision']) : null;
  if ((status == PracticeReportEvaluationStatus.running ||
          status == PracticeReportEvaluationStatus.ready) &&
      evaluationId == null) {
    throw const PracticeReportStatusDecodeException();
  }

  final hasReportRef = root.containsKey('report_ref');
  final hasScoreability = root.containsKey('scoreability_status');
  final hasSummary = root.containsKey('summary');
  final hasFailure = root.containsKey('stable_failure');
  PracticeReportRef? reportRef;
  EvaluationReportScoreability? scoreability;
  String? summary;
  PracticeReportStableFailure? failure;
  switch (status) {
    case PracticeReportEvaluationStatus.queued:
    case PracticeReportEvaluationStatus.running:
      if (hasReportRef || hasScoreability || hasSummary || hasFailure) {
        throw const PracticeReportStatusDecodeException();
      }
    case PracticeReportEvaluationStatus.ready:
      if (!hasReportRef || !hasScoreability || !hasSummary || hasFailure) {
        throw const PracticeReportStatusDecodeException();
      }
      reportRef = _reportRef(root['report_ref']);
      scoreability = switch (root['scoreability_status']) {
        'PROVISIONAL' => EvaluationReportScoreability.provisional,
        'INSUFFICIENT' => EvaluationReportScoreability.insufficient,
        _ => throw const PracticeReportStatusDecodeException(),
      };
      summary = _text(root['summary'], maxBytes: 4096);
    case PracticeReportEvaluationStatus.failed:
      if (hasReportRef || hasScoreability || hasSummary || !hasFailure) {
        throw const PracticeReportStatusDecodeException();
      }
      failure = _stableFailure(root['stable_failure']);
  }

  return PracticeReportStatus(
    practiceSessionId: sessionId,
    practiceMode: mode,
    reportScope: scope,
    availableSections: List<IeltsSpeakingPartId>.unmodifiable(sections),
    detailSchema: detailSchema,
    evaluationStatus: status,
    statusUrl: statusUrl as String,
    evaluationId: evaluationId,
    evaluationRevisionId: evaluationRevisionId,
    revision: revision,
    reportRef: reportRef,
    scoreability: scoreability,
    summary: summary,
    stableFailure: failure,
  );
}

PracticeReportRef _reportRef(Object? value) {
  final root = _exactObject(
    value,
    required: const <String>{'report_id', 'href'},
  );
  final reportId = _uuid(root['report_id']);
  if (root['href'] != '/v1/evaluation-reports/$reportId') {
    throw const PracticeReportStatusDecodeException();
  }
  return PracticeReportRef(reportId: reportId, href: root['href']! as String);
}

PracticeReportStableFailure _stableFailure(Object? value) {
  final root = _exactObject(
    value,
    required: const <String>{'reason_code', 'retryable'},
  );
  final reasonCode = root['reason_code'];
  final retryable = root['retryable'];
  if (reasonCode is! String ||
      !RegExp(r'^[A-Z][A-Z0-9_]{0,63}$').hasMatch(reasonCode) ||
      retryable is! bool) {
    throw const PracticeReportStatusDecodeException();
  }
  return PracticeReportStableFailure(
    reasonCode: reasonCode,
    retryable: retryable,
  );
}

PracticeReportScope _scope(Object? value) => switch (value) {
  'PART_1' => PracticeReportScope.part1,
  'PART_2_3' => PracticeReportScope.part2And3,
  'PART_3' => PracticeReportScope.part3,
  'FULL_MOCK' => PracticeReportScope.fullMock,
  _ => throw const PracticeReportStatusDecodeException(),
};

IeltsSpeakingPartId _part(Object? value) => switch (value) {
  'PART_1' => IeltsSpeakingPartId.part1,
  'PART_2' => IeltsSpeakingPartId.part2,
  'PART_3' => IeltsSpeakingPartId.part3,
  _ => throw const PracticeReportStatusDecodeException(),
};

List<IeltsSpeakingPartId> _expectedParts(PracticeMode mode) => switch (mode) {
  PracticeMode.part1 => const <IeltsSpeakingPartId>[IeltsSpeakingPartId.part1],
  PracticeMode.part2 => const <IeltsSpeakingPartId>[
    IeltsSpeakingPartId.part2,
    IeltsSpeakingPartId.part3,
  ],
  PracticeMode.part3 => const <IeltsSpeakingPartId>[IeltsSpeakingPartId.part3],
  PracticeMode.fullMock => const <IeltsSpeakingPartId>[
    IeltsSpeakingPartId.part1,
    IeltsSpeakingPartId.part2,
    IeltsSpeakingPartId.part3,
  ],
  _ => throw const PracticeReportStatusDecodeException(),
};

bool _sameParts(
  List<IeltsSpeakingPartId> left,
  List<IeltsSpeakingPartId> right,
) {
  if (left.length != right.length) return false;
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) return false;
  }
  return true;
}

PracticeReportEvaluationStatus _status(Object? value) => switch (value) {
  'QUEUED' => PracticeReportEvaluationStatus.queued,
  'RUNNING' => PracticeReportEvaluationStatus.running,
  'READY' => PracticeReportEvaluationStatus.ready,
  'FAILED' => PracticeReportEvaluationStatus.failed,
  _ => throw const PracticeReportStatusDecodeException(),
};

Map<String, Object?> _exactObject(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw const PracticeReportStatusDecodeException();
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const PracticeReportStatusDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.length > 128 ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value)) {
    throw const PracticeReportStatusDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String ||
      !RegExp(
        r'^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
      ).hasMatch(value)) {
    throw const PracticeReportStatusDecodeException();
  }
  return value;
}

int _positiveInt(Object? value) {
  if (value is! int || value < 1) {
    throw const PracticeReportStatusDecodeException();
  }
  return value;
}

String _text(Object? value, {required int maxBytes}) {
  if (value is! String ||
      value.trim() != value ||
      value.isEmpty ||
      utf8.encode(value).length > maxBytes) {
    throw const PracticeReportStatusDecodeException();
  }
  return value;
}
