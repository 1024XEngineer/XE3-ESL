import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/profile/profile_avatar_picker.dart';
import 'package:speakup/features/profile/profile_page.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';

void main() {
  testWidgets('avatar sheet exposes gallery, camera, and default actions', (
    tester,
  ) async {
    await tester.pumpWidget(_app(profile: _profile()));

    await tester.tap(find.byKey(const Key('profile-avatar-edit-button')));
    await tester.pumpAndSettle();

    expect(find.text('从相册选择'), findsOneWidget);
    expect(find.text('拍照'), findsOneWidget);
    expect(find.text('使用默认头像'), findsOneWidget);
    expect(
      tester
          .widget<ListTile>(find.byKey(const Key('profile-avatar-default')))
          .enabled,
      isFalse,
    );
  });

  testWidgets('gallery uploads and custom avatar can restore the default', (
    tester,
  ) async {
    var uploads = 0;
    var defaults = 0;
    final picker = _Picker();
    await tester.pumpWidget(
      _app(
        profile: _profile(customAvatar: true),
        picker: picker,
        onUpload: (_) async {
          uploads++;
          return null;
        },
        onDefault: () async {
          defaults++;
          return null;
        },
      ),
    );

    await tester.tap(find.byKey(const Key('profile-avatar-edit-button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('profile-avatar-gallery')));
    await tester.pumpAndSettle();
    expect(uploads, 1);
    expect(find.text('头像已更新'), findsOneWidget);

    await tester.tap(find.byKey(const Key('profile-avatar-edit-button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('profile-avatar-default')));
    await tester.pumpAndSettle();
    expect(defaults, 1);
  });
}

Widget _app({
  required UserProfile profile,
  ProfileAvatarPicker? picker,
  Future<String?> Function(UserAvatarImage)? onUpload,
  Future<String?> Function()? onDefault,
}) {
  return MaterialApp(
    home: ProfilePage(
      showBackButton: false,
      user: const User(id: 'user-1', email: 'learner@example.com'),
      profile: profile,
      profileErrorMessage: null,
      profileSaving: false,
      onSaveDisplayName: (_) async => null,
      avatarUrl: null,
      avatarSaving: false,
      onUploadAvatar: onUpload ?? (_) async => null,
      onUseDefaultAvatar: onDefault ?? () async => null,
      onLogout: null,
      reviewHistoryController: null,
      coachingProfileController: null,
      avatarPicker: picker,
    ),
  );
}

UserProfile _profile({bool customAvatar = false}) {
  final createdAt = DateTime.utc(2026, 8, 18, 8);
  return UserProfile(
    userId: 'user-1',
    displayName: '小林',
    profileVersion: customAvatar ? 2 : 1,
    avatar: customAvatar
        ? UserProfileAvatar(width: 512, height: 512, updatedAt: createdAt)
        : null,
    createdAt: createdAt,
    updatedAt: createdAt,
  );
}

final class _Picker implements ProfileAvatarPicker {
  @override
  Future<UserAvatarImage?> pickFromGallery() async => UserAvatarImage(
    contentType: 'image/png',
    bytes: Uint8List.fromList([1, 2, 3]),
  );

  @override
  Future<UserAvatarImage?> takePhoto() async => null;
}
