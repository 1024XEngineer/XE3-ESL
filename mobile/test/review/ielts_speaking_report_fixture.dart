import 'dart:convert';
import 'dart:io';

Map<String, Object?> ieltsSpeakingReportContractFixture() {
  final decoded = jsonDecode(
    File(
      '../api/examples/ielts-speaking-report-contract.json',
    ).readAsStringSync(),
  );
  return decoded as Map<String, Object?>;
}

Map<String, Object?> cloneIeltsSpeakingReportFixture(Object? value) =>
    jsonDecode(jsonEncode(value)) as Map<String, Object?>;

Map<String, Object?> completeIeltsSpeakingReportContractFixture() {
  final value = cloneIeltsSpeakingReportFixture(
    ieltsSpeakingReportContractFixture()['ready'],
  );
  final report = value['report']! as Map<String, Object?>;
  final criteria = report['criteria']! as List<Object?>;
  final fluency = criteria[0]! as Map<String, Object?>;
  fluency
    ..['estimated_band'] = 7
    ..['band_descriptor'] = 'Band 7 fluency descriptor.'
    ..['reason_codes'] = <Object?>['PRACTICE_ESTIMATE_UNCALIBRATED'];

  final pronunciation = cloneIeltsSpeakingReportFixture(fluency)
    ..['criterion_id'] = 'IELTS_PR'
    ..['estimated_band'] = 6
    ..['band_descriptor'] = 'Band 6 pronunciation descriptor.';
  final strengths = pronunciation['strengths']! as List<Object?>;
  final pronunciationFinding = strengths.single! as Map<String, Object?>;
  pronunciationFinding['finding_id'] = 'ielts_finding_pr_001';
  criteria[3] = pronunciation;

  final questions = report['questions']! as List<Object?>;
  final firstQuestion = questions.first! as Map<String, Object?>;
  final refs = firstQuestion['criterion_findings']! as List<Object?>;
  refs[3] = <String, Object?>{
    'criterion_id': 'IELTS_PR',
    'strength_finding_ids': <Object?>['ielts_finding_pr_001'],
    'improvement_finding_ids': <Object?>[],
    'upgrade_example_finding_ids': <Object?>[],
  };
  final parts = report['part_reviews']! as List<Object?>;
  final part1 = parts.first! as Map<String, Object?>;
  (part1['strength_finding_ids']! as List<Object?>).add('ielts_finding_pr_001');
  report['speaking_overall'] = <String, Object?>{
    'status': 'AVAILABLE',
    'estimated_band': 6.5,
    'explanation': '四项等权平均后按 0.5 分取整。',
  };
  return value;
}
