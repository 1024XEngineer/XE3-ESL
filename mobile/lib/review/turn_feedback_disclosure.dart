import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/review/turn_feedback.dart';
import 'package:speakup/review/turn_feedback_controller.dart';

typedef SpeechFeedbackRepracticeCallback =
    void Function(SpeechFeedbackItem item);

class SpeechFeedbackDisclosure extends StatefulWidget {
  const SpeechFeedbackDisclosure({
    required this.projection,
    this.onRetry,
    this.onRepractice,
    super.key,
  });

  final SpeechFeedbackProjection projection;
  final VoidCallback? onRetry;
  final SpeechFeedbackRepracticeCallback? onRepractice;

  @override
  State<SpeechFeedbackDisclosure> createState() =>
      _SpeechFeedbackDisclosureState();
}

class _SpeechFeedbackDisclosureState extends State<SpeechFeedbackDisclosure> {
  bool _expanded = false;

  @override
  void didUpdateWidget(covariant SpeechFeedbackDisclosure oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.projection.sourceKey != widget.projection.sourceKey) {
      _expanded = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final content = _contentFor(widget.projection);
    return DecoratedBox(
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Semantics(
            button: content.canExpand,
            expanded: content.canExpand ? _expanded : null,
            child: InkWell(
              key: const Key('speech-feedback-disclosure-toggle'),
              borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
              onTap: content.canExpand
                  ? () => setState(() => _expanded = !_expanded)
                  : null,
              child: ConstrainedBox(
                constraints: const BoxConstraints(
                  minHeight: SpeakUpDesign.minTapTarget,
                ),
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: SpeakUpDesign.space12,
                    vertical: SpeakUpDesign.space8,
                  ),
                  child: Row(
                    children: [
                      if (content.loading)
                        SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(
                            key: const Key('speech-feedback-loading-indicator'),
                            strokeWidth: 2,
                            color: content.color,
                          ),
                        )
                      else
                        Icon(content.icon, size: 18, color: content.color),
                      const SizedBox(width: SpeakUpDesign.space8),
                      Expanded(
                        child: Text(
                          content.title,
                          style: Theme.of(context).textTheme.labelLarge
                              ?.copyWith(color: SpeakUpDesign.ink),
                        ),
                      ),
                      if (content.canExpand) ...[
                        const SizedBox(width: SpeakUpDesign.space4),
                        Icon(
                          _expanded
                              ? Icons.keyboard_arrow_up_rounded
                              : Icons.keyboard_arrow_down_rounded,
                          size: 20,
                          color: SpeakUpDesign.secondary,
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ),
          ),
          if (_expanded && content.canExpand)
            Padding(
              key: const Key('speech-feedback-disclosure-content'),
              padding: const EdgeInsets.fromLTRB(
                SpeakUpDesign.space12,
                0,
                SpeakUpDesign.space12,
                SpeakUpDesign.space12,
              ),
              child: _details(context, widget.projection),
            ),
        ],
      ),
    );
  }

  Widget _details(BuildContext context, SpeechFeedbackProjection projection) {
    if (projection.errorMessage != null) {
      return _FailureDetails(
        message: projection.errorMessage!,
        canRetry: projection.canRetry && widget.onRetry != null,
        onRetry: widget.onRetry,
      );
    }
    final feedback = projection.feedback;
    if (feedback == null) {
      return _FailureDetails(
        message: '口语反馈暂时无法加载。',
        canRetry: projection.canRetry && widget.onRetry != null,
        onRetry: widget.onRetry,
      );
    }
    switch (feedback.feedbackStatus) {
      case SpeechFeedbackStatus.queued:
      case SpeechFeedbackStatus.running:
        return const SizedBox.shrink();
      case SpeechFeedbackStatus.failed:
        return _FailureDetails(
          message: '反馈生成遇到技术问题，这不代表你的口语表现较差。',
          canRetry:
              (feedback.stableFailure?.retryable ?? false) &&
              widget.onRetry != null,
          onRetry: widget.onRetry,
        );
      case SpeechFeedbackStatus.ready:
        if (feedback.scoreabilityStatus ==
            SpeechFeedbackScoreabilityStatus.insufficient) {
          return _InsufficientDetails(feedback: feedback);
        }
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              feedback.acousticAssessment.isAssessed
                  ? '表达反馈基于已确认文本，发音表现基于本次录音。'
                  : '以下表达反馈仅基于已确认文本；当前不包含发音或声学流利度判断。',
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: SpeakUpDesign.secondary),
            ),
            const SizedBox(height: SpeakUpDesign.space12),
            for (var index = 0; index < feedback.items.length; index++) ...[
              if (index > 0) const SizedBox(height: SpeakUpDesign.space12),
              _FeedbackItemDetails(
                item: feedback.items[index],
                onRepractice: widget.onRepractice,
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space12),
            _AcousticBoundary(assessment: feedback.acousticAssessment),
          ],
        );
    }
  }
}

final class _DisclosureContent {
  const _DisclosureContent({
    required this.title,
    required this.icon,
    required this.color,
    required this.canExpand,
    this.loading = false,
  });

  final String title;
  final IconData icon;
  final Color color;
  final bool canExpand;
  final bool loading;
}

_DisclosureContent _contentFor(SpeechFeedbackProjection projection) {
  final feedback = projection.feedback;
  if (projection.isPolling) {
    return const _DisclosureContent(
      title: '正在生成评分与纠错…',
      icon: Icons.schedule_rounded,
      color: SpeakUpDesign.secondary,
      canExpand: false,
      loading: true,
    );
  }
  if (projection.errorMessage != null) {
    return _DisclosureContent(
      title: feedback?.isPending == true ? '反馈仍在生成' : '反馈暂时不可用',
      icon: Icons.info_outline_rounded,
      color: SpeakUpDesign.secondary,
      canExpand: true,
    );
  }
  if (feedback?.isPending == true) {
    return const _DisclosureContent(
      title: '正在生成评分与纠错…',
      icon: Icons.schedule_rounded,
      color: SpeakUpDesign.secondary,
      canExpand: false,
      loading: true,
    );
  }
  if (feedback == null) {
    return const _DisclosureContent(
      title: '反馈暂时不可用',
      icon: Icons.info_outline_rounded,
      color: SpeakUpDesign.error,
      canExpand: true,
    );
  }
  if (feedback.feedbackStatus == SpeechFeedbackStatus.failed) {
    return const _DisclosureContent(
      title: '反馈生成失败',
      icon: Icons.info_outline_rounded,
      color: SpeakUpDesign.error,
      canExpand: true,
    );
  }
  if (feedback.scoreabilityStatus ==
      SpeechFeedbackScoreabilityStatus.insufficient) {
    return const _DisclosureContent(
      title: '本轮证据不足',
      icon: Icons.info_outline_rounded,
      color: SpeakUpDesign.secondary,
      canExpand: true,
    );
  }
  return const _DisclosureContent(
    title: '评分与纠错',
    icon: Icons.chat_bubble_outline_rounded,
    color: SpeakUpDesign.primary,
    canExpand: true,
  );
}

class _FeedbackItemDetails extends StatelessWidget {
  const _FeedbackItemDetails({required this.item, this.onRepractice});

  final SpeechFeedbackItem item;
  final SpeechFeedbackRepracticeCallback? onRepractice;

  @override
  Widget build(BuildContext context) {
    final canRepractice = item.canRepractice && onRepractice != null;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              _itemKindLabel(item.kind),
              style: SpeakUpDesign.label.copyWith(color: SpeakUpDesign.primary),
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            Text(
              '“${item.anchor.originalExcerpt}”',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: SpeakUpDesign.ink,
                fontStyle: FontStyle.italic,
              ),
            ),
            if (item.suggestedText != null) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              Text(
                item.suggestedText!,
                key: Key('speech-feedback-suggestion-${item.feedbackItemId}'),
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: SpeakUpDesign.success,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space8),
            Text(
              item.explanation,
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: SpeakUpDesign.secondary),
            ),
            if (canRepractice) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              Align(
                alignment: Alignment.centerLeft,
                child: OutlinedButton.icon(
                  key: Key('speech-feedback-repractice-${item.feedbackItemId}'),
                  onPressed: () => onRepractice!(item),
                  icon: const Icon(Icons.mic_none_rounded, size: 18),
                  label: Text(_repracticeLabel(item.repracticeMode)),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _InsufficientDetails extends StatelessWidget {
  const _InsufficientDetails({required this.feedback});

  final SpeechFeedback feedback;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          _insufficientMessage(feedback.reasonCodes),
          style: Theme.of(
            context,
          ).textTheme.bodySmall?.copyWith(color: SpeakUpDesign.secondary),
        ),
        const SizedBox(height: SpeakUpDesign.space12),
        _AcousticBoundary(assessment: feedback.acousticAssessment),
      ],
    );
  }
}

String _insufficientMessage(List<String> reasonCodes) {
  if (reasonCodes.contains('TRANSCRIPT_CONFIDENCE_INSUFFICIENT')) {
    return '本轮转写或英语内容不足，无法生成可靠评分；不会按低分处理。请尽量全程使用英语回答。';
  }
  if (reasonCodes.contains('EVIDENCE_INCONSISTENT')) {
    return '本轮录音与转写证据不一致，无法生成可靠评分；不会按低分处理。';
  }
  return '输入太短：这次已确认文本不足以生成可靠纠错，不会按低分处理。';
}

class _AcousticBoundary extends StatelessWidget {
  const _AcousticBoundary({required this.assessment});

  final SpeechFeedbackAcousticAssessment assessment;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(
          assessment.isAssessed
              ? Icons.graphic_eq_rounded
              : Icons.hearing_disabled_rounded,
          size: 18,
          color: SpeakUpDesign.secondary,
        ),
        const SizedBox(width: SpeakUpDesign.space8),
        Expanded(
          child: assessment.isAssessed
              ? Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '发音准确度 ${assessment.accuracyScore!.round()} · '
                      '流利度 ${assessment.fluencyScore!.round()} · '
                      '完整度 ${assessment.integrityScore!.round()}',
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                    const SizedBox(height: SpeakUpDesign.space4),
                    Text(
                      assessment.notice!,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: SpeakUpDesign.secondary,
                      ),
                    ),
                  ],
                )
              : Text(
                  '发音与声学流利度未评估：当前没有可信声学证据。',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: SpeakUpDesign.secondary,
                  ),
                ),
        ),
      ],
    );
  }
}

class _FailureDetails extends StatelessWidget {
  const _FailureDetails({
    required this.message,
    required this.canRetry,
    this.onRetry,
  });

  final String message;
  final bool canRetry;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          message,
          style: Theme.of(
            context,
          ).textTheme.bodySmall?.copyWith(color: SpeakUpDesign.secondary),
        ),
        if (canRetry) ...[
          const SizedBox(height: SpeakUpDesign.space8),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              key: const Key('speech-feedback-retry'),
              onPressed: onRetry,
              child: const Text('重试'),
            ),
          ),
        ],
      ],
    );
  }
}

String _itemKindLabel(SpeechFeedbackItemKind kind) => switch (kind) {
  SpeechFeedbackItemKind.correction => '纠错',
  SpeechFeedbackItemKind.strength => '表达亮点',
  SpeechFeedbackItemKind.improvement => '改进建议',
  SpeechFeedbackItemKind.recommendedExpression => '推荐表达',
};

String _repracticeLabel(SpeechFeedbackRepracticeMode mode) => switch (mode) {
  SpeechFeedbackRepracticeMode.sameQuestion => '再答一次',
  SpeechFeedbackRepracticeMode.sameThread => '继续练习',
  SpeechFeedbackRepracticeMode.none => throw StateError(
    'A NONE repractice mode must not render an action.',
  ),
};
