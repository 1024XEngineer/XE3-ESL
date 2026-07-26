import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  group('IosKeychainSessionStore', () {
    test('reads a previously written token', () async {
      final adapter = _MemorySecureStorageAdapter();
      final store = IosKeychainSessionStore(adapter);

      await store.writeToken('sess_first-session');

      expect(await store.readToken(), 'sess_first-session');
      expect(adapter.lastKey, 'session_token');
    });

    test('overwrites the current token', () async {
      final adapter = _MemorySecureStorageAdapter();
      final store = IosKeychainSessionStore(adapter);

      await store.writeToken('sess_first-session');
      await store.writeToken('sess_replacement-session');

      expect(await store.readToken(), 'sess_replacement-session');
    });

    test('deletes the current token', () async {
      final adapter = _MemorySecureStorageAdapter();
      final store = IosKeychainSessionStore(adapter);
      await store.writeToken('sess_current-session');

      await store.deleteToken();

      expect(await store.readToken(), isNull);
    });

    test('rejects a value without the sess_ credential marker', () async {
      final adapter = _MemorySecureStorageAdapter();
      final store = IosKeychainSessionStore(adapter);

      await expectLater(
        store.writeToken('abc123=='),
        throwsA(isA<SessionStoreException>()),
      );

      expect(await store.readToken(), isNull);
    });

    test('uses fixed non-synchronizing iOS Keychain configuration', () async {
      final storage = _RecordingFlutterSecureStorage();
      final store = IosKeychainSessionStore(
        FlutterSecureStorageAdapter(storage),
      );

      await store.writeToken('sess_current-session');
      await store.readToken();
      await store.deleteToken();

      expect(storage.keys, everyElement('session_token'));
      expect(storage.iosOptions, hasLength(3));
      for (final options in storage.iosOptions) {
        expect(options.accountName, 'com.xe3-esl.speakup.identity');
        expect(options.synchronizable, isFalse);
        expect(
          options.accessibility,
          KeychainAccessibility.first_unlock_this_device,
        );
      }
    });

    for (final operation in SessionStoreOperation.values) {
      test('${operation.name} failures do not expose the token', () async {
        const token = 'sess_secret-session-token';
        final adapter = _ThrowingSecureStorageAdapter(token);
        final store = IosKeychainSessionStore(adapter);

        final invocation = switch (operation) {
          SessionStoreOperation.read => store.readToken,
          SessionStoreOperation.write => () => store.writeToken(token),
          SessionStoreOperation.delete => store.deleteToken,
        };

        Object? error;
        try {
          await invocation();
        } on Object catch (caught) {
          error = caught;
        }

        expect(error, isA<SessionStoreException>());
        expect(error.toString(), isNot(contains(token)));
      });
    }
  });
}

final class _RecordingFlutterSecureStorage extends FlutterSecureStorage {
  final List<String> keys = [];
  final List<AppleOptions> iosOptions = [];
  String? _value;

  void _record(String key, AppleOptions? options) {
    keys.add(key);
    iosOptions.add(options!);
  }

  @override
  Future<String?> read({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    _record(key, iOptions);
    return _value;
  }

  @override
  Future<void> write({
    required String key,
    required String? value,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    _record(key, iOptions);
    _value = value;
  }

  @override
  Future<void> delete({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    _record(key, iOptions);
    _value = null;
  }
}

final class _MemorySecureStorageAdapter implements SecureStorageAdapter {
  final Map<String, String> _values = {};
  String? lastKey;

  @override
  Future<String?> read(String key) async {
    lastKey = key;
    return _values[key];
  }

  @override
  Future<void> write(String key, String value) async {
    lastKey = key;
    _values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    lastKey = key;
    _values.remove(key);
  }
}

final class _ThrowingSecureStorageAdapter implements SecureStorageAdapter {
  const _ThrowingSecureStorageAdapter(this.token);

  final String token;

  Never _throw() => throw StateError('platform failure with $token');

  @override
  Future<String?> read(String key) async => _throw();

  @override
  Future<void> write(String key, String value) async => _throw();

  @override
  Future<void> delete(String key) async => _throw();
}
