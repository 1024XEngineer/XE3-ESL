import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';

class IeltsEvaluationOverview extends StatelessWidget {
  const IeltsEvaluationOverview({
    required this.report,
    this.title = 'IELTS 四维表现',
    this.scoreTitle = '练习估分',
    this.contextLabel,
    super.key,
  });

  final EvaluationReport report;
  final String title;
  final String scoreTitle;
  final String? contextLabel;

  @override
  Widget build(BuildContext context) {
    final dimensions = <String, EvaluationReportDimension>{
      for (final dimension in report.dimensions) dimension.key: dimension,
    };
    final axes = <_RadarAxis>[
      _RadarAxis(
        label: '流利性与连贯性',
        value: dimensions['FLUENCY_COHERENCE']?.score,
      ),
      _RadarAxis(label: '发音', value: dimensions['PRONUNCIATION']?.score),
      _RadarAxis(
        label: '语法多样性及准确性',
        value: dimensions['GRAMMATICAL_RANGE_ACCURACY']?.score,
      ),
      _RadarAxis(label: '词汇丰富度', value: dimensions['LEXICAL_RESOURCE']?.score),
    ];
    final scores = axes.map((axis) => axis.value).whereType<double>().toList();
    final overall = scores.length == axes.length
        ? _roundToHalf(
            scores.reduce((left, right) => left + right) / scores.length,
          )
        : null;
    return Card(
      key: const Key('ielts-evaluation-overview'),
      elevation: 0,
      color: SpeakUpDesign.surface,
      surfaceTintColor: Colors.transparent,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 20, 20, 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(child: Text(title, style: SpeakUpDesign.cardTitle)),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    Text(scoreTitle, style: SpeakUpDesign.meta),
                    const SizedBox(height: 2),
                    Text(
                      overall == null ? '--' : _scoreLabel(overall),
                      key: const Key('ielts-evaluation-overall-score'),
                      style: SpeakUpDesign.pageTitle.copyWith(fontSize: 32),
                    ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            Text(
              contextLabel ?? '四项等权平均，并取最近的 0.5 分。',
              style: SpeakUpDesign.meta,
            ),
            const SizedBox(height: SpeakUpDesign.space12),
            _FourAxisRadar(axes: axes),
          ],
        ),
      ),
    );
  }
}

/// Refined Hero Score & Radar Overview Card for Part 1 reports.
/// Designed as a cohesive, elegant Apple-style score card.
class IeltsPart1DarkOverview extends StatelessWidget {
  const IeltsPart1DarkOverview({
    required this.report,
    this.contextLabel,
    super.key,
  });

  final EvaluationReport report;
  final String? contextLabel;

  @override
  Widget build(BuildContext context) {
    final dimensions = <String, EvaluationReportDimension>{
      for (final dimension in report.dimensions) dimension.key: dimension,
    };
    final axes = <_RadarAxis>[
      _RadarAxis(label: '流利与连贯', value: dimensions['FLUENCY_COHERENCE']?.score),
      _RadarAxis(label: '发音', value: dimensions['PRONUNCIATION']?.score),
      _RadarAxis(
        label: '语法准确性',
        value: dimensions['GRAMMATICAL_RANGE_ACCURACY']?.score,
      ),
      _RadarAxis(label: '词汇丰富度', value: dimensions['LEXICAL_RESOURCE']?.score),
    ];
    final scores = axes.map((axis) => axis.value).whereType<double>().toList();
    final overall = scores.length == axes.length
        ? _roundToHalf(
            scores.reduce((left, right) => left + right) / scores.length,
          )
        : null;

    return Container(
      key: const Key('ielts-part1-dark-overview'),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.03),
            blurRadius: 12,
            offset: const Offset(0, 4),
          ),
        ],
      ),
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Header: Title & Big Overall Score Badge
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Wrap(
                      crossAxisAlignment: WrapCrossAlignment.center,
                      spacing: 8,
                      children: [
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 7,
                            vertical: 3,
                          ),
                          decoration: BoxDecoration(
                            color: SpeakUpDesign.ink,
                            borderRadius: BorderRadius.circular(6),
                          ),
                          child: const Text(
                            'IELTS Part 1',
                            style: TextStyle(
                              color: Colors.white,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                              letterSpacing: 0.2,
                            ),
                          ),
                        ),
                        Text(
                          '四维表现',
                          style: SpeakUpDesign.label.copyWith(
                            color: SpeakUpDesign.secondary,
                            fontSize: 13,
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      contextLabel ?? '四项等权平均，取最近 0.5 分',
                      style: SpeakUpDesign.meta.copyWith(
                        color: SpeakUpDesign.secondary,
                        fontSize: 12,
                        height: 1.35,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              // Overall Band Badge
              Container(
                padding: const EdgeInsets.fromLTRB(14, 8, 14, 8),
                decoration: BoxDecoration(
                  color: SpeakUpDesign.surfaceMuted,
                  borderRadius: BorderRadius.circular(14),
                  border: Border.all(color: SpeakUpDesign.border, width: 0.8),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.center,
                  children: [
                    Text(
                      '练习估分',
                      style: SpeakUpDesign.meta.copyWith(
                        fontSize: 10,
                        fontWeight: FontWeight.w600,
                        color: SpeakUpDesign.secondary,
                      ),
                    ),
                    const SizedBox(height: 1),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.baseline,
                      textBaseline: TextBaseline.alphabetic,
                      children: [
                        Text(
                          overall == null ? '--' : _scoreLabel(overall),
                          key: const Key('ielts-evaluation-overall-score'),
                          style: const TextStyle(
                            color: SpeakUpDesign.ink,
                            fontSize: 28,
                            fontWeight: FontWeight.w800,
                            height: 1.0,
                            letterSpacing: -0.5,
                          ),
                        ),
                        const SizedBox(width: 2),
                        Text(
                          '/ 9',
                          style: TextStyle(
                            color: SpeakUpDesign.tertiary,
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          const Divider(height: 16, color: SpeakUpDesign.border),
          // Radar chart
          _FourAxisRadar(axes: axes, darkMode: false),
        ],
      ),
    );
  }
}

double _roundToHalf(double value) => (value * 2).round() / 2;

String _scoreLabel(double value) => value == value.roundToDouble()
    ? value.toInt().toString()
    : value.toStringAsFixed(1);

final class _RadarAxis {
  const _RadarAxis({required this.label, required this.value});

  final String label;
  final double? value;
}

class _FourAxisRadar extends StatelessWidget {
  const _FourAxisRadar({required this.axes, this.darkMode = false});

  final List<_RadarAxis> axes;
  final bool darkMode;

  @override
  Widget build(BuildContext context) {
    final semanticLabel = axes
        .map(
          (axis) =>
              '${axis.label} ${axis.value == null ? '未评分' : '${_scoreLabel(axis.value!)}分'}',
        )
        .join('，');
    return Semantics(
      key: const Key('ielts-evaluation-radar'),
      label: 'IELTS 口语四维雷达图，$semanticLabel',
      child: SizedBox(
        height: 236,
        child: Stack(
          alignment: Alignment.center,
          children: [
            Positioned.fill(
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 68,
                  vertical: 42,
                ),
                child: CustomPaint(
                  painter: _RadarPainter(
                    axes.map((axis) => axis.value).toList(growable: false),
                    darkMode: darkMode,
                  ),
                ),
              ),
            ),
            for (final entry in <(Alignment, _RadarAxis)>[
              (Alignment.topCenter, axes[0]),
              (Alignment.centerRight, axes[1]),
              (Alignment.bottomCenter, axes[2]),
              (Alignment.centerLeft, axes[3]),
            ])
              Align(
                alignment: entry.$1,
                child: SizedBox(
                  width: 90,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        entry.$2.label,
                        maxLines: 2,
                        textAlign: TextAlign.center,
                        style: SpeakUpDesign.meta.copyWith(
                          color: SpeakUpDesign.secondary,
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 2,
                        ),
                        decoration: BoxDecoration(
                          color: _scorePillBg(entry.$2.value),
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Text(
                          entry.$2.value == null
                              ? '--'
                              : _scoreLabel(entry.$2.value!),
                          style: TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w800,
                            color: _scorePillText(entry.$2.value),
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

Color _scorePillBg(double? score) {
  if (score == null) return SpeakUpDesign.primaryMuted;
  if (score >= 7.0) return const Color(0xFFEAF5EE);
  if (score >= 5.5) return const Color(0xFFFFF6E6);
  return const Color(0xFFFDEEEB);
}

Color _scorePillText(double? score) {
  if (score == null) return SpeakUpDesign.secondary;
  if (score >= 7.0) return const Color(0xFF1E6B37);
  if (score >= 5.5) return const Color(0xFFB25E00);
  return const Color(0xFFC0392B);
}

class _RadarPainter extends CustomPainter {
  const _RadarPainter(this.values, {this.darkMode = false});

  final List<double?> values;
  final bool darkMode;

  @override
  void paint(Canvas canvas, Size size) {
    final center = size.center(Offset.zero);
    final radius = math.min(size.width, size.height) / 2;
    final grid = Paint()
      ..color = const Color(0xFFE5E7EB)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.0;

    // Outer and inner concentric diamonds
    for (final level in const <double>[1.0, 0.75, 0.5, 0.25]) {
      canvas.drawPath(_polygon(center, radius, List.filled(4, level)), grid);
    }
    for (final point in _points(center, radius, const [1, 1, 1, 1])) {
      canvas.drawLine(center, point, grid);
    }
    final normalized = values
        .map((value) => value == null ? null : (value / 9).clamp(0.0, 1.0))
        .toList(growable: false);
    if (normalized.every((value) => value != null)) {
      final data = _polygon(
        center,
        radius,
        normalized.whereType<double>().toList(growable: false),
      );
      // Data fill with smooth tint
      canvas.drawPath(
        data,
        Paint()
          ..color = const Color(0xFF1F2937).withValues(alpha: 0.08)
          ..style = PaintingStyle.fill,
      );
      // Data boundary line
      canvas.drawPath(
        data,
        Paint()
          ..color = const Color(0xFF111827)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.0
          ..strokeCap = StrokeCap.round
          ..strokeJoin = StrokeJoin.round,
      );
      // Vertex points
      for (var index = 0; index < normalized.length; index++) {
        final point = _point(center, radius, index, normalized[index]!);
        canvas.drawCircle(point, 3.5, Paint()..color = Colors.white);
        canvas.drawCircle(point, 2.2, Paint()..color = const Color(0xFF111827));
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
    for (var index = 0; index < scales.length; index++)
      _point(center, radius, index, scales[index]),
  ];

  Offset _point(Offset center, double radius, int index, num scale) {
    final angle = -math.pi / 2 + math.pi / 2 * index;
    return Offset(
      center.dx + math.cos(angle) * radius * scale,
      center.dy + math.sin(angle) * radius * scale,
    );
  }

  @override
  bool shouldRepaint(covariant _RadarPainter oldDelegate) {
    if (darkMode != oldDelegate.darkMode) return true;
    for (var index = 0; index < values.length; index++) {
      if (values[index] != oldDelegate.values[index]) return true;
    }
    return false;
  }
}
