import '../../support/scene_fixtures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scenario/scenario_catalog.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  const expectedAssets = <String, String>{
    'scn_daily_small_talk': 'assets/images/scenes/small-talk.jpg',
    'scn_daily_restaurant_ordering': 'assets/images/scenes/daily-tutor.jpg',
    'scn_daily_shopping_return':
        'assets/images/scenes/daily-shopping-return.jpg',
    'scn_daily_airport_transport': 'assets/images/scenes/airport-transport.jpg',
    'scn_daily_hotel_checkin_issue': 'assets/images/scenes/travel-scene.jpg',
    'scn_daily_rental_maintenance':
        'assets/images/scenes/daily-rental-maintenance.jpg',
    'scn_daily_medical_appointment':
        'assets/images/scenes/daily-medical-appointment.jpg',
    'scn_daily_phone_call': 'assets/images/scenes/daily-phone-call.jpg',
    'scn_daily_complaint_help': 'assets/images/scenes/daily-complaint-help.jpg',
    'scn_workplace_progress_risk_update':
        'assets/images/scenes/workplace-scene.jpg',
    'scn_workplace_meeting_disagreement':
        'assets/images/scenes/meeting-disagreement.jpg',
    'scn_workplace_cross_team_alignment':
        'assets/images/scenes/workplace-cross-team-alignment.jpg',
    'scn_workplace_feedback_conflict':
        'assets/images/scenes/workplace-feedback-conflict.jpg',
    'scn_workplace_client_delay':
        'assets/images/scenes/workplace-client-delay.jpg',
    'scn_workplace_solution_presentation':
        'assets/images/scenes/workplace-solution-presentation.jpg',
    'scn_workplace_negotiation':
        'assets/images/scenes/workplace-negotiation.jpg',
  };

  testWidgets('uses one dedicated image for every known scenario', (
    tester,
  ) async {
    for (final entry in expectedAssets.entries) {
      final category = entry.key.startsWith('scn_workplace_')
          ? SceneCategory.workplaceGeneral
          : entry.key == 'scn_daily_airport_transport' ||
                entry.key == 'scn_daily_hotel_checkin_issue'
          ? SceneCategory.lifeTravel
          : SceneCategory.lifeDaily;

      await _pumpScene(tester, id: entry.key, category: category);

      expect(_sceneImageAsset(tester, entry.key), entry.value);
    }
    expect(expectedAssets.values.toSet(), hasLength(expectedAssets.length));
  });

  testWidgets('keeps category images as fallbacks for unknown scenarios', (
    tester,
  ) async {
    const cases = <(String, SceneCategory, String)>[
      (
        'scn_workplace_future',
        SceneCategory.workplaceGeneral,
        'assets/images/scenes/workplace-scene.jpg',
      ),
      (
        'scn_daily_future_travel',
        SceneCategory.lifeTravel,
        'assets/images/scenes/travel-scene.jpg',
      ),
      (
        'scn_daily_future_daily',
        SceneCategory.lifeDaily,
        'assets/images/scenes/daily-tutor.jpg',
      ),
    ];

    for (final (id, category, expectedAsset) in cases) {
      await _pumpScene(tester, id: id, category: category);

      expect(_sceneImageAsset(tester, id), expectedAsset);
    }
  });
}

Future<void> _pumpScene(
  WidgetTester tester, {
  required String id,
  required SceneCategory category,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: SingleChildScrollView(
          child: ScenarioCatalog(
            scenes: [
              testScene(
                id: id,
                experience: category == SceneCategory.workplaceGeneral
                    ? PracticeExperience.workplace
                    : PracticeExperience.lifeAndTravel,
                category: category,
                name: id,
              ),
            ],
            onScenePressed: (_) {},
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

String _sceneImageAsset(WidgetTester tester, String sceneId) {
  final card = find.byKey(Key('catalog-scene-$sceneId'));
  final image = tester.widget<Image>(
    find.descendant(of: card, matching: find.byType(Image)),
  );
  return (image.image as AssetImage).assetName;
}
