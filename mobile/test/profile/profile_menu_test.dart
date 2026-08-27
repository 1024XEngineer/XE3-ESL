import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';
import 'package:speakup/features/profile/profile_page.dart';
import 'package:speakup/identity/model/identity_models.dart';

void main() {
  testWidgets('profile settings own coaching memory and open its editor', (
    tester,
  ) async {
    final client = _ProfileClient();
    final controller = CoachingProfileController(client: client);
    var loggedOut = false;
    await tester.pumpWidget(
      _app(controller: controller, onLogout: () => loggedOut = true),
    );
    await tester.pumpAndSettle();

    expect(client.loads, 1);
    expect(find.byKey(const Key('coaching-profile-card')), findsNothing);
    expect(find.byKey(const Key('profile-account-menu')), findsNothing);
    expect(find.text('当前 IELTS 能力'), findsNothing);
    expect(
      find.byKey(const Key('profile-coaching-memory-button')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('profile-ielts-ability-button')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('profile-about-button')), findsOneWidget);
    expect(find.byKey(const Key('profile-logout-button')), findsOneWidget);
    expect(find.byIcon(Icons.tune_rounded), findsNothing);
    expect(find.byIcon(Icons.insights_outlined), findsNothing);
    expect(find.byIcon(Icons.info_outline_rounded), findsNothing);
    expect(find.byIcon(Icons.logout_rounded), findsNothing);
    for (final key in const [
      Key('profile-coaching-memory-button'),
      Key('profile-ielts-ability-button'),
      Key('profile-about-button'),
    ]) {
      final tile = tester.widget<ListTile>(
        find.descendant(of: find.byKey(key), matching: find.byType(ListTile)),
      );
      expect(tile.leading, isNull);
    }

    await tester.tap(find.byKey(const Key('profile-coaching-memory-button')));
    await tester.pumpAndSettle();

    expect(find.text('教练记忆'), findsOneWidget);
    expect(find.byKey(const Key('coaching-profile-page')), findsOneWidget);
    expect(find.text('关于你'), findsOneWidget);
    expect(loggedOut, isFalse);
  });

  testWidgets('IELTS ability opens on its own page', (tester) async {
    await tester.pumpWidget(_app(onLogout: () {}));

    await tester.tap(find.byKey(const Key('profile-ielts-ability-button')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('current-ielts-ability-page')), findsOneWidget);
    expect(find.text('IELTS 能力'), findsOneWidget);
    expect(find.text('当前 IELTS 能力'), findsOneWidget);
    expect(find.byKey(const Key('review-ability-empty')), findsOneWidget);
  });

  testWidgets('logout remains a distinct settings action', (tester) async {
    var loggedOut = false;
    await tester.pumpWidget(_app(onLogout: () => loggedOut = true));

    final logoutText = tester.widget<Text>(find.text('退出登录'));
    expect(logoutText.style?.color, SpeakUpDesign.error);
    expect(find.byIcon(Icons.logout_rounded), findsNothing);
    expect(
      find.descendant(
        of: find.byKey(const Key('profile-logout-button')),
        matching: find.byIcon(Icons.chevron_right_rounded),
      ),
      findsNothing,
    );
    await tester.tap(find.byKey(const Key('profile-logout-button')));
    await tester.pumpAndSettle();
    expect(loggedOut, isTrue);
  });

  testWidgets('About SpeakUp opens from its own compact group', (tester) async {
    await tester.pumpWidget(_app(onLogout: () {}));

    expect(find.byKey(const Key('profile-app-version')), findsNothing);
    await tester.tap(find.byKey(const Key('profile-about-button')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('about-speak-up-page')), findsOneWidget);
    expect(find.byKey(const Key('about-speak-up-wordmark')), findsOneWidget);
    expect(find.text('关于 SpeakUp'), findsOneWidget);
  });

  testWidgets('profile uses a compact centered visual hierarchy', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        controller: CoachingProfileController(client: _ProfileClient()),
        onLogout: () {},
      ),
    );
    await tester.pumpAndSettle();

    final avatar = tester.getRect(find.byKey(const Key('profile-avatar')));
    final page = tester.getRect(find.byKey(const Key('profile-page')));
    final coachingRow = tester.getRect(
      find.byKey(const Key('profile-coaching-memory-button')),
    );
    final logoutRow = tester.getRect(
      find.byKey(const Key('profile-logout-button')),
    );

    expect(avatar.size, const Size.square(108));
    expect(avatar.center.dx, closeTo(page.center.dx, 0.01));
    expect(coachingRow.height, 56);
    expect(logoutRow.height, 56);
  });

  testWidgets('settings remain usable on a narrow large-text screen', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(320, 700);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    await tester.pumpWidget(
      _app(
        controller: CoachingProfileController(client: _ProfileClient()),
        onLogout: () {},
        mediaQuery: const MediaQueryData(
          size: Size(320, 700),
          textScaler: TextScaler.linear(3),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.scrollUntilVisible(
      find.byKey(const Key('profile-logout-button')),
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();

    expect(find.text('记忆'), findsOneWidget);
    expect(find.text('能力'), findsOneWidget);
    expect(find.text('关于'), findsOneWidget);
    expect(find.text('退出登录'), findsOneWidget);
    expect(find.byKey(const Key('profile-account-menu')), findsNothing);
    expect(tester.takeException(), isNull);
  });
}

Widget _app({
  CoachingProfileController? controller,
  required VoidCallback onLogout,
  MediaQueryData? mediaQuery,
}) {
  final profilePage = ProfilePage(
    showBackButton: false,
    user: const User(id: 'user-1', email: 'learner@example.com'),
    profile: _profile(),
    profileErrorMessage: null,
    profileSaving: false,
    onSaveDisplayName: (_) async => null,
    avatarBytes: null,
    avatarSaving: false,
    onUploadAvatar: (_) async => null,
    onUseDefaultAvatar: () async => null,
    onLogout: onLogout,
    reviewHistoryController: null,
    coachingProfileController: controller,
  );
  return MaterialApp(
    theme: SpeakUpTheme.light,
    home: mediaQuery == null
        ? profilePage
        : MediaQuery(data: mediaQuery, child: profilePage),
  );
}

UserProfile _profile() {
  final createdAt = DateTime.utc(2026, 8, 25, 8);
  return UserProfile(
    userId: 'user-1',
    displayName: '小林',
    profileVersion: 1,
    createdAt: createdAt,
    updatedAt: createdAt,
  );
}

final class _ProfileClient implements CoachingProfileClient {
  int loads = 0;

  @override
  Future<CoachingProfile> getProfile() async {
    loads++;
    return const CoachingProfile(
      memoryEnabled: true,
      version: 1,
      data: CoachingProfileData(
        formOfAddress: 'Alex',
        occupation: '工程师',
        interests: ['音乐'],
      ),
    );
  }

  @override
  Future<CoachingProfile> updateProfile({
    required int expectedVersion,
    CoachingProfileData? updates,
    List<String> forgetFields = const <String>[],
    bool clearProfile = false,
    bool? memoryEnabled,
  }) => throw UnimplementedError();
}
