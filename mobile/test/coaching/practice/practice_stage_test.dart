import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_stage.dart';

void main() {
  testWidgets('keeps the role stage above practice content in portrait', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      const MaterialApp(
        home: PracticeStageLayout(
          stageRegionKey: Key('stage-region'),
          stage: ColoredBox(color: Colors.blue),
          content: ColoredBox(color: Colors.white),
        ),
      ),
    );

    expect(
      tester.getSize(find.byKey(const Key('stage-region'))).height,
      closeTo(844 * 0.34, 0.1),
    );
  });
}
