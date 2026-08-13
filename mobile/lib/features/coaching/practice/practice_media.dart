import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/coaching/practice/practice_client_error.dart';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/authenticated_web_socket.dart';
import 'package:speakup/identity/network/transport_security.dart';

typedef PracticeMediaClock = DateTime Function();
typedef PracticeMediaWireTransportFactory =
    PracticeMediaWireTransport Function();

abstract interface class PracticeMediaClient {
  Future<Uint8List> loadQuestionSpeech(String speechPath);

  Future<Uint8List> loadRecording(String audioAssetId);

  Future<void> deleteRecording(String audioAssetId);

  Future<void> clearAccountState();

  Future<void> dispose();
}

abstract interface class PracticeQuestionSpeechClient {
  Stream<Uint8List> streamQuestionSpeech(String questionId);
}

final class PracticeMediaWireRequest {
  const PracticeMediaWireRequest({
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

final class PracticeMediaWireResponse {
  const PracticeMediaWireResponse({
    required this.statusCode,
    required this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final Uint8List body;
  final Map<String, String> headers;
}

abstract interface class PracticeMediaWireTransport {
  Future<PracticeMediaWireResponse> send(PracticeMediaWireRequest request);

  void close({bool force = false});
}

/// Fetches ephemeral practice audio without retaining signed URLs or bytes.
///
/// API requests use the authenticated, same-origin transport. A recording's
/// signed URL is consumed by a separate request with no App Session header,
/// then only the resulting WAV bytes cross the player boundary.
final class WirePracticeMediaClient
    implements PracticeMediaClient, PracticeQuestionSpeechClient {
  factory WirePracticeMediaClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    PracticeMediaWireTransport? apiTransport,
    PracticeMediaWireTransport? signedAudioTransport,
    PracticeMediaWireTransportFactory? transportFactory,
    AuthenticatedWebSocketConnector? questionSpeechConnector,
    PracticeMediaClock? clock,
    Duration timeout = const Duration(seconds: 30),
  }) {
    if (timeout <= Duration.zero) {
      throw ArgumentError.value(timeout, 'timeout', 'must be positive');
    }
    final createTransport =
        transportFactory ?? IoPracticeMediaWireTransport.new;
    return WirePracticeMediaClient._(
      baseUri,
      TrustedIdentityHttpOrigin(baseUri),
      credentialProvider,
      invalidateSession,
      apiTransport ?? createTransport(),
      signedAudioTransport ?? createTransport(),
      apiTransport == null,
      signedAudioTransport == null,
      createTransport,
      SessionAuthenticatedWebSocketConnector(
        connector:
            questionSpeechConnector ??
            IoAuthenticatedWebSocketConnector(
              protocols: const <String>[_questionSpeechProtocol],
            ),
        credentialProvider: credentialProvider,
        invalidateSession: invalidateSession,
        trustedBaseUri: _practiceWebSocketBaseUri(baseUri),
      ),
      clock ?? _utcNow,
      timeout,
    );
  }

  WirePracticeMediaClient._(
    this._baseUri,
    this._trustedOrigin,
    this._credentialProvider,
    this._invalidateSession,
    this._apiTransport,
    this._signedAudioTransport,
    this._ownsApiTransport,
    this._ownsSignedAudioTransport,
    this._transportFactory,
    this._questionSpeechConnector,
    this._clock,
    this._timeout,
  );

  static const _maximumAudioBytes = 7400000;
  static const _maximumJsonBytes = 64 * 1024;
  static const _maximumPlaybackLifetime = Duration(minutes: 2);
  static const _maximumLocalClockSkew = Duration(seconds: 30);
  static const _questionSpeechProtocol = 'speakup.practice-question-speech.v1';

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  PracticeMediaWireTransport _apiTransport;
  PracticeMediaWireTransport _signedAudioTransport;
  final bool _ownsApiTransport;
  final bool _ownsSignedAudioTransport;
  final PracticeMediaWireTransportFactory _transportFactory;
  final SessionAuthenticatedWebSocketConnector _questionSpeechConnector;
  final PracticeMediaClock _clock;
  final Duration _timeout;

  int _accountGeneration = 0;
  bool _disposed = false;
  final Set<Future<void>> _inFlight = <Future<void>>{};

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) {
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'GET',
        path: _requireRelativeApiPath(speechPath),
        accept: 'audio/wav',
        maximumResponseBytes: _maximumAudioBytes,
      );
      try {
        _requireStatus(response, const {HttpStatus.ok});
        _requireWave(response);
        return Uint8List.fromList(response.body);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Stream<Uint8List> streamQuestionSpeech(String questionId) async* {
    final id = _requireResourceId(questionId);
    final generation = _accountGeneration;
    final uri = _practiceWebSocketBaseUri(
      _baseUri,
    ).resolve('/v1/voice-questions/${Uri.encodeComponent(id)}/speech/realtime');
    SessionAuthenticatedWebSocketConnection? connection;
    StreamIterator<dynamic>? messages;
    var receivedBytes = 0;
    try {
      connection = await _questionSpeechConnector.connect(uri: uri);
      _requireGeneration(generation);
      messages = StreamIterator<dynamic>(connection.socket);
      final ready = await _nextPracticeSpeechMessage(messages);
      _requirePracticeSpeechEvent(ready, 'stream.ready');
      while (true) {
        final message = await _nextPracticeSpeechMessage(messages);
        if (message is String) {
          _requirePracticeSpeechEvent(message, 'stream.completed');
          if (receivedBytes == 0) {
            throw const PracticeClientException(
              kind: PracticeClientFailureKind.invalidResponse,
            );
          }
          break;
        }
        if (message is! List<int> || message.isEmpty || message.length.isOdd) {
          throw const PracticeClientException(
            kind: PracticeClientFailureKind.invalidResponse,
          );
        }
        receivedBytes += message.length;
        if (receivedBytes > _maximumAudioBytes) {
          throw const PracticeClientException(
            kind: PracticeClientFailureKind.invalidResponse,
          );
        }
        yield Uint8List.fromList(message);
      }
    } on AuthenticatedWebSocketException catch (error) {
      throw PracticeClientException(
        kind: error.invalidatesAuthentication
            ? PracticeClientFailureKind.authenticationRequired
            : PracticeClientFailureKind.network,
        retryable: !error.invalidatesAuthentication,
      );
    } on FormatException {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.invalidResponse,
      );
    } finally {
      await messages?.cancel();
      await connection?.socket.close();
    }
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) {
    return _run((generation) async {
      final id = _requireResourceId(audioAssetId);
      final credential = _requireCredential();
      final metadata = await _sendApiWithCredential(
        generation: generation,
        credential: credential,
        method: 'GET',
        path: '/v1/audio-assets/${Uri.encodeComponent(id)}/playback',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      late final _PlaybackCapability capability;
      try {
        _requireStatus(metadata, const {HttpStatus.ok});
        if (_header(metadata.headers, HttpHeaders.cacheControlHeader) !=
                'no-store' ||
            _header(
                  metadata.headers,
                  HttpHeaders.contentTypeHeader,
                )?.split(';').first.trim().toLowerCase() !=
                ContentType.json.mimeType) {
          throw _invalidResponse();
        }
        capability = _decodePlayback(metadata.body, metadata.headers);
      } finally {
        _zero(metadata.body);
      }
      validateNoSessionCredentialInUri(
        capability.uri,
        sessionToken: credential.sessionToken,
      );
      _requireGeneration(generation);
      if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const PracticeClientOperationCancelled();
      }

      // Deliberately no Authorization header: the signed HTTPS URL is the
      // short-lived capability for this one request.
      final audio = await _send(
        transport: _signedAudioTransport,
        request: PracticeMediaWireRequest(
          method: 'GET',
          uri: capability.uri,
          headers: const <String, String>{
            HttpHeaders.acceptHeader: 'audio/wav',
            HttpHeaders.cacheControlHeader: 'no-store',
          },
          maximumResponseBytes: _maximumAudioBytes,
        ),
      );
      try {
        _requireGeneration(generation);
        if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
          throw const PracticeClientOperationCancelled();
        }
        _requireSignedAudioStatus(audio);
        _requireWave(audio);
        return Uint8List.fromList(audio.body);
      } finally {
        _zero(audio.body);
      }
    });
  }

  @override
  Future<void> deleteRecording(String audioAssetId) {
    return _run((generation) async {
      final id = _requireResourceId(audioAssetId);
      final response = await _sendApi(
        generation: generation,
        method: 'DELETE',
        path: '/v1/audio-assets/${Uri.encodeComponent(id)}',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const {HttpStatus.noContent});
        if (response.body.isNotEmpty) {
          throw _invalidResponse();
        }
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<void> clearAccountState() async {
    if (_disposed) {
      return;
    }
    final cleanupGeneration = ++_accountGeneration;
    if (_ownsApiTransport) {
      _apiTransport.close(force: true);
    }
    if (_ownsSignedAudioTransport) {
      _signedAudioTransport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
    if (_disposed || cleanupGeneration != _accountGeneration) {
      return;
    }
    if (_ownsApiTransport) {
      _apiTransport = _transportFactory();
    }
    if (_ownsSignedAudioTransport) {
      _signedAudioTransport = _transportFactory();
    }
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _accountGeneration++;
    if (_ownsApiTransport) {
      _apiTransport.close(force: true);
    }
    if (_ownsSignedAudioTransport) {
      _signedAudioTransport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
  }

  Future<PracticeMediaWireResponse> _sendApi({
    required int generation,
    required String method,
    required String path,
    required String accept,
    required int maximumResponseBytes,
  }) {
    return _sendApiWithCredential(
      generation: generation,
      credential: _requireCredential(),
      method: method,
      path: path,
      accept: accept,
      maximumResponseBytes: maximumResponseBytes,
    );
  }

  Future<PracticeMediaWireResponse> _sendApiWithCredential({
    required int generation,
    required AuthSessionCredential credential,
    required String method,
    required String path,
    required String accept,
    required int maximumResponseBytes,
  }) async {
    _requireGeneration(generation);
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    final response = await _send(
      transport: _apiTransport,
      request: PracticeMediaWireRequest(
        method: method,
        uri: uri,
        headers: <String, String>{
          HttpHeaders.acceptHeader: accept,
          HttpHeaders.authorizationHeader: bearerAuthorizationValue(
            credential.sessionToken,
          ),
          HttpHeaders.cacheControlHeader: 'no-store',
        },
        maximumResponseBytes: maximumResponseBytes,
      ),
    );
    try {
      _requireGeneration(generation);
      if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const PracticeClientOperationCancelled();
      }
      if (response.statusCode == HttpStatus.unauthorized) {
        unawaited(
          _invalidateSession(
            expectedSessionToken: credential.sessionToken,
            expectedGeneration: credential.generation,
          ),
        );
      }
      return response;
    } catch (_) {
      _zero(response.body);
      rethrow;
    }
  }

  Future<PracticeMediaWireResponse> _send({
    required PracticeMediaWireTransport transport,
    required PracticeMediaWireRequest request,
  }) async {
    Future<PracticeMediaWireResponse>? pending;
    try {
      pending = transport.send(request);
      return await pending.timeout(_timeout);
    } on TimeoutException {
      final lateResponse = pending;
      if (lateResponse != null) {
        unawaited(
          lateResponse.then<void>(
            (response) => _zero(response.body),
            onError: (Object _, StackTrace _) {},
          ),
        );
      }
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        errorCode: 'practice_media_request_timed_out',
        retryable: true,
      );
    } on PracticeClientException {
      rethrow;
    } on IOException {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.unexpected,
      );
    }
  }

  Future<T> _run<T>(Future<T> Function(int generation) operation) {
    if (_disposed) {
      return Future<T>.error(const PracticeClientOperationCancelled());
    }
    final generation = _accountGeneration;
    final completion = Completer<void>();
    _inFlight.add(completion.future);
    return Future<T>.sync(() => operation(generation)).whenComplete(() {
      _inFlight.remove(completion.future);
      completion.complete();
    });
  }

  AuthSessionCredential _requireCredential() {
    final credential = _credentialProvider();
    if (credential == null) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    }
    return credential;
  }

  void _requireGeneration(int generation) {
    if (_disposed || generation != _accountGeneration) {
      throw const PracticeClientOperationCancelled();
    }
  }

  _PlaybackCapability _decodePlayback(
    Uint8List bytes,
    Map<String, String> headers,
  ) {
    try {
      final decoded = jsonDecode(utf8.decode(bytes));
      if (decoded is! Map<String, Object?> ||
          decoded.keys.toSet().difference(const {
            'playback_url',
            'expires_at',
          }).isNotEmpty ||
          !decoded.keys.toSet().containsAll(const {
            'playback_url',
            'expires_at',
          })) {
        throw const FormatException();
      }
      final rawUrl = decoded['playback_url'];
      final rawExpiry = decoded['expires_at'];
      if (rawUrl is! String ||
          rawUrl.trim() != rawUrl ||
          rawUrl.isEmpty ||
          rawUrl.length > 4096 ||
          rawExpiry is! String ||
          rawExpiry.length > 64) {
        throw const FormatException();
      }
      final uri = Uri.parse(rawUrl);
      _validateSignedHttpsUri(uri);
      final expiresAt = DateTime.parse(rawExpiry);
      if (!expiresAt.isUtc) {
        throw const FormatException();
      }
      final localNow = _clock().toUtc();
      final serverDateValue = _headerRaw(headers, HttpHeaders.dateHeader);
      DateTime? serverNow;
      if (serverDateValue != null) {
        try {
          serverNow = HttpDate.parse(serverDateValue).toUtc();
        } on FormatException {
          throw const FormatException();
        }
      }
      final referenceNow = serverNow ?? localNow;
      final remaining = expiresAt.difference(referenceNow);
      final exceedsLifetime = serverNow == null
          ? remaining > _maximumPlaybackLifetime + _maximumLocalClockSkew
          : remaining >= _maximumPlaybackLifetime + const Duration(seconds: 1);
      if (remaining <= Duration.zero || exceedsLifetime) {
        throw const FormatException();
      }
      return _PlaybackCapability(uri: uri);
    } catch (_) {
      throw _invalidResponse();
    }
  }

  void _requireWave(PracticeMediaWireResponse response) {
    final contentType = _header(
      response.headers,
      HttpHeaders.contentTypeHeader,
    );
    final bytes = response.body;
    if (contentType == null ||
        contentType.split(';').first.trim().toLowerCase() != 'audio/wav' ||
        bytes.length < 44 ||
        bytes.length > _maximumAudioBytes ||
        bytes[0] != 0x52 ||
        bytes[1] != 0x49 ||
        bytes[2] != 0x46 ||
        bytes[3] != 0x46 ||
        bytes[8] != 0x57 ||
        bytes[9] != 0x41 ||
        bytes[10] != 0x56 ||
        bytes[11] != 0x45) {
      throw _invalidResponse();
    }
  }

  void _requireStatus(PracticeMediaWireResponse response, Set<int> expected) {
    if (expected.contains(response.statusCode)) {
      return;
    }
    late final String code;
    late final bool explicitRetryable;
    try {
      final root = jsonDecode(utf8.decode(response.body));
      if (root is! Map<String, Object?> ||
          root.keys.toSet().difference(const {'error'}).isNotEmpty ||
          root.keys.length != 1 ||
          root['error'] is! Map<String, Object?>) {
        throw const FormatException();
      }
      final error = root['error']! as Map<String, Object?>;
      const required = {'code', 'message', 'retryable', 'correlation_id'};
      if (!error.keys.toSet().containsAll(required) ||
          error.keys.toSet().difference(const {
            ...required,
            'details',
          }).isNotEmpty) {
        throw const FormatException();
      }
      final rawCode = error['code'];
      final message = error['message'];
      final retryable = error['retryable'];
      final correlationId = error['correlation_id'];
      if (rawCode is! String ||
          rawCode.isEmpty ||
          rawCode.length > 64 ||
          message is! String ||
          message.isEmpty ||
          message.length > 512 ||
          retryable is! bool ||
          correlationId is! String ||
          correlationId.isEmpty ||
          correlationId.length > 128) {
        throw const FormatException();
      }
      if (error['details'] case final details?) {
        if (details is! List<Object?> || details.length > 32) {
          throw const FormatException();
        }
        for (final value in details) {
          if (value is! Map<String, Object?> ||
              value.keys.toSet().difference(const {
                'field',
                'reason',
              }).isNotEmpty ||
              value.keys.length != 2 ||
              value['field'] is! String ||
              (value['field']! as String).isEmpty ||
              (value['field']! as String).length > 128 ||
              value['reason'] is! String ||
              (value['reason']! as String).isEmpty ||
              (value['reason']! as String).length > 256) {
            throw const FormatException();
          }
        }
      }
      code = rawCode;
      explicitRetryable = retryable;
    } catch (_) {
      throw _invalidResponse();
    }
    throw PracticeClientException(
      kind: switch (response.statusCode) {
        HttpStatus.badRequest => PracticeClientFailureKind.invalidRequest,
        HttpStatus.unauthorized =>
          PracticeClientFailureKind.authenticationRequired,
        HttpStatus.notFound => PracticeClientFailureKind.notFound,
        HttpStatus.conflict => PracticeClientFailureKind.conflict,
        HttpStatus.tooManyRequests => PracticeClientFailureKind.rateLimited,
        >= 500 => PracticeClientFailureKind.server,
        _ => PracticeClientFailureKind.unexpected,
      },
      statusCode: response.statusCode,
      errorCode: code,
      retryable: explicitRetryable,
    );
  }

  void _requireSignedAudioStatus(PracticeMediaWireResponse response) {
    if (response.statusCode == HttpStatus.ok) {
      return;
    }
    final retryable =
        response.statusCode == HttpStatus.unauthorized ||
        response.statusCode == HttpStatus.forbidden ||
        response.statusCode == HttpStatus.requestTimeout ||
        response.statusCode == HttpStatus.tooManyRequests ||
        response.statusCode >= 500;
    throw PracticeClientException(
      kind: retryable
          ? PracticeClientFailureKind.network
          : PracticeClientFailureKind.invalidResponse,
      statusCode: response.statusCode,
      errorCode: 'recording_playback_capability_rejected',
      retryable: retryable,
    );
  }
}

final class IoPracticeMediaWireTransport implements PracticeMediaWireTransport {
  IoPracticeMediaWireTransport({HttpClient? httpClient})
    : _httpClient = httpClient ?? HttpClient();

  final HttpClient _httpClient;

  @override
  Future<PracticeMediaWireResponse> send(
    PracticeMediaWireRequest request,
  ) async {
    final ioRequest = await _httpClient.openUrl(request.method, request.uri);
    ioRequest.followRedirects = false;
    request.headers.forEach(ioRequest.headers.set);
    final response = await ioRequest.close();
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response) {
      if (bytes.length + chunk.length > request.maximumResponseBytes) {
        throw const HttpException('Practice media response is too large.');
      }
      bytes.add(chunk);
    }
    final headers = <String, String>{};
    response.headers.forEach((name, values) {
      headers[name] = values.join(',');
    });
    return PracticeMediaWireResponse(
      statusCode: response.statusCode,
      body: bytes.takeBytes(),
      headers: headers,
    );
  }

  @override
  void close({bool force = false}) => _httpClient.close(force: force);
}

final class _PlaybackCapability {
  const _PlaybackCapability({required this.uri});

  final Uri uri;
}

void _zero(Uint8List bytes) {
  if (bytes.isNotEmpty) {
    bytes.fillRange(0, bytes.length, 0);
  }
}

String _requireRelativeApiPath(String value) {
  if (value.trim() != value ||
      value.isEmpty ||
      value.length > 512 ||
      !value.startsWith('/')) {
    throw ArgumentError.value(value, 'speechPath', 'Invalid API path.');
  }
  final uri = Uri.parse(value);
  if (uri.hasScheme ||
      uri.hasAuthority ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment) {
    throw ArgumentError.value(value, 'speechPath', 'Invalid API path.');
  }
  return value;
}

String _requireResourceId(String value) {
  if (value.trim() != value ||
      value.isEmpty ||
      value.length > 128 ||
      value.codeUnits.any((unit) => unit < 0x21 || unit == 0x7f)) {
    throw ArgumentError.value(value, 'audioAssetId', 'Invalid resource ID.');
  }
  return value;
}

void _validateSignedHttpsUri(Uri uri) {
  final host = uri.host;
  if (uri.scheme != 'https' ||
      host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasFragment ||
      host.endsWith('.') ||
      host.codeUnits.any((unit) => unit > 0x7f) ||
      uri.authority.contains('%')) {
    throw const FormatException();
  }
  if (uri.hasPort && (uri.port < 1 || uri.port > 65535)) {
    throw const FormatException();
  }
}

String? _header(Map<String, String> headers, String name) {
  return _headerRaw(headers, name)?.trim().toLowerCase();
}

String? _headerRaw(Map<String, String> headers, String name) {
  return headers.entries
      .where((entry) => entry.key.toLowerCase() == name.toLowerCase())
      .map((entry) => entry.value)
      .firstOrNull;
}

PracticeClientException _invalidResponse() {
  return const PracticeClientException(
    kind: PracticeClientFailureKind.invalidResponse,
    retryable: true,
  );
}

Future<dynamic> _nextPracticeSpeechMessage(
  StreamIterator<dynamic> messages,
) async {
  if (!await messages.moveNext()) {
    throw const PracticeClientException(
      kind: PracticeClientFailureKind.network,
      retryable: true,
    );
  }
  return messages.current;
}

void _requirePracticeSpeechEvent(dynamic message, String expected) {
  if (message is! String) {
    throw const PracticeClientException(
      kind: PracticeClientFailureKind.invalidResponse,
    );
  }
  final decoded = jsonDecode(message);
  if (decoded is! Map<String, dynamic> || decoded['type'] != expected) {
    throw const PracticeClientException(
      kind: PracticeClientFailureKind.invalidResponse,
    );
  }
  final data = decoded['data'];
  if (data is! Map<String, dynamic>) {
    throw const PracticeClientException(
      kind: PracticeClientFailureKind.invalidResponse,
    );
  }
  if (expected == 'stream.ready' &&
      (data['content_type'] != 'audio/pcm' ||
          data['sample_rate'] != 24000 ||
          data['channel_count'] != 1 ||
          data['bits_per_sample'] != 16)) {
    throw const PracticeClientException(
      kind: PracticeClientFailureKind.invalidResponse,
    );
  }
}

Uri _practiceWebSocketBaseUri(Uri httpBaseUri) {
  final scheme = switch (httpBaseUri.scheme) {
    'https' => 'wss',
    'http' => 'ws',
    _ => throw ArgumentError('Practice media base URI must use HTTP or HTTPS.'),
  };
  return httpBaseUri.replace(scheme: scheme);
}

DateTime _utcNow() => DateTime.now().toUtc();
