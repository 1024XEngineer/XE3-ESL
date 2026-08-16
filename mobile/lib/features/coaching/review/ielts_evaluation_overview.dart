import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';

class IeltsEvaluationOverview extends StatelessWidget {
  const IeltsEvaluationOverview({
    required this.report,
    this.title = 'IELTS 四维表现',
    this.scoreTitle = '练习估分',
    super.key,
  });

  final EvaluationReport report;
  final String title;
  final String scoreTitle;

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
            Text('四项等权平均，并取最近的 0.5 分。', style: SpeakUpDesign.meta),
            const SizedBox(height: SpeakUpDesign.space8),
            _FourAxisRadar(axes: axes),
          ],
        ),
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
  const _FourAxisRadar({required this.axes});

  final List<_RadarAxis> axes;

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
        height: 292,
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
                  painter: _RadarPainter(
                    axes.map((axis) => axis.value).toList(growable: false),
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
                  width: 96,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        entry.$2.label,
                        maxLines: 2,
                        textAlign: TextAlign.center,
                        style: SpeakUpDesign.label,
                      ),
                      const SizedBox(height: 6),
                      Text(
                        entry.$2.value == null
                            ? '--'
                            : _scoreLabel(entry.$2.value!),
                        style: SpeakUpDesign.cardTitle.copyWith(fontSize: 22),
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

class _RadarPainter extends CustomPainter {
  const _RadarPainter(this.values);

  final List<double?> values;

  @override
  void paint(Canvas canvas, Size size) {
    final center = size.center(Offset.zero);
    final radius = math.min(size.width, size.height) / 2;
    final grid = Paint()
      ..color = SpeakUpDesign.border
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.1;
    for (final level in const <double>[1, 0.75, 0.5, 0.25]) {
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
      canvas.drawPath(
        data,
        Paint()
          ..color = SpeakUpDesign.ink.withValues(alpha: 0.08)
          ..style = PaintingStyle.fill,
      );
      canvas.drawPath(
        data,
        Paint()
          ..color = SpeakUpDesign.ink.withValues(alpha: 0.72)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.4,
      );
      for (var index = 0; index < normalized.length; index++) {
        final point = _point(center, radius, index, normalized[index]!);
        canvas.drawCircle(point, 4, Paint()..color = SpeakUpDesign.surface);
        canvas.drawCircle(point, 2.8, Paint()..color = SpeakUpDesign.ink);
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
    for (var index = 0; index < values.length; index++) {
      if (values[index] != oldDelegate.values[index]) return true;
    }
    return false;
  }
}
