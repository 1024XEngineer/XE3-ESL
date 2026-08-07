import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/design/speak_up_design.dart';

class PlatformNavigationDestination {
  const PlatformNavigationDestination({
    required this.label,
    required this.icon,
    required this.selectedIcon,
    required this.iosSystemImage,
    required this.iosSelectedSystemImage,
    required this.key,
  });

  final String label;
  final IconData icon;
  final IconData selectedIcon;
  final String iosSystemImage;
  final String iosSelectedSystemImage;
  final Key key;
}

class PlatformNavigationBar extends StatelessWidget {
  const PlatformNavigationBar({
    required this.destinations,
    required this.selectedIndex,
    required this.onDestinationSelected,
    super.key,
  });

  static const contentHeight = 64.0;
  static const minimumBottomInset = 10.0;

  final List<PlatformNavigationDestination> destinations;
  final int selectedIndex;
  final Future<int> Function(int) onDestinationSelected;

  static double heightFor(BuildContext context) => contentHeight;

  @override
  Widget build(BuildContext context) {
    if (defaultTargetPlatform == TargetPlatform.iOS && !kIsWeb) {
      return SizedBox(
        key: const Key('primary-navigation'),
        height: contentHeight + MediaQuery.viewPaddingOf(context).bottom,
        child: Stack(
          children: [
            Positioned.fill(
              child: _NativeIosTabBar(
                destinations: destinations,
                selectedIndex: selectedIndex,
                onDestinationSelected: onDestinationSelected,
              ),
            ),
            // A native UITabBar platform view is opaque to the Flutter widget
            // tree, so widget tests and integration tests cannot locate or tap
            // its items. Overlay a transparent Flutter hit target per item,
            // reusing each destination's key and forwarding to the same
            // selection callback the native view would invoke.
            Positioned.fill(
              child: Row(
                children: [
                  for (final (index, destination) in destinations.indexed)
                    Expanded(
                      child: GestureDetector(
                        key: destination.key,
                        behavior: HitTestBehavior.opaque,
                        onTap: () => unawaited(
                          onDestinationSelected(index),
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

    return SafeArea(
      minimum: const EdgeInsets.only(bottom: minimumBottomInset),
      child: NavigationBar(
        key: const Key('primary-navigation'),
        height: contentHeight,
        selectedIndex: selectedIndex,
        onDestinationSelected: (index) {
          unawaited(onDestinationSelected(index));
        },
        backgroundColor: SpeakUpDesign.surface,
        indicatorColor: SpeakUpDesign.primaryMuted,
        elevation: 0,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        destinations: [
          for (final destination in destinations)
            NavigationDestination(
              key: destination.key,
              icon: Icon(destination.icon),
              selectedIcon: Icon(destination.selectedIcon),
              label: destination.label,
            ),
        ],
      ),
    );
  }
}

class _NativeIosTabBar extends StatefulWidget {
  const _NativeIosTabBar({
    required this.destinations,
    required this.selectedIndex,
    required this.onDestinationSelected,
  });

  final List<PlatformNavigationDestination> destinations;
  final int selectedIndex;
  final Future<int> Function(int) onDestinationSelected;

  @override
  State<_NativeIosTabBar> createState() => _NativeIosTabBarState();
}

class _NativeIosTabBarState extends State<_NativeIosTabBar> {
  MethodChannel? _channel;

  @override
  void didUpdateWidget(covariant _NativeIosTabBar oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.selectedIndex != widget.selectedIndex) {
      _channel?.invokeMethod<void>('setSelectedIndex', widget.selectedIndex);
    }
  }

  @override
  void dispose() {
    _channel?.setMethodCallHandler(null);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return UiKitView(
      viewType: 'speakup/native_tab_bar',
      creationParamsCodec: const StandardMessageCodec(),
      creationParams: <String, Object>{
        'selectedIndex': widget.selectedIndex,
        'items': [
          for (final destination in widget.destinations)
            <String, String>{
              'label': destination.label,
              'systemImage': destination.iosSystemImage,
              'selectedSystemImage': destination.iosSelectedSystemImage,
            },
        ],
      },
      onPlatformViewCreated: (viewId) {
        final channel = MethodChannel('speakup/native_tab_bar/$viewId');
        channel.setMethodCallHandler((call) async {
          if (call.method == 'onSelected') {
            return widget.onDestinationSelected(call.arguments as int);
          }
        });
        _channel = channel;
      },
    );
  }
}
