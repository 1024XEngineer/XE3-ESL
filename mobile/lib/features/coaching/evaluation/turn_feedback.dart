enum SpeechFeedbackStatus { queued, running, ready, failed }

enum SpeechFeedbackScoreabilityStatus { provisional, insufficient }

enum SpeechFeedbackItemKind { correction, strength, recommendedExpression }

enum SpeechFeedbackRepracticeMode { none, sameQuestion }

enum SpeechFeedbackSourceKind { practiceTurn, agentMessage }

bool validSpeechFeedbackStatusUrl(String value) =>
    speechFeedbackStatusSource(value) != null;

SpeechFeedbackStatusSource? speechFeedbackStatusSource(String value) {
  if (value.length > 160) {
    return null;
  }
  final match = _speechFeedbackStatusUrlPattern.firstMatch(value);
  if (match == null) {
    return null;
  }
  return SpeechFeedbackStatusSource(
    kind: match.namedGroup('kind') == 'practice-turns'
        ? SpeechFeedbackSourceKind.practiceTurn
        : SpeechFeedbackSourceKind.agentMessage,
    sourceId: match.namedGroup('source')!,
  );
}

final class SpeechFeedbackStatusSource {
  const SpeechFeedbackStatusSource({
    required this.kind,
    required this.sourceId,
  });

  final SpeechFeedbackSourceKind kind;
  final String sourceId;
}

final class SpeechFeedbackSource {
  const SpeechFeedbackSource({
    required this.kind,
    required this.sourceId,
    required this.contextId,
  });

  final SpeechFeedbackSourceKind kind;
  final String sourceId;
  final String contextId;
}

final class SpeechFeedbackAnchor {
  const SpeechFeedbackAnchor({
    required this.evidenceRefId,
    required this.startUtf8Byte,
    required this.endUtf8Byte,
    required this.originalExcerpt,
  });

  final String evidenceRefId;
  final int startUtf8Byte;
  final int endUtf8Byte;
  final String originalExcerpt;
}

final class SpeechFeedbackItem {
  const SpeechFeedbackItem({
    required this.feedbackItemId,
    required this.evaluationId,
    required this.position,
    required this.kind,
    required this.anchor,
    required this.explanation,
    required this.repracticeMode,
    required this.createdAt,
    this.severity,
    this.suggestedText,
  });

  final String feedbackItemId;
  final String evaluationId;
  final int position;
  final SpeechFeedbackItemKind kind;
  final String? severity;
  final SpeechFeedbackAnchor anchor;
  final String explanation;
  final String? suggestedText;
  final SpeechFeedbackRepracticeMode repracticeMode;
  final DateTime createdAt;

  bool get canRepractice =>
      repracticeMode == SpeechFeedbackRepracticeMode.sameQuestion;
}

extension SpeechFeedbackItemsPresentation on Iterable<SpeechFeedbackItem> {
  SpeechFeedbackItem? get strength {
    final values = toList(growable: false);
    if (values.length == 1 &&
        values.single.kind == SpeechFeedbackItemKind.strength) {
      return values.single;
    }
    return null;
  }

  SpeechFeedbackItem? get correction {
    if (strength != null) {
      return null;
    }
    for (final item in this) {
      if (item.kind == SpeechFeedbackItemKind.correction &&
          item.hasLocatableLanguageCorrection) {
        return item;
      }
    }
    return null;
  }

  SpeechFeedbackItem? get polish {
    if (strength != null) {
      return null;
    }
    for (final item in this) {
      if (item.kind == SpeechFeedbackItemKind.recommendedExpression &&
          item.suggestedText != null) {
        return item;
      }
    }
    return null;
  }

  List<SpeechFeedbackItem> get presentationItems {
    final noChange = strength;
    if (noChange != null) {
      return List<SpeechFeedbackItem>.unmodifiable([noChange]);
    }
    return List<SpeechFeedbackItem>.unmodifiable(
      where(
        (item) =>
            item.kind != SpeechFeedbackItemKind.strength &&
            (item.kind != SpeechFeedbackItemKind.correction ||
                item.hasLocatableLanguageCorrection),
      ),
    );
  }
}

extension SpeechFeedbackItemContract on SpeechFeedbackItem {
  bool get hasLocatableLanguageCorrection {
    final suggested = suggestedText;
    if (kind != SpeechFeedbackItemKind.correction || suggested == null) {
      return false;
    }
    final before = _speechFeedbackWords(anchor.originalExcerpt);
    final after = _speechFeedbackWords(suggested);
    if (before.isEmpty || _sameSpeechFeedbackWords(before, after)) {
      return false;
    }
    if (before.length == 1 && after.length == 1) {
      final source = before.single;
      final replacement = after.single;
      if (source != replacement &&
          (source == 'and' || source == 'so') &&
          (replacement == 'and' || replacement == 'so')) {
        return false;
      }
    }
    return true;
  }
}

List<String> _speechFeedbackWords(String value) => RegExp(
  r"[A-Za-z]+(?:['’][A-Za-z]+)*",
).allMatches(value).map((match) => match.group(0)!.toLowerCase()).toList();

bool _sameSpeechFeedbackWords(List<String> left, List<String> right) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) {
      return false;
    }
  }
  return true;
}

final class SpeechFeedbackAcousticAssessment {
  const SpeechFeedbackAcousticAssessment.notAssessed({required this.reason})
    : pronunciationScore = null,
      fluencyScore = null,
      integrityScore = null,
      speakingSpeedWpm = null;

  const SpeechFeedbackAcousticAssessment.assessed({
    required this.pronunciationScore,
    this.fluencyScore,
    this.integrityScore,
    this.speakingSpeedWpm,
  }) : reason = null;

  final String? reason;
  final double? pronunciationScore;
  final double? fluencyScore;
  final double? integrityScore;
  final double? speakingSpeedWpm;

  bool get isAssessed => pronunciationScore != null;
}

final class SpeechFeedbackStableFailure {
  const SpeechFeedbackStableFailure({
    required this.code,
    required this.retryable,
    required this.message,
  });

  final String code;
  final bool retryable;
  final String message;
}

final class SpeechFeedback {
  const SpeechFeedback({
    required this.evaluationId,
    required this.source,
    required this.feedbackStatus,
    required this.items,
    required this.statusUrl,
    required this.createdAt,
    required this.updatedAt,
    required this.reasonCodes,
    this.summary,
    this.scoreabilityStatus,
    this.acousticAssessment,
    this.stableFailure,
  });

  final String evaluationId;
  final SpeechFeedbackSource source;
  final SpeechFeedbackStatus feedbackStatus;
  final SpeechFeedbackScoreabilityStatus? scoreabilityStatus;
  final String? summary;
  final List<String> reasonCodes;
  final List<SpeechFeedbackItem> items;
  final SpeechFeedbackAcousticAssessment? acousticAssessment;
  final SpeechFeedbackStableFailure? stableFailure;
  final String statusUrl;
  final DateTime createdAt;
  final DateTime updatedAt;

  bool get isPending =>
      feedbackStatus == SpeechFeedbackStatus.queued ||
      feedbackStatus == SpeechFeedbackStatus.running;
}

final _speechFeedbackStatusUrlPattern = RegExp(
  r'^/v1/(?<kind>practice-turns|agent-messages)/(?<source>[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})/evaluation$',
);
