import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/profile/coach_presentation_settings.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'loads the service catalog and account preference with Bearer',
    () async {
      final transport = _Transport(<IdentityHttpResponse>[
        _response(_catalogJson),
        _response(_preferenceJson),
      ]);
      final cache = _Cache();
      final store = _store(transport: transport, cache: cache);

      final settings = await store.load(accountId: 'user_account-a');

      expect(settings.catalog.avatars.map((option) => option.id), <String>[
        'avatar_lisa',
        'avatar_nathan',
      ]);
      expect(settings.preference.voiceOptionId, 'voice_ava');
      expect(transport.requests.map((request) => request.uri.path), <String>[
        '/v1/coach-presentation-catalog',
        '/v1/me/coach-presentation',
      ]);
      for (final request in transport.requests) {
        expect(
          request.headers[HttpHeaders.authorizationHeader],
          'Bearer sess_presentation',
        );
      }
      expect(cache.savedAccounts, <String>['user_account-a']);
    },
  );

  test(
    'updates internal option ids with the current service version',
    () async {
      final transport = _Transport(<IdentityHttpResponse>[
        _response(_catalogJson),
        _response(_preferenceJson),
        _response(
          '{"avatar_option_id":"avatar_nathan",'
          '"voice_option_id":"voice_john","version":4}',
        ),
      ]);
      final store = _store(transport: transport, cache: _Cache());
      await store.load(accountId: 'user_account-a');

      final saved = await store.save(
        accountId: 'user_account-a',
        avatarOptionId: 'avatar_nathan',
        voiceOptionId: 'voice_john',
        expectedVersion: 3,
      );

      expect(saved.version, 4);
      final request = transport.requests.last;
      expect(request.method, 'PATCH');
      expect(jsonDecode(request.body!), <String, Object>{
        'avatar_option_id': 'avatar_nathan',
        'voice_option_id': 'voice_john',
        'expected_version': 3,
      });
    },
  );

  test('surfaces version conflicts for one refresh and retry', () async {
    final transport = _Transport(<IdentityHttpResponse>[
      _response(_catalogJson),
      _response(_preferenceJson),
      const IdentityHttpResponse(statusCode: HttpStatus.conflict, body: '{}'),
    ]);
    final store = _store(transport: transport, cache: _Cache());
    await store.load(accountId: 'user_account-a');

    await expectLater(
      store.save(
        accountId: 'user_account-a',
        avatarOptionId: 'avatar_nathan',
        voiceOptionId: 'voice_john',
        expectedVersion: 3,
      ),
      throwsA(isA<CoachPresentationVersionConflict>()),
    );
  });

  test('uses only the matching account cache on a network failure', () async {
    final cache = _Cache()
      ..values['user_account-a'] = const CoachPresentationSettings(
        catalog: previewCoachPresentationCatalog,
        preference: CoachPresentationPreference(
          avatarOptionId: 'avatar_nathan',
          voiceOptionId: 'voice_john',
          version: 7,
        ),
      );
    final store = _store(
      transport: _Transport(<IdentityHttpResponse>[
        const IdentityHttpResponse(
          statusCode: HttpStatus.serviceUnavailable,
          body: '{}',
        ),
        const IdentityHttpResponse(
          statusCode: HttpStatus.serviceUnavailable,
          body: '{}',
        ),
      ]),
      cache: cache,
    );

    final cached = await store.load(accountId: 'user_account-a');
    expect(cached.preference.version, 7);

    await expectLater(
      store.load(accountId: 'user_account-b'),
      throwsA(isA<Object>()),
    );
  });
}

WireCoachPresentationSettingsStore _store({
  required IdentityHttpTransport transport,
  required CoachPresentationSettingsCache cache,
}) => WireCoachPresentationSettingsStore(
  baseUri: Uri.parse('https://api.example.test'),
  credentialProvider: () => const AuthSessionCredential(
    sessionToken: 'sess_presentation',
    generation: 1,
  ),
  invalidateSession:
      ({required expectedSessionToken, required expectedGeneration}) async {},
  transport: transport,
  cache: cache,
);

IdentityHttpResponse _response(String body) =>
    IdentityHttpResponse(statusCode: HttpStatus.ok, body: body);

const _catalogJson = '''
{
  "avatars": [
    {"id":"avatar_lisa","display_name":"莉萨","description":"亲切、开朗","preview_asset_key":"coach-avatar-lisa"},
    {"id":"avatar_nathan","display_name":"内森","description":"温暖、沉稳","preview_asset_key":"coach-avatar-nathan"}
  ],
  "voices": [
    {"id":"voice_ava","display_name":"艾娃","description":"清晰自然 · 美式英语 · 女声","locale":"en-US","gender":"female"},
    {"id":"voice_john","display_name":"约翰","description":"温暖沉稳 · 美式英语 · 男声","locale":"en-US","gender":"male"}
  ],
  "defaults": {"avatar_option_id":"avatar_lisa","voice_option_id":"voice_ava"}
}
''';

const _preferenceJson =
    '{"avatar_option_id":"avatar_lisa",'
    '"voice_option_id":"voice_ava","version":3}';

final class _Request {
  const _Request({
    required this.method,
    required this.uri,
    required this.headers,
    required this.body,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final String? body;
}

final class _Transport implements IdentityHttpTransport {
  _Transport(Iterable<IdentityHttpResponse> responses)
    : responses = Queue<IdentityHttpResponse>.of(responses);

  final Queue<IdentityHttpResponse> responses;
  final List<_Request> requests = <_Request>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    requests.add(
      _Request(method: method, uri: uri, headers: headers, body: body),
    );
    return responses.removeFirst();
  }
}

final class _Cache implements CoachPresentationSettingsCache {
  final Map<String, CoachPresentationSettings> values = {};
  final List<String> savedAccounts = <String>[];

  @override
  Future<CoachPresentationSettings?> load(String accountId) async =>
      values[accountId];

  @override
  Future<void> save(
    String accountId,
    CoachPresentationSettings settings,
  ) async {
    savedAccounts.add(accountId);
    values[accountId] = settings;
  }
}
