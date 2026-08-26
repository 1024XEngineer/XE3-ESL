import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/practice/question_tip_sheet.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_message_bubble.dart';
import 'package:speakup/features/coaching/practice/practice_completion_sheet.dart';
import 'package:speakup/features/coaching/practice/practice_stage.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/scenario/scenario_assets.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

typedef ScenarioAsyncAction = Future<void> Function();
typedef ScenarioAvatarSurfaceBuilder = Widget Function(BuildContext context);
typedef OpenScenarioPracticeReport =
    Future<CompletedPracticeRouteResult?> Function(String practiceSessionId);

class ScenarioPracticePage extends StatefulWidget {
  const ScenarioPracticePage({
    required this.practiceController,
    this.avatarSurfaceBuilder,
    this.avatarSurfaceVisible = true,
    this.avatarStatusLabel,
    this.onBeforeStartRecording,
    this.onBeforeSubmitText,
    this.onPlayQuestion,
    this.onReplayQuestion,
    this.questionSpeaker,
    this.onPracticeCompleted,
    this.onOpenReport,
    this.speechFeedbackController,
    this.onExitRequested,
    this.previewMode = false,
    this.replayLoading = false,
    this.replayPlaying = false,
    super.key,
  });

  final PracticeController practiceController;
  final ScenarioAvatarSurfaceBuilder? avatarSurfaceBuilder;
  final bool avatarSurfaceVisible;
  final String? avatarStatusLabel;
  final ScenarioAsyncAction? onBeforeStartRecording;
  final ScenarioAsyncAction? onBeforeSubmitText;
  final Future<bool> Function()? onPlayQuestion;
  final ScenarioAsyncAction? onReplayQuestion;
  final PracticePromptSpeaker? questionSpeaker;
  final Future<bool> Function()? onPracticeCompleted;
  final OpenScenarioPracticeReport? onOpenReport;
  final SpeechFeedbackController? speechFeedbackController;
  final Future<bool> Function()? onExitRequested;
  final bool previewMode;
  final bool replayLoading;
  final bool replayPlaying;

  @override
  State<ScenarioPracticePage> createState() => _ScenarioPracticePageState();
}

class _ScenarioPracticePageState extends State<ScenarioPracticePage> {
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
  bool _reportRouteActive = false;
  PracticePromptSpeaker? _ownedQuestionSpeaker;
  PracticePromptSpeaker? _ownedTipSpeaker;
  String? _visibleTipQuestionId;
  String? _playingQuestionId;
  String? _questionNarrationErrorId;
  String? _autoNarratedQuestionId;
  int _questionNarrationGeneration = 0;

  bool get _isInterview =>
      widget.practiceController.practiceExperience ==
      PracticeExperience.interview;

  @override
  void initState() {
    super.initState();
    _observedMessageCount = widget.practiceController.practiceMessages.length;
    widget.practiceController.addListener(_handleControllerState);
    widget.speechFeedbackController?.addListener(_handleFeedbackState);
    _syncSpeechFeedbackSources();
    _syncRecordingTimer();
    _scheduleConversationScrollToBottom(animated: false);
    _scheduleQuestionNarration();
  }

  @override
  void didUpdateWidget(covariant ScenarioPracticePage oldWidget) {
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
      _questionTranslations.clear();
      _questionNarrationGeneration++;
      _autoNarratedQuestionId = null;
      _playingQuestionId = null;
      _questionNarrationErrorId = null;
      unawaited(oldWidget.practiceController.stopPracticeAudio(notify: false));
      final previousSpeaker =
          oldWidget.questionSpeaker ?? _ownedQuestionSpeaker;
      if (previousSpeaker != null) {
        unawaited(_stopSpeakerSafely(previousSpeaker));
      }
      widget.practiceController.addListener(_handleControllerState);
      _syncRecordingTimer();
      _scheduleQuestionNarration();
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
    if (widget.questionSpeaker case final speaker?) {
      unawaited(_stopSpeakerSafely(speaker));
    } else if (_ownedQuestionSpeaker case final speaker?) {
      unawaited(speaker.dispose());
    }
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
    _scheduleConversationScrollToBottom();
  }

  Future<void> _speakQuestionTip() async {
    final tip = widget.practiceController.questionTip;
    if (tip == null || _visibleTipQuestionId != tip.questionId) {
      return;
    }
    await _stopQuestionNarration();
    final speaker =
        widget.questionSpeaker ??
        (_ownedTipSpeaker ??= SystemPracticePromptSpeaker());
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
      await (widget.questionSpeaker ?? _ownedTipSpeaker)?.stop();
    } on Object {
      // Recording and dismissal must not be blocked by a platform TTS error.
    }
  }

  Future<void> _beforeStartRecording() async {
    await _stopQuestionTipSpeech();
    await _stopQuestionNarration();
    await _runBoundedUserTurnAction(widget.onBeforeStartRecording);
  }

  Future<void> _playQuestion(PracticeMessage message) async {
    final controller = widget.practiceController;
    final currentQuestion = controller.currentQuestion;
    final current = currentQuestion?.id == message.id;

    if (_playingQuestionId == message.id) {
      await _stopQuestionNarration();
      return;
    }

    await _stopQuestionTipSpeech();

    if (current && widget.onReplayQuestion != null) {
      _questionNarrationGeneration++;
      await _stopQuestionSpeakerSafely();
      if (!mounted) {
        return;
      }
      setState(() => _questionNarrationErrorId = null);
      try {
        await widget.onReplayQuestion!();
      } on Object {
        if (mounted) {
          setState(() => _questionNarrationErrorId = message.id);
        }
      }
      return;
    }

    if (current && controller.canPlayQuestionAudio) {
      _questionNarrationGeneration++;
      await _stopQuestionSpeakerSafely();
      if (mounted) {
        setState(() => _questionNarrationErrorId = null);
      }
      await controller.toggleQuestionAudio();
      return;
    }

    await _stopQuestionNarration();
    final generation = ++_questionNarrationGeneration;
    if (!mounted) {
      return;
    }
    setState(() {
      _playingQuestionId = message.id;
      _questionNarrationErrorId = null;
    });
    try {
      final speaker = _questionSpeaker;
      if (speaker is CoachingSpeechPlayer) {
        await speaker.speakQuestion(
          questionId: message.id,
          fallbackText: message.text,
        );
      } else {
        await speaker.speak(message.text);
      }
    } on Object {
      if (mounted && generation == _questionNarrationGeneration) {
        setState(() => _questionNarrationErrorId = message.id);
      }
    } finally {
      if (mounted && generation == _questionNarrationGeneration) {
        setState(() => _playingQuestionId = null);
      }
    }
  }

  void _scheduleQuestionNarration() {
    if (widget.onPlayQuestion == null) {
      // Without an avatar session the PracticeController owns automatic
      // realtime question speech.
      return;
    }
    final question = widget.practiceController.currentQuestion;
    if (question == null || _autoNarratedQuestionId == question.id) {
      return;
    }
    _autoNarratedQuestionId = question.id;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          widget.practiceController.currentQuestion?.id != question.id) {
        return;
      }
      unawaited(_playCurrentQuestion(question));
    });
  }

  Future<void> _playCurrentQuestion(PracticeQuestion question) async {
    final playWithAvatar = widget.onPlayQuestion;
    if (playWithAvatar == null) {
      await _playQuestion(question.presentation);
      return;
    }
    final generation = ++_questionNarrationGeneration;
    if (mounted) {
      setState(() => _questionNarrationErrorId = null);
    }
    final completed = await playWithAvatar();
    if (!completed &&
        mounted &&
        generation == _questionNarrationGeneration &&
        widget.practiceController.currentQuestion?.id == question.id) {
      setState(() => _questionNarrationErrorId = question.id);
    }
  }

  Future<void> _stopQuestionNarration() async {
    _questionNarrationGeneration++;
    await widget.practiceController.stopPracticeAudio();
    await _stopQuestionSpeakerSafely();
    if (mounted && _playingQuestionId != null) {
      setState(() => _playingQuestionId = null);
    }
  }

  Future<void> _stopQuestionSpeakerSafely() async {
    final speaker = widget.questionSpeaker ?? _ownedQuestionSpeaker;
    if (speaker == null) {
      return;
    }
    await _stopSpeakerSafely(speaker);
  }

  Future<void> _stopSpeakerSafely(PracticePromptSpeaker speaker) async {
    try {
      await speaker.stop();
    } on Object {
      // Recording and navigation remain usable when platform TTS degrades.
    }
  }

  PracticePromptSpeaker get _questionSpeaker =>
      widget.questionSpeaker ??
      (_ownedQuestionSpeaker ??= SystemPracticePromptSpeaker());

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
    _scheduleQuestionNarration();
    if (shouldFollowConversation) {
      _scheduleConversationScrollToBottom();
    }
  }

  void _scheduleConversationScrollToBottom({bool animated = true}) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_conversationScrollController.hasClients) {
        return;
      }
      final target = _conversationScrollController.position.maxScrollExtent;
      if (!animated) {
        _conversationScrollController.jumpTo(target);
        return;
      }
      unawaited(
        _conversationScrollController.animateTo(
          target,
          duration: const Duration(milliseconds: 220),
          curve: Curves.easeOutCubic,
        ),
      );
    });
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
        current[_scenarioFeedbackSourceKey(
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
    await _stopQuestionTipSpeech();
    await _stopQuestionNarration();
    if (!mounted) {
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
      Navigator.of(context).pop(CompletedPracticeRouteResult.returnToTraining);
      return;
    }
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(const SnackBar(content: Text('练习正在完成，请稍后重试。')));
  }

  Future<void> _requestUserControlledCompletion() async {
    final controller = widget.practiceController;
    if (!mounted || !controller.canCompleteActivePractice) {
      return;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(_isInterview ? '结束面试练习？' : '结束练习？'),
        content: Text(
          _isInterview
              ? '结束后将保存本次回答并生成面试复盘。'
              : '结束后将保存本次对话并生成练习报告，稍后可在“复盘”中查看。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('继续练习'),
          ),
          FilledButton(
            key: const Key('scenario-confirm-completion'),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('结束练习'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) {
      return;
    }
    final completed = await controller.completeActivePractice();
    if (!mounted) {
      return;
    }
    if (completed) {
      return;
    }
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(const SnackBar(content: Text('练习暂时无法结束，请稍后重试。')));
  }

  Future<void> _openCompletedReport() async {
    final callback = widget.onOpenReport;
    final sessionId = widget.practiceController.practiceSessionId;
    if (callback == null ||
        sessionId == null ||
        widget.practiceController.recordingState !=
            PracticeRecordingState.completed ||
        _reportRouteActive) {
      return;
    }
    _reportRouteActive = true;
    try {
      final result = await callback(sessionId);
      if (mounted && result != null) {
        Navigator.of(context).pop(result);
      }
    } finally {
      _reportRouteActive = false;
    }
  }

  Future<void> _submitText() async {
    await _stopQuestionTipSpeech();
    await _stopQuestionNarration();
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
    await _stopQuestionTipSpeech();
    await _stopQuestionNarration();
    if (!mounted) {
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
        key: const Key('scenario-practice-page'),
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: scene == null
              ? const Center(child: Text('请先选择一个情景开始对话。'))
              : PracticeStageLayout(
                  stageRegionKey: const Key('scenario-role-region'),
                  stage: PracticeRoleStage(
                    title: scene.name,
                    fallback: PracticeRoleFallback(
                      assetName: scenarioStageAssetPath(scene),
                      semanticLabel: '${scene.name}角色画面',
                      imageKey: const Key('scenario-role-placeholder'),
                    ),
                    surfaceBuilder: widget.avatarSurfaceBuilder,
                    surfaceVisible: widget.avatarSurfaceVisible,
                    statusLabel: widget.avatarStatusLabel,
                    exitInFlight: _exitInFlight,
                    exitButtonKey: const Key('scenario-exit'),
                    onExit: _requestExit,
                  ),
                  content: Stack(
                    children: [
                      _ConversationPanel(
                        controller: widget.practiceController,
                        scrollController: _conversationScrollController,
                        textController: _textController,
                        textFocusNode: _textFocusNode,
                        textMode: _textMode,
                        recordingSeconds: _recordingSeconds,
                        previewMode: widget.previewMode,
                        onBeforeStartRecording: _beforeStartRecording,
                        speechFeedbackController:
                            widget.speechFeedbackController,
                        playingQuestionId: _playingQuestionId,
                        narrationErrorQuestionId: _questionNarrationErrorId,
                        onPlayQuestion: _playQuestion,
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
                        onCompleteRequested: _requestUserControlledCompletion,
                      ),
                      if (widget.practiceController.recordingState ==
                          PracticeRecordingState.completed)
                        Positioned.fill(
                          child: PracticeCompletionOverlay(
                            keyPrefix: _isInterview
                                ? 'interview-completion'
                                : 'scenario-completion',
                            title: _isInterview ? '面试练习已完成' : '场景练习已完成',
                            message: _isInterview
                                ? '${widget.practiceController.completedTurns} 道回答已保存'
                                : '${widget.practiceController.completedTurns} 轮对话已保存',
                            primaryLabel: '查看复盘报告',
                            secondaryLabel: _isInterview ? '返回面试列表' : '返回场景列表',
                            onPrimary: widget.onOpenReport == null
                                ? null
                                : _openCompletedReport,
                            onSecondary: _completionInFlight
                                ? null
                                : _completePractice,
                            primaryLoading: _reportRouteActive,
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
    required this.speechFeedbackController,
    required this.playingQuestionId,
    required this.narrationErrorQuestionId,
    required this.onPlayQuestion,
    required this.onToggleTextMode,
    required this.onSubmitText,
    required this.onTranslateQuestion,
    required this.onShowTip,
    required this.onHideTip,
    required this.onSpeakTip,
    required this.visibleTipQuestionId,
    required this.onCompleteRequested,
  });

  final PracticeController controller;
  final ScrollController scrollController;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final bool textMode;
  final int recordingSeconds;
  final bool previewMode;
  final ScenarioAsyncAction? onBeforeStartRecording;
  final SpeechFeedbackController? speechFeedbackController;
  final String? playingQuestionId;
  final String? narrationErrorQuestionId;
  final Future<void> Function(PracticeMessage message) onPlayQuestion;
  final VoidCallback onToggleTextMode;
  final VoidCallback onSubmitText;
  final Future<String> Function(PracticeMessage message)? onTranslateQuestion;
  final VoidCallback onShowTip;
  final VoidCallback onHideTip;
  final Future<void> Function() onSpeakTip;
  final String? visibleTipQuestionId;
  final VoidCallback onCompleteRequested;

  @override
  Widget build(BuildContext context) {
    final messages = controller.practiceMessages;
    return ColoredBox(
      color: SpeakUpDesign.surface,
      child: Column(
        children: [
          _ConversationHeader(
            controller: controller,
            onCompleteRequested: onCompleteRequested,
          ),
          Expanded(
            child: messages.isEmpty
                ? const _ConversationEmpty()
                : ListView.separated(
                    key: const Key('scenario-conversation-history'),
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
                        final assistant =
                            message.role == PracticeMessageRole.assistant;
                        final currentQuestion =
                            assistant && message.id == controller.questionId;
                        final playing =
                            playingQuestionId == message.id ||
                            (currentQuestion &&
                                controller.isQuestionAudioPlaying);
                        final playbackLoading =
                            currentQuestion &&
                            controller.isQuestionAudioLoading;
                        final tipsAvailable =
                            currentQuestion &&
                            (controller
                                    .practiceCapabilities
                                    ?.questionTipsAllowed ??
                                false);
                        final tip = controller.questionTip;
                        final showTip =
                            assistant &&
                            tip != null &&
                            tip.questionId == message.id &&
                            tip.questionId == visibleTipQuestionId;
                        final tipError = currentQuestion
                            ? controller.questionTipErrorMessage
                            : null;
                        final projection = _feedbackProjection(message);
                        return Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            PracticeMessageBubble(
                              message: message,
                              practiceController: controller,
                              feedbackProjection: projection,
                              messageTextVisible: true,
                              onTranslate: assistant
                                  ? onTranslateQuestion
                                  : null,
                              actions: assistant
                                  ? _ScenarioQuestionActions(
                                      messageId: message.id,
                                      playing: playing,
                                      playbackLoading: playbackLoading,
                                      playbackFailed:
                                          narrationErrorQuestionId ==
                                          message.id,
                                      playbackEnabled:
                                          !playbackLoading &&
                                          _canTriggerScenarioReplay(controller),
                                      tipsAvailable: tipsAvailable,
                                      tipLoading:
                                          currentQuestion &&
                                          controller.isQuestionTipLoading,
                                      tipEnabled:
                                          currentQuestion &&
                                          controller.canRequestQuestionTip,
                                      onPlay: () =>
                                          unawaited(onPlayQuestion(message)),
                                      onShowTip: onShowTip,
                                    )
                                  : null,
                            ),
                            if (showTip)
                              Padding(
                                padding: const EdgeInsets.only(top: 4),
                                child: QuestionTipCard(
                                  content: tip.content,
                                  translation: tip.translation,
                                  onClose: onHideTip,
                                  onSpeak: onSpeakTip,
                                ),
                              ),
                            if (assistant && tipError != null)
                              Padding(
                                padding: const EdgeInsets.fromLTRB(
                                  12,
                                  4,
                                  12,
                                  0,
                                ),
                                child: Text(
                                  tipError,
                                  key: const Key('scenario-question-tip-error'),
                                  style: const TextStyle(
                                    color: SpeakUpDesign.error,
                                    fontSize: 12,
                                  ),
                                ),
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
          _ScenarioComposer(
            controller: controller,
            textController: textController,
            textFocusNode: textFocusNode,
            textMode: textMode,
            recordingSeconds: recordingSeconds,
            onBeforeStartRecording: onBeforeStartRecording,
            onToggleTextMode: onToggleTextMode,
            onSubmitText: onSubmitText,
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
      _scenarioFeedbackSourceKey(controller, message),
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
    required this.onCompleteRequested,
  });

  final PracticeController controller;
  final VoidCallback onCompleteRequested;

  @override
  Widget build(BuildContext context) {
    final current =
        (controller.completedTurns +
                (controller.currentQuestion?.isFollowUp == true ? 0 : 1))
            .clamp(1, controller.turnLimit == 0 ? 1 : controller.turnLimit);
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 10),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: SpeakUpDesign.border)),
      ),
      child: Row(
        children: [
          const Expanded(child: Text('对话', style: SpeakUpDesign.cardTitle)),
          if (controller.completionMode ==
              PracticeCompletionMode.userControlled)
            TextButton.icon(
              key: const Key('scenario-complete-practice'),
              onPressed: controller.canCompleteActivePractice
                  ? onCompleteRequested
                  : null,
              icon: const Icon(Icons.stop_circle_outlined, size: 18),
              label: const Text('结束练习'),
            )
          else
            Flexible(
              child: Text(
                '第 $current 轮 · 共 ${controller.turnLimit} 轮',
                key: const Key('scenario-turn-progress'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.end,
                style: SpeakUpDesign.meta,
              ),
            ),
        ],
      ),
    );
  }
}

class _ScenarioQuestionActions extends StatelessWidget {
  const _ScenarioQuestionActions({
    required this.messageId,
    required this.playing,
    required this.playbackLoading,
    required this.playbackFailed,
    required this.playbackEnabled,
    required this.tipsAvailable,
    required this.tipLoading,
    required this.tipEnabled,
    required this.onPlay,
    required this.onShowTip,
  });

  final String messageId;
  final bool playing;
  final bool playbackLoading;
  final bool playbackFailed;
  final bool playbackEnabled;
  final bool tipsAvailable;
  final bool tipLoading;
  final bool tipEnabled;
  final VoidCallback onPlay;
  final VoidCallback onShowTip;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 0,
      runSpacing: 4,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        TextButton.icon(
          key: ValueKey('scenario-question-voice-$messageId'),
          onPressed: playbackEnabled ? onPlay : null,
          style: TextButton.styleFrom(
            foregroundColor: SpeakUpDesign.primary,
            backgroundColor: SpeakUpDesign.surfaceMuted,
            minimumSize: const Size(0, 32),
            padding: const EdgeInsets.symmetric(horizontal: 10),
            visualDensity: VisualDensity.compact,
            textStyle: const TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w600,
            ),
          ),
          icon: playbackLoading
              ? const SizedBox.square(
                  dimension: 14,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : Icon(
                  playing ? Icons.stop_rounded : Icons.volume_up_outlined,
                  size: 17,
                ),
          label: Text(
            playing
                ? '停止朗读'
                : playbackFailed
                ? '重试朗读'
                : '朗读',
          ),
        ),
        if (tipsAvailable)
          TextButton.icon(
            key: ValueKey('scenario-question-tip-$messageId'),
            onPressed: tipEnabled ? onShowTip : null,
            style: TextButton.styleFrom(
              foregroundColor: SpeakUpDesign.primary,
              minimumSize: const Size(0, 32),
              padding: const EdgeInsets.symmetric(horizontal: 8),
              visualDensity: VisualDensity.compact,
              textStyle: const TextStyle(
                fontSize: 13,
                fontWeight: FontWeight.w600,
              ),
            ),
            icon: tipLoading
                ? const SizedBox.square(
                    dimension: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.lightbulb_outline_rounded, size: 17),
            label: Text(tipLoading ? '生成中' : 'Tips'),
          ),
      ],
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

class _ScenarioComposer extends StatefulWidget {
  const _ScenarioComposer({
    required this.controller,
    required this.textController,
    required this.textFocusNode,
    required this.textMode,
    required this.recordingSeconds,
    required this.onBeforeStartRecording,
    required this.onToggleTextMode,
    required this.onSubmitText,
  });

  final PracticeController controller;
  final TextEditingController textController;
  final FocusNode textFocusNode;
  final bool textMode;
  final int recordingSeconds;
  final ScenarioAsyncAction? onBeforeStartRecording;
  final VoidCallback onToggleTextMode;
  final VoidCallback onSubmitText;

  @override
  State<_ScenarioComposer> createState() => _ScenarioComposerState();
}

class _ScenarioComposerState extends State<_ScenarioComposer> {
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
                    keyPrefix: 'scenario',
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
            transcript: widget.controller.transcript ?? '',
          ),
          PracticeRecordingState.transcribing => PracticeLoadingComposer(
            label: '正在识别你的回答…',
          ),
          PracticeRecordingState.awaitingConfirmation =>
            PracticeTranscriptComposer(
              transcript: widget.controller.transcript ?? '',
              keyPrefix: 'scenario',
              onRerecord: widget.controller.rerecord,
              onConfirm: widget.controller.confirmTranscript,
            ),
          PracticeRecordingState.submitting => PracticeLoadingComposer(
            label: widget.controller.isFinalSubmission
                ? '正在提交最后一轮回答…'
                : '回答已发送，Agent 正在回复…',
          ),
          PracticeRecordingState.completed => SizedBox(
            height: 48,
            child: Center(
              child: Text(
                '练习已完成',
                style: SpeakUpDesign.body.copyWith(
                  color: SpeakUpDesign.secondary,
                ),
              ),
            ),
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
      keyPrefix: 'scenario',
    );
  }
}

class _RecordingComposer extends StatelessWidget {
  const _RecordingComposer({
    required this.phase,
    required this.seconds,
    required this.capture,
    required this.transcript,
  });

  final VoiceCapturePhase phase;
  final int seconds;
  final VoiceCaptureView capture;
  final String transcript;

  @override
  Widget build(BuildContext context) {
    return PracticeRecordingComposer(
      capture: capture,
      phase: phase,
      keyPrefix: 'scenario',
      elapsed: Duration(seconds: seconds),
      transcript: transcript,
      upwardCancelOnly: true,
    );
  }
}

String _scenarioFeedbackSourceKey(
  PracticeController controller,
  PracticeMessage message,
) => 'practice:${controller.practiceSessionId}:${message.id}';

bool _canTriggerScenarioReplay(PracticeController controller) {
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

Future<void> _runBoundedUserTurnAction(ScenarioAsyncAction? action) async {
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
