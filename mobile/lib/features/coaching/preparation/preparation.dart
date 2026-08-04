/// Preparation module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/ielts_question_bank.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';

const _jobInterviewSceneId = 'scn_programmer_interview';
const _interviewFullSceneId = _jobInterviewSceneId;
const _ieltsFullSceneId = 'scn_ielts_speaking_full';
const _ieltsSceneIds = <String>{
  'scn_ielts_speaking_part_1',
  'scn_ielts_speaking_part_2',
  'scn_ielts_speaking_part_3',
  _ieltsFullSceneId,
};
const _reservedCatalogSceneIds = <String>{
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
    this.practiceController,
    this.preparationController,
    this.launchController,
    this.onOpenJobPreparation,
    this.onSceneSelected,
    this.onPracticeStarted,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final PracticeController? practiceController;
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
  IeltsPracticeMode? _selectedIeltsSection;
  IeltsPracticeSelection? _launchingIeltsSelection;
  bool _handlingIeltsNavigation = false;

  @override
  void initState() {
    super.initState();
    widget.practiceController?.addListener(_rebuild);
    widget.preparationController?.addListener(_rebuild);
    widget.launchController?.addListener(_rebuild);
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
    if (oldWidget.launchController != widget.launchController) {
      oldWidget.launchController?.removeListener(_rebuild);
      widget.launchController?.addListener(_rebuild);
      _backgroundController?.dispose();
      _backgroundController = _newBackgroundController(widget.launchController);
    }
  }

  @override
  void dispose() {
    widget.practiceController?.removeListener(_rebuild);
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
      final scene = _ieltsSceneForMode(
        _scenesForHub(controller.scenes, _PracticeHub.ielts),
        selection.mode,
      );
      if (scene != null) {
        await _startSceneDirectly(
          controller,
          scene,
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
    if (launch.backgroundSummary.trim().isEmpty) {
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
    );
    if (started && mounted) {
      final bootstrap = launch.bootstrap;
      if (bootstrap != null && ieltsSelection != null) {
        await catalog.beginIeltsSession(bootstrap.session.id, ieltsSelection);
      }
      catalog.showSceneList();
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
      catalog?.showSceneList();
      setState(() {
        _selectedHub = null;
        _selectedIeltsSection = null;
        _launchingIeltsSelection = null;
      });
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _startSceneDirectly(
    PreparationController controller,
    SceneDefinition scene, {
    IeltsPracticeSelection? ieltsSelection,
    bool forceReplaceCurrentPractice = false,
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
              : controller.ieltsSelectionForSession(resumableSessionId);
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
    final configured = controller.selectRecommendedConfiguration();
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
    );
  }

  Future<void> _startIeltsFullMock(
    PreparationController controller,
    SceneDefinition scene,
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
    await _startSceneDirectly(controller, scene, ieltsSelection: selection);
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

  void _handleBack(PreparationController? controller) {
    if (widget.launchController?.isSelectionLocked ?? false) {
      return;
    }
    if (controller?.selectedScene != null) {
      widget.launchController?.selectionChanged();
      controller?.showSceneList();
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
        controller?.selectedScene != null || _selectedHub != null;
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
    final selectedScene = controller.selectedScene;
    if (selectedScene != null) {
      return _SceneLaunchStatus(
        controller: controller,
        scene: selectedScene,
        launchController: widget.launchController,
        hasPrimaryNavigation: !widget.showBackButton,
        onBack: () => controller.showSceneList(),
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
    final scenes = _scenesForHub(controller.scenes, hub);
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
            scenes: scenes,
            onScenePressed: (scene) =>
                unawaited(_startSceneDirectly(controller, scene)),
            onOpenJobPreparation: widget.onOpenJobPreparation,
          )
        else if (hub == _PracticeHub.ielts)
          if (ieltsSection == null)
            _IeltsHub(
              scenes: scenes,
              onFullMockPressed: (scene) =>
                  unawaited(_startIeltsFullMock(controller, scene)),
              onPartPressed: (scene) {
                final mode = _ieltsModeForScene(scene.id);
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
              scene: _ieltsSceneForMode(scenes, ieltsSection),
              onRetry: controller.loadIeltsQuestionBankIfNeeded,
              onSelectionPressed: (scene, selection) => unawaited(
                _startSceneDirectly(
                  controller,
                  scene,
                  ieltsSelection: selection,
                ),
              ),
            )
        else
          _RoleplayHub(
            scenes: scenes,
            onScenePressed: (scene) =>
                unawaited(_startSceneDirectly(controller, scene)),
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
          const _HubEmpty(message: '连接场景服务后即可查看练习。'),
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
          widget.previewMode ? '今天想练什么？' : '练习内容暂时无法加载，请稍后重试。',
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
    final launchLocked = launchController?.isSelectionLocked ?? false;
    final message =
        controller.errorMessage ??
        launchController?.errorMessage ??
        launchController?.workspaceErrorMessage;
    return ListView(
      key: const Key('preparation-scene-launch-status'),
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
    required this.scenes,
    required this.onScenePressed,
    required this.onOpenJobPreparation,
  });

  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onScenePressed;
  final VoidCallback? onOpenJobPreparation;

  @override
  Widget build(BuildContext context) {
    final fullScene = _sceneById(scenes, _interviewFullSceneId);
    const knownSceneIds = <String>{
      _interviewFullSceneId,
      'scn_interview_self_introduction',
      'scn_interview_recruiter_screening',
      'scn_interview_behavioral',
      'scn_interview_system_design_spoken',
      'scn_interview_hiring_manager',
    };
    final dedicatedScenes = scenes
        .where(
          (scene) =>
              scene.id != _interviewFullSceneId &&
              !_reservedCatalogSceneIds.contains(scene.id),
        )
        .toList(growable: false);
    final additionalScenes = scenes
        .where(
          (scene) =>
              !knownSceneIds.contains(scene.id) &&
              !_reservedCatalogSceneIds.contains(scene.id),
        )
        .toList(growable: false);
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
                scenes: _scenesByIds(scenes, const [
                  'scn_interview_recruiter_screening',
                  'scn_interview_self_introduction',
                ]),
              ),
              (
                id: 'behavioral',
                title: '行为面试',
                caption: '经历、行动与结果',
                icon: Icons.forum_outlined,
                scenes: _scenesByIds(scenes, const [
                  'scn_interview_behavioral',
                ]),
              ),
              (
                id: 'professional',
                title: '岗位专业面试',
                caption: '项目与专业表达',
                icon: Icons.laptop_mac_outlined,
                scenes: [
                  if (onOpenJobPreparation != null)
                    ..._scenesByIds(scenes, const [_jobInterviewSceneId]),
                  ..._scenesByIds(scenes, const [
                    'scn_interview_system_design_spoken',
                  ]),
                  ...additionalScenes,
                ],
              ),
              (
                id: 'manager',
                title: 'Hiring Manager',
                caption: '岗位匹配与业务影响',
                icon: Icons.supervisor_account_outlined,
                scenes: _scenesByIds(scenes, const [
                  'scn_interview_hiring_manager',
                ]),
              ),
            ]
            .where((mode) => mode.scenes.isNotEmpty)
            .toList(growable: false);
    if (fullScene == null && dedicatedScenes.isEmpty && modes.isEmpty) {
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
        if (onOpenJobPreparation != null || fullScene != null) ...[
          _FeaturedScene(
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

class _IeltsHub extends StatelessWidget {
  const _IeltsHub({
    required this.scenes,
    required this.onFullMockPressed,
    required this.onPartPressed,
  });

  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onFullMockPressed;
  final ValueChanged<SceneDefinition> onPartPressed;

  @override
  Widget build(BuildContext context) {
    final fullScene = _sceneById(scenes, _ieltsFullSceneId);
    final partScenes = _scenesByIds(scenes, const [
      'scn_ielts_speaking_part_1',
      'scn_ielts_speaking_part_2',
      'scn_ielts_speaking_part_3',
    ]);
    if (fullScene == null && partScenes.isEmpty) {
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
        if (fullScene != null) ...[
          _FeaturedScene(
            key: const Key('ielts-mode-full'),
            eyebrow: '推荐',
            title: '一次完成三个 Part',
            description: '连续完成整套口语流程，中途不打断。',
            actionLabel: '开始完整模考',
            icon: Icons.timer_outlined,
            color: PreparationDesign.ielts,
            foregroundColor: Colors.white,
            assetPath: 'assets/images/scenes/ielts-hero.jpg',
            onPressed: () => onFullMockPressed(fullScene),
          ),
          const SizedBox(height: 28),
        ],
        if (partScenes.isNotEmpty) ...[
          const Text('分段练习', style: PreparationDesign.sectionTitle),
          const SizedBox(height: 12),
          for (var index = 0; index < partScenes.length; index++)
            _IeltsPartStep(
              scene: partScenes[index],
              partNumber: _ieltsPartNumber(partScenes[index].id),
              label: _ieltsPartLabel(partScenes[index].id),
              isLast: index == partScenes.length - 1,
              onPressed: () => onPartPressed(partScenes[index]),
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
    required this.scene,
    required this.onRetry,
    required this.onSelectionPressed,
  });

  final PreparationController controller;
  final IeltsPracticeMode mode;
  final SceneDefinition? scene;
  final Future<void> Function() onRetry;
  final void Function(SceneDefinition scene, IeltsPracticeSelection selection)
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
        if (scene == null)
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
                scene!,
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
                scene!,
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
  const _RoleplayHub({required this.scenes, required this.onScenePressed});

  final List<SceneDefinition> scenes;
  final ValueChanged<SceneDefinition> onScenePressed;

  @override
  State<_RoleplayHub> createState() => _RoleplayHubState();
}

class _RoleplayHubState extends State<_RoleplayHub> {
  static const _travelSceneIds = <String>{
    'scn_daily_airport_transport',
    'scn_daily_hotel_checkin_issue',
  };
  static const _recommendedSceneIds = <String>[
    'scn_workplace_progress_risk_update',
    'scn_workplace_meeting_disagreement',
    'scn_daily_restaurant_ordering',
    'scn_daily_airport_transport',
    'scn_daily_hotel_checkin_issue',
    'scn_daily_small_talk',
  ];

  _RoleplayFilter _filter = _RoleplayFilter.recommended;

  List<SceneDefinition> get _visibleScenes {
    final available = widget.scenes
        .where((scene) => !_reservedCatalogSceneIds.contains(scene.id))
        .toList(growable: false);
    switch (_filter) {
      case _RoleplayFilter.recommended:
        final recommended = _scenesByIds(
          available,
          _recommendedSceneIds,
        ).toList();
        for (final scene in available) {
          if (recommended.length >= 6) {
            break;
          }
          if (!recommended.any((item) => item.id == scene.id)) {
            recommended.add(scene);
          }
        }
        return recommended;
      case _RoleplayFilter.workplace:
        return available
            .where((scene) => scene.family == SceneFamily.workplace)
            .toList(growable: false);
      case _RoleplayFilter.travel:
        return available
            .where((scene) => _travelSceneIds.contains(scene.id))
            .toList(growable: false);
      case _RoleplayFilter.daily:
        return available
            .where(
              (scene) =>
                  scene.family == SceneFamily.daily &&
                  !_travelSceneIds.contains(scene.id),
            )
            .toList(growable: false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.scenes.isEmpty) {
      return const _HubEmpty(message: '当前没有可用的情景陪练。');
    }
    final visibleScenes = _visibleScenes;
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
          _RoleplaySceneGrid(
            scenes: visibleScenes,
            includeCustom: _filter == _RoleplayFilter.recommended,
            onScenePressed: widget.onScenePressed,
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

class _FeaturedScene extends StatelessWidget {
  const _FeaturedScene({
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

class _IeltsPartStep extends StatelessWidget {
  const _IeltsPartStep({
    required this.scene,
    required this.partNumber,
    required this.label,
    required this.isLast,
    required this.onPressed,
  });

  final SceneDefinition scene;
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
            key: Key('catalog-scene-${scene.id}'),
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

class _RoleplaySceneGrid extends StatelessWidget {
  const _RoleplaySceneGrid({
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
        _RoleplaySceneCard(
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

class _RoleplaySceneCard extends StatelessWidget {
  const _RoleplaySceneCard({required this.scene, required this.onPressed});

  final SceneDefinition scene;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final style = _roleplayCardStyle(scene);
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
                    _sceneFamilyIcon(scene.family),
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

String _practiceHubLabel(_PracticeHub hub) {
  return switch (hub) {
    _PracticeHub.interview => '英文面试',
    _PracticeHub.ielts => 'IELTS 口语',
    _PracticeHub.roleplay => '情景对话',
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
            scene.family == SceneFamily.interview &&
                !_reservedCatalogSceneIds.contains(scene.id),
          _PracticeHub.ielts => _ieltsSceneIds.contains(scene.id),
          _PracticeHub.roleplay =>
            (scene.family == SceneFamily.workplace ||
                    scene.family == SceneFamily.daily) &&
                !_reservedCatalogSceneIds.contains(scene.id),
        };
      })
      .toList(growable: false);
}

SceneDefinition? _sceneById(List<SceneDefinition> scenes, String id) {
  for (final scene in scenes) {
    if (scene.id == id) {
      return scene;
    }
  }
  return null;
}

List<SceneDefinition> _scenesByIds(
  List<SceneDefinition> scenes,
  List<String> ids,
) {
  final byId = {for (final scene in scenes) scene.id: scene};
  return [for (final id in ids) ?byId[id]];
}

String _ieltsPartNumber(String sceneId) {
  return switch (sceneId) {
    'scn_ielts_speaking_part_1' => '1',
    'scn_ielts_speaking_part_2' => '2',
    'scn_ielts_speaking_part_3' => '3',
    _ => '•',
  };
}

String _ieltsPartLabel(String sceneId) {
  return switch (sceneId) {
    'scn_ielts_speaking_part_1' => '熟悉话题问答',
    'scn_ielts_speaking_part_2' => '题卡陈述 · 可继续 Part 3',
    'scn_ielts_speaking_part_3' => '承接 Part 2 主题讨论',
    _ => '专项练习',
  };
}

IeltsPracticeMode? _ieltsModeForScene(String sceneId) {
  return switch (sceneId) {
    'scn_ielts_speaking_part_1' => IeltsPracticeMode.part1,
    'scn_ielts_speaking_part_2' => IeltsPracticeMode.part2,
    'scn_ielts_speaking_part_3' => IeltsPracticeMode.part3,
    _ieltsFullSceneId => IeltsPracticeMode.fullMock,
    _ => null,
  };
}

SceneDefinition? _ieltsSceneForMode(
  List<SceneDefinition> scenes,
  IeltsPracticeMode mode,
) {
  final id = switch (mode) {
    IeltsPracticeMode.fullMock => _ieltsFullSceneId,
    IeltsPracticeMode.part1 => 'scn_ielts_speaking_part_1',
    IeltsPracticeMode.part2 => 'scn_ielts_speaking_part_2',
    IeltsPracticeMode.part3 => 'scn_ielts_speaking_part_3',
  };
  return _sceneById(scenes, id);
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
_roleplayCardStyle(SceneDefinition scene) {
  final id = scene.id;
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
  if (scene.family == SceneFamily.workplace) {
    return (
      background: const Color(0xFFE8EBED),
      foreground: const Color(0xFF273238),
      icon: icon,
      category: '职场',
      assetPath: assetPath,
      imageAlignment: Alignment.topCenter,
    );
  }
  if (_RoleplayHubState._travelSceneIds.contains(id)) {
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

IconData _sceneFamilyIcon(SceneFamily family) {
  return switch (family) {
    SceneFamily.interview => Icons.work_outline_rounded,
    SceneFamily.exam => Icons.school_outlined,
    SceneFamily.workplace => Icons.groups_outlined,
    SceneFamily.daily => Icons.hotel_outlined,
  };
}

class _InlineFailure extends StatelessWidget {
  const _InlineFailure({
    required this.message,
    required this.retryKey,
    this.onRetry,
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
