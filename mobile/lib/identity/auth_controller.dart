import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';

typedef PrivateStateCleanup = FutureOr<void> Function();

final class AuthController extends ChangeNotifier {
  AuthController({
    required this.identityClient,
    required this.sessionStore,
    PrivateStateCleanup? clearPrivateState,
  }) : _clearPrivateState = clearPrivateState ?? _noCleanup;

  final IdentityClient identityClient;
  final SessionStore sessionStore;
  final PrivateStateCleanup _clearPrivateState;

  AuthState _state = const AuthLoading();
  String? _sessionToken;
  bool _initializing = false;

  AuthState get state => _state;

  Future<void> initialize() async {
    if (_initializing) {
      return;
    }
    _initializing = true;
    _setState(const AuthLoading());

    try {
      final token = await sessionStore.readToken();
      if (token == null || token.isEmpty) {
        _sessionToken = null;
        _setState(const AuthSignedOut());
        return;
      }
      if (!isValidOpaqueSessionToken(token)) {
        await _finishSessionCleanup();
        return;
      }

      _sessionToken = token;
      final user = await identityClient.currentUser(sessionToken: token);
      _setState(AuthAuthenticated(user));
    } on IdentityClientException catch (error) {
      if (error.isAuthenticationFailure) {
        await _finishSessionCleanup();
      } else {
        _setState(const AuthRetryableError());
      }
    } catch (_) {
      _setState(const AuthRetryableError());
    } finally {
      _initializing = false;
    }
  }

  void showLogin() {
    _setState(const AuthSignedOut(form: AuthForm.login));
  }

  void showRegister() {
    _setState(const AuthSignedOut(form: AuthForm.register));
  }

  Future<void> register({
    required String email,
    required String password,
  }) async {
    if (!_beginSubmission(AuthForm.register)) {
      return;
    }

    try {
      await identityClient.register(email: email, password: password);
      _setState(
        const AuthSignedOut(
          noticeMessage: 'Account created. Sign in to continue.',
        ),
      );
    } on IdentityClientException catch (error) {
      _finishSubmission(AuthForm.register, _registrationMessage(error));
    } catch (_) {
      _finishSubmission(AuthForm.register, _tryAgainMessage);
    }
  }

  Future<void> login({required String email, required String password}) async {
    if (!_beginSubmission(AuthForm.login)) {
      return;
    }

    try {
      final result = await identityClient.login(
        email: email,
        password: password,
      );
      await _prepareForUser(result.user);
      _sessionToken = result.sessionToken;
      try {
        await sessionStore.writeToken(result.sessionToken);
      } catch (_) {
        await _revokeUnpersistedSession(result.sessionToken);
        return;
      }
      _setState(AuthAuthenticated(result.user));
    } on IdentityClientException catch (error) {
      _finishSubmission(AuthForm.login, _loginMessage(error));
    } catch (_) {
      _finishSubmission(AuthForm.login, _tryAgainMessage);
    }
  }

  Future<void> logout() async {
    final token = _sessionToken;
    try {
      if (token != null && token.isNotEmpty) {
        await identityClient.logout(sessionToken: token);
      }
    } on IdentityClientException catch (error) {
      if (!error.isAuthenticationFailure) {
        _showLogoutRetry();
        return;
      }
    } catch (_) {
      _showLogoutRetry();
      return;
    }
    await _finishSessionCleanup();
  }

  Future<void> invalidateSession() async {
    await _finishSessionCleanup();
  }

  Future<void> switchAccount() async {
    await logout();
  }

  Future<void> retry() async {
    final current = _state;
    if (current is! AuthRetryableError) {
      return;
    }
    switch (current.action) {
      case AuthRetryAction.restoreSession:
        await initialize();
      case AuthRetryAction.logout:
        await logout();
      case AuthRetryAction.clearSession:
        await _finishSessionCleanup();
    }
  }

  bool _beginSubmission(AuthForm form) {
    final current = _state;
    if (current is AuthSignedOut && !current.isSubmitting) {
      _setState(AuthSignedOut(form: form, isSubmitting: true));
      return true;
    }
    return false;
  }

  void _finishSubmission(AuthForm form, String message) {
    _setState(AuthSignedOut(form: form, errorMessage: message));
  }

  Future<void> _prepareForUser(User nextUser) async {
    final current = _state;
    if (current is AuthAuthenticated && current.user.id != nextUser.id) {
      await logout();
    }
  }

  void _showLogoutRetry() {
    _setState(
      const AuthRetryableError(
        message:
            'We could not sign you out. Your session is still protected on this device.',
        action: AuthRetryAction.logout,
      ),
    );
  }

  Future<void> _finishSessionCleanup() async {
    final cleared = await _clearSessionAndPrivateState();
    _setState(
      cleared
          ? const AuthSignedOut()
          : const AuthRetryableError(
              message:
                  'We could not remove the session from this device. Try again.',
              action: AuthRetryAction.clearSession,
            ),
    );
  }

  Future<bool> _clearSessionAndPrivateState() async {
    _sessionToken = null;
    var succeeded = true;
    try {
      await sessionStore.deleteToken();
    } catch (_) {
      succeeded = false;
    }
    try {
      await _clearPrivateState();
    } catch (_) {
      succeeded = false;
    }
    return succeeded;
  }

  Future<void> _revokeUnpersistedSession(String token) async {
    try {
      await identityClient.logout(sessionToken: token);
    } catch (_) {
      // Revocation is best effort because the token could not be persisted.
    }
    final cleared = await _clearSessionAndPrivateState();
    _setState(
      cleared
          ? const AuthSignedOut(
              errorMessage:
                  'We could not securely save your session. Sign in again.',
            )
          : const AuthRetryableError(
              message:
                  'We could not remove the incomplete session from this device. Try again.',
              action: AuthRetryAction.clearSession,
            ),
    );
  }

  void _setState(AuthState value) {
    _state = value;
    notifyListeners();
  }
}

Future<void> _noCleanup() async {}

const _tryAgainMessage =
    'Something went wrong. Check your connection and try again.';

String _loginMessage(IdentityClientException error) {
  return switch (error.kind) {
    IdentityFailureKind.invalidCredentials =>
      'The email or password is incorrect.',
    IdentityFailureKind.rateLimited =>
      'Too many attempts. Wait a moment and try again.',
    IdentityFailureKind.invalidRequest =>
      'Check your email and password, then try again.',
    _ => _tryAgainMessage,
  };
}

String _registrationMessage(IdentityClientException error) {
  return switch (error.kind) {
    IdentityFailureKind.registrationUnavailable =>
      'An account cannot be created with these details.',
    IdentityFailureKind.rateLimited =>
      'Too many attempts. Wait a moment and try again.',
    IdentityFailureKind.invalidRequest =>
      'Use a valid email and a password between 15 and 128 characters.',
    _ => _tryAgainMessage,
  };
}
