import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

enum IeltsSpeakingPart {
  part1('PART_1'),
  part2('PART_2'),
  part3('PART_3');

  const IeltsSpeakingPart(this.wireValue);

  final String wireValue;

  static IeltsSpeakingPart? fromWireValue(String value) {
    for (final part in values) {
      if (part.wireValue == value) {
        return part;
      }
    }
    return null;
  }
}

final class IeltsPracticePartAssignment {
  const IeltsPracticePartAssignment({
    required this.part,
    required this.sourceId,
    required this.turnBlueprints,
    this.topicTitle,
    this.cueCard,
    this.preparedAnswers = const <IeltsPreparedAnswer>[],
  });

  final IeltsSpeakingPart part;
  final String sourceId;
  final String? topicTitle;
  final String? cueCard;
  final List<String> turnBlueprints;
  final List<IeltsPreparedAnswer> preparedAnswers;

  Map<String, Object> toJson() => <String, Object>{
    'part': part.wireValue,
    'source_id': sourceId,
    'topic_title': ?topicTitle,
    'cue_card': ?cueCard,
    'turn_blueprints': turnBlueprints,
    if (preparedAnswers.isNotEmpty)
      'prepared_answers': preparedAnswers
          .map(
            (answer) => <String, Object>{
              'question_position': answer.questionPosition,
              'answer': answer.answer,
              'personalized': answer.personalized,
            },
          )
          .toList(growable: false),
  };

  @override
  bool operator ==(Object other) =>
      other is IeltsPracticePartAssignment &&
      other.part == part &&
      other.sourceId == sourceId &&
      other.topicTitle == topicTitle &&
      other.cueCard == cueCard &&
      _sameStrings(other.turnBlueprints, turnBlueprints) &&
      _samePreparedAnswers(other.preparedAnswers, preparedAnswers);

  @override
  int get hashCode => Object.hash(
    part,
    sourceId,
    topicTitle,
    cueCard,
    Object.hashAll(turnBlueprints),
    Object.hashAll(preparedAnswers.map(_preparedAnswerHash)),
  );
}

final class IeltsPracticeAssignment {
  const IeltsPracticeAssignment({
    required this.bankId,
    required this.season,
    required this.mode,
    required this.parts,
  });

  final String bankId;
  final String season;
  final PracticeMode mode;
  final List<IeltsPracticePartAssignment> parts;

  List<String> get turnBlueprints =>
      List<String>.unmodifiable(parts.expand((part) => part.turnBlueprints));

  IeltsPracticePartAssignment? part(IeltsSpeakingPart value) =>
      parts.where((part) => part.part == value).firstOrNull;

  bool matchesSelection(IeltsPracticeSelection selection) {
    if (selection.cueCardType != null) {
      return selection.isValidForCreateMode(mode);
    }
    final part1Source = part(IeltsSpeakingPart.part1)?.sourceId;
    final topicSource =
        part(IeltsSpeakingPart.part2)?.sourceId ??
        part(IeltsSpeakingPart.part3)?.sourceId;
    return part1Source == selection.part1SetId &&
        topicSource == selection.topicGroupId;
  }

  Map<String, Object> toJson() => <String, Object>{
    'bank_id': bankId,
    'season': season,
    'mode': mode.wireValue,
    'parts': parts.map((part) => part.toJson()).toList(growable: false),
  };

  @override
  bool operator ==(Object other) =>
      other is IeltsPracticeAssignment &&
      other.bankId == bankId &&
      other.season == season &&
      other.mode == mode &&
      _sameParts(other.parts, parts);

  @override
  int get hashCode => Object.hash(bankId, season, mode, Object.hashAll(parts));
}

bool _sameParts(
  List<IeltsPracticePartAssignment> left,
  List<IeltsPracticePartAssignment> right,
) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) => left[index] == right[index],
    ).every((same) => same);

bool _sameStrings(List<String> left, List<String> right) =>
    left.length == right.length &&
    List<bool>.generate(
      left.length,
      (index) => left[index] == right[index],
    ).every((same) => same);

bool _samePreparedAnswers(
  List<IeltsPreparedAnswer> left,
  List<IeltsPreparedAnswer> right,
) =>
    left.length == right.length &&
    List<bool>.generate(left.length, (index) {
      final leftAnswer = left[index];
      final rightAnswer = right[index];
      return leftAnswer.bankId == rightAnswer.bankId &&
          leftAnswer.part == rightAnswer.part &&
          leftAnswer.sourceId == rightAnswer.sourceId &&
          leftAnswer.questionPosition == rightAnswer.questionPosition &&
          leftAnswer.answer == rightAnswer.answer &&
          leftAnswer.personalized == rightAnswer.personalized;
    }).every((same) => same);

int _preparedAnswerHash(IeltsPreparedAnswer answer) => Object.hash(
  answer.bankId,
  answer.part,
  answer.sourceId,
  answer.questionPosition,
  answer.answer,
  answer.personalized,
);
