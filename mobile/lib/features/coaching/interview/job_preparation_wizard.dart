import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';

final _jobDescriptionMarkers = RegExp(
  r"岗位职责|工作职责|任职要求|职位要求|responsibilities?|requirements?|qualifications?|what you(?:'|’)ll do|what we(?:'|’)re looking for",
  caseSensitive: false,
);

InterviewPreparationSource _interviewPreparationSource(String value) {
  if (value.contains('\n') ||
      value.runes.length > 80 ||
      _jobDescriptionMarkers.hasMatch(value)) {
    return InterviewPreparationSource.jobDescription;
  }
  return InterviewPreparationSource.quickStart;
}

class JobPreparationWizard extends StatefulWidget {
  const JobPreparationWizard({
    required this.controller,
    this.catalogController,
    this.resumeFilePicker,
    this.onPracticeStarted,
    this.onExit,
    super.key,
  });

  final JobPreparationController controller;
  final PreparationController? catalogController;
  final InterviewResumeFilePicker? resumeFilePicker;
  final FutureOr<void> Function()? onPracticeStarted;
  final VoidCallback? onExit;

  @override
  State<JobPreparationWizard> createState() => _JobPreparationWizardState();
}

class _JobPreparationWizardState extends State<JobPreparationWizard> {
  late final TextEditingController _jobInput;
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
    unawaited(_prepareCatalog());
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
      return;
    }
    oldWidget.controller.removeListener(_rebuild);
    widget.controller.addListener(_rebuild);
    if (oldWidget.catalogController != widget.catalogController) {
      oldWidget.catalogController?.removeListener(_rebuild);
      widget.catalogController?.addListener(_rebuild);
    }
    _syncInput();
    unawaited(_prepareCatalog());
  }

  @override
  void dispose() {
    widget.controller.removeListener(_rebuild);
    widget.catalogController?.removeListener(_rebuild);
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

  void _syncInput() {
    final input = widget.controller.input;
    _replaceText(_jobInput, input.jobDescription ?? input.jobTitle);
  }

  Future<void> _prepareCatalog() async {
    final catalog = widget.catalogController;
    final candidate = widget.controller.candidate;
    if (!mounted ||
        _practiceStarted ||
        _exitInFlight ||
        _exitApproved ||
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
          _exitInFlight ||
          _exitApproved ||
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
          _exitInFlight ||
          _exitApproved ||
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

  InterviewPreparationInput _currentInput() {
    String? normalized(String value) {
      final result = value.trim();
      return result.isEmpty ? null : result;
    }

    final value = normalized(_jobInput.text);
    final resolvedSource = _interviewPreparationSource(value ?? '');
    return InterviewPreparationInput(
      source: resolvedSource,
      jobTitle: resolvedSource == InterviewPreparationSource.quickStart
          ? value
          : null,
      jobDescription:
          resolvedSource == InterviewPreparationSource.jobDescription
          ? value
          : null,
    );
  }

  void _commitInput() {
    widget.controller.updateInput(_currentInput());
  }

  Future<void> _pickResume() async {
    final resume = await widget.resumeFilePicker?.pickPdf();
    if (resume != null && mounted) {
      widget.controller.selectResume(resume);
    }
  }

  Widget _buildResumeSource() {
    if (widget.resumeFilePicker == null) return const SizedBox.shrink();
    final resume = widget.controller.resumeSelection;
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
                : () => unawaited(_pickResume()),
            icon: const Icon(Icons.upload_file_rounded),
            label: Text(resume == null ? '上传 PDF 简历' : '更换 PDF 简历'),
          ),
          if (resume != null) ...[
            const SizedBox(height: 10),
            _Notice(
              key: const Key('temporary-resume-status'),
              icon: Icons.check_circle_outline,
              text: '已选择 ${resume.name}，创建面试准备时会上传并解析。',
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
      await widget.onPracticeStarted?.call();
    }
  }

  Future<void> _createAndStartPractice() async {
    _commitInput();
    final started = await widget.controller.createAndStartPractice();
    if (started && mounted) {
      _practiceStarted = true;
      widget.catalogController?.showSceneList();
      await widget.onPracticeStarted?.call();
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
    widget.catalogController?.showSceneList();
    setState(() {});
    await WidgetsBinding.instance.endOfFrame;
    if (mounted) {
      final onExit = widget.onExit;
      if (onExit != null) {
        onExit();
      } else {
        await Navigator.of(context).maybePop();
      }
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
          title: Text(controller.openedSavedPlan ? '模拟面试' : '准备英文面试'),
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
              if (controller.interviewPreparation != null &&
                  controller.candidate == null) ...[
                const SizedBox(height: 14),
                const _Notice(
                  icon: Icons.refresh_rounded,
                  text: '岗位信息已修改，之前的分析、确认和计划预览已失效，需要重新分析。',
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
    final jobCandidate = plan.preparationSnapshot.interview?.candidate;
    final minutes = (plan.sessionPolicy.suggestedDurationSeconds / 60).ceil();
    return ListView(
      key: const Key('job-wizard-preview-step'),
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
      children: [
        Text(
          jobCandidate?.jobTitle ?? plan.sceneSelection.scene.name,
          style: SpeakUpDesign.pageTitle,
        ),
        const SizedBox(height: SpeakUpDesign.space8),
        Text(
          '${plan.sceneSelection.scene.name} · ${plan.practiceOption.displayName}',
          style: SpeakUpDesign.body,
        ),
        const SizedBox(height: SpeakUpDesign.space24),
        _PlanSummary(
          key: const Key('job-plan-summary'),
          duration: '约 $minutes 分钟',
          turns: plan.sessionPolicy.maxEffectiveTurns == 0
              ? '开放轮次'
              : '${plan.sessionPolicy.minEffectiveTurns}–'
                    '${plan.sessionPolicy.maxEffectiveTurns} 轮',
          role: plan.selectedRoles.single.displayName,
          objective: plan.practiceObjectives
              .map((item) => item.description)
              .join('、'),
        ),
        const SizedBox(height: SpeakUpDesign.space20),
        const Divider(height: 1),
        if (controller.errorMessage case final message?)
          _ErrorCard(
            key: const Key('job-preview-error'),
            message: message,
            onRetry: controller.canRetry ? controller.retry : null,
          ),
        const SizedBox(height: SpeakUpDesign.space24),
        const Text(
          '准备好后开始，届时才会创建练习记录。',
          textAlign: TextAlign.center,
          style: SpeakUpDesign.meta,
        ),
        const SizedBox(height: SpeakUpDesign.space8),
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

class _PlanSummary extends StatelessWidget {
  const _PlanSummary({
    required this.duration,
    required this.turns,
    required this.role,
    required this.objective,
    super.key,
  });

  final String duration;
  final String turns;
  final String role;
  final String objective;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          padding: const EdgeInsets.symmetric(
            horizontal: SpeakUpDesign.space16,
            vertical: SpeakUpDesign.space12,
          ),
          decoration: BoxDecoration(
            color: SpeakUpDesign.surfaceMuted,
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: _PlanStat(label: '时长', value: duration),
              ),
              Expanded(
                child: _PlanStat(label: '轮次', value: turns),
              ),
              Expanded(
                child: _PlanStat(label: '面试官', value: role),
              ),
            ],
          ),
        ),
        const SizedBox(height: SpeakUpDesign.space24),
        const Text('本次练习重点', style: SpeakUpDesign.sectionTitle),
        const SizedBox(height: SpeakUpDesign.space8),
        Text(objective.isEmpty ? '—' : objective, style: SpeakUpDesign.body),
      ],
    );
  }
}

class _PlanStat extends StatelessWidget {
  const _PlanStat({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: SpeakUpDesign.meta),
        const SizedBox(height: SpeakUpDesign.space4),
        Text(value, style: SpeakUpDesign.label),
      ],
    );
  }
}

String _busyLabel(JobPreparationOperationStage? stage) {
  return switch (stage) {
    JobPreparationOperationStage.interviewPreparation => '正在分析岗位信息',
    JobPreparationOperationStage.confirmation => '正在确认岗位分析',
    JobPreparationOperationStage.plan => '正在生成练习计划',
    JobPreparationOperationStage.session => '正在创建练习',
    JobPreparationOperationStage.voice => '正在连接语音练习',
    null => '正在处理',
  };
}

final ButtonStyle _primaryButtonStyle = FilledButton.styleFrom(
  minimumSize: const Size.fromHeight(52),
);
