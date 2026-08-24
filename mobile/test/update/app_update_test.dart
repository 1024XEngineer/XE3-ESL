import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/update/app_update.dart';

void main() {
  group('AppReleaseClient', () {
    test('loads the fixed production metadata URL', () async {
      Uri? requestedUri;
      final client = AppReleaseClient(
        bodyLoader: (uri) async {
          requestedUri = uri;
          return jsonEncode(_validMetadata());
        },
      );

      final release = await client.fetchLatest();

      expect(requestedUri, appReleaseMetadataUri);
      expect(release.version, '0.1.5');
      expect(release.versionCode, 6);
      expect(
        release.downloadUri,
        Uri.parse(
          'https://speak-up.top/downloads/android/v0.1.5/'
          'speakup-v0.1.5-production-arm64.apk',
        ),
      );
    });

    test('rejects invalid JSON and oversized metadata', () async {
      final invalidJson = AppReleaseClient(bodyLoader: (_) async => '{');
      final oversized = AppReleaseClient(
        bodyLoader: (_) async => 'x' * (16 * 1024 + 1),
      );

      await expectLater(invalidJson.fetchLatest(), throwsA(_invalidResponse));
      await expectLater(oversized.fetchLatest(), throwsA(_invalidResponse));
    });

    for (final invalid in <Map<String, Object?>>[
      {..._validMetadata(), 'unexpected': true},
      {..._validMetadata()}..remove('version_code'),
      {..._validMetadata(), 'metadata_version': 2},
      {..._validMetadata(), 'version': '01.1.5'},
      {..._validMetadata(), 'version_code': 0},
      {..._validMetadata(), 'version_code': 6.0},
      {..._validMetadata(), 'published_at': '2026-02-31T20:16:29Z'},
      {..._validMetadata(), 'published_at': '2026-08-23T20:16:29.000Z'},
      {..._validMetadata(), 'file_name': 'speakup-latest.apk'},
      {..._validMetadata(), 'download_path': 'https://evil.example/app.apk'},
      {..._validMetadata(), 'size_bytes': 0},
      {..._validMetadata(), 'minimum_android_api': 23},
      {
        ..._validMetadata(),
        'abis': <String>['armeabi-v7a'],
      },
      {..._validMetadata(), 'apk_sha256': 'A' * 64},
      {..._validMetadata(), 'apk_certificate_sha256': '0' * 64},
    ]) {
      test('rejects metadata outside the release contract: $invalid', () {
        expect(
          () => parseAppReleaseMetadata(invalid),
          throwsA(_invalidResponse),
        );
      });
    }
  });

  group('AppUpdateService', () {
    test('uses versionCode to decide whether an update is available', () async {
      final available = _service(
        installedVersion: const InstalledAppVersion(
          version: '0.1.4',
          versionCode: 5,
        ),
      );
      final current = _service(
        installedVersion: const InstalledAppVersion(
          version: '0.1.5',
          versionCode: 6,
        ),
      );
      final newerLocalBuild = _service(
        installedVersion: const InstalledAppVersion(
          version: '0.1.6',
          versionCode: 7,
        ),
      );

      expect(
        (await available.check(automatic: false)).status,
        AppUpdateCheckStatus.available,
      );
      expect(
        (await current.check(automatic: false)).status,
        AppUpdateCheckStatus.upToDate,
      );
      expect(
        (await newerLocalBuild.check(automatic: false)).status,
        AppUpdateCheckStatus.upToDate,
      );
    });

    test('manual failures are represented as failures, not latest', () async {
      final service = _service(
        bodyLoader: (_) async => throw StateError('offline'),
      );

      final result = await service.check(automatic: false);

      expect(result.status, AppUpdateCheckStatus.failed);
      expect(result.release, isNull);
    });

    test('automatic checks persist and honor the 24 hour throttle', () async {
      final store = _MemoryCheckStore();
      var requests = 0;
      final first = _service(
        store: store,
        clock: () => DateTime.utc(2026, 8, 24, 8),
        bodyLoader: (_) async {
          requests++;
          return jsonEncode(_validMetadata());
        },
      );

      expect(
        (await first.check(automatic: true)).status,
        AppUpdateCheckStatus.available,
      );
      final restarted = _service(
        store: store,
        clock: () => DateTime.utc(2026, 8, 24, 9),
        bodyLoader: (_) async {
          requests++;
          return jsonEncode(_validMetadata());
        },
      );
      expect(
        (await restarted.check(automatic: true)).status,
        AppUpdateCheckStatus.skipped,
      );
      expect(requests, 1);
      expect(store.value, DateTime.utc(2026, 8, 24, 8));
    });

    test('manual checks bypass the automatic throttle', () async {
      var requests = 0;
      final service = _service(
        store: _MemoryCheckStore(value: DateTime.utc(2026, 8, 24, 8)),
        clock: () => DateTime.utc(2026, 8, 24, 9),
        bodyLoader: (_) async {
          requests++;
          return jsonEncode(_validMetadata());
        },
      );

      final result = await service.check(automatic: false);

      expect(result.status, AppUpdateCheckStatus.available);
      expect(requests, 1);
    });

    test(
      'a throttle store failure does not suppress update discovery',
      () async {
        final service = _service(store: _FailingCheckStore());

        final result = await service.check(automatic: true);

        expect(result.status, AppUpdateCheckStatus.available);
      },
    );

    test('opens only the validated versioned HTTPS APK URL', () async {
      Uri? launched;
      final service = _service(
        uriLauncher: (uri) async {
          launched = uri;
          return true;
        },
      );
      final result = await service.check(automatic: false);

      final opened = await service.openDownload(result.release!);

      expect(opened, isTrue);
      expect(
        launched,
        Uri.parse(
          'https://speak-up.top/downloads/android/v0.1.5/'
          'speakup-v0.1.5-production-arm64.apk',
        ),
      );
    });

    test(
      'caches the installed package version after a successful read',
      () async {
        var reads = 0;
        final service = _service(
          installedVersionLoader: () async {
            reads++;
            return const InstalledAppVersion(version: '0.1.4', versionCode: 5);
          },
        );

        await service.loadInstalledVersion();
        await service.check(automatic: false);

        expect(reads, 1);
      },
    );
  });

  group('automatic update presentation policy', () {
    test('allows a prompt only while the app is idle on the current route', () {
      expect(
        canPresentAutomaticAppUpdate(
          routeCurrent: true,
          practiceRouteInFlight: false,
          conversationBusy: false,
          composerActiveWorkflow: false,
          practiceBusy: false,
          practiceRecordingIdle: true,
        ),
        isTrue,
      );

      for (final blocked in <Map<String, bool>>[
        {'routeCurrent': false},
        {'practiceRouteInFlight': true},
        {'conversationBusy': true},
        {'composerActiveWorkflow': true},
        {'practiceBusy': true},
        {'practiceRecordingIdle': false},
      ]) {
        expect(
          canPresentAutomaticAppUpdate(
            routeCurrent: blocked['routeCurrent'] ?? true,
            practiceRouteInFlight: blocked['practiceRouteInFlight'] ?? false,
            conversationBusy: blocked['conversationBusy'] ?? false,
            composerActiveWorkflow: blocked['composerActiveWorkflow'] ?? false,
            practiceBusy: blocked['practiceBusy'] ?? false,
            practiceRecordingIdle: blocked['practiceRecordingIdle'] ?? true,
          ),
          isFalse,
          reason: '$blocked must defer the prompt',
        );
      }
    });
  });
}

final _invalidResponse = isA<AppUpdateException>().having(
  (error) => error.kind,
  'kind',
  AppUpdateFailureKind.invalidResponse,
);

Map<String, Object?> _validMetadata() => <String, Object?>{
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

AppUpdateService _service({
  InstalledAppVersion installedVersion = const InstalledAppVersion(
    version: '0.1.4',
    versionCode: 5,
  ),
  InstalledAppVersionLoader? installedVersionLoader,
  AppReleaseBodyLoader? bodyLoader,
  AppUpdateCheckStore? store,
  AppUpdateUriLauncher? uriLauncher,
  AppUpdateClock? clock,
}) {
  return AppUpdateService(
    releaseClient: AppReleaseClient(
      bodyLoader: bodyLoader ?? (_) async => jsonEncode(_validMetadata()),
    ),
    checkStore: store ?? _MemoryCheckStore(),
    installedVersionLoader:
        installedVersionLoader ?? () async => installedVersion,
    uriLauncher: uriLauncher ?? (_) async => true,
    clock: clock,
  );
}

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

final class _FailingCheckStore implements AppUpdateCheckStore {
  @override
  Future<DateTime?> readLastAutomaticCheckAt() {
    throw StateError('read failed');
  }

  @override
  Future<void> writeLastAutomaticCheckAt(DateTime value) {
    throw StateError('write failed');
  }
}
