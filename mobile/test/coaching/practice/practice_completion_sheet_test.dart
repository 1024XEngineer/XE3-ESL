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
}
