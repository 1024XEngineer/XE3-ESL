import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/profile/coach_presentation_page.dart';
import 'package:speakup/features/profile/coach_presentation_settings.dart';
import 'package:speakup/features/profile/coach_voice_gaze_avatar.dart';
import 'package:speakup/features/profile/coach_voice_selection_page.dart';
import 'package:speakup/platform/audio/ephemeral_wav_audio_player.dart';

void main() {
  testWidgets('selects and saves the configured avatar and voice ids', (
    tester,
  ) async {
    final store = _Store();
    final player = _FakePreviewPlayer();
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: CoachPresentationPage(
          store: store,
          voicePreviewPlayerFactory: () => player,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('选择你喜欢的陪练形象和声音'), findsNothing);
    expect(find.text('莉萨'), findsOneWidget);
    expect(find.byType(Image), findsWidgets);
    await tester.drag(
      find.byKey(const Key('coach-avatar-carousel')),
      const Offset(-500, 0),
    );
    await tester.pumpAndSettle();
    expect(find.text('内森'), findsOneWidget);
    expect(store.avatarId, 'avatar_lisa');
    await tester.tap(find.byKey(const Key('coach-avatar-select-1001')));
    await tester.pumpAndSettle();
    expect(store.avatarId, 'avatar_nathan');
    await tester.drag(find.byType(ListView), const Offset(0, -520));
    await tester.pumpAndSettle();
    await tester.ensureVisible(
      find.byKey(const Key('coach-voice-selection-entry')),
    );
    await tester.pumpAndSettle();
    expect(find.text('清晰自然 · 美式英语 · 女声'), findsNothing);
    await tester.tap(find.byKey(const Key('coach-voice-selection-entry')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('coach-voice-selection-page')), findsOneWidget);
    expect(find.text('艾娃'), findsOneWidget);
    expect(find.text('约翰'), findsOneWidget);
    expect(find.text('玛丽'), findsOneWidget);
    expect(find.text('清晰自然 · 美式英语 · 女声'), findsOneWidget);
    await tester.ensureVisible(
      find.byKey(const Key('coach-voice-option-voice_ivy')),
    );
    await tester.pumpAndSettle();
    expect(find.text('艾薇'), findsOneWidget);
    await tester.tap(find.byKey(const Key('coach-voice-option-voice_ivy')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('coach-voice-selection-complete')),
      findsNothing,
    );
    expect(store.voiceId, 'voice_ivy');
    expect(store.previewVoiceIds, <String>['voice_ivy']);
    expect(player.playCount, 1);
    await tester.tap(find.byKey(const Key('coach-voice-option-voice_ivy')));
    await tester.pumpAndSettle();
    expect(store.previewVoiceIds, <String>['voice_ivy', 'voice_ivy']);
    expect(player.playCount, 2);
    await tester.pageBack();
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('coach-presentation-page')), findsOneWidget);
    expect(find.text('艾薇'), findsOneWidget);
    expect(store.avatarId, 'avatar_nathan');
    expect(store.voiceId, 'voice_ivy');
    final selectedVoiceAvatar = tester.widget<CoachVoiceGazeAvatar>(
      find.byKey(const Key('coach-selected-voice-gaze')),
    );
    expect(selectedVoiceAvatar.voiceId, 'voice_ivy');
    expect(
      CoachVoiceGazeAvatar.bodyColors,
      contains(selectedVoiceAvatar.bodyColor),
    );
    final selectedVoiceGender = tester.widget<CoachVoiceGenderIcon>(
      find.byKey(const Key('coach-selected-voice-gender')),
    );
    expect(selectedVoiceGender.gender, 'female');
    expect(selectedVoiceGender.icon, Icons.female_rounded);
    expect(find.byKey(const Key('coach-presentation-save')), findsNothing);
  });

  testWidgets('voice selection saves immediately and survives back', (
    tester,
  ) async {
    final store = _Store();
    final player = _FakePreviewPlayer();
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: CoachPresentationPage(
          store: store,
          voicePreviewPlayerFactory: () => player,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.drag(find.byType(ListView), const Offset(0, -520));
    await tester.pumpAndSettle();
    await tester.ensureVisible(
      find.byKey(const Key('coach-voice-selection-entry')),
    );
    await tester.tap(find.byKey(const Key('coach-voice-selection-entry')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('coach-voice-option-voice_john')));
    await tester.pageBack();
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('coach-presentation-page')), findsOneWidget);
    expect(find.text('约翰'), findsOneWidget);
    expect(store.voiceId, 'voice_john');
    expect(find.byKey(const Key('coach-presentation-save')), findsNothing);
  });

  testWidgets('coalesces a rapid avatar and voice change in order', (
    tester,
  ) async {
    final store = _ControlledStore();
    final player = _FakePreviewPlayer();
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: CoachPresentationPage(
          store: store,
          voicePreviewPlayerFactory: () => player,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.drag(
      find.byKey(const Key('coach-avatar-carousel')),
      const Offset(-500, 0),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('coach-avatar-select-1001')));
    await tester.pump();
    expect(store.calls, hasLength(1));
    expect(store.calls.single.avatarOptionId, 'avatar_nathan');

    await tester.drag(find.byType(ListView), const Offset(0, -520));
    await tester.pump();
    await tester.ensureVisible(
      find.byKey(const Key('coach-voice-selection-entry')),
    );
    await tester.tap(find.byKey(const Key('coach-voice-selection-entry')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('coach-voice-option-voice_john')));
    await tester.pump();

    store.completeNext(version: 1);
    await tester.pump();
    expect(store.calls, hasLength(2));
    expect(store.calls.last.avatarOptionId, 'avatar_nathan');
    expect(store.calls.last.voiceOptionId, 'voice_john');
    expect(store.calls.last.expectedVersion, 1);
    store.completeNext(version: 2);
    await tester.pumpAndSettle();

    await tester.pageBack();
    await tester.pumpAndSettle();

    expect(find.text('约翰'), findsOneWidget);
    expect(find.byKey(const Key('coach-presentation-saving')), findsNothing);
  });

  testWidgets('refreshes once and reapplies the desired choice on 409', (
    tester,
  ) async {
    final store = _ConflictStore();
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: CoachPresentationPage(store: store),
      ),
    );
    await tester.pumpAndSettle();

    await tester.drag(
      find.byKey(const Key('coach-avatar-carousel')),
      const Offset(-500, 0),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('coach-avatar-select-1001')));
    await tester.pumpAndSettle();

    expect(store.expectedVersions, <int>[0, 4]);
    expect(store.refreshCount, 1);
    expect(store.savedAvatarId, 'avatar_nathan');
    expect(store.savedVoiceId, 'voice_ava');
    expect(find.text('保存失败，请稍后重试。'), findsNothing);
  });

  testWidgets('ignores a stale preview after rapid voice switching', (
    tester,
  ) async {
    final player = _FakePreviewPlayer();
    final previews = <String, Completer<Uint8List>>{
      'voice_ava': Completer<Uint8List>(),
      'voice_john': Completer<Uint8List>(),
    };
    final options = previewCoachPresentationCatalog.voices
        .where((voice) => voice.id == 'voice_ava' || voice.id == 'voice_john')
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
          onSelected: (voiceOptionId) async => voiceOptionId,
          loadPreview: (voiceOptionId) => previews[voiceOptionId]!.future,
          audioPlayerFactory: () => player,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('coach-voice-option-voice_john')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('coach-voice-option-voice_ava')));
    await tester.pump();
    previews['voice_ava']!.complete(Uint8List.fromList(_previewWav));
    await tester.pumpAndSettle();
    expect(player.playCount, 1);
    expect(
      tester
          .widget<ListTile>(
            find.byKey(const Key('coach-voice-option-voice_ava')),
          )
          .selected,
      isTrue,
    );

    previews['voice_john']!.complete(Uint8List.fromList(_previewWav));
    await tester.pumpAndSettle();
    expect(player.playCount, 1);
  });
}

final class _Store implements CoachPresentationSettingsStore {
  String avatarId = 'avatar_lisa';
  String voiceId = 'voice_ava';
  int version = 0;
  final List<String> previewVoiceIds = <String>[];

  @override
  Future<CoachPresentationSettings> load({required String accountId}) async =>
      _settings;

  @override
  Future<CoachPresentationSettings> refresh({
    required String accountId,
  }) async => _settings;

  @override
  Future<CoachPresentationPreference> save({
    required String accountId,
    required String avatarOptionId,
    required String voiceOptionId,
    required int expectedVersion,
  }) async {
    avatarId = avatarOptionId;
    voiceId = voiceOptionId;
    version++;
    return CoachPresentationPreference(
      avatarOptionId: avatarId,
      voiceOptionId: voiceId,
      version: version,
    );
  }

  @override
  Future<Uint8List> previewVoice({
    required String accountId,
    required String voiceOptionId,
  }) async {
    previewVoiceIds.add(voiceOptionId);
    return Uint8List.fromList(_previewWav);
  }

  CoachPresentationSettings get _settings => CoachPresentationSettings(
    catalog: previewCoachPresentationCatalog,
    preference: CoachPresentationPreference(
      avatarOptionId: avatarId,
      voiceOptionId: voiceId,
      version: version,
    ),
  );
}

typedef _SaveCall = ({
  String avatarOptionId,
  String voiceOptionId,
  int expectedVersion,
});

final class _ControlledStore implements CoachPresentationSettingsStore {
  final List<_SaveCall> calls = <_SaveCall>[];
  final List<Completer<CoachPresentationPreference>> pending = [];

  @override
  Future<CoachPresentationSettings> load({required String accountId}) async =>
      const CoachPresentationSettings(
        catalog: previewCoachPresentationCatalog,
        preference: CoachPresentationPreference(
          avatarOptionId: 'avatar_lisa',
          voiceOptionId: 'voice_ava',
          version: 0,
        ),
      );

  @override
  Future<CoachPresentationSettings> refresh({required String accountId}) =>
      load(accountId: accountId);

  @override
  Future<CoachPresentationPreference> save({
    required String accountId,
    required String avatarOptionId,
    required String voiceOptionId,
    required int expectedVersion,
  }) {
    calls.add((
      avatarOptionId: avatarOptionId,
      voiceOptionId: voiceOptionId,
      expectedVersion: expectedVersion,
    ));
    final operation = Completer<CoachPresentationPreference>();
    pending.add(operation);
    return operation.future;
  }

  @override
  Future<Uint8List> previewVoice({
    required String accountId,
    required String voiceOptionId,
  }) async => Uint8List.fromList(_previewWav);

  void completeNext({required int version}) {
    final call = calls.last;
    pending
        .removeAt(0)
        .complete(
          CoachPresentationPreference(
            avatarOptionId: call.avatarOptionId,
            voiceOptionId: call.voiceOptionId,
            version: version,
          ),
        );
  }
}

final class _ConflictStore implements CoachPresentationSettingsStore {
  final List<int> expectedVersions = <int>[];
  int refreshCount = 0;
  String? savedAvatarId;
  String? savedVoiceId;

  @override
  Future<CoachPresentationSettings> load({required String accountId}) async =>
      const CoachPresentationSettings(
        catalog: previewCoachPresentationCatalog,
        preference: CoachPresentationPreference(
          avatarOptionId: 'avatar_lisa',
          voiceOptionId: 'voice_ava',
          version: 0,
        ),
      );

  @override
  Future<CoachPresentationSettings> refresh({required String accountId}) async {
    refreshCount++;
    return const CoachPresentationSettings(
      catalog: previewCoachPresentationCatalog,
      preference: CoachPresentationPreference(
        avatarOptionId: 'avatar_lisa',
        voiceOptionId: 'voice_john',
        version: 4,
      ),
    );
  }

  @override
  Future<CoachPresentationPreference> save({
    required String accountId,
    required String avatarOptionId,
    required String voiceOptionId,
    required int expectedVersion,
  }) async {
    expectedVersions.add(expectedVersion);
    if (expectedVersions.length == 1) {
      throw const CoachPresentationVersionConflict();
    }
    savedAvatarId = avatarOptionId;
    savedVoiceId = voiceOptionId;
    return CoachPresentationPreference(
      avatarOptionId: avatarOptionId,
      voiceOptionId: voiceOptionId,
      version: expectedVersion + 1,
    );
  }

  @override
  Future<Uint8List> previewVoice({
    required String accountId,
    required String voiceOptionId,
  }) async => Uint8List.fromList(_previewWav);
}

final class _FakePreviewPlayer implements EphemeralWavAudioPlayer {
  final StreamController<void> _completions =
      StreamController<void>.broadcast();
  int playCount = 0;

  @override
  Stream<void> get onComplete => _completions.stream;

  @override
  Future<void> play(Uint8List bytes) async {
    playCount++;
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> dispose() async {
    await _completions.close();
  }
}

const _previewWav = <int>[
  0x52,
  0x49,
  0x46,
  0x46,
  0x26,
  0,
  0,
  0,
  0x57,
  0x41,
  0x56,
  0x45,
  0x66,
  0x6d,
  0x74,
  0x20,
  0x10,
  0,
  0,
  0,
  1,
  0,
  1,
  0,
  0xc0,
  0x5d,
  0,
  0,
  0x80,
  0xbb,
  0,
  0,
  2,
  0,
  0x10,
  0,
  0x64,
  0x61,
  0x74,
  0x61,
  2,
  0,
  0,
  0,
  0,
  0,
];
