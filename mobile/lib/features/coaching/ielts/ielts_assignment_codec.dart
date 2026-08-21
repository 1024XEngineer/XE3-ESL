import 'dart:convert';

import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

final class IeltsAssignmentWireFormatException implements Exception {
  const IeltsAssignmentWireFormatException();
}

IeltsPracticeAssignment decodeIeltsAssignment(Object? value) {
  final object = _object(
    value,
    required: const <String>{'bank_id', 'season', 'mode', 'parts'},
  );
  final bankId = _resourceId(object['bank_id']);
  final mode = PracticeMode.fromWireValue(_text(object['mode'], maxBytes: 16));
  final rawParts = object['parts'];
  if (mode == null ||
      rawParts is! List<Object?> ||
      rawParts.isEmpty ||
      rawParts.length > 3) {
    throw const IeltsAssignmentWireFormatException();
  }
  final parts = rawParts
      .map((value) => _part(value, bankId))
      .toList(growable: false);
  final expectedParts = switch (mode) {
    PracticeMode.fullMock => const <IeltsSpeakingPart>[
      IeltsSpeakingPart.part1,
      IeltsSpeakingPart.part2,
      IeltsSpeakingPart.part3,
    ],
    PracticeMode.part1 => const <IeltsSpeakingPart>[IeltsSpeakingPart.part1],
    PracticeMode.part2 => const <IeltsSpeakingPart>[
      IeltsSpeakingPart.part2,
      IeltsSpeakingPart.part3,
    ],
    PracticeMode.part3 => const <IeltsSpeakingPart>[IeltsSpeakingPart.part3],
    PracticeMode.fullSimulation ||
    PracticeMode.focus => const <IeltsSpeakingPart>[],
  };
  final topicParts = parts
      .where((part) => part.part != IeltsSpeakingPart.part1)
      .toList(growable: false);
  if (parts.length != expectedParts.length ||
      List<bool>.generate(
        parts.length,
        (index) => parts[index].part == expectedParts[index],
      ).any((matches) => !matches) ||
      (topicParts.length > 1 &&
          (topicParts.map((part) => part.sourceId).toSet().length != 1 ||
              topicParts.map((part) => part.topicTitle).toSet().length != 1)) ||
      parts.fold<int>(0, (total, part) => total + part.turnBlueprints.length) >
          practiceTurnSafetyLimit) {
    throw const IeltsAssignmentWireFormatException();
  }
  return IeltsPracticeAssignment(
    bankId: bankId,
    season: _text(object['season']),
    mode: mode,
    parts: List<IeltsPracticePartAssignment>.unmodifiable(parts),
  );
}

IeltsPracticePartAssignment _part(Object? value, String bankId) {
  final object = _object(
    value,
    required: const <String>{'part', 'source_id', 'turn_blueprints'},
    optional: const <String>{'topic_title', 'cue_card', 'prepared_answers'},
  );
  final part = IeltsSpeakingPart.fromWireValue(
    _text(object['part'], maxBytes: 16),
  );
  final rawBlueprints = object['turn_blueprints'];
  if (part == null ||
      rawBlueprints is! List<Object?> ||
      rawBlueprints.isEmpty ||
      rawBlueprints.length > practiceTurnSafetyLimit) {
    throw const IeltsAssignmentWireFormatException();
  }
  final turnBlueprints = <String>[];
  for (final raw in rawBlueprints) {
    turnBlueprints.add(_text(raw));
  }
  final topicTitle = object.containsKey('topic_title')
      ? _text(object['topic_title'])
      : null;
  final cueCard = object.containsKey('cue_card')
      ? _text(object['cue_card'])
      : null;
  final sourceId = _resourceId(object['source_id']);
  final preparedAnswers = object.containsKey('prepared_answers')
      ? _preparedAnswers(
          object['prepared_answers'],
          bankId: bankId,
          part: part,
          sourceId: sourceId,
          turnCount: turnBlueprints.length,
        )
      : const <IeltsPreparedAnswer>[];
  final validMetadata = switch (part) {
    IeltsSpeakingPart.part1 => topicTitle == null && cueCard == null,
    IeltsSpeakingPart.part2 =>
      topicTitle != null && cueCard != null && turnBlueprints.length == 1,
    IeltsSpeakingPart.part3 => topicTitle != null && cueCard == null,
  };
  if (!validMetadata) {
    throw const IeltsAssignmentWireFormatException();
  }
  return IeltsPracticePartAssignment(
    part: part,
    sourceId: sourceId,
    topicTitle: topicTitle,
    cueCard: cueCard,
    turnBlueprints: List<String>.unmodifiable(turnBlueprints),
    preparedAnswers: preparedAnswers,
  );
}

List<IeltsPreparedAnswer> _preparedAnswers(
  Object? value, {
  required String bankId,
  required IeltsSpeakingPart part,
  required String sourceId,
  required int turnCount,
}) {
  if (value is! List<Object?> || value.length > practiceTurnSafetyLimit) {
    throw const IeltsAssignmentWireFormatException();
  }
  final positions = <int>{};
  final answers = <IeltsPreparedAnswer>[];
  for (final rawAnswer in value) {
    final object = _object(
      rawAnswer,
      required: const <String>{'question_position', 'answer', 'personalized'},
    );
    final position = object['question_position'];
    final personalized = object['personalized'];
    if (position is! int ||
        position < 1 ||
        position > turnCount ||
        !positions.add(position) ||
        personalized is! bool) {
      throw const IeltsAssignmentWireFormatException();
    }
    final answer = _text(object['answer'], maxBytes: 8000);
    if (answer.runes.length > 2000) {
      throw const IeltsAssignmentWireFormatException();
    }
    answers.add(
      IeltsPreparedAnswer(
        bankId: bankId,
        part: part.wireValue,
        sourceId: sourceId,
        questionPosition: position,
        answer: answer,
        personalized: personalized,
      ),
    );
  }
  return List<IeltsPreparedAnswer>.unmodifiable(answers);
}

Map<String, Object?> _object(
  Object? value, {
  required Set<String> required,
  Set<String> optional = const <String>{},
}) {
  if (value is! Map<String, Object?>) {
    throw const IeltsAssignmentWireFormatException();
  }
  final allowed = <String>{...required, ...optional};
  if (!value.keys.toSet().containsAll(required) ||
      value.keys.any((key) => !allowed.contains(key))) {
    throw const IeltsAssignmentWireFormatException();
  }
  return value;
}

String _resourceId(Object? value) => _text(value, maxBytes: 128);

String _text(Object? value, {int maxBytes = 4096}) {
  if (value is! String ||
      value.trim().isEmpty ||
      value.trim() != value ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const IeltsAssignmentWireFormatException();
  }
  return value;
}
