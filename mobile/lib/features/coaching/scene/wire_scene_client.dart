import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireSceneClient implements SceneClient, SceneQuestionBankClient {
  factory WireSceneClient({
    required Uri baseUri,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 15),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WireSceneClient._(
      baseUri,
      transport ?? _IoSceneTransport(_maximumBodyBytes),
      requestTimeout,
    );
  }

  WireSceneClient._(this._baseUri, this._transport, this._requestTimeout)
    : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  static const _maximumBodyBytes = 256 * 1024;

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<List<SceneDefinition>> listScenes() async {
    final response = await _get('/v1/scenes');
    final root = _object(
      _decode(response.body),
      required: const <String>{'scenes'},
    );
    final rawScenes = root['scenes'];
    if (rawScenes is! List<Object?> || rawScenes.length > 50) {
      throw _invalidResponse();
    }
    final ids = <String>{};
    final scenes = <SceneDefinition>[];
    for (final value in rawScenes) {
      final scene = _sceneDefinition(value);
      if (scene.status != SceneStatus.active || !ids.add(scene.id)) {
        throw _invalidResponse();
      }
      scenes.add(scene);
    }
    return List<SceneDefinition>.unmodifiable(scenes);
  }

  @override
  Future<SceneDefinition> getScene(String sceneId) async {
    _resourceId(sceneId);
    final response = await _get('/v1/scenes/${Uri.encodeComponent(sceneId)}');
    final scene = _sceneDefinition(_decode(response.body));
    if (scene.id != sceneId || scene.status != SceneStatus.active) {
      throw _invalidResponse();
    }
    return scene;
  }

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async {
    _resourceId(sceneId);
    final response = await _get(
      '/v1/scenes/${Uri.encodeComponent(sceneId)}/roles',
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
    final roles = <RoleDefinition>[];
    for (final value in rawRoles) {
      final role = _role(value);
      if (role.sceneId != sceneId || !ids.add(role.id)) {
        throw _invalidResponse();
      }
      roles.add(role);
    }
    return List<RoleDefinition>.unmodifiable(roles);
  }

  @override
  Future<IeltsQuestionBank> getIeltsQuestionBank() async {
    final response = await _get('/v1/scenes/ielts-speaking/question-bank');
    final root = _object(
      _decode(response.body),
      required: const <String>{
        'schema_version',
        'bank_id',
        'season',
        'source_cutoff',
        'part1_sets',
        'part1_topics',
        'topic_groups',
      },
    );
    if (root['schema_version'] != 2) {
      throw _invalidResponse();
    }
    final sourceCutoff = DateTime.tryParse(_string(root['source_cutoff']));
    final rawPart1Sets = root['part1_sets'];
    final rawPart1Topics = root['part1_topics'];
    final rawTopicGroups = root['topic_groups'];
    if (sourceCutoff == null ||
        rawPart1Sets is! List<Object?> ||
        rawPart1Sets.length != 38 ||
        rawPart1Topics is! List<Object?> ||
        rawPart1Topics.length != 38 ||
        rawTopicGroups is! List<Object?> ||
        rawTopicGroups.length != 56) {
      throw _invalidResponse();
    }
    final part1Ids = <String>{};
    final part1Sets = rawPart1Sets
        .map((raw) {
          final set = _ieltsPart1Set(raw);
          if (!part1Ids.add(set.id)) {
            throw _invalidResponse();
          }
          return set;
        })
        .toList(growable: false);
    final groupIds = <String>{};
    final part1TopicIds = <String>{};
    final part1Topics = rawPart1Topics
        .map((raw) {
          final topic = _ieltsPart1PracticeTopic(raw);
          if (!part1TopicIds.add(topic.id)) {
            throw _invalidResponse();
          }
          return topic;
        })
        .toList(growable: false);
    final topicGroups = rawTopicGroups
        .map((raw) {
          final group = _ieltsTopicGroup(raw);
          if (!groupIds.add(group.id)) {
            throw _invalidResponse();
          }
          return group;
        })
        .toList(growable: false);
    return IeltsQuestionBank(
      bankId: _resourceId(root['bank_id']),
      season: _string(root['season']),
      sourceCutoff: sourceCutoff.toUtc(),
      part1Sets: List<IeltsPart1Set>.unmodifiable(part1Sets),
      part1Topics: List<IeltsPart1PracticeTopic>.unmodifiable(part1Topics),
      topicGroups: List<IeltsTopicGroup>.unmodifiable(topicGroups),
    );
  }

  Future<IdentityHttpResponse> _get(String path) async {
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
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    } on _SceneTransportResponseException {
      throw _invalidResponse();
    }
    if (response.statusCode == HttpStatus.ok) {
      if (utf8.encode(response.body).length > _maximumBodyBytes) {
        throw _invalidResponse();
      }
      return response;
    }
    if (response.statusCode == HttpStatus.notFound ||
        response.statusCode >= 500) {
      throw SceneClientException(
        kind: SceneClientFailureKind.unavailable,
        statusCode: response.statusCode,
        retryable: response.statusCode >= 500,
      );
    }
    throw SceneClientException(
      kind: SceneClientFailureKind.invalidResponse,
      statusCode: response.statusCode,
    );
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

SceneDefinition _sceneDefinition(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'scene_id',
      'scene_family',
      'scene_model',
      'name',
      'scene_version',
      'status',
      'turn_policy_ref',
      'session_policy_ref',
      'evaluation_policy_ref',
      'prompt',
      'roles',
      'practice_options',
    },
  );
  final status = switch (_string(object['status'], maximumBytes: 16)) {
    'active' => SceneStatus.active,
    'inactive' => SceneStatus.inactive,
    _ => throw _invalidResponse(),
  };
  final family = SceneFamily.fromWireValue(_wireEnum(object['scene_family']));
  final model = SceneModel.fromWireValue(_wireEnum(object['scene_model']));
  if (family == null ||
      model == null ||
      !_validSceneFamilyModel(family, model)) {
    throw _invalidResponse();
  }
  final sceneId = _resourceId(object['scene_id']);
  final rawRoles = object['roles'];
  final rawOptions = object['practice_options'];
  if (rawRoles is! List<Object?> ||
      rawRoles.isEmpty ||
      rawRoles.length > 50 ||
      rawOptions is! List<Object?> ||
      rawOptions.isEmpty ||
      rawOptions.length > 100) {
    throw _invalidResponse();
  }
  final roleIds = <String>{};
  final roles = <RoleDefinition>[];
  for (final value in rawRoles) {
    final role = _role(value);
    if (role.sceneId != sceneId || !roleIds.add(role.id)) {
      throw _invalidResponse();
    }
    roles.add(role);
  }
  final optionIds = <String>{};
  final options = <PracticeOption>[];
  for (final value in rawOptions) {
    final option = _practiceOption(value);
    if (option.sceneId != sceneId ||
        !optionIds.add(option.id) ||
        (option.roleId != null && !roleIds.contains(option.roleId))) {
      throw _invalidResponse();
    }
    options.add(option);
  }
  return SceneDefinition(
    id: sceneId,
    family: family,
    model: model,
    name: _string(object['name']),
    version: _version(object['scene_version']),
    status: status,
    turnPolicyRef: _resourceId(object['turn_policy_ref']),
    sessionPolicyRef: _resourceId(object['session_policy_ref']),
    evaluationPolicyRef: _resourceId(object['evaluation_policy_ref']),
    prompt: _scenePrompt(object['prompt']),
    roles: List<RoleDefinition>.unmodifiable(roles),
    practiceOptions: List<PracticeOption>.unmodifiable(options),
  );
}

ScenePrompt _scenePrompt(Object? value) {
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
  return ScenePrompt(
    publicSceneBrief: _string(object['public_scene_brief']),
    practiceGoal: _string(object['practice_goal']),
    userRole: _string(object['user_role']),
    aiRole: _string(object['ai_role']),
    personaSummary: _string(object['persona_summary']),
    focusAreas: _stringList(object['focus_areas']),
    turnBlueprints: _stringList(
      object['turn_blueprints'],
      maximumItemBytes: 4096,
    ),
    suggestedDurationSeconds: duration,
  );
}

bool _validSceneFamilyModel(SceneFamily family, SceneModel model) {
  return switch ((family, model)) {
    (SceneFamily.interview, SceneModel.projectExperienceDeepDive) ||
    (SceneFamily.interview, SceneModel.interviewBasicDialogue) ||
    (SceneFamily.exam, SceneModel.ieltsSpeakingPart1) ||
    (SceneFamily.exam, SceneModel.ieltsSpeakingPart2) ||
    (SceneFamily.exam, SceneModel.ieltsSpeakingPart3) ||
    (SceneFamily.exam, SceneModel.ieltsSpeakingFullMock) ||
    (SceneFamily.exam, SceneModel.examBasicDialogue) ||
    (SceneFamily.workplace, SceneModel.progressAndRiskUpdate) ||
    (SceneFamily.workplace, SceneModel.workplaceBasicDialogue) ||
    (SceneFamily.daily, SceneModel.hotelCheckinAndIssueHandling) ||
    (SceneFamily.daily, SceneModel.dailyBasicDialogue) => true,
    _ => false,
  };
}

IeltsPart1Set _ieltsPart1Set(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'id',
      'title',
      'topics',
      'question_count',
      'published',
    },
  );
  final rawTopics = object['topics'];
  if (rawTopics is! List<Object?> ||
      rawTopics.length != 3 ||
      object['question_count'] != 8 ||
      object['published'] != true) {
    throw _invalidResponse();
  }
  final topics = rawTopics.map(_ieltsPart1Topic).toList(growable: false);
  if (topics.fold<int>(0, (total, topic) => total + topic.questions.length) !=
      8) {
    throw _invalidResponse();
  }
  return IeltsPart1Set(
    id: _resourceId(object['id']),
    title: _string(object['title']),
    topics: List<IeltsPart1Topic>.unmodifiable(topics),
    questionCount: 8,
  );
}

IeltsPart1Topic _ieltsPart1Topic(Object? value) {
  final object = _object(
    value,
    required: const <String>{'title', 'release', 'questions'},
  );
  final release = _string(object['release'], maximumBytes: 32);
  if (!const <String>{'new', 'carry_over', 'evergreen'}.contains(release)) {
    throw _invalidResponse();
  }
  final questions = _stringList(object['questions'], maximumItemBytes: 1024);
  if (questions.length < 2) {
    throw _invalidResponse();
  }
  return IeltsPart1Topic(
    title: _string(object['title']),
    release: release,
    questions: questions,
  );
}

IeltsPart1PracticeTopic _ieltsPart1PracticeTopic(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'id',
      'title_zh',
      'title_en',
      'release',
      'category',
      'questions',
      'published',
    },
  );
  final release = _string(object['release'], maximumBytes: 32);
  final category = IeltsTopicCategory.fromWireName(
    _string(object['category'], maximumBytes: 16),
  );
  final questions = _stringList(object['questions'], maximumItemBytes: 1024);
  if (!const <String>{'new', 'carry_over', 'evergreen'}.contains(release) ||
      category == null ||
      questions.length < 2 ||
      object['published'] != true) {
    throw _invalidResponse();
  }
  return IeltsPart1PracticeTopic(
    id: _resourceId(object['id']),
    titleZh: _string(object['title_zh']),
    titleEn: _string(object['title_en']),
    release: release,
    category: category,
    questions: questions,
  );
}

IeltsTopicGroup _ieltsTopicGroup(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'id',
      'title_zh',
      'release',
      'region',
      'category',
      'part2',
      'part3_questions',
      'published',
      'supplemented_question_count',
    },
  );
  final release = _string(object['release'], maximumBytes: 32);
  final category = IeltsTopicCategory.fromWireName(
    _string(object['category'], maximumBytes: 16),
  );
  final supplemented = object['supplemented_question_count'];
  if (!const <String>{'new', 'carry_over'}.contains(release) ||
      category == null ||
      object['region'] != 'mainland' ||
      object['published'] != true ||
      supplemented is! int ||
      supplemented < 0 ||
      supplemented > 5) {
    throw _invalidResponse();
  }
  final cueObject = _object(
    object['part2'],
    required: const <String>{'prompt', 'points'},
  );
  final points = _stringList(cueObject['points'], maximumItemBytes: 1024);
  final questions = _stringList(
    object['part3_questions'],
    maximumItemBytes: 1024,
  );
  if (points.length < 3 || questions.isEmpty || questions.length > 5) {
    throw _invalidResponse();
  }
  return IeltsTopicGroup(
    id: _resourceId(object['id']),
    title: _string(object['title_zh']),
    release: release,
    category: category,
    cueCard: IeltsCueCard(prompt: _string(cueObject['prompt']), points: points),
    part3Questions: questions,
    supplementedQuestionCount: supplemented,
  );
}

RoleDefinition _role(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'role_definition_id',
      'scene_id',
      'role_type',
      'display_name',
      'responsibilities',
      'style',
      'practice_objectives',
    },
    optional: const <String>{'voice_config_ref'},
  );
  return RoleDefinition(
    id: _resourceId(object['role_definition_id']),
    sceneId: _resourceId(object['scene_id']),
    type: _wireEnum(object['role_type']),
    displayName: _string(object['display_name']),
    responsibilities: _string(object['responsibilities']),
    style: _string(object['style']),
    practiceObjectives: _rolePracticeObjectives(object['practice_objectives']),
    voiceConfigRef: object.containsKey('voice_config_ref')
        ? _string(object['voice_config_ref'])
        : null,
  );
}

List<RolePracticeObjective> _rolePracticeObjectives(Object? value) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw _invalidResponse();
  }
  final ids = <String>{};
  final objectives = <RolePracticeObjective>[];
  for (final item in value) {
    final object = _object(
      item,
      required: const <String>{'objective_id', 'description'},
    );
    final objectiveId = _resourceId(object['objective_id']);
    if (!ids.add(objectiveId)) {
      throw _invalidResponse();
    }
    objectives.add(
      RolePracticeObjective(
        objectiveId: objectiveId,
        description: _string(object['description']),
      ),
    );
  }
  return List<RolePracticeObjective>.unmodifiable(objectives);
}

PracticeOption _practiceOption(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'practice_option_id',
      'scene_id',
      'practice_option_type',
      'display_name',
    },
    optional: const <String>{'role_definition_id'},
  );
  final type = PracticeOptionType.fromWireValue(
    _wireEnum(object['practice_option_type']),
  );
  if (type == null) {
    throw _invalidResponse();
  }
  final roleId = object.containsKey('role_definition_id')
      ? _resourceId(object['role_definition_id'])
      : null;
  if ((type == PracticeOptionType.fullSimulation && roleId != null) ||
      (type == PracticeOptionType.focus && roleId == null)) {
    throw _invalidResponse();
  }
  return PracticeOption(
    id: _resourceId(object['practice_option_id']),
    sceneId: _resourceId(object['scene_id']),
    roleId: roleId,
    type: type,
    displayName: _string(object['display_name']),
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

List<String> _stringList(Object? value, {int maximumItemBytes = 128}) {
  if (value is! List<Object?> || value.isEmpty || value.length > 50) {
    throw _invalidResponse();
  }
  final seen = <String>{};
  final result = <String>[];
  for (final item in value) {
    final text = _string(item, maximumBytes: maximumItemBytes);
    if (!seen.add(text)) {
      throw _invalidResponse();
    }
    result.add(text);
  }
  return List<String>.unmodifiable(result);
}

SceneClientException _invalidResponse() =>
    const SceneClientException(kind: SceneClientFailureKind.invalidResponse);

final class _SceneTransportResponseException implements Exception {
  const _SceneTransportResponseException();
}

final class _IoSceneTransport implements IdentityHttpTransport {
  _IoSceneTransport(this.maximumBodyBytes) : _httpClient = HttpClient();

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
      throw const _SceneTransportResponseException();
    }
    final bytes = BytesBuilder(copy: false);
    var received = 0;
    await for (final chunk in response) {
      received += chunk.length;
      if (received > maximumBodyBytes) {
        throw const _SceneTransportResponseException();
      }
      bytes.add(chunk);
    }
    late final String responseBody;
    try {
      responseBody = utf8.decode(bytes.takeBytes());
    } on FormatException {
      throw const _SceneTransportResponseException();
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
