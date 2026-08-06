import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/audio/agent_audio_player.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_client.dart';
import 'package:speakup/features/agent/conversation/agent_message_audio_controller.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';

void main() {
  testWidgets('voice reply speaks complete sentences before commit', (
    tester,
  ) async {
    final conversation = ConversationController(client: FakeAgentClient());
    await conversation.initialize();
    final media = _FakeMessageAudioClient();
    final speech = _FakeAssistantSpeechClient();
    final player = FakeAgentAudioPlayer();
    final controller = AgentMessageAudioController(
      conversationController: conversation,
      client: media,
      audioPlayer: player,
      assistantSpeechClient: speech,
    );
    addTearDown(() {
      controller.dispose();
      conversation.dispose();
    });

    conversation.commitComposerMessages(const <AgentMessage>[
      AgentMessage(
        id: 'voice-user',
        role: AgentMessageRole.user,
        text: 'Please help me practice.',
        modality: AgentMessageModality.voice,
        audio: AgentMessageAudio(
          id: 'voice-audio',
          status: AgentMessageAudioStatus.readable,
          contentType: 'audio/wav',
          sizeBytes: 64,
          duration: Duration(seconds: 2),
          playbackPath: '/audio',
        ),
      ),
    ]);
    conversation.changeComposerStreamMessage(
      null,
      const AgentMessage(
        id: 'stream-run-a',
        role: AgentMessageRole.assistant,
        text: '',
        isStreaming: true,
      ),
    );
    conversation.changeComposerStreamMessage(
      'stream-run-a',
      const AgentMessage(
        id: 'stream-run-a',
        role: AgentMessageRole.assistant,
        text: 'First sentence. Second sentence',
        isStreaming: true,
      ),
    );
    await tester.pump();

    expect(speech.texts, <String>['First sentence.']);
    expect(controller.playingMessageId, 'stream-run-a');

    conversation.changeComposerStreamMessage(
      'stream-run-a',
      const AgentMessage(
        id: 'assistant-a',
        role: AgentMessageRole.assistant,
        text: 'First sentence. Second sentence',
      ),
    );
    await tester.pump();
    expect(speech.texts, <String>['First sentence.', 'Second sentence']);

    player.complete();
    await tester.pump();
    expect(controller.playingMessageId, 'assistant-a');
    player.complete();
    await tester.pump();
    expect(controller.playingMessageId, isNull);
  });
}

final class _FakeAssistantSpeechClient implements AgentAssistantSpeechClient {
  final List<String> texts = <String>[];

  @override
  Stream<AgentAssistantSpeechAudioSegment> streamAssistantSpeech({
    required String threadId,
    required Stream<AgentAssistantSpeechTextSegment> segments,
  }) async* {
    await for (final segment in segments) {
      texts.add(segment.text);
      yield AgentAssistantSpeechAudioSegment(
        sequence: segment.sequence,
        audio: Uint8List.fromList(<int>[1, 2, 3]),
      );
    }
  }
}

final class _FakeMessageAudioClient implements AgentMessageAudioClient {
  @override
  Future<void> deleteMessageAudio({required String audioId}) async {}

  @override
  Future<Uint8List> loadAssistantSpeech({required String messageId}) async =>
      Uint8List(0);

  @override
  Future<Uint8List> loadMessageAudio({required String audioId}) async =>
      Uint8List(0);

  @override
  Future<Uint8List> loadSpeechPreview({
    required String messageId,
    required String text,
  }) async => Uint8List(0);
}
