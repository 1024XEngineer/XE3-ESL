import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_client.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/agent_message_image_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_handoff_controller.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice.dart';
import 'package:speakup/features/coaching/scenario/scenario_practice_session.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_practice.dart';
import 'package:speakup/features/coaching/interview/interview_practice.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_wizard.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_gate.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/review/interview_report_controller.dart';
import 'package:speakup/features/coaching/review/interview_report_view.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/resume/resume_controller.dart';

class SpeakUpApp extends StatelessWidget {
  const SpeakUpApp({
    required AuthController authController,
    required this.conversationController,
    required this.composerController,
    required this.messageAudioController,
    required this.messageTranslationClient,
    required this.practiceController,
    required this.preparationController,
    required this.ieltsPreparationController,
    this.jobPreparationController,
    this.preparationLaunchController,
    this.practicePlanHandoffController,
    this.reviewHistoryController,
    this.avatarControllerFactory,
    this.interviewReportController,
    this.ieltsSpeakingReportController,
    this.speechFeedbackController,
    this.resumeController,
    super.key,
  }) : _authentication = (controller: authController),
       _allowFakePreview = false;

  const SpeakUpApp.preview({
    this.conversationController,
    this.composerController,
    this.messageAudioController,
    this.messageTranslationClient,
    this.practiceController,
    this.preparationController,
    this.ieltsPreparationController,
    this.jobPreparationController,
    this.preparationLaunchController,
    this.practicePlanHandoffController,
    this.reviewHistoryController,
    this.avatarControllerFactory,
    this.interviewReportController,
    this.ieltsSpeakingReportController,
    this.speechFeedbackController,
    this.resumeController,
    super.key,
  }) : _authentication = null,
       _allowFakePreview = true;

  final ({AuthController controller})? _authentication;
  final ConversationController? conversationController;
  final ComposerController? composerController;
  final AgentMessageAudioController? messageAudioController;
  final AgentMessageTranslationClient? messageTranslationClient;
  final PracticeController? practiceController;
  final PreparationController? preparationController;
  final IeltsPreparationController? ieltsPreparationController;
  final JobPreparationController? jobPreparationController;
  final PreparationLaunchController? preparationLaunchController;
  final PracticePlanHandoffController? practicePlanHandoffController;
  final ReviewHistoryController? reviewHistoryController;
  final AvatarControllerFactory? avatarControllerFactory;
  final InterviewReportController? interviewReportController;
  final IeltsSpeakingReportController? ieltsSpeakingReportController;
  final SpeechFeedbackController? speechFeedbackController;
  final ResumeController? resumeController;
  final bool _allowFakePreview;

  @override
  Widget build(BuildContext context) {
    final controller = _authentication?.controller;
    return MaterialApp(
      title: 'SpeakUp',
      debugShowCheckedModeBanner: false,
      theme: SpeakUpTheme.light,
      home: controller == null
          ? _AuthenticatedNavigator(
              conversationController: conversationController,
              composerController: composerController,
              messageAudioController: messageAudioController,
              messageTranslationClient: messageTranslationClient,
              practiceController: practiceController,
              preparationController: preparationController,
              ieltsPreparationController: ieltsPreparationController,
              jobPreparationController: jobPreparationController,
              preparationLaunchController: preparationLaunchController,
              practicePlanHandoffController: practicePlanHandoffController,
              reviewHistoryController: reviewHistoryController,
              avatarControllerFactory: avatarControllerFactory,
              interviewReportController: interviewReportController,
              ieltsSpeakingReportController: ieltsSpeakingReportController,
              speechFeedbackController: speechFeedbackController,
              resumeController: resumeController,
              allowFakePreview: _allowFakePreview,
            )
          : AuthGate(
              controller: controller,
              authenticatedBuilder: (_, user) => _AuthenticatedNavigator(
                user: user,
                authController: controller,
                conversationController: conversationController,
                composerController: composerController,
                messageAudioController: messageAudioController,
                messageTranslationClient: messageTranslationClient,
                practiceController: practiceController,
                preparationController: preparationController,
                ieltsPreparationController: ieltsPreparationController,
                jobPreparationController: jobPreparationController,
                preparationLaunchController: preparationLaunchController,
                practicePlanHandoffController: practicePlanHandoffController,
                reviewHistoryController: reviewHistoryController,
                avatarControllerFactory: avatarControllerFactory,
                interviewReportController: interviewReportController,
                ieltsSpeakingReportController: ieltsSpeakingReportController,
                speechFeedbackController: speechFeedbackController,
                resumeController: resumeController,
                allowFakePreview: _allowFakePreview,
              ),
            ),
    );
  }
}

class _AuthenticatedNavigator extends StatefulWidget {
  const _AuthenticatedNavigator({
    this.user,
    this.authController,
    this.conversationController,
    this.composerController,
    this.messageAudioController,
    this.messageTranslationClient,
    this.practiceController,
    this.preparationController,
    this.ieltsPreparationController,
    this.jobPreparationController,
    this.preparationLaunchController,
    this.practicePlanHandoffController,
    this.reviewHistoryController,
    this.avatarControllerFactory,
    this.interviewReportController,
    this.ieltsSpeakingReportController,
    this.speechFeedbackController,
    this.resumeController,
    required this.allowFakePreview,
  });

  final User? user;
  final AuthController? authController;
  final ConversationController? conversationController;
  final ComposerController? composerController;
  final AgentMessageAudioController? messageAudioController;
  final AgentMessageTranslationClient? messageTranslationClient;
  final PracticeController? practiceController;
  final PreparationController? preparationController;
  final IeltsPreparationController? ieltsPreparationController;
  final JobPreparationController? jobPreparationController;
  final PreparationLaunchController? preparationLaunchController;
  final PracticePlanHandoffController? practicePlanHandoffController;
  final ReviewHistoryController? reviewHistoryController;
  final AvatarControllerFactory? avatarControllerFactory;
  final InterviewReportController? interviewReportController;
  final IeltsSpeakingReportController? ieltsSpeakingReportController;
  final SpeechFeedbackController? speechFeedbackController;
  final ResumeController? resumeController;
  final bool allowFakePreview;

  @override
  State<_AuthenticatedNavigator> createState() =>
      _AuthenticatedNavigatorState();
}

class _AuthenticatedNavigatorState extends State<_AuthenticatedNavigator> {
  final _navigatorKey = GlobalKey<NavigatorState>();
  late final ConversationController _conversationController;
  late final ComposerController _composerController;
  late final AgentMessageAudioController? _messageAudioController;
  late final PracticeController _practiceController;
  late final bool _ownsConversationController;
  late final bool _ownsComposerController;
  late final bool _ownsMessageAudioController;
  late final bool _ownsPracticeController;

  Future<void> _openSavedInterviewPlan(String planId) async {
    final controller = widget.jobPreparationController;
    if (controller == null || !await controller.openSavedPlan(planId)) {
      return;
    }
    _navigatorKey.currentState?.pushNamed(AppRoutes.jobPreparation);
  }

  @override
  void initState() {
    super.initState();
    final previewVoiceClient = widget.allowFakePreview
        ? FakeAgentVoiceClient()
        : null;
    final injectedController = widget.conversationController;
    if (injectedController == null && !widget.allowFakePreview) {
      throw StateError(
        'Production SpeakUpApp requires an injected ConversationController.',
      );
    }
    _ownsConversationController = injectedController == null;
    _conversationController =
        injectedController ??
        ConversationController(
          client: FakeAgentClient(),
          messageImageClient: FakeAgentMessageImageClient(),
        );
    final injectedMessageAudioController = widget.messageAudioController;
    if (injectedMessageAudioController == null && !widget.allowFakePreview) {
      throw StateError(
        'Production SpeakUpApp requires an injected '
        'AgentMessageAudioController.',
      );
    }
    _ownsMessageAudioController = injectedMessageAudioController == null;
    _messageAudioController =
        injectedMessageAudioController ??
        AgentMessageAudioController(
          conversationController: _conversationController,
          client: previewVoiceClient!,
          audioPlayer: FakeAgentAudioPlayer(),
        );
    final injectedComposerController = widget.composerController;
    if (injectedComposerController == null && !widget.allowFakePreview) {
      throw StateError(
        'Production SpeakUpApp requires an injected ComposerController.',
      );
    }
    _ownsComposerController = injectedComposerController == null;
    _composerController =
        injectedComposerController ??
        ComposerController(
          conversationController: _conversationController,
          voiceClient: previewVoiceClient,
          onAssistantCommitted: _messageAudioController?.playCommittedAssistant,
        );
    final injectedPracticeController = widget.practiceController;
    if (injectedPracticeController == null && !widget.allowFakePreview) {
      throw StateError(
        'Production SpeakUpApp requires an injected PracticeController.',
      );
    }
    _ownsPracticeController = injectedPracticeController == null;
    _practiceController =
        injectedPracticeController ??
        PracticeController(client: FakePracticeClient());
    _conversationController.initialize();
    final user = widget.user;
    if (user != null) {
      unawaited(_activateAccount(user.id));
    }
  }

  @override
  void didUpdateWidget(covariant _AuthenticatedNavigator oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.user?.id != widget.user?.id ||
        oldWidget.preparationController != widget.preparationController ||
        oldWidget.ieltsPreparationController !=
            widget.ieltsPreparationController ||
        oldWidget.jobPreparationController != widget.jobPreparationController ||
        oldWidget.preparationLaunchController !=
            widget.preparationLaunchController) {
      final user = widget.user;
      if (user != null) {
        unawaited(_activateAccount(user.id));
      }
    }
  }

  Future<void> _activateAccount(String accountId) async {
    await widget.preparationLaunchController?.activateAccount(accountId);
    if (!mounted || widget.user?.id != accountId) {
      return;
    }
    await widget.ieltsPreparationController?.activateAccount(accountId);
    if (!mounted || widget.user?.id != accountId) {
      return;
    }
    await widget.jobPreparationController?.activateAccount(accountId);
  }

  @override
  void dispose() {
    if (_ownsComposerController) {
      _composerController.dispose();
    }
    if (_ownsMessageAudioController) {
      _messageAudioController?.dispose();
    }
    if (_ownsConversationController) {
      _conversationController.dispose();
    }
    if (_ownsPracticeController) {
      _practiceController.dispose();
    }
    super.dispose();
  }

  Future<CompletedPracticeRouteResult?> _openInterviewReport(
    InterviewPracticeCompletion completion,
  ) {
    final navigator = _navigatorKey.currentState;
    final reportController = widget.interviewReportController;
    if (navigator == null || reportController == null) {
      throw StateError('Interview report route is not configured.');
    }
    return navigator.push<CompletedPracticeRouteResult>(
      MaterialPageRoute<CompletedPracticeRouteResult>(
        builder: (_) => InterviewReportPage(
          practiceSessionId: completion.practiceSessionId,
          controller: reportController,
          title: completion.title,
          speechFeedbackController: widget.speechFeedbackController,
          speechFeedbackSourceKeys: completion.speechFeedbackSourceKeys,
          onContinueWithAgent:
              widget.preparationLaunchController?.completeAndContinueWithAgent,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Navigator(
      key: _navigatorKey,
      initialRoute: AppRoutes.home,
      onGenerateRoute: (settings) {
        final page = switch (settings.name) {
          AppRoutes.home => SpeakUpShell(
            previewMode: widget.allowFakePreview,
            user: widget.user,
            authController: widget.authController,
            conversationController: _conversationController,
            composerController: _composerController,
            messageAudioController: _messageAudioController,
            messageTranslationClient: widget.messageTranslationClient,
            practiceController: _practiceController,
            preparationController: widget.preparationController,
            ieltsPreparationController: widget.ieltsPreparationController,
            jobPreparationController: widget.jobPreparationController,
            preparationLaunchController: widget.preparationLaunchController,
            practicePlanHandoffController: widget.practicePlanHandoffController,
            reviewHistoryController: widget.reviewHistoryController,
            interviewReportController: widget.interviewReportController,
            ieltsSpeakingReportController: widget.ieltsSpeakingReportController,
            speechFeedbackController: widget.speechFeedbackController,
            resumeController: widget.resumeController,
          ),
          AppRoutes.preparation => PreparationPage(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            practiceController: _practiceController,
            preparationController: widget.preparationController,
            ieltsController: widget.ieltsPreparationController,
            launchController: widget.preparationLaunchController,
            jobPreparationController: widget.jobPreparationController,
            onOpenJobPreparation: widget.jobPreparationController == null
                ? null
                : () {
                    widget.jobPreparationController!.beginNewPreparation();
                    _navigatorKey.currentState?.pushNamed(
                      AppRoutes.jobPreparation,
                    );
                  },
            onOpenInterviewPlan: widget.jobPreparationController == null
                ? null
                : (planId) => unawaited(_openSavedInterviewPlan(planId)),
            onPracticeStarted: () => _navigatorKey.currentState
                ?.pushReplacementNamed(AppRoutes.practice),
          ),
          AppRoutes.jobPreparation
              when widget.jobPreparationController != null =>
            JobPreparationWizard(
              controller: widget.jobPreparationController!,
              catalogController: widget.preparationController,
              resumeController: widget.resumeController,
              onPracticeStarted: () => _navigatorKey.currentState
                  ?.pushReplacementNamed(AppRoutes.practice),
              onPlanCreated: () => _navigatorKey.currentState?.pop(),
            ),
          AppRoutes.practice => _buildPracticePage(),
          AppRoutes.conversation => SpeakUpShell(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            user: widget.user,
            authController: widget.authController,
            conversationController: _conversationController,
            composerController: _composerController,
            messageAudioController: _messageAudioController,
            messageTranslationClient: widget.messageTranslationClient,
            practiceController: _practiceController,
            preparationController: widget.preparationController,
            ieltsPreparationController: widget.ieltsPreparationController,
            jobPreparationController: widget.jobPreparationController,
            preparationLaunchController: widget.preparationLaunchController,
            practicePlanHandoffController: widget.practicePlanHandoffController,
            reviewHistoryController: widget.reviewHistoryController,
            interviewReportController: widget.interviewReportController,
            ieltsSpeakingReportController: widget.ieltsSpeakingReportController,
            speechFeedbackController: widget.speechFeedbackController,
            resumeController: widget.resumeController,
          ),
          AppRoutes.review => ReviewPage(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            practiceAvailable: true,
            historyController: widget.reviewHistoryController,
            ieltsSpeakingReportController: widget.ieltsSpeakingReportController,
          ),
          _ => null,
        };
        if (page == null) {
          return null;
        }
        return MaterialPageRoute<Object?>(
          settings: settings,
          builder: (_) => page,
        );
      },
    );
  }

  Widget _buildPracticePage() {
    final launchController = widget.preparationLaunchController;
    if (_practiceController.practiceExperience ==
        PracticeExperience.ieltsSpeaking) {
      return IeltsSpeakingMockPage(
        controller: _practiceController,
        onExitRequested: launchController?.parkCurrentPractice,
        ieltsController: widget.ieltsPreparationController,
        completedReportBuilder: widget.ieltsSpeakingReportController == null
            ? null
            : (_, practiceSessionId) => IeltsSpeakingSessionReportPanel(
                practiceSessionId: practiceSessionId,
                controller: widget.ieltsSpeakingReportController!,
              ),
        speechFeedbackController: widget.speechFeedbackController,
      );
    }
    final experience = _practiceController.practiceExperience;
    if (experience == PracticeExperience.interview ||
        experience == PracticeExperience.workplace ||
        experience == PracticeExperience.lifeAndTravel) {
      final factory = widget.avatarControllerFactory;
      if (factory != null) {
        return ScenarioPracticeSession(
          practiceController: _practiceController,
          avatarControllerFactory: factory,
          onPracticeCompleted: launchController?.completeAndContinueWithAgent,
          speechFeedbackController: widget.speechFeedbackController,
          onExitRequested: launchController?.parkCurrentPractice,
        );
      }
      return ScenarioPracticePage(
        previewMode: widget.allowFakePreview,
        practiceController: _practiceController,
        onPracticeCompleted: launchController?.completeAndContinueWithAgent,
        speechFeedbackController: widget.speechFeedbackController,
        onExitRequested: launchController?.parkCurrentPractice,
      );
    }
    if (experience == null && !widget.allowFakePreview) {
      return const _NoActivePracticePage();
    }
    return InterviewPracticePage(
      previewMode: widget.allowFakePreview,
      practiceController: _practiceController,
      onOpenInterviewReport: widget.interviewReportController == null
          ? null
          : _openInterviewReport,
      speechFeedbackController: widget.speechFeedbackController,
      onExitRequested: launchController?.parkCurrentPractice,
      onContinueWithAgent: launchController?.completeAndContinueWithAgent,
    );
  }
}

class _NoActivePracticePage extends StatelessWidget {
  const _NoActivePracticePage();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('practice-page'),
      appBar: AppBar(title: const Text('练习')),
      body: const SafeArea(child: Center(child: Text('当前没有进行中的练习。'))),
    );
  }
}
