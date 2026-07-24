void validateIdentityHttpBaseUri(Uri uri) {
  if (uri.host.isEmpty || uri.userInfo.isNotEmpty) {
    throw ArgumentError('Identity base URI is invalid.');
  }
  if (uri.scheme == 'https') {
    return;
  }
  if (uri.scheme == 'http' && isLoopbackHost(uri.host)) {
    return;
  }
  throw ArgumentError(
    'Identity connections must use HTTPS outside loopback development.',
  );
}

bool isLoopbackHost(String host) {
  final normalized = host.toLowerCase();
  return normalized == 'localhost' ||
      normalized == '127.0.0.1' ||
      normalized == '::1';
}
