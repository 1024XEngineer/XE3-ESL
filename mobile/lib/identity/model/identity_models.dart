import 'package:speakup/identity/auth_input.dart';

final RegExp _opaqueSessionTokenPattern = RegExp(
  r'^sess_[A-Za-z0-9._~+/-]+={0,}$',
);
final RegExp _userIdPattern = RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]*$');
final RegExp _rfc3339DateTimePattern = RegExp(
  r'^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})'
  r'(?:\.(\d+))?(?:[Zz]|[+-](\d{2}):(\d{2}))$',
);

bool isValidOpaqueSessionToken(String sessionToken) {
  return _opaqueSessionTokenPattern.hasMatch(sessionToken);
}

final class User {
  const User({required this.id, required this.email});

  factory User.fromJson(Map<String, Object?> json) {
    if (!_hasExactKeys(json, const {'user_id', 'email'})) {
      throw const FormatException('Invalid identity response.');
    }
    final id = _requiredString(json, 'user_id');
    final email = _requiredString(json, 'email');
    if (id.length > 128 ||
        !_userIdPattern.hasMatch(id) ||
        normalizeIdentityEmailInput(email) != email ||
        email.toLowerCase() != email ||
        !isValidIdentityEmailInput(email)) {
      throw const FormatException('Invalid identity response.');
    }
    return User(id: id, email: email);
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
    if (!_hasExactKeys(json, const {
      'user',
      'session_token',
      'token_type',
      'expires_at',
    })) {
      throw const FormatException('Invalid login response.');
    }
    final userJson = json['user'];
    final sessionToken = json['session_token'];
    final tokenType = json['token_type'];
    final expiresAt = json['expires_at'];
    if (userJson is! Map<String, Object?> ||
        sessionToken is! String ||
        !isValidOpaqueSessionToken(sessionToken) ||
        tokenType != 'Bearer' ||
        expiresAt is! String ||
        !_rfc3339DateTimePattern.hasMatch(expiresAt)) {
      throw const FormatException('Invalid login response.');
    }

    final parsedExpiry = _tryParseStrictRfc3339(expiresAt);
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

bool _hasExactKeys(Map<String, Object?> json, Set<String> expected) {
  return json.length == expected.length && expected.every(json.containsKey);
}

DateTime? _tryParseStrictRfc3339(String value) {
  final match = _rfc3339DateTimePattern.firstMatch(value);
  if (match == null) {
    return null;
  }

  final year = int.parse(match.group(1)!);
  final month = int.parse(match.group(2)!);
  final day = int.parse(match.group(3)!);
  final hour = int.parse(match.group(4)!);
  final minute = int.parse(match.group(5)!);
  final second = int.parse(match.group(6)!);
  final offsetHour = match.group(8) == null ? 0 : int.parse(match.group(8)!);
  final offsetMinute = match.group(9) == null ? 0 : int.parse(match.group(9)!);

  if (month < 1 ||
      month > 12 ||
      day < 1 ||
      day > _daysInMonth(year, month) ||
      hour > 23 ||
      minute > 59 ||
      second > 60 ||
      offsetHour > 23 ||
      offsetMinute > 59) {
    return null;
  }
  return DateTime.tryParse(
    value.replaceRange(10, 11, 'T').replaceAll(RegExp(r'z$'), 'Z'),
  );
}

int _daysInMonth(int year, int month) {
  return switch (month) {
    2 => _isLeapYear(year) ? 29 : 28,
    4 || 6 || 9 || 11 => 30,
    _ => 31,
  };
}

bool _isLeapYear(int year) {
  return year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
}
