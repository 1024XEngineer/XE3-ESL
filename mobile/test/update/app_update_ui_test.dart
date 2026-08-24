import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/profile/profile_page.dart';
import 'package:speakup/features/update/app_update.dart';
import 'package:speakup/features/update/app_update_ui.dart';

void main() {
  testWidgets('cold start presents a validated available update once', (
    tester,
  ) async {
    var metadataRequests = 0;
    final service = _service(
      bodyLoader: (_) async {
        metadataRequests++;
        return jsonEncode(_metadata());
      },
    );

    await tester.pumpWidget(SpeakUpApp.preview(appUpdateService: service));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('app-update-dialog')), findsOneWidget);
    expect(find.text('发现新版本 v0.1.5'), findsOneWidget);
    expect(find.text('当前版本：v0.1.4'), findsOneWidget);
    expect(find.text('安装包：66.8 MB'), findsOneWidget);
    expect(
      find.byWidgetPredicate(
        (widget) =>
            widget is Text &&
            (widget.data?.startsWith('发布时间：2026-08-2') ?? false),
      ),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('app-update-later')));
    await tester.pumpAndSettle();
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('app-update-dialog')), findsNothing);
    expect(metadataRequests, 1);
  });

  testWidgets('profile shows the installed version and exposes manual check', (
    tester,
  ) async {
    var checks = 0;
    final service = _service();
    await tester.pumpWidget(
      MaterialApp(
        home: ProfilePage(
          showBackButton: false,
          user: null,
          profile: null,
          profileErrorMessage: null,
          profileSaving: false,
          onSaveDisplayName: null,
          avatarBytes: null,
          avatarSaving: false,
          onUploadAvatar: null,
          onUseDefaultAvatar: null,
          onLogout: null,
          reviewHistoryController: null,
          coachingProfileController: null,
          appUpdateService: service,
          onCheckForUpdate: () async => checks++,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.byKey(const Key('profile-app-update-section')),
      240,
    );

    expect(find.text('当前 v0.1.4（5）'), findsOneWidget);
    await tester.tap(find.byKey(const Key('profile-check-update')));
    await tester.pump();

    expect(checks, 1);
  });

  testWidgets('version section remains usable on a narrow large-text screen', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 3;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: AppUpdateSection(
                service: _service(),
                checking: false,
                onCheck: () async {},
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.byKey(const Key('profile-check-update')), findsOneWidget);
  });

  testWidgets('manual current and failure results are explicit', (
    tester,
  ) async {
    final currentService = _service(
      installedVersion: const InstalledAppVersion(
        version: '0.1.5',
        versionCode: 6,
      ),
      store: _MemoryCheckStore(value: DateTime.now().toUtc()),
    );
    await tester.pumpWidget(
      SpeakUpApp.preview(appUpdateService: currentService),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.byKey(const Key('profile-check-update')),
      240,
    );
    await tester.tap(find.byKey(const Key('profile-check-update')));
    await tester.pumpAndSettle();

    expect(find.text('已是最新版本 v0.1.5'), findsOneWidget);

    final failedService = _service(
      bodyLoader: (_) async => throw StateError('offline'),
      store: _MemoryCheckStore(value: DateTime.now().toUtc()),
    );
    await tester.pumpWidget(
      SpeakUpApp.preview(key: UniqueKey(), appUpdateService: failedService),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.byKey(const Key('profile-check-update')),
      240,
    );
    await tester.tap(find.byKey(const Key('profile-check-update')));
    await tester.pumpAndSettle();

    expect(find.text('暂时无法检查更新，请检查网络后重试'), findsOneWidget);
  });

  testWidgets('immediate update opens the exact versioned APK URL', (
    tester,
  ) async {
    Uri? launched;
    final service = _service(
      uriLauncher: (uri) async {
        launched = uri;
        return true;
      },
    );
    await tester.pumpWidget(SpeakUpApp.preview(appUpdateService: service));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('app-update-now')));
    await tester.pumpAndSettle();

    expect(
      launched,
      Uri.parse(
        'https://speak-up.top/downloads/android/v0.1.5/'
        'speakup-v0.1.5-production-arm64.apk',
      ),
    );
  });
}

AppUpdateService _service({
  InstalledAppVersion installedVersion = const InstalledAppVersion(
    version: '0.1.4',
    versionCode: 5,
  ),
  AppReleaseBodyLoader? bodyLoader,
  AppUpdateCheckStore? store,
  AppUpdateUriLauncher? uriLauncher,
}) {
  return AppUpdateService(
    releaseClient: AppReleaseClient(
      bodyLoader: bodyLoader ?? (_) async => jsonEncode(_metadata()),
    ),
    checkStore: store ?? _MemoryCheckStore(),
    installedVersionLoader: () async => installedVersion,
    uriLauncher: uriLauncher ?? (_) async => true,
    clock: DateTime.now,
  );
}

Map<String, Object?> _metadata() => <String, Object?>{
  'metadata_version': 1,
  'version': '0.1.5',
  'version_code': 6,
  'published_at': '2026-08-23T20:16:29Z',
  'file_name': 'speakup-v0.1.5-production-arm64.apk',
  'download_path':
      '/downloads/android/v0.1.5/speakup-v0.1.5-production-arm64.apk',
  'size_bytes': 70024938,
  'minimum_android_api': 24,
  'abis': <String>['arm64-v8a'],
  'apk_sha256': '1' * 64,
  'apk_certificate_sha256': '2' * 64,
};

final class _MemoryCheckStore implements AppUpdateCheckStore {
  _MemoryCheckStore({this.value});

  DateTime? value;

  @override
  Future<DateTime?> readLastAutomaticCheckAt() async => value;

  @override
  Future<void> writeLastAutomaticCheckAt(DateTime value) async {
    this.value = value;
  }
}
