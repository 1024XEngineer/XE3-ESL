import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class ScenarioPreparationForm extends StatefulWidget {
  const ScenarioPreparationForm({
    required this.scene,
    required this.hasPrimaryNavigation,
    required this.onBack,
    required this.onSubmit,
    this.isSubmitting = false,
    super.key,
  });

  final SceneDefinition scene;
  final bool hasPrimaryNavigation;
  final bool isSubmitting;
  final VoidCallback onBack;
  final Future<void> Function(ScenarioPreparationContext context) onSubmit;

  @override
  State<ScenarioPreparationForm> createState() =>
      _ScenarioPreparationFormState();
}

class _ScenarioPreparationFormState extends State<ScenarioPreparationForm> {
  final _formKey = GlobalKey<FormState>();
  late TextEditingController _situation;
  late TextEditingController _userRole;
  late TextEditingController _counterpartRole;
  late TextEditingController _goal;
  late TextEditingController _counterpartPersona;

  @override
  void initState() {
    super.initState();
    _initialize(widget.scene);
  }

  @override
  void didUpdateWidget(covariant ScenarioPreparationForm oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.scene.id != widget.scene.id ||
        oldWidget.scene.version != widget.scene.version) {
      _disposeControllers();
      _initialize(widget.scene);
    }
  }

  void _initialize(SceneDefinition scene) {
    final prompt = scene.prompt;
    _situation = TextEditingController(text: prompt.publicSceneBrief.trim());
    _userRole = TextEditingController(text: prompt.userRole.trim());
    _counterpartRole = TextEditingController(text: prompt.aiRole.trim());
    _goal = TextEditingController(text: prompt.practiceGoal.trim());
    _counterpartPersona = TextEditingController(
      text: prompt.personaSummary.trim(),
    );
  }

  @override
  void dispose() {
    _disposeControllers();
    super.dispose();
  }

  void _disposeControllers() {
    _situation.dispose();
    _userRole.dispose();
    _counterpartRole.dispose();
    _goal.dispose();
    _counterpartPersona.dispose();
  }

  Future<void> _submit() async {
    if (widget.isSubmitting || !(_formKey.currentState?.validate() ?? false)) {
      return;
    }
    FocusScope.of(context).unfocus();
    await widget.onSubmit(
      ScenarioPreparationContext(
        situation: _situation.text.trim(),
        userRole: _userRole.text.trim(),
        counterpartRole: _counterpartRole.text.trim(),
        goal: _goal.text.trim(),
        counterpartPersona: _counterpartPersona.text.trim(),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final pagePadding = PreparationDesign.pagePadding(
      context,
      hasPrimaryNavigation: widget.hasPrimaryNavigation,
      top: 8,
    );
    return Form(
      key: _formKey,
      child: ListView(
        key: const Key('scenario-preparation-form'),
        primary: false,
        padding: pagePadding,
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: IconButton(
              key: const Key('scenario-preparation-back'),
              tooltip: '返回场景列表',
              onPressed: widget.isSubmitting ? null : widget.onBack,
              icon: const Icon(Icons.arrow_back_rounded),
              color: PreparationDesign.ink,
              style: IconButton.styleFrom(
                backgroundColor: PreparationDesign.surface,
                side: const BorderSide(color: PreparationDesign.border),
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text(widget.scene.name, style: PreparationDesign.pageTitle),
          const SizedBox(height: 8),
          const Text(
            '开始前，先确认这次练习的场景信息。你可以直接使用默认内容，也可以按真实情况修改。',
            style: PreparationDesign.body,
          ),
          const SizedBox(height: 24),
          _ScenarioField(
            key: const Key('scenario-situation'),
            controller: _situation,
            label: '情景描述',
            hint: '例如：在周会上汇报项目进度，并回应同事的不同意见。',
            minLines: 3,
            enabled: !widget.isSubmitting,
          ),
          const SizedBox(height: 16),
          _ScenarioField(
            key: const Key('scenario-user-role'),
            controller: _userRole,
            label: '我的身份',
            hint: '例如：项目负责人',
            enabled: !widget.isSubmitting,
          ),
          const SizedBox(height: 16),
          _ScenarioField(
            key: const Key('scenario-counterpart-role'),
            controller: _counterpartRole,
            label: '对方身份',
            hint: '例如：关注交付风险的同事',
            enabled: !widget.isSubmitting,
          ),
          const SizedBox(height: 16),
          _ScenarioField(
            key: const Key('scenario-goal'),
            controller: _goal,
            label: '我的目标',
            hint: '例如：清楚说明进度、风险和下一步计划。',
            minLines: 2,
            enabled: !widget.isSubmitting,
          ),
          const SizedBox(height: 16),
          _ScenarioField(
            key: const Key('scenario-counterpart-persona'),
            controller: _counterpartPersona,
            label: '对方人设',
            hint: '例如：直接、务实，会追问具体证据。',
            minLines: 2,
            enabled: !widget.isSubmitting,
          ),
          const SizedBox(height: 28),
          FilledButton.icon(
            key: const Key('scenario-preparation-submit'),
            onPressed: widget.isSubmitting ? null : () => _submit(),
            icon: widget.isSubmitting
                ? const SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.play_arrow_rounded),
            label: Text(widget.isSubmitting ? '正在创建练习…' : '确认并开始练习'),
          ),
          const SizedBox(height: 12),
          const Text(
            '确认后会冻结为本次练习快照，后续修改不会影响已经开始的练习。',
            textAlign: TextAlign.center,
            style: PreparationDesign.meta,
          ),
        ],
      ),
    );
  }
}

class _ScenarioField extends StatelessWidget {
  const _ScenarioField({
    required this.controller,
    required this.label,
    required this.hint,
    required this.enabled,
    this.minLines = 1,
    super.key,
  });

  final TextEditingController controller;
  final String label;
  final String hint;
  final bool enabled;
  final int minLines;

  @override
  Widget build(BuildContext context) {
    return TextFormField(
      controller: controller,
      enabled: enabled,
      minLines: minLines,
      maxLines: minLines + 2,
      maxLength: 16 * 1024,
      buildCounter:
          (
            context, {
            required currentLength,
            required isFocused,
            required maxLength,
          }) => null,
      autovalidateMode: AutovalidateMode.onUserInteraction,
      textInputAction: minLines == 1
          ? TextInputAction.next
          : TextInputAction.newline,
      decoration: InputDecoration(
        labelText: label,
        hintText: hint,
        alignLabelWithHint: minLines > 1,
        filled: true,
        fillColor: PreparationDesign.surface,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PreparationDesign.radiusCard),
        ),
      ),
      validator: (value) {
        final normalized = value?.trim() ?? '';
        if (normalized.isEmpty) {
          return '请填写$label';
        }
        if (normalized.contains('\u0000') ||
            utf8.encode(normalized).length > 16 * 1024) {
          return '$label内容过长，请适当精简';
        }
        return null;
      },
    );
  }
}
