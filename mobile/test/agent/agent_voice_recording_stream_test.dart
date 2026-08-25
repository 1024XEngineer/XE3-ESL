import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/composer/voice/agent_voice_recording.dart';

void main() {
  test(
    'streaming recorder forwards PCM and retains one valid local WAV',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'agent-voice-stream-recorder-',
      );
      addTearDown(() => directory.delete(recursive: true));
      final native = _StreamingNativeRecorder();
      final recorder = IosAgentVoiceRecorder(
        recorder: native,
        temporaryDirectory: () async => directory,
        clock: () => DateTime.utc(2026, 8, 6, 1, 0, 1),
      );

      final stream = await recorder.startAudioStream();
      final forwarded = stream.expand((chunk) => chunk).toList();
      native.add(Uint8List.fromList(<int>[0, 0, 1, 0]));
      final recording = await recorder.stopAudioStream();

      expect(await forwarded, <int>[0, 0, 1, 0]);
      expect(recording.contentType, 'audio/wav');
      expect(recording.sizeBytes, 48);
      final bytes = await File(recording.path).readAsBytes();
      expect(String.fromCharCodes(bytes.sublist(0, 4)), 'RIFF');
      expect(String.fromCharCodes(bytes.sublist(8, 12)), 'WAVE');
      expect(bytes.sublist(44), <int>[0, 0, 1, 0]);

      await recorder.discard(recording);
      expect(await File(recording.path).exists(), isFalse);
    },
  );

  test('ephemeral streaming stop clears PCM without creating a WAV', () async {
    final directory = await Directory.systemTemp.createTemp(
      'agent-voice-ephemeral-recorder-',
    );
    addTearDown(() => directory.delete(recursive: true));
    final native = _StreamingNativeRecorder();
    final recorder = IosAgentVoiceRecorder(
      recorder: native,
      temporaryDirectory: () async => directory,
    );

    final stream = await recorder.startAudioStream();
    final forwarded = stream.expand((chunk) => chunk).toList();
    final pcm = Uint8List.fromList(<int>[0, 0, 1, 0]);
    native.add(pcm);
    await recorder.stopAudioStreamAndDiscard();

    expect(await forwarded, <int>[0, 0, 1, 0]);
    expect(pcm, <int>[0, 0, 0, 0]);
    expect(
      await directory
          .list(recursive: true, followLinks: false)
          .where((entity) => entity is File)
          .toList(),
      isEmpty,
    );
  });

  test('streaming stop failure cancels the native capture', () async {
    final directory = await Directory.systemTemp.createTemp(
      'agent-voice-stream-recorder-failure-',
    );
    addTearDown(() => directory.delete(recursive: true));
    final native = _StreamingNativeRecorder()
      ..stopError = StateError('stop failed');
    final recorder = IosAgentVoiceRecorder(
      recorder: native,
      temporaryDirectory: () async => directory,
    );

    final stream = await recorder.startAudioStream();
    final subscription = stream.listen((_) {}, onError: (_) {});
    final pcm = Uint8List.fromList(<int>[1, 0, 2, 0]);
    native.add(pcm);

    await expectLater(
      recorder.stopAudioStream(),
      throwsA(
        isA<AgentVoiceRecordingException>().having(
          (error) => error.kind,
          'kind',
          AgentVoiceRecordingFailureKind.unavailable,
        ),
      ),
    );

    expect(native.cancelCount, 1);
    expect(pcm, <int>[0, 0, 0, 0]);
    await subscription.cancel();
  });

  test(
    'account clear cancels a native stream that finishes starting late',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'agent-voice-late-stream-recorder-',
      );
      addTearDown(() => directory.delete(recursive: true));
      final native = _DelayedStartNativeRecorder();
      addTearDown(native.dispose);
      final recorder = IosAgentVoiceRecorder(
        recorder: native,
        temporaryDirectory: () async => directory,
      );

      final starting = recorder.startAudioStream();
      await native.firstStartRequested.future.timeout(
        const Duration(seconds: 1),
      );

      await recorder.clearAccountState().timeout(const Duration(seconds: 1));
      await expectLater(
        recorder.startAudioStream(),
        throwsA(
          isA<AgentVoiceRecordingException>().having(
            (error) => error.kind,
            'kind',
            AgentVoiceRecordingFailureKind.alreadyRecording,
          ),
        ),
      );

      final lateStartFailure = expectLater(
        starting,
        throwsA(
          isA<AgentVoiceRecordingException>().having(
            (error) => error.kind,
            'kind',
            AgentVoiceRecordingFailureKind.unavailable,
          ),
        ),
      );
      native.releaseFirstStart();
      await lateStartFailure;

      expect(native.cancelCount, 1);
      expect(native.stopCount, 2);
      expect(
        await directory
            .list(recursive: true, followLinks: false)
            .where((entity) => entity is File)
            .toList(),
        isEmpty,
      );

      final next = await recorder.startAudioStream();
      final nextSubscription = next.listen((_) {});
      await recorder.discardCurrent();
      await nextSubscription.cancel();
      expect(native.startCount, 2);
    },
  );

  test(
    'account clear is bounded while native stop remains in flight',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'agent-voice-bounded-recorder-clear-',
      );
      addTearDown(() => directory.delete(recursive: true));
      final native = _DelayedStartNativeRecorder(gateFirstStop: true);
      addTearDown(() async {
        native.releaseFirstStart();
        native.releaseFirstStop();
        await native.dispose();
      });
      final recorder = IosAgentVoiceRecorder(
        recorder: native,
        temporaryDirectory: () async => directory,
      );

      final starting = recorder.startAudioStream();
      await native.firstStartRequested.future.timeout(
        const Duration(seconds: 1),
      );
      native.releaseFirstStart();
      final stream = await starting;
      final subscription = stream.listen((_) {});

      final clear = recorder.clearAccountState();
      await native.firstStopRequested.future.timeout(
        const Duration(seconds: 1),
      );
      await clear.timeout(const Duration(seconds: 3));
      await expectLater(
        recorder.startAudioStream(),
        throwsA(
          isA<AgentVoiceRecordingException>().having(
            (error) => error.kind,
            'kind',
            AgentVoiceRecordingFailureKind.alreadyRecording,
          ),
        ),
      );

      native.releaseFirstStop();
      await native.firstStopCompleted.future.timeout(
        const Duration(seconds: 1),
      );
      await Future<void>.delayed(Duration.zero);
      final next = await recorder.startAudioStream();
      final nextSubscription = next.listen((_) {});
      await recorder.discardCurrent();
      await Future.wait<void>([
        subscription.cancel(),
        nextSubscription.cancel(),
      ]);
    },
  );
}

final class _StreamingNativeRecorder implements NativeAgentVoiceRecorder {
  _StreamingNativeRecorder() {
    _chunks = StreamController<Uint8List>(onCancel: () => cancelCount++);
  }

  late final StreamController<Uint8List> _chunks;
  Object? stopError;
  int cancelCount = 0;

  void add(Uint8List chunk) => _chunks.add(chunk);

  @override
  Future<bool> hasPermission() async => true;

  @override
  Future<void> startWav(String path) => throw UnimplementedError();

  @override
  Future<Stream<Uint8List>> startPcm16Stream() async => _chunks.stream;

  @override
  Future<String?> stop() async {
    if (stopError case final error?) {
      throw error;
    }
    await _chunks.close();
    return null;
  }
}

final class _DelayedStartNativeRecorder implements NativeAgentVoiceRecorder {
  _DelayedStartNativeRecorder({this.gateFirstStop = false});

  final bool gateFirstStop;
  final Completer<void> firstStartRequested = Completer<void>();
  final Completer<void> _firstStartGate = Completer<void>();
  final Completer<void> firstStopRequested = Completer<void>();
  final Completer<void> firstStopCompleted = Completer<void>();
  final Completer<void> _firstStopGate = Completer<void>();
  final List<StreamController<Uint8List>> _streams =
      <StreamController<Uint8List>>[];
  int startCount = 0;
  int stopCount = 0;
  int cancelCount = 0;

  void releaseFirstStart() {
    if (!_firstStartGate.isCompleted) {
      _firstStartGate.complete();
    }
  }

  void releaseFirstStop() {
    if (!_firstStopGate.isCompleted) {
      _firstStopGate.complete();
    }
  }

  Future<void> dispose() async {
    for (final stream in _streams) {
      await stream.close();
    }
  }

  @override
  Future<bool> hasPermission() async => true;

  @override
  Future<void> startWav(String path) => throw UnimplementedError();

  @override
  Future<Stream<Uint8List>> startPcm16Stream() async {
    startCount++;
    final stream = StreamController<Uint8List>(onCancel: () => cancelCount++);
    _streams.add(stream);
    if (startCount == 1) {
      firstStartRequested.complete();
      await _firstStartGate.future;
    }
    return stream.stream;
  }

  @override
  Future<String?> stop() async {
    stopCount++;
    if (stopCount == 1) {
      firstStopRequested.complete();
      if (gateFirstStop) {
        await _firstStopGate.future;
      }
      firstStopCompleted.complete();
    }
    return null;
  }
}
