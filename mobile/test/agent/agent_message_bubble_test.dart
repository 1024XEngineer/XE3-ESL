import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/agent_message_bubble.dart';
import 'package:speakup/features/agent/client_action/practice_plan_client_action_card.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';

void main() {
  testWidgets('renders assistant emphasis instead of Markdown markers', (
    tester,
  ) async {
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'assistant-markdown',
        role: AgentMessageRole.assistant,
        text: 'You are **Xiaohua**.',
      ),
    );

    final selectableText = _messageSelectableText(tester, 'assistant-markdown');
    expect(
      selectableText.map(_selectablePlainText).join(),
      contains('You are Xiaohua.'),
    );
    expect(
      selectableText
          .expand(
            (widget) => widget.textSpan == null
                ? const <TextSpan>[]
                : _flatten(widget.textSpan!),
          )
          .any(
            (span) =>
                span.text == 'Xiaohua' &&
                span.style?.fontWeight == FontWeight.w700,
          ),
      isTrue,
    );
    expect(
      selectableText.any(
        (widget) => _selectablePlainText(widget).contains('**'),
      ),
      isFalse,
    );
  });

  testWidgets('renders assistant block Markdown inside the message bubble', (
    tester,
  ) async {
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'assistant-list',
        role: AgentMessageRole.assistant,
        text: '## Practice\n\n- First answer\n- Add evidence',
      ),
    );

    final text = _messageSelectableText(
      tester,
      'assistant-list',
    ).map(_selectablePlainText).join(' ');
    expect(text, contains('Practice'));
    expect(text, contains('First answer'));
    expect(text, contains('Add evidence'));
  });

  testWidgets('keeps user Markdown input as literal plain text', (
    tester,
  ) async {
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'user-markdown',
        role: AgentMessageRole.user,
        text: 'I typed **literal markers**.',
      ),
    );

    expect(find.text('I typed **literal markers**.'), findsOneWidget);
    expect(
      find.descendant(
        of: find.byKey(const Key('agent-message-user-markdown')),
        matching: find.byType(SelectableText),
      ),
      findsNothing,
    );
  });

  testWidgets('does not load remote images from assistant Markdown', (
    tester,
  ) async {
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'assistant-image',
        role: AgentMessageRole.assistant,
        text: '![private diagram](https://example.com/private.png)',
      ),
    );

    expect(find.byType(Image), findsNothing);
    expect(find.text('[图片：private diagram]'), findsOneWidget);
  });

  testWidgets('renders multimodal image metadata with a safe reload action', (
    tester,
  ) async {
    var refreshCalls = 0;
    final message = AgentMessage(
      id: 'user-image',
      role: AgentMessageRole.user,
      text: 'Please explain this.',
      modality: AgentMessageModality.multimodal,
      images: <AgentImageAsset>[
        AgentImageAsset(
          id: 'image-1',
          contentType: 'image/png',
          sizeBytes: 128,
          width: 32,
          height: 24,
          status: AgentImageAssetStatus.ready,
          createdAt: DateTime.utc(2026, 7, 30),
        ),
      ],
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: message,
            onRefreshImage: (_, _) async => refreshCalls++,
          ),
        ),
      ),
    );

    expect(find.text('Please explain this.'), findsOneWidget);
    expect(find.byIcon(Icons.broken_image_outlined), findsOneWidget);
    await tester.tap(find.byIcon(Icons.broken_image_outlined));
    await tester.pump();
    expect(refreshCalls, 1);
  });

  testWidgets('expands correction and polish inside a voice bubble', (
    tester,
  ) async {
    const message = AgentMessage(
      id: 'user-voice-polish',
      role: AgentMessageRole.user,
      text: '我觉得 this plan is good.',
      modality: AgentMessageModality.voice,
      audio: AgentMessageAudio(
        id: 'audio-polish',
        status: AgentMessageAudioStatus.readable,
        contentType: 'audio/mp4',
        sizeBytes: 128,
        duration: Duration(seconds: 3),
        playbackPath: '/v1/agent-audio/audio-polish/playback',
      ),
    );
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: message,
            correction: InlineLanguageSuggestion(
              originalText: 'I was planning this project.',
              text: 'I am planning this project.',
              explanation: 'Use the present tense for a current action.',
            ),
            polish: InlineLanguageSuggestion(
              text: 'This plan sounds good.',
              explanation: 'Sounds more natural in conversation.',
            ),
          ),
        ),
      ),
    );

    expect(find.text('优化'), findsNothing);
    expect(find.byKey(const Key('inline-language-optimize')), findsOneWidget);
    expect(find.text('纠错'), findsNothing);
    expect(find.text('This plan sounds good.'), findsNothing);

    await tester.tap(find.byKey(const Key('inline-language-optimize')));
    await tester.pump();

    final expandedBubble = tester.widget<Container>(
      find.byKey(const Key('agent-message-user-voice-polish')),
    );
    expect(expandedBubble.padding, const EdgeInsets.fromLTRB(14, 11, 12, 11));
    expect(find.text('纠错'), findsOneWidget);
    expect(find.text('更自然的表达'), findsOneWidget);
    expect(find.text('This plan sounds good.'), findsOneWidget);
    expect(
      find.text('Use the present tense for a current action.'),
      findsOneWidget,
    );
    final correctionText = tester.widget<Text>(
      find.byKey(const Key('inline-language-correction-diff')),
    );
    final spans = (correctionText.textSpan as TextSpan).children!
        .cast<TextSpan>();
    expect(
      spans.any(
        (span) =>
            span.text!.contains('was') &&
            span.style?.decoration == TextDecoration.lineThrough &&
            span.style?.color == SpeakUpDesign.error,
      ),
      isTrue,
    );
    expect(
      spans.any(
        (span) =>
            span.text!.contains('am') &&
            span.style?.color == SpeakUpDesign.success,
      ),
      isTrue,
    );
  });

  testWidgets(
    'keeps deleted voice transcript with an explicit privacy notice',
    (tester) async {
      await _pumpMessage(
        tester,
        const AgentMessage(
          id: 'user-voice-deleted',
          role: AgentMessageRole.user,
          text: 'Keep this corrected transcript.',
          modality: AgentMessageModality.voice,
        ),
      );

      expect(find.text('Keep this corrected transcript.'), findsOneWidget);
      expect(find.text('录音已删除，文字已保留'), findsOneWidget);
      expect(
        find.byKey(const Key('agent-user-voice-play-user-voice-deleted')),
        findsNothing,
      );
    },
  );

  testWidgets('keeps voice actions compact without shrinking tap targets', (
    tester,
  ) async {
    const message = AgentMessage(
      id: 'user-voice-spacing',
      role: AgentMessageRole.user,
      text: '嗯。',
      modality: AgentMessageModality.voice,
      audio: AgentMessageAudio(
        id: 'audio-spacing',
        status: AgentMessageAudioStatus.readable,
        contentType: 'audio/mp4',
        sizeBytes: 128,
        duration: Duration(seconds: 3),
        playbackPath: '/v1/agent-audio/audio-spacing/playback',
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ListView(
            children: const [
              AgentMessageBubble(
                message: message,
                correction: InlineLanguageSuggestion(
                  originalText: 'study knowledge about AI',
                  text: 'learn about AI',
                ),
              ),
            ],
          ),
        ),
      ),
    );

    final bubble = find.byKey(const Key('agent-message-user-voice-spacing'));
    final transcript = find.byKey(
      const Key('agent-user-voice-transcript-user-voice-spacing'),
    );
    final playback = find.byKey(
      const Key('agent-user-voice-play-user-voice-spacing'),
    );
    final optimize = find.byKey(const Key('inline-language-optimize'));
    final transcriptRect = tester.getRect(transcript);
    final playbackRect = tester.getRect(playback);

    expect(playbackRect.top, transcriptRect.bottom);
    expect(
      tester.widget<Container>(bubble).padding,
      const EdgeInsets.fromLTRB(14, 11, 12, 0),
    );
    expect(tester.getSize(playback), const Size.square(44));
    expect(tester.getSize(optimize), const Size.square(40));
    expect(tester.getSize(bubble).width, lessThan(340));
  });

  testWidgets('renders and dispatches a practice plan client action', (
    tester,
  ) async {
    const action = ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000001',
      planVersion: 2,
      sceneId: 'scn_interview_project_deep_dive',
      sceneName: '项目经历深挖',
      userRole: '候选人',
      practiceGoal: 'Java Interview Practice',
      practiceExperience: 'INTERVIEW',
      sceneCategory: 'INTERVIEW_PROFESSIONAL',
      practiceMode: 'FULL_SIMULATION',
      aiRoles: <String>['面试官'],
      practiceScope: '围绕项目难点完成三轮追问',
      suggestedDuration: Duration(minutes: 12),
      minEffectiveTurns: 3,
      maxEffectiveTurns: 5,
      confirmationPrompt: '请确认是否按此方案开始练习。',
    );
    ConfirmPracticePlanClientAction? selected;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: AgentMessage(
              id: 'assistant-interview',
              role: AgentMessageRole.assistant,
              text: '面试场景已创建。',
              clientActions: <AgentClientAction>[
                encodeConfirmPracticePlanClientAction(action),
              ],
            ),
            clientActionBuilder: _practiceActionBuilder(
              (value) => selected = value,
            ),
          ),
        ),
      ),
    );

    expect(find.text('项目经历深挖'), findsOneWidget);
    expect(find.text('Java Interview Practice'), findsOneWidget);
    expect(find.text('已为你准备好'), findsOneWidget);
    expect(find.text('你：候选人 · AI：面试官'), findsOneWidget);
    expect(find.text('约 12 分钟'), findsOneWidget);
    expect(find.text('围绕项目难点完成三轮追问'), findsOneWidget);
    expect(find.bySemanticsLabel('面试官场景图'), findsOneWidget);
    expect(find.text('开始练习'), findsOneWidget);
    expect(find.text('确认并开始练习'), findsNothing);
    expect(find.text('场景：项目经历深挖'), findsNothing);
    expect(find.text('角色：面试官、候选人'), findsNothing);
    expect(find.text('范围：围绕项目难点完成三轮追问'), findsNothing);
    expect(find.textContaining('3–5 轮'), findsNothing);
    expect(find.text('请确认是否按此方案开始练习。'), findsNothing);
    await tester.tap(
      find.byKey(
        const Key(
          'confirm-practice-plan-'
          '10000000-0000-4000-8000-000000000001-2',
        ),
      ),
    );
    expect(selected?.practicePlanId, action.practicePlanId);
    expect(selected?.planVersion, action.planVersion);
  });

  testWidgets('renders the interview self-introduction recording sample', (
    tester,
  ) async {
    const action = ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000006',
      planVersion: 1,
      sceneId: 'scn_interview_self_introduction',
      sceneName: '英文自我介绍',
      userRole: '候选人',
      practiceGoal: '说清背景、优势和岗位匹配，并自然回应一到两个追问。',
      practiceExperience: 'INTERVIEW',
      sceneCategory: 'INTERVIEW_RECRUITER',
      practiceMode: 'FOCUS',
      aiRoles: <String>['招聘方'],
      practiceScope: '重点练习',
      suggestedDuration: Duration(minutes: 8),
      minEffectiveTurns: 3,
      maxEffectiveTurns: 8,
      confirmationPrompt: '确认后将创建练习会话；确认前不会开始练习。',
    );
    await _pumpMessage(
      tester,
      AgentMessage(
        id: 'assistant-interview-self-introduction',
        role: AgentMessageRole.assistant,
        text: '我们从自我介绍开始。',
        clientActions: <AgentClientAction>[
          encodeConfirmPracticePlanClientAction(action),
        ],
      ),
    );

    expect(find.text('英文自我介绍'), findsOneWidget);
    expect(find.text('你：候选人 · AI：招聘方'), findsOneWidget);
    expect(find.text('约 8 分钟'), findsOneWidget);
    expect(find.text('重点练习'), findsOneWidget);
    expect(find.bySemanticsLabel('面试官场景图'), findsOneWidget);
    expect(find.text('开始练习'), findsOneWidget);
  });

  testWidgets('renders an unscored IELTS warm-up without a client action', (
    tester,
  ) async {
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'assistant-ielts-warm-up',
        role: AgentMessageRole.assistant,
        text: '可以。最近有没有谁让你印象挺深？用一两句英语说说。',
      ),
    );

    final markdown = _messageSelectableText(
      tester,
      'assistant-ielts-warm-up',
    ).map(_selectablePlainText).join(' ');
    expect(markdown, contains('最近有没有谁让你印象挺深？'));
    expect(markdown, contains('用一两句英语说说。'));
    expect(markdown, isNot(contains('练前跟练')));
    expect(markdown, isNot(contains('Warm-up')));
    expect(markdown, isNot(contains('不计分')));
    expect(markdown, isNot(contains('卡住')));
    expect(markdown, isNot(contains('提示')));
    expect(markdown, isNot(contains('直接开始')));
    expect(find.text('确认并开始练习'), findsNothing);
    expect(find.text('范围：Part 1'), findsNothing);
  });

  testWidgets('renders and dispatches the later Part 1 confirmation action', (
    tester,
  ) async {
    const action = ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000003',
      planVersion: 1,
      sceneId: 'ielts-speaking',
      sceneName: 'IELTS 口语',
      userRole: '考生',
      practiceGoal: '按所选 IELTS 口语模式完成真实节奏的连续表达。',
      practiceExperience: 'IELTS_SPEAKING',
      sceneCategory: 'IELTS_SPEAKING',
      practiceMode: 'PART_1',
      aiRoles: <String>['IELTS 口语考官'],
      practiceScope: 'Part 1',
      suggestedDuration: Duration(minutes: 5),
      minEffectiveTurns: 3,
      maxEffectiveTurns: 3,
      confirmationPrompt: '确认后进入正式练习。',
    );
    ConfirmPracticePlanClientAction? selected;
    await _pumpMessage(
      tester,
      AgentMessage(
        id: 'assistant-ielts-part-1-confirmation',
        role: AgentMessageRole.assistant,
        text: '听到了，你是在确认我能不能听见。',
        clientActions: <AgentClientAction>[
          encodeConfirmPracticePlanClientAction(action),
        ],
      ),
      onClientAction: (value) => selected = value,
    );

    final markdown = _messageSelectableText(
      tester,
      'assistant-ielts-part-1-confirmation',
    ).map(_selectablePlainText).join(' ');
    expect(markdown, contains('听到了，你是在确认我能不能听见。'));
    expect(markdown, isNot(contains('准备好')));
    expect(markdown, isNot(contains('开始')));
    expect(markdown, isNot(contains('卡片')));
    expect(markdown, isNot(contains('Part 1')));
    expect(markdown, isNot(contains('练前跟练')));
    expect(find.text('IELTS Speaking · Part 1'), findsOneWidget);
    expect(find.text(action.practiceGoal), findsOneWidget);
    expect(find.text('你：考生 · AI：IELTS 口语考官'), findsOneWidget);
    expect(find.text('约 5 分钟'), findsOneWidget);
    expect(find.text('3 个问题'), findsOneWidget);
    expect(find.bySemanticsLabel('IELTS 考官头像'), findsOneWidget);
    final card = find.byKey(
      const Key(
        'agent-client-action-practice-plan-'
        '10000000-0000-4000-8000-000000000003-1',
      ),
    );
    expect(
      tester.getCenter(card).dx,
      tester.view.physicalSize.width / tester.view.devicePixelRatio / 2,
    );
    expect(find.text('场景：IELTS 口语'), findsNothing);
    expect(find.text('角色：IELTS 口语考官'), findsNothing);
    expect(find.text('范围：Part 1'), findsNothing);
    expect(find.textContaining('3–3 轮'), findsNothing);
    expect(find.text(action.confirmationPrompt), findsNothing);
    expect(find.text('开始练习'), findsOneWidget);

    await tester.tap(
      find.byKey(
        const Key(
          'confirm-practice-plan-'
          '10000000-0000-4000-8000-000000000003-1',
        ),
      ),
    );
    expect(selected?.practicePlanId, action.practicePlanId);
    expect(selected?.planVersion, action.planVersion);
  });

  testWidgets('keeps a full mock action free of warm-up and question text', (
    tester,
  ) async {
    const action = ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000004',
      planVersion: 1,
      sceneId: 'ielts-speaking',
      sceneName: 'IELTS 口语',
      userRole: '考生',
      practiceGoal: '按所选 IELTS 口语模式完成真实节奏的连续表达。',
      practiceExperience: 'IELTS_SPEAKING',
      sceneCategory: 'IELTS_SPEAKING',
      practiceMode: 'FULL_MOCK',
      aiRoles: <String>['IELTS 口语考官'],
      practiceScope: '完整模考',
      suggestedDuration: Duration(minutes: 14),
      minEffectiveTurns: 14,
      maxEffectiveTurns: 14,
      confirmationPrompt: '题目将在正式开始后展示。',
    );
    await _pumpMessage(
      tester,
      AgentMessage(
        id: 'assistant-ielts-full-mock',
        role: AgentMessageRole.assistant,
        text: '好。',
        clientActions: <AgentClientAction>[
          encodeConfirmPracticePlanClientAction(action),
        ],
      ),
    );

    final markdown = _messageSelectableText(
      tester,
      'assistant-ielts-full-mock',
    ).map(_selectablePlainText).join(' ');
    expect(markdown, '好。');
    expect(find.text('IELTS Speaking · 完整模考'), findsOneWidget);
    expect(find.text('约 14 分钟'), findsOneWidget);
    expect(find.text('14 个问题'), findsOneWidget);
    final card = find.byKey(
      const Key(
        'agent-client-action-practice-plan-'
        '10000000-0000-4000-8000-000000000004-1',
      ),
    );
    final entry = tester.widget<InkWell>(
      find.byKey(
        const Key(
          'confirm-practice-plan-'
          '10000000-0000-4000-8000-000000000004-1',
        ),
      ),
    );
    expect(entry.onTap, isNull);
    expect(
      find.descendant(
        of: card,
        matching: find.byIcon(Icons.chevron_right_rounded),
      ),
      findsNothing,
    );
    for (final hidden in const <String>[
      '练前跟练',
      '不计分',
      'Describe a person',
      'What I enjoy most is',
    ]) {
      expect(markdown, isNot(contains(hidden)));
      expect(
        find.descendant(of: card, matching: find.textContaining(hidden)),
        findsNothing,
      );
    }
  });

  testWidgets('does not show a round count for an open-ended action', (
    tester,
  ) async {
    const action = ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000002',
      planVersion: 1,
      sceneId: 'scn_daily_rental_viewing',
      sceneName: '看房与租赁咨询',
      userRole: '租客',
      practiceGoal: '了解房屋条件、租金费用、租期和入住要求',
      practiceExperience: 'LIFE_AND_TRAVEL',
      sceneCategory: 'LIFE_DAILY',
      practiceMode: 'FULL_SIMULATION',
      aiRoles: <String>['房产中介'],
      practiceScope: '开放对话',
      suggestedDuration: Duration(minutes: 10),
      minEffectiveTurns: 1,
      maxEffectiveTurns: 0,
      confirmationPrompt: '请确认是否开始练习。',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: AgentMessage(
              id: 'assistant-open-practice',
              role: AgentMessageRole.assistant,
              text: '旅行场景已创建。',
              clientActions: <AgentClientAction>[
                encodeConfirmPracticePlanClientAction(action),
              ],
            ),
            clientActionBuilder: _practiceActionBuilder(null),
          ),
        ),
      ),
    );

    expect(find.text('约 10 分钟'), findsOneWidget);
    expect(find.text('看房与租赁咨询'), findsOneWidget);
    expect(find.bySemanticsLabel('生活场景图'), findsOneWidget);
    final image = tester.widget<Image>(find.byType(Image));
    expect(
      (image.image as AssetImage).assetName,
      'assets/images/scenes/daily-rental-viewing.jpg',
    );
    expect(find.textContaining('轮'), findsNothing);
  });

  testWidgets(
    'unknown and malformed actions do not hide the Message or add card spacing',
    (tester) async {
      const baseMessage = AgentMessage(
        id: 'assistant-forward-compatible',
        role: AgentMessageRole.assistant,
        text: 'The answer remains visible.',
      );
      await _pumpMessage(tester, baseMessage);
      final baseSize = tester.getSize(
        find.byKey(const Key('agent-message-assistant-forward-compatible')),
      );

      await _pumpMessage(
        tester,
        const AgentMessage(
          id: 'assistant-forward-compatible',
          role: AgentMessageRole.assistant,
          text: 'The answer remains visible.',
          clientActions: <AgentClientAction>[
            AgentClientAction(
              type: 'future.client.action.v1',
              payload: <String, Object?>{'future': true},
            ),
            AgentClientAction(
              type: practicePlanConfirmClientActionType,
              payload: <String, Object?>{'practice_plan_id': 'malformed'},
            ),
          ],
        ),
      );

      expect(find.text('The answer remains visible.'), findsOneWidget);
      expect(
        find.byKey(const Key('agent-message-assistant-forward-compatible')),
        findsOneWidget,
      );
      expect(
        tester.getSize(
          find.byKey(const Key('agent-message-assistant-forward-compatible')),
        ),
        baseSize,
      );
      expect(find.byType(PracticePlanClientActionCard), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('renders a supported action beside an unknown future action', (
    tester,
  ) async {
    const action = ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000007',
      planVersion: 1,
      sceneId: 'scn_daily_restaurant_ordering',
      sceneName: '餐厅点餐',
      userRole: '顾客',
      aiRoles: <String>['服务员'],
      practiceGoal: '完成点餐并确认忌口信息',
      practiceExperience: 'LIFE_AND_TRAVEL',
      sceneCategory: 'LIFE_DAILY',
      practiceMode: 'FULL_SIMULATION',
      practiceScope: '开放对话',
      suggestedDuration: Duration(minutes: 8),
      minEffectiveTurns: 1,
      maxEffectiveTurns: 0,
      confirmationPrompt: '确认后开始练习。',
    );
    await _pumpMessage(
      tester,
      AgentMessage(
        id: 'assistant-known-and-unknown',
        role: AgentMessageRole.assistant,
        text: '餐厅场景已创建。',
        clientActions: <AgentClientAction>[
          const AgentClientAction(
            type: 'future.client.action.v1',
            payload: <String, Object?>{'future': true},
          ),
          encodeConfirmPracticePlanClientAction(action),
        ],
      ),
    );

    expect(find.text('餐厅场景已创建。'), findsOneWidget);
    expect(find.byType(PracticePlanClientActionCard), findsOneWidget);
    expect(find.text('餐厅点餐'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  for (final width in <double>[320, 390]) {
    for (final textScale in <double>[2, 3]) {
      testWidgets('keeps a long custom action usable at ${width.toInt()}px '
          'and ${textScale.toInt()}x text', (tester) async {
        tester.view.physicalSize = Size(width, 800);
        tester.view.devicePixelRatio = 1;
        tester.platformDispatcher.textScaleFactorTestValue = textScale;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
        final semantics = tester.ensureSemantics();
        var confirmed = false;
        final action = ConfirmPracticePlanClientAction(
          label: '确认并开始练习',
          practicePlanId: '10000000-0000-4000-8000-000000000008',
          planVersion: 1,
          sceneId: 'custom_scene_long',
          sceneName: List<String>.filled(100, '场景').join(),
          userRole: List<String>.filled(100, '用户').join(),
          aiRoles: List<String>.generate(
            8,
            (index) => 'ROLE$index${List<String>.filled(190, 'X').join()}',
          ),
          practiceGoal: List<String>.filled(500, 'G').join(),
          practiceExperience: 'WORKPLACE',
          sceneCategory: 'WORKPLACE_GENERAL',
          practiceMode: 'FULL_SIMULATION',
          practiceScope: '开放对话',
          suggestedDuration: const Duration(minutes: 8),
          minEffectiveTurns: 1,
          maxEffectiveTurns: 0,
          confirmationPrompt: '确认后开始练习。',
        );

        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: SingleChildScrollView(
                child: AgentMessageBubble(
                  message: AgentMessage(
                    id: 'assistant-long-custom-${width.toInt()}',
                    role: AgentMessageRole.assistant,
                    text: '定制场景已创建。',
                    clientActions: <AgentClientAction>[
                      encodeConfirmPracticePlanClientAction(action),
                    ],
                  ),
                  clientActionBuilder: _practiceActionBuilder(
                    (_) => confirmed = true,
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pump();

        final button = find.byKey(
          const Key(
            'confirm-practice-plan-'
            '10000000-0000-4000-8000-000000000008-1',
          ),
        );
        expect(tester.takeException(), isNull);
        expect(
          find.bySemanticsLabel(
            '已为你准备好。练习场景：${action.sceneName}。'
            '练习目标：${action.practiceGoal}。角色：'
            '你：${action.userRole} · AI：${action.aiRoles.join('、')}。',
          ),
          findsOneWidget,
        );
        expect(find.bySemanticsLabel('开始练习'), findsOneWidget);
        semantics.dispose();
        await tester.ensureVisible(button);
        await tester.pump();
        expect(button.hitTestable(), findsOneWidget);
        await tester.tap(button);
        expect(confirmed, isTrue);
        expect(tester.takeException(), isNull);
      });
    }
  }

  testWidgets('loads, caches, and toggles an assistant translation', (
    tester,
  ) async {
    const message = AgentMessage(
      id: 'assistant-translation',
      role: AgentMessageRole.assistant,
      text: 'Start with the result.',
    );
    var calls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: message,
            onTranslate: (_) async {
              calls++;
              return '先说结果。';
            },
          ),
        ),
      ),
    );

    final button = find.byKey(
      const Key('agent-assistant-translate-assistant-translation'),
    );
    expect(button, findsOneWidget);
    expect(find.text('先说结果。'), findsNothing);

    await tester.tap(button);
    await tester.pumpAndSettle();
    expect(find.text('先说结果。'), findsOneWidget);
    expect(calls, 1);

    await tester.tap(button);
    await tester.pump();
    expect(find.text('先说结果。'), findsNothing);

    await tester.tap(button);
    await tester.pump();
    expect(find.text('先说结果。'), findsOneWidget);
    expect(calls, 1);
  });

  testWidgets('retries an assistant translation after a request failure', (
    tester,
  ) async {
    const message = AgentMessage(
      id: 'assistant-translation-retry',
      role: AgentMessageRole.assistant,
      text: 'Add one concrete example.',
    );
    var calls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: message,
            onTranslate: (_) async {
              calls++;
              if (calls == 1) {
                throw StateError('temporary failure');
              }
              return '补充一个具体例子。';
            },
          ),
        ),
      ),
    );

    final button = find.byKey(
      const Key('agent-assistant-translate-assistant-translation-retry'),
    );
    await tester.tap(button);
    await tester.pumpAndSettle();
    expect(find.text('翻译失败，请重试。'), findsOneWidget);

    await tester.tap(button);
    await tester.pumpAndSettle();
    expect(find.text('翻译失败，请重试。'), findsNothing);
    expect(find.text('补充一个具体例子。'), findsOneWidget);
    expect(calls, 2);
  });
}

Future<void> _pumpMessage(
  WidgetTester tester,
  AgentMessage message, {
  ValueChanged<ConfirmPracticePlanClientAction>? onClientAction,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: AgentMessageBubble(
          message: message,
          clientActionBuilder: _practiceActionBuilder(onClientAction),
        ),
      ),
    ),
  );
  await tester.pump();
}

AgentClientActionBuilder _practiceActionBuilder(
  ValueChanged<ConfirmPracticePlanClientAction>? onAction,
) {
  return (context, envelope) {
    final action = tryDecodeConfirmPracticePlanClientAction(envelope);
    if (action == null) {
      return null;
    }
    return PracticePlanClientActionCard(
      action: action,
      onConfirm: onAction == null ? null : () => onAction(action),
    );
  };
}

Iterable<SelectableText> _messageSelectableText(
  WidgetTester tester,
  String messageID,
) {
  return tester.widgetList<SelectableText>(
    find.descendant(
      of: find.byKey(Key('agent-assistant-text-$messageID')),
      matching: find.byType(SelectableText),
    ),
  );
}

String _selectablePlainText(SelectableText widget) {
  return widget.textSpan?.toPlainText() ?? widget.data ?? '';
}

Iterable<TextSpan> _flatten(InlineSpan span) sync* {
  if (span is! TextSpan) {
    return;
  }
  yield span;
  for (final child in span.children ?? const <InlineSpan>[]) {
    yield* _flatten(child);
  }
}
