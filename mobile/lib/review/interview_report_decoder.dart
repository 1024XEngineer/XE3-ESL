import 'dart:convert';

import 'package:speakup/review/interview_report.dart';

final class InterviewReportDecodeException implements Exception {
  const InterviewReportDecodeException();

  @override
  String toString() => 'InterviewReportDecodeException';
}

InterviewReportEnvelope decodeInterviewReportJson(String body) {
  try {
    return decodeInterviewReport(jsonDecode(body));
  } on FormatException {
    throw const InterviewReportDecodeException();
  }
}

InterviewReportEnvelope decodeInterviewReport(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'practice_session_id',
      'evaluation_id',
      'evaluation_revision_id',
      'revision',
      'evaluation_status',
      'is_final',
      'status_url',
    },
    optional: const {'report', 'stable_failure'},
  );
  final practiceSessionId = _identifier(root['practice_session_id']);
  final evaluationId = _uuid(root['evaluation_id']);
  final evaluationRevisionId = _uuid(root['evaluation_revision_id']);
  final revision = _positiveInt(root['revision']);
  final status = _evaluationStatus(root['evaluation_status']);
  final isFinal = root['is_final'];
  final statusUrl = root['status_url'];
  if (isFinal != false ||
      statusUrl is! String ||
      statusUrl !=
          '/v1/practice-sessions/$practiceSessionId/interview-report') {
    throw const InterviewReportDecodeException();
  }

  final hasReport = root.containsKey('report');
  final hasFailure = root.containsKey('stable_failure');
  if ((hasReport && root['report'] == null) ||
      (hasFailure && root['stable_failure'] == null)) {
    throw const InterviewReportDecodeException();
  }
  InterviewReport? report;
  InterviewReportStableFailure? failure;
  switch (status) {
    case InterviewReportEvaluationStatus.queued:
    case InterviewReportEvaluationStatus.running:
      if (hasReport || hasFailure) {
        throw const InterviewReportDecodeException();
      }
    case InterviewReportEvaluationStatus.ready:
      if (!hasReport || hasFailure) {
        throw const InterviewReportDecodeException();
      }
      report = _report(root['report']);
    case InterviewReportEvaluationStatus.failed:
      if (hasReport || !hasFailure) {
        throw const InterviewReportDecodeException();
      }
      failure = _failure(root['stable_failure']);
  }
  return InterviewReportEnvelope(
    practiceSessionId: practiceSessionId,
    evaluationId: evaluationId,
    evaluationRevisionId: evaluationRevisionId,
    revision: revision,
    evaluationStatus: status,
    isFinal: false,
    statusUrl: statusUrl,
    report: report,
    stableFailure: failure,
  );
}

InterviewReport _report(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'schema_version',
      'scoreability_status',
      'gate_status',
      'readiness_level',
      'readiness_notice',
      'dimensions',
      'questions',
      'priority_actions',
    },
  );
  if (root['schema_version'] != 'interview-report/v1') {
    throw const InterviewReportDecodeException();
  }
  final scoreability = _scoreability(root['scoreability_status']);
  final gate = _gate(root['gate_status']);
  if ((scoreability == InterviewReportScoreabilityStatus.provisional &&
          gate != InterviewReportGateStatus.feedbackOnly) ||
      (scoreability == InterviewReportScoreabilityStatus.insufficient &&
          gate != InterviewReportGateStatus.blocked)) {
    throw const InterviewReportDecodeException();
  }
  final readinessNotice = _text(root['readiness_notice'], maxBytes: 256);
  if (readinessNotice != _expectedReadinessNotice) {
    throw const InterviewReportDecodeException();
  }
  final dimensions = _dimensions(root['dimensions']);
  final findings =
      <
        String,
        ({
          InterviewReportDimensionId dimension,
          String kind,
          InterviewReportFinding finding,
        })
      >{};
  final evidenceTurns = <String, String>{};
  final evidenceAnchors = <InterviewReportEvidence>[];
  for (final dimension in dimensions) {
    if (!_validDimensionGate(
      rootScoreability: scoreability,
      rootGate: gate,
      dimension: dimension,
    )) {
      throw const InterviewReportDecodeException();
    }
    final dimensionEvidence = <String>{};
    for (final group in <({String kind, List<InterviewReportFinding> values})>[
      (kind: 'strength', values: dimension.strengths),
      (kind: 'improvement', values: dimension.improvements),
      (
        kind: 'recommended_expression',
        values: dimension.recommendedExpressions,
      ),
    ]) {
      for (final finding in group.values) {
        if (findings.containsKey(finding.id)) {
          throw const InterviewReportDecodeException();
        }
        findings[finding.id] = (
          dimension: dimension.id,
          kind: group.kind,
          finding: finding,
        );
        for (final item in finding.evidence) {
          dimensionEvidence.add(item.evidenceRefId);
          final existingTurn = evidenceTurns[item.evidenceRefId];
          if (existingTurn != null && existingTurn != item.turnId) {
            throw const InterviewReportDecodeException();
          }
          evidenceTurns[item.evidenceRefId] = item.turnId;
          evidenceAnchors.add(item);
        }
      }
    }
    if (!_sameSet(dimensionEvidence, dimension.evidenceRefIds.toSet())) {
      throw const InterviewReportDecodeException();
    }
  }

  final questions = _questions(root['questions']);
  final questionIds = questions.map((question) => question.questionId).toSet();
  if (questionIds.length != questions.length) {
    throw const InterviewReportDecodeException();
  }
  final questionsById = <String, InterviewReportQuestion>{
    for (final question in questions) question.questionId: question,
  };
  final questionsByTurnId = <String, InterviewReportQuestion>{};
  for (final question in questions) {
    final parent = question.parentQuestionId;
    if ((question.questionType == InterviewReportQuestionType.primary &&
            parent != null) ||
        (question.questionType == InterviewReportQuestionType.followUp &&
            (parent == null ||
                parent == question.questionId ||
                questionsById[parent]?.questionType !=
                    InterviewReportQuestionType.primary))) {
      throw const InterviewReportDecodeException();
    }
    final responseTurnId = question.responseTurnId;
    if (responseTurnId != null &&
        questionsByTurnId.putIfAbsent(responseTurnId, () => question) !=
            question) {
      throw const InterviewReportDecodeException();
    }
    for (final ref in question.evidenceRefIds) {
      final evidenceTurn = evidenceTurns[ref];
      if (evidenceTurn != null && evidenceTurn != responseTurnId) {
        throw const InterviewReportDecodeException();
      }
    }
    for (final result in question.dimensionFindings) {
      for (final group in <({String kind, List<String> ids})>[
        (kind: 'strength', ids: result.strengthFindingIds),
        (kind: 'improvement', ids: result.improvementFindingIds),
        (
          kind: 'recommended_expression',
          ids: result.recommendedExpressionFindingIds,
        ),
      ]) {
        for (final id in group.ids) {
          final resolved = findings[id];
          if (resolved == null ||
              resolved.dimension != result.dimensionId ||
              resolved.kind != group.kind ||
              !resolved.finding.evidence.any(
                (item) => question.evidenceRefIds.contains(item.evidenceRefId),
              )) {
            throw const InterviewReportDecodeException();
          }
        }
      }
    }
  }
  for (final evidence in evidenceAnchors) {
    final question = questionsByTurnId[evidence.turnId];
    if (question == null ||
        !question.evidenceRefIds.contains(evidence.evidenceRefId) ||
        question.confirmedTranscript == null ||
        !_containsExcerpt(question.confirmedTranscript!, evidence)) {
      throw const InterviewReportDecodeException();
    }
  }

  final priorityActions = _priorityActions(root['priority_actions']);
  for (final action in priorityActions) {
    final resolved = findings[action.findingId];
    if (resolved == null ||
        resolved.dimension != action.dimensionId ||
        resolved.kind != 'improvement' ||
        resolved.finding.suggestion == null) {
      throw const InterviewReportDecodeException();
    }
  }
  if (scoreability == InterviewReportScoreabilityStatus.insufficient &&
      (priorityActions.isNotEmpty ||
          dimensions.any(
            (dimension) =>
                dimension.strengths.isNotEmpty ||
                dimension.improvements.isNotEmpty ||
                dimension.recommendedExpressions.isNotEmpty,
          ))) {
    throw const InterviewReportDecodeException();
  }
  return InterviewReport(
    schemaVersion: 'interview-report/v1',
    scoreabilityStatus: scoreability,
    gateStatus: gate,
    readinessLevel: _readiness(root['readiness_level']),
    readinessNotice: readinessNotice,
    dimensions: dimensions,
    questions: questions,
    priorityActions: priorityActions,
  );
}

List<InterviewReportDimension> _dimensions(Object? value) {
  if (value is! List<Object?> || value.length != _dimensionOrder.length) {
    throw const InterviewReportDecodeException();
  }
  return List<InterviewReportDimension>.unmodifiable([
    for (var index = 0; index < value.length; index++)
      _dimension(value[index], expected: _dimensionOrder[index]),
  ]);
}

InterviewReportDimension _dimension(
  Object? value, {
  required InterviewReportDimensionId expected,
}) {
  final root = _exactObject(
    value,
    required: const {
      'dimension_id',
      'scoreability_status',
      'gate_status',
      'coverage',
      'confidence',
      'reason_codes',
      'evidence_ref_ids',
      'strengths',
      'improvements',
      'recommended_expressions',
    },
  );
  final id = _dimensionId(root['dimension_id']);
  if (id != expected) {
    throw const InterviewReportDecodeException();
  }
  final result = InterviewReportDimension(
    id: id,
    scoreabilityStatus: _scoreability(root['scoreability_status']),
    gateStatus: _gate(root['gate_status']),
    coverage: _ratio(root['coverage']),
    confidence: _ratio(root['confidence']),
    reasonCodes: _reasonCodes(root['reason_codes']),
    evidenceRefIds: _uniqueIdentifiers(root['evidence_ref_ids']),
    strengths: _findings(root['strengths']),
    improvements: _findings(root['improvements']),
    recommendedExpressions: _findings(root['recommended_expressions']),
  );
  if (result.reasonCodes.length != 1) {
    throw const InterviewReportDecodeException();
  }
  if (result.scoreabilityStatus ==
      InterviewReportScoreabilityStatus.provisional) {
    if (result.gateStatus != InterviewReportGateStatus.feedbackOnly ||
        result.reasonCodes.single != 'ASR_CONFIDENCE_UNAVAILABLE' ||
        result.evidenceRefIds.isEmpty ||
        (result.strengths.isEmpty && result.improvements.isEmpty)) {
      throw const InterviewReportDecodeException();
    }
  } else if (result.gateStatus != InterviewReportGateStatus.blocked ||
      !const {
        'INSUFFICIENT_EVIDENCE',
        'OPPORTUNITY_NOT_PROVIDED',
      }.contains(result.reasonCodes.single) ||
      result.evidenceRefIds.isNotEmpty ||
      result.strengths.isNotEmpty ||
      result.improvements.isNotEmpty ||
      result.recommendedExpressions.isNotEmpty) {
    throw const InterviewReportDecodeException();
  }
  return result;
}

List<InterviewReportFinding> _findings(Object? value) {
  if (value is! List<Object?> || value.length > 3) {
    throw const InterviewReportDecodeException();
  }
  return List<InterviewReportFinding>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {'finding_id', 'message', 'evidence'},
        optional: const {'suggestion'},
      );
      final evidence = root['evidence'];
      if (evidence is! List<Object?> ||
          evidence.isEmpty ||
          evidence.length > 4) {
        throw const InterviewReportDecodeException();
      }
      return InterviewReportFinding(
        id: _identifier(root['finding_id']),
        message: _text(root['message'], maxBytes: _maximumFeedbackTextBytes),
        suggestion: root.containsKey('suggestion')
            ? _text(root['suggestion'], maxBytes: _maximumFeedbackTextBytes)
            : null,
        evidence: List<InterviewReportEvidence>.unmodifiable(
          evidence.map(_evidence),
        ),
      );
    }),
  );
}

InterviewReportEvidence _evidence(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'evidence_ref_id',
      'turn_id',
      'start_utf8_byte',
      'end_utf8_byte',
      'original_excerpt',
    },
  );
  final start = _nonNegativeInt(root['start_utf8_byte']);
  final end = _positiveInt(root['end_utf8_byte']);
  final excerpt = _text(
    root['original_excerpt'],
    maxBytes: _maximumSourceTextBytes,
  );
  if (end <= start || utf8.encode(excerpt).length != end - start) {
    throw const InterviewReportDecodeException();
  }
  return InterviewReportEvidence(
    evidenceRefId: _identifier(root['evidence_ref_id']),
    turnId: _identifier(root['turn_id']),
    startUtf8Byte: start,
    endUtf8Byte: end,
    originalExcerpt: excerpt,
  );
}

List<InterviewReportQuestion> _questions(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 64) {
    throw const InterviewReportDecodeException();
  }
  return List<InterviewReportQuestion>.unmodifiable(value.map(_question));
}

InterviewReportQuestion _question(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'question_id',
      'question_type',
      'opportunity_status',
      'assessment_status',
      'question_text',
      'evidence_ref_ids',
      'dimension_findings',
    },
    optional: const {
      'parent_question_id',
      'response_turn_id',
      'confirmed_transcript',
    },
  );
  final opportunity = _opportunity(root['opportunity_status']);
  final assessment = _assessment(root['assessment_status']);
  final hasResponseTurn = root.containsKey('response_turn_id');
  final hasTranscript = root.containsKey('confirmed_transcript');
  final evidenceRefIds = _uniqueIdentifiers(root['evidence_ref_ids']);
  if (opportunity == InterviewReportOpportunityStatus.provided) {
    if (assessment != InterviewReportAssessmentStatus.assessed ||
        !hasResponseTurn ||
        !hasTranscript ||
        evidenceRefIds.isEmpty) {
      throw const InterviewReportDecodeException();
    }
  } else if (assessment != InterviewReportAssessmentStatus.notAssessed ||
      hasResponseTurn ||
      hasTranscript ||
      evidenceRefIds.isNotEmpty) {
    throw const InterviewReportDecodeException();
  }
  final dimensions = root['dimension_findings'];
  if (dimensions is! List<Object?> ||
      dimensions.length != _dimensionOrder.length) {
    throw const InterviewReportDecodeException();
  }
  final dimensionFindings = <InterviewReportQuestionDimensionFindings>[
    for (var index = 0; index < dimensions.length; index++)
      _questionDimensionFindings(
        dimensions[index],
        expected: _dimensionOrder[index],
      ),
  ];
  if (opportunity == InterviewReportOpportunityStatus.notProvided &&
      dimensionFindings.any(
        (result) =>
            result.strengthFindingIds.isNotEmpty ||
            result.improvementFindingIds.isNotEmpty ||
            result.recommendedExpressionFindingIds.isNotEmpty,
      )) {
    throw const InterviewReportDecodeException();
  }
  return InterviewReportQuestion(
    questionId: _identifier(root['question_id']),
    questionType: _questionType(root['question_type']),
    parentQuestionId: root.containsKey('parent_question_id')
        ? _identifier(root['parent_question_id'])
        : null,
    opportunityStatus: opportunity,
    assessmentStatus: assessment,
    questionText: _text(
      root['question_text'],
      maxBytes: _maximumSourceTextBytes,
    ),
    responseTurnId: hasResponseTurn
        ? _identifier(root['response_turn_id'])
        : null,
    confirmedTranscript: hasTranscript
        ? _text(root['confirmed_transcript'], maxBytes: _maximumSourceTextBytes)
        : null,
    evidenceRefIds: evidenceRefIds,
    dimensionFindings:
        List<InterviewReportQuestionDimensionFindings>.unmodifiable(
          dimensionFindings,
        ),
  );
}

InterviewReportQuestionDimensionFindings _questionDimensionFindings(
  Object? value, {
  required InterviewReportDimensionId expected,
}) {
  final root = _exactObject(
    value,
    required: const {
      'dimension_id',
      'strength_finding_ids',
      'improvement_finding_ids',
      'recommended_expression_finding_ids',
    },
  );
  final id = _dimensionId(root['dimension_id']);
  if (id != expected) {
    throw const InterviewReportDecodeException();
  }
  return InterviewReportQuestionDimensionFindings(
    dimensionId: id,
    strengthFindingIds: _uniqueIdentifiers(
      root['strength_finding_ids'],
      maximumItems: 3,
    ),
    improvementFindingIds: _uniqueIdentifiers(
      root['improvement_finding_ids'],
      maximumItems: 3,
    ),
    recommendedExpressionFindingIds: _uniqueIdentifiers(
      root['recommended_expression_finding_ids'],
      maximumItems: 3,
    ),
  );
}

List<InterviewReportPriorityAction> _priorityActions(Object? value) {
  if (value is! List<Object?> || value.length > 3) {
    throw const InterviewReportDecodeException();
  }
  final findingIds = <String>{};
  return List<InterviewReportPriorityAction>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {'dimension_id', 'finding_id'},
      );
      final findingId = _identifier(root['finding_id']);
      if (!findingIds.add(findingId)) {
        throw const InterviewReportDecodeException();
      }
      return InterviewReportPriorityAction(
        dimensionId: _dimensionId(root['dimension_id']),
        findingId: findingId,
      );
    }),
  );
}

InterviewReportStableFailure _failure(Object? value) {
  final root = _exactObject(
    value,
    required: const {'reason_code', 'retryable'},
  );
  final reasonCode = root['reason_code'];
  final retryable = root['retryable'];
  if (reasonCode is! String ||
      !_failureReasonCodes.contains(reasonCode) ||
      retryable is! bool ||
      (reasonCode == 'INTERNAL_RETRYABLE') != retryable) {
    throw const InterviewReportDecodeException();
  }
  return InterviewReportStableFailure(
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
    throw const InterviewReportDecodeException();
  }
  final keys = value.keys.toSet();
  if (!keys.containsAll(required) ||
      keys.any((key) => !required.contains(key) && !optional.contains(key))) {
    throw const InterviewReportDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.length > 128 ||
      value != value.trim() ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value)) {
    throw const InterviewReportDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String ||
      !RegExp(
        r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
      ).hasMatch(value)) {
    throw const InterviewReportDecodeException();
  }
  return value;
}

String _text(Object? value, {required int maxBytes}) {
  if (value is! String ||
      value.isEmpty ||
      value != value.trim() ||
      utf8.encode(value).length > maxBytes) {
    throw const InterviewReportDecodeException();
  }
  return value;
}

int _positiveInt(Object? value) {
  if (value is! int || value < 1) {
    throw const InterviewReportDecodeException();
  }
  return value;
}

int _nonNegativeInt(Object? value) {
  if (value is! int || value < 0) {
    throw const InterviewReportDecodeException();
  }
  return value;
}

double _ratio(Object? value) {
  if (value is! num || !value.isFinite || value < 0 || value > 1) {
    throw const InterviewReportDecodeException();
  }
  return value.toDouble();
}

List<String> _uniqueIdentifiers(Object? value, {int maximumItems = 64}) {
  if (value is! List<Object?> || value.length > maximumItems) {
    throw const InterviewReportDecodeException();
  }
  final seen = <String>{};
  return List<String>.unmodifiable(
    value.map((item) {
      final result = _identifier(item);
      if (!seen.add(result)) {
        throw const InterviewReportDecodeException();
      }
      return result;
    }),
  );
}

List<String> _reasonCodes(Object? value) {
  final codes = _uniqueIdentifiers(value);
  if (codes.any((code) => !_reasonCodesAllowed.contains(code))) {
    throw const InterviewReportDecodeException();
  }
  return codes;
}

InterviewReportEvaluationStatus _evaluationStatus(Object? value) =>
    switch (value) {
      'QUEUED' => InterviewReportEvaluationStatus.queued,
      'RUNNING' => InterviewReportEvaluationStatus.running,
      'READY' => InterviewReportEvaluationStatus.ready,
      'FAILED' => InterviewReportEvaluationStatus.failed,
      _ => throw const InterviewReportDecodeException(),
    };

InterviewReportScoreabilityStatus _scoreability(Object? value) =>
    switch (value) {
      'PROVISIONAL' => InterviewReportScoreabilityStatus.provisional,
      'INSUFFICIENT' => InterviewReportScoreabilityStatus.insufficient,
      _ => throw const InterviewReportDecodeException(),
    };

InterviewReportGateStatus _gate(Object? value) => switch (value) {
  'FEEDBACK_ONLY' => InterviewReportGateStatus.feedbackOnly,
  'BLOCKED' => InterviewReportGateStatus.blocked,
  _ => throw const InterviewReportDecodeException(),
};

InterviewReportReadinessLevel _readiness(Object? value) => switch (value) {
  'NOT_ASSESSED' => InterviewReportReadinessLevel.notAssessed,
  _ => throw const InterviewReportDecodeException(),
};

InterviewReportOpportunityStatus _opportunity(Object? value) => switch (value) {
  'PROVIDED' => InterviewReportOpportunityStatus.provided,
  'NOT_PROVIDED' => InterviewReportOpportunityStatus.notProvided,
  _ => throw const InterviewReportDecodeException(),
};

InterviewReportQuestionType _questionType(Object? value) => switch (value) {
  'PRIMARY' => InterviewReportQuestionType.primary,
  'FOLLOW_UP' => InterviewReportQuestionType.followUp,
  _ => throw const InterviewReportDecodeException(),
};

InterviewReportAssessmentStatus _assessment(Object? value) => switch (value) {
  'ASSESSED' => InterviewReportAssessmentStatus.assessed,
  'NOT_ASSESSED' => InterviewReportAssessmentStatus.notAssessed,
  _ => throw const InterviewReportDecodeException(),
};

InterviewReportDimensionId _dimensionId(Object? value) => switch (value) {
  'INTERVIEW_RELEVANCE' => InterviewReportDimensionId.relevance,
  'INTERVIEW_STRUCTURE' => InterviewReportDimensionId.structure,
  'INTERVIEW_EVIDENCE' => InterviewReportDimensionId.evidence,
  'INTERVIEW_PROFESSIONAL' => InterviewReportDimensionId.professional,
  'INTERVIEW_INTERACTION' => InterviewReportDimensionId.interaction,
  _ => throw const InterviewReportDecodeException(),
};

bool _sameSet(Set<String> left, Set<String> right) =>
    left.length == right.length && left.containsAll(right);

bool _containsExcerpt(String response, InterviewReportEvidence evidence) {
  final bytes = utf8.encode(response);
  if (evidence.endUtf8Byte > bytes.length) {
    return false;
  }
  try {
    return utf8.decode(
          bytes.sublist(evidence.startUtf8Byte, evidence.endUtf8Byte),
          allowMalformed: false,
        ) ==
        evidence.originalExcerpt;
  } on FormatException {
    return false;
  }
}

bool _validDimensionGate({
  required InterviewReportScoreabilityStatus rootScoreability,
  required InterviewReportGateStatus rootGate,
  required InterviewReportDimension dimension,
}) {
  if (rootScoreability == InterviewReportScoreabilityStatus.insufficient) {
    return rootGate == InterviewReportGateStatus.blocked &&
        dimension.scoreabilityStatus ==
            InterviewReportScoreabilityStatus.insufficient &&
        dimension.gateStatus == InterviewReportGateStatus.blocked;
  }
  return rootScoreability == InterviewReportScoreabilityStatus.provisional &&
      rootGate == InterviewReportGateStatus.feedbackOnly &&
      ((dimension.scoreabilityStatus ==
                  InterviewReportScoreabilityStatus.provisional &&
              dimension.gateStatus == InterviewReportGateStatus.feedbackOnly) ||
          (dimension.scoreabilityStatus ==
                  InterviewReportScoreabilityStatus.insufficient &&
              dimension.gateStatus == InterviewReportGateStatus.blocked));
}

const _dimensionOrder = <InterviewReportDimensionId>[
  InterviewReportDimensionId.relevance,
  InterviewReportDimensionId.structure,
  InterviewReportDimensionId.evidence,
  InterviewReportDimensionId.professional,
  InterviewReportDimensionId.interaction,
];

const _reasonCodesAllowed = <String>{
  'ASR_CONFIDENCE_UNAVAILABLE',
  'INSUFFICIENT_EVIDENCE',
  'OPPORTUNITY_NOT_PROVIDED',
};

const _failureReasonCodes = <String>{
  'POLICY_VIOLATION',
  'EVIDENCE_REF_INVALID',
  'VERSION_CONFLICT',
  'INTERNAL_RETRYABLE',
  'INTERNAL_NON_RETRYABLE',
};

const _expectedReadinessNotice =
    'Practice feedback only; not a hiring decision or probability.';
const _maximumFeedbackTextBytes = 2048;
const _maximumSourceTextBytes = 16384;
