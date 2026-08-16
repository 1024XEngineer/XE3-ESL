import 'dart:typed_data';

import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';

abstract interface class PracticeClient {
  Future<void> clearAccountState();

  Future<PracticeSessionSnapshot> restorePractice({required String sessionId});

  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
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

abstract interface class PracticeRealtimeTranscriptionClient {
  Stream<PracticeTranscriptionEvent> transcribeRealtime({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
    required Stream<Uint8List> audioChunks,
  });
}

sealed class PracticeTranscriptionEvent {
  const PracticeTranscriptionEvent();
}

final class PracticeTranscriptUpdated extends PracticeTranscriptionEvent {
  const PracticeTranscriptUpdated({required this.text, required this.isFinal});

  final String text;
  final bool isFinal;
}

final class PracticeCandidateCompleted extends PracticeTranscriptionEvent {
  const PracticeCandidateCompleted(this.candidate);

  final TranscriptionCandidate candidate;
}

abstract interface class PracticeLifecycleClient {
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  });
}

abstract interface class PracticeCompletionClient {
  Future<PracticeSessionLifecycle> complete({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  });
}

abstract interface class PracticeQuestionTranslationClient {
  Future<PracticeQuestionTranslation> translateQuestion({
    required String questionId,
  });
}

/// Optional interview-only reference answer capability.
///
/// A Tip is a separate read aid. It is never submitted as a candidate or Turn.
abstract interface class PracticeQuestionTipClient {
  Future<PracticeQuestionTip> ensureQuestionTip({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
  });
}

/// Optional Daily/Workplace same-question retry capability.
///
/// Keeping this separate from [PracticeClient] lets existing test doubles and
/// non-feedback practice clients remain unchanged.
abstract interface class PracticeSpeechFeedbackRetryClient {
  Future<PracticeRetryTurn> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
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

final class FakePracticeClient
    implements
        PracticeClient,
        PracticeLifecycleClient,
        PracticeCompletionClient {
  FakePracticeClient({
    this.delay = Duration.zero,
    this.practiceExperience = PracticeExperience.interview,
    this.sceneCategory = SceneCategory.interviewProfessional,
    this.practiceMode = PracticeMode.fullSimulation,
    this.capabilities = const PracticeCapabilities(
      retryAllowed: false,
      questionTranslationAllowed: false,
      questionTipsAllowed: true,
      speechFeedbackAllowed: false,
    ),
    this.turnLimit = 3,
    this.completionMode = PracticeCompletionMode.turnLimited,
    this.ieltsAssignment,
    this.planId,
    PracticeSessionSnapshot? initialSnapshot,
  }) : _snapshot = initialSnapshot {
    if ((completionMode == PracticeCompletionMode.turnLimited &&
            (turnLimit < 1 || turnLimit > practiceTurnSafetyLimit)) ||
        (completionMode == PracticeCompletionMode.userControlled &&
            turnLimit != 0) ||
        (practiceExperience == PracticeExperience.ieltsSpeaking) !=
            (ieltsAssignment != null) ||
        (ieltsAssignment != null &&
            (ieltsAssignment!.mode != practiceMode ||
                ieltsAssignment!.turnBlueprints.length != turnLimit))) {
      throw ArgumentError('Invalid Fake Practice Session configuration.');
    }
  }

  final Duration delay;
  final PracticeExperience practiceExperience;
  final SceneCategory sceneCategory;
  final PracticeMode practiceMode;
  final PracticeCapabilities capabilities;
  final int turnLimit;
  final PracticeCompletionMode completionMode;
  final IeltsPracticeAssignment? ieltsAssignment;
  final String? planId;
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
  Future<PracticeSessionSnapshot> restorePractice({
    required String sessionId,
  }) async {
    await _wait(_generation);
    final snapshot = _snapshot;
    if (snapshot == null || snapshot.sessionId != sessionId) {
      throw StateError('Unknown Fake practice Session.');
    }
    return snapshot;
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) async {
    final generation = _generation;
    await _wait(generation);
    final existing = _snapshot;
    if (existing != null) {
      if (existing.sessionId != sessionId) {
        throw StateError('A different Fake practice Session is active.');
      }
      return existing;
    }
    final snapshot = PracticeSessionSnapshot(
      sessionId: sessionId,
      planId: planId ?? 'practice-plan-$sessionId',
      practiceExperience: practiceExperience,
      sceneCategory: sceneCategory,
      practiceMode: practiceMode,
      capabilities: capabilities,
      sessionVersion: 1,
      completedTurns: 0,
      turnLimit: turnLimit,
      completionMode: completionMode,
      sessionCompleted: false,
      ieltsAssignment: ieltsAssignment,
      currentQuestion: PracticeQuestion(
        id: 'question-$generation-1',
        sessionId: sessionId,
        text: '第一轮：请先用英文回答，你希望面试官首先了解你的哪段经历？',
      ),
    );
    _snapshot = snapshot;
    return snapshot;
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
    final nextSessionVersion = snapshot.sessionVersion + 1;
    final completed =
        snapshot.completionMode == PracticeCompletionMode.turnLimited &&
        completedTurns == snapshot.turnLimit;
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
    final confirmation = PracticeTurnConfirmation(
      turnId: 'turn-$generation-$completedTurns',
      sessionId: sessionId,
      questionId: questionId,
      candidateId: candidateId,
      answer: PracticeMessage(
        id: 'answer-${++_messageSequence}',
        role: PracticeMessageRole.user,
        text: candidate.text,
      ),
      completedTurns: completedTurns,
      turnLimit: snapshot.turnLimit,
      completionMode: snapshot.completionMode,
      sessionCompleted: completed,
      practiceExperience: snapshot.practiceExperience,
      sceneCategory: snapshot.sceneCategory,
      practiceMode: snapshot.practiceMode,
      capabilities: snapshot.capabilities,
      sessionVersion: nextSessionVersion,
      nextQuestion: nextQuestion,
    );
    _confirmations[key] = confirmation;
    _snapshot = PracticeSessionSnapshot(
      sessionId: sessionId,
      planId: snapshot.planId,
      practiceExperience: snapshot.practiceExperience,
      sceneCategory: snapshot.sceneCategory,
      practiceMode: snapshot.practiceMode,
      capabilities: snapshot.capabilities,
      sessionVersion: nextSessionVersion,
      completedTurns: completedTurns,
      turnLimit: snapshot.turnLimit,
      completionMode: snapshot.completionMode,
      sessionCompleted: completed,
      currentQuestion: nextQuestion,
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
  Future<PracticeSessionLifecycle> complete({
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
        snapshot.completionMode != PracticeCompletionMode.userControlled ||
        snapshot.completedTurns < 1 ||
        idempotencyKey.trim().isEmpty) {
      throw StateError('Fake practice cannot be completed.');
    }
    _snapshot = PracticeSessionSnapshot(
      sessionId: snapshot.sessionId,
      planId: snapshot.planId,
      practiceExperience: snapshot.practiceExperience,
      sceneCategory: snapshot.sceneCategory,
      practiceMode: snapshot.practiceMode,
      capabilities: snapshot.capabilities,
      sessionVersion: expectedSessionVersion + 1,
      completedTurns: snapshot.completedTurns,
      turnLimit: snapshot.turnLimit,
      completionMode: snapshot.completionMode,
      sessionCompleted: true,
      currentTurn: snapshot.currentTurn,
      turnHistory: snapshot.turnHistory,
    );
    return PracticeSessionLifecycle(
      sessionId: sessionId,
      status: PracticeSessionLifecycleStatus.completed,
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
