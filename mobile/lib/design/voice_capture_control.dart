import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

enum VoiceCapturePhase { idle, starting, recording, busy }

enum VoiceCaptureMode { pressAndHold, tapToToggle }

typedef VoiceCaptureAction = FutureOr<void> Function();

typedef VoiceCaptureTargetBuilder =
    Widget Function({
      Key? key,
      required Widget child,
      required String semanticsLabel,
    });

typedef VoiceCaptureBuilder =
    Widget Function(BuildContext context, VoiceCaptureView view);

@immutable
class VoiceCaptureView {
  const VoiceCaptureView({
    required this.pressed,
    required this.cancelArmed,
    required this.tapMode,
    required this.wrapTarget,
    required this.finishTapCapture,
    required this.cancelTapCapture,
  });

  final bool pressed;
  final bool cancelArmed;
  final bool tapMode;
  final VoiceCaptureTargetBuilder wrapTarget;
  final VoidCallback finishTapCapture;
  final VoidCallback cancelTapCapture;
}

class VoiceCaptureControl extends StatefulWidget {
  const VoiceCaptureControl({
    required this.phase,
    required this.onStart,
    required this.onFinish,
    required this.onCancel,
    required this.builder,
    this.onBeforeStart,
    this.enabled = true,
    this.mode = VoiceCaptureMode.pressAndHold,
    this.holdDelay = const Duration(milliseconds: 180),
    this.cancelDistance = 72,
    super.key,
  });

  final VoiceCapturePhase phase;
  final VoiceCaptureAction onStart;
  final VoiceCaptureAction? onBeforeStart;
  final VoiceCaptureAction onFinish;
  final VoiceCaptureAction onCancel;
  final VoiceCaptureBuilder builder;
  final bool enabled;
  final VoiceCaptureMode mode;
  final Duration holdDelay;
  final double cancelDistance;

  @override
  State<VoiceCaptureControl> createState() => _VoiceCaptureControlState();
}

enum _PendingCaptureAction { finish, cancel }

class _VoiceCaptureControlState extends State<VoiceCaptureControl> {
  Timer? _holdTimer;
  Offset? _pointerOrigin;
  bool _pointerActive = false;
  bool _holdStarted = false;
  bool _cancelArmed = false;
  bool _tapMode = false;
  bool _startInFlight = false;
  int _operationGeneration = 0;
  _PendingCaptureAction? _pendingAction;

  bool get _isCapturing =>
      widget.phase == VoiceCapturePhase.starting ||
      widget.phase == VoiceCapturePhase.recording;

  @override
  void didUpdateWidget(covariant VoiceCaptureControl oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.phase == VoiceCapturePhase.busy ||
        (oldWidget.phase != VoiceCapturePhase.idle &&
            widget.phase == VoiceCapturePhase.idle &&
            !_startInFlight)) {
      _resetInteraction();
    }
  }

  @override
  void dispose() {
    _holdTimer?.cancel();
    _operationGeneration++;
    super.dispose();
  }

  void _resetInteraction() {
    _holdTimer?.cancel();
    _holdTimer = null;
    _pointerOrigin = null;
    _pointerActive = false;
    _holdStarted = false;
    _cancelArmed = false;
    _tapMode = false;
    _pendingAction = null;
  }

  void _handlePointerDown(PointerDownEvent event) {
    if (!widget.enabled || _pointerActive) {
      return;
    }
    final startingCapture = widget.phase == VoiceCapturePhase.idle;
    final endingTapCapture = _tapMode && _isCapturing;
    if (!startingCapture && !endingTapCapture) {
      return;
    }
    _holdTimer?.cancel();
    setState(() {
      _pointerActive = true;
      _holdStarted = false;
      _cancelArmed = false;
      _pointerOrigin = event.position;
    });
    if (!startingCapture || widget.mode == VoiceCaptureMode.tapToToggle) {
      return;
    }
    _holdTimer = Timer(widget.holdDelay, () {
      if (!mounted ||
          !_pointerActive ||
          widget.phase != VoiceCapturePhase.idle) {
        return;
      }
      setState(() => _holdStarted = true);
      unawaited(HapticFeedback.mediumImpact());
      unawaited(_beginCapture(tapMode: false));
    });
  }

  void _handlePointerMove(PointerMoveEvent event) {
    final origin = _pointerOrigin;
    if (!_pointerActive || origin == null) {
      return;
    }
    final cancelArmed = event.position.dy <= origin.dy - widget.cancelDistance;
    if (cancelArmed == _cancelArmed) {
      return;
    }
    setState(() => _cancelArmed = cancelArmed);
    unawaited(HapticFeedback.selectionClick());
  }

  void _handlePointerUp(PointerUpEvent event) {
    _finishPointer(cancel: _cancelArmed);
  }

  void _handlePointerCancel(PointerCancelEvent event) {
    _finishPointer(cancel: true);
  }

  void _finishPointer({required bool cancel}) {
    if (!_pointerActive) {
      return;
    }
    _holdTimer?.cancel();
    _holdTimer = null;
    final holdStarted = _holdStarted;
    final endingTapCapture = _tapMode && _isCapturing;
    setState(() {
      _pointerActive = false;
      _holdStarted = false;
      _cancelArmed = false;
      _pointerOrigin = null;
    });
    if (holdStarted || endingTapCapture) {
      _requestAction(
        cancel ? _PendingCaptureAction.cancel : _PendingCaptureAction.finish,
      );
      return;
    }
    if (!cancel && widget.phase == VoiceCapturePhase.idle) {
      unawaited(_beginCapture(tapMode: true));
    }
  }

  Future<void> _beginCapture({required bool tapMode}) async {
    if (!widget.enabled ||
        widget.phase != VoiceCapturePhase.idle ||
        _startInFlight) {
      return;
    }
    final generation = ++_operationGeneration;
    setState(() {
      _tapMode = tapMode;
      _startInFlight = true;
      _pendingAction = null;
    });
    if (tapMode) {
      unawaited(HapticFeedback.mediumImpact());
    }
    try {
      await widget.onBeforeStart?.call();
      if (_pendingAction == _PendingCaptureAction.cancel) {
        return;
      }
      await widget.onStart();
    } finally {
      if (mounted && generation == _operationGeneration) {
        setState(() => _startInFlight = false);
        final pending = _pendingAction;
        if (pending != null) {
          _pendingAction = null;
          await _runAction(pending, generation);
        }
      }
    }
  }

  void _requestAction(_PendingCaptureAction action) {
    if (_startInFlight) {
      _pendingAction = action;
      if (mounted) {
        setState(() {
          if (action == _PendingCaptureAction.cancel) {
            _tapMode = false;
          }
        });
      }
      return;
    }
    unawaited(_runAction(action, _operationGeneration));
  }

  Future<void> _runAction(_PendingCaptureAction action, int generation) async {
    if (!mounted || generation != _operationGeneration) {
      return;
    }
    setState(() => _tapMode = false);
    if (action == _PendingCaptureAction.cancel) {
      await widget.onCancel();
    } else {
      await widget.onFinish();
    }
  }

  void _handleSemanticTap() {
    if (!widget.enabled) {
      return;
    }
    if (widget.phase == VoiceCapturePhase.idle) {
      unawaited(_beginCapture(tapMode: true));
    } else if (_isCapturing) {
      _requestAction(_PendingCaptureAction.finish);
    }
  }

  Widget _wrapTarget({
    Key? key,
    required Widget child,
    required String semanticsLabel,
  }) {
    return Semantics(
      button: true,
      enabled: widget.enabled,
      label: semanticsLabel,
      onTap: widget.enabled ? _handleSemanticTap : null,
      child: ExcludeSemantics(
        child: Listener(
          key: key,
          behavior: HitTestBehavior.opaque,
          onPointerDown: _handlePointerDown,
          onPointerMove: _handlePointerMove,
          onPointerUp: _handlePointerUp,
          onPointerCancel: _handlePointerCancel,
          child: child,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return widget.builder(
      context,
      VoiceCaptureView(
        pressed: _pointerActive,
        cancelArmed: _cancelArmed,
        tapMode: _tapMode,
        wrapTarget: _wrapTarget,
        finishTapCapture: () => _requestAction(_PendingCaptureAction.finish),
        cancelTapCapture: () => _requestAction(_PendingCaptureAction.cancel),
      ),
    );
  }
}
