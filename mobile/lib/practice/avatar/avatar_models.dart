import 'dart:typed_data';

enum AvatarRegion {
  usWest('us-west'),
  apNortheast('ap-northeast'),
  cnBeijing('cn-beijing');

  const AvatarRegion(this.wireName);

  final String wireName;

  static AvatarRegion? fromWireName(String value) {
    for (final region in values) {
      if (region.wireName == value) {
        return region;
      }
    }
    return null;
  }
}

final class AvatarAudioFormat {
  const AvatarAudioFormat({
    required this.encoding,
    required this.sampleRateHz,
    required this.channels,
  });

  static const pcmS16le24kMono = AvatarAudioFormat(
    encoding: 'PCM_S16LE',
    sampleRateHz: 24000,
    channels: 1,
  );

  final String encoding;
  final int sampleRateHz;
  final int channels;

  bool get isSupported =>
      encoding == pcmS16le24kMono.encoding &&
      sampleRateHz == pcmS16le24kMono.sampleRateHz &&
      channels == pcmS16le24kMono.channels;

  int get bytesPerSecond => sampleRateHz * channels * 2;
}

/// A short-lived, server-minted capability for one avatar session.
///
/// [toString] deliberately excludes every credential-shaped value.
final class AvatarSessionGrant {
  const AvatarSessionGrant({
    required this.appId,
    required this.avatarId,
    required this.sessionToken,
    required this.region,
    required this.expiresAt,
    required this.audioFormat,
  });

  final String appId;
  final String avatarId;
  final String sessionToken;
  final AvatarRegion region;
  final DateTime expiresAt;
  final AvatarAudioFormat audioFormat;

  @override
  String toString() =>
      'AvatarSessionGrant(region: ${region.wireName}, '
      'expiresAt: $expiresAt, audioFormat: ${audioFormat.encoding}/'
      '${audioFormat.sampleRateHz}/${audioFormat.channels})';
}

enum AvatarRendererConnection {
  idle,
  preparing,
  surfaceReady,
  connecting,
  connected,
  failed,
  closed,
}

enum AvatarRendererConversation { idle, playing }

enum AvatarRendererFailure {
  unsupportedDevice,
  invalidConfiguration,
  authentication,
  insufficientBalance,
  sessionLimit,
  sessionExpired,
  network,
  rendering,
  unavailable,
}

final class AvatarRendererState {
  const AvatarRendererState({
    this.connection = AvatarRendererConnection.idle,
    this.conversation = AvatarRendererConversation.idle,
    this.failure,
  });

  final AvatarRendererConnection connection;
  final AvatarRendererConversation conversation;
  final AvatarRendererFailure? failure;

  bool get canAcceptAudio =>
      connection == AvatarRendererConnection.connected && failure == null;

  AvatarRendererState copyWith({
    AvatarRendererConnection? connection,
    AvatarRendererConversation? conversation,
    AvatarRendererFailure? failure,
    bool clearFailure = false,
  }) {
    return AvatarRendererState(
      connection: connection ?? this.connection,
      conversation: conversation ?? this.conversation,
      failure: clearFailure ? null : failure ?? this.failure,
    );
  }
}

final class AvatarRendererException implements Exception {
  const AvatarRendererException(this.failure);

  final AvatarRendererFailure failure;

  @override
  String toString() => 'Avatar rendering is unavailable (${failure.name}).';
}

enum AvatarControllerPhase {
  idle,
  preparing,
  ready,
  speaking,
  fallback,
  failed,
  closed,
}

final class AvatarControllerState {
  const AvatarControllerState({
    required this.phase,
    required this.renderer,
    this.failure,
  });

  const AvatarControllerState.idle()
    : phase = AvatarControllerPhase.idle,
      renderer = const AvatarRendererState(),
      failure = null;

  final AvatarControllerPhase phase;
  final AvatarRendererState renderer;
  final AvatarRendererFailure? failure;

  bool get canUseAvatar =>
      phase != AvatarControllerPhase.closed && renderer.canAcceptAudio;

  AvatarControllerState copyWith({
    AvatarControllerPhase? phase,
    AvatarRendererState? renderer,
    AvatarRendererFailure? failure,
    bool clearFailure = false,
  }) {
    return AvatarControllerState(
      phase: phase ?? this.phase,
      renderer: renderer ?? this.renderer,
      failure: clearFailure ? null : failure ?? this.failure,
    );
  }
}

enum AvatarSpeechResult { avatar, fallback, interrupted }

typedef AvatarFallbackPlayback = Future<void> Function(Uint8List wavBytes);
typedef AvatarFallbackStop = Future<void> Function();
typedef AvatarDelay = Future<void> Function(Duration duration);
