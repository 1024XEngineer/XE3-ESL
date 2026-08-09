import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/ios_practice_recorder.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';

void main() {
  late Directory root;

  setUp(() async {
    root = await Directory.systemTemp.createTemp('speakup-recorder-test-');
  });

  tearDown(() async {
    if (await root.exists()) {
      await root.delete(recursive: true);
    }
  });

  test('stop failure immediately deletes the controlled active path', () async {
    final native = _NativeRecorder()..stopError = StateError('stop failed');
    final recorder = IosPracticeRecorder(
      recorder: native,
      temporaryDirectory: () async => root,
    );

    await recorder.start();
    final activePath = native.startedPath!;
    expect(await File(activePath).exists(), isTrue);

    await expectLater(
      recorder.stop(),
      throwsA(
        isA<PracticeRecordingException>().having(
          (error) => error.kind,
          'kind',
          PracticeRecordingFailureKind.unavailable,
        ),
      ),
    );

    expect(await File(activePath).exists(), isFalse);
  });

  test(
    'a changed managed stop path removes the unused expected path',
    () async {
      final native = _NativeRecorder();
      final recorder = IosPracticeRecorder(
        recorder: native,
        temporaryDirectory: () async => root,
      );

      await recorder.start();
      final expectedPath = native.startedPath!;
      final alternatePath =
          '${File(expectedPath).parent.path}/native-alternate.wav';
      await File(alternatePath).writeAsBytes(List<int>.filled(100, 1));
      native.stopResult = alternatePath;

      final audio = await recorder.stop();

      expect(audio.path, alternatePath);
      expect(await File(expectedPath).exists(), isFalse);
      await recorder.discard(audio);
      expect(await File(alternatePath).exists(), isFalse);
    },
  );

  test(
    'discard rejects arbitrary absolute paths without deleting them',
    () async {
      final native = _NativeRecorder();
      final recorder = IosPracticeRecorder(
        recorder: native,
        temporaryDirectory: () async => root,
      );
      final unrelated = File('${root.path}/unrelated.wav');
      await unrelated.writeAsBytes(List<int>.filled(100, 1));

      await expectLater(
        recorder.discard(
          RecordedPracticeAudio(
            path: unrelated.path,
            contentType: 'audio/wav',
            sizeBytes: 100,
          ),
        ),
        throwsA(
          isA<PracticeRecordingException>().having(
            (error) => error.kind,
            'kind',
            PracticeRecordingFailureKind.invalidAudio,
          ),
        ),
      );

      expect(await unrelated.exists(), isTrue);
    },
  );

  test('streaming recorder forwards PCM and retains a local WAV', () async {
    final native = _StreamingNativeRecorder();
    final recorder = IosPracticeRecorder(
      recorder: native,
      temporaryDirectory: () async => root,
    );

    final stream = await recorder.startAudioStream();
    final forwarded = stream.expand((chunk) => chunk).toList();
    native.add(Uint8List.fromList(<int>[0, 0, 1, 0]));
    final audio = await recorder.stopAudioStream();

    expect(await forwarded, <int>[0, 0, 1, 0]);
    expect(audio.contentType, 'audio/wav');
    expect(audio.sizeBytes, 48);
    final bytes = await File(audio.path).readAsBytes();
    expect(String.fromCharCodes(bytes.sublist(0, 4)), 'RIFF');
    expect(String.fromCharCodes(bytes.sublist(8, 12)), 'WAVE');
    expect(bytes.sublist(44), <int>[0, 0, 1, 0]);

    await recorder.discard(audio);
    expect(await File(audio.path).exists(), isFalse);
  });
}

final class _NativeRecorder implements NativePracticeRecorder {
  String? startedPath;
  String? stopResult;
  Object? stopError;

  @override
  Future<bool> hasPermission() async => true;

  @override
  Future<void> startWav(String path) async {
    startedPath = path;
    await File(path).writeAsBytes(List<int>.filled(100, 1));
  }

  @override
  Future<String?> stop() async {
    if (stopError case final error?) {
      throw error;
    }
    return stopResult ?? startedPath;
  }
}

final class _StreamingNativeRecorder
    implements NativePracticeRecorder, NativeStreamingPracticeRecorder {
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
