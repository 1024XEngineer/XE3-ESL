import 'dart:async';

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

enum VoiceCapturePhase { idle, starting, recording, busy }

enum VoiceCaptureMode { pressAndHold, tapToToggle }

enum VoiceCaptureReleaseIntent { sendVoice, convertToText, cancel }

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
    required this.sendVoiceTapCapture,
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
  final VoidCallback sendVoiceTapCapture;
  final VoidCallback convertTapCapture;
  final VoidCallback cancelTapCapture;
}

class VoiceCaptureControl extends StatefulWidget {
  const VoiceCaptureControl({
    required this.phase,
    required this.onStart,
    required this.onSendVoice,
    required this.onConvertToText,
    required this.onCancel,
    required this.builder,
    this.onBeforeStart,
    this.enabled = true,
    this.mode = VoiceCaptureMode.pressAndHold,
    this.holdDelay = const Duration(milliseconds: 180),
    this.intentDistance = 72,
    this.upwardCancelOnly = false,
    super.key,
  });

  final VoiceCapturePhase phase;
  final VoiceCaptureAction onStart;
  final VoiceCaptureAction? onBeforeStart;
  final VoiceCaptureAction onSendVoice;
  final VoiceCaptureAction onConvertToText;
  final VoiceCaptureAction onCancel;
  final VoiceCaptureBuilder builder;
  final bool enabled;
  final VoiceCaptureMode mode;
  final Duration holdDelay;
  final double intentDistance;
  final bool upwardCancelOnly;

  @override
  State<VoiceCaptureControl> createState() => _VoiceCaptureControlState();
}

class _VoiceCaptureControlState extends State<VoiceCaptureControl> {
  Timer? _holdTimer;
  Offset? _pointerOrigin;
  Offset? _pointerPosition;
  int? _activePointer;
  bool _pointerActive = false;
  bool _holdStarted = false;
  bool _tapMode = false;
  bool _startInFlight = false;
  bool _actionInFlight = false;
  int _operationGeneration = 0;
  VoiceCaptureReleaseIntent _releaseIntent =
      VoiceCaptureReleaseIntent.sendVoice;
  VoiceCaptureReleaseIntent? _pendingAction;

  bool get _isCapturing =>
      widget.phase == VoiceCapturePhase.starting ||
      widget.phase == VoiceCapturePhase.recording;

  @override
  void didUpdateWidget(covariant VoiceCaptureControl oldWidget) {
    super.didUpdateWidget(oldWidget);
    final wasCapturing =
        oldWidget.phase == VoiceCapturePhase.starting ||
        oldWidget.phase == VoiceCapturePhase.recording;
    if (!wasCapturing && _isCapturing && !_pointerActive && !_startInFlight) {
      _tapMode = true;
    }
    if (widget.phase == VoiceCapturePhase.busy ||
        (oldWidget.phase != VoiceCapturePhase.idle &&
            widget.phase == VoiceCapturePhase.idle &&
            !_startInFlight)) {
      _resetInteraction();
    }
  }

  @override
  void dispose() {
    _removePointerRoute();
    _holdTimer?.cancel();
    _operationGeneration++;
    super.dispose();
  }

  void _resetInteraction() {
    _removePointerRoute();
    _holdTimer?.cancel();
    _holdTimer = null;
    _pointerOrigin = null;
    _pointerPosition = null;
    _activePointer = null;
    _pointerActive = false;
    _holdStarted = false;
    _tapMode = false;
    _releaseIntent = VoiceCaptureReleaseIntent.sendVoice;
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
      return VoiceCaptureReleaseIntent.sendVoice;
    }
    if (widget.upwardCancelOnly) {
      return position.dy - origin.dy <= -widget.intentDistance
          ? VoiceCaptureReleaseIntent.cancel
          : VoiceCaptureReleaseIntent.sendVoice;
    }
    final horizontalDistance = position.dx - origin.dx;
    if (horizontalDistance <= -widget.intentDistance) {
      return VoiceCaptureReleaseIntent.cancel;
    }
    if (horizontalDistance >= widget.intentDistance) {
      return VoiceCaptureReleaseIntent.convertToText;
    }
    return VoiceCaptureReleaseIntent.sendVoice;
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
      _activePointer = event.pointer;
      _pointerActive = true;
      _holdStarted = false;
      _releaseIntent = VoiceCaptureReleaseIntent.sendVoice;
      _pointerOrigin = event.position;
      _pointerPosition = event.position;
    });
    GestureBinding.instance.pointerRouter.addRoute(
      event.pointer,
      _handleTrackedPointerEvent,
    );
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
    if (!mounted ||
        event.pointer != _activePointer ||
        !_pointerActive ||
        _pointerOrigin == null) {
      return;
    }
    _pointerPosition = event.position;
    if (!_holdStarted && !(_tapMode && _isCapturing)) {
      return;
    }
    _setReleaseIntent(_intentForPosition(event.position));
  }

  void _handlePointerUp(PointerUpEvent event) {
    if (event.pointer != _activePointer) {
      return;
    }
    _finishPointer(
      widget.upwardCancelOnly
          ? _intentForPosition(event.position)
          : _releaseIntent,
    );
  }

  void _handlePointerCancel(PointerCancelEvent event) {
    if (event.pointer != _activePointer) {
      return;
    }
    _finishPointer(VoiceCaptureReleaseIntent.cancel);
  }

  void _handleTrackedPointerEvent(PointerEvent event) {
    if (event.pointer != _activePointer) {
      return;
    }
    switch (event) {
      case final PointerMoveEvent move:
        _handlePointerMove(move);
        return;
      case final PointerUpEvent up:
        _handlePointerUp(up);
        return;
      case final PointerCancelEvent cancel:
        _handlePointerCancel(cancel);
        return;
      default:
        return;
    }
  }

  void _removePointerRoute() {
    final pointer = _activePointer;
    if (pointer == null) {
      return;
    }
    GestureBinding.instance.pointerRouter.removeRoute(
      pointer,
      _handleTrackedPointerEvent,
    );
  }

  void _finishPointer(VoiceCaptureReleaseIntent intent) {
    if (!mounted || !_pointerActive) {
      return;
    }
    _holdTimer?.cancel();
    _holdTimer = null;
    _removePointerRoute();
    final holdStarted = _holdStarted;
    final endingTapCapture = _tapMode && _isCapturing;
    setState(() {
      _activePointer = null;
      _pointerActive = false;
      _holdStarted = false;
      _releaseIntent = VoiceCaptureReleaseIntent.sendVoice;
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
    if (_actionInFlight) {
      return;
    }
    if (_startInFlight) {
      _pendingAction = action;
      if (mounted && action == VoiceCaptureReleaseIntent.cancel) {
        setState(() => _tapMode = false);
      }
      return;
    }
    unawaited(_runAction(action, _operationGeneration));
  }

  Future<void> _runAction(
    VoiceCaptureReleaseIntent action,
    int generation,
  ) async {
    if (!mounted || generation != _operationGeneration || _actionInFlight) {
      return;
    }
    setState(() {
      _tapMode = false;
      _actionInFlight = true;
    });
    try {
      switch (action) {
        case VoiceCaptureReleaseIntent.sendVoice:
          await widget.onSendVoice();
        case VoiceCaptureReleaseIntent.convertToText:
          await widget.onConvertToText();
        case VoiceCaptureReleaseIntent.cancel:
          await widget.onCancel();
      }
    } finally {
      if (mounted && generation == _operationGeneration) {
        setState(() => _actionInFlight = false);
      }
    }
  }

  void _handleSemanticTap() {
    if (!widget.enabled) {
      return;
    }
    if (widget.phase == VoiceCapturePhase.idle) {
      unawaited(_beginCapture(tapMode: true));
    } else if (_isCapturing) {
      _requestAction(VoiceCaptureReleaseIntent.sendVoice);
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
        sendVoiceTapCapture: () =>
            _requestAction(VoiceCaptureReleaseIntent.sendVoice),
        convertTapCapture: () =>
            _requestAction(VoiceCaptureReleaseIntent.convertToText),
        cancelTapCapture: () =>
            _requestAction(VoiceCaptureReleaseIntent.cancel),
      ),
    );
  }
}
