import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/review/formal_review.dart';
import 'package:speakup/review/formal_review_decoder.dart';
import 'package:speakup/review/formal_review_presentation.dart';
import 'package:speakup/review/review_history_client.dart';

final class WireReviewHistoryClient implements ReviewHistoryClient {
  factory WireReviewHistoryClient({
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
        transport ?? _IoReviewHistoryHttpTransport(requestTimeout);
    return WireReviewHistoryClient._(
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

  WireReviewHistoryClient._(
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
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    if (limit < 1 || limit > 50 || (cursor != null && !_validCursor(cursor))) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidRequest,
      );
    }
    final generation = _accountGeneration;
    final uri = _baseUri
        .resolve('/v1/formal-reviews')
        .replace(
          queryParameters: <String, String>{
            'limit': '$limit',
            ...cursor == null
                ? const <String, String>{}
                : <String, String>{'cursor': cursor},
          },
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
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.superseded,
      );
    } on StateError {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    }
    if (generation != _accountGeneration) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.superseded,
      );
    }
    switch (response.statusCode) {
      case HttpStatus.ok:
        return _decodeHistoryPage(response.body, limit: limit);
      case HttpStatus.unauthorized:
        throw const ReviewHistoryException(
          kind: ReviewHistoryFailureKind.authenticationRequired,
          statusCode: HttpStatus.unauthorized,
        );
      case HttpStatus.badRequest:
        throw const ReviewHistoryException(
          kind: ReviewHistoryFailureKind.invalidRequest,
          statusCode: HttpStatus.badRequest,
        );
      default:
        if (response.statusCode >= 500) {
          throw ReviewHistoryException(
            kind: ReviewHistoryFailureKind.server,
            statusCode: response.statusCode,
            retryable: true,
          );
        }
        throw ReviewHistoryException(
          kind: ReviewHistoryFailureKind.invalidResponse,
          statusCode: response.statusCode,
        );
    }
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

ReviewHistoryPage _decodeHistoryPage(String body, {required int limit}) {
  late final Object? decoded;
  try {
    decoded = jsonDecode(body);
  } on FormatException {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final root = _object(
    decoded,
    required: const <String>{'items'},
    optional: const <String>{'next_cursor'},
  );
  final rawItems = root['items'];
  if (rawItems is! List<Object?> || rawItems.length > limit) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final items = <ReviewHistoryItem>[];
  final ids = <String>{};
  ReviewHistoryItem? previous;
  for (final rawItem in rawItems) {
    final item = _decodeHistoryItem(rawItem);
    if (!ids.add(item.review.id) ||
        (previous != null && !_isBefore(item, previous))) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidResponse,
      );
    }
    items.add(item);
    previous = item;
  }
  String? nextCursor;
  if (root.containsKey('next_cursor')) {
    final value = root['next_cursor'];
    if (value is! String || !_validCursor(value) || items.length != limit) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidResponse,
      );
    }
    nextCursor = value;
  }
  return ReviewHistoryPage(
    items: List<ReviewHistoryItem>.unmodifiable(items),
    nextCursor: nextCursor,
  );
}

ReviewHistoryItem _decodeHistoryItem(Object? value) {
  late final FormalReview formalReview;
  try {
    formalReview = decodeFormalReview(value);
  } on FormalReviewDecodeException {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  if (formalReview.status != FormalReviewStatus.completed ||
      formalReview.result == null ||
      formalReview.completedAt == null ||
      !_validUuid(formalReview.id)) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return ReviewHistoryItem(
    review: presentFormalReview(formalReview),
    formalReview: formalReview,
    practiceSessionId: formalReview.practiceSessionId,
    createdAt: formalReview.createdAt,
    completedAt: formalReview.completedAt!,
  );
}

Map<String, Object?> _object(
  Object? value, {
  Set<String> required = const <String>{},
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.invalidResponse,
    );
  }
  return value;
}

bool _validUuid(String value) => RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
).hasMatch(value);

bool _validCursor(String value) =>
    value.isNotEmpty &&
    value.length <= 512 &&
    RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(value);

bool _isBefore(ReviewHistoryItem item, ReviewHistoryItem boundary) =>
    item.createdAt.isBefore(boundary.createdAt) ||
    (item.createdAt == boundary.createdAt &&
        item.review.id.compareTo(boundary.review.id) < 0);

final class _IoReviewHistoryHttpTransport implements IdentityHttpTransport {
  const _IoReviewHistoryHttpTransport(this._requestTimeout);

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
        if (response.contentLength > _maxReviewHistoryResponseBytes) {
          request!.abort();
          throw const ReviewHistoryException(
            kind: ReviewHistoryFailureKind.invalidResponse,
            retryable: true,
          );
        }
        final responseBytes = await _readBoundedReviewHistoryResponse(
          response,
          request!,
        );
        late final String responseBody;
        try {
          responseBody = utf8.decode(responseBytes, allowMalformed: false);
        } on FormatException {
          request!.abort();
          throw const ReviewHistoryException(
            kind: ReviewHistoryFailureKind.invalidResponse,
            retryable: true,
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
      return await operation.timeout(
        _requestTimeout,
        onTimeout: () {
          request?.abort();
          client.close(force: true);
          throw TimeoutException('Review History HTTP request timed out.');
        },
      );
    } on TimeoutException {
      request?.abort();
      rethrow;
    } finally {
      client.close(force: true);
    }
  }
}

Future<Uint8List> _readBoundedReviewHistoryResponse(
  HttpClientResponse response,
  HttpClientRequest request,
) async {
  final builder = BytesBuilder(copy: false);
  var length = 0;
  await for (final chunk in response) {
    length += chunk.length;
    if (length > _maxReviewHistoryResponseBytes) {
      request.abort();
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.invalidResponse,
        retryable: true,
      );
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

const _maxReviewHistoryResponseBytes = 1024 * 1024;
