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
}

final class _StreamingNativeRecorder implements NativeAgentVoiceRecorder {
  final StreamController<Uint8List> _chunks = StreamController<Uint8List>();

  void add(Uint8List chunk) => _chunks.add(chunk);

  @override
  Future<bool> hasPermission() async => true;

  @override
  Future<void> startWav(String path) => throw UnimplementedError();

  @override
  Future<Stream<Uint8List>> startPcm16Stream() async => _chunks.stream;

  @override
  Future<String?> stop() async {
    await _chunks.close();
    return null;
  }
}
