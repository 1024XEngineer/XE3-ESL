/// Preparation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_design.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

const _jobInterviewScenarioId = 'scn_programmer_interview';
const _interviewFullScenarioId = _jobInterviewScenarioId;
const _ieltsFullScenarioId = 'scn_ielts_speaking_full';
const _ieltsScenarioIds = <String>{
  'scn_ielts_speaking_part_1',
  'scn_ielts_speaking_part_2',
  'scn_ielts_speaking_part_3',
  _ieltsFullScenarioId,
};
const _hiddenCatalogScenarioIds = <String>{
  'scn_interview_custom',
  'scn_speaking_exam_custom',
  'scn_workplace_custom',
  'scn_daily_custom',
};

enum _PracticeHub { interview, ielts, roleplay }

enum _ExistingPracticeAction { cancel, continuePractice, replace }

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
  _PracticeHub? _selectedHub;

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
    var replaceCurrentPractice = false;
    if (launch.hasResumablePractice) {
      final action = await _chooseExistingPracticeAction(
        currentTitle: launch.resumablePracticeTitle,
        nextTitle: scenario.name,
      );
      if (!mounted || action == _ExistingPracticeAction.cancel) {
        return;
      }
      if (action == _ExistingPracticeAction.continuePractice) {
        await _continueCurrentPractice();
        return;
      }
      replaceCurrentPractice = true;
    }
    final started = await launch.start(
      PreparationLaunchSelection.fromCatalog(
        scenario: scenario,
        config: config,
        role: role,
        option: option,
      ),
      replaceCurrentPractice: replaceCurrentPractice,
    );
    if (started && mounted) {
      catalog.showScenarioList();
      setState(() => _selectedHub = null);
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _retryLaunch() async {
    final started = await widget.launchController?.retry() ?? false;
    if (started && mounted) {
      widget.preparationController?.showScenarioList();
      setState(() => _selectedHub = null);
      widget.onPracticeStarted?.call();
    }
  }

  Future<_ExistingPracticeAction> _chooseExistingPracticeAction({
    required String? currentTitle,
    required String nextTitle,
  }) async {
    return await showDialog<_ExistingPracticeAction>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('你还有一项练习未完成'),
            content: Text(
              '当前练习：${currentTitle ?? '上次练习'}\n'
              '如果开始“$nextTitle”，当前练习会提前结束。',
            ),
            actions: [
              TextButton(
                onPressed: () =>
                    Navigator.of(context).pop(_ExistingPracticeAction.cancel),
                child: const Text('取消'),
              ),
              TextButton(
                key: const Key('continue-existing-practice'),
                onPressed: () => Navigator.of(
                  context,
                ).pop(_ExistingPracticeAction.continuePractice),
                child: const Text('继续上次练习'),
              ),
              FilledButton(
                key: const Key('replace-existing-practice'),
                onPressed: () =>
                    Navigator.of(context).pop(_ExistingPracticeAction.replace),
                child: const Text('结束并开始新的'),
              ),
            ],
          ),
        ) ??
        _ExistingPracticeAction.cancel;
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

  Future<void> _continueCurrentPractice() async {
    final launch = widget.launchController;
    if (launch?.hasResumablePractice ?? false) {
      final resumed = await launch!.resumeCurrentPractice();
      if (resumed && mounted) {
        widget.preparationController?.showScenarioList();
        setState(() => _selectedHub = null);
        widget.onPracticeStarted?.call();
      }
      return;
    }
    final agent = widget.agentController;
    if (agent?.activeMatter == null) {
      return;
    }
    if (agent?.hasActivePractice ?? false) {
      widget.onPracticeStarted?.call();
    } else if (widget.onSceneSelected case final callback?) {
      callback();
    } else if (widget.showBackButton) {
      Navigator.of(context).maybePop();
    }
  }

  bool get _canContinueCurrentPractice {
    if (widget.launchController?.hasResumablePractice ?? false) {
      return widget.onPracticeStarted != null;
    }
    final agent = widget.agentController;
    if (agent?.activeMatter == null) {
      return false;
    }
    if (agent?.hasActivePractice ?? false) {
      return widget.onPracticeStarted != null;
    }
    return widget.onSceneSelected != null || widget.showBackButton;
  }

  void _handleBack(PreparationController? controller) {
    if (widget.launchController?.isSelectionLocked ?? false) {
      return;
    }
    if (controller?.selectedScenario != null) {
      widget.launchController?.selectionChanged();
      controller?.showScenarioList();
      return;
    }
    if (_selectedHub != null) {
      setState(() => _selectedHub = null);
      return;
    }
    Navigator.of(context).maybePop();
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.preparationController;
    final launchLocked = widget.launchController?.isSelectionLocked ?? false;
    final hasInternalRoute =
        controller?.selectedScenario != null || _selectedHub != null;
    return PopScope<void>(
      canPop: !launchLocked && !hasInternalRoute,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          _handleBack(controller);
        }
      },
      child: Scaffold(
        key: const Key('scenes-page'),
        backgroundColor: PreparationDesign.canvas,
        appBar: widget.showBackButton
            ? AppBar(
                backgroundColor: PreparationDesign.canvas,
                surfaceTintColor: Colors.transparent,
                elevation: 0,
                scrolledUnderElevation: 0,
                leading: IconButton(
                  key: const Key('preparation-route-back-button'),
                  tooltip: '返回',
                  onPressed: launchLocked
                      ? null
                      : () => _handleBack(controller),
                  icon: const Icon(Icons.arrow_back_rounded),
                ),
              )
            : null,
        body: SafeArea(
          bottom: false,
          child: PreparationContentWidth(
            child: controller == null
                ? _buildPreview()
                : _buildCatalog(controller),
          ),
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
        backLabel: _selectedHub == null
            ? _scenarioFamilyLabel(selectedScenario.type)
            : _practiceHubLabel(_selectedHub!),
        launchController: widget.launchController,
        backgroundController: _backgroundController,
        hasAgentContext:
            widget.launchController?.workspaceController != null ||
            widget.agentController?.threadId != null,
        hasPrimaryNavigation: !widget.showBackButton,
        onStart: _startPractice,
        onRetry: _retryLaunch,
      );
    }
    final selectedHub = _selectedHub;
    if (selectedHub != null) {
      return _buildHub(controller, selectedHub);
    }
    return ListView(
      key: const Key('preparation-catalog-list'),
      primary: false,
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: !widget.showBackButton,
        top: 18,
      ),
      children: [
        const Text(
          '场景练习',
          key: Key('training-center-title'),
          style: PreparationDesign.pageTitle,
        ),
        const SizedBox(height: 8),
        const Text(
          '今天想练什么？',
          key: Key('practice-hub-page-title'),
          style: PreparationDesign.body,
        ),
        if ((widget.launchController?.hasResumablePractice ?? false) ||
            widget.agentController?.activeMatter != null) ...[
          const SizedBox(height: 20),
          _PracticeContinuation(
            sceneTitle:
                widget.launchController?.resumablePracticeTitle ??
                widget.agentController?.activeMatter?.scene.title,
            hasActivePractice:
                (widget.launchController?.hasResumablePractice ?? false) ||
                (widget.agentController?.hasActivePractice ?? false),
            onPressed: _canContinueCurrentPractice
                ? () => unawaited(_continueCurrentPractice())
                : null,
          ),
        ],
        if (widget.launchController?.workspaceErrorMessage case final message?)
          Padding(
            padding: const EdgeInsets.only(top: 10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  message,
                  key: const Key('practice-workspace-error'),
                  style: const TextStyle(color: Color(0xFF9A332A), height: 1.4),
                ),
                if (widget.launchController?.canRetryWorkspaceActivation ??
                    false)
                  TextButton(
                    key: const Key('retry-practice-workspace-activation'),
                    onPressed: () => unawaited(
                      widget.launchController!.retryWorkspaceActivation(),
                    ),
                    child: const Text('重试读取练习记录'),
                  ),
              ],
            ),
          ),
        const SizedBox(height: 24),
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
        else ...[
          _PracticeHubEntry(
            key: const Key('practice-hub-interview'),
            title: '英文面试',
            description: '模拟面试与轮次专项练习',
            icon: Icons.work_outline_rounded,
            accentColor: PreparationDesign.interview,
            tintColor: PreparationDesign.interviewTint,
            assetPath: 'assets/images/scenes/interview-hero.jpg',
            onPressed: () =>
                setState(() => _selectedHub = _PracticeHub.interview),
          ),
          const SizedBox(height: 12),
          _PracticeHubEntry(
            key: const Key('practice-hub-exam'),
            title: 'IELTS 口语',
            description: 'Part 1、2、3 与完整模考',
            icon: Icons.school_outlined,
            accentColor: PreparationDesign.ielts,
            tintColor: PreparationDesign.ieltsTint,
            assetPath: 'assets/images/scenes/ielts-hero.jpg',
            onPressed: () => setState(() => _selectedHub = _PracticeHub.ielts),
          ),
          const SizedBox(height: 12),
          _PracticeHubEntry(
            key: const Key('practice-hub-roleplay'),
            title: 'AI 数字人陪练',
            description: '工作、旅行与日常真实对话',
            icon: Icons.record_voice_over_outlined,
            accentColor: PreparationDesign.roleplay,
            tintColor: PreparationDesign.roleplayTint,
            assetPath: 'assets/images/scenes/daily-tutor.jpg',
            onPressed: () =>
                setState(() => _selectedHub = _PracticeHub.roleplay),
          ),
        ],
      ],
    );
  }

  Widget _buildHub(PreparationController controller, _PracticeHub hub) {
    final scenarios = _scenariosForHub(controller.scenarios, hub);
    return ListView(
      key: Key('preparation-hub-list-${hub.name}'),
      primary: false,
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: !widget.showBackButton,
        top: 8,
      ),
      children: [
        Align(
          alignment: Alignment.centerLeft,
          child: IconButton(
            key: const Key('preparation-back-to-families'),
            tooltip: '返回场景练习',
            onPressed: () => setState(() => _selectedHub = null),
            icon: const Icon(Icons.arrow_back_rounded),
            color: PreparationDesign.ink,
            style: IconButton.styleFrom(
              backgroundColor: PreparationDesign.surface,
              side: const BorderSide(color: PreparationDesign.border),
            ),
          ),
        ),
        const SizedBox(height: 12),
        if (hub == _PracticeHub.interview)
          _InterviewHub(
            scenarios: scenarios,
            onScenarioPressed: controller.selectScenario,
            onOpenJobPreparation: widget.onOpenJobPreparation,
          )
        else if (hub == _PracticeHub.ielts)
          _IeltsHub(
            scenarios: scenarios,
            onScenarioPressed: controller.selectScenario,
          )
        else
          _RoleplayHub(
            scenarios: scenarios,
            onScenarioPressed: controller.selectScenario,
          ),
      ],
    );
  }

  Widget _buildPreview() {
    final controller = widget.agentController;
    final practiceAvailable = controller?.supportsPracticeFlow ?? true;
    final selectedHub = _selectedHub;
    if (selectedHub != null) {
      return ListView(
        key: Key('preparation-hub-list-${selectedHub.name}'),
        primary: false,
        padding: PreparationDesign.pagePadding(
          context,
          hasPrimaryNavigation: !widget.showBackButton,
          top: 8,
        ),
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: IconButton(
              key: const Key('preparation-back-to-families'),
              tooltip: '返回场景练习',
              onPressed: () => setState(() => _selectedHub = null),
              icon: const Icon(Icons.arrow_back_rounded),
              color: PreparationDesign.ink,
              style: IconButton.styleFrom(
                backgroundColor: PreparationDesign.surface,
                side: const BorderSide(color: PreparationDesign.border),
              ),
            ),
          ),
          const SizedBox(height: 12),
          _HubHeader(
            title: _practiceHubLabel(selectedHub),
            description: selectedHub == _PracticeHub.interview
                ? '选择一项面试能力开始练习。'
                : '该模块需要连接真实场景目录后使用。',
            titleKey: Key('practice-hub-title-${selectedHub.name}'),
          ),
          const SizedBox(height: 24),
          if (controller?.isBusy ?? false)
            const LinearProgressIndicator(
              key: Key('scene-selection-progress'),
              minHeight: 2,
            ),
          if (controller?.errorMessage case final message?) ...[
            _InlineFailure(
              key: const Key('scene-operation-error'),
              message: message,
              retryKey: const Key('scene-retry-operation'),
              onRetry: controller?.canRetry ?? false
                  ? _retryPreviewOperation
                  : null,
            ),
            const SizedBox(height: 14),
          ],
          if (selectedHub == _PracticeHub.interview)
            for (final scene in agentScenes) ...[
              _PreviewSceneCard(
                scene: scene,
                selected: controller?.scene?.id == scene.id,
                enabled:
                    practiceAvailable && (controller?.canSelectScene ?? false),
                onPressed: () => _selectPreviewScene(scene),
              ),
              const SizedBox(height: 12),
            ]
          else
            const _HubEmpty(message: '连接本地服务后即可查看真实练习。'),
        ],
      );
    }
    return ListView(
      primary: false,
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: !widget.showBackButton,
        top: 18,
      ),
      children: [
        const Text(
          '场景练习',
          key: Key('training-center-title'),
          style: PreparationDesign.pageTitle,
        ),
        const SizedBox(height: 8),
        Text(
          practiceAvailable
              ? widget.previewMode
                    ? '今天想练什么？'
                    : '练习内容暂时无法加载，请稍后重试。'
              : '练习功能正在准备中，目前可以先使用文字陪练。',
          key: const Key('practice-availability-message'),
          style: PreparationDesign.body,
        ),
        const SizedBox(height: 24),
        _PracticeHubEntry(
          key: const Key('practice-hub-interview'),
          title: '英文面试',
          description: '模拟面试与轮次专项练习',
          icon: Icons.work_outline_rounded,
          accentColor: PreparationDesign.interview,
          tintColor: PreparationDesign.interviewTint,
          assetPath: 'assets/images/scenes/interview-hero.jpg',
          onPressed: () =>
              setState(() => _selectedHub = _PracticeHub.interview),
        ),
        const SizedBox(height: 12),
        _PracticeHubEntry(
          key: const Key('practice-hub-exam'),
          title: 'IELTS 口语',
          description: 'Part 1、2、3 与完整模考',
          icon: Icons.school_outlined,
          accentColor: PreparationDesign.ielts,
          tintColor: PreparationDesign.ieltsTint,
          assetPath: 'assets/images/scenes/ielts-hero.jpg',
          onPressed: () => setState(() => _selectedHub = _PracticeHub.ielts),
        ),
        const SizedBox(height: 12),
        _PracticeHubEntry(
          key: const Key('practice-hub-roleplay'),
          title: 'AI 数字人陪练',
          description: '工作、旅行与日常真实对话',
          icon: Icons.record_voice_over_outlined,
          accentColor: PreparationDesign.roleplay,
          tintColor: PreparationDesign.roleplayTint,
          assetPath: 'assets/images/scenes/daily-tutor.jpg',
          onPressed: () => setState(() => _selectedHub = _PracticeHub.roleplay),
        ),
        if (!practiceAvailable) ...[
          const SizedBox(height: 16),
          const _HubEmpty(message: '当前可以先使用文字陪练。'),
        ],
      ],
    );
  }
}

class _ScenarioDetailView extends StatelessWidget {
  const _ScenarioDetailView({
    required this.controller,
    required this.scenario,
    required this.backLabel,
    required this.launchController,
    required this.backgroundController,
    required this.hasAgentContext,
    required this.hasPrimaryNavigation,
    required this.onStart,
    required this.onRetry,
  });

  final PreparationController controller;
  final PreparationScenario scenario;
  final String backLabel;
  final PreparationLaunchController? launchController;
  final TextEditingController? backgroundController;
  final bool hasAgentContext;
  final bool hasPrimaryNavigation;
  final Future<void> Function() onStart;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final detail = controller.detail;
    final selectedRole = controller.selectedRole;
    final launchLocked = launchController?.isSelectionLocked ?? false;
    return ListView(
      key: const Key('preparation-scenario-detail'),
      primary: false,
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: hasPrimaryNavigation,
        top: 8,
      ),
      children: [
        Align(
          alignment: Alignment.centerLeft,
          child: IconButton(
            key: const Key('preparation-back-to-catalog'),
            tooltip: '返回$backLabel',
            onPressed: launchLocked
                ? null
                : () {
                    launchController?.selectionChanged();
                    controller.showScenarioList();
                  },
            icon: const Icon(Icons.arrow_back_rounded),
            color: PreparationDesign.ink,
            style: IconButton.styleFrom(
              backgroundColor: PreparationDesign.surface,
              side: const BorderSide(color: PreparationDesign.border),
            ),
          ),
        ),
        const SizedBox(height: 12),
        Text(
          backLabel,
          style: PreparationDesign.label.copyWith(
            color: PreparationDesign.secondary,
          ),
        ),
        const SizedBox(height: 6),
        Text(
          scenario.name,
          key: const Key('preparation-scenario-title'),
          style: PreparationDesign.pageTitle,
        ),
        const SizedBox(height: 8),
        const Text('确认练习目标与角色后即可开始。', style: PreparationDesign.body),
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
            const Text('选择对话角色', style: PreparationDesign.sectionTitle),
            const SizedBox(height: 6),
            const Text(
              'AI 会按所选角色和场景目标推进对话。',
              key: Key('preparation-role-guidance'),
              style: PreparationDesign.body,
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
              const Text('选择练习方式', style: PreparationDesign.sectionTitle),
              const SizedBox(height: 6),
              const Text(
                '完整模拟和专项练习都围绕当前选择的视角进行。',
                style: PreparationDesign.body,
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
            const SizedBox(height: 22),
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

class _PracticeContinuation extends StatelessWidget {
  const _PracticeContinuation({
    required this.sceneTitle,
    required this.hasActivePractice,
    required this.onPressed,
  });

  final String? sceneTitle;
  final bool hasActivePractice;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final title = sceneTitle;
    final hasCurrentPractice = title != null;
    final label = hasCurrentPractice
        ? hasActivePractice
              ? '继续练习'
              : '继续当前话题'
        : '最近练习';
    final description = title ?? '完成一次练习后，这里会保留你的进度。';
    final content = Padding(
      padding: const EdgeInsets.symmetric(vertical: 14),
      child: Row(
        children: [
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: const Color(0xFFE4E4DF),
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Icon(
              Icons.history_rounded,
              size: 20,
              color: Color(0xFF4F5054),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: const TextStyle(
                    color: Color(0xFF696B73),
                    fontSize: 12,
                    fontWeight: FontWeight.w800,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  description,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 14, height: 1.35),
                ),
              ],
            ),
          ),
          if (onPressed != null) ...[
            const SizedBox(width: 10),
            const Icon(Icons.arrow_forward_rounded, size: 20),
          ],
        ],
      ),
    );
    return DecoratedBox(
      key: const Key('practice-continuation'),
      decoration: const BoxDecoration(
        border: Border.symmetric(
          horizontal: BorderSide(color: Color(0xFFD8D8D2)),
        ),
      ),
      child: onPressed == null
          ? content
          : Semantics(
              button: true,
              label: '$label，$description',
              onTap: onPressed,
              excludeSemantics: true,
              child: InkWell(onTap: onPressed, child: content),
            ),
    );
  }
}

class _PracticeHubEntry extends StatelessWidget {
  const _PracticeHubEntry({
    required this.title,
    required this.description,
    required this.icon,
    required this.accentColor,
    required this.tintColor,
    required this.assetPath,
    required this.onPressed,
    super.key,
  });

  final String title;
  final String description;
  final IconData icon;
  final Color accentColor;
  final Color tintColor;
  final String assetPath;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    final compact = MediaQuery.sizeOf(context).width < 360 || textScale > 1.35;
    final mediaWidth = compact ? 88.0 : 118.0;
    return Material(
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Semantics(
        button: true,
        label: '$title。$description',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          onTap: onPressed,
          child: IntrinsicHeight(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Expanded(
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(minHeight: 112),
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(18, 17, 12, 16),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: [
                          Text(
                            title,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: PreparationDesign.sectionTitle,
                          ),
                          const SizedBox(height: 6),
                          Text(
                            description,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: PreparationDesign.label.copyWith(
                              color: PreparationDesign.secondary,
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                          const SizedBox(height: 10),
                          Row(
                            children: [
                              Text(
                                '进入',
                                style: PreparationDesign.label.copyWith(
                                  color: accentColor,
                                ),
                              ),
                              const SizedBox(width: 4),
                              Icon(
                                Icons.arrow_forward_rounded,
                                size: 18,
                                color: accentColor,
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
                SizedBox(
                  width: mediaWidth,
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      ColoredBox(color: tintColor),
                      Image.asset(
                        assetPath,
                        fit: BoxFit.cover,
                        alignment: Alignment.topCenter,
                        errorBuilder: (_, _, _) => ColoredBox(
                          color: tintColor,
                          child: Icon(icon, color: accentColor, size: 36),
                        ),
                      ),
                      Positioned(
                        top: 10,
                        right: 10,
                        child: Container(
                          width: 36,
                          height: 36,
                          decoration: BoxDecoration(
                            color: Colors.white.withValues(alpha: 0.88),
                            shape: BoxShape.circle,
                          ),
                          child: Icon(icon, color: accentColor, size: 19),
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

class _AvatarSlotPreview extends StatelessWidget {
  const _AvatarSlotPreview({required this.size});

  final double size;

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: SizedBox.square(
        dimension: size,
        child: DecoratedBox(
          decoration: const BoxDecoration(
            color: PreparationDesign.roleplayTint,
            shape: BoxShape.circle,
          ),
          child: Icon(
            Icons.record_voice_over_outlined,
            size: size * 0.46,
            color: PreparationDesign.roleplay,
          ),
        ),
      ),
    );
  }
}

class _HubHeader extends StatelessWidget {
  const _HubHeader({
    required this.title,
    required this.description,
    required this.titleKey,
  });

  final String title;
  final String description;
  final Key titleKey;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Semantics(
          header: true,
          container: true,
          child: Text(title, key: titleKey, style: PreparationDesign.pageTitle),
        ),
        const SizedBox(height: 8),
        Text(description, style: PreparationDesign.body),
      ],
    );
  }
}

class _InterviewHub extends StatelessWidget {
  const _InterviewHub({
    required this.scenarios,
    required this.onScenarioPressed,
    required this.onOpenJobPreparation,
  });

  final List<PreparationScenario> scenarios;
  final ValueChanged<PreparationScenario> onScenarioPressed;
  final VoidCallback? onOpenJobPreparation;

  @override
  Widget build(BuildContext context) {
    final fullScenario = _scenarioById(scenarios, _interviewFullScenarioId);
    const knownScenarioIds = <String>{
      _interviewFullScenarioId,
      'scn_interview_self_introduction',
      'scn_interview_recruiter_screening',
      'scn_interview_behavioral',
      'scn_interview_system_design_spoken',
      'scn_interview_hiring_manager',
    };
    final dedicatedScenarios = scenarios
        .where(
          (scenario) =>
              scenario.id != 'scn_interview_custom' &&
              scenario.id != _interviewFullScenarioId,
        )
        .toList(growable: false);
    final additionalScenarios = scenarios
        .where(
          (scenario) =>
              !knownScenarioIds.contains(scenario.id) &&
              !_hiddenCatalogScenarioIds.contains(scenario.id),
        )
        .toList(growable: false);
    final modes =
        <
              ({
                String id,
                String title,
                String caption,
                IconData icon,
                List<PreparationScenario> scenarios,
              })
            >[
              (
                id: 'hr',
                title: '招聘初筛',
                caption: '自我介绍与求职动机',
                icon: Icons.badge_outlined,
                scenarios: _scenariosByIds(scenarios, const [
                  'scn_interview_recruiter_screening',
                  'scn_interview_self_introduction',
                ]),
              ),
              (
                id: 'behavioral',
                title: '行为面试',
                caption: '经历、行动与结果',
                icon: Icons.forum_outlined,
                scenarios: _scenariosByIds(scenarios, const [
                  'scn_interview_behavioral',
                ]),
              ),
              (
                id: 'professional',
                title: '岗位专业面试',
                caption: '项目与专业表达',
                icon: Icons.laptop_mac_outlined,
                scenarios: [
                  if (onOpenJobPreparation != null)
                    ..._scenariosByIds(scenarios, const [
                      _jobInterviewScenarioId,
                    ]),
                  ..._scenariosByIds(scenarios, const [
                    'scn_interview_system_design_spoken',
                  ]),
                  ...additionalScenarios,
                ],
              ),
              (
                id: 'manager',
                title: 'Hiring Manager',
                caption: '岗位匹配与业务影响',
                icon: Icons.supervisor_account_outlined,
                scenarios: _scenariosByIds(scenarios, const [
                  'scn_interview_hiring_manager',
                ]),
              ),
            ]
            .where((mode) => mode.scenarios.isNotEmpty)
            .toList(growable: false);
    if (fullScenario == null && dedicatedScenarios.isEmpty && modes.isEmpty) {
      return const _HubEmpty(message: '当前没有可用的英文面试练习。');
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _HubHeader(
          title: '英文面试',
          description: '准备一整轮，或只练最需要的一关。',
          titleKey: Key('practice-hub-title-interview'),
        ),
        const SizedBox(height: 20),
        if (onOpenJobPreparation != null || fullScenario != null) ...[
          _FeaturedScenario(
            key: const Key('interview-mode-full'),
            eyebrow: '推荐',
            title: onOpenJobPreparation == null ? '开始岗位专业面试' : '开始模拟面试',
            description: onOpenJobPreparation == null
                ? fullScenario?.summary ?? '围绕项目与专业表达进入一轮练习。'
                : '带上岗位信息，问题会更贴近真实面试。',
            actionLabel: onOpenJobPreparation == null ? '直接开始' : '使用 JD 开始',
            actionKey: onOpenJobPreparation == null
                ? fullScenario == null
                      ? null
                      : Key('catalog-scenario-${fullScenario.id}')
                : const Key('open-job-preparation'),
            icon: Icons.play_arrow_rounded,
            color: const Color(0xFF20252A),
            foregroundColor: Colors.white,
            assetPath: 'assets/images/scenes/interview-hero.jpg',
            onPressed:
                onOpenJobPreparation ??
                (fullScenario == null
                    ? null
                    : () => onScenarioPressed(fullScenario)),
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
                scenarioCount: mode.scenarios.length,
                onPressed: () => _openScenarioPicker(
                  context,
                  title: mode.title,
                  scenarios: mode.scenarios,
                  onScenarioPressed: onScenarioPressed,
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _IeltsHub extends StatelessWidget {
  const _IeltsHub({required this.scenarios, required this.onScenarioPressed});

  final List<PreparationScenario> scenarios;
  final ValueChanged<PreparationScenario> onScenarioPressed;

  @override
  Widget build(BuildContext context) {
    final fullScenario = _scenarioById(scenarios, _ieltsFullScenarioId);
    final partScenarios = _scenariosByIds(scenarios, const [
      'scn_ielts_speaking_part_1',
      'scn_ielts_speaking_part_2',
      'scn_ielts_speaking_part_3',
    ]);
    if (fullScenario == null && partScenarios.isEmpty) {
      return const _HubEmpty(message: '当前没有可用的 IELTS 口语练习。');
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _HubHeader(
          title: 'IELTS 口语',
          description: '按真实考试顺序练 Part 1、Part 2、Part 3。',
          titleKey: Key('practice-hub-title-ielts'),
        ),
        const SizedBox(height: 20),
        if (fullScenario != null) ...[
          _FeaturedScenario(
            key: const Key('ielts-mode-full'),
            eyebrow: '推荐',
            title: '一次完成三个 Part',
            description: '连续完成整套口语流程，中途不打断。',
            actionLabel: '开始完整模考',
            icon: Icons.timer_outlined,
            color: PreparationDesign.ielts,
            foregroundColor: Colors.white,
            assetPath: 'assets/images/scenes/ielts-hero.jpg',
            onPressed: () => onScenarioPressed(fullScenario),
          ),
          const SizedBox(height: 28),
        ],
        if (partScenarios.isNotEmpty) ...[
          const Text('分段练习', style: PreparationDesign.sectionTitle),
          const SizedBox(height: 12),
          for (var index = 0; index < partScenarios.length; index++)
            _IeltsPartStep(
              scenario: partScenarios[index],
              partNumber: _ieltsPartNumber(partScenarios[index].id),
              label: _ieltsPartLabel(partScenarios[index].id),
              isLast: index == partScenarios.length - 1,
              onPressed: () => onScenarioPressed(partScenarios[index]),
            ),
        ],
      ],
    );
  }
}

enum _RoleplayFilter { recommended, workplace, travel, daily }

class _RoleplayHub extends StatefulWidget {
  const _RoleplayHub({
    required this.scenarios,
    required this.onScenarioPressed,
  });

  final List<PreparationScenario> scenarios;
  final ValueChanged<PreparationScenario> onScenarioPressed;

  @override
  State<_RoleplayHub> createState() => _RoleplayHubState();
}

class _RoleplayHubState extends State<_RoleplayHub> {
  static const _travelScenarioIds = <String>{
    'scn_daily_airport_transport',
    'scn_daily_hotel_checkin_issue',
  };
  static const _recommendedScenarioIds = <String>[
    'scn_workplace_progress_risk_update',
    'scn_workplace_meeting_disagreement',
    'scn_daily_restaurant_ordering',
    'scn_daily_airport_transport',
    'scn_daily_hotel_checkin_issue',
    'scn_daily_small_talk',
  ];

  _RoleplayFilter _filter = _RoleplayFilter.recommended;

  List<PreparationScenario> get _visibleScenarios {
    final available = widget.scenarios
        .where((scenario) => !_hiddenCatalogScenarioIds.contains(scenario.id))
        .toList(growable: false);
    switch (_filter) {
      case _RoleplayFilter.recommended:
        final recommended = _scenariosByIds(
          available,
          _recommendedScenarioIds,
        ).toList();
        for (final scenario in available) {
          if (recommended.length >= 6) {
            break;
          }
          if (!recommended.any((item) => item.id == scenario.id)) {
            recommended.add(scenario);
          }
        }
        return recommended;
      case _RoleplayFilter.workplace:
        return available
            .where((scenario) => scenario.type == 'WORKPLACE')
            .toList(growable: false);
      case _RoleplayFilter.travel:
        return available
            .where((scenario) => _travelScenarioIds.contains(scenario.id))
            .toList(growable: false);
      case _RoleplayFilter.daily:
        return available
            .where(
              (scenario) =>
                  scenario.type == 'DAILY' &&
                  !_travelScenarioIds.contains(scenario.id),
            )
            .toList(growable: false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.scenarios.isEmpty) {
      return const _HubEmpty(message: '当前没有可用的情景陪练。');
    }
    final visibleScenarios = _visibleScenarios;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _RoleplayModuleHeader(),
        const SizedBox(height: 20),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final filter in _RoleplayFilter.values)
              ChoiceChip(
                key: Key('roleplay-filter-${filter.name}'),
                label: Text(_roleplayFilterLabel(filter)),
                selected: _filter == filter,
                onSelected: (_) => setState(() => _filter = filter),
                showCheckmark: false,
                visualDensity: VisualDensity.compact,
                padding: const EdgeInsets.symmetric(horizontal: 10),
                side: BorderSide(
                  color: _filter == filter
                      ? PreparationDesign.roleplay
                      : PreparationDesign.border,
                ),
                selectedColor: PreparationDesign.roleplay,
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
        if (visibleScenarios.isEmpty)
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
          _RoleplayScenarioGrid(
            scenarios: visibleScenarios,
            includeCustom: _filter == _RoleplayFilter.recommended,
            onScenarioPressed: widget.onScenarioPressed,
          ),
      ],
    );
  }
}

class _RoleplayModuleHeader extends StatelessWidget {
  const _RoleplayModuleHeader();

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(
                header: true,
                container: true,
                child: const Text(
                  'AI 数字人陪练',
                  key: Key('practice-hub-title-roleplay'),
                  style: PreparationDesign.pageTitle,
                ),
              ),
              const SizedBox(height: 8),
              const Text('选一个真实场景，马上开口。', style: PreparationDesign.body),
            ],
          ),
        ),
        const SizedBox(width: 16),
        const _AvatarSlotPreview(size: 58),
      ],
    );
  }
}

class _FeaturedScenario extends StatelessWidget {
  const _FeaturedScenario({
    required this.eyebrow,
    required this.title,
    required this.description,
    required this.actionLabel,
    required this.icon,
    required this.color,
    required this.foregroundColor,
    required this.assetPath,
    required this.onPressed,
    this.actionKey,
    super.key,
  });

  final String eyebrow;
  final String title;
  final String description;
  final String actionLabel;
  final IconData icon;
  final Color color;
  final Color foregroundColor;
  final String assetPath;
  final VoidCallback? onPressed;
  final Key? actionKey;

  @override
  Widget build(BuildContext context) {
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    final cardHeight = 184 + (textScale - 1).clamp(0, 2).toDouble() * 190;
    return Material(
      color: color,
      clipBehavior: Clip.antiAlias,
      borderRadius: BorderRadius.circular(PreparationDesign.radiusHero),
      child: Semantics(
        button: onPressed != null,
        enabled: onPressed != null,
        label: '$title。$description$actionLabel',
        onTap: onPressed,
        excludeSemantics: true,
        child: InkWell(
          key: actionKey,
          onTap: onPressed,
          child: SizedBox(
            height: cardHeight,
            child: Stack(
              fit: StackFit.expand,
              children: [
                Image.asset(
                  assetPath,
                  fit: BoxFit.cover,
                  alignment: Alignment.topCenter,
                  errorBuilder: (_, _, _) => ColoredBox(color: color),
                ),
                ColoredBox(color: color.withValues(alpha: 0.64)),
                Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      Row(
                        children: [
                          Container(
                            width: 32,
                            height: 32,
                            decoration: BoxDecoration(
                              color: Colors.white.withValues(alpha: 0.18),
                              shape: BoxShape.circle,
                            ),
                            child: Icon(icon, color: foregroundColor, size: 19),
                          ),
                          const SizedBox(width: 10),
                          Text(
                            eyebrow,
                            style: PreparationDesign.label.copyWith(
                              color: foregroundColor.withValues(alpha: 0.86),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 12),
                      Text(
                        title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: PreparationDesign.sectionTitle.copyWith(
                          color: foregroundColor,
                          fontSize: 22,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: PreparationDesign.label.copyWith(
                          color: foregroundColor.withValues(alpha: 0.86),
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                      const SizedBox(height: 12),
                      Align(
                        alignment: Alignment.centerLeft,
                        child: Container(
                          constraints: const BoxConstraints(minHeight: 36),
                          padding: const EdgeInsets.symmetric(
                            horizontal: 14,
                            vertical: 8,
                          ),
                          decoration: BoxDecoration(
                            color: Colors.white.withValues(alpha: 0.92),
                            borderRadius: BorderRadius.circular(999),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Text(
                                actionLabel,
                                style: PreparationDesign.label.copyWith(
                                  color: PreparationDesign.ink,
                                ),
                              ),
                              const SizedBox(width: 6),
                              const Icon(
                                Icons.arrow_forward_rounded,
                                color: PreparationDesign.ink,
                                size: 17,
                              ),
                            ],
                          ),
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
    required this.scenarioCount,
    required this.onPressed,
    super.key,
  });

  final String title;
  final String caption;
  final IconData icon;
  final int scenarioCount;
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
        label: '$title。$caption。$scenarioCount 个练习',
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

class _IeltsPartStep extends StatelessWidget {
  const _IeltsPartStep({
    required this.scenario,
    required this.partNumber,
    required this.label,
    required this.isLast,
    required this.onPressed,
  });

  final PreparationScenario scenario;
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
          label: 'Part $partNumber，$label。${scenario.summary}',
          onTap: onPressed,
          excludeSemantics: true,
          child: InkWell(
            key: Key('catalog-scenario-${scenario.id}'),
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

class _RoleplayScenarioGrid extends StatelessWidget {
  const _RoleplayScenarioGrid({
    required this.scenarios,
    required this.includeCustom,
    required this.onScenarioPressed,
  });

  final List<PreparationScenario> scenarios;
  final bool includeCustom;
  final ValueChanged<PreparationScenario> onScenarioPressed;

  @override
  Widget build(BuildContext context) {
    final children = <Widget>[
      for (final scenario in scenarios)
        _RoleplayScenarioCard(
          scenario: scenario,
          onPressed: () => onScenarioPressed(scenario),
        ),
      if (includeCustom) const _ReservedScenarioTile(),
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

class _RoleplayScenarioCard extends StatelessWidget {
  const _RoleplayScenarioCard({
    required this.scenario,
    required this.onPressed,
  });

  final PreparationScenario scenario;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final style = _roleplayCardStyle(scenario);
    return Material(
      key: Key('catalog-scenario-${scenario.id}'),
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Semantics(
        button: true,
        label: '${scenario.name}。${scenario.summary}',
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
                        scenario.name,
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

class _ReservedScenarioTile extends StatelessWidget {
  const _ReservedScenarioTile();

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '自定义场景，即将开放',
      enabled: false,
      child: ClipRRect(
        key: const Key('roleplay-custom-reserved'),
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

void _openScenarioPicker(
  BuildContext context, {
  required String title,
  required List<PreparationScenario> scenarios,
  required ValueChanged<PreparationScenario> onScenarioPressed,
}) {
  if (scenarios.isEmpty) {
    return;
  }
  showModalBottomSheet<void>(
    context: context,
    useSafeArea: true,
    isScrollControlled: true,
    showDragHandle: true,
    backgroundColor: Colors.white,
    builder: (sheetContext) => _ScenarioPickerSheet(
      title: title,
      scenarios: scenarios,
      onScenarioPressed: (scenario) {
        Navigator.of(sheetContext).pop();
        onScenarioPressed(scenario);
      },
    ),
  );
}

class _ScenarioPickerSheet extends StatelessWidget {
  const _ScenarioPickerSheet({
    required this.title,
    required this.scenarios,
    required this.onScenarioPressed,
  });

  final String title;
  final List<PreparationScenario> scenarios;
  final ValueChanged<PreparationScenario> onScenarioPressed;

  @override
  Widget build(BuildContext context) {
    final screenHeight = MediaQuery.sizeOf(context).height;
    final textScale = MediaQuery.textScalerOf(context).scale(1);
    final estimatedHeight =
        132 + scenarios.length * (98 + (textScale - 1).clamp(0, 1) * 120);
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
              style: TextStyle(color: Color(0xFF696B73), fontSize: 14),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.separated(
                itemCount: scenarios.length,
                separatorBuilder: (_, _) =>
                    const Divider(height: 1, color: Color(0xFFE2E2DE)),
                itemBuilder: (context, index) {
                  final scenario = scenarios[index];
                  return _CatalogScenarioCard(
                    scenario: scenario,
                    onPressed: () => onScenarioPressed(scenario),
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

class _HubEmpty extends StatelessWidget {
  const _HubEmpty({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Center(
        child: Text(
          message,
          textAlign: TextAlign.center,
          style: const TextStyle(color: Color(0xFF696B73)),
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
    return Semantics(
      button: true,
      label: '${scenario.name}。${scenario.summary}',
      onTap: onPressed,
      excludeSemantics: true,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          key: Key('catalog-scenario-${scenario.id}'),
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
                    _scenarioFamilyIcon(scenario.type),
                    size: 20,
                    color: const Color(0xFF55575E),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        scenario.name,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        scenario.summary,
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          color: Color(0xFF696B73),
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

class _ScenarioConfigCard extends StatelessWidget {
  const _ScenarioConfigCard({required this.config});

  final PreparationScenarioConfig config;

  @override
  Widget build(BuildContext context) {
    final prompt = config.prompt;
    return Material(
      key: const Key('preparation-scenario-config'),
      color: PreparationDesign.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('这次会练', style: PreparationDesign.sectionTitle),
            const SizedBox(height: 8),
            Text(prompt.publicSceneBrief, style: PreparationDesign.body),
            const SizedBox(height: 14),
            Text(prompt.practiceGoal, style: PreparationDesign.cardTitle),
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
        color: PreparationDesign.softSurface,
        borderRadius: BorderRadius.all(Radius.circular(12)),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 17, color: PreparationDesign.secondary),
            const SizedBox(width: 6),
            Flexible(child: Text(text, style: PreparationDesign.meta)),
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
      margin: EdgeInsets.zero,
      elevation: 0,
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
        side: BorderSide(
          color: selected ? PreparationDesign.ink : PreparationDesign.border,
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
                  color: selected
                      ? PreparationDesign.ink
                      : PreparationDesign.tertiary,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        role.displayName,
                        style: PreparationDesign.cardTitle,
                      ),
                      const SizedBox(height: 5),
                      Text(
                        role.responsibilities,
                        style: PreparationDesign.body.copyWith(fontSize: 14),
                      ),
                      const SizedBox(height: 5),
                      Text(role.style, style: PreparationDesign.meta),
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
      margin: EdgeInsets.zero,
      elevation: 0,
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
        side: BorderSide(
          color: selected ? PreparationDesign.ink : PreparationDesign.border,
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
                  color: selected
                      ? PreparationDesign.ink
                      : PreparationDesign.tertiary,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(typeLabel, style: PreparationDesign.cardTitle),
                      const SizedBox(height: 4),
                      Text(option.displayName, style: PreparationDesign.body),
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
      color: PreparationDesign.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusMedia),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('准备开始', style: PreparationDesign.sectionTitle),
            const SizedBox(height: 6),
            const Text('需要更贴近你的情况？可以补充一句背景。', style: PreparationDesign.body),
            const SizedBox(height: 12),
            TextField(
              key: const Key('preparation-background-summary'),
              controller: backgroundController,
              enabled: !controller.isSelectionLocked,
              minLines: 2,
              maxLines: 4,
              maxLength: 4000,
              textInputAction: TextInputAction.newline,
              decoration: const InputDecoration(
                labelText: '背景或目标（可选）',
                hintText: '可留空直接开始',
                filled: true,
                fillColor: PreparationDesign.canvas,
                enabledBorder: OutlineInputBorder(
                  borderSide: BorderSide(color: PreparationDesign.border),
                  borderRadius: BorderRadius.all(
                    Radius.circular(PreparationDesign.radiusControl),
                  ),
                ),
                focusedBorder: OutlineInputBorder(
                  borderSide: BorderSide(color: PreparationDesign.ink),
                  borderRadius: BorderRadius.all(
                    Radius.circular(PreparationDesign.radiusControl),
                  ),
                ),
                disabledBorder: OutlineInputBorder(
                  borderSide: BorderSide(color: PreparationDesign.border),
                  borderRadius: BorderRadius.all(
                    Radius.circular(PreparationDesign.radiusControl),
                  ),
                ),
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
            if (controller.errorMessage ?? controller.workspaceErrorMessage
                case final message?) ...[
              const SizedBox(height: 8),
              Text(
                message,
                key: const Key('preparation-launch-error'),
                style: const TextStyle(color: Color(0xFF9A332A), height: 1.4),
              ),
              if (controller.canRetryWorkspaceActivation)
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    key: const Key('retry-practice-workspace-detail'),
                    onPressed: () =>
                        unawaited(controller.retryWorkspaceActivation()),
                    child: const Text('重试读取练习记录'),
                  ),
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
                style: PreparationDesign.meta,
              ),
            ],
            const SizedBox(height: 12),
            FilledButton(
              key: const Key('preparation-start-practice'),
              onPressed: controller.isStarting ? null : onStart,
              style: FilledButton.styleFrom(
                minimumSize: const Size.fromHeight(52),
                backgroundColor: PreparationDesign.ink,
                foregroundColor: Colors.white,
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(
                    PreparationDesign.radiusControl,
                  ),
                ),
              ),
              child: Text(controller.isStarting ? '正在创建练习' : '开始练习'),
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
    PreparationLaunchStage.context => '正在准备独立练习空间',
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
    'EXAM' => 'IELTS 口语',
    'WORKPLACE' || 'DAILY' => 'AI 数字人陪练',
    _ => family,
  };
}

String _practiceHubLabel(_PracticeHub hub) {
  return switch (hub) {
    _PracticeHub.interview => '英文面试',
    _PracticeHub.ielts => 'IELTS 口语',
    _PracticeHub.roleplay => 'AI 数字人陪练',
  };
}

List<PreparationScenario> _scenariosForHub(
  List<PreparationScenario> scenarios,
  _PracticeHub hub,
) {
  return scenarios
      .where((scenario) {
        return switch (hub) {
          _PracticeHub.interview =>
            scenario.type == 'INTERVIEW' &&
                !_hiddenCatalogScenarioIds.contains(scenario.id),
          _PracticeHub.ielts => _ieltsScenarioIds.contains(scenario.id),
          _PracticeHub.roleplay =>
            (scenario.type == 'WORKPLACE' || scenario.type == 'DAILY') &&
                !_hiddenCatalogScenarioIds.contains(scenario.id),
        };
      })
      .toList(growable: false);
}

PreparationScenario? _scenarioById(
  List<PreparationScenario> scenarios,
  String id,
) {
  for (final scenario in scenarios) {
    if (scenario.id == id) {
      return scenario;
    }
  }
  return null;
}

List<PreparationScenario> _scenariosByIds(
  List<PreparationScenario> scenarios,
  List<String> ids,
) {
  final byId = {for (final scenario in scenarios) scenario.id: scenario};
  return [for (final id in ids) ?byId[id]];
}

String _ieltsPartNumber(String scenarioId) {
  return switch (scenarioId) {
    'scn_ielts_speaking_part_1' => '1',
    'scn_ielts_speaking_part_2' => '2',
    'scn_ielts_speaking_part_3' => '3',
    _ => '•',
  };
}

String _ieltsPartLabel(String scenarioId) {
  return switch (scenarioId) {
    'scn_ielts_speaking_part_1' => '熟悉话题问答',
    'scn_ielts_speaking_part_2' => '题卡陈述',
    'scn_ielts_speaking_part_3' => '延伸讨论',
    _ => '专项练习',
  };
}

String _roleplayFilterLabel(_RoleplayFilter filter) {
  return switch (filter) {
    _RoleplayFilter.recommended => '推荐',
    _RoleplayFilter.workplace => '职场',
    _RoleplayFilter.travel => '旅行',
    _RoleplayFilter.daily => '日常',
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
_roleplayCardStyle(PreparationScenario scenario) {
  final id = scenario.id;
  final icon = switch (id) {
    String value when value.contains('restaurant') => Icons.restaurant_outlined,
    String value when value.contains('shopping') => Icons.shopping_bag_outlined,
    String value when value.contains('airport') => Icons.flight_outlined,
    String value when value.contains('hotel') => Icons.hotel_outlined,
    String value when value.contains('medical') =>
      Icons.medical_services_outlined,
    String value when value.contains('rental') =>
      Icons.home_repair_service_outlined,
    String value when value.contains('phone') => Icons.phone_outlined,
    String value when value.contains('presentation') =>
      Icons.co_present_outlined,
    String value when value.contains('progress') => Icons.insights_outlined,
    String value when value.contains('meeting') => Icons.groups_outlined,
    String value when value.contains('alignment') => Icons.hub_outlined,
    String value when value.contains('negotiation') => Icons.handshake_outlined,
    String value when value.contains('feedback') => Icons.forum_outlined,
    String value when value.contains('client') => Icons.support_agent_outlined,
    String value when value.contains('complaint') =>
      Icons.support_agent_outlined,
    String value
        when value.contains('social') || value.contains('small_talk') =>
      Icons.people_outline_rounded,
    _ => Icons.chat_bubble_outline_rounded,
  };
  final assetPath = switch (id) {
    'scn_workplace_progress_risk_update' =>
      'assets/images/scenes/workplace-scene.jpg',
    'scn_workplace_meeting_disagreement' =>
      'assets/images/scenes/meeting-disagreement.jpg',
    'scn_daily_restaurant_ordering' => 'assets/images/scenes/daily-tutor.jpg',
    'scn_daily_airport_transport' =>
      'assets/images/scenes/airport-transport.jpg',
    'scn_daily_hotel_checkin_issue' => 'assets/images/scenes/travel-scene.jpg',
    'scn_daily_small_talk' => 'assets/images/scenes/small-talk.jpg',
    _ => null,
  };
  if (scenario.type == 'WORKPLACE') {
    return (
      background: const Color(0xFFE8EBED),
      foreground: const Color(0xFF273238),
      icon: icon,
      category: '职场',
      assetPath: assetPath,
      imageAlignment: Alignment.topCenter,
    );
  }
  if (_RoleplayHubState._travelScenarioIds.contains(id)) {
    return (
      background: const Color(0xFFDDEBF0),
      foreground: const Color(0xFF1D4754),
      icon: icon,
      category: '旅行',
      assetPath: assetPath,
      imageAlignment: Alignment.center,
    );
  }
  return (
    background: const Color(0xFFF2E8DE),
    foreground: const Color(0xFF4C392B),
    icon: icon,
    category: '日常',
    assetPath: assetPath,
    imageAlignment: const Alignment(0, -0.65),
  );
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
