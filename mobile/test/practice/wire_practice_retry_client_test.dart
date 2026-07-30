import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/practice/wire_practice_client.dart';

void main() {
  test(
    'uses the server answer path for one non-effective retry turn',
    () async {
      final audioFile = await _temporaryAudio();
      addTearDown(() => audioFile.parent.delete(recursive: true));
      final transport = _Transport([
        _Step(
          method: 'POST',
          path: '/v1/feedback-items/$_feedbackItemId/retry-requests',
          verify: (request) {
            expect(request.jsonBody, isNull);
            expect(request.rawFilePath, isNull);
            expect(request.headers['Idempotency-Key'], 'retry-request-op');
          },
          response: _json(HttpStatus.created, _turnCreatedRetry()),
        ),
        _Step(
          method: 'GET',
          path: '/v1/retry-requests/$_retryRequestId',
          response: _json(HttpStatus.ok, _turnCreatedRetry()),
        ),
        _Step(
          method: 'POST',
          path: _answerPath,
          verify: (request) {
            expect(request.jsonBody, isNull);
            expect(request.rawFilePath, audioFile.path);
            expect(request.headers['Idempotency-Key'], 'retry-audio-op');
            expect(request.headers[HttpHeaders.contentTypeHeader], 'audio/wav');
          },
          response: _json(HttpStatus.created, _retryCandidate()),
        ),
        _Step(
          method: 'POST',
          path:
              '/v1/retry-turns/$_retryTurnId/transcription-candidates/'
              '$_candidateId/confirmations',
          verify: (request) {
            expect(request.jsonBody, isNull);
            expect(request.rawFilePath, isNull);
            expect(request.headers['Idempotency-Key'], 'retry-confirm-op');
          },
          response: _json(HttpStatus.ok, _confirmedRetry()),
        ),
      ]);
      final PracticeSpeechFeedbackRetryClient client = _client(transport);

      final request = await client.requestSameQuestionRetry(
        feedbackItemId: _feedbackItemId,
        idempotencyKey: 'retry-request-op',
      );
      final restored = await client.getSameQuestionRetryRequest(
        retryRequestId: request.retryRequestId,
      );
      final candidate = await client.transcribeRetry(
        answerPath: restored.answerPath!,
        idempotencyKey: 'retry-audio-op',
        audio: RecordedPracticeAudio(
          path: audioFile.path,
          contentType: 'audio/wav',
          sizeBytes: 64044,
        ),
      );
      final confirmation = await client.confirmRetry(
        retryTurnId: candidate.retryTurnId,
        candidateId: candidate.id,
        idempotencyKey: 'retry-confirm-op',
      );

      expect(request.retryStatus, PracticeRetryRequestStatus.turnCreated);
      expect(request.newTurnId, _retryTurnId);
      expect(request.answerPath, _answerPath);
      expect(candidate.retryRequestId, _retryRequestId);
      expect(candidate.text, _answer);
      expect(confirmation.turnId, _retryTurnId);
      expect(confirmation.answerText, _answer);
      expect(confirmation.countsTowardTurnLimit, isFalse);
      transport.expectDone();
    },
  );

  test('accepts an idempotent 200 retry request replay', () async {
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/feedback-items/$_feedbackItemId/retry-requests',
        response: _json(HttpStatus.ok, _turnCreatedRetry()),
      ),
    ]);

    final request = await _client(transport).requestSameQuestionRetry(
      feedbackItemId: _feedbackItemId,
      idempotencyKey: 'retry-replay-op',
    );

    expect(request.retryRequestId, _retryRequestId);
    expect(request.retryStatus, PracticeRetryRequestStatus.turnCreated);
    transport.expectDone();
  });

  test('strictly decodes pending and failed retry status shapes', () async {
    final transport = _Transport([
      _Step(
        method: 'GET',
        path: '/v1/retry-requests/retry-pending',
        response: _json(HttpStatus.ok, {
          ..._retryBase(
            retryRequestId: 'retry-pending',
            feedbackItemId: 'feedback-pending',
          ),
          'retry_status': 'PENDING',
          'status_url': '/v1/retry-requests/retry-pending',
        }),
      ),
      _Step(
        method: 'GET',
        path: '/v1/retry-requests/retry-failed',
        response: _json(HttpStatus.ok, {
          ..._retryBase(
            retryRequestId: 'retry-failed',
            feedbackItemId: 'feedback-failed',
          ),
          'retry_status': 'FAILED',
          'stable_failure': {
            'reason_code': 'RETRY_TURN_CREATION_FAILED',
            'retryable': true,
          },
          'status_url': '/v1/retry-requests/retry-failed',
          'updated_at': '2026-07-30T10:05:01Z',
          'completed_at': '2026-07-30T10:05:01Z',
        }),
      ),
    ]);
    final client = _client(transport);

    final pending = await client.getSameQuestionRetryRequest(
      retryRequestId: 'retry-pending',
    );
    final failed = await client.getSameQuestionRetryRequest(
      retryRequestId: 'retry-failed',
    );

    expect(pending.retryStatus, PracticeRetryRequestStatus.pending);
    expect(pending.completedAt, isNull);
    expect(failed.retryStatus, PracticeRetryRequestStatus.failed);
    expect(
      failed.stableFailure?.reason,
      PracticeRetryFailureReason.retryTurnCreationFailed,
    );
    expect(failed.stableFailure?.retryable, isTrue);
    transport.expectDone();
  });

  test('rejects forged retry URLs and impossible status shapes', () async {
    final invalidResponses = <Map<String, Object?>>[
      {..._turnCreatedRetry(), 'status_url': '/v1/retry-requests/other'},
      {
        ..._turnCreatedRetry(),
        'answer_path': '/v1/retry-turns/other/transcription-candidates',
      },
      {
        ..._retryBase(),
        'retry_status': 'PENDING',
        'status_url': '/v1/retry-requests/$_retryRequestId',
        'new_turn_id': _retryTurnId,
      },
      {..._turnCreatedRetry(), 'unexpected': true},
      {
        ..._retryBase(),
        'retry_status': 'FAILED',
        'stable_failure': {
          'reason_code': 'SOURCE_NO_LONGER_AVAILABLE',
          'retryable': true,
        },
        'status_url': '/v1/retry-requests/$_retryRequestId',
        'updated_at': '2026-07-30T10:05:01Z',
        'completed_at': '2026-07-30T10:05:01Z',
      },
    ];

    for (final response in invalidResponses) {
      final transport = _Transport([
        _Step(
          method: 'POST',
          path: '/v1/feedback-items/$_feedbackItemId/retry-requests',
          response: _json(HttpStatus.created, response),
        ),
      ]);
      await expectLater(
        _client(transport).requestSameQuestionRetry(
          feedbackItemId: _feedbackItemId,
          idempotencyKey: 'retry-invalid-op',
        ),
        throwsA(_invalidResponse),
      );
    }
  });

  test('requires canonical 201 READY retry transcription', () async {
    final audioFile = await _temporaryAudio();
    addTearDown(() => audioFile.parent.delete(recursive: true));
    final invalidCandidates = <(int, Map<String, Object?>)>[
      (HttpStatus.ok, _retryCandidate()),
      (
        HttpStatus.created,
        {..._retryCandidate(), 'candidate_status': 'PENDING'},
      ),
      (
        HttpStatus.created,
        {..._retryCandidate(), 'retry_turn_id': 'retry-turn-other'},
      ),
      (HttpStatus.created, {..._retryCandidate(), 'effective_turns': 1}),
    ];

    for (final (status, body) in invalidCandidates) {
      final transport = _Transport([
        _Step(method: 'POST', path: _answerPath, response: _json(status, body)),
      ]);
      await expectLater(
        _client(transport).transcribeRetry(
          answerPath: _answerPath,
          idempotencyKey: 'retry-audio-invalid',
          audio: RecordedPracticeAudio(
            path: audioFile.path,
            contentType: 'audio/wav',
            sizeBytes: 64044,
          ),
        ),
        throwsA(_invalidResponse),
      );
    }
  });

  test('rejects retry confirmation that changes practice progress', () async {
    final invalidConfirmations = <Map<String, Object?>>[
      {..._confirmedRetry(), 'turn_kind': 'EFFECTIVE'},
      {..._confirmedRetry(), 'counts_toward_turn_limit': true},
      {..._confirmedRetry(), 'effective_turns': 4},
      {..._confirmedRetry(), 'session_completed': true},
    ];

    for (final response in invalidConfirmations) {
      final transport = _Transport([
        _Step(
          method: 'POST',
          path:
              '/v1/retry-turns/$_retryTurnId/transcription-candidates/'
              '$_candidateId/confirmations',
          response: _json(HttpStatus.ok, response),
        ),
      ]);
      await expectLater(
        _client(transport).confirmRetry(
          retryTurnId: _retryTurnId,
          candidateId: _candidateId,
          idempotencyKey: 'retry-confirm-invalid',
        ),
        throwsA(_invalidResponse),
      );
    }
  });
}

Future<File> _temporaryAudio() async {
  final directory = await Directory.systemTemp.createTemp(
    'speakup-retry-audio-',
  );
  final file = File('${directory.path}/retry.wav');
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

Map<String, Object?> _retryBase({
  String retryRequestId = _retryRequestId,
  String feedbackItemId = _feedbackItemId,
}) {
  return {
    'retry_request_id': retryRequestId,
    'feedback_item_id': feedbackItemId,
    'practice_session_id': _sessionId,
    'original_turn_id': _originalTurnId,
    'question_id': _questionId,
    'created_at': '2026-07-30T10:05:00Z',
    'updated_at': '2026-07-30T10:05:00Z',
  };
}

Map<String, Object?> _turnCreatedRetry() {
  return {
    ..._retryBase(),
    'new_turn_id': _retryTurnId,
    'new_turn_status': 'ANSWERING',
    'answer_path': _answerPath,
    'retry_status': 'TURN_CREATED',
    'status_url': '/v1/retry-requests/$_retryRequestId',
    'updated_at': '2026-07-30T10:05:01Z',
    'completed_at': '2026-07-30T10:05:01Z',
  };
}

Map<String, Object?> _retryCandidate() {
  return {
    'candidate_id': _candidateId,
    'retry_turn_id': _retryTurnId,
    'retry_request_id': _retryRequestId,
    'practice_session_id': _sessionId,
    'question_id': _questionId,
    'respondent_participant_id': 'participant-user',
    'candidate_status': 'READY',
    'transcript_id': 'retry-transcript-1',
    'evidence_version': 1,
    'transcript': _answer,
    'created_at': '2026-07-30T10:07:00Z',
  };
}

Map<String, Object?> _confirmedRetry() {
  return {
    'turn_id': _retryTurnId,
    'retry_request_id': _retryRequestId,
    'original_turn_id': _originalTurnId,
    'practice_session_id': _sessionId,
    'question_id': _questionId,
    'respondent_participant_id': 'participant-user',
    'candidate_id': _candidateId,
    'interaction_mode': 'PUSH_TO_TALK',
    'answer_text': _answer,
    'evidence_version': 1,
    'turn_kind': 'RETRY',
    'turn_status': 'CONFIRMED',
    'counts_toward_turn_limit': false,
    'audio_asset_id': 'retry-audio-1',
    'created_at': '2026-07-30T10:05:01Z',
    'confirmed_at': '2026-07-30T10:07:01Z',
  };
}

PracticeWireResponse _json(int statusCode, Object body) {
  return PracticeWireResponse(statusCode: statusCode, body: jsonEncode(body));
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

  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) async {
    final step = _steps.removeFirst();
    expect(request.method, step.method);
    expect(request.uri.path, step.path);
    expect(
      request.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_retry-session',
    );
    step.verify?.call(request);
    return step.response;
  }

  void expectDone() => expect(_steps, isEmpty);

  @override
  void close({bool force = false}) {}
}

final _invalidResponse = isA<AgentClientException>().having(
  _failureKind,
  'kind',
  AgentClientFailureKind.invalidResponse,
);

AgentClientFailureKind _failureKind(AgentClientException error) => error.kind;

const _credential = AuthSessionCredential(
  sessionToken: 'sess_retry-session',
  generation: 11,
);
const _feedbackItemId = 'feedback-item-1';
const _retryRequestId = 'retry-request-1';
const _sessionId = 'practice-session-1';
const _originalTurnId = 'original-turn-1';
const _questionId = 'question-1';
const _retryTurnId = 'retry-turn-1';
const _candidateId = 'retry-candidate-1';
const _answerPath = '/v1/retry-turns/$_retryTurnId/transcription-candidates';
const _answer =
    'I managed the release risk by sharing an early mitigation plan.';
