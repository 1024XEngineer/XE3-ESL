import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

final class CoachAvatarOption {
  const CoachAvatarOption({
    required this.id,
    required this.displayName,
    required this.description,
    required this.previewAssetKey,
  });

  final String id;
  final String displayName;
  final String description;
  final String previewAssetKey;

  Map<String, Object> toJson() => <String, Object>{
    'id': id,
    'display_name': displayName,
    'description': description,
    'preview_asset_key': previewAssetKey,
  };
}

final class CoachPresentationVoiceOption {
  const CoachPresentationVoiceOption({
    required this.id,
    required this.displayName,
    required this.description,
    required this.locale,
    required this.gender,
  });

  final String id;
  final String displayName;
  final String description;
  final String locale;
  final String gender;

  Map<String, Object> toJson() => <String, Object>{
    'id': id,
    'display_name': displayName,
    'description': description,
    'locale': locale,
    'gender': gender,
  };
}

final class CoachPresentationCatalog {
  const CoachPresentationCatalog({
    required this.avatars,
    required this.voices,
    required this.defaultAvatarOptionId,
    required this.defaultVoiceOptionId,
  });

  final List<CoachAvatarOption> avatars;
  final List<CoachPresentationVoiceOption> voices;
  final String defaultAvatarOptionId;
  final String defaultVoiceOptionId;

  bool contains(String avatarOptionId, String voiceOptionId) =>
      avatars.any((option) => option.id == avatarOptionId) &&
      voices.any((option) => option.id == voiceOptionId);

  Map<String, Object> toJson() => <String, Object>{
    'avatars': avatars.map((option) => option.toJson()).toList(),
    'voices': voices.map((option) => option.toJson()).toList(),
    'defaults': <String, Object>{
      'avatar_option_id': defaultAvatarOptionId,
      'voice_option_id': defaultVoiceOptionId,
    },
  };
}

final class CoachPresentationPreference {
  const CoachPresentationPreference({
    required this.avatarOptionId,
    required this.voiceOptionId,
    required this.version,
  });

  final String avatarOptionId;
  final String voiceOptionId;
  final int version;

  Map<String, Object> toJson() => <String, Object>{
    'avatar_option_id': avatarOptionId,
    'voice_option_id': voiceOptionId,
    'version': version,
  };
}

final class CoachPresentationSettings {
  const CoachPresentationSettings({
    required this.catalog,
    required this.preference,
  });

  final CoachPresentationCatalog catalog;
  final CoachPresentationPreference preference;

  Map<String, Object> toJson() => <String, Object>{
    'catalog': catalog.toJson(),
    'preference': preference.toJson(),
  };
}

abstract interface class CoachPresentationSettingsStore {
  Future<CoachPresentationSettings> load({required String accountId});

  Future<CoachPresentationSettings> refresh({required String accountId});

  Future<CoachPresentationPreference> save({
    required String accountId,
    required String avatarOptionId,
    required String voiceOptionId,
    required int expectedVersion,
  });

  Future<Uint8List> previewVoice({
    required String accountId,
    required String voiceOptionId,
  });
}

const previewCoachPresentationCatalog = CoachPresentationCatalog(
  avatars: <CoachAvatarOption>[
    CoachAvatarOption(
      id: 'avatar_lisa',
      displayName: '莉萨',
      description: '亲切、开朗',
      previewAssetKey: 'coach-avatar-lisa',
    ),
    CoachAvatarOption(
      id: 'avatar_nathan',
      displayName: '内森',
      description: '温暖、沉稳',
      previewAssetKey: 'coach-avatar-nathan',
    ),
  ],
  voices: <CoachPresentationVoiceOption>[
    CoachPresentationVoiceOption(
      id: 'voice_ava',
      displayName: '艾娃',
      description: '清晰自然 · 美式英语 · 女声',
      locale: 'en-US',
      gender: 'female',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_john',
      displayName: '约翰',
      description: '温暖沉稳 · 美式英语 · 男声',
      locale: 'en-US',
      gender: 'male',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_mary',
      displayName: '玛丽',
      description: '温暖自然 · 英式英语 · 女声',
      locale: 'en-GB',
      gender: 'female',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_olivia',
      displayName: '奥利维亚',
      description: '温柔知性 · 英语 · 女声',
      locale: 'en',
      gender: 'female',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_luna',
      displayName: '露娜',
      description: '清晰灵动 · 英语 · 女声',
      locale: 'en',
      gender: 'female',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_nora',
      displayName: '诺拉',
      description: '活泼自然 · 英语 · 女声',
      locale: 'en',
      gender: 'female',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_adrian',
      displayName: '阿德里安',
      description: '沉稳大气 · 英语 · 男声',
      locale: 'en',
      gender: 'male',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_james',
      displayName: '詹姆斯',
      description: '清晰专业 · 英语 · 男声',
      locale: 'en',
      gender: 'male',
    ),
    CoachPresentationVoiceOption(
      id: 'voice_ivy',
      displayName: '艾薇',
      description: '自信从容 · 英语 · 女声',
      locale: 'en',
      gender: 'female',
    ),
  ],
  defaultAvatarOptionId: 'avatar_lisa',
  defaultVoiceOptionId: 'voice_ava',
);

final class PreviewCoachPresentationSettingsStore
    implements CoachPresentationSettingsStore {
  const PreviewCoachPresentationSettingsStore();

  static const _settings = CoachPresentationSettings(
    catalog: previewCoachPresentationCatalog,
    preference: CoachPresentationPreference(
      avatarOptionId: 'avatar_lisa',
      voiceOptionId: 'voice_ava',
      version: 0,
    ),
  );

  @override
  Future<CoachPresentationSettings> load({required String accountId}) async =>
      _settings;

  @override
  Future<CoachPresentationSettings> refresh({
    required String accountId,
  }) async => _settings;

  @override
  Future<CoachPresentationPreference> save({
    required String accountId,
    required String avatarOptionId,
    required String voiceOptionId,
    required int expectedVersion,
  }) async => CoachPresentationPreference(
    avatarOptionId: avatarOptionId,
    voiceOptionId: voiceOptionId,
    version: expectedVersion + 1,
  );

  @override
  Future<Uint8List> previewVoice({
    required String accountId,
    required String voiceOptionId,
  }) async {
    _requireAccountId(accountId);
    _optionId(voiceOptionId);
    return Uint8List.fromList(_silentPreviewWav);
  }
}

final class CoachPresentationVersionConflict implements Exception {
  const CoachPresentationVersionConflict();
}

final class WireCoachPresentationSettingsStore
    implements CoachPresentationSettingsStore {
  factory WireCoachPresentationSettingsStore({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    CoachPresentationSettingsCache? cache,
  }) => WireCoachPresentationSettingsStore._(
    baseUri,
    SessionAuthenticatedHttpTransport(
      transport: transport ?? IoIdentityHttpTransport(),
      credentialProvider: credentialProvider,
      invalidateSession: invalidateSession,
      trustedBaseUri: baseUri,
    ),
    cache ?? const SecureCoachPresentationSettingsCache(),
  );

  WireCoachPresentationSettingsStore._(
    this._baseUri,
    this._transport,
    this._cache,
  );

  static const _maximumResponseBytes = 64 * 1024;
  static const _maximumPreviewBytes = 2 * 1024 * 1024;
  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final CoachPresentationSettingsCache _cache;
  final Map<String, CoachPresentationCatalog> _catalogs = {};

  @override
  Future<CoachPresentationSettings> load({required String accountId}) async {
    _requireAccountId(accountId);
    try {
      return await refresh(accountId: accountId);
    } on Object catch (error) {
      if (error is AuthSessionSupersededException ||
          error is _CoachPresentationRequestException &&
              error.statusCode == HttpStatus.unauthorized) {
        rethrow;
      }
      final cached = await _cache.load(accountId);
      if (cached == null) rethrow;
      _catalogs[accountId] = cached.catalog;
      return cached;
    }
  }

  @override
  Future<CoachPresentationSettings> refresh({required String accountId}) async {
    _requireAccountId(accountId);
    final responses = await Future.wait<IdentityHttpResponse>([
      _get('/v1/coach-presentation-catalog'),
      _get('/v1/me/coach-presentation'),
    ]);
    final catalog = _decodeCatalog(_requireOK(responses[0]));
    final preference = _decodePreference(_requireOK(responses[1]));
    if (!catalog.contains(
      preference.avatarOptionId,
      preference.voiceOptionId,
    )) {
      throw const FormatException('Preference is outside the catalog.');
    }
    final settings = CoachPresentationSettings(
      catalog: catalog,
      preference: preference,
    );
    _catalogs[accountId] = catalog;
    await _tryCache(accountId, settings);
    return settings;
  }

  @override
  Future<CoachPresentationPreference> save({
    required String accountId,
    required String avatarOptionId,
    required String voiceOptionId,
    required int expectedVersion,
  }) async {
    _requireAccountId(accountId);
    final catalog = _catalogs[accountId];
    if (catalog == null ||
        expectedVersion < 0 ||
        !catalog.contains(avatarOptionId, voiceOptionId)) {
      throw ArgumentError('Invalid coach presentation update.');
    }
    final response = await _transport
        .send(
          method: 'PATCH',
          uri: _baseUri.resolve('/v1/me/coach-presentation'),
          headers: const <String, String>{
            HttpHeaders.acceptHeader: 'application/json',
            HttpHeaders.contentTypeHeader: 'application/json',
          },
          body: jsonEncode(<String, Object>{
            'avatar_option_id': avatarOptionId,
            'voice_option_id': voiceOptionId,
            'expected_version': expectedVersion,
          }),
        )
        .timeout(const Duration(seconds: 15));
    if (response.statusCode == HttpStatus.conflict) {
      throw const CoachPresentationVersionConflict();
    }
    final preference = _decodePreference(_requireOK(response));
    if (!catalog.contains(
      preference.avatarOptionId,
      preference.voiceOptionId,
    )) {
      throw const FormatException('Preference is outside the catalog.');
    }
    await _tryCache(
      accountId,
      CoachPresentationSettings(catalog: catalog, preference: preference),
    );
    return preference;
  }

  @override
  Future<Uint8List> previewVoice({
    required String accountId,
    required String voiceOptionId,
  }) async {
    _requireAccountId(accountId);
    final normalizedVoiceOptionId = _optionId(voiceOptionId);
    final response = await _transport
        .send(
          method: 'POST',
          uri: _baseUri.resolve(
            '/v1/coach-presentation/voices/'
            '${Uri.encodeComponent(normalizedVoiceOptionId)}/previews',
          ),
          headers: const <String, String>{
            HttpHeaders.acceptHeader: 'audio/wav',
          },
        )
        .timeout(const Duration(seconds: 20));
    final contentType = response.headers[HttpHeaders.contentTypeHeader] ?? '';
    final bytes = Uint8List.fromList(response.bodyBytes);
    if (response.statusCode != HttpStatus.ok ||
        !contentType.toLowerCase().startsWith('audio/wav') ||
        bytes.length <= 44 ||
        bytes.length > _maximumPreviewBytes ||
        !_isWave(bytes)) {
      bytes.fillRange(0, bytes.length, 0);
      throw _CoachPresentationRequestException(response.statusCode);
    }
    return bytes;
  }

  Future<IdentityHttpResponse> _get(String path) => _transport
      .send(
        method: 'GET',
        uri: _baseUri.resolve(path),
        headers: const <String, String>{
          HttpHeaders.acceptHeader: 'application/json',
        },
      )
      .timeout(const Duration(seconds: 15));

  String _requireOK(IdentityHttpResponse response) {
    if (response.statusCode != HttpStatus.ok ||
        utf8.encode(response.body).length > _maximumResponseBytes) {
      throw _CoachPresentationRequestException(response.statusCode);
    }
    return response.body;
  }

  Future<void> _tryCache(
    String accountId,
    CoachPresentationSettings settings,
  ) async {
    try {
      await _cache.save(accountId, settings);
    } on Object {
      // The account-scoped cache is best-effort; the service remains canonical.
    }
  }
}

bool _isWave(Uint8List bytes) =>
    bytes.length >= 44 &&
    bytes[0] == 0x52 &&
    bytes[1] == 0x49 &&
    bytes[2] == 0x46 &&
    bytes[3] == 0x46 &&
    bytes[8] == 0x57 &&
    bytes[9] == 0x41 &&
    bytes[10] == 0x56 &&
    bytes[11] == 0x45;

const _silentPreviewWav = <int>[
  0x52,
  0x49,
  0x46,
  0x46,
  0x26,
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
  0xc0,
  0x5d,
  0x00,
  0x00,
  0x80,
  0xbb,
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
  0x02,
  0x00,
  0x00,
  0x00,
  0x00,
  0x00,
];

final class _CoachPresentationRequestException implements Exception {
  const _CoachPresentationRequestException(this.statusCode);

  final int statusCode;
}

abstract interface class CoachPresentationSettingsCache {
  Future<CoachPresentationSettings?> load(String accountId);

  Future<void> save(String accountId, CoachPresentationSettings settings);
}

final class SecureCoachPresentationSettingsCache
    implements CoachPresentationSettingsCache {
  const SecureCoachPresentationSettingsCache([
    this._storage = const FlutterSecureStorage(),
  ]);

  static const _keyPrefix = 'coach_presentation_cache_v1:';
  final FlutterSecureStorage _storage;

  @override
  Future<CoachPresentationSettings?> load(String accountId) async {
    _requireAccountId(accountId);
    final value = await _storage.read(key: '$_keyPrefix$accountId');
    if (value == null) return null;
    try {
      return _decodeSettings(value);
    } on Object {
      return null;
    }
  }

  @override
  Future<void> save(String accountId, CoachPresentationSettings settings) {
    _requireAccountId(accountId);
    return _storage.write(
      key: '$_keyPrefix$accountId',
      value: jsonEncode(settings.toJson()),
    );
  }
}

CoachPresentationSettings _decodeSettings(String source) {
  final root = _object(jsonDecode(source));
  _requireKeys(root, const {'catalog', 'preference'});
  final catalog = _decodeCatalogObject(_object(root['catalog']));
  final preference = _decodePreferenceObject(_object(root['preference']));
  if (!catalog.contains(preference.avatarOptionId, preference.voiceOptionId)) {
    throw const FormatException('Preference is outside the catalog.');
  }
  return CoachPresentationSettings(catalog: catalog, preference: preference);
}

CoachPresentationCatalog _decodeCatalog(String source) =>
    _decodeCatalogObject(_object(jsonDecode(source)));

CoachPresentationCatalog _decodeCatalogObject(Map<String, Object?> root) {
  _requireKeys(root, const {'avatars', 'voices', 'defaults'});
  final avatars = _list(root['avatars'])
      .map((value) {
        final option = _object(value);
        _requireKeys(option, const {
          'id',
          'display_name',
          'description',
          'preview_asset_key',
        });
        return CoachAvatarOption(
          id: _optionId(option['id']),
          displayName: _text(option['display_name'], 64),
          description: _text(option['description'], 200),
          previewAssetKey: _token(option['preview_asset_key'], 128),
        );
      })
      .toList(growable: false);
  final voices = _list(root['voices'])
      .map((value) {
        final option = _object(value);
        _requireKeys(option, const {
          'id',
          'display_name',
          'description',
          'locale',
          'gender',
        });
        final gender = _token(option['gender'], 16);
        if (gender != 'female' && gender != 'male') {
          throw const FormatException('Invalid voice gender.');
        }
        return CoachPresentationVoiceOption(
          id: _optionId(option['id']),
          displayName: _text(option['display_name'], 64),
          description: _text(option['description'], 200),
          locale: _token(option['locale'], 32),
          gender: gender,
        );
      })
      .toList(growable: false);
  if (avatars.isEmpty ||
      voices.isEmpty ||
      _duplicates(avatars.map((e) => e.id)) ||
      _duplicates(voices.map((e) => e.id))) {
    throw const FormatException('Invalid coach presentation catalog.');
  }
  final defaults = _object(root['defaults']);
  _requireKeys(defaults, const {'avatar_option_id', 'voice_option_id'});
  final catalog = CoachPresentationCatalog(
    avatars: avatars,
    voices: voices,
    defaultAvatarOptionId: _optionId(defaults['avatar_option_id']),
    defaultVoiceOptionId: _optionId(defaults['voice_option_id']),
  );
  if (!catalog.contains(
    catalog.defaultAvatarOptionId,
    catalog.defaultVoiceOptionId,
  )) {
    throw const FormatException('Invalid coach presentation defaults.');
  }
  return catalog;
}

CoachPresentationPreference _decodePreference(String source) =>
    _decodePreferenceObject(_object(jsonDecode(source)));

CoachPresentationPreference _decodePreferenceObject(Map<String, Object?> root) {
  const required = {'avatar_option_id', 'voice_option_id', 'version'};
  const allowed = {...required, 'created_at', 'updated_at'};
  if (!required.every(root.containsKey) ||
      root.keys.any((key) => !allowed.contains(key))) {
    throw const FormatException('Invalid coach presentation preference.');
  }
  final version = root['version'];
  if (version is! int || version < 0) {
    throw const FormatException('Invalid coach presentation version.');
  }
  return CoachPresentationPreference(
    avatarOptionId: _optionId(root['avatar_option_id']),
    voiceOptionId: _optionId(root['voice_option_id']),
    version: version,
  );
}

Map<String, Object?> _object(Object? value) {
  if (value is! Map<String, Object?>) {
    throw const FormatException('Expected a JSON object.');
  }
  return value;
}

List<Object?> _list(Object? value) {
  if (value is! List<Object?>) {
    throw const FormatException('Expected a JSON array.');
  }
  return value;
}

void _requireKeys(Map<String, Object?> value, Set<String> expected) {
  if (value.length != expected.length || !expected.every(value.containsKey)) {
    throw const FormatException('Unexpected JSON fields.');
  }
}

String _optionId(Object? value) {
  final id = _token(value, 64);
  if (!RegExp(r'^[a-z][a-z0-9_]{0,63}$').hasMatch(id)) {
    throw const FormatException('Invalid option id.');
  }
  return id;
}

String _text(Object? value, int maximumRunes) {
  if (value is! String ||
      value.trim() != value ||
      value.isEmpty ||
      value.runes.length > maximumRunes) {
    throw const FormatException('Invalid text.');
  }
  return value;
}

String _token(Object? value, int maximumRunes) {
  final token = _text(value, maximumRunes);
  if (RegExp(r'\s').hasMatch(token)) {
    throw const FormatException('Invalid token.');
  }
  return token;
}

bool _duplicates(Iterable<String> values) {
  final seen = <String>{};
  return values.any((value) => !seen.add(value));
}

void _requireAccountId(String accountId) {
  if (accountId.isEmpty ||
      accountId.length > 128 ||
      RegExp(r'[\s:]').hasMatch(accountId)) {
    throw ArgumentError.value(accountId, 'accountId');
  }
}
