import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report_decoder.dart';

enum SessionEvaluationStatus { queued, running, ready, failed }

final class SessionEvaluationFailure {
  const SessionEvaluationFailure({
    required this.code,
    required this.retryable,
    required this.message,
  });

  final String code;
  final bool retryable;
  final String message;
}

final class SessionEvaluation {
  const SessionEvaluation({
    required this.evaluationId,
    required this.practiceSessionId,
    required this.status,
    required this.updatedAt,
    this.report,
    this.failure,
  });

  final String evaluationId;
  final String practiceSessionId;
  final SessionEvaluationStatus status;
  final DateTime updatedAt;
  final EvaluationReport? report;
  final SessionEvaluationFailure? failure;
}

final class SessionEvaluationDecodeException implements Exception {
  const SessionEvaluationDecodeException();
}

SessionEvaluation decodeSessionEvaluation(Object? value) {
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
  final sessionId = _uuid(root['source_id']);
  if (root['kind'] != 'SESSION_REPORT' ||
      root['context_id'] != sessionId ||
      root['feedback_items'] is! List<Object?> ||
      (root['feedback_items']! as List<Object?>).isNotEmpty) {
    throw const SessionEvaluationDecodeException();
  }
  final status = switch (root['status']) {
    'QUEUED' => SessionEvaluationStatus.queued,
    'RUNNING' => SessionEvaluationStatus.running,
    'READY' => SessionEvaluationStatus.ready,
    'FAILED' => SessionEvaluationStatus.failed,
    _ => throw const SessionEvaluationDecodeException(),
  };
  final createdAt = _dateTime(root['created_at']);
  final updatedAt = _dateTime(root['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const SessionEvaluationDecodeException();
  }
  EvaluationReport? report;
  SessionEvaluationFailure? failure;
  if (status == SessionEvaluationStatus.ready) {
    if (!root.containsKey('result') || root.containsKey('error')) {
      throw const SessionEvaluationDecodeException();
    }
    try {
      report = decodeEvaluationReport(<String, Object?>{
        'report_id': evaluationId,
        'evaluation_id': evaluationId,
        'practice_session_id': sessionId,
        'report': root['result'],
        'created_at': root['updated_at'],
      });
    } on EvaluationReportDecodeException {
      throw const SessionEvaluationDecodeException();
    }
  } else if (status == SessionEvaluationStatus.failed) {
    if (!root.containsKey('error') || root.containsKey('result')) {
      throw const SessionEvaluationDecodeException();
    }
    final rawFailure = _exactObject(
      root['error'],
      required: const {'code', 'retryable', 'message'},
    );
    final retryable = rawFailure['retryable'];
    if (retryable is! bool) throw const SessionEvaluationDecodeException();
    failure = SessionEvaluationFailure(
      code: _identifier(rawFailure['code']),
      retryable: retryable,
      message: _text(rawFailure['message'], 2048),
    );
  } else if (root.containsKey('result') || root.containsKey('error')) {
    throw const SessionEvaluationDecodeException();
  }
  return SessionEvaluation(
    evaluationId: evaluationId,
    practiceSessionId: sessionId,
    status: status,
    updatedAt: updatedAt,
    report: report,
    failure: failure,
  );
}

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
    throw const SessionEvaluationDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String || !_uuidPattern.hasMatch(value)) {
    throw const SessionEvaluationDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String || !_identifierPattern.hasMatch(value)) {
    throw const SessionEvaluationDecodeException();
  }
  return value;
}

String _text(Object? value, int maximumLength) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.length > maximumLength ||
      value.contains('\u0000')) {
    throw const SessionEvaluationDecodeException();
  }
  return value;
}

DateTime _dateTime(Object? value) {
  if (value is! String || value.length > 64) {
    throw const SessionEvaluationDecodeException();
  }
  final parsed = DateTime.tryParse(value);
  if (parsed == null || !value.contains(RegExp(r'(?:Z|[+-]\d\d:\d\d)$'))) {
    throw const SessionEvaluationDecodeException();
  }
  return parsed.toUtc();
}

final _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
);
final _identifierPattern = RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$');
