import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/glass_navigation_bar.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/preparation/job_preparation_controller.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/review/review_history_controller.dart';

class SpeakUpShell extends StatefulWidget {
  const SpeakUpShell({
    this.showBackButton = false,
    this.previewMode = false,
    this.user,
    this.authController,
    this.preparationController,
    this.preparationLaunchController,
    this.jobPreparationController,
    this.reviewHistoryController,
    required this.agentController,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final User? user;
  final AuthController? authController;
  final AgentController agentController;
  final PreparationController? preparationController;
  final PreparationLaunchController? preparationLaunchController;
  final JobPreparationController? jobPreparationController;
  final ReviewHistoryController? reviewHistoryController;

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
  bool _reviewPresented = false;

  @override
  void initState() {
    super.initState();
    widget.agentController.addListener(_handleAgentState);
    _restorePresentedReview();
  }

  @override
  void didUpdateWidget(covariant SpeakUpShell oldWidget) {
    super.didUpdateWidget(oldWidget);
    final agentControllerChanged =
        oldWidget.agentController != widget.agentController;
    final historyControllerChanged =
        oldWidget.reviewHistoryController != widget.reviewHistoryController;
    if (agentControllerChanged) {
      oldWidget.agentController.removeListener(_handleAgentState);
      widget.agentController.addListener(_handleAgentState);
    }
    if (agentControllerChanged || historyControllerChanged) {
      _restorePresentedReview();
    }
  }

  @override
  void dispose() {
    widget.agentController.removeListener(_handleAgentState);
    super.dispose();
  }

  void _selectDestination(int index) {
    if (_selectedIndex == index) {
      if (index == 2) {
        unawaited(widget.reviewHistoryController?.refresh());
      }
      return;
    }
    unawaited(widget.agentController.stopPracticeAudio());
    if (index == 2) {
      unawaited(widget.reviewHistoryController?.refresh());
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

  void _showMockNotice(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  void _openPractice() {
    if (!widget.agentController.supportsPracticeFlow) {
      _showMockNotice('场景、语音练习与复盘尚未开放，当前可以使用 Agent 文本对话');
      return;
    }
    if (widget.agentController.review != null) {
      _selectDestination(2);
      return;
    }
    Navigator.of(context).pushNamed(AppRoutes.practice);
  }

  void _handleAgentState() {
    if (!mounted) {
      return;
    }
    final review = widget.agentController.review;
    if (review == null) {
      _reviewPresented = false;
    } else if (!_reviewPresented) {
      _reviewPresented = true;
      _selectedIndex = 2;
      unawaited(widget.reviewHistoryController?.refresh());
    }
    setState(() {});
  }

  void _restorePresentedReview() {
    if (widget.agentController.review == null) {
      _reviewPresented = false;
      return;
    }
    _reviewPresented = true;
    _selectedIndex = 2;
    unawaited(widget.reviewHistoryController?.refresh());
  }

  @override
  Widget build(BuildContext context) {
    final practiceAvailable = widget.agentController.supportsPracticeFlow;
    final keyboardVisible = MediaQuery.viewInsetsOf(context).bottom > 0;
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
        threadId: widget.agentController.threadId,
        displayName: widget.authController?.profile?.displayName,
        onOpenMenu: () => _scaffoldKey.currentState?.openDrawer(),
        onNavigateBack: widget.showBackButton
            ? () => Navigator.of(context).maybePop()
            : null,
        onCreatePlan: () => _selectDestination(1),
        onContinuePractice: _openPractice,
        onOpenReview: () => _selectDestination(2),
        onStartVoice: widget.agentController.supportsAgentVoice
            ? () => unawaited(widget.agentController.startAgentVoiceRecording())
            : null,
        voiceController: widget.agentController.voiceController,
        onCreateConversation: widget.agentController.supportsThreadHistory
            ? () => unawaited(widget.agentController.createThread())
            : null,
        messages: widget.agentController.messages,
        activeScene: widget.agentController.scene,
        hasFocusedThread:
            !widget.agentController.isInitialized ||
            widget.agentController.threadId != null,
        hasEarlierMessages: widget.agentController.hasEarlierMessages,
        isLoadingEarlierMessages:
            widget.agentController.isLoadingEarlierMessages,
        isBusy: widget.agentController.isBusy,
        errorMessage:
            widget.agentController.errorMessage ??
            (widget.agentController.canRetryThreadHistory
                ? widget.agentController.threadHistoryErrorMessage
                : null),
        onSubmitText: widget.agentController.sendText,
        onRetryOperation: widget.agentController.canRetry
            ? widget.agentController.retryLastOperation
            : widget.agentController.canRetryThreadHistory &&
                  !widget.agentController.isBusy
            ? widget.agentController.retryThreadHistory
            : null,
        onLoadEarlierMessages: widget.agentController.hasEarlierMessages
            ? widget.agentController.loadEarlierMessages
            : null,
      ),
      PreparationPage(
        showBackButton: widget.showBackButton,
        previewMode: widget.previewMode,
        agentController: widget.agentController,
        preparationController: widget.preparationController,
        launchController: widget.preparationLaunchController,
        onOpenJobPreparation: widget.jobPreparationController == null
            ? null
            : _openJobPreparation,
        onSceneSelected: () => _selectDestination(0),
        onPracticeStarted: _openPractice,
      ),
      ReviewPage(
        showBackButton: widget.showBackButton,
        previewMode: widget.previewMode,
        practiceAvailable: practiceAvailable,
        historyController: widget.reviewHistoryController,
        agentController: widget.agentController,
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
      ),
    ];

    return Scaffold(
      key: _scaffoldKey,
      extendBody: true,
      resizeToAvoidBottomInset: false,
      backgroundColor: Colors.transparent,
      drawer: _ConversationDrawer(
        previewMode: widget.previewMode,
        controller: widget.agentController,
      ),
      drawerScrimColor: const Color(0x330E1120),
      body: IndexedStack(index: _selectedIndex, children: pages),
      bottomNavigationBar: keyboardVisible
          ? null
          : GlassNavigationBar(
              destinations: _destinations,
              selectedIndex: _selectedIndex,
              onDestinationSelected: _selectDestination,
            ),
    );
  }
}

class _ConversationDrawer extends StatelessWidget {
  const _ConversationDrawer({
    required this.previewMode,
    required this.controller,
  });

  final bool previewMode;
  final AgentController controller;

  @override
  Widget build(BuildContext context) {
    final current = controller.currentThreadSummary;
    final currentThreadId = controller.threadId;
    final recentThreads = <AgentThreadSummary>[
      for (final thread in controller.threads)
        if (thread.id != currentThreadId) thread,
    ];
    return Drawer(
      width: 300,
      backgroundColor: const Color(0xFFF5F5F2),
      child: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 20),
          children: [
            Row(
              children: [
                const Expanded(
                  child: Text(
                    'SpeakUp',
                    style: TextStyle(fontSize: 22, fontWeight: FontWeight.w800),
                  ),
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
              style: const TextStyle(color: Color(0xFF6B6D74), fontSize: 13),
            ),
            const SizedBox(height: 20),
            FilledButton.tonalIcon(
              key: const Key('new-conversation-button'),
              onPressed: controller.isBusy || !controller.supportsThreadHistory
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
                backgroundColor: const Color(0xFFE8E8E4),
                foregroundColor: const Color(0xFF202124),
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
            const Text(
              '当前对话',
              style: TextStyle(
                color: Color(0xFF777983),
                fontSize: 13,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            if (currentThreadId == null)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8, vertical: 10),
                child: Text(
                  '尚未选择对话',
                  key: Key('no-focused-conversation'),
                  style: TextStyle(color: Color(0xFF777983)),
                ),
              )
            else
              _ConversationThreadTile(
                threadId: currentThreadId,
                updatedAt: current?.updatedAt,
                selected: true,
                enabled: !controller.isBusy,
                onTap: () => Navigator.of(context).pop(),
              ),
            const SizedBox(height: 24),
            const Text(
              '近期对话',
              style: TextStyle(
                color: Color(0xFF777983),
                fontSize: 13,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            if (recentThreads.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8, vertical: 10),
                child: Text(
                  '暂无其他对话',
                  key: Key('no-recent-conversations'),
                  style: TextStyle(color: Color(0xFF777983)),
                ),
              )
            else
              for (final thread in recentThreads)
                _ConversationThreadTile(
                  threadId: thread.id,
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
                ),
            if (controller.threadHistoryErrorMessage case final message?) ...[
              const SizedBox(height: 10),
              Text(
                message,
                key: const Key('conversation-history-error'),
                style: const TextStyle(
                  color: Color(0xFF9B2C24),
                  fontSize: 13,
                  height: 1.35,
                ),
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
}

class _ConversationThreadTile extends StatelessWidget {
  const _ConversationThreadTile({
    required this.threadId,
    required this.updatedAt,
    required this.selected,
    required this.enabled,
    required this.onTap,
  });

  final String threadId;
  final DateTime? updatedAt;
  final bool selected;
  final bool enabled;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final lastUpdatedAt = updatedAt;
    return Semantics(
      selected: selected,
      button: true,
      label: selected ? '当前 Agent 对话' : 'Agent 对话',
      child: ListTile(
        key: Key('conversation-thread-$threadId'),
        contentPadding: const EdgeInsets.symmetric(horizontal: 8),
        selected: selected,
        selectedTileColor: const Color(0xFFE8E8E4),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        leading: const Icon(Icons.chat_bubble_outline_rounded),
        title: const Text('Agent 对话'),
        subtitle: lastUpdatedAt == null
            ? null
            : Text('更新于 ${_formatThreadUpdatedAt(lastUpdatedAt)}'),
        trailing: selected
            ? const Icon(
                Icons.check_rounded,
                key: Key('focused-conversation-indicator'),
                size: 20,
              )
            : null,
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
  });

  final bool showBackButton;
  final User? user;
  final UserProfile? profile;
  final String? profileErrorMessage;
  final bool profileSaving;
  final Future<String?> Function(String)? onSaveDisplayName;
  final VoidCallback? onLogout;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('profile-page'),
      backgroundColor: const Color(0xFFF3F3F0),
      appBar: showBackButton
          ? AppBar(
              backgroundColor: const Color(0xFFF3F3F0),
              surfaceTintColor: Colors.transparent,
              elevation: 0,
              scrolledUnderElevation: 0,
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
          padding: const EdgeInsets.fromLTRB(20, 28, 20, 140),
          children: [
            const Text(
              '我的',
              style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 8),
            const Text(
              '当前账号与本机登录状态。',
              style: TextStyle(color: Color(0xFF696B73), fontSize: 15),
            ),
            const SizedBox(height: 28),
            Card(
              elevation: 0,
              color: Colors.white,
              child: ListTile(
                leading: CircleAvatar(
                  backgroundColor: Color(0xFFE8E8E5),
                  foregroundColor: Color(0xFF35363A),
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
              const SizedBox(height: 8),
              Text(
                profileErrorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 16),
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
