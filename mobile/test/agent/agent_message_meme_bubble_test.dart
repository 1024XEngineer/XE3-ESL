import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_message_bubble.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';

void main() {
  testWidgets('Assistant bubble renders a loaded Meme below text', (
    tester,
  ) async {
    final meme = AgentMessageMeme(
      id: '20000000-0000-4000-8000-000000000003',
      memeId: 'official-001:happy:01',
      category: 'happy',
      contentType: 'image/png',
      sizeBytes: _onePixelPng.length,
      width: 1,
      height: 1,
      contentPath:
          '/v1/agent-message-memes/20000000-0000-4000-8000-000000000003/content',
      bytes: _onePixelPng,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentMessageBubble(
            message: AgentMessage(
              id: 'assistant-1',
              role: AgentMessageRole.assistant,
              text: 'Nice work!',
              memes: <AgentMessageMeme>[meme],
            ),
          ),
        ),
      ),
    );

    expect(find.text('Nice work!'), findsOneWidget);
    expect(
      find.byKey(
        const Key('agent-message-meme-20000000-0000-4000-8000-000000000003'),
      ),
      findsOneWidget,
    );
    expect(find.byType(Image), findsOneWidget);
  });
}

final _onePixelPng = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
);
