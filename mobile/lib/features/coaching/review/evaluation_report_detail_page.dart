import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/review/evaluation_report_detail_content.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presenter.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';

class ReviewReportDetailPage extends StatelessWidget {
  const ReviewReportDetailPage({required this.item, super.key});

  final ReviewHistoryItem item;

  @override
  Widget build(BuildContext context) {
    final viewModel = presentEvaluationReportDetail(item.report);
    return Scaffold(
      key: const Key('evaluation-report-detail-page'),
      appBar: AppBar(
        title: Text(
          viewModel.pageTitle,
          key: const Key('evaluation-report-detail-title'),
        ),
        leading: IconButton(
          key: const Key('evaluation-report-detail-back'),
          tooltip: '返回复盘历史',
          onPressed: () => Navigator.of(context).maybePop(),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: SafeArea(
        top: false,
        child: ListView(
          key: const Key('evaluation-report-detail-scroll'),
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 48),
          children: <Widget>[
            EvaluationReportDetailContent(viewModel: viewModel),
          ],
        ),
      ),
    );
  }
}
