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
  const IeltsSpeakingReportPanel({required this.controller, super.key});

  final IeltsSpeakingReportController controller;

  @override
  State<IeltsSpeakingReportPanel> createState() =>
      _IeltsSpeakingReportPanelState();
}

class IeltsSpeakingReadyReportView extends StatelessWidget {
  const IeltsSpeakingReadyReportView({required this.report, super.key});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) => _ReadyReport(report: report);
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
      IeltsSpeakingReportEvaluationStatus.ready => IeltsSpeakingReadyReportView(
        report: envelope.report!,
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
          color: SpeakUpDesign.surfaceMuted,
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusMedia),
          border: Border.all(color: SpeakUpDesign.border),
        ),
        child: Padding(
          padding: const EdgeInsets.all(SpeakUpDesign.space20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('本次模考', style: SpeakUpDesign.cardTitle),
              const SizedBox(height: SpeakUpDesign.space12),
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
    return Card(
      key: Key(
        band == null
            ? 'ielts-speaking-overall-unavailable'
            : 'ielts-speaking-overall-available',
      ),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                const Text('口语总分', style: SpeakUpDesign.cardTitle),
                const SizedBox(width: SpeakUpDesign.space8),
                Text(
                  band == null ? '暂不可用' : _bandLabel(band),
                  style: SpeakUpDesign.pageTitle.copyWith(
                    color: SpeakUpDesign.ink,
                    fontSize: band == null ? 24 : 42,
                  ),
                ),
                const Spacer(),
                Text('0–9 分练习估分', style: SpeakUpDesign.meta),
              ],
            ),
            const SizedBox(height: SpeakUpDesign.space16),
            const Divider(height: 1),
            const SizedBox(height: SpeakUpDesign.space16),
            Text(report.speakingOverallExplanation, style: SpeakUpDesign.body),
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
    this.completedAt,
    super.key,
  });

  final IeltsSpeakingReport? report;
  final bool loading;
  final DateTime? completedAt;

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
              _AbilitySummary(report: value, completedAt: completedAt),
            ],
          ],
        ),
      ),
    );
  }
}

class _AbilitySummary extends StatelessWidget {
  const _AbilitySummary({required this.report, required this.completedAt});

  final IeltsSpeakingReport report;
  final DateTime? completedAt;

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
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('当前综合水平', style: SpeakUpDesign.meta),
                const SizedBox(height: SpeakUpDesign.space4),
                Text(
                  band?.toStringAsFixed(1) ?? '--',
                  key: const Key('review-ability-overall-band'),
                  style: SpeakUpDesign.sectionTitle.copyWith(
                    color: _abilityBlue,
                    fontSize: 28,
                    height: 1,
                  ),
                ),
              ],
            ),
          ),
          if (completedAt case final value?)
            Column(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Text('最近更新', style: SpeakUpDesign.meta),
                const SizedBox(height: SpeakUpDesign.space4),
                Text(_abilityDate(value), style: SpeakUpDesign.label),
              ],
            ),
        ],
      ),
    );
  }
}

String _abilityDate(DateTime value) {
  final local = value.toLocal();
  return '${local.month} 月 ${local.day} 日';
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
            loading ? '正在读取能力数据' : '完成至少一次完整口语模拟',
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
    this.height = 320,
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
        .map((item) => item?.estimatedBand?.toDouble())
        .toList(growable: false);
    return FourAxisScoreRadar(
      axes: <FourAxisRadarAxis>[
        FourAxisRadarAxis(label: '流利与连贯', value: values[0]),
        FourAxisRadarAxis(label: '发音', value: values[1]),
        FourAxisRadarAxis(label: '语法', value: values[2]),
        FourAxisRadarAxis(label: '词汇', value: values[3]),
      ],
      maximum: 9,
      semanticsKey: semanticsKey,
      semanticsPrefix: 'IELTS 口语四维雷达图',
      height: height,
      emphasized: profileMode,
    );
  }
}

final class FourAxisRadarAxis {
  const FourAxisRadarAxis({required this.label, required this.value});

  final String label;
  final double? value;
}

class FourAxisScoreRadar extends StatelessWidget {
  const FourAxisScoreRadar({
    required this.axes,
    required this.maximum,
    required this.semanticsKey,
    required this.semanticsPrefix,
    this.height = 300,
    this.emphasized = false,
    super.key,
  }) : assert(axes.length == 4),
       assert(maximum > 0);

  final List<FourAxisRadarAxis> axes;
  final double maximum;
  final Key semanticsKey;
  final String semanticsPrefix;
  final double height;
  final bool emphasized;

  @override
  Widget build(BuildContext context) {
    final semanticLabel = axes
        .map(
          (axis) =>
              '${axis.label} ${axis.value == null ? '未评分' : _radarScoreLabel(axis.value!)}分',
        )
        .join('，');
    return Semantics(
      key: semanticsKey,
      label: '$semanticsPrefix，$semanticLabel',
      child: SizedBox(
        height: height,
        child: Stack(
          alignment: Alignment.center,
          children: [
            Positioned.fill(
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 72,
                  vertical: 56,
                ),
                child: CustomPaint(
                  painter: _FourAxisRadarPainter(
                    axes.map((axis) => axis.value).toList(growable: false),
                    maximum: maximum,
                    emphasized: emphasized,
                  ),
                ),
              ),
            ),
            _FourAxisRadarLabel(alignment: Alignment.topCenter, axis: axes[0]),
            _FourAxisRadarLabel(
              alignment: Alignment.centerRight,
              axis: axes[1],
            ),
            _FourAxisRadarLabel(
              alignment: Alignment.bottomCenter,
              axis: axes[2],
            ),
            _FourAxisRadarLabel(alignment: Alignment.centerLeft, axis: axes[3]),
          ],
        ),
      ),
    );
  }
}

class _FourAxisRadarLabel extends StatelessWidget {
  const _FourAxisRadarLabel({required this.alignment, required this.axis});

  final Alignment alignment;
  final FourAxisRadarAxis axis;

  @override
  Widget build(BuildContext context) {
    final score = Text(
      axis.value == null ? '--' : _radarScoreLabel(axis.value!),
      style: SpeakUpDesign.cardTitle.copyWith(
        color: const Color(0xFF3679F5),
        fontSize: 22,
        height: 1,
      ),
    );
    final label = Text(
      axis.label,
      maxLines: 1,
      textAlign: TextAlign.center,
      style: SpeakUpDesign.label.copyWith(
        color: SpeakUpDesign.ink,
        fontWeight: FontWeight.w500,
      ),
    );
    return Align(
      alignment: alignment,
      child: SizedBox(
        width: 92,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [label, const SizedBox(height: 7), score],
        ),
      ),
    );
  }
}

String _radarScoreLabel(double value) => value == value.roundToDouble()
    ? value.toInt().toString()
    : value.toStringAsFixed(1);

class _FourAxisRadarPainter extends CustomPainter {
  const _FourAxisRadarPainter(
    this.values, {
    required this.maximum,
    this.emphasized = false,
  });

  final List<double?> values;
  final double maximum;
  final bool emphasized;

  @override
  void paint(Canvas canvas, Size size) {
    final center = size.center(Offset.zero);
    final radius = math.min(size.width, size.height) / 2;
    final grid = Paint()
      ..color = const Color(0xFFDCE3F1)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.1;
    final levels = <double>[1, 0.75, 0.5, 0.25];
    for (final level in levels) {
      final path = _polygon(center, radius, List<double>.filled(4, level));
      canvas.drawPath(path, grid);
    }
    final outerPoints = _points(center, radius, const [1, 1, 1, 1]);
    for (final point in outerPoints) {
      canvas.drawLine(center, point, grid);
      canvas.drawCircle(
        point,
        3,
        Paint()
          ..color = const Color(0xFFB8C5DC)
          ..style = PaintingStyle.fill,
      );
    }
    final normalized = values
        .map(
          (value) => value == null ? null : (value / maximum).clamp(0.0, 1.0),
        )
        .toList(growable: false);
    if (normalized.every((value) => value != null)) {
      final dataPath = _polygon(
        center,
        radius,
        normalized.whereType<double>().toList(growable: false),
      );
      canvas.drawPath(
        dataPath,
        Paint()
          ..color = const Color(
            0xFF3679F5,
          ).withValues(alpha: emphasized ? 0.18 : 0.14)
          ..style = PaintingStyle.fill,
      );
      canvas.drawPath(
        dataPath,
        Paint()
          ..color = const Color(0xFF3679F5)
          ..style = PaintingStyle.stroke
          ..strokeWidth = emphasized ? 2.8 : 2.4,
      );
    }
    for (var index = 0; index < normalized.length; index++) {
      final scale = normalized[index];
      if (scale == null) {
        continue;
      }
      final point = _point(center, radius, index, scale);
      canvas.drawCircle(
        point,
        emphasized ? 4.5 : 4,
        Paint()
          ..color = SpeakUpDesign.surface
          ..style = PaintingStyle.fill,
      );
      canvas.drawCircle(
        point,
        emphasized ? 3.2 : 2.8,
        Paint()
          ..color = const Color(0xFF3679F5)
          ..style = PaintingStyle.fill,
      );
    }
  }

  Path _polygon(Offset center, double radius, List<num> scales) {
    final points = _points(center, radius, scales);
    return Path()
      ..moveTo(points.first.dx, points.first.dy)
      ..addPolygon(points, true);
  }

  List<Offset> _points(Offset center, double radius, List<num> scales) => [
    for (var index = 0; index < scales.length; index++)
      _point(center, radius, index, scales[index]),
  ];

  Offset _point(Offset center, double radius, int index, num scale) {
    if (index < 0 || index > 3) {
      throw ArgumentError.value(index, 'index');
    }
    final angle = -math.pi / 2 + (math.pi / 2 * index);
    return Offset(
      center.dx + math.cos(angle) * radius * scale,
      center.dy + math.sin(angle) * radius * scale,
    );
  }

  @override
  bool shouldRepaint(covariant _FourAxisRadarPainter oldDelegate) =>
      oldDelegate.values != values ||
      oldDelegate.maximum != maximum ||
      oldDelegate.emphasized != emphasized;
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

Color _criterionColor(IeltsSpeakingCriterionId _) => SpeakUpDesign.ink;

String _durationLabel(int durationMs) {
  final totalSeconds = durationMs ~/ 1000;
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes分${seconds.toString().padLeft(2, '0')}秒';
}

String _bandLabel(double band) => band == band.roundToDouble()
    ? band.toInt().toString()
    : band.toStringAsFixed(1);
