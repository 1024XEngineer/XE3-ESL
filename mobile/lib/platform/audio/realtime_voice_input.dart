import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/network/authenticated_web_socket.dart';

const realtimeVoiceInputProtocol = 'speakup.voice-input.v1';

enum RealtimeVoiceInputFailureKind {
  authenticationRequired,
  network,
  invalidResponse,
}

final class RealtimeVoiceInputException implements Exception {
  const RealtimeVoiceInputException(this.kind);

  final RealtimeVoiceInputFailureKind kind;
}

final class RealtimeVoiceInputEnvelope {
  const RealtimeVoiceInputEnvelope({required this.type, required this.data});

  final String type;
  final Map<String, Object?> data;
}

final class RealtimeVoiceInputTransport {
  RealtimeVoiceInputTransport({
    required this.connector,
    this.connectionTimeout = const Duration(seconds: 15),
    this.pingInterval = const Duration(seconds: 10),
  }) {
    if (connectionTimeout <= Duration.zero || pingInterval <= Duration.zero) {
      throw ArgumentError('Realtime voice input timing is invalid.');
    }
  }

  final SessionAuthenticatedWebSocketConnector connector;
  final Duration connectionTimeout;
  final Duration pingInterval;

  Stream<RealtimeVoiceInputEnvelope> stream({
    required Uri uri,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
    required void Function() ensureCurrent,
    int maximumChunkBytes = 7_400_000,
    Set<String> terminalEventTypes = const <String>{
      'candidate.ready',
      'candidate.failed',
    },
  }) async* {
    if (idempotencyKey.length < 8 ||
        idempotencyKey.length > 128 ||
        maximumChunkBytes < 1 ||
        terminalEventTypes.isEmpty ||
        terminalEventTypes.any((type) => type.isEmpty || type.length > 64)) {
      throw ArgumentError('Realtime voice input configuration is invalid.');
    }
    SessionAuthenticatedWebSocketConnection? connection;
    StreamIterator<Uint8List>? chunks;
    Future<void>? sender;
    final senderControl = _RealtimeVoiceInputSenderControl();
    var receivedTerminalEvent = false;
    try {
      connection = await _connect(uri);
      ensureCurrent();
      connection.socket.pingInterval = pingInterval;
      connection.socket.add(
        jsonEncode(<String, Object>{
          'type': 'start',
          'idempotency_key': idempotencyKey,
          'sample_rate': 16000,
        }),
      );
      chunks = StreamIterator<Uint8List>(audioChunks);
      sender = _sendAudio(
        connection: connection,
        chunks: chunks,
        ensureCurrent: ensureCurrent,
        maximumChunkBytes: maximumChunkBytes,
        control: senderControl,
        onAudioFinished: () {
          // The server stops reading after `finish` while it waits for the
          // provider terminal result. Bound that phase at the controller
          // instead of treating a missing control-frame pong as a disconnect.
          try {
            connection?.socket.pingInterval = null;
          } catch (_) {
            // The socket may already have closed because the sender failed.
          }
        },
      );
      await for (final message in connection.socket) {
        ensureCurrent();
        final envelope = _decodeEnvelope(message);
        yield envelope;
        if (terminalEventTypes.contains(envelope.type)) {
          receivedTerminalEvent = true;
          break;
        }
      }
      if (receivedTerminalEvent) {
        senderControl.stop();
      }
      await chunks.cancel();
      await sender;
      if (!receivedTerminalEvent) {
        await connection.handleDisconnect(
          closeCode: connection.socket.closeCode,
          closeReason: connection.socket.closeReason,
        );
        throw const RealtimeVoiceInputException(
          RealtimeVoiceInputFailureKind.network,
        );
      }
      return;
    } on AuthenticatedWebSocketException catch (error) {
      throw RealtimeVoiceInputException(
        error.invalidatesAuthentication
            ? RealtimeVoiceInputFailureKind.authenticationRequired
            : RealtimeVoiceInputFailureKind.network,
      );
    } on FormatException {
      throw const RealtimeVoiceInputException(
        RealtimeVoiceInputFailureKind.invalidResponse,
      );
    } finally {
      if (connection != null) {
        if (receivedTerminalEvent) {
          senderControl.stop();
        } else {
          senderControl.cancel(connection);
        }
      }
      await chunks?.cancel();
      if (sender != null) {
        try {
          await sender;
        } catch (_) {
          // The active consumer already receives this failure above. A
          // cancelled consumer only needs its socket and microphone released.
        }
      }
      if (connection != null) {
        await _closeBestEffort(connection.socket);
      }
    }
  }

  Future<SessionAuthenticatedWebSocketConnection> _connect(Uri uri) async {
    final pending = connector.connect(uri: uri);
    try {
      return await pending.timeout(connectionTimeout);
    } on TimeoutException {
      unawaited(_closeLateConnection(pending));
      throw const RealtimeVoiceInputException(
        RealtimeVoiceInputFailureKind.network,
      );
    }
  }

  Future<void> _closeLateConnection(
    Future<SessionAuthenticatedWebSocketConnection> pending,
  ) async {
    try {
      final connection = await pending;
      await _closeBestEffort(connection.socket);
    } catch (_) {
      // The timed-out dial either failed independently or was closed here.
    }
  }

  Future<void> _closeBestEffort(WebSocket socket) async {
    socket.pingInterval = null;
    try {
      await socket.close().timeout(connectionTimeout);
    } catch (_) {
      // Closing is terminal cleanup. The active stream already exposes the
      // protocol, network, or cancellation result that selected this path.
    }
  }
}

Future<void> _sendAudio({
  required SessionAuthenticatedWebSocketConnection connection,
  required StreamIterator<Uint8List> chunks,
  required void Function() ensureCurrent,
  required int maximumChunkBytes,
  required _RealtimeVoiceInputSenderControl control,
  required void Function() onAudioFinished,
}) async {
  try {
    while (await chunks.moveNext()) {
      if (control.stopped) {
        return;
      }
      ensureCurrent();
      final chunk = chunks.current;
      if (chunk.isEmpty || chunk.lengthInBytes > maximumChunkBytes) {
        throw ArgumentError('Realtime voice input chunk is invalid.');
      }
      connection.socket.add(chunk);
    }
    if (control.stopped) {
      return;
    }
    ensureCurrent();
    if (control.stopped) {
      return;
    }
    connection.socket.add(jsonEncode(const <String, String>{'type': 'finish'}));
  } catch (error, stackTrace) {
    try {
      control.cancel(connection);
      await connection.socket.close();
    } catch (_) {
      // Preserve the capture or account-fence failure that ended the stream.
    }
    Error.throwWithStackTrace(error, stackTrace);
  } finally {
    onAudioFinished();
  }
}

final class _RealtimeVoiceInputSenderControl {
  bool _stopped = false;
  bool _cancelSent = false;

  bool get stopped => _stopped;

  void stop() {
    _stopped = true;
  }

  void cancel(SessionAuthenticatedWebSocketConnection connection) {
    _stopped = true;
    if (_cancelSent) {
      return;
    }
    _cancelSent = true;
    try {
      connection.socket.add(
        jsonEncode(const <String, String>{'type': 'cancel'}),
      );
    } catch (_) {
      // Closing the socket below still cancels the server-side workflow.
    }
  }
}

Uri realtimeVoiceWebSocketBaseUri(Uri baseUri) {
  return baseUri.replace(
    scheme: switch (baseUri.scheme) {
      'https' => 'wss',
      'http' => 'ws',
      final scheme => throw ArgumentError.value(
        scheme,
        'baseUri',
        'Realtime voice input requires HTTP or HTTPS.',
      ),
    },
  );
}

RealtimeVoiceInputEnvelope _decodeEnvelope(Object? message) {
  if (message is! String) {
    throw const FormatException('Realtime voice event must be JSON text.');
  }
  final decoded = jsonDecode(message);
  if (decoded is! Map<String, dynamic> ||
      decoded.length != 2 ||
      !decoded.containsKey('type') ||
      !decoded.containsKey('data')) {
    throw const FormatException('Realtime voice event envelope is invalid.');
  }
  final type = decoded['type'];
  final data = decoded['data'];
  if (type is! String ||
      type.isEmpty ||
      type.length > 64 ||
      data is! Map<String, dynamic>) {
    throw const FormatException('Realtime voice event payload is invalid.');
  }
  return RealtimeVoiceInputEnvelope(
    type: type,
    data: Map<String, Object?>.unmodifiable(data),
  );
}
