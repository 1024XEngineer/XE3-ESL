import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/practice/wire_practice_client.dart';
import 'package:speakup/identity/auth_state.dart';

void main() {
  test(
    'creates, transcribes, and confirms one same-question retry turn',
    () async {
      final audioFile = await _temporaryAudio();
      addTearDown(() => audioFile.parent.delete(recursive: true));
      final transport = _Transport([
        _Step(
          method: 'POST',
          path: '/v1/evaluation-feedback-items/$_feedbackItemId/retry-turns',
          verify: (request) {
            expect(request.headers['Idempotency-Key'], 'retry-create-op');
            expect(request.rawFilePath, isNull);
          },
          response: _json(HttpStatus.created, _retryTurn()),
        ),
        _Step(
          method: 'POST',
          path: _answerPath,
          verify: (request) {
            expect(request.headers['Idempotency-Key'], 'retry-audio-op');
            expect(request.rawFilePath, audioFile.path);
          },
          response: _json(HttpStatus.created, _retryCandidate()),
        ),
        _Step(
          method: 'POST',
          path:
              '/v1/retry-turns/$_retryTurnId/transcription-candidates/'
              '$_candidateId/confirmations',
          response: _json(HttpStatus.ok, _confirmedRetry()),
        ),
      ]);
      final client = _client(transport);

      final turn = await client.requestSameQuestionRetry(
        feedbackItemId: _feedbackItemId,
        idempotencyKey: 'retry-create-op',
      );
      final candidate = await client.transcribeRetry(
        answerPath: turn.answerPath,
        idempotencyKey: 'retry-audio-op',
        audio: RecordedPracticeAudio(
          path: audioFile.path,
          contentType: 'audio/wav',
          sizeBytes: 64044,
        ),
      );
      final confirmed = await client.confirmRetry(
        retryTurnId: candidate.retryTurnId,
        candidateId: candidate.id,
        idempotencyKey: 'retry-confirm-op',
      );

      expect(turn.status.name, 'answering');
      expect(turn.replayed, isFalse);
      expect(candidate.text, _answer);
      expect(confirmed.turnId, _retryTurnId);
      expect(confirmed.countsTowardTurnLimit, isFalse);
      expect(transport.requests.map((request) => request.uri.path), <String>[
        '/v1/evaluation-feedback-items/$_feedbackItemId/retry-turns',
        _answerPath,
        '/v1/retry-turns/$_retryTurnId/transcription-candidates/'
            '$_candidateId/confirmations',
      ]);
      transport.expectDone();
    },
  );

  test('accepts idempotent replay with the current turn state', () async {
    final transport = _Transport([
      _Step(
        method: 'POST',
        path: '/v1/evaluation-feedback-items/$_feedbackItemId/retry-turns',
        response: _json(
          HttpStatus.ok,
          _retryTurn(status: 'confirmed', replayed: true),
        ),
      ),
    ]);

    final turn = await _client(transport).requestSameQuestionRetry(
      feedbackItemId: _feedbackItemId,
      idempotencyKey: 'retry-replay-op',
    );

    expect(turn.status.name, 'confirmed');
    expect(turn.replayed, isTrue);
  });

  test('strictly rejects forged and obsolete retry response fields', () async {
    for (final response in <Map<String, Object?>>[
      {..._retryTurn(), 'retry_request_id': 'obsolete'},
      {..._retryTurn(), 'replayed': 'false'},
      {
        ..._retryTurn(),
        'turn': {
          ...(_retryTurn()['turn']! as Map<String, Object?>),
          'status': 'pending',
        },
      },
    ]) {
      final transport = _Transport([
        _Step(
          method: 'POST',
          path: '/v1/evaluation-feedback-items/$_feedbackItemId/retry-turns',
          response: _json(HttpStatus.created, response),
        ),
      ]);
      await expectLater(
        _client(transport).requestSameQuestionRetry(
          feedbackItemId: _feedbackItemId,
          idempotencyKey: 'retry-invalid-op',
        ),
        throwsA(
          isA<PracticeClientException>().having(
            (error) => error.kind,
            'kind',
            PracticeClientFailureKind.invalidResponse,
          ),
        ),
      );
    }
  });
}

Future<File> _temporaryAudio() async {
  final directory = await Directory.systemTemp.createTemp('speakup-retry-');
  final file = File('${directory.path}/retry.wav');
  await file.writeAsBytes(List<int>.filled(64044, 1));
  return file;
}

WirePracticeClient _client(
  PracticeWireTransport transport,
) => WirePracticeClient(
  baseUri: Uri.parse('https://api.speak-up.test'),
  credentialProvider: () =>
      const AuthSessionCredential(sessionToken: 'sess_practice', generation: 1),
  invalidateSession:
      ({required expectedSessionToken, required expectedGeneration}) async {},
  transport: transport,
);

Map<String, Object?> _retryTurn({
  String status = 'answering',
  bool replayed = false,
}) => <String, Object?>{
  'turn': <String, Object?>{
    'turn_id': _retryTurnId,
    'practice_session_id': _sessionId,
    'question_id': _questionId,
    'original_turn_id': _originalTurnId,
    'sequence': 4,
    'status': status,
    'created_at': '2026-08-15T02:00:00Z',
  },
  'replayed': replayed,
};

Map<String, Object?> _retryCandidate() => <String, Object?>{
  'candidate_id': _candidateId,
  'retry_turn_id': _retryTurnId,
  'practice_session_id': _sessionId,
  'question_id': _questionId,
  'respondent_participant_id': 'participant-user',
  'candidate_status': 'READY',
  'transcript_id': 'retry-transcript-1',
  'evidence_version': 1,
  'transcript': _answer,
  'created_at': '2026-08-15T02:01:00Z',
};

Map<String, Object?> _confirmedRetry() => <String, Object?>{
  'turn_id': _retryTurnId,
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
  'audio_asset_id': '00000000-0000-4000-8000-000000000002',
  'created_at': '2026-08-15T02:00:00Z',
  'confirmed_at': '2026-08-15T02:02:00Z',
};

PracticeWireResponse _json(int statusCode, Object body) =>
    PracticeWireResponse(statusCode: statusCode, body: jsonEncode(body));

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
  final List<PracticeWireRequest> requests = <PracticeWireRequest>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) async {
    if (_steps.isEmpty) {
      throw StateError('unexpected request');
    }
    final step = _steps.removeFirst();
    requests.add(request);
    step.verify?.call(request);
    return step.response;
  }

  void expectDone() => expect(_steps, isEmpty);
}

const _feedbackItemId = '40000000-0000-4000-8000-000000000001';
const _sessionId = '30000000-0000-4000-8000-000000000001';
const _questionId = '70000000-0000-4000-8000-000000000001';
const _originalTurnId = '20000000-0000-4000-8000-000000000001';
const _retryTurnId = '80000000-0000-4000-8000-000000000001';
const _candidateId = '90000000-0000-4000-8000-000000000001';
const _answerPath = '/v1/retry-turns/$_retryTurnId/transcription-candidates';
const _answer = 'I explained the issue clearly.';
