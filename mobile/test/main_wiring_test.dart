import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/main.dart' as app;

void main() {
  test('production composition uses the wire client and iOS Keychain', () {
    final controller = app.createProductionAuthController(
      baseUri: Uri.parse('http://127.0.0.1:8080'),
    );

    expect(controller.identityClient, isA<WireIdentityClient>());
    expect(controller.sessionStore, isA<IosKeychainSessionStore>());
  });

  testWidgets('global AuthGate enters Agent home and removes private routes', (
    tester,
  ) async {
    final controller = AuthController(
      identityClient: _IdentityClient(),
      sessionStore: _SessionStore(token: 'sess_test-token'),
    );
    await tester.pumpWidget(SpeakUpApp(authController: controller));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
    final shellContext = tester.element(
      find.byKey(const Key('agent-home-page')),
    );
    Navigator.of(shellContext).pushNamed(AppRoutes.review);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-route-back-button')), findsOneWidget);

    await controller.logout();
    await tester.pumpAndSettle();

    expect(find.text('Welcome back'), findsOneWidget);
    expect(find.byKey(const Key('agent-home-page')), findsNothing);
    expect(find.byKey(const Key('review-route-back-button')), findsNothing);
  });
}

final class _SessionStore implements SessionStore {
  _SessionStore({this.token});

  String? token;

  @override
  Future<void> deleteToken() async => token = null;

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String value) async => token = value;
}

final class _IdentityClient implements IdentityClient {
  static const _user = User(id: 'user-1', email: 'learner@example.com');

  @override
  Future<User> currentUser({required String sessionToken}) async => _user;

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async {
    return LoginResult(
      user: _user,
      sessionToken: 'sess_test-token',
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
    return _user;
  }
}
