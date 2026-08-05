import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/providers/agent/wire_agent_image_client.dart';
import 'package:speakup/identity/auth_state.dart';

void main() {
  test(
    'uploads raw image bytes, signs content, and deletes the asset',
    () async {
      final payload = Uint8List.fromList(<int>[137, 80, 78, 71]);
      final transport = _ImageTransport(<_ImageStep>[
        _ImageStep(
          statusCode: HttpStatus.created,
          body: _jsonBytes(_assetJson()),
          verify: (request) {
            expect(request.method, 'POST');
            expect(
              request.uri.path,
              '/v1/agent-threads/$_threadId/image-assets',
            );
            expect(request.body, payload);
            expect(request.headers['Idempotency-Key'], 'image-upload-123');
            expect(request.headers[HttpHeaders.contentTypeHeader], 'image/png');
            expect(
              request.headers[HttpHeaders.authorizationHeader],
              'Bearer sess_account-a',
            );
          },
        ),
        _ImageStep(
          statusCode: HttpStatus.ok,
          body: _jsonBytes(<String, Object?>{
            'content_url':
                'https://private.example.test/image.png?signature=secret',
            'expires_at': DateTime.now()
                .toUtc()
                .add(const Duration(minutes: 1))
                .toIso8601String(),
          }),
        ),
        const _ImageStep(statusCode: HttpStatus.noContent, body: <int>[]),
      ]);
      final client = WireAgentImageClient(
        baseUri: Uri.parse('https://api.example.test'),
        credentialProvider: () => const AuthSessionCredential(
          sessionToken: 'sess_account-a',
          generation: 1,
        ),
        invalidateSession:
            ({
              required expectedSessionToken,
              required expectedGeneration,
            }) async {},
        transport: transport,
      );

      final asset = await client.uploadImage(
        threadId: _threadId,
        image: AgentLocalImage(
          name: 'fixture.png',
          contentType: 'image/png',
          bytes: payload,
        ),
        idempotencyKey: 'image-upload-123',
      );
      final content = await client.getMessageImageContent(
        imageAssetId: _imageId,
      );
      await client.deleteImage(imageAssetId: _imageId);

      expect(asset.id, _imageId);
      expect(asset.status, AgentImageAssetStatus.staged);
      expect(content.url.scheme, 'https');
      transport.expectDone();
    },
  );

  test('rejects oversized local image before transport', () async {
    final transport = _ImageTransport(const <_ImageStep>[]);
    final client = WireAgentImageClient(
      baseUri: Uri.parse('https://api.example.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_account-a',
        generation: 1,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
    );

    await expectLater(
      client.uploadImage(
        threadId: _threadId,
        image: AgentLocalImage(
          name: 'too-large.png',
          contentType: 'image/png',
          bytes: Uint8List(agentMaximumImageBytes + 1),
        ),
        idempotencyKey: 'image-upload-oversized',
      ),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidRequest,
        ),
      ),
    );
    transport.expectDone();
  });

  test('maps the server image size limit to a stable client error', () async {
    final transport = _ImageTransport(<_ImageStep>[
      _ImageStep(
        statusCode: HttpStatus.requestEntityTooLarge,
        body: _jsonBytes(<String, Object?>{
          'error': <String, Object?>{
            'code': 'image_too_large',
            'message': 'The image exceeds the allowed limits.',
            'retryable': false,
            'correlation_id': 'corr-image-limit',
          },
        }),
      ),
    ]);
    final client = WireAgentImageClient(
      baseUri: Uri.parse('https://api.example.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_account-a',
        generation: 1,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
    );

    await expectLater(
      client.uploadImage(
        threadId: _threadId,
        image: AgentLocalImage(
          name: 'large.png',
          contentType: 'image/png',
          bytes: Uint8List.fromList(<int>[137, 80, 78, 71]),
        ),
        idempotencyKey: 'image-upload-server-limit',
      ),
      throwsA(
        isA<AgentClientException>()
            .having(
              (error) => error.kind,
              'kind',
              AgentClientFailureKind.invalidRequest,
            )
            .having((error) => error.errorCode, 'errorCode', 'image_too_large'),
      ),
    );
    transport.expectDone();
  });
}

final class _ImageStep {
  const _ImageStep({required this.statusCode, required this.body, this.verify});

  final int statusCode;
  final List<int> body;
  final void Function(AgentImageWireRequest request)? verify;
}

final class _ImageTransport implements AgentImageWireTransport {
  _ImageTransport(Iterable<_ImageStep> steps)
    : _steps = Queue<_ImageStep>.of(steps);

  final Queue<_ImageStep> _steps;

  @override
  Future<AgentImageWireResponse> send(AgentImageWireRequest request) async {
    if (_steps.isEmpty) {
      throw StateError('unexpected image request');
    }
    final step = _steps.removeFirst();
    step.verify?.call(request);
    return AgentImageWireResponse(
      statusCode: step.statusCode,
      body: Uint8List.fromList(step.body),
    );
  }

  @override
  void close({bool force = false}) {}

  void expectDone() => expect(_steps, isEmpty);
}

Map<String, Object?> _assetJson() => <String, Object?>{
  'image_asset_id': _imageId,
  'thread_id': _threadId,
  'content_type': 'image/png',
  'size_bytes': 4,
  'width': 1,
  'height': 1,
  'status': 'staged',
  'created_at': '2026-07-30T00:00:00Z',
};

Uint8List _jsonBytes(Object? value) =>
    Uint8List.fromList(utf8.encode(jsonEncode(value)));

const _threadId = '10000000-0000-4000-8000-000000000001';
const _imageId = '70000000-0000-4000-8000-000000000001';
