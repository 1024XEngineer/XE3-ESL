import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_client.dart';
import 'package:speakup/review/interview_report_controller.dart';
import 'package:speakup/review/interview_report_decoder.dart';

import 'interview_report_fixture.dart';

void main() {
  test('polls QUEUED and RUNNING until READY', () async {
    final fixture = interviewReportContractFixture();
    final client = _QueueClient([
      decodeInterviewReport(fixture['queued']),
      decodeInterviewReport(fixture['running']),
      decodeInterviewReport(fixture['ready']),
    ]);
    final controller = InterviewReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 3,
    );
    addTearDown(controller.dispose);

    await controller.load('session_interview_report_001');

    expect(client.calls, 3);
    expect(
      controller.envelope?.evaluationStatus,
      InterviewReportEvaluationStatus.ready,
    );
    expect(controller.isLoading, isFalse);
    expect(controller.errorMessage, isNull);
  });

  test('bounded polling stops and exposes an explicit retry', () async {
    final queued = decodeInterviewReport(
      interviewReportContractFixture()['queued'],
    );
    final client = _QueueClient([queued, queued]);
    final controller = InterviewReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 2,
    );
    addTearDown(controller.dispose);

    await controller.load('session_interview_report_001');

    expect(client.calls, 2);
    expect(controller.isLoading, isFalse);
    expect(controller.canRetry, isTrue);
    expect(controller.errorMessage, '报告仍在生成，请稍后重试。');
  });

  test(
    'retries an initial 404 while the completion task is being queued',
    () async {
      final ready = decodeInterviewReport(
        interviewReportContractFixture()['ready'],
      );
      final client = _NotFoundThenReadyClient(ready);
      final controller = InterviewReportController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 2,
      );
      addTearDown(controller.dispose);

      await controller.load('session_interview_report_001');

      expect(client.calls, 2);
      expect(
        controller.envelope?.evaluationStatus,
        InterviewReportEvaluationStatus.ready,
      );
      expect(controller.errorMessage, isNull);
    },
  );

  test('leaving the detail fences a late response and clears memory', () async {
    final client = _ControlledClient();
    final controller = InterviewReportController(client: client);
    addTearDown(controller.dispose);
    final pending = controller.load('session_interview_report_001');
    await client.started.future;

    controller.cancel('session_interview_report_001');
    client.response.complete(
      decodeInterviewReport(interviewReportContractFixture()['ready']),
    );
    await pending;

    expect(controller.practiceSessionId, isNull);
    expect(controller.envelope, isNull);
    expect(controller.errorMessage, isNull);
  });

  test(
    'account clear cancels work, clears result, and clears the client',
    () async {
      final ready = decodeInterviewReport(
        interviewReportContractFixture()['ready'],
      );
      final client = _QueueClient([ready]);
      final controller = InterviewReportController(client: client);
      addTearDown(controller.dispose);
      await controller.load('session_interview_report_001');

      await controller.clearPrivateState();

      expect(client.clearCalls, 1);
      expect(controller.practiceSessionId, isNull);
      expect(controller.envelope, isNull);
      expect(controller.errorMessage, isNull);
    },
  );
}

final class _QueueClient implements InterviewReportClient {
  _QueueClient(this.responses);

  final List<InterviewReportEnvelope> responses;
  int calls = 0;
  int clearCalls = 0;

  @override
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) async {
    final index = calls++;
    return responses[index];
  }

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }
}

final class _ControlledClient implements InterviewReportClient {
  final started = Completer<void>();
  final response = Completer<InterviewReportEnvelope>();

  @override
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) {
    started.complete();
    return response.future;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _NotFoundThenReadyClient implements InterviewReportClient {
  _NotFoundThenReadyClient(this.ready);

  final InterviewReportEnvelope ready;
  int calls = 0;

  @override
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) async {
    calls++;
    if (calls == 1) {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.notFound,
      );
    }
    return ready;
  }

  @override
  Future<void> clearAccountState() async {}
}
