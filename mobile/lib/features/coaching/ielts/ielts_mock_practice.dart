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
import 'package:speakup/features/coaching/practice/practice_message_bubble.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/practice/practice_stage.dart';
import 'package:speakup/features/coaching/practice/question_tip_sheet.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
import 'package:speakup/features/coaching/review/practice_report_status_controller.dart';
import 'package:speakup/features/coaching/review/practice_report_status_view.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';

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
  final PracticeReportStatusController? reportStatusController;
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
  Timer? _bufferedPart3RecordingLimitTimer;
  Future<void>? _bufferedPart3StartFuture;
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
  bool _exitInFlight = false;
  bool _narrationBusy = false;
  bool _introNarrated = false;
  String? _narrationError;
  String? _answerLanguageError;
  PracticeRecordingState _bufferedPart3RecordingState =
      PracticeRecordingState.idle;
  RecordedPracticeAudio? _bufferedPart3Audio;
  bool _flushingBufferedPart3Audio = false;
  final Map<String, String> _questionTranslations = <String, String>{};
  PracticePromptSpeaker? _ownedTipSpeaker;
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

  bool get _part2BackgroundProcessing {
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

  bool get _usesBufferedPart3Recorder =>
      _progress?.phase == IeltsMockPhase.part3 &&
      (!_part2TurnConfirmed ||
          _bufferedPart3RecordingState != PracticeRecordingState.idle ||
          _bufferedPart3Audio != null);

  Future<void> _startBufferedPart3Recording() {
    final existing = _bufferedPart3StartFuture;
    if (existing != null) {
      return existing;
    }
    final operation = _startBufferedPart3RecordingOperation();
    _bufferedPart3StartFuture = operation;
    return operation.whenComplete(() {
      if (identical(_bufferedPart3StartFuture, operation)) {
        _bufferedPart3StartFuture = null;
      }
    });
  }

  Future<void> _startBufferedPart3RecordingOperation() async {
    if (!_usesBufferedPart3Recorder ||
        _bufferedPart3RecordingState != PracticeRecordingState.idle ||
        _bufferedPart3Audio != null) {
      return;
    }
    setState(() {
      _answerLanguageError = null;
      _bufferedPart3RecordingState = PracticeRecordingState.starting;
    });
    await widget.controller.waitForPracticeRecorderRelease();
    if (!mounted || _progress?.phase != IeltsMockPhase.part3) {
      return;
    }
    if (_part2TurnConfirmed) {
      setState(() {
        _bufferedPart3RecordingState = PracticeRecordingState.idle;
      });
      await _startShortRecording();
      return;
    }
    await _stopQuestionTipSpeech();
    await _stopQuestionNarration();
    final beforeUserTurn = widget.onBeforeUserTurn;
    if (beforeUserTurn != null) {
      await beforeUserTurn();
    }
    if (!mounted || _progress?.phase != IeltsMockPhase.part3) {
      return;
    }
    try {
      await widget.controller.recorder.start();
      if (!mounted || _progress?.phase != IeltsMockPhase.part3) {
        await widget.controller.recorder.discardCurrent();
        return;
      }
      _bufferedPart3RecordingLimitTimer?.cancel();
      _bufferedPart3RecordingLimitTimer = Timer(
        const Duration(seconds: 120),
        () => unawaited(_stopBufferedPart3Recording()),
      );
      setState(() {
        _bufferedPart3RecordingState = PracticeRecordingState.recording;
      });
      _syncRecordingTimer();
    } on Object {
      if (mounted) {
        setState(() {
          _bufferedPart3RecordingState = PracticeRecordingState.idle;
          _answerLanguageError = '暂时无法开始录音，请重新尝试。';
        });
        _syncRecordingTimer();
      }
    }
  }

  Future<void> _stopBufferedPart3Recording() async {
    final start = _bufferedPart3StartFuture;
    if (start != null) {
      await start;
    }
    if (!mounted ||
        _bufferedPart3RecordingState != PracticeRecordingState.recording) {
      return;
    }
    _bufferedPart3RecordingLimitTimer?.cancel();
    _bufferedPart3RecordingLimitTimer = null;
    setState(() {
      _bufferedPart3RecordingState = PracticeRecordingState.transcribing;
    });
    _syncRecordingTimer();
    try {
      final audio = await widget.controller.recorder.stop();
      if (!mounted) {
        await widget.controller.recorder.discard(audio);
        return;
      }
      _bufferedPart3Audio = audio;
      _flushBufferedPart3Audio();
    } on Object {
      if (mounted) {
        setState(() {
          _bufferedPart3RecordingState = PracticeRecordingState.idle;
          _answerLanguageError = '录音保存失败，请重新录音。';
        });
        _syncRecordingTimer();
      }
    }
  }

  Future<void> _discardBufferedPart3Recording() async {
    _bufferedPart3RecordingLimitTimer?.cancel();
    _bufferedPart3RecordingLimitTimer = null;
    final start = _bufferedPart3StartFuture;
    if (start != null) {
      await start;
    }
    final state = _bufferedPart3RecordingState;
    final audio = _bufferedPart3Audio;
    _bufferedPart3Audio = null;
    _bufferedPart3RecordingState = PracticeRecordingState.idle;
    _syncRecordingTimer();
    try {
      if (state == PracticeRecordingState.starting ||
          state == PracticeRecordingState.recording) {
        await widget.controller.recorder.discardCurrent();
      }
      if (audio != null) {
        await widget.controller.recorder.discard(audio);
      }
    } on Object {
      // The recorder cleanup is best-effort; account cleanup remains the
      // durable privacy boundary.
    }
    if (mounted && !_disposing) {
      setState(() {});
    }
  }

  void _flushBufferedPart3Audio() {
    final audio = _bufferedPart3Audio;
    if (_flushingBufferedPart3Audio ||
        audio == null ||
        !_part2TurnConfirmed ||
        widget.controller.recordingState != PracticeRecordingState.idle) {
      return;
    }
    _flushingBufferedPart3Audio = true;
    final accepted = widget.controller.submitBufferedPracticeAudio(audio);
    if (accepted) {
      _bufferedPart3Audio = null;
      _bufferedPart3RecordingState = PracticeRecordingState.idle;
      _syncRecordingTimer();
    }
    _flushingBufferedPart3Audio = false;
    if (mounted) {
      setState(() {});
    }
  }

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
    _bufferedPart3RecordingLimitTimer?.cancel();
    unawaited(_discardBufferedPart3Recording());
    unawaited(_stopExaminerSpeakerSafely());
    if (_ownsExaminerSpeaker) {
      unawaited(_examinerSpeaker.dispose());
    }
    if (_ownedTipSpeaker case final speaker?) {
      unawaited(speaker.dispose());
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
    PracticeMode.part3 => IeltsMockPhase.part3Intro,
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
            : completed == 0 && value.phase == IeltsMockPhase.part3Intro
            ? IeltsMockPhase.part3Intro
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
        if (value.phase == IeltsMockPhase.part3 ||
            value.phase == IeltsMockPhase.part3Intro) {
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
        IeltsMockPhase.part2Speaking ||
        IeltsMockPhase.part2Complete ||
        IeltsMockPhase.part3Intro ||
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
        IeltsMockPhase.part3Intro => IeltsMockPhase.part3Intro,
        _ => IeltsMockPhase.part2Complete,
      };
      return value.copyWith(
        phase: phase,
        clearPreparationDeadline: true,
        clearSpeakingStartedAt: true,
        clearSpeakingDeadline: true,
      );
    }
    if (value.phase == IeltsMockPhase.part3 ||
        value.phase == IeltsMockPhase.part3Intro) {
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
        IeltsMockPhase.part3Intro => value.phase,
        IeltsMockPhase.part2Complete => IeltsMockPhase.part2Speaking,
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
            _progress?.phase == IeltsMockPhase.part3Intro ||
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
    _flushBufferedPart3Audio();
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
    final preview = phase == IeltsMockPhase.part3 && !_part2TurnConfirmed
        ? _part3ConversationMessages.firstOrNull
        : null;
    final questionId = preview?.id ?? widget.controller.currentQuestion?.id;
    final questionText =
        preview?.text ?? widget.controller.currentQuestion?.text;
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
    if (preview == null &&
        widget.controller.currentQuestion?.speechPath != null &&
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
      await _examinerSpeaker.speak(text);
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
      // Recording must remain usable if platform TTS cannot stop cleanly.
    }
  }

  void _syncRecordingTimer() {
    final recording =
        widget.controller.recordingState == PracticeRecordingState.recording ||
        _bufferedPart3RecordingState == PracticeRecordingState.recording;
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
        if (completed >= widget.controller.turnLimit) {
          complete(PracticeMode.part3);
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
      unawaited(_discardBufferedPart3Recording());
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
          phase: IeltsMockPhase.part2Speaking,
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

  Future<void> _beginStandalonePart3() => _beginPart3();

  Future<void> _requestExit({
    IeltsPracticeRouteResult? result,
    bool fromCompletion = false,
  }) async {
    if (_exitApproved || _exitInFlight || !mounted) {
      return;
    }
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
    if (!shouldExit || !mounted) {
      return;
    }
    setState(() => _exitInFlight = true);
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
    final selection = _selection;
    final shouldReturnToSectionList =
        selection != null &&
        _mode != PracticeMode.fullMock &&
        (result == null || result.action == IeltsPracticeCompletionAction.list);
    if (shouldReturnToSectionList) {
      widget.ieltsController?.requestNavigation(
        IeltsPracticeNavigationRequest(mode: _mode),
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
    final showCompletionPage = complete && !_preserveCompletedConversation;
    final completionSheet = _completionSheet(progress);
    final stagePhase = complete && _preserveCompletedConversation
        ? (_mode == PracticeMode.part1
              ? IeltsMockPhase.part1
              : IeltsMockPhase.part3)
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
                  avatarRegionKey: const Key('ielts-avatar-region'),
                  portraitAvatarFraction: switch (stagePhase) {
                    IeltsMockPhase.part2Intro ||
                    IeltsMockPhase.part2CueCard ||
                    IeltsMockPhase.part2Preparation ||
                    IeltsMockPhase.part2Speaking => 0.40,
                    _ => 0.34,
                  },
                  avatar: PracticeAvatarStage(
                    title: _stageTitle(stagePhase),
                    fallback: const PracticeAvatarFallback(
                      semanticLabel: 'IELTS 考官静态画面',
                      imageKey: Key('ielts-avatar-placeholder'),
                    ),
                    surfaceBuilder: widget.avatarSurfaceBuilder,
                    statusLabel: widget.avatarSurfaceBuilder == null
                        ? null
                        : widget.avatarStatusLabel,
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
                      if (completionSheet != null) ...[
                        const Positioned.fill(
                          child: ModalBarrier(
                            dismissible: false,
                            color: Color(0x14000000),
                            semanticsLabel: '练习完成',
                          ),
                        ),
                        Align(
                          alignment: Alignment.bottomCenter,
                          child: completionSheet,
                        ),
                      ],
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
              _preserveCompletedConversation
          ? _completedConversationPhase()
          : _buildPhase(progress),
    );
  }

  Widget? _completionSheet(IeltsMockProgress progress) {
    if (_showCompletionSheet) {
      return _SectionCompletionSheet(
        title: _completionTitle,
        message: '${widget.controller.completedTurns} 道回答已保存',
        primaryLabel: _mode == PracticeMode.fullMock ? '查看报告状态' : '查看专项复盘',
        secondaryLabel: _mode == PracticeMode.fullMock ? '返回训练' : '返回题单',
        onPrimary: _openCompletedReview,
        onSecondary: _leaveCompletedPractice,
      );
    }
    if (_mode != PracticeMode.fullMock) {
      return null;
    }
    if (progress.phase == IeltsMockPhase.part1Complete) {
      return _SectionCompletionSheet(
        title: 'Part 1 已完成',
        message: '$_part1Total 道回答已保存',
        primaryLabel: '进入 Part 2',
        secondaryLabel: '保存并退出',
        onPrimary: () => unawaited(_beginPart2Intro()),
        onSecondary: () => unawaited(_requestExit(fromCompletion: true)),
      );
    }
    if (progress.phase == IeltsMockPhase.part2Complete && _part2TurnConfirmed) {
      return _SectionCompletionSheet(
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

  String get _completionTitle => switch (_mode) {
    PracticeMode.part1 => 'Part 1 已完成',
    PracticeMode.part2 => 'Part 2 + Part 3 已完成',
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
    return _conversationPhase(
      key: const Key('ielts-mock-part-3'),
      partLabel: 'Part 3 · Discussion',
      completed: _part3Total,
      total: _part3Total,
      sectionStart: _part3Start,
    );
  }

  void _openCompletedReview() {
    setState(() {
      _showCompletionSheet = false;
      _preserveCompletedConversation = false;
    });
  }

  Future<void> _openReadyReport() async {
    final statusController = widget.reportStatusController;
    final sessionId = widget.controller.practiceSessionId;
    if (statusController == null || sessionId == null) return;
    final report = await statusController.loadReadyReport();
    if (!mounted || report == null) return;
    if (_mode == PracticeMode.fullMock) {
      try {
        final detail = decodeIeltsSpeakingReportDetail(report.detail);
        await Navigator.of(context).push<void>(
          MaterialPageRoute<void>(
            builder: (_) => _CompletedReportPage(
              title: evaluationReportTitle(report),
              child: IeltsSpeakingReadyReportView(report: detail),
            ),
          ),
        );
      } on IeltsSpeakingReportDecodeException {
        if (mounted) {
          ScaffoldMessenger.of(context)
            ..hideCurrentSnackBar()
            ..showSnackBar(const SnackBar(content: Text('报告内容暂时无法识别，请稍后重试。')));
        }
      }
      return;
    }
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
      stateOverride: _usesBufferedPart3Recorder
          ? _bufferedPart3RecordingState
          : null,
      enabledOverride: _usesBufferedPart3Recorder
          ? _bufferedPart3RecordingState == PracticeRecordingState.idle
          : null,
      allowTextAnswer: !_usesBufferedPart3Recorder,
      validationMessage: _answerLanguageError,
      convertedAnswerController: _convertedAnswerController,
      convertedAnswerFocusNode: _convertedAnswerFocusNode,
      convertedAnswerMode: _convertedAnswerMode,
      convertedAnswerSubmitting: _convertedAnswerSubmitting,
      onStart: _usesBufferedPart3Recorder
          ? _startBufferedPart3Recording
          : _startShortRecording,
      onSendVoice: _usesBufferedPart3Recorder
          ? _stopBufferedPart3Recording
          : _sendShortVoice,
      onConvertToText: _usesBufferedPart3Recorder
          ? _stopBufferedPart3Recording
          : _convertShortVoice,
      onCancelRecording: _usesBufferedPart3Recorder
          ? _discardBufferedPart3Recording
          : _cancelShortVoice,
      onSubmitConvertedAnswer: _submitConvertedAnswer,
      onCancelConvertedAnswer: _cancelConvertedAnswer,
      onOpenTextAnswer: _openTextAnswer,
    );
  }

  String _stageTitle(IeltsMockPhase phase) {
    return switch (phase) {
      IeltsMockPhase.part1 => 'IELTS · Part 1',
      IeltsMockPhase.part2Intro ||
      IeltsMockPhase.part2CueCard ||
      IeltsMockPhase.part2Preparation ||
      IeltsMockPhase.part2Speaking ||
      IeltsMockPhase.part2Complete => 'IELTS · Part 2',
      IeltsMockPhase.part3Intro || IeltsMockPhase.part3 => 'IELTS · Part 3',
      _ => 'IELTS Speaking',
    };
  }

  PreferredSizeWidget? _buildAppBar(IeltsMockPhase phase) {
    final title = switch (phase) {
      IeltsMockPhase.part1 => 'IELTS · Part 1',
      IeltsMockPhase.part2Intro ||
      IeltsMockPhase.part2CueCard ||
      IeltsMockPhase.part2Preparation ||
      IeltsMockPhase.part2Speaking => 'IELTS · Part 2',
      IeltsMockPhase.part3Intro || IeltsMockPhase.part3 => 'IELTS · Part 3',
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
        processing: _part2BackgroundProcessing,
        ready: _part2TurnConfirmed,
        errorMessage:
            _answerLanguageError ??
            widget.controller.errorMessage ??
            (_part2RetryNeeded && !_part2BackgroundProcessing
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
      IeltsMockPhase.part3Intro => _Part3Intro(
        topicTitle: _currentTopicTitle(),
        cueCardPrompt: _currentCueCard(),
        ready: _part2TurnConfirmed,
        onPressed: _beginStandalonePart3,
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
            playingQuestionId:
                widget.avatarReplayLoading || widget.avatarReplayPlaying
                ? widget.controller.questionId
                : _playingQuestionId,
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

  String _currentTopicTitle() =>
      _assignment.part(IeltsSpeakingPart.part2)?.topicTitle ??
      _assignment.part(IeltsSpeakingPart.part3)?.topicTitle ??
      (throw StateError('IELTS topic title is missing from the assignment.'));

  String? _currentCueCard() =>
      _assignment.part(IeltsSpeakingPart.part2)?.cueCard;

  bool get _completionOverlayVisible {
    final progress = _progress;
    return _showCompletionSheet ||
        (_mode == PracticeMode.fullMock &&
            (progress?.phase == IeltsMockPhase.part1Complete ||
                (progress?.phase == IeltsMockPhase.part2Complete &&
                    _part2TurnConfirmed)));
  }
}

class _SectionProgress extends StatelessWidget {
  const _SectionProgress({
    required this.label,
    required this.completed,
    required this.total,
  });

  final String label;
  final int completed;
  final int total;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Row(
          children: [
            Text(label, style: SpeakUpDesign.body),
            const Spacer(),
            Text('$completed/$total', style: SpeakUpDesign.body),
          ],
        ),
        const SizedBox(height: 10),
        ClipRRect(
          borderRadius: BorderRadius.circular(99),
          child: LinearProgressIndicator(
            minHeight: 5,
            value: completed / total,
            backgroundColor: SpeakUpDesign.surfaceMuted,
            color: const Color(0xFF5C97E5),
          ),
        ),
      ],
    );
  }
}

class _ExamConversation extends StatelessWidget {
  const _ExamConversation({
    required this.messages,
    required this.controller,
    required this.scrollController,
    required this.bottomPadding,
    required this.playingQuestionId,
    required this.narrationErrorQuestionId,
    required this.mediaPlayingQuestionId,
    required this.onPlayQuestion,
    required this.onTranslateQuestion,
    required this.visibleTipQuestionId,
    required this.onShowTip,
    required this.onHideTip,
    required this.onSpeakTip,
    this.speechFeedbackController,
  });

  final List<PracticeMessage> messages;
  final PracticeController controller;
  final ScrollController scrollController;
  final double bottomPadding;
  final String? playingQuestionId;
  final String? narrationErrorQuestionId;
  final String? mediaPlayingQuestionId;
  final Future<void> Function(String questionId, String text) onPlayQuestion;
  final Future<String> Function(PracticeMessage message)? onTranslateQuestion;
  final String? visibleTipQuestionId;
  final VoidCallback onShowTip;
  final VoidCallback onHideTip;
  final Future<void> Function() onSpeakTip;
  final SpeechFeedbackController? speechFeedbackController;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      key: const Key('ielts-mock-conversation'),
      controller: scrollController,
      padding: EdgeInsets.fromLTRB(20, 26, 20, bottomPadding),
      itemCount: messages.length,
      itemBuilder: (context, index) {
        final message = messages[index];
        final assistant = message.role == PracticeMessageRole.assistant;
        final candidateProjection =
            !assistant &&
                message.speechFeedbackStatusUrl != null &&
                speechFeedbackController != null
            ? speechFeedbackController!.projectionFor(
                _ieltsFeedbackSourceKey(controller, message),
              )
            : null;
        final projection =
            candidateProjection?.feedback?.scoreabilityStatus ==
                SpeechFeedbackScoreabilityStatus.insufficient
            ? null
            : candidateProjection;
        final currentQuestion = message.id == controller.questionId;
        final playing =
            playingQuestionId == message.id ||
            mediaPlayingQuestionId == message.id;
        final tipsAvailable =
            currentQuestion &&
            (controller.practiceCapabilities?.questionTipsAllowed ?? false);
        final tip = controller.questionTip;
        final showTip =
            assistant &&
            tip != null &&
            tip.questionId == message.id &&
            tip.questionId == visibleTipQuestionId;
        final tipError = currentQuestion
            ? controller.questionTipErrorMessage
            : null;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: PracticeMessageBubble(
                message: message,
                feedbackProjection: projection,
                onTranslate: assistant ? onTranslateQuestion : null,
                actions: assistant
                    ? _IeltsQuestionActions(
                        messageId: message.id,
                        playing: playing,
                        playbackFailed: narrationErrorQuestionId == message.id,
                        tipsAvailable: tipsAvailable,
                        tipLoading:
                            currentQuestion && controller.isQuestionTipLoading,
                        tipEnabled:
                            currentQuestion && controller.canRequestQuestionTip,
                        onPlay: () => onPlayQuestion(message.id, message.text),
                        onShowTip: onShowTip,
                      )
                    : null,
              ),
            ),
            if (showTip)
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: QuestionTipCard(
                  content: tip.content,
                  onClose: onHideTip,
                  onSpeak: onSpeakTip,
                ),
              ),
            if (assistant && tipError != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
                child: Text(
                  tipError,
                  key: const Key('ielts-question-tip-error'),
                  style: const TextStyle(
                    color: SpeakUpDesign.error,
                    fontSize: 12,
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}

class _IeltsQuestionActions extends StatelessWidget {
  const _IeltsQuestionActions({
    required this.messageId,
    required this.playing,
    required this.playbackFailed,
    required this.tipsAvailable,
    required this.tipLoading,
    required this.tipEnabled,
    required this.onPlay,
    required this.onShowTip,
  });

  final String messageId;
  final bool playing;
  final bool playbackFailed;
  final bool tipsAvailable;
  final bool tipLoading;
  final bool tipEnabled;
  final VoidCallback onPlay;
  final VoidCallback onShowTip;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        TextButton.icon(
          key: ValueKey('ielts-question-voice-$messageId'),
          onPressed: onPlay,
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
          icon: Icon(
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
            key: ValueKey('ielts-question-tip-$messageId'),
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

String _ieltsFeedbackSourceKey(
  PracticeController controller,
  PracticeMessage message,
) => 'practice:${controller.practiceSessionId}:${message.id}';

class _RecorderDock extends StatelessWidget {
  const _RecorderDock({
    required this.controller,
    required this.recordingSeconds,
    required this.stateOverride,
    required this.enabledOverride,
    required this.allowTextAnswer,
    required this.validationMessage,
    required this.convertedAnswerController,
    required this.convertedAnswerFocusNode,
    required this.convertedAnswerMode,
    required this.convertedAnswerSubmitting,
    required this.onStart,
    required this.onSendVoice,
    required this.onConvertToText,
    required this.onCancelRecording,
    required this.onSubmitConvertedAnswer,
    required this.onCancelConvertedAnswer,
    required this.onOpenTextAnswer,
  });

  final PracticeController controller;
  final int recordingSeconds;
  final PracticeRecordingState? stateOverride;
  final bool? enabledOverride;
  final bool allowTextAnswer;
  final String? validationMessage;
  final TextEditingController convertedAnswerController;
  final FocusNode convertedAnswerFocusNode;
  final bool convertedAnswerMode;
  final bool convertedAnswerSubmitting;
  final FutureOr<void> Function() onStart;
  final FutureOr<void> Function() onSendVoice;
  final FutureOr<void> Function() onConvertToText;
  final FutureOr<void> Function() onCancelRecording;
  final FutureOr<void> Function() onSubmitConvertedAnswer;
  final VoidCallback onCancelConvertedAnswer;
  final VoidCallback onOpenTextAnswer;

  @override
  Widget build(BuildContext context) {
    final state = stateOverride ?? controller.recordingState;
    final phase = switch (state) {
      PracticeRecordingState.idle ||
      PracticeRecordingState.completed => VoiceCapturePhase.idle,
      PracticeRecordingState.starting => VoiceCapturePhase.starting,
      PracticeRecordingState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    final working =
        state == PracticeRecordingState.transcribing ||
        state == PracticeRecordingState.awaitingConfirmation ||
        state == PracticeRecordingState.submitting;
    final control = VoiceCaptureControl(
      phase: phase,
      enabled:
          enabledOverride ??
          (!convertedAnswerMode &&
              !controller.hasPendingPracticeAudio &&
              !working),
      onStart: onStart,
      onSendVoice: onSendVoice,
      onConvertToText: onConvertToText,
      onCancel: onCancelRecording,
      upwardCancelOnly: true,
      builder: (context, capture) {
        final content = convertedAnswerMode
            ? _IeltsConvertedAnswerDock(
                controller: convertedAnswerController,
                focusNode: convertedAnswerFocusNode,
                submitting: convertedAnswerSubmitting,
                onSubmit: onSubmitConvertedAnswer,
                onCancel: onCancelConvertedAnswer,
              )
            : working
            ? _IeltsRecorderWorkingState(state: state)
            : _IeltsVoiceCaptureDock(
                phase: phase,
                capture: capture,
                transcript: controller.transcript ?? '',
                recordingSeconds: recordingSeconds,
                allowTextAnswer: allowTextAnswer,
                onShowText: onOpenTextAnswer,
              );
        return PracticeComposerSurface(child: content);
      },
    );
    final message = validationMessage;
    if (message == null) {
      return control;
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
          child: Text(
            message,
            key: const Key('ielts-answer-language-error'),
            textAlign: TextAlign.center,
            style: const TextStyle(color: SpeakUpDesign.error, fontSize: 13),
          ),
        ),
        control,
      ],
    );
  }
}

class _IeltsVoiceCaptureDock extends StatelessWidget {
  const _IeltsVoiceCaptureDock({
    required this.phase,
    required this.capture,
    required this.transcript,
    required this.recordingSeconds,
    required this.allowTextAnswer,
    required this.onShowText,
  });

  final VoiceCapturePhase phase;
  final VoiceCaptureView capture;
  final String transcript;
  final int recordingSeconds;
  final bool allowTextAnswer;
  final VoidCallback onShowText;

  @override
  Widget build(BuildContext context) {
    return VoiceComposerDock(
      capture: capture,
      phase: phase,
      elapsed: Duration(seconds: recordingSeconds),
      enabled: true,
      recordKey: const Key('ielts-mock-record'),
      stopRecordingKey: const Key('ielts-mock-stop-recording'),
      stateLabelKey: const Key('ielts-mock-voice-state-label'),
      durationKey: const Key('ielts-mock-voice-target-duration'),
      liveTranscript: transcript,
      liveTranscriptKey: const Key('ielts-mock-live-transcript'),
      upwardCancelOnly: true,
      showTextAction: allowTextAnswer,
      directTapToSend: !allowTextAnswer,
      showTextKey: const Key('ielts-mock-open-keyboard'),
      onShowText: onShowText,
    );
  }
}

class _IeltsRecorderWorkingState extends StatelessWidget {
  const _IeltsRecorderWorkingState({required this.state});

  final PracticeRecordingState state;

  @override
  Widget build(BuildContext context) {
    final label = switch (state) {
      PracticeRecordingState.transcribing => '正在识别你的回答…',
      PracticeRecordingState.awaitingConfirmation => '正在提交你的回答…',
      PracticeRecordingState.submitting => '回答已发送，正在进入下一题…',
      _ => '正在处理…',
    };
    return KeyedSubtree(
      key: const Key('ielts-mock-recorder-working'),
      child: PracticeLoadingComposer(label: label),
    );
  }
}

class _IeltsConvertedAnswerDock extends StatelessWidget {
  const _IeltsConvertedAnswerDock({
    required this.controller,
    required this.focusNode,
    required this.submitting,
    required this.onSubmit,
    required this.onCancel,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool submitting;
  final FutureOr<void> Function() onSubmit;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    return ConversationTextComposerDock(
      key: const Key('ielts-mock-converted-answer'),
      controller: controller,
      focusNode: focusNode,
      enabled: !submitting,
      canSubmit: !submitting,
      submitting: submitting,
      onReturn: onCancel,
      onSubmit: onSubmit,
      returnKey: const Key('ielts-mock-cancel-converted-answer'),
      fieldKey: const Key('ielts-mock-converted-answer-field'),
      submitKey: const Key('ielts-mock-submit-converted-answer'),
      returnTooltip: 'Cancel text draft',
      returnIcon: Icons.close_rounded,
      hintText: 'Edit the transcript before sending…',
      maxLines: 3,
      maxLength: 8000,
      textInputAction: TextInputAction.newline,
    );
  }
}

class _CompletionStep extends StatelessWidget {
  const _CompletionStep({
    required this.title,
    required this.message,
    required this.buttonLabel,
    required this.onPressed,
    this.buttonKey = const Key('ielts-mock-continue'),
    super.key,
  });

  final String title;
  final String message;
  final String buttonLabel;
  final VoidCallback? onPressed;
  final Key buttonKey;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(20),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 520),
          child: Column(
            children: [
              ClipOval(
                child: Image.asset(
                  'assets/images/scenes/ielts-complete-orb.png',
                  width: 80,
                  height: 80,
                  fit: BoxFit.cover,
                  filterQuality: FilterQuality.high,
                  semanticLabel: 'Section complete',
                ),
              ),
              const SizedBox(height: 24),
              Text(
                title,
                textAlign: TextAlign.center,
                style: SpeakUpDesign.pageTitle.copyWith(fontSize: 26),
              ),
              const SizedBox(height: 10),
              Text(
                message,
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body,
              ),
              const SizedBox(height: 36),
              FilledButton(
                key: buttonKey,
                onPressed: onPressed,
                style: FilledButton.styleFrom(
                  minimumSize: const Size.fromHeight(52),
                  backgroundColor: SpeakUpDesign.ink,
                  foregroundColor: Colors.white,
                ),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text(buttonLabel),
                    const SizedBox(width: 8),
                    const Icon(Icons.arrow_forward_rounded, size: 20),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SectionCompletionSheet extends StatelessWidget {
  const _SectionCompletionSheet({
    required this.title,
    required this.message,
    required this.primaryLabel,
    required this.secondaryLabel,
    required this.onPrimary,
    required this.onSecondary,
  });

  final String title;
  final String message;
  final String primaryLabel;
  final String secondaryLabel;
  final VoidCallback onPrimary;
  final VoidCallback onSecondary;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(10, 0, 10, 8),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: Material(
          key: const Key('ielts-section-completion-sheet'),
          color: SpeakUpDesign.surface,
          elevation: 12,
          shadowColor: const Color(0x26000000),
          borderRadius: BorderRadius.circular(28),
          clipBehavior: Clip.antiAlias,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Center(
                  child: Container(
                    width: 42,
                    height: 4,
                    decoration: BoxDecoration(
                      color: SpeakUpDesign.border,
                      borderRadius: BorderRadius.circular(99),
                    ),
                  ),
                ),
                const SizedBox(height: 18),
                Center(
                  child: Container(
                    width: 52,
                    height: 52,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      border: Border.all(color: SpeakUpDesign.ink, width: 2),
                    ),
                    child: const Icon(
                      Icons.check_rounded,
                      size: 34,
                      color: SpeakUpDesign.ink,
                    ),
                  ),
                ),
                const SizedBox(height: 16),
                Text(
                  title,
                  textAlign: TextAlign.center,
                  style: SpeakUpDesign.pageTitle.copyWith(fontSize: 24),
                ),
                const SizedBox(height: 6),
                Text(
                  message,
                  textAlign: TextAlign.center,
                  style: SpeakUpDesign.body.copyWith(
                    color: SpeakUpDesign.secondary,
                  ),
                ),
                const SizedBox(height: 20),
                FilledButton(
                  key: const Key('ielts-section-review-action'),
                  onPressed: onPrimary,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(54),
                    backgroundColor: SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(14),
                    ),
                  ),
                  child: Text(primaryLabel),
                ),
                const SizedBox(height: 4),
                TextButton(
                  key: const Key('ielts-section-list-action'),
                  onPressed: onSecondary,
                  style: TextButton.styleFrom(
                    foregroundColor: SpeakUpDesign.ink,
                    minimumSize: const Size.fromHeight(44),
                  ),
                  child: Text(secondaryLabel),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _MockExitSheet extends StatelessWidget {
  const _MockExitSheet({required this.onContinue, required this.onSaveAndExit});

  final VoidCallback onContinue;
  final VoidCallback onSaveAndExit;

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
        child: Material(
          key: const Key('ielts-mock-exit-sheet'),
          color: SpeakUpDesign.surface,
          borderRadius: BorderRadius.circular(28),
          clipBehavior: Clip.antiAlias,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(24, 28, 24, 22),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  '退出模拟考试？',
                  style: SpeakUpDesign.pageTitle.copyWith(fontSize: 26),
                ),
                const SizedBox(height: 24),
                FilledButton(
                  key: const Key('ielts-mock-continue-answering'),
                  onPressed: onContinue,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(54),
                    backgroundColor: const Color(0xFFF4F4F6),
                    foregroundColor: SpeakUpDesign.ink,
                    elevation: 0,
                  ),
                  child: const Text('继续答题'),
                ),
                const SizedBox(height: 12),
                FilledButton(
                  key: const Key('ielts-mock-save-and-exit'),
                  onPressed: onSaveAndExit,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(54),
                    backgroundColor: SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                  ),
                  child: const Text('保存并退出'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SectionPracticeComplete extends StatelessWidget {
  const _SectionPracticeComplete({
    required this.mode,
    required this.completedAnswerCount,
    required this.reportStatusController,
    required this.onOpenReport,
    required this.onNext,
    required this.onRetry,
  });

  final PracticeMode mode;
  final int completedAnswerCount;
  final PracticeReportStatusController? reportStatusController;
  final Future<void> Function() onOpenReport;
  final VoidCallback onNext;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final part = switch (mode) {
      PracticeMode.part1 => 'Part 1',
      PracticeMode.part2 => 'Part 2 + Part 3',
      PracticeMode.part3 => 'Part 3',
      PracticeMode.fullMock => 'IELTS Speaking',
      PracticeMode.fullSimulation || PracticeMode.focus => throw StateError(
        'Non-IELTS mode in IELTS practice.',
      ),
    };
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        key: Key('ielts-section-practice-complete-${mode.name}'),
        padding: const EdgeInsets.fromLTRB(20, 24, 20, 32),
        child: Center(
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxWidth: 520,
              minHeight: math.max(0, constraints.maxHeight - 56),
            ),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    _CompactCompletionHeader(
                      title: '$part 已完成',
                      message: '$completedAnswerCount 道回答已保存',
                    ),
                    const SizedBox(height: 24),
                    PracticeReportStatusCard(
                      controller: reportStatusController,
                      onOpenReport: onOpenReport,
                    ),
                  ],
                ),
                Padding(
                  padding: const EdgeInsets.only(top: 40),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text('继续练习', style: SpeakUpDesign.cardTitle),
                      const SizedBox(height: 12),
                      OutlinedButton(
                        key: const Key('ielts-section-next-action'),
                        onPressed: onNext,
                        style: OutlinedButton.styleFrom(
                          minimumSize: const Size.fromHeight(52),
                        ),
                        child: const Text('练习下一套'),
                      ),
                      const SizedBox(height: 4),
                      TextButton(
                        key: const Key('ielts-section-retry-action'),
                        onPressed: onRetry,
                        child: const Text('再练本套'),
                      ),
                    ],
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

class _Part2Transition extends StatelessWidget {
  const _Part2Transition({
    required this.processing,
    required this.ready,
    required this.errorMessage,
    required this.onContinue,
    required this.onReturn,
    required this.retryingConfirmation,
    required this.onRetry,
    required this.onRerecord,
    super.key,
  });

  final bool processing;
  final bool ready;
  final String? errorMessage;
  final VoidCallback onContinue;
  final VoidCallback onReturn;
  final bool retryingConfirmation;
  final VoidCallback onRetry;
  final VoidCallback onRerecord;

  @override
  Widget build(BuildContext context) {
    final failed = errorMessage != null && !processing && !ready;
    final color = failed
        ? SpeakUpDesign.error
        : ready
        ? SpeakUpDesign.success
        : const Color(0xFF276C82);
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(20),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 520),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Icon(
                failed
                    ? Icons.error_outline_rounded
                    : ready
                    ? Icons.check_circle_rounded
                    : Icons.cloud_upload_outlined,
                size: 88,
                color: color,
              ),
              const SizedBox(height: 24),
              Text(
                'Part 2 已完成',
                textAlign: TextAlign.center,
                style: SpeakUpDesign.pageTitle.copyWith(fontSize: 30),
              ),
              const SizedBox(height: 12),
              Text(
                failed
                    ? errorMessage!
                    : ready
                    ? '作答已经保存。请选择继续 Part 3，或返回训练。'
                    : '录音已保存，正在完成识别；成功后即可进入 Part 3。',
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body,
              ),
              const SizedBox(height: 28),
              if (failed)
                Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    FilledButton(
                      key: Key(
                        retryingConfirmation
                            ? 'ielts-part2-retry-confirmation'
                            : 'ielts-part2-retry-transcription',
                      ),
                      onPressed: onRetry,
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(54),
                        backgroundColor: SpeakUpDesign.ink,
                        foregroundColor: Colors.white,
                      ),
                      child: Text(retryingConfirmation ? '重试提交' : '重试识别'),
                    ),
                    const SizedBox(height: 12),
                    OutlinedButton(
                      key: const Key('ielts-part2-rerecord'),
                      onPressed: onRerecord,
                      style: OutlinedButton.styleFrom(
                        minimumSize: const Size.fromHeight(52),
                      ),
                      child: const Text('重新录音'),
                    ),
                  ],
                )
              else
                FilledButton(
                  key: const Key('ielts-part2-continue-part3'),
                  onPressed: ready ? onContinue : null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(54),
                    backgroundColor: SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                  ),
                  child: Text(ready ? '继续 Part 3 →' : '正在识别…'),
                ),
              const SizedBox(height: 12),
              OutlinedButton(
                key: const Key('ielts-part2-return-training'),
                onPressed: onReturn,
                style: OutlinedButton.styleFrom(
                  minimumSize: const Size.fromHeight(52),
                ),
                child: const Text('返回训练'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Part3Intro extends StatelessWidget {
  const _Part3Intro({
    required this.topicTitle,
    required this.cueCardPrompt,
    required this.ready,
    required this.onPressed,
  });

  final String topicTitle;
  final String? cueCardPrompt;
  final bool ready;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return _CompletionStep(
      key: const Key('ielts-part3-topic-intro'),
      title: ready ? 'Part 3 Ready' : '正在进入 Part 3',
      message:
          '${cueCardPrompt == null ? 'Discussion topic' : 'This discussion continues the Part 2 topic'}:\n'
          '$topicTitle'
          '${cueCardPrompt == null ? '' : '\n\n$cueCardPrompt'}'
          '${ready ? '' : '\n\nPart 2 正在后台确认，完成后会自动开始。'}',
      buttonLabel: ready ? 'Start Part 3' : '正在准备第一个问题…',
      buttonKey: const Key('ielts-part3-start'),
      onPressed: ready ? onPressed : null,
    );
  }
}

class _Part2Intro extends StatelessWidget {
  const _Part2Intro({
    required this.narrationBusy,
    required this.narrationReady,
    required this.errorMessage,
    required this.onRetry,
    required this.onPressed,
  });

  final bool narrationBusy;
  final bool narrationReady;
  final String? errorMessage;
  final VoidCallback onRetry;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('ielts-mock-part-2-intro'),
      padding: const EdgeInsets.fromLTRB(20, 22, 20, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Row(
            children: [
              Text('Part 2 · Long Turn', style: SpeakUpDesign.body),
              Spacer(),
              Text('3–4 min', style: SpeakUpDesign.body),
            ],
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Spacer(flex: 2),
                const Text(
                  'You will have 1 minute to prepare and up to 2 minutes to speak. You may take notes during preparation.',
                  textAlign: TextAlign.center,
                  style: SpeakUpDesign.body,
                ),
                const SizedBox(height: 28),
                FilledButton(
                  key: const Key('ielts-mock-part-2-start'),
                  onPressed: narrationReady ? onPressed : null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(58),
                    backgroundColor: SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                  ),
                  child: Text(
                    narrationBusy
                        ? 'Examiner is speaking…'
                        : 'I understand — Start →',
                  ),
                ),
                if (errorMessage != null) ...[
                  const SizedBox(height: 12),
                  Text(
                    errorMessage!,
                    key: const Key('ielts-part2-narration-error'),
                    textAlign: TextAlign.center,
                    style: SpeakUpDesign.meta.copyWith(
                      color: SpeakUpDesign.error,
                    ),
                  ),
                  TextButton(
                    key: const Key('ielts-part2-retry-narration'),
                    onPressed: narrationBusy ? null : onRetry,
                    child: const Text('Replay examiner'),
                  ),
                ],
                const Spacer(flex: 3),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Part2CueCardReading extends StatelessWidget {
  const _Part2CueCardReading({
    required this.question,
    required this.narrationBusy,
    required this.errorMessage,
    required this.onRetry,
  });

  final String question;
  final bool narrationBusy;
  final String? errorMessage;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return SizedBox.expand(
      key: const Key('ielts-mock-part-2-cue-card-reading'),
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 24, 20, 28),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              'Listen to the examiner',
              style: SpeakUpDesign.sectionTitle,
            ),
            const SizedBox(height: 16),
            _CueCard(question: question),
            const SizedBox(height: 16),
            if (narrationBusy)
              const Row(
                children: [
                  SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  SizedBox(width: 10),
                  Text('考官正在朗读 Cue Card…'),
                ],
              ),
            if (errorMessage != null) ...[
              Text(
                errorMessage!,
                key: const Key('ielts-part2-narration-error'),
                style: SpeakUpDesign.meta.copyWith(color: SpeakUpDesign.error),
              ),
              const SizedBox(height: 10),
              FilledButton(
                key: const Key('ielts-part2-retry-narration'),
                onPressed: narrationBusy ? null : onRetry,
                child: const Text('重新播放 Cue Card'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _Part2LongTurn extends StatelessWidget {
  const _Part2LongTurn({
    required this.speaking,
    required this.secondsRemaining,
    required this.question,
    required this.notesController,
    required this.notes,
    required this.recordingState,
    required this.hasPendingAudio,
    required this.busy,
    required this.errorMessage,
    required this.onPressed,
    required this.onRetryTranscription,
    required this.onRetryConfirmation,
    required this.onRerecord,
  });

  final bool speaking;
  final int secondsRemaining;
  final String question;
  final TextEditingController notesController;
  final String notes;
  final PracticeRecordingState recordingState;
  final bool hasPendingAudio;
  final bool busy;
  final String? errorMessage;
  final VoidCallback onPressed;
  final VoidCallback onRetryTranscription;
  final VoidCallback onRetryConfirmation;
  final VoidCallback onRerecord;

  @override
  Widget build(BuildContext context) {
    final minutes = secondsRemaining ~/ 60;
    final seconds = (secondsRemaining % 60).toString().padLeft(2, '0');
    return SizedBox.expand(
      key: const Key('ielts-mock-part-2-long-turn'),
      child: speaking
          ? Padding(
              padding: const EdgeInsets.fromLTRB(20, 24, 20, 28),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  _Part2RecordingStatus(
                    state: recordingState,
                    secondsRemaining: secondsRemaining,
                  ),
                  const SizedBox(height: 20),
                  const Divider(height: 1, color: SpeakUpDesign.border),
                  const SizedBox(height: 18),
                  const Text('我的小抄', style: SpeakUpDesign.meta),
                  const SizedBox(height: 8),
                  Expanded(
                    child: SingleChildScrollView(
                      child: Text(
                        notes.isEmpty ? '准备阶段未记录要点。' : notes,
                        style: notes.isEmpty
                            ? SpeakUpDesign.body
                            : const TextStyle(
                                color: SpeakUpDesign.ink,
                                fontSize: 18,
                                height: 1.5,
                              ),
                      ),
                    ),
                  ),
                  if (errorMessage != null) ...[
                    const SizedBox(height: 16),
                    Text(
                      errorMessage!,
                      textAlign: TextAlign.center,
                      style: const TextStyle(color: SpeakUpDesign.error),
                    ),
                  ],
                  if ((recordingState == PracticeRecordingState.idle ||
                          recordingState ==
                              PracticeRecordingState.awaitingConfirmation) &&
                      errorMessage != null) ...[
                    const SizedBox(height: 20),
                    FilledButton(
                      key: Key(
                        recordingState ==
                                PracticeRecordingState.awaitingConfirmation
                            ? 'ielts-part2-retry-confirmation'
                            : hasPendingAudio
                            ? 'ielts-part2-retry-transcription'
                            : 'ielts-part2-rerecord',
                      ),
                      onPressed: busy
                          ? null
                          : recordingState ==
                                PracticeRecordingState.awaitingConfirmation
                          ? onRetryConfirmation
                          : hasPendingAudio
                          ? onRetryTranscription
                          : onRerecord,
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(52),
                        backgroundColor: SpeakUpDesign.ink,
                        foregroundColor: Colors.white,
                      ),
                      child: Text(
                        recordingState ==
                                PracticeRecordingState.awaitingConfirmation
                            ? '重试提交'
                            : hasPendingAudio
                            ? '重试识别'
                            : '重新录音 →',
                      ),
                    ),
                    if (hasPendingAudio ||
                        recordingState ==
                            PracticeRecordingState.awaitingConfirmation) ...[
                      const SizedBox(height: 12),
                      OutlinedButton(
                        key: const Key('ielts-part2-rerecord'),
                        onPressed: busy ? null : onRerecord,
                        style: OutlinedButton.styleFrom(
                          minimumSize: const Size.fromHeight(52),
                        ),
                        child: const Text('重新录音'),
                      ),
                    ],
                  ],
                ],
              ),
            )
          : SingleChildScrollView(
              padding: const EdgeInsets.fromLTRB(20, 10, 20, 28),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    '$minutes:$seconds',
                    key: const Key('ielts-mock-preparation-countdown'),
                    textAlign: TextAlign.center,
                    style: SpeakUpDesign.pageTitle.copyWith(fontSize: 44),
                  ),
                  const SizedBox(height: 4),
                  const Text(
                    '准备时间 · 阅读题目并记录要点',
                    textAlign: TextAlign.center,
                    style: SpeakUpDesign.body,
                  ),
                  const SizedBox(height: 18),
                  _CueCard(question: question),
                  const SizedBox(height: 14),
                  TextField(
                    key: const Key('ielts-mock-notes'),
                    controller: notesController,
                    minLines: 2,
                    maxLines: 4,
                    maxLength: 4000,
                    decoration: const InputDecoration(
                      hintText: '在这里记录要点…',
                      counterText: '',
                      alignLabelWithHint: true,
                    ),
                  ),
                  const SizedBox(height: 18),
                  FilledButton(
                    key: const Key('ielts-mock-start-speaking'),
                    onPressed: busy ? null : onPressed,
                    style: FilledButton.styleFrom(
                      minimumSize: const Size.fromHeight(52),
                      backgroundColor: SpeakUpDesign.ink,
                      foregroundColor: Colors.white,
                    ),
                    child: const Text('提前开始作答 →'),
                  ),
                ],
              ),
            ),
    );
  }
}

class _Part2RecordingStatus extends StatelessWidget {
  const _Part2RecordingStatus({
    required this.state,
    required this.secondsRemaining,
  });

  final PracticeRecordingState state;
  final int secondsRemaining;

  @override
  Widget build(BuildContext context) {
    final recording =
        state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording;
    final minutes = (secondsRemaining.clamp(0, 120) ~/ 60).toString();
    final seconds = (secondsRemaining.clamp(0, 120) % 60).toString().padLeft(
      2,
      '0',
    );
    final label = switch (state) {
      PracticeRecordingState.starting => '正在打开麦克风…',
      PracticeRecordingState.recording => '录音中',
      PracticeRecordingState.transcribing => '正在识别你的作答…',
      PracticeRecordingState.awaitingConfirmation => '正在提交作答…',
      PracticeRecordingState.submitting => '正在进入下一环节…',
      _ => '等待录音',
    };
    final color = recording ? SpeakUpDesign.ink : SpeakUpDesign.secondary;

    return Row(
      key: const Key('ielts-part2-recording-status'),
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(Icons.graphic_eq_rounded, size: 26, color: color),
        const SizedBox(width: 10),
        Text(
          '$minutes:$seconds',
          key: const Key('ielts-mock-speaking-countdown'),
          style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 24),
        ),
        const SizedBox(width: 10),
        Flexible(
          child: Text(
            label,
            key: const Key('ielts-part2-recording-label'),
            style: SpeakUpDesign.body,
          ),
        ),
      ],
    );
  }
}

class _CueCard extends StatelessWidget {
  const _CueCard({required this.question});

  final String question;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('ielts-mock-cue-card'),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: SpeakUpDesign.ink, width: 2),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'CUE CARD',
            style: SpeakUpDesign.label.copyWith(
              color: SpeakUpDesign.tertiary,
              fontSize: 11,
              letterSpacing: 1.3,
            ),
          ),
          const SizedBox(height: 12),
          Text(
            question,
            style: SpeakUpDesign.body.copyWith(
              color: SpeakUpDesign.ink,
              fontSize: 13,
              height: 1.55,
            ),
          ),
        ],
      ),
    );
  }
}

class _MockComplete extends StatelessWidget {
  const _MockComplete({
    required this.progress,
    required this.totalQuestionCount,
    required this.part1AnswerCount,
    required this.part3AnswerCount,
    required this.onPressed,
    required this.reportStatusController,
    required this.onOpenReport,
    this.report,
  });

  final IeltsMockProgress progress;
  final int totalQuestionCount;
  final int part1AnswerCount;
  final int part3AnswerCount;
  final VoidCallback onPressed;
  final PracticeReportStatusController? reportStatusController;
  final Future<void> Function() onOpenReport;
  final Widget? report;

  @override
  Widget build(BuildContext context) {
    final elapsed = DateTime.now().toUtc().difference(progress.startedAt);
    final totalMinutes = math.max(1, elapsed.inMinutes);
    return SingleChildScrollView(
      key: const Key('ielts-mock-complete'),
      padding: const EdgeInsets.fromLTRB(20, 28, 20, 32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _CompactCompletionHeader(
            title: '模考已完成',
            message: '$totalQuestionCount 道回答已保存，报告将在后台生成。',
          ),
          const SizedBox(height: 20),
          PracticeReportStatusCard(
            controller: reportStatusController,
            onOpenReport: onOpenReport,
          ),
          if (report case final reportPanel?) ...[
            const SizedBox(height: 20),
            reportPanel,
          ],
          const SizedBox(height: 24),
          Text('本次模考', style: SpeakUpDesign.sectionTitle),
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.all(18),
            decoration: BoxDecoration(
              color: const Color(0xFFF4F7FD),
              borderRadius: BorderRadius.circular(20),
            ),
            child: Column(
              children: [
                _ResultLine(label: 'Part 1', value: '$part1AnswerCount 题'),
                const SizedBox(height: 12),
                _ResultLine(
                  label: 'Part 2',
                  value: '${progress.part2SpokenSeconds} 秒',
                ),
                const SizedBox(height: 12),
                _ResultLine(label: 'Part 3', value: '$part3AnswerCount 题'),
                const Divider(height: 28),
                _ResultLine(label: '总用时', value: '$totalMinutes 分钟'),
              ],
            ),
          ),
          const SizedBox(height: 28),
          FilledButton(
            key: const Key('ielts-mock-back-to-training'),
            onPressed: onPressed,
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(58),
              backgroundColor: SpeakUpDesign.ink,
              foregroundColor: Colors.white,
            ),
            child: const Text('返回训练'),
          ),
        ],
      ),
    );
  }
}

class _CompactCompletionHeader extends StatelessWidget {
  const _CompactCompletionHeader({required this.title, required this.message});

  final String title;
  final String message;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: 42,
          height: 42,
          decoration: BoxDecoration(
            color: SpeakUpDesign.success.withValues(alpha: 0.12),
            shape: BoxShape.circle,
          ),
          child: const Icon(Icons.check_rounded, color: SpeakUpDesign.success),
        ),
        const SizedBox(width: 14),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: SpeakUpDesign.pageTitle.copyWith(fontSize: 26),
              ),
              const SizedBox(height: 4),
              Text(message, style: SpeakUpDesign.body),
            ],
          ),
        ),
      ],
    );
  }
}

class _CompletedReportPage extends StatelessWidget {
  const _CompletedReportPage({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(title)),
      body: SafeArea(
        top: false,
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
          child: child,
        ),
      ),
    );
  }
}

class _ResultLine extends StatelessWidget {
  const _ResultLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(label, style: SpeakUpDesign.body),
        const Spacer(),
        Text(value, style: SpeakUpDesign.cardTitle),
      ],
    );
  }
}

bool _sameProgress(IeltsMockProgress a, IeltsMockProgress b) =>
    a.sessionId == b.sessionId &&
    a.phase == b.phase &&
    a.startedAt == b.startedAt &&
    a.preparationDeadline == b.preparationDeadline &&
    a.speakingStartedAt == b.speakingStartedAt &&
    a.speakingDeadline == b.speakingDeadline &&
    a.part2SpokenSeconds == b.part2SpokenSeconds &&
    a.notes == b.notes;
