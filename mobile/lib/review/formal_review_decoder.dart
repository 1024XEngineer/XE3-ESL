import 'dart:convert';

import 'package:speakup/review/formal_review.dart';

final class FormalReviewDecodeException implements Exception {
  const FormalReviewDecodeException();

  @override
  String toString() => 'FormalReviewDecodeException';
}

FormalReview decodeFormalReview(Object? value) {
  final initial = _object(value);
  final implementationVersion = _string(
    initial,
    'implementation_version',
    maxBytes: _maxMetadataBytes,
  );
  final schema = implementationVersion == _sceneImplementation
      ? FormalReviewSchema.sceneV2
      : FormalReviewSchema.legacyVoiceV1;
  final root = _exactObject(
    initial,
    required: {
      'review_id',
      'practice_session_id',
      'status',
      'implementation_version',
      'source_turn_id',
      'source_turn_version',
      'created_at',
      'updated_at',
      if (schema == FormalReviewSchema.sceneV2) ...{
        'evaluation_context_type',
        'evaluation_context',
      },
    },
    optional: const {'result', 'completed_at'},
  );
  final id = _string(root, 'review_id', maxBytes: _maxMetadataBytes);
  final practiceSessionId = _string(
    root,
    'practice_session_id',
    maxBytes: _maxMetadataBytes,
  );
  final status = _status(root['status']);
  final sourceTurnId = _string(
    root,
    'source_turn_id',
    maxBytes: _maxMetadataBytes,
  );
  final sourceTurnVersion = _string(
    root,
    'source_turn_version',
    maxBytes: _maxMetadataBytes,
  );
  if (!_sourceTurnVersionPattern.hasMatch(sourceTurnVersion)) {
    throw const FormalReviewDecodeException();
  }
  final createdAt = _dateTime(root['created_at']);
  final updatedAt = _dateTime(root['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const FormalReviewDecodeException();
  }

  FormalReviewContextType? contextType;
  if (schema == FormalReviewSchema.sceneV2) {
    contextType = _contextType(root['evaluation_context_type']);
    _decodeEvaluationContext(root['evaluation_context'], contextType);
  }

  final hasResult = root.containsKey('result');
  final hasCompletedAt = root.containsKey('completed_at');
  if ((hasResult && root['result'] == null) ||
      (hasCompletedAt && root['completed_at'] == null)) {
    throw const FormalReviewDecodeException();
  }
  FormalReviewResult? result;
  DateTime? completedAt;
  if (status == FormalReviewStatus.completed) {
    if (!hasResult || !hasCompletedAt) {
      throw const FormalReviewDecodeException();
    }
    completedAt = _dateTime(root['completed_at']);
    if (completedAt.isBefore(createdAt)) {
      throw const FormalReviewDecodeException();
    }
    result = schema == FormalReviewSchema.sceneV2
        ? _decodeSceneResult(root['result'], contextType!)
        : _decodeLegacyResult(root['result']);
  } else if (hasResult || hasCompletedAt) {
    throw const FormalReviewDecodeException();
  }

  return FormalReview(
    id: id,
    practiceSessionId: practiceSessionId,
    status: status,
    schema: schema,
    implementationVersion: implementationVersion,
    sourceTurnId: sourceTurnId,
    sourceTurnVersion: sourceTurnVersion,
    contextType: contextType,
    result: result,
    createdAt: createdAt,
    updatedAt: updatedAt,
    completedAt: completedAt,
  );
}

FormalReviewResult _decodeLegacyResult(Object? value) {
  final root = _exactObject(
    _object(value),
    required: const {'overall_score', 'summary', 'conclusions'},
    optional: const {'summary_eligibility'},
  );
  _validateResultSize(root);
  if (root.containsKey('summary_eligibility') &&
      _eligibility(root['summary_eligibility']) !=
          FormalReviewSummaryEligibility.eligible) {
    throw const FormalReviewDecodeException();
  }
  final overallScore = _score(root['overall_score']);
  final dimensions = _dimensions(
    root['conclusions'],
    requireNonEmpty: true,
    requireScore: false,
  );
  return FormalReviewResult(
    eligibility: FormalReviewSummaryEligibility.eligible,
    overallScore: overallScore,
    summary: _string(root, 'summary', maxBytes: _maxTextBytes),
    dimensions: dimensions,
    feedbackItems: const <FormalReviewFeedbackItem>[],
    repracticeSuggestionRefs: const <String>[],
    insufficientEvidenceReasons: const <String>[],
  );
}

FormalReviewResult _decodeSceneResult(
  Object? value,
  FormalReviewContextType contextType,
) {
  final root = _exactObject(
    _object(value),
    required: const {'summary_eligibility', 'summary', 'conclusions'},
    optional: const {
      'overall_score',
      'feedback_items',
      'repractice_suggestion_refs',
      'insufficient_evidence_reasons',
    },
  );
  _validateResultSize(root);
  final eligibility = _eligibility(root['summary_eligibility']);
  final overallScore = root.containsKey('overall_score')
      ? _score(root['overall_score'])
      : null;
  final dimensions = _dimensions(
    root['conclusions'],
    requireNonEmpty:
        eligibility != FormalReviewSummaryEligibility.insufficientEvidence,
    requireScore:
        eligibility != FormalReviewSummaryEligibility.insufficientEvidence,
  );
  final feedbackItems = root.containsKey('feedback_items')
      ? _feedbackItems(root['feedback_items'])
      : const <FormalReviewFeedbackItem>[];
  final repracticeRefs = root.containsKey('repractice_suggestion_refs')
      ? _uniqueStrings(
          root['repractice_suggestion_refs'],
          maximumItems: _maxFeedbackItems,
          maxBytes: _maxLabelBytes,
          allowEmpty: true,
        )
      : const <String>[];
  final insufficientReasons = root.containsKey('insufficient_evidence_reasons')
      ? _uniqueStrings(
          root['insufficient_evidence_reasons'],
          maxBytes: _maxLabelBytes,
          allowEmpty: false,
        )
      : const <String>[];

  final feedbackKeys = feedbackItems.map((item) => item.key).toSet();
  if (repracticeRefs.any((ref) => !feedbackKeys.contains(ref))) {
    throw const FormalReviewDecodeException();
  }
  switch (eligibility) {
    case FormalReviewSummaryEligibility.eligible:
      if (insufficientReasons.isNotEmpty ||
          (contextType == FormalReviewContextType.ieltsSpeakingPart2 &&
              overallScore == null) ||
          (overallScore != null &&
              contextType != FormalReviewContextType.ieltsSpeakingPart2)) {
        throw const FormalReviewDecodeException();
      }
    case FormalReviewSummaryEligibility.provisional:
      if (contextType != FormalReviewContextType.ieltsSpeakingPart2 ||
          overallScore != null ||
          insufficientReasons.isEmpty) {
        throw const FormalReviewDecodeException();
      }
    case FormalReviewSummaryEligibility.insufficientEvidence:
      if (overallScore != null ||
          dimensions.isNotEmpty ||
          feedbackItems.isNotEmpty ||
          repracticeRefs.isNotEmpty ||
          insufficientReasons.isEmpty) {
        throw const FormalReviewDecodeException();
      }
  }
  return FormalReviewResult(
    eligibility: eligibility,
    overallScore: overallScore,
    summary: _string(root, 'summary', maxBytes: _maxTextBytes),
    dimensions: dimensions,
    feedbackItems: feedbackItems,
    repracticeSuggestionRefs: repracticeRefs,
    insufficientEvidenceReasons: insufficientReasons,
  );
}

List<FormalReviewDimension> _dimensions(
  Object? value, {
  required bool requireNonEmpty,
  required bool requireScore,
}) {
  if (value is! List<Object?> ||
      value.length > _maxDimensions ||
      (requireNonEmpty && value.isEmpty)) {
    throw const FormalReviewDecodeException();
  }
  final keys = <String>{};
  return List<FormalReviewDimension>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        _object(item),
        required: const {'key', 'category', 'message'},
        optional: const {'score', 'suggestion'},
      );
      final key = _string(root, 'key', maxBytes: _maxLabelBytes);
      if (key != key.trim() || !keys.add(key)) {
        throw const FormalReviewDecodeException();
      }
      if (requireScore && !root.containsKey('score')) {
        throw const FormalReviewDecodeException();
      }
      return FormalReviewDimension(
        key: key,
        category: _string(root, 'category', maxBytes: _maxLabelBytes),
        score: root.containsKey('score') ? _score(root['score']) : null,
        message: _string(root, 'message', maxBytes: _maxTextBytes),
        suggestion: root.containsKey('suggestion')
            ? _string(root, 'suggestion', maxBytes: _maxTextBytes)
            : null,
      );
    }),
  );
}

List<FormalReviewFeedbackItem> _feedbackItems(Object? value) {
  if (value is! List<Object?> || value.length > _maxFeedbackItems) {
    throw const FormalReviewDecodeException();
  }
  final keys = <String>{};
  return List<FormalReviewFeedbackItem>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        _object(item),
        required: const {'key', 'kind', 'message'},
        optional: const {'suggestion'},
      );
      final key = _string(root, 'key', maxBytes: _maxLabelBytes);
      if (key != key.trim() || !keys.add(key)) {
        throw const FormalReviewDecodeException();
      }
      return FormalReviewFeedbackItem(
        key: key,
        kind: _feedbackKind(root['kind']),
        message: _string(root, 'message', maxBytes: _maxTextBytes),
        suggestion: root.containsKey('suggestion')
            ? _string(root, 'suggestion', maxBytes: _maxTextBytes)
            : null,
      );
    }),
  );
}

void _decodeEvaluationContext(
  Object? value,
  FormalReviewContextType expectedType,
) {
  final root = _exactObject(
    _object(value),
    required: const {
      'schema_version',
      'context_type',
      'scene_key',
      'scene_id',
      'scene_version',
      'practice_option_type',
      'difficulty_ref',
      'assistance_ref',
      'turn_policy_ref',
      'session_policy_ref',
      'scene_specific_context',
    },
  );
  if (root['schema_version'] != 'evaluation-context.v1' ||
      _contextType(root['context_type']) != expectedType) {
    throw const FormalReviewDecodeException();
  }
  _string(root, 'scene_key', maxBytes: _maxMetadataBytes);
  _string(root, 'scene_id', maxBytes: _maxMetadataBytes);
  final sceneVersion = root['scene_version'];
  if (sceneVersion is! int || sceneVersion < 1) {
    throw const FormalReviewDecodeException();
  }
  _string(root, 'practice_option_type', maxBytes: 64);
  _string(root, 'difficulty_ref', maxBytes: _maxMetadataBytes);
  _string(root, 'assistance_ref', maxBytes: _maxMetadataBytes);
  _string(root, 'turn_policy_ref', maxBytes: _maxMetadataBytes);
  _string(root, 'session_policy_ref', maxBytes: _maxMetadataBytes);
  _decodeSceneSpecificContext(root['scene_specific_context'], expectedType);
}

void _decodeSceneSpecificContext(
  Object? value,
  FormalReviewContextType expectedType,
) {
  final field = switch (expectedType) {
    FormalReviewContextType.interviewProjectDeepDive =>
      'interview_project_deep_dive',
    FormalReviewContextType.ieltsSpeakingPart2 => 'ielts_speaking_part2',
    FormalReviewContextType.workplaceProgressRiskUpdate =>
      'workplace_progress_risk_update',
    FormalReviewContextType.dailyHotelCheckinIssue =>
      'daily_hotel_checkin_issue',
    FormalReviewContextType.genericPractice => 'generic_practice',
  };
  final root = _exactObject(_object(value), required: {'type', field});
  if (_contextType(root['type']) != expectedType) {
    throw const FormalReviewDecodeException();
  }
  final details = _object(root[field]);
  switch (expectedType) {
    case FormalReviewContextType.interviewProjectDeepDive:
      final item = _exactObject(
        details,
        required: const {
          'version',
          'project_brief',
          'candidate_role',
          'focus_points',
        },
      );
      if (item['version'] != 'interview.project_deep_dive.v1') {
        throw const FormalReviewDecodeException();
      }
      _string(item, 'project_brief', maxBytes: 4096);
      _string(item, 'candidate_role', maxBytes: 256);
      _stringList(
        item['focus_points'],
        minItems: 1,
        maxItems: 12,
        maxBytes: 256,
      );
    case FormalReviewContextType.ieltsSpeakingPart2:
      final item = _exactObject(
        details,
        required: const {
          'version',
          'cue_card_topic',
          'cue_card_points',
          'strict_simulation',
        },
      );
      if (item['version'] != 'ielts.speaking_part2.v1' ||
          item['strict_simulation'] is! bool) {
        throw const FormalReviewDecodeException();
      }
      _string(item, 'cue_card_topic', maxBytes: 2048);
      _stringList(
        item['cue_card_points'],
        minItems: 1,
        maxItems: 12,
        maxBytes: 512,
      );
    case FormalReviewContextType.workplaceProgressRiskUpdate:
      final item = _exactObject(
        details,
        required: const {
          'version',
          'initiative_brief',
          'audience',
          'expected_sections',
        },
      );
      if (item['version'] != 'workplace.progress_risk_update.v1') {
        throw const FormalReviewDecodeException();
      }
      _string(item, 'initiative_brief', maxBytes: 4096);
      _string(item, 'audience', maxBytes: 256);
      _stringList(
        item['expected_sections'],
        minItems: 1,
        maxItems: 12,
        maxBytes: 256,
      );
    case FormalReviewContextType.dailyHotelCheckinIssue:
      final item = _exactObject(
        details,
        required: const {
          'version',
          'reservation_brief',
          'issue',
          'desired_outcome',
        },
      );
      if (item['version'] != 'daily.hotel_checkin_issue.v1') {
        throw const FormalReviewDecodeException();
      }
      _string(item, 'reservation_brief', maxBytes: 2048);
      _string(item, 'issue', maxBytes: 2048);
      _string(item, 'desired_outcome', maxBytes: 2048);
    case FormalReviewContextType.genericPractice:
      final item = _exactObject(
        details,
        required: const {'version', 'practice_goal'},
      );
      if (item['version'] != 'generic.practice.v1') {
        throw const FormalReviewDecodeException();
      }
      _string(item, 'practice_goal', maxBytes: 2048);
  }
}

FormalReviewStatus _status(Object? value) => switch (value) {
  'pending' => FormalReviewStatus.pending,
  'generating' => FormalReviewStatus.generating,
  'completed' => FormalReviewStatus.completed,
  'failed' => FormalReviewStatus.failed,
  _ => throw const FormalReviewDecodeException(),
};

FormalReviewContextType _contextType(Object? value) => switch (value) {
  'interview.project_deep_dive' =>
    FormalReviewContextType.interviewProjectDeepDive,
  'ielts.speaking_part2' => FormalReviewContextType.ieltsSpeakingPart2,
  'workplace.progress_risk_update' =>
    FormalReviewContextType.workplaceProgressRiskUpdate,
  'daily.hotel_checkin_issue' => FormalReviewContextType.dailyHotelCheckinIssue,
  'generic.practice' => FormalReviewContextType.genericPractice,
  _ => throw const FormalReviewDecodeException(),
};

FormalReviewSummaryEligibility _eligibility(Object? value) => switch (value) {
  'eligible' => FormalReviewSummaryEligibility.eligible,
  'provisional' => FormalReviewSummaryEligibility.provisional,
  'insufficient_evidence' =>
    FormalReviewSummaryEligibility.insufficientEvidence,
  _ => throw const FormalReviewDecodeException(),
};

FormalReviewFeedbackKind _feedbackKind(Object? value) => switch (value) {
  'correction' => FormalReviewFeedbackKind.correction,
  'strength' => FormalReviewFeedbackKind.strength,
  'improvement' => FormalReviewFeedbackKind.improvement,
  'recommended_expression' => FormalReviewFeedbackKind.recommendedExpression,
  _ => throw const FormalReviewDecodeException(),
};

Map<String, Object?> _object(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const FormalReviewDecodeException();
  }
  return value;
}

Map<String, Object?> _exactObject(
  Map<String, Object?> value, {
  Set<String> required = const <String>{},
  Set<String> optional = const <String>{},
}) {
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const FormalReviewDecodeException();
  }
  return value;
}

String _string(
  Map<String, Object?> value,
  String key, {
  required int maxBytes,
}) {
  final item = value[key];
  if (item is! String ||
      item.trim().isEmpty ||
      item.contains('\u0000') ||
      utf8.encode(item).length > maxBytes) {
    throw const FormalReviewDecodeException();
  }
  return item;
}

DateTime _dateTime(Object? value) {
  if (value is! String ||
      !_dateTimePattern.hasMatch(value) ||
      value.contains('\u0000') ||
      utf8.encode(value).length > 64) {
    throw const FormalReviewDecodeException();
  }
  final result = DateTime.tryParse(value);
  if (result == null) {
    throw const FormalReviewDecodeException();
  }
  return result.toUtc();
}

int _score(Object? value) {
  if (value is! int || value < 0 || value > 100) {
    throw const FormalReviewDecodeException();
  }
  return value;
}

List<String> _uniqueStrings(
  Object? value, {
  int? maximumItems,
  required int maxBytes,
  required bool allowEmpty,
}) {
  if (value is! List<Object?> ||
      (maximumItems != null && value.length > maximumItems) ||
      (!allowEmpty && value.isEmpty)) {
    throw const FormalReviewDecodeException();
  }
  final seen = <String>{};
  return List<String>.unmodifiable(
    value.map((item) {
      final text = _standaloneString(item, maxBytes: maxBytes);
      if (!seen.add(text)) {
        throw const FormalReviewDecodeException();
      }
      return text;
    }),
  );
}

void _stringList(
  Object? value, {
  required int minItems,
  required int maxItems,
  required int maxBytes,
}) {
  if (value is! List<Object?> ||
      value.length < minItems ||
      value.length > maxItems) {
    throw const FormalReviewDecodeException();
  }
  for (final item in value) {
    _standaloneString(item, maxBytes: maxBytes);
  }
}

String _standaloneString(Object? value, {required int maxBytes}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const FormalReviewDecodeException();
  }
  return value;
}

void _validateResultSize(Map<String, Object?> value) {
  try {
    if (utf8.encode(jsonEncode(value)).length > _maxResultBytes) {
      throw const FormalReviewDecodeException();
    }
  } on JsonUnsupportedObjectError {
    throw const FormalReviewDecodeException();
  }
}

const _sceneImplementation = 'qianwen-scene-review-v2';
const _maxResultBytes = 12 * 1024;
const _maxMetadataBytes = 128;
const _maxLabelBytes = 64;
const _maxTextBytes = 2048;
const _maxDimensions = 8;
const _maxFeedbackItems = 16;
final _sourceTurnVersionPattern = RegExp(
  r'^conversation-turn:evidence-v[1-9][0-9]*$',
);
final _dateTimePattern = RegExp(
  r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$',
);
