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
import 'package:speakup/features/coaching/preparation/practice_plan_handoff_controller.dart';
import 'package:speakup/features/coaching/goal/goal_client.dart';
import 'package:speakup/features/coaching/goal/wire_goal_client.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice_session.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_practice_history_store.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_draft_store.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
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
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/practice/wire_practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/review/interview_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_wire_client.dart';
import 'package:speakup/features/coaching/ielts/wire_ielts_answer_preparation_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/review/wire_interview_report_client.dart';
import 'package:speakup/features/coaching/review/wire_review_history_client.dart';
import 'package:speakup/features/coaching/evaluation/wire_turn_feedback_client.dart';
import 'package:speakup/resume/resume.dart';

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
      practicePlanHandoffController: dependencies.practicePlanHandoffController,
      reviewHistoryController: dependencies.reviewHistoryController,
      avatarControllerFactory: avatarEnabled
          ? dependencies.avatarControllerFactory
          : null,
      interviewReportController: dependencies.interviewReportController,
      ieltsSpeakingReportController: dependencies.ieltsSpeakingReportController,
      speechFeedbackController: dependencies.speechFeedbackController,
      resumeController: dependencies.resumeController,
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
    required this.goalClient,
    required this.preparationController,
    required this.ieltsPreparationController,
    required this.jobPreparationController,
    required this.preparationLaunchController,
    required this.practicePlanHandoffController,
    required this.reviewHistoryController,
    required this.avatarControllerFactory,
    required this.interviewReportController,
    required this.ieltsSpeakingReportController,
    required this.speechFeedbackController,
    required this.resumeController,
  });

  final AuthController authController;
  final ConversationController conversationController;
  final ComposerController composerController;
  final AgentMessageAudioController messageAudioController;
  final AgentMessageTranslationClient messageTranslationClient;
  final PracticeController practiceController;
  final GoalClient goalClient;
  final PreparationController preparationController;
  final IeltsPreparationController ieltsPreparationController;
  final JobPreparationController jobPreparationController;
  final PreparationLaunchController preparationLaunchController;
  final PracticePlanHandoffController practicePlanHandoffController;
  final ReviewHistoryController reviewHistoryController;
  final AvatarControllerFactory avatarControllerFactory;
  final InterviewReportController interviewReportController;
  final IeltsSpeakingReportController ieltsSpeakingReportController;
  final SpeechFeedbackController speechFeedbackController;
  final ResumeController resumeController;
}

ProductionAppDependencies createProductionAppDependencies({
  required Uri baseUri,
  IdentityHttpTransport? identityTransport,
  IdentityHttpTransport? agentTransport,
  IdentityHttpTransport? goalTransport,
  AgentVoiceWireTransport? agentVoiceTransport,
  AgentVoiceWireTransport? signedAgentVoiceTransport,
  AgentImageWireTransport? agentImageTransport,
  IdentityHttpTransport? preparationTransport,
  IdentityHttpTransport? jobPreparationTransport,
  IdentityHttpTransport? preparationLaunchTransport,
  IdentityHttpTransport? reviewHistoryTransport,
  IdentityHttpTransport? interviewReportTransport,
  IdentityHttpTransport? ieltsSpeakingReportTransport,
  IdentityHttpTransport? ieltsAnswerPreparationTransport,
  IdentityHttpTransport? speechFeedbackTransport,
  PracticeWireTransport? practiceTransport,
  PracticeMediaWireTransport? practiceMediaTransport,
  PracticeMediaWireTransport? signedAudioTransport,
  PracticeRecorder? practiceRecorder,
  AgentVoiceRecorder? agentVoiceRecorder,
  AgentAudioPlayer? agentMessageAudioPlayer,
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
  final goalClient = WireGoalClient(
    baseUri: baseUri,
    credentialProvider: () => authController.currentCredential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) {
          return authController.invalidateSession(
            expectedSessionToken: expectedSessionToken,
            expectedGeneration: expectedGeneration,
          );
        },
    transport: goalTransport,
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
  final conversationController = ConversationController(
    client: agentClient,
    messageImageClient: agentImageClient,
  );
  final GoalActivationClient goalActivationClient = goalClient;
  final messageAudioController = AgentMessageAudioController(
    conversationController: conversationController,
    client: agentVoiceClient,
    audioPlayer: agentMessageAudioPlayer ?? AudioplayersAgentAudioPlayer(),
    assistantSpeechClient: agentVoiceClient,
  );
  final composerController = ComposerController(
    conversationController: conversationController,
    imageClient: agentImageClient,
    imagePicker: ImagePickerAgentImagePicker(),
    voiceInputClient: agentVoiceClient,
    voiceRecorder: agentVoiceRecorder ?? IosAgentVoiceRecorder(),
  );
  final practiceController = PracticeController(
    client: practiceClient,
    recorder: practiceRecorder ?? IosPracticeRecorder(),
    mediaClient: resolvedPracticeMediaClient,
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
  final preparationCatalogClient = WireSceneClient(
    baseUri: baseUri,
    transport: preparationTransport,
  );
  final ieltsQuestionBankClient = WireIeltsQuestionBankClient(
    baseUri: baseUri,
    transport: preparationTransport,
  );
  final ieltsSpeakingReportClient = WireIeltsSpeakingReportClient(
    baseUri: baseUri,
    credentialProvider: () => authController.currentCredential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) {
          return authController.invalidateSession(
            expectedSessionToken: expectedSessionToken,
            expectedGeneration: expectedGeneration,
          );
        },
    transport: ieltsSpeakingReportTransport,
  );
  final ieltsAnswerPreparationClient = WireIeltsAnswerPreparationClient(
    baseUri: baseUri,
    credentialProvider: () => authController.currentCredential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) {
          return authController.invalidateSession(
            expectedSessionToken: expectedSessionToken,
            expectedGeneration: expectedGeneration,
          );
        },
    transport: ieltsAnswerPreparationTransport,
  );
  final ieltsSpeakingReportController = IeltsSpeakingReportController(
    client: ieltsSpeakingReportClient,
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
    answerPreparationClient: ieltsAnswerPreparationClient,
    historyStore: const SecureIeltsPracticeHistoryStore(),
  );
  final practiceWorkspaceController = PracticeWorkspaceController(
    conversationController: conversationController,
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
    contextProvider: () {
      final threadId = conversationController.threadId;
      final goalId = conversationController.activeGoalId;
      if (threadId == null || goalId == null) {
        return null;
      }
      return AgentPracticeContext(threadId: threadId, goalId: goalId);
    },
    threadIdProvider: () => conversationController.threadId,
    goalActivator:
        ({
          required threadId,
          required selection,
          required clientOperationId,
        }) async {
          final goal = await goalActivationClient.startScene(
            threadId: threadId,
            scene: selection.scene,
            clientOperationId: clientOperationId,
          );
          conversationController.applyActiveGoal(
            threadId: threadId,
            goalId: goal.id,
          );
          return AgentPracticeContext(threadId: threadId, goalId: goal.id);
        },
    voiceActivator:
        ({
          required context,
          required scene,
          required bootstrap,
          required clientOperationId,
        }) => practiceController.activateCreatedPractice(
          scene: scene,
          sessionId: bootstrap.session.id,
          planId: bootstrap.session.planId,
          practiceMode: bootstrap.session.practiceMode,
          turnLimit: bootstrap.maxEffectiveTurns,
          clientOperationId: clientOperationId,
        ),
  );
  final practicePlanHandoffController = PracticePlanHandoffController(
    conversationController: conversationController,
    practiceController: practiceController,
    workspaceController: practiceWorkspaceController,
    readPlan: preparationLaunchClient.getPlan,
    confirmPlan: ({required plan, required input, required idempotencyKey}) =>
        preparationLaunchClient.createSession(
          plan: plan,
          input: input,
          idempotencyKey: idempotencyKey,
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
    threadIdProvider: () => conversationController.threadId,
    goalActivator:
        ({
          required threadId,
          required candidate,
          required clientOperationId,
        }) async {
          final scene = await preparationCatalogClient.getScene(
            candidate.catalogRecommendation.sceneId,
          );
          if (scene.version != candidate.catalogRecommendation.sceneVersion) {
            throw StateError('Scene catalog changed before Goal activation.');
          }
          final goal = await goalActivationClient.startScene(
            threadId: threadId,
            scene: scene,
            clientOperationId: clientOperationId,
          );
          conversationController.applyActiveGoal(
            threadId: threadId,
            goalId: goal.id,
          );
          return AgentPracticeContext(threadId: threadId, goalId: goal.id);
        },
    voiceActivator:
        ({
          required context,
          required scene,
          required bootstrap,
          required clientOperationId,
        }) => practiceController.activateCreatedPractice(
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
  final resumeController = ResumeController(
    client: WireResumeClient(
      baseUri: baseUri,
      credentialProvider: () => authController.currentCredential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) {
            return authController.invalidateSession(
              expectedSessionToken: expectedSessionToken,
              expectedGeneration: expectedGeneration,
            );
          },
    ),
    filePicker: const SystemResumeFilePicker(),
    urlOpener: const SystemResumeUrlOpener(),
  );
  Future<void> clearAvatarPrivateState() async {
    final controllers = accountAvatarControllers.toList(growable: false);
    activeAvatarControllers.clear();
    accountAvatarControllers.clear();
    await _runAllPrivateStateCleanups([
      for (final controller in controllers) controller.clearAccountState,
      resolvedAvatarSessionTokenClient.clearAccountState,
    ]);
  }

  authController = AuthController(
    identityClient: identityClient,
    profileClient: identityClient,
    sessionStore: sessionStore ?? const IosKeychainSessionStore(),
    clearPrivateState: () => _runAllPrivateStateCleanups([
      interviewReportController.clearPrivateState,
      ieltsSpeakingReportController.clearPrivateState,
      speechFeedbackController.clearPrivateState,
      resumeController.clearPrivateState,
      preparationLaunchController.clearPrivateState,
      practicePlanHandoffController.clearAccountState,
      clearAvatarPrivateState,
      conversationController.clearPrivateState,
      composerController.clearPrivateState,
      messageAudioController.clearPrivateState,
      practiceController.clearPrivateState,
      goalClient.clearAccountState,
      preparationController.clearPrivateState,
      ieltsPreparationController.clearPrivateState,
      jobPreparationController.clearPrivateState,
      reviewHistoryController.clearPrivateState,
    ]),
  );
  return ProductionAppDependencies(
    authController: authController,
    conversationController: conversationController,
    composerController: composerController,
    messageAudioController: messageAudioController,
    messageTranslationClient: agentClient,
    practiceController: practiceController,
    goalClient: goalClient,
    preparationController: preparationController,
    ieltsPreparationController: ieltsPreparationController,
    jobPreparationController: jobPreparationController,
    preparationLaunchController: preparationLaunchController,
    practicePlanHandoffController: practicePlanHandoffController,
    reviewHistoryController: reviewHistoryController,
    avatarControllerFactory: createAvatarController,
    interviewReportController: interviewReportController,
    ieltsSpeakingReportController: ieltsSpeakingReportController,
    speechFeedbackController: speechFeedbackController,
    resumeController: resumeController,
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
