import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';

class IeltsSpeakingReportPanel extends StatefulWidget {
  const IeltsSpeakingReportPanel({required this.controller, super.key});

  final IeltsSpeakingReportController controller;

  @override
  State<IeltsSpeakingReportPanel> createState() =>
      _IeltsSpeakingReportPanelState();
}

class _IeltsSpeakingReportPanelState extends State<IeltsSpeakingReportPanel> {
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_rebuild);
  }

  @override
  void didUpdateWidget(covariant IeltsSpeakingReportPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller.removeListener(_rebuild);
      widget.controller.addListener(_rebuild);
    }
  }

  @override
  void dispose() {
    widget.controller.removeListener(_rebuild);
    super.dispose();
  }

  void _rebuild() {
    if (mounted) {
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final envelope = controller.envelope;
    final errorMessage = controller.errorMessage;
    if (envelope == null) {
      if (errorMessage != null) {
        return _ReportFailure(
          message: errorMessage,
          retryable: controller.canRetry,
          onRetry: controller.retry,
        );
      }
      return const _GeneratingReport();
    }
    return switch (envelope.evaluationStatus) {
      IeltsSpeakingReportEvaluationStatus.queued ||
      IeltsSpeakingReportEvaluationStatus.running => _GeneratingReport(
        message: errorMessage,
        onRetry: controller.canRetry ? controller.retry : null,
      ),
      IeltsSpeakingReportEvaluationStatus.ready => _ReadyReport(
        report: envelope.report!,
      ),
      IeltsSpeakingReportEvaluationStatus.failed => _ReportFailure(
        message: '报告生成遇到技术问题，这不代表你的 IELTS 口语表现较差。',
        retryable: envelope.stableFailure!.retryable,
        onRetry: controller.retry,
      ),
    };
  }
}

class _GeneratingReport extends StatelessWidget {
  const _GeneratingReport({this.message, this.onRetry});

  final String? message;
  final Future<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-generating'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          children: [
            if (message == null)
              const CircularProgressIndicator(
                key: Key('ielts-speaking-report-progress'),
              ),
            if (message == null) const SizedBox(height: 14),
            Text(
              message ?? 'IELTS 练习报告生成中',
              textAlign: TextAlign.center,
              style: message == null
                  ? SpeakUpDesign.cardTitle
                  : const TextStyle(color: SpeakUpDesign.error),
            ),
            if (message == null) ...[
              const SizedBox(height: 6),
              Text(
                '答题已经完成，正在整理 14 道题的文本证据。',
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body,
              ),
            ],
            if (onRetry != null) ...[
              const SizedBox(height: 12),
              OutlinedButton(
                key: const Key('ielts-speaking-report-retry'),
                onPressed: onRetry,
                child: const Text('重新查询'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ReportFailure extends StatelessWidget {
  const _ReportFailure({
    required this.message,
    required this.retryable,
    required this.onRetry,
  });

  final String message;
  final bool retryable;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-failed'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          children: [
            const Icon(Icons.error_outline_rounded, color: SpeakUpDesign.error),
            const SizedBox(height: 10),
            Text(
              message,
              textAlign: TextAlign.center,
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: 6),
            Text(
              retryable ? '可以稍后重新查询。' : '请保留本次模考，稍后再试。',
              textAlign: TextAlign.center,
              style: SpeakUpDesign.meta,
            ),
            if (retryable) ...[
              const SizedBox(height: 12),
              OutlinedButton(
                key: const Key('ielts-speaking-report-retry'),
                onPressed: onRetry,
                child: const Text('重新查询'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ReadyReport extends StatelessWidget {
  const _ReadyReport({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    if (report.scoreabilityStatus ==
        IeltsSpeakingReportScoreabilityStatus.insufficient) {
      return _InsufficientReport(disclaimer: report.disclaimer);
    }
    return Column(
      key: const Key('ielts-speaking-report-ready'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _ReportNotice(disclaimer: report.disclaimer),
        const SizedBox(height: 12),
        const _OverallUnavailable(),
        const SizedBox(height: 12),
        _Criteria(report: report),
        const SizedBox(height: 12),
        _PartReviews(report: report),
        const SizedBox(height: 12),
        _QuestionReviews(report: report),
        const SizedBox(height: 12),
        _TargetPlan(report: report),
      ],
    );
  }
}

class _ReportNotice extends StatelessWidget {
  const _ReportNotice({required this.disclaimer});

  final String disclaimer;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-notice'),
      color: SpeakUpDesign.primaryMuted,
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('部分练习报告', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 8),
            const Text(
              '词汇与语法可显示基于已确认文字的暂定整数 Band；流利与连贯只显示定性反馈。',
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: 6),
            const Text(
              '当前没有可信发音工件，因此不评估发音，也不计算 Speaking Overall。',
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: 6),
            Text(
              disclaimer,
              key: const Key('ielts-speaking-report-disclaimer'),
              style: SpeakUpDesign.body,
            ),
          ],
        ),
      ),
    );
  }
}

class _InsufficientReport extends StatelessWidget {
  const _InsufficientReport({required this.disclaimer});

  final String disclaimer;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-insufficient'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('证据不足', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 8),
            const Text(
              '本次已确认的回答不足以形成练习估分。这里不会显示 0 分，也不会补猜发音或 Overall。',
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: 6),
            Text(disclaimer, style: SpeakUpDesign.meta),
          ],
        ),
      ),
    );
  }
}

class _OverallUnavailable extends StatelessWidget {
  const _OverallUnavailable();

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-overall-unavailable'),
      child: const Padding(
        padding: EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Speaking Overall', style: SpeakUpDesign.cardTitle),
            SizedBox(height: 6),
            Text('暂不可用', style: SpeakUpDesign.body),
            SizedBox(height: 4),
            Text('四项标准尚未全部具备可靠证据，因此不计算总分。', style: SpeakUpDesign.meta),
          ],
        ),
      ),
    );
  }
}

class _Criteria extends StatelessWidget {
  const _Criteria({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-criteria'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('评分标准', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            for (var index = 0; index < report.criteria.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 16),
                const Divider(height: 1),
                const SizedBox(height: 16),
              ],
              _CriterionFeedback(criterion: report.criteria[index]),
            ],
          ],
        ),
      ),
    );
  }
}

class _CriterionFeedback extends StatelessWidget {
  const _CriterionFeedback({required this.criterion});

  final IeltsSpeakingCriterion criterion;

  @override
  Widget build(BuildContext context) {
    final findings = <({String label, IeltsSpeakingFinding finding})>[
      for (final finding in criterion.strengths)
        (label: '做得好', finding: finding),
      for (final finding in criterion.improvements)
        (label: '可改进', finding: finding),
      for (final finding in criterion.upgradeExamples)
        (label: '提升表达', finding: finding),
    ];
    return Column(
      key: Key('ielts-speaking-criterion-${criterion.id.name}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(_criterionLabel(criterion.id), style: SpeakUpDesign.label),
        const SizedBox(height: 5),
        if (criterion.estimatedBand case final band?)
          Text(
            '练习估分 Band $band（暂定）',
            key: Key('ielts-speaking-band-${criterion.id.name}'),
            style: SpeakUpDesign.cardTitle,
          )
        else
          Text(_unscoredCriterionLabel(criterion), style: SpeakUpDesign.meta),
        if (criterion.bandDescriptor case final descriptor?) ...[
          const SizedBox(height: 4),
          Text(descriptor, style: SpeakUpDesign.body),
        ],
        for (final item in findings) ...[
          const SizedBox(height: 10),
          Text(item.label, style: SpeakUpDesign.meta),
          const SizedBox(height: 3),
          Text(item.finding.message, style: SpeakUpDesign.body),
          if (item.finding.suggestion case final suggestion?) ...[
            const SizedBox(height: 3),
            Text('建议：$suggestion', style: SpeakUpDesign.body),
          ],
          for (final evidence in item.finding.evidence) ...[
            const SizedBox(height: 5),
            Text('原句：“${evidence.originalExcerpt}”', style: SpeakUpDesign.meta),
          ],
        ],
      ],
    );
  }
}

class _PartReviews extends StatelessWidget {
  const _PartReviews({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-parts'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('分 Part 复盘', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            Text('Part 级只汇总反馈，不显示 Band 或平均分。', style: SpeakUpDesign.meta),
            const SizedBox(height: 14),
            for (var index = 0; index < report.partReviews.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 14),
                const Divider(height: 1),
                const SizedBox(height: 14),
              ],
              _PartFeedback(part: report.partReviews[index], report: report),
            ],
          ],
        ),
      ),
    );
  }
}

class _PartFeedback extends StatelessWidget {
  const _PartFeedback({required this.part, required this.report});

  final IeltsSpeakingPartReview part;
  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    final ids = <String>[
      ...part.strengthFindingIds,
      ...part.improvementFindingIds,
      ...part.upgradeExampleFindingIds,
    ];
    return Column(
      key: Key('ielts-speaking-part-${part.id.name}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(_partLabel(part.id), style: SpeakUpDesign.label),
        const SizedBox(height: 4),
        Text(
          '题目 ${_questionRange(part.questionIndexes)}',
          style: SpeakUpDesign.meta,
        ),
        if (ids.isEmpty) ...[
          const SizedBox(height: 6),
          Text('本 Part 暂无额外结论。', style: SpeakUpDesign.body),
        ],
        for (final id in ids) ...[
          const SizedBox(height: 6),
          Text(report.finding(id)!.message, style: SpeakUpDesign.body),
        ],
      ],
    );
  }
}

class _QuestionReviews extends StatelessWidget {
  const _QuestionReviews({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-questions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('14 题复盘', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            Text('逐题只展示证据与建议，不显示题级分数。', style: SpeakUpDesign.meta),
            const SizedBox(height: 14),
            for (var index = 0; index < report.questions.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 14),
                const Divider(height: 1),
                const SizedBox(height: 14),
              ],
              _QuestionFeedback(
                question: report.questions[index],
                report: report,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _QuestionFeedback extends StatelessWidget {
  const _QuestionFeedback({required this.question, required this.report});

  final IeltsSpeakingQuestionReview question;
  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    final ids = <String>{};
    for (final result in question.criterionFindings) {
      ids.addAll(result.strengthFindingIds);
      ids.addAll(result.improvementFindingIds);
      ids.addAll(result.upgradeExampleFindingIds);
    }
    return Column(
      key: Key('ielts-speaking-question-${question.index}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '${_partLabel(question.partId)} · 第 ${question.index} 题',
          style: SpeakUpDesign.meta,
        ),
        const SizedBox(height: 4),
        Text(question.questionText, style: SpeakUpDesign.label),
        const SizedBox(height: 7),
        if (question.confirmedTranscript case final response?) ...[
          Text('你的原回答', style: SpeakUpDesign.meta),
          const SizedBox(height: 3),
          Text(response, style: SpeakUpDesign.body),
        ] else
          Text('本题未提供可确认的回答。', style: SpeakUpDesign.body),
        for (final id in ids) ...[
          const SizedBox(height: 8),
          Text(report.finding(id)!.message, style: SpeakUpDesign.body),
          if (report.finding(id)!.suggestion case final suggestion?) ...[
            const SizedBox(height: 3),
            Text('建议：$suggestion', style: SpeakUpDesign.body),
          ],
        ],
      ],
    );
  }
}

class _TargetPlan extends StatelessWidget {
  const _TargetPlan({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-target-not-configured'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('优先练习建议', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            Text('尚未配置目标 Band，因此不显示“距目标差值”。', style: SpeakUpDesign.meta),
            if (report.priorityActions.isEmpty) ...[
              const SizedBox(height: 10),
              Text('现有证据不足以生成优先建议。', style: SpeakUpDesign.body),
            ],
            for (
              var index = 0;
              index < report.priorityActions.length;
              index++
            ) ...[
              const SizedBox(height: 10),
              Text(
                '${index + 1}. ${_actionText(report, report.priorityActions[index])}',
                style: SpeakUpDesign.body,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

String _actionText(
  IeltsSpeakingReport report,
  IeltsSpeakingPriorityAction action,
) {
  final finding = report.finding(action.findingId)!;
  return finding.suggestion ?? finding.message;
}

String _unscoredCriterionLabel(IeltsSpeakingCriterion criterion) {
  if (criterion.id == IeltsSpeakingCriterionId.pronunciation) {
    return '未评估：缺少可信发音工件';
  }
  if (criterion.scoreabilityStatus ==
      IeltsSpeakingReportScoreabilityStatus.insufficient) {
    return '证据不足，不显示 Band';
  }
  return '定性反馈，不提供完整 Band';
}

String _criterionLabel(IeltsSpeakingCriterionId criterion) =>
    switch (criterion) {
      IeltsSpeakingCriterionId.fluencyAndCoherence =>
        'Fluency and Coherence (FC)',
      IeltsSpeakingCriterionId.lexicalResource => 'Lexical Resource (LR)',
      IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy =>
        'Grammatical Range and Accuracy (GRA)',
      IeltsSpeakingCriterionId.pronunciation => 'Pronunciation (PR)',
    };

String _partLabel(IeltsSpeakingPartId part) => switch (part) {
  IeltsSpeakingPartId.part1 => 'Part 1',
  IeltsSpeakingPartId.part2 => 'Part 2',
  IeltsSpeakingPartId.part3 => 'Part 3',
};

String _questionRange(List<int> indexes) => indexes.length == 1
    ? '${indexes.single}'
    : '${indexes.first}–${indexes.last}';
