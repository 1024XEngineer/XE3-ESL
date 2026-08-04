import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';

import 'avatar_models.dart';
import 'avatar_session_token_client.dart';

final class AvatarSessionWireRequest {
  const AvatarSessionWireRequest({
    required this.method,
    required this.uri,
    required this.headers,
    required this.maximumResponseBytes,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final int maximumResponseBytes;
}

final class AvatarSessionWireResponse {
  const AvatarSessionWireResponse({
    required this.statusCode,
    required this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final Uint8List body;
  final Map<String, String> headers;
}

abstract interface class AvatarSessionWireTransport {
  Future<AvatarSessionWireResponse> send(AvatarSessionWireRequest request);

  void close({bool force = false});
}

typedef AvatarSessionWireTransportFactory =
    AvatarSessionWireTransport Function();
typedef AvatarSessionClock = DateTime Function();

final class WireAvatarSessionTokenClient implements AvatarSessionTokenClient {
  factory WireAvatarSessionTokenClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    AvatarSessionWireTransport? transport,
    AvatarSessionWireTransportFactory? transportFactory,
    AvatarSessionClock? clock,
    Duration timeout = const Duration(seconds: 30),
  }) {
    if (timeout <= Duration.zero ||
        (transport != null && transportFactory != null)) {
      throw ArgumentError('Avatar session transport configuration is invalid.');
    }
    final createTransport =
        transportFactory ??
        () => IoAvatarSessionWireTransport(timeout: timeout);
    return WireAvatarSessionTokenClient._(
      baseUri,
      TrustedIdentityHttpOrigin(baseUri),
      credentialProvider,
      invalidateSession,
      transport ?? createTransport(),
      transport == null,
      createTransport,
      clock ?? _utcNow,
      timeout,
    );
  }

  WireAvatarSessionTokenClient._(
    this._baseUri,
    this._trustedOrigin,
    this._credentialProvider,
    this._invalidateSession,
    this._transport,
    this._ownsTransport,
    this._transportFactory,
    this._clock,
    this._timeout,
  );

  static const _maximumJsonBytes = 32 * 1024;
  static const _maximumGrantLifetime = Duration(minutes: 10);
  static const _clockSkew = Duration(seconds: 30);
  static final _resourceIdPattern = RegExp(
    r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$',
  );
  static final _rfc3339UtcPattern = RegExp(
    r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}'
    r'(?:\.\d+)?[Zz]$',
  );

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  AvatarSessionWireTransport _transport;
  final bool _ownsTransport;
  final AvatarSessionWireTransportFactory _transportFactory;
  final AvatarSessionClock _clock;
  final Duration _timeout;

  int _accountGeneration = 0;
  bool _disposed = false;
  Future<void>? _cleanupFuture;
  final Set<Future<void>> _inFlight = <Future<void>>{};

  @override
  Future<AvatarSessionGrant> createSession({
    required String practiceSessionId,
  }) {
    if (!_resourceIdPattern.hasMatch(practiceSessionId)) {
      return Future<AvatarSessionGrant>.error(
        const AvatarSessionTokenException(
          failure: AvatarSessionTokenFailure.invalidResponse,
        ),
      );
    }
    return _run((generation) async {
      final credential = _requireCredential();
      final path =
          '/v1/practice-sessions/'
          '${Uri.encodeComponent(practiceSessionId)}/avatar-session-token';
      final uri = _baseUri.resolve(path);
      _trustedOrigin.validateResourceUri(uri);
      validateNoSessionCredentialInUri(
        uri,
        sessionToken: credential.sessionToken,
      );

      late final AvatarSessionWireResponse response;
      try {
        response = await _transport
            .send(
              AvatarSessionWireRequest(
                method: 'POST',
                uri: uri,
                headers: <String, String>{
                  HttpHeaders.acceptHeader: ContentType.json.mimeType,
                  HttpHeaders.authorizationHeader: bearerAuthorizationValue(
                    credential.sessionToken,
                  ),
                  HttpHeaders.cacheControlHeader: 'no-store',
                },
                maximumResponseBytes: _maximumJsonBytes,
              ),
            )
            .timeout(_timeout);
      } on AvatarSessionTokenException {
        rethrow;
      } on TimeoutException {
        throw const AvatarSessionTokenException(
          failure: AvatarSessionTokenFailure.network,
          retryable: true,
        );
      } on IOException {
        throw const AvatarSessionTokenException(
          failure: AvatarSessionTokenFailure.network,
          retryable: true,
        );
      } catch (_) {
        throw const AvatarSessionTokenException(
          failure: AvatarSessionTokenFailure.network,
          retryable: true,
        );
      }

      try {
        _requireGeneration(generation);
        if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
          throw const AvatarSessionTokenException(
            failure: AvatarSessionTokenFailure.cancelled,
          );
        }
        if (response.statusCode == HttpStatus.unauthorized) {
          await _invalidateSession(
            expectedSessionToken: credential.sessionToken,
            expectedGeneration: credential.generation,
          );
        }
        _requireStatus(response);
        final contentType = _header(
          response.headers,
          HttpHeaders.contentTypeHeader,
        );
        if (_header(response.headers, HttpHeaders.cacheControlHeader) !=
                'no-store' ||
            contentType == null ||
            contentType.split(';').first.trim().toLowerCase() !=
                ContentType.json.mimeType) {
          throw const AvatarSessionTokenException(
            failure: AvatarSessionTokenFailure.invalidResponse,
          );
        }
        return _decodeGrant(response.body);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<void> clearAccountState() {
    final existing = _cleanupFuture;
    if (existing != null) {
      return existing;
    }
    final cleanup = _performCleanup();
    _cleanupFuture = cleanup;
    return cleanup.whenComplete(() {
      if (identical(_cleanupFuture, cleanup)) {
        _cleanupFuture = null;
      }
    });
  }

  Future<void> _performCleanup() async {
    if (_disposed) {
      return;
    }
    final cleanupGeneration = ++_accountGeneration;
    if (_ownsTransport) {
      _transport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
    if (_disposed || cleanupGeneration != _accountGeneration) {
      return;
    }
    if (_ownsTransport) {
      _transport = _transportFactory();
    }
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _accountGeneration++;
    if (_ownsTransport) {
      _transport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
  }

  Future<T> _run<T>(Future<T> Function(int generation) operation) async {
    final cleanup = _cleanupFuture;
    if (cleanup != null) {
      await cleanup;
    }
    if (_disposed) {
      throw const AvatarSessionTokenException(
        failure: AvatarSessionTokenFailure.cancelled,
      );
    }
    final generation = _accountGeneration;
    final completion = Completer<void>();
    final marker = completion.future;
    _inFlight.add(marker);
    try {
      return await operation(generation);
    } finally {
      _inFlight.remove(marker);
      completion.complete();
    }
  }

  AuthSessionCredential _requireCredential() {
    final credential = _credentialProvider();
    if (credential == null) {
      throw const AvatarSessionTokenException(
        failure: AvatarSessionTokenFailure.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    }
    return credential;
  }

  void _requireGeneration(int generation) {
    if (_disposed || generation != _accountGeneration) {
      throw const AvatarSessionTokenException(
        failure: AvatarSessionTokenFailure.cancelled,
      );
    }
  }

  AvatarSessionGrant _decodeGrant(Uint8List bytes) {
    try {
      final decoded = jsonDecode(utf8.decode(bytes));
      if (decoded is! Map<String, Object?> ||
          !_hasExactKeys(decoded, const {
            'app_id',
            'avatar_id',
            'session_token',
            'region',
            'expires_at',
            'audio_format',
          })) {
        throw const FormatException();
      }
      final appId = _strictResourceId(decoded['app_id']);
      final avatarId = _strictResourceId(decoded['avatar_id']);
      final sessionToken = _strictSecret(decoded['session_token']);
      final rawRegion = decoded['region'];
      final rawExpiresAt = decoded['expires_at'];
      final rawAudioFormat = decoded['audio_format'];
      if (rawRegion is! String ||
          rawExpiresAt is! String ||
          rawAudioFormat is! Map<String, Object?> ||
          !_hasExactKeys(rawAudioFormat, const {
            'encoding',
            'sample_rate_hz',
            'channels',
          })) {
        throw const FormatException();
      }
      final region = AvatarRegion.fromWireName(rawRegion);
      if (region == null ||
          !_rfc3339UtcPattern.hasMatch(rawExpiresAt) ||
          rawAudioFormat['encoding'] !=
              AvatarAudioFormat.pcmS16le24kMono.encoding ||
          rawAudioFormat['sample_rate_hz'] !=
              AvatarAudioFormat.pcmS16le24kMono.sampleRateHz ||
          rawAudioFormat['channels'] !=
              AvatarAudioFormat.pcmS16le24kMono.channels) {
        throw const FormatException();
      }
      final expiresAt = DateTime.parse(rawExpiresAt);
      if (!expiresAt.isUtc) {
        throw const FormatException();
      }
      final remaining = expiresAt.difference(_clock().toUtc());
      if (remaining <= Duration.zero ||
          remaining > _maximumGrantLifetime + _clockSkew) {
        throw const FormatException();
      }
      return AvatarSessionGrant(
        appId: appId,
        avatarId: avatarId,
        sessionToken: sessionToken,
        region: region,
        expiresAt: expiresAt,
        audioFormat: AvatarAudioFormat.pcmS16le24kMono,
      );
    } catch (error) {
      if (error is AvatarSessionTokenException) {
        rethrow;
      }
      throw const AvatarSessionTokenException(
        failure: AvatarSessionTokenFailure.invalidResponse,
      );
    }
  }

  void _requireStatus(AvatarSessionWireResponse response) {
    if (response.statusCode == HttpStatus.ok) {
      return;
    }
    final failure =
        response.statusCode >= HttpStatus.internalServerError &&
            response.statusCode <= 599
        ? AvatarSessionTokenFailure.unavailable
        : switch (response.statusCode) {
            HttpStatus.unauthorized =>
              AvatarSessionTokenFailure.authenticationRequired,
            HttpStatus.forbidden => AvatarSessionTokenFailure.forbidden,
            HttpStatus.notFound => AvatarSessionTokenFailure.notFound,
            HttpStatus.conflict => AvatarSessionTokenFailure.conflict,
            HttpStatus.tooManyRequests => AvatarSessionTokenFailure.unavailable,
            _ => AvatarSessionTokenFailure.invalidResponse,
          };
    throw AvatarSessionTokenException(
      failure: failure,
      statusCode: response.statusCode,
      retryable:
          failure == AvatarSessionTokenFailure.unavailable ||
          response.statusCode >= 500,
    );
  }

  static bool _hasExactKeys(Map<String, Object?> object, Set<String> expected) {
    return object.length == expected.length &&
        expected.every(object.containsKey);
  }

  static String _strictResourceId(Object? value) {
    if (value is! String || !_resourceIdPattern.hasMatch(value)) {
      throw const FormatException();
    }
    return value;
  }

  static String _strictSecret(Object? value) {
    if (value is! String ||
        value.isEmpty ||
        value.length > 8192 ||
        value.trim() != value ||
        value.codeUnits.any((unit) => unit <= 0x20 || unit > 0x7e)) {
      throw const FormatException();
    }
    return value;
  }
}

final class IoAvatarSessionWireTransport implements AvatarSessionWireTransport {
  IoAvatarSessionWireTransport({required Duration timeout})
    : _timeout = timeout,
      _client = HttpClient()..connectionTimeout = timeout;

  final Duration _timeout;
  final HttpClient _client;

  @override
  Future<AvatarSessionWireResponse> send(
    AvatarSessionWireRequest request,
  ) async {
    HttpClientRequest? nativeRequest;
    try {
      nativeRequest = await _client
          .openUrl(request.method, request.uri)
          .timeout(_timeout);
      nativeRequest.followRedirects = false;
      request.headers.forEach(nativeRequest.headers.set);
      nativeRequest.contentLength = 0;
      final response = await nativeRequest.close().timeout(
        _timeout,
        onTimeout: () {
          nativeRequest?.abort();
          throw TimeoutException('Avatar session response timed out.');
        },
      );
      if (response.contentLength > request.maximumResponseBytes) {
        nativeRequest.abort();
        throw const AvatarSessionTokenException(
          failure: AvatarSessionTokenFailure.invalidResponse,
        );
      }
      final builder = BytesBuilder(copy: false);
      var length = 0;
      await for (final chunk in response.timeout(_timeout)) {
        length += chunk.length;
        if (length > request.maximumResponseBytes) {
          nativeRequest.abort();
          throw const AvatarSessionTokenException(
            failure: AvatarSessionTokenFailure.invalidResponse,
          );
        }
        builder.add(chunk);
      }
      final headers = <String, String>{};
      response.headers.forEach((name, values) {
        headers[name.toLowerCase()] = values.join(',');
      });
      return AvatarSessionWireResponse(
        statusCode: response.statusCode,
        body: builder.takeBytes(),
        headers: headers,
      );
    } on TimeoutException {
      nativeRequest?.abort();
      rethrow;
    }
  }

  @override
  void close({bool force = false}) {
    _client.close(force: force);
  }
}

String? _header(Map<String, String> headers, String name) {
  final lowerName = name.toLowerCase();
  for (final entry in headers.entries) {
    if (entry.key.toLowerCase() == lowerName) {
      return entry.value.trim().toLowerCase();
    }
  }
  return null;
}

void _zero(Uint8List bytes) {
  bytes.fillRange(0, bytes.length, 0);
}

DateTime _utcNow() => DateTime.now().toUtc();
