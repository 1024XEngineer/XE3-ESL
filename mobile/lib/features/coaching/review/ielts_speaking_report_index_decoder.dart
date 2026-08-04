import 'dart:convert';

import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';

final class IeltsSpeakingReportIndexDecodeException implements Exception {
  const IeltsSpeakingReportIndexDecodeException();
}

IeltsSpeakingReportIndexPage decodeIeltsSpeakingReportIndexJson(String body) {
  try {
    final root = jsonDecode(body);
    if (root is! Map<String, dynamic> || root['items'] is! List) {
      throw const IeltsSpeakingReportIndexDecodeException();
    }
    final items = (root['items'] as List)
        .map((raw) {
          if (raw is! Map<String, dynamic>) {
            throw const IeltsSpeakingReportIndexDecodeException();
          }
          final kind = switch (raw['report_kind']) {
            'IELTS_SPEAKING_FULL_MOCK' => IeltsSpeakingReportKind.fullMock,
            'INTERVIEW' => IeltsSpeakingReportKind.interview,
            _ => throw const IeltsSpeakingReportIndexDecodeException(),
          };
          final sessionId = _id(raw['practice_session_id']);
          final expectedSuffix = kind == IeltsSpeakingReportKind.interview
              ? 'interview-report'
              : 'ielts-speaking-report';
          final statusUrl = raw['status_url'];
          if (statusUrl != '/v1/practice-sessions/$sessionId/$expectedSuffix' ||
              raw['is_final'] != false) {
            throw const IeltsSpeakingReportIndexDecodeException();
          }
          final createdAt = _date(raw['created_at']);
          final updatedAt = _date(raw['updated_at']);
          if (updatedAt.isBefore(createdAt)) {
            throw const IeltsSpeakingReportIndexDecodeException();
          }
          final title = raw['title'];
          if (title != null && (title is! String || title.trim().isEmpty)) {
            throw const IeltsSpeakingReportIndexDecodeException();
          }
          return IeltsSpeakingReportIndexItem(
            reportKind: kind,
            practiceSessionId: sessionId,
            evaluationId: _uuid(raw['evaluation_id']),
            evaluationRevisionId: _uuid(raw['evaluation_revision_id']),
            revision: _positiveInt(raw['revision']),
            evaluationStatus: _status(raw['evaluation_status']),
            isFinal: false,
            statusUrl: statusUrl as String,
            createdAt: createdAt,
            updatedAt: updatedAt,
            title: title as String?,
          );
        })
        .toList(growable: false);
    final cursor = root['next_cursor'];
    if (cursor != null &&
        (cursor is! String ||
            !RegExp(r'^[A-Za-z0-9_-]{16,512}$').hasMatch(cursor))) {
      throw const IeltsSpeakingReportIndexDecodeException();
    }
    return IeltsSpeakingReportIndexPage(
      items: items,
      nextCursor: cursor as String?,
    );
  } on FormatException {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
}

String _id(Object? value) {
  if (value is! String ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$').hasMatch(value)) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String || !RegExp(r'^[0-9a-fA-F-]{36}$').hasMatch(value)) {
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

DateTime _date(Object? value) {
  if (value is! String || DateTime.tryParse(value) == null) {
    throw const IeltsSpeakingReportIndexDecodeException();
  }
  return DateTime.parse(value).toUtc();
}

IeltsSpeakingReportEvaluationStatus _status(Object? value) => switch (value) {
  'QUEUED' => IeltsSpeakingReportEvaluationStatus.queued,
  'RUNNING' => IeltsSpeakingReportEvaluationStatus.running,
  'READY' => IeltsSpeakingReportEvaluationStatus.ready,
  'FAILED' => IeltsSpeakingReportEvaluationStatus.failed,
  _ => throw const IeltsSpeakingReportIndexDecodeException(),
};
