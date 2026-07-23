import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';

void main() {
  testWidgets('app starts and exposes every feature entry point', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp());

    expect(find.text('SpeakUp'), findsOneWidget);

    await _expectNavigationTo<PreparationPage>(tester, 'Preparation');
    await _expectNavigationTo<PracticePage>(tester, 'Practice');
    await _expectNavigationTo<ConversationPage>(tester, 'Conversation');
    await _expectNavigationTo<ReviewPage>(tester, 'Review');
  });
}

Future<void> _expectNavigationTo<T extends Widget>(
  WidgetTester tester,
  String entryLabel,
) async {
  await tester.tap(find.text(entryLabel));
  await tester.pumpAndSettle();

  expect(find.byType(T), findsOneWidget);

  await tester.pageBack();
  await tester.pumpAndSettle();
}
