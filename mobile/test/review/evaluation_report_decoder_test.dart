import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report_decoder.dart';

import 'evaluation_report_fixture.dart';

void main() {
  test('decodes the canonical Evaluation report', () {
    final report = decodeEvaluationReport(evaluationReportWireFixture());

    expect(report.id, '20000000-0000-4000-8000-000000000002');
    expect(report.sceneType, EvaluationReportSceneType.interview);
    expect(report.practiceExperience, 'INTERVIEW');
    expect(report.practiceMode, 'FULL_SIMULATION');
    expect(report.scoreability, EvaluationReportScoreability.provisional);
    expect(report.questions.single.text, 'Tell me about your experience.');
    expect(
      report.questions.single.answer?.transcript,
      'I led a project and improved delivery.',
    );
    expect(report.dimensions.single.score, 82);
    expect(report.priorityActions.single.findingId, 'improvement_action');
    expect(
      report
          .dimensions
          .single
          .improvements
          .single
          .evidence
          .single
          .originalExcerpt,
      'I made the product better.',
    );
  });

  test('rejects a practice context that conflicts with the scene type', () {
    final mismatchedExperience = evaluationReportWireFixture();
    _formal(mismatchedExperience)['practice_experience'] = 'IELTS_SPEAKING';
    final mismatchedMode = evaluationReportWireFixture();
    _formal(mismatchedMode)['practice_mode'] = 'FULL_MOCK';

    expect(
      () => decodeEvaluationReport(mismatchedExperience),
      throwsA(_decodeFailure),
    );
    expect(
      () => decodeEvaluationReport(mismatchedMode),
      throwsA(_decodeFailure),
    );
  });

  test('decodes insufficient evidence without a score', () {
    final report = decodeEvaluationReport(
      evaluationReportWireFixture(scoreability: 'INSUFFICIENT'),
    );

    expect(report.scoreability, EvaluationReportScoreability.insufficient);
    expect(report.dimensions.single.score, isNull);
    expect(report.priorityActions, isEmpty);
  });

  test('rejects unknown fields and superseded schema shapes', () {
    final unknown = evaluationReportWireFixture()..['status'] = 'completed';
    final old = <String, Object?>{
      'review_id': '20000000-0000-4000-8000-000000000002',
      'status': 'completed',
      'result': <String, Object?>{},
    };

    expect(() => decodeEvaluationReport(unknown), throwsA(_decodeFailure));
    expect(() => decodeEvaluationReport(old), throwsA(_decodeFailure));
  });

  test('rejects invalid score scale and insufficient score', () {
    final invalidBand = evaluationReportWireFixture();
    _dimension(invalidBand)
      ..['scale'] = 'IELTS_BAND'
      ..['score'] = 9.5;
    final insufficient = evaluationReportWireFixture(
      scoreability: 'INSUFFICIENT',
    );
    _dimension(insufficient)['score'] = 1;

    expect(() => decodeEvaluationReport(invalidBand), throwsA(_decodeFailure));
    expect(() => decodeEvaluationReport(insufficient), throwsA(_decodeFailure));
  });

  test('rejects broken priority references and duplicate finding ids', () {
    final broken = evaluationReportWireFixture();
    final action =
        (_formal(broken)['priority_actions']! as List<Object?>).single
            as Map<String, Object?>;
    action['finding_id'] = 'missing_finding';
    final duplicate = evaluationReportWireFixture();
    final dimension = _dimension(duplicate);
    dimension['recommended_examples'] = <Object?>[
      <String, Object?>{
        'finding_id': 'improvement_action',
        'message': 'duplicate',
        'evidence': <Object?>[],
      },
    ];

    expect(() => decodeEvaluationReport(broken), throwsA(_decodeFailure));
    expect(() => decodeEvaluationReport(duplicate), throwsA(_decodeFailure));
  });

  test('rejects unknown report fields and malformed date', () {
    final unknown = evaluationReportWireFixture();
    _formal(unknown)['detail'] = <String, Object?>{};
    final malformedDate = evaluationReportWireFixture()
      ..['created_at'] = '2026-07-26T10:00:00';

    expect(() => decodeEvaluationReport(unknown), throwsA(_decodeFailure));
    expect(
      () => decodeEvaluationReport(malformedDate),
      throwsA(_decodeFailure),
    );
  });

  test('accepts a JSON round trip of the stored report envelope', () {
    final raw = evaluationReportWireFixture();
    final report = decodeEvaluationReport(jsonDecode(jsonEncode(raw)));

    expect(report.practiceSessionId, '30000000-0000-4000-8000-000000000003');
  });
}

Map<String, Object?> _dimension(Map<String, Object?> report) =>
    (_formal(report)['dimensions']! as List<Object?>).single
        as Map<String, Object?>;

Map<String, Object?> _formal(Map<String, Object?> report) =>
    report['report']! as Map<String, Object?>;

final _decodeFailure = isA<EvaluationReportDecodeException>();
