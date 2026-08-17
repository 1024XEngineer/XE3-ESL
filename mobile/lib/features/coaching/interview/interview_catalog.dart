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
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除模拟面试？'),
        content: const Text('该面试将从列表移除，已产生的练习和复盘会保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const Key('confirm-delete-interview-plan'),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed == true && context.mounted) {
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
    return Semantics(
      button: true,
      label: '模拟面试：$title',
      hint: '长按删除模拟面试',
      onTap: onPressed,
      onLongPress: onDelete,
      excludeSemantics: true,
      child: Card(
        key: Key('interview-plan-${plan.id}'),
        margin: EdgeInsets.zero,
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onPressed,
          onLongPress: onDelete,
          child: Stack(
            children: [
              Positioned.fill(
                child: Image.asset(
                  'assets/images/scenes/interview-plan-card-v2.webp',
                  key: Key('interview-plan-cover-${plan.id}'),
                  fit: BoxFit.cover,
                  excludeFromSemantics: true,
                ),
              ),
              const Positioned.fill(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      colors: [
                        Color(0x18000000),
                        Color(0x66000000),
                        Color(0xD6000000),
                      ],
                      stops: [0, 0.46, 1],
                    ),
                  ),
                ),
              ),
              ConstrainedBox(
                constraints: const BoxConstraints(minHeight: 164),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 14, 10, 14),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Expanded(flex: 4, child: SizedBox.shrink()),
                      const SizedBox(width: 12),
                      Expanded(
                        flex: 6,
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Align(
                              alignment: Alignment.centerLeft,
                              child: Container(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 9,
                                  vertical: 4,
                                ),
                                decoration: BoxDecoration(
                                  color: Colors.white.withValues(alpha: 0.16),
                                  borderRadius: BorderRadius.circular(999),
                                ),
                                child: Text(
                                  '模拟面试',
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: PreparationDesign.meta.copyWith(
                                    color: Colors.white,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                              ),
                            ),
                            const SizedBox(height: 6),
                            Text(
                              title,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context).textTheme.titleMedium
                                  ?.copyWith(
                                    color: Colors.white,
                                    fontWeight: FontWeight.w800,
                                    height: 1.1,
                                  ),
                            ),
                            const SizedBox(height: 6),
                            Text(
                              plan.practiceScope,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: PreparationDesign.body.copyWith(
                                color: Colors.white70,
                              ),
                            ),
                            const SizedBox(height: 10),
                            Wrap(
                              spacing: 14,
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
                          ],
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

class _PlanMeta extends StatelessWidget {
  const _PlanMeta({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: Colors.white70),
        const SizedBox(width: 4),
        Flexible(
          child: Text(
            label,
            style: PreparationDesign.meta.copyWith(color: Colors.white70),
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
