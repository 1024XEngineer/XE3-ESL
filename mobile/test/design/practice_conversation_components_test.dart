import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/voice_capture_control.dart';

void main() {
  testWidgets('inline feedback reports when no polish is needed', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: InlineLanguageFeedback(feedbackNotice: '表达已经很自然，无需润色'),
        ),
      ),
    );

    expect(find.text('表达已经很自然，无需润色'), findsNothing);
    await tester.tap(find.byKey(const Key('inline-language-optimize')));
    await tester.pump();
    expect(find.text('表达已经很自然，无需润色'), findsOneWidget);
  });

  testWidgets('shared recording composer shows the live transcript', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: VoiceCaptureControl(
            phase: VoiceCapturePhase.recording,
            onStart: () {},
            onSendVoice: () {},
            onConvertToText: () {},
            onCancel: () {},
            builder: (_, capture) => PracticeRecordingComposer(
              capture: capture,
              phase: VoiceCapturePhase.recording,
              keyPrefix: 'practice',
              transcript: 'I led the migration safely.',
            ),
          ),
        ),
      ),
    );

    expect(find.byKey(const Key('practice-live-transcript')), findsOneWidget);
    expect(find.text('I led the migration safely.'), findsOneWidget);
  });

  testWidgets('shared recording composer hides an empty snapshot', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: VoiceCaptureControl(
            phase: VoiceCapturePhase.recording,
            onStart: () {},
            onSendVoice: () {},
            onConvertToText: () {},
            onCancel: () {},
            builder: (_, capture) => PracticeRecordingComposer(
              capture: capture,
              phase: VoiceCapturePhase.recording,
              keyPrefix: 'practice',
              transcript: '   ',
            ),
          ),
        ),
      ),
    );

    expect(find.byKey(const Key('practice-live-transcript')), findsNothing);
  });
}
