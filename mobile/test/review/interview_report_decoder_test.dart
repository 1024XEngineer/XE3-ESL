import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_decoder.dart';

import 'interview_report_fixture.dart';

void main() {
  test('decodes every canonical Interview report envelope', () {
    final contract = interviewReportContractFixture();

    expect(
      decodeInterviewReport(contract['queued']).evaluationStatus,
      InterviewReportEvaluationStatus.queued,
    );
    expect(
      decodeInterviewReport(contract['running']).evaluationStatus,
      InterviewReportEvaluationStatus.running,
    );
    final ready = decodeInterviewReport(contract['ready']);
    expect(ready.evaluationStatus, InterviewReportEvaluationStatus.ready);
    expect(ready.isFinal, isFalse);
    expect(ready.report?.dimensions, hasLength(5));
    expect(ready.report?.questions, hasLength(2));
    expect(ready.report?.priorityActions, hasLength(3));
    expect(
      ready.report?.readinessLevel,
      InterviewReportReadinessLevel.notAssessed,
    );
    final insufficient = decodeInterviewReport(contract['insufficient']);
    expect(
      insufficient.report?.scoreabilityStatus,
      InterviewReportScoreabilityStatus.insufficient,
    );
    expect(
      insufficient.report?.questions.last.assessmentStatus,
      InterviewReportAssessmentStatus.notAssessed,
    );
    final failed = decodeInterviewReport(contract['failed']);
    expect(failed.evaluationStatus, InterviewReportEvaluationStatus.failed);
    expect(failed.stableFailure?.retryable, isTrue);
  });

  test('decodes a report for a digit-leading Practice session UUID', () {
    final value = cloneInterviewReportFixture(
      interviewReportContractFixture()['ready'],
    );
    const practiceSessionId = '20000000-0000-4000-8000-000000000001';
    value['practice_session_id'] = practiceSessionId;
    value['status_url'] =
        '/v1/practice-sessions/$practiceSessionId/interview-report';

    final decoded = decodeInterviewReport(value);

    expect(decoded.practiceSessionId, practiceSessionId);
  });

  test(
    'rejects unknown and numeric scoring fields at every report boundary',
    () {
      final contract = interviewReportContractFixture();
      final rootScore = cloneInterviewReportFixture(contract['ready']);
      rootScore['overall_score'] = 82;
      final reportScore = cloneInterviewReportFixture(contract['ready']);
      (reportScore['report']! as Map<String, Object?>)['display_score'] = 4.5;
      final dimensionScore = cloneInterviewReportFixture(contract['ready']);
      (((dimensionScore['report']! as Map<String, Object?>)['dimensions']!
                      as List<Object?>)
                  .first
              as Map<String, Object?>)['raw_score'] =
          90;

      for (final value in [rootScore, reportScore, dimensionScore]) {
        expect(
          () => decodeInterviewReport(value),
          throwsA(isA<InterviewReportDecodeException>()),
        );
      }
    },
  );

  test('rejects status payload shape mismatches and a non-report poll URL', () {
    final contract = interviewReportContractFixture();
    final queuedWithReport = cloneInterviewReportFixture(contract['queued']);
    queuedWithReport['report'] = cloneInterviewReportFixture(
      contract['ready'],
    )['report'];
    final readyWithoutReport = cloneInterviewReportFixture(contract['ready'])
      ..remove('report');
    final failedWithReport = cloneInterviewReportFixture(contract['failed']);
    failedWithReport['report'] = cloneInterviewReportFixture(
      contract['ready'],
    )['report'];
    final wrongStatusUrl = cloneInterviewReportFixture(contract['ready']);
    wrongStatusUrl['status_url'] =
        '/v1/evaluations/${wrongStatusUrl['evaluation_id']}';

    for (final value in [
      queuedWithReport,
      readyWithoutReport,
      failedWithReport,
      wrongStatusUrl,
    ]) {
      expect(
        () => decodeInterviewReport(value),
        throwsA(isA<InterviewReportDecodeException>()),
      );
    }
  });

  test('rejects dangling, cross-kind, and cross-question finding refs', () {
    final contract = interviewReportContractFixture();
    final dangling = cloneInterviewReportFixture(contract['ready']);
    _firstQuestionDimension(dangling)['improvement_finding_ids'] = [
      'missing_finding',
    ];
    final wrongKind = cloneInterviewReportFixture(contract['ready']);
    final firstDimension = _firstDimension(wrongKind);
    final improvement =
        (firstDimension['improvements']! as List<Object?>).single
            as Map<String, Object?>;
    firstDimension['strengths'] = [improvement];
    firstDimension['improvements'] = <Object?>[];
    final crossQuestion = cloneInterviewReportFixture(contract['ready']);
    final followupDimensions =
        (((crossQuestion['report']! as Map<String, Object?>)['questions']!
                        as List<Object?>)
                    .last
                as Map<String, Object?>)['dimension_findings']!
            as List<Object?>;
    (followupDimensions.first
        as Map<String, Object?>)['improvement_finding_ids'] = [
      'interview_finding_relevance_001',
    ];

    for (final value in [dangling, wrongKind, crossQuestion]) {
      expect(
        () => decodeInterviewReport(value),
        throwsA(isA<InterviewReportDecodeException>()),
      );
    }
  });

  test('allows a blocked dimension in a provisional report', () {
    final contract = interviewReportContractFixture();
    final value = cloneInterviewReportFixture(contract['ready']);
    (_dimensions(value).last as Map<String, Object?>)
      ..['scoreability_status'] = 'INSUFFICIENT'
      ..['gate_status'] = 'BLOCKED'
      ..['coverage'] = 0
      ..['confidence'] = 0
      ..['reason_codes'] = ['OPPORTUNITY_NOT_PROVIDED']
      ..['evidence_ref_ids'] = <Object?>[]
      ..['strengths'] = <Object?>[]
      ..['improvements'] = <Object?>[]
      ..['recommended_expressions'] = <Object?>[];
    final questions =
        ((value['report']! as Map<String, Object?>)['questions']!
            as List<Object?>);
    final followupFindings =
        ((questions.last as Map<String, Object?>)['dimension_findings']!
            as List<Object?>);
    (followupFindings.last as Map<String, Object?>)['improvement_finding_ids'] =
        <Object?>[];

    final decoded = decodeInterviewReport(value);

    expect(
      decoded.report?.dimensions.last.scoreabilityStatus,
      InterviewReportScoreabilityStatus.insufficient,
    );
    expect(
      decoded.report?.scoreabilityStatus,
      InterviewReportScoreabilityStatus.provisional,
    );
  });

  test('strictly validates question type and PRIMARY parent lineage', () {
    final contract = interviewReportContractFixture();
    final unknownType = cloneInterviewReportFixture(contract['ready']);
    _questions(unknownType).first['question_type'] = 'SECONDARY';
    final primaryWithParent = cloneInterviewReportFixture(contract['ready']);
    _questions(primaryWithParent).first['parent_question_id'] =
        'question_followup_001';
    final followupToFollowup = cloneInterviewReportFixture(contract['ready']);
    _questions(followupToFollowup).last['parent_question_id'] =
        'question_followup_001';

    for (final value in [unknownType, primaryWithParent, followupToFollowup]) {
      expect(
        () => decodeInterviewReport(value),
        throwsA(isA<InterviewReportDecodeException>()),
      );
    }
  });

  test('strictly validates stable failure reasons and retryability', () {
    final contract = interviewReportContractFixture();
    final nonRetryable = cloneInterviewReportFixture(contract['failed']);
    (nonRetryable['stable_failure']! as Map<String, Object?>)
      ..['reason_code'] = 'INTERNAL_NON_RETRYABLE'
      ..['retryable'] = false;
    final decodedNonRetryable = decodeInterviewReport(nonRetryable);
    expect(
      decodedNonRetryable.stableFailure?.reasonCode,
      'INTERNAL_NON_RETRYABLE',
    );
    expect(decodedNonRetryable.stableFailure?.retryable, isFalse);

    final unsupported = cloneInterviewReportFixture(contract['failed']);
    (unsupported['stable_failure']! as Map<String, Object?>)['reason_code'] =
        'AUDIO_UNUSABLE';
    final internalNotRetryable = cloneInterviewReportFixture(
      contract['failed'],
    );
    (internalNotRetryable['stable_failure']!
            as Map<String, Object?>)['retryable'] =
        false;
    final policyRetryable = cloneInterviewReportFixture(contract['failed']);
    (policyRetryable['stable_failure']! as Map<String, Object?>)
      ..['reason_code'] = 'POLICY_VIOLATION'
      ..['retryable'] = true;
    final internalRetryable = cloneInterviewReportFixture(contract['failed']);
    (internalRetryable['stable_failure']! as Map<String, Object?>)
      ..['reason_code'] = 'INTERNAL_NON_RETRYABLE'
      ..['retryable'] = true;

    for (final value in [
      unsupported,
      internalNotRetryable,
      policyRetryable,
      internalRetryable,
    ]) {
      expect(
        () => decodeInterviewReport(value),
        throwsA(isA<InterviewReportDecodeException>()),
      );
    }
  });

  test('enforces UTF-8 budgets for feedback and source text separately', () {
    final contract = interviewReportContractFixture();
    final oversizedFeedback = cloneInterviewReportFixture(contract['ready']);
    final firstImprovement =
        (_firstDimension(oversizedFeedback)['improvements']! as List<Object?>)
                .first
            as Map<String, Object?>;
    firstImprovement['message'] = '好' * 683;
    final oversizedSource = cloneInterviewReportFixture(contract['ready']);
    _questions(oversizedSource).first['question_text'] = '好' * 5462;

    expect(
      () => decodeInterviewReport(oversizedFeedback),
      throwsA(isA<InterviewReportDecodeException>()),
    );
    expect(
      () => decodeInterviewReport(oversizedSource),
      throwsA(isA<InterviewReportDecodeException>()),
    );
  });

  test('allows one evidence ref to carry different valid anchors', () {
    final value = cloneInterviewReportFixture(
      interviewReportContractFixture()['ready'],
    );
    final structure = _dimensions(value)[1] as Map<String, Object?>;
    final finding =
        (structure['improvements']! as List<Object?>).single
            as Map<String, Object?>;
    final anchor =
        (finding['evidence']! as List<Object?>).single as Map<String, Object?>
          ..['start_utf8_byte'] = 0
          ..['end_utf8_byte'] = 1
          ..['original_excerpt'] = 'I';

    final decoded = decodeInterviewReport(value);

    expect(anchor['evidence_ref_id'], 'evidence_primary_001');
    expect(
      decoded
          .report
          ?.dimensions[1]
          .improvements
          .single
          .evidence
          .single
          .originalExcerpt,
      'I',
    );
  });

  test('allows one finding to anchor evidence across two questions', () {
    final value = cloneInterviewReportFixture(
      interviewReportContractFixture()['ready'],
    );
    final dimensions = _dimensions(value);
    final relevance = dimensions.first as Map<String, Object?>;
    relevance['evidence_ref_ids'] = [
      'evidence_primary_001',
      'evidence_followup_001',
    ];
    final relevanceFinding =
        (relevance['improvements']! as List<Object?>).single
            as Map<String, Object?>;
    final interaction = dimensions.last as Map<String, Object?>;
    final interactionFinding =
        (interaction['improvements']! as List<Object?>).single
            as Map<String, Object?>;
    final followupAnchor =
        ((interactionFinding['evidence']! as List<Object?>).single
            as Map<String, Object?>);
    (relevanceFinding['evidence']! as List<Object?>).add(
      Map<String, Object?>.from(followupAnchor),
    );

    final decoded = decodeInterviewReport(value);

    expect(
      decoded.report?.dimensions.first.improvements.single.evidence,
      hasLength(2),
    );
  });
}

List<Object?> _dimensions(Map<String, Object?> value) =>
    (value['report']! as Map<String, Object?>)['dimensions']! as List<Object?>;

Map<String, Object?> _firstDimension(Map<String, Object?> value) =>
    _dimensions(value).first as Map<String, Object?>;

Map<String, Object?> _firstQuestionDimension(Map<String, Object?> value) {
  final questions = _questions(value);
  final dimensions = questions.first['dimension_findings']! as List<Object?>;
  return dimensions.first as Map<String, Object?>;
}

List<Map<String, Object?>> _questions(Map<String, Object?> value) =>
    ((value['report']! as Map<String, Object?>)['questions']! as List<Object?>)
        .cast<Map<String, Object?>>();
