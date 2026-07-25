import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/glass_navigation_bar.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/model/identity_models.dart';

class SpeakUpShell extends StatefulWidget {
  const SpeakUpShell({
    this.showBackButton = false,
    this.previewMode = false,
    this.user,
    this.authController,
    required this.agentController,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final User? user;
  final AuthController? authController;
  final AgentController agentController;

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
      label: '场景',
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
    if (oldWidget.agentController == widget.agentController) {
      return;
    }
    oldWidget.agentController.removeListener(_handleAgentState);
    widget.agentController.addListener(_handleAgentState);
    _restorePresentedReview();
  }

  @override
  void dispose() {
    widget.agentController.removeListener(_handleAgentState);
    super.dispose();
  }

  void _selectDestination(int index) {
    if (_selectedIndex == index) {
      return;
    }
    unawaited(widget.agentController.stopPracticeAudio());
    setState(() => _selectedIndex = index);
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

  void _openVoicePractice() {
    if (!widget.agentController.supportsPracticeFlow) {
      _showMockNotice('语音练习尚未开放，当前可以使用 Agent 文本对话');
      return;
    }
    if (widget.agentController.review != null) {
      _selectDestination(2);
      return;
    }
    if (!widget.agentController.hasActivePractice) {
      _selectDestination(1);
      _showMockNotice('请先选择一个练习场景');
      return;
    }
    _openPractice();
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
        onOpenMenu: () => _scaffoldKey.currentState?.openDrawer(),
        onNavigateBack: widget.showBackButton
            ? () => Navigator.of(context).maybePop()
            : null,
        onCreatePlan: () => _selectDestination(1),
        onContinuePractice: _openPractice,
        onOpenReview: () => _selectDestination(2),
        onVoicePlaceholder: practiceAvailable ? _openVoicePractice : null,
        messages: widget.agentController.messages,
        activeScene: widget.agentController.scene,
        isBusy: widget.agentController.isBusy,
        errorMessage: widget.agentController.errorMessage,
        onSubmitText: widget.agentController.sendText,
        onRetryOperation: widget.agentController.canRetry
            ? widget.agentController.retryLastOperation
            : null,
      ),
      PreparationPage(
        showBackButton: widget.showBackButton,
        previewMode: widget.previewMode,
        agentController: widget.agentController,
        onSceneSelected: () => _selectDestination(0),
      ),
      ReviewPage(
        showBackButton: widget.showBackButton,
        previewMode: widget.previewMode,
        agentController: widget.agentController,
      ),
      _ProfilePage(
        showBackButton: widget.showBackButton,
        user: widget.user,
        onLogout: widget.authController?.logout,
      ),
    ];

    return Scaffold(
      key: _scaffoldKey,
      extendBody: true,
      resizeToAvoidBottomInset: false,
      backgroundColor: Colors.transparent,
      drawer: _ConversationDrawer(previewMode: widget.previewMode),
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
  const _ConversationDrawer({required this.previewMode});

  final bool previewMode;

  @override
  Widget build(BuildContext context) {
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
            ListTile(
              contentPadding: const EdgeInsets.symmetric(horizontal: 8),
              leading: const Icon(Icons.chat_bubble_outline_rounded),
              title: const Text('Agent 对话'),
              subtitle: Text(previewMode ? '本地界面预览 · UI Mock' : '已连接当前账号'),
            ),
          ],
        ),
      ),
    );
  }
}

class _ProfilePage extends StatelessWidget {
  const _ProfilePage({
    required this.showBackButton,
    required this.user,
    required this.onLogout,
  });

  final bool showBackButton;
  final User? user;
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
                leading: const CircleAvatar(
                  backgroundColor: Color(0xFFE8E8E5),
                  foregroundColor: Color(0xFF35363A),
                  child: Icon(Icons.person_rounded),
                ),
                title: Text(user?.email ?? '本地界面预览'),
                subtitle: Text(user == null ? '尚未连接正式账号' : '当前登录账号'),
              ),
            ),
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
}
