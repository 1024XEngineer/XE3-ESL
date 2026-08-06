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
  testWidgets('voice reply forwards assistant deltas before commit', (
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
    await controller.startLiveAssistantSpeech(
      transientMessageId: 'stream-run-a',
    );
    controller.appendLiveAssistantSpeech(
      transientMessageId: 'stream-run-a',
      delta: 'First sentence. Second sentence',
    );
    await tester.pump();

    expect(speech.texts, <String>['First sentence. Second sentence']);
    expect(controller.playingMessageId, 'stream-run-a');

    const committed = AgentMessage(
      id: 'assistant-a',
      role: AgentMessageRole.assistant,
      text: 'First sentence. Second sentence',
    );
    controller.completeLiveAssistantSpeech(
      transientMessageId: 'stream-run-a',
      message: committed,
    );
    conversation.changeComposerStreamMessage('stream-run-a', committed);
    await tester.pump();
    expect(speech.texts, <String>['First sentence. Second sentence']);
    expect(controller.playingMessageId, 'assistant-a');
    player.complete();
    await tester.pump();
    expect(controller.playingMessageId, isNull);
  });

  testWidgets('voice reply fills streamed suffix from committed message', (
    tester,
  ) async {
    final conversation = ConversationController(client: FakeAgentClient());
    await conversation.initialize();
    final speech = _BlockingAssistantSpeechClient();
    final controller = AgentMessageAudioController(
      conversationController: conversation,
      client: _FakeMessageAudioClient(),
      audioPlayer: FakeAgentAudioPlayer(),
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
        text: 'Please correct me.',
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
        id: 'stream-run-b',
        role: AgentMessageRole.assistant,
        text: 'Actually.',
        isStreaming: true,
      ),
    );
    await controller.startLiveAssistantSpeech(
      transientMessageId: 'stream-run-b',
    );
    controller.appendLiveAssistantSpeech(
      transientMessageId: 'stream-run-b',
      delta: 'Actually.',
    );
    await tester.pump();
    expect(speech.texts, <String>['Actually.']);

    const committed = AgentMessage(
      id: 'assistant-b',
      role: AgentMessageRole.assistant,
      text: 'Actually. Please say the complete sentence again.',
    );
    controller.completeLiveAssistantSpeech(
      transientMessageId: 'stream-run-b',
      message: committed,
    );
    conversation.changeComposerStreamMessage('stream-run-b', committed);
    unawaited(controller.playCommittedAssistant(committed));
    await tester.pump();
    speech.releaseFirstSegment();
    await tester.pumpAndSettle();

    expect(speech.texts, <String>[
      'Actually.',
      ' Please say the complete sentence again.',
    ]);
    expect(speech.cancelledBeforeRelease, isFalse);
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
      for (var chunkIndex = 1; chunkIndex <= 2; chunkIndex++) {
        yield AgentAssistantSpeechAudioSegment(
          sequence: segment.sequence,
          chunkIndex: chunkIndex,
          audio: Uint8List.fromList(<int>[1, 2, 3, 4]),
        );
      }
    }
  }
}

final class _BlockingAssistantSpeechClient
    implements AgentAssistantSpeechClient {
  final List<String> texts = <String>[];
  final Completer<void> _firstSegmentRelease = Completer<void>();
  bool _released = false;
  bool cancelledBeforeRelease = false;

  void releaseFirstSegment() {
    _released = true;
    _firstSegmentRelease.complete();
  }

  @override
  Stream<AgentAssistantSpeechAudioSegment> streamAssistantSpeech({
    required String threadId,
    required Stream<AgentAssistantSpeechTextSegment> segments,
  }) async* {
    try {
      await for (final segment in segments) {
        texts.add(segment.text);
        yield AgentAssistantSpeechAudioSegment(
          sequence: segment.sequence,
          chunkIndex: 1,
          audio: Uint8List.fromList(<int>[1, 2, 3, 4]),
        );
        if (segment.sequence == 1) {
          await _firstSegmentRelease.future;
        }
      }
    } finally {
      if (!_released) {
        cancelledBeforeRelease = true;
      }
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
