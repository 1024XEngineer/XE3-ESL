import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class IeltsSetDetailPage extends StatelessWidget {
  const IeltsSetDetailPage({
    required this.mode,
    required this.title,
    required this.subtitle,
    required this.questions,
    required this.onStart,
    this.cueCard,
    super.key,
  });

  final PracticeMode mode;
  final String title;
  final String subtitle;
  final List<String> questions;
  final IeltsCueCard? cueCard;
  final VoidCallback? onStart;

  String get _partLabel => switch (mode) {
    PracticeMode.part1 => 'Part 1',
    PracticeMode.part2 => 'Part 2',
    PracticeMode.part3 => 'Part 3',
    _ => throw StateError('IELTS set details require a section mode.'),
  };

  String get _questionsTitle => switch (mode) {
    PracticeMode.part2 => '同组 Part 3 · ${questions.length} 题',
    _ => '$_partLabel · ${questions.length} 题',
  };

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('ielts-set-detail'),
      backgroundColor: PreparationDesign.canvas,
      appBar: AppBar(
        backgroundColor: PreparationDesign.canvas,
        surfaceTintColor: Colors.transparent,
        leading: IconButton(
          key: const Key('ielts-set-detail-back'),
          tooltip: '返回题库',
          onPressed: () => Navigator.of(context).pop(),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        title: Text(
          'IELTS · $_partLabel',
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
      ),
      body: SafeArea(
        top: false,
        child: Column(
          children: [
            Expanded(
              child: SingleChildScrollView(
                key: const Key('ielts-set-detail-scroll'),
                padding: PreparationDesign.pagePadding(
                  context,
                  hasPrimaryNavigation: false,
                  top: 12,
                ).copyWith(bottom: 24),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (mode == PracticeMode.part1)
                      _PartOneHeader(
                        title: title,
                        subtitle: subtitle,
                        questionCount: questions.length,
                      )
                    else ...[
                      _PartBadge(label: _partLabel),
                      const SizedBox(height: 14),
                      Text(
                        title,
                        key: const Key('ielts-set-detail-title'),
                        style: PreparationDesign.pageTitle,
                      ),
                      if (cueCard == null && subtitle.isNotEmpty) ...[
                        const SizedBox(height: 8),
                        Text(
                          subtitle,
                          key: const Key('ielts-set-detail-subtitle'),
                          style: PreparationDesign.body,
                        ),
                      ],
                      const SizedBox(height: 28),
                    ],
                    if (cueCard case final value?) ...[
                      _CueCardContent(cueCard: value),
                      if (questions.isNotEmpty) const SizedBox(height: 30),
                    ],
                    if (questions.isNotEmpty) ...[
                      if (mode != PracticeMode.part1) ...[
                        Text(
                          _questionsTitle,
                          style: PreparationDesign.sectionTitle,
                        ),
                        const SizedBox(height: 8),
                      ],
                      for (var index = 0; index < questions.length; index++)
                        _QuestionRow(
                          index: index + 1,
                          question: questions[index],
                        ),
                    ],
                  ],
                ),
              ),
            ),
            DecoratedBox(
              decoration: const BoxDecoration(
                color: PreparationDesign.surface,
                border: Border(
                  top: BorderSide(color: PreparationDesign.border),
                ),
              ),
              child: Padding(
                padding: EdgeInsets.fromLTRB(
                  PreparationDesign.horizontalInset(context),
                  12,
                  PreparationDesign.horizontalInset(context),
                  12 + MediaQuery.viewPaddingOf(context).bottom,
                ),
                child: FilledButton(
                  key: const Key('ielts-set-detail-start'),
                  onPressed: onStart,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(52),
                    backgroundColor: PreparationDesign.ink,
                    foregroundColor: Colors.white,
                    textStyle: PreparationDesign.cardTitle,
                  ),
                  child: Text(
                    onStart == null
                        ? '练习暂不可用'
                        : mode == PracticeMode.part1
                        ? '开始整组练习'
                        : '开始 $_partLabel',
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PartOneHeader extends StatelessWidget {
  const _PartOneHeader({
    required this.title,
    required this.subtitle,
    required this.questionCount,
  });

  final String title;
  final String subtitle;
  final int questionCount;

  @override
  Widget build(BuildContext context) => Column(
    key: const Key('ielts-set-detail-topic'),
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Wrap(
        crossAxisAlignment: WrapCrossAlignment.end,
        spacing: 14,
        runSpacing: 6,
        children: [
          Text(
            title,
            key: const Key('ielts-set-detail-title'),
            style: PreparationDesign.pageTitle,
          ),
          if (subtitle.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(
                subtitle,
                key: const Key('ielts-set-detail-subtitle'),
                style: PreparationDesign.sectionTitle.copyWith(
                  color: PreparationDesign.inkSecondary,
                  fontWeight: FontWeight.w500,
                ),
              ),
            ),
        ],
      ),
      const SizedBox(height: 8),
      Text(
        '$questionCount 题',
        style: PreparationDesign.body.copyWith(
          color: PreparationDesign.inkSecondary,
        ),
      ),
      const SizedBox(height: 20),
      const Divider(height: 1, color: PreparationDesign.border),
      const SizedBox(height: 8),
    ],
  );
}

class _PartBadge extends StatelessWidget {
  const _PartBadge({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) => DecoratedBox(
    decoration: BoxDecoration(
      color: PreparationDesign.surfaceMuted,
      borderRadius: BorderRadius.circular(PreparationDesign.radiusControl),
    ),
    child: Padding(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      child: Text(label.toUpperCase(), style: PreparationDesign.label),
    ),
  );
}

class _CueCardContent extends StatelessWidget {
  const _CueCardContent({required this.cueCard});

  final IeltsCueCard cueCard;

  @override
  Widget build(BuildContext context) => Column(
    key: const Key('ielts-set-detail-cue-card'),
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const Text('Cue Card', style: PreparationDesign.sectionTitle),
      const SizedBox(height: 10),
      Text(
        cueCard.prompt,
        style: PreparationDesign.cardTitle.copyWith(fontSize: 18),
      ),
      if (cueCard.points.isNotEmpty) ...[
        const SizedBox(height: 14),
        const Text('You should say:', style: PreparationDesign.label),
        const SizedBox(height: 6),
        for (final point in cueCard.points)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Padding(
                  padding: EdgeInsets.only(top: 7),
                  child: Icon(Icons.circle, size: 5),
                ),
                const SizedBox(width: 10),
                Expanded(child: Text(point, style: PreparationDesign.body)),
              ],
            ),
          ),
      ],
    ],
  );
}

class _QuestionRow extends StatelessWidget {
  const _QuestionRow({required this.index, required this.question});

  final int index;
  final String question;

  @override
  Widget build(BuildContext context) => Container(
    key: Key('ielts-set-detail-question-$index'),
    width: double.infinity,
    padding: const EdgeInsets.symmetric(vertical: 12),
    decoration: const BoxDecoration(
      border: Border(bottom: BorderSide(color: PreparationDesign.border)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        SizedBox(
          width: 28,
          child: Text(
            '$index',
            style: PreparationDesign.label.copyWith(
              color: PreparationDesign.inkSecondary,
            ),
          ),
        ),
        Expanded(
          child: Text(
            question,
            style: PreparationDesign.body.copyWith(
              color: PreparationDesign.ink,
            ),
          ),
        ),
      ],
    ),
  );
}
