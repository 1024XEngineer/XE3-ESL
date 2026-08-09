import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_message_bubble.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

void main() {
  testWidgets('uses the shared Markdown renderer for an assistant message', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: PracticeMessageBubble(
            message: PracticeMessage(
              id: 'practice-assistant',
              role: PracticeMessageRole.assistant,
              text: 'Try **one complete sentence**.',
            ),
          ),
        ),
      ),
    );

    final selectable = find.descendant(
      of: find.byKey(const Key('practice-message-practice-assistant')),
      matching: find.byType(SelectableText),
    );
    expect(selectable, findsWidgets);
    expect(find.textContaining('one complete sentence'), findsOneWidget);
    expect(find.textContaining('**'), findsNothing);
  });

  testWidgets('places a practice action beside the shared translation action', (
    tester,
  ) async {
    var playbackCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeMessageBubble(
            message: const PracticeMessage(
              id: 'practice-actions',
              role: PracticeMessageRole.assistant,
              text: 'Tell me about your role.',
            ),
            onTranslate: (_) async => '介绍一下你的角色。',
            actions: TextButton(
              key: const Key('practice-play'),
              onPressed: () => playbackCalls++,
              child: const Text('播放'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('practice-play')));
    await tester.tap(
      find.byKey(const Key('practice-assistant-translate-practice-actions')),
    );
    await tester.pump();

    expect(playbackCalls, 1);
    expect(find.text('介绍一下你的角色。'), findsOneWidget);
  });
}
