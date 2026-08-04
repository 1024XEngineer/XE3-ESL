import 'package:flutter/foundation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_client.dart';

final class IeltsSpeakingReportIndexController extends ChangeNotifier {
  IeltsSpeakingReportIndexController({required this.client});

  final IeltsSpeakingReportIndexClient client;
  List<IeltsSpeakingReportIndexItem> _items = const [];
  String? _nextCursor;
  String? _errorMessage;
  bool _loading = false;
  bool _disposed = false;
  int _generation = 0;
  Future<void>? _activeLoad;

  List<IeltsSpeakingReportIndexItem> get items => List.unmodifiable(_items);
  String? get errorMessage => _errorMessage;
  bool get isLoading => _loading;
  bool get hasMore => _nextCursor != null;

  Future<void> refresh() => _load(replace: true, cursor: null);

  Future<void> loadMore() => _nextCursor == null
      ? Future<void>.value()
      : _load(replace: false, cursor: _nextCursor);

  Future<void> retryLastFailure() => refresh();

  Future<void> _load({required bool replace, required String? cursor}) {
    if (_disposed) return Future<void>.value();
    if (_activeLoad case final active?) return active;
    final generation = ++_generation;
    late final Future<void> operation;
    operation =
        _performLoad(
          generation: generation,
          replace: replace,
          cursor: cursor,
        ).whenComplete(() {
          if (identical(_activeLoad, operation)) _activeLoad = null;
        });
    _activeLoad = operation;
    return operation;
  }

  Future<void> _performLoad({
    required int generation,
    required bool replace,
    required String? cursor,
  }) async {
    _loading = true;
    _errorMessage = null;
    notifyListeners();
    try {
      final page = await client.listReports(cursor: cursor);
      if (!_current(generation)) return;
      if (replace) {
        _items = List.unmodifiable(page.items);
      } else {
        final sessions = _items.map((item) => item.practiceSessionId).toSet();
        if (page.items.any((item) => !sessions.add(item.practiceSessionId))) {
          throw const IeltsSpeakingReportException(
            kind: IeltsSpeakingReportFailureKind.invalidResponse,
          );
        }
        _items = List.unmodifiable([..._items, ...page.items]);
      }
      _nextCursor = page.nextCursor;
    } on IeltsSpeakingReportException catch (error) {
      if (_current(generation) &&
          error.kind != IeltsSpeakingReportFailureKind.superseded) {
        _errorMessage = '报告记录暂时无法加载，请稍后重试。';
      }
    } on Object {
      if (_current(generation)) _errorMessage = '报告记录暂时无法加载，请稍后重试。';
    } finally {
      if (_current(generation)) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> clearPrivateState() async {
    _generation++;
    _activeLoad = null;
    _items = const [];
    _nextCursor = null;
    _errorMessage = null;
    _loading = false;
    await client.clearAccountState();
    if (!_disposed) notifyListeners();
  }

  bool _current(int generation) => !_disposed && generation == _generation;

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    _activeLoad = null;
    super.dispose();
  }
}
