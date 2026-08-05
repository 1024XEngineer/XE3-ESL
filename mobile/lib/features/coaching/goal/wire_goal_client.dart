import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/coaching/goal/goal.dart';
import 'package:speakup/features/coaching/goal/goal_client.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

/// Authenticated Goal storage and Agent Thread activation owned by Coaching.
final class WireGoalClient implements GoalClient, GoalActivationClient {
  factory WireGoalClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration requestTimeout = const Duration(seconds: 20),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(requestTimeout, 'requestTimeout');
    }
    return WireGoalClient._(
      baseUri,
      SessionAuthenticatedHttpTransport(
        transport: transport ?? IoIdentityHttpTransport(),
        credentialProvider: credentialProvider,
        invalidateSession: invalidateSession,
        trustedBaseUri: baseUri,
      ),
      requestTimeout,
    );
  }

  WireGoalClient._(this._baseUri, this._transport, this._requestTimeout)
    : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  static const _maximumResponseBytes = 1024 * 1024;

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final Duration _requestTimeout;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  int _accountGeneration = 0;
  final Set<Future<void>> _inFlightOperations = <Future<void>>{};
  final Map<_GoalActivationKey, _GoalActivation> _activations =
      <_GoalActivationKey, _GoalActivation>{};
  Future<void>? _cleanupFuture;

  @override
  Future<Goal> createGoal({required String title}) {
    _requireTitle(title);
    return _runAccountOperation((generation) async {
      final response = await _send(
        generation: generation,
        method: 'POST',
        path: '/v1/goals',
        body: <String, Object?>{'title': title},
      );
      _requireStatus(response, const <int>{HttpStatus.created});
      return _decodeGoal(response.body);
    });
  }

  @override
  Future<Goal> getGoal(String goalId) {
    _requireUuid(goalId);
    return _runAccountOperation(
      (generation) => _loadGoal(generation: generation, goalId: goalId),
    );
  }

  @override
  Future<List<Goal>> listGoals() {
    return _runAccountOperation((generation) async {
      return List<Goal>.unmodifiable(await _listGoals(generation));
    });
  }

  @override
  Future<Goal> startScene({
    required String threadId,
    required SceneDefinition scene,
    required String clientOperationId,
  }) {
    _requireUuid(threadId);
    _requireClientIdentity(clientOperationId);
    _requireTitle(scene.name);
    return _runAccountOperation((generation) async {
      final key = (
        generation: generation,
        threadId: threadId,
        clientOperationId: clientOperationId,
      );
      final existing = _activations[key];
      final activation =
          existing ?? _GoalActivation(sceneId: scene.id, title: scene.name);
      if (existing == null) {
        _activations[key] = activation;
      } else if (existing.sceneId != scene.id || existing.title != scene.name) {
        throw const GoalClientException(
          kind: GoalClientFailureKind.conflict,
          errorCode: 'idempotency_key_conflict',
        );
      }
      final goal = await _resolveActivationGoal(
        generation: generation,
        key: key,
        activation: activation,
      );
      final response = await _send(
        generation: generation,
        method: 'PUT',
        path: '/v1/agent-threads/$threadId/active-goal',
        body: <String, Object?>{'goal_id': goal.id},
      );
      _requireStatus(response, const <int>{HttpStatus.ok});
      _decodeGoalLink(
        response.body,
        expectedThreadId: threadId,
        expectedGoalId: goal.id,
      );
      return goal;
    });
  }

  @override
  Future<Goal> selectExistingGoal({
    required String threadId,
    required String goalId,
  }) {
    _requireUuid(threadId);
    _requireUuid(goalId);
    return _runAccountOperation((generation) async {
      final goal = await _loadGoal(generation: generation, goalId: goalId);
      if (goal.status != GoalStatus.active) {
        throw const GoalClientException(
          kind: GoalClientFailureKind.conflict,
          errorCode: 'goal_not_active',
        );
      }
      final response = await _send(
        generation: generation,
        method: 'PUT',
        path: '/v1/agent-threads/$threadId/active-goal',
        body: <String, Object?>{'goal_id': goalId},
      );
      _requireStatus(response, const <int>{HttpStatus.ok});
      _decodeGoalLink(
        response.body,
        expectedThreadId: threadId,
        expectedGoalId: goalId,
      );
      return goal;
    });
  }

  @override
  Future<void> clearAccountState() {
    final existing = _cleanupFuture;
    if (existing != null) {
      return existing;
    }
    _accountGeneration++;
    _activations.clear();
    final cleanup = Future.wait<void>(
      List<Future<void>>.of(_inFlightOperations),
    );
    _cleanupFuture = cleanup;
    return cleanup.whenComplete(() {
      if (identical(_cleanupFuture, cleanup)) {
        _cleanupFuture = null;
      }
    });
  }

  Future<Goal> _loadGoal({
    required int generation,
    required String goalId,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/goals/$goalId',
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    final goal = _decodeGoal(response.body);
    if (goal.id != goalId) {
      throw _invalidResponse();
    }
    return goal;
  }

  Future<List<Goal>> _listGoals(int generation) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/goals',
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    return _decodeGoalList(response.body);
  }

  Future<Goal> _resolveActivationGoal({
    required int generation,
    required _GoalActivationKey key,
    required _GoalActivation activation,
  }) {
    final resolved = activation.goal;
    if (resolved != null) {
      return Future<Goal>.value(resolved);
    }
    final pending = activation.pendingGoal;
    if (pending != null) {
      return pending;
    }
    late final Future<Goal> operation;
    operation =
        _createOrRecoverActivationGoal(
              generation: generation,
              key: key,
              activation: activation,
            )
            .then((goal) {
              _requireActivationCurrent(
                generation: generation,
                key: key,
                activation: activation,
              );
              activation.goal = goal;
              return goal;
            })
            .whenComplete(() {
              if (identical(activation.pendingGoal, operation)) {
                activation.pendingGoal = null;
              }
            });
    activation.pendingGoal = operation;
    return operation;
  }

  Future<Goal> _createOrRecoverActivationGoal({
    required int generation,
    required _GoalActivationKey key,
    required _GoalActivation activation,
  }) async {
    if (activation.baselineGoalIds == null) {
      final goals = await _listGoals(generation);
      _requireActivationCurrent(
        generation: generation,
        key: key,
        activation: activation,
      );
      activation.baselineGoalIds = {
        for (final goal in goals)
          if (goal.title == activation.title) goal.id,
      };
    } else if (activation.createAmbiguous) {
      final goals = await _listGoals(generation);
      _requireActivationCurrent(
        generation: generation,
        key: key,
        activation: activation,
      );
      final recovered = [
        for (final goal in goals)
          if (goal.title == activation.title &&
              goal.status == GoalStatus.active &&
              !activation.baselineGoalIds!.contains(goal.id))
            goal,
      ];
      if (recovered.length > 1) {
        throw const GoalClientException(
          kind: GoalClientFailureKind.conflict,
          errorCode: 'resource_conflict',
        );
      }
      if (recovered case [final goal]) {
        return goal;
      }
      activation.createAmbiguous = false;
    }

    try {
      final response = await _send(
        generation: generation,
        method: 'POST',
        path: '/v1/goals',
        body: <String, Object?>{'title': activation.title},
      );
      _requireStatus(response, const <int>{HttpStatus.created});
      final goal = _decodeGoal(response.body);
      if (goal.title != activation.title || goal.status != GoalStatus.active) {
        throw _invalidResponse();
      }
      return goal;
    } on GoalClientException catch (error) {
      if (error.kind == GoalClientFailureKind.network ||
          error.kind == GoalClientFailureKind.invalidResponse) {
        _requireActivationCurrent(
          generation: generation,
          key: key,
          activation: activation,
        );
        activation.createAmbiguous = true;
      }
      rethrow;
    }
  }

  void _requireActivationCurrent({
    required int generation,
    required _GoalActivationKey key,
    required _GoalActivation activation,
  }) {
    _requireCurrent(generation);
    if (!identical(_activations[key], activation)) {
      throw const GoalClientException(kind: GoalClientFailureKind.superseded);
    }
  }

  Future<T> _runAccountOperation<T>(
    Future<T> Function(int generation) operation,
  ) {
    final generation = _accountGeneration;
    final result = Future<T>.sync(() => operation(generation));
    late final Future<void> tracked;
    tracked = result.then<void>((_) {}, onError: (_) {}).whenComplete(() {
      _inFlightOperations.remove(tracked);
    });
    _inFlightOperations.add(tracked);
    return result;
  }

  Future<IdentityHttpResponse> _send({
    required int generation,
    required String method,
    required String path,
    Map<String, Object?>? body,
  }) async {
    _requireCurrent(generation);
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    late final IdentityHttpResponse response;
    try {
      response = await _transport
          .send(
            method: method,
            uri: uri,
            headers: <String, String>{
              HttpHeaders.acceptHeader: ContentType.json.mimeType,
              if (body != null)
                HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
            },
            body: body == null ? null : jsonEncode(body),
          )
          .timeout(_requestTimeout);
    } on AuthSessionSupersededException {
      throw const GoalClientException(kind: GoalClientFailureKind.superseded);
    } on StateError {
      throw const GoalClientException(
        kind: GoalClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    } on TimeoutException {
      throw const GoalClientException(
        kind: GoalClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const GoalClientException(
        kind: GoalClientFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const GoalClientException(
        kind: GoalClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const GoalClientException(
        kind: GoalClientFailureKind.network,
        retryable: true,
      );
    }
    _requireCurrent(generation);
    if (utf8.encode(response.body).length > _maximumResponseBytes) {
      throw _invalidResponse();
    }
    return response;
  }

  void _requireStatus(
    IdentityHttpResponse response,
    Set<int> expectedStatuses,
  ) {
    if (!expectedStatuses.contains(response.statusCode)) {
      throw _responseException(response);
    }
  }

  void _requireCurrent(int generation) {
    if (generation != _accountGeneration) {
      throw const GoalClientException(kind: GoalClientFailureKind.superseded);
    }
  }
}

typedef _GoalActivationKey = ({
  int generation,
  String threadId,
  String clientOperationId,
});

final class _GoalActivation {
  _GoalActivation({required this.sceneId, required this.title});

  final String sceneId;
  final String title;
  Set<String>? baselineGoalIds;
  Goal? goal;
  Future<Goal>? pendingGoal;
  bool createAmbiguous = false;
}

List<Goal> _decodeGoalList(String body) {
  final root = _decodeObject(body, required: const <String>{'goals'});
  final values = root['goals'];
  if (values is! List<Object?> || values.length > 1000) {
    throw _invalidResponse();
  }
  final ids = <String>{};
  final goals = <Goal>[];
  for (final value in values) {
    final goal = _decodeGoalObject(value);
    if (!ids.add(goal.id)) {
      throw _invalidResponse();
    }
    goals.add(goal);
  }
  return goals;
}

Goal _decodeGoal(String body) => _decodeGoalObject(_decodeJson(body));

Goal _decodeGoalObject(Object? value) {
  final object = _object(
    value,
    required: const <String>{
      'goal_id',
      'title',
      'status',
      'version',
      'created_at',
      'updated_at',
    },
  );
  final createdAt = _dateTime(object['created_at']);
  final updatedAt = _dateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw _invalidResponse();
  }
  return Goal(
    id: _uuid(object['goal_id']),
    title: _title(object['title']),
    status: switch (_string(object['status'], maximumRunes: 32)) {
      'active' => GoalStatus.active,
      'completed' => GoalStatus.completed,
      'archived' => GoalStatus.archived,
      _ => throw _invalidResponse(),
    },
    version: _integer(object['version'], minimum: 1),
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

void _decodeGoalLink(
  String body, {
  required String expectedThreadId,
  required String expectedGoalId,
}) {
  final object = _decodeObject(
    body,
    required: const <String>{
      'thread_id',
      'goal_id',
      'active',
      'linked_at',
      'updated_at',
    },
  );
  if (_uuid(object['thread_id']) != expectedThreadId ||
      _uuid(object['goal_id']) != expectedGoalId ||
      object['active'] != true) {
    throw _invalidResponse();
  }
  _dateTime(object['linked_at']);
  _dateTime(object['updated_at']);
}

Map<String, Object?> _decodeObject(
  String body, {
  required Set<String> required,
}) => _object(_decodeJson(body), required: required);

Object? _decodeJson(String body) {
  try {
    return jsonDecode(body);
  } on FormatException {
    throw _invalidResponse();
  }
}

Map<String, Object?> _object(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?> ||
      !value.keys.toSet().containsAll(required) ||
      value.keys.any(
        (key) => !required.contains(key) && !optional.contains(key),
      )) {
    throw _invalidResponse();
  }
  return value;
}

String _title(Object? value) {
  final title = _string(value, maximumRunes: 200);
  if (title.trim().isEmpty ||
      title.contains('\u0000') ||
      utf8.encode(title).length > 512) {
    throw _invalidResponse();
  }
  return title;
}

String _string(Object? value, {required int maximumRunes}) {
  if (value is! String ||
      value.runes.isEmpty ||
      value.runes.length > maximumRunes) {
    throw _invalidResponse();
  }
  return value;
}

String _uuid(Object? value) {
  final id = _string(value, maximumRunes: 36);
  if (!_uuidPattern.hasMatch(id)) {
    throw _invalidResponse();
  }
  return id;
}

int _integer(Object? value, {required int minimum}) {
  if (value is! int || value < minimum) {
    throw _invalidResponse();
  }
  return value;
}

DateTime _dateTime(Object? value) {
  final raw = _string(value, maximumRunes: 64);
  final parsed = DateTime.tryParse(raw);
  if (parsed == null || !raw.contains(RegExp(r'(Z|[+-]\d{2}:\d{2})$'))) {
    throw _invalidResponse();
  }
  return parsed.toUtc();
}

void _requireUuid(String value) {
  if (!_uuidPattern.hasMatch(value)) {
    throw const GoalClientException(kind: GoalClientFailureKind.invalidRequest);
  }
}

void _requireClientIdentity(String value) {
  if (!_clientIdentityPattern.hasMatch(value) || value.length > 128) {
    throw const GoalClientException(kind: GoalClientFailureKind.invalidRequest);
  }
}

void _requireTitle(String value) {
  if (value.trim().isEmpty ||
      value.contains('\u0000') ||
      value.runes.length > 200 ||
      utf8.encode(value).length > 512) {
    throw const GoalClientException(kind: GoalClientFailureKind.invalidRequest);
  }
}

GoalClientException _responseException(IdentityHttpResponse response) {
  String? code;
  String? correlationId;
  try {
    final root = _object(
      jsonDecode(response.body),
      required: const <String>{'error'},
    );
    final error = _object(
      root['error'],
      required: const <String>{
        'code',
        'message',
        'retryable',
        'correlation_id',
      },
      optional: const <String>{'details'},
    );
    code = _string(error['code'], maximumRunes: 64);
    _string(error['message'], maximumRunes: 512);
    if (error['retryable'] is! bool) {
      throw _invalidResponse();
    }
    correlationId = _string(error['correlation_id'], maximumRunes: 128);
  } catch (_) {
    code = null;
    correlationId = null;
  }
  final kind = switch (response.statusCode) {
    HttpStatus.badRequest => GoalClientFailureKind.invalidRequest,
    HttpStatus.unauthorized => GoalClientFailureKind.authenticationRequired,
    HttpStatus.notFound => GoalClientFailureKind.notFound,
    HttpStatus.conflict => GoalClientFailureKind.conflict,
    HttpStatus.tooManyRequests => GoalClientFailureKind.rateLimited,
    >= 500 => GoalClientFailureKind.server,
    _ => GoalClientFailureKind.invalidResponse,
  };
  return GoalClientException(
    kind: kind,
    statusCode: response.statusCode,
    errorCode: code,
    retryable:
        kind == GoalClientFailureKind.rateLimited ||
        kind == GoalClientFailureKind.server,
    correlationId: correlationId,
  );
}

GoalClientException _invalidResponse() => const GoalClientException(
  kind: GoalClientFailureKind.invalidResponse,
  retryable: true,
);

final _uuidPattern = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
);
final _clientIdentityPattern = RegExp(r'^[A-Za-z0-9._:-]+$');
