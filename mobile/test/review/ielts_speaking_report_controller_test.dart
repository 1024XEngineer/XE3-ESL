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

  test('polling window continues automatically without a user retry', () async {
    final queued = decodeIeltsSpeakingReport(
      ieltsSpeakingReportContractFixture()['queued'],
    );
    final ready = decodeIeltsSpeakingReport(
      ieltsSpeakingReportContractFixture()['ready'],
    );
    final client = _QueueClient([queued, queued, ready]);
    final controller = IeltsSpeakingReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 2,
      automaticRecoveryInterval: Duration.zero,
    );
    addTearDown(controller.dispose);

    await controller.load('session_ielts_report_001');
    await _waitFor(
      () =>
          controller.envelope?.evaluationStatus ==
          IeltsSpeakingReportEvaluationStatus.ready,
    );

    expect(client.calls, 3);
    expect(controller.isLoading, isFalse);
    expect(controller.canRetry, isFalse);
    expect(controller.errorMessage, isNull);
  });

  test(
    'terminal report is replaced automatically even when marked nonretryable',
    () async {
      final fixture = ieltsSpeakingReportContractFixture();
      final failedValue = cloneIeltsSpeakingReportFixture(fixture['failed']);
      failedValue['stable_failure'] = <String, Object?>{
        'reason_code': 'VERSION_CONFLICT',
        'retryable': false,
      };
      final client = _RegeneratingClient(
        decodeIeltsSpeakingReport(failedValue),
        decodeIeltsSpeakingReport(fixture['ready']),
      );
      final controller = IeltsSpeakingReportController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 3,
      );
      addTearDown(controller.dispose);

      await controller.load('session_ielts_report_001');

      expect(client.regenerationCalls, 1);
      expect(client.getCalls, 2);
      expect(
        controller.envelope?.evaluationStatus,
        IeltsSpeakingReportEvaluationStatus.ready,
      );
      expect(controller.canRetry, isFalse);
      expect(controller.errorMessage, isNull);
    },
  );

  test(
    'automatically replaces a retryable failed revision and reaches READY',
    () async {
      final fixture = ieltsSpeakingReportContractFixture();
      final client = _RegeneratingClient(
        decodeIeltsSpeakingReport(fixture['failed']),
        decodeIeltsSpeakingReport(fixture['ready']),
      );
      final controller = IeltsSpeakingReportController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 3,
      );
      addTearDown(controller.dispose);

      await controller.load('session_ielts_report_001');

      expect(client.regenerationCalls, 1);
      expect(client.getCalls, 2);
      expect(
        controller.envelope?.evaluationStatus,
        IeltsSpeakingReportEvaluationStatus.ready,
      );
      expect(controller.errorMessage, isNull);
      expect(controller.canRetry, isFalse);
    },
  );

  test('automatic recovery never creates revisions without a bound', () async {
    final failed = decodeIeltsSpeakingReport(
      ieltsSpeakingReportContractFixture()['failed'],
    );
    final client = _AlwaysFailedRegeneratingClient(failed);
    final controller = IeltsSpeakingReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 2,
      maximumAutomaticRegenerations: 1,
      automaticRecoveryInterval: Duration.zero,
    );
    addTearDown(controller.dispose);

    await controller.load('session_ielts_report_001');
    await Future<void>.delayed(Duration.zero);

    expect(client.regenerationCalls, 1);
  });

  test('keeps polling through a transient revision conflict', () async {
    final ready = decodeIeltsSpeakingReport(
      ieltsSpeakingReportContractFixture()['ready'],
    );
    final client = _TransientFailureClient(
      const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.conflict,
        statusCode: 409,
        retryable: true,
      ),
      ready,
    );
    final controller = IeltsSpeakingReportController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 2,
    );
    addTearDown(controller.dispose);

    await controller.load('session_ielts_report_001');

    expect(client.calls, 2);
    expect(
      controller.envelope?.evaluationStatus,
      IeltsSpeakingReportEvaluationStatus.ready,
    );
    expect(controller.errorMessage, isNull);
  });

  test(
    'keeps polling while the completion hook is creating the report',
    () async {
      final ready = decodeIeltsSpeakingReport(
        ieltsSpeakingReportContractFixture()['ready'],
      );
      final client = _TransientFailureClient(
        const IeltsSpeakingReportException(
          kind: IeltsSpeakingReportFailureKind.notFound,
          statusCode: 404,
        ),
        ready,
      );
      final controller = IeltsSpeakingReportController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 2,
      );
      addTearDown(controller.dispose);

      await controller.load('session_ielts_report_001');

      expect(client.calls, 2);
      expect(
        controller.envelope?.evaluationStatus,
        IeltsSpeakingReportEvaluationStatus.ready,
      );
    },
  );

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

Future<void> _waitFor(bool Function() condition) async {
  for (var attempt = 0; attempt < 20; attempt++) {
    if (condition()) {
      return;
    }
    await Future<void>.delayed(Duration.zero);
  }
  fail('condition was not reached');
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

final class _RegeneratingClient
    implements
        IeltsSpeakingReportClient,
        IeltsSpeakingReportRegenerationClient {
  _RegeneratingClient(this.failed, this.ready);

  final IeltsSpeakingReportEnvelope failed;
  final IeltsSpeakingReportEnvelope ready;
  int getCalls = 0;
  int regenerationCalls = 0;

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async {
    return getCalls++ == 0 ? failed : ready;
  }

  @override
  Future<void> regenerateReport(IeltsSpeakingReportEnvelope envelope) async {
    regenerationCalls++;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _AlwaysFailedRegeneratingClient
    implements
        IeltsSpeakingReportClient,
        IeltsSpeakingReportRegenerationClient {
  _AlwaysFailedRegeneratingClient(this.failed);

  final IeltsSpeakingReportEnvelope failed;
  int regenerationCalls = 0;

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async => failed;

  @override
  Future<void> regenerateReport(IeltsSpeakingReportEnvelope envelope) async {
    regenerationCalls++;
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _TransientFailureClient implements IeltsSpeakingReportClient {
  _TransientFailureClient(this.failure, this.ready);

  final IeltsSpeakingReportException failure;
  final IeltsSpeakingReportEnvelope ready;
  int calls = 0;

  @override
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async {
    if (calls++ == 0) {
      throw failure;
    }
    return ready;
  }

  @override
  Future<void> clearAccountState() async {}
}
