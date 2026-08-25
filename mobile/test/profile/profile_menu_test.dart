import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';
import 'package:speakup/features/profile/profile_page.dart';
import 'package:speakup/identity/model/identity_models.dart';

void main() {
  testWidgets('more menu owns coaching memory and opens its existing editor', (
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
    expect(find.text('当前 IELTS 能力'), findsOneWidget);

    await tester.tap(find.byKey(const Key('profile-account-menu')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('profile-coaching-memory-button')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('profile-logout-button')), findsOneWidget);
    final buttonBottom = tester
        .getBottomRight(find.byKey(const Key('profile-account-menu')))
        .dy;
    final menuTop = tester
        .getTopLeft(find.byKey(const Key('profile-coaching-memory-button')))
        .dy;
    expect(menuTop, greaterThan(buttonBottom));

    final anchor = tester.widget<MenuAnchor>(
      find.byKey(const Key('profile-more-menu-anchor')),
    );
    expect(anchor.style!.alignment, AlignmentDirectional.bottomEnd);
    expect(anchor.reservedPadding, const EdgeInsets.all(SpeakUpDesign.space16));
    expect(anchor.alignmentOffset, const Offset(-184, SpeakUpDesign.space4));
    expect(
      anchor.style!.maximumSize!.resolve(<WidgetState>{}),
      const Size(184, double.infinity),
    );
    final shape = anchor.style!.shape!.resolve(<WidgetState>{});
    expect(shape!.dimensions.vertical, SpeakUpDesign.space8);
    final menuPath = shape.getOuterPath(const Rect.fromLTWH(0, 0, 184, 120));
    expect(menuPath.contains(const Offset(162, 4)), isTrue);

    await tester.tap(find.byKey(const Key('profile-coaching-memory-button')));
    await tester.pumpAndSettle();

    expect(find.text('教练记忆'), findsOneWidget);
    expect(find.widgetWithText(TextFormField, '职业'), findsOneWidget);
    expect(loggedOut, isFalse);
  });

  testWidgets('logout remains a distinct destructive menu action', (
    tester,
  ) async {
    var loggedOut = false;
    await tester.pumpWidget(_app(onLogout: () => loggedOut = true));

    await tester.tap(find.byKey(const Key('profile-account-menu')));
    await tester.pumpAndSettle();

    final logout = tester.widget<MenuItemButton>(
      find.byKey(const Key('profile-logout-button')),
    );
    expect(
      logout.style!.foregroundColor!.resolve(<WidgetState>{}),
      SpeakUpDesign.error,
    );
    await tester.tap(find.byKey(const Key('profile-logout-button')));
    await tester.pumpAndSettle();
    expect(loggedOut, isTrue);
  });

  testWidgets('more menu remains usable on a narrow large-text screen', (
    tester,
  ) async {
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

    await tester.tap(find.byKey(const Key('profile-account-menu')));
    await tester.pumpAndSettle();

    expect(find.text('教练记忆'), findsOneWidget);
    expect(find.text('退出登录'), findsOneWidget);
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
