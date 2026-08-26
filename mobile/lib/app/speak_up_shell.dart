import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/platform_navigation_bar.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/client_action/practice_plan_client_action_card.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action_controller.dart';
import 'package:speakup/features/agent/conversation/conversation.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_practice.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/agent_conversation_feedback_presenter.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';
import 'package:speakup/features/profile/profile_page.dart';
import 'package:speakup/features/profile/profile_avatar_view.dart';
import 'package:speakup/features/update/app_update.dart';
import 'package:speakup/features/update/app_update_ui.dart';

class SpeakUpShell extends StatefulWidget {
  const SpeakUpShell({
    this.showBackButton = false,
    this.previewMode = false,
    this.user,
    this.authController,
    this.preparationController,
    this.ieltsPreparationController,
    this.preparationLaunchController,
    this.practicePlanClientActionController,
    this.jobPreparationController,
    this.reviewHistoryController,
    this.speechFeedbackController,
    this.coachingProfileController,
    this.appUpdateService,
    this.routeObserver,
    required this.conversationController,
    required this.composerController,
    this.messageAudioController,
    this.messageTranslationClient,
    required this.practiceController,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final User? user;
  final AuthController? authController;
  final ConversationController conversationController;
  final ComposerController composerController;
  final AgentMessageAudioController? messageAudioController;
  final AgentMessageTranslationClient? messageTranslationClient;
  final PracticeController practiceController;
  final PreparationController? preparationController;
  final IeltsPreparationController? ieltsPreparationController;
  final PreparationLaunchController? preparationLaunchController;
  final PracticePlanClientActionController? practicePlanClientActionController;
  final JobPreparationController? jobPreparationController;
  final ReviewHistoryController? reviewHistoryController;
  final SpeechFeedbackController? speechFeedbackController;
  final CoachingProfileController? coachingProfileController;
  final AppUpdateService? appUpdateService;
  final RouteObserver<ModalRoute<Object?>>? routeObserver;

  @override
  State<SpeakUpShell> createState() => _SpeakUpShellState();
}

class _SpeakUpShellState extends State<SpeakUpShell>
    with WidgetsBindingObserver, RouteAware {
  static const _destinations = [
    PlatformNavigationDestination(
      label: 'SpeakUp',
      icon: CupertinoIcons.waveform_circle,
      selectedIcon: CupertinoIcons.waveform_circle_fill,
      iosSystemImage: 'waveform.circle',
      iosSelectedSystemImage: 'waveform.circle.fill',
      key: Key('primary-tab-agent'),
    ),
    PlatformNavigationDestination(
      label: '训练',
      icon: CupertinoIcons.square_grid_2x2,
      selectedIcon: CupertinoIcons.square_grid_2x2_fill,
      iosSystemImage: 'square.grid.2x2',
      iosSelectedSystemImage: 'square.grid.2x2.fill',
      key: Key('primary-tab-scenes'),
    ),
    PlatformNavigationDestination(
      label: '复盘',
      icon: CupertinoIcons.chart_bar,
      selectedIcon: CupertinoIcons.chart_bar_fill,
      iosSystemImage: 'chart.bar',
      iosSelectedSystemImage: 'chart.bar.fill',
      key: Key('primary-tab-review'),
    ),
    PlatformNavigationDestination(
      label: '我的',
      icon: CupertinoIcons.person,
      selectedIcon: CupertinoIcons.person_fill,
      iosSystemImage: 'person',
      iosSelectedSystemImage: 'person.fill',
      key: Key('primary-tab-profile'),
    ),
  ];

  final _scaffoldKey = GlobalKey<ScaffoldState>();
  int _selectedIndex = 0;
  AgentConversationFeedbackPresenter? _feedbackPresenter;
  bool _practiceRouteInFlight = false;
  bool _clientActionInFlight = false;
  bool _conversationDrawerOpen = false;
  bool _updateCheckInFlight = false;
  bool _updateDialogVisible = false;
  bool _updatePresentationScheduled = false;
  ({InstalledAppVersion installedVersion, AppRelease release})?
  _pendingAutomaticUpdate;
  int _navigationGeneration = 0;
  ModalRoute<Object?>? _observedRoute;

  @override
  void initState() {
    super.initState();
    widget.authController?.addListener(_handleAuthState);
    widget.conversationController.addListener(_handleAgentInteractionState);
    widget.composerController.addListener(_handleAgentInteractionState);
    widget.practiceController.addListener(_handlePracticeState);
    _feedbackPresenter = _createFeedbackPresenter();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        unawaited(_checkForUpdate(automatic: true));
      }
    });
  }

  @override
  void didUpdateWidget(covariant SpeakUpShell oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.authController != widget.authController) {
      oldWidget.authController?.removeListener(_handleAuthState);
      widget.authController?.addListener(_handleAuthState);
    }
    final conversationControllerChanged =
        oldWidget.conversationController != widget.conversationController;
    if (conversationControllerChanged) {
      oldWidget.conversationController.removeListener(
        _handleAgentInteractionState,
      );
      widget.conversationController.addListener(_handleAgentInteractionState);
    }
    if (oldWidget.composerController != widget.composerController) {
      oldWidget.composerController.removeListener(_handleAgentInteractionState);
      widget.composerController.addListener(_handleAgentInteractionState);
    }
    if (oldWidget.practiceController != widget.practiceController) {
      oldWidget.practiceController.removeListener(_handlePracticeState);
      widget.practiceController.addListener(_handlePracticeState);
    }
    if (oldWidget.speechFeedbackController != widget.speechFeedbackController) {
      _feedbackPresenter?.dispose();
      _feedbackPresenter = _createFeedbackPresenter();
    }
    if (oldWidget.routeObserver != widget.routeObserver) {
      oldWidget.routeObserver?.unsubscribe(this);
      _observedRoute = null;
      _subscribeToRoute();
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _subscribeToRoute();
  }

  @override
  void dispose() {
    widget.routeObserver?.unsubscribe(this);
    widget.authController?.removeListener(_handleAuthState);
    widget.conversationController.removeListener(_handleAgentInteractionState);
    widget.composerController.removeListener(_handleAgentInteractionState);
    widget.practiceController.removeListener(_handlePracticeState);
    _feedbackPresenter?.dispose();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  void _subscribeToRoute() {
    final observer = widget.routeObserver;
    final route = ModalRoute.of<Object?>(context);
    if (observer == null || route == null || identical(route, _observedRoute)) {
      return;
    }
    observer.unsubscribe(this);
    observer.subscribe(this, route);
    _observedRoute = route;
  }

  @override
  void didPopNext() {
    _schedulePendingUpdatePresentation();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      unawaited(_checkForUpdate(automatic: true));
    }
  }

  void _handleAuthState() {
    if (mounted) {
      setState(() {});
    }
  }

  void _selectDestination(int index) {
    unawaited(_selectDestinationAfterParking(index));
  }

  Future<int> _selectDestinationFromNavigation(int index) async {
    await _selectDestinationAfterParking(index);
    return _selectedIndex;
  }

  Future<void> _selectDestinationAfterParking(int index) async {
    if (_selectedIndex == index) {
      if (index == 2 || index == 3) {
        _refreshReviewIndexes();
      }
      return;
    }
    if (_practiceRouteInFlight) {
      _showMockNotice('正在恢复上次练习，请稍候');
      return;
    }
    final navigationGeneration = ++_navigationGeneration;
    if (_selectedIndex == 0 && index != 0) {
      final parked =
          await widget.conversationController.prepareToLeaveConversation() &&
          await widget.composerController.prepareToLeave();
      if (!mounted || navigationGeneration != _navigationGeneration) {
        return;
      }
      if (!parked) {
        _showMockNotice('语音正在发送，请完成后再离开');
        return;
      }
    }
    if (_selectedIndex == 1 && index != 1) {
      final launch = widget.preparationLaunchController;
      if (launch?.isStarting ?? false) {
        _showMockNotice('练习正在准备，请完成后再离开训练页');
        return;
      }
      if (launch?.workspaceController.currentLease != null) {
        final parked = await launch!.parkCurrentPractice();
        if (!mounted || navigationGeneration != _navigationGeneration) {
          return;
        }
        if (!parked) {
          _showMockNotice(launch.workspaceErrorMessage ?? '暂时无法安全离开训练页');
          return;
        }
      }
    }
    if (!mounted || navigationGeneration != _navigationGeneration) {
      return;
    }
    unawaited(widget.practiceController.stopPracticeAudio());
    if (index == 2 || index == 3) {
      _refreshReviewIndexes();
    }
    setState(() => _selectedIndex = index);
  }

  void _openJobPreparation() {
    if (widget.jobPreparationController == null) {
      _showMockNotice('岗位准备流程尚未连接');
      return;
    }
    widget.jobPreparationController!.beginNewPreparation();
    Navigator.of(context).pushNamed(AppRoutes.jobPreparation);
  }

  Future<void> _openInterviewPlan(String planId) async {
    final controller = widget.jobPreparationController;
    if (controller == null) {
      _showMockNotice('岗位准备流程尚未连接');
      return;
    }
    final opened = await controller.openSavedPlan(planId);
    if (!mounted) return;
    if (!opened) {
      _showMockNotice(controller.errorMessage ?? '暂时无法打开这场模拟面试。');
      return;
    }
    Navigator.of(context).pushNamed(AppRoutes.jobPreparation);
  }

  void _refreshReviewIndexes() {
    unawaited(widget.reviewHistoryController?.refresh());
  }

  void _showMockNotice(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _checkForUpdate({required bool automatic}) async {
    final service = widget.appUpdateService;
    if (service == null || _updateCheckInFlight) {
      return;
    }
    if (!automatic) {
      _pendingAutomaticUpdate = null;
    }
    setState(() => _updateCheckInFlight = true);
    late final AppUpdateCheckResult result;
    try {
      result = await service.check(automatic: automatic);
    } finally {
      if (mounted) {
        setState(() => _updateCheckInFlight = false);
      }
    }
    if (!mounted) {
      return;
    }
    switch (result.status) {
      case AppUpdateCheckStatus.available:
        final installedVersion = result.installedVersion!;
        final release = result.release!;
        if (automatic) {
          _pendingAutomaticUpdate = (
            installedVersion: installedVersion,
            release: release,
          );
          _schedulePendingUpdatePresentation();
        } else {
          await _presentAvailableUpdate(installedVersion, release);
        }
      case AppUpdateCheckStatus.upToDate:
        if (!automatic) {
          _showMockNotice('已是最新版本 v${result.installedVersion!.version}');
        }
      case AppUpdateCheckStatus.failed:
        if (!automatic) {
          _showMockNotice('暂时无法检查更新，请检查网络后重试');
        }
      case AppUpdateCheckStatus.skipped:
        break;
    }
  }

  void _schedulePendingUpdatePresentation() {
    if (_updatePresentationScheduled || _pendingAutomaticUpdate == null) {
      return;
    }
    _updatePresentationScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _updatePresentationScheduled = false;
      if (!mounted || !_canPresentAutomaticUpdate) {
        return;
      }
      final pending = _pendingAutomaticUpdate;
      if (pending == null) {
        return;
      }
      _pendingAutomaticUpdate = null;
      unawaited(
        _presentAvailableUpdate(pending.installedVersion, pending.release),
      );
    });
  }

  bool get _canPresentAutomaticUpdate {
    return !_updateDialogVisible &&
        canPresentAutomaticAppUpdate(
          routeCurrent: ModalRoute.of(context)?.isCurrent ?? false,
          practiceRouteInFlight: _practiceRouteInFlight,
          conversationBusy: widget.conversationController.isBusy,
          composerActiveWorkflow: widget.composerController.hasActiveWorkflow,
          practiceBusy: widget.practiceController.isBusy,
          practiceRecordingIdle:
              widget.practiceController.recordingState ==
              PracticeRecordingState.idle,
        );
  }

  Future<void> _presentAvailableUpdate(
    InstalledAppVersion installedVersion,
    AppRelease release,
  ) async {
    final service = widget.appUpdateService;
    if (service == null || _updateDialogVisible || !mounted) {
      return;
    }
    _updateDialogVisible = true;
    final openDownload = await showAppUpdateDialog(
      context,
      installedVersion: installedVersion,
      release: release,
    );
    _updateDialogVisible = false;
    if (!mounted || openDownload != true) {
      return;
    }
    if (!await service.openDownload(release) && mounted) {
      _showMockNotice('无法打开下载页面，请稍后重试');
    }
  }

  Future<void> _openPractice() => _openPracticeRoute();

  Widget? _buildAgentClientAction(
    BuildContext context,
    AgentClientAction action,
  ) {
    final practiceAction = tryDecodeConfirmPracticePlanClientAction(action);
    if (practiceAction == null) {
      return null;
    }
    return PracticePlanClientActionCard(
      action: practiceAction,
      onConfirm: () => unawaited(_confirmAgentClientAction(practiceAction)),
    );
  }

  Future<void> _confirmAgentClientAction(
    ConfirmPracticePlanClientAction action,
  ) async {
    final controller = widget.practicePlanClientActionController;
    if (controller == null ||
        _clientActionInFlight ||
        controller.isBusy ||
        widget.conversationController.isBusy) {
      return;
    }
    setState(() => _clientActionInFlight = true);
    try {
      final workspace = controller.workspaceController;
      if (action.practiceExperience == 'IELTS_SPEAKING' &&
          !workspace.hasResumableForPlan(action.practicePlanId)) {
        try {
          final assignment = await controller.loadIELTSPreview(action);
          if (!mounted || !await _showIELTSPlanPreview(assignment)) {
            return;
          }
        } on Object {
          if (mounted) _showMockNotice('暂时无法预览这组雅思题目，请重试。');
          return;
        }
      }
      var replaceCurrentPractice = false;
      if (workspace.hasResumable &&
          !workspace.hasResumableForPlan(action.practicePlanId)) {
        if (!workspace.resumableHasProgress) {
          replaceCurrentPractice = true;
        } else {
          final choice = await _chooseClientActionPracticeAction(
            controller,
            action,
          );
          if (!mounted || choice == null) {
            return;
          }
          if (choice == ExistingPracticeAction.continuePractice) {
            await _openPracticeRoute();
            return;
          }
          replaceCurrentPractice = true;
        }
      }
      var confirmed = await controller.confirm(
        action,
        replaceCurrentPractice: replaceCurrentPractice,
      );
      if (!mounted) {
        return;
      }
      if (!confirmed &&
          !replaceCurrentPractice &&
          controller.failure ==
              PracticePlanClientActionFailure.localExistingPractice) {
        final choice = await _chooseClientActionPracticeAction(
          controller,
          action,
        );
        if (!mounted || choice == null) {
          return;
        }
        if (choice == ExistingPracticeAction.continuePractice) {
          if (controller.workspaceController.hasResumable) {
            await _openPracticeRoute();
          }
          return;
        }
        confirmed = await controller.confirm(
          action,
          replaceCurrentPractice: true,
        );
        if (!mounted) {
          return;
        }
      }
      if (!confirmed) {
        _showMockNotice(controller.errorMessage ?? '练习暂时无法开始，请重试');
        return;
      }
      await _openPracticeRoute();
    } finally {
      if (mounted) {
        setState(() => _clientActionInFlight = false);
      }
    }
  }

  Future<bool> _showIELTSPlanPreview(IeltsPracticeAssignment assignment) async {
    final result = await showModalBottomSheet<bool>(
      context: context,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (_) => _IeltsPlanPreviewSheet(assignment: assignment),
    );
    return result ?? false;
  }

  Future<ExistingPracticeAction?> _chooseClientActionPracticeAction(
    PracticePlanClientActionController controller,
    ConfirmPracticePlanClientAction action,
  ) {
    final scope = action.practiceScope.trim();
    return showExistingPracticeActionSheet(
      context,
      currentTitle: controller.workspaceController.currentTitle,
      nextTitle: scope.isEmpty
          ? action.sceneName
          : '${action.sceneName} · $scope',
    );
  }

  Future<void> _openPracticeRoute() async {
    if (_practiceRouteInFlight) {
      return;
    }
    _practiceRouteInFlight = true;
    final navigationGeneration = _navigationGeneration;
    final selectedIndex = _selectedIndex;
    try {
      final launch = widget.preparationLaunchController;
      if (launch?.hasResumablePractice ?? false) {
        final resumed = await launch!.resumeCurrentPractice();
        final navigationChanged =
            !mounted ||
            navigationGeneration != _navigationGeneration ||
            selectedIndex != _selectedIndex;
        if (navigationChanged) {
          if (resumed) {
            await launch.parkCurrentPractice();
          }
          return;
        }
        if (!resumed) {
          if (mounted) {
            _showMockNotice(launch.workspaceErrorMessage ?? '暂时无法继续上次练习');
          }
          return;
        }
      }
      if (!widget.practiceController.hasActivePractice) {
        _showMockNotice('请先从训练页选择一项练习');
        return;
      }
      if (!mounted) {
        return;
      }
      final result = await Navigator.of(
        context,
      ).pushNamed<Object?>(AppRoutes.practice);
      if (mounted && result is IeltsPracticeRouteResult) {
        setState(() => _selectedIndex = 1);
        widget.ieltsPreparationController?.requestNavigation(
          IeltsPracticeNavigationRequest(
            mode: result.mode,
            selection: result.selection,
          ),
        );
      } else if (mounted &&
          result == CompletedPracticeRouteResult.returnToConversation) {
        setState(() => _selectedIndex = 0);
      } else if (mounted &&
          result == CompletedPracticeRouteResult.returnToTraining) {
        setState(() => _selectedIndex = 1);
      }
    } finally {
      _practiceRouteInFlight = false;
      _schedulePendingUpdatePresentation();
    }
  }

  void _handleAgentInteractionState() {
    if (!mounted) {
      return;
    }
    setState(() {});
    _schedulePendingUpdatePresentation();
  }

  void _handlePracticeState() {
    if (!mounted) {
      return;
    }
    setState(() {});
    _schedulePendingUpdatePresentation();
  }

  @override
  Widget build(BuildContext context) {
    const practiceAvailable = true;
    final canContinuePractice =
        widget.practiceController.hasActivePractice ||
        (widget.preparationLaunchController?.hasResumablePractice ?? false);
    final practiceSelected = _selectedIndex == 1;
    final safeBottom = math.max(
      MediaQuery.viewPaddingOf(context).bottom,
      PlatformNavigationBar.minimumBottomInset,
    );
    final composerBottomInset =
        PlatformNavigationBar.heightFor(context) + safeBottom + 10;
    final pages = [
      ConversationPage(
        previewMode: widget.previewMode,
        practiceAvailable: practiceAvailable,
        restingComposerBottom: composerBottomInset,
        threadId: widget.conversationController.threadId,
        displayName: widget.authController?.profile?.displayName,
        onOpenMenu: () {
          _scaffoldKey.currentState?.openDrawer();
          if (!widget.previewMode) {
            unawaited(widget.conversationController.refreshThreadHistory());
          }
        },
        onNavigateBack: widget.showBackButton
            ? () => Navigator.of(context).maybePop()
            : null,
        onCreatePlan: () => unawaited(
          widget.composerController.sendText('我想创建一场模拟面试，请先帮我梳理面试信息。'),
        ),
        onBrowseScenes: () => _selectDestination(1),
        onContinuePractice: canContinuePractice ? _openPractice : null,
        onOpenReview: () => _selectDestination(2),
        clientActionBuilder: _buildAgentClientAction,
        onStartVoice: widget.composerController.supportsAgentVoice
            ? () async {
                await widget.messageAudioController?.stopPlayback();
                await widget.composerController.startAgentVoiceRecording();
              }
            : null,
        voiceController: widget.composerController.voiceController,
        messageAudioController: widget.messageAudioController,
        onTranslateMessage: widget.messageTranslationClient == null
            ? null
            : (message) async {
                final translation = await widget.messageTranslationClient!
                    .translateMessage(messageId: message.id);
                return translation.content;
              },
        pendingImages: widget.composerController.pendingImages,
        imageErrorMessage: widget.composerController.imageErrorMessage,
        imageSelectionInFlight:
            widget.composerController.isImageSelectionInFlight,
        onPickImages: widget.composerController.supportsAgentImages
            ? widget.composerController.pickAgentImages
            : null,
        onTakePhoto: widget.composerController.supportsAgentImages
            ? widget.composerController.takeAgentPhoto
            : null,
        onRemovePendingImage: widget.composerController.removePendingImage,
        onRetryPendingImage: widget.composerController.retryPendingImage,
        onRefreshMessageImage:
            widget.conversationController.refreshMessageImage,
        onCreateConversation: () =>
            unawaited(widget.conversationController.createThread()),
        draftThreadRecoveryGeneration:
            widget.conversationController.draftThreadRecoveryGeneration,
        messages: widget.conversationController.messages,
        activeSceneName: widget.practiceController.scene?.name,
        hasFocusedThread:
            !widget.conversationController.isInitialized ||
            widget.conversationController.threadId != null,
        hasEarlierMessages: widget.conversationController.hasEarlierMessages,
        isLoadingEarlierMessages:
            widget.conversationController.isLoadingEarlierMessages,
        isBusy: widget.conversationController.isBusy,
        isRestoring: widget.conversationController.isRestoring,
        isReplyPending: widget.conversationController.isReplyPending,
        isComposerBlocked: widget.conversationController.isComposerBlocked,
        errorMessage:
            widget.conversationController.errorMessage ??
            widget.conversationController.threadHistoryErrorMessage,
        retryOperationLabel: widget.conversationController.canRetry
            ? widget.conversationController.retryActionLabel
            : '重试',
        onSubmitText: widget.composerController.sendText,
        onRetryOperation: widget.conversationController.canRetry
            ? widget.conversationController.retryLastOperation
            : widget.conversationController.canRetryThreadHistory &&
                  !widget.conversationController.isBusy
            ? widget.conversationController.retryThreadHistory
            : null,
        onLoadEarlierMessages: widget.conversationController.hasEarlierMessages
            ? widget.conversationController.loadEarlierMessages
            : null,
        feedbackPresenter: _feedbackPresenter,
      ),
      PreparationPage(
        showBackButton: false,
        previewMode: widget.previewMode,
        practiceController: widget.practiceController,
        preparationController: widget.preparationController,
        ieltsController: widget.ieltsPreparationController,
        launchController: widget.preparationLaunchController,
        jobPreparationController: widget.jobPreparationController,
        onOpenJobPreparation: widget.jobPreparationController == null
            ? null
            : _openJobPreparation,
        onOpenInterviewPlan: widget.jobPreparationController == null
            ? null
            : (planId) => unawaited(_openInterviewPlan(planId)),
        onSceneSelected: () => _selectDestination(0),
        onPracticeStarted: _openPractice,
      ),
      ReviewPage(
        showBackButton: false,
        previewMode: widget.previewMode,
        practiceAvailable: practiceAvailable,
        historyController: widget.reviewHistoryController,
        autoload: false,
      ),
      ProfilePage(
        showBackButton: false,
        user: widget.user,
        profile: widget.authController?.profile,
        profileErrorMessage: widget.authController?.profileErrorMessage,
        profileSaving: widget.authController?.profileSaving ?? false,
        onSaveDisplayName: widget.authController?.updateDisplayName,
        avatarBytes: widget.authController?.avatarBytes,
        avatarSaving: widget.authController?.avatarSaving ?? false,
        onUploadAvatar: widget.authController?.updateAvatar,
        onUseDefaultAvatar: widget.authController?.useDefaultAvatar,
        onLogout: widget.authController?.logout,
        reviewHistoryController: widget.reviewHistoryController,
        coachingProfileController: widget.coachingProfileController,
        appUpdateService: widget.appUpdateService,
        updateCheckInProgress: _updateCheckInFlight,
        onCheckForUpdate: widget.appUpdateService == null
            ? null
            : () => _checkForUpdate(automatic: false),
      ),
    ];

    return Stack(
      children: [
        Scaffold(
          key: _scaffoldKey,
          extendBody: !practiceSelected,
          resizeToAvoidBottomInset: false,
          backgroundColor: Colors.transparent,
          drawer: _ConversationDrawer(
            controller: widget.conversationController,
            avatarBytes: widget.authController?.avatarBytes,
            onOpenProfile: () => _selectDestination(3),
          ),
          onDrawerChanged: (open) {
            if (_conversationDrawerOpen != open) {
              setState(() => _conversationDrawerOpen = open);
            }
          },
          drawerScrimColor: const Color(0x52000000),
          body: IndexedStack(index: _selectedIndex, children: pages),
          bottomNavigationBar: _conversationDrawerOpen
              ? null
              : PlatformNavigationBar(
                  destinations: _destinations,
                  selectedIndex: _selectedIndex,
                  onDestinationSelected: _selectDestinationFromNavigation,
                ),
        ),
        if (_clientActionInFlight)
          const Positioned.fill(child: _PracticeTransitionOverlay()),
      ],
    );
  }

  AgentConversationFeedbackPresenter? _createFeedbackPresenter() {
    final controller = widget.speechFeedbackController;
    return controller == null
        ? null
        : AgentConversationFeedbackPresenter(controller: controller);
  }
}

class _IeltsPlanPreviewSheet extends StatelessWidget {
  const _IeltsPlanPreviewSheet({required this.assignment});

  final IeltsPracticeAssignment assignment;

  @override
  Widget build(BuildContext context) => DraggableScrollableSheet(
    expand: false,
    initialChildSize: 0.82,
    minChildSize: 0.55,
    maxChildSize: 0.94,
    builder: (context, scrollController) => Padding(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Expanded(
                child: Text('开始前预览', style: SpeakUpDesign.sectionTitle),
              ),
              IconButton(
                tooltip: '关闭',
                onPressed: () => Navigator.of(context).pop(false),
                icon: const Icon(Icons.close_rounded),
              ),
            ],
          ),
          Text(
            'IELTS Speaking · ${assignment.mode.wireValue.replaceAll('_', ' ')}',
            style: SpeakUpDesign.body.copyWith(color: SpeakUpDesign.secondary),
          ),
          const SizedBox(height: 14),
          Expanded(
            child: ListView.separated(
              key: const Key('agent-ielts-plan-preview'),
              controller: scrollController,
              itemCount: assignment.parts.length,
              separatorBuilder: (_, _) => const SizedBox(height: 18),
              itemBuilder: (context, index) =>
                  _IeltsPreviewPart(part: assignment.parts[index]),
            ),
          ),
          const SizedBox(height: 14),
          FilledButton.icon(
            key: const Key('agent-ielts-preview-start'),
            onPressed: () => Navigator.of(context).pop(true),
            icon: const Icon(Icons.play_arrow_rounded),
            label: const Text('开始练习'),
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(52),
              backgroundColor: SpeakUpDesign.ink,
              foregroundColor: Colors.white,
            ),
          ),
        ],
      ),
    ),
  );
}

class _IeltsPreviewPart extends StatelessWidget {
  const _IeltsPreviewPart({required this.part});

  final IeltsPracticePartAssignment part;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        part.part.wireValue.replaceAll('_', ' '),
        style: SpeakUpDesign.cardTitle,
      ),
      if (part.topicTitle case final title?) ...[
        const SizedBox(height: 4),
        Text(title, style: SpeakUpDesign.body),
      ],
      if (part.cueCard case final cueCard?) ...[
        const SizedBox(height: 10),
        DecoratedBox(
          decoration: BoxDecoration(
            color: SpeakUpDesign.surfaceMuted,
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
          ),
          child: Padding(
            padding: const EdgeInsets.all(14),
            child: Text(cueCard, style: SpeakUpDesign.body),
          ),
        ),
      ],
      if (part.turnBlueprints.isNotEmpty) ...[
        const SizedBox(height: 10),
        for (var index = 0; index < part.turnBlueprints.length; index++)
          Padding(
            padding: const EdgeInsets.only(bottom: 9),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: 26,
                  child: Text(
                    '${index + 1}.',
                    style: SpeakUpDesign.meta.copyWith(
                      color: SpeakUpDesign.secondary,
                    ),
                  ),
                ),
                Expanded(
                  child: Text(
                    part.turnBlueprints[index],
                    style: SpeakUpDesign.body,
                  ),
                ),
              ],
            ),
          ),
      ],
    ],
  );
}

class _PracticeTransitionOverlay extends StatelessWidget {
  const _PracticeTransitionOverlay();

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      key: const Key('agent-practice-transition-overlay'),
      color: SpeakUpDesign.canvas,
      child: SafeArea(
        child: Center(
          child: Semantics(
            label: '正在进入练习',
            liveRegion: true,
            child: const Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.play_circle_outline_rounded, size: 34),
                SizedBox(height: 16),
                Text('正在进入练习…', style: SpeakUpDesign.body),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ConversationDrawer extends StatelessWidget {
  const _ConversationDrawer({
    required this.controller,
    required this.onOpenProfile,
    this.avatarBytes,
  });

  final ConversationController controller;
  final VoidCallback onOpenProfile;
  final Uint8List? avatarBytes;

  @override
  Widget build(BuildContext context) {
    final current = controller.currentThreadSummary;
    final currentThreadId = controller.threadId;
    final recentThreads = <AgentThreadSummary>[
      for (final thread in controller.threads)
        if (thread.id != currentThreadId) thread,
    ];
    final busy = controller.isBusy;
    final threadWidgets = <Widget>[
      if (currentThreadId == null)
        _ConversationThreadTile(
          key: const Key('no-focused-conversation'),
          title: '新对话 · 未发送',
          selected: true,
          enabled: !busy,
          onTap: () => Navigator.of(context).pop(),
        )
      else
        _ConversationThreadTile(
          key: Key('conversation-thread-$currentThreadId'),
          title: current?.title ?? '新对话',
          selected: true,
          enabled: !busy,
          onTap: () => Navigator.of(context).pop(),
          onDelete: () =>
              _confirmDelete(context, currentThreadId, current?.title),
        ),
      for (final thread in recentThreads)
        _ConversationThreadTile(
          key: Key('conversation-thread-${thread.id}'),
          title: thread.title ?? '新对话',
          selected: false,
          enabled: !busy,
          onTap: () async {
            final selected = await controller.selectThread(thread.id);
            if (!context.mounted || !selected) {
              return;
            }
            Navigator.of(context).pop();
          },
          onDelete: () => _confirmDelete(context, thread.id, thread.title),
        ),
    ];

    return Drawer(
      width: 300,
      backgroundColor: SpeakUpDesign.canvas,
      child: SafeArea(
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 8, 8),
              child: Row(
                children: [
                  const Expanded(
                    child: Align(
                      alignment: Alignment.centerLeft,
                      child: SpeakUpWordmark(height: 30),
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭对话菜单',
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            Expanded(
              child: Stack(
                children: [
                  Positioned.fill(
                    child: ListView(
                      padding: const EdgeInsets.fromLTRB(16, 8, 16, 88),
                      children: [
                        if (controller.threadHistoryErrorMessage
                            case final message?)
                          Padding(
                            padding: const EdgeInsets.only(bottom: 10),
                            child: Text(
                              message,
                              key: const Key('conversation-history-error'),
                              style: SpeakUpDesign.meta.copyWith(
                                color: SpeakUpDesign.error,
                              ),
                            ),
                          ),
                        if (threadWidgets.isEmpty)
                          const Padding(
                            padding: EdgeInsets.symmetric(
                              horizontal: 12,
                              vertical: 10,
                            ),
                            child: Text('暂无聊天', style: SpeakUpDesign.body),
                          )
                        else
                          ...threadWidgets,
                        if (controller.hasMoreThreads) ...[
                          const SizedBox(height: 8),
                          TextButton(
                            key: const Key('load-more-conversations'),
                            onPressed: controller.isLoadingMoreThreads || busy
                                ? null
                                : controller.loadMoreThreads,
                            child: Text(
                              controller.isLoadingMoreThreads
                                  ? '正在加载…'
                                  : '加载更早',
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  Positioned(
                    left: 16,
                    right: 16,
                    bottom: 16,
                    child: Row(
                      children: [
                        FilledButton.icon(
                          key: const Key('new-conversation-button'),
                          onPressed: busy
                              ? null
                              : () async {
                                  final created = await controller
                                      .createThread();
                                  if (!context.mounted || !created) {
                                    return;
                                  }
                                  Navigator.of(context).pop();
                                },
                          icon: const Icon(Icons.edit_outlined, size: 22),
                          label: const Text('聊天'),
                          style: FilledButton.styleFrom(
                            minimumSize: const Size(0, 48),
                            padding: const EdgeInsets.symmetric(horizontal: 20),
                            backgroundColor: SpeakUpDesign.primary,
                            foregroundColor: SpeakUpDesign.canvas,
                            shape: const StadiumBorder(),
                            textStyle: SpeakUpDesign.cardTitle,
                          ),
                        ),
                        const Spacer(),
                        IconButton(
                          key: const Key('drawer-profile-button'),
                          tooltip: '打开我的页面',
                          onPressed: () {
                            Navigator.of(context).pop();
                            onOpenProfile();
                          },
                          padding: EdgeInsets.zero,
                          constraints: const BoxConstraints.tightFor(
                            width: 48,
                            height: 48,
                          ),
                          icon: ProfileAvatarView(
                            size: 48,
                            avatarBytes: avatarBytes,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _confirmDelete(
    BuildContext context,
    String threadId,
    String? title,
  ) async {
    final displayTitle = title ?? '新对话';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除对话？'),
        content: Text('“$displayTitle”将从对话列表中删除，相关练习与复盘记录会保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const Key('confirm-delete-conversation'),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true || !context.mounted) {
      return;
    }
    await controller.deleteThread(threadId);
  }
}

class _ConversationThreadTile extends StatelessWidget {
  const _ConversationThreadTile({
    super.key,
    required this.title,
    required this.selected,
    required this.enabled,
    required this.onTap,
    this.onDelete,
  });

  final String title;
  final bool selected;
  final bool enabled;
  final VoidCallback onTap;
  final VoidCallback? onDelete;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      selected: selected,
      button: true,
      label: selected ? '当前对话：$title' : title,
      hint: onDelete == null ? null : '长按删除对话',
      onTap: enabled ? onTap : null,
      onLongPress: enabled ? onDelete : null,
      excludeSemantics: true,
      child: Material(
        color: selected ? SpeakUpDesign.primaryMuted : Colors.transparent,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        child: InkWell(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          onTap: enabled ? onTap : null,
          onLongPress: enabled ? onDelete : null,
          child: Container(
            height: 44,
            padding: const EdgeInsets.symmetric(horizontal: 12),
            alignment: Alignment.centerLeft,
            child: Text(
              title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: selected
                  ? SpeakUpDesign.body.copyWith(
                      color: SpeakUpDesign.ink,
                      fontWeight: FontWeight.w600,
                    )
                  : SpeakUpDesign.body,
            ),
          ),
        ),
      ),
    );
  }
}
