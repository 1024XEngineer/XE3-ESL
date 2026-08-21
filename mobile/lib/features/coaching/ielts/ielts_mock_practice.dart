import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/design/voice_composer_dock.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_progress_store.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_completion_sheet.dart';
import 'package:speakup/features/coaching/practice/practice_message_bubble.dart';
import 'package:speakup/features/coaching/practice/practice_stage.dart';
import 'package:speakup/features/coaching/practice/question_tip_sheet.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_controller.dart';
import 'package:speakup/features/coaching/review/evaluation_report_detail_page.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';

part 'ielts_mock_practice_widgets.dart';
part 'ielts_mock_practice_review.dart';

const _part2IntroNarration =
    'You will have one minute to prepare and up to two minutes to speak. '
    'You may take notes during preparation.';
final RegExp _ieltsEnglishLetter = RegExp('[A-Za-z]');
final RegExp _ieltsChineseCharacter = RegExp(
  r'[\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF]',
);

bool _requiresEnglishRetry(String text) =>
    _ieltsChineseCharacter.hasMatch(text) &&
    !_ieltsEnglishLetter.hasMatch(text);

bool isIeltsSpeakingFullMockSession(PracticeController controller) =>
    controller.practiceExperience == PracticeExperience.ieltsSpeaking &&
    controller.practiceMode == PracticeMode.fullMock;

bool isIeltsSpeakingSession(PracticeController controller) =>
    controller.practiceExperience == PracticeExperience.ieltsSpeaking;

enum IeltsPracticeCompletionAction { next, retry, list }

typedef IeltsCompletedReportBuilder =
    Widget Function(BuildContext context, String practiceSessionId);

final class IeltsPracticeRouteResult {
  const IeltsPracticeRouteResult({
    required this.mode,
    required this.action,
    this.selection,
  });

  final PracticeMode mode;
  final IeltsPracticeCompletionAction action;
  final IeltsPracticeSelection? selection;
}

class IeltsSpeakingMockPage extends StatefulWidget {
  const IeltsSpeakingMockPage({
    required this.controller,
    this.onExitRequested,
    this.progressStore,
    this.ieltsController,
    this.completedReportBuilder,
    this.reportStatusController,
    this.speechFeedbackController,
    this.examinerSpeaker,
    this.avatarSurfaceBuilder,
    this.avatarStatusLabel,
    this.onBeforeUserTurn,
    this.onReplayQuestionWithAvatar,
    this.avatarReplayLoading = false,
    this.avatarReplayPlaying = false,
    this.now = DateTime.now,
    super.key,
  });

  final PracticeController controller;
  final Future<bool> Function()? onExitRequested;
  final IeltsMockProgressStore? progressStore;
  final IeltsPreparationController? ieltsController;
  final IeltsCompletedReportBuilder? completedReportBuilder;
  final SessionEvaluationController? reportStatusController;
  final SpeechFeedbackController? speechFeedbackController;
  final PracticePromptSpeaker? examinerSpeaker;
  final WidgetBuilder? avatarSurfaceBuilder;
  final String? avatarStatusLabel;
  final Future<void> Function()? onBeforeUserTurn;
  final Future<void> Function()? onReplayQuestionWithAvatar;
  final bool avatarReplayLoading;
  final bool avatarReplayPlaying;
  final DateTime Function() now;

  @override
  State<IeltsSpeakingMockPage> createState() => _IeltsSpeakingMockPageState();
}

class _IeltsSpeakingMockPageState extends State<IeltsSpeakingMockPage> {
  late final IeltsMockProgressStore _progressStore;
  late final PracticePromptSpeaker _examinerSpeaker;
  late final bool _ownsExaminerSpeaker;
  final ScrollController _conversationScrollController = ScrollController();
  final TextEditingController _notesController = TextEditingController();
  final TextEditingController _convertedAnswerController =
      TextEditingController();
  final FocusNode _convertedAnswerFocusNode = FocusNode();

  IeltsMockProgress? _progress;
  Timer? _ticker;
  Timer? _recordingTicker;
  int _recordingSeconds = 0;
  Timer? _part2TranscriptionRetryTimer;
  DateTime _now = DateTime.now().toUtc();
  bool _loading = true;
  bool _disposing = false;
  bool _confirming = false;
  bool _conversionRequested = false;
  bool _convertedAnswerMode = false;
  bool _convertedAnswerSubmitting = false;
  bool _startingPart2Recording = false;
  bool _finishingPart2Recording = false;
  bool _part2DeadlineHandled = false;
  bool _part2RetryNeeded = false;
  bool _enteringPart3 = false;
  String? _part2QuestionId;
  int _part2TranscriptionRetryAttempts = 0;
  bool _exitApproved = false;
  bool _exitPromptOpen = false;
  bool _exitInFlight = false;
  bool _narrationBusy = false;
  bool _introNarrated = false;
  String? _narrationError;
  String? _answerLanguageError;
  final Map<String, String> _questionTranslations = <String, String>{};
  String? _visibleTipQuestionId;
  String? _autoNarratedQuestionId;
  String? _autoNarratedQuestionText;
  String? _playingQuestionId;
  String? _questionNarrationErrorId;
  int _questionNarrationGeneration = 0;
  final Set<PracticeMode> _recordedCompletions = <PracticeMode>{};
  final Map<String, String> _feedbackSources = <String, String>{};
  bool _feedbackRebuildScheduled = false;
  int _observedMessageCount = 0;
  int _observedCompletedTurns = 0;
  bool _preserveCompletedConversation = false;
  bool _showCompletionSheet = false;
  bool _openingReadyReport = false;

  IeltsPracticeSelection? get _selection {
    final sessionId = widget.controller.practiceSessionId;
    return sessionId == null
        ? null
        : widget.ieltsController?.selectionForSession(sessionId);
  }

  bool get _part2TurnConfirmed => _part2ResponseAlreadyConfirmed;

  bool get _part2ResponseAlreadyConfirmed {
    final part2 = _assignment.part(IeltsSpeakingPart.part2);
    return part2 == null ||
        widget.controller.completedTurns >= _partEnd(IeltsSpeakingPart.part2);
  }

  bool get _part2Processing {
    final state = widget.controller.recordingState;
    final submissionActive =
        state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording ||
        state == PracticeRecordingState.transcribing ||
        state == PracticeRecordingState.awaitingConfirmation ||
        state == PracticeRecordingState.submitting;
    return !_part2TurnConfirmed &&
        (_finishingPart2Recording ||
            (_part2TranscriptionRetryTimer != null &&
                widget.controller.hasPendingPracticeAudio) ||
            submissionActive);
  }

  PracticeMode get _mode {
    final mode = widget.controller.practiceMode;
    if (mode != PracticeMode.fullMock &&
        mode != PracticeMode.part1 &&
        mode != PracticeMode.part2 &&
        mode != PracticeMode.part3) {
      throw StateError('IELTS practice requires an IELTS PracticeOption mode.');
    }
    return mode!;
  }

  IeltsPracticeAssignment get _assignment {
    final assignment = widget.controller.ieltsAssignment;
    if (assignment == null || assignment.mode != _mode) {
      throw StateError('IELTS practice requires its frozen assignment.');
    }
    return assignment;
  }

  int _partStart(IeltsSpeakingPart target) {
    var start = 0;
    for (final part in _assignment.parts) {
      if (part.part == target) {
        return start;
      }
      start += part.turnBlueprints.length;
    }
    return start;
  }

  int _partEnd(IeltsSpeakingPart target) {
    final part = _assignment.part(target);
    return _partStart(target) + (part?.turnBlueprints.length ?? 0);
  }

  int get _part1Total =>
      _assignment.part(IeltsSpeakingPart.part1)?.turnBlueprints.length ?? 0;

  int get _part2Total =>
      _assignment.part(IeltsSpeakingPart.part2)?.turnBlueprints.length ?? 0;

  int get _part3Total =>
      _assignment.part(IeltsSpeakingPart.part3)?.turnBlueprints.length ?? 0;

  @override
  void initState() {
    super.initState();
    _progressStore = widget.progressStore ?? FileIeltsMockProgressStore();
    _ownsExaminerSpeaker = widget.examinerSpeaker == null;
    _examinerSpeaker = widget.examinerSpeaker ?? SystemPracticePromptSpeaker();
    _now = widget.now().toUtc();
    _observedMessageCount = widget.controller.practiceMessages.length;
    _observedCompletedTurns = widget.controller.completedTurns;
    widget.controller.addListener(_handleControllerState);
    widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    _notesController.addListener(_saveNotes);
    _syncSpeechFeedbackSources();
    _syncRecordingTimer();
    unawaited(_restoreProgress());
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _jumpToLatestMessage();
    });
  }

  @override
  void didUpdateWidget(covariant IeltsSpeakingMockPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    final controllerChanged = oldWidget.controller != widget.controller;
    if (controllerChanged) {
      oldWidget.controller.removeListener(_handleControllerState);
      widget.controller.addListener(_handleControllerState);
      _observedMessageCount = widget.controller.practiceMessages.length;
      _observedCompletedTurns = widget.controller.completedTurns;
      _preserveCompletedConversation = false;
      _showCompletionSheet = false;
      _openingReadyReport = false;
      _questionNarrationGeneration++;
      _autoNarratedQuestionId = null;
      _autoNarratedQuestionText = null;
      _playingQuestionId = null;
      _questionNarrationErrorId = null;
      _questionTranslations.clear();
      _visibleTipQuestionId = null;
      unawaited(_stopExaminerSpeakerSafely());
      unawaited(_restoreProgress());
      _syncRecordingTimer();
    }
    if (oldWidget.speechFeedbackController != widget.speechFeedbackController) {
      oldWidget.speechFeedbackController?.removeListener(
        _handleSpeechFeedbackState,
      );
      _removeSpeechFeedbackSources(oldWidget.speechFeedbackController);
      widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    }
    if (controllerChanged ||
        oldWidget.speechFeedbackController != widget.speechFeedbackController) {
      _syncSpeechFeedbackSources();
    }
    if (oldWidget.reportStatusController != widget.reportStatusController) {
      final sessionId = oldWidget.reportStatusController?.practiceSessionId;
      if (sessionId != null) {
        oldWidget.reportStatusController?.cancel(sessionId);
      }
      _openingReadyReport = false;
      _syncCompletionReport();
    }
  }

  @override
  void dispose() {
    _disposing = true;
    widget.controller.removeListener(_handleControllerState);
    widget.speechFeedbackController?.removeListener(_handleSpeechFeedbackState);
    _removeSpeechFeedbackSources(widget.speechFeedbackController);
    unawaited(widget.controller.cancelRecording());
    _notesController.removeListener(_saveNotes);
    _notesController.dispose();
    _conversationScrollController.dispose();
    _convertedAnswerController.dispose();
    _convertedAnswerFocusNode.dispose();
    _ticker?.cancel();
    _recordingTicker?.cancel();
    _part2TranscriptionRetryTimer?.cancel();
    unawaited(_stopExaminerSpeakerSafely());
    if (_ownsExaminerSpeaker) {
      unawaited(_examinerSpeaker.dispose());
    }
    final reportSessionId = widget.reportStatusController?.practiceSessionId;
    if (reportSessionId != null) {
      widget.reportStatusController?.cancel(reportSessionId);
    }
    super.dispose();
  }

  Future<void> _restoreProgress() async {
    final sessionId = widget.controller.practiceSessionId;
    if (sessionId == null) {
      if (mounted) {
        setState(() => _loading = false);
      }
      return;
    }
    final stored = await _progressStore.read(sessionId);
    if (!mounted || sessionId != widget.controller.practiceSessionId) {
      return;
    }
    final restored = _reconcileProgress(
      stored ??
          IeltsMockProgress(
            sessionId: sessionId,
            phase: _initialPhase(),
            startedAt: widget.now().toUtc(),
          ),
    );
    _notesController.text = restored.notes;
    if (!_part2ResponseAlreadyConfirmed &&
        (restored.phase == IeltsMockPhase.part2Intro ||
            restored.phase == IeltsMockPhase.part2CueCard ||
            restored.phase == IeltsMockPhase.part2Preparation ||
            restored.phase == IeltsMockPhase.part2Speaking ||
            restored.phase == IeltsMockPhase.part2Complete)) {
      _part2QuestionId ??= widget.controller.currentQuestion?.id;
    }
    setState(() {
      _progress = restored;
      _loading = false;
      _now = widget.now().toUtc();
    });
    await _progressStore.write(restored);
    if (!mounted) {
      return;
    }
    _syncTicker();
    _handleExpiredTimer();
    if (restored.phase == IeltsMockPhase.part2Speaking &&
        widget.controller.recordingState == PracticeRecordingState.idle &&
        !widget.controller.hasPendingPracticeAudio &&
        widget.controller.errorMessage == null &&
        _secondsUntil(restored.speakingDeadline) > 0) {
      unawaited(_startPart2Speaking(restart: true));
    } else if (restored.phase == IeltsMockPhase.part2Speaking &&
        widget.controller.recordingState == PracticeRecordingState.idle &&
        _secondsUntil(restored.speakingDeadline) <= 0) {
      setState(() => _part2RetryNeeded = true);
    }
    _recordCompletedParts();
    _syncCompletionReport();
    unawaited(_resumePart2Narration(restored.phase));
    _scheduleQuestionNarration();
  }

  IeltsMockPhase _initialPhase() => switch (_mode) {
    PracticeMode.fullMock || PracticeMode.part1 => IeltsMockPhase.part1,
    PracticeMode.part2 => IeltsMockPhase.part2Intro,
    PracticeMode.part3 => IeltsMockPhase.part3,
    PracticeMode.fullSimulation ||
    PracticeMode.focus => throw StateError('Non-IELTS mode in IELTS practice.'),
  };

  IeltsMockProgress _reconcileProgress(IeltsMockProgress value) {
    final completed = widget.controller.completedTurns;
    if (_mode == PracticeMode.part1) {
      return value.copyWith(
        phase: completed >= _partEnd(IeltsSpeakingPart.part1)
            ? IeltsMockPhase.complete
            : IeltsMockPhase.part1,
        clearPreparationDeadline: true,
        clearSpeakingStartedAt: true,
        clearSpeakingDeadline: true,
      );
    }
    if (_mode == PracticeMode.part3) {
      return value.copyWith(
        phase: completed >= widget.controller.turnLimit
            ? IeltsMockPhase.complete
            : IeltsMockPhase.part3,
        clearPreparationDeadline: true,
        clearSpeakingStartedAt: true,
        clearSpeakingDeadline: true,
      );
    }
    if (_mode == PracticeMode.part2) {
      if (completed >= widget.controller.turnLimit) {
        return value.copyWith(
          phase: IeltsMockPhase.complete,
          clearPreparationDeadline: true,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        );
      }
      if (completed >= _partEnd(IeltsSpeakingPart.part2)) {
        if (value.phase == IeltsMockPhase.part3) {
          return value.copyWith(
            phase: value.phase,
            clearPreparationDeadline: true,
            clearSpeakingStartedAt: true,
            clearSpeakingDeadline: true,
          );
        }
        return value.copyWith(
          phase: IeltsMockPhase.part2Complete,
          clearPreparationDeadline: true,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        );
      }
      final phase = switch (value.phase) {
        IeltsMockPhase.part2Intro ||
        IeltsMockPhase.part2CueCard ||
        IeltsMockPhase.part2Preparation => value.phase,
        IeltsMockPhase.part2Complete => IeltsMockPhase.part2Complete,
        IeltsMockPhase.part2Speaking ||
        IeltsMockPhase.part3 => IeltsMockPhase.part2Speaking,
        _ => IeltsMockPhase.part2Intro,
      };
      return value.copyWith(phase: phase);
    }
    if (completed >= widget.controller.turnLimit) {
      return value.copyWith(
        phase: IeltsMockPhase.complete,
        clearPreparationDeadline: true,
        clearSpeakingDeadline: true,
      );
    }
    if (completed >= _partEnd(IeltsSpeakingPart.part2)) {
      final phase = switch (value.phase) {
        IeltsMockPhase.part3 => IeltsMockPhase.part3,
        _ => IeltsMockPhase.part2Complete,
      };
      return value.copyWith(
        phase: phase,
        clearPreparationDeadline: true,
        clearSpeakingStartedAt: true,
        clearSpeakingDeadline: true,
      );
    }
    if (value.phase == IeltsMockPhase.part3) {
      return value.copyWith(
        phase: IeltsMockPhase.part2Speaking,
        clearPreparationDeadline: true,
        clearSpeakingStartedAt: true,
        clearSpeakingDeadline: true,
      );
    }
    if (completed >= _partEnd(IeltsSpeakingPart.part1)) {
      final phase = switch (value.phase) {
        IeltsMockPhase.part1Complete ||
        IeltsMockPhase.part2Intro ||
        IeltsMockPhase.part2CueCard ||
        IeltsMockPhase.part2Preparation ||
        IeltsMockPhase.part2Complete => value.phase,
        IeltsMockPhase.part2Speaking
            when _part2RetryNeeded ||
                widget.controller.errorMessage != null ||
                widget.controller.hasPendingPracticeAudio ||
                widget.controller.recordingState !=
                    PracticeRecordingState.idle =>
          IeltsMockPhase.part2Speaking,
        IeltsMockPhase.part2Speaking => IeltsMockPhase.part2Intro,
        _ => IeltsMockPhase.part1Complete,
      };
      return value.copyWith(
        phase: phase,
        clearSpeakingStartedAt: phase != IeltsMockPhase.part2Speaking,
        clearSpeakingDeadline: phase != IeltsMockPhase.part2Speaking,
      );
    }
    return value.copyWith(
      phase: IeltsMockPhase.part1,
      clearPreparationDeadline: true,
      clearSpeakingStartedAt: true,
      clearSpeakingDeadline: true,
    );
  }

  void _handleControllerState() {
    if (!mounted) {
      return;
    }
    if (widget.controller.practiceSessionId == null ||
        widget.controller.practiceExperience !=
            PracticeExperience.ieltsSpeaking ||
        widget.controller.ieltsAssignment == null) {
      _removeSpeechFeedbackSources(widget.speechFeedbackController);
      _progress = null;
      _loading = false;
      _observedMessageCount = 0;
      _observedCompletedTurns = 0;
      _preserveCompletedConversation = false;
      _showCompletionSheet = false;
      setState(() {});
      return;
    }
    final messageCount = widget.controller.practiceMessages.length;
    final shouldFollowConversation = messageCount != _observedMessageCount;
    _observedMessageCount = messageCount;
    final completedTurns = widget.controller.completedTurns;
    final turnLimit = widget.controller.turnLimit;
    final justCompletedSession =
        turnLimit > 0 &&
        _observedCompletedTurns < turnLimit &&
        completedTurns >= turnLimit;
    _observedCompletedTurns = completedTurns;
    if (justCompletedSession) {
      _preserveCompletedConversation = true;
      _showCompletionSheet = true;
    }
    if (_visibleTipQuestionId != widget.controller.currentQuestion?.id) {
      _visibleTipQuestionId = null;
    }
    _syncRecordingTimer();
    _syncSpeechFeedbackSources();
    _confirmPendingTranscript();
    if ((_progress?.phase == IeltsMockPhase.part2Speaking ||
            _progress?.phase == IeltsMockPhase.part2Complete ||
            _progress?.phase == IeltsMockPhase.part3) &&
        widget.controller.recordingState == PracticeRecordingState.idle &&
        widget.controller.hasPendingPracticeAudio &&
        widget.controller.errorMessage != null &&
        !_finishingPart2Recording) {
      _part2RetryNeeded = true;
      _schedulePart2TranscriptionRetry();
    }

    final progress = _progress;
    if (progress != null) {
      final reconciled = _reconcileProgress(progress);
      if (!_sameProgress(progress, reconciled)) {
        _progress = reconciled;
        unawaited(_progressStore.write(reconciled));
        _syncTicker();
      }
    }
    _recordCompletedParts();
    _syncCompletionReport();
    setState(() {});
    if (shouldFollowConversation) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _scrollToLatestMessage();
      });
    }
    _scheduleQuestionNarration();
  }

  void _jumpToLatestMessage() {
    if (!mounted || !_conversationScrollController.hasClients) {
      return;
    }
    _conversationScrollController.jumpTo(
      _conversationScrollController.position.maxScrollExtent,
    );
  }

  void _scrollToLatestMessage() {
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
  }

  void _syncSpeechFeedbackSources() {
    final feedbackController = widget.speechFeedbackController;
    if (feedbackController == null) {
      _feedbackSources.clear();
      return;
    }
    final current = <String, String>{};
    for (final message in widget.controller.practiceMessages) {
      final statusUrl = message.speechFeedbackStatusUrl;
      if (statusUrl != null) {
        current[_ieltsFeedbackSourceKey(widget.controller, message)] =
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

  void _handleSpeechFeedbackState() {
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

  void _scheduleQuestionNarration() {
    final phase = _progress?.phase;
    if (phase != IeltsMockPhase.part1 && phase != IeltsMockPhase.part3) {
      return;
    }
    final questionId = widget.controller.currentQuestion?.id;
    final questionText = widget.controller.currentQuestion?.text;
    if (questionId == null ||
        questionText == null ||
        _autoNarratedQuestionId == questionId) {
      return;
    }
    if (phase == IeltsMockPhase.part3 &&
        _autoNarratedQuestionText == questionText) {
      _autoNarratedQuestionId = questionId;
      return;
    }
    _autoNarratedQuestionId = questionId;
    _autoNarratedQuestionText = questionText;
    if (widget.controller.currentQuestion?.speechPath != null &&
        widget.onReplayQuestionWithAvatar != null) {
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _progress?.phase != phase) {
        return;
      }
      unawaited(_playQuestionNarration(questionId, questionText));
    });
  }

  Future<void> _playQuestionNarration(String questionId, String text) async {
    final currentQuestion = widget.controller.currentQuestion;
    final replayWithAvatar = widget.onReplayQuestionWithAvatar;
    if (currentQuestion?.id == questionId &&
        currentQuestion?.speechPath != null &&
        replayWithAvatar != null) {
      await replayWithAvatar();
      return;
    }
    if (_progress?.phase == IeltsMockPhase.part1 &&
        currentQuestion?.id == questionId &&
        widget.controller.canUsePracticeAudio) {
      await _stopExaminerSpeakerSafely();
      _questionNarrationGeneration++;
      _playingQuestionId = null;
      _questionNarrationErrorId = null;
      await widget.controller.toggleQuestionAudio();
      return;
    }
    if (_playingQuestionId == questionId) {
      await _stopQuestionNarration();
      return;
    }
    final generation = ++_questionNarrationGeneration;
    await _stopExaminerSpeakerSafely();
    if (!mounted || generation != _questionNarrationGeneration) {
      return;
    }
    setState(() {
      _playingQuestionId = questionId;
      _questionNarrationErrorId = null;
    });
    try {
      final speaker = _examinerSpeaker;
      if (speaker is CoachingSpeechPlayer) {
        await speaker.speakQuestion(questionId: questionId, fallbackText: text);
      } else {
        await speaker.speak(text);
      }
    } catch (_) {
      if (mounted && generation == _questionNarrationGeneration) {
        setState(() => _questionNarrationErrorId = questionId);
      }
    } finally {
      if (mounted && generation == _questionNarrationGeneration) {
        setState(() => _playingQuestionId = null);
      }
    }
  }

  Future<void> _stopQuestionNarration() async {
    _questionNarrationGeneration++;
    await widget.controller.stopPracticeAudio();
    await _stopExaminerSpeakerSafely();
    if (mounted && _playingQuestionId != null) {
      setState(() => _playingQuestionId = null);
    }
  }

  Future<void> _stopExaminerSpeakerSafely() async {
    try {
      await _examinerSpeaker.stop();
    } catch (_) {
      // Playback is best-effort; recording and navigation must stay usable.
    }
  }

  Future<String> _translateQuestion(PracticeMessage message) async {
    final cached = _questionTranslations[message.id];
    if (cached != null) {
      return cached;
    }
    final client = widget.controller.client;
    if (client is! PracticeQuestionTranslationClient) {
      throw StateError('Question translation is unavailable.');
    }
    final translationClient = client as PracticeQuestionTranslationClient;
    final translation = await translationClient.translateQuestion(
      questionId: message.id,
    );
    if (translation.questionId != message.id) {
      throw StateError('Question translation does not match the message.');
    }
    _questionTranslations[message.id] = translation.content;
    return translation.content;
  }

  Future<void> _showQuestionTip() async {
    final tip = await widget.controller.requestQuestionTip();
    if (!mounted ||
        tip == null ||
        widget.controller.currentQuestion?.id != tip.questionId) {
      return;
    }
    setState(() => _visibleTipQuestionId = tip.questionId);
  }

  Future<void> _speakQuestionTip() async {
    final tip = widget.controller.questionTip;
    if (tip == null || _visibleTipQuestionId != tip.questionId) {
      return;
    }
    await _examinerSpeaker.speak(tip.content);
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
      await _examinerSpeaker.stop();
    } on Object {
      // Recording must remain usable if platform TTS cannot stop cleanly.
    }
  }

  void _syncRecordingTimer() {
    final recording =
        widget.controller.recordingState == PracticeRecordingState.recording;
    if (recording) {
      if (_recordingTicker != null) {
        return;
      }
      _recordingSeconds = 0;
      _recordingTicker = Timer.periodic(const Duration(seconds: 1), (timer) {
        if (!mounted) {
          return;
        }
        setState(() => _recordingSeconds = timer.tick);
      });
      return;
    }
    _recordingTicker?.cancel();
    _recordingTicker = null;
    _recordingSeconds = 0;
  }

  void _recordCompletedParts() {
    final sessionId = widget.controller.practiceSessionId;
    final history = widget.ieltsController;
    if (sessionId == null || history == null) {
      return;
    }
    final completed = widget.controller.completedTurns;
    void complete(PracticeMode mode) {
      if (_recordedCompletions.add(mode)) {
        unawaited(history.markPartCompleted(sessionId, mode));
      }
    }

    switch (_mode) {
      case PracticeMode.fullMock:
        if (completed >= _partEnd(IeltsSpeakingPart.part1)) {
          complete(PracticeMode.part1);
        }
        if (completed >= _partEnd(IeltsSpeakingPart.part2)) {
          complete(PracticeMode.part2);
        }
        if (completed >= widget.controller.turnLimit) {
          complete(PracticeMode.part3);
        }
      case PracticeMode.part1:
        if (completed >= _partEnd(IeltsSpeakingPart.part1)) {
          complete(PracticeMode.part1);
        }
      case PracticeMode.part2:
        if (completed >= _partEnd(IeltsSpeakingPart.part2)) {
          complete(PracticeMode.part2);
        }
      case PracticeMode.part3:
        if (completed >= widget.controller.turnLimit) {
          complete(PracticeMode.part3);
        }
      case PracticeMode.fullSimulation || PracticeMode.focus:
        throw StateError('Non-IELTS mode in IELTS practice.');
    }
  }

  void _confirmPendingTranscript() {
    if (!mounted ||
        _confirming ||
        _conversionRequested ||
        _convertedAnswerMode ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final transcript = widget.controller.transcript?.trim() ?? '';
    if (_requiresEnglishRetry(transcript)) {
      _rejectNonEnglishAnswer();
      return;
    }
    if (_answerLanguageError != null) {
      setState(() => _answerLanguageError = null);
    }
    _confirming = true;
    unawaited(
      widget.controller.confirmTranscript().whenComplete(() {
        if (mounted) {
          setState(() => _confirming = false);
        }
      }),
    );
  }

  void _rejectNonEnglishAnswer() {
    if (_confirming) {
      return;
    }
    _confirming = true;
    final rejectingPart2 =
        widget.controller.currentQuestion?.id != null &&
        widget.controller.currentQuestion?.id == _part2QuestionId;
    widget.controller.rerecord();
    _confirming = false;
    _answerLanguageError = '未检测到可评分的英文，请使用英文重新作答。';
    if (rejectingPart2) {
      _part2RetryNeeded = true;
      final progress = _progress;
      if (progress != null) {
        unawaited(
          _setProgress(
            progress.copyWith(
              phase: IeltsMockPhase.part2Speaking,
              clearPreparationDeadline: true,
              clearSpeakingStartedAt: true,
              clearSpeakingDeadline: true,
            ),
          ),
        );
      }
    }
    if (mounted && !_disposing) {
      setState(() {});
    }
  }

  void _saveNotes() {
    final progress = _progress;
    if (progress == null || _notesController.text == progress.notes) {
      return;
    }
    final updated = progress.copyWith(notes: _notesController.text);
    _progress = updated;
    unawaited(_progressStore.write(updated));
  }

  Future<void> _setProgress(IeltsMockProgress value) async {
    if (!mounted) {
      return;
    }
    setState(() {
      _progress = value;
      _now = widget.now().toUtc();
    });
    _syncTicker();
    await _progressStore.write(value);
    _scheduleQuestionNarration();
    _syncCompletionReport();
  }

  void _syncCompletionReport() {
    final reportController = widget.reportStatusController;
    final sessionId = widget.controller.practiceSessionId;
    if (_progress?.phase != IeltsMockPhase.complete ||
        reportController == null ||
        sessionId == null ||
        reportController.practiceSessionId == sessionId) {
      return;
    }
    unawaited(reportController.load(sessionId));
  }

  void _syncTicker() {
    final phase = _progress?.phase;
    final needsTicker =
        phase == IeltsMockPhase.part2Preparation ||
        phase == IeltsMockPhase.part2Speaking;
    if (!needsTicker) {
      _ticker?.cancel();
      _ticker = null;
      return;
    }
    if (_ticker != null) {
      return;
    }
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) {
        return;
      }
      setState(() => _now = widget.now().toUtc());
      _handleExpiredTimer();
    });
  }

  void _handleExpiredTimer() {
    final progress = _progress;
    if (progress == null) {
      return;
    }
    if (progress.phase == IeltsMockPhase.part2Preparation &&
        _secondsUntil(progress.preparationDeadline) <= 0) {
      unawaited(_startPart2Speaking());
      return;
    }
    if (progress.phase == IeltsMockPhase.part2Speaking &&
        _secondsUntil(progress.speakingDeadline) <= 0) {
      if (_part2DeadlineHandled) {
        return;
      }
      _part2DeadlineHandled = true;
      unawaited(_finishPart2Speaking());
    }
  }

  int _secondsUntil(DateTime? deadline) {
    if (deadline == null) {
      return 0;
    }
    return math.max(0, deadline.toUtc().difference(_now).inSeconds);
  }

  Future<void> _beginPart2Intro() async {
    final progress = _progress;
    if (progress == null) {
      return;
    }
    _part2QuestionId = widget.controller.currentQuestion?.id;
    await _stopQuestionNarration();
    await _setProgress(progress.copyWith(phase: IeltsMockPhase.part2Intro));
    await _narratePart2Intro();
  }

  Future<void> _beginPart2CueCard() async {
    final progress = _progress;
    if (progress == null || !_introNarrated || _narrationBusy) {
      return;
    }
    await _examinerSpeaker.stop();
    await _setProgress(
      progress.copyWith(
        phase: IeltsMockPhase.part2CueCard,
        clearPreparationDeadline: true,
      ),
    );
    await _narratePart2CueCard();
  }

  Future<void> _beginPart2Preparation() async {
    final progress = _progress;
    if (progress == null) {
      return;
    }
    final now = widget.now().toUtc();
    await _setProgress(
      progress.copyWith(
        phase: IeltsMockPhase.part2Preparation,
        preparationDeadline: now.add(const Duration(seconds: 60)),
        clearSpeakingStartedAt: true,
        clearSpeakingDeadline: true,
      ),
    );
  }

  Future<void> _resumePart2Narration(IeltsMockPhase phase) async {
    if (phase == IeltsMockPhase.part2Intro) {
      await _narratePart2Intro();
    } else if (phase == IeltsMockPhase.part2CueCard) {
      await _narratePart2CueCard();
    }
  }

  Future<void> _narratePart2Intro() async {
    if (_narrationBusy ||
        _progress?.phase != IeltsMockPhase.part2Intro ||
        _introNarrated) {
      return;
    }
    setState(() {
      _narrationBusy = true;
      _narrationError = null;
    });
    try {
      await _examinerSpeaker.speak(_part2IntroNarration);
      if (!mounted || _progress?.phase != IeltsMockPhase.part2Intro) {
        return;
      }
      setState(() => _introNarrated = true);
    } catch (_) {
      if (mounted && _progress?.phase == IeltsMockPhase.part2Intro) {
        setState(() => _narrationError = '考官说明播放失败，请重试。');
      }
    } finally {
      if (mounted) {
        setState(() => _narrationBusy = false);
      }
    }
  }

  Future<void> _narratePart2CueCard() async {
    if (_narrationBusy || _progress?.phase != IeltsMockPhase.part2CueCard) {
      return;
    }
    setState(() {
      _narrationBusy = true;
      _narrationError = null;
    });
    var completed = false;
    try {
      await _examinerSpeaker.speak(_currentQuestionText());
      completed = mounted && _progress?.phase == IeltsMockPhase.part2CueCard;
    } catch (_) {
      if (mounted && _progress?.phase == IeltsMockPhase.part2CueCard) {
        setState(() => _narrationError = 'Cue Card 播放失败，请重试。');
      }
    } finally {
      if (mounted) {
        setState(() => _narrationBusy = false);
      }
    }
    if (completed) {
      await _beginPart2Preparation();
    }
  }

  Future<void> _startPart2Speaking({bool restart = false}) async {
    final progress = _progress;
    if (progress == null ||
        _startingPart2Recording ||
        (!restart &&
            progress.phase == IeltsMockPhase.part2Speaking &&
            (_part2DeadlineHandled ||
                _secondsUntil(progress.speakingDeadline) <= 0)) ||
        (progress.phase == IeltsMockPhase.part2Speaking &&
            widget.controller.recordingState != PracticeRecordingState.idle)) {
      return;
    }
    if (widget.controller.hasPendingPracticeAudio) {
      if (!restart) {
        return;
      }
      await widget.controller.discardPendingPracticeAudio();
      if (widget.controller.hasPendingPracticeAudio) {
        return;
      }
    }
    final beforeUserTurn = widget.onBeforeUserTurn;
    if (beforeUserTurn != null) {
      await beforeUserTurn();
      if (!mounted) {
        return;
      }
    }
    _startingPart2Recording = true;
    _part2DeadlineHandled = false;
    _part2RetryNeeded = false;
    _answerLanguageError = null;
    _part2TranscriptionRetryTimer?.cancel();
    _part2TranscriptionRetryTimer = null;
    _part2TranscriptionRetryAttempts = 0;
    final now = widget.now().toUtc();
    final speaking = progress.copyWith(
      phase: IeltsMockPhase.part2Speaking,
      clearPreparationDeadline: true,
      speakingStartedAt: now,
      speakingDeadline: now.add(const Duration(seconds: 120)),
    );
    if (!_sameProgress(progress, speaking)) {
      await _setProgress(speaking);
    }
    await widget.controller.startRecording(
      limit: const Duration(seconds: 120),
      useRealtimeTranscription: false,
    );
    if (!mounted) {
      return;
    }
    _startingPart2Recording = false;
    if (widget.controller.recordingState == PracticeRecordingState.idle) {
      _part2RetryNeeded = true;
      setState(() {});
    } else {
      setState(() {});
    }
  }

  Future<void> _retryPart2Transcription() async {
    if (widget.controller.recordingState != PracticeRecordingState.idle ||
        !widget.controller.hasPendingPracticeAudio) {
      return;
    }
    _part2TranscriptionRetryTimer?.cancel();
    _part2TranscriptionRetryTimer = null;
    await widget.controller.retryPracticeTranscription();
  }

  void _retryPart2Confirmation() {
    if (widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation ||
        widget.controller.errorMessage == null) {
      return;
    }
    _confirmPendingTranscript();
  }

  Future<void> _rerecordPart2() async {
    final state = widget.controller.recordingState;
    if (state == PracticeRecordingState.awaitingConfirmation) {
      widget.controller.rerecord();
    } else if (state != PracticeRecordingState.idle) {
      return;
    }
    _part2TranscriptionRetryTimer?.cancel();
    _part2TranscriptionRetryTimer = null;
    if (widget.controller.hasPendingPracticeAudio) {
      await widget.controller.discardPendingPracticeAudio();
      if (widget.controller.hasPendingPracticeAudio) {
        return;
      }
    }
    await _startPart2Speaking(restart: true);
  }

  void _schedulePart2TranscriptionRetry() {
    if (!mounted ||
        _exitPromptOpen ||
        _exitInFlight ||
        _part2TranscriptionRetryTimer != null ||
        _part2TranscriptionRetryAttempts >= 3 ||
        widget.controller.recordingState != PracticeRecordingState.idle ||
        !widget.controller.hasPendingPracticeAudio) {
      return;
    }
    _part2TranscriptionRetryAttempts++;
    _part2TranscriptionRetryTimer = Timer(
      Duration(seconds: _part2TranscriptionRetryAttempts),
      () {
        _part2TranscriptionRetryTimer = null;
        if (!mounted ||
            widget.controller.recordingState != PracticeRecordingState.idle ||
            !widget.controller.hasPendingPracticeAudio) {
          return;
        }
        unawaited(widget.controller.retryPracticeTranscription());
      },
    );
  }

  Future<void> _finishPart2Speaking() async {
    if (_finishingPart2Recording) {
      return;
    }
    _finishingPart2Recording = true;
    final progress = _progress;
    if (progress != null) {
      final speakingStartedAt = progress.speakingStartedAt;
      final spoken = speakingStartedAt == null
          ? progress.part2SpokenSeconds
          : widget
                .now()
                .toUtc()
                .difference(speakingStartedAt)
                .inSeconds
                .clamp(0, 120);
      await _setProgress(
        progress.copyWith(
          phase: IeltsMockPhase.part2Complete,
          part2SpokenSeconds: spoken,
          clearPreparationDeadline: true,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        ),
      );
    }
    final state = widget.controller.recordingState;
    if (state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording) {
      await widget.controller.finishRecordingGesture();
      if (widget.controller.hasPendingPracticeAudio) {
        _part2RetryNeeded = true;
        _schedulePart2TranscriptionRetry();
      }
    } else if (state == PracticeRecordingState.awaitingConfirmation) {
      _confirmPendingTranscript();
    }
    if (mounted) {
      setState(() => _finishingPart2Recording = false);
    }
  }

  Future<void> _continueFromPart2() async {
    final progress = _progress;
    if (progress == null ||
        progress.phase != IeltsMockPhase.part2Complete ||
        !_part2TurnConfirmed ||
        _enteringPart3) {
      return;
    }
    await _beginPart3();
  }

  Future<void> _startShortRecording() async {
    _conversionRequested = false;
    await _stopQuestionTipSpeech();
    if (_answerLanguageError != null) {
      setState(() => _answerLanguageError = null);
    }
    await _stopQuestionNarration();
    final beforeUserTurn = widget.onBeforeUserTurn;
    await beforeUserTurn?.call();
    if (!mounted) {
      return;
    }
    await widget.controller.startRecording();
  }

  Future<void> _sendShortVoice() async {
    _conversionRequested = false;
    await widget.controller.finishRecordingGesture();
    if (widget.controller.hasPendingPracticeAudio) {
      await widget.controller.discardPendingPracticeAudio();
      return;
    }
    _confirmPendingTranscript();
  }

  Future<void> _convertShortVoice() async {
    _conversionRequested = true;
    await widget.controller.finishRecordingGesture();
    if (!mounted) {
      return;
    }
    if (widget.controller.recordingState !=
        PracticeRecordingState.awaitingConfirmation) {
      setState(() => _conversionRequested = false);
      return;
    }
    final transcript = widget.controller.transcript?.trim() ?? '';
    if (_requiresEnglishRetry(transcript)) {
      _conversionRequested = false;
      _rejectNonEnglishAnswer();
      return;
    }
    widget.controller.rerecord();
    _convertedAnswerController.value = TextEditingValue(
      text: transcript,
      selection: TextSelection.collapsed(offset: transcript.length),
    );
    setState(() {
      _conversionRequested = false;
      _convertedAnswerMode = true;
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _convertedAnswerFocusNode.requestFocus();
      }
    });
  }

  Future<void> _cancelShortVoice() async {
    _conversionRequested = false;
    await widget.controller.cancelRecording();
  }

  Future<void> _submitConvertedAnswer() async {
    final text = _convertedAnswerController.text.trim();
    if (text.isEmpty || _convertedAnswerSubmitting) {
      return;
    }
    if (_requiresEnglishRetry(text)) {
      setState(() {
        _answerLanguageError = '未检测到可评分的英文，请使用英文重新作答。';
      });
      return;
    }
    final beforeUserTurn = widget.onBeforeUserTurn;
    if (beforeUserTurn != null) {
      await beforeUserTurn();
      if (!mounted) {
        return;
      }
    }
    setState(() => _convertedAnswerSubmitting = true);
    final submitted = await widget.controller.submitPracticeText(text);
    if (!mounted) {
      return;
    }
    setState(() {
      _convertedAnswerSubmitting = false;
      if (submitted) {
        _answerLanguageError = null;
        _convertedAnswerMode = false;
        _convertedAnswerController.clear();
        _convertedAnswerFocusNode.unfocus();
      }
    });
  }

  void _cancelConvertedAnswer() {
    _convertedAnswerController.clear();
    _convertedAnswerFocusNode.unfocus();
    setState(() => _convertedAnswerMode = false);
  }

  void _openTextAnswer() {
    unawaited(widget.onBeforeUserTurn?.call());
    setState(() {
      _answerLanguageError = null;
      _convertedAnswerMode = true;
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _convertedAnswerFocusNode.requestFocus();
      }
    });
  }

  Future<void> _beginPart3() async {
    final progress = _progress;
    if (progress == null || _enteringPart3) {
      return;
    }
    _enteringPart3 = true;
    final sessionId = widget.controller.practiceSessionId;
    try {
      if (sessionId != null) {
        await widget.ieltsController?.markPartStarted(
          sessionId,
          PracticeMode.part3,
        );
      }
      if (!mounted) {
        return;
      }
      await _setProgress(progress.copyWith(phase: IeltsMockPhase.part3));
    } finally {
      _enteringPart3 = false;
    }
  }

  Future<void> _requestExit({
    IeltsPracticeRouteResult? result,
    bool fromCompletion = false,
  }) async {
    if (_exitApproved || _exitPromptOpen || _exitInFlight || !mounted) {
      return;
    }
    final exitMode = _mode;
    _exitPromptOpen = true;
    _part2TranscriptionRetryTimer?.cancel();
    _part2TranscriptionRetryTimer = null;
    final shouldExit =
        fromCompletion ||
        _progress?.phase == IeltsMockPhase.complete ||
        await showModalBottomSheet<bool>(
              context: context,
              useSafeArea: true,
              backgroundColor: Colors.transparent,
              barrierColor: const Color(0x66000000),
              builder: (sheetContext) => _MockExitSheet(
                onContinue: () => Navigator.pop(sheetContext, false),
                onSaveAndExit: () => Navigator.pop(sheetContext, true),
              ),
            ) ==
            true;
    _exitPromptOpen = false;
    if (!shouldExit || !mounted) {
      _schedulePart2TranscriptionRetry();
      return;
    }
    setState(() => _exitInFlight = true);
    await widget.controller.abandonPendingTranscriptionForExit();
    if (!mounted) {
      return;
    }
    final callback = widget.onExitRequested;
    var parked = callback == null;
    try {
      if (callback != null) {
        parked = await callback();
      }
    } catch (_) {
      parked = false;
    }
    if (!mounted) {
      return;
    }
    setState(() => _exitInFlight = false);
    if (!parked) {
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(
          const SnackBar(content: Text('Progress is still saving. Try again.')),
        );
      return;
    }
    if (_progress?.phase == IeltsMockPhase.complete) {
      final sessionId = widget.controller.practiceSessionId;
      if (sessionId != null) {
        await _progressStore.delete(sessionId);
      }
    }
    if (!mounted) {
      return;
    }
    final shouldReturnToIeltsHub =
        result == null || result.action == IeltsPracticeCompletionAction.list;
    if (shouldReturnToIeltsHub) {
      widget.ieltsController?.requestNavigation(
        IeltsPracticeNavigationRequest(mode: result?.mode ?? exitMode),
      );
    }
    _exitApproved = true;
    setState(() {});
    await WidgetsBinding.instance.endOfFrame;
    if (mounted) {
      if (result == null) {
        await Navigator.of(context).maybePop();
      } else {
        Navigator.of(context).pop(result);
      }
    }
  }

  Future<void> _finishSection(IeltsPracticeCompletionAction action) async {
    final selection = _selection;
    final history = widget.ieltsController;
    if (selection == null || history == null) {
      await _requestExit(fromCompletion: true);
      return;
    }
    final listMode = _mode == PracticeMode.fullMock
        ? PracticeMode.part1
        : _mode;
    IeltsPracticeSelection? target;
    if (action == IeltsPracticeCompletionAction.retry) {
      target = selection;
    } else if (action == IeltsPracticeCompletionAction.next) {
      target = history.nextUnfinishedSelection(
        listMode,
        afterId: listMode == PracticeMode.part1
            ? selection.part1SetId
            : selection.topicGroupId,
      );
    }
    await _requestExit(
      fromCompletion: true,
      result: IeltsPracticeRouteResult(
        mode: listMode,
        action: action,
        selection: target,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final progress = _progress;
    if (_loading || progress == null) {
      return const Scaffold(
        key: Key('ielts-mock-loading'),
        body: Center(child: CircularProgressIndicator()),
      );
    }
    final complete = progress.phase == IeltsMockPhase.complete;
    final keepSectionCompletionConversation =
        _keepsSectionCompletionInConversation(progress);
    final keepCompletedConversation =
        _preserveCompletedConversation || keepSectionCompletionConversation;
    final showCompletionPage = complete && !keepCompletedConversation;
    final completionSheet = _completionSheet(progress);
    final stagePhase = complete && keepCompletedConversation
        ? switch (_mode) {
            PracticeMode.part1 => IeltsMockPhase.part1,
            PracticeMode.part2 => IeltsMockPhase.part2Speaking,
            PracticeMode.part3 => IeltsMockPhase.part3,
            _ => progress.phase,
          }
        : _mode == PracticeMode.fullMock &&
              progress.phase == IeltsMockPhase.part1Complete
        ? IeltsMockPhase.part1
        : progress.phase;
    return PopScope<Object?>(
      canPop: _exitApproved,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          unawaited(_requestExit());
        }
      },
      child: Scaffold(
        key: const Key('ielts-mock-page'),
        backgroundColor: SpeakUpDesign.surface,
        appBar: showCompletionPage ? _buildAppBar(progress.phase) : null,
        body: SafeArea(
          top: !showCompletionPage,
          child: showCompletionPage
              ? _buildAnimatedPhase(progress)
              : PracticeStageLayout(
                  stageRegionKey: const Key('ielts-avatar-region'),
                  portraitStageFraction: switch (stagePhase) {
                    IeltsMockPhase.part2Intro ||
                    IeltsMockPhase.part2CueCard ||
                    IeltsMockPhase.part2Preparation ||
                    IeltsMockPhase.part2Speaking => 0.40,
                    _ => 0.34,
                  },
                  stage: PracticeRoleStage(
                    title: _stageTitle(stagePhase),
                    fallback: const PracticeRoleFallback(
                      semanticLabel: 'IELTS 考官静态画面',
                      assetName: 'assets/images/scenes/ielts-examiner.jpg',
                      alignment: Alignment(0, -0.32),
                      imageKey: Key('ielts-avatar-placeholder'),
                    ),
                    surfaceBuilder: widget.avatarSurfaceBuilder,
                    statusLabel: widget.avatarStatusLabel,
                    exitInFlight: _exitInFlight,
                    exitButtonKey: const Key('ielts-mock-exit'),
                    onExit: _requestExit,
                  ),
                  content: Stack(
                    children: [
                      Column(
                        children: [
                          Expanded(child: _buildAnimatedPhase(progress)),
                          if (_usesShortAnswerRecorder(progress.phase))
                            _buildRecorderDock(),
                        ],
                      ),
                      if (completionSheet != null)
                        Positioned.fill(child: completionSheet),
                    ],
                  ),
                ),
        ),
      ),
    );
  }

  Widget _buildAnimatedPhase(IeltsMockProgress progress) {
    if (_mode == PracticeMode.fullMock &&
        progress.phase == IeltsMockPhase.part1Complete) {
      return _conversationPhase(
        key: const Key('ielts-mock-part-1'),
        partLabel: 'Part 1',
        completed: _part1Total,
        total: _part1Total,
        sectionStart: _partStart(IeltsSpeakingPart.part1),
        messages: _sectionMessages(
          _partStart(IeltsSpeakingPart.part1),
          _part1Total,
          includeCurrentQuestion: false,
        ),
      );
    }
    if (_mode == PracticeMode.fullMock &&
        progress.phase == IeltsMockPhase.part2Complete &&
        _part2TurnConfirmed) {
      return const SizedBox.expand(key: Key('ielts-mock-part-2-complete'));
    }
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 220),
      child:
          progress.phase == IeltsMockPhase.complete &&
              (_preserveCompletedConversation ||
                  _keepsSectionCompletionInConversation(progress))
          ? _completedConversationPhase()
          : _buildPhase(progress),
    );
  }

  String _stageTitle(IeltsMockPhase phase) => switch (phase) {
    IeltsMockPhase.part1 => 'IELTS · Part 1',
    IeltsMockPhase.part2Intro ||
    IeltsMockPhase.part2CueCard ||
    IeltsMockPhase.part2Preparation ||
    IeltsMockPhase.part2Speaking ||
    IeltsMockPhase.part2Complete => 'IELTS · Part 2',
    IeltsMockPhase.part3 => 'IELTS · Part 3',
    _ => 'IELTS Speaking',
  };

  Widget? _completionSheet(IeltsMockProgress progress) {
    if (_showCompletionSheet ||
        _keepsSectionCompletionInConversation(progress)) {
      return PracticeCompletionOverlay(
        keyPrefix: 'ielts-${_mode.name}-completion',
        sheetKey: const Key('ielts-section-completion-sheet'),
        primaryKey: const Key('ielts-section-review-action'),
        secondaryKey: const Key('ielts-section-list-action'),
        title: _completionTitle,
        message: '${widget.controller.completedTurns} 道回答已保存',
        primaryLabel: switch (_mode) {
          PracticeMode.fullMock => '查看报告状态',
          PracticeMode.part1 ||
          PracticeMode.part2 ||
          PracticeMode.part3 => '查看复盘报告',
          _ => '查看专项复盘',
        },
        secondaryLabel: _mode == PracticeMode.fullMock ? '返回训练' : '返回题单',
        onPrimary: _openCompletedReview,
        onSecondary: _leaveCompletedPractice,
      );
    }
    if (_mode != PracticeMode.fullMock) {
      return null;
    }
    if (progress.phase == IeltsMockPhase.part1Complete) {
      return PracticeCompletionOverlay(
        keyPrefix: 'ielts-part1-transition',
        sheetKey: const Key('ielts-section-completion-sheet'),
        primaryKey: const Key('ielts-section-review-action'),
        secondaryKey: const Key('ielts-section-list-action'),
        title: 'Part 1 已完成',
        message: '$_part1Total 道回答已保存',
        primaryLabel: '进入 Part 2',
        secondaryLabel: '保存并退出',
        onPrimary: () => unawaited(_beginPart2Intro()),
        onSecondary: () => unawaited(_requestExit(fromCompletion: true)),
      );
    }
    if (progress.phase == IeltsMockPhase.part2Complete && _part2TurnConfirmed) {
      return PracticeCompletionOverlay(
        keyPrefix: 'ielts-part2-transition',
        sheetKey: const Key('ielts-section-completion-sheet'),
        primaryKey: const Key('ielts-section-review-action'),
        secondaryKey: const Key('ielts-section-list-action'),
        title: 'Part 2 已完成',
        message: '$_part2Total 道回答已保存',
        primaryLabel: '继续 Part 3',
        secondaryLabel: '保存并退出',
        onPrimary: () => unawaited(_continueFromPart2()),
        onSecondary: () => unawaited(_requestExit(fromCompletion: true)),
      );
    }
    return null;
  }

  bool _keepsSectionCompletionInConversation(IeltsMockProgress progress) {
    return (_mode == PracticeMode.part1 ||
            _mode == PracticeMode.part2 ||
            _mode == PracticeMode.part3) &&
        progress.phase == IeltsMockPhase.complete;
  }

  String get _completionTitle => switch (_mode) {
    PracticeMode.part1 => 'Part 1 已完成',
    PracticeMode.part2 => 'Part 2 已完成',
    PracticeMode.part3 => 'Part 3 已完成',
    PracticeMode.fullMock => '口语模考已完成',
    PracticeMode.fullSimulation ||
    PracticeMode.focus => throw StateError('Non-IELTS mode in IELTS practice.'),
  };

  Widget _completedConversationPhase() {
    if (_mode == PracticeMode.part1) {
      return _conversationPhase(
        key: const Key('ielts-mock-part-1'),
        partLabel: 'Part 1',
        completed: _part1Total,
        total: _part1Total,
        sectionStart: _partStart(IeltsSpeakingPart.part1),
      );
    }
    if (_mode == PracticeMode.part2) {
      return _conversationPhase(
        key: const Key('ielts-mock-part-2'),
        partLabel: 'Part 2 · Long Turn',
        completed: _part2Total,
        total: _part2Total,
        sectionStart: _partStart(IeltsSpeakingPart.part2),
      );
    }
    return _conversationPhase(
      key: const Key('ielts-mock-part-3'),
      partLabel: 'Part 3 · Discussion',
      completed: _part3Total,
      total: _part3Total,
      sectionStart: _part3Start,
    );
  }

  void _openCompletedReview() {
    if (_mode == PracticeMode.part1 ||
        _mode == PracticeMode.part2 ||
        _mode == PracticeMode.part3) {
      unawaited(_openSectionReview());
      return;
    }
    setState(() {
      _showCompletionSheet = false;
      _preserveCompletedConversation = false;
    });
  }

  Future<void> _openSectionReview() async {
    if (_openingReadyReport) return;
    _openingReadyReport = true;
    try {
      await Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => _SectionReviewPage(
            controller: widget.reportStatusController,
            answerCount: widget.controller.completedTurns,
            title: switch (_mode) {
              PracticeMode.part1 => 'Part 1 专项复盘',
              PracticeMode.part2 => 'Part 2 专项复盘',
              PracticeMode.part3 => 'Part 3 专项复盘',
              _ => throw StateError(
                'Only standalone IELTS parts use the section review page.',
              ),
            },
          ),
        ),
      );
      if (mounted &&
          (widget.controller.practiceMode == PracticeMode.part1 ||
              widget.controller.practiceMode == PracticeMode.part2 ||
              widget.controller.practiceMode == PracticeMode.part3)) {
        await _finishSection(IeltsPracticeCompletionAction.list);
      }
    } finally {
      _openingReadyReport = false;
    }
  }

  Future<void> _openReadyReport() async {
    if (_openingReadyReport) return;
    final statusController = widget.reportStatusController;
    final sessionId = widget.controller.practiceSessionId;
    if (statusController == null || sessionId == null) return;
    _openingReadyReport = true;
    try {
      await statusController.load(sessionId);
      final report = statusController.evaluation?.report;
      if (!mounted || report == null) return;
      final item = ReviewHistoryItem(
        review: presentEvaluationReport(report),
        report: report,
        practiceSessionId: report.practiceSessionId,
        createdAt: report.createdAt,
        completedAt: report.createdAt,
      );
      await Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => ReviewReportDetailPage(item: item),
        ),
      );
      if (mounted && widget.controller.practiceMode == PracticeMode.part1) {
        await _finishSection(IeltsPracticeCompletionAction.list);
      }
    } finally {
      _openingReadyReport = false;
    }
  }

  void _leaveCompletedPractice() {
    if (_mode == PracticeMode.fullMock) {
      unawaited(_requestExit(fromCompletion: true));
      return;
    }
    unawaited(_finishSection(IeltsPracticeCompletionAction.list));
  }

  bool _usesShortAnswerRecorder(IeltsMockPhase phase) =>
      phase == IeltsMockPhase.part1 || phase == IeltsMockPhase.part3;

  Widget _buildRecorderDock() {
    return _RecorderDock(
      controller: widget.controller,
      recordingSeconds: _recordingSeconds,
      stateOverride: null,
      enabledOverride: null,
      allowTextAnswer: true,
      validationMessage: _answerLanguageError,
      convertedAnswerController: _convertedAnswerController,
      convertedAnswerFocusNode: _convertedAnswerFocusNode,
      convertedAnswerMode: _convertedAnswerMode,
      convertedAnswerSubmitting: _convertedAnswerSubmitting,
      onStart: _startShortRecording,
      onSendVoice: _sendShortVoice,
      onConvertToText: _convertShortVoice,
      onCancelRecording: _cancelShortVoice,
      onSubmitConvertedAnswer: _submitConvertedAnswer,
      onCancelConvertedAnswer: _cancelConvertedAnswer,
      onOpenTextAnswer: _openTextAnswer,
    );
  }

  PreferredSizeWidget? _buildAppBar(IeltsMockPhase phase) {
    final title = switch (phase) {
      IeltsMockPhase.part1 => 'IELTS · Part 1',
      IeltsMockPhase.part2Intro ||
      IeltsMockPhase.part2CueCard ||
      IeltsMockPhase.part2Preparation ||
      IeltsMockPhase.part2Speaking => 'IELTS · Part 2',
      IeltsMockPhase.part3 => 'IELTS · Part 3',
      IeltsMockPhase.complete =>
        _mode == PracticeMode.fullMock ? 'IELTS 口语报告' : '练习完成',
      _ => 'IELTS Speaking',
    };
    return AppBar(
      backgroundColor: SpeakUpDesign.surface,
      surfaceTintColor: Colors.transparent,
      centerTitle: true,
      leading: IconButton(
        key: const Key('ielts-mock-exit'),
        tooltip:
            phase == IeltsMockPhase.complete && _mode != PracticeMode.fullMock
            ? '返回题单'
            : '退出模考',
        onPressed: _exitInFlight ? null : _requestExit,
        icon: const Icon(Icons.chevron_left_rounded),
      ),
      title: Text(
        title,
        style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w800),
      ),
      bottom: const PreferredSize(
        preferredSize: Size.fromHeight(1),
        child: Divider(height: 1, color: SpeakUpDesign.border),
      ),
    );
  }

  Widget _buildPhase(IeltsMockProgress progress) {
    return switch (progress.phase) {
      IeltsMockPhase.part1 => _conversationPhase(
        key: const Key('ielts-mock-part-1'),
        partLabel: 'Part 1',
        completed: widget.controller.completedTurns.clamp(0, _part1Total),
        total: _part1Total,
        sectionStart: _partStart(IeltsSpeakingPart.part1),
      ),
      IeltsMockPhase.part1Complete => _CompletionStep(
        key: const Key('ielts-mock-part-1-complete'),
        title: 'Part 1 已完成',
        message: '接下来进入 Part 2。你将有 1 分钟准备时间。',
        buttonLabel: '进入 Part 2',
        onPressed: _beginPart2Intro,
      ),
      IeltsMockPhase.part2Intro => _Part2Intro(
        narrationBusy: _narrationBusy,
        narrationReady: _introNarrated,
        errorMessage: _narrationError,
        onRetry: _narratePart2Intro,
        onPressed: _beginPart2CueCard,
      ),
      IeltsMockPhase.part2CueCard => _Part2CueCardReading(
        question: _currentQuestionText(),
        narrationBusy: _narrationBusy,
        errorMessage: _narrationError,
        onRetry: _narratePart2CueCard,
      ),
      IeltsMockPhase.part2Preparation => _Part2LongTurn(
        speaking: false,
        secondsRemaining: _secondsUntil(progress.preparationDeadline),
        question: _currentQuestionText(),
        notesController: _notesController,
        notes: progress.notes,
        recordingState: widget.controller.recordingState,
        hasPendingAudio: widget.controller.hasPendingPracticeAudio,
        busy: _startingPart2Recording,
        errorMessage: null,
        onPressed: _startPart2Speaking,
        onRetryTranscription: _retryPart2Transcription,
        onRetryConfirmation: _retryPart2Confirmation,
        onRerecord: _rerecordPart2,
      ),
      IeltsMockPhase.part2Speaking => _Part2LongTurn(
        speaking: true,
        secondsRemaining: _secondsUntil(progress.speakingDeadline),
        question: _currentQuestionText(),
        notesController: _notesController,
        notes: progress.notes,
        recordingState: widget.controller.recordingState,
        hasPendingAudio: widget.controller.hasPendingPracticeAudio,
        busy:
            _finishingPart2Recording ||
            widget.controller.recordingState ==
                PracticeRecordingState.transcribing ||
            widget.controller.recordingState ==
                PracticeRecordingState.submitting,
        errorMessage:
            _answerLanguageError ??
            widget.controller.errorMessage ??
            (_part2RetryNeeded
                ? widget.controller.hasPendingPracticeAudio
                      ? '录音识别失败，录音已保留。'
                      : '上次录音未能保存，请重新录音。'
                : null),
        onPressed: _finishPart2Speaking,
        onRetryTranscription: _retryPart2Transcription,
        onRetryConfirmation: _retryPart2Confirmation,
        onRerecord: _rerecordPart2,
      ),
      IeltsMockPhase.part2Complete => _Part2Transition(
        key: const Key('ielts-mock-part-2-transition'),
        processing: _part2Processing,
        ready: _part2TurnConfirmed,
        errorMessage:
            _answerLanguageError ??
            widget.controller.errorMessage ??
            (_part2RetryNeeded && !_part2Processing
                ? 'Part 2 录音暂时无法识别，录音已保留。'
                : null),
        onContinue: _continueFromPart2,
        onReturn: () => _requestExit(fromCompletion: true),
        retryingConfirmation:
            widget.controller.recordingState ==
            PracticeRecordingState.awaitingConfirmation,
        onRetry:
            widget.controller.recordingState ==
                PracticeRecordingState.awaitingConfirmation
            ? _retryPart2Confirmation
            : _retryPart2Transcription,
        onRerecord: _rerecordPart2,
      ),
      IeltsMockPhase.part3 => _conversationPhase(
        key: const Key('ielts-mock-part-3'),
        partLabel: 'Part 3 · Discussion',
        completed: _part3CompletedTurns,
        total: _part3Total,
        sectionStart: _part3Start,
        messages: _part3ConversationMessages,
      ),
      IeltsMockPhase.complete =>
        _mode == PracticeMode.fullMock
            ? _MockComplete(
                progress: progress,
                totalQuestionCount: _assignment.turnBlueprints.length,
                part1AnswerCount: _part1Total,
                part3AnswerCount: _part3Total,
                report: widget.reportStatusController == null
                    ? _completedReport()
                    : null,
                reportStatusController: widget.reportStatusController,
                onOpenReport: _openReadyReport,
                onPressed: () => _requestExit(fromCompletion: true),
              )
            : _SectionPracticeComplete(
                mode: _mode,
                completedAnswerCount: widget.controller.completedTurns,
                reportStatusController: widget.reportStatusController,
                onOpenReport: _openReadyReport,
                onNext: () =>
                    _finishSection(IeltsPracticeCompletionAction.next),
                onRetry: () =>
                    _finishSection(IeltsPracticeCompletionAction.retry),
              ),
    };
  }

  Widget? _completedReport() {
    final builder = widget.completedReportBuilder;
    final sessionId = widget.controller.practiceSessionId;
    if (builder == null || sessionId == null) {
      return null;
    }
    return builder(context, sessionId);
  }

  int get _part3Start => _partStart(IeltsSpeakingPart.part3);

  int get _part3CompletedTurns =>
      (widget.controller.completedTurns - _part3Start).clamp(0, _part3Total);

  Widget _conversationPhase({
    required Key key,
    required String partLabel,
    required int completed,
    required int total,
    required int sectionStart,
    List<PracticeMessage>? messages,
  }) {
    return Column(
      key: key,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 0),
          child: _SectionProgress(
            label: partLabel,
            completed: completed,
            total: total,
          ),
        ),
        Expanded(
          child: _ExamConversation(
            messages: messages ?? _sectionMessages(sectionStart, completed),
            controller: widget.controller,
            scrollController: _conversationScrollController,
            bottomPadding: _completionOverlayVisible ? 292 : 28,
            speechFeedbackController: widget.speechFeedbackController,
            playingQuestionId: _playingQuestionId,
            narrationErrorQuestionId: _questionNarrationErrorId,
            mediaPlayingQuestionId: widget.controller.isQuestionAudioPlaying
                ? widget.controller.questionId
                : null,
            onPlayQuestion: _playQuestionNarration,
            onTranslateQuestion:
                widget.controller.canTranslateQuestion &&
                    widget.controller.client
                        is PracticeQuestionTranslationClient
                ? _translateQuestion
                : null,
            visibleTipQuestionId: _visibleTipQuestionId,
            onShowTip: _showQuestionTip,
            onHideTip: _hideQuestionTip,
            onSpeakTip: _speakQuestionTip,
          ),
        ),
        if (widget.controller.errorMessage case final error?)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
            child: Text(
              error,
              key: const Key('ielts-mock-error'),
              textAlign: TextAlign.center,
              style: const TextStyle(color: SpeakUpDesign.error),
            ),
          ),
      ],
    );
  }

  List<PracticeMessage> _sectionMessages(
    int sectionStart,
    int completed, {
    bool includeCurrentQuestion = true,
  }) {
    final relevantCount =
        completed * 2 +
        (includeCurrentQuestion && widget.controller.currentQuestion != null
            ? 1
            : 0);
    final currentQuestionId = widget.controller.currentQuestion?.id;
    final all = widget.controller.practiceMessages
        .where(
          (message) =>
              (message.role == PracticeMessageRole.assistant ||
                  message.role == PracticeMessageRole.user) &&
              (includeCurrentQuestion || message.id != currentQuestionId),
        )
        .toList(growable: false);
    if (all.length <= relevantCount) {
      return all;
    }
    return all.sublist(all.length - relevantCount);
  }

  List<PracticeMessage> get _part3ConversationMessages {
    if (_part2TurnConfirmed) {
      return _sectionMessages(_part3Start, _part3CompletedTurns);
    }
    final part3 = _assignment.part(IeltsSpeakingPart.part3)!;
    final text = part3.turnBlueprints.first;
    return <PracticeMessage>[
      PracticeMessage(
        id: 'ielts-part3-preview-${part3.sourceId}',
        role: PracticeMessageRole.assistant,
        text: text,
      ),
    ];
  }

  String _currentQuestionText() {
    for (final message in widget.controller.practiceMessages.reversed) {
      if (message.role == PracticeMessageRole.assistant) {
        return message.text;
      }
    }
    return 'Describe a skill you would like to learn.';
  }

  bool get _completionOverlayVisible {
    final progress = _progress;
    return _showCompletionSheet ||
        (_mode == PracticeMode.fullMock &&
            (progress?.phase == IeltsMockPhase.part1Complete ||
                (progress?.phase == IeltsMockPhase.part2Complete &&
                    _part2TurnConfirmed)));
  }
}
