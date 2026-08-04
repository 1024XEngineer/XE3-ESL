import 'dart:convert';

import 'package:speakup/identity/session_store.dart';

abstract interface class IeltsPracticeHistoryStore {
  Future<String?> read(String accountId);

  Future<void> write(String accountId, String value);

  Future<void> delete(String accountId);
}

final class SecureIeltsPracticeHistoryStore
    implements IeltsPracticeHistoryStore {
  const SecureIeltsPracticeHistoryStore([
    this._storage = const FlutterSecureStorageAdapter(),
  ]);

  final SecureStorageAdapter _storage;

  @override
  Future<String?> read(String accountId) => _storage.read(_key(accountId));

  @override
  Future<void> write(String accountId, String value) =>
      _storage.write(_key(accountId), value);

  @override
  Future<void> delete(String accountId) => _storage.delete(_key(accountId));

  String _key(String accountId) {
    final encoded = base64Url
        .encode(utf8.encode(accountId))
        .replaceAll('=', '');
    return 'ielts_practice_history_v1_$encoded';
  }
}

final class MemoryIeltsPracticeHistoryStore
    implements IeltsPracticeHistoryStore {
  final Map<String, String> _values = <String, String>{};

  @override
  Future<String?> read(String accountId) async => _values[accountId];

  @override
  Future<void> write(String accountId, String value) async {
    _values[accountId] = value;
  }

  @override
  Future<void> delete(String accountId) async {
    _values.remove(accountId);
  }
}

final class NullIeltsPracticeHistoryStore implements IeltsPracticeHistoryStore {
  const NullIeltsPracticeHistoryStore();

  @override
  Future<String?> read(String accountId) async => null;

  @override
  Future<void> write(String accountId, String value) async {}

  @override
  Future<void> delete(String accountId) async {}
}
