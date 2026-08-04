import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_client.dart';

final class IeltsSpeakingReportIndexController extends ChangeNotifier {
  IeltsSpeakingReportIndexController({required this.client});

  final IeltsSpeakingReportIndexClient client;

  List<IeltsSpeakingReportIndexItem> _items =
      const <IeltsSpeakingReportIndexItem>[];
  String? _nextCursor;
  String? _errorMessage;
  bool _loading = false;
  bool _disposed = false;
  int _requestGeneration = 0;
  Future<void>? _activeLoad;
  ({bool replace, String? cursor})? _failedRequest;

  List<IeltsSpeakingReportIndexItem> get items =>
      List<IeltsSpeakingReportIndexItem>.unmodifiable(_items);
  String? get errorMessage => _errorMessage;
  bool get isLoading => _loading;
  bool get hasMore => _nextCursor != null;

  Future<void> refresh() => _start(replace: true, cursor: null);

  Future<void> loadMore() {
    final cursor = _nextCursor;
    return cursor == null
        ? Future<void>.value()
        : _start(replace: false, cursor: cursor);
  }

  Future<void> retryLastFailure() {
    final request = _failedRequest;
    return request == null
        ? Future<void>.value()
        : _start(replace: request.replace, cursor: request.cursor);
  }

  Future<void> _start({required bool replace, required String? cursor}) {
    if (_disposed) {
      return Future<void>.value();
    }
    final active = _activeLoad;
    if (active != null) {
      return active;
    }
    final generation = ++_requestGeneration;
    late final Future<void> operation;
    operation = _load(generation: generation, replace: replace, cursor: cursor)
        .whenComplete(() {
          if (identical(_activeLoad, operation)) {
            _activeLoad = null;
          }
        });
    _activeLoad = operation;
    return operation;
  }

  Future<void> _load({
    required int generation,
    required bool replace,
    required String? cursor,
  }) async {
    _loading = true;
    _errorMessage = null;
    _failedRequest = null;
    notifyListeners();
    try {
      final page = await client.listReports(cursor: cursor);
      if (!_isCurrent(generation)) {
        return;
      }
      if (replace) {
        _items = List<IeltsSpeakingReportIndexItem>.unmodifiable(page.items);
      } else {
        final sessionIds = _items.map((item) => item.practiceSessionId).toSet();
        final evaluationIds = _items.map((item) => item.evaluationId).toSet();
        if (page.items.any(
          (item) =>
              !sessionIds.add(item.practiceSessionId) ||
              !evaluationIds.add(item.evaluationId),
        )) {
          throw const IeltsSpeakingReportException(
            kind: IeltsSpeakingReportFailureKind.invalidResponse,
          );
        }
        _items = List<IeltsSpeakingReportIndexItem>.unmodifiable([
          ..._items,
          ...page.items,
        ]);
      }
      _nextCursor = page.nextCursor;
    } on IeltsSpeakingReportException catch (error) {
      if (_isCurrent(generation) &&
          error.kind != IeltsSpeakingReportFailureKind.superseded) {
        _errorMessage = _indexMessageFor(error);
        _failedRequest = (replace: replace, cursor: cursor);
      }
    } on Object {
      if (_isCurrent(generation)) {
        _errorMessage = 'IELTS 模考记录暂时无法加载，请稍后重试。';
        _failedRequest = (replace: replace, cursor: cursor);
      }
    } finally {
      if (_isCurrent(generation)) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> clearPrivateState() async {
    _requestGeneration++;
    _activeLoad = null;
    _items = const <IeltsSpeakingReportIndexItem>[];
    _nextCursor = null;
    _errorMessage = null;
    _failedRequest = null;
    _loading = false;
    await client.clearAccountState();
    if (!_disposed) {
      notifyListeners();
    }
  }

  bool _isCurrent(int generation) =>
      !_disposed && generation == _requestGeneration;

  @override
  void dispose() {
    _disposed = true;
    _requestGeneration++;
    _activeLoad = null;
    super.dispose();
  }
}

String _indexMessageFor(IeltsSpeakingReportException error) =>
    switch (error.kind) {
      IeltsSpeakingReportFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
      IeltsSpeakingReportFailureKind.invalidRequest ||
      IeltsSpeakingReportFailureKind.invalidResponse =>
        'IELTS 模考记录响应无法识别，请稍后重试。',
      IeltsSpeakingReportFailureKind.network ||
      IeltsSpeakingReportFailureKind.server => 'IELTS 模考记录暂时无法加载，请稍后重试。',
      IeltsSpeakingReportFailureKind.notFound ||
      IeltsSpeakingReportFailureKind.conflict => 'IELTS 模考记录暂时无法安全展示。',
      IeltsSpeakingReportFailureKind.superseded => '',
    };
