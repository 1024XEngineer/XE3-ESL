import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/glass_navigation_bar.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';

class SpeakUpShell extends StatefulWidget {
  const SpeakUpShell({this.showBackButton = false, super.key});

  final bool showBackButton;

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

  void _selectDestination(int index) {
    if (_selectedIndex == index) {
      return;
    }
    setState(() => _selectedIndex = index);
  }

  void _showMockNotice(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    final keyboardVisible = MediaQuery.viewInsetsOf(context).bottom > 0;
    final safeBottom = math.max(
      MediaQuery.viewPaddingOf(context).bottom,
      GlassNavigationBar.minimumBottomInset,
    );
    final composerBottomInset =
        GlassNavigationBar.heightFor(context) + safeBottom + 10;
    final pages = [
      ConversationPage(
        restingComposerBottom: composerBottomInset,
        onOpenMenu: () => _scaffoldKey.currentState?.openDrawer(),
        onNavigateBack: widget.showBackButton
            ? () => Navigator.of(context).maybePop()
            : null,
        onCreatePlan: () => _selectDestination(1),
        onContinuePractice: () =>
            Navigator.of(context).pushNamed(AppRoutes.practice),
        onOpenReview: () => _selectDestination(2),
        onVoicePlaceholder: () => _showMockNotice('该能力将在后续任务接入'),
      ),
      PreparationPage(showBackButton: widget.showBackButton),
      ReviewPage(showBackButton: widget.showBackButton),
      _ProfilePage(showBackButton: widget.showBackButton),
    ];

    return Scaffold(
      key: _scaffoldKey,
      extendBody: true,
      resizeToAvoidBottomInset: false,
      backgroundColor: Colors.transparent,
      drawer: const _ConversationDrawer(),
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
  const _ConversationDrawer();

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
            const SizedBox(height: 16),
            FilledButton.icon(
              key: const Key('new-conversation-button'),
              onPressed: () => Navigator.of(context).pop(),
              icon: const Icon(Icons.edit_square),
              label: const Text('开始新对话'),
            ),
            const SizedBox(height: 28),
            const Text(
              '最近对话',
              style: TextStyle(
                color: Color(0xFF777983),
                fontSize: 13,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            const _ConversationTile(title: '准备后端开发模拟面试', subtitle: '刚刚'),
            const _ConversationTile(title: '项目经历怎么说更清楚', subtitle: '昨天'),
            const _ConversationTile(title: '系统设计表达复盘', subtitle: '7 月 22 日'),
            const SizedBox(height: 28),
            const Text(
              '当前内容为 UI Mock',
              textAlign: TextAlign.center,
              style: TextStyle(color: Color(0xFF989AA3), fontSize: 12),
            ),
          ],
        ),
      ),
    );
  }
}

class _ConversationTile extends StatelessWidget {
  const _ConversationTile({required this.title, required this.subtitle});

  final String title;
  final String subtitle;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 8),
      leading: const Icon(Icons.chat_bubble_outline_rounded),
      title: Text(title, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(subtitle),
      onTap: () => Navigator.of(context).pop(),
    );
  }
}

class _ProfilePage extends StatelessWidget {
  const _ProfilePage({required this.showBackButton});

  final bool showBackButton;

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
          children: const [
            Text(
              '我的',
              style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
            ),
            SizedBox(height: 8),
            Text(
              '个人资料与偏好将在后续任务中接入。',
              style: TextStyle(color: Color(0xFF696B73), fontSize: 15),
            ),
            SizedBox(height: 28),
            Card(
              elevation: 0,
              child: ListTile(
                leading: CircleAvatar(child: Icon(Icons.person_rounded)),
                title: Text('演示用户'),
                subtitle: Text('固定身份 · UI Mock'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
