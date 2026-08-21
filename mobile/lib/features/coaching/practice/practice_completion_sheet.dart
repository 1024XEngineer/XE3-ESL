import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

/// Blocks the active practice surface and presents the available actions after
/// the server has confirmed that the current practice session is complete.
class PracticeCompletionOverlay extends StatefulWidget {
  const PracticeCompletionOverlay({
    required this.title,
    required this.message,
    required this.primaryLabel,
    required this.secondaryLabel,
    required this.onPrimary,
    required this.onSecondary,
    this.dismissible = true,
    this.onDismissed,
    this.primaryLoading = false,
    this.keyPrefix = 'practice-completion',
    this.sheetKey,
    this.primaryKey,
    this.secondaryKey,
    super.key,
  });

  final String title;
  final String message;
  final String primaryLabel;
  final String secondaryLabel;
  final VoidCallback? onPrimary;
  final VoidCallback? onSecondary;
  final bool dismissible;
  final VoidCallback? onDismissed;
  final bool primaryLoading;
  final String keyPrefix;
  final Key? sheetKey;
  final Key? primaryKey;
  final Key? secondaryKey;

  @override
  State<PracticeCompletionOverlay> createState() =>
      _PracticeCompletionOverlayState();
}

class _PracticeCompletionOverlayState extends State<PracticeCompletionOverlay>
    with SingleTickerProviderStateMixin {
  static const double _dismissDistanceRatio = 0.25;
  static const double _dismissVelocity = 900;
  static const Duration _dismissDuration = Duration(milliseconds: 220);
  static const Duration _reboundDuration = Duration(milliseconds: 180);

  final GlobalKey _sheetMeasureKey = GlobalKey();
  late final AnimationController _animationController;
  Animation<double>? _offsetAnimation;
  double _dragOffset = 0;
  double? _measuredSheetHeight;
  bool _hidden = false;

  double get _currentOffset => _offsetAnimation?.value ?? _dragOffset;

  double _readSheetHeight() {
    final measuredHeight = _sheetMeasureKey.currentContext?.size?.height;
    if (measuredHeight != null) {
      _measuredSheetHeight = measuredHeight;
    }
    return _measuredSheetHeight ?? MediaQuery.sizeOf(context).height * 0.45;
  }

  @override
  void initState() {
    super.initState();
    _animationController = AnimationController(vsync: this)
      ..addListener(() {
        if (mounted) {
          setState(() {});
        }
      });
  }

  @override
  void dispose() {
    _animationController.dispose();
    super.dispose();
  }

  void _handleDragUpdate(DragUpdateDetails details) {
    if (!widget.dismissible || _animationController.isAnimating) {
      return;
    }
    _readSheetHeight();
    setState(() {
      _offsetAnimation = null;
      _dragOffset = (_dragOffset + details.delta.dy).clamp(
        0.0,
        double.infinity,
      );
    });
  }

  void _handleDragEnd(DragEndDetails details) {
    if (!widget.dismissible || _animationController.isAnimating) {
      return;
    }
    final downwardVelocity = details.primaryVelocity ?? 0;
    final sheetHeight = _readSheetHeight();
    final shouldDismiss =
        _dragOffset >= sheetHeight * _dismissDistanceRatio ||
        downwardVelocity >= _dismissVelocity;
    if (shouldDismiss) {
      unawaited(_animateDismiss());
    } else {
      unawaited(_animateBack());
    }
  }

  Future<void> _animateBack() async {
    _offsetAnimation = Tween<double>(begin: _dragOffset, end: 0).animate(
      CurvedAnimation(parent: _animationController, curve: Curves.easeOutCubic),
    );
    _animationController.duration = _reboundDuration;
    await _animationController.forward(from: 0);
    if (!mounted) {
      return;
    }
    _animationController.reset();
    setState(() {
      _dragOffset = 0;
      _offsetAnimation = null;
    });
  }

  Future<void> _animateDismiss() async {
    if (!widget.dismissible || _hidden || _animationController.isAnimating) {
      return;
    }
    final targetOffset = MediaQuery.sizeOf(context).height + _readSheetHeight();
    _offsetAnimation = Tween<double>(begin: _dragOffset, end: targetOffset)
        .animate(
          CurvedAnimation(
            parent: _animationController,
            curve: Curves.easeOutCubic,
          ),
        );
    _animationController.duration = _dismissDuration;
    await _animationController.forward(from: 0);
    if (!mounted || _hidden) {
      return;
    }
    setState(() => _hidden = true);
    widget.onDismissed?.call();
  }

  @override
  Widget build(BuildContext context) {
    if (_hidden) {
      return const SizedBox.shrink();
    }
    final sheetHeight =
        _measuredSheetHeight ?? MediaQuery.sizeOf(context).height * 0.45;
    final dismissProgress = (_currentOffset / sheetHeight).clamp(0.0, 1.0);
    return Stack(
      key: Key('${widget.keyPrefix}-overlay'),
      fit: StackFit.expand,
      children: [
        ModalBarrier(
          dismissible: false,
          color: Color.lerp(
            const Color(0x14000000),
            Colors.transparent,
            dismissProgress,
          ),
          semanticsLabel: widget.title,
        ),
        Align(
          alignment: Alignment.bottomCenter,
          child: SafeArea(
            top: false,
            child: SingleChildScrollView(
              reverse: true,
              padding: const EdgeInsets.fromLTRB(10, 0, 10, 8),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 560),
                child: Transform.translate(
                  offset: Offset(0, _currentOffset),
                  child: KeyedSubtree(
                    key: _sheetMeasureKey,
                    child: Material(
                      key: widget.sheetKey ?? Key('${widget.keyPrefix}-sheet'),
                      color: SpeakUpDesign.surface,
                      elevation: 12,
                      shadowColor: const Color(0x26000000),
                      borderRadius: BorderRadius.circular(28),
                      clipBehavior: Clip.antiAlias,
                      child: Padding(
                        padding: const EdgeInsets.fromLTRB(20, 12, 20, 16),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            GestureDetector(
                              key: Key('${widget.keyPrefix}-drag-region'),
                              behavior: HitTestBehavior.opaque,
                              onVerticalDragUpdate: widget.dismissible
                                  ? _handleDragUpdate
                                  : null,
                              onVerticalDragEnd: widget.dismissible
                                  ? _handleDragEnd
                                  : null,
                              child: Semantics(
                                container: true,
                                hint: widget.dismissible
                                    ? '向下拖动可关闭完成提示并查看练习对话'
                                    : null,
                                onTap: widget.dismissible
                                    ? () => unawaited(_animateDismiss())
                                    : null,
                                child: Column(
                                  children: [
                                    Center(
                                      child: Container(
                                        width: 42,
                                        height: 4,
                                        decoration: BoxDecoration(
                                          color: SpeakUpDesign.border,
                                          borderRadius: BorderRadius.circular(
                                            99,
                                          ),
                                        ),
                                      ),
                                    ),
                                    const SizedBox(height: 18),
                                    Center(
                                      child: Container(
                                        width: 52,
                                        height: 52,
                                        decoration: BoxDecoration(
                                          shape: BoxShape.circle,
                                          border: Border.all(
                                            color: SpeakUpDesign.ink,
                                            width: 2,
                                          ),
                                        ),
                                        child: const Icon(
                                          Icons.check_rounded,
                                          size: 34,
                                          color: SpeakUpDesign.ink,
                                        ),
                                      ),
                                    ),
                                    const SizedBox(height: 16),
                                    Text(
                                      widget.title,
                                      textAlign: TextAlign.center,
                                      style: SpeakUpDesign.pageTitle.copyWith(
                                        fontSize: 24,
                                      ),
                                    ),
                                    const SizedBox(height: 6),
                                    Text(
                                      widget.message,
                                      textAlign: TextAlign.center,
                                      style: SpeakUpDesign.body.copyWith(
                                        color: SpeakUpDesign.secondary,
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            ),
                            const SizedBox(height: 20),
                            FilledButton(
                              key:
                                  widget.primaryKey ??
                                  Key('${widget.keyPrefix}-primary'),
                              onPressed: widget.primaryLoading
                                  ? null
                                  : widget.onPrimary,
                              style: FilledButton.styleFrom(
                                minimumSize: const Size.fromHeight(54),
                                backgroundColor: SpeakUpDesign.ink,
                                foregroundColor: Colors.white,
                                shape: RoundedRectangleBorder(
                                  borderRadius: BorderRadius.circular(14),
                                ),
                              ),
                              child: widget.primaryLoading
                                  ? const SizedBox.square(
                                      dimension: 20,
                                      child: CircularProgressIndicator(
                                        strokeWidth: 2,
                                        color: Colors.white,
                                      ),
                                    )
                                  : Text(widget.primaryLabel),
                            ),
                            const SizedBox(height: 4),
                            TextButton(
                              key:
                                  widget.secondaryKey ??
                                  Key('${widget.keyPrefix}-secondary'),
                              onPressed: widget.onSecondary,
                              style: TextButton.styleFrom(
                                foregroundColor: SpeakUpDesign.ink,
                                minimumSize: const Size.fromHeight(44),
                              ),
                              child: Text(widget.secondaryLabel),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}
