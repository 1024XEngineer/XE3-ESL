import 'dart:io';

typedef PlatformWebSocketDialer =
    Future<WebSocket> Function(
      String url, {
      Iterable<String>? protocols,
      Map<String, dynamic>? headers,
    });

final class PlatformWebSocketException implements Exception {
  const PlatformWebSocketException({this.httpStatusCode});

  final int? httpStatusCode;
}

abstract interface class PlatformWebSocketTransport {
  Future<WebSocket> connect({
    required Uri uri,
    required Iterable<String> protocols,
    required Map<String, dynamic> headers,
  });
}

/// Protocol-neutral WebSocket transport shared by authenticated product flows.
final class IoPlatformWebSocketTransport implements PlatformWebSocketTransport {
  IoPlatformWebSocketTransport({PlatformWebSocketDialer? dialer})
    : _dialer = dialer ?? WebSocket.connect;

  final PlatformWebSocketDialer _dialer;

  @override
  Future<WebSocket> connect({
    required Uri uri,
    required Iterable<String> protocols,
    required Map<String, dynamic> headers,
  }) async {
    if (uri.scheme != 'ws' && uri.scheme != 'wss') {
      throw ArgumentError('WebSocket URI must use ws or wss.');
    }
    if (!uri.hasAuthority || uri.host.isEmpty) {
      throw ArgumentError('WebSocket URI must include a host.');
    }
    if (uri.hasFragment) {
      throw ArgumentError('WebSocket URI must not include a fragment.');
    }
    if (uri.userInfo.isNotEmpty) {
      throw ArgumentError('WebSocket URI must not contain credentials.');
    }
    try {
      return await _dialer(
        uri.toString(),
        protocols: protocols,
        headers: headers,
      );
    } on WebSocketException catch (error) {
      throw PlatformWebSocketException(httpStatusCode: error.httpStatusCode);
    } on SocketException {
      throw const PlatformWebSocketException();
    } on HttpException {
      throw const PlatformWebSocketException();
    } catch (_) {
      throw const PlatformWebSocketException();
    }
  }
}
