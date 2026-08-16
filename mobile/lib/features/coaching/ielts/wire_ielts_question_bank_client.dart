import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank_codec.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireIeltsQuestionBankClient implements IeltsQuestionBankClient {
  factory WireIeltsQuestionBankClient({
    required Uri baseUri,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WireIeltsQuestionBankClient._(
      baseUri,
      transport ?? _IoQuestionBankTransport(_maximumBodyBytes),
      requestTimeout,
    );
  }

  WireIeltsQuestionBankClient._(
    this._baseUri,
    this._transport,
    this._requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  static const _maximumBodyBytes = 256 * 1024;

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<IeltsQuestionBank> getQuestionBank() async {
    final uri = _baseUri.resolve('/v1/ielts-speaking/question-bank');
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
      throw const IeltsQuestionBankClientException(
        kind: IeltsQuestionBankFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const IeltsQuestionBankClientException(
        kind: IeltsQuestionBankFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const IeltsQuestionBankClientException(
        kind: IeltsQuestionBankFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const IeltsQuestionBankClientException(
        kind: IeltsQuestionBankFailureKind.network,
        retryable: true,
      );
    } on _QuestionBankTransportResponseException {
      throw _invalidResponse();
    }
    if (response.statusCode == HttpStatus.ok) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw _invalidResponse();
      }
      try {
        return decodeIeltsQuestionBank(jsonDecode(response.body));
      } on FormatException {
        throw _invalidResponse();
      } on IeltsQuestionBankWireFormatException {
        throw _invalidResponse();
      }
    }
    if (response.statusCode == HttpStatus.notFound ||
        response.statusCode >= 500) {
      throw IeltsQuestionBankClientException(
        kind: IeltsQuestionBankFailureKind.unavailable,
        statusCode: response.statusCode,
        retryable: response.statusCode >= 500,
      );
    }
    throw IeltsQuestionBankClientException(
      kind: IeltsQuestionBankFailureKind.invalidResponse,
      statusCode: response.statusCode,
    );
  }
}

IeltsQuestionBankClientException _invalidResponse() =>
    const IeltsQuestionBankClientException(
      kind: IeltsQuestionBankFailureKind.invalidResponse,
    );

final class _QuestionBankTransportResponseException implements Exception {
  const _QuestionBankTransportResponseException();
}

final class _IoQuestionBankTransport implements IdentityHttpTransport {
  _IoQuestionBankTransport(this.maximumBodyBytes) : _httpClient = HttpClient();

  final int maximumBodyBytes;
  final HttpClient _httpClient;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    if (body != null && bodyBytes != null) {
      throw ArgumentError('Only one request body may be provided.');
    }
    final request = await _httpClient.openUrl(method, uri);
    request.followRedirects = false;
    headers.forEach(request.headers.set);
    if (bodyBytes != null) {
      request.add(bodyBytes);
    } else if (body != null) {
      request.write(body);
    }
    final response = await request.close();
    if (response.contentLength > maximumBodyBytes) {
      throw const _QuestionBankTransportResponseException();
    }
    final bytes = BytesBuilder(copy: false);
    var received = 0;
    await for (final chunk in response) {
      received += chunk.length;
      if (received > maximumBodyBytes) {
        throw const _QuestionBankTransportResponseException();
      }
      bytes.add(chunk);
    }
    late final String responseBody;
    try {
      responseBody = utf8.decode(bytes.takeBytes());
    } on FormatException {
      throw const _QuestionBankTransportResponseException();
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
