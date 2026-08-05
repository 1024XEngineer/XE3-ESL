import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/agent/conversation/agent_message_meme_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireAgentMessageMemeClient implements AgentMessageMemeClient {
  factory WireAgentMessageMemeClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    Duration timeout = const Duration(seconds: 20),
  }) => WireAgentMessageMemeClient._(baseUri, credentialProvider, timeout);

  WireAgentMessageMemeClient._(
    this._baseUri,
    this._credentialProvider,
    this._timeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri),
      _client = HttpClient()..connectionTimeout = _timeout;

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final Duration _timeout;
  final HttpClient _client;

  @override
  Future<Uint8List> getMemeContent({
    required String contentPath,
    required int expectedSizeBytes,
    required String expectedContentType,
  }) async {
    if (!_validContentPath(contentPath) ||
        expectedSizeBytes < 1 ||
        expectedSizeBytes > 20 * 1024 * 1024 ||
        !_supportedContentType(expectedContentType)) {
      throw ArgumentError('Meme content request is invalid.');
    }
    final credential = _credentialProvider();
    if (credential == null) {
      throw const HttpException('Authentication is required.');
    }
    final uri = _baseUri.resolve(contentPath);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    HttpClientRequest? request;
    try {
      request = await _client.getUrl(uri).timeout(_timeout);
      request.followRedirects = false;
      request.headers.set(HttpHeaders.acceptHeader, expectedContentType);
      request.headers.set(
        HttpHeaders.authorizationHeader,
        bearerAuthorizationValue(credential.sessionToken),
      );
      final response = await request.close().timeout(_timeout);
      if (response.statusCode != HttpStatus.ok ||
          response.headers.contentType?.mimeType != expectedContentType ||
          (response.contentLength >= 0 &&
              response.contentLength != expectedSizeBytes)) {
        throw const HttpException('Meme response is invalid.');
      }
      final builder = BytesBuilder(copy: false);
      var length = 0;
      await for (final chunk in response.timeout(_timeout)) {
        length += chunk.length;
        if (length > expectedSizeBytes) {
          request.abort();
          throw const HttpException('Meme response exceeds expected size.');
        }
        builder.add(chunk);
      }
      if (length != expectedSizeBytes ||
          !isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const HttpException('Meme response is stale or incomplete.');
      }
      return builder.takeBytes();
    } on TimeoutException {
      request?.abort();
      rethrow;
    }
  }
}

bool _validContentPath(String value) => RegExp(
  r'^/v1/agent-message-memes/[0-9a-fA-F-]{36}/content$',
).hasMatch(value);

bool _supportedContentType(String value) =>
    value == 'image/gif' ||
    value == 'image/jpeg' ||
    value == 'image/png' ||
    value == 'image/webp';
