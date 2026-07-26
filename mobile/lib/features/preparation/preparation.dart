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
          '按目标选择训练专题。当前先把英文面试做深，后续专题会沿用同一套练习与复盘能力。',
          style: TextStyle(
            color: Color(0xFF696B73),
            fontSize: 14,
            height: 1.45,
          ),
        ),
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
          for (final scenario in controller.scenarios) ...[
            _CatalogScenarioCard(
              scenario: scenario,
              onPressed: widget.onOpenJobPreparation == null
                  ? () => controller.selectScenario(scenario)
                  : widget.onOpenJobPreparation!,
            ),
            const SizedBox(height: 12),
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
                    ? '本地 UI Mock；练习结果不会写入正式服务。'
                    : '练习目录未注入，当前页面不可用于正式运行。'
              : '服务端场景与语音契约尚未开放，当前仅提供 Agent 文本对话。',
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
            label: const Text('全部场景'),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          scenario.name,
          key: const Key('preparation-scenario-title'),
          style: const TextStyle(fontSize: 30, fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text(
          '这是同一个 SpeakUp Agent 中的重点练习内容包。',
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
          const SizedBox(height: 28),
          const Text(
            '选择面试官视角',
            style: TextStyle(fontSize: 21, fontWeight: FontWeight.w800),
          ),
          const SizedBox(height: 6),
          const Text(
            '每个视角都可以独立练习，排列顺序不代表固定招聘阶段。',
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
          if (controller.hasCompleteSelection) ...[
            const SizedBox(height: 18),
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

class _CatalogScenarioCard extends StatelessWidget {
  const _CatalogScenarioCard({required this.scenario, required this.onPressed});

  final PreparationScenario scenario;
  final VoidCallback onPressed;

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
                child: const Icon(
                  Icons.work_outline_rounded,
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
                    const Text(
                      '从岗位与 JD 开始，生成专属练习计划',
                      style: TextStyle(color: Color(0xFF696B73), height: 1.4),
                    ),
                    const SizedBox(height: 9),
                    const _AvailableTopicLabel(),
                  ],
                ),
              ),
              const Icon(
                Icons.arrow_forward_ios_rounded,
                size: 15,
                color: Color(0xFF777980),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _AvailableTopicLabel extends StatelessWidget {
  const _AvailableTopicLabel();

  @override
  Widget build(BuildContext context) {
    return const Align(
      alignment: Alignment.centerLeft,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: Color(0xFFE7ECE6),
          borderRadius: BorderRadius.all(Radius.circular(99)),
        ),
        child: Padding(
          padding: EdgeInsets.symmetric(horizontal: 9, vertical: 4),
          child: Text(
            '可用',
            style: TextStyle(
              color: Color(0xFF405846),
              fontSize: 12,
              fontWeight: FontWeight.w700,
            ),
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
              config.jobTitle,
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 8),
            Text(
              config.jobDescription,
              style: const TextStyle(color: Color(0xFF5F6168), height: 1.45),
            ),
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
            const Text('准备开始练习', style: TextStyle(fontWeight: FontWeight.w800)),
            const SizedBox(height: 6),
            const Text(
              '请补充真实背景和练习目标。这里只创建本次练习上下文，不会从昵称或历史消息猜测。',
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
                labelText: '你的背景与本次练习目标（必填）',
                hintText: '例如：你的岗位、经历，以及这次最想练习的表达。',
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
              child: Text(controller.isStarting ? '正在创建练习' : '开始语音练习'),
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
