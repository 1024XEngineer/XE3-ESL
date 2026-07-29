import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/preparation/preparation_client.dart';
import 'package:speakup/features/preparation/preparation_models.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WirePreparationCatalogClient implements PreparationCatalogClient {
  factory WirePreparationCatalogClient({
    required Uri baseUri,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WirePreparationCatalogClient._(
      baseUri,
      transport ?? _IoPreparationCatalogTransport(_maximumBodyBytes),
      requestTimeout,
    );
  }

  WirePreparationCatalogClient._(
    this._baseUri,
    this._transport,
    this._requestTimeout,
  ) : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  static const _maximumBodyBytes = 256 * 1024;

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  int _accountGeneration = 0;

  @override
  Future<List<PreparationScenario>> listScenarios() async {
    final response = await _get('/v1/scenario-definitions');
    final root = _object(
      _decode(response.body),
      required: const <String>{'scenarios'},
    );
    final rawScenarios = root['scenarios'];
    if (rawScenarios is! List<Object?> || rawScenarios.length > 50) {
      throw _invalidResponse();
    }
    final ids = <String>{};
    final scenarios = <PreparationScenario>[];
    for (final value in rawScenarios) {
      final scenario = _scenario(value);
      if (scenario.status != 'active' || !ids.add(scenario.id)) {
        throw _invalidResponse();
      }
      scenarios.add(scenario);
    }
    return List<PreparationScenario>.unmodifiable(scenarios);
  }

  @override
  Future<PreparationScenarioDetail> getScenario(String scenarioId) async {
    _resourceId(scenarioId);
    final response = await _get(
      '/v1/scenario-definitions/${Uri.encodeComponent(scenarioId)}',
    );
    final root = _object(
      _decode(response.body),
      required: const <String>{
        'scenario_definition',
        'scenario_config',
        'practice_options',
      },
    );
    final config = _scenarioConfig(root['scenario_config']);
    final scenario = _scenario(
      root['scenario_definition'],
      summary: config.prompt.publicSceneBrief,
    );
    if (scenario.id != scenarioId || scenario.status != 'active') {
      throw _invalidResponse();
    }
    if (config.scenarioId != scenarioId ||
        config.type != scenario.type ||
        config.model != scenario.model) {
      throw _invalidResponse();
    }
    final rawOptions = root['practice_options'];
    if (rawOptions is! List<Object?> ||
        rawOptions.isEmpty ||
        rawOptions.length > 100) {
      throw _invalidResponse();
    }
    final ids = <String>{};
    final options = <PreparationOption>[];
    for (final value in rawOptions) {
      final option = _practiceOption(value);
      if (option.scenarioId != scenarioId || !ids.add(option.id)) {
        throw _invalidResponse();
      }
      options.add(option);
    }
    return PreparationScenarioDetail(
      scenario: scenario,
      config: config,
      options: List<PreparationOption>.unmodifiable(options),
    );
  }

  @override
  Future<List<PreparationRole>> listRoles(String scenarioId) async {
    _resourceId(scenarioId);
    final response = await _get(
      '/v1/scenario-definitions/${Uri.encodeComponent(scenarioId)}'
      '/role-definitions',
    );
    final root = _object(
      _decode(response.body),
      required: const <String>{'roles'},
    );
    final rawRoles = root['roles'];
    if (rawRoles is! List<Object?> ||
        rawRoles.isEmpty ||
        rawRoles.length > 50) {
      throw _invalidResponse();
    }
    final ids = <String>{};
    final roles = <PreparationRole>[];
    for (final value in rawRoles) {
      final role = _role(value);
      if (role.scenarioId != scenarioId || !ids.add(role.id)) {
        throw _invalidResponse();
      }
      roles.add(role);
    }
    return List<PreparationRole>.unmodifiable(roles);
  }

  Future<IdentityHttpResponse> _get(String path) async {
    final generation = _accountGeneration;
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: 'GET',
            uri: uri,
            headers: const <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
            },
          )
          .timeout(_requestTimeout);
    } on TimeoutException {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.network,
        retryable: true,
      );
    } on _PreparationTransportResponseException {
      throw _invalidResponse();
    }
    if (generation != _accountGeneration) {
      throw const PreparationCatalogException(
        kind: PreparationCatalogFailureKind.superseded,
      );
    }
    if (response.statusCode == HttpStatus.ok) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw _invalidResponse();
      }
      return response;
    }
    if (response.statusCode == HttpStatus.notFound ||
        response.statusCode >= 500) {
      throw PreparationCatalogException(
        kind: PreparationCatalogFailureKind.unavailable,
        statusCode: response.statusCode,
        retryable: response.statusCode >= 500,
      );
    }
    throw PreparationCatalogException(
      kind: PreparationCatalogFailureKind.invalidResponse,
      statusCode: response.statusCode,
    );
  }

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
  }
}

Object? _decode(String body) {
  try {
    return jsonDecode(body);
  } on FormatException {
    throw _invalidResponse();
  }
}

Map<String, Object?> _object(
  Object? value, {
  Set<String> required = const <String>{},
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw _invalidResponse();
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw _invalidResponse();
  }
  return value;
}

PreparationScenario _scenario(Object? value, {String? summary}) {
  final object = _object(
    value,
    required: <String>{
      'scenario_definition_id',
      'scenario_type',
      'scenario_model',
      'name',
      'version',
      'status',
      'turn_policy_ref',
      'session_policy_ref',
      if (summary == null) 'summary',
    },
  );
  final status = _string(object['status'], maximumBytes: 16);
  if (status != 'active' && status != 'inactive') {
    throw _invalidResponse();
  }
  final type = _wireEnum(object['scenario_type']);
  final model = _wireEnum(object['scenario_model']);
  if (!_validScenarioFamilyModel(type, model)) {
    throw _invalidResponse();
  }
  _resourceId(object['turn_policy_ref']);
  _resourceId(object['session_policy_ref']);
  return PreparationScenario(
    id: _resourceId(object['scenario_definition_id']),
    type: type,
    model: model,
    name: _string(object['name']),
    summary: summary ?? _string(object['summary']),
    version: _version(object['version']),
    status: status,
  );
}

PreparationScenarioConfig _scenarioConfig(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scenario_config_id',
      'scenario_definition_id',
      'config_type',
      'scenario_model',
      'version',
      'prompt_model',
    },
    optional: const <String>{'job_title', 'job_description'},
  );
  final type = _wireEnum(object['config_type']);
  final model = _wireEnum(object['scenario_model']);
  if (!_validScenarioFamilyModel(type, model)) {
    throw _invalidResponse();
  }
  final prompt = _scenarioPrompt(object['prompt_model']);
  return PreparationScenarioConfig(
    id: _resourceId(object['scenario_config_id']),
    scenarioId: _resourceId(object['scenario_definition_id']),
    type: type,
    model: model,
    version: _version(object['version']),
    jobTitle: object.containsKey('job_title')
        ? _string(object['job_title'])
        : null,
    jobDescription: object.containsKey('job_description')
        ? _string(object['job_description'])
        : null,
    prompt: prompt,
  );
}

PreparationScenarioPrompt _scenarioPrompt(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'public_scene_brief',
      'practice_goal',
      'user_role',
      'ai_role',
      'persona_summary',
      'focus_areas',
      'turn_blueprints',
      'suggested_duration_seconds',
    },
  );
  final duration = object['suggested_duration_seconds'];
  if (duration is! int || duration < 1 || duration > 3600) {
    throw _invalidResponse();
  }
  return PreparationScenarioPrompt(
    publicSceneBrief: _string(object['public_scene_brief']),
    practiceGoal: _string(object['practice_goal']),
    userRole: _string(object['user_role']),
    aiRole: _string(object['ai_role']),
    personaSummary: _string(object['persona_summary']),
    focusAreas: _stringList(object['focus_areas']),
    turnBlueprints: _stringList(object['turn_blueprints']),
    suggestedDurationSeconds: duration,
  );
}

bool _validScenarioFamilyModel(String family, String model) {
  return switch ((family, model)) {
    ('INTERVIEW', 'PROJECT_EXPERIENCE_DEEP_DIVE') ||
    ('INTERVIEW', 'INTERVIEW_BASIC_DIALOGUE') ||
    ('EXAM', 'IELTS_SPEAKING_PART_2') ||
    ('EXAM', 'EXAM_BASIC_DIALOGUE') ||
    ('WORKPLACE', 'PROGRESS_AND_RISK_UPDATE') ||
    ('WORKPLACE', 'WORKPLACE_BASIC_DIALOGUE') ||
    ('DAILY', 'HOTEL_CHECKIN_AND_ISSUE_HANDLING') ||
    ('DAILY', 'DAILY_BASIC_DIALOGUE') => true,
    _ => false,
  };
}

PreparationRole _role(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'role_definition_id',
      'scenario_definition_id',
      'role_type',
      'display_name',
      'responsibilities',
      'style',
      'focus_areas',
      'version',
    },
    optional: const <String>{'voice_config_ref'},
  );
  return PreparationRole(
    id: _resourceId(object['role_definition_id']),
    scenarioId: _resourceId(object['scenario_definition_id']),
    type: _wireEnum(object['role_type']),
    displayName: _string(object['display_name']),
    responsibilities: _string(object['responsibilities']),
    style: _string(object['style']),
    focusAreas: _stringList(object['focus_areas']),
    version: _version(object['version']),
    voiceConfigRef: object.containsKey('voice_config_ref')
        ? _string(object['voice_config_ref'])
        : null,
  );
}

PreparationOption _practiceOption(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'practice_option_id',
      'scenario_definition_id',
      'practice_option_type',
      'display_name',
      'version',
    },
    optional: const <String>{'role_definition_id'},
  );
  final type = switch (_wireEnum(object['practice_option_type'])) {
    'FULL_SIMULATION' => PreparationOptionType.fullSimulation,
    'FOCUS' => PreparationOptionType.focus,
    _ => throw _invalidResponse(),
  };
  final roleId = object.containsKey('role_definition_id')
      ? _resourceId(object['role_definition_id'])
      : null;
  if ((type == PreparationOptionType.fullSimulation && roleId != null) ||
      (type == PreparationOptionType.focus && roleId == null)) {
    throw _invalidResponse();
  }
  return PreparationOption(
    id: _resourceId(object['practice_option_id']),
    scenarioId: _resourceId(object['scenario_definition_id']),
    roleId: roleId,
    type: type,
    displayName: _string(object['display_name']),
    version: _version(object['version']),
  );
}

String _resourceId(Object? value) => _string(value, maximumBytes: 128);

String _wireEnum(Object? value) {
  final result = _string(value, maximumBytes: 64);
  if (!RegExp(r'^[A-Z][A-Z0-9_]*$').hasMatch(result)) {
    throw _invalidResponse();
  }
  return result;
}

int _version(Object? value) {
  if (value is! int || value < 1) {
    throw _invalidResponse();
  }
  return value;
}

String _string(Object? value, {int maximumBytes = 4096}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maximumBytes) {
    throw _invalidResponse();
  }
  return value;
}

List<String> _stringList(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw _invalidResponse();
  }
  final seen = <String>{};
  final result = <String>[];
  for (final item in value) {
    final text = _string(item, maximumBytes: 128);
    if (!seen.add(text)) {
      throw _invalidResponse();
    }
    result.add(text);
  }
  return List<String>.unmodifiable(result);
}

PreparationCatalogException _invalidResponse() =>
    const PreparationCatalogException(
      kind: PreparationCatalogFailureKind.invalidResponse,
    );

final class _PreparationTransportResponseException implements Exception {
  const _PreparationTransportResponseException();
}

final class _IoPreparationCatalogTransport implements IdentityHttpTransport {
  _IoPreparationCatalogTransport(this.maximumBodyBytes)
    : _httpClient = HttpClient();

  final int maximumBodyBytes;
  final HttpClient _httpClient;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    final request = await _httpClient.openUrl(method, uri);
    request.followRedirects = false;
    headers.forEach(request.headers.set);
    if (body != null) {
      request.write(body);
    }
    final response = await request.close();
    if (response.contentLength > maximumBodyBytes) {
      throw const _PreparationTransportResponseException();
    }
    final bytes = BytesBuilder(copy: false);
    var received = 0;
    await for (final chunk in response) {
      received += chunk.length;
      if (received > maximumBodyBytes) {
        throw const _PreparationTransportResponseException();
      }
      bytes.add(chunk);
    }
    late final String responseBody;
    try {
      responseBody = utf8.decode(bytes.takeBytes());
    } on FormatException {
      throw const _PreparationTransportResponseException();
    }
    final responseHeaders = <String, String>{};
    response.headers.forEach((name, values) {
      responseHeaders[name] = values.join(',');
    });
    return IdentityHttpResponse(
      statusCode: response.statusCode,
      body: responseBody,
      headers: responseHeaders,
    );
  }
}
