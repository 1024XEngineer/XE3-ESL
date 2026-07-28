import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('account switch fences an old scene before startScene', () async {
    final account = _AccountMarker();
    final agent = _AccountAgentClient(account);
    final practice = _AccountPracticeClient(account, restoreActive: false);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      agent: agent,
      practice: practice,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    final stop = player.blockNextStop();
    final selection = controller.selectScene(agentScenes.first);
    await stop.entered.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await controller.initialize();
    stop.release.complete();
    await selection;

    expect(agent.sceneCalls, isEmpty);
    expect(practice.startCalls, isEmpty);
    expect(controller.threadId, 'thread-B');
  });

  test('account switch fences old Thread before Practice restore', () async {
    final account = _AccountMarker();
    final agent = _AccountAgentClient(account, blockFirstRestore: true);
    final practice = _AccountPracticeClient(account, restoreActive: false);
    final controller = AgentController(
      client: agent,
      practiceClient: practice,
      recorder: _AccountRecorder(account),
    );
    addTearDown(controller.dispose);

    final oldInitialization = controller.initialize();
    await agent.firstRestoreStarted.future;
    await controller.clearPrivateState();
    account.value = 'B';
    await controller.initialize();

    agent.firstRestoreResult.complete(_threadSnapshot('A'));
    await oldInitialization;

    expect(practice.restoreCalls, ['B:thread-B']);
    expect(controller.threadId, 'thread-B');
  });

  test('account switch fences old recording playback before fetch', () async {
    final account = _AccountMarker();
    final media = _AccountMediaClient(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      agent: _AccountAgentClient(account),
      practice: _AccountPracticeClient(account),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    final stop = player.blockNextStop();
    final playback = controller.toggleRecordingAudio('audio-A');
    await stop.entered.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await controller.initialize();
    stop.release.complete();
    await playback;

    expect(media.recordingLoads, isEmpty);
    expect(controller.recordings.single.audioAssetId, 'audio-B');
  });

  test('account switch fences old recording before delete', () async {
    final account = _AccountMarker();
    final media = _AccountMediaClient(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      agent: _AccountAgentClient(account),
      practice: _AccountPracticeClient(account),
      media: media,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.toggleRecordingAudio('audio-A');

    final stop = player.blockNextStop();
    final deletion = controller.deleteRecording('audio-A');
    await stop.entered.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await controller.initialize();
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
      agent: _AccountAgentClient(account),
      practice: _AccountPracticeClient(account),
      recorder: recorder,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();

    final stop = player.blockNextStop();
    final recording = controller.startRecording();
    await stop.entered.future;

    final cleanup = controller.clearPrivateState();
    account.value = 'B';
    stop.release.complete();
    await recording;
    await cleanup;
    await controller.initialize();

    expect(recorder.startAccounts, isEmpty);
    expect(controller.threadId, 'thread-B');
  });

  test('account switch fences old Candidate before confirmation', () async {
    final account = _AccountMarker();
    final practice = _AccountPracticeClient(account);
    final player = _GatedAudioPlayer();
    final controller = _controller(
      agent: _AccountAgentClient(account),
      practice: practice,
      player: player,
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await controller.startRecording();
    await controller.stopRecording();
    expect(controller.candidateId, 'candidate-A');

    final stop = player.blockNextStop();
    final confirmation = controller.confirmTranscript();
    await stop.entered.future;

    await controller.clearPrivateState();
    account.value = 'B';
    await controller.initialize();
    stop.release.complete();
    await confirmation;

    expect(practice.confirmCalls, isEmpty);
    expect(controller.practiceSessionId, 'session-B');
  });
}

AgentController _controller({
  required _AccountAgentClient agent,
  required _AccountPracticeClient practice,
  _AccountRecorder? recorder,
  _AccountMediaClient? media,
  required _GatedAudioPlayer player,
}) {
  return AgentController(
    client: agent,
    practiceClient: practice,
    recorder: recorder ?? _AccountRecorder(agent.account),
    mediaClient: media ?? _AccountMediaClient(agent.account),
    audioPlayer: player,
    clientIdFactory: (scope) => '$scope-stable-id',
  );
}

final class _AccountMarker {
  String value = 'A';
}

final class _AccountAgentClient implements AgentClient {
  _AccountAgentClient(this.account, {this.blockFirstRestore = false});

  final _AccountMarker account;
  final bool blockFirstRestore;
  final Completer<void> firstRestoreStarted = Completer<void>();
  final Completer<AgentThreadSnapshot> firstRestoreResult =
      Completer<AgentThreadSnapshot>();
  final List<String> sceneCalls = <String>[];
  int _restoreCalls = 0;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<AgentThreadSnapshot> restoreThread() {
    _restoreCalls++;
    if (blockFirstRestore && _restoreCalls == 1) {
      firstRestoreStarted.complete();
      return firstRestoreResult.future;
    }
    return Future<AgentThreadSnapshot>.value(_threadSnapshot(account.value));
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) async {
    sceneCalls.add('${account.value}:$threadId');
    return AgentSceneStart(
      activeMatter: AgentMatter(id: 'matter-${account.value}', scene: scene),
      assistantMessage: AgentMessage(
        id: 'scene-message-${account.value}',
        role: AgentMessageRole.assistant,
        text: scene.title,
      ),
    );
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    throw UnimplementedError();
  }
}

final class _AccountPracticeClient implements PracticeClient {
  _AccountPracticeClient(this.account, {this.restoreActive = true});

  final _AccountMarker account;
  final bool restoreActive;
  final List<String> restoreCalls = <String>[];
  final List<String> startCalls = <String>[];
  final List<String> confirmCalls = <String>[];

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    restoreCalls.add('${account.value}:$threadId');
    return restoreActive ? _practiceSnapshot(account.value) : null;
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    startCalls.add('${account.value}:$threadId');
    return PracticeStartResult(snapshot: _practiceSnapshot(account.value));
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
      answer: AgentMessage(
        id: 'answer-${account.value}',
        role: AgentMessageRole.user,
        text: 'answer-${account.value}',
      ),
      completedTurns: 2,
      turnLimit: 2,
      sessionCompleted: true,
      review: AgentReview(
        id: 'review-${account.value}',
        title: 'Review',
        summary: 'Summary',
        strength: 'Strength',
        nextFocus: 'Next',
      ),
      audioAssetId: 'audio-${account.value}-2',
    );
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

AgentThreadSnapshot _threadSnapshot(String account) {
  return AgentThreadSnapshot(threadId: 'thread-$account');
}

PracticeSessionSnapshot _practiceSnapshot(String account) {
  final sessionId = 'session-$account';
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    threadId: 'thread-$account',
    matter: AgentMatter(id: 'matter-$account', scene: agentScenes.first),
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
