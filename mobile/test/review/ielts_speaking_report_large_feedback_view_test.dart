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
    for (final criterion in IeltsSpeakingCriterionId.values) {
      expect(
        find.byKey(Key('ielts-speaking-criterion-${criterion.name}')),
        findsOneWidget,
      );
    }
    expect(find.text('评分描述'), findsOneWidget);
    expect(find.text('改进建议'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-target-not-configured')),
      findsOneWidget,
    );
    expect(
      find.textContaining('Replace the general word with one exact noun.'),
      findsWidgets,
    );
  });
}
