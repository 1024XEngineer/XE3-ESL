import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/agent/handoff/agent_handoff_codec.dart';

void main() {
  test('decodes one exact executable PracticePlan handoff', () {
    final handoffs = decodeAgentHandoffs(<Object?>[_validHandoff()]);

    expect(handoffs, hasLength(1));
    final handoff = handoffs.single as ConfirmPracticePlanHandoff;
    expect(handoff.practicePlanId, _planId);
    expect(handoff.planRevision, 2);
    expect(handoff.roles, <String>['面试官']);
  });

  test('decodes an open-ended PracticePlan handoff', () {
    final payload = _validHandoff()..['max_effective_turns'] = 0;
    final handoff =
        decodeAgentHandoffs(<Object?>[payload]).single
            as ConfirmPracticePlanHandoff;

    expect(handoff.minEffectiveTurns, 3);
    expect(handoff.maxEffectiveTurns, 0);
  });

  test('rejects malformed or ambiguous handoff payloads', () {
    final unknownField = _validHandoff()..['unexpected'] = true;
    final invalidScene = _validHandoff()..['practice_experience'] = 'UNKNOWN';
    final duplicateRoles = _validHandoff()..['roles'] = <String>['面试官', '面试官'];

    for (final payload in <Object?>[
      <Object?>[unknownField],
      <Object?>[invalidScene],
      <Object?>[duplicateRoles],
      List<Object?>.generate(5, (_) => _validHandoff()),
    ]) {
      expect(
        () => decodeAgentHandoffs(payload),
        throwsA(isA<FormatException>()),
      );
    }
  });
}

Map<String, Object?> _validHandoff() => <String, Object?>{
  'type': 'confirm_practice_plan',
  'label': '确认并开始练习',
  'practice_plan_id': _planId,
  'plan_revision': 2,
  'target': '准备 Java 后端面试',
  'scene_name': '项目经历深挖',
  'practice_experience': 'INTERVIEW',
  'scene_category': 'INTERVIEW_PROFESSIONAL',
  'practice_mode': 'FULL_SIMULATION',
  'roles': <String>['面试官'],
  'practice_scope': '完整模拟',
  'suggested_duration_seconds': 600,
  'min_effective_turns': 3,
  'max_effective_turns': 6,
  'executable_status': 'ready',
  'confirmation_prompt': '确认后将创建练习会话；确认前不会开始练习。',
};

const _planId = '10000000-0000-4000-8000-000000000001';
