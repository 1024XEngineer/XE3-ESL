import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/ielts_evaluation_overview.dart';

/// IELTS report content follows the official four assessment criteria while
/// keeping every conclusion traceable to the frozen Practice evidence.
class IeltsReportDetailContent extends StatelessWidget {
  const IeltsReportDetailContent({required this.report, super.key});

  final EvaluationReport report;

  @override
  Widget build(BuildContext context) {
    final dimensions = <String, EvaluationReportDimension>{
      for (final dimension in report.dimensions) dimension.key: dimension,
    };
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _QuestionAnswers(questions: report.questions),
        const SizedBox(height: SpeakUpDesign.space12),
        IeltsEvaluationOverview(report: report),
        const SizedBox(height: SpeakUpDesign.space12),
        Text('每一项的依据与建议', style: SpeakUpDesign.sectionTitle),
        const SizedBox(height: SpeakUpDesign.space12),
        for (final key in _criterionKeys) ...[
          _CriterionDetail(
            key: Key('ielts-criterion-$key'),
            dimension: dimensions[key],
            label: _criterionLabels[key]!,
          ),
          if (key != _criterionKeys.last)
            const SizedBox(height: SpeakUpDesign.space12),
        ],
      ],
    );
  }
}

class _QuestionAnswers extends StatelessWidget {
  const _QuestionAnswers({required this.questions});

  final List<EvaluationReportQuestion> questions;

  @override
  Widget build(BuildContext context) {
    final ordered = [...questions]
      ..sort((left, right) => left.position.compareTo(right.position));
    return Card(
      key: const Key('ielts-report-questions'),
      elevation: 0,
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('题目与作答', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: SpeakUpDesign.space16),
            for (var index = 0; index < ordered.length; index++) ...[
              if (index > 0) const Divider(height: SpeakUpDesign.space32),
              Text(
                '${ordered[index].position}. ${ordered[index].text}',
                key: Key('ielts-report-question-${ordered[index].id}'),
                style: SpeakUpDesign.label,
              ),
              const SizedBox(height: SpeakUpDesign.space8),
              Text(
                ordered[index].answer?.transcript ?? '未作答',
                style: SpeakUpDesign.body.copyWith(
                  color: ordered[index].answer == null
                      ? SpeakUpDesign.secondary
                      : SpeakUpDesign.ink,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _CriterionDetail extends StatelessWidget {
  const _CriterionDetail({
    required this.dimension,
    required this.label,
    super.key,
  });

  final EvaluationReportDimension? dimension;
  final String label;

  @override
  Widget build(BuildContext context) {
    final value = dimension;
    final findings = value == null
        ? const <EvaluationReportFinding>[]
        : <EvaluationReportFinding>[...value.strengths, ...value.improvements];
    final suggestions = _suggestions(value);
    return Card(
      elevation: 0,
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(child: Text(label, style: SpeakUpDesign.cardTitle)),
                Text(
                  value?.score == null ? '未评分' : '${_score(value!.score!)} / 9',
                  style: SpeakUpDesign.cardTitle,
                ),
              ],
            ),
            if (value != null && value.coverage < 1) ...[
              const SizedBox(height: SpeakUpDesign.space4),
              Text(
                '证据覆盖 ${(value.coverage * 100).round()}%',
                style: SpeakUpDesign.meta,
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space16),
            Text('评分依据', style: SpeakUpDesign.label),
            const SizedBox(height: SpeakUpDesign.space8),
            if (findings.isEmpty)
              Text(_unavailableReason(value), style: SpeakUpDesign.body)
            else
              for (var index = 0; index < findings.length; index++) ...[
                if (index > 0) const SizedBox(height: SpeakUpDesign.space12),
                _FindingEvidence(finding: findings[index]),
              ],
            const Divider(height: SpeakUpDesign.space32),
            Text('改进建议', style: SpeakUpDesign.label),
            const SizedBox(height: SpeakUpDesign.space8),
            if (suggestions.isEmpty)
              Text('本项暂无额外建议。', style: SpeakUpDesign.body)
            else
              for (var index = 0; index < suggestions.length; index++) ...[
                if (index > 0) const SizedBox(height: SpeakUpDesign.space8),
                Text('• ${suggestions[index]}', style: SpeakUpDesign.body),
              ],
          ],
        ),
      ),
    );
  }
}

class _FindingEvidence extends StatelessWidget {
  const _FindingEvidence({required this.finding});

  final EvaluationReportFinding finding;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(finding.message, style: SpeakUpDesign.body),
      for (final evidence in finding.evidence) ...[
        const SizedBox(height: SpeakUpDesign.space4),
        Text('原句：“${evidence.originalExcerpt}”', style: SpeakUpDesign.meta),
      ],
    ],
  );
}

List<String> _suggestions(EvaluationReportDimension? dimension) {
  if (dimension == null) return const [];
  final seen = <String>{};
  final result = <String>[];
  for (final finding in <EvaluationReportFinding>[
    ...dimension.improvements,
    ...dimension.recommendedExamples,
  ]) {
    final suggestion = (finding.suggestion ?? finding.message).trim();
    if (suggestion.isNotEmpty && seen.add(suggestion)) result.add(suggestion);
  }
  return result;
}

String _unavailableReason(EvaluationReportDimension? dimension) {
  final reason = dimension?.reasonCodes.firstOrNull;
  return switch (reason) {
    'ACOUSTIC_ASSESSMENT_NOT_CONFIGURED' => '当前环境未启用发音评测。',
    'ACOUSTIC_ASSESSMENT_FAILED' => '本次发音评测失败，未使用文字推测发音。',
    'PRACTICE_TURN_AUDIO_UNAVAILABLE' => '本次录音不可用，无法形成发音依据。',
    'NO_EFFECTIVE_TURNS' => '本次没有足够的有效作答。',
    _ => '本项没有足够的评分依据。',
  };
}

String _score(double value) => value == value.roundToDouble()
    ? value.toInt().toString()
    : value.toStringAsFixed(1);

const _criterionKeys = <String>[
  'FLUENCY_COHERENCE',
  'LEXICAL_RESOURCE',
  'GRAMMATICAL_RANGE_ACCURACY',
  'PRONUNCIATION',
];

const _criterionLabels = <String, String>{
  'FLUENCY_COHERENCE': '流利性与连贯性',
  'LEXICAL_RESOURCE': '词汇资源',
  'GRAMMATICAL_RANGE_ACCURACY': '语法多样性及准确性',
  'PRONUNCIATION': '发音',
};
