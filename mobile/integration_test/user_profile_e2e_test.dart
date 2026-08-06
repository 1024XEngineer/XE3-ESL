import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/main.dart' as app;

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('real registration, profile edit, logout and restore', (
    tester,
  ) async {
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    final email =
        'profile149-${DateTime.now().microsecondsSinceEpoch}@example.com';
    const password = 'profile e2e password 149';
    const initialName = '小林 E2E';
    const updatedName = '林同学 E2E';
    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      sessionStore: _MemorySessionStore(),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        conversationController: dependencies.conversationController,
        composerController: dependencies.composerController,
        messageAudioController: dependencies.messageAudioController,
        practiceController: dependencies.practiceController,
        preparationController: dependencies.preparationController,
        ieltsPreparationController: dependencies.ieltsPreparationController,
        jobPreparationController: dependencies.jobPreparationController,
        preparationLaunchController: dependencies.preparationLaunchController,
        reviewHistoryController: dependencies.reviewHistoryController,
      ),
    );

    await _waitUntil(tester, () => find.text('欢迎回来').evaluate().isNotEmpty);
    await tester.tap(find.widgetWithText(TextButton, '创建账号'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('register-display-name')),
      initialName,
    );
    await tester.enterText(find.widgetWithText(TextFormField, '邮箱'), email);
    await tester.enterText(find.widgetWithText(TextFormField, '密码'), password);
    await tester.tap(find.widgetWithText(FilledButton, '创建账号'));
    await _waitUntil(
      tester,
      () => find.text('账号创建成功，请登录后继续。').evaluate().isNotEmpty,
    );

    await _signIn(tester, email: email, password: password);
    await _waitUntil(
      tester,
      () => find.text('你好，$initialName').evaluate().isNotEmpty,
    );
    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    expect(find.text(initialName), findsOneWidget);
    expect(find.text(email), findsOneWidget);

    await tester.tap(find.byKey(const Key('profile-edit-display-name')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('profile-display-name-input')),
      updatedName,
    );
    await tester.tap(find.byKey(const Key('profile-save-display-name')));
    await _waitUntil(
      tester,
      () => find.text(updatedName).evaluate().isNotEmpty,
    );
    final screenshot = await binding.takeScreenshot('user-profile-real-e2e');
    expect(screenshot, isNotEmpty);

    await tester.tap(find.byKey(const Key('profile-logout-button')));
    await _waitUntil(tester, () => find.text('欢迎回来').evaluate().isNotEmpty);
    await _signIn(tester, email: email, password: password);
    await _waitUntil(
      tester,
      () => find.text('你好，$updatedName').evaluate().isNotEmpty,
    );
    await dependencies.authController.logout();
  });
}

Future<void> _signIn(
  WidgetTester tester, {
  required String email,
  required String password,
}) async {
  await tester.enterText(find.widgetWithText(TextFormField, '邮箱'), email);
  await tester.enterText(find.widgetWithText(TextFormField, '密码'), password);
  await tester.tap(find.widgetWithText(FilledButton, '登录'));
  await tester.pump();
}

Future<void> _waitUntil(
  WidgetTester tester,
  bool Function() condition, [
  Duration timeout = const Duration(seconds: 20),
]) async {
  final deadline = DateTime.now().add(timeout);
  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out waiting for the expected user-profile state.');
    }
    await tester.pump(const Duration(milliseconds: 100));
  }
  await tester.pumpAndSettle();
}

final class _MemorySessionStore implements SessionStore {
  String? _token;

  @override
  Future<void> deleteToken() async => _token = null;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String token) async => _token = token;
}
