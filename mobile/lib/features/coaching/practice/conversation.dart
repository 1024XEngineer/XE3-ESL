/// Conversation module boundary.
library;

import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/agent/agent_image_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_voice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/agent/agent_voice_widgets.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_disclosure.dart';

typedef ConversationVoiceStarter = FutureOr<void> Function();
typedef ConversationPendingImageAction = FutureOr<void> Function(String);
typedef ConversationMessageImageAction =
    FutureOr<void> Function(String messageId, String imageAssetId);

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
    this.onBrowseScenes,
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
    this.pendingImages = const <AgentPendingImage>[],
    this.imageErrorMessage,
    this.imageSelectionInFlight = false,
    this.onPickImages,
    this.onTakePhoto,
    this.onRemovePendingImage,
    this.onRetryPendingImage,
    this.onRefreshMessageImage,
    this.speechFeedbackController,
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
  final VoidCallback? onBrowseScenes;
  final VoidCallback? onContinuePractice;
  final VoidCallback? onOpenReview;
  final ValueChanged<AgentMessageAction>? onMessageAction;
  final ConversationVoiceStarter? onStartVoice;
  final VoidCallback? onCreateConversation;
  final int draftThreadRecoveryGeneration;
  final List<AgentMessage> messages;
  final SceneDefinition? activeScene;
  final bool hasFocusedThread;
  final bool hasEarlierMessages;
  final bool isLoadingEarlierMessages;
  final bool isBusy;
  final String? errorMessage;
  final Future<bool> Function(String)? onSubmitText;
  final VoidCallback? onRetryOperation;
  final VoidCallback? onLoadEarlierMessages;
  final AgentVoiceController? voiceController;
  final List<AgentPendingImage> pendingImages;
  final String? imageErrorMessage;
  final bool imageSelectionInFlight;
  final ConversationVoiceStarter? onPickImages;
  final ConversationVoiceStarter? onTakePhoto;
  final ConversationPendingImageAction? onRemovePendingImage;
  final ConversationPendingImageAction? onRetryPendingImage;
  final ConversationMessageImageAction? onRefreshMessageImage;
  final SpeechFeedbackController? speechFeedbackController;

  @override
  State<ConversationPage> createState() => _ConversationPageState();

  Widget _build(
    BuildContext context, {
    required ScrollController scrollController,
    required VoidCallback? onLoadEarlierMessages,
    required bool showJumpToLatest,
    required VoidCallback onJumpToLatest,
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
                                  onBrowseScenes: onBrowseScenes,
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
                                        scene.name,
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
                                onRefreshImage: onRefreshMessageImage,
                                speechFeedbackController:
                                    speechFeedbackController,
                                feedbackSourceKey: (message) =>
                                    _agentFeedbackSourceKey(threadId, message),
                                onRepractice: !isBusy && onStartVoice != null
                                    ? (item) {
                                        if (item.repracticeMode ==
                                            SpeechFeedbackRepracticeMode
                                                .sameThread) {
                                          unawaited(
                                            Future<void>.sync(onStartVoice!),
                                          );
                                        }
                                      }
                                    : null,
                              ),
                            ],
                            if (isBusy) ...[
                              const SizedBox(height: 14),
                              Center(
                                child: Semantics(
                                  label: 'SpeakUp 正在处理',
                                  child: const Wrap(
                                    key: Key('agent-operation-progress'),
                                    alignment: WrapAlignment.center,
                                    crossAxisAlignment:
                                        WrapCrossAlignment.center,
                                    spacing: 10,
                                    children: [
                                      SizedBox.square(
                                        dimension: 18,
                                        child: CircularProgressIndicator(
                                          strokeWidth: 2,
                                        ),
                                      ),
                                      Text(
                                        'SpeakUp 正在回复…',
                                        style: SpeakUpDesign.meta,
                                      ),
                                    ],
                                  ),
                                ),
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
                        pendingImages: pendingImages,
                        imageErrorMessage: imageErrorMessage,
                        imageSelectionInFlight: imageSelectionInFlight,
                        onPickImages: onPickImages,
                        onTakePhoto: onTakePhoto,
                        onRemovePendingImage: onRemovePendingImage,
                        onRetryPendingImage: onRetryPendingImage,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
          if (showJumpToLatest)
            Positioned(
              right: horizontalPadding,
              bottom: composerBottom + 92,
              child: FloatingActionButton.small(
                key: const Key('agent-jump-to-latest'),
                tooltip: '查看最新回复',
                onPressed: onJumpToLatest,
                backgroundColor: SpeakUpDesign.surface,
                foregroundColor: SpeakUpDesign.primary,
                child: const Icon(Icons.arrow_downward_rounded),
              ),
            ),
        ],
      ),
    );
  }
}

class _ConversationPageState extends State<ConversationPage> {
  final ScrollController _scrollController = ScrollController();
  final Map<String, String> _feedbackSources = {};
  _ConversationScrollAnchor? _earlierMessagesAnchor;
  int _scrollRequestGeneration = 0;
  bool _showJumpToLatest = false;
  bool _feedbackRebuildScheduled = false;
  bool _voiceRebuildScheduled = false;

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_handleScroll);
    _scheduleThreadInitialPosition();
    _syncSpeechFeedbackSources();
    widget.speechFeedbackController?.addListener(_handleFeedbackState);
    widget.voiceController?.addListener(_handleConversationVoiceState);
  }

  @override
  void didUpdateWidget(covariant ConversationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.speechFeedbackController != widget.speechFeedbackController) {
      oldWidget.speechFeedbackController?.removeListener(_handleFeedbackState);
      _removeSpeechFeedbackSources(oldWidget.speechFeedbackController);
      widget.speechFeedbackController?.addListener(_handleFeedbackState);
    }
    if (oldWidget.voiceController != widget.voiceController) {
      oldWidget.voiceController?.removeListener(_handleConversationVoiceState);
      widget.voiceController?.addListener(_handleConversationVoiceState);
    }
    _syncSpeechFeedbackSources();
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
      } else if (_isNearLatest()) {
        _scheduleScrollToLatest();
      } else {
        _setJumpToLatestVisible(true);
      }
      return;
    }

    final previousLast = oldWidget.messages.lastOrNull;
    final currentLast = widget.messages.lastOrNull;
    if (previousLast?.id == currentLast?.id &&
        previousLast?.text != currentLast?.text) {
      if (_isNearLatest()) {
        _scheduleScrollToLatest();
      } else {
        _setJumpToLatestVisible(true);
      }
    }

    if (oldWidget.isLoadingEarlierMessages &&
        !widget.isLoadingEarlierMessages) {
      _earlierMessagesAnchor = null;
    }
  }

  @override
  void dispose() {
    _scrollRequestGeneration++;
    _scrollController.removeListener(_handleScroll);
    widget.speechFeedbackController?.removeListener(_handleFeedbackState);
    widget.voiceController?.removeListener(_handleConversationVoiceState);
    _removeSpeechFeedbackSources(widget.speechFeedbackController);
    _scrollController.dispose();
    super.dispose();
  }

  bool _isNearLatest() {
    if (!_scrollController.hasClients) {
      return true;
    }
    final position = _scrollController.position;
    return position.maxScrollExtent - position.pixels <= 120;
  }

  void _handleScroll() {
    if (_showJumpToLatest && _isNearLatest()) {
      setState(() => _showJumpToLatest = false);
    }
  }

  void _setJumpToLatestVisible(bool value) {
    if (_showJumpToLatest == value) {
      return;
    }
    setState(() => _showJumpToLatest = value);
  }

  void _syncSpeechFeedbackSources() {
    final controller = widget.speechFeedbackController;
    if (controller == null) {
      _feedbackSources.clear();
      return;
    }
    final current = <String, String>{};
    for (final message in widget.messages) {
      final statusUrl = message.speechFeedbackStatusUrl;
      if (statusUrl != null) {
        current[_agentFeedbackSourceKey(widget.threadId, message)] = statusUrl;
      }
    }
    for (final entry in _feedbackSources.entries.toList()) {
      if (current[entry.key] != entry.value) {
        controller.removeSource(entry.key);
        _feedbackSources.remove(entry.key);
      }
    }
    for (final entry in current.entries) {
      if (_feedbackSources[entry.key] == entry.value) {
        continue;
      }
      _feedbackSources[entry.key] = entry.value;
      unawaited(controller.load(sourceKey: entry.key, statusUrl: entry.value));
    }
  }

  void _removeSpeechFeedbackSources(SpeechFeedbackController? controller) {
    if (controller != null) {
      for (final sourceKey in _feedbackSources.keys) {
        controller.removeSource(sourceKey);
      }
    }
    _feedbackSources.clear();
  }

  void _handleFeedbackState() {
    if (_feedbackRebuildScheduled) {
      return;
    }
    _feedbackRebuildScheduled = true;
    scheduleMicrotask(() {
      _feedbackRebuildScheduled = false;
      if (mounted) {
        setState(() {});
      }
    });
  }

  void _handleConversationVoiceState() {
    if (_voiceRebuildScheduled) {
      return;
    }
    _voiceRebuildScheduled = true;
    scheduleMicrotask(() {
      _voiceRebuildScheduled = false;
      if (!mounted) {
        return;
      }
      final keepLatestVisible = _isNearLatest();
      final voiceState = widget.voiceController?.state;
      setState(() {});
      if (voiceState == AgentVoiceComposerState.awaitingAssistant ||
          (keepLatestVisible &&
              voiceState == AgentVoiceComposerState.confirming)) {
        _scheduleScrollToLatest();
      }
    });
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
      _setJumpToLatestVisible(false);
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
      showJumpToLatest: _showJumpToLatest,
      onJumpToLatest: _scheduleScrollToLatest,
    );
  }
}

String _agentFeedbackSourceKey(String? threadId, AgentMessage message) =>
    'agent:$threadId:${message.id}';

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
    required this.onBrowseScenes,
    required this.onContinuePractice,
    required this.onOpenReview,
  });

  final bool compact;
  final VoidCallback? onCreatePlan;
  final VoidCallback? onBrowseScenes;
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
        onPressed: onBrowseScenes,
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
    this.onRefreshImage,
    this.speechFeedbackController,
    this.feedbackSourceKey,
    this.onRepractice,
  });

  final List<AgentMessage> messages;
  final AgentVoiceController? voiceController;
  final ValueChanged<AgentMessageAction>? onAction;
  final ConversationMessageImageAction? onRefreshImage;
  final SpeechFeedbackController? speechFeedbackController;
  final String Function(AgentMessage message)? feedbackSourceKey;
  final SpeechFeedbackRepracticeCallback? onRepractice;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('agent-message-list'),
      children: [
        for (final message in messages) ...[
          AgentMessageBubble(
            message: message,
            voiceController: voiceController,
            onAction: onAction,
            onRefreshImage: onRefreshImage,
            polishedText: _polishedText(_feedbackProjection(message)),
            polishLoading: _feedbackProjection(message)?.isPolling ?? false,
          ),
          if (_feedbackProjection(message) case final projection?) ...[
            const SizedBox(height: SpeakUpDesign.space8),
            Align(
              alignment: Alignment.centerRight,
              child: FractionallySizedBox(
                widthFactor: 0.78,
                child: SpeechFeedbackDisclosure(
                  key: ValueKey(
                    'agent-speech-feedback-${projection.sourceKey}',
                  ),
                  projection: projection,
                  onRetry: projection.canRetry
                      ? () => unawaited(
                          speechFeedbackController!.retry(projection.sourceKey),
                        )
                      : null,
                  onRepractice: onRepractice,
                ),
              ),
            ),
          ],
        ],
      ],
    );
  }

  SpeechFeedbackProjection? _feedbackProjection(AgentMessage message) {
    if (message.speechFeedbackStatusUrl == null ||
        speechFeedbackController == null ||
        feedbackSourceKey == null) {
      return null;
    }
    final projection = speechFeedbackController!.projectionFor(
      feedbackSourceKey!(message),
    );
    if (projection?.feedback?.scoreabilityStatus ==
        SpeechFeedbackScoreabilityStatus.insufficient) {
      return null;
    }
    return projection;
  }

  String? _polishedText(SpeechFeedbackProjection? projection) {
    final items = projection?.feedback?.items;
    if (items == null) {
      return null;
    }
    for (final item in items) {
      if (item.kind == SpeechFeedbackItemKind.recommendedExpression &&
          item.suggestedText != null) {
        return item.suggestedText;
      }
    }
    for (final item in items) {
      if (item.suggestedText != null) {
        return item.suggestedText;
      }
    }
    return null;
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
    required this.pendingImages,
    required this.imageErrorMessage,
    required this.imageSelectionInFlight,
    required this.onPickImages,
    required this.onTakePhoto,
    required this.onRemovePendingImage,
    required this.onRetryPendingImage,
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
  final List<AgentPendingImage> pendingImages;
  final String? imageErrorMessage;
  final bool imageSelectionInFlight;
  final ConversationVoiceStarter? onPickImages;
  final ConversationVoiceStarter? onTakePhoto;
  final ConversationPendingImageAction? onRemovePendingImage;
  final ConversationPendingImageAction? onRetryPendingImage;

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
        _textMode = false;
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
    await WidgetsBinding.instance.endOfFrame;
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
        setState(() => _textMode = false);
      }
    } finally {
      if (mounted) {
        setState(() => _textSubmissionInFlight = false);
      }
    }
  }

  Future<void> _submit() async {
    final text = _controller.text.trim();
    final imageUploadPending = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.uploading,
    );
    final imageUploadFailed = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.failed,
    );
    if (!widget.enabled ||
        text.isEmpty ||
        imageUploadPending ||
        imageUploadFailed ||
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

  Future<void> _showImageSource() async {
    if (widget.onPickImages == null && widget.onTakePhoto == null) {
      return;
    }
    final source = await showModalBottomSheet<_AgentImageSource>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (widget.onPickImages != null)
              ListTile(
                key: const Key('agent-image-source-gallery'),
                leading: const Icon(Icons.photo_library_outlined),
                title: const Text('从相册选择'),
                onTap: () =>
                    Navigator.of(context).pop(_AgentImageSource.gallery),
              ),
            if (widget.onTakePhoto != null)
              ListTile(
                key: const Key('agent-image-source-camera'),
                leading: const Icon(Icons.photo_camera_outlined),
                title: const Text('拍照'),
                onTap: () =>
                    Navigator.of(context).pop(_AgentImageSource.camera),
              ),
          ],
        ),
      ),
    );
    if (!mounted) {
      return;
    }
    switch (source) {
      case _AgentImageSource.gallery:
        await widget.onPickImages?.call();
      case _AgentImageSource.camera:
        await widget.onTakePhoto?.call();
      case null:
        return;
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
    final voiceSubmissionInFlight =
        voiceState == AgentVoiceComposerState.confirming ||
        voiceState == AgentVoiceComposerState.awaitingAssistant;
    final voiceFailure = voiceState == AgentVoiceComposerState.failed;
    final capturePhase = switch (voiceState) {
      AgentVoiceComposerState.idle => VoiceCapturePhase.idle,
      AgentVoiceComposerState.starting => VoiceCapturePhase.starting,
      AgentVoiceComposerState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    final voiceCaptureEnabled =
        starting ||
        recording ||
        (widget.onStartVoice != null &&
            widget.voiceEnabled &&
            widget.enabled &&
            !widget.isBusy);
    final showTextComposer =
        confirmingText ||
        (_textMode &&
            !starting &&
            !recording &&
            !voiceProgress &&
            !voiceFailure);
    final imageUploadPending = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.uploading,
    );
    final imageUploadFailed = widget.pendingImages.any(
      (image) => image.state == AgentPendingImageState.failed,
    );

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.pendingImages.isNotEmpty) ...[
          _PendingAgentImages(
            images: widget.pendingImages,
            onRemove: widget.onRemovePendingImage,
            onRetry: widget.onRetryPendingImage,
          ),
          const SizedBox(height: 8),
        ],
        if (widget.imageErrorMessage case final error?) ...[
          Text(
            error,
            key: const Key('agent-image-error'),
            style: const TextStyle(
              color: SpeakUpDesign.error,
              fontSize: 12,
              height: 1.35,
            ),
          ),
          const SizedBox(height: 8),
        ],
        VoiceCaptureControl(
          phase: capturePhase,
          enabled: voiceCaptureEnabled,
          onStart: _startVoice,
          onSendVoice: _sendVoiceMessage,
          onConvertToText: _convertVoiceToText,
          onCancel: _cancelVoice,
          upwardCancelOnly: true,
          builder: (context, capture) => AnimatedContainer(
            key: const Key('agent-composer-surface'),
            duration: const Duration(milliseconds: 180),
            curve: Curves.easeOut,
            constraints: BoxConstraints(
              minHeight: widget.keyboardVisible ? 52 : 54,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
            decoration: BoxDecoration(
              color: SpeakUpDesign.primaryMuted.withValues(alpha: 0.9),
              borderRadius: BorderRadius.circular(999),
              border: Border.all(
                color: SpeakUpDesign.surface.withValues(alpha: 0.72),
              ),
            ),
            child: voiceProgress || voiceFailure
                ? _AgentVoiceStatusDock(
                    state: voiceState,
                    message: voiceFailure
                        ? voice?.errorMessage ?? '语音识别失败'
                        : voiceState == AgentVoiceComposerState.transcribing &&
                              voice?.liveTranscript.trim().isNotEmpty == true
                        ? voice!.liveTranscript
                        : _composerVoiceStateLabel(voiceState),
                    canCancel: !voiceSubmissionInFlight,
                    canRetry: voiceFailure && voice?.canRetry == true,
                    onCancel: _cancelVoice,
                    onRetry: voice?.retry,
                  )
                : showTextComposer
                ? _AgentTextDock(
                    controller: _controller,
                    focusNode: _focusNode,
                    keyboardVisible: widget.keyboardVisible,
                    enabled: confirmingText || widget.enabled,
                    confirmingConvertedText: confirmingText,
                    submitting: _textSubmissionInFlight,
                    canSubmitConvertedText: voice?.canConfirm == true,
                    canSubmitText:
                        widget.onSubmitText != null &&
                        widget.enabled &&
                        !widget.isBusy &&
                        !imageUploadPending &&
                        !imageUploadFailed,
                    onReturnToVoice: confirmingText
                        ? _cancelVoice
                        : _showVoiceComposer,
                    onSubmit: confirmingText ? _submitConvertedText : _submit,
                  )
                : _AgentVoiceDock(
                    capture: capture,
                    phase: capturePhase,
                    elapsed: voice?.recordingElapsed ?? Duration.zero,
                    enabled: voiceCaptureEnabled,
                    textEnabled: widget.enabled,
                    canAddImages:
                        widget.onPickImages != null ||
                        widget.onTakePhoto != null,
                    onAddImages: _showImageSource,
                    onShowText: _showTextComposer,
                  ),
          ),
        ),
      ],
    );
  }
}

class _AgentVoiceDock extends StatelessWidget {
  const _AgentVoiceDock({
    required this.capture,
    required this.phase,
    required this.elapsed,
    required this.enabled,
    required this.textEnabled,
    required this.canAddImages,
    required this.onAddImages,
    required this.onShowText,
  });

  final VoiceCaptureView capture;
  final VoiceCapturePhase phase;
  final Duration elapsed;
  final bool enabled;
  final bool textEnabled;
  final bool canAddImages;
  final FutureOr<void> Function() onAddImages;
  final VoidCallback onShowText;

  @override
  Widget build(BuildContext context) {
    final capturing =
        phase == VoiceCapturePhase.starting ||
        phase == VoiceCapturePhase.recording;
    final label = switch ((phase, capture.releaseIntent, capture.tapMode)) {
      (VoiceCapturePhase.starting, _, _) => '正在打开麦克风…',
      (_, VoiceCaptureReleaseIntent.cancel, _) => '松开取消',
      (VoiceCapturePhase.recording, _, true) => '点击发送 · 上滑取消',
      (VoiceCapturePhase.recording, _, false) => '上滑取消 · 松开发送',
      (_, VoiceCaptureReleaseIntent.convertToText, _) => '松开发送语音',
      _ => enabled ? '点击或长按说话' : '暂时无法录音',
    };
    final mainTarget = capture.wrapTarget(
      key: const Key('agent-mic-placeholder'),
      semanticsLabel: capturing ? '发送语音' : '开始录音',
      child: AnimatedContainer(
        key: capturing ? const Key('agent-voice-stop') : null,
        duration: const Duration(milliseconds: 100),
        constraints: const BoxConstraints(minHeight: 48),
        decoration: BoxDecoration(
          color: capture.cancelArmed
              ? SpeakUpDesign.errorMuted
              : capture.convertArmed
              ? SpeakUpDesign.primaryMuted
              : capturing
              ? SpeakUpDesign.surfaceMuted
              : Colors.transparent,
          borderRadius: BorderRadius.circular(999),
          border: Border.all(
            color: capture.cancelArmed
                ? SpeakUpDesign.error
                : capture.convertArmed
                ? SpeakUpDesign.primary
                : capturing
                ? SpeakUpDesign.border
                : Colors.transparent,
          ),
        ),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            if (capturing) ...[
              const Icon(
                Icons.graphic_eq_rounded,
                color: SpeakUpDesign.ink,
                size: 22,
              ),
              const SizedBox(width: 9),
            ],
            Flexible(
              child: Text(
                label,
                key: const Key('agent-voice-state-label'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  color: capturing
                      ? SpeakUpDesign.ink
                      : SpeakUpDesign.secondary,
                  fontSize: 15,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
            if (phase == VoiceCapturePhase.recording) ...[
              const SizedBox(width: 10),
              Text(
                _formatComposerDuration(elapsed),
                key: const Key('agent-voice-recording-duration'),
                style: const TextStyle(
                  color: SpeakUpDesign.secondary,
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ],
        ),
      ),
    );

    return Row(
      children: [
        if (!capturing)
          IconButton(
            key: const Key('agent-image-picker-button'),
            tooltip: '添加图片',
            onPressed: textEnabled && canAddImages ? () => onAddImages() : null,
            constraints: const BoxConstraints.tightFor(width: 42, height: 42),
            padding: EdgeInsets.zero,
            color: SpeakUpDesign.secondary,
            icon: const Icon(Icons.add_rounded, size: 28),
          ),
        Expanded(child: mainTarget),
        if (!capturing) ...[
          IconButton(
            key: const Key('agent-show-text-composer'),
            tooltip: '切换到键盘输入',
            onPressed: textEnabled ? onShowText : null,
            constraints: const BoxConstraints.tightFor(width: 42, height: 42),
            color: SpeakUpDesign.secondary,
            icon: const Icon(Icons.keyboard_alt_outlined, size: 24),
          ),
        ],
      ],
    );
  }
}

class _AgentTextDock extends StatelessWidget {
  const _AgentTextDock({
    required this.controller,
    required this.focusNode,
    required this.keyboardVisible,
    required this.enabled,
    required this.confirmingConvertedText,
    required this.submitting,
    required this.canSubmitConvertedText,
    required this.canSubmitText,
    required this.onReturnToVoice,
    required this.onSubmit,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool keyboardVisible;
  final bool enabled;
  final bool confirmingConvertedText;
  final bool submitting;
  final bool canSubmitConvertedText;
  final bool canSubmitText;
  final FutureOr<void> Function() onReturnToVoice;
  final FutureOr<void> Function() onSubmit;

  @override
  Widget build(BuildContext context) {
    final canSend =
        controller.text.trim().isNotEmpty &&
        !submitting &&
        (confirmingConvertedText ? canSubmitConvertedText : canSubmitText);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        if (confirmingConvertedText)
          IconButton(
            key: const Key('agent-voice-cancel'),
            tooltip: '取消转文字',
            onPressed: enabled ? () => onReturnToVoice() : null,
            constraints: const BoxConstraints.tightFor(width: 44, height: 44),
            padding: EdgeInsets.zero,
            icon: const Icon(Icons.close_rounded, size: 21),
          )
        else
          IconButton(
            key: const Key('agent-show-voice-composer'),
            tooltip: '切换到语音输入',
            onPressed: enabled ? () => onReturnToVoice() : null,
            constraints: const BoxConstraints.tightFor(width: 44, height: 44),
            padding: EdgeInsets.zero,
            icon: const Icon(Icons.mic_none_rounded, size: 21),
          ),
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
            onSubmitted: (_) {
              if (canSend) {
                onSubmit();
              }
            },
            style: const TextStyle(
              color: SpeakUpDesign.ink,
              fontSize: 15,
              height: 1.4,
            ),
            decoration: InputDecoration(
              hintText: enabled
                  ? confirmingConvertedText
                        ? '编辑识别文字后发送'
                        : '问问 SpeakUp'
                  : '暂时无法开始对话',
              hintStyle: const TextStyle(
                color: SpeakUpDesign.tertiary,
                fontSize: 15,
              ),
              border: InputBorder.none,
              isDense: true,
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 7,
                vertical: 10,
              ),
            ),
          ),
        ),
        IconButton.filled(
          key: Key(
            confirmingConvertedText
                ? 'agent-voice-confirm'
                : 'agent-send-button',
          ),
          tooltip: '发送',
          onPressed: canSend ? () => onSubmit() : null,
          constraints: const BoxConstraints.tightFor(width: 40, height: 40),
          padding: EdgeInsets.zero,
          icon: const Icon(Icons.arrow_upward_rounded, size: 20),
        ),
      ],
    );
  }
}

enum _AgentImageSource { gallery, camera }

class _PendingAgentImages extends StatelessWidget {
  const _PendingAgentImages({
    required this.images,
    required this.onRemove,
    required this.onRetry,
  });

  final List<AgentPendingImage> images;
  final ConversationPendingImageAction? onRemove;
  final ConversationPendingImageAction? onRetry;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 82,
      child: ListView.separated(
        key: const Key('agent-pending-images'),
        scrollDirection: Axis.horizontal,
        itemCount: images.length,
        separatorBuilder: (_, _) => const SizedBox(width: 8),
        itemBuilder: (context, index) {
          final pending = images[index];
          return Stack(
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: Image.memory(
                  pending.image.bytes,
                  key: Key('agent-pending-image-${pending.localId}'),
                  width: 82,
                  height: 82,
                  fit: BoxFit.cover,
                  gaplessPlayback: true,
                ),
              ),
              Positioned.fill(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: pending.state == AgentPendingImageState.ready
                        ? Colors.transparent
                        : const Color(0x66000000),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: switch (pending.state) {
                    AgentPendingImageState.uploading => const Center(
                      child: SizedBox.square(
                        dimension: 22,
                        child: CircularProgressIndicator(
                          strokeWidth: 2.5,
                          color: Colors.white,
                        ),
                      ),
                    ),
                    AgentPendingImageState.failed => Center(
                      child: IconButton.filled(
                        key: Key('agent-retry-image-${pending.localId}'),
                        tooltip: '重试上传',
                        onPressed: onRetry == null
                            ? null
                            : () => onRetry!(pending.localId),
                        icon: const Icon(Icons.refresh_rounded, size: 18),
                      ),
                    ),
                    AgentPendingImageState.ready => const SizedBox.shrink(),
                  },
                ),
              ),
              Positioned(
                right: 2,
                top: 2,
                child: IconButton.filled(
                  key: Key('agent-remove-image-${pending.localId}'),
                  tooltip: '移除图片',
                  onPressed: onRemove == null
                      ? null
                      : () => onRemove!(pending.localId),
                  constraints: const BoxConstraints.tightFor(
                    width: 28,
                    height: 28,
                  ),
                  padding: EdgeInsets.zero,
                  style: IconButton.styleFrom(
                    backgroundColor: const Color(0x99000000),
                    foregroundColor: Colors.white,
                  ),
                  icon: const Icon(Icons.close_rounded, size: 16),
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

class _AgentVoiceStatusDock extends StatelessWidget {
  const _AgentVoiceStatusDock({
    required this.state,
    required this.message,
    required this.canCancel,
    required this.canRetry,
    required this.onCancel,
    required this.onRetry,
  });

  final AgentVoiceComposerState state;
  final String message;
  final bool canCancel;
  final bool canRetry;
  final FutureOr<void> Function() onCancel;
  final FutureOr<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    final failed = state == AgentVoiceComposerState.failed;
    if (!failed) {
      return SizedBox(
        height: 48,
        child: Stack(
          alignment: Alignment.center,
          children: [
            if (canCancel)
              Align(
                alignment: Alignment.centerLeft,
                child: IconButton(
                  key: const Key('agent-voice-cancel'),
                  tooltip: '取消语音输入',
                  onPressed: () => onCancel(),
                  constraints: const BoxConstraints.tightFor(
                    width: 40,
                    height: 40,
                  ),
                  padding: EdgeInsets.zero,
                  icon: const Icon(Icons.close_rounded, size: 21),
                ),
              ),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox.square(
                  dimension: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                const SizedBox(width: 8),
                Flexible(
                  child: Text(
                    message,
                    key: const Key('agent-voice-state-label'),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      color: SpeakUpDesign.secondary,
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      );
    }
    return Row(
      children: [
        if (canCancel) ...[
          IconButton(
            key: const Key('agent-voice-cancel'),
            tooltip: '取消语音输入',
            onPressed: () => onCancel(),
            constraints: const BoxConstraints.tightFor(width: 40, height: 40),
            padding: EdgeInsets.zero,
            icon: const Icon(Icons.close_rounded, size: 21),
          ),
          const SizedBox(width: 4),
        ],
        Expanded(
          child: Text(
            message,
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
        if (canRetry)
          IconButton(
            key: const Key('agent-voice-retry'),
            tooltip: '重试转文字',
            onPressed: onRetry == null ? null : () => onRetry?.call(),
            icon: const Icon(Icons.refresh_rounded),
          ),
      ],
    );
  }
}

String _composerVoiceStateLabel(AgentVoiceComposerState state) {
  return switch (state) {
    AgentVoiceComposerState.starting => '正在打开麦克风…',
    AgentVoiceComposerState.uploading => '正在处理语音…',
    AgentVoiceComposerState.transcribing => '正在转写…',
    AgentVoiceComposerState.confirming => '已识别，SpeakUp 正在回复…',
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
