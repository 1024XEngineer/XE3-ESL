import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:speakup/main.dart' as app;

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('real iOS identity and Qianwen Agent path', (tester) async {
    const email = String.fromEnvironment('SPEAKUP_E2E_EMAIL');
    const password = String.fromEnvironment('SPEAKUP_E2E_PASSWORD');
    const captureHoldMs = int.fromEnvironment('SPEAKUP_E2E_CAPTURE_HOLD_MS');
    if (email.isEmpty || password.runes.length < 15) {
      fail('A disposable E2E account with a valid password is required.');
    }

    app.main();
    await _waitUntil(
      tester,
      () =>
          find.text('Welcome back').evaluate().isNotEmpty ||
          find.text('Connection needed').evaluate().isNotEmpty ||
          find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty,
      const Duration(seconds: 15),
    );
    if (find.text('Connection needed').evaluate().isNotEmpty) {
      await tester.tap(find.text('Try again'));
      await _waitUntil(
        tester,
        () =>
            find.text('Welcome back').evaluate().isNotEmpty ||
            find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty,
        const Duration(seconds: 15),
      );
    }

    if (find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty) {
      await _verifySignedInAccount(tester, email);
    } else {
      await tester.tap(find.text('Create an account'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextFormField).at(0), email);
      await tester.enterText(find.byType(TextFormField).at(1), password);
      await tester.tap(find.text('Create account'));
      await _waitUntil(
        tester,
        () =>
            find
                .text('Account created. Sign in to continue.')
                .evaluate()
                .isNotEmpty ||
            find
                .text('An account cannot be created with these details.')
                .evaluate()
                .isNotEmpty,
        const Duration(seconds: 15),
      );
      if (find
          .text('An account cannot be created with these details.')
          .evaluate()
          .isNotEmpty) {
        await tester.tap(find.text('Back to sign in'));
        await _waitUntil(
          tester,
          () => find.text('Welcome back').evaluate().isNotEmpty,
          const Duration(seconds: 5),
        );
      }

      await tester.enterText(find.byType(TextFormField).at(0), email);
      await tester.enterText(find.byType(TextFormField).at(1), password);
      await tester.tap(find.text('Sign in'));
      await _waitUntil(
        tester,
        () =>
            find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty &&
            _composerIsReady(tester),
        const Duration(seconds: 20),
      );
    }

    const prompt =
        'Reply with one short English sentence. Start it with a marker made by '
        'joining these three fragments with underscores: SPEAKUP, E2E, OK.';
    final marker = find.textContaining('SPEAKUP_E2E_OK');
    final previousMarkerCount = marker.evaluate().length;
    await tester.enterText(
      find.byKey(const Key('agent-composer-field')),
      prompt,
    );
    await _waitUntil(
      tester,
      () => _sendButtonIsEnabled(tester),
      const Duration(seconds: 5),
    );
    await tester.tap(find.byKey(const Key('agent-send-button')));
    await _waitUntil(
      tester,
      () => marker.evaluate().length > previousMarkerCount,
      const Duration(seconds: 90),
    );

    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pumpAndSettle();
    final screenshot = await binding.takeScreenshot('ios-real-qianwen-e2e');
    expect(screenshot, isNotEmpty);
    debugPrint('SPEAKUP_E2E_CAPTURE_READY=true');
    if (captureHoldMs > 0) {
      await tester.runAsync(
        () => Future<void>.delayed(Duration(milliseconds: captureHoldMs)),
      );
    }
    await _signOut(tester);
  });
}

Future<void> _verifySignedInAccount(
  WidgetTester tester,
  String expectedEmail,
) async {
  await tester.tap(find.byKey(const Key('primary-tab-profile')));
  await _waitUntil(
    tester,
    () => find.byKey(const Key('profile-page')).evaluate().isNotEmpty,
    const Duration(seconds: 5),
  );
  if (find.text(expectedEmail).evaluate().isEmpty) {
    fail('The restored E2E Session belongs to a different account.');
  }
  await tester.tap(find.byKey(const Key('primary-tab-agent')));
  await _waitUntil(
    tester,
    () =>
        find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty &&
        _composerIsReady(tester),
    const Duration(seconds: 20),
  );
}

Future<void> _signOut(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('primary-tab-profile')));
  await _waitUntil(
    tester,
    () => find.byKey(const Key('profile-page')).evaluate().isNotEmpty,
    const Duration(seconds: 5),
  );
  await tester.tap(find.byKey(const Key('profile-logout-button')));
  await _waitUntil(
    tester,
    () => find.text('Welcome back').evaluate().isNotEmpty,
    const Duration(seconds: 15),
  );
}

bool _composerIsReady(WidgetTester tester) {
  final composer = find.byKey(const Key('agent-composer-field'));
  if (composer.evaluate().length != 1 ||
      find.byKey(const Key('agent-operation-progress')).evaluate().isNotEmpty) {
    return false;
  }
  return tester.widget<TextField>(composer).enabled == true;
}

bool _sendButtonIsEnabled(WidgetTester tester) {
  final sendButton = find.byKey(const Key('agent-send-button'));
  return sendButton.evaluate().length == 1 &&
      tester.widget<IconButton>(sendButton).onPressed != null;
}

Future<void> _waitUntil(
  WidgetTester tester,
  bool Function() condition,
  Duration timeout,
) async {
  final deadline = DateTime.now().add(timeout);
  while (!condition() && DateTime.now().isBefore(deadline)) {
    await tester.pump(const Duration(milliseconds: 250));
  }
  if (!condition()) {
    fail('Timed out waiting for the expected E2E state.');
  }
}
