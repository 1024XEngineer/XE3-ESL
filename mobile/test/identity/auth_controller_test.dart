import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  const user = User(id: 'user-1', email: 'learner@example.com');
  final loginResult = LoginResult(
    user: user,
    sessionToken: 'sess_new-session-token',
    expiresAt: DateTime.utc(2030),
  );

  group('cold-start session restoration', () {
    test('shows login when no token exists', () async {
      final controller = AuthController(
        identityClient: FakeIdentityClient(),
        sessionStore: FakeSessionStore(),
      );

      await controller.initialize();

      expect(controller.state, isA<AuthSignedOut>());
    });

    test('authenticates when the stored token is valid', () async {
      final client = FakeIdentityClient(currentUserResult: user);
      final store = FakeSessionStore(token: 'sess_stored-token');
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
      );

      await controller.initialize();

      expect((controller.state as AuthAuthenticated).user, same(user));
      expect(client.currentUserTokens, ['sess_stored-token']);
      expect(store.deleteCount, 0);
    });

    test(
      'clears session and private state when current user returns 401',
      () async {
        var cleanupCount = 0;
        final store = FakeSessionStore(token: 'sess_expired-token');
        final controller = AuthController(
          identityClient: FakeIdentityClient(
            currentUserError: const IdentityClientException(
              kind: IdentityFailureKind.authenticationRequired,
              statusCode: 401,
            ),
          ),
          sessionStore: store,
          clearPrivateState: () => cleanupCount++,
        );

        await controller.initialize();

        expect(controller.state, isA<AuthSignedOut>());
        expect(store.token, isNull);
        expect(store.deleteCount, 1);
        expect(cleanupCount, 1);
      },
    );

    test('keeps token on network error and allows retry', () async {
      final client = FakeIdentityClient(
        currentUserError: const IdentityClientException(
          kind: IdentityFailureKind.network,
          retryable: true,
        ),
      );
      final store = FakeSessionStore(token: 'sess_stored-token');
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
      );

      await controller.initialize();

      expect(controller.state, isA<AuthRetryableError>());
      expect(store.token, 'sess_stored-token');
      expect(store.deleteCount, 0);

      client
        ..currentUserError = null
        ..currentUserResult = user;
      await controller.initialize();

      expect(controller.state, isA<AuthAuthenticated>());
    });

    test('clears a stored token without the sess_ marker', () async {
      var cleanupCount = 0;
      final client = FakeIdentityClient(currentUserResult: user);
      final store = FakeSessionStore(token: 'abc123==');
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
        clearPrivateState: () => cleanupCount++,
      );

      await controller.initialize();

      expect(controller.state, isA<AuthSignedOut>());
      expect(store.token, isNull);
      expect(client.currentUserTokens, isEmpty);
      expect(cleanupCount, 1);
    });
  });

  group('registration and login', () {
    test('registration returns to login without creating a session', () async {
      final client = FakeIdentityClient(registerResult: user);
      final store = FakeSessionStore();
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
      )..showRegister();

      await controller.register(
        email: 'learner@example.com',
        password: 'a sufficiently long password',
      );

      final state = controller.state as AuthSignedOut;
      expect(state.form, AuthForm.login);
      expect(state.noticeMessage, contains('Account created'));
      expect(store.writtenTokens, isEmpty);
    });

    test('login writes the raw token once and authenticates', () async {
      final store = FakeSessionStore();
      final controller = AuthController(
        identityClient: FakeIdentityClient(loginResult: loginResult),
        sessionStore: store,
      )..showLogin();

      await controller.login(
        email: 'learner@example.com',
        password: 'a sufficiently long password',
      );

      expect(store.writtenTokens, ['sess_new-session-token']);
      expect(controller.state, isA<AuthAuthenticated>());
    });

    test('invalid credentials stay signed out with a stable message', () async {
      final controller = AuthController(
        identityClient: FakeIdentityClient(
          loginError: const IdentityClientException(
            kind: IdentityFailureKind.invalidCredentials,
          ),
        ),
        sessionStore: FakeSessionStore(),
      )..showLogin();

      await controller.login(
        email: 'learner@example.com',
        password: 'a sufficiently long password',
      );

      final state = controller.state as AuthSignedOut;
      expect(state.errorMessage, 'The email or password is incorrect.');
    });

    test(
      'failed token persistence attempts revocation and stays signed out',
      () async {
        var cleanupCount = 0;
        final store = FakeSessionStore(throwOnWrite: true);
        final client = FakeIdentityClient(loginResult: loginResult);
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
          clearPrivateState: () => cleanupCount++,
        )..showLogin();

        await controller.login(
          email: 'learner@example.com',
          password: 'a sufficiently long password',
        );

        expect(client.logoutTokens, ['sess_new-session-token']);
        expect(controller.state, isA<AuthSignedOut>());
        expect(cleanupCount, 1);
      },
    );
  });

  group('private-state cleanup', () {
    test(
      'manual logout clears local state even when server returns 401',
      () async {
        var cleanupCount = 0;
        final store = FakeSessionStore(token: 'sess_stored-token');
        final client = FakeIdentityClient(
          currentUserResult: user,
          logoutError: const IdentityClientException(
            kind: IdentityFailureKind.authenticationRequired,
            statusCode: 401,
          ),
        );
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
          clearPrivateState: () => cleanupCount++,
        );
        await controller.initialize();

        await controller.logout();

        expect(client.logoutTokens, ['sess_stored-token']);
        expect(store.token, isNull);
        expect(cleanupCount, 1);
        expect(controller.state, isA<AuthSignedOut>());
      },
    );

    test(
      'remote invalidation and account switching use shared cleanup',
      () async {
        var cleanupCount = 0;
        final store = FakeSessionStore(token: 'sess_stored-token');
        final controller = AuthController(
          identityClient: FakeIdentityClient(currentUserResult: user),
          sessionStore: store,
          clearPrivateState: () => cleanupCount++,
        );
        await controller.initialize();

        await controller.invalidateSession();
        store.token = 'sess_replacement-token';
        await controller.switchAccount();

        expect(cleanupCount, 2);
        expect(store.deleteCount, 2);
        expect(controller.state, isA<AuthSignedOut>());
      },
    );

    test(
      'logout network failure preserves token until a retry succeeds',
      () async {
        final store = FakeSessionStore(token: 'sess_stored-token');
        final client = FakeIdentityClient(
          currentUserResult: user,
          logoutError: const IdentityClientException(
            kind: IdentityFailureKind.network,
            retryable: true,
          ),
        );
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
        );
        await controller.initialize();

        await controller.logout();

        final retryState = controller.state as AuthRetryableError;
        expect(retryState.action, AuthRetryAction.logout);
        expect(store.token, 'sess_stored-token');
        expect(store.deleteCount, 0);

        client.logoutError = null;
        await controller.retry();
        expect(store.token, isNull);
        expect(controller.state, isA<AuthSignedOut>());
      },
    );

    test(
      'delete failure still clears private state and can be retried',
      () async {
        var cleanupCount = 0;
        final store = FakeSessionStore(
          token: 'sess_expired-token',
          throwOnDelete: true,
        );
        final controller = AuthController(
          identityClient: FakeIdentityClient(
            currentUserError: const IdentityClientException(
              kind: IdentityFailureKind.authenticationRequired,
            ),
          ),
          sessionStore: store,
          clearPrivateState: () => cleanupCount++,
        );

        await controller.initialize();

        final retryState = controller.state as AuthRetryableError;
        expect(retryState.action, AuthRetryAction.clearSession);
        expect(cleanupCount, 1);

        store.throwOnDelete = false;
        await controller.retry();
        expect(controller.state, isA<AuthSignedOut>());
        expect(cleanupCount, 2);
      },
    );
  });
}

final class FakeSessionStore implements SessionStore {
  FakeSessionStore({
    this.token,
    this.throwOnWrite = false,
    this.throwOnDelete = false,
  });

  String? token;
  bool throwOnWrite;
  bool throwOnDelete;
  final List<String> writtenTokens = [];
  int deleteCount = 0;

  @override
  Future<void> deleteToken() async {
    deleteCount++;
    if (throwOnDelete) {
      throw const SessionStoreException(SessionStoreOperation.delete);
    }
    token = null;
  }

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String value) async {
    if (throwOnWrite) {
      throw const SessionStoreException(SessionStoreOperation.write);
    }
    writtenTokens.add(value);
    token = value;
  }
}

final class FakeIdentityClient implements IdentityClient {
  FakeIdentityClient({
    this.registerResult,
    this.registerError,
    this.loginResult,
    this.loginError,
    this.currentUserResult,
    this.currentUserError,
    this.logoutError,
  });

  User? registerResult;
  IdentityClientException? registerError;
  LoginResult? loginResult;
  IdentityClientException? loginError;
  User? currentUserResult;
  IdentityClientException? currentUserError;
  IdentityClientException? logoutError;
  final List<String> currentUserTokens = [];
  final List<String> logoutTokens = [];

  @override
  Future<User> register({
    required String email,
    required String password,
  }) async {
    final error = registerError;
    if (error != null) {
      throw error;
    }
    return registerResult!;
  }

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    final error = loginError;
    if (error != null) {
      throw error;
    }
    return loginResult!;
  }

  @override
  Future<User> currentUser({required String sessionToken}) async {
    currentUserTokens.add(sessionToken);
    final error = currentUserError;
    if (error != null) {
      throw error;
    }
    return currentUserResult!;
  }

  @override
  Future<void> logout({required String sessionToken}) async {
    logoutTokens.add(sessionToken);
    final error = logoutError;
    if (error != null) {
      throw error;
    }
  }
}
