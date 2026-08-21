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
    if (report.practiceMode == 'PART_1') {
      return _Part1ReportContent(report: report, dimensions: dimensions);
    }
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

/// Part 1 redesigned layout: refined overview card followed by prioritized
/// dimension cards, then the Q&A disclosure.
class _Part1ReportContent extends StatelessWidget {
  const _Part1ReportContent({required this.report, required this.dimensions});

  final EvaluationReport report;
  final Map<String, EvaluationReportDimension> dimensions;

  @override
  Widget build(BuildContext context) {
    final answeredCount = report.questions
        .where((question) => question.answer != null)
        .length;
    final priorityFindingIds = <String, List<String>>{};
    final priorityKeys = <String>[];
    for (final action in report.priorityActions) {
      if (!dimensions.containsKey(action.dimensionKey) ||
          !_criterionLabels.containsKey(action.dimensionKey)) {
        continue;
      }
      final findingIds = priorityFindingIds.putIfAbsent(
        action.dimensionKey,
        () => <String>[],
      );
      findingIds.add(action.findingId);
      if (!priorityKeys.contains(action.dimensionKey)) {
        priorityKeys.add(action.dimensionKey);
      }
    }
    final remainingKeys =
        _criterionKeys
            .where((key) => !priorityFindingIds.containsKey(key))
            .toList()
          ..sort((left, right) {
            final leftScore = dimensions[left]?.score;
            final rightScore = dimensions[right]?.score;
            if (leftScore == null && rightScore == null) {
              return _criterionKeys
                  .indexOf(left)
                  .compareTo(_criterionKeys.indexOf(right));
            }
            if (leftScore == null) return 1;
            if (rightScore == null) return -1;
            final scoreOrder = leftScore.compareTo(rightScore);
            return scoreOrder != 0
                ? scoreOrder
                : _criterionKeys
                      .indexOf(left)
                      .compareTo(_criterionKeys.indexOf(right));
          });
    final sortedKeys = <String>[...priorityKeys, ...remainingKeys];
    return Column(
      key: const Key('ielts-part1-report-content'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        IeltsPart1DarkOverview(
          report: report,
          contextLabel: '基于本次 $answeredCount 道已记录回答的阶段性估分，不等同于官方考试成绩。',
        ),
        const SizedBox(height: SpeakUpDesign.space24),
        Row(
          children: [
            Text(
              '分项详情与建议',
              style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 17),
            ),
            const Spacer(),
            Text(
              priorityKeys.isEmpty ? '从薄弱项开始' : '优先项在前',
              style: SpeakUpDesign.meta.copyWith(
                color: SpeakUpDesign.secondary,
                fontSize: 12,
              ),
            ),
          ],
        ),
        const SizedBox(height: SpeakUpDesign.space12),
        for (final key in sortedKeys) ...[
          _DimensionCard(
            key: Key('ielts-part1-dimension-$key'),
            dimension: dimensions[key],
            label: _criterionLabels[key]!,
            priorityFindingIds: priorityFindingIds[key] ?? const <String>[],
          ),
          const SizedBox(height: SpeakUpDesign.space12),
        ],
        const SizedBox(height: SpeakUpDesign.space8),
        _QuestionAnswersDisclosure(questions: report.questions),
      ],
    );
  }
}

/// A clean, modern dimension card with score progress bar, tier pill badge,
/// original quote excerpt container, and actionable suggestion block.
class _DimensionCard extends StatefulWidget {
  const _DimensionCard({
    required this.dimension,
    required this.label,
    this.priorityFindingIds = const <String>[],
    super.key,
  });

  final EvaluationReportDimension? dimension;
  final String label;
  final List<String> priorityFindingIds;

  @override
  State<_DimensionCard> createState() => _DimensionCardState();
}

class _DimensionCardState extends State<_DimensionCard> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final value = widget.dimension;
    final score = value?.score;
    final allFindings = value == null
        ? const <EvaluationReportFinding>[]
        : <EvaluationReportFinding>[...value.strengths, ...value.improvements];
    final findingsById = <String, EvaluationReportFinding>{
      for (final finding in allFindings) finding.id: finding,
    };
    final prioritizedFindings = <EvaluationReportFinding>[
      for (final id in widget.priorityFindingIds)
        if (findingsById[id] != null) findingsById[id]!,
    ];
    final prioritizedIds = prioritizedFindings
        .map((finding) => finding.id)
        .toSet();
    final findings = <EvaluationReportFinding>[
      ...prioritizedFindings,
      ...allFindings.where((finding) => !prioritizedIds.contains(finding.id)),
    ];
    final suggestions = _suggestions(
      value,
      priorityFindingIds: widget.priorityFindingIds,
    );
    final firstFinding = findings.firstOrNull;
    final firstSuggestion = suggestions.firstOrNull;
    final hasMore = findings.length > 1 || suggestions.length > 1;

    return Container(
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      padding: const EdgeInsets.all(SpeakUpDesign.space16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Header: Label & Score Pill
          Row(
            children: [
              Expanded(
                child: Text(
                  widget.label,
                  style: SpeakUpDesign.cardTitle.copyWith(fontSize: 15),
                ),
              ),
              _ScorePillBadge(score: score),
            ],
          ),
          const SizedBox(height: SpeakUpDesign.space12),
          // Progress bar
          _ScoreProgressBar(score: score),
          if (value != null && value.coverage < 1) ...[
            const SizedBox(height: SpeakUpDesign.space4),
            Text(
              '证据覆盖 ${(value.coverage * 100).round()}%',
              style: SpeakUpDesign.meta.copyWith(fontSize: 11),
            ),
          ],
          // Key finding summary
          if (firstFinding != null) ...[
            const SizedBox(height: SpeakUpDesign.space12),
            _FindingSummary(finding: firstFinding),
          ],
          // Key suggestion
          if (firstSuggestion != null) ...[
            const SizedBox(height: SpeakUpDesign.space12),
            _SuggestionBox(suggestion: firstSuggestion),
          ],
          // Expanded content
          if (_expanded && hasMore) ...[
            const SizedBox(height: SpeakUpDesign.space12),
            for (final finding in findings.skip(1)) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              _FindingSummary(finding: finding),
            ],
            if (suggestions.length > 1) ...[
              const SizedBox(height: SpeakUpDesign.space12),
              for (final suggestion in suggestions.skip(1)) ...[
                Padding(
                  padding: const EdgeInsets.only(bottom: SpeakUpDesign.space8),
                  child: _SuggestionBox(suggestion: suggestion),
                ),
              ],
            ],
          ],
          // Expand/collapse toggle
          if (hasMore) ...[
            const SizedBox(height: SpeakUpDesign.space8),
            GestureDetector(
              behavior: HitTestBehavior.opaque,
              onTap: () => setState(() => _expanded = !_expanded),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 4),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
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
          // Show unavailable reason when no findings exist
          if (findings.isEmpty && firstSuggestion == null) ...[
            const SizedBox(height: SpeakUpDesign.space12),
            Text(
              _unavailableReason(value),
              style: SpeakUpDesign.body.copyWith(fontSize: 13),
            ),
          ],
        ],
      ),
    );
  }
}

class _ScorePillBadge extends StatelessWidget {
  const _ScorePillBadge({required this.score});

  final double? score;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
      decoration: BoxDecoration(
        color: _pillBgColor(score),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(
        score == null ? '未评分' : '${_score(score!)} / 9',
        style: TextStyle(
          fontSize: 13,
          fontWeight: FontWeight.w700,
          color: _pillTextColor(score),
        ),
      ),
    );
  }

  Color _pillBgColor(double? score) {
    if (score == null) return SpeakUpDesign.primaryMuted;
    if (score >= 7.0) return const Color(0xFFEAF5EE);
    if (score >= 5.5) return const Color(0xFFFFF6E6);
    return const Color(0xFFFDEEEB);
  }

  Color _pillTextColor(double? score) {
    if (score == null) return SpeakUpDesign.secondary;
    if (score >= 7.0) return const Color(0xFF1E6B37);
    if (score >= 5.5) return const Color(0xFFB25E00);
    return const Color(0xFFC0392B);
  }
}

class _ScoreProgressBar extends StatelessWidget {
  const _ScoreProgressBar({required this.score});

  final double? score;

  @override
  Widget build(BuildContext context) {
    final progress = score == null ? 0.0 : (score! / 9).clamp(0.0, 1.0);
    return ClipRRect(
      borderRadius: BorderRadius.circular(2),
      child: LinearProgressIndicator(
        value: progress,
        minHeight: 3.5,
        backgroundColor: const Color(0xFFEFEFEF),
        valueColor: AlwaysStoppedAnimation<Color>(_barColor(score)),
      ),
    );
  }

  Color _barColor(double? score) {
    if (score == null) return SpeakUpDesign.border;
    if (score >= 7.0) return const Color(0xFF285443);
    if (score >= 5.5) return const Color(0xFFC58000);
    return const Color(0xFF8A2D21);
  }
}

class _FindingSummary extends StatelessWidget {
  const _FindingSummary({required this.finding});

  final EvaluationReportFinding finding;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        finding.message,
        style: SpeakUpDesign.body.copyWith(
          color: SpeakUpDesign.ink,
          fontSize: 14,
          height: 1.45,
        ),
      ),
      for (final evidence in finding.evidence) ...[
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
            children: [
              Text(
                '作答：',
                style: SpeakUpDesign.meta.copyWith(
                  fontWeight: FontWeight.w600,
                  fontSize: 12,
                  color: SpeakUpDesign.secondary,
                ),
              ),
              Expanded(
                child: Text(
                  '“${evidence.originalExcerpt}”',
                  style: SpeakUpDesign.body.copyWith(
                    fontStyle: FontStyle.italic,
                    fontSize: 13,
                    color: SpeakUpDesign.ink,
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

class _SuggestionBox extends StatelessWidget {
  const _SuggestionBox({required this.suggestion});

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
        children: [
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

class _QuestionAnswersDisclosure extends StatelessWidget {
  const _QuestionAnswersDisclosure({required this.questions});

  final List<EvaluationReportQuestion> questions;

  @override
  Widget build(BuildContext context) => Card(
    key: const Key('ielts-report-questions-disclosure'),
    elevation: 0,
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      side: const BorderSide(color: SpeakUpDesign.border),
    ),
    color: SpeakUpDesign.surface,
    child: Theme(
      data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
      child: ExpansionTile(
        key: const Key('ielts-report-questions-toggle'),
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
        children: [_QuestionAnswers(questions: questions, embedded: true)],
      ),
    ),
  );
}

class _QuestionAnswers extends StatelessWidget {
  const _QuestionAnswers({required this.questions, this.embedded = false});

  final List<EvaluationReportQuestion> questions;
  final bool embedded;

  @override
  Widget build(BuildContext context) {
    final ordered = [...questions]
      ..sort((left, right) => left.position.compareTo(right.position));
    final content = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (!embedded) ...[
          Text('题目与作答', style: SpeakUpDesign.cardTitle),
          const SizedBox(height: SpeakUpDesign.space16),
        ],
        for (var index = 0; index < ordered.length; index++) ...[
          if (index > 0) const Divider(height: SpeakUpDesign.space24),
          Text(
            '${ordered[index].position}. ${ordered[index].text}',
            key: Key('ielts-report-question-${ordered[index].id}'),
            style: SpeakUpDesign.label.copyWith(fontSize: 13),
          ),
          const SizedBox(height: SpeakUpDesign.space8),
          Text(
            ordered[index].answer?.transcript ?? '未作答',
            style: SpeakUpDesign.body.copyWith(
              fontSize: 14,
              color: ordered[index].answer == null
                  ? SpeakUpDesign.secondary
                  : SpeakUpDesign.ink,
            ),
          ),
        ],
      ],
    );
    if (embedded) return content;
    return Card(
      key: const Key('ielts-report-questions'),
      elevation: 0,
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: content,
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
        Text('原句："${evidence.originalExcerpt}"', style: SpeakUpDesign.meta),
      ],
    ],
  );
}

List<String> _suggestions(
  EvaluationReportDimension? dimension, {
  List<String> priorityFindingIds = const <String>[],
}) {
  if (dimension == null) return const [];
  final improvementsById = <String, EvaluationReportFinding>{
    for (final finding in dimension.improvements) finding.id: finding,
  };
  final prioritizedIds = priorityFindingIds.toSet();
  final seen = <String>{};
  final result = <String>[];
  for (final finding in <EvaluationReportFinding>[
    for (final id in priorityFindingIds)
      if (improvementsById[id] != null) improvementsById[id]!,
    ...dimension.improvements.where(
      (finding) => !prioritizedIds.contains(finding.id),
    ),
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
