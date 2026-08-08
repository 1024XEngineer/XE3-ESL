import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/platform_navigation_bar.dart';

void main() {
  testWidgets('iOS leaves touch handling to the native tab bar', (
    tester,
  ) async {
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
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            bottomNavigationBar: PlatformNavigationBar(
              destinations: destinations,
              selectedIndex: 0,
              onDestinationSelected: (index) async => index,
            ),
          ),
        ),
      );

      expect(find.byType(UiKitView), findsOneWidget);
      expect(find.byType(GestureDetector), findsNothing);
      final platformView = tester.widget<UiKitView>(find.byType(UiKitView));
      expect(platformView.viewType, 'speakup/native_tab_bar');
      expect(platformView.creationParams, <String, Object>{
        'selectedIndex': 0,
        'items': <Map<String, String>>[
          for (final destination in destinations)
            <String, String>{
              'label': destination.label,
              'systemImage': destination.iosSystemImage,
              'selectedSystemImage': destination.iosSelectedSystemImage,
            },
        ],
      });
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}
