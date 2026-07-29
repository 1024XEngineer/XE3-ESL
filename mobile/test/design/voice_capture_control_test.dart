import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/voice_capture_control.dart';

void main() {
  testWidgets('quick tap starts and second tap finishes once', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);

    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 1);
  });

  testWidgets('long press starts and release finishes once', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 1);
  });

  testWidgets('swipe up and release cancels the capture', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(0, -80));
    await tester.pump();

    expect(find.text('cancel armed'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.cancels, 1);
  });

  testWidgets('pointer cancel does not start after a quick press', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 100));
    await gesture.cancel();
    await tester.pump(const Duration(milliseconds: 200));

    expect(harness.currentState?.starts, 0);
    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('release waits for an asynchronous microphone start', (
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
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);

    startCompleter.complete();
    await tester.pump();

    expect(harness.currentState?.finishes, 1);
  });

  testWidgets('cancel during preparation never starts the microphone', (
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
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(0, -80));
    await gesture.up();
    await tester.pump();

    beforeStartCompleter.complete();
    await tester.pump();

    expect(harness.currentState?.starts, 0);
    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.cancels, 1);
  });

  testWidgets('tap-to-toggle mode does not start during a hold', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(key: harness, mode: VoiceCaptureMode.tapToToggle),
    );

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 250));

    expect(harness.currentState?.starts, 0);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
  });
}

class _VoiceCaptureHarness extends StatefulWidget {
  const _VoiceCaptureHarness({
    required super.key,
    this.startCompleter,
    this.beforeStartCompleter,
    this.mode = VoiceCaptureMode.pressAndHold,
  });

  final Completer<void>? startCompleter;
  final Completer<void>? beforeStartCompleter;
  final VoiceCaptureMode mode;

  @override
  State<_VoiceCaptureHarness> createState() => _VoiceCaptureHarnessState();
}

class _VoiceCaptureHarnessState extends State<_VoiceCaptureHarness> {
  VoiceCapturePhase phase = VoiceCapturePhase.idle;
  int starts = 0;
  int finishes = 0;
  int cancels = 0;

  Future<void> _start() async {
    starts++;
    setState(() => phase = VoiceCapturePhase.starting);
    await widget.startCompleter?.future;
    if (mounted) {
      setState(() => phase = VoiceCapturePhase.recording);
    }
  }

  void _finish() {
    finishes++;
    setState(() => phase = VoiceCapturePhase.busy);
  }

  void _cancel() {
    cancels++;
    setState(() => phase = VoiceCapturePhase.idle);
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: Scaffold(
        body: Center(
          child: VoiceCaptureControl(
            phase: phase,
            mode: widget.mode,
            onBeforeStart: () async {
              await widget.beforeStartCompleter?.future;
            },
            onStart: _start,
            onFinish: _finish,
            onCancel: _cancel,
            builder: (context, view) => view.wrapTarget(
              key: const Key('voice-capture-target'),
              semanticsLabel: phase == VoiceCapturePhase.idle ? '开始录音' : '结束录音',
              child: SizedBox(
                width: 180,
                height: 64,
                child: Center(
                  child: Text(
                    view.cancelArmed ? 'cancel armed' : 'voice capture',
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
