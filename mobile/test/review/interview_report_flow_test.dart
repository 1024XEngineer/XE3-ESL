import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/features/coaching/review/formal_review.dart';
import 'package:speakup/features/coaching/review/interview_report.dart';
import 'package:speakup/features/coaching/review/interview_report_client.dart';
import 'package:speakup/features/coaching/review/interview_report_controller.dart';
import 'package:speakup/features/coaching/review/interview_report_decoder.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';

import 'interview_report_fixture.dart';

void main() {
  testWidgets('Interview detail loads its session report and clears on leave', (
    tester,
  ) async {
    final item = _sceneItem(
      contextType: FormalReviewContextType.interviewProjectDeepDive,
      practiceSessionId: 'session_interview_report_001',
    );
    final history = ReviewHistoryController(client: _HistoryClient([item]));
    final reportClient = _ReportClient(
      value: decodeInterviewReport(interviewReportContractFixture()['ready']),
    );
    final report = InterviewReportController(
      client: reportClient,
      maximumPollAttempts: 1,
    );
    addTearDown(history.dispose);
    addTearDown(report.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(
          historyController: history,
          interviewReportController: report,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(reportClient.sessionIds, ['session_interview_report_001']);
    expect(find.byKey(const Key('interview-report-ready')), findsOneWidget);
    expect(find.byKey(const Key('review-detail-summary')), findsNothing);

    await tester.tap(find.byKey(const Key('review-detail-back')));
    await tester.pumpAndSettle();
    expect(report.practiceSessionId, isNull);
    expect(report.envelope, isNull);
  });

  testWidgets('a report 404 preserves the existing FormalReview detail', (
    tester,
  ) async {
    final item = _sceneItem(
      contextType: FormalReviewContextType.interviewProjectDeepDive,
      practiceSessionId: 'session_interview_report_404',
    );
    final history = ReviewHistoryController(client: _HistoryClient([item]));
    final reportClient = _ReportClient(
      error: const InterviewReportException(
        kind: InterviewReportFailureKind.notFound,
      ),
    );
    final report = InterviewReportController(
      client: reportClient,
      maximumPollAttempts: 1,
    );
    addTearDown(history.dispose);
    addTearDown(report.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(
          historyController: history,
          interviewReportController: report,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('review-detail-summary')), findsOneWidget);
    expect(find.text('FormalReview 仍可查看。'), findsOneWidget);
    expect(find.byKey(const Key('interview-report-failed')), findsNothing);
  });

  testWidgets('non-Interview FormalReview never requests an Interview report', (
    tester,
  ) async {
    final item = _sceneItem(
      contextType: FormalReviewContextType.dailyHotelCheckinIssue,
      practiceSessionId: 'session_daily_001',
    );
    final history = ReviewHistoryController(client: _HistoryClient([item]));
    final reportClient = _ReportClient(
      value: decodeInterviewReport(interviewReportContractFixture()['ready']),
    );
    final report = InterviewReportController(
      client: reportClient,
      maximumPollAttempts: 1,
    );
    addTearDown(history.dispose);
    addTearDown(report.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(
          historyController: history,
          interviewReportController: report,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(reportClient.sessionIds, isEmpty);
    expect(find.byKey(const Key('review-detail-summary')), findsOneWidget);
    expect(find.byKey(const Key('interview-report-ready')), findsNothing);
  });
}

ReviewHistoryItem _sceneItem({
  required FormalReviewContextType contextType,
  required String practiceSessionId,
}) {
  final review = AgentReview(
    id: 'review_$practiceSessionId',
    title: '面试复盘',
    summary: 'FormalReview 仍可查看。',
    strength: '回答与问题相关。',
    nextFocus: '下一次补充具体结果。',
  );
  final createdAt = DateTime.utc(2026, 7, 30, 9);
  final completedAt = DateTime.utc(2026, 7, 30, 9, 10);
  final formalReview = FormalReview(
    id: review.id,
    practiceSessionId: practiceSessionId,
    status: FormalReviewStatus.completed,
    schema: FormalReviewSchema.sceneV2,
    implementationVersion: 'qianwen-scene-review-v2',
    sourceTurnId: 'turn_review_001',
    sourceTurnVersion: 'conversation-turn:evidence-v1',
    contextType: contextType,
    result: const FormalReviewResult(
      eligibility: FormalReviewSummaryEligibility.provisional,
      summary: 'FormalReview 仍可查看。',
      dimensions: <FormalReviewDimension>[
        FormalReviewDimension(
          key: 'relevance',
          category: 'relevance',
          message: '回答与问题相关。',
        ),
      ],
      feedbackItems: <FormalReviewFeedbackItem>[],
      repracticeSuggestionRefs: <String>[],
      insufficientEvidenceReasons: <String>['audio_not_assessed'],
    ),
    createdAt: createdAt,
    updatedAt: completedAt,
    completedAt: completedAt,
  );
  return ReviewHistoryItem(
    review: review,
    formalReview: formalReview,
    practiceSessionId: practiceSessionId,
    createdAt: createdAt,
    completedAt: completedAt,
  );
}

final class _HistoryClient implements ReviewHistoryClient {
  const _HistoryClient(this.items);

  final List<ReviewHistoryItem> items;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async =>
      ReviewHistoryPage(items: items);

  @override
  Future<void> clearAccountState() async {}
}

final class _ReportClient implements InterviewReportClient {
  _ReportClient({this.value, this.error});

  final InterviewReportEnvelope? value;
  final InterviewReportException? error;
  final List<String> sessionIds = <String>[];

  @override
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) async {
    sessionIds.add(practiceSessionId);
    if (error case final failure?) {
      throw failure;
    }
    return value!;
  }

  @override
  Future<void> clearAccountState() async {}
}
