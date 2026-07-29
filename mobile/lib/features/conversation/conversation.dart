/// Conversation module boundary.
library;

import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_controller.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/agent/agent_voice_widgets.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';

typedef ConversationVoiceStarter = FutureOr<void> Function();

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
    this.onMessageAction,
    ConversationVoiceStarter? onStartVoice,
    ConversationVoiceStarter? onVoicePlaceholder,
    this.onCreateConversation,
    this.draftThreadRecoveryGeneration = 0,
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
  final ValueChanged<AgentMessageAction>? onMessageAction;
  final ConversationVoiceStarter? onStartVoice;
  final VoidCallback? onCreateConversation;
  final int draftThreadRecoveryGeneration;
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
    final canCompose = hasFocusedThread || onCreateConversation != null;

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
                            if (messages.isEmpty) ...[
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
                                onAction: onMessageAction,
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
                        threadId: threadId,
                        draftThreadRecoveryGeneration:
                            draftThreadRecoveryGeneration,
                        keyboardVisible: keyboardVisible,
                        acceptedUserMessageId: acceptedUserMessage?.id,
                        acceptedUserMessageText: acceptedUserMessage?.text,
                        onStartVoice: onStartVoice,
                        voiceController: voiceController,
                        voiceEnabled: voiceController != null && !isBusy,
                        onSubmitText: onSubmitText,
                        enabled: canCompose,
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
  const _MessageList({
    required this.messages,
    this.voiceController,
    this.onAction,
  });

  final List<AgentMessage> messages;
  final AgentVoiceController? voiceController;
  final ValueChanged<AgentMessageAction>? onAction;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('agent-message-list'),
      children: [
        for (final message in messages)
          AgentMessageBubble(
            message: message,
            voiceController: voiceController,
            onAction: onAction,
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
    required this.threadId,
    required this.draftThreadRecoveryGeneration,
    required this.keyboardVisible,
    required this.acceptedUserMessageId,
    required this.acceptedUserMessageText,
    required this.onStartVoice,
    required this.voiceController,
    required this.voiceEnabled,
    required this.onSubmitText,
    required this.enabled,
    required this.isBusy,
  });

  final String? threadId;
  final int draftThreadRecoveryGeneration;
  final bool keyboardVisible;
  final String? acceptedUserMessageId;
  final String? acceptedUserMessageText;
  final ConversationVoiceStarter? onStartVoice;
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
  final _focusNode = FocusNode();
  bool _suppressControllerNotifications = false;
  bool _textSubmissionInFlight = false;
  bool _draftMaterializationPending = false;
  bool _voiceMaterializationPending = false;
  bool _textMode = false;
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
    if (oldWidget.threadId != widget.threadId) {
      final preserveMaterializedDraft =
          oldWidget.threadId == null &&
          widget.threadId != null &&
          (_draftMaterializationPending ||
              _voiceMaterializationPending ||
              oldWidget.draftThreadRecoveryGeneration !=
                  widget.draftThreadRecoveryGeneration);
      _draftMaterializationPending = false;
      _voiceMaterializationPending = false;
      if (!preserveMaterializedDraft && _controller.text.isNotEmpty) {
        _draftBeforeVoice = '';
        _suppressControllerNotifications = true;
        _controller.clear();
        _suppressControllerNotifications = false;
      }
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
    _focusNode.dispose();
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
    if (voice?.state == AgentVoiceComposerState.awaitingConfirmation) {
      _textMode = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _focusNode.requestFocus();
        }
      });
    }
    setState(() {});
  }

  Future<void> _startVoice() async {
    _draftBeforeVoice = _controller.text;
    _voiceMaterializationPending = widget.threadId == null;
    try {
      await widget.onStartVoice?.call();
    } finally {
      if (mounted && _voiceMaterializationPending) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && widget.threadId == null) {
            _voiceMaterializationPending = false;
          }
        });
      }
    }
  }

  Future<void> _sendVoiceMessage() async {
    final voice = widget.voiceController;
    if (voice == null) {
      return;
    }
    await voice.stopRecordingAndUpload();
    if (!mounted || !voice.canConfirm) {
      return;
    }
    await voice.confirm();
    if (!mounted || voice.editedTranscript.isNotEmpty) {
      return;
    }
    _replaceComposerText(_draftBeforeVoice);
  }

  Future<void> _convertVoiceToText() async {
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
    _replaceComposerText(_draftBeforeVoice);
  }

  void _replaceComposerText(String value) {
    _suppressControllerNotifications = true;
    _controller.value = TextEditingValue(
      text: value,
      selection: TextSelection.collapsed(offset: value.length),
    );
    _suppressControllerNotifications = false;
    setState(() => _textMode = value.isNotEmpty);
    if (value.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _focusNode.requestFocus();
        }
      });
    }
  }

  void _showTextComposer() {
    setState(() => _textMode = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _focusNode.requestFocus();
      }
    });
  }

  void _showVoiceComposer() {
    _focusNode.unfocus();
    setState(() => _textMode = false);
  }

  Future<void> _submitConvertedText() async {
    final voice = widget.voiceController;
    final text = _controller.text.trim();
    if (voice == null ||
        !voice.canConfirm ||
        text.isEmpty ||
        _textSubmissionInFlight ||
        widget.onSubmitText == null) {
      return;
    }
    setState(() => _textSubmissionInFlight = true);
    try {
      await voice.cancel();
      final sent = await widget.onSubmitText!(text);
      if (mounted && sent) {
        _controller.clear();
      }
    } finally {
      if (mounted) {
        setState(() => _textSubmissionInFlight = false);
      }
    }
  }

  Future<void> _submit() async {
    final text = _controller.text.trim();
    if (!widget.enabled ||
        text.isEmpty ||
        widget.isBusy ||
        _textSubmissionInFlight ||
        widget.onSubmitText == null) {
      return;
    }
    setState(() => _textSubmissionInFlight = true);
    _draftMaterializationPending = widget.threadId == null;
    try {
      final sent = await widget.onSubmitText!(text);
      if (mounted && sent) {
        _controller.clear();
      }
    } finally {
      if (mounted) {
        setState(() => _textSubmissionInFlight = false);
        if (_draftMaterializationPending) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted && widget.threadId == null) {
              _draftMaterializationPending = false;
            }
          });
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final voice = widget.voiceController;
    final voiceState = voice?.state ?? AgentVoiceComposerState.idle;
    final starting = voiceState == AgentVoiceComposerState.starting;
    final recording = voiceState == AgentVoiceComposerState.recording;
    final confirmingText =
        voiceState == AgentVoiceComposerState.awaitingConfirmation;
    final voiceProgress =
        voiceState == AgentVoiceComposerState.uploading ||
        voiceState == AgentVoiceComposerState.transcribing ||
        voiceState == AgentVoiceComposerState.confirming ||
        voiceState == AgentVoiceComposerState.awaitingAssistant;
    final voiceFailure = voiceState == AgentVoiceComposerState.failed;
    final capturePhase = switch (voiceState) {
      AgentVoiceComposerState.idle => VoiceCapturePhase.idle,
      AgentVoiceComposerState.starting => VoiceCapturePhase.starting,
      AgentVoiceComposerState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    return VoiceCaptureControl(
      phase: capturePhase,
      enabled: widget.voiceEnabled && widget.onStartVoice != null,
      onStart: _startVoice,
      onFinish: _sendVoiceMessage,
      onConvertToText: _convertVoiceToText,
      onCancel: _cancelVoice,
      builder: (context, capture) => Material(
        key: const Key('agent-composer-surface'),
        color: SpeakUpDesign.surface,
        shape: const Border(top: BorderSide(color: SpeakUpDesign.border)),
        child: SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(0, 14, 0, 12),
            child: voiceProgress || voiceFailure
                ? _AgentVoiceStatusDock(
                    label: voiceFailure
                        ? voice?.errorMessage ?? '语音识别失败'
                        : _composerVoiceStateLabel(voiceState),
                    failed: voiceFailure,
                    canCancel:
                        voiceState != AgentVoiceComposerState.confirming &&
                        voiceState != AgentVoiceComposerState.awaitingAssistant,
                    onCancel: _cancelVoice,
                    onRetry: voiceFailure && voice?.canRetry == true
                        ? voice?.retry
                        : null,
                  )
                : confirmingText || _textMode
                ? _AgentTextDock(
                    controller: _controller,
                    focusNode: _focusNode,
                    enabled:
                        confirmingText || (widget.enabled && !widget.isBusy),
                    confirmingText: confirmingText,
                    keyboardVisible: widget.keyboardVisible,
                    submissionInFlight: _textSubmissionInFlight,
                    canSubmit:
                        _controller.text.trim().isNotEmpty &&
                        widget.enabled &&
                        (!widget.isBusy || confirmingText) &&
                        widget.onSubmitText != null &&
                        (!confirmingText || voice?.canConfirm == true),
                    onReturnToVoice: confirmingText
                        ? _cancelVoice
                        : _showVoiceComposer,
                    onSubmit: confirmingText ? _submitConvertedText : _submit,
                  )
                : _AgentVoiceDock(
                    capture: capture,
                    enabled: widget.voiceEnabled && widget.onStartVoice != null,
                    preparing: starting,
                    recording: recording,
                    elapsed: voice?.recordingElapsed ?? Duration.zero,
                    onOpenKeyboard: _showTextComposer,
                  ),
          ),
        ),
      ),
    );
  }
}

class _AgentVoiceDock extends StatelessWidget {
  const _AgentVoiceDock({
    required this.capture,
    required this.enabled,
    required this.preparing,
    required this.recording,
    required this.elapsed,
    required this.onOpenKeyboard,
  });

  final VoiceCaptureView capture;
  final bool enabled;
  final bool preparing;
  final bool recording;
  final Duration elapsed;
  final VoidCallback onOpenKeyboard;

  @override
  Widget build(BuildContext context) {
    final capturing = preparing || recording;
    final showTargets = capture.holdStarted || (capture.tapMode && capturing);
    final label = !capturing
        ? '点击或按住说话'
        : preparing
        ? '正在打开麦克风…'
        : capture.tapMode
        ? '请选择发送方式'
        : switch (capture.releaseIntent) {
            VoiceCaptureReleaseIntent.finish => '松开发送语音',
            VoiceCaptureReleaseIntent.convertToText => '松开转成文字',
            VoiceCaptureReleaseIntent.cancel => '上滑选择发送方式',
          };
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (showTargets) ...[
          _AgentVoiceTargets(
            intent: capture.releaseIntent,
            elapsed: elapsed,
            interactive: capture.tapMode,
            onSendVoice: capture.finishTapCapture,
            onConvertToText: capture.convertTapCapture,
          ),
          const SizedBox(height: 10),
        ],
        Row(
          children: [
            Expanded(
              child: capture.wrapTarget(
                key: const Key('agent-mic-placeholder'),
                semanticsLabel: capturing ? label : '点击或按住说话',
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 100),
                  height: 56,
                  decoration: BoxDecoration(
                    color: capturing || (enabled && capture.pressed)
                        ? SpeakUpDesign.primary.withValues(alpha: 0.82)
                        : enabled
                        ? SpeakUpDesign.primary
                        : SpeakUpDesign.tertiary,
                    borderRadius: BorderRadius.circular(
                      SpeakUpDesign.radiusControl,
                    ),
                  ),
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Icon(
                        capturing
                            ? Icons.graphic_eq_rounded
                            : Icons.mic_rounded,
                        color: Colors.white,
                      ),
                      const SizedBox(width: 10),
                      Flexible(
                        child: Text(
                          label,
                          key: const Key('agent-voice-state-label'),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 16,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                      if (recording && !showTargets) ...[
                        const SizedBox(width: 10),
                        Text(
                          _formatComposerDuration(elapsed),
                          key: const Key('agent-voice-recording-duration'),
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 14,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ),
            const SizedBox(width: 10),
            IconButton.outlined(
              key: Key(
                capturing ? 'agent-voice-cancel' : 'agent-open-keyboard',
              ),
              tooltip: capturing ? '取消录音' : '键盘输入',
              onPressed: capturing ? capture.cancelTapCapture : onOpenKeyboard,
              icon: Icon(
                capturing ? Icons.close_rounded : Icons.keyboard_alt_outlined,
              ),
              style: IconButton.styleFrom(minimumSize: const Size.square(56)),
            ),
          ],
        ),
      ],
    );
  }
}

class _AgentVoiceTargets extends StatelessWidget {
  const _AgentVoiceTargets({
    required this.intent,
    required this.elapsed,
    required this.interactive,
    required this.onSendVoice,
    required this.onConvertToText,
  });

  final VoiceCaptureReleaseIntent intent;
  final Duration elapsed;
  final bool interactive;
  final VoidCallback onSendVoice;
  final VoidCallback onConvertToText;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('agent-voice-targets'),
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          _formatComposerDuration(elapsed),
          key: const Key('agent-voice-target-duration'),
          style: SpeakUpDesign.meta.copyWith(fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            Expanded(
              child: _AgentVoiceTarget(
                key: const Key('agent-voice-target-send'),
                active: intent == VoiceCaptureReleaseIntent.finish,
                icon: Icons.mic_rounded,
                label: '发语音',
                onPressed: interactive ? onSendVoice : null,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: _AgentVoiceTarget(
                key: const Key('agent-voice-target-convert'),
                active: intent == VoiceCaptureReleaseIntent.convertToText,
                icon: Icons.text_fields_rounded,
                label: '转文字',
                onPressed: interactive ? onConvertToText : null,
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _AgentVoiceTarget extends StatelessWidget {
  const _AgentVoiceTarget({
    required this.active,
    required this.icon,
    required this.label,
    required this.onPressed,
    super.key,
  });

  final bool active;
  final IconData icon;
  final String label;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final content = AnimatedContainer(
      duration: const Duration(milliseconds: 100),
      height: 64,
      decoration: BoxDecoration(
        color: active ? SpeakUpDesign.primary : SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        border: Border.all(
          color: active ? SpeakUpDesign.primary : SpeakUpDesign.border,
        ),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            icon,
            color: active ? Colors.white : SpeakUpDesign.ink,
            size: 21,
          ),
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: active ? Colors.white : SpeakUpDesign.ink,
                fontSize: 15,
                fontWeight: FontWeight.w700,
              ),
            ),
          ),
        ],
      ),
    );
    if (onPressed == null) {
      return content;
    }
    return Semantics(
      button: true,
      label: label,
      child: InkWell(
        onTap: onPressed,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        child: content,
      ),
    );
  }
}

class _AgentTextDock extends StatelessWidget {
  const _AgentTextDock({
    required this.controller,
    required this.focusNode,
    required this.enabled,
    required this.confirmingText,
    required this.keyboardVisible,
    required this.submissionInFlight,
    required this.canSubmit,
    required this.onReturnToVoice,
    required this.onSubmit,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool enabled;
  final bool confirmingText;
  final bool keyboardVisible;
  final bool submissionInFlight;
  final bool canSubmit;
  final VoidCallback onReturnToVoice;
  final VoidCallback onSubmit;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(7, 7, 7, 7),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            IconButton(
              key: Key(
                confirmingText ? 'agent-voice-cancel' : 'agent-return-to-voice',
              ),
              tooltip: confirmingText ? '取消语音输入' : '切换到语音',
              onPressed: onReturnToVoice,
              constraints: const BoxConstraints.tightFor(width: 40, height: 40),
              padding: EdgeInsets.zero,
              icon: Icon(
                confirmingText ? Icons.close_rounded : Icons.mic_none_rounded,
                size: 21,
              ),
            ),
            const SizedBox(width: 2),
            Expanded(
              child: TextField(
                key: const Key('agent-composer-field'),
                controller: controller,
                focusNode: focusNode,
                enabled: enabled,
                minLines: 1,
                maxLines: keyboardVisible ? 3 : 2,
                inputFormatters: <TextInputFormatter>[_agentContentFormatter],
                textInputAction: TextInputAction.send,
                onSubmitted: (_) => onSubmit(),
                style: const TextStyle(
                  color: SpeakUpDesign.ink,
                  fontSize: 15,
                  height: 1.4,
                ),
                decoration: InputDecoration(
                  hintText: enabled
                      ? confirmingText
                            ? '检查识别文字'
                            : '问问 SpeakUp'
                      : '暂时无法开始对话',
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
            IconButton.filled(
              key: Key(
                confirmingText ? 'agent-voice-confirm' : 'agent-send-button',
              ),
              tooltip: '发送',
              onPressed: canSubmit && !submissionInFlight ? onSubmit : null,
              constraints: const BoxConstraints.tightFor(width: 40, height: 40),
              padding: EdgeInsets.zero,
              icon: const Icon(Icons.arrow_upward_rounded, size: 20),
            ),
          ],
        ),
      ),
    );
  }
}

class _AgentVoiceStatusDock extends StatelessWidget {
  const _AgentVoiceStatusDock({
    required this.label,
    required this.failed,
    required this.canCancel,
    required this.onCancel,
    required this.onRetry,
  });

  final String label;
  final bool failed;
  final bool canCancel;
  final VoidCallback onCancel;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minHeight: 56),
      padding: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      child: Row(
        children: [
          if (canCancel)
            IconButton(
              key: const Key('agent-voice-cancel'),
              tooltip: '取消语音输入',
              onPressed: onCancel,
              icon: const Icon(Icons.close_rounded, size: 21),
            ),
          if (!failed) ...[
            const SizedBox.square(
              dimension: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            const SizedBox(width: 10),
          ],
          Expanded(
            child: Text(
              label,
              key: const Key('agent-voice-state-label'),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: failed ? SpeakUpDesign.error : SpeakUpDesign.secondary,
                fontSize: 14,
                fontWeight: FontWeight.w600,
                height: 1.3,
              ),
            ),
          ),
          if (onRetry != null)
            IconButton(
              key: const Key('agent-voice-retry'),
              tooltip: '重试',
              onPressed: onRetry,
              icon: const Icon(Icons.refresh_rounded),
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
