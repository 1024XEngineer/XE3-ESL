part of 'ielts_mock_practice.dart';

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
                  translation: tip.translation,
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
    required this.onRetryConfirmation,
    required this.onRerecord,
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
  final VoidCallback onRetryConfirmation;
  final VoidCallback onRerecord;
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
        state == PracticeRecordingState.submitting;
    final control = VoiceCaptureControl(
      phase: phase,
      enabled:
          enabledOverride ??
          (!convertedAnswerMode &&
              !controller.hasPendingPracticeAudio &&
              state != PracticeRecordingState.awaitingConfirmation &&
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
            : state == PracticeRecordingState.awaitingConfirmation
            ? PracticeTranscriptComposer(
                transcript: controller.transcript ?? '',
                keyPrefix: 'ielts-mock',
                onRerecord: onRerecord,
                onConfirm: onRetryConfirmation,
                confirmLabel: controller.errorMessage == null ? '发送回答' : '重试提交',
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
      PracticeRecordingState.submitting => '正在提交你的回答…',
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
    super.key,
  });

  final String title;
  final String message;
  final String buttonLabel;
  final VoidCallback? onPressed;

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
  final SessionEvaluationController? reportStatusController;
  final Future<void> Function() onOpenReport;
  final VoidCallback onNext;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final part = switch (mode) {
      PracticeMode.part1 => 'Part 1',
      PracticeMode.part2 => 'Part 2',
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
                    _SessionEvaluationStatusCard(
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
              if (processing && !ready)
                Center(
                  child: SizedBox.square(
                    dimension: 72,
                    child: CircularProgressIndicator(
                      key: const Key('ielts-part2-processing-indicator'),
                      color: color,
                      strokeWidth: 6,
                      semanticsLabel: '正在识别 Part 2 作答',
                    ),
                  ),
                )
              else
                Icon(
                  failed
                      ? Icons.error_outline_rounded
                      : Icons.check_circle_rounded,
                  size: 88,
                  color: color,
                ),
              const SizedBox(height: 24),
              Text(
                failed
                    ? 'Part 2 处理失败'
                    : ready
                    ? 'Part 2 已完成'
                    : '正在处理 Part 2',
                textAlign: TextAlign.center,
                style: SpeakUpDesign.pageTitle.copyWith(fontSize: 30),
              ),
              const SizedBox(height: 12),
              Text(
                failed
                    ? errorMessage!
                    : ready
                    ? '作答已经保存。请选择继续 Part 3，或返回训练。'
                    : '录音已保存，正在识别你的回答。完成前不会进入 Part 3。',
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
              else if (ready)
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
                  if (recordingState == PracticeRecordingState.starting ||
                      recordingState == PracticeRecordingState.recording) ...[
                    const SizedBox(height: 20),
                    FilledButton(
                      key: const Key('ielts-mock-finish-speaking'),
                      onPressed: busy ? null : onPressed,
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(52),
                        backgroundColor: SpeakUpDesign.ink,
                        foregroundColor: Colors.white,
                      ),
                      child: const Text('结束作答 →'),
                    ),
                  ],
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
  final SessionEvaluationController? reportStatusController;
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
          _SessionEvaluationStatusCard(
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

class _SessionEvaluationStatusCard extends StatelessWidget {
  const _SessionEvaluationStatusCard({
    required this.controller,
    required this.onOpenReport,
  });

  final SessionEvaluationController? controller;
  final Future<void> Function() onOpenReport;

  @override
  Widget build(BuildContext context) {
    final current = controller;
    if (current == null) {
      return const Card(
        child: Padding(
          padding: EdgeInsets.all(16),
          child: Text('复盘会在练习完成后生成。'),
        ),
      );
    }
    return AnimatedBuilder(
      animation: current,
      builder: (context, _) {
        final evaluation = current.evaluation;
        final ready = evaluation?.status == SessionEvaluationStatus.ready;
        final failed =
            evaluation?.status == SessionEvaluationStatus.failed ||
            current.errorMessage != null && !current.isLoading;
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  ready
                      ? '复盘已生成'
                      : failed
                      ? current.errorMessage ?? '复盘生成失败。'
                      : '正在生成复盘…',
                  style: SpeakUpDesign.body,
                ),
                if (ready) ...[
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: () => unawaited(onOpenReport()),
                    child: const Text('查看复盘'),
                  ),
                ] else if (failed && current.canRetry) ...[
                  const SizedBox(height: 12),
                  OutlinedButton(
                    onPressed: () => unawaited(current.retry()),
                    child: const Text('重新生成报告'),
                  ),
                ] else ...[
                  const SizedBox(height: 12),
                  const LinearProgressIndicator(),
                ],
              ],
            ),
          ),
        );
      },
    );
  }
}
