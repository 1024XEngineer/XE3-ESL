import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';

final class IeltsSpeakingReportController extends ChangeNotifier {
  IeltsSpeakingReportController({
    required this.client,
    this.pollInterval = const Duration(seconds: 2),
    this.maximumPollAttempts = 30,
    this.maximumAutomaticRegenerations = 1,
    this.maximumAutomaticRecoveryCycles = 5,
    this.automaticRecoveryInterval = const Duration(seconds: 10),
  }) {
    if (pollInterval < Duration.zero) {
      throw ArgumentError.value(pollInterval, 'pollInterval');
    }
    if (maximumPollAttempts < 1) {
      throw ArgumentError.value(maximumPollAttempts, 'maximumPollAttempts');
    }
    if (maximumAutomaticRegenerations < 0 ||
        maximumAutomaticRegenerations > 10) {
      throw ArgumentError.value(
        maximumAutomaticRegenerations,
        'maximumAutomaticRegenerations',
      );
    }
    if (maximumAutomaticRecoveryCycles < 0 ||
        maximumAutomaticRecoveryCycles > 10) {
      throw ArgumentError.value(
        maximumAutomaticRecoveryCycles,
        'maximumAutomaticRecoveryCycles',
      );
    }
    if (automaticRecoveryInterval < Duration.zero) {
      throw ArgumentError.value(
        automaticRecoveryInterval,
        'automaticRecoveryInterval',
      );
    }
  }

  final IeltsSpeakingReportClient client;
  final Duration pollInterval;
  final int maximumPollAttempts;
  final int maximumAutomaticRegenerations;
  final int maximumAutomaticRecoveryCycles;
  final Duration automaticRecoveryInterval;

  String? _practiceSessionId;
  IeltsSpeakingReportEnvelope? _envelope;
  String? _errorMessage;
  IeltsSpeakingReportFailureKind? _failureKind;
  bool _loading = false;
  bool _canRetry = false;
  bool _disposed = false;
  int _requestGeneration = 0;
  int _automaticRecoveryCycle = 0;
  int _automaticRegenerationCount = 0;
  Timer? _automaticRecoveryTimer;

  String? get practiceSessionId => _practiceSessionId;
  IeltsSpeakingReportEnvelope? get envelope => _envelope;
  String? get errorMessage => _errorMessage;
  IeltsSpeakingReportFailureKind? get failureKind => _failureKind;
  bool get isLoading => _loading;
  bool get canRetry => _canRetry;

  Future<void> load(String practiceSessionId) =>
      _load(practiceSessionId, automatic: false);

  Future<void> _load(
    String practiceSessionId, {
    required bool automatic,
  }) async {
    if (_disposed || practiceSessionId.isEmpty) {
      return;
    }
    final sameSession = _practiceSessionId == practiceSessionId;
    _automaticRecoveryTimer?.cancel();
    _automaticRecoveryTimer = null;
    if (!automatic || !sameSession) {
      _automaticRecoveryCycle = 0;
      if (!sameSession) {
        _automaticRegenerationCount = 0;
      }
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
                IeltsSpeakingReportEvaluationStatus.queued ||
            envelope.evaluationStatus ==
                IeltsSpeakingReportEvaluationStatus.running;
        final regenerationClient =
            client is IeltsSpeakingReportRegenerationClient
            ? client as IeltsSpeakingReportRegenerationClient
            : null;
        final automaticallyRecoverable =
            envelope.evaluationStatus ==
                IeltsSpeakingReportEvaluationStatus.failed &&
            regenerationClient != null &&
            _automaticRegenerationCount < maximumAutomaticRegenerations;
        if (automaticallyRecoverable) {
          _automaticRegenerationCount++;
          await regenerationClient.regenerateReport(envelope);
          if (!_isCurrent(generation, practiceSessionId)) {
            return;
          }
          _envelope = null;
          notifyListeners();
          if (attempt + 1 < maximumPollAttempts) {
            if (!await _waitBeforeNextPoll(generation, practiceSessionId)) {
              return;
            }
          }
          continue;
        }
        if (!pending) {
          if (envelope.evaluationStatus ==
              IeltsSpeakingReportEvaluationStatus.failed) {
            // A terminal revision cannot become READY by polling it. Keep the
            // bounded automatic regeneration above as the only background
            // mutation; otherwise every recovery window would create another
            // revision forever.
            _loading = false;
            _canRetry = true;
            _errorMessage = 'IELTS 练习报告生成失败，请重新生成。';
            notifyListeners();
            return;
          }
          _automaticRecoveryCycle = 0;
          _loading = false;
          _canRetry = false;
          notifyListeners();
          return;
        }
        notifyListeners();
        if (attempt + 1 < maximumPollAttempts) {
          if (!await _waitBeforeNextPoll(generation, practiceSessionId)) {
            return;
          }
        }
      } on IeltsSpeakingReportException catch (error) {
        if (!_isCurrent(generation, practiceSessionId)) {
          return;
        }
        final transient =
            error.kind == IeltsSpeakingReportFailureKind.notFound ||
            error.kind == IeltsSpeakingReportFailureKind.conflict ||
            error.retryable;
        if (transient && attempt + 1 < maximumPollAttempts) {
          _envelope = null;
          _errorMessage = null;
          _failureKind = null;
          _canRetry = false;
          notifyListeners();
          if (!await _waitBeforeNextPoll(generation, practiceSessionId)) {
            return;
          }
          continue;
        }
        if (error.kind == IeltsSpeakingReportFailureKind.superseded) {
          return;
        }
        _failureKind = error.kind;
        if (error.kind == IeltsSpeakingReportFailureKind.notFound) {
          _finishWithRetry(_messageFor(error));
          return;
        }
        _scheduleAutomaticRecovery(
          generation,
          practiceSessionId,
          terminalMessage: _messageFor(error),
        );
        return;
      } on Object {
        if (!_isCurrent(generation, practiceSessionId)) {
          return;
        }
        _failureKind = IeltsSpeakingReportFailureKind.network;
        _scheduleAutomaticRecovery(
          generation,
          practiceSessionId,
          terminalMessage: 'IELTS 练习报告暂时无法加载，请稍后重试。',
        );
        return;
      }
    }
    if (_isCurrent(generation, practiceSessionId)) {
      _failureKind = IeltsSpeakingReportFailureKind.server;
      _scheduleAutomaticRecovery(
        generation,
        practiceSessionId,
        terminalMessage: 'IELTS 练习报告生成时间超过预期，请稍后重试。',
      );
    }
  }

  void _scheduleAutomaticRecovery(
    int generation,
    String practiceSessionId, {
    required String terminalMessage,
  }) {
    if (!_isCurrent(generation, practiceSessionId)) {
      return;
    }
    if (_automaticRecoveryCycle >= maximumAutomaticRecoveryCycles) {
      _finishWithRetry(terminalMessage);
      return;
    }
    _loading = true;
    _canRetry = false;
    _errorMessage = null;
    _envelope = null;
    _automaticRecoveryTimer?.cancel();
    final multiplier = 1 << _automaticRecoveryCycle.clamp(0, 3);
    _automaticRecoveryCycle++;
    _automaticRecoveryTimer = Timer(automaticRecoveryInterval * multiplier, () {
      _automaticRecoveryTimer = null;
      if (!_isCurrent(generation, practiceSessionId)) {
        return;
      }
      unawaited(_load(practiceSessionId, automatic: true));
    });
    notifyListeners();
  }

  void _finishWithRetry(String message) {
    _automaticRecoveryTimer?.cancel();
    _automaticRecoveryTimer = null;
    _envelope = null;
    _loading = false;
    _canRetry = true;
    _errorMessage = message;
    notifyListeners();
  }

  Future<bool> _waitBeforeNextPoll(
    int generation,
    String practiceSessionId,
  ) async {
    await Future<void>.delayed(pollInterval);
    return _isCurrent(generation, practiceSessionId);
  }

  Future<void> retry() async {
    final sessionId = _practiceSessionId;
    if (sessionId == null) {
      return;
    }
    final retryGeneration = _requestGeneration;
    final envelope = _envelope;
    final regenerationClient = client is IeltsSpeakingReportRegenerationClient
        ? client as IeltsSpeakingReportRegenerationClient
        : null;
    if (envelope?.evaluationStatus ==
            IeltsSpeakingReportEvaluationStatus.failed &&
        regenerationClient != null) {
      _loading = true;
      _canRetry = false;
      notifyListeners();
      try {
        await regenerationClient.regenerateReport(envelope!);
      } on IeltsSpeakingReportException catch (error) {
        if (!_isCurrent(retryGeneration, sessionId)) {
          return;
        }
        _loading = false;
        _failureKind = error.kind;
        _canRetry = error.retryable;
        _errorMessage = _messageFor(error);
        notifyListeners();
        return;
      }
      if (!_isCurrent(retryGeneration, sessionId)) {
        return;
      }
    }
    await load(sessionId);
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
    _automaticRecoveryTimer?.cancel();
    _automaticRecoveryTimer = null;
    _automaticRecoveryCycle = 0;
    _automaticRegenerationCount = 0;
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

String _messageFor(IeltsSpeakingReportException error) => switch (error.kind) {
  IeltsSpeakingReportFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
  IeltsSpeakingReportFailureKind.notFound => '这次 IELTS 模考报告尚未生成。',
  IeltsSpeakingReportFailureKind.conflict => '这次 IELTS 模考存在多份结果，暂时无法安全展示。',
  IeltsSpeakingReportFailureKind.invalidRequest ||
  IeltsSpeakingReportFailureKind.invalidResponse => 'IELTS 练习报告响应无法识别，请稍后重试。',
  IeltsSpeakingReportFailureKind.network ||
  IeltsSpeakingReportFailureKind.server => 'IELTS 练习报告暂时无法加载，请稍后重试。',
  IeltsSpeakingReportFailureKind.superseded => '',
};
