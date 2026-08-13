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
    final isIELTS = handoff.practiceExperience == 'IELTS_SPEAKING';
    final practiceSummary = <String>[
      handoff.sceneName,
      if (!isIELTS) handoff.roles.join('、'),
      handoff.practiceScope,
    ].join(' · ');
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
          Text(
            isIELTS ? practiceSummary : handoff.target,
            style: SpeakUpDesign.cardTitle,
          ),
          if (!isIELTS) ...[
            const SizedBox(height: 6),
            Text(
              practiceSummary,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: SpeakUpDesign.body,
            ),
          ],
          const SizedBox(height: 6),
          Text('约 $minutes 分钟', style: SpeakUpDesign.meta),
          const SizedBox(height: 10),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              key: Key(
                'confirm-practice-plan-'
                '${handoff.practicePlanId}-${handoff.planRevision}',
              ),
              onPressed: onConfirm,
              child: const Text('开始练习'),
            ),
          ),
        ],
      ),
    );
  }
}
