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

  testWidgets('three-way neutral release finishes the capture', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness, threeWay: true));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('three-way left release cancels the capture', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness, threeWay: true));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(-80, 0));
    await tester.pump();

    expect(find.text('cancel armed'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 1);
  });

  testWidgets('three-way right release converts the capture to text', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness, threeWay: true));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();

    expect(find.text('convert armed'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.converts, 1);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('three-way drag returning to center finishes', (tester) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness, threeWay: true));

    final origin = tester.getCenter(
      find.byKey(const Key('voice-capture-target')),
    );
    final gesture = await tester.startGesture(origin);
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(find.text('convert armed'), findsOneWidget);

    await gesture.moveTo(origin);
    await tester.pump();
    expect(find.text('voice capture'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.finishes, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('pointer cancel always cancels an armed conversion', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness, threeWay: true));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();
    await gesture.cancel();
    await tester.pump();

    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 1);
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

  testWidgets('default mode ignores a horizontal conversion gesture', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(_VoiceCaptureHarness(key: harness));

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();
    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.finishes, 1);
    expect(harness.currentState?.converts, 0);
    expect(harness.currentState?.cancels, 0);
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

  testWidgets('pending conversion waits for asynchronous start and runs once', (
    tester,
  ) async {
    final startCompleter = Completer<void>();
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(
        key: harness,
        startCompleter: startCompleter,
        threeWay: true,
      ),
    );

    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 200));
    await gesture.moveBy(const Offset(80, 0));
    await gesture.up();
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.converts, 0);

    startCompleter.complete();
    await tester.pump();
    await tester.pump();

    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.converts, 1);
    expect(harness.currentState?.cancels, 0);
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

  testWidgets('convert tap action dispatches conversion in three-way mode', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(
        key: harness,
        threeWay: true,
        showConvertButton: true,
      ),
    );

    await tester.tap(find.byKey(const Key('voice-capture-target')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('voice-convert-action')));
    await tester.pump();

    expect(harness.currentState?.starts, 1);
    expect(harness.currentState?.finishes, 0);
    expect(harness.currentState?.converts, 1);
    expect(harness.currentState?.cancels, 0);
  });

  testWidgets('overlay follows intent and is removed for every lifecycle end', (
    tester,
  ) async {
    final harness = GlobalKey<_VoiceCaptureHarnessState>();
    await tester.pumpWidget(
      _VoiceCaptureHarness(key: harness, threeWay: true, showOverlay: true),
    );

    var gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 179));
    expect(find.byKey(const Key('voice-capture-overlay')), findsNothing);

    await tester.pump(const Duration(milliseconds: 1));
    expect(find.text('finish'), findsOneWidget);

    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(find.text('convertToText'), findsOneWidget);

    await gesture.moveBy(const Offset(-160, 0));
    await tester.pump();
    expect(find.text('cancel'), findsOneWidget);

    await gesture.up();
    await tester.pump();
    expect(find.byKey(const Key('voice-capture-overlay')), findsNothing);

    harness.currentState?.setIdle();
    await tester.pump();
    gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    expect(find.byKey(const Key('voice-capture-overlay')), findsOneWidget);

    harness.currentState?.setBusy();
    await tester.pump();
    await tester.pump();
    expect(find.byKey(const Key('voice-capture-overlay')), findsNothing);
    await gesture.cancel();

    harness.currentState?.setIdle();
    await tester.pump();
    gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const Key('voice-capture-target'))),
    );
    await tester.pump(const Duration(milliseconds: 180));
    expect(find.byKey(const Key('voice-capture-overlay')), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    expect(find.byKey(const Key('voice-capture-overlay')), findsNothing);
    expect(tester.takeException(), isNull);
    await gesture.cancel();
  });
}

class _VoiceCaptureHarness extends StatefulWidget {
  const _VoiceCaptureHarness({
    required super.key,
    this.startCompleter,
    this.beforeStartCompleter,
    this.mode = VoiceCaptureMode.pressAndHold,
    this.threeWay = false,
    this.showOverlay = false,
    this.showConvertButton = false,
  });

  final Completer<void>? startCompleter;
  final Completer<void>? beforeStartCompleter;
  final VoiceCaptureMode mode;
  final bool threeWay;
  final bool showOverlay;
  final bool showConvertButton;

  @override
  State<_VoiceCaptureHarness> createState() => _VoiceCaptureHarnessState();
}

class _VoiceCaptureHarnessState extends State<_VoiceCaptureHarness> {
  VoiceCapturePhase phase = VoiceCapturePhase.idle;
  int starts = 0;
  int finishes = 0;
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

  void _finish() {
    finishes++;
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

  void setIdle() => setState(() => phase = VoiceCapturePhase.idle);

  void setBusy() => setState(() => phase = VoiceCapturePhase.busy);

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
            onConvertToText: widget.threeWay ? _convert : null,
            onCancel: _cancel,
            overlayBuilder: widget.showOverlay
                ? (context, intent) => Center(
                    child: Material(
                      key: const Key('voice-capture-overlay'),
                      child: Text(intent.name),
                    ),
                  )
                : null,
            builder: (context, view) {
              final target = view.wrapTarget(
                key: const Key('voice-capture-target'),
                semanticsLabel: phase == VoiceCapturePhase.idle
                    ? '开始录音'
                    : '结束录音',
                child: SizedBox(
                  width: 180,
                  height: 64,
                  child: Center(
                    child: Text(
                      view.cancelArmed
                          ? 'cancel armed'
                          : view.convertArmed
                          ? 'convert armed'
                          : 'voice capture',
                    ),
                  ),
                ),
              );
              if (!widget.showConvertButton) {
                return target;
              }
              return Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  target,
                  TextButton(
                    key: const Key('voice-convert-action'),
                    onPressed: view.convertTapCapture,
                    child: const Text('convert'),
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
