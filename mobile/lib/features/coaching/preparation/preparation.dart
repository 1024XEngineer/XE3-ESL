/// Preparation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/features/coaching/ielts/ielts_catalog.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/interview/interview_catalog.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/scenario/scenario_catalog.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_catalog_components.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/scenario_preparation_form.dart';

enum _PracticeHub { interview, ielts, workplace, life }

enum _ExistingPracticeAction { continuePractice, replace }

class PreparationPage extends StatefulWidget {
  const PreparationPage({
    this.showBackButton = false,
    this.previewMode = false,
    this.practiceController,
    this.preparationController,
    this.ieltsController,
    this.launchController,
    this.jobPreparationController,
    this.onOpenJobPreparation,
    this.onOpenInterviewPlan,
    this.onSceneSelected,
    this.onPracticeStarted,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final PracticeController? practiceController;
  final PreparationController? preparationController;
  final IeltsPreparationController? ieltsController;
  final PreparationLaunchController? launchController;
  final JobPreparationController? jobPreparationController;
  final VoidCallback? onOpenJobPreparation;
  final ValueChanged<String>? onOpenInterviewPlan;
  final VoidCallback? onSceneSelected;
  final VoidCallback? onPracticeStarted;

  @override
  State<PreparationPage> createState() => _PreparationPageState();
}

class _PreparationPageState extends State<PreparationPage> {
  TextEditingController? _backgroundController;
  _PracticeHub? _selectedHub;
  IeltsPracticeSelection? _launchingIeltsSelection;
  bool _scenarioFormVisible = false;
  bool _scenarioReplaceCurrentPractice = false;
  bool _handlingIeltsNavigation = false;

  @override
  void initState() {
    super.initState();
    widget.practiceController?.addListener(_rebuild);
    widget.preparationController?.addListener(_rebuild);
    widget.ieltsController?.addListener(_rebuild);
    widget.launchController?.addListener(_rebuild);
    widget.jobPreparationController?.addListener(_rebuild);
    _backgroundController = _newBackgroundController(widget.launchController);
    unawaited(widget.preparationController?.loadIfNeeded());
  }

  @override
  void didUpdateWidget(covariant PreparationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.practiceController != widget.practiceController) {
      oldWidget.practiceController?.removeListener(_rebuild);
      widget.practiceController?.addListener(_rebuild);
    }
    if (oldWidget.preparationController != widget.preparationController) {
      oldWidget.preparationController?.removeListener(_rebuild);
      widget.preparationController?.addListener(_rebuild);
      unawaited(widget.preparationController?.loadIfNeeded());
    }
    if (oldWidget.ieltsController != widget.ieltsController) {
      oldWidget.ieltsController?.removeListener(_rebuild);
      widget.ieltsController?.addListener(_rebuild);
    }
    if (oldWidget.launchController != widget.launchController) {
      oldWidget.launchController?.removeListener(_rebuild);
      widget.launchController?.addListener(_rebuild);
      _backgroundController?.dispose();
      _backgroundController = _newBackgroundController(widget.launchController);
    }
    if (oldWidget.jobPreparationController != widget.jobPreparationController) {
      oldWidget.jobPreparationController?.removeListener(_rebuild);
      widget.jobPreparationController?.addListener(_rebuild);
    }
  }

  @override
  void dispose() {
    widget.practiceController?.removeListener(_rebuild);
    widget.preparationController?.removeListener(_rebuild);
    widget.ieltsController?.removeListener(_rebuild);
    widget.launchController?.removeListener(_rebuild);
    widget.jobPreparationController?.removeListener(_rebuild);
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
      final request = widget.ieltsController?.takeNavigationRequest();
      if (request != null && !_handlingIeltsNavigation) {
        _handlingIeltsNavigation = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          unawaited(_handleIeltsNavigation(request));
        });
      }
    }
  }

  void _openInterviewCatalog() {
    setState(() => _selectedHub = _PracticeHub.interview);
    unawaited(widget.jobPreparationController?.loadInterviewPlans());
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
    });
    final selection = request.selection;
    if (selection != null) {
      final scene = ieltsSceneForMode(
        _scenesForHub(controller.scenes, _PracticeHub.ielts),
        request.mode,
      );
      if (scene != null) {
        await _startSceneDirectly(
          controller,
          scene,
          practiceMode: request.mode,
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
    ScenarioPreparationContext? scenarioContext,
  }) async {
    final catalog = widget.preparationController;
    final launch = widget.launchController;
    final scene = catalog?.selectedScene;
    final detail = catalog?.detail;
    final role = catalog?.selectedRole;
    final option = catalog?.selectedOption;
    if (catalog == null ||
        launch == null ||
        scene == null ||
        detail == null ||
        role == null ||
        option == null) {
      return;
    }
    if (scenarioContext == null && launch.backgroundSummary.trim().isEmpty) {
      launch.updateBackgroundSummary('默认示例：${detail.prompt.publicSceneBrief}');
    }
    final started = await launch.start(
      PreparationLaunchSelection.fromCatalog(
        scene: scene,
        role: role,
        option: option,
        ieltsSelection: ieltsSelection,
      ),
      replaceCurrentPractice: replaceCurrentPractice,
      scenarioContext: scenarioContext,
    );
    if (started && mounted) {
      final bootstrap = launch.bootstrap;
      if (bootstrap != null && ieltsSelection != null) {
        await widget.ieltsController?.beginSession(
          bootstrap.session.id,
          option.mode,
          ieltsSelection,
        );
      }
      catalog.showSceneList();
      setState(() {
        _selectedHub = null;
        _launchingIeltsSelection = null;
        _scenarioFormVisible = false;
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
        final mode = catalog.selectedOption?.mode;
        if (mode != null) {
          await widget.ieltsController?.beginSession(
            bootstrap.session.id,
            mode,
            selection,
          );
        }
      }
      catalog?.showSceneList();
      setState(() {
        _selectedHub = null;
        _launchingIeltsSelection = null;
        _scenarioFormVisible = false;
      });
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _startSceneDirectly(
    PreparationController controller,
    SceneDefinition scene, {
    PracticeMode? practiceMode,
    IeltsPracticeSelection? ieltsSelection,
    bool forceReplaceCurrentPractice = false,
    bool requireScenarioPreparation = false,
  }) async {
    var replaceCurrentPractice = forceReplaceCurrentPractice;
    final launch = widget.launchController;
    if ((launch?.hasResumablePractice ?? false) &&
        !forceReplaceCurrentPractice) {
      if (ieltsSelection != null) {
        replaceCurrentPractice = true;
      } else if (!(launch?.resumableHasProgress ?? true)) {
        replaceCurrentPractice = true;
      } else {
        if (launch?.resumableSceneId == scene.id) {
          final resumableSessionId = launch?.resumableSessionId;
          final resumableSelection = resumableSessionId == null
              ? null
              : widget.ieltsController?.selectionForSession(resumableSessionId);
          if (ieltsSelection == null || resumableSelection == ieltsSelection) {
            await _continueCurrentPractice();
            return;
          }
        }
        final action = await _chooseExistingPracticeAction(
          currentTitle: launch?.resumablePracticeTitle,
          nextTitle: scene.name,
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
    }
    await controller.selectScene(scene);
    if (!mounted || controller.selectedScene?.id != scene.id) {
      return;
    }
    final configured = controller.selectRecommendedConfiguration(
      practiceMode: practiceMode,
    );
    if (!configured) {
      return;
    }
    if (widget.launchController == null) {
      controller.showSceneList();
      return;
    }
    if (requireScenarioPreparation) {
      setState(() {
        _scenarioFormVisible = true;
        _scenarioReplaceCurrentPractice = replaceCurrentPractice;
      });
      return;
    }
    _launchingIeltsSelection = ieltsSelection;
    await _startPractice(
      replaceCurrentPractice: replaceCurrentPractice,
      ieltsSelection: ieltsSelection,
    );
  }

  Future<void> _submitScenarioPreparation(
    ScenarioPreparationContext context,
  ) async {
    setState(() {
      _scenarioFormVisible = false;
    });
    await _startPractice(
      replaceCurrentPractice: _scenarioReplaceCurrentPractice,
      scenarioContext: context,
    );
  }

  Future<void> _startIeltsFullMock(
    PreparationController controller,
    SceneDefinition scene,
  ) async {
    await _startSceneDirectly(
      controller,
      scene,
      practiceMode: PracticeMode.fullMock,
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

  Future<void> _continueCurrentPractice() async {
    final launch = widget.launchController;
    if (launch?.hasResumablePractice ?? false) {
      final resumed = await launch!.resumeCurrentPractice();
      if (resumed && mounted) {
        widget.preparationController?.showSceneList();
        setState(() => _selectedHub = null);
        widget.onPracticeStarted?.call();
      }
      return;
    }
    if (widget.practiceController?.hasActivePractice ?? false) {
      widget.onPracticeStarted?.call();
    } else if (widget.onSceneSelected case final callback?) {
      callback();
    } else if (widget.showBackButton) {
      Navigator.of(context).maybePop();
    }
  }

  Future<void> _handleBack(PreparationController? controller) async {
    final launch = widget.launchController;
    if (launch?.isStarting ?? false) {
      if (!await launch!.cancelCurrentPreparation() || !mounted) {
        return;
      }
    } else if (launch?.isNavigationLocked ?? false) {
      return;
    }
    if (controller?.selectedScene != null) {
      if (!(launch?.prepareFailedAttemptForNavigation() ?? true)) {
        return;
      }
      launch?.selectionChanged();
      controller?.showSceneList();
      setState(() {
        _scenarioFormVisible = false;
        _scenarioReplaceCurrentPractice = false;
      });
      return;
    }
    if (_selectedHub != null) {
      setState(() => _selectedHub = null);
      return;
    }
    await Navigator.of(context).maybePop();
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.preparationController;
    final navigationLocked =
        widget.launchController?.isNavigationLocked ?? false;
    final hasInternalRoute =
        controller?.selectedScene != null || _selectedHub != null;
    return PopScope<void>(
      canPop: !navigationLocked && !hasInternalRoute,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          unawaited(_handleBack(controller));
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
                  onPressed:
                      navigationLocked &&
                          !(widget.launchController?.isStarting ?? false)
                      ? null
                      : () => unawaited(_handleBack(controller)),
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
    final selectedScene = controller.selectedScene;
    if (selectedScene != null) {
      if (_scenarioFormVisible &&
          (selectedScene.experience == PracticeExperience.workplace ||
              selectedScene.experience == PracticeExperience.lifeAndTravel)) {
        return ScenarioPreparationForm(
          key: ValueKey(
            'scenario-preparation-${selectedScene.id}-${selectedScene.version}',
          ),
          scene: selectedScene,
          hasPrimaryNavigation: !widget.showBackButton,
          onBack: () => unawaited(_handleBack(controller)),
          onSubmit: _submitScenarioPreparation,
        );
      }
      return _SceneLaunchStatus(
        controller: controller,
        scene: selectedScene,
        launchController: widget.launchController,
        hasPrimaryNavigation: !widget.showBackButton,
        onBack: () => unawaited(_handleBack(controller)),
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
        const SizedBox(height: 16),
        if (controller.isLoadingScenes)
          const _CatalogLoading(key: Key('preparation-catalog-loading'))
        else if (controller.errorMessage case final message?)
          _CatalogFailure(
            key: const Key('preparation-catalog-error'),
            message: message,
            onRetry: controller.retryLastFailure,
          )
        else if (controller.scenes.isEmpty)
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
            onPressed: _openInterviewCatalog,
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
              final ielts = widget.ieltsController;
              if (ielts == null) {
                throw StateError('IELTS controller is not configured.');
              }
              setState(() => _selectedHub = _PracticeHub.ielts);
              unawaited(ielts.loadIfNeeded());
            },
          ),
          const SizedBox(height: 12),
          _PracticeHubEntry(
            key: const Key('practice-hub-workplace'),
            title: '职场英语',
            description: '会议、协作与客户沟通',
            icon: Icons.business_center_outlined,
            accentColor: PreparationDesign.scenario,
            tintColor: PreparationDesign.scenarioTint,
            assetPath: 'assets/images/scenes/workplace-scene.jpg',
            onPressed: () =>
                setState(() => _selectedHub = _PracticeHub.workplace),
          ),
          const SizedBox(height: 12),
          _PracticeHubEntry(
            key: const Key('practice-hub-life'),
            title: '生活与旅行',
            description: '日常交流与出行场景实战',
            icon: Icons.travel_explore_outlined,
            accentColor: PreparationDesign.scenario,
            tintColor: PreparationDesign.scenarioTint,
            assetPath: 'assets/images/scenes/travel-scene.jpg',
            onPressed: () => setState(() => _selectedHub = _PracticeHub.life),
          ),
        ],
      ],
    );
  }

  Widget _buildHub(PreparationController controller, _PracticeHub hub) {
    final scenes = _scenesForHub(controller.scenes, hub);
    final ielts = widget.ieltsController;
    final routeKey = hub.name;
    return ListView(
      key: Key('preparation-hub-list-$routeKey'),
      primary: false,
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: !widget.showBackButton,
        top: 8,
      ),
      children: [
        SpeakUpNavigationHeader(
          title: _practiceHubLabel(hub),
          backButtonKey: const Key('preparation-back-to-families'),
          titleKey: Key('practice-hub-title-${hub.name}'),
          onBack: () => setState(() => _selectedHub = null),
          trailing: hub == _PracticeHub.interview
              ? InterviewCatalogCreateButton(
                  onPressed: widget.onOpenJobPreparation,
                )
              : null,
        ),
        SizedBox(height: hub == _PracticeHub.interview ? 20 : 16),
        if (hub == _PracticeHub.interview)
          InterviewCatalog(
            plans: widget.jobPreparationController?.interviewPlans ?? const [],
            loading: widget.jobPreparationController?.plansLoading ?? false,
            errorMessage: widget.jobPreparationController?.plansErrorMessage,
            onCreatePressed: widget.onOpenJobPreparation,
            onPlanPressed: (plan) => widget.onOpenInterviewPlan?.call(plan.id),
            onPlanDeleted: (plan) => unawaited(
              widget.jobPreparationController?.deleteInterviewPlan(plan.id),
            ),
            onRetry: () => unawaited(
              widget.jobPreparationController?.loadInterviewPlans(force: true),
            ),
          )
        else if (hub == _PracticeHub.ielts)
          IeltsCatalog(
            controller: ielts!,
            scenes: scenes,
            onFullMockPressed: (scene) =>
                unawaited(_startIeltsFullMock(controller, scene)),
            onRetry: ielts.retryLoad,
            onSelectionPressed: (scene, mode, selection) => unawaited(
              _startSceneDirectly(
                controller,
                scene,
                practiceMode: mode,
                ieltsSelection: selection,
              ),
            ),
          )
        else
          ScenarioCatalog(
            scenes: scenes,
            onScenePressed: (scene) => unawaited(
              _startSceneDirectly(
                controller,
                scene,
                requireScenarioPreparation: true,
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildPreview() {
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
          SpeakUpNavigationHeader(
            title: _practiceHubLabel(selectedHub),
            backButtonKey: const Key('preparation-back-to-families'),
            titleKey: Key('practice-hub-title-${selectedHub.name}'),
            onBack: () => setState(() => _selectedHub = null),
            trailing: selectedHub == _PracticeHub.interview
                ? InterviewCatalogCreateButton(
                    onPressed: widget.onOpenJobPreparation,
                  )
                : null,
          ),
          SizedBox(height: selectedHub == _PracticeHub.interview ? 20 : 16),
          const PreparationCatalogEmpty(message: '连接场景服务后即可查看练习。'),
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
        if (!widget.previewMode) ...[
          const SizedBox(height: 8),
          const Text(
            '练习内容暂时无法加载，请稍后重试。',
            key: Key('practice-availability-message'),
            style: PreparationDesign.body,
          ),
        ],
        const SizedBox(height: 16),
        _PracticeHubEntry(
          key: const Key('practice-hub-interview'),
          title: '英文面试',
          description: '创建并管理你的模拟面试',
          icon: Icons.work_outline_rounded,
          accentColor: PreparationDesign.interview,
          tintColor: PreparationDesign.interviewTint,
          assetPath: 'assets/images/scenes/interview-hero.jpg',
          onPressed: _openInterviewCatalog,
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
          key: const Key('practice-hub-workplace'),
          title: '职场英语',
          description: '会议、协作与客户沟通',
          icon: Icons.business_center_outlined,
          accentColor: PreparationDesign.scenario,
          tintColor: PreparationDesign.scenarioTint,
          assetPath: 'assets/images/scenes/workplace-scene.jpg',
          onPressed: () =>
              setState(() => _selectedHub = _PracticeHub.workplace),
        ),
        const SizedBox(height: 12),
        _PracticeHubEntry(
          key: const Key('practice-hub-life'),
          title: '生活与旅行',
          description: '日常交流与出行场景实战',
          icon: Icons.travel_explore_outlined,
          accentColor: PreparationDesign.scenario,
          tintColor: PreparationDesign.scenarioTint,
          assetPath: 'assets/images/scenes/travel-scene.jpg',
          onPressed: () => setState(() => _selectedHub = _PracticeHub.life),
        ),
      ],
    );
  }
}

class _SceneLaunchStatus extends StatelessWidget {
  const _SceneLaunchStatus({
    required this.controller,
    required this.scene,
    required this.launchController,
    required this.hasPrimaryNavigation,
    required this.onBack,
    required this.onRetry,
  });

  final PreparationController controller;
  final SceneDefinition scene;
  final PreparationLaunchController? launchController;
  final bool hasPrimaryNavigation;
  final VoidCallback onBack;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final navigationLocked = launchController?.isNavigationLocked ?? false;
    final message =
        controller.errorMessage ??
        launchController?.errorMessage ??
        launchController?.workspaceErrorMessage;
    final pagePadding = PreparationDesign.pagePadding(
      context,
      hasPrimaryNavigation: hasPrimaryNavigation,
      top: 8,
    );
    return Column(
      key: const Key('preparation-scene-launch-status'),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(
            pagePadding.left,
            pagePadding.top,
            pagePadding.right,
            0,
          ),
          child: Align(
            alignment: Alignment.centerLeft,
            child: IconButton(
              key: const Key('preparation-back-to-catalog'),
              tooltip: '取消并返回',
              onPressed:
                  navigationLocked && !(launchController?.isStarting ?? false)
                  ? null
                  : onBack,
              icon: const Icon(Icons.arrow_back_rounded),
              color: PreparationDesign.ink,
              style: IconButton.styleFrom(
                backgroundColor: PreparationDesign.surface,
                side: const BorderSide(color: PreparationDesign.border),
              ),
            ),
          ),
        ),
        Expanded(
          child: ListView(
            primary: false,
            padding: EdgeInsets.fromLTRB(
              pagePadding.left,
              18,
              pagePadding.right,
              pagePadding.bottom,
            ),
            children: [
              Text(
                scene.name,
                key: const Key('preparation-scene-title'),
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
          ),
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
    return PreparationInlineFailure(
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

String _practiceHubLabel(_PracticeHub hub) {
  return switch (hub) {
    _PracticeHub.interview => '英文面试',
    _PracticeHub.ielts => 'IELTS 口语',
    _PracticeHub.workplace => '职场英语',
    _PracticeHub.life => '生活与旅行',
  };
}

List<SceneDefinition> _scenesForHub(
  List<SceneDefinition> scenes,
  _PracticeHub hub,
) {
  return scenes
      .where((scene) {
        return switch (hub) {
          _PracticeHub.interview =>
            scene.experience == PracticeExperience.interview,
          _PracticeHub.ielts =>
            scene.experience == PracticeExperience.ieltsSpeaking,
          _PracticeHub.workplace =>
            scene.experience == PracticeExperience.workplace,
          _PracticeHub.life =>
            scene.experience == PracticeExperience.lifeAndTravel,
        };
      })
      .toList(growable: false);
}
