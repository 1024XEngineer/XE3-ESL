import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'native transport sends Unicode registration JSON as UTF-8 bytes',
    () async {
      final capturedRequest = Completer<_CapturedRequest>();
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      server.listen((request) async {
        final bodyBytes = await request.fold<List<int>>(
          <int>[],
          (bytes, chunk) => bytes..addAll(chunk),
        );
        capturedRequest.complete(
          _CapturedRequest(
            method: request.method,
            path: request.uri.path,
            contentType: request.headers.contentType,
            bodyBytes: bodyBytes,
          ),
        );
        request.response
          ..statusCode = HttpStatus.created
          ..headers.contentType = ContentType.json
          ..write('{"user_id":"user_1","email":"learner@example.com"}');
        await request.response.close();
      });
      final transport = IoIdentityHttpTransport();

      try {
        final client = WireIdentityClient(
          baseUri: Uri.parse('http://127.0.0.1:${server.port}'),
          transport: transport,
        );
        const password = '安全-Passphrase-🔐';

        final user = await client.register(
          email: 'learner@example.com',
          password: password,
        );
        final captured = await capturedRequest.future;

        expect(user.id, 'user_1');
        expect(captured.method, 'POST');
        expect(captured.path, '/v1/auth/register');
        expect(captured.contentType?.mimeType, ContentType.json.mimeType);
        expect(
          captured.bodyBytes,
          utf8.encode(
            jsonEncode(<String, Object?>{
              'email': 'learner@example.com',
              'password': password,
            }),
          ),
        );
        expect(jsonDecode(utf8.decode(captured.bodyBytes)), <String, Object?>{
          'email': 'learner@example.com',
          'password': password,
        });
      } finally {
        transport.close(force: true);
        await server.close(force: true);
      }
    },
  );

  test('native transport preserves an ordinary request and response', () async {
    final capturedRequest = Completer<_CapturedRequest>();
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((request) async {
      final bodyBytes = await request.fold<List<int>>(
        <int>[],
        (bytes, chunk) => bytes..addAll(chunk),
      );
      capturedRequest.complete(
        _CapturedRequest(
          method: request.method,
          path: request.uri.path,
          contentType: request.headers.contentType,
          bodyBytes: bodyBytes,
        ),
      );
      request.response
        ..statusCode = HttpStatus.ok
        ..headers.set('x-request-result', 'accepted')
        ..write('{"status":"ok"}');
      await request.response.close();
    });
    final transport = IoIdentityHttpTransport();

    try {
      final response = await transport.send(
        method: 'GET',
        uri: Uri.parse('http://127.0.0.1:${server.port}/health'),
        headers: const <String, String>{
          HttpHeaders.acceptHeader: 'application/json',
        },
      );
      final captured = await capturedRequest.future;

      expect(captured.method, 'GET');
      expect(captured.path, '/health');
      expect(captured.bodyBytes, isEmpty);
      expect(response.statusCode, HttpStatus.ok);
      expect(response.body, '{"status":"ok"}');
      expect(response.headers['x-request-result'], 'accepted');
    } finally {
      transport.close(force: true);
      await server.close(force: true);
    }
  });
}

final class _CapturedRequest {
  const _CapturedRequest({
    required this.method,
    required this.path,
    required this.contentType,
    required this.bodyBytes,
  });

  final String method;
  final String path;
  final ContentType? contentType;
  final List<int> bodyBytes;
}
