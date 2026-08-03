import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';

final class IeltsSpeakingReportController extends ChangeNotifier {
  IeltsSpeakingReportController({
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

  final IeltsSpeakingReportClient client;
  final Duration pollInterval;
  final int maximumPollAttempts;

  String? _practiceSessionId;
  IeltsSpeakingReportEnvelope? _envelope;
  String? _errorMessage;
  IeltsSpeakingReportFailureKind? _failureKind;
  bool _loading = false;
  bool _canRetry = false;
  bool _disposed = false;
  int _requestGeneration = 0;

  String? get practiceSessionId => _practiceSessionId;
  IeltsSpeakingReportEnvelope? get envelope => _envelope;
  String? get errorMessage => _errorMessage;
  IeltsSpeakingReportFailureKind? get failureKind => _failureKind;
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
                IeltsSpeakingReportEvaluationStatus.queued ||
            envelope.evaluationStatus ==
                IeltsSpeakingReportEvaluationStatus.running;
        if (!pending) {
          _loading = false;
          _canRetry =
              envelope.evaluationStatus ==
                  IeltsSpeakingReportEvaluationStatus.failed &&
              ((envelope.stableFailure?.retryable ?? false) ||
                  client is IeltsSpeakingReportRegenerationClient);
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
      } on IeltsSpeakingReportException catch (error) {
        if (!_isCurrent(generation, practiceSessionId) ||
            error.kind == IeltsSpeakingReportFailureKind.superseded) {
          return;
        }
        _loading = false;
        _failureKind = error.kind;
        _canRetry =
            error.retryable ||
            error.kind == IeltsSpeakingReportFailureKind.notFound;
        _errorMessage = _messageFor(error);
        notifyListeners();
        return;
      } on Object {
        if (!_isCurrent(generation, practiceSessionId)) {
          return;
        }
        _loading = false;
        _failureKind = IeltsSpeakingReportFailureKind.network;
        _canRetry = true;
        _errorMessage = 'IELTS 练习报告暂时无法加载，请稍后重试。';
        notifyListeners();
        return;
      }
    }
    if (_isCurrent(generation, practiceSessionId)) {
      _loading = false;
      _failureKind = IeltsSpeakingReportFailureKind.network;
      _canRetry = true;
      _errorMessage = '报告仍在生成，请稍后重试。';
      notifyListeners();
    }
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
