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
        _ReportHeader(report: report),
        const SizedBox(height: SpeakUpDesign.space16),
        _OverallScore(report: report),
        const SizedBox(height: SpeakUpDesign.space16),
        _ScoreOverview(report: report),
        const SizedBox(height: SpeakUpDesign.space24),
        const _ReportSectionTitle(title: '评分描述'),
        const SizedBox(height: SpeakUpDesign.space12),
        for (final criterion in report.criteria) ...[
          _CriterionFeedback(criterion: criterion),
          const SizedBox(height: SpeakUpDesign.space12),
        ],
        const SizedBox(height: SpeakUpDesign.space12),
        const _ReportSectionTitle(title: '改进建议'),
        const SizedBox(height: SpeakUpDesign.space12),
        _TargetPlan(report: report),
        const SizedBox(height: SpeakUpDesign.space24),
        _ReportNotice(disclaimer: report.disclaimer),
        const SizedBox(height: SpeakUpDesign.space12),
        _PartReviews(report: report),
        const SizedBox(height: SpeakUpDesign.space12),
        _QuestionReviews(report: report),
      ],
    );
  }
}

class _ReportHeader extends StatelessWidget {
  const _ReportHeader({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    final summary = report.testSummary;
    return Semantics(
      container: true,
      label: 'IELTS 口语模考报告摘要',
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: SpeakUpDesign.primaryMuted,
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusMedia),
        ),
        child: Padding(
          padding: const EdgeInsets.all(SpeakUpDesign.space20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'IELTS Speaking',
                style: SpeakUpDesign.pageTitle.copyWith(
                  color: SpeakUpDesign.primary,
                  fontStyle: FontStyle.italic,
                ),
              ),
              Text(
                'Test Report',
                style: SpeakUpDesign.pageTitle.copyWith(
                  color: const Color(0xFF2A6D7E),
                  fontStyle: FontStyle.italic,
                ),
              ),
              const SizedBox(height: SpeakUpDesign.space20),
              _SummaryLine(label: 'Part 1', value: summary.part1Topic),
              const SizedBox(height: SpeakUpDesign.space8),
              _SummaryLine(label: 'Part 2', value: summary.part2Topic),
              const SizedBox(height: SpeakUpDesign.space8),
              _SummaryLine(label: 'Part 3', value: summary.part3Topic),
              const SizedBox(height: SpeakUpDesign.space12),
              Text(
                '共 ${summary.answeredCount}/${summary.questionCount} 题 · '
                '录音 ${_durationLabel(summary.recordingDurationMs)}',
                style: SpeakUpDesign.meta,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SummaryLine extends StatelessWidget {
  const _SummaryLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => Text.rich(
    TextSpan(
      style: SpeakUpDesign.body.copyWith(color: SpeakUpDesign.ink),
      children: [
        TextSpan(
          text: '$label: ',
          style: const TextStyle(fontWeight: FontWeight.w700),
        ),
        TextSpan(text: value),
      ],
    ),
    maxLines: 2,
    overflow: TextOverflow.ellipsis,
  );
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

class _OverallScore extends StatelessWidget {
  const _OverallScore({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    final band = report.speakingOverallBand;
    return Container(
      key: const Key('ielts-speaking-overall-unavailable'),
      decoration: BoxDecoration(
        gradient: const LinearGradient(
          colors: [SpeakUpDesign.primary, Color(0xFF2A6D7E)],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusMedia),
      ),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '口语总分',
              style: SpeakUpDesign.cardTitle.copyWith(color: Colors.white),
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            Text(
              band == null ? '暂不可用' : _bandLabel(band),
              style: SpeakUpDesign.pageTitle.copyWith(
                color: Colors.white,
                fontSize: band == null ? 26 : 48,
              ),
            ),
            const SizedBox(height: SpeakUpDesign.space12),
            Container(
              padding: const EdgeInsets.all(SpeakUpDesign.space16),
              decoration: BoxDecoration(
                color: Colors.white.withValues(alpha: 0.94),
                borderRadius: BorderRadius.circular(
                  SpeakUpDesign.radiusControl,
                ),
              ),
              child: Text(
                report.speakingOverallExplanation,
                style: SpeakUpDesign.body.copyWith(color: SpeakUpDesign.ink),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ScoreOverview extends StatelessWidget {
  const _ScoreOverview({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-criteria'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('四项评分', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: SpeakUpDesign.space16),
            GridView.builder(
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                crossAxisCount: 2,
                mainAxisSpacing: SpeakUpDesign.space12,
                crossAxisSpacing: SpeakUpDesign.space12,
                childAspectRatio: 1.25,
              ),
              itemCount: report.criteria.length,
              itemBuilder: (context, index) =>
                  _ScoreTile(criterion: report.criteria[index]),
            ),
          ],
        ),
      ),
    );
  }
}

class _ScoreTile extends StatelessWidget {
  const _ScoreTile({required this.criterion});

  final IeltsSpeakingCriterion criterion;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(SpeakUpDesign.space12),
    decoration: BoxDecoration(
      color: _criterionColor(criterion.id).withValues(alpha: 0.1),
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
    ),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Icon(
          _criterionIcon(criterion.id),
          color: _criterionColor(criterion.id),
          size: 22,
        ),
        Text(_criterionChineseLabel(criterion.id), style: SpeakUpDesign.label),
        Text(
          criterion.estimatedBand?.toString() ?? '--',
          style: SpeakUpDesign.pageTitle.copyWith(
            color: _criterionColor(criterion.id),
            fontSize: 30,
          ),
        ),
      ],
    ),
  );
}

class _ReportSectionTitle extends StatelessWidget {
  const _ReportSectionTitle({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) =>
      Text(title, style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 24));
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
    return Card(
      key: Key('ielts-speaking-criterion-${criterion.id.name}'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: _criterionColor(
                      criterion.id,
                    ).withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(
                    _criterionIcon(criterion.id),
                    color: _criterionColor(criterion.id),
                  ),
                ),
                const SizedBox(width: SpeakUpDesign.space12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _criterionChineseLabel(criterion.id),
                        style: SpeakUpDesign.cardTitle,
                      ),
                      Text(
                        _criterionEnglishLabel(criterion.id),
                        style: SpeakUpDesign.meta,
                      ),
                    ],
                  ),
                ),
                if (criterion.estimatedBand case final band?)
                  Text(
                    '$band',
                    key: Key('ielts-speaking-band-${criterion.id.name}'),
                    style: SpeakUpDesign.pageTitle.copyWith(
                      color: _criterionColor(criterion.id),
                      fontSize: 38,
                    ),
                  )
                else
                  Text(
                    '--',
                    style: SpeakUpDesign.pageTitle.copyWith(
                      color: SpeakUpDesign.tertiary,
                    ),
                  ),
              ],
            ),
            const SizedBox(height: SpeakUpDesign.space16),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(SpeakUpDesign.space16),
              decoration: BoxDecoration(
                color: SpeakUpDesign.canvas,
                borderRadius: BorderRadius.circular(
                  SpeakUpDesign.radiusControl,
                ),
                border: Border.all(color: SpeakUpDesign.border),
              ),
              child: Text(criterion.explanation, style: SpeakUpDesign.body),
            ),
            if (findings.isEmpty) ...[
              const SizedBox(height: SpeakUpDesign.space12),
              Text(
                _unscoredCriterionLabel(criterion),
                style: SpeakUpDesign.meta,
              ),
            ],
            for (final item in findings) ...[
              const SizedBox(height: SpeakUpDesign.space12),
              Text(item.label, style: SpeakUpDesign.label),
              const SizedBox(height: SpeakUpDesign.space4),
              Text(item.finding.message, style: SpeakUpDesign.body),
              if (item.finding.suggestion case final suggestion?) ...[
                const SizedBox(height: SpeakUpDesign.space4),
                Text('建议：$suggestion', style: SpeakUpDesign.body),
              ],
              for (final evidence in item.finding.evidence) ...[
                const SizedBox(height: SpeakUpDesign.space4),
                Text(
                  '原句：“${evidence.originalExcerpt}”',
                  style: SpeakUpDesign.meta,
                ),
              ],
            ],
          ],
        ),
      ),
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

String _criterionChineseLabel(IeltsSpeakingCriterionId criterion) =>
    switch (criterion) {
      IeltsSpeakingCriterionId.fluencyAndCoherence => '流利性与连贯性',
      IeltsSpeakingCriterionId.lexicalResource => '词汇丰富度',
      IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy => '语法多样性及准确性',
      IeltsSpeakingCriterionId.pronunciation => '发音',
    };

String _criterionEnglishLabel(IeltsSpeakingCriterionId criterion) =>
    switch (criterion) {
      IeltsSpeakingCriterionId.fluencyAndCoherence => 'Fluency and Coherence',
      IeltsSpeakingCriterionId.lexicalResource => 'Lexical Resource',
      IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy =>
        'Grammatical Range and Accuracy',
      IeltsSpeakingCriterionId.pronunciation => 'Pronunciation',
    };

Color _criterionColor(IeltsSpeakingCriterionId criterion) =>
    switch (criterion) {
      IeltsSpeakingCriterionId.fluencyAndCoherence => const Color(0xFF7651C8),
      IeltsSpeakingCriterionId.lexicalResource => const Color(0xFFDA633B),
      IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy => const Color(
        0xFF3E8A5B,
      ),
      IeltsSpeakingCriterionId.pronunciation => const Color(0xFF3478C8),
    };

IconData _criterionIcon(IeltsSpeakingCriterionId criterion) =>
    switch (criterion) {
      IeltsSpeakingCriterionId.fluencyAndCoherence =>
        Icons.auto_stories_rounded,
      IeltsSpeakingCriterionId.lexicalResource => Icons.translate_rounded,
      IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy =>
        Icons.task_alt_rounded,
      IeltsSpeakingCriterionId.pronunciation => Icons.record_voice_over_rounded,
    };

String _durationLabel(int durationMs) {
  final totalSeconds = durationMs ~/ 1000;
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes分${seconds.toString().padLeft(2, '0')}秒';
}

String _bandLabel(double band) => band == band.roundToDouble()
    ? band.toInt().toString()
    : band.toStringAsFixed(1);

String _partLabel(IeltsSpeakingPartId part) => switch (part) {
  IeltsSpeakingPartId.part1 => 'Part 1',
  IeltsSpeakingPartId.part2 => 'Part 2',
  IeltsSpeakingPartId.part3 => 'Part 3',
};

String _questionRange(List<int> indexes) => indexes.length == 1
    ? '${indexes.single}'
    : '${indexes.first}-${indexes.last}';
