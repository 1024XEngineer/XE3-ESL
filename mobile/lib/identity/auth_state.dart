import 'package:speakup/identity/model/identity_models.dart';

enum AuthForm { login, register }

enum AuthRetryAction { restoreSession, logout, clearSession }

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
    this.message =
        'We could not confirm your session. Check your connection and try again.',
    this.action = AuthRetryAction.restoreSession,
  });

  final String message;
  final AuthRetryAction action;
}

final class AuthAuthenticated extends AuthState {
  const AuthAuthenticated(this.user);

  final User user;
}
