import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/practice/wire_practice_client.dart';
import 'package:speakup/review/review_history_client.dart';
import 'package:speakup/review/review_history_controller.dart';

import '../review/formal_review_fixture.dart';

void main() {
  test(
    'two completed Sessions keep Agent usable without guessing a Practice',
    () async {
      final transport = _PracticeTransport([
        _PracticeStep(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/voice-practice-session',
          response: _errorResponse(
            statusCode: HttpStatus.conflict,
            code: 'resource_conflict',
            retryable: false,
          ),
        ),
        _PracticeStep(
          method: 'POST',
          path: '/v1/agent-threads/$_threadId/voice-practice-sessions',
          response: _jsonResponse(
            HttpStatus.created,
            _activeSessionJson(_newSessionId),
          ),
        ),
      ]);
      final agent = _RestoredAgentClient();
      final controller = AgentController(
        client: agent,
        practiceClient: _wirePracticeClient(transport),
        clientIdFactory: (scope) => '$scope-stable-id',
      );
      final history = ReviewHistoryController(
        client: _CompletedReviewHistoryClient(),
      );
      addTearDown(controller.dispose);
      addTearDown(history.dispose);

      await controller.initialize();

      expect(controller.threadId, _threadId);
      expect(controller.activeMatter?.id, _matterId);
      expect(controller.messages, hasLength(1));
      expect(controller.practiceSessionId, isNull);
      expect(controller.hasActivePractice, isFalse);
      expect(controller.review, isNull);
      expect(controller.canSelectScene, isTrue);
      expect(controller.canRetry, isFalse);
      expect(controller.errorMessage, isNull);

      expect(await controller.sendText('Agent remains available.'), isTrue);
      expect(agent.sentThreadIds, [_threadId]);
      expect(controller.messages, hasLength(3));

      await history.refresh();
      expect(history.items.map((item) => item.practiceSessionId), [
        _firstCompletedSessionId,
        _secondCompletedSessionId,
      ]);
      expect(controller.review, isNull);

      await controller.activateCreatedPractice(
        threadId: _threadId,
        matterId: _matterId,
        sessionId: _newSessionId,
        turnLimit: 3,
        clientOperationId: 'voice-new-practice',
      );

      expect(controller.practiceSessionId, _newSessionId);
      expect(controller.hasActivePractice, isTrue);
      transport.expectDone();
    },
  );

  test(
    'authentication network and in-progress restore failures are not ignored',
    () async {
      final cases =
          <({String name, Object? error, PracticeWireResponse? response})>[
            (
              name: 'authentication',
              error: null,
              response: _errorResponse(
                statusCode: HttpStatus.unauthorized,
                code: 'authentication_required',
                retryable: false,
              ),
            ),
            (
              name: 'network',
              error: const SocketException('offline'),
              response: null,
            ),
            (
              name: 'in-progress Session',
              error: null,
              response: _errorResponse(
                statusCode: HttpStatus.conflict,
                code: 'resource_processing',
                retryable: true,
              ),
            ),
          ];

      for (final testCase in cases) {
        final transport = _PracticeTransport([
          _PracticeStep(
            method: 'GET',
            path: '/v1/agent-threads/$_threadId/voice-practice-session',
            response: testCase.response,
            error: testCase.error,
          ),
        ]);
        final controller = AgentController(
          client: _RestoredAgentClient(),
          practiceClient: _wirePracticeClient(transport),
        );
        addTearDown(controller.dispose);

        await controller.initialize();

        expect(controller.threadId, isNull, reason: testCase.name);
        expect(controller.activeMatter, isNull, reason: testCase.name);
        expect(controller.practiceSessionId, isNull, reason: testCase.name);
        expect(controller.canSelectScene, isFalse, reason: testCase.name);
        expect(controller.canRetry, isTrue, reason: testCase.name);
        expect(controller.errorMessage, isNotNull, reason: testCase.name);
        transport.expectDone();
      }
    },
  );
}

WirePracticeClient _wirePracticeClient(PracticeWireTransport transport) {
  return WirePracticeClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => _credential,
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: transport,
  );
}

PracticeWireResponse _errorResponse({
  required int statusCode,
  required String code,
  required bool retryable,
}) {
  return _jsonResponse(statusCode, {
    'error': {
      'code': code,
      'message': 'Canonical server error.',
      'retryable': retryable,
      'correlation_id': 'corr-practice-restore',
    },
  });
}

PracticeWireResponse _jsonResponse(int statusCode, Object body) {
  return PracticeWireResponse(statusCode: statusCode, body: jsonEncode(body));
}

Map<String, Object?> _activeSessionJson(String sessionId) {
  return {
    'practice_session_id': sessionId,
    'practice_plan_id': 'plan-new',
    'thread_id': _threadId,
    'scenario_type': 'INTERVIEW',
    'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
    'matter': {
      'matter_id': _matterId,
      'title': _matter.scene.title,
      'status': 'active',
      'version': 4,
      'created_at': _timestamp,
      'updated_at': _timestamp,
    },
    'session_version': 2,
    'effective_turns': 0,
    'turn_limit': 3,
    'session_completed': false,
    'current_question': {
      'question_id': 'question-new',
      'practice_session_id': sessionId,
      'content': 'Tell me about your latest project.',
      'speaker_participant_id': 'participant-interviewer',
      'addressee_participant_ids': ['participant-candidate'],
      'speech_path': '/v1/voice-questions/question-new/speech',
    },
  };
}

final class _RestoredAgentClient implements AgentClient {
  final List<String> sentThreadIds = <String>[];

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<AgentThreadSnapshot> restoreThread() async {
    return const AgentThreadSnapshot(
      threadId: _threadId,
      activeMatter: _matter,
      messages: [
        AgentMessage(
          id: 'message-restored',
          role: AgentMessageRole.assistant,
          text: 'Restored Agent context.',
        ),
      ],
    );
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) async {
    sentThreadIds.add(threadId);
    return AgentExchange(
      userMessage: AgentMessage(
        id: 'message-user',
        role: AgentMessageRole.user,
        text: text,
      ),
      assistantMessage: const AgentMessage(
        id: 'message-assistant',
        role: AgentMessageRole.assistant,
        text: 'Agent reply.',
      ),
    );
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    throw UnimplementedError();
  }
}

final class _CompletedReviewHistoryClient implements ReviewHistoryClient {
  @override
  Future<void> clearAccountState() async {}

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    return ReviewHistoryPage(
      items: [
        _historyItem(_firstCompletedSessionId, 'review-first'),
        _historyItem(_secondCompletedSessionId, 'review-second'),
      ],
    );
  }

  ReviewHistoryItem _historyItem(String sessionId, String reviewId) {
    final completedAt = DateTime.parse(_timestamp);
    final createdAt = completedAt.subtract(const Duration(minutes: 5));
    final review = AgentReview(
      id: reviewId,
      title: 'Completed review',
      summary: 'Historical Session remains in Review history.',
      strength: 'Clear answer',
      nextFocus: 'Add evidence',
    );
    return ReviewHistoryItem(
      review: review,
      formalReview: legacyFormalReviewFixture(
        review: review,
        practiceSessionId: sessionId,
        createdAt: createdAt,
        completedAt: completedAt,
      ),
      practiceSessionId: sessionId,
      createdAt: createdAt,
      completedAt: completedAt,
    );
  }
}

final class _PracticeStep {
  const _PracticeStep({
    required this.method,
    required this.path,
    this.response,
    this.error,
  }) : assert(response != null || error != null);

  final String method;
  final String path;
  final PracticeWireResponse? response;
  final Object? error;
}

final class _PracticeTransport implements PracticeWireTransport {
  _PracticeTransport(Iterable<_PracticeStep> steps)
    : _steps = Queue<_PracticeStep>.of(steps);

  final Queue<_PracticeStep> _steps;

  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) async {
    final step = _steps.removeFirst();
    expect(request.method, step.method);
    expect(request.uri.path, step.path);
    expect(
      request.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_practice_restore',
    );
    final error = step.error;
    if (error != null) {
      throw error;
    }
    return step.response!;
  }

  void expectDone() => expect(_steps, isEmpty);

  @override
  void close({bool force = false}) {}
}

const _credential = AuthSessionCredential(
  sessionToken: 'sess_practice_restore',
  generation: 1,
);
const _threadId = '10000000-0000-4000-8000-000000000111';
const _matterId = '20000000-0000-4000-8000-000000000111';
const _firstCompletedSessionId = '30000000-0000-4000-8000-000000000111';
const _secondCompletedSessionId = '30000000-0000-4000-8000-000000000112';
const _newSessionId = '30000000-0000-4000-8000-000000000113';
const _timestamp = '2026-07-26T08:00:00Z';
const _matter = AgentMatter(
  id: _matterId,
  scene: AgentScene(
    id: 'programmer-interview',
    title: 'Programmer interview',
    description: 'Formal interview practice.',
  ),
);
