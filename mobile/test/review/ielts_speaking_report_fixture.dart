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
