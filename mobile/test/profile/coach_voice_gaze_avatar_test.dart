import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/profile/coach_presentation_settings.dart';
import 'package:speakup/features/profile/coach_voice_gaze_avatar.dart';
import 'package:speakup/features/profile/coach_voice_selection_page.dart';

void main() {
  test('current coach voices receive distinct stable Gaze variants', () {
    final variants = previewCoachPresentationCatalog.voices
        .map((voice) => GazeVariant.fromVoiceId(voice.id))
        .toList(growable: false);

    expect(variants.toSet(), hasLength(variants.length));
    expect(
      GazeVariant.fromVoiceId('voice_ava'),
      const GazeVariant(
        shape: GazeShape.arch,
        eyes: GazeEyes.wide,
        spacing: GazeSpacing.snug,
        colorIndex: 5,
        rotation: -3,
        scale: 0.97,
      ),
    );
  });

  testWidgets('voice list renders Gaze colors and explicit gender icons', (
    tester,
  ) async {
    final options = previewCoachPresentationCatalog.voices
        .map(
          (voice) => CoachVoiceOption(
            id: voice.id,
            name: voice.displayName,
            description: voice.description,
            gender: voice.gender,
          ),
        )
        .toList(growable: false);

    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: CoachVoiceSelectionPage(
          options: options,
          initialVoiceId: 'voice_ava',
        ),
      ),
    );
    await tester.pumpAndSettle();

    final avatars = tester
        .widgetList<CoachVoiceGazeAvatar>(find.byType(CoachVoiceGazeAvatar))
        .toList(growable: false);
    expect(avatars, hasLength(options.length));
    expect(
      avatars.map((avatar) => avatar.variant.signature).toSet(),
      hasLength(options.length),
    );
    expect(
      avatars.every(
        (avatar) => CoachVoiceGazeAvatar.bodyColors.contains(avatar.bodyColor),
      ),
      isTrue,
    );

    final genderIcons = tester
        .widgetList<CoachVoiceGenderIcon>(find.byType(CoachVoiceGenderIcon))
        .toList(growable: false);
    expect(genderIcons, hasLength(options.length));
    expect(
      genderIcons
          .where((icon) => icon.gender == 'male')
          .every(
            (icon) =>
                icon.icon == Icons.male_rounded &&
                icon.color == CoachVoiceGenderIcon.maleColor,
          ),
      isTrue,
    );
    expect(
      genderIcons
          .where((icon) => icon.gender == 'female')
          .every(
            (icon) =>
                icon.icon == Icons.female_rounded &&
                icon.color == CoachVoiceGenderIcon.femaleColor,
          ),
      isTrue,
    );
    expect(genderIcons.where((icon) => icon.gender == 'male'), isNotEmpty);
    expect(genderIcons.where((icon) => icon.gender == 'female'), isNotEmpty);
  });
}
