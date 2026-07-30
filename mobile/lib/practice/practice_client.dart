import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/practice/practice_models.dart';
import 'package:speakup/practice/practice_recording.dart';

abstract interface class PracticeClient {
  Future<void> clearAccountState();

  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  });

  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  });

  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  );

  /// Confirms candidate text as one effective Turn.
  ///
  /// The authenticated server resolves the respondent for the canonical
  /// `speakup.user` subject namespace. Flutter never submits a participant ID.
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  });

  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  });
}

abstract interface class PracticeLifecycleClient {
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  });
}

/// Optional Daily/Workplace same-question retry capability.
///
/// Keeping this separate from [PracticeClient] lets existing test doubles and
/// non-feedback practice clients remain unchanged.
abstract interface class PracticeSpeechFeedbackRetryClient {
  Future<PracticeRetryRequest> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
  });

  Future<PracticeRetryRequest> getSameQuestionRetryRequest({
    required String retryRequestId,
  });

  Future<RetryTranscriptionCandidate> transcribeRetry({
    required String answerPath,
    required String idempotencyKey,
    required RecordedPracticeAudio audio,
  });

  Future<ConfirmedRetryTurn> confirmRetry({
    required String retryTurnId,
    required String candidateId,
    required String idempotencyKey,
  });
}

/// Compatibility adapter for explicit Fake previews and pre-#87 test doubles.
///
/// Production composition injects [WirePracticeClient]. This adapter keeps the
/// old Fake surface usable without letting its Thread-shaped contract leak
/// into the production Controller state.
final class LegacyAgentPracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  LegacyAgentPracticeClient(this._client);

  final AgentClient _client;
  AgentThreadSnapshot? _restoredThread;
  PracticeSessionSnapshot? _snapshot;
  String? _threadId;
  TranscriptionCandidate? _candidate;
  String? _clientTurnId;
  String? _pendingReviewClientId;
  AgentSceneStart? _sceneSelection;

  void seedRestoredThread(AgentThreadSnapshot snapshot) {
    _restoredThread = snapshot;
  }

  void seedSceneSelection(AgentSceneStart selection) {
    _sceneSelection = selection;
  }

  @override
  Future<void> clearAccountState() async {
    _restoredThread = null;
    _snapshot = null;
    _threadId = null;
    _candidate = null;
    _clientTurnId = null;
    _pendingReviewClientId = null;
    _sceneSelection = null;
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    final restored = _restoredThread;
    _restoredThread = null;
    if (restored == null || restored.threadId != threadId) {
      final snapshot = _snapshot;
      final pendingReviewClientId = _pendingReviewClientId;
      if (snapshot != null &&
          snapshot.sessionCompleted &&
          snapshot.review == null &&
          pendingReviewClientId != null) {
        final review = await _client.createReview(
          threadId: threadId,
          scene: snapshot.matter.scene,
          clientReviewId: pendingReviewClientId,
        );
        return _snapshot = PracticeSessionSnapshot(
          sessionId: snapshot.sessionId,
          sessionVersion: snapshot.sessionVersion,
          matter: snapshot.matter,
          completedTurns: snapshot.completedTurns,
          turnLimit: snapshot.turnLimit,
          sessionCompleted: true,
          review: review,
        );
      }
      return _snapshot;
    }
    _threadId = threadId;
    final legacy = restored.practice;
    final matter = restored.activeMatter;
    if (legacy == null || matter == null) {
      return null;
    }
    if (legacy.completedTurns < 0 ||
        legacy.turnLimit < 1 ||
        legacy.turnLimit > 14 ||
        legacy.completedTurns > legacy.turnLimit ||
        (legacy.review != null && !legacy.sessionCompleted) ||
        (legacy.pendingReviewClientId != null &&
            (legacy.pendingReviewClientId!.trim().isEmpty ||
                !legacy.sessionCompleted ||
                legacy.review != null)) ||
        (legacy.sessionCompleted &&
            legacy.review == null &&
            legacy.pendingReviewClientId == null)) {
      throw StateError('Invalid legacy Practice snapshot.');
    }
    var review = legacy.review;
    _pendingReviewClientId = legacy.pendingReviewClientId;
    final completed = legacy.sessionCompleted;
    final question = completed
        ? null
        : PracticeQuestion(
            id: 'legacy-question-${matter.id}-${legacy.completedTurns + 1}',
            sessionId: 'legacy-session-${matter.id}',
            text:
                restored.messages
                    .where(
                      (message) => message.role == AgentMessageRole.assistant,
                    )
                    .lastOrNull
                    ?.text ??
                '继续完成当前练习。',
          );
    return _snapshot = PracticeSessionSnapshot(
      sessionId: 'legacy-session-${matter.id}',
      sessionVersion: legacy.completedTurns + 1,
      matter: matter,
      completedTurns: legacy.completedTurns,
      turnLimit: legacy.turnLimit,
      sessionCompleted: completed,
      currentQuestion: question,
      review: review,
    );
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    _threadId = threadId;
    final selection = _sceneSelection;
    _sceneSelection = null;
    final sessionId = 'legacy-session-${activeMatter.id}';
    return PracticeStartResult(
      snapshot: _snapshot = PracticeSessionSnapshot(
        sessionId: sessionId,
        sessionVersion: 1,
        matter: activeMatter,
        completedTurns: 0,
        turnLimit: 3,
        sessionCompleted: false,
        currentQuestion: PracticeQuestion(
          id:
              selection?.assistantMessage.id ??
              'legacy-question-${activeMatter.id}-1',
          sessionId: sessionId,
          text: selection?.assistantMessage.text ?? '第一轮：请先用英文回答。',
        ),
      ),
    );
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    final snapshot = _snapshot;
    final threadId = _threadId;
    if (snapshot == null || threadId == null) {
      throw StateError('Legacy practice is not active.');
    }
    _clientTurnId = request.clientTurnId;
    final transcript = await _client.transcribeTurn(
      threadId: threadId,
      turnNumber: snapshot.completedTurns + 1,
      clientTurnId: request.clientTurnId,
    );
    return _candidate = TranscriptionCandidate(
      id: 'legacy-candidate-${request.clientTurnId}',
      sessionId: request.sessionId,
      questionId: request.questionId,
      text: transcript,
    );
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    final snapshot = _snapshot;
    final candidate = _candidate;
    final clientTurnId = _clientTurnId;
    final threadId = _threadId;
    if (snapshot == null ||
        candidate == null ||
        clientTurnId == null ||
        threadId == null) {
      throw StateError('Legacy candidate is not active.');
    }
    final turnNumber = snapshot.completedTurns + 1;
    final exchange = await _client.submitPracticeTurn(
      threadId: threadId,
      scene: snapshot.matter.scene,
      turnNumber: turnNumber,
      transcript: candidate.text,
      clientTurnId: clientTurnId,
    );
    final completed = turnNumber == snapshot.turnLimit;
    final nextQuestion = completed
        ? null
        : PracticeQuestion(
            id:
                exchange.assistantMessage?.id ??
                'legacy-question-${snapshot.matter.id}-${turnNumber + 1}',
            sessionId: sessionId,
            text: exchange.assistantMessage?.text ?? '继续下一轮。',
          );
    AgentReview? review;
    if (completed) {
      _pendingReviewClientId ??= 'legacy-review-$idempotencyKey';
      try {
        review = await _client.createReview(
          threadId: threadId,
          scene: snapshot.matter.scene,
          clientReviewId: _pendingReviewClientId!,
        );
      } catch (_) {
        // Legacy Fake only: production confirmation owns Review creation.
      }
    }
    _snapshot = PracticeSessionSnapshot(
      sessionId: sessionId,
      sessionVersion: (snapshot.sessionVersion ?? 1) + 1,
      matter: snapshot.matter,
      completedTurns: turnNumber,
      turnLimit: snapshot.turnLimit,
      sessionCompleted: completed,
      currentQuestion: nextQuestion,
      review: review,
    );
    return PracticeTurnConfirmation(
      turnId: exchange.userMessage.id,
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      answer: exchange.userMessage,
      completedTurns: turnNumber,
      turnLimit: snapshot.turnLimit,
      sessionCompleted: completed,
      sessionVersion: _snapshot!.sessionVersion,
      nextQuestion: nextQuestion,
      review: review,
    );
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    final text = answerText.trim();
    if (text.isEmpty) {
      throw ArgumentError.value(answerText, 'answerText');
    }
    _clientTurnId = idempotencyKey;
    _candidate = TranscriptionCandidate(
      id: 'legacy-text-$idempotencyKey',
      sessionId: sessionId,
      questionId: questionId,
      text: text,
    );
    return confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: _candidate!.id,
      idempotencyKey: idempotencyKey,
    );
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    final snapshot = _snapshot;
    if (snapshot == null ||
        snapshot.sessionId != sessionId ||
        snapshot.sessionVersion != expectedSessionVersion ||
        idempotencyKey.trim().isEmpty) {
      throw StateError('Legacy practice cannot be ended.');
    }
    _snapshot = null;
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.endedEarly,
      version: expectedSessionVersion + 1,
    );
  }
}

final class FakePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  FakePracticeClient({this.delay = Duration.zero});

  final Duration delay;
  int _generation = 0;
  int _messageSequence = 0;
  PracticeSessionSnapshot? _snapshot;
  final Map<String, TranscriptionCandidate> _candidates = {};
  final Map<String, PracticeTurnConfirmation> _confirmations = {};

  @override
  Future<void> clearAccountState() async {
    _generation++;
    _snapshot = null;
    _candidates.clear();
    _confirmations.clear();
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) async {
    await _wait(_generation);
    return _snapshot;
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) async {
    final generation = _generation;
    await _wait(generation);
    final scene = activeMatter.scene;
    final sessionId = 'practice-session-$generation-${scene.id}';
    final snapshot = PracticeSessionSnapshot(
      sessionId: sessionId,
      sessionVersion: 1,
      matter: activeMatter,
      completedTurns: 0,
      turnLimit: 3,
      sessionCompleted: false,
      currentQuestion: PracticeQuestion(
        id: 'question-$generation-1',
        sessionId: sessionId,
        text: '第一轮：请先用英文回答，你希望面试官首先了解你的哪段经历？',
      ),
    );
    _snapshot = snapshot;
    return PracticeStartResult(snapshot: snapshot);
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) async {
    final generation = _generation;
    await _wait(generation);
    final key = '${request.sessionId}\u0000${request.clientTurnId}';
    return _candidates.putIfAbsent(
      key,
      () => TranscriptionCandidate(
        id: 'candidate-${request.clientTurnId}',
        sessionId: request.sessionId,
        questionId: request.questionId,
        text: switch (_snapshot?.completedTurns ?? 0) {
          0 =>
            'I led the backend migration and kept the rollout safe with staged checks.',
          1 =>
            'The main trade-off was delivery speed versus reliability, so I reduced the scope first.',
          _ =>
            'The result was a stable release, and I learned to communicate risks much earlier.',
        },
      ),
    );
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) async {
    final generation = _generation;
    await _wait(generation);
    final snapshot = _snapshot;
    final candidate = _candidates.values
        .where((item) => item.id == candidateId)
        .firstOrNull;
    if (snapshot == null ||
        candidate == null ||
        snapshot.sessionId != sessionId ||
        candidate.questionId != questionId) {
      throw StateError('Unknown Fake practice candidate.');
    }
    final key = '$sessionId\u0000$idempotencyKey';
    final existing = _confirmations[key];
    if (existing != null) {
      return existing;
    }
    final completedTurns = snapshot.completedTurns + 1;
    final completed = completedTurns == snapshot.turnLimit;
    final nextQuestion = completed
        ? null
        : PracticeQuestion(
            id: 'question-$generation-${completedTurns + 1}',
            sessionId: sessionId,
            text: switch (completedTurns) {
              1 => '第二轮：当时最困难的取舍是什么？请说明你为什么这样决定。',
              _ => '第三轮：结果如何？如果再做一次，你会改变什么？',
            },
          );
    final review = completed
        ? AgentReview(
            id: 'review-$sessionId',
            title: '${snapshot.matter.scene.title} · 三轮复盘',
            summary: '你已经能按“背景—行动—结果”组织回答，整体表达连贯。',
            strength: '能够说明自己的责任，并给出具体取舍。',
            nextFocus: '下一次把结果量化，同时缩短开场句。',
          )
        : null;
    final confirmation = PracticeTurnConfirmation(
      turnId: 'turn-$generation-$completedTurns',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      answer: AgentMessage(
        id: 'answer-${++_messageSequence}',
        role: AgentMessageRole.user,
        text: candidate.text,
      ),
      completedTurns: completedTurns,
      turnLimit: snapshot.turnLimit,
      sessionCompleted: completed,
      sessionVersion: (snapshot.sessionVersion ?? 1) + 1,
      nextQuestion: nextQuestion,
      review: review,
    );
    _confirmations[key] = confirmation;
    _snapshot = PracticeSessionSnapshot(
      sessionId: sessionId,
      sessionVersion: confirmation.sessionVersion,
      matter: snapshot.matter,
      completedTurns: completedTurns,
      turnLimit: snapshot.turnLimit,
      sessionCompleted: completed,
      currentQuestion: nextQuestion,
      review: review,
    );
    return confirmation;
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) async {
    final generation = _generation;
    await _wait(generation);
    final snapshot = _snapshot;
    if (snapshot == null ||
        snapshot.sessionId != sessionId ||
        snapshot.sessionVersion != expectedSessionVersion ||
        idempotencyKey.trim().isEmpty) {
      throw StateError('Fake practice cannot be ended.');
    }
    _snapshot = null;
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.endedEarly,
      version: expectedSessionVersion + 1,
    );
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) async {
    final text = answerText.trim();
    if (text.isEmpty) {
      throw ArgumentError.value(answerText, 'answerText');
    }
    final candidate = TranscriptionCandidate(
      id: 'text-candidate-$idempotencyKey',
      sessionId: sessionId,
      questionId: questionId,
      text: text,
    );
    _candidates['$sessionId\u0000$idempotencyKey'] = candidate;
    return confirm(
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidate.id,
      idempotencyKey: idempotencyKey,
    );
  }

  Future<void> _wait(int generation) async {
    if (delay != Duration.zero) {
      await Future<void>.delayed(delay);
    }
    if (generation != _generation) {
      throw StateError('Fake practice operation cancelled.');
    }
  }
}
