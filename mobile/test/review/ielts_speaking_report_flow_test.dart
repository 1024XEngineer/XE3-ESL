import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_controller.dart';
import 'package:speakup/features/coaching/review/review.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  testWidgets('IELTS history reopens the authoritative session report', (
    tester,
  ) async {
    final updatedAt = DateTime.utc(2026, 7, 30, 9, 10);
    final item = IeltsSpeakingReportIndexItem(
      reportKind: IeltsSpeakingReportKind.fullMock,
      practiceSessionId: 'session_ielts_report_001',
      evaluationId: '7b000101-0000-4000-8000-000000000001',
      evaluationRevisionId: 'a1000101-0000-4000-8000-000000000001',
      revision: 1,
      evaluationStatus: IeltsSpeakingReportEvaluationStatus.ready,
      isFinal: false,
      statusUrl:
          '/v1/practice-sessions/session_ielts_report_001/'
          'ielts-speaking-report',
      createdAt: updatedAt.subtract(const Duration(minutes: 10)),
      updatedAt: updatedAt,
    );
    final indexClient = _IndexClient([item]);
    final indexController = IeltsSpeakingReportIndexController(
      client: indexClient,
    );
    final reportClient = _ReportClient(
      decodeIeltsSpeakingReport(ieltsSpeakingReportContractFixture()['ready']),
    );
    final reportController = IeltsSpeakingReportController(
      client: reportClient,
      maximumPollAttempts: 1,
    );
    addTearDown(indexController.dispose);
    addTearDown(reportController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(
          ieltsSpeakingReportController: reportController,
          ieltsSpeakingReportIndexController: indexController,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(indexClient.calls, 1);
    expect(find.byKey(const Key('review-ability-card')), findsOneWidget);
    expect(find.byKey(const Key('review-ability-empty')), findsOneWidget);
    expect(
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
      findsNothing,
    );
    await _toggleHistory(tester);
    await _scrollReviewTo(
      tester,
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
    );

    expect(find.text('IELTS 模考报告'), findsOneWidget);
    expect(find.textContaining('评分报告已生成'), findsOneWidget);
    expect(find.text('部分练习报告'), findsNothing);
    expect(
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
      findsOneWidget,
    );

    await tester.tap(
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
    );
    await tester.pumpAndSettle();

    expect(reportClient.sessionIds, everyElement('session_ielts_report_001'));
    expect(reportClient.sessionIds.length, greaterThanOrEqualTo(2));
    expect(
      find.byKey(const Key('ielts-speaking-report-ready')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-speaking-overall-unavailable')),
      findsOneWidget,
    );

    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(reportController.practiceSessionId, 'session_ielts_report_001');
    expect(reportController.envelope, isNotNull);
  });

  testWidgets('Review leads with the latest complete four-band ability radar', (
    tester,
  ) async {
    final now = DateTime.utc(2026, 8, 5, 9);
    final item = IeltsSpeakingReportIndexItem(
      reportKind: IeltsSpeakingReportKind.fullMock,
      practiceSessionId: 'session_ielts_report_001',
      evaluationId: '7b000101-0000-4000-8000-000000000001',
      evaluationRevisionId: 'a1000101-0000-4000-8000-000000000001',
      revision: 1,
      evaluationStatus: IeltsSpeakingReportEvaluationStatus.ready,
      isFinal: false,
      statusUrl:
          '/v1/practice-sessions/session_ielts_report_001/'
          'ielts-speaking-report',
      createdAt: now,
      updatedAt: now,
    );
    final indexController = IeltsSpeakingReportIndexController(
      client: _IndexClient([item]),
    );
    final reportController = IeltsSpeakingReportController(
      client: _ReportClient(_completeReportEnvelope()),
      maximumPollAttempts: 1,
    );
    addTearDown(indexController.dispose);
    addTearDown(reportController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(
          ieltsSpeakingReportController: reportController,
          ieltsSpeakingReportIndexController: indexController,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('个人能力'), findsOneWidget);
    expect(find.byKey(const Key('review-ability-radar')), findsOneWidget);
    expect(find.byKey(const Key('review-ability-empty')), findsNothing);
    expect(find.byKey(const Key('review-ability-summary')), findsOneWidget);
    expect(find.text('综合得分'), findsOneWidget);
    expect(find.text('6.5'), findsOneWidget);
    expect(find.text('四项等权平均后按 0.5 分取整。'), findsOneWidget);
    expect(find.text('流利与连贯'), findsOneWidget);
    expect(find.text('词汇'), findsOneWidget);
    expect(find.text('语法'), findsOneWidget);
    expect(find.text('发音'), findsOneWidget);
    expect(find.text('7.0'), findsOneWidget);
    expect(find.text('6.0'), findsNWidgets(3));
    expect(
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
      findsNothing,
    );

    await _toggleHistory(tester);
    await _scrollReviewTo(
      tester,
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
    );
    expect(
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('review-history-back')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_report_001'),
      ),
      findsNothing,
    );
  });

  testWidgets(
    'interview and IELTS full mock reports render in separate sections',
    (tester) async {
      final now = DateTime.utc(2026, 7, 30, 9, 10);
      final interviewItem = IeltsSpeakingReportIndexItem(
        reportKind: IeltsSpeakingReportKind.interview,
        practiceSessionId: 'session_interview_001',
        evaluationId: '7b000101-0000-4000-8000-000000000002',
        evaluationRevisionId: 'a1000101-0000-4000-8000-000000000002',
        revision: 1,
        evaluationStatus: IeltsSpeakingReportEvaluationStatus.ready,
        isFinal: false,
        statusUrl:
            '/v1/practice-sessions/session_interview_001/interview-report',
        createdAt: now.subtract(const Duration(minutes: 10)),
        updatedAt: now,
        title: 'AI产品经理模拟面试',
      );
      final ieltsItem = IeltsSpeakingReportIndexItem(
        reportKind: IeltsSpeakingReportKind.fullMock,
        practiceSessionId: 'session_ielts_001',
        evaluationId: '7b000101-0000-4000-8000-000000000003',
        evaluationRevisionId: 'a1000101-0000-4000-8000-000000000003',
        revision: 1,
        evaluationStatus: IeltsSpeakingReportEvaluationStatus.ready,
        isFinal: false,
        statusUrl:
            '/v1/practice-sessions/session_ielts_001/ielts-speaking-report',
        createdAt: now.subtract(const Duration(minutes: 10)),
        updatedAt: now,
      );
      final indexController = IeltsSpeakingReportIndexController(
        client: _IndexClient([interviewItem, ieltsItem]),
      );
      addTearDown(indexController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: ReviewPage(ieltsSpeakingReportIndexController: indexController),
        ),
      );
      await tester.pumpAndSettle();

      await _toggleHistory(tester);
      await _scrollReviewTo(
        tester,
        find.byKey(
          const Key('ielts-report-history-select-session_interview_001'),
        ),
      );

      // The interview section shows the practice title on its card; the
      // IELTS mock card falls back to the report-type label.
      expect(find.text('面试练习报告'), findsOneWidget);
      expect(find.text('AI产品经理模拟面试'), findsOneWidget);
      await tester.scrollUntilVisible(
        find.text('IELTS 口语完整模考'),
        240,
        scrollable: find
            .descendant(
              of: find.byKey(const Key('review-history-list')),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      expect(find.text('IELTS 模考报告'), findsOneWidget);
      expect(find.text('IELTS 口语完整模考'), findsOneWidget);
      expect(
        find.byKey(
          const Key('ielts-report-history-select-session_interview_001'),
        ),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('ielts-report-history-select-session_ielts_001')),
        findsOneWidget,
      );
    },
  );

  testWidgets('failed report revisions are presented as automatic recovery', (
    tester,
  ) async {
    final now = DateTime.utc(2026, 7, 30, 9, 10);
    final item = IeltsSpeakingReportIndexItem(
      reportKind: IeltsSpeakingReportKind.fullMock,
      practiceSessionId: 'session_ielts_recovering',
      evaluationId: '7b000101-0000-4000-8000-000000000004',
      evaluationRevisionId: 'a1000101-0000-4000-8000-000000000004',
      revision: 1,
      evaluationStatus: IeltsSpeakingReportEvaluationStatus.failed,
      isFinal: false,
      statusUrl:
          '/v1/practice-sessions/session_ielts_recovering/'
          'ielts-speaking-report',
      createdAt: now.subtract(const Duration(minutes: 10)),
      updatedAt: now,
    );
    final indexController = IeltsSpeakingReportIndexController(
      client: _IndexClient([item]),
    );
    addTearDown(indexController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(ieltsSpeakingReportIndexController: indexController),
      ),
    );
    await tester.pumpAndSettle();

    await _toggleHistory(tester);
    await _scrollReviewTo(
      tester,
      find.byKey(
        const Key('ielts-report-history-select-session_ielts_recovering'),
      ),
    );

    expect(find.textContaining('报告自动恢复中'), findsOneWidget);
    expect(find.textContaining('报告生成失败'), findsNothing);
  });
}

final class _IndexClient implements IeltsSpeakingReportIndexClient {
  _IndexClient(this.items);

  final List<IeltsSpeakingReportIndexItem> items;
  int calls = 0;

  @override
  Future<IeltsSpeakingReportIndexPage> listReports({
    String? cursor,
    int limit = 20,
  }) async {
    calls++;
    return IeltsSpeakingReportIndexPage(items: items);
  }

  @override
  Future<void> clearAccountState() async {}
}

Future<void> _toggleHistory(WidgetTester tester) async {
  final entry = find.byKey(const Key('review-history-entry'));
  final scrollable = find
      .descendant(
        of: find.byKey(const Key('review-overview-scroll')),
        matching: find.byType(Scrollable),
      )
      .first;
  await tester.scrollUntilVisible(entry, 200, scrollable: scrollable);
  await tester.drag(scrollable, const Offset(0, -120));
  await tester.pumpAndSettle();
  await tester.tap(entry);
  await tester.pumpAndSettle();
  expect(find.byKey(const Key('review-history-page')), findsOneWidget);
}

Future<void> _scrollReviewTo(WidgetTester tester, Finder target) async {
  final scrollable = find
      .descendant(
        of: find.byKey(const Key('review-history-list')),
        matching: find.byType(Scrollable),
      )
      .first;
  await tester.scrollUntilVisible(target, 200, scrollable: scrollable);
  await tester.pumpAndSettle();
}

IeltsSpeakingReportEnvelope _completeReportEnvelope() {
  final value = cloneIeltsSpeakingReportFixture(
    ieltsSpeakingReportContractFixture()['ready'],
  );
  final report = value['report']! as Map<String, Object?>;
  final criteria = report['criteria']! as List<Object?>;
  final fluency = criteria[0]! as Map<String, Object?>;
  fluency
    ..['estimated_band'] = 7
    ..['band_descriptor'] = 'Band 7 fluency descriptor.'
    ..['reason_codes'] = <Object?>['PRACTICE_ESTIMATE_UNCALIBRATED'];
  final pronunciation = cloneIeltsSpeakingReportFixture(fluency)
    ..['criterion_id'] = 'IELTS_PR'
    ..['estimated_band'] = 6
    ..['band_descriptor'] = 'Band 6 pronunciation descriptor.';
  final strengths = pronunciation['strengths']! as List<Object?>;
  final pronunciationFinding = strengths.single! as Map<String, Object?>;
  pronunciationFinding['finding_id'] = 'ielts_finding_pr_001';
  criteria[3] = pronunciation;

  final questions = report['questions']! as List<Object?>;
  final firstQuestion = questions.first! as Map<String, Object?>;
  final refs = firstQuestion['criterion_findings']! as List<Object?>;
  refs[3] = <String, Object?>{
    'criterion_id': 'IELTS_PR',
    'strength_finding_ids': <Object?>['ielts_finding_pr_001'],
    'improvement_finding_ids': <Object?>[],
    'upgrade_example_finding_ids': <Object?>[],
  };
  final parts = report['part_reviews']! as List<Object?>;
  final part1 = parts.first! as Map<String, Object?>;
  (part1['strength_finding_ids']! as List<Object?>).add('ielts_finding_pr_001');
  report['speaking_overall'] = <String, Object?>{
    'status': 'AVAILABLE',
    'estimated_band': 6.5,
    'explanation': '四项等权平均后按 0.5 分取整。',
  };
  return decodeIeltsSpeakingReport(value);
}

final class _ReportClient implements IeltsSpeakingReportClient {
  _ReportClient(this.value);

  final IeltsSpeakingReportEnvelope value;
  final List<String> sessionIds = <String>[];

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async {
    sessionIds.add(practiceSessionId);
    return value;
  }

  @override
  Future<void> clearAccountState() async {}
}
