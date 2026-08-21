import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_composer_dock.dart';

void main() {
  testWidgets('composer capsule uses a glass surface with an outer shadow', (
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
    final outerSurface = tester.widget<DecoratedBox>(
      find
          .descendant(
            of: find.byKey(capsuleKey),
            matching: find.byType(DecoratedBox),
          )
          .first,
    );
    final outerDecoration = outerSurface.decoration as BoxDecoration;

    expect(decoration.color, SpeakUpDesign.surfaceGlassStrong);
    expect((decoration.border as Border).top.color, SpeakUpDesign.borderGlass);
    expect(decoration.boxShadow, isNull);
    expect(outerDecoration.boxShadow, hasLength(1));
    expect(outerDecoration.boxShadow!.single.color, const Color(0x1F2D425E));
    expect(outerDecoration.boxShadow!.single.blurRadius, 30);
    expect(outerDecoration.boxShadow!.single.offset, const Offset(0, 10));
  });
}
