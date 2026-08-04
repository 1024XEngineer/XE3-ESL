import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';

enum IeltsSpeakingReportKind { fullMock, interview }

final class IeltsSpeakingReportIndexItem {
  const IeltsSpeakingReportIndexItem({
    required this.reportKind,
    required this.practiceSessionId,
    required this.evaluationId,
    required this.evaluationRevisionId,
    required this.revision,
    required this.evaluationStatus,
    required this.isFinal,
    required this.statusUrl,
    required this.createdAt,
    required this.updatedAt,
    this.title,
  });

  final IeltsSpeakingReportKind reportKind;
  final String practiceSessionId;
  final String evaluationId;
  final String evaluationRevisionId;
  final int revision;
  final IeltsSpeakingReportEvaluationStatus evaluationStatus;
  final bool isFinal;
  final String statusUrl;
  final DateTime createdAt;
  final DateTime updatedAt;
  final String? title;
}

final class IeltsSpeakingReportIndexPage {
  const IeltsSpeakingReportIndexPage({required this.items, this.nextCursor});

  final List<IeltsSpeakingReportIndexItem> items;
  final String? nextCursor;
}
