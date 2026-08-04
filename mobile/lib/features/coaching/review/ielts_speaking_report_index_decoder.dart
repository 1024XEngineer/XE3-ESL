import 'dart:convert';

import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';

final class IeltsSpeakingReportIndexDecodeException implements Exception {
  const IeltsSpeakingReportIndexDecodeException();

  @override
  String toString() => 'IeltsSpeakingReportIndexDecodeException';
}

IeltsSpeakingReportIndexPage decodeIeltsSpeakingReportIndexJson(String body) {
  try {
    return decodeIeltsSpeakingReportIndex(jsonDecode(body));
  } on FormatException {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
}

IeltsSpeakingReportIndexPage decodeIeltsSpeakingReportIndex(Object? value) {
  final root = _exactObject(
    value,
    required: const {'items'},
    optional: const {'next_cursor'},
  );
  final rawItems = root['items'];
  if (rawItems is! List<Object?> || rawItems.length > 100) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final practiceSessionIds = <String>{};
  final evaluationIds = <String>{};
  final items = List<IeltsSpeakingReportIndexItem>.unmodifiable(
    rawItems.map((item) {
      final result = _item(item);
      if (!practiceSessionIds.add(result.practiceSessionId) ||
          !evaluationIds.add(result.evaluationId)) {
        throw const IeltsSpeakingReportIndexDecodeException();
      }
      return result;
    }),
  );
  return IeltsSpeakingReportIndexPage(
    items: items,
    nextCursor: root.containsKey('next_cursor')
        ? _cursor(root['next_cursor'])
        : null,
  );
}

IeltsSpeakingReportIndexItem _item(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'report_kind',
      'practice_session_id',
      'evaluation_id',
      'evaluation_revision_id',
      'revision',
      'evaluation_status',
      'is_final',
      'status_url',
      'created_at',
      'updated_at',
    },
    optional: const {'title'},
  );
  final kind = switch (root['report_kind']) {
    'IELTS_SPEAKING_FULL_MOCK' => IeltsSpeakingReportKind.fullMock,
    'INTERVIEW' => IeltsSpeakingReportKind.interview,
    _ => throw const IeltsSpeakingReportIndexDecodeException(),
  };
  if (root['is_final'] != false) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final practiceSessionId = _identifier(root['practice_session_id']);
  final statusUrl = root['status_url'];
  final expectedSuffix = kind == IeltsSpeakingReportKind.interview
      ? 'interview-report'
      : 'ielts-speaking-report';
  if (statusUrl != '/v1/practice-sessions/$practiceSessionId/$expectedSuffix') {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final createdAt = _dateTime(root['created_at']);
  final updatedAt = _dateTime(root['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final title = root.containsKey('title') ? _reportTitle(root['title']) : null;
  return IeltsSpeakingReportIndexItem(
    reportKind: kind,
    practiceSessionId: practiceSessionId,
    evaluationId: _uuid(root['evaluation_id']),
    evaluationRevisionId: _uuid(root['evaluation_revision_id']),
    revision: _positiveInt(root['revision']),
    evaluationStatus: _evaluationStatus(root['evaluation_status']),
    isFinal: false,
    statusUrl: statusUrl as String,
    createdAt: createdAt,
    updatedAt: updatedAt,
    title: title,
  );
}

String _reportTitle(Object? value) {
  if (value is! String) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final trimmed = value.trim();
  if (trimmed.isEmpty || trimmed.length > 256) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return trimmed;
}

Map<String, Object?> _exactObject(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const {},
}) {
  if (value is! Map<String, Object?>) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final keys = value.keys.toSet();
  if (!keys.containsAll(required) ||
      keys.any((key) => !required.contains(key) && !optional.contains(key))) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.length > 128 ||
      value != value.trim() ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value)) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String ||
      !RegExp(
        r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
      ).hasMatch(value)) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return value;
}

int _positiveInt(Object? value) {
  if (value is! int || value < 1) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return value;
}

DateTime _dateTime(Object? value) {
  if (value is! String || value != value.trim()) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final match = _rfc3339.firstMatch(value);
  if (match == null) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final year = int.parse(match[1]!);
  final month = int.parse(match[2]!);
  final day = int.parse(match[3]!);
  final hour = int.parse(match[4]!);
  final minute = int.parse(match[5]!);
  final second = int.parse(match[6]!);
  final offsetHour = match[9] == null ? 0 : int.parse(match[9]!);
  final offsetMinute = match[10] == null ? 0 : int.parse(match[10]!);
  final calendar = DateTime.utc(year, month, day, hour, minute, second);
  if (year < 1 ||
      calendar.year != year ||
      calendar.month != month ||
      calendar.day != day ||
      calendar.hour != hour ||
      calendar.minute != minute ||
      calendar.second != second ||
      offsetHour > 23 ||
      offsetMinute > 59) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  final parsed = DateTime.tryParse(value);
  if (parsed == null) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return parsed.toUtc();
}

String _cursor(Object? value) {
  if (value is! String ||
      value.length < 16 ||
      value.length > 512 ||
      !RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(value)) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return value;
}

IeltsSpeakingReportEvaluationStatus _evaluationStatus(Object? value) =>
    switch (value) {
      'QUEUED' => IeltsSpeakingReportEvaluationStatus.queued,
      'RUNNING' => IeltsSpeakingReportEvaluationStatus.running,
      'READY' => IeltsSpeakingReportEvaluationStatus.ready,
      'FAILED' => IeltsSpeakingReportEvaluationStatus.failed,
      _ => throw const IeltsSpeakingReportIndexDecodeException(),
    };

final _rfc3339 = RegExp(
  r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|[+-](\d{2}):(\d{2}))$',
);
