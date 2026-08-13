import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  testWidgets('renders a complete report with more than three Part findings', (
    tester,
  ) async {
    final value = completeIeltsSpeakingReportContractFixture();
    final report = value['report']! as Map<String, Object?>;
    final criteria = report['criteria']! as List<Object?>;
    final lexical = criteria[1]! as Map<String, Object?>;
    final improvements = lexical['improvements']! as List<Object?>;
    final source = improvements.single! as Map<String, Object?>;
    for (final id in <String>['ielts_finding_lr_002', 'ielts_finding_lr_003']) {
      improvements.add(
        cloneIeltsSpeakingReportFixture(source)..['finding_id'] = id,
      );
    }

    final questions = report['questions']! as List<Object?>;
    final firstQuestion = questions.first! as Map<String, Object?>;
    final questionFindings =
        firstQuestion['criterion_findings']! as List<Object?>;
    (questionFindings[1]!
        as Map<String, Object?>)['improvement_finding_ids'] = <Object?>[
      'ielts_finding_lr_001',
      'ielts_finding_lr_002',
      'ielts_finding_lr_003',
    ];

    final parts = report['part_reviews']! as List<Object?>;
    (parts.first!
        as Map<String, Object?>)['improvement_finding_ids'] = <Object?>[
      'ielts_finding_lr_001',
      'ielts_finding_lr_002',
      'ielts_finding_lr_003',
      'ielts_finding_gra_001',
    ];

    final decoded = decodeIeltsSpeakingReport(value).report!;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: IeltsSpeakingReadyReportView(report: decoded),
          ),
        ),
      ),
    );

    expect(
      find.byKey(const Key('ielts-speaking-overall-available')),
      findsOneWidget,
    );
    expect(find.text('6.5'), findsOneWidget);
    final radar = tester.widget<FourAxisScoreRadar>(
      find.byType(FourAxisScoreRadar),
    );
    expect(radar.maximum, 9);
    expect(radar.cornerLabels, isTrue);
    expect(radar.height, 300);
    expect(radar.axes.map((axis) => axis.label), <String>[
      '流利性与连贯性',
      '词汇丰富度',
      '发音',
      '语法多样性及准确性',
    ]);
    for (final criterion in IeltsSpeakingCriterionId.values) {
      expect(
        find.byKey(Key('ielts-speaking-criterion-${criterion.name}')),
        findsOneWidget,
      );
    }
    expect(find.text('评分描述'), findsOneWidget);
    expect(find.text('改进建议'), findsOneWidget);
    expect(find.byKey(const Key('ielts-speaking-target-plan')), findsOneWidget);
    expect(find.textContaining('尚未配置目标 Band'), findsNothing);
    expect(
      find.textContaining('Replace the general word with one exact noun.'),
      findsWidgets,
    );
    expect(find.text(decoded.testSummary.part1Topic), findsNothing);
    expect(find.text(decoded.testSummary.part2Topic), findsNothing);
    expect(
      find.byKey(const Key('ielts-speaking-overall-explanation')),
      findsOneWidget,
    );
    expect(find.text(decoded.disclaimer), findsNothing);
    expect(find.text('评分依据'), findsNothing);
    expect(
      find.byKey(const Key('ielts-speaking-evidence-standard')),
      findsNothing,
    );
    expect(find.text('做得好'), findsNothing);
    expect(find.text('可改进'), findsNothing);
    for (final criterion in decoded.criteria) {
      expect(
        find.byKey(Key('ielts-speaking-explanation-${criterion.id.name}')),
        findsOneWidget,
      );
      for (final finding in criterion.upgradeExamples) {
        expect(find.text(finding.message), findsWidgets);
        if (finding.suggestion case final suggestion?) {
          expect(find.text('建议：$suggestion'), findsWidgets);
        }
        for (final evidence in finding.evidence) {
          expect(find.text('原句：“${evidence.originalExcerpt}”'), findsWidgets);
        }
      }
    }
    for (final action in decoded.priorityActions) {
      final finding = decoded.finding(action.findingId)!;
      expect(find.text(finding.suggestion ?? finding.message), findsWidgets);
    }
  });

  testWidgets('stays scrollable on a narrow screen with larger text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(640, 1440);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final decoded = decodeIeltsSpeakingReport(
      completeIeltsSpeakingReportContractFixture(),
    ).report!;

    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(1.3)),
          child: child!,
        ),
        home: IeltsSpeakingReportScaffold(
          title: 'IELTS 口语模考报告',
          child: IeltsSpeakingReadyReportView(report: decoded),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.text('IELTS'), findsOneWidget);
    expect(find.text('口语模考报告'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-report-scroll')),
      findsOneWidget,
    );
    await tester.ensureVisible(
      find.byKey(const Key('ielts-speaking-target-plan')),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(
      find.byKey(const Key('ielts-speaking-report-disclaimer')),
      findsNothing,
    );
  });

  testWidgets(
    'keeps an insufficient full-mock title in the two-line hierarchy',
    (tester) async {
      tester.view.physicalSize = const Size(640, 1440);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        const MaterialApp(
          home: IeltsSpeakingReportScaffold(
            title: 'IELTS 口语模考报告 · 证据不足',
            child: SizedBox.shrink(),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(find.text('IELTS'), findsOneWidget);
      expect(find.text('口语模考报告 · 证据不足'), findsOneWidget);
      expect(find.text('IELTS 口语模考报告 · 证据不足'), findsNothing);

      final firstLine = tester.getRect(find.text('IELTS'));
      final secondLine = tester.getRect(find.text('口语模考报告 · 证据不足'));
      expect(secondLine.top, greaterThanOrEqualTo(firstLine.bottom));
      expect(secondLine.right, lessThanOrEqualTo(320));
    },
  );
}
