import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/wire_preparation_client.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/practice/ios_practice_recorder.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/practice/wire_practice_client.dart';
import 'package:speakup/review/review_history_controller.dart';
import 'package:speakup/review/wire_review_history_client.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  const apiBaseUrl = String.fromEnvironment(
    'SPEAKUP_API_BASE_URL',
    defaultValue: 'http://127.0.0.1:8080',
  );
  final dependencies = createProductionAppDependencies(
    baseUri: Uri.parse(apiBaseUrl),
  );
  runApp(
    SpeakUpApp(
      authController: dependencies.authController,
      agentController: dependencies.agentController,
      preparationController: dependencies.preparationController,
      reviewHistoryController: dependencies.reviewHistoryController,
    ),
  );
}

final class ProductionAppDependencies {
  const ProductionAppDependencies({
    required this.authController,
    required this.agentController,
    required this.preparationController,
    required this.reviewHistoryController,
  });

  final AuthController authController;
  final AgentController agentController;
  final PreparationController preparationController;
  final ReviewHistoryController reviewHistoryController;
}

ProductionAppDependencies createProductionAppDependencies({
  required Uri baseUri,
  IdentityHttpTransport? identityTransport,
  IdentityHttpTransport? agentTransport,
  IdentityHttpTransport? preparationTransport,
  IdentityHttpTransport? reviewHistoryTransport,
  PracticeWireTransport? practiceTransport,
  PracticeMediaWireTransport? practiceMediaTransport,
  PracticeMediaWireTransport? signedAudioTransport,
  PracticeRecorder? practiceRecorder,
  PracticeMediaClient? practiceMediaClient,
  PracticeAudioPlayer? practiceAudioPlayer,
  SessionStore? sessionStore,
}) {
  late final AuthController authController;
  final agentController = AgentController(
    client: WireAgentClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: agentTransport,
    ),
    practiceClient: WirePracticeClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: practiceTransport,
    ),
    recorder: practiceRecorder ?? IosPracticeRecorder(),
    mediaClient:
        practiceMediaClient ??
        WirePracticeMediaClient(
          baseUri: baseUri,
          credentialProvider: () => authController.currentCredential,
          invalidateSession:
              ({required expectedSessionToken, required expectedGeneration}) {
                return authController.invalidateSession(
                  expectedSessionToken: expectedSessionToken,
                  expectedGeneration: expectedGeneration,
                );
              },
          apiTransport: practiceMediaTransport,
          signedAudioTransport: signedAudioTransport,
        ),
    audioPlayer: practiceAudioPlayer ?? AudioplayersPracticeAudioPlayer(),
  );
  final reviewHistoryController = ReviewHistoryController(
    client: WireReviewHistoryClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: reviewHistoryTransport,
    ),
  );
  final preparationController = PreparationController(
    client: WirePreparationCatalogClient(
      baseUri: baseUri,
      transport: preparationTransport,
    ),
  );
  authController = AuthController(
    identityClient: WireIdentityClient(
      baseUri: baseUri,
      transport: identityTransport,
    ),
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: () async {
      await Future.wait<void>([
        agentController.clearPrivateState(),
        preparationController.clearPrivateState(),
        reviewHistoryController.clearPrivateState(),
      ]);
    },
  );
  return ProductionAppDependencies(
    authController: authController,
    agentController: agentController,
    preparationController: preparationController,
    reviewHistoryController: reviewHistoryController,
  );
}
