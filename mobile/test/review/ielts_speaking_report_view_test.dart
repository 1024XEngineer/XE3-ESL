import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  testWidgets('keeps the empty ability profile compact', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: CustomScrollView(
            slivers: [
              SliverToBoxAdapter(
                child: IeltsSpeakingAbilityProfile(
                  report: null,
                  loading: false,
                ),
              ),
            ],
          ),
        ),
      ),
    );

    expect(find.byKey(const Key('review-ability-empty')), findsOneWidget);
    expect(
      tester.getSize(find.byType(IeltsSpeakingAbilityProfile)).height,
      lessThan(180),
    );
  });

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
    expect(find.text('本次模考'), findsOneWidget);
    expect(find.textContaining('Part 2&3'), findsOneWidget);
    expect(find.textContaining('共 14/14 题'), findsOneWidget);
    expect(find.textContaining('缺少可信发音工件'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-overall-unavailable')),
      findsOneWidget,
    );
    expect(
      tester.widget(
        find.byKey(const Key('ielts-speaking-overall-unavailable')),
      ),
      isA<Card>(),
    );
    expect(find.text('暂不可用'), findsOneWidget);
    expect(find.textContaining('非 IELTS 官方成绩'), findsOneWidget);
    expect(find.textContaining('距目标差值'), findsOneWidget);
    expect(find.byKey(const Key('ielts-speaking-score-radar')), findsOneWidget);
    expect(find.byType(FourAxisScoreRadar), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-evidence-standard')),
      findsOneWidget,
    );
    expect(find.text('部分练习报告'), findsNothing);
    expect(find.text('分 Part 复盘'), findsNothing);
    expect(find.text('同题复练'), findsNothing);
    expect(find.textContaining('Band 0'), findsNothing);
    for (final criterion in <String>[
      'fluencyAndCoherence',
      'lexicalResource',
      'grammaticalRangeAndAccuracy',
      'pronunciation',
    ]) {
      expect(
        find.descendant(
          of: find.byKey(Key('ielts-speaking-criterion-$criterion')),
          matching: find.byType(Icon),
        ),
        findsNothing,
      );
    }
  });

  testWidgets('renders all four Bands and rounded Overall', (tester) async {
    final envelope = decodeIeltsSpeakingReport(
      completeIeltsSpeakingReportContractFixture(),
    );
    final controller = IeltsSpeakingReportController(
      client: _Client(envelope),
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load('session_ielts_report_001');

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-overall-available')),
      findsOneWidget,
    );
    expect(find.text('6.5'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-band-pronunciation')),
      findsOneWidget,
    );
    expect(find.text('发音'), findsWidgets);
  });

  testWidgets('full mock report presents one continuous summary', (
    tester,
  ) async {
    final controller = await _controllerFor('ready');
    addTearDown(controller.dispose);

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-report-scope-tabs')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('ielts-speaking-report-criteria')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-speaking-report-questions')),
      findsNothing,
    );
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

  testWidgets('terminal technical failure offers an explicit retry', (
    tester,
  ) async {
    final controller = IeltsSpeakingReportController(
      client: _Client(
        decodeIeltsSpeakingReport(
          ieltsSpeakingReportContractFixture()['failed'],
        ),
      ),
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
      maximumAutomaticRegenerations: 0,
    );
    addTearDown(controller.dispose);
    await controller.load('session_ielts_report_001');

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-report-failed')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-speaking-report-retry')),
      findsOneWidget,
    );
    expect(find.textContaining('暂时无法生成'), findsOneWidget);

    controller.cancel('session_ielts_report_001');
  });

  testWidgets('session panel owns report loading and cancellation', (
    tester,
  ) async {
    final client = _PendingClient();
    final controller = IeltsSpeakingReportController(client: client);
    addTearDown(controller.dispose);

    Widget app(String sessionId) => MaterialApp(
      home: IeltsSpeakingSessionReportPanel(
        practiceSessionId: sessionId,
        controller: controller,
      ),
    );

    await tester.pumpWidget(app('session-one'));
    await tester.pump();
    expect(client.sessionIds, <String>['session-one']);
    expect(controller.practiceSessionId, 'session-one');

    await tester.pumpWidget(app('session-two'));
    await tester.pump();
    expect(client.sessionIds, <String>['session-one', 'session-two']);
    expect(controller.practiceSessionId, 'session-two');

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    expect(controller.practiceSessionId, isNull);
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

final class _PendingClient implements IeltsSpeakingReportClient {
  final sessionIds = <String>[];

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(String practiceSessionId) {
    sessionIds.add(practiceSessionId);
    return Completer<IeltsSpeakingReportEnvelope>().future;
  }

  @override
  Future<void> clearAccountState() async {}
}
