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
    final title = isIELTS ? practiceSummary : handoff.target;
    return Material(
      key: Key(
        'agent-handoff-practice-plan-'
        '${handoff.practicePlanId}-${handoff.planRevision}',
      ),
      color: SpeakUpDesign.surface,
      shape: RoundedRectangleBorder(
        side: const BorderSide(color: SpeakUpDesign.border),
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: Key(
          'confirm-practice-plan-'
          '${handoff.practicePlanId}-${handoff.planRevision}',
        ),
        onTap: onConfirm,
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: [
              Expanded(
                child: isIELTS
                    ? Text.rich(
                        TextSpan(
                          children: [
                            TextSpan(
                              text: title,
                              style: SpeakUpDesign.cardTitle,
                            ),
                            TextSpan(
                              text: '   约 $minutes 分钟',
                              style: SpeakUpDesign.meta,
                            ),
                          ],
                        ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(title, style: SpeakUpDesign.cardTitle),
                          const SizedBox(height: 6),
                          Text(
                            practiceSummary,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: SpeakUpDesign.body,
                          ),
                          const SizedBox(height: 6),
                          Text('约 $minutes 分钟', style: SpeakUpDesign.meta),
                        ],
                      ),
              ),
              if (onConfirm != null) ...[
                const SizedBox(width: 12),
                const Icon(
                  Icons.chevron_right_rounded,
                  size: 22,
                  color: SpeakUpDesign.secondary,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
