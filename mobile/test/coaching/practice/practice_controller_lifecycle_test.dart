import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('completes one formal Practice after its three Turns', () async {
    final client = _CountingPracticeClient(testScenes.first);
    final controller = PracticeController(client: client);
    addTearDown(controller.dispose);

    await activateTestPractice(controller: controller, scene: testScenes.first);

    expect(controller.scene, same(testScenes.first));
    expect(
      controller.practiceMessages.single.role,
      PracticeMessageRole.assistant,
    );

    for (var turn = 1; turn <= 3; turn++) {
      await controller.startRecording();
      expect(controller.recordingState, PracticeRecordingState.recording);

      await controller.stopRecording();
      expect(
        controller.recordingState,
        PracticeRecordingState.awaitingConfirmation,
      );
      expect(controller.completedTurns, turn - 1);

      await controller.confirmTranscript();
      expect(controller.completedTurns, turn);
    }

    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(client.confirmationIds, hasLength(3));
    expect(client.confirmationIds.toSet(), hasLength(3));

    await controller.confirmTranscript();
    expect(client.confirmationIds, hasLength(3));
  });

  test('retries a failed Turn with one stable client Turn identity', () async {
    final client = _FailOnceConfirmationPracticeClient(testScenes.first);
    final controller = PracticeController(client: client);
    addTearDown(controller.dispose);
    await activateTestPractice(controller: controller, scene: testScenes.first);

    await controller.startRecording();
    await controller.stopRecording();
    final transcript = controller.transcript;
    await controller.confirmTranscript();

    expect(controller.completedTurns, 0);
    expect(controller.transcript, transcript);
    expect(
      controller.recordingState,
      PracticeRecordingState.awaitingConfirmation,
    );

    await controller.confirmTranscript();

    expect(controller.completedTurns, 1);
    expect(client.confirmationIds, hasLength(2));
    expect(client.confirmationIds.toSet(), hasLength(1));
  });

  test('never confirms a fourth Turn after Practice completion', () async {
    final scene = testScene(
      id: 'daily-review',
      experience: PracticeExperience.roleplay,
      category: SceneCategory.roleplayDaily,
      name: 'Daily review',
    );
    final client = _CountingPracticeClient(scene);
    final controller = PracticeController(client: client);
    addTearDown(controller.dispose);

    await activateTestPractice(controller: controller, scene: scene);
    for (var turn = 1; turn <= 3; turn++) {
      await controller.startRecording();
      await controller.stopRecording();
      await controller.confirmTranscript();
    }

    expect(controller.completedTurns, 3);
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(client.confirmationIds, hasLength(3));
    expect(client.restoreCalls, 0);

    await controller.confirmTranscript();

    expect(controller.completedTurns, 3);
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(client.confirmationIds, hasLength(3));
    expect(client.restoreCalls, 0);
  });

  test('restores and continues from the authoritative 2 of 3', () async {
    final scene = testScenes.first;
    final practice = _CountingPracticeClient(
      scene,
      initialSnapshot: testPracticeSnapshot(
        scene: scene,
        sessionId: 'session_server_1',
        completedTurns: 2,
      ),
    );
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);

    await controller.restoreCreatedPractice(
      sessionId: 'session_server_1',
      scene: scene,
    );

    expect(controller.scene, same(scene));
    expect(controller.completedTurns, 2);
    expect(controller.recordingState, PracticeRecordingState.idle);

    await controller.startRecording();
    await controller.stopRecording();

    expect(practice.transcribedQuestionIds, <String>[
      'question-session_server_1-3',
    ]);
    await controller.confirmTranscript();
    expect(controller.completedTurns, 3);
    expect(controller.recordingState, PracticeRecordingState.completed);
  });

  test('restores a completed Practice without a retry state', () async {
    final scene = testScene(
      id: 'daily-restored-review',
      experience: PracticeExperience.roleplay,
      category: SceneCategory.roleplayDaily,
      name: 'Daily restored review',
    );
    final practice = _CountingPracticeClient(
      scene,
      initialSnapshot: testPracticeSnapshot(
        scene: scene,
        sessionId: 'session_server_2',
        completedTurns: 3,
      ),
    );
    final controller = PracticeController(client: practice);
    addTearDown(controller.dispose);

    await controller.restoreCreatedPractice(
      sessionId: 'session_server_2',
      scene: scene,
    );

    expect(controller.completedTurns, 3);
    expect(controller.recordingState, PracticeRecordingState.completed);
    expect(practice.restoreCalls, 1);
  });
}

class _CountingPracticeClient implements PracticeClient {
  _CountingPracticeClient(
    SceneDefinition scene, {
    PracticeSessionSnapshot? initialSnapshot,
    this.confirmationFailuresRemaining = 0,
  }) : _delegate = FakePracticeClient(
         practiceExperience: scene.experience,
         sceneCategory: scene.category,
         initialSnapshot: initialSnapshot,
       );

  final FakePracticeClient _delegate;
  final List<String> confirmationIds = <String>[];
  final List<String> transcribedQuestionIds = <String>[];
  int confirmationFailuresRemaining;
  int restoreCalls = 0;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) => _delegate.activatePractice(
    sessionId: sessionId,
    clientOperationId: clientOperationId,
  );

  @override
  Future<PracticeSessionSnapshot> restorePractice({required String sessionId}) {
    restoreCalls++;
    return _delegate.restorePractice(sessionId: sessionId);
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    transcribedQuestionIds.add(request.questionId);
    return _delegate.transcribe(request);
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    confirmationIds.add(idempotencyKey);
    if (confirmationFailuresRemaining > 0) {
      confirmationFailuresRemaining--;
      throw StateError('temporary Practice confirmation failure');
    }
    return _delegate.confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      idempotencyKey: idempotencyKey,
    );
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) => _delegate.submitText(
    sessionId: sessionId,
    questionId: questionId,
    answerText: answerText,
    idempotencyKey: idempotencyKey,
  );
}

final class _FailOnceConfirmationPracticeClient
    extends _CountingPracticeClient {
  _FailOnceConfirmationPracticeClient(super.scene)
    : super(confirmationFailuresRemaining: 1);
}
