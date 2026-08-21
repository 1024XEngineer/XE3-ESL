import 'package:flutter/material.dart';

/// The single visual source of truth for SpeakUp's mobile surfaces.
abstract final class SpeakUpDesign {
  // Professional blue with warm accent - refined and trustworthy
  static const canvas = Color(0xFFFAFAF9);
  static const surface = Color(0xFFFFFFFF);
  static const surfaceMuted = Color(0xFFF5F5F4);
  static const ink = Color(0xFF1F2937);
  static const secondary = Color(0xFF6B7280);
  static const tertiary = Color(0xFF9CA3AF);
  static const border = Color(0xFFE5E7EB);
  static const primary = Color(0xFF2D5F7F);
  static const primaryMuted = Color(0xFFD4E4ED);
  static const strongAction = Color(0xFF1E4A5F);
  static const success = Color(0xFF10B981);
  static const successMuted = Color(0xFFD1FAE5);
  static const error = Color(0xFFEF4444);
  static const errorMuted = Color(0xFFFEE2E2);
  static const accentBlue = Color(0xFFDEEBF0);
  static const accentBlueStrong = Color(0xFF2D5F7F);
  static const accentLavender = Color(0xFFEDE9FE);
  static const accentLavenderStrong = Color(0xFF7C3AED);
  static const accentPeriwinkle = Color(0xFFE0E7FF);
  static const accentSage = Color(0xFFFFE5DF);
  static const accentSageStrong = Color(0xFFFF7C5C);

  static const space4 = 4.0;
  static const space8 = 8.0;
  static const space12 = 12.0;
  static const space16 = 16.0;
  static const space20 = 20.0;
  static const space24 = 24.0;
  static const space32 = 32.0;

  static const radiusControl = 12.0;
  static const radiusCard = 16.0;
  static const radiusMedia = 20.0;
  static const radiusSheet = 24.0;

  static const minTapTarget = 44.0;
  static const maxContentWidth = 680.0;

  static const motionPress = Duration(milliseconds: 120);
  static const motionRelease = Duration(milliseconds: 160);
  static const motionState = Duration(milliseconds: 180);
  static const motionEaseOut = Cubic(0.23, 1, 0.32, 1);

  static const pageTitle = TextStyle(
    color: ink,
    fontSize: 30,
    fontWeight: FontWeight.w800,
    height: 1.08,
    letterSpacing: -0.35,
  );

  static const displayTitle = TextStyle(
    color: ink,
    fontFamily: 'Allura',
    fontSize: 48,
    fontWeight: FontWeight.w400,
    height: 1,
  );

  static const secondaryDisplayTitle = TextStyle(
    color: ink,
    fontFamily: 'Georgia',
    fontFamilyFallback: ['serif'],
    fontSize: 34,
    fontWeight: FontWeight.w600,
    fontStyle: FontStyle.italic,
    height: 1.05,
    letterSpacing: -0.4,
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
