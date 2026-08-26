import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/voice_capture_control.dart';

void main() {
  testWidgets('long press neutral release sends voice exactly once', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    expect(harness.currentState?.starts, 1);
    expect(find.text('send armed'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.sends, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('long press left release cancels', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    await gesture.moveBy(const Offset(-80, 0));
    await tester.pump();
    expect(find.text('cancel armed'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 1);
  });

  testWidgets('long press right release converts to text', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(find.text('convert armed'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.converts, 1);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('moving back to the main target restores send intent', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));
    final origin = tester.getCenter(
      find.byKey(const Key('voice-capture-target')),
    );

    final gesture = await tester.startGesture(origin);
    await tester.pump(const Duration(milliseconds: 180));
    await gesture.moveBy(const Offset(80, -80));
    await tester.pump();
    expect(find.text('convert armed'), findsOneWidget);

    await gesture.moveTo(origin);
    await tester.pump();
    expect(find.text('send armed'), findsOneWidget);
    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.sends, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('system pointer cancel never sends an armed action', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    await gesture.moveBy(const Offset(80, 0));
    await gesture.cancel();
    await tester.pump();

    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 1);
  });

  testWidgets('secondary pointer move and up do not finish active capture', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));
    final target = tester.getCenter(
      find.byKey(const Key('voice-capture-target')),
    );

    final primary = await tester.startGesture(target, pointer: 1);
    await tester.pump(const Duration(milliseconds: 180));
    await primary.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(find.text('convert armed'), findsOneWidget);

    final secondary = await tester.startGesture(target, pointer: 2);
    await secondary.moveBy(const Offset(-160, 0));
    await secondary.up();
    await tester.pump();

    expect(find.text('convert armed'), findsOneWidget);
    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);

    await primary.up();
    await tester.pump();

    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.converts, 1);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('secondary pointer cancel does not cancel active capture', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));
    final target = tester.getCenter(
      find.byKey(const Key('voice-capture-target')),
    );

    final primary = await tester.startGesture(target, pointer: 1);
    await tester.pump(const Duration(milliseconds: 180));
    final secondary = await tester.startGesture(target, pointer: 2);
    await secondary.cancel();
    await tester.pump();

    expect(find.text('send armed'), findsOneWidget);
    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);

    await primary.up();
    await tester.pump();

    expect(harness.currentState?.sends, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('quick tap exposes all three equivalent tap actions', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(key: harness, showTapActions: true),
    );

    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    expect(harness.currentState?.starts, 1);

    await tester.tap(find.byKey(const Key('voice-convert-action')));
    await tester.pump();
    expect(harness.currentState?.converts, 1);

    harness.currentState?.reset();
    await tester.pump();
    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('voice-cancel-action')));
    await tester.pump();
    expect(harness.currentState?.cancels, 1);

    harness.currentState?.reset();
    await tester.pump();
    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    expect(harness.currentState?.sends, 1);
  });

  testWidgets('external restart after busy restores tap send and cancel', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(key: harness, upwardCancelOnly: true),
    );

    harness.currentState?.setPhase(VoiceCapturePhase.busy);
    await tester.pump();
    harness.currentState?.setPhase(VoiceCapturePhase.starting);
    await tester.pump();
    harness.currentState?.setPhase(VoiceCapturePhase.recording);
    await tester.pump();

    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    expect(harness.currentState?.sends, 1);

    harness.currentState?.setPhase(VoiceCapturePhase.starting);
    await tester.pump();
    harness.currentState?.setPhase(VoiceCapturePhase.recording);
    await tester.pump();

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await gesture.moveBy(const Offset(0, -80));
    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.cancels, 1);
  });

  testWidgets('release waits for asynchronous microphone start', (
    tester,
  ) async {
    final startCompleter = Completer<void>();
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(key: harness, startCompleter: startCompleter),
    );

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    await gesture.up();
    await tester.pump();
    expect(harness.currentState?.sends, 0);

    startCompleter.complete();
    await tester.pump();
    expect(harness.currentState?.sends, 1);
  });

  testWidgets('release survives the wrapped target rebuilding away', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(key: harness, detachTargetWhileRecording: true),
    );

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));

    expect(harness.currentState?.starts, 1);
    expect(find.byKey(const Key('voice-capture-target')), findsNothing);
    expect(find.byKey(const Key('detached-capture-target')), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.sends, 1);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('one release action fences later taps until it completes', (
    tester,
  ) async {
    final sendCompleter = Completer<void>();
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(
        key: harness,
        showTapActions: true,
        sendCompleter: sendCompleter,
      ),
    );

    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    expect(harness.currentState?.sends, 1);

    await tester.tap(find.byKey(const Key('voice-convert-action')));
    await tester.tap(find.byKey(const Key('voice-cancel-action')));
    await tester.pump();
    expect(harness.currentState?.sends, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);

    sendCompleter.complete();
    await tester.pump();
  });

  testWidgets('left release during preparation never starts microphone', (
    tester,
  ) async {
    final beforeStartCompleter = Completer<void>();
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(
        key: harness,
        beforeStartCompleter: beforeStartCompleter,
      ),
    );

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    await gesture.moveBy(const Offset(-80, 0));
    await gesture.up();
    await tester.pump();

    beforeStartCompleter.complete();
    await tester.pump();

    expect(harness.currentState?.starts, 0);
    expect(harness.currentState?.sends, 0);
    expect(harness.currentState?.cancels, 1);
  });
}

class _VoiceCaptureHarness extends StatefulWidget {
  const _VoiceCaptureHarness({
    required super.key,
    this.startCompleter,
    this.beforeStartCompleter,
    this.sendCompleter,
    this.showTapActions = false,
    this.detachTargetWhileRecording = false,
    this.upwardCancelOnly = false,
  });

  final Completer<void>? startCompleter;
  final Completer<void>? beforeStartCompleter;
  final Completer<void>? sendCompleter;
  final bool showTapActions;
  final bool detachTargetWhileRecording;
  final bool upwardCancelOnly;

  @override
  State<_VoiceCaptureHarness> createState() => _VoiceCaptureHarnessState();
}

class _VoiceCaptureHarnessState extends State<_VoiceCaptureHarness> {
  VoiceCapturePhase phase = VoiceCapturePhase.idle;
  int starts = 0;
  int sends = 0;
  int converts = 0;
  int cancels = 0;

  Future<void> _start() async {
    starts++;
    setState(() => phase = VoiceCapturePhase.starting);
    await widget.startCompleter?.future;
    if (mounted) {
      setState(() => phase = VoiceCapturePhase.recording);
    }
  }

  Future<void> _send() async {
    sends++;
    await widget.sendCompleter?.future;
    if (!mounted) {
      return;
    }
    setState(() => phase = VoiceCapturePhase.busy);
  }

  void _convert() {
    converts++;
    setState(() => phase = VoiceCapturePhase.busy);
  }

  void _cancel() {
    cancels++;
    setState(() => phase = VoiceCapturePhase.idle);
  }

  void reset() {
    starts = 0;
    sends = 0;
    converts = 0;
    cancels = 0;
    setState(() => phase = VoiceCapturePhase.idle);
  }

  void setPhase(VoiceCapturePhase value) {
    setState(() => phase = value);
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: Scaffold(
        body: Center(
          child: VoiceCaptureControl(
            phase: phase,
            onBeforeStart: () async {
              await widget.beforeStartCompleter?.future;
            },
            onStart: _start,
            onSendVoice: _send,
            onConvertToText: _convert,
            onCancel: _cancel,
            upwardCancelOnly: widget.upwardCancelOnly,
            builder: (context, capture) {
              if (widget.detachTargetWhileRecording &&
                  phase == VoiceCapturePhase.recording) {
                return const SizedBox(
                  key: Key('detached-capture-target'),
                  width: 180,
                  height: 64,
                );
              }
              final target = capture.wrapTarget(
                key: const Key('voice-capture-target'),
                semanticsLabel: phase == VoiceCapturePhase.idle
                    ? '开始录音'
                    : '发送语音',
                child: SizedBox(
                  width: 180,
                  height: 64,
                  child: Center(
                    child: Text(
                      capture.cancelArmed
                          ? 'cancel armed'
                          : capture.convertArmed
                          ? 'convert armed'
                          : capture.holdStarted
                          ? 'send armed'
                          : 'voice capture',
                    ),
                  ),
                ),
              );
              if (!widget.showTapActions) {
                return target;
              }
              return Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  target,
                  TextButton(
                    key: const Key('voice-convert-action'),
                    onPressed: capture.convertTapCapture,
                    child: const Text('convert'),
                  ),
                  TextButton(
                    key: const Key('voice-cancel-action'),
                    onPressed: capture.cancelTapCapture,
                    child: const Text('cancel'),
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}
