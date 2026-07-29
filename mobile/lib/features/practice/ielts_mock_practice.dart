import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/voice_capture_control.dart';
import 'package:speakup/practice/ielts_mock_progress_store.dart';

const ieltsSpeakingFullMockScenarioId = 'scn_ielts_speaking_full';
const _ieltsSpeakingFullMockTitle = 'IELTS 口语完整模拟';

bool isIeltsSpeakingFullMockSession(AgentController controller) =>
    controller.turnLimit == 14 &&
    (controller.scene?.id == ieltsSpeakingFullMockScenarioId ||
        controller.scene?.title == _ieltsSpeakingFullMockTitle);

class IeltsSpeakingMockPage extends StatefulWidget {
  const IeltsSpeakingMockPage({
    required this.controller,
    this.onExitRequested,
    this.progressStore,
    this.now = DateTime.now,
    super.key,
  });

  final AgentController controller;
  final Future<bool> Function()? onExitRequested;
  final IeltsMockProgressStore? progressStore;
  final DateTime Function() now;

  @override
  State<IeltsSpeakingMockPage> createState() => _IeltsSpeakingMockPageState();
}

class _IeltsSpeakingMockPageState extends State<IeltsSpeakingMockPage> {
  late final IeltsMockProgressStore _progressStore;
  final TextEditingController _notesController = TextEditingController();

  IeltsMockProgress? _progress;
  Timer? _ticker;
  DateTime _now = DateTime.now().toUtc();
  bool _loading = true;
  bool _confirming = false;
  bool _startingPart2Recording = false;
  bool _finishingPart2Recording = false;
  bool _exitApproved = false;
  bool _exitInFlight = false;

  @override
  void initState() {
    super.initState();
    _progressStore = widget.progressStore ?? FileIeltsMockProgressStore();
    _now = widget.now().toUtc();
    widget.controller.addListener(_handleControllerState);
    _notesController.addListener(_saveNotes);
    unawaited(_restoreProgress());
  }

  @override
  void didUpdateWidget(covariant IeltsSpeakingMockPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller.removeListener(_handleControllerState);
      widget.controller.addListener(_handleControllerState);
      unawaited(_restoreProgress());
    }
  }

  @override
  void dispose() {
    widget.controller.removeListener(_handleControllerState);
    _notesController.removeListener(_saveNotes);
    _notesController.dispose();
    _ticker?.cancel();
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
            phase: IeltsMockPhase.part1,
            startedAt: widget.now().toUtc(),
          ),
    );
    _notesController.text = restored.notes;
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
  }

  IeltsMockProgress _reconcileProgress(IeltsMockProgress value) {
    final completed = widget.controller.completedTurns;
    if (completed >= 14) {
      return value.copyWith(
        phase: IeltsMockPhase.complete,
        clearPreparationDeadline: true,
        clearSpeakingDeadline: true,
      );
    }
    if (completed >= 10) {
      return value.copyWith(
        phase: IeltsMockPhase.part3,
        clearPreparationDeadline: true,
        clearSpeakingDeadline: true,
      );
    }
    if (completed == 9) {
      final phase = value.phase == IeltsMockPhase.part3
          ? IeltsMockPhase.part3
          : IeltsMockPhase.part2Complete;
      return value.copyWith(
        phase: phase,
        clearPreparationDeadline: true,
        clearSpeakingDeadline: true,
      );
    }
    if (completed == 8) {
      final phase = switch (value.phase) {
        IeltsMockPhase.part1Complete ||
        IeltsMockPhase.part2Intro ||
        IeltsMockPhase.part2Preparation => value.phase,
        IeltsMockPhase.part2Speaking
            when widget.controller.recordingState !=
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
    _confirmPendingTranscript();

    final progress = _progress;
    if (progress != null) {
      final reconciled = _reconcileProgress(progress);
      if (!_sameProgress(progress, reconciled)) {
        _progress = reconciled;
        unawaited(_progressStore.write(reconciled));
        _syncTicker();
      }
    }
    setState(() {});
  }

  void _confirmPendingTranscript() {
    if (!mounted ||
        _confirming ||
        widget.controller.recordingState !=
            PracticeRecordingState.awaitingConfirmation) {
      return;
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
    await _setProgress(progress.copyWith(phase: IeltsMockPhase.part2Intro));
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

  Future<void> _startPart2Speaking() async {
    final progress = _progress;
    if (progress == null ||
        _startingPart2Recording ||
        progress.phase == IeltsMockPhase.part2Speaking) {
      return;
    }
    _startingPart2Recording = true;
    final now = widget.now().toUtc();
    final speaking = progress.copyWith(
      phase: IeltsMockPhase.part2Speaking,
      clearPreparationDeadline: true,
      speakingStartedAt: now,
      speakingDeadline: now.add(const Duration(seconds: 120)),
    );
    await _setProgress(speaking);
    await widget.controller.startRecording(limit: const Duration(seconds: 120));
    if (!mounted) {
      return;
    }
    _startingPart2Recording = false;
    if (widget.controller.recordingState == PracticeRecordingState.idle) {
      await _setProgress(
        speaking.copyWith(
          phase: IeltsMockPhase.part2Intro,
          clearSpeakingStartedAt: true,
          clearSpeakingDeadline: true,
        ),
      );
    } else {
      setState(() {});
    }
  }

  Future<void> _finishPart2Speaking() async {
    if (_finishingPart2Recording) {
      return;
    }
    _finishingPart2Recording = true;
    final progress = _progress;
    if (progress != null && progress.speakingStartedAt != null) {
      final spoken = widget
          .now()
          .toUtc()
          .difference(progress.speakingStartedAt!)
          .inSeconds
          .clamp(0, 120);
      final updated = progress.copyWith(part2SpokenSeconds: spoken);
      _progress = updated;
      unawaited(_progressStore.write(updated));
    }
    final state = widget.controller.recordingState;
    if (state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording) {
      await widget.controller.finishRecordingGesture();
    } else if (state == PracticeRecordingState.awaitingConfirmation) {
      _confirmPendingTranscript();
    }
    if (mounted) {
      setState(() => _finishingPart2Recording = false);
    }
  }

  Future<void> _startShortRecording() => widget.controller.startRecording();

  Future<void> _finishShortRecording() =>
      widget.controller.finishRecordingGesture();

  Future<void> _cancelShortRecording() => widget.controller.cancelRecording();

  Future<void> _beginPart3() async {
    final progress = _progress;
    if (progress == null) {
      return;
    }
    await _setProgress(progress.copyWith(phase: IeltsMockPhase.part3));
  }

  Future<void> _requestExit() async {
    if (_exitApproved || _exitInFlight || !mounted) {
      return;
    }
    final shouldExit =
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
    _exitApproved = true;
    setState(() {});
    await WidgetsBinding.instance.endOfFrame;
    if (mounted) {
      await Navigator.of(context).maybePop();
    }
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
    return PopScope<void>(
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
                onStart: _startShortRecording,
                onFinish: _finishShortRecording,
                onCancel: _cancelShortRecording,
              )
            : null,
      ),
    );
  }

  PreferredSizeWidget? _buildAppBar(IeltsMockPhase phase) {
    final title = switch (phase) {
      IeltsMockPhase.part1 => 'IELTS · Part 1',
      IeltsMockPhase.part2Intro ||
      IeltsMockPhase.part2Preparation ||
      IeltsMockPhase.part2Speaking => 'IELTS · Part 2',
      IeltsMockPhase.part3 => 'IELTS · Part 3',
      _ => 'IELTS Speaking',
    };
    return AppBar(
      backgroundColor: SpeakUpDesign.surface,
      surfaceTintColor: Colors.transparent,
      centerTitle: true,
      leading: IconButton(
        key: const Key('ielts-mock-exit'),
        tooltip: 'Exit mock test',
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
        completed: widget.controller.completedTurns.clamp(0, 8),
        total: 8,
        sectionStart: 0,
      ),
      IeltsMockPhase.part1Complete => _CompletionStep(
        key: const Key('ielts-mock-part-1-complete'),
        title: 'Part 1 Complete',
        message: "Well done! You've finished Part 1. Next up: Part 2.",
        buttonLabel: 'Continue to Part 2',
        onPressed: _beginPart2Intro,
      ),
      IeltsMockPhase.part2Intro => _Part2Intro(
        question: _currentQuestionText(),
        onPressed: _beginPart2Preparation,
      ),
      IeltsMockPhase.part2Preparation => _Part2Preparation(
        secondsRemaining: _secondsUntil(progress.preparationDeadline),
        question: _currentQuestionText(),
        notesController: _notesController,
        onPressed: _startPart2Speaking,
      ),
      IeltsMockPhase.part2Speaking => _Part2Speaking(
        secondsRemaining: _secondsUntil(progress.speakingDeadline),
        notes: progress.notes,
        busy:
            _finishingPart2Recording ||
            widget.controller.recordingState ==
                PracticeRecordingState.transcribing ||
            widget.controller.recordingState ==
                PracticeRecordingState.submitting,
        errorMessage: widget.controller.errorMessage,
        onPressed: _finishPart2Speaking,
      ),
      IeltsMockPhase.part2Complete => _CompletionStep(
        key: const Key('ielts-mock-part-2-complete'),
        title: 'Part 2 Complete',
        message: "Well done! You've finished Part 2. Next up: Part 3.",
        buttonLabel: 'Continue to Part 3',
        onPressed: _beginPart3,
      ),
      IeltsMockPhase.part3 => _conversationPhase(
        key: const Key('ielts-mock-part-3'),
        partLabel: 'Part 3 · Discussion',
        completed: (widget.controller.completedTurns - 9).clamp(0, 5),
        total: 5,
        sectionStart: 9,
      ),
      IeltsMockPhase.complete => _MockComplete(
        progress: progress,
        onPressed: _requestExit,
      ),
    };
  }

  Widget _conversationPhase({
    required Key key,
    required String partLabel,
    required int completed,
    required int total,
    required int sectionStart,
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
            messages: _sectionMessages(sectionStart, completed),
            controller: widget.controller,
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

  List<AgentMessage> _sectionMessages(int sectionStart, int completed) {
    final relevantCount = completed * 2 + 1;
    final all = widget.controller.messages
        .where(
          (message) =>
              message.role == AgentMessageRole.assistant ||
              message.role == AgentMessageRole.user,
        )
        .toList(growable: false);
    if (all.length <= relevantCount) {
      return all;
    }
    return all.sublist(all.length - relevantCount);
  }

  String _currentQuestionText() {
    for (final message in widget.controller.messages.reversed) {
      if (message.role == AgentMessageRole.assistant) {
        return message.text;
      }
    }
    return 'Describe a skill you would like to learn.';
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
  const _ExamConversation({required this.messages, required this.controller});

  final List<AgentMessage> messages;
  final AgentController controller;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      key: const Key('ielts-mock-conversation'),
      padding: const EdgeInsets.fromLTRB(20, 26, 20, 28),
      itemCount: messages.length,
      itemBuilder: (context, index) {
        final message = messages[index];
        final assistant = message.role == AgentMessageRole.assistant;
        return Align(
          alignment: assistant ? Alignment.centerLeft : Alignment.centerRight,
          child: Padding(
            padding: const EdgeInsets.only(bottom: 16),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (assistant) ...[
                  const CircleAvatar(
                    radius: 17,
                    backgroundColor: Color(0xFF5C97E5),
                    foregroundColor: Colors.white,
                    child: Text(
                      'E',
                      style: TextStyle(fontWeight: FontWeight.w700),
                    ),
                  ),
                  const SizedBox(width: 9),
                ],
                Flexible(
                  child: Container(
                    constraints: const BoxConstraints(maxWidth: 290),
                    padding: const EdgeInsets.fromLTRB(14, 11, 14, 12),
                    decoration: BoxDecoration(
                      color: assistant
                          ? SpeakUpDesign.surfaceMuted
                          : const Color(0xFF197782),
                      borderRadius: BorderRadius.circular(15),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          assistant ? 'Examiner' : 'You',
                          style: TextStyle(
                            color: assistant
                                ? SpeakUpDesign.secondary
                                : Colors.white70,
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(height: 5),
                        Text(
                          message.text,
                          style: TextStyle(
                            color: assistant ? SpeakUpDesign.ink : Colors.white,
                            fontSize: 15,
                            height: 1.4,
                          ),
                        ),
                        if (assistant &&
                            message.id == controller.questionId) ...[
                          const SizedBox(height: 8),
                          InkWell(
                            key: const Key('ielts-mock-question-audio'),
                            onTap: controller.canUsePracticeAudio
                                ? controller.toggleQuestionAudio
                                : null,
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Icon(
                                  controller.isQuestionAudioPlaying
                                      ? Icons.stop_rounded
                                      : Icons.graphic_eq_rounded,
                                  size: 20,
                                  color: SpeakUpDesign.secondary,
                                ),
                                const SizedBox(width: 6),
                                Text(
                                  controller.isQuestionAudioLoading
                                      ? 'Loading…'
                                      : controller.isQuestionAudioPlaying
                                      ? 'Stop'
                                      : 'Play question',
                                  style: SpeakUpDesign.meta,
                                ),
                              ],
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                ),
                if (!assistant) ...[
                  const SizedBox(width: 9),
                  const CircleAvatar(
                    radius: 17,
                    backgroundColor: Color(0xFF197782),
                    foregroundColor: Colors.white,
                    child: Text(
                      'Me',
                      style: TextStyle(
                        fontSize: 10,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
        );
      },
    );
  }
}

class _RecorderDock extends StatelessWidget {
  const _RecorderDock({
    required this.controller,
    required this.onStart,
    required this.onFinish,
    required this.onCancel,
  });

  final AgentController controller;
  final VoiceCaptureAction onStart;
  final VoiceCaptureAction onFinish;
  final VoiceCaptureAction onCancel;

  @override
  Widget build(BuildContext context) {
    final state = controller.recordingState;
    final recording =
        state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording;
    final working =
        state == PracticeRecordingState.transcribing ||
        state == PracticeRecordingState.submitting;
    final capturePhase = switch (state) {
      PracticeRecordingState.idle => VoiceCapturePhase.idle,
      PracticeRecordingState.starting => VoiceCapturePhase.starting,
      PracticeRecordingState.recording => VoiceCapturePhase.recording,
      _ => VoiceCapturePhase.busy,
    };
    final label = switch (state) {
      PracticeRecordingState.starting => 'Opening microphone…',
      PracticeRecordingState.recording => 'Listening',
      PracticeRecordingState.transcribing => 'Transcribing your answer…',
      PracticeRecordingState.awaitingConfirmation => 'Submitting your answer…',
      PracticeRecordingState.submitting => 'Preparing the next question…',
      _ => 'Ready to speak',
    };
    return Material(
      color: SpeakUpDesign.surface,
      child: SafeArea(
        top: false,
        minimum: const EdgeInsets.fromLTRB(20, 12, 20, 14),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              label,
              key: const Key('ielts-mock-recorder-state'),
              style: SpeakUpDesign.cardTitle.copyWith(
                color: recording ? const Color(0xFF197782) : SpeakUpDesign.ink,
              ),
            ),
            const SizedBox(height: 12),
            VoiceCaptureControl(
              phase: capturePhase,
              mode: VoiceCaptureMode.tapToToggle,
              enabled: capturePhase != VoiceCapturePhase.busy,
              onStart: onStart,
              onFinish: onFinish,
              onCancel: onCancel,
              builder: (context, capture) => capture.wrapTarget(
                key: const Key('ielts-mock-record'),
                semanticsLabel: recording ? 'Submit answer' : 'Start recording',
                child: IconButton.filled(
                  onPressed: () {},
                  style: IconButton.styleFrom(
                    fixedSize: const Size.square(76),
                    backgroundColor: recording
                        ? const Color(0xFF197782)
                        : SpeakUpDesign.ink,
                    foregroundColor: Colors.white,
                  ),
                  icon: working
                      ? const SizedBox.square(
                          dimension: 25,
                          child: CircularProgressIndicator(
                            strokeWidth: 2.5,
                            color: Colors.white,
                          ),
                        )
                      : Icon(
                          recording
                              ? Icons.stop_rounded
                              : Icons.mic_none_rounded,
                          size: 32,
                        ),
                ),
              ),
            ),
            const SizedBox(height: 10),
            Text(
              recording
                  ? 'Tap again to submit your answer'
                  : 'Tap to start recording your answer',
              style: SpeakUpDesign.body.copyWith(fontSize: 13),
            ),
          ],
        ),
      ),
    );
  }
}

class _CompletionStep extends StatelessWidget {
  const _CompletionStep({
    required this.title,
    required this.message,
    required this.buttonLabel,
    required this.onPressed,
    super.key,
  });

  final String title;
  final String message;
  final String buttonLabel;
  final VoidCallback onPressed;

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
                key: const Key('ielts-mock-continue'),
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

class _Part2Intro extends StatelessWidget {
  const _Part2Intro({required this.question, required this.onPressed});

  final String question;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
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
          const SizedBox(height: 20),
          _CueCard(question: question),
          const SizedBox(height: 22),
          const Text(
            'You will have 1 minute to prepare and up to 2 minutes to speak. You may take notes during preparation.',
            style: SpeakUpDesign.body,
          ),
          const SizedBox(height: 28),
          FilledButton(
            key: const Key('ielts-mock-part-2-start'),
            onPressed: onPressed,
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(58),
              backgroundColor: SpeakUpDesign.ink,
              foregroundColor: Colors.white,
            ),
            child: const Text('I understand — Start →'),
          ),
        ],
      ),
    );
  }
}

class _Part2Preparation extends StatelessWidget {
  const _Part2Preparation({
    required this.secondsRemaining,
    required this.question,
    required this.notesController,
    required this.onPressed,
  });

  final int secondsRemaining;
  final String question;
  final TextEditingController notesController;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      key: const Key('ielts-mock-part-2-preparation'),
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            '${secondsRemaining}s',
            key: const Key('ielts-mock-preparation-countdown'),
            textAlign: TextAlign.center,
            style: SpeakUpDesign.pageTitle.copyWith(fontSize: 44),
          ),
          const SizedBox(height: 4),
          const Text(
            'Preparation time',
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
              hintText: 'Make notes here…',
              counterText: '',
              alignLabelWithHint: true,
            ),
          ),
          const SizedBox(height: 18),
          FilledButton(
            key: const Key('ielts-mock-start-speaking'),
            onPressed: onPressed,
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(52),
              backgroundColor: SpeakUpDesign.ink,
              foregroundColor: Colors.white,
            ),
            child: const Text('Start Speaking →'),
          ),
        ],
      ),
    );
  }
}

class _Part2Speaking extends StatelessWidget {
  const _Part2Speaking({
    required this.secondsRemaining,
    required this.notes,
    required this.busy,
    required this.errorMessage,
    required this.onPressed,
  });

  final int secondsRemaining;
  final String notes;
  final bool busy;
  final String? errorMessage;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final minutes = secondsRemaining ~/ 60;
    final seconds = (secondsRemaining % 60).toString().padLeft(2, '0');
    return SingleChildScrollView(
      key: const Key('ielts-mock-part-2-speaking'),
      padding: const EdgeInsets.fromLTRB(20, 18, 20, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Text(
                '$minutes:$seconds',
                key: const Key('ielts-mock-speaking-countdown'),
                style: SpeakUpDesign.pageTitle.copyWith(fontSize: 42),
              ),
              const Spacer(),
              const Row(
                children: [
                  CircleAvatar(radius: 5, backgroundColor: Color(0xFF5C97E5)),
                  SizedBox(width: 7),
                  CircleAvatar(radius: 5, backgroundColor: Color(0xFFAFC7EB)),
                  SizedBox(width: 7),
                  CircleAvatar(radius: 5, backgroundColor: Color(0xFFDDE9F8)),
                ],
              ),
            ],
          ),
          const SizedBox(height: 4),
          const Text('Speaking time — talk now!', style: SpeakUpDesign.body),
          const SizedBox(height: 22),
          Container(
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              color: const Color(0xFFFAFAF9),
              borderRadius: BorderRadius.circular(18),
            ),
            child: Text(
              notes.isEmpty ? 'No notes were taken during prep.' : notes,
              textAlign: TextAlign.center,
              style: SpeakUpDesign.body,
            ),
          ),
          if (errorMessage != null) ...[
            const SizedBox(height: 16),
            Text(
              errorMessage!,
              style: const TextStyle(color: SpeakUpDesign.error),
            ),
          ],
          const SizedBox(height: 26),
          FilledButton(
            key: const Key('ielts-mock-finish-speaking'),
            onPressed: busy ? null : onPressed,
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(58),
              backgroundColor: SpeakUpDesign.ink,
              foregroundColor: Colors.white,
            ),
            child: busy
                ? const SizedBox.square(
                    dimension: 22,
                    child: CircularProgressIndicator(
                      strokeWidth: 2.4,
                      color: Colors.white,
                    ),
                  )
                : const Text('Finish Speaking →'),
          ),
        ],
      ),
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
  const _MockComplete({required this.progress, required this.onPressed});

  final IeltsMockProgress progress;
  final VoidCallback onPressed;

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
            'Mock Test Complete',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.pageTitle.copyWith(fontSize: 28),
          ),
          const SizedBox(height: 10),
          const Text(
            'You have completed all three parts of the IELTS Speaking mock test.',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.body,
          ),
          const SizedBox(height: 28),
          Container(
            padding: const EdgeInsets.all(18),
            decoration: BoxDecoration(
              color: const Color(0xFFF4F7FD),
              borderRadius: BorderRadius.circular(20),
            ),
            child: Column(
              children: [
                const _ResultLine(label: 'Part 1', value: '8 answers'),
                const SizedBox(height: 12),
                _ResultLine(
                  label: 'Part 2',
                  value: '${progress.part2SpokenSeconds}s talk',
                ),
                const SizedBox(height: 12),
                const _ResultLine(label: 'Part 3', value: '5 answers'),
                const Divider(height: 28),
                _ResultLine(label: 'Total time', value: '$totalMinutes min'),
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
            child: const Text('Back to Training →'),
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
