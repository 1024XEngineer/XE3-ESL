// 本文件实现 Resume REST 契约、认证隔离、Multipart 上传与严格响应解码。

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';

import 'resume_client.dart';
import 'resume_models.dart';

/// WireResumeClient 通过冻结的 `/v1/resumes` 契约访问简历服务。
final class WireResumeClient implements ResumeClient {
  static const _maxResponseBytes = 1024 * 1024;
  factory WireResumeClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    HttpClient? httpClient,
    Duration requestTimeout = const Duration(seconds: 30),
  }) => WireResumeClient._(
    baseUri,
    credentialProvider,
    invalidateSession,
    httpClient ?? HttpClient(),
    requestTimeout,
  );

  WireResumeClient._(
    this._baseUri,
    this._credentialProvider,
    this._invalidateSession,
    this._httpClient,
    this.requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  final Uri _baseUri;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  final HttpClient _httpClient;
  final Duration requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  int _accountGeneration = 0;

  @override
  Future<List<ResumeItem>> list() async {
    final response = await _send('GET', '/v1/resumes?limit=3');
    _expect(response, HttpStatus.ok);
    final root = _decodeObject(response.body);
    final items = root['items'];
    if (items is! List<Object?> || items.length > 3) {
      throw const ResumeException(ResumeFailureKind.invalidResponse);
    }
    try {
      return List<ResumeItem>.unmodifiable(
        items.map((item) {
          if (item is! Map<String, Object?>) {
            throw const FormatException();
          }
          return ResumeItem.fromJson(item);
        }),
      );
    } on FormatException {
      throw const ResumeException(ResumeFailureKind.invalidResponse);
    }
  }

  @override
  Future<ResumeItem> create({
    required String title,
    required ResumePdfFile file,
  }) async {
    final multipart = _multipart(<String, String>{'title': title}, file);
    final response = await _send(
      'POST',
      '/v1/resumes',
      headers: <String, String>{
        'Idempotency-Key': _idempotencyKey(),
        HttpHeaders.contentTypeHeader: multipart.contentType,
      },
      body: multipart.body,
    );
    _expect(response, HttpStatus.accepted);
    return _decodeResume(response.body);
  }

  @override
  Future<ResumeDetail> get(String resumeId) async {
    final response = await _send('GET', '/v1/resumes/${_segment(resumeId)}');
    _expect(response, HttpStatus.ok);
    try {
      return ResumeDetail.fromJson(_decodeObject(response.body));
    } on FormatException {
      throw const ResumeException(ResumeFailureKind.invalidResponse);
    }
  }

  @override
  Future<ResumeItem> rename(ResumeItem resume, String title) async {
    final response = await _sendJson(
      'PATCH',
      '/v1/resumes/${_segment(resume.id)}',
      <String, Object?>{'title': title, 'expected_version': resume.version},
    );
    _expect(response, HttpStatus.ok);
    return _decodeResume(response.body);
  }

  @override
  Future<ResumeItem> replace(ResumeItem resume, ResumePdfFile file) async {
    final multipart = _multipart(<String, String>{
      'expected_version': '${resume.version}',
    }, file);
    final response = await _send(
      'PUT',
      '/v1/resumes/${_segment(resume.id)}/file',
      headers: <String, String>{
        'Idempotency-Key': _idempotencyKey(),
        HttpHeaders.contentTypeHeader: multipart.contentType,
      },
      body: multipart.body,
    );
    _expect(response, HttpStatus.accepted);
    return _decodeResume(response.body);
  }

  @override
  Future<void> delete(ResumeItem resume) async {
    final response = await _send(
      'DELETE',
      '/v1/resumes/${_segment(resume.id)}?expected_version=${resume.version}',
      headers: <String, String>{'Idempotency-Key': _idempotencyKey()},
    );
    _expect(response, HttpStatus.noContent);
  }

  @override
  Future<ResumeItem> retryParse(ResumeItem resume) async {
    final response = await _send(
      'POST',
      '/v1/resumes/${_segment(resume.id)}/parse-retries',
      headers: <String, String>{'Idempotency-Key': _idempotencyKey()},
    );
    _expect(response, HttpStatus.accepted);
    return _decodeResume(response.body);
  }

  @override
  Future<ResumeDetail> updateContent(
    ResumeDetail detail,
    ResumeContent content,
  ) async {
    final response = await _sendJson(
      'PATCH',
      '/v1/resumes/${_segment(detail.resume.id)}/content',
      <String, Object?>{
        'content': content.toJson(),
        'expected_version': detail.resume.version,
      },
    );
    _expect(response, HttpStatus.ok);
    return get(detail.resume.id);
  }

  @override
  Future<Uri> getContentUrl(String resumeId) async {
    final response = await _send(
      'GET',
      '/v1/resumes/${_segment(resumeId)}/content-url',
    );
    _expect(response, HttpStatus.ok);
    final value = _decodeObject(response.body)['url'];
    final uri = value is String ? Uri.tryParse(value) : null;
    if (uri == null ||
        !uri.hasScheme ||
        (uri.scheme != 'https' && uri.scheme != 'http')) {
      throw const ResumeException(ResumeFailureKind.invalidResponse);
    }
    return uri;
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }

  Future<_Response> _sendJson(
    String method,
    String path,
    Map<String, Object?> body,
  ) {
    return _send(
      method,
      path,
      headers: <String, String>{
        'Idempotency-Key': _idempotencyKey(),
        HttpHeaders.contentTypeHeader: 'application/json',
      },
      body: utf8.encode(jsonEncode(body)),
    );
  }

  Future<_Response> _send(
    String method,
    String path, {
    Map<String, String> headers = const <String, String>{},
    List<int>? body,
  }) async {
    final credential = _credentialProvider();
    if (credential == null) {
      throw const ResumeException(ResumeFailureKind.authenticationRequired);
    }
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    try {
      final request = await _httpClient
          .openUrl(method, uri)
          .timeout(requestTimeout);
      request.followRedirects = false;
      request.headers.set(HttpHeaders.acceptHeader, 'application/json');
      request.headers.set(
        HttpHeaders.authorizationHeader,
        bearerAuthorizationValue(credential.sessionToken),
      );
      headers.forEach(request.headers.set);
      if (body != null) {
        request.add(body);
      }
      final raw = await request.close().timeout(requestTimeout);
      final responseBytes = await raw
          .fold<List<int>>(<int>[], (all, chunk) {
            if (all.length + chunk.length > _maxResponseBytes) {
              throw const ResumeException(ResumeFailureKind.invalidResponse);
            }
            all.addAll(chunk);
            return all;
          })
          .timeout(requestTimeout);
      final responseBody = utf8.decode(responseBytes);
      if (generation != _accountGeneration ||
          !isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const ResumeException(ResumeFailureKind.superseded);
      }
      if (raw.statusCode == HttpStatus.unauthorized) {
        await _invalidateSession(
          expectedSessionToken: credential.sessionToken,
          expectedGeneration: credential.generation,
        );
      }
      return _Response(raw.statusCode, responseBody);
    } on ResumeException {
      rethrow;
    } on FormatException {
      throw const ResumeException(ResumeFailureKind.invalidResponse);
    } on TimeoutException {
      throw const ResumeException(ResumeFailureKind.network, retryable: true);
    } on IOException {
      throw const ResumeException(ResumeFailureKind.network, retryable: true);
    }
  }

  void _expect(_Response response, int expected) {
    if (response.statusCode == expected) {
      return;
    }
    final kind = switch (response.statusCode) {
      HttpStatus.badRequest => ResumeFailureKind.invalidRequest,
      HttpStatus.unauthorized => ResumeFailureKind.authenticationRequired,
      HttpStatus.notFound => ResumeFailureKind.notFound,
      HttpStatus.conflict =>
        _isLimitError(response.body)
            ? ResumeFailureKind.limitReached
            : ResumeFailureKind.conflict,
      >= 500 => ResumeFailureKind.server,
      _ => ResumeFailureKind.invalidResponse,
    };
    throw ResumeException(
      kind,
      retryable:
          kind == ResumeFailureKind.server || kind == ResumeFailureKind.network,
    );
  }

  ResumeItem _decodeResume(String body) {
    try {
      return ResumeItem.fromJson(_decodeObject(body));
    } on FormatException {
      throw const ResumeException(ResumeFailureKind.invalidResponse);
    }
  }
}

Map<String, Object?> _decodeObject(String body) {
  try {
    final decoded = jsonDecode(body);
    if (decoded is! Map<String, Object?>) {
      throw const FormatException();
    }
    return decoded;
  } on FormatException {
    throw const ResumeException(ResumeFailureKind.invalidResponse);
  }
}

String _segment(String value) => Uri.encodeComponent(value);

String _idempotencyKey() {
  final random = Random.secure();
  return 'resume-${DateTime.now().microsecondsSinceEpoch}-${random.nextInt(1 << 32)}';
}

bool _isLimitError(String body) {
  return body.contains('resume_limit') || body.contains('limit_reached');
}

_Multipart _multipart(Map<String, String> fields, ResumePdfFile file) {
  final boundary = 'speakup-${DateTime.now().microsecondsSinceEpoch}';
  final bytes = <int>[];
  void text(String value) => bytes.addAll(utf8.encode(value));
  for (final entry in fields.entries) {
    text('--$boundary\r\n');
    text('Content-Disposition: form-data; name="${entry.key}"\r\n\r\n');
    text('${entry.value}\r\n');
  }
  final safeName = file.name.replaceAll(RegExp(r'[\r\n"]'), '_');
  text('--$boundary\r\n');
  text('Content-Disposition: form-data; name="file"; filename="$safeName"\r\n');
  text('Content-Type: application/pdf\r\n\r\n');
  bytes.addAll(file.bytes);
  text('\r\n--$boundary--\r\n');
  return _Multipart('multipart/form-data; boundary=$boundary', bytes);
}

final class _Multipart {
  const _Multipart(this.contentType, this.body);
  final String contentType;
  final List<int> body;
}

final class _Response {
  const _Response(this.statusCode, this.body);
  final int statusCode;
  final String body;
}
