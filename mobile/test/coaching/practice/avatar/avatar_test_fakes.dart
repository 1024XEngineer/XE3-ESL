import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/widgets.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';

final class FakeAvatarRenderer implements AvatarRenderer {
  FakeAvatarRenderer({
    this.connectOnPrepare = true,
    this.prepareFailure,
    this.prepareGate,
    this.surfaceBuilder,
  });

  final bool connectOnPrepare;
  final AvatarRendererFailure? prepareFailure;
  final Completer<void>? prepareGate;
  final Widget Function(Key? key)? surfaceBuilder;
  final StreamController<AvatarRendererState> _states =
      StreamController<AvatarRendererState>.broadcast(sync: true);

  AvatarRendererState _state = const AvatarRendererState();
  AvatarSessionGrant? preparedGrant;
  final List<({Uint8List bytes, bool end})> sends = [];
  final List<Completer<void>> pendingSends = [];
  final List<String> actions = [];
  int interruptCount = 0;
  int pauseCount = 0;
  int resumeCount = 0;
  int closeCount = 0;
  int? failSendAt;
  bool holdSends = false;
  Completer<void>? interruptGate;
  Object? interruptError;
  Object? closeError;

  @override
  AvatarRendererState get state => _state;

  @override
  Stream<AvatarRendererState> get states => _states.stream;

  @override
  Future<void> prepare(AvatarSessionGrant grant) async {
    preparedGrant = grant;
    final gate = prepareGate;
    if (gate != null) {
      await gate.future;
    }
    final failure = prepareFailure;
    if (failure != null) {
      emit(
        AvatarRendererState(
          connection: AvatarRendererConnection.failed,
          failure: failure,
        ),
      );
      throw AvatarRendererException(failure);
    }
    emit(
      AvatarRendererState(
        connection: connectOnPrepare
            ? AvatarRendererConnection.connected
            : AvatarRendererConnection.surfaceReady,
      ),
    );
  }

  @override
  Widget buildSurface({Key? key}) =>
      surfaceBuilder?.call(key) ?? SizedBox(key: key);

  @override
  Future<void> sendPcm(Uint8List pcmBytes, {required bool end}) async {
    actions.add('send');
    sends.add((bytes: Uint8List.fromList(pcmBytes), end: end));
    if (failSendAt == sends.length - 1) {
      throw const AvatarRendererException(AvatarRendererFailure.network);
    }
    if (holdSends) {
      final completer = Completer<void>();
      pendingSends.add(completer);
      await completer.future;
    }
  }

  @override
  Future<void> interrupt() async {
    interruptCount++;
    actions.add('interrupt');
    final gate = interruptGate;
    if (gate != null) {
      await gate.future;
    }
    final error = interruptError;
    if (error != null) {
      throw error;
    }
  }

  @override
  Future<void> pauseRendering() async {
    pauseCount++;
  }

  @override
  Future<void> resumeRendering() async {
    resumeCount++;
  }

  @override
  Future<void> close() async {
    closeCount++;
    actions.add('close');
    final error = closeError;
    if (error != null) {
      throw error;
    }
    emit(
      const AvatarRendererState(connection: AvatarRendererConnection.closed),
    );
    await _states.close();
  }

  void emit(AvatarRendererState state) {
    _state = state;
    if (!_states.isClosed) {
      _states.add(state);
    }
  }
}

final class FakeAvatarSessionTokenClient implements AvatarSessionTokenClient {
  FakeAvatarSessionTokenClient({AvatarSessionGrant? grant, this.error})
    : grant = grant ?? testAvatarGrant;

  final AvatarSessionGrant grant;
  final Object? error;
  final List<String> requestedSessionIds = [];
  int clearCount = 0;
  int disposeCount = 0;

  @override
  Future<AvatarSessionGrant> createSession({
    required String practiceSessionId,
  }) async {
    requestedSessionIds.add(practiceSessionId);
    if (error case final error?) {
      throw error;
    }
    return grant;
  }

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }

  @override
  Future<void> dispose() async {
    disposeCount++;
  }
}

final testAvatarGrant = AvatarSessionGrant(
  appId: 'app-1',
  avatarId: 'avatar-1',
  sessionToken: 'private-avatar-token',
  region: AvatarRegion.apNortheast,
  expiresAt: DateTime.utc(2030),
  audioFormat: AvatarAudioFormat.pcmS16le24kMono,
);

Uint8List buildPcmWave({
  int channels = 1,
  int sampleRate = 24000,
  int bitsPerSample = 16,
  int? byteRate,
  int? blockAlign,
  int audioEncoding = 1,
  Uint8List? pcm,
  List<({String id, Uint8List bytes})> beforeFormat = const [],
  List<({String id, Uint8List bytes})> betweenFormatAndData = const [],
  List<({String id, Uint8List bytes})> afterData = const [],
}) {
  final resolvedPcm = pcm ?? Uint8List(96000);
  final resolvedBlockAlign = blockAlign ?? channels * (bitsPerSample ~/ 8);
  final resolvedByteRate = byteRate ?? sampleRate * resolvedBlockAlign;
  final fmt = ByteData(16)
    ..setUint16(0, audioEncoding, Endian.little)
    ..setUint16(2, channels, Endian.little)
    ..setUint32(4, sampleRate, Endian.little)
    ..setUint32(8, resolvedByteRate, Endian.little)
    ..setUint16(12, resolvedBlockAlign, Endian.little)
    ..setUint16(14, bitsPerSample, Endian.little);
  final chunks = <({String id, Uint8List bytes})>[
    ...beforeFormat,
    (id: 'fmt ', bytes: fmt.buffer.asUint8List()),
    ...betweenFormatAndData,
    (id: 'data', bytes: resolvedPcm),
    ...afterData,
  ];
  final payloadLength = chunks.fold<int>(
    4,
    (total, chunk) => total + 8 + chunk.bytes.length + chunk.bytes.length % 2,
  );
  final result = Uint8List(8 + payloadLength);
  final view = ByteData.sublistView(result);
  _writeAscii(result, 0, 'RIFF');
  view.setUint32(4, payloadLength, Endian.little);
  _writeAscii(result, 8, 'WAVE');
  var offset = 12;
  for (final chunk in chunks) {
    _writeAscii(result, offset, chunk.id);
    view.setUint32(offset + 4, chunk.bytes.length, Endian.little);
    result.setRange(offset + 8, offset + 8 + chunk.bytes.length, chunk.bytes);
    offset += 8 + chunk.bytes.length + chunk.bytes.length % 2;
  }
  return result;
}

void _writeAscii(Uint8List target, int offset, String value) {
  target.setRange(offset, offset + value.length, value.codeUnits);
}
