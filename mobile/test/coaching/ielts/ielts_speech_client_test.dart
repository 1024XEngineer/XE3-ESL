import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_speech_client.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';

void main() {
  test('loads IELTS speech only through stable resource paths', () async {
    final media = _MediaClient();
    final client = WireIeltsSpeechClient(media);

    await client.loadQuestion(
      const IeltsQuestionReference(
        bankId: 'bank-2026',
        part: 'PART_1',
        sourceId: 'teachers',
        questionPosition: 2,
      ),
    );
    expect(media.paths, [
      '/v1/ielts-speaking/question-banks/bank-2026/PART_1/teachers/questions/2/speech',
    ]);
  });
}

final class _MediaClient implements PracticeMediaClient {
  final List<String> paths = [];

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async {
    paths.add(speechPath);
    return Uint8List.fromList([1]);
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) =>
      throw UnimplementedError();

  @override
  Future<void> deleteRecording(String audioAssetId) =>
      throw UnimplementedError();

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}
