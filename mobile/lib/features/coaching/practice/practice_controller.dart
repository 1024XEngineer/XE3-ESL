import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';

typedef PracticeClientIdFactory = String Function(String scope);

final class PracticeController extends ChangeNotifier
    with WidgetsBindingObserver {
  static const _speechFeedbackRetryPollInterval = Duration(seconds: 2);
  static const _speechFeedbackRetryMaximumPollAttempts = 8;

  PracticeController({
    required this.client,
    PracticeRecorder? recorder,
    this.mediaClient,
    this.audioPlayer,
    PracticeClientIdFactory? clientIdFactory,
    Duration recordingLimit = const Duration(seconds: 58),
  }) : recorder = recorder ?? FakePracticeRecorder(),
       _clientIdFactory = clientIdFactory ?? _createSecureClientId,
       _recordingLimit = recordingLimit {
    if ((mediaClient == null) != (audioPlayer == null)) {
      throw ArgumentError(
        'Practice media client and audio player must be injected together.',
      );
    }
    if (recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 60)) {
      throw ArgumentError.value(
        recordingLimit,
        'recordingLimit',
        'must be positive and no longer than the server 60-second limit',
      );
    }
    _mediaCompletionSubscription = audioPlayer?.onComplete.listen((_) {
      _handleMediaCompletion();
    });
    if (mediaClient != null) {
      WidgetsBinding.instance.addObserver(this);
    }
  }

  final PracticeClient client;
  final PracticeRecorder recorder;
  final PracticeMediaClient? mediaClient;
  final PracticeAudioPlayer? audioPlayer;
  final PracticeClientIdFactory _clientIdFactory;
  final Duration _recordingLimit;
  String? _practiceSessionId;
  SceneFamily? _practiceSceneFamily;
  SceneModel? _practiceSceneModel;
  int? _practiceSessionVersion;
  String? _endPracticeClientId;
  PracticeQuestion? _currentQuestion;
  PracticeQuestionTip? _questionTip;
  String? _questionTipRequestId;
  String? _questionTipErrorMessage;
  bool _questionTipLoading = false;
  int _questionTipGeneration = 0;
  TranscriptionCandidate? _candidate;
  _PendingPracticeAudio? _pendingPracticeAudio;
  String? _activeConfirmationId;
  String? _activeTextAnswer;
  _SpeechFeedbackRetryContext? _speechFeedbackRetry;
  RetryTranscriptionCandidate? _speechFeedbackRetryCandidate;
  int _speechFeedbackRetryCompletionCount = 0;
  SceneDefinition? _activeScene;
  List<PracticeMessage> _practiceMessages = const <PracticeMessage>[];
  PracticeRecordingState _recordingState = PracticeRecordingState.idle;
  String? _errorMessage;
  int _completedTurns = 0;
  int _turnLimit = 0;
  bool _sessionCompleted = false;
  int _epoch = 0;
  int _practiceGeneration = 0;
  bool _busy = false;
  bool _disposed = false;
  Future<void>? _accountCleanupFuture;
  Future<void>? _recorderStartFuture;
  Future<RecordedPracticeAudio>? _recorderStopFuture;
  Future<void>? _stopRecordingFuture;
  Timer? _recordingLimitTimer;
  StreamSubscription<void>? _mediaCompletionSubscription;
  List<PracticeRecordingReference> _recordings =
      const <PracticeRecordingReference>[];
  String? _playingMediaKey;
  String? _loadingMediaKey;
  String? _deletingAudioAssetId;
  String? _mediaErrorMessage;
  int _mediaGeneration = 0;
  Future<void>? _mediaOperation;
  String? get practiceSessionId => _practiceSessionId;
  SceneFamily? get practiceSceneFamily => _practiceSceneFamily;
  SceneModel? get practiceSceneModel => _practiceSceneModel;
  int? get practiceSessionVersion => _practiceSessionVersion;
  PracticeQuestion? get currentQuestion => _currentQuestion;
  PracticeQuestionTip? get questionTip => _questionTip;
  bool get isQuestionTipLoading => _questionTipLoading;
  String? get questionTipErrorMessage => _questionTipErrorMessage;
  bool get canRequestQuestionTip =>
      client is PracticeQuestionTipClient &&
      isInterviewPracticeScene(_practiceSceneFamily, _practiceSceneModel) &&
      _practiceSessionId != null &&
      _currentQuestion != null &&
      !_sessionCompleted &&
      !_disposed &&
      !isBusy &&
      !_questionTipLoading &&
      _recordingState == PracticeRecordingState.idle;
  String? get questionId => _currentQuestion?.id;
  String? get candidateId => _candidate?.id;
  bool get hasPendingPracticeAudio => _pendingPracticeAudio != null;
  SceneDefinition? get scene => _activeScene;

  /// Resolves as soon as the active capture has released the shared recorder.
  /// Network transcription may still continue after this Future completes.
  Future<void> waitForPracticeRecorderRelease() async {
    final operation = _recorderStopFuture;
    if (operation == null) {
      return;
    }
    try {
      await operation;
    } on Object {
      // The caller only needs the recorder lease to be released. The original
      // recording flow owns and reports its transport failure separately.
    }
  }

  /// Adopts a locally buffered answer for the current practice question.
  ///
  /// IELTS Part 3 uses this after it records while the previous Part 2 answer
  /// is still being transcribed. Returning true transfers audio ownership to
  /// this controller; false leaves ownership with the caller.
  bool submitBufferedPracticeAudio(RecordedPracticeAudio audio) {
    final practice = client;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    if (sessionId == null ||
        question == null ||
        _disposed ||
        isBusy ||
        _isSessionCompleted ||
        _pendingPracticeAudio != null ||
        _recordingState != PracticeRecordingState.idle) {
      return false;
    }
    final generation = ++_practiceGeneration;
    final pending = _PendingPracticeAudio(
      audio: audio,
      sessionId: sessionId,
      questionId: question.id,
      clientTurnId: _newClientId('turn'),
    );
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _errorMessage = null;
    _pendingPracticeAudio = pending;
    _recordingState = PracticeRecordingState.transcribing;
    final fence = _captureOperationFence(
      practiceGeneration: generation,
      practiceSessionId: sessionId,
      questionId: question.id,
    );
    notifyListeners();
    final operation = _transcribePendingPracticeAudio(
      practice: practice,
      pending: pending,
      fence: fence,
    );
    _stopRecordingFuture = operation;
    unawaited(
      operation.whenComplete(() {
        if (identical(_stopRecordingFuture, operation)) {
          _stopRecordingFuture = null;
        }
        if (_isOperationCurrent(fence)) {
          notifyListeners();
        }
      }),
    );
    return true;
  }

  List<PracticeMessage> get practiceMessages =>
      List.unmodifiable(_practiceMessages);
  PracticeRecordingState get recordingState => _recordingState;
  String? get transcript =>
      _speechFeedbackRetryCandidate?.text ?? _candidate?.text;
  String? get errorMessage => _errorMessage;
  int get completedTurns => _completedTurns;
  int get turnLimit => _turnLimit;
  bool get isFinalInterviewSubmission =>
      _recordingState == PracticeRecordingState.submitting &&
      _speechFeedbackRetry == null &&
      !_sessionCompleted &&
      _turnLimit > 0 &&
      _completedTurns + 1 == _turnLimit &&
      isInterviewPracticeScene(_practiceSceneFamily, _practiceSceneModel);
  bool get isBusy =>
      _busy ||
      _practiceRequestInFlight ||
      _questionTipLoading ||
      _speechFeedbackRetry != null;
  bool get isSpeechFeedbackRetryActive => _speechFeedbackRetry != null;
  int get speechFeedbackRetryCompletionCount =>
      _speechFeedbackRetryCompletionCount;
  bool get canStartSpeechFeedbackRetry =>
      client is PracticeSpeechFeedbackRetryClient &&
      _practiceSessionId != null &&
      isTurnFeedbackEligiblePracticeScene(
        _practiceSceneFamily,
        _practiceSceneModel,
      ) &&
      !_disposed &&
      !isBusy &&
      _speechFeedbackRetry == null &&
      switch (_recordingState) {
        PracticeRecordingState.idle || PracticeRecordingState.completed => true,
        PracticeRecordingState.starting ||
        PracticeRecordingState.recording ||
        PracticeRecordingState.transcribing ||
        PracticeRecordingState.awaitingConfirmation ||
        PracticeRecordingState.submitting => false,
      };
  bool get supportsPracticeMedia => mediaClient != null && audioPlayer != null;
  List<PracticeRecordingReference> get recordings =>
      List<PracticeRecordingReference>.unmodifiable(_recordings);
  String? get mediaErrorMessage => _mediaErrorMessage;
  bool get isQuestionAudioLoading =>
      _currentQuestion != null &&
      _loadingMediaKey == _questionMediaKey(_currentQuestion!.id);
  bool get isQuestionAudioPlaying =>
      _currentQuestion != null &&
      _playingMediaKey == _questionMediaKey(_currentQuestion!.id);
  bool get canPlayQuestionAudio =>
      supportsPracticeMedia && _currentQuestion?.speechPath != null;
  bool get canUsePracticeAudio =>
      supportsPracticeMedia &&
      !_disposed &&
      !_busy &&
      switch (_recordingState) {
        PracticeRecordingState.idle ||
        PracticeRecordingState.awaitingConfirmation ||
        PracticeRecordingState.completed => true,
        PracticeRecordingState.starting ||
        PracticeRecordingState.recording ||
        PracticeRecordingState.transcribing ||
        PracticeRecordingState.submitting => false,
      };
  bool isRecordingAudioLoading(String audioAssetId) =>
      _loadingMediaKey == _recordingMediaKey(audioAssetId);
  bool isRecordingAudioPlaying(String audioAssetId) =>
      _playingMediaKey == _recordingMediaKey(audioAssetId);
  bool isRecordingDeleting(String audioAssetId) =>
      _deletingAudioAssetId == audioAssetId;

  bool get hasActivePractice {
    return _practiceSessionId != null && !_isSessionCompleted;
  }

  bool get _isSessionCompleted => _sessionCompleted;

  bool get _practiceRequestInFlight {
    return _recordingState == PracticeRecordingState.transcribing ||
        _recordingState == PracticeRecordingState.submitting;
  }

  Future<void> activateCreatedPractice({
    required SceneDefinition scene,
    required String sessionId,
    required String planId,
    required int turnLimit,
    required String clientOperationId,
  }) async {
    final accountFence = _captureOperationFence();
    final practice = client;
    if (!_isOperationCurrent(accountFence) ||
        planId.trim().isEmpty ||
        scene.id.trim().isEmpty ||
        scene.name.trim().isEmpty ||
        turnLimit < 1 ||
        turnLimit > 14 ||
        clientOperationId.trim().isEmpty ||
        isBusy ||
        _disposed) {
      throw StateError('The Practice context changed before activation.');
    }
    if (hasActivePractice) {
      if (_practiceSessionId != sessionId || _turnLimit != turnLimit) {
        throw StateError(
          'A different active Practice Session cannot be replaced.',
        );
      }
      return;
    }
    final fence = _captureOperationFence();
    final epoch = fence.epoch;
    _setBusy(true);
    try {
      final snapshot = await practice.activatePractice(
        sessionId: sessionId,
        clientOperationId: clientOperationId,
      );
      if (!_isOperationCurrent(fence)) {
        throw const PracticeClientOperationCancelled();
      }
      if (snapshot.sessionId != sessionId ||
          snapshot.planId != planId ||
          snapshot.sceneFamily != scene.family ||
          snapshot.sceneModel != scene.model ||
          snapshot.turnLimit != turnLimit) {
        throw StateError(
          'Voice activation did not return the created Practice Session.',
        );
      }
      _activeScene = scene;
      _applyPracticeSnapshot(snapshot);
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  /// Restores the exact voice state for a previously created Session.
  Future<void> restoreCreatedPractice({
    required String sessionId,
    required SceneDefinition scene,
  }) async {
    final accountFence = _captureOperationFence();
    final practice = client;
    if (!_isOperationCurrent(accountFence) ||
        sessionId.trim().isEmpty ||
        scene.id.trim().isEmpty ||
        isBusy ||
        _disposed) {
      throw StateError('The Practice context changed before restore.');
    }
    final fence = _captureOperationFence();
    final epoch = fence.epoch;
    _setBusy(true);
    try {
      final snapshot = await practice.restorePractice(sessionId: sessionId);
      if (!_isOperationCurrent(fence)) {
        throw const PracticeClientOperationCancelled();
      }
      if (snapshot.sessionId != sessionId ||
          snapshot.sceneFamily != scene.family ||
          snapshot.sceneModel != scene.model) {
        throw StateError(
          'Voice restore returned a different Practice Session.',
        );
      }
      _activeScene = scene;
      _applyPracticeSnapshot(snapshot);
    } finally {
      if (_isCurrent(epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> toggleQuestionAudio() async {
    final question = _currentQuestion;
    final speechPath = question?.speechPath;
    if (question == null || speechPath == null) {
      return;
    }
    await _togglePracticeMedia(
      key: _questionMediaKey(question.id)!,
      fence: _captureOperationFence(
        practiceSessionId: _practiceSessionId,
        questionId: question.id,
        questionSpeechPath: speechPath,
      ),
      load: (client) => client.loadQuestionSpeech(speechPath),
      isStillAvailable: () =>
          _currentQuestion?.id == question.id &&
          _currentQuestion?.speechPath == speechPath,
    );
  }

  Future<void> toggleRecordingAudio(String audioAssetId) async {
    if (!_recordings.any(
      (recording) => recording.audioAssetId == audioAssetId,
    )) {
      return;
    }
    await _togglePracticeMedia(
      key: _recordingMediaKey(audioAssetId),
      fence: _captureOperationFence(
        practiceSessionId: _practiceSessionId,
        recordingAudioAssetId: audioAssetId,
      ),
      load: (client) => client.loadRecording(audioAssetId),
      isStillAvailable: () => _recordings.any(
        (recording) => recording.audioAssetId == audioAssetId,
      ),
    );
  }

  Future<void> deleteRecording(String audioAssetId) async {
    final client = mediaClient;
    if (client == null ||
        _disposed ||
        _deletingAudioAssetId != null ||
        !_recordings.any(
          (recording) => recording.audioAssetId == audioAssetId,
        )) {
      return;
    }
    final fence = _captureOperationFence(
      practiceSessionId: _practiceSessionId,
      recordingAudioAssetId: audioAssetId,
    );
    final targetKey = _recordingMediaKey(audioAssetId);
    if (_playingMediaKey == targetKey || _loadingMediaKey == targetKey) {
      final pendingPlayback = _mediaOperation;
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return;
      }
      await pendingPlayback;
      if (!_isOperationCurrent(fence)) {
        return;
      }
      try {
        await audioPlayer?.stop();
      } catch (_) {
        // The generation fence already prevents a late playback presentation.
      }
      if (!_isOperationCurrent(fence)) {
        return;
      }
    }
    if (!_isOperationCurrent(fence)) {
      return;
    }
    final epoch = fence.epoch;
    _deletingAudioAssetId = audioAssetId;
    _mediaErrorMessage = null;
    notifyListeners();
    try {
      await client.deleteRecording(audioAssetId);
      if (!_isCurrent(epoch)) {
        return;
      }
      _recordings = [
        for (final recording in _recordings)
          if (recording.audioAssetId != audioAssetId) recording,
      ];
      _mediaErrorMessage = null;
    } on PracticeClientOperationCancelled {
      // Account cleanup already removed the private presentation.
    } catch (error) {
      if (_isCurrent(epoch)) {
        _mediaErrorMessage = _mediaFailureMessage(error, action: '删除录音');
        if (error is PracticeClientException &&
            error.kind == PracticeClientFailureKind.authenticationRequired) {
          await _clearPlayerAfterAuthenticationFailure();
        }
      }
    } finally {
      if (_isCurrent(epoch) && _deletingAudioAssetId == audioAssetId) {
        _deletingAudioAssetId = null;
        notifyListeners();
      }
    }
  }

  Future<void> stopPracticeAudio({bool notify = true}) async {
    _mediaGeneration++;
    _playingMediaKey = null;
    _loadingMediaKey = null;
    if (notify && !_disposed) {
      notifyListeners();
    }
    try {
      await audioPlayer?.stop();
    } catch (_) {
      // The private UI is already cleared; native cleanup remains best effort.
    }
  }

  /// Safely detaches the locally presented Session before the App leaves its
  /// dedicated Practice Thread. Durable Session progress remains on the
  /// server and can only be restored through [restoreCreatedPractice].
  Future<bool> parkPractice() async {
    if (_disposed || _practiceRequestInFlight) {
      return false;
    }
    if (_pendingPracticeAudio != null) {
      return false;
    }
    final state = _recordingState;
    if (state == PracticeRecordingState.starting ||
        state == PracticeRecordingState.recording ||
        state == PracticeRecordingState.awaitingConfirmation) {
      final speechFeedbackRetry = _speechFeedbackRetry;
      _practiceGeneration++;
      _cancelRecordingLimit();
      await _recorderStartFuture;
      if (_disposed || _practiceRequestInFlight) {
        return false;
      }
      await recorder.discardCurrent();
      _candidate = null;
      _activeConfirmationId = null;
      _activeTextAnswer = null;
      if (speechFeedbackRetry != null &&
          identical(_speechFeedbackRetry, speechFeedbackRetry)) {
        _restoreSpeechFeedbackRetryState(speechFeedbackRetry);
      } else {
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = null;
      }
      notifyListeners();
    }
    await stopPracticeAudio();
    if (_disposed || _practiceRequestInFlight) {
      return false;
    }
    _applyPracticeSnapshot(null);
    notifyListeners();
    return true;
  }

  Future<bool> endActivePracticeEarly() async {
    final practice = client;
    final PracticeLifecycleClient? lifecycle =
        practice is PracticeLifecycleClient
        ? practice as PracticeLifecycleClient
        : null;
    final sessionId = _practiceSessionId;
    final sessionVersion = _practiceSessionVersion;
    if (lifecycle == null ||
        sessionId == null ||
        sessionVersion == null ||
        !hasActivePractice ||
        isBusy ||
        _disposed) {
      return false;
    }
    final fence = _captureOperationFence(practiceSessionId: sessionId);
    final operationId = _endPracticeClientId ??= _newClientId('practice-end');
    _setBusy(true);
    _errorMessage = null;
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      final result = await lifecycle.endEarly(
        sessionId: sessionId,
        expectedSessionVersion: sessionVersion,
        idempotencyKey: operationId,
      );
      if (!_isOperationCurrent(fence) ||
          result.sessionId != sessionId ||
          result.status != PracticeSessionLifecycleStatus.endedEarly ||
          result.version <= sessionVersion) {
        throw StateError('Practice end response did not match the session.');
      }
      _applyPracticeSnapshot(null);
      _endPracticeClientId = null;
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _errorMessage =
            error is PracticeClientException &&
                error.kind == PracticeClientFailureKind.authenticationRequired
            ? '登录状态已失效，请重新登录后继续。'
            : '暂时无法结束当前练习，进度仍已保留，可以重试。';
      }
      return false;
    } finally {
      if (_isCurrent(fence.epoch)) {
        _setBusy(false);
      }
    }
  }

  Future<void> _togglePracticeMedia({
    required String key,
    required _PracticeOperationFence fence,
    required Future<Uint8List> Function(PracticeMediaClient client) load,
    required bool Function() isStillAvailable,
  }) async {
    final client = mediaClient;
    final player = audioPlayer;
    if (client == null ||
        player == null ||
        _disposed ||
        !canUsePracticeAudio ||
        _loadingMediaKey != null ||
        _mediaOperation != null) {
      return;
    }
    if (_playingMediaKey == key) {
      await stopPracticeAudio();
      return;
    }
    await stopPracticeAudio();
    if (!_isOperationCurrent(fence) ||
        !canUsePracticeAudio ||
        !isStillAvailable()) {
      return;
    }
    final generation = ++_mediaGeneration;
    _loadingMediaKey = key;
    _mediaErrorMessage = null;
    notifyListeners();
    final operation = _loadAndPlayPracticeMedia(
      generation: generation,
      key: key,
      fence: fence,
      client: client,
      player: player,
      load: load,
      isStillAvailable: isStillAvailable,
    );
    _mediaOperation = operation;
    try {
      await operation;
    } finally {
      if (identical(_mediaOperation, operation)) {
        _mediaOperation = null;
      }
    }
  }

  Future<void> _loadAndPlayPracticeMedia({
    required int generation,
    required String key,
    required _PracticeOperationFence fence,
    required PracticeMediaClient client,
    required PracticeAudioPlayer player,
    required Future<Uint8List> Function(PracticeMediaClient client) load,
    required bool Function() isStillAvailable,
  }) async {
    Uint8List? bytes;
    try {
      bytes = await load(client);
      if (!_isCurrentMedia(generation) || !_isOperationCurrent(fence)) {
        return;
      }
      if (!isStillAvailable()) {
        if (_loadingMediaKey == key) {
          _loadingMediaKey = null;
        }
        return;
      }
      await player.playWav(bytes);
      if (!_isCurrentMedia(generation) || !_isOperationCurrent(fence)) {
        await player.stop();
        return;
      }
      if (!isStillAvailable()) {
        if (_loadingMediaKey == key) {
          _loadingMediaKey = null;
        }
        await player.stop();
        return;
      }
      _loadingMediaKey = null;
      _playingMediaKey = key;
      _mediaErrorMessage = null;
    } on PracticeClientOperationCancelled {
      // Account cleanup already removed the private presentation.
    } on PracticeAudioPlaybackInterruptedException {
      if (_isCurrentMedia(generation)) {
        _loadingMediaKey = null;
        _playingMediaKey = null;
      }
    } catch (error) {
      if (_isCurrentMedia(generation)) {
        _loadingMediaKey = null;
        _playingMediaKey = null;
        _mediaErrorMessage = _mediaFailureMessage(error, action: '播放音频');
        if (error is PracticeClientException &&
            error.kind == PracticeClientFailureKind.authenticationRequired) {
          await _clearPlayerAfterAuthenticationFailure();
        }
      }
    } finally {
      if (bytes != null) {
        try {
          bytes.fillRange(0, bytes.length, 0);
        } catch (_) {
          // The production media client always returns an owned byte buffer.
        }
      }
      if (_isCurrentMedia(generation)) {
        notifyListeners();
      }
    }
  }

  Future<bool> startSpeechFeedbackRetry(SpeechFeedbackItem item) async {
    final PracticeSpeechFeedbackRetryClient? practice = switch (client) {
      final PracticeSpeechFeedbackRetryClient client => client,
      _ => null,
    };
    final sessionId = _practiceSessionId;
    if (practice == null ||
        sessionId == null ||
        item.repracticeMode != SpeechFeedbackRepracticeMode.sameQuestion ||
        item.anchor is! ConversationTranscriptFeedbackAnchor ||
        !canStartSpeechFeedbackRetry) {
      return false;
    }
    final generation = ++_practiceGeneration;
    final context = _SpeechFeedbackRetryContext(
      feedbackItemId: item.feedbackItemId,
      originalTurnId:
          (item.anchor as ConversationTranscriptFeedbackAnchor).turnId,
      returnState: _recordingState,
      returnErrorMessage: _errorMessage,
      requestIdempotencyKey: _newClientId('feedback-retry'),
    );
    _speechFeedbackRetry = context;
    _speechFeedbackRetryCandidate = null;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _errorMessage = null;
    _cancelRecordingLimit();
    _recordingState = PracticeRecordingState.submitting;
    notifyListeners();
    final fence = _captureOperationFence(
      practiceGeneration: generation,
      practiceSessionId: sessionId,
    );
    try {
      var request = await practice.requestSameQuestionRetry(
        feedbackItemId: item.feedbackItemId,
        idempotencyKey: context.requestIdempotencyKey,
      );
      request = await _awaitSpeechFeedbackRetryTurn(
        practice: practice,
        request: request,
        fence: fence,
        context: context,
      );
      if (!_isOperationCurrent(fence) ||
          !identical(_speechFeedbackRetry, context)) {
        return false;
      }
      _validateSpeechFeedbackRetryRequest(
        request,
        context: context,
        sessionId: sessionId,
      );
      if (request.retryStatus == PracticeRetryRequestStatus.failed) {
        throw _SpeechFeedbackRetryCreationException(request.stableFailure);
      }
      if (request.retryStatus != PracticeRetryRequestStatus.turnCreated ||
          request.newTurnId == null ||
          request.answerPath == null) {
        throw const _SpeechFeedbackRetryCreationException(null);
      }
      context.request = request;
      _recordingState = PracticeRecordingState.starting;
      notifyListeners();
      final operation = _startRecorder(fence, _recordingLimit);
      _recorderStartFuture = operation;
      await operation.whenComplete(() {
        if (identical(_recorderStartFuture, operation)) {
          _recorderStartFuture = null;
        }
      });
      return _isOperationCurrent(fence) &&
          identical(_speechFeedbackRetry, context) &&
          _recordingState == PracticeRecordingState.recording;
    } catch (error) {
      if (_isOperationCurrent(fence) &&
          identical(_speechFeedbackRetry, context)) {
        _restoreSpeechFeedbackRetryState(context);
        _errorMessage = _speechFeedbackRetryFailureMessage(error);
        notifyListeners();
      }
      return false;
    }
  }

  Future<PracticeRetryRequest> _awaitSpeechFeedbackRetryTurn({
    required PracticeSpeechFeedbackRetryClient practice,
    required PracticeRetryRequest request,
    required _PracticeOperationFence fence,
    required _SpeechFeedbackRetryContext context,
  }) async {
    var current = request;
    for (
      var attempt = 0;
      attempt < _speechFeedbackRetryMaximumPollAttempts;
      attempt++
    ) {
      _validateSpeechFeedbackRetryRequest(
        current,
        context: context,
        sessionId: fence.practiceSessionId!,
      );
      if (current.retryStatus != PracticeRetryRequestStatus.pending) {
        return current;
      }
      if (attempt + 1 == _speechFeedbackRetryMaximumPollAttempts) {
        return current;
      }
      await Future<void>.delayed(_speechFeedbackRetryPollInterval);
      if (!_isOperationCurrent(fence) ||
          !identical(_speechFeedbackRetry, context)) {
        throw const PracticeClientOperationCancelled();
      }
      current = await practice.getSameQuestionRetryRequest(
        retryRequestId: current.retryRequestId,
      );
    }
    return current;
  }

  Future<void> startRecording({Duration? limit}) {
    if (!hasActivePractice ||
        isBusy ||
        _pendingPracticeAudio != null ||
        _currentQuestion == null ||
        _recordingState != PracticeRecordingState.idle) {
      return Future<void>.value();
    }
    final recordingLimit = limit ?? _recordingLimit;
    if (recordingLimit <= Duration.zero ||
        recordingLimit > const Duration(seconds: 120)) {
      throw ArgumentError.value(
        recordingLimit,
        'limit',
        'must be positive and no longer than 120 seconds',
      );
    }
    final generation = ++_practiceGeneration;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _errorMessage = null;
    _cancelRecordingLimit();
    _recordingState = PracticeRecordingState.starting;
    notifyListeners();
    final operation = _startRecorder(
      _captureOperationFence(
        practiceGeneration: generation,
        practiceSessionId: _practiceSessionId,
        questionId: _currentQuestion?.id,
      ),
      recordingLimit,
    );
    _recorderStartFuture = operation;
    return operation.whenComplete(() {
      if (identical(_recorderStartFuture, operation)) {
        _recorderStartFuture = null;
      }
    });
  }

  Future<void> _startRecorder(
    _PracticeOperationFence fence,
    Duration recordingLimit,
  ) async {
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence) ||
          _recordingState != PracticeRecordingState.starting) {
        return;
      }
      await recorder.start();
      if (!_isOperationCurrent(fence)) {
        await recorder.discardCurrent();
        return;
      }
      _recordingState = PracticeRecordingState.recording;
      _recordingLimitTimer = Timer(recordingLimit, () {
        if (_isOperationCurrent(fence) &&
            !_disposed &&
            _recordingState == PracticeRecordingState.recording) {
          unawaited(stopRecording());
        }
      });
    } on PracticeRecordingException catch (error) {
      if (_isOperationCurrent(fence)) {
        final retry = _speechFeedbackRetry;
        if (retry == null) {
          _recordingState = PracticeRecordingState.idle;
        } else {
          _restoreSpeechFeedbackRetryState(retry);
        }
        _errorMessage =
            error.kind == PracticeRecordingFailureKind.permissionDenied
            ? '需要麦克风权限；请在 iOS“设置”中允许 SpeakUp 使用麦克风。'
            : '暂时无法开始录音，请稍后重试。';
      }
    } catch (_) {
      if (_isOperationCurrent(fence)) {
        final retry = _speechFeedbackRetry;
        if (retry == null) {
          _recordingState = PracticeRecordingState.idle;
        } else {
          _restoreSpeechFeedbackRetryState(retry);
        }
        _errorMessage = '暂时无法开始录音，请稍后重试。';
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<void> stopRecording() {
    final speechFeedbackRetry = _speechFeedbackRetry;
    final PracticeSpeechFeedbackRetryClient? retryPractice = switch (client) {
      final PracticeSpeechFeedbackRetryClient client => client,
      _ => null,
    };
    if (speechFeedbackRetry != null &&
        retryPractice != null &&
        _recordingState == PracticeRecordingState.recording) {
      _cancelRecordingLimit();
      final fence = _captureOperationFence(
        practiceGeneration: _practiceGeneration,
        practiceSessionId: _practiceSessionId,
      );
      final operation = _stopSpeechFeedbackRetryRecording(
        practice: retryPractice,
        context: speechFeedbackRetry,
        fence: fence,
      );
      _stopRecordingFuture = operation;
      return operation.whenComplete(() {
        if (identical(_stopRecordingFuture, operation)) {
          _stopRecordingFuture = null;
        }
      });
    }
    final practice = client;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    if (sessionId == null ||
        question == null ||
        _recordingState != PracticeRecordingState.recording) {
      return Future<void>.value();
    }
    _cancelRecordingLimit();
    final fence = _captureOperationFence(
      practiceGeneration: _practiceGeneration,
      practiceSessionId: sessionId,
      questionId: question.id,
    );
    final clientTurnId = _newClientId('turn');
    final operation = _stopRecording(
      practice: practice,
      sessionId: sessionId,
      question: question,
      fence: fence,
      clientTurnId: clientTurnId,
    );
    _stopRecordingFuture = operation;
    return operation.whenComplete(() {
      if (identical(_stopRecordingFuture, operation)) {
        _stopRecordingFuture = null;
      }
    });
  }

  Future<void> finishRecordingGesture() async {
    if (_recordingState == PracticeRecordingState.starting) {
      await _recorderStartFuture;
    }
    if (_recordingState == PracticeRecordingState.recording) {
      await stopRecording();
    }
  }

  Future<void> cancelRecording() async {
    if (_recordingState != PracticeRecordingState.starting &&
        _recordingState != PracticeRecordingState.recording) {
      return;
    }
    _practiceGeneration++;
    final speechFeedbackRetry = _speechFeedbackRetry;
    _cancelRecordingLimit();
    await _recorderStartFuture;
    if (_disposed) {
      return;
    }
    await recorder.discardCurrent();
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    if (speechFeedbackRetry != null &&
        identical(_speechFeedbackRetry, speechFeedbackRetry)) {
      _restoreSpeechFeedbackRetryState(speechFeedbackRetry);
    } else {
      _recordingState = PracticeRecordingState.idle;
      _errorMessage = null;
    }
    notifyListeners();
  }

  Future<void> _stopRecording({
    required PracticeClient practice,
    required String sessionId,
    required PracticeQuestion question,
    required _PracticeOperationFence fence,
    required String clientTurnId,
  }) async {
    RecordedPracticeAudio? audio;
    _recordingState = PracticeRecordingState.transcribing;
    notifyListeners();
    try {
      final stopOperation = recorder.stop();
      _recorderStopFuture = stopOperation;
      try {
        audio = await stopOperation;
      } finally {
        if (identical(_recorderStopFuture, stopOperation)) {
          _recorderStopFuture = null;
        }
      }
      if (!_isOperationCurrent(fence)) {
        return;
      }
      final pending = _PendingPracticeAudio(
        audio: audio,
        sessionId: sessionId,
        questionId: question.id,
        clientTurnId: clientTurnId,
      );
      _pendingPracticeAudio = pending;
      audio = null;
      await _transcribePendingPracticeAudio(
        practice: practice,
        pending: pending,
        fence: fence,
      );
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        _candidate = null;
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = _transcriptionFailureMessage(error);
      }
    } finally {
      if (audio != null) {
        try {
          await recorder.discard(audio);
        } catch (_) {
          // Account cleanup retries deletion before another user can enter.
        }
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<void> _transcribePendingPracticeAudio({
    required PracticeClient practice,
    required _PendingPracticeAudio pending,
    required _PracticeOperationFence fence,
  }) async {
    var discardAudio = false;
    try {
      final candidate = await practice.transcribe(
        PracticeTranscriptionRequest(
          sessionId: pending.sessionId,
          questionId: pending.questionId,
          clientTurnId: pending.clientTurnId,
          audio: pending.audio,
        ),
      );
      if (!_isOperationCurrent(fence) ||
          !identical(_pendingPracticeAudio, pending)) {
        return;
      }
      _validateCandidate(candidate, pending.sessionId, pending.questionId);
      _candidate = candidate;
      _pendingPracticeAudio = null;
      discardAudio = true;
      _activeConfirmationId = null;
      _activeTextAnswer = null;
      _recordingState = PracticeRecordingState.awaitingConfirmation;
      _errorMessage = null;
    } catch (error) {
      if (_isOperationCurrent(fence) &&
          identical(_pendingPracticeAudio, pending)) {
        _candidate = null;
        _recordingState = PracticeRecordingState.idle;
        _errorMessage = _transcriptionFailureMessage(error);
      }
    } finally {
      if (discardAudio ||
          !_isOperationCurrent(fence) ||
          !identical(_pendingPracticeAudio, pending)) {
        if (identical(_pendingPracticeAudio, pending)) {
          _pendingPracticeAudio = null;
        }
        try {
          await recorder.discard(pending.audio);
        } catch (_) {
          // Account cleanup retries deletion before another user can enter.
        }
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  Future<void> _stopSpeechFeedbackRetryRecording({
    required PracticeSpeechFeedbackRetryClient practice,
    required _SpeechFeedbackRetryContext context,
    required _PracticeOperationFence fence,
  }) async {
    final request = context.request;
    final answerPath = request?.answerPath;
    final retryTurnId = request?.newTurnId;
    if (request == null || answerPath == null || retryTurnId == null) {
      _restoreSpeechFeedbackRetryState(context);
      _errorMessage = '同题复练草稿已失效，请重新发起。';
      notifyListeners();
      return;
    }
    RecordedPracticeAudio? audio;
    _recordingState = PracticeRecordingState.transcribing;
    notifyListeners();
    try {
      audio = await recorder.stop();
      if (!_isOperationCurrent(fence) ||
          !identical(_speechFeedbackRetry, context)) {
        return;
      }
      final candidate = await practice.transcribeRetry(
        answerPath: answerPath,
        idempotencyKey: context.transcriptionIdempotencyKey ??= _newClientId(
          'feedback-retry-audio',
        ),
        audio: audio,
      );
      if (!_isOperationCurrent(fence) ||
          !identical(_speechFeedbackRetry, context)) {
        return;
      }
      _validateSpeechFeedbackRetryCandidate(
        candidate,
        request: request,
        retryTurnId: retryTurnId,
      );
      _speechFeedbackRetryCandidate = candidate;
      _activeConfirmationId = null;
      _recordingState = PracticeRecordingState.awaitingConfirmation;
      _errorMessage = null;
    } catch (error) {
      if (_isOperationCurrent(fence) &&
          identical(_speechFeedbackRetry, context)) {
        _restoreSpeechFeedbackRetryState(context);
        _errorMessage = _transcriptionFailureMessage(error);
      }
    } finally {
      if (audio != null) {
        try {
          await recorder.discard(audio);
        } catch (_) {
          // Account cleanup retries deletion before another user can enter.
        }
      }
    }
  }

  Future<void> retryPracticeTranscription() {
    final practice = client;
    final pending = _pendingPracticeAudio;
    final question = _currentQuestion;
    final inFlight = _stopRecordingFuture;
    if (inFlight != null) {
      return inFlight;
    }
    if (pending == null ||
        question == null ||
        _disposed ||
        _recordingState != PracticeRecordingState.idle ||
        pending.sessionId != _practiceSessionId ||
        pending.questionId != question.id) {
      return Future<void>.value();
    }
    final fence = _captureOperationFence(
      practiceGeneration: _practiceGeneration,
      practiceSessionId: pending.sessionId,
      questionId: pending.questionId,
    );
    _recordingState = PracticeRecordingState.transcribing;
    _errorMessage = null;
    notifyListeners();
    final operation = _transcribePendingPracticeAudio(
      practice: practice,
      pending: pending,
      fence: fence,
    );
    _stopRecordingFuture = operation;
    return operation.whenComplete(() {
      if (identical(_stopRecordingFuture, operation)) {
        _stopRecordingFuture = null;
      }
      if (_isOperationCurrent(fence)) {
        notifyListeners();
      }
    });
  }

  Future<void> discardPendingPracticeAudio() {
    final pending = _pendingPracticeAudio;
    final inFlight = _stopRecordingFuture;
    if (inFlight != null) {
      return inFlight;
    }
    if (pending == null ||
        _disposed ||
        _recordingState != PracticeRecordingState.idle) {
      return Future<void>.value();
    }
    final fence = _captureOperationFence(
      practiceGeneration: _practiceGeneration,
      practiceSessionId: pending.sessionId,
      questionId: pending.questionId,
    );
    final operation = _discardPendingPracticeAudio(pending, fence);
    _stopRecordingFuture = operation;
    return operation.whenComplete(() {
      if (identical(_stopRecordingFuture, operation)) {
        _stopRecordingFuture = null;
      }
    });
  }

  Future<void> _discardPendingPracticeAudio(
    _PendingPracticeAudio pending,
    _PracticeOperationFence fence,
  ) async {
    try {
      await recorder.discard(pending.audio);
      if (_isOperationCurrent(fence) &&
          identical(_pendingPracticeAudio, pending)) {
        _pendingPracticeAudio = null;
        _errorMessage = null;
      }
    } catch (_) {
      if (_isOperationCurrent(fence) &&
          identical(_pendingPracticeAudio, pending)) {
        _errorMessage = '暂时无法删除本地录音，请重试。';
      }
    }
    if (_isOperationCurrent(fence)) {
      notifyListeners();
    }
  }

  void rerecord() {
    if (_recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    _practiceGeneration++;
    final speechFeedbackRetry = _speechFeedbackRetry;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    if (speechFeedbackRetry != null) {
      _restoreSpeechFeedbackRetryState(speechFeedbackRetry);
    } else {
      _recordingState = PracticeRecordingState.idle;
      _errorMessage = null;
    }
    notifyListeners();
  }

  Future<void> confirmTranscript() async {
    final speechFeedbackRetry = _speechFeedbackRetry;
    final speechFeedbackRetryCandidate = _speechFeedbackRetryCandidate;
    final PracticeSpeechFeedbackRetryClient? retryPractice = switch (client) {
      final PracticeSpeechFeedbackRetryClient client => client,
      _ => null,
    };
    if (speechFeedbackRetry != null &&
        speechFeedbackRetryCandidate != null &&
        retryPractice != null &&
        _recordingState == PracticeRecordingState.awaitingConfirmation) {
      await _confirmSpeechFeedbackRetry(
        practice: retryPractice,
        context: speechFeedbackRetry,
        candidate: speechFeedbackRetryCandidate,
      );
      return;
    }
    final practice = client;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    final candidate = _candidate;
    if (sessionId == null ||
        question == null ||
        candidate == null ||
        _isSessionCompleted ||
        _recordingState != PracticeRecordingState.awaitingConfirmation) {
      return;
    }
    final isFinalInterviewTurn =
        _turnLimit > 0 &&
        _completedTurns + 1 == _turnLimit &&
        isInterviewPracticeScene(_practiceSceneFamily, _practiceSceneModel);
    final completedTurns = _completedTurns;
    final turnLimit = _turnLimit;
    final fence = _captureOperationFence(
      practiceGeneration: _practiceGeneration,
      practiceSessionId: sessionId,
      questionId: question.id,
      candidateId: candidate.id,
    );
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return;
      }
      final confirmation = await practice.confirm(
        sessionId: sessionId,
        questionId: question.id,
        candidateId: candidate.id,
        idempotencyKey: _activeConfirmationId ??= _newClientId('confirm'),
      );
      if (!_isOperationCurrent(fence)) {
        return;
      }
      _validateConfirmation(confirmation, candidate);
      _applyPracticeConfirmation(confirmation);
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        final reconciled = isFinalInterviewTurn && _canRetry(error)
            ? await _reconcileFinalInterviewSubmission(
                practice: practice,
                fence: fence,
                expectedSessionId: sessionId,
                expectedQuestionId: question.id,
                expectedCandidateId: candidate.id,
                expectedAnswer: candidate.text,
                previousCompletedTurns: completedTurns,
                expectedTurnLimit: turnLimit,
              )
            : false;
        if (!reconciled && _isOperationCurrent(fence)) {
          _recordingState = PracticeRecordingState.awaitingConfirmation;
          _errorMessage = _confirmationFailureMessage(error);
        }
      }
    }
    if (_isCurrent(fence.epoch)) {
      notifyListeners();
    }
  }

  Future<void> _confirmSpeechFeedbackRetry({
    required PracticeSpeechFeedbackRetryClient practice,
    required _SpeechFeedbackRetryContext context,
    required RetryTranscriptionCandidate candidate,
  }) async {
    final request = context.request;
    final retryTurnId = request?.newTurnId;
    if (request == null || retryTurnId == null) {
      _restoreSpeechFeedbackRetryState(context);
      _errorMessage = '同题复练草稿已失效，请重新发起。';
      notifyListeners();
      return;
    }
    final fence = _captureOperationFence(
      practiceGeneration: _practiceGeneration,
      practiceSessionId: request.sessionId,
    );
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence) ||
          !identical(_speechFeedbackRetry, context) ||
          !identical(_speechFeedbackRetryCandidate, candidate)) {
        return;
      }
      final confirmation = await practice.confirmRetry(
        retryTurnId: retryTurnId,
        candidateId: candidate.id,
        idempotencyKey: _activeConfirmationId ??= _newClientId(
          'feedback-retry-confirm',
        ),
      );
      if (!_isOperationCurrent(fence) ||
          !identical(_speechFeedbackRetry, context) ||
          !identical(_speechFeedbackRetryCandidate, candidate)) {
        return;
      }
      _validateSpeechFeedbackRetryConfirmation(
        confirmation,
        request: request,
        candidate: candidate,
      );
      _restoreSpeechFeedbackRetryState(context);
      _speechFeedbackRetryCompletionCount++;
    } catch (error) {
      if (_isOperationCurrent(fence) &&
          identical(_speechFeedbackRetry, context)) {
        _recordingState = PracticeRecordingState.awaitingConfirmation;
        _errorMessage = _speechFeedbackRetryConfirmationFailureMessage(error);
      }
    }
    if (_isCurrent(fence.epoch)) {
      notifyListeners();
    }
  }

  Future<bool> submitPracticeText(String answerText) async {
    final practice = client;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    final text = answerText.trim();
    if (sessionId == null ||
        question == null ||
        text.isEmpty ||
        text.length > 8000 ||
        _pendingPracticeAudio != null ||
        _isSessionCompleted ||
        isBusy ||
        _recordingState != PracticeRecordingState.idle) {
      return false;
    }
    final generation = ++_practiceGeneration;
    if (_activeTextAnswer != text) {
      _activeConfirmationId = _newClientId('text-turn');
      _activeTextAnswer = text;
    }
    final isFinalInterviewTurn =
        _turnLimit > 0 &&
        _completedTurns + 1 == _turnLimit &&
        isInterviewPracticeScene(_practiceSceneFamily, _practiceSceneModel);
    final completedTurns = _completedTurns;
    final turnLimit = _turnLimit;
    final fence = _captureOperationFence(
      practiceGeneration: generation,
      practiceSessionId: sessionId,
      questionId: question.id,
    );
    _recordingState = PracticeRecordingState.submitting;
    _errorMessage = null;
    notifyListeners();
    try {
      await stopPracticeAudio();
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      final confirmation = await practice.submitText(
        sessionId: sessionId,
        questionId: question.id,
        answerText: text,
        idempotencyKey: _activeConfirmationId!,
      );
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      _validateConfirmationFields(
        confirmation,
        expectedSessionId: sessionId,
        expectedQuestionId: question.id,
        expectedAnswer: text,
      );
      _applyPracticeConfirmation(confirmation);
      _activeTextAnswer = null;
      return true;
    } catch (error) {
      if (_isOperationCurrent(fence)) {
        final reconciled = isFinalInterviewTurn && _canRetry(error)
            ? await _reconcileFinalInterviewSubmission(
                practice: practice,
                fence: fence,
                expectedSessionId: sessionId,
                expectedQuestionId: question.id,
                expectedAnswer: text,
                previousCompletedTurns: completedTurns,
                expectedTurnLimit: turnLimit,
              )
            : false;
        if (reconciled) {
          return true;
        }
        if (_isOperationCurrent(fence)) {
          _recordingState = PracticeRecordingState.idle;
          _errorMessage = _confirmationFailureMessage(error);
        }
      }
      return false;
    } finally {
      if (_isCurrent(fence.epoch)) {
        notifyListeners();
      }
    }
  }

  void _applyPracticeConfirmation(PracticeTurnConfirmation confirmation) {
    _practiceSceneFamily = confirmation.sceneFamily;
    _practiceSceneModel = confirmation.sceneModel;
    _completedTurns = confirmation.completedTurns;
    _turnLimit = confirmation.turnLimit;
    _sessionCompleted = confirmation.sessionCompleted;
    _practiceSessionVersion =
        confirmation.sessionVersion ?? _practiceSessionVersion;
    _endPracticeClientId = null;
    _currentQuestion = confirmation.nextQuestion;
    _clearQuestionTip();
    final audioAssetId = confirmation.audioAssetId;
    if (audioAssetId != null &&
        !_recordings.any(
          (recording) => recording.audioAssetId == audioAssetId,
        )) {
      _recordings = [
        ..._recordings,
        PracticeRecordingReference(
          audioAssetId: audioAssetId,
          effectiveTurn: confirmation.completedTurns,
        ),
      ];
    }
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _appendPracticeMessages([
      confirmation.answer,
      ?confirmation.nextQuestion?.presentation,
    ]);
    if (confirmation.sessionCompleted) {
      _recordingState = PracticeRecordingState.completed;
      _errorMessage = null;
    } else {
      _recordingState = PracticeRecordingState.idle;
    }
  }

  Future<PracticeQuestionTip?> requestQuestionTip() async {
    final tipClient = client;
    final sessionId = _practiceSessionId;
    final question = _currentQuestion;
    if (tipClient is! PracticeQuestionTipClient ||
        sessionId == null ||
        question == null ||
        !isInterviewPracticeScene(_practiceSceneFamily, _practiceSceneModel) ||
        _sessionCompleted ||
        _recordingState != PracticeRecordingState.idle) {
      return null;
    }
    final cached = _questionTip;
    if (cached != null &&
        cached.sessionId == sessionId &&
        cached.questionId == question.id) {
      return cached;
    }
    if (_questionTipLoading) {
      return null;
    }
    final generation = ++_questionTipGeneration;
    _questionTipLoading = true;
    _questionTipErrorMessage = null;
    _questionTipRequestId ??= _newClientId('question-tip');
    notifyListeners();
    try {
      final tip = await tipClient.ensureQuestionTip(
        sessionId: sessionId,
        questionId: question.id,
        idempotencyKey: _questionTipRequestId!,
      );
      if (_disposed ||
          generation != _questionTipGeneration ||
          _practiceSessionId != sessionId ||
          _currentQuestion?.id != question.id) {
        return null;
      }
      if (tip.sessionId != sessionId ||
          tip.questionId != question.id ||
          tip.content.trim().isEmpty) {
        throw StateError('Invalid question Tip response.');
      }
      _questionTip = tip;
      _questionTipRequestId = null;
      return tip;
    } catch (_) {
      if (!_disposed &&
          generation == _questionTipGeneration &&
          _practiceSessionId == sessionId &&
          _currentQuestion?.id == question.id) {
        _questionTipErrorMessage = '暂时无法生成参考答案，请稍后重试。';
      }
      return null;
    } finally {
      if (!_disposed && generation == _questionTipGeneration) {
        _questionTipLoading = false;
        notifyListeners();
      }
    }
  }

  void _clearQuestionTip() {
    _questionTipGeneration++;
    _questionTip = null;
    _questionTipRequestId = null;
    _questionTipErrorMessage = null;
    _questionTipLoading = false;
  }

  Future<bool> _reconcileFinalInterviewSubmission({
    required PracticeClient practice,
    required _PracticeOperationFence fence,
    required String expectedSessionId,
    required String expectedQuestionId,
    required String expectedAnswer,
    required int previousCompletedTurns,
    required int expectedTurnLimit,
    String? expectedCandidateId,
  }) async {
    try {
      final snapshot = await practice.restorePractice(
        sessionId: expectedSessionId,
      );
      if (!_isOperationCurrent(fence)) {
        return false;
      }
      final currentTurn = snapshot.currentTurn;
      if (snapshot.sessionId != expectedSessionId ||
          snapshot.sceneFamily != _practiceSceneFamily ||
          snapshot.sceneModel != _practiceSceneModel ||
          !snapshot.sessionCompleted ||
          snapshot.completedTurns != previousCompletedTurns + 1 ||
          snapshot.completedTurns != expectedTurnLimit ||
          snapshot.turnLimit != expectedTurnLimit ||
          currentTurn == null ||
          currentTurn.sessionId != expectedSessionId ||
          currentTurn.questionId != expectedQuestionId ||
          currentTurn.answerText != expectedAnswer ||
          currentTurn.effectiveTurns != expectedTurnLimit ||
          !currentTurn.sessionCompleted ||
          (expectedCandidateId != null &&
              currentTurn.candidateId != expectedCandidateId)) {
        return false;
      }
      _applyPracticeSnapshot(snapshot, preserveKnownRecordings: true);
      _errorMessage = null;
      return true;
    } on Object {
      return false;
    }
  }

  /// Invalidates private UI state synchronously, then removes temporary audio
  /// and waits for all account-scoped transports to stop.
  Future<void> clearPrivateState() async {
    _cancelRecordingLimit();
    _epoch++;
    _practiceGeneration++;
    _pendingPracticeAudio = null;
    _practiceSessionId = null;
    _practiceSceneFamily = null;
    _practiceSceneModel = null;
    _practiceSessionVersion = null;
    _endPracticeClientId = null;
    _currentQuestion = null;
    _clearQuestionTip();
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _speechFeedbackRetry = null;
    _speechFeedbackRetryCandidate = null;
    _speechFeedbackRetryCompletionCount = 0;
    _practiceMessages = const <PracticeMessage>[];
    _recordingState = PracticeRecordingState.idle;
    _errorMessage = null;
    _completedTurns = 0;
    _turnLimit = 0;
    _sessionCompleted = false;
    _recordings = const <PracticeRecordingReference>[];
    _playingMediaKey = null;
    _loadingMediaKey = null;
    _deletingAudioAssetId = null;
    _mediaErrorMessage = null;
    _mediaGeneration++;
    _busy = false;
    if (!_disposed) {
      notifyListeners();
    }

    final cleanup = Future.wait<void>([
      Future<void>.sync(client.clearAccountState),
      Future<void>.sync(() async {
        await _recorderStartFuture;
        await _stopRecordingFuture;
        await recorder.clearAccountState();
      }),
      if (mediaClient case final media?)
        Future<void>.sync(media.clearAccountState),
      if (audioPlayer case final player?)
        Future<void>.sync(player.clearAccountState),
    ]);
    _accountCleanupFuture = cleanup;
    try {
      await cleanup;
    } finally {
      await _mediaOperation;
      // A second strict cleanup pass fences a late native play completion.
      // Failure must remain visible to Auth so account switching stays closed.
      await audioPlayer?.clearAccountState();
      if (identical(_accountCleanupFuture, cleanup)) {
        _accountCleanupFuture = null;
      }
    }
  }

  @override
  void dispose() {
    final pendingPracticeAudio = _pendingPracticeAudio;
    final practiceAudioOperation = _stopRecordingFuture;
    _pendingPracticeAudio = null;
    _disposed = true;
    if (mediaClient != null) {
      WidgetsBinding.instance.removeObserver(this);
    }
    _cancelRecordingLimit();
    _epoch++;
    _practiceGeneration++;
    _mediaGeneration++;
    _speechFeedbackRetry = null;
    _speechFeedbackRetryCandidate = null;
    unawaited(
      Future<void>.sync(() async {
        await _recorderStartFuture;
        await practiceAudioOperation;
        if (practiceAudioOperation == null && pendingPracticeAudio != null) {
          try {
            await recorder.discard(pendingPracticeAudio.audio);
          } catch (_) {
            // The strict account cleanup below retries all managed recordings.
          }
        }
        await recorder.clearAccountState();
      }),
    );
    unawaited(_mediaCompletionSubscription?.cancel());
    unawaited(mediaClient?.dispose());
    unawaited(audioPlayer?.dispose());
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed ||
        _disposed ||
        !supportsPracticeMedia ||
        (_playingMediaKey == null &&
            _loadingMediaKey == null &&
            _mediaOperation == null)) {
      return;
    }
    unawaited(stopPracticeAudio());
  }

  void _applyPracticeSnapshot(
    PracticeSessionSnapshot? snapshot, {
    bool preserveKnownRecordings = false,
  }) {
    if (_pendingPracticeAudio != null) {
      throw StateError(
        'Pending Practice audio must be resolved before replacing its snapshot.',
      );
    }
    final previousSessionId = _practiceSessionId;
    _cancelRecordingLimit();
    _practiceGeneration++;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
    _speechFeedbackRetry = null;
    _speechFeedbackRetryCandidate = null;
    _playingMediaKey = null;
    _loadingMediaKey = null;
    _deletingAudioAssetId = null;
    _mediaErrorMessage = null;
    _mediaGeneration++;
    unawaited(audioPlayer?.stop());
    if (snapshot == null) {
      _practiceSessionId = null;
      _practiceSceneFamily = null;
      _practiceSceneModel = null;
      _practiceSessionVersion = null;
      _endPracticeClientId = null;
      _currentQuestion = null;
      _clearQuestionTip();
      _activeScene = null;
      _completedTurns = 0;
      _turnLimit = 0;
      _sessionCompleted = false;
      _recordings = const <PracticeRecordingReference>[];
      _practiceMessages = const <PracticeMessage>[];
      _recordingState = PracticeRecordingState.idle;
      return;
    }
    _validatePracticeSnapshot(snapshot);
    if (snapshot.sessionId != previousSessionId) {
      _practiceMessages = const <PracticeMessage>[];
    }
    final mayPreserveKnownRecordings =
        preserveKnownRecordings && snapshot.sessionId == _practiceSessionId;
    _practiceSessionId = snapshot.sessionId;
    _practiceSceneFamily = snapshot.sceneFamily;
    _practiceSceneModel = snapshot.sceneModel;
    _practiceSessionVersion = snapshot.sessionVersion;
    _endPracticeClientId = null;
    _currentQuestion = snapshot.currentQuestion;
    _clearQuestionTip();
    _completedTurns = snapshot.completedTurns;
    _turnLimit = snapshot.turnLimit;
    _sessionCompleted = snapshot.sessionCompleted;
    final currentTurn = snapshot.currentTurn;
    final audioAssetId = currentTurn?.audioAssetId;
    if (!mayPreserveKnownRecordings) {
      _recordings = <PracticeRecordingReference>[
        for (final exchange in snapshot.turnHistory)
          if (exchange.turn.audioAssetId case final assetId?)
            PracticeRecordingReference(
              audioAssetId: assetId,
              effectiveTurn: exchange.turn.effectiveTurns,
            ),
        if (snapshot.turnHistory.isEmpty && audioAssetId != null)
          PracticeRecordingReference(
            audioAssetId: audioAssetId,
            effectiveTurn: currentTurn!.effectiveTurns,
          ),
      ];
    }
    final currentQuestion = snapshot.currentQuestion?.presentation;
    if (snapshot.turnHistory.isNotEmpty) {
      _practiceMessages = <PracticeMessage>[
        for (final exchange in snapshot.turnHistory) ...[
          exchange.question.presentation,
          PracticeMessage(
            id: exchange.turn.id,
            role: PracticeMessageRole.user,
            text: exchange.turn.answerText,
            speechFeedbackStatusUrl: exchange.turn.speechFeedbackStatusUrl,
          ),
        ],
        ?currentQuestion,
      ];
    } else {
      _appendPracticeMessages([?currentQuestion]);
    }
    _recordingState = snapshot.sessionCompleted
        ? PracticeRecordingState.completed
        : PracticeRecordingState.idle;
  }

  void _appendPracticeMessages(Iterable<PracticeMessage> values) {
    final messages = List<PracticeMessage>.from(_practiceMessages);
    final ids = {for (final message in messages) message.id};
    for (final message in values) {
      if (ids.add(message.id)) {
        messages.add(message);
      }
    }
    _practiceMessages = messages;
  }

  bool _isCurrent(int epoch) => !_disposed && epoch == _epoch;

  _PracticeOperationFence _captureOperationFence({
    int? practiceGeneration,
    String? practiceSessionId,
    String? questionId,
    String? candidateId,
    String? questionSpeechPath,
    String? recordingAudioAssetId,
  }) {
    return _PracticeOperationFence(
      epoch: _epoch,
      practiceGeneration: practiceGeneration,
      practiceSessionId: practiceSessionId,
      questionId: questionId,
      candidateId: candidateId,
      questionSpeechPath: questionSpeechPath,
      recordingAudioAssetId: recordingAudioAssetId,
    );
  }

  bool _isOperationCurrent(_PracticeOperationFence fence) {
    return _isCurrent(fence.epoch) &&
        (fence.practiceGeneration == null ||
            fence.practiceGeneration == _practiceGeneration) &&
        (fence.practiceSessionId == null ||
            fence.practiceSessionId == _practiceSessionId) &&
        (fence.questionId == null ||
            fence.questionId == _currentQuestion?.id) &&
        (fence.candidateId == null || fence.candidateId == _candidate?.id) &&
        (fence.questionSpeechPath == null ||
            fence.questionSpeechPath == _currentQuestion?.speechPath) &&
        (fence.recordingAudioAssetId == null ||
            _recordings.any(
              (recording) =>
                  recording.audioAssetId == fence.recordingAudioAssetId,
            ));
  }

  bool _isCurrentMedia(int generation) {
    return !_disposed && generation == _mediaGeneration;
  }

  void _handleMediaCompletion() {
    if (_disposed || _playingMediaKey == null) {
      return;
    }
    _playingMediaKey = null;
    notifyListeners();
  }

  Future<void> _clearPlayerAfterAuthenticationFailure() async {
    _mediaGeneration++;
    _playingMediaKey = null;
    _loadingMediaKey = null;
    _deletingAudioAssetId = null;
    try {
      await audioPlayer?.clearAccountState();
    } catch (_) {
      // Authentication invalidation will repeat private-state cleanup.
    }
    if (!_disposed) {
      notifyListeners();
    }
  }

  String _mediaFailureMessage(Object error, {required String action}) {
    if (error is PracticeClientException) {
      if (error.kind == PracticeClientFailureKind.notFound) {
        return '$action失败：录音不存在或已删除。';
      }
      if (error.kind == PracticeClientFailureKind.authenticationRequired) {
        return '登录状态已失效，请重新登录。';
      }
      if (error.kind == PracticeClientFailureKind.network) {
        return '$action失败：请检查网络后重试。';
      }
      if (error.kind == PracticeClientFailureKind.rateLimited) {
        return '$action过于频繁，请稍后重试。';
      }
    }
    return '$action暂时不可用，请稍后重试。';
  }

  void _cancelRecordingLimit() {
    _recordingLimitTimer?.cancel();
    _recordingLimitTimer = null;
  }

  String _transcriptionFailureMessage(Object error) {
    if (error is PracticeClientException) {
      if (_isFreeQuotaExhausted(error)) {
        return '今日免费语音额度已用完，录音已保留，本轮未计入进度。';
      }
      if (error.kind == PracticeClientFailureKind.network) {
        return '网络连接不稳定，录音已保留；可重试转写，或删除后请重新录音。';
      }
      if (error.kind == PracticeClientFailureKind.rateLimited) {
        return '语音请求过于频繁，录音已保留；请稍后重试转写。';
      }
    }
    return '没有识别出这一轮，录音已保留；可重试转写或删除。';
  }

  String _confirmationFailureMessage(Object error) {
    if (error is PracticeClientException) {
      if (_isFreeQuotaExhausted(error)) {
        return '今日免费练习额度已用完，这一轮尚未确认。';
      }
      if (error.kind == PracticeClientFailureKind.conflict &&
          error.errorCode == 'resource_conflict' &&
          !error.retryable) {
        return '录音已失效，请重新录音。';
      }
      if (error.kind == PracticeClientFailureKind.network) {
        return '网络连接不稳定，这一轮尚未确认，请重试。';
      }
      if (error.kind == PracticeClientFailureKind.rateLimited) {
        return '提交过于频繁，请稍后重试；转写内容已保留。';
      }
    }
    return '这一轮没有提交成功，请重试。';
  }

  String _speechFeedbackRetryFailureMessage(Object error) {
    if (error case _SpeechFeedbackRetryCreationException(:final failure)) {
      if (failure?.reason ==
          PracticeRetryFailureReason.sourceNoLongerAvailable) {
        return '原题已经不可复练，请选择其他反馈继续练习。';
      }
      return '同题复练暂时无法准备，请重试。';
    }
    if (error is PracticeClientException) {
      if (error.kind == PracticeClientFailureKind.authenticationRequired) {
        return '登录状态已失效，请重新登录。';
      }
      if (error.kind == PracticeClientFailureKind.notFound) {
        return '这条反馈或原题已不可用。';
      }
      if (error.kind == PracticeClientFailureKind.conflict) {
        return '原题当前无法复练，请刷新后重试。';
      }
      if (error.kind == PracticeClientFailureKind.network) {
        return '网络连接不稳定，未能准备同题复练。';
      }
      if (error.kind == PracticeClientFailureKind.rateLimited) {
        return '同题复练请求过于频繁，请稍后重试。';
      }
    }
    return '同题复练暂时无法准备，请重试。';
  }

  String _speechFeedbackRetryConfirmationFailureMessage(Object error) {
    if (error is PracticeClientException) {
      if (error.kind == PracticeClientFailureKind.authenticationRequired) {
        return '登录状态已失效，请重新登录。';
      }
      if (error.kind == PracticeClientFailureKind.conflict &&
          !error.retryable) {
        return '这次复练录音已失效，请重新发起。';
      }
      if (error.kind == PracticeClientFailureKind.network) {
        return '网络连接不稳定，复练尚未提交，请重试。';
      }
      if (error.kind == PracticeClientFailureKind.rateLimited) {
        return '提交过于频繁，请稍后重试；转写内容已保留。';
      }
    }
    return '同题复练没有提交成功，请重试。';
  }

  bool _isFreeQuotaExhausted(PracticeClientException error) {
    return error.errorCode == 'quota_exhausted';
  }

  void _setBusy(bool value) {
    if (_disposed) {
      return;
    }
    _busy = value;
    notifyListeners();
  }

  void _validatePracticeSnapshot(PracticeSessionSnapshot snapshot) {
    if (snapshot.sessionId.trim().isEmpty ||
        snapshot.planId.trim().isEmpty ||
        !validPracticeSceneIdentity(
          snapshot.sceneFamily,
          snapshot.sceneModel,
        ) ||
        snapshot.completedTurns < 0 ||
        snapshot.turnLimit < 1 ||
        snapshot.turnLimit > 14 ||
        snapshot.completedTurns > snapshot.turnLimit ||
        snapshot.sessionVersion < 1 ||
        (!snapshot.sessionCompleted && snapshot.currentQuestion == null) ||
        (snapshot.currentQuestion != null &&
            snapshot.currentQuestion!.sessionId != snapshot.sessionId) ||
        (snapshot.currentTurn?.audioAssetId != null &&
            (snapshot.currentTurn!.audioAssetId!.trim().isEmpty ||
                snapshot.currentTurn!.audioAssetId!.length > 128 ||
                snapshot.currentTurn!.audioAssetId!.trim() !=
                    snapshot.currentTurn!.audioAssetId))) {
      throw StateError('Invalid Practice Session snapshot.');
    }
  }

  void _validateCandidate(
    TranscriptionCandidate candidate,
    String sessionId,
    String questionId,
  ) {
    if (candidate.id.trim().isEmpty ||
        candidate.text.trim().isEmpty ||
        candidate.sessionId != sessionId ||
        candidate.questionId != questionId) {
      throw StateError('Invalid transcription candidate.');
    }
  }

  void _validateSpeechFeedbackRetryRequest(
    PracticeRetryRequest request, {
    required _SpeechFeedbackRetryContext context,
    required String sessionId,
  }) {
    context.retryRequestId ??= request.retryRequestId;
    final expectedStatusUrl = '/v1/retry-requests/${request.retryRequestId}';
    final expectedAnswerPath = request.newTurnId == null
        ? null
        : '/v1/retry-turns/${request.newTurnId}/transcription-candidates';
    final validStatusShape = switch (request.retryStatus) {
      PracticeRetryRequestStatus.pending =>
        request.newTurnId == null &&
            request.answerPath == null &&
            request.stableFailure == null &&
            request.completedAt == null,
      PracticeRetryRequestStatus.turnCreated =>
        request.newTurnId != null &&
            request.answerPath == expectedAnswerPath &&
            request.stableFailure == null &&
            request.completedAt != null,
      PracticeRetryRequestStatus.failed =>
        request.newTurnId == null &&
            request.answerPath == null &&
            request.stableFailure != null &&
            request.completedAt != null,
    };
    if (request.retryRequestId.trim().isEmpty ||
        request.retryRequestId != context.retryRequestId ||
        request.feedbackItemId != context.feedbackItemId ||
        request.sessionId != sessionId ||
        request.originalTurnId != context.originalTurnId ||
        request.questionId.trim().isEmpty ||
        request.statusUrl != expectedStatusUrl ||
        request.updatedAt.isBefore(request.createdAt) ||
        (request.completedAt != null &&
            request.completedAt!.isBefore(request.createdAt)) ||
        !validStatusShape) {
      throw StateError('Invalid same-question retry request.');
    }
  }

  void _validateSpeechFeedbackRetryCandidate(
    RetryTranscriptionCandidate candidate, {
    required PracticeRetryRequest request,
    required String retryTurnId,
  }) {
    if (candidate.id.trim().isEmpty ||
        candidate.retryTurnId != retryTurnId ||
        candidate.retryRequestId != request.retryRequestId ||
        candidate.sessionId != request.sessionId ||
        candidate.questionId != request.questionId ||
        candidate.respondentParticipantId.trim().isEmpty ||
        candidate.transcriptId.trim().isEmpty ||
        candidate.evidenceVersion < 1 ||
        candidate.text.trim().isEmpty) {
      throw StateError('Invalid retry transcription candidate.');
    }
  }

  void _validateSpeechFeedbackRetryConfirmation(
    ConfirmedRetryTurn confirmation, {
    required PracticeRetryRequest request,
    required RetryTranscriptionCandidate candidate,
  }) {
    if (confirmation.turnId != request.newTurnId ||
        confirmation.retryRequestId != request.retryRequestId ||
        confirmation.originalTurnId != request.originalTurnId ||
        confirmation.sessionId != request.sessionId ||
        confirmation.questionId != request.questionId ||
        confirmation.respondentParticipantId !=
            candidate.respondentParticipantId ||
        confirmation.candidateId != candidate.id ||
        confirmation.answerText != candidate.text ||
        confirmation.evidenceVersion != candidate.evidenceVersion ||
        confirmation.countsTowardTurnLimit ||
        confirmation.confirmedAt.isBefore(confirmation.createdAt)) {
      throw StateError('Invalid retry Turn confirmation.');
    }
  }

  void _restoreSpeechFeedbackRetryState(_SpeechFeedbackRetryContext context) {
    if (!identical(_speechFeedbackRetry, context)) {
      return;
    }
    _recordingState = context.returnState;
    _errorMessage = context.returnErrorMessage;
    _speechFeedbackRetry = null;
    _speechFeedbackRetryCandidate = null;
    _candidate = null;
    _activeConfirmationId = null;
    _activeTextAnswer = null;
  }

  void _validateConfirmation(
    PracticeTurnConfirmation confirmation,
    TranscriptionCandidate candidate,
  ) {
    _validateConfirmationFields(
      confirmation,
      expectedSessionId: candidate.sessionId,
      expectedQuestionId: candidate.questionId,
      expectedCandidateId: candidate.id,
      expectedAnswer: candidate.text,
    );
  }

  void _validateConfirmationFields(
    PracticeTurnConfirmation confirmation, {
    required String expectedSessionId,
    required String expectedQuestionId,
    String? expectedCandidateId,
    required String expectedAnswer,
  }) {
    if (confirmation.turnId.trim().isEmpty ||
        confirmation.sceneFamily != _practiceSceneFamily ||
        confirmation.sceneModel != _practiceSceneModel ||
        confirmation.candidateId.trim().isEmpty ||
        confirmation.sessionId != expectedSessionId ||
        confirmation.questionId != expectedQuestionId ||
        (expectedCandidateId != null &&
            confirmation.candidateId != expectedCandidateId) ||
        confirmation.answer.text != expectedAnswer ||
        confirmation.completedTurns < 1 ||
        confirmation.turnLimit < 1 ||
        confirmation.turnLimit > 14 ||
        confirmation.completedTurns > confirmation.turnLimit ||
        (confirmation.sessionVersion != null &&
            confirmation.sessionVersion! < 1) ||
        (!confirmation.sessionCompleted && confirmation.nextQuestion == null) ||
        (confirmation.nextQuestion != null &&
            confirmation.nextQuestion!.sessionId != confirmation.sessionId) ||
        (confirmation.audioAssetId != null &&
            (confirmation.audioAssetId!.trim().isEmpty ||
                confirmation.audioAssetId!.length > 128 ||
                confirmation.audioAssetId!.trim() !=
                    confirmation.audioAssetId))) {
      throw StateError('Invalid Practice Turn confirmation.');
    }
  }

  String _newClientId(String scope) {
    final value = _clientIdFactory(scope);
    if (value.isEmpty) {
      throw StateError('Agent client identity must not be empty.');
    }
    return value;
  }

  bool _canRetry(Object error) {
    return error is! PracticeClientException || error.retryable;
  }
}

final class _PracticeOperationFence {
  const _PracticeOperationFence({
    required this.epoch,
    this.practiceGeneration,
    this.practiceSessionId,
    this.questionId,
    this.candidateId,
    this.questionSpeechPath,
    this.recordingAudioAssetId,
  });

  final int epoch;
  final int? practiceGeneration;
  final String? practiceSessionId;
  final String? questionId;
  final String? candidateId;
  final String? questionSpeechPath;
  final String? recordingAudioAssetId;
}

final class _PendingPracticeAudio {
  const _PendingPracticeAudio({
    required this.audio,
    required this.sessionId,
    required this.questionId,
    required this.clientTurnId,
  });

  final RecordedPracticeAudio audio;
  final String sessionId;
  final String questionId;
  final String clientTurnId;
}

final class _SpeechFeedbackRetryContext {
  _SpeechFeedbackRetryContext({
    required this.feedbackItemId,
    required this.originalTurnId,
    required this.returnState,
    required this.returnErrorMessage,
    required this.requestIdempotencyKey,
  });

  final String feedbackItemId;
  final String originalTurnId;
  final PracticeRecordingState returnState;
  final String? returnErrorMessage;
  final String requestIdempotencyKey;
  PracticeRetryRequest? request;
  String? retryRequestId;
  String? transcriptionIdempotencyKey;
}

final class _SpeechFeedbackRetryCreationException implements Exception {
  const _SpeechFeedbackRetryCreationException(this.failure);

  final PracticeRetryFailure? failure;
}

final Random _clientIdRandom = Random.secure();

String _createSecureClientId(String scope) {
  final buffer = StringBuffer('${scope}_');
  for (var index = 0; index < 16; index++) {
    buffer.write(
      _clientIdRandom.nextInt(256).toRadixString(16).padLeft(2, '0'),
    );
  }
  return buffer.toString();
}

String? _questionMediaKey(String? questionId) {
  return questionId == null ? null : 'question:$questionId';
}

String _recordingMediaKey(String audioAssetId) {
  return 'recording:$audioAssetId';
}
