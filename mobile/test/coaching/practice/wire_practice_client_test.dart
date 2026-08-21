import '../../support/scene_fixtures.dart';
import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/practice/wire_practice_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  test('recorded transcription allows the server processing margin', () {
    expect(
      WirePracticeClient.defaultTranscriptionTimeout,
      const Duration(seconds: 540),
    );
  });

  test('decodes the frozen IELTS assignment from voice state', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/interaction-state',
        response: _json(HttpStatus.ok, _ieltsPart2SessionJson()),
      ),
    ]);

    final snapshot = await _client(
      transport,
    ).restorePractice(sessionId: _sessionId);

    expect(snapshot.practiceExperience, PracticeExperience.ieltsSpeaking);
    expect(snapshot.practiceMode, PracticeMode.part2);
    expect(snapshot.turnLimit, 3);
    expect(
      snapshot.ieltsAssignment?.parts.map((part) => part.part),
      const <IeltsSpeakingPart>[
        IeltsSpeakingPart.part2,
        IeltsSpeakingPart.part3,
      ],
    );
    expect(
      snapshot.ieltsAssignment?.part(IeltsSpeakingPart.part2)?.cueCard,
      'Describe a useful skill you learned.',
    );
    expect(snapshot.ieltsAssignment?.turnBlueprints, hasLength(3));
    transport.expectDone();
  });

  test(
    'rejects voice state whose IELTS assignment is absent or mismatched',
    () async {
      final invalidStates = <Map<String, Object?>>[
        _ieltsPart2SessionJson()..remove('ielts_assignment'),
        _ieltsPart2SessionJson()..['practice_mode'] = 'PART_3',
        _ieltsPart2SessionJson()..['turn_limit'] = 4,
        _sessionJson()..['ielts_assignment'] = _ieltsPart2AssignmentJson(),
      ];

      for (final state in invalidStates) {
        final client = _client(
          _Transport([
            _Step(
              method: 'GET',
              path: '/v1/practice-sessions/$_sessionId/interaction-state',
              response: _json(HttpStatus.ok, state),
            ),
          ]),
        );
        await expectLater(
          client.restorePractice(sessionId: _sessionId),
          throwsA(
            isA<PracticeClientException>().having(
              (error) => error.kind,
              'kind',
              PracticeClientFailureKind.invalidResponse,
            ),
          ),
        );
      }
    },
  );

  test(
    'accepts server-authoritative completion before the frozen max turns',
    () async {
      final transport = _Transport([
        _Step(
          method: 'GET',
          path: '/v1/practice-sessions/$_sessionId/interaction-state',
          response: _json(HttpStatus.ok, {
            'practice_session_id': _sessionId,
            'practice_plan_id': 'plan-1',
            'scene_id': 'scene-project-deep-dive',
            'scene_version': 1,
            'practice_experience': 'INTERVIEW',
            'scene_category': 'INTERVIEW_PROFESSIONAL',
            'practice_mode': 'FOCUS',
            'practice_capabilities': _practiceCapabilitiesJson,
            'practice_session_status': 'completed',
            'session_version': 5,
            'effective_turns': 4,
            'turn_limit': 6,
            'session_completed': true,
            'current_turn': {
              'turn_id': _turnId,
              'practice_session_id': _sessionId,
              'question_id': _questionId,
              'respondent_participant_id': 'participant-user',
              'candidate_id': _candidateId,
              'answer_text': 'I explained the trade-off.',
              'evidence_version': 1,
              'effective_turns': 4,
              'session_completed': true,
            },
          }),
        ),
      ]);

      final snapshot = await _client(
        transport,
      ).restorePractice(sessionId: _sessionId);

      expect(snapshot.completedTurns, 4);
      expect(snapshot.turnLimit, 6);
      expect(snapshot.sessionCompleted, isTrue);
      expect(snapshot.practiceExperience, _scene.experience);
      expect(snapshot.sceneCategory, _scene.category);
      transport.expectDone();
    },
  );

  test('accepts a practice ended before the learner answered', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/interaction-state',
        response: _json(HttpStatus.ok, {
          'practice_session_id': _sessionId,
          'practice_plan_id': 'plan-1',
          'scene_id': 'scene-project-deep-dive',
          'scene_version': 1,
          'practice_experience': 'INTERVIEW',
          'scene_category': 'INTERVIEW_PROFESSIONAL',
          'practice_mode': 'FOCUS',
          'practice_session_status': 'ended_early',
          'practice_capabilities': _practiceCapabilitiesJson,
          'session_version': 2,
          'effective_turns': 0,
          'turn_limit': 6,
          'session_completed': true,
        }),
      ),
    ]);

    final snapshot = await _client(
      transport,
    ).restorePractice(sessionId: _sessionId);

    expect(snapshot.completedTurns, 0);
    expect(snapshot.sessionCompleted, isTrue);
    expect(snapshot.currentQuestion, isNull);
    expect(snapshot.currentTurn, isNull);
    transport.expectDone();
  });

  test('encodes every opaque resource ID as one path segment', () {
    const endpoints = PracticeWireEndpoints();
    const opaque = 'resource/part?query#fragment%value';
    const encoded = 'resource%2Fpart%3Fquery%23fragment%25value';

    expect(
      endpoints.interactionStatePath(opaque),
      '/v1/practice-sessions/$encoded/interaction-state',
    );
    expect(
      endpoints.activationPath(opaque),
      '/v1/practice-sessions/$encoded/activation',
    );
    expect(
      endpoints.transcribePath(opaque, opaque),
      '/v1/practice-sessions/$encoded/questions/'
      '$encoded/transcription-candidates',
    );
    expect(
      endpoints.transcribeRealtimePath(opaque, opaque),
      '/v1/practice-sessions/$encoded/questions/'
      '$encoded/transcription-candidates/realtime',
    );
    expect(
      endpoints.submitTextPath(opaque, opaque),
      '/v1/practice-sessions/$encoded/questions/'
      '$encoded/text-answers',
    );
    expect(
      endpoints.questionTipPath(opaque, opaque),
      '/v1/practice-sessions/$encoded/questions/$encoded/tips',
    );
    expect(
      endpoints.confirmPath(opaque),
      '/v1/transcription-candidates/$encoded/confirmations',
    );
    expect(
      endpoints.questionTranslationPath(opaque),
      '/v1/practice-questions/$encoded/translation',
    );
    expect(
      endpoints.endEarlyPath(opaque),
      '/v1/practice-sessions/$encoded/end-early',
    );
    expect(
      endpoints.completePath(opaque),
      '/v1/practice-sessions/$encoded/complete',
    );
  });

  test('decodes one bounded Simplified Chinese question translation', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-questions/$_questionId/translation',
        response: _json(HttpStatus.ok, {
          'question_id': _questionId,
          'target_language': 'zh-CN',
          'translation': '请介绍一次你解决团队分歧的经历。',
        }),
      ),
    ]);

    final translation = await _client(
      transport,
    ).translateQuestion(questionId: _questionId);

    expect(translation.questionId, _questionId);
    expect(translation.targetLanguage, 'zh-CN');
    expect(translation.content, '请介绍一次你解决团队分歧的经历。');
    transport.expectDone();
  });

  test('requests and decodes a Question Tip without an answer body', () async {
    final transport = _Transport([
      _Step(
        method: 'POST',
        path:
            '/v1/practice-sessions/$_sessionId/questions/'
            '$_questionId/tips',
        verify: (request) {
          expect(request.jsonBody, isNull);
          expect(request.rawFilePath, isNull);
          expect(request.headers['Idempotency-Key'], 'question-tip-operation');
        },
        response: _json(HttpStatus.ok, {
          'tip_id': 'question-tip-1',
          'practice_session_id': _sessionId,
          'question_id': _questionId,
          'content':
              'I would clarify the goal first. Then I would explain my approach clearly.',
          'translation': '我会先确认目标，然后清晰地说明我的方法。',
          'created_at': _timestamp,
        }),
      ),
    ]);

    final tip = await _client(transport).ensureQuestionTip(
      sessionId: _sessionId,
      questionId: _questionId,
      idempotencyKey: 'question-tip-operation',
    );

    expect(tip.id, 'question-tip-1');
    expect(tip.questionId, _questionId);
    expect(tip.content, contains('clarify the goal'));
    expect(tip.translation, '我会先确认目标，然后清晰地说明我的方法。');
    transport.expectDone();
  });

  test('uses the frozen #87 empty-body and raw WAV routes', () async {
    final audioFile = await _temporaryAudio();
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/activation',
        verify: (request) {
          expect(request.jsonBody, isNull);
          expect(request.rawFilePath, isNull);
          expect(request.headers['Idempotency-Key'], 'scene-operation');
        },
        response: _json(HttpStatus.ok, _sessionJson()),
      ),
      _Step(
        method: 'POST',
        path:
            '/v1/practice-sessions/$_sessionId/questions/'
            '$_questionId/transcription-candidates',
        verify: (request) {
          expect(request.jsonBody, isNull);
          expect(request.rawFilePath, audioFile.path);
          expect(request.headers[HttpHeaders.contentTypeHeader], 'audio/wav');
          expect(request.headers['Idempotency-Key'], 'turn-operation');
        },
        response: _json(HttpStatus.created, {
          'candidate_id': _candidateId,
          'practice_session_id': _sessionId,
          'question_id': _questionId,
          'respondent_participant_id': 'participant-user',
          'transcript_id': 'transcript-1',
          'evidence_version': 1,
          'transcript': 'I led the migration safely.',
          'created_at': _timestamp,
        }),
      ),
      _Step(
        method: 'POST',
        path: '/v1/transcription-candidates/$_candidateId/confirmations',
        verify: (request) {
          expect(request.jsonBody, isNull);
          expect(request.rawFilePath, isNull);
          expect(request.headers['Idempotency-Key'], 'confirm-operation');
          expect(
            request.headers.keys.where((key) => key.contains('participant')),
            isEmpty,
          );
        },
        response: _json(HttpStatus.ok, {
          'practice_session_id': _sessionId,
          'practice_plan_id': 'plan-1',
          'scene_id': 'scene-project-deep-dive',
          'scene_version': 1,
          'practice_experience': 'INTERVIEW',
          'scene_category': 'INTERVIEW_PROFESSIONAL',
          'practice_mode': 'FOCUS',
          'practice_capabilities': _practiceCapabilitiesJson,
          'practice_session_status': 'in_progress',
          'session_version': 2,
          'effective_turns': 1,
          'turn_limit': 3,
          'session_completed': false,
          'current_question': {
            'question_id': _nextQuestionId,
            'practice_session_id': _sessionId,
            'content': 'What trade-off did you make?',
            'speaker_participant_id': 'participant-agent',
            'addressee_participant_ids': ['participant-user'],
            'speech_path': '/v1/questions/$_nextQuestionId/speech',
          },
          'current_turn': {
            'turn_id': _turnId,
            'practice_session_id': _sessionId,
            'question_id': _questionId,
            'respondent_participant_id': 'participant-user',
            'candidate_id': _candidateId,
            'answer_text': 'I led the migration safely.',
            'evidence_version': 1,
            'effective_turns': 1,
            'session_completed': false,
          },
        }),
      ),
    ]);
    final client = _client(transport);
    final start = await client.activatePractice(
      sessionId: _sessionId,
      clientOperationId: 'scene-operation',
    );
    expect(start.sessionId, _sessionId);
    expect(start.currentQuestion?.id, _questionId);

    final candidate = await client.transcribe(
      PracticeTranscriptionRequest(
        sessionId: _sessionId,
        questionId: _questionId,
        clientTurnId: 'turn-operation',
        audio: RecordedPracticeAudio(
          path: audioFile.path,
          contentType: 'audio/wav',
          sizeBytes: 64044,
        ),
      ),
    );
    final confirmation = await client.confirm(
      sessionId: _sessionId,
      questionId: _questionId,
      candidateId: candidate.id,
      idempotencyKey: 'confirm-operation',
    );

    expect(confirmation.turnId, _turnId);
    expect(confirmation.nextQuestion?.id, _nextQuestionId);
    expect(confirmation.completedTurns, 1);
    transport.expectDone();
  });

  test('uploads a full 120-second IELTS Part 2 WAV', () async {
    const fullPart2WavBytes = 3_840_044;
    final audioFile = await _temporaryAudio(sizeBytes: fullPart2WavBytes);
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final transport = _Transport([
      _Step(
        method: 'POST',
        path:
            '/v1/practice-sessions/$_sessionId/questions/'
            '$_questionId/transcription-candidates',
        verify: (request) {
          expect(request.rawFilePath, audioFile.path);
          expect(request.headers[HttpHeaders.contentTypeHeader], 'audio/wav');
          expect(request.headers['Idempotency-Key'], 'part-2-turn');
        },
        response: _json(HttpStatus.created, {
          'candidate_id': _candidateId,
          'practice_session_id': _sessionId,
          'question_id': _questionId,
          'respondent_participant_id': 'participant-user',
          'transcript_id': 'part-2-transcript',
          'evidence_version': 1,
          'transcript': 'I would like to describe my experience.',
          'created_at': _timestamp,
        }),
      ),
    ]);

    final candidate = await _client(transport).transcribe(
      PracticeTranscriptionRequest(
        sessionId: _sessionId,
        questionId: _questionId,
        clientTurnId: 'part-2-turn',
        audio: RecordedPracticeAudio(
          path: audioFile.path,
          contentType: 'audio/wav',
          sizeBytes: fullPart2WavBytes,
        ),
      ),
    );

    expect(candidate.id, _candidateId);
    transport.expectDone();
  });

  test('streams a Practice transcript before PCM capture finishes', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    final handled = Completer<void>();
    server.listen((request) async {
      expect(
        request.uri.path,
        '/v1/practice-sessions/$_sessionId/questions/'
        '$_questionId/transcription-candidates/realtime',
      );
      expect(
        request.headers.value(HttpHeaders.authorizationHeader),
        'Bearer sess_practice',
      );
      final socket = await WebSocketTransformer.upgrade(
        request,
        protocolSelector: (protocols) => 'speakup.voice-input.v1',
      );
      var sentUpdate = false;
      await for (final message in socket) {
        if (message is List<int> && !sentUpdate) {
          sentUpdate = true;
          socket.add(
            jsonEncode(<String, Object>{
              'type': 'transcription.updated',
              'data': <String, Object>{
                'transcript': 'I led the migration',
                'final': false,
              },
            }),
          );
        }
        if (message is String &&
            (jsonDecode(message) as Map<String, dynamic>)['type'] == 'finish') {
          socket.add(
            jsonEncode(<String, Object>{
              'type': 'candidate.ready',
              'data': <String, Object>{
                'candidate': <String, Object>{
                  'candidate_id': _candidateId,
                  'practice_session_id': _sessionId,
                  'question_id': _questionId,
                  'respondent_participant_id': 'participant-user',
                  'transcript_id': 'transcript-realtime',
                  'evidence_version': 1,
                  'transcript': 'I led the migration safely.',
                  'created_at': _timestamp,
                },
              },
            }),
          );
          await socket.close();
          if (!handled.isCompleted) {
            handled.complete();
          }
        }
      }
    });
    final client = WirePracticeClient(
      baseUri: Uri.parse('http://${server.address.address}:${server.port}'),
      credentialProvider: () => _credential,
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: _Transport(const <_Step>[]),
    );
    final chunks = StreamController<Uint8List>();
    final update = Completer<PracticeTranscriptUpdated>();
    final events = <PracticeTranscriptionEvent>[];
    final completed = Completer<void>();
    final subscription = client
        .transcribeRealtime(
          sessionId: _sessionId,
          questionId: _questionId,
          idempotencyKey: 'turn-realtime-001',
          audioChunks: chunks.stream,
        )
        .listen(
          (event) {
            events.add(event);
            if (event case final PracticeTranscriptUpdated value) {
              if (!update.isCompleted) {
                update.complete(value);
              }
            }
          },
          onError: completed.completeError,
          onDone: completed.complete,
        );
    addTearDown(subscription.cancel);

    chunks.add(Uint8List.fromList(<int>[1, 2]));
    final firstUpdate = await update.future.timeout(const Duration(seconds: 2));
    expect(firstUpdate.text, 'I led the migration');
    expect(completed.isCompleted, isFalse);

    chunks.add(Uint8List.fromList(<int>[3, 4]));
    await chunks.close();
    await completed.future;
    await handled.future;

    expect(events.last, isA<PracticeCandidateCompleted>());
    expect(
      (events.last as PracticeCandidateCompleted).candidate.text,
      'I led the migration safely.',
    );
  });

  test('submits text through the combined durable answer route', () async {
    const answer = 'I led the rollout and communicated the risk.';
    final transport = _Transport([
      _Step(
        method: 'POST',
        path:
            '/v1/practice-sessions/$_sessionId/questions/'
            '$_questionId/text-answers',
        verify: (request) {
          expect(request.rawFilePath, isNull);
          expect(jsonDecode(request.jsonBody!), {'answer_text': answer});
          expect(request.headers['Idempotency-Key'], 'text-operation');
        },
        response: _json(HttpStatus.ok, {
          'practice_session_id': _sessionId,
          'practice_plan_id': 'plan-1',
          'scene_id': 'scene-project-deep-dive',
          'scene_version': 1,
          'practice_experience': 'INTERVIEW',
          'scene_category': 'INTERVIEW_PROFESSIONAL',
          'practice_mode': 'FOCUS',
          'practice_capabilities': _practiceCapabilitiesJson,
          'practice_session_status': 'in_progress',
          'session_version': 2,
          'effective_turns': 1,
          'turn_limit': 3,
          'session_completed': false,
          'current_question': {
            'question_id': _nextQuestionId,
            'practice_session_id': _sessionId,
            'content': 'What did you learn?',
            'speaker_participant_id': 'participant-agent',
            'addressee_participant_ids': ['participant-user'],
            'speech_path': '/v1/questions/$_nextQuestionId/speech',
          },
          'current_turn': {
            'turn_id': _turnId,
            'practice_session_id': _sessionId,
            'question_id': _questionId,
            'respondent_participant_id': 'participant-user',
            'candidate_id': 'text-candidate-1',
            'answer_text': answer,
            'evidence_version': 1,
            'effective_turns': 1,
            'session_completed': false,
          },
        }),
      ),
    ]);

    final confirmation = await _client(transport).submitText(
      sessionId: _sessionId,
      questionId: _questionId,
      answerText: '  $answer  ',
      idempotencyKey: 'text-operation',
    );

    expect(confirmation.answer.text, answer);
    expect(confirmation.candidateId, 'text-candidate-1');
    expect(confirmation.nextQuestion?.id, _nextQuestionId);
    transport.expectDone();
  });

  test('ends the exact active session with its current version', () async {
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/end-early',
        verify: (request) {
          expect(request.rawFilePath, isNull);
          expect(jsonDecode(request.jsonBody!), {
            'expected_session_version': 3,
          });
          expect(request.headers['Idempotency-Key'], 'end-practice-operation');
        },
        response: _json(HttpStatus.ok, {
          'practice_session_id': _sessionId,
          'practice_plan_id': _planId,
          'plan_version': 1,
          'practice_experience': 'INTERVIEW',
          'scene_category': 'INTERVIEW_PROFESSIONAL',
          'practice_mode': 'FOCUS',
          'evaluation_policy_ref': 'interview.shadow.evaluation.v1',
          'practice_session_status': 'ended_early',
          'session_version': 4,
          'started_at': '2026-07-25T09:00:01Z',
          'ended_at': '2026-07-25T09:10:00Z',
          'end_reason': 'USER_ENDED',
          'created_at': _timestamp,
        }),
      ),
    ]);

    final lifecycle = await _client(transport).endEarly(
      sessionId: _sessionId,
      expectedSessionVersion: 3,
      idempotencyKey: 'end-practice-operation',
    );

    expect(lifecycle.sessionId, _sessionId);
    expect(lifecycle.status, PracticeSessionLifecycleStatus.endedEarly);
    expect(lifecycle.version, 4);
    transport.expectDone();
  });

  test(
    'completes a user-controlled session with its current version',
    () async {
      final transport = _Transport([
        _Step(
          method: 'POST',
          path: '/v1/practice-sessions/$_sessionId/complete',
          verify: (request) {
            expect(jsonDecode(request.jsonBody!), {
              'expected_session_version': 8,
            });
            expect(
              request.headers['Idempotency-Key'],
              'complete-practice-operation',
            );
          },
          response: _json(HttpStatus.ok, {
            'practice_session_id': _sessionId,
            'practice_plan_id': _planId,
            'plan_version': 1,
            'practice_experience': 'LIFE_AND_TRAVEL',
            'scene_category': 'LIFE_TRAVEL',
            'practice_mode': 'FULL_SIMULATION',
            'evaluation_policy_ref': 'scenario.shadow.evaluation.v1',
            'practice_session_status': 'completed',
            'session_version': 9,
            'started_at': '2026-07-25T09:00:01Z',
            'ended_at': '2026-07-25T09:10:00Z',
            'end_reason': 'USER_COMPLETED',
            'created_at': _timestamp,
          }),
        ),
      ]);

      final lifecycle = await _client(transport).complete(
        sessionId: _sessionId,
        expectedSessionVersion: 8,
        idempotencyKey: 'complete-practice-operation',
      );

      expect(lifecycle.status, PracticeSessionLifecycleStatus.completed);
      expect(lifecycle.version, 9);
      transport.expectDone();
    },
  );

  test('a practice 401 invalidates the captured Session generation', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/interaction-state',
        response: const PracticeWireResponse(
          statusCode: HttpStatus.unauthorized,
          body: '{}',
        ),
      ),
    ]);
    final invalidations = <AuthSessionCredential>[];
    final client = WirePracticeClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => _credential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) async {
            invalidations.add(
              AuthSessionCredential(
                sessionToken: expectedSessionToken,
                generation: expectedGeneration,
              ),
            );
          },
      transport: transport,
    );

    await expectLater(
      client.restorePractice(sessionId: _sessionId),
      throwsA(isA<Exception>()),
    );
    await Future<void>.delayed(Duration.zero);

    expect(invalidations, hasLength(1));
    expect(invalidations.single.sessionToken, _credential.sessionToken);
    expect(invalidations.single.generation, _credential.generation);
  });

  test('transcription has a timeout independent from JSON calls', () async {
    final audioFile = await _temporaryAudio();
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final transport = _NeverCompletesTransport();
    final client = WirePracticeClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => _credential,
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
      jsonTimeout: const Duration(seconds: 5),
      transcriptionTimeout: const Duration(milliseconds: 1),
    );

    await expectLater(
      client.transcribe(
        PracticeTranscriptionRequest(
          sessionId: _sessionId,
          questionId: _questionId,
          clientTurnId: 'turn-timeout',
          audio: RecordedPracticeAudio(
            path: audioFile.path,
            contentType: 'audio/wav',
            sizeBytes: 64044,
          ),
        ),
      ),
      throwsA(isA<Exception>()),
    );
  });

  test(
    'account cleanup rebuilds an owned transport for the next login',
    () async {
      final first = _Transport([
        _Step(
          method: 'GET',
          path: '/v1/practice-sessions/$_sessionId/interaction-state',
          response: const PracticeWireResponse(
            statusCode: HttpStatus.notFound,
            body: '{}',
          ),
        ),
      ]);
      final second = _Transport([
        _Step(
          method: 'GET',
          path: '/v1/practice-sessions/$_sessionId/interaction-state',
          response: const PracticeWireResponse(
            statusCode: HttpStatus.notFound,
            body: '{}',
          ),
        ),
      ]);
      final transports = Queue<PracticeWireTransport>.of([first, second]);
      final client = WirePracticeClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        credentialProvider: () => _credential,
        invalidateSession:
            ({
              required expectedSessionToken,
              required expectedGeneration,
            }) async {},
        transportFactory: () => transports.removeFirst(),
      );

      await expectLater(
        client.restorePractice(sessionId: _sessionId),
        throwsA(isA<PracticeClientException>()),
      );
      await client.clearAccountState();
      expect(first.closed, isTrue);
      await expectLater(
        client.restorePractice(sessionId: _sessionId),
        throwsA(isA<PracticeClientException>()),
      );
      expect(second.closed, isFalse);
    },
  );

  test('rejects an explicit null optional audio asset', () async {
    final state = <String, Object?>{
      ..._sessionJson(),
      'session_version': 2,
      'effective_turns': 1,
      'current_question': {
        'question_id': _nextQuestionId,
        'practice_session_id': _sessionId,
        'content': 'What trade-off did you make?',
        'speaker_participant_id': 'participant-agent',
        'addressee_participant_ids': ['participant-user'],
        'speech_path': '/v1/questions/$_nextQuestionId/speech',
      },
      'current_turn': {
        'turn_id': _turnId,
        'practice_session_id': _sessionId,
        'question_id': _questionId,
        'respondent_participant_id': 'participant-user',
        'candidate_id': _candidateId,
        'answer_text': 'I led the migration safely.',
        'evidence_version': 1,
        'effective_turns': 1,
        'session_completed': false,
        'audio_asset_id': null,
      },
    };
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/transcription-candidates/$_candidateId/confirmations',
        response: _json(HttpStatus.ok, state),
      ),
    ]);

    await expectLater(
      _client(transport).confirm(
        sessionId: _sessionId,
        questionId: _questionId,
        candidateId: _candidateId,
        idempotencyKey: 'confirm-operation',
      ),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.invalidResponse,
        ),
      ),
    );
    transport.expectDone();
  });

  test('retains a valid optional audio asset', () async {
    final state = <String, Object?>{
      ..._sessionJson(),
      'session_version': 2,
      'effective_turns': 1,
      'current_question': {
        'question_id': _nextQuestionId,
        'practice_session_id': _sessionId,
        'content': 'What trade-off did you make?',
        'speaker_participant_id': 'participant-agent',
        'addressee_participant_ids': ['participant-user'],
        'speech_path': '/v1/questions/$_nextQuestionId/speech',
      },
      'current_turn': {
        'turn_id': _turnId,
        'practice_session_id': _sessionId,
        'question_id': _questionId,
        'respondent_participant_id': 'participant-user',
        'candidate_id': _candidateId,
        'answer_text': 'I led the migration safely.',
        'evidence_version': 1,
        'effective_turns': 1,
        'session_completed': false,
        'audio_asset_id': '00000000-0000-4000-8000-000000000001',
      },
    };
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/transcription-candidates/$_candidateId/confirmations',
        response: _json(HttpStatus.ok, state),
      ),
    ]);

    final confirmation = await _client(transport).confirm(
      sessionId: _sessionId,
      questionId: _questionId,
      candidateId: _candidateId,
      idempotencyKey: 'confirm-operation',
    );

    expect(confirmation.audioAssetId, '00000000-0000-4000-8000-000000000001');
    transport.expectDone();
  });

  test('does not accept a noncanonical success status', () async {
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/activation',
        response: _json(HttpStatus.created, _sessionJson()),
      ),
    ]);

    await expectLater(
      _client(transport).activatePractice(
        sessionId: _sessionId,
        clientOperationId: 'scene-operation',
      ),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('rejects provisional candidate fields outside the frozen DTO', () async {
    final audioFile = await _temporaryAudio();
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final transport = _Transport([
      _Step(
        method: 'POST',
        path:
            '/v1/practice-sessions/$_sessionId/questions/'
            '$_questionId/transcription-candidates',
        response: _json(HttpStatus.created, {
          'candidate_id': _candidateId,
          'practice_session_id': _sessionId,
          'question_id': _questionId,
          'respondent_participant_id': 'participant-user',
          'transcript_id': 'transcript-1',
          'evidence_version': 1,
          'transcript': 'I led the migration safely.',
          'created_at': _timestamp,
          'provider': 'provisional-field',
        }),
      ),
    ]);

    await expectLater(
      _client(transport).transcribe(
        PracticeTranscriptionRequest(
          sessionId: _sessionId,
          questionId: _questionId,
          clientTurnId: 'turn-operation',
          audio: RecordedPracticeAudio(
            path: audioFile.path,
            contentType: 'audio/wav',
            sizeBytes: 64044,
          ),
        ),
      ),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('strictly parses the standard error and Retry-After', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/interaction-state',
        response: _json(HttpStatus.tooManyRequests, {
          'error': {
            'code': 'voice_rate_limited',
            'message': 'Retry later.',
            'retryable': true,
            'correlation_id': 'corr-88',
          },
        }).copyWith(headers: const {'Retry-After': '7'}),
      ),
    ]);

    await expectLater(
      _client(transport).restorePractice(sessionId: _sessionId),
      throwsA(
        isA<PracticeClientException>()
            .having(
              (error) => error.kind,
              'kind',
              PracticeClientFailureKind.rateLimited,
            )
            .having(
              (error) => error.errorCode,
              'errorCode',
              'voice_rate_limited',
            )
            .having((error) => error.correlationId, 'correlationId', 'corr-88')
            .having(
              (error) => error.retryAfter,
              'retryAfter',
              const Duration(seconds: 7),
            ),
      ),
    );
  });

  test('explicit retryable false overrides 429 and Retry-After', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/interaction-state',
        response: _json(HttpStatus.tooManyRequests, {
          'error': {
            'code': 'quota_exhausted',
            'message': 'Quota exhausted.',
            'retryable': false,
            'correlation_id': 'corr-88',
          },
        }).copyWith(headers: const {'Retry-After': '7'}),
      ),
    ]);

    await expectLater(
      _client(transport).restorePractice(sessionId: _sessionId),
      throwsA(
        isA<PracticeClientException>()
            .having((error) => error.retryable, 'retryable', isFalse)
            .having(
              (error) => error.retryAfter,
              'retryAfter',
              const Duration(seconds: 7),
            ),
      ),
    );
  });

  test('missing retryable violates the frozen Error DTO', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/interaction-state',
        response: _json(HttpStatus.tooManyRequests, {
          'error': {
            'code': 'voice_rate_limited',
            'message': 'Retry later.',
            'correlation_id': 'corr-88',
          },
        }),
      ),
    ]);

    await expectLater(
      _client(transport).restorePractice(sessionId: _sessionId),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.invalidResponse,
        ),
      ),
    );
  });
}

Future<File> _temporaryAudio({int sizeBytes = 64044}) async {
  final directory = await Directory.systemTemp.createTemp(
    'speakup-wire-audio-',
  );
  final file = File('${directory.path}/turn.wav');
  await file.writeAsBytes(List<int>.filled(sizeBytes, 1));
  return file;
}

WirePracticeClient _client(PracticeWireTransport transport) {
  return WirePracticeClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => _credential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

Map<String, Object?> _sessionJson() {
  return {
    'practice_session_id': _sessionId,
    'practice_plan_id': 'plan-1',
    'scene_id': 'scene-project-deep-dive',
    'scene_version': 1,
    'practice_experience': 'INTERVIEW',
    'scene_category': 'INTERVIEW_PROFESSIONAL',
    'practice_mode': 'FOCUS',
    'practice_capabilities': _practiceCapabilitiesJson,
    'practice_session_status': 'in_progress',
    'session_version': 1,
    'effective_turns': 0,
    'turn_limit': 3,
    'session_completed': false,
    'current_question': {
      'question_id': _questionId,
      'practice_session_id': _sessionId,
      'content': 'Tell me about your project.',
      'speaker_participant_id': 'participant-agent',
      'addressee_participant_ids': ['participant-user'],
      'speech_path': '/v1/questions/$_questionId/speech',
    },
  };
}

Map<String, Object?> _ieltsPart2SessionJson() => <String, Object?>{
  ..._sessionJson(),
  'practice_experience': 'IELTS_SPEAKING',
  'scene_category': 'IELTS_SPEAKING',
  'practice_mode': 'PART_2',
  'turn_limit': 3,
  'ielts_assignment': _ieltsPart2AssignmentJson(),
};

Map<String, Object?> _ieltsPart2AssignmentJson() => <String, Object?>{
  'bank_id': 'ielts-bank-test',
  'season': '2026-08',
  'mode': 'PART_2',
  'parts': <Object?>[
    <String, Object?>{
      'part': 'PART_2',
      'source_id': 'topic-group-test',
      'topic_title': 'Learning skills',
      'cue_card': 'Describe a useful skill you learned.',
      'turn_blueprints': <String>['Describe a useful skill you learned.'],
    },
    <String, Object?>{
      'part': 'PART_3',
      'source_id': 'topic-group-test',
      'topic_title': 'Learning skills',
      'turn_blueprints': <String>[
        'Why do people learn new skills?',
        'Should employers support learning?',
      ],
    },
  ],
};

const _practiceCapabilitiesJson = <String, Object?>{
  'retry_allowed': true,
  'question_translation_allowed': true,
  'question_tips_allowed': true,
  'speech_feedback_allowed': false,
};

PracticeWireResponse _json(int statusCode, Object body) {
  return PracticeWireResponse(statusCode: statusCode, body: jsonEncode(body));
}

extension on PracticeWireResponse {
  PracticeWireResponse copyWith({Map<String, String>? headers}) {
    return PracticeWireResponse(
      statusCode: statusCode,
      body: body,
      headers: headers ?? this.headers,
    );
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
  final PracticeWireResponse response;
  final void Function(PracticeWireRequest request)? verify;
}

final class _Transport implements PracticeWireTransport {
  _Transport(Iterable<_Step> steps) : _steps = Queue<_Step>.of(steps);

  final Queue<_Step> _steps;
  bool closed = false;

  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) async {
    final step = _steps.removeFirst();
    expect(request.method, step.method);
    expect(request.uri.path, step.path);
    expect(
      request.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_practice',
    );
    step.verify?.call(request);
    return step.response;
  }

  void expectDone() => expect(_steps, isEmpty);

  @override
  void close({bool force = false}) {
    closed = true;
  }
}

final class _NeverCompletesTransport implements PracticeWireTransport {
  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) {
    return Completer<PracticeWireResponse>().future;
  }

  @override
  void close({bool force = false}) {}
}

final _scene = testScenes[2];

const _credential = AuthSessionCredential(
  sessionToken: 'sess_practice',
  generation: 7,
);
const _sessionId = '30000000-0000-4000-8000-000000000088';
const _planId = '20000000-0000-4000-8000-000000000088';
const _questionId = '40000000-0000-4000-8000-000000000088';
const _nextQuestionId = '40000000-0000-4000-8000-000000000089';
const _candidateId = '50000000-0000-4000-8000-000000000088';
const _turnId = '60000000-0000-4000-8000-000000000088';
const _timestamp = '2026-07-25T09:00:00Z';
