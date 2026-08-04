import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/coaching/review/formal_review.dart';

AgentReview presentFormalReview(FormalReview review) {
  final result = review.result;
  if (review.status != FormalReviewStatus.completed || result == null) {
    throw StateError('A completed FormalReview result is required.');
  }
  final strength =
      result.feedbackItems
          .where((item) => item.kind == FormalReviewFeedbackKind.strength)
          .map((item) => item.message)
          .firstOrNull ??
      result.dimensions.firstOrNull?.message ??
      result.summary;
  String? nextFocus;
  for (final ref in result.repracticeSuggestionRefs) {
    final item = result.feedbackItems
        .where((candidate) => candidate.key == ref)
        .firstOrNull;
    if (item != null) {
      nextFocus = item.suggestion ?? item.message;
      break;
    }
  }
  nextFocus ??=
      result.feedbackItems
          .where(
            (item) =>
                item.kind == FormalReviewFeedbackKind.correction ||
                item.kind == FormalReviewFeedbackKind.improvement,
          )
          .map((item) => item.suggestion ?? item.message)
          .firstOrNull ??
      result.dimensions
          .map((item) => item.suggestion)
          .whereType<String>()
          .firstOrNull ??
      result.dimensions.lastOrNull?.message ??
      result.summary;
  return AgentReview(
    id: review.id,
    title: formalReviewTitle(review),
    summary: result.summary,
    strength: strength,
    nextFocus: nextFocus,
  );
}

String formalReviewTitle(FormalReview review) {
  final result = review.result;
  if (result == null) {
    throw StateError('A FormalReview result is required.');
  }
  if (review.schema == FormalReviewSchema.legacyVoiceV1) {
    return '本次练习 · ${result.overallScore} 分';
  }
  final base = switch (review.contextType!) {
    FormalReviewContextType.interviewProjectDeepDive => '面试复盘',
    FormalReviewContextType.ieltsSpeakingPart2 => 'IELTS 口语练习',
    FormalReviewContextType.workplaceProgressRiskUpdate => '职场英语复盘',
    FormalReviewContextType.dailyHotelCheckinIssue => '日常英语复盘',
    FormalReviewContextType.genericPractice => '情景练习复盘',
  };
  if (result.eligibility ==
      FormalReviewSummaryEligibility.insufficientEvidence) {
    return '$base · 证据不足';
  }
  if (result.eligibility == FormalReviewSummaryEligibility.provisional) {
    return '$base · 暂定反馈';
  }
  return result.overallScore == null
      ? base
      : '$base · ${result.overallScore} 分';
}
