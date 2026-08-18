import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';

final class PracticePlanClientActionCard extends StatelessWidget {
  const PracticePlanClientActionCard({
    required this.action,
    required this.onConfirm,
    super.key,
  });

  final ConfirmPracticePlanClientAction action;
  final VoidCallback? onConfirm;

  @override
  Widget build(BuildContext context) {
    final minutes = (action.suggestedDuration.inSeconds / 60).ceil();
    final isIELTS = action.practiceExperience == 'IELTS_SPEAKING';
    final isInterview = action.practiceExperience == 'INTERVIEW';
    final title = isIELTS
        ? 'IELTS Speaking · ${action.practiceScope}'
        : action.sceneName;
    final aiRole = action.aiRoles.join('、');
    final role = action.userRole == null
        ? aiRole
        : '你：${action.userRole} · AI：$aiRole';

    return Align(
      alignment: Alignment.center,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 353),
        child: Column(
          key: Key(
            'agent-client-action-practice-plan-'
            '${action.practicePlanId}-${action.planVersion}',
          ),
          mainAxisSize: MainAxisSize.min,
          children: [
            _PracticeHero(
              title: title,
              goal: action.practiceGoal,
              role: role,
              isIELTS: isIELTS,
              isInterview: isInterview,
            ),
            Transform.translate(
              offset: const Offset(0, -7),
              child: Material(
                color: SpeakUpDesign.surface,
                elevation: 4,
                shadowColor: const Color(0x1A000000),
                borderRadius: BorderRadius.circular(SpeakUpDesign.radiusMedia),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(14, 13, 14, 13),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: _PracticeFact(
                              icon: Icons.schedule_rounded,
                              label: '约 $minutes 分钟',
                            ),
                          ),
                          const SizedBox(
                            height: 32,
                            child: VerticalDivider(width: 1),
                          ),
                          Expanded(
                            child: _PracticeFact(
                              icon: Icons.chat_bubble_outline_rounded,
                              label: isInterview
                                  ? action.practiceScope
                                  : _questionCountLabel(action),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 12),
                      Semantics(
                        button: true,
                        enabled: onConfirm != null,
                        label: '开始练习',
                        child: ExcludeSemantics(
                          child: Material(
                            color: onConfirm == null
                                ? SpeakUpDesign.primaryMuted
                                : SpeakUpDesign.primary,
                            borderRadius: BorderRadius.circular(999),
                            clipBehavior: Clip.antiAlias,
                            child: InkWell(
                              key: Key(
                                'confirm-practice-plan-'
                                '${action.practicePlanId}-'
                                '${action.planVersion}',
                              ),
                              onTap: onConfirm,
                              child: Padding(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 18,
                                  vertical: 11,
                                ),
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
                                    Flexible(
                                      child: Text(
                                        '开始练习',
                                        textAlign: TextAlign.center,
                                        style: SpeakUpDesign.cardTitle.copyWith(
                                          color: onConfirm == null
                                              ? SpeakUpDesign.tertiary
                                              : SpeakUpDesign.surface,
                                        ),
                                      ),
                                    ),
                                  ],
                                ),
                              ),
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
    );
  }
}

final class _PracticeHero extends StatelessWidget {
  const _PracticeHero({
    required this.title,
    required this.goal,
    required this.role,
    required this.isIELTS,
    required this.isInterview,
  });

  final String title;
  final String goal;
  final String role;
  final bool isIELTS;
  final bool isInterview;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      container: true,
      explicitChildNodes: true,
      label: '已为你准备好。练习场景：$title。练习目标：$goal。角色：$role。',
      child: ClipRRect(
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusMedia),
        child: Stack(
          children: [
            DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: isInterview
                      ? const [Color(0xFFF3F0EA), Color(0xFFE6DDD2)]
                      : const [Color(0xFFF4F1FF), Color(0xFFE9E3FF)],
                ),
              ),
            ),
            if (isIELTS || isInterview)
              Positioned(
                top: 0,
                right: -4,
                bottom: 0,
                width: 160,
                child: Semantics(
                  image: true,
                  label: isIELTS ? 'IELTS 考官头像' : '面试官场景图',
                  child: ShaderMask(
                    blendMode: BlendMode.dstIn,
                    shaderCallback: (bounds) => const LinearGradient(
                      colors: [Colors.transparent, Colors.black],
                      stops: [0, 0.3],
                    ).createShader(bounds),
                    child: Image.asset(
                      isIELTS
                          ? 'assets/images/scenes/ielts-examiner.jpg'
                          : 'assets/images/scenes/interview-plan-card-v2.webp',
                      fit: BoxFit.cover,
                      alignment: isIELTS
                          ? const Alignment(0, -0.3)
                          : const Alignment(-1, -0.2),
                    ),
                  ),
                ),
              ),
            if (isIELTS || isInterview)
              Positioned.fill(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      colors: isInterview
                          ? const [
                              Color(0xFFF3F0EA),
                              Color(0xE6F3F0EA),
                              Color(0x00F3F0EA),
                            ]
                          : const [
                              Color(0xFFF4F1FF),
                              Color(0xE6F4F1FF),
                              Color(0x00F4F1FF),
                            ],
                      stops: [0, 0.42, 0.78],
                    ),
                  ),
                ),
              ),
            ExcludeSemantics(
              child: ConstrainedBox(
                constraints: const BoxConstraints(minHeight: 190),
                child: Padding(
                  padding: EdgeInsets.fromLTRB(
                    14,
                    14,
                    isIELTS || isInterview ? 74 : 14,
                    14,
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Align(
                        alignment: Alignment.centerLeft,
                        child: DecoratedBox(
                          decoration: BoxDecoration(
                            color: const Color(0x99FFFFFF),
                            borderRadius: BorderRadius.circular(999),
                          ),
                          child: const Padding(
                            padding: EdgeInsets.symmetric(
                              horizontal: 8,
                              vertical: 4,
                            ),
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
                      const SizedBox(height: 12),
                      Text(
                        title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: SpeakUpDesign.cardTitle.copyWith(
                          fontSize: 18,
                          height: 1.2,
                          letterSpacing: -0.25,
                        ),
                      ),
                      const SizedBox(height: 5),
                      Text(
                        goal,
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                        style: SpeakUpDesign.body.copyWith(
                          color: SpeakUpDesign.secondary,
                          fontSize: 12.5,
                          height: 1.25,
                        ),
                      ),
                      const SizedBox(height: 16),
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Icon(
                            Icons.account_circle_outlined,
                            size: 18,
                            color: SpeakUpDesign.secondary,
                          ),
                          const SizedBox(width: 7),
                          Expanded(
                            child: Text(
                              role,
                              maxLines: 2,
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
                    ],
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
            maxLines: 2,
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

String _questionCountLabel(ConfirmPracticePlanClientAction action) {
  final maximum = action.maxEffectiveTurns;
  if (maximum == 0) {
    return '${action.minEffectiveTurns}+ 个问题';
  }
  if (maximum == action.minEffectiveTurns) {
    return '$maximum 个问题';
  }
  return '${action.minEffectiveTurns}–$maximum 个问题';
}
