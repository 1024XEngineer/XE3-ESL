import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report_decoder.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';
import 'package:speakup/features/coaching/review/practice_report_status_client.dart';
import 'package:speakup/features/coaching/review/practice_report_status_decoder.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WirePracticeReportStatusClient
    implements PracticeReportStatusClient, PracticeReportRegenerationClient {
  factory WirePracticeReportStatusClient({
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
        transport ?? _IoPracticeReportStatusHttpTransport(requestTimeout);
    return WirePracticeReportStatusClient._(
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

  WirePracticeReportStatusClient._(
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
  Future<PracticeReportStatus> getStatus(String practiceSessionId) async {
    if (!_validPracticeSessionId(practiceSessionId)) {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.invalidRequest,
      );
    }
    final response = await _get(
      '/v1/practice-sessions/${Uri.encodeComponent(practiceSessionId)}/report',
    );
    switch (response.statusCode) {
      case HttpStatus.ok:
        try {
          final result = decodePracticeReportStatusJson(response.body);
          if (result.practiceSessionId != practiceSessionId) {
            throw const PracticeReportStatusDecodeException();
          }
          return result;
        } on PracticeReportStatusDecodeException {
          throw const PracticeReportStatusException(
            kind: PracticeReportStatusFailureKind.invalidResponse,
          );
        }
      case HttpStatus.unauthorized:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.badRequest:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.invalidRequest,
          statusCode: HttpStatus.badRequest,
        );
      case HttpStatus.notFound:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.notFound,
          statusCode: HttpStatus.notFound,
          retryable: true,
        );
      case HttpStatus.conflict:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.conflict,
          statusCode: HttpStatus.conflict,
          retryable: true,
        );
      default:
        throw _unexpectedStatus(response.statusCode);
    }
  }

  @override
  Future<EvaluationReport> getReadyReport(PracticeReportRef reportRef) async {
    if (!_validReportRef(reportRef)) {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.invalidRequest,
      );
    }
    final response = await _get(reportRef.href);
    switch (response.statusCode) {
      case HttpStatus.ok:
        try {
          final value = jsonDecode(response.body);
          final report = decodeEvaluationReport(value);
          if (report.id != reportRef.reportId) {
            throw const EvaluationReportDecodeException();
          }
          return report;
        } on FormatException {
          throw const PracticeReportStatusException(
            kind: PracticeReportStatusFailureKind.invalidResponse,
          );
        } on EvaluationReportDecodeException {
          throw const PracticeReportStatusException(
            kind: PracticeReportStatusFailureKind.invalidResponse,
          );
        }
      case HttpStatus.unauthorized:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.notFound:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.notFound,
          statusCode: HttpStatus.notFound,
          retryable: true,
        );
      default:
        throw _unexpectedStatus(response.statusCode);
    }
  }

  @override
  Future<void> regenerateReport(PracticeReportStatus status) async {
    final evaluationId = status.evaluationId;
    if (status.evaluationStatus != PracticeReportEvaluationStatus.failed ||
        status.stableFailure?.retryable != true ||
        !_ieltsPracticeMode(status.practiceMode) ||
        evaluationId == null ||
        !_validEvaluationId(evaluationId)) {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.invalidRequest,
      );
    }
    final response = await _request(
      '/v1/evaluations/${Uri.encodeComponent(evaluationId)}/re-evaluate',
      method: 'POST',
      body: jsonEncode(<String, Object>{
        'channels': const <String>['SCENE'],
        'scene_strategy_ref': 'ielts-speaking-full-mock-shadow/v1',
        'pipeline_version': 'evaluation-pipeline-shadow/v1',
      }),
    );
    switch (response.statusCode) {
      case HttpStatus.ok:
      case HttpStatus.accepted:
        return;
      case HttpStatus.unauthorized:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.badRequest:
      case HttpStatus.unprocessableEntity:
        throw PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.invalidRequest,
          statusCode: response.statusCode,
        );
      case HttpStatus.notFound:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.notFound,
          statusCode: HttpStatus.notFound,
        );
      case HttpStatus.conflict:
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.conflict,
          statusCode: HttpStatus.conflict,
          retryable: true,
        );
      default:
        throw _unexpectedStatus(response.statusCode);
    }
  }

  Future<IdentityHttpResponse> _get(String path) => _request(path);

  Future<IdentityHttpResponse> _request(
    String path, {
    String method = 'GET',
    String? body,
  }) async {
    final generation = _accountGeneration;
    final uri = _baseUri.replace(path: path, query: null, fragment: null);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    try {
      final response = await _transport
          .send(
            method: method,
            uri: uri,
            headers: <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
              if (body != null)
                HttpHeaders.contentTypeHeader: 'application/json',
            },
            body: body,
          )
          .timeout(_requestTimeout);
      if (generation != _accountGeneration) {
        throw const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.superseded,
        );
      }
      return response;
    } on AuthSessionSupersededException {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.superseded,
      );
    } on StateError {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.network,
        retryable: true,
      );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

bool _ieltsPracticeMode(PracticeMode mode) =>
    mode == PracticeMode.part1 ||
    mode == PracticeMode.part2 ||
    mode == PracticeMode.part3 ||
    mode == PracticeMode.fullMock;

PracticeReportStatusException _unexpectedStatus(int statusCode) {
  if (statusCode >= 500) {
    return PracticeReportStatusException(
      kind: PracticeReportStatusFailureKind.server,
      statusCode: statusCode,
      retryable: true,
    );
  }
  return PracticeReportStatusException(
    kind: PracticeReportStatusFailureKind.invalidResponse,
    statusCode: statusCode,
  );
}

bool _validPracticeSessionId(String value) =>
    value.isNotEmpty &&
    value.length <= 128 &&
    value == value.trim() &&
    RegExp(r'^[A-Za-z0-9][A-Za-z0-9_-]*$').hasMatch(value);

bool _validEvaluationId(String value) => RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
).hasMatch(value);

bool _validReportRef(PracticeReportRef ref) =>
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
    ).hasMatch(ref.reportId) &&
    ref.href == '/v1/evaluation-reports/${ref.reportId}';

final class _IoPracticeReportStatusHttpTransport
    implements IdentityHttpTransport {
  const _IoPracticeReportStatusHttpTransport(this._requestTimeout);

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
          throw const PracticeReportStatusException(
            kind: PracticeReportStatusFailureKind.invalidResponse,
          );
        }
        final bytes = await _readBoundedResponse(response, request!);
        late final String responseBody;
        try {
          responseBody = utf8.decode(bytes, allowMalformed: false);
        } on FormatException {
          throw const PracticeReportStatusException(
            kind: PracticeReportStatusFailureKind.invalidResponse,
          );
        }
        return IdentityHttpResponse(
          statusCode: response.statusCode,
          body: responseBody,
          headers: const <String, String>{},
        );
      }();
      return await operation.timeout(_requestTimeout);
    } on TimeoutException {
      request?.abort();
      rethrow;
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
  var count = 0;
  await for (final chunk in response) {
    count += chunk.length;
    if (count > _maximumResponseBytes) {
      request.abort();
      throw const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.invalidResponse,
      );
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

const _maximumResponseBytes = 2 * 1024 * 1024;
