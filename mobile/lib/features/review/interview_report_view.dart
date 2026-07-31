import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/review/interview_report.dart';
import 'package:speakup/review/interview_report_controller.dart';
import 'package:speakup/review/turn_feedback_controller.dart';
import 'package:speakup/review/turn_feedback_disclosure.dart';

class InterviewReportPage extends StatefulWidget {
  const InterviewReportPage({
    required this.practiceSessionId,
    required this.controller,
    this.title = '面试复盘报告',
    this.speechFeedbackController,
    this.speechFeedbackSourceKeys = const <String>[],
    this.onContinueWithAgent,
    super.key,
  });

  final String practiceSessionId;
  final InterviewReportController controller;
  final String title;
  final SpeechFeedbackController? speechFeedbackController;
  final List<String> speechFeedbackSourceKeys;
  final Future<bool> Function(String reportSummary)? onContinueWithAgent;

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
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (widget.speechFeedbackController != null &&
                  widget.speechFeedbackSourceKeys.isNotEmpty) ...[
                _LanguagePerformancePanel(
                  controller: widget.speechFeedbackController!,
                  sourceKeys: widget.speechFeedbackSourceKeys,
                ),
                const SizedBox(height: SpeakUpDesign.space12),
              ],
              InterviewReportPanel(
                controller: widget.controller,
                onContinueWithAgent: widget.onContinueWithAgent,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class InterviewReportPanel extends StatefulWidget {
  const InterviewReportPanel({
    required this.controller,
    this.onContinueWithAgent,
    super.key,
  });

  final InterviewReportController controller;
  final Future<bool> Function(String reportSummary)? onContinueWithAgent;

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
        onContinueWithAgent: widget.onContinueWithAgent,
      ),
      InterviewReportEvaluationStatus.failed => _ReportFailure(
        message: '报告生成遇到技术问题，这不代表你的面试表现较差。',
        retryable: envelope.stableFailure!.retryable,
        onRetry: controller.retry,
      ),
    };
  }
}

class _LanguagePerformancePanel extends StatefulWidget {
  const _LanguagePerformancePanel({
    required this.controller,
    required this.sourceKeys,
  });

  final SpeechFeedbackController controller;
  final List<String> sourceKeys;

  @override
  State<_LanguagePerformancePanel> createState() =>
      _LanguagePerformancePanelState();
}

class _LanguagePerformancePanelState extends State<_LanguagePerformancePanel> {
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_rebuild);
  }

  @override
  void didUpdateWidget(covariant _LanguagePerformancePanel oldWidget) {
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
    final projections = <SpeechFeedbackProjection>[
      for (final sourceKey in widget.sourceKeys)
        ?widget.controller.projectionFor(sourceKey),
    ];
    return Card(
      key: const Key('interview-report-language-performance'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('语言表现 · 逐轮真实数据', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            const Text(
              '每轮纠错来自确认后的转写文本；发音、语速等声学指标来自该轮实际录音。',
              style: SpeakUpDesign.meta,
            ),
            const SizedBox(height: 14),
            if (projections.isEmpty)
              const Text('逐轮评分仍在准备，请稍候。', style: SpeakUpDesign.body)
            else
              for (var index = 0; index < projections.length; index++) ...[
                if (index > 0) const SizedBox(height: 12),
                Text('第 ${index + 1} 轮', style: SpeakUpDesign.label),
                const SizedBox(height: 6),
                SpeechFeedbackDisclosure(
                  key: ValueKey('interview-report-turn-feedback-$index'),
                  projection: projections[index],
                  onRetry: projections[index].canRetry
                      ? () => widget.controller.retry(
                          projections[index].sourceKey,
                        )
                      : null,
                ),
              ],
          ],
        ),
      ),
    );
  }
}

class _ContinueWithAgentButton extends StatefulWidget {
  const _ContinueWithAgentButton({required this.onPressed});

  final Future<bool> Function() onPressed;

  @override
  State<_ContinueWithAgentButton> createState() =>
      _ContinueWithAgentButtonState();
}

class _ContinueWithAgentButtonState extends State<_ContinueWithAgentButton> {
  bool _busy = false;
  String? _error;

  Future<void> _continue() async {
    if (_busy) {
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    final completed = await widget.onPressed();
    if (!mounted) {
      return;
    }
    if (completed) {
      Navigator.of(context).pop(CompletedPracticeRouteResult.continueWithAgent);
      return;
    }
    setState(() {
      _busy = false;
      _error = '暂时无法回到 Agent，请稍后重试。';
    });
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        FilledButton.icon(
          key: const Key('interview-report-continue-agent'),
          onPressed: _busy ? null : _continue,
          icon: _busy
              ? const SizedBox.square(
                  dimension: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.chat_bubble_outline_rounded),
          label: Text(_busy ? '正在回到原会话…' : '和 Agent 继续复盘'),
        ),
        if (_error != null) ...[
          const SizedBox(height: 8),
          Text(
            _error!,
            style: SpeakUpDesign.meta.copyWith(color: SpeakUpDesign.error),
          ),
        ],
      ],
    );
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
  const _ReadyInterviewReport({required this.report, this.onContinueWithAgent});

  final InterviewReport report;
  final Future<bool> Function(String reportSummary)? onContinueWithAgent;

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
        if (onContinueWithAgent != null) ...[
          const SizedBox(height: 16),
          _ContinueWithAgentButton(
            onPressed: () => onContinueWithAgent!(_agentReportSummary(report)),
          ),
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
            Text('面试能力反馈', style: SpeakUpDesign.cardTitle),
            SizedBox(height: 8),
            Text(
              '基于本次回答，分析回答相关性、结构、说服力、职业表达与追问应对能力。',
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
            Text('五维反馈概览', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 8),
            Text('图形展示本次五维能力得分。', style: SpeakUpDesign.meta),
            const SizedBox(height: 12),
            if (report.dimensions.every((dimension) => dimension.score != null))
              _DimensionRadar(dimensions: report.dimensions)
            else
              Text(
                '部分维度证据不足，暂不绘制雷达图。',
                key: const Key('interview-report-dimension-radar-unavailable'),
                style: SpeakUpDesign.meta,
              ),
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

class _DimensionRadar extends StatelessWidget {
  const _DimensionRadar({required this.dimensions});

  final List<InterviewReportDimension> dimensions;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      key: const Key('interview-report-dimension-radar'),
      width: double.infinity,
      height: 250,
      child: CustomPaint(
        painter: _DimensionRadarPainter(
          values: [for (final dimension in dimensions) dimension.score! / 100],
          labels: [
            for (final dimension in dimensions) _dimensionLabel(dimension.id),
          ],
        ),
      ),
    );
  }
}

class _DimensionRadarPainter extends CustomPainter {
  const _DimensionRadarPainter({required this.values, required this.labels});

  final List<double> values;
  final List<String> labels;

  @override
  void paint(Canvas canvas, Size size) {
    if (values.length != 5 || labels.length != 5) return;
    final center = Offset(size.width / 2, size.height / 2 + 4);
    final radius = math.min(size.width, size.height) * 0.31;
    final grid = Paint()
      ..color = const Color(0xFFD8DEE1)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1;
    final fill = Paint()
      ..color = SpeakUpDesign.primary.withValues(alpha: 0.18)
      ..style = PaintingStyle.fill;
    final stroke = Paint()
      ..color = SpeakUpDesign.primary
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2;

    Offset point(int index, double scale) {
      final angle = -math.pi / 2 + index * math.pi * 2 / 5;
      return center + Offset(math.cos(angle), math.sin(angle)) * radius * scale;
    }

    Path polygon(double scale) {
      final path = Path()..moveTo(point(0, scale).dx, point(0, scale).dy);
      for (var index = 1; index < 5; index++) {
        path.lineTo(point(index, scale).dx, point(index, scale).dy);
      }
      return path..close();
    }

    for (final scale in const [0.25, 0.5, 0.75, 1.0]) {
      canvas.drawPath(polygon(scale), grid);
    }
    for (var index = 0; index < 5; index++) {
      canvas.drawLine(center, point(index, 1), grid);
    }

    final result = Path();
    for (var index = 0; index < 5; index++) {
      final value = values[index].clamp(0.0, 1.0);
      final current = point(index, value);
      if (index == 0) {
        result.moveTo(current.dx, current.dy);
      } else {
        result.lineTo(current.dx, current.dy);
      }
    }
    result.close();
    canvas.drawPath(result, fill);
    canvas.drawPath(result, stroke);

    for (var index = 0; index < 5; index++) {
      final anchor = point(index, 1.28);
      final text = TextPainter(
        text: TextSpan(
          text: labels[index],
          style: const TextStyle(
            color: Color(0xFF4F565A),
            fontSize: 12,
            fontWeight: FontWeight.w600,
          ),
        ),
        textDirection: TextDirection.ltr,
      )..layout();
      text.paint(canvas, anchor - Offset(text.width / 2, text.height / 2));
    }
  }

  @override
  bool shouldRepaint(covariant _DimensionRadarPainter oldDelegate) =>
      oldDelegate.values != values || oldDelegate.labels != labels;
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
        Text(
          dimension.score == null
              ? '${_dimensionLabel(dimension.id)} · 未评估'
              : '${_dimensionLabel(dimension.id)} · ${dimension.score} / 100',
          style: SpeakUpDesign.label,
        ),
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

String _agentReportSummary(InterviewReport report) {
  final lines = <String>['练习报告摘要'];
  for (final dimension in report.dimensions) {
    if (dimension.strengths.firstOrNull case final finding?) {
      lines.add('优势：${finding.message}');
      break;
    }
  }
  for (final action in report.priorityActions.take(2)) {
    lines.add(
      '待提升（${_dimensionLabel(action.dimensionId)}）：${report.finding(action.findingId)!.message}',
    );
  }
  return lines.join('\n');
}
