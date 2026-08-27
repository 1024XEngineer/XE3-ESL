import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/profile/coach_presentation_page.dart';

void main() {
  testWidgets('selects and saves the configured avatar and voice ids', (
    tester,
  ) async {
    final store = _Store();
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: CoachPresentationPage(store: store),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('莉萨'), findsOneWidget);
    expect(find.byType(Image), findsWidgets);
    await tester.drag(
      find.byKey(const Key('coach-avatar-carousel')),
      const Offset(-500, 0),
    );
    await tester.pumpAndSettle();
    expect(find.text('内森'), findsOneWidget);
    await tester.drag(find.byType(ListView), const Offset(0, -520));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const Key('coach-voice-male')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('coach-voice-male')));
    await tester.ensureVisible(
      find.byKey(const Key('coach-presentation-save')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('coach-presentation-save')));
    await tester.pumpAndSettle();

    expect(store.avatarId, '1843ff9f-db3a-45de-be28-9c2b9d6412a3');
    expect(store.voiceId, 'loongjohn');
    expect(find.text('设置已保存'), findsOneWidget);
  });
}

final class _Store implements CoachPresentationSettingsStore {
  String avatarId = '94a60c13-e835-4bde-aa93-00a1cf178dcd';
  String voiceId = 'loongeva_v3.6';

  @override
  Future<({String avatarId, String voiceId})> load() async =>
      (avatarId: avatarId, voiceId: voiceId);

  @override
  Future<void> save({required String avatarId, required String voiceId}) async {
    this.avatarId = avatarId;
    this.voiceId = voiceId;
  }
}
