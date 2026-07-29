import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/preparation/practice_launch_record_store.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  test('secure records stay isolated by encoded account key', () async {
    final adapter = _MemorySecureStorageAdapter();
    final store = SecurePracticeLaunchRecordStore(adapter);

    await store.write('user-1', 'first record');
    await store.write('user:2', 'second record');

    expect(await store.read('user-1'), 'first record');
    expect(await store.read('user:2'), 'second record');
    expect(adapter.values, hasLength(2));
    expect(adapter.values.keys.toSet(), hasLength(2));
    expect(
      adapter.values.keys,
      everyElement(startsWith('practice_launch_record_v1_')),
    );
    expect(adapter.values.keys.join(' '), isNot(contains('user-1')));
    expect(adapter.values.keys.join(' '), isNot(contains('=')));
  });

  test('secure delete removes only the selected account record', () async {
    final adapter = _MemorySecureStorageAdapter();
    final store = SecurePracticeLaunchRecordStore(adapter);
    await store.write('user-1', 'first');
    await store.write('user-2', 'second');

    await store.delete('user-1');

    expect(await store.read('user-1'), isNull);
    expect(await store.read('user-2'), 'second');
  });

  test('memory store supports account-scoped read write and delete', () async {
    final store = MemoryPracticeLaunchRecordStore();

    await store.write('user-1', 'first');
    await store.write('user-2', 'second');
    await store.write('user-1', 'replacement');

    expect(await store.read('user-1'), 'replacement');
    expect(await store.read('user-2'), 'second');

    await store.delete('user-1');

    expect(await store.read('user-1'), isNull);
    expect(await store.read('user-2'), 'second');
  });

  test('null store ignores writes and deletes', () async {
    const store = NullPracticeLaunchRecordStore();

    await store.write('user-1', 'record');
    expect(await store.read('user-1'), isNull);

    await store.delete('user-1');
    expect(await store.read('user-1'), isNull);
  });
}

final class _MemorySecureStorageAdapter implements SecureStorageAdapter {
  final Map<String, String> values = <String, String>{};

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String value) async {
    values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    values.remove(key);
  }
}
