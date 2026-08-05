import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/glass_navigation_bar.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/agent/conversation/conversation.dart';
import 'package:speakup/features/coaching/practice/practice.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/review/review.dart';

void main() {
  testWidgets('starts on the Agent home with four primary navigation entries', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp.preview());

    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
    expect(find.text('你好，今天想练什么？'), findsOneWidget);
    expect(find.text('Hi, 智'), findsNothing);
    expect(find.byKey(const Key('quick-action-create-plan')), findsOneWidget);
    expect(
      find.byKey(const Key('quick-action-continue-practice')),
      findsNothing,
    );
    expect(find.byKey(const Key('quick-action-recent-review')), findsOneWidget);
    expect(find.byKey(const Key('agent-mic-placeholder')), findsOneWidget);
    expect(find.byKey(const Key('agent-show-text-composer')), findsOneWidget);
    expect(find.byKey(const Key('agent-preview-label')), findsOneWidget);

    for (final key in _primaryTabKeys) {
      expect(find.byKey(Key(key)), findsOneWidget);
    }

    final navigation = find.byKey(const Key('primary-navigation'));
    expect(navigation, findsOneWidget);
    expect(
      find.ancestor(of: navigation, matching: find.byType(BackdropFilter)),
      findsNothing,
    );
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    final navigationRect = tester.getRect(navigation);
    expect(navigationRect.height, closeTo(GlassNavigationBar.height, 0.1));
    expect(navigationRect.top - composerRect.bottom, closeTo(10, 1));
    expect(navigationRect.left, closeTo(composerRect.left, 1));
    expect(navigationRect.right, closeTo(composerRect.right, 1));

    final semantics = tester.ensureSemantics();
    expect(
      tester.getSemantics(find.byKey(const Key('primary-tab-agent'))),
      isSemantics(
        label: 'SpeakUp',
        isButton: true,
        hasSelectedState: true,
        isSelected: true,
        hasTapAction: true,
      ),
    );
    semantics.dispose();
  });

  testWidgets('switches between every primary destination', (tester) async {
    await tester.pumpWidget(const SpeakUpApp.preview());
    final semantics = tester.ensureSemantics();

    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-scenes',
      expectedPageKey: 'scenes-page',
    );
    final practiceNavigation = tester.widget<GlassNavigationBar>(
      find.byType(GlassNavigationBar),
    );
    expect(practiceNavigation.solid, isTrue);
    expect(
      find.ancestor(
        of: find.byKey(const Key('primary-navigation')),
        matching: find.byType(BackdropFilter),
      ),
      findsNothing,
    );
    final shellScaffold = tester.widget<Scaffold>(
      find.ancestor(
        of: find.byKey(const Key('primary-navigation')),
        matching: find.byType(Scaffold),
      ),
    );
    expect(shellScaffold.extendBody, isFalse);
    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-review',
      expectedPageKey: 'review-page',
    );
    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-profile',
      expectedPageKey: 'profile-page',
    );
    expect(find.text('管理账号与练习身份。'), findsOneWidget);
    await _tapPrimaryDestination(
      tester,
      key: 'primary-tab-agent',
      expectedPageKey: 'agent-home-page',
    );
    semantics.dispose();
  });

  testWidgets('clears retained text after an external retry is accepted', (
    tester,
  ) async {
    var messages = const <AgentMessage>[];
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            update = setState;
            return ConversationPage(
              messages: messages,
              onSubmitText: (_) async => false,
            );
          },
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'retry this retained text');
    expect(tester.widget<TextField>(composer).controller?.text, isNotEmpty);

    update(() {
      messages = const <AgentMessage>[
        AgentMessage(
          id: 'accepted-user-message',
          role: AgentMessageRole.user,
          text: 'retry this retained text',
        ),
      ];
    });
    await tester.pump();

    expect(tester.widget<TextField>(composer).controller?.text, isEmpty);
  });

  testWidgets('preserves a new draft when an older retry is accepted', (
    tester,
  ) async {
    var messages = const <AgentMessage>[];
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            update = setState;
            return ConversationPage(
              messages: messages,
              onSubmitText: (_) async => false,
            );
          },
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'a newer draft');
    update(() {
      messages = const <AgentMessage>[
        AgentMessage(
          id: 'accepted-older-message',
          role: AgentMessageRole.user,
          text: 'the older failed text',
        ),
      ];
    });
    await tester.pump();

    expect(
      tester.widget<TextField>(composer).controller?.text,
      'a newer draft',
    );
  });

  testWidgets('no focused Thread presents a writable new conversation draft', (
    tester,
  ) async {
    var createCalls = 0;
    var openCalls = 0;
    final submitted = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        home: ConversationPage(
          hasFocusedThread: false,
          onCreateConversation: () => createCalls++,
          onOpenMenu: () => openCalls++,
          onSubmitText: (value) async {
            submitted.add(value);
            return true;
          },
        ),
      ),
    );

    expect(find.byKey(const Key('no-focused-conversation-home')), findsNothing);
    expect(find.text('你好，今天想练什么？'), findsOneWidget);
    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    expect(tester.widget<TextField>(composer).enabled, isTrue);

    await tester.enterText(composer, 'Start immediately');
    await tester.pump();
    final sendButton = find.byKey(const Key('agent-send-button'));
    expect(tester.widget<IconButton>(sendButton).onPressed, isNotNull);
    await tester.tap(sendButton);
    await tester.pump();

    expect(submitted, <String>['Start immediately']);
    expect(tester.widget<TextField>(composer).controller?.text, isEmpty);

    await tester.tap(find.byKey(const Key('conversation-create-button')));
    await tester.tap(find.byKey(const Key('conversation-menu-button')));

    expect(createCalls, 1);
    expect(openCalls, 1);
  });

  testWidgets('home surfaces a definite lazy Thread creation failure', (
    tester,
  ) async {
    final controller = ConversationController(
      client: _DefiniteCreateFailureAgentClient(),
    );
    addTearDown(controller.dispose);
    await controller.initialize();
    await tester.pumpWidget(
      SpeakUpApp.preview(conversationController: controller),
    );
    await tester.pumpAndSettle();

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'Show the failure');
    await tester.pump();
    await tester.tap(find.byKey(const Key('agent-send-button')));
    await tester.pumpAndSettle();

    expect(find.text('暂时无法创建新对话，请稍后再试。'), findsOneWidget);
    expect(
      tester.widget<TextField>(composer).controller?.text,
      'Show the failure',
    );
  });

  testWidgets(
    'materializing a draft Thread preserves text when the first send fails',
    (tester) async {
      String? threadId;
      late StateSetter update;
      await tester.pumpWidget(
        MaterialApp(
          home: StatefulBuilder(
            builder: (context, setState) {
              update = setState;
              return ConversationPage(
                threadId: threadId,
                hasFocusedThread: threadId != null,
                onCreateConversation: () {},
                onSubmitText: (_) async {
                  update(() => threadId = 'created-from-draft');
                  return false;
                },
              );
            },
          ),
        ),
      );

      await _openAgentTextInput(tester);
      final composer = find.byKey(const Key('agent-composer-field'));
      await tester.enterText(composer, 'Keep this draft');
      await tester.pump();
      await tester.tap(find.byKey(const Key('agent-send-button')));
      await tester.pump();

      expect(threadId, 'created-from-draft');
      expect(
        tester.widget<TextField>(composer).controller?.text,
        'Keep this draft',
      );
    },
  );

  testWidgets('inline recovery moves the draft into the recovered Thread', (
    tester,
  ) async {
    String? threadId;
    var recoveryGeneration = 0;
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            update = setState;
            return ConversationPage(
              threadId: threadId,
              draftThreadRecoveryGeneration: recoveryGeneration,
              hasFocusedThread: threadId != null,
              onCreateConversation: () {},
              onSubmitText: (_) async => false,
            );
          },
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'Recover this draft');
    update(() {
      threadId = 'recovered-thread';
      recoveryGeneration++;
    });
    await tester.pump();

    expect(
      tester.widget<TextField>(composer).controller?.text,
      'Recover this draft',
    );
  });

  testWidgets('opening existing history does not inherit an empty-page draft', (
    tester,
  ) async {
    String? threadId;
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            update = setState;
            return ConversationPage(
              threadId: threadId,
              hasFocusedThread: threadId != null,
              onCreateConversation: () {},
              onSubmitText: (_) async => false,
            );
          },
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'Private empty-page draft');
    update(() => threadId = 'existing-history-thread');
    await tester.pump();

    await _openAgentTextInput(tester);
    expect(tester.widget<TextField>(composer).controller?.text, isEmpty);
  });

  testWidgets('switching between existing Threads clears the private draft', (
    tester,
  ) async {
    var threadId = 'thread-a';
    late StateSetter update;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            update = setState;
            return ConversationPage(
              threadId: threadId,
              onSubmitText: (_) async => false,
            );
          },
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'Thread A private draft');
    update(() => threadId = 'thread-b');
    await tester.pump();

    await _openAgentTextInput(tester);
    expect(tester.widget<TextField>(composer).controller?.text, isEmpty);
  });

  testWidgets('repeated keyboard submissions share one in-flight send', (
    tester,
  ) async {
    final sendResult = Completer<bool>();
    var submitCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ConversationPage(
          threadId: 'thread-a',
          onSubmitText: (_) {
            submitCalls++;
            return sendResult.future;
          },
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    await tester.enterText(composer, 'Send once');
    await tester.pump();
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();

    expect(submitCalls, 1);

    sendResult.complete(false);
    await tester.pump();
    expect(tester.widget<TextField>(composer).controller?.text, 'Send once');
  });

  testWidgets('a busy run keeps drafting enabled and submission disabled', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: ConversationPage(
          hasFocusedThread: false,
          isBusy: true,
          onCreateConversation: () {},
          onSubmitText: (_) async => true,
        ),
      ),
    );

    await _openAgentTextInput(tester);
    final composer = find.byKey(const Key('agent-composer-field'));
    expect(tester.widget<TextField>(composer).enabled, isTrue);
    expect(
      tester
          .widget<IconButton>(find.byKey(const Key('agent-send-button')))
          .onPressed,
      isNull,
    );
  });

  testWidgets('older Message pagination is visible and accessible', (
    tester,
  ) async {
    var loadCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: ConversationPage(
          messages: const <AgentMessage>[
            AgentMessage(
              id: 'current-message',
              role: AgentMessageRole.assistant,
              text: 'Current message',
              sequence: 2,
            ),
          ],
          hasEarlierMessages: true,
          onLoadEarlierMessages: () => loadCalls++,
        ),
      ),
    );
    final semantics = tester.ensureSemantics();
    final loadButton = find.byKey(const Key('load-earlier-agent-messages'));

    expect(loadButton, findsOneWidget);
    expect(
      tester.getSemantics(loadButton),
      isSemantics(label: '加载更早消息', isButton: true, hasTapAction: true),
    );
    await tester.tap(loadButton);
    expect(loadCalls, 1);
    semantics.dispose();
  });

  testWidgets(
    'keeps conversation controls fixed and omits the generic thread heading',
    (tester) async {
      tester.view.physicalSize = const Size(390, 700);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final messages = List<AgentMessage>.generate(
        28,
        (index) => AgentMessage(
          id: 'scroll-message-$index',
          role: index.isEven
              ? AgentMessageRole.user
              : AgentMessageRole.assistant,
          text: 'Conversation message $index with enough text to scroll.',
        ),
      );

      await tester.pumpWidget(
        MaterialApp(
          home: ConversationPage(
            messages: messages,
            onOpenMenu: () {},
            onCreateConversation: () {},
            onSubmitText: (_) async => true,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Agent 对话'), findsNothing);
      expect(find.byKey(const Key('agent-thread-title')), findsNothing);
      final menu = find.byKey(const Key('conversation-menu-button'));
      final create = find.byKey(const Key('conversation-create-button'));
      final fixedTitle = find.byKey(const Key('conversation-fixed-title'));
      final menuBefore = tester.getRect(menu);
      final createBefore = tester.getRect(create);
      final titleBefore = tester.getRect(fixedTitle);
      final scroll = find.byType(SingleChildScrollView);
      final scrollController = tester
          .widget<SingleChildScrollView>(scroll)
          .controller!;
      final pixelsBefore = scrollController.position.pixels;
      expect(pixelsBefore, greaterThan(0));

      await tester.drag(scroll, const Offset(0, 360));
      await tester.pumpAndSettle();

      expect(scrollController.position.pixels, lessThan(pixelsBefore));
      expect(tester.getRect(menu), menuBefore);
      expect(tester.getRect(create), createBefore);
      expect(tester.getRect(fixedTitle), titleBefore);
    },
  );

  testWidgets('keeps available Agent actions above the composer on iPhone', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(402, 874);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(const SpeakUpApp.preview());
    await tester.pumpAndSettle();

    const actionKeys = [
      'quick-action-create-plan',
      'quick-action-browse-scenes',
      'quick-action-recent-review',
    ];
    for (final key in actionKeys) {
      final action = find.byKey(Key(key));
      expect(action, findsOneWidget);
      expect(tester.getRect(action).bottom, lessThan(874));
    }

    final lastActionRect = tester.getRect(
      find.byKey(const Key('quick-action-recent-review')),
    );
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(composerRect.top - lastActionRect.bottom, greaterThanOrEqualTo(16));
  });

  testWidgets('conversation drawer exposes bounded Thread actions only', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp.preview());
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('conversation-menu-button')));
    await tester.pumpAndSettle();

    final drawer = find.byType(Drawer);
    expect(drawer, findsOneWidget);
    expect(
      find.descendant(
        of: drawer,
        matching: find.byKey(const Key('new-conversation-button')),
      ),
      findsOneWidget,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('新对话')),
      findsNWidgets(2),
    );
    expect(
      find.descendant(of: drawer, matching: find.text('当前对话')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('本地 Fake 预览，未连接正式账号')),
      findsOneWidget,
    );
    expect(
      find.descendant(
        of: drawer,
        matching: find.byKey(const Key('focused-conversation-indicator')),
      ),
      findsOneWidget,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('近期对话')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('场景')),
      findsNothing,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('复盘')),
      findsNothing,
    );
    expect(
      find.descendant(of: drawer, matching: find.text('我的')),
      findsNothing,
    );
  });

  testWidgets(
    'new conversation becomes selected and the old one stays recent',
    (tester) async {
      final controller = ConversationController(client: FakeAgentClient());
      addTearDown(controller.dispose);
      await tester.pumpWidget(
        SpeakUpApp.preview(conversationController: controller),
      );
      await tester.pumpAndSettle();
      final originalThreadId = controller.threadId;

      await tester.tap(find.byKey(const Key('conversation-menu-button')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('new-conversation-button')));
      await tester.pumpAndSettle();

      expect(controller.threadId, isNot(originalThreadId));
      expect(find.byType(Drawer), findsNothing);

      await tester.tap(find.byKey(const Key('conversation-menu-button')));
      await tester.pumpAndSettle();
      expect(
        find.byKey(Key('conversation-thread-${controller.threadId}')),
        findsOneWidget,
      );
      expect(
        find.byKey(Key('conversation-thread-$originalThreadId')),
        findsOneWidget,
      );

      await tester.tap(
        find.byKey(Key('conversation-thread-$originalThreadId')),
      );
      await tester.pumpAndSettle();
      expect(controller.threadId, originalThreadId);
    },
  );

  testWidgets('conversation drawer confirms and deletes each Thread', (
    tester,
  ) async {
    final controller = ConversationController(client: FakeAgentClient());
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      SpeakUpApp.preview(conversationController: controller),
    );
    await tester.pumpAndSettle();
    final originalThreadId = controller.threadId!;

    await tester.tap(find.byKey(const Key('conversation-menu-button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('new-conversation-button')));
    await tester.pumpAndSettle();
    final currentThreadId = controller.threadId!;

    await tester.tap(find.byKey(const Key('conversation-menu-button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(Key('delete-conversation-$originalThreadId')));
    await tester.pumpAndSettle();
    expect(find.text('删除对话？'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm-delete-conversation')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(Key('conversation-thread-$originalThreadId')),
      findsNothing,
    );

    await tester.tap(find.byKey(Key('delete-conversation-$currentThreadId')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('confirm-delete-conversation')));
    await tester.pumpAndSettle();

    expect(controller.threadId, isNull);
    expect(
      find.byKey(Key('conversation-thread-$currentThreadId')),
      findsNothing,
    );
    expect(find.byKey(const Key('no-focused-conversation')), findsOneWidget);
  });

  testWidgets('Agent actions start planning or open the matching destination', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp.preview());

    await _tapVisible(tester, 'quick-action-create-plan');
    expect(find.text('我想创建一场模拟面试，请先帮我梳理面试信息。'), findsOneWidget);
    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);

    await tester.tap(find.byKey(const Key('conversation-create-button')));
    await tester.pumpAndSettle();
    await _tapVisible(tester, 'quick-action-browse-scenes');
    expect(find.byKey(const Key('scenes-page')), findsOneWidget);
    expect(
      find.byKey(const Key('practice-availability-message')),
      findsOneWidget,
    );
    expect(find.text('今天想练什么？'), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-agent')));
    await tester.pumpAndSettle();
    await _tapVisible(tester, 'quick-action-recent-review');
    expect(find.byKey(const Key('review-page')), findsOneWidget);
    expect(find.text('本地预览；结果不会写入正式服务。'), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-agent')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('quick-action-continue-practice')),
      findsNothing,
    );
  });

  testWidgets('keeps every formal feature route reachable', (tester) async {
    await tester.pumpWidget(const SpeakUpApp.preview());

    await _expectNamedRoute<PreparationPage>(
      tester,
      AppRoutes.preparation,
      backButton: find.byKey(const Key('preparation-route-back-button')),
    );
    await _expectNamedRoute<PracticePage>(
      tester,
      AppRoutes.practice,
      backButton: find.byType(BackButton),
    );
    await _expectNamedRoute<ConversationPage>(
      tester,
      AppRoutes.conversation,
      backButton: find.byKey(const Key('conversation-route-back-button')),
    );
    await _expectNamedRoute<ReviewPage>(
      tester,
      AppRoutes.review,
      backButton: find.byKey(const Key('review-route-back-button')),
    );
  });

  testWidgets('keeps the named conversation route escapable on every tab', (
    tester,
  ) async {
    await tester.pumpWidget(const SpeakUpApp.preview());

    final shellContext = tester.element(find.byType(SpeakUpShell));
    Navigator.of(shellContext).pushNamed(AppRoutes.conversation);
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('conversation-route-back-button')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('preparation-route-back-button')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('primary-tab-review')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-route-back-button')), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();
    final profileBackButton = find.byKey(
      const Key('profile-route-back-button'),
    );
    expect(profileBackButton, findsOneWidget);

    await tester.tap(profileBackButton);
    await tester.pumpAndSettle();

    expect(profileBackButton, findsNothing);
    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
    expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
    final rootShellContext = tester.element(find.byType(SpeakUpShell));
    expect(Navigator.of(rootShellContext).canPop(), isFalse);
  });

  testWidgets('stays usable on a narrow screen and with the keyboard open', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);

    await tester.pumpWidget(const SpeakUpApp.preview());
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    final lastAction = find.byKey(const Key('quick-action-recent-review'));
    await tester.ensureVisible(lastAction);
    await tester.pumpAndSettle();
    final lastActionRect = tester.getRect(lastAction);
    final restingComposerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(
      restingComposerRect.top - lastActionRect.bottom,
      greaterThanOrEqualTo(16),
    );
    expect(lastAction.hitTestable(), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-scenes')));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    await tester.tap(find.byKey(const Key('primary-tab-agent')));
    await tester.pumpAndSettle();
    await _openAgentTextInput(tester);
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    await tester.showKeyboard(find.byKey(const Key('agent-composer-field')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('agent-composer-field')), findsOneWidget);
    expect(find.byKey(const Key('primary-navigation')), findsNothing);
    final keyboardTop =
        tester.view.physicalSize.height / tester.view.devicePixelRatio - 240;
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(keyboardTop - composerRect.bottom, closeTo(10, 1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('supports larger system text without covering Agent actions', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(402, 874);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 1.5;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

    await tester.pumpWidget(const SpeakUpApp.preview());
    await tester.pumpAndSettle();

    final lastAction = find.byKey(const Key('quick-action-recent-review'));
    await tester.ensureVisible(lastAction);
    await tester.pumpAndSettle();

    final lastActionRect = tester.getRect(lastAction);
    final composerRect = tester.getRect(
      find.byKey(const Key('agent-composer-surface')),
    );
    expect(composerRect.top - lastActionRect.bottom, greaterThanOrEqualTo(16));
    expect(lastAction.hitTestable(), findsOneWidget);
    expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'keeps navigation and drawer usable at accessibility text sizes',
    (tester) async {
      tester.view.physicalSize = const Size(320, 568);
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 3;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

      await tester.pumpWidget(const SpeakUpApp.preview());
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
      expect(tester.takeException(), isNull);
      final selectedLabel = tester.renderObject<RenderParagraph>(
        find.descendant(
          of: find.byKey(const Key('primary-navigation')),
          matching: find.text('SpeakUp'),
        ),
      );
      expect(selectedLabel.didExceedMaxLines, isFalse);

      final lastAction = find.byKey(const Key('quick-action-recent-review'));
      await tester.ensureVisible(lastAction);
      await tester.pumpAndSettle();
      final lastActionRect = tester.getRect(lastAction);
      final composerRect = tester.getRect(
        find.byKey(const Key('agent-composer-surface')),
      );
      expect(
        composerRect.top - lastActionRect.bottom,
        greaterThanOrEqualTo(16),
      );
      expect(lastAction.hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SpeakUpApp.preview());
      await tester.pumpAndSettle();

      final menuButton = find.byKey(const Key('conversation-menu-button'));
      await tester.tap(menuButton);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.byType(Drawer), findsOneWidget);

      await tester.drag(find.byType(ListView), const Offset(0, -1000));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('no-recent-conversations')).hitTestable(),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);

      await tester.drag(find.byType(ListView), const Offset(0, 1000));
      await tester.pumpAndSettle();
      final accountLabel = find.text('本地 Fake 预览，未连接正式账号');
      expect(accountLabel.hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}

const _primaryTabKeys = [
  'primary-tab-agent',
  'primary-tab-scenes',
  'primary-tab-review',
  'primary-tab-profile',
];

Future<void> _openAgentTextInput(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('agent-show-text-composer')));
  await tester.pump();
}

Future<void> _tapPrimaryDestination(
  WidgetTester tester, {
  required String key,
  required String expectedPageKey,
}) async {
  await tester.tap(find.byKey(Key(key)));
  await tester.pumpAndSettle();

  expect(find.byKey(Key(expectedPageKey)), findsOneWidget);
  expect(
    tester.getSemantics(find.byKey(Key(key))),
    isSemantics(hasSelectedState: true, isSelected: true),
  );
  expect(find.byKey(const Key('preparation-route-back-button')), findsNothing);
  expect(find.byKey(const Key('review-route-back-button')), findsNothing);
  expect(find.byKey(const Key('conversation-route-back-button')), findsNothing);
  expect(find.byKey(const Key('profile-route-back-button')), findsNothing);
}

Future<void> _expectNamedRoute<T extends Widget>(
  WidgetTester tester,
  String route, {
  required Finder backButton,
}) async {
  final shellContext = tester.element(find.byType(SpeakUpShell));
  Navigator.of(shellContext).pushNamed(route);
  await tester.pumpAndSettle();

  expect(find.byType(T), findsOneWidget);
  expect(backButton, findsOneWidget);

  await tester.tap(backButton);
  await tester.pumpAndSettle();

  expect(backButton, findsNothing);
  expect(find.byKey(const Key('agent-home-page')), findsOneWidget);
  expect(find.byKey(const Key('primary-navigation')), findsOneWidget);
  final rootShellContext = tester.element(find.byType(SpeakUpShell));
  expect(Navigator.of(rootShellContext).canPop(), isFalse);
}

Future<void> _tapVisible(WidgetTester tester, String key) async {
  final finder = find.byKey(Key(key));
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

final class _DefiniteCreateFailureAgentClient implements AgentClient {
  final FakeAgentClient _delegate = FakeAgentClient();

  @override
  Future<void> clearAccountState() => _delegate.clearAccountState();

  @override
  Future<AgentThreadPage> listThreads({
    int pageSize = 20,
    String? cursor,
  }) async {
    final page = await _delegate.listThreads(
      pageSize: pageSize,
      cursor: cursor,
    );
    return AgentThreadPage(threads: page.threads, nextCursor: page.nextCursor);
  }

  @override
  Future<AgentThreadSnapshot?> getFocusedThread() async => null;

  @override
  Future<AgentThreadSummary> createThread() {
    throw StateError('Definite Thread creation failure.');
  }

  @override
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) =>
      _delegate.setFocusedThread(threadId: threadId);

  @override
  Future<void> clearFocusedThread() => _delegate.clearFocusedThread();

  @override
  Future<void> deleteThread({required String threadId}) =>
      _delegate.deleteThread(threadId: threadId);

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) => _delegate.listMessages(
    threadId: threadId,
    pageSize: pageSize,
    cursor: cursor,
  );

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) => _delegate.sendText(
    threadId: threadId,
    text: text,
    clientMessageId: clientMessageId,
    imageAssetIds: imageAssetIds,
  );
}
