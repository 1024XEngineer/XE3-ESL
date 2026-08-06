import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/glass_navigation_bar.dart';
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
    GlassNavigationDestination(
      label: 'SpeakUp',
      icon: Icons.chat_bubble_outline_rounded,
      key: Key('primary-tab-agent'),
    ),
    GlassNavigationDestination(
      label: '训练',
      icon: Icons.grid_view_rounded,
      key: Key('primary-tab-scenes'),
    ),
    GlassNavigationDestination(
      label: '复盘',
      icon: Icons.fact_check_outlined,
      key: Key('primary-tab-review'),
    ),
    GlassNavigationDestination(
      label: '我的',
      icon: Icons.person_rounded,
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
      GlassNavigationBar.minimumBottomInset,
    );
    final composerBottomInset =
        GlassNavigationBar.heightFor(context) + safeBottom + 10;
    final pages = [
      ConversationPage(
        previewMode: widget.previewMode,
        practiceAvailable: practiceAvailable,
        restingComposerBottom: composerBottomInset,
        threadId: widget.conversationController.threadId,
        displayName: widget.authController?.profile?.displayName,
        onOpenMenu: () => _scaffoldKey.currentState?.openDrawer(),
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
            ? widget.composerController.startAgentVoiceRecording
            : null,
        voiceController: widget.composerController.voiceController,
        messageAudioController: widget.messageAudioController,
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
        showBackButton: widget.showBackButton,
        previewMode: widget.previewMode,
        practiceController: widget.practiceController,
        preparationController: widget.preparationController,
        ieltsController: widget.ieltsPreparationController,
        launchController: widget.preparationLaunchController,
        onOpenJobPreparation: widget.jobPreparationController == null
            ? null
            : _openJobPreparation,
        onSceneSelected: () => _selectDestination(0),
        onPracticeStarted: _openPractice,
      ),
      ReviewPage(
        showBackButton: widget.showBackButton,
        onExit: widget.showBackButton ? null : () => _selectDestination(0),
        previewMode: widget.previewMode,
        practiceAvailable: practiceAvailable,
        historyController: widget.reviewHistoryController,
        ieltsSpeakingReportController: widget.ieltsSpeakingReportController,
        autoload: false,
      ),
      _ProfilePage(
        showBackButton: widget.showBackButton,
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
        previewMode: widget.previewMode,
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
      bottomNavigationBar: GlassNavigationBar(
        destinations: _destinations,
        selectedIndex: _selectedIndex,
        onDestinationSelected: _selectDestination,
        solid: practiceSelected,
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
    required this.previewMode,
    required this.controller,
    this.hiddenThreadIds = const <String>{},
  });

  final bool previewMode;
  final ConversationController controller;
  final Set<String> hiddenThreadIds;

  @override
  Widget build(BuildContext context) {
    final current = controller.currentThreadSummary;
    final currentThreadId = controller.threadId;
    final recentThreads = <AgentThreadSummary>[
      for (final thread in controller.threads)
        if (thread.id != currentThreadId &&
            !hiddenThreadIds.contains(thread.id))
          thread,
    ];
    return Drawer(
      width: 300,
      backgroundColor: SpeakUpDesign.canvas,
      child: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 20),
          children: [
            Row(
              children: [
                const Expanded(
                  child: Text('SpeakUp', style: SpeakUpDesign.sectionTitle),
                ),
                IconButton(
                  tooltip: '关闭对话菜单',
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close_rounded),
                ),
              ],
            ),
            Text(
              previewMode ? '本地 Fake 预览，未连接正式账号' : '已连接当前账号',
              style: SpeakUpDesign.meta,
            ),
            const SizedBox(height: 20),
            FilledButton.tonalIcon(
              key: const Key('new-conversation-button'),
              onPressed: controller.isBusy
                  ? null
                  : () async {
                      final created = await controller.createThread();
                      if (!context.mounted || !created) {
                        return;
                      }
                      Navigator.of(context).pop();
                    },
              icon: const Icon(Icons.add_rounded),
              label: const Text('新对话'),
              style: FilledButton.styleFrom(
                alignment: Alignment.centerLeft,
                minimumSize: const Size.fromHeight(48),
                backgroundColor: SpeakUpDesign.primaryMuted,
                foregroundColor: SpeakUpDesign.primary,
              ),
            ),
            if (controller.isBusy) ...[
              const SizedBox(height: 12),
              const LinearProgressIndicator(
                key: Key('conversation-drawer-progress'),
                minHeight: 2,
              ),
            ],
            const SizedBox(height: 28),
            const Text('当前对话', style: SpeakUpDesign.label),
            const SizedBox(height: 8),
            if (currentThreadId == null)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8, vertical: 10),
                child: Text(
                  '新对话 · 未发送',
                  key: Key('no-focused-conversation'),
                  style: SpeakUpDesign.body,
                ),
              )
            else
              _ConversationThreadTile(
                threadId: currentThreadId,
                title: current?.title,
                updatedAt: current?.updatedAt,
                selected: true,
                enabled: !controller.isBusy,
                onTap: () => Navigator.of(context).pop(),
                onDelete: () =>
                    _confirmDelete(context, currentThreadId, current?.title),
              ),
            const SizedBox(height: 24),
            const Text('近期对话', style: SpeakUpDesign.label),
            const SizedBox(height: 8),
            if (recentThreads.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8, vertical: 10),
                child: Text(
                  '暂无其他对话',
                  key: Key('no-recent-conversations'),
                  style: SpeakUpDesign.body,
                ),
              )
            else
              for (final thread in recentThreads)
                _ConversationThreadTile(
                  threadId: thread.id,
                  title: thread.title,
                  updatedAt: thread.updatedAt,
                  selected: false,
                  enabled: !controller.isBusy,
                  onTap: () async {
                    final selected = await controller.selectThread(thread.id);
                    if (!context.mounted || !selected) {
                      return;
                    }
                    Navigator.of(context).pop();
                  },
                  onDelete: () =>
                      _confirmDelete(context, thread.id, thread.title),
                ),
            if (controller.threadHistoryErrorMessage case final message?) ...[
              const SizedBox(height: 10),
              Text(
                message,
                key: const Key('conversation-history-error'),
                style: SpeakUpDesign.meta.copyWith(color: SpeakUpDesign.error),
              ),
            ],
            if (controller.hasMoreThreads) ...[
              const SizedBox(height: 10),
              TextButton(
                key: const Key('load-more-conversations'),
                onPressed: controller.isLoadingMoreThreads || controller.isBusy
                    ? null
                    : controller.loadMoreThreads,
                child: Text(controller.isLoadingMoreThreads ? '正在加载…' : '加载更早'),
              ),
            ],
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
    required this.threadId,
    required this.title,
    required this.updatedAt,
    required this.selected,
    required this.enabled,
    required this.onTap,
    required this.onDelete,
  });

  final String threadId;
  final String? title;
  final DateTime? updatedAt;
  final bool selected;
  final bool enabled;
  final VoidCallback onTap;
  final VoidCallback? onDelete;

  @override
  Widget build(BuildContext context) {
    final lastUpdatedAt = updatedAt;
    final displayTitle = title ?? '新对话';
    return Semantics(
      selected: selected,
      button: true,
      label: selected ? '当前对话：$displayTitle' : displayTitle,
      child: ListTile(
        key: Key('conversation-thread-$threadId'),
        contentPadding: const EdgeInsets.symmetric(horizontal: 8),
        selected: selected,
        selectedTileColor: SpeakUpDesign.primaryMuted,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        ),
        leading: const Icon(Icons.chat_bubble_outline_rounded),
        title: Text(displayTitle, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: lastUpdatedAt == null
            ? null
            : Text('更新于 ${_formatThreadUpdatedAt(lastUpdatedAt)}'),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (selected)
              const Icon(
                Icons.check_rounded,
                key: Key('focused-conversation-indicator'),
                size: 20,
              ),
            IconButton(
              key: Key('delete-conversation-$threadId'),
              tooltip: '删除对话',
              onPressed: enabled ? onDelete : null,
              icon: const Icon(Icons.delete_outline_rounded, size: 20),
            ),
          ],
        ),
        onTap: enabled ? onTap : null,
      ),
    );
  }
}

String _formatThreadUpdatedAt(DateTime value) {
  final local = value.toLocal();
  String twoDigits(int part) => part.toString().padLeft(2, '0');
  return '${twoDigits(local.month)}月${twoDigits(local.day)}日 '
      '${twoDigits(local.hour)}:${twoDigits(local.minute)}';
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
            const SpeakUpPageHeader(title: '我的', subtitle: '管理账号与练习身份。'),
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
