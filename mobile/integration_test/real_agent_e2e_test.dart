import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:path_provider/path_provider.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/main.dart' as app;
import 'package:speakup/practice/practice_recording.dart';
import 'package:speakup/review/review_history_controller.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('real iOS identity, Qianwen Agent, voice, and Review path', (
    tester,
  ) async {
    const email = String.fromEnvironment('SPEAKUP_E2E_EMAIL');
    const password = String.fromEnvironment('SPEAKUP_E2E_PASSWORD');
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    const voiceWavBase64 = String.fromEnvironment('SPEAKUP_E2E_WAV_BASE64');
    const captureHoldMs = int.fromEnvironment('SPEAKUP_E2E_CAPTURE_HOLD_MS');
    const validateAudioMedia = bool.fromEnvironment(
      'SPEAKUP_E2E_VALIDATE_AUDIO_MEDIA',
    );
    if (email.isEmpty || password.runes.length < 8) {
      fail('A disposable E2E account with a valid password is required.');
    }
    final voiceFixture = _decodeVoiceFixture(voiceWavBase64);

    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
      practiceRecorder: _FixturePracticeRecorder(voiceFixture),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        agentController: dependencies.agentController,
        preparationController: dependencies.preparationController,
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

    await _completeRealVoicePractice(
      tester,
      controller: dependencies.agentController,
      validateAudioMedia: validateAudioMedia,
    );
    final completedReviewId = dependencies.agentController.review?.id;
    if (completedReviewId == null) {
      fail('The completed Practice did not expose its server Review identity.');
    }
    await _waitForPersistedReview(
      tester,
      controller: dependencies.reviewHistoryController,
      reviewId: completedReviewId,
      timeout: const Duration(seconds: 15),
    );
    final reviewScreenshot = await binding.takeScreenshot(
      'ios-real-voice-review-e2e',
    );
    expect(reviewScreenshot, isNotEmpty);

    await _signOut(tester);
    await _signIn(tester, email: email, password: password);
    await tester.tap(find.byKey(const Key('primary-tab-review')));
    await _waitForPersistedReview(
      tester,
      controller: dependencies.reviewHistoryController,
      reviewId: completedReviewId,
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
    const email = String.fromEnvironment('SPEAKUP_E2E_EMAIL');
    const password = String.fromEnvironment('SPEAKUP_E2E_PASSWORD');
    const apiBaseUrl = String.fromEnvironment(
      'SPEAKUP_API_BASE_URL',
      defaultValue: 'http://127.0.0.1:8080',
    );
    if (email.isEmpty || password.runes.length < 8) {
      fail('A disposable E2E account with a valid password is required.');
    }

    final dependencies = app.createProductionAppDependencies(
      baseUri: Uri.parse(apiBaseUrl),
    );
    runApp(
      SpeakUpApp(
        authController: dependencies.authController,
        agentController: dependencies.agentController,
        preparationController: dependencies.preparationController,
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

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
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
      find.byKey(const Key('catalog-scenario-scn_daily_hotel_checkin_issue')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('preparation-back-to-families')));
    await tester.pumpAndSettle();
    await _signOut(tester);
  });
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
  await _ensureFocusedConversation(tester);
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
        find.byKey(const Key('primary-tab-review')).evaluate().isNotEmpty,
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

Future<void> _completeRealVoicePractice(
  WidgetTester tester, {
  required AgentController controller,
  required bool validateAudioMedia,
}) async {
  FocusManager.instance.primaryFocus?.unfocus();
  await tester.tap(find.byKey(const Key('primary-tab-scenes')));
  await tester.pump();
  final interviewHub = find.byKey(const Key('practice-hub-interview'));
  await _waitForPreparationTarget(
    tester,
    target: interviewHub,
    operation: 'load the three practice hubs',
    timeout: const Duration(seconds: 30),
  );
  await _scrollPreparationIntoView(tester, interviewHub);
  await tester.tap(interviewHub);
  await tester.pump();
  final scenario = find.byKey(
    const Key('catalog-scenario-scn_programmer_interview'),
  );
  if (scenario.evaluate().isEmpty) {
    final professionalMode = find.byKey(
      const Key('interview-mode-professional'),
    );
    await _waitForPreparationTarget(
      tester,
      target: professionalMode,
      operation: 'load the direct professional interview entry',
      timeout: const Duration(seconds: 30),
    );
    await _scrollPreparationIntoView(tester, professionalMode);
    await tester.tap(professionalMode);
    await tester.pumpAndSettle();
  }
  await _waitForPreparationTarget(
    tester,
    target: scenario,
    operation: 'load the formal preparation catalog',
    timeout: const Duration(seconds: 30),
  );
  await _scrollPreparationIntoView(tester, scenario);
  await tester.tap(scenario);
  await tester.pump();

  final role = find.byKey(
    const Key('preparation-role-role_technical_interviewer'),
  );
  await _waitForPreparationTarget(
    tester,
    target: role,
    operation: 'load the technical interview preparation detail',
    timeout: const Duration(seconds: 30),
  );
  await _scrollPreparationIntoView(tester, role);
  await tester.tap(role);
  await tester.pump();

  final option = find.byKey(
    const Key('preparation-option-option_technical_focus'),
  );
  await _scrollPreparationIntoView(tester, option);
  await tester.tap(option);
  await tester.pump();

  final background = find.byKey(const Key('preparation-background-summary'));
  await _scrollPreparationIntoView(tester, background);
  await tester.enterText(
    background,
    'Backend engineer preparing for a technical interview. '
    'Focus on concise spoken explanations of engineering trade-offs.',
  );
  FocusManager.instance.primaryFocus?.unfocus();
  await tester.pump(const Duration(milliseconds: 300));

  final start = find.byKey(const Key('preparation-start-practice'));
  await _scrollPreparationIntoView(tester, start);
  await tester.tap(start);
  await tester.pump();
  await _waitForPreparationTarget(
    tester,
    target: find.byKey(const Key('practice-page')),
    operation: 'create and open the formal FOCUS Practice Session',
    timeout: const Duration(seconds: 90),
  );
  await tester.pumpAndSettle();

  for (var turn = 1; turn <= 3; turn++) {
    if (validateAudioMedia) {
      await _validateQuestionTts(controller);
    }
    await _tapPracticeControl(tester, const Key('practice-record'));
    await _waitUntil(
      tester,
      () =>
          find
              .byKey(const Key('practice-stop-recording'))
              .evaluate()
              .isNotEmpty ||
          find.byKey(const Key('practice-error-message')).evaluate().isNotEmpty,
      const Duration(seconds: 10),
    );
    _failOnPracticeError(tester, 'start recording for turn $turn');

    await _tapPracticeControl(tester, const Key('practice-stop-recording'));
    await _waitUntil(
      tester,
      () =>
          find.byKey(const Key('practice-transcript')).evaluate().isNotEmpty ||
          find.byKey(const Key('practice-error-message')).evaluate().isNotEmpty,
      const Duration(seconds: 90),
    );
    _failOnPracticeError(tester, 'transcribe turn $turn');
    expect(
      tester
          .widget<Text>(find.byKey(const Key('practice-transcript')))
          .data
          ?.trim(),
      isNotEmpty,
    );

    await _tapPracticeControl(tester, const Key('practice-confirm-turn'));
    await tester.pump();
    if (turn < 3) {
      await _waitUntil(
        tester,
        () =>
            find.byKey(const Key('practice-record')).evaluate().isNotEmpty ||
            find
                .byKey(const Key('practice-error-message'))
                .evaluate()
                .isNotEmpty,
        const Duration(seconds: 45),
      );
      _failOnPracticeError(tester, 'confirm turn $turn');
      expect(find.text('$turn / 3'), findsOneWidget);
    } else {
      await _waitForRealReview(tester);
    }
    if (validateAudioMedia) {
      expect(controller.recordings, hasLength(turn));
      await _validateRecordingPlayback(
        controller,
        controller.recordings.last.audioAssetId,
      );
    }
  }

  expect(find.byKey(const Key('practice-page')), findsNothing);
  expect(find.byKey(const Key('review-content')).hitTestable(), findsOneWidget);
  expect(find.byKey(const Key('review-title')).hitTestable(), findsOneWidget);
  if (validateAudioMedia) {
    final audioAssetIds = [
      for (final recording in controller.recordings) recording.audioAssetId,
    ];
    expect(audioAssetIds, hasLength(3));
    expect(audioAssetIds.toSet(), hasLength(3));
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
          isA<AgentClientException>().having(
            (error) => error.kind,
            'kind',
            AgentClientFailureKind.notFound,
          ),
        ),
      );
    }
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

String? _preparationFailure(WidgetTester tester) {
  const failures = <(String, String)>[
    ('preparation-catalog-error', 'catalog request failed'),
    ('preparation-detail-error', 'scenario detail request failed'),
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
    return 'scenario detail is still loading';
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

Future<void> _validateQuestionTts(AgentController controller) async {
  expect(controller.canPlayQuestionAudio, isTrue);
  await controller.toggleQuestionAudio();
  expect(controller.isQuestionAudioLoading, isFalse);
  expect(controller.mediaErrorMessage, isNull);
  expect(controller.isQuestionAudioPlaying, isTrue);
  await controller.stopPracticeAudio();
}

Future<void> _validateRecordingPlayback(
  AgentController controller,
  String audioAssetId,
) async {
  expect(audioAssetId, isNotEmpty);
  await controller.toggleRecordingAudio(audioAssetId);
  expect(controller.isRecordingAudioLoading(audioAssetId), isFalse);
  expect(controller.mediaErrorMessage, isNull);
  expect(controller.isRecordingAudioPlaying(audioAssetId), isTrue);
  await controller.stopPracticeAudio();
}

Future<void> _waitForRealReview(WidgetTester tester) async {
  for (var attempt = 0; attempt < 4; attempt++) {
    await _waitUntil(
      tester,
      () =>
          find.byKey(const Key('review-content')).evaluate().isNotEmpty ||
          find
              .byKey(const Key('practice-retry-review'))
              .evaluate()
              .isNotEmpty ||
          find.byKey(const Key('practice-confirm-turn')).evaluate().isNotEmpty,
      const Duration(seconds: 90),
    );
    if (find.byKey(const Key('review-content')).evaluate().isNotEmpty) {
      await _waitUntil(
        tester,
        () =>
            find.byKey(const Key('practice-page')).evaluate().isEmpty &&
            find
                .byKey(const Key('review-content'))
                .hitTestable()
                .evaluate()
                .isNotEmpty,
        const Duration(seconds: 5),
      );
      await tester.pumpAndSettle();
      return;
    }
    if (find.byKey(const Key('practice-confirm-turn')).evaluate().isNotEmpty) {
      _failOnPracticeError(tester, 'confirm turn 3');
      fail('The third confirmation did not advance the Practice Session.');
    }
    if (attempt == 3) {
      break;
    }
    await tester.tap(find.byKey(const Key('practice-retry-review')));
    await tester.pump();
  }
  _failOnPracticeError(tester, 'restore the Review');
  fail('The real service did not return a Review after retrying.');
}

Future<void> _waitForPersistedReview(
  WidgetTester tester, {
  required ReviewHistoryController controller,
  required String reviewId,
  required Duration timeout,
  bool Function()? additionalCondition,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    final loaded = controller.items.any((item) => item.review.id == reviewId);
    if (loaded && (additionalCondition?.call() ?? true)) {
      return;
    }
    final error = controller.errorMessage;
    if (error != null && error.isNotEmpty) {
      fail('Failed to load persisted Review $reviewId: $error');
    }
    await tester.pump(const Duration(milliseconds: 250));
  }
  fail('Timed out waiting for persisted Review $reviewId.');
}

void _failOnPracticeError(WidgetTester tester, String operation) {
  final error = find.byKey(const Key('practice-error-message'));
  if (error.evaluate().isEmpty) {
    return;
  }
  final message = tester.widget<Text>(error).data ?? 'Unknown practice error';
  fail('Failed to $operation: $message');
}

Future<bool> _signedInAccountMatches(
  WidgetTester tester,
  String expectedEmail,
) async {
  await tester.tap(find.byKey(const Key('primary-tab-profile')));
  await _waitUntil(
    tester,
    () => find.byKey(const Key('profile-page')).evaluate().isNotEmpty,
    const Duration(seconds: 5),
  );
  if (find.text(expectedEmail).evaluate().isEmpty) {
    return false;
  }
  await tester.tap(find.byKey(const Key('primary-tab-agent')));
  await _ensureFocusedConversation(tester);
  return true;
}

Future<void> _signOut(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('primary-tab-profile')));
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

Future<void> _ensureFocusedConversation(WidgetTester tester) async {
  final createConversation = find.byKey(
    const Key('no-focused-create-conversation'),
  );
  await _waitUntil(
    tester,
    () =>
        find.byKey(const Key('agent-home-page')).evaluate().isNotEmpty &&
        (_composerIsReady(tester) ||
            createConversation.hitTestable().evaluate().length == 1),
    const Duration(seconds: 20),
  );
  if (_composerIsReady(tester)) {
    return;
  }

  await tester.tap(createConversation);
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

List<int> _decodeVoiceFixture(String encoded) {
  if (encoded.isEmpty) {
    fail(
      'SPEAKUP_E2E_WAV_BASE64 must contain a private spoken-English WAV '
      'fixture supplied at test time.',
    );
  }
  late final List<int> bytes;
  try {
    bytes = base64Decode(encoded);
  } on FormatException {
    fail('SPEAKUP_E2E_WAV_BASE64 is not valid Base64.');
  }
  if (bytes.length <= 44 ||
      ascii.decode(bytes.sublist(0, 4), allowInvalid: true) != 'RIFF' ||
      ascii.decode(bytes.sublist(8, 12), allowInvalid: true) != 'WAVE') {
    fail('SPEAKUP_E2E_WAV_BASE64 must decode to a non-empty WAV file.');
  }
  return bytes;
}

final class _FixturePracticeRecorder implements PracticeRecorder {
  _FixturePracticeRecorder(this._bytes);

  final List<int> _bytes;
  File? _currentFile;
  bool _recording = false;
  int _sequence = 0;

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
    final directory = await getTemporaryDirectory();
    final file = File('${directory.path}/speakup-real-e2e-${++_sequence}.wav');
    await file.writeAsBytes(_bytes, flush: true);
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
