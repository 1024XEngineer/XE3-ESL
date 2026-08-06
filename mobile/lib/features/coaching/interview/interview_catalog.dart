import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/preparation/preparation_catalog_components.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class InterviewCatalog extends StatelessWidget {
  const InterviewCatalog({
    required this.scenes,
    required this.onScenePressed,
    required this.onOpenJobPreparation,
    super.key,
  });

  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onScenePressed;
  final VoidCallback? onOpenJobPreparation;

  @override
  Widget build(BuildContext context) {
    final fullScene =
        scenes
            .where(
              (scene) => scene.category == SceneCategory.interviewProfessional,
            )
            .firstOrNull ??
        scenes.firstOrNull;
    final modes =
        <
              ({
                String id,
                String title,
                String caption,
                IconData icon,
                List<SceneDefinition> scenes,
              })
            >[
              (
                id: 'hr',
                title: '招聘初筛',
                caption: '自我介绍与求职动机',
                icon: Icons.badge_outlined,
                scenes: _scenesByCategory(
                  scenes,
                  SceneCategory.interviewRecruiter,
                ),
              ),
              (
                id: 'behavioral',
                title: '行为面试',
                caption: '经历、行动与结果',
                icon: Icons.forum_outlined,
                scenes: _scenesByCategory(
                  scenes,
                  SceneCategory.interviewBehavioral,
                ),
              ),
              (
                id: 'professional',
                title: '岗位专业面试',
                caption: '项目与专业表达',
                icon: Icons.laptop_mac_outlined,
                scenes: _scenesByCategory(
                  scenes,
                  SceneCategory.interviewProfessional,
                ),
              ),
              (
                id: 'manager',
                title: 'Hiring Manager',
                caption: '岗位匹配与业务影响',
                icon: Icons.supervisor_account_outlined,
                scenes: _scenesByCategory(
                  scenes,
                  SceneCategory.interviewHiringManager,
                ),
              ),
              (
                id: 'custom',
                title: '自定义面试',
                caption: '按你的目标配置',
                icon: Icons.tune_rounded,
                scenes: _scenesByCategory(
                  scenes,
                  SceneCategory.interviewCustom,
                ),
              ),
            ]
            .where((mode) => mode.scenes.isNotEmpty)
            .toList(growable: false);
    if (fullScene == null && modes.isEmpty) {
      return const PreparationCatalogEmpty(message: '当前没有可用的英文面试练习。');
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const PreparationCatalogHeader(
          title: '英文面试',
          description: '准备一整轮，或只练最需要的一关。',
          titleKey: Key('practice-hub-title-interview'),
        ),
        const SizedBox(height: 20),
        if (onOpenJobPreparation != null || fullScene != null) ...[
          PreparationFeaturedScene(
            key: const Key('interview-mode-full'),
            eyebrow: '推荐',
            title: onOpenJobPreparation == null ? '开始岗位专业面试' : '开始模拟面试',
            description: onOpenJobPreparation == null
                ? fullScene?.prompt.publicSceneBrief ?? '围绕项目与专业表达进入一轮练习。'
                : '带上岗位信息，问题会更贴近真实面试。',
            actionLabel: onOpenJobPreparation == null ? '直接开始' : '使用 JD 开始',
            actionKey: onOpenJobPreparation == null
                ? fullScene == null
                      ? null
                      : Key('catalog-scene-${fullScene.id}')
                : const Key('open-job-preparation'),
            icon: Icons.play_arrow_rounded,
            color: const Color(0xFF20252A),
            foregroundColor: Colors.white,
            assetPath: 'assets/images/scenes/interview-hero.jpg',
            onPressed:
                onOpenJobPreparation ??
                (fullScene == null ? null : () => onScenePressed(fullScene)),
          ),
          const SizedBox(height: 24),
        ],
        const Text('专项练习', style: PreparationDesign.sectionTitle),
        const SizedBox(height: 12),
        _AdaptiveCardGrid(
          children: [
            for (final mode in modes)
              _InterviewModeCard(
                key: Key('interview-mode-${mode.id}'),
                title: mode.title,
                caption: mode.caption,
                icon: mode.icon,
                sceneCount: mode.scenes.length,
                onPressed: () => _openScenePicker(
                  context,
                  title: mode.title,
                  scenes: mode.scenes,
                  onScenePressed: onScenePressed,
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _AdaptiveCardGrid extends StatelessWidget {
  const _AdaptiveCardGrid({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth < 310 || textScale > 1.2) {
          final cardHeight = 158 + (textScale - 1).clamp(0, 2).toDouble() * 80;
          return Column(
            children: [
              for (var index = 0; index < children.length; index++) ...[
                SizedBox(height: cardHeight, child: children[index]),
                if (index != children.length - 1) const SizedBox(height: 10),
              ],
            ],
          );
        }
        return GridView.count(
          shrinkWrap: true,
          primary: false,
          physics: const NeverScrollableScrollPhysics(),
          crossAxisCount: 2,
          mainAxisSpacing: 12,
          crossAxisSpacing: 12,
          mainAxisExtent: 158 + (textScale - 1).clamp(0, 0.2).toDouble() * 100,
          children: children,
        );
      },
    );
  }
}

class _InterviewModeCard extends StatelessWidget {
  const _InterviewModeCard({
    required this.title,
    required this.caption,
    required this.icon,
    required this.sceneCount,
    required this.onPressed,
    super.key,
  });

  final String title;
  final String caption;
  final IconData icon;
  final int sceneCount;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Semantics(
        button: true,
        label: '$title。$caption。$sceneCount 个练习',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          onTap: onPressed,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: double.infinity,
                height: 64,
                color: PreparationDesign.interviewTint,
                child: Icon(icon, size: 25, color: PreparationDesign.interview),
              ),
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(14, 12, 12, 14),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: PreparationDesign.cardTitle,
                      ),
                      const Spacer(),
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              caption,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: PreparationDesign.meta,
                            ),
                          ),
                          const SizedBox(width: 4),
                          const Icon(
                            Icons.arrow_forward_rounded,
                            size: 16,
                            color: PreparationDesign.secondary,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

void _openScenePicker(
  BuildContext context, {
  required String title,
  required List<SceneDefinition> scenes,
  required ValueChanged<SceneDefinition> onScenePressed,
}) {
  if (scenes.isEmpty) {
    return;
  }
  showModalBottomSheet<void>(
    context: context,
    useSafeArea: true,
    isScrollControlled: true,
    showDragHandle: true,
    backgroundColor: Colors.white,
    builder: (sheetContext) => _ScenePickerSheet(
      title: title,
      scenes: scenes,
      onScenePressed: (scene) {
        Navigator.of(sheetContext).pop();
        onScenePressed(scene);
      },
    ),
  );
}

class _ScenePickerSheet extends StatelessWidget {
  const _ScenePickerSheet({
    required this.title,
    required this.scenes,
    required this.onScenePressed,
  });

  final String title;
  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onScenePressed;

  @override
  Widget build(BuildContext context) {
    final screenHeight = MediaQuery.sizeOf(context).height;
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    final estimatedHeight =
        132 + scenes.length * (98 + (textScale - 1).clamp(0, 1) * 120);
    final maximumSheetHeight = screenHeight * 0.82;
    final minimumSheetHeight = maximumSheetHeight < 280
        ? maximumSheetHeight
        : 280.0;
    final sheetHeight = estimatedHeight
        .clamp(minimumSheetHeight, maximumSheetHeight)
        .toDouble();
    return SizedBox(
      height: sheetHeight,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 5),
            const Text(
              '选择一个练习',
              style: TextStyle(
                color: PreparationDesign.inkSecondary,
                fontSize: 14,
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.separated(
                itemCount: scenes.length,
                separatorBuilder: (_, _) =>
                    const Divider(height: 1, color: PreparationDesign.border),
                itemBuilder: (context, index) {
                  final scene = scenes[index];
                  return _CatalogSceneCard(
                    scene: scene,
                    onPressed: () => onScenePressed(scene),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _CatalogSceneCard extends StatelessWidget {
  const _CatalogSceneCard({required this.scene, required this.onPressed});

  final SceneDefinition scene;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      label: '${scene.name}。${scene.prompt.publicSceneBrief}',
      onTap: onPressed,
      excludeSemantics: true,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          key: Key('catalog-scene-${scene.id}'),
          borderRadius: BorderRadius.circular(14),
          onTap: onPressed,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 13),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: Icon(
                    _practiceExperienceIcon(scene.experience),
                    size: 20,
                    color: PreparationDesign.inkSecondary,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        scene.name,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        scene.prompt.publicSceneBrief,
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          color: PreparationDesign.inkSecondary,
                          fontSize: 13,
                          height: 1.4,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 10),
                const Padding(
                  padding: EdgeInsets.only(top: 3),
                  child: Icon(
                    Icons.arrow_forward_ios_rounded,
                    size: 15,
                    color: PreparationDesign.inkTertiary,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

List<SceneDefinition> _scenesByCategory(
  List<SceneDefinition> scenes,
  SceneCategory category,
) =>
    scenes.where((scene) => scene.category == category).toList(growable: false);

IconData _practiceExperienceIcon(PracticeExperience experience) {
  return switch (experience) {
    PracticeExperience.interview => Icons.work_outline_rounded,
    PracticeExperience.ieltsSpeaking => Icons.school_outlined,
    PracticeExperience.workplace => Icons.business_center_outlined,
    PracticeExperience.lifeAndTravel => Icons.travel_explore_outlined,
  };
}
