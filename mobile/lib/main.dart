import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  const apiBaseUrl = String.fromEnvironment(
    'SPEAKUP_API_BASE_URL',
    defaultValue: 'http://127.0.0.1:8080',
  );
  // #88 remains an explicit local preview until #86 publishes the reviewed
  // Agent HTTP contract. Authentication is real; Agent data is still Fake.
  final agentController = AgentController(client: FakeAgentClient());
  runApp(
    SpeakUpApp(
      authController: createProductionAuthController(
        baseUri: Uri.parse(apiBaseUrl),
        clearPrivateState: agentController.clearPrivateState,
      ),
      agentController: agentController,
    ),
  );
}

AuthController createProductionAuthController({
  required Uri baseUri,
  IdentityHttpTransport? transport,
  SessionStore? sessionStore,
  PrivateStateCleanup? clearPrivateState,
}) {
  return AuthController(
    identityClient: WireIdentityClient(baseUri: baseUri, transport: transport),
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: clearPrivateState,
  );
}
