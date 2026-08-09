import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_answer_preparation.dart';
import 'package:speakup/features/coaching/ielts/wire_ielts_answer_preparation_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test('creates and decodes an IELTS answer preparation', () async {
    final transport = _QueueTransport(<IdentityHttpResponse>[
      _response(HttpStatus.created, _preparationJson(status: 'ready')),
    ]);
    final client = _client(transport);

    final preparation = await client.create(
      question: _question,
      personalPoints: const <String>['I listen on my commute.'],
      targetBand: 7,
    );

    expect(preparation.status, IeltsAnswerPreparationStatus.ready);
    expect(
      preparation.answer,
      'I prefer happy music because it lifts my mood.',
    );
    expect(preparation.question.sourceId, 'music');
    expect(transport.calls, hasLength(1));
    final call = transport.calls.single;
    expect(call.method, 'POST');
    expect(call.uri.path, '/v1/ielts-speaking/answer-preparations');
    expect(
      call.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_account-a',
    );
    expect(call.headers['Idempotency-Key'], startsWith('ielts_create_'));
    expect(jsonDecode(call.body!), <String, Object?>{
      'question': _question.toJson(),
      'personal_points': <String>['I listen on my commute.'],
      'target_band': 7.0,
    });
  });

  test('updates, generates, and deletes with explicit versions', () async {
    final transport = _QueueTransport(<IdentityHttpResponse>[
      _response(HttpStatus.ok, _preparationJson(status: 'draft', version: 2)),
      _response(HttpStatus.ok, _preparationJson(status: 'ready', version: 3)),
      const IdentityHttpResponse(statusCode: HttpStatus.noContent, body: ''),
    ]);
    final client = _client(transport);

    final draft = await client.update(
      id: _preparationId,
      expectedVersion: 1,
      personalPoints: const <String>['I listen while commuting.'],
      targetBand: 7,
    );
    final ready = await client.generate(
      id: draft.id,
      expectedVersion: draft.version,
    );
    await client.delete(id: ready.id, expectedVersion: ready.version);

    expect(transport.calls.map((call) => call.method), <String>[
      'PATCH',
      'POST',
      'DELETE',
    ]);
    expect(
      transport.calls[0].uri.path,
      '/v1/ielts-speaking/answer-preparations/$_preparationId',
    );
    expect(jsonDecode(transport.calls[0].body!), <String, Object?>{
      'expected_version': 1,
      'personal_points': <String>['I listen while commuting.'],
      'target_band': 7.0,
    });
    expect(
      transport.calls[1].uri.path,
      '/v1/ielts-speaking/answer-preparations/$_preparationId/generations',
    );
    expect(jsonDecode(transport.calls[1].body!), <String, Object?>{
      'expected_version': 2,
    });
    expect(transport.calls[2].uri.queryParameters, <String, String>{
      'expected_version': '3',
    });
    for (final call in transport.calls) {
      expect(call.headers['Idempotency-Key'], isNotEmpty);
    }
  });

  test('maps conflicts and rejects malformed responses', () async {
    final conflictClient = _client(
      _QueueTransport(<IdentityHttpResponse>[
        const IdentityHttpResponse(statusCode: HttpStatus.conflict, body: '{}'),
      ]),
    );

    await expectLater(
      conflictClient.create(
        question: _question,
        personalPoints: const <String>[],
        targetBand: 7,
      ),
      throwsA(
        isA<IeltsAnswerPreparationException>().having(
          (error) => error.kind,
          'kind',
          IeltsAnswerPreparationFailureKind.conflict,
        ),
      ),
    );

    final invalidClient = _client(
      _QueueTransport(<IdentityHttpResponse>[
        _response(HttpStatus.created, <String, Object?>{'status': 'draft'}),
      ]),
    );
    await expectLater(
      invalidClient.create(
        question: _question,
        personalPoints: const <String>[],
        targetBand: 7,
      ),
      throwsA(
        isA<IeltsAnswerPreparationException>().having(
          (error) => error.kind,
          'kind',
          IeltsAnswerPreparationFailureKind.invalidResponse,
        ),
      ),
    );
  });
}

WireIeltsAnswerPreparationClient _client(IdentityHttpTransport transport) {
  const credential = AuthSessionCredential(
    sessionToken: 'sess_account-a',
    generation: 1,
  );
  return WireIeltsAnswerPreparationClient(
    baseUri: Uri.parse('https://api.speak-up.top'),
    credentialProvider: () => credential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

Map<String, Object?> _preparationJson({
  required String status,
  int version = 1,
}) => <String, Object?>{
  'answer_preparation_id': _preparationId,
  'question': <String, Object?>{'reference': _question.toJson()},
  'personal_points': <String>[],
  'target_band': 7,
  'status': status,
  'version': version,
  'generation_revision': status == 'ready' ? 1 : 0,
  if (status == 'ready') ...<String, Object?>{
    'answer': 'I prefer happy music because it lifts my mood.',
    'outline': <String>['Preference', 'Reason'],
    'useful_expressions': <String>['lifts my mood'],
    'speech_text': 'I prefer happy music because it lifts my mood.',
  },
};

IdentityHttpResponse _response(int status, Map<String, Object?> body) =>
    IdentityHttpResponse(statusCode: status, body: jsonEncode(body));

final class _Call {
  const _Call({
    required this.method,
    required this.uri,
    required this.headers,
    this.body,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final String? body;
}

final class _QueueTransport implements IdentityHttpTransport {
  _QueueTransport(this.responses);

  final List<IdentityHttpResponse> responses;
  final List<_Call> calls = <_Call>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    calls.add(
      _Call(
        method: method,
        uri: uri,
        headers: Map<String, String>.of(headers),
        body: body,
      ),
    );
    return responses.removeAt(0);
  }
}

const _preparationId = 'ielts_answer_00000000000000000000000000000000';
const _question = IeltsAnswerQuestionReference(
  bankId: 'bank-1',
  part: 'PART_1',
  sourceId: 'music',
  questionPosition: 1,
);
