import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/practice/immersive_roleplay.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('shows an injected avatar above the live conversation', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _roleplayController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          avatarStatusLabel: '画面已连接',
          avatarSurfaceBuilder: (_) => const ColoredBox(
            key: Key('test-avatar-surface'),
            color: Colors.green,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const Key('immersive-roleplay-page')), findsOneWidget);
    expect(find.byKey(const Key('test-avatar-surface')), findsOneWidget);
    expect(find.text('画面已连接'), findsOneWidget);
    expect(find.byKey(const Key('immersive-live-subtitle')), findsOneWidget);
    expect(
      tester.getSize(find.byKey(const Key('immersive-avatar-region'))).height,
      closeTo(844 * 0.44, 0.1),
    );
    expect(find.byKey(const Key('immersive-conversation-history')), findsOne);
    expect(find.textContaining('评分'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('keeps the existing typed-answer flow in the immersive shell', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    var interruptedBeforeSubmit = false;
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeSubmitText: () async {
            interruptedBeforeSubmit = true;
          },
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byKey(const Key('immersive-open-keyboard')));
    await tester.pump();
    const answer = 'Could I change my reservation to tomorrow morning?';
    await tester.enterText(
      find.byKey(const Key('immersive-text-answer')),
      answer,
    );
    await tester.tap(find.byKey(const Key('immersive-submit-text')));
    await tester.pumpAndSettle();

    expect(
      controller.messages.any(
        (message) =>
            message.role == AgentMessageRole.user && message.text == answer,
      ),
      isTrue,
    );
    expect(find.text(answer), findsOneWidget);
    expect(controller.completedTurns, 1);
    expect(interruptedBeforeSubmit, isTrue);
  });

  testWidgets('bounds avatar interruption before opening the microphone', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    final neverCompletes = Completer<void>();
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeStartRecording: () => neverCompletes.future,
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('immersive-record')));
    await tester.pump(const Duration(milliseconds: 499));
    expect(controller.recordingState, PracticeRecordingState.idle);

    await tester.pump(const Duration(milliseconds: 2));
    await tester.pump();
    expect(controller.recordingState, PracticeRecordingState.recording);
    await controller.cancelRecording();
  });

  testWidgets('interrupts the avatar before tap and hold recording starts', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    final tapInterrupt = Completer<void>();
    var interruptCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeStartRecording: () {
            interruptCalls++;
            return tapInterrupt.future;
          },
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('immersive-record')));
    await tester.pump();
    expect(interruptCalls, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);

    tapInterrupt.complete();
    await tester.pumpAndSettle();
    expect(controller.recordingState, PracticeRecordingState.recording);
    await tester.tap(find.byKey(const Key('immersive-stop-recording')));
    await tester.pumpAndSettle();
    controller.rerecord();
    await tester.pump();

    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onBeforeStartRecording: () async {
            interruptCalls++;
          },
        ),
      ),
    );
    final hold = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    expect(interruptCalls, 2);
    expect(controller.recordingState, PracticeRecordingState.recording);
    await hold.up();
    await tester.pumpAndSettle();
    expect(controller.recordingState, PracticeRecordingState.idle);
    expect(controller.completedTurns, 2);
  });

  testWidgets('supports send, left cancel, and right editable text release', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ImmersiveRoleplayPage(agentController: controller)),
    );

    final send = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    expect(find.byKey(const Key('immersive-voice-targets')), findsOneWidget);
    await send.up();
    await tester.pumpAndSettle();
    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);

    final userTurnsAfterSend = controller.messages
        .where((message) => message.role == AgentMessageRole.user)
        .length;
    final cancel = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await cancel.moveBy(const Offset(-80, 0));
    await tester.pump();
    expect(find.text('松开取消'), findsWidgets);
    await cancel.up();
    await tester.pumpAndSettle();
    expect(controller.completedTurns, 1);
    expect(
      controller.messages
          .where((message) => message.role == AgentMessageRole.user)
          .length,
      userTurnsAfterSend,
    );

    final convert = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await convert.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(find.text('松开转成文字'), findsWidgets);
    await convert.up();
    await tester.pumpAndSettle();

    final textField = tester.widget<TextField>(
      find.byKey(const Key('immersive-text-answer')),
    );
    expect(
      textField.controller?.text,
      'The main trade-off was delivery speed versus reliability, so I reduced the scope first.',
    );
    expect(controller.completedTurns, 1);
    expect(controller.recordingState, PracticeRecordingState.idle);
  });

  testWidgets('keeps failed transcription audio for retry or deletion', (
    tester,
  ) async {
    final practiceClient = _FailOncePracticeClient();
    final controller = await _roleplayController(
      practiceClient: practiceClient,
    );
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ImmersiveRoleplayPage(agentController: controller)),
    );

    final send = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('immersive-record'))),
    );
    await tester.pump(const Duration(milliseconds: 220));
    await send.up();
    await tester.pumpAndSettle();

    expect(controller.hasPendingPracticeAudio, isTrue);
    expect(find.byKey(const Key('immersive-pending-audio')), findsOneWidget);
    expect(
      find.byKey(const Key('immersive-retry-transcription')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('immersive-delete-pending-audio')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('immersive-delete-pending-audio')));
    await tester.pumpAndSettle();
    expect(controller.hasPendingPracticeAudio, isFalse);
    expect(find.byKey(const Key('immersive-record')), findsOneWidget);
  });

  testWidgets('prefers the injected avatar replay action over audio playback', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    var replayCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onReplayQuestion: () async {
            replayCalls++;
          },
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('immersive-replay-question')));
    await tester.pump();

    expect(replayCalls, 1);
  });

  testWidgets('disables injected replay while the microphone is recording', (
    tester,
  ) async {
    final controller = await _roleplayController();
    addTearDown(controller.dispose);
    await controller.startRecording();
    await tester.pumpWidget(
      MaterialApp(
        home: ImmersiveRoleplayPage(
          agentController: controller,
          onReplayQuestion: () async {},
        ),
      ),
    );

    final button = tester.widget<IconButton>(
      find.byKey(const Key('immersive-replay-question')),
    );
    expect(button.onPressed, isNull);
    await controller.cancelRecording();
  });

  testWidgets('fits a compact phone with large text and landscape layout', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final controller = await _roleplayController();
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 568),
            textScaler: TextScaler.linear(2),
          ),
          child: ImmersiveRoleplayPage(agentController: controller),
        ),
      ),
    );
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.byKey(const Key('immersive-record')), findsOneWidget);

    tester.view.physicalSize = const Size(844, 390);
    await tester.pumpWidget(
      MaterialApp(home: ImmersiveRoleplayPage(agentController: controller)),
    );
    await tester.pump();

    expect(
      tester.getSize(find.byKey(const Key('immersive-avatar-region'))).width,
      closeTo(844 * 0.44, 0.1),
    );
    expect(tester.takeException(), isNull);
  });
}

Future<AgentController> _roleplayController({
  PracticeClient? practiceClient,
}) async {
  final controller = AgentController(
    client: FakeAgentClient(),
    practiceClient: practiceClient,
  );
  await controller.initialize();
  await controller.selectScene(
    const AgentScene(
      id: 'daily-hotel',
      title: '酒店入住',
      description: '练习办理入住与需求沟通。',
      scenarioType: 'DAILY',
      presentationMode: AgentScenePresentationMode.immersiveRoleplay,
    ),
  );
  return controller;
}

final class _FailOncePracticeClient implements PracticeClient {
  final _delegate = FakePracticeClient();
  bool _shouldFail = true;

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) =>
      _delegate.restorePractice(threadId: threadId, activeMatter: activeMatter);

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) => _delegate.startPractice(
    threadId: threadId,
    activeMatter: activeMatter,
    clientOperationId: clientOperationId,
  );

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    if (_shouldFail) {
      _shouldFail = false;
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    }
    return _delegate.transcribe(request);
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) => _delegate.confirm(
    sessionId: sessionId,
    questionId: questionId,
    candidateId: candidateId,
    idempotencyKey: idempotencyKey,
  );

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
