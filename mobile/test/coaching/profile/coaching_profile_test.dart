import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';

void main() {
  test('clear and memory toggle preserve the server version chain', () async {
    final client = _ProfileClient();
    final controller = CoachingProfileController(client: client);
    await controller.load();
    expect(await controller.setMemoryEnabled(false), isTrue);
    expect(controller.profile!.version, 2);
    expect(await controller.clear(), isTrue);
    expect(controller.profile!.version, 3);
    expect(controller.profile!.data.isEmpty, isTrue);
  });

  testWidgets('editor groups memory, identity, and communication settings', (
    tester,
  ) async {
    final controller = CoachingProfileController(client: _ProfileClient());
    await controller.load();
    await tester.pumpWidget(_app(controller));

    expect(find.byKey(const Key('coaching-profile-memory-panel')), findsOne);
    expect(find.text('关于你'), findsOne);
    expect(find.text('沟通偏好'), findsOne);
    expect(find.text('希望怎么称呼你'), findsOne);
    expect(find.text('职业'), findsOne);
    expect(find.text('职业背景'), findsOne);

    final panel = tester.widget<Container>(
      find.byKey(const Key('coaching-profile-memory-panel')),
    );
    final decoration = panel.decoration! as BoxDecoration;
    expect(decoration.color, SpeakUpDesign.surfaceMuted);
    expect(
      decoration.borderRadius,
      BorderRadius.circular(SpeakUpDesign.radiusCard),
    );

    await tester.scrollUntilVisible(
      find.byKey(const Key('coaching-profile-save-button')),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.byKey(const Key('coaching-profile-save-button')), findsOne);
    expect(find.byKey(const Key('coaching-profile-clear-button')), findsOne);
  });

  testWidgets('editor stays usable on a narrow large-text screen', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(320, 700);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    final controller = CoachingProfileController(client: _ProfileClient());
    await controller.load();

    await tester.pumpWidget(
      MediaQuery(
        data: const MediaQueryData(
          size: Size(320, 700),
          textScaler: TextScaler.linear(2),
        ),
        child: _app(controller),
      ),
    );
    await tester.scrollUntilVisible(
      find.byKey(const Key('coaching-profile-save-button')),
      320,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('coaching-profile-save-button')), findsOne);
    expect(find.byKey(const Key('coaching-profile-clear-button')), findsOne);
    expect(tester.takeException(), isNull);
  });
}

Widget _app(CoachingProfileController controller) => MaterialApp(
  theme: SpeakUpTheme.light,
  home: CoachingProfilePage(controller: controller),
);

final class _ProfileClient implements CoachingProfileClient {
  CoachingProfile value = const CoachingProfile(
    memoryEnabled: true,
    version: 1,
    data: CoachingProfileData(
      formOfAddress: 'Alex',
      occupation: '工程师',
      interests: ['音乐'],
    ),
  );

  @override
  Future<CoachingProfile> getProfile() async => value;

  @override
  Future<CoachingProfile> updateProfile({
    required int expectedVersion,
    CoachingProfileData? updates,
    List<String> forgetFields = const <String>[],
    bool clearProfile = false,
    bool? memoryEnabled,
  }) async {
    expect(expectedVersion, value.version);
    value = CoachingProfile(
      memoryEnabled: memoryEnabled ?? value.memoryEnabled,
      data: clearProfile ? const CoachingProfileData() : value.data,
      version: value.version + 1,
    );
    return value;
  }
}
