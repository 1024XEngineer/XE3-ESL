import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_client.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_controller.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/review_summary.dart';
import 'package:speakup/features/coaching/review/session_evaluation_page.dart';

import 'evaluation_report_fixture.dart';

void main() {
  test(
    'polls a running Session Evaluation until the report is ready',
    () async {
      final client = _EvaluationClient(<SessionEvaluation>[
        _evaluation(SessionEvaluationStatus.running),
        _readyEvaluation(EvaluationReportSceneType.interview),
      ]);
      final controller = SessionEvaluationController(
        client: client,
        pollInterval: const Duration(milliseconds: 1),
        maximumPolls: 2,
      );
      addTearDown(controller.dispose);

      await controller.load(_sessionId);

      expect(client.calls, 2);
      expect(controller.isLoading, isFalse);
      expect(controller.errorMessage, isNull);
      expect(controller.evaluation?.status, SessionEvaluationStatus.ready);
      expect(
        controller.evaluation?.report?.sceneType,
        EvaluationReportSceneType.interview,
      );
    },
  );

  for (final sceneType in <EvaluationReportSceneType>[
    EvaluationReportSceneType.ieltsSpeaking,
    EvaluationReportSceneType.interview,
  ]) {
    testWidgets('opens the ready ${sceneType.name} report directly', (
      tester,
    ) async {
      final controller = SessionEvaluationController(
        client: _EvaluationClient(<SessionEvaluation>[
          _readyEvaluation(sceneType),
        ]),
        pollInterval: const Duration(milliseconds: 1),
        maximumPolls: 1,
      );
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: SessionEvaluationPage(
            practiceSessionId: _sessionId,
            controller: controller,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
      expect(
        find.text(
          sceneType == EvaluationReportSceneType.ieltsSpeaking
              ? 'IELTS 口语模考报告'
              : '面试复盘',
        ),
        findsOneWidget,
      );
    });
  }

  testWidgets('shows a failed evaluation and retries the same Session', (
    tester,
  ) async {
    final client = _EvaluationClient(<SessionEvaluation>[
      _failedEvaluation(),
      _readyEvaluation(EvaluationReportSceneType.interview),
    ]);
    final controller = SessionEvaluationController(
      client: client,
      pollInterval: const Duration(milliseconds: 1),
      maximumPolls: 1,
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SessionEvaluationPage(
          practiceSessionId: _sessionId,
          controller: controller,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('评分服务暂不可用。'), findsOneWidget);
    expect(find.text('重新加载'), findsOneWidget);

    await tester.tap(find.text('重新加载'));
    await tester.pumpAndSettle();

    expect(client.calls, 2);
    expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
  });
}

final class _EvaluationClient implements SessionEvaluationClient {
  _EvaluationClient(this._responses);

  final List<SessionEvaluation> _responses;
  int calls = 0;

  @override
  Future<SessionEvaluation> get(String practiceSessionId) async {
    expect(practiceSessionId, _sessionId);
    final index = calls < _responses.length ? calls : _responses.length - 1;
    calls++;
    return _responses[index];
  }

  @override
  Future<void> clearAccountState() async {}
}

SessionEvaluation _evaluation(SessionEvaluationStatus status) =>
    SessionEvaluation(
      evaluationId: _evaluationId,
      practiceSessionId: _sessionId,
      status: status,
      updatedAt: _completedAt,
    );

SessionEvaluation _readyEvaluation(EvaluationReportSceneType sceneType) =>
    SessionEvaluation(
      evaluationId: _evaluationId,
      practiceSessionId: _sessionId,
      status: SessionEvaluationStatus.ready,
      updatedAt: _completedAt,
      report: evaluationReportFixture(
        review: const ReviewSummary(
          id: 'review-ready',
          title: 'Ready',
          summary: '本次练习已完成。',
          strength: '回答切题。',
          nextFocus: '表达更具体。',
        ),
        practiceSessionId: _sessionId,
        completedAt: _completedAt,
        sceneType: sceneType,
      ),
    );

SessionEvaluation _failedEvaluation() => SessionEvaluation(
  evaluationId: _evaluationId,
  practiceSessionId: _sessionId,
  status: SessionEvaluationStatus.failed,
  updatedAt: _completedAt,
  failure: const SessionEvaluationFailure(
    code: 'PROVIDER_UNAVAILABLE',
    retryable: true,
    message: '评分服务暂不可用。',
  ),
);

const _sessionId = '30000000-0000-4000-8000-000000000003';
const _evaluationId = '70000000-0000-4000-8000-000000000007';
final _completedAt = DateTime.utc(2026, 8, 16, 10);
