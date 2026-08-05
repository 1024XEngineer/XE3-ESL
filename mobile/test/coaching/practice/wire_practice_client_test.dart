import '../../support/scene_fixtures.dart';
import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/practice/wire_practice_client.dart';

void main() {
  test(
    'accepts server-authoritative completion before the frozen max turns',
    () async {
      final transport = _Transport([
        _Step(
          method: 'GET',
          path: '/v1/practice-sessions/$_sessionId/voice-state',
          response: _json(HttpStatus.ok, {
            'practice_session_id': _sessionId,
            'practice_plan_id': 'plan-1',
            'scene_id': 'scene-project-deep-dive',
            'scene_version': 1,
            'scene_family': 'INTERVIEW',
            'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
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
      expect(snapshot.sceneFamily, _scene.family);
      expect(snapshot.sceneModel, _scene.model);
      transport.expectDone();
    },
  );

  test('accepts a practice ended before the learner answered', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/voice-state',
        response: _json(HttpStatus.ok, {
          'practice_session_id': _sessionId,
          'practice_plan_id': 'plan-1',
          'scene_id': 'scene-project-deep-dive',
          'scene_version': 1,
          'scene_family': 'INTERVIEW',
          'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
          'practice_session_status': 'ended_early',
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
      endpoints.voiceStatePath(opaque),
      '/v1/practice-sessions/$encoded/voice-state',
    );
    expect(
      endpoints.voiceActivationPath(opaque),
      '/v1/practice-sessions/$encoded/voice-activation',
    );
    expect(
      endpoints.transcribePath(opaque, opaque),
      '/v1/voice-practice-sessions/$encoded/questions/'
      '$encoded/transcription-candidates',
    );
    expect(
      endpoints.submitTextPath(opaque, opaque),
      '/v1/voice-practice-sessions/$encoded/questions/'
      '$encoded/text-answers',
    );
    expect(
      endpoints.questionTipPath(opaque, opaque),
      '/v1/voice-practice-sessions/$encoded/questions/$encoded/tips',
    );
    expect(
      endpoints.confirmPath(opaque),
      '/v1/transcription-candidates/$encoded/confirmations',
    );
    expect(
      endpoints.questionTranslationPath(opaque),
      '/v1/voice-questions/$encoded/translation',
    );
    expect(
      endpoints.endEarlyPath(opaque),
      '/v1/practice-sessions/$encoded/end-early',
    );
  });

  test('decodes one bounded Simplified Chinese question translation', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/voice-questions/$_questionId/translation',
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
            '/v1/voice-practice-sessions/$_sessionId/questions/'
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
    transport.expectDone();
  });

  test('uses the frozen #87 empty-body and raw WAV routes', () async {
    final audioFile = await _temporaryAudio();
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/voice-activation',
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
            '/v1/voice-practice-sessions/$_sessionId/questions/'
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
          'scene_family': 'INTERVIEW',
          'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
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
            '/v1/voice-practice-sessions/$_sessionId/questions/'
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

  test('submits text through the combined durable answer route', () async {
    const answer = 'I led the rollout and communicated the risk.';
    final transport = _Transport([
      _Step(
        method: 'POST',
        path:
            '/v1/voice-practice-sessions/$_sessionId/questions/'
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
          'scene_family': 'INTERVIEW',
          'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
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
          'practice_plan_id': 'plan-1',
          'plan_revision': 1,
          'scene_family': 'INTERVIEW',
          'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
          'evaluation_policy_ref': 'interview.shadow.evaluation.v1',
          'snapshot_id': 'snapshot-1',
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

  test('a practice 401 invalidates the captured Session generation', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/practice-sessions/$_sessionId/voice-state',
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
          path: '/v1/practice-sessions/$_sessionId/voice-state',
          response: const PracticeWireResponse(
            statusCode: HttpStatus.notFound,
            body: '{}',
          ),
        ),
      ]);
      final second = _Transport([
        _Step(
          method: 'GET',
          path: '/v1/practice-sessions/$_sessionId/voice-state',
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
        'audio_asset_id': 'audio-asset-1',
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

    expect(confirmation.audioAssetId, 'audio-asset-1');
    transport.expectDone();
  });

  test('accepts the server activation success status', () async {
    final response = <String, Object?>{
      ..._sessionJson(),
      'scene_id': 'scn_ielts_speaking_part_1',
      'scene_version': 1,
    };
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/voice-activation',
        response: _json(HttpStatus.ok, response),
      ),
    ]);

    final snapshot = await _client(transport).activatePractice(
      sessionId: _sessionId,
      clientOperationId: 'scene-operation',
    );

    expect(snapshot.sessionId, _sessionId);
    transport.expectDone();
  });

  test('accepts the legacy server activation created status', () async {
    final response = <String, Object?>{
      ..._sessionJson(),
      'scene_id': 'scn_ielts_speaking_part_1',
      'scene_version': 1,
    };
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/voice-activation',
        response: _json(HttpStatus.created, response),
      ),
    ]);

    final snapshot = await _client(transport).activatePractice(
      sessionId: _sessionId,
      clientOperationId: 'scene-operation',
    );

    expect(snapshot.sessionId, _sessionId);
    transport.expectDone();
  });

  test('rejects malformed server scene identity metadata', () async {
    final response = <String, Object?>{
      ..._sessionJson(),
      'scene_id': 'scn_ielts_speaking_part_1',
      'scene_version': 0,
    };
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/practice-sessions/$_sessionId/voice-activation',
        response: _json(HttpStatus.ok, response),
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
            '/v1/voice-practice-sessions/$_sessionId/questions/'
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
        path: '/v1/practice-sessions/$_sessionId/voice-state',
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
        path: '/v1/practice-sessions/$_sessionId/voice-state',
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
        path: '/v1/practice-sessions/$_sessionId/voice-state',
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
    'scene_family': 'INTERVIEW',
    'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
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
const _questionId = '40000000-0000-4000-8000-000000000088';
const _nextQuestionId = '40000000-0000-4000-8000-000000000089';
const _candidateId = '50000000-0000-4000-8000-000000000088';
const _turnId = '60000000-0000-4000-8000-000000000088';
const _timestamp = '2026-07-25T09:00:00Z';
