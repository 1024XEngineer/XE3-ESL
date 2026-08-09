import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

/// Responsive shell shared by avatar-led practice experiences.
///
/// The avatar occupies 34% of the available height in portrait and the
/// leading 44% of the available width in landscape.
class PracticeStageLayout extends StatelessWidget {
  const PracticeStageLayout({
    required this.avatar,
    required this.content,
    this.avatarRegionKey,
    super.key,
  });

  final Widget avatar;
  final Widget content;
  final Key? avatarRegionKey;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final landscape = constraints.maxWidth > constraints.maxHeight;
        if (landscape) {
          return Row(
            children: [
              SizedBox(
                key: avatarRegionKey,
                width: constraints.maxWidth * 0.44,
                child: avatar,
              ),
              Expanded(child: content),
            ],
          );
        }
        return Column(
          children: [
            SizedBox(
              key: avatarRegionKey,
              height: constraints.maxHeight * 0.34,
              child: avatar,
            ),
            Expanded(child: content),
          ],
        );
      },
    );
  }
}

/// Shared visual surface for an avatar-led practice experience.
class PracticeAvatarStage extends StatelessWidget {
  const PracticeAvatarStage({
    required this.title,
    required this.fallback,
    required this.onExit,
    this.surfaceBuilder,
    this.statusLabel,
    this.exitInFlight = false,
    this.exitButtonKey,
    this.statusKey,
    super.key,
  });

  final String title;
  final Widget fallback;
  final VoidCallback onExit;
  final WidgetBuilder? surfaceBuilder;
  final String? statusLabel;
  final bool exitInFlight;
  final Key? exitButtonKey;
  final Key? statusKey;

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: const Color(0xFFE5E9E5),
      child: Stack(
        fit: StackFit.expand,
        children: [
          if (surfaceBuilder case final builder?)
            builder(context)
          else
            fallback,
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
                      shadows: [
                        Shadow(color: Color(0x66000000), blurRadius: 8),
                      ],
                    ),
                  ),
                ),
                if (statusLabel case final label?)
                  Flexible(
                    child: Semantics(
                      liveRegion: true,
                      child: Container(
                        key: statusKey,
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
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
