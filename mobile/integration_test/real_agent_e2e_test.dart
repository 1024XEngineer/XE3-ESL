import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:path_provider/path_provider.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/main.dart' as app;
import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/identity/session_store.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('real iOS identity, Qianwen Agent, voice, and Review path', (
    tester,
  ) async {
    final email =
        'voice-report-${DateTime.now().microsecondsSinceEpoch}@example.com';
    const password = 'Voice report e2e password 8142';
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    const voiceWavBase64 = <String>[
      String.fromEnvironment('SPEAKUP_E2E_WAV_BASE64'),
      String.fromEnvironment('SPEAKUP_E2E_WAV_BASE64_2'),
      String.fromEnvironment('SPEAKUP_E2E_WAV_BASE64_3'),
      String.fromEnvironment('SPEAKUP_E2E_WAV_BASE64_4'),
    ];
    const captureHoldMs = int.fromEnvironment('SPEAKUP_E2E_CAPTURE_HOLD_MS');
    const validateAudioMedia = bool.fromEnvironment(
      'SPEAKUP_E2E_VALIDATE_AUDIO_MEDIA',
    );
    final voiceFixtures = <List<int>>[
      for (var index = 0; index < voiceWavBase64.length; index++)
        _decodeVoiceFixture(
          voiceWavBase64[index],
          variableName: index == 0
              ? 'SPEAKUP_E2E_WAV_BASE64'
              : 'SPEAKUP_E2E_WAV_BASE64_${index + 1}',
        ),
    ];
    if ({for (final fixture in voiceFixtures) base64Encode(fixture)}.length !=
        voiceFixtures.length) {
      fail('The four voice E2E WAV fixtures must contain distinct speech.');
    }
    final practiceRecorder = _FixturePracticeRecorder(voiceFixtures);
    final practiceAudioPlayer = _ObservedPracticeAudioPlayer();
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);

    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      practiceRecorder: practiceRecorder,
      practiceAudioPlayer: practiceAudioPlayer,
      sessionStore: _MemorySessionStore(),
    );
    final deletedAudioAssetIds = <String>{};
    addTearDown(() async {
      final remainingAudioAssetIds = <String>{
        for (final recording in dependencies.practiceController.recordings)
          if (!deletedAudioAssetIds.contains(recording.audioAssetId))
            recording.audioAssetId,
      };
      if (remainingAudioAssetIds.isNotEmpty) {
        await _deleteTestRecordings(
          dependencies.practiceController,
          remainingAudioAssetIds.toList(growable: false),
        );
      }
      await practiceRecorder.clearAccountState();
      await practiceAudioPlayer.dispose();
    });
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        conversationController: dependencies.conversationController,
        composerController: dependencies.composerController,
        messageAudioController: dependencies.messageAudioController,
        messageTranslationClient: dependencies.messageTranslationClient,
        practiceController: dependencies.practiceController,
        preparationController: dependencies.preparationController,
        ieltsPreparationController: dependencies.ieltsPreparationController,
        jobPreparationController: dependencies.jobPreparationController,
        preparationLaunchController: dependencies.preparationLaunchController,
        reviewHistoryController: dependencies.reviewHistoryController,
        sessionEvaluationController: dependencies.sessionEvaluationController,
      ),
    );
    await _waitUntil(
      tester,
      () =>
          find.text('欢迎回来').evaluate().isNotEmpty ||
          find.text('需要网络连接').evaluate().isNotEmpty ||
          find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty,
      const Duration(seconds: 15),
    );
    if (find.text('需要网络连接').evaluate().isNotEmpty) {
      await tester.tap(find.text('重试'));
      await _waitUntil(
        tester,
        () =>
            find.text('欢迎回来').evaluate().isNotEmpty ||
            find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty,
        const Duration(seconds: 15),
      );
    }

    if (find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty) {
      final sameAccount = await _signedInAccountMatches(tester, email);
      if (!sameAccount) {
        await _signOut(tester);
        await _registerOrSignIn(tester, email: email, password: password);
      }
    } else {
      await _registerOrSignIn(tester, email: email, password: password);
    }

    const prompt =
        'Reply with one short English sentence. Start it with a marker made by '
        'joining these three fragments with underscores: SPEAKUP, E2E, OK.';
    final marker = find.textContaining('SPEAKUP_E2E_OK');
    final previousMarkerCount = marker.evaluate().length;
    await tester.enterText(
      find.byKey(const Key('agent-composer-field')),
      prompt,
    );
    await _waitUntil(
      tester,
      () => _sendButtonIsEnabled(tester),
      const Duration(seconds: 5),
    );
    await tester.tap(find.byKey(const Key('agent-send-button')));
    await _waitUntil(
      tester,
      () => marker.evaluate().length > previousMarkerCount,
      const Duration(seconds: 90),
    );

    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pumpAndSettle();
    final agentScreenshot = await binding.takeScreenshot(
      'ios-real-qianwen-e2e',
    );
    expect(agentScreenshot, isNotEmpty);

    final audioAssetIds = await _completeRealVoicePractice(
      tester,
      controller: dependencies.practiceController,
      practiceAudioPlayer: practiceAudioPlayer,
      validateAudioMedia: validateAudioMedia,
    );
    final completedSessionId =
        dependencies.practiceController.practiceSessionId;
    if (completedSessionId == null) {
      fail('The completed Practice did not retain its Session identity.');
    }
    await tester.runAsync(
      () => dependencies.sessionEvaluationController.load(completedSessionId),
    );
    expect(
      dependencies.sessionEvaluationController.evaluation?.status,
      SessionEvaluationStatus.ready,
      reason: dependencies.sessionEvaluationController.errorMessage,
    );
    _expectScoredPracticeReport(
      dependencies.sessionEvaluationController.evaluation?.report,
      expectedAnsweredQuestions: 4,
    );
    await _deleteTestRecordings(dependencies.practiceController, audioAssetIds);
    deletedAudioAssetIds.addAll(audioAssetIds);
    await dependencies.reviewHistoryController.refresh();
    Navigator.of(
      tester.element(find.byKey(const Key('scenario-practice-page'))),
    ).pop();
    await tester.pumpAndSettle();
    await _tapPrimaryTab(tester, 2);
    await _waitForPersistedSessionReview(
      tester,
      controller: dependencies.reviewHistoryController,
      practiceSessionId: completedSessionId,
      timeout: const Duration(seconds: 15),
    );
    final completedReviewId = dependencies.reviewHistoryController.items
        .firstWhere((item) => item.practiceSessionId == completedSessionId)
        .review
        .id;
    final reviewScreenshot = await binding.takeScreenshot(
      'ios-real-voice-review-e2e',
    );
    expect(reviewScreenshot, isNotEmpty);
    await _signOut(tester);
    await _signIn(tester, email: email, password: password);
    await _tapPrimaryTab(tester, 2);
    await _waitForPersistedSessionReview(
      tester,
      controller: dependencies.reviewHistoryController,
      practiceSessionId: completedSessionId,
      timeout: const Duration(seconds: 20),
      additionalCondition: () =>
          find
              .byKey(Key('review-history-$completedReviewId'))
              .evaluate()
              .isNotEmpty &&
          find
              .byKey(const Key('review-content'))
              .hitTestable()
              .evaluate()
              .isNotEmpty,
    );
    final restoredReviewScreenshot = await binding.takeScreenshot(
      'ios-real-review-history-restore-e2e',
    );
    expect(restoredReviewScreenshot, isNotEmpty);
    debugPrint('SPEAKUP_E2E_CAPTURE_READY=true');
    if (captureHoldMs > 0) {
      await tester.runAsync(
        () => Future<void>.delayed(Duration(milliseconds: captureHoldMs)),
      );
    }
    await tester.pumpAndSettle();
    await _signOut(tester);
  });

  testWidgets('real iOS three practice hubs stay focused and reachable', (
    tester,
  ) async {
    final email =
        'practice-hubs-${DateTime.now().microsecondsSinceEpoch}@example.com';
    const password = 'Practice hubs e2e password 526';
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      sessionStore: _MemorySessionStore(),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        conversationController: dependencies.conversationController,
        composerController: dependencies.composerController,
        messageAudioController: dependencies.messageAudioController,
        messageTranslationClient: dependencies.messageTranslationClient,
        practiceController: dependencies.practiceController,
        preparationController: dependencies.preparationController,
        ieltsPreparationController: dependencies.ieltsPreparationController,
        jobPreparationController: dependencies.jobPreparationController,
        preparationLaunchController: dependencies.preparationLaunchController,
        reviewHistoryController: dependencies.reviewHistoryController,
      ),
    );
    await _waitUntil(
      tester,
      () =>
          find.text('欢迎回来').evaluate().isNotEmpty ||
          find.text('需要网络连接').evaluate().isNotEmpty ||
          find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty,
      const Duration(seconds: 15),
    );
    if (find.text('需要网络连接').evaluate().isNotEmpty) {
      await tester.tap(find.text('重试'));
      await tester.pump();
    }
    if (find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty) {
      final sameAccount = await _signedInAccountMatches(tester, email);
      if (!sameAccount) {
        await _signOut(tester);
        await _registerOrSignIn(tester, email: email, password: password);
      }
    } else {
      await _registerOrSignIn(tester, email: email, password: password);
    }

    await _tapPrimaryTab(tester, 1);
    await tester.pump();
    final interviewHub = find.byKey(const Key('practice-hub-interview'));
    await _waitForPreparationTarget(
      tester,
      target: interviewHub,
      operation: 'load the three product practice hubs',
      timeout: const Duration(seconds: 30),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-hub-exam')), findsOneWidget);
    expect(find.byKey(const Key('practice-hub-roleplay')), findsOneWidget);
    await _expectRealUiScreenshot(tester, binding, 'scenes-home');

    await _scrollPreparationIntoView(tester, interviewHub);
    await tester.tap(interviewHub);
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-hub-title-interview')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('open-job-preparation')), findsOneWidget);
    await _expectRealUiScreenshot(tester, binding, 'scenes-interview');

    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();
    final examHub = find.byKey(const Key('practice-hub-exam'));
    await _scrollPreparationIntoView(tester, examHub);
    await tester.tap(examHub);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('practice-hub-title-ielts')), findsOneWidget);
    expect(find.byKey(const Key('ielts-mode-full')), findsOneWidget);
    await _expectRealUiScreenshot(tester, binding, 'scenes-ielts');

    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();
    final roleplayHub = find.byKey(const Key('practice-hub-roleplay'));
    await _scrollPreparationIntoView(tester, roleplayHub);
    await tester.tap(roleplayHub);
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('practice-hub-title-roleplay')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('roleplay-filter-recommended')),
      findsOneWidget,
    );
    await _expectRealUiScreenshot(tester, binding, 'scenes-roleplay');

    await tester.tap(find.byKey(const Key('roleplay-filter-travel')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('catalog-scene-scn_travel_hotel_checkin')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();
    await _signOut(tester);
  });

  testWidgets('real iOS IELTS Part 1 creates a Practice Session', (
    tester,
  ) async {
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    final email =
        'ielts-launch-${DateTime.now().microsecondsSinceEpoch}@example.com';
    const password = 'IELTS launch e2e password 414';
    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      sessionStore: _MemorySessionStore(),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        conversationController: dependencies.conversationController,
        composerController: dependencies.composerController,
        messageAudioController: dependencies.messageAudioController,
        messageTranslationClient: dependencies.messageTranslationClient,
        practiceController: dependencies.practiceController,
        preparationController: dependencies.preparationController,
        ieltsPreparationController: dependencies.ieltsPreparationController,
        jobPreparationController: dependencies.jobPreparationController,
        preparationLaunchController: dependencies.preparationLaunchController,
        reviewHistoryController: dependencies.reviewHistoryController,
        sessionEvaluationController: dependencies.sessionEvaluationController,
      ),
    );

    await _registerOrSignIn(
      tester,
      email: email,
      password: password,
      requireFocusedConversation: false,
    );
    await _waitUntil(
      tester,
      () => !dependencies.conversationController.isBusy,
      const Duration(seconds: 20),
    );
    await _tapPrimaryTab(tester, 1);
    await tester.pump();

    final interviewHub = find.byKey(const Key('practice-hub-interview'));
    final examHub = find.byKey(const Key('practice-hub-exam'));
    final workplaceHub = find.byKey(const Key('practice-hub-workplace'));
    await _waitForPreparationTarget(
      tester,
      target: interviewHub,
      operation: 'load the four practice hubs',
      timeout: const Duration(seconds: 30),
    );
    expect(examHub, findsOneWidget);

    await _scrollPreparationIntoView(tester, interviewHub);
    await tester.tap(interviewHub);
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('practice-hub-title-interview')),
      operation: 'open the English interview hub',
      timeout: const Duration(seconds: 10),
    );
    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();

    await _scrollPreparationIntoView(tester, workplaceHub);
    await tester.tap(workplaceHub);
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('practice-hub-title-workplace')),
      operation: 'open the workplace hub',
      timeout: const Duration(seconds: 10),
    );
    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();

    await _scrollPreparationIntoView(tester, examHub);
    await tester.tap(examHub);
    await tester.pump();

    final questionSet = find.byKey(const Key('ielts-part1-set-p1-topic-001'));
    await _waitForPreparationTarget(
      tester,
      target: questionSet,
      operation: 'load the first IELTS Part 1 question topic',
      timeout: const Duration(seconds: 30),
    );
    await _scrollPreparationIntoView(tester, questionSet);
    await tester.tap(questionSet);
    await tester.pump();

    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-set-detail')),
      operation: 'open the IELTS Part 1 question set details',
      timeout: const Duration(seconds: 20),
    );
    await _tapPracticeControl(tester, const Key('ielts-set-detail-start'));

    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-mock-page')),
      operation: 'create and open the IELTS Part 1 Practice Session',
      timeout: const Duration(seconds: 90),
    );
    expect(dependencies.practiceController.practiceSessionId, isNotNull);
    expect(find.byKey(const Key('ielts-mock-part-1')), findsOneWidget);
    await _expectRealUiScreenshot(
      tester,
      binding,
      'ielts-part1-practice-started',
    );

    final firstSessionId = dependencies.practiceController.practiceSessionId!;
    final ended = await dependencies.practiceController
        .endActivePracticeEarly();
    expect(
      ended,
      isTrue,
      reason:
          'error=${dependencies.practiceController.errorMessage}; '
          'session=${dependencies.practiceController.practiceSessionId}; '
          'version=${dependencies.practiceController.practiceSessionVersion}; '
          'active=${dependencies.practiceController.hasActivePractice}',
    );
    expect(
      await dependencies.preparationLaunchController.resumeCurrentPractice(),
      isFalse,
    );
    expect(
      dependencies.preparationLaunchController.hasResumablePractice,
      isFalse,
    );

    await tester.tap(find.byKey(const Key('ielts-mock-exit')));
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-mock-exit-sheet')),
      operation: 'open the IELTS exit confirmation',
      timeout: const Duration(seconds: 10),
    );
    await _tapPracticeControl(tester, const Key('ielts-mock-save-and-exit'));
    await _waitForPreparationTarget(
      tester,
      target: examHub,
      operation: 'return to the practice hubs after ending the first session',
      timeout: const Duration(seconds: 20),
    );
    await _scrollPreparationIntoView(tester, examHub);
    await tester.tap(examHub);
    await _waitForPreparationTarget(
      tester,
      target: questionSet,
      operation: 'reload IELTS Part 1 after ending the first session',
      timeout: const Duration(seconds: 30),
    );
    await _scrollPreparationIntoView(tester, questionSet);
    await tester.tap(questionSet);
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-set-detail')),
      operation: 'reopen the IELTS Part 1 question set details',
      timeout: const Duration(seconds: 20),
    );
    await _tapPracticeControl(tester, const Key('ielts-set-detail-start'));
    await _waitUntil(tester, () {
      final currentSessionId =
          dependencies.practiceController.practiceSessionId;
      return currentSessionId != null && currentSessionId != firstSessionId;
    }, const Duration(seconds: 90));
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-mock-page')),
      operation: 'create a new IELTS Part 1 session after terminal recovery',
      timeout: const Duration(seconds: 90),
    );
    expect(
      dependencies.practiceController.practiceSessionId,
      isNot(firstSessionId),
    );
    final secondSessionId = dependencies.practiceController.practiceSessionId!;
    const answers = <String>[
      'I prefer happy music because its energetic rhythm improves my mood '
          'and helps me feel optimistic after a demanding day.',
      'Yes, happy music usually makes me feel more excited, especially when '
          'I listen to an upbeat song before exercising or meeting friends.',
      'I took piano classes for two years at primary school, which taught me '
          'basic rhythm and helped me appreciate how songs are composed.',
      'I often listen to quiet instrumental music while reading or doing '
          'routine work, but I turn it off when a task requires deep '
          'concentration.',
    ];
    await _completeIeltsPart1TextPractice(
      tester,
      controller: dependencies.practiceController,
      answers: answers,
    );
    expect(dependencies.practiceController.practiceSessionId, secondSessionId);
    expect(dependencies.practiceController.hasActivePractice, isFalse);
    expect(
      find.byKey(const Key('ielts-section-completion-sheet')),
      findsOneWidget,
    );
    await _tapPracticeControl(tester, const Key('ielts-section-review-action'));
    await _waitUntil(
      tester,
      () =>
          find
              .byKey(const Key('evaluation-report-detail-page'))
              .evaluate()
              .isNotEmpty ||
          dependencies.sessionEvaluationController.evaluation?.status ==
              SessionEvaluationStatus.failed ||
          dependencies.sessionEvaluationController.errorMessage != null,
      const Duration(minutes: 5),
    );
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsOneWidget,
      reason: dependencies.sessionEvaluationController.errorMessage,
    );
    expect(
      dependencies.sessionEvaluationController.evaluation?.status,
      SessionEvaluationStatus.ready,
      reason: dependencies.sessionEvaluationController.errorMessage,
    );
    _expectScoredIeltsPart1Report(
      dependencies.sessionEvaluationController.evaluation?.report,
      expectedAnswers: answers,
    );
  });

  testWidgets('real iOS Agent distinguishes chat from hotel Practice creation', (
    tester,
  ) async {
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    final email =
        'agent-hotel-${DateTime.now().microsecondsSinceEpoch}@example.com';
    const password = 'Agent hotel e2e password 731';
    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      sessionStore: _MemorySessionStore(),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        conversationController: dependencies.conversationController,
        composerController: dependencies.composerController,
        messageAudioController: dependencies.messageAudioController,
        messageTranslationClient: dependencies.messageTranslationClient,
        practiceController: dependencies.practiceController,
        preparationController: dependencies.preparationController,
        ieltsPreparationController: dependencies.ieltsPreparationController,
        jobPreparationController: dependencies.jobPreparationController,
        preparationLaunchController: dependencies.preparationLaunchController,
        practicePlanClientActionController:
            dependencies.practicePlanClientActionController,
        reviewHistoryController: dependencies.reviewHistoryController,
        sessionEvaluationController: dependencies.sessionEvaluationController,
      ),
    );

    await _registerOrSignIn(
      tester,
      email: email,
      password: password,
      requireFocusedConversation: false,
    );
    await _waitUntil(
      tester,
      () => !dependencies.conversationController.isBusy,
      const Duration(seconds: 20),
    );
    expect(await dependencies.conversationController.createThread(), isTrue);
    await _ensureWritableConversation(tester);

    final chat = await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: '你好，今天先随便聊两句，不要创建练习。',
    );
    expect(chat.text.trim(), isNotEmpty);
    expect(chat.clientActions.where(_isPracticePlanConfirmAction), isEmpty);
    expect(
      dependencies.conversationController.messages
          .expand((message) => message.clientActions)
          .where(_isPracticePlanConfirmAction),
      isEmpty,
    );

    final correction = await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: '做一个简单纠错练习：请纠正 I am agree with you，只回复正确说法，不要创建练习场景。',
    );
    expect(correction.text, contains('I agree with you'));
    expect(
      correction.clientActions.where(_isPracticePlanConfirmAction),
      isEmpty,
    );
    expect(
      dependencies.conversationController.messages
          .expand((message) => message.clientActions)
          .where(_isPracticePlanConfirmAction),
      isEmpty,
    );

    final hotel = await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: '明天去酒店办理入住，想练习跟英文前台核对预订和房型，直接创建。',
    );
    final hotelActions = hotel.clientActions
        .where(_isPracticePlanConfirmAction)
        .map(decodeConfirmPracticePlanClientAction)
        .toList(growable: false);
    expect(hotelActions, hasLength(1));
    final action = hotelActions.single;
    expect(action.sceneId, 'scn_travel_hotel_checkin');
    expect(action.practiceExperience, 'LIFE_AND_TRAVEL');
    expect(action.sceneCategory, 'LIFE_TRAVEL');
    expect(
      dependencies.conversationController.messages
          .expand((message) => message.clientActions)
          .where(_isPracticePlanConfirmAction),
      hasLength(1),
    );

    final card = find.byKey(
      Key(
        'agent-client-action-practice-plan-'
        '${action.practicePlanId}-${action.planVersion}',
      ),
    );
    final confirm = find.byKey(
      Key(
        'confirm-practice-plan-'
        '${action.practicePlanId}-${action.planVersion}',
      ),
    );
    expect(card, findsOneWidget);
    FocusManager.instance.primaryFocus?.unfocus();
    await tester.ensureVisible(confirm);
    await tester.pumpAndSettle();
    await _waitUntil(
      tester,
      () => confirm.hitTestable().evaluate().length == 1,
      const Duration(seconds: 10),
    );
    await tester.tap(confirm);
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('scenario-practice-page')),
      operation: 'open the Agent-created hotel Practice Session',
      timeout: const Duration(seconds: 90),
    );
    expect(dependencies.practiceController.practiceSessionId, isNotNull);
    expect(
      dependencies.practiceController.scene?.id,
      'scn_travel_hotel_checkin',
    );
    final sessionId = dependencies.practiceController.practiceSessionId;
    if (sessionId == null) {
      fail(
        'The Agent-created hotel Practice did not retain its Session identity.',
      );
    }
    await _completeTextPractice(
      tester,
      controller: dependencies.practiceController,
      answers: const [
        'Hello, I have a reservation under the name Li Qiang.',
        'I booked a quiet non-smoking room for two nights.',
        'Could you confirm breakfast and the check-out time, please?',
      ],
    );
    await tester.runAsync(
      () => dependencies.sessionEvaluationController.load(sessionId),
    );
    expect(
      dependencies.sessionEvaluationController.evaluation?.status,
      SessionEvaluationStatus.ready,
      reason: dependencies.sessionEvaluationController.errorMessage,
    );
    _expectScoredPracticeReport(
      dependencies.sessionEvaluationController.evaluation?.report,
      expectedAnsweredQuestions: 3,
    );
  });

  testWidgets('real iOS Agent recovers a short IELTS warm-up answer', (
    tester,
  ) async {
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    final email =
        'ielts-agent-${DateTime.now().microsecondsSinceEpoch}@example.com';
    const password = 'IELTS Agent e2e password 648';
    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      sessionStore: _MemorySessionStore(),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        conversationController: dependencies.conversationController,
        composerController: dependencies.composerController,
        messageAudioController: dependencies.messageAudioController,
        messageTranslationClient: dependencies.messageTranslationClient,
        practiceController: dependencies.practiceController,
        preparationController: dependencies.preparationController,
        ieltsPreparationController: dependencies.ieltsPreparationController,
        jobPreparationController: dependencies.jobPreparationController,
        preparationLaunchController: dependencies.preparationLaunchController,
        reviewHistoryController: dependencies.reviewHistoryController,
      ),
    );

    await _registerOrSignIn(
      tester,
      email: email,
      password: password,
      requireFocusedConversation: false,
    );
    await _waitUntil(
      tester,
      () => !dependencies.conversationController.isBusy,
      const Duration(seconds: 20),
    );
    expect(await dependencies.conversationController.createThread(), isTrue);
    await _ensureWritableConversation(tester);
    await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: '嗯，我最近在学雅思。',
    );
    await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: 'Part One',
    );
    final warmUp = await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: '呃你给我随便挑一个。',
    );
    expect(warmUp.clientActions, isEmpty);

    final confirmation = await _sendRealAgentMessage(
      tester,
      controller: dependencies.conversationController,
      text: '呃。 no person.',
    );
    final action = confirmation.clientActions
        .where(_isPracticePlanConfirmAction)
        .map(decodeConfirmPracticePlanClientAction)
        .single;
    expect(confirmation.text.trim(), isNot(warmUp.text.trim()));
    expect(
      dependencies.conversationController.messages
          .where(
            (message) =>
                message.role == AgentMessageRole.assistant &&
                message.text.trim() == warmUp.text.trim(),
          )
          .length,
      1,
    );
    expect(
      dependencies.conversationController.messages
          .expand((message) => message.clientActions)
          .where(_isPracticePlanConfirmAction)
          .length,
      1,
    );
    expect(action.practiceMode, 'PART_1');
    expect(
      find.byKey(
        Key(
          'agent-client-action-practice-plan-'
          '${action.practicePlanId}-${action.planVersion}',
        ),
      ),
      findsOneWidget,
    );
    final confirmationButton = find.byKey(
      Key(
        'confirm-practice-plan-'
        '${action.practicePlanId}-${action.planVersion}',
      ),
    );
    await _waitUntil(
      tester,
      () => confirmationButton.hitTestable().evaluate().length == 1,
      const Duration(seconds: 10),
    );
    await _expectRealUiScreenshot(tester, binding, 'agent-short-warmup-action');
    await tester.tap(confirmationButton);
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-mock-page')),
      operation: 'open the Agent-created IELTS Part 1 Practice Session',
      timeout: const Duration(seconds: 90),
    );
    expect(dependencies.practiceController.practiceSessionId, isNotNull);
    expect(
      await dependencies.practiceController.endActivePracticeEarly(),
      isTrue,
      reason: dependencies.practiceController.errorMessage,
    );
    expect(dependencies.practiceController.hasActivePractice, isFalse);
  });
}

bool _isPracticePlanConfirmAction(AgentClientAction action) {
  return action.type == practicePlanConfirmClientActionType;
}

Future<AgentMessage> _sendRealAgentMessage(
  WidgetTester tester, {
  required ConversationController controller,
  required String text,
}) async {
  await _waitUntil(
    tester,
    () => _composerIsReady(tester) && !controller.isBusy,
    const Duration(seconds: 20),
  );
  final previousAssistantIDs = <String>{
    for (final message in controller.messages)
      if (message.role == AgentMessageRole.assistant) message.id,
  };
  await tester.enterText(find.byKey(const Key('agent-composer-field')), text);
  await _waitUntil(
    tester,
    () => _sendButtonIsEnabled(tester),
    const Duration(seconds: 5),
  );
  await tester.tap(find.byKey(const Key('agent-send-button')));
  await _waitUntil(
    tester,
    () =>
        !controller.isBusy &&
        controller.messages.any(
          (message) =>
              message.role == AgentMessageRole.assistant &&
              !message.isStreaming &&
              !previousAssistantIDs.contains(message.id),
        ),
    const Duration(seconds: 90),
  );
  if (controller.errorMessage != null) {
    fail('Agent message failed: ${controller.errorMessage}');
  }
  return controller.messages.lastWhere(
    (message) =>
        message.role == AgentMessageRole.assistant &&
        !message.isStreaming &&
        !previousAssistantIDs.contains(message.id),
  );
}

Future<void> _expectRealUiScreenshot(
  WidgetTester tester,
  IntegrationTestWidgetsFlutterBinding binding,
  String name,
) async {
  // Let the on-device test pointer fade before taking a product screenshot.
  await tester.pump(const Duration(milliseconds: 600));
  final bytes = await binding.takeScreenshot('ios-real-$name');
  expect(bytes, isNotEmpty);
}

Future<void> _registerOrSignIn(
  WidgetTester tester, {
  required String email,
  required String password,
  bool requireFocusedConversation = true,
}) async {
  await _waitUntil(
    tester,
    () => find.text('欢迎回来').evaluate().isNotEmpty,
    const Duration(seconds: 15),
  );
  final openRegistration = find.widgetWithText(TextButton, '创建账号');
  await _waitUntil(
    tester,
    () => openRegistration.hitTestable().evaluate().length == 1,
    const Duration(seconds: 5),
  );
  await tester.ensureVisible(openRegistration);
  await tester.pumpAndSettle();
  await tester.tap(openRegistration);
  await tester.pumpAndSettle();
  await _waitUntil(
    tester,
    () =>
        find.widgetWithText(FilledButton, '创建账号').evaluate().length == 1 &&
        find.text('返回登录').evaluate().length == 1,
    const Duration(seconds: 5),
  );
  await tester.enterText(
    find.byKey(const Key('register-display-name')),
    'Codex QA',
  );
  await tester.enterText(find.byType(TextFormField).at(1), email);
  await tester.enterText(find.byType(TextFormField).at(2), password);
  await _tapAuthSubmit(tester, '创建账号');
  await _waitUntil(
    tester,
    () =>
        find.text('账号创建成功，请登录后继续。').evaluate().isNotEmpty ||
        find.text('无法使用这些信息创建账号。').evaluate().isNotEmpty,
    const Duration(seconds: 15),
  );
  if (find.text('无法使用这些信息创建账号。').evaluate().isNotEmpty) {
    await tester.tap(find.text('返回登录'));
    await _waitUntil(
      tester,
      () => find.text('欢迎回来').evaluate().isNotEmpty,
      const Duration(seconds: 5),
    );
  }

  await tester.enterText(find.byType(TextFormField).at(0), email);
  await tester.enterText(find.byType(TextFormField).at(1), password);
  await _tapAuthSubmit(tester, '登录');
  if (requireFocusedConversation) {
    await _ensureWritableConversation(tester);
  } else {
    await _waitUntil(
      tester,
      () =>
          find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty &&
          find.byKey(const Key('primary-navigation')).evaluate().isNotEmpty,
      const Duration(seconds: 20),
    );
  }
}

Future<void> _signIn(
  WidgetTester tester, {
  required String email,
  required String password,
}) async {
  await _waitUntil(
    tester,
    () => find.text('欢迎回来').evaluate().isNotEmpty,
    const Duration(seconds: 15),
  );
  await tester.enterText(find.byType(TextFormField).at(0), email);
  await tester.enterText(find.byType(TextFormField).at(1), password);
  await _tapAuthSubmit(tester, '登录');
  await _waitUntil(
    tester,
    () =>
        find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty &&
        find.byKey(const Key('primary-navigation')).evaluate().isNotEmpty,
    const Duration(seconds: 20),
  );
}

Future<void> _tapAuthSubmit(WidgetTester tester, String label) async {
  FocusManager.instance.primaryFocus?.unfocus();
  await tester.pump(const Duration(milliseconds: 200));
  final button = find.byType(FilledButton, skipOffstage: false);
  if (button.evaluate().length != 1) {
    fail(
      'Expected one auth submit button for $label, '
      'found ${button.evaluate().length}.',
    );
  }
  await tester.ensureVisible(button);
  await tester.pump(const Duration(milliseconds: 100));
  await tester.tap(button);
}

Future<void> _tapPrimaryTab(WidgetTester tester, int index) async {
  final navigation = find.byKey(const Key('primary-navigation'));
  await _waitUntil(
    tester,
    () => navigation.evaluate().length == 1,
    const Duration(seconds: 20),
  );
  expect(find.byType(UiKitView), findsOneWidget);
  ByteData? response;
  // UIKit view IDs increase when auth rebuilds the native tab bar.
  for (var viewId = 0; viewId < 256 && response == null; viewId++) {
    response = await tester.binding.defaultBinaryMessenger
        .handlePlatformMessage(
          'speakup/native_tab_bar/$viewId',
          const StandardMethodCodec().encodeMethodCall(
            MethodCall('onSelected', index),
          ),
          null,
        );
  }
  expect(response, isNotNull);
  expect(const StandardMethodCodec().decodeEnvelope(response!), index);
  await tester.pump();
}

Future<List<String>> _completeRealVoicePractice(
  WidgetTester tester, {
  required PracticeController controller,
  required _ObservedPracticeAudioPlayer practiceAudioPlayer,
  required bool validateAudioMedia,
}) async {
  FocusManager.instance.primaryFocus?.unfocus();
  await _tapPrimaryTab(tester, 1);
  await tester.pump();
  final interviewHub = find.byKey(const Key('practice-hub-interview'));
  final workplaceHub = find.byKey(const Key('practice-hub-workplace'));
  await _waitForPreparationTarget(
    tester,
    target: interviewHub,
    operation: 'load the three practice hubs',
    timeout: const Duration(seconds: 30),
  );
  await _showPracticeHub(tester, workplaceHub);
  await tester.tap(workplaceHub);
  await tester.pump();
  final scene = find.byKey(
    const Key('catalog-scene-scn_workplace_progress_risk_update'),
  );
  await _waitForPreparationTarget(
    tester,
    target: scene,
    operation: 'load the workplace practice catalog',
    timeout: const Duration(seconds: 30),
  );
  await _scrollPreparationIntoView(tester, scene);
  await tester.tap(scene);
  await tester.pump();
  await _waitForPreparationTarget(
    tester,
    target: find.byKey(const Key('scenario-practice-page')),
    operation: 'create and open the workplace Practice Session',
    timeout: const Duration(seconds: 90),
  );
  await tester.pumpAndSettle();

  for (var turn = 1; turn <= 4; turn++) {
    final questionId = controller.questionId;
    expect(questionId, isNotNull);
    if (validateAudioMedia) {
      await _validateQuestionTts(controller);
    }
    await _tapPracticeControl(tester, const Key('scenario-record'));
    await _waitUntil(
      tester,
      () =>
          find
              .byKey(const Key('scenario-stop-recording'))
              .evaluate()
              .isNotEmpty ||
          controller.errorMessage != null,
      const Duration(seconds: 10),
    );
    _failOnPracticeControllerError(
      controller,
      'start recording for turn $turn',
    );

    await _tapPracticeControl(tester, const Key('scenario-stop-recording'));
    await _waitUntil(
      tester,
      () =>
          controller.completedTurns >= turn || controller.errorMessage != null,
      const Duration(seconds: 90),
    );
    _failOnPracticeControllerError(controller, 'send voice turn $turn');
    expect(controller.hasPendingPracticeAudio, isFalse);
    final userMessage = controller.practiceMessages.lastWhere(
      (message) => message.role == PracticeMessageRole.user,
    );
    expect(userMessage.text.trim(), isNotEmpty);
    expect(controller.completedTurns, turn);
    expect(controller.questionId, isNot(questionId));
    if (validateAudioMedia) {
      expect(controller.recordings, hasLength(turn));
      await _validateRecordingPlayback(
        controller,
        controller.recordings.last.audioAssetId,
        practiceAudioPlayer,
      );
    }
  }

  await _tapPracticeControl(tester, const Key('scenario-complete-practice'));
  await _waitForPreparationTarget(
    tester,
    target: find.byKey(const Key('scenario-confirm-completion')),
    operation: 'confirm the user-controlled workplace Practice Session',
    timeout: const Duration(seconds: 10),
  );
  await _tapPracticeControl(tester, const Key('scenario-confirm-completion'));
  await _waitUntil(
    tester,
    () =>
        find
            .byKey(const Key('scenario-completion-overlay'))
            .evaluate()
            .isNotEmpty ||
        controller.errorMessage != null,
    const Duration(seconds: 90),
  );
  _failOnPracticeControllerError(controller, 'complete the Practice Session');
  expect(find.byKey(const Key('scenario-practice-page')), findsOneWidget);
  expect(find.byKey(const Key('scenario-completion-overlay')), findsOneWidget);
  final audioAssetIds = [
    for (final recording in controller.recordings) recording.audioAssetId,
  ];
  if (validateAudioMedia) {
    expect(audioAssetIds, hasLength(4));
    expect(audioAssetIds.toSet(), hasLength(4));
  }
  return audioAssetIds;
}

Future<void> _completeTextPractice(
  WidgetTester tester, {
  required PracticeController controller,
  required List<String> answers,
}) async {
  for (var index = 0; index < answers.length; index++) {
    final questionId = controller.questionId;
    expect(questionId, isNotNull);
    await _tapPracticeControl(tester, const Key('scenario-open-keyboard'));
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('scenario-text-answer')),
      operation: 'open the hotel Practice text composer',
      timeout: const Duration(seconds: 10),
    );
    await tester.enterText(
      find.byKey(const Key('scenario-text-answer')),
      answers[index],
    );
    await _tapPracticeControl(tester, const Key('scenario-submit-text'));
    await _waitUntil(
      tester,
      () =>
          controller.completedTurns >= index + 1 ||
          controller.errorMessage != null,
      const Duration(seconds: 90),
    );
    _failOnPracticeControllerError(
      controller,
      'send hotel text turn ${index + 1}',
    );
    expect(controller.completedTurns, index + 1);
    expect(controller.questionId, isNot(questionId));
  }

  await _tapPracticeControl(tester, const Key('scenario-complete-practice'));
  await _waitForPreparationTarget(
    tester,
    target: find.byKey(const Key('scenario-confirm-completion')),
    operation: 'confirm the Agent-created hotel Practice Session',
    timeout: const Duration(seconds: 10),
  );
  await _tapPracticeControl(tester, const Key('scenario-confirm-completion'));
  await _waitUntil(
    tester,
    () =>
        find
            .byKey(const Key('scenario-completion-overlay'))
            .evaluate()
            .isNotEmpty ||
        controller.errorMessage != null,
    const Duration(seconds: 90),
  );
  _failOnPracticeControllerError(
    controller,
    'complete the Agent-created hotel Practice Session',
  );
  expect(find.byKey(const Key('scenario-completion-overlay')), findsOneWidget);
}

Future<void> _completeIeltsPart1TextPractice(
  WidgetTester tester, {
  required PracticeController controller,
  required List<String> answers,
}) async {
  expect(answers, hasLength(4));
  expect(answers.toSet(), hasLength(4));
  for (var index = 0; index < answers.length; index++) {
    final questionId = controller.questionId;
    expect(questionId, isNotNull);
    await _tapPracticeControl(tester, const Key('ielts-mock-open-keyboard'));
    await _waitForPreparationTarget(
      tester,
      target: find.byKey(const Key('ielts-mock-converted-answer-field')),
      operation: 'open IELTS Part 1 text answer ${index + 1}',
      timeout: const Duration(seconds: 10),
    );
    await tester.enterText(
      find.byKey(const Key('ielts-mock-converted-answer-field')),
      answers[index],
    );
    await _tapPracticeControl(
      tester,
      const Key('ielts-mock-submit-converted-answer'),
    );
    await _waitUntil(
      tester,
      () =>
          controller.completedTurns >= index + 1 ||
          controller.errorMessage != null,
      const Duration(seconds: 90),
    );
    _failOnPracticeControllerError(
      controller,
      'submit IELTS Part 1 text answer ${index + 1}',
    );
    expect(controller.completedTurns, index + 1);
    if (index < answers.length - 1) {
      expect(controller.questionId, isNot(questionId));
    }
  }
  await _waitForPreparationTarget(
    tester,
    target: find.byKey(const Key('ielts-section-completion-sheet')),
    operation: 'show the completed IELTS Part 1 review action',
    timeout: const Duration(seconds: 20),
  );
}

Future<void> _deleteTestRecordings(
  PracticeController controller,
  List<String> audioAssetIds,
) async {
  for (final deletedId in audioAssetIds) {
    await controller.deleteRecording(deletedId);
    expect(
      controller.recordings.any(
        (recording) => recording.audioAssetId == deletedId,
      ),
      isFalse,
    );
    await expectLater(
      controller.mediaClient!.loadRecording(deletedId),
      throwsA(
        isA<PracticeClientException>().having(
          (error) => error.kind,
          'kind',
          PracticeClientFailureKind.notFound,
        ),
      ),
    );
  }
}

Future<void> _tapPracticeControl(WidgetTester tester, Key key) async {
  final control = find.byKey(key);
  await _waitUntil(
    tester,
    () => control.evaluate().isNotEmpty,
    const Duration(seconds: 10),
  );
  await tester.ensureVisible(control);
  await tester.pump(const Duration(milliseconds: 100));
  if (control.hitTestable().evaluate().isEmpty) {
    fail('Practice control $key is not tappable.');
  }
  await tester.tap(control);
}

Future<void> _waitForPreparationTarget(
  WidgetTester tester, {
  required Finder target,
  required String operation,
  required Duration timeout,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    final failure = _preparationFailure(tester);
    if (failure != null) {
      fail('Failed to $operation: $failure');
    }
    if (target.evaluate().isNotEmpty) {
      return;
    }
    await tester.pump(const Duration(milliseconds: 250));
  }

  final pending = _preparationPendingState(tester);
  fail('Timed out waiting to $operation. Current state: $pending.');
}

Future<void> _scrollPreparationIntoView(
  WidgetTester tester,
  Finder target,
) async {
  if (target.hitTestable().evaluate().isEmpty) {
    final scrollable = find.byType(Scrollable).first;
    if (scrollable.evaluate().isEmpty) {
      fail('Preparation content has no scrollable surface.');
    }
    await tester.scrollUntilVisible(target, 240, scrollable: scrollable);
  }
  await tester.ensureVisible(target);
  await tester.pump(const Duration(milliseconds: 100));
  if (target.hitTestable().evaluate().isEmpty) {
    fail('Preparation control $target is not tappable.');
  }
}

Future<void> _showPracticeHub(WidgetTester tester, Finder target) async {
  final carousel = find.byKey(const Key('practice-hub-carousel'));
  await _waitUntil(
    tester,
    () => carousel.evaluate().isNotEmpty,
    const Duration(seconds: 10),
  );
  for (var attempt = 0; attempt < 6; attempt++) {
    if (target.hitTestable().evaluate().isNotEmpty) {
      return;
    }
    await tester.drag(
      carousel,
      Offset(-tester.getSize(carousel).width * 0.8, 0),
    );
    await tester.pumpAndSettle();
  }
  fail('Practice hub $target is not reachable in the carousel.');
}

String? _preparationFailure(WidgetTester tester) {
  const failures = <(String, String)>[
    ('preparation-catalog-error', 'catalog request failed'),
    ('preparation-detail-error', 'scene detail request failed'),
    ('preparation-launch-error', 'practice launch failed'),
  ];
  for (final (key, fallback) in failures) {
    final finder = find.byKey(Key(key));
    if (finder.evaluate().isNotEmpty) {
      final message = _visibleText(tester, finder);
      return message.isEmpty ? fallback : '$fallback: $message';
    }
  }
  return null;
}

String _preparationPendingState(WidgetTester tester) {
  if (find
      .byKey(const Key('preparation-launch-progress'))
      .evaluate()
      .isNotEmpty) {
    final stage = _visibleText(
      tester,
      find.byKey(const Key('preparation-launch-stage')),
    );
    return stage.isEmpty ? 'practice launch is still running' : stage;
  }
  if (find
      .byKey(const Key('preparation-detail-loading'))
      .evaluate()
      .isNotEmpty) {
    return 'scene detail is still loading';
  }
  if (find
      .byKey(const Key('preparation-catalog-loading'))
      .evaluate()
      .isNotEmpty) {
    return 'preparation catalog is still loading';
  }
  return 'the expected preparation control is absent';
}

String _visibleText(WidgetTester tester, Finder root) {
  final values = <String>[
    for (final element in root.evaluate())
      if (element.widget case final Text text)
        if ((text.data ?? '').trim().isNotEmpty) text.data!.trim(),
    for (final text in tester.widgetList<Text>(
      find.descendant(of: root, matching: find.byType(Text)),
    ))
      if ((text.data ?? '').trim().isNotEmpty) text.data!.trim(),
  ];
  return values.toSet().join(' ');
}

Future<void> _validateQuestionTts(PracticeController controller) async {
  expect(controller.canPlayQuestionAudio, isTrue);
  await controller.toggleQuestionAudio();
  expect(controller.isQuestionAudioLoading, isFalse);
  expect(controller.mediaErrorMessage, isNull);
  await controller.stopPracticeAudio();
}

Future<void> _validateRecordingPlayback(
  PracticeController controller,
  String audioAssetId,
  _ObservedPracticeAudioPlayer practiceAudioPlayer,
) async {
  final previousPlaybackCount = practiceAudioPlayer.successfulPlaybackCount;
  expect(audioAssetId, isNotEmpty);
  await controller.stopPracticeAudio();
  WidgetsBinding.instance.handleAppLifecycleStateChanged(
    AppLifecycleState.resumed,
  );
  await controller.toggleRecordingAudio(audioAssetId);
  expect(controller.isRecordingAudioLoading(audioAssetId), isFalse);
  expect(controller.mediaErrorMessage, isNull);
  expect(controller.isRecordingAudioPlaying(audioAssetId), isTrue);
  expect(
    practiceAudioPlayer.successfulPlaybackCount,
    previousPlaybackCount + 1,
  );
  await controller.stopPracticeAudio();
}

void _expectScoredPracticeReport(
  EvaluationReport? report, {
  required int expectedAnsweredQuestions,
}) {
  expect(report, isNotNull);
  final value = report!;
  expect(value.scoreability, EvaluationReportScoreability.provisional);
  expect(value.summary.trim(), isNotEmpty);
  expect(
    value.questions.where((question) => question.answer != null),
    hasLength(expectedAnsweredQuestions),
  );
  expect(
    value.questions.every(
      (question) => question.answer?.transcript.trim().isNotEmpty ?? false,
    ),
    isTrue,
  );
  expect(
    value.dimensions.map((dimension) => dimension.key).toList(growable: false),
    const [
      'TASK_ACHIEVEMENT',
      'CLARITY_COHERENCE',
      'LANGUAGE_CONTROL',
      'INTERACTION',
    ],
  );
  expect(
    value.dimensions.every(
      (dimension) =>
          dimension.score != null &&
          dimension.score! >= 0 &&
          dimension.score! <= 100,
    ),
    isTrue,
  );
  expect(
    value.dimensions.every(
      (dimension) =>
          dimension.coverage >= 0 &&
          dimension.coverage <= 1 &&
          dimension.confidence >= 0 &&
          dimension.confidence <= 1,
    ),
    isTrue,
  );
  final findings = <EvaluationReportFinding>[
    for (final dimension in value.dimensions) ...[
      ...dimension.strengths,
      ...dimension.improvements,
      ...dimension.recommendedExamples,
    ],
  ];
  expect(
    findings.any(
      (finding) =>
          finding.message.trim().isNotEmpty &&
          finding.evidence.isNotEmpty &&
          finding.evidence.every(
            (evidence) =>
                evidence.originalExcerpt.trim().isNotEmpty &&
                evidence.startUtf8Byte >= 0 &&
                evidence.endUtf8Byte > evidence.startUtf8Byte,
          ),
    ),
    isTrue,
  );
}

void _expectScoredIeltsPart1Report(
  EvaluationReport? report, {
  required List<String> expectedAnswers,
}) {
  expect(report, isNotNull);
  final value = report!;
  expect(value.sceneType, EvaluationReportSceneType.ieltsSpeaking);
  expect(value.scoreability, EvaluationReportScoreability.provisional);
  expect(value.summary.trim(), isNotEmpty);
  expect(
    value.questions.map((question) => question.answer?.transcript).toList(),
    expectedAnswers,
  );
  expect(value.dimensions.map((dimension) => dimension.key).toList(), const [
    'FLUENCY_COHERENCE',
    'LEXICAL_RESOURCE',
    'GRAMMATICAL_RANGE_ACCURACY',
    'PRONUNCIATION',
  ]);
  for (final dimension in value.dimensions) {
    expect(dimension.scale, EvaluationReportScoreScale.ieltsBand);
    expect(dimension.coverage, inInclusiveRange(0, 1));
    expect(dimension.confidence, inInclusiveRange(0, 1));
    if (dimension.key == 'PRONUNCIATION') {
      expect(dimension.score, isNull);
      expect(
        dimension.reasonCodes,
        contains('ACOUSTIC_ASSESSMENT_NOT_CONFIGURED'),
      );
      continue;
    }
    expect(dimension.score, isNotNull);
    expect(dimension.score!, inInclusiveRange(0, 9));
    expect(dimension.score! * 2, (dimension.score! * 2).roundToDouble());
  }
}

Future<void> _waitForPersistedSessionReview(
  WidgetTester tester, {
  required ReviewHistoryController controller,
  required String practiceSessionId,
  required Duration timeout,
  bool Function()? additionalCondition,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    final loaded = controller.items.any(
      (item) => item.practiceSessionId == practiceSessionId,
    );
    if (loaded && (additionalCondition?.call() ?? true)) {
      return;
    }
    final error = controller.errorMessage;
    if (error != null && error.isNotEmpty) {
      fail('Failed to load persisted Review for $practiceSessionId: $error');
    }
    await tester.pump(const Duration(milliseconds: 250));
  }
  fail('Timed out waiting for persisted Review for $practiceSessionId.');
}

void _failOnPracticeControllerError(
  PracticeController controller,
  String operation,
) {
  final error = controller.errorMessage ?? controller.mediaErrorMessage;
  if (error != null && error.isNotEmpty) {
    fail('Failed to $operation: $error');
  }
}

Future<bool> _signedInAccountMatches(
  WidgetTester tester,
  String expectedEmail,
) async {
  await _tapPrimaryTab(tester, 3);
  await _waitUntil(
    tester,
    () => find.byKey(const Key('profile-page')).evaluate().isNotEmpty,
    const Duration(seconds: 5),
  );
  if (find.text(expectedEmail).evaluate().isEmpty) {
    return false;
  }
  await _tapPrimaryTab(tester, 0);
  await _ensureWritableConversation(tester);
  return true;
}

Future<void> _signOut(WidgetTester tester) async {
  await _tapPrimaryTab(tester, 3);
  await _waitUntil(
    tester,
    () => find.byKey(const Key('profile-page')).evaluate().isNotEmpty,
    const Duration(seconds: 5),
  );
  await tester.tap(find.byKey(const Key('profile-logout-button')));
  await _waitUntil(
    tester,
    () => find.text('欢迎回来').evaluate().isNotEmpty,
    const Duration(seconds: 15),
  );
}

bool _composerIsReady(WidgetTester tester) {
  final composer = find.byKey(const Key('agent-composer-field'));
  if (composer.evaluate().length != 1 ||
      find.byKey(const Key('agent-operation-progress')).evaluate().isNotEmpty) {
    return false;
  }
  return tester.widget<TextField>(composer).enabled == true;
}

Future<void> _ensureWritableConversation(WidgetTester tester) async {
  final showTextComposer = find.byKey(const Key('agent-show-text-composer'));
  await _waitUntil(
    tester,
    () =>
        find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty &&
        (_composerIsReady(tester) ||
            showTextComposer.hitTestable().evaluate().length == 1),
    const Duration(seconds: 20),
  );
  if (_composerIsReady(tester)) {
    return;
  }

  await tester.tap(showTextComposer);
  await tester.pump();
  await _waitUntil(
    tester,
    () => _composerIsReady(tester),
    const Duration(seconds: 20),
  );
}

bool _sendButtonIsEnabled(WidgetTester tester) {
  final sendButton = find.byKey(const Key('agent-send-button'));
  return sendButton.evaluate().length == 1 &&
      tester.widget<IconButton>(sendButton).onPressed != null;
}

Future<void> _waitUntil(
  WidgetTester tester,
  bool Function() condition,
  Duration timeout,
) async {
  final deadline = DateTime.now().add(timeout);
  while (!condition() && DateTime.now().isBefore(deadline)) {
    await tester.pump(const Duration(milliseconds: 250));
  }
  if (!condition()) {
    fail('Timed out waiting for the expected E2E state.');
  }
}

List<int> _decodeVoiceFixture(String encoded, {required String variableName}) {
  if (encoded.isEmpty) {
    fail(
      '$variableName must contain a private spoken-English WAV '
      'fixture supplied at test time.',
    );
  }
  late final List<int> bytes;
  try {
    bytes = base64Decode(encoded);
  } on FormatException {
    fail('$variableName is not valid Base64.');
  }
  if (bytes.length <= 44 ||
      ascii.decode(bytes.sublist(0, 4), allowInvalid: true) != 'RIFF' ||
      ascii.decode(bytes.sublist(8, 12), allowInvalid: true) != 'WAVE') {
    fail('$variableName must decode to a non-empty WAV file.');
  }
  return bytes;
}

final class _MemorySessionStore implements SessionStore {
  String? _token;

  @override
  Future<void> deleteToken() async => _token = null;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String token) async => _token = token;
}

final class _FixturePracticeRecorder implements PracticeRecorder {
  _FixturePracticeRecorder(this._fixtures);

  final List<List<int>> _fixtures;
  File? _currentFile;
  bool _recording = false;
  int _nextFixture = 0;

  @override
  Future<void> start() async {
    if (_recording) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.alreadyRecording,
      );
    }
    _recording = true;
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    if (!_recording) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.notRecording,
      );
    }
    _recording = false;
    if (_nextFixture >= _fixtures.length) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.unavailable,
      );
    }
    final directory = await getTemporaryDirectory();
    final sequence = ++_nextFixture;
    final file = File('${directory.path}/speakup-real-e2e-$sequence.wav');
    await file.writeAsBytes(_fixtures[sequence - 1], flush: true);
    _currentFile = file;
    return RecordedPracticeAudio(
      path: file.path,
      contentType: 'audio/wav',
      sizeBytes: await file.length(),
    );
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {
    final file = File(audio.path);
    if (await file.exists()) {
      await file.delete();
    }
    if (_currentFile?.path == audio.path) {
      _currentFile = null;
    }
  }

  @override
  Future<void> discardCurrent() async {
    _recording = false;
    final file = _currentFile;
    _currentFile = null;
    if (file != null && await file.exists()) {
      await file.delete();
    }
  }

  @override
  Future<void> clearAccountState() => discardCurrent();
}

final class _ObservedPracticeAudioPlayer implements PracticeAudioPlayer {
  _ObservedPracticeAudioPlayer()
    : _delegate = AudioplayersPracticeAudioPlayer();

  final PracticeAudioPlayer _delegate;
  int successfulPlaybackCount = 0;

  @override
  Stream<void> get onComplete => _delegate.onComplete;

  @override
  Future<void> playWav(Uint8List bytes) async {
    await _delegate.playWav(bytes);
    successfulPlaybackCount++;
  }

  @override
  Future<void> stop() => _delegate.stop();

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<void> dispose() => _delegate.dispose();
}
