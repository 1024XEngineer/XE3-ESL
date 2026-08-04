import 'dart:typed_data';

import 'package:flutter/widgets.dart';

import 'avatar_models.dart';

/// The only vendor-neutral boundary used by practice UI and orchestration.
///
/// A renderer owns visual playback. Callers must not start a second audio
/// player after [sendPcm] succeeds.
abstract interface class AvatarRenderer {
  AvatarRendererState get state;

  Stream<AvatarRendererState> get states;

  Future<void> prepare(AvatarSessionGrant grant);

  Widget buildSurface({Key? key});

  Future<void> sendPcm(Uint8List pcmBytes, {required bool end});

  Future<void> interrupt();

  Future<void> pauseRendering();

  Future<void> resumeRendering();

  Future<void> close();
}
