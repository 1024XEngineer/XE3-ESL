import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_client.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_decoder.dart';

final class WireSpeechFeedbackClient implements SpeechFeedbackClient {
  factory WireSpeechFeedbackClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    final rawTransport =
        transport ?? _IoSpeechFeedbackHttpTransport(requestTimeout);
    return WireSpeechFeedbackClient._(
      baseUri,
      SessionAuthenticatedHttpTransport(
        transport: rawTransport,
        credentialProvider: credentialProvider,
        invalidateSession: invalidateSession,
        trustedBaseUri: baseUri,
      ),
      requestTimeout,
    );
  }

  WireSpeechFeedbackClient._(
    this._baseUri,
    this._transport,
    this._requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  int _accountGeneration = 0;

  @override
  Future<SpeechFeedback> getFeedback(String statusUrl) async {
    if (!validSpeechFeedbackStatusUrl(statusUrl)) {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.invalidRequest,
      );
    }
    final generation = _accountGeneration;
    final uri = _baseUri.replace(path: statusUrl, query: null, fragment: null);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: 'GET',
            uri: uri,
            headers: const <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
            },
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.superseded,
      );
    } on StateError {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.network,
        retryable: true,
      );
    }
    _requireCurrent(generation);
    switch (response.statusCode) {
      case HttpStatus.ok:
        try {
          final result = decodeSpeechFeedbackJson(response.body);
          if (result.statusUrl != statusUrl) {
            throw const SpeechFeedbackDecodeException();
          }
          return result;
        } on SpeechFeedbackDecodeException {
          throw const SpeechFeedbackException(
            kind: SpeechFeedbackFailureKind.invalidResponse,
          );
        }
      case HttpStatus.badRequest:
        throw const SpeechFeedbackException(
          kind: SpeechFeedbackFailureKind.invalidRequest,
          statusCode: HttpStatus.badRequest,
        );
      case HttpStatus.unauthorized:
        throw const SpeechFeedbackException(
          kind: SpeechFeedbackFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.notFound:
        throw const SpeechFeedbackException(
          kind: SpeechFeedbackFailureKind.notFound,
          statusCode: HttpStatus.notFound,
        );
      case HttpStatus.conflict:
        throw const SpeechFeedbackException(
          kind: SpeechFeedbackFailureKind.conflict,
          statusCode: HttpStatus.conflict,
        );
      default:
        if (response.statusCode >= 500) {
          throw SpeechFeedbackException(
            kind: SpeechFeedbackFailureKind.server,
            statusCode: response.statusCode,
            retryable: true,
          );
        }
        throw SpeechFeedbackException(
          kind: SpeechFeedbackFailureKind.invalidResponse,
          statusCode: response.statusCode,
        );
    }
  }

  void _requireCurrent(int generation) {
    if (generation != _accountGeneration) {
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.superseded,
      );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

final class _IoSpeechFeedbackHttpTransport implements IdentityHttpTransport {
  const _IoSpeechFeedbackHttpTransport(this._requestTimeout);

  final Duration _requestTimeout;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    final client = HttpClient()..connectionTimeout = _requestTimeout;
    HttpClientRequest? request;
    try {
      final operation = () async {
        request = await client.openUrl(method, uri);
        request!.followRedirects = false;
        headers.forEach(request!.headers.set);
        if (body != null) {
          request!.add(utf8.encode(body));
        }
        final response = await request!.close();
        if (response.contentLength > _maximumFeedbackResponseBytes) {
          request!.abort();
          throw const SpeechFeedbackException(
            kind: SpeechFeedbackFailureKind.invalidResponse,
          );
        }
        final bytes = await _readBoundedResponse(response, request!);
        late final String responseBody;
        try {
          responseBody = utf8.decode(bytes, allowMalformed: false);
        } on FormatException {
          throw const SpeechFeedbackException(
            kind: SpeechFeedbackFailureKind.invalidResponse,
          );
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
      }();
      return await operation.timeout(_requestTimeout);
    } finally {
      client.close(force: true);
    }
  }
}

Future<Uint8List> _readBoundedResponse(
  HttpClientResponse response,
  HttpClientRequest request,
) async {
  final builder = BytesBuilder(copy: false);
  var length = 0;
  await for (final chunk in response) {
    length += chunk.length;
    if (length > _maximumFeedbackResponseBytes) {
      request.abort();
      throw const SpeechFeedbackException(
        kind: SpeechFeedbackFailureKind.invalidResponse,
      );
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

const _maximumFeedbackResponseBytes = 512 * 1024;
