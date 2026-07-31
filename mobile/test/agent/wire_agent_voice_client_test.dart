import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/agent/wire_agent_voice_client.dart';
import 'package:speakup/identity/auth_state.dart';

void main() {
  test('uploads raw WAV to the exact candidate route', () async {
    final root = await Directory.systemTemp.createTemp(
      'agent-voice-wire-test-',
    );
    addTearDown(() => root.delete(recursive: true));
    final file = File('${root.path}/message.wav');
    await file.writeAsBytes(_waveBytes);
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-threads/$_threadId/voice-message-candidates/stream',
        verify: (request) {
          expect(
            request.headers[HttpHeaders.authorizationHeader],
            'Bearer sess_voice',
          );
          expect(request.headers[HttpHeaders.contentTypeHeader], 'audio/wav');
          expect(request.headers['Idempotency-Key'], 'voice_upload_001');
          expect(request.body, _waveBytes);
        },
        response: _sseResponse(<(String, Object?)>[
          ('transcription.started', <String, Object?>{}),
          (
            'transcription.updated',
            <String, Object?>{
              'transcript': 'Candidate transcript',
              'final': false,
            },
          ),
          (
            'candidate.ready',
            <String, Object?>{
              'candidate': _candidateJson(status: 'candidate_ready'),
            },
          ),
        ]),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final candidate = await client.createCandidate(
      threadId: _threadId,
      recording: AgentVoiceLocalRecording(
        path: file.path,
        contentType: 'audio/wav',
        sizeBytes: _waveBytes.length,
        duration: const Duration(seconds: 3),
      ),
      idempotencyKey: 'voice_upload_001',
    );

    expect(candidate.id, _candidateId);
    expect(candidate.transcript?.text, 'Candidate transcript');
    expect(candidate.status, AgentVoiceCandidateStatus.candidateReady);
    transport.expectDone();
  });

  test('confirms one voice Message with exact strict JSON', () async {
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-voice-message-candidates/$_candidateId/confirmations',
        verify: (request) {
          expect(jsonDecode(utf8.decode(request.body!)), <String, Object?>{
            'candidate_version': 1,
            'client_message_id': 'voice_message_001',
            'confirmed_text': 'Edited confirmed transcript',
          });
        },
        response: _jsonResponse(HttpStatus.accepted, <String, Object?>{
          'candidate': _candidateJson(status: 'confirmed', confirmed: true),
          'message': _voiceMessageJson(content: 'Edited confirmed transcript'),
          'run': _runJson(status: 'pending'),
        }),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final confirmation = await client.confirmCandidate(
      candidateId: _candidateId,
      candidateVersion: 1,
      clientMessageId: 'voice_message_001',
      confirmedText: 'Edited confirmed transcript',
    );

    expect(confirmation.message.modality, AgentMessageModality.voice);
    expect(confirmation.message.audio?.id, _audioId);
    expect(confirmation.run.status, AgentVoiceRunStatus.pending);
    expect(confirmation.candidate.confirmedMessageId, _messageId);
    transport.expectDone();
  });

  test(
    'decodes confirmation fields for confirmed deleting and deleted candidates',
    () async {
      for (final status in <String>['confirmed', 'deleting', 'deleted']) {
        final candidateJson = _candidateJson(status: status, confirmed: true);
        if (status == 'deleted') {
          candidateJson['deleted_at'] = _timestamp;
        }
        final transport = _ScriptedVoiceTransport([
          _Step(
            method: 'GET',
            path: '/v1/agent-voice-message-candidates/$_candidateId',
            response: _jsonResponse(HttpStatus.ok, candidateJson),
          ),
        ]);
        final client = _client(transport);
        addTearDown(client.dispose);

        final candidate = await client.getCandidate(candidateId: _candidateId);

        expect(candidate.confirmedMessageId, _messageId, reason: status);
        expect(candidate.confirmedRunId, _runId, reason: status);
        expect(candidate.messageAudioId, _audioId, reason: status);
        expect(candidate.confirmedAt, isNotNull, reason: status);
        transport.expectDone();
      }
    },
  );

  test(
    'decodes absent modality as text and rejects text carrying audio',
    () async {
      final validTransport = _ScriptedVoiceTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages?page_size=100',
          response: _jsonResponse(HttpStatus.ok, {
            'messages': [_assistantMessageJson()],
          }),
        ),
      ]);
      final validClient = _client(validTransport);
      addTearDown(validClient.dispose);

      final message = await validClient.getMessage(
        threadId: _threadId,
        messageId: _assistantMessageId,
      );
      expect(message?.modality, AgentMessageModality.text);
      expect(message?.audio, isNull);

      final invalid = _assistantMessageJson()
        ..['audio'] = _audioJson()
        ..remove('modality');
      final invalidTransport = _ScriptedVoiceTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages?page_size=100',
          response: _jsonResponse(HttpStatus.ok, {
            'messages': [invalid],
          }),
        ),
      ]);
      final invalidClient = _client(invalidTransport);
      addTearDown(invalidClient.dispose);

      await expectLater(
        invalidClient.getMessage(
          threadId: _threadId,
          messageId: _assistantMessageId,
        ),
        throwsA(
          isA<AgentClientException>().having(
            (error) => error.kind,
            'kind',
            AgentClientFailureKind.invalidResponse,
          ),
        ),
      );
    },
  );

  test('restores an assistant Message carrying a scene action', () async {
    final assistant = _assistantMessageJson()
      ..['actions'] = <Object?>[
        <String, Object?>{
          'type': 'open_interview_preparation',
          'label': '开始准备',
          'matter_id': _matterId,
          'title': '阿里高级 Java 开发面试',
        },
      ];
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'GET',
        path: '/v1/agent-threads/$_threadId/messages?page_size=100',
        response: _jsonResponse(HttpStatus.ok, {
          'messages': [assistant],
        }),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final message = await client.getMessage(
      threadId: _threadId,
      messageId: _assistantMessageId,
    );

    expect(message?.actions, hasLength(1));
    expect(
      message?.actions.single.type,
      AgentMessageActionType.openInterviewPreparation,
    );
    expect(message?.actions.single.matterId, _matterId);
    transport.expectDone();
  });

  test(
    'loads private recording without forwarding Session auth to OSS',
    () async {
      final expiresAt = DateTime.utc(2026, 7, 26, 12, 1);
      final api = _ScriptedVoiceTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-message-audios/$_audioId/playback',
          response: _jsonResponse(
            HttpStatus.ok,
            {
              'playback_url': 'https://private.example.test/object.wav?sig=x',
              'expires_at': expiresAt.toIso8601String(),
            },
            headers: const <String, String>{
              HttpHeaders.cacheControlHeader: 'no-store',
            },
          ),
        ),
      ]);
      final signed = _ScriptedVoiceTransport([
        _Step(
          method: 'GET',
          path: '/object.wav?sig=x',
          verify: (request) {
            expect(
              request.headers.containsKey(HttpHeaders.authorizationHeader),
              isFalse,
            );
          },
          response: AgentVoiceWireResponse(
            statusCode: HttpStatus.ok,
            body: Uint8List.fromList(_waveBytes),
            headers: const <String, String>{
              HttpHeaders.contentTypeHeader: 'audio/wav',
            },
          ),
        ),
      ]);
      final client = WireAgentVoiceClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        credentialProvider: () => _credential,
        invalidateSession: _ignoreInvalidation,
        apiTransport: api,
        signedAudioTransport: signed,
        now: () => DateTime.utc(2026, 7, 26, 12),
      );
      addTearDown(client.dispose);

      final bytes = await client.loadMessageAudio(audioId: _audioId);

      expect(bytes, _waveBytes);
      api.expectDone();
      signed.expectDone();
    },
  );
}

WireAgentVoiceClient _client(_ScriptedVoiceTransport transport) {
  return WireAgentVoiceClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => _credential,
    invalidateSession: _ignoreInvalidation,
    apiTransport: transport,
    signedAudioTransport: _ScriptedVoiceTransport(const <_Step>[]),
  );
}

Future<void> _ignoreInvalidation({
  required String expectedSessionToken,
  required int expectedGeneration,
}) async {}

const _credential = AuthSessionCredential(
  sessionToken: 'sess_voice',
  generation: 1,
);

final class _ScriptedVoiceTransport implements AgentVoiceWireTransport {
  _ScriptedVoiceTransport(Iterable<_Step> steps)
    : _steps = Queue<_Step>.of(steps);

  final Queue<_Step> _steps;

  @override
  Future<AgentVoiceWireResponse> send(AgentVoiceWireRequest request) async {
    final step = _takeStep(request);
    return step.response;
  }

  @override
  Future<AgentVoiceWireStreamResponse> openStream(
    AgentVoiceWireRequest request,
  ) async {
    final step = _takeStep(request);
    return AgentVoiceWireStreamResponse(
      statusCode: step.response.statusCode,
      body: Stream<Uint8List>.value(step.response.body),
      headers: step.response.headers,
    );
  }

  _Step _takeStep(AgentVoiceWireRequest request) {
    if (_steps.isEmpty) {
      fail('Unexpected ${request.method} ${request.uri}');
    }
    final step = _steps.removeFirst();
    expect(request.method, step.method);
    expect(
      '${request.uri.path}${request.uri.hasQuery ? '?${request.uri.query}' : ''}',
      step.path,
    );
    step.verify?.call(
      AgentVoiceWireRequest(
        method: request.method,
        uri: request.uri,
        headers: Map<String, String>.of(request.headers),
        maximumResponseBytes: request.maximumResponseBytes,
        body: request.body == null ? null : Uint8List.fromList(request.body!),
      ),
    );
    return step;
  }

  @override
  void close({bool force = false}) {}

  void expectDone() {
    expect(_steps, isEmpty);
  }
}

final class _Step {
  const _Step({
    required this.method,
    required this.path,
    required this.response,
    this.verify,
  });

  final String method;
  final String path;
  final AgentVoiceWireResponse response;
  final void Function(AgentVoiceWireRequest request)? verify;
}

AgentVoiceWireResponse _jsonResponse(
  int status,
  Object? body, {
  Map<String, String> headers = const <String, String>{},
}) {
  return AgentVoiceWireResponse(
    statusCode: status,
    body: Uint8List.fromList(utf8.encode(jsonEncode(body))),
    headers: headers,
  );
}

AgentVoiceWireResponse _sseResponse(Iterable<(String, Object?)> events) {
  final body = StringBuffer();
  for (final (event, data) in events) {
    body
      ..writeln('event: $event')
      ..writeln('data: ${jsonEncode(data)}')
      ..writeln();
  }
  return AgentVoiceWireResponse(
    statusCode: HttpStatus.ok,
    body: Uint8List.fromList(utf8.encode(body.toString())),
    headers: const <String, String>{
      HttpHeaders.contentTypeHeader: 'text/event-stream; charset=utf-8',
    },
  );
}

Map<String, Object?> _candidateJson({
  required String status,
  bool confirmed = false,
}) {
  return <String, Object?>{
    'candidate_id': _candidateId,
    'thread_id': _threadId,
    'status': status,
    'asr_attempt': 1,
    'candidate_version': 1,
    'recording': <String, Object?>{
      'content_type': 'audio/wav',
      'size_bytes': _waveBytes.length,
      'duration_ms': 3000,
      'sample_rate': 16000,
    },
    'transcript': <String, Object?>{
      'candidate_text': 'Candidate transcript',
      'request_id': 'request-voice-001',
      'provider': 'qwen',
      'model': 'qwen-asr',
      'language': 'en',
      'finish_reason': 'stop',
    },
    'expires_at': '2026-07-26T13:00:00Z',
    if (confirmed) ...<String, Object?>{
      'confirmed_message_id': _messageId,
      'confirmed_run_id': _runId,
      'message_audio_id': _audioId,
      'confirmed_at': _timestamp,
    },
    'created_at': _timestamp,
    'updated_at': _timestamp,
  };
}

Map<String, Object?> _voiceMessageJson({required String content}) {
  return <String, Object?>{
    'message_id': _messageId,
    'thread_id': _threadId,
    'sequence': 1,
    'role': 'user',
    'client_message_id': 'voice_message_001',
    'modality': 'voice',
    'content': content,
    'audio': _audioJson(),
    'created_at': _timestamp,
  };
}

Map<String, Object?> _audioJson() {
  return <String, Object?>{
    'audio_id': _audioId,
    'status': 'readable',
    'content_type': 'audio/wav',
    'size_bytes': _waveBytes.length,
    'duration_ms': 3000,
    'playback_path': '/v1/agent-message-audios/$_audioId/playback',
  };
}

Map<String, Object?> _assistantMessageJson() {
  return <String, Object?>{
    'message_id': _assistantMessageId,
    'thread_id': _threadId,
    'sequence': 2,
    'role': 'assistant',
    'produced_by_run_id': _runId,
    'content': 'Plain assistant text',
    'created_at': _timestamp,
  };
}

Map<String, Object?> _runJson({required String status}) {
  return <String, Object?>{
    'run_id': _runId,
    'thread_id': _threadId,
    'input_message_id': _messageId,
    'attempt': 1,
    'status': status,
    'requested_provider': 'qwen',
    'requested_model': 'qwen-plus',
    'max_output_tokens': 512,
    'created_at': _timestamp,
    'updated_at': _timestamp,
  };
}

const _threadId = '11111111-1111-4111-8111-111111111111';
const _candidateId = '22222222-2222-4222-8222-222222222222';
const _messageId = '33333333-3333-4333-8333-333333333333';
const _runId = '44444444-4444-4444-8444-444444444444';
const _audioId = '55555555-5555-4555-8555-555555555555';
const _assistantMessageId = '66666666-6666-4666-8666-666666666666';
const _matterId = '77777777-7777-4777-8777-777777777777';
const _timestamp = '2026-07-26T12:00:00Z';

const _waveBytes = <int>[
  0x52,
  0x49,
  0x46,
  0x46,
  0x28,
  0x00,
  0x00,
  0x00,
  0x57,
  0x41,
  0x56,
  0x45,
  0x66,
  0x6d,
  0x74,
  0x20,
  0x10,
  0x00,
  0x00,
  0x00,
  0x01,
  0x00,
  0x01,
  0x00,
  0x80,
  0x3e,
  0x00,
  0x00,
  0x00,
  0x7d,
  0x00,
  0x00,
  0x02,
  0x00,
  0x10,
  0x00,
  0x64,
  0x61,
  0x74,
  0x61,
  0x04,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
];
