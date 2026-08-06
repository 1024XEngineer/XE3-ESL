import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:speakup/identity/model/identity_models.dart';

/// Persists the single opaque token that represents the current session.
abstract interface class SessionStore {
  Future<String?> readToken();

  Future<void> writeToken(String token);

  Future<void> deleteToken();
}

/// The narrow storage boundary used to test session persistence without a
/// platform Keychain.
abstract interface class SecureStorageAdapter {
  Future<String?> read(String key);

  Future<void> write(String key, String value);

  Future<void> delete(String key);
}

/// Stores the session token in the iOS Keychain.
///
/// The service and account names are intentionally fixed. Keychain syncing is
/// disabled so a session remains bound to the device where it was created.
final class IosKeychainSessionStore implements SessionStore {
  const IosKeychainSessionStore([
    this._adapter = const FlutterSecureStorageAdapter(),
  ]);

  static const _tokenAccount = 'session_token';

  final SecureStorageAdapter _adapter;

  @override
  Future<String?> readToken() async {
    try {
      return await _adapter.read(_tokenAccount);
    } on Object {
      throw const SessionStoreException(SessionStoreOperation.read);
    }
  }

  @override
  Future<void> writeToken(String token) async {
    if (!isValidOpaqueSessionToken(token)) {
      throw const SessionStoreException(SessionStoreOperation.write);
    }
    try {
      await _adapter.write(_tokenAccount, token);
    } on Object {
      // Do not retain the original exception: platform failures may include
      // method arguments, which contain the raw token for writes.
      throw const SessionStoreException(SessionStoreOperation.write);
    }
  }

  @override
  Future<void> deleteToken() async {
    try {
      await _adapter.delete(_tokenAccount);
    } on Object {
      throw const SessionStoreException(SessionStoreOperation.delete);
    }
  }
}

/// Production adapter for [FlutterSecureStorage].
final class FlutterSecureStorageAdapter implements SecureStorageAdapter {
  const FlutterSecureStorageAdapter([
    this._storage = const FlutterSecureStorage(),
  ]);

  static const _iosOptions = IOSOptions(
    accountName: 'com.xe3-esl.speakup.identity',
    synchronizable: false,
    accessibility: KeychainAccessibility.first_unlock_this_device,
  );

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) {
    return _storage.read(key: key, iOptions: _iosOptions);
  }

  @override
  Future<void> write(String key, String value) {
    return _storage.write(key: key, value: value, iOptions: _iosOptions);
  }

  @override
  Future<void> delete(String key) {
    return _storage.delete(key: key, iOptions: _iosOptions);
  }
}

enum SessionStoreOperation { read, write, delete }

/// A deliberately redacted persistence error suitable for user-facing state.
final class SessionStoreException implements Exception {
  const SessionStoreException(this.operation);

  final SessionStoreOperation operation;

  @override
  String toString() => 'Unable to ${operation.name} session token.';
}
