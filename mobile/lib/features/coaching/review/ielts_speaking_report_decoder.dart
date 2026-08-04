import 'dart:convert';

import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';

final class IeltsSpeakingReportDecodeException implements Exception {
  const IeltsSpeakingReportDecodeException();

  @override
  String toString() => 'IeltsSpeakingReportDecodeException';
}

IeltsSpeakingReportEnvelope decodeIeltsSpeakingReportJson(String body) {
  try {
    return decodeIeltsSpeakingReport(jsonDecode(body));
  } on FormatException {
    throw const IeltsSpeakingReportDecodeException();
  }
}

IeltsSpeakingReportEnvelope decodeIeltsSpeakingReport(Object? value) {
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
  final evaluationStatus = _evaluationStatus(root['evaluation_status']);
  final statusUrl = root['status_url'];
  if (root['is_final'] != false ||
      statusUrl is! String ||
      statusUrl !=
          '/v1/practice-sessions/$practiceSessionId/ielts-speaking-report') {
    throw const IeltsSpeakingReportDecodeException();
  }

  final hasReport = root.containsKey('report');
  final hasFailure = root.containsKey('stable_failure');
  if ((hasReport && root['report'] == null) ||
      (hasFailure && root['stable_failure'] == null)) {
    throw const IeltsSpeakingReportDecodeException();
  }
  IeltsSpeakingReport? report;
  IeltsSpeakingReportStableFailure? failure;
  switch (evaluationStatus) {
    case IeltsSpeakingReportEvaluationStatus.queued:
    case IeltsSpeakingReportEvaluationStatus.running:
      if (hasReport || hasFailure) {
        throw const IeltsSpeakingReportDecodeException();
      }
    case IeltsSpeakingReportEvaluationStatus.ready:
      if (!hasReport || hasFailure) {
        throw const IeltsSpeakingReportDecodeException();
      }
      report = _report(root['report']);
    case IeltsSpeakingReportEvaluationStatus.failed:
      if (hasReport || !hasFailure) {
        throw const IeltsSpeakingReportDecodeException();
      }
      failure = _failure(root['stable_failure']);
  }

  return IeltsSpeakingReportEnvelope(
    practiceSessionId: practiceSessionId,
    evaluationId: _uuid(root['evaluation_id']),
    evaluationRevisionId: _uuid(root['evaluation_revision_id']),
    revision: _positiveInt(root['revision']),
    evaluationStatus: evaluationStatus,
    isFinal: false,
    statusUrl: statusUrl,
    report: report,
    stableFailure: failure,
  );
}

IeltsSpeakingReport _report(Object? value) {
  final root = _exactObject(
    value,
    required: const {
      'schema_version',
      'disclaimer_code',
      'disclaimer',
      'scoreability_status',
      'gate_status',
      'criteria',
      'speaking_overall',
      'part_reviews',
      'questions',
      'target_plan',
      'priority_actions',
    },
  );
  if (root['schema_version'] != 'ielts-speaking-report/v1' ||
      root['disclaimer_code'] != 'AI_PRACTICE_ESTIMATE_NOT_OFFICIAL_IELTS' ||
      root['disclaimer'] != 'AI 练习估分，非 IELTS 官方成绩。') {
    throw const IeltsSpeakingReportDecodeException();
  }
  final scoreability = _scoreability(root['scoreability_status']);
  final gate = _gate(root['gate_status']);
  if ((scoreability == IeltsSpeakingReportScoreabilityStatus.provisional &&
          gate != IeltsSpeakingReportGateStatus.feedbackOnly) ||
      (scoreability == IeltsSpeakingReportScoreabilityStatus.insufficient &&
          gate != IeltsSpeakingReportGateStatus.blocked)) {
    throw const IeltsSpeakingReportDecodeException();
  }

  final criteria = _criteria(root['criteria']);
  final findings =
      <
        String,
        ({
          IeltsSpeakingCriterionId criterion,
          String kind,
          IeltsSpeakingFinding finding,
        })
      >{};
  final evidenceTurns = <String, String>{};
  final evidenceAnchors = <IeltsSpeakingEvidence>[];
  var hasProvisionalCriterion = false;
  for (final criterion in criteria) {
    if (!_validCriterionGate(
      rootScoreability: scoreability,
      rootGate: gate,
      criterion: criterion,
    )) {
      throw const IeltsSpeakingReportDecodeException();
    }
    hasProvisionalCriterion |=
        criterion.scoreabilityStatus ==
        IeltsSpeakingReportScoreabilityStatus.provisional;
    final criterionEvidence = <String>{};
    for (final group in <({String kind, List<IeltsSpeakingFinding> findings})>[
      (kind: 'strength', findings: criterion.strengths),
      (kind: 'improvement', findings: criterion.improvements),
      (kind: 'upgrade_example', findings: criterion.upgradeExamples),
    ]) {
      for (final finding in group.findings) {
        if (findings.containsKey(finding.id)) {
          throw const IeltsSpeakingReportDecodeException();
        }
        findings[finding.id] = (
          criterion: criterion.id,
          kind: group.kind,
          finding: finding,
        );
        for (final evidence in finding.evidence) {
          criterionEvidence.add(evidence.evidenceRefId);
          final existingTurn = evidenceTurns[evidence.evidenceRefId];
          if (existingTurn != null && existingTurn != evidence.turnId) {
            throw const IeltsSpeakingReportDecodeException();
          }
          evidenceTurns[evidence.evidenceRefId] = evidence.turnId;
          evidenceAnchors.add(evidence);
        }
      }
    }
    if (!_sameSet(criterion.evidenceRefIds.toSet(), criterionEvidence)) {
      throw const IeltsSpeakingReportDecodeException();
    }
  }
  if ((scoreability == IeltsSpeakingReportScoreabilityStatus.provisional &&
          !hasProvisionalCriterion) ||
      (scoreability == IeltsSpeakingReportScoreabilityStatus.insufficient &&
          hasProvisionalCriterion)) {
    throw const IeltsSpeakingReportDecodeException();
  }

  final questions = _questions(root['questions']);
  final questionsByTurnId = <String, IeltsSpeakingQuestionReview>{};
  final questionsByEvidenceRefId = <String, IeltsSpeakingQuestionReview>{};
  final questionIds = <String>{};
  for (final question in questions) {
    if (!questionIds.add(question.questionId)) {
      throw const IeltsSpeakingReportDecodeException();
    }
    final turnId = question.responseTurnId;
    if (turnId != null &&
        questionsByTurnId.putIfAbsent(turnId, () => question) != question) {
      throw const IeltsSpeakingReportDecodeException();
    }
    for (final refId in question.evidenceRefIds) {
      final evidenceTurn = evidenceTurns[refId];
      if (turnId == null ||
          questionsByEvidenceRefId.putIfAbsent(refId, () => question) !=
              question ||
          (evidenceTurn != null && evidenceTurn != turnId)) {
        throw const IeltsSpeakingReportDecodeException();
      }
    }
    for (final result in question.criterionFindings) {
      for (final group in <({String kind, List<String> ids})>[
        (kind: 'strength', ids: result.strengthFindingIds),
        (kind: 'improvement', ids: result.improvementFindingIds),
        (kind: 'upgrade_example', ids: result.upgradeExampleFindingIds),
      ]) {
        for (final id in group.ids) {
          final resolved = findings[id];
          if (resolved == null ||
              resolved.criterion != result.criterionId ||
              resolved.kind != group.kind ||
              !resolved.finding.evidence.any(
                (item) => question.evidenceRefIds.contains(item.evidenceRefId),
              )) {
            throw const IeltsSpeakingReportDecodeException();
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
      throw const IeltsSpeakingReportDecodeException();
    }
  }

  final partReviews = _partReviews(root['part_reviews']);
  for (var index = 0; index < partReviews.length; index++) {
    final part = partReviews[index];
    final expectedIndexes = _partQuestionIndexes[index];
    if (!_sameIntList(part.questionIndexes, expectedIndexes)) {
      throw const IeltsSpeakingReportDecodeException();
    }
    final partEvidence = <String>{
      for (final questionIndex in expectedIndexes)
        ...questions[questionIndex - 1].evidenceRefIds,
    };
    if (!_sameSet(part.evidenceRefIds.toSet(), partEvidence)) {
      throw const IeltsSpeakingReportDecodeException();
    }
    for (final group in <({String kind, List<String> ids})>[
      (kind: 'strength', ids: part.strengthFindingIds),
      (kind: 'improvement', ids: part.improvementFindingIds),
      (kind: 'upgrade_example', ids: part.upgradeExampleFindingIds),
    ]) {
      for (final id in group.ids) {
        final resolved = findings[id];
        if (resolved == null ||
            resolved.kind != group.kind ||
            !resolved.finding.evidence.any(
              (item) => partEvidence.contains(item.evidenceRefId),
            )) {
          throw const IeltsSpeakingReportDecodeException();
        }
      }
    }
  }

  final priorityActions = _priorityActions(root['priority_actions']);
  for (final action in priorityActions) {
    final resolved = findings[action.findingId];
    if (resolved == null ||
        resolved.criterion != action.criterionId ||
        resolved.kind != 'improvement') {
      throw const IeltsSpeakingReportDecodeException();
    }
  }
  if (scoreability == IeltsSpeakingReportScoreabilityStatus.insufficient &&
      (priorityActions.isNotEmpty || findings.isNotEmpty)) {
    throw const IeltsSpeakingReportDecodeException();
  }

  final speakingOverall = _exactObject(
    root['speaking_overall'],
    required: const {'status'},
  );
  if (speakingOverall['status'] != 'NOT_AVAILABLE') {
    throw const IeltsSpeakingReportDecodeException();
  }
  final targetPlan = _exactObject(
    root['target_plan'],
    required: const {'status'},
  );
  if (targetPlan['status'] != 'NOT_CONFIGURED') {
    throw const IeltsSpeakingReportDecodeException();
  }
  return IeltsSpeakingReport(
    schemaVersion: 'ielts-speaking-report/v1',
    disclaimerCode: 'AI_PRACTICE_ESTIMATE_NOT_OFFICIAL_IELTS',
    disclaimer: 'AI 练习估分，非 IELTS 官方成绩。',
    scoreabilityStatus: scoreability,
    gateStatus: gate,
    criteria: criteria,
    speakingOverallStatus: IeltsSpeakingOverallStatus.notAvailable,
    partReviews: partReviews,
    questions: questions,
    targetPlanStatus: IeltsSpeakingTargetPlanStatus.notConfigured,
    priorityActions: priorityActions,
  );
}

List<IeltsSpeakingCriterion> _criteria(Object? value) {
  if (value is! List<Object?> || value.length != _criterionOrder.length) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return List<IeltsSpeakingCriterion>.unmodifiable([
    for (var index = 0; index < value.length; index++)
      _criterion(value[index], expected: _criterionOrder[index]),
  ]);
}

IeltsSpeakingCriterion _criterion(
  Object? value, {
  required IeltsSpeakingCriterionId expected,
}) {
  final root = _exactObject(
    value,
    required: const {
      'criterion_id',
      'scoreability_status',
      'gate_status',
      'coverage',
      'confidence',
      'reason_codes',
      'evidence_ref_ids',
      'strengths',
      'improvements',
      'upgrade_examples',
    },
    optional: const {'estimated_band', 'band_descriptor'},
  );
  final id = _criterionId(root['criterion_id']);
  if (id != expected) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final scoreability = _scoreability(root['scoreability_status']);
  final gate = _gate(root['gate_status']);
  final reasonCodes = _reasonCodes(root['reason_codes']);
  final hasBand = root.containsKey('estimated_band');
  final hasDescriptor = root.containsKey('band_descriptor');
  if (hasBand != hasDescriptor ||
      (hasBand &&
          (root['estimated_band'] is! int ||
              (root['estimated_band'] as int) < 1 ||
              (root['estimated_band'] as int) > 9))) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final estimatedBand = hasBand ? root['estimated_band']! as int : null;
  final descriptor = hasDescriptor
      ? _text(root['band_descriptor'], maxBytes: _maximumFeedbackTextBytes)
      : null;
  final evidenceRefIds = _uniqueIdentifiers(
    root['evidence_ref_ids'],
    maximumItems: 128,
  );
  final confidence = _ratio(root['confidence']);
  if (confidence != 0) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final strengths = _findings(root['strengths']);
  final improvements = _findings(root['improvements']);
  final upgradeExamples = _findings(root['upgrade_examples']);

  final insufficient =
      scoreability == IeltsSpeakingReportScoreabilityStatus.insufficient;
  if (insufficient) {
    if (gate != IeltsSpeakingReportGateStatus.blocked ||
        hasBand ||
        evidenceRefIds.isNotEmpty ||
        strengths.isNotEmpty ||
        improvements.isNotEmpty ||
        upgradeExamples.isNotEmpty ||
        reasonCodes.any((code) => !_insufficientReasonCodes.contains(code))) {
      throw const IeltsSpeakingReportDecodeException();
    }
  } else if (gate != IeltsSpeakingReportGateStatus.feedbackOnly ||
      evidenceRefIds.isEmpty ||
      (strengths.isEmpty && improvements.isEmpty)) {
    throw const IeltsSpeakingReportDecodeException();
  }

  switch (id) {
    case IeltsSpeakingCriterionId.fluencyAndCoherence:
      if (hasBand ||
          (!insufficient &&
              !reasonCodes.contains('FLUENCY_TIMING_UNAVAILABLE'))) {
        throw const IeltsSpeakingReportDecodeException();
      }
    case IeltsSpeakingCriterionId.lexicalResource:
    case IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy:
      if ((!insufficient &&
              (!hasBand ||
                  !reasonCodes.contains('ASR_CONFIDENCE_UNAVAILABLE'))) ||
          (insufficient && hasBand)) {
        throw const IeltsSpeakingReportDecodeException();
      }
    case IeltsSpeakingCriterionId.pronunciation:
      if (!insufficient ||
          gate != IeltsSpeakingReportGateStatus.blocked ||
          hasBand ||
          reasonCodes.length != 1 ||
          reasonCodes.single != 'PRONUNCIATION_ARTIFACT_UNAVAILABLE') {
        throw const IeltsSpeakingReportDecodeException();
      }
  }
  return IeltsSpeakingCriterion(
    id: id,
    scoreabilityStatus: scoreability,
    gateStatus: gate,
    estimatedBand: estimatedBand,
    bandDescriptor: descriptor,
    coverage: _ratio(root['coverage']),
    confidence: confidence,
    reasonCodes: reasonCodes,
    evidenceRefIds: evidenceRefIds,
    strengths: strengths,
    improvements: improvements,
    upgradeExamples: upgradeExamples,
  );
}

List<IeltsSpeakingFinding> _findings(Object? value) {
  if (value is! List<Object?> || value.length > 3) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return List<IeltsSpeakingFinding>.unmodifiable(value.map(_finding));
}

IeltsSpeakingFinding _finding(Object? value) {
  final root = _exactObject(
    value,
    required: const {'finding_id', 'message', 'evidence'},
    optional: const {'suggestion'},
  );
  final evidenceValue = root['evidence'];
  if (evidenceValue is! List<Object?> ||
      evidenceValue.isEmpty ||
      evidenceValue.length > 8) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return IeltsSpeakingFinding(
    id: _identifier(root['finding_id']),
    message: _text(root['message'], maxBytes: _maximumFeedbackTextBytes),
    suggestion: root.containsKey('suggestion')
        ? _text(root['suggestion'], maxBytes: _maximumFeedbackTextBytes)
        : null,
    evidence: List<IeltsSpeakingEvidence>.unmodifiable(
      evidenceValue.map(_evidence),
    ),
  );
}

IeltsSpeakingEvidence _evidence(Object? value) {
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
  if (start >= end) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return IeltsSpeakingEvidence(
    evidenceRefId: _identifier(root['evidence_ref_id']),
    turnId: _identifier(root['turn_id']),
    startUtf8Byte: start,
    endUtf8Byte: end,
    originalExcerpt: _text(
      root['original_excerpt'],
      maxBytes: _maximumSourceTextBytes,
    ),
  );
}

List<IeltsSpeakingQuestionReview> _questions(Object? value) {
  if (value is! List<Object?> || value.length != 14) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return List<IeltsSpeakingQuestionReview>.unmodifiable([
    for (var index = 0; index < value.length; index++)
      _question(value[index], expectedIndex: index + 1),
  ]);
}

IeltsSpeakingQuestionReview _question(
  Object? value, {
  required int expectedIndex,
}) {
  final root = _exactObject(
    value,
    required: const {
      'question_id',
      'part_id',
      'index',
      'question_text',
      'opportunity_status',
      'assessment_status',
      'evidence_ref_ids',
      'criterion_findings',
    },
    optional: const {'confirmed_transcript', 'response_turn_id'},
  );
  if (root['index'] != expectedIndex) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final part = _partId(root['part_id']);
  if (part != _partForIndex(expectedIndex)) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final opportunity = _opportunity(root['opportunity_status']);
  final assessment = _assessment(root['assessment_status']);
  final evidenceRefIds = _uniqueIdentifiers(root['evidence_ref_ids']);
  final hasTranscript = root.containsKey('confirmed_transcript');
  final hasTurn = root.containsKey('response_turn_id');
  if (hasTranscript != hasTurn ||
      (opportunity == IeltsSpeakingOpportunityStatus.provided &&
          (!hasTranscript ||
              evidenceRefIds.isEmpty ||
              assessment != IeltsSpeakingAssessmentStatus.assessed)) ||
      (opportunity == IeltsSpeakingOpportunityStatus.notProvided &&
          (hasTranscript ||
              evidenceRefIds.isNotEmpty ||
              assessment != IeltsSpeakingAssessmentStatus.notAssessed)) ||
      (assessment == IeltsSpeakingAssessmentStatus.assessed &&
          opportunity != IeltsSpeakingOpportunityStatus.provided)) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final criterionFindings = _questionCriterionFindings(
    root['criterion_findings'],
  );
  if (assessment == IeltsSpeakingAssessmentStatus.notAssessed &&
      criterionFindings.any(
        (item) =>
            item.strengthFindingIds.isNotEmpty ||
            item.improvementFindingIds.isNotEmpty ||
            item.upgradeExampleFindingIds.isNotEmpty,
      )) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return IeltsSpeakingQuestionReview(
    questionId: _identifier(root['question_id']),
    partId: part,
    index: expectedIndex,
    questionText: _text(
      root['question_text'],
      maxBytes: _maximumSourceTextBytes,
    ),
    opportunityStatus: opportunity,
    assessmentStatus: assessment,
    confirmedTranscript: hasTranscript
        ? _text(root['confirmed_transcript'], maxBytes: _maximumSourceTextBytes)
        : null,
    responseTurnId: hasTurn ? _identifier(root['response_turn_id']) : null,
    evidenceRefIds: evidenceRefIds,
    criterionFindings: criterionFindings,
  );
}

List<IeltsSpeakingQuestionCriterionFindings> _questionCriterionFindings(
  Object? value,
) {
  if (value is! List<Object?> || value.length != _criterionOrder.length) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return List<IeltsSpeakingQuestionCriterionFindings>.unmodifiable([
    for (var index = 0; index < value.length; index++)
      _questionCriterionFinding(value[index], expected: _criterionOrder[index]),
  ]);
}

IeltsSpeakingQuestionCriterionFindings _questionCriterionFinding(
  Object? value, {
  required IeltsSpeakingCriterionId expected,
}) {
  final root = _exactObject(
    value,
    required: const {
      'criterion_id',
      'strength_finding_ids',
      'improvement_finding_ids',
      'upgrade_example_finding_ids',
    },
  );
  if (_criterionId(root['criterion_id']) != expected) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final result = IeltsSpeakingQuestionCriterionFindings(
    criterionId: expected,
    strengthFindingIds: _uniqueIdentifiers(
      root['strength_finding_ids'],
      maximumItems: 3,
    ),
    improvementFindingIds: _uniqueIdentifiers(
      root['improvement_finding_ids'],
      maximumItems: 3,
    ),
    upgradeExampleFindingIds: _uniqueIdentifiers(
      root['upgrade_example_finding_ids'],
      maximumItems: 3,
    ),
  );
  if (expected == IeltsSpeakingCriterionId.pronunciation &&
      (result.strengthFindingIds.isNotEmpty ||
          result.improvementFindingIds.isNotEmpty ||
          result.upgradeExampleFindingIds.isNotEmpty)) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return result;
}

List<IeltsSpeakingPartReview> _partReviews(Object? value) {
  if (value is! List<Object?> || value.length != _partOrder.length) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return List<IeltsSpeakingPartReview>.unmodifiable([
    for (var index = 0; index < value.length; index++)
      _partReview(value[index], expected: _partOrder[index]),
  ]);
}

IeltsSpeakingPartReview _partReview(
  Object? value, {
  required IeltsSpeakingPartId expected,
}) {
  final root = _exactObject(
    value,
    required: const {
      'part_id',
      'question_indexes',
      'evidence_ref_ids',
      'strength_finding_ids',
      'improvement_finding_ids',
      'upgrade_example_finding_ids',
    },
  );
  if (_partId(root['part_id']) != expected) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final indexValue = root['question_indexes'];
  if (indexValue is! List<Object?> ||
      indexValue.isEmpty ||
      indexValue.length > 8 ||
      indexValue.any((item) => item is! int || item < 1 || item > 14) ||
      indexValue.toSet().length != indexValue.length) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return IeltsSpeakingPartReview(
    id: expected,
    questionIndexes: List<int>.unmodifiable(indexValue.cast<int>()),
    evidenceRefIds: _uniqueIdentifiers(
      root['evidence_ref_ids'],
      maximumItems: 128,
    ),
    strengthFindingIds: _uniqueIdentifiers(
      root['strength_finding_ids'],
      maximumItems: 3,
    ),
    improvementFindingIds: _uniqueIdentifiers(
      root['improvement_finding_ids'],
      maximumItems: 3,
    ),
    upgradeExampleFindingIds: _uniqueIdentifiers(
      root['upgrade_example_finding_ids'],
      maximumItems: 3,
    ),
  );
}

List<IeltsSpeakingPriorityAction> _priorityActions(Object? value) {
  if (value is! List<Object?> || value.length > 3) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final keys = <String>{};
  return List<IeltsSpeakingPriorityAction>.unmodifiable(
    value.map((item) {
      final root = _exactObject(
        item,
        required: const {'criterion_id', 'finding_id'},
      );
      final result = IeltsSpeakingPriorityAction(
        criterionId: _criterionId(root['criterion_id']),
        findingId: _identifier(root['finding_id']),
      );
      if (!keys.add('${result.criterionId.name}:${result.findingId}')) {
        throw const IeltsSpeakingReportDecodeException();
      }
      return result;
    }),
  );
}

IeltsSpeakingReportStableFailure _failure(Object? value) {
  final root = _exactObject(
    value,
    required: const {'reason_code', 'retryable'},
  );
  final reason = root['reason_code'];
  final retryable = root['retryable'];
  if (reason is! String ||
      !_failureReasonCodes.contains(reason) ||
      retryable is! bool ||
      (reason == 'INTERNAL_RETRYABLE') != retryable) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return IeltsSpeakingReportStableFailure(
    reasonCode: reason,
    retryable: retryable,
  );
}

Map<String, Object?> _exactObject(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const {},
}) {
  if (value is! Map<String, Object?>) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final keys = value.keys.toSet();
  if (!keys.containsAll(required) ||
      keys.any((key) => !required.contains(key) && !optional.contains(key))) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value;
}

String _identifier(Object? value) {
  if (value is! String ||
      value.isEmpty ||
      value.length > 128 ||
      value != value.trim() ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value)) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value;
}

String _uuid(Object? value) {
  if (value is! String ||
      !RegExp(
        r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
      ).hasMatch(value)) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value;
}

String _text(Object? value, {required int maxBytes}) {
  if (value is! String ||
      value.isEmpty ||
      value != value.trim() ||
      utf8.encode(value).length > maxBytes) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value;
}

int _positiveInt(Object? value) {
  if (value is! int || value < 1) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value;
}

int _nonNegativeInt(Object? value) {
  if (value is! int || value < 0) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value;
}

double _ratio(Object? value) {
  if (value is! num || !value.isFinite || value < 0 || value > 1) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return value.toDouble();
}

List<String> _uniqueIdentifiers(Object? value, {int maximumItems = 64}) {
  if (value is! List<Object?> || value.length > maximumItems) {
    throw const IeltsSpeakingReportDecodeException();
  }
  final seen = <String>{};
  return List<String>.unmodifiable(
    value.map((item) {
      final result = _identifier(item);
      if (!seen.add(result)) {
        throw const IeltsSpeakingReportDecodeException();
      }
      return result;
    }),
  );
}

List<String> _reasonCodes(Object? value) {
  final result = _uniqueIdentifiers(value, maximumItems: 3);
  if (result.isEmpty ||
      result.any((code) => !_reportReasonCodes.contains(code))) {
    throw const IeltsSpeakingReportDecodeException();
  }
  return result;
}

IeltsSpeakingReportEvaluationStatus _evaluationStatus(Object? value) =>
    switch (value) {
      'QUEUED' => IeltsSpeakingReportEvaluationStatus.queued,
      'RUNNING' => IeltsSpeakingReportEvaluationStatus.running,
      'READY' => IeltsSpeakingReportEvaluationStatus.ready,
      'FAILED' => IeltsSpeakingReportEvaluationStatus.failed,
      _ => throw const IeltsSpeakingReportDecodeException(),
    };

IeltsSpeakingReportScoreabilityStatus _scoreability(Object? value) =>
    switch (value) {
      'PROVISIONAL' => IeltsSpeakingReportScoreabilityStatus.provisional,
      'INSUFFICIENT' => IeltsSpeakingReportScoreabilityStatus.insufficient,
      _ => throw const IeltsSpeakingReportDecodeException(),
    };

IeltsSpeakingReportGateStatus _gate(Object? value) => switch (value) {
  'FEEDBACK_ONLY' => IeltsSpeakingReportGateStatus.feedbackOnly,
  'BLOCKED' => IeltsSpeakingReportGateStatus.blocked,
  _ => throw const IeltsSpeakingReportDecodeException(),
};

IeltsSpeakingCriterionId _criterionId(Object? value) => switch (value) {
  'IELTS_FC' => IeltsSpeakingCriterionId.fluencyAndCoherence,
  'IELTS_LR' => IeltsSpeakingCriterionId.lexicalResource,
  'IELTS_GRA' => IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy,
  'IELTS_PR' => IeltsSpeakingCriterionId.pronunciation,
  _ => throw const IeltsSpeakingReportDecodeException(),
};

IeltsSpeakingPartId _partId(Object? value) => switch (value) {
  'PART_1' => IeltsSpeakingPartId.part1,
  'PART_2' => IeltsSpeakingPartId.part2,
  'PART_3' => IeltsSpeakingPartId.part3,
  _ => throw const IeltsSpeakingReportDecodeException(),
};

IeltsSpeakingOpportunityStatus _opportunity(Object? value) => switch (value) {
  'PROVIDED' => IeltsSpeakingOpportunityStatus.provided,
  'NOT_PROVIDED' => IeltsSpeakingOpportunityStatus.notProvided,
  _ => throw const IeltsSpeakingReportDecodeException(),
};

IeltsSpeakingAssessmentStatus _assessment(Object? value) => switch (value) {
  'ASSESSED' => IeltsSpeakingAssessmentStatus.assessed,
  'NOT_ASSESSED' => IeltsSpeakingAssessmentStatus.notAssessed,
  _ => throw const IeltsSpeakingReportDecodeException(),
};

IeltsSpeakingPartId _partForIndex(int index) {
  if (index <= 8) {
    return IeltsSpeakingPartId.part1;
  }
  return index == 9 ? IeltsSpeakingPartId.part2 : IeltsSpeakingPartId.part3;
}

bool _validCriterionGate({
  required IeltsSpeakingReportScoreabilityStatus rootScoreability,
  required IeltsSpeakingReportGateStatus rootGate,
  required IeltsSpeakingCriterion criterion,
}) {
  if (rootScoreability == IeltsSpeakingReportScoreabilityStatus.insufficient) {
    return rootGate == IeltsSpeakingReportGateStatus.blocked &&
        criterion.scoreabilityStatus ==
            IeltsSpeakingReportScoreabilityStatus.insufficient &&
        criterion.gateStatus == IeltsSpeakingReportGateStatus.blocked;
  }
  return rootGate == IeltsSpeakingReportGateStatus.feedbackOnly &&
      ((criterion.scoreabilityStatus ==
                  IeltsSpeakingReportScoreabilityStatus.provisional &&
              criterion.gateStatus ==
                  IeltsSpeakingReportGateStatus.feedbackOnly) ||
          (criterion.scoreabilityStatus ==
                  IeltsSpeakingReportScoreabilityStatus.insufficient &&
              criterion.gateStatus == IeltsSpeakingReportGateStatus.blocked));
}

bool _containsExcerpt(String transcript, IeltsSpeakingEvidence evidence) {
  final bytes = utf8.encode(transcript);
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

bool _sameSet(Set<String> left, Set<String> right) =>
    left.length == right.length && left.containsAll(right);

bool _sameIntList(List<int> left, List<int> right) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) {
      return false;
    }
  }
  return true;
}

const _criterionOrder = <IeltsSpeakingCriterionId>[
  IeltsSpeakingCriterionId.fluencyAndCoherence,
  IeltsSpeakingCriterionId.lexicalResource,
  IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy,
  IeltsSpeakingCriterionId.pronunciation,
];

const _partOrder = <IeltsSpeakingPartId>[
  IeltsSpeakingPartId.part1,
  IeltsSpeakingPartId.part2,
  IeltsSpeakingPartId.part3,
];

const _partQuestionIndexes = <List<int>>[
  <int>[1, 2, 3, 4, 5, 6, 7, 8],
  <int>[9],
  <int>[10, 11, 12, 13, 14],
];

const _reportReasonCodes = <String>{
  'ASR_CONFIDENCE_UNAVAILABLE',
  'FLUENCY_TIMING_UNAVAILABLE',
  'PRONUNCIATION_ARTIFACT_UNAVAILABLE',
  'INSUFFICIENT_EVIDENCE',
  'OPPORTUNITY_NOT_PROVIDED',
};

const _insufficientReasonCodes = <String>{
  'PRONUNCIATION_ARTIFACT_UNAVAILABLE',
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

const _maximumFeedbackTextBytes = 2048;
const _maximumSourceTextBytes = 16384;
