import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_composer_dock.dart';

void main() {
  testWidgets('composer capsule uses a white surface with an outer shadow', (
    tester,
  ) async {
    const capsuleKey = Key('composer-capsule');

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ConversationComposerCapsule(
            key: capsuleKey,
            child: SizedBox.shrink(),
          ),
        ),
      ),
    );

    final capsule = tester.widget<AnimatedContainer>(
      find.descendant(
        of: find.byKey(capsuleKey),
        matching: find.byType(AnimatedContainer),
      ),
    );
    final decoration = capsule.decoration! as BoxDecoration;

    expect(decoration.color, SpeakUpDesign.surface);
    expect(decoration.border, isNull);
    expect(decoration.boxShadow, hasLength(1));
    expect(decoration.boxShadow!.single.color, const Color(0x14000000));
    expect(decoration.boxShadow!.single.blurRadius, 18);
    expect(decoration.boxShadow!.single.offset, const Offset(0, 4));
  });
}
