import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_input_client.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';
import 'package:speakup/providers/agent/wire_agent_voice_client.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';
import 'package:speakup/identity/auth_state.dart';

void main() {
  test('receives assistant PCM before text input stream completes', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    final handled = Completer<void>();
    server.listen((request) async {
      expect(
        request.uri.path,
        '/v1/agent-threads/$_threadId/assistant-speech/realtime',
      );
      final socket = await WebSocketTransformer.upgrade(
        request,
        protocolSelector: (protocols) => 'speakup.assistant-speech.v1',
      );
      socket.add(
        jsonEncode(<String, Object>{
          'type': 'stream.ready',
          'data': <String, Object>{
            'content_type': 'audio/pcm',
            'sample_rate': 24000,
            'channel_count': 1,
            'bits_per_sample': 16,
          },
        }),
      );
      var sentAudio = false;
      await for (final message in socket) {
        final frame = jsonDecode(message as String) as Map<String, dynamic>;
        if (frame['type'] == 'segment' && !sentAudio) {
          sentAudio = true;
          socket.add(Uint8List.fromList(<int>[1, 2, 3, 4]));
        }
        if (frame['type'] == 'finish') {
          socket.add(
            jsonEncode(const <String, Object>{
              'type': 'stream.completed',
              'data': <String, Object>{},
            }),
          );
          await socket.close();
          handled.complete();
          break;
        }
      }
    });
    final client = WireAgentVoiceClient(
      baseUri: Uri.parse('http://${server.address.address}:${server.port}'),
      credentialProvider: () => _credential,
      invalidateSession: _ignoreInvalidation,
      apiTransport: _ScriptedVoiceTransport(const <_Step>[]),
      signedAudioTransport: _ScriptedVoiceTransport(const <_Step>[]),
    );
    addTearDown(client.dispose);
    final text = StreamController<AgentAssistantSpeechTextSegment>();
    addTearDown(text.close);
    final audio = StreamIterator<AgentAssistantSpeechAudioSegment>(
      client.streamAssistantSpeech(threadId: _threadId, segments: text.stream),
    );
    addTearDown(audio.cancel);

    final firstAudio = audio.moveNext();
    text.add(
      const AgentAssistantSpeechTextSegment(
        sequence: 1,
        text: 'This arrives while the model is still streaming',
      ),
    );
    expect(await firstAudio.timeout(const Duration(seconds: 2)), isTrue);
    expect(audio.current.audio, <int>[1, 2, 3, 4]);
    await text.close();
    expect(await audio.moveNext(), isFalse);
    await handled.future;
  });

  test('streams PCM chunks over the authenticated voice WebSocket', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    final received = <Object>[];
    final handled = Completer<void>();
    server.listen((request) async {
      expect(
        request.uri.path,
        '/v1/agent-threads/$_threadId/voice-drafts/realtime',
      );
      expect(
        request.headers.value(HttpHeaders.authorizationHeader),
        'Bearer sess_voice',
      );
      final socket = await WebSocketTransformer.upgrade(
        request,
        protocolSelector: (protocols) => 'speakup.voice-input.v1',
      );
      await for (final message in socket) {
        received.add(message);
        if (message is String &&
            (jsonDecode(message) as Map<String, dynamic>)['type'] == 'finish') {
          socket.add(
            jsonEncode(<String, Object>{
              'type': 'draft.ready',
              'data': <String, Object>{'draft': _draftJson(status: 'ready')},
            }),
          );
          await socket.close();
          if (!handled.isCompleted) {
            handled.complete();
          }
          break;
        }
      }
    });
    final baseUri = Uri.parse(
      'http://${server.address.address}:${server.port}',
    );
    final client = WireAgentVoiceClient(
      baseUri: baseUri,
      credentialProvider: () => _credential,
      invalidateSession: _ignoreInvalidation,
      apiTransport: _ScriptedVoiceTransport(const <_Step>[]),
      signedAudioTransport: _ScriptedVoiceTransport(const <_Step>[]),
    );
    addTearDown(client.dispose);

    final events = await client
        .createDraftRealtime(
          threadId: _threadId,
          audioChunks: Stream<Uint8List>.fromIterable(<Uint8List>[
            Uint8List.fromList(<int>[1, 2]),
            Uint8List.fromList(<int>[3, 4]),
          ]),
          idempotencyKey: 'voice_realtime_001',
        )
        .toList();
    await handled.future;

    expect(events, hasLength(1));
    final completed = events.single as AgentVoiceDraftCompleted;
    expect(completed.draft.isReady, isTrue);

    expect(jsonDecode(received.first as String), <String, Object?>{
      'type': 'start',
      'idempotency_key': 'voice_realtime_001',
      'sample_rate': 16000,
    });
    expect(received[1], <int>[1, 2]);
    expect(received[2], <int>[3, 4]);
    expect(jsonDecode(received.last as String), <String, Object?>{
      'type': 'finish',
    });
  });

  test(
    'decodes ephemeral voice-to-text events from the frozen endpoint',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final received = <Object>[];
      final handled = Completer<void>();
      server.listen((request) async {
        expect(
          request.uri.path,
          '/v1/agent-threads/$_threadId/voice-transcriptions/realtime',
        );
        expect(
          request.headers.value(HttpHeaders.authorizationHeader),
          'Bearer sess_voice',
        );
        final socket = await WebSocketTransformer.upgrade(
          request,
          protocolSelector: (protocols) {
            expect(protocols, contains('speakup.voice-input.v1'));
            return 'speakup.voice-input.v1';
          },
        );
        await for (final message in socket) {
          received.add(message);
          if (received.length == 1) {
            socket.add(
              jsonEncode(const <String, Object>{
                'type': 'transcription.started',
                'data': <String, Object>{},
              }),
            );
            socket.add(
              jsonEncode(const <String, Object>{
                'type': 'transcription.updated',
                'data': <String, Object>{
                  'transcript': 'Partial answer',
                  'final': false,
                },
              }),
            );
          }
          if (message is String &&
              (jsonDecode(message) as Map<String, dynamic>)['type'] ==
                  'finish') {
            socket.add(
              jsonEncode(const <String, Object>{
                'type': 'transcription.completed',
                'data': <String, Object>{
                  'transcript': 'Completed answer',
                  'final': true,
                },
              }),
            );
            await socket.close();
            handled.complete();
            break;
          }
        }
      });
      final client = WireAgentVoiceClient(
        baseUri: Uri.parse('http://${server.address.address}:${server.port}'),
        credentialProvider: () => _credential,
        invalidateSession: _ignoreInvalidation,
        apiTransport: _ScriptedVoiceTransport(const <_Step>[]),
        signedAudioTransport: _ScriptedVoiceTransport(const <_Step>[]),
      );
      addTearDown(client.dispose);

      final events = await client
          .transcribeRealtime(
            threadId: _threadId,
            audioChunks: Stream<Uint8List>.value(
              Uint8List.fromList(const <int>[1, 0, 2, 0]),
            ),
            idempotencyKey: 'voice_input_001',
          )
          .toList();
      await handled.future;

      expect(events, hasLength(3));
      expect(events[0], isA<AgentVoiceInputStarted>());
      expect(
        (events[1] as AgentVoiceInputUpdated).transcript,
        'Partial answer',
      );
      expect(
        (events[2] as AgentVoiceInputCompleted).transcript,
        'Completed answer',
      );
      expect(jsonDecode(received.first as String), <String, Object?>{
        'type': 'start',
        'idempotency_key': 'voice_input_001',
        'sample_rate': 16000,
      });
      expect(received[1], <int>[1, 0, 2, 0]);
      expect(jsonDecode(received.last as String), <String, Object?>{
        'type': 'finish',
      });
    },
  );

  test(
    'rejects a completed voice transcript unless final is exactly true',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final handled = Completer<void>();
      server.listen((request) async {
        final socket = await WebSocketTransformer.upgrade(
          request,
          protocolSelector: (_) => 'speakup.voice-input.v1',
        );
        await for (final message in socket) {
          if (message is String &&
              (jsonDecode(message) as Map<String, dynamic>)['type'] ==
                  'start') {
            socket.add(
              jsonEncode(const <String, Object>{
                'type': 'transcription.started',
                'data': <String, Object>{},
              }),
            );
          }
          if (message is String &&
              (jsonDecode(message) as Map<String, dynamic>)['type'] ==
                  'finish') {
            socket.add(
              jsonEncode(const <String, Object>{
                'type': 'transcription.completed',
                'data': <String, Object>{
                  'transcript': 'Not terminal',
                  'final': false,
                },
              }),
            );
            await socket.close();
            handled.complete();
            break;
          }
        }
      });
      final client = WireAgentVoiceClient(
        baseUri: Uri.parse('http://${server.address.address}:${server.port}'),
        credentialProvider: () => _credential,
        invalidateSession: _ignoreInvalidation,
        apiTransport: _ScriptedVoiceTransport(const <_Step>[]),
        signedAudioTransport: _ScriptedVoiceTransport(const <_Step>[]),
      );
      addTearDown(client.dispose);

      await expectLater(
        client.transcribeRealtime(
          threadId: _threadId,
          audioChunks: Stream<Uint8List>.value(
            Uint8List.fromList(const <int>[1, 0]),
          ),
          idempotencyKey: 'voice_input_002',
        ),
        emitsInOrder(<Object>[
          isA<AgentVoiceInputStarted>(),
          emitsError(
            isA<AgentClientException>().having(
              (error) => error.kind,
              'kind',
              AgentClientFailureKind.invalidResponse,
            ),
          ),
        ]),
      );
      await handled.future;
    },
  );

  test(
    'cancelling ephemeral transcription sends cancel instead of finish',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final receivedTypes = <String>[];
      final handled = Completer<void>();
      server.listen((request) async {
        final socket = await WebSocketTransformer.upgrade(
          request,
          protocolSelector: (_) => 'speakup.voice-input.v1',
        );
        await for (final message in socket) {
          if (message is! String) {
            continue;
          }
          final type = (jsonDecode(message) as Map<String, dynamic>)['type'];
          if (type is! String) {
            continue;
          }
          receivedTypes.add(type);
          if (type == 'start') {
            socket.add(
              jsonEncode(const <String, Object>{
                'type': 'transcription.started',
                'data': <String, Object>{},
              }),
            );
          } else if (type == 'cancel') {
            if (!handled.isCompleted) {
              handled.complete();
            }
            await socket.close();
            break;
          }
        }
      });
      final client = WireAgentVoiceClient(
        baseUri: Uri.parse('http://${server.address.address}:${server.port}'),
        credentialProvider: () => _credential,
        invalidateSession: _ignoreInvalidation,
        apiTransport: _ScriptedVoiceTransport(const <_Step>[]),
        signedAudioTransport: _ScriptedVoiceTransport(const <_Step>[]),
      );
      addTearDown(client.dispose);
      final audio = StreamController<Uint8List>();
      addTearDown(audio.close);
      final started = Completer<void>();
      final subscription = client
          .transcribeRealtime(
            threadId: _threadId,
            audioChunks: audio.stream,
            idempotencyKey: 'voice_input_003',
          )
          .listen((event) {
            if (event is AgentVoiceInputStarted && !started.isCompleted) {
              started.complete();
            }
          });

      await started.future.timeout(const Duration(seconds: 2));
      await subscription.cancel();
      await handled.future.timeout(const Duration(seconds: 2));

      expect(
        receivedTypes,
        containsAllInOrder(const <String>['start', 'cancel']),
      );
      expect(receivedTypes, isNot(contains('finish')));
    },
  );

  test('confirms one voice Message with exact strict JSON', () async {
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-voice-drafts/$_draftId/confirmations',
        verify: (request) {
          expect(jsonDecode(utf8.decode(request.body!)), <String, Object?>{
            'draft_version': 1,
            'client_message_id': 'voice_message_001',
            'confirmed_text': 'Edited confirmed transcript',
          });
        },
        response: _jsonResponse(HttpStatus.accepted, <String, Object?>{
          'draft': _draftJson(status: 'confirmed', confirmed: true),
          'message': _voiceMessageJson(content: 'Edited confirmed transcript'),
          'run': _runJson(status: 'pending'),
        }),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final confirmation = await client.confirmDraft(
      draftId: _draftId,
      draftVersion: 1,
      clientMessageId: 'voice_message_001',
      confirmedText: 'Edited confirmed transcript',
    );

    expect(confirmation.message.modality, AgentMessageModality.voice);
    expect(confirmation.message.audio?.id, _audioId);
    expect(confirmation.run.status, AgentVoiceRunStatus.pending);
    expect(confirmation.draft.confirmedMessageId, _messageId);
    transport.expectDone();
  });

  test('rejects obsolete durable Message audio states', () async {
    final message = _voiceMessageJson(content: 'Edited confirmed transcript');
    (message['audio']! as Map<String, Object?>)['status'] = 'deleted';
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-voice-drafts/$_draftId/confirmations',
        response: _jsonResponse(HttpStatus.accepted, <String, Object?>{
          'draft': _draftJson(status: 'confirmed', confirmed: true),
          'message': message,
          'run': _runJson(status: 'pending'),
        }),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    await expectLater(
      client.confirmDraft(
        draftId: _draftId,
        draftVersion: 1,
        clientMessageId: 'voice_message_001',
        confirmedText: 'Edited confirmed transcript',
      ),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidResponse,
        ),
      ),
    );
    transport.expectDone();
  });

  test(
    'keeps a confirmation SSE stream open across assistant deltas',
    () async {
      final completedRun = _runJson(status: 'completed')
        ..['assistant_message_id'] = _assistantMessageId
        ..['completion_source'] = 'model'
        ..['provider_completion_id'] = 'completion-voice-001'
        ..['provider_model'] = 'qwen-plus'
        ..['finish_reason'] = 'stop'
        ..['usage'] = <String, Object?>{
          'input_tokens': 10,
          'output_tokens': 4,
          'total_tokens': 14,
        }
        ..['started_at'] = _timestamp
        ..['completed_at'] = _timestamp;
      final transport = _ScriptedVoiceTransport([
        _Step(
          method: 'POST',
          path: '/v1/agent-voice-drafts/$_draftId/confirmations/stream',
          response: _sseResponse(<(String, Object?)>[
            (
              'input.committed',
              <String, Object?>{
                'draft': _draftJson(status: 'confirmed', confirmed: true),
                'message': _voiceMessageJson(
                  content: 'Edited confirmed transcript',
                ),
                'run': _runJson(status: 'pending'),
              },
            ),
            (
              'assistant.output.started',
              <String, Object?>{
                'run_id': _runId,
                'output_id': _assistantMessageId,
              },
            ),
            (
              'assistant.output.delta',
              <String, Object?>{
                'run_id': _runId,
                'output_id': _assistantMessageId,
                'sequence': 1,
                'delta': 'Hello',
              },
            ),
            (
              'assistant.output.delta',
              <String, Object?>{
                'run_id': _runId,
                'output_id': _assistantMessageId,
                'sequence': 2,
                'delta': ', how can I help?',
              },
            ),
            (
              'assistant.output.completed',
              <String, Object?>{
                'run_id': _runId,
                'output_id': _assistantMessageId,
                'text': 'Hello, how can I help?',
              },
            ),
            ('run.completed', <String, Object?>{'run': completedRun}),
          ]),
        ),
      ]);
      final client = _client(transport);
      addTearDown(client.dispose);

      final events = await client
          .confirmDraftStream(
            draftId: _draftId,
            draftVersion: 1,
            clientMessageId: 'voice_message_001',
            confirmedText: 'Edited confirmed transcript',
          )
          .toList();

      expect(events, hasLength(6));
      expect(
        events.whereType<AgentVoiceAssistantOutputDelta>().map(
          (event) => event.delta,
        ),
        <String>['Hello', ', how can I help?'],
      );
      expect(events.last, isA<AgentVoiceRunCompleted>());
      transport.expectDone();
    },
  );

  test('decodes a domain-completed Run from stream and recovery GET', () async {
    final completedRun = _completedRunJson(completionSource: 'domain');
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-voice-drafts/$_draftId/confirmations/stream',
        response: _sseResponse(<(String, Object?)>[
          (
            'input.committed',
            <String, Object?>{
              'draft': _draftJson(status: 'confirmed', confirmed: true),
              'message': _voiceMessageJson(
                content: 'Edited confirmed transcript',
              ),
              'run': _runJson(status: 'pending'),
            },
          ),
          (
            'assistant.output.started',
            <String, Object?>{
              'run_id': _runId,
              'output_id': _assistantMessageId,
            },
          ),
          (
            'assistant.output.delta',
            <String, Object?>{
              'run_id': _runId,
              'output_id': _assistantMessageId,
              'sequence': 1,
              'delta': 'Your practice is ready.',
            },
          ),
          (
            'assistant.output.completed',
            <String, Object?>{
              'run_id': _runId,
              'output_id': _assistantMessageId,
              'text': 'Your practice is ready.',
            },
          ),
          ('run.completed', <String, Object?>{'run': completedRun}),
        ]),
      ),
      _Step(
        method: 'GET',
        path: '/v1/agent-runs/$_runId',
        response: _jsonResponse(HttpStatus.ok, completedRun),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final events = await client
        .confirmDraftStream(
          draftId: _draftId,
          draftVersion: 1,
          clientMessageId: 'voice_message_001',
          confirmedText: 'Edited confirmed transcript',
        )
        .toList();
    final streamed = (events.last as AgentVoiceRunCompleted).run;
    final restored = await client.getRun(runId: _runId);

    expect(streamed.status, AgentVoiceRunStatus.completed);
    expect(restored.status, AgentVoiceRunStatus.completed);
    expect(restored.assistantMessageId, _assistantMessageId);
    expect(streamed.attempt, 1);
    expect(streamed.threadId, _threadId);
    expect(streamed.inputMessageId, _messageId);
    expect(streamed.completion, isA<AgentDomainRunCompletion>());
    transport.expectDone();
  });

  test(
    'preserves the authoritative failed Run from the terminal event',
    () async {
      final failedRun = _runJson(status: 'failed')
        ..addAll(<String, Object?>{
          'failure': <String, Object?>{
            'kind': 'provider_unavailable',
            'retryable': true,
          },
          'started_at': _timestamp,
          'completed_at': _timestamp,
        });
      final transport = _ScriptedVoiceTransport([
        _Step(
          method: 'POST',
          path: '/v1/agent-voice-drafts/$_draftId/confirmations/stream',
          response: _sseResponse(<(String, Object?)>[
            (
              'input.committed',
              <String, Object?>{
                'draft': _draftJson(status: 'confirmed', confirmed: true),
                'message': _voiceMessageJson(
                  content: 'Edited confirmed transcript',
                ),
                'run': _runJson(status: 'pending'),
              },
            ),
            (
              'run.failed',
              <String, Object?>{
                'run': failedRun,
                'kind': 'provider_unavailable',
                'retryable': true,
              },
            ),
          ]),
        ),
      ]);
      final client = _client(transport);
      addTearDown(client.dispose);

      final events = await client
          .confirmDraftStream(
            draftId: _draftId,
            draftVersion: 1,
            clientMessageId: 'voice_message_001',
            confirmedText: 'Edited confirmed transcript',
          )
          .toList();

      final terminal = events.last as AgentVoiceRunFailed;
      expect(terminal.run?.status, AgentRunStatus.failed);
      expect(terminal.run?.threadId, _threadId);
      expect(terminal.run?.inputMessageId, _messageId);
      expect(terminal.run?.failureKind, 'provider_unavailable');
      transport.expectDone();
    },
  );

  test('rejects a retry response with different retry identity', () async {
    final mismatched = _runJson(
      status: 'pending',
      runId: _retryRunId,
      attempt: 2,
      retryOfRunId: _assistantMessageId,
      clientRetryId: 'voice_retry_001',
    );
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-runs/$_runId/retries',
        response: _jsonResponse(HttpStatus.accepted, mismatched),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    await expectLater(
      client.retryRun(runId: _runId, clientRetryId: 'voice_retry_001'),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidResponse,
        ),
      ),
    );
    transport.expectDone();
  });

  test('rejects a non-contiguous assistant output sequence', () async {
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'POST',
        path: '/v1/agent-voice-drafts/$_draftId/confirmations/stream',
        response: _sseResponse(<(String, Object?)>[
          (
            'input.committed',
            <String, Object?>{
              'draft': _draftJson(status: 'confirmed', confirmed: true),
              'message': _voiceMessageJson(
                content: 'Edited confirmed transcript',
              ),
              'run': _runJson(status: 'pending'),
            },
          ),
          (
            'assistant.output.started',
            <String, Object?>{
              'run_id': _runId,
              'output_id': _assistantMessageId,
            },
          ),
          (
            'assistant.output.delta',
            <String, Object?>{
              'run_id': _runId,
              'output_id': _assistantMessageId,
              'sequence': 2,
              'delta': 'Skipped sequence one.',
            },
          ),
        ]),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    await expectLater(
      client
          .confirmDraftStream(
            draftId: _draftId,
            draftVersion: 1,
            clientMessageId: 'voice_message_001',
            confirmedText: 'Edited confirmed transcript',
          )
          .toList(),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidResponse,
        ),
      ),
    );
    transport.expectDone();
  });

  test('decodes exactly four durable draft states', () async {
    final transport = _ScriptedVoiceTransport([
      for (final status in const <String>[
        'transcribing',
        'ready',
        'failed',
        'confirmed',
      ])
        _Step(
          method: 'GET',
          path: '/v1/agent-voice-drafts/$_draftId',
          response: _jsonResponse(
            HttpStatus.ok,
            _draftJson(status: status, confirmed: status == 'confirmed'),
          ),
        ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final drafts = <AgentVoiceDraft>[];
    for (var index = 0; index < 4; index++) {
      drafts.add(await client.getDraft(draftId: _draftId));
    }

    expect(drafts.map((draft) => draft.status), AgentVoiceDraftStatus.values);
    expect(drafts.take(3).every((draft) => draft.expiresAt != null), isTrue);
    final confirmed = drafts.last;
    expect(confirmed.expiresAt, isNull);
    expect(confirmed.confirmedMessageId, _messageId);
    expect(confirmed.confirmedRunId, _runId);
    expect(confirmed.messageAudioId, _audioId);
    expect(confirmed.confirmedAt, isNotNull);
    transport.expectDone();
  });

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

  test('finds an assistant Message after multimodal history', () async {
    final transport = _ScriptedVoiceTransport([
      _Step(
        method: 'GET',
        path: '/v1/agent-threads/$_threadId/messages?page_size=100',
        response: _jsonResponse(HttpStatus.ok, {
          'messages': <Object?>[
            <String, Object?>{
              'message_id': '88888888-8888-4888-8888-888888888888',
              'thread_id': _threadId,
              'sequence': 1,
              'role': 'user',
              'client_message_id': 'image-message-001',
              'modality': 'multimodal',
              'content': 'Please use this image.',
              'images': <Object?>[
                <String, Object?>{
                  'image_asset_id': '99999999-9999-4999-8999-999999999999',
                  'content_type': 'image/jpeg',
                  'size_bytes': 2048,
                  'width': 640,
                  'height': 480,
                  'status': 'ready',
                  'created_at': _timestamp,
                  'attached_at': _timestamp,
                },
              ],
              'created_at': _timestamp,
            },
            _assistantMessageJson()..['sequence'] = 2,
          ],
        }),
      ),
    ]);
    final client = _client(transport);
    addTearDown(client.dispose);

    final message = await client.getMessage(
      threadId: _threadId,
      messageId: _assistantMessageId,
    );

    expect(message?.id, _assistantMessageId);
    transport.expectDone();
  });

  test(
    'restores an assistant Message carrying a practice plan client action',
    () async {
      final assistant = _assistantMessageJson()
        ..['client_actions'] = <Object?>[
          <String, Object?>{
            'type': practicePlanConfirmClientActionType,
            'payload': <String, Object?>{
              'label': '确认并开始练习',
              'practice_plan_id': _practicePlanId,
              'plan_version': 2,
              'scene_id': 'scn_interview_project_deep_dive',
              'scene_name': '项目经历深挖',
              'user_role': '候选人',
              'ai_roles': <Object?>['面试官'],
              'practice_goal': '阿里高级 Java 开发面试',
              'practice_experience': 'INTERVIEW',
              'scene_category': 'INTERVIEW_PROFESSIONAL',
              'practice_mode': 'FULL_SIMULATION',
              'practice_scope': '围绕项目难点完成三轮追问',
              'suggested_duration_seconds': 720,
              'min_effective_turns': 3,
              'max_effective_turns': 5,
              'confirmation_prompt': '请确认是否按此方案开始练习。',
            },
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

      expect(message?.clientActions, hasLength(1));
      final action = decodeConfirmPracticePlanClientAction(
        message!.clientActions.single,
      );
      expect(action.practicePlanId, _practicePlanId);
      transport.expectDone();
    },
  );

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

Map<String, Object?> _draftJson({
  required String status,
  bool confirmed = false,
}) {
  final hasTranscript = status == 'ready' || status == 'confirmed';
  return <String, Object?>{
    'draft_id': _draftId,
    'thread_id': _threadId,
    'status': status,
    'asr_attempt': 1,
    'draft_version': 1,
    'recording': <String, Object?>{
      'content_type': 'audio/wav',
      'size_bytes': _waveBytes.length,
      'duration_ms': 3000,
      'sample_rate': 16000,
    },
    if (hasTranscript)
      'transcript': <String, Object?>{
        'text': 'Draft transcript',
        'request_id': 'request-voice-001',
        'provider': 'qwen',
        'model': 'qwen-asr',
        'language': 'en',
        'finish_reason': 'stop',
      },
    if (status == 'failed')
      'failure': <String, Object?>{
        'kind': 'provider_unavailable',
        'retryable': true,
      },
    if (!confirmed) 'expires_at': '2026-07-26T13:00:00Z',
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

Map<String, Object?> _runJson({
  required String status,
  String runId = _runId,
  String threadId = _threadId,
  String inputMessageId = _messageId,
  int attempt = 1,
  String? retryOfRunId,
  String? clientRetryId,
}) {
  return <String, Object?>{
    'run_id': runId,
    'thread_id': threadId,
    'input_message_id': inputMessageId,
    'attempt': attempt,
    'retry_of_run_id': ?retryOfRunId,
    'client_retry_id': ?clientRetryId,
    'status': status,
    'requested_provider': 'qwen',
    'requested_model': 'qwen-plus',
    'max_output_tokens': 512,
    'created_at': _timestamp,
    'updated_at': _timestamp,
  };
}

Map<String, Object?> _completedRunJson({required String completionSource}) {
  return _runJson(status: 'completed')..addAll(<String, Object?>{
    'assistant_message_id': _assistantMessageId,
    'completion_source': completionSource,
    if (completionSource == 'model') ...<String, Object?>{
      'provider_completion_id': 'completion-voice-001',
      'provider_model': 'qwen-plus',
      'finish_reason': 'stop',
      'usage': <String, Object?>{
        'input_tokens': 10,
        'output_tokens': 4,
        'total_tokens': 14,
      },
    } else ...<String, Object?>{
      'domain_tool_call_id': 'call-practice-preview-1',
      'domain_tool_name': 'practice.preview.v2',
    },
    'started_at': _timestamp,
    'completed_at': _timestamp,
  });
}

const _threadId = '11111111-1111-4111-8111-111111111111';
const _draftId = '22222222-2222-4222-8222-222222222222';
const _messageId = '33333333-3333-4333-8333-333333333333';
const _runId = '44444444-4444-4444-8444-444444444444';
const _retryRunId = '44444444-4444-4444-8444-444444444445';
const _audioId = '55555555-5555-4555-8555-555555555555';
const _assistantMessageId = '66666666-6666-4666-8666-666666666666';
const _practicePlanId = '77777777-7777-4777-8777-777777777777';
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
