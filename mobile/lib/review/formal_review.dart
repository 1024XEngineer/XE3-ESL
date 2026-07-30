enum FormalReviewSchema { legacyVoiceV1, scenarioV2 }

enum FormalReviewStatus { pending, generating, completed, failed }

enum FormalReviewContextType {
  interviewProjectDeepDive,
  ieltsSpeakingPart2,
  workplaceProgressRiskUpdate,
  dailyHotelCheckinIssue,
  genericPractice,
}

enum FormalReviewSummaryEligibility {
  eligible,
  provisional,
  insufficientEvidence,
}

enum FormalReviewFeedbackKind {
  correction,
  strength,
  improvement,
  recommendedExpression,
}

final class FormalReview {
  const FormalReview({
    required this.id,
    required this.practiceSessionId,
    required this.status,
    required this.schema,
    required this.implementationVersion,
    required this.sourceTurnId,
    required this.sourceTurnVersion,
    required this.createdAt,
    required this.updatedAt,
    this.contextType,
    this.result,
    this.completedAt,
  });

  final String id;
  final String practiceSessionId;
  final FormalReviewStatus status;
  final FormalReviewSchema schema;
  final String implementationVersion;
  final String sourceTurnId;
  final String sourceTurnVersion;
  final FormalReviewContextType? contextType;
  final FormalReviewResult? result;
  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? completedAt;
}

final class FormalReviewResult {
  const FormalReviewResult({
    required this.eligibility,
    required this.summary,
    required this.dimensions,
    required this.feedbackItems,
    required this.repracticeSuggestionRefs,
    required this.insufficientEvidenceReasons,
    this.overallScore,
  });

  final FormalReviewSummaryEligibility eligibility;
  final int? overallScore;
  final String summary;
  final List<FormalReviewDimension> dimensions;
  final List<FormalReviewFeedbackItem> feedbackItems;
  final List<String> repracticeSuggestionRefs;
  final List<String> insufficientEvidenceReasons;
}

final class FormalReviewDimension {
  const FormalReviewDimension({
    required this.key,
    required this.category,
    required this.message,
    this.score,
    this.suggestion,
  });

  final String key;
  final String category;
  final int? score;
  final String message;
  final String? suggestion;
}

final class FormalReviewFeedbackItem {
  const FormalReviewFeedbackItem({
    required this.key,
    required this.kind,
    required this.message,
    this.suggestion,
  });

  final String key;
  final FormalReviewFeedbackKind kind;
  final String message;
  final String? suggestion;
}
