import 'dart:typed_data';

import 'package:speakup/features/coaching/ielts/ielts_answer_preparation.dart';
import 'package:speakup/features/coaching/practice/practice_media.dart';

abstract interface class IeltsSpeechClient {
  Future<Uint8List> loadQuestion(IeltsAnswerQuestionReference question);

  Future<Uint8List> loadAnswer(String answerPreparationId);
}

final class WireIeltsSpeechClient implements IeltsSpeechClient {
  const WireIeltsSpeechClient(this._mediaClient);

  final PracticeMediaClient _mediaClient;

  @override
  Future<Uint8List> loadQuestion(IeltsAnswerQuestionReference question) {
    final bankId = Uri.encodeComponent(question.bankId);
    final part = Uri.encodeComponent(question.part);
    final sourceId = Uri.encodeComponent(question.sourceId);
    return _mediaClient.loadQuestionSpeech(
      '/v1/ielts-speaking/question-banks/$bankId/$part/$sourceId/questions/${question.questionPosition}/speech',
    );
  }

  @override
  Future<Uint8List> loadAnswer(
    String answerPreparationId,
  ) => _mediaClient.loadQuestionSpeech(
    '/v1/ielts-speaking/answer-preparations/${Uri.encodeComponent(answerPreparationId)}/speech',
  );
}
