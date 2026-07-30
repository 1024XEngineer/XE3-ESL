import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/image_picker_agent_image_picker.dart';
import 'package:speakup/agent/agent_voice_recording.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/agent/wire_agent_image_client.dart';
import 'package:speakup/agent/wire_agent_voice_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/job_preparation_controller.dart';
import 'package:speakup/features/preparation/job_preparation_draft_store.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/preparation/wire_preparation_client.dart';
import 'package:speakup/features/preparation/wire_job_preparation_client.dart';
import 'package:speakup/features/preparation/wire_preparation_launch_client.dart';
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
      jobPreparationController: dependencies.jobPreparationController,
      preparationLaunchController: dependencies.preparationLaunchController,
      reviewHistoryController: dependencies.reviewHistoryController,
    ),
  );
}

final class ProductionAppDependencies {
  const ProductionAppDependencies({
    required this.authController,
    required this.agentController,
    required this.preparationController,
    required this.jobPreparationController,
    required this.preparationLaunchController,
    required this.reviewHistoryController,
  });

  final AuthController authController;
  final AgentController agentController;
  final PreparationController preparationController;
  final JobPreparationController jobPreparationController;
  final PreparationLaunchController preparationLaunchController;
  final ReviewHistoryController reviewHistoryController;
}

ProductionAppDependencies createProductionAppDependencies({
  required Uri baseUri,
  IdentityHttpTransport? identityTransport,
  IdentityHttpTransport? agentTransport,
  AgentVoiceWireTransport? agentVoiceTransport,
  AgentVoiceWireTransport? signedAgentVoiceTransport,
  AgentImageWireTransport? agentImageTransport,
  IdentityHttpTransport? preparationTransport,
  IdentityHttpTransport? jobPreparationTransport,
  IdentityHttpTransport? preparationLaunchTransport,
  IdentityHttpTransport? reviewHistoryTransport,
  PracticeWireTransport? practiceTransport,
  PracticeMediaWireTransport? practiceMediaTransport,
  PracticeMediaWireTransport? signedAudioTransport,
  PracticeRecorder? practiceRecorder,
  AgentVoiceRecorder? agentVoiceRecorder,
  AgentVoiceAudioPlayer? agentVoiceAudioPlayer,
  PracticeMediaClient? practiceMediaClient,
  PracticeAudioPlayer? practiceAudioPlayer,
  JobPreparationDraftStore? jobPreparationDraftStore,
  PracticeLaunchRecordStore? practiceLaunchRecordStore,
  SessionStore? sessionStore,
}) {
  late final AuthController authController;
  final agentClient = WireAgentClient(
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
  );
  final agentVoiceClient = WireAgentVoiceClient(
    baseUri: baseUri,
    credentialProvider: () => authController.currentCredential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) {
          return authController.invalidateSession(
            expectedSessionToken: expectedSessionToken,
            expectedGeneration: expectedGeneration,
          );
        },
    apiTransport: agentVoiceTransport,
    signedAudioTransport: signedAgentVoiceTransport,
  );
  final agentImageClient = WireAgentImageClient(
    baseUri: baseUri,
    credentialProvider: () => authController.currentCredential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) {
          return authController.invalidateSession(
            expectedSessionToken: expectedSessionToken,
            expectedGeneration: expectedGeneration,
          );
        },
    transport: agentImageTransport,
  );
  final agentController = AgentController(
    client: agentClient,
    imageClient: agentImageClient,
    imagePicker: ImagePickerAgentImagePicker(),
    voiceClient: agentVoiceClient,
    voiceRecorder: agentVoiceRecorder ?? IosAgentVoiceRecorder(),
    voiceAudioPlayer:
        agentVoiceAudioPlayer ?? AudioplayersAgentVoiceAudioPlayer(),
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
  final practiceWorkspaceController = PracticeWorkspaceController(
    agentController: agentController,
    recordStore:
        practiceLaunchRecordStore ?? const SecurePracticeLaunchRecordStore(),
  );
  final preparationLaunchController = PreparationLaunchController(
    client: WirePreparationLaunchClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: preparationLaunchTransport,
    ),
    workspaceController: practiceWorkspaceController,
    contextProvider: () {
      final threadId = agentController.threadId;
      final matterId = agentController.activeMatter?.id;
      if (threadId == null || matterId == null) {
        return null;
      }
      return AgentPracticeContext(threadId: threadId, matterId: matterId);
    },
    threadIdProvider: () => agentController.threadId,
    matterActivator:
        ({
          required threadId,
          required selection,
          required clientOperationId,
        }) async {
          final matter = await agentController.activateMatterForScenario(
            threadId: threadId,
            scene: AgentScene(
              id: selection.scenarioDefinitionId,
              title: selection.scenarioDisplayName,
              description: selection.scenarioDescription,
            ),
            clientOperationId: clientOperationId,
          );
          return AgentPracticeContext(threadId: threadId, matterId: matter.id);
        },
    voiceActivator:
        ({required context, required bootstrap, required clientOperationId}) =>
            agentController.activateCreatedPractice(
              threadId: context.threadId,
              matterId: context.matterId,
              sessionId: bootstrap.session.id,
              turnLimit: bootstrap.maxEffectiveTurns,
              clientOperationId: clientOperationId,
            ),
  );
  final jobPreparationController = JobPreparationController(
    client: WireJobPreparationClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: jobPreparationTransport,
    ),
    draftStore:
        jobPreparationDraftStore ?? const SecureJobPreparationDraftStore(),
    workspaceController: practiceWorkspaceController,
    threadIdProvider: () => agentController.threadId,
    matterActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async {
          final matter = await agentController.activateMatterForScenario(
            threadId: threadId,
            scene: AgentScene(
              id: candidate.catalogRecommendation.scenarioDefinitionId,
              title: '${candidate.jobTitle}英文面试',
              description: candidate.scopeNotice,
            ),
            clientOperationId: clientOperationId,
          );
          return AgentPracticeContext(threadId: threadId, matterId: matter.id);
        },
    voiceActivator:
        ({required context, required bootstrap, required clientOperationId}) =>
            agentController.activateCreatedPractice(
              threadId: context.threadId,
              matterId: context.matterId,
              sessionId: bootstrap.session.id,
              turnLimit: bootstrap.maxEffectiveTurns,
              clientOperationId: clientOperationId,
            ),
  );
  final identityClient = WireIdentityClient(
    baseUri: baseUri,
    transport: identityTransport,
  );
  authController = AuthController(
    identityClient: identityClient,
    profileClient: identityClient,
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: () async {
      await preparationLaunchController.clearPrivateState();
      await Future.wait<void>([
        agentController.clearPrivateState(),
        preparationController.clearPrivateState(),
        jobPreparationController.clearPrivateState(),
        reviewHistoryController.clearPrivateState(),
      ]);
    },
  );
  return ProductionAppDependencies(
    authController: authController,
    agentController: agentController,
    preparationController: preparationController,
    jobPreparationController: jobPreparationController,
    preparationLaunchController: preparationLaunchController,
    reviewHistoryController: reviewHistoryController,
  );
}
