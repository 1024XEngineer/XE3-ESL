import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/review/turn_feedback.dart';
import 'package:speakup/review/turn_feedback_controller.dart';
import 'package:speakup/review/turn_feedback_disclosure.dart';

void main() {
  testWidgets('stays folded when pending feedback becomes READY', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        _projection(
          _feedback(status: SpeechFeedbackStatus.queued),
          isPolling: true,
        ),
      ),
    );
    expect(find.text('正在生成文字反馈…'), findsOneWidget);

    await tester.pumpWidget(
      _app(_projection(_feedback(status: SpeechFeedbackStatus.ready))),
    );
    await tester.pump();

    expect(find.text('本轮表达反馈'), findsOneWidget);
    expect(
      find.byKey(const Key('speech-feedback-disclosure-content')),
      findsNothing,
    );
    expect(find.text('I was'), findsNothing);
  });

  testWidgets('reveals provisional text-only feedback only after a tap', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(_projection(_feedback(status: SpeechFeedbackStatus.ready))),
    );

    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();

    expect(find.textContaining('基于已确认文本'), findsOneWidget);
    expect(find.text('I was'), findsOneWidget);
    expect(find.textContaining('发音与声学流利度未评估'), findsOneWidget);
    expect(find.textContaining('分'), findsNothing);
  });

  testWidgets('shows repractice only from the server-declared mode', (
    tester,
  ) async {
    SpeechFeedbackItem? selected;
    await tester.pumpWidget(
      _app(
        _projection(_feedback(status: SpeechFeedbackStatus.ready)),
        onRepractice: (item) => selected = item,
      ),
    );
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();

    final action = find.byKey(const Key('speech-feedback-repractice-item_001'));
    expect(action, findsOneWidget);
    await tester.tap(action);
    expect(selected?.repracticeMode, SpeechFeedbackRepracticeMode.sameQuestion);

    await tester.pumpWidget(
      _app(
        _projection(
          _feedback(
            status: SpeechFeedbackStatus.ready,
            repracticeMode: SpeechFeedbackRepracticeMode.none,
          ),
        ),
        onRepractice: (item) => selected = item,
      ),
    );
    await tester.pump();
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();

    expect(action, findsNothing);
  });

  testWidgets('separates insufficient evidence and technical failure', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        _projection(
          _feedback(status: SpeechFeedbackStatus.ready, insufficient: true),
        ),
      ),
    );
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();
    expect(find.textContaining('不会按低分处理'), findsOneWidget);

    var retried = false;
    await tester.pumpWidget(
      _app(
        SpeechFeedbackProjection(
          sourceKey: 'turn_002',
          statusUrl: '/v1/speech-feedback/feedback_000000000002',
          feedback: _feedback(
            id: 'feedback_000000000002',
            statusUrl: '/v1/speech-feedback/feedback_000000000002',
            status: SpeechFeedbackStatus.failed,
          ),
          isPolling: false,
          canRetry: true,
        ),
        onRetry: () => retried = true,
      ),
    );
    await tester.pump();
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();

    expect(find.textContaining('不代表你的口语表现较差'), findsOneWidget);
    await tester.tap(find.byKey(const Key('speech-feedback-retry')));
    expect(retried, isTrue);
  });
}

Widget _app(
  SpeechFeedbackProjection projection, {
  VoidCallback? onRetry,
  SpeechFeedbackRepracticeCallback? onRepractice,
}) {
  return MaterialApp(
    home: Scaffold(
      body: Align(
        alignment: Alignment.topCenter,
        child: SizedBox(
          width: 360,
          child: SpeechFeedbackDisclosure(
            projection: projection,
            onRetry: onRetry,
            onRepractice: onRepractice,
          ),
        ),
      ),
    ),
  );
}

SpeechFeedbackProjection _projection(
  SpeechFeedback feedback, {
  bool isPolling = false,
}) {
  return SpeechFeedbackProjection(
    sourceKey: 'turn_001',
    statusUrl: feedback.statusUrl,
    feedback: feedback,
    isPolling: isPolling,
    canRetry: false,
  );
}

SpeechFeedback _feedback({
  String id = 'feedback_000000000001',
  String statusUrl = '/v1/speech-feedback/feedback_000000000001',
  required SpeechFeedbackStatus status,
  bool insufficient = false,
  SpeechFeedbackRepracticeMode repracticeMode =
      SpeechFeedbackRepracticeMode.sameQuestion,
}) {
  final ready = status == SpeechFeedbackStatus.ready;
  final provisional = ready && !insufficient;
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
        ? insufficient
              ? SpeechFeedbackScoreabilityStatus.insufficient
              : SpeechFeedbackScoreabilityStatus.provisional
        : null,
    gateStatus: ready
        ? insufficient
              ? SpeechFeedbackGateStatus.blocked
              : SpeechFeedbackGateStatus.feedbackOnly
        : null,
    reasonCodes: insufficient ? const ['INSUFFICIENT_EVIDENCE'] : const [],
    schemaVersion: 'speech-feedback/v1',
    strategyRef: 'qianwen-speech-feedback/v1',
    pipelineVersion: 'speech-feedback-pipeline/v1',
    isFinal: false,
    items: provisional
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
              repracticeMode: repracticeMode,
              createdAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
            ),
          ]
        : const [],
    acousticAssessment: const SpeechFeedbackAcousticAssessment(
      pronunciation: SpeechFeedbackAssessmentStatus.notAssessed,
      acousticFluency: SpeechFeedbackAssessmentStatus.notAssessed,
      reasonCode: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
    ),
    stableFailure: status == SpeechFeedbackStatus.failed
        ? const SpeechFeedbackStableFailure(
            reasonCode: 'INTERNAL_RETRYABLE',
            retryable: true,
          )
        : null,
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
    completedAt:
        status == SpeechFeedbackStatus.ready ||
            status == SpeechFeedbackStatus.failed
        ? DateTime.utc(2026, 7, 30, 10, 0, 1)
        : null,
  );
}
