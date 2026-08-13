import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';

class InterviewCatalog extends StatelessWidget {
  const InterviewCatalog({
    required this.plans,
    required this.loading,
    required this.onCreatePressed,
    required this.onPlanPressed,
    required this.onPlanDeleted,
    required this.onRetry,
    this.errorMessage,
    super.key,
  });

  final List<PracticePlanSummary> plans;
  final bool loading;
  final String? errorMessage;
  final VoidCallback? onCreatePressed;
  final ValueChanged<PracticePlanSummary> onPlanPressed;
  final ValueChanged<PracticePlanSummary> onPlanDeleted;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (loading && plans.isEmpty)
          const Center(child: CircularProgressIndicator())
        else if (errorMessage != null && plans.isEmpty)
          _LoadError(message: errorMessage!, onRetry: onRetry)
        else if (plans.isEmpty)
          _EmptyState(onCreatePressed: onCreatePressed)
        else ...[
          for (var index = 0; index < plans.length; index++) ...[
            _PlanCard(
              plan: plans[index],
              onPressed: () => onPlanPressed(plans[index]),
              onDelete: () => _confirmDelete(context, plans[index]),
            ),
            if (index != plans.length - 1) const SizedBox(height: 12),
          ],
          if (errorMessage != null) ...[
            const SizedBox(height: 16),
            _LoadError(message: errorMessage!, onRetry: onRetry),
          ],
        ],
      ],
    );
  }

  Future<void> _confirmDelete(
    BuildContext context,
    PracticePlanSummary plan,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除模拟面试？'),
        content: const Text('该面试将从列表移除，已产生的练习和复盘会保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const Key('confirm-delete-interview-plan'),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      onPlanDeleted(plan);
    }
  }
}

class InterviewCatalogCreateButton extends StatelessWidget {
  const InterviewCatalogCreateButton({required this.onPressed, super.key});

  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return IconButton.filled(
      key: const Key('create-interview-plan'),
      tooltip: '创建模拟面试',
      onPressed: onPressed,
      icon: const Icon(Icons.add_rounded),
      style: IconButton.styleFrom(
        minimumSize: const Size(44, 44),
        backgroundColor: PreparationDesign.ink,
        foregroundColor: Colors.white,
      ),
    );
  }
}

class _PlanCard extends StatelessWidget {
  const _PlanCard({
    required this.plan,
    required this.onPressed,
    required this.onDelete,
  });

  final PracticePlanSummary plan;
  final VoidCallback onPressed;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final minutes = (plan.suggestedDurationSeconds / 60).ceil();
    final title = plan.jobTitle.isEmpty ? plan.sceneName : plan.jobTitle;
    return Card(
      key: Key('interview-plan-${plan.id}'),
      margin: EdgeInsets.zero,
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onPressed,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 44,
                    height: 44,
                    decoration: BoxDecoration(
                      color: PreparationDesign.scenarioTint,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: const Icon(Icons.mic_none_rounded),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  IconButton(
                    key: Key('delete-interview-plan-${plan.id}'),
                    tooltip: '删除模拟面试',
                    onPressed: onDelete,
                    icon: const Icon(Icons.delete_outline_rounded),
                  ),
                  const Icon(Icons.chevron_right_rounded),
                ],
              ),
              Padding(
                padding: const EdgeInsets.only(left: 56, top: 4),
                child: Text(
                  plan.practiceScope,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(color: PreparationDesign.inkSecondary),
                ),
              ),
              Padding(
                padding: const EdgeInsets.only(left: 56, top: 10),
                child: Wrap(
                  spacing: 16,
                  runSpacing: 8,
                  children: [
                    _PlanMeta(
                      icon: Icons.schedule_rounded,
                      label: '约 $minutes 分钟',
                    ),
                    _PlanMeta(
                      icon: Icons.repeat_rounded,
                      label: plan.maxEffectiveTurns == 0
                          ? '开放轮次'
                          : '${plan.minEffectiveTurns}–${plan.maxEffectiveTurns} 轮',
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

class _PlanMeta extends StatelessWidget {
  const _PlanMeta({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: PreparationDesign.inkTertiary),
        const SizedBox(width: 4),
        Flexible(
          child: Text(
            label,
            style: PreparationDesign.meta.copyWith(
              color: PreparationDesign.inkTertiary,
            ),
          ),
        ),
      ],
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.onCreatePressed});

  final VoidCallback? onCreatePressed;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('interview-plan-empty'),
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 40),
      decoration: BoxDecoration(
        color: PreparationDesign.surface,
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: PreparationDesign.border),
      ),
      child: Column(
        children: [
          const Icon(Icons.event_note_outlined, size: 40),
          const SizedBox(height: 14),
          Text(
            '还没有模拟面试',
            style: Theme.of(
              context,
            ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w800),
          ),
          const SizedBox(height: 6),
          const Text('点击右上角 +，从目标岗位开始创建。'),
          const SizedBox(height: 18),
          FilledButton(onPressed: onCreatePressed, child: const Text('创建第一场')),
        ],
      ),
    );
  }
}

class _LoadError extends StatelessWidget {
  const _LoadError({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Expanded(child: Text(message)),
        TextButton(onPressed: onRetry, child: const Text('重试')),
      ],
    );
  }
}
