import 'package:flutter/material.dart';

/// The single visual source of truth for SpeakUp's mobile surfaces.
abstract final class SpeakUpDesign {
  static const canvas = Color(0xFFF6F7FA);
  static const surface = Color(0xFFFFFFFF);
  static const surfaceMuted = Color(0xFFF0F1F3);
  static const ink = Color(0xFF111216);
  static const secondary = Color(0xFF6E727B);
  static const tertiary = Color(0xFFA6A9B0);
  static const border = Color(0xFFE7E8EC);
  static const primary = Color(0xFF173B47);
  static const primaryMuted = Color(0xFFDDECF0);
  static const success = Color(0xFF285443);
  static const successMuted = Color(0xFFEAF3EF);
  static const error = Color(0xFF8A2D21);
  static const errorMuted = Color(0xFFFFEDE8);

  static const space4 = 4.0;
  static const space8 = 8.0;
  static const space12 = 12.0;
  static const space16 = 16.0;
  static const space20 = 20.0;
  static const space24 = 24.0;
  static const space32 = 32.0;

  static const radiusControl = 12.0;
  static const radiusCard = 20.0;
  static const radiusMedia = 24.0;
  static const radiusSheet = 28.0;

  static const minTapTarget = 44.0;
  static const maxContentWidth = 680.0;

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
    return MediaQuery.sizeOf(context).width < 360 ? space16 : space20;
  }

  static EdgeInsets pagePadding(
    BuildContext context, {
    bool hasPrimaryNavigation = false,
    double top = space16,
  }) {
    final horizontal = horizontalInset(context);
    final safeBottom = MediaQuery.viewPaddingOf(context).bottom;
    final bottom = (hasPrimaryNavigation ? 0.0 : safeBottom) + space24;
    return EdgeInsets.fromLTRB(horizontal, top, horizontal, bottom);
  }
}
