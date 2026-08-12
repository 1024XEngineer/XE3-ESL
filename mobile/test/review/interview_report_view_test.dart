import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/interview_report_view.dart';
import 'package:speakup/features/coaching/review/interview_report.dart';
import 'package:speakup/features/coaching/review/interview_report_client.dart';
import 'package:speakup/features/coaching/review/interview_report_controller.dart';
import 'package:speakup/features/coaching/review/interview_report_decoder.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_client.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

import 'interview_report_fixture.dart';

void main() {
  testWidgets(
    'READY renders real dimension scores without invented acoustic scores',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(320, 568));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final ready = decodeInterviewReport(
        interviewReportContractFixture()['ready'],
      );
      final controller = InterviewReportController(
        client: _FixedClient(ready),
        maximumPollAttempts: 1,
      );
      addTearDown(controller.dispose);
      await controller.load(ready.practiceSessionId);

      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) {
            return MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: const TextScaler.linear(2)),
              child: child!,
            );
          },
          home: Scaffold(
            body: ListView(
              padding: const EdgeInsets.all(12),
              children: [InterviewReportPanel(controller: controller)],
            ),
          ),
        ),
      );
      await tester.pump();

      expect(find.text('面试能力反馈'), findsOneWidget);
      expect(
        find.byKey(const Key('interview-report-readiness-notice')),
        findsOneWidget,
      );
      expect(find.text('五维反馈概览'), findsOneWidget);
      expect(
        find.byKey(const Key('interview-report-dimension-radar')),
        findsOneWidget,
      );
      expect(find.text('逐题复盘'), findsOneWidget);
      expect(find.text('优先改进'), findsOneWidget);
      expect(find.textContaining('I led the migration'), findsOneWidget);
      expect(find.text('回答相关性 · 78 / 100'), findsOneWidget);
      expect(find.textContaining('录用概率：'), findsNothing);
      expect(tester.takeException(), isNull);

      await tester.scrollUntilVisible(
        find.byKey(const Key('interview-report-priority-actions')),
        300,
      );
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('INSUFFICIENT renders evidence shortage without a zero', (
    tester,
  ) async {
    final insufficient = decodeInterviewReport(
      interviewReportContractFixture()['insufficient'],
    );
    final controller = InterviewReportController(
      client: _FixedClient(insufficient),
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load(insufficient.practiceSessionId);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: InterviewReportPanel(controller: controller)),
      ),
    );

    expect(
      find.byKey(const Key('interview-report-insufficient')),
      findsOneWidget,
    );
    expect(find.text('证据不足'), findsOneWidget);
    expect(find.textContaining('0 分'), findsOneWidget);
    expect(find.textContaining('/ 100'), findsNothing);
    expect(find.byKey(const Key('interview-report-dimensions')), findsNothing);
  });

  testWidgets('report includes real per-turn acoustic scores', (tester) async {
    final ready = decodeInterviewReport(
      interviewReportContractFixture()['ready'],
    );
    final reportController = InterviewReportController(
      client: _FixedClient(ready),
      maximumPollAttempts: 1,
    );
    final now = DateTime.utc(2026, 7, 31);
    final feedbackController = SpeechFeedbackController(
      client: _FixedSpeechFeedbackClient(
        SpeechFeedback(
          speechFeedbackId: 'feedback-1',
          source: const ConversationTurnFeedbackSource(
            practiceSessionId: 'session-1',
            turnId: 'turn-1',
            inputRevision: 1,
            evidenceSnapshotId: 'evidence-1',
          ),
          feedbackStatus: SpeechFeedbackStatus.ready,
          scoreabilityStatus: SpeechFeedbackScoreabilityStatus.provisional,
          gateStatus: SpeechFeedbackGateStatus.feedbackOnly,
          reasonCodes: const [],
          schemaVersion: 'speech-feedback/v1',
          strategyRef: 'qianwen-speech-feedback/v1',
          pipelineVersion: 'speech-feedback-pipeline/v1',
          isFinal: false,
          items: const [],
          acousticAssessment: const SpeechFeedbackAcousticAssessment(
            pronunciation: SpeechFeedbackAssessmentStatus.assessed,
            acousticFluency: SpeechFeedbackAssessmentStatus.assessed,
            reasonCode: '',
            pronunciationScore: 82,
            speakingSpeedWpm: 118,
            semanticScore: 76,
            provider: 'xfyun-ise',
            providerSessionId: 'provider-1',
            category: 'topic',
            notice: '根据本次录音自动评估，仅供练习参考。',
          ),
          statusUrl: '/v1/speech-feedback/feedback-1',
          createdAt: now,
          updatedAt: now,
          completedAt: now,
        ),
      ),
      maximumPollAttempts: 1,
    );
    addTearDown(reportController.dispose);
    addTearDown(feedbackController.dispose);
    await feedbackController.load(
      sourceKey: 'practice:session-1:turn-1',
      statusUrl: '/v1/speech-feedback/feedback-1',
    );

    await tester.pumpWidget(
      MaterialApp(
        home: InterviewReportPage(
          practiceSessionId: ready.practiceSessionId,
          controller: reportController,
          speechFeedbackController: feedbackController,
          speechFeedbackSourceKeys: const ['practice:session-1:turn-1'],
        ),
      ),
    );
    await tester.pump();

    expect(find.text('语言表现 · 逐轮真实数据'), findsOneWidget);
    await tester.tap(
      find.byKey(const Key('speech-feedback-disclosure-toggle')),
    );
    await tester.pump();
    expect(find.textContaining('发音准确度 82'), findsOneWidget);
    expect(find.textContaining('语速 118'), findsOneWidget);
    expect(find.textContaining('题意相关 76'), findsOneWidget);
  });

  testWidgets('FAILED is explicitly technical and only retries when allowed', (
    tester,
  ) async {
    final failed = decodeInterviewReport(
      interviewReportContractFixture()['failed'],
    );
    final controller = InterviewReportController(
      client: _FixedClient(failed),
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load(failed.practiceSessionId);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: InterviewReportPanel(controller: controller)),
      ),
    );

    expect(find.textContaining('技术问题'), findsOneWidget);
    expect(find.textContaining('表现较差'), findsOneWidget);
    expect(find.byKey(const Key('interview-report-retry')), findsOneWidget);
    expect(find.byKey(const Key('interview-report-dimensions')), findsNothing);
  });

  testWidgets('return to conversation invokes the completion callback', (
    tester,
  ) async {
    final ready = decodeInterviewReport(
      interviewReportContractFixture()['ready'],
    );
    final controller = InterviewReportController(
      client: _FixedClient(ready),
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    var continued = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: InterviewReportPanel(
              controller: controller,
              onReturnToConversation: () async {
                continued = true;
                return false;
              },
            ),
          ),
        ),
      ),
    );
    await controller.load(ready.practiceSessionId);
    await tester.pump();
    await tester.ensureVisible(
      find.byKey(const Key('interview-report-return-conversation')),
    );
    await tester.tap(
      find.byKey(const Key('interview-report-return-conversation')),
    );
    await tester.pump();

    expect(continued, isTrue);
  });
}

final class _FixedClient implements InterviewReportClient {
  const _FixedClient(this.value);

  final InterviewReportEnvelope value;

  @override
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) async =>
      value;

  @override
  Future<void> clearAccountState() async {}
}

final class _FixedSpeechFeedbackClient implements SpeechFeedbackClient {
  const _FixedSpeechFeedbackClient(this.value);

  final SpeechFeedback value;

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) async => value;

  @override
  Future<void> clearAccountState() async {}
}
