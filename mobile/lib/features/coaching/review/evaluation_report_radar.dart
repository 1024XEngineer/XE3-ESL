import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presenter.dart';

enum EvaluationScoreTone { unavailable, strong, developing, priority }

EvaluationScoreTone evaluationScoreTone(double? normalizedValue) {
  if (normalizedValue == null) return EvaluationScoreTone.unavailable;
  if (normalizedValue >= 0.78) return EvaluationScoreTone.strong;
  if (normalizedValue >= 0.61) return EvaluationScoreTone.developing;
  return EvaluationScoreTone.priority;
}

Color evaluationScoreBackground(double? normalizedValue) =>
    switch (evaluationScoreTone(normalizedValue)) {
      EvaluationScoreTone.unavailable => SpeakUpDesign.primaryMuted,
      EvaluationScoreTone.strong => const Color(0xFFEAF5EE),
      EvaluationScoreTone.developing => const Color(0xFFFFF6E6),
      EvaluationScoreTone.priority => const Color(0xFFFDEEEB),
    };

Color evaluationScoreForeground(double? normalizedValue) =>
    switch (evaluationScoreTone(normalizedValue)) {
      EvaluationScoreTone.unavailable => SpeakUpDesign.secondary,
      EvaluationScoreTone.strong => const Color(0xFF1E6B37),
      EvaluationScoreTone.developing => const Color(0xFFB25E00),
      EvaluationScoreTone.priority => const Color(0xFFC0392B),
    };

Color evaluationScoreBarColor(double? normalizedValue) =>
    switch (evaluationScoreTone(normalizedValue)) {
      EvaluationScoreTone.unavailable => SpeakUpDesign.border,
      EvaluationScoreTone.strong => const Color(0xFF285443),
      EvaluationScoreTone.developing => const Color(0xFFC58000),
      EvaluationScoreTone.priority => const Color(0xFF8A2D21),
    };

Color evaluationRadarAccent(int index) => const <Color>[
  SpeakUpDesign.accentCyan,
  SpeakUpDesign.primary,
  SpeakUpDesign.accentViolet,
  SpeakUpDesign.accentAmber,
  SpeakUpDesign.accentMint,
][index % 5];

class EvaluationRadarChart extends StatelessWidget {
  const EvaluationRadarChart({
    required this.axes,
    required this.rootKey,
    super.key,
  }) : assert(axes.length >= 3 && axes.length <= 5);

  final List<EvaluationRadarAxis> axes;
  final Key rootKey;

  @override
  Widget build(BuildContext context) {
    final semanticLabel = axes
        .map((axis) => '${axis.label} ${axis.scoreLabel}')
        .join('，');
    return Semantics(
      key: rootKey,
      label: '能力雷达图，$semanticLabel',
      child: SizedBox(
        height: 250,
        child: LayoutBuilder(
          builder: (context, constraints) {
            final size = Size(constraints.maxWidth, 250);
            final center = size.center(Offset.zero);
            final labelRadius = math.min(
              size.width / 2 - 48,
              size.height / 2 - 28,
            );
            return Stack(
              clipBehavior: Clip.none,
              children: <Widget>[
                Positioned.fill(
                  child: CustomPaint(
                    painter: EvaluationRadarPainter(
                      axes.map((axis) => axis.normalizedValue).toList(),
                    ),
                  ),
                ),
                for (var index = 0; index < axes.length; index++)
                  _RadarAxisLabel(
                    axis: axes[index],
                    accentColor: evaluationRadarAccent(index),
                    center: center,
                    radius: labelRadius,
                    index: index,
                    axisCount: axes.length,
                    bounds: size,
                  ),
              ],
            );
          },
        ),
      ),
    );
  }
}

class _RadarAxisLabel extends StatelessWidget {
  const _RadarAxisLabel({
    required this.axis,
    required this.accentColor,
    required this.center,
    required this.radius,
    required this.index,
    required this.axisCount,
    required this.bounds,
  });

  static const _width = 96.0;
  static const _height = 64.0;

  final EvaluationRadarAxis axis;
  final Color accentColor;
  final Offset center;
  final double radius;
  final int index;
  final int axisCount;
  final Size bounds;

  @override
  Widget build(BuildContext context) {
    final point = evaluationRadarPoint(
      center: center,
      radius: radius,
      index: index,
      axisCount: axisCount,
      scale: 1,
    );
    final left = (point.dx - _width / 2).clamp(0.0, bounds.width - _width);
    final top = (point.dy - _height / 2).clamp(0.0, bounds.height - _height);
    return Positioned(
      left: left,
      top: top,
      width: _width,
      height: _height,
      child: FittedBox(
        fit: BoxFit.scaleDown,
        child: SizedBox(
          width: _width,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            mainAxisAlignment: MainAxisAlignment.center,
            children: <Widget>[
              Text(
                axis.label,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: SpeakUpDesign.meta.copyWith(
                  color: SpeakUpDesign.secondary,
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                  height: 1.15,
                ),
              ),
              const SizedBox(height: 2),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                decoration: BoxDecoration(
                  color: accentColor.withValues(alpha: 0.13),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Text(
                  axis.scoreLabel,
                  style: TextStyle(
                    color: Color.lerp(accentColor, SpeakUpDesign.ink, 0.28),
                    fontSize: 14,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class EvaluationRadarPainter extends CustomPainter {
  const EvaluationRadarPainter(this.values);

  final List<double?> values;

  @override
  void paint(Canvas canvas, Size size) {
    if (values.length < 3) return;
    final center = size.center(Offset.zero);
    final radius = math.min(size.width / 2 - 66, size.height / 2 - 54);
    if (radius <= 0) return;
    final grid = Paint()
      ..color = const Color(0xFFE5E7EB)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1;
    for (final level in const <double>[1, 0.75, 0.5, 0.25]) {
      canvas.drawPath(
        _polygon(center, radius, List.filled(values.length, level)),
        grid,
      );
    }
    for (var index = 0; index < values.length; index++) {
      canvas.drawLine(
        center,
        evaluationRadarPoint(
          center: center,
          radius: radius,
          index: index,
          axisCount: values.length,
          scale: 1,
        ),
        grid,
      );
    }
    final normalized = values
        .map((value) => (value ?? 0).clamp(0.0, 1.0))
        .toList(growable: false);
    final data = _polygon(center, radius, normalized);
    final radarBounds = Rect.fromCircle(center: center, radius: radius);
    final radarColors = <Color>[
      for (var index = 0; index < normalized.length; index++)
        evaluationRadarAccent(index),
      evaluationRadarAccent(0),
    ];
    canvas.drawPath(
      data,
      Paint()
        ..shader = SweepGradient(
          colors: [
            for (final color in radarColors) color.withValues(alpha: 0.16),
          ],
        ).createShader(radarBounds)
        ..style = PaintingStyle.fill,
    );
    canvas.drawPath(
      data,
      Paint()
        ..shader = SweepGradient(colors: radarColors).createShader(radarBounds)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2.4
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round,
    );
    for (var index = 0; index < normalized.length; index++) {
      final point = evaluationRadarPoint(
        center: center,
        radius: radius,
        index: index,
        axisCount: normalized.length,
        scale: normalized[index],
      );
      canvas.drawCircle(point, 3.5, Paint()..color = Colors.white);
      canvas.drawCircle(
        point,
        2.2,
        Paint()..color = evaluationRadarAccent(index),
      );
    }
  }

  Path _polygon(Offset center, double radius, List<double> scales) {
    final points = <Offset>[
      for (var index = 0; index < scales.length; index++)
        evaluationRadarPoint(
          center: center,
          radius: radius,
          index: index,
          axisCount: scales.length,
          scale: scales[index],
        ),
    ];
    return Path()
      ..moveTo(points.first.dx, points.first.dy)
      ..addPolygon(points, true);
  }

  @override
  bool shouldRepaint(covariant EvaluationRadarPainter oldDelegate) {
    if (values.length != oldDelegate.values.length) return true;
    for (var index = 0; index < values.length; index++) {
      if (values[index] != oldDelegate.values[index]) return true;
    }
    return false;
  }
}

Offset evaluationRadarPoint({
  required Offset center,
  required double radius,
  required int index,
  required int axisCount,
  required double scale,
}) {
  final angle = -math.pi / 2 + math.pi * 2 * index / axisCount;
  return Offset(
    center.dx + math.cos(angle) * radius * scale,
    center.dy + math.sin(angle) * radius * scale,
  );
}
