import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/profile/about_speak_up_page.dart';
import 'package:speakup/features/profile/profile_page.dart';
import 'package:speakup/features/update/app_update.dart';

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

  testWidgets('deferred update appears after a blocking modal closes', (
    tester,
  ) async {
    final metadata = Completer<String>();
    final requestStarted = Completer<void>();
    final service = _service(
      bodyLoader: (_) {
        requestStarted.complete();
        return metadata.future;
      },
    );

    await tester.pumpWidget(SpeakUpApp.preview(appUpdateService: service));
    await tester.pump();
    await requestStarted.future;

    final shellContext = tester.element(
      find.byKey(const Key('primary-tab-agent')),
    );
    unawaited(
      showModalBottomSheet<void>(
        context: shellContext,
        builder: (_) =>
            const SizedBox(key: Key('blocking-update-modal'), height: 120),
      ),
    );
    await tester.pumpAndSettle();

    metadata.complete(jsonEncode(_metadata()));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('app-update-dialog')), findsNothing);

    Navigator.of(
      tester.element(find.byKey(const Key('blocking-update-modal'))),
    ).pop();
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('app-update-dialog')), findsOneWidget);
  });

  testWidgets('profile keeps version and manual check inside About SpeakUp', (
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
          onCheckForUpdate: () async {
            checks++;
            return null;
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('profile-app-version')), findsNothing);
    await tester.tap(find.byKey(const Key('profile-about-button')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('about-speak-up-page')), findsOneWidget);
    expect(find.text('版本 v0.1.4 (5)'), findsOneWidget);
    await tester.tap(find.byKey(const Key('profile-check-update')));
    await tester.pump();

    expect(checks, 1);
  });

  testWidgets('About page remains usable on a narrow large-text screen', (
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
        home: AboutSpeakUpPage(
          appUpdateService: _service(),
          onCheckForUpdate: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.byKey(const Key('profile-check-update')), findsOneWidget);
  });

  testWidgets('About page keeps the approved compact proportions', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: AboutSpeakUpPage(
          appUpdateService: _service(),
          onCheckForUpdate: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    final section = tester.getRect(
      find.byKey(const Key('about-speak-up-actions')),
    );
    final action = tester.getRect(
      find.byKey(const Key('profile-check-update')),
    );

    expect(section.height, 170);
    expect(action.height, 56);
    expect(
      tester.getSize(find.byKey(const Key('about-speak-up-wordmark'))).height,
      54,
    );
    expect(
      tester.getSize(find.byKey(const Key('about-speak-up-app-icon'))),
      const Size.square(104),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'About page keeps update information when action is unavailable',
    (tester) async {
      PackageInfo.setMockInitialValues(
        appName: 'SpeakUp',
        packageName: 'top.speak-up.app',
        version: '0.1.8',
        buildNumber: '9',
        buildSignature: '',
      );
      await tester.pumpWidget(const MaterialApp(home: AboutSpeakUpPage()));
      await tester.pumpAndSettle();

      expect(find.text('版本 v0.1.8 (9)'), findsOneWidget);
      expect(find.byKey(const Key('profile-check-update')), findsOneWidget);
      expect(find.text('当前版本'), findsOneWidget);
      expect(find.text('产品官网'), findsOneWidget);
      expect(find.text('开源许可'), findsOneWidget);
      expect(find.text('© 2026 SpeakUp\nspeak-up.top'), findsOneWidget);
      expect(find.byKey(const Key('about-speak-up-page')), findsOneWidget);

      await tester.tap(find.byKey(const Key('profile-check-update')));
      await tester.pump();
      expect(find.text('当前平台请通过应用商店更新'), findsOneWidget);
    },
  );

  testWidgets('manual check reports an automatic check already in progress', (
    tester,
  ) async {
    final metadata = Completer<String>();
    final requestStarted = Completer<void>();
    var metadataRequests = 0;
    final service = _service(
      installedVersion: const InstalledAppVersion(
        version: '0.1.5',
        versionCode: 6,
      ),
      bodyLoader: (_) {
        metadataRequests++;
        requestStarted.complete();
        return metadata.future;
      },
    );

    await tester.pumpWidget(SpeakUpApp.preview(appUpdateService: service));
    await tester.pump();
    await requestStarted.future;
    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('profile-about-button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('profile-check-update')));
    await tester.pumpAndSettle();

    expect(metadataRequests, 1);
    expect(find.text('稍后再试'), findsOneWidget);

    metadata.complete(jsonEncode(_metadata()));
    await tester.pumpAndSettle();
  });

  testWidgets('About page opens the exact product website', (tester) async {
    Uri? launched;
    await tester.pumpWidget(
      MaterialApp(
        home: AboutSpeakUpPage(
          appUpdateService: _service(),
          websiteLauncher: (uri) async {
            launched = uri;
            return true;
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('about-speak-up-website')));
    await tester.pump();

    expect(launched, Uri.parse('https://speak-up.top'));
  });

  testWidgets('About page opens Flutter licenses with app information', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(home: AboutSpeakUpPage(appUpdateService: _service())),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('about-speak-up-licenses')));
    await tester.pumpAndSettle();

    expect(find.byType(LicensePage), findsOneWidget);
    expect(find.text('SpeakUp'), findsWidgets);
    expect(find.text('v0.1.4 (5)'), findsOneWidget);
  });

  testWidgets('update status remains usable with large text', (tester) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 3;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    final service = _service(
      installedVersion: const InstalledAppVersion(
        version: '0.1.5',
        versionCode: 6,
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: AboutSpeakUpPage(
          appUpdateService: service,
          onCheckForUpdate: () => service.check(automatic: false),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('profile-check-update')));
    await tester.pumpAndSettle();

    expect(find.text('已是最新版本'), findsOneWidget);
    expect(tester.takeException(), isNull);
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
    await tester.tap(find.byKey(const Key('profile-about-button')));
    await tester.pumpAndSettle();
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
    await tester.tap(find.byKey(const Key('profile-about-button')));
    await tester.pumpAndSettle();
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
