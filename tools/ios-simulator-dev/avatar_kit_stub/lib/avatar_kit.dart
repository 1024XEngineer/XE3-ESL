import 'dart:typed_data';

import 'package:flutter/widgets.dart';

class Configuration {
  const Configuration({
    required this.environment,
    this.audioFormat = const AudioFormat(),
    this.drivingServiceMode = DrivingServiceMode.sdk,
    this.logLevel = LogLevel.off,
  });

  final Environment environment;
  final AudioFormat audioFormat;
  final DrivingServiceMode drivingServiceMode;
  final LogLevel logLevel;
}

enum Environment { intl, cn }

class AudioFormat {
  const AudioFormat({this.sampleRate = 16000});

  final int sampleRate;
}

enum DrivingServiceMode { sdk, host }

enum LogLevel { off, error, warning, all }

enum ConnectionState { disconnected, connecting, connected, failed }

enum ConversationState { idle, paused, playing }

enum AvatarError {
  appIDUnrecognized,
  avatarIDUnrecognized,
  avatarAssetMissing,
  sessionTokenInvalid,
  sessionTokenExpired,
  failedToFetchAvatarMetadata,
  failedToDownloadAvatarAssets,
  insufficientBalance,
  sessionTimeout,
  concurrentLimitExceeded,
  serverError,
}

class FrameRateInfo {
  const FrameRateInfo();
}

abstract final class AvatarSDK {
  static Future<bool> isDeviceSupported() async => false;

  static Future<void> initialize({
    required String appID,
    required Configuration configuration,
  }) => Future<void>.error(_unsupported());

  static Future<void> setSessionToken(String sessionToken) =>
      Future<void>.error(_unsupported());
}

class AvatarManager {
  AvatarManager._();

  static final AvatarManager shared = AvatarManager._();

  Future<Avatar> load({required String id}) =>
      Future<Avatar>.error(_unsupported());

  Future<void> cancelLoading({required String id}) async {}
}

class Avatar {
  const Avatar();
}

class AvatarWidget extends StatelessWidget {
  const AvatarWidget({
    super.key,
    required this.avatar,
    required this.onPlatformViewCreated,
  });

  final Avatar avatar;
  final ValueChanged<AvatarController> onPlatformViewCreated;

  @override
  Widget build(BuildContext context) => const SizedBox.expand();
}

class AvatarController {
  AvatarController._();

  VoidCallback? onFirstRendering;
  void Function(ConnectionState state, String? errorMessage)? onConnectionState;
  ValueChanged<ConversationState>? onConversationState;
  ValueChanged<AvatarError>? onError;
  ValueChanged<FrameRateInfo>? onFrameRateInfo;

  Future<void> start() => Future<void>.error(_unsupported());

  Future<String> send(Uint8List audioData, {bool end = false}) =>
      Future<String>.error(_unsupported());

  Future<void> interrupt() async {}

  Future<void> close() async {}

  Future<void> pauseRendering() async {}

  Future<void> resumeRendering() async {}
}

UnsupportedError _unsupported() =>
    UnsupportedError('AvatarKit is disabled on the iOS Simulator.');
