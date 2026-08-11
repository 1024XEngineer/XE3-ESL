import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';
import 'package:speakup/features/coaching/review/practice_report_status_client.dart';

final class PracticeReportStatusController extends ChangeNotifier {
  PracticeReportStatusController({
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

  final PracticeReportStatusClient client;
  final Duration pollInterval;
  final int maximumPollAttempts;

  String? _practiceSessionId;
  PracticeReportStatus? _status;
  EvaluationReport? _readyReport;
  String? _errorMessage;
  bool _loading = false;
  bool _regenerating = false;
  bool _loadingReadyReport = false;
  bool _retryAllowed = false;
  bool _disposed = false;
  int _generation = 0;

  String? get practiceSessionId => _practiceSessionId;
  PracticeReportStatus? get status => _status;
  EvaluationReport? get readyReport => _readyReport;
  String? get errorMessage => _errorMessage;
  bool get isLoading => _loading;
  bool get isRegenerating => _regenerating;
  bool get isLoadingReadyReport => _loadingReadyReport;
  bool get canRetry => !_loading && _errorMessage != null && _retryAllowed;
  bool get canRegenerate {
    final currentStatus = _status;
    return !_loading &&
        !_regenerating &&
        currentStatus?.evaluationStatus ==
            PracticeReportEvaluationStatus.failed &&
        currentStatus?.stableFailure?.retryable == true &&
        currentStatus?.evaluationId != null &&
        currentStatus?.reportScope == PracticeReportScope.fullMock &&
        client is PracticeReportRegenerationClient;
  }

  Future<void> load(String practiceSessionId) async {
    if (_disposed || practiceSessionId.isEmpty) return;
    final generation = ++_generation;
    _practiceSessionId = practiceSessionId;
    _status = null;
    _readyReport = null;
    _errorMessage = null;
    _loading = true;
    _regenerating = false;
    _loadingReadyReport = false;
    _retryAllowed = false;
    notifyListeners();

    for (var attempt = 0; attempt < maximumPollAttempts; attempt++) {
      try {
        final status = await client.getStatus(practiceSessionId);
        if (!_isCurrent(generation, practiceSessionId)) return;
        _status = status;
        _errorMessage = null;
        final pending =
            status.evaluationStatus == PracticeReportEvaluationStatus.queued ||
            status.evaluationStatus == PracticeReportEvaluationStatus.running;
        if (!pending) {
          _loading = false;
          notifyListeners();
          return;
        }
        notifyListeners();
        if (attempt + 1 < maximumPollAttempts) {
          await Future<void>.delayed(pollInterval);
          if (!_isCurrent(generation, practiceSessionId)) return;
        }
      } on PracticeReportStatusException catch (error) {
        if (!_isCurrent(generation, practiceSessionId)) return;
        if (error.kind == PracticeReportStatusFailureKind.superseded) return;
        if (error.retryable && attempt + 1 < maximumPollAttempts) {
          await Future<void>.delayed(pollInterval);
          if (!_isCurrent(generation, practiceSessionId)) return;
          continue;
        }
        _loading = false;
        _retryAllowed = error.retryable;
        _errorMessage = _messageFor(error);
        notifyListeners();
        return;
      } on Object {
        if (!_isCurrent(generation, practiceSessionId)) return;
        _loading = false;
        _retryAllowed = true;
        _errorMessage = '复盘状态暂时无法加载，请稍后重试。';
        notifyListeners();
        return;
      }
    }
    if (_isCurrent(generation, practiceSessionId)) {
      _loading = false;
      _retryAllowed = true;
      _errorMessage = '报告仍在后台生成，可稍后到复盘页查看。';
      notifyListeners();
    }
  }

  Future<void> retry() {
    final sessionId = _practiceSessionId;
    if (sessionId == null) return Future<void>.value();
    return load(sessionId);
  }

  Future<void> regenerate() async {
    final sessionId = _practiceSessionId;
    final currentStatus = _status;
    if (!canRegenerate ||
        sessionId == null ||
        currentStatus == null ||
        client is! PracticeReportRegenerationClient) {
      return;
    }
    final regenerationClient = client as PracticeReportRegenerationClient;
    final generation = _generation;
    _regenerating = true;
    _errorMessage = null;
    _retryAllowed = false;
    notifyListeners();
    try {
      await regenerationClient.regenerateReport(currentStatus);
      if (!_isCurrent(generation, sessionId)) return;
      _regenerating = false;
      await load(sessionId);
    } on PracticeReportStatusException catch (error) {
      if (!_isCurrent(generation, sessionId) ||
          error.kind == PracticeReportStatusFailureKind.superseded) {
        return;
      }
      _regenerating = false;
      _retryAllowed = error.retryable;
      _errorMessage = _regenerationMessageFor(error);
      notifyListeners();
    } on Object {
      if (!_isCurrent(generation, sessionId)) return;
      _regenerating = false;
      _retryAllowed = true;
      _errorMessage = '报告暂时无法重新生成，请稍后重试。';
      notifyListeners();
    }
  }

  Future<EvaluationReport?> loadReadyReport() async {
    final sessionId = _practiceSessionId;
    final currentStatus = _status;
    final reportRef = currentStatus?.reportRef;
    if (_disposed ||
        sessionId == null ||
        currentStatus == null ||
        currentStatus.evaluationStatus !=
            PracticeReportEvaluationStatus.ready ||
        reportRef == null) {
      return null;
    }
    final cached = _readyReport;
    if (cached != null) return cached;
    final generation = _generation;
    _loadingReadyReport = true;
    _errorMessage = null;
    _retryAllowed = false;
    notifyListeners();
    try {
      final report = await client.getReadyReport(reportRef);
      if (!_isCurrent(generation, sessionId)) return null;
      if (report.practiceSessionId != sessionId ||
          report.evaluationId != currentStatus.evaluationId ||
          report.evaluationRevisionId != currentStatus.evaluationRevisionId ||
          report.revision != currentStatus.revision ||
          report.practiceMode != currentStatus.practiceMode.wireValue ||
          report.scoreability != currentStatus.scoreability ||
          report.detailSchema != currentStatus.detailSchema) {
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.invalidResponse,
        );
      }
      _readyReport = report;
      return report;
    } on PracticeReportStatusException catch (error) {
      if (_isCurrent(generation, sessionId) &&
          error.kind != PracticeReportStatusFailureKind.superseded) {
        _retryAllowed = error.retryable;
        _errorMessage = _readyReportMessageFor(error);
      }
      return null;
    } on Object {
      if (_isCurrent(generation, sessionId)) {
        _retryAllowed = true;
        _errorMessage = '报告暂时无法加载，请稍后重试。';
      }
      return null;
    } finally {
      if (_isCurrent(generation, sessionId)) {
        _loadingReadyReport = false;
        notifyListeners();
      }
    }
  }

  void cancel(String practiceSessionId) {
    if (_practiceSessionId != practiceSessionId) return;
    _generation++;
    _practiceSessionId = null;
    _status = null;
    _readyReport = null;
    _errorMessage = null;
    _loading = false;
    _regenerating = false;
    _loadingReadyReport = false;
    _retryAllowed = false;
    notifyListeners();
  }

  Future<void> clearPrivateState() async {
    _generation++;
    _practiceSessionId = null;
    _status = null;
    _readyReport = null;
    _errorMessage = null;
    _loading = false;
    _regenerating = false;
    _loadingReadyReport = false;
    _retryAllowed = false;
    await client.clearAccountState();
    if (!_disposed) notifyListeners();
  }

  bool _isCurrent(int generation, String practiceSessionId) =>
      !_disposed &&
      generation == _generation &&
      _practiceSessionId == practiceSessionId;

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    super.dispose();
  }
}

String _messageFor(PracticeReportStatusException error) => switch (error.kind) {
  PracticeReportStatusFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
  PracticeReportStatusFailureKind.invalidRequest ||
  PracticeReportStatusFailureKind.invalidResponse => '复盘状态响应无法识别，请稍后重试。',
  PracticeReportStatusFailureKind.notFound => '复盘任务尚未就绪，请稍后重试。',
  PracticeReportStatusFailureKind.conflict => '报告正在处理中，请稍后刷新状态。',
  PracticeReportStatusFailureKind.network ||
  PracticeReportStatusFailureKind.server => '复盘状态暂时无法加载，请稍后重试。',
  PracticeReportStatusFailureKind.superseded => '',
};

String _readyReportMessageFor(PracticeReportStatusException error) =>
    switch (error.kind) {
      PracticeReportStatusFailureKind.authenticationRequired =>
        '登录状态已失效，请重新登录。',
      PracticeReportStatusFailureKind.invalidRequest ||
      PracticeReportStatusFailureKind.invalidResponse => '报告内容暂时无法识别，请稍后重试。',
      PracticeReportStatusFailureKind.notFound => '报告文件尚未就绪，请稍后重试。',
      PracticeReportStatusFailureKind.conflict => '报告正在处理中，请稍后重试。',
      PracticeReportStatusFailureKind.network ||
      PracticeReportStatusFailureKind.server => '报告暂时无法加载，请稍后重试。',
      PracticeReportStatusFailureKind.superseded => '',
    };

String _regenerationMessageFor(PracticeReportStatusException error) =>
    switch (error.kind) {
      PracticeReportStatusFailureKind.authenticationRequired =>
        '登录状态已失效，请重新登录。',
      PracticeReportStatusFailureKind.invalidRequest => '当前报告无法重新生成，请返回训练后重试。',
      PracticeReportStatusFailureKind.notFound => '原报告已不存在，无法重新生成。',
      PracticeReportStatusFailureKind.conflict => '报告正在处理中，请稍后刷新状态。',
      PracticeReportStatusFailureKind.network ||
      PracticeReportStatusFailureKind.server => '重新生成请求未完成，请检查网络后重试。',
      PracticeReportStatusFailureKind.invalidResponse => '重新生成响应无法识别，请稍后重试。',
      PracticeReportStatusFailureKind.superseded => '',
    };
