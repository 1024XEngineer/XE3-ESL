import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  testWidgets('profile avatar refreshes without rebuilding the shell route', (
    tester,
  ) async {
    final accountClient = _AccountClient();
    final authController = AuthController(
      identityClient: accountClient,
      profileClient: accountClient,
      avatarClient: accountClient,
      sessionStore: _SessionStore(),
    );
    await authController.initialize();

    final conversationController = ConversationController(
      client: FakeAgentClient(),
    );
    final composerController = ComposerController(
      conversationController: conversationController,
    );
    final practiceController = PracticeController(client: FakePracticeClient());
    addTearDown(authController.dispose);
    addTearDown(composerController.dispose);
    addTearDown(conversationController.dispose);
    addTearDown(practiceController.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SpeakUpShell(
          user: _AccountClient.user,
          authController: authController,
          conversationController: conversationController,
          composerController: composerController,
          practiceController: practiceController,
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();

    expect(_avatarProvider(tester), isA<MemoryImage>());

    expect(await authController.useDefaultAvatar(), isNull);
    await tester.pump();
    expect(_avatarProvider(tester), isA<AssetImage>());

    expect(
      await authController.updateAvatar(
        UserAvatarImage(contentType: 'image/png', bytes: _newAvatarBytes),
      ),
      isNull,
    );
    await tester.pump();
    final refreshed = _avatarProvider(tester) as MemoryImage;
    expect(refreshed.bytes, orderedEquals(_newAvatarBytes));
  });
}

ImageProvider<Object> _avatarProvider(WidgetTester tester) {
  final image = tester.widget<Image>(
    find
        .descendant(
          of: find.byKey(const Key('profile-avatar')),
          matching: find.byType(Image),
        )
        .first,
  );
  return image.image;
}

final Uint8List _initialAvatarBytes = Uint8List.fromList(
  base64Decode(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
  ),
);

final Uint8List _newAvatarBytes = Uint8List.fromList(
  base64Decode(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAAC0lEQVR42mP8/x8AAusB9Y9Z4pAAAAAASUVORK5CYII=',
  ),
);

final class _SessionStore implements SessionStore {
  String? _token = 'sess_avatar-refresh';

  @override
  Future<void> deleteToken() async => _token = null;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String value) async => _token = value;
}

final class _AccountClient
    implements IdentityClient, UserProfileClient, UserAvatarClient {
  static const user = User(id: 'user-1', email: 'learner@example.com');
  static final _createdAt = DateTime.utc(2026, 8, 18, 8);

  UserProfile _profile = _customProfile(1);
  Uint8List _content = Uint8List.fromList(_initialAvatarBytes);

  @override
  Future<User> currentUser({required String sessionToken}) async => user;

  @override
  Future<UserProfile> currentProfile({required String sessionToken}) async =>
      _profile;

  @override
  Future<UserAvatarContent> currentAvatarContent({
    required String sessionToken,
  }) async => UserAvatarContent(contentType: 'image/png', bytes: _content);

  @override
  Future<UserProfile> uploadAvatar({
    required String sessionToken,
    required UserAvatarImage image,
    required int expectedProfileVersion,
    required String idempotencyKey,
  }) async {
    _content = Uint8List.fromList(image.bytes);
    _profile = _customProfile(expectedProfileVersion + 1);
    return _profile;
  }

  @override
  Future<UserProfile> useDefaultAvatar({
    required String sessionToken,
    required int expectedProfileVersion,
  }) async {
    _profile = UserProfile(
      userId: user.id,
      displayName: 'Dada',
      profileVersion: expectedProfileVersion + 1,
      createdAt: _createdAt,
      updatedAt: _createdAt.add(const Duration(minutes: 1)),
    );
    return _profile;
  }

  @override
  Future<UserProfile> updateProfile({
    required String sessionToken,
    required String displayName,
    required int? expectedProfileVersion,
  }) async => throw UnimplementedError();

  @override
  Future<LoginResult> login({
    required String email,
    required String password,
  }) async => throw UnimplementedError();

  @override
  Future<void> logout({required String sessionToken}) async {}

  @override
  Future<User> register({
    required String email,
    required String password,
  }) async => throw UnimplementedError();

  static UserProfile _customProfile(int version) => UserProfile(
    userId: user.id,
    displayName: 'Dada',
    profileVersion: version,
    avatar: UserProfileAvatar(width: 1, height: 1, updatedAt: _createdAt),
    createdAt: _createdAt,
    updatedAt: _createdAt,
  );
}
