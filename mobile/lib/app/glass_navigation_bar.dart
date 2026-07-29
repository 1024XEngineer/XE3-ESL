import 'dart:ui' as ui;

import 'package:flutter/material.dart';

class GlassNavigationDestination {
  const GlassNavigationDestination({
    required this.label,
    required this.icon,
    required this.key,
  });

  final String label;
  final IconData icon;
  final Key key;
}

class GlassNavigationBar extends StatelessWidget {
  const GlassNavigationBar({
    required this.destinations,
    required this.selectedIndex,
    required this.onDestinationSelected,
    this.solid = false,
    super.key,
  });

  static const height = 64.0;
  static const minimumBottomInset = 10.0;
  static const _maximumLabelScale = 1.5;

  final List<GlassNavigationDestination> destinations;
  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;
  final bool solid;

  static double heightFor(BuildContext context) {
    final labelScale = _labelScaleFor(context);
    return height + ((labelScale - 1) * 14);
  }

  static double _labelScaleFor(BuildContext context) {
    return MediaQuery.textScalerOf(
      context,
    ).scale(1).clamp(1.0, _maximumLabelScale).toDouble();
  }

  @override
  Widget build(BuildContext context) {
    final highContrast = MediaQuery.highContrastOf(context);
    final reduceMotion = MediaQuery.disableAnimationsOf(context);
    final navigationHeight = heightFor(context);
    final horizontalInset = MediaQuery.sizeOf(context).width >= 390
        ? 20.0
        : 16.0;

    final navigation = Container(
      key: const Key('primary-navigation'),
      height: navigationHeight,
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(
        color: solid || highContrast
            ? const Color(0xFFF8F8F6)
            : const Color(0xDEFFFFFF),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(
          color: solid || highContrast
              ? const Color(0xFFD8DAE2)
              : const Color(0xBFFFFFFF),
        ),
      ),
      child: Semantics(
        container: true,
        label: '主导航',
        child: Row(
          children: [
            for (var index = 0; index < destinations.length; index++)
              Expanded(
                child: _NavigationItem(
                  destination: destinations[index],
                  selected: selectedIndex == index,
                  reduceMotion: reduceMotion,
                  onTap: () => onDestinationSelected(index),
                ),
              ),
          ],
        ),
      ),
    );
    final clippedNavigation = ClipRRect(
      borderRadius: BorderRadius.circular(24),
      child: solid
          ? navigation
          : BackdropFilter(
              filter: ui.ImageFilter.blur(sigmaX: 18, sigmaY: 18),
              child: navigation,
            ),
    );

    return SafeArea(
      minimum: EdgeInsets.fromLTRB(
        horizontalInset,
        0,
        horizontalInset,
        minimumBottomInset,
      ),
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(24),
          boxShadow: const [
            BoxShadow(
              color: Color(0x12000000),
              blurRadius: 18,
              offset: Offset(0, 6),
            ),
          ],
        ),
        child: clippedNavigation,
      ),
    );
  }
}

class _NavigationItem extends StatelessWidget {
  const _NavigationItem({
    required this.destination,
    required this.selected,
    required this.reduceMotion,
    required this.onTap,
  });

  final GlassNavigationDestination destination;
  final bool selected;
  final bool reduceMotion;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final labelScale = GlassNavigationBar._labelScaleFor(context);

    return Semantics(
      key: destination.key,
      button: true,
      selected: selected,
      label: destination.label,
      onTap: onTap,
      child: ExcludeSemantics(
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            borderRadius: BorderRadius.circular(20),
            onTap: onTap,
            child: AnimatedContainer(
              duration: reduceMotion
                  ? Duration.zero
                  : const Duration(milliseconds: 180),
              curve: Curves.easeOutCubic,
              decoration: BoxDecoration(
                color: selected ? const Color(0xD9E8E8E5) : Colors.transparent,
                borderRadius: BorderRadius.circular(20),
                border: selected
                    ? Border.all(color: const Color(0xCFFFFFFF))
                    : null,
              ),
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(
                    destination.icon,
                    size: 21,
                    color: selected
                        ? const Color(0xFF111217)
                        : const Color(0xFF686A72),
                  ),
                  const SizedBox(height: 2),
                  FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text(
                      destination.label,
                      maxLines: 1,
                      textScaler: TextScaler.linear(labelScale),
                      style: TextStyle(
                        color: selected
                            ? const Color(0xFF111217)
                            : const Color(0xFF686A72),
                        fontSize: 11,
                        fontWeight: selected
                            ? FontWeight.w600
                            : FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
