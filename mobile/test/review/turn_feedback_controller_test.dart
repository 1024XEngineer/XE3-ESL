import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/turn_feedback.dart';
import 'package:speakup/features/coaching/review/turn_feedback_client.dart';
import 'package:speakup/features/coaching/review/turn_feedback_controller.dart';

void main() {
  test('polls each source independently until its feedback is READY', () async {
    final client = _QueueClient({
      '/v1/speech-feedback/feedback_000000000001': [
        _feedback(status: SpeechFeedbackStatus.queued),
        _feedback(status: SpeechFeedbackStatus.running),
        _feedback(status: SpeechFeedbackStatus.ready),
      ],
      '/v1/speech-feedback/feedback_000000000002': [
        _feedback(
          id: 'feedback_000000000002',
          statusUrl: '/v1/speech-feedback/feedback_000000000002',
          status: SpeechFeedbackStatus.ready,
        ),
      ],
    });
    final controller = SpeechFeedbackController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 3,
    );
    addTearDown(controller.dispose);

    await Future.wait([
      controller.load(
        sourceKey: 'turn_001',
        statusUrl: '/v1/speech-feedback/feedback_000000000001',
      ),
      controller.load(
        sourceKey: 'turn_002',
        statusUrl: '/v1/speech-feedback/feedback_000000000002',
      ),
    ]);

    expect(client.calls['/v1/speech-feedback/feedback_000000000001'], 3);
    expect(client.calls['/v1/speech-feedback/feedback_000000000002'], 1);
    expect(
      controller.projectionFor('turn_001')?.feedback?.feedbackStatus,
      SpeechFeedbackStatus.ready,
    );
    expect(
      controller.projectionFor('turn_002')?.feedback?.speechFeedbackId,
      'feedback_000000000002',
    );
  });

  test('bounded polling stops without silently inventing feedback', () async {
    final queued = _feedback(status: SpeechFeedbackStatus.queued);
    final client = _QueueClient({
      queued.statusUrl: [queued, queued],
    });
    final controller = SpeechFeedbackController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 2,
    );
    addTearDown(controller.dispose);

    await controller.load(sourceKey: 'turn_001', statusUrl: queued.statusUrl);

    final projection = controller.projectionFor('turn_001');
    expect(projection?.isPolling, isFalse);
    expect(projection?.canRetry, isTrue);
    expect(projection?.feedback?.feedbackStatus, SpeechFeedbackStatus.queued);
    expect(projection?.errorMessage, '反馈仍在生成，请稍后重试。');
  });

  test('rebinding one source fences its older late response', () async {
    final client = _RebindingClient();
    final controller = SpeechFeedbackController(
      client: client,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);

    final oldLoad = controller.load(
      sourceKey: 'message_001',
      statusUrl: '/v1/speech-feedback/feedback_000000000001',
    );
    await client.oldStarted.future;
    final newLoad = controller.load(
      sourceKey: 'message_001',
      statusUrl: '/v1/speech-feedback/feedback_000000000002',
    );
    await client.newStarted.future;
    client.newResponse.complete(
      _feedback(
        id: 'feedback_000000000002',
        statusUrl: '/v1/speech-feedback/feedback_000000000002',
        status: SpeechFeedbackStatus.ready,
      ),
    );
    await newLoad;
    client.oldResponse.complete(_feedback(status: SpeechFeedbackStatus.ready));
    await oldLoad;

    expect(
      controller.projectionFor('message_001')?.feedback?.speechFeedbackId,
      'feedback_000000000002',
    );
  });

  test(
    'removing and re-adding one source fences its older late response',
    () async {
      final client = _RemoveAndReaddClient();
      final controller = SpeechFeedbackController(
        client: client,
        maximumPollAttempts: 1,
      );
      addTearDown(controller.dispose);
      const sourceKey = 'message_001';
      const statusUrl = '/v1/speech-feedback/feedback_000000000001';

      final oldLoad = controller.load(
        sourceKey: sourceKey,
        statusUrl: statusUrl,
      );
      await client.oldStarted.future;
      controller.removeSource(sourceKey);
      final newLoad = controller.load(
        sourceKey: sourceKey,
        statusUrl: statusUrl,
      );
      await client.newStarted.future;
      client.newResponse.complete(
        _feedback(status: SpeechFeedbackStatus.ready),
      );
      await newLoad;
      client.oldResponse.complete(
        _feedback(status: SpeechFeedbackStatus.queued),
      );
      await oldLoad;

      expect(
        controller.projectionFor(sourceKey)?.feedback?.feedbackStatus,
        SpeechFeedbackStatus.ready,
      );
    },
  );

  test(
    'account clear fences late work and clears every private projection',
    () async {
      final client = _ControlledClient();
      final controller = SpeechFeedbackController(client: client);
      addTearDown(controller.dispose);
      final pending = controller.load(
        sourceKey: 'turn_001',
        statusUrl: '/v1/speech-feedback/feedback_000000000001',
      );
      await client.started.future;

      await controller.clearPrivateState();
      client.response.complete(_feedback(status: SpeechFeedbackStatus.ready));
      await pending;

      expect(client.clearCalls, 1);
      expect(controller.projections, isEmpty);
    },
  );
}

SpeechFeedback _feedback({
  String id = 'feedback_000000000001',
  String statusUrl = '/v1/speech-feedback/feedback_000000000001',
  required SpeechFeedbackStatus status,
}) {
  final ready = status == SpeechFeedbackStatus.ready;
  return SpeechFeedback(
    speechFeedbackId: id,
    source: const ConversationTurnFeedbackSource(
      practiceSessionId: 'session_001',
      turnId: 'turn_001',
      inputRevision: 1,
      evidenceSnapshotId: 'evidence_001',
    ),
    feedbackStatus: status,
    scoreabilityStatus: ready
        ? SpeechFeedbackScoreabilityStatus.provisional
        : null,
    gateStatus: ready ? SpeechFeedbackGateStatus.feedbackOnly : null,
    reasonCodes: const [],
    schemaVersion: 'speech-feedback/v1',
    strategyRef: 'qianwen-speech-feedback/v1',
    pipelineVersion: 'speech-feedback-pipeline/v1',
    isFinal: false,
    items: ready
        ? [
            SpeechFeedbackItem(
              feedbackItemId: 'item_001',
              speechFeedbackId: id,
              kind: SpeechFeedbackItemKind.correction,
              anchor: const ConversationTranscriptFeedbackAnchor(
                evidenceRefId: 'evidence_001',
                turnId: 'turn_001',
                startUtf8Byte: 0,
                endUtf8Byte: 4,
                originalExcerpt: 'I am',
              ),
              explanation: 'Use the past tense for a completed event.',
              suggestedText: 'I was',
              repracticeMode: SpeechFeedbackRepracticeMode.sameQuestion,
              createdAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
            ),
          ]
        : const [],
    acousticAssessment: const SpeechFeedbackAcousticAssessment(
      pronunciation: SpeechFeedbackAssessmentStatus.notAssessed,
      acousticFluency: SpeechFeedbackAssessmentStatus.notAssessed,
      reasonCode: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    ),
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
    completedAt: ready ? DateTime.utc(2026, 7, 30, 10, 0, 1) : null,
  );
}

final class _QueueClient implements SpeechFeedbackClient {
  _QueueClient(this.responses);

  final Map<String, List<SpeechFeedback>> responses;
  final Map<String, int> calls = {};

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) async {
    final index = calls.update(
      statusUrl,
      (value) => value + 1,
      ifAbsent: () => 1,
    );
    return responses[statusUrl]![index - 1];
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _RebindingClient implements SpeechFeedbackClient {
  final oldStarted = Completer<void>();
  final newStarted = Completer<void>();
  final oldResponse = Completer<SpeechFeedback>();
  final newResponse = Completer<SpeechFeedback>();

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) {
    if (statusUrl.endsWith('0001')) {
      oldStarted.complete();
      return oldResponse.future;
    }
    newStarted.complete();
    return newResponse.future;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _RemoveAndReaddClient implements SpeechFeedbackClient {
  final oldStarted = Completer<void>();
  final newStarted = Completer<void>();
  final oldResponse = Completer<SpeechFeedback>();
  final newResponse = Completer<SpeechFeedback>();
  int calls = 0;

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) {
    calls++;
    if (calls == 1) {
      oldStarted.complete();
      return oldResponse.future;
    }
    newStarted.complete();
    return newResponse.future;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _ControlledClient implements SpeechFeedbackClient {
  final started = Completer<void>();
  final response = Completer<SpeechFeedback>();
  int clearCalls = 0;

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) {
    started.complete();
    return response.future;
  }

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }
}
