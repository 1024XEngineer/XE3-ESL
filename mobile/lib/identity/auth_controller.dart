import 'dart:async';
import 'dart:convert';
import 'dart:math';

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
    this.profileClient,
    PrivateStateCleanup? clearPrivateState,
  }) : _clearPrivateState = clearPrivateState ?? _noCleanup;

  final IdentityClient identityClient;
  final SessionStore sessionStore;
  final UserProfileClient? profileClient;
  final PrivateStateCleanup _clearPrivateState;

  AuthState _state = const AuthLoading();
  String? _sessionToken;
  int _authEpoch = 0;
  int _sessionGeneration = 0;
  Future<void> _sessionStoreTail = Future<void>.value();
  Future<bool> _localCleanupTail = Future<bool>.value(true);
  UserProfile? _profile;
  bool _profileLoaded = false;
  bool _profileSaving = false;
  bool _profilePromptDismissed = false;
  String? _profileErrorMessage;
  _PendingProfileUpdate? _pendingProfileUpdate;

  AuthState get state => _state;
  UserProfile? get profile => _profile;
  bool get profileSaving => _profileSaving;
  String? get profileErrorMessage => _profileErrorMessage;
  bool get shouldPromptForProfile =>
      profileClient != null &&
      _profileLoaded &&
      _profile == null &&
      _profileErrorMessage == null &&
      !_profilePromptDismissed;
  AuthSessionCredential? get currentCredential {
    if (_state is! AuthAuthenticated) {
      return null;
    }
    final token = _sessionToken;
    return token == null
        ? null
        : AuthSessionCredential(
            sessionToken: token,
            generation: _sessionGeneration,
          );
  }

  Future<void> initialize() async {
    final epoch = ++_authEpoch;
    String? expectedToken;
    _setState(const AuthLoading());

    try {
      final token = await _withSessionStoreLock(sessionStore.readToken);
      if (!_isCurrent(epoch)) {
        return;
      }
      if (token == null || token.isEmpty) {
        _clearActiveCredential();
        await _completeLocalSignOut(
          epoch: epoch,
          expectedStoredToken: token,
          deleteRegardless: true,
        );
        return;
      }
      if (!isValidOpaqueSessionToken(token)) {
        _clearActiveCredential();
        await _completeLocalSignOut(epoch: epoch, expectedStoredToken: token);
        return;
      }

      expectedToken = token;
      _setActiveCredential(token);
      final user = await identityClient.currentUser(sessionToken: token);
      if (!_isCurrentSession(epoch, token)) {
        return;
      }
      await _loadProfile(epoch: epoch, token: token, userId: user.id);
      if (!_isCurrentSession(epoch, token)) {
        return;
      }
      _setState(AuthAuthenticated(user));
    } on IdentityClientException catch (error) {
      if (!_isCurrent(epoch) ||
          (expectedToken != null && !_isCurrentSession(epoch, expectedToken))) {
        return;
      }
      if (error.isAuthenticationFailure) {
        _clearActiveCredential();
        await _completeLocalSignOut(
          epoch: epoch,
          expectedStoredToken: expectedToken,
        );
      } else {
        _setState(const AuthRetryableError());
      }
    } catch (_) {
      if (_isCurrent(epoch) &&
          (expectedToken == null || _isCurrentSession(epoch, expectedToken))) {
        _setState(const AuthRetryableError());
      }
    }
  }

  void showLogin() {
    final current = _state;
    if (current is! AuthSignedOut || current.isSubmitting) {
      return;
    }
    _setState(const AuthSignedOut(form: AuthForm.login));
  }

  void showRegister() {
    final current = _state;
    if (current is! AuthSignedOut || current.isSubmitting) {
      return;
    }
    _setState(const AuthSignedOut(form: AuthForm.register));
  }

  Future<void> register({
    required String email,
    required String password,
    String? displayName,
  }) async {
    if (!_beginSubmission(AuthForm.register)) {
      return;
    }
    final epoch = _authEpoch;

    try {
      final profileRegistrationClient =
          identityClient is ProfileRegistrationClient
          ? identityClient as ProfileRegistrationClient
          : null;
      if (displayName != null && profileRegistrationClient != null) {
        await profileRegistrationClient.registerWithProfile(
          email: email,
          password: password,
          displayName: displayName,
        );
      } else {
        await identityClient.register(email: email, password: password);
      }
      if (!_isCurrent(epoch)) {
        return;
      }
      _setState(const AuthSignedOut(noticeMessage: '账号创建成功，请登录后继续。'));
    } on IdentityClientException catch (error) {
      if (_isCurrent(epoch)) {
        _finishSubmission(AuthForm.register, _registrationMessage(error));
      }
    } catch (_) {
      if (_isCurrent(epoch)) {
        _finishSubmission(AuthForm.register, _tryAgainMessage);
      }
    }
  }

  Future<void> login({required String email, required String password}) async {
    if (!_beginSubmission(AuthForm.login)) {
      return;
    }
    final epoch = ++_authEpoch;

    try {
      final result = await identityClient.login(
        email: email,
        password: password,
      );
      if (!_isCurrent(epoch)) {
        unawaited(_bestEffortRevoke(result.sessionToken));
        return;
      }

      final locallyIsolated = await _localCleanupTail;
      if (!_isCurrent(epoch)) {
        unawaited(_bestEffortRevoke(result.sessionToken));
        return;
      }
      if (!locallyIsolated) {
        unawaited(_bestEffortRevoke(result.sessionToken));
        _setState(
          const AuthRetryableError(
            message: '暂时无法清理上一个账号的本地数据，请重试。',
            action: AuthRetryAction.clearLocalState,
          ),
        );
        return;
      }

      try {
        final persisted = await _withSessionStoreLock(() async {
          if (!_isCurrent(epoch)) {
            return false;
          }
          await sessionStore.writeToken(result.sessionToken);
          return _isCurrent(epoch);
        });
        if (!persisted) {
          unawaited(_bestEffortRevoke(result.sessionToken));
          return;
        }
      } catch (_) {
        if (!_isCurrent(epoch)) {
          unawaited(_bestEffortRevoke(result.sessionToken));
          return;
        }
        unawaited(_bestEffortRevoke(result.sessionToken));
        await _completeLocalSignOut(
          epoch: epoch,
          expectedStoredToken: result.sessionToken,
          signedOutError: '无法安全保存登录状态，请重新登录。',
        );
        return;
      }
      if (!_isCurrent(epoch)) {
        unawaited(_bestEffortRevoke(result.sessionToken));
        return;
      }
      _setActiveCredential(result.sessionToken);
      await _loadProfile(
        epoch: epoch,
        token: result.sessionToken,
        userId: result.user.id,
      );
      if (!_isCurrentSession(epoch, result.sessionToken)) {
        return;
      }
      _setState(AuthAuthenticated(result.user));
    } on IdentityClientException catch (error) {
      if (_isCurrent(epoch)) {
        _finishSubmission(AuthForm.login, _loginMessage(error));
      }
    } catch (_) {
      if (_isCurrent(epoch)) {
        _finishSubmission(AuthForm.login, _tryAgainMessage);
      }
    }
  }

  Future<void> logout() async {
    await _leaveSession(revokeServerSession: true);
  }

  Future<void> invalidateSession({
    required String expectedSessionToken,
    required int expectedGeneration,
  }) async {
    if (!_matchesCredential(expectedGeneration, expectedSessionToken)) {
      return;
    }
    await _leaveSession(revokeServerSession: false);
  }

  Future<void> switchAccount() async {
    await _leaveSession(revokeServerSession: true);
  }

  void dismissProfilePrompt() {
    if (!shouldPromptForProfile) {
      return;
    }
    _profilePromptDismissed = true;
    notifyListeners();
  }

  Future<String?> updateDisplayName(String displayName) async {
    final client = profileClient;
    final credential = currentCredential;
    final currentState = _state;
    if (client == null ||
        credential == null ||
        currentState is! AuthAuthenticated ||
        _profileSaving) {
      return '当前账号暂时不能修改昵称。';
    }
    final expectedGeneration = credential.generation;
    final expectedToken = credential.sessionToken;
    final expectedProfileVersion = _profile?.profileVersion;
    final pending = _pendingProfileUpdate;
    final operation =
        pending != null &&
            pending.displayName == displayName &&
            pending.expectedProfileVersion == expectedProfileVersion
        ? pending
        : _PendingProfileUpdate(
            displayName: displayName,
            expectedProfileVersion: expectedProfileVersion,
            idempotencyKey: _newProfileIdempotencyKey(),
          );
    _pendingProfileUpdate = operation;
    _profileSaving = true;
    _profileErrorMessage = null;
    notifyListeners();
    try {
      final updated = await client.updateProfile(
        sessionToken: expectedToken,
        displayName: displayName,
        expectedProfileVersion: operation.expectedProfileVersion,
        idempotencyKey: operation.idempotencyKey,
      );
      _pendingProfileUpdate = null;
      if (!_matchesCredential(expectedGeneration, expectedToken) ||
          updated.userId != currentState.user.id) {
        return null;
      }
      _profile = updated;
      _profileLoaded = true;
      _profilePromptDismissed = false;
      return null;
    } on IdentityClientException catch (error) {
      if (!_matchesCredential(expectedGeneration, expectedToken)) {
        return null;
      }
      if (error.isAuthenticationFailure) {
        _pendingProfileUpdate = null;
        await invalidateSession(
          expectedSessionToken: expectedToken,
          expectedGeneration: expectedGeneration,
        );
        return '登录状态已失效，请重新登录。';
      }
      if (error.kind == IdentityFailureKind.profileVersionConflict) {
        _pendingProfileUpdate = null;
        await _loadProfile(
          epoch: _authEpoch,
          token: expectedToken,
          userId: currentState.user.id,
        );
        return '昵称已在其他设备修改，已为你刷新。';
      }
      if (error.kind == IdentityFailureKind.invalidRequest) {
        _pendingProfileUpdate = null;
        return '昵称需要为 1–40 个有效字符。';
      }
      if (!error.retryable) {
        _pendingProfileUpdate = null;
      }
      return '昵称保存失败，请稍后重试。';
    } catch (_) {
      return '昵称保存失败，请稍后重试。';
    } finally {
      if (_matchesCredential(expectedGeneration, expectedToken)) {
        _profileSaving = false;
        notifyListeners();
      }
    }
  }

  Future<void> retry() async {
    final current = _state;
    if (current is! AuthRetryableError) {
      return;
    }
    switch (current.action) {
      case AuthRetryAction.restoreSession:
        await initialize();
      case AuthRetryAction.clearLocalState:
        await _leaveSession(revokeServerSession: false);
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

  Future<void> _leaveSession({required bool revokeServerSession}) async {
    final epoch = ++_authEpoch;
    final token = _sessionToken;
    _clearActiveCredential();
    _setState(const AuthSignedOut(isSubmitting: true));

    if (revokeServerSession && token != null && token.isNotEmpty) {
      unawaited(_bestEffortRevoke(token));
    }

    await _completeLocalSignOut(
      epoch: epoch,
      expectedStoredToken: token,
      deleteRegardless: token == null,
    );
  }

  Future<void> _completeLocalSignOut({
    required int epoch,
    required String? expectedStoredToken,
    bool deleteRegardless = false,
    String? signedOutError,
  }) async {
    final cleared = await _queueLocalCleanup(() async {
      var succeeded = true;
      try {
        await _withSessionStoreLock(() async {
          final storedToken = await sessionStore.readToken();
          final tokenMatches =
              expectedStoredToken != null && storedToken == expectedStoredToken;
          final currentCompensatingDelete =
              deleteRegardless && _isCurrent(epoch);
          if (!tokenMatches && !currentCompensatingDelete) {
            return;
          }
          await sessionStore.deleteToken();
        });
      } catch (_) {
        succeeded = false;
      }

      try {
        await _clearPrivateState();
      } catch (_) {
        succeeded = false;
      }
      return succeeded;
    });

    if (!_isCurrent(epoch)) {
      return;
    }
    if (!cleared) {
      _setState(
        const AuthRetryableError(
          message: '暂时无法完整清理本机登录状态，请重试后再登录。',
          action: AuthRetryAction.clearLocalState,
        ),
      );
      return;
    }
    _setState(AuthSignedOut(errorMessage: signedOutError));
  }

  Future<void> _bestEffortRevoke(String token) async {
    try {
      await identityClient
          .logout(sessionToken: token)
          .timeout(const Duration(seconds: 2));
    } catch (_) {
      // Local logout is authoritative for the client. Server revocation is
      // bounded and never restores a locally invalidated session.
    }
  }

  bool _isCurrent(int epoch) => epoch == _authEpoch;

  bool _isCurrentSession(int epoch, String token) {
    return _isCurrent(epoch) && _sessionToken == token;
  }

  bool _matchesCredential(int generation, String token) {
    return generation == _sessionGeneration && _sessionToken == token;
  }

  void _setActiveCredential(String token) {
    if (_sessionToken == token) {
      return;
    }
    _sessionToken = token;
    _sessionGeneration++;
    _resetProfileState();
  }

  void _clearActiveCredential() {
    if (_sessionToken == null) {
      return;
    }
    _sessionToken = null;
    _sessionGeneration++;
    _resetProfileState();
  }

  Future<void> _loadProfile({
    required int epoch,
    required String token,
    required String userId,
  }) async {
    final client = profileClient;
    if (client == null) {
      return;
    }
    _profileLoaded = false;
    _profileErrorMessage = null;
    try {
      final loaded = await client.currentProfile(sessionToken: token);
      if (!_isCurrentSession(epoch, token) || loaded.userId != userId) {
        return;
      }
      _profile = loaded;
      _profileLoaded = true;
    } on IdentityClientException catch (error) {
      if (!_isCurrentSession(epoch, token)) {
        return;
      }
      _profileLoaded = true;
      if (error.kind == IdentityFailureKind.profileNotFound) {
        _profile = null;
      } else {
        _profileErrorMessage = '昵称暂时无法加载，请稍后重试。';
      }
    } catch (_) {
      if (_isCurrentSession(epoch, token)) {
        _profileLoaded = true;
        _profileErrorMessage = '昵称暂时无法加载，请稍后重试。';
      }
    }
  }

  void _resetProfileState() {
    _profile = null;
    _profileLoaded = false;
    _profileSaving = false;
    _profilePromptDismissed = false;
    _profileErrorMessage = null;
    _pendingProfileUpdate = null;
  }

  Future<T> _withSessionStoreLock<T>(Future<T> Function() action) {
    final result = _sessionStoreTail.then((_) => action());
    _sessionStoreTail = result.then<void>(
      (_) {},
      onError: (Object _, StackTrace _) {},
    );
    return result;
  }

  Future<bool> _queueLocalCleanup(Future<bool> Function() action) {
    final result = _localCleanupTail.then((_) => action());
    _localCleanupTail = result.then<bool>(
      (succeeded) => succeeded,
      onError: (Object _, StackTrace _) => false,
    );
    return result;
  }

  void _setState(AuthState value) {
    _state = value;
    notifyListeners();
  }
}

Future<void> _noCleanup() async {}

const _tryAgainMessage = '操作未完成，请检查网络后重试。';

String _loginMessage(IdentityClientException error) {
  return switch (error.kind) {
    IdentityFailureKind.invalidCredentials => '邮箱或密码不正确。',
    IdentityFailureKind.rateLimited => '尝试次数过多，请稍后重试。',
    IdentityFailureKind.invalidRequest => '请检查邮箱和密码后重试。',
    _ => _tryAgainMessage,
  };
}

String _registrationMessage(IdentityClientException error) {
  return switch (error.kind) {
    IdentityFailureKind.registrationUnavailable => '无法使用这些信息创建账号。',
    IdentityFailureKind.rateLimited => '尝试次数过多，请稍后重试。',
    IdentityFailureKind.invalidRequest => '请输入有效邮箱和 15–128 个字符的密码。',
    _ => _tryAgainMessage,
  };
}

String _newProfileIdempotencyKey() {
  final random = Random.secure();
  final bytes = List<int>.generate(18, (_) => random.nextInt(256));
  return 'profile_${base64Url.encode(bytes).replaceAll('=', '')}';
}

final class _PendingProfileUpdate {
  const _PendingProfileUpdate({
    required this.displayName,
    required this.expectedProfileVersion,
    required this.idempotencyKey,
  });

  final String displayName;
  final int? expectedProfileVersion;
  final String idempotencyKey;
}
