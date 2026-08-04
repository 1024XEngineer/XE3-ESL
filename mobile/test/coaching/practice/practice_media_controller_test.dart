import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

void main() {
  test('Practice recording reference contains no Review identity', () {
    const recording = PracticeRecordingReference(
      audioAssetId: 'audio-1',
      effectiveTurn: 2,
    );
    expect(recording.audioAssetId, 'audio-1');
    expect(recording.effectiveTurn, 2);
  });
}
