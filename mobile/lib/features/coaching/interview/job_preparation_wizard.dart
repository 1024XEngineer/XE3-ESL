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

enum _JobExistingPracticeAction { cancel, continuePractice, replace }

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
  late final TextEditingController _jd;
  late final TextEditingController _title;
  late final TextEditingController _candidateTitle;
  late final TextEditingController _candidateSeniority;
  late final TextEditingController _responsibilities;
  late final TextEditingController _skills;
  late final TextEditingController _communication;
  late final TextEditingController _goals;
  JobTargetCandidate? _shownCandidate;
  int? _shownPlanRevision;
  int? _selectedTurnLimit;
  bool _catalogSyncing = false;
  bool _exitInFlight = false;
  bool _exitApproved = false;

  @override
  void initState() {
    super.initState();
    final input = widget.controller.input;
    _jd = TextEditingController(text: input.jobDescription);
    _title = TextEditingController(text: input.jobTitle);
    _candidateTitle = TextEditingController();
    _candidateSeniority = TextEditingController();
    _responsibilities = TextEditingController();
    _skills = TextEditingController();
    _communication = TextEditingController();
    _goals = TextEditingController();
    widget.controller.addListener(_rebuild);
    widget.catalogController?.addListener(_rebuild);
    widget.resumeController?.addListener(_rebuild);
    _syncCandidate();
    unawaited(_prepareCatalog());
    if (widget.resumeController case final resumes?) {
      unawaited(resumes.load());
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
          unawaited(resumes.load());
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
        unawaited(resumes.load());
      }
    }
    _syncInput();
    _syncCandidate();
    unawaited(_prepareCatalog());
  }

  @override
  void dispose() {
    widget.controller.removeListener(_rebuild);
    widget.catalogController?.removeListener(_rebuild);
    widget.resumeController?.removeListener(_rebuild);
    for (final controller in <TextEditingController>[
      _jd,
      _title,
      _candidateTitle,
      _candidateSeniority,
      _responsibilities,
      _skills,
      _communication,
      _goals,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  void _rebuild() {
    if (!mounted) {
      return;
    }
    _syncCandidate();
    unawaited(_prepareCatalog());
    setState(() {});
  }

  void _syncInput() {
    final input = widget.controller.input;
    _replaceText(_jd, input.jobDescription);
    _replaceText(_title, input.jobTitle);
  }

  void _syncCandidate() {
    final plan = widget.controller.plan;
    if (plan != null && plan.revision != _shownPlanRevision) {
      _shownPlanRevision = plan.revision;
      _selectedTurnLimit = plan.sessionPolicy.maxEffectiveTurns;
    }
    final candidate = widget.controller.candidate;
    if (candidate == null || identical(candidate, _shownCandidate)) {
      return;
    }
    _shownCandidate = candidate;
    _replaceText(_candidateTitle, candidate.jobTitle);
    _replaceText(_candidateSeniority, candidate.seniority);
    _replaceText(_responsibilities, candidate.responsibilities.join('\n'));
    _replaceText(_skills, candidate.coreSkills.join('\n'));
    _replaceText(_communication, candidate.communicationFocus.join('\n'));
    _replaceText(_goals, candidate.practiceGoals.join('\n'));
  }

  Future<void> _prepareCatalog() async {
    final catalog = widget.catalogController;
    final candidate = widget.controller.candidate;
    if (!mounted || catalog == null || candidate == null || _catalogSyncing) {
      return;
    }
    _catalogSyncing = true;
    try {
      await catalog.loadIfNeeded();
      if (!mounted || widget.controller.candidate != candidate) {
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
      if (!mounted || widget.controller.candidate != candidate) {
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

  JobTargetInput _currentInput({JobTargetSource? source}) {
    String? normalized(String value) {
      final result = value.trim();
      return result.isEmpty ? null : result;
    }

    final resolvedSource = source ?? widget.controller.input.source;
    return JobTargetInput(
      source: resolvedSource,
      jobTitle: resolvedSource == JobTargetSource.quickStart
          ? normalized(_title.text)
          : null,
      jobDescription: resolvedSource == JobTargetSource.jobDescription
          ? normalized(_jd.text)
          : null,
    );
  }

  void _commitInput() {
    widget.controller.updateInput(_currentInput());
  }

  void _selectSavedResume(ResumeItem? resume) {
    final revision = resume?.currentRevision;
    if (resume == null || revision == null) {
      widget.controller.selectResume(null);
      return;
    }
    widget.controller.selectResume(
      JobPreparationResumeSelection(
        resumeId: resume.id,
        revision: revision,
        resourceVersion: resume.version,
        temporary: false,
        title: resume.title,
      ),
    );
  }

  Future<void> _pickTemporaryResume() async {
    await widget.resumeController?.pickTemporary();
    if (widget.resumeController?.temporaryItem != null) {
      widget.controller.selectResume(null);
    }
    for (
      var attempt = 0;
      mounted &&
          widget.controller.step == JobPreparationStep.confirmation &&
          attempt < 75;
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
    final ready = resumes.items
        .where(
          (item) =>
              item.parseStatus == ResumeParseStatus.ready &&
              item.currentRevision != null,
        )
        .toList(growable: false);
    final selection = widget.controller.resumeSelection;
    final temporary = resumes.temporaryItem;
    final selectedSavedId =
        selection?.temporary == false &&
            ready.any((item) => item.id == selection?.resumeId)
        ? selection?.resumeId
        : null;
    return Card(
      key: const Key('job-resume-source-card'),
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              '简历（可选）',
              style: Theme.of(
                context,
              ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 6),
            const Text('用于生成更贴合个人经历的追问，不使用也可以继续。'),
            const SizedBox(height: 14),
            DropdownButtonFormField<String?>(
              key: ValueKey<String>(
                'saved-resume-selector-${selectedSavedId ?? 'none'}-'
                '${ready.map((item) => item.id).join(',')}',
              ),
              initialValue: selectedSavedId,
              decoration: const InputDecoration(labelText: '选择已上传的简历'),
              items: <DropdownMenuItem<String?>>[
                const DropdownMenuItem<String?>(
                  value: null,
                  child: Text('不使用简历'),
                ),
                for (final item in ready)
                  DropdownMenuItem<String?>(
                    value: item.id,
                    child: Text(item.title, overflow: TextOverflow.ellipsis),
                  ),
              ],
              onChanged: widget.controller.isBusy
                  ? null
                  : (id) => _selectSavedResume(
                      ready.where((item) => item.id == id).firstOrNull,
                    ),
            ),
            const SizedBox(height: 12),
            OutlinedButton.icon(
              key: const Key('temporary-resume-upload-button'),
              onPressed: widget.controller.isBusy
                  ? null
                  : () => unawaited(_pickTemporaryResume()),
              icon: const Icon(Icons.upload_file_outlined),
              label: Text(temporary == null ? '临时上传简历' : '更换临时简历'),
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
      ),
    );
  }

  JobTargetCandidate _editedCandidate(JobTargetCandidate original) {
    List<String> lines(TextEditingController controller) => controller.text
        .split('\n')
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList(growable: false);

    return JobTargetCandidate(
      source: original.source,
      generalAdviceOnly: original.generalAdviceOnly,
      jobTitle: _candidateTitle.text.trim(),
      seniority: _candidateSeniority.text.trim(),
      responsibilities: lines(_responsibilities),
      coreSkills: lines(_skills),
      communicationFocus: lines(_communication),
      practiceGoals: lines(_goals),
      scopeNotice: original.scopeNotice,
      catalogRecommendation: original.catalogRecommendation,
    );
  }

  Future<void> _startPractice() async {
    final started = await widget.controller.startPractice();
    if (started && mounted) {
      widget.onPracticeStarted?.call();
    }
  }

  Future<void> _createPreview() async {
    var replaceCurrentPractice = false;
    if (widget.controller.hasResumablePractice) {
      final action =
          await showDialog<_JobExistingPracticeAction>(
            context: context,
            builder: (context) => AlertDialog(
              title: const Text('你还有一项练习未完成'),
              content: Text(
                '当前练习：${widget.controller.resumablePracticeTitle ?? '上次练习'}\n'
                '如果继续生成本轮面试，当前练习会提前结束。',
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.of(
                    context,
                  ).pop(_JobExistingPracticeAction.cancel),
                  child: const Text('取消'),
                ),
                TextButton(
                  key: const Key('continue-existing-job-practice'),
                  onPressed: () => Navigator.of(
                    context,
                  ).pop(_JobExistingPracticeAction.continuePractice),
                  child: const Text('继续上次练习'),
                ),
                FilledButton(
                  key: const Key('replace-existing-job-practice'),
                  onPressed: () => Navigator.of(
                    context,
                  ).pop(_JobExistingPracticeAction.replace),
                  child: const Text('结束并生成新的'),
                ),
              ],
            ),
          ) ??
          _JobExistingPracticeAction.cancel;
      if (!mounted || action == _JobExistingPracticeAction.cancel) {
        return;
      }
      if (action == _JobExistingPracticeAction.continuePractice) {
        final resumed = await widget.controller.resumeCurrentPractice();
        if (resumed && mounted) {
          widget.onPracticeStarted?.call();
        }
        return;
      }
      replaceCurrentPractice = true;
    }
    final temporarySelected =
        widget.controller.resumeSelection?.temporary == true;
    final created = await widget.controller.createPreview(
      replaceCurrentPractice: replaceCurrentPractice,
    );
    final snapshotCaptured =
        created ||
        widget.controller.plan != null ||
        widget.controller.operationStage == JobPreparationOperationStage.plan;
    if (temporarySelected && snapshotCaptured) {
      await widget.resumeController?.refreshTemporary();
      await widget.resumeController?.deleteTemporary();
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
              _StepProgress(step: controller.step),
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
                child: switch (controller.step) {
                  JobPreparationStep.input => _buildInput(controller),
                  JobPreparationStep.confirmation => _buildConfirmation(
                    controller,
                  ),
                  JobPreparationStep.setup => _buildSetup(controller),
                  JobPreparationStep.preview => _buildPreview(controller),
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildInput(JobPreparationController controller) {
    final source = controller.input.source;
    return ListView(
      key: const Key('job-wizard-input-step'),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
      children: [
        Text(
          '先告诉我你要准备什么岗位',
          style: Theme.of(
            context,
          ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text('只需输入职位名称，或粘贴一份职位 JD。', style: SpeakUpDesign.body),
        const SizedBox(height: 20),
        SegmentedButton<JobTargetSource>(
          key: const Key('job-source-selector'),
          segments: const [
            ButtonSegment(
              value: JobTargetSource.jobDescription,
              label: Text('职位 JD'),
              icon: Icon(Icons.description_outlined),
            ),
            ButtonSegment(
              value: JobTargetSource.quickStart,
              label: Text('职位名称'),
              icon: Icon(Icons.bolt_outlined),
            ),
          ],
          selected: {source},
          onSelectionChanged: controller.isBusy
              ? null
              : (selection) {
                  FocusManager.instance.primaryFocus?.unfocus();
                  widget.controller.updateInput(
                    _currentInput(source: selection.single),
                  );
                },
        ),
        const SizedBox(height: 18),
        if (source == JobTargetSource.jobDescription)
          _Field(
            key: const Key('job-description-field'),
            controller: _jd,
            label: '职位描述（JD）',
            hint: '粘贴岗位职责、要求和英语使用场景',
            maxLines: 8,
            onChanged: (_) => _commitInput(),
          )
        else ...[
          _Field(
            key: const Key('job-title-field'),
            controller: _title,
            label: '目标岗位',
            hint: '例如：后端工程师',
            onChanged: (_) => _commitInput(),
          ),
        ],
        if (controller.agentIntentPrefill case final prefill?) ...[
          const SizedBox(height: 14),
          _AgentIntentCard(
            text: prefill,
            onApply: controller.applyAgentIntentPrefill,
            onDismiss: controller.dismissAgentIntentPrefill,
          ),
        ],
        if (controller.target != null && controller.candidate == null) ...[
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
        const SizedBox(height: 24),
        FilledButton.icon(
          key: const Key('analyze-job-button'),
          onPressed: controller.isBusy
              ? null
              : () {
                  _commitInput();
                  unawaited(controller.analyze());
                },
          icon: const Icon(Icons.auto_awesome_outlined),
          label: Text('生成面试信息'),
          style: _primaryButtonStyle,
        ),
      ],
    );
  }

  Widget _buildConfirmation(JobPreparationController controller) {
    final candidate = controller.candidate;
    if (candidate == null) {
      return const Center(child: Text('分析结果已失效，请返回重试。'));
    }
    return ListView(
      key: const Key('job-wizard-confirmation-step'),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
      children: [
        Text(
          '面试信息已准备好',
          style: Theme.of(
            context,
          ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text('确认岗位重点并选择是否使用简历，然后生成面试方案。'),
        const SizedBox(height: 18),
        _SummaryCard(
          title: candidate.jobTitle,
          subtitle: candidate.seniority,
          rows: [
            ('核心职责', candidate.responsibilities.join('、')),
            ('核心能力', candidate.coreSkills.join('、')),
            ('建议重点', candidate.practiceGoals.join('、')),
          ],
        ),
        const SizedBox(height: 14),
        _buildResumeSource(),
        const SizedBox(height: 14),
        ExpansionTile(
          key: const Key('job-analysis-editor'),
          tilePadding: const EdgeInsets.symmetric(horizontal: 4),
          title: const Text('查看并编辑岗位分析'),
          subtitle: Text(candidate.scopeNotice),
          children: [
            _Field(
              key: const Key('candidate-title-field'),
              controller: _candidateTitle,
              label: '岗位',
              onChanged: (_) =>
                  controller.updateCandidate(_editedCandidate(candidate)),
            ),
            const SizedBox(height: 12),
            _Field(
              key: const Key('candidate-seniority-field'),
              controller: _candidateSeniority,
              label: '职级',
              onChanged: (_) =>
                  controller.updateCandidate(_editedCandidate(candidate)),
            ),
            const SizedBox(height: 12),
            _Field(
              key: const Key('candidate-responsibilities-field'),
              controller: _responsibilities,
              label: '核心职责（每行一项）',
              maxLines: 4,
              onChanged: (_) =>
                  controller.updateCandidate(_editedCandidate(candidate)),
            ),
            const SizedBox(height: 12),
            _Field(
              key: const Key('candidate-skills-field'),
              controller: _skills,
              label: '核心能力（每行一项）',
              maxLines: 4,
              onChanged: (_) =>
                  controller.updateCandidate(_editedCandidate(candidate)),
            ),
            const SizedBox(height: 12),
            _Field(
              key: const Key('candidate-communication-field'),
              controller: _communication,
              label: '英语沟通重点（每行一项）',
              maxLines: 4,
              onChanged: (_) =>
                  controller.updateCandidate(_editedCandidate(candidate)),
            ),
            const SizedBox(height: 12),
            _Field(
              key: const Key('candidate-goals-field'),
              controller: _goals,
              label: '建议训练重点（每行一项）',
              maxLines: 4,
              onChanged: (_) =>
                  controller.updateCandidate(_editedCandidate(candidate)),
            ),
            const SizedBox(height: 12),
          ],
        ),
        if (controller.errorMessage case final message?)
          _ErrorCard(
            message: message,
            onRetry: controller.canRetry ? controller.retry : null,
          ),
        const SizedBox(height: 24),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                key: const Key('edit-job-input-button'),
                onPressed: controller.isBusy ? null : controller.returnToInput,
                style: _secondaryButtonStyle,
                child: const Text('修改原始信息'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: FilledButton(
                key: const Key('confirm-job-analysis-button'),
                onPressed: controller.isBusy
                    ? null
                    : () => unawaited(controller.confirm()),
                style: _primaryButtonStyle,
                child: const Text('确认分析'),
              ),
            ),
          ],
        ),
      ],
    );
  }

  Widget _buildSetup(JobPreparationController controller) {
    final candidate = controller.candidate;
    return ListView(
      key: const Key('job-wizard-setup-step'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
      children: [
        Text(
          '生成练习计划',
          style: Theme.of(
            context,
          ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text(
          '系统会根据已确认的岗位信息、服务端训练目录和练习策略生成不可变预览，不会立即创建练习。',
          style: SpeakUpDesign.body,
        ),
        const SizedBox(height: 18),
        _SummaryCard(
          title: candidate?.jobTitle ?? '目标岗位',
          subtitle: candidate?.seniority ?? '',
          rows: [
            ('训练重点', candidate?.practiceGoals.join('、') ?? '由服务端推荐'),
            ('简历', controller.resumeSelection?.title ?? '不使用简历'),
          ],
        ),
        if (controller.errorMessage case final message?)
          _ErrorCard(
            message: message,
            onRetry: controller.canRetry ? controller.retry : null,
          ),
        const SizedBox(height: 24),
        OutlinedButton(
          key: const Key('back-to-confirmation-button'),
          onPressed: controller.isBusy ? null : controller.returnToInput,
          style: _secondaryButtonStyle,
          child: const Text('重新填写岗位信息'),
        ),
        const SizedBox(height: 12),
        FilledButton.icon(
          key: const Key('create-plan-preview-button'),
          onPressed: controller.isBusy
              ? null
              : () => unawaited(_createPreview()),
          icon: const Icon(Icons.event_note_outlined),
          label: const Text('生成计划预览'),
          style: _primaryButtonStyle,
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
    if (jobCandidate == null) {
      return const Center(child: Text('岗位准备快照无效，请重新生成。'));
    }
    final minutes = (plan.sessionPolicy.suggestedDurationSeconds / 60).ceil();
    return ListView(
      key: const Key('job-wizard-preview-step'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
      children: [
        Text(
          '确认本次练习',
          style: Theme.of(
            context,
          ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 8),
        const Text(
          '计划已冻结。只有点击“开始练习”才会创建正式 Session。',
          style: SpeakUpDesign.body,
        ),
        const SizedBox(height: 18),
        _SummaryCard(
          key: const Key('job-plan-summary'),
          title: plan.sceneSelection.scene.name,
          subtitle: plan.practiceOption.displayName,
          rows: [
            ('岗位', jobCandidate.jobTitle),
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
        OutlinedButton(
          key: const Key('revise-job-plan-button'),
          onPressed: controller.isBusy ? null : controller.returnToSetup,
          style: _secondaryButtonStyle,
          child: const Text('返回调整'),
        ),
        const SizedBox(height: 12),
        FilledButton.icon(
          key: const Key('start-job-practice-button'),
          onPressed: controller.isBusy ? null : _startPractice,
          icon: const Icon(Icons.mic_none_rounded),
          label: Text(controller.bootstrap == null ? '开始练习' : '重新连接语音练习'),
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

class _StepProgress extends StatelessWidget {
  const _StepProgress({required this.step});

  final JobPreparationStep step;

  @override
  Widget build(BuildContext context) {
    final index = switch (step) {
      JobPreparationStep.input => 0,
      JobPreparationStep.confirmation => 1,
      JobPreparationStep.setup => 2,
      JobPreparationStep.preview => 3,
    };
    final title = switch (step) {
      JobPreparationStep.input => '岗位信息',
      JobPreparationStep.confirmation => '确认分析',
      JobPreparationStep.setup => '练习设置',
      JobPreparationStep.preview => '开始前确认',
    };
    return Semantics(
      label: '准备进度，第 ${index + 1} 步，共 4 步',
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '第 ${index + 1}/4 步 · $title',
              key: const Key('job-wizard-step-label'),
              style: SpeakUpDesign.meta.copyWith(
                color: SpeakUpDesign.primary,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            Row(
              children: List.generate(
                4,
                (item) => Expanded(
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 180),
                    height: 4,
                    margin: EdgeInsets.only(right: item == 3 ? 0 : 6),
                    decoration: BoxDecoration(
                      color: item <= index
                          ? SpeakUpDesign.primary
                          : SpeakUpDesign.border,
                      borderRadius: BorderRadius.circular(99),
                    ),
                  ),
                ),
              ),
            ),
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

final ButtonStyle _secondaryButtonStyle = OutlinedButton.styleFrom(
  minimumSize: const Size.fromHeight(52),
);
