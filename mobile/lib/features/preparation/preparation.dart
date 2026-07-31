/// Preparation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_design.dart';
import 'package:speakup/features/preparation/ielts_question_bank.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_models.dart';
import 'package:speakup/features/preparation/preparation_models.dart';

const _jobInterviewScenarioId = 'scn_programmer_interview';
const _interviewFullScenarioId = _jobInterviewScenarioId;
const _agentCreatedInterviewScenarioId = 'scn_interview_custom';
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

enum _ExistingPracticeAction { continuePractice, replace }

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
    this.openInterviewRequestGeneration = 0,
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
  final int openInterviewRequestGeneration;

  @override
  State<PreparationPage> createState() => _PreparationPageState();
}

class _PreparationPageState extends State<PreparationPage> {
  TextEditingController? _backgroundController;
  _PracticeHub? _selectedHub;
  IeltsPracticeMode? _selectedIeltsSection;
  IeltsPracticeSelection? _launchingIeltsSelection;
  bool _handlingIeltsNavigation = false;
  int _handledOpenInterviewRequestGeneration = 0;

  @override
  void initState() {
    super.initState();
    widget.agentController?.addListener(_rebuild);
    widget.preparationController?.addListener(_rebuild);
    widget.launchController?.addListener(_rebuild);
    _backgroundController = _newBackgroundController(widget.launchController);
    _scheduleOpenInterviewRequest();
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
    if (oldWidget.openInterviewRequestGeneration !=
        widget.openInterviewRequestGeneration) {
      _scheduleOpenInterviewRequest();
    }
  }

  void _scheduleOpenInterviewRequest() {
    final generation = widget.openInterviewRequestGeneration;
    if (generation <= 0 ||
        generation == _handledOpenInterviewRequestGeneration) {
      return;
    }
    _handledOpenInterviewRequestGeneration = generation;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && widget.openInterviewRequestGeneration == generation) {
        unawaited(_openAgentCreatedInterview());
      }
    });
  }

  Future<void> _openAgentCreatedInterview() async {
    final controller = widget.preparationController;
    if (controller == null) {
      return;
    }
    await controller.loadIfNeeded();
    if (!mounted) {
      return;
    }
    final scenario = _scenarioById(
      controller.scenarios,
      _agentCreatedInterviewScenarioId,
    );
    if (scenario == null) {
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('专属面试场景暂时不可用，请稍后重试')));
      return;
    }
    final matter = widget.agentController?.activeMatter;
    final background = matter?.scene.description.trim().isNotEmpty == true
        ? matter!.scene.description.trim()
        : matter?.scene.title.trim();
    if (background != null && background.isNotEmpty) {
      widget.launchController?.updateBackgroundSummary(background);
    }
    await _startScenarioDirectly(controller, scenario);
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
      final request = widget.preparationController
          ?.takeIeltsNavigationRequest();
      if (request != null && !_handlingIeltsNavigation) {
        _handlingIeltsNavigation = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          unawaited(_handleIeltsNavigation(request));
        });
      }
    }
  }

  Future<void> _handleIeltsNavigation(
    IeltsPracticeNavigationRequest request,
  ) async {
    final controller = widget.preparationController;
    if (!mounted || controller == null) {
      _handlingIeltsNavigation = false;
      return;
    }
    setState(() {
      _selectedHub = _PracticeHub.ielts;
      _selectedIeltsSection = request.mode;
    });
    final selection = request.selection;
    if (selection != null) {
      final scenario = _ieltsScenarioForMode(
        _scenariosForHub(controller.scenarios, _PracticeHub.ielts),
        selection.mode,
      );
      if (scenario != null) {
        await _startScenarioDirectly(
          controller,
          scenario,
          ieltsSelection: selection,
          forceReplaceCurrentPractice: true,
        );
      }
    }
    _handlingIeltsNavigation = false;
  }

  TextEditingController? _newBackgroundController(
    PreparationLaunchController? controller,
  ) {
    return controller == null
        ? null
        : TextEditingController(text: controller.backgroundSummary);
  }

  Future<void> _startPractice({
    required bool replaceCurrentPractice,
    IeltsPracticeSelection? ieltsSelection,
  }) async {
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
        ieltsSelection: ieltsSelection,
      ),
      replaceCurrentPractice: replaceCurrentPractice,
    );
    if (started && mounted) {
      final bootstrap = launch.bootstrap;
      if (bootstrap != null && ieltsSelection != null) {
        await catalog.beginIeltsSession(bootstrap.session.id, ieltsSelection);
      }
      catalog.showScenarioList();
      setState(() {
        _selectedHub = null;
        _selectedIeltsSection = null;
        _launchingIeltsSelection = null;
      });
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _retryLaunch() async {
    final started = await widget.launchController?.retry() ?? false;
    if (started && mounted) {
      final catalog = widget.preparationController;
      final bootstrap = widget.launchController?.bootstrap;
      final selection = _launchingIeltsSelection;
      if (catalog != null && bootstrap != null && selection != null) {
        await catalog.beginIeltsSession(bootstrap.session.id, selection);
      }
      catalog?.showScenarioList();
      setState(() {
        _selectedHub = null;
        _selectedIeltsSection = null;
        _launchingIeltsSelection = null;
      });
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _startScenarioDirectly(
    PreparationController controller,
    PreparationScenario scenario, {
    IeltsPracticeSelection? ieltsSelection,
    bool forceReplaceCurrentPractice = false,
  }) async {
    var replaceCurrentPractice = forceReplaceCurrentPractice;
    final launch = widget.launchController;
    if ((launch?.hasResumablePractice ?? false) &&
        !forceReplaceCurrentPractice) {
      if (launch?.resumableScenarioId == scenario.id) {
        final resumableSessionId = launch?.resumableSessionId;
        final resumableSelection = resumableSessionId == null
            ? null
            : controller.ieltsSelectionForSession(resumableSessionId);
        if (ieltsSelection == null || resumableSelection == ieltsSelection) {
          await _continueCurrentPractice();
          return;
        }
      }
      final action = await _chooseExistingPracticeAction(
        currentTitle: launch?.resumablePracticeTitle,
        nextTitle: scenario.name,
      );
      if (!mounted || action == null) {
        return;
      }
      if (action == _ExistingPracticeAction.continuePractice) {
        await _continueCurrentPractice();
        return;
      }
      replaceCurrentPractice = true;
    }
    await controller.selectScenario(scenario);
    if (!mounted || controller.selectedScenario?.id != scenario.id) {
      return;
    }
    final configured = controller.selectRecommendedConfiguration();
    if (!configured) {
      return;
    }
    if (widget.launchController == null) {
      controller.showScenarioList();
      return;
    }
    _launchingIeltsSelection = ieltsSelection;
    await _startPractice(
      replaceCurrentPractice: replaceCurrentPractice,
      ieltsSelection: ieltsSelection,
    );
  }

  Future<void> _startIeltsFullMock(
    PreparationController controller,
    PreparationScenario scenario,
  ) async {
    await controller.loadIeltsQuestionBankIfNeeded();
    if (!mounted) {
      return;
    }
    final selection = controller.randomFullMockSelection();
    if (selection == null) {
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(
            content: Text(controller.ieltsErrorMessage ?? '雅思口语题库暂时不可用，请稍后重试。'),
          ),
        );
      return;
    }
    await _startScenarioDirectly(
      controller,
      scenario,
      ieltsSelection: selection,
    );
  }

  Future<_ExistingPracticeAction?> _chooseExistingPracticeAction({
    required String? currentTitle,
    required String nextTitle,
  }) async {
    final activeTitle = currentTitle ?? '上次练习';
    return showModalBottomSheet<_ExistingPracticeAction>(
      context: context,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (context) => Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '开始新的练习？',
                        style: Theme.of(context).textTheme.titleLarge,
                      ),
                      const SizedBox(height: 8),
                      Text(
                        '你正在练“$activeTitle”。开始“$nextTitle”后，'
                        '当前进度将结束。',
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                IconButton(
                  tooltip: '关闭',
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close_rounded),
                ),
              ],
            ),
            const SizedBox(height: 24),
            FilledButton(
              key: const Key('continue-existing-practice'),
              onPressed: () => Navigator.of(
                context,
              ).pop(_ExistingPracticeAction.continuePractice),
              child: Text('继续“$activeTitle”'),
            ),
            const SizedBox(height: 8),
            OutlinedButton(
              key: const Key('replace-existing-practice'),
              onPressed: () =>
                  Navigator.of(context).pop(_ExistingPracticeAction.replace),
              child: Text('开始“$nextTitle”'),
            ),
          ],
        ),
      ),
    );
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
      return _ScenarioLaunchStatus(
        controller: controller,
        scenario: selectedScenario,
        launchController: widget.launchController,
        hasPrimaryNavigation: !widget.showBackButton,
        onBack: () => controller.showScenarioList(),
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
            onPressed: () {
              setState(() => _selectedHub = _PracticeHub.ielts);
              unawaited(controller.loadIeltsQuestionBankIfNeeded());
            },
          ),
          const SizedBox(height: 12),
          _PracticeHubEntry(
            key: const Key('practice-hub-roleplay'),
            title: '情景对话',
            description: '工作、旅行与日常英语实战',
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
    final ieltsSection = hub == _PracticeHub.ielts
        ? _selectedIeltsSection
        : null;
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
            onPressed: () => setState(() {
              if (ieltsSection != null) {
                _selectedIeltsSection = null;
              } else {
                _selectedHub = null;
              }
            }),
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
            onScenarioPressed: (scenario) =>
                unawaited(_startScenarioDirectly(controller, scenario)),
            onOpenJobPreparation: widget.onOpenJobPreparation,
          )
        else if (hub == _PracticeHub.ielts)
          if (ieltsSection == null)
            _IeltsHub(
              scenarios: scenarios,
              onFullMockPressed: (scenario) =>
                  unawaited(_startIeltsFullMock(controller, scenario)),
              onPartPressed: (scenario) {
                final mode = _ieltsModeForScenario(scenario.id);
                if (mode != null) {
                  setState(() => _selectedIeltsSection = mode);
                  unawaited(controller.loadIeltsQuestionBankIfNeeded());
                }
              },
            )
          else
            _IeltsSetList(
              controller: controller,
              mode: ieltsSection,
              scenario: _ieltsScenarioForMode(scenarios, ieltsSection),
              onRetry: controller.loadIeltsQuestionBankIfNeeded,
              onSelectionPressed: (scenario, selection) => unawaited(
                _startScenarioDirectly(
                  controller,
                  scenario,
                  ieltsSelection: selection,
                ),
              ),
            )
        else
          _RoleplayHub(
            scenarios: scenarios,
            onScenarioPressed: (scenario) =>
                unawaited(_startScenarioDirectly(controller, scenario)),
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
          title: '情景对话',
          description: '工作、旅行与日常英语实战',
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

class _ScenarioLaunchStatus extends StatelessWidget {
  const _ScenarioLaunchStatus({
    required this.controller,
    required this.scenario,
    required this.launchController,
    required this.hasPrimaryNavigation,
    required this.onBack,
    required this.onRetry,
  });

  final PreparationController controller;
  final PreparationScenario scenario;
  final PreparationLaunchController? launchController;
  final bool hasPrimaryNavigation;
  final VoidCallback onBack;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final launchLocked = launchController?.isSelectionLocked ?? false;
    final message =
        controller.errorMessage ??
        launchController?.errorMessage ??
        launchController?.workspaceErrorMessage;
    return ListView(
      key: const Key('preparation-scenario-launch-status'),
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
            tooltip: '取消并返回',
            onPressed: launchLocked ? null : onBack,
            icon: const Icon(Icons.arrow_back_rounded),
            color: PreparationDesign.ink,
            style: IconButton.styleFrom(
              backgroundColor: PreparationDesign.surface,
              side: const BorderSide(color: PreparationDesign.border),
            ),
          ),
        ),
        const SizedBox(height: 18),
        Text(
          scenario.name,
          key: const Key('preparation-scenario-title'),
          style: PreparationDesign.pageTitle,
        ),
        const SizedBox(height: 8),
        Text(
          message == null ? '正在准备练习…' : '暂时无法开始这项练习。',
          style: PreparationDesign.body,
        ),
        const SizedBox(height: 28),
        if (message != null)
          _CatalogFailure(
            key: const Key('preparation-launch-error'),
            message: message,
            onRetry: controller.errorMessage != null
                ? controller.retryLastFailure
                : launchController?.canRetry == true
                ? onRetry
                : () async => onBack(),
          )
        else
          const LinearProgressIndicator(
            key: Key('preparation-launch-progress'),
          ),
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
      color: PreparationDesign.surface,
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
              style: TextStyle(color: PreparationDesign.inkSecondary),
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
              color: PreparationDesign.surfaceMuted,
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Icon(
              Icons.history_rounded,
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
                  label,
                  style: const TextStyle(
                    color: PreparationDesign.inkSecondary,
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
          horizontal: BorderSide(color: PreparationDesign.border),
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
  const _IeltsHub({
    required this.scenarios,
    required this.onFullMockPressed,
    required this.onPartPressed,
  });

  final List<PreparationScenario> scenarios;
  final ValueChanged<PreparationScenario> onFullMockPressed;
  final ValueChanged<PreparationScenario> onPartPressed;

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
            onPressed: () => onFullMockPressed(fullScenario),
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
              onPressed: () => onPartPressed(partScenarios[index]),
            ),
        ],
      ],
    );
  }
}

class _IeltsSetList extends StatelessWidget {
  const _IeltsSetList({
    required this.controller,
    required this.mode,
    required this.scenario,
    required this.onRetry,
    required this.onSelectionPressed,
  });

  final PreparationController controller;
  final IeltsPracticeMode mode;
  final PreparationScenario? scenario;
  final Future<void> Function() onRetry;
  final void Function(
    PreparationScenario scenario,
    IeltsPracticeSelection selection,
  )
  onSelectionPressed;

  @override
  Widget build(BuildContext context) {
    final bank = controller.ieltsQuestionBank;
    final title = switch (mode) {
      IeltsPracticeMode.part1 => 'Part 1 套题',
      IeltsPracticeMode.part2 => 'Part 2 题卡',
      IeltsPracticeMode.part3 => 'Part 3 主题讨论',
      IeltsPracticeMode.fullMock => '完整模考',
    };
    if (controller.isLoadingIeltsQuestionBank && bank == null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _HubHeader(
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
          _HubHeader(
            title: title,
            description: '暂时无法显示套题。',
            titleKey: Key('ielts-set-list-title-${mode.name}'),
          ),
          const SizedBox(height: 24),
          _InlineFailure(
            message: controller.ieltsErrorMessage ?? '雅思口语题库暂时不可用。',
            retryKey: const Key('ielts-question-bank-retry'),
            onRetry: onRetry,
          ),
        ],
      );
    }
    final total = mode == IeltsPracticeMode.part1
        ? bank.part1Sets.length
        : bank.topicGroups.length;
    final completed = mode == IeltsPracticeMode.part1
        ? bank.part1Sets
              .where(
                (set) => controller
                    .ieltsProgress(IeltsPracticeMode.part1, set.id)
                    .completed,
              )
              .length
        : bank.topicGroups
              .where(
                (group) => controller.ieltsProgress(mode, group.id).completed,
              )
              .length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _HubHeader(
          title: title,
          description: switch (mode) {
            IeltsPracticeMode.part1 => '每套包含 3 个熟悉话题，共 8 道题。',
            IeltsPracticeMode.part2 => '完成题卡后，可以继续同主题 Part 3。',
            IeltsPracticeMode.part3 => '先看对应 Part 2 背景，再练绑定讨论题。',
            IeltsPracticeMode.fullMock => '按正式顺序完成三个 Part。',
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
        if (scenario == null)
          const _HubEmpty(message: '当前分段练习尚未开放。')
        else if (mode == IeltsPracticeMode.part1)
          for (final set in bank.part1Sets) ...[
            _IeltsSetCard(
              key: Key('ielts-part1-set-${set.id}'),
              title: set.title,
              description: set.topicSummary,
              meta: '${set.questionCount} 道题',
              progress: controller.ieltsProgress(mode, set.id),
              onPressed: () => onSelectionPressed(
                scenario!,
                IeltsPracticeSelection(mode: mode, part1SetId: set.id),
              ),
            ),
            const SizedBox(height: 10),
          ]
        else
          for (final group in bank.topicGroups) ...[
            _IeltsSetCard(
              key: Key('ielts-${mode.name}-set-${group.id}'),
              title: mode == IeltsPracticeMode.part2
                  ? group.cueCard.prompt
                  : group.title,
              description: mode == IeltsPracticeMode.part2
                  ? '${_ieltsReleaseLabel(group.release)} · 可继续对应 Part 3'
                  : '对应 Part 2：${group.cueCard.prompt}',
              meta: mode == IeltsPracticeMode.part2
                  ? _pairedProgressLabel(controller, group.id)
                  : '${group.part3Questions.length} 道讨论题',
              progress: controller.ieltsProgress(mode, group.id),
              showProgressStatus: mode != IeltsPracticeMode.part2,
              onPressed: () => onSelectionPressed(
                scenario!,
                IeltsPracticeSelection(mode: mode, topicGroupId: group.id),
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
                  '情景对话',
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
              style: TextStyle(
                color: PreparationDesign.inkSecondary,
                fontSize: 14,
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.separated(
                itemCount: scenarios.length,
                separatorBuilder: (_, _) =>
                    const Divider(height: 1, color: PreparationDesign.border),
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
          style: const TextStyle(color: PreparationDesign.inkSecondary),
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
                    color: PreparationDesign.inkSecondary,
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

String _practiceHubLabel(_PracticeHub hub) {
  return switch (hub) {
    _PracticeHub.interview => '英文面试',
    _PracticeHub.ielts => 'IELTS 口语',
    _PracticeHub.roleplay => '情景对话',
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
    'scn_ielts_speaking_part_2' => '题卡陈述 · 可继续 Part 3',
    'scn_ielts_speaking_part_3' => '承接 Part 2 主题讨论',
    _ => '专项练习',
  };
}

IeltsPracticeMode? _ieltsModeForScenario(String scenarioId) {
  return switch (scenarioId) {
    'scn_ielts_speaking_part_1' => IeltsPracticeMode.part1,
    'scn_ielts_speaking_part_2' => IeltsPracticeMode.part2,
    'scn_ielts_speaking_part_3' => IeltsPracticeMode.part3,
    _ieltsFullScenarioId => IeltsPracticeMode.fullMock,
    _ => null,
  };
}

PreparationScenario? _ieltsScenarioForMode(
  List<PreparationScenario> scenarios,
  IeltsPracticeMode mode,
) {
  final id = switch (mode) {
    IeltsPracticeMode.fullMock => _ieltsFullScenarioId,
    IeltsPracticeMode.part1 => 'scn_ielts_speaking_part_1',
    IeltsPracticeMode.part2 => 'scn_ielts_speaking_part_2',
    IeltsPracticeMode.part3 => 'scn_ielts_speaking_part_3',
  };
  return _scenarioById(scenarios, id);
}

String _ieltsReleaseLabel(String release) {
  return switch (release) {
    'new' => '当季新题',
    'carry_over' => '老题沿用',
    _ => '本季题目',
  };
}

String _pairedProgressLabel(PreparationController controller, String groupId) {
  final part2 = controller.ieltsProgress(IeltsPracticeMode.part2, groupId);
  final part3 = controller.ieltsProgress(IeltsPracticeMode.part3, groupId);
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
      color: PreparationDesign.errorMuted,
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
        child: Row(
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 20,
              color: PreparationDesign.error,
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
      color: PreparationDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: BorderSide(
          color: selected ? PreparationDesign.ink : PreparationDesign.border,
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
                  color: PreparationDesign.surfaceMuted,
                  borderRadius: BorderRadius.circular(15),
                ),
                child: Icon(
                  selected ? Icons.check_rounded : Icons.work_outline_rounded,
                  color: PreparationDesign.inkSecondary,
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
                        color: PreparationDesign.inkSecondary,
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
