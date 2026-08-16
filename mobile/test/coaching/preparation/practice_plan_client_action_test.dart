import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';

void main() {
  test('decodes one exact PracticePlan business payload', () {
    final action = decodeConfirmPracticePlanClientAction(_validAction());

    expect(action.practicePlanId, _planId);
    expect(action.planVersion, 2);
    expect(action.roles, <String>['面试官']);
  });

  test('decodes an open-ended PracticePlan payload', () {
    final payload = Map<String, Object?>.of(_validAction().payload)
      ..['max_effective_turns'] = 0;
    final action = decodeConfirmPracticePlanClientAction(
      AgentClientAction(
        type: practicePlanConfirmClientActionType,
        payload: payload,
      ),
    );

    expect(action.minEffectiveTurns, 3);
    expect(action.maxEffectiveTurns, 0);
  });

  test('rejects the wrong version or malformed business payload', () {
    final invalidScene = Map<String, Object?>.of(_validAction().payload)
      ..['practice_experience'] = 'UNKNOWN';
    final duplicateRoles = Map<String, Object?>.of(_validAction().payload)
      ..['roles'] = <String>['面试官', '面试官'];
    for (final action in <AgentClientAction>[
      AgentClientAction(
        type: 'practice.plan.confirm.v2',
        payload: _validAction().payload,
      ),
      AgentClientAction(
        type: practicePlanConfirmClientActionType,
        payload: invalidScene,
      ),
      AgentClientAction(
        type: practicePlanConfirmClientActionType,
        payload: duplicateRoles,
      ),
    ]) {
      expect(
        () => decodeConfirmPracticePlanClientAction(action),
        throwsA(isA<FormatException>()),
      );
    }
  });
}

AgentClientAction _validAction() => AgentClientAction(
  type: practicePlanConfirmClientActionType,
  payload: <String, Object?>{
    'label': '确认并开始练习',
    'practice_plan_id': _planId,
    'plan_version': 2,
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
    'confirmation_prompt': '确认后将创建练习会话；确认前不会开始练习。',
  },
);

const _planId = '10000000-0000-4000-8000-000000000001';
