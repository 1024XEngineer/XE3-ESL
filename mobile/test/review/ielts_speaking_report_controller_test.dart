import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  test('polls QUEUED and RUNNING until READY', () async {
    final fixture = ieltsSpeakingReportContractFixture();
    final client = _QueueClient([
      decodeIeltsSpeakingReport(fixture['queued']),
      decodeIeltsSpeakingReport(fixture['running']),
      decodeIeltsSpeakingReport(fixture['ready']),
    ]);
    final controller = IeltsSpeakingReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 3,
    );
    addTearDown(controller.dispose);

    await controller.load('session_ielts_report_001');

    expect(client.calls, 3);
    expect(
      controller.envelope?.evaluationStatus,
      IeltsSpeakingReportEvaluationStatus.ready,
    );
    expect(controller.isLoading, isFalse);
    expect(controller.errorMessage, isNull);
  });

  test('bounded polling stops and exposes an explicit retry', () async {
    final queued = decodeIeltsSpeakingReport(
      ieltsSpeakingReportContractFixture()['queued'],
    );
    final client = _QueueClient([queued, queued]);
    final controller = IeltsSpeakingReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 2,
    );
    addTearDown(controller.dispose);

    await controller.load('session_ielts_report_001');

    expect(client.calls, 2);
    expect(controller.isLoading, isFalse);
    expect(controller.canRetry, isTrue);
    expect(controller.errorMessage, '报告仍在生成，请稍后重试。');
  });

  test('leaving the report fences a late response and clears memory', () async {
    final client = _ControlledClient();
    final controller = IeltsSpeakingReportController(client: client);
    addTearDown(controller.dispose);
    final pending = controller.load('session_ielts_report_001');
    await client.started.future;

    controller.cancel('session_ielts_report_001');
    client.response.complete(
      decodeIeltsSpeakingReport(ieltsSpeakingReportContractFixture()['ready']),
    );
    await pending;

    expect(controller.practiceSessionId, isNull);
    expect(controller.envelope, isNull);
    expect(controller.errorMessage, isNull);
  });

  test('account clear fences work and clears the private result', () async {
    final ready = decodeIeltsSpeakingReport(
      ieltsSpeakingReportContractFixture()['ready'],
    );
    final client = _QueueClient([ready]);
    final controller = IeltsSpeakingReportController(client: client);
    addTearDown(controller.dispose);
    await controller.load('session_ielts_report_001');

    await controller.clearPrivateState();

    expect(client.clearCalls, 1);
    expect(controller.practiceSessionId, isNull);
    expect(controller.envelope, isNull);
    expect(controller.errorMessage, isNull);
  });
}

final class _QueueClient implements IeltsSpeakingReportClient {
  _QueueClient(this.responses);

  final List<IeltsSpeakingReportEnvelope> responses;
  int calls = 0;
  int clearCalls = 0;

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async {
    final index = calls++;
    return responses[index];
  }

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }
}

final class _ControlledClient implements IeltsSpeakingReportClient {
  final started = Completer<void>();
  final response = Completer<IeltsSpeakingReportEnvelope>();

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(String practiceSessionId) {
    started.complete();
    return response.future;
  }

  @override
  Future<void> clearAccountState() async {}
}
