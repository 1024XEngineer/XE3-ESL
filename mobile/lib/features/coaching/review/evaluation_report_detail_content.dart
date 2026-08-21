import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/review/evaluation_report_components.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presenter.dart';

class EvaluationReportDetailContent extends StatelessWidget {
  const EvaluationReportDetailContent({required this.viewModel, super.key});

  final EvaluationReportViewModel viewModel;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('evaluation-report-detail-content'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        EvaluationReportOverviewCard(viewModel: viewModel),
        if (viewModel.insufficient) ...<Widget>[
          const SizedBox(height: 12),
          const EvaluationInsufficientNotice(),
        ],
        const SizedBox(height: 24),
        EvaluationDimensionSection(viewModel: viewModel),
        const SizedBox(height: 8),
        EvaluationQuestionAnswersDisclosure(questions: viewModel.questions),
      ],
    );
  }
}
