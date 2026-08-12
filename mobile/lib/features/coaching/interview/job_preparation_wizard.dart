import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/resume/resume_controller.dart';
import 'package:speakup/resume/resume_models.dart';

final _jobDescriptionMarkers = RegExp(
  r"岗位职责|工作职责|任职要求|职位要求|responsibilities?|requirements?|qualifications?|what you(?:'|’)ll do|what we(?:'|’)re looking for",
  caseSensitive: false,
);

JobTargetSource _jobTargetSource(String value) {
  if (value.contains('\n') ||
      value.runes.length > 80 ||
      _jobDescriptionMarkers.hasMatch(value)) {
    return JobTargetSource.jobDescription;
  }
  return JobTargetSource.quickStart;
}

class JobPreparationWizard extends StatefulWidget {
  const JobPreparationWizard({
    required this.controller,
    this.catalogController,
    this.resumeController,
    this.onPracticeStarted,
    super.key,
  });

  final JobPreparationController controller;
  final PreparationController? catalogController;
  final ResumeController? resumeController;
  final VoidCallback? onPracticeStarted;

  @override
  State<JobPreparationWizard> createState() => _JobPreparationWizardState();
}

class _JobPreparationWizardState extends State<JobPreparationWizard> {
  late final TextEditingController _jobInput;
  int? _selectedTurnLimit;
  bool _catalogSyncing = false;
  bool _practiceStarted = false;
  bool _exitInFlight = false;
  bool _exitApproved = false;

  @override
  void initState() {
    super.initState();
    final input = widget.controller.input;
    _jobInput = TextEditingController(
      text: input.jobDescription ?? input.jobTitle,
    );
    widget.controller.addListener(_rebuild);
    widget.catalogController?.addListener(_rebuild);
    widget.resumeController?.addListener(_rebuild);
    unawaited(_prepareCatalog());
    if (widget.resumeController case final resumes?) {
      _loadResumesAfterBuild(resumes);
    }
  }

  @override
  void didUpdateWidget(covariant JobPreparationWizard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller == widget.controller) {
      if (oldWidget.catalogController != widget.catalogController) {
        oldWidget.catalogController?.removeListener(_rebuild);
        widget.catalogController?.addListener(_rebuild);
        unawaited(_prepareCatalog());
      }
      if (oldWidget.resumeController != widget.resumeController) {
        oldWidget.resumeController?.removeListener(_rebuild);
        widget.resumeController?.addListener(_rebuild);
        if (widget.resumeController case final resumes?) {
          _loadResumesAfterBuild(resumes);
        }
      }
      return;
    }
    oldWidget.controller.removeListener(_rebuild);
    widget.controller.addListener(_rebuild);
    if (oldWidget.catalogController != widget.catalogController) {
      oldWidget.catalogController?.removeListener(_rebuild);
      widget.catalogController?.addListener(_rebuild);
    }
    if (oldWidget.resumeController != widget.resumeController) {
      oldWidget.resumeController?.removeListener(_rebuild);
      widget.resumeController?.addListener(_rebuild);
      if (widget.resumeController case final resumes?) {
        _loadResumesAfterBuild(resumes);
      }
    }
    _syncInput();
    unawaited(_prepareCatalog());
  }

  @override
  void dispose() {
    widget.controller.removeListener(_rebuild);
    widget.catalogController?.removeListener(_rebuild);
    widget.resumeController?.removeListener(_rebuild);
    _jobInput.dispose();
    super.dispose();
  }

  void _rebuild() {
    if (!mounted) {
      return;
    }
    _syncInput();
    unawaited(_prepareCatalog());
    setState(() {});
  }

  void _loadResumesAfterBuild(ResumeController resumes) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && widget.resumeController == resumes) {
        unawaited(resumes.load());
      }
    });
  }

  void _syncInput() {
    final input = widget.controller.input;
    _replaceText(_jobInput, input.jobDescription ?? input.jobTitle);
  }

  Future<void> _prepareCatalog() async {
    final catalog = widget.catalogController;
    final candidate = widget.controller.candidate;
    if (!mounted ||
        _practiceStarted ||
        catalog == null ||
        candidate == null ||
        _catalogSyncing) {
      return;
    }
    _catalogSyncing = true;
    try {
      await catalog.loadIfNeeded();
      if (!mounted ||
          _practiceStarted ||
          widget.controller.candidate != candidate) {
        return;
      }
      final sceneId = candidate.catalogRecommendation.sceneId;
      final scene = catalog.scenes
          .where((item) => item.id == sceneId)
          .firstOrNull;
      if (scene == null) {
        return;
      }
      if (catalog.selectedScene?.id != scene.id || catalog.detail == null) {
        await catalog.selectScene(scene);
      }
      if (!mounted ||
          _practiceStarted ||
          widget.controller.candidate != candidate) {
        if (_practiceStarted && catalog.selectedScene != null) {
          catalog.showSceneList();
        }
        return;
      }
      final plan = widget.controller.plan;
      final roleId =
          plan?.selectedRoles.single.id ??
          candidate.catalogRecommendation.selectedRoleIds.firstOrNull;
      final role = catalog.roles.where((item) => item.id == roleId).firstOrNull;
      if (role != null && catalog.selectedRole?.id != role.id) {
        catalog.selectRole(role);
      }
      final optionId =
          plan?.practiceOption.id ??
          candidate.catalogRecommendation.practiceOptionId;
      final option = catalog.availableOptions
          .where((item) => item.id == optionId)
          .firstOrNull;
      if (option != null && catalog.selectedOption?.id != option.id) {
        catalog.selectOption(option);
      }
    } finally {
      if (_practiceStarted && catalog.selectedScene != null) {
        catalog.showSceneList();
      }
      _catalogSyncing = false;
    }
  }

  void _replaceText(TextEditingController controller, String? value) {
    final next = value ?? '';
    if (controller.text == next) {
      return;
    }
    controller.value = TextEditingValue(
      text: next,
      selection: TextSelection.collapsed(offset: next.length),
    );
  }

  JobTargetInput _currentInput() {
    String? normalized(String value) {
      final result = value.trim();
      return result.isEmpty ? null : result;
    }

    final value = normalized(_jobInput.text);
    final resolvedSource = _jobTargetSource(value ?? '');
    return JobTargetInput(
      source: resolvedSource,
      jobTitle: resolvedSource == JobTargetSource.quickStart ? value : null,
      jobDescription: resolvedSource == JobTargetSource.jobDescription
          ? value
          : null,
    );
  }

  void _commitInput() {
    widget.controller.updateInput(_currentInput());
  }

  Future<void> _pickTemporaryResume() async {
    await widget.resumeController?.pickTemporary();
    if (widget.resumeController?.temporaryItem != null) {
      widget.controller.selectResume(null);
    }
    for (
      var attempt = 0;
      mounted && !widget.controller.openedSavedPlan && attempt < 75;
      attempt++
    ) {
      await _refreshTemporaryResume();
      final status = widget.resumeController?.temporaryItem?.parseStatus;
      if (status == null ||
          status == ResumeParseStatus.ready ||
          status == ResumeParseStatus.failed) {
        return;
      }
      await Future<void>.delayed(const Duration(seconds: 1));
    }
  }

  Future<void> _refreshTemporaryResume() async {
    final resumes = widget.resumeController;
    if (resumes == null) return;
    await resumes.refreshTemporary();
    final resume = resumes.temporaryItem;
    final revision = resume?.currentRevision;
    if (resume?.parseStatus == ResumeParseStatus.ready && revision != null) {
      widget.controller.selectResume(
        JobPreparationResumeSelection(
          resumeId: resume!.id,
          revision: revision,
          resourceVersion: resume.version,
          temporary: true,
          title: resume.title,
        ),
      );
    }
  }

  Widget _buildResumeSource() {
    final resumes = widget.resumeController;
    if (resumes == null) return const SizedBox.shrink();
    final temporary = resumes.temporaryItem;
    return _InputSectionCard(
      key: const Key('job-resume-source-card'),
      icon: Icons.description_outlined,
      title: '上传简历',
      badge: '可选',
      description: '上传本次面试使用的 PDF 简历，让问题更贴合你的经历。',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          OutlinedButton.icon(
            key: const Key('temporary-resume-upload-button'),
            onPressed: widget.controller.isBusy
                ? null
                : () => unawaited(_pickTemporaryResume()),
            icon: const Icon(Icons.upload_file_rounded),
            label: Text(temporary == null ? '上传 PDF 简历' : '更换 PDF 简历'),
          ),
          if (temporary != null) ...[
            const SizedBox(height: 10),
            _Notice(
              key: const Key('temporary-resume-status'),
              icon: switch (temporary.parseStatus) {
                ResumeParseStatus.ready => Icons.check_circle_outline,
                ResumeParseStatus.failed => Icons.error_outline,
                _ => Icons.hourglass_top_rounded,
              },
              text: switch (temporary.parseStatus) {
                ResumeParseStatus.ready => '临时简历已解析，可用于本次面试。',
                ResumeParseStatus.failed => '临时简历解析失败，可以重试或重新上传。',
                _ => '临时简历正在解析，完成后即可继续。',
              },
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              children: [
                TextButton(
                  onPressed: () => unawaited(_refreshTemporaryResume()),
                  child: const Text('刷新状态'),
                ),
                if (temporary.parseStatus == ResumeParseStatus.failed)
                  TextButton(
                    onPressed: () => unawaited(
                      resumes.retryTemporaryParse().then(
                        (_) => _refreshTemporaryResume(),
                      ),
                    ),
                    child: const Text('重试解析'),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _startPractice() async {
    final started = await widget.controller.startPractice();
    if (started && mounted) {
      _practiceStarted = true;
      widget.catalogController?.showSceneList();
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _createAndStartPractice() async {
    _commitInput();
    final temporarySelected =
        widget.controller.resumeSelection?.temporary == true;
    final started = await widget.controller.createAndStartPractice();
    final snapshotCaptured =
        widget.controller.plan != null ||
        widget.controller.operationStage == JobPreparationOperationStage.plan ||
        widget.controller.operationStage ==
            JobPreparationOperationStage.session ||
        widget.controller.operationStage == JobPreparationOperationStage.voice;
    if (temporarySelected && snapshotCaptured) {
      await widget.resumeController?.refreshTemporary();
      await widget.resumeController?.deleteTemporary();
    }
    if (started && mounted) {
      _practiceStarted = true;
      widget.catalogController?.showSceneList();
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _requestExit() async {
    if (!mounted ||
        widget.controller.isBusy ||
        _exitInFlight ||
        _exitApproved) {
      return;
    }
    _exitInFlight = true;
    final approved = await widget.controller.parkCurrentPractice();
    if (!mounted) {
      return;
    }
    _exitInFlight = false;
    if (!approved) {
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('面试准备已保留，请稍后再返回。')));
      setState(() {});
      return;
    }
    _exitApproved = true;
    setState(() {});
    await WidgetsBinding.instance.endOfFrame;
    if (mounted) {
      await Navigator.of(context).maybePop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    return PopScope<void>(
      canPop: !controller.isBusy && _exitApproved,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          unawaited(_requestExit());
        }
      },
      child: Scaffold(
        key: const Key('job-preparation-wizard'),
        appBar: AppBar(
          title: const Text('准备英文面试'),
          leading: IconButton(
            key: const Key('job-wizard-close'),
            tooltip: '关闭准备流程',
            onPressed: controller.isBusy
                ? null
                : () => unawaited(_requestExit()),
            icon: const Icon(Icons.close_rounded),
          ),
        ),
        body: SafeArea(
          top: false,
          child: Column(
            children: [
              if (controller.isBusy)
                LinearProgressIndicator(
                  key: const Key('job-wizard-progress'),
                  minHeight: 2,
                  semanticsLabel: _busyLabel(controller.operationStage),
                ),
              if (controller.isBusy)
                Padding(
                  padding: const EdgeInsets.fromLTRB(20, 8, 20, 0),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: Text(
                      _busyLabel(controller.operationStage),
                      key: const Key('job-wizard-progress-label'),
                      style: SpeakUpDesign.meta,
                    ),
                  ),
                ),
              Expanded(
                child: controller.openedSavedPlan
                    ? _buildPreview(controller)
                    : _buildInput(controller),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildInput(JobPreparationController controller) {
    return Column(
      children: [
        Expanded(
          child: ListView(
            key: const Key('job-wizard-input-step'),
            keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
            padding: EdgeInsets.fromLTRB(
              SpeakUpDesign.horizontalInset(context),
              SpeakUpDesign.space16,
              SpeakUpDesign.horizontalInset(context),
              SpeakUpDesign.space24,
            ),
            children: [
              const Text('准备你的下一场面试', style: SpeakUpDesign.pageTitle),
              const SizedBox(height: SpeakUpDesign.space8),
              const Text(
                '提供岗位信息和简历，AI 会自动分析并生成专属面试问题。',
                style: SpeakUpDesign.body,
              ),
              const SizedBox(height: SpeakUpDesign.space24),
              _InputSectionCard(
                icon: Icons.work_outline_rounded,
                title: '岗位信息',
                description: '输入职位名称，或直接粘贴完整的职位 JD。',
                child: _Field(
                  key: const Key('job-input-field'),
                  controller: _jobInput,
                  label: '职位名称或 JD',
                  hint: '例如：后端工程师\n或粘贴岗位职责、任职要求等信息',
                  maxLines: 6,
                  onChanged: (_) => _commitInput(),
                ),
              ),
              const SizedBox(height: SpeakUpDesign.space16),
              _buildResumeSource(),
              if (controller.agentIntentPrefill case final prefill?) ...[
                const SizedBox(height: 14),
                _AgentIntentCard(
                  text: prefill,
                  onApply: controller.applyAgentIntentPrefill,
                  onDismiss: controller.dismissAgentIntentPrefill,
                ),
              ],
              if (controller.target != null &&
                  controller.candidate == null) ...[
                const SizedBox(height: 14),
                const _Notice(
                  icon: Icons.refresh_rounded,
                  text: '岗位信息已修改，之前的分析、确认和计划预览已失效，需要重新分析。',
                ),
              ],
              if (controller.hasRestorableDraft) ...[
                const SizedBox(height: 14),
                _DraftCard(
                  onResume: controller.isBusy ? null : controller.resumeDraft,
                  onDiscard: controller.isBusy ? null : controller.discardDraft,
                ),
              ],
              if (controller.errorMessage case final message?)
                _ErrorCard(
                  message: message,
                  onRetry: controller.canRetry ? controller.retry : null,
                ),
            ],
          ),
        ),
        Container(
          padding: EdgeInsets.fromLTRB(
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space12,
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space16,
          ),
          decoration: const BoxDecoration(
            color: SpeakUpDesign.surface,
            border: Border(top: BorderSide(color: SpeakUpDesign.border)),
          ),
          child: FilledButton.icon(
            key: const Key('create-and-start-interview-button'),
            onPressed: controller.isBusy
                ? null
                : () => unawaited(_createAndStartPractice()),
            icon: const Icon(Icons.auto_awesome_rounded),
            label: const Text('开始面试'),
            style: _primaryButtonStyle,
          ),
        ),
      ],
    );
  }

  Widget _buildPreview(JobPreparationController controller) {
    final plan = controller.plan;
    if (plan == null) {
      return const Center(child: Text('计划预览已失效，请重新生成。'));
    }
    final jobCandidate = plan.preparationSnapshot.jobTargetCandidate;
    final minutes = (plan.sessionPolicy.suggestedDurationSeconds / 60).ceil();
    return ListView(
      key: const Key('job-wizard-preview-step'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
      children: [
        Text(
          '模拟面试详情',
          style: Theme.of(
            context,
          ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text('开始后才会创建正式练习记录。', style: SpeakUpDesign.body),
        const SizedBox(height: 18),
        _SummaryCard(
          key: const Key('job-plan-summary'),
          title: plan.sceneSelection.scene.name,
          subtitle: plan.practiceOption.displayName,
          rows: [
            ('岗位', jobCandidate?.jobTitle ?? plan.sceneSelection.scene.name),
            ('预计时长', '约 $minutes 分钟'),
            (
              '有效轮次',
              '${plan.sessionPolicy.minEffectiveTurns}–'
                  '${plan.sessionPolicy.maxEffectiveTurns} 轮',
            ),
            ('训练视角', plan.selectedRoles.single.displayName),
            (
              '重点',
              plan.practiceObjectives.map((item) => item.description).join('、'),
            ),
          ],
        ),
        const SizedBox(height: 14),
        ExpansionTile(
          key: const Key('job-plan-advanced-settings'),
          tilePadding: const EdgeInsets.symmetric(horizontal: 4),
          title: const Text('高级设置'),
          subtitle: const Text('面试官视角由服务端按岗位推荐'),
          children: [_buildAdvancedSettings(controller, plan)],
        ),
        if (controller.errorMessage case final message?)
          _ErrorCard(
            key: const Key('job-preview-error'),
            message: message,
            onRetry: controller.canRetry ? controller.retry : null,
          ),
        const SizedBox(height: 24),
        FilledButton.icon(
          key: const Key('start-job-practice-button'),
          onPressed: controller.isBusy ? null : _startPractice,
          icon: const Icon(Icons.mic_none_rounded),
          label: Text(controller.bootstrap == null ? '开始模拟面试' : '重新连接语音练习'),
          style: _primaryButtonStyle,
        ),
      ],
    );
  }

  Widget _buildAdvancedSettings(
    JobPreparationController controller,
    PracticePlan plan,
  ) {
    final catalog = widget.catalogController;
    final roles = catalog?.roles ?? const <RoleDefinition>[];
    final options = catalog?.availableOptions ?? const <PracticeOption>[];
    final selectedRole = catalog?.selectedRole;
    final selectedOption = catalog?.selectedOption;
    final minTurns = plan.sessionPolicy.minEffectiveTurns;
    final maxTurns = plan.sessionPolicy.maxEffectiveTurns < 6
        ? 6
        : plan.sessionPolicy.maxEffectiveTurns;
    final selectedTurns = (_selectedTurnLimit ?? maxTurns)
        .clamp(minTurns, maxTurns)
        .toInt();
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 0, 4, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (roles.isEmpty)
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('当前视角'),
              subtitle: Text(plan.selectedRoles.single.displayName),
            )
          else
            DropdownButtonFormField<String>(
              key: const Key('job-plan-role-selector'),
              initialValue: selectedRole?.id,
              decoration: const InputDecoration(labelText: '面试官视角'),
              items: [
                for (final role in roles)
                  DropdownMenuItem(
                    value: role.id,
                    child: Text(role.displayName),
                  ),
              ],
              onChanged: controller.isBusy
                  ? null
                  : (id) {
                      final role = roles
                          .where((item) => item.id == id)
                          .firstOrNull;
                      if (role != null) {
                        catalog?.selectRole(role);
                      }
                    },
            ),
          const SizedBox(height: 12),
          if (options.isEmpty)
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('练习方式'),
              subtitle: Text(plan.practiceOption.displayName),
            )
          else
            DropdownButtonFormField<String>(
              key: const Key('job-plan-option-selector'),
              initialValue: selectedOption?.id,
              decoration: const InputDecoration(labelText: '训练重点'),
              items: [
                for (final option in options)
                  DropdownMenuItem(
                    value: option.id,
                    child: Text(option.displayName),
                  ),
              ],
              onChanged: controller.isBusy
                  ? null
                  : (id) {
                      final option = options
                          .where((item) => item.id == id)
                          .firstOrNull;
                      if (option != null) {
                        catalog?.selectOption(option);
                      }
                    },
            ),
          const SizedBox(height: 12),
          Text('最多 $selectedTurns 个有效轮次'),
          Slider(
            key: const Key('job-plan-turn-limit'),
            value: selectedTurns.toDouble(),
            min: minTurns.toDouble(),
            max: maxTurns.toDouble(),
            divisions: maxTurns - minTurns,
            label: '$selectedTurns 轮',
            onChanged: controller.isBusy
                ? null
                : (value) => setState(() => _selectedTurnLimit = value.round()),
          ),
          FilledButton.tonal(
            key: const Key('save-job-plan-revision'),
            onPressed:
                controller.isBusy ||
                    selectedRole == null ||
                    selectedOption == null
                ? null
                : () => unawaited(
                    controller.revisePreview(
                      roleDefinitionId: selectedRole.id,
                      practiceOptionId: selectedOption.id,
                      maxEffectiveTurns: selectedTurns,
                    ),
                  ),
            child: const Text('保存高级设置'),
          ),
        ],
      ),
    );
  }
}

class _InputSectionCard extends StatelessWidget {
  const _InputSectionCard({
    required this.icon,
    required this.title,
    required this.description,
    required this.child,
    this.badge,
    super.key,
  });

  final IconData icon;
  final String title;
  final String description;
  final String? badge;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: SpeakUpDesign.primaryMuted,
                    borderRadius: BorderRadius.circular(
                      SpeakUpDesign.radiusControl,
                    ),
                  ),
                  child: Icon(icon, size: 21),
                ),
                const SizedBox(width: SpeakUpDesign.space12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: Text(title, style: SpeakUpDesign.cardTitle),
                          ),
                          if (badge case final value?)
                            Container(
                              padding: const EdgeInsets.symmetric(
                                horizontal: SpeakUpDesign.space8,
                                vertical: SpeakUpDesign.space4,
                              ),
                              decoration: BoxDecoration(
                                color: SpeakUpDesign.surfaceMuted,
                                borderRadius: BorderRadius.circular(999),
                              ),
                              child: Text(value, style: SpeakUpDesign.meta),
                            ),
                        ],
                      ),
                      const SizedBox(height: SpeakUpDesign.space4),
                      Text(description, style: SpeakUpDesign.body),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: SpeakUpDesign.space16),
            child,
          ],
        ),
      ),
    );
  }
}

class _Field extends StatelessWidget {
  const _Field({
    required this.controller,
    required this.label,
    this.hint,
    this.maxLines = 1,
    this.onChanged,
    super.key,
  });

  final TextEditingController controller;
  final String label;
  final String? hint;
  final int maxLines;
  final ValueChanged<String>? onChanged;

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: controller,
      maxLines: maxLines,
      minLines: maxLines > 1 ? 2 : 1,
      textInputAction: maxLines > 1
          ? TextInputAction.newline
          : TextInputAction.next,
      onChanged: onChanged,
      decoration: InputDecoration(
        labelText: label,
        hintText: hint,
        alignLabelWithHint: maxLines > 1,
      ),
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.icon, required this.text, super.key});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20),
          const SizedBox(width: 10),
          Expanded(child: Text(text, style: const TextStyle(height: 1.4))),
        ],
      ),
    );
  }
}

class _ErrorCard extends StatelessWidget {
  const _ErrorCard({required this.message, required this.onRetry, super.key});

  final String message;
  final Future<bool> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Semantics(
        liveRegion: true,
        child: Container(
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(
            color: SpeakUpDesign.errorMuted,
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
          ),
          child: Row(
            children: [
              const Icon(Icons.error_outline_rounded),
              const SizedBox(width: 10),
              Expanded(child: Text(message)),
              if (onRetry != null)
                TextButton(
                  key: const Key('job-wizard-retry-button'),
                  onPressed: () => unawaited(onRetry!()),
                  child: const Text('重试'),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _AgentIntentCard extends StatelessWidget {
  const _AgentIntentCard({
    required this.text,
    required this.onApply,
    required this.onDismiss,
  });

  final String text;
  final VoidCallback onApply;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('agent-intent-prefill-card'),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              '从 Agent 对话带入',
              style: TextStyle(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            Text(text, maxLines: 3, overflow: TextOverflow.ellipsis),
            const SizedBox(height: 10),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(onPressed: onDismiss, child: const Text('忽略')),
                FilledButton.tonal(
                  onPressed: onApply,
                  child: const Text('填入 JD'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _DraftCard extends StatelessWidget {
  const _DraftCard({required this.onResume, required this.onDiscard});

  final Future<bool> Function()? onResume;
  final Future<bool> Function()? onDiscard;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('job-draft-card'),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              '发现未完成的准备草稿',
              style: TextStyle(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 8),
            const Text('草稿只属于当前账号。你可以继续，或安全丢弃后重新开始。'),
            const SizedBox(height: 10),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  key: const Key('discard-job-draft-button'),
                  onPressed: onDiscard == null
                      ? null
                      : () => unawaited(onDiscard!()),
                  child: const Text('丢弃'),
                ),
                FilledButton.tonal(
                  key: const Key('resume-job-draft-button'),
                  onPressed: onResume == null
                      ? null
                      : () => unawaited(onResume!()),
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, SpeakUpDesign.minTapTarget),
                  ),
                  child: const Text('继续'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _SummaryCard extends StatelessWidget {
  const _SummaryCard({
    required this.title,
    required this.subtitle,
    required this.rows,
    super.key,
  });

  final String title;
  final String subtitle;
  final List<(String, String)> rows;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: SpeakUpDesign.sectionTitle),
            if (subtitle.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(subtitle, style: SpeakUpDesign.body),
            ],
            const SizedBox(height: 14),
            for (final row in rows) ...[
              Text(row.$1, style: SpeakUpDesign.meta),
              const SizedBox(height: 3),
              Text(row.$2.isEmpty ? '—' : row.$2),
              const SizedBox(height: 12),
            ],
          ],
        ),
      ),
    );
  }
}

String _busyLabel(JobPreparationOperationStage? stage) {
  return switch (stage) {
    JobPreparationOperationStage.target => '正在保存岗位信息',
    JobPreparationOperationStage.analysis => '正在分析岗位信息',
    JobPreparationOperationStage.confirmation => '正在确认岗位分析',
    JobPreparationOperationStage.goal => '正在准备练习事项',
    JobPreparationOperationStage.profile => '正在保存个人背景',
    JobPreparationOperationStage.snapshot => '正在冻结练习资料',
    JobPreparationOperationStage.plan => '正在生成练习计划',
    JobPreparationOperationStage.session => '正在创建练习',
    JobPreparationOperationStage.voice => '正在连接语音练习',
    null => '正在处理',
  };
}

final ButtonStyle _primaryButtonStyle = FilledButton.styleFrom(
  minimumSize: const Size.fromHeight(52),
);
