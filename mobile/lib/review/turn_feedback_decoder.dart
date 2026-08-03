import 'dart:convert';

import 'package:speakup/review/turn_feedback.dart';

final class SpeechFeedbackDecodeException implements Exception {
  const SpeechFeedbackDecodeException();

  @override
  String toString() => 'SpeechFeedbackDecodeException';
}

SpeechFeedback decodeSpeechFeedbackJson(String body) {
  try {
    if (utf8.encode(body).length > _maximumFeedbackResponseBytes) {
      throw const SpeechFeedbackDecodeException();
    }
    return decodeSpeechFeedback(jsonDecode(body));
  } on FormatException {
    throw const SpeechFeedbackDecodeException();
  }
}

SpeechFeedback decodeSpeechFeedback(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'speech_feedback_id',
      'source',
      'feedback_status',
      'schema_version',
      'strategy_ref',
      'pipeline_version',
      'is_final',
      'items',
      'acoustic_assessment',
      'status_url',
      'created_at',
      'updated_at',
    },
    optional: const {
      'scoreability_status',
      'gate_status',
      'reason_codes',
      'stable_failure',
      'completed_at',
    },
  );
  _validateResponseSize(root);
  final speechFeedbackId = _resourceId(root['speech_feedback_id']);
  final statusUrl = root['status_url'];
  if (statusUrl is! String ||
      !validSpeechFeedbackStatusUrl(statusUrl) ||
      statusUrl != '/v1/speech-feedback/$speechFeedbackId' ||
      root['schema_version'] != 'speech-feedback/v1' ||
      root['strategy_ref'] != _speechFeedbackStrategyRef ||
      root['pipeline_version'] != _speechFeedbackPipelineVersion ||
      root['is_final'] != false) {
    throw const SpeechFeedbackDecodeException();
  }

  final source = _source(root['source']);
  final feedbackStatus = _feedbackStatus(root['feedback_status']);
  final items = _items(
    root['items'],
    speechFeedbackId: speechFeedbackId,
    source: source,
  );
  final createdAt = _dateTime(root['created_at']);
  final updatedAt = _dateTime(root['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const SpeechFeedbackDecodeException();
  }

  SpeechFeedbackScoreabilityStatus? scoreability;
  SpeechFeedbackGateStatus? gate;
  var reasonCodes = const <String>[];
  SpeechFeedbackStableFailure? stableFailure;
  DateTime? completedAt;
  final hasScoreability = root.containsKey('scoreability_status');
  final hasGate = root.containsKey('gate_status');
  final hasReasonCodes = root.containsKey('reason_codes');
  final hasFailure = root.containsKey('stable_failure');
  final hasCompletedAt = root.containsKey('completed_at');
  switch (feedbackStatus) {
    case SpeechFeedbackStatus.queued:
    case SpeechFeedbackStatus.running:
      if (hasScoreability ||
          hasGate ||
          hasReasonCodes ||
          hasFailure ||
          hasCompletedAt ||
          items.isNotEmpty) {
        throw const SpeechFeedbackDecodeException();
      }
    case SpeechFeedbackStatus.ready:
      if (!hasScoreability || !hasGate || hasFailure || !hasCompletedAt) {
        throw const SpeechFeedbackDecodeException();
      }
      scoreability = _scoreability(root['scoreability_status']);
      gate = _gate(root['gate_status']);
      completedAt = _dateTime(root['completed_at']);
      if (completedAt.isBefore(createdAt)) {
        throw const SpeechFeedbackDecodeException();
      }
      switch (scoreability) {
        case SpeechFeedbackScoreabilityStatus.provisional:
          if (gate != SpeechFeedbackGateStatus.feedbackOnly ||
              hasReasonCodes ||
              items.isEmpty) {
            throw const SpeechFeedbackDecodeException();
          }
        case SpeechFeedbackScoreabilityStatus.insufficient:
          if (gate != SpeechFeedbackGateStatus.blocked ||
              !hasReasonCodes ||
              items.isNotEmpty) {
            throw const SpeechFeedbackDecodeException();
          }
          reasonCodes = _reasonCodes(root['reason_codes']);
      }
    case SpeechFeedbackStatus.failed:
      if (hasScoreability ||
          hasGate ||
          hasReasonCodes ||
          !hasFailure ||
          !hasCompletedAt ||
          items.isNotEmpty) {
        throw const SpeechFeedbackDecodeException();
      }
      stableFailure = _stableFailure(root['stable_failure']);
      completedAt = _dateTime(root['completed_at']);
      if (completedAt.isBefore(createdAt)) {
        throw const SpeechFeedbackDecodeException();
      }
  }

  return SpeechFeedback(
    speechFeedbackId: speechFeedbackId,
    source: source,
    feedbackStatus: feedbackStatus,
    scoreabilityStatus: scoreability,
    gateStatus: gate,
    reasonCodes: reasonCodes,
    schemaVersion: 'speech-feedback/v1',
    strategyRef: _speechFeedbackStrategyRef,
    pipelineVersion: _speechFeedbackPipelineVersion,
    isFinal: false,
    items: items,
    acousticAssessment: _acousticAssessment(root['acoustic_assessment']),
    stableFailure: stableFailure,
    statusUrl: statusUrl,
    createdAt: createdAt,
    updatedAt: updatedAt,
    completedAt: completedAt,
  );
}

SpeechFeedbackSource _source(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const SpeechFeedbackDecodeException();
  }
  return switch (value['source_kind']) {
    'CONVERSATION_TURN' => _conversationSource(value),
    'AGENT_VOICE_MESSAGE' => _agentSource(value),
    _ => throw const SpeechFeedbackDecodeException(),
  };
}

ConversationTurnFeedbackSource _conversationSource(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'source_kind',
      'practice_session_id',
      'turn_id',
      'input_revision',
      'evidence_snapshot_id',
    },
  );
  if (root['source_kind'] != 'CONVERSATION_TURN') {
    throw const SpeechFeedbackDecodeException();
  }
  return ConversationTurnFeedbackSource(
    practiceSessionId: _resourceId(root['practice_session_id']),
    turnId: _resourceId(root['turn_id']),
    inputRevision: _positiveRevision(root['input_revision']),
    evidenceSnapshotId: _resourceId(root['evidence_snapshot_id']),
  );
}

AgentVoiceMessageFeedbackSource _agentSource(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'source_kind',
      'thread_id',
      'message_id',
      'transcript_evidence_id',
      'candidate_version',
    },
  );
  if (root['source_kind'] != 'AGENT_VOICE_MESSAGE') {
    throw const SpeechFeedbackDecodeException();
  }
  return AgentVoiceMessageFeedbackSource(
    threadId: _resourceId(root['thread_id']),
    messageId: _resourceId(root['message_id']),
    transcriptEvidenceId: _resourceId(root['transcript_evidence_id']),
    candidateVersion: _positiveRevision(root['candidate_version']),
  );
}

List<SpeechFeedbackItem> _items(
  Object? value, {
  required String speechFeedbackId,
  required SpeechFeedbackSource source,
}) {
  if (value is! List<Object?> || value.length > 8) {
    throw const SpeechFeedbackDecodeException();
  }
  final itemIds = <String>{};
  return List<SpeechFeedbackItem>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {
          'feedback_item_id',
          'speech_feedback_id',
          'kind',
          'anchor',
          'explanation',
          'repractice_mode',
          'created_at',
        },
        optional: const {'suggested_text'},
      );
      final itemId = _resourceId(root['feedback_item_id']);
      final parentId = _resourceId(root['speech_feedback_id']);
      if (!itemIds.add(itemId) || parentId != speechFeedbackId) {
        throw const SpeechFeedbackDecodeException();
      }
      final kind = _itemKind(root['kind']);
      final hasSuggestedText = root.containsKey('suggested_text');
      final suggestedText = hasSuggestedText
          ? _feedbackText(root['suggested_text'])
          : null;
      final repracticeMode = _repracticeMode(root['repractice_mode']);
      if ((kind == SpeechFeedbackItemKind.strength &&
              (hasSuggestedText ||
                  repracticeMode != SpeechFeedbackRepracticeMode.none)) ||
          (kind != SpeechFeedbackItemKind.strength && !hasSuggestedText)) {
        throw const SpeechFeedbackDecodeException();
      }
      final anchor = _anchor(root['anchor']);
      _validateItemSource(
        source: source,
        anchor: anchor,
        repracticeMode: repracticeMode,
      );
      return SpeechFeedbackItem(
        feedbackItemId: itemId,
        speechFeedbackId: parentId,
        kind: kind,
        anchor: anchor,
        explanation: _feedbackText(root['explanation']),
        suggestedText: suggestedText,
        repracticeMode: repracticeMode,
        createdAt: _dateTime(root['created_at']),
      );
    }),
  );
}

SpeechFeedbackAnchor _anchor(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const SpeechFeedbackDecodeException();
  }
  return switch (value['anchor_kind']) {
    'CONVERSATION_TRANSCRIPT' => _conversationAnchor(value),
    'AGENT_TRANSCRIPT' => _agentAnchor(value),
    _ => throw const SpeechFeedbackDecodeException(),
  };
}

ConversationTranscriptFeedbackAnchor _conversationAnchor(
  Map<String, Object?> value,
) {
  final root = _exactObject(
    value,
    required: const {
      'anchor_kind',
      'evidence_ref_id',
      'turn_id',
      'start_utf8_byte',
      'end_utf8_byte',
      'original_excerpt',
    },
  );
  if (root['anchor_kind'] != 'CONVERSATION_TRANSCRIPT') {
    throw const SpeechFeedbackDecodeException();
  }
  final anchor = ConversationTranscriptFeedbackAnchor(
    evidenceRefId: _resourceId(root['evidence_ref_id']),
    turnId: _resourceId(root['turn_id']),
    startUtf8Byte: _offset(root['start_utf8_byte'], maximum: 16383),
    endUtf8Byte: _offset(root['end_utf8_byte'], minimum: 1, maximum: 16384),
    originalExcerpt: _transcriptExcerpt(root['original_excerpt']),
  );
  _validateAnchorRange(anchor);
  return anchor;
}

AgentTranscriptFeedbackAnchor _agentAnchor(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'anchor_kind',
      'transcript_evidence_id',
      'message_id',
      'start_utf8_byte',
      'end_utf8_byte',
      'original_excerpt',
    },
  );
  if (root['anchor_kind'] != 'AGENT_TRANSCRIPT') {
    throw const SpeechFeedbackDecodeException();
  }
  final anchor = AgentTranscriptFeedbackAnchor(
    transcriptEvidenceId: _resourceId(root['transcript_evidence_id']),
    messageId: _resourceId(root['message_id']),
    startUtf8Byte: _offset(root['start_utf8_byte'], maximum: 16383),
    endUtf8Byte: _offset(root['end_utf8_byte'], minimum: 1, maximum: 16384),
    originalExcerpt: _transcriptExcerpt(root['original_excerpt']),
  );
  _validateAnchorRange(anchor);
  return anchor;
}

void _validateAnchorRange(SpeechFeedbackAnchor anchor) {
  if (anchor.endUtf8Byte <= anchor.startUtf8Byte ||
      utf8.encode(anchor.originalExcerpt).length !=
          anchor.endUtf8Byte - anchor.startUtf8Byte) {
    throw const SpeechFeedbackDecodeException();
  }
}

void _validateItemSource({
  required SpeechFeedbackSource source,
  required SpeechFeedbackAnchor anchor,
  required SpeechFeedbackRepracticeMode repracticeMode,
}) {
  switch (source) {
    case ConversationTurnFeedbackSource():
      if (anchor is! ConversationTranscriptFeedbackAnchor ||
          anchor.turnId != source.turnId ||
          repracticeMode == SpeechFeedbackRepracticeMode.sameThread) {
        throw const SpeechFeedbackDecodeException();
      }
    case AgentVoiceMessageFeedbackSource():
      if (anchor is! AgentTranscriptFeedbackAnchor ||
          anchor.transcriptEvidenceId != source.transcriptEvidenceId ||
          anchor.messageId != source.messageId ||
          repracticeMode == SpeechFeedbackRepracticeMode.sameQuestion) {
        throw const SpeechFeedbackDecodeException();
      }
  }
}

SpeechFeedbackAcousticAssessment _acousticAssessment(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const SpeechFeedbackDecodeException();
  }
  if (value['pronunciation'] == 'NOT_ASSESSED') {
    final root = _exactObject(
      value,
      required: const {'pronunciation', 'acoustic_fluency', 'reason_code'},
    );
    if (root['acoustic_fluency'] != 'NOT_ASSESSED' ||
        root['reason_code'] != 'ACOUSTIC_EVIDENCE_UNAVAILABLE') {
      throw const SpeechFeedbackDecodeException();
    }
    return const SpeechFeedbackAcousticAssessment(
      pronunciation: SpeechFeedbackAssessmentStatus.notAssessed,
      acousticFluency: SpeechFeedbackAssessmentStatus.notAssessed,
      reasonCode: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    );
  }
  if (value['category'] == 'topic') {
    final root = _exactObject(
      value,
      required: const {
        'pronunciation',
        'acoustic_fluency',
        'pronunciation_score',
        'speaking_speed_wpm',
        'semantic_score',
        'provider',
        'provider_session_id',
        'category',
        'notice',
      },
    );
    final providerSessionId = root['provider_session_id'];
    if (root['pronunciation'] != 'ASSESSED' ||
        root['acoustic_fluency'] != 'ASSESSED' ||
        root['provider'] != 'xfyun-ise' ||
        providerSessionId is! String ||
        providerSessionId.isEmpty ||
        providerSessionId.length > 256 ||
        providerSessionId != providerSessionId.trim() ||
        root['notice'] != '根据本次录音自动评估，仅供练习参考。') {
      throw const SpeechFeedbackDecodeException();
    }
    return SpeechFeedbackAcousticAssessment(
      pronunciation: SpeechFeedbackAssessmentStatus.assessed,
      acousticFluency: SpeechFeedbackAssessmentStatus.assessed,
      reasonCode: '',
      pronunciationScore: _acousticScore(root['pronunciation_score']),
      speakingSpeedWpm: _speakingSpeed(root['speaking_speed_wpm']),
      semanticScore: _acousticScore(root['semantic_score']),
      provider: root['provider']! as String,
      providerSessionId: providerSessionId,
      category: 'topic',
      notice: root['notice']! as String,
    );
  }
  final root = _exactObject(
    value,
    required: const {
      'pronunciation',
      'acoustic_fluency',
      'integrity',
      'accuracy_score',
      'fluency_score',
      'integrity_score',
      'provider',
      'provider_session_id',
      'category',
      'notice',
    },
  );
  final accuracy = _acousticScore(root['accuracy_score']);
  final fluency = _acousticScore(root['fluency_score']);
  final integrity = _acousticScore(root['integrity_score']);
  final providerSessionId = root['provider_session_id'];
  if (root['pronunciation'] != 'ASSESSED' ||
      root['acoustic_fluency'] != 'ASSESSED' ||
      root['integrity'] != 'ASSESSED' ||
      root['provider'] != 'xfyun-ise' ||
      providerSessionId is! String ||
      providerSessionId.isEmpty ||
      providerSessionId.length > 256 ||
      providerSessionId != providerSessionId.trim() ||
      (root['category'] != 'read_word' &&
          root['category'] != 'read_sentence') ||
      root['notice'] != '根据本次录音自动评估，仅供练习参考。') {
    throw const SpeechFeedbackDecodeException();
  }
  return SpeechFeedbackAcousticAssessment(
    pronunciation: SpeechFeedbackAssessmentStatus.assessed,
    acousticFluency: SpeechFeedbackAssessmentStatus.assessed,
    integrity: SpeechFeedbackAssessmentStatus.assessed,
    reasonCode: '',
    accuracyScore: accuracy,
    fluencyScore: fluency,
    integrityScore: integrity,
    provider: root['provider']! as String,
    providerSessionId: providerSessionId,
    category: root['category']! as String,
    notice: root['notice']! as String,
  );
}

double _acousticScore(Object? value) {
  if (value is! num) {
    throw const SpeechFeedbackDecodeException();
  }
  final score = value.toDouble();
  if (!score.isFinite || score < 0 || score > 100) {
    throw const SpeechFeedbackDecodeException();
  }
  return score;
}

double _speakingSpeed(Object? value) {
  if (value is! num) {
    throw const SpeechFeedbackDecodeException();
  }
  final speed = value.toDouble();
  if (!speed.isFinite || speed <= 0 || speed > 1000) {
    throw const SpeechFeedbackDecodeException();
  }
  return speed;
}

SpeechFeedbackStableFailure _stableFailure(Object? value) {
  final root = _exactObject(
    value,
    required: const {'reason_code', 'retryable'},
  );
  final reasonCode = root['reason_code'];
  final retryable = root['retryable'];
  if (reasonCode is! String ||
      !_stableFailureReasonCodes.contains(reasonCode) ||
      retryable is! bool) {
    throw const SpeechFeedbackDecodeException();
  }
  return SpeechFeedbackStableFailure(
    reasonCode: reasonCode,
    retryable: retryable,
  );
}

Map<String, Object?> _exactObject(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const {},
}) {
  if (value is! Map<String, Object?>) {
    throw const SpeechFeedbackDecodeException();
  }
  final keys = value.keys.toSet();
  if (!keys.containsAll(required) ||
      keys.any((key) => !required.contains(key) && !optional.contains(key))) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

String _resourceId(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.runes.length > 128 ||
      value.contains('\u0000')) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

int _positiveRevision(Object? value) {
  if (value is! int || value < 1 || value > 9007199254740991) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

int _offset(Object? value, {int minimum = 0, required int maximum}) {
  if (value is! int || value < minimum || value > maximum) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

String _feedbackText(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.runes.length > 2048 ||
      value.trim().isEmpty ||
      _containsForbiddenControl(value) ||
      utf8.encode(value).length > 2048) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

String _transcriptExcerpt(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.runes.length > 4096 ||
      _containsForbiddenControl(value) ||
      utf8.encode(value).length > 16384) {
    throw const SpeechFeedbackDecodeException();
  }
  return value;
}

bool _containsForbiddenControl(String value) {
  return value.runes.any(
    (rune) =>
        (rune >= 0x00 && rune <= 0x08) ||
        rune == 0x0B ||
        rune == 0x0C ||
        (rune >= 0x0E && rune <= 0x1F) ||
        rune == 0x7F ||
        (rune >= 0x80 && rune <= 0x9F),
  );
}

void _validateResponseSize(Map<String, Object?> value) {
  try {
    if (utf8.encode(jsonEncode(value)).length > _maximumFeedbackResponseBytes) {
      throw const SpeechFeedbackDecodeException();
    }
  } on JsonUnsupportedObjectError {
    throw const SpeechFeedbackDecodeException();
  }
}

DateTime _dateTime(Object? value) {
  if (value is! String || value.length > 64 || value != value.trim()) {
    throw const SpeechFeedbackDecodeException();
  }
  final match = _rfc3339.firstMatch(value);
  if (match == null) {
    throw const SpeechFeedbackDecodeException();
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
    throw const SpeechFeedbackDecodeException();
  }
  final parsed = DateTime.tryParse(value);
  if (parsed == null) {
    throw const SpeechFeedbackDecodeException();
  }
  return parsed.toUtc();
}

List<String> _reasonCodes(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 3) {
    throw const SpeechFeedbackDecodeException();
  }
  final seen = <String>{};
  return List<String>.unmodifiable(
    value.map((item) {
      if (item is! String ||
          !_insufficientReasonCodes.contains(item) ||
          !seen.add(item)) {
        throw const SpeechFeedbackDecodeException();
      }
      return item;
    }),
  );
}

SpeechFeedbackStatus _feedbackStatus(Object? value) => switch (value) {
  'QUEUED' => SpeechFeedbackStatus.queued,
  'RUNNING' => SpeechFeedbackStatus.running,
  'READY' => SpeechFeedbackStatus.ready,
  'FAILED' => SpeechFeedbackStatus.failed,
  _ => throw const SpeechFeedbackDecodeException(),
};

SpeechFeedbackScoreabilityStatus _scoreability(Object? value) =>
    switch (value) {
      'PROVISIONAL' => SpeechFeedbackScoreabilityStatus.provisional,
      'INSUFFICIENT' => SpeechFeedbackScoreabilityStatus.insufficient,
      _ => throw const SpeechFeedbackDecodeException(),
    };

SpeechFeedbackGateStatus _gate(Object? value) => switch (value) {
  'FEEDBACK_ONLY' => SpeechFeedbackGateStatus.feedbackOnly,
  'BLOCKED' => SpeechFeedbackGateStatus.blocked,
  _ => throw const SpeechFeedbackDecodeException(),
};

SpeechFeedbackItemKind _itemKind(Object? value) => switch (value) {
  'CORRECTION' => SpeechFeedbackItemKind.correction,
  'STRENGTH' => SpeechFeedbackItemKind.strength,
  'IMPROVEMENT' => SpeechFeedbackItemKind.improvement,
  'RECOMMENDED_EXPRESSION' => SpeechFeedbackItemKind.recommendedExpression,
  _ => throw const SpeechFeedbackDecodeException(),
};

SpeechFeedbackRepracticeMode _repracticeMode(Object? value) => switch (value) {
  'NONE' => SpeechFeedbackRepracticeMode.none,
  'SAME_QUESTION' => SpeechFeedbackRepracticeMode.sameQuestion,
  'SAME_THREAD' => SpeechFeedbackRepracticeMode.sameThread,
  _ => throw const SpeechFeedbackDecodeException(),
};

const _insufficientReasonCodes = <String>{
  'TEXT_TOO_SHORT',
  'TRANSCRIPT_CONFIDENCE_INSUFFICIENT',
  'EVIDENCE_INCONSISTENT',
};

const _stableFailureReasonCodes = <String>{
  'PROVIDER_UNAVAILABLE',
  'PROVIDER_RESPONSE_INVALID',
  'PROCESSING_TIMEOUT',
  'INTERNAL_PROCESSING_ERROR',
};

const _speechFeedbackStrategyRef = 'qianwen-speech-feedback/v1';
const _speechFeedbackPipelineVersion = 'speech-feedback-pipeline/v1';
final _rfc3339 = RegExp(
  r'^(\d{4,})-(\d{2})-(\d{2})T'
  r'(\d{2}):(\d{2}):(\d{2})(\.\d+)?'
  r'(Z|([+-])(\d{2}):(\d{2}))$',
);

const _maximumFeedbackResponseBytes = 512 * 1024;
