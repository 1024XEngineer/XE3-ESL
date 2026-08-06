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
    expect(find.textContaining('Part 2&3'), findsOneWidget);
    expect(find.textContaining('共 14/14 题'), findsOneWidget);
    expect(find.textContaining('缺少可信发音工件'), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-overall-unavailable')),
      findsOneWidget,
    );
    expect(find.text('暂不可用'), findsOneWidget);
    expect(find.textContaining('非 IELTS 官方成绩'), findsOneWidget);
    expect(find.textContaining('距目标差值'), findsOneWidget);
    expect(find.byKey(const Key('ielts-speaking-score-radar')), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-evidence-standard')),
      findsOneWidget,
    );
    expect(find.text('部分练习报告'), findsNothing);
    expect(find.text('分 Part 复盘'), findsNothing);
    expect(find.text('同题复练'), findsOneWidget);
    expect(find.byKey(const Key('ielts-speaking-question-14')), findsOneWidget);
    expect(
      find.byKey(const Key('ielts-speaking-repractice-14')),
      findsOneWidget,
    );
    expect(find.textContaining('Band 0'), findsNothing);
  });

  testWidgets('same-question list starts the selected question directly', (
    tester,
  ) async {
    final controller = await _controllerFor('ready');
    addTearDown(controller.dispose);
    IeltsSpeakingQuestionReview? selected;

    await tester.pumpWidget(
      _app(
        controller,
        onRepracticeQuestion: (question) async {
          selected = question;
          return true;
        },
      ),
    );
    await tester.pump();
    final button = find.byKey(const Key('ielts-speaking-repractice-1'));
    await tester.ensureVisible(button);
    await tester.tap(button);
    await tester.pump();

    expect(selected?.index, 1);
    expect(selected?.questionText, isNotEmpty);
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

  testWidgets('technical failure stays in automatic recovery without retry', (
    tester,
  ) async {
    final controller = await _controllerFor('failed');
    addTearDown(controller.dispose);

    await tester.pumpWidget(_app(controller));
    await tester.pump();

    expect(
      find.byKey(const Key('ielts-speaking-report-generating')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('ielts-speaking-report-retry')), findsNothing);
    expect(find.byKey(const Key('ielts-speaking-report-failed')), findsNothing);
    expect(find.textContaining('失败'), findsNothing);

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

Widget _app(
  IeltsSpeakingReportController controller, {
  Future<bool> Function(IeltsSpeakingQuestionReview question)?
  onRepracticeQuestion,
}) => MaterialApp(
  home: Scaffold(
    body: SingleChildScrollView(
      child: IeltsSpeakingReportPanel(
        controller: controller,
        onRepracticeQuestion: onRepracticeQuestion,
      ),
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
