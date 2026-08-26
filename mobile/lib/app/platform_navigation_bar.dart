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
  static const iosVerticalOffset = 12.0;

  final List<PlatformNavigationDestination> destinations;
  final int selectedIndex;
  final Future<int> Function(int) onDestinationSelected;

  static double heightFor(BuildContext context) =>
      defaultTargetPlatform == TargetPlatform.iOS && !kIsWeb
      ? contentHeight - iosVerticalOffset
      : contentHeight;

  @override
  Widget build(BuildContext context) {
    if (defaultTargetPlatform == TargetPlatform.iOS && !kIsWeb) {
      return SizedBox(
        key: const Key('primary-navigation'),
        height: contentHeight + MediaQuery.viewPaddingOf(context).bottom,
        child: Padding(
          padding: const EdgeInsets.only(top: iosVerticalOffset),
          child: _NativeIosTabBar(
            destinations: destinations,
            selectedIndex: selectedIndex,
            onDestinationSelected: onDestinationSelected,
          ),
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
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          final selected = states.contains(WidgetState.selected);
          return SpeakUpDesign.label.copyWith(
            color: selected ? SpeakUpDesign.ink : SpeakUpDesign.secondary,
            fontWeight: selected ? FontWeight.w700 : FontWeight.w600,
          );
        }),
        destinations: [
          for (final destination in destinations)
            NavigationDestination(
              key: destination.key,
              icon: Icon(destination.icon, color: SpeakUpDesign.secondary),
              selectedIcon: Icon(
                destination.selectedIcon,
                color: SpeakUpDesign.ink,
              ),
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
