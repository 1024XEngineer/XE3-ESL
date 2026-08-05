import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
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

  testWidgets('renders and dispatches a practice plan handoff', (tester) async {
    const handoff = ConfirmPracticePlanHandoff(
      label: '确认并开始练习',
      practicePlanId: '10000000-0000-4000-8000-000000000001',
      planRevision: 2,
      target: 'Java Interview Practice',
      sceneName: '项目经历深挖',
      sceneFamily: 'INTERVIEW',
      sceneModel: 'PROJECT_EXPERIENCE_DEEP_DIVE',
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
    expect(find.text('场景：项目经历深挖'), findsOneWidget);
    expect(find.text('角色：面试官、候选人'), findsOneWidget);
    expect(find.text('预计 12 分钟 · 3–5 轮'), findsOneWidget);
    expect(find.text('确认并开始练习'), findsOneWidget);
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
}

Future<void> _pumpMessage(WidgetTester tester, AgentMessage message) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(body: AgentMessageBubble(message: message)),
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
