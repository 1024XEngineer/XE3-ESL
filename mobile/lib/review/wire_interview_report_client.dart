import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_client.dart';
import 'package:speakup/review/interview_report_decoder.dart';

final class WireInterviewReportClient implements InterviewReportClient {
  factory WireInterviewReportClient({
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
        transport ?? _IoInterviewReportHttpTransport(requestTimeout);
    return WireInterviewReportClient._(
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

  WireInterviewReportClient._(
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
  Future<InterviewReportEnvelope> getReport(String practiceSessionId) async {
    if (!_validPracticeSessionId(practiceSessionId)) {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.invalidResponse,
      );
    }
    final generation = _accountGeneration;
    final uri = _baseUri.replace(
      path:
          '/v1/practice-sessions/${Uri.encodeComponent(practiceSessionId)}/interview-report',
      query: null,
      fragment: null,
    );
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
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.superseded,
      );
    } on StateError {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.network,
        retryable: true,
      );
    }
    if (generation != _accountGeneration) {
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.superseded,
      );
    }
    switch (response.statusCode) {
      case HttpStatus.ok:
        try {
          final result = decodeInterviewReportJson(response.body);
          if (result.practiceSessionId != practiceSessionId) {
            throw const InterviewReportDecodeException();
          }
          return result;
        } on InterviewReportDecodeException {
          throw const InterviewReportException(
            kind: InterviewReportFailureKind.invalidResponse,
          );
        }
      case HttpStatus.unauthorized:
        throw const InterviewReportException(
          kind: InterviewReportFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.notFound:
        throw const InterviewReportException(
          kind: InterviewReportFailureKind.notFound,
          statusCode: HttpStatus.notFound,
        );
      case HttpStatus.conflict:
        throw const InterviewReportException(
          kind: InterviewReportFailureKind.conflict,
          statusCode: HttpStatus.conflict,
        );
      default:
        if (response.statusCode >= 500) {
          throw InterviewReportException(
            kind: InterviewReportFailureKind.server,
            statusCode: response.statusCode,
            retryable: true,
          );
        }
        throw InterviewReportException(
          kind: InterviewReportFailureKind.invalidResponse,
          statusCode: response.statusCode,
        );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

bool _validPracticeSessionId(String value) =>
    value.isNotEmpty &&
    value.length <= 128 &&
    value == value.trim() &&
    RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value);

final class _IoInterviewReportHttpTransport implements IdentityHttpTransport {
  const _IoInterviewReportHttpTransport(this._requestTimeout);

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
        if (response.contentLength > _maximumReportResponseBytes) {
          request!.abort();
          throw const InterviewReportException(
            kind: InterviewReportFailureKind.invalidResponse,
          );
        }
        final bytes = await _readBoundedResponse(response, request!);
        late final String responseBody;
        try {
          responseBody = utf8.decode(bytes, allowMalformed: false);
        } on FormatException {
          throw const InterviewReportException(
            kind: InterviewReportFailureKind.invalidResponse,
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
    if (length > _maximumReportResponseBytes) {
      request.abort();
      throw const InterviewReportException(
        kind: InterviewReportFailureKind.invalidResponse,
      );
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

const _maximumReportResponseBytes = 1024 * 1024;
