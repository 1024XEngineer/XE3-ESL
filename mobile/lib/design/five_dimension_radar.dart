import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

final class FiveDimensionScore {
  const FiveDimensionScore({required this.label, required this.score})
    : assert(score >= 0 && score <= 100);

  final String label;
  final int score;
}

class FiveDimensionRadar extends StatelessWidget {
  const FiveDimensionRadar({required this.scores, super.key});

  final List<FiveDimensionScore> scores;

  @override
  Widget build(BuildContext context) {
    assert(scores.length == 5);
    return Semantics(
      label: scores.map((item) => '${item.label} ${item.score} 分').join('，'),
      child: SizedBox(
        width: double.infinity,
        height: 250,
        child: CustomPaint(painter: _FiveDimensionRadarPainter(scores)),
      ),
    );
  }
}

class _FiveDimensionRadarPainter extends CustomPainter {
  const _FiveDimensionRadarPainter(this.scores);

  final List<FiveDimensionScore> scores;

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2 + 4);
    final radius = math.min(size.width, size.height) * 0.29;
    final grid = Paint()
      ..color = SpeakUpDesign.border
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1;
    final fill = Paint()
      ..color = SpeakUpDesign.ink.withValues(alpha: 0.10)
      ..style = PaintingStyle.fill;
    final stroke = Paint()
      ..color = SpeakUpDesign.ink.withValues(alpha: 0.72)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2;
    final pointPaint = Paint()
      ..color = SpeakUpDesign.ink
      ..style = PaintingStyle.fill;

    Offset point(int index, double scale) {
      final angle = -math.pi / 2 + index * math.pi * 2 / scores.length;
      return center + Offset(math.cos(angle), math.sin(angle)) * radius * scale;
    }

    Path polygon(double scale) {
      final path = Path()..moveTo(point(0, scale).dx, point(0, scale).dy);
      for (var index = 1; index < scores.length; index++) {
        path.lineTo(point(index, scale).dx, point(index, scale).dy);
      }
      return path..close();
    }

    for (final scale in const [0.25, 0.5, 0.75, 1.0]) {
      canvas.drawPath(polygon(scale), grid);
    }
    for (var index = 0; index < scores.length; index++) {
      canvas.drawLine(center, point(index, 1), grid);
    }

    final result = Path();
    for (var index = 0; index < scores.length; index++) {
      final current = point(index, scores[index].score / 100);
      if (index == 0) {
        result.moveTo(current.dx, current.dy);
      } else {
        result.lineTo(current.dx, current.dy);
      }
    }
    result.close();
    canvas.drawPath(result, fill);
    canvas.drawPath(result, stroke);
    for (var index = 0; index < scores.length; index++) {
      canvas.drawCircle(point(index, scores[index].score / 100), 3, pointPaint);
    }

    for (var index = 0; index < scores.length; index++) {
      final anchor = point(index, 1.36);
      final score = scores[index];
      final text = TextPainter(
        text: TextSpan(
          text: '${score.label}\n${score.score}',
          style: const TextStyle(
            color: SpeakUpDesign.secondary,
            fontSize: 11,
            fontWeight: FontWeight.w600,
            height: 1.2,
          ),
        ),
        textAlign: TextAlign.center,
        textDirection: TextDirection.ltr,
      )..layout(maxWidth: 72);
      text.paint(canvas, anchor - Offset(text.width / 2, text.height / 2));
    }
  }

  @override
  bool shouldRepaint(covariant _FiveDimensionRadarPainter oldDelegate) =>
      oldDelegate.scores != scores;
}
