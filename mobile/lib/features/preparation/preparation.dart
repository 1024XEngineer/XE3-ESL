/// Preparation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

const _jobInterviewScenarioId = 'scn_programmer_interview';

class PreparationPage extends StatefulWidget {
  const PreparationPage({
    this.showBackButton = false,
    this.previewMode = false,
    this.agentController,
    this.preparationController,
    this.launchController,
    this.onOpenJobPreparation,
    this.onSceneSelected,
    this.onPracticeStarted,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final AgentController? agentController;
  final PreparationController? preparationController;
  final PreparationLaunchController? launchController;
  final VoidCallback? onOpenJobPreparation;
  final VoidCallback? onSceneSelected;
  final VoidCallback? onPracticeStarted;

  @override
  State<PreparationPage> createState() => _PreparationPageState();
}

class _PreparationPageState extends State<PreparationPage> {
  TextEditingController? _backgroundController;
  String? _selectedFamily;

  @override
  void initState() {
    super.initState();
    widget.agentController?.addListener(_rebuild);
    widget.preparationController?.addListener(_rebuild);
    widget.launchController?.addListener(_rebuild);
    _backgroundController = _newBackgroundController(widget.launchController);
    unawaited(widget.preparationController?.loadIfNeeded());
  }

  @override
  void didUpdateWidget(covariant PreparationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.agentController != widget.agentController) {
      oldWidget.agentController?.removeListener(_rebuild);
      widget.agentController?.addListener(_rebuild);
    }
    if (oldWidget.preparationController != widget.preparationController) {
      oldWidget.preparationController?.removeListener(_rebuild);
      widget.preparationController?.addListener(_rebuild);
      unawaited(widget.preparationController?.loadIfNeeded());
    }
    if (oldWidget.launchController != widget.launchController) {
      oldWidget.launchController?.removeListener(_rebuild);
      widget.launchController?.addListener(_rebuild);
      _backgroundController?.dispose();
      _backgroundController = _newBackgroundController(widget.launchController);
    }
  }

  @override
  void dispose() {
    widget.agentController?.removeListener(_rebuild);
    widget.preparationController?.removeListener(_rebuild);
    widget.launchController?.removeListener(_rebuild);
    _backgroundController?.dispose();
    super.dispose();
  }

  void _rebuild() {
    if (mounted) {
      final launchBackground = widget.launchController?.backgroundSummary;
      final textController = _backgroundController;
      if (launchBackground != null &&
          textController != null &&
          textController.text != launchBackground) {
        textController.value = TextEditingValue(
          text: launchBackground,
          selection: TextSelection.collapsed(offset: launchBackground.length),
        );
      }
      setState(() {});
    }
  }

  TextEditingController? _newBackgroundController(
    PreparationLaunchController? controller,
  ) {
    return controller == null
        ? null
        : TextEditingController(text: controller.backgroundSummary);
  }

  Future<void> _startPractice() async {
    final catalog = widget.preparationController;
    final launch = widget.launchController;
    final scenario = catalog?.selectedScenario;
    final config = catalog?.detail?.config;
    final role = catalog?.selectedRole;
    final option = catalog?.selectedOption;
    if (catalog == null ||
        launch == null ||
        scenario == null ||
        config == null ||
        role == null ||
        option == null) {
      return;
    }
    if (launch.backgroundSummary.trim().isEmpty) {
      launch.updateBackgroundSummary('默认示例：${config.prompt.publicSceneBrief}');
    }
    final started = await launch.start(
      PreparationLaunchSelection.fromCatalog(
        scenario: scenario,
        config: config,
        role: role,
        option: option,
      ),
    );
    if (started && mounted) {
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _retryLaunch() async {
    final started = await widget.launchController?.retry() ?? false;
    if (started && mounted) {
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _selectPreviewScene(AgentScene scene) async {
    final controller = widget.agentController;
    if (controller == null || controller.isBusy) {
      return;
    }
    await controller.selectScene(scene);
    if (!mounted || controller.scene?.id != scene.id) {
      return;
    }
    if (widget.onSceneSelected case final callback?) {
      callback();
    } else if (widget.showBackButton) {
      Navigator.of(context).maybePop();
    }
  }

  Future<void> _retryPreviewOperation() async {
    final controller = widget.agentController;
    if (controller == null || !controller.canRetry) {
      return;
    }
    await controller.retryLastOperation();
    if (!mounted ||
        controller.errorMessage != null ||
        controller.activeMatter == null) {
      return;
    }
    if (widget.onSceneSelected case final callback?) {
      callback();
    } else if (widget.showBackButton) {
      Navigator.of(context).maybePop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.preparationController;
    final launchLocked = widget.launchController?.isSelectionLocked ?? false;
    return PopScope<void>(
      canPop: !launchLocked,
      child: Scaffold(
        key: const Key('scenes-page'),
        backgroundColor: const Color(0xFFF3F3F0),
        appBar: widget.showBackButton
            ? AppBar(
                backgroundColor: const Color(0xFFF3F3F0),
                surfaceTintColor: Colors.transparent,
                elevation: 0,
                scrolledUnderElevation: 0,
                leading: IconButton(
                  key: const Key('preparation-route-back-button'),
                  tooltip: '返回',
                  onPressed: launchLocked
                      ? null
                      : () => Navigator.of(context).maybePop(),
                  icon: const Icon(Icons.arrow_back_rounded),
                ),
              )
            : null,
        body: SafeArea(
          bottom: false,
          child: controller == null
              ? _buildPreview()
              : _buildCatalog(controller),
        ),
      ),
    );
  }

  Widget _buildCatalog(PreparationController controller) {
    final selectedScenario = controller.selectedScenario;
    if (selectedScenario != null) {
      return _ScenarioDetailView(
        controller: controller,
        scenario: selectedScenario,
        launchController: widget.launchController,
        backgroundController: _backgroundController,
        hasAgentContext: widget.agentController?.threadId != null,
        onStart: _startPractice,
        onRetry: _retryLaunch,
      );
    }
    final selectedFamily = _selectedFamily;
    if (selectedFamily != null) {
      return _buildFamilyScenarios(controller, selectedFamily);
    }
    return ListView(
      key: const Key('preparation-catalog-list'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 112),
      children: [
        const Row(
          children: [
            Expanded(
              child: Text(
                '练习中心',
                key: Key('training-center-title'),
                style: TextStyle(fontSize: 28, fontWeight: FontWeight.w800),
              ),
            ),
            _TrainingCatalogStatus(),
          ],
        ),
        const SizedBox(height: 8),
        const Text(
          '先选择一个使用场景，再挑选可直接开始的基础练习。',
          style: TextStyle(
            color: Color(0xFF696B73),
            fontSize: 14,
            height: 1.45,
          ),
        ),
        if (widget.onOpenJobPreparation != null) ...[
          const SizedBox(height: 14),
          OutlinedButton.icon(
            key: const Key('open-job-preparation'),
            onPressed: widget.onOpenJobPreparation,
            icon: const Icon(Icons.description_outlined),
            label: const Text('有职位描述？使用 JD 专项准备'),
            style: OutlinedButton.styleFrom(
              minimumSize: const Size.fromHeight(48),
            ),
          ),
        ],
        const SizedBox(height: 22),
        if (controller.isLoadingScenarios)
          const _CatalogLoading(key: Key('preparation-catalog-loading'))
        else if (controller.errorMessage case final message?)
          _CatalogFailure(
            key: const Key('preparation-catalog-error'),
            message: message,
            onRetry: controller.retryLastFailure,
          )
        else if (controller.scenarios.isEmpty)
          const _CatalogEmpty()
        else
          for (final family in const [
            'INTERVIEW',
            'EXAM',
            'WORKPLACE',
            'DAILY',
          ]) ...[
            _ScenarioFamilyCard(
              family: family,
              scenarios: controller.scenarios
                  .where((scenario) => scenario.type == family)
                  .toList(growable: false),
              onPressed: () => setState(() => _selectedFamily = family),
            ),
            const SizedBox(height: 14),
          ],
      ],
    );
  }

  Widget _buildFamilyScenarios(
    PreparationController controller,
    String family,
  ) {
    final scenarios = controller.scenarios
        .where((scenario) => scenario.type == family)
        .toList(growable: false);
    return ListView(
      key: Key('preparation-family-list-$family'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 112),
      children: [
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            key: const Key('preparation-back-to-families'),
            onPressed: () => setState(() => _selectedFamily = null),
            icon: const Icon(Icons.arrow_back_rounded),
            label: const Text('四大场景'),
          ),
        ),
        const SizedBox(height: 8),
        _ScenarioFamilyHeading(family: family),
        const SizedBox(height: 8),
        Text(
          '${scenarios.length} 个基础子场景，均可使用默认设定直接开始。',
          style: const TextStyle(color: Color(0xFF696B73), height: 1.45),
        ),
        const SizedBox(height: 20),
        for (final scenario in scenarios) ...[
          _CatalogScenarioCard(
            scenario: scenario,
            onPressed: () => controller.selectScenario(scenario),
            onOpenJobPreparation:
                widget.onOpenJobPreparation != null &&
                    scenario.id == _jobInterviewScenarioId
                ? widget.onOpenJobPreparation
                : null,
          ),
          const SizedBox(height: 14),
        ],
      ],
    );
  }

  Widget _buildPreview() {
    final controller = widget.agentController;
    final practiceAvailable = controller?.supportsPracticeFlow ?? true;
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 28, 20, 140),
      children: [
        const Text(
          '练习中心',
          style: TextStyle(fontSize: 28, fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        Text(
          practiceAvailable
              ? widget.previewMode
                    ? '预览练习专题与进入流程。'
                    : '练习内容暂时无法加载，请稍后重试。'
              : '练习功能正在准备中，目前可以先使用文字陪练。',
          key: const Key('practice-availability-message'),
          style: const TextStyle(color: Color(0xFF696B73), fontSize: 15),
        ),
        const SizedBox(height: 28),
        for (final scene in agentScenes) ...[
          _PreviewSceneCard(
            scene: scene,
            selected: controller?.scene?.id == scene.id,
            enabled: practiceAvailable && (controller?.canSelectScene ?? false),
            onPressed: () => _selectPreviewScene(scene),
          ),
          const SizedBox(height: 12),
        ],
        if (controller?.isBusy ?? false)
          const LinearProgressIndicator(
            key: Key('scene-selection-progress'),
            minHeight: 2,
          ),
        if (controller?.errorMessage case final message?) ...[
          const SizedBox(height: 14),
          _InlineFailure(
            key: const Key('scene-operation-error'),
            message: message,
            retryKey: const Key('scene-retry-operation'),
            onRetry: controller?.canRetry ?? false
                ? _retryPreviewOperation
                : null,
          ),
        ],
      ],
    );
  }
}

class _ScenarioDetailView extends StatelessWidget {
  const _ScenarioDetailView({
    required this.controller,
    required this.scenario,
    required this.launchController,
    required this.backgroundController,
    required this.hasAgentContext,
    required this.onStart,
    required this.onRetry,
  });

  final PreparationController controller;
  final PreparationScenario scenario;
  final PreparationLaunchController? launchController;
  final TextEditingController? backgroundController;
  final bool hasAgentContext;
  final Future<void> Function() onStart;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final detail = controller.detail;
    final selectedRole = controller.selectedRole;
    final launchLocked = launchController?.isSelectionLocked ?? false;
    return ListView(
      key: const Key('preparation-scenario-detail'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 140),
      children: [
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            key: const Key('preparation-back-to-catalog'),
            onPressed: launchLocked
                ? null
                : () {
                    launchController?.selectionChanged();
                    controller.showScenarioList();
                  },
            icon: const Icon(Icons.arrow_back_rounded),
            label: Text('返回${_scenarioFamilyLabel(scenario.type)}'),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          _scenarioFamilyLabel(scenario.type),
          style: const TextStyle(
            color: Color(0xFF696B73),
            fontSize: 13,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 6),
        Text(
          scenario.name,
          key: const Key('preparation-scenario-title'),
          style: const TextStyle(fontSize: 30, fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text(
          '先预览场景目标和双方角色，确认后再创建本次语音练习。',
          style: TextStyle(color: Color(0xFF696B73), fontSize: 15),
        ),
        const SizedBox(height: 24),
        if (controller.isLoadingDetail)
          const _CatalogLoading(key: Key('preparation-detail-loading'))
        else if (controller.errorMessage case final message?)
          _CatalogFailure(
            key: const Key('preparation-detail-error'),
            message: message,
            onRetry: controller.retryLastFailure,
          )
        else if (detail != null) ...[
          _ScenarioConfigCard(config: detail.config),
          if (controller.roles.length > 1) ...[
            const SizedBox(height: 28),
            const Text(
              '选择对话角色',
              style: TextStyle(fontSize: 21, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 6),
            const Text(
              'AI 会按所选角色和场景目标推进对话。',
              style: TextStyle(color: Color(0xFF696B73), height: 1.45),
            ),
            const SizedBox(height: 14),
            for (final role in controller.roles) ...[
              _RoleCard(
                role: role,
                selected: selectedRole?.id == role.id,
                onPressed: launchLocked
                    ? null
                    : () {
                        launchController?.selectionChanged();
                        controller.selectRole(role);
                      },
              ),
              const SizedBox(height: 10),
            ],
            if (selectedRole != null) ...[
              const SizedBox(height: 18),
              const Text(
                '选择练习方式',
                style: TextStyle(fontSize: 21, fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 6),
              const Text(
                '完整模拟和专项练习都围绕当前选择的视角进行。',
                style: TextStyle(color: Color(0xFF696B73), height: 1.45),
              ),
              const SizedBox(height: 14),
              for (final option in controller.availableOptions) ...[
                _OptionCard(
                  option: option,
                  selected: controller.selectedOption?.id == option.id,
                  onPressed: launchLocked
                      ? null
                      : () {
                          launchController?.selectionChanged();
                          controller.selectOption(option);
                        },
                ),
                const SizedBox(height: 10),
              ],
            ],
          ],
          if (controller.hasCompleteSelection) ...[
            const SizedBox(height: 18),
            _PracticePreviewCard(
              scenario: scenario,
              config: detail.config,
              role: selectedRole!,
              option: controller.selectedOption!,
            ),
            const SizedBox(height: 12),
            if (launchController case final launch?)
              _LaunchSelectionCard(
                controller: launch,
                backgroundController: backgroundController!,
                hasAgentContext: hasAgentContext,
                onStart: onStart,
                onRetry: onRetry,
              )
            else
              const _LaunchUnavailableNotice(),
          ],
        ],
      ],
    );
  }
}

class _CatalogLoading extends StatelessWidget {
  const _CatalogLoading({super.key});

  @override
  Widget build(BuildContext context) {
    return const Padding(
      padding: EdgeInsets.symmetric(vertical: 48),
      child: Center(
        child: Column(
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 14),
            Text('正在加载练习专题'),
          ],
        ),
      ),
    );
  }
}

class _TrainingCatalogStatus extends StatelessWidget {
  const _TrainingCatalogStatus();

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '练习专题目录',
      child: Container(
        key: const Key('training-catalog-status'),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(
          color: const Color(0xFFE7E7E3),
          borderRadius: BorderRadius.circular(99),
        ),
        child: const Text(
          '持续更新',
          style: TextStyle(
            color: Color(0xFF55575E),
            fontSize: 12,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
    );
  }
}

class _CatalogFailure extends StatelessWidget {
  const _CatalogFailure({
    required this.message,
    required this.onRetry,
    super.key,
  });

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return _InlineFailure(
      message: message,
      retryKey: const Key('preparation-catalog-retry'),
      onRetry: onRetry,
    );
  }
}

class _CatalogEmpty extends StatelessWidget {
  const _CatalogEmpty();

  @override
  Widget build(BuildContext context) {
    return const Material(
      key: Key('preparation-catalog-empty'),
      color: Colors.white,
      borderRadius: BorderRadius.all(Radius.circular(20)),
      child: Padding(
        padding: EdgeInsets.all(20),
        child: Column(
          children: [
            Icon(Icons.inbox_outlined, size: 30),
            SizedBox(height: 10),
            Text('暂时没有开放的练习场景', style: TextStyle(fontWeight: FontWeight.w700)),
            SizedBox(height: 5),
            Text(
              '你仍然可以返回 Agent 首页描述自己的职业英语需求。',
              textAlign: TextAlign.center,
              style: TextStyle(color: Color(0xFF696B73)),
            ),
          ],
        ),
      ),
    );
  }
}

class _ScenarioFamilyHeading extends StatelessWidget {
  const _ScenarioFamilyHeading({required this.family});

  final String family;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      header: true,
      child: Row(
        children: [
          Icon(
            _scenarioFamilyIcon(family),
            size: 20,
            color: const Color(0xFF4F5054),
          ),
          const SizedBox(width: 8),
          Text(
            _scenarioFamilyLabel(family),
            key: Key('preparation-family-$family'),
            style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w800),
          ),
        ],
      ),
    );
  }
}

class _ScenarioFamilyCard extends StatelessWidget {
  const _ScenarioFamilyCard({
    required this.family,
    required this.scenarios,
    required this.onPressed,
  });

  final String family;
  final List<PreparationScenario> scenarios;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final examples = scenarios
        .take(3)
        .map((scenario) => scenario.name)
        .join(' · ');
    return Card(
      elevation: 0,
      color: Colors.white,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: const BorderSide(color: Color(0xFFDEDEDA)),
      ),
      child: Semantics(
        button: true,
        label:
            '${_scenarioFamilyLabel(family)}，${scenarios.length} 个子场景。'
            '${_scenarioFamilyDescription(family)}',
        excludeSemantics: true,
        child: InkWell(
          key: Key('preparation-family-entry-$family'),
          onTap: onPressed,
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 48,
                  height: 48,
                  decoration: BoxDecoration(
                    color: const Color(0xFFE9E9E5),
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Icon(
                    _scenarioFamilyIcon(family),
                    color: const Color(0xFF4F5054),
                  ),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              _scenarioFamilyLabel(family),
                              key: Key('preparation-family-$family'),
                              style: const TextStyle(
                                fontSize: 19,
                                fontWeight: FontWeight.w800,
                              ),
                            ),
                          ),
                          Text(
                            '${scenarios.length} 个',
                            style: const TextStyle(
                              color: Color(0xFF777980),
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 6),
                      Text(
                        _scenarioFamilyDescription(family),
                        style: const TextStyle(
                          color: Color(0xFF5F6168),
                          height: 1.4,
                        ),
                      ),
                      if (examples.isNotEmpty) ...[
                        const SizedBox(height: 9),
                        Text(
                          examples,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            color: Color(0xFF85878D),
                            fontSize: 13,
                            height: 1.35,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                const Padding(
                  padding: EdgeInsets.only(top: 4),
                  child: Icon(
                    Icons.arrow_forward_ios_rounded,
                    size: 15,
                    color: Color(0xFF777980),
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

class _CatalogScenarioCard extends StatelessWidget {
  const _CatalogScenarioCard({
    required this.scenario,
    required this.onPressed,
    this.onOpenJobPreparation,
  });

  final PreparationScenario scenario;
  final VoidCallback onPressed;
  final VoidCallback? onOpenJobPreparation;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: const Color(0xFFFAFAF8),
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(18),
        side: const BorderSide(color: Color(0xFFDEDEDA)),
      ),
      child: InkWell(
        key: Key('catalog-scenario-${scenario.id}'),
        onTap: onPressed,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: const Color(0xFFE9E9E5),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  _scenarioFamilyIcon(scenario.type),
                  color: Color(0xFF4F5054),
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      scenario.name,
                      style: const TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Text(
                      scenario.summary,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        color: Color(0xFF696B73),
                        height: 1.4,
                      ),
                    ),
                    if (onOpenJobPreparation != null) ...[
                      const SizedBox(height: 9),
                      OutlinedButton.icon(
                        key: const Key('open-job-preparation'),
                        onPressed: onOpenJobPreparation,
                        icon: const Icon(Icons.description_outlined, size: 18),
                        label: const Text('按岗位与 JD 定制'),
                      ),
                    ],
                  ],
                ),
              ),
              const Padding(
                padding: EdgeInsets.only(top: 10),
                child: Text(
                  '开始练习',
                  style: TextStyle(
                    color: Color(0xFF303136),
                    fontWeight: FontWeight.w800,
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

class _ScenarioConfigCard extends StatelessWidget {
  const _ScenarioConfigCard({required this.config});

  final PreparationScenarioConfig config;

  @override
  Widget build(BuildContext context) {
    final prompt = config.prompt;
    return Material(
      key: const Key('preparation-scenario-config'),
      color: Colors.white,
      borderRadius: BorderRadius.circular(20),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '默认示例',
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 8),
            Text(
              prompt.publicSceneBrief,
              style: const TextStyle(color: Color(0xFF5F6168), height: 1.45),
            ),
            const SizedBox(height: 16),
            const Text('练习目标', style: TextStyle(fontWeight: FontWeight.w800)),
            const SizedBox(height: 5),
            Text(
              prompt.practiceGoal,
              style: const TextStyle(color: Color(0xFF5F6168), height: 1.45),
            ),
            const SizedBox(height: 14),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _PreviewLabel(
                  icon: Icons.person_outline_rounded,
                  text: '你：${prompt.userRole}',
                ),
                _PreviewLabel(
                  icon: Icons.smart_toy_outlined,
                  text: 'AI：${prompt.aiRole}',
                ),
                _PreviewLabel(
                  icon: Icons.schedule_rounded,
                  text:
                      '约 ${_durationMinutes(prompt.suggestedDurationSeconds)} 分钟',
                ),
              ],
            ),
            const SizedBox(height: 14),
            const Text('对话重点', style: TextStyle(fontWeight: FontWeight.w800)),
            const SizedBox(height: 7),
            for (final blueprint in prompt.turnBlueprints.take(4))
              Padding(
                padding: const EdgeInsets.only(bottom: 6),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('• '),
                    Expanded(
                      child: Text(
                        blueprint,
                        style: const TextStyle(
                          color: Color(0xFF5F6168),
                          height: 1.4,
                        ),
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

class _PreviewLabel extends StatelessWidget {
  const _PreviewLabel({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: const BoxDecoration(
        color: Color(0xFFF1F1EE),
        borderRadius: BorderRadius.all(Radius.circular(12)),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 17, color: const Color(0xFF55575E)),
            const SizedBox(width: 6),
            Flexible(child: Text(text, style: const TextStyle(fontSize: 13))),
          ],
        ),
      ),
    );
  }
}

class _RoleCard extends StatelessWidget {
  const _RoleCard({
    required this.role,
    required this.selected,
    required this.onPressed,
  });

  final PreparationRole role;
  final bool selected;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: Colors.white,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(18),
        side: BorderSide(
          color: selected ? const Color(0xFF303136) : const Color(0xFFE8E8E4),
          width: selected ? 1.5 : 1,
        ),
      ),
      child: Semantics(
        key: Key('preparation-role-${role.id}'),
        container: true,
        button: true,
        selected: selected,
        inMutuallyExclusiveGroup: true,
        label: '${role.displayName}. ${role.responsibilities} ${role.style}',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          onTap: onPressed,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  selected
                      ? Icons.radio_button_checked_rounded
                      : Icons.radio_button_unchecked_rounded,
                  color: const Color(0xFF4F5054),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        role.displayName,
                        style: const TextStyle(fontWeight: FontWeight.w800),
                      ),
                      const SizedBox(height: 5),
                      Text(
                        role.responsibilities,
                        style: const TextStyle(
                          color: Color(0xFF696B73),
                          height: 1.4,
                        ),
                      ),
                      const SizedBox(height: 5),
                      Text(
                        role.style,
                        style: const TextStyle(
                          color: Color(0xFF85878D),
                          fontSize: 13,
                          height: 1.35,
                        ),
                      ),
                    ],
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

class _OptionCard extends StatelessWidget {
  const _OptionCard({
    required this.option,
    required this.selected,
    required this.onPressed,
  });

  final PreparationOption option;
  final bool selected;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final typeLabel = switch (option.type) {
      PreparationOptionType.fullSimulation => '完整模拟',
      PreparationOptionType.focus => '专项练习',
    };
    return Card(
      elevation: 0,
      color: Colors.white,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(18),
        side: BorderSide(
          color: selected ? const Color(0xFF303136) : const Color(0xFFE8E8E4),
          width: selected ? 1.5 : 1,
        ),
      ),
      child: Semantics(
        key: Key('preparation-option-${option.id}'),
        container: true,
        button: true,
        selected: selected,
        inMutuallyExclusiveGroup: true,
        label: '$typeLabel: ${option.displayName}',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          onTap: onPressed,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Icon(
                  selected ? Icons.check_circle_rounded : Icons.circle_outlined,
                  color: const Color(0xFF4F5054),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        typeLabel,
                        style: const TextStyle(fontWeight: FontWeight.w800),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        option.displayName,
                        style: const TextStyle(color: Color(0xFF696B73)),
                      ),
                    ],
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

class _PracticePreviewCard extends StatelessWidget {
  const _PracticePreviewCard({
    required this.scenario,
    required this.config,
    required this.role,
    required this.option,
  });

  final PreparationScenario scenario;
  final PreparationScenarioConfig config;
  final PreparationRole role;
  final PreparationOption option;

  @override
  Widget build(BuildContext context) {
    return Material(
      key: const Key('preparation-practice-preview'),
      color: Colors.white,
      borderRadius: BorderRadius.circular(18),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Row(
              children: [
                Icon(Icons.fact_check_outlined, size: 21),
                SizedBox(width: 8),
                Text('开始前确认', style: TextStyle(fontWeight: FontWeight.w800)),
              ],
            ),
            const SizedBox(height: 12),
            _PreviewRow(label: '场景', value: scenario.name),
            _PreviewRow(label: '目标', value: config.prompt.practiceGoal),
            _PreviewRow(label: '你的角色', value: config.prompt.userRole),
            _PreviewRow(label: 'AI 角色', value: role.displayName),
            _PreviewRow(label: '练习方式', value: option.displayName),
          ],
        ),
      ),
    );
  }
}

class _PreviewRow extends StatelessWidget {
  const _PreviewRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 72,
            child: Text(
              label,
              style: const TextStyle(
                color: Color(0xFF777980),
                fontWeight: FontWeight.w700,
              ),
            ),
          ),
          Expanded(child: Text(value, style: const TextStyle(height: 1.35))),
        ],
      ),
    );
  }
}

class _LaunchSelectionCard extends StatelessWidget {
  const _LaunchSelectionCard({
    required this.controller,
    required this.backgroundController,
    required this.hasAgentContext,
    required this.onStart,
    required this.onRetry,
  });

  final PreparationLaunchController controller;
  final TextEditingController backgroundController;
  final bool hasAgentContext;
  final Future<void> Function() onStart;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Material(
      key: const Key('preparation-launch-selection'),
      color: const Color(0xFFEDEDEA),
      borderRadius: BorderRadius.circular(18),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              '自定义设置（可选）',
              style: TextStyle(fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 6),
            const Text(
              '留空会直接使用上方默认示例；也可以补充一句真实背景或练习目标。',
              style: TextStyle(color: Color(0xFF5F6168), height: 1.45),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('preparation-background-summary'),
              controller: backgroundController,
              enabled: !controller.isSelectionLocked,
              minLines: 3,
              maxLines: 6,
              maxLength: 4000,
              textInputAction: TextInputAction.newline,
              decoration: const InputDecoration(
                labelText: '一句话背景或目标',
                hintText: '可留空直接开始',
                filled: true,
                fillColor: Colors.white,
                border: OutlineInputBorder(),
              ),
              onChanged: controller.updateBackgroundSummary,
            ),
            if (!hasAgentContext) ...[
              const SizedBox(height: 4),
              const Text(
                'Agent 对话仍在恢复。无需预先建立事项，恢复完成后可从这里直接开始。',
                key: Key('preparation-agent-context-missing'),
                style: TextStyle(color: Color(0xFF6A5B38), height: 1.4),
              ),
            ],
            if (controller.errorMessage case final message?) ...[
              const SizedBox(height: 8),
              Text(
                message,
                key: const Key('preparation-launch-error'),
                style: const TextStyle(color: Color(0xFF9A332A), height: 1.4),
              ),
            ],
            if (controller.isStarting) ...[
              const SizedBox(height: 10),
              const LinearProgressIndicator(
                key: Key('preparation-launch-progress'),
              ),
              const SizedBox(height: 6),
              Text(
                _launchStageLabel(controller.stage),
                key: const Key('preparation-launch-stage'),
                style: const TextStyle(color: Color(0xFF5F6168)),
              ),
            ],
            const SizedBox(height: 12),
            FilledButton(
              key: const Key('preparation-start-practice'),
              onPressed: controller.isStarting ? null : onStart,
              child: Text(controller.isStarting ? '正在创建练习' : '使用当前设定开始练习'),
            ),
            if (controller.canRetry && !controller.isStarting)
              TextButton(
                key: const Key('preparation-retry-launch'),
                onPressed: onRetry,
                child: const Text('重试上次启动'),
              ),
          ],
        ),
      ),
    );
  }
}

class _LaunchUnavailableNotice extends StatelessWidget {
  const _LaunchUnavailableNotice();

  @override
  Widget build(BuildContext context) {
    return const Material(
      key: Key('preparation-launch-unavailable'),
      color: Color(0xFFEDEDEA),
      borderRadius: BorderRadius.all(Radius.circular(18)),
      child: Padding(
        padding: EdgeInsets.all(16),
        child: Text('正式练习启动服务未注入，当前选择不会写入业务数据。'),
      ),
    );
  }
}

String _launchStageLabel(PreparationLaunchStage? stage) {
  return switch (stage) {
    PreparationLaunchStage.context => '正在确认 Agent 对话与事项',
    PreparationLaunchStage.matter => '正在创建或激活本次求职事项',
    PreparationLaunchStage.profile => '正在保存本次背景',
    PreparationLaunchStage.snapshot => '正在冻结练习背景',
    PreparationLaunchStage.plan => '正在创建练习计划',
    PreparationLaunchStage.session => '正在创建练习会话',
    PreparationLaunchStage.voice => '正在连接第一道语音题目',
    null => '正在准备练习',
  };
}

String _scenarioFamilyLabel(String family) {
  return switch (family) {
    'INTERVIEW' => '求职面试',
    'EXAM' => '考试口语',
    'WORKPLACE' => '职场沟通',
    'DAILY' => '生活沟通',
    _ => family,
  };
}

String _scenarioFamilyDescription(String family) {
  return switch (family) {
    'INTERVIEW' => '练习自我介绍、项目表达、行为问题与岗位沟通。',
    'EXAM' => '覆盖 IELTS 分段练习、完整模拟与自定义题型。',
    'WORKPLACE' => '处理汇报、会议、协作、客户沟通和条件协商。',
    'DAILY' => '练习餐厅、交通、酒店、购物、预约等真实交流。',
    _ => '选择一个基础子场景开始对话。',
  };
}

IconData _scenarioFamilyIcon(String family) {
  return switch (family) {
    'INTERVIEW' => Icons.work_outline_rounded,
    'EXAM' => Icons.school_outlined,
    'WORKPLACE' => Icons.groups_outlined,
    'DAILY' => Icons.hotel_outlined,
    _ => Icons.record_voice_over_outlined,
  };
}

int _durationMinutes(int seconds) => (seconds / 60).ceil();

class _InlineFailure extends StatelessWidget {
  const _InlineFailure({
    required this.message,
    required this.retryKey,
    this.onRetry,
    super.key,
  });

  final String message;
  final Key retryKey;
  final Future<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: const Color(0xFFFFF3F1),
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
        child: Row(
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 20,
              color: Color(0xFF8B2E26),
            ),
            const SizedBox(width: 8),
            Expanded(child: Text(message)),
            if (onRetry case final callback?)
              TextButton(
                key: retryKey,
                onPressed: callback,
                child: const Text('重试'),
              ),
          ],
        ),
      ),
    );
  }
}

class _PreviewSceneCard extends StatelessWidget {
  const _PreviewSceneCard({
    required this.scene,
    required this.selected,
    required this.enabled,
    required this.onPressed,
  });

  final AgentScene scene;
  final bool selected;
  final bool enabled;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: Colors.white,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: BorderSide(
          color: selected ? const Color(0xFF303136) : const Color(0xFFE8E8E4),
          width: selected ? 1.5 : 1,
        ),
      ),
      child: InkWell(
        key: Key('scene-${scene.id}'),
        onTap: enabled ? onPressed : null,
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 48,
                height: 48,
                decoration: BoxDecoration(
                  color: const Color(0xFFE8E8E5),
                  borderRadius: BorderRadius.circular(15),
                ),
                child: Icon(
                  selected ? Icons.check_rounded : Icons.work_outline_rounded,
                  color: const Color(0xFF4F5054),
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      scene.title,
                      style: const TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Text(
                      scene.description,
                      style: const TextStyle(
                        color: Color(0xFF696B73),
                        height: 1.4,
                      ),
                    ),
                  ],
                ),
              ),
              const Icon(Icons.arrow_forward_ios_rounded, size: 16),
            ],
          ),
        ),
      ),
    );
  }
}
