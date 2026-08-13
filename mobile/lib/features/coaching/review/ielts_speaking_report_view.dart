import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';

const _abilityBlue = Color(0xFF2563EB);
const _reportAccent = Color(0xFF2F72F5);
const _reportCanvas = Color(0xFFF7F8FC);
const _reportBorder = Color(0xFFE9ECF2);
const _reportMuted = Color(0xFF727987);
const _reportBodyStyle = TextStyle(
  color: _reportMuted,
  fontSize: 13,
  fontWeight: FontWeight.w400,
  height: 1.55,
);

class _ReportCard extends Card {
  const _ReportCard({required super.child, super.key})
    : super(
        margin: EdgeInsets.zero,
        elevation: 0,
        color: SpeakUpDesign.surface,
        surfaceTintColor: Colors.transparent,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(16)),
          side: BorderSide(color: _reportBorder),
        ),
      );
}

class IeltsSpeakingReportScaffold extends StatelessWidget {
  const IeltsSpeakingReportScaffold({
    required this.title,
    required this.child,
    super.key,
  });

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final titleParts = title == 'IELTS 口语模考报告'
        ? const ('IELTS', '口语模考报告')
        : (title, '');
    return Scaffold(
      backgroundColor: _reportCanvas,
      appBar: AppBar(
        backgroundColor: _reportCanvas,
        foregroundColor: SpeakUpDesign.ink,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        toolbarHeight: 76,
        titleSpacing: 0,
        title: Semantics(
          header: true,
          label: title,
          excludeSemantics: true,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                titleParts.$1,
                style: const TextStyle(
                  color: _reportAccent,
                  fontFamily: 'Georgia',
                  fontFamilyFallback: ['serif'],
                  fontSize: 23,
                  fontWeight: FontWeight.w700,
                  height: 1.05,
                  letterSpacing: -0.35,
                ),
              ),
              if (titleParts.$2.isNotEmpty)
                Text(
                  titleParts.$2,
                  style: const TextStyle(
                    color: SpeakUpDesign.ink,
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                    height: 1.2,
                    letterSpacing: 0.1,
                  ),
                ),
            ],
          ),
        ),
      ),
      body: SafeArea(
        top: false,
        child: SingleChildScrollView(
          key: const Key('ielts-speaking-report-scroll'),
          padding: EdgeInsets.fromLTRB(
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space8,
            SpeakUpDesign.horizontalInset(context),
            40,
          ),
          child: child,
        ),
      ),
    );
  }
}

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
        _OverallScore(report: report),
        const SizedBox(height: SpeakUpDesign.space24),
        const _ReportSectionTitle(title: '评分维度'),
        const SizedBox(height: SpeakUpDesign.space12),
        IeltsSpeakingScoreOverview(
          key: const Key('ielts-speaking-report-criteria'),
          criteria: report.criteria,
        ),
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
    return _ReportCard(
      key: Key(
        band == null
            ? 'ielts-speaking-overall-unavailable'
            : 'ielts-speaking-overall-available',
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 22, 20, 20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            FittedBox(
              fit: BoxFit.scaleDown,
              alignment: Alignment.centerLeft,
              child: Row(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.center,
                children: [
                  Text(
                    '口语总分',
                    style: SpeakUpDesign.label.copyWith(
                      color: SpeakUpDesign.ink,
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Text(
                    band == null ? '暂不可用' : _bandLabel(band),
                    style: SpeakUpDesign.pageTitle.copyWith(
                      color: band == null ? _reportMuted : _reportAccent,
                      fontSize: band == null ? 22 : 42,
                      height: 1,
                      letterSpacing: -0.6,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 9,
                      vertical: 5,
                    ),
                    decoration: BoxDecoration(
                      color: _reportCanvas,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Text(
                      '评分标准',
                      style: SpeakUpDesign.meta.copyWith(
                        color: const Color(0xFF9AA1AE),
                        fontSize: 10,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 20),
            _ReportParagraphs(
              text: _displayOverallExplanation(report),
              key: const Key('ielts-speaking-overall-explanation'),
              style: _reportBodyStyle.copyWith(fontSize: 14, height: 1.65),
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
    this.radarSemanticsKey = const Key('ielts-speaking-score-radar'),
    super.key,
  });

  final List<IeltsSpeakingCriterion> criteria;
  final Key radarSemanticsKey;

  @override
  Widget build(BuildContext context) {
    return _ReportCard(
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space12,
          vertical: SpeakUpDesign.space16,
        ),
        child: IeltsSpeakingScoreRadar(
          criteria: criteria,
          semanticsKey: radarSemanticsKey,
          height: 300,
          cornerLabels: true,
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
    this.cornerLabels = false,
    super.key,
  });

  final List<IeltsSpeakingCriterion> criteria;
  final Key semanticsKey;
  final double height;
  final bool profileMode;
  final bool cornerLabels;

  @override
  Widget build(BuildContext context) {
    final byId = {for (final item in criteria) item.id: item};
    final ordered = [
      byId[IeltsSpeakingCriterionId.fluencyAndCoherence],
      byId[IeltsSpeakingCriterionId.lexicalResource],
      byId[IeltsSpeakingCriterionId.pronunciation],
      byId[IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy],
    ];
    final values = ordered
        .map((item) => item?.estimatedBand?.toDouble())
        .toList(growable: false);
    return FourAxisScoreRadar(
      axes: <FourAxisRadarAxis>[
        FourAxisRadarAxis(label: '流利性与连贯性', value: values[0]),
        FourAxisRadarAxis(label: '词汇丰富度', value: values[1]),
        FourAxisRadarAxis(label: '发音', value: values[2]),
        FourAxisRadarAxis(label: '语法多样性及准确性', value: values[3]),
      ],
      maximum: 9,
      semanticsKey: semanticsKey,
      semanticsPrefix: 'IELTS 口语四维雷达图',
      height: height,
      emphasized: profileMode,
      cornerLabels: cornerLabels,
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
    this.cornerLabels = false,
    super.key,
  }) : assert(axes.length == 4),
       assert(maximum > 0);

  final List<FourAxisRadarAxis> axes;
  final double maximum;
  final Key semanticsKey;
  final String semanticsPrefix;
  final double height;
  final bool emphasized;
  final bool cornerLabels;

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
        child: cornerLabels
            ? FittedBox(
                fit: BoxFit.contain,
                child: SizedBox(
                  width: 337,
                  height: 300,
                  child: Stack(
                    children: [
                      Positioned(
                        left: 107,
                        top: 74,
                        child: SizedBox.square(
                          dimension: 123,
                          child: CustomPaint(
                            painter: _IeltsReportRadarPainter(
                              axes
                                  .map((axis) => axis.value)
                                  .toList(growable: false),
                              maximum: maximum,
                            ),
                          ),
                        ),
                      ),
                      _FourAxisRadarCornerLabel(
                        axis: axes[0],
                        color: const Color(0xFF4285F4),
                        left: 0,
                        top: 58,
                      ),
                      _FourAxisRadarCornerLabel(
                        axis: axes[1],
                        color: const Color(0xFF5C9BFA),
                        right: 0,
                        top: 58,
                      ),
                      _FourAxisRadarCornerLabel(
                        axis: axes[2],
                        color: const Color(0xFF625DEF),
                        right: 0,
                        top: 218,
                      ),
                      _FourAxisRadarCornerLabel(
                        axis: axes[3],
                        color: const Color(0xFF2CAFE8),
                        left: 0,
                        top: 218,
                      ),
                    ],
                  ),
                ),
              )
            : Stack(
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
                          axes
                              .map((axis) => axis.value)
                              .toList(growable: false),
                          maximum: maximum,
                          emphasized: emphasized,
                        ),
                      ),
                    ),
                  ),
                  _FourAxisRadarLabel(
                    alignment: Alignment.topCenter,
                    axis: axes[0],
                  ),
                  _FourAxisRadarLabel(
                    alignment: Alignment.centerRight,
                    axis: axes[1],
                  ),
                  _FourAxisRadarLabel(
                    alignment: Alignment.bottomCenter,
                    axis: axes[2],
                  ),
                  _FourAxisRadarLabel(
                    alignment: Alignment.centerLeft,
                    axis: axes[3],
                  ),
                ],
              ),
      ),
    );
  }
}

class _FourAxisRadarCornerLabel extends StatelessWidget {
  const _FourAxisRadarCornerLabel({
    required this.axis,
    required this.color,
    required this.top,
    this.left,
    this.right,
  });

  final FourAxisRadarAxis axis;
  final Color color;
  final double top;
  final double? left;
  final double? right;

  @override
  Widget build(BuildContext context) {
    final isRightSide = right != null;
    return Positioned(
      top: top,
      left: left,
      right: right,
      child: SizedBox(
        width: 100,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: isRightSide
              ? CrossAxisAlignment.end
              : CrossAxisAlignment.start,
          children: [
            Text(
              axis.value == null ? '--' : _radarScoreLabel(axis.value!),
              style: SpeakUpDesign.cardTitle.copyWith(
                color: color,
                fontSize: 21,
                height: 1,
              ),
            ),
            const SizedBox(height: 5),
            Text(
              axis.label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              textAlign: isRightSide ? TextAlign.right : TextAlign.left,
              style: SpeakUpDesign.meta.copyWith(
                color: _reportMuted,
                fontSize: axis.label.length > 8 ? 9 : 11,
                fontWeight: FontWeight.w500,
                height: 1.2,
              ),
            ),
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

String _displayOverallExplanation(IeltsSpeakingReport report) {
  const legacy = '口语总分按四项练习估分等权平均，并四舍五入到最近的 0.5 分。';
  if (report.speakingOverallExplanation != legacy) {
    return report.speakingOverallExplanation;
  }
  final scored = report.criteria
      .where((criterion) => criterion.estimatedBand != null)
      .toList(growable: false);
  if (scored.length != 4) return legacy;
  final bands = scored.map((criterion) => criterion.estimatedBand!).toList();
  final minimum = bands.reduce(math.min);
  final maximum = bands.reduce(math.max);
  if (minimum == maximum) {
    return '$legacy四项表现较为均衡，具体表现与改进依据见下方各评分维度。';
  }
  final strongest = scored
      .where((criterion) => criterion.estimatedBand == maximum)
      .map(
        (criterion) =>
            '${_radarCriterionLabel(criterion.id)}（${criterion.estimatedBand} 分）',
      )
      .join('、');
  final priority = scored
      .where((criterion) => criterion.estimatedBand == minimum)
      .map(
        (criterion) =>
            '${_radarCriterionLabel(criterion.id)}（${criterion.estimatedBand} 分）',
      )
      .join('、');
  final middle = scored
      .where(
        (criterion) =>
            criterion.estimatedBand != minimum &&
            criterion.estimatedBand != maximum,
      )
      .map(
        (criterion) =>
            '${_radarCriterionLabel(criterion.id)}（${criterion.estimatedBand} 分）',
      )
      .join('、');
  final middleSummary = middle.isEmpty ? '' : '$middle处于中间水平，可结合维度反馈继续巩固。';
  return '$legacy$strongest是当前相对优势；$priority是优先提升方向。'
      '$middleSummary具体表现与改进依据见下方各评分维度。';
}

String _radarCriterionLabel(IeltsSpeakingCriterionId criterion) =>
    switch (criterion) {
      IeltsSpeakingCriterionId.fluencyAndCoherence => '流利性与连贯性',
      IeltsSpeakingCriterionId.lexicalResource => '词汇丰富度',
      IeltsSpeakingCriterionId.grammaticalRangeAndAccuracy => '语法多样性及准确性',
      IeltsSpeakingCriterionId.pronunciation => '发音',
    };

class _IeltsReportRadarPainter extends CustomPainter {
  const _IeltsReportRadarPainter(this.values, {required this.maximum});

  final List<double?> values;
  final double maximum;

  @override
  void paint(Canvas canvas, Size size) {
    final scale = math.min(size.width, size.height) / 274;
    final bounds = Offset.zero & size;
    final grid = Paint()
      ..color = const Color(0xFFE9EEF6)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2 * scale;
    canvas.drawRect(
      bounds,
      Paint()
        ..color = const Color(0xFFFAFBFE)
        ..style = PaintingStyle.fill,
    );
    canvas.drawRect(bounds.deflate(scale), grid);
    canvas
      ..drawLine(bounds.topLeft, bounds.bottomRight, grid)
      ..drawLine(bounds.topRight, bounds.bottomLeft, grid);

    final normalized = values
        .map(
          (value) => value == null ? null : (value / maximum).clamp(0.0, 1.0),
        )
        .toList(growable: false);
    if (normalized.any((value) => value == null)) {
      return;
    }

    final center = size.center(Offset.zero);
    final radius = math.min(size.width, size.height) / 2;
    final scales = normalized.whereType<double>().toList(growable: false);
    final points = <Offset>[
      Offset(center.dx - radius * scales[0], center.dy - radius * scales[0]),
      Offset(center.dx + radius * scales[1], center.dy - radius * scales[1]),
      Offset(center.dx + radius * scales[2], center.dy + radius * scales[2]),
      Offset(center.dx - radius * scales[3], center.dy + radius * scales[3]),
    ];
    final segments = <Path>[
      _curvedSegment(points[0], points[1], _RadarCurveSide.top),
      _curvedSegment(points[1], points[2], _RadarCurveSide.right),
      _curvedSegment(points[2], points[3], _RadarCurveSide.bottom),
      _curvedSegment(points[3], points[0], _RadarCurveSide.left),
    ];
    final dataPath = Path()..moveTo(points[0].dx, points[0].dy);
    for (var index = 0; index < points.length; index++) {
      final controls = _curveControls(
        points[index],
        points[(index + 1) % points.length],
        _RadarCurveSide.values[index],
      );
      dataPath.cubicTo(
        controls.$1.dx,
        controls.$1.dy,
        controls.$2.dx,
        controls.$2.dy,
        points[(index + 1) % points.length].dx,
        points[(index + 1) % points.length].dy,
      );
    }
    dataPath.close();
    canvas.drawPath(
      dataPath,
      Paint()
        ..color = const Color(0xFF6DB7FF).withValues(alpha: 0.24)
        ..style = PaintingStyle.fill,
    );

    const markerColors = <Color>[
      Color(0xFF3F86F8),
      Color(0xFF64A7FA),
      Color(0xFF6869EE),
      Color(0xFF2DB5E6),
    ];
    for (var index = 0; index < segments.length; index++) {
      canvas.drawPath(
        segments[index],
        Paint()
          ..shader = ui.Gradient.linear(
            points[index],
            points[(index + 1) % points.length],
            <Color>[
              markerColors[index],
              markerColors[(index + 1) % markerColors.length],
            ],
          )
          ..style = PaintingStyle.stroke
          ..strokeWidth = 6 * scale
          ..strokeCap = StrokeCap.round,
      );
    }
    for (var index = 0; index < points.length; index++) {
      canvas.drawCircle(
        points[index],
        15 * scale,
        Paint()
          ..color = Colors.white
          ..style = PaintingStyle.fill,
      );
      canvas.drawCircle(
        points[index],
        11 * scale,
        Paint()
          ..color = markerColors[index]
          ..style = PaintingStyle.fill,
      );
    }
  }

  Path _curvedSegment(Offset start, Offset end, _RadarCurveSide side) {
    final controls = _curveControls(start, end, side);
    return Path()
      ..moveTo(start.dx, start.dy)
      ..cubicTo(
        controls.$1.dx,
        controls.$1.dy,
        controls.$2.dx,
        controls.$2.dy,
        end.dx,
        end.dy,
      );
  }

  (Offset, Offset) _curveControls(
    Offset start,
    Offset end,
    _RadarCurveSide side,
  ) {
    const depth = 10.0;
    final first = Offset.lerp(start, end, 0.34)!;
    final second = Offset.lerp(start, end, 0.66)!;
    final inward = switch (side) {
      _RadarCurveSide.top => const Offset(0, depth),
      _RadarCurveSide.right => const Offset(-depth, 0),
      _RadarCurveSide.bottom => const Offset(0, -depth),
      _RadarCurveSide.left => const Offset(depth, 0),
    };
    return (first + inward, second + inward);
  }

  @override
  bool shouldRepaint(covariant _IeltsReportRadarPainter oldDelegate) =>
      oldDelegate.values != values || oldDelegate.maximum != maximum;
}

enum _RadarCurveSide { top, right, bottom, left }

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

class _ReportSectionTitle extends StatelessWidget {
  const _ReportSectionTitle({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) => Semantics(
    header: true,
    child: Row(
      children: [
        Container(
          width: 3,
          height: 15,
          decoration: BoxDecoration(
            color: _reportAccent,
            borderRadius: BorderRadius.circular(2),
          ),
        ),
        const SizedBox(width: SpeakUpDesign.space8),
        Text(title, style: SpeakUpDesign.cardTitle.copyWith(fontSize: 16)),
      ],
    ),
  );
}

class _ReportParagraphs extends StatelessWidget {
  const _ReportParagraphs({required this.text, required this.style, super.key});

  final String text;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    final paragraphs = _splitReportParagraphs(text);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var index = 0; index < paragraphs.length; index++) ...[
          if (index > 0) const SizedBox(height: SpeakUpDesign.space8),
          Text(paragraphs[index], style: style),
        ],
      ],
    );
  }
}

List<String> _splitReportParagraphs(String text) {
  final paragraphs = <String>[];
  var start = 0;
  for (var index = 0; index < text.length; index++) {
    if (!'。！？!?'.contains(text[index])) continue;
    paragraphs.add(text.substring(start, index + 1).trim());
    start = index + 1;
  }
  if (start < text.length) {
    paragraphs.add(text.substring(start).trim());
  }
  return paragraphs.where((paragraph) => paragraph.isNotEmpty).toList();
}

class _CriterionFeedback extends StatelessWidget {
  const _CriterionFeedback({required this.criterion});

  final IeltsSpeakingCriterion criterion;

  @override
  Widget build(BuildContext context) {
    final findings = <({String label, IeltsSpeakingFinding finding})>[
      for (final finding in criterion.upgradeExamples)
        (label: '提升表达', finding: finding),
    ];
    return _ReportCard(
      key: Key('ielts-speaking-criterion-${criterion.id.name}'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Container(
                        width: 6,
                        height: 6,
                        margin: const EdgeInsets.only(top: 6),
                        decoration: const BoxDecoration(
                          color: _reportAccent,
                          shape: BoxShape.circle,
                        ),
                      ),
                      const SizedBox(width: SpeakUpDesign.space8),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              _criterionChineseLabel(criterion.id),
                              style: SpeakUpDesign.cardTitle.copyWith(
                                fontSize: 14,
                              ),
                            ),
                            const SizedBox(height: 1),
                            Text(
                              _criterionEnglishLabel(criterion.id),
                              style: SpeakUpDesign.meta.copyWith(
                                color: const Color(0xFF9AA1AE),
                                fontSize: 10,
                              ),
                            ),
                          ],
                        ),
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
                      fontSize: 21,
                      height: 1,
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
            _ReportParagraphs(
              text: criterion.explanation,
              key: Key('ielts-speaking-explanation-${criterion.id.name}'),
              style: _reportBodyStyle,
            ),
            for (final item in findings) ...[
              const SizedBox(height: SpeakUpDesign.space16),
              Container(height: 1, color: const Color(0xFFF1F2F5)),
              const SizedBox(height: SpeakUpDesign.space12),
              Text(
                item.label,
                style: SpeakUpDesign.label.copyWith(fontSize: 12),
              ),
              const SizedBox(height: SpeakUpDesign.space4),
              Text(item.finding.message, style: _reportBodyStyle),
              if (item.finding.suggestion case final suggestion?) ...[
                const SizedBox(height: SpeakUpDesign.space4),
                Text('建议：$suggestion', style: _reportBodyStyle),
              ],
              for (final evidence in item.finding.evidence) ...[
                const SizedBox(height: SpeakUpDesign.space4),
                Text(
                  '原句：“${evidence.originalExcerpt}”',
                  style: SpeakUpDesign.meta.copyWith(
                    color: _reportMuted,
                    height: 1.45,
                  ),
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
    return _ReportCard(
      key: const Key('ielts-speaking-target-plan'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '优先练习建议',
              style: SpeakUpDesign.cardTitle.copyWith(fontSize: 14),
            ),
            if (report.priorityActions.isEmpty) ...[
              const SizedBox(height: 10),
              Text('现有证据不足以生成优先建议。', style: _reportBodyStyle),
            ],
            for (
              var index = 0;
              index < report.priorityActions.length;
              index++
            ) ...[
              const SizedBox(height: SpeakUpDesign.space12),
              _NumberedRecommendation(
                number: index + 1,
                text: _actionText(report, report.priorityActions[index]),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _NumberedRecommendation extends StatelessWidget {
  const _NumberedRecommendation({required this.number, required this.text});

  final int number;
  final String text;

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      SizedBox(
        width: 22,
        child: Text(
          '$number.',
          style: SpeakUpDesign.label.copyWith(
            color: _reportAccent,
            fontSize: 12,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
      Expanded(child: Text(text, style: _reportBodyStyle)),
    ],
  );
}

String _actionText(
  IeltsSpeakingReport report,
  IeltsSpeakingPriorityAction action,
) {
  final finding = report.finding(action.findingId)!;
  return finding.suggestion ?? finding.message;
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

Color _criterionColor(IeltsSpeakingCriterionId _) => _reportAccent;

String _bandLabel(double band) => band == band.roundToDouble()
    ? band.toInt().toString()
    : band.toStringAsFixed(1);
