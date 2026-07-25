import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';

void main() {
  testWidgets('starts on the Agent home with four glass navigation entries', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp());

    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
    expect(find.text('我能为你做什么？'), findsOneWidget);
    expect(find.byKey(const Key('quick-action-create-plan')), findsOneWidget);
    expect(
      find.byKey(const Key('quick-action-continue-practice')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('quick-action-recent-review')), findsOneWidget);
    expect(find.byKey(const Key('agent-composer-field')), findsOneWidget);

    for (final key in _primaryTabKeys) {
      expect(find.byKey(Key(key)), findsOneWidget);
    }

    final navigation = find.byKey(const Key('primary-navigation'));
    expect(navigation, findsOneWidget);
    expect(
      find.ancestor(of: navigation, matching: find.byType(BackdropFilter)),
      findsOneWidget,
    );
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    final navigationRect = tester.getRect(navigation);
    expect(navigationRect.top - composerRect.bottom, closeTo(10, 1));
    expect(navigationRect.left, closeTo(composerRect.left, 1));
    expect(navigationRect.right, closeTo(composerRect.right, 1));

    final semantics = tester.ensureSemantics();
    expect(
      tester.getSemantics(find.byKey(const Key('primary-tab-agent'))),
      isSemantics(
        label: 'SpeakUp',
        isButton: true,
        hasSelectedState: true,
        isSelected: true,
        hasTapAction: true,
      ),
    );
    semantics.dispose();
  });

  testWidgets('switches between every primary destination', (tester) async {
    await tester.pumpWidget(const SpeakUpApp());
    final semantics = tester.ensureSemantics();

    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-scenes',
      expectedPageKey: 'scenes-page',
    );
    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-review',
      expectedPageKey: 'review-page',
    );
    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-profile',
      expectedPageKey: 'profile-page',
    );
    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-agent',
      expectedPageKey: 'agent-home-page',
    );
    semantics.dispose();
  });

  testWidgets('keeps all four Agent actions above the composer on iPhone', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(402, 874);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(const SpeakUpApp());
    await tester.pumpAndSettle();

    const actionKeys = [
      'quick-action-create-plan',
      'quick-action-continue-practice',
      'quick-action-browse-scenes',
      'quick-action-recent-review',
    ];
    for (final key in actionKeys) {
      final action = find.byKey(Key(key));
      expect(action, findsOneWidget);
      expect(tester.getRect(action).bottom, lessThan(874));
    }

    final lastActionRect = tester.getRect(
      find.byKey(const Key('quick-action-recent-review')),
    );
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(composerRect.top - lastActionRect.bottom, greaterThanOrEqualTo(16));
  });

  testWidgets('conversation drawer contains no duplicate primary navigation', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp());

    await tester.tap(find.byKey(const Key('conversation-menu-button')));
    await tester.pumpAndSettle();

    final drawer = find.byType(Drawer);
    expect(drawer, findsOneWidget);
    expect(
      find.descendant(
        of: drawer,
        matching: find.byKey(const Key('new-conversation-button')),
      ),
      findsOneWidget,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('最近对话')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('场景')),
      findsNothing,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('复盘')),
      findsNothing,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('我的')),
      findsNothing,
    );
  });

  testWidgets('Agent actions reuse the existing feature entry points', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp());

    await _tapVisible(tester, 'quick-action-create-plan');
    expect(find.byKey(const Key('scenes-page')), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-agent')));
    await tester.pumpAndSettle();
    await _tapVisible(tester, 'quick-action-recent-review');
    expect(find.byKey(const Key('review-page')), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-agent')));
    await tester.pumpAndSettle();
    await _tapVisible(tester, 'quick-action-continue-practice');
    expect(find.byType(PracticePage), findsOneWidget);
  });

  testWidgets('keeps every formal feature route reachable', (tester) async {
    await tester.pumpWidget(const SpeakUpApp());

    await _expectNamedRoute<PreparationPage>(
      tester,
      AppRoutes.preparation,
      backButton: find.byKey(const Key('preparation-route-back-button')),
    );
    await _expectNamedRoute<PracticePage>(
      tester,
      AppRoutes.practice,
      backButton: find.byType(BackButton),
    );
    await _expectNamedRoute<ConversationPage>(
      tester,
      AppRoutes.conversation,
      backButton: find.byKey(const Key('conversation-route-back-button')),
    );
    await _expectNamedRoute<ReviewPage>(
      tester,
      AppRoutes.review,
      backButton: find.byKey(const Key('review-route-back-button')),
    );
  });

  testWidgets('keeps the named conversation route escapable on every tab', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp());

    final shellContext = tester.element(find.byType(SpeakUpShell));
    Navigator.of(shellContext).pushNamed(AppRoutes.conversation);
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('conversation-route-back-button')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('preparation-route-back-button')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('primary-tab-review')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-route-back-button')), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    final profileBackButton = find.byKey(
      const Key('profile-route-back-button'),
    );
    expect(profileBackButton, findsOneWidget);

    await tester.tap(profileBackButton);
    await tester.pumpAndSettle();

    expect(profileBackButton, findsNothing);
    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
    expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
    final rootShellContext = tester.element(find.byType(SpeakUpShell));
    expect(Navigator.of(rootShellContext).canPop(), isFalse);
  });

  testWidgets('stays usable on a narrow screen and with the keyboard open', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);

    await tester.pumpWidget(const SpeakUpApp());
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    final lastAction = find.byKey(const Key('quick-action-recent-review'));
    await tester.ensureVisible(lastAction);
    await tester.pumpAndSettle();
    final lastActionRect = tester.getRect(lastAction);
    final restingComposerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(
      restingComposerRect.top - lastActionRect.bottom,
      greaterThanOrEqualTo(16),
    );
    expect(lastAction.hitTestable(), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    await tester.tap(find.byKey(const Key('primary-tab-agent')));
    await tester.pumpAndSettle();
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    await tester.showKeyboard(find.byKey(const Key('agent-composer-field')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('agent-composer-field')), findsOneWidget);
    expect(find.byKey(const Key('primary-navigation')), findsNothing);
    final keyboardTop =
        tester.view.physicalSize.height / tester.view.devicePixelRatio - 240;
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(keyboardTop - composerRect.bottom, closeTo(10, 1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('supports larger system text without covering Agent actions', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(402, 874);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 1.5;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

    await tester.pumpWidget(const SpeakUpApp());
    await tester.pumpAndSettle();

    final lastAction = find.byKey(const Key('quick-action-recent-review'));
    await tester.ensureVisible(lastAction);
    await tester.pumpAndSettle();

    final lastActionRect = tester.getRect(lastAction);
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(composerRect.top - lastActionRect.bottom, greaterThanOrEqualTo(16));
    expect(lastAction.hitTestable(), findsOneWidget);
    expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'keeps navigation and drawer usable at accessibility text sizes',
    (tester) async {
      tester.view.physicalSize = const Size(320, 568);
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 3;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

      await tester.pumpWidget(const SpeakUpApp());
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
      expect(tester.takeException(), isNull);
      final selectedLabel = tester.renderObject<RenderParagraph>(
        find.text('SpeakUp'),
      );
      expect(selectedLabel.didExceedMaxLines, isFalse);

      final lastAction = find.byKey(const Key('quick-action-recent-review'));
      await tester.ensureVisible(lastAction);
      await tester.pumpAndSettle();
      final lastActionRect = tester.getRect(lastAction);
      final composerRect = tester.getRect(
        find.byKey(const Key('agent-composer-surface')),
      );
      expect(
        composerRect.top - lastActionRect.bottom,
        greaterThanOrEqualTo(16),
      );
      expect(lastAction.hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SpeakUpApp());
      await tester.pumpAndSettle();

      final menuButton = find.byKey(const Key('conversation-menu-button'));
      await tester.tap(menuButton);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(Drawer), findsOneWidget);

      await tester.drag(find.byType(ListView), const Offset(0, -1000));
      await tester.pumpAndSettle();
      final mockLabel = find.text('当前内容为 UI Mock');
      expect(mockLabel.hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}

const _primaryTabKeys = [
  'primary-tab-agent',
  'primary-tab-scenes',
  'primary-tab-review',
  'primary-tab-profile',
];

Future<void> _tapPrimaryDestination(
  WidgetTester tester, {
  required String key,
  required String expectedPageKey,
}) async {
  await tester.tap(find.byKey(Key(key)));
  await tester.pumpAndSettle();

  expect(find.byKey(Key(expectedPageKey)), findsOneWidget);
  expect(
    tester.getSemantics(find.byKey(Key(key))),
    isSemantics(hasSelectedState: true, isSelected: true),
  );
  expect(find.byKey(const Key('preparation-route-back-button')), findsNothing);
  expect(find.byKey(const Key('review-route-back-button')), findsNothing);
  expect(find.byKey(const Key('conversation-route-back-button')), findsNothing);
  expect(find.byKey(const Key('profile-route-back-button')), findsNothing);
}

Future<void> _expectNamedRoute<T extends Widget>(
  WidgetTester tester,
  String route, {
  required Finder backButton,
}) async {
  final shellContext = tester.element(find.byType(SpeakUpShell));
  Navigator.of(shellContext).pushNamed(route);
  await tester.pumpAndSettle();

  expect(find.byType(T), findsOneWidget);
  expect(backButton, findsOneWidget);

  await tester.tap(backButton);
  await tester.pumpAndSettle();

  expect(backButton, findsNothing);
  expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
  expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
  final rootShellContext = tester.element(find.byType(SpeakUpShell));
  expect(Navigator.of(rootShellContext).canPop(), isFalse);
}

Future<void> _tapVisible(WidgetTester tester, String key) async {
  final finder = find.byKey(Key(key));
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}
