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
    super.key,
  });

  static const height = 74.0;
  static const minimumBottomInset = 12.0;

  final List<GlassNavigationDestination> destinations;
  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;

  @override
  Widget build(BuildContext context) {
    final highContrast = MediaQuery.highContrastOf(context);
    final reduceMotion = MediaQuery.disableAnimationsOf(context);

    return SafeArea(
      minimum: const EdgeInsets.fromLTRB(16, 0, 16, minimumBottomInset),
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(32),
          boxShadow: const [
            BoxShadow(
              color: Color(0x24513E70),
              blurRadius: 38,
              offset: Offset(0, 14),
            ),
          ],
        ),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(32),
          child: BackdropFilter(
            filter: ui.ImageFilter.blur(sigmaX: 24, sigmaY: 24),
            child: Container(
              key: const Key('primary-navigation'),
              height: height,
              padding: const EdgeInsets.all(6),
              decoration: BoxDecoration(
                color: highContrast ? const Color(0xFFF8F8FB) : null,
                gradient: highContrast
                    ? null
                    : const LinearGradient(
                        begin: Alignment.topLeft,
                        end: Alignment.bottomRight,
                        colors: [Color(0xD9FFFFFF), Color(0x99FFFFFF)],
                      ),
                borderRadius: BorderRadius.circular(32),
                border: Border.all(
                  color: highContrast
                      ? const Color(0xFFD8DAE2)
                      : const Color(0xD9FFFFFF),
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
            ),
          ),
        ),
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
            borderRadius: BorderRadius.circular(26),
            onTap: onTap,
            child: AnimatedContainer(
              duration: reduceMotion
                  ? Duration.zero
                  : const Duration(milliseconds: 180),
              curve: Curves.easeOutCubic,
              decoration: BoxDecoration(
                color: selected ? const Color(0xB8E7E8F2) : Colors.transparent,
                borderRadius: BorderRadius.circular(26),
                border: selected
                    ? Border.all(color: const Color(0xCFFFFFFF))
                    : null,
              ),
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(
                    destination.icon,
                    size: 23,
                    color: selected
                        ? const Color(0xFF111217)
                        : const Color(0xFF686A72),
                  ),
                  const SizedBox(height: 3),
                  Text(
                    destination.label,
                    maxLines: 1,
                    style: TextStyle(
                      color: selected
                          ? const Color(0xFF111217)
                          : const Color(0xFF686A72),
                      fontSize: 11,
                      fontWeight: selected ? FontWeight.w700 : FontWeight.w600,
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
