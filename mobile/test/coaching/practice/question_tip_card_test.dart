import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/question_tip_sheet.dart';

void main() {
  testWidgets('places a smaller Chinese translation after the English Tip', (
    tester,
  ) async {
    var speakCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: QuestionTipCard(
            content: 'I would start with the goal and explain my approach.',
            translation: '我会先说明目标，再解释我的方法。',
            onClose: () {},
            onSpeak: () async => speakCalls++,
          ),
        ),
      ),
    );

    final english = tester.widget<Text>(
      find.byKey(const Key('practice-question-tip-content')),
    );
    final chinese = tester.widget<Text>(
      find.byKey(const Key('practice-question-tip-translation')),
    );
    expect(chinese.data, '我会先说明目标，再解释我的方法。');
    expect(chinese.style!.fontSize, lessThan(english.style!.fontSize!));
    expect(
      tester.getTopLeft(find.byWidget(chinese)).dy,
      greaterThan(tester.getTopLeft(find.byWidget(english)).dy),
    );

    await tester.tap(
      find.byKey(const Key('practice-question-tip-speak-inline')),
    );
    await tester.pump();
    expect(speakCalls, 1);
  });

  testWidgets('bilingual Tip remains scrollable at 320pt and large text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(3)),
          child: Scaffold(
            body: QuestionTipCard(
              content: List.filled(
                4,
                'I would explain the context, action, and result clearly.',
              ).join(' '),
              translation: List.filled(4, '我会清晰说明背景、行动和结果。').join(''),
              onClose: () {},
              onSpeak: () async {},
            ),
          ),
        ),
      ),
    );

    expect(find.byType(SingleChildScrollView), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('bilingual Tip sheet remains usable at 320pt and large text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(3)),
          child: Scaffold(
            body: QuestionTipSheet(
              content: List.filled(
                4,
                'I would explain the context, action, and result clearly.',
              ).join(' '),
              translation: List.filled(4, '我会清晰说明背景、行动和结果。').join(''),
              onSpeak: () async {},
            ),
          ),
        ),
      ),
    );

    expect(find.byType(SingleChildScrollView), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
