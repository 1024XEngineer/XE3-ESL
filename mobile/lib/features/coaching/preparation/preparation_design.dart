import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';

/// Visual tokens shared by the preparation discovery and selection surfaces.
///
abstract final class PreparationDesign {
  static const canvas = SpeakUpDesign.canvas;
  static const surface = SpeakUpDesign.surface;
  static const surfaceMuted = SpeakUpDesign.surfaceMuted;
  static const ink = SpeakUpDesign.ink;
  static const inkSecondary = SpeakUpDesign.secondary;
  static const inkTertiary = SpeakUpDesign.tertiary;
  static const secondary = SpeakUpDesign.secondary;
  static const tertiary = SpeakUpDesign.tertiary;
  static const border = SpeakUpDesign.border;
  static const softSurface = SpeakUpDesign.surfaceMuted;
  static const error = SpeakUpDesign.error;
  static const errorMuted = SpeakUpDesign.errorMuted;

  static const interview = SpeakUpDesign.strongAction;
  static const interviewTint = SpeakUpDesign.successMuted;
  static const ielts = SpeakUpDesign.accentLavenderStrong;
  static const ieltsDeep = SpeakUpDesign.accentLavenderStrong;
  static const ieltsTint = SpeakUpDesign.accentPeriwinkle;
  static const ieltsBorder = SpeakUpDesign.accentLavender;
  static const scenario = SpeakUpDesign.accentSageStrong;
  static const scenarioTint = SpeakUpDesign.accentSage;

  static const radiusControl = SpeakUpDesign.radiusControl;
  static const radiusCard = SpeakUpDesign.radiusCard;
  static const radiusMedia = SpeakUpDesign.radiusMedia;
  static const radiusHero = SpeakUpDesign.radiusSheet;

  static const primary = SpeakUpDesign.primary;
  static const motionState = SpeakUpDesign.motionState;
  static const motionEaseOut = SpeakUpDesign.motionEaseOut;

  static const pageTitle = SpeakUpDesign.pageTitle;
  static const sectionTitle = SpeakUpDesign.sectionTitle;
  static const cardTitle = SpeakUpDesign.cardTitle;
  static const body = SpeakUpDesign.body;
  static const label = SpeakUpDesign.label;
  static const meta = SpeakUpDesign.meta;

  static double horizontalInset(BuildContext context) {
    return SpeakUpDesign.horizontalInset(context);
  }

  static EdgeInsets pagePadding(
    BuildContext context, {
    required bool hasPrimaryNavigation,
    double top = 16,
  }) {
    return SpeakUpDesign.pagePadding(
      context,
      hasPrimaryNavigation: hasPrimaryNavigation,
      top: top,
    );
  }
}

class PreparationContentWidth extends StatelessWidget {
  const PreparationContentWidth({required this.child, super.key});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return SpeakUpContentWidth(child: child);
  }
}
