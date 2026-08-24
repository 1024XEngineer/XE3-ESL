import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:url_launcher/url_launcher.dart';

final appReleaseMetadataUri = Uri(
  scheme: 'https',
  host: 'speak-up.top',
  path: '/downloads/android/release.json',
);

const _maximumReleaseMetadataBytes = 16 * 1024;
const _maximumSafeInteger = 9007199254740991;
const _automaticCheckInterval = Duration(hours: 24);
const _requestTimeout = Duration(seconds: 10);

typedef AppReleaseBodyLoader = Future<String> Function(Uri uri);
typedef InstalledAppVersionLoader = Future<InstalledAppVersion> Function();
typedef AppUpdateUriLauncher = Future<bool> Function(Uri uri);
typedef AppUpdateClock = DateTime Function();

final class AppRelease {
  const AppRelease._({
    required this.version,
    required this.versionCode,
    required this.publishedAt,
    required this.fileName,
    required this.downloadPath,
    required this.sizeBytes,
    required this.minimumAndroidApi,
    required this.abis,
    required this.apkSha256,
    required this.apkCertificateSha256,
  });

  final String version;
  final int versionCode;
  final DateTime publishedAt;
  final String fileName;
  final String downloadPath;
  final int sizeBytes;
  final int minimumAndroidApi;
  final List<String> abis;
  final String apkSha256;
  final String apkCertificateSha256;

  Uri get downloadUri => Uri(
    scheme: 'https',
    host: appReleaseMetadataUri.host,
    path: downloadPath,
  );
}

final class InstalledAppVersion {
  const InstalledAppVersion({required this.version, required this.versionCode});

  final String version;
  final int versionCode;
}

enum AppUpdateCheckStatus { available, upToDate, failed, skipped }

final class AppUpdateCheckResult {
  const AppUpdateCheckResult._({
    required this.status,
    this.installedVersion,
    this.release,
  });

  const AppUpdateCheckResult.available(
    InstalledAppVersion installedVersion,
    AppRelease release,
  ) : this._(
        status: AppUpdateCheckStatus.available,
        installedVersion: installedVersion,
        release: release,
      );

  const AppUpdateCheckResult.upToDate(
    InstalledAppVersion installedVersion,
    AppRelease release,
  ) : this._(
        status: AppUpdateCheckStatus.upToDate,
        installedVersion: installedVersion,
        release: release,
      );

  const AppUpdateCheckResult.failed()
    : this._(status: AppUpdateCheckStatus.failed);

  const AppUpdateCheckResult.skipped()
    : this._(status: AppUpdateCheckStatus.skipped);

  final AppUpdateCheckStatus status;
  final InstalledAppVersion? installedVersion;
  final AppRelease? release;
}

enum AppUpdateFailureKind { unavailable, invalidResponse }

final class AppUpdateException implements Exception {
  const AppUpdateException(this.kind);

  final AppUpdateFailureKind kind;

  @override
  String toString() => 'App update ${kind.name}.';
}

final class AppReleaseClient {
  AppReleaseClient({AppReleaseBodyLoader? bodyLoader})
    : _bodyLoader = bodyLoader ?? _loadReleaseBody;

  final AppReleaseBodyLoader _bodyLoader;

  Future<AppRelease> fetchLatest() async {
    late final String body;
    try {
      body = await _bodyLoader(appReleaseMetadataUri);
    } on AppUpdateException {
      rethrow;
    } on Object {
      throw const AppUpdateException(AppUpdateFailureKind.unavailable);
    }
    if (utf8.encode(body).length > _maximumReleaseMetadataBytes) {
      throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
    }
    try {
      return parseAppReleaseMetadata(jsonDecode(body));
    } on AppUpdateException {
      rethrow;
    } on Object {
      throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
    }
  }
}

abstract interface class AppUpdateCheckStore {
  Future<DateTime?> readLastAutomaticCheckAt();

  Future<void> writeLastAutomaticCheckAt(DateTime value);
}

final class SecureAppUpdateCheckStore implements AppUpdateCheckStore {
  const SecureAppUpdateCheckStore([
    this._storage = const FlutterSecureStorage(),
  ]);

  static const _lastCheckKey = 'app_update_last_automatic_check_at';

  final FlutterSecureStorage _storage;

  @override
  Future<DateTime?> readLastAutomaticCheckAt() async {
    final value = await _storage.read(key: _lastCheckKey);
    if (value == null) {
      return null;
    }
    final parsed = DateTime.tryParse(value);
    if (parsed == null || !parsed.isUtc) {
      throw const FormatException('Invalid app update check timestamp.');
    }
    return parsed;
  }

  @override
  Future<void> writeLastAutomaticCheckAt(DateTime value) {
    return _storage.write(
      key: _lastCheckKey,
      value: value.toUtc().toIso8601String(),
    );
  }
}

final class AppUpdateService {
  factory AppUpdateService({
    required AppReleaseClient releaseClient,
    required AppUpdateCheckStore checkStore,
    required InstalledAppVersionLoader installedVersionLoader,
    required AppUpdateUriLauncher uriLauncher,
    AppUpdateClock? clock,
  }) {
    return AppUpdateService._(
      releaseClient,
      checkStore,
      installedVersionLoader,
      uriLauncher,
      clock ?? DateTime.now,
    );
  }

  AppUpdateService._(
    this._releaseClient,
    this._checkStore,
    this._installedVersionLoader,
    this._uriLauncher,
    this._clock,
  );

  final AppReleaseClient _releaseClient;
  final AppUpdateCheckStore _checkStore;
  final InstalledAppVersionLoader _installedVersionLoader;
  final AppUpdateUriLauncher _uriLauncher;
  final AppUpdateClock _clock;

  InstalledAppVersion? _installedVersion;
  Future<InstalledAppVersion>? _installedVersionOperation;
  DateTime? _lastAutomaticCheckAt;
  bool _checkStoreRead = false;

  Future<InstalledAppVersion> loadInstalledVersion() {
    final installed = _installedVersion;
    if (installed != null) {
      return Future<InstalledAppVersion>.value(installed);
    }
    final existing = _installedVersionOperation;
    if (existing != null) {
      return existing;
    }
    late final Future<InstalledAppVersion> operation;
    operation = _installedVersionLoader()
        .then((value) {
          _installedVersion = value;
          return value;
        })
        .whenComplete(() {
          if (identical(_installedVersionOperation, operation)) {
            _installedVersionOperation = null;
          }
        });
    _installedVersionOperation = operation;
    return operation;
  }

  Future<AppUpdateCheckResult> check({required bool automatic}) async {
    if (automatic && !await _beginAutomaticCheck()) {
      return const AppUpdateCheckResult.skipped();
    }
    try {
      final installed = await loadInstalledVersion();
      final release = await _releaseClient.fetchLatest();
      if (release.versionCode > installed.versionCode) {
        return AppUpdateCheckResult.available(installed, release);
      }
      return AppUpdateCheckResult.upToDate(installed, release);
    } on Object {
      return const AppUpdateCheckResult.failed();
    }
  }

  Future<bool> openDownload(AppRelease release) async {
    if (!_hasTrustedDownloadLocation(release)) {
      return false;
    }
    try {
      return await _uriLauncher(release.downloadUri);
    } on Object {
      return false;
    }
  }

  Future<bool> _beginAutomaticCheck() async {
    final now = _clock().toUtc();
    if (!_checkStoreRead) {
      _checkStoreRead = true;
      try {
        _lastAutomaticCheckAt = await _checkStore.readLastAutomaticCheckAt();
      } on Object {
        // The timestamp only rate-limits traffic. Storage failure must not
        // suppress update discovery for the rest of the process.
      }
    }
    final lastCheck = _lastAutomaticCheckAt;
    if (lastCheck != null &&
        now.isBefore(lastCheck.add(_automaticCheckInterval))) {
      return false;
    }
    _lastAutomaticCheckAt = now;
    try {
      await _checkStore.writeLastAutomaticCheckAt(now);
    } on Object {
      // In-memory throttling remains active for this process.
    }
    return true;
  }
}

Future<InstalledAppVersion> loadInstalledAppVersion() async {
  final packageInfo = await PackageInfo.fromPlatform();
  final version = packageInfo.version.trim();
  final versionCode = int.tryParse(packageInfo.buildNumber);
  if (!_versionPattern.hasMatch(version) ||
      versionCode == null ||
      !_isPositiveSafeInteger(versionCode)) {
    throw const FormatException('Installed app version is invalid.');
  }
  return InstalledAppVersion(version: version, versionCode: versionCode);
}

Future<bool> launchAppUpdateUri(Uri uri) {
  return launchUrl(uri, mode: LaunchMode.externalApplication);
}

bool canPresentAutomaticAppUpdate({
  required bool routeCurrent,
  required bool practiceRouteInFlight,
  required bool conversationBusy,
  required bool composerActiveWorkflow,
  required bool practiceBusy,
  required bool practiceRecordingIdle,
}) {
  return routeCurrent &&
      !practiceRouteInFlight &&
      !conversationBusy &&
      !composerActiveWorkflow &&
      !practiceBusy &&
      practiceRecordingIdle;
}

AppRelease parseAppReleaseMetadata(Object? value) {
  const expectedKeys = <String>{
    'abis',
    'apk_certificate_sha256',
    'apk_sha256',
    'download_path',
    'file_name',
    'metadata_version',
    'minimum_android_api',
    'published_at',
    'size_bytes',
    'version',
    'version_code',
  };
  if (value is! Map<String, Object?> ||
      value.length != expectedKeys.length ||
      !value.keys.every(expectedKeys.contains)) {
    throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
  }

  final version = value['version'];
  final versionCode = value['version_code'];
  final publishedAtValue = value['published_at'];
  final fileName = value['file_name'];
  final downloadPath = value['download_path'];
  final sizeBytes = value['size_bytes'];
  final minimumAndroidApi = value['minimum_android_api'];
  final abis = value['abis'];
  final apkSha256 = value['apk_sha256'];
  final apkCertificateSha256 = value['apk_certificate_sha256'];

  if (value['metadata_version'] != 1 ||
      version is! String ||
      !_versionPattern.hasMatch(version) ||
      versionCode is! int ||
      !_isPositiveSafeInteger(versionCode) ||
      publishedAtValue is! String ||
      !_canonicalUtcPattern.hasMatch(publishedAtValue) ||
      fileName is! String ||
      fileName != 'speakup-v$version-production-arm64.apk' ||
      downloadPath is! String ||
      downloadPath != '/downloads/android/v$version/$fileName' ||
      sizeBytes is! int ||
      !_isPositiveSafeInteger(sizeBytes) ||
      minimumAndroidApi != 24 ||
      abis is! List<Object?> ||
      abis.length != 1 ||
      abis.single != 'arm64-v8a' ||
      apkSha256 is! String ||
      !_isNonzeroSha256(apkSha256) ||
      apkCertificateSha256 is! String ||
      !_isNonzeroSha256(apkCertificateSha256)) {
    throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
  }

  final publishedAt = DateTime.tryParse(publishedAtValue);
  if (publishedAt == null ||
      !publishedAt.isUtc ||
      _canonicalUtc(publishedAt) != publishedAtValue) {
    throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
  }

  final release = AppRelease._(
    version: version,
    versionCode: versionCode,
    publishedAt: publishedAt,
    fileName: fileName,
    downloadPath: downloadPath,
    sizeBytes: sizeBytes,
    minimumAndroidApi: minimumAndroidApi as int,
    abis: List<String>.unmodifiable(abis.cast<String>()),
    apkSha256: apkSha256,
    apkCertificateSha256: apkCertificateSha256,
  );
  if (!_hasTrustedDownloadLocation(release)) {
    throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
  }
  return release;
}

Future<String> _loadReleaseBody(Uri uri) async {
  if (uri != appReleaseMetadataUri) {
    throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
  }
  final client = HttpClient()..connectionTimeout = _requestTimeout;
  try {
    final request = await client.getUrl(uri).timeout(_requestTimeout);
    request
      ..followRedirects = false
      ..headers.set(HttpHeaders.acceptHeader, ContentType.json.mimeType)
      ..headers.set(HttpHeaders.cacheControlHeader, 'no-cache');
    final response = await request.close().timeout(_requestTimeout);
    if (response.statusCode != HttpStatus.ok) {
      throw const AppUpdateException(AppUpdateFailureKind.unavailable);
    }
    if (response.headers.contentType?.mimeType != ContentType.json.mimeType ||
        response.contentLength > _maximumReleaseMetadataBytes) {
      throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
    }
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.timeout(_requestTimeout)) {
      bytes.add(chunk);
      if (bytes.length > _maximumReleaseMetadataBytes) {
        throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
      }
    }
    try {
      return utf8.decode(bytes.takeBytes(), allowMalformed: false);
    } on FormatException {
      throw const AppUpdateException(AppUpdateFailureKind.invalidResponse);
    }
  } on AppUpdateException {
    rethrow;
  } on Object {
    throw const AppUpdateException(AppUpdateFailureKind.unavailable);
  } finally {
    client.close(force: true);
  }
}

bool _hasTrustedDownloadLocation(AppRelease release) {
  final expectedPath =
      '/downloads/android/v${release.version}/'
      'speakup-v${release.version}-production-arm64.apk';
  final uri = release.downloadUri;
  return release.fileName ==
          'speakup-v${release.version}-production-arm64.apk' &&
      release.downloadPath == expectedPath &&
      uri.scheme == 'https' &&
      uri.host == appReleaseMetadataUri.host &&
      !uri.hasPort &&
      uri.userInfo.isEmpty &&
      uri.path == expectedPath &&
      !uri.hasQuery &&
      !uri.hasFragment;
}

bool _isPositiveSafeInteger(int value) {
  return value >= 1 && value <= _maximumSafeInteger;
}

bool _isNonzeroSha256(String value) {
  return _sha256Pattern.hasMatch(value) &&
      !value.runes.every((rune) => rune == 48);
}

String _canonicalUtc(DateTime value) {
  return value.toUtc().toIso8601String().replaceFirst('.000Z', 'Z');
}

final _versionPattern = RegExp(r'^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$');
final _sha256Pattern = RegExp(r'^[0-9a-f]{64}$');
final _canonicalUtcPattern = RegExp(
  r'^\d{4}-(0[1-9]|1[0-2])-([012]\d|3[01])T([01]\d|2[0-3]):[0-5]\d:[0-5]\dZ$',
);
