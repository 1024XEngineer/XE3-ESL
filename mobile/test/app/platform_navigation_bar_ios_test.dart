import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/platform_navigation_bar.dart';

void main() {
  testWidgets('iOS native tab bar exposes destination keys and forwards taps',
      (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;

    final destinations = <PlatformNavigationDestination>[
      PlatformNavigationDestination(
        label: 'SpeakUp',
        icon: Icons.chat_bubble_outline_rounded,
        selectedIcon: Icons.chat_bubble_rounded,
        iosSystemImage: 'bubble.left',
        iosSelectedSystemImage: 'bubble.left.fill',
        key: const Key('primary-tab-agent'),
      ),
      PlatformNavigationDestination(
        label: '训练',
        icon: Icons.grid_view_rounded,
        selectedIcon: Icons.dashboard_rounded,
        iosSystemImage: 'square.grid.2x2',
        iosSelectedSystemImage: 'square.grid.2x2.fill',
        key: const Key('primary-tab-scenes'),
      ),
      PlatformNavigationDestination(
        label: '复盘',
        icon: Icons.fact_check_outlined,
        selectedIcon: Icons.fact_check_rounded,
        iosSystemImage: 'checklist',
        iosSelectedSystemImage: 'checkmark.square.fill',
        key: const Key('primary-tab-review'),
      ),
      PlatformNavigationDestination(
        label: '我的',
        icon: Icons.person_outline_rounded,
        selectedIcon: Icons.person_rounded,
        iosSystemImage: 'person',
        iosSelectedSystemImage: 'person.fill',
        key: const Key('primary-tab-profile'),
      ),
    ];

    try {
      final selectedIndexes = <int>[];
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            bottomNavigationBar: PlatformNavigationBar(
              destinations: destinations,
              selectedIndex: 0,
              onDestinationSelected: (index) async {
                selectedIndexes.add(index);
                return index;
              },
            ),
          ),
        ),
      );

      expect(find.byKey(const Key('primary-tab-scenes')), findsOneWidget);
      expect(find.byKey(const Key('primary-tab-profile')), findsOneWidget);

      await tester.tap(find.byKey(const Key('primary-tab-scenes')));
      await tester.pump();
      expect(selectedIndexes, [1]);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}
