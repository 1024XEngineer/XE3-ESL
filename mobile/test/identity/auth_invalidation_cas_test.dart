import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/network/authenticated_web_socket.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  const userA = User(id: 'user-a', email: 'a@example.com');
  const userB = User(id: 'user-b', email: 'b@example.com');
  final loginB = LoginResult(
    user: userB,
    sessionToken: 'sess_account-b',
    expiresAt: DateTime.utc(2030),
  );

  test('A request late 401 cannot invalidate B login', () async {
    final client = _IdentityClient(
      currentUserResult: userA,
      loginResult: loginB,
    );
    final store = _SessionStore('sess_account-a');
    final controller = AuthController(
      identityClient: client,
      sessionStore: store,
    );
    await controller.initialize();

    final transport = _ControlledHttpTransport();
    final authenticatedTransport = SessionAuthenticatedHttpTransport(
      transport: transport,
      credentialProvider: () => controller.currentCredential,
      invalidateSession: controller.invalidateSession,
      trustedBaseUri: Uri.parse('https://api.speak-up.test'),
    );
    final request = authenticatedTransport.send(
      method: 'GET',
      uri: Uri.parse('https://api.speak-up.test/v1/private-resource'),
      headers: const {},
    );
    await transport.started.future;
    expect(
      transport.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_account-a',
    );

    await controller.logout();
    controller.showLogin();
    await controller.login(email: userB.email, password: 'long password value');
    transport.response.complete(
      const IdentityHttpResponse(statusCode: 401, body: ''),
    );
    await request;

    expect(controller.state, isA<AuthAuthenticated>());
    expect(controller.currentCredential?.sessionToken, 'sess_account-b');
    expect(store.token, 'sess_account-b');
  });

  test('A WebSocket late 4401 cannot invalidate B login', () async {
    final client = _IdentityClient(
      currentUserResult: userA,
      loginResult: loginB,
    );
    final store = _SessionStore('sess_account-a');
    final controller = AuthController(
      identityClient: client,
      sessionStore: store,
    );
    await controller.initialize();

    final connector = _WebSocketConnector();
    final authenticatedConnector = SessionAuthenticatedWebSocketConnector(
      connector: connector,
      credentialProvider: () => controller.currentCredential,
      invalidateSession: controller.invalidateSession,
      trustedBaseUri: Uri.parse('wss://api.speak-up.test'),
    );
    final connection = await authenticatedConnector.connect(
      uri: Uri.parse('wss://api.speak-up.test/events'),
    );
    expect(connector.tokens, ['sess_account-a']);

    await controller.logout();
    controller.showLogin();
    await controller.login(email: userB.email, password: 'long password value');
    await connection.handleDisconnect(
      closeCode: 4401,
      closeReason: 'session_invalid',
    );

    expect(controller.state, isA<AuthAuthenticated>());
    expect(controller.currentCredential?.sessionToken, 'sess_account-b');
    expect(store.token, 'sess_account-b');
  });

  test('A WebSocket late Upgrade 401 cannot invalidate B login', () async {
    final controller = AuthController(
      identityClient: _IdentityClient(
        currentUserResult: userA,
        loginResult: loginB,
      ),
      sessionStore: _SessionStore('sess_account-a'),
    );
    await controller.initialize();

    final connector = _ControlledWebSocketConnector();
    final authenticatedConnector = SessionAuthenticatedWebSocketConnector(
      connector: connector,
      credentialProvider: () => controller.currentCredential,
      invalidateSession: controller.invalidateSession,
      trustedBaseUri: Uri.parse('wss://api.speak-up.test'),
    );
    final connection = authenticatedConnector.connect(
      uri: Uri.parse('wss://api.speak-up.test/events'),
    );
    await connector.started.future;

    await controller.logout();
    controller.showLogin();
    await controller.login(email: userB.email, password: 'long password value');
    connector.result.completeError(
      const AuthenticatedWebSocketException(
        WebSocketFailureKind.authenticationInvalid,
      ),
    );
    await expectLater(
      connection,
      throwsA(isA<AuthenticatedWebSocketException>()),
    );

    expect(controller.state, isA<AuthAuthenticated>());
    expect(controller.currentCredential?.sessionToken, 'sess_account-b');
  });

  test('current HTTP 401 invalidates the captured session', () async {
    final controller = AuthController(
      identityClient: _IdentityClient(
        currentUserResult: userA,
        loginResult: loginB,
      ),
      sessionStore: _SessionStore('sess_account-a'),
    );
    await controller.initialize();
    final transport = _ControlledHttpTransport()
      ..response.complete(
        const IdentityHttpResponse(statusCode: 401, body: ''),
      );
    final authenticatedTransport = SessionAuthenticatedHttpTransport(
      transport: transport,
      credentialProvider: () => controller.currentCredential,
      invalidateSession: controller.invalidateSession,
      trustedBaseUri: Uri.parse('https://api.speak-up.test'),
    );

    await authenticatedTransport.send(
      method: 'GET',
      uri: Uri.parse('https://api.speak-up.test/v1/private-resource'),
      headers: const {},
    );

    expect(controller.state, isA<AuthSignedOut>());
    expect(controller.currentCredential, isNull);
  });

  test('current WebSocket 4401 invalidates the captured session', () async {
    final controller = AuthController(
      identityClient: _IdentityClient(
        currentUserResult: userA,
        loginResult: loginB,
      ),
      sessionStore: _SessionStore('sess_account-a'),
    );
    await controller.initialize();
    final authenticatedConnector = SessionAuthenticatedWebSocketConnector(
      connector: _WebSocketConnector(),
      credentialProvider: () => controller.currentCredential,
      invalidateSession: controller.invalidateSession,
      trustedBaseUri: Uri.parse('wss://api.speak-up.test'),
    );
    final connection = await authenticatedConnector.connect(
      uri: Uri.parse('wss://api.speak-up.test/events'),
    );

    await connection.handleDisconnect(
      closeCode: 4401,
      closeReason: 'session_invalid',
    );

    expect(controller.state, isA<AuthSignedOut>());
    expect(controller.currentCredential, isNull);
  });

  test('older generation is a no-op even if a token value is reused', () async {
    final reusedLogin = LoginResult(
      user: userB,
      sessionToken: 'sess_reused-token',
      expiresAt: DateTime.utc(2030),
    );
    final controller = AuthController(
      identityClient: _IdentityClient(
        currentUserResult: userA,
        loginResult: reusedLogin,
      ),
      sessionStore: _SessionStore('sess_reused-token'),
    );
    await controller.initialize();
    final oldCredential = controller.currentCredential!;

    await controller.logout();
    controller.showLogin();
    await controller.login(email: userB.email, password: 'long password value');
    await controller.invalidateSession(
      expectedSessionToken: oldCredential.sessionToken,
      expectedGeneration: oldCredential.generation,
    );

    expect(controller.state, isA<AuthAuthenticated>());
    expect(controller.currentCredential?.sessionToken, 'sess_reused-token');
    expect(
      controller.currentCredential?.generation,
      isNot(oldCredential.generation),
    );
    expect(
      oldCredential.toString(),
      isNot(contains(oldCredential.sessionToken)),
    );
  });
}

final class _ControlledHttpTransport implements IdentityHttpTransport {
  final started = Completer<void>();
  final response = Completer<IdentityHttpResponse>();
  Map<String, String> headers = const {};

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) {
    this.headers = headers;
    started.complete();
    return response.future;
  }
}

final class _WebSocketConnector implements AuthenticatedWebSocketConnector {
  final tokens = <String>[];

  @override
  Future<WebSocket> connect({
    required Uri uri,
    required String sessionToken,
  }) async {
    tokens.add(sessionToken);
    return _WebSocket();
  }
}

final class _ControlledWebSocketConnector
    implements AuthenticatedWebSocketConnector {
  final started = Completer<void>();
  final result = Completer<WebSocket>();

  @override
  Future<WebSocket> connect({required Uri uri, required String sessionToken}) {
    started.complete();
    return result.future;
  }
}

final class _WebSocket implements WebSocket {
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

final class _SessionStore implements SessionStore {
  _SessionStore(this.token);

  String? token;

  @override
  Future<void> deleteToken() async => token = null;

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String token) async => this.token = token;
}

final class _IdentityClient implements IdentityClient {
  _IdentityClient({required this.currentUserResult, required this.loginResult});

  final User currentUserResult;
  final LoginResult loginResult;

  @override
  Future<User> currentUser({required String sessionToken}) async {
    return currentUserResult;
  }

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    return loginResult;
  }

  @override
  Future<void> logout({required String sessionToken}) async {}

  @override
  Future<User> register({required String email, required String password}) {
    throw UnimplementedError();
  }
}
