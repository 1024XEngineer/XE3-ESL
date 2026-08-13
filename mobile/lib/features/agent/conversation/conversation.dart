/// Conversation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/composer/agent_composer.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';
import 'package:speakup/features/agent/composer/pending_image_strip.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_controller.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_models.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/agent_message_bubble.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';

typedef ConversationVoiceStarter = AgentComposerAction;
typedef ConversationPendingImageAction = AgentComposerPendingImageAction;
typedef ConversationMessageImageAction =
    FutureOr<void> Function(String messageId, String imageAssetId);
typedef ConversationMessageTranslator =
    Future<String> Function(AgentMessage message);

/// App-injected presentation port for optional Message feedback.
abstract interface class ConversationMessageFeedbackPresenter
    implements Listenable {
  void syncMessages({
    required String? threadId,
    required List<AgentMessage> messages,
  });

  void clearMessages();

  InlineLanguageSuggestion? correctionFor(AgentMessage message);

  InlineLanguageSuggestion? polishFor(AgentMessage message);

  String? feedbackNoticeFor(AgentMessage message);
}

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
    this.onMessageHandoff,
    ConversationVoiceStarter? onStartVoice,
    ConversationVoiceStarter? onVoicePlaceholder,
    this.onCreateConversation,
    this.draftThreadRecoveryGeneration = 0,
    this.messages = const <AgentMessage>[],
    this.activeSceneName,
    this.hasFocusedThread = true,
    this.hasEarlierMessages = false,
    this.isLoadingEarlierMessages = false,
    this.isBusy = false,
    this.errorMessage,
    this.onSubmitText,
    this.onRetryOperation,
    this.onLoadEarlierMessages,
    this.voiceController,
    this.messageAudioController,
    this.onTranslateMessage,
    this.pendingImages = const <AgentPendingImage>[],
    this.imageErrorMessage,
    this.imageSelectionInFlight = false,
    this.onPickImages,
    this.onTakePhoto,
    this.onRemovePendingImage,
    this.onRetryPendingImage,
    this.onRefreshMessageImage,
    this.feedbackPresenter,
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
  final ValueChanged<AgentHandoff>? onMessageHandoff;
  final ConversationVoiceStarter? onStartVoice;
  final VoidCallback? onCreateConversation;
  final int draftThreadRecoveryGeneration;
  final List<AgentMessage> messages;
  final String? activeSceneName;
  final bool hasFocusedThread;
  final bool hasEarlierMessages;
  final bool isLoadingEarlierMessages;
  final bool isBusy;
  final String? errorMessage;
  final Future<bool> Function(String)? onSubmitText;
  final VoidCallback? onRetryOperation;
  final VoidCallback? onLoadEarlierMessages;
  final AgentVoiceController? voiceController;
  final AgentMessageAudioController? messageAudioController;
  final ConversationMessageTranslator? onTranslateMessage;
  final List<AgentPendingImage> pendingImages;
  final String? imageErrorMessage;
  final bool imageSelectionInFlight;
  final ConversationVoiceStarter? onPickImages;
  final ConversationVoiceStarter? onTakePhoto;
  final ConversationPendingImageAction? onRemovePendingImage;
  final ConversationPendingImageAction? onRetryPendingImage;
  final ConversationMessageImageAction? onRefreshMessageImage;
  final ConversationMessageFeedbackPresenter? feedbackPresenter;

  @override
  State<ConversationPage> createState() => _ConversationPageState();

  Widget _build(
    BuildContext context, {
    required ScrollController scrollController,
    required VoidCallback? onLoadEarlierMessages,
    required bool showJumpToLatest,
    required VoidCallback onJumpToLatest,
    required double composerHeight,
    required GlobalKey composerKey,
    required VoidCallback onComposerSizeChanged,
  }) {
    final width = MediaQuery.sizeOf(context).width;
    final horizontalPadding = width >= 390 ? 20.0 : 16.0;
    final keyboardVisible = MediaQuery.viewInsetsOf(context).bottom > 0;
    final titleSize = width < 350 ? 27.0 : 30.0;
    final emptyHomeActionGap =
        (MediaQuery.sizeOf(context).height * 0.325).clamp(180.0, 274.0) +
        (onContinuePractice == null ? 52.0 : 0.0);
    final composerBottom = keyboardVisible ? 10.0 : restingComposerBottom;
    final acceptedUserMessage = _lastUserMessage(messages);
    final canCompose = hasFocusedThread || onCreateConversation != null;
    final voiceShowsReplyProgress = switch (voiceController?.state) {
      AgentVoiceComposerState.confirming ||
      AgentVoiceComposerState.awaitingAssistant => true,
      _ => false,
    };
    final replyPending = isBusy || voiceShowsReplyProgress;
    const topContentInset = 65.0;
    const topOverlayExtent = 76.0;
    final bottomOverlayExtent = composerBottom + composerHeight + 16;

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
              child: Stack(
                children: [
                  Positioned.fill(
                    child: SingleChildScrollView(
                      key: const Key('agent-conversation-scroll'),
                      controller: scrollController,
                      padding: EdgeInsets.fromLTRB(
                        horizontalPadding,
                        topContentInset,
                        horizontalPadding,
                        bottomOverlayExtent,
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
                            SizedBox(height: emptyHomeActionGap),
                            if (practiceAvailable)
                              _QuickActions(
                                onCreatePlan: onCreatePlan,
                                onBrowseScenes: onBrowseScenes,
                                onContinuePractice: onContinuePractice,
                                onOpenReview: onOpenReview,
                              )
                            else
                              const _PracticeUnavailableNotice(),
                          ] else ...[
                            if (activeSceneName case final sceneName?) ...[
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
                                      sceneName,
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
                                  key: const Key('load-earlier-agent-messages'),
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
                              suppressLoadingFeedback: replyPending,
                              messageAudioController: messageAudioController,
                              onTranslateMessage: onTranslateMessage,
                              onHandoff: onMessageHandoff,
                              onRefreshImage: onRefreshMessageImage,
                              feedbackPresenter: feedbackPresenter,
                              onSameThreadRepractice:
                                  !isBusy && onStartVoice != null
                                  ? () => unawaited(
                                      Future<void>.sync(onStartVoice!),
                                    )
                                  : null,
                            ),
                          ],
                          if (isBusy && !voiceShowsReplyProgress) ...[
                            const SizedBox(height: 14),
                            Center(
                              child: Semantics(
                                label: 'SpeakUp 正在处理',
                                child: const Wrap(
                                  key: Key('agent-operation-progress'),
                                  alignment: WrapAlignment.center,
                                  crossAxisAlignment: WrapCrossAlignment.center,
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
                  Positioned(
                    left: 0,
                    top: 0,
                    right: 0,
                    height: topOverlayExtent,
                    child: IgnorePointer(
                      child: DecoratedBox(
                        key: const Key('agent-top-overlay-scrim'),
                        decoration: BoxDecoration(
                          gradient: LinearGradient(
                            begin: Alignment.topCenter,
                            end: Alignment.bottomCenter,
                            colors: [
                              SpeakUpDesign.canvas,
                              SpeakUpDesign.canvas.withValues(alpha: 0.94),
                              SpeakUpDesign.canvas.withValues(alpha: 0),
                            ],
                            stops: const [0, 0.7, 1],
                          ),
                        ),
                      ),
                    ),
                  ),
                  if (messages.isNotEmpty)
                    Positioned(
                      left: 0,
                      right: 0,
                      bottom: 0,
                      height: bottomOverlayExtent + 52,
                      child: IgnorePointer(
                        child: DecoratedBox(
                          key: const Key('agent-bottom-overlay-scrim'),
                          decoration: BoxDecoration(
                            gradient: LinearGradient(
                              begin: Alignment.topCenter,
                              end: Alignment.bottomCenter,
                              colors: [
                                SpeakUpDesign.canvas.withValues(alpha: 0),
                                SpeakUpDesign.canvas.withValues(alpha: 0.94),
                                SpeakUpDesign.canvas,
                              ],
                              stops: const [0, 0.38, 1],
                            ),
                          ),
                        ),
                      ),
                    ),
                  Positioned(
                    left: horizontalPadding,
                    top: 12,
                    right: horizontalPadding,
                    child: _AgentTopBar(
                      previewMode: previewMode,
                      onOpenMenu: onOpenMenu,
                      onNavigateBack: onNavigateBack,
                      onCreateConversation: onCreateConversation,
                      isBusy: isBusy,
                    ),
                  ),
                  Positioned(
                    left: horizontalPadding,
                    right: horizontalPadding,
                    bottom: composerBottom,
                    child: NotificationListener<SizeChangedLayoutNotification>(
                      onNotification: (_) {
                        onComposerSizeChanged();
                        return false;
                      },
                      child: SizeChangedLayoutNotifier(
                        key: const Key('agent-composer-overlay'),
                        child: KeyedSubtree(
                          key: composerKey,
                          child: AgentComposer(
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
                      ),
                    ),
                  ),
                  if (showJumpToLatest)
                    Positioned(
                      right: horizontalPadding,
                      bottom: bottomOverlayExtent,
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
            ),
          ),
        ],
      ),
    );
  }
}

class _ConversationPageState extends State<ConversationPage> {
  final ScrollController _scrollController = ScrollController();
  final GlobalKey _composerKey = GlobalKey();
  _ConversationScrollAnchor? _earlierMessagesAnchor;
  int _scrollRequestGeneration = 0;
  bool _showJumpToLatest = false;
  bool _voiceRebuildScheduled = false;
  bool _composerMeasurementScheduled = false;
  double _composerHeight = 54;

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_handleScroll);
    _scheduleThreadInitialPosition();
    _syncFeedbackPresenter();
    widget.feedbackPresenter?.addListener(_handleFeedbackState);
    widget.voiceController?.addListener(_handleConversationVoiceState);
  }

  @override
  void didUpdateWidget(covariant ConversationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.feedbackPresenter != widget.feedbackPresenter) {
      oldWidget.feedbackPresenter?.removeListener(_handleFeedbackState);
      oldWidget.feedbackPresenter?.clearMessages();
      widget.feedbackPresenter?.addListener(_handleFeedbackState);
    }
    if (oldWidget.voiceController != widget.voiceController) {
      oldWidget.voiceController?.removeListener(_handleConversationVoiceState);
      widget.voiceController?.addListener(_handleConversationVoiceState);
    }
    _syncFeedbackPresenter();
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
    final handoffFootprintChanged =
        previousLast?.handoffs.length != currentLast?.handoffs.length;
    if (previousLast?.id == currentLast?.id &&
        (previousLast?.text != currentLast?.text || handoffFootprintChanged)) {
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
    widget.feedbackPresenter?.removeListener(_handleFeedbackState);
    widget.voiceController?.removeListener(_handleConversationVoiceState);
    widget.feedbackPresenter?.clearMessages();
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

  void _scheduleComposerMeasurement() {
    if (_composerMeasurementScheduled) {
      return;
    }
    _composerMeasurementScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _composerMeasurementScheduled = false;
      if (!mounted) {
        return;
      }
      final height = _composerKey.currentContext?.size?.height;
      if (height == null || (height - _composerHeight).abs() < 0.5) {
        return;
      }
      final keepLatestVisible = _isNearLatest();
      setState(() => _composerHeight = height);
      if (keepLatestVisible) {
        _scheduleScrollToLatest();
      }
    });
  }

  void _setJumpToLatestVisible(bool value) {
    if (_showJumpToLatest == value) {
      return;
    }
    setState(() => _showJumpToLatest = value);
  }

  void _syncFeedbackPresenter() => widget.feedbackPresenter?.syncMessages(
    threadId: widget.threadId,
    messages: widget.messages,
  );

  void _handleFeedbackState() {
    scheduleMicrotask(() {
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
    _scheduleComposerMeasurement();
    return widget._build(
      context,
      scrollController: _scrollController,
      onLoadEarlierMessages: widget.onLoadEarlierMessages == null
          ? null
          : _handleLoadEarlierMessages,
      showJumpToLatest: _showJumpToLatest,
      onJumpToLatest: _scheduleScrollToLatest,
      composerHeight: _composerHeight,
      composerKey: _composerKey,
      onComposerSizeChanged: _scheduleComposerMeasurement,
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
                    const SpeakUpWordmark(
                      key: Key('conversation-fixed-title'),
                      height: 26,
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
    required this.onCreatePlan,
    required this.onBrowseScenes,
    required this.onContinuePractice,
    required this.onOpenReview,
  });

  final VoidCallback? onCreatePlan;
  final VoidCallback? onBrowseScenes;
  final VoidCallback? onContinuePractice;
  final VoidCallback? onOpenReview;

  @override
  Widget build(BuildContext context) {
    final actions = <_QuickActionButton>[
      if (onContinuePractice != null)
        _QuickActionButton(
          actionKey: const Key('quick-action-continue-practice'),
          icon: Icons.play_circle_outline_rounded,
          label: '继续上次练习',
          onPressed: onContinuePractice,
        ),
      _QuickActionButton(
        actionKey: const Key('quick-action-create-plan'),
        icon: Icons.work_outline_rounded,
        label: '准备模拟面试',
        onPressed: onCreatePlan,
      ),
      _QuickActionButton(
        actionKey: const Key('quick-action-browse-scenes'),
        icon: Icons.record_voice_over_outlined,
        label: '选择口语训练',
        onPressed: onBrowseScenes,
      ),
      _QuickActionButton(
        actionKey: const Key('quick-action-recent-review'),
        icon: Icons.history_rounded,
        label: '回顾最近练习',
        onPressed: onOpenReview,
      ),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var index = 0; index < actions.length; index++) ...[
          actions[index],
          if (index != actions.length - 1) const SizedBox.shrink(),
        ],
      ],
    );
  }
}

class _QuickActionButton extends StatelessWidget {
  const _QuickActionButton({
    this.actionKey,
    required this.icon,
    required this.label,
    required this.onPressed,
  });

  final Key? actionKey;
  final IconData icon;
  final String label;
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
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onPressed,
          child: Container(
            constraints: const BoxConstraints(minHeight: 52),
            padding: const EdgeInsets.symmetric(
              horizontal: SpeakUpDesign.space4,
              vertical: SpeakUpDesign.space8,
            ),
            child: Row(
              children: [
                Icon(icon, size: 24, color: SpeakUpDesign.secondary),
                const SizedBox(width: SpeakUpDesign.space16),
                Expanded(
                  child: Text(
                    label,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: SpeakUpDesign.secondary,
                      fontSize: 17,
                      fontWeight: FontWeight.w500,
                      height: 1.25,
                    ),
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
    required this.suppressLoadingFeedback,
    this.messageAudioController,
    this.onTranslateMessage,
    this.onHandoff,
    this.onRefreshImage,
    this.feedbackPresenter,
    this.onSameThreadRepractice,
  });

  final List<AgentMessage> messages;
  final bool suppressLoadingFeedback;
  final AgentMessageAudioController? messageAudioController;
  final ConversationMessageTranslator? onTranslateMessage;
  final ValueChanged<AgentHandoff>? onHandoff;
  final ConversationMessageImageAction? onRefreshImage;
  final ConversationMessageFeedbackPresenter? feedbackPresenter;
  final VoidCallback? onSameThreadRepractice;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('agent-message-list'),
      children: [
        for (final message in messages)
          if (!suppressLoadingFeedback ||
              message.role != AgentMessageRole.assistant ||
              !message.isStreaming ||
              message.text.isNotEmpty)
            AgentMessageBubble(
              message: message,
              messageAudioController: messageAudioController,
              onTranslate: onTranslateMessage,
              onHandoff: onHandoff,
              onRefreshImage: onRefreshImage,
              correction: feedbackPresenter?.correctionFor(message),
              polish: feedbackPresenter?.polishFor(message),
              feedbackNotice: feedbackPresenter?.feedbackNoticeFor(message),
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

AgentMessage? _lastUserMessage(List<AgentMessage> messages) {
  for (final message in messages.reversed) {
    if (message.role == AgentMessageRole.user) {
      return message;
    }
  }
  return null;
}
