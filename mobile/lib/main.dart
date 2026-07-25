import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/practice/ios_practice_recorder.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/practice/wire_practice_client.dart';

void main() {
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
    ),
  );
}

final class ProductionAppDependencies {
  const ProductionAppDependencies({
    required this.authController,
    required this.agentController,
  });

  final AuthController authController;
  final AgentController agentController;
}

ProductionAppDependencies createProductionAppDependencies({
  required Uri baseUri,
  IdentityHttpTransport? identityTransport,
  IdentityHttpTransport? agentTransport,
  PracticeWireTransport? practiceTransport,
  PracticeRecorder? practiceRecorder,
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
  );
  authController = AuthController(
    identityClient: WireIdentityClient(
      baseUri: baseUri,
      transport: identityTransport,
    ),
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: agentController.clearPrivateState,
  );
  return ProductionAppDependencies(
    authController: authController,
    agentController: agentController,
  );
}
