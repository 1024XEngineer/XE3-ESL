import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/practice/question_tip_sheet.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_message_bubble.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

/// A vendor-neutral surface used by the immersive roleplay shell.
///
/// The injected builder may render any avatar implementation. Keeping the
/// dependency in the composition root lets the product UI survive a vendor
/// change without importing a provider SDK.
typedef ImmersiveAvatarSurfaceBuilder = Widget Function(BuildContext context);
typedef ImmersiveAsyncAction = Future<void> Function();

class ImmersiveRoleplayPage extends StatefulWidget {
  const ImmersiveRoleplayPage({
    required this.practiceController,
    this.avatarSurfaceBuilder,
    this.avatarStatusLabel = '正在准备画面',
    this.onBeforeStartRecording,
    this.onBeforeSubmitText,
    this.onReplayQuestion,
    this.onPracticeCompleted,
    this.speechFeedbackController,
    this.replayLoading = false,
    this.replayPlaying = false,
    this.onExitRequested,
    this.previewMode = false,
    super.key,
  });

  final PracticeController practiceController;
  final ImmersiveAvatarSurfaceBuilder? avatarSurfaceBuilder;
  final String? avatarStatusLabel;
  final ImmersiveAsyncAction? onBeforeStartRecording;
  final ImmersiveAsyncAction? onBeforeSubmitText;
  final ImmersiveAsyncAction? onReplayQuestion;
  final Future<bool> Function()? onPracticeCompleted;
  final SpeechFeedbackController? speechFeedbackController;
  final bool replayLoading;
  final bool replayPlaying;
  final Future<bool> Function()? onExitRequested;
  final bool previewMode;

  @override
  State<ImmersiveRoleplayPage> createState() => _ImmersiveRoleplayPageState();
}

class _ImmersiveRoleplayPageState extends State<ImmersiveRoleplayPage> {
  final _conversationScrollController = ScrollController();
  final _textController = TextEditingController();
  final _textFocusNode = FocusNode();
  Timer? _recordingTicker;
  DateTime? _recordingStartedAt;
  int _recordingSeconds = 0;
  int _observedMessageCount = 0;
  final Map<String, String> _feedbackSources = {};
  bool _textMode = false;
  bool _exitInFlight = false;
  bool _exitApproved = false;
  final Map<String, String> _questionTranslations = <String, String>{};
  bool _feedbackRebuildScheduled = false;
  bool _completionInFlight = false;
  PracticePromptSpeaker? _ownedTipSpeaker;
  String? _visibleTipQuestionId;

  @override
  void initState() {
    super.initState();
    _observedMessageCount = widget.practiceController.practiceMessages.length;
    widget.practiceController.addListener(_handleControllerState);
    widget.speechFeedbackController?.addListener(_handleFeedbackState);
    _syncSpeechFeedbackSources();
    _syncRecordingTimer();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _conversationScrollController.hasClients) {
        _conversationScrollController.jumpTo(
          _conversationScrollController.position.maxScrollExtent,
        );
      }
    });
  }

  @override
  void didUpdateWidget(covariant ImmersiveRoleplayPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    final controllerChanged =
        oldWidget.practiceController != widget.practiceController;
    final feedbackControllerChanged =
        oldWidget.speechFeedbackController != widget.speechFeedbackController;
    if (controllerChanged || feedbackControllerChanged) {
      _removeSpeechFeedbackSources(oldWidget.speechFeedbackController);
    }
    if (feedbackControllerChanged) {
      oldWidget.speechFeedbackController?.removeListener(_handleFeedbackState);
      widget.speechFeedbackController?.addListener(_handleFeedbackState);
    }
    if (controllerChanged) {
      oldWidget.practiceController.removeListener(_handleControllerState);
      _observedMessageCount = widget.practiceController.practiceMessages.length;
      _visibleTipQuestionId = null;
      widget.practiceController.addListener(_handleControllerState);
      _syncRecordingTimer();
    }
    _syncSpeechFeedbackSources();
  }

  @override
  void dispose() {
    widget.practiceController.removeListener(_handleControllerState);
    widget.speechFeedbackController?.removeListener(_handleFeedbackState);
    _removeSpeechFeedbackSources(widget.speechFeedbackController);
    _recordingTicker?.cancel();
    _conversationScrollController.dispose();
    _textController.dispose();
    _textFocusNode.dispose();
    unawaited(widget.practiceController.stopPracticeAudio(notify: false));
    if (_ownedTipSpeaker case final speaker?) {
      unawaited(speaker.dispose());
    }
    super.dispose();
  }

  Future<void> _showQuestionTip() async {
    final controller = widget.practiceController;
    final tip = await controller.requestQuestionTip();
    if (!mounted ||
        tip == null ||
        controller.currentQuestion?.id != tip.questionId) {
      return;
    }
    setState(() => _visibleTipQuestionId = tip.questionId);
  }

  Future<void> _speakQuestionTip() async {
    final tip = widget.practiceController.questionTip;
    if (tip == null || _visibleTipQuestionId != tip.questionId) {
      return;
    }
    final speaker = _ownedTipSpeaker ??= SystemPracticePromptSpeaker();
    await speaker.speak(tip.content);
  }

  void _hideQuestionTip() {
    if (_visibleTipQuestionId == null) {
      return;
    }
    setState(() => _visibleTipQuestionId = null);
    unawaited(_stopQuestionTipSpeech());
  }

  Future<void> _stopQuestionTipSpeech() async {
    try {
      await _ownedTipSpeaker?.stop();
    } on Object {
      // Recording and dismissal must not be blocked by a platform TTS error.
    }
  }

  Future<void> _beforeStartRecording() async {
    await _stopQuestionTipSpeech();
    await _runBoundedUserTurnAction(widget.onBeforeStartRecording);
  }

  void _handleControllerState() {
    if (!mounted) {
      return;
    }
    final messageCount = widget.practiceController.practiceMessages.length;
    final shouldFollowConversation = messageCount != _observedMessageCount;
    _observedMessageCount = messageCount;
    if (_visibleTipQuestionId !=
        widget.practiceController.currentQuestion?.id) {
      _visibleTipQuestionId = null;
    }
    _syncRecordingTimer();
    _syncSpeechFeedbackSources();
    setState(() {});
    if (shouldFollowConversation) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_conversationScrollController.hasClients) {
          return;
        }
        unawaited(
          _conversationScrollController.animateTo(
            _conversationScrollController.position.maxScrollExtent,
            duration: const Duration(milliseconds: 220),
            curve: Curves.easeOutCubic,
          ),
        );
      });
    }
  }

  void _syncSpeechFeedbackSources() {
    final feedbackController = widget.speechFeedbackController;
    if (feedbackController == null) {
      _feedbackSources.clear();
      return;
    }
    final current = <String, String>{};
    for (final message in widget.practiceController.practiceMessages) {
      final statusUrl = message.speechFeedbackStatusUrl;
      if (statusUrl != null) {
        current[_immersiveFeedbackSourceKey(
              widget.practiceController,
              message,
            )] =
            statusUrl;
      }
    }
    for (final entry in _feedbackSources.entries.toList()) {
      if (current[entry.key] != entry.value) {
        feedbackController.removeSource(entry.key);
        _feedbackSources.remove(entry.key);
      }
    }
    for (final entry in current.entries) {
      if (_feedbackSources[entry.key] == entry.value) {
        continue;
      }
      _feedbackSources[entry.key] = entry.value;
      unawaited(
        feedbackController.load(sourceKey: entry.key, statusUrl: entry.value),
      );
    }
  }

  void _removeSpeechFeedbackSources(
    SpeechFeedbackController? feedbackController,
  ) {
    if (feedbackController != null) {
      for (final sourceKey in _feedbackSources.keys) {
        feedbackController.removeSource(sourceKey);
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

  void _syncRecordingTimer() {
    final isRecording =
        widget.practiceController.recordingState ==
        PracticeRecordingState.recording;
    if (isRecording) {
      if (_recordingTicker != null) {
        return;
      }
      _recordingStartedAt = DateTime.now();
      _recordingSeconds = 0;
      _recordingTicker = Timer.periodic(const Duration(seconds: 1), (_) {
        final startedAt = _recordingStartedAt;
        if (!mounted || startedAt == null) {
          return;
        }
        setState(() {
          _recordingSeconds = DateTime.now().difference(startedAt).inSeconds;
        });
      });
      return;
    }
    _recordingTicker?.cancel();
    _recordingTicker = null;
    _recordingStartedAt = null;
    _recordingSeconds = 0;
    if (widget.practiceController.recordingState !=
        PracticeRecordingState.idle) {
      _textMode = false;
    }
  }

  Future<void> _completePractice() async {
    if (!mounted || _completionInFlight) {
      return;
    }
    final callback = widget.onPracticeCompleted;
    if (callback == null) {
      await Navigator.of(context).maybePop();
      return;
    }
    setState(() => _completionInFlight = true);
    var completed = false;
    try {
      completed = await callback();
    } on Object {
      completed = false;
    }
    if (!mounted) {
      return;
    }
    setState(() => _completionInFlight = false);
    if (completed) {
      Navigator.of(context).pop(CompletedPracticeRouteResult.continueWithAgent);
      return;
    }
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(const SnackBar(content: Text('练习正在完成，请稍后重试。')));
  }

  Future<void> _submitText() async {
    await _runBoundedUserTurnAction(widget.onBeforeSubmitText);
    if (!mounted) {
      return;
    }
    final submitted = await widget.practiceController.submitPracticeText(
      _textController.text,
    );
    if (!mounted || !submitted) {
      return;
    }
    _textController.clear();
    _textFocusNode.unfocus();
    setState(() => _textMode = false);
  }

  void _toggleTextMode() {
    setState(() => _textMode = !_textMode);
    if (!_textMode) {
      _textFocusNode.unfocus();
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _textFocusNode.requestFocus();
      }
    });
  }

  Future<String> _translateQuestion(PracticeMessage message) async {
    final cached = _questionTranslations[message.id];
    if (cached != null) {
      return cached;
    }
    final client = widget.practiceController.client;
    if (client is! PracticeQuestionTranslationClient) {
      throw StateError('Question translation is unavailable.');
    }
    final translation = await (client as PracticeQuestionTranslationClient)
        .translateQuestion(questionId: message.id);
    if (translation.questionId != message.id) {
      throw StateError('Question translation does not match the message.');
    }
    _questionTranslations[message.id] = translation.content;
    return translation.content;
  }

  Future<void> _requestExit() async {
    if (!mounted || _exitInFlight || _exitApproved) {
      return;
    }
    final route = ModalRoute.of(context);
    final navigator = Navigator.of(context);
    final callback = widget.onExitRequested;
    if (callback == null) {
      _exitApproved = true;
    } else {
      setState(() => _exitInFlight = true);
      var approved = false;
      try {
        approved = await callback();
      } on Object {
        approved = false;
      }
      if (!mounted) {
        return;
      }
      _exitInFlight = false;
      if (!approved) {
        ScaffoldMessenger.of(context)
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('当前练习正在保存，请稍后再返回。')));
        setState(() {});
        return;
      }
      _exitApproved = true;
    }
    if (mounted && route?.isCurrent == true) {
      setState(() {});
      await WidgetsBinding.instance.endOfFrame;
    }
    if (mounted && route?.isCurrent == true) {
      await navigator.maybePop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final scene = widget.practiceController.scene;
    return PopScope<void>(
      canPop: widget.onExitRequested == null || _exitApproved,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          unawaited(_requestExit());
        }
      },
      child: Scaffold(
        key: const Key('immersive-roleplay-page'),
        backgroundColor: SpeakUpDesign.canvas,
        body: SafeArea(
          child: scene == null
              ? const Center(child: Text('请先选择一个情景开始对话。'))
              : LayoutBuilder(
                  builder: (context, constraints) {
                    final landscape =
                        constraints.maxWidth > constraints.maxHeight;
                    final avatar = _AvatarStage(
                      scene: scene,
                      surfaceBuilder:
                          _exitApproved ||
                              !widget.practiceController.canUseAvatar
                          ? null
                          : widget.avatarSurfaceBuilder,
                      statusLabel: widget.avatarStatusLabel,
                      latestAssistantMessage:
                          widget.practiceController.practiceExperience ==
                              PracticeExperience.interview
                          ? null
                          : _latestAssistantMessage(
                              widget.practiceController.practiceMessages,
                            ),
                      exitInFlight: _exitInFlight,
                      onExit: _requestExit,
                    );
                    final conversation = _ConversationPanel(
                      controller: widget.practiceController,
                      scrollController: _conversationScrollController,
                      textController: _textController,
                      textFocusNode: _textFocusNode,
                      textMode: _textMode,
                      recordingSeconds: _recordingSeconds,
                      previewMode: widget.previewMode,
                      onBeforeStartRecording: _beforeStartRecording,
                      onReplayQuestion: widget.onReplayQuestion,
                      speechFeedbackController: widget.speechFeedbackController,
                      replayLoading: widget.replayLoading,
                      replayPlaying: widget.replayPlaying,
                      onToggleTextMode: _toggleTextMode,
                      onSubmitText: _submitText,
                      onTranslateQuestion:
                          widget.practiceController.canTranslateQuestion &&
                              widget.practiceController.client
                                  is PracticeQuestionTranslationClient
                          ? _translateQuestion
                          : null,
                      onShowTip: _showQuestionTip,
                      onHideTip: _hideQuestionTip,
                      onSpeakTip: _speakQuestionTip,
                      visibleTipQuestionId: _visibleTipQuestionId,
                      onPracticeCompleted: _completePractice,
                    );
                    if (landscape) {
                      return Row(
                        children: [
                          SizedBox(
                            key: const Key('immersive-avatar-region'),
                            width: constraints.maxWidth * 0.44,
                            child: avatar,
                          ),
                          Expanded(child: conversation),
                        ],
                      );
                    }
                    return Column(
                      children: [
                        SizedBox(
                          key: const Key('immersive-avatar-region'),
                          height: constraints.maxHeight * 0.44,
                          child: avatar,
                        ),
                        Expanded(child: conversation),
                      ],
                    );
                  },
                ),
        ),
      ),
    );
  }
}

class _AvatarStage extends StatelessWidget {
  const _AvatarStage({
    required this.scene,
    required this.surfaceBuilder,
    required this.statusLabel,
    required this.latestAssistantMessage,
    required this.exitInFlight,
    required this.onExit,
  });

  final SceneDefinition scene;
  final ImmersiveAvatarSurfaceBuilder? surfaceBuilder;
  final String? statusLabel;
  final PracticeMessage? latestAssistantMessage;
  final bool exitInFlight;
  final VoidCallback onExit;

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: const Color(0xFFE5E9E5),
      child: Stack(
        fit: StackFit.expand,
        children: [
          if (surfaceBuilder case final builder?)
            builder(context)
          else
            const _AvatarPlaceholder(),
          const Positioned.fill(
            child: IgnorePointer(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: [
                      Color(0x33000000),
                      Colors.transparent,
                      Color(0x4D000000),
                    ],
                    stops: [0, 0.45, 1],
                  ),
                ),
              ),
            ),
          ),
          Positioned(
            left: 12,
            right: 12,
            top: 10,
            child: Row(
              children: [
                IconButton.filledTonal(
                  key: const Key('immersive-exit'),
                  tooltip: '返回',
                  onPressed: exitInFlight ? null : onExit,
                  icon: exitInFlight
                      ? const SizedBox.square(
                          dimension: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.arrow_back_rounded),
                  style: IconButton.styleFrom(
                    backgroundColor: Colors.white.withValues(alpha: 0.9),
                    foregroundColor: SpeakUpDesign.ink,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    scene.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                      shadows: [
                        Shadow(color: Color(0x66000000), blurRadius: 8),
                      ],
                    ),
                  ),
                ),
                if (statusLabel case final label?)
                  Flexible(
                    child: Semantics(
                      liveRegion: true,
                      child: Container(
                        key: const Key('immersive-avatar-status'),
                        padding: const EdgeInsets.symmetric(
                          horizontal: 10,
                          vertical: 7,
                        ),
                        decoration: BoxDecoration(
                          color: Colors.black.withValues(alpha: 0.46),
                          borderRadius: BorderRadius.circular(20),
                        ),
                        child: Text(
                          label,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
          if (latestAssistantMessage case final message?)
            Positioned(
              left: 16,
              right: 16,
              bottom: 14,
              child: Container(
                key: const Key('immersive-live-subtitle'),
                padding: const EdgeInsets.symmetric(
                  horizontal: 15,
                  vertical: 11,
                ),
                decoration: BoxDecoration(
                  color: Colors.black.withValues(alpha: 0.58),
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Text(
                  message.text,
                  key: const Key('immersive-live-subtitle-text'),
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  textAlign: TextAlign.center,
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 15,
                    height: 1.35,
                    fontWeight: FontWeight.w500,
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _AvatarPlaceholder extends StatelessWidget {
  const _AvatarPlaceholder();

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '情景对话画面',
      child: const Center(
        child: Icon(
          Icons.person_outline_rounded,
          key: Key('immersive-avatar-placeholder'),
          size: 84,
          color: Color(0xFF819087),
        ),
      ),
    );
  }
}

class _ConversationPanel extends StatelessWidget {
  const _ConversationPanel({
    required this.controller,
    required this.scrollController,
    required this.textController,
    required this.textFocusNode,
    required this.textMode,
    required this.recordingSeconds,
    required this.previewMode,
    required this.onBeforeStartRecording,
    required this.onReplayQuestion,
    required this.speechFeedbackController,
    required this.replayLoading,
    required this.replayPlaying,
    required this.onToggleTextMode,
    required this.onSubmitText,
    required this.onTranslateQuestion,
    required this.onShowTip,
    required this.onHideTip,
    required this.onSpeakTip,
    required this.visibleTipQuestionId,
    required this.onPracticeCompleted,
  });

  final PracticeController controller;
  final ScrollController scrollController;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final bool textMode;
  final int recordingSeconds;
  final bool previewMode;
  final ImmersiveAsyncAction? onBeforeStartRecording;
  final ImmersiveAsyncAction? onReplayQuestion;
  final SpeechFeedbackController? speechFeedbackController;
  final bool replayLoading;
  final bool replayPlaying;
  final VoidCallback onToggleTextMode;
  final VoidCallback onSubmitText;
  final Future<String> Function(PracticeMessage message)? onTranslateQuestion;
  final VoidCallback onShowTip;
  final VoidCallback onHideTip;
  final Future<void> Function() onSpeakTip;
  final String? visibleTipQuestionId;
  final VoidCallback onPracticeCompleted;

  @override
  Widget build(BuildContext context) {
    final messages = controller.practiceMessages;
    return ColoredBox(
      color: SpeakUpDesign.surface,
      child: Column(
        children: [
          _ConversationHeader(
            controller: controller,
            onShowTip: onShowTip,
            onReplayQuestion: onReplayQuestion,
            replayLoading: replayLoading,
            replayPlaying: replayPlaying,
          ),
          Expanded(
            child: messages.isEmpty
                ? const _ConversationEmpty()
                : ListView.separated(
                    key: const Key('immersive-conversation-history'),
                    controller: scrollController,
                    padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                    itemCount:
                        messages.length +
                        (controller.errorMessage == null ? 0 : 1) +
                        (controller.mediaErrorMessage == null ? 0 : 1) +
                        (previewMode ? 1 : 0),
                    separatorBuilder: (_, _) => const SizedBox(height: 8),
                    itemBuilder: (context, index) {
                      if (index < messages.length) {
                        final message = messages[index];
                        final projection = _feedbackProjection(message);
                        return Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            PracticeMessageBubble(
                              message: message,
                              feedbackProjection: projection,
                              messageTextVisible: true,
                              onTranslate:
                                  message.role == PracticeMessageRole.assistant
                                  ? onTranslateQuestion
                                  : null,
                            ),
                          ],
                        );
                      }
                      var extraIndex = index - messages.length;
                      if (controller.errorMessage case final error?) {
                        if (extraIndex == 0) {
                          return _InlineError(message: error);
                        }
                        extraIndex--;
                      }
                      if (controller.mediaErrorMessage case final error?) {
                        if (extraIndex == 0) {
                          return _InlineError(message: error);
                        }
                        extraIndex--;
                      }
                      return const Text(
                        '当前为预览模式，语音服务可能不可用。',
                        textAlign: TextAlign.center,
                        style: SpeakUpDesign.meta,
                      );
                    },
                  ),
          ),
          if (controller.questionTip case final tip?
              when tip.questionId == visibleTipQuestionId)
            QuestionTipCard(
              content: tip.content,
              onClose: onHideTip,
              onSpeak: onSpeakTip,
            ),
          if (controller.questionTipErrorMessage case final message?)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: Text(
                message,
                key: const Key('immersive-question-tip-error'),
                textAlign: TextAlign.right,
                style: const TextStyle(
                  color: SpeakUpDesign.error,
                  fontSize: 12,
                ),
              ),
            ),
          _ImmersiveComposer(
            controller: controller,
            textController: textController,
            textFocusNode: textFocusNode,
            textMode: textMode,
            recordingSeconds: recordingSeconds,
            onBeforeStartRecording: onBeforeStartRecording,
            onToggleTextMode: onToggleTextMode,
            onSubmitText: onSubmitText,
            onPracticeCompleted: onPracticeCompleted,
          ),
        ],
      ),
    );
  }

  SpeechFeedbackProjection? _feedbackProjection(PracticeMessage message) {
    if (message.speechFeedbackStatusUrl == null ||
        speechFeedbackController == null) {
      return null;
    }
    final projection = speechFeedbackController!.projectionFor(
      _immersiveFeedbackSourceKey(controller, message),
    );
    if (projection?.feedback?.scoreabilityStatus ==
        SpeechFeedbackScoreabilityStatus.insufficient) {
      return null;
    }
    return projection;
  }
}

class _ConversationHeader extends StatelessWidget {
  const _ConversationHeader({
    required this.controller,
    required this.onShowTip,
    required this.onReplayQuestion,
    required this.replayLoading,
    required this.replayPlaying,
  });

  final PracticeController controller;
  final VoidCallback onShowTip;
  final ImmersiveAsyncAction? onReplayQuestion;
  final bool replayLoading;
  final bool replayPlaying;

  @override
  Widget build(BuildContext context) {
    final current =
        (controller.completedTurns +
                (controller.currentQuestion?.isFollowUp == true ? 0 : 1))
            .clamp(1, controller.turnLimit);
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 10),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: SpeakUpDesign.border)),
      ),
      child: Row(
        children: [
          const Expanded(child: Text('对话', style: SpeakUpDesign.cardTitle)),
          Flexible(
            child: Text(
              '第 $current 轮 · 共 ${controller.turnLimit} 轮',
              key: const Key('immersive-turn-progress'),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              textAlign: TextAlign.end,
              style: SpeakUpDesign.meta,
            ),
          ),
          if (controller.practiceCapabilities?.questionTipsAllowed ??
              false) ...[
            const SizedBox(width: 8),
            TextButton.icon(
              key: const Key('immersive-question-tip'),
              onPressed: controller.canRequestQuestionTip ? onShowTip : null,
              style: TextButton.styleFrom(
                foregroundColor: SpeakUpDesign.primary,
                backgroundColor: SpeakUpDesign.primaryMuted,
                minimumSize: const Size(0, 34),
                padding: const EdgeInsets.symmetric(horizontal: 10),
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                visualDensity: VisualDensity.compact,
              ),
              icon: controller.isQuestionTipLoading
                  ? const SizedBox.square(
                      dimension: 15,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.lightbulb_outline_rounded, size: 18),
              label: Text(controller.isQuestionTipLoading ? '生成中' : 'Tips'),
            ),
          ],
          if (onReplayQuestion != null || controller.canPlayQuestionAudio) ...[
            const SizedBox(width: 6),
            IconButton(
              key: const Key('immersive-replay-question'),
              tooltip:
                  (onReplayQuestion != null
                      ? replayPlaying
                      : controller.isQuestionAudioPlaying)
                  ? '停止播放'
                  : '重听对方',
              onPressed: onReplayQuestion != null
                  ? replayLoading || !_canTriggerImmersiveReplay(controller)
                        ? null
                        : () => unawaited(onReplayQuestion!())
                  : controller.canUsePracticeAudio
                  ? controller.toggleQuestionAudio
                  : null,
              visualDensity: VisualDensity.compact,
              icon:
                  (onReplayQuestion != null
                      ? replayLoading
                      : controller.isQuestionAudioLoading)
                  ? const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      (onReplayQuestion != null
                              ? replayPlaying
                              : controller.isQuestionAudioPlaying)
                          ? Icons.stop_circle_outlined
                          : Icons.volume_up_outlined,
                    ),
            ),
          ],
        ],
      ),
    );
  }
}

class _ConversationEmpty extends StatelessWidget {
  const _ConversationEmpty();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: Padding(
        padding: EdgeInsets.all(24),
        child: Text(
          '对方正在准备开场，请稍候。',
          textAlign: TextAlign.center,
          style: SpeakUpDesign.body,
        ),
      ),
    );
  }
}

class _InlineError extends StatelessWidget {
  const _InlineError({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Text(
      message,
      textAlign: TextAlign.center,
      style: const TextStyle(color: SpeakUpDesign.error, fontSize: 13),
    );
  }
}

class _ImmersiveComposer extends StatefulWidget {
  const _ImmersiveComposer({
    required this.controller,
    required this.textController,
    required this.textFocusNode,
    required this.textMode,
    required this.recordingSeconds,
    required this.onBeforeStartRecording,
    required this.onToggleTextMode,
    required this.onSubmitText,
    required this.onPracticeCompleted,
  });

  final PracticeController controller;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final bool textMode;
  final int recordingSeconds;
  final ImmersiveAsyncAction? onBeforeStartRecording;
  final VoidCallback onToggleTextMode;
  final VoidCallback onSubmitText;
  final VoidCallback onPracticeCompleted;

  @override
  State<_ImmersiveComposer> createState() => _ImmersiveComposerState();
}

class _ImmersiveComposerState extends State<_ImmersiveComposer> {
  Future<void> _sendVoice() async {
    await widget.controller.finishRecordingGesture();
    if (!mounted ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    await WidgetsBinding.instance.endOfFrame;
    if (!mounted ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    await widget.controller.confirmTranscript();
  }

  Future<void> _convertToText() async {
    await widget.controller.finishRecordingGesture();
    if (!mounted ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final transcript = widget.controller.transcript?.trim() ?? '';
    widget.controller.rerecord();
    widget.textController.value = TextEditingValue(
      text: transcript,
      selection: TextSelection.collapsed(offset: transcript.length),
    );
    if (!widget.textMode) {
      widget.onToggleTextMode();
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.controller.recordingState;
    final capturePhase = switch (state) {
      PracticeRecordingState.idle => VoiceCapturePhase.idle,
      PracticeRecordingState.starting => VoiceCapturePhase.starting,
      PracticeRecordingState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    final captureEnabled =
        !widget.textMode &&
        !widget.controller.hasPendingPracticeAudio &&
        (state == PracticeRecordingState.idle ||
            state == PracticeRecordingState.starting ||
            state == PracticeRecordingState.recording);
    return VoiceCaptureControl(
      phase: capturePhase,
      enabled: captureEnabled,
      onBeforeStart: widget.onBeforeStartRecording,
      onStart: widget.controller.startRecording,
      onSendVoice: _sendVoice,
      onConvertToText: _convertToText,
      onCancel: widget.controller.cancelRecording,
      upwardCancelOnly: true,
      builder: (context, capture) => PracticeComposerSurface(
        child: switch (state) {
          PracticeRecordingState.idle =>
            widget.controller.hasPendingPracticeAudio
                ? PracticePendingAudioComposer(
                    keyPrefix: 'immersive',
                    onDelete: widget.controller.discardPendingPracticeAudio,
                    onRetry: widget.controller.retryPracticeTranscription,
                  )
                : _IdleComposer(
                    textController: widget.textController,
                    textFocusNode: widget.textFocusNode,
                    textMode: widget.textMode,
                    onToggleTextMode: widget.onToggleTextMode,
                    onSubmitText: widget.onSubmitText,
                    capture: capture,
                  ),
          PracticeRecordingState.starting ||
          PracticeRecordingState.recording => _RecordingComposer(
            phase: capturePhase,
            seconds: widget.recordingSeconds,
            capture: capture,
          ),
          PracticeRecordingState.transcribing => const PracticeLoadingComposer(
            label: '正在识别你的回答…',
          ),
          PracticeRecordingState.awaitingConfirmation =>
            PracticeTranscriptComposer(
              transcript: widget.controller.transcript ?? '',
              keyPrefix: 'immersive',
              onRerecord: widget.controller.rerecord,
              onConfirm: widget.controller.confirmTranscript,
            ),
          PracticeRecordingState.submitting => PracticeLoadingComposer(
            label: widget.controller.isFinalSubmission
                ? '正在提交最后一轮回答…'
                : '回答已发送，Agent 正在回复…',
          ),
          PracticeRecordingState.completed => PracticeComposerAction(
            label: '练习已完成，可以返回继续对话。',
            actionLabel: '完成并返回',
            onPressed: widget.onPracticeCompleted,
          ),
        },
      ),
    );
  }
}

class _IdleComposer extends StatelessWidget {
  const _IdleComposer({
    required this.textController,
    required this.textFocusNode,
    required this.textMode,
    required this.onToggleTextMode,
    required this.onSubmitText,
    required this.capture,
  });

  final TextEditingController textController;
  final FocusNode textFocusNode;
  final bool textMode;
  final VoidCallback onToggleTextMode;
  final VoidCallback onSubmitText;
  final VoiceCaptureView capture;

  @override
  Widget build(BuildContext context) {
    return PracticeIdleComposer(
      capture: capture,
      textController: textController,
      textFocusNode: textFocusNode,
      textMode: textMode,
      onToggleTextMode: onToggleTextMode,
      onSubmitText: onSubmitText,
      keyPrefix: 'immersive',
    );
  }
}

class _RecordingComposer extends StatelessWidget {
  const _RecordingComposer({
    required this.phase,
    required this.seconds,
    required this.capture,
  });

  final VoiceCapturePhase phase;
  final int seconds;
  final VoiceCaptureView capture;

  @override
  Widget build(BuildContext context) {
    return PracticeRecordingComposer(
      capture: capture,
      phase: phase,
      keyPrefix: 'immersive',
      elapsed: Duration(seconds: seconds),
      upwardCancelOnly: true,
    );
  }
}

String _immersiveFeedbackSourceKey(
  PracticeController controller,
  PracticeMessage message,
) => 'practice:${controller.practiceSessionId}:${message.id}';

PracticeMessage? _latestAssistantMessage(List<PracticeMessage> messages) {
  for (final message in messages.reversed) {
    if (message.role == PracticeMessageRole.assistant) {
      return message;
    }
  }
  return null;
}

bool _canTriggerImmersiveReplay(PracticeController controller) {
  if (controller.isBusy) {
    return false;
  }
  return switch (controller.recordingState) {
    PracticeRecordingState.idle ||
    PracticeRecordingState.awaitingConfirmation ||
    PracticeRecordingState.completed => true,
    PracticeRecordingState.starting ||
    PracticeRecordingState.recording ||
    PracticeRecordingState.transcribing ||
    PracticeRecordingState.submitting => false,
  };
}

Future<void> _runBoundedUserTurnAction(ImmersiveAsyncAction? action) async {
  if (action == null) {
    return;
  }
  Future<void> guardedAction() async {
    try {
      await action();
    } on Object {
      // User input remains available when avatar interruption degrades.
    }
  }

  final timeout = Completer<void>();
  final timer = Timer(const Duration(milliseconds: 500), timeout.complete);
  try {
    await Future.any<void>([guardedAction(), timeout.future]);
  } finally {
    timer.cancel();
  }
}
