import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  testWidgets('IELTS section report stays on the generic detail route', (
    tester,
  ) async {
    final item = _item(
      practiceMode: 'PART_2',
      detailSchema: 'ielts-speaking-practice-report/v1',
    );
    final history = ReviewHistoryController(client: _HistoryClient(item));
    final fullMockClient = _PendingIeltsClient();
    final fullMockController = IeltsSpeakingReportController(
      client: fullMockClient,
    );
    addTearDown(history.dispose);
    addTearDown(fullMockController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ReviewPage(
          historyController: history,
          ieltsSpeakingReportController: fullMockController,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('review-history-entry')));
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 350));

    expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
    expect(find.text('Part 2 + Part 3 联合复盘'), findsWidgets);
    expect(find.byKey(const Key('ielts-section-part2')), findsOneWidget);
    expect(find.byKey(const Key('ielts-section-part3')), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-report-detail-page')),
      findsNothing,
    );
    expect(fullMockClient.calls, 0);
  });

  testWidgets('legacy IELTS section report keeps scoped generic detail', (
    tester,
  ) async {
    final item = _item(
      practiceMode: 'PART_2',
      detailSchema: 'general-scene-evaluation/v1',
    );

    await tester.pumpWidget(
      MaterialApp(home: ReviewReportDetailPage(item: item)),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
    expect(find.text('Part 2 + Part 3 联合复盘'), findsWidgets);
    expect(find.byKey(const Key('review-detail-summary')), findsOneWidget);
    expect(find.byKey(const Key('ielts-section-report')), findsNothing);
    expect(find.byKey(const Key('ielts-section-detail-invalid')), findsNothing);
  });

  testWidgets('section detail must match the outer practice mode', (
    tester,
  ) async {
    final item = _item(
      practiceMode: 'PART_1',
      detailSchema: 'ielts-speaking-practice-report/v1',
    );

    await tester.pumpWidget(
      MaterialApp(home: ReviewReportDetailPage(item: item)),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('ielts-section-report')), findsNothing);
    expect(
      find.byKey(const Key('ielts-section-detail-invalid')),
      findsOneWidget,
    );
  });

  testWidgets(
    'full mock history opens its embedded report without refetching',
    (tester) async {
      final item = _item(
        practiceMode: 'FULL_MOCK',
        detailSchema: 'ielts-speaking-report/v1',
      );
      final history = ReviewHistoryController(client: _HistoryClient(item));
      final fullMockClient = _PendingIeltsClient();
      final fullMockController = IeltsSpeakingReportController(
        client: fullMockClient,
      );
      addTearDown(history.dispose);
      addTearDown(fullMockController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: ReviewPage(
            historyController: history,
            ieltsSpeakingReportController: fullMockController,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(fullMockClient.calls, 1);
      await tester.tap(find.byKey(const Key('review-history-entry')));
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(Key('review-history-select-${item.review.id}')),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));

      expect(
        find.byKey(const Key('ielts-speaking-report-detail-page')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('review-detail-page')), findsNothing);
      expect(
        find.byKey(const Key('ielts-speaking-report-scope-tabs')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('ielts-speaking-report-generating')),
        findsNothing,
      );
      expect(fullMockClient.calls, 1);
    },
  );
}

ReviewHistoryItem _item({
  required String practiceMode,
  required String detailSchema,
}) {
  final createdAt = DateTime.utc(2026, 8, 11, 4);
  final report = EvaluationReport(
    id: '20000000-0000-4000-8000-000000000002',
    evaluationId: '7b000001-0000-4000-8000-000000000001',
    evaluationRevisionId: 'a1000001-0000-4000-8000-000000000001',
    practiceSessionId: 'session_595',
    revision: 1,
    sceneType: EvaluationReportSceneType.ieltsSpeaking,
    practiceExperience: 'IELTS_SPEAKING',
    sceneCategory: 'IELTS_SPEAKING',
    practiceMode: practiceMode,
    scoreability: EvaluationReportScoreability.provisional,
    summary: '本次练习已形成复盘。',
    dimensions: const <EvaluationReportDimension>[],
    priorityActions: const <EvaluationReportPriorityAction>[],
    detailSchema: detailSchema,
    detail: switch (detailSchema) {
      'ielts-speaking-practice-report/v1' => _sectionDetail(),
      'ielts-speaking-report/v1' =>
        completeIeltsSpeakingReportContractFixture()['report']!
            as Map<String, Object?>,
      _ => <String, Object?>{'schema_version': detailSchema},
    },
    createdAt: createdAt,
  );
  return ReviewHistoryItem(
    review: presentEvaluationReport(report),
    report: report,
    practiceSessionId: report.practiceSessionId,
    createdAt: createdAt,
    completedAt: createdAt,
  );
}

Map<String, Object?> _sectionDetail() => <String, Object?>{
  'schema_version': 'ielts-speaking-practice-report/v1',
  'report_scope': 'PART_2_3',
  'available_sections': <Object?>['PART_2', 'PART_3'],
  'questions': <Object?>[
    _sectionQuestion(part: 'PART_2', index: 1),
    _sectionQuestion(part: 'PART_3', index: 2),
  ],
  'section_reviews': <Object?>[
    _sectionReview(part: 'PART_2', index: 1),
    _sectionReview(part: 'PART_3', index: 2),
  ],
};

Map<String, Object?> _sectionQuestion({
  required String part,
  required int index,
}) => <String, Object?>{
  'question_id': 'question_$index',
  'part_id': part,
  'index': index,
  'question_text': 'Question $index?',
  'confirmed_transcript': 'Answer $index.',
  'response_turn_id': 'turn_$index',
  'evidence_ref_ids': <Object?>['evidence_$index'],
};

Map<String, Object?> _sectionReview({
  required String part,
  required int index,
}) => <String, Object?>{
  'part_id': part,
  'question_indexes': <Object?>[index],
  'evidence_ref_ids': <Object?>['evidence_$index'],
  'strength_finding_ids': <Object?>[],
  'improvement_finding_ids': <Object?>[],
  'upgrade_example_finding_ids': <Object?>[],
};

final class _HistoryClient implements ReviewHistoryClient {
  const _HistoryClient(this.item);

  final ReviewHistoryItem item;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async =>
      ReviewHistoryPage(items: <ReviewHistoryItem>[item]);

  @override
  Future<void> clearAccountState() async {}
}

final class _PendingIeltsClient implements IeltsSpeakingReportClient {
  int calls = 0;
  final Completer<IeltsSpeakingReportEnvelope> pending =
      Completer<IeltsSpeakingReportEnvelope>();

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(String practiceSessionId) {
    calls++;
    return pending.future;
  }

  @override
  Future<void> clearAccountState() async {}
}
