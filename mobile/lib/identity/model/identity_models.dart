final RegExp _opaqueSessionTokenPattern = RegExp(
  r'^sess_[A-Za-z0-9._~+/-]+={0,}$',
);

bool isValidOpaqueSessionToken(String sessionToken) {
  return _opaqueSessionTokenPattern.hasMatch(sessionToken);
}

final class User {
  const User({required this.id, required this.email});

  factory User.fromJson(Map<String, Object?> json) {
    return User(
      id: _requiredString(json, 'user_id'),
      email: _requiredString(json, 'email'),
    );
  }

  final String id;
  final String email;

  static String _requiredString(Map<String, Object?> json, String key) {
    final value = json[key];
    if (value is! String || value.isEmpty) {
      throw const FormatException('Invalid identity response.');
    }
    return value;
  }
}

final class LoginResult {
  const LoginResult({
    required this.user,
    required this.sessionToken,
    required this.expiresAt,
  });

  factory LoginResult.fromJson(Map<String, Object?> json) {
    final userJson = json['user'];
    final sessionToken = json['session_token'];
    final tokenType = json['token_type'];
    final expiresAt = json['expires_at'];
    if (userJson is! Map<String, Object?> ||
        sessionToken is! String ||
        !isValidOpaqueSessionToken(sessionToken) ||
        tokenType != 'Bearer' ||
        expiresAt is! String) {
      throw const FormatException('Invalid login response.');
    }

    final parsedExpiry = DateTime.tryParse(expiresAt);
    if (parsedExpiry == null) {
      throw const FormatException('Invalid login response.');
    }

    return LoginResult(
      user: User.fromJson(userJson),
      sessionToken: sessionToken,
      expiresAt: parsedExpiry,
    );
  }

  final User user;
  final String sessionToken;
  final DateTime expiresAt;
}
