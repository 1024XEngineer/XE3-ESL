import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/image_picker_agent_image_picker.dart';
import 'package:speakup/agent/agent_voice_recording.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/agent/wire_agent_image_client.dart';
import 'package:speakup/agent/wire_agent_voice_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/practice/immersive_roleplay_session.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/ielts_practice_history_store.dart';
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
import 'package:speakup/practice/avatar/avatar.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/practice/wire_practice_client.dart';
import 'package:speakup/review/interview_report_controller.dart';
import 'package:speakup/review/review_history_controller.dart';
import 'package:speakup/review/wire_interview_report_client.dart';
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
      avatarControllerFactory: dependencies.avatarControllerFactory,
      interviewReportController: dependencies.interviewReportController,
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
    required this.avatarControllerFactory,
    required this.interviewReportController,
  });

  final AuthController authController;
  final AgentController agentController;
  final PreparationController preparationController;
  final JobPreparationController jobPreparationController;
  final PreparationLaunchController preparationLaunchController;
  final ReviewHistoryController reviewHistoryController;
  final AvatarControllerFactory avatarControllerFactory;
  final InterviewReportController interviewReportController;
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
  IdentityHttpTransport? interviewReportTransport,
  PracticeWireTransport? practiceTransport,
  PracticeMediaWireTransport? practiceMediaTransport,
  PracticeMediaWireTransport? signedAudioTransport,
  PracticeRecorder? practiceRecorder,
  AgentVoiceRecorder? agentVoiceRecorder,
  AgentVoiceAudioPlayer? agentVoiceAudioPlayer,
  PracticeMediaClient? practiceMediaClient,
  PracticeAudioPlayer? practiceAudioPlayer,
  AvatarSessionTokenClient? avatarSessionTokenClient,
  AvatarControllerFactory? avatarControllerFactory,
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
  final resolvedPracticeAudioPlayer =
      practiceAudioPlayer ?? AudioplayersPracticeAudioPlayer();
  final resolvedAvatarSessionTokenClient =
      avatarSessionTokenClient ??
      WireAvatarSessionTokenClient(
        baseUri: baseUri,
        credentialProvider: () => authController.currentCredential,
        invalidateSession:
            ({required expectedSessionToken, required expectedGeneration}) {
              return authController.invalidateSession(
                expectedSessionToken: expectedSessionToken,
                expectedGeneration: expectedGeneration,
              );
            },
      );
  final activeAvatarControllers = <AvatarController>{};
  final accountAvatarControllers = <AvatarController>{};
  AvatarController createAvatarController() {
    for (final active in activeAvatarControllers.toList(growable: false)) {
      unawaited(active.close().catchError((_) {}));
    }
    final controller =
        avatarControllerFactory?.call() ??
        AvatarController(
          renderer: SpatiusAvatarRenderer(),
          tokenClient: resolvedAvatarSessionTokenClient,
          fallbackPlayback: resolvedPracticeAudioPlayer.playWav,
          fallbackStop: resolvedPracticeAudioPlayer.stop,
        );
    activeAvatarControllers.add(controller);
    accountAvatarControllers.add(controller);
    late final void Function() removeClosedController;
    removeClosedController = () {
      if (controller.state.phase != AvatarControllerPhase.closed) {
        return;
      }
      controller.removeListener(removeClosedController);
      activeAvatarControllers.remove(controller);
    };
    controller.addListener(removeClosedController);
    return controller;
  }

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
    audioPlayer: resolvedPracticeAudioPlayer,
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
  final interviewReportController = InterviewReportController(
    client: WireInterviewReportClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: interviewReportTransport,
    ),
  );
  final preparationCatalogClient = WirePreparationCatalogClient(
    baseUri: baseUri,
    transport: preparationTransport,
  );
  final preparationController = PreparationController(
    client: preparationCatalogClient,
    ieltsQuestionBankClient: preparationCatalogClient,
    ieltsHistoryStore: const SecureIeltsPracticeHistoryStore(),
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
              scenarioType: selection.scenarioType,
              presentationMode:
                  selection.scenarioType == 'WORKPLACE' ||
                      selection.scenarioType == 'DAILY'
                  ? AgentScenePresentationMode.immersiveRoleplay
                  : AgentScenePresentationMode.standard,
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
  Future<void> clearAvatarPrivateState() async {
    final controllers = accountAvatarControllers.toList(growable: false);
    activeAvatarControllers.clear();
    accountAvatarControllers.clear();
    await Future.wait<void>([
      for (final controller in controllers)
        controller.clearAccountState().catchError((_) {}),
    ]);
    await resolvedAvatarSessionTokenClient.clearAccountState();
  }

  authController = AuthController(
    identityClient: identityClient,
    profileClient: identityClient,
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: () async {
      final interviewReportCleanup = interviewReportController
          .clearPrivateState();
      try {
        await preparationLaunchController.clearPrivateState();
        await clearAvatarPrivateState();
        await Future.wait<void>([
          agentController.clearPrivateState(),
          preparationController.clearPrivateState(),
          jobPreparationController.clearPrivateState(),
          reviewHistoryController.clearPrivateState(),
        ]);
      } finally {
        await interviewReportCleanup;
      }
    },
  );
  return ProductionAppDependencies(
    authController: authController,
    agentController: agentController,
    preparationController: preparationController,
    jobPreparationController: jobPreparationController,
    preparationLaunchController: preparationLaunchController,
    reviewHistoryController: reviewHistoryController,
    avatarControllerFactory: createAvatarController,
    interviewReportController: interviewReportController,
  );
}
