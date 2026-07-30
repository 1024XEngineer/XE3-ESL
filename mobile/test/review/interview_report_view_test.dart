import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/review/interview_report_view.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_client.dart';
import 'package:speakup/review/interview_report_controller.dart';
import 'package:speakup/review/interview_report_decoder.dart';

import 'interview_report_fixture.dart';

void main() {
  testWidgets(
    'READY renders qualitative evidence without numeric or acoustic scores',
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

      expect(find.text('暂定文本反馈'), findsOneWidget);
      expect(
        find.byKey(const Key('interview-report-readiness-notice')),
        findsOneWidget,
      );
      expect(find.text('五维反馈'), findsOneWidget);
      expect(find.text('逐题复盘'), findsOneWidget);
      expect(find.text('优先改进'), findsOneWidget);
      expect(find.textContaining('I led the migration'), findsOneWidget);
      expect(find.textContaining('/ 100'), findsNothing);
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
