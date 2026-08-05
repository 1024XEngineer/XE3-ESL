import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireSceneClient implements SceneClient {
  factory WireSceneClient({
    required Uri baseUri,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WireSceneClient._(
      baseUri,
      transport ?? _IoSceneTransport(_maximumBodyBytes),
      requestTimeout,
    );
  }

  WireSceneClient._(this._baseUri, this._transport, this._requestTimeout)
    : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  static const _maximumBodyBytes = 256 * 1024;

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<List<SceneDefinition>> listScenes() async {
    final response = await _get('/v1/scenes');
    final root = _object(_decode(response.body), required: const {'scenes'});
    final rawScenes = root['scenes'];
    if (rawScenes is! List<Object?> || rawScenes.length > 50) {
      throw _invalidResponse();
    }
    final ids = <String>{};
    final scenes = <SceneDefinition>[];
    for (final value in rawScenes) {
      final scene = _sceneDefinition(value);
      if (scene.status != SceneStatus.active || !ids.add(scene.id)) {
        throw _invalidResponse();
      }
      scenes.add(scene);
    }
    return List<SceneDefinition>.unmodifiable(scenes);
  }

  @override
  Future<SceneDefinition> getScene(String sceneId) async {
    _resourceId(sceneId);
    final response = await _get('/v1/scenes/${Uri.encodeComponent(sceneId)}');
    final scene = _sceneDefinition(_decode(response.body));
    if (scene.id != sceneId || scene.status != SceneStatus.active) {
      throw _invalidResponse();
    }
    return scene;
  }

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async {
    _resourceId(sceneId);
    final response = await _get(
      '/v1/scenes/${Uri.encodeComponent(sceneId)}/roles',
    );
    final root = _object(_decode(response.body), required: const {'roles'});
    final rawRoles = root['roles'];
    if (rawRoles is! List<Object?> ||
        rawRoles.isEmpty ||
        rawRoles.length > 50) {
      throw _invalidResponse();
    }
    final ids = <String>{};
    final roles = <RoleDefinition>[];
    for (final value in rawRoles) {
      final role = _role(value);
      if (role.sceneId != sceneId || !ids.add(role.id)) {
        throw _invalidResponse();
      }
      roles.add(role);
    }
    return List<RoleDefinition>.unmodifiable(roles);
  }

  Future<IdentityHttpResponse> _get(String path) async {
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: 'GET',
            uri: uri,
            headers: const {HttpHeaders.acceptHeader: 'application/json'},
          )
          .timeout(_requestTimeout);
    } on TimeoutException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on _SceneTransportResponseException {
      throw _invalidResponse();
    }
    if (response.statusCode == HttpStatus.ok) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw _invalidResponse();
      }
      return response;
    }
    if (response.statusCode == HttpStatus.notFound ||
        response.statusCode >= 500) {
      throw SceneClientException(
        kind: SceneClientFailureKind.unavailable,
        statusCode: response.statusCode,
        retryable: response.statusCode >= 500,
      );
    }
    throw SceneClientException(
      kind: SceneClientFailureKind.invalidResponse,
      statusCode: response.statusCode,
    );
  }
}

Object? _decode(String body) {
  try {
    return jsonDecode(body);
  } on FormatException {
    throw _invalidResponse();
  }
}

Map<String, Object?> _object(Object? value, {Set<String> required = const {}}) {
  if (value is! Map<String, Object?> ||
      value.keys.toSet().length != required.length ||
      !value.keys.toSet().containsAll(required)) {
    throw _invalidResponse();
  }
  return value;
}

SceneDefinition _sceneDefinition(Object? value) {
  try {
    return decodeSceneDefinition(value);
  } on SceneWireFormatException {
    throw _invalidResponse();
  }
}

RoleDefinition _role(Object? value) {
  try {
    return decodeRoleDefinition(value);
  } on SceneWireFormatException {
    throw _invalidResponse();
  }
}

String _resourceId(Object? value) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > 128) {
    throw _invalidResponse();
  }
  return value;
}

SceneClientException _invalidResponse() =>
    const SceneClientException(kind: SceneClientFailureKind.invalidResponse);

final class _SceneTransportResponseException implements Exception {
  const _SceneTransportResponseException();
}

final class _IoSceneTransport implements IdentityHttpTransport {
  _IoSceneTransport(this.maximumBodyBytes) : _httpClient = HttpClient();

  final int maximumBodyBytes;
  final HttpClient _httpClient;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    final request = await _httpClient.openUrl(method, uri);
    request.followRedirects = false;
    headers.forEach(request.headers.set);
    if (body != null) {
      request.write(body);
    }
    final response = await request.close();
    if (response.contentLength > maximumBodyBytes) {
      throw const _SceneTransportResponseException();
    }
    final bytes = BytesBuilder(copy: false);
    var received = 0;
    await for (final chunk in response) {
      received += chunk.length;
      if (received > maximumBodyBytes) {
        throw const _SceneTransportResponseException();
      }
      bytes.add(chunk);
    }
    late final String responseBody;
    try {
      responseBody = utf8.decode(bytes.takeBytes());
    } on FormatException {
      throw const _SceneTransportResponseException();
    }
    final responseHeaders = <String, String>{};
    response.headers.forEach((name, values) {
      responseHeaders[name] = values.join(',');
    });
    return IdentityHttpResponse(
      statusCode: response.statusCode,
      body: responseBody,
      headers: responseHeaders,
    );
  }
}
