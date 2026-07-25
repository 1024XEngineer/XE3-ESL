import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_gate.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  const user = User(id: 'user-1', email: 'learner@example.com');

  for (final scalarCount in [14, 15, 128, 129]) {
    testWidgets(
      'password validation counts $scalarCount Unicode scalars and preserves input',
      (tester) async {
        final client = GateIdentityClient(user: user);
        final controller = AuthController(
          identityClient: client,
          sessionStore: MemorySessionStore(),
        );
        final password = scalarPassword(scalarCount);
        expect(password.runes.length, scalarCount);

        await tester.pumpWidget(testApp(controller));
        await tester.pumpAndSettle();
        await tester.enterText(
          find.widgetWithText(TextFormField, '邮箱'),
          'learner@example.com',
        );
        await tester.enterText(
          find.widgetWithText(TextFormField, '密码'),
          password,
        );
        await tester.tap(find.text('登录'));
        await tester.pumpAndSettle();

        if (scalarCount >= 15 && scalarCount <= 128) {
          expect(client.lastLoginPassword, password);
          expect(client.lastLoginPassword!.codeUnits, password.codeUnits);
          expect(find.text('密码长度需为 15–128 个字符。'), findsNothing);
        } else {
          expect(client.lastLoginPassword, isNull);
          expect(find.text('密码长度需为 15–128 个字符。'), findsOneWidget);
        }
      },
    );
  }

  testWidgets('shows loading while the stored session is read', (tester) async {
    final store = ControlledSessionStore();
    final controller = AuthController(
      identityClient: GateIdentityClient(user: user),
      sessionStore: store,
    );

    await tester.pumpWidget(testApp(controller));

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    store.readCompleter.complete(null);
    await tester.pumpAndSettle();
    expect(find.text('欢迎回来'), findsOneWidget);
  });

  testWidgets('switches between login and registration', (tester) async {
    final controller = AuthController(
      identityClient: GateIdentityClient(user: user),
      sessionStore: MemorySessionStore(),
    );

    await tester.pumpWidget(testApp(controller));
    await tester.pumpAndSettle();
    await tester.tap(find.text('创建账号'));
    await tester.pump();

    expect(find.text('创建账号'), findsNWidgets(2));
    expect(find.text('返回登录'), findsOneWidget);

    await tester.tap(find.text('返回登录'));
    await tester.pump();
    expect(find.text('欢迎回来'), findsOneWidget);
  });

  testWidgets('registration returns to login and does not authenticate', (
    tester,
  ) async {
    final store = MemorySessionStore();
    final controller = AuthController(
      identityClient: GateIdentityClient(user: user),
      sessionStore: store,
    );
    await tester.pumpWidget(testApp(controller));
    await tester.pumpAndSettle();
    await tester.tap(find.text('创建账号'));
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextFormField, '邮箱'),
      'learner@example.com',
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, '密码'),
      'a sufficiently long password',
    );
    await tester.tap(find.widgetWithText(FilledButton, '创建账号'));
    await tester.pumpAndSettle();

    expect(find.text('欢迎回来'), findsOneWidget);
    expect(find.text('账号创建成功，请登录后继续。'), findsOneWidget);
    expect(store.token, isNull);
  });

  testWidgets('shows retry without deleting token after a network error', (
    tester,
  ) async {
    final client = GateIdentityClient(
      user: user,
      currentUserError: const IdentityClientException(
        kind: IdentityFailureKind.network,
        retryable: true,
      ),
    );
    final store = MemorySessionStore(token: 'sess_stored-token');
    final controller = AuthController(
      identityClient: client,
      sessionStore: store,
    );
    await tester.pumpWidget(testApp(controller));
    await tester.pumpAndSettle();

    expect(find.text('需要网络连接'), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);
    expect(store.token, 'sess_stored-token');

    client.currentUserError = null;
    await tester.tap(find.text('重试'));
    await tester.pumpAndSettle();
    expect(find.text('Agent home for learner@example.com'), findsOneWidget);
  });

  testWidgets('authenticated builder receives restored user', (tester) async {
    final controller = AuthController(
      identityClient: GateIdentityClient(user: user),
      sessionStore: MemorySessionStore(token: 'sess_stored-token'),
    );

    await tester.pumpWidget(testApp(controller));
    await tester.pumpAndSettle();

    expect(find.text('Agent home for learner@example.com'), findsOneWidget);
    expect(find.text('欢迎回来'), findsNothing);
  });

  testWidgets('login remains usable at 320pt with 3x text and keyboard', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 3;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

    final controller = AuthController(
      identityClient: GateIdentityClient(user: user),
      sessionStore: MemorySessionStore(),
    );
    await tester.pumpWidget(testApp(controller));
    await tester.pumpAndSettle();

    final emailField = find.widgetWithText(TextFormField, '邮箱');
    await tester.ensureVisible(emailField);
    await tester.showKeyboard(emailField);
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    await tester.pumpAndSettle();
    expect(emailField.hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);

    tester.view.viewInsets = FakeViewPadding.zero;
    tester.testTextInput.hide();
    await tester.pumpAndSettle();
    final createAccount = find.widgetWithText(TextButton, '创建账号');
    await tester.ensureVisible(createAccount);
    await tester.pumpAndSettle();
    expect(createAccount.hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}

Widget testApp(AuthController controller) {
  return MaterialApp(
    home: AuthGate(
      controller: controller,
      authenticatedBuilder: (context, user) =>
          Scaffold(body: Text('Agent home for ${user.email}')),
    ),
  );
}

final class MemorySessionStore implements SessionStore {
  MemorySessionStore({this.token});

  String? token;

  @override
  Future<void> deleteToken() async => token = null;

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String value) async => token = value;
}

final class ControlledSessionStore implements SessionStore {
  final readCompleter = Completer<String?>();

  @override
  Future<void> deleteToken() async {}

  @override
  Future<String?> readToken() => readCompleter.future;

  @override
  Future<void> writeToken(String token) async {}
}

final class GateIdentityClient implements IdentityClient {
  GateIdentityClient({required this.user, this.currentUserError});

  final User user;
  IdentityClientException? currentUserError;
  String? lastLoginPassword;

  @override
  Future<User> currentUser({required String sessionToken}) async {
    final error = currentUserError;
    if (error != null) {
      throw error;
    }
    return user;
  }

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    lastLoginPassword = password;
    return LoginResult(
      user: user,
      sessionToken: 'sess_new-session-token',
      expiresAt: DateTime.utc(2030),
    );
  }

  @override
  Future<void> logout({required String sessionToken}) async {}

  @override
  Future<User> register({
    required String email,
    required String password,
  }) async {
    return user;
  }
}

String scalarPassword(int scalarCount) {
  const boundary = ' e\u0301';
  const suffix = ' ';
  return '$boundary${List.filled(scalarCount - 4, '😀').join()}$suffix';
}
