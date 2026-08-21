import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presenter.dart';
import 'package:speakup/features/coaching/review/evaluation_report_radar.dart';

class IeltsEvaluationOverview extends StatelessWidget {
  const IeltsEvaluationOverview({
    required this.report,
    this.title = 'IELTS 四维表现',
    this.scoreTitle = '练习估分',
    this.contextLabel,
    super.key,
  });

  final EvaluationReport report;
  final String title;
  final String scoreTitle;
  final String? contextLabel;

  @override
  Widget build(BuildContext context) {
    final viewModel = presentEvaluationReportDetail(report);
    return Card(
      key: const Key('ielts-evaluation-overview'),
      elevation: 0,
      color: SpeakUpDesign.surface,
      surfaceTintColor: Colors.transparent,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 20, 20, 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: <Widget>[
                Expanded(child: Text(title, style: SpeakUpDesign.cardTitle)),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: <Widget>[
                    Text(scoreTitle, style: SpeakUpDesign.meta),
                    const SizedBox(height: 2),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.baseline,
                      textBaseline: TextBaseline.alphabetic,
                      children: <Widget>[
                        Text(
                          viewModel.overallScoreLabel,
                          key: const Key('evaluation-profile-overall-score'),
                          style: SpeakUpDesign.pageTitle.copyWith(fontSize: 32),
                        ),
                        const SizedBox(width: 3),
                        Text(
                          viewModel.scaleSuffix,
                          style: SpeakUpDesign.meta.copyWith(fontSize: 12),
                        ),
                      ],
                    ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: SpeakUpDesign.space8),
            Text(
              contextLabel ?? '四项等权平均，并取最近的 0.5 分。',
              style: SpeakUpDesign.meta,
            ),
            const SizedBox(height: SpeakUpDesign.space12),
            EvaluationRadarChart(
              axes: viewModel.radarAxes,
              rootKey: const Key('evaluation-profile-radar'),
            ),
          ],
        ),
      ),
    );
  }
}
