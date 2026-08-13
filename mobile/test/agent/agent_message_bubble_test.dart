import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/agent_message_bubble.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';

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
          threadId: 'thread-1',
          contentType: 'image/png',
          sizeBytes: 128,
          width: 32,
          height: 24,
          status: AgentImageAssetStatus.attached,
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

  testWidgets('renders and dispatches a practice plan handoff', (tester) async {
    const handoff = ConfirmPracticePlanHandoff(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000001',
      planRevision: 2,
      target: 'Java Interview Practice',
      sceneName: '项目经历深挖',
      practiceExperience: 'INTERVIEW',
      sceneCategory: 'INTERVIEW_PROFESSIONAL',
      practiceMode: 'FULL_SIMULATION',
      roles: <String>['面试官', '候选人'],
      practiceScope: '围绕项目难点完成三轮追问',
      suggestedDuration: Duration(minutes: 12),
      minEffectiveTurns: 3,
      maxEffectiveTurns: 5,
      executableStatus: 'ready',
      confirmationPrompt: '请确认是否按此方案开始练习。',
    );
    AgentHandoff? selected;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: const AgentMessage(
              id: 'assistant-interview',
              role: AgentMessageRole.assistant,
              text: '面试场景已创建。',
              handoffs: <AgentHandoff>[handoff],
            ),
            onHandoff: (value) => selected = value,
          ),
        ),
      ),
    );

    expect(find.text('Java Interview Practice'), findsOneWidget);
    expect(find.text('已为你准备好'), findsOneWidget);
    expect(find.text('围绕项目难点完成三轮追问'), findsNothing);
    expect(find.text('面试官、候选人'), findsOneWidget);
    expect(find.text('约 12 分钟'), findsOneWidget);
    expect(find.text('3–5 个问题'), findsOneWidget);
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
    expect(selected, same(handoff));
  });

  testWidgets('renders an unscored IELTS warm-up without a handoff', (
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

  testWidgets('renders and dispatches the later Part 1 confirmation handoff', (
    tester,
  ) async {
    const handoff = ConfirmPracticePlanHandoff(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000003',
      planRevision: 1,
      target: '按所选 IELTS 口语模式完成真实节奏的连续表达。',
      sceneName: 'IELTS 口语',
      practiceExperience: 'IELTS_SPEAKING',
      sceneCategory: 'IELTS_SPEAKING',
      practiceMode: 'PART_1',
      roles: <String>['IELTS 口语考官'],
      practiceScope: 'Part 1',
      suggestedDuration: Duration(minutes: 5),
      minEffectiveTurns: 3,
      maxEffectiveTurns: 3,
      executableStatus: 'ready',
      confirmationPrompt: '确认后进入正式练习。',
    );
    AgentHandoff? selected;
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'assistant-ielts-part-1-confirmation',
        role: AgentMessageRole.assistant,
        text: '听到了，你是在确认我能不能听见。',
        handoffs: <AgentHandoff>[handoff],
      ),
      onHandoff: (value) => selected = value,
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
    expect(find.text(handoff.target), findsNothing);
    expect(find.text('IELTS 口语考官 · IELTS Examiner'), findsOneWidget);
    expect(find.text('约 5 分钟'), findsOneWidget);
    expect(find.text('3 个问题'), findsOneWidget);
    expect(find.bySemanticsLabel('IELTS 考官头像'), findsOneWidget);
    final card = find.byKey(
      const Key(
        'agent-handoff-practice-plan-'
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
    expect(find.text(handoff.confirmationPrompt), findsNothing);
    expect(find.text('开始练习'), findsOneWidget);

    await tester.tap(
      find.byKey(
        const Key(
          'confirm-practice-plan-'
          '10000000-0000-4000-8000-000000000003-1',
        ),
      ),
    );
    expect(selected, same(handoff));
  });

  testWidgets('keeps a full mock handoff free of warm-up and question text', (
    tester,
  ) async {
    const handoff = ConfirmPracticePlanHandoff(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000004',
      planRevision: 1,
      target: '按所选 IELTS 口语模式完成真实节奏的连续表达。',
      sceneName: 'IELTS 口语',
      practiceExperience: 'IELTS_SPEAKING',
      sceneCategory: 'IELTS_SPEAKING',
      practiceMode: 'FULL_MOCK',
      roles: <String>['IELTS 口语考官'],
      practiceScope: '完整模考',
      suggestedDuration: Duration(minutes: 14),
      minEffectiveTurns: 14,
      maxEffectiveTurns: 14,
      executableStatus: 'ready',
      confirmationPrompt: '题目将在正式开始后展示。',
    );
    await _pumpMessage(
      tester,
      const AgentMessage(
        id: 'assistant-ielts-full-mock',
        role: AgentMessageRole.assistant,
        text: '好。',
        handoffs: <AgentHandoff>[handoff],
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
        'agent-handoff-practice-plan-'
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

  testWidgets('does not show a round count for an open-ended handoff', (
    tester,
  ) async {
    const handoff = ConfirmPracticePlanHandoff(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000002',
      planRevision: 1,
      target: 'Travel English Practice',
      sceneName: '酒店入住',
      practiceExperience: 'LIFE_AND_TRAVEL',
      sceneCategory: 'LIFE_TRAVEL',
      practiceMode: 'FULL_SIMULATION',
      roles: <String>['前台'],
      practiceScope: '开放对话',
      suggestedDuration: Duration(minutes: 10),
      minEffectiveTurns: 1,
      maxEffectiveTurns: 0,
      executableStatus: 'ready',
      confirmationPrompt: '请确认是否开始练习。',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: const AgentMessage(
              id: 'assistant-open-practice',
              role: AgentMessageRole.assistant,
              text: '旅行场景已创建。',
              handoffs: <AgentHandoff>[handoff],
            ),
          ),
        ),
      ),
    );

    expect(find.text('约 10 分钟'), findsOneWidget);
    expect(find.textContaining('轮'), findsNothing);
  });

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
  ValueChanged<AgentHandoff>? onHandoff,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: AgentMessageBubble(message: message, onHandoff: onHandoff),
      ),
    ),
  );
  await tester.pump();
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
