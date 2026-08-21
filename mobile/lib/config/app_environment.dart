const _developmentApiBaseUrl = 'http://127.0.0.1:8080';
const _stagingApiBaseUrl = 'https://staging-api.speak-up.top';
const _productionApiBaseUrl = 'https://api.speak-up.top';

Uri resolveApiBaseUri({
  required String? flavor,
  required bool isReleaseMode,
  required String explicitBaseUrl,
}) {
  if (!isReleaseMode && explicitBaseUrl.isNotEmpty) {
    return Uri.parse(explicitBaseUrl);
  }

  final releaseBaseUrl = switch (flavor) {
    'staging' => _stagingApiBaseUrl,
    'production' => _productionApiBaseUrl,
    _ => null,
  };
  if (releaseBaseUrl != null) {
    if (explicitBaseUrl.isNotEmpty && explicitBaseUrl != releaseBaseUrl) {
      throw StateError('Release API URL does not match the selected flavor.');
    }
    return Uri.parse(releaseBaseUrl);
  }

  if (isReleaseMode) {
    throw StateError(
      'Android release builds require a known environment flavor.',
    );
  }
  return Uri.parse(_developmentApiBaseUrl);
}
