import 'dart:convert';
import 'dart:io';

Map<String, Object?> speechFeedbackContractFixture() {
  final decoded = jsonDecode(
    File('../api/examples/speech-feedback-contract.json').readAsStringSync(),
  );
  return decoded as Map<String, Object?>;
}

Map<String, Object?> cloneSpeechFeedbackFixture(Object? value) =>
    jsonDecode(jsonEncode(value)) as Map<String, Object?>;
