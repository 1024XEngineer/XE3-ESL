import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_client.dart';

final class SessionEvaluationController extends ChangeNotifier {
  SessionEvaluationController({
    required this.client,
    this.pollInterval = const Duration(seconds: 2),
    this.maximumPolls = 150,
  }) {
    if (pollInterval <= Duration.zero) {
      throw ArgumentError.value(pollInterval, 'pollInterval');
    }
    if (maximumPolls < 1) {
      throw ArgumentError.value(maximumPolls, 'maximumPolls');
    }
  }

  final SessionEvaluationClient client;
  final Duration pollInterval;
  final int maximumPolls;

  SessionEvaluation? _evaluation;
  String? _practiceSessionId;
  String? _errorMessage;
  bool _isLoading = false;
  int _generation = 0;

  SessionEvaluation? get evaluation => _evaluation;
  String? get practiceSessionId => _practiceSessionId;
  String? get errorMessage => _errorMessage;
  bool get isLoading => _isLoading;
  bool get canRetry =>
      _practiceSessionId != null &&
      !_isLoading &&
      _evaluation?.status == SessionEvaluationStatus.failed &&
      (_evaluation?.failure?.retryable ?? false);

  Future<void> load(String practiceSessionId) async {
    final generation = ++_generation;
    _practiceSessionId = practiceSessionId;
    _evaluation = null;
    _errorMessage = null;
    _isLoading = true;
    notifyListeners();
    for (var poll = 0; poll < maximumPolls; poll++) {
      try {
        final value = await client.get(practiceSessionId);
        if (generation != _generation) return;
        _evaluation = value;
        _errorMessage = value.status == SessionEvaluationStatus.failed
            ? value.failure?.message ?? '本次复盘生成失败。'
            : null;
        final terminal =
            value.status == SessionEvaluationStatus.ready ||
            value.status == SessionEvaluationStatus.failed;
        _isLoading = !terminal;
        notifyListeners();
        if (terminal) return;
      } on SessionEvaluationException catch (error) {
        if (generation != _generation ||
            error.kind == SessionEvaluationFailureKind.superseded) {
          return;
        }
        _isLoading = false;
        _errorMessage = _messageFor(error.kind);
        notifyListeners();
        return;
      }
      await Future<void>.delayed(pollInterval);
      if (generation != _generation) return;
    }
    if (generation != _generation) return;
    _isLoading = false;
    _errorMessage = '复盘仍在生成中，请稍后刷新。';
    notifyListeners();
  }

  Future<void> retry() async {
    final sessionId = _practiceSessionId;
    if (sessionId == null || !canRetry) return;
    final generation = ++_generation;
    _errorMessage = null;
    _isLoading = true;
    notifyListeners();
    try {
      final value = await client.retry(sessionId);
      if (generation != _generation) return;
      _evaluation = value;
      if (value.status == SessionEvaluationStatus.ready) {
        _isLoading = false;
        notifyListeners();
        return;
      }
    } on SessionEvaluationException catch (error) {
      if (generation != _generation ||
          error.kind == SessionEvaluationFailureKind.superseded) {
        return;
      }
      _isLoading = false;
      _errorMessage = _messageFor(error.kind);
      notifyListeners();
      return;
    }
    if (generation != _generation) return;
    await load(sessionId);
  }

  void cancel(String practiceSessionId) {
    if (_practiceSessionId != practiceSessionId) return;
    _generation++;
    _isLoading = false;
  }

  Future<void> clearAccountState() async {
    _generation++;
    _practiceSessionId = null;
    _evaluation = null;
    _errorMessage = null;
    _isLoading = false;
    await client.clearAccountState();
    notifyListeners();
  }
}

String _messageFor(SessionEvaluationFailureKind kind) => switch (kind) {
  SessionEvaluationFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
  SessionEvaluationFailureKind.notFound => '复盘任务尚未创建，请稍后重试。',
  SessionEvaluationFailureKind.invalidRequest ||
  SessionEvaluationFailureKind.invalidResponse => '复盘响应无法识别，请稍后重试。',
  SessionEvaluationFailureKind.network ||
  SessionEvaluationFailureKind.server => '复盘暂时无法加载，请检查网络后重试。',
  SessionEvaluationFailureKind.superseded => '',
};
