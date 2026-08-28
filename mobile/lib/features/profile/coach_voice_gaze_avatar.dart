import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';

/// A deterministic, local rendering of DiceBear's CC0 Gaze style.
///
/// Source: https://www.dicebear.com/styles/gaze/
class CoachVoiceGazeAvatar extends StatelessWidget {
  const CoachVoiceGazeAvatar({
    required this.voiceId,
    this.size = 48,
    super.key,
  });

  static const bodyColors = <Color>[
    Color(0xFFFBABAF),
    Color(0xFFFBB271),
    Color(0xFF9BD78D),
    Color(0xFF52DCD8),
    Color(0xFF9CC8FB),
    Color(0xFFD8B1FB),
  ];
  static const inkColor = Color(0xFF182230);

  final String voiceId;
  final double size;

  GazeVariant get variant => GazeVariant.fromVoiceId(voiceId);

  Color get bodyColor => bodyColors[variant.colorIndex];

  Color get backgroundColor => bodyColor.withValues(alpha: 0.18);

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: RepaintBoundary(
        child: SizedBox.square(
          dimension: size,
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: backgroundColor,
              shape: BoxShape.circle,
            ),
            child: CustomPaint(
              painter: _GazePainter(
                variant: variant,
                bodyColor: bodyColor,
                inkColor: inkColor,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class CoachVoiceGenderIcon extends StatelessWidget {
  const CoachVoiceGenderIcon({required this.gender, this.size = 18, super.key})
    : assert(gender == 'female' || gender == 'male');

  static const femaleColor = Color(0xFFE96F9D);
  static const maleColor = Color(0xFF4C8DFF);

  final String gender;
  final double size;

  IconData get icon =>
      gender == 'male' ? Icons.male_rounded : Icons.female_rounded;

  Color get color => gender == 'male' ? maleColor : femaleColor;

  @override
  Widget build(BuildContext context) => ExcludeSemantics(
    child: Icon(icon, size: size, color: color),
  );
}

enum GazeShape {
  arch,
  circle,
  column,
  diamond,
  egg,
  hexagon,
  octagon,
  pentagon,
  pill,
  square,
  triangle,
}

enum GazeEyes {
  bars,
  beans,
  big,
  dots,
  grin,
  happy,
  shine,
  small,
  squint,
  tall,
  wide,
}

enum GazeSpacing { close, snug, normal, wide, far }

@immutable
final class GazeVariant {
  const GazeVariant({
    required this.shape,
    required this.eyes,
    required this.spacing,
    required this.colorIndex,
    required this.rotation,
    required this.scale,
  });

  factory GazeVariant.fromVoiceId(String voiceId) {
    final hash = _stableHash(voiceId);
    return GazeVariant(
      shape: GazeShape.values[hash % GazeShape.values.length],
      eyes: GazeEyes
          .values[(hash ~/ GazeShape.values.length) % GazeEyes.values.length],
      spacing:
          GazeSpacing.values[(hash ~/
                  (GazeShape.values.length * GazeEyes.values.length)) %
              GazeSpacing.values.length],
      colorIndex: (hash ~/ 605) % CoachVoiceGazeAvatar.bodyColors.length,
      rotation: ((hash ~/ 605) % 13) - 6,
      scale: 0.96 + ((hash ~/ 7865) % 7) / 100,
    );
  }

  final GazeShape shape;
  final GazeEyes eyes;
  final GazeSpacing spacing;
  final int colorIndex;
  final int rotation;
  final double scale;

  String get signature => '${shape.name}:${eyes.name}:${spacing.name}';

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is GazeVariant &&
          shape == other.shape &&
          eyes == other.eyes &&
          spacing == other.spacing &&
          colorIndex == other.colorIndex &&
          rotation == other.rotation &&
          scale == other.scale;

  @override
  int get hashCode =>
      Object.hash(shape, eyes, spacing, colorIndex, rotation, scale);
}

int _stableHash(String value) {
  var hash = 0x811C9DC5;
  for (final byte in utf8.encode(value)) {
    hash ^= byte;
    hash = (hash * 0x01000193) & 0xFFFFFFFF;
  }
  return hash;
}

final class _GazePainter extends CustomPainter {
  const _GazePainter({
    required this.variant,
    required this.bodyColor,
    required this.inkColor,
  });

  final GazeVariant variant;
  final Color bodyColor;
  final Color inkColor;

  @override
  void paint(Canvas canvas, Size size) {
    final shortestSide = math.min(size.width, size.height);
    canvas.save();
    canvas.translate(
      (size.width - shortestSide) / 2,
      (size.height - shortestSide) / 2,
    );
    canvas.scale(shortestSide / 100);
    canvas.translate(50, 50);
    canvas.rotate(variant.rotation * math.pi / 180);
    canvas.scale(variant.scale);
    canvas.translate(-50, -50);

    _drawBody(canvas);
    _drawEyes(canvas);
    canvas.restore();
  }

  void _drawBody(Canvas canvas) {
    final paint = Paint()..color = bodyColor;
    switch (variant.shape) {
      case GazeShape.arch:
        final path = Path()
          ..moveTo(18, 86)
          ..lineTo(18, 47)
          ..cubicTo(18, 27, 31, 14, 50, 14)
          ..cubicTo(69, 14, 82, 27, 82, 47)
          ..lineTo(82, 86)
          ..close();
        canvas.drawPath(path, paint);
      case GazeShape.circle:
        canvas.drawCircle(const Offset(50, 50), 34, paint);
      case GazeShape.column:
        canvas.drawRRect(
          RRect.fromRectAndRadius(
            const Rect.fromLTWH(28, 11, 44, 78),
            const Radius.circular(22),
          ),
          paint,
        );
      case GazeShape.diamond:
        canvas.drawPath(
          Path()
            ..moveTo(50, 11)
            ..lineTo(89, 50)
            ..lineTo(50, 89)
            ..lineTo(11, 50)
            ..close(),
          paint,
        );
      case GazeShape.egg:
        final path = Path()
          ..moveTo(50, 13)
          ..cubicTo(69, 13, 79, 32, 79, 50)
          ..cubicTo(79, 72, 66, 86, 50, 86)
          ..cubicTo(34, 86, 21, 72, 21, 50)
          ..cubicTo(21, 32, 31, 13, 50, 13)
          ..close();
        canvas.drawPath(path, paint);
      case GazeShape.hexagon:
        canvas.drawPath(_polygonPath(6, 38, -math.pi / 2), paint);
      case GazeShape.octagon:
        canvas.drawPath(_polygonPath(8, 38, math.pi / 8), paint);
      case GazeShape.pentagon:
        canvas.drawPath(_polygonPath(5, 38, -math.pi / 2), paint);
      case GazeShape.pill:
        canvas.drawRRect(
          RRect.fromRectAndRadius(
            const Rect.fromLTWH(11, 25, 78, 50),
            const Radius.circular(25),
          ),
          paint,
        );
      case GazeShape.square:
        canvas.drawRRect(
          RRect.fromRectAndRadius(
            const Rect.fromLTWH(16, 16, 68, 68),
            const Radius.circular(15),
          ),
          paint,
        );
      case GazeShape.triangle:
        canvas.drawPath(
          Path()
            ..moveTo(50, 12)
            ..lineTo(88, 78)
            ..lineTo(12, 78)
            ..close(),
          paint,
        );
    }
  }

  Path _polygonPath(int sides, double radius, double startAngle) {
    final path = Path();
    for (var index = 0; index < sides; index++) {
      final angle = startAngle + index * math.pi * 2 / sides;
      final point = Offset(
        50 + math.cos(angle) * radius,
        50 + math.sin(angle) * radius,
      );
      if (index == 0) {
        path.moveTo(point.dx, point.dy);
      } else {
        path.lineTo(point.dx, point.dy);
      }
    }
    return path..close();
  }

  void _drawEyes(Canvas canvas) {
    final gap = switch (variant.spacing) {
      GazeSpacing.close => 9.5,
      GazeSpacing.snug => 11.0,
      GazeSpacing.normal => 12.5,
      GazeSpacing.wide => 14.0,
      GazeSpacing.far => 15.5,
    };
    final eyeY = switch (variant.shape) {
      GazeShape.triangle => 56.0,
      GazeShape.arch => 54.0,
      _ => 51.0,
    };
    _drawEye(canvas, Offset(50 - gap, eyeY));
    _drawEye(canvas, Offset(50 + gap, eyeY));
  }

  void _drawEye(Canvas canvas, Offset center) {
    final fill = Paint()..color = inkColor;
    switch (variant.eyes) {
      case GazeEyes.bars:
        canvas.drawRRect(
          RRect.fromRectAndRadius(
            Rect.fromCenter(center: center, width: 12, height: 6.7),
            const Radius.circular(3.4),
          ),
          fill,
        );
      case GazeEyes.beans:
        canvas.drawRRect(
          RRect.fromRectAndRadius(
            Rect.fromCenter(center: center, width: 6.7, height: 12),
            const Radius.circular(3.4),
          ),
          fill,
        );
      case GazeEyes.big:
        canvas.drawCircle(center, 6.2, fill);
      case GazeEyes.dots:
        canvas.drawCircle(center, 4.9, fill);
      case GazeEyes.grin:
        _drawArcEye(canvas, center, width: 13, rise: 7, strokeWidth: 3.8);
      case GazeEyes.happy:
        _drawArcEye(canvas, center, width: 11, rise: 5, strokeWidth: 3.4);
      case GazeEyes.shine:
        canvas.drawCircle(center, 5.6, fill);
        canvas.drawCircle(
          center.translate(2.0, -2.1),
          1.7,
          Paint()..color = Colors.white,
        );
      case GazeEyes.small:
        canvas.drawCircle(center, 3.6, fill);
      case GazeEyes.squint:
        _drawArcEye(canvas, center, width: 11, rise: 3.8, strokeWidth: 3.1);
      case GazeEyes.tall:
        canvas.drawOval(
          Rect.fromCenter(center: center, width: 8.3, height: 12.3),
          fill,
        );
      case GazeEyes.wide:
        canvas.drawOval(
          Rect.fromCenter(center: center, width: 12.3, height: 8.6),
          fill,
        );
    }
  }

  void _drawArcEye(
    Canvas canvas,
    Offset center, {
    required double width,
    required double rise,
    required double strokeWidth,
  }) {
    final path = Path()
      ..moveTo(center.dx - width / 2, center.dy + rise / 3)
      ..quadraticBezierTo(
        center.dx,
        center.dy - rise,
        center.dx + width / 2,
        center.dy + rise / 3,
      );
    canvas.drawPath(
      path,
      Paint()
        ..color = inkColor
        ..style = PaintingStyle.stroke
        ..strokeWidth = strokeWidth
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(_GazePainter oldDelegate) =>
      oldDelegate.variant != variant ||
      oldDelegate.bodyColor != bodyColor ||
      oldDelegate.inkColor != inkColor;
}
