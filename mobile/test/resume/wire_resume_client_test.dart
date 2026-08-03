// 本文件验证 Resume 线上客户端的认证、列表解码与 Multipart 上传契约。

import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/resume/resume.dart';

void main() {
  test('list sends Bearer and strictly decodes three-item contract', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final requestSeen = _serveOnce(server, (request, body) {
      expect(request.method, 'GET');
      expect(request.uri.path, '/v1/resumes');
      expect(request.uri.queryParameters['limit'], '3');
      expect(
        request.headers.value(HttpHeaders.authorizationHeader),
        'Bearer sess_resume-test',
      );
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode(<String, Object?>{
          'items': <Object?>[_resumeJson('resume-1')],
        }),
      );
    });
    final client = _client(server);

    final items = await client.list();
    await requestSeen;

    expect(items.single.id, 'resume-1');
    expect(items.single.parseStatus, ResumeParseStatus.ready);
    await server.close(force: true);
  });

  test('create sends PDF multipart and an idempotency key', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final requestSeen = _serveOnce(server, (request, body) {
      expect(request.method, 'POST');
      expect(request.uri.path, '/v1/resumes');
      expect(request.headers.value('Idempotency-Key'), startsWith('resume-'));
      expect(request.headers.contentType?.mimeType, 'multipart/form-data');
      final encoded = latin1.decode(body);
      expect(encoded, contains('name="title"'));
      expect(encoded, contains('Backend Engineer'));
      expect(encoded, contains('filename="resume.pdf"'));
      expect(encoded, contains('%PDF-test'));
      request.response.statusCode = HttpStatus.accepted;
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode(_resumeJson('created')));
    });
    final client = _client(server);

    final created = await client.create(
      title: 'Backend Engineer',
      file: ResumePdfFile(name: 'resume.pdf', bytes: '%PDF-test'.codeUnits),
    );
    await requestSeen;

    expect(created.id, 'created');
    await server.close(force: true);
  });
}

WireResumeClient _client(HttpServer server) => WireResumeClient(
  baseUri: Uri.parse('http://${server.address.address}:${server.port}'),
  credentialProvider: () => const AuthSessionCredential(
    sessionToken: 'sess_resume-test',
    generation: 1,
  ),
  invalidateSession:
      ({required expectedSessionToken, required expectedGeneration}) async {},
);

Future<void> _serveOnce(
  HttpServer server,
  void Function(HttpRequest request, List<int> body) handler,
) async {
  final request = await server.first;
  final body = await request.fold<List<int>>(<int>[], (all, chunk) {
    all.addAll(chunk);
    return all;
  });
  handler(request, body);
  await request.response.close();
}

Map<String, Object?> _resumeJson(String id) => <String, Object?>{
  'resume_id': id,
  'title': 'Backend Engineer',
  'original_filename': 'resume.pdf',
  'content_type': 'application/pdf',
  'size_bytes': 1024,
  'file_status': 'AVAILABLE',
  'parse_status': 'READY',
  'version': 1,
  'created_at': '2026-08-03T00:00:00Z',
  'updated_at': '2026-08-03T00:00:00Z',
};
