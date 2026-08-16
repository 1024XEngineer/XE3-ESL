import 'dart:typed_data';

import 'package:speakup/features/coaching/practice/practice_media.dart';

final class IeltsQuestionReference {
  const IeltsQuestionReference({
    required this.bankId,
    required this.part,
    required this.sourceId,
    required this.questionPosition,
  });

  final String bankId;
  final String part;
  final String sourceId;
  final int questionPosition;
}

abstract interface class IeltsSpeechClient {
  Future<Uint8List> loadQuestion(IeltsQuestionReference question);
}

final class WireIeltsSpeechClient implements IeltsSpeechClient {
  const WireIeltsSpeechClient(this._mediaClient);

  final PracticeMediaClient _mediaClient;

  @override
  Future<Uint8List> loadQuestion(IeltsQuestionReference question) {
    final bankId = Uri.encodeComponent(question.bankId);
    final part = Uri.encodeComponent(question.part);
    final sourceId = Uri.encodeComponent(question.sourceId);
    return _mediaClient.loadQuestionSpeech(
      '/v1/ielts-speaking/question-banks/$bankId/$part/$sourceId/questions/${question.questionPosition}/speech',
    );
  }
}
