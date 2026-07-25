/// Conversation module boundary.
library;

import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/agent/agent_models.dart';

class ConversationPage extends StatelessWidget {
  const ConversationPage({
    this.previewMode = false,
    this.practiceAvailable = true,
    this.restingComposerBottom = 16,
    this.onOpenMenu,
    this.onNavigateBack,
    this.onCreatePlan,
    this.onContinuePractice,
    this.onOpenReview,
    this.onVoicePlaceholder,
    this.messages = const <AgentMessage>[],
    this.activeScene,
    this.isBusy = false,
    this.errorMessage,
    this.onSubmitText,
    this.onRetryOperation,
    super.key,
  });

  final bool previewMode;
  final bool practiceAvailable;
  final double restingComposerBottom;
  final VoidCallback? onOpenMenu;
  final VoidCallback? onNavigateBack;
  final VoidCallback? onCreatePlan;
  final VoidCallback? onContinuePractice;
  final VoidCallback? onOpenReview;
  final VoidCallback? onVoicePlaceholder;
  final List<AgentMessage> messages;
  final AgentScene? activeScene;
  final bool isBusy;
  final String? errorMessage;
  final Future<bool> Function(String)? onSubmitText;
  final VoidCallback? onRetryOperation;

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final horizontalPadding = width >= 390 ? 20.0 : 16.0;
    final keyboardVisible = MediaQuery.viewInsetsOf(context).bottom > 0;
    final textScaler = MediaQuery.textScalerOf(context);
    final titleSize = width < 350 ? 30.0 : 36.0;
    final composerBottom = keyboardVisible ? 10.0 : restingComposerBottom;
    final acceptedUserMessage = _lastUserMessage(messages);

    return Scaffold(
      key: const Key('agent-home-page'),
      resizeToAvoidBottomInset: true,
      backgroundColor: Colors.transparent,
      body: Stack(
        children: [
          const Positioned.fill(child: _AgentBackground()),
          Positioned.fill(
            child: SafeArea(
              bottom: false,
              child: Padding(
                padding: EdgeInsets.only(bottom: composerBottom),
                child: Column(
                  children: [
                    Expanded(
                      child: SingleChildScrollView(
                        padding: EdgeInsets.fromLTRB(
                          horizontalPadding,
                          12,
                          horizontalPadding,
                          0,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            _AgentTopBar(
                              previewMode: previewMode,
                              onOpenMenu: onOpenMenu,
                              onNavigateBack: onNavigateBack,
                            ),
                            SizedBox(height: width < 350 ? 32 : 48),
                            if (messages.isEmpty) ...[
                              const _Greeting(),
                              const SizedBox(height: 8),
                              Text(
                                '我能为你做什么？',
                                style: TextStyle(
                                  color: const Color(0xFF0B0B0D),
                                  fontSize: titleSize,
                                  fontWeight: FontWeight.w600,
                                  height: 1.12,
                                  letterSpacing: -0.8,
                                ),
                              ),
                              SizedBox(height: width < 350 ? 20 : 26),
                              if (practiceAvailable)
                                _QuickActions(
                                  compact:
                                      width < 350 || textScaler.scale(1) > 1.2,
                                  onCreatePlan: onCreatePlan,
                                  onContinuePractice: onContinuePractice,
                                  onOpenReview: onOpenReview,
                                )
                              else
                                const _PracticeUnavailableNotice(),
                            ] else ...[
                              Text(
                                activeScene?.title ?? 'Agent 对话',
                                key: const Key('agent-thread-title'),
                                style: TextStyle(
                                  color: const Color(0xFF0B0B0D),
                                  fontSize: width < 350 ? 25 : 29,
                                  fontWeight: FontWeight.w700,
                                ),
                              ),
                              const SizedBox(height: 18),
                              _MessageList(messages: messages),
                            ],
                            if (isBusy) ...[
                              const SizedBox(height: 14),
                              const LinearProgressIndicator(
                                key: Key('agent-operation-progress'),
                                minHeight: 2,
                              ),
                            ],
                            if (errorMessage case final message?) ...[
                              const SizedBox(height: 14),
                              _InlineError(
                                message: message,
                                onRetry: onRetryOperation,
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                    Padding(
                      padding: EdgeInsets.fromLTRB(
                        horizontalPadding,
                        16,
                        horizontalPadding,
                        0,
                      ),
                      child: _AgentComposer(
                        keyboardVisible: keyboardVisible,
                        acceptedUserMessageId: acceptedUserMessage?.id,
                        acceptedUserMessageText: acceptedUserMessage?.text,
                        onVoicePlaceholder: onVoicePlaceholder,
                        onSubmitText: onSubmitText,
                        isBusy: isBusy,
                      ),
                    ),
                  ],
                ),
              ),
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
    return const ExcludeSemantics(child: ColoredBox(color: Color(0xFFF3F3F0)));
  }
}

class _AgentTopBar extends StatelessWidget {
  const _AgentTopBar({
    required this.previewMode,
    required this.onOpenMenu,
    required this.onNavigateBack,
  });

  final bool previewMode;
  final VoidCallback? onOpenMenu;
  final VoidCallback? onNavigateBack;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        _RoundGlassButton(
          key: Key(
            onNavigateBack == null
                ? 'conversation-menu-button'
                : 'conversation-route-back-button',
          ),
          tooltip: onNavigateBack == null ? '打开对话菜单' : '返回',
          icon: onNavigateBack == null
              ? Icons.menu_rounded
              : Icons.arrow_back_rounded,
          onPressed: onNavigateBack ?? onOpenMenu,
        ),
        if (previewMode) ...[
          const SizedBox(width: 12),
          Semantics(
            label: '当前为 UI Mock',
            child: ExcludeSemantics(
              child: MediaQuery.withNoTextScaling(
                child: const Text(
                  'UI Mock',
                  key: Key('agent-preview-label'),
                  style: TextStyle(
                    color: Color(0xFF6C6E75),
                    fontSize: 13,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
            ),
          ),
        ],
        const Spacer(),
      ],
    );
  }
}

class _PracticeUnavailableNotice extends StatelessWidget {
  const _PracticeUnavailableNotice();

  @override
  Widget build(BuildContext context) {
    return const Text(
      '当前已开放 Agent 文本对话；场景、语音练习与复盘待正式服务契约接入。',
      key: Key('agent-practice-unavailable'),
      style: TextStyle(color: Color(0xFF6C6E75), height: 1.45),
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
          color: const Color(0xD9FFFFFF),
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

class _Greeting extends StatelessWidget {
  const _Greeting();

  @override
  Widget build(BuildContext context) {
    return const Text(
      '你好',
      style: TextStyle(
        color: Color(0xFF5F6064),
        fontSize: 29,
        fontWeight: FontWeight.w500,
        height: 1.1,
        letterSpacing: -0.5,
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
          label: '创建模拟面试',
          compact: compact,
          onPressed: onCreatePlan,
        ),
        const SizedBox(height: 10),
        _QuickActionButton(
          actionKey: const Key('quick-action-continue-practice'),
          label: '继续上次练习',
          compact: compact,
          onPressed: onContinuePractice,
        ),
        const SizedBox(height: 10),
        _QuickActionButton(
          actionKey: const Key('quick-action-browse-scenes'),
          label: '浏览练习场景',
          compact: compact,
          onPressed: onCreatePlan,
        ),
        const SizedBox(height: 10),
        _QuickActionButton(
          actionKey: const Key('quick-action-recent-review'),
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
    required this.label,
    required this.compact,
    required this.onPressed,
  });

  final Key? actionKey;
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
              color: Color(0x10000000),
              blurRadius: 14,
              offset: Offset(0, 6),
            ),
          ],
        ),
        child: Material(
          color: const Color(0xDEFFFFFF),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(28),
            side: const BorderSide(color: Color(0xF2FFFFFF)),
          ),
          clipBehavior: Clip.antiAlias,
          child: InkWell(
            key: actionKey,
            onTap: onPressed,
            child: Container(
              constraints: const BoxConstraints(minHeight: 50),
              padding: EdgeInsets.symmetric(
                horizontal: compact ? 18 : 22,
                vertical: 11,
              ),
              child: Text(
                label,
                style: TextStyle(
                  color: const Color(0xFF15161A),
                  fontSize: compact ? 15 : 16,
                  fontWeight: FontWeight.w500,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _MessageList extends StatelessWidget {
  const _MessageList({required this.messages});

  final List<AgentMessage> messages;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('agent-message-list'),
      children: [
        for (final message in messages)
          Align(
            alignment: message.role == AgentMessageRole.user
                ? Alignment.centerRight
                : Alignment.centerLeft,
            child: Container(
              key: Key('agent-message-${message.id}'),
              constraints: const BoxConstraints(maxWidth: 310),
              margin: const EdgeInsets.only(bottom: 12),
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 13),
              decoration: BoxDecoration(
                color: message.role == AgentMessageRole.user
                    ? const Color(0xFF202124)
                    : Colors.white,
                borderRadius: BorderRadius.circular(20),
                border: Border.all(
                  color: message.role == AgentMessageRole.user
                      ? const Color(0xFF202124)
                      : const Color(0xFFE6E6E2),
                ),
              ),
              child: Text(
                message.text,
                style: TextStyle(
                  color: message.role == AgentMessageRole.user
                      ? Colors.white
                      : const Color(0xFF202124),
                  height: 1.45,
                ),
              ),
            ),
          ),
      ],
    );
  }
}

class _InlineError extends StatelessWidget {
  const _InlineError({required this.message, required this.onRetry});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: const Color(0xFFFFF3F1),
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
        child: Row(
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 20,
              color: Color(0xFF8B2E26),
            ),
            const SizedBox(width: 8),
            Expanded(child: Text(message)),
            if (onRetry != null)
              TextButton(
                key: const Key('agent-retry-operation-button'),
                onPressed: onRetry,
                child: const Text('重试'),
              ),
          ],
        ),
      ),
    );
  }
}

class _AgentComposer extends StatefulWidget {
  const _AgentComposer({
    required this.keyboardVisible,
    required this.acceptedUserMessageId,
    required this.acceptedUserMessageText,
    required this.onVoicePlaceholder,
    required this.onSubmitText,
    required this.isBusy,
  });

  final bool keyboardVisible;
  final String? acceptedUserMessageId;
  final String? acceptedUserMessageText;
  final VoidCallback? onVoicePlaceholder;
  final Future<bool> Function(String)? onSubmitText;
  final bool isBusy;

  @override
  State<_AgentComposer> createState() => _AgentComposerState();
}

class _AgentComposerState extends State<_AgentComposer> {
  final _controller = TextEditingController();
  bool _suppressControllerNotifications = false;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_handleTextChanged);
  }

  @override
  void didUpdateWidget(covariant _AgentComposer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.acceptedUserMessageId != null &&
        widget.acceptedUserMessageId != oldWidget.acceptedUserMessageId &&
        _controller.text.trim() == widget.acceptedUserMessageText) {
      _suppressControllerNotifications = true;
      _controller.clear();
      _suppressControllerNotifications = false;
    }
  }

  @override
  void dispose() {
    _controller
      ..removeListener(_handleTextChanged)
      ..dispose();
    super.dispose();
  }

  void _handleTextChanged() {
    if (!_suppressControllerNotifications) {
      setState(() {});
    }
  }

  Future<void> _submit() async {
    final text = _controller.text.trim();
    if (text.isEmpty || widget.isBusy || widget.onSubmitText == null) {
      return;
    }
    final sent = await widget.onSubmitText!(text);
    if (mounted && sent) {
      _controller.clear();
    }
  }

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(28),
        boxShadow: const [
          BoxShadow(
            color: Color(0x1C000000),
            blurRadius: 28,
            offset: Offset(0, 12),
          ),
        ],
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(28),
        child: BackdropFilter(
          filter: ui.ImageFilter.blur(sigmaX: 24, sigmaY: 24),
          child: Container(
            key: const Key('agent-composer-surface'),
            constraints: BoxConstraints(
              minHeight: widget.keyboardVisible ? 82 : 104,
            ),
            padding: const EdgeInsets.fromLTRB(12, 9, 10, 9),
            decoration: BoxDecoration(
              color: const Color(0xE6FFFFFF),
              borderRadius: BorderRadius.circular(28),
              border: Border.all(color: const Color(0xFFFFFFFF)),
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  key: const Key('agent-composer-field'),
                  controller: _controller,
                  enabled: !widget.isBusy,
                  minLines: 1,
                  maxLines: 2,
                  inputFormatters: <TextInputFormatter>[_agentContentFormatter],
                  textInputAction: TextInputAction.send,
                  onSubmitted: (_) => _submit(),
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
                if (!widget.keyboardVisible) const SizedBox(height: 5),
                Row(
                  children: [
                    const Spacer(),
                    if (widget.onVoicePlaceholder != null) ...[
                      Semantics(
                        key: const Key('agent-mic-placeholder'),
                        button: true,
                        label: '开始按轮语音练习',
                        onTap: widget.onVoicePlaceholder,
                        child: ExcludeSemantics(
                          child: IconButton.filledTonal(
                            tooltip: '开始按轮语音练习',
                            onPressed: widget.onVoicePlaceholder,
                            style: IconButton.styleFrom(
                              backgroundColor: const Color(0xFFE8E8E5),
                              foregroundColor: const Color(0xFF44464D),
                            ),
                            icon: const Icon(Icons.mic_none_rounded),
                          ),
                        ),
                      ),
                      const SizedBox(width: 6),
                    ],
                    IconButton.filled(
                      key: const Key('agent-send-button'),
                      tooltip: '发送',
                      onPressed:
                          _controller.text.trim().isEmpty ||
                              widget.isBusy ||
                              widget.onSubmitText == null
                          ? null
                          : _submit,
                      icon: const Icon(Icons.arrow_upward_rounded),
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

final TextInputFormatter _agentContentFormatter =
    TextInputFormatter.withFunction((oldValue, newValue) {
      final text = newValue.text;
      return text.runes.length <= 4096 && utf8.encode(text).length <= 16384
          ? newValue
          : oldValue;
    });

AgentMessage? _lastUserMessage(List<AgentMessage> messages) {
  for (final message in messages.reversed) {
    if (message.role == AgentMessageRole.user) {
      return message;
    }
  }
  return null;
}
