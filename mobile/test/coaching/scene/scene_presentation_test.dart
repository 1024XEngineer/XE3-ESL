import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/scene_fixtures.dart';

void main() {
  test('routes each Practice Experience to its intended presentation', () {
    expect(
      testScene(experience: PracticeExperience.interview).presentationMode,
      ScenePresentationMode.immersiveRoleplay,
    );
    expect(
      testScene(
        experience: PracticeExperience.workplace,
        category: SceneCategory.workplaceGeneral,
      ).presentationMode,
      ScenePresentationMode.immersiveRoleplay,
    );
    expect(
      testScene(
        experience: PracticeExperience.lifeAndTravel,
        category: SceneCategory.lifeDaily,
      ).presentationMode,
      ScenePresentationMode.immersiveRoleplay,
    );
    expect(
      testScene(
        experience: PracticeExperience.ieltsSpeaking,
        category: SceneCategory.ieltsSpeaking,
      ).presentationMode,
      ScenePresentationMode.standard,
    );
  });
}
