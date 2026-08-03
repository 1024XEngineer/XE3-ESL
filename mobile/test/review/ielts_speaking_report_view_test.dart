import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  testWidgets('renders honest partial Bands and unavailable dimensions', (
    tester,
  ) async {
    final controller = await _controllerFor('ready');
    addTearDown(controller.dispose);

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-report-ready')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-speaking-band-lexicalResource')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-speaking-band-grammaticalRangeAndAccuracy')),
      findsOneWidget,
    );
    expect(find.text('IELTS Speaking'), findsOneWidget);
    expect(find.textContaining('共 14/14 题'), findsOneWidget);
    expect(find.textContaining('缺少可信发音工件'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-overall-unavailable')),
      findsOneWidget,
    );
    expect(find.text('暂不可用'), findsOneWidget);
    expect(find.textContaining('非 IELTS 官方成绩'), findsOneWidget);
    expect(find.textContaining('距目标差值'), findsOneWidget);
    expect(find.byKey(const Key('ielts-speaking-question-14')), findsOneWidget);
    expect(find.textContaining('Band 0'), findsNothing);
  });

  testWidgets('renders insufficient evidence without a zero score', (
    tester,
  ) async {
    final controller = await _controllerFor('ready_insufficient');
    addTearDown(controller.dispose);

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-report-insufficient')),
      findsOneWidget,
    );
    expect(find.textContaining('不会显示 0 分'), findsOneWidget);
    expect(find.textContaining('Band 0'), findsNothing);
  });

  testWidgets('technical failure is not displayed as poor performance', (
    tester,
  ) async {
    final controller = await _controllerFor('failed');
    addTearDown(controller.dispose);

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-report-failed')),
      findsOneWidget,
    );
    expect(find.textContaining('不代表你的 IELTS 口语表现较差'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-report-retry')),
      findsOneWidget,
    );
  });
}

Future<IeltsSpeakingReportController> _controllerFor(String fixtureKey) async {
  final envelope = decodeIeltsSpeakingReport(
    ieltsSpeakingReportContractFixture()[fixtureKey],
  );
  final controller = IeltsSpeakingReportController(
    client: _Client(envelope),
    pollInterval: Duration.zero,
    maximumPollAttempts: 1,
  );
  await controller.load('session_ielts_report_001');
  return controller;
}

Widget _app(IeltsSpeakingReportController controller) => MaterialApp(
  home: Scaffold(
    body: SingleChildScrollView(
      child: IeltsSpeakingReportPanel(controller: controller),
    ),
  ),
);

final class _Client implements IeltsSpeakingReportClient {
  const _Client(this.envelope);

  final IeltsSpeakingReportEnvelope envelope;

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async => envelope;

  @override
  Future<void> clearAccountState() async {}
}
