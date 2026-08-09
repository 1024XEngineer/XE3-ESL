import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_stage.dart';

void main() {
  testWidgets('lays out arbitrary practice content in portrait and landscape', (
    tester,
  ) async {
    Future<void> pumpAt(Size size) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      await tester.pumpWidget(
        const MaterialApp(
          home: PracticeStageLayout(
            avatarRegionKey: Key('avatar-region'),
            avatar: ColoredBox(key: Key('custom-avatar'), color: Colors.blue),
            content: ColoredBox(
              key: Key('custom-content'),
              color: Colors.white,
            ),
          ),
        ),
      );
    }

    addTearDown(tester.view.reset);

    await pumpAt(const Size(390, 844));
    expect(find.byKey(const Key('custom-avatar')), findsOneWidget);
    expect(find.byKey(const Key('custom-content')), findsOneWidget);
    expect(
      tester.getSize(find.byKey(const Key('avatar-region'))).height,
      closeTo(844 * 0.44, 0.1),
    );

    await pumpAt(const Size(844, 390));
    expect(
      tester.getSize(find.byKey(const Key('avatar-region'))).width,
      closeTo(844 * 0.44, 0.1),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('avatar stage accepts a surface and shared chrome content', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: PracticeAvatarStage(
          title: 'Custom practice',
          fallback: const ColoredBox(
            key: Key('custom-fallback'),
            color: Colors.grey,
          ),
          surfaceBuilder: (_) =>
              const ColoredBox(key: Key('custom-surface'), color: Colors.green),
          statusLabel: 'Connected',
          subtitle: 'Try answering in one complete sentence.',
          exitButtonKey: const Key('custom-exit'),
          statusKey: const Key('custom-status'),
          subtitleKey: const Key('custom-subtitle'),
          onExit: () {},
        ),
      ),
    );

    expect(find.byKey(const Key('custom-surface')), findsOneWidget);
    expect(find.byKey(const Key('custom-fallback')), findsNothing);
    expect(find.text('Custom practice'), findsOneWidget);
    expect(find.text('Connected'), findsOneWidget);
    expect(
      find.text('Try answering in one complete sentence.'),
      findsOneWidget,
    );
    expect(find.byKey(const Key('custom-exit')), findsOneWidget);
    expect(find.byKey(const Key('custom-status')), findsOneWidget);
    expect(find.byKey(const Key('custom-subtitle')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
