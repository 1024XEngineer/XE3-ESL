import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/goal/goal_client.dart';
import 'package:speakup/features/coaching/goal/wire_goal_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

import '../../support/scene_fixtures.dart';

void main() {
  test('owned transport sends a Unicode scene title as UTF-8 JSON', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final requests = <_LoopbackRequest>[];
    final subscription = server.listen((request) async {
      final bytes = await request.fold<List<int>>(
        <int>[],
        (buffer, chunk) => buffer..addAll(chunk),
      );
      requests.add(
        _LoopbackRequest(
          method: request.method,
          path: request.uri.path,
          authorization:
              request.headers.value(HttpHeaders.authorizationHeader) ?? '',
          bodyBytes: bytes,
        ),
      );
      final (status, body) = switch ((request.method, request.uri.path)) {
        ('GET', '/v1/goals') => (
          HttpStatus.ok,
          <String, Object?>{'goals': <Object?>[]},
        ),
        ('POST', '/v1/goals') => (
          HttpStatus.created,
          _goalJson(id: _goalId, title: testScenes.first.name),
        ),
        ('PUT', '/v1/agent-threads/$_threadId/active-goal') => (
          HttpStatus.ok,
          _goalLinkJson(goalId: _goalId),
        ),
        _ => (HttpStatus.notFound, _errorJson()),
      };
      final responseBytes = utf8.encode(jsonEncode(body));
      request.response
        ..statusCode = status
        ..headers.contentType = ContentType.json
        ..contentLength = responseBytes.length
        ..add(responseBytes);
      await request.response.close();
    });
    addTearDown(() async {
      await subscription.cancel();
      await server.close(force: true);
    });
    final client = _client(
      baseUri: Uri.parse('http://127.0.0.1:${server.port}'),
    );
    addTearDown(client.clearAccountState);

    final goal = await client.startScene(
      threadId: _threadId,
      scene: testScenes.first,
      clientOperationId: 'scene_unicode',
    );

    expect(
      requests.map((request) => '${request.method} ${request.path}'),
      <String>[
        'GET /v1/goals',
        'POST /v1/goals',
        'PUT /v1/agent-threads/$_threadId/active-goal',
      ],
    );
    expect(requests[1].authorization, 'Bearer sess_account-a');
    expect(
      requests[1].bodyBytes,
      utf8.encode(jsonEncode(<String, Object?>{'title': '英文自我介绍'})),
    );
    expect(goal.id, _goalId);
    expect(goal.title, '英文自我介绍');
  });

  test('catalog activation creates a fresh Goal with the same title', () async {
    final scene = testScene(
      id: 'catalog-scene-new',
      name: 'Technical interview',
    );
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {
          'goals': [_goalJson(id: _goalId, title: scene.name)],
        }),
      ),
      _Step(
        method: 'POST',
        path: '/v1/goals',
        response: _jsonResponse(
          HttpStatus.created,
          _goalJson(id: _goalBId, title: scene.name),
        ),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        verify: (call) => expect(jsonDecode(call.body!), {'goal_id': _goalBId}),
        response: _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalBId)),
      ),
    ]);
    final client = _client(transport: transport);

    final goal = await client.startScene(
      threadId: _threadId,
      scene: scene,
      clientOperationId: 'scene_select',
    );

    expect(goal.id, _goalBId);
    transport.expectDone();
  });

  test('existing Goal selection does not create a duplicate', () async {
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals/$_goalId',
        response: _jsonResponse(
          HttpStatus.ok,
          _goalJson(id: _goalId, title: 'Saved interview'),
        ),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        response: _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalId)),
      ),
    ]);
    final client = _client(transport: transport);

    final goal = await client.selectExistingGoal(
      threadId: _threadId,
      goalId: _goalId,
    );

    expect(goal.title, 'Saved interview');
    expect(transport.calls.where((call) => call.method == 'POST'), isEmpty);
    transport.expectDone();
  });

  test('exactly one new Goal recovers an ambiguous create', () async {
    final scene = testScenes.first;
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {
          'goals': [_goalJson(id: _goalId, title: scene.name)],
        }),
      ),
      const _Step(
        method: 'POST',
        path: '/v1/goals',
        error: SocketException('response lost'),
      ),
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {
          'goals': [
            _goalJson(id: _goalId, title: scene.name),
            _goalJson(id: _goalBId, title: scene.name),
          ],
        }),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        response: _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalBId)),
      ),
    ]);
    final client = _client(transport: transport);
    const operationId = 'scene_ambiguous';

    await expectLater(
      client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      ),
      throwsA(
        isA<GoalClientException>().having(
          (error) => error.kind,
          'kind',
          GoalClientFailureKind.network,
        ),
      ),
    );
    final goal = await client.startScene(
      threadId: _threadId,
      scene: scene,
      clientOperationId: operationId,
    );

    expect(goal.id, _goalBId);
    expect(
      transport.calls.where(
        (call) => call.method == 'POST' && call.path == '/v1/goals',
      ),
      hasLength(1),
    );
    transport.expectDone();
  });

  test('malformed create response recovers its committed Goal', () async {
    final scene = testScenes.first;
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {'goals': <Object?>[]}),
      ),
      _Step(
        method: 'POST',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.created, <String, Object?>{}),
      ),
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {
          'goals': [_goalJson(id: _goalBId, title: scene.name)],
        }),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        response: _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalBId)),
      ),
    ]);
    final client = _client(transport: transport);
    const operationId = 'scene_malformed_201';

    await expectLater(
      client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      ),
      throwsA(
        isA<GoalClientException>().having(
          (error) => error.kind,
          'kind',
          GoalClientFailureKind.invalidResponse,
        ),
      ),
    );
    final goal = await client.startScene(
      threadId: _threadId,
      scene: scene,
      clientOperationId: operationId,
    );

    expect(goal.id, _goalBId);
    transport.expectDone();
  });

  test('a restarted client never guesses an earlier ambiguous Goal', () async {
    final scene = testScenes.first;
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {'goals': <Object?>[]}),
      ),
      _Step(
        method: 'POST',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.created, <String, Object?>{}),
      ),
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {
          'goals': [_goalJson(id: _goalBId, title: scene.name)],
        }),
      ),
      _Step(
        method: 'POST',
        path: '/v1/goals',
        response: _jsonResponse(
          HttpStatus.created,
          _goalJson(id: _goalCId, title: scene.name),
        ),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        response: _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalCId)),
      ),
    ]);
    const operationId = 'scene_restart_retry';
    final firstClient = _client(transport: transport);

    await expectLater(
      firstClient.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      ),
      throwsA(isA<GoalClientException>()),
    );
    final restarted = _client(transport: transport);
    final goal = await restarted.startScene(
      threadId: _threadId,
      scene: scene,
      clientOperationId: operationId,
    );

    expect(goal.id, _goalCId);
    expect(
      transport.calls.where(
        (call) => call.method == 'POST' && call.path == '/v1/goals',
      ),
      hasLength(2),
    );
    transport.expectDone();
  });

  test('multiple recovery candidates fail with a Goal conflict', () async {
    final scene = testScenes.first;
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {'goals': <Object?>[]}),
      ),
      const _Step(
        method: 'POST',
        path: '/v1/goals',
        error: SocketException('response lost'),
      ),
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {
          'goals': [
            _goalJson(id: _goalBId, title: scene.name),
            _goalJson(id: _goalCId, title: scene.name),
          ],
        }),
      ),
    ]);
    final client = _client(transport: transport);
    const operationId = 'scene_conflict';
    await expectLater(
      client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      ),
      throwsA(isA<GoalClientException>()),
    );

    await expectLater(
      client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      ),
      throwsA(
        isA<GoalClientException>()
            .having(
              (error) => error.kind,
              'kind',
              GoalClientFailureKind.conflict,
            )
            .having(
              (error) => error.errorCode,
              'errorCode',
              'resource_conflict',
            ),
      ),
    );
    transport.expectDone();
  });

  test('link retry reuses the already created Goal', () async {
    final scene = testScenes.first;
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/goals',
        response: _jsonResponse(HttpStatus.ok, {'goals': <Object?>[]}),
      ),
      _Step(
        method: 'POST',
        path: '/v1/goals',
        response: _jsonResponse(
          HttpStatus.created,
          _goalJson(id: _goalBId, title: scene.name),
        ),
      ),
      const _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        error: SocketException('link response lost'),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-goal',
        response: _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalBId)),
      ),
    ]);
    final client = _client(transport: transport);
    const operationId = 'scene_link_retry';
    await expectLater(
      client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      ),
      throwsA(isA<GoalClientException>()),
    );

    final goal = await client.startScene(
      threadId: _threadId,
      scene: scene,
      clientOperationId: operationId,
    );

    expect(goal.id, _goalBId);
    expect(
      transport.calls.where(
        (call) => call.method == 'POST' && call.path == '/v1/goals',
      ),
      hasLength(1),
    );
    transport.expectDone();
  });

  test(
    'account cleanup fences in-flight activation and clears recovery',
    () async {
      final transport = _ControlledTransport();
      final initialList = transport.enqueue();
      final staleCreate = transport.enqueue();
      var credential = const AuthSessionCredential(
        sessionToken: 'sess_account-a',
        generation: 1,
      );
      final client = _client(
        transport: transport,
        credentialProvider: () => credential,
      );
      final scene = testScenes.first;
      const operationId = 'scene_logout_race';

      final staleStart = client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      );
      await transport.waitForCalls(1);
      initialList.complete(
        _jsonResponse(HttpStatus.ok, {'goals': <Object?>[]}),
      );
      await transport.waitForCalls(2);

      final cleanup = client.clearAccountState();
      staleCreate.complete(
        _jsonResponse(
          HttpStatus.created,
          _goalJson(id: _goalId, title: scene.name),
        ),
      );
      await expectLater(
        staleStart,
        throwsA(
          isA<GoalClientException>().having(
            (error) => error.kind,
            'kind',
            GoalClientFailureKind.superseded,
          ),
        ),
      );
      await cleanup;

      credential = const AuthSessionCredential(
        sessionToken: 'sess_account-b',
        generation: 2,
      );
      final freshList = transport.enqueue();
      final freshCreate = transport.enqueue();
      final freshLink = transport.enqueue();
      final freshStart = client.startScene(
        threadId: _threadId,
        scene: scene,
        clientOperationId: operationId,
      );
      await transport.waitForCalls(3);
      freshList.complete(
        _jsonResponse(HttpStatus.ok, {
          'goals': [_goalJson(id: _goalId, title: scene.name)],
        }),
      );
      await transport.waitForCalls(4);
      freshCreate.complete(
        _jsonResponse(
          HttpStatus.created,
          _goalJson(id: _goalBId, title: scene.name),
        ),
      );
      await transport.waitForCalls(5);
      freshLink.complete(
        _jsonResponse(HttpStatus.ok, _goalLinkJson(goalId: _goalBId)),
      );

      final goal = await freshStart;
      expect(goal.id, _goalBId);
      expect(
        transport.calls.last.headers[HttpHeaders.authorizationHeader],
        'Bearer sess_account-b',
      );
    },
  );
}

WireGoalClient _client({
  Uri? baseUri,
  IdentityHttpTransport? transport,
  AuthSessionCredentialProvider? credentialProvider,
}) {
  return WireGoalClient(
    baseUri: baseUri ?? Uri.parse('https://api.speak-up.test'),
    credentialProvider:
        credentialProvider ??
        () => const AuthSessionCredential(
          sessionToken: 'sess_account-a',
          generation: 1,
        ),
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

final class _Step {
  const _Step({
    required this.method,
    required this.path,
    this.response,
    this.error,
    this.verify,
  }) : assert((response == null) != (error == null));

  final String method;
  final String path;
  final IdentityHttpResponse? response;
  final Object? error;
  final void Function(_Call call)? verify;
}

final class _Call {
  const _Call({
    required this.method,
    required this.path,
    required this.headers,
    required this.body,
  });

  final String method;
  final String path;
  final Map<String, String> headers;
  final String? body;
}

final class _ScriptedTransport implements IdentityHttpTransport {
  _ScriptedTransport(Iterable<_Step> steps) : _steps = Queue<_Step>.of(steps);

  final Queue<_Step> _steps;
  final List<_Call> calls = <_Call>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    if (_steps.isEmpty) {
      throw StateError('Unexpected Goal HTTP request.');
    }
    final step = _steps.removeFirst();
    final call = _Call(
      method: method,
      path: uri.path,
      headers: Map<String, String>.of(headers),
      body: body,
    );
    calls.add(call);
    expect(method, step.method);
    expect(uri.path, step.path);
    step.verify?.call(call);
    if (step.error case final error?) {
      throw error;
    }
    return step.response!;
  }

  void expectDone() => expect(_steps, isEmpty);
}

final class _ControlledTransport implements IdentityHttpTransport {
  final Queue<Completer<IdentityHttpResponse>> _responses =
      Queue<Completer<IdentityHttpResponse>>();
  final List<_Call> calls = <_Call>[];

  Completer<IdentityHttpResponse> enqueue() {
    final completer = Completer<IdentityHttpResponse>();
    _responses.add(completer);
    return completer;
  }

  Future<void> waitForCalls(int count) async {
    while (calls.length < count) {
      await Future<void>.delayed(Duration.zero);
    }
  }

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) {
    if (_responses.isEmpty) {
      throw StateError('No controlled Goal response was queued.');
    }
    calls.add(
      _Call(
        method: method,
        path: uri.path,
        headers: Map<String, String>.of(headers),
        body: body,
      ),
    );
    return _responses.removeFirst().future;
  }
}

final class _LoopbackRequest {
  const _LoopbackRequest({
    required this.method,
    required this.path,
    required this.authorization,
    required this.bodyBytes,
  });

  final String method;
  final String path;
  final String authorization;
  final List<int> bodyBytes;
}

Map<String, Object?> _goalJson({
  required String id,
  required String title,
  String status = 'active',
}) => <String, Object?>{
  'goal_id': id,
  'title': title,
  'status': status,
  'version': 1,
  'created_at': _createdAt,
  'updated_at': _updatedAt,
};

Map<String, Object?> _goalLinkJson({required String goalId}) =>
    <String, Object?>{
      'thread_id': _threadId,
      'goal_id': goalId,
      'active': true,
      'linked_at': _createdAt,
      'updated_at': _updatedAt,
    };

Map<String, Object?> _errorJson() => <String, Object?>{
  'error': <String, Object?>{
    'code': 'resource_not_found',
    'message': 'Resource not found.',
    'retryable': false,
    'correlation_id': 'corr_goal_test',
  },
};

IdentityHttpResponse _jsonResponse(int statusCode, Object? body) =>
    IdentityHttpResponse(statusCode: statusCode, body: jsonEncode(body));

const _threadId = '10000000-0000-4000-8000-000000000001';
const _goalId = '40000000-0000-4000-8000-000000000001';
const _goalBId = '40000000-0000-4000-8000-000000000002';
const _goalCId = '40000000-0000-4000-8000-000000000003';
const _createdAt = '2026-07-25T09:00:00Z';
const _updatedAt = '2026-07-25T09:00:03Z';
