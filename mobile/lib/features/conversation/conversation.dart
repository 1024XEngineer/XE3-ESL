/// Conversation module boundary.
library;

import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_controller.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/agent/agent_voice_widgets.dart';
import 'package:speakup/design/speak_up_design.dart';

class ConversationPage extends StatefulWidget {
  const ConversationPage({
    this.previewMode = false,
    this.practiceAvailable = true,
    this.restingComposerBottom = 16,
    this.threadId,
    this.displayName,
    this.onOpenMenu,
    this.onNavigateBack,
    this.onCreatePlan,
    this.onContinuePractice,
    this.onOpenReview,
    VoidCallback? onStartVoice,
    VoidCallback? onVoicePlaceholder,
    this.onCreateConversation,
    this.messages = const <AgentMessage>[],
    this.activeScene,
    this.hasFocusedThread = true,
    this.hasEarlierMessages = false,
    this.isLoadingEarlierMessages = false,
    this.isBusy = false,
    this.errorMessage,
    this.onSubmitText,
    this.onRetryOperation,
    this.onLoadEarlierMessages,
    this.voiceController,
    super.key,
  }) : onStartVoice = onStartVoice ?? onVoicePlaceholder;

  final bool previewMode;
  final bool practiceAvailable;
  final double restingComposerBottom;
  final String? threadId;
  final String? displayName;
  final VoidCallback? onOpenMenu;
  final VoidCallback? onNavigateBack;
  final VoidCallback? onCreatePlan;
  final VoidCallback? onContinuePractice;
  final VoidCallback? onOpenReview;
  final VoidCallback? onStartVoice;
  final VoidCallback? onCreateConversation;
  final List<AgentMessage> messages;
  final AgentScene? activeScene;
  final bool hasFocusedThread;
  final bool hasEarlierMessages;
  final bool isLoadingEarlierMessages;
  final bool isBusy;
  final String? errorMessage;
  final Future<bool> Function(String)? onSubmitText;
  final VoidCallback? onRetryOperation;
  final VoidCallback? onLoadEarlierMessages;
  final AgentVoiceController? voiceController;

  @override
  State<ConversationPage> createState() => _ConversationPageState();

  Widget _build(
    BuildContext context, {
    required ScrollController scrollController,
    required VoidCallback? onLoadEarlierMessages,
  }) {
    final width = MediaQuery.sizeOf(context).width;
    final horizontalPadding = width >= 390 ? 20.0 : 16.0;
    final keyboardVisible = MediaQuery.viewInsetsOf(context).bottom > 0;
    final textScaler = MediaQuery.textScalerOf(context);
    final titleSize = width < 350 ? 28.0 : 32.0;
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
                    Padding(
                      padding: EdgeInsets.fromLTRB(
                        horizontalPadding,
                        12,
                        horizontalPadding,
                        8,
                      ),
                      child: _AgentTopBar(
                        previewMode: previewMode,
                        onOpenMenu: onOpenMenu,
                        onNavigateBack: onNavigateBack,
                        onCreateConversation: onCreateConversation,
                        isBusy: isBusy,
                      ),
                    ),
                    Expanded(
                      child: SingleChildScrollView(
                        controller: scrollController,
                        padding: EdgeInsets.fromLTRB(
                          horizontalPadding,
                          8,
                          horizontalPadding,
                          0,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            SizedBox(
                              height: hasFocusedThread && messages.isNotEmpty
                                  ? 4
                                  : width < 350
                                  ? 16
                                  : 24,
                            ),
                            if (!hasFocusedThread) ...[
                              _NoFocusedConversation(
                                onCreateConversation: onCreateConversation,
                                onOpenConversations: onOpenMenu,
                                isBusy: isBusy,
                              ),
                            ] else if (messages.isEmpty) ...[
                              _Greeting(displayName: displayName),
                              if (displayName != null) ...[
                                const SizedBox(height: 5),
                                Text(
                                  '今天想练什么？',
                                  style: TextStyle(
                                    color: SpeakUpDesign.ink,
                                    fontSize: titleSize,
                                    fontWeight: FontWeight.w600,
                                    height: 1.12,
                                    letterSpacing: -0.8,
                                  ),
                                ),
                              ],
                              SizedBox(height: width < 350 ? 16 : 20),
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
                              if (activeScene case final scene?) ...[
                                Row(
                                  children: [
                                    const Text(
                                      '当前场景',
                                      style: TextStyle(
                                        color: SpeakUpDesign.secondary,
                                        fontSize: 13,
                                        fontWeight: FontWeight.w500,
                                      ),
                                    ),
                                    const SizedBox(width: 8),
                                    Flexible(
                                      child: Text(
                                        scene.title,
                                        key: const Key('agent-thread-title'),
                                        maxLines: 1,
                                        overflow: TextOverflow.ellipsis,
                                        style: const TextStyle(
                                          color: SpeakUpDesign.ink,
                                          fontSize: 14,
                                          fontWeight: FontWeight.w600,
                                        ),
                                      ),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 8),
                              ],
                              if (hasEarlierMessages) ...[
                                Align(
                                  alignment: Alignment.centerLeft,
                                  child: TextButton.icon(
                                    key: const Key(
                                      'load-earlier-agent-messages',
                                    ),
                                    onPressed:
                                        isLoadingEarlierMessages ||
                                            onLoadEarlierMessages == null
                                        ? null
                                        : onLoadEarlierMessages,
                                    icon: isLoadingEarlierMessages
                                        ? const SizedBox.square(
                                            dimension: 16,
                                            child: CircularProgressIndicator(
                                              strokeWidth: 2,
                                            ),
                                          )
                                        : const Icon(
                                            Icons.history_rounded,
                                            size: 18,
                                          ),
                                    label: Text(
                                      isLoadingEarlierMessages
                                          ? '正在加载更早消息'
                                          : '加载更早消息',
                                    ),
                                  ),
                                ),
                                const SizedBox(height: 4),
                              ],
                              _MessageList(
                                messages: messages,
                                voiceController: voiceController,
                              ),
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
                        key: ValueKey<String?>(threadId),
                        keyboardVisible: keyboardVisible,
                        acceptedUserMessageId: acceptedUserMessage?.id,
                        acceptedUserMessageText: acceptedUserMessage?.text,
                        onStartVoice: onStartVoice,
                        voiceController: voiceController,
                        voiceEnabled: voiceController != null && !isBusy,
                        onSubmitText: onSubmitText,
                        enabled: hasFocusedThread,
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

class _ConversationPageState extends State<ConversationPage> {
  final ScrollController _scrollController = ScrollController();
  _ConversationScrollAnchor? _earlierMessagesAnchor;
  int _scrollRequestGeneration = 0;

  @override
  void initState() {
    super.initState();
    _scheduleThreadInitialPosition();
  }

  @override
  void didUpdateWidget(covariant ConversationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.threadId != widget.threadId) {
      _earlierMessagesAnchor = null;
      _scheduleThreadInitialPosition();
      return;
    }

    final messageCountGrew = widget.messages.length > oldWidget.messages.length;
    if (messageCountGrew) {
      final anchor = _earlierMessagesAnchor;
      _earlierMessagesAnchor = null;
      if (anchor != null &&
          anchor.threadId == widget.threadId &&
          widget.messages.length > anchor.messageCount) {
        _schedulePreserveEarlierMessagesAnchor(anchor);
      } else {
        _scheduleScrollToLatest();
      }
      return;
    }

    if (oldWidget.isLoadingEarlierMessages &&
        !widget.isLoadingEarlierMessages) {
      _earlierMessagesAnchor = null;
    }
  }

  @override
  void dispose() {
    _scrollRequestGeneration++;
    _scrollController.dispose();
    super.dispose();
  }

  void _handleLoadEarlierMessages() {
    if (_scrollController.hasClients) {
      final position = _scrollController.position;
      _earlierMessagesAnchor = _ConversationScrollAnchor(
        threadId: widget.threadId,
        messageCount: widget.messages.length,
        pixels: position.pixels,
        maxScrollExtent: position.maxScrollExtent,
      );
    }
    widget.onLoadEarlierMessages?.call();
  }

  void _scheduleScrollToLatest() {
    final requestGeneration = ++_scrollRequestGeneration;
    final threadId = widget.threadId;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          requestGeneration != _scrollRequestGeneration ||
          threadId != widget.threadId ||
          !_scrollController.hasClients) {
        return;
      }
      final position = _scrollController.position;
      _scrollController.jumpTo(position.maxScrollExtent);
    });
  }

  void _scheduleThreadInitialPosition() {
    final requestGeneration = ++_scrollRequestGeneration;
    final threadId = widget.threadId;
    final hasMessages = widget.messages.isNotEmpty;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          requestGeneration != _scrollRequestGeneration ||
          threadId != widget.threadId ||
          !_scrollController.hasClients) {
        return;
      }
      final position = _scrollController.position;
      _scrollController.jumpTo(
        hasMessages ? position.maxScrollExtent : position.minScrollExtent,
      );
    });
  }

  void _schedulePreserveEarlierMessagesAnchor(
    _ConversationScrollAnchor anchor,
  ) {
    final requestGeneration = ++_scrollRequestGeneration;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          requestGeneration != _scrollRequestGeneration ||
          anchor.threadId != widget.threadId ||
          !_scrollController.hasClients) {
        return;
      }
      final position = _scrollController.position;
      final insertedExtent = position.maxScrollExtent - anchor.maxScrollExtent;
      final target = (anchor.pixels + insertedExtent)
          .clamp(position.minScrollExtent, position.maxScrollExtent)
          .toDouble();
      _scrollController.jumpTo(target);
    });
  }

  @override
  Widget build(BuildContext context) {
    return widget._build(
      context,
      scrollController: _scrollController,
      onLoadEarlierMessages: widget.onLoadEarlierMessages == null
          ? null
          : _handleLoadEarlierMessages,
    );
  }
}

final class _ConversationScrollAnchor {
  const _ConversationScrollAnchor({
    required this.threadId,
    required this.messageCount,
    required this.pixels,
    required this.maxScrollExtent,
  });

  final String? threadId;
  final int messageCount;
  final double pixels;
  final double maxScrollExtent;
}

class _AgentBackground extends StatelessWidget {
  const _AgentBackground();

  @override
  Widget build(BuildContext context) {
    return const ExcludeSemantics(
      child: ColoredBox(color: SpeakUpDesign.canvas),
    );
  }
}

class _AgentTopBar extends StatelessWidget {
  const _AgentTopBar({
    required this.previewMode,
    required this.onOpenMenu,
    required this.onNavigateBack,
    required this.onCreateConversation,
    required this.isBusy,
  });

  final bool previewMode;
  final VoidCallback? onOpenMenu;
  final VoidCallback? onNavigateBack;
  final VoidCallback? onCreateConversation;
  final bool isBusy;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 48,
      child: Stack(
        alignment: Alignment.center,
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: _RoundGlassButton(
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
          ),
          Positioned(
            left: 56,
            right: 56,
            top: 0,
            bottom: 0,
            child: Center(
              child: FittedBox(
                fit: BoxFit.scaleDown,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Text(
                      'SpeakUp',
                      key: Key('conversation-fixed-title'),
                      style: SpeakUpDesign.cardTitle,
                    ),
                    if (previewMode) ...[
                      const SizedBox(width: 8),
                      const Text(
                        'UI Mock',
                        key: Key('agent-preview-label'),
                        style: SpeakUpDesign.meta,
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ),
          if (onNavigateBack == null && onCreateConversation != null)
            Align(
              alignment: Alignment.centerRight,
              child: _RoundGlassButton(
                key: const Key('conversation-create-button'),
                tooltip: '新对话',
                icon: Icons.add_rounded,
                onPressed: isBusy ? null : onCreateConversation,
              ),
            ),
        ],
      ),
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
      style: SpeakUpDesign.body,
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
    return Material(
      color: SpeakUpDesign.surface,
      shape: const CircleBorder(side: BorderSide(color: SpeakUpDesign.border)),
      child: IconButton(
        tooltip: tooltip,
        onPressed: onPressed,
        icon: Icon(icon, color: SpeakUpDesign.ink),
        iconSize: 24,
        constraints: const BoxConstraints.tightFor(width: 48, height: 48),
      ),
    );
  }
}

class _Greeting extends StatelessWidget {
  const _Greeting({required this.displayName});

  final String? displayName;

  @override
  Widget build(BuildContext context) {
    return Text(
      displayName == null ? '你好，今天想练什么？' : '你好，$displayName',
      style: SpeakUpDesign.sectionTitle.copyWith(
        color: SpeakUpDesign.secondary,
        fontWeight: FontWeight.w600,
      ),
    );
  }
}

class _NoFocusedConversation extends StatelessWidget {
  const _NoFocusedConversation({
    required this.onCreateConversation,
    required this.onOpenConversations,
    required this.isBusy,
  });

  final VoidCallback? onCreateConversation;
  final VoidCallback? onOpenConversations;
  final bool isBusy;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('no-focused-conversation-home'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('选择一个对话', style: SpeakUpDesign.pageTitle),
        const SizedBox(height: 10),
        const Text('打开近期对话，或创建一个新对话后再开始输入。', style: SpeakUpDesign.body),
        const SizedBox(height: 22),
        if (onCreateConversation != null)
          FilledButton.icon(
            key: const Key('no-focused-create-conversation'),
            onPressed: isBusy ? null : onCreateConversation,
            icon: const Icon(Icons.add_rounded),
            label: const Text('创建新对话'),
          ),
        if (onOpenConversations != null) ...[
          const SizedBox(height: 8),
          TextButton.icon(
            key: const Key('no-focused-open-conversations'),
            onPressed: onOpenConversations,
            icon: const Icon(Icons.chat_bubble_outline_rounded),
            label: const Text('打开近期对话'),
          ),
        ],
      ],
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
    final actions = <_QuickActionButton>[
      _QuickActionButton(
        actionKey: const Key('quick-action-create-plan'),
        icon: Icons.auto_awesome_outlined,
        label: '创建模拟面试',
        compact: compact,
        onPressed: onCreatePlan,
      ),
      if (onContinuePractice != null)
        _QuickActionButton(
          actionKey: const Key('quick-action-continue-practice'),
          icon: Icons.play_arrow_rounded,
          label: '继续上次练习',
          compact: compact,
          onPressed: onContinuePractice,
        ),
      _QuickActionButton(
        actionKey: const Key('quick-action-browse-scenes'),
        icon: Icons.grid_view_rounded,
        label: '浏览练习场景',
        compact: compact,
        onPressed: onCreatePlan,
      ),
      _QuickActionButton(
        actionKey: const Key('quick-action-recent-review'),
        icon: Icons.fact_check_outlined,
        label: '查看最近复盘',
        compact: compact,
        onPressed: onOpenReview,
      ),
    ];
    if (compact) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var index = 0; index < actions.length; index++) ...[
            actions[index],
            if (index != actions.length - 1) const SizedBox(height: 8),
          ],
        ],
      );
    }
    return LayoutBuilder(
      builder: (context, constraints) {
        final itemWidth = (constraints.maxWidth - 10) / 2;
        return Wrap(
          spacing: 10,
          runSpacing: 10,
          children: [
            for (final action in actions)
              SizedBox(width: itemWidth, child: action),
          ],
        );
      },
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
    return Semantics(
      key: actionKey,
      button: true,
      enabled: onPressed != null,
      label: label,
      onTap: onPressed,
      excludeSemantics: true,
      child: Material(
        color: SpeakUpDesign.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          side: const BorderSide(color: SpeakUpDesign.border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onPressed,
          child: Container(
            constraints: const BoxConstraints(
              minHeight: SpeakUpDesign.minTapTarget,
            ),
            padding: const EdgeInsets.symmetric(
              horizontal: SpeakUpDesign.space12,
              vertical: SpeakUpDesign.space12,
            ),
            child: Row(
              children: [
                Icon(icon, size: 19, color: SpeakUpDesign.primary),
                const SizedBox(width: SpeakUpDesign.space8),
                Expanded(
                  child: Text(
                    label,
                    maxLines: compact ? 2 : 1,
                    overflow: TextOverflow.ellipsis,
                    style: SpeakUpDesign.label,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _MessageList extends StatelessWidget {
  const _MessageList({required this.messages, this.voiceController});

  final List<AgentMessage> messages;
  final AgentVoiceController? voiceController;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('agent-message-list'),
      children: [
        for (final message in messages)
          AgentMessageBubble(
            message: message,
            voiceController: voiceController,
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
      color: SpeakUpDesign.errorMuted,
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
        child: Row(
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 20,
              color: SpeakUpDesign.error,
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
    required this.onStartVoice,
    required this.voiceController,
    required this.voiceEnabled,
    required this.onSubmitText,
    required this.enabled,
    required this.isBusy,
    super.key,
  });

  final bool keyboardVisible;
  final String? acceptedUserMessageId;
  final String? acceptedUserMessageText;
  final VoidCallback? onStartVoice;
  final AgentVoiceController? voiceController;
  final bool voiceEnabled;
  final Future<bool> Function(String)? onSubmitText;
  final bool enabled;
  final bool isBusy;

  @override
  State<_AgentComposer> createState() => _AgentComposerState();
}

class _AgentComposerState extends State<_AgentComposer> {
  final _controller = TextEditingController();
  bool _suppressControllerNotifications = false;
  String _draftBeforeVoice = '';

  @override
  void initState() {
    super.initState();
    _controller.addListener(_handleTextChanged);
    widget.voiceController?.addListener(_handleVoiceChanged);
  }

  @override
  void didUpdateWidget(covariant _AgentComposer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.voiceController != widget.voiceController) {
      oldWidget.voiceController?.removeListener(_handleVoiceChanged);
      widget.voiceController?.addListener(_handleVoiceChanged);
    }
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
    widget.voiceController?.removeListener(_handleVoiceChanged);
    _controller
      ..removeListener(_handleTextChanged)
      ..dispose();
    super.dispose();
  }

  void _handleTextChanged() {
    if (!_suppressControllerNotifications) {
      final voice = widget.voiceController;
      if (voice?.state == AgentVoiceComposerState.awaitingConfirmation) {
        voice?.updateTranscript(_controller.text);
      }
      setState(() {});
    }
  }

  void _handleVoiceChanged() {
    if (!mounted) {
      return;
    }
    final voice = widget.voiceController;
    if (voice?.state == AgentVoiceComposerState.awaitingConfirmation &&
        _controller.text != voice?.editedTranscript) {
      _suppressControllerNotifications = true;
      _controller.value = TextEditingValue(
        text: voice!.editedTranscript,
        selection: TextSelection.collapsed(
          offset: voice.editedTranscript.length,
        ),
      );
      _suppressControllerNotifications = false;
    }
    setState(() {});
  }

  void _startVoice() {
    _draftBeforeVoice = _controller.text;
    widget.onStartVoice?.call();
  }

  Future<void> _stopVoice() async {
    final voice = widget.voiceController;
    if (voice == null) {
      return;
    }
    await voice.stopRecordingAndUpload();
  }

  Future<void> _cancelVoice() async {
    final voice = widget.voiceController;
    if (voice == null ||
        voice.state == AgentVoiceComposerState.confirming ||
        voice.state == AgentVoiceComposerState.awaitingAssistant) {
      return;
    }
    await voice.cancel();
    if (!mounted) {
      return;
    }
    _suppressControllerNotifications = true;
    _controller.value = TextEditingValue(
      text: _draftBeforeVoice,
      selection: TextSelection.collapsed(offset: _draftBeforeVoice.length),
    );
    _suppressControllerNotifications = false;
    setState(() {});
  }

  Future<void> _submit() async {
    final text = _controller.text.trim();
    if (!widget.enabled ||
        text.isEmpty ||
        widget.isBusy ||
        widget.onSubmitText == null) {
      return;
    }
    final sent = await widget.onSubmitText!(text);
    if (mounted && sent) {
      _controller.clear();
    }
  }

  @override
  Widget build(BuildContext context) {
    final voice = widget.voiceController;
    final voiceState = voice?.state ?? AgentVoiceComposerState.idle;
    final recording = voiceState == AgentVoiceComposerState.recording;
    final confirmingText =
        voiceState == AgentVoiceComposerState.awaitingConfirmation;
    final voiceProgress =
        voiceState == AgentVoiceComposerState.starting ||
        voiceState == AgentVoiceComposerState.uploading ||
        voiceState == AgentVoiceComposerState.transcribing ||
        voiceState == AgentVoiceComposerState.confirming ||
        voiceState == AgentVoiceComposerState.awaitingAssistant;
    final voiceSubmissionInFlight =
        voiceState == AgentVoiceComposerState.confirming ||
        voiceState == AgentVoiceComposerState.awaitingAssistant;
    final voiceFailure = voiceState == AgentVoiceComposerState.failed;
    return AnimatedContainer(
      key: const Key('agent-composer-surface'),
      duration: const Duration(milliseconds: 180),
      curve: Curves.easeOut,
      constraints: BoxConstraints(minHeight: widget.keyboardVisible ? 56 : 58),
      padding: const EdgeInsets.fromLTRB(8, 7, 7, 7),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      child: recording || voiceProgress || voiceFailure
          ? Row(
              children: [
                if (!voiceSubmissionInFlight) ...[
                  IconButton(
                    key: const Key('agent-voice-cancel'),
                    tooltip: '取消语音输入',
                    onPressed: _cancelVoice,
                    constraints: const BoxConstraints.tightFor(
                      width: 40,
                      height: 40,
                    ),
                    padding: EdgeInsets.zero,
                    icon: const Icon(Icons.close_rounded, size: 21),
                  ),
                  const SizedBox(width: 4),
                ],
                if (recording) ...[
                  const Icon(
                    Icons.fiber_manual_record_rounded,
                    size: 12,
                    color: SpeakUpDesign.error,
                  ),
                  const SizedBox(width: 8),
                ] else if (voiceProgress) ...[
                  const SizedBox.square(
                    dimension: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  const SizedBox(width: 8),
                ],
                Expanded(
                  child: Text(
                    recording
                        ? '正在录音'
                        : voiceFailure
                        ? voice?.errorMessage ?? '语音识别失败'
                        : _composerVoiceStateLabel(voiceState),
                    key: const Key('agent-voice-state-label'),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      color: voiceFailure
                          ? SpeakUpDesign.error
                          : SpeakUpDesign.secondary,
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                      height: 1.3,
                    ),
                  ),
                ),
                if (recording) ...[
                  const SizedBox(width: 8),
                  Text(
                    _formatComposerDuration(voice!.recordingElapsed),
                    key: const Key('agent-voice-recording-duration'),
                    style: const TextStyle(
                      color: SpeakUpDesign.secondary,
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(width: 6),
                  IconButton.filled(
                    key: const Key('agent-voice-stop'),
                    tooltip: '结束录音并自动转写',
                    onPressed: _stopVoice,
                    constraints: const BoxConstraints.tightFor(
                      width: 40,
                      height: 40,
                    ),
                    padding: EdgeInsets.zero,
                    icon: const Icon(Icons.stop_rounded, size: 20),
                  ),
                ] else if (voiceFailure && voice?.canRetry == true)
                  IconButton(
                    key: const Key('agent-voice-retry'),
                    tooltip: '重试',
                    onPressed: voice?.retry,
                    icon: const Icon(Icons.refresh_rounded),
                  ),
              ],
            )
          : Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                if (confirmingText)
                  IconButton(
                    key: const Key('agent-voice-cancel'),
                    tooltip: '取消语音输入',
                    onPressed: _cancelVoice,
                    constraints: const BoxConstraints.tightFor(
                      width: 40,
                      height: 40,
                    ),
                    padding: EdgeInsets.zero,
                    icon: const Icon(Icons.close_rounded, size: 21),
                  ),
                Expanded(
                  child: TextField(
                    key: const Key('agent-composer-field'),
                    controller: _controller,
                    enabled:
                        confirmingText || (widget.enabled && !widget.isBusy),
                    minLines: 1,
                    maxLines: widget.keyboardVisible ? 3 : 2,
                    inputFormatters: <TextInputFormatter>[
                      _agentContentFormatter,
                    ],
                    textInputAction: TextInputAction.send,
                    onSubmitted: (_) => confirmingText
                        ? widget.voiceController?.confirm()
                        : _submit(),
                    style: const TextStyle(
                      color: SpeakUpDesign.ink,
                      fontSize: 15,
                      height: 1.4,
                    ),
                    decoration: InputDecoration(
                      hintText: widget.enabled
                          ? confirmingText
                                ? '检查识别文字'
                                : '问问 SpeakUp'
                          : '请先选择或创建对话',
                      hintStyle: const TextStyle(
                        color: SpeakUpDesign.tertiary,
                        fontSize: 15,
                      ),
                      border: InputBorder.none,
                      isDense: true,
                      contentPadding: const EdgeInsets.fromLTRB(7, 9, 6, 8),
                    ),
                  ),
                ),
                if (!confirmingText && widget.onStartVoice != null) ...[
                  Semantics(
                    key: const Key('agent-mic-placeholder'),
                    button: true,
                    enabled: widget.voiceEnabled,
                    label: '录制 Agent 语音消息',
                    onTap: widget.voiceEnabled ? _startVoice : null,
                    child: ExcludeSemantics(
                      child: IconButton(
                        tooltip: '录制 Agent 语音消息',
                        onPressed: widget.voiceEnabled ? _startVoice : null,
                        constraints: const BoxConstraints.tightFor(
                          width: 40,
                          height: 40,
                        ),
                        padding: EdgeInsets.zero,
                        color: SpeakUpDesign.secondary,
                        icon: const Icon(Icons.mic_none_rounded, size: 21),
                      ),
                    ),
                  ),
                  const SizedBox(width: 2),
                ],
                IconButton.filled(
                  key: Key(
                    confirmingText
                        ? 'agent-voice-confirm'
                        : 'agent-send-button',
                  ),
                  tooltip: '发送',
                  onPressed:
                      _controller.text.trim().isEmpty ||
                          (widget.isBusy && !confirmingText) ||
                          !widget.enabled
                      ? null
                      : confirmingText
                      ? voice?.canConfirm == true
                            ? voice?.confirm
                            : null
                      : widget.onSubmitText == null
                      ? null
                      : _submit,
                  constraints: const BoxConstraints.tightFor(
                    width: 40,
                    height: 40,
                  ),
                  padding: EdgeInsets.zero,
                  icon: const Icon(Icons.arrow_upward_rounded, size: 20),
                ),
              ],
            ),
    );
  }
}

String _composerVoiceStateLabel(AgentVoiceComposerState state) {
  return switch (state) {
    AgentVoiceComposerState.starting => '正在打开麦克风…',
    AgentVoiceComposerState.uploading => '正在处理语音…',
    AgentVoiceComposerState.transcribing => '正在转写…',
    AgentVoiceComposerState.confirming => '正在发送…',
    AgentVoiceComposerState.awaitingAssistant => 'SpeakUp 正在回复…',
    _ => '正在处理…',
  };
}

String _formatComposerDuration(Duration value) {
  final totalSeconds = value.inSeconds.clamp(0, 3599);
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes:${seconds.toString().padLeft(2, '0')}';
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
