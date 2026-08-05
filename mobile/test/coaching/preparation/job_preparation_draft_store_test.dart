import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/interview/job_preparation_draft_store.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  test('secure draft keys and values stay isolated by account', () async {
    final adapter = _MemorySecureStorageAdapter();
    final store = SecureJobPreparationDraftStore(adapter);

    await store.write('user-1', '{"draft":1}');
    await store.write('user:2', '{"draft":2}');

    expect(await store.read('user-1'), '{"draft":1}');
    expect(await store.read('user:2'), '{"draft":2}');
    expect(adapter.values, hasLength(2));
    expect(adapter.values.keys.toSet(), hasLength(2));
    expect(
      adapter.values.keys,
      everyElement(startsWith('job_preparation_draft_v1_')),
    );
    expect(adapter.values.keys.join(' '), isNot(contains('user-1')));
  });

  test('deleting one account never removes another account draft', () async {
    final adapter = _MemorySecureStorageAdapter();
    final store = SecureJobPreparationDraftStore(adapter);
    await store.write('user-1', 'first');
    await store.write('user-2', 'second');

    await store.delete('user-1');

    expect(await store.read('user-1'), isNull);
    expect(await store.read('user-2'), 'second');
  });

  test(
    'memory store supports deterministic restart and logout tests',
    () async {
      final store = MemoryJobPreparationDraftStore();

      await store.write('user-1', 'draft');
      expect(await store.read('user-1'), 'draft');
      expect(await store.read('user-2'), isNull);

      await store.delete('user-1');
      expect(await store.read('user-1'), isNull);
    },
  );
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
