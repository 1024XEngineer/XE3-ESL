import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/scene_fixtures.dart';

void main() {
  test('routes each Scene Family to its intended presentation', () {
    expect(
      testScene(family: SceneFamily.interview).presentationMode,
      ScenePresentationMode.immersiveRoleplay,
    );
    expect(
      testScene(
        family: SceneFamily.workplace,
        model: SceneModel.workplaceBasicDialogue,
      ).presentationMode,
      ScenePresentationMode.immersiveRoleplay,
    );
    expect(
      testScene(
        family: SceneFamily.daily,
        model: SceneModel.dailyBasicDialogue,
      ).presentationMode,
      ScenePresentationMode.immersiveRoleplay,
    );
    expect(
      testScene(
        family: SceneFamily.exam,
        model: SceneModel.examBasicDialogue,
      ).presentationMode,
      ScenePresentationMode.standard,
    );
  });
}
