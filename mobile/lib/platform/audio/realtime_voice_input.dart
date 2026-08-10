import 'dart:async';
import 'dart:convert';
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
  const RealtimeVoiceInputTransport({required this.connector});

  final SessionAuthenticatedWebSocketConnector connector;

  Stream<RealtimeVoiceInputEnvelope> stream({
    required Uri uri,
    required Stream<Uint8List> audioChunks,
    required String idempotencyKey,
    required void Function() ensureCurrent,
    int maximumChunkBytes = 7_400_000,
  }) async* {
    if (idempotencyKey.length < 8 ||
        idempotencyKey.length > 128 ||
        maximumChunkBytes < 1) {
      throw ArgumentError('Realtime voice input configuration is invalid.');
    }
    SessionAuthenticatedWebSocketConnection? connection;
    StreamIterator<Uint8List>? chunks;
    Future<void>? sender;
    try {
      connection = await connector.connect(uri: uri);
      ensureCurrent();
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
      );
      var receivedTerminalEvent = false;
      await for (final message in connection.socket) {
        ensureCurrent();
        final envelope = _decodeEnvelope(message);
        yield envelope;
        if (envelope.type == 'candidate.ready' ||
            envelope.type == 'candidate.failed') {
          receivedTerminalEvent = true;
          break;
        }
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
      await chunks?.cancel();
      if (sender != null) {
        try {
          await sender;
        } catch (_) {
          // The active consumer already receives this failure above. A
          // cancelled consumer only needs its socket and microphone released.
        }
      }
      await connection?.socket.close();
    }
  }
}

Future<void> _sendAudio({
  required SessionAuthenticatedWebSocketConnection connection,
  required StreamIterator<Uint8List> chunks,
  required void Function() ensureCurrent,
  required int maximumChunkBytes,
}) async {
  try {
    while (await chunks.moveNext()) {
      ensureCurrent();
      final chunk = chunks.current;
      if (chunk.isEmpty || chunk.lengthInBytes > maximumChunkBytes) {
        throw ArgumentError('Realtime voice input chunk is invalid.');
      }
      connection.socket.add(chunk);
    }
    ensureCurrent();
    connection.socket.add(jsonEncode(const <String, String>{'type': 'finish'}));
  } catch (error, stackTrace) {
    try {
      connection.socket.add(
        jsonEncode(const <String, String>{'type': 'cancel'}),
      );
      await connection.socket.close();
    } catch (_) {
      // Preserve the capture or account-fence failure that ended the stream.
    }
    Error.throwWithStackTrace(error, stackTrace);
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
