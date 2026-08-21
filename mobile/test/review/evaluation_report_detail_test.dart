import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_detail_page.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/evaluation_report_radar.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';

void main() {
  testWidgets('all scene families use the shared detail structure', (
    tester,
  ) async {
    final cases = <({EvaluationReport report, int axes, String badge})>[
      (
        report: _report(
          sceneType: EvaluationReportSceneType.ieltsSpeaking,
          practiceMode: 'PART_1',
          keys: const <String>[
            'FLUENCY_COHERENCE',
            'LEXICAL_RESOURCE',
            'GRAMMATICAL_RANGE_ACCURACY',
            'PRONUNCIATION',
          ],
        ),
        axes: 4,
        badge: 'IELTS Part 1',
      ),
      (
        report: _report(
          sceneType: EvaluationReportSceneType.interview,
          keys: const <String>[
            'INTERVIEW_RELEVANCE',
            'INTERVIEW_STRUCTURE',
            'INTERVIEW_EVIDENCE',
            'INTERVIEW_PROFESSIONAL',
            'INTERVIEW_INTERACTION',
          ],
        ),
        axes: 5,
        badge: '模拟面试',
      ),
      (
        report: _report(
          sceneType: EvaluationReportSceneType.overseasDailyLife,
          keys: const <String>[
            'TASK_ACHIEVEMENT',
            'CLARITY_COHERENCE',
            'LANGUAGE_CONTROL',
            'INTERACTION',
          ],
        ),
        axes: 4,
        badge: '日常沟通',
      ),
      (
        report: _report(
          sceneType: EvaluationReportSceneType.overseasWorkplace,
          keys: const <String>[
            'TASK_ACHIEVEMENT',
            'CLARITY_COHERENCE',
            'LANGUAGE_CONTROL',
            'INTERACTION',
          ],
        ),
        axes: 4,
        badge: '职场沟通',
      ),
    ];

    for (final testCase in cases) {
      await tester.pumpWidget(
        MaterialApp(home: ReviewReportDetailPage(item: _item(testCase.report))),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('evaluation-report-detail-page')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('evaluation-report-overview')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('evaluation-report-dimensions')),
        findsOneWidget,
      );
      expect(find.text(testCase.badge), findsOneWidget);
      final radar = tester.widget<EvaluationRadarChart>(
        find.byType(EvaluationRadarChart).first,
      );
      expect(radar.axes, hasLength(testCase.axes));
      expect(tester.takeException(), isNull);
    }
  });

  testWidgets('dimension and question disclosures expand shared content', (
    tester,
  ) async {
    const first = EvaluationReportFinding(
      id: 'first',
      message: '第一条改进依据',
      suggestion: '第一条建议',
      evidence: <EvaluationReportEvidence>[
        EvaluationReportEvidence(
          evidenceRefId: 'evidence',
          turnId: 'turn',
          startUtf8Byte: 0,
          endUtf8Byte: 5,
          originalExcerpt: 'frozen quote',
        ),
      ],
    );
    const second = EvaluationReportFinding(
      id: 'second',
      message: '第二条改进依据',
      suggestion: '第二条建议',
      evidence: <EvaluationReportEvidence>[],
    );
    final report = _report(
      sceneType: EvaluationReportSceneType.overseasWorkplace,
      keys: const <String>[
        'TASK_ACHIEVEMENT',
        'CLARITY_COHERENCE',
        'LANGUAGE_CONTROL',
        'INTERACTION',
      ],
      firstDimensionImprovements: const <EvaluationReportFinding>[
        first,
        second,
      ],
      questions: const <EvaluationReportQuestion>[
        EvaluationReportQuestion(
          id: 'question',
          position: 1,
          text: 'What happened?',
          answer: EvaluationReportAnswer(
            turnId: 'turn',
            transcript: 'I resolved the issue.',
          ),
        ),
      ],
    );

    await tester.pumpWidget(
      MaterialApp(home: ReviewReportDetailPage(item: _item(report))),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('frozen quote'), findsOneWidget);
    expect(find.text('第二条改进依据'), findsNothing);
    final scrollable = find.descendant(
      of: find.byKey(const Key('evaluation-report-detail-scroll')),
      matching: find.byType(Scrollable),
    );
    final moreButton = find.byKey(
      const Key('evaluation-report-dimension-more-TASK_ACHIEVEMENT'),
    );
    await tester.drag(scrollable, const Offset(0, -400));
    await tester.pumpAndSettle();
    await tester.ensureVisible(moreButton);
    await tester.pumpAndSettle();
    await tester.tap(moreButton);
    await tester.pumpAndSettle();
    expect(find.text('第二条改进依据'), findsOneWidget);
    expect(find.text('第二条建议'), findsOneWidget);

    final questionsToggle = find.byKey(
      const Key('evaluation-report-questions-toggle'),
    );
    await tester.drag(scrollable, const Offset(0, -500));
    await tester.pumpAndSettle();
    await tester.ensureVisible(questionsToggle);
    await tester.pumpAndSettle();
    await tester.tap(questionsToggle);
    await tester.pumpAndSettle();
    expect(find.textContaining('What happened?'), findsOneWidget);
    expect(find.text('I resolved the issue.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('insufficient report stays readable without a zero score', (
    tester,
  ) async {
    final report = _report(
      sceneType: EvaluationReportSceneType.overseasDailyLife,
      scoreability: EvaluationReportScoreability.insufficient,
      keys: const <String>[
        'TASK_ACHIEVEMENT',
        'CLARITY_COHERENCE',
        'LANGUAGE_CONTROL',
        'INTERACTION',
      ],
    );

    await tester.pumpWidget(
      MaterialApp(home: ReviewReportDetailPage(item: _item(report))),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('evaluation-report-insufficient-notice')),
      findsOneWidget,
    );
    expect(
      tester
          .widget<Text>(
            find.byKey(const Key('evaluation-report-overall-score')),
          )
          .data,
      '--',
    );
    expect(find.textContaining('0 / 100'), findsNothing);
  });

  testWidgets('five-axis overview survives narrow 2x text layout', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 720);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final report = _report(
      sceneType: EvaluationReportSceneType.interview,
      keys: const <String>[
        'INTERVIEW_RELEVANCE',
        'INTERVIEW_STRUCTURE',
        'INTERVIEW_EVIDENCE',
        'INTERVIEW_PROFESSIONAL',
        'INTERVIEW_INTERACTION',
      ],
    );

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(2)),
          child: ReviewReportDetailPage(item: _item(report)),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('evaluation-report-radar')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}

EvaluationReport _report({
  required EvaluationReportSceneType sceneType,
  required List<String> keys,
  String practiceMode = 'FULL_SIMULATION',
  EvaluationReportScoreability scoreability =
      EvaluationReportScoreability.provisional,
  List<EvaluationReportFinding> firstDimensionImprovements =
      const <EvaluationReportFinding>[],
  List<EvaluationReportQuestion> questions = const <EvaluationReportQuestion>[],
}) {
  final scale = sceneType == EvaluationReportSceneType.ieltsSpeaking
      ? EvaluationReportScoreScale.ieltsBand
      : EvaluationReportScoreScale.percentage100;
  return EvaluationReport(
    id: 'report-${sceneType.name}',
    evaluationId: 'evaluation',
    practiceSessionId: 'session',
    sceneType: sceneType,
    practiceExperience: 'experience',
    sceneCategory: 'category',
    practiceMode: practiceMode,
    scoreability: scoreability,
    summary: 'summary',
    questions: questions,
    dimensions: <EvaluationReportDimension>[
      for (var index = 0; index < keys.length; index++)
        EvaluationReportDimension(
          key: keys[index],
          score: scoreability == EvaluationReportScoreability.insufficient
              ? null
              : scale == EvaluationReportScoreScale.ieltsBand
              ? 6 + index * 0.5
              : 70 + index.toDouble(),
          scale: scale,
          coverage: scoreability == EvaluationReportScoreability.insufficient
              ? 0
              : 1,
          confidence: scoreability == EvaluationReportScoreability.insufficient
              ? 0
              : 0.8,
          reasonCodes: scoreability == EvaluationReportScoreability.insufficient
              ? const <String>['NO_EFFECTIVE_TURNS']
              : const <String>[],
          evidenceRefIds: const <String>[],
          strengths: const <EvaluationReportFinding>[],
          improvements: index == 0
              ? firstDimensionImprovements
              : const <EvaluationReportFinding>[],
          recommendedExamples: const <EvaluationReportFinding>[],
        ),
    ],
    priorityActions: const <EvaluationReportPriorityAction>[],
    createdAt: DateTime.utc(2026, 8, 21),
  );
}

ReviewHistoryItem _item(EvaluationReport report) => ReviewHistoryItem(
  review: presentEvaluationReport(report),
  report: report,
  practiceSessionId: report.practiceSessionId,
  createdAt: report.createdAt,
  completedAt: report.createdAt,
);
