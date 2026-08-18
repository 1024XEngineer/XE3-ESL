import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';

void main() {
  test('decodes a historical v1 payload into the canonical model', () {
    final action = decodeConfirmPracticePlanClientAction(_validV1Action());

    expect(action.protocol, ConfirmPracticePlanProtocol.v1);
    expect(action.practicePlanId, _planId);
    expect(action.planVersion, 2);
    expect(action.sceneId, isNull);
    expect(action.userRole, isNull);
    expect(action.aiRoles, <String>['面试官']);
    expect(action.practiceGoal, '准备 Java 后端面试');
  });

  test('encodes and decodes only the explicit v2 payload', () {
    final envelope = encodeConfirmPracticePlanClientAction(_validV2Action());
    final action = decodeConfirmPracticePlanClientAction(envelope);

    expect(envelope.type, practicePlanConfirmClientActionType);
    expect(envelope.payload, containsPair('user_role', '候选人'));
    expect(
      envelope.payload,
      containsPair('scene_id', 'scn_interview_project_deep_dive'),
    );
    expect(envelope.payload['ai_roles'], <String>['面试官']);
    expect(envelope.payload, containsPair('practice_goal', '准备 Java 后端面试'));
    expect(envelope.payload, isNot(contains('target')));
    expect(envelope.payload, isNot(contains('roles')));
    expect(action.protocol, ConfirmPracticePlanProtocol.v2);
    expect(action.userRole, '候选人');
  });

  test('decodes an open-ended v2 PracticePlan payload', () {
    final payload = Map<String, Object?>.of(
      encodeConfirmPracticePlanClientAction(_validV2Action()).payload,
    )..['max_effective_turns'] = 0;
    final action = decodeConfirmPracticePlanClientAction(
      AgentClientAction(
        type: practicePlanConfirmClientActionType,
        payload: payload,
      ),
    );

    expect(action.minEffectiveTurns, 3);
    expect(action.maxEffectiveTurns, 0);
  });

  test('rejects mixed, malformed, or unknown version payloads', () {
    final v2 = encodeConfirmPracticePlanClientAction(_validV2Action());
    final invalidScene = Map<String, Object?>.of(v2.payload)
      ..['practice_experience'] = 'UNKNOWN';
    final duplicateRoles = Map<String, Object?>.of(v2.payload)
      ..['ai_roles'] = <String>['面试官', '面试官'];
    final mixedV1 = Map<String, Object?>.of(_validV1Action().payload)
      ..['practice_goal'] = '不允许混用字段';
    for (final action in <AgentClientAction>[
      AgentClientAction(
        type: 'practice.plan.confirm.v999',
        payload: v2.payload,
      ),
      AgentClientAction(
        type: practicePlanConfirmClientActionType,
        payload: invalidScene,
      ),
      AgentClientAction(
        type: practicePlanConfirmClientActionType,
        payload: duplicateRoles,
      ),
      AgentClientAction(
        type: practicePlanConfirmClientActionV1Type,
        payload: mixedV1,
      ),
    ]) {
      expect(
        () => decodeConfirmPracticePlanClientAction(action),
        throwsA(isA<FormatException>()),
      );
    }
  });

  test('presentation decoder skips unknown and malformed actions', () {
    final v2 = encodeConfirmPracticePlanClientAction(_validV2Action());
    expect(
      tryDecodeConfirmPracticePlanClientAction(
        AgentClientAction(
          type: 'practice.plan.confirm.v999',
          payload: v2.payload,
        ),
      ),
      isNull,
    );
    expect(
      tryDecodeConfirmPracticePlanClientAction(
        AgentClientAction(
          type: practicePlanConfirmClientActionType,
          payload: Map<String, Object?>.of(v2.payload)..remove('user_role'),
        ),
      ),
      isNull,
    );
  });

  test('refuses to encode a historical v1 domain value', () {
    expect(
      () => encodeConfirmPracticePlanClientAction(
        ConfirmPracticePlanClientAction(
          protocol: ConfirmPracticePlanProtocol.v1,
          label: '确认并开始练习',
          practicePlanId: _planId,
          planVersion: 2,
          sceneId: null,
          sceneName: '项目经历深挖',
          practiceGoal: '准备 Java 后端面试',
          aiRoles: const <String>['面试官'],
          practiceExperience: 'INTERVIEW',
          sceneCategory: 'INTERVIEW_PROFESSIONAL',
          practiceMode: 'FULL_SIMULATION',
          practiceScope: '完整模拟',
          suggestedDuration: const Duration(seconds: 600),
          minEffectiveTurns: 3,
          maxEffectiveTurns: 6,
          confirmationPrompt: '确认后将创建练习会话；确认前不会开始练习。',
        ),
      ),
      throwsA(isA<FormatException>()),
    );
  });
}

ConfirmPracticePlanClientAction _validV2Action() =>
    const ConfirmPracticePlanClientAction(
      label: '确认并开始练习',
      practicePlanId: _planId,
      planVersion: 2,
      sceneId: 'scn_interview_project_deep_dive',
      sceneName: '项目经历深挖',
      userRole: '候选人',
      aiRoles: <String>['面试官'],
      practiceGoal: '准备 Java 后端面试',
      practiceExperience: 'INTERVIEW',
      sceneCategory: 'INTERVIEW_PROFESSIONAL',
      practiceMode: 'FULL_SIMULATION',
      practiceScope: '完整模拟',
      suggestedDuration: Duration(seconds: 600),
      minEffectiveTurns: 3,
      maxEffectiveTurns: 6,
      confirmationPrompt: '确认后将创建练习会话；确认前不会开始练习。',
    );

AgentClientAction _validV1Action() => const AgentClientAction(
  type: practicePlanConfirmClientActionV1Type,
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
