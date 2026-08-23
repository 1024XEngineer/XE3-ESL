import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_completion_sheet.dart';

void main() {
  testWidgets('blocks practice content and exposes both completion actions', (
    tester,
  ) async {
    var primaryCalls = 0;
    var secondaryCalls = 0;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Stack(
            children: [
              const Positioned.fill(child: Text('练习内容')),
              Positioned.fill(
                child: PracticeCompletionOverlay(
                  title: '练习已完成',
                  message: '3 轮对话已保存',
                  primaryLabel: '查看复盘报告',
                  secondaryLabel: '返回列表',
                  onPrimary: () => primaryCalls++,
                  onSecondary: () => secondaryCalls++,
                ),
              ),
            ],
          ),
        ),
      ),
    );

    expect(
      find.descendant(
        of: find.byKey(const Key('practice-completion-overlay')),
        matching: find.byType(ModalBarrier),
      ),
      findsOneWidget,
    );
    expect(find.byKey(const Key('practice-completion-sheet')), findsOneWidget);

    await tester.tap(find.byKey(const Key('practice-completion-primary')));
    await tester.tap(find.byKey(const Key('practice-completion-secondary')));

    expect(primaryCalls, 1);
    expect(secondaryCalls, 1);
  });

  testWidgets('disables the primary action while it is loading', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Stack(
            children: [
              Positioned.fill(
                child: PracticeCompletionOverlay(
                  title: '练习已完成',
                  message: '回答已保存',
                  primaryLabel: '查看复盘报告',
                  secondaryLabel: '返回列表',
                  onPrimary: null,
                  onSecondary: null,
                  primaryLoading: true,
                ),
              ),
            ],
          ),
        ),
      ),
    );

    final button = tester.widget<FilledButton>(
      find.byKey(const Key('practice-completion-primary')),
    );
    expect(button.onPressed, isNull);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('follows a downward drag and rebounds below the threshold', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeCompletionOverlay(
            title: '练习已完成',
            message: '回答已保存',
            primaryLabel: '查看复盘报告',
            secondaryLabel: '返回列表',
            onPrimary: () {},
            onSecondary: () {},
          ),
        ),
      ),
    );

    final sheet = find.byKey(const Key('practice-completion-sheet'));
    final dragRegion = find.byKey(const Key('practice-completion-drag-region'));
    final initialTop = tester.getTopLeft(sheet).dy;
    final gesture = await tester.startGesture(tester.getCenter(dragRegion));
    await gesture.moveBy(const Offset(0, 30));
    await tester.pump();

    expect(tester.getTopLeft(sheet).dy, greaterThan(initialTop));
    final barrierDuringDrag = tester.widget<ModalBarrier>(
      find.descendant(
        of: find.byKey(const Key('practice-completion-overlay')),
        matching: find.byType(ModalBarrier),
      ),
    );
    expect(barrierDuringDrag.color!.a, lessThan(const Color(0x14000000).a));

    await gesture.up();
    await tester.pumpAndSettle();

    expect(tester.getTopLeft(sheet).dy, initialTop);
  });

  testWidgets('dismisses after a drag beyond the distance threshold', (
    tester,
  ) async {
    var dismissCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeCompletionOverlay(
            title: '练习已完成',
            message: '回答已保存',
            primaryLabel: '查看复盘报告',
            secondaryLabel: '返回列表',
            onPrimary: () {},
            onSecondary: () {},
            onDismissed: () => dismissCalls++,
          ),
        ),
      ),
    );

    await tester.timedDrag(
      find.byKey(const Key('practice-completion-drag-region')),
      const Offset(0, 180),
      const Duration(milliseconds: 600),
    );
    await tester.pumpAndSettle();

    expect(dismissCalls, 1);
    expect(find.byKey(const Key('practice-completion-sheet')), findsNothing);
    expect(
      find.descendant(
        of: find.byKey(const Key('practice-completion-overlay')),
        matching: find.byType(ModalBarrier),
      ),
      findsNothing,
    );
  });

  testWidgets('dismisses after a fast downward fling', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeCompletionOverlay(
            title: '练习已完成',
            message: '回答已保存',
            primaryLabel: '查看复盘报告',
            secondaryLabel: '返回列表',
            onPrimary: () {},
            onSecondary: () {},
          ),
        ),
      ),
    );

    await tester.fling(
      find.byKey(const Key('practice-completion-drag-region')),
      const Offset(0, 80),
      2000,
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-completion-sheet')), findsNothing);
  });

  testWidgets('cannot dismiss a required transition sheet', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeCompletionOverlay(
            title: 'Part 1 已完成',
            message: '回答已保存',
            primaryLabel: '进入 Part 2',
            secondaryLabel: '保存并退出',
            onPrimary: () {},
            onSecondary: () {},
            dismissible: false,
          ),
        ),
      ),
    );

    await tester.drag(
      find.byKey(const Key('practice-completion-drag-region')),
      const Offset(0, 240),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('practice-completion-sheet')), findsOneWidget);
    expect(find.text('进入 Part 2'), findsOneWidget);
  });
}
