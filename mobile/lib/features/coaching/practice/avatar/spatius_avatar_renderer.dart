import 'dart:async';
import 'dart:typed_data';

import 'package:avatar_kit/avatar_kit.dart' as kit;
import 'package:flutter/widgets.dart';

import 'avatar_models.dart';
import 'avatar_renderer.dart';

/// The only file allowed to depend on the temporary Spatius/AvatarKit SDK.
///
/// Replacing the beta package later does not change practice UI, orchestration,
/// token transport, or audio parsing.
final class SpatiusAvatarRenderer implements AvatarRenderer {
  SpatiusAvatarRenderer()
    : _stateController = StreamController<AvatarRendererState>.broadcast(
        sync: true,
      );

  static const _cancelLoadingTimeout = Duration(seconds: 1);

  final StreamController<AvatarRendererState> _stateController;

  static int _nextTokenLease = 0;
  static int _activeTokenLease = 0;
  static Future<void> _tokenLeaseTail = Future<void>.value();
  static Future<void>? _sdkInitializationFuture;
  static String? _initializedAppId;
  static int? _initializedSampleRateHz;

  AvatarRendererState _state = const AvatarRendererState();
  kit.Avatar? _avatar;
  kit.AvatarController? _controller;
  String? _loadingAvatarId;
  bool _closed = false;
  bool _prepared = false;
  bool _startRequested = false;
  int? _tokenLease;
  Completer<void>? _tokenLeaseRelease;
  Future<void>? _sdkConfigurationFuture;
  Future<void>? _closeFuture;

  @override
  AvatarRendererState get state => _state;

  @override
  Stream<AvatarRendererState> get states => _stateController.stream;

  @override
  Future<void> prepare(AvatarSessionGrant grant) async {
    if (_closed || _prepared || !grant.audioFormat.isSupported) {
      throw const AvatarRendererException(
        AvatarRendererFailure.invalidConfiguration,
      );
    }
    _prepared = true;
    _setState(
      const AvatarRendererState(connection: AvatarRendererConnection.preparing),
    );
    try {
      if (!await kit.AvatarSDK.isDeviceSupported()) {
        throw const AvatarRendererException(
          AvatarRendererFailure.unsupportedDevice,
        );
      }
      final configuration = _configureSdk(grant);
      _sdkConfigurationFuture = configuration;
      try {
        await configuration;
      } finally {
        if (identical(_sdkConfigurationFuture, configuration)) {
          _sdkConfigurationFuture = null;
        }
      }
      if (_closed) {
        await _clearSdkTokenIfOwned();
        throw const AvatarRendererException(AvatarRendererFailure.unavailable);
      }
      final avatarId = grant.avatarId;
      _loadingAvatarId = avatarId;
      try {
        _avatar = await kit.AvatarManager.shared.load(
          id: avatarId,
          useCompressedModel: true,
        );
      } finally {
        if (_loadingAvatarId == avatarId) {
          _loadingAvatarId = null;
        }
      }
      if (_closed) {
        await _clearSdkTokenIfOwned();
        throw const AvatarRendererException(AvatarRendererFailure.unavailable);
      }
      _setState(
        const AvatarRendererState(
          connection: AvatarRendererConnection.surfaceReady,
        ),
      );
    } on AvatarRendererException catch (error) {
      await _cleanupFailedPrepare();
      _fail(error.failure);
      rethrow;
    } on kit.AvatarError catch (error) {
      await _cleanupFailedPrepare();
      final failure = _mapError(error);
      _fail(failure);
      throw AvatarRendererException(failure);
    } catch (_) {
      await _cleanupFailedPrepare();
      _fail(AvatarRendererFailure.unavailable);
      throw const AvatarRendererException(AvatarRendererFailure.unavailable);
    }
  }

  @override
  Widget buildSurface({Key? key}) {
    final avatar = _avatar;
    if (_closed || avatar == null) {
      return SizedBox.expand(key: key);
    }
    return _AvatarSurfaceHost(
      key: key,
      avatar: avatar,
      onController: _attachController,
    );
  }

  void _attachController(kit.AvatarController controller) {
    if (_closed) {
      unawaited(controller.close().catchError((_) {}));
      return;
    }
    final previous = _controller;
    if (previous != null && !identical(previous, controller)) {
      unawaited(previous.close().catchError((_) {}));
    }
    if (!identical(previous, controller)) {
      _startRequested = false;
    }
    _controller = controller;
    controller.onFirstRendering = () {
      if (_closed || _startRequested || !identical(_controller, controller)) {
        return;
      }
      _setState(
        _state.copyWith(
          connection: AvatarRendererConnection.connecting,
          clearFailure: true,
        ),
      );
      _startOnce(controller);
    };
    controller.onConnectionState = (connection, errorMessage) {
      if (_closed || !identical(_controller, controller)) {
        return;
      }
      switch (connection) {
        case kit.ConnectionState.disconnected:
          _setState(
            _state.copyWith(connection: AvatarRendererConnection.surfaceReady),
          );
        case kit.ConnectionState.connecting:
          _setState(
            _state.copyWith(connection: AvatarRendererConnection.connecting),
          );
        case kit.ConnectionState.connected:
          _setState(
            _state.copyWith(
              connection: AvatarRendererConnection.connected,
              clearFailure: true,
            ),
          );
        case kit.ConnectionState.failed:
          _fail(_mapConnectionFailure(errorMessage));
      }
    };
    controller.onConversationState = (conversation) {
      if (_closed || !identical(_controller, controller)) {
        return;
      }
      final mapped = switch (conversation) {
        kit.ConversationState.playing => AvatarRendererConversation.playing,
        kit.ConversationState.idle ||
        kit.ConversationState.paused => AvatarRendererConversation.idle,
      };
      _setState(_state.copyWith(conversation: mapped));
    };
    controller.onError = (error) {
      if (!_closed && identical(_controller, controller)) {
        _fail(_mapError(error));
      }
    };
  }

  void _startOnce(kit.AvatarController controller) {
    if (_startRequested || _closed || !identical(_controller, controller)) {
      return;
    }
    _startRequested = true;
    unawaited(_start(controller));
  }

  Future<void> _start(kit.AvatarController controller) async {
    try {
      await controller.start();
    } catch (_) {
      if (!_closed && identical(_controller, controller)) {
        _fail(AvatarRendererFailure.network);
      }
    }
  }

  @override
  Future<void> sendPcm(Uint8List pcmBytes, {required bool end}) async {
    final controller = _controller;
    if (_closed ||
        controller == null ||
        !_state.canAcceptAudio ||
        pcmBytes.isEmpty ||
        pcmBytes.length.isOdd) {
      throw AvatarRendererException(
        _state.failure ?? AvatarRendererFailure.unavailable,
      );
    }
    try {
      await controller.send(pcmBytes, end: end);
    } catch (_) {
      _fail(AvatarRendererFailure.network);
      throw const AvatarRendererException(AvatarRendererFailure.network);
    }
  }

  @override
  Future<void> interrupt() async {
    final controller = _controller;
    if (_closed || controller == null) {
      return;
    }
    try {
      await controller.interrupt();
      if (!_closed) {
        _setState(
          _state.copyWith(conversation: AvatarRendererConversation.idle),
        );
      }
    } catch (_) {
      _fail(AvatarRendererFailure.rendering);
      throw const AvatarRendererException(AvatarRendererFailure.rendering);
    }
  }

  @override
  Future<void> pauseRendering() async {
    final controller = _controller;
    if (!_closed && controller != null) {
      await controller.pauseRendering();
    }
  }

  @override
  Future<void> resumeRendering() async {
    final controller = _controller;
    if (!_closed && controller != null) {
      await controller.resumeRendering();
    }
  }

  @override
  Future<void> close() {
    final existing = _closeFuture;
    if (existing != null) {
      return existing;
    }
    _closed = true;
    final completion = _performClose();
    _closeFuture = completion;
    return completion;
  }

  Future<void> _performClose() async {
    final configuration = _sdkConfigurationFuture;
    if (configuration != null) {
      try {
        await configuration;
      } catch (_) {
        // Failed/cancelled configuration releases its global SDK lease.
      }
    }
    final loadingAvatarId = _loadingAvatarId;
    _loadingAvatarId = null;
    if (loadingAvatarId != null) {
      await _cancelLoadingBestEffort(loadingAvatarId);
    }
    final controller = _controller;
    _controller = null;
    _startRequested = false;
    var nativeCloseFailed = false;
    if (controller != null) {
      controller.onFirstRendering = null;
      controller.onConnectionState = null;
      controller.onConversationState = null;
      controller.onError = null;
      controller.onFrameRateInfo = null;
      try {
        await controller.interrupt();
      } catch (_) {
        // Continue closing even if the native session is already gone.
      }
      try {
        await controller.close();
      } catch (_) {
        nativeCloseFailed = true;
      }
    }
    _avatar = null;
    await _clearSdkTokenIfOwned();
    _setState(
      const AvatarRendererState(connection: AvatarRendererConnection.closed),
    );
    await _stateController.close();
    if (nativeCloseFailed) {
      throw const AvatarRendererException(AvatarRendererFailure.rendering);
    }
  }

  Future<void> _configureSdk(AvatarSessionGrant grant) async {
    final previousLease = _tokenLeaseTail.catchError((_) {});
    final release = Completer<void>();
    _tokenLeaseTail = previousLease.then((_) => release.future);
    await previousLease;
    if (_closed) {
      release.complete();
      throw const AvatarRendererException(AvatarRendererFailure.unavailable);
    }

    final lease = ++_nextTokenLease;
    _activeTokenLease = lease;
    _tokenLease = lease;
    _tokenLeaseRelease = release;
    try {
      await _ensureSdkInitialized(grant);
      if (_closed || _activeTokenLease != lease) {
        throw const AvatarRendererException(AvatarRendererFailure.unavailable);
      }
      await kit.AvatarSDK.setSessionToken(grant.sessionToken);
      if (_closed || _activeTokenLease != lease) {
        throw const AvatarRendererException(AvatarRendererFailure.unavailable);
      }
    } catch (_) {
      await _clearSdkTokenIfOwned();
      rethrow;
    }
  }

  static Future<void> _ensureSdkInitialized(AvatarSessionGrant grant) async {
    final existing = _sdkInitializationFuture;
    if (existing != null) {
      await existing;
      if (_initializedAppId != grant.appId ||
          _initializedSampleRateHz != grant.audioFormat.sampleRateHz) {
        throw const AvatarRendererException(
          AvatarRendererFailure.invalidConfiguration,
        );
      }
      return;
    }

    _initializedAppId = grant.appId;
    _initializedSampleRateHz = grant.audioFormat.sampleRateHz;
    final initialization = kit.AvatarSDK.initialize(
      appID: grant.appId,
      configuration: kit.Configuration(
        environment: kit.Environment.intl,
        audioFormat: kit.AudioFormat(
          sampleRate: grant.audioFormat.sampleRateHz,
        ),
        drivingServiceMode: kit.DrivingServiceMode.sdk,
        logLevel: kit.LogLevel.off,
      ),
    );
    _sdkInitializationFuture = initialization;
    try {
      await initialization;
    } catch (_) {
      if (identical(_sdkInitializationFuture, initialization)) {
        _sdkInitializationFuture = null;
        _initializedAppId = null;
        _initializedSampleRateHz = null;
      }
      rethrow;
    }
  }

  Future<void> _cleanupFailedPrepare() async {
    final loadingAvatarId = _loadingAvatarId;
    _loadingAvatarId = null;
    if (loadingAvatarId != null) {
      await _cancelLoadingBestEffort(loadingAvatarId);
    }
    _avatar = null;
    await _clearSdkTokenIfOwned();
  }

  Future<void> _cancelLoadingBestEffort(String avatarId) async {
    try {
      await kit.AvatarManager.shared
          .cancelLoading(id: avatarId)
          .timeout(_cancelLoadingTimeout);
    } catch (_) {
      // beta SDKs may cancel natively without completing the Flutter call.
    }
  }

  Future<void> _clearSdkTokenIfOwned() async {
    final lease = _tokenLease;
    final release = _tokenLeaseRelease;
    _tokenLease = null;
    _tokenLeaseRelease = null;
    if (lease == null) {
      if (release != null && !release.isCompleted) {
        release.complete();
      }
      return;
    }
    try {
      if (_activeTokenLease == lease) {
        try {
          await kit.AvatarSDK.setSessionToken('');
        } catch (_) {
          // Some native versions reject an empty token after shutdown.
        }
        if (_activeTokenLease == lease) {
          _activeTokenLease = 0;
        }
      }
    } finally {
      if (release != null && !release.isCompleted) {
        release.complete();
      }
    }
  }

  void _fail(AvatarRendererFailure failure) {
    if (_closed) {
      return;
    }
    _setState(
      AvatarRendererState(
        connection: AvatarRendererConnection.failed,
        failure: failure,
      ),
    );
  }

  void _setState(AvatarRendererState next) {
    _state = next;
    if (!_stateController.isClosed) {
      _stateController.add(next);
    }
  }

  static AvatarRendererFailure _mapError(kit.AvatarError error) {
    return switch (error) {
      kit.AvatarError.appIDUnrecognized ||
      kit.AvatarError.avatarIDUnrecognized ||
      kit.AvatarError.avatarAssetMissing =>
        AvatarRendererFailure.invalidConfiguration,
      kit.AvatarError.sessionTokenInvalid =>
        AvatarRendererFailure.authentication,
      kit.AvatarError.sessionTokenExpired ||
      kit.AvatarError.sessionTimeout => AvatarRendererFailure.sessionExpired,
      kit.AvatarError.insufficientBalance =>
        AvatarRendererFailure.insufficientBalance,
      kit.AvatarError.concurrentLimitExceeded =>
        AvatarRendererFailure.sessionLimit,
      kit.AvatarError.failedToFetchAvatarMetadata ||
      kit.AvatarError.failedToDownloadAvatarAssets ||
      kit.AvatarError.serverError => AvatarRendererFailure.network,
    };
  }

  static AvatarRendererFailure _mapConnectionFailure(String? errorMessage) {
    final normalized = errorMessage?.toLowerCase();
    if (normalized == null || normalized.isEmpty) {
      return AvatarRendererFailure.network;
    }
    if (normalized.contains('sessiontokenexpired') ||
        normalized.contains('session token expired') ||
        normalized.contains('sessiontimeout') ||
        normalized.contains('session timeout')) {
      return AvatarRendererFailure.sessionExpired;
    }
    if (normalized.contains('appidunrecognized') ||
        normalized.contains('avataridunrecognized') ||
        normalized.contains('avatarassetmissing')) {
      return AvatarRendererFailure.invalidConfiguration;
    }
    if (normalized.contains('insufficientbalance') ||
        normalized.contains('insufficient balance') ||
        normalized.contains('insufficient credits')) {
      return AvatarRendererFailure.insufficientBalance;
    }
    if (normalized.contains('concurrentlimitexceeded') ||
        normalized.contains('concurrent limit')) {
      return AvatarRendererFailure.sessionLimit;
    }
    if (normalized.contains('sessiontokeninvalid') ||
        normalized.contains('session token invalid') ||
        normalized.contains('authentication') ||
        normalized.contains('unauthorized') ||
        normalized.contains('forbidden')) {
      return AvatarRendererFailure.authentication;
    }
    return AvatarRendererFailure.network;
  }
}

/// Keeps the SDK-specific controller type out of the vendor-neutral boundary.
final class _AvatarSurfaceHost extends StatelessWidget {
  const _AvatarSurfaceHost({
    super.key,
    required this.avatar,
    required this.onController,
  });

  final kit.Avatar avatar;
  final ValueChanged<kit.AvatarController> onController;

  @override
  Widget build(BuildContext context) {
    return kit.AvatarWidget(
      avatar: avatar,
      onPlatformViewCreated: onController,
    );
  }
}
