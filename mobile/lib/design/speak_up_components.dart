import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

class SpeakUpWordmark extends StatelessWidget {
  const SpeakUpWordmark({this.height = 28, super.key});

  final double height;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'Speak Up',
      image: true,
      child: ExcludeSemantics(
        child: Image.asset(
          'assets/images/brand/speak-up-wordmark-black.png',
          height: height,
          fit: BoxFit.contain,
        ),
      ),
    );
  }
}

class SpeakUpContentWidth extends StatelessWidget {
  const SpeakUpContentWidth({required this.child, super.key});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(
          maxWidth: SpeakUpDesign.maxContentWidth,
        ),
        child: child,
      ),
    );
  }
}

class SpeakUpPage extends StatelessWidget {
  const SpeakUpPage({
    required this.child,
    this.hasPrimaryNavigation = false,
    this.padding,
    this.scrollable = true,
    this.controller,
    super.key,
  });

  final Widget child;
  final bool hasPrimaryNavigation;
  final EdgeInsetsGeometry? padding;
  final bool scrollable;
  final ScrollController? controller;

  @override
  Widget build(BuildContext context) {
    final content = Padding(
      padding:
          padding ??
          SpeakUpDesign.pagePadding(
            context,
            hasPrimaryNavigation: hasPrimaryNavigation,
          ),
      child: child,
    );
    return SafeArea(
      bottom: !hasPrimaryNavigation,
      child: SpeakUpContentWidth(
        child: scrollable
            ? SingleChildScrollView(
                controller: controller,
                keyboardDismissBehavior:
                    ScrollViewKeyboardDismissBehavior.onDrag,
                child: content,
              )
            : content,
      ),
    );
  }
}

class SpeakUpPageHeader extends StatelessWidget {
  const SpeakUpPageHeader({
    required this.title,
    this.subtitle,
    this.leading,
    this.trailing,
    super.key,
  });

  final String title;
  final String? subtitle;
  final Widget? leading;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      header: true,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (leading != null) ...[
            leading!,
            const SizedBox(width: SpeakUpDesign.space8),
          ],
          Expanded(
            child: Padding(
              padding: EdgeInsets.only(top: leading == null ? 0 : 5),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: Theme.of(context).textTheme.headlineLarge),
                  if (subtitle != null) ...[
                    const SizedBox(height: SpeakUpDesign.space4),
                    Text(
                      subtitle!,
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                  ],
                ],
              ),
            ),
          ),
          if (trailing != null) ...[
            const SizedBox(width: SpeakUpDesign.space12),
            trailing!,
          ],
        ],
      ),
    );
  }
}

class SpeakUpSectionHeader extends StatelessWidget {
  const SpeakUpSectionHeader({
    required this.title,
    this.subtitle,
    this.action,
    super.key,
  });

  final String title;
  final String? subtitle;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      header: true,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleLarge),
                if (subtitle != null) ...[
                  const SizedBox(height: SpeakUpDesign.space4),
                  Text(
                    subtitle!,
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                ],
              ],
            ),
          ),
          if (action != null) ...[
            const SizedBox(width: SpeakUpDesign.space12),
            action!,
          ],
        ],
      ),
    );
  }
}

class SpeakUpBackButton extends StatelessWidget {
  const SpeakUpBackButton({this.onPressed, super.key});

  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return IconButton(
      tooltip: '返回',
      constraints: const BoxConstraints.tightFor(
        width: SpeakUpDesign.minTapTarget,
        height: SpeakUpDesign.minTapTarget,
      ),
      onPressed: onPressed ?? () => Navigator.of(context).maybePop(),
      icon: const Icon(Icons.arrow_back_rounded),
    );
  }
}

class SpeakUpNavigationHeader extends StatelessWidget {
  const SpeakUpNavigationHeader({
    required this.title,
    this.onBack,
    this.backButtonKey,
    this.titleKey,
    this.trailing,
    super.key,
  });

  final String title;
  final VoidCallback? onBack;
  final Key? backButtonKey;
  final Key? titleKey;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        SpeakUpBackButton(key: backButtonKey, onPressed: onBack),
        const SizedBox(width: SpeakUpDesign.space8),
        Expanded(
          child: Semantics(
            header: true,
            child: Text(
              title,
              key: titleKey,
              style: Theme.of(context).textTheme.titleLarge,
            ),
          ),
        ),
        if (trailing != null) ...[
          const SizedBox(width: SpeakUpDesign.space12),
          trailing!,
        ],
      ],
    );
  }
}

class SpeakUpTaskCard extends StatelessWidget {
  const SpeakUpTaskCard({
    required this.title,
    required this.onTap,
    this.subtitle,
    this.media,
    this.meta,
    this.trailing,
    this.semanticLabel,
    super.key,
  });

  final String title;
  final String? subtitle;
  final Widget? media;
  final Widget? meta;
  final Widget? trailing;
  final String? semanticLabel;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: onTap != null,
      label: semanticLabel,
      child: Material(
        color: SpeakUpDesign.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
          side: const BorderSide(color: SpeakUpDesign.border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ?media,
              Padding(
                padding: const EdgeInsets.all(SpeakUpDesign.space16),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            title,
                            style: Theme.of(context).textTheme.titleMedium,
                          ),
                          if (subtitle != null) ...[
                            const SizedBox(height: SpeakUpDesign.space4),
                            Text(
                              subtitle!,
                              style: Theme.of(context).textTheme.bodyMedium,
                            ),
                          ],
                          if (meta != null) ...[
                            const SizedBox(height: SpeakUpDesign.space12),
                            meta!,
                          ],
                        ],
                      ),
                    ),
                    if (trailing != null) ...[
                      const SizedBox(width: SpeakUpDesign.space12),
                      trailing!,
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class SpeakUpStepRow extends StatelessWidget {
  const SpeakUpStepRow({
    required this.index,
    required this.title,
    this.subtitle,
    this.completed = false,
    this.selected = false,
    this.onTap,
    super.key,
  });

  final int index;
  final String title;
  final String? subtitle;
  final bool completed;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: onTap != null,
      selected: selected,
      label: '第 $index 步，$title',
      child: ListTile(
        onTap: onTap,
        minTileHeight: SpeakUpDesign.minTapTarget,
        contentPadding: EdgeInsets.zero,
        leading: CircleAvatar(
          radius: 18,
          backgroundColor: selected
              ? SpeakUpDesign.primary
              : SpeakUpDesign.surfaceMuted,
          foregroundColor: selected ? Colors.white : SpeakUpDesign.secondary,
          child: completed
              ? const Icon(Icons.check_rounded, size: 18)
              : Text('$index', style: SpeakUpDesign.label),
        ),
        title: Text(title, style: Theme.of(context).textTheme.titleMedium),
        subtitle: subtitle == null
            ? null
            : Text(subtitle!, style: Theme.of(context).textTheme.bodyMedium),
        trailing: onTap == null
            ? null
            : const Icon(
                Icons.chevron_right_rounded,
                color: SpeakUpDesign.secondary,
              ),
      ),
    );
  }
}

class SpeakUpEmptyState extends StatelessWidget {
  const SpeakUpEmptyState({
    required this.title,
    required this.message,
    this.action,
    this.icon = Icons.chat_bubble_outline_rounded,
    super.key,
  });

  final String title;
  final String message;
  final Widget? action;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      liveRegion: true,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: SpeakUpDesign.space32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 28, color: SpeakUpDesign.secondary),
            const SizedBox(height: SpeakUpDesign.space16),
            Text(
              title,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            Text(
              message,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (action != null) ...[
              const SizedBox(height: SpeakUpDesign.space20),
              action!,
            ],
          ],
        ),
      ),
    );
  }
}
