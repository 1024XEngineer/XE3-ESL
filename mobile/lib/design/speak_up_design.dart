import 'package:flutter/material.dart';

/// The single visual source of truth for SpeakUp's mobile surfaces.
abstract final class SpeakUpDesign {
  static const canvasBase = Color(0xFFF7F8FC);
  static const surfaceSolid = Color(0xFFFFFFFF);
  static const surfaceGlassStrong = Color(0xD1FFFFFF);
  static const surfaceGlassSoft = Color(0xA3FFFFFF);
  static const surfaceTint = Color(0xB8F7FAFF);
  static const ink = Color(0xFF142033);
  static const inkSecondary = Color(0xFF5E6A7D);
  static const inkTertiary = Color(0xFF8A94A6);
  static const borderGlass = Color(0xC7FFFFFF);
  static const borderSubtle = Color(0xFFDDE5F0);
  static const cardShadow = Color(0x122D425E);
  static const cardDraggedShadow = Color(0x292D425E);
  static const primary = Color(0xFF0D6FD8);
  static const primaryPressed = Color(0xFF095FB9);
  static const primaryMuted = Color(0xFFE6F2FF);
  static const accentCyan = Color(0xFF30C7EA);
  static const accentViolet = Color(0xFF7668F2);
  static const accentMint = Color(0xFF35C9A2);
  static const accentAmber = Color(0xFFF2A91E);
  static const accentCoral = Color(0xFFE65E58);
  static const quickActionViolet = Color(0xFF5949DB);
  static const quickActionMint = Color(0xFF178B70);
  static const quickActionAmber = Color(0xFF9A6500);
  static const error = Color(0xFFC53B45);
  static const errorMuted = Color(0xFFFDECEF);

  // Compatibility aliases keep the existing pages token-driven while the
  // refreshed palette naturally propagates through the app.
  static const canvas = canvasBase;
  static const surface = surfaceSolid;
  static const surfaceMuted = surfaceTint;
  static const secondary = inkSecondary;
  static const tertiary = inkTertiary;
  static const border = borderSubtle;
  static const strongAction = primaryPressed;
  static const success = accentMint;
  static const successMuted = Color(0xFFE4FAF4);
  static const warning = accentAmber;
  static const accentBlue = Color(0xFFE5FAFF);
  static const accentBlueStrong = accentCyan;
  static const accentLavender = Color(0xFFF0EDFF);
  static const accentLavenderStrong = accentViolet;
  static const accentPeriwinkle = Color(0xFFEFECFF);
  static const accentSage = Color(0xFFE4FAF4);
  static const accentSageStrong = accentMint;

  static const ambientTop = Color(0xFFEEF7FA);
  static const skyTop = Color(0xFFA9D8F1);
  static const skyMiddle = Color(0xFFE7F4F9);
  static const focusTop = Color(0xFFF2F4F7);

  static const space4 = 4.0;
  static const space8 = 8.0;
  static const space12 = 12.0;
  static const space16 = 16.0;
  static const space20 = 20.0;
  static const space24 = 24.0;
  static const space32 = 32.0;
  static const space40 = 40.0;
  static const space48 = 48.0;

  static const radiusXs = 8.0;
  static const radiusControl = 14.0;
  static const radiusCompactCard = 18.0;
  static const radiusCard = 22.0;
  static const radiusMedia = 28.0;
  static const radiusSheet = 30.0;
  static const radiusPill = 999.0;

  static const minTapTarget = 44.0;
  static const standardControlHeight = 48.0;
  static const primaryActionHeight = 52.0;
  static const iconDefault = 20.0;
  static const iconNavigation = 22.0;
  static const iconState = 24.0;
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
    fontSize: 22,
    fontWeight: FontWeight.w700,
    height: 1.2,
    letterSpacing: -0.1,
  );

  static const cardTitle = TextStyle(
    color: ink,
    fontSize: 18,
    fontWeight: FontWeight.w700,
    height: 1.28,
  );

  static const body = TextStyle(
    color: secondary,
    fontSize: 16,
    fontWeight: FontWeight.w400,
    height: 1.45,
  );

  static const label = TextStyle(
    color: ink,
    fontSize: 14,
    fontWeight: FontWeight.w600,
    height: 1.3,
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
