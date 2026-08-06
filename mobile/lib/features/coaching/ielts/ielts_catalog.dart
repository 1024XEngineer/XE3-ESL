import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_catalog_components.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class IeltsCatalog extends StatelessWidget {
  const IeltsCatalog({
    required this.scenes,
    required this.onFullMockPressed,
    required this.onPartPressed,
    super.key,
  });

  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onFullMockPressed;
  final void Function(SceneDefinition scene, PracticeMode mode) onPartPressed;

  @override
  Widget build(BuildContext context) {
    final scene = scenes.firstOrNull;
    final modes =
        scene?.practiceOptions.map((option) => option.mode).toSet() ??
        const <PracticeMode>{};
    final hasFullMock = modes.contains(PracticeMode.fullMock);
    final partModes = const [
      PracticeMode.part1,
      PracticeMode.part2,
      PracticeMode.part3,
    ].where(modes.contains).toList(growable: false);
    if (scene == null) {
      return const PreparationCatalogEmpty(message: '当前没有可用的 IELTS 口语练习。');
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const PreparationCatalogHeader(
          title: 'IELTS 口语',
          description: '按真实考试顺序练 Part 1、Part 2、Part 3。',
          titleKey: Key('practice-hub-title-ielts'),
        ),
        const SizedBox(height: 20),
        if (hasFullMock) ...[
          PreparationFeaturedScene(
            key: const Key('ielts-mode-full'),
            eyebrow: '推荐',
            title: '一次完成三个 Part',
            description: '连续完成整套口语流程，中途不打断。',
            actionLabel: '开始完整模考',
            icon: Icons.timer_outlined,
            color: PreparationDesign.ielts,
            foregroundColor: Colors.white,
            assetPath: 'assets/images/scenes/ielts-hero.jpg',
            onPressed: () => onFullMockPressed(scene),
          ),
          const SizedBox(height: 28),
        ],
        if (partModes.isNotEmpty) ...[
          const Text('分段练习', style: PreparationDesign.sectionTitle),
          const SizedBox(height: 12),
          for (var index = 0; index < partModes.length; index++)
            _IeltsPartStep(
              scene: scene,
              mode: partModes[index],
              partNumber: _ieltsPartNumber(partModes[index]),
              label: _ieltsPartLabel(partModes[index]),
              isLast: index == partModes.length - 1,
              onPressed: () => onPartPressed(scene, partModes[index]),
            ),
        ],
      ],
    );
  }
}

class IeltsSetCatalog extends StatefulWidget {
  const IeltsSetCatalog({
    required this.controller,
    required this.mode,
    required this.scene,
    required this.onRetry,
    required this.onSelectionPressed,
    super.key,
  });

  final IeltsPreparationController controller;
  final PracticeMode mode;
  final SceneDefinition? scene;
  final Future<void> Function() onRetry;
  final void Function(SceneDefinition scene, IeltsPracticeSelection selection)
  onSelectionPressed;

  @override
  State<IeltsSetCatalog> createState() => _IeltsSetCatalogState();
}

class _IeltsSetCatalogState extends State<IeltsSetCatalog> {
  late final TextEditingController _searchController;

  @override
  void initState() {
    super.initState();
    _searchController = TextEditingController()..addListener(_onSearchChanged);
  }

  @override
  void dispose() {
    _searchController
      ..removeListener(_onSearchChanged)
      ..dispose();
    super.dispose();
  }

  void _onSearchChanged() => setState(() {});

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final mode = widget.mode;
    final bank = controller.questionBank;
    final title = switch (mode) {
      PracticeMode.part1 => 'Part 1 套题',
      PracticeMode.part2 => 'Part 2 题卡',
      PracticeMode.part3 => 'Part 3 主题讨论',
      PracticeMode.fullMock => '完整模考',
      PracticeMode.fullSimulation || PracticeMode.focus => throw StateError(
        'Non-IELTS mode in IELTS question list.',
      ),
    };
    if (controller.isLoading && bank == null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          PreparationCatalogHeader(
            title: title,
            description: '正在读取本季题库…',
            titleKey: Key('ielts-set-list-title-${mode.name}'),
          ),
          const SizedBox(height: 36),
          const Center(child: CircularProgressIndicator()),
        ],
      );
    }
    if (bank == null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          PreparationCatalogHeader(
            title: title,
            description: '暂时无法显示套题。',
            titleKey: Key('ielts-set-list-title-${mode.name}'),
          ),
          const SizedBox(height: 24),
          PreparationInlineFailure(
            message: controller.errorMessage ?? '雅思口语题库暂时不可用。',
            retryKey: const Key('ielts-question-bank-retry'),
            onRetry: widget.onRetry,
          ),
        ],
      );
    }
    final query = _searchController.text.trim().toLowerCase();
    final part1Topics = bank.part1Topics
        .where(
          (topic) =>
              query.isEmpty ||
              [
                topic.titleZh,
                topic.titleEn,
                ...topic.questions,
              ].join(' ').toLowerCase().contains(query),
        )
        .toList(growable: false);
    final topicGroups = bank.topicGroups
        .where(
          (group) =>
              query.isEmpty ||
              [
                group.title,
                group.cueCard.prompt,
                ...group.cueCard.points,
                ...group.part3Questions,
              ].join(' ').toLowerCase().contains(query),
        )
        .toList(growable: false);
    final total = mode == PracticeMode.part1
        ? bank.part1Topics.length
        : bank.topicGroups.length;
    final completed = mode == PracticeMode.part1
        ? bank.part1Topics
              .where(
                (set) =>
                    controller.progress(PracticeMode.part1, set.id).completed,
              )
              .length
        : bank.topicGroups
              .where((group) => controller.progress(mode, group.id).completed)
              .length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        PreparationCatalogHeader(
          title: title,
          description: switch (mode) {
            PracticeMode.part1 => '按一个熟悉话题完成对应问答。',
            PracticeMode.part2 => '完成题卡后，可以继续同主题 Part 3。',
            PracticeMode.part3 => '先看对应 Part 2 背景，再练绑定讨论题。',
            PracticeMode.fullMock => '按正式顺序完成三个 Part。',
            PracticeMode.fullSimulation || PracticeMode.focus =>
              throw StateError('Non-IELTS mode in IELTS question list.'),
          },
          titleKey: Key('ielts-set-list-title-${mode.name}'),
        ),
        const SizedBox(height: 14),
        Text(
          '已完成 $completed / $total 套',
          key: Key('ielts-set-list-progress-${mode.name}'),
          style: PreparationDesign.meta.copyWith(
            color: PreparationDesign.ielts,
            fontWeight: FontWeight.w800,
          ),
        ),
        const SizedBox(height: 18),
        TextField(
          key: const Key('ielts-browser-search'),
          controller: _searchController,
          textInputAction: TextInputAction.search,
          decoration: InputDecoration(
            hintText: '搜索中英文题目…',
            prefixIcon: const Icon(Icons.search_rounded),
            suffixIcon: _searchController.text.isEmpty
                ? null
                : IconButton(
                    tooltip: '清除搜索',
                    onPressed: _searchController.clear,
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
        const SizedBox(height: 18),
        if (widget.scene == null)
          const PreparationCatalogEmpty(message: '当前分段练习尚未开放。')
        else if (mode == PracticeMode.part1)
          for (final topic in part1Topics) ...[
            _IeltsSetCard(
              key: Key('ielts-part1-set-${topic.id}'),
              title: topic.titleZh,
              description: topic.titleEn,
              meta:
                  '${topic.questions.length} 道题 · ${_ieltsReleaseLabel(topic.release)}',
              progress: controller.progress(mode, topic.id),
              onPressed: () => widget.onSelectionPressed(
                widget.scene!,
                IeltsPracticeSelection(part1SetId: topic.id),
              ),
            ),
            const SizedBox(height: 10),
          ]
        else
          for (final group in topicGroups) ...[
            _IeltsSetCard(
              key: Key('ielts-${mode.name}-set-${group.id}'),
              title: mode == PracticeMode.part2
                  ? group.cueCard.prompt
                  : group.title,
              description: mode == PracticeMode.part2
                  ? '${_ieltsReleaseLabel(group.release)} · 可继续对应 Part 3'
                  : '对应 Part 2：${group.cueCard.prompt}',
              meta: mode == PracticeMode.part2
                  ? _pairedProgressLabel(controller, group.id)
                  : '${group.part3Questions.length} 道讨论题',
              progress: controller.progress(mode, group.id),
              showProgressStatus: mode != PracticeMode.part2,
              onPressed: () => widget.onSelectionPressed(
                widget.scene!,
                IeltsPracticeSelection(topicGroupId: group.id),
              ),
            ),
            const SizedBox(height: 10),
          ],
      ],
    );
  }
}

class _IeltsSetCard extends StatelessWidget {
  const _IeltsSetCard({
    required this.title,
    required this.description,
    required this.meta,
    required this.progress,
    required this.onPressed,
    this.showProgressStatus = true,
    super.key,
  });

  final String title;
  final String description;
  final String meta;
  final IeltsSetProgress progress;
  final VoidCallback onPressed;
  final bool showProgressStatus;

  @override
  Widget build(BuildContext context) {
    final status = _ieltsProgressLabel(progress);
    return Material(
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: InkWell(
        onTap: onPressed,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 15, 14, 15),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: PreparationDesign.cardTitle,
                    ),
                    const SizedBox(height: 6),
                    Text(
                      description,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: PreparationDesign.meta,
                    ),
                    const SizedBox(height: 9),
                    Wrap(
                      spacing: 8,
                      runSpacing: 5,
                      children: [
                        Text(
                          meta,
                          style: PreparationDesign.meta.copyWith(
                            color: PreparationDesign.inkSecondary,
                          ),
                        ),
                        if (showProgressStatus)
                          Text(
                            status,
                            style: PreparationDesign.meta.copyWith(
                              color: progress.completed || progress.inProgress
                                  ? PreparationDesign.ielts
                                  : PreparationDesign.inkSecondary,
                              fontWeight: FontWeight.w800,
                            ),
                          ),
                      ],
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              const Icon(Icons.arrow_forward_rounded, size: 22),
            ],
          ),
        ),
      ),
    );
  }
}

class _IeltsPartStep extends StatelessWidget {
  const _IeltsPartStep({
    required this.scene,
    required this.mode,
    required this.partNumber,
    required this.label,
    required this.isLast,
    required this.onPressed,
  });

  final SceneDefinition scene;
  final PracticeMode mode;
  final String partNumber;
  final String label;
  final bool isLast;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(bottom: isLast ? 0 : 10),
      child: Material(
        color: PreparationDesign.surface,
        clipBehavior: Clip.antiAlias,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
          side: const BorderSide(color: PreparationDesign.border),
        ),
        child: Semantics(
          button: true,
          label: 'Part $partNumber，$label。${scene.prompt.publicSceneBrief}',
          onTap: onPressed,
          excludeSemantics: true,
          child: InkWell(
            key: Key('catalog-scene-${scene.id}-${mode.name}'),
            onTap: onPressed,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(14, 13, 14, 13),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.center,
                children: [
                  Container(
                    width: 42,
                    height: 42,
                    alignment: Alignment.center,
                    decoration: const BoxDecoration(
                      color: PreparationDesign.ieltsTint,
                      shape: BoxShape.circle,
                    ),
                    child: Text(
                      partNumber,
                      style: const TextStyle(
                        color: PreparationDesign.ielts,
                        fontSize: 17,
                        fontWeight: FontWeight.w900,
                      ),
                    ),
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'Part $partNumber',
                          style: PreparationDesign.cardTitle,
                        ),
                        const SizedBox(height: 3),
                        Text(label, style: PreparationDesign.meta),
                      ],
                    ),
                  ),
                  const SizedBox(width: 10),
                  const Icon(
                    Icons.arrow_forward_rounded,
                    size: 20,
                    color: PreparationDesign.ink,
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

String _ieltsPartNumber(PracticeMode mode) {
  return switch (mode) {
    PracticeMode.part1 => '1',
    PracticeMode.part2 => '2',
    PracticeMode.part3 => '3',
    _ => '•',
  };
}

String _ieltsPartLabel(PracticeMode mode) {
  return switch (mode) {
    PracticeMode.part1 => '熟悉话题问答',
    PracticeMode.part2 => '题卡陈述 · 可继续 Part 3',
    PracticeMode.part3 => '承接 Part 2 主题讨论',
    _ => '专项练习',
  };
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

String _ieltsReleaseLabel(String release) {
  return switch (release) {
    'new' => '当季新题',
    'carry_over' => '老题沿用',
    _ => '本季题目',
  };
}

String _pairedProgressLabel(
  IeltsPreparationController controller,
  String groupId,
) {
  final part2 = controller.progress(PracticeMode.part2, groupId);
  final part3 = controller.progress(PracticeMode.part3, groupId);
  return '${_ieltsPartProgressLabel('Part 2', part2)}'
      ' · ${_ieltsPartProgressLabel('Part 3', part3)}';
}

String _ieltsPartProgressLabel(String part, IeltsSetProgress progress) {
  if (progress.inProgress) {
    return '$part 进行中';
  }
  if (progress.completed) {
    final date = progress.lastPracticedAt?.toLocal();
    final recent = date == null ? '' : ' · ${date.month}月${date.day}日';
    return '$part 已完成 ✓ · ${progress.attemptCount} 次$recent';
  }
  return '$part 未练习';
}

String _ieltsProgressLabel(IeltsSetProgress progress) {
  if (progress.inProgress) {
    final completed = progress.attemptCount == 0
        ? ''
        : ' · 已练 ${progress.attemptCount} 次';
    return '进行中$completed';
  }
  if (progress.completed) {
    final date = progress.lastPracticedAt?.toLocal();
    final recent = date == null ? '' : ' · 最近 ${date.month}月${date.day}日';
    return '已完成 ✓ · 练习 ${progress.attemptCount} 次$recent';
  }
  return '未练习';
}
