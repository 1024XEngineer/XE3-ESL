import '../../support/scene_fixtures.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/wire_practice_client.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/review/review_summary.dart';

import '../../review/evaluation_report_fixture.dart';

void main() {
  test(
    'two completed Sessions keep Agent usable without guessing a Practice',
    () async {
      final transport = _PracticeTransport([
        _PracticeStep(
          method: 'POST',
          path: '/v1/practice-sessions/$_newSessionId/voice-activation',
          response: _jsonResponse(
            HttpStatus.ok,
            _activeSessionJson(_newSessionId),
          ),
        ),
      ]);
      final agent = _RestoredAgentClient();
      final conversationController = ConversationController(
        client: agent,
        clientIdFactory: (scope) => '$scope-stable-id',
      );
      final practiceController = PracticeController(
        client: _wirePracticeClient(transport),
        clientIdFactory: (scope) => '$scope-stable-id',
      );
      final history = ReviewHistoryController(
        client: _CompletedReviewHistoryClient(),
      );
      addTearDown(conversationController.dispose);
      addTearDown(practiceController.dispose);
      addTearDown(history.dispose);

      await conversationController.initialize();

      expect(conversationController.threadId, _threadId);
      expect(conversationController.activeGoalId, _goalId);
      expect(conversationController.messages, hasLength(1));
      expect(practiceController.practiceSessionId, isNull);
      expect(practiceController.hasActivePractice, isFalse);
      expect(conversationController.canRetry, isFalse);
      expect(conversationController.errorMessage, isNull);

      expect(
        await conversationController.sendText('Agent remains available.'),
        isTrue,
      );
      expect(agent.sentThreadIds, [_threadId]);
      expect(conversationController.messages, hasLength(3));

      await history.refresh();
      expect(history.items.map((item) => item.practiceSessionId), [
        _firstCompletedSessionId,
        _secondCompletedSessionId,
      ]);

      await practiceController.activateCreatedPractice(
        scene: _scene,
        sessionId: _newSessionId,
        planId: 'plan-new',
        turnLimit: 3,
        clientOperationId: 'voice-new-practice',
      );

      expect(practiceController.practiceSessionId, _newSessionId);
      expect(practiceController.hasActivePractice, isTrue);
      transport.expectDone();
    },
  );

  test(
    'explicit Session restore failures do not erase the Agent context',
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
            path: '/v1/practice-sessions/$_newSessionId/voice-state',
            response: testCase.response,
            error: testCase.error,
          ),
        ]);
        final conversationController = ConversationController(
          client: _RestoredAgentClient(),
        );
        final practiceController = PracticeController(
          client: _wirePracticeClient(transport),
        );
        addTearDown(conversationController.dispose);
        addTearDown(practiceController.dispose);

        await conversationController.initialize();
        await expectLater(
          practiceController.restoreCreatedPractice(
            sessionId: _newSessionId,
            scene: _scene,
          ),
          throwsA(isA<PracticeClientException>()),
          reason: testCase.name,
        );

        expect(
          conversationController.threadId,
          _threadId,
          reason: testCase.name,
        );
        expect(
          conversationController.activeGoalId,
          _goalId,
          reason: testCase.name,
        );
        expect(
          practiceController.practiceSessionId,
          isNull,
          reason: testCase.name,
        );
        expect(conversationController.canRetry, isFalse, reason: testCase.name);
        expect(
          conversationController.errorMessage,
          isNull,
          reason: testCase.name,
        );
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
    'scene_id': 'scene-project-deep-dive',
    'scene_version': 1,
    'scene_family': 'INTERVIEW',
    'scene_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
    'practice_session_status': 'in_progress',
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
  bool _focused = true;
  bool _deleted = false;

  @override
  Future<void> clearAccountState() async {}

  @override
  Future<AgentThreadPage> listThreads({
    int pageSize = 20,
    String? cursor,
  }) async {
    expect(cursor, isNull);
    return AgentThreadPage(
      threads: _deleted
          ? const <AgentThreadSummary>[]
          : <AgentThreadSummary>[_summary],
      focusedThreadId: _focused && !_deleted ? _threadId : null,
    );
  }

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() async =>
      _focused && !_deleted ? _snapshot : null;

  @override
  Future<AgentThreadSummary> createThread() async {
    _deleted = false;
    _focused = true;
    return _summary;
  }

  @override
  Future<AgentThreadSnapshot> setFocusedThread({
    required String threadId,
  }) async {
    if (_deleted || threadId != _threadId) {
      throw StateError('Unknown test Agent Thread.');
    }
    _focused = true;
    return _snapshot;
  }

  @override
  Future<void> clearFocusedThread() async {
    _focused = false;
  }

  @override
  Future<void> deleteThread({required String threadId}) async {
    if (threadId != _threadId) {
      throw StateError('Unknown test Agent Thread.');
    }
    _focused = false;
    _deleted = true;
  }

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) async {
    if (_deleted || threadId != _threadId || cursor != null) {
      throw StateError('Unknown test Agent message page.');
    }
    return AgentMessagePage(messages: _snapshot.messages);
  }

  AgentThreadSnapshot get _snapshot => AgentThreadSnapshot(
    threadId: _threadId,
    title: _summary.title,
    activeGoalId: _goalId,
    messages: const <AgentMessage>[
      AgentMessage(
        id: 'message-restored',
        role: AgentMessageRole.assistant,
        text: 'Restored Agent context.',
      ),
    ],
    createdAt: _summary.createdAt,
    updatedAt: _summary.updatedAt,
  );

  AgentThreadSummary get _summary {
    final timestamp = DateTime.parse(_timestamp);
    return AgentThreadSummary(
      id: _threadId,
      title: 'Restored conversation',
      activeGoalId: _goalId,
      createdAt: timestamp,
      updatedAt: timestamp,
    );
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
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
    final review = ReviewSummary(
      id: reviewId,
      title: 'Completed review',
      summary: 'Historical Session remains in Review history.',
      strength: 'Clear answer',
      nextFocus: 'Add evidence',
    );
    return ReviewHistoryItem(
      review: review,
      report: evaluationReportFixture(
        review: review,
        practiceSessionId: sessionId,
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
const _goalId = '20000000-0000-4000-8000-000000000111';
const _firstCompletedSessionId = '30000000-0000-4000-8000-000000000111';
const _secondCompletedSessionId = '30000000-0000-4000-8000-000000000112';
const _newSessionId = '30000000-0000-4000-8000-000000000113';
const _timestamp = '2026-07-26T08:00:00Z';
final _scene = testScene(
  id: 'programmer-interview',
  family: SceneFamily.interview,
  model: SceneModel.projectExperienceDeepDive,
  name: 'Programmer interview',
  prompt: const ScenePrompt(
    publicSceneBrief: 'Formal interview practice.',
    practiceGoal: 'Complete the interview practice.',
    userRole: 'Candidate',
    aiRole: 'Interviewer',
    personaSummary: 'Professional and focused.',
    focusAreas: <String>['clarity'],
    turnBlueprints: <String>['Ask one interview question.'],
    suggestedDurationSeconds: 600,
  ),
);
