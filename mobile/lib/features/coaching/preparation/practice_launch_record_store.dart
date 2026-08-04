import 'dart:convert';

import 'package:speakup/identity/session_store.dart';

abstract interface class PracticeLaunchRecordStore {
  Future<String?> read(String accountId);

  Future<void> write(String accountId, String value);

  Future<void> delete(String accountId);
}

final class SecurePracticeLaunchRecordStore
    implements PracticeLaunchRecordStore {
  const SecurePracticeLaunchRecordStore([
    this._storage = const FlutterSecureStorageAdapter(),
  ]);

  final SecureStorageAdapter _storage;

  @override
  Future<String?> read(String accountId) {
    return _storage.read(_key(accountId));
  }

  @override
  Future<void> write(String accountId, String value) {
    return _storage.write(_key(accountId), value);
  }

  @override
  Future<void> delete(String accountId) {
    return _storage.delete(_key(accountId));
  }

  String _key(String accountId) {
    final encoded = base64Url
        .encode(utf8.encode(accountId))
        .replaceAll('=', '');
    return 'practice_launch_record_v1_$encoded';
  }
}

final class MemoryPracticeLaunchRecordStore
    implements PracticeLaunchRecordStore {
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

final class NullPracticeLaunchRecordStore implements PracticeLaunchRecordStore {
  const NullPracticeLaunchRecordStore();

  @override
  Future<String?> read(String accountId) async => null;

  @override
  Future<void> write(String accountId, String value) async {}

  @override
  Future<void> delete(String accountId) async {}
}
