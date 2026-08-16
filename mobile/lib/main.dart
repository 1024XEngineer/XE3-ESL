import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/composer/image/image_picker_agent_image_picker.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/providers/agent/wire_agent_client.dart';
import 'package:speakup/providers/agent/wire_agent_image_client.dart';
import 'package:speakup/providers/agent/wire_agent_voice_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_answer_generation.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/ielts/wire_ielts_question_bank_client.dart';
import 'package:speakup/features/coaching/scene/wire_scene_client.dart';
import 'package:speakup/features/coaching/interview/wire_job_preparation_client.dart';
import 'package:speakup/features/coaching/preparation/wire_preparation_launch_client.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/features/coaching/practice/ios_practice_recorder.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/practice/wire_practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_client.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/review/wire_review_history_client.dart';
import 'package:speakup/features/coaching/evaluation/wire_turn_feedback_client.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice_session.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  const apiBaseUrl = String.fromEnvironment(
    'SPEAKUP_API_BASE_URL',
    defaultValue: 'http://127.0.0.1:8080',
  );
  const avatarEnabled = bool.fromEnvironment(
    'SPEAKUP_AVATAR_ENABLED',
    defaultValue: true,
  );
  final dependencies = createProductionAppDependencies(
    baseUri: Uri.parse(apiBaseUrl),
  );
  runApp(
    SpeakUpApp(
      authController: dependencies.authController,
      conversationController: dependencies.conversationController,
      composerController: dependencies.composerController,
      messageAudioController: dependencies.messageAudioController,
      messageTranslationClient: dependencies.messageTranslationClient,
      practiceController: dependencies.practiceController,
      preparationController: dependencies.preparationController,
      ieltsPreparationController: dependencies.ieltsPreparationController,
      jobPreparationController: dependencies.jobPreparationController,
      preparationLaunchController: dependencies.preparationLaunchController,
      practicePlanClientActionController:
          dependencies.practicePlanClientActionController,
      reviewHistoryController: dependencies.reviewHistoryController,
      sessionEvaluationController: dependencies.sessionEvaluationController,
      speechFeedbackController: dependencies.speechFeedbackController,
      coachingProfileController: dependencies.coachingProfileController,
      avatarControllerFactory: avatarEnabled
          ? dependencies.avatarControllerFactory
          : null,
    ),
  );
}

final class ProductionAppDependencies {
  const ProductionAppDependencies({
    required this.authController,
    required this.conversationController,
    required this.composerController,
    required this.messageAudioController,
    required this.messageTranslationClient,
    required this.practiceController,
    required this.preparationController,
    required this.ieltsPreparationController,
    required this.jobPreparationController,
    required this.preparationLaunchController,
    required this.practicePlanClientActionController,
    required this.reviewHistoryController,
    required this.sessionEvaluationController,
    required this.speechFeedbackController,
    required this.coachingProfileController,
    required this.avatarControllerFactory,
  });

  final AuthController authController;
  final ConversationController conversationController;
  final ComposerController composerController;
  final AgentMessageAudioController messageAudioController;
  final AgentMessageTranslationClient messageTranslationClient;
  final PracticeController practiceController;
  final PreparationController preparationController;
  final IeltsPreparationController ieltsPreparationController;
  final JobPreparationController jobPreparationController;
  final PreparationLaunchController preparationLaunchController;
  final PracticePlanClientActionController practicePlanClientActionController;
  final ReviewHistoryController reviewHistoryController;
  final SessionEvaluationController sessionEvaluationController;
  final SpeechFeedbackController speechFeedbackController;
  final CoachingProfileController coachingProfileController;
  final AvatarControllerFactory avatarControllerFactory;
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
  IdentityHttpTransport? sessionEvaluationTransport,
  IdentityHttpTransport? speechFeedbackTransport,
  IdentityHttpTransport? ieltsAnswerTransport,
  IdentityHttpTransport? coachingProfileTransport,
  PracticeWireTransport? practiceTransport,
  PracticeMediaWireTransport? practiceMediaTransport,
  PracticeMediaWireTransport? signedAudioTransport,
  PracticeRecorder? practiceRecorder,
  AgentVoiceRecorder? agentVoiceRecorder,
  AgentAudioPlayer? agentComposerAudioPlayer,
  AgentAudioPlayer? agentMessageAudioPlayer,
  PracticeMediaClient? practiceMediaClient,
  PracticeAudioPlayer? practiceAudioPlayer,
  AvatarSessionTokenClient? avatarSessionTokenClient,
  AvatarControllerFactory? avatarControllerFactory,
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
  AvatarController createAvatarController() =>
      avatarControllerFactory?.call() ??
      AvatarController(
        renderer: SpatiusAvatarRenderer(),
        tokenClient: resolvedAvatarSessionTokenClient,
        fallbackPlayback: resolvedPracticeAudioPlayer.playWav,
        fallbackStop: resolvedPracticeAudioPlayer.stop,
      );
  final practiceClient = WirePracticeClient(
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
  );
  final resolvedPracticeMediaClient =
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
      );
  late final AgentMessageAudioController messageAudioController;
  final conversationController = ConversationController(
    client: agentClient,
    messageImageClient: agentImageClient,
    onAssistantStreamStarted: (transientMessageId) => messageAudioController
        .startLiveAssistantSpeech(transientMessageId: transientMessageId),
    onAssistantStreamDelta: (transientMessageId, delta) =>
        messageAudioController.appendLiveAssistantSpeech(
          transientMessageId: transientMessageId,
          delta: delta,
        ),
    onAssistantStreamCompleted: (transientMessageId, message) =>
        messageAudioController.completeLiveAssistantSpeech(
          transientMessageId: transientMessageId,
          message: message,
        ),
    onAssistantStreamFailed: (transientMessageId) => messageAudioController
        .failLiveAssistantSpeech(transientMessageId: transientMessageId),
  );
  messageAudioController = AgentMessageAudioController(
    conversationController: conversationController,
    client: agentVoiceClient,
    audioPlayer: agentMessageAudioPlayer ?? AudioplayersAgentAudioPlayer(),
    assistantSpeechClient: agentVoiceClient,
  );
  final composerController = ComposerController(
    conversationController: conversationController,
    imageClient: agentImageClient,
    imagePicker: ImagePickerAgentImagePicker(),
    voiceClient: agentVoiceClient,
    voiceRecorder: agentVoiceRecorder ?? IosAgentVoiceRecorder(),
    draftAudioPlayer:
        agentComposerAudioPlayer ?? AudioplayersAgentAudioPlayer(),
    onAssistantStreamStarted: (transientMessageId) => messageAudioController
        .startLiveAssistantSpeech(transientMessageId: transientMessageId),
    onAssistantStreamDelta: (transientMessageId, delta) =>
        messageAudioController.appendLiveAssistantSpeech(
          transientMessageId: transientMessageId,
          delta: delta,
        ),
    onAssistantStreamCompleted: (transientMessageId, message) =>
        messageAudioController.completeLiveAssistantSpeech(
          transientMessageId: transientMessageId,
          message: message,
        ),
    onAssistantStreamFailed: (transientMessageId) => messageAudioController
        .failLiveAssistantSpeech(transientMessageId: transientMessageId),
  );
  final practiceController = PracticeController(
    client: practiceClient,
    recorder: practiceRecorder ?? IosPracticeRecorder(),
    mediaClient: resolvedPracticeMediaClient,
    audioPlayer: resolvedPracticeAudioPlayer,
    questionSpeechPlayer:
        resolvedPracticeMediaClient is PracticeQuestionSpeechClient
        ? MethodChannelPracticePCMStreamPlayer()
        : null,
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
  final preparationCatalogClient = WireSceneClient(
    baseUri: baseUri,
    transport: preparationTransport,
  );
  final ieltsQuestionBankClient = WireIeltsQuestionBankClient(
    baseUri: baseUri,
    transport: preparationTransport,
  );
  final sessionEvaluationController = SessionEvaluationController(
    client: WireSessionEvaluationClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: sessionEvaluationTransport,
    ),
  );
  final speechFeedbackController = SpeechFeedbackController(
    client: WireSpeechFeedbackClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: speechFeedbackTransport,
    ),
  );
  final preparationController = PreparationController(
    client: preparationCatalogClient,
  );
  final ieltsPreparationController = IeltsPreparationController(
    client: ieltsQuestionBankClient,
    answerGenerator: WireIeltsAnswerGenerator(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: ieltsAnswerTransport,
    ),
    historyStore: const SecureIeltsPracticeHistoryStore(),
  );
  final practiceWorkspaceController = PracticeWorkspaceController(
    practiceController: practiceController,
    recordStore:
        practiceLaunchRecordStore ?? const SecurePracticeLaunchRecordStore(),
  );
  final preparationLaunchClient = WirePreparationLaunchClient(
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
  );
  final preparationLaunchController = PreparationLaunchController(
    client: preparationLaunchClient,
    workspaceController: practiceWorkspaceController,
    voiceActivator:
        ({required scene, required bootstrap, required clientOperationId}) =>
            practiceController.activateCreatedPractice(
              scene: scene,
              sessionId: bootstrap.session.id,
              planId: bootstrap.session.planId,
              practiceMode: bootstrap.session.practiceMode,
              turnLimit: bootstrap.maxEffectiveTurns,
              clientOperationId: clientOperationId,
            ),
  );
  final practicePlanClientActionController = PracticePlanClientActionController(
    conversationController: conversationController,
    practiceController: practiceController,
    ieltsPreparationController: ieltsPreparationController,
    workspaceController: practiceWorkspaceController,
    readPlan: preparationLaunchClient.getPlan,
    confirmPlan: preparationLaunchClient.confirmPlan,
    createSession: preparationLaunchClient.createSession,
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
    workspaceController: practiceWorkspaceController,
    voiceActivator:
        ({required scene, required bootstrap, required clientOperationId}) =>
            practiceController.activateCreatedPractice(
              scene: scene,
              sessionId: bootstrap.session.id,
              planId: bootstrap.session.planId,
              practiceMode: bootstrap.session.practiceMode,
              turnLimit: bootstrap.maxEffectiveTurns,
              clientOperationId: clientOperationId,
            ),
  );
  final identityClient = WireIdentityClient(
    baseUri: baseUri,
    transport: identityTransport,
  );
  final coachingProfileController = CoachingProfileController(
    client: WireCoachingProfileClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
      transport: coachingProfileTransport,
    ),
  );
  authController = AuthController(
    identityClient: identityClient,
    profileClient: identityClient,
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: () => _runAllPrivateStateCleanups([
      sessionEvaluationController.clearAccountState,
      speechFeedbackController.clearPrivateState,
      preparationLaunchController.clearPrivateState,
      practicePlanClientActionController.clearAccountState,
      conversationController.clearPrivateState,
      composerController.clearPrivateState,
      messageAudioController.clearPrivateState,
      practiceController.clearPrivateState,
      preparationController.clearPrivateState,
      ieltsPreparationController.clearPrivateState,
      jobPreparationController.clearPrivateState,
      reviewHistoryController.clearPrivateState,
      coachingProfileController.clearPrivateState,
    ]),
  );
  return ProductionAppDependencies(
    authController: authController,
    conversationController: conversationController,
    composerController: composerController,
    messageAudioController: messageAudioController,
    messageTranslationClient: agentClient,
    practiceController: practiceController,
    preparationController: preparationController,
    ieltsPreparationController: ieltsPreparationController,
    jobPreparationController: jobPreparationController,
    preparationLaunchController: preparationLaunchController,
    practicePlanClientActionController: practicePlanClientActionController,
    reviewHistoryController: reviewHistoryController,
    sessionEvaluationController: sessionEvaluationController,
    speechFeedbackController: speechFeedbackController,
    coachingProfileController: coachingProfileController,
    avatarControllerFactory: createAvatarController,
  );
}

Future<void> _runAllPrivateStateCleanups(
  List<Future<void> Function()> cleanups,
) async {
  Object? firstError;
  StackTrace? firstStackTrace;
  for (final cleanup in cleanups) {
    try {
      await cleanup();
    } catch (error, stackTrace) {
      if (firstError == null) {
        firstError = error;
        firstStackTrace = stackTrace;
      }
    }
  }
  if (firstError case final error?) {
    Error.throwWithStackTrace(error, firstStackTrace!);
  }
}
