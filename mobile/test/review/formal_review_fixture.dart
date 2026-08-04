import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/review/formal_review.dart';

FormalReview legacyFormalReviewFixture({
  required AgentReview review,
  required String practiceSessionId,
  required DateTime createdAt,
  required DateTime completedAt,
  int overallScore = 80,
}) {
  return FormalReview(
    id: review.id,
    practiceSessionId: practiceSessionId,
    status: FormalReviewStatus.completed,
    schema: FormalReviewSchema.legacyVoiceV1,
    implementationVersion: 'review-v1',
    sourceTurnId: 'turn-${review.id}',
    sourceTurnVersion: 'conversation-turn:evidence-v1',
    result: FormalReviewResult(
      eligibility: FormalReviewSummaryEligibility.eligible,
      overallScore: overallScore,
      summary: review.summary,
      dimensions: <FormalReviewDimension>[
        FormalReviewDimension(
          key: 'clarity',
          category: 'clarity',
          message: review.strength,
          suggestion: review.nextFocus,
        ),
      ],
      feedbackItems: const <FormalReviewFeedbackItem>[],
      repracticeSuggestionRefs: const <String>[],
      insufficientEvidenceReasons: const <String>[],
    ),
    createdAt: createdAt,
    updatedAt: completedAt,
    completedAt: completedAt,
  );
}
