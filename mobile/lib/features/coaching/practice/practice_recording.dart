final class RecordedPracticeAudio {
  const RecordedPracticeAudio({
    required this.path,
    required this.contentType,
    required this.sizeBytes,
  });

  final String path;
  final String contentType;
  final int sizeBytes;
}

abstract interface class PracticeRecorder {
  Future<void> start();

  Future<RecordedPracticeAudio> stop();

  Future<void> discardCurrent();

  Future<void> discard(RecordedPracticeAudio audio);

  Future<void> clearAccountState();
}

final class PracticeRecordingException implements Exception {
  const PracticeRecordingException(this.kind);

  final PracticeRecordingFailureKind kind;
}

enum PracticeRecordingFailureKind {
  permissionDenied,
  alreadyRecording,
  notRecording,
  emptyAudio,
  invalidAudio,
  unavailable,
}

/// Deterministic recorder used only by explicit Fake previews and tests.
final class FakePracticeRecorder implements PracticeRecorder {
  bool _recording = false;
  int _sequence = 0;

  @override
  Future<void> start() async {
    if (_recording) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.alreadyRecording,
      );
    }
    _recording = true;
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    if (!_recording) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.notRecording,
      );
    }
    _recording = false;
    return RecordedPracticeAudio(
      path: 'fake-recording-${++_sequence}.wav',
      contentType: 'audio/wav',
      sizeBytes: 44,
    );
  }

  @override
  Future<void> discardCurrent() async {
    _recording = false;
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> clearAccountState() => discardCurrent();
}
