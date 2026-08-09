import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/practice_conversation_components.dart';

void main() {
  testWidgets('shared transcribing composer shows the live transcript', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: PracticeTranscribingComposer(
            label: '正在识别英文回答…',
            transcript: 'I led the migration safely.',
            keyPrefix: 'practice',
          ),
        ),
      ),
    );

    expect(find.text('正在识别英文回答…'), findsOneWidget);
    expect(find.byKey(const Key('practice-live-transcript')), findsOneWidget);
    expect(find.text('I led the migration safely.'), findsOneWidget);
  });

  testWidgets('shared transcribing composer hides an empty snapshot', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: PracticeTranscribingComposer(
            label: 'Transcribing your answer…',
            transcript: '   ',
            keyPrefix: 'ielts-mock',
          ),
        ),
      ),
    );

    expect(find.text('Transcribing your answer…'), findsOneWidget);
    expect(find.byKey(const Key('ielts-mock-live-transcript')), findsNothing);
  });
}
