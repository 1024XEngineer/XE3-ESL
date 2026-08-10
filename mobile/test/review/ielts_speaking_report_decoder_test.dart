import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  test(
    'decodes pending, partial READY, insufficient, and FAILED envelopes',
    () {
      final fixture = ieltsSpeakingReportContractFixture();

      expect(
        decodeIeltsSpeakingReport(fixture['queued']).evaluationStatus,
        IeltsSpeakingReportEvaluationStatus.queued,
      );
      final ready = decodeIeltsSpeakingReport(fixture['ready']);
      expect(
        ready.report?.criteria.map((criterion) => criterion.id),
        IeltsSpeakingCriterionId.values,
      );
      expect(ready.report?.criteria[0].estimatedBand, isNull);
      expect(ready.report?.criteria[1].estimatedBand, 6);
      expect(ready.report?.criteria[2].estimatedBand, 6);
      expect(
        ready.report?.criteria[3].scoreabilityStatus,
        IeltsSpeakingReportScoreabilityStatus.insufficient,
      );
      expect(
        ready.report?.speakingOverallStatus,
        IeltsSpeakingOverallStatus.notAvailable,
      );
      expect(ready.report?.questions, hasLength(14));
      expect(
        ready.report?.partReviews.map((part) => part.id),
        IeltsSpeakingPartId.values,
      );

      expect(
        decodeIeltsSpeakingReport(
          fixture['ready_insufficient'],
        ).report?.scoreabilityStatus,
        IeltsSpeakingReportScoreabilityStatus.insufficient,
      );
      expect(
        decodeIeltsSpeakingReport(fixture['failed']).stableFailure?.retryable,
        isTrue,
      );
    },
  );

  test('decodes a report for a digit-leading Practice session UUID', () {
    const practiceSessionId = '20000000-0000-4000-8000-000000000001';
    final value = _readyClone()
      ..['practice_session_id'] = practiceSessionId
      ..['status_url'] =
          '/v1/practice-sessions/$practiceSessionId/ielts-speaking-report';

    final decoded = decodeIeltsSpeakingReport(value);

    expect(decoded.practiceSessionId, practiceSessionId);
  });

  test('decodes a complete four-band report and its rounded overall', () {
    final value = completeIeltsSpeakingReportContractFixture();

    final decoded = decodeIeltsSpeakingReport(value).report!;

    expect(decoded.speakingOverallStatus, IeltsSpeakingOverallStatus.available);
    expect(decoded.speakingOverallBand, 6.5);
    expect(decoded.criteria[0].estimatedBand, 7);
    expect(decoded.criteria[3].estimatedBand, 6);
  });

  test('derives non-default Part boundaries from the report questions', () {
    final value = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['ready_insufficient'],
    );
    final report = _report(value);
    final originalQuestions = report['questions']! as List<Object?>;
    final questions = <Map<String, Object?>>[
      originalQuestions[0]! as Map<String, Object?>,
      originalQuestions[1]! as Map<String, Object?>,
      originalQuestions[8]! as Map<String, Object?>,
      originalQuestions[9]! as Map<String, Object?>,
      originalQuestions[10]! as Map<String, Object?>,
    ];
    for (var index = 0; index < questions.length; index++) {
      questions[index]['index'] = index + 1;
    }
    report['questions'] = questions;
    final summary = report['test_summary']! as Map<String, Object?>;
    summary
      ..['question_count'] = questions.length
      ..['answered_count'] = questions.length;
    final parts = report['part_reviews']! as List<Object?>;
    for (var index = 0; index < parts.length; index++) {
      final part = parts[index]! as Map<String, Object?>;
      final indexes = switch (index) {
        0 => <int>[1, 2],
        1 => <int>[3],
        _ => <int>[4, 5],
      };
      part['question_indexes'] = indexes;
      part['evidence_ref_ids'] = <Object?>[
        for (final questionIndex in indexes)
          ...(questions[questionIndex - 1]['evidence_ref_ids']!
              as List<Object?>),
      ];
    }

    final decoded = decodeIeltsSpeakingReport(value).report!;

    expect(decoded.questions, hasLength(5));
    expect(decoded.partReviews[0].questionIndexes, <int>[1, 2]);
    expect(decoded.partReviews[1].questionIndexes, <int>[3]);
    expect(decoded.partReviews[2].questionIndexes, <int>[4, 5]);
  });

  test('accepts only false for an explicitly non-retryable failure', () {
    final value = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['failed'],
    );
    value['stable_failure'] = <String, Object?>{
      'reason_code': 'INTERNAL_NON_RETRYABLE',
      'retryable': false,
    };

    final decoded = decodeIeltsSpeakingReport(value);

    expect(decoded.stableFailure?.reasonCode, 'INTERNAL_NON_RETRYABLE');
    expect(decoded.stableFailure?.retryable, isFalse);

    (value['stable_failure']! as Map<String, Object?>)['retryable'] = true;
    expect(
      () => decodeIeltsSpeakingReport(value),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );
  });

  test('rejects fields that invent unsupported IELTS scores or targets', () {
    for (final mutate in <void Function(Map<String, Object?>)>[
      (root) => _criterion(root, 0)['estimated_band'] = 6,
      (root) => _criterion(root, 3)['estimated_band'] = 6,
      (root) => _report(root)['speaking_overall'] = <String, Object?>{
        'status': 'NOT_AVAILABLE',
        'band': 6,
      },
      (root) => _part(root, 0)['estimated_band'] = 6,
      (root) => _question(root, 0)['score'] = 6,
      (root) => _criterion(root, 1)['confidence'] = 0.7,
      (root) => _report(root)['target_plan'] = <String, Object?>{
        'status': 'NOT_CONFIGURED',
        'target_band': 7,
      },
    ]) {
      final invalid = cloneIeltsSpeakingReportFixture(
        ieltsSpeakingReportContractFixture()['ready'],
      );
      mutate(invalid);
      expect(
        () => decodeIeltsSpeakingReport(invalid),
        throwsA(isA<IeltsSpeakingReportDecodeException>()),
      );
    }
  });

  test('rejects reordered questions, Parts, and criteria', () {
    final wrongIndex = _readyClone();
    _question(wrongIndex, 0)['index'] = 2;
    expect(
      () => decodeIeltsSpeakingReport(wrongIndex),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );

    final wrongPart = _readyClone();
    _question(wrongPart, 8)['part_id'] = 'PART_1';
    expect(
      () => decodeIeltsSpeakingReport(wrongPart),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );

    final reordered = _readyClone();
    final criteria = _report(reordered)['criteria']! as List<Object?>;
    final first = criteria[0];
    criteria[0] = criteria[1];
    criteria[1] = first;
    expect(
      () => decodeIeltsSpeakingReport(reordered),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );
  });

  test('rejects answered_count that differs from provided responses', () {
    final invalid = _readyClone();
    final summary = _report(invalid)['test_summary']! as Map<String, Object?>;
    summary['answered_count'] = (summary['answered_count']! as int) - 1;

    expect(
      () => decodeIeltsSpeakingReport(invalid),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );
  });

  test('rejects forged excerpts and cross-reference corruption', () {
    final forgedExcerpt = _readyClone();
    final strengths = _criterion(forgedExcerpt, 0)['strengths']! as List;
    final evidence =
        (strengths.single as Map<String, Object?>)['evidence']! as List;
    (evidence.single as Map<String, Object?>)['original_excerpt'] = 'forged';
    expect(
      () => decodeIeltsSpeakingReport(forgedExcerpt),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );

    final dangling = _readyClone();
    final findings =
        _question(dangling, 0)['criterion_findings']! as List<Object?>;
    (findings[1] as Map<String, Object?>)['improvement_finding_ids'] = [
      'ielts_finding_missing',
    ];
    expect(
      () => decodeIeltsSpeakingReport(dangling),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );

    final wrongPartEvidence = _readyClone();
    _part(wrongPartEvidence, 0)['evidence_ref_ids'] = <String>[];
    expect(
      () => decodeIeltsSpeakingReport(wrongPartEvidence),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );

    final duplicatedEvidence = _readyClone();
    _question(duplicatedEvidence, 2)['evidence_ref_ids'] = <String>[
      (_question(duplicatedEvidence, 1)['evidence_ref_ids']! as List).single
          as String,
    ];
    final partEvidence =
        _part(duplicatedEvidence, 0)['evidence_ref_ids']! as List;
    partEvidence.remove('ielts_evidence_003');
    expect(
      () => decodeIeltsSpeakingReport(duplicatedEvidence),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );
  });

  test('rejects an envelope status URL for another Practice Session', () {
    final invalid = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['running'],
    );
    invalid['status_url'] =
        '/v1/practice-sessions/session_other/ielts-speaking-report';

    expect(
      () => decodeIeltsSpeakingReport(invalid),
      throwsA(isA<IeltsSpeakingReportDecodeException>()),
    );
  });

  test(
    'decodes an explicit unprovided question without inventing evidence',
    () {
      final partial = _readyClone();
      final question = _question(partial, 13);
      question['opportunity_status'] = 'NOT_PROVIDED';
      question['assessment_status'] = 'NOT_ASSESSED';
      question.remove('confirmed_transcript');
      question.remove('response_turn_id');
      question['evidence_ref_ids'] = <String>[];
      final summary = _report(partial)['test_summary']! as Map<String, Object?>;
      summary['answered_count'] = (summary['answered_count']! as int) - 1;
      final partEvidence =
          _part(partial, 2)['evidence_ref_ids']! as List<Object?>;
      partEvidence.removeLast();

      final decoded = decodeIeltsSpeakingReport(partial);

      expect(
        decoded.report?.questions.last.opportunityStatus,
        IeltsSpeakingOpportunityStatus.notProvided,
      );
      expect(decoded.report?.questions.last.confirmedTranscript, isNull);
    },
  );
}

Map<String, Object?> _readyClone() => cloneIeltsSpeakingReportFixture(
  ieltsSpeakingReportContractFixture()['ready'],
);

Map<String, Object?> _report(Map<String, Object?> root) =>
    root['report']! as Map<String, Object?>;

Map<String, Object?> _criterion(Map<String, Object?> root, int index) =>
    (_report(root)['criteria']! as List<Object?>)[index]
        as Map<String, Object?>;

Map<String, Object?> _question(Map<String, Object?> root, int index) =>
    (_report(root)['questions']! as List<Object?>)[index]
        as Map<String, Object?>;

Map<String, Object?> _part(Map<String, Object?> root, int index) =>
    (_report(root)['part_reviews']! as List<Object?>)[index]
        as Map<String, Object?>;
