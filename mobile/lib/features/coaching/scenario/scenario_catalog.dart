import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/preparation/preparation_catalog_components.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

enum _ScenarioFilter { recommended, workplace, travel, daily }

class ScenarioCatalog extends StatefulWidget {
  const ScenarioCatalog({
    required this.scenes,
    required this.onScenePressed,
    super.key,
  });

  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onScenePressed;

  @override
  State<ScenarioCatalog> createState() => _ScenarioCatalogState();
}

class _ScenarioCatalogState extends State<ScenarioCatalog> {
  _ScenarioFilter _filter = _ScenarioFilter.recommended;

  List<_ScenarioFilter> get _availableFilters {
    return [
      _ScenarioFilter.recommended,
      if (widget.scenes.any(
        (scene) => scene.category == SceneCategory.workplaceGeneral,
      ))
        _ScenarioFilter.workplace,
      if (widget.scenes.any(
        (scene) => scene.category == SceneCategory.lifeTravel,
      ))
        _ScenarioFilter.travel,
      if (widget.scenes.any(
        (scene) => scene.category == SceneCategory.lifeDaily,
      ))
        _ScenarioFilter.daily,
    ];
  }

  List<SceneDefinition> get _visibleScenes {
    final available = widget.scenes;
    switch (_filter) {
      case _ScenarioFilter.recommended:
        return available.take(6).toList(growable: false);
      case _ScenarioFilter.workplace:
        return available
            .where((scene) => scene.category == SceneCategory.workplaceGeneral)
            .toList(growable: false);
      case _ScenarioFilter.travel:
        return available
            .where((scene) => scene.category == SceneCategory.lifeTravel)
            .toList(growable: false);
      case _ScenarioFilter.daily:
        return available
            .where((scene) => scene.category == SceneCategory.lifeDaily)
            .toList(growable: false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.scenes.isEmpty) {
      return const PreparationCatalogEmpty(message: '当前没有可用的情景陪练。');
    }
    final visibleScenes = _visibleScenes;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (_availableFilters.length > 2) ...[
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final filter in _availableFilters)
                ChoiceChip(
                  key: Key('scenario-filter-${filter.name}'),
                  label: Text(_scenarioFilterLabel(filter)),
                  selected: _filter == filter,
                  onSelected: (_) => setState(() => _filter = filter),
                  showCheckmark: false,
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.symmetric(horizontal: 10),
                  side: BorderSide(
                    color: _filter == filter
                        ? PreparationDesign.scenario
                        : PreparationDesign.border,
                  ),
                  selectedColor: PreparationDesign.scenario,
                  backgroundColor: PreparationDesign.surface,
                  labelStyle: TextStyle(
                    color: _filter == filter
                        ? Colors.white
                        : PreparationDesign.secondary,
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                  shape: const StadiumBorder(),
                ),
            ],
          ),
          const SizedBox(height: 16),
        ],
        if (visibleScenes.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 36),
            child: Center(
              child: Text(
                '这个分类暂时没有场景',
                style: TextStyle(color: Color(0xFF696B73)),
              ),
            ),
          )
        else
          _ScenarioSceneGrid(
            scenes: visibleScenes,
            includeCustom: _filter == _ScenarioFilter.recommended,
            onScenePressed: widget.onScenePressed,
          ),
      ],
    );
  }
}

class _ScenarioSceneGrid extends StatelessWidget {
  const _ScenarioSceneGrid({
    required this.scenes,
    required this.includeCustom,
    required this.onScenePressed,
  });

  final List<SceneDefinition> scenes;
  final bool includeCustom;
  final ValueChanged<SceneDefinition> onScenePressed;

  @override
  Widget build(BuildContext context) {
    final children = <Widget>[
      for (final scene in scenes)
        _ScenarioSceneCard(
          scene: scene,
          onPressed: () => onScenePressed(scene),
        ),
      if (includeCustom) const _ReservedSceneTile(),
    ];
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth < 310 || textScale > 1.25) {
          final cardHeight = 206 + (textScale - 1).clamp(0, 2).toDouble() * 100;
          return Column(
            children: [
              for (var index = 0; index < children.length; index++) ...[
                SizedBox(height: cardHeight, child: children[index]),
                if (index != children.length - 1) const SizedBox(height: 10),
              ],
            ],
          );
        }
        final cardHeight =
            206 + (textScale - 1).clamp(0, 0.25).toDouble() * 120;
        return GridView.builder(
          shrinkWrap: true,
          primary: false,
          physics: const NeverScrollableScrollPhysics(),
          itemCount: children.length,
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 2,
            crossAxisSpacing: 12,
            mainAxisSpacing: 12,
            mainAxisExtent: cardHeight,
          ),
          itemBuilder: (context, index) => children[index],
        );
      },
    );
  }
}

class _ScenarioSceneCard extends StatelessWidget {
  const _ScenarioSceneCard({required this.scene, required this.onPressed});

  final SceneDefinition scene;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final style = _scenarioCardStyle(scene);
    return Material(
      key: Key('catalog-scene-${scene.id}'),
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Semantics(
        button: true,
        label: '${scene.name}。${scene.prompt.publicSceneBrief}',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          onTap: onPressed,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                height: 122,
                width: double.infinity,
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    ColoredBox(color: style.background),
                    if (style.assetPath case final assetPath?)
                      Image.asset(
                        assetPath,
                        fit: BoxFit.cover,
                        alignment: style.imageAlignment,
                        errorBuilder: (_, _, _) => ColoredBox(
                          color: style.background,
                          child: Icon(
                            style.icon,
                            color: style.foreground,
                            size: 32,
                          ),
                        ),
                      )
                    else
                      Icon(style.icon, color: style.foreground, size: 34),
                    if (style.assetPath != null)
                      Positioned(
                        left: 10,
                        top: 10,
                        child: Container(
                          width: 32,
                          height: 32,
                          decoration: BoxDecoration(
                            color: Colors.white.withValues(alpha: 0.9),
                            shape: BoxShape.circle,
                          ),
                          child: Icon(
                            style.icon,
                            color: style.foreground,
                            size: 17,
                          ),
                        ),
                      ),
                  ],
                ),
              ),
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(13, 10, 11, 10),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        style.category,
                        style: PreparationDesign.meta.copyWith(
                          color: style.foreground,
                        ),
                      ),
                      const SizedBox(height: 3),
                      Text(
                        scene.name,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: PreparationDesign.cardTitle.copyWith(
                          fontSize: 15,
                        ),
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

class _ReservedSceneTile extends StatelessWidget {
  const _ReservedSceneTile();

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '自定义场景，即将开放',
      enabled: false,
      child: ClipRRect(
        key: const Key('scenario-custom-reserved'),
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        child: DecoratedBox(
          decoration: BoxDecoration(
            color: PreparationDesign.surface,
            borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
            border: Border.all(color: PreparationDesign.border),
          ),
          child: const Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                height: 122,
                width: double.infinity,
                child: ColoredBox(
                  color: PreparationDesign.softSurface,
                  child: Icon(
                    Icons.add_rounded,
                    size: 34,
                    color: PreparationDesign.tertiary,
                  ),
                ),
              ),
              Padding(
                padding: EdgeInsets.fromLTRB(13, 10, 11, 10),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('由你定义', style: PreparationDesign.meta),
                    SizedBox(height: 3),
                    Text('自定义场景', style: PreparationDesign.cardTitle),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

String _scenarioFilterLabel(_ScenarioFilter filter) {
  return switch (filter) {
    _ScenarioFilter.recommended => '推荐',
    _ScenarioFilter.workplace => '职场',
    _ScenarioFilter.travel => '旅行',
    _ScenarioFilter.daily => '日常',
  };
}

({
  Color background,
  Color foreground,
  IconData icon,
  String category,
  String? assetPath,
  Alignment imageAlignment,
})
_scenarioCardStyle(SceneDefinition scene) {
  return switch (scene.category) {
    SceneCategory.workplaceGeneral => (
      background: const Color(0xFFE8EBED),
      foreground: const Color(0xFF273238),
      icon: Icons.groups_outlined,
      category: '职场',
      assetPath: 'assets/images/scenes/workplace-scene.jpg',
      imageAlignment: Alignment.topCenter,
    ),
    SceneCategory.lifeTravel => (
      background: const Color(0xFFDDEBF0),
      foreground: const Color(0xFF1D4754),
      icon: Icons.flight_outlined,
      category: '旅行',
      assetPath: 'assets/images/scenes/travel-scene.jpg',
      imageAlignment: Alignment.center,
    ),
    SceneCategory.lifeDaily => (
      background: const Color(0xFFF2E8DE),
      foreground: const Color(0xFF4C392B),
      icon: Icons.chat_bubble_outline_rounded,
      category: '日常',
      assetPath: 'assets/images/scenes/daily-tutor.jpg',
      imageAlignment: const Alignment(0, -0.65),
    ),
    _ => (
      background: const Color(0xFFF0ECE7),
      foreground: PreparationDesign.ink,
      icon: Icons.tune_rounded,
      category: '自定义',
      assetPath: null,
      imageAlignment: Alignment.center,
    ),
  };
}
