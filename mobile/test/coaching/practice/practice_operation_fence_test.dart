import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';

import '../../support/scene_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('account switch fences an old Practice restore', () async {
    final account = _AccountMarker();
    final practice = _AccountPracticeClient(account, blockFirstRestore: true);
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);

    final oldRestore = controller.restoreCreatedPractice(
      sessionId: 'session-A',
      scene: testScenes.first,
    );
    final staleResult = expectLater(
      oldRestore,
      throwsA(isA<PracticeClientOperationCancelled>()),
    );
    await practice.firstRestoreStarted.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await _restorePractice(controller, 'B');

    practice.firstRestoreResult.complete(_practiceSnapshot('A'));
    await staleResult;

    expect(practice.restoreCalls, ['A:session-A', 'B:session-B']);
    expect(controller.practiceSessionId, 'session-B');
  });

  test('account switch fences old recording playback before fetch', () async {
    final account = _AccountMarker();
    final media = _AccountMediaClient(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      practice: _AccountPracticeClient(account),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await _restorePractice(controller, 'A');

    final stop = player.blockNextStop();
    final playback = controller.toggleRecordingAudio('audio-A');
    await stop.entered.future;

    final cleanup = controller.clearPrivateState();
    account.value = 'B';
    stop.release.complete();
    await playback;
    await cleanup;
    await _restorePractice(controller, 'B');

    expect(media.recordingLoads, isEmpty);
    expect(controller.recordings.single.audioAssetId, 'audio-B');
  });

  test('account switch fences old recording before delete', () async {
    final account = _AccountMarker();
    final media = _AccountMediaClient(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      practice: _AccountPracticeClient(account),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await _restorePractice(controller, 'A');
    await controller.toggleRecordingAudio('audio-A');

    final stop = player.blockNextStop();
    final deletion = controller.deleteRecording('audio-A');
    await stop.entered.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await _restorePractice(controller, 'B');
    stop.release.complete();
    await deletion;

    expect(media.deletions, isEmpty);
    expect(controller.recordings.single.audioAssetId, 'audio-B');
  });

  test('logout fences microphone start waiting on old playback', () async {
    final account = _AccountMarker();
    final recorder = _AccountRecorder(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      practice: _AccountPracticeClient(account),
      recorder: recorder,
      player: player,
    );
    addTearDown(controller.dispose);
    await _restorePractice(controller, 'A');

    final stop = player.blockNextStop();
    final recording = controller.startRecording();
    await stop.entered.future;

    final cleanup = controller.clearPrivateState();
    account.value = 'B';
    stop.release.complete();
    await recording;
    await cleanup;
    await _restorePractice(controller, 'B');

    expect(recorder.startAccounts, isEmpty);
    expect(controller.practiceSessionId, 'session-B');
  });

  test('account switch fences old Candidate before confirmation', () async {
    final account = _AccountMarker();
    final practice = _AccountPracticeClient(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(practice: practice, player: player);
    addTearDown(controller.dispose);
    await _restorePractice(controller, 'A');
    await controller.startRecording();
    await controller.stopRecording();
    expect(controller.candidateId, 'candidate-A');

    final stop = player.blockNextStop();
    final confirmation = controller.confirmTranscript();
    await stop.entered.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await _restorePractice(controller, 'B');
    stop.release.complete();
    await confirmation;

    expect(practice.confirmCalls, isEmpty);
    expect(controller.practiceSessionId, 'session-B');
  });
}

Future<void> _restorePractice(PracticeController controller, String account) =>
    controller.restoreCreatedPractice(
      sessionId: 'session-$account',
      scene: testScenes.first,
    );

PracticeController _controller({
  required _AccountPracticeClient practice,
  _AccountRecorder? recorder,
  _AccountMediaClient? media,
  required _GatedAudioPlayer player,
}) {
  final account = practice.account;
  return PracticeController(
    client: practice,
    recorder: recorder ?? _AccountRecorder(account),
    mediaClient: media ?? _AccountMediaClient(account),
    audioPlayer: player,
    clientIdFactory: (scope) => '$scope-stable-id',
  );
}

final class _AccountMarker {
  String value = 'A';
}

final class _AccountPracticeClient implements PracticeClient {
  _AccountPracticeClient(this.account, {this.blockFirstRestore = false});

  final _AccountMarker account;
  final bool blockFirstRestore;
  final Completer<void> firstRestoreStarted = Completer<void>();
  final Completer<PracticeSessionSnapshot> firstRestoreResult =
      Completer<PracticeSessionSnapshot>();
  final List<String> restoreCalls = <String>[];
  final List<String> activationCalls = <String>[];
  final List<String> confirmCalls = <String>[];

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot> restorePractice({required String sessionId}) {
    restoreCalls.add('${account.value}:$sessionId');
    if (blockFirstRestore && restoreCalls.length == 1) {
      firstRestoreStarted.complete();
      return firstRestoreResult.future;
    }
    return Future<PracticeSessionSnapshot>.value(
      _practiceSnapshot(account.value),
    );
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    activationCalls.add('${account.value}:$sessionId');
    return _practiceSnapshot(account.value);
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    return TranscriptionCandidate(
      id: 'candidate-${account.value}',
      sessionId: request.sessionId,
      questionId: request.questionId,
      text: 'answer-${account.value}',
    );
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    confirmCalls.add('${account.value}:$candidateId');
    return PracticeTurnConfirmation(
      turnId: 'turn-${account.value}-2',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      answer: PracticeMessage(
        id: 'answer-${account.value}',
        role: PracticeMessageRole.user,
        text: 'answer-${account.value}',
      ),
      completedTurns: 2,
      turnLimit: 2,
      sessionCompleted: true,
      audioAssetId: 'audio-${account.value}-2',
    );
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

final class _AccountMediaClient implements PracticeMediaClient {
  _AccountMediaClient(this.account);

  final _AccountMarker account;
  final List<String> recordingLoads = <String>[];
  final List<String> deletions = <String>[];

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) async => _wave();

  @override
  Future<Uint8List> loadRecording(String audioAssetId) async {
    recordingLoads.add('${account.value}:$audioAssetId');
    return _wave();
  }

  @override
  Future<void> deleteRecording(String audioAssetId) async {
    deletions.add('${account.value}:$audioAssetId');
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

final class _GatedAudioPlayer implements PracticeAudioPlayer {
  Completer<void>? _nextStopGate;
  Completer<void>? _nextStopEntered;

  ({Completer<void> entered, Completer<void> release}) blockNextStop() {
    final entered = Completer<void>();
    final release = Completer<void>();
    _nextStopEntered = entered;
    _nextStopGate = release;
    return (entered: entered, release: release);
  }

  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> playWav(Uint8List bytes) async {}

  @override
  Future<void> stop() async {
    final entered = _nextStopEntered;
    final gate = _nextStopGate;
    _nextStopEntered = null;
    _nextStopGate = null;
    entered?.complete();
    await gate?.future;
  }

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<void> dispose() async {}
}

final class _AccountRecorder implements PracticeRecorder {
  _AccountRecorder(this.account);

  final _AccountMarker account;
  final List<String> startAccounts = <String>[];

  @override
  Future<void> start() async {
    startAccounts.add(account.value);
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    return const RecordedPracticeAudio(
      path: 'account-fence.wav',
      contentType: 'audio/wav',
      sizeBytes: 100,
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> clearAccountState() async {}
}

PracticeSessionSnapshot _practiceSnapshot(String account) {
  final sessionId = 'session-$account';
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: 'plan-$account',
    sceneFamily: testScenes.first.family,
    sceneModel: testScenes.first.model,
    sessionVersion: 1,
    completedTurns: 1,
    turnLimit: 2,
    sessionCompleted: false,
    currentQuestion: PracticeQuestion(
      id: 'question-$account',
      sessionId: sessionId,
      text: 'Question for $account',
      speechPath: '/v1/questions/$account/speech',
    ),
    currentTurn: PracticeTurnSnapshot(
      id: 'turn-$account-1',
      sessionId: sessionId,
      questionId: 'question-$account-1',
      respondentParticipantId: 'participant-$account',
      candidateId: 'candidate-$account-1',
      answerText: 'Previous answer for $account',
      evidenceVersion: 1,
      effectiveTurns: 1,
      sessionCompleted: false,
      audioAssetId: 'audio-$account',
    ),
  );
}

Uint8List _wave() {
  final bytes = Uint8List(44);
  bytes.setAll(0, const [0x52, 0x49, 0x46, 0x46]);
  bytes.setAll(8, const [0x57, 0x41, 0x56, 0x45]);
  return bytes;
}
