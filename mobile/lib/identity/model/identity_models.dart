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

final class UserProfile {
  const UserProfile({
    required this.userId,
    required this.displayName,
    required this.profileVersion,
    this.avatar,
    required this.createdAt,
    required this.updatedAt,
  });

  factory UserProfile.fromJson(Map<String, Object?> json) {
    const requiredKeys = {
      'user_id',
      'display_name',
      'profile_version',
      'created_at',
      'updated_at',
    };
    if (!_hasRequiredAndAllowedKeys(json, requiredKeys, const {
      ...requiredKeys,
      'avatar',
    })) {
      throw const FormatException('Invalid user profile response.');
    }
    final userId = User._requiredString(json, 'user_id');
    final displayName = User._requiredString(json, 'display_name');
    final profileVersion = json['profile_version'];
    final createdAt = json['created_at'];
    final updatedAt = json['updated_at'];
    final avatarJson = json['avatar'];
    if (userId.length > 128 ||
        !_userIdPattern.hasMatch(userId) ||
        displayName.trim() != displayName ||
        validateDisplayNameInput(displayName) != null ||
        profileVersion is! int ||
        profileVersion < 1 ||
        createdAt is! String ||
        updatedAt is! String) {
      throw const FormatException('Invalid user profile response.');
    }
    final parsedCreatedAt = _tryParseStrictRfc3339(createdAt);
    final parsedUpdatedAt = _tryParseStrictRfc3339(updatedAt);
    if (parsedCreatedAt == null ||
        parsedUpdatedAt == null ||
        parsedUpdatedAt.isBefore(parsedCreatedAt)) {
      throw const FormatException('Invalid user profile response.');
    }
    return UserProfile(
      userId: userId,
      displayName: displayName,
      profileVersion: profileVersion,
      avatar: avatarJson == null
          ? null
          : avatarJson is Map<String, Object?>
          ? UserProfileAvatar.fromJson(avatarJson)
          : throw const FormatException('Invalid user profile response.'),
      createdAt: parsedCreatedAt,
      updatedAt: parsedUpdatedAt,
    );
  }

  final String userId;
  final String displayName;
  final int profileVersion;
  final UserProfileAvatar? avatar;
  final DateTime createdAt;
  final DateTime updatedAt;
}

final class UserProfileAvatar {
  const UserProfileAvatar({
    required this.width,
    required this.height,
    required this.updatedAt,
  });

  factory UserProfileAvatar.fromJson(Map<String, Object?> json) {
    if (!_hasExactKeys(json, const {'width', 'height', 'updated_at'})) {
      throw const FormatException('Invalid user profile avatar response.');
    }
    final width = json['width'];
    final height = json['height'];
    final updatedAt = json['updated_at'];
    if (width is! int ||
        width < 1 ||
        width > 16384 ||
        height is! int ||
        height < 1 ||
        height > 16384 ||
        updatedAt is! String) {
      throw const FormatException('Invalid user profile avatar response.');
    }
    final parsedUpdatedAt = _tryParseStrictRfc3339(updatedAt);
    if (parsedUpdatedAt == null) {
      throw const FormatException('Invalid user profile avatar response.');
    }
    return UserProfileAvatar(
      width: width,
      height: height,
      updatedAt: parsedUpdatedAt,
    );
  }

  final int width;
  final int height;
  final DateTime updatedAt;
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

bool _hasRequiredAndAllowedKeys(
  Map<String, Object?> json,
  Set<String> required,
  Set<String> allowed,
) {
  return required.every(json.containsKey) && json.keys.every(allowed.contains);
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
