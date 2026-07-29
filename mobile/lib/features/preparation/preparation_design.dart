import 'package:flutter/material.dart';

/// Visual tokens shared by the preparation discovery and selection surfaces.
///
/// These values intentionally stay local to the feature until the app-wide
/// visual foundation tracked by Issue #189 is available.
abstract final class PreparationDesign {
  static const canvas = Color(0xFFF6F7FA);
  static const surface = Colors.white;
  static const ink = Color(0xFF111216);
  static const secondary = Color(0xFF6E727B);
  static const tertiary = Color(0xFFA6A9B0);
  static const border = Color(0xFFE7E8EC);
  static const softSurface = Color(0xFFF0F1F3);

  static const interview = Color(0xFF20252A);
  static const interviewTint = Color(0xFFE9EAEC);
  static const ielts = Color(0xFF274B3E);
  static const ieltsTint = Color(0xFFF4EFE4);
  static const roleplay = Color(0xFF173B47);
  static const roleplayTint = Color(0xFFDDECF0);

  static const radiusControl = 12.0;
  static const radiusCard = 20.0;
  static const radiusMedia = 24.0;
  static const radiusHero = 28.0;

  static const pageTitle = TextStyle(
    color: ink,
    fontSize: 30,
    fontWeight: FontWeight.w800,
    height: 1.08,
    letterSpacing: -0.35,
  );

  static const sectionTitle = TextStyle(
    color: ink,
    fontSize: 20,
    fontWeight: FontWeight.w800,
    height: 1.2,
    letterSpacing: -0.1,
  );

  static const cardTitle = TextStyle(
    color: ink,
    fontSize: 16,
    fontWeight: FontWeight.w700,
    height: 1.3,
  );

  static const body = TextStyle(
    color: secondary,
    fontSize: 15,
    fontWeight: FontWeight.w400,
    height: 1.45,
  );

  static const label = TextStyle(
    color: ink,
    fontSize: 13,
    fontWeight: FontWeight.w600,
    height: 1.35,
  );

  static const meta = TextStyle(
    color: secondary,
    fontSize: 12,
    fontWeight: FontWeight.w500,
    height: 1.35,
  );

  static double horizontalInset(BuildContext context) {
    return MediaQuery.sizeOf(context).width < 360 ? 16 : 20;
  }

  static EdgeInsets pagePadding(
    BuildContext context, {
    required bool hasPrimaryNavigation,
    double top = 16,
  }) {
    final horizontal = horizontalInset(context);
    final safeBottom = MediaQuery.viewPaddingOf(context).bottom;
    final bottom = (hasPrimaryNavigation ? 0.0 : safeBottom) + 24;
    return EdgeInsets.fromLTRB(horizontal, top, horizontal, bottom);
  }
}

class PreparationContentWidth extends StatelessWidget {
  const PreparationContentWidth({required this.child, super.key});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 680),
        child: child,
      ),
    );
  }
}
