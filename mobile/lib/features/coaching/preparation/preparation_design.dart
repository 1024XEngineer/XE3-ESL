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

  static const interview = SpeakUpDesign.ink;
  static const interviewTint = SpeakUpDesign.primaryMuted;
  static const ielts = SpeakUpDesign.ink;
  static const ieltsDeep = SpeakUpDesign.ink;
  static const ieltsTint = SpeakUpDesign.primaryMuted;
  static const ieltsBorder = SpeakUpDesign.border;
  static const scenario = SpeakUpDesign.ink;
  static const scenarioTint = SpeakUpDesign.primaryMuted;

  static const radiusControl = SpeakUpDesign.radiusControl;
  static const radiusCard = SpeakUpDesign.radiusCard;
  static const radiusMedia = SpeakUpDesign.radiusMedia;
  static const radiusHero = SpeakUpDesign.radiusSheet;

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
