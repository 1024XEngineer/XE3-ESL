import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/review/review_history_client.dart';

final class ReviewHistoryController extends ChangeNotifier {
  ReviewHistoryController({required this.client});

  final ReviewHistoryClient client;

  List<ReviewHistoryItem> _items = const <ReviewHistoryItem>[];
  String? _nextCursor;
  String? _errorMessage;
  bool _loading = false;
  bool _disposed = false;
  int _accountEpoch = 0;
  _ReviewHistoryLoadCycle? _loadCycle;
  ({bool replace, String? cursor})? _failedRequest;

  List<ReviewHistoryItem> get items =>
      List<ReviewHistoryItem>.unmodifiable(_items);
  String? get errorMessage => _errorMessage;
  bool get isLoading => _loading;
  bool get hasMore => _nextCursor != null;

  Future<void> refresh() => _schedule(replace: true, cursor: null);

  Future<void> loadMore() {
    final cursor = _nextCursor;
    if (cursor == null) {
      return Future<void>.value();
    }
    return _schedule(replace: false, cursor: cursor);
  }

  Future<void> retryLastFailure() {
    final request = _failedRequest;
    if (request == null) {
      return Future<void>.value();
    }
    return _schedule(replace: request.replace, cursor: request.cursor);
  }

  Future<void> _schedule({required bool replace, required String? cursor}) {
    if (_disposed) {
      return Future<void>.value();
    }
    final epoch = _accountEpoch;
    final current = _loadCycle;
    if (current != null && current.epoch == epoch) {
      if (!replace) {
        // Pagination is single-flight. A second tap never creates a duplicate
        // request for the same cursor or overtakes an active refresh.
        return current.active.done;
      }
      final pending = current.pendingRefresh;
      if (pending != null) {
        return pending.done;
      }
      final intent = _ReviewHistoryLoadIntent(
        epoch: epoch,
        replace: true,
        cursor: null,
      );
      current.pendingRefresh = intent;
      return intent.done;
    }

    final intent = _ReviewHistoryLoadIntent(
      epoch: epoch,
      replace: replace,
      cursor: cursor,
    );
    final cycle = _ReviewHistoryLoadCycle(epoch: epoch, active: intent);
    _loadCycle = cycle;
    unawaited(_drain(cycle));
    return intent.done;
  }

  Future<void> _drain(_ReviewHistoryLoadCycle cycle) async {
    try {
      while (true) {
        final intent = cycle.active;
        if (_isCurrent(intent.epoch)) {
          await _performLoad(
            epoch: intent.epoch,
            replace: intent.replace,
            cursor: intent.cursor,
          );
        }
        intent.complete();

        if (!_isCurrent(cycle.epoch)) {
          cycle.cancelPending();
          return;
        }
        final next = cycle.pendingRefresh;
        cycle.pendingRefresh = null;
        if (next == null) {
          return;
        }
        cycle.active = next;
      }
    } finally {
      cycle.active.complete();
      cycle.cancelPending();
      // Account cleanup detaches the old cycle so its late completion cannot
      // clear a new account's active scheduler.
      if (identical(_loadCycle, cycle)) {
        _loadCycle = null;
      }
    }
  }

  Future<void> _performLoad({
    required int epoch,
    required bool replace,
    required String? cursor,
  }) async {
    _loading = true;
    _errorMessage = null;
    _failedRequest = null;
    notifyListeners();
    if (!_isCurrent(epoch)) {
      return;
    }
    try {
      final page = await client.list(cursor: cursor);
      if (!_isCurrent(epoch)) {
        return;
      }
      if (replace) {
        _items = List<ReviewHistoryItem>.unmodifiable(page.items);
      } else {
        final known = _items.map((item) => item.review.id).toSet();
        if (page.items.any((item) => !known.add(item.review.id))) {
          throw const ReviewHistoryException(
            kind: ReviewHistoryFailureKind.invalidResponse,
          );
        }
        _items = List<ReviewHistoryItem>.unmodifiable([
          ..._items,
          ...page.items,
        ]);
      }
      _nextCursor = page.nextCursor;
    } on ReviewHistoryException catch (error) {
      if (_isCurrent(epoch)) {
        _errorMessage = _messageFor(error);
        _failedRequest = (replace: replace, cursor: cursor);
      }
    } on Object {
      if (_isCurrent(epoch)) {
        _errorMessage = '复盘记录暂时无法加载，请稍后重试。';
        _failedRequest = (replace: replace, cursor: cursor);
      }
    } finally {
      if (_isCurrent(epoch)) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> clearPrivateState() async {
    _accountEpoch++;
    final detachedCycle = _loadCycle;
    _loadCycle = null;
    detachedCycle?.cancelAllWaiters();
    _items = const <ReviewHistoryItem>[];
    _nextCursor = null;
    _errorMessage = null;
    _failedRequest = null;
    _loading = false;
    await client.clearAccountState();
    if (!_disposed) {
      notifyListeners();
    }
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _accountEpoch;

  @override
  void dispose() {
    _disposed = true;
    _accountEpoch++;
    final detachedCycle = _loadCycle;
    _loadCycle = null;
    detachedCycle?.cancelAllWaiters();
    super.dispose();
  }
}

final class _ReviewHistoryLoadIntent {
  _ReviewHistoryLoadIntent({
    required this.epoch,
    required this.replace,
    required this.cursor,
  });

  final int epoch;
  final bool replace;
  final String? cursor;
  final Completer<void> _completion = Completer<void>();

  Future<void> get done => _completion.future;

  void complete() {
    if (!_completion.isCompleted) {
      _completion.complete();
    }
  }
}

final class _ReviewHistoryLoadCycle {
  _ReviewHistoryLoadCycle({required this.epoch, required this.active});

  final int epoch;
  _ReviewHistoryLoadIntent active;
  _ReviewHistoryLoadIntent? pendingRefresh;

  void cancelPending() {
    pendingRefresh?.complete();
    pendingRefresh = null;
  }

  void cancelAllWaiters() {
    active.complete();
    cancelPending();
  }
}

String _messageFor(ReviewHistoryException error) {
  return switch (error.kind) {
    ReviewHistoryFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
    ReviewHistoryFailureKind.invalidRequest ||
    ReviewHistoryFailureKind.invalidResponse => '复盘记录响应无法识别，请稍后重试。',
    ReviewHistoryFailureKind.network ||
    ReviewHistoryFailureKind.server => '复盘记录暂时无法加载，请稍后重试。',
    ReviewHistoryFailureKind.superseded => '',
  };
}
