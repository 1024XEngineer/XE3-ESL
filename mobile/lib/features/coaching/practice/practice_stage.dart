import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

/// Shared practice layout: examiner/role stage above the active conversation.
class PracticeStageLayout extends StatelessWidget {
  const PracticeStageLayout({
    required this.stage,
    required this.content,
    this.stageRegionKey,
    this.portraitStageFraction = 0.34,
    super.key,
  });

  final Widget stage;
  final Widget content;
  final Key? stageRegionKey;
  final double portraitStageFraction;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      if (constraints.maxWidth > constraints.maxHeight) {
        return Row(
          children: [
            SizedBox(
              key: stageRegionKey,
              width: constraints.maxWidth * 0.44,
              child: stage,
            ),
            Expanded(child: content),
          ],
        );
      }
      return Column(
        children: [
          SizedBox(
            key: stageRegionKey,
            height: constraints.maxHeight * portraitStageFraction,
            child: stage,
          ),
          Expanded(child: content),
        ],
      );
    },
  );
}

/// Stable stage chrome. A live renderer stays mounted behind [fallback] until
/// it is ready to be shown, so connecting and replacement do not flash.
class PracticeRoleStage extends StatelessWidget {
  const PracticeRoleStage({
    required this.title,
    required this.fallback,
    required this.onExit,
    this.surfaceBuilder,
    this.surfaceVisible = true,
    this.statusLabel,
    this.exitInFlight = false,
    this.exitButtonKey,
    super.key,
  });

  final String title;
  final Widget fallback;
  final VoidCallback onExit;
  final WidgetBuilder? surfaceBuilder;
  final bool surfaceVisible;
  final String? statusLabel;
  final bool exitInFlight;
  final Key? exitButtonKey;

  @override
  Widget build(BuildContext context) => ColoredBox(
    color: const Color(0xFFE5E9E5),
    child: Stack(
      fit: StackFit.expand,
      children: [
        if (surfaceBuilder case final builder?) builder(context),
        if (surfaceBuilder == null || !surfaceVisible) fallback,
        const Positioned.fill(
          child: IgnorePointer(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: [
                    Color(0x33000000),
                    Colors.transparent,
                    Color(0x4D000000),
                  ],
                  stops: [0, 0.45, 1],
                ),
              ),
            ),
          ),
        ),
        Positioned(
          left: 12,
          right: 12,
          top: 10,
          child: Row(
            children: [
              IconButton.filledTonal(
                key: exitButtonKey,
                tooltip: '返回',
                onPressed: exitInFlight ? null : onExit,
                icon: exitInFlight
                    ? const SizedBox.square(
                        dimension: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.arrow_back_rounded),
                style: IconButton.styleFrom(
                  backgroundColor: Colors.white.withValues(alpha: 0.9),
                  foregroundColor: SpeakUpDesign.ink,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 16,
                    fontWeight: FontWeight.w700,
                    shadows: [Shadow(color: Color(0x66000000), blurRadius: 8)],
                  ),
                ),
              ),
              if (statusLabel case final label?)
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 7,
                  ),
                  decoration: BoxDecoration(
                    color: Colors.black.withValues(alpha: 0.46),
                    borderRadius: BorderRadius.circular(20),
                  ),
                  child: Text(
                    label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 12,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
            ],
          ),
        ),
      ],
    ),
  );
}

class PracticeRoleFallback extends StatelessWidget {
  const PracticeRoleFallback({
    required this.semanticLabel,
    required this.assetName,
    this.alignment = Alignment.center,
    this.imageKey,
    super.key,
  });

  final String semanticLabel;
  final String assetName;
  final Alignment alignment;
  final Key? imageKey;

  @override
  Widget build(BuildContext context) => Semantics(
    label: semanticLabel,
    image: true,
    child: Image.asset(
      assetName,
      key: imageKey,
      fit: BoxFit.cover,
      alignment: alignment,
    ),
  );
}
