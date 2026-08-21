import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

abstract final class SpeakUpTheme {
  static ThemeData get light {
    const scheme = ColorScheme.light(
      primary: SpeakUpDesign.primary,
      onPrimary: Colors.white,
      primaryContainer: SpeakUpDesign.primaryMuted,
      onPrimaryContainer: SpeakUpDesign.ink,
      secondary: SpeakUpDesign.accentViolet,
      onSecondary: Colors.white,
      error: SpeakUpDesign.error,
      onError: Colors.white,
      surface: SpeakUpDesign.surface,
      onSurface: SpeakUpDesign.ink,
      outline: SpeakUpDesign.border,
      outlineVariant: SpeakUpDesign.border,
    );
    final baseTextTheme = ThemeData.light(useMaterial3: true).textTheme;

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: Colors.transparent,
      canvasColor: SpeakUpDesign.canvas,
      dividerColor: SpeakUpDesign.border,
      disabledColor: SpeakUpDesign.tertiary,
      textTheme: baseTextTheme.copyWith(
        displayLarge: SpeakUpDesign.pageTitle,
        displayMedium: SpeakUpDesign.pageTitle,
        headlineLarge: SpeakUpDesign.pageTitle,
        headlineMedium: SpeakUpDesign.sectionTitle,
        titleLarge: SpeakUpDesign.sectionTitle,
        titleMedium: SpeakUpDesign.cardTitle,
        bodyLarge: SpeakUpDesign.body,
        bodyMedium: SpeakUpDesign.body,
        labelLarge: SpeakUpDesign.label,
        labelMedium: SpeakUpDesign.meta,
      ),
      appBarTheme: const AppBarTheme(
        backgroundColor: Colors.transparent,
        foregroundColor: SpeakUpDesign.ink,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: SpeakUpDesign.sectionTitle,
      ),
      cardTheme: CardThemeData(
        color: SpeakUpDesign.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
          side: const BorderSide(color: SpeakUpDesign.border),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size.fromHeight(
            SpeakUpDesign.standardControlHeight,
          ),
          backgroundColor: SpeakUpDesign.primary,
          foregroundColor: Colors.white,
          disabledBackgroundColor: SpeakUpDesign.surfaceMuted,
          disabledForegroundColor: SpeakUpDesign.tertiary,
          padding: const EdgeInsets.symmetric(
            horizontal: SpeakUpDesign.space20,
            vertical: SpeakUpDesign.space12,
          ),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          ),
          textStyle: SpeakUpDesign.label,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: const Size.fromHeight(
            SpeakUpDesign.standardControlHeight,
          ),
          foregroundColor: SpeakUpDesign.ink,
          side: const BorderSide(color: SpeakUpDesign.border),
          padding: const EdgeInsets.symmetric(
            horizontal: SpeakUpDesign.space20,
            vertical: SpeakUpDesign.space12,
          ),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          ),
          textStyle: SpeakUpDesign.label,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          minimumSize: const Size(
            SpeakUpDesign.minTapTarget,
            SpeakUpDesign.minTapTarget,
          ),
          foregroundColor: SpeakUpDesign.primary,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          ),
          textStyle: SpeakUpDesign.label,
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: SpeakUpDesign.surface,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space16,
          vertical: SpeakUpDesign.space16,
        ),
        labelStyle: SpeakUpDesign.body,
        hintStyle: SpeakUpDesign.body,
        helperStyle: SpeakUpDesign.meta,
        errorStyle: SpeakUpDesign.meta.copyWith(color: SpeakUpDesign.error),
        border: _inputBorder(SpeakUpDesign.border),
        enabledBorder: _inputBorder(SpeakUpDesign.border),
        focusedBorder: _inputBorder(SpeakUpDesign.primary, width: 1.5),
        errorBorder: _inputBorder(SpeakUpDesign.error),
        focusedErrorBorder: _inputBorder(SpeakUpDesign.error, width: 1.5),
      ),
      chipTheme: const ChipThemeData(
        backgroundColor: SpeakUpDesign.surface,
        selectedColor: SpeakUpDesign.primaryMuted,
        disabledColor: SpeakUpDesign.surfaceMuted,
        side: BorderSide(color: SpeakUpDesign.border),
        shape: StadiumBorder(),
        padding: EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space8,
          vertical: SpeakUpDesign.space8,
        ),
        labelStyle: SpeakUpDesign.label,
        secondaryLabelStyle: SpeakUpDesign.label,
        showCheckmark: false,
      ),
      dividerTheme: const DividerThemeData(
        color: SpeakUpDesign.border,
        thickness: 1,
        space: 1,
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        backgroundColor: SpeakUpDesign.surface,
        surfaceTintColor: Colors.transparent,
        modalBackgroundColor: SpeakUpDesign.surface,
        modalBarrierColor: Color(0x52000000),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(
            top: Radius.circular(SpeakUpDesign.radiusSheet),
          ),
        ),
        showDragHandle: true,
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: SpeakUpDesign.surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        ),
        titleTextStyle: SpeakUpDesign.sectionTitle,
        contentTextStyle: SpeakUpDesign.body,
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: SpeakUpDesign.strongAction,
        contentTextStyle: SpeakUpDesign.body.copyWith(color: Colors.white),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        ),
      ),
      progressIndicatorTheme: const ProgressIndicatorThemeData(
        color: SpeakUpDesign.primary,
        linearTrackColor: SpeakUpDesign.surfaceMuted,
      ),
    );
  }

  static OutlineInputBorder _inputBorder(Color color, {double width = 1}) {
    return OutlineInputBorder(
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
      borderSide: BorderSide(color: color, width: width),
    );
  }
}
