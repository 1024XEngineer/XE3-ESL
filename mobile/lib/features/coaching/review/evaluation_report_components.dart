import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presenter.dart';
import 'package:speakup/features/coaching/review/evaluation_report_radar.dart';

class EvaluationReportOverviewCard extends StatelessWidget {
  const EvaluationReportOverviewCard({required this.viewModel, super.key});

  final EvaluationReportViewModel viewModel;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('evaluation-report-overview'),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
        boxShadow: <BoxShadow>[
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.03),
            blurRadius: 12,
            offset: const Offset(0, 4),
          ),
        ],
      ),
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          _OverviewHeader(viewModel: viewModel),
          const SizedBox(height: 8),
          const Divider(height: 16, color: SpeakUpDesign.border),
          EvaluationRadarChart(
            axes: viewModel.radarAxes,
            rootKey: const Key('evaluation-report-radar'),
          ),
        ],
      ),
    );
  }
}

class _OverviewHeader extends StatelessWidget {
  const _OverviewHeader({required this.viewModel});

  final EvaluationReportViewModel viewModel;

  @override
  Widget build(BuildContext context) {
    final description = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Wrap(
          crossAxisAlignment: WrapCrossAlignment.center,
          spacing: 8,
          runSpacing: 4,
          children: <Widget>[
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
              decoration: BoxDecoration(
                color: SpeakUpDesign.ink,
                borderRadius: BorderRadius.circular(6),
              ),
              child: Text(
                viewModel.badgeLabel,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 11,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 0.2,
                ),
              ),
            ),
            Text(
              '${viewModel.radarAxes.length}维表现',
              style: SpeakUpDesign.label.copyWith(
                color: SpeakUpDesign.secondary,
                fontSize: 13,
              ),
            ),
          ],
        ),
        const SizedBox(height: 6),
        Text(
          viewModel.contextLabel,
          style: SpeakUpDesign.meta.copyWith(
            color: SpeakUpDesign.secondary,
            fontSize: 12,
            height: 1.35,
          ),
        ),
      ],
    );
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth < 300) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              description,
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerRight,
                child: _OverallScore(viewModel: viewModel),
              ),
            ],
          );
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Expanded(child: description),
            const SizedBox(width: 12),
            _OverallScore(viewModel: viewModel),
          ],
        );
      },
    );
  }
}

class _OverallScore extends StatelessWidget {
  const _OverallScore({required this.viewModel});

  final EvaluationReportViewModel viewModel;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minWidth: 92),
      padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: SpeakUpDesign.border, width: 0.8),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            '练习估分',
            style: SpeakUpDesign.meta.copyWith(
              color: SpeakUpDesign.secondary,
              fontSize: 10,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 1),
          FittedBox(
            fit: BoxFit.scaleDown,
            child: Row(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.baseline,
              textBaseline: TextBaseline.alphabetic,
              children: <Widget>[
                Text(
                  viewModel.overallScoreLabel,
                  key: const Key('evaluation-report-overall-score'),
                  style: const TextStyle(
                    color: SpeakUpDesign.ink,
                    fontSize: 28,
                    fontWeight: FontWeight.w800,
                    height: 1,
                    letterSpacing: -0.5,
                  ),
                ),
                const SizedBox(width: 3),
                Text(
                  viewModel.scaleSuffix,
                  style: const TextStyle(
                    color: SpeakUpDesign.tertiary,
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class EvaluationInsufficientNotice extends StatelessWidget {
  const EvaluationInsufficientNotice({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('evaluation-report-insufficient-notice'),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      padding: const EdgeInsets.all(SpeakUpDesign.space16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text('本次暂不评分', style: SpeakUpDesign.cardTitle),
          const SizedBox(height: 6),
          Text('本次有效证据不足，暂不形成能力结论。', style: SpeakUpDesign.body),
        ],
      ),
    );
  }
}

class EvaluationDimensionSection extends StatelessWidget {
  const EvaluationDimensionSection({required this.viewModel, super.key});

  final EvaluationReportViewModel viewModel;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('evaluation-report-dimensions'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Text(
                '分项详情与建议',
                style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 17),
              ),
            ),
            Text(
              viewModel.hasPriorityDimensions ? '优先项在前' : '从薄弱项开始',
              style: SpeakUpDesign.meta.copyWith(
                color: SpeakUpDesign.secondary,
                fontSize: 12,
              ),
            ),
          ],
        ),
        const SizedBox(height: SpeakUpDesign.space12),
        for (final dimension in viewModel.dimensions) ...<Widget>[
          EvaluationDimensionCard(
            key: Key('evaluation-report-dimension-${dimension.key}'),
            viewModel: dimension,
          ),
          const SizedBox(height: SpeakUpDesign.space12),
        ],
      ],
    );
  }
}

class EvaluationDimensionCard extends StatefulWidget {
  const EvaluationDimensionCard({required this.viewModel, super.key});

  final EvaluationDimensionViewModel viewModel;

  @override
  State<EvaluationDimensionCard> createState() =>
      _EvaluationDimensionCardState();
}

class _EvaluationDimensionCardState extends State<EvaluationDimensionCard> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final viewModel = widget.viewModel;
    final firstFinding = viewModel.findings.firstOrNull;
    final firstSuggestion = viewModel.suggestions.firstOrNull;
    final hasMore =
        viewModel.findings.length > 1 || viewModel.suggestions.length > 1;
    final coverage = viewModel.coverage;
    return Container(
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      padding: const EdgeInsets.all(SpeakUpDesign.space16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Text(
                  viewModel.label,
                  style: SpeakUpDesign.cardTitle.copyWith(fontSize: 15),
                ),
              ),
              const SizedBox(width: 8),
              EvaluationScoreBadge(viewModel: viewModel),
            ],
          ),
          const SizedBox(height: SpeakUpDesign.space12),
          EvaluationScoreProgressBar(
            normalizedScore: viewModel.normalizedScore,
          ),
          if (coverage != null && coverage < 1) ...<Widget>[
            const SizedBox(height: SpeakUpDesign.space4),
            Text(
              '证据覆盖 ${(coverage * 100).round()}%',
              style: SpeakUpDesign.meta.copyWith(fontSize: 11),
            ),
          ],
          if (firstFinding != null) ...<Widget>[
            const SizedBox(height: SpeakUpDesign.space12),
            EvaluationFindingSummary(finding: firstFinding),
          ],
          if (firstSuggestion != null) ...<Widget>[
            const SizedBox(height: SpeakUpDesign.space12),
            EvaluationSuggestionBox(suggestion: firstSuggestion),
          ],
          if (_expanded && hasMore) ...<Widget>[
            for (final finding in viewModel.findings.skip(1)) ...<Widget>[
              const SizedBox(height: SpeakUpDesign.space12),
              EvaluationFindingSummary(finding: finding),
            ],
            for (final suggestion in viewModel.suggestions.skip(1)) ...<Widget>[
              const SizedBox(height: SpeakUpDesign.space12),
              EvaluationSuggestionBox(suggestion: suggestion),
            ],
          ],
          if (hasMore) ...<Widget>[
            const SizedBox(height: SpeakUpDesign.space8),
            InkWell(
              key: Key('evaluation-report-dimension-more-${viewModel.key}'),
              borderRadius: BorderRadius.circular(8),
              onTap: () => setState(() => _expanded = !_expanded),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: <Widget>[
                    Text(
                      _expanded ? '收起详情' : '查看更多依据与建议',
                      style: SpeakUpDesign.meta.copyWith(
                        color: SpeakUpDesign.secondary,
                        fontWeight: FontWeight.w600,
                        fontSize: 12,
                      ),
                    ),
                    const SizedBox(width: 4),
                    Icon(
                      _expanded
                          ? Icons.keyboard_arrow_up_rounded
                          : Icons.keyboard_arrow_down_rounded,
                      size: 16,
                      color: SpeakUpDesign.secondary,
                    ),
                  ],
                ),
              ),
            ),
          ],
          if (firstFinding == null && firstSuggestion == null) ...<Widget>[
            const SizedBox(height: SpeakUpDesign.space12),
            Text(
              viewModel.unavailableReason,
              style: SpeakUpDesign.body.copyWith(fontSize: 13),
            ),
          ],
        ],
      ),
    );
  }
}

class EvaluationScoreBadge extends StatelessWidget {
  const EvaluationScoreBadge({required this.viewModel, super.key});

  final EvaluationDimensionViewModel viewModel;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
      decoration: BoxDecoration(
        color: evaluationScoreBackground(viewModel.normalizedScore),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(
        viewModel.scoreLabel,
        style: TextStyle(
          color: evaluationScoreForeground(viewModel.normalizedScore),
          fontSize: 13,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class EvaluationScoreProgressBar extends StatelessWidget {
  const EvaluationScoreProgressBar({required this.normalizedScore, super.key});

  final double? normalizedScore;

  @override
  Widget build(BuildContext context) {
    return ClipRRect(
      borderRadius: BorderRadius.circular(2),
      child: LinearProgressIndicator(
        value: normalizedScore ?? 0,
        minHeight: 3.5,
        backgroundColor: const Color(0xFFEFEFEF),
        valueColor: AlwaysStoppedAnimation<Color>(
          evaluationScoreBarColor(normalizedScore),
        ),
      ),
    );
  }
}

class EvaluationFindingSummary extends StatelessWidget {
  const EvaluationFindingSummary({required this.finding, super.key});

  final EvaluationFindingViewModel finding;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(
          finding.message,
          style: SpeakUpDesign.body.copyWith(
            color: SpeakUpDesign.ink,
            fontSize: 14,
            height: 1.45,
          ),
        ),
        for (final excerpt in finding.evidenceExcerpts) ...<Widget>[
          const SizedBox(height: SpeakUpDesign.space8),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            decoration: BoxDecoration(
              color: const Color(0xFFF7F7F8),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: const Color(0xFFEBEBEB), width: 0.8),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  '作答：',
                  style: SpeakUpDesign.meta.copyWith(
                    color: SpeakUpDesign.secondary,
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                Expanded(
                  child: Text(
                    '“$excerpt”',
                    style: SpeakUpDesign.body.copyWith(
                      color: SpeakUpDesign.ink,
                      fontSize: 13,
                      fontStyle: FontStyle.italic,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ],
    );
  }
}

class EvaluationSuggestionBox extends StatelessWidget {
  const EvaluationSuggestionBox({required this.suggestion, super.key});

  final String suggestion;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: const Color(0xFFFAF9F6),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: const Color(0xFFEFECE6), width: 0.8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const Text('💡', style: TextStyle(fontSize: 13)),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              suggestion,
              style: SpeakUpDesign.body.copyWith(
                color: const Color(0xFF333333),
                fontSize: 13,
                height: 1.4,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class EvaluationQuestionAnswersDisclosure extends StatelessWidget {
  const EvaluationQuestionAnswersDisclosure({
    required this.questions,
    super.key,
  });

  final List<EvaluationQuestionViewModel> questions;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('evaluation-report-questions-disclosure'),
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        side: const BorderSide(color: SpeakUpDesign.border),
      ),
      color: SpeakUpDesign.surface,
      child: Theme(
        data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          key: const Key('evaluation-report-questions-toggle'),
          tilePadding: const EdgeInsets.symmetric(
            horizontal: SpeakUpDesign.space16,
            vertical: SpeakUpDesign.space4,
          ),
          childrenPadding: const EdgeInsets.only(
            left: SpeakUpDesign.space16,
            right: SpeakUpDesign.space16,
            bottom: SpeakUpDesign.space16,
          ),
          title: Text(
            '题目与作答（${questions.length}）',
            style: SpeakUpDesign.cardTitle.copyWith(fontSize: 15),
          ),
          subtitle: Text(
            '查看本次完整转写',
            style: SpeakUpDesign.meta.copyWith(fontSize: 12),
          ),
          children: <Widget>[_QuestionAnswers(questions: questions)],
        ),
      ),
    );
  }
}

class _QuestionAnswers extends StatelessWidget {
  const _QuestionAnswers({required this.questions});

  final List<EvaluationQuestionViewModel> questions;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        for (var index = 0; index < questions.length; index++) ...<Widget>[
          if (index > 0) const Divider(height: SpeakUpDesign.space24),
          Text(
            '${questions[index].position}. ${questions[index].text}',
            key: Key('evaluation-report-question-${questions[index].id}'),
            style: SpeakUpDesign.label.copyWith(fontSize: 13),
          ),
          const SizedBox(height: SpeakUpDesign.space8),
          Text(
            questions[index].answerText,
            style: SpeakUpDesign.body.copyWith(
              color: questions[index].answered
                  ? SpeakUpDesign.ink
                  : SpeakUpDesign.secondary,
              fontSize: 14,
            ),
          ),
        ],
      ],
    );
  }
}
