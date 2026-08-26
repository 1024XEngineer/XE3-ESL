import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';

void main() {
  test('fake client completes and replays a deferred transcription', () async {
    final client = FakePracticeClient(turnLimit: 1);
    const sessionId = 'session-deferred-test';
    final snapshot = await client.activatePractice(
      sessionId: sessionId,
      clientOperationId: 'activate-deferred-test',
    );
    final question = snapshot.currentQuestion!;

    final staged = await client.stageDeferredTranscription(
      sessionId: sessionId,
      questionId: question.id,
      idempotencyKey: 'part-2-recording',
      audio: const RecordedPracticeAudio(
        path: 'part-2.wav',
        contentType: 'audio/wav',
        sizeBytes: 64044,
      ),
    );
    final completed = await client.getDeferredTranscription(
      statusUrl: staged.statusUrl,
    );
    final replayed = await client.getDeferredTranscription(
      statusUrl: staged.statusUrl,
    );
    final restored = await client.restorePractice(sessionId: sessionId);

    expect(staged.status, DeferredTranscriptionStatus.processing);
    expect(completed.status, DeferredTranscriptionStatus.completed);
    expect(replayed.status, DeferredTranscriptionStatus.completed);
    expect(restored.completedTurns, 1);
    expect(restored.sessionCompleted, isTrue);
  });

  test('fake client rejects an unknown deferred status URL', () async {
    final client = FakePracticeClient();
    await client.activatePractice(
      sessionId: 'session-deferred-test',
      clientOperationId: 'activate-deferred-test',
    );

    await expectLater(
      client.getDeferredTranscription(
        statusUrl:
            '/v1/practice-sessions/unknown/deferred-transcriptions/missing',
      ),
      throwsStateError,
    );
  });
}
