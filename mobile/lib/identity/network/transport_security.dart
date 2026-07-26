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

const _maxCredentialDecodePasses = 8;
final RegExp _sessionCredentialMarker = RegExp(r'sess_', caseSensitive: false);

void validateNoSessionCredentialInUri(Uri uri, {String? sessionToken}) {
  final credentialSurface = <Object?>[
    uri.scheme,
    uri.authority,
    uri.userInfo,
    uri.host,
    uri.path,
    uri.query,
    uri.fragment,
    uri.toString(),
  ].join('\n');
  if (_containsSessionCredential(
    credentialSurface,
    sessionToken: sessionToken,
  )) {
    throw ArgumentError('Network URI must not contain session credentials.');
  }
}

final class TrustedIdentityHttpOrigin {
  factory TrustedIdentityHttpOrigin(Uri trustedBaseUri) {
    validateIdentityHttpBaseUri(trustedBaseUri);
    _validateStrictHttpUri(trustedBaseUri);
    validateNoSessionCredentialInUri(trustedBaseUri);
    return TrustedIdentityHttpOrigin._(
      scheme: trustedBaseUri.scheme,
      host: trustedBaseUri.host.toLowerCase(),
      port: _validatedEffectivePort(trustedBaseUri),
    );
  }

  const TrustedIdentityHttpOrigin._({
    required this.scheme,
    required this.host,
    required this.port,
  });

  final String scheme;
  final String host;
  final int port;

  void validateResourceUri(Uri uri) {
    validateIdentityHttpBaseUri(uri);
    _validateStrictHttpUri(uri);
    if (uri.scheme != scheme ||
        uri.host.toLowerCase() != host ||
        _validatedEffectivePort(uri) != port) {
      throw ArgumentError(
        'Authenticated HTTP URI must use the trusted origin.',
      );
    }
  }
}

final class TrustedIdentityWebSocketOrigin {
  factory TrustedIdentityWebSocketOrigin(Uri trustedBaseUri) {
    _validateWebSocketBaseUri(trustedBaseUri);
    _validateStrictNetworkUri(trustedBaseUri);
    validateNoSessionCredentialInUri(trustedBaseUri);
    return TrustedIdentityWebSocketOrigin._(
      scheme: trustedBaseUri.scheme,
      host: trustedBaseUri.host.toLowerCase(),
      port: _validatedEffectivePort(trustedBaseUri),
    );
  }

  const TrustedIdentityWebSocketOrigin._({
    required this.scheme,
    required this.host,
    required this.port,
  });

  final String scheme;
  final String host;
  final int port;

  void validateResourceUri(Uri uri) {
    _validateWebSocketBaseUri(uri);
    _validateStrictNetworkUri(uri);
    if (uri.scheme != scheme ||
        uri.host.toLowerCase() != host ||
        _validatedEffectivePort(uri) != port) {
      throw ArgumentError(
        'Authenticated WebSocket URI must use the trusted origin.',
      );
    }
  }
}

void _validateStrictHttpUri(Uri uri) {
  _validateStrictNetworkUri(uri);
}

void _validateStrictNetworkUri(Uri uri) {
  final host = uri.host;
  final hasNonAsciiHost = host.codeUnits.any((unit) => unit > 0x7f);
  if (uri.hasFragment ||
      host.endsWith('.') ||
      hasNonAsciiHost ||
      uri.authority.contains('%')) {
    throw ArgumentError('Authenticated network URI is invalid.');
  }
}

int _validatedEffectivePort(Uri uri) {
  if (!uri.hasPort) {
    return switch (uri.scheme) {
      'https' || 'wss' => 443,
      'http' || 'ws' => 80,
      _ => throw ArgumentError(
        'Authenticated network URI uses an unsupported scheme.',
      ),
    };
  }

  late final int port;
  try {
    port = uri.port;
  } on FormatException {
    throw ArgumentError('Authenticated network URI has an invalid port.');
  }
  if (port < 1 || port > 65535) {
    throw ArgumentError('Authenticated network URI has an invalid port.');
  }
  return port;
}

void _validateWebSocketBaseUri(Uri uri) {
  if (uri.host.isEmpty || uri.userInfo.isNotEmpty) {
    throw ArgumentError('WebSocket base URI is invalid.');
  }
  if (uri.scheme == 'wss') {
    return;
  }
  if (uri.scheme == 'ws' && isLoopbackHost(uri.host)) {
    return;
  }
  throw ArgumentError(
    'WebSocket connections must use WSS outside loopback development.',
  );
}

bool _containsSessionCredential(String value, {String? sessionToken}) {
  var decoded = value;
  final encodedToken = sessionToken == null
      ? null
      : Uri.encodeComponent(sessionToken);
  var changedOnLastPass = false;
  for (var pass = 0; pass < _maxCredentialDecodePasses; pass += 1) {
    if (_hasSessionCredentialMarker(
      decoded,
      sessionToken: sessionToken,
      encodedToken: encodedToken,
    )) {
      return true;
    }
    if (!decoded.contains('%')) {
      return false;
    }
    try {
      final next = Uri.decodeFull(decoded);
      if (next == decoded) {
        return true;
      }
      changedOnLastPass = true;
      decoded = next;
    } on FormatException {
      return true;
    }
  }
  return changedOnLastPass ||
      decoded.contains('%') ||
      _hasSessionCredentialMarker(
        decoded,
        sessionToken: sessionToken,
        encodedToken: encodedToken,
      );
}

bool _hasSessionCredentialMarker(
  String value, {
  required String? sessionToken,
  required String? encodedToken,
}) {
  return (sessionToken != null && value.contains(sessionToken)) ||
      (encodedToken != null && value.contains(encodedToken)) ||
      _sessionCredentialMarker.hasMatch(value);
}

bool isLoopbackHost(String host) {
  final normalized = host.toLowerCase();
  return normalized == 'localhost' ||
      normalized == '127.0.0.1' ||
      normalized == '::1';
}
