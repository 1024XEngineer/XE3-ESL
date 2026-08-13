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
    final title = isIELTS
        ? 'IELTS Speaking · ${handoff.practiceScope}'
        : handoff.target;
    final role = handoff.roles.join('、');

    return Align(
      alignment: Alignment.center,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 353),
        child: SizedBox(
          key: Key(
            'agent-handoff-practice-plan-'
            '${handoff.practicePlanId}-${handoff.planRevision}',
          ),
          width: double.infinity,
          height: 282,
          child: Stack(
            children: [
              Positioned(
                top: 0,
                left: 0,
                right: 0,
                height: 160,
                child: _PracticeHero(
                  title: title,
                  role: role,
                  isIELTS: isIELTS,
                ),
              ),
              Positioned(
                left: 0,
                right: 0,
                bottom: 0,
                height: 129,
                child: Material(
                  color: SpeakUpDesign.surface,
                  elevation: 4,
                  shadowColor: const Color(0x1A000000),
                  borderRadius: BorderRadius.circular(
                    SpeakUpDesign.radiusMedia,
                  ),
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(14, 13, 14, 13),
                    child: Column(
                      children: [
                        Expanded(
                          child: Row(
                            children: [
                              Expanded(
                                child: _PracticeFact(
                                  icon: Icons.schedule_rounded,
                                  label: '约 $minutes 分钟',
                                ),
                              ),
                              const SizedBox(
                                height: 24,
                                child: VerticalDivider(width: 1),
                              ),
                              Expanded(
                                child: _PracticeFact(
                                  icon: Icons.chat_bubble_outline_rounded,
                                  label: _questionCountLabel(handoff),
                                ),
                              ),
                            ],
                          ),
                        ),
                        const SizedBox(height: 9),
                        Material(
                          color: onConfirm == null
                              ? SpeakUpDesign.primaryMuted
                              : SpeakUpDesign.primary,
                          borderRadius: BorderRadius.circular(999),
                          clipBehavior: Clip.antiAlias,
                          child: InkWell(
                            key: Key(
                              'confirm-practice-plan-'
                              '${handoff.practicePlanId}-'
                              '${handoff.planRevision}',
                            ),
                            onTap: onConfirm,
                            child: SizedBox(
                              height: 46,
                              child: Row(
                                mainAxisAlignment: MainAxisAlignment.center,
                                children: [
                                  Icon(
                                    Icons.play_arrow_rounded,
                                    size: 23,
                                    color: onConfirm == null
                                        ? SpeakUpDesign.tertiary
                                        : SpeakUpDesign.surface,
                                  ),
                                  const SizedBox(width: 7),
                                  Text(
                                    '开始练习',
                                    style: SpeakUpDesign.cardTitle.copyWith(
                                      color: onConfirm == null
                                          ? SpeakUpDesign.tertiary
                                          : SpeakUpDesign.surface,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
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

final class _PracticeHero extends StatelessWidget {
  const _PracticeHero({
    required this.title,
    required this.role,
    required this.isIELTS,
  });

  final String title;
  final String role;
  final bool isIELTS;

  @override
  Widget build(BuildContext context) {
    return ClipRRect(
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusMedia),
      child: Stack(
        fit: StackFit.expand,
        children: [
          const DecoratedBox(
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [Color(0xFFF4F1FF), Color(0xFFE9E3FF)],
              ),
            ),
          ),
          if (isIELTS)
            Positioned(
              top: 0,
              right: -4,
              bottom: 0,
              width: 160,
              child: Semantics(
                image: true,
                label: 'IELTS 考官头像',
                child: ShaderMask(
                  blendMode: BlendMode.dstIn,
                  shaderCallback: (bounds) => const LinearGradient(
                    colors: [Colors.transparent, Colors.black],
                    stops: [0, 0.3],
                  ).createShader(bounds),
                  child: Image.asset(
                    'assets/images/scenes/ielts-examiner.jpg',
                    fit: BoxFit.cover,
                    alignment: const Alignment(0, -0.3),
                  ),
                ),
              ),
            ),
          if (isIELTS)
            const Positioned.fill(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    colors: [
                      Color(0xFFF4F1FF),
                      Color(0xE6F4F1FF),
                      Color(0x00F4F1FF),
                    ],
                    stops: [0, 0.42, 0.78],
                  ),
                ),
              ),
            ),
          Positioned(
            top: 14,
            left: 14,
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: const Color(0x99FFFFFF),
                borderRadius: BorderRadius.circular(999),
              ),
              child: const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                child: Text(
                  '已为你准备好',
                  style: TextStyle(
                    color: SpeakUpDesign.ink,
                    fontSize: 11.5,
                    fontWeight: FontWeight.w500,
                    height: 1.1,
                  ),
                ),
              ),
            ),
          ),
          Positioned(
            top: 48,
            left: 14,
            right: isIELTS ? 74 : 14,
            child: Text(
              title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: SpeakUpDesign.cardTitle.copyWith(
                fontSize: 18,
                height: 1.2,
                letterSpacing: -0.25,
              ),
            ),
          ),
          Positioned(
            top: 88,
            left: 14,
            right: isIELTS ? 90 : 14,
            child: Row(
              children: [
                const Icon(
                  Icons.account_circle_outlined,
                  size: 18,
                  color: SpeakUpDesign.secondary,
                ),
                const SizedBox(width: 7),
                Expanded(
                  child: Text(
                    isIELTS ? '$role · IELTS Examiner' : role,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: SpeakUpDesign.ink,
                      fontSize: 12.5,
                      fontWeight: FontWeight.w500,
                      height: 1.25,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

final class _PracticeFact extends StatelessWidget {
  const _PracticeFact({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Icon(icon, size: 19, color: SpeakUpDesign.secondary),
        const SizedBox(width: 7),
        Flexible(
          child: Text(
            label,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: SpeakUpDesign.body.copyWith(
              color: SpeakUpDesign.ink,
              fontSize: 13,
            ),
          ),
        ),
      ],
    );
  }
}

String _questionCountLabel(ConfirmPracticePlanHandoff handoff) {
  final maximum = handoff.maxEffectiveTurns;
  if (maximum == 0) {
    return '${handoff.minEffectiveTurns}+ 个问题';
  }
  if (maximum == handoff.minEffectiveTurns) {
    return '$maximum 个问题';
  }
  return '${handoff.minEffectiveTurns}–$maximum 个问题';
}
