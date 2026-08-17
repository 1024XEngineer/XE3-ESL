import 'package:speakup/features/coaching/scene/scene.dart';

String? scenarioAssetPath(SceneDefinition scene) {
  const sceneAssets = <String, String>{
    'scn_daily_small_talk': 'assets/images/scenes/small-talk.jpg',
    'scn_daily_restaurant_ordering': 'assets/images/scenes/daily-tutor.jpg',
    'scn_daily_product_shopping':
        'assets/images/scenes/daily-product-shopping.jpg',
    'scn_daily_return_refund': 'assets/images/scenes/daily-shopping-return.jpg',
    'scn_daily_airport_transport': 'assets/images/scenes/airport-transport.jpg',
    'scn_daily_hotel_checkin_issue': 'assets/images/scenes/travel-scene.jpg',
    'scn_daily_rental_maintenance':
        'assets/images/scenes/daily-rental-maintenance.jpg',
    'scn_daily_medical_appointment':
        'assets/images/scenes/daily-medical-appointment.jpg',
    'scn_daily_phone_call': 'assets/images/scenes/daily-phone-call.jpg',
    'scn_daily_social_invitation':
        'assets/images/scenes/daily-social-invitation.jpg',
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
  if (sceneAssets[scene.id] case final assetPath?) {
    return assetPath;
  }
  return switch (scene.category) {
    SceneCategory.workplaceGeneral =>
      'assets/images/scenes/workplace-scene.jpg',
    SceneCategory.lifeTravel => 'assets/images/scenes/travel-scene.jpg',
    SceneCategory.lifeDaily => 'assets/images/scenes/daily-tutor.jpg',
    _ => null,
  };
}

String scenarioStageAssetPath(SceneDefinition scene) =>
    scenarioAssetPath(scene) ?? 'assets/images/scenes/daily-tutor.jpg';
