import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/review/interview_report.dart';
import 'package:speakup/features/coaching/review/interview_report_client.dart';

final class InterviewReportController extends ChangeNotifier {
  InterviewReportController({
    required this.client,
    this.pollInterval = const Duration(seconds: 2),
    this.maximumPollAttempts = 30,
  }) {
    if (pollInterval < Duration.zero) {
      throw ArgumentError.value(pollInterval, 'pollInterval');
    }
    if (maximumPollAttempts < 1) {
      throw ArgumentError.value(maximumPollAttempts, 'maximumPollAttempts');
    }
  }

  final InterviewReportClient client;
  final Duration pollInterval;
  final int maximumPollAttempts;

  String? _practiceSessionId;
  InterviewReportEnvelope? _envelope;
  String? _errorMessage;
  InterviewReportFailureKind? _failureKind;
  bool _loading = false;
  bool _canRetry = false;
  bool _disposed = false;
  int _requestGeneration = 0;

  String? get practiceSessionId => _practiceSessionId;
  InterviewReportEnvelope? get envelope => _envelope;
  String? get errorMessage => _errorMessage;
  InterviewReportFailureKind? get failureKind => _failureKind;
  bool get isLoading => _loading;
  bool get canRetry => _canRetry;

  Future<void> load(String practiceSessionId) async {
    if (_disposed || practiceSessionId.isEmpty) {
      return;
    }
    final generation = ++_requestGeneration;
    _practiceSessionId = practiceSessionId;
    _envelope = null;
    _errorMessage = null;
    _failureKind = null;
    _canRetry = false;
    _loading = true;
    notifyListeners();

    for (var attempt = 0; attempt < maximumPollAttempts; attempt++) {
      try {
        final envelope = await client.getReport(practiceSessionId);
        if (!_isCurrent(generation, practiceSessionId)) {
          return;
        }
        _envelope = envelope;
        _errorMessage = null;
        _failureKind = null;
        final pending =
            envelope.evaluationStatus ==
                InterviewReportEvaluationStatus.queued ||
            envelope.evaluationStatus ==
                InterviewReportEvaluationStatus.running;
        if (!pending) {
          _loading = false;
          _canRetry =
              envelope.evaluationStatus ==
                  InterviewReportEvaluationStatus.failed &&
              (envelope.stableFailure?.retryable ?? false);
          notifyListeners();
          return;
        }
        notifyListeners();
        if (attempt + 1 < maximumPollAttempts) {
          await Future<void>.delayed(pollInterval);
          if (!_isCurrent(generation, practiceSessionId)) {
            return;
          }
        }
      } on InterviewReportException catch (error) {
        if (!_isCurrent(generation, practiceSessionId) ||
            error.kind == InterviewReportFailureKind.superseded) {
          return;
        }
        if (error.kind == InterviewReportFailureKind.notFound &&
            attempt + 1 < maximumPollAttempts) {
          await Future<void>.delayed(pollInterval);
          if (!_isCurrent(generation, practiceSessionId)) {
            return;
          }
          continue;
        }
        _loading = false;
        _failureKind = error.kind;
        _canRetry =
            error.retryable ||
            error.kind == InterviewReportFailureKind.notFound;
        _errorMessage = _messageFor(error);
        notifyListeners();
        return;
      } on Object {
        if (!_isCurrent(generation, practiceSessionId)) {
          return;
        }
        _loading = false;
        _failureKind = InterviewReportFailureKind.network;
        _canRetry = true;
        _errorMessage = '面试报告暂时无法加载，请稍后重试。';
        notifyListeners();
        return;
      }
    }
    if (_isCurrent(generation, practiceSessionId)) {
      _loading = false;
      _failureKind = InterviewReportFailureKind.network;
      _canRetry = true;
      _errorMessage = '报告仍在生成，请稍后重试。';
      notifyListeners();
    }
  }

  Future<void> retry() {
    final sessionId = _practiceSessionId;
    return sessionId == null ? Future<void>.value() : load(sessionId);
  }

  void cancel(String practiceSessionId) {
    if (_disposed || _practiceSessionId != practiceSessionId) {
      return;
    }
    _requestGeneration++;
    _reset();
    notifyListeners();
  }

  Future<void> clearPrivateState() async {
    _requestGeneration++;
    _reset();
    await client.clearAccountState();
    if (!_disposed) {
      notifyListeners();
    }
  }

  bool _isCurrent(int generation, String practiceSessionId) =>
      !_disposed &&
      generation == _requestGeneration &&
      practiceSessionId == _practiceSessionId;

  void _reset() {
    _practiceSessionId = null;
    _envelope = null;
    _errorMessage = null;
    _failureKind = null;
    _loading = false;
    _canRetry = false;
  }

  @override
  void dispose() {
    _disposed = true;
    _requestGeneration++;
    _reset();
    super.dispose();
  }
}

String _messageFor(InterviewReportException error) {
  return switch (error.kind) {
    InterviewReportFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
    InterviewReportFailureKind.notFound => '这次面试的报告尚未生成。',
    InterviewReportFailureKind.conflict => '这次面试存在多份结果，暂时无法安全展示。',
    InterviewReportFailureKind.invalidResponse => '面试报告响应无法识别，请稍后重试。',
    InterviewReportFailureKind.network ||
    InterviewReportFailureKind.server => '面试报告暂时无法加载，请稍后重试。',
    InterviewReportFailureKind.superseded => '',
  };
}
