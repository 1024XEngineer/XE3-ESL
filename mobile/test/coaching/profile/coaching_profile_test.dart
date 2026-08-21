import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';

void main() {
  testWidgets('loads coaching memory and opens its editor', (tester) async {
    final client = _ProfileClient();
    final controller = CoachingProfileController(client: client);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: CoachingProfileCard(controller: controller)),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Alex · 工程师 · 音乐'), findsOneWidget);
    await tester.tap(find.byKey(const Key('coaching-profile-card')));
    await tester.pumpAndSettle();
    expect(find.text('教练记忆'), findsWidgets);
    expect(find.widgetWithText(TextFormField, '职业'), findsOneWidget);
  });

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
}

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
