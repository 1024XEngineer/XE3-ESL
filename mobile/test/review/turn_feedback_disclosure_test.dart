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

  testWidgets('shows topic pronunciation, speed, and relevance', (
    tester,
  ) async {
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

    expect(find.text('发音准确度 89 · 语速 156 词/分钟 · 题意相关 82'), findsOneWidget);
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
  bool assessed = false,
  bool topic = false,
  List<String>? reasonCodes,
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
    reasonCodes:
        reasonCodes ??
        (insufficient ? const ['INSUFFICIENT_EVIDENCE'] : const []),
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
    acousticAssessment: assessed
        ? topic
              ? const SpeechFeedbackAcousticAssessment(
                  pronunciation: SpeechFeedbackAssessmentStatus.assessed,
                  acousticFluency: SpeechFeedbackAssessmentStatus.assessed,
                  pronunciationScore: 88.5,
                  speakingSpeedWpm: 156,
                  semanticScore: 82,
                  provider: 'xfyun-ise',
                  providerSessionId: 'ise-topic-session-1',
                  category: 'topic',
                  notice: '根据本次录音自动评估，仅供练习参考。',
                  reasonCode: '',
                )
              : const SpeechFeedbackAcousticAssessment(
                  pronunciation: SpeechFeedbackAssessmentStatus.assessed,
                  acousticFluency: SpeechFeedbackAssessmentStatus.assessed,
                  integrity: SpeechFeedbackAssessmentStatus.assessed,
                  accuracyScore: 81.5,
                  fluencyScore: 92.25,
                  integrityScore: 100,
                  provider: 'xfyun-ise',
                  providerSessionId: 'ise-session-1',
                  category: 'read_sentence',
                  notice: '根据本次录音自动评估，仅供练习参考。',
                  reasonCode: '',
                )
        : const SpeechFeedbackAcousticAssessment(
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
