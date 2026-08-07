import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_catalog_components.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class IeltsCatalog extends StatefulWidget {
  const IeltsCatalog({
    required this.controller,
    required this.scenes,
    required this.onFullMockPressed,
    required this.onSelectionPressed,
    required this.onRetry,
    super.key,
  });

  final IeltsPreparationController controller;
  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onFullMockPressed;
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
    final fullMockScene = ieltsSceneForMode(
      widget.scenes,
      PracticeMode.fullMock,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        PreparationCatalogHeader(
          title: '专项练习',
          description: bank == null
              ? '按题季、Part 和主题分类选择题目。'
              : '${bank.seasonLabel} · 按 Part 和主题分类选择题目。',
          titleKey: const Key('practice-hub-title-ielts'),
        ),
        const SizedBox(height: 20),
        if (fullMockScene != null) ...[
          PreparationFeaturedScene(
            key: const Key('ielts-mode-full'),
            eyebrow: '完整模拟',
            title: '快速开始整轮模考',
            description: '服务端按本季题库组装 Part 1、2、3。',
            actionLabel: '开始模考',
            icon: Icons.timer_outlined,
            color: PreparationDesign.ielts,
            foregroundColor: Colors.white,
            assetPath: 'assets/images/scenes/ielts-hero.jpg',
            onPressed: () => widget.onFullMockPressed(fullMockScene),
          ),
          const SizedBox(height: 22),
        ],
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
      TextField(
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
          fillColor: PreparationDesign.surface,
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
            borderSide: const BorderSide(color: PreparationDesign.border),
          ),
        ),
      ),
      const SizedBox(height: 14),
      _FilterRow(
        allLabel: '全部题季',
        options: bank.filters.releases,
        selected: _release,
        onSelected: (value) => setState(() => _release = value),
      ),
      const SizedBox(height: 8),
      _FilterRow(
        allLabel: '全部 Part',
        options: bank.filters.parts,
        selected: _part,
        onSelected: (value) => setState(() => _part = value),
      ),
      const SizedBox(height: 8),
      _FilterRow(
        allLabel: '全部标签',
        options: [...bank.filters.cueCardTypes, ...bank.filters.topicTags],
        selected: _tag,
        onSelected: (value) => setState(() => _tag = value),
      ),
      const SizedBox(height: 20),
      Text('共 ${items.length} 道专项题', style: PreparationDesign.sectionTitle),
      const SizedBox(height: 12),
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
            childAspectRatio: 1.08,
          ),
          itemBuilder: (context, index) => _IeltsTopicCard(
            item: items[index],
            onPressed: () => _open(items[index]),
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
            partLabel: 'PART 1',
            title: topic.titleZh,
            subtitle: topic.titleEn,
            release: topic.releaseStatus,
            tags: topic.tagCodes,
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
            partLabel: 'PART 2',
            title: group.title,
            subtitle: '${group.cueCard.prompt} · 完成后继续同组 Part 3',
            release: group.releaseStatus,
            tags: [group.cueCardType, ...group.tagCodes],
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
            partLabel: 'PART 3',
            title: group.title,
            subtitle: '对应 Part 2：${group.cueCard.prompt}',
            release: group.releaseStatus,
            tags: [group.cueCardType, ...group.tagCodes],
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

  void _open(_CatalogItem item) {
    final scene = ieltsSceneForMode(widget.scenes, item.mode);
    if (scene == null) return;
    widget.onSelectionPressed(
      scene,
      item.mode,
      IeltsPracticeSelection(
        part1SetId: item.mode == PracticeMode.part1 ? item.id : null,
        topicGroupId: item.mode == PracticeMode.part1 ? null : item.id,
      ),
    );
  }
}

class _FilterRow extends StatelessWidget {
  const _FilterRow({
    required this.allLabel,
    required this.options,
    required this.selected,
    required this.onSelected,
  });

  final String allLabel;
  final List<IeltsFilterOption> options;
  final String? selected;
  final ValueChanged<String?> onSelected;

  @override
  Widget build(BuildContext context) => SingleChildScrollView(
    scrollDirection: Axis.horizontal,
    child: Row(
      children: [
        ChoiceChip(
          label: Text(allLabel),
          selected: selected == null,
          onSelected: (_) => onSelected(null),
        ),
        for (final option in options) ...[
          const SizedBox(width: 8),
          ChoiceChip(
            label: Text(option.label),
            selected: selected == option.code,
            onSelected: (_) => onSelected(option.code),
          ),
        ],
      ],
    ),
  );
}

class _IeltsTopicCard extends StatelessWidget {
  const _IeltsTopicCard({required this.item, required this.onPressed});

  final _CatalogItem item;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) => Material(
    color: PreparationDesign.surface,
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
      side: const BorderSide(color: PreparationDesign.border),
    ),
    clipBehavior: Clip.antiAlias,
    child: InkWell(
      key: Key('ielts-${item.mode.name}-set-${item.id}'),
      onTap: onPressed,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            DecoratedBox(
              decoration: BoxDecoration(
                color: PreparationDesign.ieltsTint,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                child: Text(
                  item.partLabel,
                  style: PreparationDesign.meta.copyWith(
                    color: PreparationDesign.ielts,
                    fontWeight: FontWeight.w900,
                  ),
                ),
              ),
            ),
            const Spacer(),
            Text(
              item.title,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: PreparationDesign.cardTitle,
            ),
            const SizedBox(height: 5),
            Text(
              item.subtitle,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: PreparationDesign.meta,
            ),
          ],
        ),
      ),
    ),
  );
}

final class _CatalogItem {
  const _CatalogItem({
    required this.id,
    required this.mode,
    required this.partLabel,
    required this.title,
    required this.subtitle,
    required this.release,
    required this.tags,
    required this.searchable,
  });
  final String id;
  final PracticeMode mode;
  final String partLabel;
  final String title;
  final String subtitle;
  final String release;
  final List<String> tags;
  final String searchable;
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
