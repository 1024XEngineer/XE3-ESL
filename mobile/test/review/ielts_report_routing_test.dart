import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
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
    expect(find.byKey(const Key('review-detail-title')), findsOneWidget);
    await tester.tap(find.text('详细反馈'));
    await tester.pumpAndSettle();
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
    expect(find.text('Part 2 + Part 3 联合复盘'), findsOneWidget);
    expect(find.byKey(const Key('review-detail-summary')), findsNothing);
    expect(find.text('整体表现'), findsNothing);
    expect(find.byKey(const Key('ielts-section-report')), findsNothing);
    expect(find.byKey(const Key('ielts-section-detail-invalid')), findsNothing);
  });

  testWidgets(
    'single Part report prioritizes summary, scores, and next action',
    (tester) async {
      const finding = EvaluationReportFinding(
        id: 'improvement_task',
        message: '交流目标表达不够清晰。',
        suggestion: 'State the intended outcome first.',
        evidence: <EvaluationReportEvidence>[
          EvaluationReportEvidence(
            evidenceRefId: 'evidence_1',
            turnId: 'turn_1',
            startUtf8Byte: 0,
            endUtf8Byte: 8,
            originalExcerpt: 'I think maybe...',
          ),
        ],
      );
      final item = _item(
        practiceMode: 'PART_1',
        detailSchema: 'general-scene-evaluation/v1',
        summary: '回答基本完成，但交流目标表达不够清晰。',
        dimensions: const <EvaluationReportDimension>[
          EvaluationReportDimension(
            key: 'TASK_ACHIEVEMENT',
            score: 5,
            scale: EvaluationReportScoreScale.percentage100,
            coverage: 1,
            confidence: 0.8,
            reasonCodes: <String>[],
            evidenceRefIds: <String>['evidence_1'],
            strengths: <EvaluationReportFinding>[],
            improvements: <EvaluationReportFinding>[finding],
            recommendedExamples: <EvaluationReportFinding>[],
          ),
        ],
        priorityActions: const <EvaluationReportPriorityAction>[
          EvaluationReportPriorityAction(
            dimensionKey: 'TASK_ACHIEVEMENT',
            findingId: 'improvement_task',
          ),
        ],
      );

      await tester.pumpWidget(
        MaterialApp(home: ReviewReportDetailPage(item: item)),
      );
      await tester.pumpAndSettle();

      expect(find.text('已完成'), findsNothing);
      expect(find.text('本次表现'), findsNothing);
      expect(find.text(item.report.summary), findsNothing);
      expect(find.byKey(const Key('review-detail-dimensions')), findsOneWidget);
      expect(
        find.byKey(const Key('review-section-score-radar')),
        findsOneWidget,
      );
      expect(find.byType(FourAxisScoreRadar), findsOneWidget);
      expect(find.byType(LinearProgressIndicator), findsNothing);
      expect(find.text('任务达成'), findsWidgets);
      expect(find.text('5'), findsOneWidget);
      expect(find.textContaining('非 IELTS'), findsNothing);
      await tester.drag(
        find.byKey(const Key('review-detail-content')),
        const Offset(0, -400),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('review-detail-priority-focus')),
        findsOneWidget,
      );
      expect(find.text('下一步先练这个'), findsOneWidget);
      expect(find.text('State the intended outcome first.'), findsOneWidget);
      expect(find.text('“I think maybe...”'), findsOneWidget);
      await tester.drag(
        find.byKey(const Key('review-detail-content')),
        const Offset(0, -500),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('review-detail-feedback')), findsOneWidget);
    },
  );

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
        findsNothing,
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
  String summary = '本次练习已形成复盘。',
  List<EvaluationReportDimension> dimensions =
      const <EvaluationReportDimension>[],
  List<EvaluationReportPriorityAction> priorityActions =
      const <EvaluationReportPriorityAction>[],
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
    summary: summary,
    dimensions: dimensions,
    priorityActions: priorityActions,
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
