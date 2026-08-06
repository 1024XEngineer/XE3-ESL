import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';

/// Owns playback and deletion state for audio on committed Agent Messages.
final class AgentMessageAudioController extends ChangeNotifier
    with WidgetsBindingObserver {
  AgentMessageAudioController({
    required this.conversationController,
    required this.client,
    required this.audioPlayer,
    this.assistantSpeechClient,
  }) {
    _positionSubscription = audioPlayer.onPosition.listen(_handlePosition);
    _completionSubscription = audioPlayer.onComplete.listen((_) {
      _handlePlaybackCompletion();
    });
    conversationController.addListener(_syncConversation);
    WidgetsBinding.instance.addObserver(this);
    _syncConversation();
  }

  final ConversationController conversationController;
  final AgentMessageAudioClient client;
  final AgentAudioPlayer audioPlayer;
  final AgentAssistantSpeechClient? assistantSpeechClient;

  String? _threadId;
  Set<String> _visibleMessageIds = <String>{};
  Map<String, String?> _visibleMessageAudioIds = <String, String?>{};
  String? _loadingMessageId;
  String? _playingMessageId;
  bool _messagePlaybackUsesPreview = false;
  String? _activeMessageAudioId;
  String? _deletingMessageId;
  String? _deletingAudioId;
  String? _errorMessage;
  String? _errorMessageId;
  Duration _playbackPosition = Duration.zero;
  double _speechSpeed = 1;
  int _accountEpoch = 0;
  int _generation = 0;
  bool _disposed = false;
  Future<void>? _operation;
  Future<void>? _cleanupFuture;
  StreamSubscription<Duration>? _positionSubscription;
  StreamSubscription<void>? _completionSubscription;
  _LiveAssistantSpeech? _liveAssistantSpeech;
  Object? _liveSpeechStartToken;
  String? _liveSpeechCommittedMessageId;

  String? get threadId => _threadId;
  double get speechSpeed => _speechSpeed;
  String? get loadingMessageId => _loadingMessageId;
  String? get playingMessageId => _playingMessageId;
  bool get messagePlaybackUsesPreview => _messagePlaybackUsesPreview;
  String? get deletingMessageId => _deletingMessageId;
  String? get errorMessage => _errorMessage;
  String? get errorMessageId => _errorMessageId;
  Duration get playbackPosition => _playbackPosition;

  Future<void> toggleMessagePlayback(AgentMessage message) {
    return _toggleMessagePlayback(message);
  }

  Future<void> toggleSpeechPreview(AgentMessage message, String text) {
    return _toggleMessagePlayback(message, previewText: text);
  }

  /// Starts playback for an assistant Message just committed by a voice Run.
  Future<void> playCommittedAssistant(AgentMessage message) {
    if (message.role != AgentMessageRole.assistant) {
      return Future<void>.value();
    }
    if (_liveSpeechCommittedMessageId == message.id) {
      return Future<void>.value();
    }
    return _toggleMessagePlayback(message);
  }

  Future<void> stopPlayback() async {
    if (_disposed) {
      return;
    }
    _generation++;
    await _cancelLiveAssistantSpeech();
    _resetPlaybackPresentation();
    notifyListeners();
    await audioPlayer.stop();
  }

  Future<void> _toggleMessagePlayback(
    AgentMessage message, {
    String? previewText,
  }) {
    if (_liveAssistantSpeech != null) {
      return _cancelLiveAssistantSpeech().then(
        (_) => _toggleMessagePlayback(message, previewText: previewText),
      );
    }
    if (!_visibleMessageIds.contains(message.id) ||
        _threadId == null ||
        _disposed ||
        _deletingMessageId == message.id) {
      return Future<void>.value();
    }
    final usesPreview = previewText != null;
    if ((_playingMessageId == message.id || _loadingMessageId == message.id) &&
        _messagePlaybackUsesPreview == usesPreview) {
      _generation++;
      _resetPlaybackPresentation();
      notifyListeners();
      return audioPlayer.stop();
    }
    final audio = message.audio;
    if (message.role == AgentMessageRole.user &&
        (message.modality != AgentMessageModality.voice ||
            audio == null ||
            !audio.isReadable)) {
      return Future<void>.value();
    }
    final generation = ++_generation;
    final fence = _MessageAudioFence(
      accountEpoch: _accountEpoch,
      generation: generation,
      threadId: _threadId!,
      messageId: message.id,
      audioId: audio?.id,
    );
    _resetPlaybackPresentation();
    _loadingMessageId = message.id;
    _messagePlaybackUsesPreview = usesPreview;
    _activeMessageAudioId = audio?.id;
    notifyListeners();
    late final Future<void> operation;
    operation = _playMessage(fence, message, previewText: previewText)
        .whenComplete(() {
          if (identical(_operation, operation)) {
            _operation = null;
          }
        });
    _operation = operation;
    return operation;
  }

  Future<void> _playMessage(
    _MessageAudioFence fence,
    AgentMessage message, {
    String? previewText,
  }) async {
    Uint8List? bytes;
    try {
      await audioPlayer.stop();
      bytes = previewText != null
          ? await client.loadSpeechPreview(
              messageId: message.id,
              text: previewText,
            )
          : message.role == AgentMessageRole.user
          ? await client.loadMessageAudio(audioId: message.audio!.id)
          : await client.loadAssistantSpeech(messageId: message.id);
      if (!_isCurrent(fence)) {
        return;
      }
      _loadingMessageId = null;
      _playingMessageId = message.id;
      _playbackPosition = Duration.zero;
      notifyListeners();
      await audioPlayer.playWav(
        bytes,
        speed: message.role == AgentMessageRole.assistant ? _speechSpeed : 1,
      );
      if (!_isCurrent(fence)) {
        await audioPlayer.stop();
      }
    } catch (error) {
      if (_isCurrent(fence)) {
        _loadingMessageId = null;
        _playingMessageId = null;
        _errorMessageId = message.id;
        _errorMessage = _playbackFailureMessage(error, message.role);
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    } finally {
      bytes?.fillRange(0, bytes.length, 0);
    }
  }

  void cycleSpeechSpeed() {
    const speeds = <double>[0.75, 1, 1.25, 1.5];
    final index = speeds.indexOf(_speechSpeed);
    _speechSpeed = speeds[(index + 1) % speeds.length];
    if (_playingMessageId != null) {
      _generation++;
      unawaited(_cancelLiveAssistantSpeech());
      _resetPlaybackPresentation();
      unawaited(audioPlayer.stop());
    }
    notifyListeners();
  }

  Future<void> deleteMessageAudio(AgentMessage message) async {
    final audio = message.audio;
    if (message.role != AgentMessageRole.user ||
        message.modality != AgentMessageModality.voice ||
        audio == null ||
        !audio.isReadable ||
        !_visibleMessageIds.contains(message.id) ||
        _deletingMessageId != null ||
        _threadId == null) {
      return;
    }
    final fence = _captureFence(messageId: message.id, audioId: audio.id);
    _deletingMessageId = message.id;
    _deletingAudioId = audio.id;
    _errorMessage = null;
    _errorMessageId = null;
    final stopsCurrentPlayback =
        _playingMessageId == message.id || _loadingMessageId == message.id;
    if (stopsCurrentPlayback) {
      _generation++;
      _resetPlaybackPresentation();
    }
    notifyListeners();
    try {
      if (stopsCurrentPlayback) {
        await audioPlayer.stop();
        if (!_isCurrent(fence, allowGenerationAdvance: true)) {
          return;
        }
      }
      await client.deleteMessageAudio(audioId: audio.id);
      if (!_isCurrent(fence, allowGenerationAdvance: true)) {
        return;
      }
      conversationController.markMessageAudioDeleted(
        message.id,
        audio.copyWith(
          status: AgentMessageAudioStatus.deleted,
          clearPlaybackPath: true,
          deletedAt: DateTime.now().toUtc(),
        ),
      );
      _deletingMessageId = null;
      _deletingAudioId = null;
      notifyListeners();
    } catch (error) {
      if (_isCurrent(fence, allowGenerationAdvance: true)) {
        _deletingMessageId = null;
        _deletingAudioId = null;
        _errorMessageId = message.id;
        _errorMessage = _deleteFailureMessage(error);
        notifyListeners();
        await _clearOnAuthenticationFailure(error);
      }
    }
  }

  double playbackProgressFor(AgentMessage message) {
    if (_playingMessageId != message.id) {
      return 0;
    }
    final duration = message.audio?.duration;
    if (duration == null || duration <= Duration.zero) {
      return 0;
    }
    return (_playbackPosition.inMilliseconds / duration.inMilliseconds).clamp(
      0,
      1,
    );
  }

  Future<void> clearPrivateState() async {
    final existing = _cleanupFuture;
    if (existing != null) {
      await existing;
      return;
    }
    _accountEpoch++;
    _generation++;
    await _cancelLiveAssistantSpeech();
    _threadId = null;
    _visibleMessageIds = <String>{};
    _visibleMessageAudioIds = <String, String?>{};
    _resetMediaPresentation();
    if (!_disposed) {
      notifyListeners();
    }
    final cleanup = Future<void>.sync(audioPlayer.clearAccountState);
    _cleanupFuture = cleanup;
    try {
      await cleanup;
      await _operation;
      await audioPlayer.clearAccountState();
    } finally {
      if (identical(_cleanupFuture, cleanup)) {
        _cleanupFuture = null;
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (_disposed || state == AppLifecycleState.resumed) {
      return;
    }
    _generation++;
    unawaited(_cancelLiveAssistantSpeech());
    _resetPlaybackPresentation(clearError: false);
    notifyListeners();
    unawaited(audioPlayer.stop());
  }

  @override
  void dispose() {
    if (_disposed) {
      return;
    }
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    conversationController.removeListener(_syncConversation);
    _accountEpoch++;
    _generation++;
    unawaited(_cancelLiveAssistantSpeech());
    unawaited(_positionSubscription?.cancel());
    unawaited(_completionSubscription?.cancel());
    unawaited(audioPlayer.dispose());
    super.dispose();
  }

  void _syncConversation() {
    if (_disposed) {
      return;
    }
    final nextThreadId = conversationController.threadId;
    final messages = conversationController.messages;
    final messageIds = {for (final message in messages) message.id};
    final messageAudioIds = {
      for (final message in messages)
        message.id: message.audio?.isReadable == true
            ? message.audio!.id
            : null,
    };
    if (nextThreadId != _threadId) {
      _generation++;
      unawaited(_cancelLiveAssistantSpeech());
      _liveSpeechCommittedMessageId = null;
      _threadId = nextThreadId;
      _visibleMessageIds = messageIds;
      _visibleMessageAudioIds = messageAudioIds;
      _resetMediaPresentation();
      notifyListeners();
      unawaited(audioPlayer.stop());
      return;
    }
    _visibleMessageIds = messageIds;
    _visibleMessageAudioIds = messageAudioIds;
    _syncLiveAssistantSpeech(messages);
    _fenceUnavailableMedia();
  }

  void _handlePosition(Duration position) {
    if (_disposed || _playingMessageId == null) {
      return;
    }
    _playbackPosition = position;
    notifyListeners();
  }

  void _handlePlaybackCompletion() {
    if (_disposed) {
      return;
    }
    final live = _liveAssistantSpeech;
    if (live != null && live.playing) {
      live.playing = false;
      if (live.remoteCompleted) {
        _finishLiveAssistantSpeech(live);
      }
      return;
    }
    _resetPlaybackPresentation(clearError: false);
    notifyListeners();
  }

  void _syncLiveAssistantSpeech(List<AgentMessage> messages) {
    final speechClient = assistantSpeechClient;
    final threadId = _threadId;
    if (speechClient == null ||
        threadId == null ||
        audioPlayer is! AgentPCMStreamPlayer) {
      return;
    }
    final live = _liveAssistantSpeech;
    AgentMessage? streaming;
    for (var index = messages.length - 1; index >= 0; index--) {
      final candidate = messages[index];
      if (candidate.role != AgentMessageRole.assistant ||
          !candidate.isStreaming ||
          candidate.hasFailed ||
          !candidate.id.startsWith('stream-')) {
        continue;
      }
      AgentMessage? precedingUser;
      for (var userIndex = index - 1; userIndex >= 0; userIndex--) {
        if (messages[userIndex].role == AgentMessageRole.user) {
          precedingUser = messages[userIndex];
          break;
        }
      }
      if (precedingUser?.modality == AgentMessageModality.voice) {
        streaming = candidate;
        if (live == null || live.messageId != candidate.id) {
          unawaited(
            _startLiveAssistantSpeech(
              threadId,
              candidate,
              precedingUserMessageId: precedingUser!.id,
            ),
          );
          return;
        }
      }
      break;
    }
    if (streaming != null && live != null) {
      _appendLiveAssistantText(live, streaming.text, finalText: false);
      return;
    }
    if (live == null || live.inputClosed) {
      return;
    }
    final userIndex = messages.indexWhere(
      (message) => message.id == live.precedingUserMessageId,
    );
    AgentMessage? committed;
    if (userIndex >= 0) {
      for (var index = userIndex + 1; index < messages.length; index++) {
        final candidate = messages[index];
        if (candidate.role == AgentMessageRole.user) {
          break;
        }
        if (candidate.role == AgentMessageRole.assistant &&
            !candidate.isStreaming &&
            !candidate.hasFailed &&
            candidate.text.startsWith(live.observedText)) {
          committed = candidate;
          break;
        }
      }
    }
    if (committed == null) {
      unawaited(_cancelLiveAssistantSpeech());
      return;
    }
    final transientId = live.messageId;
    live.messageId = committed.id;
    _liveSpeechCommittedMessageId = committed.id;
    if (_playingMessageId == transientId) {
      _playingMessageId = committed.id;
    }
    if (_loadingMessageId == transientId) {
      _loadingMessageId = committed.id;
    }
    _appendLiveAssistantText(live, committed.text, finalText: true);
    live.inputClosed = true;
    unawaited(live.segments.close());
  }

  Future<void> _startLiveAssistantSpeech(
    String threadId,
    AgentMessage message, {
    required String precedingUserMessageId,
  }) async {
    final startToken = Object();
    _liveSpeechStartToken = startToken;
    await _cancelLiveAssistantSpeech(preserveStartToken: true);
    if (_disposed ||
        _threadId != threadId ||
        !identical(_liveSpeechStartToken, startToken)) {
      return;
    }
    _liveSpeechStartToken = null;
    final speechClient = assistantSpeechClient;
    if (speechClient == null || audioPlayer is! AgentPCMStreamPlayer) {
      return;
    }
    final live = _LiveAssistantSpeech(
      threadId: threadId,
      messageId: message.id,
      precedingUserMessageId: precedingUserMessageId,
    );
    _liveAssistantSpeech = live;
    _liveSpeechCommittedMessageId = null;
    _loadingMessageId = message.id;
    _playingMessageId = null;
    _messagePlaybackUsesPreview = false;
    _errorMessage = null;
    _errorMessageId = null;
    notifyListeners();
    live.subscription = speechClient
        .streamAssistantSpeech(
          threadId: threadId,
          segments: live.segments.stream,
        )
        .listen(
          (segment) => _handleLiveAssistantAudio(live, segment),
          onError: (Object error, StackTrace _) {
            _failLiveAssistantSpeech(live, error);
          },
          onDone: () {
            if (!identical(_liveAssistantSpeech, live)) {
              return;
            }
            live.remoteCompleted = true;
            if (!live.playing) {
              _finishLiveAssistantSpeech(live);
              return;
            }
            live.playbackOperation = live.playbackOperation
                .then(
                  (_) =>
                      (audioPlayer as AgentPCMStreamPlayer).finishPCMStream(),
                )
                .catchError((Object error) {
                  _failLiveAssistantSpeech(live, error);
                });
          },
        );
    _appendLiveAssistantText(live, message.text, finalText: false);
  }

  void _appendLiveAssistantText(
    _LiveAssistantSpeech live,
    String text, {
    required bool finalText,
  }) {
    if (!identical(_liveAssistantSpeech, live) ||
        live.inputClosed ||
        !text.startsWith(live.observedText)) {
      return;
    }
    live.observedText = text;
    final boundaries = _assistantSpeechBoundaries(
      text,
      live.consumedOffset,
      finalText: finalText,
    );
    for (final boundary in boundaries) {
      if (live.nextSequence > 64) {
        _failLiveAssistantSpeech(
          live,
          StateError('Assistant speech segment limit exceeded.'),
        );
        return;
      }
      final spoken = _cleanAssistantSpeechText(
        text.substring(live.consumedOffset, boundary),
      );
      live.consumedOffset = boundary;
      if (spoken.isEmpty) {
        continue;
      }
      live.segments.add(
        AgentAssistantSpeechTextSegment(
          sequence: live.nextSequence++,
          text: spoken,
        ),
      );
    }
  }

  void _handleLiveAssistantAudio(
    _LiveAssistantSpeech live,
    AgentAssistantSpeechAudioSegment segment,
  ) {
    final sameSegment = segment.sequence == live.audioSequence;
    final nextSegment = segment.sequence == live.audioSequence + 1;
    final validChunk = sameSegment
        ? segment.chunkIndex == live.nextChunkIndex
        : nextSegment && segment.chunkIndex == 1;
    if (!identical(_liveAssistantSpeech, live) || !validChunk) {
      segment.audio.fillRange(0, segment.audio.length, 0);
      _failLiveAssistantSpeech(
        live,
        StateError('Assistant speech audio sequence is invalid.'),
      );
      return;
    }
    if (nextSegment) {
      live.audioSequence = segment.sequence;
      live.nextChunkIndex = 1;
    }
    live.nextChunkIndex++;
    final audio = segment.audio;
    final streamPlayer = audioPlayer as AgentPCMStreamPlayer;
    if (!live.playing) {
      live.playing = true;
      _loadingMessageId = null;
      _playingMessageId = live.messageId;
      _playbackPosition = Duration.zero;
      notifyListeners();
    }
    live.playbackOperation = live.playbackOperation
        .then((_) async {
          if (!identical(_liveAssistantSpeech, live)) {
            return;
          }
          if (!live.playerStarted) {
            live.playerStarted = true;
            await streamPlayer.startPCMStream(speed: _speechSpeed);
          }
          await streamPlayer.appendPCM(audio);
        })
        .catchError((Object error) {
          _failLiveAssistantSpeech(live, error);
        })
        .whenComplete(() {
          audio.fillRange(0, audio.length, 0);
        });
  }

  void _finishLiveAssistantSpeech(_LiveAssistantSpeech live) {
    if (!identical(_liveAssistantSpeech, live)) {
      return;
    }
    _liveAssistantSpeech = null;
    _loadingMessageId = null;
    _playingMessageId = null;
    _playbackPosition = Duration.zero;
    notifyListeners();
  }

  void _failLiveAssistantSpeech(_LiveAssistantSpeech live, Object error) {
    if (!identical(_liveAssistantSpeech, live)) {
      return;
    }
    final messageId = live.messageId;
    unawaited(_cancelLiveAssistantSpeech());
    _loadingMessageId = null;
    _playingMessageId = null;
    _errorMessageId = messageId;
    _errorMessage = '实时朗读中断，可点击朗读重新播放。';
    notifyListeners();
    unawaited(_clearOnAuthenticationFailure(error));
  }

  Future<void> _cancelLiveAssistantSpeech({
    bool preserveStartToken = false,
  }) async {
    if (!preserveStartToken) {
      _liveSpeechStartToken = null;
    }
    final live = _liveAssistantSpeech;
    if (live == null) {
      return;
    }
    _liveAssistantSpeech = null;
    if (!live.inputClosed) {
      live.inputClosed = true;
      await live.segments.close();
    }
    await live.subscription?.cancel();
    if (live.playerStarted) {
      await audioPlayer.stop();
    }
  }

  void _fenceUnavailableMedia() {
    var changed = false;
    var stopPlayback = false;
    final playbackId = _playingMessageId ?? _loadingMessageId;
    if (playbackId != null &&
        (!_visibleMessageIds.contains(playbackId) ||
            (_activeMessageAudioId != null &&
                _visibleMessageAudioIds[playbackId] !=
                    _activeMessageAudioId))) {
      _generation++;
      _resetPlaybackPresentation();
      stopPlayback = true;
      changed = true;
    }
    final deletingId = _deletingMessageId;
    if (deletingId != null &&
        (!_visibleMessageIds.contains(deletingId) ||
            _visibleMessageAudioIds[deletingId] != _deletingAudioId)) {
      _deletingMessageId = null;
      _deletingAudioId = null;
      changed = true;
    }
    if (!changed) {
      return;
    }
    if (stopPlayback) {
      unawaited(audioPlayer.stop());
    }
    notifyListeners();
  }

  void _resetPlaybackPresentation({bool clearError = true}) {
    _loadingMessageId = null;
    _playingMessageId = null;
    _messagePlaybackUsesPreview = false;
    _activeMessageAudioId = null;
    if (clearError) {
      _errorMessage = null;
      _errorMessageId = null;
    }
    _playbackPosition = Duration.zero;
  }

  void _resetMediaPresentation() {
    _resetPlaybackPresentation();
    _deletingMessageId = null;
    _deletingAudioId = null;
  }

  _MessageAudioFence _captureFence({
    required String messageId,
    String? audioId,
  }) {
    final threadId = _threadId;
    if (threadId == null) {
      throw StateError('An Agent Thread is required for Message audio.');
    }
    return _MessageAudioFence(
      accountEpoch: _accountEpoch,
      generation: _generation,
      threadId: threadId,
      messageId: messageId,
      audioId: audioId,
    );
  }

  bool _isCurrent(
    _MessageAudioFence fence, {
    bool allowGenerationAdvance = false,
  }) {
    return !_disposed &&
        fence.accountEpoch == _accountEpoch &&
        (allowGenerationAdvance
            ? fence.generation <= _generation
            : fence.generation == _generation) &&
        fence.threadId == _threadId &&
        _visibleMessageIds.contains(fence.messageId) &&
        (fence.audioId == null ||
            _visibleMessageAudioIds[fence.messageId] == fence.audioId);
  }

  Future<void> _clearOnAuthenticationFailure(Object error) async {
    if (error is! AgentClientException ||
        error.kind != AgentClientFailureKind.authenticationRequired) {
      return;
    }
    _generation++;
    _resetMediaPresentation();
    await audioPlayer.clearAccountState();
    if (!_disposed) {
      notifyListeners();
    }
  }

  String _playbackFailureMessage(Object error, AgentMessageRole role) {
    final prefix = role == AgentMessageRole.user ? '录音播放' : '朗读';
    if (error is AgentClientException) {
      return switch (error.kind) {
        AgentClientFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
        AgentClientFailureKind.notFound =>
          role == AgentMessageRole.user ? '录音不存在或已删除，确认文字仍会保留。' : '这条回复暂时无法朗读。',
        AgentClientFailureKind.network => '$prefix失败，请检查网络后重试。',
        _ => '$prefix暂时不可用，可以重试。',
      };
    }
    return '$prefix暂时不可用，可以重试。';
  }

  String _deleteFailureMessage(Object error) {
    if (error is AgentClientException &&
        error.kind == AgentClientFailureKind.authenticationRequired) {
      return '登录状态已失效，请重新登录。';
    }
    if (error is AgentClientException &&
        error.kind == AgentClientFailureKind.network) {
      return '录音尚未删除，请检查网络后重试。';
    }
    return '录音尚未删除，可以重试；确认文字不会受影响。';
  }
}

final class _MessageAudioFence {
  const _MessageAudioFence({
    required this.accountEpoch,
    required this.generation,
    required this.threadId,
    required this.messageId,
    this.audioId,
  });

  final int accountEpoch;
  final int generation;
  final String threadId;
  final String messageId;
  final String? audioId;
}

final class _LiveAssistantSpeech {
  _LiveAssistantSpeech({
    required this.threadId,
    required this.messageId,
    required this.precedingUserMessageId,
  });

  final String threadId;
  final String precedingUserMessageId;
  String messageId;
  final StreamController<AgentAssistantSpeechTextSegment> segments =
      StreamController<AgentAssistantSpeechTextSegment>();
  StreamSubscription<AgentAssistantSpeechAudioSegment>? subscription;
  String observedText = '';
  int consumedOffset = 0;
  int nextSequence = 1;
  int audioSequence = 0;
  int nextChunkIndex = 1;
  bool inputClosed = false;
  bool remoteCompleted = false;
  bool playing = false;
  bool playerStarted = false;
  Future<void> playbackOperation = Future<void>.value();
}

List<int> _assistantSpeechBoundaries(
  String text,
  int start, {
  required bool finalText,
}) {
  final boundaries = <int>[];
  for (var index = start; index < text.length; index++) {
    if (_assistantSpeechTerminators.contains(text[index])) {
      boundaries.add(index + 1);
    }
  }
  if (finalText && (boundaries.isEmpty || boundaries.last < text.length)) {
    boundaries.add(text.length);
  }
  return boundaries;
}

String _cleanAssistantSpeechText(String value) {
  return value
      .replaceAll(RegExp(r'[`*_#>]'), '')
      .replaceAll(RegExp(r'^\s*[-+]\s+', multiLine: true), '')
      .trim();
}

const _assistantSpeechTerminators = <String>{
  '.',
  '?',
  '!',
  '。',
  '？',
  '！',
  '\n',
};
