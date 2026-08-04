import 'dart:io';

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
