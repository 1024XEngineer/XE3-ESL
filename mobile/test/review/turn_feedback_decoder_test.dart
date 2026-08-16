import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_decoder.dart';

import 'turn_feedback_fixture.dart';

void main() {
  test('decodes the ready practice evaluation resource', () {
    final feedback = decodeSpeechFeedback(
      readyPracticeFeedbackFixture(),
      statusUrl: practiceStatusUrl,
    );

    expect(feedback.evaluationId, evaluationId);
    expect(feedback.source.kind, SpeechFeedbackSourceKind.practiceTurn);
    expect(feedback.source.contextId, practiceSessionId);
    expect(feedback.feedbackStatus, SpeechFeedbackStatus.ready);
    expect(
      feedback.scoreabilityStatus,
      SpeechFeedbackScoreabilityStatus.provisional,
    );
    expect(feedback.items.single.feedbackItemId, feedbackItemId);
    expect(feedback.items.single.canRepractice, isTrue);
    expect(feedback.acousticAssessment?.isAssessed, isFalse);
  });

  test('decodes pending, failed, and Agent feedback states', () {
    expect(
      decodeSpeechFeedback(
        pendingPracticeFeedbackFixture('QUEUED'),
        statusUrl: practiceStatusUrl,
      ).isPending,
      isTrue,
    );
    final failed = decodeSpeechFeedback(
      failedPracticeFeedbackFixture(),
      statusUrl: practiceStatusUrl,
    );
    expect(failed.feedbackStatus, SpeechFeedbackStatus.failed);
    expect(failed.stableFailure?.retryable, isTrue);

    final agent = decodeSpeechFeedback(
      readyAgentFeedbackFixture(),
      statusUrl: agentStatusUrl,
    );
    expect(agent.source.kind, SpeechFeedbackSourceKind.agentMessage);
    expect(
      agent.items.single.repracticeMode,
      SpeechFeedbackRepracticeMode.none,
    );
  });

  test('rejects a URL whose source does not match the resource', () {
    expect(
      () => decodeSpeechFeedback(
        readyPracticeFeedbackFixture(),
        statusUrl: agentStatusUrl,
      ),
      throwsA(isA<SpeechFeedbackDecodeException>()),
    );
    expect(
      validSpeechFeedbackStatusUrl('/v1/speech-feedback/$evaluationId'),
      isFalse,
    );
  });

  test('rejects unknown fields and invalid state payloads', () {
    final unknown = readyPracticeFeedbackFixture()..['score'] = 90;
    final queuedWithResult = pendingPracticeFeedbackFixture('QUEUED')
      ..['result'] = readyPracticeFeedbackFixture()['result'];
    final readyWithError = readyPracticeFeedbackFixture()
      ..['error'] = failedPracticeFeedbackFixture()['error'];

    for (final value in [unknown, queuedWithResult, readyWithError]) {
      expect(
        () => decodeSpeechFeedback(value, statusUrl: practiceStatusUrl),
        throwsA(isA<SpeechFeedbackDecodeException>()),
      );
    }
  });

  test('rejects invalid item ownership and repractice semantics', () {
    final wrongEvaluation = readyPracticeFeedbackFixture();
    _item(wrongEvaluation)['evaluation_id'] = agentThreadId;

    final wrongEvidence = readyPracticeFeedbackFixture();
    (_item(wrongEvidence)['evidence']!
            as Map<String, Object?>)['evidence_ref_id'] =
        agentMessageId;

    final agentRetry = readyAgentFeedbackFixture();
    _item(agentRetry)['repractice_mode'] = 'SAME_QUESTION';

    final removedMode = readyPracticeFeedbackFixture();
    _item(removedMode)['repractice_mode'] = 'SAME_THREAD';

    for (final value in [
      wrongEvaluation,
      wrongEvidence,
      agentRetry,
      removedMode,
    ]) {
      final statusUrl = identical(value, agentRetry)
          ? agentStatusUrl
          : practiceStatusUrl;
      expect(
        () => decodeSpeechFeedback(value, statusUrl: statusUrl),
        throwsA(isA<SpeechFeedbackDecodeException>()),
      );
    }
  });

  test('validates exact UTF-8 evidence byte ranges', () {
    final valid = readyPracticeFeedbackFixture();
    final evidence = _item(valid)['evidence']! as Map<String, Object?>;
    evidence
      ..['start_utf8_byte'] = 4
      ..['end_utf8_byte'] = 10
      ..['original_excerpt'] = '你好';
    expect(
      decodeSpeechFeedback(
        valid,
        statusUrl: practiceStatusUrl,
      ).items.single.anchor.originalExcerpt,
      '你好',
    );

    final invalid = cloneFeedbackFixture(valid);
    (_item(invalid)['evidence']! as Map<String, Object?>)['end_utf8_byte'] = 9;
    expect(
      () => decodeSpeechFeedback(invalid, statusUrl: practiceStatusUrl),
      throwsA(isA<SpeechFeedbackDecodeException>()),
    );
  });
}

Map<String, Object?> _item(Map<String, Object?> root) =>
    (root['feedback_items']! as List<Object?>).single as Map<String, Object?>;
