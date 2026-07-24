/// Conversation module boundary.
library;

import 'dart:ui' as ui;

import 'package:flutter/material.dart';

class ConversationPage extends StatelessWidget {
  const ConversationPage({
    this.restingComposerBottom = 16,
    this.onOpenMenu,
    this.onCreatePlan,
    this.onContinuePractice,
    this.onOpenReview,
    this.onVoicePlaceholder,
    super.key,
  });

  final double restingComposerBottom;
  final VoidCallback? onOpenMenu;
  final VoidCallback? onCreatePlan;
  final VoidCallback? onContinuePractice;
  final VoidCallback? onOpenReview;
  final VoidCallback? onVoicePlaceholder;

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final horizontalPadding = width >= 390 ? 20.0 : 16.0;
    final keyboardVisible = MediaQuery.viewInsetsOf(context).bottom > 0;
    final textScaler = MediaQuery.textScalerOf(context);
    final titleSize = width < 350 ? 32.0 : 38.0;

    return Scaffold(
      key: const Key('agent-home-page'),
      resizeToAvoidBottomInset: true,
      backgroundColor: Colors.transparent,
      body: Stack(
        children: [
          const Positioned.fill(child: _AgentBackground()),
          SafeArea(
            bottom: false,
            child: LayoutBuilder(
              builder: (context, constraints) {
                return SingleChildScrollView(
                  padding: EdgeInsets.fromLTRB(
                    horizontalPadding,
                    12,
                    horizontalPadding,
                    keyboardVisible ? 142 : 250,
                  ),
                  child: ConstrainedBox(
                    constraints: BoxConstraints(
                      minHeight: (constraints.maxHeight - 230).clamp(
                        300,
                        double.infinity,
                      ),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _AgentTopBar(
                          onOpenMenu: onOpenMenu,
                          onVoicePlaceholder: onVoicePlaceholder,
                        ),
                        SizedBox(height: width < 350 ? 22 : 34),
                        const Center(child: _AgentOrb()),
                        SizedBox(height: width < 350 ? 24 : 34),
                        const _Greeting(),
                        const SizedBox(height: 8),
                        Text(
                          '我能为你做什么？',
                          style: TextStyle(
                            color: const Color(0xFF0B0B0D),
                            fontSize: titleSize,
                            fontWeight: FontWeight.w800,
                            height: 1.12,
                            letterSpacing: -1.2,
                          ),
                        ),
                        SizedBox(height: width < 350 ? 22 : 30),
                        _QuickActions(
                          compact: width < 350 || textScaler.scale(1) > 1.2,
                          onCreatePlan: onCreatePlan,
                          onContinuePractice: onContinuePractice,
                          onOpenReview: onOpenReview,
                        ),
                      ],
                    ),
                  ),
                );
              },
            ),
          ),
          Positioned(
            left: horizontalPadding,
            right: horizontalPadding,
            bottom: keyboardVisible ? 10 : restingComposerBottom,
            child: _AgentComposer(
              keyboardVisible: keyboardVisible,
              onVoicePlaceholder: onVoicePlaceholder,
            ),
          ),
        ],
      ),
    );
  }
}

class _AgentBackground extends StatelessWidget {
  const _AgentBackground();

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: Stack(
        fit: StackFit.expand,
        children: [
          const DecoratedBox(
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [
                  Color(0xFFDDF5F5),
                  Color(0xFFF8F8FC),
                  Color(0xFFE4E8FF),
                  Color(0xFFF0ECFA),
                ],
                stops: [0, 0.42, 0.7, 1],
              ),
            ),
          ),
          Positioned(
            top: -80,
            right: -90,
            child: ImageFiltered(
              imageFilter: ui.ImageFilter.blur(sigmaX: 42, sigmaY: 42),
              child: const _GlowCircle(size: 240, color: Color(0x667E8CFF)),
            ),
          ),
          Positioned(
            top: 170,
            left: -110,
            child: ImageFiltered(
              imageFilter: ui.ImageFilter.blur(sigmaX: 50, sigmaY: 50),
              child: const _GlowCircle(size: 230, color: Color(0x665DE1DD)),
            ),
          ),
        ],
      ),
    );
  }
}

class _GlowCircle extends StatelessWidget {
  const _GlowCircle({required this.size, required this.color});

  final double size;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(color: color, shape: BoxShape.circle),
    );
  }
}

class _AgentTopBar extends StatelessWidget {
  const _AgentTopBar({
    required this.onOpenMenu,
    required this.onVoicePlaceholder,
  });

  final VoidCallback? onOpenMenu;
  final VoidCallback? onVoicePlaceholder;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        _RoundGlassButton(
          key: const Key('conversation-menu-button'),
          tooltip: '打开对话菜单',
          icon: Icons.menu_rounded,
          onPressed: onOpenMenu,
        ),
        const Spacer(),
        const _BrandCapsule(),
        const Spacer(),
        _RoundGlassButton(
          tooltip: '语音播放，尚未接入',
          icon: Icons.volume_up_outlined,
          onPressed: onVoicePlaceholder,
        ),
      ],
    );
  }
}

class _RoundGlassButton extends StatelessWidget {
  const _RoundGlassButton({
    required this.tooltip,
    required this.icon,
    required this.onPressed,
    super.key,
  });

  final String tooltip;
  final IconData icon;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return ClipOval(
      child: BackdropFilter(
        filter: ui.ImageFilter.blur(sigmaX: 18, sigmaY: 18),
        child: Material(
          color: const Color(0xAFFFFFFF),
          child: IconButton(
            tooltip: tooltip,
            onPressed: onPressed,
            icon: Icon(icon, color: const Color(0xFF15161A)),
            iconSize: 25,
            constraints: const BoxConstraints.tightFor(width: 48, height: 48),
          ),
        ),
      ),
    );
  }
}

class _BrandCapsule extends StatelessWidget {
  const _BrandCapsule();

  @override
  Widget build(BuildContext context) {
    return ClipRRect(
      borderRadius: BorderRadius.circular(24),
      child: BackdropFilter(
        filter: ui.ImageFilter.blur(sigmaX: 16, sigmaY: 16),
        child: Container(
          height: 44,
          padding: const EdgeInsets.symmetric(horizontal: 18),
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: const Color(0xA8FFFFFF),
            borderRadius: BorderRadius.circular(24),
            border: Border.all(color: const Color(0xBFFFFFFF)),
          ),
          child: const Text(
            'SpeakUp',
            style: TextStyle(fontSize: 17, fontWeight: FontWeight.w800),
          ),
        ),
      ),
    );
  }
}

class _AgentOrb extends StatelessWidget {
  const _AgentOrb();

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: Container(
        width: 104,
        height: 104,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: const RadialGradient(
            center: Alignment(-0.35, -0.42),
            radius: 1.05,
            colors: [
              Color(0xFFF5FFFF),
              Color(0xFFCBE7FF),
              Color(0xFFB8B9F4),
              Color(0xFFA78FDF),
            ],
            stops: [0, 0.36, 0.7, 1],
          ),
          boxShadow: const [
            BoxShadow(
              color: Color(0x447A88D9),
              blurRadius: 38,
              spreadRadius: 4,
              offset: Offset(0, 16),
            ),
          ],
        ),
        child: Align(
          alignment: const Alignment(-0.24, -0.34),
          child: Container(
            width: 18,
            height: 18,
            decoration: const BoxDecoration(
              shape: BoxShape.circle,
              color: Color(0xBFFFFFFF),
            ),
          ),
        ),
      ),
    );
  }
}

class _Greeting extends StatelessWidget {
  const _Greeting();

  @override
  Widget build(BuildContext context) {
    return ShaderMask(
      blendMode: BlendMode.srcIn,
      shaderCallback: (bounds) => const LinearGradient(
        colors: [Color(0xFF5B9DF5), Color(0xFFD76ABB)],
      ).createShader(bounds),
      child: const Text(
        'Hi, 智',
        style: TextStyle(
          color: Colors.white,
          fontSize: 34,
          fontWeight: FontWeight.w500,
          height: 1.1,
          letterSpacing: -0.7,
        ),
      ),
    );
  }
}

class _QuickActions extends StatelessWidget {
  const _QuickActions({
    required this.compact,
    required this.onCreatePlan,
    required this.onContinuePractice,
    required this.onOpenReview,
  });

  final bool compact;
  final VoidCallback? onCreatePlan;
  final VoidCallback? onContinuePractice;
  final VoidCallback? onOpenReview;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _QuickActionButton(
          actionKey: const Key('quick-action-create-plan'),
          icon: Icons.auto_awesome_rounded,
          label: '创建模拟面试',
          compact: compact,
          onPressed: onCreatePlan,
        ),
        const SizedBox(height: 11),
        _QuickActionButton(
          actionKey: const Key('quick-action-continue-practice'),
          icon: Icons.play_circle_outline_rounded,
          label: '继续上次练习',
          compact: compact,
          onPressed: onContinuePractice,
        ),
        const SizedBox(height: 11),
        _QuickActionButton(
          icon: Icons.grid_view_rounded,
          label: '浏览练习场景',
          compact: compact,
          onPressed: onCreatePlan,
        ),
        const SizedBox(height: 11),
        _QuickActionButton(
          actionKey: const Key('quick-action-recent-review'),
          icon: Icons.fact_check_outlined,
          label: '查看最近复盘',
          compact: compact,
          onPressed: onOpenReview,
        ),
      ],
    );
  }
}

class _QuickActionButton extends StatelessWidget {
  const _QuickActionButton({
    this.actionKey,
    required this.icon,
    required this.label,
    required this.compact,
    required this.onPressed,
  });

  final Key? actionKey;
  final IconData icon;
  final String label;
  final bool compact;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(28),
          boxShadow: const [
            BoxShadow(
              color: Color(0x14513E70),
              blurRadius: 18,
              offset: Offset(0, 8),
            ),
          ],
        ),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(28),
          child: BackdropFilter(
            filter: ui.ImageFilter.blur(sigmaX: 18, sigmaY: 18),
            child: Material(
              color: const Color(0xB8FFFFFF),
              child: InkWell(
                key: actionKey,
                onTap: onPressed,
                child: Container(
                  constraints: const BoxConstraints(minHeight: 52),
                  padding: EdgeInsets.symmetric(
                    horizontal: compact ? 16 : 20,
                    vertical: 12,
                  ),
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(28),
                    border: Border.all(color: const Color(0xCFFFFFFF)),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(icon, size: 22, color: const Color(0xFF555AE6)),
                      const SizedBox(width: 12),
                      Flexible(
                        child: Text(
                          label,
                          style: TextStyle(
                            color: const Color(0xFF15161A),
                            fontSize: compact ? 15 : 16,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _AgentComposer extends StatelessWidget {
  const _AgentComposer({
    required this.keyboardVisible,
    required this.onVoicePlaceholder,
  });

  final bool keyboardVisible;
  final VoidCallback? onVoicePlaceholder;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(28),
        boxShadow: const [
          BoxShadow(
            color: Color(0x26513E70),
            blurRadius: 32,
            offset: Offset(0, 14),
          ),
        ],
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(28),
        child: BackdropFilter(
          filter: ui.ImageFilter.blur(sigmaX: 24, sigmaY: 24),
          child: Container(
            key: const Key('agent-composer-surface'),
            constraints: BoxConstraints(minHeight: keyboardVisible ? 82 : 104),
            padding: const EdgeInsets.fromLTRB(12, 9, 10, 9),
            decoration: BoxDecoration(
              gradient: const LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [Color(0xE6FFFFFF), Color(0xB8FFFFFF)],
              ),
              borderRadius: BorderRadius.circular(28),
              border: Border.all(color: const Color(0xE0FFFFFF)),
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  key: const Key('agent-composer-field'),
                  minLines: 1,
                  maxLines: 2,
                  textInputAction: TextInputAction.send,
                  decoration: const InputDecoration(
                    hintText: '问问 SpeakUp',
                    hintStyle: TextStyle(
                      color: Color(0xFF989AA3),
                      fontSize: 16,
                    ),
                    border: InputBorder.none,
                    isDense: true,
                    contentPadding: EdgeInsets.fromLTRB(4, 4, 4, 2),
                  ),
                ),
                if (!keyboardVisible) const SizedBox(height: 5),
                Row(
                  children: [
                    IconButton(
                      tooltip: '添加内容，尚未接入',
                      onPressed: onVoicePlaceholder,
                      icon: const Icon(Icons.add_rounded),
                    ),
                    IconButton(
                      tooltip: '调整偏好，尚未接入',
                      onPressed: onVoicePlaceholder,
                      icon: const Icon(Icons.tune_rounded),
                    ),
                    const Spacer(),
                    Semantics(
                      key: const Key('agent-mic-placeholder'),
                      button: true,
                      label: '语音输入，即将开放',
                      onTap: onVoicePlaceholder,
                      child: ExcludeSemantics(
                        child: IconButton.filledTonal(
                          tooltip: '语音输入，即将开放',
                          onPressed: onVoicePlaceholder,
                          icon: const Icon(Icons.mic_none_rounded),
                        ),
                      ),
                    ),
                    const SizedBox(width: 6),
                    const IconButton.filled(
                      tooltip: '发送，尚未接入',
                      onPressed: null,
                      icon: Icon(Icons.arrow_upward_rounded),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
