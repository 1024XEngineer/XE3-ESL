/// Preparation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/ielts/ielts_catalog.dart';
import 'package:speakup/features/coaching/ielts/ielts_speech_client.dart';
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

enum _PracticeHub { interview, ielts, workplace, life }

_PracticeHub? _practiceHubForExperience(PracticeExperience? experience) =>
    switch (experience) {
      PracticeExperience.interview => _PracticeHub.interview,
      PracticeExperience.ieltsSpeaking => _PracticeHub.ielts,
      PracticeExperience.workplace => _PracticeHub.workplace,
      PracticeExperience.lifeAndTravel => _PracticeHub.life,
      null => null,
    };

enum ExistingPracticeAction { continuePractice, replace }

typedef PracticeStartedCallback = FutureOr<void> Function();

Future<ExistingPracticeAction?> showExistingPracticeActionSheet(
  BuildContext context, {
  required String? currentTitle,
  required String nextTitle,
  bool startingFullMock = false,
}) {
  final activeTitle = currentTitle ?? '上次练习';
  return showModalBottomSheet<ExistingPracticeAction>(
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
                      startingFullMock ? '开始新的模考？' : '开始新的练习？',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      startingFullMock
                          ? '你还有未完成的“$activeTitle”。可以继续当前进度，'
                                '或结束它并开始新的完整模考。'
                          : '你正在练“$activeTitle”。开始“$nextTitle”后，'
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
            ).pop(ExistingPracticeAction.continuePractice),
            child: Text(startingFullMock ? '继续上次练习' : '继续“$activeTitle”'),
          ),
          const SizedBox(height: 8),
          OutlinedButton(
            key: const Key('replace-existing-practice'),
            onPressed: () =>
                Navigator.of(context).pop(ExistingPracticeAction.replace),
            child: Text(startingFullMock ? '开始新模考' : '开始“$nextTitle”'),
          ),
          const SizedBox(height: 4),
          TextButton(
            key: const Key('cancel-existing-practice-action'),
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('取消'),
          ),
        ],
      ),
    ),
  );
}

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
  final PracticeStartedCallback? onPracticeStarted;

  @override
  State<PreparationPage> createState() => _PreparationPageState();
}

class _PreparationPageState extends State<PreparationPage> {
  TextEditingController? _backgroundController;
  _PracticeHub? _selectedHub;
  IeltsPracticeSelection? _launchingIeltsSelection;
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
    if (selection == null) {
      await widget.ieltsController?.loadIfNeeded();
      if (!mounted) {
        _handlingIeltsNavigation = false;
        return;
      }
    }
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
    List<IeltsPreparedAnswer> ieltsPreparedAnswers = const [],
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
        ieltsPreparedAnswers: ieltsPreparedAnswers,
      ),
      replaceCurrentPractice: replaceCurrentPractice,
      scenarioContext: scenarioContext,
    );
    if (started && mounted) {
      final returnHub = _practiceHubForExperience(scene.experience);
      final bootstrap = launch.bootstrap;
      if (bootstrap != null && ieltsSelection != null) {
        await widget.ieltsController?.beginSession(
          bootstrap.session.id,
          option.mode,
          ieltsSelection,
        );
      }
      await widget.onPracticeStarted?.call();
      if (!mounted) {
        return;
      }
      catalog.showSceneList();
      setState(() {
        _selectedHub = returnHub;
        _launchingIeltsSelection = null;
      });
    }
  }

  Future<void> _retryLaunch() async {
    final started = await widget.launchController?.retry() ?? false;
    if (started && mounted) {
      final catalog = widget.preparationController;
      final returnHub = _practiceHubForExperience(
        catalog?.selectedScene?.experience,
      );
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
      await widget.onPracticeStarted?.call();
      if (!mounted) {
        return;
      }
      catalog?.showSceneList();
      setState(() {
        _selectedHub = returnHub;
        _launchingIeltsSelection = null;
      });
    }
  }

  Future<void> _startSceneDirectly(
    PreparationController controller,
    SceneDefinition scene, {
    PracticeMode? practiceMode,
    IeltsPracticeSelection? ieltsSelection,
    List<IeltsPreparedAnswer> ieltsPreparedAnswers = const [],
    bool forceReplaceCurrentPractice = false,
    bool useDefaultScenarioContext = false,
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
        final action = await showExistingPracticeActionSheet(
          context,
          currentTitle: launch?.resumablePracticeTitle,
          nextTitle: scene.name,
        );
        if (!mounted || action == null) {
          return;
        }
        if (action == ExistingPracticeAction.continuePractice) {
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
    _launchingIeltsSelection = ieltsSelection;
    await _startPractice(
      replaceCurrentPractice: replaceCurrentPractice,
      ieltsSelection: ieltsSelection,
      ieltsPreparedAnswers: ieltsPreparedAnswers,
      scenarioContext: useDefaultScenarioContext
          ? _defaultScenarioContext(controller.detail!)
          : null,
    );
  }

  Future<void> _startIeltsFullMock(
    PreparationController controller,
    SceneDefinition scene,
  ) async {
    final launch = widget.launchController;
    if (launch?.hasResumablePractice ?? false) {
      final action = await showExistingPracticeActionSheet(
        context,
        currentTitle: launch?.resumablePracticeTitle,
        nextTitle: scene.name,
        startingFullMock: true,
      );
      if (!mounted || action == null) {
        return;
      }
      if (action == ExistingPracticeAction.continuePractice) {
        await _continueCurrentPractice();
        return;
      }
      await _startSceneDirectly(
        controller,
        scene,
        practiceMode: PracticeMode.fullMock,
        forceReplaceCurrentPractice: true,
      );
      return;
    }
    await _startSceneDirectly(
      controller,
      scene,
      practiceMode: PracticeMode.fullMock,
    );
  }

  Future<void> _continueCurrentPractice() async {
    final launch = widget.launchController;
    if (launch?.hasResumablePractice ?? false) {
      final returnHub = _practiceHubForExperience(
        PracticeExperience.fromWireValue(
          launch?.resumablePracticeExperience ?? '',
        ),
      );
      final resumed = await launch!.resumeCurrentPractice();
      if (resumed && mounted) {
        await widget.onPracticeStarted?.call();
        if (!mounted) {
          return;
        }
        widget.preparationController?.showSceneList();
        setState(() => _selectedHub = returnHub);
      }
      return;
    }
    if (widget.practiceController?.hasActivePractice ?? false) {
      await widget.onPracticeStarted?.call();
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
    final Widget catalogBody;
    if (controller.isLoadingScenes) {
      catalogBody = const Center(
        child: _CatalogLoading(key: Key('preparation-catalog-loading')),
      );
    } else if (controller.errorMessage case final message?) {
      catalogBody = Align(
        alignment: Alignment.topCenter,
        child: _CatalogFailure(
          key: const Key('preparation-catalog-error'),
          message: message,
          onRetry: controller.retryLastFailure,
        ),
      );
    } else if (controller.scenes.isEmpty) {
      catalogBody = const Align(
        alignment: Alignment.topCenter,
        child: _CatalogEmpty(),
      );
    } else {
      catalogBody = _buildPracticeHubCarousel();
    }
    return Padding(
      key: const Key('preparation-catalog-list'),
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: !widget.showBackButton,
        top: 18,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const SpeakUpDisplayTitle(
            title: 'Practice',
            semanticLabel: '训练',
            key: Key('training-center-title'),
          ),
          if (widget.launchController?.workspaceErrorMessage
              case final message?)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    message,
                    key: const Key('practice-workspace-error'),
                    style: const TextStyle(
                      color: Color(0xFF9A332A),
                      height: 1.4,
                    ),
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
          const SizedBox(height: 14),
          Expanded(child: catalogBody),
        ],
      ),
    );
  }

  Widget _buildPracticeHubCarousel() {
    return _PracticeHubCarousel(
      items: [
        _PracticeHubItem(
          key: const Key('practice-hub-interview'),
          title: '英文面试',
          description: '模拟面试与轮次专项练习',
          assetPath: 'assets/images/scenes/practice-interview.webp',
          fallbackIcon: Icons.work_outline_rounded,
          onPressed: _openInterviewCatalog,
        ),
        _PracticeHubItem(
          key: const Key('practice-hub-exam'),
          title: 'IELTS 口语',
          description: 'Part 1、2、3 与完整模考',
          assetPath: 'assets/images/scenes/practice-ielts.webp',
          fallbackIcon: Icons.school_outlined,
          onPressed: () {
            final ielts = widget.ieltsController;
            if (ielts == null) {
              setState(() => _selectedHub = _PracticeHub.ielts);
              return;
            }
            setState(() => _selectedHub = _PracticeHub.ielts);
            unawaited(ielts.loadIfNeeded());
          },
        ),
        _PracticeHubItem(
          key: const Key('practice-hub-workplace'),
          title: '职场英语',
          description: '会议、协作与客户沟通',
          assetPath: 'assets/images/scenes/practice-workplace.webp',
          fallbackIcon: Icons.business_center_outlined,
          onPressed: () =>
              setState(() => _selectedHub = _PracticeHub.workplace),
        ),
        _PracticeHubItem(
          key: const Key('practice-hub-life'),
          title: '生活与旅行',
          description: '日常交流与出行场景实战',
          assetPath: 'assets/images/scenes/practice-travel.webp',
          fallbackIcon: Icons.travel_explore_outlined,
          onPressed: () => setState(() => _selectedHub = _PracticeHub.life),
        ),
      ],
    );
  }

  Widget _buildHub(PreparationController controller, _PracticeHub hub) {
    final scenes = _scenesForHub(controller.scenes, hub);
    final ielts = widget.ieltsController;
    final fullMockScene = hub == _PracticeHub.ielts
        ? ieltsSceneForMode(scenes, PracticeMode.fullMock)
        : null;
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
        _buildHubHeader(
          hub,
          trailing: fullMockScene == null
              ? null
              : IeltsFullMockButton(
                  onPressed: () =>
                      unawaited(_startIeltsFullMock(controller, fullMockScene)),
                ),
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
            answerSpeaker: widget.practiceController?.promptSpeaker,
            speechClient: widget.practiceController?.mediaClient == null
                ? null
                : WireIeltsSpeechClient(
                    widget.practiceController!.mediaClient!,
                  ),
            audioPlayer: widget.practiceController?.audioPlayer,
            onRetry: ielts.retryLoad,
            onSelectionPressed: (scene, mode, selection, preparedAnswers) =>
                unawaited(
                  _startSceneDirectly(
                    controller,
                    scene,
                    practiceMode: mode,
                    ieltsSelection: selection,
                    ieltsPreparedAnswers: preparedAnswers,
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
                useDefaultScenarioContext: true,
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
          _buildHubHeader(selectedHub),
          SizedBox(height: selectedHub == _PracticeHub.interview ? 20 : 16),
          const PreparationCatalogEmpty(message: '连接场景服务后即可查看练习。'),
        ],
      );
    }
    return Padding(
      padding: PreparationDesign.pagePadding(
        context,
        hasPrimaryNavigation: !widget.showBackButton,
        top: 18,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const SpeakUpDisplayTitle(
            title: 'Practice',
            semanticLabel: '训练',
            key: Key('training-center-title'),
          ),
          if (!widget.previewMode) ...[
            const SizedBox(height: 8),
            const Text(
              '练习内容暂时无法加载，请稍后重试。',
              key: Key('practice-availability-message'),
              style: PreparationDesign.body,
            ),
          ],
          const SizedBox(height: 14),
          Expanded(child: _buildPracticeHubCarousel()),
        ],
      ),
    );
  }

  Widget _buildHubHeader(_PracticeHub hub, {Widget? trailing}) {
    return SpeakUpNavigationHeader(
      title: _practiceHubDisplayTitle(hub),
      semanticLabel: _practiceHubLabel(hub),
      titleStyle: SpeakUpDesign.secondaryDisplayTitle,
      backButtonKey: const Key('preparation-back-to-families'),
      titleKey: Key('practice-hub-title-${hub.name}'),
      onBack: () => setState(() => _selectedHub = null),
      trailing:
          trailing ??
          (hub == _PracticeHub.interview
              ? InterviewCatalogCreateButton(
                  onPressed: widget.onOpenJobPreparation,
                )
              : null),
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

class _PracticeHubCarousel extends StatefulWidget {
  const _PracticeHubCarousel({required this.items});

  final List<_PracticeHubItem> items;

  @override
  State<_PracticeHubCarousel> createState() => _PracticeHubCarouselState();
}

class _PracticeHubCarouselState extends State<_PracticeHubCarousel> {
  late final PageController _controller;
  int _currentPage = 0;

  @override
  void initState() {
    super.initState();
    _controller = PageController(initialPage: 1, viewportFraction: 0.88);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final pageCount = widget.items.length + 2;
    return Column(
      children: [
        Expanded(
          child: PageView.builder(
            key: const Key('practice-hub-carousel'),
            controller: _controller,
            allowImplicitScrolling: true,
            itemCount: pageCount,
            onPageChanged: _handlePageChanged,
            itemBuilder: (context, page) => Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6),
              child: _PracticeHubEntry(
                item: widget.items[_itemIndexForPage(page)],
                entryKey: _entryKeyForPage(page),
              ),
            ),
          ),
        ),
        const SizedBox(height: 12),
        Semantics(
          label: '第 ${_currentPage + 1} 页，共 ${widget.items.length} 页',
          liveRegion: true,
          child: ExcludeSemantics(
            child: Row(
              key: const Key('practice-hub-page-indicator'),
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                for (var index = 0; index < widget.items.length; index++)
                  AnimatedContainer(
                    key: Key('practice-hub-page-dot-$index'),
                    duration: const Duration(milliseconds: 180),
                    width: index == _currentPage ? 18 : 6,
                    height: 6,
                    margin: const EdgeInsets.symmetric(horizontal: 3),
                    decoration: BoxDecoration(
                      color: index == _currentPage
                          ? PreparationDesign.ink
                          : PreparationDesign.border,
                      borderRadius: BorderRadius.circular(3),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  int _itemIndexForPage(int page) {
    if (page == 0) {
      return widget.items.length - 1;
    }
    if (page == widget.items.length + 1) {
      return 0;
    }
    return page - 1;
  }

  Key _entryKeyForPage(int page) {
    if (page == 0) {
      return const Key('practice-hub-loop-leading');
    }
    if (page == widget.items.length + 1) {
      return const Key('practice-hub-loop-trailing');
    }
    return widget.items[page - 1].key;
  }

  void _handlePageChanged(int page) {
    final logicalPage = _itemIndexForPage(page);
    if (_currentPage != logicalPage) {
      setState(() => _currentPage = logicalPage);
    }
    if (page == 0 || page == widget.items.length + 1) {
      final destination = page == 0 ? widget.items.length : 1;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _controller.hasClients) {
          _controller.jumpToPage(destination);
        }
      });
    }
  }
}

class _PracticeHubItem {
  const _PracticeHubItem({
    required this.key,
    required this.title,
    required this.description,
    required this.assetPath,
    required this.fallbackIcon,
    required this.onPressed,
  });

  final Key key;
  final String title;
  final String description;
  final String assetPath;
  final IconData fallbackIcon;
  final VoidCallback onPressed;
}

class _PracticeHubEntry extends StatelessWidget {
  const _PracticeHubEntry({required this.item, required this.entryKey});

  final _PracticeHubItem item;
  final Key entryKey;

  @override
  Widget build(BuildContext context) {
    return Material(
      key: entryKey,
      color: PreparationDesign.surfaceMuted,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(PreparationDesign.radiusHero),
        side: const BorderSide(color: PreparationDesign.border),
      ),
      child: Semantics(
        button: true,
        label: '${item.title}。${item.description}',
        onTap: item.onPressed,
        excludeSemantics: true,
        child: InkWell(
          onTap: item.onPressed,
          child: Stack(
            fit: StackFit.expand,
            children: [
              Image.asset(
                item.assetPath,
                fit: BoxFit.cover,
                alignment: Alignment.topCenter,
                errorBuilder: (_, _, _) => ColoredBox(
                  color: PreparationDesign.surfaceMuted,
                  child: Icon(
                    item.fallbackIcon,
                    color: PreparationDesign.ink,
                    size: 44,
                  ),
                ),
              ),
              const DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment(0, -0.15),
                    end: Alignment.bottomCenter,
                    colors: [Colors.transparent, Color(0xD9000000)],
                  ),
                ),
              ),
              Positioned(
                left: 22,
                right: 22,
                bottom: 22,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      item.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: PreparationDesign.pageTitle.copyWith(
                        color: Colors.white,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      item.description,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: PreparationDesign.body.copyWith(
                        color: Colors.white.withValues(alpha: 0.82),
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: 16),
                    Container(
                      width: double.infinity,
                      constraints: const BoxConstraints(minHeight: 48),
                      padding: const EdgeInsets.symmetric(
                        horizontal: 20,
                        vertical: 12,
                      ),
                      decoration: BoxDecoration(
                        color: Colors.white.withValues(alpha: 0.94),
                        borderRadius: BorderRadius.circular(
                          PreparationDesign.radiusControl,
                        ),
                      ),
                      child: Text(
                        '开始练习',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        textAlign: TextAlign.center,
                        style: PreparationDesign.cardTitle,
                      ),
                    ),
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

String _practiceHubLabel(_PracticeHub hub) {
  return switch (hub) {
    _PracticeHub.interview => '英文面试',
    _PracticeHub.ielts => 'IELTS 口语',
    _PracticeHub.workplace => '职场英语',
    _PracticeHub.life => '生活与旅行',
  };
}

ScenarioPreparationContext _defaultScenarioContext(SceneDefinition scene) {
  final prompt = scene.prompt;
  return ScenarioPreparationContext(
    situation: prompt.publicSceneBrief.trim(),
    userRole: prompt.userRole.trim(),
    counterpartRole: prompt.aiRole.trim(),
    goal: prompt.practiceGoal.trim(),
    counterpartPersona: prompt.personaSummary.trim(),
  );
}

String _practiceHubDisplayTitle(_PracticeHub hub) {
  return switch (hub) {
    _PracticeHub.interview => 'Interview',
    _PracticeHub.ielts => 'IELTS',
    _PracticeHub.workplace => 'Workplace',
    _PracticeHub.life => 'Travel',
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
