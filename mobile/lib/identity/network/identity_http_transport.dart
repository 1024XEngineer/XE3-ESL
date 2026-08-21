import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';

import 'bearer_authentication.dart';
import 'transport_security.dart';

final class IdentityHttpResponse {
  const IdentityHttpResponse({
    required this.statusCode,
    required this.body,
    this.bodyBytes = const <int>[],
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final String body;
  final List<int> bodyBytes;
  final Map<String, String> headers;
}

abstract interface class IdentityHttpTransport {
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  });
}

final class SessionAuthenticatedHttpTransport implements IdentityHttpTransport {
  factory SessionAuthenticatedHttpTransport({
    required IdentityHttpTransport transport,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    required Uri trustedBaseUri,
  }) {
    return SessionAuthenticatedHttpTransport._(
      transport,
      credentialProvider,
      invalidateSession,
      TrustedIdentityHttpOrigin(trustedBaseUri),
    );
  }

  SessionAuthenticatedHttpTransport._(
    this.transport,
    this.credentialProvider,
    this.invalidateSession,
    this._trustedOrigin,
  );

  final IdentityHttpTransport transport;
  final AuthSessionCredentialProvider credentialProvider;
  final AuthSessionInvalidator invalidateSession;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    final credential = credentialProvider();
    if (credential == null) {
      throw StateError('An authenticated session is required.');
    }
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    final response = await transport.send(
      method: method,
      uri: uri,
      headers: <String, String>{
        ...headers,
        HttpHeaders.authorizationHeader: bearerAuthorizationValue(
          credential.sessionToken,
        ),
      },
      body: body,
      bodyBytes: bodyBytes,
    );
    if (!isSameAuthSessionCredential(credentialProvider(), credential)) {
      throw const AuthSessionSupersededException();
    }
    if (response.statusCode == HttpStatus.unauthorized) {
      await invalidateSession(
        expectedSessionToken: credential.sessionToken,
        expectedGeneration: credential.generation,
      );
    }
    return response;
  }
}

final class IoIdentityHttpTransport implements IdentityHttpTransport {
  IoIdentityHttpTransport({HttpClient? httpClient})
    : _httpClient = httpClient ?? HttpClient();

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
      request.add(utf8.encode(body));
    }

    final response = await request.close();
    final responseBytes = Uint8List.fromList(
      await response.fold<List<int>>(<int>[], (buffer, chunk) {
        buffer.addAll(chunk);
        return buffer;
      }),
    );
    final responseBody = utf8.decode(responseBytes, allowMalformed: true);
    final responseHeaders = <String, String>{};
    response.headers.forEach((name, values) {
      responseHeaders[name] = values.join(',');
    });
    return IdentityHttpResponse(
      statusCode: response.statusCode,
      body: responseBody,
      bodyBytes: responseBytes,
      headers: responseHeaders,
    );
  }

  void close({bool force = false}) {
    _httpClient.close(force: force);
  }
}
