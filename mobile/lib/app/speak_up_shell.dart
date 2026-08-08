import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/platform_navigation_bar.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_handoff_controller.dart';
import 'package:speakup/features/agent/conversation/conversation.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_practice.dart';
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
import 'package:speakup/features/coaching/review/interview_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/agent_conversation_feedback_presenter.dart';
import 'package:speakup/resume/resume.dart';

class SpeakUpShell extends StatefulWidget {
  const SpeakUpShell({
    this.showBackButton = false,
    this.previewMode = false,
    this.user,
    this.authController,
    this.preparationController,
    this.ieltsPreparationController,
    this.preparationLaunchController,
    this.practicePlanHandoffController,
    this.jobPreparationController,
    this.reviewHistoryController,
    this.interviewReportController,
    this.ieltsSpeakingReportController,
    this.speechFeedbackController,
    this.resumeController,
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
  final PracticePlanHandoffController? practicePlanHandoffController;
  final JobPreparationController? jobPreparationController;
  final ReviewHistoryController? reviewHistoryController;
  final InterviewReportController? interviewReportController;
  final IeltsSpeakingReportController? ieltsSpeakingReportController;
  final SpeechFeedbackController? speechFeedbackController;
  final ResumeController? resumeController;

  @override
  State<SpeakUpShell> createState() => _SpeakUpShellState();
}

class _SpeakUpShellState extends State<SpeakUpShell> {
  static const _destinations = [
    PlatformNavigationDestination(
      label: 'SpeakUp',
      icon: Icons.chat_bubble_outline_rounded,
      selectedIcon: Icons.chat_bubble_rounded,
      iosSystemImage: 'bubble.left',
      iosSelectedSystemImage: 'bubble.left.fill',
      key: Key('primary-tab-agent'),
    ),
    PlatformNavigationDestination(
      label: '训练',
      icon: Icons.grid_view_rounded,
      selectedIcon: Icons.dashboard_rounded,
      iosSystemImage: 'square.grid.2x2',
      iosSelectedSystemImage: 'square.grid.2x2.fill',
      key: Key('primary-tab-scenes'),
    ),
    PlatformNavigationDestination(
      label: '复盘',
      icon: Icons.fact_check_outlined,
      selectedIcon: Icons.fact_check_rounded,
      iosSystemImage: 'checklist',
      iosSelectedSystemImage: 'checkmark.square.fill',
      key: Key('primary-tab-review'),
    ),
    PlatformNavigationDestination(
      label: '我的',
      icon: Icons.person_outline_rounded,
      selectedIcon: Icons.person_rounded,
      iosSystemImage: 'person',
      iosSelectedSystemImage: 'person.fill',
      key: Key('primary-tab-profile'),
    ),
  ];

  final _scaffoldKey = GlobalKey<ScaffoldState>();
  int _selectedIndex = 0;
  AgentConversationFeedbackPresenter? _feedbackPresenter;
  bool _practiceRouteInFlight = false;
  int _navigationGeneration = 0;

  @override
  void initState() {
    super.initState();
    widget.conversationController.addListener(_handleAgentInteractionState);
    widget.composerController.addListener(_handleAgentInteractionState);
    widget.practiceController.addListener(_handlePracticeState);
    _feedbackPresenter = _createFeedbackPresenter();
  }

  @override
  void didUpdateWidget(covariant SpeakUpShell oldWidget) {
    super.didUpdateWidget(oldWidget);
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
  }

  @override
  void dispose() {
    widget.conversationController.removeListener(_handleAgentInteractionState);
    widget.composerController.removeListener(_handleAgentInteractionState);
    widget.practiceController.removeListener(_handlePracticeState);
    _feedbackPresenter?.dispose();
    super.dispose();
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
      if (index == 2) {
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
      if (launch?.workspaceController?.currentLease != null) {
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
    if (index == 2) {
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
    if (controller == null ||
        !await controller.openSavedPlan(planId) ||
        !mounted) {
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

  void _openPractice() {
    unawaited(_openPracticeRoute());
  }

  Future<void> _confirmAgentHandoff(AgentHandoff handoff) async {
    final controller = widget.practicePlanHandoffController;
    if (handoff is! ConfirmPracticePlanHandoff ||
        controller == null ||
        controller.isBusy ||
        widget.conversationController.isBusy) {
      return;
    }
    final confirmed = await controller.confirm(handoff);
    if (!mounted) {
      return;
    }
    if (!confirmed) {
      _showMockNotice(controller.errorMessage ?? '练习暂时无法开始，请重试');
      return;
    }
    await _openPracticeRoute();
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
          result == CompletedPracticeRouteResult.continueWithAgent) {
        setState(() => _selectedIndex = 0);
      }
    } finally {
      _practiceRouteInFlight = false;
    }
  }

  void _handleAgentInteractionState() {
    if (!mounted) {
      return;
    }
    setState(() {});
  }

  void _handlePracticeState() {
    if (!mounted) {
      return;
    }
    setState(() {});
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
        onMessageHandoff: (handoff) => unawaited(_confirmAgentHandoff(handoff)),
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
        errorMessage:
            widget.conversationController.errorMessage ??
            widget.conversationController.threadHistoryErrorMessage,
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
        ieltsSpeakingReportController: widget.ieltsSpeakingReportController,
        autoload: false,
      ),
      _ProfilePage(
        showBackButton: false,
        user: widget.user,
        profile: widget.authController?.profile,
        profileErrorMessage: widget.authController?.profileErrorMessage,
        profileSaving: widget.authController?.profileSaving ?? false,
        onSaveDisplayName: widget.authController?.updateDisplayName,
        onLogout: widget.authController?.logout,
        resumeController: widget.resumeController,
      ),
    ];

    return Scaffold(
      key: _scaffoldKey,
      extendBody: !practiceSelected,
      resizeToAvoidBottomInset: false,
      backgroundColor: practiceSelected
          ? SpeakUpDesign.canvas
          : Colors.transparent,
      drawer: _ConversationDrawer(
        controller: widget.conversationController,
        hiddenThreadIds: {
          ?widget
              .preparationLaunchController
              ?.workspaceController
              ?.currentPracticeThreadId,
        },
      ),
      drawerScrimColor: const Color(0x52000000),
      body: IndexedStack(index: _selectedIndex, children: pages),
      bottomNavigationBar: PlatformNavigationBar(
        destinations: _destinations,
        selectedIndex: _selectedIndex,
        onDestinationSelected: _selectDestinationFromNavigation,
      ),
    );
  }

  AgentConversationFeedbackPresenter? _createFeedbackPresenter() {
    final controller = widget.speechFeedbackController;
    return controller == null
        ? null
        : AgentConversationFeedbackPresenter(controller: controller);
  }
}

class _ConversationDrawer extends StatelessWidget {
  const _ConversationDrawer({
    required this.controller,
    this.hiddenThreadIds = const <String>{},
  });

  final ConversationController controller;
  final Set<String> hiddenThreadIds;

  @override
  Widget build(BuildContext context) {
    final current = controller.currentThreadSummary;
    final currentThreadId = controller.threadId;
    final recentThreads = <AgentThreadSummary>[
      for (final thread in controller.threads)
        if (thread.id != currentThreadId &&
            thread.activeGoalId == null &&
            !hiddenThreadIds.contains(thread.id))
          thread,
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
      else if (current?.activeGoalId == null)
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
                    bottom: 16,
                    child: FilledButton.icon(
                      key: const Key('new-conversation-button'),
                      onPressed: busy
                          ? null
                          : () async {
                              final created = await controller.createThread();
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

class _ProfilePage extends StatelessWidget {
  const _ProfilePage({
    required this.showBackButton,
    required this.user,
    required this.profile,
    required this.profileErrorMessage,
    required this.profileSaving,
    required this.onSaveDisplayName,
    required this.onLogout,
    required this.resumeController,
  });

  final bool showBackButton;
  final User? user;
  final UserProfile? profile;
  final String? profileErrorMessage;
  final bool profileSaving;
  final Future<String?> Function(String)? onSaveDisplayName;
  final VoidCallback? onLogout;
  final ResumeController? resumeController;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('profile-page'),
      appBar: showBackButton
          ? AppBar(
              leading: IconButton(
                key: const Key('profile-route-back-button'),
                tooltip: '返回',
                onPressed: () => Navigator.of(context).maybePop(),
                icon: const Icon(Icons.arrow_back_rounded),
              ),
            )
          : null,
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: EdgeInsets.fromLTRB(
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space24,
            SpeakUpDesign.horizontalInset(context),
            140,
          ),
          children: [
            const SpeakUpDisplayTitle(
              key: Key('profile-page-title'),
              title: 'Profile',
              semanticLabel: '我的',
            ),
            const SizedBox(height: SpeakUpDesign.space24),
            Card(
              child: ListTile(
                contentPadding: const EdgeInsets.symmetric(
                  horizontal: SpeakUpDesign.space16,
                  vertical: SpeakUpDesign.space8,
                ),
                leading: CircleAvatar(
                  backgroundColor: SpeakUpDesign.primaryMuted,
                  foregroundColor: SpeakUpDesign.primary,
                  child: Text(
                    _profileInitial(profile?.displayName),
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                ),
                title: Text(
                  profile?.displayName ?? (user == null ? '本地界面预览' : '尚未设置昵称'),
                ),
                subtitle: Text(user?.email ?? '尚未连接正式账号'),
                trailing: user == null
                    ? null
                    : IconButton(
                        key: const Key('profile-edit-display-name'),
                        tooltip: '编辑昵称',
                        onPressed: profileSaving || onSaveDisplayName == null
                            ? null
                            : () => _editDisplayName(context),
                        icon: const Icon(Icons.edit_rounded),
                      ),
              ),
            ),
            if (profileErrorMessage != null) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              Text(
                profileErrorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space16),
            if (resumeController != null) ...[
              ResumeSummaryCard(controller: resumeController!),
              const SizedBox(height: SpeakUpDesign.space16),
            ],
            OutlinedButton.icon(
              key: const Key('profile-logout-button'),
              onPressed: onLogout,
              icon: const Icon(Icons.logout_rounded),
              label: Text(user == null ? '预览模式不可退出' : '退出登录'),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _editDisplayName(BuildContext context) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (_) => _DisplayNameDialog(
        initialName: profile?.displayName ?? '',
        onSave: onSaveDisplayName!,
      ),
    );
    if (saved == true && context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('昵称已更新')));
    }
  }
}

class _DisplayNameDialog extends StatefulWidget {
  const _DisplayNameDialog({required this.initialName, required this.onSave});

  final String initialName;
  final Future<String?> Function(String) onSave;

  @override
  State<_DisplayNameDialog> createState() => _DisplayNameDialogState();
}

class _DisplayNameDialogState extends State<_DisplayNameDialog> {
  late final TextEditingController _controller;
  String? _errorMessage;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.initialName);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('编辑昵称'),
      content: TextField(
        key: const Key('profile-display-name-input'),
        controller: _controller,
        autofocus: true,
        enabled: !_saving,
        maxLength: 40,
        decoration: InputDecoration(labelText: '昵称', errorText: _errorMessage),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(false),
          child: const Text('取消'),
        ),
        FilledButton(
          key: const Key('profile-save-display-name'),
          onPressed: _saving ? null : _save,
          child: Text(_saving ? '正在保存…' : '保存'),
        ),
      ],
    );
  }

  Future<void> _save() async {
    final value = _controller.text.trim();
    if (value.isEmpty) {
      setState(() => _errorMessage = '请输入昵称');
      return;
    }
    setState(() {
      _saving = true;
      _errorMessage = null;
    });
    final error = await widget.onSave(value);
    if (!mounted) {
      return;
    }
    if (error != null) {
      setState(() {
        _saving = false;
        _errorMessage = error;
      });
      return;
    }
    Navigator.of(context).pop(true);
  }
}

String _profileInitial(String? displayName) {
  if (displayName == null || displayName.isEmpty) {
    return '我';
  }
  return String.fromCharCode(displayName.runes.first).toUpperCase();
}
