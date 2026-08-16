import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';

import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_image_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';

final class AgentImageWireRequest {
  const AgentImageWireRequest({
    required this.method,
    required this.uri,
    required this.headers,
    required this.maximumResponseBytes,
    this.body,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final int maximumResponseBytes;
  final Uint8List? body;
}

final class AgentImageWireResponse {
  const AgentImageWireResponse({
    required this.statusCode,
    required this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final Uint8List body;
  final Map<String, String> headers;
}

abstract interface class AgentImageWireTransport {
  Future<AgentImageWireResponse> send(AgentImageWireRequest request);

  void close({bool force = false});
}

final class WireAgentImageClient
    implements AgentImageClient, AgentMessageImageClient {
  factory WireAgentImageClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    AgentImageWireTransport? transport,
    Duration requestTimeout = const Duration(seconds: 75),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    AgentImageWireTransport createTransport() =>
        IoAgentImageWireTransport(requestTimeout: requestTimeout);
    return WireAgentImageClient._(
      baseUri,
      TrustedIdentityHttpOrigin(baseUri),
      credentialProvider,
      invalidateSession,
      transport ?? createTransport(),
      transport == null,
      createTransport,
    );
  }

  WireAgentImageClient._(
    this._baseUri,
    this._trustedOrigin,
    this._credentialProvider,
    this._invalidateSession,
    this._transport,
    this._ownsTransport,
    this._transportFactory,
  );

  static const _maximumJsonBytes = 1024 * 1024;

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  AgentImageWireTransport _transport;
  final bool _ownsTransport;
  final AgentImageWireTransport Function() _transportFactory;
  int _accountGeneration = 0;
  final Set<Future<void>> _inFlight = <Future<void>>{};

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
    final pending = List<Future<void>>.of(_inFlight);
    if (_ownsTransport) {
      _transport.close(force: true);
    }
    await Future.wait(pending);
    if (_ownsTransport) {
      _transport = _transportFactory();
    }
  }

  @override
  Future<AgentImageAsset> uploadImage({
    required String threadId,
    required AgentLocalImage image,
    required String idempotencyKey,
  }) async {
    _requireUuid(threadId);
    _requireClientIdentity(idempotencyKey);
    if (!_supportedContentType(image.contentType) ||
        image.sizeBytes < 1 ||
        image.sizeBytes > agentMaximumImageBytes) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidRequest,
      );
    }
    return _run((generation) async {
      final response = await _send(
        generation: generation,
        method: 'POST',
        path: '/v1/agent-threads/${Uri.encodeComponent(threadId)}/image-assets',
        headers: <String, String>{
          HttpHeaders.contentTypeHeader: image.contentType,
          HttpHeaders.acceptHeader: ContentType.json.mimeType,
          'Idempotency-Key': idempotencyKey,
        },
        body: image.bytes,
      );
      _requireStatus(response, const <int>{
        HttpStatus.created,
        HttpStatus.accepted,
      });
      final asset = _decodeAsset(response.body);
      return asset;
    });
  }

  @override
  Future<AgentMessageImageContent> getMessageImageContent({
    required String imageAssetId,
  }) async {
    _requireUuid(imageAssetId);
    return _run((generation) async {
      final response = await _send(
        generation: generation,
        method: 'GET',
        path:
            '/v1/agent-image-assets/${Uri.encodeComponent(imageAssetId)}/content',
        headers: <String, String>{
          HttpHeaders.acceptHeader: ContentType.json.mimeType,
        },
      );
      _requireStatus(response, const <int>{HttpStatus.ok});
      return _decodeContent(response.body);
    });
  }

  @override
  Future<void> deleteImage({required String imageAssetId}) async {
    _requireUuid(imageAssetId);
    return _run((generation) async {
      final response = await _send(
        generation: generation,
        method: 'DELETE',
        path: '/v1/agent-image-assets/${Uri.encodeComponent(imageAssetId)}',
        headers: <String, String>{
          HttpHeaders.acceptHeader: ContentType.json.mimeType,
        },
      );
      _requireStatus(response, const <int>{HttpStatus.noContent});
      if (response.body.isNotEmpty) {
        throw _invalidResponse();
      }
    });
  }

  Future<AgentImageWireResponse> _send({
    required int generation,
    required String method,
    required String path,
    required Map<String, String> headers,
    Uint8List? body,
  }) async {
    _requireCurrent(generation);
    final credential = _credentialProvider();
    if (credential == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
        errorCode: 'authentication_required',
      );
    }
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    late AgentImageWireResponse response;
    try {
      response = await _transport.send(
        AgentImageWireRequest(
          method: method,
          uri: uri,
          headers: <String, String>{
            ...headers,
            HttpHeaders.authorizationHeader: bearerAuthorizationValue(
              credential.sessionToken,
            ),
          },
          body: body,
          maximumResponseBytes: _maximumJsonBytes,
        ),
      );
    } on TimeoutException {
      throw _networkFailure;
    } on SocketException {
      throw _networkFailure;
    } on HttpException {
      throw _networkFailure;
    } on IOException {
      throw _networkFailure;
    }
    _requireCurrent(generation);
    if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
      throw const AgentClientOperationCancelled();
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
  }

  Future<T> _run<T>(Future<T> Function(int generation) operation) async {
    final generation = _accountGeneration;
    final completer = Completer<void>();
    _inFlight.add(completer.future);
    try {
      return await operation(generation);
    } finally {
      _inFlight.remove(completer.future);
      completer.complete();
    }
  }

  void _requireCurrent(int generation) {
    if (generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }

  void _requireStatus(AgentImageWireResponse response, Set<int> expected) {
    if (expected.contains(response.statusCode)) {
      return;
    }
    throw _responseFailure(response);
  }
}

final class IoAgentImageWireTransport implements AgentImageWireTransport {
  IoAgentImageWireTransport({
    this._requestTimeout = const Duration(seconds: 75),
    HttpClient? httpClient,
  }) : _httpClient = httpClient ?? HttpClient();

  final Duration _requestTimeout;
  final HttpClient _httpClient;

  @override
  Future<AgentImageWireResponse> send(AgentImageWireRequest request) async {
    final httpRequest = await _httpClient
        .openUrl(request.method, request.uri)
        .timeout(_requestTimeout);
    httpRequest.followRedirects = false;
    request.headers.forEach(httpRequest.headers.set);
    if (request.body case final body?) {
      httpRequest.contentLength = body.length;
      httpRequest.add(body);
    }
    final response = await httpRequest.close().timeout(_requestTimeout);
    final builder = BytesBuilder(copy: false);
    var total = 0;
    await for (final chunk in response.timeout(_requestTimeout)) {
      total += chunk.length;
      if (total > request.maximumResponseBytes) {
        throw const HttpException('response exceeds configured limit');
      }
      builder.add(chunk);
    }
    final headers = <String, String>{};
    response.headers.forEach((name, values) {
      headers[name] = values.join(',');
    });
    return AgentImageWireResponse(
      statusCode: response.statusCode,
      body: builder.takeBytes(),
      headers: headers,
    );
  }

  @override
  void close({bool force = false}) {
    _httpClient.close(force: force);
  }
}

AgentImageAsset _decodeAsset(Uint8List bytes) {
  final object = _jsonObject(bytes);
  const required = <String>{
    'image_asset_id',
    'content_type',
    'size_bytes',
    'width',
    'height',
    'status',
    'created_at',
  };
  if (!object.keys.toSet().containsAll(required) ||
      object.keys.any(
        (key) => !required.contains(key) && key != 'attached_at',
      )) {
    throw _invalidResponse();
  }
  final status = switch (object['status']) {
    'staged' => AgentImageAssetStatus.staged,
    'ready' => AgentImageAssetStatus.ready,
    'deleting' => AgentImageAssetStatus.deleting,
    _ => throw _invalidResponse(),
  };
  final contentType = object['content_type'];
  final size = object['size_bytes'];
  final width = object['width'];
  final height = object['height'];
  if (contentType is! String ||
      !_supportedContentType(contentType) ||
      size is! int ||
      size < 1 ||
      size > agentMaximumImageBytes ||
      width is! int ||
      width < 1 ||
      height is! int ||
      height < 1) {
    throw _invalidResponse();
  }
  return AgentImageAsset(
    id: _jsonUuid(object['image_asset_id']),
    contentType: contentType,
    sizeBytes: size,
    width: width,
    height: height,
    status: status,
    createdAt: _jsonDateTime(object['created_at']),
    attachedAt: object['attached_at'] == null
        ? null
        : _jsonDateTime(object['attached_at']),
  );
}

AgentMessageImageContent _decodeContent(Uint8List bytes) {
  final object = _jsonObject(bytes);
  if (object.length != 2 ||
      !object.containsKey('content_url') ||
      !object.containsKey('expires_at')) {
    throw _invalidResponse();
  }
  final rawUrl = object['content_url'];
  if (rawUrl is! String) {
    throw _invalidResponse();
  }
  final url = Uri.tryParse(rawUrl);
  final expiresAt = _jsonDateTime(object['expires_at']);
  if (url == null ||
      url.scheme != 'https' ||
      url.host.isEmpty ||
      url.userInfo.isNotEmpty ||
      url.fragment.isNotEmpty ||
      !expiresAt.isAfter(DateTime.now().toUtc())) {
    throw _invalidResponse();
  }
  return AgentMessageImageContent(url: url, expiresAt: expiresAt);
}

Map<String, Object?> _jsonObject(Uint8List bytes) {
  try {
    final decoded = jsonDecode(utf8.decode(bytes));
    if (decoded is! Map<String, Object?>) {
      throw const FormatException();
    }
    return decoded;
  } catch (_) {
    throw _invalidResponse();
  }
}

String _jsonUuid(Object? value) {
  if (value is! String) {
    throw _invalidResponse();
  }
  _requireUuid(value);
  return value;
}

DateTime _jsonDateTime(Object? value) {
  if (value is! String) {
    throw _invalidResponse();
  }
  final parsed = DateTime.tryParse(value);
  if (parsed == null || !value.endsWith('Z')) {
    throw _invalidResponse();
  }
  return parsed.toUtc();
}

void _requireUuid(String value) {
  if (!RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
  ).hasMatch(value)) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

void _requireClientIdentity(String value) {
  if (value.length < 8 ||
      value.length > 128 ||
      !RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]*$').hasMatch(value)) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

bool _supportedContentType(String value) =>
    value == 'image/jpeg' || value == 'image/png' || value == 'image/webp';

AgentClientException _responseFailure(AgentImageWireResponse response) {
  String? code;
  try {
    final root = _jsonObject(response.body);
    final error = root['error'];
    if (error is Map<String, Object?> && error['code'] is String) {
      code = error['code']! as String;
    }
  } catch (_) {
    code = null;
  }
  code = switch ((response.statusCode, code)) {
    (HttpStatus.badRequest, 'invalid_request') => 'invalid_request',
    (HttpStatus.badRequest, 'invalid_image') => 'invalid_image',
    (HttpStatus.badRequest, 'unsupported_image_format') =>
      'unsupported_image_format',
    (HttpStatus.requestEntityTooLarge, 'image_too_large') => 'image_too_large',
    (HttpStatus.unauthorized, 'authentication_required') =>
      'authentication_required',
    (HttpStatus.notFound, 'resource_not_found') => 'resource_not_found',
    (HttpStatus.conflict, 'idempotency_key_conflict') =>
      'idempotency_key_conflict',
    (HttpStatus.conflict, 'resource_conflict') => 'resource_conflict',
    (HttpStatus.tooManyRequests, 'rate_limited') => 'rate_limited',
    (>= 500, 'provider_unavailable') => 'provider_unavailable',
    (>= 500, 'internal_error') => 'internal_error',
    _ => null,
  };
  final kind = switch (response.statusCode) {
    HttpStatus.badRequest ||
    HttpStatus.requestEntityTooLarge => AgentClientFailureKind.invalidRequest,
    HttpStatus.unauthorized => AgentClientFailureKind.authenticationRequired,
    HttpStatus.notFound => AgentClientFailureKind.notFound,
    HttpStatus.conflict => AgentClientFailureKind.conflict,
    HttpStatus.tooManyRequests => AgentClientFailureKind.rateLimited,
    >= 500 => AgentClientFailureKind.server,
    _ => AgentClientFailureKind.unexpected,
  };
  return AgentClientException(
    kind: kind,
    statusCode: response.statusCode,
    errorCode: code,
    retryable:
        kind == AgentClientFailureKind.rateLimited ||
        kind == AgentClientFailureKind.server,
  );
}

AgentClientException _invalidResponse() => const AgentClientException(
  kind: AgentClientFailureKind.invalidResponse,
  retryable: true,
);

const _networkFailure = AgentClientException(
  kind: AgentClientFailureKind.network,
  retryable: true,
);
