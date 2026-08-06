import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';

class PreparationCatalogAvatarPreview extends StatelessWidget {
  const PreparationCatalogAvatarPreview({required this.size, super.key});

  final double size;

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: SizedBox.square(
        dimension: size,
        child: DecoratedBox(
          decoration: const BoxDecoration(
            color: PreparationDesign.scenarioTint,
            shape: BoxShape.circle,
          ),
          child: Icon(
            Icons.record_voice_over_outlined,
            size: size * 0.46,
            color: PreparationDesign.scenario,
          ),
        ),
      ),
    );
  }
}

class PreparationCatalogHeader extends StatelessWidget {
  const PreparationCatalogHeader({
    required this.title,
    required this.description,
    required this.titleKey,
    super.key,
  });

  final String title;
  final String description;
  final Key titleKey;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Semantics(
          header: true,
          container: true,
          child: Text(title, key: titleKey, style: PreparationDesign.pageTitle),
        ),
        const SizedBox(height: 8),
        Text(description, style: PreparationDesign.body),
      ],
    );
  }
}

class PreparationFeaturedScene extends StatelessWidget {
  const PreparationFeaturedScene({
    required this.eyebrow,
    required this.title,
    required this.description,
    required this.actionLabel,
    required this.icon,
    required this.color,
    required this.foregroundColor,
    required this.assetPath,
    required this.onPressed,
    this.actionKey,
    super.key,
  });

  final String eyebrow;
  final String title;
  final String description;
  final String actionLabel;
  final IconData icon;
  final Color color;
  final Color foregroundColor;
  final String assetPath;
  final VoidCallback? onPressed;
  final Key? actionKey;

  @override
  Widget build(BuildContext context) {
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    final cardHeight = 184 + (textScale - 1).clamp(0, 2).toDouble() * 190;
    return Material(
      color: color,
      clipBehavior: Clip.antiAlias,
      borderRadius: BorderRadius.circular(PreparationDesign.radiusHero),
      child: Semantics(
        button: onPressed != null,
        enabled: onPressed != null,
        label: '$title。$description$actionLabel',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          key: actionKey,
          onTap: onPressed,
          child: SizedBox(
            height: cardHeight,
            child: Stack(
              fit: StackFit.expand,
              children: [
                Image.asset(
                  assetPath,
                  fit: BoxFit.cover,
                  alignment: Alignment.topCenter,
                  errorBuilder: (_, _, _) => ColoredBox(color: color),
                ),
                ColoredBox(color: color.withValues(alpha: 0.64)),
                Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      Row(
                        children: [
                          Container(
                            width: 32,
                            height: 32,
                            decoration: BoxDecoration(
                              color: Colors.white.withValues(alpha: 0.18),
                              shape: BoxShape.circle,
                            ),
                            child: Icon(icon, color: foregroundColor, size: 19),
                          ),
                          const SizedBox(width: 10),
                          Text(
                            eyebrow,
                            style: PreparationDesign.label.copyWith(
                              color: foregroundColor.withValues(alpha: 0.86),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 12),
                      Text(
                        title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: PreparationDesign.sectionTitle.copyWith(
                          color: foregroundColor,
                          fontSize: 22,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: PreparationDesign.label.copyWith(
                          color: foregroundColor.withValues(alpha: 0.86),
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                      const SizedBox(height: 12),
                      Align(
                        alignment: Alignment.centerLeft,
                        child: Container(
                          constraints: const BoxConstraints(minHeight: 36),
                          padding: const EdgeInsets.symmetric(
                            horizontal: 14,
                            vertical: 8,
                          ),
                          decoration: BoxDecoration(
                            color: Colors.white.withValues(alpha: 0.92),
                            borderRadius: BorderRadius.circular(999),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Text(
                                actionLabel,
                                style: PreparationDesign.label.copyWith(
                                  color: PreparationDesign.ink,
                                ),
                              ),
                              const SizedBox(width: 6),
                              const Icon(
                                Icons.arrow_forward_rounded,
                                color: PreparationDesign.ink,
                                size: 17,
                              ),
                            ],
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class PreparationCatalogEmpty extends StatelessWidget {
  const PreparationCatalogEmpty({required this.message, super.key});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Center(
        child: Text(
          message,
          textAlign: TextAlign.center,
          style: const TextStyle(color: PreparationDesign.inkSecondary),
        ),
      ),
    );
  }
}

class PreparationInlineFailure extends StatelessWidget {
  const PreparationInlineFailure({
    required this.message,
    required this.retryKey,
    this.onRetry,
    super.key,
  });

  final String message;
  final Key retryKey;
  final Future<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: PreparationDesign.errorMuted,
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
        child: Row(
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 20,
              color: PreparationDesign.error,
            ),
            const SizedBox(width: 8),
            Expanded(child: Text(message)),
            if (onRetry case final callback?)
              TextButton(
                key: retryKey,
                onPressed: callback,
                child: const Text('重试'),
              ),
          ],
        ),
      ),
    );
  }
}
