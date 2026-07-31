import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_controller.dart';

class InterviewReportPage extends StatefulWidget {
  const InterviewReportPage({
    required this.practiceSessionId,
    required this.controller,
    this.title = '面试复盘报告',
    super.key,
  });

  final String practiceSessionId;
  final InterviewReportController controller;
  final String title;

  @override
  State<InterviewReportPage> createState() => _InterviewReportPageState();
}

class _InterviewReportPageState extends State<InterviewReportPage> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.controller.load(widget.practiceSessionId));
  }

  @override
  void didUpdateWidget(covariant InterviewReportPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller == widget.controller &&
        oldWidget.practiceSessionId == widget.practiceSessionId) {
      return;
    }
    oldWidget.controller.cancel(oldWidget.practiceSessionId);
    unawaited(widget.controller.load(widget.practiceSessionId));
  }

  @override
  void dispose() {
    widget.controller.cancel(widget.practiceSessionId);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('interview-report-page'),
      appBar: AppBar(title: Text(widget.title)),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(SpeakUpDesign.space16),
          child: InterviewReportPanel(controller: widget.controller),
        ),
      ),
    );
  }
}

class InterviewReportPanel extends StatefulWidget {
  const InterviewReportPanel({required this.controller, super.key});

  final InterviewReportController controller;

  @override
  State<InterviewReportPanel> createState() => _InterviewReportPanelState();
}

class _InterviewReportPanelState extends State<InterviewReportPanel> {
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_rebuild);
  }

  @override
  void didUpdateWidget(covariant InterviewReportPanel oldWidget) {
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
      InterviewReportEvaluationStatus.queued ||
      InterviewReportEvaluationStatus.running => _GeneratingReport(
        message: errorMessage,
        onRetry: controller.canRetry ? controller.retry : null,
      ),
      InterviewReportEvaluationStatus.ready => _ReadyInterviewReport(
        report: envelope.report!,
      ),
      InterviewReportEvaluationStatus.failed => _ReportFailure(
        message: '报告生成遇到技术问题，这不代表你的面试表现较差。',
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
      key: const Key('interview-report-generating'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          children: [
            if (message == null)
              const CircularProgressIndicator(
                key: Key('interview-report-progress'),
              ),
            if (message == null) const SizedBox(height: 14),
            Text(
              message ?? '报告生成中',
              textAlign: TextAlign.center,
              style: message == null
                  ? SpeakUpDesign.cardTitle
                  : const TextStyle(color: SpeakUpDesign.error),
            ),
            if (message == null) ...[
              const SizedBox(height: 6),
              Text(
                '正在整理本次面试的文本证据，请稍候。',
                textAlign: TextAlign.center,
                style: SpeakUpDesign.body,
              ),
            ],
            if (onRetry != null) ...[
              const SizedBox(height: 12),
              OutlinedButton(
                key: const Key('interview-report-retry'),
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
      key: const Key('interview-report-failed'),
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
              retryable ? '可以稍后重新查询。' : '请保留本次练习，稍后再试。',
              textAlign: TextAlign.center,
              style: SpeakUpDesign.meta,
            ),
            if (retryable) ...[
              const SizedBox(height: 12),
              OutlinedButton(
                key: const Key('interview-report-retry'),
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

class _ReadyInterviewReport extends StatelessWidget {
  const _ReadyInterviewReport({required this.report});

  final InterviewReport report;

  @override
  Widget build(BuildContext context) {
    if (report.scoreabilityStatus ==
        InterviewReportScoreabilityStatus.insufficient) {
      return const _InsufficientReport();
    }
    return Column(
      key: const Key('interview-report-ready'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const _ReportNotice(),
        const SizedBox(height: 12),
        _ReportDimensions(report: report),
        const SizedBox(height: 12),
        _ReportQuestions(report: report),
        if (report.priorityActions.isNotEmpty) ...[
          const SizedBox(height: 12),
          _PriorityActions(report: report),
        ],
      ],
    );
  }
}

class _ReportNotice extends StatelessWidget {
  const _ReportNotice();

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('interview-report-notice'),
      color: SpeakUpDesign.primaryMuted,
      child: const Padding(
        padding: EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('暂定文本反馈', style: SpeakUpDesign.cardTitle),
            SizedBox(height: 8),
            Text(
              '本报告只基于本次练习中已确认的文字回答，不评估发音、语速或纯声学流利度。',
              style: SpeakUpDesign.body,
            ),
            SizedBox(height: 6),
            Text(
              '反馈仅用于练习，不代表录用结论或录用概率。',
              key: Key('interview-report-readiness-notice'),
              style: SpeakUpDesign.body,
            ),
          ],
        ),
      ),
    );
  }
}

class _InsufficientReport extends StatelessWidget {
  const _InsufficientReport();

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('interview-report-insufficient'),
      child: const Padding(
        padding: EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('证据不足', style: SpeakUpDesign.cardTitle),
            SizedBox(height: 8),
            Text(
              '本次已确认的文字回答不足以形成可靠反馈。这里不会显示 0 分，也不会补猜发音、流利度或录用结论。',
              style: SpeakUpDesign.body,
            ),
          ],
        ),
      ),
    );
  }
}

class _ReportDimensions extends StatelessWidget {
  const _ReportDimensions({required this.report});

  final InterviewReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('interview-report-dimensions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('五维反馈', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            for (var index = 0; index < report.dimensions.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 16),
                const Divider(height: 1),
                const SizedBox(height: 16),
              ],
              _DimensionFeedback(dimension: report.dimensions[index]),
            ],
          ],
        ),
      ),
    );
  }
}

class _DimensionFeedback extends StatelessWidget {
  const _DimensionFeedback({required this.dimension});

  final InterviewReportDimension dimension;

  @override
  Widget build(BuildContext context) {
    final findings = <({String label, InterviewReportFinding finding})>[
      for (final finding in dimension.strengths)
        (label: '做得好', finding: finding),
      for (final finding in dimension.improvements)
        (label: '可改进', finding: finding),
      for (final finding in dimension.recommendedExpressions)
        (label: '推荐表达', finding: finding),
    ];
    return Column(
      key: Key('interview-report-dimension-${dimension.id.name}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(_dimensionLabel(dimension.id), style: SpeakUpDesign.label),
        if (findings.isEmpty) ...[
          const SizedBox(height: 6),
          Text('现有文本证据不足以形成该维度结论。', style: SpeakUpDesign.meta),
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

class _ReportQuestions extends StatelessWidget {
  const _ReportQuestions({required this.report});

  final InterviewReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('interview-report-questions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('逐题复盘', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            for (var index = 0; index < report.questions.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 16),
                const Divider(height: 1),
                const SizedBox(height: 16),
              ],
              _QuestionFeedback(
                index: index,
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
  const _QuestionFeedback({
    required this.index,
    required this.question,
    required this.report,
  });

  final int index;
  final InterviewReportQuestion question;
  final InterviewReport report;

  @override
  Widget build(BuildContext context) {
    final findingIds = <String>{};
    for (final result in question.dimensionFindings) {
      findingIds.addAll(result.strengthFindingIds);
      findingIds.addAll(result.improvementFindingIds);
      findingIds.addAll(result.recommendedExpressionFindingIds);
    }
    final findings = findingIds
        .map(report.finding)
        .whereType<InterviewReportFinding>()
        .toList(growable: false);
    return Column(
      key: Key('interview-report-question-${question.questionId}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('问题 ${index + 1}', style: SpeakUpDesign.meta),
        const SizedBox(height: 4),
        Text(question.questionText, style: SpeakUpDesign.label),
        const SizedBox(height: 8),
        if (question.confirmedTranscript case final response?) ...[
          Text('你的原回答', style: SpeakUpDesign.meta),
          const SizedBox(height: 3),
          Text(response, style: SpeakUpDesign.body),
        ] else
          Text('本题未提供可确认的回答。', style: SpeakUpDesign.body),
        for (final finding in findings) ...[
          const SizedBox(height: 9),
          Text(finding.message, style: SpeakUpDesign.body),
          if (finding.suggestion case final suggestion?) ...[
            const SizedBox(height: 3),
            Text('建议：$suggestion', style: SpeakUpDesign.body),
          ],
        ],
      ],
    );
  }
}

class _PriorityActions extends StatelessWidget {
  const _PriorityActions({required this.report});

  final InterviewReport report;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('interview-report-priority-actions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('优先改进', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 12),
            for (var index = 0; index < report.priorityActions.length; index++)
              Padding(
                padding: EdgeInsets.only(
                  bottom: index + 1 == report.priorityActions.length ? 0 : 10,
                ),
                child: Text(
                  '${index + 1}. ${_actionText(report, report.priorityActions[index])}',
                  style: SpeakUpDesign.body,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

String _actionText(
  InterviewReport report,
  InterviewReportPriorityAction action,
) {
  final finding = report.finding(action.findingId)!;
  return finding.suggestion ?? finding.message;
}

String _dimensionLabel(InterviewReportDimensionId dimension) =>
    switch (dimension) {
      InterviewReportDimensionId.relevance => '回答相关性',
      InterviewReportDimensionId.structure => '表达结构',
      InterviewReportDimensionId.evidence => '事实与证据',
      InterviewReportDimensionId.professional => '职业表达',
      InterviewReportDimensionId.interaction => '追问互动',
    };
