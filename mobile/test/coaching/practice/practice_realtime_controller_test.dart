import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';

import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('realtime transcript becomes the existing confirmation draft', () async {
    final client = _RealtimePracticeClient();
    final recorder = _StreamingPracticeRecorder();
    final controller = PracticeController(
      client: client,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-realtime-001',
    );
    addTearDown(controller.dispose);
    await _restore(controller);

    await controller.startRecording();
    expect(controller.recordingState, PracticeRecordingState.recording);
    expect(recorder.streamingStarts, 1);
    expect(recorder.legacyStarts, 0);

    recorder.add(Uint8List.fromList(<int>[1, 2]));
    await client.firstUpdate.future;
    expect(controller.transcript, 'I led the migration');

    final stopping = controller.stopRecording();
    await client.captureFinished.future;
    expect(controller.recordingState, PracticeRecordingState.transcribing);
    expect(controller.transcript, 'I led the migration');

    client.releaseCandidate.complete();
    await stopping;

    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
    expect(controller.candidateId, 'candidate-realtime');
    expect(controller.transcript, 'I led the migration safely.');
    expect(client.legacyTranscriptions, 0);
    expect(recorder.discarded, 1);
  });

  test('recorded mode bypasses realtime on a dual-capable recorder', () async {
    final client = _RealtimePracticeClient();
    final recorder = _StreamingPracticeRecorder();
    final controller = PracticeController(
      client: client,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-recorded-001',
    );
    addTearDown(controller.dispose);
    await _restore(controller);

    await controller.startRecording(useRealtimeTranscription: false);

    expect(controller.recordingState, PracticeRecordingState.recording);
    expect(recorder.legacyStarts, 1);
    expect(recorder.streamingStarts, 0);

    await controller.stopRecording();

    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
    expect(client.legacyTranscriptions, 1);
    expect(recorder.streamingStarts, 0);
    expect(recorder.discarded, 1);
  });

  test(
    'realtime failure is explicit and never falls back to WAV upload',
    () async {
      final client = _RealtimePracticeClient(failAfterCapture: true);
      final recorder = _StreamingPracticeRecorder();
      final controller = PracticeController(
        client: client,
        recorder: recorder,
        clientIdFactory: (scope) => '$scope-realtime-002',
      );
      addTearDown(controller.dispose);
      await _restore(controller);

      await controller.startRecording();
      recorder.add(Uint8List.fromList(<int>[1, 2]));
      await client.firstUpdate.future;
      await controller.stopRecording();

      expect(controller.recordingState, PracticeRecordingState.idle);
      expect(controller.candidateId, isNull);
      expect(controller.hasPendingPracticeAudio, isFalse);
      expect(controller.errorMessage, contains('请重新录音'));
      expect(client.legacyTranscriptions, 0);
      expect(recorder.discarded, 1);
    },
  );

  test('realtime failure can fall back to the retained WAV', () async {
    final client = _RealtimePracticeClient(failAfterCapture: true);
    final recorder = _StreamingPracticeRecorder();
    final controller = PracticeController(
      client: client,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-realtime-fallback-001',
    );
    addTearDown(controller.dispose);
    await _restore(controller);

    await controller.startRecording(fallbackToRecordedTranscription: true);
    recorder.add(Uint8List.fromList(<int>[1, 2]));
    await client.firstUpdate.future;
    await controller.stopRecording();

    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
    expect(controller.candidateId, 'candidate-legacy');
    expect(controller.transcript, 'Legacy answer');
    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(client.legacyTranscriptions, 1);
    expect(recorder.discarded, 1);
  });

  test('fallback keeps recording after realtime disconnects', () async {
    final client = _RealtimePracticeClient(failDuringCapture: true);
    final recorder = _StreamingPracticeRecorder();
    final controller = PracticeController(
      client: client,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-realtime-fallback-002',
    );
    addTearDown(controller.dispose);
    await _restore(controller);

    await controller.startRecording(fallbackToRecordedTranscription: true);
    recorder.add(Uint8List.fromList(<int>[1, 2]));
    await client.firstUpdate.future;
    await Future<void>.delayed(Duration.zero);

    expect(controller.recordingState, PracticeRecordingState.recording);

    await controller.stopRecording();

    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );
    expect(controller.candidateId, 'candidate-legacy');
    expect(client.legacyTranscriptions, 1);
  });

  test('account cleanup fences a late realtime Candidate', () async {
    final client = _RealtimePracticeClient();
    final recorder = _StreamingPracticeRecorder();
    final controller = PracticeController(
      client: client,
      recorder: recorder,
      clientIdFactory: (scope) => '$scope-realtime-003',
    );
    addTearDown(controller.dispose);
    await _restore(controller);

    await controller.startRecording();
    recorder.add(Uint8List.fromList(<int>[1, 2]));
    await client.firstUpdate.future;
    await controller.clearPrivateState();
    client.releaseCandidate.complete();
    await Future<void>.delayed(Duration.zero);

    expect(controller.practiceSessionId, isNull);
    expect(controller.candidateId, isNull);
    expect(controller.transcript, isNull);
    expect(client.legacyTranscriptions, 0);
  });

  test(
    'recording cancellation cannot apply a late realtime Candidate',
    () async {
      final client = _RealtimePracticeClient();
      final recorder = _StreamingPracticeRecorder();
      final controller = PracticeController(
        client: client,
        recorder: recorder,
        clientIdFactory: (scope) => '$scope-realtime-004',
      );
      addTearDown(controller.dispose);
      await _restore(controller);

      await controller.startRecording();
      recorder.add(Uint8List.fromList(<int>[1, 2]));
      await client.firstUpdate.future;
      await controller.cancelRecording();
      client.releaseCandidate.complete();
      await Future<void>.delayed(Duration.zero);

      expect(controller.recordingState, PracticeRecordingState.idle);
      expect(controller.candidateId, isNull);
      expect(controller.transcript, isNull);
      expect(client.legacyTranscriptions, 0);
    },
  );
}

Future<void> _restore(PracticeController controller) {
  return controller.restoreCreatedPractice(
    sessionId: 'session-realtime',
    scene: testScenes.first,
  );
}

final class _RealtimePracticeClient
    implements PracticeClient, PracticeRealtimeTranscriptionClient {
  _RealtimePracticeClient({
    this.failAfterCapture = false,
    this.failDuringCapture = false,
  });

  final bool failAfterCapture;
  final bool failDuringCapture;
  final Completer<void> firstUpdate = Completer<void>();
  final Completer<void> captureFinished = Completer<void>();
  final Completer<void> releaseCandidate = Completer<void>();
  int legacyTranscriptions = 0;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    return _snapshot();
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    return _snapshot();
  }

  @override
  Stream<PracticeTranscriptionEvent> transcribeRealtime({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
    required Stream<Uint8List> audioChunks,
  }) async* {
    var sentUpdate = false;
    await for (final chunk in audioChunks) {
      expect(chunk, isNotEmpty);
      if (!sentUpdate) {
        sentUpdate = true;
        if (!firstUpdate.isCompleted) {
          firstUpdate.complete();
        }
        yield const PracticeTranscriptUpdated(
          text: 'I led the migration',
          isFinal: false,
        );
        if (failDuringCapture) {
          throw const PracticeClientException(
            kind: PracticeClientFailureKind.network,
            retryable: true,
          );
        }
      }
    }
    if (!captureFinished.isCompleted) {
      captureFinished.complete();
    }
    if (failAfterCapture) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        retryable: true,
      );
    }
    await releaseCandidate.future;
    yield const PracticeTranscriptUpdated(
      text: 'I led the migration safely.',
      isFinal: true,
    );
    yield PracticeCandidateCompleted(
      TranscriptionCandidate(
        id: 'candidate-realtime',
        sessionId: sessionId,
        questionId: questionId,
        text: 'I led the migration safely.',
      ),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    legacyTranscriptions++;
    return TranscriptionCandidate(
      id: 'candidate-legacy',
      sessionId: request.sessionId,
      questionId: request.questionId,
      text: 'Legacy answer',
    );
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    throw UnimplementedError();
  }
}

final class _StreamingPracticeRecorder
    implements PracticeRecorder, PracticeStreamingRecorder {
  final StreamController<Uint8List> _chunks = StreamController<Uint8List>();
  int legacyStarts = 0;
  int streamingStarts = 0;
  int discarded = 0;
  bool _closed = false;

  void add(Uint8List chunk) => _chunks.add(chunk);

  @override
  Future<void> start() async {
    legacyStarts++;
  }

  @override
  Future<Stream<Uint8List>> startAudioStream() async {
    streamingStarts++;
    return _chunks.stream;
  }

  @override
  Future<RecordedPracticeAudio> stop() async => _audio;

  @override
  Future<RecordedPracticeAudio> stopAudioStream() async {
    await _close();
    return _audio;
  }

  @override
  Future<void> discardCurrent() => _close();

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {
    discarded++;
  }

  @override
  Future<void> clearAccountState() => _close();

  Future<void> _close() async {
    if (_closed) {
      return;
    }
    _closed = true;
    await _chunks.close();
  }
}

const _audio = RecordedPracticeAudio(
  path: 'realtime-practice.wav',
  contentType: 'audio/wav',
  sizeBytes: 48,
);

PracticeSessionSnapshot _snapshot() {
  return PracticeSessionSnapshot(
    sessionId: 'session-realtime',
    planId: 'plan-realtime',
    practiceExperience: testScenes.first.experience,
    sceneCategory: testScenes.first.category,
    practiceMode: testScenes.first.practiceOptions.first.mode,
    capabilities: testPracticeCapabilities,
    sessionVersion: 1,
    completedTurns: 0,
    turnLimit: 2,
    sessionCompleted: false,
    currentQuestion: const PracticeQuestion(
      id: 'question-realtime',
      sessionId: 'session-realtime',
      text: 'Tell me about a project.',
      speechPath: '/v1/questions/question-realtime/speech',
    ),
  );
}
