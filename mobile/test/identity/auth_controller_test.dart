import 'dart:async';

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
      var cleanupCount = 0;
      final store = FakeSessionStore();
      final controller = AuthController(
        identityClient: FakeIdentityClient(),
        sessionStore: store,
        clearPrivateState: () => cleanupCount++,
      );

      await controller.initialize();

      expect(controller.state, isA<AuthSignedOut>());
      expect(store.deleteCount, 1);
      expect(cleanupCount, 1);
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

    test('delayed initialize success cannot restore after logout', () async {
      final currentUser = Completer<User>();
      final store = FakeSessionStore(token: 'sess_stored-token');
      final client = FakeIdentityClient(currentUserFuture: currentUser.future);
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
      );

      final initialize = controller.initialize();
      await _waitFor(() => client.currentUserTokens.isNotEmpty);
      expect(controller.currentCredential, isNull);
      await controller.logout();
      currentUser.complete(user);
      await initialize;

      expect(controller.state, isA<AuthSignedOut>());
      expect(store.token, isNull);
    });

    test(
      '4401 during delayed session revalidation prevents stale success',
      () async {
        final currentUser = Completer<User>();
        final store = FakeSessionStore(token: 'sess_stored-token');
        final client = FakeIdentityClient(currentUserResult: user);
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
        );
        await controller.initialize();
        final socketCredential = controller.currentCredential!;

        client.currentUserFuture = currentUser.future;
        final revalidation = controller.initialize();
        await _waitFor(() => client.currentUserTokens.length == 2);

        await controller.invalidateSession(
          expectedSessionToken: socketCredential.sessionToken,
          expectedGeneration: socketCredential.generation,
        );
        currentUser.complete(user);
        await revalidation;

        expect(controller.state, isA<AuthSignedOut>());
        expect(controller.currentCredential, isNull);
        expect(store.token, isNull);
      },
    );

    test('form navigation cannot bypass delayed session restoration', () async {
      final currentUser = Completer<User>();
      final client = FakeIdentityClient(currentUserFuture: currentUser.future);
      final controller = AuthController(
        identityClient: client,
        sessionStore: FakeSessionStore(token: 'sess_stored-token'),
      );

      final initialize = controller.initialize();
      await _waitFor(() => client.currentUserTokens.isNotEmpty);
      controller.showLogin();
      expect(controller.state, isA<AuthLoading>());
      currentUser.complete(user);
      await initialize;

      expect(controller.state, isA<AuthAuthenticated>());
    });

    test('old session 401 cannot delete a newer login token', () async {
      final oldCurrentUser = Completer<User>();
      final store = FakeSessionStore(token: 'sess_account-a');
      final client = FakeIdentityClient(
        currentUserFuture: oldCurrentUser.future,
        loginResult: loginResult,
      );
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
      );

      final initialize = controller.initialize();
      await _waitFor(() => client.currentUserTokens.isNotEmpty);
      await controller.logout();
      controller.showLogin();
      await controller.login(
        email: 'learner@example.com',
        password: 'a sufficiently long password',
      );

      oldCurrentUser.completeError(
        const IdentityClientException(
          kind: IdentityFailureKind.authenticationRequired,
          statusCode: 401,
        ),
      );
      await initialize;

      expect(controller.state, isA<AuthAuthenticated>());
      expect(store.token, 'sess_new-session-token');
      expect(store.deleteCount, 1);
    });

    test(
      'login is blocked until older private-state cleanup completes',
      () async {
        final cleanupStarted = Completer<void>();
        final allowCleanup = Completer<void>();
        final store = FakeSessionStore();
        final client = FakeIdentityClient(loginResult: loginResult);
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
          clearPrivateState: () async {
            cleanupStarted.complete();
            await allowCleanup.future;
          },
        );

        final initialize = controller.initialize();
        await cleanupStarted.future;
        controller.showLogin();
        controller.showRegister();
        await controller.login(
          email: 'learner@example.com',
          password: 'a sufficiently long password',
        );

        expect(store.writtenTokens, isEmpty);
        expect(client.loginCount, 0);
        expect(controller.state, isA<AuthLoading>());

        allowCleanup.complete();
        await initialize;
        expect(controller.state, isA<AuthSignedOut>());

        controller.showLogin();
        await controller.login(
          email: 'learner@example.com',
          password: 'a sufficiently long password',
        );

        expect(client.loginCount, 1);
        expect(store.token, 'sess_new-session-token');
        expect(controller.state, isA<AuthAuthenticated>());
      },
    );
  });

  group('registration and login', () {
    test('registration returns to login without creating a session', () async {
      final client = FakeIdentityClient(registerResult: user);
      final store = FakeSessionStore();
      final controller = AuthController(
        identityClient: client,
        sessionStore: store,
      );
      await controller.initialize();
      controller.showRegister();

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
      );
      await controller.initialize();
      controller.showLogin();

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
      );
      await controller.initialize();
      controller.showLogin();

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
        );
        await controller.initialize();
        expect(cleanupCount, 1);
        controller.showLogin();

        await controller.login(
          email: 'learner@example.com',
          password: 'a sufficiently long password',
        );

        expect(client.logoutTokens, ['sess_new-session-token']);
        expect(controller.state, isA<AuthSignedOut>());
        expect(cleanupCount, 2);
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
      'logout isolates UI and deletes credentials before cleanup completes',
      () async {
        final cleanupStarted = Completer<void>();
        final allowCleanup = Completer<void>();
        final store = FakeSessionStore(token: 'sess_stored-token');
        final controller = AuthController(
          identityClient: FakeIdentityClient(currentUserResult: user),
          sessionStore: store,
          clearPrivateState: () async {
            cleanupStarted.complete();
            await allowCleanup.future;
          },
        );
        await controller.initialize();

        final logout = controller.logout();

        expect(controller.state, isNot(isA<AuthAuthenticated>()));
        await cleanupStarted.future;
        expect(store.token, isNull);
        expect(controller.state, isNot(isA<AuthAuthenticated>()));

        allowCleanup.complete();
        await logout;
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

        await _invalidateCurrentSession(controller);
        store.token = 'sess_replacement-token';
        await controller.switchAccount();

        expect(cleanupCount, 2);
        expect(store.deleteCount, 2);
        expect(controller.state, isA<AuthSignedOut>());
      },
    );

    test(
      'A logout failure after B login and logout cannot restore state',
      () async {
        final revokeA = Completer<void>();
        final store = FakeSessionStore(token: 'sess_account-a');
        final client = FakeIdentityClient(
          currentUserResult: user,
          loginResult: loginResult,
          logoutFutures: {
            'sess_account-a': revokeA.future,
            'sess_new-session-token': Future<void>.value(),
          },
        );
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
        );
        await controller.initialize();

        await controller.logout();
        controller.showLogin();
        await controller.login(
          email: 'learner@example.com',
          password: 'a sufficiently long password',
        );
        expect(controller.state, isA<AuthAuthenticated>());
        await controller.logout();

        expect(controller.state, isA<AuthSignedOut>());
        expect(store.token, isNull);
        expect(client.logoutTokens, [
          'sess_account-a',
          'sess_new-session-token',
        ]);

        revokeA.completeError(
          const IdentityClientException(
            kind: IdentityFailureKind.network,
            retryable: true,
          ),
        );
        await Future<void>.delayed(Duration.zero);
        expect(controller.state, isA<AuthSignedOut>());
        expect(store.token, isNull);
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

        final failedState = controller.state as AuthRetryableError;
        expect(failedState.action, AuthRetryAction.clearLocalState);
        expect(cleanupCount, 1);

        store.throwOnDelete = false;
        await controller.retry();
        expect(controller.state, isA<AuthSignedOut>());
        expect(store.token, isNull);
        expect(cleanupCount, 2);
      },
    );

    test(
      'failed A cleanup blocks B login until local isolation succeeds',
      () async {
        var cleanupCount = 0;
        final store = FakeSessionStore(token: 'sess_account-a');
        final client = FakeIdentityClient(
          currentUserResult: user,
          loginResult: loginResult,
        );
        final controller = AuthController(
          identityClient: client,
          sessionStore: store,
          clearPrivateState: () {
            cleanupCount++;
            if (cleanupCount == 1) {
              throw StateError('simulated private-state cleanup failure');
            }
          },
        );
        await controller.initialize();

        await controller.logout();

        final failedState = controller.state as AuthRetryableError;
        expect(failedState.action, AuthRetryAction.clearLocalState);
        expect(store.token, isNull);
        expect(cleanupCount, 1);

        controller.showLogin();
        await controller.login(
          email: 'second@example.com',
          password: 'a sufficiently long password',
        );

        expect(controller.state, same(failedState));
        expect(client.loginCount, 0);
        expect(store.token, isNull);

        await controller.retry();
        expect(controller.state, isA<AuthSignedOut>());
        expect(cleanupCount, 2);

        controller.showLogin();
        await controller.login(
          email: 'second@example.com',
          password: 'a sufficiently long password',
        );

        expect(client.loginCount, 1);
        expect(store.token, 'sess_new-session-token');
        expect(controller.state, isA<AuthAuthenticated>());
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
    this.currentUserFuture,
    this.logoutError,
    this.logoutFuture,
    this.logoutFutures = const {},
  });

  User? registerResult;
  IdentityClientException? registerError;
  LoginResult? loginResult;
  IdentityClientException? loginError;
  User? currentUserResult;
  IdentityClientException? currentUserError;
  Future<User>? currentUserFuture;
  IdentityClientException? logoutError;
  Future<void>? logoutFuture;
  Map<String, Future<void>> logoutFutures;
  final List<String> currentUserTokens = [];
  final List<String> logoutTokens = [];
  int loginCount = 0;

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
    loginCount++;
    final error = loginError;
    if (error != null) {
      throw error;
    }
    return loginResult!;
  }

  @override
  Future<User> currentUser({required String sessionToken}) async {
    currentUserTokens.add(sessionToken);
    final future = currentUserFuture;
    if (future != null) {
      return future;
    }
    final error = currentUserError;
    if (error != null) {
      throw error;
    }
    return currentUserResult!;
  }

  @override
  Future<void> logout({required String sessionToken}) async {
    logoutTokens.add(sessionToken);
    final tokenFuture = logoutFutures[sessionToken];
    if (tokenFuture != null) {
      return tokenFuture;
    }
    final future = logoutFuture;
    if (future != null) {
      return future;
    }
    final error = logoutError;
    if (error != null) {
      throw error;
    }
  }
}

Future<void> _waitFor(bool Function() condition) async {
  while (!condition()) {
    await Future<void>.delayed(Duration.zero);
  }
}

Future<void> _invalidateCurrentSession(AuthController controller) async {
  final credential = controller.currentCredential!;
  await controller.invalidateSession(
    expectedSessionToken: credential.sessionToken,
    expectedGeneration: credential.generation,
  );
}
