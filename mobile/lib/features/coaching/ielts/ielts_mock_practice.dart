import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
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
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

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
    this.speechFeedbackController,
    this.examinerSpeaker,
    this.now = DateTime.now,
    super.key,
  });

  final PracticeController controller;
  final Future<bool> Function()? onExitRequested;
  final IeltsMockProgressStore? progressStore;
  final IeltsPreparationController? ieltsController;
  final IeltsCompletedReportBuilder? completedReportBuilder;
  final SpeechFeedbackController? speechFeedbackController;
  final PracticePromptSpeaker? examinerSpeaker;
  final DateTime Function() now;

  @override
  State<IeltsSpeakingMockPage> createState() => _IeltsSpeakingMockPageState();
}

class _IeltsSpeakingMockPageState extends State<IeltsSpeakingMockPage> {
  late final IeltsMockProgressStore _progressStore;
  late final PracticePromptSpeaker _examinerSpeaker;
  late final bool _ownsExaminerSpeaker;
  final TextEditingController _notesController = TextEditingController();
  final TextEditingController _convertedAnswerController =
      TextEditingController();
  final FocusNode _convertedAnswerFocusNode = FocusNode();

  IeltsMockProgress? _progress;
  Timer? _ticker;
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
  final Set<String> _revealedQuestionIds = <String>{};
  String? _autoNarratedQuestionId;
  String? _autoNarratedQuestionText;
  String? _playingQuestionId;
  String? _questionNarrationErrorId;
  int _questionNarrationGeneration = 0;
  final Set<PracticeMode> _recordedCompletions = <PracticeMode>{};
  final Map<String, String> _feedbackSources = <String, String>{};
  bool _feedbackRebuildScheduled = false;

  IeltsPracticeSelection? get _selection {
    final sessionId = widget.controller.practiceSessionId;
    return sessionId == null
        ? null
        : widget.ieltsController?.selectionForSession(sessionId);
  }

  bool get _part2TurnConfirmed {
    if (_mode != PracticeMode.fullMock && _mode != PracticeMode.part2) {
      return true;
    }
    final questionId = _part2QuestionId;
    if (questionId != null &&
        widget.controller.currentQuestion?.id != questionId) {
      return true;
    }
    final state = widget.controller.recordingState;
    final submissionActive =
        state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording ||
        state == PracticeRecordingState.transcribing ||
        state == PracticeRecordingState.awaitingConfirmation ||
        state == PracticeRecordingState.submitting;
    return (_progress?.phase == IeltsMockPhase.part3Intro ||
            _progress?.phase == IeltsMockPhase.part3) &&
        !submissionActive &&
        !widget.controller.hasPendingPracticeAudio;
  }

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

  bool get _part2SubmissionStarted =>
      switch (widget.controller.recordingState) {
        PracticeRecordingState.transcribing ||
        PracticeRecordingState.awaitingConfirmation ||
        PracticeRecordingState.submitting => true,
        _ => false,
      };

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
    await _stopQuestionNarration();
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
    } on Object {
      if (mounted) {
        setState(() {
          _bufferedPart3RecordingState = PracticeRecordingState.idle;
          _answerLanguageError = '暂时无法开始录音，请重新尝试。';
        });
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
    widget.controller.addListener(_handleControllerState);
    widget.speechFeedbackController?.addListener(_handleSpeechFeedbackState);
    _notesController.addListener(_saveNotes);
    _syncSpeechFeedbackSources();
    unawaited(_restoreProgress());
  }

  @override
  void didUpdateWidget(covariant IeltsSpeakingMockPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    final controllerChanged = oldWidget.controller != widget.controller;
    if (controllerChanged) {
      oldWidget.controller.removeListener(_handleControllerState);
      widget.controller.addListener(_handleControllerState);
      _questionNarrationGeneration++;
      _autoNarratedQuestionId = null;
      _autoNarratedQuestionText = null;
      _playingQuestionId = null;
      _questionNarrationErrorId = null;
      _revealedQuestionIds.clear();
      unawaited(_stopExaminerSpeakerSafely());
      unawaited(_restoreProgress());
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
    _convertedAnswerController.dispose();
    _convertedAnswerFocusNode.dispose();
    _ticker?.cancel();
    _part2TranscriptionRetryTimer?.cancel();
    _bufferedPart3RecordingLimitTimer?.cancel();
    unawaited(_discardBufferedPart3Recording());
    unawaited(_stopExaminerSpeakerSafely());
    if (_ownsExaminerSpeaker) {
      unawaited(_examinerSpeaker.dispose());
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
      if (value.phase == IeltsMockPhase.part3) {
        return value.copyWith(
          phase: IeltsMockPhase.part3,
          clearPreparationDeadline: true,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        );
      }
      if (value.phase == IeltsMockPhase.part3Intro) {
        return value.copyWith(
          phase: IeltsMockPhase.part3Intro,
          clearPreparationDeadline: true,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        );
      }
      if (completed >= _partEnd(IeltsSpeakingPart.part2) ||
          value.phase == IeltsMockPhase.part2Complete) {
        return value.copyWith(
          phase: IeltsMockPhase.part2Complete,
          clearPreparationDeadline: true,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        );
      }
      if (value.phase == IeltsMockPhase.part2Speaking &&
          _part2SubmissionStarted) {
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
        IeltsMockPhase.part2Speaking => IeltsMockPhase.part2Speaking,
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
    if (value.phase == IeltsMockPhase.part3 ||
        value.phase == IeltsMockPhase.part3Intro) {
      return value.copyWith(
        phase: IeltsMockPhase.part3,
        clearPreparationDeadline: true,
        clearSpeakingStartedAt: true,
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
    if (completed >= _partEnd(IeltsSpeakingPart.part1)) {
      final phase = switch (value.phase) {
        IeltsMockPhase.part1Complete ||
        IeltsMockPhase.part2Intro ||
        IeltsMockPhase.part2CueCard ||
        IeltsMockPhase.part2Preparation ||
        IeltsMockPhase.part2Complete ||
        IeltsMockPhase.part3Intro => value.phase,
        IeltsMockPhase.part2Speaking when _part2SubmissionStarted =>
          IeltsMockPhase.part2Complete,
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
      setState(() {});
      return;
    }
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
    setState(() {});
    _scheduleQuestionNarration();
    _flushBufferedPart3Audio();
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
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _progress?.phase != phase) {
        return;
      }
      unawaited(_playQuestionNarration(questionId, questionText));
    });
  }

  Future<void> _playQuestionNarration(String questionId, String text) async {
    final currentQuestion = widget.controller.currentQuestion;
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

  void _toggleQuestionTranscript(String questionId) {
    setState(() {
      if (!_revealedQuestionIds.add(questionId)) {
        _revealedQuestionIds.remove(questionId);
      }
    });
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
      if (progress != null && progress.phase != IeltsMockPhase.part2Complete) {
        unawaited(
          _setProgress(
            progress.copyWith(
              phase: IeltsMockPhase.part2Complete,
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
        (progress.phase == IeltsMockPhase.part2Speaking &&
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
    await widget.controller.startRecording(limit: const Duration(seconds: 120));
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

  Future<void> _handlePart2SpeakingAction() async {
    if (widget.controller.recordingState == PracticeRecordingState.idle) {
      if (widget.controller.hasPendingPracticeAudio) {
        await widget.controller.retryPracticeTranscription();
        return;
      }
      await _startPart2Speaking(restart: true);
      return;
    }
    await _finishPart2Speaking();
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
        _enteringPart3) {
      return;
    }
    await _beginPart3();
  }

  Future<void> _startShortRecording() {
    _conversionRequested = false;
    if (_answerLanguageError != null) {
      setState(() => _answerLanguageError = null);
    }
    unawaited(_stopQuestionNarration());
    return widget.controller.startRecording();
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
        await showDialog<bool>(
              context: context,
              builder: (context) => AlertDialog(
                title: const Text('Exit mock test?'),
                content: const Text(
                  'Your completed answers are saved. You can continue from the latest safe step later.',
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Stay'),
                  ),
                  FilledButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('Save & exit'),
                  ),
                ],
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
        appBar: _buildAppBar(progress.phase),
        body: SafeArea(
          top: false,
          child: AnimatedSwitcher(
            duration: const Duration(milliseconds: 220),
            child: _buildPhase(progress),
          ),
        ),
        bottomNavigationBar:
            progress.phase == IeltsMockPhase.part1 ||
                progress.phase == IeltsMockPhase.part3
            ? _RecorderDock(
                controller: widget.controller,
                stateOverride: _usesBufferedPart3Recorder
                    ? _bufferedPart3RecordingState
                    : null,
                enabledOverride: _usesBufferedPart3Recorder
                    ? _bufferedPart3RecordingState ==
                          PracticeRecordingState.idle
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
              )
            : null,
      ),
    );
  }

  PreferredSizeWidget? _buildAppBar(IeltsMockPhase phase) {
    final title = switch (phase) {
      IeltsMockPhase.part1 => 'IELTS · Part 1',
      IeltsMockPhase.part2Intro ||
      IeltsMockPhase.part2CueCard ||
      IeltsMockPhase.part2Preparation ||
      IeltsMockPhase.part2Speaking => 'IELTS · Part 2',
      IeltsMockPhase.part3Intro || IeltsMockPhase.part3 => 'IELTS · Part 3',
      IeltsMockPhase.complete => 'IELTS 口语报告',
      _ => 'IELTS Speaking',
    };
    return AppBar(
      backgroundColor: SpeakUpDesign.surface,
      surfaceTintColor: Colors.transparent,
      centerTitle: true,
      leading: IconButton(
        key: const Key('ielts-mock-exit'),
        tooltip: '退出模考',
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
        busy: _startingPart2Recording,
        errorMessage: null,
        onPressed: _startPart2Speaking,
      ),
      IeltsMockPhase.part2Speaking => _Part2LongTurn(
        speaking: true,
        secondsRemaining: _secondsUntil(progress.speakingDeadline),
        question: _currentQuestionText(),
        notesController: _notesController,
        notes: progress.notes,
        recordingState: widget.controller.recordingState,
        busy:
            _finishingPart2Recording ||
            widget.controller.recordingState ==
                PracticeRecordingState.transcribing ||
            widget.controller.recordingState ==
                PracticeRecordingState.submitting,
        errorMessage:
            _answerLanguageError ??
            widget.controller.errorMessage ??
            (_part2RetryNeeded ? '录音识别失败，请重新录音。' : null),
        onPressed: _handlePart2SpeakingAction,
      ),
      IeltsMockPhase.part2Complete => _Part2Transition(
        key: const Key('ielts-mock-part-2-transition'),
        processing: _part2BackgroundProcessing,
        ready: _part2TurnConfirmed,
        errorMessage:
            _answerLanguageError ??
            widget.controller.errorMessage ??
            (_part2RetryNeeded && !_part2BackgroundProcessing
                ? 'Part 2 录音暂时无法识别，请重新提交。'
                : null),
        onContinue: _continueFromPart2,
        onReturn: () => _requestExit(fromCompletion: true),
        onRetry: _handlePart2SpeakingAction,
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
                report: _completedReport(),
                onPressed: () => _requestExit(fromCompletion: true),
              )
            : _SectionPracticeComplete(
                mode: _mode,
                onNext: () =>
                    _finishSection(IeltsPracticeCompletionAction.next),
                onRetry: () =>
                    _finishSection(IeltsPracticeCompletionAction.retry),
                onList: () =>
                    _finishSection(IeltsPracticeCompletionAction.list),
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
            speechFeedbackController: widget.speechFeedbackController,
            revealedQuestionIds: _revealedQuestionIds,
            playingQuestionId: _playingQuestionId,
            narrationErrorQuestionId: _questionNarrationErrorId,
            mediaPlayingQuestionId: widget.controller.isQuestionAudioPlaying
                ? widget.controller.questionId
                : null,
            onPlayQuestion: _playQuestionNarration,
            onToggleTranscript: _toggleQuestionTranscript,
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

  List<PracticeMessage> _sectionMessages(int sectionStart, int completed) {
    final relevantCount = completed * 2 + 1;
    final all = widget.controller.practiceMessages
        .where(
          (message) =>
              message.role == PracticeMessageRole.assistant ||
              message.role == PracticeMessageRole.user,
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
    required this.revealedQuestionIds,
    required this.playingQuestionId,
    required this.narrationErrorQuestionId,
    required this.mediaPlayingQuestionId,
    required this.onPlayQuestion,
    required this.onToggleTranscript,
    this.speechFeedbackController,
  });

  final List<PracticeMessage> messages;
  final PracticeController controller;
  final Set<String> revealedQuestionIds;
  final String? playingQuestionId;
  final String? narrationErrorQuestionId;
  final String? mediaPlayingQuestionId;
  final Future<void> Function(String questionId, String text) onPlayQuestion;
  final ValueChanged<String> onToggleTranscript;
  final SpeechFeedbackController? speechFeedbackController;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      key: const Key('ielts-mock-conversation'),
      padding: const EdgeInsets.fromLTRB(20, 26, 20, 28),
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
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: assistant
                  ? _ExaminerQuestionBubble(
                      message: message,
                      playing:
                          playingQuestionId == message.id ||
                          mediaPlayingQuestionId == message.id,
                      transcriptVisible: revealedQuestionIds.contains(
                        message.id,
                      ),
                      playbackFailed: narrationErrorQuestionId == message.id,
                      onPlay: () => onPlayQuestion(message.id, message.text),
                      onToggleTranscript: () => onToggleTranscript(message.id),
                    )
                  : PracticeMessageBubble(
                      message: message,
                      feedbackProjection: projection,
                    ),
            ),
          ],
        );
      },
    );
  }
}

class _ExaminerQuestionBubble extends StatelessWidget {
  const _ExaminerQuestionBubble({
    required this.message,
    required this.playing,
    required this.transcriptVisible,
    required this.playbackFailed,
    required this.onPlay,
    required this.onToggleTranscript,
  });

  final PracticeMessage message;
  final bool playing;
  final bool transcriptVisible;
  final bool playbackFailed;
  final VoidCallback onPlay;
  final VoidCallback onToggleTranscript;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 340),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Material(
              key: ValueKey('ielts-question-voice-${message.id}'),
              color: SpeakUpDesign.surfaceMuted,
              borderRadius: BorderRadius.circular(22),
              child: InkWell(
                borderRadius: BorderRadius.circular(22),
                onTap: onPlay,
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 12, 12, 12),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        playing
                            ? Icons.stop_circle_outlined
                            : Icons.play_circle_outline_rounded,
                        color: SpeakUpDesign.primary,
                      ),
                      const SizedBox(width: 10),
                      Icon(
                        Icons.graphic_eq_rounded,
                        color: SpeakUpDesign.primary,
                        size: 34,
                      ),
                      const SizedBox(width: 10),
                      Flexible(
                        child: Text(
                          playing ? 'Examiner is speaking…' : 'Examiner voice',
                          style: SpeakUpDesign.body.copyWith(
                            color: SpeakUpDesign.primary,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
            TextButton.icon(
              key: ValueKey('ielts-question-transcript-toggle-${message.id}'),
              onPressed: onToggleTranscript,
              icon: Icon(
                transcriptVisible
                    ? Icons.visibility_off_outlined
                    : Icons.visibility_outlined,
                size: 18,
              ),
              label: Text(transcriptVisible ? 'Hide English' : 'Show English'),
            ),
            if (playbackFailed)
              const Padding(
                padding: EdgeInsets.only(left: 12, bottom: 8),
                child: Text(
                  'Playback failed. Tap the voice bubble to retry.',
                  style: TextStyle(color: SpeakUpDesign.error, fontSize: 12),
                ),
              ),
            if (transcriptVisible)
              Container(
                key: ValueKey('ielts-question-transcript-${message.id}'),
                margin: const EdgeInsets.only(bottom: 4),
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: SpeakUpDesign.surface,
                  border: Border.all(color: SpeakUpDesign.border),
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Text(message.text, style: SpeakUpDesign.body),
              ),
          ],
        ),
      ),
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
    required this.allowTextAnswer,
    required this.onShowText,
  });

  final VoiceCapturePhase phase;
  final VoiceCaptureView capture;
  final bool allowTextAnswer;
  final VoidCallback onShowText;

  @override
  Widget build(BuildContext context) {
    return VoiceComposerDock(
      capture: capture,
      phase: phase,
      elapsed: Duration.zero,
      enabled: true,
      recordKey: const Key('ielts-mock-record'),
      stopRecordingKey: const Key('ielts-mock-stop-recording'),
      stateLabelKey: const Key('ielts-mock-voice-state-label'),
      durationKey: const Key('ielts-mock-voice-target-duration'),
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
      PracticeRecordingState.transcribing => 'Transcribing your answer…',
      PracticeRecordingState.awaitingConfirmation => 'Submitting your answer…',
      PracticeRecordingState.submitting => 'Answer sent. Agent is replying…',
      _ => 'Working…',
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

class _SectionPracticeComplete extends StatelessWidget {
  const _SectionPracticeComplete({
    required this.mode,
    required this.onNext,
    required this.onRetry,
    required this.onList,
  });

  final PracticeMode mode;
  final VoidCallback onNext;
  final VoidCallback onRetry;
  final VoidCallback onList;

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
    return _SectionActionLayout(
      key: Key('ielts-section-practice-complete-${mode.name}'),
      title: '$part 已完成',
      message: '本套练习已完成，进度已保存。',
      primaryLabel: '下一套未练习',
      onPrimary: onNext,
      onNext: null,
      onRetry: onRetry,
      onList: onList,
    );
  }
}

class _SectionActionLayout extends StatelessWidget {
  const _SectionActionLayout({
    required this.title,
    required this.message,
    required this.primaryLabel,
    required this.onPrimary,
    required this.onNext,
    required this.onRetry,
    required this.onList,
    super.key,
  });

  final String title;
  final String message;
  final String primaryLabel;
  final VoidCallback onPrimary;
  final VoidCallback? onNext;
  final VoidCallback onRetry;
  final VoidCallback onList;

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
              const SizedBox(height: 30),
              FilledButton(
                key: const Key('ielts-section-primary-action'),
                onPressed: onPrimary,
                style: FilledButton.styleFrom(
                  minimumSize: const Size.fromHeight(52),
                  backgroundColor: SpeakUpDesign.ink,
                  foregroundColor: Colors.white,
                ),
                child: Text(primaryLabel),
              ),
              if (onNext case final callback?) ...[
                const SizedBox(height: 10),
                OutlinedButton(
                  key: const Key('ielts-section-next-action'),
                  onPressed: callback,
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  child: const Text('下一套未练习'),
                ),
              ],
              const SizedBox(height: 10),
              TextButton(
                key: const Key('ielts-section-retry-action'),
                onPressed: onRetry,
                child: const Text('再练本套'),
              ),
              TextButton(
                key: const Key('ielts-section-list-action'),
                onPressed: onList,
                child: const Text('返回套题列表'),
              ),
            ],
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
    required this.onRetry,
    super.key,
  });

  final bool processing;
  final bool ready;
  final String? errorMessage;
  final VoidCallback onContinue;
  final VoidCallback onReturn;
  final VoidCallback onRetry;

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
                    : '录音已安全提交，转写和评分将在后台继续，不会阻塞进入 Part 3。',
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body,
              ),
              const SizedBox(height: 28),
              if (failed)
                FilledButton(
                  key: const Key('ielts-part2-retry-submission'),
                  onPressed: onRetry,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(54),
                    backgroundColor: SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                  ),
                  child: const Text('重新提交录音'),
                )
              else
                FilledButton(
                  key: const Key('ielts-part2-continue-part3'),
                  onPressed: onContinue,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(54),
                    backgroundColor: SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                  ),
                  child: const Text('继续 Part 3 →'),
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
    return SingleChildScrollView(
      key: const Key('ielts-mock-part-2-cue-card-reading'),
      padding: const EdgeInsets.fromLTRB(20, 24, 20, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Text(
            'Listen to the examiner',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.sectionTitle,
          ),
          const SizedBox(height: 18),
          _CueCard(question: question),
          const SizedBox(height: 18),
          if (narrationBusy)
            const Row(
              mainAxisAlignment: MainAxisAlignment.center,
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
              textAlign: TextAlign.center,
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
    required this.busy,
    required this.errorMessage,
    required this.onPressed,
  });

  final bool speaking;
  final int secondsRemaining;
  final String question;
  final TextEditingController notesController;
  final String notes;
  final PracticeRecordingState recordingState;
  final bool busy;
  final String? errorMessage;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final minutes = secondsRemaining ~/ 60;
    final seconds = (secondsRemaining % 60).toString().padLeft(2, '0');
    return SingleChildScrollView(
      key: const Key('ielts-mock-part-2-long-turn'),
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            '$minutes:$seconds',
            key: Key(
              speaking
                  ? 'ielts-mock-speaking-countdown'
                  : 'ielts-mock-preparation-countdown',
            ),
            textAlign: TextAlign.center,
            style: SpeakUpDesign.pageTitle.copyWith(fontSize: 44),
          ),
          const SizedBox(height: 4),
          Text(
            speaking ? '作答时间 · 已自动开始录音' : '准备时间 · 阅读题目并记录要点',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.body,
          ),
          if (!speaking) ...[
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
          ] else ...[
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: const Color(0xFFFAFAF9),
                borderRadius: BorderRadius.circular(18),
              ),
              child: Text(
                notes.isEmpty ? '准备阶段未记录要点。' : notes,
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body,
              ),
            ),
            const SizedBox(height: 20),
            _Part2RecordingStatus(
              state: recordingState,
              elapsedSeconds: 120 - secondsRemaining,
            ),
            if (errorMessage != null) ...[
              const SizedBox(height: 16),
              Text(
                errorMessage!,
                textAlign: TextAlign.center,
                style: const TextStyle(color: SpeakUpDesign.error),
              ),
            ],
            if (recordingState == PracticeRecordingState.idle &&
                errorMessage != null) ...[
              const SizedBox(height: 20),
              FilledButton(
                key: const Key('ielts-mock-finish-speaking'),
                onPressed: busy ? null : onPressed,
                style: FilledButton.styleFrom(
                  minimumSize: const Size.fromHeight(52),
                  backgroundColor: SpeakUpDesign.ink,
                  foregroundColor: Colors.white,
                ),
                child: const Text('重新录音 →'),
              ),
            ],
          ],
        ],
      ),
    );
  }
}

class _Part2RecordingStatus extends StatelessWidget {
  const _Part2RecordingStatus({
    required this.state,
    required this.elapsedSeconds,
  });

  final PracticeRecordingState state;
  final int elapsedSeconds;

  @override
  Widget build(BuildContext context) {
    final recording =
        state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording;
    final minutes = (elapsedSeconds.clamp(0, 120) ~/ 60).toString();
    final seconds = (elapsedSeconds.clamp(0, 120) % 60).toString().padLeft(
      2,
      '0',
    );
    final label = switch (state) {
      PracticeRecordingState.starting => '正在打开麦克风…',
      PracticeRecordingState.recording => '录音中·$minutes:$seconds',
      PracticeRecordingState.transcribing => '正在识别你的作答…',
      PracticeRecordingState.awaitingConfirmation => '正在提交作答…',
      PracticeRecordingState.submitting => '正在进入下一环节…',
      _ => '等待录音',
    };
    final color = recording ? const Color(0xFF197782) : SpeakUpDesign.secondary;

    return Column(
      key: const Key('ielts-part2-recording-status'),
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          label,
          key: const Key('ielts-part2-recording-label'),
          textAlign: TextAlign.center,
          style: SpeakUpDesign.cardTitle.copyWith(color: color),
        ),
        const SizedBox(height: 10),
        Icon(Icons.graphic_eq_rounded, size: 42, color: color),
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
    this.report,
  });

  final IeltsMockProgress progress;
  final int totalQuestionCount;
  final int part1AnswerCount;
  final int part3AnswerCount;
  final VoidCallback onPressed;
  final Widget? report;

  @override
  Widget build(BuildContext context) {
    final elapsed = DateTime.now().toUtc().difference(progress.startedAt);
    final totalMinutes = math.max(1, elapsed.inMinutes);
    return SingleChildScrollView(
      key: const Key('ielts-mock-complete'),
      padding: const EdgeInsets.fromLTRB(20, 28, 20, 32),
      child: Column(
        children: [
          const Icon(
            Icons.check_circle_rounded,
            size: 88,
            color: SpeakUpDesign.success,
          ),
          const SizedBox(height: 22),
          Text(
            '模考已完成',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.pageTitle.copyWith(fontSize: 28),
          ),
          const SizedBox(height: 10),
          Text(
            '$totalQuestionCount 道题已全部作答，正在整理你的口语报告。',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.body,
          ),
          if (report case final reportPanel?) ...[
            const SizedBox(height: 24),
            reportPanel,
          ],
          const SizedBox(height: 28),
          Align(
            alignment: Alignment.centerLeft,
            child: Text('本次模考', style: SpeakUpDesign.sectionTitle),
          ),
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
