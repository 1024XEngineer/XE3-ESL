import 'dart:convert';
import 'dart:io';

Map<String, Object?> interviewReportContractFixture() {
  final decoded = jsonDecode(
    File('../api/examples/interview-report-contract.json').readAsStringSync(),
  );
  return decoded as Map<String, Object?>;
}

Map<String, Object?> cloneInterviewReportFixture(Object? value) =>
    jsonDecode(jsonEncode(value)) as Map<String, Object?>;
