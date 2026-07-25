import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/practice/wire_practice_client.dart';

void main() {
  test('uses the frozen #87 empty-body and raw WAV routes', () async {
    final audioFile = await _temporaryAudio();
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/agent-threads/$_threadId/voice-practice-sessions',
        verify: (request) {
          expect(request.jsonBody, isNull);
          expect(request.rawFilePath, isNull);
          expect(request.headers['Idempotency-Key'], 'scene-operation');
        },
        response: _json(HttpStatus.created, _sessionJson()),
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
          'thread_id': _threadId,
          'matter': _matterJson(),
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
    final matter = AgentMatter(id: _matterId, scene: agentScenes.first);

    final start = await client.startPractice(
      threadId: _threadId,
      activeMatter: matter,
      clientOperationId: 'scene-operation',
    );
    expect(start.snapshot.sessionId, _sessionId);
    expect(start.snapshot.sessionId, isNot(_threadId));
    expect(start.snapshot.currentQuestion?.id, _questionId);

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

  test('a practice 401 invalidates the captured Session generation', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/agent-threads/$_threadId/voice-practice-session',
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
      client.restorePractice(threadId: _threadId),
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
          path: '/v1/agent-threads/$_threadId/voice-practice-session',
          response: const PracticeWireResponse(
            statusCode: HttpStatus.notFound,
            body: '{}',
          ),
        ),
      ]);
      final second = _Transport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/voice-practice-session',
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

      expect(await client.restorePractice(threadId: _threadId), isNull);
      await client.clearAccountState();
      expect(first.closed, isTrue);
      expect(await client.restorePractice(threadId: _threadId), isNull);
      expect(second.closed, isFalse);
    },
  );

  test(
    'maps a completed formal Review from the canonical root state',
    () async {
      final transport = _Transport([
        _Step(
          method: 'POST',
          path: '/v1/transcription-candidates/$_candidateId/confirmations',
          response: _json(HttpStatus.ok, {
            'practice_session_id': _sessionId,
            'practice_plan_id': 'plan-1',
            'thread_id': _threadId,
            'matter': _matterJson(),
            'session_version': 4,
            'effective_turns': 3,
            'turn_limit': 3,
            'session_completed': true,
            'current_turn': {
              'turn_id': _turnId,
              'practice_session_id': _sessionId,
              'question_id': _questionId,
              'respondent_participant_id': 'participant-user',
              'candidate_id': _candidateId,
              'answer_text': 'I led the migration safely.',
              'evidence_version': 2,
              'effective_turns': 3,
              'session_completed': true,
              'review_id': 'review-1',
            },
            'review': {
              'review_id': 'review-1',
              'practice_session_id': _sessionId,
              'status': 'completed',
              'implementation_version': 'review-v1',
              'source_turn_id': _turnId,
              'source_turn_version': 'conversation-turn:evidence-v2',
              'created_at': _timestamp,
              'updated_at': '2026-07-25T09:00:02Z',
              'result': {
                'overall_score': 88,
                'summary': 'Clear and evidence based.',
                'conclusions': [
                  {
                    'key': 'clarity',
                    'category': 'strength',
                    'message': 'You gave a concrete result.',
                  },
                  {
                    'key': 'scope',
                    'category': 'focus',
                    'message': 'Make the trade-off more explicit.',
                    'suggestion': 'Name the rejected alternative.',
                  },
                ],
              },
              'completed_at': '2026-07-25T09:00:03Z',
            },
          }),
        ),
      ]);

      final confirmation = await _client(transport).confirm(
        sessionId: _sessionId,
        questionId: _questionId,
        candidateId: _candidateId,
        idempotencyKey: 'confirm-operation',
      );

      expect(confirmation.sessionCompleted, isTrue);
      expect(confirmation.review?.id, 'review-1');
      expect(confirmation.review?.title, '本次练习 · 88 分');
      expect(confirmation.review?.strength, 'You gave a concrete result.');
      expect(confirmation.review?.nextFocus, 'Name the rejected alternative.');
    },
  );

  test(
    'accepts a nullable future audio asset without making it required',
    () async {
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

      final confirmation = await _client(transport).confirm(
        sessionId: _sessionId,
        questionId: _questionId,
        candidateId: _candidateId,
        idempotencyKey: 'confirm-operation',
      );

      expect(confirmation.completedTurns, 1);
      expect(confirmation.review, isNull);
      transport.expectDone();
    },
  );

  test('does not accept a noncanonical success status', () async {
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/agent-threads/$_threadId/voice-practice-sessions',
        response: _json(HttpStatus.ok, _sessionJson()),
      ),
    ]);

    await expectLater(
      _client(transport).startPractice(
        threadId: _threadId,
        activeMatter: AgentMatter(id: _matterId, scene: agentScenes.first),
        clientOperationId: 'scene-operation',
      ),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.unexpected,
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
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('rejects inconsistent canonical Review references', () async {
    final completed = <String, Object?>{
      ..._sessionJson(),
      'session_version': 4,
      'effective_turns': 3,
      'session_completed': true,
      'current_turn': {
        'turn_id': _turnId,
        'practice_session_id': _sessionId,
        'question_id': _questionId,
        'respondent_participant_id': 'participant-user',
        'candidate_id': _candidateId,
        'answer_text': 'I led the migration safely.',
        'evidence_version': 2,
        'effective_turns': 3,
        'session_completed': true,
        'review_id': 'review-turn',
      },
      'review': {
        'review_id': 'review-other',
        'practice_session_id': _sessionId,
        'status': 'pending',
        'implementation_version': 'review-v1',
        'source_turn_id': _turnId,
        'source_turn_version': 'conversation-turn:evidence-v2',
        'created_at': _timestamp,
        'updated_at': _timestamp,
      },
    };
    completed.remove('current_question');
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/agent-threads/$_threadId/voice-practice-session',
        response: _json(HttpStatus.ok, completed),
      ),
    ]);

    await expectLater(
      _client(transport).restorePractice(threadId: _threadId),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('rejects a noncanonical FormalReview source turn version', () async {
    final completed = <String, Object?>{
      ..._sessionJson(),
      'session_version': 4,
      'effective_turns': 3,
      'session_completed': true,
      'current_turn': {
        'turn_id': _turnId,
        'practice_session_id': _sessionId,
        'question_id': _questionId,
        'respondent_participant_id': 'participant-user',
        'candidate_id': _candidateId,
        'answer_text': 'I led the migration safely.',
        'evidence_version': 2,
        'effective_turns': 3,
        'session_completed': true,
        'review_id': 'review-1',
      },
      'review': {
        'review_id': 'review-1',
        'practice_session_id': _sessionId,
        'status': 'pending',
        'implementation_version': 'review-v1',
        'source_turn_id': _turnId,
        'source_turn_version': 'conversation-turn:evidence-v02',
        'created_at': _timestamp,
        'updated_at': _timestamp,
      },
    };
    completed.remove('current_question');
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/agent-threads/$_threadId/voice-practice-session',
        response: _json(HttpStatus.ok, completed),
      ),
    ]);

    await expectLater(
      _client(transport).restorePractice(threadId: _threadId),
      throwsA(
        isA<AgentClientException>().having(
          (error) => error.kind,
          'kind',
          AgentClientFailureKind.invalidResponse,
        ),
      ),
    );
  });

  test('strictly parses the standard error and Retry-After', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/agent-threads/$_threadId/voice-practice-session',
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
      _client(transport).restorePractice(threadId: _threadId),
      throwsA(
        isA<AgentClientException>()
            .having(
              (error) => error.kind,
              'kind',
              AgentClientFailureKind.rateLimited,
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
}

Future<File> _temporaryAudio() async {
  final directory = await Directory.systemTemp.createTemp(
    'speakup-wire-audio-',
  );
  final file = File('${directory.path}/turn.wav');
  await file.writeAsBytes(List<int>.filled(64044, 1));
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
    'thread_id': _threadId,
    'matter': _matterJson(),
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

Map<String, Object?> _matterJson() {
  return {
    'matter_id': _matterId,
    'title': agentScenes.first.title,
    'status': 'active',
    'version': 1,
    'created_at': _timestamp,
    'updated_at': _timestamp,
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

const _credential = AuthSessionCredential(
  sessionToken: 'sess_practice',
  generation: 7,
);
const _threadId = '10000000-0000-4000-8000-000000000088';
const _matterId = '20000000-0000-4000-8000-000000000088';
const _sessionId = '30000000-0000-4000-8000-000000000088';
const _questionId = '40000000-0000-4000-8000-000000000088';
const _nextQuestionId = '40000000-0000-4000-8000-000000000089';
const _candidateId = '50000000-0000-4000-8000-000000000088';
const _turnId = '60000000-0000-4000-8000-000000000088';
const _timestamp = '2026-07-25T09:00:00Z';
