import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/main.dart' as production;

void main() {
  test('iOS allows local development traffic without a global ATS bypass', () {
    final plist = File('ios/Runner/Info.plist').readAsStringSync();

    expect(
      plist,
      matches(
        RegExp(
          r'<key>NSAppTransportSecurity</key>\s*'
          r'<dict>\s*'
          r'<key>NSAllowsLocalNetworking</key>\s*'
          r'<true\s*/>\s*'
          r'</dict>',
        ),
      ),
    );
    expect(plist, isNot(contains('<key>NSAllowsArbitraryLoads</key>')));
  });

  testWidgets(
    'production composition restores Auth and Agent data without Fake fallback',
    (tester) async {
      final identityTransport = _Transport([
        _Response(
          method: 'GET',
          path: '/v1/me',
          statusCode: HttpStatus.ok,
          body: {'user_id': 'user_15919508513', 'email': '15919508513@163.com'},
        ),
      ]);
      final agentTransport = _Transport([
        _Response(
          method: 'GET',
          path: '/v1/agent-threads',
          statusCode: HttpStatus.ok,
          body: {
            'threads': [
              {
                'thread_id': _threadId,
                'created_at': _timestamp,
                'updated_at': _timestamp,
              },
            ],
          },
        ),
        const _Response(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages',
          statusCode: HttpStatus.ok,
          body: {'messages': <Object?>[]},
        ),
      ]);
      final dependencies = production.createProductionAppDependencies(
        baseUri: Uri.parse('https://api.speak-up.test'),
        identityTransport: identityTransport,
        agentTransport: agentTransport,
        sessionStore: _MemorySessionStore('sess_main-wiring'),
      );

      expect(dependencies.agentController.client, isA<WireAgentClient>());
      expect(
        dependencies.agentController.client,
        isNot(isA<FakeAgentClient>()),
      );

      await tester.pumpWidget(
        SpeakUpApp(
          authController: dependencies.authController,
          agentController: dependencies.agentController,
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(dependencies.agentController.threadId, _threadId);
      expect(dependencies.authController.state, isA<AuthAuthenticated>());
      expect(
        identityTransport.calls.single.authorization,
        'Bearer sess_main-wiring',
      );
      expect(
        agentTransport.calls.every(
          (call) => call.authorization == 'Bearer sess_main-wiring',
        ),
        isTrue,
      );

      await tester.tap(find.byKey(const Key('primary-tab-scenes')));
      await tester.pumpAndSettle();
      expect(find.text('服务端场景与语音契约尚未开放，当前仅提供 Agent 文本对话。'), findsOneWidget);
      final scene = tester.widget<InkWell>(
        find.byKey(const Key('scene-self-introduction')),
      );
      expect(scene.onTap, isNull);

      identityTransport.expectDone();
      agentTransport.expectDone();
    },
  );
}

final class _MemorySessionStore implements SessionStore {
  _MemorySessionStore(this.token);

  String? token;

  @override
  Future<void> deleteToken() async {
    token = null;
  }

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String token) async {
    this.token = token;
  }
}

final class _Response {
  const _Response({
    required this.method,
    required this.path,
    required this.statusCode,
    required this.body,
  });

  final String method;
  final String path;
  final int statusCode;
  final Object? body;
}

final class _Call {
  const _Call({required this.authorization});

  final String? authorization;
}

final class _Transport implements IdentityHttpTransport {
  _Transport(Iterable<_Response> responses)
    : _responses = Queue<_Response>.of(responses);

  final Queue<_Response> _responses;
  final List<_Call> calls = <_Call>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    if (_responses.isEmpty) {
      throw StateError('Unexpected production wiring request.');
    }
    final response = _responses.removeFirst();
    expect(method, response.method);
    expect(uri.path, response.path);
    calls.add(_Call(authorization: headers[HttpHeaders.authorizationHeader]));
    return IdentityHttpResponse(
      statusCode: response.statusCode,
      body: jsonEncode(response.body),
    );
  }

  void expectDone() {
    expect(_responses, isEmpty);
  }
}

const _threadId = '10000000-0000-4000-8000-000000000088';
const _timestamp = '2026-07-25T09:00:00Z';
