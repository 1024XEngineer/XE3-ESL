import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';

final class IeltsPracticeReportDetail {
  const IeltsPracticeReportDetail({
    required this.reportScope,
    required this.availableSections,
    required this.questions,
    required this.sectionReviews,
  });

  final PracticeReportScope reportScope;
  final List<IeltsSpeakingPartId> availableSections;
  final List<IeltsPracticeReportQuestion> questions;
  final List<IeltsPracticeSectionReview> sectionReviews;
}

final class IeltsPracticeReportQuestion {
  const IeltsPracticeReportQuestion({
    required this.questionId,
    required this.partId,
    required this.index,
    required this.questionText,
    required this.evidenceRefIds,
    this.confirmedTranscript,
    this.responseTurnId,
  });

  final String questionId;
  final IeltsSpeakingPartId partId;
  final int index;
  final String questionText;
  final String? confirmedTranscript;
  final String? responseTurnId;
  final List<String> evidenceRefIds;
}

final class IeltsPracticeSectionReview {
  const IeltsPracticeSectionReview({
    required this.partId,
    required this.questionIndexes,
    required this.evidenceRefIds,
    required this.strengthFindingIds,
    required this.improvementFindingIds,
    required this.upgradeExampleFindingIds,
  });

  final IeltsSpeakingPartId partId;
  final List<int> questionIndexes;
  final List<String> evidenceRefIds;
  final List<String> strengthFindingIds;
  final List<String> improvementFindingIds;
  final List<String> upgradeExampleFindingIds;
}
