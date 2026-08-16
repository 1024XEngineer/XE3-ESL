import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_answer_generation.dart';
import 'package:speakup/features/coaching/ielts/ielts_set_detail.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_speech_client.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  testWidgets('prepares a personalized answer before starting practice', (
    tester,
  ) async {
    final generator = _AnswerGenerator();
    final speaker = _AnswerSpeaker();
    List<IeltsPreparedAnswer>? preparedAnswers;
    await tester.pumpWidget(
      MaterialApp(
        home: IeltsSetDetailPage(
          mode: PracticeMode.part1,
          title: 'Music',
          subtitle: '音乐',
          questions: const ['What music do you like?'],
          questionReferences: const [
            IeltsQuestionReference(
              bankId: 'bank_1',
              part: 'PART_1',
              sourceId: 'topic_1',
              questionPosition: 1,
            ),
          ],
          answerGenerator: generator,
          answerSpeaker: speaker,
          onStart: (answers) => preparedAnswers = answers,
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('ielts-answer-toggle-1')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('生成示例回答'));
    await tester.pumpAndSettle();

    expect(find.text('参考回答'), findsOneWidget);
    expect(find.text('定制'), findsOneWidget);
    await tester.tap(find.byKey(const Key('ielts-answer-speak-1')));
    await tester.pumpAndSettle();
    expect(speaker.spoken, 'I like jazz because it helps me relax.');

    await tester.tap(find.byKey(const Key('ielts-answer-adjust-1')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('ielts-personal-answer-points')),
      'I listen to jazz on my commute.',
    );
    await tester.tap(find.byKey(const Key('ielts-generate-personal-answer')));
    await tester.pumpAndSettle();

    expect(generator.points, ['I listen to jazz on my commute.']);
    expect(find.text('我的回答'), findsOneWidget);
    expect(find.text('调整'), findsOneWidget);
    expect(find.text('I like jazz because it helps me relax.'), findsOneWidget);
    expect(find.byKey(const Key('ielts-set-detail-start')), findsOneWidget);
    await tester.tap(find.byKey(const Key('ielts-set-detail-start')));
    expect(preparedAnswers, hasLength(1));
    expect(
      preparedAnswers!.single.answer,
      'I like jazz because it helps me relax.',
    );
    expect(preparedAnswers!.single.personalized, isTrue);
  });
}

final class _AnswerSpeaker implements PracticePromptSpeaker {
  String? spoken;

  @override
  Future<void> speak(String text) async => spoken = text;

  @override
  Future<void> stop() async {}

  @override
  Future<void> dispose() async {}
}

final class _AnswerGenerator implements IeltsAnswerGenerator {
  List<String>? points;

  @override
  Future<IeltsGeneratedAnswer> generate({
    required IeltsQuestionReference question,
    required List<String> personalPoints,
    double targetBand = 7,
  }) async {
    points = personalPoints;
    return const IeltsGeneratedAnswer(
      answer: 'I like jazz because it helps me relax.',
      outline: ['answer', 'reason'],
      usefulExpressions: ['helps me relax'],
      speechText: 'I like jazz because it helps me relax.',
    );
  }
}
