import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../model/identity_models.dart';
import '../network/bearer_authentication.dart';
import '../network/identity_http_transport.dart';
import '../network/transport_security.dart';

enum IdentityFailureKind {
  invalidRequest,
  invalidCredentials,
  authenticationRequired,
  registrationUnavailable,
  profileNotFound,
  profileVersionConflict,
  rateLimited,
  server,
  network,
  invalidResponse,
  unexpected,
  imageTooLarge,
  invalidImage,
  idempotencyConflict,
  resourceNotFound,
  resourceProcessing,
}

enum _IdentityOperation {
  register,
  login,
  currentUser,
  currentProfile,
  updateProfile,
  logout,
  uploadAvatar,
  useDefaultAvatar,
  avatarContent,
}

final class IdentityClientException implements Exception {
  const IdentityClientException({
    required this.kind,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
    this.correlationId,
  });

  final IdentityFailureKind kind;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;
  final String? correlationId;

  bool get isAuthenticationFailure =>
      kind == IdentityFailureKind.authenticationRequired;

  @override
  String toString() {
    final status = statusCode == null ? '' : ', statusCode: $statusCode';
    final code = errorCode == null ? '' : ', errorCode: $errorCode';
    return 'IdentityClientException(kind: ${kind.name}$status$code)';
  }
}

abstract interface class IdentityClient {
  Future<User> register({required String email, required String password});

  Future<LoginResult> login({required String email, required String password});

  Future<User> currentUser({required String sessionToken});

  Future<void> logout({required String sessionToken});
}

abstract interface class ProfileRegistrationClient {
  Future<User> registerWithProfile({
    required String email,
    required String password,
    required String displayName,
  });
}

abstract interface class UserProfileClient {
  Future<UserProfile> currentProfile({required String sessionToken});

  Future<UserProfile> updateProfile({
    required String sessionToken,
    required String displayName,
    required int? expectedProfileVersion,
  });
}

final class UserAvatarImage {
  UserAvatarImage({required this.contentType, required Uint8List bytes})
    : bytes = Uint8List.fromList(bytes);

  final String contentType;
  final Uint8List bytes;
}

final class UserAvatarContent {
  const UserAvatarContent({required this.url, required this.expiresAt});

  final Uri url;
  final DateTime expiresAt;
}

abstract interface class UserAvatarClient {
  Future<UserProfile> uploadAvatar({
    required String sessionToken,
    required UserAvatarImage image,
    required int expectedProfileVersion,
    required String idempotencyKey,
  });

  Future<UserProfile> useDefaultAvatar({
    required String sessionToken,
    required int expectedProfileVersion,
  });

  Future<UserAvatarContent> currentAvatarContent({
    required String sessionToken,
  });
}

final class WireIdentityClient
    implements
        IdentityClient,
        ProfileRegistrationClient,
        UserProfileClient,
        UserAvatarClient {
  factory WireIdentityClient({
    required Uri baseUri,
    IdentityHttpTransport? transport,
  }) {
    final trustedOrigin = TrustedIdentityHttpOrigin(baseUri);
    return WireIdentityClient._(
      baseUri,
      transport ?? IoIdentityHttpTransport(),
      trustedOrigin,
    );
  }

  WireIdentityClient._(this._baseUri, this._transport, this._trustedOrigin);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<User> register({
    required String email,
    required String password,
  }) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/register',
      body: <String, Object?>{'email': email, 'password': password},
    );
    _requireStatus(response, 201, _IdentityOperation.register);
    return _decode(response, (json) => User.fromJson(json));
  }

  @override
  Future<User> registerWithProfile({
    required String email,
    required String password,
    required String displayName,
  }) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/register',
      body: <String, Object?>{
        'email': email,
        'password': password,
        'display_name': displayName,
      },
    );
    _requireStatus(response, 201, _IdentityOperation.register);
    return _decode(response, (json) => User.fromJson(json));
  }

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/login',
      body: <String, Object?>{'email': email, 'password': password},
    );
    _requireStatus(response, 200, _IdentityOperation.login);
    return _decode(response, (json) => LoginResult.fromJson(json));
  }

  @override
  Future<User> currentUser({required String sessionToken}) async {
    final response = await _send(
      method: 'GET',
      path: '/v1/me',
      sessionToken: sessionToken,
    );
    _requireStatus(response, 200, _IdentityOperation.currentUser);
    return _decode(response, (json) => User.fromJson(json));
  }

  @override
  Future<UserProfile> currentProfile({required String sessionToken}) async {
    final response = await _send(
      method: 'GET',
      path: '/v1/me/profile',
      sessionToken: sessionToken,
    );
    _requireStatus(response, 200, _IdentityOperation.currentProfile);
    return _decode(response, (json) => UserProfile.fromJson(json));
  }

  @override
  Future<UserProfile> updateProfile({
    required String sessionToken,
    required String displayName,
    required int? expectedProfileVersion,
  }) async {
    final body = <String, Object?>{'display_name': displayName};
    if (expectedProfileVersion != null) {
      body['expected_profile_version'] = expectedProfileVersion;
    }
    final response = await _send(
      method: 'PATCH',
      path: '/v1/me/profile',
      sessionToken: sessionToken,
      body: body,
    );
    _requireStatus(response, 200, _IdentityOperation.updateProfile);
    return _decode(response, (json) => UserProfile.fromJson(json));
  }

  @override
  Future<void> logout({required String sessionToken}) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/auth/logout',
      sessionToken: sessionToken,
    );
    _requireStatus(response, 204, _IdentityOperation.logout);
  }

  @override
  Future<UserProfile> uploadAvatar({
    required String sessionToken,
    required UserAvatarImage image,
    required int expectedProfileVersion,
    required String idempotencyKey,
  }) async {
    final response = await _send(
      method: 'POST',
      path: '/v1/me/avatar',
      sessionToken: sessionToken,
      bodyBytes: image.bytes,
      headers: <String, String>{
        HttpHeaders.contentTypeHeader: image.contentType,
        HttpHeaders.ifMatchHeader: '"$expectedProfileVersion"',
        'Idempotency-Key': idempotencyKey,
      },
    );
    _requireStatus(response, 200, _IdentityOperation.uploadAvatar);
    return _decode(response, UserProfile.fromJson);
  }

  @override
  Future<UserProfile> useDefaultAvatar({
    required String sessionToken,
    required int expectedProfileVersion,
  }) async {
    final response = await _send(
      method: 'DELETE',
      path: '/v1/me/avatar',
      sessionToken: sessionToken,
      headers: <String, String>{
        HttpHeaders.ifMatchHeader: '"$expectedProfileVersion"',
      },
    );
    _requireStatus(response, 200, _IdentityOperation.useDefaultAvatar);
    return _decode(response, UserProfile.fromJson);
  }

  @override
  Future<UserAvatarContent> currentAvatarContent({
    required String sessionToken,
  }) async {
    final response = await _send(
      method: 'GET',
      path: '/v1/me/avatar/content',
      sessionToken: sessionToken,
    );
    _requireStatus(response, 200, _IdentityOperation.avatarContent);
    return _decode(response, (json) {
      if (!_hasExactJsonKeys(json, const {'content_url', 'expires_at'})) {
        throw const FormatException('Invalid avatar content response.');
      }
      final rawUrl = json['content_url'];
      final rawExpiry = json['expires_at'];
      if (rawUrl is! String || rawExpiry is! String) {
        throw const FormatException('Invalid avatar content response.');
      }
      final url = Uri.tryParse(rawUrl);
      final expiresAt = DateTime.tryParse(rawExpiry)?.toUtc();
      if (url == null ||
          !url.hasAuthority ||
          (url.scheme != 'https' && url.scheme != 'http') ||
          expiresAt == null ||
          !expiresAt.isAfter(DateTime.now().toUtc())) {
        throw const FormatException('Invalid avatar content response.');
      }
      return UserAvatarContent(url: url, expiresAt: expiresAt);
    });
  }

  Future<IdentityHttpResponse> _send({
    required String method,
    required String path,
    Map<String, Object?>? body,
    Uint8List? bodyBytes,
    String? sessionToken,
    Map<String, String> headers = const {},
  }) async {
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri, sessionToken: sessionToken);
    final requestHeaders = <String, String>{
      ...headers,
      HttpHeaders.acceptHeader: ContentType.json.mimeType,
      if (body != null)
        HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
      if (sessionToken != null)
        HttpHeaders.authorizationHeader: bearerAuthorizationValue(sessionToken),
    };

    try {
      return await _transport.send(
        method: method,
        uri: uri,
        headers: requestHeaders,
        body: body == null ? null : jsonEncode(body),
        bodyBytes: bodyBytes,
      );
    } on IdentityClientException {
      rethrow;
    } on SocketException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } on TimeoutException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      throw const IdentityClientException(kind: IdentityFailureKind.unexpected);
    }
  }

  void _requireStatus(
    IdentityHttpResponse response,
    int expected,
    _IdentityOperation operation,
  ) {
    if (response.statusCode == expected) {
      return;
    }
    throw _exceptionFor(response, operation);
  }

  IdentityClientException _exceptionFor(
    IdentityHttpResponse response,
    _IdentityOperation operation,
  ) {
    String? decodedErrorCode;
    String? correlationId;
    try {
      final decoded = jsonDecode(response.body);
      if (decoded is Map<String, Object?>) {
        final error = decoded['error'];
        if (error is Map<String, Object?>) {
          final decodedCode = error['code'];
          final decodedCorrelationId = error['correlation_id'];
          if (decodedCode is String) {
            decodedErrorCode = decodedCode;
          }
          if (decodedCorrelationId is String) {
            correlationId = decodedCorrelationId;
          }
        }
      }
    } on FormatException {
      // Status remains authoritative when an intermediary returns non-JSON.
    }

    final errorCode = _normalizedErrorCode(
      operation: operation,
      statusCode: response.statusCode,
      decodedErrorCode: decodedErrorCode,
    );
    final kind = switch (errorCode) {
      'invalid_request' => IdentityFailureKind.invalidRequest,
      'invalid_credentials' => IdentityFailureKind.invalidCredentials,
      'authentication_required' => IdentityFailureKind.authenticationRequired,
      'account_registration_unavailable' =>
        IdentityFailureKind.registrationUnavailable,
      'profile_not_found' => IdentityFailureKind.profileNotFound,
      'profile_version_conflict' => IdentityFailureKind.profileVersionConflict,
      'rate_limited' => IdentityFailureKind.rateLimited,
      'internal_error' => IdentityFailureKind.server,
      'image_too_large' => IdentityFailureKind.imageTooLarge,
      'invalid_image' ||
      'unsupported_image_format' => IdentityFailureKind.invalidImage,
      'idempotency_key_conflict' => IdentityFailureKind.idempotencyConflict,
      'resource_not_found' => IdentityFailureKind.resourceNotFound,
      'resource_processing' => IdentityFailureKind.resourceProcessing,
      _ => IdentityFailureKind.unexpected,
    };

    return IdentityClientException(
      kind: kind,
      statusCode: response.statusCode,
      errorCode: errorCode,
      retryable:
          kind == IdentityFailureKind.rateLimited ||
          kind == IdentityFailureKind.server,
      correlationId: correlationId,
    );
  }

  String? _normalizedErrorCode({
    required _IdentityOperation operation,
    required int statusCode,
    required String? decodedErrorCode,
  }) {
    final allowedCode = switch ((operation, statusCode, decodedErrorCode)) {
      (
        _IdentityOperation.register || _IdentityOperation.login,
        400,
        'invalid_request',
      ) =>
        'invalid_request',
      (_IdentityOperation.login, 401, 'invalid_credentials') =>
        'invalid_credentials',
      (
        _IdentityOperation.currentUser ||
            _IdentityOperation.currentProfile ||
            _IdentityOperation.updateProfile ||
            _IdentityOperation.uploadAvatar ||
            _IdentityOperation.useDefaultAvatar ||
            _IdentityOperation.avatarContent ||
            _IdentityOperation.logout,
        401,
        'authentication_required',
      ) =>
        'authentication_required',
      (_IdentityOperation.register, 409, 'account_registration_unavailable') =>
        'account_registration_unavailable',
      (_IdentityOperation.currentProfile, 404, 'profile_not_found') =>
        'profile_not_found',
      (_IdentityOperation.updateProfile, 409, 'profile_version_conflict') =>
        'profile_version_conflict',
      (_IdentityOperation.updateProfile, 400, 'invalid_request') =>
        'invalid_request',
      (_IdentityOperation.uploadAvatar, 400, 'invalid_request') =>
        'invalid_request',
      (_IdentityOperation.uploadAvatar, 400, 'invalid_image') =>
        'invalid_image',
      (_IdentityOperation.uploadAvatar, 400, 'unsupported_image_format') =>
        'unsupported_image_format',
      (_IdentityOperation.uploadAvatar, 413, 'image_too_large') =>
        'image_too_large',
      (_IdentityOperation.uploadAvatar, 409, 'idempotency_key_conflict') =>
        'idempotency_key_conflict',
      (_IdentityOperation.uploadAvatar, 409, 'resource_processing') =>
        'resource_processing',
      (
        _IdentityOperation.uploadAvatar || _IdentityOperation.useDefaultAvatar,
        409,
        'profile_version_conflict',
      ) =>
        'profile_version_conflict',
      (_IdentityOperation.avatarContent, 404, 'resource_not_found') =>
        'resource_not_found',
      (
        _IdentityOperation.register || _IdentityOperation.login,
        429,
        'rate_limited',
      ) =>
        'rate_limited',
      (_, >= 500, 'internal_error') => 'internal_error',
      _ => null,
    };
    if (allowedCode != null) {
      return allowedCode;
    }

    return switch ((operation, statusCode)) {
      (_IdentityOperation.register || _IdentityOperation.login, 400) =>
        'invalid_request',
      (_IdentityOperation.login, 401) => 'invalid_credentials',
      (
        _IdentityOperation.currentUser ||
            _IdentityOperation.currentProfile ||
            _IdentityOperation.updateProfile ||
            _IdentityOperation.uploadAvatar ||
            _IdentityOperation.useDefaultAvatar ||
            _IdentityOperation.avatarContent ||
            _IdentityOperation.logout,
        401,
      ) =>
        'authentication_required',
      (_IdentityOperation.register, 409) => 'account_registration_unavailable',
      (_IdentityOperation.currentProfile, 404) => 'profile_not_found',
      (_IdentityOperation.updateProfile, 400) => 'invalid_request',
      (_IdentityOperation.updateProfile, 409) => 'profile_version_conflict',
      (_IdentityOperation.uploadAvatar, 400) => 'invalid_image',
      (_IdentityOperation.uploadAvatar, 413) => 'image_too_large',
      (
        _IdentityOperation.uploadAvatar || _IdentityOperation.useDefaultAvatar,
        409,
      ) =>
        'profile_version_conflict',
      (_IdentityOperation.avatarContent, 404) => 'resource_not_found',
      (_IdentityOperation.register || _IdentityOperation.login, 429) =>
        'rate_limited',
      (_, >= 500) => 'internal_error',
      _ => null,
    };
  }

  T _decode<T>(
    IdentityHttpResponse response,
    T Function(Map<String, Object?> json) decode,
  ) {
    try {
      final json = jsonDecode(response.body);
      if (json is! Map<String, Object?>) {
        throw const FormatException('Invalid identity response.');
      }
      return decode(json);
    } on FormatException {
      throw IdentityClientException(
        kind: IdentityFailureKind.invalidResponse,
        statusCode: response.statusCode,
      );
    } on TypeError {
      throw IdentityClientException(
        kind: IdentityFailureKind.invalidResponse,
        statusCode: response.statusCode,
      );
    }
  }
}

bool _hasExactJsonKeys(Map<String, Object?> json, Set<String> expected) {
  return json.length == expected.length && expected.every(json.containsKey);
}
