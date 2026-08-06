import 'package:speakup/identity/model/identity_models.dart';

enum AuthForm { login, register }

enum AuthRetryAction { restoreSession, clearLocalState }

final class AuthSessionCredential {
  const AuthSessionCredential({
    required this.sessionToken,
    required this.generation,
  });

  final String sessionToken;
  final int generation;

  @override
  String toString() => 'AuthSessionCredential(generation: $generation)';
}

typedef AuthSessionCredentialProvider = AuthSessionCredential? Function();
typedef AuthSessionInvalidator =
    Future<void> Function({
      required String expectedSessionToken,
      required int expectedGeneration,
    });

bool isSameAuthSessionCredential(
  AuthSessionCredential? current,
  AuthSessionCredential captured,
) {
  return current != null &&
      current.generation == captured.generation &&
      current.sessionToken == captured.sessionToken;
}

/// A redacted cancellation signal for work that completed after account
/// switch, logout, or remote invalidation.
///
/// The response or connection must not be exposed to the caller because it
/// belongs to a credential generation that is no longer active.
final class AuthSessionSupersededException implements Exception {
  const AuthSessionSupersededException();

  @override
  String toString() =>
      'Authenticated operation was discarded because the session changed.';
}

sealed class AuthState {
  const AuthState();
}

final class AuthLoading extends AuthState {
  const AuthLoading();
}

final class AuthSignedOut extends AuthState {
  const AuthSignedOut({
    this.form = AuthForm.login,
    this.isSubmitting = false,
    this.errorMessage,
    this.noticeMessage,
  });

  final AuthForm form;
  final bool isSubmitting;
  final String? errorMessage;
  final String? noticeMessage;

  AuthSignedOut copyWith({
    AuthForm? form,
    bool? isSubmitting,
    String? errorMessage,
    String? noticeMessage,
    bool clearMessages = false,
  }) {
    return AuthSignedOut(
      form: form ?? this.form,
      isSubmitting: isSubmitting ?? this.isSubmitting,
      errorMessage: clearMessages ? null : errorMessage ?? this.errorMessage,
      noticeMessage: clearMessages ? null : noticeMessage ?? this.noticeMessage,
    );
  }
}

final class AuthRetryableError extends AuthState {
  const AuthRetryableError({
    this.message = '暂时无法确认登录状态，请检查网络后重试。',
    this.action = AuthRetryAction.restoreSession,
  });

  final String message;
  final AuthRetryAction action;
}

final class AuthAuthenticated extends AuthState {
  const AuthAuthenticated(this.user);

  final User user;
}
