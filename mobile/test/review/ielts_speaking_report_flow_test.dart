import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/review/ielts_speaking_report.dart';
import 'package:speakup/review/ielts_speaking_report_client.dart';
import 'package:speakup/review/ielts_speaking_report_controller.dart';
import 'package:speakup/review/ielts_speaking_report_decoder.dart';
import 'package:speakup/review/ielts_speaking_report_index.dart';
import 'package:speakup/review/ielts_speaking_report_index_client.dart';
import 'package:speakup/review/ielts_speaking_report_index_controller.dart';

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
    final indexClient = _IndexClient(item);
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
    expect(find.text('IELTS 模考报告'), findsOneWidget);
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

    expect(reportClient.sessionIds, ['session_ielts_report_001']);
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
    expect(reportController.practiceSessionId, isNull);
    expect(reportController.envelope, isNull);
  });
}

final class _IndexClient implements IeltsSpeakingReportIndexClient {
  _IndexClient(this.item);

  final IeltsSpeakingReportIndexItem item;
  int calls = 0;

  @override
  Future<IeltsSpeakingReportIndexPage> listReports({
    String? cursor,
    int limit = 20,
  }) async {
    calls++;
    return IeltsSpeakingReportIndexPage(items: [item]);
  }

  @override
  Future<void> clearAccountState() async {}
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
