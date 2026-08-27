import 'dart:async';

import 'package:flutter/widgets.dart';

void schedulePracticeConversationScrollToBottom({
  required ScrollController controller,
  required bool Function() isMounted,
  bool animated = true,
  Duration duration = const Duration(milliseconds: 220),
  Curve curve = Curves.easeOutCubic,
}) {
  WidgetsBinding.instance.addPostFrameCallback((_) {
    if (!isMounted() || !controller.hasClients) {
      return;
    }
    final position = controller.position;
    if (!position.hasContentDimensions) {
      return;
    }
    final target = position.maxScrollExtent;
    if (!animated) {
      controller.jumpTo(target);
      return;
    }
    unawaited(controller.animateTo(target, duration: duration, curve: curve));
  });
}
