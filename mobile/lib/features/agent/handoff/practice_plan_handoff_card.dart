import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';

final class PracticePlanHandoffCard extends StatelessWidget {
  const PracticePlanHandoffCard({
    required this.handoff,
    required this.onConfirm,
    super.key,
  });

  final ConfirmPracticePlanHandoff handoff;
  final VoidCallback? onConfirm;

  @override
  Widget build(BuildContext context) {
    final minutes = (handoff.suggestedDuration.inSeconds / 60).ceil();
    return Container(
      key: Key(
        'agent-handoff-practice-plan-'
        '${handoff.practicePlanId}-${handoff.planRevision}',
      ),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        border: Border.all(color: SpeakUpDesign.border),
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(handoff.target, style: SpeakUpDesign.cardTitle),
          const SizedBox(height: 8),
          Text('场景：${handoff.sceneName}', style: SpeakUpDesign.body),
          Text('角色：${handoff.roles.join('、')}', style: SpeakUpDesign.body),
          Text('范围：${handoff.practiceScope}', style: SpeakUpDesign.body),
          Text(
            handoff.maxEffectiveTurns == 0
                ? '预计 $minutes 分钟'
                : '预计 $minutes 分钟 · '
                      '${handoff.minEffectiveTurns}–${handoff.maxEffectiveTurns} 轮',
            style: SpeakUpDesign.meta,
          ),
          const SizedBox(height: 8),
          Text(handoff.confirmationPrompt, style: SpeakUpDesign.meta),
          const SizedBox(height: 10),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              key: Key(
                'confirm-practice-plan-'
                '${handoff.practicePlanId}-${handoff.planRevision}',
              ),
              onPressed: onConfirm,
              child: Text(handoff.label),
            ),
          ),
        ],
      ),
    );
  }
}
