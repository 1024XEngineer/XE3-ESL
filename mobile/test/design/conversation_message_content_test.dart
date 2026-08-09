import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/conversation_message_content.dart';

void main() {
  testWidgets('renders shared assistant Markdown without loading images', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ConversationTextMessageContent(
            sourceId: 'assistant-1',
            text: '**Clear answer**\n\n![diagram](https://example.com/a.png)',
            isUser: false,
            translationButtonKey: Key('translate'),
            translationContentKey: Key('translation'),
            translationErrorKey: Key('translation-error'),
          ),
        ),
      ),
    );

    expect(find.textContaining('Clear answer'), findsOneWidget);
    expect(find.text('[图片：diagram]'), findsOneWidget);
    expect(find.byType(Image), findsNothing);
  });

  testWidgets('shows a streaming placeholder and withholds translation', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ConversationTextMessageContent(
            sourceId: 'assistant-stream',
            text: '',
            isUser: false,
            isStreaming: true,
            streamingKey: const Key('streaming'),
            translationButtonKey: const Key('translate'),
            translationContentKey: const Key('translation'),
            translationErrorKey: const Key('translation-error'),
            onTranslate: () async => '不应请求',
          ),
        ),
      ),
    );

    expect(find.byKey(const Key('streaming')), findsOneWidget);
    expect(find.byKey(const Key('translate')), findsNothing);
  });

  testWidgets('hosts playback and translation in one shared action row', (
    tester,
  ) async {
    var playbackCalls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ConversationTextMessageContent(
            sourceId: 'assistant-actions',
            text: 'How are you?',
            isUser: false,
            translationButtonKey: const Key('translate'),
            translationContentKey: const Key('translation'),
            translationErrorKey: const Key('translation-error'),
            onTranslate: () async => '你好吗？',
            leadingActions: [
              TextButton(
                key: const Key('play'),
                onPressed: () => playbackCalls++,
                child: const Text('播放'),
              ),
            ],
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('play')));
    await tester.tap(find.byKey(const Key('translate')));
    await tester.pump();

    expect(playbackCalls, 1);
    expect(find.text('你好吗？'), findsOneWidget);
  });
}
