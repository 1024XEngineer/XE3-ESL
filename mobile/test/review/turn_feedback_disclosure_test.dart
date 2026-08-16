import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_disclosure.dart';

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
    expect(find.text('正在生成评分与纠错…'), findsOneWidget);
    expect(
      find.byKey(const Key('speech-feedback-loading-indicator')),
      findsOneWidget,
    );

    await tester.pumpWidget(
      _app(_projection(_feedback(status: SpeechFeedbackStatus.ready))),
    );
    await tester.pump();

    expect(find.text('评分与纠错'), findsOneWidget);
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
    expect(find.textContaining('发音准确度'), findsNothing);
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

  testWidgets('shows the three trusted acoustic scores and notice', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        _projection(
          _feedback(status: SpeechFeedbackStatus.ready, assessed: true),
        ),
      ),
    );
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();

    expect(find.text('发音准确度 82 · 流利度 92 · 完整度 100'), findsOneWidget);
    expect(find.text('根据本次录音自动评估，仅供练习参考。'), findsOneWidget);
    expect(find.textContaining('未评估'), findsNothing);
  });

  testWidgets('shows topic pronunciation and speed', (tester) async {
    await tester.pumpWidget(
      _app(
        _projection(
          _feedback(
            status: SpeechFeedbackStatus.ready,
            assessed: true,
            topic: true,
          ),
        ),
      ),
    );
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();

    expect(find.text('发音准确度 89 · 语速 156 词/分钟'), findsOneWidget);
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
    expect(find.textContaining('输入太短'), findsOneWidget);
    expect(find.textContaining('不会按低分处理'), findsOneWidget);

    await tester.pumpWidget(
      _app(
        _projection(
          _feedback(
            status: SpeechFeedbackStatus.ready,
            insufficient: true,
            reasonCodes: const ['TRANSCRIPT_CONFIDENCE_INSUFFICIENT'],
          ),
        ),
      ),
    );
    await tester.pump();
    expect(find.textContaining('请尽量全程使用英语回答'), findsOneWidget);

    var retried = false;
    await tester.pumpWidget(
      _app(
        SpeechFeedbackProjection(
          sourceKey: 'turn_002',
          statusUrl: _statusUrl2,
          feedback: _feedback(
            evaluationId: _evaluationId2,
            statusUrl: _statusUrl2,
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
  String evaluationId = _evaluationId1,
  String statusUrl = _statusUrl1,
  required SpeechFeedbackStatus status,
  bool insufficient = false,
  bool assessed = false,
  bool topic = false,
  List<String>? reasonCodes,
  SpeechFeedbackRepracticeMode repracticeMode =
      SpeechFeedbackRepracticeMode.sameQuestion,
}) {
  final ready = status == SpeechFeedbackStatus.ready;
  final provisional = ready && !insufficient;
  final sourceId = speechFeedbackStatusSource(statusUrl)!.sourceId;
  return SpeechFeedback(
    evaluationId: evaluationId,
    source: SpeechFeedbackSource(
      kind: SpeechFeedbackSourceKind.practiceTurn,
      sourceId: sourceId,
      contextId: _sessionId,
    ),
    feedbackStatus: status,
    scoreabilityStatus: ready
        ? insufficient
              ? SpeechFeedbackScoreabilityStatus.insufficient
              : SpeechFeedbackScoreabilityStatus.provisional
        : null,
    summary: ready ? 'Feedback summary.' : null,
    reasonCodes:
        reasonCodes ??
        (insufficient ? const ['INSUFFICIENT_EVIDENCE'] : const []),
    items: provisional
        ? [
            SpeechFeedbackItem(
              feedbackItemId: 'item_001',
              evaluationId: evaluationId,
              position: 1,
              kind: SpeechFeedbackItemKind.correction,
              anchor: SpeechFeedbackAnchor(
                evidenceRefId: sourceId,
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
    acousticAssessment: assessed
        ? topic
              ? const SpeechFeedbackAcousticAssessment.assessed(
                  pronunciationScore: 88.5,
                  speakingSpeedWpm: 156,
                )
              : const SpeechFeedbackAcousticAssessment.assessed(
                  pronunciationScore: 81.5,
                  fluencyScore: 92.25,
                  integrityScore: 100,
                )
        : ready
        ? const SpeechFeedbackAcousticAssessment.notAssessed(
            reason: 'ACOUSTIC_EVIDENCE_UNAVAILABLE',
          )
        : null,
    stableFailure: status == SpeechFeedbackStatus.failed
        ? const SpeechFeedbackStableFailure(
            code: 'INTERNAL_RETRYABLE',
            retryable: true,
            message: 'Feedback generation failed.',
          )
        : null,
    statusUrl: statusUrl,
    createdAt: DateTime.utc(2026, 7, 30, 10),
    updatedAt: DateTime.utc(2026, 7, 30, 10, 0, 1),
  );
}

const _evaluationId1 = '10000000-0000-4000-8000-000000000001';
const _evaluationId2 = '10000000-0000-4000-8000-000000000002';
const _turnId1 = '20000000-0000-4000-8000-000000000001';
const _turnId2 = '20000000-0000-4000-8000-000000000002';
const _sessionId = '30000000-0000-4000-8000-000000000001';
const _statusUrl1 = '/v1/practice-turns/$_turnId1/evaluation';
const _statusUrl2 = '/v1/practice-turns/$_turnId2/evaluation';
