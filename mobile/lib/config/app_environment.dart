const _developmentApiBaseUrl = 'http://127.0.0.1:8080';
const _stagingApiBaseUrl = 'https://staging-api.speak-up.top';
const _productionApiBaseUrl = 'https://api.speak-up.top';

Uri resolveApiBaseUri({
  required String? flavor,
  required bool isReleaseMode,
  required bool isAndroid,
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

  if (isReleaseMode && isAndroid) {
    throw StateError(
      'Android release builds require a known environment flavor.',
    );
  }
  if (isReleaseMode) {
    return _parseUnflavoredReleaseApiBaseUri(explicitBaseUrl);
  }
  return Uri.parse(_developmentApiBaseUrl);
}

Uri _parseUnflavoredReleaseApiBaseUri(String value) {
  final uri = Uri.tryParse(value);
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasFragment ||
      _isLoopbackHost(uri.host)) {
    throw StateError(
      'Non-Android release builds require an explicit non-loopback HTTPS API URL.',
    );
  }
  return uri;
}

bool _isLoopbackHost(String host) {
  final normalized = host.toLowerCase();
  return normalized == 'localhost' ||
      normalized == '127.0.0.1' ||
      normalized == '::1';
}
