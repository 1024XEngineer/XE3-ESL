import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

enum VoiceCapturePhase { idle, starting, recording, busy }

enum VoiceCaptureMode { pressAndHold, tapToToggle }

enum VoiceCaptureReleaseIntent { finish, convertToText, cancel }

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
    required this.holdStarted,
    required this.cancelArmed,
    required this.convertArmed,
    required this.tapMode,
    required this.releaseIntent,
    required this.wrapTarget,
    required this.finishTapCapture,
    required this.convertTapCapture,
    required this.cancelTapCapture,
  });

  final bool pressed;
  final bool holdStarted;
  final bool cancelArmed;
  final bool convertArmed;
  final bool tapMode;
  final VoiceCaptureReleaseIntent releaseIntent;
  final VoiceCaptureTargetBuilder wrapTarget;
  final VoidCallback finishTapCapture;
  final VoidCallback convertTapCapture;
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
    this.onConvertToText,
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
  final VoiceCaptureAction? onConvertToText;
  final VoiceCaptureAction onCancel;
  final VoiceCaptureBuilder builder;
  final bool enabled;
  final VoiceCaptureMode mode;
  final Duration holdDelay;
  final double cancelDistance;

  @override
  State<VoiceCaptureControl> createState() => _VoiceCaptureControlState();
}

class _VoiceCaptureControlState extends State<VoiceCaptureControl> {
  Timer? _holdTimer;
  Offset? _pointerOrigin;
  Offset? _pointerPosition;
  bool _pointerActive = false;
  bool _holdStarted = false;
  bool _tapMode = false;
  bool _startInFlight = false;
  int _operationGeneration = 0;
  VoiceCaptureReleaseIntent _releaseIntent = VoiceCaptureReleaseIntent.finish;
  VoiceCaptureReleaseIntent? _pendingAction;

  bool get _isCapturing =>
      widget.phase == VoiceCapturePhase.starting ||
      widget.phase == VoiceCapturePhase.recording;

  bool get _dualTargetEnabled => widget.onConvertToText != null;

  @override
  void didUpdateWidget(covariant VoiceCaptureControl oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.phase == VoiceCapturePhase.busy ||
        (oldWidget.phase != VoiceCapturePhase.idle &&
            widget.phase == VoiceCapturePhase.idle &&
            !_startInFlight)) {
      _resetInteraction();
      return;
    }
    if (!_dualTargetEnabled &&
        _releaseIntent == VoiceCaptureReleaseIntent.convertToText) {
      _releaseIntent = VoiceCaptureReleaseIntent.finish;
    }
    if (!_dualTargetEnabled &&
        _pendingAction == VoiceCaptureReleaseIntent.convertToText) {
      _pendingAction = VoiceCaptureReleaseIntent.finish;
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
    _pointerPosition = null;
    _pointerActive = false;
    _holdStarted = false;
    _tapMode = false;
    _releaseIntent = VoiceCaptureReleaseIntent.finish;
    _pendingAction = null;
  }

  void _setReleaseIntent(VoiceCaptureReleaseIntent intent) {
    if (intent == _releaseIntent) {
      return;
    }
    setState(() => _releaseIntent = intent);
    unawaited(HapticFeedback.selectionClick());
  }

  VoiceCaptureReleaseIntent _intentForPosition(Offset position) {
    final origin = _pointerOrigin;
    if (origin == null) {
      return VoiceCaptureReleaseIntent.finish;
    }
    final delta = position - origin;
    if (_dualTargetEnabled) {
      if (delta.dy > -widget.cancelDistance) {
        return VoiceCaptureReleaseIntent.cancel;
      }
      return delta.dx <= 0
          ? VoiceCaptureReleaseIntent.finish
          : VoiceCaptureReleaseIntent.convertToText;
    }
    return delta.dy <= -widget.cancelDistance
        ? VoiceCaptureReleaseIntent.cancel
        : VoiceCaptureReleaseIntent.finish;
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
      _releaseIntent = VoiceCaptureReleaseIntent.finish;
      _pointerOrigin = event.position;
      _pointerPosition = event.position;
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
      final intent = _intentForPosition(_pointerPosition ?? event.position);
      setState(() {
        _holdStarted = true;
        _releaseIntent = intent;
      });
      unawaited(HapticFeedback.mediumImpact());
      unawaited(_beginCapture(tapMode: false));
    });
  }

  void _handlePointerMove(PointerMoveEvent event) {
    final origin = _pointerOrigin;
    if (!mounted || !_pointerActive || origin == null) {
      return;
    }
    _pointerPosition = event.position;
    if (_dualTargetEnabled && !_holdStarted) {
      return;
    }
    _setReleaseIntent(_intentForPosition(event.position));
  }

  void _handlePointerUp(PointerUpEvent event) {
    _finishPointer(_releaseIntent);
  }

  void _handlePointerCancel(PointerCancelEvent event) {
    _finishPointer(VoiceCaptureReleaseIntent.cancel);
  }

  void _finishPointer(VoiceCaptureReleaseIntent intent) {
    if (!mounted || !_pointerActive) {
      return;
    }
    _holdTimer?.cancel();
    _holdTimer = null;
    final holdStarted = _holdStarted;
    final endingTapCapture = _tapMode && _isCapturing;
    setState(() {
      _pointerActive = false;
      _holdStarted = false;
      _releaseIntent = VoiceCaptureReleaseIntent.finish;
      _pointerOrigin = null;
      _pointerPosition = null;
    });
    if (holdStarted || endingTapCapture) {
      _requestAction(intent);
      return;
    }
    if (intent != VoiceCaptureReleaseIntent.cancel &&
        widget.phase == VoiceCapturePhase.idle) {
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
      if (_pendingAction == VoiceCaptureReleaseIntent.cancel) {
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

  void _requestAction(VoiceCaptureReleaseIntent action) {
    if (_startInFlight) {
      _pendingAction = action;
      if (mounted) {
        setState(() {
          if (action == VoiceCaptureReleaseIntent.cancel) {
            _tapMode = false;
          }
        });
      }
      return;
    }
    unawaited(_runAction(action, _operationGeneration));
  }

  Future<void> _runAction(
    VoiceCaptureReleaseIntent action,
    int generation,
  ) async {
    if (!mounted || generation != _operationGeneration) {
      return;
    }
    setState(() => _tapMode = false);
    switch (action) {
      case VoiceCaptureReleaseIntent.finish:
        await widget.onFinish();
      case VoiceCaptureReleaseIntent.convertToText:
        await widget.onConvertToText?.call();
      case VoiceCaptureReleaseIntent.cancel:
        await widget.onCancel();
    }
  }

  void _handleSemanticTap() {
    if (!widget.enabled) {
      return;
    }
    if (widget.phase == VoiceCapturePhase.idle) {
      unawaited(_beginCapture(tapMode: true));
    } else if (_isCapturing) {
      _requestAction(VoiceCaptureReleaseIntent.finish);
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
        holdStarted: _holdStarted,
        cancelArmed: _releaseIntent == VoiceCaptureReleaseIntent.cancel,
        convertArmed: _releaseIntent == VoiceCaptureReleaseIntent.convertToText,
        tapMode: _tapMode,
        releaseIntent: _releaseIntent,
        wrapTarget: _wrapTarget,
        finishTapCapture: () =>
            _requestAction(VoiceCaptureReleaseIntent.finish),
        convertTapCapture: () {
          if (_dualTargetEnabled) {
            _requestAction(VoiceCaptureReleaseIntent.convertToText);
          }
        },
        cancelTapCapture: () =>
            _requestAction(VoiceCaptureReleaseIntent.cancel),
      ),
    );
  }
}
