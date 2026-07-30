import 'package:flutter/foundation.dart';
import 'package:speakup/review/turn_feedback.dart';
import 'package:speakup/review/turn_feedback_client.dart';

final class SpeechFeedbackProjection {
  const SpeechFeedbackProjection({
    required this.sourceKey,
    required this.statusUrl,
    required this.isPolling,
    required this.canRetry,
    this.feedback,
    this.failureKind,
    this.errorMessage,
  });

  final String sourceKey;
  final String statusUrl;
  final SpeechFeedback? feedback;
  final SpeechFeedbackFailureKind? failureKind;
  final String? errorMessage;
  final bool isPolling;
  final bool canRetry;
}

final class SpeechFeedbackController extends ChangeNotifier {
  SpeechFeedbackController({
    required this.client,
    this.pollInterval = const Duration(seconds: 2),
    this.maximumPollAttempts = 8,
  }) {
    if (pollInterval < Duration.zero) {
      throw ArgumentError.value(pollInterval, 'pollInterval');
    }
    if (maximumPollAttempts < 1) {
      throw ArgumentError.value(maximumPollAttempts, 'maximumPollAttempts');
    }
  }

  final SpeechFeedbackClient client;
  final Duration pollInterval;
  final int maximumPollAttempts;

  final Map<String, SpeechFeedbackProjection> _projections = {};
  final Map<String, int> _sourceGenerations = {};
  int _accountGeneration = 0;
  int _nextSourceGeneration = 0;
  bool _disposed = false;

  Map<String, SpeechFeedbackProjection> get projections =>
      Map<String, SpeechFeedbackProjection>.unmodifiable(_projections);

  SpeechFeedbackProjection? projectionFor(String sourceKey) =>
      _projections[sourceKey];

  Future<void> load({
    required String sourceKey,
    required String statusUrl,
  }) async {
    if (_disposed) {
      return;
    }
    if (sourceKey.isEmpty || !validSpeechFeedbackStatusUrl(statusUrl)) {
      throw ArgumentError('sourceKey or statusUrl is invalid.');
    }
    final accountGeneration = _accountGeneration;
    final sourceGeneration = ++_nextSourceGeneration;
    _sourceGenerations[sourceKey] = sourceGeneration;
    _projections[sourceKey] = SpeechFeedbackProjection(
      sourceKey: sourceKey,
      statusUrl: statusUrl,
      isPolling: true,
      canRetry: false,
    );
    notifyListeners();

    for (var attempt = 0; attempt < maximumPollAttempts; attempt++) {
      try {
        final feedback = await client.getFeedback(statusUrl);
        if (!_isCurrent(
          accountGeneration: accountGeneration,
          sourceKey: sourceKey,
          sourceGeneration: sourceGeneration,
          statusUrl: statusUrl,
        )) {
          return;
        }
        if (!feedback.isPending) {
          _projections[sourceKey] = SpeechFeedbackProjection(
            sourceKey: sourceKey,
            statusUrl: statusUrl,
            feedback: feedback,
            isPolling: false,
            canRetry:
                feedback.feedbackStatus == SpeechFeedbackStatus.failed &&
                (feedback.stableFailure?.retryable ?? false),
          );
          notifyListeners();
          return;
        }
        _projections[sourceKey] = SpeechFeedbackProjection(
          sourceKey: sourceKey,
          statusUrl: statusUrl,
          feedback: feedback,
          isPolling: true,
          canRetry: false,
        );
        notifyListeners();
        if (attempt + 1 < maximumPollAttempts) {
          await Future<void>.delayed(pollInterval);
          if (!_isCurrent(
            accountGeneration: accountGeneration,
            sourceKey: sourceKey,
            sourceGeneration: sourceGeneration,
            statusUrl: statusUrl,
          )) {
            return;
          }
        }
      } on SpeechFeedbackException catch (error) {
        if (!_isCurrent(
              accountGeneration: accountGeneration,
              sourceKey: sourceKey,
              sourceGeneration: sourceGeneration,
              statusUrl: statusUrl,
            ) ||
            error.kind == SpeechFeedbackFailureKind.superseded) {
          return;
        }
        _projections[sourceKey] = SpeechFeedbackProjection(
          sourceKey: sourceKey,
          statusUrl: statusUrl,
          isPolling: false,
          canRetry: error.retryable,
          failureKind: error.kind,
          errorMessage: _messageFor(error),
        );
        notifyListeners();
        return;
      } on Object {
        if (!_isCurrent(
          accountGeneration: accountGeneration,
          sourceKey: sourceKey,
          sourceGeneration: sourceGeneration,
          statusUrl: statusUrl,
        )) {
          return;
        }
        _projections[sourceKey] = SpeechFeedbackProjection(
          sourceKey: sourceKey,
          statusUrl: statusUrl,
          isPolling: false,
          canRetry: true,
          failureKind: SpeechFeedbackFailureKind.network,
          errorMessage: '口语反馈暂时无法加载，请稍后重试。',
        );
        notifyListeners();
        return;
      }
    }

    if (_isCurrent(
      accountGeneration: accountGeneration,
      sourceKey: sourceKey,
      sourceGeneration: sourceGeneration,
      statusUrl: statusUrl,
    )) {
      final pending = _projections[sourceKey]?.feedback;
      _projections[sourceKey] = SpeechFeedbackProjection(
        sourceKey: sourceKey,
        statusUrl: statusUrl,
        feedback: pending,
        isPolling: false,
        canRetry: true,
        failureKind: SpeechFeedbackFailureKind.network,
        errorMessage: '反馈仍在生成，请稍后重试。',
      );
      notifyListeners();
    }
  }

  Future<void> retry(String sourceKey) {
    final projection = _projections[sourceKey];
    return projection == null
        ? Future<void>.value()
        : load(sourceKey: sourceKey, statusUrl: projection.statusUrl);
  }

  void removeSource(String sourceKey) {
    if (_disposed || !_projections.containsKey(sourceKey)) {
      return;
    }
    _sourceGenerations.remove(sourceKey);
    _projections.remove(sourceKey);
    notifyListeners();
  }

  Future<void> clearPrivateState() async {
    _accountGeneration++;
    _projections.clear();
    _sourceGenerations.clear();
    await client.clearAccountState();
    if (!_disposed) {
      notifyListeners();
    }
  }

  bool _isCurrent({
    required int accountGeneration,
    required String sourceKey,
    required int sourceGeneration,
    required String statusUrl,
  }) {
    return !_disposed &&
        accountGeneration == _accountGeneration &&
        sourceGeneration == _sourceGenerations[sourceKey] &&
        statusUrl == _projections[sourceKey]?.statusUrl;
  }

  @override
  void dispose() {
    _disposed = true;
    _accountGeneration++;
    _projections.clear();
    _sourceGenerations.clear();
    super.dispose();
  }
}

String _messageFor(SpeechFeedbackException error) => switch (error.kind) {
  SpeechFeedbackFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
  SpeechFeedbackFailureKind.notFound => '这条语音反馈已不可用。',
  SpeechFeedbackFailureKind.conflict => '语音来源已经变化，无法安全展示这条反馈。',
  SpeechFeedbackFailureKind.invalidRequest ||
  SpeechFeedbackFailureKind.invalidResponse => '口语反馈响应无法识别，请稍后重试。',
  SpeechFeedbackFailureKind.network ||
  SpeechFeedbackFailureKind.server => '口语反馈暂时无法加载，请稍后重试。',
  SpeechFeedbackFailureKind.superseded => '',
};
