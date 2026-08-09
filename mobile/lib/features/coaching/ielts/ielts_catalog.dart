import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_answer_preparation.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_set_detail.dart';
import 'package:speakup/features/coaching/preparation/preparation_catalog_components.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class IeltsCatalog extends StatefulWidget {
  const IeltsCatalog({
    required this.controller,
    required this.scenes,
    required this.onSelectionPressed,
    required this.onRetry,
    super.key,
  });

  final IeltsPreparationController controller;
  final List<SceneDefinition> scenes;
  final void Function(
    SceneDefinition scene,
    PracticeMode mode,
    IeltsPracticeSelection selection,
  )
  onSelectionPressed;
  final Future<void> Function() onRetry;

  @override
  State<IeltsCatalog> createState() => _IeltsCatalogState();
}

class _IeltsCatalogState extends State<IeltsCatalog> {
  late final TextEditingController _search;
  String? _release;
  String? _part;
  String? _tag;

  @override
  void initState() {
    super.initState();
    _search = TextEditingController()..addListener(_refresh);
  }

  @override
  void dispose() {
    _search
      ..removeListener(_refresh)
      ..dispose();
    super.dispose();
  }

  void _refresh() => setState(() {});

  @override
  Widget build(BuildContext context) {
    final bank = widget.controller.questionBank;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (widget.controller.isLoading && bank == null)
          const Center(
            child: Padding(
              padding: EdgeInsets.all(36),
              child: CircularProgressIndicator(),
            ),
          )
        else if (bank == null)
          PreparationInlineFailure(
            message: widget.controller.errorMessage ?? '雅思口语题库暂时不可用。',
            retryKey: const Key('ielts-question-bank-retry'),
            onRetry: widget.onRetry,
          )
        else
          ..._catalog(bank),
      ],
    );
  }

  List<Widget> _catalog(IeltsQuestionBank bank) {
    final items = _filteredItems(bank);
    return [
      SizedBox(
        height: 44,
        child: TextField(
          key: const Key('ielts-browser-search'),
          controller: _search,
          textInputAction: TextInputAction.search,
          decoration: InputDecoration(
            hintText: '搜索中英文题目…',
            prefixIcon: const Icon(Icons.search_rounded),
            suffixIcon: _search.text.isEmpty
                ? null
                : IconButton(
                    onPressed: _search.clear,
                    icon: const Icon(Icons.close_rounded),
                  ),
            filled: true,
            fillColor: PreparationDesign.surfaceMuted,
            contentPadding: const EdgeInsets.symmetric(vertical: 8),
            border: OutlineInputBorder(
              borderRadius: BorderRadius.circular(
                PreparationDesign.radiusControl,
              ),
              borderSide: BorderSide.none,
            ),
            enabledBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(
                PreparationDesign.radiusControl,
              ),
              borderSide: BorderSide.none,
            ),
            focusedBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(
                PreparationDesign.radiusControl,
              ),
              borderSide: const BorderSide(color: PreparationDesign.ink),
            ),
          ),
        ),
      ),
      const SizedBox(height: 12),
      _FilterRow(
        semanticLabel: 'Part 筛选',
        allLabel: '全部',
        options: bank.filters.parts,
        selected: _part,
        onSelected: (value) => setState(() => _part = value),
      ),
      const SizedBox(height: 4),
      _FilterRow(
        key: const Key('ielts-release-filter'),
        semanticLabel: '题季筛选',
        allLabel: '全部',
        options: bank.filters.releases,
        selected: _release,
        onSelected: (value) => setState(() => _release = value),
      ),
      const SizedBox(height: 4),
      _FilterRow(
        key: const Key('ielts-tag-filter'),
        semanticLabel: '标签筛选',
        allLabel: '全部',
        options: [...bank.filters.cueCardTypes, ...bank.filters.topicTags],
        selected: _tag,
        onSelected: (value) => setState(() => _tag = value),
      ),
      const SizedBox(height: 12),
      Text(
        '${items.length} 道题',
        style: PreparationDesign.sectionTitle.copyWith(
          fontSize: 18,
          fontWeight: FontWeight.w700,
        ),
      ),
      const SizedBox(height: 10),
      if (items.isEmpty)
        const PreparationCatalogEmpty(message: '没有符合当前筛选条件的题目。')
      else
        GridView.builder(
          shrinkWrap: true,
          primary: false,
          itemCount: items.length,
          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 2,
            crossAxisSpacing: 10,
            mainAxisSpacing: 10,
            mainAxisExtent: 128,
          ),
          itemBuilder: (context, index) => _IeltsTopicCard(
            item: items[index],
            onPressed: () => unawaited(_open(items[index])),
          ),
        ),
    ];
  }

  List<_CatalogItem> _filteredItems(IeltsQuestionBank bank) {
    final query = _search.text.trim().toLowerCase();
    final items = <_CatalogItem>[];
    if (_part == null || _part == 'PART_1') {
      for (final topic in bank.part1Topics) {
        items.add(
          _CatalogItem(
            id: topic.id,
            mode: PracticeMode.part1,
            title: topic.titleZh,
            subtitle: topic.titleEn,
            release: topic.releaseStatus,
            tags: topic.tagCodes,
            imagePath: _topicImageFor(topic.tagCodes, topic.id),
            questions: topic.questions,
            searchable:
                '${topic.titleZh} ${topic.titleEn} ${topic.questions.join(' ')}',
          ),
        );
      }
    }
    for (final group in bank.topicGroups) {
      if (_part == null || _part == 'PART_2') {
        items.add(
          _CatalogItem(
            id: group.id,
            mode: PracticeMode.part2,
            title: group.title,
            subtitle: group.cueCard.prompt,
            release: group.releaseStatus,
            tags: [group.cueCardType, ...group.tagCodes],
            imagePath: _topicImageFor(group.tagCodes, '${group.id}-part2'),
            questions: group.part3Questions,
            cueCard: group.cueCard,
            searchable:
                '${group.title} ${group.cueCard.prompt} ${group.cueCard.points.join(' ')} ${group.part3Questions.join(' ')}',
          ),
        );
      }
      if (_part == null || _part == 'PART_3') {
        items.add(
          _CatalogItem(
            id: group.id,
            mode: PracticeMode.part3,
            title: group.title,
            subtitle: group.cueCard.prompt,
            release: group.releaseStatus,
            tags: [group.cueCardType, ...group.tagCodes],
            imagePath: _topicImageFor(group.tagCodes, '${group.id}-part3'),
            questions: group.part3Questions,
            searchable:
                '${group.title} ${group.cueCard.prompt} ${group.part3Questions.join(' ')}',
          ),
        );
      }
    }
    return items
        .where(
          (item) =>
              (_release == null || item.release == _release) &&
              (_tag == null || item.tags.contains(_tag)) &&
              (query.isEmpty || item.searchable.toLowerCase().contains(query)),
        )
        .toList(growable: false);
  }

  Future<void> _open(_CatalogItem item) async {
    final bank = widget.controller.questionBank;
    if (bank == null) {
      return;
    }
    final scene = ieltsSceneForMode(widget.scenes, item.mode);
    final selection = IeltsPracticeSelection(
      part1SetId: item.mode == PracticeMode.part1 ? item.id : null,
      topicGroupId: item.mode == PracticeMode.part1 ? null : item.id,
    );
    await Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => IeltsSetDetailPage(
          mode: item.mode,
          title: item.title,
          subtitle: item.subtitle,
          questions: item.questions,
          cueCard: item.cueCard,
          answerPreparationClient: widget.controller.answerPreparationClient,
          questionReferences: [
            for (var index = 0; index < item.questions.length; index++)
              IeltsAnswerQuestionReference(
                bankId: bank.bankId,
                part: item.mode == PracticeMode.part1 ? 'PART_1' : 'PART_3',
                sourceId: item.id,
                questionPosition: index + 1,
              ),
          ],
          onStart: scene == null
              ? null
              : () {
                  Navigator.of(context).pop();
                  widget.onSelectionPressed(scene, item.mode, selection);
                },
        ),
      ),
    );
  }
}

class IeltsFullMockButton extends StatelessWidget {
  const IeltsFullMockButton({required this.onPressed, super.key});

  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) => FilledButton.icon(
    key: const Key('ielts-mode-full'),
    onPressed: onPressed,
    icon: const Icon(Icons.timer_outlined, size: 18),
    label: const Text('模考'),
    style: FilledButton.styleFrom(
      minimumSize: const Size(88, 44),
      padding: const EdgeInsets.symmetric(horizontal: 13),
      backgroundColor: PreparationDesign.ink,
      foregroundColor: Colors.white,
      textStyle: PreparationDesign.label,
      shape: const StadiumBorder(),
    ),
  );
}

class _FilterRow extends StatelessWidget {
  const _FilterRow({
    required this.semanticLabel,
    required this.allLabel,
    required this.options,
    required this.selected,
    required this.onSelected,
    super.key,
  });

  final String semanticLabel;
  final String allLabel;
  final List<IeltsFilterOption> options;
  final String? selected;
  final ValueChanged<String?> onSelected;

  @override
  Widget build(BuildContext context) {
    final choices = SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          _FilterChoice(
            label: Text(allLabel),
            selected: selected == null,
            onSelected: (_) => onSelected(null),
          ),
          for (final option in options) ...[
            const SizedBox(width: 6),
            _FilterChoice(
              label: Text(option.label),
              selected: selected == option.code,
              onSelected: (_) => onSelected(option.code),
            ),
          ],
        ],
      ),
    );
    return Semantics(container: true, label: semanticLabel, child: choices);
  }
}

class _FilterChoice extends StatelessWidget {
  const _FilterChoice({
    required this.label,
    required this.selected,
    required this.onSelected,
  });

  final Widget label;
  final bool selected;
  final ValueChanged<bool> onSelected;

  @override
  Widget build(BuildContext context) => ChoiceChip(
    label: label,
    selected: selected,
    showCheckmark: false,
    selectedColor: PreparationDesign.ink,
    backgroundColor: PreparationDesign.surfaceMuted,
    side: BorderSide.none,
    labelStyle: PreparationDesign.label.copyWith(
      color: selected ? Colors.white : PreparationDesign.ink,
    ),
    onSelected: onSelected,
  );
}

class _IeltsTopicCard extends StatelessWidget {
  const _IeltsTopicCard({required this.item, required this.onPressed});

  final _CatalogItem item;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final hasImage = item.imagePath != null;
    return Material(
      color: PreparationDesign.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: Key('ielts-${item.mode.name}-set-${item.id}'),
        onTap: onPressed,
        child: Stack(
          fit: StackFit.expand,
          children: [
            if (hasImage)
              Image.asset(
                item.imagePath!,
                fit: BoxFit.cover,
                excludeFromSemantics: true,
              ),
            if (hasImage)
              const DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: [Colors.transparent, Color(0xD9000000)],
                    stops: [0.28, 1],
                  ),
                ),
              ),
            Padding(
              padding: const EdgeInsets.all(12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Spacer(),
                  Text(
                    item.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: PreparationDesign.cardTitle.copyWith(
                      color: hasImage ? Colors.white : PreparationDesign.ink,
                    ),
                  ),
                  const SizedBox(height: 3),
                  Text(
                    item.subtitle,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: PreparationDesign.meta.copyWith(
                      color: hasImage
                          ? Colors.white.withValues(alpha: 0.78)
                          : PreparationDesign.inkSecondary,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

final class _CatalogItem {
  const _CatalogItem({
    required this.id,
    required this.mode,
    required this.title,
    required this.subtitle,
    required this.release,
    required this.tags,
    required this.imagePath,
    required this.questions,
    required this.searchable,
    this.cueCard,
  });
  final String id;
  final PracticeMode mode;
  final String title;
  final String subtitle;
  final String release;
  final List<String> tags;
  final String? imagePath;
  final List<String> questions;
  final IeltsCueCard? cueCard;
  final String searchable;
}

const _topicImages = <String, List<String>>{
  'daily_life': [
    'assets/images/scenes/ielts-topic-daily-life.webp',
    'assets/images/scenes/ielts-topic-daily-life-2.webp',
    'assets/images/scenes/ielts-topic-daily-life-3.webp',
  ],
  'work_study': [
    'assets/images/scenes/ielts-topic-work-study.webp',
    'assets/images/scenes/ielts-topic-work-study-2.webp',
    'assets/images/scenes/ielts-topic-work-study-3.webp',
  ],
  'people_relationships': [
    'assets/images/scenes/ielts-topic-people-relationships.webp',
    'assets/images/scenes/ielts-topic-people-relationships-2.webp',
    'assets/images/scenes/ielts-topic-people-relationships-3.webp',
  ],
  'technology_media': [
    'assets/images/scenes/ielts-topic-technology-media.webp',
    'assets/images/scenes/ielts-topic-technology-media-2.webp',
    'assets/images/scenes/ielts-topic-technology-media-3.webp',
  ],
  'culture_entertainment': [
    'assets/images/scenes/ielts-topic-culture-entertainment.webp',
    'assets/images/scenes/ielts-topic-culture-entertainment-2.webp',
    'assets/images/scenes/ielts-topic-culture-entertainment-3.webp',
  ],
  'travel_places': [
    'assets/images/scenes/ielts-topic-travel-places.webp',
    'assets/images/scenes/ielts-topic-travel-places-2.webp',
    'assets/images/scenes/ielts-topic-travel-places-3.webp',
  ],
  'nature_environment': [
    'assets/images/scenes/ielts-topic-nature-environment.webp',
    'assets/images/scenes/ielts-topic-nature-environment-2.webp',
    'assets/images/scenes/ielts-topic-nature-environment-3.webp',
  ],
  'society_rules': [
    'assets/images/scenes/ielts-topic-society-rules.webp',
    'assets/images/scenes/ielts-topic-society-rules-2.webp',
    'assets/images/scenes/ielts-topic-society-rules-3.webp',
  ],
  'personal_growth': [
    'assets/images/scenes/ielts-topic-personal-growth.webp',
    'assets/images/scenes/ielts-topic-personal-growth-2.webp',
    'assets/images/scenes/ielts-topic-personal-growth-3.webp',
  ],
  'health_sports': [
    'assets/images/scenes/ielts-topic-health-sports.webp',
    'assets/images/scenes/ielts-topic-health-sports-2.webp',
    'assets/images/scenes/ielts-topic-health-sports-3.webp',
  ],
};

String? _topicImageFor(List<String> tags, String itemId) {
  final candidates = <String>[];
  for (final tag in tags) {
    final images = _topicImages[tag];
    if (images != null) candidates.addAll(images);
  }
  if (candidates.isEmpty) return null;
  final index = itemId.codeUnits.fold(0, (sum, codeUnit) => sum + codeUnit);
  return candidates[index % candidates.length];
}

SceneDefinition? ieltsSceneForMode(
  List<SceneDefinition> scenes,
  PracticeMode mode,
) => scenes
    .where(
      (scene) =>
          scene.experience == PracticeExperience.ieltsSpeaking &&
          scene.practiceOptions.any((option) => option.mode == mode),
    )
    .firstOrNull;
