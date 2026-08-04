import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';

final class WireIeltsSpeakingReportClient implements IeltsSpeakingReportClient {
  factory WireIeltsSpeakingReportClient({
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
        transport ?? _IoIeltsSpeakingReportHttpTransport(requestTimeout);
    return WireIeltsSpeakingReportClient._(
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

  WireIeltsSpeakingReportClient._(
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
  Future<IeltsSpeakingReportEnvelope> getReport(
    String practiceSessionId,
  ) async {
    if (!_validPracticeSessionId(practiceSessionId)) {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.invalidRequest,
      );
    }
    final generation = _accountGeneration;
    final uri = _baseUri.replace(
      path:
          '/v1/practice-sessions/${Uri.encodeComponent(practiceSessionId)}/ielts-speaking-report',
      query: null,
      fragment: null,
    );
    final response = await _send(uri);
    _requireCurrent(generation);
    switch (response.statusCode) {
      case HttpStatus.ok:
        try {
          final result = decodeIeltsSpeakingReportJson(response.body);
          if (result.practiceSessionId != practiceSessionId) {
            throw const IeltsSpeakingReportDecodeException();
          }
          return result;
        } on IeltsSpeakingReportDecodeException {
          throw const IeltsSpeakingReportException(
            kind: IeltsSpeakingReportFailureKind.invalidResponse,
          );
        }
      case HttpStatus.unauthorized:
        throw const IeltsSpeakingReportException(
          kind: IeltsSpeakingReportFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.notFound:
        throw const IeltsSpeakingReportException(
          kind: IeltsSpeakingReportFailureKind.notFound,
          statusCode: HttpStatus.notFound,
        );
      case HttpStatus.conflict:
        throw const IeltsSpeakingReportException(
          kind: IeltsSpeakingReportFailureKind.conflict,
          statusCode: HttpStatus.conflict,
        );
      default:
        throw _unexpectedStatus(response.statusCode);
    }
  }

  Future<IdentityHttpResponse> _send(Uri uri) async {
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    try {
      return await _transport
          .send(
            method: 'GET',
            uri: uri,
            headers: const <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
            },
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.superseded,
      );
    } on StateError {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.network,
        retryable: true,
      );
    }
  }

  void _requireCurrent(int generation) {
    if (generation != _accountGeneration) {
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.superseded,
      );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

IeltsSpeakingReportException _unexpectedStatus(int statusCode) {
  if (statusCode >= 500) {
    return IeltsSpeakingReportException(
      kind: IeltsSpeakingReportFailureKind.server,
      statusCode: statusCode,
      retryable: true,
    );
  }
  return IeltsSpeakingReportException(
    kind: IeltsSpeakingReportFailureKind.invalidResponse,
    statusCode: statusCode,
  );
}

bool _validPracticeSessionId(String value) =>
    value.isNotEmpty &&
    value.length <= 128 &&
    value == value.trim() &&
    RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value);

final class _IoIeltsSpeakingReportHttpTransport
    implements IdentityHttpTransport {
  const _IoIeltsSpeakingReportHttpTransport(this._requestTimeout);

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
        if (response.contentLength > _maximumResponseBytes) {
          request!.abort();
          throw const IeltsSpeakingReportException(
            kind: IeltsSpeakingReportFailureKind.invalidResponse,
          );
        }
        final bytes = await _readBoundedResponse(response, request!);
        late final String responseBody;
        try {
          responseBody = utf8.decode(bytes, allowMalformed: false);
        } on FormatException {
          throw const IeltsSpeakingReportException(
            kind: IeltsSpeakingReportFailureKind.invalidResponse,
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
    if (length > _maximumResponseBytes) {
      request.abort();
      throw const IeltsSpeakingReportException(
        kind: IeltsSpeakingReportFailureKind.invalidResponse,
      );
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

const _maximumResponseBytes = 8 * 1024 * 1024;
