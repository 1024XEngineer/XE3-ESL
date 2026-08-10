import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';

const _abilityBlue = Color(0xFF2563EB);

class IeltsSpeakingSessionReportPanel extends StatefulWidget {
  const IeltsSpeakingSessionReportPanel({
    required this.practiceSessionId,
    required this.controller,
    super.key,
  });

  final String practiceSessionId;
  final IeltsSpeakingReportController controller;

  @override
  State<IeltsSpeakingSessionReportPanel> createState() =>
      _IeltsSpeakingSessionReportPanelState();
}

class _IeltsSpeakingSessionReportPanelState
    extends State<IeltsSpeakingSessionReportPanel> {
  @override
  void initState() {
    super.initState();
    _loadCurrentSession();
  }

  @override
  void didUpdateWidget(covariant IeltsSpeakingSessionReportPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller == widget.controller &&
        oldWidget.practiceSessionId == widget.practiceSessionId) {
      return;
    }
    oldWidget.controller.cancel(oldWidget.practiceSessionId);
    _loadCurrentSession();
  }

  void _loadCurrentSession() {
    if (widget.controller.practiceSessionId != widget.practiceSessionId) {
      unawaited(widget.controller.load(widget.practiceSessionId));
    }
  }

  @override
  void dispose() {
    widget.controller.cancel(widget.practiceSessionId);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      IeltsSpeakingReportPanel(controller: widget.controller);
}

class IeltsSpeakingReportPanel extends StatefulWidget {
  const IeltsSpeakingReportPanel({
    required this.controller,
    this.onRepracticeQuestion,
    super.key,
  });

  final IeltsSpeakingReportController controller;
  final Future<bool> Function(IeltsSpeakingQuestionReview question)?
  onRepracticeQuestion;

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
    if (controller.canRetry && controller.errorMessage != null) {
      return _ReportFailure(
        message: controller.errorMessage!,
        onRetry: controller.retry,
      );
    }
    final envelope = controller.envelope;
    if (envelope == null) {
      return const _GeneratingReport();
    }
    return switch (envelope.evaluationStatus) {
      IeltsSpeakingReportEvaluationStatus.queued ||
      IeltsSpeakingReportEvaluationStatus.running => const _GeneratingReport(),
      IeltsSpeakingReportEvaluationStatus.ready => _ReadyReport(
        report: envelope.report!,
        onRepracticeQuestion: widget.onRepracticeQuestion,
      ),
      IeltsSpeakingReportEvaluationStatus.failed => const _GeneratingReport(
        message: '报告正在自动恢复，无需重新操作',
      ),
    };
  }
}

class _ReportFailure extends StatelessWidget {
  const _ReportFailure({required this.message, required this.onRetry});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-failed'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          children: [
            const Icon(Icons.error_outline_rounded, size: 48),
            const SizedBox(height: 12),
            const Text('报告暂时无法生成', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 8),
            Text(
              message,
              textAlign: TextAlign.center,
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              key: const Key('ielts-speaking-report-retry'),
              onPressed: () => unawaited(onRetry()),
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重新生成报告'),
            ),
          ],
        ),
      ),
    );
  }
}

class _GeneratingReport extends StatelessWidget {
  const _GeneratingReport({this.message});

  final String? message;

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
              style: SpeakUpDesign.cardTitle,
            ),
            const SizedBox(height: 6),
            Text(
              message == null
                  ? '答题已经完成，系统正在整理评分证据。完成后会自动显示报告。'
                  : '本次模考已安全保存，系统会在后台继续处理。',
              textAlign: TextAlign.center,
              style: SpeakUpDesign.body,
            ),
          ],
        ),
      ),
    );
  }
}

class _ReadyReport extends StatelessWidget {
  const _ReadyReport({required this.report, this.onRepracticeQuestion});

  final IeltsSpeakingReport report;
  final Future<bool> Function(IeltsSpeakingQuestionReview question)?
  onRepracticeQuestion;

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
        IeltsSpeakingScoreOverview(
          key: const Key('ielts-speaking-report-criteria'),
          criteria: report.criteria,
        ),
        const SizedBox(height: SpeakUpDesign.space12),
        _EvidenceStandard(report: report),
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
        _QuestionReviews(
          report: report,
          onRepracticeQuestion: onRepracticeQuestion,
        ),
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
              _SummaryLine(label: 'Part 2&3', value: summary.part2Topic),
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
      key: Key(
        band == null
            ? 'ielts-speaking-overall-unavailable'
            : 'ielts-speaking-overall-available',
      ),
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

class IeltsSpeakingScoreOverview extends StatelessWidget {
  const IeltsSpeakingScoreOverview({
    required this.criteria,
    this.title = '四项评分',
    this.radarSemanticsKey = const Key('ielts-speaking-score-radar'),
    super.key,
  });

  final List<IeltsSpeakingCriterion> criteria;
  final String title;
  final Key radarSemanticsKey;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: SpeakUpDesign.cardTitle),
            const SizedBox(height: SpeakUpDesign.space8),
            Text('0-9 分练习估分 · 图形越靠外代表该维度表现越强', style: SpeakUpDesign.meta),
            const SizedBox(height: SpeakUpDesign.space16),
            IeltsSpeakingScoreRadar(
              criteria: criteria,
              semanticsKey: radarSemanticsKey,
            ),
          ],
        ),
      ),
    );
  }
}

class IeltsSpeakingAbilityProfile extends StatelessWidget {
  const IeltsSpeakingAbilityProfile({
    required this.report,
    required this.loading,
    super.key,
  });

  final IeltsSpeakingReport? report;
  final bool loading;

  @override
  Widget build(BuildContext context) {
    final value = report;
    final criteria = value?.criteria;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('个人能力', style: SpeakUpDesign.sectionTitle),
            const SizedBox(height: SpeakUpDesign.space16),
            if (criteria == null)
              _AbilityEmptyState(
                key: const Key('review-ability-empty'),
                loading: loading,
              )
            else
              Center(
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 380),
                  child: IeltsSpeakingScoreRadar(
                    criteria: criteria,
                    semanticsKey: const Key('review-ability-radar'),
                    height: 292,
                    profileMode: true,
                  ),
                ),
              ),
            if (value != null) ...[
              const SizedBox(height: SpeakUpDesign.space12),
              _AbilitySummary(report: value),
            ],
          ],
        ),
      ),
    );
  }
}

class _AbilitySummary extends StatelessWidget {
  const _AbilitySummary({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    final band = report.speakingOverallBand;
    return Container(
      key: const Key('review-ability-summary'),
      width: double.infinity,
      padding: const EdgeInsets.symmetric(
        horizontal: SpeakUpDesign.space16,
        vertical: SpeakUpDesign.space12,
      ),
      decoration: BoxDecoration(
        color: SpeakUpDesign.canvas,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
      ),
      child: Row(
        children: [
          Text('综合得分', style: SpeakUpDesign.meta),
          const SizedBox(width: SpeakUpDesign.space8),
          Text(
            band?.toStringAsFixed(1) ?? '--',
            key: const Key('review-ability-overall-band'),
            style: SpeakUpDesign.sectionTitle.copyWith(
              color: _abilityBlue,
              fontSize: 28,
              height: 1,
            ),
          ),
          const SizedBox(width: SpeakUpDesign.space12),
          Container(width: 1, height: 28, color: SpeakUpDesign.border),
          const SizedBox(width: SpeakUpDesign.space12),
          Expanded(
            child: Text(
              report.speakingOverallExplanation,
              key: const Key('review-ability-summary-text'),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: SpeakUpDesign.meta,
            ),
          ),
        ],
      ),
    );
  }
}

class _AbilityEmptyState extends StatelessWidget {
  const _AbilityEmptyState({required this.loading, super.key});

  final bool loading;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.symmetric(vertical: SpeakUpDesign.space8),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            loading ? '正在读取能力数据' : '完成一次 IELTS 口语完整模考',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.cardTitle,
          ),
          const SizedBox(height: SpeakUpDesign.space4),
          Text(
            loading ? '四项练习估分将在这里呈现' : '获得流利度、词汇、语法和发音四维能力图',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.meta,
          ),
        ],
      ),
    ),
  );
}

class IeltsSpeakingScoreRadar extends StatelessWidget {
  const IeltsSpeakingScoreRadar({
    required this.criteria,
    this.semanticsKey = const Key('ielts-speaking-score-radar'),
    this.height = 300,
    this.profileMode = false,
    super.key,
  });

  final List<IeltsSpeakingCriterion> criteria;
  final Key semanticsKey;
  final double height;
  final bool profileMode;

  @override
  Widget build(BuildContext context) {
    final byId = {for (final item in criteria) item.id: item};
    final ordered = [
      byId[IeltsSpeakingCriterionId.fluencyAndCoherence],
      byId[IeltsSpeakingCriterionId.pronunciation],
      byId[IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy],
      byId[IeltsSpeakingCriterionId.lexicalResource],
    ];
    final values = ordered
        .map((item) => (item?.estimatedBand ?? 0).toDouble())
        .toList(growable: false);
    final semanticLabel = ordered
        .whereType<IeltsSpeakingCriterion>()
        .map(
          (item) =>
              '${_criterionChineseLabel(item.id)} ${item.estimatedBand ?? '未评分'}分',
        )
        .join('，');
    return Semantics(
      key: semanticsKey,
      label: 'IELTS 口语四维雷达图，$semanticLabel',
      child: SizedBox(
        height: height,
        child: Stack(
          alignment: Alignment.center,
          children: [
            Positioned.fill(
              child: Padding(
                padding: const EdgeInsets.all(48),
                child: CustomPaint(
                  painter: _RadarPainter(values, emphasized: profileMode),
                ),
              ),
            ),
            _radarLabel(
              alignment: Alignment.topCenter,
              label: '流利与连贯',
              score: ordered[0]?.estimatedBand,
            ),
            _radarLabel(
              alignment: Alignment.centerRight,
              label: '发音',
              score: ordered[1]?.estimatedBand,
            ),
            _radarLabel(
              alignment: Alignment.bottomCenter,
              label: '语法',
              score: ordered[2]?.estimatedBand,
            ),
            _radarLabel(
              alignment: Alignment.centerLeft,
              label: '词汇',
              score: ordered[3]?.estimatedBand,
            ),
          ],
        ),
      ),
    );
  }

  Widget _radarLabel({
    required Alignment alignment,
    required String label,
    required int? score,
  }) {
    if (!profileMode) {
      return _RadarLabel(alignment: alignment, label: label, score: score);
    }
    return _AbilityRadarLabel(alignment: alignment, label: label, score: score);
  }
}

class _RadarLabel extends StatelessWidget {
  const _RadarLabel({required this.alignment, required this.label, this.score});

  final Alignment alignment;
  final String label;
  final int? score;

  @override
  Widget build(BuildContext context) => Align(
    alignment: alignment,
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(label, style: SpeakUpDesign.label),
        Text(
          score?.toString() ?? '--',
          style: SpeakUpDesign.cardTitle.copyWith(color: SpeakUpDesign.primary),
        ),
      ],
    ),
  );
}

class _AbilityRadarLabel extends StatelessWidget {
  const _AbilityRadarLabel({
    required this.alignment,
    required this.label,
    this.score,
  });

  final Alignment alignment;
  final String label;
  final int? score;

  @override
  Widget build(BuildContext context) => Align(
    alignment: alignment,
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          label,
          maxLines: 1,
          style: SpeakUpDesign.meta.copyWith(color: SpeakUpDesign.secondary),
        ),
        const SizedBox(height: 2),
        Text(
          score == null ? '--' : '${score!}.0',
          style: SpeakUpDesign.cardTitle.copyWith(
            color: SpeakUpDesign.ink,
            fontSize: 20,
            height: 1.1,
          ),
        ),
      ],
    ),
  );
}

class _RadarPainter extends CustomPainter {
  const _RadarPainter(this.values, {this.emphasized = false});

  final List<double> values;
  final bool emphasized;

  @override
  void paint(Canvas canvas, Size size) {
    final center = size.center(Offset.zero);
    final radius = math.min(size.width, size.height) / 2;
    final grid = Paint()
      ..color = SpeakUpDesign.border
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1;
    for (final level in [1 / 3, 2 / 3, 1.0]) {
      canvas.drawPath(
        _polygon(center, radius * level, const [1, 1, 1, 1]),
        grid,
      );
    }
    for (final point in _points(center, radius, const [1, 1, 1, 1])) {
      canvas.drawLine(center, point, grid);
    }
    final normalized = values
        .map((value) => (value / 9).clamp(0.0, 1.0))
        .toList();
    final dataPath = _polygon(center, radius, normalized);
    canvas.drawPath(
      dataPath,
      Paint()
        ..color = (emphasized ? _abilityBlue : SpeakUpDesign.primary)
            .withValues(alpha: emphasized ? 0.12 : 0.2)
        ..style = PaintingStyle.fill,
    );
    canvas.drawPath(
      dataPath,
      Paint()
        ..color = emphasized ? _abilityBlue : SpeakUpDesign.primary
        ..style = PaintingStyle.stroke
        ..strokeWidth = emphasized ? 2.4 : 2.5,
    );
    if (emphasized) {
      for (final point in _points(center, radius, normalized)) {
        canvas.drawCircle(
          point,
          4.5,
          Paint()
            ..color = SpeakUpDesign.surface
            ..style = PaintingStyle.fill,
        );
        canvas.drawCircle(
          point,
          2.8,
          Paint()
            ..color = _abilityBlue
            ..style = PaintingStyle.fill,
        );
      }
    }
  }

  Path _polygon(Offset center, double radius, List<num> scales) {
    final points = _points(center, radius, scales);
    return Path()
      ..moveTo(points.first.dx, points.first.dy)
      ..addPolygon(points, true);
  }

  List<Offset> _points(Offset center, double radius, List<num> scales) => [
    Offset(center.dx, center.dy - radius * scales[0]),
    Offset(center.dx + radius * scales[1], center.dy),
    Offset(center.dx, center.dy + radius * scales[2]),
    Offset(center.dx - radius * scales[3], center.dy),
  ];

  @override
  bool shouldRepaint(covariant _RadarPainter oldDelegate) =>
      oldDelegate.values != values || oldDelegate.emphasized != emphasized;
}

class _EvidenceStandard extends StatelessWidget {
  const _EvidenceStandard({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    final pronunciation = report.criteria.firstWhere(
      (item) => item.id == IeltsSpeakingCriterionId.pronunciation,
    );
    final acousticSamples =
        (pronunciation.coverage * report.testSummary.questionCount).round();
    return Card(
      key: const Key('ielts-speaking-evidence-standard'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('评分依据', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: SpeakUpDesign.space12),
            Text(
              '语音证据 · $acousticSamples/${report.testSummary.questionCount} 道回答通过声学验真；累计有效英文语音不少于 3 秒才进入发音评分。',
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            const Text(
              '文字证据 · 仅使用已确认的英文转写，按整场回答评估衔接、词汇和语法，不按单个错误机械扣分。',
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            const Text(
              '中英混合 · 中文片段不计入英文词汇、语法或发音证据，也不会被标成英文错误；只评估其中可确认的英文表达。',
              style: SpeakUpDesign.body,
            ),
            const SizedBox(height: SpeakUpDesign.space12),
            Text(
              report.disclaimer,
              key: const Key('ielts-speaking-report-disclaimer'),
              style: SpeakUpDesign.meta,
            ),
          ],
        ),
      ),
    );
  }
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

class _QuestionReviews extends StatelessWidget {
  const _QuestionReviews({required this.report, this.onRepracticeQuestion});

  final IeltsSpeakingReport report;
  final Future<bool> Function(IeltsSpeakingQuestionReview question)?
  onRepracticeQuestion;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('ielts-speaking-report-questions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('同题复练', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            Text(
              '本次问到的 ${report.questions.length} 道题，可直接选择原题重新作答。',
              style: SpeakUpDesign.meta,
            ),
            const SizedBox(height: 14),
            for (var index = 0; index < report.questions.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 14),
                const Divider(height: 1),
                const SizedBox(height: 14),
              ],
              _RepracticeQuestion(
                question: report.questions[index],
                onPressed: onRepracticeQuestion == null
                    ? null
                    : () => onRepracticeQuestion!(report.questions[index]),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _RepracticeQuestion extends StatelessWidget {
  const _RepracticeQuestion({required this.question, this.onPressed});

  final IeltsSpeakingQuestionReview question;
  final Future<bool> Function()? onPressed;

  @override
  Widget build(BuildContext context) {
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
        const SizedBox(height: 10),
        Align(
          alignment: Alignment.centerRight,
          child: OutlinedButton.icon(
            key: Key('ielts-speaking-repractice-${question.index}'),
            onPressed: onPressed,
            icon: const Icon(Icons.mic_none_rounded, size: 18),
            label: const Text('直接重练'),
          ),
        ),
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
