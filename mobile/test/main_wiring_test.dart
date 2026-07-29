import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_voice_models.dart';
import 'package:speakup/agent/agent_voice_recording.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/agent/wire_agent_voice_client.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/features/preparation/job_preparation_draft_store.dart';
import 'package:speakup/features/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/preparation/wire_job_preparation_client.dart';
import 'package:speakup/features/preparation/wire_preparation_client.dart';
import 'package:speakup/features/preparation/wire_preparation_launch_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/session_store.dart';
import 'package:speakup/main.dart' as production;
import 'package:speakup/practice/practice_audio_player.dart';
import 'package:speakup/practice/practice_media.dart';
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/practice/wire_practice_client.dart';
import 'package:speakup/review/wire_review_history_client.dart';

void main() {
  test('iOS allows local development traffic without a global ATS bypass', () {
    final plist = File('ios/Runner/Info.plist').readAsStringSync();

    expect(
      plist,
      matches(
        RegExp(
          r'<key>NSAppTransportSecurity</key>\s*'
          r'<dict>\s*'
          r'<key>NSAllowsLocalNetworking</key>\s*'
          r'<true\s*/>\s*'
          r'</dict>',
        ),
      ),
    );
    expect(plist, isNot(contains('<key>NSAllowsArbitraryLoads</key>')));
    expect(plist, contains('<key>NSMicrophoneUsageDescription</key>'));
  });

  testWidgets(
    'production composition restores Auth and Agent data without Fake fallback',
    (tester) async {
      final identityTransport = _Transport([
        _Response(
          method: 'GET',
          path: '/v1/me',
          statusCode: HttpStatus.ok,
          body: {'user_id': 'user_fixture', 'email': 'test.user@example.com'},
        ),
        const _Response(
          method: 'POST',
          path: '/v1/auth/logout',
          statusCode: HttpStatus.noContent,
          body: null,
        ),
      ]);
      final agentTransport = _Transport([
        _Response(
          method: 'GET',
          path: '/v1/agent-threads',
          statusCode: HttpStatus.ok,
          body: {
            'threads': [
              {
                'thread_id': _threadId,
                'created_at': _timestamp,
                'updated_at': _timestamp,
              },
            ],
            'focused_thread_id': _threadId,
          },
        ),
        _Response(
          method: 'GET',
          path: '/v1/agent-threads/focused',
          statusCode: HttpStatus.ok,
          body: {
            'thread_id': _threadId,
            'created_at': _timestamp,
            'updated_at': _timestamp,
          },
        ),
        const _Response(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages',
          statusCode: HttpStatus.ok,
          body: {'messages': <Object?>[]},
        ),
      ]);
      final reviewHistoryTransport = _ControlledReviewHistoryTransport();
      final preparationTransport = _Transport([
        _Response(
          method: 'GET',
          path: '/v1/scenario-definitions',
          statusCode: HttpStatus.ok,
          body: {
            'scenarios': [
              {
                'scenario_definition_id': 'scn_programmer_interview',
                'scenario_type': 'INTERVIEW',
                'scenario_model': 'PROJECT_EXPERIENCE_DEEP_DIVE',
                'name': 'English interview for technical roles',
                'summary': 'Discuss one backend project.',
                'version': 1,
                'status': 'active',
              },
            ],
          },
        ),
      ]);
      addTearDown(reviewHistoryTransport.completeEmptyIfPending);
      final practiceRecorder = _TrackingPracticeRecorder();
      final practiceMediaClient = _TrackingPracticeMediaClient();
      final practiceAudioPlayer = _TrackingPracticeAudioPlayer();
      final agentVoiceRecorder = _TrackingAgentVoiceRecorder();
      final agentVoiceAudioPlayer = _TrackingAgentVoiceAudioPlayer();
      final dependencies = production.createProductionAppDependencies(
        baseUri: Uri.parse('https://api.speak-up.test'),
        identityTransport: identityTransport,
        agentTransport: agentTransport,
        preparationTransport: preparationTransport,
        reviewHistoryTransport: reviewHistoryTransport,
        practiceTransport: _PracticeTransport(),
        practiceRecorder: practiceRecorder,
        agentVoiceRecorder: agentVoiceRecorder,
        agentVoiceAudioPlayer: agentVoiceAudioPlayer,
        practiceMediaClient: practiceMediaClient,
        practiceAudioPlayer: practiceAudioPlayer,
        jobPreparationDraftStore: MemoryJobPreparationDraftStore(),
        practiceLaunchRecordStore: MemoryPracticeLaunchRecordStore(),
        sessionStore: _MemorySessionStore('sess_main-wiring'),
      );
      addTearDown(dependencies.agentController.dispose);
      addTearDown(dependencies.preparationController.dispose);
      addTearDown(dependencies.preparationLaunchController.dispose);
      addTearDown(dependencies.jobPreparationController.dispose);
      addTearDown(dependencies.reviewHistoryController.dispose);

      expect(dependencies.agentController.client, isA<WireAgentClient>());
      expect(
        dependencies.agentController.client,
        isNot(isA<FakeAgentClient>()),
      );
      expect(
        dependencies.agentController.mediaClient,
        same(practiceMediaClient),
      );
      expect(
        dependencies.agentController.audioPlayer,
        same(practiceAudioPlayer),
      );
      expect(
        dependencies.agentController.voiceController?.client,
        isA<WireAgentVoiceClient>(),
      );
      expect(
        dependencies.agentController.voiceController?.recorder,
        same(agentVoiceRecorder),
      );
      expect(
        dependencies.agentController.voiceController?.audioPlayer,
        same(agentVoiceAudioPlayer),
      );
      expect(
        dependencies.reviewHistoryController.client,
        isA<WireReviewHistoryClient>(),
      );
      expect(
        dependencies.preparationController.client,
        isA<WirePreparationCatalogClient>(),
      );
      expect(
        dependencies.preparationLaunchController.client,
        isA<WirePreparationLaunchClient>(),
      );
      expect(
        dependencies.jobPreparationController.client,
        isA<WireJobPreparationClient>(),
      );

      await tester.pumpWidget(
        SpeakUpApp(
          authController: dependencies.authController,
          agentController: dependencies.agentController,
          preparationController: dependencies.preparationController,
          jobPreparationController: dependencies.jobPreparationController,
          preparationLaunchController: dependencies.preparationLaunchController,
          reviewHistoryController: dependencies.reviewHistoryController,
        ),
      );
      for (var attempt = 0; attempt < 100; attempt++) {
        await tester.pump(const Duration(milliseconds: 20));
        if (dependencies.authController.state is AuthAuthenticated &&
            dependencies.agentController.threadId != null) {
          break;
        }
      }

      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
      expect(
        dependencies.agentController.practiceClient,
        isA<WirePracticeClient>(),
      );
      expect(find.byKey(const Key('agent-practice-unavailable')), findsNothing);
      expect(find.byKey(const Key('agent-mic-placeholder')), findsOneWidget);
      expect(find.byKey(const Key('agent-preview-label')), findsNothing);
      expect(find.byKey(const Key('quick-action-create-plan')), findsOneWidget);
      expect(dependencies.agentController.threadId, _threadId);
      expect(dependencies.authController.state, isA<AuthAuthenticated>());
      expect(
        identityTransport.calls.first.authorization,
        'Bearer sess_main-wiring',
      );
      expect(
        agentTransport.calls.every(
          (call) => call.authorization == 'Bearer sess_main-wiring',
        ),
        isTrue,
      );
      expect(agentVoiceRecorder.clearCount, 2);
      expect(agentVoiceAudioPlayer.clearCount, 2);

      await dependencies.preparationController.loadIfNeeded();
      expect(dependencies.preparationController.errorMessage, isNull);
      expect(dependencies.preparationController.scenarios, isNotEmpty);
      await tester.tap(find.byKey(const Key('primary-tab-scenes')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('scenes-page')), findsOneWidget);
      expect(find.byKey(const Key('training-center-title')), findsOneWidget);
      expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
      await tester.tap(find.byKey(const Key('practice-hub-interview')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('open-job-preparation')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('job-preparation-wizard')), findsOneWidget);
      expect(find.byKey(const Key('job-description-field')), findsOneWidget);
      expect(find.byKey(const Key('primary-navigation')), findsNothing);
      await tester.tap(find.byKey(const Key('job-wizard-close')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('primary-tab-review')));
      await tester.pump();
      expect(reviewHistoryTransport.calls, 1);
      expect(reviewHistoryTransport.authorization, 'Bearer sess_main-wiring');

      final logout = dependencies.authController.logout();
      await tester.pump();
      await logout.timeout(const Duration(seconds: 1));
      await tester.pump();

      expect(dependencies.reviewHistoryController.items, isEmpty);
      expect(dependencies.reviewHistoryController.errorMessage, isNull);
      expect(dependencies.reviewHistoryController.isLoading, isFalse);
      expect(dependencies.preparationController.scenarios, isEmpty);
      expect(dependencies.preparationController.selectedScenario, isNull);
      expect(dependencies.jobPreparationController.target, isNull);
      expect(dependencies.jobPreparationController.plan, isNull);
      expect(dependencies.agentController.threadId, isNull);
      expect(dependencies.agentController.messages, isEmpty);
      expect(practiceRecorder.clearCount, 1);
      expect(practiceMediaClient.clearCount, 1);
      expect(practiceAudioPlayer.clearCount, 2);
      expect(agentVoiceRecorder.clearCount, 4);
      expect(agentVoiceAudioPlayer.clearCount, 4);

      reviewHistoryTransport.completeWithReview();
      await tester.pump();

      expect(
        dependencies.authController.state,
        isA<AuthSignedOut>().having(
          (state) => state.isSubmitting,
          'isSubmitting',
          isFalse,
        ),
      );
      expect(find.text('欢迎回来'), findsOneWidget);
      expect(dependencies.reviewHistoryController.items, isEmpty);
      expect(dependencies.reviewHistoryController.errorMessage, isNull);
      expect(
        identityTransport.calls.every(
          (call) => call.authorization == 'Bearer sess_main-wiring',
        ),
        isTrue,
      );
      identityTransport.expectDone();
      agentTransport.expectDone();
      preparationTransport.expectDone();
    },
  );
}

final class _PracticeTransport implements PracticeWireTransport {
  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) async {
    expect(request.method, 'GET');
    expect(
      request.uri.path,
      '/v1/agent-threads/$_threadId/voice-practice-session',
    );
    expect(
      request.headers[HttpHeaders.authorizationHeader],
      'Bearer sess_main-wiring',
    );
    return const PracticeWireResponse(
      statusCode: HttpStatus.notFound,
      body: '{}',
    );
  }

  @override
  void close({bool force = false}) {}
}

final class _TrackingPracticeRecorder implements PracticeRecorder {
  int clearCount = 0;

  @override
  Future<void> start() async {}

  @override
  Future<RecordedPracticeAudio> stop() {
    throw StateError('Unexpected recording stop in production wiring test.');
  }

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {}

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }
}

final class _TrackingAgentVoiceRecorder implements AgentVoiceRecorder {
  int clearCount = 0;

  @override
  Future<void> start() async {}

  @override
  Future<AgentVoiceLocalRecording> stop() {
    throw StateError('Unexpected Agent recording stop in production wiring.');
  }

  @override
  Future<void> discardCurrent() async {}

  @override
  Future<void> discard(AgentVoiceLocalRecording recording) async {}

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }
}

final class _TrackingAgentVoiceAudioPlayer implements AgentVoiceAudioPlayer {
  int clearCount = 0;

  @override
  Stream<Duration> get onPosition => const Stream<Duration>.empty();

  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> playFile(String path, {required double speed}) {
    throw StateError('Unexpected Agent draft playback in production wiring.');
  }

  @override
  Future<void> playWav(Uint8List bytes, {required double speed}) {
    throw StateError('Unexpected Agent speech playback in production wiring.');
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }

  @override
  Future<void> dispose() async {}
}

final class _TrackingPracticeMediaClient implements PracticeMediaClient {
  int clearCount = 0;

  @override
  Future<Uint8List> loadQuestionSpeech(String speechPath) {
    throw StateError('Unexpected question speech in production wiring test.');
  }

  @override
  Future<Uint8List> loadRecording(String audioAssetId) {
    throw StateError('Unexpected recording load in production wiring test.');
  }

  @override
  Future<void> deleteRecording(String audioAssetId) {
    throw StateError('Unexpected recording delete in production wiring test.');
  }

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }

  @override
  Future<void> dispose() async {}
}

final class _TrackingPracticeAudioPlayer implements PracticeAudioPlayer {
  int clearCount = 0;

  @override
  Stream<void> get onComplete => const Stream<void>.empty();

  @override
  Future<void> dispose() async {}

  @override
  Future<void> playWav(Uint8List bytes) async {}

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }

  @override
  Future<void> stop() async {}
}

final class _MemorySessionStore implements SessionStore {
  _MemorySessionStore(this.token);

  String? token;

  @override
  Future<void> deleteToken() async {
    token = null;
  }

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String token) async {
    this.token = token;
  }
}

final class _Response {
  const _Response({
    required this.method,
    required this.path,
    required this.statusCode,
    required this.body,
  });

  final String method;
  final String path;
  final int statusCode;
  final Object? body;
}

final class _Call {
  const _Call({required this.authorization});

  final String? authorization;
}

final class _ControlledReviewHistoryTransport implements IdentityHttpTransport {
  final Completer<IdentityHttpResponse> _response =
      Completer<IdentityHttpResponse>();
  int calls = 0;
  String? authorization;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) {
    expect(method, 'GET');
    expect(uri.path, '/v1/formal-reviews');
    expect(uri.queryParameters, {'limit': '20'});
    calls++;
    authorization = headers[HttpHeaders.authorizationHeader];
    return _response.future;
  }

  void completeWithReview() {
    _response.complete(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode({
          'items': [
            {
              'review_id': '20000000-0000-4000-8000-000000000088',
              'practice_session_id': 'practice_session_main_wiring',
              'status': 'completed',
              'implementation_version': 'qianwen-voice-review-v1',
              'source_turn_id': 'turn_main_wiring',
              'source_turn_version': 'conversation-turn:evidence-v1',
              'result': {
                'overall_score': 90,
                'summary': 'The response is clear.',
                'conclusions': [
                  {
                    'key': 'clarity',
                    'category': 'clarity',
                    'message': 'The answer is easy to follow.',
                    'suggestion': 'Add one concrete metric.',
                  },
                ],
              },
              'created_at': _timestamp,
              'updated_at': _timestamp,
              'completed_at': _timestamp,
            },
          ],
        }),
      ),
    );
  }

  void completeEmptyIfPending() {
    if (_response.isCompleted) {
      return;
    }
    _response.complete(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode({'items': <Object?>[]}),
      ),
    );
  }
}

final class _Transport implements IdentityHttpTransport {
  _Transport(Iterable<_Response> responses)
    : _responses = Queue<_Response>.of(responses);

  final Queue<_Response> _responses;
  final List<_Call> calls = <_Call>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    if (_responses.isEmpty) {
      throw StateError('Unexpected production wiring request.');
    }
    final response = _responses.removeFirst();
    expect(method, response.method);
    expect(uri.path, response.path);
    calls.add(_Call(authorization: headers[HttpHeaders.authorizationHeader]));
    return IdentityHttpResponse(
      statusCode: response.statusCode,
      body: jsonEncode(response.body),
    );
  }

  void expectDone() {
    expect(_responses, isEmpty);
  }
}

const _threadId = '10000000-0000-4000-8000-000000000088';
const _timestamp = '2026-07-25T09:00:00Z';
